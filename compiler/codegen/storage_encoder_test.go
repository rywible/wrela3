package codegen

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/asm"
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
