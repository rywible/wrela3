package sem

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
)

const storageAuthorityPrelude = `
module storage.authority
unique class OwnedHardware {}
unique class DelegatedHardware {
    fn exit_to_owned_hardware(self) -> OwnedHardware {
        return OwnedHardware()
    }
}
data ForegroundStoragePath { value: U64 }
data BackgroundStoragePath { value: U64 }
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
`

func TestStorageWriterCannotBeForged(t *testing.T) {
	ds := storageDiagsForSource(t, storageAuthorityPrelude+`
class Maker {
    fn make(self) -> StorageWriter {
        let foreground = ForegroundStoragePath(value = 1)
        let background = BackgroundStoragePath(value = 2)
        let stream_directory = StreamDirectory(next_stream_id = 0)
        let metrics = StorageMetrics()
        return StorageWriter(foreground = foreground, background = background, stream_directory = stream_directory, metrics = metrics)
    }
}
`)
	if !hasCode(ds, diag.SEM0113) {
		t.Fatalf("diagnostics = %#v, want SEM0113", ds)
	}
}

func TestStorageWriterConstructsInsideOwnedHardware(t *testing.T) {
	modules := parseModulesForTest(t, storageAuthorityPrelude+`
image StorageImage {
    transitions {
        delegated_hardware -> owned_hardware
    }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.exit_to_owned_hardware()
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        let foreground = ForegroundStoragePath(value = 1)
        let background = BackgroundStoragePath(value = 2)
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
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}
}

func TestStorageAppendResultMustBeObserved(t *testing.T) {
	ds := storageDiagsForSource(t, storageAuthorityPrelude+`
class Worker {
    fn run(self, writer: StorageWriter) {
        writer.enqueue_atomic_group(group = PendingAtomicGroup())
    }
}
`)
	if !hasCode(ds, diag.SEM0116) {
		t.Fatalf("diagnostics = %#v, want SEM0116", ds)
	}
}
