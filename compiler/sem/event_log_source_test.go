package sem

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/parse"
	"github.com/ryanwible/wrela3/compiler/source"
	"github.com/ryanwible/wrela3/compiler/storagefmt"
)

func TestEventLogSourceCompiles(t *testing.T) {
	modules := parseEventLogModules(t, `
module sem.event_log_consumer

use { BatchPacker, EventSlotWriter, ReservedEmptySlot } from storage.event_log

data EventLogConsumer {
    packer: BatchPacker
    writer: EventSlotWriter
    reserved: ReservedEmptySlot
}
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	_, ds := checkAllowingMissingImage(t, index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}
}

func TestEventLogBatchPackerMirrorContract(t *testing.T) {
	modules := parseEventLogModules(t, `
module sem.event_log_mirror

use { STORAGE_EVENT_SLOT_SIZE } from storage.format
use { BatchPacker, EventSlotWriter, ReservedEmptySlot } from storage.event_log

const MIRRORED_SLOT_SIZE: U64 = STORAGE_EVENT_SLOT_SIZE
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	checked, ds := checkAllowingMissingImage(t, index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}

	assertWrelaConstU64(t, checked.Index, "storage.format", "STORAGE_EVENT_SLOT_SIZE", storagefmt.EventSlotSize)
	assertMethodExists(t, moduleType(t, checked.Index, "storage.event_log", "BatchPacker"), "finish_batch")
	assertMethodExists(t, moduleType(t, checked.Index, "storage.event_log", "EventSlotWriter"), "slots_per_lba")
	assertMethodExists(t, moduleType(t, checked.Index, "storage.event_log", "ReservedEmptySlot"), "header")

	source := readRepoFile(t, "wrela/storage/event_log.wrela")
	wantBody := `    fn finish_batch(self, semantic_slots: U64) -> BatchPackingResult {
        let slots_per_lba = self.active_lba_size / STORAGE_EVENT_SLOT_SIZE
        let remainder = semantic_slots % slots_per_lba
        if remainder == 0 {
            return BatchPackingResult(semantic_slots = semantic_slots, reserved_empty_slots = 0, total_slot_positions = semantic_slots)
        }
        let empty = slots_per_lba - remainder
        return BatchPackingResult(semantic_slots = semantic_slots, reserved_empty_slots = empty, total_slot_positions = semantic_slots + empty)
    }`
	if !strings.Contains(source, wantBody) {
		t.Fatalf("BatchPacker.finish_batch body does not match Task 20 mirror contract")
	}
}

func TestEventLogSuperblockMirrorContract(t *testing.T) {
	modules := parseEventLogModules(t, `
module sem.event_log_superblock_mirror

use { SuperblockChoice, SuperblockPair, StorageRegionValidator } from storage.event_log

data SuperblockConsumer {
    choice: SuperblockChoice
    pair: SuperblockPair
    validator: StorageRegionValidator
}
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	checked, ds := checkAllowingMissingImage(t, index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}

	assertMethodExists(t, moduleType(t, checked.Index, "storage.event_log", "SuperblockPair"), "choose")
	assertMethodExists(t, moduleType(t, checked.Index, "storage.event_log", "StorageRegionValidator"), "validate_pair")
	assertTypeFields(t, moduleType(t, checked.Index, "storage.event_log", "SuperblockChoice"), map[string]string{
		"selected_generation": "U64",
		"valid":               "Bool",
	})

	source := readRepoFile(t, "wrela/storage/event_log.wrela")
	choose := sourceBetween(t, source, "fn choose(self) -> SuperblockChoice {", "\n    fn superblock_valid")
	for _, want := range []string{
		"let first_valid = self.superblock_valid(block = self.first)",
		"let second_valid = self.superblock_valid(block = self.second)",
		"if first_valid",
		"if second_valid",
		"valid = false",
	} {
		if !strings.Contains(choose, want) {
			t.Fatalf("SuperblockPair.choose missing checksum-validity branch %q", want)
		}
	}
	valid := sourceBetween(t, source, "fn superblock_valid(self, block: StorageSuperblock) -> Bool {", "\n    fn superblock_checksum")
	if !strings.Contains(valid, "block.checksum32 == self.superblock_checksum(generation = block.generation)") {
		t.Fatalf("SuperblockPair.superblock_valid must compare checksum32 against generation checksum")
	}
}

func TestEventLogRecoveryMirrorContract(t *testing.T) {
	modules := parseEventLogModules(t, `
module sem.event_log_recovery_mirror

use {
    STORAGE_RECOVERY_STOP_CLEAN_EOF,
    STORAGE_RECOVERY_STOP_CHECKSUM_MISMATCH,
    STORAGE_RECOVERY_STOP_INCOMPLETE_ATOMIC_GROUP,
    STORAGE_RECOVERY_STOP_INVALID_EMPTY_SLOT,
    EventRecoveryScanner,
    RecoveryResult
} from storage.event_log

const CLEAN_EOF: U64 = STORAGE_RECOVERY_STOP_CLEAN_EOF
const CHECKSUM_MISMATCH: U64 = STORAGE_RECOVERY_STOP_CHECKSUM_MISMATCH
const INCOMPLETE_ATOMIC_GROUP: U64 = STORAGE_RECOVERY_STOP_INCOMPLETE_ATOMIC_GROUP
const INVALID_EMPTY_SLOT: U64 = STORAGE_RECOVERY_STOP_INVALID_EMPTY_SLOT

data RecoveryConsumer {
    scanner: EventRecoveryScanner
    result: RecoveryResult
}
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	checked, ds := checkAllowingMissingImage(t, index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}

	assertTypeFields(t, moduleType(t, checked.Index, "storage.event_log", "RecoveryResult"), map[string]string{
		"visible_events":           "U64",
		"next_event_id":            "U64",
		"last_committed_group_end": "U64",
		"stop_reason":              "U64",
	})
	for _, name := range []string{
		"STORAGE_RECOVERY_STOP_CLEAN_EOF",
		"STORAGE_RECOVERY_STOP_CHECKSUM_MISMATCH",
		"STORAGE_RECOVERY_STOP_INCOMPLETE_ATOMIC_GROUP",
		"STORAGE_RECOVERY_STOP_INVALID_EMPTY_SLOT",
	} {
		if _, ok := checked.Index.LookupConst("storage.event_log", name); !ok {
			t.Fatalf("missing const storage.event_log.%s", name)
		}
	}
	assertMethodExists(t, moduleType(t, checked.Index, "storage.event_log", "EventRecoveryScanner"), "validate_group_member")
}

func TestEventLogSegmentMirrorContract(t *testing.T) {
	modules := parseEventLogModules(t, `
module sem.event_log_segment_mirror

use { EventSegment, EventSegmentMap, PackedSegmentCodec, SegmentIndexEntry } from storage.event_log

data SegmentConsumer {
    segment: EventSegment
    index: SegmentIndexEntry
    segment_map: EventSegmentMap
    codec: PackedSegmentCodec
}
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	checked, ds := checkAllowingMissingImage(t, index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}

	assertTypeFields(t, moduleType(t, checked.Index, "storage.event_log", "EventSegment"), map[string]string{
		"state":            "U64",
		"zone_start_lba":   "U64",
		"zone_block_count": "U64",
	})
	assertTypeFields(t, moduleType(t, checked.Index, "storage.event_log", "SegmentIndexEntry"), map[string]string{
		"event_id_delta": "U64",
	})
	assertMethodExists(t, moduleType(t, checked.Index, "storage.event_log", "PackedSegmentCodec"), "pack_slots")
	assertMethodExists(t, moduleType(t, checked.Index, "storage.event_log", "EventSegmentMap"), "contains_event")
}

func parseEventLogModules(t *testing.T, consumer string) []*ast.Module {
	t.Helper()
	paths := []string{
		repoPath(t, "wrela/lang/core.wrela"),
		repoPath(t, "wrela/storage/format.wrela"),
		repoPath(t, "wrela/storage/event_log.wrela"),
	}
	files := make([]*source.File, 0, len(paths)+1)
	for i, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		files = append(files, source.NewFile(source.FileID(i+1), path, string(raw)))
	}
	files = append(files, source.NewFile(source.FileID(len(files)+1), "event-log-consumer.wrela", consumer))
	modules, ds := parse.ParseGraph(source.Graph{Files: files})
	if len(ds) != 0 {
		t.Fatalf("parse diagnostics: %#v", ds)
	}
	return modules
}

func repoPath(t *testing.T, rel string) string {
	t.Helper()
	workdir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Join(filepath.Clean(filepath.Join(workdir, "..", "..")), rel)
}
