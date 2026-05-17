package codegen

import (
	"bytes"
	"testing"

	"github.com/ryanwible/wrela3/compiler/asm"
)

func compileX2APICWriteUnitForTest(msr uint32, value uint64) compiledUnit {
	e := &Emitter{Labels: map[string]int{}, ctx: compileContext{}}
	emitMovImmToReg(e, asm.MustLookup("r10"), int64(value))
	emitX2APICWriteMSR(e, msr, asm.MustLookup("r10"))
	return compiledUnit{Symbol: "x2apic_write_test", Bytes: e.Code}
}

func TestX2APICWriteUsesWrmsr(t *testing.T) {
	unit := compileX2APICWriteUnitForTest(0x80B, 0x0100000000000040)
	for _, want := range [][]byte{
		{0x0F, 0x30},
	} {
		if !bytes.Contains(unit.Bytes, want) {
			t.Fatalf("x2APIC write missing %x in %x", want, unit.Bytes)
		}
	}
	for _, want := range [][]byte{
		{0xB9, 0x0B, 0x08, 0x00, 0x00},
		{0x49, 0x8B, 0xC2},
		{0x49, 0x8B, 0xD2},
		{0x48, 0xC1, 0xEA, 0x20},
	} {
		if !bytes.Contains(unit.Bytes, want) {
			t.Fatalf("x2APIC write missing setup %x in %x", want, unit.Bytes)
		}
	}
}
