package sem

import (
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

const duplicateHardwareClaimSource = `
module examples.bad_duplicate_bar
use { BootPanic } from platform.hardware.panic
use { PlatformDiscoveryRoot } from platform.hardware.discovery
use { DelegatedHardware } from platform.uefi.transition
use { OwnedHardware, OwnedMemory, IoPortAuthority, MemoryPlan, CpuPlan } from machine.x86_64.cpu_state
use { HardwarePlan, InterruptRoutingPlan, ClaimedPciPlanBuilder } from machine.x86_64.cpu_state
use { MutableBytes, Bytes } from machine.x86_64.executor_memory
use { InterruptVector } from machine.x86_64.interrupts

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
        let hardware_plan = HardwarePlan(
            cpus = discovery.acpi.require_madt().enabled_cpus().require_count(count = 2),
            interrupts = InterruptRoutingPlan(
                local_apic = interrupts.local_apic,
                serial_irq4 = interrupts.route_isa_irq(irq = 4, vector = InterruptVector(value = 0x40))
            ),
            pci = ClaimedPciPlanBuilder(panic = BootPanic()).empty()
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
use { OwnedHardware, OwnedMemory, IoPortAuthority, MemoryPlan, CpuPlan } from machine.x86_64.cpu_state
use { HardwarePlan, InterruptRoutingPlan, ClaimedPciPlanBuilder } from machine.x86_64.cpu_state
use { MutableBytes, Bytes } from machine.x86_64.executor_memory
use { InterruptVector } from machine.x86_64.interrupts

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
        let hardware_plan = HardwarePlan(
            cpus = discovery.acpi.require_madt().enabled_cpus().require_count(count = 2),
            interrupts = InterruptRoutingPlan(
                local_apic = interrupts.local_apic,
                serial_irq4 = first
            ),
            pci = ClaimedPciPlanBuilder(panic = BootPanic()).empty()
        )
        return hardware.exit_to_owned_hardware(memory_plan = memory_plan, cpu_plan = cpu_plan, hardware_plan = hardware_plan)
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        while true {}
    }
}
`
