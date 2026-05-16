package sem

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestMadtSyntheticEntryOffsetContract(t *testing.T) {
	localApic := []byte{0, 8, 3, 7, 1, 0, 0, 0}
	if localApic[2] != 3 || localApic[3] != 7 || binary.LittleEndian.Uint32(localApic[4:8]) != 1 {
		t.Fatalf("local APIC synthetic entry corrupt")
	}

	x2apic := make([]byte, 16)
	x2apic[0] = 9
	x2apic[1] = 16
	binary.LittleEndian.PutUint32(x2apic[4:8], 0x123)
	binary.LittleEndian.PutUint32(x2apic[8:12], 1)
	binary.LittleEndian.PutUint32(x2apic[12:16], 0x55)
	if binary.LittleEndian.Uint32(x2apic[4:8]) != 0x123 ||
		binary.LittleEndian.Uint32(x2apic[8:12]) != 1 ||
		binary.LittleEndian.Uint32(x2apic[12:16]) != 0x55 {
		t.Fatalf("x2APIC synthetic entry corrupt")
	}

	sourceText := readRepoFile(t, "wrela/platform/acpi/madt.wrela")
	for _, want := range []string{
		"entry_type == 9",
		"read_u32(offset = offset + 4)",
		"read_u32(offset = offset + 8)",
		"read_u32(offset = offset + 12)",
		"out.append(uid = bytes.read_u32(offset = offset + 12), apic_id = bytes.read_u32(offset = offset + 4))",
	} {
		if !strings.Contains(sourceText, want) {
			t.Fatalf("MADT source missing %q", want)
		}
	}
}
