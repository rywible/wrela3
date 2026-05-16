package ir

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/parse"
	"github.com/ryanwible/wrela3/compiler/sem"
	"github.com/ryanwible/wrela3/compiler/source"
)

func TestTopicOpsDefineExpectedValues(t *testing.T) {
	publish := TopicPublish{TopicLabel: "counter", Kind: "gap_u64", Value: ConstInt{Value: 1}}
	tryNext := TopicTryNext{TopicLabel: "counter", Subscription: Local{Symbol: "sub"}, Type: Type{Name: "U64TopicNext"}}
	arm := TopicArmWait{TopicLabel: "counter", Subscription: Local{Symbol: "sub"}}
	reliable := ReliableTopicTryPublish{TopicLabel: "commands", Value: ConstInt{Value: 7}, Type: Type{Name: "U64PublishResult"}}
	ops := []Operation{publish, tryNext, arm, reliable}
	for _, op := range ops {
		if op == nil {
			t.Fatal("nil op")
		}
	}
	if len(valuesDefinedBy(tryNext)) != 1 {
		t.Fatal("TopicTryNext must define a value")
	}
	if len(valuesDefinedBy(reliable)) != 1 {
		t.Fatal("ReliableTopicTryPublish must define a value")
	}
}

func TestLowerTopicCallsToIntrinsicOps(t *testing.T) {
	checked := checkedProgramFromSourcesForTest(t, topicContractForTest(), `
module test.topic_lower
use { DelegatedHardware, ExecutorSlot, OwnedHardware, SlotIdentity } from machine.x86_64.cpu_state
use { EventSleepPolicy } from machine.x86_64.executor_loop
use { TopicIdentity, U64GapSubscription, U64GapTopic, U64ReliablePublisher, U64ReliableSubscription, U64ReliableTopic } from machine.x86_64.topic_u64

executor Worker {
    slot: ExecutorSlot
    loop: EventSleepPolicy
    input: U64GapSubscription
    reliable_input: U64ReliableSubscription
    reliable_output: U64ReliablePublisher
    start fn run(self) -> never {
        let next = self.input.try_next()
        self.input.arm_wait()
        let armed = self.input.is_wait_armed()
        self.reliable_output.publish_or_wait(value = 42)
        if self.input.is_wait_armed() {
            if self.reliable_input.is_wait_armed() {
                self.loop.wait()
            }
        }
        self.loop.wait()
        while true {}
    }
}

image Img {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware { return hardware.exit_to_owned_hardware() }
    phase owned_hardware(hardware: OwnedHardware) -> never {
        let worker_slot = hardware.executors.claim(identity = SlotIdentity(label = "worker"))
        let counter = U64GapTopic(identity = TopicIdentity(label = "counter"), id = 0, depth = 64)
        let input = counter.subscribe(subscriber = worker_slot)
        let commands = U64ReliableTopic(identity = TopicIdentity(label = "commands"), id = 1, depth = 64)
        let command_pub = commands.publisher()
        let reliable_input = commands.subscribe(subscriber = worker_slot)
        let results = U64ReliableTopic(identity = TopicIdentity(label = "results"), id = 2, depth = 64)
        let worker = Worker(slot = worker_slot, loop = EventSleepPolicy(), input = input, reliable_input = reliable_input, reliable_output = results.publisher())
        counter.publisher().publish(value = 1)
        let result = command_pub.try_publish(value = 2)
        command_pub.wait_for_subscriber_advance()
        hardware.vcpu0.enter(executor = worker)
    }
}`)
	program, diags := Lower(checked)
	if len(diags) != 0 {
		t.Fatalf("Lower diagnostics: %#v", diags)
	}

	worker := findFunction(program, "_wrela_method_test_topic_lower_Worker_run")
	if worker == nil {
		t.Fatal("missing lowered worker")
	}
	tryNext, ok := functionOp[TopicTryNext](*worker)
	if !ok || tryNext.TopicLabel != "counter" || tryNext.SubscriberSlot != "worker" {
		t.Fatalf("worker missing TopicTryNext: %#v", worker.Blocks)
	}
	armWait, ok := functionOp[TopicArmWait](*worker)
	if !ok || armWait.TopicLabel != "counter" || armWait.SubscriberSlot != "worker" {
		t.Fatalf("worker missing TopicArmWait: %#v", worker.Blocks)
	}
	isArmed, ok := functionOp[TopicIsWaitArmed](*worker)
	if !ok || isArmed.TopicLabel != "counter" || isArmed.SubscriberSlot != "worker" {
		t.Fatalf("worker missing TopicIsWaitArmed: %#v", worker.Blocks)
	}
	topicWait, ok := functionOp[TopicWait](*worker)
	if !ok || topicWait.SlotLabel != "worker" || topicWait.Policy != "EventSleepPolicy" {
		t.Fatalf("worker missing TopicWait: %#v", worker.Blocks)
	}
	waitIfArmed, ok := functionOp[*TopicWaitIfArmed](*worker)
	if !ok || waitIfArmed.TopicLabel != "counter" || waitIfArmed.SubscriberSlot != "worker" {
		t.Fatalf("worker missing TopicWaitIfArmed: %#v", worker.Blocks)
	}
	if len(waitIfArmed.Guards) != 2 || waitIfArmed.Guards[0].TopicLabel != "counter" || waitIfArmed.Guards[1].TopicLabel != "commands" {
		t.Fatalf("TopicWaitIfArmed guards = %#v, want counter and commands", waitIfArmed.Guards)
	}
	workerTryPublish, ok := functionOp[*ReliableTopicTryPublish](*worker)
	if !ok || workerTryPublish.TopicLabel != "results" {
		t.Fatalf("worker missing lowered publish_or_wait try-publish: %#v", worker.Blocks)
	}
	workerWaitAdvance, ok := functionOp[*ReliableTopicWaitForAdvance](*worker)
	if !ok || workerWaitAdvance.TopicLabel != "results" || workerWaitAdvance.PublisherSlot != "worker" {
		t.Fatalf("worker missing lowered publish_or_wait wait: %#v", worker.Blocks)
	}
	if call, ok := functionOp[*Call](*worker); ok && strings.Contains(call.Symbol, "publish_or_wait") {
		t.Fatalf("worker kept publish_or_wait as ordinary call: %#v", call)
	}

	owned := findFunction(program, "_wrela_phase_test_topic_lower_Img_owned_hardware")
	if owned == nil {
		t.Fatal("missing owned hardware phase")
	}
	publish, ok := functionOp[TopicPublish](*owned)
	if !ok || publish.TopicLabel != "counter" || publish.Kind != "gap_u64" {
		t.Fatalf("owned phase missing TopicPublish: %#v", owned.Blocks)
	}
	tryPublish, ok := functionOp[ReliableTopicTryPublish](*owned)
	if !ok || tryPublish.TopicLabel != "commands" {
		t.Fatalf("owned phase missing ReliableTopicTryPublish: %#v", owned.Blocks)
	}
	waitAdvance, ok := functionOp[ReliableTopicWaitForAdvance](*owned)
	if !ok || waitAdvance.TopicLabel != "commands" {
		t.Fatalf("owned phase missing ReliableTopicWaitForAdvance: %#v", owned.Blocks)
	}
	if len(program.Topics) != 3 {
		t.Fatalf("program topics = %#v, want three topic layouts", program.Topics)
	}
	for _, topic := range program.Topics {
		if topic.Depth != 64 {
			t.Fatalf("topic %q depth = %d, want 64", topic.Label, topic.Depth)
		}
	}
}

func TestLowerSpecializesTopicOpsForSameExecutorTypePlacements(t *testing.T) {
	checked := checkedProgramFromSourcesForTest(t, topicContractForTest(), `
module test.same_type_topic_lower
use { DelegatedHardware, ExecutorSlot, OwnedHardware, SlotIdentity } from machine.x86_64.cpu_state
use { EventSleepPolicy } from machine.x86_64.executor_loop
use { TopicIdentity, U64GapSubscription, U64GapTopic } from machine.x86_64.topic_u64

executor Worker {
    slot: ExecutorSlot
    loop: EventSleepPolicy
    input: U64GapSubscription
    start fn run(self) -> never {
        let next = self.input.try_next()
        if next.has_message {
        }
        while true {}
    }
}

image Img {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware { return hardware.exit_to_owned_hardware() }
    phase owned_hardware(hardware: OwnedHardware) -> never {
        let slot_a = hardware.executors.claim(identity = SlotIdentity(label = "worker.a"))
        let slot_b = hardware.executors.claim(identity = SlotIdentity(label = "worker.b"))
        let topic_a = U64GapTopic(identity = TopicIdentity(label = "topic.a"), id = 0, depth = 64)
        let topic_b = U64GapTopic(identity = TopicIdentity(label = "topic.b"), id = 1, depth = 64)
        let input_a = topic_a.subscribe(subscriber = slot_a)
        let input_b = topic_b.subscribe(subscriber = slot_b)
        let worker_a = Worker(slot = slot_a, loop = EventSleepPolicy(), input = input_a)
        let worker_b = Worker(slot = slot_b, loop = EventSleepPolicy(), input = input_b)
        hardware.vcpu1.start(executor = worker_a)
        hardware.vcpu0.enter(executor = worker_b)
    }
}`)
	program, diags := Lower(checked)
	if len(diags) != 0 {
		t.Fatalf("Lower diagnostics: %#v", diags)
	}

	got := map[string]string{}
	for _, fn := range program.Functions {
		if !strings.HasPrefix(fn.Symbol, "_wrela_method_test_same_type_topic_lower_Worker_run") {
			continue
		}
		next, ok := functionOp[TopicTryNext](fn)
		if !ok {
			continue
		}
		got[next.SubscriberSlot] = next.TopicLabel
	}
	want := map[string]string{"worker.a": "topic.a", "worker.b": "topic.b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("specialized worker topic ops = %#v, want %#v", got, want)
	}
}

func TestLowerSerialRxNextMessageFieldUsesIRHandleOffset(t *testing.T) {
	checked := checkedProgramFromSourcesForTest(t, `
module machine.x86_64.serial
data SerialPathInterrupt {
    has_byte: Bool
    byte: U8
}

data SerialRxNext {
    has_message: Bool
    message: SerialPathInterrupt
}

module test.serial_next
use { SerialRxNext } from machine.x86_64.serial

executor Worker {
    start fn run(self, next: SerialRxNext) -> never {
        if next.has_message {
            let byte = next.message.byte
        }
        while true {}
    }
}
`)
	program, diags := Lower(checked)
	if len(diags) != 0 {
		t.Fatalf("Lower diagnostics: %#v", diags)
	}

	nextInfo := program.Types["machine.x86_64.serial.SerialRxNext"]
	messageOffset := nextInfo.Fields["message"].Offset
	if messageOffset != 8 {
		t.Fatalf("SerialRxNext.message IR offset = %d, want 8", messageOffset)
	}

	worker := findFunction(program, "_wrela_method_test_serial_next_Worker_run")
	if worker == nil {
		t.Fatalf("missing worker run function")
	}
	messageLoad := fieldLoadByName(worker.Blocks[0].Ops, "message")
	if messageLoad == nil {
		t.Fatalf("worker missing next.message FieldLoad: %#v", worker.Blocks)
	}
	if messageLoad.Offset != messageOffset {
		t.Fatalf("next.message FieldLoad offset = %d, want IR offset %d", messageLoad.Offset, messageOffset)
	}
}

func fieldLoadByName(ops []Operation, name string) *FieldLoad {
	for _, op := range ops {
		if load, ok := op.(*FieldLoad); ok && load.Field == name {
			return load
		}
		switch nested := op.(type) {
		case *If:
			if load := fieldLoadByName(nested.ConditionOps, name); load != nil {
				return load
			}
			if load := fieldLoadByName(nested.Then, name); load != nil {
				return load
			}
			if load := fieldLoadByName(nested.Else, name); load != nil {
				return load
			}
		case *While:
			if load := fieldLoadByName(nested.ConditionOps, name); load != nil {
				return load
			}
			if load := fieldLoadByName(nested.Body, name); load != nil {
				return load
			}
		}
	}
	return nil
}

func checkedProgramFromSourcesForTest(t *testing.T, sources ...string) *sem.CheckedProgram {
	t.Helper()
	files := make([]*source.File, 0, len(sources))
	for _, sourceText := range sources {
		for _, singleModule := range splitModulesForTest(sourceText) {
			files = append(files, source.NewFile(source.FileID(len(files)+1), "topic_lower_test.wrela", singleModule))
		}
	}
	modules, ds := parse.ParseGraph(source.Graph{Files: files})
	if len(ds) != 0 {
		t.Fatalf("parse diagnostics: %#v", ds)
	}
	index, ds := sem.BuildIndex(modules)
	ds = filterMissingImageDiagnosticForTopicTest(ds)
	if len(ds) != 0 {
		t.Fatalf("index diagnostics: %#v", ds)
	}
	checked, ds := sem.Check(index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}
	return checked
}

func splitModulesForTest(sourceText string) []string {
	parts := strings.Split(sourceText, "\nmodule ")
	out := make([]string, 0, len(parts))
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if i != 0 {
			part = "module " + part
		}
		out = append(out, part)
	}
	return out
}

func filterMissingImageDiagnosticForTopicTest(ds []diag.Diagnostic) []diag.Diagnostic {
	out := ds[:0]
	for _, d := range ds {
		if d.Code == diag.SEM0004 {
			continue
		}
		out = append(out, d)
	}
	return out
}

func functionHasOp[T any](fn Function) bool {
	_, ok := functionOp[T](fn)
	return ok
}

func functionOp[T any](fn Function) (T, bool) {
	for _, block := range fn.Blocks {
		if op, ok := opFromOps[T](block.Ops); ok {
			return op, true
		}
	}
	var zero T
	return zero, false
}

func opsHaveOp[T any](ops []Operation) bool {
	_, ok := opFromOps[T](ops)
	return ok
}

func opFromOps[T any](ops []Operation) (T, bool) {
	for _, op := range ops {
		if typed, ok := any(op).(T); ok {
			return typed, true
		}
		switch nested := op.(type) {
		case *While:
			if typed, ok := opFromOps[T](nested.ConditionOps); ok {
				return typed, true
			}
			if typed, ok := opFromOps[T](nested.Body); ok {
				return typed, true
			}
		case *If:
			if typed, ok := opFromOps[T](nested.ConditionOps); ok {
				return typed, true
			}
			if typed, ok := opFromOps[T](nested.Then); ok {
				return typed, true
			}
			if typed, ok := opFromOps[T](nested.Else); ok {
				return typed, true
			}
		case *ForBytes:
			if typed, ok := opFromOps[T](nested.IterableOps); ok {
				return typed, true
			}
			if typed, ok := opFromOps[T](nested.Body); ok {
				return typed, true
			}
		}
	}
	var zero T
	return zero, false
}

func topicContractForTest() string {
	return `
module machine.x86_64.cpu_state
data SlotIdentity { label: StringLiteral }
data ExecutorSlot { id: U64 }
data VcpuStartStatus { started: Bool; id: U64 }
class ExecutorRegistry { fn claim(self, identity: SlotIdentity) -> ExecutorSlot { return ExecutorSlot(id = 0) } }
class Vcpu { id: U64 }
class OwnedHardware { executors: ExecutorRegistry; vcpu0: Vcpu; vcpu1: Vcpu }
unique class DelegatedHardware { asm fn exit_to_owned_hardware(self) -> OwnedHardware { ret } }

module machine.x86_64.executor_memory
class ExecutorMemory { arena_base: PhysicalAddress; arena_length: U64; next_offset: U64 }

module machine.x86_64.executor_loop
class EventSleepPolicy { asm fn wait(self) { hlt; ret } }

module machine.x86_64.topic_u64
use { ExecutorSlot } from machine.x86_64.cpu_state
data TopicIdentity { label: StringLiteral }
data U64TopicMessage { sequence: U64; value: U64 }
data U64TopicNext { has_message: Bool; gap: Bool; missed: U64; message: U64TopicMessage }
data U64PublishResult { published: Bool; full: Bool }
class U64GapTopic {
    identity: TopicIdentity
    id: U64
    depth: U64
    asm fn publisher(self) -> U64GapPublisher { ret }
    asm fn subscribe(self, subscriber: ExecutorSlot) -> U64GapSubscription { ret }
}
class U64GapPublisher {
    topic: U64GapTopic
    fn publish(self, value: U64) { self.publish_intrinsic(value = value) }
    asm fn publish_intrinsic(self, value: U64) { ret }
}
class U64GapSubscription {
    topic: U64GapTopic
    subscriber: ExecutorSlot
    cursor: U64
    armed: Bool
    asm fn try_next(self) -> U64TopicNext { ret }
    fn arm_wait(self) { self.armed = true }
    fn is_wait_armed(self) -> Bool { return self.armed }
}
class U64ReliableTopic {
    identity: TopicIdentity
    id: U64
    depth: U64
    asm fn publisher(self) -> U64ReliablePublisher { ret }
    asm fn subscribe(self, subscriber: ExecutorSlot) -> U64ReliableSubscription { ret }
}
class U64ReliablePublisher {
    topic: U64ReliableTopic
    asm fn try_publish(self, value: U64) -> U64PublishResult { ret }
    fn publish_or_wait(self, value: U64) {
        let result = self.try_publish(value = value)
        while result.full {
            self.wait_for_subscriber_advance()
            result = self.try_publish(value = value)
        }
    }
    asm fn wait_for_subscriber_advance(self) { hlt; ret }
}
class U64ReliableSubscription {
    topic: U64ReliableTopic
    subscriber: ExecutorSlot
    cursor: U64
    armed: Bool
    asm fn try_next(self) -> U64TopicNext { ret }
    fn arm_wait(self) { self.armed = true }
    fn is_wait_armed(self) -> Bool { return self.armed }
}
`
}
