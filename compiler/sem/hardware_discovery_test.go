package sem

import "testing"

func TestHardwareDiscoverySourceShape(t *testing.T) {
	modules := parseUEFIModuleSet(t)
	index, ds := BuildIndex(modules)
	if len(ds) != 0 {
		t.Fatalf("build index diagnostics: %#v", ds)
	}

	assertMethodExists(t, moduleType(t, index, "platform.hardware.discovery", "PlatformDiscoveryRoot"), "from_uefi")
	assertMethodExists(t, moduleType(t, index, "platform.uefi.transition", "DelegatedHardware"), "uefi_configuration_tables")
	assertMethodExists(t, moduleType(t, index, "platform.uefi.transition", "DelegatedHardware"), "memory_map")
	assertMethodExists(t, moduleType(t, index, "platform.acpi.root", "AcpiRoot"), "require_madt")
	assertMethodExists(t, moduleType(t, index, "platform.acpi.root", "AcpiRoot"), "require_mcfg")
	assertMethodExists(t, moduleType(t, index, "machine.x86_64.pci", "PciDeviceSet"), "require_device")
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
	_ = moduleType(t, index, "machine.x86_64.pci", "PcieEcamWindows")
}
