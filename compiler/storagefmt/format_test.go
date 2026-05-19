package storagefmt

import "testing"

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
