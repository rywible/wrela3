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

func TestProjectionCannotAdvancePastFrontier(t *testing.T) {
	state := ProjectionTruth{AtomicGroupFrontier: 10}
	ok := state.AcceptAdvance(AdvanceProjection{ProjectionID: 12, ThroughEventID: 11})
	if ok {
		t.Fatal("projection advanced past frontier")
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

func TestChooseSuperblockRejectsBothInvalidChecksums(t *testing.T) {
	a := validSuperblockForTest(1)
	b := validSuperblockForTest(3)
	a.Checksum32++
	b.Checksum32++

	if _, err := ChooseSuperblock(a, b); err != ErrNoValidSuperblock {
		t.Fatalf("ChooseSuperblock() error = %v, want %v", err, ErrNoValidSuperblock)
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

func TestRecoveryStopsBeforeChecksumMismatch(t *testing.T) {
	good := ValidSlotForTest(0)
	bad := ValidSlotForTest(1)
	bad.Header.Checksum32++

	got := RecoverSlots([]Slot{good, bad})
	if got.StopReason != StopChecksumMismatch {
		t.Fatalf("stop reason = %v, want checksum mismatch", got.StopReason)
	}
	if got.VisibleEvents != 1 || got.NextEventID != 1 || got.LastCommittedGroupEnd != 0 {
		t.Fatalf("recovery = %#v, want one visible event before mismatch", got)
	}
}

func TestRecoveryStopsBeforeIncompleteAtomicGroup(t *testing.T) {
	first := ValidSlotForTest(0)
	first.Header.AtomicGroupLen = 2
	first.Header.AtomicGroupIndex = 0
	RefreshSlotChecksum(&first)

	got := RecoverSlots([]Slot{first})
	if got.StopReason != StopIncompleteAtomicGroup {
		t.Fatalf("stop reason = %v, want incomplete atomic group", got.StopReason)
	}
	if got.VisibleEvents != 0 || got.NextEventID != 0 {
		t.Fatalf("recovery = %#v, want no visible events", got)
	}
}

func TestRecoveryRejectsZeroLengthAtomicGroup(t *testing.T) {
	slot := ValidSlotForTest(0)
	slot.Header.AtomicGroupLen = 0
	RefreshSlotChecksum(&slot)

	got := RecoverSlots([]Slot{slot})
	if got.StopReason != StopIncompleteAtomicGroup {
		t.Fatalf("stop reason = %v, want incomplete atomic group", got.StopReason)
	}
	if got.VisibleEvents != 0 || got.NextEventID != 0 {
		t.Fatalf("recovery = %#v, want stop before zero-length group", got)
	}
}

func TestRecoveryRejectsEmptySlotOutsidePadding(t *testing.T) {
	slot := ValidSlotForTest(7)
	slot.Header.EventTypeID = 0
	slot.Header.Flags = 0
	RefreshSlotChecksum(&slot)

	got := RecoverSlots([]Slot{slot})
	if got.StopReason != StopInvalidEmptySlot || got.VisibleEvents != 0 {
		t.Fatalf("recovery = %#v, want invalid empty slot", got)
	}
}

func TestRecoverySkipsReservedEmptySlots(t *testing.T) {
	first := ValidSlotForTest(0)
	padding := ReservedEmptySlotForTest(1)
	second := ValidSlotForTest(2)

	got := RecoverSlots([]Slot{first, padding, second})
	if got.StopReason != StopCleanEOF {
		t.Fatalf("stop reason = %v, want clean EOF", got.StopReason)
	}
	if got.VisibleEvents != 2 || got.NextEventID != 3 || got.LastCommittedGroupEnd != 2 {
		t.Fatalf("recovery = %#v, want reserved empty skipped", got)
	}
}

func TestPackedSegmentCodecStripsPadding(t *testing.T) {
	slot := ValidSlotForTest(0)
	slot.Header.PayloadLength = 12
	for i := uint32(0); i < slot.Header.PayloadLength; i++ {
		slot.Payload[i] = byte(i + 1)
	}
	RefreshSlotChecksum(&slot)

	packed := PackSlots([]Slot{slot}, 16)
	if got, want := len(packed.Bytes), int(EventHeaderSize+12); got != want {
		t.Fatalf("packed bytes = %d, want %d", got, want)
	}
	if len(packed.Index) != 1 || packed.Index[0].EventIDDelta != 0 {
		t.Fatalf("packed index = %#v", packed.Index)
	}
}

func TestBlobAllocatorSplitsAndCoalesces(t *testing.T) {
	a := NewFreeExtentList(1024)
	a.Free(Extent{StartLBA: 100, BlockCount: 10})

	got := a.Allocate(4)
	if got.StartLBA != 100 || got.BlockCount != 4 {
		t.Fatalf("allocated = %#v, want start 100 block count 4", got)
	}
	remaining := a.Extents()
	if len(remaining) != 1 || remaining[0].StartLBA != 104 || remaining[0].BlockCount != 6 {
		t.Fatalf("remaining free list = %#v, want one extent at 104 count 6", remaining)
	}

	a.Free(got)
	if len(a.Extents()) != 1 || a.Extents()[0].StartLBA != 100 || a.Extents()[0].BlockCount != 10 {
		t.Fatalf("free list did not coalesce: %#v", a.Extents())
	}

	capacity := NewFreeExtentList(1024)
	for i := uint64(0); i < 1024; i++ {
		capacity.Free(Extent{StartLBA: i * 2, BlockCount: 1})
	}
	capacity.Free(Extent{StartLBA: 4096, BlockCount: 1})
	if got := len(capacity.Extents()); got != 1024 {
		t.Fatalf("free list capacity = %d, want 1024", got)
	}
}

func TestOrphanCollectorUsesAcknowledgedBlobRefs(t *testing.T) {
	collector := NewOrphanCollector([]Extent{
		{StartLBA: 10, BlockCount: 2},
		{StartLBA: 20, BlockCount: 2},
		{StartLBA: 30, BlockCount: 2},
	})
	collector.MarkAcknowledged(BlobRefForExtent(10, 2))
	collector.MarkAcknowledged(BlobRefForExtent(30, 2))
	collector.MarkUnacknowledged(BlobRefForExtent(30, 2))

	got := collector.Reclaimable()
	want := []Extent{
		{StartLBA: 20, BlockCount: 2},
		{StartLBA: 30, BlockCount: 2},
	}
	if len(got) != len(want) {
		t.Fatalf("reclaimable = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("reclaimable[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func TestRelocateBlobRejectsStaleVersion(t *testing.T) {
	writer := BlobTruth{Version: 4, Ref: BlobRef{BlobID: 9}}
	ok := writer.AcceptRelocate(RelocateBlobProposal{BlobID: 9, OldRef: writer.Ref, NewRef: BlobRef{BlobID: 9}, ObservedVersion: 3})
	if ok {
		t.Fatal("stale relocation must be rejected")
	}
}

func TestRelocateBlobRejectsStaleOldRef(t *testing.T) {
	truth := BlobTruth{Version: 4, Ref: BlobRef{BlobID: 9, StartLBA: 20, BlockCount: 2}}
	ok := truth.AcceptRelocate(RelocateBlobProposal{
		BlobID:          9,
		OldRef:          BlobRef{BlobID: 9, StartLBA: 10, BlockCount: 2},
		NewRef:          BlobRef{BlobID: 9, StartLBA: 30, BlockCount: 2},
		ObservedVersion: 4,
	})
	if ok {
		t.Fatal("stale old ref must be rejected")
	}
}

func TestRelocateBlobAcceptsOnlyCurrentTruth(t *testing.T) {
	truth := BlobTruth{Version: 4, Ref: BlobRef{BlobID: 9, StartLBA: 20, BlockCount: 2}}
	writer := WriterPolicy{}

	staleVersion := writer.AcceptRelocateBlob(&truth, RelocateBlobProposal{
		BlobID:          9,
		OldRef:          truth.Ref,
		NewRef:          BlobRef{BlobID: 9, StartLBA: 30, BlockCount: 2},
		ObservedVersion: 3,
	})
	if staleVersion.Accepted {
		t.Fatalf("stale version relocation accepted: %#v", staleVersion)
	}

	staleOldRef := writer.AcceptRelocateBlob(&truth, RelocateBlobProposal{
		BlobID:          9,
		OldRef:          BlobRef{BlobID: 9, StartLBA: 10, BlockCount: 2},
		NewRef:          BlobRef{BlobID: 9, StartLBA: 30, BlockCount: 2},
		ObservedVersion: 4,
	})
	if staleOldRef.Accepted {
		t.Fatalf("stale old ref relocation accepted: %#v", staleOldRef)
	}

	current := writer.AcceptRelocateBlob(&truth, RelocateBlobProposal{
		BlobID:          9,
		OldRef:          truth.Ref,
		NewRef:          BlobRef{BlobID: 9, StartLBA: 30, BlockCount: 2},
		ObservedVersion: 4,
	})
	if !current.Accepted || truth.Ref.StartLBA != 30 || truth.Version != 5 {
		t.Fatalf("current relocation = %#v, truth = %#v", current, truth)
	}
}

func TestDirectoryProjectionPublishesGroupWatermark(t *testing.T) {
	p := DirectoryProjection{}
	p.ApplyGroup(CommittedGroup{LastEventID: 9}, []Event{{TypeID: 1001, FileID: 7}})
	if p.Watermark != 9 {
		t.Fatalf("watermark = %d, want 9", p.Watermark)
	}
}

func TestDirectoryProjectionAppliesKnownFileEvents(t *testing.T) {
	p := DirectoryProjection{}
	p.ApplyGroup(CommittedGroup{LastEventID: 4}, []Event{
		{TypeID: 1001, FileID: 7, ParentID: 1, NameRef: 11, StreamSequence: 1},
		{TypeID: 1002, FileID: 7, ParentID: 2, NameRef: 12, StreamSequence: 2},
		{TypeID: 1003, FileID: 7, BlobRef: BlobRef{BlobID: 99}, StreamSequence: 3},
		{TypeID: 1004, FileID: 7, StreamSequence: 4},
		{TypeID: 9999, FileID: 8},
	})
	state, ok := p.Files[7]
	if !ok {
		t.Fatal("file 7 missing")
	}
	if state.ParentID != 2 || state.NameRef != 12 || state.CurrentBlobRef.BlobID != 99 || !state.Deleted || state.StreamSequence != 4 {
		t.Fatalf("file state = %#v", state)
	}
	if _, ok := p.Files[8]; ok {
		t.Fatalf("unknown event created file 8: %#v", p.Files[8])
	}
}

func TestStorageWriterRejectsOversizedAtomicGroup(t *testing.T) {
	writer := WriterPolicy{}
	got := writer.EnqueueAtomicGroup(StorageMaxAtomicGroupSlots + 1)
	if got.Accepted || got.RejectCode != "SEM0114" {
		t.Fatalf("enqueue = %#v, want SEM0114 rejection", got)
	}
}

func TestStorageWriterRejectsZeroAtomicGroup(t *testing.T) {
	writer := WriterPolicy{NextEventID: 7, OpenBatchSlots: 3}
	got := writer.EnqueueAtomicGroup(0)
	if got.Accepted || got.RejectCode != "SEM0114" {
		t.Fatalf("enqueue = %#v, want SEM0114 rejection", got)
	}
	if writer.NextEventID != 7 || writer.OpenBatchSlots != 3 {
		t.Fatalf("writer state mutated on rejection: %#v", writer)
	}
}

func TestStorageWriterBatchOverflowDoesNotSplitGroup(t *testing.T) {
	writer := WriterPolicy{OpenBatchSlots: 63}
	got := writer.EnqueueAtomicGroup(2)
	if !got.Accepted || got.OpenBatchSlots != 65 || !got.FlushRequested {
		t.Fatalf("enqueue = %#v", got)
	}
}

func TestStorageWriterRequestsFlushAtTargetBoundary(t *testing.T) {
	writer := WriterPolicy{OpenBatchSlots: 62}
	got := writer.EnqueueAtomicGroup(2)
	if !got.Accepted || got.OpenBatchSlots != StorageTargetBatchSlots || !got.FlushRequested {
		t.Fatalf("enqueue = %#v, want flush at target boundary", got)
	}
}

func TestStorageWriterStartsNewBatchAfterMaxOverflow(t *testing.T) {
	writer := WriterPolicy{NextEventID: 10, OpenBatchSlots: 70}
	got := writer.EnqueueAtomicGroup(3)
	if !got.Accepted || got.FirstEventID != 10 || got.LastEventID != 12 {
		t.Fatalf("enqueue ids = %#v, want 10..12", got)
	}
	if got.OpenBatchSlots != 3 || !got.FlushRequested {
		t.Fatalf("enqueue batch state = %#v, want new open batch with flush signal", got)
	}
	if writer.NextEventID != 13 || writer.OpenBatchSlots != 3 {
		t.Fatalf("writer state = %#v, want next_event_id=13 open_batch_slots=3", writer)
	}
}

func TestStorageWriterFirstAtomicGroupStartsAtZero(t *testing.T) {
	writer := WriterPolicy{}
	got := writer.EnqueueAtomicGroup(2)
	if !got.Accepted || got.FirstEventID != 0 || got.LastEventID != 1 {
		t.Fatalf("enqueue = %#v, want event ids 0..1", got)
	}
	first, last := AssignEventIDs(9, 3)
	if first != 9 || last != 11 {
		t.Fatalf("AssignEventIDs = %d, %d; want 9, 11", first, last)
	}
}

func TestStorageWriterAdvancesEventIDsAcrossAppends(t *testing.T) {
	writer := WriterPolicy{}
	first := writer.EnqueueAtomicGroup(2)
	second := writer.EnqueueAtomicGroup(3)
	if !first.Accepted || !second.Accepted {
		t.Fatalf("appends rejected: first=%#v second=%#v", first, second)
	}
	if first.FirstEventID != 0 || first.LastEventID != 1 {
		t.Fatalf("first append = %#v, want event ids 0..1", first)
	}
	if second.FirstEventID != 2 || second.LastEventID != 4 {
		t.Fatalf("second append = %#v, want event ids 2..4", second)
	}
	if writer.NextEventID != 5 || writer.OpenBatchSlots != 5 {
		t.Fatalf("writer state = %#v, want next_event_id=5 open_batch_slots=5", writer)
	}
}

func TestStreamDirectoryMath(t *testing.T) {
	dir := StreamDirectory{NextStreamID: 8}
	if !dir.Exists(7) || dir.Exists(8) {
		t.Fatalf("stream existence broken")
	}
	if got := dir.EntryOffset(5); got != 160 {
		t.Fatalf("entry offset = %d, want 160", got)
	}
}

func TestStreamDirectoryAllocatesMonotonicIDs(t *testing.T) {
	dir := StreamDirectory{NextStreamID: 9}
	first := dir.AllocateStreamID()
	second := dir.AllocateStreamID()

	if first != 9 || second != 10 || dir.NextStreamID != 11 {
		t.Fatalf("allocated (%d, %d), next = %d; want (9, 10), next 11", first, second, dir.NextStreamID)
	}
}

func TestStreamDirectoryEntryExpectedSequence(t *testing.T) {
	entry := StreamDirectoryEntry{LatestSequence: 41}
	if !entry.ExpectsSequence(42) || entry.ExpectsSequence(41) {
		t.Fatalf("expected sequence validation broken")
	}
}

func TestStreamDirectoryCheckpointIgnoresStaleLayout(t *testing.T) {
	checkpoint := StreamCheckpoint{CurrentLayoutID: 7, StateLayoutID: 6}
	if checkpoint.Applies() {
		t.Fatalf("stale checkpoint should be ignored")
	}

	checkpoint.StateLayoutID = 7
	if !checkpoint.Applies() {
		t.Fatalf("checkpoint with current layout should apply")
	}
}

func TestStreamDirectoryCacheHitRateX1000(t *testing.T) {
	cache := StreamDirectoryCache{}
	cache.RecordHit()
	cache.RecordHit()
	cache.RecordMiss()

	if got := cache.HitRateX1000(); got != 666 {
		t.Fatalf("hit rate x1000 = %d, want 666", got)
	}
}
