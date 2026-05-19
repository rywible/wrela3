package sem

import (
	"strings"
	"testing"
)

func TestPciBarClaimSourceContract(t *testing.T) {
	source := readRepoFile(t, "wrela/machine/x86_64/pci.wrela")
	required := []string{
		"fn claim_mmio_bar(self, index: U8) -> MmioRegion",
		"fn claim_mmio_bar_at32(self, index: U8, base: U32) -> MmioRegion",
		"fn claim_io_bar(self, index: U8) -> IoPortRegion",
		"self.write_config32(offset = offset, value = 0xFFFFFFFF)",
		"let mask = self.read_config32(offset = offset)",
		"original & 0xFFFFFFF0",
		"mask & 0xFFFFFFF0",
		"command_status & 0x0000FFF9",
		"(base64 + size) > 0x100000000",
		"(base & 0xFFFFFFF0) | (original & 0xF)",
		"self.write_config32_mmio(offset = high_offset, value = 0)",
		"0xAC060006",
		"let bar_kind = original & 6",
		"bar_kind < 4",
		"bar_kind > 4",
		"let original_high = self.read_config32(offset = high_offset)",
		"self.write_config32(offset = high_offset, value = 0xFFFFFFFF)",
		"let mask_high = self.read_config32(offset = high_offset)",
		"let base = (self.u32_to_u64(value = original_high) << 32) | self.u32_to_u64(value = original & 0xFFFFFFF0)",
		"let mask = (self.u32_to_u64(value = mask_high) << 32) | self.u32_to_u64(value = masked_low)",
		"0xAC060004",
		"0xAC060005",
		"original & 0xFFFC",
		"mask & 0xFFFC",
		"0xAC060001",
		"0xAC060002",
		"0xAC060003",
		"(command_status & 0x0000FFFF) | 0x00000006",
		"let table = self.claim_mmio_bar(index = table_bar_index)",
		"self.enable_mmio_and_bus_master()",
	}
	for _, needle := range required {
		if !strings.Contains(source, needle) {
			t.Fatalf("pci BAR contract missing %q", needle)
		}
	}
	if strings.Contains(source, "self.write_config32_mmio(offset = 0x14, value = 0)") {
		t.Fatalf("pci BAR contract must not zero a hard-coded BAR1 high half")
	}
	tableClaim := strings.Index(source, "let table = self.claim_mmio_bar(index = table_bar_index)")
	if tableClaim < 0 || !strings.Contains(source[tableClaim:], "self.enable_mmio_and_bus_master()") {
		t.Fatalf("MSI-X must claim the table BAR before enabling PCI MMIO decode")
	}
}

func TestPciBridgeWalkingSourceShape(t *testing.T) {
	sourceText := readRepoFile(t, "wrela/machine/x86_64/pci.wrela")
	for _, want := range []string{
		"data PciBridgeBusRange",
		"data PciFunctionFacts",
		"fn scan_function(self, window: PcieEcamWindow, bus: U8, device: U8, function: U8, depth: U64)",
		"self.panic.fail(code = 0xAC060016)",
		"offset = 0x18",
		"class_code == 0x06",
		"subclass == 0x04",
		"self.scan_bus_depth(window = window, bus = next, depth = depth + 1)",
		"fn contains(self, window: PcieEcamWindow, bus: U8, device: U8, function: U8) -> Bool",
		"if self.contains(window = window, bus = bus, device = device, function = function) == true",
	} {
		if !strings.Contains(sourceText, want) {
			t.Fatalf("PCI bridge source missing %q", want)
		}
	}
}

func TestPciRuntimeFactSourceShape(t *testing.T) {
	sourceText := readRepoFile(t, "wrela/machine/x86_64/pci.wrela")
	for _, want := range []string{
		"revision: U8",
		"header_type: U8",
		"interrupt_pin: U8",
		"interrupt_line: U8",
		"has_msi: Bool",
		"has_msix: Bool",
		"fn facts(self) -> PciFunctionFacts",
		"self.find_capability_optional(capability_id = 0x05)",
		"self.find_capability_optional(capability_id = 0x11)",
	} {
		if !strings.Contains(sourceText, want) {
			t.Fatalf("PCI facts source missing %q", want)
		}
	}
}
