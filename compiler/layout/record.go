package layout

type Field struct {
	Name string
	Type string
}

type FieldLayout struct {
	Offset int
	Size   int
	Align  int
}

type Record struct {
	Fields map[string]FieldLayout
	Size   int
	Align  int
}

func AlignUp(value, align int) int {
	if align <= 1 {
		return value
	}
	rem := value % align
	if rem == 0 {
		return value
	}
	return value + (align - rem)
}

func Compute(fields []Field) (Record, error) {
	out := Record{Fields: map[string]FieldLayout{}}
	offset := 0
	recordAlign := 1
	for _, f := range fields {
		size, align, err := SizeAlign(f.Type)
		if err != nil {
			return Record{}, err
		}
		offset = AlignUp(offset, align)
		out.Fields[f.Name] = FieldLayout{Offset: offset, Size: size, Align: align}
		offset += size
		if align > recordAlign {
			recordAlign = align
		}
	}
	out.Size = AlignUp(offset, recordAlign)
	out.Align = recordAlign
	return out, nil
}

func SizeAlign(typeName string) (size, align int, err error) {
	switch typeName {
	case "Bool":
		return 1, 1, nil
	case "U8":
		return 1, 1, nil
	case "U16":
		return 2, 2, nil
	case "U32":
		return 4, 4, nil
	case "U64", "I64":
		return 8, 8, nil
	case "PhysicalAddress", "VirtualAddress":
		return 8, 8, nil
	case "StringLiteral":
		return 16, 8, nil
	case "data", "class", "driver", "path", "executor":
		return 8, 8, nil
	default:
		return 8, 8, nil
	}
}
