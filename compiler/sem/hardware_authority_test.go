package sem

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/parse"
	"github.com/ryanwible/wrela3/compiler/source"
)

func checkUEFIModulesWithExtraSource(t *testing.T, name string, sourceText string) (*CheckedProgram, []diag.Diagnostic) {
	t.Helper()
	modules := parseUEFIModuleSet(t)
	for i := range modules {
		if modules[i].Name == "sem.uefi_test_harness" {
			modules = append(modules[:i], modules[i+1:]...)
			break
		}
	}
	extra, pds := parse.ParseGraph(source.Graph{
		Files: []*source.File{source.NewFile(source.FileID(9000), name, sourceText)},
	})
	if len(pds) != 0 {
		t.Fatalf("parse extra source: %#v", pds)
	}
	modules = append(modules, extra...)
	index, ds := BuildIndex(modules)
	if len(ds) != 0 {
		return nil, ds
	}
	return Check(index, modules)
}

func TestForgedHardwareAuthorityRejected(t *testing.T) {
	_, ds := checkUEFIModulesWithExtraSource(t, "forged-mmio-test.wrela", `
module examples.bad
use { BootPanic } from platform.hardware.panic
use { PlatformDiscoveryRoot } from platform.hardware.discovery
use { MmioRegion } from platform.hardware.bytes
use { DelegatedHardware } from platform.uefi.transition
use { OwnedHardware, OwnedMemory, IoPortAuthority, MemoryPlan, CpuPlan } from machine.x86_64.cpu_state
use { HardwarePlan, InterruptRoutingPlan, ClaimedPciPlanBuilder } from machine.x86_64.cpu_state
use { MutableBytes, Bytes } from machine.x86_64.executor_memory
use { InterruptVector } from machine.x86_64.interrupts

image BadForgedMmio {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        let panic = BootPanic()
        let fake = MmioRegion(address = 0xFEC00000, length = 4096, panic = panic)
        let arena = MutableBytes(address = 0, length = 0)
        let owned_memory = OwnedMemory(arena = arena)
        let memory_plan = MemoryPlan(
            owned_memory = owned_memory,
            executor_arena = MutableBytes(address = 0, length = 0),
            io_ports = IoPortAuthority()
        )
        let cpu_plan = CpuPlan(
            owned_stack_top = 0,
            gdt_descriptor = Bytes(address = 0, length = 0),
            idt_descriptor = Bytes(address = 0, length = 0),
            cr3 = 0
        )
        let discovery = PlatformDiscoveryRoot(panic = panic).from_uefi(hardware = hardware)
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
}`)
	if !hasCode(ds, diag.SEM0049) {
		t.Fatalf("expected SEM0049, got %#v", ds)
	}
}
