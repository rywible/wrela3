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

func TestCompilePreservesInterruptBindings(t *testing.T) {
	program := &ir.Program{
		InterruptBindings: []ir.InterruptBinding{{
			EventSymbol:           "interrupt_event::machine.x86_64.serial::SerialConsolePath::interrupt",
			HandlerSymbol:         "on_handler::examples.hello.program::HelloWorld::serial_path::interrupt",
			EventFunctionSymbol:   "_wrela_event_fn_machine_x86_64_serial_SerialConsolePath_interrupt",
			HandlerFunctionSymbol: "_wrela_on_fn_examples_hello_program_HelloWorld_serial_path_interrupt",
			PathFieldOffset:       8,
			EventStorageSize:      1,
			EventStorageSymbol:    "_wrela_interrupt_event_40",
			Vector:                0x40,
		}},
	}

	image, diags := Compile(program)
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}
	if len(image.InterruptBindings) != 1 {
		t.Fatalf("image interrupt bindings = %#v, want one", image.InterruptBindings)
	}
	got := image.InterruptBindings[0]
	if got.Vector != 0x40 || got.PathFieldOffset != 8 || got.EventStorageSymbol != "_wrela_interrupt_event_40" {
		t.Fatalf("image interrupt binding = %#v", got)
	}
	if got.EventFunctionSymbol != program.InterruptBindings[0].EventFunctionSymbol || got.HandlerFunctionSymbol != program.InterruptBindings[0].HandlerFunctionSymbol {
		t.Fatalf("image interrupt binding functions = %#v, want %#v", got, program.InterruptBindings[0])
	}
}
