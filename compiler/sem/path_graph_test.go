package sem

import (
	"strings"
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
)

func TestPath(t *testing.T) {
	t.Run("root driver passed to executor", func(t *testing.T) {
		src := `
module index.path_root_driver

unique class OwnedHardware {}
unique class DelegatedHardware {
    fn exit_to_owned_hardware(self) -> OwnedHardware {
        return OwnedHardware()
    }
}

unique driver SerialDriver {}

executor HelloWorld {
    serial: SerialDriver

    start fn run(self) -> never {
        while true {}
    }
}

image Bad {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.exit_to_owned_hardware()
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        let serial = SerialDriver()
        let hello = HelloWorld(serial = serial)
        hello.run()
    }
}
`
		modules := parseModulesForTest(t, src)
		index, ds := BuildIndex(modules)
		if len(ds) != 0 {
			t.Fatalf("index diagnostics: %#v", ds)
		}
		_, diags := Check(index, modules)
		if !hasCode(diags, diag.SEM0010) {
			t.Fatalf("expected SEM0010, got %#v", diags)
		}
		wantStart := strings.Index(src, "serial = serial")
		for _, d := range diags {
			if d.Code == diag.SEM0010 && d.Start != wantStart {
				t.Fatalf("SEM0010 start = %d, want field binding start %d: %#v", d.Start, wantStart, d)
			}
		}
	})

	t.Run("driver path assigned twice", func(t *testing.T) {
		src := `
module index.path_twice

data ExecutorPlacement {
    id: U64
}

data Bytes {
    address: U64
    length: U64
}

driver path SerialWritePath {
    owner: ExecutorPlacement
}

executor A {
    serial_path: SerialWritePath

    start fn run(self) -> never {
        while true {}
    }
}

executor B {
    serial_path: SerialWritePath

    start fn run(self) -> never {
        while true {}
    }
}

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
        let serial_path = SerialWritePath(owner = ExecutorPlacement(id = 0))
        let a = A(serial_path = serial_path)
        let b = B(serial_path = serial_path)
        a.run()
        b.run()
    }
}
`
		modules := parseModulesForTest(t, src)
		index, ds := BuildIndex(modules)
		if len(ds) != 0 {
			t.Fatalf("index diagnostics: %#v", ds)
		}
		_, diags := Check(index, modules)
		if !hasCode(diags, diag.SEM0011) {
			t.Fatalf("expected SEM0011, got %#v", diags)
		}
		wantStart := strings.LastIndex(src, "serial_path = serial_path")
		for _, d := range diags {
			if d.Code == diag.SEM0011 && d.Start != wantStart {
				t.Fatalf("SEM0011 start = %d, want duplicate field binding start %d: %#v", d.Start, wantStart, d)
			}
		}
	})

	t.Run("driver path constructed without executor owner", func(t *testing.T) {
		src := `
module index.path_unowned

data ExecutorPlacement {
    id: U64
}

driver path SerialWritePath {
    owner: ExecutorPlacement
}

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
        let serial_path = SerialWritePath(owner = ExecutorPlacement(id = 0))
        while true {}
    }
}
`
		modules := parseModulesForTest(t, src)
		index, ds := BuildIndex(modules)
		if len(ds) != 0 {
			t.Fatalf("index diagnostics: %#v", ds)
		}
		_, diags := Check(index, modules)
		if !hasCode(diags, diag.SEM0011) {
			t.Fatalf("expected SEM0011 for unowned driver path, got %#v", diags)
		}
	})

	t.Run("direct driver path constructor is owned by executor field", func(t *testing.T) {
		src := `
module index.path_direct_constructor

data ExecutorPlacement {
    id: U64
}

driver path SerialWritePath {
    owner: ExecutorPlacement
}

executor A {
    serial_path: SerialWritePath

    start fn run(self) -> never {
        while true {}
    }
}

unique class OwnedHardware {}
unique class DelegatedHardware {
    fn exit_to_owned_hardware(self) -> OwnedHardware {
        return OwnedHardware()
    }
}

image Good {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.exit_to_owned_hardware()
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        let a = A(serial_path = SerialWritePath(owner = ExecutorPlacement(id = 0)))
        a.run()
    }
}
`
		modules := parseModulesForTest(t, src)
		index, ds := BuildIndex(modules)
		if len(ds) != 0 {
			t.Fatalf("index diagnostics: %#v", ds)
		}
		_, diags := Check(index, modules)
		if hasCode(diags, diag.SEM0011) {
			t.Fatalf("unexpected SEM0011 for direct owned driver path: %#v", diags)
		}
	})

	t.Run("driver path alias resolves to constructed instance", func(t *testing.T) {
		src := `
module index.path_alias

data ExecutorPlacement {
    id: U64
}

driver path SerialWritePath {
    owner: ExecutorPlacement
}

executor A {
    serial_path: SerialWritePath

    start fn run(self) -> never {
        while true {}
    }
}

unique class OwnedHardware {}
unique class DelegatedHardware {
    fn exit_to_owned_hardware(self) -> OwnedHardware {
        return OwnedHardware()
    }
}

image Good {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.exit_to_owned_hardware()
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        let original = SerialWritePath(owner = ExecutorPlacement(id = 0))
        let serial_path = original
        let a = A(serial_path = serial_path)
        a.run()
    }
}
`
		modules := parseModulesForTest(t, src)
		index, ds := BuildIndex(modules)
		if len(ds) != 0 {
			t.Fatalf("index diagnostics: %#v", ds)
		}
		_, diags := Check(index, modules)
		if hasCode(diags, diag.SEM0011) {
			t.Fatalf("unexpected SEM0011 for driver path alias: %#v", diags)
		}
	})

	t.Run("nested driver path is owned through executor-owned path", func(t *testing.T) {
		src := `
module index.path_nested_owned

data ExecutorPlacement {
    id: U64
}

driver path Registers {
    port: U16
}

driver path SerialWritePath {
    owner: ExecutorPlacement
    registers: Registers
}

unique driver SerialDriver {
    registers: Registers

    fn initialize(self) -> SerialDriver {
        return self
    }
}

executor A {
    serial_path: SerialWritePath

    start fn run(self) -> never {
        while true {}
    }
}

unique class OwnedHardware {}
unique class DelegatedHardware {
    fn exit_to_owned_hardware(self) -> OwnedHardware {
        return OwnedHardware()
    }
}

image Good {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.exit_to_owned_hardware()
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        let registers = Registers(port = 0x3F8)
        let serial_driver = SerialDriver(registers = registers).initialize()
        let serial_path = SerialWritePath(owner = ExecutorPlacement(id = 0), registers = serial_driver.registers)
        let a = A(serial_path = serial_path)
        a.run()
    }
}
`
		modules := parseModulesForTest(t, src)
		index, ds := BuildIndex(modules)
		if len(ds) != 0 {
			t.Fatalf("index diagnostics: %#v", ds)
		}
		_, diags := Check(index, modules)
		if hasCode(diags, diag.SEM0011) {
			t.Fatalf("unexpected SEM0011 for nested driver path ownership: %#v", diags)
		}
	})

	t.Run("same driver path binding name in sibling scopes stays distinct", func(t *testing.T) {
		src := `
module index.path_same_name_scopes

data ExecutorPlacement {
    id: U64
}

driver path SerialWritePath {
    owner: ExecutorPlacement
}

executor A {
    serial_path: SerialWritePath

    start fn run(self) -> never {
        while true {}
    }
}

executor B {
    serial_path: SerialWritePath

    start fn run(self) -> never {
        while true {}
    }
}

unique class OwnedHardware {}
unique class DelegatedHardware {
    fn exit_to_owned_hardware(self) -> OwnedHardware {
        return OwnedHardware()
    }
}

image Good {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.exit_to_owned_hardware()
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        if true {
            let serial_path = SerialWritePath(owner = ExecutorPlacement(id = 0))
            let a = A(serial_path = serial_path)
            a.run()
        }
        if true {
            let serial_path = SerialWritePath(owner = ExecutorPlacement(id = 1))
            let b = B(serial_path = serial_path)
            b.run()
        }
        while true {}
    }
}
`
		modules := parseModulesForTest(t, src)
		index, ds := BuildIndex(modules)
		if len(ds) != 0 {
			t.Fatalf("index diagnostics: %#v", ds)
		}
		_, diags := Check(index, modules)
		if hasCode(diags, diag.SEM0011) {
			t.Fatalf("unexpected SEM0011 for same binding names in sibling scopes: %#v", diags)
		}
	})
}
