package ir

type Type struct {
	Name string
}

type Value interface {
	isValue()
}

type Block struct {
	Label string
	Ops   []Operation
}

type Function struct {
	Symbol string
	Params []Value
	Blocks []Block
}

func (f Function) ValuesInDeterministicOrder() []Value {
	seen := map[Value]struct{}{}
	out := make([]Value, 0)

	for _, block := range f.Blocks {
		appendFromOps(block.Ops, &out, seen)
	}

	return out
}

func appendFromOps(ops []Operation, out *[]Value, seen map[Value]struct{}) {
	for _, op := range ops {
		for _, v := range valuesDefinedBy(op) {
			if _, ok := seen[v]; ok {
				continue
			}
			seen[v] = struct{}{}
			*out = append(*out, v)
		}
	}
}

func valuesDefinedBy(op Operation) []Value {
	switch v := op.(type) {
	case *Call:
		return []Value{v}
	case *Binary:
		return []Value{v}
	case *ConstInt:
		return []Value{v}
	case *FieldLoad:
		return []Value{v}
	case *ForBytes:
		values := valuesFromValue(v.Index)
		values = append(values, valuesFromValue(v.Iterable)...)
		values = append(values, valuesFromValue(v.ByteValue)...)
		return append(values, valuesFromOps(v.Body)...)
	case *If:
		values := valuesFromOps(v.Then)
		values = append(values, valuesFromOps(v.Else)...)
		return values
	case *While:
		return valuesFromOps(v.Body)
	}
	return nil
}

func valuesFromValue(v Value) []Value {
	if v == nil {
		return nil
	}
	return []Value{v}
}

func valuesFromOps(ops []Operation) []Value {
	out := make([]Value, 0)
	seen := map[Value]struct{}{}
	appendFromOps(ops, &out, seen)
	return out
}

type ValueType uint8

const (
	ValueTypeUnknown ValueType = iota
	ValueTypeU8
	ValueTypeU16
	ValueTypeU32
	ValueTypeU64
	ValueTypeI64
	ValueTypeBool
)

type Operation interface {
	isOperation()
}

type Param struct {
	Symbol string
	Type   Type
}

func (Param) isValue()     {}
func (Param) isOperation() {}

type ConstInt struct {
	Symbol string
	Value  uint64
	Type   Type
}

func (ConstInt) isValue()     {}
func (ConstInt) isOperation() {}

type Binary struct {
	Op    string
	Left  Value
	Right Value
	Type  Type
}

func (Binary) isValue()     {}
func (Binary) isOperation() {}

type Call struct {
	Symbol   string
	Receiver Value
	Args     []Value
	Type     Type
}

func (Call) isValue()     {}
func (Call) isOperation() {}

type Branch struct {
	Condition Value
	True      string
	False     string
}

func (Branch) isOperation() {}

type Return struct {
	Value Value
}

func (Return) isOperation() {}

type ForBytes struct {
	Iterable  Value
	Index     *Param
	ByteValue Value
	Body      []Operation
}

func (ForBytes) isOperation() {}

type While struct {
	Condition Value
	Body      []Operation
}

func (While) isOperation() {}

type If struct {
	Condition Value
	Then      []Operation
	Else      []Operation
}

func (If) isOperation() {}

type FieldLoad struct {
	Object     Value
	ObjectType string
	Field      string
	Type       Type
	Offset     int
}

func (FieldLoad) isValue()     {}
func (FieldLoad) isOperation() {}

type DataObject struct {
	Symbol string
	Bytes  []byte
}

type EntryAdapter struct {
	Symbol                string
	DelegatedPhaseSymbol  string
	OwnedPhaseSymbol      string
	DelegatedHardwareType string
	OwnedHardwareType     string
}

type AsmMethod struct {
	Symbol       string
	ReceiverType string
	Params       []Value
	Return       Type
	Body         string
	// ReceiverFieldOffsets is optional layout data produced by lowering.
	ReceiverFieldOffsets map[string]int
	// ReceiverFieldWidths is optional byte widths for lower-bound field loads.
	ReceiverFieldWidths map[string]int
}

type Program struct {
	Functions  []Function
	AsmMethods []AsmMethod
	Data       []DataObject
	Entry      EntryAdapter
}
