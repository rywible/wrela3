package sem

import (
	"strings"
	"testing"
)

func TestHardwareDiscoverySourceShape(t *testing.T) {
	modules := parseUEFIModuleSet(t)
	index, ds := BuildIndex(modules)
	if len(ds) != 0 {
		t.Fatalf("build index diagnostics: %#v", ds)
	}

	assertMethodExists(t, moduleType(t, index, "platform.hardware.discovery", "PlatformDiscoveryRoot"), "from_uefi")
	assertMethodExists(t, moduleType(t, index, "platform.uefi.transition", "DelegatedHardware"), "uefi_configuration_tables")
	assertMethodExists(t, moduleType(t, index, "platform.uefi.transition", "DelegatedHardware"), "memory_map")
	plan := moduleType(t, index, "machine.x86_64.cpu_state", "HardwarePlan")
	if fieldTypeName(t, plan, "cpus") != "CpuTopology" {
		t.Fatalf("HardwarePlan.cpus must be CpuTopology")
	}
	owned := moduleType(t, index, "machine.x86_64.cpu_state", "OwnedHardware")
	if fieldTypeName(t, owned, "hardware_plan") != "HardwarePlan" {
		t.Fatalf("OwnedHardware.hardware_plan must be HardwarePlan")
	}
	discovered := moduleType(t, index, "platform.hardware.discovery", "DiscoveredHardware")
	if fieldTypeName(t, discovered, "pci") != "PciDeviceSet" {
		t.Fatalf("DiscoveredHardware.pci must be PciDeviceSet")
	}
	if fieldTypeName(t, discovered, "cpus") != "CpuDiscovery" {
		t.Fatalf("DiscoveredHardware.cpus must be CpuDiscovery")
	}
	if fieldTypeName(t, discovered, "timers") != "TimerDiscovery" {
		t.Fatalf("DiscoveredHardware.timers must be TimerDiscovery")
	}
	if fieldTypeName(t, discovered, "framebuffer") != "FramebufferInfo" {
		t.Fatalf("DiscoveredHardware.framebuffer must be FramebufferInfo")
	}
	framebuffer := moduleType(t, index, "platform.hardware.discovery", "FramebufferInfo")
	if fieldTypeName(t, framebuffer, "known") != "Bool" {
		t.Fatalf("FramebufferInfo.known must make absence explicit")
	}
	for _, typ := range []struct{ module, name string }{
		{"machine.x86_64.cpu_state", "CpuLocalityFacts"},
		{"machine.x86_64.cpu_state", "CpuDiscovery"},
		{"machine.x86_64.interrupts", "ApicModeFacts"},
		{"machine.x86_64.timer", "TimerDiscovery"},
		{"machine.x86_64.timer", "TimerAuthority"},
		{"platform.hardware.discovery", "FramebufferInfo"},
	} {
		_ = moduleType(t, index, typ.module, typ.name)
	}
	report := moduleType(t, index, "platform.hardware.discovery", "DiscoveryReport")
	for _, field := range []string{
		"memory_base",
		"memory_length",
		"bootstrap_apic_id",
		"secondary_apic_id",
		"local_apic_base",
		"io_apic_base",
		"serial_gsi",
		"pci_device_count",
		"edu_bar0",
		"ivshmem_rx_bar0",
	} {
		_ = fieldTypeName(t, report, field)
	}
	assertMethodExists(t, discovered, "report")
	assertMethodExists(t, moduleType(t, index, "platform.acpi.root", "AcpiRoot"), "require_madt")
	assertMethodExists(t, moduleType(t, index, "platform.acpi.root", "AcpiRoot"), "require_mcfg")
	root := moduleType(t, index, "platform.acpi.root", "AcpiRoot")
	assertMethodExists(t, root, "require_table")
	assertMethodExists(t, root, "require_madt")
	assertMethodExists(t, root, "require_mcfg")
	assertMethodExists(t, moduleType(t, index, "platform.acpi.root", "AcpiLocator"), "find")
	assertMethodExists(t, moduleType(t, index, "platform.acpi.tables", "AcpiHelpers"), "checksum_ok")

	tables := moduleType(t, index, "platform.uefi.types", "UefiConfigurationTables")
	assertMethodExists(t, tables, "entry_at")
	assertMethodExists(t, tables, "find_acpi_rsdp")
	assertMethodExists(t, moduleType(t, index, "platform.uefi.types", "UefiMemoryMap"), "descriptor_at")
	assertMethodExists(t, moduleType(t, index, "platform.uefi.types", "UefiMemoryMap"), "require_usable_region")

	assertMethodExists(t, moduleType(t, index, "platform.hardware.panic", "BootPanic"), "fail")
	bounded := moduleType(t, index, "platform.hardware.bytes", "BoundedBytes")
	assertMethodExists(t, bounded, "read_u32")
	if fieldTypeName(t, bounded, "panic") != "BootPanic" {
		t.Fatalf("BoundedBytes.panic must be BootPanic")
	}
	assertMethodExists(t, moduleType(t, index, "platform.hardware.bytes", "MmioRegion"), "read32")
	assertMethodExists(t, moduleType(t, index, "platform.hardware.bytes", "MmioRegion"), "write32")

	madt := moduleType(t, index, "platform.acpi.madt", "MadtTable")
	assertMethodExists(t, madt, "local_apic_base")
	assertMethodExists(t, madt, "enabled_cpus")
	assertMethodExists(t, madt, "io_apics")
	assertMethodExists(t, madt, "interrupt_source_overrides")
	assertMethodExists(t, madt, "interrupt_authority")

	assertMethodExists(t, moduleType(t, index, "platform.acpi.mcfg", "McfgTable"), "ecam_windows")
	windows := moduleType(t, index, "machine.x86_64.pci", "PcieEcamWindows")
	assertMethodExists(t, windows, "enumerate")
	pci := moduleType(t, index, "machine.x86_64.pci", "PciDeviceSet")
	assertMethodExists(t, pci, "require_device")
	assertMethodExists(t, moduleType(t, index, "machine.x86_64.pci", "PcieEcamWindow"), "read_config32")
	dev := moduleType(t, index, "machine.x86_64.pci", "PciDevice")
	assertMethodExists(t, dev, "claim_mmio_bar")
	assertMethodExists(t, dev, "claim_io_bar")

	interrupts := moduleType(t, index, "machine.x86_64.interrupts", "InterruptAuthority")
	assertMethodExists(t, interrupts, "route_isa_irq")
	assertMethodExists(t, interrupts, "select_apic_mode")
	assertMethodExists(t, interrupts, "require_x2apic")
	assertMethodExists(t, moduleType(t, index, "machine.x86_64.interrupts", "ApicModeSelection"), "with_xapic_fallback")
	assertMethodExists(t, moduleType(t, index, "machine.x86_64.interrupts", "IoApicRoute"), "program")
	route := moduleType(t, index, "machine.x86_64.interrupts", "IoApicRoute")
	if fieldTypeName(t, route, "destination_apic_id") != "U32" || fieldTypeName(t, route, "flags") != "U16" {
		t.Fatalf("IoApicRoute must carry destination APIC ID and MADT override flags")
	}
	discoverySource := readRepoFile(t, "wrela/platform/hardware/discovery.wrela")
	if !strings.Contains(discoverySource, "pci = mcfg.ecam_windows().enumerate()") ||
		!strings.Contains(discoverySource, "pci_device_count = self.pci.count") {
		t.Fatalf("hardware discovery source must enumerate PCI and report discovered device count")
	}
	for _, want := range []string{
		"local_apic_timer_available = true",
		"pit_available = true",
		"known = false",
	} {
		if !strings.Contains(discoverySource, want) {
			t.Fatalf("hardware discovery source missing %q", want)
		}
	}
	bytesSource := readRepoFile(t, "wrela/platform/hardware/bytes.wrela")
	for _, want := range []string{
		"self.panic.fail(code = 0xAC030001)",
		"self.panic.fail(code = 0xAC030002)",
		"BoundedBytes(address = self.address + offset, length = length, panic = self.panic)",
		"BoundedBytes(address = self.address, length = self.length, panic = self.panic)",
	} {
		if !strings.Contains(bytesSource, want) {
			t.Fatalf("hardware bytes source missing %q", want)
		}
	}
	source := readRepoFile(t, "wrela/machine/x86_64/interrupts.wrela")
	for _, want := range []string{
		"self.destination_apic_id << 24",
		"flags & 0x0003",
		"flags & 0x000C",
		"flags_for_isa_irq",
		"self.io_apics.count == 0",
		"(self.apic_id & 0xFF) << 12",
		"select_apic_mode",
		"require_x2apic",
		"with_xapic_fallback",
		"0xAC050010",
		"0xAC050011",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("interrupt route source missing %q", want)
		}
	}
}

func TestDiscoveryFactsRecordedFromImageSource(t *testing.T) {
	checked, ds := checkUEFIModulesWithExtraSource(t, "discovery-facts.wrela", discoveryFactExtractionSource)
	if len(ds) != 0 {
		t.Fatalf("diagnostics: %#v", ds)
	}
	if len(checked.ImageGraph.APICFacts) == 0 || checked.ImageGraph.APICFacts[len(checked.ImageGraph.APICFacts)-1].Mode != "x2apic_with_xapic_fallback" {
		t.Fatalf("APIC facts not recorded: %#v", checked.ImageGraph.APICFacts)
	}
	if len(checked.ImageGraph.TimerFacts) != 1 ||
		checked.ImageGraph.TimerFacts[0].Label != "periodic.1000us" ||
		checked.ImageGraph.TimerFacts[0].PeriodUS != 1000 {
		t.Fatalf("timer facts not recorded: %#v", checked.ImageGraph.TimerFacts)
	}
	if len(checked.ImageGraph.LocalityFacts) != 1 ||
		checked.ImageGraph.LocalityFacts[0].Subject != "cpu0" ||
		checked.ImageGraph.LocalityFacts[0].Known {
		t.Fatalf("unknown locality fact not recorded: %#v", checked.ImageGraph.LocalityFacts)
	}
	if len(checked.ImageGraph.FramebufferFacts) != 1 || checked.ImageGraph.FramebufferFacts[0].Known {
		t.Fatalf("unknown framebuffer fact not recorded: %#v", checked.ImageGraph.FramebufferFacts)
	}
	reportImage := BuildImageReport(checked)
	if len(reportImage.Hardware.Timers) != 1 || reportImage.Hardware.Timers[0].PeriodUS != 1000 {
		t.Fatalf("timer facts missing from report: %#v", reportImage.Hardware.Timers)
	}
	if reportImage.Hardware.Framebuffer.Known {
		t.Fatalf("framebuffer fact missing from report: %#v", reportImage.Hardware.Framebuffer)
	}
}

const discoveryFactExtractionSource = `
module examples.discovery_facts
use { BootPanic } from platform.hardware.panic
use { PlatformDiscoveryRoot } from platform.hardware.discovery
use { DelegatedHardware } from platform.uefi.transition
use { CpuFeatureFacts, OwnedHardware, OwnedMemory, IoPortAuthority, MemoryPlan, CpuPlan, HardwarePlan, InterruptRoutingPlan, ClaimedPciPlanBuilder } from machine.x86_64.cpu_state
use { ExecutorSlot } from machine.x86_64.executor_slot
use { MutableBytes, Bytes } from machine.x86_64.executor_memory
use { InterruptSourceIdentity, InterruptVector } from machine.x86_64.interrupts
use { InterruptOverflowPolicy, InterruptPayloadKind, QueueIdentity } from machine.x86_64.interrupt_queue
use { ArenaIdentity, ArenaPolicy } from platform.hardware.memory

image DiscoveryFactsImage {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        let panic = BootPanic()
        let discovery = PlatformDiscoveryRoot(panic = panic).from_uefi(hardware = hardware)
        let interrupts = discovery.interrupts
        let apic_mode = interrupts.select_apic_mode().with_xapic_fallback()
        let local_apic = interrupts.local_apic
        let timer = discovery.timers.require_periodic(period_us = 1000)
        let cpu0_numa = discovery.cpus.locality0.numa_node
        let framebuffer_known = discovery.framebuffer.known
        let root_region = discovery.memory.require_usable_region(min_base = 0x200000, length = 0x400000, align = 4096)
        let root = root_region.create_arena(identity = ArenaIdentity(label = "discovery.facts.root"), policy = ArenaPolicy(evict_cache_by_default = true))
        let console_seed = ExecutorSlot(id = 0)
        let worker_seed = ExecutorSlot(id = 1)
        let console_memory = root.executor_memory(owner = console_seed, length = 0x100000, align = 4096)
        let worker_memory = root.executor_memory(owner = worker_seed, length = 0x100000, align = 4096)
        let shared = interrupts.route_shared_irq(irq = 4, vector = InterruptVector(value = 0x40))
        let queue = root.interrupt_queue(identity = QueueIdentity(label = "irq.serial.rx"), owner = console_seed, capacity = 64, payload = InterruptPayloadKind(kind = 1, size = 8, align = 8), overflow = InterruptOverflowPolicy(mode = 0))
        let arena = MutableBytes(address = 0, length = 0)
        let hardware_plan = HardwarePlan(
            cpus = discovery.cpus.require_min_count(count = 2),
            interrupts = InterruptRoutingPlan(
                local_apic = local_apic,
                serial_irq4 = shared.route,
                serial_shared_irq4 = shared,
                serial_irq_source = shared.claim_source(identity = InterruptSourceIdentity(label = "serial.rx"))
            ),
            pci = ClaimedPciPlanBuilder(panic = panic).empty(),
            timer = timer,
            serial_irq_queue = queue,
            console_memory = console_memory,
            worker_memory = worker_memory,
            wake_strategy = discovery.cpus.wake_strategy(features = CpuFeatureFacts(monitor_mwait_available = true))
        )
        let _timer_source = timer.source.kind
        let _apic_mode = apic_mode.mode
        let _cpu0_numa = cpu0_numa
        let _framebuffer_known = framebuffer_known
        return hardware.exit_to_owned_hardware(
            memory_plan = MemoryPlan(owned_memory = OwnedMemory(arena = arena), executor_arena = arena, io_ports = IoPortAuthority()),
            cpu_plan = CpuPlan(owned_stack_top = 0, gdt_descriptor = Bytes(address = 0, length = 0), idt_descriptor = Bytes(address = 0, length = 0), cr3 = 0),
            hardware_plan = hardware_plan
        )
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        while true {}
    }
}
`
