package sem

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
)

func TestStorageIndexRecordsEventsAndProjections(t *testing.T) {
	modules := parseModulesForTest(t, `
module storage.good
data FileId { value: U64 }
data FileNameKey { value: U64 }
data DirectoryChild { value: U64 }
data OrderedPages<Partition, SortKey, Row> { root: U64 }
event FileCreated id 1001 {
    file_id: U64
    layout 1 current {
        file_id: U64 = self.file_id
    }
}
projection DirectoryChildren id 12 {
    layout 1 current {
        children: OrderedPages<FileId, FileNameKey, DirectoryChild>
    }
}`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	checked, ds := checkAllowingMissingImage(t, index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}

	event := checked.Storage.EventsByTypeID[1001]
	if event.Module != "storage.good" || event.Name != "FileCreated" || event.EventTypeID != 1001 {
		t.Fatalf("event metadata = %#v", event)
	}
	if got := checked.Storage.EventsByKey["storage.good.FileCreated"].EventTypeID; got != 1001 {
		t.Fatalf("event by key id = %d, want 1001", got)
	}
	projection := checked.Storage.ProjectionsByID[12]
	if projection.Module != "storage.good" || projection.Name != "DirectoryChildren" || projection.ProjectionID != 12 {
		t.Fatalf("projection metadata = %#v", projection)
	}
	if got := checked.Storage.ProjectionsByKey["storage.good.DirectoryChildren"].ProjectionID; got != 12 {
		t.Fatalf("projection by key id = %d, want 12", got)
	}
}

func TestStorageRejectsDuplicateEventTypeID(t *testing.T) {
	ds := storageDiagsForSource(t, `
module storage.duplicate_event
event A id 7 {
    layout 1 current {}
}
event B id 7 {
    layout 1 current {}
}`)
	if !hasCode(ds, diag.SEM0099) {
		t.Fatalf("diagnostics = %#v, want SEM0099", ds)
	}
}

func TestStorageRejectsDuplicateProjectionID(t *testing.T) {
	ds := storageDiagsForSource(t, `
module storage.duplicate_projection
projection A id 4 {
    layout 1 current {}
}
projection B id 4 {
    layout 1 current {}
}`)
	if !hasCode(ds, diag.SEM0106) {
		t.Fatalf("diagnostics = %#v, want SEM0106", ds)
	}
}

func TestStorageRejectsZeroEventTypeID(t *testing.T) {
	ds := storageDiagsForSource(t, `
module storage.zero_event
event A id 0 {
    layout 1 current {}
}`)
	if !hasCode(ds, diag.SEM0100) {
		t.Fatalf("diagnostics = %#v, want SEM0100", ds)
	}
}

func TestStorageRejectsZeroProjectionID(t *testing.T) {
	ds := storageDiagsForSource(t, `
module storage.zero_projection
projection A id 0 {
    layout 1 current {}
}`)
	if !hasMessage(ds, diag.SEM0106, "invalid projection id 0") {
		t.Fatalf("diagnostics = %#v, want SEM0106 invalid projection id 0", ds)
	}
}

func storageDiagsForSource(t *testing.T, sourceText string) []diag.Diagnostic {
	t.Helper()
	modules := parseModulesForTest(t, sourceText)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	_, ds := checkAllowingMissingImage(t, index, modules)
	return ds
}
