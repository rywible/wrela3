package codegen

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/ir"
)

func TestAsmMethodExternalBranchRelocation(t *testing.T) {
	method := ir.AsmMethod{
		Symbol: "_wrela_method_platform_uefi_transition_DelegatedHardware_capture_vector40_serial_handler",
		Body:   "call _wrela_interrupt_vector40_serial\njmp _wrela_interrupt_vector41_edu_msi\nret",
	}

	unit, diags := compileAsmMethodUnit(method)
	if len(diags) != 0 {
		t.Fatalf("compileAsmMethodUnit() diagnostics = %#v", diags)
	}
	if len(unit.CallReloc) != 2 {
		t.Fatalf("compileAsmMethodUnit() call relocs = %#v, want 2", unit.CallReloc)
	}

	wantRelocs := []internalReloc{
		{Offset: 1, Symbol: "_wrela_interrupt_vector40_serial"},
		{Offset: 6, Symbol: "_wrela_interrupt_vector41_edu_msi"},
	}
	for i, want := range wantRelocs {
		if unit.CallReloc[i] != want {
			t.Fatalf("compileAsmMethodUnit() call reloc %d = %#v, want %#v", i, unit.CallReloc[i], want)
		}
	}

	if !containsBytes(unit.Bytes, []byte{0xE8, 0, 0, 0, 0}) {
		t.Fatalf("external call must encode as zero rel32 before relocation: %#x", unit.Bytes)
	}
	if !containsBytes(unit.Bytes, []byte{0xE9, 0, 0, 0, 0}) {
		t.Fatalf("external jmp must encode as zero rel32 before relocation: %#x", unit.Bytes)
	}
}
