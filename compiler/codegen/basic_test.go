package codegen

import (
	"bytes"
	"testing"

	"github.com/ryanwible/wrela3/compiler/ir"
)

func TestCompileReturnConstantPrologueEpilogue(t *testing.T) {
	answer := &ir.ConstInt{
		Symbol: "ret",
		Value:  42,
		Type:   ir.Type{Name: "U64"},
	}
	program := &ir.Program{
		Functions: []ir.Function{
			{
				Symbol: "answer",
				Blocks: []ir.Block{
					{
						Label: "entry",
						Ops: []ir.Operation{
							answer,
							&ir.Return{Value: answer},
						},
					},
				},
			},
		},
	}

	image, diags := Compile(program)
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}

	code := image.Sections[0].Data
	prologue := []byte{0x55, 0x48, 0x89, 0xE5, 0x48, 0x81, 0xEC, 0x10, 0x00, 0x00, 0x00}
	if len(code) < len(prologue) {
		t.Fatalf("generated code too short: %d", len(code))
	}
	if !bytes.Equal(code[:len(prologue)], prologue) {
		t.Fatalf("prologue bytes = %#x, want %#x", code[:len(prologue)], prologue)
	}

	if !bytes.Contains(code, []byte{0x48, 0xB8, 0x2A, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}) {
		t.Fatalf("missing mov rax,42 in code %#x", code)
	}
	if !bytes.HasSuffix(code, []byte{0x48, 0x89, 0xEC, 0x5D, 0xC3}) {
		t.Fatalf("epilogue bytes = %#x, want ... 48 89 ec 5d c3", code[len(code)-5:])
	}
}
