package codegen

import "github.com/ryanwible/wrela3/compiler/asm"

const (
	lapicBase    = 0xFEE00000
	lapicICRLow  = 0x300
	lapicICRHigh = 0x310
)

func emitLapicWrite(e *Emitter, offset uint32, value uint32) {
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("r11")},
		asm.ImmOperand{Value: lapicBase},
	}})
	emitMovImmToReg(e, asm.MustLookup("rax"), int64(value))
	emitStoreMemFromReg(e, asm.MustLookup("r11"), int64(offset), asm.MustLookup("rax"), 32)
}
