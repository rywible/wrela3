package sem

import (
	"strings"
	"testing"
)

func TestPciEcamEnumerationSourceContract(t *testing.T) {
	source := readRepoFile(t, "wrela/machine/x86_64/pci.wrela")
	required := []string{
		"let bus_index = self.u8_to_u64(value = bus) - self.u8_to_u64(value = self.start_bus)",
		"device_index * 32768",
		"function_index * 4096",
		"offset & 0x0FFC",
		"fn io_config_address(self, bus: U8, device: U8, function: U8, offset: U16) -> U32",
		"address = address | (self.u8_to_u32(value = bus) << 16)",
		"asm fn read_io_config32(self, address: U32) -> U32",
		"asm fn write_io_config32(self, address: U32, value: U32)",
		"out dx, eax",
		"in eax, dx",
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
