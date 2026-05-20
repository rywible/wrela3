package sem

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/parse"
	"github.com/ryanwible/wrela3/compiler/source"
	"github.com/ryanwible/wrela3/compiler/storagefmt"
)

func TestStorageFormatSourceCompiles(t *testing.T) {
	modules := parseStorageFormatModules(t, `
module sem.storage_format_consumer

use {
    STORAGE_EVENT_SLOT_SIZE,
    STORAGE_EVENT_HEADER_SIZE,
    STORAGE_EVENT_PAYLOAD_BYTES,
    STORAGE_TARGET_BATCH_SLOTS,
    STORAGE_MAX_OVERFLOW_SLOTS,
    STORAGE_MAX_BATCH_SLOTS,
    STORAGE_MAX_ATOMIC_GROUP_SLOTS,
    STORAGE_GROUP_COMMIT_TIMER_US,
    STORAGE_HOT_SEGMENT_SLOTS,
    STORAGE_HOT_SEGMENT_BYTES,
    EventSlotHeader,
    StreamDirectoryEntry,
    StorageMetrics
} from storage.format

const EVENT_SLOT_SIZE: U64 = sizeof(EventSlotHeader) + STORAGE_EVENT_PAYLOAD_BYTES
const STREAM_ENTRY_SIZE: U64 = sizeof(StreamDirectoryEntry)

static_assert(EVENT_SLOT_SIZE == STORAGE_EVENT_SLOT_SIZE, message = "event slot size")
static_assert(sizeof(EventSlotHeader) == STORAGE_EVENT_HEADER_SIZE, message = "event header size")
static_assert(STREAM_ENTRY_SIZE == 32, message = "stream directory entry size")
static_assert(STORAGE_MAX_BATCH_SLOTS == STORAGE_TARGET_BATCH_SLOTS + STORAGE_MAX_OVERFLOW_SLOTS, message = "batch slot limit")

data StorageFormatConsumer {
    metrics: StorageMetrics
}
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	checked, ds := checkAllowingMissingImage(t, index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}

	assertWrelaConstU64(t, checked.Index, "storage.format", "STORAGE_EVENT_SLOT_SIZE", storagefmt.EventSlotSize)
	assertWrelaConstU64(t, checked.Index, "storage.format", "STORAGE_EVENT_HEADER_SIZE", storagefmt.EventHeaderSize)
	assertWrelaConstU64(t, checked.Index, "storage.format", "STORAGE_EVENT_PAYLOAD_BYTES", storagefmt.EventPayloadBytes)
	assertWrelaConstU64(t, checked.Index, "storage.format", "STORAGE_TARGET_BATCH_SLOTS", storagefmt.StorageTargetBatchSlots)
	assertWrelaConstU64(t, checked.Index, "storage.format", "STORAGE_MAX_OVERFLOW_SLOTS", storagefmt.StorageMaxOverflowSlots)
	assertWrelaConstU64(t, checked.Index, "storage.format", "STORAGE_MAX_BATCH_SLOTS", storagefmt.StorageMaxBatchSlots)
	assertWrelaConstU64(t, checked.Index, "storage.format", "STORAGE_MAX_ATOMIC_GROUP_SLOTS", storagefmt.StorageMaxAtomicGroupSlots)
	assertWrelaConstU64(t, checked.Index, "storage.format", "STORAGE_HOT_SEGMENT_BYTES", storagefmt.StorageHotSegmentBytes)

	entry := moduleType(t, checked.Index, "storage.format", "StreamDirectoryEntry")
	assertTypeFields(t, entry, map[string]string{
		"latest_sequence":       "U64",
		"latest_event_id":       "U64",
		"latest_checkpoint_ref": "U64",
		"flags":                 "U64",
	})

	metrics := moduleType(t, checked.Index, "storage.format", "StorageMetrics")
	assertTypeFields(t, metrics, map[string]string{
		"foreground_path_owner":               "U64",
		"foreground_path_queue_id":            "U64",
		"foreground_path_vector":              "U64",
		"background_path_owner":               "U64",
		"background_path_queue_id":            "U64",
		"background_path_vector":              "U64",
		"selected_durability_mode":            "U64",
		"event_slot_size":                     "U64",
		"event_header_size":                   "U64",
		"event_payload_bytes":                 "U64",
		"event_slots_written":                 "U64",
		"event_slots_reserved_empty":          "U64",
		"event_slots_recovered":               "U64",
		"blob_orphan_bytes":                   "U64",
		"projection_lag_events":               "U64",
		"stream_directory_cache_hits":         "U64",
		"stream_directory_cache_misses":       "U64",
		"stream_directory_cache_hit_rate_ppm": "U64",
		"device_media_write_commands":         "U64",
		"device_media_write_bytes":            "U64",
		"target_batch_slots":                  "U64",
		"max_batch_slots":                     "U64",
		"batches_submitted":                   "U64",
		"batch_overflow_count":                "U64",
		"append_latency_us":                   "U64",
		"durability_latency_us":               "U64",
		"admin_queue_depth":                   "U64",
		"foreground_io_queue_depth":           "U64",
		"background_io_queue_depth":           "U64",
		"core_link_committed_groups":          "U64",
		"core_link_backpressure_count":        "U64",
		"event_upcast_count":                  "U64",
		"projection_upcast_count":             "U64",
		"projection_rebuild_count":            "U64",
	})
}

func TestStorageFormatMirrorContract(t *testing.T) {
	modules := parseStorageFormatModules(t, `
module sem.storage_format_mirror

use { STORAGE_EVENT_SLOT_SIZE } from storage.format

const MIRRORED_SLOT_SIZE: U64 = STORAGE_EVENT_SLOT_SIZE
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	checked, ds := checkAllowingMissingImage(t, index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}

	for name, want := range map[string]uint64{
		"STORAGE_EVENT_SLOT_SIZE":        storagefmt.EventSlotSize,
		"STORAGE_EVENT_HEADER_SIZE":      storagefmt.EventHeaderSize,
		"STORAGE_EVENT_PAYLOAD_BYTES":    storagefmt.EventPayloadBytes,
		"STORAGE_TARGET_BATCH_SLOTS":     storagefmt.StorageTargetBatchSlots,
		"STORAGE_MAX_OVERFLOW_SLOTS":     storagefmt.StorageMaxOverflowSlots,
		"STORAGE_MAX_BATCH_SLOTS":        storagefmt.StorageMaxBatchSlots,
		"STORAGE_MAX_ATOMIC_GROUP_SLOTS": storagefmt.StorageMaxAtomicGroupSlots,
		"STORAGE_HOT_SEGMENT_BYTES":      storagefmt.StorageHotSegmentBytes,
	} {
		assertWrelaConstU64(t, checked.Index, "storage.format", name, want)
	}
}

func assertWrelaConstU64(t *testing.T, index *Index, moduleName, constName string, want uint64) {
	t.Helper()
	got, ok := index.LookupConst(moduleName, constName)
	if !ok {
		t.Fatalf("missing const %s.%s", moduleName, constName)
	}
	if got.Value != want {
		t.Fatalf("%s.%s = %d, want %d", moduleName, constName, got.Value, want)
	}
}

func parseStorageFormatModules(t *testing.T, consumer string) []*ast.Module {
	t.Helper()
	workdir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	repoRoot := filepath.Clean(filepath.Join(workdir, "..", ".."))
	paths := []string{
		filepath.Join(repoRoot, "wrela/lang/core.wrela"),
		filepath.Join(repoRoot, "wrela/storage/format.wrela"),
	}
	files := make([]*source.File, 0, len(paths)+1)
	for i, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		files = append(files, source.NewFile(source.FileID(i+1), path, string(raw)))
	}
	files = append(files, source.NewFile(source.FileID(len(files)+1), "storage-format-consumer.wrela", consumer))
	modules, ds := parse.ParseGraph(source.Graph{Files: files})
	if len(ds) != 0 {
		t.Fatalf("parse diagnostics: %#v", ds)
	}
	return modules
}
