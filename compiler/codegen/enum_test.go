package codegen

import (
	"bytes"
	"testing"

	"github.com/ryanwible/wrela3/compiler/ir"
)

func testEnumTypeInfos() map[string]ir.TypeInfo {
	u64 := ir.Type{Name: "U64", Kind: ir.TypeKindPrimitive}
	event := ir.Type{Name: "Event", Kind: ir.TypeKindData}
	return map[string]ir.TypeInfo{
		"Event": {Name: "Event", Kind: ir.TypeKindData, Size: 8, Align: 8, StorageSize: 8, Fields: map[string]ir.FieldInfo{
			"kind": {Name: "kind", Type: u64, Offset: 0, Size: 8, Align: 8, StorageOffset: 0, StorageSize: 8},
		}},
		"Option<Event>": {
			Name: "Option<Event>", Kind: ir.TypeKindEnum, Size: 16, Align: 8, StorageSize: 16,
			Fields: map[string]ir.FieldInfo{
				"$tag":       {Name: "$tag", Type: u64, Offset: 0, Size: 8, Align: 8, StorageOffset: 0, StorageSize: 8},
				"Some.value": {Name: "Some.value", Type: event, Offset: 8, Size: 8, Align: 8, StorageOffset: 8, StorageSize: 8},
			},
			EnumVariants: []ir.EnumVariantInfo{
				{Name: "None", Discriminant: 0},
				{Name: "Some", Discriminant: 1, Fields: []string{"Some.value"}},
			},
		},
	}
}

func TestEnumConstructStoresDiscriminantAndPayload(t *testing.T) {
	event := ir.Local{Symbol: "event", Type: ir.Type{Name: "Event"}}
	program := &ir.Program{Types: testEnumTypeInfos(), Functions: []ir.Function{{
		Symbol: "_wrela_test_enum_construct",
		Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
			&event,
			&ir.EnumConstruct{
				Symbol:  "next",
				Type:    ir.Type{Name: "Option<Event>"},
				Variant: "Some",
				Fields:  []ir.FieldValue{{Name: "value", Value: &event}},
			},
		}}},
	}}}
	image, ds := Compile(program)
	if len(ds) != 0 {
		t.Fatalf("Compile diagnostics: %#v", ds)
	}
	code := symbolBytes(t, image, "_wrela_test_enum_construct")
	if !bytes.Contains(code, []byte{0x48, 0xC7}) {
		t.Fatalf("enum constructor must emit an immediate discriminant store, got %#x", code)
	}
	if !bytes.Contains(code, []byte{0x89}) && !bytes.Contains(code, []byte{0x8B}) {
		t.Fatalf("enum constructor must copy payload bytes, got %#x", code)
	}
	if !containsBytes(code, []byte{0x01, 0x00, 0x00, 0x00}) {
		t.Fatalf("enum constructor must store Some discriminant 1, got %#x", code)
	}
	if !containsBytes(code, []byte{0x08, 0x00, 0x00, 0x00}) {
		t.Fatalf("enum constructor must use Some.value payload offset 8, got %#x", code)
	}
}

func TestEnumVariantTestComparesTag(t *testing.T) {
	next := ir.Local{Symbol: "next", Type: ir.Type{Name: "Option<Event>"}}
	program := &ir.Program{Types: testEnumTypeInfos(), Functions: []ir.Function{{
		Symbol: "_wrela_test_enum_variant_test",
		Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
			&next,
			&ir.EnumVariantTest{Value: &next, Type: ir.Type{Name: "Option<Event>"}, Variant: "Some"},
		}}},
	}}}
	image, ds := Compile(program)
	if len(ds) != 0 {
		t.Fatalf("Compile diagnostics: %#v", ds)
	}
	code := symbolBytes(t, image, "_wrela_test_enum_variant_test")
	if !bytes.Contains(code, []byte{0x48, 0x83}) && !bytes.Contains(code, []byte{0x48, 0x81}) {
		t.Fatalf("variant test must emit a 64-bit compare against the tag, got %#x", code)
	}
	if !containsBytes(code, []byte{0x01, 0x00, 0x00, 0x00}) {
		t.Fatalf("variant test must compare against Some discriminant 1, got %#x", code)
	}
}

func TestEnumPayloadExtractLoadsPayloadOffset(t *testing.T) {
	next := ir.Local{Symbol: "next", Type: ir.Type{Name: "Option<Event>"}}
	program := &ir.Program{Types: testEnumTypeInfos(), Functions: []ir.Function{{
		Symbol: "_wrela_test_enum_payload_extract",
		Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
			&next,
			&ir.EnumPayloadExtract{Value: &next, Type: ir.Type{Name: "Option<Event>"}, Variant: "Some", Field: "value"},
		}}},
	}}}
	image, ds := Compile(program)
	if len(ds) != 0 {
		t.Fatalf("Compile diagnostics: %#v", ds)
	}
	code := symbolBytes(t, image, "_wrela_test_enum_payload_extract")
	if !bytes.Contains(code, []byte{0x8B}) && !bytes.Contains(code, []byte{0x8D}) && !bytes.Contains(code, []byte{0x89}) {
		t.Fatalf("payload extract must emit a load or address calculation, got %#x", code)
	}
	if !containsBytes(code, []byte{0x08, 0x00, 0x00, 0x00}) {
		t.Fatalf("payload extract must use Some.value offset 8, got %#x", code)
	}
}
