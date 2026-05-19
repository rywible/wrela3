package codegen

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/asm"
	"github.com/ryanwible/wrela3/compiler/ir"
)

func storageEncoderProgramForCodegenTest() *ir.Program {
	slot := &ir.Param{Symbol: "slot", Type: ir.Type{Name: "StorageEventSlot", Module: "storage.format", Kind: ir.TypeKindData}}
	eventTypeID := &ir.ConstInt{Symbol: "event_type_id", Value: 1001, Type: ir.Type{Name: "U32", Module: "builtin", Kind: ir.TypeKindPrimitive}}
	payloadLayoutID := &ir.ConstInt{Symbol: "payload_layout_id", Value: 1, Type: ir.Type{Name: "U32", Module: "builtin", Kind: ir.TypeKindPrimitive}}
	checksumZero := &ir.ConstInt{Symbol: "checksum_zero", Value: 0, Type: ir.Type{Name: "U32", Module: "builtin", Kind: ir.TypeKindPrimitive}}
	checksum := &ir.StorageCRC32C{Slot: slot, Length: 512, Type: ir.Type{Name: "U32", Module: "builtin", Kind: ir.TypeKindPrimitive}}
	return &ir.Program{
		Functions: []ir.Function{{
			Symbol: "_wrela_storage_event_app_FileCreated_layout_1_encode",
			Return: ir.Type{Name: "void", Module: "builtin", Kind: ir.TypeKindPrimitive},
			Params: []ir.Value{slot},
			Blocks: []ir.Block{{
				Label: "entry",
				Ops: []ir.Operation{
					eventTypeID,
					payloadLayoutID,
					checksumZero,
					&ir.StorageSlotStore{Slot: slot, Offset: 24, Value: eventTypeID, Type: eventTypeID.Type},
					&ir.StorageSlotStore{Slot: slot, Offset: 28, Value: payloadLayoutID, Type: payloadLayoutID.Type},
					&ir.StoragePayloadZero{Slot: slot, Offset: 64, Length: 448},
					&ir.StorageSlotStore{Slot: slot, Offset: 48, Value: checksumZero, Type: checksumZero.Type},
					checksum,
					&ir.StorageSlotStore{Slot: slot, Offset: 48, Value: checksum, Type: checksum.Type},
					&ir.Return{},
				},
			}},
		}},
	}
}

func findTextUnit(t *testing.T, image *Image, symbol string) Section {
	t.Helper()
	if image == nil {
		t.Fatal("nil image")
	}
	if _, ok := image.Symbols[symbol]; !ok {
		t.Fatalf("missing text unit %s", symbol)
	}
	text := symbolBytes(t, image, symbol)
	return Section{Name: ".text." + symbol, Data: text, Characteristics: 0x60000020}
}

func storageSlotStoreInstructionsForTest(fn ir.Function) []asm.Instruction {
	ctx := compileContext{types: map[string]ir.TypeInfo{}}
	var out []asm.Instruction
	for _, block := range fn.Blocks {
		for _, op := range block.Ops {
			store, ok := op.(*ir.StorageSlotStore)
			if !ok {
				continue
			}
			width := storageSlotStoreWidthBits(ctx, store.Type)
			out = append(out, asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
				asm.MemOperand{Base: asm.MustLookup("r11"), Disp: int64(store.Offset), Width: width},
				asm.RegOperand{Reg: registerForWidth(asm.MustLookup("rax"), width)},
			}})
		}
	}
	return out
}
