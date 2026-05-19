package sem

import (
	"os"
	"strings"
	"testing"

	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/parse"
	"github.com/ryanwible/wrela3/compiler/source"
	"github.com/ryanwible/wrela3/compiler/storagefmt"
)

func TestProjectionSourceCompiles(t *testing.T) {
	modules := parseProjectionModules(t, `
module sem.projection_consumer

use {
    AdvanceProjection,
    DenseEntityMap,
    OrderedPages,
    ProjectionCheckpoint,
    ProjectionTruth,
    StateCell
} from storage.projections

data FileId { value: U64 }
data FileNameKey { value: U64 }
data DirectoryChild { value: U64 }

data ProjectionConsumer {
    state: StateCell<DirectoryChild>
    by_id: DenseEntityMap<FileId, DirectoryChild>
    children: OrderedPages<FileId, FileNameKey, DirectoryChild>
    checkpoint: ProjectionCheckpoint
    advance: AdvanceProjection
    truth: ProjectionTruth
}
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	_, ds := checkAllowingMissingImage(t, index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}
}

func TestProjectionSourceMirrorContract(t *testing.T) {
	index := checkedProjectionSourceIndex(t)

	_ = moduleType(t, index, "storage.projections", "StateCell")
	_ = moduleType(t, index, "storage.projections", "DenseEntityMap")
	_ = moduleType(t, index, "storage.projections", "OrderedPages")
	assertTypeFields(t, moduleType(t, index, "storage.projections", "AdvanceProjection"), map[string]string{
		"projection_id":    "U64",
		"through_event_id": "U64",
	})
	assertTypeFields(t, moduleType(t, index, "storage.projections", "ProjectionCheckpoint"), map[string]string{
		"projection_id":            "U64",
		"layout_id":                "U64",
		"layout_hash":              "U64",
		"worker_code_hash":         "U64",
		"last_append_log_event_id": "U64",
		"root_ref_count":           "U64",
	})
	assertMethodExists(t, moduleType(t, index, "storage.projections", "ProjectionTruth"), "accept_advance")

	state := storagefmt.ProjectionTruth{AtomicGroupFrontier: 10}
	if state.AcceptAdvance(storagefmt.AdvanceProjection{ProjectionID: 12, ThroughEventID: 11}) {
		t.Fatal("host ProjectionTruth accepted advance past frontier")
	}
	if !state.AcceptAdvance(storagefmt.AdvanceProjection{ProjectionID: 12, ThroughEventID: 10}) {
		t.Fatal("host ProjectionTruth rejected advance through frontier")
	}

	source := readRepoFile(t, "wrela/storage/projection.wrela")
	if !strings.Contains(source, "if advance.through_event_id > self.atomic_group_frontier") {
		t.Fatal("ProjectionTruth.accept_advance must reject through_event_id past atomic_group_frontier")
	}
}

func TestProjectionInvalidWatermarkFails(t *testing.T) {
	modules := parseProjectionModules(t, `
module sem.projection_bad_watermark

use { AdvanceProjection, ProjectionTruth } from storage.projections

executor ProjectionWatermarkExecutor {
    start fn main(self) -> never {
        let truth = ProjectionTruth(atomic_group_frontier = 10)
        let advance = AdvanceProjection(projection_id = 12, through_event_id = 11, checkpoint_root_ref = 0)
        let accepted = truth.accept_advance(advance = advance)
        while true {}
    }
}
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	_, ds := checkAllowingMissingImage(t, index, modules)
	if !hasCode(ds, diag.SEM0119) {
		t.Fatalf("diagnostics = %#v, want SEM0119", ds)
	}
}

func checkedProjectionSourceIndex(t *testing.T) *Index {
	t.Helper()
	modules := parseProjectionModules(t, `
module sem.projection_mirror

use { AdvanceProjection, ProjectionTruth } from storage.projections

data ProjectionMirror {
    advance: AdvanceProjection
    truth: ProjectionTruth
}
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	checked, ds := checkAllowingMissingImage(t, index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}
	return checked.Index
}

func parseProjectionModules(t *testing.T, consumer string) []*ast.Module {
	t.Helper()
	paths := []string{
		repoPath(t, "wrela/lang/core.wrela"),
		repoPath(t, "wrela/storage/projection.wrela"),
	}
	files := make([]*source.File, 0, len(paths)+1)
	for i, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		files = append(files, source.NewFile(source.FileID(i+1), path, string(raw)))
	}
	files = append(files, source.NewFile(source.FileID(len(files)+1), "projection-consumer.wrela", consumer))
	modules, ds := parse.ParseGraph(source.Graph{Files: files})
	if len(ds) != 0 {
		t.Fatalf("parse diagnostics: %#v", ds)
	}
	return modules
}
