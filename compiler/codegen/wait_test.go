package codegen

import (
	"bytes"
	"testing"
)

func TestWaitFallbackEmitsHlt(t *testing.T) {
	unit := compileWaitFallbackUnitForTest()
	if !bytes.Contains(unit.Bytes, []byte{0xF4}) {
		t.Fatalf("fallback wait must emit hlt: %x", unit.Bytes)
	}
}

func TestMonitorMwaitBytesAreAvailable(t *testing.T) {
	unit := compileMonitorMwaitUnitForTest()
	for _, want := range [][]byte{
		{0x0F, 0x01, 0xC8},
		{0x0F, 0x01, 0xC9},
	} {
		if !bytes.Contains(unit.Bytes, want) {
			t.Fatalf("monitor/mwait unit missing %x in %x", want, unit.Bytes)
		}
	}
}
