package sem

import (
	"strings"
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
)

const overlappingArenaSource = `
module examples.bad_arena_overlap
use { BootPanic } from platform.hardware.panic
use { ArenaIdentity, ArenaPolicy, PhysicalRegionAuthority } from platform.hardware.memory

class GoodRoot {
    fn build(self) {
        let region = PhysicalRegionAuthority(base = 0x200000, length = 0x10000, align = 4096, provenance = 1, panic = BootPanic())
        let root = region.create_arena(identity = ArenaIdentity(label = "root"), policy = ArenaPolicy(evict_cache_by_default = true))
        let a = root.child_at(identity = ArenaIdentity(label = "a"), offset = 0, length = 8192, align = 4096)
        let b = root.child_at(identity = ArenaIdentity(label = "b"), offset = 4096, length = 8192, align = 4096)
    }
}`

func TestArenaGraphRejectsStaticOverlap(t *testing.T) {
	_, ds := checkTrustedPlatformSourceForTest(t, "platform.test.bad_arena_overlap", overlappingArenaSource)
	if !hasCode(ds, diag.SEM0058) {
		t.Fatalf("expected SEM0058, got %#v", ds)
	}
}

func TestArenaGraphRejectsDuplicateIdentity(t *testing.T) {
	src := strings.ReplaceAll(overlappingArenaSource, `label = "b"`, `label = "a"`)
	_, ds := checkTrustedPlatformSourceForTest(t, "platform.test.duplicate_arena", src)
	if !hasCode(ds, diag.SEM0057) {
		t.Fatalf("expected SEM0057, got %#v", ds)
	}
}

const executorMemoryNearSource = `
module examples.executor_memory_near
use { BootPanic } from platform.hardware.panic
use { PlatformDiscoveryRoot } from platform.hardware.discovery
use { DelegatedHardware } from platform.uefi.transition
use { ArenaIdentity, ArenaPolicy } from platform.hardware.memory
use { OwnedHardware, OwnedMemory, IoPortAuthority, MemoryPlan, CpuPlan, HardwarePlan, InterruptRoutingPlan, ClaimedPciPlanBuilder, ExecutorSlot } from machine.x86_64.cpu_state
use { MutableBytes, Bytes } from machine.x86_64.executor_memory
use { InterruptVector } from machine.x86_64.interrupts
image MemoryNearImage {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        let panic = BootPanic()
        let discovery = PlatformDiscoveryRoot(panic = panic).from_uefi(hardware = hardware)
        let root_region = discovery.memory.require_usable_region(min_base = 0x200000, length = 0x1000000, align = 4096)
        let root = root_region.create_arena(identity = ArenaIdentity(label = "root"), policy = ArenaPolicy(evict_cache_by_default = true))
        let placement = discovery.cpus.require_min_count(count = 2).placement(panic = panic)
        let memory = root.executor_memory_near(owner = ExecutorSlot(id = 0), near = placement.cpu_for(slot = ExecutorSlot(id = 0)), length = 0x200000, align = 4096)
        let compat = root_region.bytes()
        return hardware.exit_to_owned_hardware(memory_plan = MemoryPlan(owned_memory = OwnedMemory(arena = compat), executor_arena = compat, io_ports = IoPortAuthority()), cpu_plan = CpuPlan(owned_stack_top = 0, gdt_descriptor = Bytes(address = 0, length = 0), idt_descriptor = Bytes(address = 0, length = 0), cr3 = 0), hardware_plan = HardwarePlan(cpus = discovery.cpus.require_min_count(count = 2), interrupts = InterruptRoutingPlan(local_apic = discovery.interrupts.local_apic, serial_irq4 = discovery.interrupts.route_isa_irq(irq = 4, vector = InterruptVector(value = 0x40))), pci = ClaimedPciPlanBuilder(panic = panic).empty()))
    }
    phase owned_hardware(hardware: OwnedHardware) -> never { while true {} }
}
`

func TestExecutorMemoryNearRecordsFallback(t *testing.T) {
	checked, ds := checkUEFIModulesWithExtraSource(t, "memory-near.wrela", executorMemoryNearSource)
	if len(ds) != 0 {
		t.Fatalf("diagnostics: %#v", ds)
	}
	r := BuildImageReport(checked)
	if len(r.Memory.ExecutorBudgets) == 0 {
		t.Fatalf("executor memory budget missing: %#v", r.Memory)
	}
	budget := r.Memory.ExecutorBudgets[0]
	if budget.SlotLabel != "executor_slot.0" || budget.Bytes != 0x200000 {
		t.Fatalf("executor memory budget = %#v", budget)
	}
	if !containsPlacementFallback(r.Runtime.Placement, "unknown_locality") {
		t.Fatalf("missing unknown-locality fallback: %#v", r.Runtime.Placement)
	}
}
