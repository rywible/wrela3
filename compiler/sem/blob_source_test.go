package sem

import (
	"os"
	"testing"

	"github.com/ryanwible/wrela3/compiler/ast"
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
    Extent,
    BlobManifest,
    BlobExtentAllocator
} from storage.blob

const MIRRORED_FREE_EXTENT_LIMIT: U64 = BLOB_ALLOCATOR_FREE_EXTENT_LIMIT

data BlobMirror {
    ref: BlobRef
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
		"blob_id": "U64",
	})
	assertTypeFields(t, moduleType(t, checked.Index, "storage.blob", "Extent"), map[string]string{
		"start_lba":   "U64",
		"block_count": "U64",
	})
	assertTypeFields(t, moduleType(t, checked.Index, "storage.blob", "BlobManifest"), map[string]string{
		"blob_id":          "U64",
		"key_metadata_ref": "U64",
		"extent_count":     "U64",
		"logical_bytes":    "U64",
	})
	allocator := moduleType(t, checked.Index, "storage.blob", "BlobExtentAllocator")
	assertMethodExists(t, allocator, "allocate")
	assertMethodExists(t, allocator, "free")
	assertMethodExists(t, allocator, "extents")
}

func parseBlobModules(t *testing.T, consumer string) []*ast.Module {
	t.Helper()
	paths := []string{
		repoPath(t, "wrela/lang/core.wrela"),
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
