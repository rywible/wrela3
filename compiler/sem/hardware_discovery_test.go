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

	tables := moduleType(t, index, "platform.uefi.types", "UefiConfigurationTables")
	assertMethodExists(t, tables, "entry_at")
	assertMethodExists(t, tables, "find_acpi_rsdp")
	assertMethodExists(t, moduleType(t, index, "platform.uefi.types", "UefiMemoryMap"), "descriptor_at")
	assertMethodExists(t, moduleType(t, index, "platform.uefi.types", "UefiMemoryMap"), "require_usable_region")
}
