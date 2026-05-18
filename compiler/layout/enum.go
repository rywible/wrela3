package layout

type EnumVariant struct {
	Name   string
	Fields []Field
}

type EnumVariantLayout struct {
	Fields map[string]FieldLayout
	Size   int
	Align  int
}

type EnumRecord struct {
	DiscriminantOffset int
	PayloadOffset      int
	Variants           map[string]EnumVariantLayout
	Size               int
	Align              int
}

func ComputeEnum(variants []EnumVariant) (EnumRecord, error) {
	out := EnumRecord{
		DiscriminantOffset: 0,
		PayloadOffset:      8,
		Variants:           map[string]EnumVariantLayout{},
		Align:              8,
	}
	maxPayloadSize := 0
	maxPayloadAlign := 1
	for _, variant := range variants {
		rec, err := Compute(variant.Fields)
		if err != nil {
			return EnumRecord{}, err
		}
		shifted := map[string]FieldLayout{}
		for name, field := range rec.Fields {
			field.Offset += out.PayloadOffset
			shifted[name] = field
		}
		out.Variants[variant.Name] = EnumVariantLayout{Fields: shifted, Size: rec.Size, Align: rec.Align}
		if rec.Size > maxPayloadSize {
			maxPayloadSize = rec.Size
		}
		if rec.Align > maxPayloadAlign {
			maxPayloadAlign = rec.Align
		}
	}
	out.Align = max(8, maxPayloadAlign)
	out.Size = AlignUp(out.PayloadOffset+maxPayloadSize, out.Align)
	return out, nil
}
