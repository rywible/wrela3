package sem

import (
	"strings"
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
)

const placementSource = `
module examples.placement_good
use { BootPanic } from platform.hardware.panic
use { PlatformDiscoveryRoot } from platform.hardware.discovery
use { DelegatedHardware } from platform.uefi.transition
use { CpuFeatureFacts, OwnedHardware, OwnedMemory, IoPortAuthority, MemoryPlan, CpuPlan, HardwarePlan, InterruptRoutingPlan, ClaimedPciPlanBuilder, ExecutorRegistry, SlotIdentity } from machine.x86_64.cpu_state
use { ExecutorSlot } from machine.x86_64.executor_slot
use { MutableBytes, Bytes } from machine.x86_64.executor_memory
use { HotPollPolicy } from machine.x86_64.executor_loop
use { InterruptSourceIdentity, InterruptVector } from machine.x86_64.interrupts
use { InterruptOverflowPolicy, InterruptQueue, QueueIdentity } from machine.x86_64.interrupt_queue
use { ArenaIdentity, ArenaPolicy } from platform.hardware.memory
use { SerialPathInterrupt } from machine.x86_64.topic_payload
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
        let root_region = discovery.memory.require_usable_region(min_base = 0x200000, length = 0x400000, align = 4096)
        let root = root_region.create_arena(identity = ArenaIdentity(label = "placement.root"), policy = ArenaPolicy(evict_cache_by_default = true))
        let console_seed = ExecutorSlot(id = 0)
        let worker_seed = ExecutorSlot(id = 1)
        let console_memory = root.executor_memory(owner = console_seed, length = 0x100000, align = 4096)
        let worker_memory = root.executor_memory(owner = worker_seed, length = 0x100000, align = 4096)
        let shared = discovery.interrupts.route_shared_irq(irq = 4, vector = InterruptVector(value = 0x40))
        let queue_slots = console_memory.reserve_array(SerialPathInterrupt, count = 64)
        let queue = InterruptQueue<SerialPathInterrupt>(identity = QueueIdentity(label = "irq.serial.rx"), owner = console_seed, slots = queue_slots, capacity = 64, overflow = InterruptOverflowPolicy(mode = 0), head = 0, tail = 0, overflowed = false)
        let arena = MutableBytes(address = 0, length = 0)
        return hardware.exit_to_owned_hardware(memory_plan = MemoryPlan(owned_memory = OwnedMemory(arena = arena), executor_arena = arena, io_ports = IoPortAuthority()), cpu_plan = CpuPlan(owned_stack_top = 0, gdt_descriptor = Bytes(address = 0, length = 0), idt_descriptor = Bytes(address = 0, length = 0), cr3 = 0), hardware_plan = HardwarePlan(cpus = topology, interrupts = InterruptRoutingPlan(local_apic = discovery.interrupts.local_apic, serial_irq4 = shared.route, serial_shared_irq4 = shared, serial_irq_source = shared.claim_source(identity = InterruptSourceIdentity(label = "serial.rx"))), pci = ClaimedPciPlanBuilder(panic = panic).empty(), timer = discovery.timers.require_periodic(period_us = 1000), serial_irq_queue = queue, console_memory = console_memory, worker_memory = worker_memory, wake_strategy = discovery.cpus.wake_strategy(features = CpuFeatureFacts(monitor_mwait_available = true))))
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
	if !required.Satisfied {
		t.Fatalf("required placement constraint should be proven by distinct vCPU placements = %#v", required)
	}
	if len(checked.ImageGraph.PlacementDecisions) != 1 {
		t.Fatalf("placement decisions = %#v", checked.ImageGraph.PlacementDecisions)
	}
	decision := checked.ImageGraph.PlacementDecisions[0]
	if decision.SlotLabel != "worker" || decision.Target != "cpu" || decision.Satisfied || decision.Fallback != "unknown_locality" {
		t.Fatalf("placement decision = %#v", decision)
	}
}

const placementSameSlotSource = `
module examples.placement_same_slot
use { BootPanic } from platform.hardware.panic
use { PlatformDiscoveryRoot } from platform.hardware.discovery
use { DelegatedHardware } from platform.uefi.transition
use { CpuFeatureFacts, OwnedHardware, OwnedMemory, IoPortAuthority, MemoryPlan, CpuPlan, HardwarePlan, InterruptRoutingPlan, ClaimedPciPlanBuilder, SlotIdentity } from machine.x86_64.cpu_state
use { ExecutorSlot } from machine.x86_64.executor_slot
use { MutableBytes, Bytes } from machine.x86_64.executor_memory
use { HotPollPolicy } from machine.x86_64.executor_loop
use { InterruptSourceIdentity, InterruptVector } from machine.x86_64.interrupts
use { InterruptOverflowPolicy, InterruptQueue, QueueIdentity } from machine.x86_64.interrupt_queue
use { ArenaIdentity, ArenaPolicy } from platform.hardware.memory
use { SerialPathInterrupt } from machine.x86_64.topic_payload
executor Worker {
    slot: ExecutorSlot
    loop: HotPollPolicy
    start fn run(self) -> never { while true {} }
}
image PlacementSameSlot {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        let panic = BootPanic()
        let discovery = PlatformDiscoveryRoot(panic = panic).from_uefi(hardware = hardware)
        let topology = discovery.cpus.require_min_count(count = 2)
        let root_region = discovery.memory.require_usable_region(min_base = 0x200000, length = 0x400000, align = 4096)
        let root = root_region.create_arena(identity = ArenaIdentity(label = "placement.root"), policy = ArenaPolicy(evict_cache_by_default = true))
        let console_seed = ExecutorSlot(id = 0)
        let worker_seed = ExecutorSlot(id = 1)
        let console_memory = root.executor_memory(owner = console_seed, length = 0x100000, align = 4096)
        let worker_memory = root.executor_memory(owner = worker_seed, length = 0x100000, align = 4096)
        let shared = discovery.interrupts.route_shared_irq(irq = 4, vector = InterruptVector(value = 0x40))
        let queue_slots = console_memory.reserve_array(SerialPathInterrupt, count = 64)
        let queue = InterruptQueue<SerialPathInterrupt>(identity = QueueIdentity(label = "irq.serial.rx"), owner = console_seed, slots = queue_slots, capacity = 64, overflow = InterruptOverflowPolicy(mode = 0), head = 0, tail = 0, overflowed = false)
        let arena = MutableBytes(address = 0, length = 0)
        return hardware.exit_to_owned_hardware(memory_plan = MemoryPlan(owned_memory = OwnedMemory(arena = arena), executor_arena = arena, io_ports = IoPortAuthority()), cpu_plan = CpuPlan(owned_stack_top = 0, gdt_descriptor = Bytes(address = 0, length = 0), idt_descriptor = Bytes(address = 0, length = 0), cr3 = 0), hardware_plan = HardwarePlan(cpus = topology, interrupts = InterruptRoutingPlan(local_apic = discovery.interrupts.local_apic, serial_irq4 = shared.route, serial_shared_irq4 = shared, serial_irq_source = shared.claim_source(identity = InterruptSourceIdentity(label = "serial.rx"))), pci = ClaimedPciPlanBuilder(panic = panic).empty(), timer = discovery.timers.require_periodic(period_us = 1000), serial_irq_queue = queue, console_memory = console_memory, worker_memory = worker_memory, wake_strategy = discovery.cpus.wake_strategy(features = CpuFeatureFacts(monitor_mwait_available = true))))
    }
    phase owned_hardware(hardware: OwnedHardware) -> never {
        let panic = BootPanic()
        let placement = hardware.hardware_plan.cpus.placement(panic = panic)
        let console = hardware.executors.claim(identity = SlotIdentity(label = "console"))
        placement.require_separate_physical_cores(a = console, b = console)
        let worker_exec = Worker(slot = console, loop = HotPollPolicy())
        hardware.vcpu0.enter(executor = worker_exec)
    }
}
`

func TestPlacementConstraintSeparateSameSlotIsNotSatisfied(t *testing.T) {
	checked, ds := checkUEFIModulesWithExtraSource(t, "placement-same-slot.wrela", placementSameSlotSource)
	if len(ds) != 0 {
		t.Fatalf("diagnostics: %#v", ds)
	}
	if len(checked.ImageGraph.PlacementConstraints) != 1 {
		t.Fatalf("placement constraints = %#v", checked.ImageGraph.PlacementConstraints)
	}
	constraint := checked.ImageGraph.PlacementConstraints[0]
	if constraint.Kind != "separate_physical_cores" || !constraint.Required {
		t.Fatalf("unexpected placement constraint = %#v", constraint)
	}
	if constraint.A == "" || constraint.B == "" {
		t.Fatalf("expected resolved slot labels, got %#v", constraint)
	}
	if constraint.A != constraint.B {
		t.Fatalf("expected identical slot labels in fixture, got %#v", constraint)
	}
	if constraint.Satisfied {
		t.Fatalf("same-slot constraint must not be satisfied = %#v", constraint)
	}
	if constraint.Fallback != "boot_fatal" {
		t.Fatalf("unsatisfied required placement should report boot-fatal fallback = %#v", constraint)
	}
}

func TestPlacementConstraintRequiresDistinctVcpuEvidence(t *testing.T) {
	constraint := PlacementConstraintNode{
		Kind:     "separate_physical_cores",
		A:        "console",
		B:        "worker",
		Required: true,
	}
	if placementConstraintSatisfied(constraint, map[string]int{"console": 0}) {
		t.Fatalf("missing worker vCPU placement must not satisfy constraint")
	}
	if placementConstraintSatisfied(constraint, map[string]int{"console": 0, "worker": 0}) {
		t.Fatalf("same vCPU placement must not satisfy constraint")
	}
	if !placementConstraintSatisfied(constraint, map[string]int{"console": 0, "worker": 1}) {
		t.Fatalf("distinct vCPU placements should satisfy constraint")
	}
}

func TestPlacementRuntimeChecksSlotIdentity(t *testing.T) {
	source := readRepoFile(t, "wrela/machine/x86_64/cpu_state.wrela")
	method := methodSourceForTypeFromClassDecl(source, "CpuPlacementPlan", "require_separate_physical_cores")
	if method == "" {
		t.Fatalf("missing CpuPlacementPlan.require_separate_physical_cores")
	}
	if !strings.Contains(method, "if a.id == b.id") {
		t.Fatalf("runtime placement check must reject identical executor slots: %s", method)
	}
	if !strings.Contains(method, "self.topology.can_prove_separate_cores() == false") {
		t.Fatalf("runtime placement check must retain topology proof guard: %s", method)
	}
}

func methodSourceForTypeFromClassDecl(sourceText, typeName, methodName string) string {
	typeHeader := "class " + typeName
	typeStart := strings.Index(sourceText, typeHeader)
	if typeStart < 0 {
		return ""
	}
	methodNameMarker := "fn " + methodName + "("
	methodStart := strings.Index(sourceText[typeStart:], methodNameMarker)
	if methodStart < 0 {
		return ""
	}
	methodStart += typeStart
	bodyStart := strings.Index(sourceText[methodStart:], "{")
	if bodyStart < 0 {
		return ""
	}
	bodyStart += methodStart

	depth := 0
	for i := bodyStart; i < len(sourceText); i++ {
		switch sourceText[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return sourceText[methodStart : i+1]
			}
		}
	}
	return ""
}

func TestHiddenSchedulerConstructRejected(t *testing.T) {
	_, ds := checkModuleForTest(t, hiddenSchedulerSource)
	if !hasCode(ds, diag.SEM0067) {
		t.Fatalf("expected SEM0067, got %#v", ds)
	}
}
