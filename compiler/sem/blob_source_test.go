package sem

import (
	"os"
	"strings"
	"testing"

	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/parse"
	"github.com/ryanwible/wrela3/compiler/source"
)

func TestBlobSourceCompiles(t *testing.T) {
	modules := parseBlobModules(t, `
module sem.blob_consumer

use { BlobRef, Extent, BlobManifest, BlobExtentAllocator } from storage.blob

data BlobConsumer {
    ref: BlobRef
    extent: Extent
    manifest: BlobManifest
    allocator: BlobExtentAllocator
}
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	_, ds := checkAllowingMissingImage(t, index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}
}

func TestBlobSourceMirrorContract(t *testing.T) {
	modules := parseBlobModules(t, `
module sem.blob_mirror

use {
    BLOB_ALLOCATOR_FREE_EXTENT_LIMIT,
    BlobRef,
    BlobReclaimableExtents,
    Extent,
    BlobManifest,
    BlobExtentAllocator
} from storage.blob

const MIRRORED_FREE_EXTENT_LIMIT: U64 = BLOB_ALLOCATOR_FREE_EXTENT_LIMIT

data BlobMirror {
    ref: BlobRef
    reclaimable: BlobReclaimableExtents
    extent: Extent
    manifest: BlobManifest
    allocator: BlobExtentAllocator
}
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	checked, ds := checkAllowingMissingImage(t, index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}

	assertWrelaConstU64(t, checked.Index, "storage.blob", "BLOB_ALLOCATOR_FREE_EXTENT_LIMIT", 1024)
	assertTypeFields(t, moduleType(t, checked.Index, "storage.blob", "BlobRef"), map[string]string{
		"blob_id":     "U64",
		"start_lba":   "U64",
		"block_count": "U64",
	})
	assertTypeFields(t, moduleType(t, checked.Index, "storage.blob", "Extent"), map[string]string{
		"start_lba":   "U64",
		"block_count": "U64",
	})
	assertTypeFields(t, moduleType(t, checked.Index, "storage.blob", "BlobReclaimableExtents"), map[string]string{
		"extent_count": "U64",
		"extents":      "MutableSlice<Extent>",
	})
	assertTypeFields(t, moduleType(t, checked.Index, "storage.blob", "BlobManifest"), map[string]string{
		"blob_id":          "U64",
		"key_metadata_ref": "U64",
		"extent_count":     "U64",
		"logical_bytes":    "U64",
	})
	allocator := moduleType(t, checked.Index, "storage.blob", "BlobExtentAllocator")
	assertTypeFields(t, allocator, map[string]string{
		"free_extent_count": "U64",
		"free_extents":      "MutableSlice<Extent>",
	})
	assertMethodExists(t, allocator, "allocate")
	assertMethodExists(t, allocator, "free")
	assertMethodExists(t, allocator, "extents")

	source := readRepoFile(t, "wrela/storage/blob.wrela")
	for _, want := range []string{
		"while index < self.free_extent_count",
		"self.free_extents.get(index = index)",
		"current.start_lba + block_count",
		"current.block_count - block_count",
		"extent.start_lba + extent.block_count",
		"current.start_lba + current.block_count",
		"if self.free_extent_count >= BLOB_ALLOCATOR_FREE_EXTENT_LIMIT",
		"self.free_extents.set(index = insert_index, value = extent)",
		"fn coalesce_sorted(self)",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("BlobExtentAllocator source missing split/coalesce shape %q", want)
		}
	}
	if strings.Contains(source, "first: Extent") {
		t.Fatalf("BlobExtentAllocator must not be a single-entry first extent store")
	}
	coalesce := strings.Index(source, "if self.merge_with_existing(extent = extent)")
	capacity := strings.Index(source, "if self.free_extent_count >= BLOB_ALLOCATOR_FREE_EXTENT_LIMIT")
	if coalesce < 0 || capacity < 0 || capacity < coalesce {
		t.Fatalf("BlobExtentAllocator.free must attempt coalescing before enforcing capacity")
	}
	if strings.Contains(source, "acknowledged_refs.blob_id == self.allocated.start_lba") {
		t.Fatalf("BlobOrphanCollector must not compare blob_id to start_lba")
	}
	if strings.Contains(source, "fn reclaimable(self) -> Extent") {
		t.Fatalf("BlobOrphanCollector must not expose single-output reclaimable API")
	}
	for _, want := range []string{
		"data BlobReclaimableExtents {",
		"extent_count: U64",
		"extents: MutableSlice<Extent>",
		"allocated_extents: MutableSlice<Extent>",
		"acknowledged_refs: MutableSlice<BlobRef>",
		"fn reclaimable(self, scratch: MutableSlice<Extent>) -> BlobReclaimableExtents",
		"let written = 0",
		"while index < self.allocated_extent_count",
		"if written < scratch.length",
		"scratch.set(index = written, value = allocated)",
		"written = written + 1",
		"return BlobReclaimableExtents(extent_count = written, extents = scratch)",
		"while index < self.acknowledged_ref_count",
		"ref.start_lba == extent.start_lba",
		"ref.block_count == extent.block_count",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("BlobOrphanCollector source missing extent liveness check %q", want)
		}
	}
}

func TestBlobRelocationMirrorContract(t *testing.T) {
	modules := parseBlobModules(t, `
module sem.blob_relocation_mirror

use { BlobRef, BlobTruth, RelocateBlobProposal } from storage.blob

data BlobRelocationMirror {
    ref: BlobRef
    truth: BlobTruth
    proposal: RelocateBlobProposal
}
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	checked, ds := checkAllowingMissingImage(t, index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}

	assertTypeFields(t, moduleType(t, checked.Index, "storage.blob", "RelocateBlobProposal"), map[string]string{
		"blob_id":          "U64",
		"old_ref":          "BlobRef",
		"new_ref":          "BlobRef",
		"observed_version": "U64",
	})
	assertTypeFields(t, moduleType(t, checked.Index, "storage.blob", "BlobTruth"), map[string]string{
		"blob_id": "U64",
		"ref":     "BlobRef",
		"version": "U64",
	})

	source := readRepoFile(t, "wrela/storage/blob.wrela")
	accept := sourceBetween(t, source, "fn accept_relocate(self, proposal: RelocateBlobProposal) -> Bool {", "\n}\n\ndata Extent")
	for _, want := range []string{
		"if self.can_accept_relocate(proposal = proposal) == false",
		"return false",
		"self.ref = proposal.new_ref",
		"self.version = self.version + 1",
		"return true",
	} {
		if !strings.Contains(accept, want) {
			t.Fatalf("BlobTruth.accept_relocate source missing mutation shape %q", want)
		}
	}
}

func TestBlobWriterRelocationMirrorContract(t *testing.T) {
	modules := parseStorageWriterModules(t, `
module sem.blob_writer_relocation_mirror

use { StorageWriter, BlobRelocateResult } from storage.writer
use { BlobTruth, RelocateBlobProposal } from storage.blob

class BlobWriterRelocationMirror {
    fn run(self, writer: StorageWriter, truth: BlobTruth, proposal: RelocateBlobProposal) -> BlobRelocateResult {
        return writer.accept_relocate_blob(truth = truth, proposal = proposal)
    }
}
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	_, ds := checkAllowingMissingImage(t, index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}

	relocate := moduleType(t, index, "storage.writer", "BlobRelocateResult")
	assertTypeFields(t, relocate, map[string]string{
		"append": "StorageAppendResult",
		"truth":  "BlobTruth",
	})
	writer := moduleType(t, index, "storage.writer", "StorageWriter")
	assertMethodSignature(t, methodByName(t, writer, "accept_relocate_blob"), []string{"truth:BlobTruth", "proposal:RelocateBlobProposal"}, "BlobRelocateResult")
	source := readRepoFile(t, "wrela/storage/writer.wrela")
	accept := sourceBetween(t, source, "fn accept_relocate_blob(self, truth: BlobTruth, proposal: RelocateBlobProposal) -> BlobRelocateResult {", "\n    }\n}")
	for _, want := range []string{
		"if truth.accept_relocate(proposal = proposal) == false",
		"append = StorageAppendResult(accepted = false",
		"truth = truth",
		"append = StorageAppendResult(accepted = true",
	} {
		if !strings.Contains(accept, want) {
			t.Fatalf("StorageWriter.accept_relocate_blob missing %q", want)
		}
	}
}

func TestMaintenanceMutatesBlobTruthFails(t *testing.T) {
	modules := parseBlobModules(t, `
module sem.maintenance_mutates_blob_truth

use { BlobTruth, RelocateBlobProposal } from storage.blob

class MaintenanceWorker {
    fn run(self, truth: BlobTruth, proposal: RelocateBlobProposal) -> Bool {
        return truth.accept_relocate(proposal = proposal)
    }
}
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	_, ds := checkAllowingMissingImage(t, index, modules)
	if !hasCode(ds, diag.SEM0118) {
		t.Fatalf("diagnostics = %#v, want SEM0118", ds)
	}
}

func TestDirectBlobTruthMutationFailsIndependentOfClassName(t *testing.T) {
	modules := parseBlobModules(t, `
module sem.renamed_worker_mutates_blob_truth

use { BlobTruth, RelocateBlobProposal } from storage.blob

class Relocator {
    fn run(self, truth: BlobTruth, proposal: RelocateBlobProposal) -> Bool {
        return truth.accept_relocate(proposal = proposal)
    }
}
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	_, ds := checkAllowingMissingImage(t, index, modules)
	if !hasCode(ds, diag.SEM0118) {
		t.Fatalf("diagnostics = %#v, want SEM0118", ds)
	}
}

func parseBlobModules(t *testing.T, consumer string) []*ast.Module {
	t.Helper()
	paths := []string{
		repoPath(t, "wrela/lang/core.wrela"),
		repoPath(t, "wrela/machine/x86_64/executor_memory.wrela"),
		repoPath(t, "wrela/storage/blob.wrela"),
	}
	files := make([]*source.File, 0, len(paths)+1)
	for i, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		files = append(files, source.NewFile(source.FileID(i+1), path, string(raw)))
	}
	files = append(files, source.NewFile(source.FileID(len(files)+1), "blob-consumer.wrela", consumer))
	modules, ds := parse.ParseGraph(source.Graph{Files: files})
	if len(ds) != 0 {
		t.Fatalf("parse diagnostics: %#v", ds)
	}
	return modules
}
