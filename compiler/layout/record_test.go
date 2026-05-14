package layout

import "testing"

func TestComputePaddedLayout(t *testing.T) {
	rec, err := Compute([]Field{
		{Name: "a", Type: "U8"},
		{Name: "b", Type: "U64"},
	})
	if err != nil {
		t.Fatalf("Compute returned error: %v", err)
	}
	if rec.Fields["a"].Offset != 0 || rec.Fields["b"].Offset != 8 {
		t.Fatalf("offsets = %#v, %#v", rec.Fields["a"], rec.Fields["b"])
	}
	if rec.Size != 16 || rec.Align != 8 {
		t.Fatalf("size/align = %d/%d, want 16/8", rec.Size, rec.Align)
	}
}

func TestSizeAlignPrimitives(t *testing.T) {
	cases := map[string][2]int{
		"Bool":            {1, 1},
		"U8":              {1, 1},
		"U16":             {2, 2},
		"U32":             {4, 4},
		"U64":             {8, 8},
		"I64":             {8, 8},
		"PhysicalAddress": {8, 8},
		"VirtualAddress":  {8, 8},
		"StringLiteral":   {16, 8},
		"data":            {8, 8},
	}
	for typ, want := range cases {
		size, align, err := SizeAlign(typ)
		if err != nil {
			t.Fatalf("SizeAlign(%s) returned error: %v", typ, err)
		}
		if size != want[0] || align != want[1] {
			t.Fatalf("SizeAlign(%s) = (%d, %d), want (%d, %d)", typ, size, align, want[0], want[1])
		}
	}
}

func TestComputeUnknownTypeDefaultsPointer(t *testing.T) {
	rec, err := Compute([]Field{{Name: "x", Type: "SomeClass"}})
	if err != nil {
		t.Fatalf("Compute returned error: %v", err)
	}
	field := rec.Fields["x"]
	if field.Size != 8 || field.Align != 8 || field.Offset != 0 {
		t.Fatalf("field layout = %#v, want size=8 align=8 offset=0", field)
	}
}
