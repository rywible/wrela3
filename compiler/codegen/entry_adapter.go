package codegen

import (
	"github.com/ryanwible/wrela3/compiler/asm"
	"github.com/ryanwible/wrela3/compiler/ir"
)

func emitEntryAdapter(e *Emitter, entry ir.EntryAdapter) {
	e.emit(0x55)
	e.emit(0x48, 0x89, 0xE5)
	e.emitInstruction(asm.Instruction{Mnemonic: "sub", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("rsp")},
		asm.ImmOperand{Value: 64},
	}})

	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.MemOperand{Base: asm.MustLookup("rbp"), Disp: -64, Width: 64},
		asm.RegOperand{Reg: asm.MustLookup("rcx")},
	}})
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("rax")},
		asm.MemOperand{Base: asm.MustLookup("rdx"), Disp: 96, Width: 64},
	}})
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.MemOperand{Base: asm.MustLookup("rbp"), Disp: -56, Width: 64},
		asm.RegOperand{Reg: asm.MustLookup("rax")},
	}})
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("rax")},
		asm.ImmOperand{Value: 0x200000},
	}})
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.MemOperand{Base: asm.MustLookup("rbp"), Disp: -48, Width: 64},
		asm.RegOperand{Reg: asm.MustLookup("rax")},
	}})
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.MemOperand{Base: asm.MustLookup("rbp"), Disp: -40, Width: 64},
		asm.RegOperand{Reg: asm.MustLookup("rax")},
	}})
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("rax")},
		asm.ImmOperand{Value: 0},
	}})
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.MemOperand{Base: asm.MustLookup("rbp"), Disp: -32, Width: 64},
		asm.RegOperand{Reg: asm.MustLookup("rax")},
	}})
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.MemOperand{Base: asm.MustLookup("rbp"), Disp: -24, Width: 64},
		asm.RegOperand{Reg: asm.MustLookup("rax")},
	}})

	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("rdi")},
		asm.RegOperand{Reg: asm.MustLookup("rbp")},
	}})
	e.emitInstruction(asm.Instruction{Mnemonic: "add", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("rdi")},
		asm.ImmOperand{Value: -64},
	}})

	emitSymbolCall(e, entry.DelegatedPhaseSymbol)
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("rdi")},
		asm.RegOperand{Reg: asm.MustLookup("rax")},
	}})
	emitSymbolCall(e, entry.OwnedPhaseSymbol)

	loop := e.newLabel("entry_halt")
	e.bindLabel(loop)
	e.emit(0xF4)
	e.emitJmp(loop)
}

func emitSymbolCall(e *Emitter, symbol string) {
	e.emit(0xE8, 0, 0, 0, 0)
	e.CallReloc = append(e.CallReloc, internalReloc{Offset: uint64(len(e.Code) - 4), Symbol: symbol})
}
