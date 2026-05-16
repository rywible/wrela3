package sem

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/source"
)

func TestExecutorTopicKindClassification(t *testing.T) {
	tests := []struct {
		name string
		typ  *Type
		fn   func(*Type) bool
	}{
		{
			name: "executor slot",
			typ:  &Type{Module: "machine.x86_64.cpu_state", Name: "ExecutorSlot", Kind: KindClass},
			fn:   IsExecutorSlotType,
		},
		{
			name: "vcpu",
			typ:  &Type{Module: "machine.x86_64.cpu_state", Name: "Vcpu", Kind: KindClass},
			fn:   IsVcpuType,
		},
		{
			name: "gap topic",
			typ:  &Type{Module: "machine.x86_64.topic_u64", Name: "U64GapTopic", Kind: KindClass},
			fn:   IsTopicType,
		},
		{
			name: "reliable topic",
			typ:  &Type{Module: "machine.x86_64.topic_u64", Name: "U64ReliableTopic", Kind: KindClass},
			fn:   IsTopicType,
		},
		{
			name: "gap publisher",
			typ:  &Type{Module: "machine.x86_64.topic_u64", Name: "U64GapPublisher", Kind: KindClass},
			fn:   IsTopicPublisherType,
		},
		{
			name: "reliable publisher",
			typ:  &Type{Module: "machine.x86_64.topic_u64", Name: "U64ReliablePublisher", Kind: KindClass},
			fn:   IsTopicPublisherType,
		},
		{
			name: "gap subscription",
			typ:  &Type{Module: "machine.x86_64.topic_u64", Name: "U64GapSubscription", Kind: KindClass},
			fn:   IsTopicSubscriptionType,
		},
		{
			name: "reliable subscription",
			typ:  &Type{Module: "machine.x86_64.topic_u64", Name: "U64ReliableSubscription", Kind: KindClass},
			fn:   IsTopicSubscriptionType,
		},
		{
			name: "serial rx topic",
			typ:  &Type{Module: "machine.x86_64.serial", Name: "SerialRxTopic", Kind: KindClass},
			fn:   IsTopicType,
		},
		{
			name: "serial publisher",
			typ:  &Type{Module: "machine.x86_64.serial", Name: "SerialRxPublisher", Kind: KindClass},
			fn:   IsTopicPublisherType,
		},
		{
			name: "serial subscription",
			typ:  &Type{Module: "machine.x86_64.serial", Name: "SerialRxSubscription", Kind: KindClass},
			fn:   IsTopicSubscriptionType,
		},
		{
			name: "edu interrupt topic",
			typ:  &Type{Module: "machine.x86_64.edu", Name: "EduInterruptTopic", Kind: KindClass},
			fn:   IsTopicType,
		},
		{
			name: "edu interrupt publisher",
			typ:  &Type{Module: "machine.x86_64.edu", Name: "EduInterruptPublisher", Kind: KindClass},
			fn:   IsTopicPublisherType,
		},
		{
			name: "edu interrupt subscription",
			typ:  &Type{Module: "machine.x86_64.edu", Name: "EduInterruptSubscription", Kind: KindClass},
			fn:   IsTopicSubscriptionType,
		},
		{
			name: "ivshmem doorbell topic",
			typ:  &Type{Module: "machine.x86_64.ivshmem", Name: "IvshmemDoorbellTopic", Kind: KindClass},
			fn:   IsTopicType,
		},
		{
			name: "ivshmem doorbell publisher",
			typ:  &Type{Module: "machine.x86_64.ivshmem", Name: "IvshmemDoorbellPublisher", Kind: KindClass},
			fn:   IsTopicPublisherType,
		},
		{
			name: "ivshmem doorbell subscription",
			typ:  &Type{Module: "machine.x86_64.ivshmem", Name: "IvshmemDoorbellSubscription", Kind: KindClass},
			fn:   IsTopicSubscriptionType,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !test.fn(test.typ) {
				t.Fatalf("expected %s to be classified", qualifiedTypeName(test.typ))
			}
		})
	}

	shadow := &Type{Module: "user.module", Name: "ExecutorSlot", Kind: KindClass}
	if IsExecutorSlotType(shadow) {
		t.Fatalf("user.module.ExecutorSlot should not be classified as an executor slot")
	}
}

func TestExecutorSlotTopicGraphExtraction(t *testing.T) {
	contract := parseModulesForTest(t, `
module machine.x86_64.cpu_state
data SlotIdentity { label: StringLiteral }
data ExecutorSlot { id: U64 }
data VcpuStartStatus { started: Bool; id: U64 }
class ExecutorRegistry {
    next_id: U64
    fn claim(self, identity: SlotIdentity) -> ExecutorSlot { return ExecutorSlot(id = 0) }
}

class Vcpu { id: U64 }
class OwnedHardware {
    executors: ExecutorRegistry
    vcpu0: Vcpu
    vcpu1: Vcpu
}
unique class DelegatedHardware {
    asm fn exit_to_owned_hardware(self) -> OwnedHardware { ret }
}
`, `
module machine.x86_64.executor_memory
class ExecutorMemory { arena_base: PhysicalAddress; arena_length: U64; next_offset: U64 }
`, `
module machine.x86_64.executor_loop
class EventSleepPolicy { asm fn wait(self) { hlt; ret } }
`, `
module machine.x86_64.topic_u64
use { ExecutorSlot } from machine.x86_64.cpu_state
data TopicIdentity { label: StringLiteral }
class U64GapTopic {
    identity: TopicIdentity
    id: U64
    depth: U64
    asm fn subscribe(self, subscriber: ExecutorSlot) -> U64GapSubscription { ret }
}
data U64GapSubscription { topic: U64GapTopic; subscriber: ExecutorSlot; cursor: U64; armed: Bool }
`)
	src := `
module test.graph
use { ExecutorSlot, SlotIdentity, OwnedHardware, DelegatedHardware } from machine.x86_64.cpu_state
use { U64GapSubscription, U64GapTopic, TopicIdentity } from machine.x86_64.topic_u64
use { EventSleepPolicy } from machine.x86_64.executor_loop
use { ExecutorMemory } from machine.x86_64.executor_memory

executor Worker {
    slot: ExecutorSlot
    loop: EventSleepPolicy
    memory: ExecutorMemory
    input: U64GapSubscription
    start fn run(self) -> never { while true {} }
}

image Img {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware { return hardware.exit_to_owned_hardware() }
    phase owned_hardware(hardware: OwnedHardware) -> never {
        let worker_slot = hardware.executors.claim(identity = SlotIdentity(label = "worker"))
        let topic = U64GapTopic(identity = TopicIdentity(label = "counter"), id = 0, depth = 64)
        let input = topic.subscribe(subscriber = worker_slot)
        let worker = Worker(slot = worker_slot, loop = EventSleepPolicy(), memory = ExecutorMemory(arena_base = 0, arena_length = 4096, next_offset = 0), input = input)
        hardware.vcpu1.start(executor = worker)
        while true {}
    }
}`
	modules := append(contract, parseModulesForTest(t, src)...)
	index := mustBuildIndex(t, modules)
	checked, ds := Check(index, modules)
	if len(ds) != 0 {
		t.Fatalf("Check diagnostics: %#v", ds)
	}
	if len(checked.ImageGraph.ExecutorSlots) != 1 || checked.ImageGraph.ExecutorSlots[0].Label != "worker" {
		t.Fatalf("executor slots = %#v", checked.ImageGraph.ExecutorSlots)
	}
	if len(checked.ImageGraph.TopicSubscriptions) != 1 || checked.ImageGraph.TopicSubscriptions[0].SubscriberLabel != "worker" {
		t.Fatalf("topic subscriptions = %#v", checked.ImageGraph.TopicSubscriptions)
	}
	if len(checked.ImageGraph.VcpuPlacements) != 1 || checked.ImageGraph.VcpuPlacements[0].VcpuID != 1 {
		t.Fatalf("vcpu placements = %#v", checked.ImageGraph.VcpuPlacements)
	}
}

func TestExecutorTopicGraphDiagnostics(t *testing.T) {
	workerType := &Type{Name: "Worker", Kind: KindExecutor}
	otherType := &Type{Name: "Other", Kind: KindExecutor}
	pathType := &Type{Name: "SerialPath", Kind: KindDriverPath}

	tests := []struct {
		name  string
		code  string
		graph ImageGraph
	}{
		{
			name: "duplicate slot label",
			code: diag.SEM0033,
			graph: ImageGraph{ExecutorSlots: []ExecutorSlotNode{
				{Label: "worker", Binding: "a", Span: testSpan(1)},
				{Label: "worker", Binding: "b", Span: testSpan(2)},
			}},
		},
		{
			name: "subscription executor slot mismatch",
			code: diag.SEM0035,
			graph: ImageGraph{
				Executors: []ExecutorNode{{
					Type:          workerType,
					FieldBindings: map[string]string{"input": "sub"},
					BoundTypes:    map[string]*Type{"input": topicSubscriptionType()},
					SlotLabel:     "worker",
					FieldSpans:    map[string]source.Span{"input": testSpan(3)},
				}},
				TopicSubscriptions: []TopicSubscriptionNode{{TopicLabel: "counter", SubscriberLabel: "other", Binding: "sub", Span: testSpan(4)}},
			},
		},
		{
			name: "shared path across executor constructors",
			code: diag.SEM0038,
			graph: ImageGraph{
				DriverPaths: []DriverPathNode{{Type: pathType, Binding: "path", Span: testSpan(5)}},
				Executors: []ExecutorNode{
					{Type: workerType, PathUses: map[string]DriverPathUse{"serial": {Key: "span:5:6", Span: testSpan(6)}}},
					{Type: otherType, PathUses: map[string]DriverPathUse{"serial": {Key: "span:5:6", Span: testSpan(7)}}},
				},
			},
		},
		{
			name: "two executors started on same vcpu",
			code: diag.SEM0037,
			graph: ImageGraph{VcpuPlacements: []VcpuPlacementNode{
				{VcpuID: 1, ExecutorBinding: "a", SlotLabel: "a", Span: testSpan(8)},
				{VcpuID: 1, ExecutorBinding: "b", SlotLabel: "b", Span: testSpan(9)},
			}},
		},
		{
			name: "publisher assigned to more than one producer",
			code: diag.SEM0039,
			graph: ImageGraph{
				Executors: []ExecutorNode{
					{Type: workerType, FieldBindings: map[string]string{"out": "pub"}, BoundTypes: map[string]*Type{"out": topicPublisherType()}, FieldSpans: map[string]source.Span{"out": testSpan(10)}},
					{Type: otherType, FieldBindings: map[string]string{"out": "pub"}, BoundTypes: map[string]*Type{"out": topicPublisherType()}, FieldSpans: map[string]source.Span{"out": testSpan(11)}},
				},
				TopicPublishers: []TopicPublisherNode{{TopicLabel: "counter", Binding: "pub", Span: testSpan(12)}},
			},
		},
		{
			name:  "slot not bound to one executor",
			code:  diag.SEM0034,
			graph: ImageGraph{ExecutorSlots: []ExecutorSlotNode{{Label: "worker", Binding: "slot", Span: testSpan(13)}}},
		},
		{
			name: "terminal vcpu enter has reachable follower",
			code: diag.SEM0036,
			graph: ImageGraph{VcpuPlacements: []VcpuPlacementNode{
				{VcpuID: 0, ExecutorBinding: "worker", SlotLabel: "worker", Terminal: true, Span: testSpan(14)},
				{VcpuID: 1, ExecutorBinding: "other", SlotLabel: "other", Span: testSpan(15)},
			}},
		},
		{
			name:  "subscription used by wrong executor",
			code:  diag.SEM0040,
			graph: ImageGraph{SubscriptionUses: []SubscriptionUseNode{{TopicLabel: "counter", SubscriberLabel: "worker", CurrentSlotLabel: "other", Span: testSpan(16)}}},
		},
		{
			name: "gap topic uses no gap policy",
			code: diag.SEM0041,
			graph: ImageGraph{
				Executors:          []ExecutorNode{{Type: workerType, SlotLabel: "worker", LoopPolicy: "NoGapRequiredPolicy"}},
				TopicSubscriptions: []TopicSubscriptionNode{{TopicLabel: "counter", SubscriberLabel: "worker", Binding: "sub", Span: testSpan(17)}},
				Topics:             []TopicNode{{Label: "counter", Kind: "gap", Span: testSpan(18)}},
			},
		},
		{
			name:  "sleeping executor has no wake source",
			code:  diag.SEM0044,
			graph: ImageGraph{Executors: []ExecutorNode{{Type: workerType, SlotLabel: "worker", LoopPolicy: "EventSleepPolicy", Span: testSpan(19)}}},
		},
		{
			name:  "reliable try publish result ignored",
			code:  diag.SEM0045,
			graph: ImageGraph{ReliableTryPublishCalls: []ReliableTryPublishCallNode{{ResultObserved: false, Span: testSpan(20)}}},
		},
		{
			name:  "executor memory owner mismatch",
			code:  diag.SEM0047,
			graph: ImageGraph{Executors: []ExecutorNode{{Type: workerType, SlotLabel: "worker", MemoryOwnerLabel: "other", Span: testSpan(21)}}},
		},
		{
			name:  "publishing path missing identity",
			code:  diag.SEM0048,
			graph: ImageGraph{Paths: []PathNode{{Binding: "path", PublishesInterrupts: true, Span: testSpan(22)}}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := &checker{graph: test.graph}
			c.checkExecutorTopicGraph()
			if !hasCode(c.diags, test.code) {
				t.Fatalf("expected %s, got %#v", test.code, c.diags)
			}
		})
	}
}

func testSpan(start int) source.Span {
	return source.Span{Start: start, End: start + 1}
}

func topicPublisherType() *Type {
	return &Type{Module: "machine.x86_64.topic_u64", Name: "U64ReliablePublisher", Kind: KindClass}
}

func topicSubscriptionType() *Type {
	return &Type{Module: "machine.x86_64.topic_u64", Name: "U64GapSubscription", Kind: KindData}
}
