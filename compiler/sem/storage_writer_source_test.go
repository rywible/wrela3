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

use { StorageWriter, PendingAtomicGroup, CommittedAtomicGroup, CommitToken, StorageAppendResult } from storage.writer

data StorageWriterConsumer {
    writer: StorageWriter
    pending: PendingAtomicGroup
    committed: CommittedAtomicGroup
    token: CommitToken
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
		"self.next_event_id = last + 1",
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
}

func checkedStorageWriterSourceIndex(t *testing.T) *Index {
	t.Helper()
	modules := parseStorageWriterModules(t, `
module sem.storage_writer_mirror

use { StorageWriter, PendingAtomicGroup, CommittedAtomicGroup, CommitToken, StorageAppendResult } from storage.writer

data StorageWriterMirror {
    writer: StorageWriter
    pending: PendingAtomicGroup
    committed: CommittedAtomicGroup
    token: CommitToken
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
