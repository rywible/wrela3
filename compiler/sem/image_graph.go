package sem

import "github.com/ryanwible/wrela3/compiler/source"

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
	MemoryOwnerLabel string
}

type ExecutorSlotNode struct {
	Label   string
	Binding string
	Span    source.Span
}

type TopicNode struct {
	Label   string
	Kind    string
	Binding string
	Span    source.Span
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
	TopicLabel       string
	SubscriberLabel  string
	CurrentSlotLabel string
	Span             source.Span
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
	ContextSymbol       string
	PathFieldOffset     int
	TopicLabel          string
	TopicKind           string
	EventType           string
	EventFunctionSymbol string
	SubscriberSlots     []string
	Span                source.Span
}

type VcpuPlacementNode struct {
	VcpuID          int
	ExecutorBinding string
	SlotLabel       string
	Terminal        bool
	Span            source.Span
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
	VcpuPlacements          []VcpuPlacementNode
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
	return false
}
