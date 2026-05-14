package codegen

import "github.com/ryanwible/wrela3/compiler/asm"

var argRegs = []asm.Reg{
	asm.MustLookup("rdi"),
	asm.MustLookup("rsi"),
	asm.MustLookup("rdx"),
	asm.MustLookup("rcx"),
	asm.MustLookup("r8"),
	asm.MustLookup("r9"),
}

var scratchRegs = []asm.Reg{
	asm.MustLookup("rax"),
	asm.MustLookup("r10"),
	asm.MustLookup("r11"),
}
