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
	    asm fn publisher(self) -> U64GapPublisher { ret }
	    asm fn subscribe(self, subscriber: ExecutorSlot) -> U64GapSubscription { ret }
	}
	class U64GapPublisher { topic: U64GapTopic }
	data U64GapSubscription { topic: U64GapTopic; subscriber: ExecutorSlot; cursor: U64; armed: Bool }
	`)
	src := `
module test.graph
use { ExecutorSlot, SlotIdentity, OwnedHardware, DelegatedHardware } from machine.x86_64.cpu_state
	use { U64GapPublisher, U64GapSubscription, U64GapTopic, TopicIdentity } from machine.x86_64.topic_u64
use { EventSleepPolicy } from machine.x86_64.executor_loop

executor Worker {
    slot: ExecutorSlot
    loop: EventSleepPolicy
	    input: U64GapSubscription
	    out: U64GapPublisher
	    start fn run(self) -> never { while true {} }
	}

image Img {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware { return hardware.exit_to_owned_hardware() }
    phase owned_hardware(hardware: OwnedHardware) -> never {
        let worker_slot = hardware.executors.claim(identity = SlotIdentity(label = "worker"))
        let topic = U64GapTopic(identity = TopicIdentity(label = "counter"), id = 0, depth = 64)
        let input = topic.subscribe(subscriber = worker_slot)
	        let worker = Worker(slot = worker_slot, loop = EventSleepPolicy(), input = input, out = topic.publisher())
        hardware.vcpu0.enter(executor = worker)
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
	if len(checked.ImageGraph.Topics) != 1 || checked.ImageGraph.Topics[0].Depth != 64 {
		t.Fatalf("topics = %#v, want one depth-64 topic", checked.ImageGraph.Topics)
	}
	if len(checked.ImageGraph.TopicPublishers) != 1 || checked.ImageGraph.TopicPublishers[0].TopicLabel != "counter" {
		t.Fatalf("topic publishers = %#v", checked.ImageGraph.TopicPublishers)
	}
	if len(checked.ImageGraph.Executors) != 1 || checked.ImageGraph.Executors[0].FieldBindings["out"] == "" {
		t.Fatalf("executor publisher field binding missing: %#v", checked.ImageGraph.Executors)
	}
	if len(checked.ImageGraph.VcpuPlacements) != 1 || checked.ImageGraph.VcpuPlacements[0].VcpuID != 0 {
		t.Fatalf("vcpu placements = %#v", checked.ImageGraph.VcpuPlacements)
	}
}

func TestRawExecutorSlotConstructorCannotBypassClaimGraph(t *testing.T) {
	modules := parseModulesForTest(t, `
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
}
unique class DelegatedHardware {
    asm fn exit_to_owned_hardware(self) -> OwnedHardware { ret }
}
`, `
module machine.x86_64.executor_loop
class HotPollPolicy {}
`, `
module test.raw_executor_slot
use { DelegatedHardware, ExecutorSlot, OwnedHardware } from machine.x86_64.cpu_state
use { HotPollPolicy } from machine.x86_64.executor_loop

executor Worker {
    slot: ExecutorSlot
    loop: HotPollPolicy
    start fn run(self) -> never { while true {} }
}

image Img {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware { return hardware.exit_to_owned_hardware() }
    phase owned_hardware(hardware: OwnedHardware) -> never {
        let raw_slot = ExecutorSlot(id = 0)
        let worker = Worker(slot = raw_slot, loop = HotPollPolicy())
        hardware.vcpu0.enter(executor = worker)
    }
}
`)
	index := mustBuildIndex(t, modules)
	_, ds := Check(index, modules)
	if !hasMessage(ds, diag.SEM0035, "executor Worker uses an unclaimed executor slot") {
		t.Fatalf("expected SEM0035 for raw executor slot, got %#v", ds)
	}
}

func TestForgedExecutorRegistryClaimCannotBypassClaimGraph(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.cpu_state
data SlotIdentity { label: StringLiteral }
data ExecutorSlot { id: U64 }
class ExecutorRegistry {
    next_id: U64
    fn claim(self, identity: SlotIdentity) -> ExecutorSlot { return ExecutorSlot(id = 0) }
}
class Vcpu {}
class OwnedHardware { vcpu0: Vcpu; executors: ExecutorRegistry }
unique class DelegatedHardware { asm fn exit_to_owned_hardware(self) -> OwnedHardware { ret } }
`, `
module test.forged_executor_registry
use { DelegatedHardware, ExecutorRegistry, ExecutorSlot, OwnedHardware, SlotIdentity } from machine.x86_64.cpu_state

executor Worker {
    slot: ExecutorSlot
    start fn run(self) -> never { while true {} }
}

image Img {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware { return hardware.exit_to_owned_hardware() }
    phase owned_hardware(hardware: OwnedHardware) -> never {
        let registry = ExecutorRegistry(next_id = 0)
        let worker_slot = registry.claim(identity = SlotIdentity(label = "worker"))
        let worker = Worker(slot = worker_slot)
        hardware.vcpu0.enter(executor = worker)
    }
}
`)
	index := mustBuildIndex(t, modules)
	_, ds := Check(index, modules)
	if !hasCode(ds, diag.SEM0049) {
		t.Fatalf("expected SEM0049 for forged ExecutorRegistry, got %#v", ds)
	}
	if !hasMessage(ds, diag.SEM0035, "executor Worker uses an unclaimed executor slot") {
		t.Fatalf("expected SEM0035 for forged registry claim, got %#v", ds)
	}
}

func TestInlineTopicSubscriptionGraphExtraction(t *testing.T) {
	contract := parseModulesForTest(t, `
module machine.x86_64.cpu_state
data SlotIdentity { label: StringLiteral }
data ExecutorSlot { id: U64 }
data VcpuStartStatus { started: Bool; id: U64 }
class ExecutorRegistry { fn claim(self, identity: SlotIdentity) -> ExecutorSlot { return ExecutorSlot(id = 0) } }
class Vcpu {}
class OwnedHardware { vcpu0: Vcpu; executors: ExecutorRegistry }
unique class DelegatedHardware { fn exit_to_owned_hardware(self) -> OwnedHardware { return OwnedHardware(vcpu0 = Vcpu(), executors = ExecutorRegistry()) } }
`, `
module machine.x86_64.topic_u64
use { ExecutorSlot } from machine.x86_64.cpu_state
data TopicIdentity { label: StringLiteral }
class U64GapTopic {
    identity: TopicIdentity
    asm fn subscribe(self, subscriber: ExecutorSlot) -> U64GapSubscription { ret }
}
data U64GapSubscription { topic: U64GapTopic; subscriber: ExecutorSlot }
	`)
	src := `
module test.inline_subscription_graph
use { DelegatedHardware, ExecutorSlot, OwnedHardware, SlotIdentity } from machine.x86_64.cpu_state
use { TopicIdentity, U64GapSubscription, U64GapTopic } from machine.x86_64.topic_u64

executor Worker {
    slot: ExecutorSlot
    input: U64GapSubscription
    start fn run(self) -> never { while true {} }
}

image Img {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware { return hardware.exit_to_owned_hardware() }
    phase owned_hardware(hardware: OwnedHardware) -> never {
        let worker_slot = hardware.executors.claim(identity = SlotIdentity(label = "worker"))
        let topic = U64GapTopic(identity = TopicIdentity(label = "counter"))
        let worker = Worker(slot = worker_slot, input = topic.subscribe(subscriber = worker_slot))
        hardware.vcpu0.enter(executor = worker)
    }
}`
	modules := append(contract, parseModulesForTest(t, src)...)
	index := mustBuildIndex(t, modules)
	checked, ds := Check(index, modules)
	if len(ds) != 0 {
		t.Fatalf("Check diagnostics: %#v", ds)
	}
	if len(checked.ImageGraph.TopicSubscriptions) != 1 || checked.ImageGraph.TopicSubscriptions[0].SubscriberLabel != "worker" {
		t.Fatalf("topic subscriptions = %#v", checked.ImageGraph.TopicSubscriptions)
	}
}

func TestInlineTopicSubscriptionSlotMismatchIsRejectedFromSource(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.cpu_state
data SlotIdentity { label: StringLiteral }
data ExecutorSlot { id: U64 }
class ExecutorRegistry { fn claim(self, identity: SlotIdentity) -> ExecutorSlot { return ExecutorSlot(id = 0) } }
class Vcpu {}
class OwnedHardware { vcpu0: Vcpu; executors: ExecutorRegistry }
unique class DelegatedHardware { fn exit_to_owned_hardware(self) -> OwnedHardware { return OwnedHardware(vcpu0 = Vcpu(), executors = ExecutorRegistry()) } }
`, `
module machine.x86_64.topic_u64
use { ExecutorSlot } from machine.x86_64.cpu_state
data TopicIdentity { label: StringLiteral }
class U64GapTopic {
    identity: TopicIdentity
    asm fn subscribe(self, subscriber: ExecutorSlot) -> U64GapSubscription { ret }
}
data U64GapSubscription { topic: U64GapTopic; subscriber: ExecutorSlot }
`, `
module test.inline_subscription_mismatch
use { DelegatedHardware, ExecutorSlot, OwnedHardware, SlotIdentity } from machine.x86_64.cpu_state
use { TopicIdentity, U64GapSubscription, U64GapTopic } from machine.x86_64.topic_u64

executor Worker {
    slot: ExecutorSlot
    input: U64GapSubscription
    start fn run(self) -> never { while true {} }
}

image Img {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware { return hardware.exit_to_owned_hardware() }
    phase owned_hardware(hardware: OwnedHardware) -> never {
        let worker_slot = hardware.executors.claim(identity = SlotIdentity(label = "worker"))
        let other_slot = hardware.executors.claim(identity = SlotIdentity(label = "other"))
        let topic = U64GapTopic(identity = TopicIdentity(label = "counter"))
        let worker = Worker(slot = worker_slot, input = topic.subscribe(subscriber = other_slot))
        hardware.vcpu0.enter(executor = worker)
    }
}
`)
	index := mustBuildIndex(t, modules)
	_, ds := Check(index, modules)
	if !hasCode(ds, diag.SEM0035) {
		t.Fatalf("expected SEM0035, got %#v", ds)
	}
}

func TestInlineExecutorArenaOwnerMismatchIsRejectedFromSource(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.cpu_state
use { ExecutorMemory } from machine.x86_64.executor_memory
data SlotIdentity { label: StringLiteral }
data ExecutorSlot { id: U64 }
class ExecutorRegistry { fn claim(self, identity: SlotIdentity) -> ExecutorSlot { return ExecutorSlot(id = 0) } }
class OwnedMemory { asm fn claim_executor_arena(self, owner: ExecutorSlot) -> ExecutorMemory { ret } }
class Vcpu {}
class OwnedHardware { vcpu0: Vcpu; executors: ExecutorRegistry }
unique class DelegatedHardware { fn exit_to_owned_hardware(self) -> OwnedHardware { return OwnedHardware(vcpu0 = Vcpu(), executors = ExecutorRegistry()) } }
`, `
module machine.x86_64.executor_memory
class ExecutorMemory {}
`, `
module test.inline_memory_mismatch
use { DelegatedHardware, ExecutorSlot, OwnedHardware, OwnedMemory, SlotIdentity } from machine.x86_64.cpu_state
use { ExecutorMemory } from machine.x86_64.executor_memory

executor Worker {
    slot: ExecutorSlot
    memory: ExecutorMemory
    start fn run(self) -> never { while true {} }
}

image Img {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware { return hardware.exit_to_owned_hardware() }
    phase owned_hardware(hardware: OwnedHardware) -> never {
        let worker_slot = hardware.executors.claim(identity = SlotIdentity(label = "worker"))
        let other_slot = hardware.executors.claim(identity = SlotIdentity(label = "other"))
        let worker = Worker(slot = worker_slot, memory = OwnedMemory().claim_executor_arena(owner = other_slot))
        hardware.vcpu0.enter(executor = worker)
    }
}
`)
	index := mustBuildIndex(t, modules)
	_, ds := Check(index, modules)
	if !hasCode(ds, diag.SEM0047) {
		t.Fatalf("expected SEM0047, got %#v", ds)
	}
}

func TestHardwarePlanExecutorMemoryCannotLaunderOwner(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory
data ExecutorMemory {}
`, `
module machine.x86_64.cpu_state
use { ExecutorMemory } from machine.x86_64.executor_memory
data SlotIdentity { label: StringLiteral }
data ExecutorSlot { id: U64 }
class ExecutorRegistry {
    next_id: U64
    fn claim(self, identity: SlotIdentity) -> ExecutorSlot {
        let id = self.next_id
        self.next_id = self.next_id + 1
        return ExecutorSlot(id = id)
    }
}
data HardwarePlan {
    console_memory: ExecutorMemory
    worker_memory: ExecutorMemory

    fn executor_memory(self, owner: ExecutorSlot, memory: ExecutorMemory) -> ExecutorMemory {
        return memory
    }
}
class Vcpu {}
class OwnedHardware {
    vcpu0: Vcpu
    executors: ExecutorRegistry
    hardware_plan: HardwarePlan
}
unique class DelegatedHardware {
    fn exit_to_owned_hardware(self) -> OwnedHardware {
        return OwnedHardware(
            vcpu0 = Vcpu(),
            executors = ExecutorRegistry(next_id = 0),
            hardware_plan = HardwarePlan(console_memory = ExecutorMemory(), worker_memory = ExecutorMemory())
        )
    }
}
`, `
module test.hardware_plan_memory_launder
use { DelegatedHardware, ExecutorSlot, OwnedHardware, SlotIdentity } from machine.x86_64.cpu_state
use { ExecutorMemory } from machine.x86_64.executor_memory

executor Worker {
    slot: ExecutorSlot
    memory: ExecutorMemory
    start fn run(self) -> never { while true {} }
}

image Img {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware { return hardware.exit_to_owned_hardware() }
    phase owned_hardware(hardware: OwnedHardware) -> never {
        let console_slot = hardware.executors.claim(identity = SlotIdentity(label = "console"))
        let worker_slot = hardware.executors.claim(identity = SlotIdentity(label = "worker"))
        let worker_memory = hardware.hardware_plan.executor_memory(
            owner = worker_slot,
            memory = hardware.hardware_plan.console_memory
        )
        let worker = Worker(slot = worker_slot, memory = worker_memory)
        hardware.vcpu0.enter(executor = worker)
    }
}
`)
	index := mustBuildIndex(t, modules)
	_, ds := Check(index, modules)
	if !hasMessage(ds, diag.SEM0047, "executor Worker memory is owned by console but slot is worker") {
		t.Fatalf("expected SEM0047 for laundered executor memory, got %#v", ds)
	}
}

func TestOwnedHardwareMustTerminalEnterVcpu0FromSource(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.cpu_state
data SlotIdentity { label: StringLiteral }
data ExecutorSlot { id: U64 }
data VcpuStartStatus { started: Bool; id: U64 }
class ExecutorRegistry { fn claim(self, identity: SlotIdentity) -> ExecutorSlot { return ExecutorSlot(id = 0) } }
class Vcpu {}
class OwnedHardware { vcpu0: Vcpu; vcpu1: Vcpu; executors: ExecutorRegistry }
unique class DelegatedHardware { fn exit_to_owned_hardware(self) -> OwnedHardware { return OwnedHardware(vcpu0 = Vcpu(), vcpu1 = Vcpu(), executors = ExecutorRegistry()) } }

executor Worker {
    slot: ExecutorSlot
    start fn run(self) -> never { while true {} }
}

image Img {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware { return hardware.exit_to_owned_hardware() }
    phase owned_hardware(hardware: OwnedHardware) -> never {
        let worker_slot = hardware.executors.claim(identity = SlotIdentity(label = "worker"))
        let worker = Worker(slot = worker_slot)
        hardware.vcpu1.start(executor = worker)
        while true {}
    }
}
`)
	index := mustBuildIndex(t, modules)
	_, ds := Check(index, modules)
	if !hasCode(ds, diag.SEM0036) {
		t.Fatalf("expected SEM0036, got %#v", ds)
	}
}

func TestConditionalVcpu0EnterIsRejectedFromSource(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.cpu_state
data SlotIdentity { label: StringLiteral }
data ExecutorSlot { id: U64 }
class ExecutorRegistry { fn claim(self, identity: SlotIdentity) -> ExecutorSlot { return ExecutorSlot(id = 0) } }
class Vcpu {}
class OwnedHardware { vcpu0: Vcpu; executors: ExecutorRegistry }
unique class DelegatedHardware { fn exit_to_owned_hardware(self) -> OwnedHardware { return OwnedHardware(vcpu0 = Vcpu(), executors = ExecutorRegistry()) } }

executor Worker {
    slot: ExecutorSlot
    start fn run(self) -> never { while true {} }
}

image Img {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware { return hardware.exit_to_owned_hardware() }
    phase owned_hardware(hardware: OwnedHardware) -> never {
        let worker_slot = hardware.executors.claim(identity = SlotIdentity(label = "worker"))
        let worker = Worker(slot = worker_slot)
        if true {
            hardware.vcpu0.enter(executor = worker)
        }
        while true {}
    }
}
`)
	index := mustBuildIndex(t, modules)
	_, ds := Check(index, modules)
	if !hasMessage(ds, diag.SEM0036, "vCPU enter must be the final reachable statement in the phase") {
		t.Fatalf("expected SEM0036 for conditional vcpu0.enter, got %#v", ds)
	}
}

func TestDirectTopicCapabilityConstructorsAreRejectedFromSource(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.cpu_state
data SlotIdentity { label: StringLiteral }
data ExecutorSlot { id: U64 }
class ExecutorRegistry { fn claim(self, identity: SlotIdentity) -> ExecutorSlot { return ExecutorSlot(id = 0) } }
class Vcpu {}
class OwnedHardware { vcpu0: Vcpu; executors: ExecutorRegistry }
unique class DelegatedHardware { fn exit_to_owned_hardware(self) -> OwnedHardware { return OwnedHardware(vcpu0 = Vcpu(), executors = ExecutorRegistry()) } }
`, `
module machine.x86_64.topic_u64
use { ExecutorSlot } from machine.x86_64.cpu_state
data TopicIdentity { label: StringLiteral }
class U64GapTopic {
    identity: TopicIdentity
    fn publisher(self) -> U64GapPublisher { return U64GapPublisher(topic = self) }
    fn subscribe(self, subscriber: ExecutorSlot) -> U64GapSubscription { return U64GapSubscription(topic = self, subscriber = subscriber, cursor = 0, armed = false) }
}
class U64GapPublisher { topic: U64GapTopic }
class U64GapSubscription { topic: U64GapTopic; subscriber: ExecutorSlot; cursor: U64; armed: Bool }
`, `
module test.direct_topic_capability
use { DelegatedHardware, ExecutorSlot, OwnedHardware, SlotIdentity } from machine.x86_64.cpu_state
use { TopicIdentity, U64GapPublisher, U64GapTopic } from machine.x86_64.topic_u64

executor Worker {
    slot: ExecutorSlot
    out: U64GapPublisher
    start fn run(self) -> never { while true {} }
}

image Img {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware { return hardware.exit_to_owned_hardware() }
    phase owned_hardware(hardware: OwnedHardware) -> never {
        let worker_slot = hardware.executors.claim(identity = SlotIdentity(label = "worker"))
        let topic = U64GapTopic(identity = TopicIdentity(label = "counter"))
        let worker = Worker(slot = worker_slot, out = U64GapPublisher(topic = topic))
        hardware.vcpu0.enter(executor = worker)
    }
}
`)
	index := mustBuildIndex(t, modules)
	_, ds := Check(index, modules)
	if !hasMessage(ds, diag.SEM0039, "topic publisher must be created with topic.publisher()") {
		t.Fatalf("expected SEM0039 for direct publisher constructor, got %#v", ds)
	}
}

func TestDirectExecutorMemoryConstructorWithSlotIsRejectedFromSource(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.cpu_state
data SlotIdentity { label: StringLiteral }
data ExecutorSlot { id: U64 }
class ExecutorRegistry { fn claim(self, identity: SlotIdentity) -> ExecutorSlot { return ExecutorSlot(id = 0) } }
class Vcpu {}
class OwnedHardware { vcpu0: Vcpu; executors: ExecutorRegistry }
unique class DelegatedHardware { fn exit_to_owned_hardware(self) -> OwnedHardware { return OwnedHardware(vcpu0 = Vcpu(), executors = ExecutorRegistry()) } }
`, `
module machine.x86_64.executor_memory
class ExecutorMemory { arena_base: PhysicalAddress; arena_length: U64; next_offset: U64 }
`, `
module test.direct_executor_memory
use { DelegatedHardware, ExecutorSlot, OwnedHardware, SlotIdentity } from machine.x86_64.cpu_state
use { ExecutorMemory } from machine.x86_64.executor_memory

executor Worker {
    slot: ExecutorSlot
    memory: ExecutorMemory
    start fn run(self) -> never { while true {} }
}

image Img {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware { return hardware.exit_to_owned_hardware() }
    phase owned_hardware(hardware: OwnedHardware) -> never {
        let worker_slot = hardware.executors.claim(identity = SlotIdentity(label = "worker"))
        let worker = Worker(slot = worker_slot, memory = ExecutorMemory(arena_base = 0, arena_length = 4096, next_offset = 0))
        hardware.vcpu0.enter(executor = worker)
    }
}
`)
	index := mustBuildIndex(t, modules)
	_, ds := Check(index, modules)
	if !hasMessage(ds, diag.SEM0047, "executor Worker memory must be claimed for slot worker") {
		t.Fatalf("expected SEM0047 for direct executor memory constructor, got %#v", ds)
	}
}

func TestPlacedExecutorWithoutSlotFieldIsRejectedFromSource(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.cpu_state
class Vcpu {}
class OwnedHardware { vcpu0: Vcpu }
unique class DelegatedHardware { fn exit_to_owned_hardware(self) -> OwnedHardware { return OwnedHardware(vcpu0 = Vcpu()) } }

executor Worker {
    start fn run(self) -> never { while true {} }
}

image Img {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware { return hardware.exit_to_owned_hardware() }
    phase owned_hardware(hardware: OwnedHardware) -> never {
        let worker = Worker()
        hardware.vcpu0.enter(executor = worker)
    }
}
`)
	index := mustBuildIndex(t, modules)
	_, ds := Check(index, modules)
	if !hasMessage(ds, diag.SEM0035, "executor worker must declare an ExecutorSlot field") {
		t.Fatalf("expected SEM0035 for placed executor without slot, got %#v", ds)
	}
}

func TestTopicFactoryCallOutsideImageWiringIsRejectedFromSource(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.cpu_state
data SlotIdentity { label: StringLiteral }
data ExecutorSlot { id: U64 }
class ExecutorRegistry { fn claim(self, identity: SlotIdentity) -> ExecutorSlot { return ExecutorSlot(id = 0) } }
class Vcpu {}
class OwnedHardware { vcpu0: Vcpu; executors: ExecutorRegistry }
unique class DelegatedHardware { fn exit_to_owned_hardware(self) -> OwnedHardware { return OwnedHardware(vcpu0 = Vcpu(), executors = ExecutorRegistry()) } }
`, `
module machine.x86_64.topic_u64
data TopicIdentity { label: StringLiteral }
class U64GapTopic {
    identity: TopicIdentity
    asm fn publisher(self) -> U64GapPublisher { ret }
}
class U64GapPublisher { topic: U64GapTopic }
`, `
module test.runtime_topic_factory
use { DelegatedHardware, ExecutorSlot, OwnedHardware, SlotIdentity } from machine.x86_64.cpu_state
use { TopicIdentity, U64GapTopic } from machine.x86_64.topic_u64

executor Worker {
    slot: ExecutorSlot
    topic: U64GapTopic
    start fn run(self) -> never {
        let pub = self.topic.publisher()
        while true {}
    }
}

image Img {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware { return hardware.exit_to_owned_hardware() }
    phase owned_hardware(hardware: OwnedHardware) -> never {
        let worker_slot = hardware.executors.claim(identity = SlotIdentity(label = "worker"))
        let topic = U64GapTopic(identity = TopicIdentity(label = "counter"))
        let worker = Worker(slot = worker_slot, topic = topic)
        hardware.vcpu0.enter(executor = worker)
    }
}
`)
	index := mustBuildIndex(t, modules)
	_, ds := Check(index, modules)
	if !hasMessage(ds, diag.SEM0039, "topic publisher must be created in image wiring") {
		t.Fatalf("expected SEM0039 for runtime topic publisher factory, got %#v", ds)
	}
}

func TestExecutorTopicGraphRejectsTwoProducerFieldsForOneTopic(t *testing.T) {
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
class HotPollPolicy {}
`, `
module machine.x86_64.topic_u64
use { ExecutorSlot } from machine.x86_64.cpu_state
data TopicIdentity { label: StringLiteral }
class U64GapTopic {
    identity: TopicIdentity
    id: U64
    depth: U64
    asm fn publisher(self) -> U64GapPublisher { ret }
}
class U64GapPublisher { topic: U64GapTopic }
`)
	src := `
module test.two_producers
use { ExecutorSlot, SlotIdentity, OwnedHardware, DelegatedHardware } from machine.x86_64.cpu_state
use { U64GapPublisher, U64GapTopic, TopicIdentity } from machine.x86_64.topic_u64
use { HotPollPolicy } from machine.x86_64.executor_loop
use { ExecutorMemory } from machine.x86_64.executor_memory

executor Producer {
    slot: ExecutorSlot
    loop: HotPollPolicy
    memory: ExecutorMemory
    out: U64GapPublisher
    start fn run(self) -> never { while true {} }
}

image Img {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware { return hardware.exit_to_owned_hardware() }
    phase owned_hardware(hardware: OwnedHardware) -> never {
        let slot_a = hardware.executors.claim(identity = SlotIdentity(label = "producer.a"))
        let slot_b = hardware.executors.claim(identity = SlotIdentity(label = "producer.b"))
        let topic = U64GapTopic(identity = TopicIdentity(label = "counter"), id = 0, depth = 64)
        let producer_a = Producer(slot = slot_a, loop = HotPollPolicy(), memory = ExecutorMemory(arena_base = 0, arena_length = 4096, next_offset = 0), out = topic.publisher())
        let producer_b = Producer(slot = slot_b, loop = HotPollPolicy(), memory = ExecutorMemory(arena_base = 0, arena_length = 4096, next_offset = 0), out = topic.publisher())
        hardware.vcpu1.start(executor = producer_a)
        hardware.vcpu0.enter(executor = producer_b)
    }
}`
	modules := append(contract, parseModulesForTest(t, src)...)
	index := mustBuildIndex(t, modules)
	_, ds := Check(index, modules)
	if !hasCode(ds, diag.SEM0039) {
		t.Fatalf("expected SEM0039, got %#v", ds)
	}
}

func TestExplicitInterruptConfiguratorSetsRouteVector(t *testing.T) {
	tests := []struct {
		name       string
		configure  string
		wantVector int
	}{
		{
			name:       "configured",
			configure:  "self.interrupts.initialize_for_com1_receive()",
			wantVector: 0x40,
		},
		{
			name:       "missing configurator",
			wantVector: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			base := parseUEFIModuleSet(t)
			modules := base[:0]
			for _, module := range base {
				if module.Name != "sem.uefi_test_harness" {
					modules = append(modules, module)
				}
			}
			modules = append(modules, parseModulesForTest(t, `
module sem.explicit_interrupt_configurator

use { DelegatedHardware } from platform.uefi.transition
use { BootPanic } from platform.hardware.panic
use { PlatformDiscoveryRoot } from platform.hardware.discovery
use { ClaimedPciPlanBuilder, CpuFeatureFacts, CpuPlan, HardwarePlan, InterruptRoutingPlan, IoPortAuthority, MemoryPlan, OwnedHardware, OwnedMemory, PathIdentity, SlotIdentity } from machine.x86_64.cpu_state
use { ExecutorSlot } from machine.x86_64.executor_slot
use { Bytes, MutableBytes } from machine.x86_64.executor_memory
use { HotPollPolicy } from machine.x86_64.executor_loop
use { ApicInterruptController, InterruptSourceIdentity, InterruptVector, IoApicDiscovered, IoApicRoute, LocalApic } from machine.x86_64.interrupts
use { InterruptOverflowPolicy, InterruptPayloadKind, QueueIdentity } from machine.x86_64.interrupt_queue
use { SerialConsolePath, SerialRxSubscription, SerialRxTopic, SerialWriterRegisters } from machine.x86_64.serial
use { TopicIdentity } from machine.x86_64.topic_u64
use { ArenaIdentity, ArenaPolicy } from platform.hardware.memory

executor Worker {
    slot: ExecutorSlot
    loop: HotPollPolicy
    interrupts: ApicInterruptController
    serial_rx: SerialRxSubscription

    start fn run(self) -> never {
        `+test.configure+`
        while true {}
    }
}

image ExplicitInterruptConfigurator {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        let owned_memory = OwnedMemory(arena = MutableBytes(address = 0, length = 0))
        let memory_plan = MemoryPlan(
            owned_memory = owned_memory,
            executor_arena = MutableBytes(address = 0, length = 0),
            io_ports = IoPortAuthority()
        )
	        let cpu_plan = CpuPlan(
	            owned_stack_top = 0,
	            gdt_descriptor = Bytes(address = 0, length = 0),
	            idt_descriptor = Bytes(address = 0, length = 0),
	            cr3 = 0
	        )
	        let panic = BootPanic()
	        let discovery = PlatformDiscoveryRoot(panic = panic).from_uefi(hardware = hardware)
	        let root_region = discovery.memory.require_usable_region(min_base = 0x200000, length = 0x400000, align = 4096)
	        let root = root_region.create_arena(identity = ArenaIdentity(label = "explicit.interrupt.root"), policy = ArenaPolicy(evict_cache_by_default = true))
	        let console_seed = ExecutorSlot(id = 0)
	        let worker_seed = ExecutorSlot(id = 1)
	        let console_memory = root.executor_memory(owner = console_seed, length = 0x100000, align = 4096)
	        let worker_memory = root.executor_memory(owner = worker_seed, length = 0x100000, align = 4096)
	        let shared = discovery.interrupts.route_shared_irq(irq = 6, vector = InterruptVector(value = 0x46))
	        let queue = root.interrupt_queue(identity = QueueIdentity(label = "irq.serial.rx"), owner = console_seed, capacity = 64, payload = InterruptPayloadKind(kind = 1, size = 8, align = 8), overflow = InterruptOverflowPolicy(mode = 0))
	        let hardware_plan = HardwarePlan(
	            cpus = discovery.cpus.require_min_count(count = 2),
	            interrupts = InterruptRoutingPlan(
	                local_apic = discovery.interrupts.local_apic,
	                serial_irq4 = shared.route,
	                serial_shared_irq4 = shared,
	                serial_irq_source = shared.claim_source(identity = InterruptSourceIdentity(label = "serial.rx"))
	            ),
	            pci = ClaimedPciPlanBuilder(panic = panic).empty(),
	            timer = discovery.timers.require_periodic(period_us = 1000),
	            serial_irq_queue = queue,
	            console_memory = console_memory,
	            worker_memory = worker_memory,
	            wake_strategy = discovery.cpus.wake_strategy(features = CpuFeatureFacts(monitor_mwait_available = true))
	        )
	        return hardware.exit_to_owned_hardware(memory_plan = memory_plan, cpu_plan = cpu_plan, hardware_plan = hardware_plan)
	    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        let slot = hardware.executors.claim(identity = SlotIdentity(label = "worker"))
        let topic = SerialRxTopic(identity = TopicIdentity(label = "serial.rx"), id = 0)
        let path = SerialConsolePath(
            identity = PathIdentity(label = "serial"),
            registers = SerialWriterRegisters(port_base = 0x03f8),
            route = IoApicRoute(
                io_apic = IoApicDiscovered(id = 0, address = 0, gsi_base = 0, panic = BootPanic()),
                gsi = 4,
                flags = 0,
                vector = InterruptVector(value = 0x40),
                destination_apic_id = 0
            ),
            rx = topic.publisher()
        )
        let sub = topic.subscribe(subscriber = slot)
        let worker = Worker(
            slot = slot,
            loop = HotPollPolicy(),
            interrupts = ApicInterruptController(
                local_apic = LocalApic(base = 0xFEE00000, apic_id = 0, panic = BootPanic())
            ),
            serial_rx = sub
        )
        hardware.vcpu0.enter(executor = worker)
    }
}
`)...)
			index := mustBuildIndex(t, modules)
			checked, ds := Check(index, modules)
			if len(ds) != 0 {
				t.Fatalf("Check diagnostics: %#v", ds)
			}
			if len(checked.ImageGraph.InterruptTopicRoutes) != 1 {
				t.Fatalf("interrupt routes = %#v", checked.ImageGraph.InterruptTopicRoutes)
			}
			if got := checked.ImageGraph.InterruptTopicRoutes[0].Vector; got != test.wantVector {
				t.Fatalf("route vector = %#x, want %#x", got, test.wantVector)
			}
		})
	}
}

func TestPathPublisherWithoutTopicIdentityIsRejectedFromSource(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.cpu_state
data PathIdentity { label: StringLiteral }
`, `
module machine.x86_64.serial
use { PathIdentity } from machine.x86_64.cpu_state

data SerialPathInterrupt { has_byte: Bool; byte: U8 }

class SerialRxTopic {
    id: U64
    fn publisher(self) -> SerialRxPublisher {
        return SerialRxPublisher(topic = self)
    }
}

class SerialRxPublisher {
    topic: SerialRxTopic
}

driver path SerialWriterRegisters {
    port_base: U16
}

driver path SerialConsolePath {
    identity: PathIdentity
    registers: SerialWriterRegisters
    rx: SerialRxPublisher

    interrupt receiver -> SerialPathInterrupt {
        return SerialPathInterrupt(has_byte = false, byte = 0)
    }
}
`, `
module test.path_topic_identity
use { PathIdentity } from machine.x86_64.cpu_state
use { SerialConsolePath, SerialRxTopic, SerialWriterRegisters } from machine.x86_64.serial

unique class OwnedHardware {}
unique class DelegatedHardware {
    fn exit_to_owned_hardware(self) -> OwnedHardware {
        return OwnedHardware()
    }
}

image Img {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.exit_to_owned_hardware()
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        let topic = SerialRxTopic(id = 0)
        let path = SerialConsolePath(
            identity = PathIdentity(label = "serial"),
            registers = SerialWriterRegisters(port_base = 0x03f8),
            rx = topic.publisher()
        )
        while true {}
    }
}
`)
	index := mustBuildIndex(t, modules)
	_, ds := Check(index, modules)
	if !hasMessage(ds, diag.SEM0048, "publishing topic path is missing identity") {
		t.Fatalf("expected SEM0048 for path publisher topic identity, got %#v", ds)
	}
}

func TestInterruptRouteVectorsComeFromExplicitConfigurators(t *testing.T) {
	c := &checker{graph: ImageGraph{
		InterruptTopicRoutes: []InterruptTopicRouteNode{
			{TopicKind: "serial_rx", TopicLabel: "serial.rx"},
			{TopicKind: "edu_interrupt", TopicLabel: "edu.irq"},
			{TopicKind: "ivshmem_doorbell", TopicLabel: "ivshmem.doorbell"},
		},
		InterruptConfigurators: []InterruptConfiguratorNode{
			{TopicKind: "edu_interrupt", Vector: 0x41},
			{TopicKind: "ivshmem_doorbell", Vector: 0x42},
		},
	}}
	c.finalizeInterruptTopicRoutes()

	want := map[string]int{
		"serial_rx":        0,
		"edu_interrupt":    0x41,
		"ivshmem_doorbell": 0x42,
	}
	for _, route := range c.graph.InterruptTopicRoutes {
		if got := route.Vector; got != want[route.TopicKind] {
			t.Fatalf("%s vector = %#x, want %#x", route.TopicKind, got, want[route.TopicKind])
		}
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
			name: "duplicate label across graph kinds",
			code: diag.SEM0033,
			graph: ImageGraph{
				ExecutorSlots: []ExecutorSlotNode{{Label: "worker", Binding: "slot", Span: testSpan(1)}},
				Topics:        []TopicNode{{Label: "worker", Binding: "topic", Span: testSpan(2)}},
			},
		},
		{
			name: "duplicate label after symbol sanitization",
			code: diag.SEM0033,
			graph: ImageGraph{Topics: []TopicNode{
				{Label: "counter.rx", Binding: "a", Span: testSpan(1)},
				{Label: "counter_rx", Binding: "b", Span: testSpan(2)},
			}},
		},
		{
			name: "executor constructed with unclaimed slot",
			code: diag.SEM0035,
			graph: ImageGraph{Executors: []ExecutorNode{{
				Type:       workerWithSlotType(),
				Span:       testSpan(2),
				FieldSpans: map[string]source.Span{"slot": testSpan(3)},
			}}},
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
			name: "path interrupt publisher shares executor producer topic",
			code: diag.SEM0039,
			graph: ImageGraph{
				Executors: []ExecutorNode{{
					Type:          workerType,
					FieldBindings: map[string]string{"out": "pub"},
					BoundTypes:    map[string]*Type{"out": topicPublisherType()},
					FieldSpans:    map[string]source.Span{"out": testSpan(10)},
				}},
				TopicPublishers:      []TopicPublisherNode{{TopicLabel: "counter", Binding: "pub", Span: testSpan(11)}},
				InterruptTopicRoutes: []InterruptTopicRouteNode{{PathBinding: "path", TopicLabel: "counter", Span: testSpan(12)}},
			},
		},
		{
			name:  "slot not bound to one executor",
			code:  diag.SEM0034,
			graph: ImageGraph{ExecutorSlots: []ExecutorSlotNode{{Label: "worker", Binding: "slot", Span: testSpan(13)}}},
		},
		{
			name: "current vcpu must enter",
			code: diag.SEM0036,
			graph: ImageGraph{VcpuPlacements: []VcpuPlacementNode{
				{VcpuID: 0, ExecutorBinding: "worker", SlotLabel: "worker", Span: testSpan(14)},
			}},
		},
		{
			name: "non-current vcpu must start",
			code: diag.SEM0036,
			graph: ImageGraph{VcpuPlacements: []VcpuPlacementNode{
				{VcpuID: 1, ExecutorBinding: "worker", SlotLabel: "worker", Terminal: true, Span: testSpan(14)},
			}},
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
			graph: ImageGraph{SubscriptionUses: []SubscriptionUseNode{{TopicLabel: "counter", FieldName: "input", CurrentExecutorType: "Other", Span: testSpan(16)}}},
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
			name: "executor memory owner mismatch",
			code: diag.SEM0047,
			graph: ImageGraph{Executors: []ExecutorNode{{
				Type:             workerWithMemoryType(),
				SlotLabel:        "worker",
				MemoryOwnerLabel: "other",
				FieldSpans:       map[string]source.Span{"memory": testSpan(21)},
				Span:             testSpan(21),
			}}},
		},
		{
			name:  "publishing path missing identity",
			code:  diag.SEM0048,
			graph: ImageGraph{Paths: []PathNode{{Binding: "path", PublishesInterrupts: true, Span: testSpan(22)}}},
		},
		{
			name: "publishing path missing topic identity",
			code: diag.SEM0048,
			graph: ImageGraph{
				Paths: []PathNode{{Label: "serial", Binding: "path", PublishesInterrupts: true, Span: testSpan(23)}},
			},
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

func workerWithSlotType() *Type {
	return &Type{
		Name: "Worker",
		Kind: KindExecutor,
		Fields: []Field{{
			Name: "slot",
			Type: &Type{Module: "machine.x86_64.cpu_state", Name: "ExecutorSlot", Kind: KindData},
		}},
	}
}

func workerWithMemoryType() *Type {
	return &Type{
		Name: "Worker",
		Kind: KindExecutor,
		Fields: []Field{{
			Name: "memory",
			Type: &Type{Module: "machine.x86_64.executor_memory", Name: "ExecutorMemory", Kind: KindClass},
		}},
	}
}
