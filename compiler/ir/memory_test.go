package ir

import "testing"

func TestMemoryOpsDefineExpectedValues(t *testing.T) {
	frame := &FrameBegin{Symbol: "tick", Parent: Local{Symbol: "memory"}, Length: ConstInt{Value: 64}}
	reserve := &ArenaReserve{
		Arena: frame,
		Length: ConstInt{Value: 32},
		Align:  ConstInt{Value: 8},
	}
	place := &ArenaPlace{Arena: frame, Type: Type{Name: "Message"}}
	fn := Function{
		Blocks: []Block{{
			Label: "entry",
			Ops:   []Operation{frame, reserve, place, &FrameEnd{Frame: frame}},
		}},
	}
	values := fn.ValuesInDeterministicOrder()
	if len(values) != 3 {
		t.Fatalf("values = %#v, want frame reserve place", values)
	}
}
