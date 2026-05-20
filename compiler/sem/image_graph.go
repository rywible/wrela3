package sem

import (
	"fmt"

	"github.com/ryanwible/wrela3/compiler/source"
)

type ConstructedNode struct {
	Type *Type
	Span source.Span
}

type DriverPathNode struct {
	Type      *Type
	Span      source.Span
	Binding   string
	FieldUses map[string]DriverPathUse
}

type DriverPathUse struct {
	Key  string
	Span source.Span
}

func (u DriverPathUse) spanOr(fallback source.Span) source.Span {
	if u.Span.End == 0 {
		return fallback
	}
	return u.Span
}

type ExecutorNode struct {
	Type             *Type
	Span             source.Span
	FieldBindings    map[string]string
	FieldSpans       map[string]source.Span
	BoundTypes       map[string]*Type
	PathUses         map[string]DriverPathUse
	SlotLabel        string
	LoopPolicy       string
	LoopStrategy     string
	LoopFallback     string
	MemoryOwnerLabel string
}

type ExecutorSlotNode struct {
	Label   string
	Binding string
	Span    source.Span
}

type TopicNode struct {
	Label        string
	Type         string
	TypeKey      string
	Kind         string
	Depth        uint64
	PayloadType  string
	PayloadKey   string
	PayloadSize  uint64
	PayloadAlign uint64
	NextType     string
	NextKey      string
	Binding      string
	Span         source.Span
}

type TopicPublisherNode struct {
	TopicLabel string
	OwnerKind  string
	OwnerLabel string
	Binding    string
	Span       source.Span
}

type TopicSubscriptionNode struct {
	TopicLabel      string
	SubscriberLabel string
	Binding         string
	Span            source.Span
}

type SubscriptionUseNode struct {
	TopicLabel          string
	FieldName           string
	CurrentExecutorType string
	Span                source.Span
}

type ReliableTryPublishCallNode struct {
	ResultObserved bool
	Span           source.Span
}

type PathNode struct {
	Label               string
	Kind                string
	Binding             string
	PublishesInterrupts bool
	Span                source.Span
}

type InterruptTopicRouteNode struct {
	Vector              int
	PathLabel           string
	PathBinding         string
	PathBindingType     *Type
	PathField           string
	ContextSymbol       string
	PathFieldOffset     int
	TopicLabel          string
	TopicKind           string
	EventType           string
	EventFunctionSymbol string
	SubscriberSlots     []string
	Span                source.Span
}

type InterruptConfiguratorNode struct {
	TopicKind string
	Vector    int
	Span      source.Span
}

type APICFactNode struct {
	Mode            string
	Required        bool
	Fallback        string
	XAPICAvailable  bool
	X2APICAvailable bool
	Span            source.Span
}

type TimerFactNode struct {
	Label    string
	Source   string
	PeriodUS uint64
	Span     source.Span
}

type TimerRouteNode struct {
	Label           string
	Source          string
	PeriodUS        uint64
	Vector          uint8
	TopicLabel      string
	SubscriberSlots []string
	Span            source.Span
}

type LocalityFactNode struct {
	Subject string
	Kind    string
	Value   string
	Known   bool
	Span    source.Span
}

type FramebufferFactNode struct {
	Base   uint64
	Bytes  uint64
	Width  uint32
	Height uint32
	Stride uint32
	Format uint32
	Known  bool
	Span   source.Span
}

type HardwareClaimNode struct {
	Kind string
	Key  string
	Span source.Span
}

type MemoryRootNode struct {
	Label string
	Base  uint64
	Bytes uint64
	Span  source.Span
}

type ArenaNode struct {
	Label  string
	Parent string
	Base   uint64
	Offset uint64
	Bytes  uint64
	Align  uint64
	Owner  string
	Kind   string
	Span   source.Span
}

type DMABufferNode struct {
	Label       string
	OwnerDevice string
	Base        uint64
	Bytes       uint64
	Span        source.Span
}

type InterruptQueueNode struct {
	Label        string
	Owner        string
	Capacity     uint64
	PayloadKind  string
	PayloadSize  uint64
	PayloadAlign uint64
	Overflow     string
	Span         source.Span
}

type SharedInterruptSourceNode struct {
	RouteKey    string
	SourceLabel string
	Vector      int
	Span        source.Span
}

type VcpuPlacementNode struct {
	VcpuID          int
	ExecutorBinding string
	SlotLabel       string
	Terminal        bool
	Span            source.Span
}

type PlacementConstraintNode struct {
	Kind      string
	A         string
	B         string
	Required  bool
	Satisfied bool
	Fallback  string
	Span      source.Span
}

type PlacementDecisionNode struct {
	SlotLabel string
	Target    string
	Satisfied bool
	Fallback  string
	Span      source.Span
}

type WakeTargetNode struct {
	SlotLabel string
	Owner     string
	Strategy  string
	Fallback  string
	Span      source.Span
}

type ImageGraph struct {
	Constructed             []ConstructedNode
	DriverPaths             []DriverPathNode
	Executors               []ExecutorNode
	ExecutorSlots           []ExecutorSlotNode
	Topics                  []TopicNode
	TopicPublishers         []TopicPublisherNode
	TopicSubscriptions      []TopicSubscriptionNode
	SubscriptionUses        []SubscriptionUseNode
	ReliableTryPublishCalls []ReliableTryPublishCallNode
	Paths                   []PathNode
	InterruptTopicRoutes    []InterruptTopicRouteNode
	InterruptConfigurators  []InterruptConfiguratorNode
	APICFacts               []APICFactNode
	TimerFacts              []TimerFactNode
	TimerRoutes             []TimerRouteNode
	LocalityFacts           []LocalityFactNode
	FramebufferFacts        []FramebufferFactNode
	HardwareClaims          []HardwareClaimNode
	VcpuPlacements          []VcpuPlacementNode
	PlacementConstraints    []PlacementConstraintNode
	PlacementDecisions      []PlacementDecisionNode
	WakeTargets             []WakeTargetNode
	MemoryRoots             []MemoryRootNode
	Arenas                  []ArenaNode
	DMABuffers              []DMABufferNode
	InterruptQueues         []InterruptQueueNode
	SharedInterruptSources  []SharedInterruptSourceNode
	StoragePaths            []StoragePathNode
	CoreLinkEndpoints       []CoreLinkEndpointNode
	ProjectionFeeds         []ProjectionFeedNode
	StorageWriters          []StorageWriterNode
	StorageAppendCalls      []StorageAppendCallNode
}

func sharedIRQRouteKey(irq uint64, vector uint64) string {
	return fmt.Sprintf("isa_irq:%d/vector:0x%02x", irq, vector)
}

func (g ImageGraph) TopicByLabel(label string) TopicNode {
	for _, topic := range g.Topics {
		if topic.Label == label {
			return topic
		}
	}
	return TopicNode{}
}

func (g ImageGraph) ExecutorBySlot(slot string) ExecutorNode {
	for _, exec := range g.Executors {
		if exec.SlotLabel == slot {
			return exec
		}
	}
	return ExecutorNode{}
}

func (g ImageGraph) HasWakeSource(slot string) bool {
	for _, sub := range g.TopicSubscriptions {
		if sub.SubscriberLabel == slot {
			return true
		}
	}
	for _, route := range g.InterruptTopicRoutes {
		for _, subscriber := range route.SubscriberSlots {
			if subscriber == slot {
				return true
			}
		}
	}
	for _, route := range g.TimerRoutes {
		for _, subscriber := range route.SubscriberSlots {
			if subscriber == slot {
				return true
			}
		}
	}
	return false
}
