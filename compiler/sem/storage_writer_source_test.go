package sem

import (
	"os"
	"strings"
	"testing"

	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/parse"
	"github.com/ryanwible/wrela3/compiler/source"
)

func TestStorageWriterSourceCompiles(t *testing.T) {
	modules := parseStorageWriterModules(t, `
	module sem.storage_writer_consumer

use { StorageWriter, PendingAtomicGroup, CommittedAtomicGroup, CommitToken, StorageAppendResult, DurableBlobRef } from storage.writer
use { UnpublishedBlobRef, PublishedBlobRef } from storage.blob

data StorageWriterConsumer {
    writer: StorageWriter
    pending: PendingAtomicGroup
    committed: CommittedAtomicGroup
    token: CommitToken
    blob: UnpublishedBlobRef
	    durable_blob: DurableBlobRef
	    published_blob: PublishedBlobRef
	    result: StorageAppendResult
	}
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	_, ds := checkAllowingMissingImage(t, index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}
}

func TestStorageWriterSourceMirrorContract(t *testing.T) {
	index := checkedStorageWriterSourceIndex(t)
	writer := moduleType(t, index, "storage.writer", "StorageWriter")
	assertMethodExists(t, writer, "enqueue_atomic_group")
	assertMethodExists(t, writer, "on_durability_completed")
	assertMethodExists(t, writer, "publish_committed_group")
	assertMethodExists(t, writer, "publish_blob_ref")
	assertTypeFields(t, writer, map[string]string{
		"next_event_id":    "U64",
		"next_stream_id":   "U64",
		"durable_frontier": "U64",
		"open_batch_slots": "U64",
		"foreground":       "ForegroundStoragePath",
		"background":       "BackgroundStoragePath",
		"stream_directory": "StreamDirectory",
		"metrics":          "StorageMetrics",
		"committed_groups": "CoreSpscProducer<CommittedAtomicGroup>",
	})
	assertTypeFields(t, moduleType(t, index, "storage.writer", "PendingAtomicGroup"), map[string]string{
		"semantic_event_count":      "U64",
		"reserved_empty_slot_count": "U64",
	})
	assertTypeFields(t, moduleType(t, index, "storage.writer", "StorageAppendResult"), map[string]string{
		"accepted":         "Bool",
		"first_event_id":   "U64",
		"last_event_id":    "U64",
		"open_batch_slots": "U64",
		"flush_requested":  "Bool",
	})
	source := readRepoFile(t, "wrela/storage/writer.wrela")
	enqueue := sourceBetween(t, source, "fn enqueue_atomic_group(self, group: PendingAtomicGroup) -> StorageAppendResult {", "\n    fn on_durability_completed")
	for _, want := range []string{
		"group.semantic_event_count == 0",
		"let consumed_slots = group.semantic_event_count + group.reserved_empty_slot_count",
		"self.next_event_id = first + consumed_slots",
		"self.open_batch_slots = open",
		"STORAGE_MAX_ATOMIC_GROUP_SLOTS",
		"STORAGE_TARGET_BATCH_SLOTS",
		"STORAGE_MAX_BATCH_SLOTS",
		"if combined < STORAGE_TARGET_BATCH_SLOTS",
	} {
		if !strings.Contains(enqueue, want) {
			t.Fatalf("StorageWriter.enqueue_atomic_group missing %q", want)
		}
	}
	for _, forbidden := range []string{" > 32", ">= 64", "<= 64", "<= 72"} {
		if strings.Contains(enqueue, forbidden) {
			t.Fatalf("StorageWriter.enqueue_atomic_group should use storage constants, found %q", forbidden)
		}
	}
	publish := sourceBetween(t, source, "fn publish_committed_group(self, token: CommitToken) -> CommittedAtomicGroup {", "\n    fn publish_blob_ref")
	for _, want := range []string{
		"self.durable_frontier = token.last_event_id",
		"self.committed_groups.try_send",
		"Result.Ok",
		"self.metrics.core_link_committed_groups = self.metrics.core_link_committed_groups + 1",
		"self.metrics.core_link_backpressure_count = self.metrics.core_link_backpressure_count + 1",
		"return CommittedAtomicGroup(first_event_id = 0, last_event_id = 0)",
	} {
		if !strings.Contains(publish, want) {
			t.Fatalf("StorageWriter.publish_committed_group missing %q", want)
		}
	}
	assertOrderedSubstrings(t, publish, []string{
		"self.committed_groups.try_send",
		"Result.Ok",
		"if self.durable_frontier < token.last_event_id",
		"self.durable_frontier = token.last_event_id",
		"Result.Err",
		"self.metrics.core_link_backpressure_count = self.metrics.core_link_backpressure_count + 1",
		"return CommittedAtomicGroup(first_event_id = 0, last_event_id = 0)",
	})
}

func TestStorageWriterBlobPublicationUsesStorageBlobRefs(t *testing.T) {
	index := checkedStorageWriterSourceIndex(t)
	durableBlob := moduleType(t, index, "storage.writer", "DurableBlobRef")
	assertTypeFields(t, durableBlob, map[string]string{
		"unpublished": "UnpublishedBlobRef",
	})
	writer := moduleType(t, index, "storage.writer", "StorageWriter")
	publish := methodByName(t, writer, "publish_blob_ref")
	assertMethodSignature(t, publish, []string{"durable:DurableBlobRef"}, "PublishedBlobRef")

	source := readRepoFile(t, "wrela/storage/writer.wrela")
	for _, want := range []string{
		"BlobTruth, RelocateBlobProposal, UnpublishedBlobRef, PublishedBlobRef",
		"data DurableBlobRef",
		"unpublished: UnpublishedBlobRef",
		"data BlobRelocateResult",
		"class BlobRelocateRequest",
		"truth: BlobTruth",
		"proposal: RelocateBlobProposal",
		"fn publish_blob_ref(self, durable: DurableBlobRef) -> PublishedBlobRef",
		"return PublishedBlobRef(ref = durable.unpublished.ref)",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("storage.writer missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"\ndata BlobRef {\n",
		"\ndata PublishedBlobRef {\n",
		"fn publish_blob_ref(self, ref: BlobRef) -> PublishedBlobRef",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("storage.writer must not contain %q", forbidden)
		}
	}
}

func TestStorageWriterDurabilityMirrorContract(t *testing.T) {
	index := checkedStorageWriterSourceIndex(t)
	writer := moduleType(t, index, "storage.writer", "StorageWriter")
	assertMethodExists(t, writer, "on_durability_completed")
	assertMethodExists(t, writer, "publish_committed_group")
	token := moduleType(t, index, "storage.writer", "CommitToken")
	assertMethodExists(t, token, "acknowledged")
	assertTypeFields(t, token, map[string]string{
		"pending_write_count":   "U64",
		"completed_write_count": "U64",
		"flush_required":        "Bool",
		"flush_completed":       "Bool",
		"durability_failed":     "Bool",
	})
	source := readRepoFile(t, "wrela/storage/writer.wrela")
	durable := sourceBetween(t, source, "fn on_durability_completed(self, token: CommitToken) -> StorageAppendResult {", "\n    fn publish_committed_group")
	for _, want := range []string{
		"if self.durable_frontier < token.last_event_id",
		"self.durable_frontier = token.last_event_id",
		"if token.flush_required",
		"if token.flush_completed",
		"self.open_batch_slots = 0",
	} {
		if !strings.Contains(durable, want) {
			t.Fatalf("StorageWriter.on_durability_completed missing %q", want)
		}
	}
}

func checkedStorageWriterSourceIndex(t *testing.T) *Index {
	t.Helper()
	modules := parseStorageWriterModules(t, `
module sem.storage_writer_mirror

use { StorageWriter, PendingAtomicGroup, CommittedAtomicGroup, CommitToken, StorageAppendResult, DurableBlobRef } from storage.writer
use { PublishedBlobRef } from storage.blob

data StorageWriterMirror {
    writer: StorageWriter
    pending: PendingAtomicGroup
    committed: CommittedAtomicGroup
    token: CommitToken
	    durable_blob: DurableBlobRef
	    published_blob: PublishedBlobRef
	    result: StorageAppendResult
	}
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	checked, ds := checkAllowingMissingImage(t, index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}
	return checked.Index
}

func parseStorageWriterModules(t *testing.T, consumer string) []*ast.Module {
	t.Helper()
	modules := parseUEFIModuleSet(t)
	paths := []string{
		repoPath(t, "wrela/storage/blob.wrela"),
		repoPath(t, "wrela/storage/format.wrela"),
		repoPath(t, "wrela/storage/stream.wrela"),
		repoPath(t, "wrela/storage/writer.wrela"),
	}
	files := make([]*source.File, 0, len(paths)+1)
	for i, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		files = append(files, source.NewFile(source.FileID(i+1), path, string(raw)))
	}
	files = append(files, source.NewFile(source.FileID(len(files)+1), "storage-writer-consumer.wrela", consumer))
	parsed, ds := parse.ParseGraph(source.Graph{Files: files})
	if len(ds) != 0 {
		t.Fatalf("parse diagnostics: %#v", ds)
	}
	return append(modules, parsed...)
}
