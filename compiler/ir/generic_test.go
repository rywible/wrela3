package ir

import (
	"testing"
)

func TestGenericMethodBodyIsLoweredForConcreteInstantiation(t *testing.T) {
	program := lowerSourceForTest(t, `
module ir.generics
data Event { kind: U64 }
class Holder<T> {
    value: T
    fn read(self) -> T {
        return self.value
    }
}
executor Worker {
    holder: Holder<Event>
    start fn run(self) -> never {
        let event = self.holder.read()
        while true {}
    }
}
`)
	fn := findFunction(program, "_wrela_method_ir_generics_Holder_Event_read")
	if fn == nil {
		t.Fatal("missing concrete Holder<Event>.read function")
	}
	if !containsOp[*FieldLoad](*fn) {
		t.Fatalf("read body did not lower the cloned field load: %#v", fn.Blocks)
	}
}

func TestLowerGenericEnumTypeInfo(t *testing.T) {
	program := lowerSourceForTest(t, `
module ir.enum_layout
enum Option<T> { None Some(value: T) }
data Event {
    first: U64
    second: U64
    kind: U32
}
executor Worker {
    start fn run(self, next: Option<Event>) -> never {
        while true {}
    }
}
`)
	info, ok := program.Types["ir.enum_layout.Option[ir.enum_layout.Event]"]
	if !ok {
		t.Fatalf("missing concrete Option<Event> type info: %#v", program.Types)
	}
	if info.Fields["$tag"].Offset != 0 || info.Fields["Some.value"].Offset != 8 {
		t.Fatalf("enum field offsets = %#v", info.Fields)
	}
	if info.Fields["Some.value"].StorageSize <= 8 || info.StorageSize != 32 {
		t.Fatalf("enum did not use semantic payload size: info=%#v", info)
	}
	if len(info.EnumVariants) != 2 || info.EnumVariants[0].Name != "None" || info.EnumVariants[1].Discriminant != 1 {
		t.Fatalf("enum variants = %#v", info.EnumVariants)
	}
}
