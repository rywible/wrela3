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
	if !bytes.Contains(code, []byte{0x48, 0x8B, 0x47, 0x08}) {
		t.Fatalf("expected load from offset 8, got %#x", code)
	}
}
