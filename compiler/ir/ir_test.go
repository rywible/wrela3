package ir

import "testing"

func TestValuesInDeterministicOrder(t *testing.T) {
	param := &Param{Symbol: "in", Type: Type{Name: "U64"}}
	x := &ConstInt{Symbol: "x", Value: 1, Type: Type{Name: "U64"}}
	y := &ConstInt{Symbol: "y", Value: 2, Type: Type{Name: "U64"}}
	bin := &Binary{Op: "add", Left: x, Right: y, Type: Type{Name: "U64"}}
	call := &Call{Symbol: "foo", Receiver: x, Args: []Value{y}, Type: Type{Name: "U64"}}
	load := &FieldLoad{Object: param, ObjectType: "MyData", Field: "x", Type: Type{Name: "U64"}}
	branch := &Branch{Condition: x, True: "a", False: "b"}

	fn := Function{
		Params: []Value{param},
		Blocks: []Block{
			{Label: "entry", Ops: []Operation{x, load, call, branch}},
			{Label: "then", Ops: []Operation{bin, y}},
		},
	}

	got := fn.ValuesInDeterministicOrder()
	want := []Value{x, load, call, bin, y}
	if len(got) != len(want) {
		t.Fatalf("len(values) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("value[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func TestBlockAndOperationTypesExist(t *testing.T) {
	fn := Function{
		Symbol: "entry",
		Params: []Value{&Param{Symbol: "p", Type: Type{Name: "U64"}}},
		Blocks: []Block{
			{
				Label: "entry",
				Ops: []Operation{
					&ConstInt{Symbol: "x", Value: 42, Type: Type{Name: "U64"}},
					&Binary{Op: "add", Left: &ConstInt{Symbol: "a", Value: 1, Type: Type{Name: "U64"}}, Right: &ConstInt{Symbol: "b", Value: 2, Type: Type{Name: "U64"}}, Type: Type{Name: "U64"}},
					&Call{Symbol: "callee", Receiver: &ConstInt{Symbol: "recv", Value: 0, Type: Type{Name: "U64"}}, Type: Type{Name: "U64"}},
					&Branch{},
					&Return{Value: &ConstInt{Symbol: "ret", Value: 0, Type: Type{Name: "U64"}}},
					&ForBytes{},
				},
			},
		},
	}

	if len(fn.Symbol) == 0 || len(fn.Params) != 1 || len(fn.Blocks) != 1 || len(fn.Blocks[0].Ops) != 6 {
		t.Fatalf("unexpected IR scaffolding: %#v", fn)
	}
}

func TestValuesInDeterministicOrderIncludesElseBranchValues(t *testing.T) {
	els := &ConstInt{Symbol: "elseVal", Value: 9, Type: Type{Name: "U64"}}
	fn := Function{
		Symbol: "entry",
		Blocks: []Block{
			{
				Label: "entry",
				Ops: []Operation{
					&If{
						Condition: &ConstInt{Value: 0, Type: Type{Name: "U64"}},
						Then:      []Operation{},
						Else:      []Operation{els},
					},
				},
			},
		},
	}

	got := fn.ValuesInDeterministicOrder()
	if len(got) != 1 || got[0] != els {
		t.Fatalf("ValuesInDeterministicOrder() = %#v, want else branch value only", got)
	}
}
