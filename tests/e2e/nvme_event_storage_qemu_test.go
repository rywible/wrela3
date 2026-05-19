package e2e

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryanwible/wrela3/compiler/storagefmt"
)

const nvmeStorageHotEventRegionOffset int64 = 1056768

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
	assertFirstAppendEventSlots(t, disk)
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

func assertFirstAppendEventSlots(t *testing.T, disk string) {
	t.Helper()
	f, err := os.Open(disk)
	if err != nil {
		t.Fatalf("open storage disk: %v", err)
	}
	defer f.Close()

	eventTypes := []uint32{1001, 1003}
	streamSequences := []uint64{1, 2}
	for i := uint64(0); i < 2; i++ {
		slot := make([]byte, storagefmt.EventSlotSize)
		if _, err := f.ReadAt(slot, nvmeStorageHotEventRegionOffset+int64(i*storagefmt.EventSlotSize)); err != nil {
			t.Fatalf("read hot event slot %d: %v", i, err)
		}
		assertFirstAppendEventSlot(t, slot, i, streamSequences[i], eventTypes[i], 2, uint32(i))
	}
}

func assertFirstAppendEventSlot(t *testing.T, slot []byte, eventID, streamSequence uint64, eventTypeID, atomicGroupLen, atomicGroupIndex uint32) {
	t.Helper()
	if got := binary.LittleEndian.Uint64(slot[0:8]); got != eventID {
		t.Fatalf("hot event slot event_id = %d, want %d", got, eventID)
	}
	if got := binary.LittleEndian.Uint64(slot[8:16]); got != 1 {
		t.Fatalf("hot event slot %d stream_id = %d, want 1", eventID, got)
	}
	if got := binary.LittleEndian.Uint64(slot[16:24]); got != streamSequence {
		t.Fatalf("hot event slot %d stream_sequence = %d, want %d", eventID, got, streamSequence)
	}
	if got := binary.LittleEndian.Uint32(slot[24:28]); got != eventTypeID {
		t.Fatalf("hot event slot %d event_type_id = %d, want %d", eventID, got, eventTypeID)
	}
	if got := binary.LittleEndian.Uint32(slot[28:32]); got != 1 {
		t.Fatalf("hot event slot %d payload_layout_id = %d, want 1", eventID, got)
	}
	if got := binary.LittleEndian.Uint32(slot[32:36]); got != atomicGroupLen {
		t.Fatalf("hot event slot %d atomic_group_len = %d, want %d", eventID, got, atomicGroupLen)
	}
	if got := binary.LittleEndian.Uint32(slot[36:40]); got != atomicGroupIndex {
		t.Fatalf("hot event slot %d atomic_group_index = %d, want %d", eventID, got, atomicGroupIndex)
	}
	if got := binary.LittleEndian.Uint32(slot[40:44]); got != 0 {
		t.Fatalf("hot event slot %d payload_length = %d, want 0", eventID, got)
	}
	if got := binary.LittleEndian.Uint16(slot[52:54]); got != 1 {
		t.Fatalf("hot event slot %d header_version = %d, want 1", eventID, got)
	}
	gotChecksum := binary.LittleEndian.Uint32(slot[storagefmt.Checksum32Offset : storagefmt.Checksum32Offset+4])
	if gotChecksum == 0 {
		t.Fatalf("hot event slot %d checksum32 is zero", eventID)
	}
	checksummed := append([]byte(nil), slot...)
	for i := uint64(0); i < 4; i++ {
		checksummed[storagefmt.Checksum32Offset+i] = 0
	}
	if want := storagefmt.CRC32C(checksummed); gotChecksum != want {
		t.Fatalf("hot event slot %d checksum32 = %#x, want CRC32C %#x", eventID, gotChecksum, want)
	}
}
