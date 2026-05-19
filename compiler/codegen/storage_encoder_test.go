package codegen

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/ryanwible/wrela3/compiler/asm"
	"github.com/ryanwible/wrela3/compiler/storagefmt"
)

func TestStorageEncoderCodegenHarnessBuildsProgram(t *testing.T) {
	program := storageEncoderProgramForCodegenTest()
	if len(program.Functions) != 1 {
		t.Fatalf("functions = %d, want 1", len(program.Functions))
	}
	if program.Functions[0].Symbol == "" {
		t.Fatalf("encoder symbol must not be empty")
	}
}

func TestStorageSlotStoreCodegen(t *testing.T) {
	program := storageEncoderProgramForCodegenTest()
	image, diags := Compile(program)
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}
	unit := findTextUnit(t, image, "_wrela_storage_event_app_FileCreated_layout_1_encode")
	if len(unit.Data) == 0 {
		t.Fatal("storage encoder text is empty")
	}
	instructions := storageSlotStoreInstructionsForTest(program.Functions[0])
	stores := storageSlotStoresForTest(instructions)
	for _, offset := range []uint64{24, 28} {
		if !stores[offset] {
			t.Fatalf("slot stores = %#v, want offset %d", stores, offset)
		}
	}
}

func TestStorageCRC32CCodegen(t *testing.T) {
	program := storageEncoderProgramForCodegenTest()
	image, diags := Compile(program)
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}
	helper := symbolBytes(t, image, "_wrela_crc32c_castagnoli")
	if len(helper) == 0 {
		t.Fatal("missing crc32c helper body")
	}
	if !bytes.Contains(helper, []byte{0x78, 0x3b, 0xf6, 0x82}) {
		t.Fatalf("crc32c helper does not contain reflected Castagnoli polynomial: %#x", helper)
	}

	slot := make([]byte, storagefmt.EventSlotSize)
	binary.LittleEndian.PutUint64(slot[0:8], 1)
	binary.LittleEndian.PutUint32(slot[24:28], 1001)
	binary.LittleEndian.PutUint32(slot[28:32], 1)
	binary.LittleEndian.PutUint32(slot[48:52], 0)
	got := crc32cCastagnoliBitwiseForTest(slot)
	want := storagefmt.CRC32C(slot)
	if got != want {
		t.Fatalf("crc32c helper mirror = %#x, want storagefmt %#x", got, want)
	}
	binary.LittleEndian.PutUint32(slot[48:52], got)
	if binary.LittleEndian.Uint32(slot[48:52]) != want {
		t.Fatalf("checksum field = %#x, want %#x", binary.LittleEndian.Uint32(slot[48:52]), want)
	}
}

func crc32cCastagnoliBitwiseForTest(data []byte) uint32 {
	crc := uint32(0xffffffff)
	for _, b := range data {
		crc ^= uint32(b)
		for bit := 0; bit < 8; bit++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0x82f63b78
			} else {
				crc >>= 1
			}
		}
	}
	return ^crc
}

func storageSlotStoresForTest(instructions []asm.Instruction) map[uint64]bool {
	out := map[uint64]bool{}
	for _, ins := range instructions {
		if ins.Mnemonic != "mov" || len(ins.Operands) != 2 {
			continue
		}
		mem, ok := ins.Operands[0].(asm.MemOperand)
		if !ok || mem.Base.Name != "r11" {
			continue
		}
		out[uint64(mem.Disp)] = true
	}
	return out
}
