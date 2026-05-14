package codegen

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/ir"
)

func TestCompileWhileLoops(t *testing.T) {
	cond := &ir.Param{Symbol: "cond", Type: ir.Type{Name: "U64"}}
	done := &ir.ConstInt{
		Symbol: "done",
		Value:  0,
		Type:   ir.Type{Name: "U64"},
	}
	fn := ir.Function{
		Symbol: "while_fn",
		Params: []ir.Value{
			cond,
		},
		Blocks: []ir.Block{
			{
				Label: "entry",
				Ops: []ir.Operation{
					&ir.While{
						Condition: cond,
						Body:      []ir.Operation{},
					},
					done,
					&ir.Return{Value: done},
				},
			},
		},
	}

	image, diags := Compile(&ir.Program{Functions: []ir.Function{fn}})
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}

	code := image.Sections[0].Data
	if !bytes.Contains(code, []byte{0x0F, 0x84}) {
		t.Fatalf("expected conditional jump for while condition, got %#x", code)
	}
	if !bytes.Contains(code, []byte{0xE9}) {
		t.Fatalf("expected loop jump in while body, got %#x", code)
	}
}

func TestCompileForBytes(t *testing.T) {
	input := &ir.Param{Symbol: "input", Type: ir.Type{Name: "Bytes"}}
	done := &ir.ConstInt{
		Symbol: "done",
		Value:  0,
		Type:   ir.Type{Name: "U64"},
	}
	fn := ir.Function{
		Symbol: "bytes_fn",
		Params: []ir.Value{
			input,
		},
		Blocks: []ir.Block{
			{
				Label: "entry",
				Ops: []ir.Operation{
					&ir.ForBytes{
						Iterable:  input,
						Index:     &ir.Param{Symbol: "index", Type: ir.Type{Name: "U64"}},
						ByteValue: &ir.Param{Symbol: "byte", Type: ir.Type{Name: "U8"}},
						Body:      []ir.Operation{},
					},
					done,
					&ir.Return{Value: done},
				},
			},
		},
	}

	image, diags := Compile(&ir.Program{Functions: []ir.Function{fn}})
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}

	code := image.Sections[0].Data
	if !bytes.Contains(code, []byte{0x48, 0x0F, 0xB6, 0x04, 0x0F}) {
		t.Fatalf("expected u8 load for ForBytes, got %#x", code)
	}
	if !bytes.Contains(code, []byte{0x48, 0x83, 0xC1, 0x01}) {
		t.Fatalf("expected index increment in ForBytes loop, got %#x", code)
	}
}

func TestCompileCallRelocation(t *testing.T) {
	calleeRet := &ir.ConstInt{
		Symbol: "ret",
		Value:  7,
		Type:   ir.Type{Name: "U64"},
	}
	callee := ir.Function{
		Symbol: "callee",
		Blocks: []ir.Block{
			{
				Label: "entry",
				Ops: []ir.Operation{
					calleeRet,
					&ir.Return{Value: calleeRet},
				},
			},
		},
	}

	call := &ir.Call{
		Symbol: "callee",
		Type:   ir.Type{Name: "U64"},
	}
	caller := ir.Function{
		Symbol: "caller",
		Blocks: []ir.Block{
			{
				Label: "entry",
				Ops: []ir.Operation{
					call,
					&ir.Return{
						Value: call,
					},
				},
			},
		},
	}

	image, diags := Compile(&ir.Program{Functions: []ir.Function{callee, caller}})
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}

	callerStart := int(image.Symbols["caller"] - 0x1000)
	callerCode := image.Sections[0].Data[callerStart:]
	callOffset := bytes.IndexByte(callerCode, 0xE8)
	if callOffset < 0 {
		t.Fatalf("missing call opcode in caller")
	}

	callRel := int32(binary.LittleEndian.Uint32(callerCode[callOffset+1 : callOffset+5]))
	got := int64(callRel)
	expect := int64(int64(image.Symbols["callee"]) - int64(image.Symbols["caller"]+uint64(callOffset+5)))
	if got != expect {
		t.Fatalf("call rel32 = %d, want %d", got, expect)
	}
}

func TestCompileRejectsTooManyCallArguments(t *testing.T) {
	args := make([]ir.Value, 6)
	for i := range args {
		args[i] = &ir.ConstInt{
			Symbol: fmt.Sprintf("arg%d", i),
			Value:  uint64(i),
			Type:   ir.Type{Name: "U64"},
		}
	}

	fn := ir.Function{
		Symbol: "bad_call",
		Blocks: []ir.Block{
			{
				Label: "entry",
				Ops: []ir.Operation{
					&ir.Call{
						Symbol: "callee",
						Args:   args,
						Type:   ir.Type{Name: "U64"},
					},
				},
			},
		},
	}

	_, diags := Compile(&ir.Program{Functions: []ir.Function{fn}})
	if len(diags) != 1 || diags[0].Code != diag.SEM0013 {
		t.Fatalf("diags = %#v, want one SEM0013", diags)
	}
}

func TestCompileBranchTargetsBlockLabelsAfterPrologue(t *testing.T) {
	cond := &ir.Param{Symbol: "cond", Type: ir.Type{Name: "Bool"}}
	one := &ir.ConstInt{Symbol: "one", Value: 1, Type: ir.Type{Name: "U64"}}
	zero := &ir.ConstInt{Symbol: "zero", Value: 0, Type: ir.Type{Name: "U64"}}
	fn := ir.Function{
		Symbol: "branch_blocks",
		Params: []ir.Value{cond},
		Blocks: []ir.Block{
			{Label: "entry", Ops: []ir.Operation{
				&ir.Branch{Condition: cond, True: "then", False: "else"},
			}},
			{Label: "then", Ops: []ir.Operation{one, &ir.Return{Value: one}}},
			{Label: "else", Ops: []ir.Operation{zero, &ir.Return{Value: zero}}},
		},
	}
	image, diags := Compile(&ir.Program{Functions: []ir.Function{fn}})
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}
	code := image.Sections[0].Data
	jcc := bytes.Index(code, []byte{0x0F, 0x84})
	if jcc < 0 {
		t.Fatalf("missing conditional branch in %#x", code)
	}
	falseTarget := jcc + 6 + int(int32(binary.LittleEndian.Uint32(code[jcc+2:jcc+6])))
	if falseTarget <= 0 {
		t.Fatalf("false branch target = %d, want after prologue", falseTarget)
	}
	jmp := bytes.IndexByte(code[jcc+6:], 0xE9)
	if jmp < 0 {
		t.Fatalf("missing unconditional branch in %#x", code)
	}
	jmp += jcc + 6
	trueTarget := jmp + 5 + int(int32(binary.LittleEndian.Uint32(code[jmp+1:jmp+5])))
	if trueTarget <= 0 {
		t.Fatalf("true branch target = %d, want after prologue", trueTarget)
	}
}
