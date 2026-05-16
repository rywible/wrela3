package codegen

import "github.com/ryanwible/wrela3/compiler/asm"

const (
	lapicSVR     = 0xF0
	lapicICRLow  = 0x300
	lapicICRHigh = 0x310
)

func emitLapicWrite(e *Emitter, args ...interface{}) {
	base, offset, value := lapicWriteArgs(args...)
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("r11")},
		asm.ImmOperand{Value: int64(base)},
	}})
	emitMovImmToReg(e, asm.MustLookup("rax"), int64(value))
	emitStoreMemFromReg(e, asm.MustLookup("r11"), int64(offset), asm.MustLookup("rax"), 32)
}

func lapicWriteArgs(args ...interface{}) (uint64, uint32, uint32) {
	base := uint64(0xFEE00000)
	if len(args) == 3 {
		base = asUint64(args[0])
		return base, asUint32(args[1]), asUint32(args[2])
	}
	return base, asUint32(args[0]), asUint32(args[1])
}

func asUint64(v interface{}) uint64 {
	switch n := v.(type) {
	case uint64:
		return n
	case uint32:
		return uint64(n)
	case int:
		return uint64(n)
	default:
		return 0
	}
}

func asUint32(v interface{}) uint32 {
	switch n := v.(type) {
	case uint32:
		return n
	case uint64:
		return uint32(n)
	case int:
		return uint32(n)
	default:
		return 0
	}
}
