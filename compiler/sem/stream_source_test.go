package sem

import (
	"os"
	"testing"

	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/parse"
	"github.com/ryanwible/wrela3/compiler/source"
)

func TestStreamSourceCompiles(t *testing.T) {
	modules := parseStreamModules(t, `
module sem.stream_consumer

use {
    StreamDirectory,
    StreamDirectoryEntry,
    StreamCheckpoint,
    StreamDirectoryCache
} from storage.stream

data StreamConsumer {
    directory: StreamDirectory
    entry: StreamDirectoryEntry
    checkpoint: StreamCheckpoint
    cache: StreamDirectoryCache
}
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	_, ds := checkAllowingMissingImage(t, index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}
}

func TestStreamSourceMirrorContract(t *testing.T) {
	index := checkedStreamSourceIndex(t)
	assertMethodExists(t, moduleType(t, index, "storage.stream", "StreamDirectory"), "entry_byte_offset")
	assertMethodExists(t, moduleType(t, index, "storage.stream", "StreamDirectory"), "exists")
	assertMethodExists(t, moduleType(t, index, "storage.stream", "StreamDirectoryCache"), "record_hit")
	assertMethodExists(t, moduleType(t, index, "storage.stream", "StreamDirectoryCache"), "hit_rate_x1000")
	_ = moduleType(t, index, "storage.stream", "StreamDirectoryEntry")
	_ = moduleType(t, index, "storage.stream", "StreamCheckpoint")
}

func checkedStreamSourceIndex(t *testing.T) *Index {
	t.Helper()
	modules := parseStreamModules(t, `
module sem.stream_mirror

use {
    StreamDirectory,
    StreamDirectoryEntry,
    StreamCheckpoint,
    StreamDirectoryCache
} from storage.stream

data StreamMirror {
    directory: StreamDirectory
    entry: StreamDirectoryEntry
    checkpoint: StreamCheckpoint
    cache: StreamDirectoryCache
}
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	checked, ds := checkAllowingMissingImage(t, index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}
	return checked.Index
}

func parseStreamModules(t *testing.T, consumer string) []*ast.Module {
	t.Helper()
	paths := []string{
		repoPath(t, "wrela/lang/core.wrela"),
		repoPath(t, "wrela/storage/format.wrela"),
		repoPath(t, "wrela/storage/stream.wrela"),
	}
	files := make([]*source.File, 0, len(paths)+1)
	for i, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		files = append(files, source.NewFile(source.FileID(i+1), path, string(raw)))
	}
	files = append(files, source.NewFile(source.FileID(len(files)+1), "stream-consumer.wrela", consumer))
	modules, ds := parse.ParseGraph(source.Graph{Files: files})
	if len(ds) != 0 {
		t.Fatalf("parse diagnostics: %#v", ds)
	}
	return modules
}
