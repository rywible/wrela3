package asm

import (
	"bytes"
	"testing"
)

func TestEncodeExactInstructions(t *testing.T) {
	r := must(Lookup("rax"))
	dx := must(Lookup("dx"))
	al := must(Lookup("al"))

	tests := []struct {
		name string
		code []Instruction
		want []byte
	}{
		{
			name: "ret",
			code: []Instruction{{Mnemonic: "ret"}},
			want: []byte{0xC3},
		},
		{
			name: "hlt",
			code: []Instruction{{Mnemonic: "hlt"}},
			want: []byte{0xF4},
		},
		{
			name: "pause",
			code: []Instruction{{Mnemonic: "pause"}},
			want: []byte{0xF3, 0x90},
		},
		{
			name: "cli",
			code: []Instruction{{Mnemonic: "cli"}},
			want: []byte{0xFA},
		},
		{
			name: "sti",
			code: []Instruction{{Mnemonic: "sti"}},
			want: []byte{0xFB},
		},
		{
			name: "out dx, al",
			code: []Instruction{{Mnemonic: "out", Operands: []Operand{RegOperand{dx}, RegOperand{al}}}},
			want: []byte{0xEE},
		},
		{
			name: "in al, dx",
			code: []Instruction{{Mnemonic: "in", Operands: []Operand{RegOperand{al}, RegOperand{dx}}}},
			want: []byte{0xEC},
		},
		{
			name: "mov cr3, rax",
			code: []Instruction{{Mnemonic: "mov", Operands: []Operand{RegOperand{must(Lookup("cr3"))}, RegOperand{r}}}},
			want: []byte{0x0F, 0x22, 0xD8},
		},
		{
			name: "mov rax, cr3",
			code: []Instruction{{Mnemonic: "mov", Operands: []Operand{RegOperand{r}, RegOperand{must(Lookup("cr3"))}}}},
			want: []byte{0x0F, 0x20, 0xD8},
		},
		{
			name: "lgdt [rax]",
			code: []Instruction{{Mnemonic: "lgdt", Operands: []Operand{MemOperand{Base: r}}}},
			want: []byte{0x0F, 0x01, 0x10},
		},
		{
			name: "lidt [rax]",
			code: []Instruction{{Mnemonic: "lidt", Operands: []Operand{MemOperand{Base: r}}}},
			want: []byte{0x0F, 0x01, 0x18},
		},
		{
			name: "push rbp",
			code: []Instruction{{Mnemonic: "push", Operands: []Operand{RegOperand{must(Lookup("rbp"))}}}},
			want: []byte{0x55},
		},
		{
			name: "pop rbp",
			code: []Instruction{{Mnemonic: "pop", Operands: []Operand{RegOperand{must(Lookup("rbp"))}}}},
			want: []byte{0x5D},
		},
		{
			name: "push imm8 and pop rax",
			code: []Instruction{
				{Mnemonic: "push", Operands: []Operand{ImmOperand{Value: 8}}},
				{Mnemonic: "pop", Operands: []Operand{RegOperand{must(Lookup("rax"))}}},
			},
			want: []byte{0x6A, 0x08, 0x58},
		},
		{
			name: "retfq",
			code: []Instruction{{Mnemonic: "retfq"}},
			want: []byte{0x48, 0xCB},
		},
		{
			name: "mov segment registers",
			code: []Instruction{
				{Mnemonic: "mov", Operands: []Operand{RegOperand{must(Lookup("ds"))}, RegOperand{must(Lookup("ax"))}}},
				{Mnemonic: "mov", Operands: []Operand{RegOperand{must(Lookup("es"))}, RegOperand{must(Lookup("ax"))}}},
				{Mnemonic: "mov", Operands: []Operand{RegOperand{must(Lookup("ss"))}, RegOperand{must(Lookup("ax"))}}},
				{Mnemonic: "mov", Operands: []Operand{RegOperand{must(Lookup("fs"))}, RegOperand{must(Lookup("ax"))}}},
				{Mnemonic: "mov", Operands: []Operand{RegOperand{must(Lookup("gs"))}, RegOperand{must(Lookup("ax"))}}},
			},
			want: []byte{0x8E, 0xD8, 0x8E, 0xC0, 0x8E, 0xD0, 0x8E, 0xE0, 0x8E, 0xE8},
		},
		{
			name: "mov high registers",
			code: []Instruction{{Mnemonic: "mov", Operands: []Operand{
				RegOperand{must(Lookup("r11"))},
				RegOperand{must(Lookup("r9"))},
			}}},
			want: []byte{0x4D, 0x8B, 0xD9},
		},
		{
			name: "call rax",
			code: []Instruction{{Mnemonic: "call", Operands: []Operand{RegOperand{must(Lookup("rax"))}}}},
			want: []byte{0xFF, 0xD0},
		},
		{
			name: "sub rsp imm32",
			code: []Instruction{{Mnemonic: "sub", Operands: []Operand{
				RegOperand{must(Lookup("rsp"))},
				ImmOperand{Value: 32},
			}}},
			want: []byte{0x48, 0x83, 0xEC, 0x20},
		},
		{
			name: "cmp rax, r10",
			code: []Instruction{{Mnemonic: "cmp", Operands: []Operand{
				RegOperand{must(Lookup("rax"))},
				RegOperand{must(Lookup("r10"))},
			}}},
			want: []byte{0x4C, 0x39, 0xD0},
		},
		{
			name: "shr rax imm8",
			code: []Instruction{{Mnemonic: "shr", Operands: []Operand{
				RegOperand{must(Lookup("rax"))},
				ImmOperand{Value: 16},
			}}},
			want: []byte{0x48, 0xC1, 0xE8, 0x10},
		},
		{
			name: "mov through rsp shadow slot",
			code: []Instruction{{Mnemonic: "mov", Operands: []Operand{
				MemOperand{Base: must(Lookup("rsp")), Disp: 32, Width: 64},
				RegOperand{must(Lookup("r11"))},
			}}},
			want: []byte{0x4C, 0x89, 0x5C, 0x24, 0x20},
		},
	}

	for _, tc := range tests {
		out, diags := Encode(tc.code)
		if len(diags) != 0 {
			t.Fatalf("%s returned diagnostics: %v", tc.name, diags)
		}
		if !bytes.Equal(out, tc.want) {
			t.Fatalf("%s = %#x, want %#x", tc.name, out, tc.want)
		}
	}
}

func must(reg Reg, ok bool) Reg {
	if !ok {
		panic("asm: unexpected register lookup failure")
	}
	return reg
}
