package layout

import "testing"

func TestEnumLayoutUsesDiscriminantAndPrimitiveMaxPayload(t *testing.T) {
	rec, err := ComputeEnum([]EnumVariant{
		{Name: "None"},
		{Name: "Some", Fields: []Field{{Name: "value", Type: "U64"}}},
	})
	if err != nil {
		t.Fatalf("ComputeEnum: %v", err)
	}
	if rec.DiscriminantOffset != 0 || rec.PayloadOffset != 8 {
		t.Fatalf("offsets = discr %d payload %d, want 0 and 8", rec.DiscriminantOffset, rec.PayloadOffset)
	}
	if rec.Size != 16 || rec.Align != 8 {
		t.Fatalf("size/align = %d/%d, want 16/8", rec.Size, rec.Align)
	}
	some := rec.Variants["Some"]
	if some.Fields["value"].Offset != 8 {
		t.Fatalf("Some.value offset = %d, want 8", some.Fields["value"].Offset)
	}
}
