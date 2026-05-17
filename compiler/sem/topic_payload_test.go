package sem

import (
	"testing"
)

const timerTickTopicSource = `
module examples.timer_tick_topic
use { BootPanic } from platform.hardware.panic
use { PlatformDiscoveryRoot } from platform.hardware.discovery
use { DelegatedHardware } from platform.uefi.transition
use { OwnedHardware, OwnedMemory, IoPortAuthority, MemoryPlan, CpuPlan, HardwarePlan, InterruptRoutingPlan, ClaimedPciPlanBuilder } from machine.x86_64.cpu_state
use { MutableBytes, Bytes } from machine.x86_64.executor_memory
use { InterruptVector } from machine.x86_64.interrupts
use { TimerTickTopic } from machine.x86_64.topic_payload
use { TopicIdentity } from machine.x86_64.topic_u64
image TimerTopicImage {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        let panic = BootPanic()
        let discovery = PlatformDiscoveryRoot(panic = panic).from_uefi(hardware = hardware)
        let topic = TimerTickTopic(identity = TopicIdentity(label = "timer.periodic"), id = 3, depth = 64)
        let arena = MutableBytes(address = 0, length = 0)
        return hardware.exit_to_owned_hardware(memory_plan = MemoryPlan(owned_memory = OwnedMemory(arena = arena), executor_arena = arena, io_ports = IoPortAuthority()), cpu_plan = CpuPlan(owned_stack_top = 0, gdt_descriptor = Bytes(address = 0, length = 0), idt_descriptor = Bytes(address = 0, length = 0), cr3 = 0), hardware_plan = HardwarePlan(cpus = discovery.acpi.require_madt().enabled_cpus().require_count(count = 2), interrupts = InterruptRoutingPlan(local_apic = discovery.interrupts.local_apic, serial_irq4 = discovery.interrupts.route_isa_irq(irq = 4, vector = InterruptVector(value = 0x40))), pci = ClaimedPciPlanBuilder(panic = panic).empty()))
    }
    phase owned_hardware(hardware: OwnedHardware) -> never { while true {} }
}
`

func TestTimerTickTopicPayloadLayoutRecorded(t *testing.T) {
	topicName := "timer.periodic"
	checked, ds := checkUEFIModulesWithExtraSource(t, "timer-topic.wrela", timerTickTopicSource)
	if len(ds) != 0 {
		t.Fatalf("diagnostics: %#v", ds)
	}
	topic := checked.ImageGraph.TopicByLabel(topicName)
	if topic.PayloadType != "machine.x86_64.topic_payload.TimerTickPayload" {
		t.Fatalf("payload type = %q", topic.PayloadType)
	}
	if topic.PayloadSize == 0 || topic.PayloadAlign == 0 {
		t.Fatalf("payload layout not recorded: %#v", topic)
	}
}
