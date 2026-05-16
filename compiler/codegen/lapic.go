package codegen

import (
	"github.com/ryanwible/wrela3/compiler/asm"
	"github.com/ryanwible/wrela3/compiler/ir"
)

const (
	lapicSVR     = 0xF0
	lapicICRLow  = 0x300
	lapicICRHigh = 0x310
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
