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
		"original & 0xFFFC",
		"mask & 0xFFFC",
		"0xAC060001",
		"0xAC060002",
		"0xAC060003",
	}
	for _, needle := range required {
		if !strings.Contains(source, needle) {
			t.Fatalf("pci BAR contract missing %q", needle)
		}
	}
}
