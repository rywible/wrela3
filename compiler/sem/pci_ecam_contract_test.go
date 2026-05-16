package sem

import (
	"strings"
	"testing"
)

func TestPciEcamEnumerationSourceContract(t *testing.T) {
	source := readRepoFile(t, "wrela/machine/x86_64/pci.wrela")
	required := []string{
		"(bus - self.start_bus) << 20",
		"device << 15",
		"function << 12",
		"offset & 0x0FFC",
		"while bus <= self.window0.end_bus",
		"while device < 32",
		"while function < 8",
		"vendor_device & 0xFFFF",
		"self.panic.fail(code = 0xAC060012)",
		"self.panic.fail(code = 0xAC060010)",
	}
	for _, needle := range required {
		if !strings.Contains(source, needle) {
			t.Fatalf("PCI ECAM contract missing %q", needle)
		}
	}
}
