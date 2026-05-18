package ir

import "testing"

func TestLowerConstReferenceToConstInt(t *testing.T) {
	program := lowerSourceForTest(t, `
module ir.const_refs

const EVENT_CAPACITY: U64 = 16

executor Worker {
    start fn run(self) -> never {
        if EVENT_CAPACITY == 16 {
            while true {}
        }
        while true {}
    }
}
`)
	fn := findFunction(program, "_wrela_method_ir_const_refs_Worker_run")
	if fn == nil {
		t.Fatal("missing Worker.run")
	}
	condition, ok := functionOp[*Binary](*fn)
	if !ok {
		t.Fatalf("missing lowered const comparison: %#v", fn.Blocks)
	}
	left, ok := condition.Left.(*ConstInt)
	if !ok || left.Value != 16 {
		t.Fatalf("const reference lowered to %#v, want ConstInt(16)", condition.Left)
	}
}
