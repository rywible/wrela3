package codegen

import (
	"github.com/ryanwible/wrela3/compiler/asm"
	"github.com/ryanwible/wrela3/compiler/ir"
)

const (
	lapicEOI               = 0xB0
	lapicSVR               = 0xF0
	lapicICRLow            = 0x300
	lapicICRHigh           = 0x310
	lapicLVTTimer          = 0x320
	lapicTimerInitialCount = 0x380
	lapicTimerCurrentCount = 0x390
	lapicTimerDivideConfig = 0x3E0
)

const localApicBaseSymbol = "_wrela_local_apic_base"

func localApicBaseDataObject() ir.DataObject {
	return ir.DataObject{Symbol: localApicBaseSymbol, Bytes: make([]byte, 8), Align: 8}
}

func emitStoreLocalApicBase(e *Emitter, value asm.Reg) {
	emitMovDataAddressToReg(e, asm.MustLookup("rax"), localApicBaseSymbol)
	emitStoreMemFromReg(e, asm.MustLookup("rax"), 0, value, 64)
}

func emitLoadLocalApicBase(e *Emitter, dst asm.Reg) {
	emitMovDataAddressToReg(e, asm.MustLookup("rax"), localApicBaseSymbol)
	emitLoadMemToReg(e, dst, asm.MustLookup("rax"), 0, 64)
}
