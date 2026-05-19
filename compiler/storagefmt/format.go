package storagefmt

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"sort"
)

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
	StorageSlotReservedEmpty   uint32 = 1

	SegmentStateOpenHot      uint8 = 1
	SegmentStateSealedHot    uint8 = 2
	SegmentStateCompressible uint8 = 3
	SegmentStateCompressed   uint8 = 4
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

type EventSlotHeader struct {
	EventID          uint64
	StreamID         uint64
	StreamSequence   uint64
	EventTypeID      uint32
	PayloadLayoutID  uint32
	AtomicGroupLen   uint32
	AtomicGroupIndex uint32
	PayloadLength    uint32
	Flags            uint32
	Checksum32       uint32
	HeaderVersion    uint16
	Reserved16       uint16
	Reserved64       uint64
}

type Slot struct {
	Header  EventSlotHeader
	Payload [EventPayloadBytes]byte
}

type RecoveryStopReason uint8

const (
	StopCleanEOF RecoveryStopReason = iota
	StopChecksumMismatch
	StopIncompleteAtomicGroup
	StopInvalidEmptySlot
)

type RecoveryResult struct {
	VisibleEvents         uint64
	NextEventID           uint64
	LastCommittedGroupEnd uint64
	StopReason            RecoveryStopReason
}

type EventSegment struct {
	FirstEventID   uint64
	LastEventID    uint64
	State          uint8
	ZoneStartLBA   uint64
	ZoneBlockCount uint64
}

type SegmentIndexEntry struct {
	EventIDDelta uint64
	ByteOffset   uint64
}

type StreamDirectory struct {
	NextStreamID uint64
}

type StreamDirectoryEntry struct {
	LatestSequence      uint64
	LatestEventID       uint64
	LatestCheckpointRef uint64
	Flags               uint64
}

type StreamCheckpoint struct {
	CurrentLayoutID uint64
	StateLayoutID   uint64
}

type StreamDirectoryCache struct {
	Hits   uint64
	Misses uint64
}

type PackedSegment struct {
	BaseEventID uint64
	Bytes       []byte
	Index       []SegmentIndexEntry
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

func RecoverSlots(slots []Slot) RecoveryResult {
	var result RecoveryResult
	var expectedEventID uint64

	for i := 0; i < len(slots); {
		slot := slots[i]
		if !slotChecksumValid(slot) {
			result.NextEventID = expectedEventID
			result.StopReason = StopChecksumMismatch
			return result
		}

		if slot.Header.EventTypeID == 0 {
			if !isReservedEmptySlot(slot.Header) {
				result.NextEventID = expectedEventID
				result.StopReason = StopInvalidEmptySlot
				return result
			}
			if slot.Header.EventID != expectedEventID {
				result.NextEventID = expectedEventID
				result.StopReason = StopChecksumMismatch
				return result
			}
			expectedEventID++
			result.NextEventID = expectedEventID
			i++
			continue
		}
		if slot.Header.EventID != expectedEventID {
			result.NextEventID = expectedEventID
			result.StopReason = StopChecksumMismatch
			return result
		}

		groupLen := slot.Header.AtomicGroupLen
		if groupLen == 0 {
			groupLen = 1
		}
		if i+int(groupLen) > len(slots) {
			result.NextEventID = expectedEventID
			result.StopReason = StopIncompleteAtomicGroup
			return result
		}

		for j := uint32(0); j < groupLen; j++ {
			member := slots[i+int(j)]
			if member.Header.EventID != expectedEventID+uint64(j) || !slotChecksumValid(member) {
				result.NextEventID = expectedEventID
				result.StopReason = StopChecksumMismatch
				return result
			}
			if member.Header.EventTypeID == 0 || member.Header.AtomicGroupLen != groupLen || member.Header.AtomicGroupIndex != j {
				result.NextEventID = expectedEventID
				result.StopReason = StopIncompleteAtomicGroup
				return result
			}
		}

		result.VisibleEvents += uint64(groupLen)
		expectedEventID += uint64(groupLen)
		result.NextEventID = expectedEventID
		result.LastCommittedGroupEnd = expectedEventID - 1
		i += int(groupLen)
	}

	result.StopReason = StopCleanEOF
	return result
}

func isReservedEmptySlot(header EventSlotHeader) bool {
	return header.EventTypeID == 0 &&
		header.PayloadLayoutID == 0 &&
		header.StreamID == 0 &&
		header.StreamSequence == 0 &&
		header.AtomicGroupLen == 0 &&
		header.AtomicGroupIndex == 0 &&
		header.PayloadLength == 0 &&
		header.Flags&StorageSlotReservedEmpty != 0
}

func slotChecksumValid(slot Slot) bool {
	return slot.Header.Checksum32 == SlotChecksum(slot)
}

func SlotChecksum(slot Slot) uint32 {
	return CRC32C(slotBytes(slot, 0))
}

func slotBytes(slot Slot, checksum uint32) []byte {
	bytes := make([]byte, EventSlotSize)
	binary.LittleEndian.PutUint64(bytes[0:], slot.Header.EventID)
	binary.LittleEndian.PutUint64(bytes[8:], slot.Header.StreamID)
	binary.LittleEndian.PutUint64(bytes[16:], slot.Header.StreamSequence)
	binary.LittleEndian.PutUint32(bytes[24:], slot.Header.EventTypeID)
	binary.LittleEndian.PutUint32(bytes[28:], slot.Header.PayloadLayoutID)
	binary.LittleEndian.PutUint32(bytes[32:], slot.Header.AtomicGroupLen)
	binary.LittleEndian.PutUint32(bytes[36:], slot.Header.AtomicGroupIndex)
	binary.LittleEndian.PutUint32(bytes[40:], slot.Header.PayloadLength)
	binary.LittleEndian.PutUint32(bytes[44:], slot.Header.Flags)
	binary.LittleEndian.PutUint32(bytes[48:], checksum)
	binary.LittleEndian.PutUint16(bytes[52:], slot.Header.HeaderVersion)
	binary.LittleEndian.PutUint16(bytes[54:], slot.Header.Reserved16)
	binary.LittleEndian.PutUint64(bytes[56:], slot.Header.Reserved64)
	copy(bytes[EventHeaderSize:], slot.Payload[:])
	return bytes
}

func RefreshSlotChecksum(slot *Slot) {
	slot.Header.Checksum32 = 0
	slot.Header.Checksum32 = SlotChecksum(*slot)
}

func ValidSlotForTest(eventID uint64) Slot {
	slot := Slot{Header: EventSlotHeader{
		EventID:          eventID,
		StreamID:         7,
		StreamSequence:   eventID,
		EventTypeID:      1001,
		PayloadLayoutID:  1,
		AtomicGroupLen:   1,
		AtomicGroupIndex: 0,
		PayloadLength:    0,
		HeaderVersion:    1,
	}}
	RefreshSlotChecksum(&slot)
	return slot
}

func (d StreamDirectory) EntryOffset(streamID uint64) uint64 {
	return streamID * 32
}

func (d StreamDirectory) Exists(streamID uint64) bool {
	return streamID < d.NextStreamID
}

func (d *StreamDirectory) AllocateStreamID() uint64 {
	streamID := d.NextStreamID
	d.NextStreamID++
	return streamID
}

func (e StreamDirectoryEntry) ExpectsSequence(sequence uint64) bool {
	return sequence == e.LatestSequence+1
}

func (c StreamCheckpoint) Applies() bool {
	return c.StateLayoutID == c.CurrentLayoutID
}

func (c *StreamDirectoryCache) RecordHit() {
	c.Hits++
}

func (c *StreamDirectoryCache) RecordMiss() {
	c.Misses++
}

func (c StreamDirectoryCache) HitRateX1000() uint64 {
	total := c.Hits + c.Misses
	if total == 0 {
		return 0
	}
	return c.Hits * 1000 / total
}

func ReservedEmptySlotForTest(eventID uint64) Slot {
	slot := Slot{Header: EventSlotHeader{
		EventID:       eventID,
		Flags:         StorageSlotReservedEmpty,
		HeaderVersion: 1,
	}}
	RefreshSlotChecksum(&slot)
	return slot
}

func PackSlots(slots []Slot, stride uint64) PackedSegment {
	if len(slots) == 0 {
		return PackedSegment{}
	}
	if stride == 0 {
		stride = 1
	}

	baseEventID := slots[0].Header.EventID
	packed := PackedSegment{BaseEventID: baseEventID}
	for _, slot := range slots {
		payloadLength := uint64(slot.Header.PayloadLength)
		if payloadLength > EventPayloadBytes {
			payloadLength = EventPayloadBytes
		}
		delta := slot.Header.EventID - baseEventID
		if delta%stride == 0 {
			packed.Index = append(packed.Index, SegmentIndexEntry{
				EventIDDelta: delta,
				ByteOffset:   uint64(len(packed.Bytes)),
			})
		}
		raw := slotBytes(slot, slot.Header.Checksum32)
		packed.Bytes = append(packed.Bytes, raw[:EventHeaderSize+payloadLength]...)
	}
	return packed
}

func FindSegmentForEventID(segments []EventSegment, eventID uint64) (EventSegment, bool) {
	for _, segment := range segments {
		if eventID >= segment.FirstEventID && eventID <= segment.LastEventID {
			return segment, true
		}
	}
	return EventSegment{}, false
}

type Region struct {
	Name         string
	Offset, Size uint64
}

type Superblock struct {
	Generation uint64
	Checksum32 uint32
}

var (
	ErrNoValidSuperblock = errors.New("no valid storage superblock")
	ErrRegionOverlap     = errors.New("storage region overlap or invalid size")
)

func (s Superblock) Valid() bool {
	return s.Checksum32 == SuperblockChecksum(s.Generation)
}

func SuperblockChecksum(generation uint64) uint32 {
	var data [8]byte
	binary.LittleEndian.PutUint64(data[:], generation)
	return CRC32C(data[:])
}

func ChooseSuperblock(a, b Superblock) (Superblock, error) {
	av := a.Valid()
	bv := b.Valid()
	if av && bv && b.Generation > a.Generation {
		return b, nil
	}
	if av {
		return a, nil
	}
	if bv {
		return b, nil
	}
	return Superblock{}, ErrNoValidSuperblock
}

func ValidateRegions(regions []Region) error {
	ordered := append([]Region(nil), regions...)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].Offset < ordered[j].Offset
	})

	var prevEnd uint64
	for _, region := range ordered {
		if region.Size == 0 || region.Offset < prevEnd {
			return ErrRegionOverlap
		}
		end := region.Offset + region.Size
		if end < region.Offset || end > StorageDiskBytes {
			return ErrRegionOverlap
		}
		prevEnd = end
	}
	return nil
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
