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
		"while device < 32",
		"while function < 8",
		"vendor_device & 0xFFFF",
		"self.count == 0",
		"self.panic.fail(code = 0xAC060013)",
		"self.panic.fail(code = 0xAC060012)",
		"self.panic.fail(code = 0xAC060010)",
		"window1: PcieEcamWindow",
		"window2: PcieEcamWindow",
		"window3: PcieEcamWindow",
		"self.panic.fail(code = 0xAC060015)",
		"devices.scan_window(window = self.at(index = index))",
	}
	for _, needle := range required {
		if !strings.Contains(source, needle) {
			t.Fatalf("PCI ECAM contract missing %q", needle)
		}
	}
}
