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
