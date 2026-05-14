package sem

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
)

func TestOwned(t *testing.T) {
	modules := parseModulesForTest(t, `
module index.owned

unique class OwnedHardware {}

unique class DelegatedHardware {
    fn exit_to_owned_hardware(self) -> OwnedHardware {
        return OwnedHardware()
    }
}

image Bad {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.exit_to_owned_hardware()
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        let h = OwnedHardware()
        while true {}
    }
}
`)
	index, ds := BuildIndex(modules)
	if len(ds) != 0 {
		t.Fatalf("index diagnostics: %#v", ds)
	}
	_, diags := Check(index, modules)
	if !hasCode(diags, diag.SEM0008) {
		t.Fatalf("expected SEM0008, got %#v", diags)
	}
}
