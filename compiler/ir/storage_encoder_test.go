package ir

import "testing"

func TestStorageEventEncoderIRStoresHeaderFields(t *testing.T) {
	checked := checkedStorageProgramForTest(t)
	program, ds := Lower(checked)
	if len(ds) != 0 {
		t.Fatalf("lower diagnostics: %#v", ds)
	}
	fn := findFunction(program, "_wrela_storage_event_app_FileCreated_layout_1_encode")
	if fn == nil {
		t.Fatal("missing storage event encoder")
	}

	offsets := map[uint64]bool{}
	var zero *StoragePayloadZero
	var crc *StorageCRC32C
	for _, block := range fn.Blocks {
		for _, op := range block.Ops {
			switch v := op.(type) {
			case *StorageSlotStore:
				offsets[v.Offset] = true
			case *StoragePayloadZero:
				zero = v
			case *StorageCRC32C:
				crc = v
			}
		}
	}
	for _, offset := range []uint64{0, 8, 16, 24, 28, 32, 36, 40, 44, 48, 52, 54, 56} {
		if !offsets[offset] {
			t.Fatalf("encoder missing header store at offset %d; got %#v", offset, offsets)
		}
	}
	if !offsets[64] {
		t.Fatalf("encoder missing payload store at offset 64; got %#v", offsets)
	}
	if zero == nil || zero.Offset != 72 || zero.Length != 440 {
		t.Fatalf("payload zero fill = %#v, want offset 72 length 440", zero)
	}
	if crc == nil || crc.Length != 512 {
		t.Fatalf("crc op = %#v, want length 512", crc)
	}
}

func TestStorageEventEncoderComputesCRC32C(t *testing.T) {
	checked := checkedStorageProgramForTest(t)
	program, ds := Lower(checked)
	if len(ds) != 0 {
		t.Fatalf("lower diagnostics: %#v", ds)
	}
	fn := findFunction(program, "_wrela_storage_event_app_FileCreated_layout_1_encode")
	if fn == nil {
		t.Fatal("missing storage event encoder")
	}
	var sawZeroBeforeCRC bool
	var sawCRC bool
	var sawChecksumStore bool
	for _, block := range fn.Blocks {
		for _, op := range block.Ops {
			if store, ok := op.(*StorageSlotStore); ok && store.Offset == 48 {
				if _, ok := store.Value.(*ConstInt); ok && !sawCRC {
					sawZeroBeforeCRC = true
				}
				if _, ok := store.Value.(*StorageCRC32C); ok && sawCRC {
					sawChecksumStore = true
				}
			}
			if _, ok := op.(*StorageCRC32C); ok {
				sawCRC = true
			}
		}
	}
	if !sawZeroBeforeCRC || !sawCRC || !sawChecksumStore {
		t.Fatalf("checksum sequence zero=%v crc=%v store=%v", sawZeroBeforeCRC, sawCRC, sawChecksumStore)
	}
}
