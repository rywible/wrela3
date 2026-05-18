package ir

type Type struct {
	Name   string
	Module string
	Kind   TypeKind
}

type Value interface {
	isValue()
}

type Block struct {
	Label string
	Ops   []Operation
}

type Function struct {
	Symbol              string
	Return              Type
	Params              []Value
	Blocks              []Block
	PreserveStackReturn bool
}

type TypeKind string

const (
	TypeKindUnknown    TypeKind = ""
	TypeKindPrimitive  TypeKind = "primitive"
	TypeKindData       TypeKind = "data"
	TypeKindClass      TypeKind = "class"
	TypeKindDriver     TypeKind = "driver"
	TypeKindDriverPath TypeKind = "driver_path"
	TypeKindExecutor   TypeKind = "executor"
	TypeKindImage      TypeKind = "image"
	TypeKindEnum       TypeKind = "enum"
)

type FieldInfo struct {
	Name          string
	Type          Type
	Offset        int
	Size          int
	Align         int
	StorageOffset int
	StorageSize   int
}

type TypeInfo struct {
	Name         string
	Module       string
	Kind         TypeKind
	Size         int
	Align        int
	StorageSize  int
	Fields       map[string]FieldInfo
	FieldOrder   []string
	EnumVariants []EnumVariantInfo
}

type EnumVariantInfo struct {
	Name         string
	Discriminant uint64
	Fields       []string
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
	case *FrameBegin:
		return []Value{v}
	case *Binary:
		return []Value{v}
	case *ConstInt:
		return []Value{v}
	case *FieldLoad:
		return []Value{v}
	case *Local:
		return []Value{v}
	case *StringLiteral:
		return []Value{v}
	case *Construct:
		return []Value{v}
	case *ArenaReserve:
		return []Value{v}
	case *ArenaPlace:
		return []Value{v}
	case ReliableTopicTryPublish:
		return []Value{v}
	case *ReliableTopicTryPublish:
		return []Value{v}
	case TopicTryNext:
		return []Value{v}
	case *TopicTryNext:
		return []Value{v}
	case TopicIsWaitArmed:
		return []Value{v}
	case *TopicIsWaitArmed:
		return []Value{v}
	case VcpuStart:
		return []Value{v}
	case *VcpuStart:
		return []Value{v}
	case *Copy:
		return valuesFromValue(v.Target)
	case *ForBytes:
		values := valuesFromOps(v.IterableOps)
		values = append(values, valuesFromValue(v.Index)...)
		values = append(values, valuesFromValue(v.Iterable)...)
		values = append(values, valuesFromValue(v.ByteValue)...)
		return append(values, valuesFromOps(v.Body)...)
	case *If:
		values := valuesFromOps(v.ConditionOps)
		values = append(values, valuesFromOps(v.Then)...)
		values = append(values, valuesFromOps(v.Else)...)
		return values
	case *While:
		values := valuesFromOps(v.ConditionOps)
		values = append(values, valuesFromOps(v.Body)...)
		return values
	case *InterruptContextStore:
		return nil
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

type Local struct {
	Symbol string
	Type   Type
}

func (Local) isValue()     {}
func (Local) isOperation() {}

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

type Copy struct {
	Target Value
	Source Value
	Type   Type
}

func (Copy) isOperation() {}

type FieldValue struct {
	Name  string
	Value Value
}

type Construct struct {
	Symbol string
	Type   Type
	Fields []FieldValue
}

func (Construct) isValue()     {}
func (Construct) isOperation() {}

type FrameBegin struct {
	Symbol string
	Parent Value
	Length Value
	Type   Type
}

func (*FrameBegin) isValue()     {}
func (*FrameBegin) isOperation() {}

type FrameEnd struct {
	Frame *FrameBegin
}

func (*FrameEnd) isOperation() {}

type ArenaReserve struct {
	Arena  Value
	Length Value
	Align  Value
	Type   Type
}

func (*ArenaReserve) isValue()     {}
func (*ArenaReserve) isOperation() {}

type ArenaPlace struct {
	Arena  Value
	Type   Type
	Fields []FieldValue
}

func (*ArenaPlace) isValue()     {}
func (*ArenaPlace) isOperation() {}

type StringLiteral struct {
	Symbol     string
	Value      string
	DataSymbol string
	Type       Type
}

func (StringLiteral) isValue()     {}
func (StringLiteral) isOperation() {}

type ForBytes struct {
	IterableOps []Operation
	Iterable    Value
	Index       Value
	ByteValue   Value
	Body        []Operation
}

func (ForBytes) isOperation() {}

type While struct {
	ConditionOps []Operation
	Condition    Value
	Body         []Operation
}

func (While) isOperation() {}

type If struct {
	ConditionOps []Operation
	Condition    Value
	Then         []Operation
	Else         []Operation
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

type FieldStore struct {
	Object     Value
	ObjectType string
	Field      string
	Value      Value
	Type       Type
	Offset     int
}

func (FieldStore) isOperation() {}

type TopicPublish struct {
	TopicLabel string
	Kind       string
	Value      Value
}

func (TopicPublish) isOperation() {}

type ReliableTopicTryPublish struct {
	TopicLabel string
	Value      Value
	Type       Type
}

func (ReliableTopicTryPublish) isValue()     {}
func (ReliableTopicTryPublish) isOperation() {}

type ReliableTopicWaitForAdvance struct {
	TopicLabel    string
	PublisherSlot string
}

func (ReliableTopicWaitForAdvance) isOperation() {}

type TopicTryNext struct {
	TopicLabel     string
	SubscriberSlot string
	Subscription   Value
	Type           Type
}

func (TopicTryNext) isValue()     {}
func (TopicTryNext) isOperation() {}

type TopicArmWait struct {
	TopicLabel     string
	SubscriberSlot string
	Subscription   Value
}

func (TopicArmWait) isOperation() {}

type TopicIsWaitArmed struct {
	TopicLabel     string
	SubscriberSlot string
	Subscription   Value
	Type           Type
}

func (TopicIsWaitArmed) isValue()     {}
func (TopicIsWaitArmed) isOperation() {}

type TopicWaitIfArmed struct {
	TopicLabel     string
	SubscriberSlot string
	Subscription   Value
	Guards         []TopicWaitGuard
}

func (TopicWaitIfArmed) isOperation() {}

type TopicWaitGuard struct {
	TopicLabel     string
	SubscriberSlot string
	Subscription   Value
}

type TopicWait struct {
	SlotLabel       string
	Policy          string
	UseMonitorMwait bool
	Fallback        string
}

func (TopicWait) isOperation() {}

type CpuFeatureFacts struct {
	MonitorMwaitAvailable bool
}

type VcpuStart struct {
	VcpuID        int
	APICID        uint32
	LocalApicBase uint64
	APICMode      string
	Vcpu          Value
	Executor      Value
	SlotLabel     string
	Type          Type
}

func (VcpuStart) isValue()     {}
func (VcpuStart) isOperation() {}

type VcpuEnter struct {
	VcpuID        int
	APICID        uint32
	LocalApicBase uint64
	APICMode      string
	Vcpu          Value
	Executor      Value
	SlotLabel     string
}

func (VcpuEnter) isOperation() {}

type TimerInit struct {
	Source    string
	PeriodUS  uint64
	Vector    uint8
	Timer     Value
	LocalApic Value
}

func (TimerInit) isOperation() {}

type DataObject struct {
	Symbol string
	Bytes  []byte
	Align  uint64
}

type InterruptContextPathField struct {
	FieldName string
	Offset    int
	Type      Type
}

type InterruptContext struct {
	Symbol       string
	ExecutorType Type
	Size         int
	PathFields   []InterruptContextPathField
}

type InterruptContextStore struct {
	ContextSymbol string
	ContextOffset int
	Source        Value
	SourceType    Type
	Size          int
}

func (InterruptContextStore) isOperation() {}

type EntryAdapter struct {
	Symbol                string
	DelegatedPhaseSymbol  string
	OwnedPhaseSymbol      string
	DelegatedHardwareType string
	OwnedHardwareType     string
}

type InterruptEvent struct {
	Symbol         string
	PathType       Type
	EventType      Type
	FunctionSymbol string
}

type OnHandler struct {
	Symbol         string
	ExecutorType   Type
	PathField      string
	EventType      Type
	FunctionSymbol string
}

type InterruptBinding struct {
	EventSymbol           string
	HandlerSymbol         string
	EventFunctionSymbol   string
	HandlerFunctionSymbol string
	ExecutorType          Type
	PathField             string
	PathFieldOffset       int
	ContextSymbol         string
	EventStorageSymbol    string
	EventStorageSize      int
	Vector                uint8
	TopicLabel            string
	TopicKind             string
	PublisherOwnerKind    string
	PublisherOwnerLabel   string
	SubscriberSlots       []string
}

type TopicLayout struct {
	Label        string
	Kind         string
	Depth        uint64
	PayloadType  Type
	PayloadSize  uint64
	PayloadAlign uint64
	SlotSize     uint64
	Producers    []string
	Subscribers  []string
}

type VcpuStartPlan struct {
	VcpuID        int
	APICID        uint32
	LocalApicBase uint64
	APICMode      string
	SlotLabel     string
	ExecutorType  Type
	EntrySymbol   string
	Terminal      bool
}

type TimerRoute struct {
	Label           string
	Source          string
	PeriodUS        uint64
	Vector          uint8
	SubscriberSlots []string
}

type InterruptQueueLayout struct {
	Label        string
	SourceLabel  string
	Vector       uint8
	Owner        string
	Capacity     uint64
	PayloadSize  uint64
	PayloadAlign uint64
	Overflow     string
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
	TypeFieldOffsets    map[string]map[string]int
}

type Program struct {
	Functions         []Function
	AsmMethods        []AsmMethod
	Data              []DataObject
	WritableData      []DataObject
	Entry             EntryAdapter
	Types             map[string]TypeInfo
	InterruptEvents   []InterruptEvent
	OnHandlers        []OnHandler
	InterruptBindings []InterruptBinding
	InterruptContexts []InterruptContext
	Topics            []TopicLayout
	InterruptQueues   []InterruptQueueLayout
	VcpuStarts        []VcpuStartPlan
	Timers            []TimerRoute
	APICMode          string
}
