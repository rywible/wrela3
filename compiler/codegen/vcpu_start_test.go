package codegen

import (
	"bytes"
	"testing"

	"github.com/ryanwible/wrela3/compiler/ir"
)

func TestVcpuStartupDataSymbolsAreDeterministic(t *testing.T) {
	program := &ir.Program{
		VcpuStarts: []ir.VcpuStartPlan{
			{VcpuID: 1, SlotLabel: "worker", Terminal: false},
			{VcpuID: 0, SlotLabel: "hello", Terminal: true},
		},
	}

	objects := vcpuStartupData(program)

	wantSymbols := []string{
		"_wrela_ap_trampoline_blob",
		"_wrela_vcpu0_ready",
		"_wrela_vcpu0_entry",
		"_wrela_vcpu0_stack_top",
		"_wrela_vcpu0_context",
		"_wrela_vcpu1_ready",
		"_wrela_vcpu1_entry",
		"_wrela_vcpu1_stack_top",
		"_wrela_vcpu1_context",
	}
	if len(objects) != len(wantSymbols) {
		t.Fatalf("len(vcpuStartupData) = %d, want %d", len(objects), len(wantSymbols))
	}
	for i, obj := range objects {
		if obj.Symbol != wantSymbols[i] {
			t.Fatalf("object %d symbol = %q, want %q", i, obj.Symbol, wantSymbols[i])
		}
		if obj.Symbol == "_wrela_ap_trampoline_blob" {
			if obj.Align != 4096 {
				t.Fatalf("object %s Align = %d, want 4096", obj.Symbol, obj.Align)
			}
			continue
		}
		if obj.Align != 64 {
			t.Fatalf("object %s Align = %d, want 64", obj.Symbol, obj.Align)
		}
	}
}

func TestAPTrampolineBlobContract(t *testing.T) {
	blob := apTrampolineBlob()
	if len(blob) > 4096 {
		t.Fatalf("AP trampoline must fit in one 4KiB SIPI page, got %d bytes", len(blob))
	}
	for _, want := range [][]byte{
		{0xFA},       // cli
		{0x0F, 0x22}, // mov to control register shape
		{0x0F, 0x30}, // wrmsr
		{0xF4},       // hlt fallback
	} {
		if !bytes.Contains(blob, want) {
			t.Fatalf("trampoline missing byte shape %x in %x", want, blob)
		}
	}
}
