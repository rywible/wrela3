package sem

import (
	"strings"
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/report"
)

const overlappingArenaSource = `
module examples.bad_arena_overlap
use { BootPanic } from platform.hardware.panic
use { ArenaIdentity, ArenaPolicy, PhysicalRegionAuthority } from platform.hardware.memory

class GoodRoot {
    fn build(self) {
        let region = PhysicalRegionAuthority(base = 0x200000, length = 0x10000, align = 4096, provenance = 1, panic = BootPanic())
        let root = region.create_arena(identity = ArenaIdentity(label = "root"), policy = ArenaPolicy(evict_cache_by_default = true))
        let a = root.child_at(identity = ArenaIdentity(label = "a"), offset = 0, length = 8192, align = 4096)
        let b = root.child_at(identity = ArenaIdentity(label = "b"), offset = 4096, length = 8192, align = 4096)
    }
}`

func TestArenaGraphRejectsStaticOverlap(t *testing.T) {
	_, ds := checkTrustedPlatformSourceForTest(t, "platform.test.bad_arena_overlap", overlappingArenaSource)
	if !hasCode(ds, diag.SEM0058) {
		t.Fatalf("expected SEM0058, got %#v", ds)
	}
}

func TestArenaGraphRejectsDuplicateIdentity(t *testing.T) {
	src := strings.ReplaceAll(overlappingArenaSource, `label = "b"`, `label = "a"`)
	_, ds := checkTrustedPlatformSourceForTest(t, "platform.test.duplicate_arena", src)
	if !hasCode(ds, diag.SEM0057) {
		t.Fatalf("expected SEM0057, got %#v", ds)
	}
}

const reverseStaticChildAtSource = `
module examples.reverse_static_child_at
use { BootPanic } from platform.hardware.panic
use { ArenaIdentity, ArenaPolicy, PhysicalRegionAuthority } from platform.hardware.memory
use { ExecutorSlot } from machine.x86_64.executor_slot

class GoodRoot {
    fn build(self) {
        let region = PhysicalRegionAuthority(base = 0x200000, length = 0x10000, align = 4096, provenance = 1, panic = BootPanic())
        let root = region.create_arena(identity = ArenaIdentity(label = "root"), policy = ArenaPolicy(evict_cache_by_default = true))
        let high = root.child_at(identity = ArenaIdentity(label = "high"), offset = 0x4000, length = 0x1000, align = 4096)
        let low = root.child_at(identity = ArenaIdentity(label = "low"), offset = 0x2000, length = 0x1000, align = 4096)
        let implicit = root.executor_memory(owner = ExecutorSlot(id = 0), length = 0x1000, align = 4096)
    }
}`

func TestArenaGraphAllowsLowerNonOverlappingRootChildAt(t *testing.T) {
	checked, ds := checkTrustedPlatformSourceForTest(t, "platform.test.reverse_root_child_at", reverseStaticChildAtSource)
	if len(ds) != 0 {
		t.Fatalf("diagnostics: %#v", ds)
	}
	high := arenaNodeByLabelForTest(checked.ImageGraph.Arenas, "high")
	low := arenaNodeByLabelForTest(checked.ImageGraph.Arenas, "low")
	if high.Base != 0x204000 || low.Base != 0x202000 {
		t.Fatalf("reverse static arenas = high %#v low %#v", high, low)
	}
}

func TestArenaGraphImplicitAllocationUsesStaticChildAtCursorMax(t *testing.T) {
	checked, ds := checkTrustedPlatformSourceForTest(t, "platform.test.reverse_root_implicit", reverseStaticChildAtSource)
	if len(ds) != 0 {
		t.Fatalf("diagnostics: %#v", ds)
	}
	executor := arenaNodeByKindAndParentForTest(checked.ImageGraph.Arenas, "executor_memory", "root")
	if executor.Base != 0x205000 || executor.Offset != 0x5000 {
		t.Fatalf("implicit arena should follow max static end, got %#v", executor)
	}
}

const reverseNestedChildAtSource = `
module examples.reverse_nested_child_at
use { BootPanic } from platform.hardware.panic
use { ArenaIdentity, ArenaPolicy, PhysicalRegionAuthority } from platform.hardware.memory

class GoodRoot {
    fn build(self) {
        let region = PhysicalRegionAuthority(base = 0x200000, length = 0x20000, align = 4096, provenance = 1, panic = BootPanic())
        let root = region.create_arena(identity = ArenaIdentity(label = "root"), policy = ArenaPolicy(evict_cache_by_default = true))
        let parent = root.child_at(identity = ArenaIdentity(label = "parent"), offset = 0x8000, length = 0x8000, align = 4096)
        let high = parent.child_at(identity = ArenaIdentity(label = "nested_high"), offset = 0x4000, length = 0x1000, align = 4096)
        let low = parent.child_at(identity = ArenaIdentity(label = "nested_low"), offset = 0x2000, length = 0x1000, align = 4096)
    }
}`

func TestArenaGraphAllowsLowerNonOverlappingNestedChildAt(t *testing.T) {
	checked, ds := checkTrustedPlatformSourceForTest(t, "platform.test.reverse_nested_child_at", reverseNestedChildAtSource)
	if len(ds) != 0 {
		t.Fatalf("diagnostics: %#v", ds)
	}
	high := arenaNodeByLabelForTest(checked.ImageGraph.Arenas, "nested_high")
	low := arenaNodeByLabelForTest(checked.ImageGraph.Arenas, "nested_low")
	if high.Base != 0x20c000 || low.Base != 0x20a000 {
		t.Fatalf("reverse nested arenas = high %#v low %#v", high, low)
	}
}

const executorMemoryNearSource = `
module examples.executor_memory_near
use { BootPanic } from platform.hardware.panic
use { PlatformDiscoveryRoot } from platform.hardware.discovery
use { DelegatedHardware } from platform.uefi.transition
use { ArenaIdentity, ArenaPolicy } from platform.hardware.memory
use { OwnedHardware, OwnedMemory, IoPortAuthority, MemoryPlan, CpuPlan, HardwarePlan, InterruptRoutingPlan, ClaimedPciPlanBuilder } from machine.x86_64.cpu_state
use { CpuFeatureFacts } from machine.x86_64.cpu_state
use { ExecutorSlot } from machine.x86_64.executor_slot
use { MutableBytes, Bytes, ExecutorMemory } from machine.x86_64.executor_memory
use { InterruptSourceIdentity, InterruptVector } from machine.x86_64.interrupts
use { InterruptOverflowPolicy, InterruptPayloadKind, QueueIdentity } from machine.x86_64.interrupt_queue
image MemoryNearImage {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        let panic = BootPanic()
        let discovery = PlatformDiscoveryRoot(panic = panic).from_uefi(hardware = hardware)
        let root_region = discovery.memory.require_usable_region(min_base = 0x200000, length = 0x1000000, align = 4096)
        let root = root_region.create_arena(identity = ArenaIdentity(label = "root"), policy = ArenaPolicy(evict_cache_by_default = true))
        let placement = discovery.cpus.require_min_count(count = 2).placement(panic = panic)
        let memory = root.executor_memory_near(owner = ExecutorSlot(id = 0), near = placement.cpu_for(slot = ExecutorSlot(id = 0)), length = 0x200000, align = 4096)
        let console_memory = root.executor_memory(owner = ExecutorSlot(id = 1), length = 0x100000, align = 4096)
        let compat = root_region.bytes()
        let shared = discovery.interrupts.route_shared_irq(irq = 4, vector = InterruptVector(value = 0x40))
        let queue = root.interrupt_queue(identity = QueueIdentity(label = "irq.serial.rx"), owner = ExecutorSlot(id = 0), capacity = 64, payload = InterruptPayloadKind(kind = 1, size = 8, align = 8), overflow = InterruptOverflowPolicy(mode = 0))
        return hardware.exit_to_owned_hardware(memory_plan = MemoryPlan(owned_memory = OwnedMemory(arena = compat), executor_arena = compat, io_ports = IoPortAuthority()), cpu_plan = CpuPlan(owned_stack_top = 0, gdt_descriptor = Bytes(address = 0, length = 0), idt_descriptor = Bytes(address = 0, length = 0), cr3 = 0), hardware_plan = HardwarePlan(cpus = discovery.cpus.require_min_count(count = 2), interrupts = InterruptRoutingPlan(local_apic = discovery.interrupts.local_apic, serial_irq4 = shared.route, serial_shared_irq4 = shared, serial_irq_source = shared.claim_source(identity = InterruptSourceIdentity(label = "serial.rx"))), pci = ClaimedPciPlanBuilder(panic = panic).empty(), timer = discovery.timers.require_periodic(period_us = 1000), serial_irq_queue = queue, console_memory = console_memory, worker_memory = memory, wake_strategy = discovery.cpus.wake_strategy(features = CpuFeatureFacts(monitor_mwait_available = false))))
    }
    phase owned_hardware(hardware: OwnedHardware) -> never { while true {} }
}
`

const executorMemoryNearKnownTargetSource = `
module examples.executor_memory_near_known
use { BootPanic } from platform.hardware.panic
use { PlatformDiscoveryRoot } from platform.hardware.discovery
use { DelegatedHardware } from platform.uefi.transition
use { ArenaIdentity, ArenaPolicy } from platform.hardware.memory
use { OwnedHardware, OwnedMemory, IoPortAuthority, MemoryPlan, CpuPlan, HardwarePlan, InterruptRoutingPlan, ClaimedPciPlanBuilder } from machine.x86_64.cpu_state
use { CpuFeatureFacts } from machine.x86_64.cpu_state
use { ExecutorSlot } from machine.x86_64.executor_slot
use { MutableBytes, Bytes, ExecutorMemory } from machine.x86_64.executor_memory
use { InterruptSourceIdentity, InterruptVector } from machine.x86_64.interrupts
use { InterruptOverflowPolicy, InterruptPayloadKind, QueueIdentity } from machine.x86_64.interrupt_queue
use { PlacementTarget } from machine.x86_64.placement
image MemoryNearKnownImage {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        let panic = BootPanic()
        let discovery = PlatformDiscoveryRoot(panic = panic).from_uefi(hardware = hardware)
        let root_region = discovery.memory.require_usable_region(min_base = 0x200000, length = 0x1000000, align = 4096)
        let root = root_region.create_arena(identity = ArenaIdentity(label = "root"), policy = ArenaPolicy(evict_cache_by_default = true))
        let memory = root.executor_memory_near(owner = ExecutorSlot(id = 0), near = PlacementTarget(kind = 1, id = 0, known = true), length = 0x200000, align = 4096)
        let console_memory = root.executor_memory(owner = ExecutorSlot(id = 1), length = 0x100000, align = 4096)
        let compat = root_region.bytes()
        let shared = discovery.interrupts.route_shared_irq(irq = 4, vector = InterruptVector(value = 0x40))
        let queue = root.interrupt_queue(identity = QueueIdentity(label = "irq.serial.rx"), owner = ExecutorSlot(id = 0), capacity = 64, payload = InterruptPayloadKind(kind = 1, size = 8, align = 8), overflow = InterruptOverflowPolicy(mode = 0))
        return hardware.exit_to_owned_hardware(memory_plan = MemoryPlan(owned_memory = OwnedMemory(arena = compat), executor_arena = compat, io_ports = IoPortAuthority()), cpu_plan = CpuPlan(owned_stack_top = 0, gdt_descriptor = Bytes(address = 0, length = 0), idt_descriptor = Bytes(address = 0, length = 0), cr3 = 0), hardware_plan = HardwarePlan(cpus = discovery.cpus.require_min_count(count = 2), interrupts = InterruptRoutingPlan(local_apic = discovery.interrupts.local_apic, serial_irq4 = shared.route, serial_shared_irq4 = shared, serial_irq_source = shared.claim_source(identity = InterruptSourceIdentity(label = "serial.rx"))), pci = ClaimedPciPlanBuilder(panic = panic).empty(), timer = discovery.timers.require_periodic(period_us = 1000), serial_irq_queue = queue, console_memory = console_memory, worker_memory = memory, wake_strategy = discovery.cpus.wake_strategy(features = CpuFeatureFacts(monitor_mwait_available = false))))
    }
    phase owned_hardware(hardware: OwnedHardware) -> never { while true {} }
}
`

const misalignedChildArenaSource = `
module examples.misaligned_child_arena
use { BootPanic } from platform.hardware.panic
use { PlatformDiscoveryRoot } from platform.hardware.discovery
use { DelegatedHardware } from platform.uefi.transition
use { ArenaIdentity, ArenaPolicy } from platform.hardware.memory
use { OwnedHardware, OwnedMemory, IoPortAuthority, MemoryPlan, CpuPlan, HardwarePlan, InterruptRoutingPlan, ClaimedPciPlanBuilder } from machine.x86_64.cpu_state
use { CpuFeatureFacts } from machine.x86_64.cpu_state
use { ExecutorSlot } from machine.x86_64.executor_slot
use { MutableBytes, Bytes, ExecutorMemory } from machine.x86_64.executor_memory
use { InterruptSourceIdentity, InterruptVector } from machine.x86_64.interrupts
use { InterruptOverflowPolicy, InterruptPayloadKind, QueueIdentity } from machine.x86_64.interrupt_queue
image ArenaAlignmentImage {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        let panic = BootPanic()
        let discovery = PlatformDiscoveryRoot(panic = panic).from_uefi(hardware = hardware)
        let root_region = discovery.memory.require_usable_region(min_base = 0x200003, length = 0x1000000, align = 4096)
        let root = root_region.create_arena(identity = ArenaIdentity(label = "root"), policy = ArenaPolicy(evict_cache_by_default = true))
        let misaligned_child = root.child_at(identity = ArenaIdentity(label = "misaligned_child"), offset = 1, length = 0x1000, align = 4096)
        let nested_child = misaligned_child.child_at(identity = ArenaIdentity(label = "nested_child"), offset = 0, length = 0x100, align = 4096)
        let console_memory = root.executor_memory(owner = ExecutorSlot(id = 0), length = 0x100000, align = 4096)
        let compat = root_region.bytes()
        let shared = discovery.interrupts.route_shared_irq(irq = 4, vector = InterruptVector(value = 0x40))
        let queue = root.interrupt_queue(identity = QueueIdentity(label = "irq.serial.rx"), owner = ExecutorSlot(id = 0), capacity = 64, payload = InterruptPayloadKind(kind = 1, size = 8, align = 8), overflow = InterruptOverflowPolicy(mode = 0))
        return hardware.exit_to_owned_hardware(memory_plan = MemoryPlan(owned_memory = OwnedMemory(arena = compat), executor_arena = compat, io_ports = IoPortAuthority()), cpu_plan = CpuPlan(owned_stack_top = 0, gdt_descriptor = Bytes(address = 0, length = 0), idt_descriptor = Bytes(address = 0, length = 0), cr3 = 0), hardware_plan = HardwarePlan(cpus = discovery.cpus.require_min_count(count = 2), interrupts = InterruptRoutingPlan(local_apic = discovery.interrupts.local_apic, serial_irq4 = shared.route, serial_shared_irq4 = shared, serial_irq_source = shared.claim_source(identity = InterruptSourceIdentity(label = "serial.rx"))), pci = ClaimedPciPlanBuilder(panic = panic).empty(), timer = discovery.timers.require_periodic(period_us = 1000), serial_irq_queue = queue, console_memory = console_memory, worker_memory = console_memory, wake_strategy = discovery.cpus.wake_strategy(features = CpuFeatureFacts(monitor_mwait_available = false))))
    }
    phase owned_hardware(hardware: OwnedHardware) -> never { while true {} }
}
`

func TestExecutorMemoryNearRecordsFallback(t *testing.T) {
	checked, ds := checkUEFIModulesWithExtraSource(t, "memory-near.wrela", executorMemoryNearSource)
	if len(ds) != 0 {
		t.Fatalf("diagnostics: %#v", ds)
	}
	r := BuildImageReport(checked)
	if len(r.Runtime.Placement) != 1 {
		t.Fatalf("expected one placement decision, got %#v", r.Runtime.Placement)
	}
	if r.Runtime.Placement[0].Kind != "cpu_for_slot" || r.Runtime.Placement[0].SubjectA != "executor_slot.0" || r.Runtime.Placement[0].Satisfied {
		t.Fatalf("executor memory near decision = %#v", r.Runtime.Placement[0])
	}
	if len(r.Memory.ExecutorBudgets) == 0 {
		t.Fatalf("executor memory budget missing: %#v", r.Memory)
	}
	budget := r.Memory.ExecutorBudgets[0]
	if budget.Bytes != 0x200000 {
		t.Fatalf("executor memory budget = %#v", budget)
	}
	if !containsPlacementFallback(r.Runtime.Placement, "unknown_locality") {
		t.Fatalf("missing unknown-locality fallback: %#v", r.Runtime.Placement)
	}
	if len(r.Memory.RootRegions) != 1 || r.Memory.RootRegions[0].Base != 0x200000 || r.Memory.RootRegions[0].Bytes != 0x1000000 {
		t.Fatalf("require_usable_region origin not captured: %#v", r.Memory.RootRegions)
	}
	rootArena := arenaReportByLabelForTest(r.Memory.Arenas, "root")
	if rootArena.Label != "root" || rootArena.Kind != "root_arena" {
		t.Fatalf("memory graph should record root arena, got %#v", r.Memory.Arenas)
	}
	executorArena := arenaReportByKindForTest(r.Memory.Arenas, "executor_memory", "executor_slot.0")
	if executorArena.Bytes != 0x200000 || executorArena.Base != 0x200000 {
		t.Fatalf("executor memory arena = %#v, want base 0x200000 bytes 0x200000", executorArena)
	}
	queueArena := arenaReportByKindForTest(r.Memory.Arenas, "interrupt_queue", "executor_slot.0")
	if queueArena.Label != "irq.serial.rx" || queueArena.Bytes != 64*8 || queueArena.Base == 0 {
		t.Fatalf("interrupt queue arena = %#v, want 512 bytes with nonzero base", queueArena)
	}
}

func TestExecutorMemoryNearKnownTargetRecordsSatisfied(t *testing.T) {
	checked, ds := checkUEFIModulesWithExtraSource(t, "memory-near-known.wrela", executorMemoryNearKnownTargetSource)
	if len(ds) != 0 {
		t.Fatalf("diagnostics: %#v", ds)
	}
	r := BuildImageReport(checked)
	if len(r.Runtime.Placement) != 1 {
		t.Fatalf("expected one placement decision, got %#v", r.Runtime.Placement)
	}
	if r.Runtime.Placement[0].Kind != "cpu_for_slot" || r.Runtime.Placement[0].SubjectA != "executor_slot.0" {
		t.Fatalf("executor memory near decision = %#v", r.Runtime.Placement[0])
	}
	if !r.Runtime.Placement[0].Satisfied {
		t.Fatalf("expected satisfied placement near decision, got %#v", r.Runtime.Placement[0])
	}
	if r.Runtime.Placement[0].Fallback == "unknown_locality" {
		t.Fatalf("unexpected unknown-locality fallback for known placement target: %#v", r.Runtime.Placement[0])
	}
}

func TestArenaAlignmentUsesAbsoluteBase(t *testing.T) {
	checked, ds := checkUEFIModulesWithExtraSource(t, "arena-alignment.wrela", misalignedChildArenaSource)
	if len(ds) != 0 {
		t.Fatalf("diagnostics: %#v", ds)
	}
	r := BuildImageReport(checked)
	childArena := arenaReportByLabelForTest(r.Memory.Arenas, "misaligned_child")
	if childArena.Label != "misaligned_child" {
		t.Fatalf("child arena not recorded: %#v", r.Memory.Arenas)
	}
	if childArena.Base != 0x201000 {
		t.Fatalf("arena base should be absolute-aligned: %#v", childArena)
	}
	nestedArena := arenaReportByLabelForTest(r.Memory.Arenas, "nested_child")
	if nestedArena.Base != 0x201000 {
		t.Fatalf("nested arena base should use parent absolute-aligned origin: %#v", nestedArena)
	}
}

func TestArenaRuntimeSourceAlignsAbsoluteBase(t *testing.T) {
	source := readRepoFile(t, "wrela/platform/hardware/memory.wrela")
	for _, want := range []string{
		"let requested_base = self.region.base + offset",
		"let aligned_offset = base - self.region.base",
		"let requested_base = self.base + offset",
		"let aligned_offset = base - self.base",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("memory arena source missing absolute alignment step %q", want)
		}
	}
}

func arenaReportByKindForTest(arenas []report.ArenaReport, kind string, owner string) report.ArenaReport {
	for _, arena := range arenas {
		if arena.Kind == kind && arena.Owner == owner {
			return arena
		}
	}
	return report.ArenaReport{}
}

func arenaReportByLabelForTest(arenas []report.ArenaReport, label string) report.ArenaReport {
	for _, arena := range arenas {
		if arena.Label == label {
			return arena
		}
	}
	return report.ArenaReport{}
}

func arenaNodeByLabelForTest(arenas []ArenaNode, label string) ArenaNode {
	for _, arena := range arenas {
		if arena.Label == label {
			return arena
		}
	}
	return ArenaNode{}
}

func arenaNodeByKindAndParentForTest(arenas []ArenaNode, kind string, parent string) ArenaNode {
	for _, arena := range arenas {
		if arena.Kind == kind && arena.Parent == parent {
			return arena
		}
	}
	return ArenaNode{}
}
