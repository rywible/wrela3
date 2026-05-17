package ir

import "testing"

const productionTimerSourceForTest = `
module examples.production_timer
use { BootPanic } from platform.hardware.panic
use { PlatformDiscoveryRoot } from platform.hardware.discovery
use { DelegatedHardware } from platform.uefi.transition
use { OwnedHardware, OwnedMemory, IoPortAuthority, MemoryPlan, CpuPlan, HardwarePlan, InterruptRoutingPlan, ClaimedPciPlanBuilder, SlotIdentity, ExecutorSlot } from machine.x86_64.cpu_state
use { EventSleepPolicy, WakeStrategy } from machine.x86_64.executor_loop
use { MutableBytes, Bytes, ExecutorMemory } from machine.x86_64.executor_memory
use { InterruptVector } from machine.x86_64.interrupts
use { TimerAuthority, TimerSource } from machine.x86_64.timer
use { TimerTickSubscription } from machine.x86_64.topic_payload
executor Worker {
    slot: ExecutorSlot
    loop: EventSleepPolicy
    memory: ExecutorMemory
    ticks: TimerTickSubscription
    start fn run(self) -> never { while true {} }
}
image TimerImage {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        let panic = BootPanic()
        let discovery = PlatformDiscoveryRoot(panic = panic).from_uefi(hardware = hardware)
        let arena = MutableBytes(address = 0, length = 0)
        return hardware.exit_to_owned_hardware(memory_plan = MemoryPlan(owned_memory = OwnedMemory(arena = arena), executor_arena = arena, io_ports = IoPortAuthority()), cpu_plan = CpuPlan(owned_stack_top = 0, gdt_descriptor = Bytes(address = 0, length = 0), idt_descriptor = Bytes(address = 0, length = 0), cr3 = 0), hardware_plan = HardwarePlan(cpus = discovery.cpus.require_min_count(count = 2), interrupts = InterruptRoutingPlan(local_apic = discovery.interrupts.local_apic, serial_irq4 = discovery.interrupts.route_isa_irq(irq = 4, vector = InterruptVector(value = 0x40))), pci = ClaimedPciPlanBuilder(panic = panic).empty()))
    }
    phase owned_hardware(hardware: OwnedHardware) -> never {
        let worker_slot = hardware.executors.claim(identity = SlotIdentity(label = "worker"))
        let worker_memory = hardware.memory.claim_executor_arena(owner = worker_slot, length = 0x200000, align = 4096)
        let timer = TimerAuthority(source = TimerSource(kind = 1), period_us = 1000, panic = BootPanic())
        let ticks = timer.subscribe(subscriber = worker_slot)
        let worker = Worker(slot = worker_slot, loop = EventSleepPolicy(strategy = WakeStrategy(monitor_mwait = false, fallback_hlt = true)), memory = worker_memory, ticks = ticks)
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
}
