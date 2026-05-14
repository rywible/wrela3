package codegen

import (
	"github.com/ryanwible/wrela3/compiler/asm"
	"github.com/ryanwible/wrela3/compiler/ir"
)

func emitEntryAdapter(e *Emitter, entry ir.EntryAdapter) {
	e.emit(0x55)
	e.emit(0x48, 0x89, 0xE5)

	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("rdi")},
		asm.RegOperand{Reg: asm.MustLookup("rcx")},
	}})
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("rsi")},
		asm.RegOperand{Reg: asm.MustLookup("rdx")},
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
