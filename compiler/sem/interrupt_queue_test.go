package sem

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
)

func TestInterruptQueueRequiresExplicitOverflowPolicy(t *testing.T) {
	_, ds := checkUEFIModulesWithExtraSource(t, "missing-overflow-policy.wrela", interruptQueueSource(`
        let queue = root.interrupt_queue(
            identity = QueueIdentity(label = "irq.serial.rx"),
            owner = ExecutorSlot(id = 0),
            capacity = 4,
            payload = InterruptPayloadKind(kind = 1, size = 8, align = 8)
        )
`))
	if !hasCode(ds, diag.SEM0060) {
		t.Fatalf("expected SEM0060, got %#v", ds)
	}
}

func TestInterruptQueueRecordsImageGraphNode(t *testing.T) {
	checked, ds := checkUEFIModulesWithExtraSource(t, "interrupt-queue-good.wrela", interruptQueueSource(`
        let queue = root.interrupt_queue(
            identity = QueueIdentity(label = "irq.serial.rx"),
            owner = ExecutorSlot(id = 0),
            capacity = 4,
            payload = InterruptPayloadKind(kind = 1, size = 8, align = 8),
            overflow = InterruptOverflowPolicy(mode = 2)
        )
`))
	if len(ds) != 0 {
		t.Fatalf("interrupt queue diagnostics: %#v", ds)
	}
	if len(checked.ImageGraph.InterruptQueues) != 1 {
		t.Fatalf("interrupt queues = %#v", checked.ImageGraph.InterruptQueues)
	}
	queue := checked.ImageGraph.InterruptQueues[0]
	if queue.Label != "irq.serial.rx" || queue.Owner != "executor_slot.0" || queue.Capacity != 4 || queue.PayloadKind != "kind:1" || queue.PayloadSize != 8 || queue.PayloadAlign != 8 || queue.Overflow != "set_flag_and_wake" {
		t.Fatalf("interrupt queue node = %#v", queue)
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
	if len(checked.ImageGraph.SharedInterruptSources) != 2 {
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
use { OwnedHardware, OwnedMemory, IoPortAuthority, MemoryPlan, CpuPlan } from machine.x86_64.cpu_state
use { HardwarePlan, InterruptRoutingPlan, ClaimedPciPlanBuilder } from machine.x86_64.cpu_state
use { ExecutorSlot } from machine.x86_64.cpu_state
use { MutableBytes, Bytes } from machine.x86_64.executor_memory
use { QueueIdentity, InterruptPayloadKind, InterruptOverflowPolicy } from machine.x86_64.interrupt_queue
use { InterruptVector } from machine.x86_64.interrupts

image InterruptQueueTest {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        let panic = BootPanic()
        let discovery = PlatformDiscoveryRoot(panic = panic).from_uefi(hardware = hardware)
        let region = PhysicalRegionAuthority(base = 0x100000, length = 0x200000, align = 4096, provenance = 1, panic = panic)
        let root = region.create_arena(identity = ArenaIdentity(label = "root"), policy = ArenaPolicy(evict_cache_by_default = false))
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
        let interrupts = discovery.interrupts
        let hardware_plan = HardwarePlan(
            cpus = discovery.acpi.require_madt().enabled_cpus().require_count(count = 2),
            interrupts = InterruptRoutingPlan(
                local_apic = interrupts.local_apic,
                serial_irq4 = interrupts.route_isa_irq(irq = 4, vector = InterruptVector(value = 0x40))
            ),
            pci = ClaimedPciPlanBuilder(panic = panic).empty()
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
use { OwnedHardware, OwnedMemory, IoPortAuthority, MemoryPlan, CpuPlan } from machine.x86_64.cpu_state
use { HardwarePlan, InterruptRoutingPlan, ClaimedPciPlanBuilder } from machine.x86_64.cpu_state
use { MutableBytes, Bytes } from machine.x86_64.executor_memory
use { InterruptSourceIdentity, InterruptVector } from machine.x86_64.interrupts

image SharedIrqTest {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        let panic = BootPanic()
        let discovery = PlatformDiscoveryRoot(panic = panic).from_uefi(hardware = hardware)
        let interrupts = discovery.interrupts
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
            cpus = discovery.acpi.require_madt().enabled_cpus().require_count(count = 2),
            interrupts = InterruptRoutingPlan(
                local_apic = interrupts.local_apic,
                serial_irq4 = ` + serialRoute + `
            ),
            pci = ClaimedPciPlanBuilder(panic = panic).empty()
        )
        return hardware.exit_to_owned_hardware(memory_plan = memory_plan, cpu_plan = cpu_plan, hardware_plan = hardware_plan)
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        while true {}
    }
}
`
}
