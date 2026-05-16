package sem

import "testing"

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
        hardware.vcpu1.enter(worker)
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
