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
	assertMethodExists(t, moduleType(t, index, "platform.hardware.bytes", "BoundedBytes"), "read_u32")
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
	source := readRepoFile(t, "wrela/machine/x86_64/interrupts.wrela")
	for _, want := range []string{"self.destination_apic_id << 24", "flags & 0x0003", "flags & 0x000C", "flags_for_isa_irq", "self.io_apics.count == 0", "(self.apic_id & 0xFF) << 12"} {
		if !strings.Contains(source, want) {
			t.Fatalf("interrupt route source missing %q", want)
		}
	}
}
