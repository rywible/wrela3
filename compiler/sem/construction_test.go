package sem

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
)

const constructionPrelude = `
unique class OwnedHardware {}
unique class DelegatedHardware {
    fn exit_to_owned_hardware(self) -> OwnedHardware {
        return OwnedHardware()
    }
}

data Bytes {
    address: U64
    length: U64
}`

func TestConstruction(t *testing.T) {
	t.Run("constructor outside image is rejected", func(t *testing.T) {
		modules := parseModulesForTest(t, `
module index.construction_outside
`+constructionPrelude+`

class Other {}

class Maker {
    fn make(self) -> U8 {
        let o = Other()
        return 0
    }
}

image Bad {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.exit_to_owned_hardware()
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        while true {}
    }
}
`)
		index, ds := BuildIndex(modules)
		if len(ds) != 0 {
			t.Fatalf("index diagnostics: %#v", ds)
		}
		_, diags := Check(index, modules)
		if !hasCode(diags, diag.SEM0006) {
			t.Fatalf("expected SEM0006, got %#v", diags)
		}
	})

	t.Run("data constructor allowed in image phase", func(t *testing.T) {
		modules := parseModulesForTest(t, `
module index.construction_data
`+constructionPrelude+`

image Bad {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.exit_to_owned_hardware()
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        let b = Bytes(address = 0, length = 0)
        while true {}
    }
}
`)
		index, ds := BuildIndex(modules)
		if len(ds) != 0 {
			t.Fatalf("index diagnostics: %#v", ds)
		}
		_, diags := Check(index, modules)
		if hasCode(diags, diag.SEM0006) {
			t.Fatalf("unexpected constructor restriction diagnostics: %#v", diags)
		}
	})
}
