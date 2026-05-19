package sem

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
)

func TestEventLayoutCurrentRules(t *testing.T) {
	ds := storageDiagsForSource(t, `
module storage.bad
event A id 1 {
    layout 1 {}
    layout 2 {}
}`)
	if !hasCode(ds, diag.SEM0101) {
		t.Fatalf("diagnostics = %#v, want SEM0101", ds)
	}
}

func TestEventLayoutDuplicateAndZeroIDs(t *testing.T) {
	t.Run("duplicate", func(t *testing.T) {
		ds := storageDiagsForSource(t, `
module storage.bad_duplicate_layout
event A id 1 {
    layout 1 {}
    layout 1 current {}
}`)
		if !hasCode(ds, diag.SEM0102) {
			t.Fatalf("diagnostics = %#v, want SEM0102", ds)
		}
	})

	t.Run("zero", func(t *testing.T) {
		ds := storageDiagsForSource(t, `
module storage.bad_zero_layout
event A id 1 {
    layout 0 current {}
}`)
		if !hasMessage(ds, diag.SEM0102, "layout id 0 is reserved") {
			t.Fatalf("diagnostics = %#v, want SEM0102 layout id 0 is reserved", ds)
		}
	})
}

func TestEventLayoutUpcastRules(t *testing.T) {
	t.Run("endpoint", func(t *testing.T) {
		ds := storageDiagsForSource(t, `
module storage.bad_upcast_endpoint
event A id 1 {
    layout 1 {
        old_name_ref: U64
    }
    layout 2 current {
        name_ref: U64
    }
    upcast 1 -> 3 {
        old_name_ref -> name_ref
    }
}`)
		if !hasCode(ds, diag.SEM0104) {
			t.Fatalf("diagnostics = %#v, want SEM0104", ds)
		}
	})

	t.Run("target_field", func(t *testing.T) {
		ds := storageDiagsForSource(t, `
module storage.bad_upcast_field
event A id 1 {
    layout 1 {
        old_name_ref: U64
    }
    layout 2 current {
        name_ref: U64
    }
    upcast 1 -> 2 {
        old_name_ref -> missing_ref
    }
}`)
		if !hasCode(ds, diag.SEM0105) {
			t.Fatalf("diagnostics = %#v, want SEM0105", ds)
		}
	})
}

func TestEventLayoutPayloadBudget(t *testing.T) {
	ds := storageDiagsForSource(t, `
module storage.too_large_payload
data Huge {
    a0: U64
    a1: U64
    a2: U64
    a3: U64
    a4: U64
    a5: U64
    a6: U64
    a7: U64
}
event A id 1 {
    layout 1 current {
        f0: Huge
        f1: Huge
        f2: Huge
        f3: Huge
        f4: Huge
        f5: Huge
        f6: Huge
        f7: Huge
    }
}`)
	if !hasCode(ds, diag.SEM0121) {
		t.Fatalf("diagnostics = %#v, want SEM0121", ds)
	}
}

func TestEventLayoutPayloadFieldMetadata(t *testing.T) {
	modules := parseModulesForTest(t, `
module storage.event_payload_metadata
event FileCreated id 1001 {
    file_id: U64
    layout 1 current {
        file_id: U64 = self.file_id
    }
}`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	checked, ds := checkAllowingMissingImage(t, index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}

	layout := checked.Storage.EventsByTypeID[1001].Layouts[0]
	if len(layout.Fields) != 1 {
		t.Fatalf("payload fields = %#v, want one field", layout.Fields)
	}
	field := layout.Fields[0]
	if field.Name != "file_id" || field.PayloadOffset != 0 || field.Type.Name != "U64" || field.StorageSize != 8 || field.Align != 8 {
		t.Fatalf("payload field metadata = %#v", field)
	}
}
