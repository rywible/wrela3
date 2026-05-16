package ir

import "testing"

func TestVcpuDispatchOpsDefineShape(t *testing.T) {
	start := VcpuStart{VcpuID: 1, Executor: Local{Symbol: "worker"}, SlotLabel: "worker", Type: Type{Name: "VcpuStartStatus"}}
	enter := VcpuEnter{VcpuID: 0, Executor: Local{Symbol: "main"}, SlotLabel: "main"}
	if start.VcpuID != 1 || enter.VcpuID != 0 {
		t.Fatalf("bad vcpu ids")
	}
	if _, ok := any(start).(Operation); !ok {
		t.Fatal("VcpuStart must be operation")
	}
	if len(valuesDefinedBy(start)) != 1 {
		t.Fatal("VcpuStart must define VcpuStartStatus")
	}
	if _, ok := any(enter).(Operation); !ok {
		t.Fatal("VcpuEnter must be operation")
	}
}

func TestLowerVcpuStartAndEnterToIntrinsicOps(t *testing.T) {
	checked := checkedProgramFromSourcesForTest(t, topicContractForTest(), `
module test.vcpu_lower
use { DelegatedHardware, ExecutorSlot, OwnedHardware, SlotIdentity } from machine.x86_64.cpu_state
use { EventSleepPolicy } from machine.x86_64.executor_loop
use { TopicIdentity, U64GapSubscription, U64GapTopic, U64ReliableSubscription, U64ReliableTopic } from machine.x86_64.topic_u64

executor Worker {
    slot: ExecutorSlot
    loop: EventSleepPolicy
    input: U64GapSubscription
    reliable_input: U64ReliableSubscription
    start fn run(self) -> never { while true {} }
}

image Img {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware { return hardware.exit_to_owned_hardware() }
    phase owned_hardware(hardware: OwnedHardware) -> never {
        let worker_slot = hardware.executors.claim(identity = SlotIdentity(label = "worker"))
        let hello_slot = hardware.executors.claim(identity = SlotIdentity(label = "hello"))
        let counter = U64GapTopic(identity = TopicIdentity(label = "counter"), id = 0, depth = 64)
        let commands = U64ReliableTopic(identity = TopicIdentity(label = "commands"), id = 1, depth = 64)
        let worker_input = counter.subscribe(subscriber = worker_slot)
        let worker_reliable_input = commands.subscribe(subscriber = worker_slot)
        let hello_input = counter.subscribe(subscriber = hello_slot)
        let hello_reliable_input = commands.subscribe(subscriber = hello_slot)
        let worker = Worker(slot = worker_slot, loop = EventSleepPolicy(), input = worker_input, reliable_input = worker_reliable_input)
        let hello = Worker(slot = hello_slot, loop = EventSleepPolicy(), input = hello_input, reliable_input = hello_reliable_input)
        hardware.vcpu1.start(executor = worker)
        hardware.vcpu0.enter(executor = hello)
    }
}`)
	program, diags := Lower(checked)
	if len(diags) != 0 {
		t.Fatalf("Lower diagnostics: %#v", diags)
	}
	owned := findFunction(program, "_wrela_phase_test_vcpu_lower_Img_owned_hardware")
	if owned == nil {
		t.Fatal("missing owned hardware phase")
	}
	start, ok := functionOp[VcpuStart](*owned)
	if !ok || start.VcpuID != 1 || start.SlotLabel != "worker" {
		t.Fatalf("owned phase missing VcpuStart: %#v", owned.Blocks)
	}
	enter, ok := functionOp[VcpuEnter](*owned)
	if !ok || enter.VcpuID != 0 || enter.SlotLabel != "hello" {
		t.Fatalf("owned phase missing VcpuEnter: %#v", owned.Blocks)
	}
	if len(program.VcpuStarts) != 2 {
		t.Fatalf("program vcpu starts = %#v, want two plans", program.VcpuStarts)
	}
}
