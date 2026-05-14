package sem

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
)

const phasePrelude = `
unique class OwnedHardware {}
unique class DelegatedHardware {
    fn exit_to_owned_hardware(self) -> OwnedHardware {
        return OwnedHardware()
    }
}`

func TestPhase(t *testing.T) {
	t.Run("missing delegated transition", func(t *testing.T) {
		modules := parseModulesForTest(t, `
module index.phase_missing_transition
`+phasePrelude+`

image Bad {
    transitions {}

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.exit_to_owned_hardware()
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        while true {}
    }
}
`)
		index, _ := BuildIndex(modules)
		if _, d := Check(index, modules); len(d) == 0 {
			t.Fatalf("expected diagnostics")
		} else {
			var got bool
			for _, entry := range d {
				if entry.Code == diag.SEM0005 {
					got = true
				}
			}
			if !got {
				t.Fatalf("diagnostics = %#v", d)
			}
		}
	})

	t.Run("wrong delegated param", func(t *testing.T) {
		modules := parseModulesForTest(t, `
module index.phase_wrong_delegated_param
`+phasePrelude+`

image Bad {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: U8) -> OwnedHardware {
        return hardware.exit_to_owned_hardware()
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        while true {}
    }
}
`)
		index, ds := BuildIndex(modules)
		if len(ds) != 0 {
			t.Fatalf("build index diagnostics: %#v", ds)
		}
		_, diags := Check(index, modules)
		if !hasCode(diags, diag.SEM0005) {
			t.Fatalf("expected SEM0005, got %#v", diags)
		}
	})

	t.Run("wrong owned param", func(t *testing.T) {
		modules := parseModulesForTest(t, `
module index.phase_wrong_owned_param
`+phasePrelude+`

image Bad {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.exit_to_owned_hardware()
    }

    phase owned_hardware(hardware: U8) -> never {
        while true {}
    }
}
`)
		index, ds := BuildIndex(modules)
		if len(ds) != 0 {
			t.Fatalf("build index diagnostics: %#v", ds)
		}
		_, diags := Check(index, modules)
		if !hasCode(diags, diag.SEM0005) {
			t.Fatalf("expected SEM0005, got %#v", diags)
		}
	})

	t.Run("owned phase must return never", func(t *testing.T) {
		modules := parseModulesForTest(t, `
module index.phase_wrong_owned_return
`+phasePrelude+`

image Bad {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.exit_to_owned_hardware()
    }

    phase owned_hardware(hardware: OwnedHardware) -> OwnedHardware {
        return hardware
    }
}
`)
		index, ds := BuildIndex(modules)
		if len(ds) != 0 {
			t.Fatalf("build index diagnostics: %#v", ds)
		}
		_, diags := Check(index, modules)
		if !hasCode(diags, diag.SEM0005) {
			t.Fatalf("expected SEM0005, got %#v", diags)
		}
	})

	t.Run("valid phases derive owned root", func(t *testing.T) {
		modules := parseModulesForTest(t, `
module index.phase_valid
`+phasePrelude+`

image Good {
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
			t.Fatalf("build index diagnostics: %#v", ds)
		}
		checked, diags := Check(index, modules)
		if len(diags) != 0 {
			t.Fatalf("expected no diagnostics, got %#v", diags)
		}
		if checked.OwnedRoot == nil || checked.OwnedRoot.Name != "OwnedHardware" {
			t.Fatalf("owned root = %#v", checked.OwnedRoot)
		}
		if len(index.Images) != 1 {
			t.Fatalf("images = %d, want 1", len(index.Images))
		}
	})
}
