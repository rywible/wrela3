package codegen

import "github.com/ryanwible/wrela3/compiler/asm"

func emitX2APICWriteMSR(e *Emitter, msr uint32, valueReg asm.Reg) {
	e.emit(0xB9)
	e.emitUint32(msr)
	emitRegRegMove(e, asm.MustLookup("rax"), valueReg)
	emitRegRegMove(e, asm.MustLookup("rdx"), valueReg)
	emitShiftImm(e, 0x05, asm.MustLookup("rdx"), 32)
	e.emit(0x0F, 0x30)
}
