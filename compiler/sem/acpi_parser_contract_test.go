package sem

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAcpiSyntheticRootTableContract(t *testing.T) {
	rsdp := make([]byte, 36)
	copy(rsdp[0:8], []byte("RSD PTR "))
	rsdp[15] = 2
	binary.LittleEndian.PutUint32(rsdp[20:24], uint32(len(rsdp)))
	binary.LittleEndian.PutUint64(rsdp[24:32], 0x12345000)
	rsdp[8] = checksumByte(rsdp[:20])
	rsdp[32] = checksumByte(rsdp)
	if checksumSum(rsdp[:20]) != 0 || checksumSum(rsdp) != 0 {
		t.Fatalf("synthetic RSDP checksums are invalid")
	}

	xsdt := make([]byte, 44)
	copy(xsdt[0:4], []byte("XSDT"))
	binary.LittleEndian.PutUint32(xsdt[4:8], uint32(len(xsdt)))
	binary.LittleEndian.PutUint64(xsdt[36:44], 0xfeedbeef)
	xsdt[9] = checksumByte(xsdt)
	if got := binary.LittleEndian.Uint64(xsdt[36:44]); got != 0xfeedbeef {
		t.Fatalf("XSDT entry offset = %#x", got)
	}

	sourceText := readRepoFile(t, "wrela/platform/acpi/root.wrela")
	for _, want := range []string{
		"read_u8(offset = 15)",
		"read_u32(offset = 20)",
		"read_u64(offset = 24)",
		"root.length - 36",
		"entry_size = 8",
		"panic.fail(code = 0xAC010006)",
	} {
		if !strings.Contains(sourceText, want) {
			t.Fatalf("ACPI root source missing %q", want)
		}
	}
}

func checksumByte(data []byte) byte {
	return byte(0 - checksumSum(data))
}

func checksumSum(data []byte) byte {
	var sum byte
	for _, b := range data {
		sum += b
	}
	return sum
}

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(wd, "..", "..", rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
