package sem

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
)

func TestDelegatedOnly(t *testing.T) {
	modules := parseModulesForTest(t, `
module index.delegated_only

data Bytes {
    address: U64
    length: U64
}

unique class OwnedHardware {}
unique class DelegatedHardware {
    fn exit_to_owned_hardware(self) -> DelegatedLeak {
        return DelegatedLeak(hardware = self)
    }
}

data DelegatedLeak {
    hardware: DelegatedHardware
}

image Bad {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> DelegatedLeak {
        return hardware.exit_to_owned_hardware()
    }

    phase owned_hardware(hardware: DelegatedLeak) -> never {
        while true {}
    }
}
`)
	index, ds := BuildIndex(modules)
	if len(ds) != 0 {
		t.Fatalf("index diagnostics: %#v", ds)
	}
	_, diags := Check(index, modules)
	if !hasCode(diags, diag.SEM0009) {
		t.Fatalf("expected SEM0009, got %#v", diags)
	}
}
