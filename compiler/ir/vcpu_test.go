package ir

import "testing"

func TestVcpuDispatchOpsDefineShape(t *testing.T) {
	start := VcpuStart{VcpuID: 1, Executor: Local{Symbol: "worker"}, SlotLabel: "worker", Type: Type{Name: "VcpuStartStatus"}}
	enter := VcpuEnter{VcpuID: 0, Executor: Local{Symbol: "main"}, SlotLabel: "main"}
	if start.VcpuID != 1 || enter.VcpuID != 0 {
		t.Fatalf("bad vcpu ids")
	}
	if _, ok := any(start).(Operation); !ok {
		t.Fatal("VcpuStart must be operation")
	}
	if len(valuesDefinedBy(start)) != 1 {
		t.Fatal("VcpuStart must define VcpuStartStatus")
	}
	if _, ok := any(enter).(Operation); !ok {
		t.Fatal("VcpuEnter must be operation")
	}
}
