package asm

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
)

func TestParseBodyMovFieldParamOperands(t *testing.T) {
	src := "mov dx, self.port_base; add dx, offset; out dx, al; ret"
	instructions, diags := ParseBody(src, []string{"offset", "value"})
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", diags)
	}
	if len(instructions) != 4 {
		t.Fatalf("len(instructions) = %d, want 4", len(instructions))
	}

	mop, ok := instructions[0].Operands[1].(FieldOperand)
	if !ok || mop.Base != "self" || mop.Field != "port_base" {
		t.Fatalf("mov operand = %#v, want self.port_base", instructions[0].Operands[1])
	}
	if _, ok := instructions[1].Operands[1].(ParamOperand); !ok {
		t.Fatalf("add operand = %#v, want param operand", instructions[1].Operands[1])
	}
}

func TestParseBodyUnknownInstruction(t *testing.T) {
	instructions, diags := ParseBody("bad_asm", nil)
	if len(instructions) != 0 {
		t.Fatalf("expected no instructions, got %d", len(instructions))
	}
	if len(diags) != 1 || diags[0].Code != diag.ASM0001 {
		t.Fatalf("diags = %+v, want one %s", diags, diag.ASM0001)
	}
}

func TestParseBodyUnknownOperand(t *testing.T) {
	instructions, diags := ParseBody("mov dx, nope", nil)
	if len(instructions) != 1 {
		t.Fatalf("expected one instruction, got %d", len(instructions))
	}
	if len(diags) != 1 || diags[0].Code != diag.ASM0002 {
		t.Fatalf("diags = %+v, want one %s", diags, diag.ASM0002)
	}
}

func TestParseBodyBranchLabelReference(t *testing.T) {
	instructions, diags := ParseBody("loop:\n  jmp loop", nil)
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", diags)
	}
	if len(instructions) != 2 {
		t.Fatalf("len(instructions) = %d, want 2", len(instructions))
	}
	if instructions[0].Label != "loop" {
		t.Fatalf("first instruction label = %q, want loop", instructions[0].Label)
	}
	if _, ok := instructions[1].Operands[0].(LabelRef); !ok {
		t.Fatalf("jmp operand = %#v, want label ref", instructions[1].Operands[0])
	}
}
