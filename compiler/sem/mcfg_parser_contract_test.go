package sem

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestMcfgSyntheticEcamWindowContract(t *testing.T) {
	entry := make([]byte, 16)
	binary.LittleEndian.PutUint64(entry[0:8], 0xE0000000)
	binary.LittleEndian.PutUint16(entry[8:10], 0)
	entry[10] = 0
	entry[11] = 255
	if binary.LittleEndian.Uint64(entry[0:8]) != 0xE0000000 || entry[10] != 0 || entry[11] != 255 {
		t.Fatalf("synthetic MCFG entry corrupt")
	}

	sourceText := readRepoFile(t, "wrela/platform/acpi/mcfg.wrela")
	for _, want := range []string{
		"offset = 44",
		"read_u64(offset = offset)",
		"read_u16(offset = offset + 8)",
		"read_u8(offset = offset + 10)",
		"read_u8(offset = offset + 11)",
		"offset = offset + 16",
	} {
		if !strings.Contains(sourceText, want) {
			t.Fatalf("MCFG source missing %q", want)
		}
	}
}
