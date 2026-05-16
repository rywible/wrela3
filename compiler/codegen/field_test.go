package codegen

import (
	"bytes"
	"testing"

	"github.com/ryanwible/wrela3/compiler/ir"
	"github.com/ryanwible/wrela3/compiler/layout"
)

func TestCompileFieldLoadUsesOffset(t *testing.T) {
	record, err := layout.Compute([]layout.Field{
		{Name: "tag", Type: "U8"},
		{Name: "value", Type: "U64"},
	})
	if err != nil {
		t.Fatalf("layout.Compute returned error: %v", err)
	}

	obj := &ir.Param{Symbol: "obj", Type: ir.Type{Name: "DataRecord"}}
	load := &ir.FieldLoad{
		Object:     obj,
		ObjectType: "DataRecord",
		Field:      "value",
		Type:       ir.Type{Name: "U64"},
		Offset:     record.Fields["value"].Offset,
	}
	gotOffset := load.Offset
	if gotOffset != 8 {
		t.Fatalf("field offset = %d, want 8", gotOffset)
	}

	fn := ir.Function{
		Symbol: "load_field",
		Params: []ir.Value{obj},
		Blocks: []ir.Block{
			{
				Label: "entry",
				Ops: []ir.Operation{
					load,
					&ir.Return{Value: load},
				},
			},
		},
	}

	image, diags := Compile(&ir.Program{Functions: []ir.Function{fn}})
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}

	code := image.Sections[0].Data
	if !bytes.Contains(code, []byte{0x49, 0x8B, 0x43, 0x08}) {
		t.Fatalf("expected load from offset 8, got %#x", code)
	}
}

func TestCompileConstructDeepCopiesNestedDataField(t *testing.T) {
	id := &ir.ConstInt{Symbol: "id", Value: 0x1122334455667788, Type: ir.Type{Name: "U64"}}
	inner := &ir.Construct{
		Symbol: "inner",
		Type:   ir.Type{Name: "Inner", Kind: ir.TypeKindData},
		Fields: []ir.FieldValue{{Name: "id", Value: id}},
	}
	outer := &ir.Construct{
		Symbol: "outer",
		Type:   ir.Type{Name: "Outer", Kind: ir.TypeKindData},
		Fields: []ir.FieldValue{{Name: "inner", Value: inner}},
	}
	fn := ir.Function{
		Symbol: "construct_outer",
		Blocks: []ir.Block{{
			Label: "entry",
			Ops:   []ir.Operation{id, inner, outer, &ir.Return{}},
		}},
	}

	image, diags := Compile(&ir.Program{Functions: []ir.Function{fn}, Types: nestedDataFieldTestTypes()})
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}
	code := symbolBytes(t, image, "construct_outer")
	if !containsNestedHandleStore(code) {
		t.Fatalf("constructor must store a destination-owned nested data handle: %#x", code)
	}
	if countBytes(code, []byte{0x48, 0x8B}) < 1 || countBytes(code, []byte{0x48, 0x89}) < 1 {
		t.Fatalf("constructor must copy nested data storage, not only the source handle: %#x", code)
	}
}

func TestCompileFieldStoreDeepCopiesNestedDataField(t *testing.T) {
	id := &ir.ConstInt{Symbol: "id", Value: 0x8877665544332211, Type: ir.Type{Name: "U64"}}
	target := &ir.Construct{
		Symbol: "target",
		Type:   ir.Type{Name: "Outer", Kind: ir.TypeKindData},
	}
	inner := &ir.Construct{
		Symbol: "inner",
		Type:   ir.Type{Name: "Inner", Kind: ir.TypeKindData},
		Fields: []ir.FieldValue{{Name: "id", Value: id}},
	}
	store := &ir.FieldStore{
		Object:     target,
		ObjectType: "Outer",
		Field:      "inner",
		Value:      inner,
		Type:       ir.Type{Name: "Inner", Kind: ir.TypeKindData},
		Offset:     0,
	}
	fn := ir.Function{
		Symbol: "store_outer_inner",
		Blocks: []ir.Block{{
			Label: "entry",
			Ops:   []ir.Operation{target, id, inner, store, &ir.Return{}},
		}},
	}

	image, diags := Compile(&ir.Program{Functions: []ir.Function{fn}, Types: nestedDataFieldTestTypes()})
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}
	code := symbolBytes(t, image, "store_outer_inner")
	if !containsNestedHandleStore(code) {
		t.Fatalf("field store must rewrite the nested data field handle to destination storage: %#x", code)
	}
	if !bytes.Contains(code, []byte{0x41, 0x53}) || !bytes.Contains(code, []byte{0x5F}) {
		t.Fatalf("field store must preserve the destination handle while loading the source data handle: %#x", code)
	}
	if countBytes(code, []byte{0x48, 0x8B}) < 1 || countBytes(code, []byte{0x48, 0x89}) < 1 {
		t.Fatalf("field store must copy nested data storage, not only the source handle: %#x", code)
	}
}

func TestCompileFieldLoadDeepCopiesNestedDataField(t *testing.T) {
	source := &ir.Param{Symbol: "source", Type: ir.Type{Name: "Outer", Kind: ir.TypeKindData}}
	load := &ir.FieldLoad{
		Object:     source,
		ObjectType: "Outer",
		Field:      "inner",
		Type:       ir.Type{Name: "Inner", Kind: ir.TypeKindData},
		Offset:     0,
	}
	fn := ir.Function{
		Symbol: "load_outer_inner",
		Params: []ir.Value{source},
		Blocks: []ir.Block{{
			Label: "entry",
			Ops:   []ir.Operation{load, &ir.Return{Value: load}},
		}},
	}

	frame := buildFrame(fn, compileContext{types: nestedDataFieldTestTypes()})
	if _, ok := frame.ObjectSlots[load]; !ok {
		t.Fatal("data field load must allocate owned object storage")
	}
	image, diags := Compile(&ir.Program{Functions: []ir.Function{fn}, Types: nestedDataFieldTestTypes()})
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}
	code := symbolBytes(t, image, "load_outer_inner")
	if !containsNestedHandleStore(code) {
		t.Fatalf("field load must materialize a destination-owned data value: %#x", code)
	}
}

func containsNestedHandleStore(code []byte) bool {
	return bytes.Contains(code, []byte{0x4C, 0x89, 0x55}) || // mov [rbp+disp], r10
		bytes.Contains(code, []byte{0x4D, 0x89, 0x13}) // mov [r11], r10
}

func nestedDataFieldTestTypes() map[string]ir.TypeInfo {
	innerType := ir.Type{Name: "Inner", Kind: ir.TypeKindData}
	return map[string]ir.TypeInfo{
		"Inner": {
			Name:        "Inner",
			Kind:        ir.TypeKindData,
			Size:        8,
			Align:       8,
			StorageSize: 8,
			Fields: map[string]ir.FieldInfo{
				"id": {Name: "id", Type: ir.Type{Name: "U64"}, Offset: 0, Size: 8, Align: 8, StorageOffset: 0, StorageSize: 8},
			},
			FieldOrder: []string{"id"},
		},
		"Outer": {
			Name:        "Outer",
			Kind:        ir.TypeKindData,
			Size:        8,
			Align:       8,
			StorageSize: 16,
			Fields: map[string]ir.FieldInfo{
				"inner": {Name: "inner", Type: innerType, Offset: 0, Size: 8, Align: 8, StorageOffset: 8, StorageSize: 8},
			},
			FieldOrder: []string{"inner"},
		},
	}
}
