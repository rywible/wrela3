package sem

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
)

func TestUnique(t *testing.T) {
	modules := parseModulesForTest(t, `
module index.uniqueness

unique driver SerialDriver {}

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
        let a = SerialDriver()
        let b = SerialDriver()
        while true {}
    }
}
`)
	index, ds := BuildIndex(modules)
	if len(ds) != 0 {
		t.Fatalf("index diagnostics: %#v", ds)
	}
	_, diags := Check(index, modules)
	if !hasCode(diags, diag.SEM0007) {
		t.Fatalf("expected SEM0007, got %#v", diags)
	}
}
