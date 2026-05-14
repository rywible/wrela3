package sem

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
)

const typePrelude = `
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

func TestType(t *testing.T) {
	t.Run("while condition must be bool", func(t *testing.T) {
		modules := parseModulesForTest(t, `
module index.types_while
`+typePrelude+`

image Bad {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.exit_to_owned_hardware()
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        while 1 {}
    }
}
`)
		index, ds := BuildIndex(modules)
		if len(ds) != 0 {
			t.Fatalf("build index diagnostics: %#v", ds)
		}
		_, diags := Check(index, modules)
		if len(diags) == 0 {
			t.Fatalf("expected error from bool condition")
		}
	})

	t.Run("constructor field completeness", func(t *testing.T) {
		modules := parseModulesForTest(t, `
module index.types_constructor_fields
`+typePrelude+`

data Pair {
    a: U64
    b: U64
}

image Bad {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.exit_to_owned_hardware()
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        let p = Pair(a: 1)
        while true {}
    }
}
`)
		index, ds := BuildIndex(modules)
		if len(ds) != 0 {
			t.Fatalf("index diagnostics: %#v", ds)
		}
		_, diags := Check(index, modules)
		if !hasCode(diags, diag.CG0001) {
			t.Fatalf("expected constructor error, got %#v", diags)
		}
	})

	t.Run("method return type mismatch", func(t *testing.T) {
		modules := parseModulesForTest(t, `
module index.types_return
`+typePrelude+`

class Bad {
    fn ok(self) -> U8 {
        return true
    }
}

image BadImage {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.exit_to_owned_hardware()
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        let bad = Bad()
        let _ = bad.ok()
        while true {}
    }
}
`)
		index, ds := BuildIndex(modules)
		if len(ds) != 0 {
			t.Fatalf("index diagnostics: %#v", ds)
		}
		_, diags := Check(index, modules)
		if !hasCode(diags, diag.CG0001) {
			t.Fatalf("expected return type error, got %#v", diags)
		}
	})

	t.Run("for byte in bytes is valid", func(t *testing.T) {
		modules := parseModulesForTest(t, `
module index.types_for
`+typePrelude+`

image Bad {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.exit_to_owned_hardware()
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        for byte in Bytes(address: 0, length: 1) {
            let _ = byte
        }
    }
}
`)
		index, ds := BuildIndex(modules)
		if len(ds) != 0 {
			t.Fatalf("index diagnostics: %#v", ds)
		}
		_, diags := Check(index, modules)
		for _, d := range diags {
			if d.Code == diag.CG0001 {
				t.Fatalf("unexpected CG0001 in valid for-byte loop: %#v", diags)
			}
		}
	})

	t.Run("method with too many params", func(t *testing.T) {
		modules := parseModulesForTest(t, `
module index.types_too_many_params
`+typePrelude+`

class TooMany {
    fn bad(self, a: U8, b: U8, c: U8, d: U8, e: U8, f: U8) {}
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
		if !hasCode(diags, diag.SEM0013) {
			t.Fatalf("expected SEM0013, got %#v", diags)
		}
	})

	t.Run("integer literals and identity mapped addresses are assignable in v0", func(t *testing.T) {
		modules := parseModulesForTest(t, `
module index.types_numeric
`+typePrelude+`

data Narrow {
    a: U8
    b: U16
    c: PhysicalAddress
    d: VirtualAddress
}

image Good {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.exit_to_owned_hardware()
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        let base = Narrow(a: 1, b: 2, c: 0x200000, d: 0x200000)
        let next = base.d + 8
        let alias = Narrow(a: 1, b: 2, c: next, d: base.c)
        while true {}
    }
}
`)
		index, ds := BuildIndex(modules)
		if len(ds) != 0 {
			t.Fatalf("index diagnostics: %#v", ds)
		}
		_, diags := Check(index, modules)
		for _, d := range diags {
			if d.Code == diag.CG0001 {
				t.Fatalf("unexpected CG0001 in v0 numeric/address compatibility: %#v", diags)
			}
		}
	})
}
