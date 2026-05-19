package storagefmt

import "hash/crc32"

const (
	EventSlotSize         uint64 = 512
	EventHeaderSize       uint64 = 64
	EventPayloadBytes     uint64 = 448
	EventTypeIDOffset     uint64 = 24
	PayloadLayoutIDOffset uint64 = 28
	Checksum32Offset      uint64 = 48

	StorageDiskBytes           uint64 = 4 * 1024 * 1024 * 1024
	StorageHotSegmentBytes     uint64 = 536870912
	StorageTargetBatchSlots    uint64 = 64
	StorageMaxOverflowSlots    uint64 = 8
	StorageMaxBatchSlots       uint64 = 72
	StorageMaxAtomicGroupSlots uint64 = 32
)

var crc32cTable = crc32.MakeTable(crc32.Castagnoli)

func CRC32C(data []byte) uint32 {
	return crc32.Checksum(data, crc32cTable)
}

type BatchPacking struct {
	SemanticSlots      uint64
	ReservedEmptySlots uint64
	TotalSlotPositions uint64
}

func FinishBatch(activeLBASize, semanticSlots uint64) BatchPacking {
	slotsPerLBA := activeLBASize / EventSlotSize
	totalSlotPositions := semanticSlots
	if slotsPerLBA > 0 {
		remainder := semanticSlots % slotsPerLBA
		if remainder != 0 {
			totalSlotPositions += slotsPerLBA - remainder
		}
	}

	return BatchPacking{
		SemanticSlots:      semanticSlots,
		ReservedEmptySlots: totalSlotPositions - semanticSlots,
		TotalSlotPositions: totalSlotPositions,
	}
}

type Region struct {
	Name         string
	Offset, Size uint64
}

func DefaultRegions() []Region {
	return []Region{
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
}
