package storagefmt

import (
	"encoding/binary"
	"testing"
)

func TestCRC32CVector(t *testing.T) {
	got := CRC32C([]byte("123456789"))
	const want uint32 = 0xE3069283
	if got != want {
		t.Fatalf("CRC32C vector mismatch: got %#08x, want %#08x", got, want)
	}
}

func TestFourKiBUnderfillConsumesEmptySlots(t *testing.T) {
	got := FinishBatch(4096, 3)

	if got.SemanticSlots != 3 {
		t.Fatalf("SemanticSlots = %d, want 3", got.SemanticSlots)
	}
	if got.ReservedEmptySlots != 5 {
		t.Fatalf("ReservedEmptySlots = %d, want 5", got.ReservedEmptySlots)
	}
	if got.TotalSlotPositions != 8 {
		t.Fatalf("TotalSlotPositions = %d, want 8", got.TotalSlotPositions)
	}
}

func TestFiveHundredTwelveByteLBAPacksOneSlotPerBlock(t *testing.T) {
	got := FinishBatch(512, 3)

	if got.SemanticSlots != 3 {
		t.Fatalf("SemanticSlots = %d, want 3", got.SemanticSlots)
	}
	if got.ReservedEmptySlots != 0 {
		t.Fatalf("ReservedEmptySlots = %d, want 0", got.ReservedEmptySlots)
	}
	if got.TotalSlotPositions != 3 {
		t.Fatalf("TotalSlotPositions = %d, want 3", got.TotalSlotPositions)
	}
}

func TestReservedEmptySlotHeader(t *testing.T) {
	const (
		eventID             uint64 = 42
		reservedFlag        uint32 = 1
		eventIDOffset              = 0
		flagsOffset                = 44
		headerVersionOffset        = 52
	)

	header := make([]byte, EventHeaderSize)
	binary.LittleEndian.PutUint64(header[eventIDOffset:], eventID)
	binary.LittleEndian.PutUint32(header[EventTypeIDOffset:], 0)
	binary.LittleEndian.PutUint32(header[PayloadLayoutIDOffset:], 0)
	binary.LittleEndian.PutUint32(header[flagsOffset:], reservedFlag)
	binary.LittleEndian.PutUint16(header[headerVersionOffset:], 1)
	checksum := CRC32C(header[:Checksum32Offset])
	binary.LittleEndian.PutUint32(header[Checksum32Offset:], checksum)

	if got := binary.LittleEndian.Uint64(header[eventIDOffset:]); got != eventID {
		t.Fatalf("event_id = %d, want %d", got, eventID)
	}
	if got := binary.LittleEndian.Uint32(header[EventTypeIDOffset:]); got != 0 {
		t.Fatalf("event_type_id = %d, want 0", got)
	}
	if got := binary.LittleEndian.Uint32(header[PayloadLayoutIDOffset:]); got != 0 {
		t.Fatalf("payload_layout_id = %d, want 0", got)
	}
	if got := binary.LittleEndian.Uint32(header[flagsOffset:]); got != reservedFlag {
		t.Fatalf("flags = %d, want reserved empty flag %d", got, reservedFlag)
	}
	if got := binary.LittleEndian.Uint32(header[Checksum32Offset:]); got != CRC32C(header[:Checksum32Offset]) {
		t.Fatalf("checksum32 = %#x, want CRC32C(header prefix)", got)
	}
}

func TestRegionLayoutFitsSparse4GiBDisk(t *testing.T) {
	if EventSlotSize != 512 {
		t.Fatalf("EventSlotSize = %d, want 512", EventSlotSize)
	}
	if EventHeaderSize != 64 {
		t.Fatalf("EventHeaderSize = %d, want 64", EventHeaderSize)
	}
	if EventPayloadBytes != 448 {
		t.Fatalf("EventPayloadBytes = %d, want 448", EventPayloadBytes)
	}
	if EventTypeIDOffset != 24 {
		t.Fatalf("EventTypeIDOffset = %d, want 24", EventTypeIDOffset)
	}
	if PayloadLayoutIDOffset != 28 {
		t.Fatalf("PayloadLayoutIDOffset = %d, want 28", PayloadLayoutIDOffset)
	}
	if Checksum32Offset != 48 {
		t.Fatalf("Checksum32Offset = %d, want 48", Checksum32Offset)
	}
	if StorageHotSegmentBytes != 536870912 {
		t.Fatalf("StorageHotSegmentBytes = %d, want 536870912", StorageHotSegmentBytes)
	}

	regions := DefaultRegions()
	want := []Region{
		{Name: "superblock copies", Offset: 0, Size: 8192},
		{Name: "region map", Offset: 8192, Size: 1048576},
		{Name: "hot event slot region", Offset: 1056768, Size: 536870912},
		{Name: "segment map", Offset: 537927680, Size: 67108864},
		{Name: "sealed segment extents", Offset: 605036544, Size: 536870912},
		{Name: "stream directory and cache chunks", Offset: 1141907456, Size: 268435456},
		{Name: "blob extents", Offset: 1410342912, Size: 1610612736},
		{Name: "blob manifests and key metadata", Offset: 3020955648, Size: 268435456},
		{Name: "projection storage", Offset: 3289391104, Size: 536870912},
		{Name: "maintenance metadata", Offset: 3826262016, Size: 268435456},
		{Name: "reserved tail", Offset: 4094697472, Size: 72159232},
	}

	if len(regions) != len(want) {
		t.Fatalf("DefaultRegions length = %d, want %d", len(regions), len(want))
	}

	var prevEnd uint64
	for i, region := range regions {
		if region != want[i] {
			t.Fatalf("DefaultRegions()[%d] = %+v, want %+v", i, region, want[i])
		}
		if region.Offset < prevEnd {
			t.Fatalf("%q overlaps previous region: offset %d < previous end %d", region.Name, region.Offset, prevEnd)
		}
		end := region.Offset + region.Size
		if end < region.Offset {
			t.Fatalf("%q overflows uint64: offset %d size %d", region.Name, region.Offset, region.Size)
		}
		if end > StorageDiskBytes {
			t.Fatalf("%q ends at %d beyond disk size %d", region.Name, end, StorageDiskBytes)
		}
		prevEnd = end
	}

	hot := regions[2]
	if hot.Name != "hot event slot region" || hot.Offset != 1056768 || hot.Size != 536870912 {
		t.Fatalf("hot event region = %+v, want offset 1056768 size 536870912", hot)
	}
}

func TestChooseSuperblockHighestValidGeneration(t *testing.T) {
	a := validSuperblockForTest(1)
	b := validSuperblockForTest(2)

	got, err := ChooseSuperblock(a, b)
	if err != nil {
		t.Fatalf("ChooseSuperblock() error = %v", err)
	}
	if got.Generation != 2 {
		t.Fatalf("selected generation = %d, want 2", got.Generation)
	}
}

func TestChooseSuperblockIgnoresInvalidChecksum(t *testing.T) {
	a := validSuperblockForTest(1)
	b := validSuperblockForTest(3)
	b.Checksum32++

	got, err := ChooseSuperblock(a, b)
	if err != nil {
		t.Fatalf("ChooseSuperblock() error = %v", err)
	}
	if got.Generation != 1 {
		t.Fatalf("selected generation = %d, want 1", got.Generation)
	}
}

func TestRegionOverlap(t *testing.T) {
	err := ValidateRegions([]Region{
		{Name: "a", Offset: 0, Size: 10},
		{Name: "b", Offset: 9, Size: 1},
	})
	if err != ErrRegionOverlap {
		t.Fatalf("ValidateRegions() error = %v, want %v", err, ErrRegionOverlap)
	}

	if err := ValidateRegions(DefaultRegions()); err != nil {
		t.Fatalf("ValidateRegions(DefaultRegions()) error = %v", err)
	}
}

func validSuperblockForTest(generation uint64) Superblock {
	return Superblock{Generation: generation, Checksum32: SuperblockChecksum(generation)}
}
