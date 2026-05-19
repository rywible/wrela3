package codegen

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/ir"
)

func storageEncoderProgramForCodegenTest() *ir.Program {
	slot := &ir.Param{Symbol: "slot", Type: ir.Type{Name: "StorageEventSlot", Module: "storage.format", Kind: ir.TypeKindData}}
	eventTypeID := &ir.ConstInt{Symbol: "event_type_id", Value: 1001, Type: ir.Type{Name: "U32", Module: "builtin", Kind: ir.TypeKindPrimitive}}
	payloadLayoutID := &ir.ConstInt{Symbol: "payload_layout_id", Value: 1, Type: ir.Type{Name: "U32", Module: "builtin", Kind: ir.TypeKindPrimitive}}
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
					&ir.StorageSlotStore{Slot: slot, Offset: 24, Value: eventTypeID, Type: eventTypeID.Type},
					&ir.StorageSlotStore{Slot: slot, Offset: 28, Value: payloadLayoutID, Type: payloadLayoutID.Type},
					&ir.StoragePayloadZero{Slot: slot, Offset: 64, Length: 448},
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
