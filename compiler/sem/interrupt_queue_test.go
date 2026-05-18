package sem

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
)

func TestInterruptQueueRequiresExplicitOverflowPolicy(t *testing.T) {
	_, ds := checkUEFIModulesWithExtraSource(t, "missing-overflow-policy.wrela", interruptQueueSource(`
        let queue_slots = console_memory.reserve_array(SerialPathInterrupt, count = 4)
        let queue = InterruptQueue<SerialPathInterrupt>(
            identity = QueueIdentity(label = "irq.serial.rx"),
            owner = ExecutorSlot(id = 0),
            slots = queue_slots,
            capacity = 4,
            payload = InterruptPayloadKind(kind = 1, size = sizeof(SerialPathInterrupt), align = alignof(SerialPathInterrupt)),
            head = 0,
            tail = 0,
            overflowed = false
        )
`))
	if !hasCode(ds, diag.SEM0060) {
		t.Fatalf("expected SEM0060, got %#v", ds)
	}
}

func TestInterruptQueueSourceShapeIsGeneric(t *testing.T) {
	checked, ds := checkUEFIModulesWithExtraSource(t, "interrupt-queue-shape.wrela", interruptQueueSource(`
        let queue_slots = console_memory.reserve_array(SerialPathInterrupt, count = 4)
        let queue = InterruptQueue<SerialPathInterrupt>(
            identity = QueueIdentity(label = "irq.serial.rx"),
            owner = ExecutorSlot(id = 0),
            slots = queue_slots,
            capacity = 4,
            payload = InterruptPayloadKind(kind = 1, size = sizeof(SerialPathInterrupt), align = alignof(SerialPathInterrupt)),
            overflow = InterruptOverflowPolicy(mode = 2),
            head = 0,
            tail = 0,
            overflowed = false
        )
`))
	if len(ds) != 0 {
		t.Fatalf("interrupt queue diagnostics: %#v", ds)
	}
	queue := moduleType(t, checked.Index, "machine.x86_64.interrupt_queue", "InterruptQueue")
	if len(queue.TypeParams) != 1 || queue.TypeParams[0].Name != "T" {
		t.Fatalf("InterruptQueue type params = %#v, want T", queue.TypeParams)
	}
	assertTypeFields(t, queue, map[string]string{
		"identity":   "QueueIdentity",
		"owner":      "ExecutorSlot",
		"slots":      "Slots<T>",
		"capacity":   "U64",
		"payload":    "InterruptPayloadKind",
		"overflow":   "InterruptOverflowPolicy",
		"head":       "U64",
		"tail":       "U64",
		"overflowed": "Bool",
	})
}

func TestInterruptQueueRecordsImageGraphNode(t *testing.T) {
	checked, ds := checkUEFIModulesWithExtraSource(t, "interrupt-queue-good.wrela", interruptQueueSource(`
        let queue_slots = console_memory.reserve_array(SerialPathInterrupt, count = 4)
        let queue = InterruptQueue<SerialPathInterrupt>(
            identity = QueueIdentity(label = "irq.serial.rx"),
            owner = ExecutorSlot(id = 0),
            slots = queue_slots,
            capacity = 4,
            payload = InterruptPayloadKind(kind = 1, size = sizeof(SerialPathInterrupt), align = alignof(SerialPathInterrupt)),
            overflow = InterruptOverflowPolicy(mode = 2),
            head = 0,
            tail = 0,
            overflowed = false
        )
`))
	if len(ds) != 0 {
		t.Fatalf("interrupt queue diagnostics: %#v", ds)
	}
	if len(checked.ImageGraph.InterruptQueues) != 1 {
		t.Fatalf("interrupt queues = %#v", checked.ImageGraph.InterruptQueues)
	}
	queue := checked.ImageGraph.InterruptQueues[0]
	if queue.Label != "irq.serial.rx" || queue.Owner != "executor_slot.0" || queue.Capacity != 4 || queue.PayloadKind != "kind:1" || queue.PayloadSize != 2 || queue.PayloadAlign != 1 || queue.Overflow != "set_flag_and_wake" {
		t.Fatalf("interrupt queue node = %#v", queue)
	}
}

func TestInterruptQueueRejectsBackingSizeOverflow(t *testing.T) {
	_, ds := checkUEFIModulesWithExtraSource(t, "interrupt-queue-overflow.wrela", interruptQueueSource(`
        let queue_slots = console_memory.reserve_array(SerialPathInterrupt, count = 1)
        let queue = InterruptQueue<SerialPathInterrupt>(
            identity = QueueIdentity(label = "irq.serial.rx"),
            owner = ExecutorSlot(id = 0),
            slots = queue_slots,
            capacity = 0x8000000000000000,
            payload = InterruptPayloadKind(kind = 1, size = sizeof(SerialPathInterrupt), align = alignof(SerialPathInterrupt)),
            overflow = InterruptOverflowPolicy(mode = 2),
            head = 0,
            tail = 0,
            overflowed = false
        )
`))
	if !hasCode(ds, diag.SEM0060) {
		t.Fatalf("expected SEM0060, got %#v", ds)
	}
}

func TestSharedInterruptAllowsMultipleSourceClaims(t *testing.T) {
	checked, ds := checkUEFIModulesWithExtraSource(t, "shared-irq-good.wrela", sharedIRQSource(`
        let route = interrupts.route_shared_irq(irq = 4, vector = InterruptVector(value = 0x40))
        route.claim_source(identity = InterruptSourceIdentity(label = "serial.rx"))
        route.claim_source(identity = InterruptSourceIdentity(label = "serial.status"))
`, "route.route"))
	if len(ds) != 0 {
		t.Fatalf("shared IRQ diagnostics: %#v", ds)
	}
	if len(checked.ImageGraph.SharedInterruptSources) != 3 {
		t.Fatalf("shared interrupt sources = %#v", checked.ImageGraph.SharedInterruptSources)
	}
}

func TestSharedInterruptClaimsHardwareRouteOnce(t *testing.T) {
	checked, ds := checkUEFIModulesWithExtraSource(t, "shared-irq-claims-once.wrela", sharedIRQSource(`
        let route = interrupts.route_shared_irq(irq = 4, vector = InterruptVector(value = 0x40))
        route.claim_source(identity = InterruptSourceIdentity(label = "serial.rx"))
        route.claim_source(identity = InterruptSourceIdentity(label = "serial.status"))
`, "route.route"))
	if len(ds) != 0 {
		t.Fatalf("shared IRQ diagnostics: %#v", ds)
	}
	isaClaims := 0
	for _, claim := range checked.ImageGraph.HardwareClaims {
		if claim.Kind == "isa_irq" && claim.Key == "4" {
			isaClaims++
		}
	}
	if isaClaims != 1 {
		t.Fatalf("isa irq claims = %d, hardware claims = %#v", isaClaims, checked.ImageGraph.HardwareClaims)
	}
}

func TestSharedInterruptDuplicateSourceRejected(t *testing.T) {
	_, ds := checkUEFIModulesWithExtraSource(t, "shared-irq-duplicate.wrela", sharedIRQSource(`
        let route = interrupts.route_shared_irq(irq = 4, vector = InterruptVector(value = 0x40))
        route.claim_source(identity = InterruptSourceIdentity(label = "serial.rx"))
        route.claim_source(identity = InterruptSourceIdentity(label = "serial.rx"))
`, "route.route"))
	if !hasCode(ds, diag.SEM0062) {
		t.Fatalf("expected SEM0062, got %#v", ds)
	}
}

func interruptQueueSource(queueSetup string) string {
	return `
module platform.test_interrupt_queue
use { BootPanic } from platform.hardware.panic
use { PlatformDiscoveryRoot } from platform.hardware.discovery
use { PhysicalRegionAuthority, ArenaIdentity, ArenaPolicy } from platform.hardware.memory
use { DelegatedHardware } from platform.uefi.transition
use { CpuFeatureFacts, OwnedHardware, OwnedMemory, IoPortAuthority, MemoryPlan, CpuPlan } from machine.x86_64.cpu_state
use { HardwarePlan, InterruptRoutingPlan, ClaimedPciPlanBuilder } from machine.x86_64.cpu_state
use { ExecutorSlot } from machine.x86_64.executor_slot
use { MutableBytes, Bytes } from machine.x86_64.executor_memory
use { QueueIdentity, InterruptPayloadKind, InterruptOverflowPolicy, InterruptQueue } from machine.x86_64.interrupt_queue
use { InterruptSourceIdentity, InterruptVector } from machine.x86_64.interrupts
use { SerialPathInterrupt } from machine.x86_64.serial

image InterruptQueueTest {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        let panic = BootPanic()
        let discovery = PlatformDiscoveryRoot(panic = panic).from_uefi(hardware = hardware)
        let region = PhysicalRegionAuthority(base = 0x100000, length = 0x200000, align = 4096, provenance = 1, panic = panic)
        let root = region.create_arena(identity = ArenaIdentity(label = "root"), policy = ArenaPolicy(evict_cache_by_default = false))
        let cpus = discovery.acpi.require_madt().enabled_cpus().require_count(count = 2)
        let console_slot_seed = ExecutorSlot(id = 0)
        let worker_slot_seed = ExecutorSlot(id = 1)
        let console_memory = root.executor_memory(owner = console_slot_seed, length = 0x80000, align = 4096)
        let worker_memory = root.executor_memory(owner = worker_slot_seed, length = 0x80000, align = 4096)
        let interrupts = discovery.interrupts
        let serial_route = interrupts.route_shared_irq(irq = 4, vector = InterruptVector(value = 0x40))
        let serial_source = serial_route.claim_source(identity = InterruptSourceIdentity(label = "serial.rx"))
` + queueSetup + `
        let arena = MutableBytes(address = 0, length = 0)
        let memory_plan = MemoryPlan(
            owned_memory = OwnedMemory(arena = arena),
            executor_arena = MutableBytes(address = 0, length = 0),
            io_ports = IoPortAuthority()
        )
        let cpu_plan = CpuPlan(
            owned_stack_top = 0,
            gdt_descriptor = Bytes(address = 0, length = 0),
            idt_descriptor = Bytes(address = 0, length = 0),
            cr3 = 0
        )
        let hardware_plan = HardwarePlan(
            cpus = cpus,
            interrupts = InterruptRoutingPlan(
                local_apic = interrupts.local_apic,
                serial_irq4 = serial_route.route,
                serial_shared_irq4 = serial_route,
                serial_irq_source = serial_source
            ),
            pci = ClaimedPciPlanBuilder(panic = panic).empty(),
            timer = discovery.timers.require_periodic(period_us = 1000),
            serial_irq_queue = queue,
            console_memory = console_memory,
            worker_memory = worker_memory,
            wake_strategy = discovery.cpus.wake_strategy(features = CpuFeatureFacts(monitor_mwait_available = true))
        )
        return hardware.exit_to_owned_hardware(memory_plan = memory_plan, cpu_plan = cpu_plan, hardware_plan = hardware_plan)
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        while true {}
    }
}
`
}

func sharedIRQSource(claims, serialRoute string) string {
	return `
module examples.shared_irq_test
use { BootPanic } from platform.hardware.panic
use { PlatformDiscoveryRoot } from platform.hardware.discovery
use { DelegatedHardware } from platform.uefi.transition
use { CpuFeatureFacts, OwnedHardware, OwnedMemory, IoPortAuthority, MemoryPlan, CpuPlan } from machine.x86_64.cpu_state
use { HardwarePlan, InterruptRoutingPlan, ClaimedPciPlanBuilder } from machine.x86_64.cpu_state
use { MutableBytes, Bytes } from machine.x86_64.executor_memory
use { ExecutorSlot } from machine.x86_64.executor_slot
use { QueueIdentity, InterruptPayloadKind, InterruptOverflowPolicy } from machine.x86_64.interrupt_queue
use { ArenaIdentity, ArenaPolicy } from platform.hardware.memory
use { InterruptSourceIdentity, InterruptVector } from machine.x86_64.interrupts

image SharedIrqTest {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        let panic = BootPanic()
        let discovery = PlatformDiscoveryRoot(panic = panic).from_uefi(hardware = hardware)
        let interrupts = discovery.interrupts
        let root_region = discovery.memory.require_usable_region(min_base = 0x200000, length = 0x400000, align = 4096)
        let root = root_region.create_arena(identity = ArenaIdentity(label = "shared.irq.root"), policy = ArenaPolicy(evict_cache_by_default = true))
        let cpus = discovery.acpi.require_madt().enabled_cpus().require_count(count = 2)
        let console_slot_seed = ExecutorSlot(id = 0)
        let worker_slot_seed = ExecutorSlot(id = 1)
        let console_memory = root.executor_memory(owner = console_slot_seed, length = 0x100000, align = 4096)
        let worker_memory = root.executor_memory(owner = worker_slot_seed, length = 0x100000, align = 4096)
        let serial_queue = root.interrupt_queue(identity = QueueIdentity(label = "irq.serial.rx"), owner = console_slot_seed, capacity = 64, payload = InterruptPayloadKind(kind = 1, size = 8, align = 8), overflow = InterruptOverflowPolicy(mode = 0))
        let plan_route = interrupts.route_shared_irq(irq = 6, vector = InterruptVector(value = 0x46))
        let plan_source = plan_route.claim_source(identity = InterruptSourceIdentity(label = "serial.plan"))
` + claims + `
        let arena = MutableBytes(address = 0, length = 0)
        let memory_plan = MemoryPlan(
            owned_memory = OwnedMemory(arena = arena),
            executor_arena = MutableBytes(address = 0, length = 0),
            io_ports = IoPortAuthority()
        )
        let cpu_plan = CpuPlan(
            owned_stack_top = 0,
            gdt_descriptor = Bytes(address = 0, length = 0),
            idt_descriptor = Bytes(address = 0, length = 0),
            cr3 = 0
        )
        let hardware_plan = HardwarePlan(
            cpus = cpus,
            interrupts = InterruptRoutingPlan(
                local_apic = interrupts.local_apic,
                serial_irq4 = ` + serialRoute + `,
                serial_shared_irq4 = plan_route,
                serial_irq_source = plan_source
            ),
            pci = ClaimedPciPlanBuilder(panic = panic).empty(),
            timer = discovery.timers.require_periodic(period_us = 1000),
            serial_irq_queue = serial_queue,
            console_memory = console_memory,
            worker_memory = worker_memory,
            wake_strategy = discovery.cpus.wake_strategy(features = CpuFeatureFacts(monitor_mwait_available = true))
        )
        return hardware.exit_to_owned_hardware(memory_plan = memory_plan, cpu_plan = cpu_plan, hardware_plan = hardware_plan)
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        while true {}
    }
}
`
}
