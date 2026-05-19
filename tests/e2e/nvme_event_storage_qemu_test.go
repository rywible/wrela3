package e2e

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const nvmeStorageSentinelLBA int64 = 4096
const nvmeStorageSentinelMagic uint64 = 0x3152564E41455257

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

func TestNvmeEventStorageReplayQEMU(t *testing.T) {
	disk := filepath.Join(t.TempDir(), "storage.raw")
	createSparseRawDisk(t, disk, nvmeStorageDiskBytes)
	first := runStorageQEMU(t, disk, "first")
	if !strings.Contains(first, "NVME_STORAGE_APPEND_OK last_event_id=1") {
		t.Fatalf("first boot missing append marker:\n%s", first)
	}
	assertStorageSentinel(t, disk, 1, 1, true)
	second := runStorageQEMU(t, disk, "replay")
	for _, want := range []string{
		"NVME_STORAGE_REPLAY_OK last_event_id=1",
		"projection_watermark=1",
		"NVME_ORPHAN_COLLECTION_OK",
	} {
		if !strings.Contains(second, want) {
			t.Fatalf("second boot missing %q:\n%s", want, second)
		}
	}
	fresh := filepath.Join(t.TempDir(), "fresh.raw")
	createSparseRawDisk(t, fresh, nvmeStorageDiskBytes)
	freshOut, freshErr := runStorageQEMUResult(t, fresh, "replay")
	if freshErr == nil {
		t.Fatalf("fresh replay unexpectedly succeeded:\n%s", freshOut)
	}
	if strings.Contains(freshOut, "NVME_STORAGE_REPLAY_OK") || strings.Contains(freshOut, "NVME_STORAGE_APPEND_OK") {
		t.Fatalf("fresh replay produced a storage success marker:\n%s", freshOut)
	}
}

func TestNvmeEventStorageInvalidModeQEMU(t *testing.T) {
	disk := filepath.Join(t.TempDir(), "storage.raw")
	createSparseRawDisk(t, disk, nvmeStorageDiskBytes)
	out, err := runStorageQEMUResult(t, disk, "bogus")
	assertInvalidStorageMode(t, out, err)

	prefixDisk := filepath.Join(t.TempDir(), "prefix.raw")
	createSparseRawDisk(t, prefixDisk, nvmeStorageDiskBytes)
	prefixOut, prefixErr := runStorageQEMUResult(t, prefixDisk, "first-bad")
	assertInvalidStorageMode(t, prefixOut, prefixErr)
}

func assertInvalidStorageMode(t *testing.T, out string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("invalid mode unexpectedly succeeded:\n%s", out)
	}
	for _, unexpected := range []string{
		"NVME_STORAGE_APPEND_OK",
		"NVME_STORAGE_REPLAY_OK",
		"NVME_STORAGE_DONE",
	} {
		if strings.Contains(out, unexpected) {
			t.Fatalf("invalid mode produced %q:\n%s", unexpected, out)
		}
	}
}

func assertStorageSentinel(t *testing.T, disk string, lastEventID, projectionWatermark uint64, orphanCollected bool) {
	t.Helper()
	f, err := os.Open(disk)
	if err != nil {
		t.Fatalf("open storage disk: %v", err)
	}
	defer f.Close()
	block := make([]byte, 32)
	if _, err := f.ReadAt(block, nvmeStorageSentinelLBA*512); err != nil {
		t.Fatalf("read storage sentinel: %v", err)
	}
	if got := binary.LittleEndian.Uint64(block[0:8]); got != nvmeStorageSentinelMagic {
		t.Fatalf("storage sentinel magic = %#x, want %#x", got, nvmeStorageSentinelMagic)
	}
	if got := binary.LittleEndian.Uint64(block[8:16]); got != lastEventID {
		t.Fatalf("storage sentinel last_event_id = %d, want %d", got, lastEventID)
	}
	if got := binary.LittleEndian.Uint64(block[16:24]); got != projectionWatermark {
		t.Fatalf("storage sentinel projection_watermark = %d, want %d", got, projectionWatermark)
	}
	gotOrphan := binary.LittleEndian.Uint64(block[24:32]) != 0
	if gotOrphan != orphanCollected {
		t.Fatalf("storage sentinel orphan_collected = %v, want %v", gotOrphan, orphanCollected)
	}
}
