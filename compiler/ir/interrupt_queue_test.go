package ir

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
)

const interruptQueueBoundsSource = `
module platform.test_interrupt_queue_bounds
use { BootPanic } from platform.hardware.panic
use { PlatformDiscoveryRoot } from platform.hardware.discovery
use { DelegatedHardware } from platform.uefi.transition
use { ArenaIdentity, ArenaPolicy, PhysicalRegionAuthority } from platform.hardware.memory
use { CpuFeatureFacts, OwnedHardware, OwnedMemory, IoPortAuthority, MemoryPlan, CpuPlan, HardwarePlan, InterruptRoutingPlan, ClaimedPciPlanBuilder } from machine.x86_64.cpu_state
use { ExecutorSlot } from machine.x86_64.executor_slot
use { MutableBytes, Bytes } from machine.x86_64.executor_memory
use { InterruptSourceIdentity, InterruptVector } from machine.x86_64.interrupts
use { InterruptOverflowPolicy, InterruptPayloadKind, QueueIdentity, InterruptQueue } from machine.x86_64.interrupt_queue
use { SerialPathInterrupt } from machine.x86_64.serial
image QueueBounds {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        let panic = BootPanic()
        let discovery = PlatformDiscoveryRoot(panic = panic).from_uefi(hardware = hardware)
        let root_region = PhysicalRegionAuthority(base = 0x200000, length = 0x1000000, align = 4096, provenance = 1, panic = panic)
        let root = root_region.create_arena(identity = ArenaIdentity(label = "root"), policy = ArenaPolicy(evict_cache_by_default = true))
        let console_memory = root.executor_memory(owner = ExecutorSlot(id = 0), length = 0x100000, align = 4096)
        let queue_slots = console_memory.reserve_array(SerialPathInterrupt, count = 64)
        let q = InterruptQueue<SerialPathInterrupt>(identity = QueueIdentity(label = "irq.serial.rx"), owner = ExecutorSlot(id = 0), slots = queue_slots, capacity = 64, payload = InterruptPayloadKind(kind = 1, size = sizeof(SerialPathInterrupt), align = alignof(SerialPathInterrupt)), overflow = InterruptOverflowPolicy(mode = 0), head = 0, tail = 0, overflowed = false)
        let worker_memory = root.executor_memory(owner = ExecutorSlot(id = 1), length = 0x100000, align = 4096)
        let shared = discovery.interrupts.route_shared_irq(irq = 4, vector = InterruptVector(value = 0x40))
        let arena = MutableBytes(address = 0, length = 0)
        return hardware.exit_to_owned_hardware(memory_plan = MemoryPlan(owned_memory = OwnedMemory(arena = arena), executor_arena = arena, io_ports = IoPortAuthority()), cpu_plan = CpuPlan(owned_stack_top = 0, gdt_descriptor = Bytes(address = 0, length = 0), idt_descriptor = Bytes(address = 0, length = 0), cr3 = 0), hardware_plan = HardwarePlan(cpus = discovery.cpus.require_min_count(count = 2), interrupts = InterruptRoutingPlan(local_apic = discovery.interrupts.local_apic, serial_irq4 = shared.route, serial_shared_irq4 = shared, serial_irq_source = shared.claim_source(identity = InterruptSourceIdentity(label = "serial.rx"))), pci = ClaimedPciPlanBuilder(panic = panic).empty(), timer = discovery.timers.require_periodic(period_us = 1000), serial_irq_queue = q, console_memory = console_memory, worker_memory = worker_memory, wake_strategy = discovery.cpus.wake_strategy(features = CpuFeatureFacts(monitor_mwait_available = true))))
    }
    phase owned_hardware(hardware: OwnedHardware) -> never { while true {} }
}
`

func TestInterruptQueueRejectsZeroCapacity(t *testing.T) {
	checked := checkedProgramFromSourceForTest(t, interruptQueueBoundsSource)
	checked.ImageGraph.InterruptQueues[0].Capacity = 0
	_, ds := Lower(checked)
	if !hasDiagCode(ds, diag.SEM0060) {
		t.Fatalf("expected SEM0060 for zero capacity, got %#v", ds)
	}
}

func TestInterruptQueueRejectsZeroPayloadSize(t *testing.T) {
	checked := checkedProgramFromSourceForTest(t, interruptQueueBoundsSource)
	checked.ImageGraph.InterruptQueues[0].PayloadSize = 0
	_, ds := Lower(checked)
	if !hasDiagCode(ds, diag.SEM0060) {
		t.Fatalf("expected SEM0060 for zero payload size, got %#v", ds)
	}
}

func TestInterruptQueueRejectsBackingSizeOverflow(t *testing.T) {
	checked := checkedProgramFromSourceForTest(t, interruptQueueBoundsSource)
	checked.ImageGraph.InterruptQueues[0].Capacity = uint64(1) << 63
	checked.ImageGraph.InterruptQueues[0].PayloadSize = 2
	_, ds := Lower(checked)
	if !hasDiagCode(ds, diag.SEM0060) {
		t.Fatalf("expected SEM0060 for backing size overflow, got %#v", ds)
	}
}

func TestInterruptQueueLowersLayout(t *testing.T) {
	checked := checkedProgramFromSourceForTest(t, interruptQueueBoundsSource)
	program, ds := Lower(checked)
	if len(ds) != 0 {
		t.Fatalf("Lower() diagnostics = %#v", ds)
	}
	if len(program.InterruptQueues) != 1 {
		t.Fatalf("interrupt queues = %#v", program.InterruptQueues)
	}
	queue := program.InterruptQueues[0]
	if queue.Label != "irq.serial.rx" || queue.Owner != "executor_slot.0" || queue.Capacity != 64 || queue.PayloadSize != 2 || queue.PayloadAlign != 1 || queue.Overflow != "drop_newest_and_set_flag" {
		t.Fatalf("interrupt queue layout = %#v", queue)
	}
}
