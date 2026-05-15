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
        let p = Pair(a = 1)
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

	t.Run("constructor duplicate field is rejected", func(t *testing.T) {
		modules := parseModulesForTest(t, `
module index.types_constructor_duplicate_fields
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
        let p = Pair(a = 1, a = 2)
        while true {}
    }
}
`)
		index, ds := BuildIndex(modules)
		if len(ds) != 0 {
			t.Fatalf("index diagnostics: %#v", ds)
		}
		_, diags := Check(index, modules)
		if !hasMessage(diags, diag.CG0001, "duplicate constructor field a") {
			t.Fatalf("expected duplicate constructor field error, got %#v", diags)
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
        for byte in Bytes(address = 0, length = 1) {
            let _ = byte
        }
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

	t.Run("method allows self plus five explicit params", func(t *testing.T) {
		modules := parseModulesForTest(t, `
module index.types_self_plus_five_params
`+typePrelude+`

class JustEnough {
    fn ok(self, a: U8, b: U8, c: U8, d: U8, e: U8) {}
}

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
			t.Fatalf("index diagnostics: %#v", ds)
		}
		_, diags := Check(index, modules)
		if hasCode(diags, diag.SEM0013) {
			t.Fatalf("unexpected SEM0013 for receiver plus five explicit params: %#v", diags)
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
        let base = Narrow(a = 1, b = 2, c = 0x200000, d = 0x200000)
        let next = base.d + 8
        let alias = Narrow(a = 1, b = 2, c = next, d = base.c)
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

	t.Run("missing return is rejected", func(t *testing.T) {
		modules := parseModulesForTest(t, `
module index.types_missing_return
`+typePrelude+`

class Bad {
    fn value(self) -> U8 {
        let x = 1
    }
}

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
			t.Fatalf("index diagnostics: %#v", ds)
		}
		_, diags := Check(index, modules)
		if !hasCode(diags, diag.CG0001) {
			t.Fatalf("expected missing return diagnostic, got %#v", diags)
		}
	})

	t.Run("unreachable tail statements still produce diagnostics", func(t *testing.T) {
		modules := parseModulesForTest(t, `
module index.types_unreachable_tail_diagnostics
`+typePrelude+`

class Bad {
    fn value(self) -> U8 {
        return 1
        let x = Missing()
    }
}

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
			t.Fatalf("index diagnostics: %#v", ds)
		}
		_, diags := Check(index, modules)
		if !hasCode(diags, diag.SEM0002) {
			t.Fatalf("expected unreachable tail type diagnostic, got %#v", diags)
		}
	})

	t.Run("while true satisfies return flow", func(t *testing.T) {
		modules := parseModulesForTest(t, `
module index.types_while_true_returns
`+typePrelude+`

class Good {
    fn value(self) -> U8 {
        while true {}
    }
}

image GoodImage {
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
		if len(diags) != 0 {
			t.Fatalf("expected no diagnostics, got %#v", diags)
		}
	})

	t.Run("never call satisfies return flow", func(t *testing.T) {
		modules := parseModulesForTest(t, `
module index.types_never_call_returns
`+typePrelude+`

class Good {
    fn halt(self) -> never {
        while true {}
    }

    fn value(self) -> U8 {
        self.halt()
    }
}

image GoodImage {
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
		if len(diags) != 0 {
			t.Fatalf("expected no diagnostics, got %#v", diags)
		}
	})

	t.Run("bare return from never is rejected", func(t *testing.T) {
		modules := parseModulesForTest(t, `
module index.types_never_bare_return
`+typePrelude+`

class Bad {
    fn halt(self) -> never {
        return
    }
}

image GoodImage {
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
		var count int
		for _, d := range diags {
			if d.Code == diag.CG0001 {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("CG0001 count = %d, want 1; diagnostics = %#v", count, diags)
		}
	})

	t.Run("unknown field type is reported", func(t *testing.T) {
		modules := parseModulesForTest(t, `
module index.types_unknown_field
`+typePrelude+`

data Bad {
    value: Missing
}

image Good {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.exit_to_owned_hardware()
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        let bad = Bad(value = 1)
        while true {}
    }
}
`)
		index, ds := BuildIndex(modules)
		if len(ds) != 0 {
			t.Fatalf("index diagnostics: %#v", ds)
		}
		_, diags := Check(index, modules)
		if !hasCode(diags, diag.SEM0002) {
			t.Fatalf("expected unknown type diagnostic, got %#v", diags)
		}
	})

	t.Run("unknown image phase parameter type is reported", func(t *testing.T) {
		modules := parseModulesForTest(t, `
module index.types_unknown_phase_param
`+typePrelude+`

image Bad {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardwar) -> OwnedHardware {
        return OwnedHardware()
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
		if !hasCode(diags, diag.SEM0002) {
			t.Fatalf("expected unknown phase parameter type diagnostic, got %#v", diags)
		}
	})

	t.Run("call arguments cannot be positional after named", func(t *testing.T) {
		modules := parseModulesForTest(t, `
module index.types_call_arg_mix
`+typePrelude+`

class Callee {
    fn take(self, a: U8, b: U8) -> U8 {
        return a
    }
}

image Good {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.exit_to_owned_hardware()
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        let c = Callee()
        let x = c.take(a = 1, 2)
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
			t.Fatalf("expected call argument diagnostic, got %#v", diags)
		}
	})
}

const interruptPrelude = `
unique class OwnedHardware {}
unique class DelegatedHardware {
    fn exit_to_owned_hardware(self) -> OwnedHardware {
        return OwnedHardware()
    }
}

data SerialInterrupt { vector: U8 }
data OtherInterrupt { vector: U8 }

driver path SerialConsolePath {
    interrupt receiver -> SerialInterrupt {
        return SerialInterrupt(vector = 1)
    }
}

executor ConsoleExec {
    serial: SerialConsolePath

    on serial.interrupt(event: SerialInterrupt) {}
}
`

func TestInterruptEventBodyContracts(t *testing.T) {
	_, diags := checkModuleForTest(t, `
module index.interrupt_event_contracts
`+typePrelude+`

data SerialInterrupt { vector: U8 }
data OtherInterrupt { vector: U8 }

driver path MissingEvent {
    interrupt receiver -> MissingInterrupt {
        return MissingInterrupt()
    }
}

driver path WrongReturn {
    interrupt receiver -> SerialInterrupt {
        return OtherInterrupt(vector = 1)
    }
}

driver path MissingReturn {
    interrupt receiver -> SerialInterrupt {}
}
`)
	if !hasCode(diags, diag.SEM0002) {
		t.Fatalf("expected SEM0002, got %#v", diags)
	}
	if !hasCode(diags, diag.SEM0015) {
		t.Fatalf("expected SEM0015, got %#v", diags)
	}
}

func TestInterruptEventMustReturnDataRecord(t *testing.T) {
	_, diags := checkModuleForTest(t, `
module index.interrupt_event_data_record
`+typePrelude+`

driver path PrimitiveEvent {
    interrupt receiver -> U8 {
        return 1
    }
}
`)
	if !hasCode(diags, diag.SEM0015) {
		t.Fatalf("expected SEM0015, got %#v", diags)
	}
}

func TestExecutorMustHandleInterruptPathFields(t *testing.T) {
	_, diags := checkModuleForTest(t, `
module index.interrupt_missing_on
`+interruptPrelude+`

executor MissingHandler {
    serial: SerialConsolePath
}

image Bad {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.exit_to_owned_hardware()
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        let serial = SerialConsolePath()
        let exec = MissingHandler(serial = serial)
        while true {}
    }
}
`)
	if !hasCode(diags, diag.SEM0017) {
		t.Fatalf("expected SEM0017, got %#v", diags)
	}
}

func TestOnHandlerMustReferenceDirectInterruptPathField(t *testing.T) {
	_, diags := checkModuleForTest(t, `
module index.interrupt_bad_on_field
`+interruptPrelude+`

executor BadField {
    serial: SerialConsolePath

    on missing.interrupt(event: SerialInterrupt) {}
}
`)
	if !hasCode(diags, diag.SEM0018) {
		t.Fatalf("expected SEM0018, got %#v", diags)
	}
}

func TestOnHandlerParamTypeMustMatchInterruptEvent(t *testing.T) {
	_, diags := checkModuleForTest(t, `
module index.interrupt_param_type
`+interruptPrelude+`

executor BadParam {
    serial: SerialConsolePath

    on serial.interrupt(event: OtherInterrupt) {}
}
`)
	if !hasCode(diags, diag.SEM0016) {
		t.Fatalf("expected SEM0016, got %#v", diags)
	}
}

func TestOnHandlerRejectsForbiddenBodyShapes(t *testing.T) {
	_, diags := checkModuleForTest(t, `
module index.interrupt_forbidden_body
`+interruptPrelude+`

class RuntimeThing {}

executor BadBody {
    serial: SerialConsolePath

    on serial.interrupt(event: SerialInterrupt) {
        while true {}
        return event
        let thing = RuntimeThing()
    }
}
`)
	if !hasCode(diags, diag.SEM0016) {
		t.Fatalf("expected SEM0016, got %#v", diags)
	}
}

func TestOnHandlerRejectsAllocationAndInterruptReconfigurationCalls(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory
class ExecutorMemory {
    fn allocate_bytes(self, length: U64) -> U64 {
        return length
    }
}
`, `
module machine.x86_64.interrupts
class LocalApic {
    fn enable(self) {}
}

class IoApic {
    fn route_gsi4_to_vector40(self) {}
}

class ApicInterruptController {
    local_apic: LocalApic
    io_apic: IoApic

    fn enable_cpu_interrupts(self) {}
}
`, `
module machine.x86_64.serial

use { ExecutorMemory } from machine.x86_64.executor_memory
use { ApicInterruptController } from machine.x86_64.interrupts

unique class OwnedHardware {}
unique class DelegatedHardware {
    fn exit_to_owned_hardware(self) -> OwnedHardware {
        return OwnedHardware()
    }
}

data SerialInterrupt { vector: U8 }

driver path SerialConsolePath {
    interrupt receiver -> SerialInterrupt {
        return SerialInterrupt(vector = 1)
    }

    fn enable_receive_interrupts(self) {}
}

executor BadHandler {
    serial: SerialConsolePath
    memory: ExecutorMemory
    interrupts: ApicInterruptController

    on serial.interrupt(event: SerialInterrupt) {
        self.memory.allocate_bytes(length = 8)
        self.serial.enable_receive_interrupts()
        self.interrupts.local_apic.enable()
        self.interrupts.io_apic.route_gsi4_to_vector40()
    }
}
`)
	index, ds := BuildIndex(modules)
	ds = filterMissingImageDiagnostic(ds)
	if len(ds) != 0 {
		t.Fatalf("index diagnostics: %#v", ds)
	}
	_, diags := Check(index, modules)
	if !hasCode(diags, diag.SEM0016) {
		t.Fatalf("expected SEM0016, got %#v", diags)
	}
}

func TestInterruptEventCannotBeCalledNormally(t *testing.T) {
	_, diags := checkModuleForTest(t, `
module index.interrupt_call
`+interruptPrelude+`

executor BadCall {
    serial: SerialConsolePath

    on serial.interrupt(event: SerialInterrupt) {
        self.serial.interrupt()
    }
}
`)
	if !hasCode(diags, diag.SEM0019) {
		t.Fatalf("expected SEM0019, got %#v", diags)
	}
}

func TestExplicitInterruptBindCallRejected(t *testing.T) {
	_, diags := checkModuleForTest(t, `
module index.interrupt_bind_call
`+interruptPrelude+`

class Interrupts {
    fn bind(self) {}
}

executor BadBind {
    serial: SerialConsolePath
    interrupts: Interrupts

    on serial.interrupt(event: SerialInterrupt) {
        self.interrupts.bind()
    }
}
`)
	if !hasCode(diags, diag.SEM0019) {
		t.Fatalf("expected SEM0019, got %#v", diags)
	}
}

func TestOnlySupportedInterruptRuntimeBindingsExposed(t *testing.T) {
	checked, diags := checkModuleForTest(t, `
module machine.x86_64.serial
`+interruptPrelude+`

image Good {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.exit_to_owned_hardware()
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        let serial = SerialConsolePath()
        let exec = ConsoleExec(serial = serial)
        while true {}
    }
}
`)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", diags)
	}
	if len(checked.InterruptBindings) != 1 || checked.InterruptBindings[0].Vector != 0x40 {
		t.Fatalf("interrupt bindings = %#v, want vector 0x40", checked.InterruptBindings)
	}
}

func TestOnlySupportedInterruptRuntimeRejectsReachableUnsupported(t *testing.T) {
	_, diags := checkModuleForTest(t, `
module index.interrupt_unsupported_reachable
`+interruptPrelude+`

image Bad {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.exit_to_owned_hardware()
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        let serial = SerialConsolePath()
        let exec = ConsoleExec(serial = serial)
        while true {}
    }
}
`)
	if !hasCode(diags, diag.SEM0020) {
		t.Fatalf("expected SEM0020, got %#v", diags)
	}
}

func TestUnsupportedInterruptRuntimeShapeIgnoredWhenUnreachable(t *testing.T) {
	checked, diags := checkModuleForTest(t, `
module index.interrupt_unsupported_unreachable
`+interruptPrelude+`

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
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", diags)
	}
	if len(checked.InterruptBindings) != 0 {
		t.Fatalf("interrupt bindings = %#v, want none", checked.InterruptBindings)
	}
}

func TestDuplicateReachableInterruptRuntimeVectorRejected(t *testing.T) {
	_, diags := checkModuleForTest(t, `
module machine.x86_64.serial
`+interruptPrelude+`

executor DuplicateSerial {
    first: SerialConsolePath
    second: SerialConsolePath

    on first.interrupt(event: SerialInterrupt) {}
    on second.interrupt(event: SerialInterrupt) {}
}

image Bad {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.exit_to_owned_hardware()
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        let first = SerialConsolePath()
        let second = SerialConsolePath()
        let exec = DuplicateSerial(first = first, second = second)
        while true {}
    }
}
`)
	if !hasCode(diags, diag.SEM0020) {
		t.Fatalf("expected SEM0020, got %#v", diags)
	}
}
