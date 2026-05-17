package sem

import (
	"strings"
	"testing"
)

func TestPciBarClaimSourceContract(t *testing.T) {
	source := readRepoFile(t, "wrela/machine/x86_64/pci.wrela")
	required := []string{
		"fn claim_mmio_bar(self, index: U8) -> MmioRegion",
		"fn claim_io_bar(self, index: U8) -> IoPortRegion",
		"self.write_config32(offset = offset, value = 0xFFFFFFFF)",
		"let mask = self.read_config32(offset = offset)",
		"original & 0xFFFFFFF0",
		"mask & 0xFFFFFFF0",
		"bar_type == 2",
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
	tableClaim := strings.Index(source, "let table = self.claim_mmio_bar(index = table_bar_index)")
	if tableClaim < 0 || !strings.Contains(source[tableClaim:], "self.enable_mmio_and_bus_master()") {
		t.Fatalf("MSI-X must claim the table BAR before enabling PCI MMIO decode")
	}
}
