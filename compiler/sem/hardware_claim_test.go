package sem

import (
	"strings"
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
)

func TestDuplicateHardwareClaimsRejected(t *testing.T) {
	_, ds := checkUEFIModulesWithExtraSource(t, "duplicate-bar-test.wrela", duplicateHardwareClaimSource)
	if !hasCode(ds, diag.SEM0050) {
		t.Fatalf("expected SEM0050, got %#v", ds)
	}
}

func TestDuplicateInterruptVectorRejected(t *testing.T) {
	_, ds := checkUEFIModulesWithExtraSource(t, "duplicate-vector-test.wrela", duplicateInterruptVectorSource)
	if !hasCode(ds, diag.SEM0050) {
		t.Fatalf("expected SEM0050, got %#v", ds)
	}
}

func TestDuplicatePciMsiClaimRejected(t *testing.T) {
	_, ds := checkUEFIModulesWithExtraSource(t, "duplicate-msi-test.wrela", duplicatePciClaimSource(`
        let first = edu.claim_msi()
        let second = edu.claim_msi()
`))
	requireOnlyDiagnostic(t, ds, diag.SEM0050, "duplicate hardware claim pci_msi:vendor=0x1234/device=0x11e8/occurrence=0")
}

func TestDuplicatePciMsixClaimRejected(t *testing.T) {
	_, ds := checkUEFIModulesWithExtraSource(t, "duplicate-msix-test.wrela", duplicatePciClaimSource(`
        let first = edu.claim_msix(table_bar_index = 1)
        let second = edu.claim_msix(table_bar_index = 2)
`))
	requireOnlyDiagnostic(t, ds, diag.SEM0050, "duplicate hardware claim pci_msix:vendor=0x1234/device=0x11e8/occurrence=0")
}

func TestDuplicateIsaIrqClaimRejected(t *testing.T) {
	_, ds := checkUEFIModulesWithExtraSource(t, "duplicate-isa-irq-test.wrela", interruptClaimSource(`
        let first = irq_authority.route_isa_irq(irq = 4, vector = InterruptVector(value = 0x40))
        let second = irq_authority.route_isa_irq(irq = 4, vector = InterruptVector(value = 0x41))
`, "first"))
	requireOnlyDiagnostic(t, ds, diag.SEM0050, "duplicate hardware claim isa_irq:4")
}

func TestHardwareClaimInterruptVectorMustBeSourceLiteral(t *testing.T) {
	_, ds := checkUEFIModulesWithExtraSource(t, "nonliteral-vector-test.wrela", interruptClaimSource(`
        let vector = InterruptVector(value = 0x40)
        let route = irq_authority.route_isa_irq(irq = 4, vector = vector)
`, "route"))
	requireOnlyDiagnostic(t, ds, diag.SEM0055, "interrupt vectors in hardware claims must be source literals")
}

func requireOnlyDiagnostic(t *testing.T, ds []diag.Diagnostic, code, message string) {
	t.Helper()
	if len(ds) != 1 {
		t.Fatalf("expected exactly one diagnostic %s containing %q, got %#v", code, message, ds)
	}
	if ds[0].Code != code || !strings.Contains(ds[0].Message, message) {
		t.Fatalf("expected %s containing %q, got %#v", code, message, ds)
	}
}

func duplicatePciClaimSource(claims string) string {
	return `
module examples.bad_duplicate_pci_claim
use { BootPanic } from platform.hardware.panic
use { PlatformDiscoveryRoot } from platform.hardware.discovery
use { DelegatedHardware } from platform.uefi.transition
use { CpuFeatureFacts, OwnedHardware, OwnedMemory, IoPortAuthority, MemoryPlan, CpuPlan } from machine.x86_64.cpu_state
use { HardwarePlan, InterruptRoutingPlan, ClaimedPciPlanBuilder } from machine.x86_64.cpu_state
use { ExecutorSlot } from machine.x86_64.executor_slot
use { MutableBytes, Bytes } from machine.x86_64.executor_memory
use { InterruptSourceIdentity, InterruptVector } from machine.x86_64.interrupts
use { InterruptOverflowPolicy, InterruptQueue, QueueIdentity } from machine.x86_64.interrupt_queue
use { ArenaIdentity, ArenaPolicy } from platform.hardware.memory
use { Option } from wrela.lang.core

image BadDuplicatePciClaim {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        let panic = BootPanic()
        let discovery = PlatformDiscoveryRoot(panic = panic).from_uefi(hardware = hardware)
        let edu = discovery.pci.require_device(vendor_id = 0x1234, device_id = 0x11e8, occurrence = 0)
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
        let interrupts = discovery.interrupts
        let root_region = discovery.memory.require_usable_region(min_base = 0x200000, length = 0x400000, align = 4096)
        let root = root_region.create_arena(identity = ArenaIdentity(label = "duplicate.pci.root"), policy = ArenaPolicy(evict_cache_by_default = true))
        let console_seed = ExecutorSlot(id = 0)
        let worker_seed = ExecutorSlot(id = 1)
        let console_memory = root.executor_memory(owner = console_seed, length = 0x100000, align = 4096)
        let worker_memory = root.executor_memory(owner = worker_seed, length = 0x100000, align = 4096)
        let serial_route = interrupts.route_shared_irq(irq = 6, vector = InterruptVector(value = 0x46))
        let serial_source = serial_route.claim_source(identity = InterruptSourceIdentity(label = "serial.rx"))
        let serial_queue_slots = console_memory.reserve_array(U8, count = 64)
        let serial_queue = InterruptQueue<U8>(identity = QueueIdentity(label = "irq.serial.rx"), owner = console_seed, slots = serial_queue_slots, capacity = 64, overflow = InterruptOverflowPolicy(mode = 0), head = 0, tail = 0, overflowed = false)
        let hardware_plan = HardwarePlan(
            cpus = discovery.acpi.require_madt().enabled_cpus().require_count(count = 2),
            interrupts = InterruptRoutingPlan(
                local_apic = interrupts.local_apic,
                serial_irq4 = serial_route.route,
                serial_shared_irq4 = serial_route,
                serial_irq_source = serial_source
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

func interruptClaimSource(claims, serialRoute string) string {
	return `
module examples.bad_interrupt_claim
use { BootPanic } from platform.hardware.panic
use { PlatformDiscoveryRoot } from platform.hardware.discovery
use { DelegatedHardware } from platform.uefi.transition
use { CpuFeatureFacts, OwnedHardware, OwnedMemory, IoPortAuthority, MemoryPlan, CpuPlan } from machine.x86_64.cpu_state
use { HardwarePlan, InterruptRoutingPlan, ClaimedPciPlanBuilder } from machine.x86_64.cpu_state
use { ExecutorSlot } from machine.x86_64.executor_slot
use { MutableBytes, Bytes } from machine.x86_64.executor_memory
use { InterruptSourceIdentity, InterruptVector } from machine.x86_64.interrupts
use { InterruptOverflowPolicy, InterruptQueue, QueueIdentity } from machine.x86_64.interrupt_queue
use { ArenaIdentity, ArenaPolicy } from platform.hardware.memory
use { Option } from wrela.lang.core

image BadInterruptClaim {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        let panic = BootPanic()
        let discovery = PlatformDiscoveryRoot(panic = panic).from_uefi(hardware = hardware)
        let irq_authority = discovery.acpi.require_madt().interrupt_authority()
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
        let interrupts = discovery.interrupts
        let root_region = discovery.memory.require_usable_region(min_base = 0x200000, length = 0x400000, align = 4096)
        let root = root_region.create_arena(identity = ArenaIdentity(label = "interrupt.claim.root"), policy = ArenaPolicy(evict_cache_by_default = true))
        let console_seed = ExecutorSlot(id = 0)
        let worker_seed = ExecutorSlot(id = 1)
        let console_memory = root.executor_memory(owner = console_seed, length = 0x100000, align = 4096)
        let worker_memory = root.executor_memory(owner = worker_seed, length = 0x100000, align = 4096)
        let serial_route = interrupts.route_shared_irq(irq = 6, vector = InterruptVector(value = 0x46))
        let serial_source = serial_route.claim_source(identity = InterruptSourceIdentity(label = "serial.rx"))
        let serial_queue_slots = console_memory.reserve_array(U8, count = 64)
        let serial_queue = InterruptQueue<U8>(identity = QueueIdentity(label = "irq.serial.rx"), owner = console_seed, slots = serial_queue_slots, capacity = 64, overflow = InterruptOverflowPolicy(mode = 0), head = 0, tail = 0, overflowed = false)
        let hardware_plan = HardwarePlan(
            cpus = discovery.acpi.require_madt().enabled_cpus().require_count(count = 2),
            interrupts = InterruptRoutingPlan(
                local_apic = interrupts.local_apic,
                serial_irq4 = ` + serialRoute + `,
                serial_shared_irq4 = serial_route,
                serial_irq_source = serial_source
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

const duplicateHardwareClaimSource = `
module examples.bad_duplicate_bar
use { BootPanic } from platform.hardware.panic
use { PlatformDiscoveryRoot } from platform.hardware.discovery
use { DelegatedHardware } from platform.uefi.transition
use { CpuFeatureFacts, OwnedHardware, OwnedMemory, IoPortAuthority, MemoryPlan, CpuPlan } from machine.x86_64.cpu_state
use { HardwarePlan, InterruptRoutingPlan, ClaimedPciPlanBuilder } from machine.x86_64.cpu_state
use { ExecutorSlot } from machine.x86_64.executor_slot
use { MutableBytes, Bytes } from machine.x86_64.executor_memory
use { InterruptSourceIdentity, InterruptVector } from machine.x86_64.interrupts
use { InterruptOverflowPolicy, InterruptQueue, QueueIdentity } from machine.x86_64.interrupt_queue
use { ArenaIdentity, ArenaPolicy } from platform.hardware.memory
use { Option } from wrela.lang.core

image BadDuplicateBar {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        let discovery = PlatformDiscoveryRoot(panic = BootPanic()).from_uefi(hardware = hardware)
        let edu = discovery.pci.require_device(vendor_id = 0x1234, device_id = 0x11E8, occurrence = 0)
        let first = edu.claim_mmio_bar(index = 0)
        let second = edu.claim_mmio_bar(index = 0)
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
        let root_region = discovery.memory.require_usable_region(min_base = 0x200000, length = 0x400000, align = 4096)
        let root = root_region.create_arena(identity = ArenaIdentity(label = "duplicate.bar.root"), policy = ArenaPolicy(evict_cache_by_default = true))
        let console_seed = ExecutorSlot(id = 0)
        let worker_seed = ExecutorSlot(id = 1)
        let console_memory = root.executor_memory(owner = console_seed, length = 0x100000, align = 4096)
        let worker_memory = root.executor_memory(owner = worker_seed, length = 0x100000, align = 4096)
        let serial_route = interrupts.route_shared_irq(irq = 6, vector = InterruptVector(value = 0x46))
        let serial_source = serial_route.claim_source(identity = InterruptSourceIdentity(label = "serial.rx"))
        let serial_queue_slots = console_memory.reserve_array(U8, count = 64)
        let serial_queue = InterruptQueue<U8>(identity = QueueIdentity(label = "irq.serial.rx"), owner = console_seed, slots = serial_queue_slots, capacity = 64, overflow = InterruptOverflowPolicy(mode = 0), head = 0, tail = 0, overflowed = false)
        let hardware_plan = HardwarePlan(
            cpus = discovery.acpi.require_madt().enabled_cpus().require_count(count = 2),
            interrupts = InterruptRoutingPlan(
                local_apic = interrupts.local_apic,
                serial_irq4 = serial_route.route,
                serial_shared_irq4 = serial_route,
                serial_irq_source = serial_source
            ),
            pci = ClaimedPciPlanBuilder(panic = BootPanic()).empty(),
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

const duplicateInterruptVectorSource = `
module examples.bad_duplicate_vector
use { BootPanic } from platform.hardware.panic
use { PlatformDiscoveryRoot } from platform.hardware.discovery
use { DelegatedHardware } from platform.uefi.transition
use { CpuFeatureFacts, OwnedHardware, OwnedMemory, IoPortAuthority, MemoryPlan, CpuPlan } from machine.x86_64.cpu_state
use { HardwarePlan, InterruptRoutingPlan, ClaimedPciPlanBuilder } from machine.x86_64.cpu_state
use { ExecutorSlot } from machine.x86_64.executor_slot
use { MutableBytes, Bytes } from machine.x86_64.executor_memory
use { InterruptSourceIdentity, InterruptVector } from machine.x86_64.interrupts
use { InterruptOverflowPolicy, InterruptQueue, QueueIdentity } from machine.x86_64.interrupt_queue
use { ArenaIdentity, ArenaPolicy } from platform.hardware.memory
use { Option } from wrela.lang.core

image BadDuplicateVector {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        let discovery = PlatformDiscoveryRoot(panic = BootPanic()).from_uefi(hardware = hardware)
        let interrupts = discovery.acpi.require_madt().interrupt_authority()
        let first = interrupts.route_isa_irq(irq = 4, vector = InterruptVector(value = 0x40))
        let second = interrupts.route_isa_irq(irq = 5, vector = InterruptVector(value = 0x40))
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
        let plan_interrupts = discovery.interrupts
        let root_region = discovery.memory.require_usable_region(min_base = 0x200000, length = 0x400000, align = 4096)
        let root = root_region.create_arena(identity = ArenaIdentity(label = "duplicate.vector.root"), policy = ArenaPolicy(evict_cache_by_default = true))
        let console_seed = ExecutorSlot(id = 0)
        let worker_seed = ExecutorSlot(id = 1)
        let console_memory = root.executor_memory(owner = console_seed, length = 0x100000, align = 4096)
        let worker_memory = root.executor_memory(owner = worker_seed, length = 0x100000, align = 4096)
        let serial_route = plan_interrupts.route_shared_irq(irq = 6, vector = InterruptVector(value = 0x46))
        let serial_source = serial_route.claim_source(identity = InterruptSourceIdentity(label = "serial.rx"))
        let serial_queue_slots = console_memory.reserve_array(U8, count = 64)
        let serial_queue = InterruptQueue<U8>(identity = QueueIdentity(label = "irq.serial.rx"), owner = console_seed, slots = serial_queue_slots, capacity = 64, overflow = InterruptOverflowPolicy(mode = 0), head = 0, tail = 0, overflowed = false)
        let hardware_plan = HardwarePlan(
            cpus = discovery.acpi.require_madt().enabled_cpus().require_count(count = 2),
            interrupts = InterruptRoutingPlan(
                local_apic = plan_interrupts.local_apic,
                serial_irq4 = first,
                serial_shared_irq4 = serial_route,
                serial_irq_source = serial_source
            ),
            pci = ClaimedPciPlanBuilder(panic = BootPanic()).empty(),
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
