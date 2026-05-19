package e2e

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNvmeEventStorageQEMU(t *testing.T) {
	disk := filepath.Join(t.TempDir(), "storage.raw")
	createSparseRawDisk(t, disk, nvmeStorageDiskBytes)
	out := runStorageQEMU(t, disk, "first")
	for _, want := range []string{
		"NVME_STORAGE_APPEND_OK last_event_id=1",
		"NVME_STORAGE_DONE",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("serial output missing %q:\n%s", want, out)
		}
	}
}
