package codegen

type RelocKind uint16

const (
	RelocKindABSOLUTE RelocKind = 0
	RelocKindDIR64    RelocKind = 10
)

type Image struct {
	EntrySymbol string
	Sections    []Section
	Symbols     map[string]uint64
	Relocs      []Reloc
}

type Section struct {
	Name            string
	Data            []byte
	RVA             uint64
	Characteristics uint32
}

type Reloc struct {
	Kind   RelocKind
	Offset uint64
	Symbol string
}
