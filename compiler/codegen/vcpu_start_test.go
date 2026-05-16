package codegen

import (
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
		if obj.Align != 64 {
			t.Fatalf("object %s Align = %d, want 64", obj.Symbol, obj.Align)
		}
	}
}
