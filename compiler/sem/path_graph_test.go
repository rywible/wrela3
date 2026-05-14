package sem

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
)

func TestPath(t *testing.T) {
	t.Run("root driver passed to executor", func(t *testing.T) {
		modules := parseModulesForTest(t, `
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
        let hello = HelloWorld(serial: serial)
        hello.run()
    }
}
`)
		index, ds := BuildIndex(modules)
		if len(ds) != 0 {
			t.Fatalf("index diagnostics: %#v", ds)
		}
		_, diags := Check(index, modules)
		if !hasCode(diags, diag.SEM0010) {
			t.Fatalf("expected SEM0010, got %#v", diags)
		}
	})

	t.Run("driver path assigned twice", func(t *testing.T) {
		modules := parseModulesForTest(t, `
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
        let serial_path = SerialWritePath(owner: ExecutorPlacement(id: 0))
        let a = A(serial_path: serial_path)
        let b = B(serial_path: serial_path)
        a.run()
        b.run()
    }
}
`)
		index, ds := BuildIndex(modules)
		if len(ds) != 0 {
			t.Fatalf("index diagnostics: %#v", ds)
		}
		_, diags := Check(index, modules)
		if !hasCode(diags, diag.SEM0011) {
			t.Fatalf("expected SEM0011, got %#v", diags)
		}
	})
}
