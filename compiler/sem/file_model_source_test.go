package sem

import (
	"os"
	"strings"
	"testing"

	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/parse"
	"github.com/ryanwible/wrela3/compiler/source"
)

func TestFileModelSourceCompiles(t *testing.T) {
	modules := parseFileModelModules(t, `
module sem.file_model_consumer

use {
    DirectoryChild,
    DirectoryChildren,
    DirectoryProjectionWorker,
    FileContentCommitted,
    FileCreated,
    FileDeleted,
    FileId,
    FileNameKey,
    FileRenamed,
    FileState
} from storage.file_model

data FileModelConsumer {
    file_id: FileId
    name: FileNameKey
    child: DirectoryChild
    state: FileState
    worker: DirectoryProjectionWorker
}
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	checked, ds := checkAllowingMissingImage(t, index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}

	for eventName, eventID := range map[string]uint64{
		"FileCreated":          1001,
		"FileRenamed":          1002,
		"FileContentCommitted": 1003,
		"FileDeleted":          1004,
	} {
		if got := checked.Storage.EventsByKey["storage.file_model."+eventName].EventTypeID; got != eventID {
			t.Fatalf("%s event id = %d, want %d", eventName, got, eventID)
		}
	}
	if got := checked.Storage.ProjectionsByKey["storage.file_model.DirectoryChildren"].Layouts[0].Fields[0].ContainerKind; got != "OrderedPages" {
		t.Fatalf("DirectoryChildren container kind = %q, want OrderedPages", got)
	}
	assertTypeFields(t, moduleType(t, checked.Index, "storage.file_model", "FileState"), map[string]string{
		"current_blob_ref": "PublishedBlobRef",
		"name_ref":         "FileNameKey",
		"parent_id":        "FileId",
		"deleted":          "Bool",
		"stream_sequence":  "U64",
	})

	sourceText := readRepoFile(t, "wrela/storage/file_model.wrela")
	if !strings.Contains(sourceText, "blob_ref: PublishedBlobRef") {
		t.Fatal("FileContentCommitted must store PublishedBlobRef")
	}
}

func TestDirectoryProjectionWorkerMirrorContract(t *testing.T) {
	modules := parseFileModelModules(t, `
module sem.file_model_worker_consumer

use {
    DirectoryProjectionWorker,
    FILE_EVENT_CREATED,
    FILE_EVENT_RENAMED,
    FILE_EVENT_CONTENT_COMMITTED,
    FILE_EVENT_DELETED
} from storage.file_model

const CREATED: U64 = FILE_EVENT_CREATED
const RENAMED: U64 = FILE_EVENT_RENAMED
const CONTENT_COMMITTED: U64 = FILE_EVENT_CONTENT_COMMITTED
const DELETED: U64 = FILE_EVENT_DELETED

data FileModelWorkerConsumer {
    worker: DirectoryProjectionWorker
}
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	checked, ds := checkAllowingMissingImage(t, index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}

	assertMethodExists(t, moduleType(t, checked.Index, "storage.file_model", "DirectoryProjectionWorker"), "apply_group")
	for name, want := range map[string]uint64{
		"FILE_EVENT_CREATED":           1001,
		"FILE_EVENT_RENAMED":           1002,
		"FILE_EVENT_CONTENT_COMMITTED": 1003,
		"FILE_EVENT_DELETED":           1004,
	} {
		assertWrelaConstU64(t, checked.Index, "storage.file_model", name, want)
	}

	sourceText := readRepoFile(t, "wrela/storage/file_model.wrela")
	for _, want := range []string{
		"event_type_id == FILE_EVENT_CREATED",
		"event_type_id == FILE_EVENT_RENAMED",
		"event_type_id == FILE_EVENT_CONTENT_COMMITTED",
		"event_type_id == FILE_EVENT_DELETED",
		"return DirectoryProjectionWorker(projection_id = self.projection_id, watermark = last_event_id)",
		"return DirectoryProjectionWorker(projection_id = self.projection_id, watermark = self.watermark)",
	} {
		if !strings.Contains(sourceText, want) {
			t.Fatalf("DirectoryProjectionWorker.apply_group missing %q", want)
		}
	}
}

func parseFileModelModules(t *testing.T, consumer string) []*ast.Module {
	t.Helper()
	paths := []string{
		repoPath(t, "wrela/lang/core.wrela"),
		repoPath(t, "wrela/machine/x86_64/executor_memory.wrela"),
		repoPath(t, "wrela/storage/blob.wrela"),
		repoPath(t, "wrela/storage/projections.wrela"),
		repoPath(t, "wrela/storage/file_model.wrela"),
	}
	files := make([]*source.File, 0, len(paths)+1)
	for i, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		files = append(files, source.NewFile(source.FileID(i+1), path, string(raw)))
	}
	files = append(files, source.NewFile(source.FileID(len(files)+1), "file-model-consumer.wrela", consumer))
	modules, ds := parse.ParseGraph(source.Graph{Files: files})
	if len(ds) != 0 {
		t.Fatalf("parse diagnostics: %#v", ds)
	}
	return modules
}
