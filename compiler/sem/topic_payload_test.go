package sem

import (
	"testing"
)

const timerTopicSource = `
module examples.timer_tick_topic
use { BootPanic } from platform.hardware.panic
use { PlatformDiscoveryRoot } from platform.hardware.discovery
use { DelegatedHardware } from platform.uefi.transition
use { CpuFeatureFacts, OwnedHardware, OwnedMemory, IoPortAuthority, MemoryPlan, CpuPlan, HardwarePlan, InterruptRoutingPlan, ClaimedPciPlanBuilder } from machine.x86_64.cpu_state
use { ExecutorSlot } from machine.x86_64.executor_slot
use { MutableBytes, Bytes } from machine.x86_64.executor_memory
use { InterruptSourceIdentity, InterruptVector } from machine.x86_64.interrupts
use { InterruptOverflowPolicy, InterruptQueue, QueueIdentity } from machine.x86_64.interrupt_queue
use { ArenaIdentity, ArenaPolicy } from platform.hardware.memory
use { Topic } from machine.x86_64.topic
use { SerialPathInterrupt, TimerTickPayload } from machine.x86_64.topic_payload
use { TopicIdentity } from machine.x86_64.topic_u64
image TimerTopicImage {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        let panic = BootPanic()
        let discovery = PlatformDiscoveryRoot(panic = panic).from_uefi(hardware = hardware)
        let topic = Topic<TimerTickPayload>(identity = TopicIdentity(label = "timer.periodic"), id = 3, depth = 64)
        let root_region = discovery.memory.require_usable_region(min_base = 0x200000, length = 0x400000, align = 4096)
        let root = root_region.create_arena(identity = ArenaIdentity(label = "timer.topic.root"), policy = ArenaPolicy(evict_cache_by_default = true))
        let console_seed = ExecutorSlot(id = 0)
        let worker_seed = ExecutorSlot(id = 1)
        let console_memory = root.executor_memory(owner = console_seed, length = 0x100000, align = 4096)
        let worker_memory = root.executor_memory(owner = worker_seed, length = 0x100000, align = 4096)
        let shared = discovery.interrupts.route_shared_irq(irq = 4, vector = InterruptVector(value = 0x40))
        let queue_slots = console_memory.reserve_array(SerialPathInterrupt, count = 64)
        let queue = InterruptQueue<SerialPathInterrupt>(identity = QueueIdentity(label = "irq.serial.rx"), owner = console_seed, slots = queue_slots, capacity = 64, overflow = InterruptOverflowPolicy(mode = 0), head = 0, tail = 0, overflowed = false)
        let arena = MutableBytes(address = 0, length = 0)
        return hardware.exit_to_owned_hardware(memory_plan = MemoryPlan(owned_memory = OwnedMemory(arena = arena), executor_arena = arena, io_ports = IoPortAuthority()), cpu_plan = CpuPlan(owned_stack_top = 0, gdt_descriptor = Bytes(address = 0, length = 0), idt_descriptor = Bytes(address = 0, length = 0), cr3 = 0), hardware_plan = HardwarePlan(cpus = discovery.acpi.require_madt().enabled_cpus().require_count(count = 2), interrupts = InterruptRoutingPlan(local_apic = discovery.interrupts.local_apic, serial_irq4 = shared.route, serial_shared_irq4 = shared, serial_irq_source = shared.claim_source(identity = InterruptSourceIdentity(label = "serial.rx"))), pci = ClaimedPciPlanBuilder(panic = panic).empty(), timer = discovery.timers.require_periodic(period_us = 1000), serial_irq_queue = queue, console_memory = console_memory, worker_memory = worker_memory, wake_strategy = discovery.cpus.wake_strategy(features = CpuFeatureFacts(monitor_mwait_available = true))))
    }
    phase owned_hardware(hardware: OwnedHardware) -> never { while true {} }
}
`

func TestTimerPayloadLayoutRecorded(t *testing.T) {
	topicName := "timer.periodic"
	checked, ds := checkUEFIModulesWithExtraSource(t, "timer-topic.wrela", timerTopicSource)
	if len(ds) != 0 {
		t.Fatalf("diagnostics: %#v", ds)
	}
	topic := checked.ImageGraph.TopicByLabel(topicName)
	if topic.PayloadType != "machine.x86_64.topic_payload.TimerTickPayload" {
		t.Fatalf("payload type = %q", topic.PayloadType)
	}
	if topic.PayloadSize != 24 || topic.PayloadAlign != 8 {
		t.Fatalf("payload layout = size %d align %d, want size 24 align 8: %#v", topic.PayloadSize, topic.PayloadAlign, topic)
	}
	payload := moduleType(t, checked.Index, "machine.x86_64.topic_payload", "TimerTickPayload")
	if len(payload.Fields) != 3 ||
		payload.Fields[0].Name != "sequence" ||
		payload.Fields[1].Name != "monotonic_us" ||
		payload.Fields[2].Name != "source_id" {
		t.Fatalf("TimerTickPayload field order drifted: %#v", payload.Fields)
	}
}

func TestGenericTopicPayloadLayoutRecorded(t *testing.T) {
	modules := parseUEFIModuleSet(t)
	index := mustBuildIndex(t, modules)
	payload := moduleType(t, index, "machine.x86_64.topic_payload", "TimerTickPayload")
	topic := index.instantiateByName("machine.x86_64.topic", "Topic", []*Type{payload})
	gotPayload, kind, ok := TopicPayloadTypeForTopic(topic)
	if !ok {
		t.Fatal("generic topic payload was not recognized")
	}
	if gotPayload.Key() != payload.Key() || kind != "timer_tick" {
		t.Fatalf("payload/kind = %s/%s, want %s/timer_tick", gotPayload.Key(), kind, payload.Key())
	}
}
