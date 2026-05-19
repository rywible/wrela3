package ir

import "testing"

const productionTimerSourceForTest = `
module examples.production_timer
use { BootPanic } from platform.hardware.panic
use { PlatformDiscoveryRoot } from platform.hardware.discovery
use { DelegatedHardware } from platform.uefi.transition
use { ArenaIdentity, ArenaPolicy } from platform.hardware.memory
use { OwnedHardware, OwnedMemory, IoPortAuthority, MemoryPlan, CpuPlan, HardwarePlan, InterruptRoutingPlan, ClaimedPciPlanBuilder, SlotIdentity } from machine.x86_64.cpu_state
use { CpuFeatureFacts } from machine.x86_64.cpu_state
use { ExecutorSlot } from machine.x86_64.executor_slot
use { EventSleepPolicy } from machine.x86_64.executor_loop
use { MutableBytes, Bytes, ExecutorMemory } from machine.x86_64.executor_memory
use { InterruptSourceIdentity, InterruptVector } from machine.x86_64.interrupts
use { InterruptOverflowPolicy, InterruptQueue, QueueIdentity } from machine.x86_64.interrupt_queue
use { TopicSubscription } from machine.x86_64.topic
use { Option } from wrela.lang.core
use { TimerTickPayload } from machine.x86_64.topic_payload
executor Worker {
    slot: ExecutorSlot
    loop: EventSleepPolicy
    memory: ExecutorMemory
    ticks: TopicSubscription<TimerTickPayload>
    start fn run(self) -> never {
        self.loop.wait()
        while true {}
    }
}
image TimerImage {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        let panic = BootPanic()
        let discovery = PlatformDiscoveryRoot(panic = panic).from_uefi(hardware = hardware)
        let root_region = discovery.memory.require_usable_region(min_base = 0x200000, length = 0x1000000, align = 4096)
        let root = root_region.create_arena(identity = ArenaIdentity(label = "root"), policy = ArenaPolicy(evict_cache_by_default = true))
        let arena = root_region.bytes()
        let shared = discovery.interrupts.route_shared_irq(irq = 4, vector = InterruptVector(value = 0x40))
        let console_memory = root.executor_memory(owner = ExecutorSlot(id = 0), length = 0x100000, align = 4096)
        let queue_slots = console_memory.reserve_array(U8, count = 64)
        let queue = InterruptQueue<U8>(identity = QueueIdentity(label = "irq.serial.rx"), owner = ExecutorSlot(id = 0), slots = queue_slots, capacity = 64, overflow = InterruptOverflowPolicy(mode = 0), head = 0, tail = 0, overflowed = false)
        let worker_memory = root.executor_memory(owner = ExecutorSlot(id = 1), length = 0x200000, align = 4096)
        return hardware.exit_to_owned_hardware(memory_plan = MemoryPlan(owned_memory = OwnedMemory(arena = arena), executor_arena = arena, io_ports = IoPortAuthority()), cpu_plan = CpuPlan(owned_stack_top = 0, gdt_descriptor = Bytes(address = 0, length = 0), idt_descriptor = Bytes(address = 0, length = 0), cr3 = 0), hardware_plan = HardwarePlan(storage_replay_last_event_id = 0, storage_replay_projection_watermark = 0, storage_replay_orphan_collected = 0, cpus = discovery.cpus.require_min_count(count = 2), interrupts = InterruptRoutingPlan(local_apic = discovery.interrupts.local_apic, serial_irq4 = shared.route, serial_shared_irq4 = shared, serial_irq_source = shared.claim_source(identity = InterruptSourceIdentity(label = "serial.rx"))), pci = ClaimedPciPlanBuilder(panic = panic).empty(), timer = discovery.timers.require_periodic(period_us = 1000), serial_irq_queue = queue, console_memory = console_memory, worker_memory = worker_memory, wake_strategy = discovery.cpus.wake_strategy(features = CpuFeatureFacts(monitor_mwait_available = true))))
    }
    phase owned_hardware(hardware: OwnedHardware) -> never {
        let worker_slot = hardware.executors.claim(identity = SlotIdentity(label = "worker"))
        let worker_memory = hardware.hardware_plan.executor_memory(owner = worker_slot, memory = hardware.hardware_plan.console_memory)
        let timer = hardware.hardware_plan.timer
        timer.initialize(local_apic = hardware.hardware_plan.interrupts.local_apic)
        let ticks = timer.subscribe(subscriber = worker_slot)
        let worker = Worker(slot = worker_slot, loop = EventSleepPolicy(strategy = hardware.hardware_plan.wake_strategy), memory = worker_memory, ticks = ticks)
        hardware.vcpu0.enter(executor = worker)
    }
}
`

func TestTimerSubscribeLowersToTimerTopic(t *testing.T) {
	checked := checkedProgramFromSourceForTest(t, productionTimerSourceForTest)
	program, ds := Lower(checked)
	if len(ds) != 0 {
		t.Fatalf("Lower diagnostics: %#v", ds)
	}
	if len(program.Timers) != 1 || program.Timers[0].Vector != 0x43 {
		t.Fatalf("timers = %#v, want vector 0x43", program.Timers)
	}
	owned := findFunction(program, program.Entry.OwnedPhaseSymbol)
	if owned == nil {
		t.Fatalf("missing owned phase %s", program.Entry.OwnedPhaseSymbol)
	}
	init, ok := functionOp[TimerInit](*owned)
	if !ok || init.Source != "local_apic_pit_calibrated" || init.PeriodUS != 1000 || init.Vector != 0x43 {
		t.Fatalf("timer init = %#v, want local APIC/PIT vector 0x43", init)
	}
	worker := findFunction(program, "_wrela_method_examples_production_timer_Worker_run")
	if worker == nil {
		t.Fatal("missing lowered worker")
	}
	wait, ok := functionOp[TopicWait](*worker)
	if !ok || !wait.UseMonitorMwait || wait.Fallback != "sti_hlt" {
		t.Fatalf("worker wait = %#v, want monitor/mwait topic wait with hlt fallback", wait)
	}
}
