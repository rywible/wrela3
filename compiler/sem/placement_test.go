package sem

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
)

const placementSource = `
module examples.placement_good
use { BootPanic } from platform.hardware.panic
use { PlatformDiscoveryRoot } from platform.hardware.discovery
use { DelegatedHardware } from platform.uefi.transition
use { OwnedHardware, OwnedMemory, IoPortAuthority, MemoryPlan, CpuPlan, HardwarePlan, InterruptRoutingPlan, ClaimedPciPlanBuilder, ExecutorRegistry, SlotIdentity } from machine.x86_64.cpu_state
use { ExecutorSlot } from machine.x86_64.cpu_state
use { MutableBytes, Bytes } from machine.x86_64.executor_memory
use { HotPollPolicy } from machine.x86_64.executor_loop
use { InterruptVector } from machine.x86_64.interrupts
executor Worker {
    slot: ExecutorSlot
    loop: HotPollPolicy
    start fn run(self) -> never { while true {} }
}
image PlacementGood {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        let panic = BootPanic()
        let discovery = PlatformDiscoveryRoot(panic = panic).from_uefi(hardware = hardware)
        let topology = discovery.cpus.require_min_count(count = 2)
        let arena = MutableBytes(address = 0, length = 0)
        return hardware.exit_to_owned_hardware(memory_plan = MemoryPlan(owned_memory = OwnedMemory(arena = arena), executor_arena = arena, io_ports = IoPortAuthority()), cpu_plan = CpuPlan(owned_stack_top = 0, gdt_descriptor = Bytes(address = 0, length = 0), idt_descriptor = Bytes(address = 0, length = 0), cr3 = 0), hardware_plan = HardwarePlan(cpus = topology, interrupts = InterruptRoutingPlan(local_apic = discovery.interrupts.local_apic, serial_irq4 = discovery.interrupts.route_isa_irq(irq = 4, vector = InterruptVector(value = 0x40))), pci = ClaimedPciPlanBuilder(panic = panic).empty()))
    }
    phase owned_hardware(hardware: OwnedHardware) -> never {
        let panic = BootPanic()
        let placement = hardware.hardware_plan.cpus.placement(panic = panic)
        let console = hardware.executors.claim(identity = SlotIdentity(label = "console"))
        let worker = hardware.executors.claim(identity = SlotIdentity(label = "worker"))
        placement.require_separate_physical_cores(a = console, b = worker)
        let preferred = placement.prefer_same_cache_group(a = console, b = worker)
        let target = placement.cpu_for(slot = worker)
        let console_executor = Worker(slot = console, loop = HotPollPolicy())
        let worker_executor = Worker(slot = worker, loop = HotPollPolicy())
        hardware.vcpu1.start(executor = worker_executor)
        hardware.vcpu0.enter(executor = console_executor)
    }
}
`

const hiddenSchedulerSource = `
module examples.hidden_scheduler
class Scheduler {}
`

func TestPlacementConstraintsRecorded(t *testing.T) {
	checked, ds := checkUEFIModulesWithExtraSource(t, "placement-good.wrela", placementSource)
	if len(ds) != 0 {
		t.Fatalf("diagnostics: %#v", ds)
	}
	if len(checked.ImageGraph.PlacementConstraints) != 2 {
		t.Fatalf("placement constraints = %#v", checked.ImageGraph.PlacementConstraints)
	}
	required := checked.ImageGraph.PlacementConstraints[0]
	if required.Kind != "separate_physical_cores" || !required.Required || required.A != "console" || required.B != "worker" {
		t.Fatalf("required placement constraint = %#v", required)
	}
	preferred := checked.ImageGraph.PlacementConstraints[1]
	if preferred.Kind != "same_cache_group" || preferred.Required || preferred.Fallback != "unknown_locality" {
		t.Fatalf("preferred placement constraint = %#v", preferred)
	}
	if len(checked.ImageGraph.PlacementDecisions) != 1 {
		t.Fatalf("placement decisions = %#v", checked.ImageGraph.PlacementDecisions)
	}
	decision := checked.ImageGraph.PlacementDecisions[0]
	if decision.SlotLabel != "worker" || decision.Target != "cpu" || decision.Satisfied || decision.Fallback != "unknown_locality" {
		t.Fatalf("placement decision = %#v", decision)
	}
}

func TestHiddenSchedulerConstructRejected(t *testing.T) {
	_, ds := checkModuleForTest(t, hiddenSchedulerSource)
	if !hasCode(ds, diag.SEM0067) {
		t.Fatalf("expected SEM0067, got %#v", ds)
	}
}
