package sem

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
)

func TestProjectionContainerThreeGenericInstantiation(t *testing.T) {
	modules := parseModulesForTest(t, `
module storage.three_generic
data Triple<A, B, C> { a: A; b: B; c: C }
data FileId { value: U64 }
data FileNameKey { value: U64 }
data DirectoryChild { value: U64 }
data Holder { rows: Triple<FileId, FileNameKey, DirectoryChild> }
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	_, ds := checkAllowingMissingImage(t, index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}
	mustCompleteGenericInstantiations(t, index)
	if _, ok := index.Instantiations["storage.three_generic.Triple[storage.three_generic.FileId,storage.three_generic.FileNameKey,storage.three_generic.DirectoryChild]"]; !ok {
		t.Fatalf("three-parameter generic instantiation missing from index")
	}
}

func TestProjectionRejectsUnsupportedContainer(t *testing.T) {
	ds := storageDiagsForSource(t, `
module storage.bad_projection
data HashMap<K, V> { root: U64 }
data FileId { value: U64 }
data Row { value: U64 }
projection Bad id 3 {
    layout 1 current {
        bad: HashMap<FileId, Row>
    }
}`)
	if !hasCode(ds, diag.SEM0108) {
		t.Fatalf("diagnostics = %#v, want SEM0108", ds)
	}
}

func TestProjectionLayoutCurrentAndDuplicateRules(t *testing.T) {
	t.Run("missing_current", func(t *testing.T) {
		ds := storageDiagsForSource(t, `
module storage.bad_projection_current
data StateCell<T> { value: T }
data Row { value: U64 }
projection Bad id 4 {
    layout 1 {
        value: StateCell<Row>
    }
    layout 2 {
        value: StateCell<Row>
    }
}`)
		if !hasCode(ds, diag.SEM0107) {
			t.Fatalf("diagnostics = %#v, want SEM0107", ds)
		}
	})

	t.Run("duplicate", func(t *testing.T) {
		ds := storageDiagsForSource(t, `
module storage.bad_projection_duplicate
data StateCell<T> { value: T }
data Row { value: U64 }
projection Bad id 5 {
    layout 1 {
        value: StateCell<Row>
    }
    layout 1 current {
        value: StateCell<Row>
    }
}`)
		if !hasCode(ds, diag.SEM0107) {
			t.Fatalf("diagnostics = %#v, want SEM0107", ds)
		}
	})
}

func TestProjectionLayoutSupportedContainers(t *testing.T) {
	modules := parseModulesForTest(t, `
module storage.good_projection
data StateCell<T> { value: T }
data DenseEntityMap<Id, T> { root: U64 }
data OrderedPages<Partition, SortKey, Row> { root: U64 }
data FileId { value: U64 }
data FileNameKey { value: U64 }
data DirectoryChild { value: U64 }
projection DirectoryChildren id 12 {
    layout 1 current {
        state: StateCell<DirectoryChild>
        by_id: DenseEntityMap<FileId, DirectoryChild>
        children: OrderedPages<FileId, FileNameKey, DirectoryChild>
    }
}`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	checked, ds := checkAllowingMissingImage(t, index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}
	projection := checked.Storage.ProjectionsByID[12]
	if projection.CurrentLayoutID != 1 || len(projection.Layouts) != 1 {
		t.Fatalf("projection metadata = %#v", projection)
	}
	if got := projection.Layouts[0].Fields[2].ContainerKind; got != "OrderedPages" {
		t.Fatalf("container kind = %q, want OrderedPages", got)
	}
}

func TestProjectionLayoutUpcastEndpoint(t *testing.T) {
	ds := storageDiagsForSource(t, `
module storage.bad_projection_upcast
data StateCell<T> { value: T }
data Row { value: U64 }
projection Bad id 6 {
    layout 1 {
        value: StateCell<Row>
    }
    layout 2 current {
        value: StateCell<Row>
    }
    upcast 1 -> 3 {
        value -> value
    }
}`)
	if !hasCode(ds, diag.SEM0109) {
		t.Fatalf("diagnostics = %#v, want SEM0109", ds)
	}
}
