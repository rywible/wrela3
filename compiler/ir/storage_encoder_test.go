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
	for _, block := range fn.Blocks {
		for _, op := range block.Ops {
			switch v := op.(type) {
			case *StorageSlotStore:
				offsets[v.Offset] = true
			case *StoragePayloadZero:
				zero = v
			}
		}
	}
	for _, offset := range []uint64{0, 8, 16, 24, 28, 32, 36, 40, 44, 52, 54, 56} {
		if !offsets[offset] {
			t.Fatalf("encoder missing header store at offset %d; got %#v", offset, offsets)
		}
	}
	if zero == nil || zero.Offset != 64 || zero.Length != 448 {
		t.Fatalf("payload zero fill = %#v, want offset 64 length 448", zero)
	}
}
