package sem

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
)

const storagePathPrelude = `
module storage.path
unique class OwnedHardware {}
unique class DelegatedHardware {
    fn exit_to_owned_hardware(self) -> OwnedHardware {
        return OwnedHardware()
    }
}
data PathIdentity { label: StringLiteral }
data ExecutorSlot { id: U64 }
data NvmePathRole { role: U32 }
const NVME_PATH_FOREGROUND: U32 = 1
const NVME_PATH_BACKGROUND: U32 = 2
data NvmeSubmission { command_id: U16 }
driver path NvmeIoPath {
    identity: PathIdentity
    role: NvmePathRole
    owner: ExecutorSlot
    queue_id: U16
    vector: U8

    fn submit_write(self) -> NvmeSubmission {
        return NvmeSubmission(command_id = 0)
    }
}
data ForegroundStoragePath { nvme_path: NvmeIoPath }
data BackgroundStoragePath { nvme_path: NvmeIoPath }
data StreamDirectory { next_stream_id: U64 }
data StorageMetrics {}
data PendingAtomicGroup {}
data StorageAppendResult { ok: Bool }
data StorageWriter {
    foreground: ForegroundStoragePath
    background: BackgroundStoragePath
    stream_directory: StreamDirectory
    metrics: StorageMetrics

    fn enqueue_atomic_group(self, group: PendingAtomicGroup) -> StorageAppendResult {
        return StorageAppendResult(ok = true)
    }
}
data MaintenanceWorker {
    background: BackgroundStoragePath

    fn submit(self, foreground: ForegroundStoragePath) -> NvmeSubmission {
        return foreground.nvme_path.submit_write()
    }
}
`

func TestStoragePathWrongOwnerFails(t *testing.T) {
	modules := parseModulesForTest(t, storagePathPrelude+`
image StorageImage {
    transitions {
        delegated_hardware -> owned_hardware
    }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.exit_to_owned_hardware()
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        let foreground_slot = ExecutorSlot(id = 0)
        let maintenance_slot = ExecutorSlot(id = 1)
        let foreground_nvme_path = NvmeIoPath(identity = PathIdentity(label = "nvme.foreground"), role = NvmePathRole(role = NVME_PATH_FOREGROUND), owner = foreground_slot, queue_id = 1, vector = 80)
        let background_nvme_path = NvmeIoPath(identity = PathIdentity(label = "nvme.background"), role = NvmePathRole(role = NVME_PATH_BACKGROUND), owner = maintenance_slot, queue_id = 2, vector = 81)
        let foreground = ForegroundStoragePath(nvme_path = background_nvme_path)
        let background = BackgroundStoragePath(nvme_path = background_nvme_path)
        let stream_directory = StreamDirectory(next_stream_id = 0)
        let metrics = StorageMetrics()
        let writer = StorageWriter(foreground = foreground, background = background, stream_directory = stream_directory, metrics = metrics)
        let worker = MaintenanceWorker(background = background)
        let submission = worker.submit(foreground = ForegroundStoragePath(nvme_path = foreground_nvme_path))
        let result = writer.enqueue_atomic_group(group = PendingAtomicGroup())
        while true {}
    }
}
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	checked, ds := checkAllowingMissingImage(t, index, modules)
	if !hasCode(ds, diag.SEM0111) {
		t.Fatalf("diagnostics = %#v, want SEM0111", ds)
	}
	if got := countCode(ds, diag.SEM0111); got != 2 {
		t.Fatalf("SEM0111 diagnostics = %d, want writer and maintenance submit failures: %#v", got, ds)
	}
	if len(checked.ImageGraph.StoragePaths) != 2 {
		t.Fatalf("storage paths = %#v, want foreground and background paths", checked.ImageGraph.StoragePaths)
	}
	want := map[string]StoragePathNode{
		"nvme.foreground": {Label: "nvme.foreground", Role: "foreground", Owner: "executor_slot.0", QueueID: 1, Vector: 80},
		"nvme.background": {Label: "nvme.background", Role: "background", Owner: "executor_slot.1", QueueID: 2, Vector: 81},
	}
	for _, path := range checked.ImageGraph.StoragePaths {
		expected, ok := want[path.Label]
		if !ok {
			t.Fatalf("unexpected storage path %#v", path)
		}
		if path.Role != expected.Role || path.Owner != expected.Owner || path.QueueID != expected.QueueID || path.Vector != expected.Vector {
			t.Fatalf("storage path %#v, want %#v", path, expected)
		}
		delete(want, path.Label)
	}
	if len(want) != 0 {
		t.Fatalf("missing storage paths %#v", want)
	}
}

func TestStoragePathWrongOwnerInlineWrapperFails(t *testing.T) {
	modules := parseModulesForTest(t, storagePathPrelude+`
image StorageImage {
    transitions {
        delegated_hardware -> owned_hardware
    }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.exit_to_owned_hardware()
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        let maintenance_slot = ExecutorSlot(id = 1)
        let background = BackgroundStoragePath(nvme_path = NvmeIoPath(identity = PathIdentity(label = "nvme.background"), role = NvmePathRole(role = NVME_PATH_BACKGROUND), owner = maintenance_slot, queue_id = 2, vector = 81))
        let foreground = ForegroundStoragePath(nvme_path = NvmeIoPath(identity = PathIdentity(label = "nvme.inline-background"), role = NvmePathRole(role = NVME_PATH_BACKGROUND), owner = maintenance_slot, queue_id = 2, vector = 81))
        let stream_directory = StreamDirectory(next_stream_id = 0)
        let metrics = StorageMetrics()
        let writer = StorageWriter(foreground = foreground, background = background, stream_directory = stream_directory, metrics = metrics)
        let result = writer.enqueue_atomic_group(group = PendingAtomicGroup())
        while true {}
    }
}
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	_, ds := checkAllowingMissingImage(t, index, modules)
	if !hasCode(ds, diag.SEM0111) {
		t.Fatalf("diagnostics = %#v, want SEM0111", ds)
	}
}

func countCode(ds []diag.Diagnostic, code string) int {
	count := 0
	for _, d := range ds {
		if d.Code == code {
			count++
		}
	}
	return count
}
