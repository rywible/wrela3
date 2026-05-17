# Production Substrate Convergence Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Converge Wrela's memory authority, hardware discovery, CPU/interrupt/timer, and executor runtime tracks into one production substrate where driver work can claim bounded resources instead of relying on QEMU constants, ambient globals, or hidden runtime behavior.

**Architecture:** Firmware-derived authorities produce physical memory roots and typed hardware facts. The compiler records an image authority graph covering arenas, queues, topics, timers, interrupts, PCI/MMIO claims, executor slots, placement decisions, and wake paths; semantic checks reject forged or duplicated authority; codegen emits deterministic bounded storage and x86_64 glue while keeping source-level policy explicit. The runtime remains static and executor-owned: no scheduler, migration, work stealing, or implicit vCPU multiplexing.

**Tech Stack:** Go 1.22+; existing hand-written Wrela lexer/parser; semantic image graph in `compiler/sem`; IR and direct x86_64 codegen in `compiler/ir` and `compiler/codegen`; platform source in `wrela/platform` and `wrela/machine/x86_64`; JSON report emission from `compiler.Build`; Go unit tests, Wrela negative fixtures, source-shape tests, and QEMU q35 + OVMF e2e tests.

---

## 0. How To Execute This Plan

This plan is written for junior engineers who know Go and can follow exact test-driven steps, but do not know Wrela's compiler internals.

Assumptions locked by this plan:

- `docs/design/2026-05-17-production-substrate-convergence-design.md` is the source design.
- Existing physical arena, frame lifetime, executor slot, SPMC topic, interrupt topic, PCI discovery, and AP startup work is the baseline. Do not remove those systems; extend them.
- Execute this plan from an isolated worktree created by `superpowers:using-git-worktrees`. The plan touches shared compiler and platform files and should not be run directly in a dirty user branch.
- Every task ends with a commit whose message ends in `-Codex Automated`.
- Use small commits. If a task edits more files than listed, write down why in the commit body.
- A failing-test step must fail for the stated reason before implementation. If it passes, stop the task, inspect the existing implementation, and either mark the already-implemented subcase in the task notes or replace only the now-obsolete assertion with the next missing behavior from that same task.
- If a task's precondition is wrong, do not widen scope. Record the exact mismatch, add a minimal compatibility step inside that task, and rerun only that task's verification before continuing.
- A passing-test step is complete only when the exact command prints `PASS` or the exact scan output described by the task.
- Do not run QEMU-only tests unless the task asks for them. Unit tests are the fast loop.
- Do not change Wrela syntax in this plan. The API names below are frozen.

Definition of done for a task:

- All checkbox steps are complete.
- All new tests fail before implementation and pass after implementation.
- `git diff --check` prints nothing.
- The task commit exists with the exact message shown.

Definition of done for the full plan:

- `go test ./...` passes.
- `go test ./tests/e2e -run 'Hello|ProductionSubstrate' -v` passes on machines with QEMU and OVMF.
- `go run ./cmd/wrela build --mode dev examples/hello/main.wrela -o build/hello.efi --report build/hello.report.json` writes both files.
- `rg -n "MutableBytes\\(address = 0x|arena_base = 0x|q35|two-vCPU|apStartupDelayLoopCount" examples tests/e2e/fixtures wrela/machine wrela/platform/hardware compiler/codegen docs/runtime docs/production-deferred-work.md docs/design/hello_world_design_doc.md` only reports approved compatibility lines listed in Task 22. Do not scan `docs/implementation` or the source design doc; those files intentionally contain historical and explanatory examples.
- `build/hello.report.json` contains `authority_audit.memory_roots`, `authority_audit.arenas`, `authority_audit.hardware_claims`, `authority_audit.interrupts`, `authority_audit.timers`, `authority_audit.queues`, `authority_audit.topics`, `authority_audit.wake_targets`, and `authority_audit.dma_buffers`.

---

## 1. Frozen Production Substrate Decisions

Do not reopen these decisions during task execution.

- Source-visible memory roots come from firmware discovery through `PhysicalRegionAuthority`.
- Ordinary modules cannot construct `PhysicalRegionAuthority`, `RootArena`, `ChildArena`, `MmioRegion`, `PciDevice`, interrupt route, timer, or executor authority roots from integer literals.
- Trusted platform modules are exactly modules whose name starts with `platform.` or `machine.x86_64.`. Only those modules can construct firmware-rooted production authorities.
- `MutableBytes(address = literal, length = literal)` remains legal only in the direct `delegated_hardware` phase until Task 19 migrates examples away from that compatibility path. That compatibility exception does not apply to `PhysicalRegionAuthority`, `RootArena`, or `ChildArena`.
- `PhysicalRegionAuthority.create_arena(...)` creates a `RootArena`.
- `RootArena.child(...)` performs monotonic allocation.
- `RootArena.child_at(...)` creates statically placed child arenas. Static overlap is rejected by the compiler.
- `ChildArena.child(...)` and `ChildArena.child_at(...)` are legal and use the same deterministic rules as root arenas.
- `executor_memory`, `executor_memory_near`, `interrupt_queue`, `cache_arena`, and `dma_buffer` are arena allocation methods, not standalone constructors.
- Arena allocation overflow is boot-fatal with `BootPanic.fail(code = 0xAC070001)`.
- Interrupt queue overflow policy is source-visible. The default constructor requires an explicit `InterruptOverflowPolicy`.
- The initial timer implementation supports local APIC timer calibrated by PIT. HPET is exposed as a discovered fact shape but is not programmed in this plan. PIT-only fallback is allowed only for calibration failure diagnostics and QEMU compatibility tests.
- AP startup contract for this plan is the documented low-page trampoline contract. The trampoline must remain below 1 MiB, 4 KiB aligned, and identity mapped; the backend must enforce those properties with tests.
- x2APIC support is runtime-selected from discovered CPU facts. xAPIC fallback is mandatory unless source calls `require_x2apic()`.
- Shared ISA IRQs claim the hardware route once and register source identities under that route.
- Typed topics are implemented by generalizing topic layout to static payload type, size, and alignment. This plan proves the model with `TimerTickPayload`, a non-`U64` payload.
- Wake strategy is chosen from discovered CPU feature facts and explicit executor wait policy. `monitor/mwait` has an `sti; hlt` fallback.
- There is no scheduler in this milestone. The compiler rejects types or calls named `Scheduler`, `RunnableQueue`, `migrate`, `work_steal`, or `spawn_on_any_cpu` outside tests and docs.
- The image report is a security audit artifact. Missing required sections are a compile error when `--report` is requested.
- Wrela field mutation through `self.field = value` is valid and already used in existing source such as `PciDeviceSet.append` and `InterruptOverrideSet.append`; arena methods may mutate `next_offset`.
- Wrela supports `true` and `false` boolean literals in current examples and tests.
- Do not use static-style factory calls on data types. Write `InterruptOverflowPolicy(mode = 0)` and `TimerSource(kind = 1)`, not `InterruptOverflowPolicy.drop_newest_and_set_flag()` or `TimerSource.local_apic_pit_calibrated()`.
- Self-check before committing any task: every symbol used by that task must already exist in the repo, be created by an earlier task, or be defined completely in that task.

---

## 2. Repository Layout And File Responsibilities

Create or modify exactly these files unless a task explicitly says no source edits are expected.

```text
compiler/diag/codes.go
  Adds convergence diagnostics SEM0056-SEM0075.

compiler/report/report.go
compiler/report/report_test.go
  Defines the stable JSON image report and helpers for authority audit sections.

compiler/sem/convergence_testutil_test.go
compiler/ir/convergence_testutil_test.go
compiler/codegen/convergence_testutil_test.go
  Defines shared helper functions and source fixtures referenced by later tasks.

compiler/build.go
compiler/build_test.go
cmd/wrela/main.go
cmd/wrela/main_test.go
compiler/integration_hardware_discovery_test.go
  Adds optional --report output without changing EFI output behavior.

compiler/sem/image_graph.go
compiler/sem/check.go
compiler/sem/memory_graph.go
compiler/sem/memory_graph_test.go
compiler/sem/hardware_authority_test.go
compiler/sem/hardware_claim_test.go
  Extends semantic graph extraction and authority checks for physical regions, arenas, queues, timers, placement, and DMA-intended buffers.

compiler/sem/hardware_discovery_test.go
compiler/sem/mcfg_parser_contract_test.go
compiler/sem/pci_ecam_contract_test.go
compiler/sem/pci_bar_contract_test.go
compiler/sem/madt_parser_contract_test.go
compiler/sem/acpi_parser_contract_test.go
  Locks source-visible discovery fact shapes.

compiler/sem/placement.go
compiler/sem/placement_test.go
  Records required/preferred placement constraints and rejects hidden scheduler constructs.

compiler/sem/topic_payload.go
compiler/sem/topic_payload_test.go
compiler/sem/interrupt_queue_test.go
compiler/sem/timer_authority_test.go
  Classifies typed topics, timer authorities, shared IRQ source claims, and bounded interrupt queues.

compiler/ir/ir.go
compiler/ir/lower.go
compiler/ir/topic_test.go
compiler/ir/timer_test.go
compiler/ir/interrupt_queue_test.go
  Carries payload layouts, queue layouts, timer routes, placement facts, and report facts to codegen.

compiler/codegen/topic_data.go
compiler/codegen/topic_test.go
compiler/codegen/interrupt_queue.go
compiler/codegen/interrupt_queue_test.go
compiler/codegen/interrupt_test.go
compiler/codegen/lapic.go
compiler/codegen/apic_mode.go
compiler/codegen/apic_mode_test.go
compiler/codegen/timer.go
compiler/codegen/timer_test.go
compiler/codegen/vcpu_start.go
compiler/codegen/vcpu_start_test.go
compiler/codegen/wait_test.go
  Emits deterministic data sections and x86_64 glue for payload topics, interrupt queues, APIC mode, timers, AP startup contract checks, and wake strategy.

wrela/platform/hardware/memory.wrela
wrela/platform/hardware/discovery.wrela
wrela/platform/hardware/bytes.wrela
wrela/platform/hardware/panic.wrela
wrela/platform/acpi/mcfg.wrela
wrela/platform/acpi/madt.wrela
wrela/platform/acpi/tables.wrela
wrela/platform/acpi/root.wrela
wrela/platform/uefi/types.wrela
wrela/platform/uefi/transition.wrela
  Implements firmware-derived authority and typed discovery facts.

wrela/machine/x86_64/cpu_state.wrela
wrela/machine/x86_64/executor_memory.wrela
wrela/machine/x86_64/cache_memory.wrela
wrela/machine/x86_64/interrupts.wrela
wrela/machine/x86_64/interrupt_queue.wrela
wrela/machine/x86_64/timer.wrela
wrela/machine/x86_64/placement.wrela
wrela/machine/x86_64/topic_payload.wrela
wrela/machine/x86_64/topic_u64.wrela
wrela/machine/x86_64/executor_loop.wrela
wrela/machine/x86_64/pci.wrela
wrela/machine/x86_64/serial.wrela
wrela/machine/x86_64/edu.wrela
wrela/machine/x86_64/ivshmem.wrela
  Provides source-visible substrate APIs consumed by boot images and drivers.

examples/hello/main.wrela
examples/hello/program.wrela
tests/e2e/fixtures/production_substrate/main.wrela
tests/e2e/fixtures/production_substrate/program.wrela
tests/e2e/production_substrate_qemu_test.go
tests/e2e/hello_qemu_test.go
  Proves the substrate in examples and QEMU.

docs/runtime/ap-startup-contract.md
docs/production-deferred-work.md
docs/design/hello_world_design_doc.md
  Documents implemented substrate contracts and keeps older docs aligned.
```

---

## 3. Execution And Merge Map

The work is one integrated plan. Parallelism is intentionally limited because several streams converge on the same compiler graph and report files. Use the streams for planning context, but use the merge gates and file ownership windows below as the real execution contract.

```text
Merge Gate 0: Task 1
  Freezes diagnostics and the JSON report skeleton.

Merge Gate 0.5: Task 1A
  Defines shared test helpers and source fixtures. No later task may reference a helper or source constant that is not listed there or defined inline.

Stream A: Memory Authority And Report Spine
  Tasks 2-4
  Owns physical region contracts, arena graph extraction, and --report output.

Stream B: Discovery Facts
  Tasks 5-8
  Owns ECAM windows, bridge walking, PCI facts, timer/APIC/topology facts, framebuffer facts, and discovery report fields.

Stream C: CPU, Timer, And Interrupt Delivery
  Tasks 9-13
  Owns APIC mode selection, AP startup contract, timer topic publication, shared IRQ claims, and bounded interrupt queues.

Stream D: Executor Runtime Integration
  Tasks 14-18
  Owns placement constraints, locality-aware memory, typed payload topics, wake selection, scheduler rejection, and final authority audit report.

Stream E: Migration And End-To-End Acceptance
  Tasks 19-22
  Owns examples, QEMU fixtures, scans, docs, and final acceptance.
```

Dependency rules:

- Task 1 must land first.
- Task 1A must land immediately after Task 1 and before any other task starts.
- Tasks 2-4 are serial.
- Tasks 5-8 may start after Task 1. Task 8 depends on Tasks 5-7.
- Tasks 9-10 may start after Task 7.
- Task 11 depends on Tasks 7, 9, and 16 because timer publication uses `TimerTickPayload`.
- Task 12 depends on Tasks 2 and 3 because it appends interrupt queue/shared IRQ fields to `compiler/sem/image_graph.go`.
- Task 13 depends on Task 12.
- Task 14 depends on Tasks 3, 7, 8, and 12 because it appends placement fields to `compiler/sem/image_graph.go` after shared IRQ fields land.
- Task 15 depends on Task 14.
- Task 16 depends on Tasks 1A and 7. Execute Task 16 before Task 11 even though it appears later in the phase order.
- Task 17 depends on Tasks 9, 14, and 16.
- Task 18 depends on Tasks 4, 8, 13, 15, 16, and 17.
- Task 19 depends on Task 18.
- Tasks 20-22 are serial and start after Task 19.

Shared-file ownership windows:

```text
compiler/sem/image_graph.go
  Task 3 owns memory graph fields.
  Task 8 may append APIC/timer/locality/framebuffer fact fields after Task 3 lands.
  Task 12 may append interrupt queue/shared IRQ fields only after Task 3 lands.
  Task 14 may append placement fields only after Task 12 lands.

compiler/sem/report.go
  Task 4 creates the report builder.
  Task 8 appends discovery facts.
  Task 15 appends locality-aware executor memory.
  Task 17 appends wake strategy.
  Task 18 performs the final completeness pass.
  No two tasks may edit this file in parallel.

compiler/ir/ir.go and compiler/ir/lower.go
  Task 16 owns typed TopicLayout payload fields.
  Task 11 owns TimerRoute after Task 16 lands.
  Task 13 owns InterruptQueueLayout.
  Task 17 owns wake strategy fields.
  Merge these tasks in this order: Task 16, Task 11, Task 13, Task 17.

wrela/machine/x86_64/interrupts.wrela
  Task 7 owns fact fields.
  Task 9 owns APIC mode selection methods.
  Task 12 owns shared IRQ route/source APIs.
  Merge these tasks in that order.

wrela/machine/x86_64/cpu_state.wrela
  Task 7 owns discovery/locality facts.
  Task 14 owns placement plan APIs.
  Task 17 owns wake feature facts.
  Merge these tasks in that order.
```

---

## 4. Canonical Production Substrate Surface

These source names, report field names, diagnostic codes, and package names are fixed.

### Memory Authority Source Surface

```wrela
module platform.hardware.memory

use { BootPanic } from platform.hardware.panic
use { ExecutorSlot } from machine.x86_64.cpu_state
use { ExecutorMemory, MutableBytes } from machine.x86_64.executor_memory
use { CacheArena } from machine.x86_64.cache_memory
use { InterruptQueue, InterruptOverflowPolicy, QueueIdentity, InterruptPayloadKind } from machine.x86_64.interrupt_queue
use { PciDevice } from machine.x86_64.pci

data ArenaIdentity {
    label: StringLiteral
}

data ArenaPolicy {
    evict_cache_by_default: Bool
}

data PhysicalRegionAuthority {
    base: PhysicalAddress
    length: U64
    align: U64
    provenance: U64
    panic: BootPanic
}

data RootArena {
    region: PhysicalRegionAuthority
    identity: ArenaIdentity
    policy: ArenaPolicy
    next_offset: U64

    fn child(self, identity: ArenaIdentity, length: U64, align: U64) -> ChildArena {
        let offset = self.next_offset
        return self.child_at(identity = identity, offset = offset, length = length, align = align)
    }

    fn child_at(self, identity: ArenaIdentity, offset: U64, length: U64, align: U64) -> ChildArena {
        let base = (self.region.base + offset + align - 1) & (0 - align)
        let aligned_offset = base - self.region.base
        let end = aligned_offset + length
        if end > self.region.length {
            self.region.panic.fail(code = 0xAC070001)
        }
        if aligned_offset < self.next_offset {
            if end > self.next_offset {
                self.region.panic.fail(code = 0xAC070001)
            }
        }
        if end > self.next_offset {
            self.next_offset = end
        }
        return ChildArena(root = self, identity = identity, base = base, length = length, next_offset = 0)
    }
}

data ChildArena {
    root: RootArena
    identity: ArenaIdentity
    base: PhysicalAddress
    length: U64
    next_offset: U64
}
```

### Discovery Source Surface

```wrela
data DiscoveredHardware {
    memory: UefiMemoryMap
    acpi: AcpiRoot
    interrupts: InterruptAuthority
    pci: PciDeviceSet
    cpus: CpuDiscovery
    timers: TimerDiscovery
    framebuffer: FramebufferDiscovery
    panic: BootPanic
}

data DiscoveryReport {
    memory_base: PhysicalAddress
    memory_length: U64
    ecam_window_count: U64
    pci_device_count: U64
    pci_bridge_count: U64
    enabled_cpu_count: U64
    bootstrap_apic_id: U32
    selected_apic_mode: U32
    timer_source: U32
    locality_fact_mask: U64
    framebuffer_base: PhysicalAddress
    framebuffer_length: U64
}
```

### Interrupt Queue Source Surface

```wrela
data QueueIdentity {
    label: StringLiteral
}

data InterruptPayloadKind {
    kind: U64
    size: U64
    align: U64
}

data InterruptOverflowPolicy {
    mode: U64
}

class InterruptQueue {
    identity: QueueIdentity
    owner: ExecutorSlot
    storage: MutableBytes
    capacity: U64
    payload: InterruptPayloadKind
    overflow: InterruptOverflowPolicy
    head: U64
    tail: U64
    overflowed: Bool
}
```

Overflow policy values are fixed:

```text
0 = drop_newest_and_set_flag
1 = drop_oldest_and_set_flag
2 = set_flag_and_wake
3 = boot_fatal
```

### Placement Source Surface

```wrela
class CpuPlacementPlan {
    topology: CpuTopology
    panic: BootPanic

    fn require_separate_physical_cores(self, a: ExecutorSlot, b: ExecutorSlot) {
        if self.topology.can_prove_separate_cores() == false {
            self.panic.fail(code = 0xAC040101)
        }
    }

    fn prefer_same_cache_group(self, a: ExecutorSlot, b: ExecutorSlot) -> PlacementPreferenceResult {
        return self.topology.prefer_same_cache_group(a = a, b = b)
    }

    fn prefer_near_device(self, slot: ExecutorSlot, device: PciDevice) -> PlacementPreferenceResult {
        return self.topology.prefer_near_device(slot = slot, device = device)
    }
}
```

### Stable JSON Report Shape

```json
{
  "version": 1,
  "image": "Hello",
  "memory": {
    "total_bytes": 16777216,
    "root_regions": [],
    "arenas": [],
    "executor_budgets": []
  },
  "hardware": {
    "pci": [],
    "apic": {},
    "timers": [],
    "locality": []
  },
  "runtime": {
    "executors": [],
    "placement": [],
    "topics": [],
    "interrupt_queues": [],
    "wake_paths": []
  },
  "authority_audit": {
    "memory_roots": [],
    "arenas": [],
    "hardware_claims": [],
    "interrupts": [],
    "timers": [],
    "queues": [],
    "topics": [],
    "wake_targets": [],
    "dma_buffers": []
  }
}
```

---

## 5. Phase 1: Memory Authority And Report Spine

**Description:** Replace the remaining raw executor/device memory root shortcuts with firmware-derived physical region authority, hierarchical arenas, static arena graph extraction, and a report artifact. This phase does not change AP startup, timers, interrupts, or topic payload behavior.

**Phase Acceptance Criteria:**

- `PhysicalRegionAuthority`, `RootArena`, and `ChildArena` exist in `platform.hardware.memory`.
- Ordinary modules cannot forge physical region or arena authorities.
- Statically overlapping `child_at` allocations are rejected.
- Arena, executor memory, cache memory, interrupt queue, and DMA-intended allocations appear in `CheckedProgram.ImageGraph`.
- `wrela build --report <path>` writes valid JSON with memory and authority audit sections.

**Phase Code Example:**

```wrela
let root_region = discovery.memory.require_usable_region(
    min_base = 0x200000,
    length = 0x1000000,
    align = 4096
)
let root_arena = root_region.create_arena(
    identity = ArenaIdentity(label = "boot.root"),
    policy = ArenaPolicy(evict_cache_by_default = true)
)
let console_arena = root_arena.child_at(
    identity = ArenaIdentity(label = "executor.console"),
    offset = 0,
    length = 0x200000,
    align = 4096
)
```

### Task 1: Diagnostics And Report Package Skeleton

**Description:** Reserve convergence diagnostic codes and create a stable report package before any stream starts emitting report data.

**Files:**
- Modify: `compiler/diag/codes.go`
- Create: `compiler/report/report.go`
- Create: `compiler/report/report_test.go`

**Code Examples:**

Add this test to `compiler/report/report_test.go`:

```go
package report

import (
	"encoding/json"
	"testing"
)

func TestImageReportJSONShape(t *testing.T) {
	r := ImageReport{
		Version: 1,
		Image:   "Hello",
		Memory: MemoryReport{
			TotalBytes: 0x1000000,
			RootRegions: []MemoryRootReport{{
				Label: "boot.root",
				Base:  0x200000,
				Bytes: 0x1000000,
			}},
		},
		AuthorityAudit: AuthorityAuditReport{
			MemoryRoots: []AuthorityRecord{{Kind: "memory_root", Label: "boot.root"}},
		},
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	for _, key := range []string{"version", "image", "memory", "hardware", "runtime", "authority_audit"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("report missing top-level key %q in %s", key, data)
		}
	}
}
```

Add this report skeleton:

```go
package report

type ImageReport struct {
	Version        int                  `json:"version"`
	Image          string               `json:"image"`
	Memory         MemoryReport         `json:"memory"`
	Hardware       HardwareReport       `json:"hardware"`
	Runtime        RuntimeReport        `json:"runtime"`
	AuthorityAudit AuthorityAuditReport `json:"authority_audit"`
}

type MemoryReport struct {
	TotalBytes      uint64                 `json:"total_bytes"`
	RootRegions     []MemoryRootReport     `json:"root_regions"`
	Arenas          []ArenaReport          `json:"arenas"`
	ExecutorBudgets []ExecutorBudgetReport `json:"executor_budgets"`
}

type MemoryRootReport struct {
	Label string `json:"label"`
	Base  uint64 `json:"base"`
	Bytes uint64 `json:"bytes"`
}

type ArenaReport struct {
	Label  string `json:"label"`
	Parent string `json:"parent"`
	Base   uint64 `json:"base"`
	Bytes  uint64 `json:"bytes"`
	Owner  string `json:"owner"`
}

type ExecutorBudgetReport struct {
	SlotLabel string `json:"slot_label"`
	Bytes     uint64 `json:"bytes"`
}

type HardwareReport struct {
	PCI      []PCIReport      `json:"pci"`
	APIC     APICReport       `json:"apic"`
	Timers   []TimerReport    `json:"timers"`
	Locality []LocalityReport `json:"locality"`
}

type PCIReport struct {
	Identity string `json:"identity"`
	BARs     []BARReport `json:"bars"`
}

type BARReport struct {
	Index uint8  `json:"index"`
	Kind  string `json:"kind"`
	Base  uint64 `json:"base"`
	Bytes uint64 `json:"bytes"`
}

type APICReport struct {
	Mode string `json:"mode"`
}

type TimerReport struct {
	Label  string `json:"label"`
	Source string `json:"source"`
	PeriodUS uint64 `json:"period_us"`
}

type LocalityReport struct {
	Subject string `json:"subject"`
	Kind    string `json:"kind"`
	Value   string `json:"value"`
	Known   bool   `json:"known"`
}

type RuntimeReport struct {
	Executors       []ExecutorReport       `json:"executors"`
	Placement       []PlacementReport      `json:"placement"`
	Topics          []TopicReport          `json:"topics"`
	InterruptQueues []InterruptQueueReport `json:"interrupt_queues"`
	WakePaths       []WakePathReport       `json:"wake_paths"`
}

type ExecutorReport struct {
	SlotLabel string `json:"slot_label"`
	VcpuID    uint64 `json:"vcpu_id"`
}

type PlacementReport struct {
	Kind      string `json:"kind"`
	SubjectA  string `json:"subject_a"`
	SubjectB  string `json:"subject_b"`
	Required  bool   `json:"required"`
	Satisfied bool   `json:"satisfied"`
	Fallback  string `json:"fallback"`
}

type TopicReport struct {
	Label       string `json:"label"`
	PayloadType string `json:"payload_type"`
	Bytes       uint64 `json:"bytes"`
	Align       uint64 `json:"align"`
	Depth       uint64 `json:"depth"`
}

type InterruptQueueReport struct {
	Label    string `json:"label"`
	Owner    string `json:"owner"`
	Capacity uint64 `json:"capacity"`
	Overflow string `json:"overflow"`
}

type WakePathReport struct {
	SlotLabel string `json:"slot_label"`
	Strategy  string `json:"strategy"`
	Fallback  string `json:"fallback"`
}

type AuthorityAuditReport struct {
	MemoryRoots    []AuthorityRecord `json:"memory_roots"`
	Arenas         []AuthorityRecord `json:"arenas"`
	HardwareClaims []AuthorityRecord `json:"hardware_claims"`
	Interrupts     []AuthorityRecord `json:"interrupts"`
	Timers         []AuthorityRecord `json:"timers"`
	Queues         []AuthorityRecord `json:"queues"`
	Topics         []AuthorityRecord `json:"topics"`
	WakeTargets    []AuthorityRecord `json:"wake_targets"`
	DMABuffers     []AuthorityRecord `json:"dma_buffers"`
}

type AuthorityRecord struct {
	Kind       string `json:"kind"`
	Label      string `json:"label"`
	Owner      string `json:"owner"`
	Provenance string `json:"provenance"`
}
```

Add these constants after `SEM0055`:

```go
	SEM0056 = "SEM0056" // physical region authority cannot be forged
	SEM0057 = "SEM0057" // duplicate arena identity
	SEM0058 = "SEM0058" // statically overlapping arena placement
	SEM0059 = "SEM0059" // arena allocation exceeds static parent bounds
	SEM0060 = "SEM0060" // interrupt queue overflow policy is missing or invalid
	SEM0061 = "SEM0061" // timer authority lacks explicit source or boot-fatal path
	SEM0062 = "SEM0062" // shared interrupt source identity is invalid or duplicated
	SEM0063 = "SEM0063" // APIC mode selection lacks fallback or required-mode fatal
	SEM0064 = "SEM0064" // required executor placement cannot be satisfied
	SEM0065 = "SEM0065" // preferred executor placement is not reportable
	SEM0066 = "SEM0066" // topic payload layout is not statically known
	SEM0067 = "SEM0067" // hidden scheduler construct is not allowed
	SEM0068 = "SEM0068" // DMA buffer requires explicit device owner
	SEM0069 = "SEM0069" // discovery capacity overflow must boot-fatal
	SEM0070 = "SEM0070" // duplicate timer claim
	SEM0071 = "SEM0071" // PCI bridge walking exceeded bounded recursion
	SEM0072 = "SEM0072" // unknown locality must be represented explicitly
	SEM0073 = "SEM0073" // x2APIC selection requires xAPIC fallback or required-mode fatal
	SEM0074 = "SEM0074" // AP startup contract is not enforced
	SEM0075 = "SEM0075" // authority audit report is incomplete
```

**Steps:**

- [ ] **Step 1: Write tests**

Add `TestImageReportJSONShape` and `TestConvergenceDiagnosticCodesExist`.

```go
func TestConvergenceDiagnosticCodesExist(t *testing.T) {
	codes := []string{
		diag.SEM0056, diag.SEM0057, diag.SEM0058, diag.SEM0059, diag.SEM0060,
		diag.SEM0061, diag.SEM0062, diag.SEM0063, diag.SEM0064, diag.SEM0065,
		diag.SEM0066, diag.SEM0067, diag.SEM0068, diag.SEM0069, diag.SEM0070,
		diag.SEM0071, diag.SEM0072, diag.SEM0073, diag.SEM0074, diag.SEM0075,
	}
	for _, code := range codes {
		if code == "" {
			t.Fatalf("diagnostic code must not be empty")
		}
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
go test ./compiler/report ./compiler/diag -run 'TestImageReportJSONShape|TestConvergenceDiagnosticCodesExist' -v
```

Expected: FAIL because `compiler/report` does not exist and `SEM0056` is undefined.

- [ ] **Step 3: Add diagnostics and report structs**

Add the constants and `compiler/report/report.go` exactly as shown.

- [ ] **Step 4: Run verification**

Run:

```bash
go test ./compiler/report ./compiler/diag -run 'TestImageReportJSONShape|TestConvergenceDiagnosticCodesExist' -v
git diff --check
```

Expected: tests PASS; `git diff --check` prints nothing.

- [ ] **Step 5: Commit**

```bash
git add compiler/diag/codes.go compiler/report/report.go compiler/report/report_test.go
git commit -m "feat: reserve production substrate report contracts -Codex Automated"
```

**Acceptance Criteria:** Convergence diagnostics SEM0056-SEM0075 exist; `compiler/report` marshals the stable top-level JSON shape; no runtime behavior changes.

### Task 1A: Shared Test Helpers And Fixture Sources

**Description:** Define every reusable helper and synthetic Wrela source constant used by later tasks so no task starts with undefined test scaffolding.

**Files:**
- Create: `compiler/sem/convergence_testutil_test.go`
- Create: `compiler/ir/convergence_testutil_test.go`
- Create: `compiler/codegen/convergence_testutil_test.go`

**Code Examples:**

Existing helpers this task intentionally reuses:

```text
compiler/sem/testutil_test.go: parseModulesForTest
compiler/sem/memory_negative_test.go: parseFixtureModulesForTest
compiler/sem/uefi_source_shape_test.go: parseUEFIModuleSet
compiler/sem/hardware_authority_test.go: checkUEFIModulesWithExtraSource
tests/e2e/hello_qemu_test.go: requireQEMUDeps, copyFile, qemuTimeout
```

If one of those helper names fails to compile because a previous task moved it, search `compiler/sem/*test*.go`, `compiler/codegen/*testutil*.go`, and `tests/e2e/*test*.go` first. Add a new helper only when no existing helper provides the same behavior.

`checkTrustedPlatformSourceForTest` rewrites `module examples.*` to `module platform.test.*` so tests can exercise trusted-platform construction. Do not ship `platform.test.*` modules in examples or runtime source; they are trusted by the semantic rule exactly like any other `platform.*` module.

Create `compiler/sem/convergence_testutil_test.go`:

```go
package sem

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/report"
)

func checkTrustedPlatformSourceForTest(t *testing.T, name string, sourceText string) (*CheckedProgram, []diag.Diagnostic) {
	t.Helper()
	if !strings.HasPrefix(sourceText, "module platform.") && !strings.HasPrefix(sourceText, "\nmodule platform.") {
		sourceText = strings.Replace(sourceText, "module examples.", "module platform.test.", 1)
	}
	modules := append(parseUEFIModuleSet(t), parseModulesForTest(t, sourceText)...)
	index, ds := BuildIndex(modules)
	if len(ds) != 0 {
		t.Fatalf("%s index diagnostics: %#v", name, ds)
	}
	return Check(index, modules)
}

func checkedProgramFromSourceForTest(t *testing.T, sourceText string) *CheckedProgram {
	t.Helper()
	modules := append(parseUEFIModuleSet(t), parseModulesForTest(t, sourceText)...)
	index, ds := BuildIndex(modules)
	if len(ds) != 0 {
		t.Fatalf("index diagnostics: %#v", ds)
	}
	checked, ds := Check(index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}
	return checked
}

func parseNegativeFixtureForConvergenceTest(t *testing.T, fixture string) []*ast.Module {
	t.Helper()
	return parseFixtureModulesForTest(t, filepath.Join("tests", "fixtures", "negative", fixture))
}

func containsPlacementFallback(placements []report.PlacementReport, fallback string) bool {
	for _, placement := range placements {
		if placement.Fallback == fallback {
			return true
		}
	}
	return false
}
```

Create `compiler/ir/convergence_testutil_test.go`:

```go
package ir

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ryanwible/wrela3/compiler/parse"
	"github.com/ryanwible/wrela3/compiler/sem"
	"github.com/ryanwible/wrela3/compiler/source"
)

func checkedProgramFromSourceForTest(t *testing.T, sourceText string) *sem.CheckedProgram {
	t.Helper()
	dir := t.TempDir()
	rootPath := filepath.Join(dir, "main.wrela")
	if err := os.WriteFile(rootPath, []byte(sourceText), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	repoRoot := repoRootForIRTest(t)
	graph, err := source.LoadGraph(source.Options{
		RootPath: rootPath,
		ImportRoots: []string{repoRoot, filepath.Join(repoRoot, "wrela")},
	})
	if err != nil {
		t.Fatalf("load graph: %v", err)
	}
	modules, ds := parse.ParseGraph(*graph)
	if len(ds) != 0 {
		t.Fatalf("parse diagnostics: %#v", ds)
	}
	index, ds := sem.BuildIndex(modules)
	if len(ds) != 0 {
		t.Fatalf("index diagnostics: %#v", ds)
	}
	checked, ds := sem.Check(index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}
	return checked
}

func repoRootForIRTest(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			return wd
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			t.Fatalf("could not find repo root from %s", wd)
		}
		wd = parent
	}
}
```

Create `compiler/codegen/convergence_testutil_test.go`:

```go
package codegen

import (
	"bytes"
	"encoding/binary"

	"github.com/ryanwible/wrela3/compiler/ir"
)

func encodeImm32ForTest(value int64) []byte {
	out := make([]byte, 4)
	binary.LittleEndian.PutUint32(out, uint32(value))
	return out
}

func compileVcpuStartForTest(t testingT) compiledUnit {
	t.Helper()
	program := &ir.Program{
		Types: map[string]ir.TypeInfo{},
		VcpuStarts: []ir.VcpuStartPlan{{VcpuID: 1, APICID: 1, SlotLabel: "worker"}},
	}
	ctx := compileContext{types: program.Types, VcpuPlans: vcpuPlanMap(program)}
	e := newEmitter(ctx)
	emitWaitForVcpuReady(e, 1)
	return compiledUnit{Symbol: "vcpu_start_test", Bytes: e.Code, CallReloc: e.CallReloc}
}

type testingT interface {
	Helper()
}

func bytesContainForTest(haystack, needle []byte) bool {
	return bytes.Contains(haystack, needle)
}
```

Use these exact source constants in later semantic and IR tests:

```go
const hiddenSchedulerSource = `
module negative.hidden_scheduler
class Scheduler {}
`

const missingOverflowPolicySource = `
module examples.missing_overflow_policy
use { BootPanic } from platform.hardware.panic
use { PlatformDiscoveryRoot } from platform.hardware.discovery
use { DelegatedHardware } from platform.uefi.transition
use { ArenaIdentity, ArenaPolicy } from platform.hardware.memory
use { OwnedHardware, OwnedMemory, IoPortAuthority, MemoryPlan, CpuPlan, HardwarePlan, InterruptRoutingPlan, ClaimedPciPlanBuilder, SlotIdentity } from machine.x86_64.cpu_state
use { MutableBytes, Bytes } from machine.x86_64.executor_memory
use { InterruptVector } from machine.x86_64.interrupts
use { InterruptPayloadKind, QueueIdentity } from machine.x86_64.interrupt_queue
image Bad {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        let panic = BootPanic()
        let discovery = PlatformDiscoveryRoot(panic = panic).from_uefi(hardware = hardware)
        let root_region = discovery.memory.require_usable_region(min_base = 0x200000, length = 0x1000000, align = 4096)
        let root = root_region.create_arena(identity = ArenaIdentity(label = "root"), policy = ArenaPolicy(evict_cache_by_default = true))
        let slot = hardware.executors.claim(identity = SlotIdentity(label = "console"))
        let q = root.interrupt_queue(identity = QueueIdentity(label = "irq.serial.rx"), owner = slot, capacity = 64, payload = InterruptPayloadKind(kind = 1, size = 8, align = 8))
        return hardware.exit_to_owned_hardware(memory_plan = MemoryPlan(owned_memory = OwnedMemory(arena = MutableBytes(address = 0, length = 0)), executor_arena = MutableBytes(address = 0, length = 0), io_ports = IoPortAuthority()), cpu_plan = CpuPlan(owned_stack_top = 0, gdt_descriptor = Bytes(address = 0, length = 0), idt_descriptor = Bytes(address = 0, length = 0), cr3 = 0), hardware_plan = HardwarePlan(cpus = discovery.cpus.require_min_count(count = 2), interrupts = InterruptRoutingPlan(local_apic = discovery.interrupts.local_apic, serial_irq4 = discovery.interrupts.route_isa_irq(irq = 4, vector = InterruptVector(value = 0x40))), pci = ClaimedPciPlanBuilder(panic = panic).empty()))
    }
    phase owned_hardware(hardware: OwnedHardware) -> never { while true {} }
}
`

const sharedIRQSource = `
module examples.shared_irq_good
// Full imports are intentionally copied from existing hardware claim tests.
use { BootPanic } from platform.hardware.panic
use { PlatformDiscoveryRoot } from platform.hardware.discovery
use { DelegatedHardware } from platform.uefi.transition
use { OwnedHardware, OwnedMemory, IoPortAuthority, MemoryPlan, CpuPlan, HardwarePlan, InterruptRoutingPlan, ClaimedPciPlanBuilder } from machine.x86_64.cpu_state
use { MutableBytes, Bytes } from machine.x86_64.executor_memory
use { InterruptVector, InterruptSourceIdentity } from machine.x86_64.interrupts
image SharedIRQGood {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        let panic = BootPanic()
        let discovery = PlatformDiscoveryRoot(panic = panic).from_uefi(hardware = hardware)
        let route = discovery.interrupts.route_shared_irq(irq = 11, vector = InterruptVector(value = 0x43))
        let nic = route.claim_source(identity = InterruptSourceIdentity(label = "nic0"))
        let storage = route.claim_source(identity = InterruptSourceIdentity(label = "ahci0"))
        let arena = MutableBytes(address = 0, length = 0)
        return hardware.exit_to_owned_hardware(memory_plan = MemoryPlan(owned_memory = OwnedMemory(arena = arena), executor_arena = arena, io_ports = IoPortAuthority()), cpu_plan = CpuPlan(owned_stack_top = 0, gdt_descriptor = Bytes(address = 0, length = 0), idt_descriptor = Bytes(address = 0, length = 0), cr3 = 0), hardware_plan = HardwarePlan(cpus = discovery.cpus.require_min_count(count = 2), interrupts = InterruptRoutingPlan(local_apic = discovery.interrupts.local_apic, serial_irq4 = discovery.interrupts.route_isa_irq(irq = 4, vector = InterruptVector(value = 0x40))), pci = ClaimedPciPlanBuilder(panic = panic).empty()))
    }
    phase owned_hardware(hardware: OwnedHardware) -> never { while true {} }
}
`

var duplicateSharedIRQSource = strings.Replace(sharedIRQSource, `label = "ahci0"`, `label = "nic0"`, 1)
```

For `placementSource`, `executorMemoryNearSource`, `productionTimerSourceForTest`, and `timerTickTopicSource`, each task that uses the constant must define the complete source inline in that task because the source depends on APIs introduced in that task.

**Steps:**

- [ ] **Step 1: Add helper files**

Add the three helper files exactly as shown. If an existing helper already provides the same function name in that package, do not duplicate it; add only the missing helpers and leave a comment in the task notes.

- [ ] **Step 2: Run helper compile check**

Run:

```bash
go test ./compiler/sem ./compiler/ir ./compiler/codegen -run '^$' -v
```

Expected: PASS, meaning all helper files compile without running tests.

- [ ] **Step 3: Commit**

```bash
git add compiler/sem/convergence_testutil_test.go compiler/ir/convergence_testutil_test.go compiler/codegen/convergence_testutil_test.go
git commit -m "test: add production substrate shared fixtures -Codex Automated"
```

**Acceptance Criteria:** Later tasks have no undefined helper names; shared source constants are either defined here or explicitly required inline by their task; helper compile check passes.

### Task 2: Physical Region And Arena Authority Source Contracts

**Description:** Add the source-visible physical region authority and arena APIs, then teach the semantic checker that these authorities cannot be forged by ordinary modules.

**Files:**
- Create: `wrela/platform/hardware/memory.wrela`
- Modify: `wrela/platform/hardware/discovery.wrela`
- Modify: `wrela/platform/uefi/types.wrela`
- Modify: `examples/hello/main.wrela`
- Modify: `examples/multi_vcpu_topics/main.wrela`
- Modify: `tests/e2e/fixtures/arena_memory/main.wrela`
- Modify: `tests/e2e/fixtures/cache_memory/main.wrela`
- Modify: `tests/e2e/fixtures/hello_ivshmem/main.wrela`
- Modify: `compiler/integration_hardware_discovery_test.go`
- Modify: `compiler/sem/memory.go`
- Modify: `compiler/sem/hardware_authority_test.go`
- Create: `tests/fixtures/negative/forged_physical_region_authority.wrela`
- Create: `tests/fixtures/negative/forged_root_arena.wrela`

**Code Examples:**

Create `wrela/platform/hardware/memory.wrela`:

```wrela
module platform.hardware.memory

use { BootPanic } from platform.hardware.panic
use { ExecutorSlot } from machine.x86_64.cpu_state
use { ExecutorMemory, MutableBytes } from machine.x86_64.executor_memory
use { CacheArena } from machine.x86_64.cache_memory
use { PciDevice } from machine.x86_64.pci

data ArenaIdentity {
    label: StringLiteral
}

data ArenaPolicy {
    evict_cache_by_default: Bool
}

data PhysicalRegionAuthority {
    base: PhysicalAddress
    length: U64
    align: U64
    provenance: U64
    panic: BootPanic

    fn create_arena(self, identity: ArenaIdentity, policy: ArenaPolicy) -> RootArena {
        return RootArena(region = self, identity = identity, policy = policy, next_offset = 0)
    }

    fn bytes(self) -> MutableBytes {
        return MutableBytes(address = self.base, length = self.length)
    }
}

data RootArena {
    region: PhysicalRegionAuthority
    identity: ArenaIdentity
    policy: ArenaPolicy
    next_offset: U64

    fn child(self, identity: ArenaIdentity, length: U64, align: U64) -> ChildArena {
        return self.child_at(identity = identity, offset = self.next_offset, length = length, align = align)
    }

    fn child_at(self, identity: ArenaIdentity, offset: U64, length: U64, align: U64) -> ChildArena {
        let base = (self.region.base + offset + align - 1) & (0 - align)
        let aligned_offset = base - self.region.base
        let end = aligned_offset + length
        if end > self.region.length {
            self.region.panic.fail(code = 0xAC070001)
        }
        if aligned_offset < self.next_offset {
            if end > self.next_offset {
                self.region.panic.fail(code = 0xAC070001)
            }
        }
        if end > self.next_offset {
            self.next_offset = end
        }
        return ChildArena(root = self, identity = identity, base = base, length = length, next_offset = 0)
    }

    fn executor_memory(self, owner: ExecutorSlot, length: U64, align: U64) -> ExecutorMemory {
        let child = self.child(identity = ArenaIdentity(label = "executor.memory"), length = length, align = align)
        return ExecutorMemory(arena_base = child.base, arena_length = child.length, next_offset = 0)
    }

    fn cache_arena(self, identity: ArenaIdentity, length: U64, align: U64) -> CacheArena {
        let child = self.child(identity = identity, length = length, align = align)
        return CacheArena(
            storage = MutableBytes(address = child.base, length = child.length),
            slot_count = 0,
            slot_size = 0,
            next_victim = 0,
            initialized = 0
        )
    }

    fn dma_buffer(self, owner: PciDevice, identity: ArenaIdentity, length: U64, align: U64) -> DmaBuffer {
        let child = self.child(identity = identity, length = length, align = align)
        return DmaBuffer(
            owner = owner,
            identity = identity,
            bytes = MutableBytes(address = child.base, length = child.length)
        )
    }
}

data ChildArena {
    root: RootArena
    identity: ArenaIdentity
    base: PhysicalAddress
    length: U64
    next_offset: U64

    fn child(self, identity: ArenaIdentity, length: U64, align: U64) -> ChildArena {
        return self.child_at(identity = identity, offset = self.next_offset, length = length, align = align)
    }

    fn child_at(self, identity: ArenaIdentity, offset: U64, length: U64, align: U64) -> ChildArena {
        let base = (self.base + offset + align - 1) & (0 - align)
        let aligned_offset = base - self.base
        let end = aligned_offset + length
        if end > self.length {
            self.root.region.panic.fail(code = 0xAC070001)
        }
        if aligned_offset < self.next_offset {
            if end > self.next_offset {
                self.root.region.panic.fail(code = 0xAC070001)
            }
        }
        if end > self.next_offset {
            self.next_offset = end
        }
        return ChildArena(root = self.root, identity = identity, base = base, length = length, next_offset = 0)
    }
}

data DmaBuffer {
    owner: PciDevice
    identity: ArenaIdentity
    bytes: MutableBytes
}
```

Update `UefiMemoryMap.require_usable_region` to return `PhysicalRegionAuthority` instead of `MutableBytes`:

```wrela
fn require_usable_region(self, min_base: PhysicalAddress, length: U64, align: U64) -> PhysicalRegionAuthority {
    let region = self.find_usable_region(min_base = min_base, length = length, align = align)
    return PhysicalRegionAuthority(
        base = region.address,
        length = length,
        align = align,
        provenance = 1,
        panic = self.panic
    )
}
```

Add semantic classification:

```go
func IsPhysicalRegionAuthorityType(t *Type) bool {
	return t != nil && t.Module == "platform.hardware.memory" && t.Name == "PhysicalRegionAuthority"
}

func IsArenaAuthorityType(t *Type) bool {
	if t == nil || t.Module != "platform.hardware.memory" {
		return false
	}
	return t.Name == "RootArena" || t.Name == "ChildArena"
}
```

Reject ordinary constructors:

```go
if IsPhysicalRegionAuthorityType(typ) || IsArenaAuthorityType(typ) {
	if !isTrustedPlatformModule(moduleName) {
		c.error(expr.SpanV, diag.SEM0056, "physical region and arena authorities cannot be forged")
		return
	}
}

func isTrustedPlatformModule(moduleName string) bool {
	return strings.HasPrefix(moduleName, "platform.") || strings.HasPrefix(moduleName, "machine.x86_64.")
}
```

**Steps:**

- [ ] **Step 1: Add negative fixtures**

Create `tests/fixtures/negative/forged_physical_region_authority.wrela`:

```wrela
// expect: SEM0056: physical region and arena authorities cannot be forged
module negative.forged_physical_region_authority

use { BootPanic } from platform.hardware.panic
use { PhysicalRegionAuthority } from platform.hardware.memory

class Bad {
    fn mint(self) -> PhysicalRegionAuthority {
        return PhysicalRegionAuthority(base = 0x200000, length = 4096, align = 4096, provenance = 99, panic = BootPanic())
    }
}
```

Create `tests/fixtures/negative/forged_root_arena.wrela`:

```wrela
// expect: SEM0056: physical region and arena authorities cannot be forged
module negative.forged_root_arena

use { ArenaIdentity, ArenaPolicy, PhysicalRegionAuthority, RootArena } from platform.hardware.memory
use { BootPanic } from platform.hardware.panic

class Bad {
    fn mint(self) -> RootArena {
        let region = PhysicalRegionAuthority(base = 0x200000, length = 4096, align = 4096, provenance = 1, panic = BootPanic())
        return RootArena(region = region, identity = ArenaIdentity(label = "bad"), policy = ArenaPolicy(evict_cache_by_default = true), next_offset = 0)
    }
}
```

- [ ] **Step 2: Add semantic tests**

Add:

```go
func TestPhysicalRegionAndArenaAuthorityForgeryRejected(t *testing.T) {
	for _, fixture := range []string{
		"forged_physical_region_authority.wrela",
		"forged_root_arena.wrela",
	} {
		t.Run(fixture, func(t *testing.T) {
			modules := parseFixtureModulesForTest(t, filepath.Join("tests", "fixtures", "negative", fixture))
			index, ds := BuildIndex(modules)
			if len(ds) != 0 {
				t.Fatalf("index diagnostics: %#v", ds)
			}
			_, ds = Check(index, modules)
			if !hasCode(ds, diag.SEM0056) {
				t.Fatalf("expected SEM0056, got %#v", ds)
			}
		})
	}
}
```

- [ ] **Step 3: Run tests to verify failure**

Run:

```bash
go test ./compiler/sem -run TestPhysicalRegionAndArenaAuthorityForgeryRejected -v
```

Expected: FAIL because the new Wrela module and semantic classification do not exist.

- [ ] **Step 4: Add source contracts and semantic rejection**

Add `platform.hardware.memory`, update `UefiMemoryMap.require_usable_region`, import the new types in `platform.hardware.discovery`, and add the checker helpers shown above.

- [ ] **Step 5: Migrate existing callers so the tree still compiles**

Today these files call `require_usable_region(...)` and expect `MutableBytes`:

```text
examples/hello/main.wrela
examples/multi_vcpu_topics/main.wrela
tests/e2e/fixtures/arena_memory/main.wrela
tests/e2e/fixtures/cache_memory/main.wrela
tests/e2e/fixtures/hello_ivshmem/main.wrela
compiler/integration_hardware_discovery_test.go
```

In each Wrela file, replace:

```wrela
let memory_region = discovery.memory.require_usable_region(
    min_base = 0x200000,
    length = 0x200000,
    align = 4096
)
```

with:

```wrela
let root_region = discovery.memory.require_usable_region(
    min_base = 0x200000,
    length = 0x200000,
    align = 4096
)
let memory_region = root_region.bytes()
```

Keep the local name `memory_region` for existing `discovery.report(memory = memory_region, ...)`, `OwnedMemory(arena = memory_region)`, and `MemoryPlan(executor_arena = memory_region)` call sites. Do not migrate those examples to arenas here; Task 19 does the final production-source rewrite.

Update `compiler/integration_hardware_discovery_test.go` so the source-shape assertion looks for both:

```go
"let root_region = discovery.memory.require_usable_region("
"let memory_region = root_region.bytes()"
```

- [ ] **Step 6: Run verification**

Run:

```bash
go test ./compiler/sem -run 'TestPhysicalRegionAndArenaAuthorityForgeryRejected|TestHardwareDiscoverySourceShape' -v
go test ./compiler -run 'Integration|HardwareDiscovery' -v
go test ./compiler -run NegativeFixtures -v
git diff --check
```

Expected: all tests PASS.

- [ ] **Step 7: Commit**

```bash
git add wrela/platform/hardware/memory.wrela wrela/platform/hardware/discovery.wrela wrela/platform/uefi/types.wrela examples/hello/main.wrela examples/multi_vcpu_topics/main.wrela tests/e2e/fixtures/arena_memory/main.wrela tests/e2e/fixtures/cache_memory/main.wrela tests/e2e/fixtures/hello_ivshmem/main.wrela compiler/integration_hardware_discovery_test.go compiler/sem/memory.go compiler/sem/hardware_authority_test.go tests/fixtures/negative/forged_physical_region_authority.wrela tests/fixtures/negative/forged_root_arena.wrela
git commit -m "feat: add physical region arena authority contracts -Codex Automated"
```

**Acceptance Criteria:** Wrela source has firmware-derived physical region authority; ordinary source cannot construct region or arena authorities; UEFI memory discovery returns `PhysicalRegionAuthority`; all current `require_usable_region(...)` callers compile through the explicit `root_region.bytes()` compatibility bridge; existing discovery source-shape tests pass.

### Task 3: Arena Graph Extraction And Static Overlap Checks

**Description:** Record arena allocations in the semantic image graph, reject duplicate arena identities, and reject statically knowable overlapping `child_at` placements.

**Files:**
- Modify: `compiler/sem/image_graph.go`
- Create: `compiler/sem/memory_graph.go`
- Create: `compiler/sem/memory_graph_test.go`
- Modify: `compiler/sem/check.go`

**Code Examples:**

Add graph nodes:

```go
type MemoryRootNode struct {
	Label string
	Base  uint64
	Bytes uint64
	Span  source.Span
}

type ArenaNode struct {
	Label  string
	Parent string
	Base   uint64
	Offset uint64
	Bytes  uint64
	Align  uint64
	Owner  string
	Kind   string
	Span   source.Span
}

type DMABufferNode struct {
	Label       string
	OwnerDevice string
	Base        uint64
	Bytes       uint64
	Span        source.Span
}
```

Extend `ImageGraph`:

```go
MemoryRoots []MemoryRootNode
Arenas      []ArenaNode
DMABuffers  []DMABufferNode
```

Static overlap helper:

```go
func arenaRangesOverlap(a, b ArenaNode) bool {
	if a.Parent == "" || b.Parent == "" || a.Parent != b.Parent {
		return false
	}
	aEnd := a.Offset + a.Bytes
	bEnd := b.Offset + b.Bytes
	return a.Offset < bEnd && b.Offset < aEnd
}
```

Test source:

```go
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
```

The constructor is intentionally inside a test module that the helper marks as trusted; do not weaken normal authority rejection to make this source pass in real user modules.

**Steps:**

- [ ] **Step 1: Add graph extraction tests**

Add:

```go
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
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
go test ./compiler/sem -run 'TestArenaGraphRejectsStaticOverlap|TestArenaGraphRejectsDuplicateIdentity' -v
```

Expected: FAIL because no arena graph exists.

- [ ] **Step 3: Implement arena graph recording**

In call checking, record these method calls:

```text
PhysicalRegionAuthority.create_arena -> MemoryRootNode + root ArenaNode
RootArena.child_at                 -> ArenaNode{Parent: root label}
ChildArena.child_at                -> ArenaNode{Parent: child label}
RootArena.executor_memory          -> ArenaNode{Kind: executor_memory, Owner: slot label}
RootArena.cache_arena              -> ArenaNode{Kind: cache_memory, Owner: arena label}
RootArena.dma_buffer               -> DMABufferNode and ArenaNode{Kind: dma_buffer, Owner: PCI identity}
```

Use literal extraction only when all `identity.label`, `offset`, `length`, and `align` values are source literals. If a value is dynamic, record the label and owner, set numeric fields to zero, and skip static overlap for that node.

- [ ] **Step 4: Implement duplicate and overlap diagnostics**

After graph extraction, run:

```go
func (c *checker) validateArenaGraph() {
	seen := map[string]source.Span{}
	for _, arena := range c.imageGraph.Arenas {
		if arena.Label == "" {
			continue
		}
		if prev, ok := seen[arena.Label]; ok {
			_ = prev
			c.error(arena.Span, diag.SEM0057, "duplicate arena identity "+arena.Label)
		}
		seen[arena.Label] = arena.Span
	}
	for i := range c.imageGraph.Arenas {
		for j := i + 1; j < len(c.imageGraph.Arenas); j++ {
			if arenaRangesOverlap(c.imageGraph.Arenas[i], c.imageGraph.Arenas[j]) {
				c.error(c.imageGraph.Arenas[j].Span, diag.SEM0058, "statically overlapping arena placement")
			}
		}
	}
}
```

- [ ] **Step 5: Run verification**

Run:

```bash
go test ./compiler/sem -run 'TestArenaGraph|TestPhysicalRegionAndArenaAuthorityForgeryRejected' -v
git diff --check
```

Expected: all tests PASS.

- [ ] **Step 6: Commit**

```bash
git add compiler/sem/image_graph.go compiler/sem/memory_graph.go compiler/sem/memory_graph_test.go compiler/sem/check.go
git commit -m "feat: record arena graph and reject overlaps -Codex Automated"
```

**Acceptance Criteria:** Arena graph nodes are available to later phases; duplicate arena labels fail with SEM0057; static sibling overlap fails with SEM0058; dynamic allocation remains legal and reportable.

### Task 4: Build Report Emission

**Description:** Add optional `--report` support to the CLI and build pipeline, then emit the memory section from the semantic image graph.

**Files:**
- Modify: `compiler/build.go`
- Modify: `compiler/build_test.go`
- Modify: `cmd/wrela/main.go`
- Modify: `cmd/wrela/main_test.go`
- Create: `compiler/sem/report.go`
- Create: `compiler/sem/report_test.go`

**Code Examples:**

Extend build options:

```go
type BuildOptions struct {
	Mode       Mode
	RootPath   string
	OutputPath string
	ReportPath string
	RepoRoot   string
}

type BuildResult struct {
	OutputPath string
	ReportPath string
	Image      *codegen.Image
	Report     *report.ImageReport
}
```

Write report when requested:

```go
if opts.ReportPath != "" {
	reportPath := opts.ReportPath
	if !filepath.IsAbs(reportPath) {
		reportPath = filepath.Join(repoRoot, reportPath)
	}
	imgReport := sem.BuildImageReport(checked)
	data, err := json.MarshalIndent(imgReport, "", "  ")
	if err != nil {
		return BuildResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(reportPath), 0o755); err != nil {
		return BuildResult{}, err
	}
	if err := os.WriteFile(reportPath, append(data, '\n'), 0o644); err != nil {
		return BuildResult{}, err
	}
	result.ReportPath = reportPath
	result.Report = &imgReport
}
```

CLI parser addition:

```go
case arg == "--report":
	i++
	if i >= len(args) {
		return "", "", "", "", "", false
	}
	reportPath = args[i]
case strings.HasPrefix(arg, "--report="):
	reportPath = strings.TrimPrefix(arg, "--report=")
```

On successful build, keep the existing stdout behavior: print only the EFI output path, followed by a newline. Do not print the report path. `TestRunAcceptsReportFlag` should continue to assert that the output file and report file exist instead of depending on extra stdout.

**Steps:**

- [ ] **Step 1: Add build test**

Add:

```go
func TestBuildWritesReportWhenRequested(t *testing.T) {
	dir := t.TempDir()
	result, err := Build(BuildOptions{
		Mode:       ModeDev,
		RootPath:   "examples/hello/main.wrela",
		OutputPath: filepath.Join(dir, "hello.efi"),
		ReportPath: filepath.Join(dir, "hello.report.json"),
		RepoRoot:   ".",
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if result.ReportPath == "" || result.Report == nil {
		t.Fatalf("BuildResult missing report: %#v", result)
	}
	data, err := os.ReadFile(result.ReportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if !bytes.Contains(data, []byte(`"authority_audit"`)) {
		t.Fatalf("report missing authority audit:\n%s", data)
	}
}
```

- [ ] **Step 2: Add CLI test**

Add:

```go
func TestRunAcceptsReportFlag(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "hello.efi")
	rep := filepath.Join(dir, "hello.report.json")
	code := run([]string{"build", "--mode", "dev", "examples/hello/main.wrela", "-o", out, "--report", rep})
	if code != 0 {
		t.Fatalf("run exit = %d, want 0", code)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("expected EFI output: %v", err)
	}
	if _, err := os.Stat(rep); err != nil {
		t.Fatalf("expected report output: %v", err)
	}
}
```

- [ ] **Step 3: Run tests to verify failure**

Run:

```bash
go test ./compiler ./cmd/wrela -run 'TestBuildWritesReportWhenRequested|TestRunAcceptsReportFlag' -v
```

Expected: FAIL because `ReportPath` and `--report` are not implemented.

- [ ] **Step 4: Implement report builder**

Add `compiler/sem/report.go`:

```go
package sem

import "github.com/ryanwible/wrela3/compiler/report"

func BuildImageReport(checked *CheckedProgram) report.ImageReport {
	r := report.ImageReport{Version: 1, Image: imageNameForReport(checked)}
	if checked == nil {
		return r
	}
	for _, root := range checked.ImageGraph.MemoryRoots {
		r.Memory.RootRegions = append(r.Memory.RootRegions, report.MemoryRootReport{
			Label: root.Label,
			Base:  root.Base,
			Bytes: root.Bytes,
		})
		r.Memory.TotalBytes += root.Bytes
		r.AuthorityAudit.MemoryRoots = append(r.AuthorityAudit.MemoryRoots, report.AuthorityRecord{
			Kind:       "memory_root",
			Label:      root.Label,
			Provenance: "firmware",
		})
	}
	for _, arena := range checked.ImageGraph.Arenas {
		r.Memory.Arenas = append(r.Memory.Arenas, report.ArenaReport{
			Label: arena.Label,
			Parent: arena.Parent,
			Base: arena.Base,
			Bytes: arena.Bytes,
			Owner: arena.Owner,
		})
		r.AuthorityAudit.Arenas = append(r.AuthorityAudit.Arenas, report.AuthorityRecord{
			Kind: "arena",
			Label: arena.Label,
			Owner: arena.Owner,
		})
	}
	return r
}
```

Implement `imageNameForReport` by returning the first `ImageDecl.Name`, or `"image"` when there is no image declaration.

- [ ] **Step 5: Run verification**

Run:

```bash
go test ./compiler/report ./compiler/sem ./compiler ./cmd/wrela -run 'TestImageReportJSONShape|TestBuildWritesReportWhenRequested|TestRunAcceptsReportFlag|TestArenaGraph' -v
git diff --check
```

Expected: all tests PASS.

- [ ] **Step 6: Commit**

```bash
git add compiler/build.go compiler/build_test.go cmd/wrela/main.go cmd/wrela/main_test.go compiler/sem/report.go compiler/sem/report_test.go
git commit -m "feat: emit production substrate image report -Codex Automated"
```

**Acceptance Criteria:** `wrela build` still writes EFI output; `--report` writes JSON; the report includes memory roots, arenas, and an authority audit skeleton.

---

## 6. Phase 2: Discovery Facts For Runtime Planning

**Description:** Expand discovery only where the runtime substrate consumes facts: multiple ECAM windows, PCI bridge walking, richer PCI facts, APIC/timer facts, CPU locality facts, and framebuffer fact shape.

**Phase Acceptance Criteria:**

- PCI enumeration scans all MCFG ECAM windows and bounded PCI bridges.
- PCI device identity includes revision, header type, BAR facts, MSI/MSI-X presence, interrupt pin/line, and bridge bus ranges.
- Discovery exposes APIC mode facts, enabled CPU facts, timer source facts, topology/locality facts with explicit unknown values, and framebuffer facts.
- Discovery capacity overflow boot-fatals instead of truncating silently.
- Discovery facts appear in the image report.

**Phase Code Example:**

```wrela
let discovery = PlatformDiscoveryRoot(panic = BootPanic()).from_uefi(hardware = hardware)
let topology = discovery.cpus.require_min_count(count = 2)
let timers = discovery.timers.require_periodic(period_us = 1000)
let fb = discovery.framebuffer.info()
```

### Task 5: Multiple ECAM Windows

**Description:** Replace single-window PCI enumeration with bounded multi-window enumeration and explicit capacity failure.

**Files:**
- Modify: `wrela/platform/acpi/mcfg.wrela`
- Modify: `wrela/machine/x86_64/pci.wrela`
- Modify: `compiler/sem/mcfg_parser_contract_test.go`
- Modify: `compiler/sem/pci_ecam_contract_test.go`

**Code Examples:**

Expand `PcieEcamWindows`:

```wrela
data PcieEcamWindows {
    count: U64
    window0: PcieEcamWindow
    window1: PcieEcamWindow
    window2: PcieEcamWindow
    window3: PcieEcamWindow
    panic: BootPanic

    fn at(self, index: U64) -> PcieEcamWindow {
        if index == 0 { return self.window0 }
        if index == 1 { return self.window1 }
        if index == 2 { return self.window2 }
        if index == 3 { return self.window3 }
        self.panic.fail(code = 0xAC060014)
    }

    fn append(self, window: PcieEcamWindow) {
        if self.count >= 4 {
            self.panic.fail(code = 0xAC060015)
        }
        if self.count == 0 { self.window0 = window }
        if self.count == 1 { self.window1 = window }
        if self.count == 2 { self.window2 = window }
        if self.count == 3 { self.window3 = window }
        self.count = self.count + 1
    }
}
```

Enumeration loop:

```wrela
fn enumerate(self) -> PciDeviceSet {
    if self.count == 0 {
        self.panic.fail(code = 0xAC060013)
    }
    let devices = PciDeviceSetBuilder(panic = self.panic).empty()
    let index = 0
    while index < self.count {
        devices.scan_window(window = self.at(index = index))
        index = index + 1
    }
    return devices
}
```

**Steps:**

- [ ] **Step 1: Add source-shape assertions**

In `compiler/sem/pci_ecam_contract_test.go`, assert these strings exist:

```go
for _, want := range []string{
	"window1: PcieEcamWindow",
	"window2: PcieEcamWindow",
	"window3: PcieEcamWindow",
	"self.panic.fail(code = 0xAC060015)",
	"devices.scan_window(window = self.at(index = index))",
} {
	if !strings.Contains(sourceText, want) {
		t.Fatalf("PCI ECAM source missing %q", want)
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
go test ./compiler/sem -run 'TestMcfgSyntheticEcamWindowContract|TestPciEcam' -v
```

Expected: FAIL because only `window0` exists.

- [ ] **Step 3: Implement bounded multi-window source**

Apply the `PcieEcamWindows` and `enumerate` changes. Move the old nested bus/device/function loop into:

```wrela
fn scan_window(self, window: PcieEcamWindow) {
    let bus = window.start_bus
    while bus <= window.end_bus {
        self.scan_bus(window = window, bus = bus)
        if bus == window.end_bus { return }
        bus = bus + 1
    }
}
```

- [ ] **Step 4: Run verification**

Run:

```bash
go test ./compiler/sem -run 'TestMcfgSyntheticEcamWindowContract|TestPciEcam|TestHardwareDiscoverySourceShape' -v
git diff --check
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add wrela/platform/acpi/mcfg.wrela wrela/machine/x86_64/pci.wrela compiler/sem/mcfg_parser_contract_test.go compiler/sem/pci_ecam_contract_test.go
git commit -m "feat: enumerate multiple PCI ECAM windows -Codex Automated"
```

**Acceptance Criteria:** Up to four ECAM windows are parsed and enumerated; overflow boot-fatals with code `0xAC060015`; no devices are silently dropped because of ignored MCFG windows.

### Task 6: PCI Bridge Walking And Runtime Facts

**Description:** Discover bridged buses and expose PCI facts needed for memory, interrupts, queues, and future drivers.

**Files:**
- Modify: `wrela/machine/x86_64/pci.wrela`
- Modify: `compiler/sem/pci_bar_contract_test.go`
- Create: `compiler/sem/pci_bridge_contract_test.go`

**Code Examples:**

Extend identity:

```wrela
data PciDeviceIdentity {
    segment: U16
    bus: U8
    device: U8
    function: U8
    vendor_id: U16
    device_id: U16
    class_code: U8
    subclass: U8
    prog_if: U8
    revision: U8
    header_type: U8
    interrupt_pin: U8
    interrupt_line: U8
}
```

Add bridge facts:

```wrela
data PciBridgeBusRange {
    primary: U8
    secondary: U8
    subordinate: U8
}

data PciFunctionFacts {
    identity: PciDeviceIdentity
    has_msi: Bool
    has_msix: Bool
    bridge_range: PciBridgeBusRange
}
```

Bridge scan:

```wrela
fn scan_function(self, window: PcieEcamWindow, bus: U8, device: U8, function: U8, depth: U64) {
    if depth > 8 {
        self.panic.fail(code = 0xAC060016)
    }
    let vendor_device = window.read_config32(bus = bus, device = device, function = function, offset = 0)
    if (vendor_device & 0xFFFF) == 0xFFFF {
        return
    }
    self.append(window = window, bus = bus, device = device, function = function)
    let class_reg = window.read_config32(bus = bus, device = device, function = function, offset = 8)
    let class_code = class_reg >> 24
    let subclass = (class_reg >> 16) & 0xFF
    if class_code == 0x06 {
        if subclass == 0x04 {
            let buses = window.read_config32(bus = bus, device = device, function = function, offset = 0x18)
            let secondary = (buses >> 8) & 0xFF
            let subordinate = (buses >> 16) & 0xFF
            let next = secondary
            while next <= subordinate {
                self.scan_bus_depth(window = window, bus = next, depth = depth + 1)
                if next == subordinate { return }
                next = next + 1
            }
        }
    }
}
```

**Steps:**

- [ ] **Step 1: Add bridge contract test**

Add:

```go
func TestPciBridgeWalkingSourceShape(t *testing.T) {
	sourceText := readRepoFile(t, "wrela/machine/x86_64/pci.wrela")
	for _, want := range []string{
		"data PciBridgeBusRange",
		"data PciFunctionFacts",
		"fn scan_function(self, window: PcieEcamWindow, bus: U8, device: U8, function: U8, depth: U64)",
		"self.panic.fail(code = 0xAC060016)",
		"offset = 0x18",
		"class_code == 0x06",
		"subclass == 0x04",
		"self.scan_bus_depth(window = window, bus = next, depth = depth + 1)",
	} {
		if !strings.Contains(sourceText, want) {
			t.Fatalf("PCI bridge source missing %q", want)
		}
	}
}
```

Add a synthetic bridge-walk algorithm test in the same file. This is a small Go mirror of the required Wrela algorithm; keep it in the test so the expected behavior is executable instead of only string-scanned:

```go
// Add reflect to compiler/sem/pci_bridge_contract_test.go imports.

func TestPciBridgeWalkSyntheticTopology(t *testing.T) {
	cfg := syntheticPciConfig{
		functions: map[pciBDF]syntheticFunction{
			{bus: 0, device: 1, function: 0}: {vendor: 0x1234, deviceID: 0x0001, class: 0x06, subclass: 0x04, secondary: 2, subordinate: 3},
			{bus: 2, device: 0, function: 0}: {vendor: 0x1234, deviceID: 0x0002, class: 0x02, subclass: 0x00},
			{bus: 3, device: 0, function: 0}: {vendor: 0x1234, deviceID: 0x0003, class: 0x01, subclass: 0x06},
		},
	}
	got := walkSyntheticPCI(cfg, 0, 0)
	want := []pciBDF{{bus: 0, device: 1, function: 0}, {bus: 2, device: 0, function: 0}, {bus: 3, device: 0, function: 0}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("walkSyntheticPCI() = %#v, want %#v", got, want)
	}
}

type pciBDF struct{ bus, device, function uint8 }
type syntheticFunction struct {
	vendor uint16
	deviceID uint16
	class uint8
	subclass uint8
	secondary uint8
	subordinate uint8
}
type syntheticPciConfig struct{ functions map[pciBDF]syntheticFunction }
```

Define `walkSyntheticPCI` in the test file with the same depth rule as Wrela:

```go
func walkSyntheticPCI(cfg syntheticPciConfig, startBus uint8, depth int) []pciBDF {
	if depth > 8 {
		panic("bridge depth exceeded")
	}
	var out []pciBDF
	for device := uint8(0); device < 32; device++ {
		bdf := pciBDF{bus: startBus, device: device, function: 0}
		fn, ok := cfg.functions[bdf]
		if !ok || fn.vendor == 0xffff {
			continue
		}
		out = append(out, bdf)
		if fn.class == 0x06 && fn.subclass == 0x04 {
			for bus := fn.secondary; bus <= fn.subordinate; bus++ {
				out = append(out, walkSyntheticPCI(cfg, bus, depth+1)...)
				if bus == fn.subordinate {
					break
				}
			}
		}
	}
	return out
}
```

- [ ] **Step 2: Add fact contract test**

Assert identity fields and capability facts:

```go
func TestPciRuntimeFactSourceShape(t *testing.T) {
	sourceText := readRepoFile(t, "wrela/machine/x86_64/pci.wrela")
	for _, want := range []string{
		"revision: U8",
		"header_type: U8",
		"interrupt_pin: U8",
		"interrupt_line: U8",
		"has_msi: Bool",
		"has_msix: Bool",
		"fn facts(self) -> PciFunctionFacts",
		"self.find_capability_optional(capability_id = 0x05)",
		"self.find_capability_optional(capability_id = 0x11)",
	} {
		if !strings.Contains(sourceText, want) {
			t.Fatalf("PCI facts source missing %q", want)
		}
	}
}
```

- [ ] **Step 3: Run tests to verify failure**

Run:

```bash
go test ./compiler/sem -run 'TestPciBridgeWalkingSourceShape|TestPciBridgeWalkSyntheticTopology|TestPciRuntimeFactSourceShape' -v
```

Expected: FAIL because bridge facts do not exist.

- [ ] **Step 4: Implement bridge walking and facts**

Implement the Wrela snippets exactly. Add `find_capability_optional` returning `0` when absent:

```wrela
fn find_capability_optional(self, capability_id: U8) -> U16 {
    let status = self.read_config32(offset = 0x04)
    if (status & 0x00100000) == 0 {
        return 0
    }
    let ptr = self.read_config32(offset = 0x34) & 0xFC
    let remaining = 48
    while remaining != 0 {
        if ptr == 0 { return 0 }
        let header = self.read_config32(offset = ptr)
        if (header & 0xFF) == capability_id { return ptr }
        ptr = (header >> 8) & 0xFC
        remaining = remaining - 1
    }
    return 0
}
```

- [ ] **Step 5: Run verification**

Run:

```bash
go test ./compiler/sem -run 'TestPciBridge|TestPciRuntime|TestPciBar|TestHardwareDiscoverySourceShape' -v
git diff --check
```

Expected: all tests PASS.

- [ ] **Step 6: Commit**

```bash
git add wrela/machine/x86_64/pci.wrela compiler/sem/pci_bar_contract_test.go compiler/sem/pci_bridge_contract_test.go
git commit -m "feat: expose PCI bridge and runtime facts -Codex Automated"
```

**Acceptance Criteria:** PCI enumeration reaches bridged buses with depth bound 8; device facts expose BAR/capability/interrupt/bridge data; bridge overflow boot-fatals with `0xAC060016`.

### Task 7: APIC, Timer, Topology, Locality, And Framebuffer Facts

**Description:** Add fact shapes that CPU placement, timer authority, wake strategy, and future framebuffer drivers can consume.

**Files:**
- Modify: `wrela/machine/x86_64/cpu_state.wrela`
- Modify: `wrela/machine/x86_64/interrupts.wrela`
- Create: `wrela/machine/x86_64/timer.wrela`
- Modify: `wrela/platform/hardware/discovery.wrela`
- Modify: `compiler/sem/hardware_discovery_test.go`
- Create: `compiler/sem/timer_authority_test.go`

**Code Examples:**

CPU and locality facts:

```wrela
data CpuLocalityFacts {
    logical_id: U32
    apic_id: U32
    x2apic_id: U32
    smt_group: U32
    core_group: U32
    package_group: U32
    llc_group: U32
    numa_node: U32
    known_mask: U64
}

data CpuDiscovery {
    enabled: EnabledCpuSet
    locality0: CpuLocalityFacts
    locality1: CpuLocalityFacts

    fn require_min_count(self, count: U64) -> CpuTopology {
        return self.enabled.require_count(count = count)
    }
}
```

APIC facts:

```wrela
data ApicModeFacts {
    xapic_available: Bool
    x2apic_available: Bool
    local_apic_base: PhysicalAddress
}

data ApicModeSelection {
    mode: U32
    required: Bool
}
```

Timer facts:

```wrela
data TimerDiscovery {
    local_apic_timer_available: Bool
    pit_available: Bool
    hpet_available: Bool
    hpet_base: PhysicalAddress
    panic: BootPanic

    fn require_periodic(self, period_us: U64) -> TimerAuthority {
        if self.local_apic_timer_available {
            if self.pit_available {
                return TimerAuthority(source = TimerSource(kind = 1), period_us = period_us, panic = self.panic)
            }
        }
        if self.hpet_available {
            return TimerAuthority(source = TimerSource(kind = 2), period_us = period_us, panic = self.panic)
        }
        self.panic.fail(code = 0xAC080001)
    }
}
```

Framebuffer facts:

```wrela
data FramebufferInfo {
    base: PhysicalAddress
    length: U64
    width: U32
    height: U32
    stride: U32
    format: U32
}
```

**Steps:**

- [ ] **Step 1: Add source-shape tests**

In `TestHardwareDiscoverySourceShape`, assert:

```go
for _, typ := range []struct{ module, name string }{
	{"machine.x86_64.cpu_state", "CpuLocalityFacts"},
	{"machine.x86_64.cpu_state", "CpuDiscovery"},
	{"machine.x86_64.interrupts", "ApicModeFacts"},
	{"machine.x86_64.timer", "TimerDiscovery"},
	{"machine.x86_64.timer", "TimerAuthority"},
	{"platform.hardware.discovery", "FramebufferInfo"},
} {
	_ = moduleType(t, index, typ.module, typ.name)
}
```

Add this timer priority source-shape test to `compiler/sem/timer_authority_test.go`:

```go
func TestTimerAuthorityPriorityOrder(t *testing.T) {
	sourceText := readRepoFile(t, "wrela/machine/x86_64/timer.wrela")
	local := strings.Index(sourceText, "TimerSource(kind = 1)")
	hpet := strings.Index(sourceText, "TimerSource(kind = 2)")
	fatal := strings.Index(sourceText, "self.panic.fail(code = 0xAC080001)")
	if local < 0 || hpet < 0 || fatal < 0 {
		t.Fatalf("timer source must include local APIC/PIT, HPET fact path, and boot fatal")
	}
	if !(local < hpet && hpet < fatal) {
		t.Fatalf("timer priority order must be local APIC/PIT, HPET, boot fatal")
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
go test ./compiler/sem -run 'TestHardwareDiscoverySourceShape|TestTimerAuthoritySourceShape|TestTimerAuthorityPriorityOrder' -v
```

Expected: FAIL because these types do not exist.

- [ ] **Step 3: Implement source facts**

Add the Wrela types and wire `DiscoveredHardware`:

```wrela
data DiscoveredHardware {
    memory: UefiMemoryMap
    acpi: AcpiRoot
    interrupts: InterruptAuthority
    pci: PciDeviceSet
    cpus: CpuDiscovery
    timers: TimerDiscovery
    framebuffer: FramebufferInfo
    panic: BootPanic
}
```

For unknown locality, set `known_mask = 0` and all group fields to `0`. Do not infer SMT, package, LLC, NUMA, or device locality from QEMU-specific constants.

- [ ] **Step 4: Run verification**

Run:

```bash
go test ./compiler/sem -run 'TestHardwareDiscoverySourceShape|TestTimerAuthoritySourceShape|TestTimerAuthorityPriorityOrder|TestMadt|TestAcpi' -v
git diff --check
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add wrela/machine/x86_64/cpu_state.wrela wrela/machine/x86_64/interrupts.wrela wrela/machine/x86_64/timer.wrela wrela/platform/hardware/discovery.wrela compiler/sem/hardware_discovery_test.go compiler/sem/timer_authority_test.go
git commit -m "feat: expose runtime discovery facts -Codex Automated"
```

**Acceptance Criteria:** Discovery exposes CPU, APIC, timer, locality, and framebuffer facts; unknown locality is explicit; timer priority is local APIC timer calibrated by PIT, then HPET fact path, then boot fatal.

### Task 8: Discovery Report Integration

**Description:** Carry discovery facts into the JSON report so placement and runtime decisions are auditable.

**Files:**
- Modify: `compiler/sem/image_graph.go`
- Modify: `compiler/sem/check.go`
- Modify: `compiler/sem/report.go`
- Modify: `compiler/sem/report_test.go`
- Modify: `compiler/sem/hardware_discovery_test.go`
- Modify: `wrela/platform/hardware/discovery.wrela`

**Code Examples:**

Add these graph nodes to `compiler/sem/image_graph.go` before writing the report mapping:

```go
type APICFactNode struct {
	Mode string
	XAPICAvailable bool
	X2APICAvailable bool
	Span source.Span
}

type TimerFactNode struct {
	Label string
	Source string
	PeriodUS uint64
	Span source.Span
}

type LocalityFactNode struct {
	Subject string
	Kind string
	Value string
	Known bool
	Span source.Span
}

type FramebufferFactNode struct {
	Base uint64
	Bytes uint64
	Width uint32
	Height uint32
	Stride uint32
	Format uint32
	Span source.Span
}
```

Extend `ImageGraph`:

```go
APICFacts []APICFactNode
TimerFacts []TimerFactNode
LocalityFacts []LocalityFactNode
FramebufferFacts []FramebufferFactNode
```

Report builder additions:

```go
func appendDiscoveryFacts(r *report.ImageReport, g ImageGraph) {
	for _, claim := range g.HardwareClaims {
		r.AuthorityAudit.HardwareClaims = append(r.AuthorityAudit.HardwareClaims, report.AuthorityRecord{
			Kind:  claim.Kind,
			Label: claim.Key,
			Owner: "delegated_hardware",
		})
	}
	for _, fact := range g.APICFacts {
		r.Hardware.APIC.Mode = fact.Mode
	}
	for _, timer := range g.TimerFacts {
		r.Hardware.Timers = append(r.Hardware.Timers, report.TimerReport{
			Label: timer.Label,
			Source: timer.Source,
			PeriodUS: timer.PeriodUS,
		})
	}
	for _, locality := range g.LocalityFacts {
		r.Hardware.Locality = append(r.Hardware.Locality, report.LocalityReport{
			Subject: locality.Subject,
			Kind: locality.Kind,
			Value: locality.Value,
			Known: locality.Known,
		})
	}
}
```

Wire the helper into `BuildImageReport` immediately after the existing arena loop:

```go
func BuildImageReport(checked *CheckedProgram) report.ImageReport {
	r := report.ImageReport{Version: 1, Image: imageNameForReport(checked)}
	if checked == nil {
		return r
	}
	// existing memory root loop
	// existing arena loop
	appendDiscoveryFacts(&r, checked.ImageGraph)
	return r
}
```

Populate the graph from real source expressions in `compiler/sem/check.go`. Add the field-access hook for `discovery.interrupts.local_apic`, then add a call from the existing expression/call checker after callee and argument types are known:

```go
func (c *checker) recordDiscoveryFactFromField(sel *ast.SelectorExpr, recvType *Type) {
	if recvType == nil {
		return
	}
	if recvType.Module == "machine.x86_64.interrupts" && recvType.Name == "InterruptAuthority" && sel.Field == "local_apic" {
		c.graph.APICFacts = append(c.graph.APICFacts, APICFactNode{
			Mode: "xapic_fallback",
			XAPICAvailable: true,
			X2APICAvailable: false,
			Span: sel.SpanV,
		})
	}
}

func (c *checker) recordDiscoveryFactFromCall(call *ast.CallExpr, recvType *Type, args map[string]constValue) {
	if recvType == nil {
		return
	}
	if recvType.Module == "machine.x86_64.timer" && recvType.Name == "TimerDiscovery" && call.Method == "require_periodic" {
		period := uint64(0)
		if v, ok := args["period_us"].asUint(); ok {
			period = v
		}
		c.graph.TimerFacts = append(c.graph.TimerFacts, TimerFactNode{
			Label: fmt.Sprintf("periodic.%dus", period),
			Source: "local_apic_pit_calibrated",
			PeriodUS: period,
			Span: call.SpanV,
		})
	}
	if recvType.Module == "machine.x86_64.cpu_state" && recvType.Name == "CpuPlacementPlan" && call.Method == "cpu_for" {
		c.graph.LocalityFacts = append(c.graph.LocalityFacts, LocalityFactNode{
			Subject: "executor",
			Kind: "cpu_locality",
			Value: "unknown",
			Known: false,
			Span: call.SpanV,
		})
	}
	if recvType.Module == "platform.hardware.discovery" && recvType.Name == "FramebufferDiscovery" && call.Method == "require_framebuffer" {
		c.graph.FramebufferFacts = append(c.graph.FramebufferFacts, FramebufferFactNode{Span: call.SpanV})
	}
}
```

Use the checker's existing constant-literal helper for `args["period_us"]`; if it is named differently in the current code, add this local adapter beside the new call recorder:

```go
type constValue struct {
	Uint uint64
	String string
	Known bool
	Fields map[string]constValue
}

func (v constValue) asUint() (uint64, bool) {
	return v.Uint, v.Known
}

func (v constValue) asString() (string, bool) {
	return v.String, v.Known
}
```

Expected report fields:

```json
"hardware": {
  "apic": { "mode": "xapic_fallback" },
  "timers": [{ "label": "periodic.1000us", "source": "local_apic_pit_calibrated", "period_us": 1000 }],
  "locality": [{ "subject": "cpu0", "kind": "numa_node", "value": "0", "known": false }]
}
```

**Steps:**

- [ ] **Step 1: Add report test**

Add:

```go
func TestImageReportIncludesDiscoveryFacts(t *testing.T) {
	checked := &CheckedProgram{ImageGraph: ImageGraph{
		HardwareClaims: []HardwareClaimNode{{Kind: "pci_bar", Key: "edu.bar0"}},
		APICFacts: []APICFactNode{{Mode: "xapic_fallback"}},
		TimerFacts: []TimerFactNode{{Label: "periodic.1000us", Source: "local_apic_pit_calibrated", PeriodUS: 1000}},
		LocalityFacts: []LocalityFactNode{{Subject: "cpu0", Kind: "numa_node", Value: "0", Known: false}},
	}}
	r := BuildImageReport(checked)
	if len(r.AuthorityAudit.HardwareClaims) != 1 {
		t.Fatalf("hardware claims missing from report: %#v", r)
	}
	if r.Hardware.APIC.Mode != "xapic_fallback" {
		t.Fatalf("APIC mode missing from report: %#v", r.Hardware.APIC)
	}
	if len(r.Hardware.Timers) != 1 || r.Hardware.Timers[0].Source != "local_apic_pit_calibrated" {
		t.Fatalf("timer facts missing from report: %#v", r.Hardware.Timers)
	}
	if len(r.Hardware.Locality) != 1 || r.Hardware.Locality[0].Known {
		t.Fatalf("unknown locality fact missing from report: %#v", r.Hardware.Locality)
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run:

```bash
go test ./compiler/sem -run TestImageReportIncludesDiscoveryFacts -v
```

Expected: FAIL because report hardware claim mapping is absent.

- [ ] **Step 3: Add source extraction and report mapping**

Add the four graph node slices, call `recordDiscoveryFactFromCall(...)` from the call checker, add `appendDiscoveryFacts(...)`, and call `appendDiscoveryFacts(&r, checked.ImageGraph)` in `BuildImageReport` immediately after the arena loop. If a fact is unknown, emit `Known: false` instead of omitting it. Task 9 may later update APIC mode facts from explicit `select_apic_mode()` calls; Task 8's required default is `"xapic_fallback"` for images that use the discovered local APIC authority before explicit APIC mode selection exists.

- [ ] **Step 4: Run verification**

Run:

```bash
go test ./compiler/sem -run 'TestImageReportIncludesDiscoveryFacts|TestHardwareDiscoverySourceShape' -v
git diff --check
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add compiler/sem/image_graph.go compiler/sem/check.go compiler/sem/report.go compiler/sem/report_test.go compiler/sem/hardware_discovery_test.go wrela/platform/hardware/discovery.wrela
git commit -m "feat: report runtime discovery facts -Codex Automated"
```

**Acceptance Criteria:** The report includes hardware claims, APIC mode facts, timer facts, and explicit unknown locality facts; no report consumer must infer missing facts.

---

## 7. Phase 3: CPU, Timers, And Interrupt Queues

**Description:** Make CPU/APIC mode, AP startup, timers, shared interrupts, and interrupt queues production-shaped and bounded. Timers and interrupts enter executor-owned queues/topics; no hidden callbacks are added.

**Phase Acceptance Criteria:**

- x2APIC selection has xAPIC fallback or explicit required-mode boot fatal.
- AP startup has a documented low-page trampoline contract and backend tests that enforce it.
- At least one periodic timer source publishes typed timer ticks to an executor subscription.
- Shared IRQ source claiming is source-visible and preserves one hardware route owner.
- Interrupt queues are backed by arena memory, have bounded capacity, and expose overflow policy.

**Phase Code Example:**

```wrela
let apic = discovery.interrupts.select_apic_mode().with_xapic_fallback()
let timer = discovery.timers.require_periodic(period_us = 1000)
let ticks = timer.subscribe(subscriber = worker_slot)
let irq11 = discovery.interrupts.route_shared_irq(irq = 11, vector = InterruptVector(value = 0x43))
let nic_irq = irq11.claim_source(identity = InterruptSourceIdentity(label = "nic0"))
```

### Task 9: APIC Mode Selection With xAPIC Fallback

**Description:** Add source and backend support for x2APIC when available while keeping xAPIC as the default fallback path.

**Files:**
- Modify: `wrela/machine/x86_64/interrupts.wrela`
- Create: `compiler/codegen/apic_mode.go`
- Create: `compiler/codegen/apic_mode_test.go`
- Modify: `compiler/codegen/lapic.go`
- Modify: `compiler/sem/hardware_discovery_test.go`

**Code Examples:**

Source API:

```wrela
data ApicModeSelection {
    mode: U32
    required: Bool

    fn with_xapic_fallback(self) -> ApicModeSelection {
        if self.mode == 2 {
            return self
        }
        return ApicModeSelection(mode = 1, required = false)
    }
}

data InterruptAuthority {
    local_apic: LocalApic
    io_apics: IoApicSet
    overrides: InterruptOverrideSet
    apic_facts: ApicModeFacts
    panic: BootPanic

    fn select_apic_mode(self) -> ApicModeSelection {
        if self.apic_facts.x2apic_available {
            return ApicModeSelection(mode = 2, required = false)
        }
        if self.apic_facts.xapic_available {
            return ApicModeSelection(mode = 1, required = false)
        }
        self.panic.fail(code = 0xAC050010)
    }

    fn require_x2apic(self) -> ApicModeSelection {
        if self.apic_facts.x2apic_available == false {
            self.panic.fail(code = 0xAC050011)
        }
        return ApicModeSelection(mode = 2, required = true)
    }
}
```

x2APIC backend test:

```go
// Add github.com/ryanwible/wrela3/compiler/asm to compiler/codegen/apic_mode_test.go imports.

func compileX2APICWriteUnitForTest(msr uint32, value uint64) compiledUnit {
	e := newEmitter(compileContext{})
	emitMovImmToReg(e, asm.MustLookup("r10"), int64(value))
	emitX2APICWriteMSR(e, msr, asm.MustLookup("r10"))
	return compiledUnit{Symbol: "x2apic_write_test", Bytes: e.Code}
}

func TestX2APICWriteUsesWrmsr(t *testing.T) {
	unit := compileX2APICWriteUnitForTest(0x80B, 0)
	for _, want := range [][]byte{
		{0x0F, 0x30}, // wrmsr
	} {
		if !bytes.Contains(unit.Bytes, want) {
			t.Fatalf("x2APIC write missing %x in %x", want, unit.Bytes)
		}
	}
}
```

**Steps:**

- [ ] **Step 1: Add source-shape and codegen tests**

Add `TestX2APICWriteUsesWrmsr` and assert source contains `select_apic_mode`, `require_x2apic`, `with_xapic_fallback`, and panic codes `0xAC050010` and `0xAC050011`.

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
go test ./compiler/codegen ./compiler/sem -run 'TestX2APICWriteUsesWrmsr|TestHardwareDiscoverySourceShape' -v
```

Expected: FAIL because x2APIC mode support is not present.

- [ ] **Step 3: Implement x2APIC write helper**

Add:

```go
func emitX2APICWriteMSR(e *Emitter, msr uint32, valueReg asm.Reg) {
	emitMovImmToReg(e, asm.MustLookup("rcx"), int64(msr))
	emitRegRegMove(e, asm.MustLookup("rax"), valueReg)
	emitMovImmToReg(e, asm.MustLookup("rdx"), 0)
	e.emit(0x0F, 0x30)
}
```

Keep all existing xAPIC MMIO helpers unchanged.

- [ ] **Step 4: Run verification**

Run:

```bash
go test ./compiler/codegen ./compiler/sem -run 'TestX2APIC|TestHardwareDiscoverySourceShape' -v
git diff --check
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add wrela/machine/x86_64/interrupts.wrela compiler/codegen/apic_mode.go compiler/codegen/apic_mode_test.go compiler/codegen/lapic.go compiler/sem/hardware_discovery_test.go
git commit -m "feat: add x2apic selection with xapic fallback -Codex Automated"
```

**Acceptance Criteria:** Runtime source can select x2APIC or xAPIC; `require_x2apic` boot-fatals when unavailable; backend has tested `wrmsr` emission; existing xAPIC paths still compile.

### Task 10: AP Startup Low-Page Contract

**Description:** Document and enforce the AP startup low-page trampoline contract instead of relying on implicit q35 assumptions.

**Files:**
- Create: `docs/runtime/ap-startup-contract.md`
- Modify: `compiler/codegen/vcpu_start.go`
- Modify: `compiler/codegen/vcpu_start_test.go`

**Code Examples:**

Contract text:

```markdown
# AP Startup Contract

The bootstrap processor starts application processors through a real-mode SIPI trampoline.

Required invariants:

- `apTrampolineBase` is `0x8000`.
- The trampoline page range is `[0x8000, 0x9000)`.
- The range is below `0x100000`.
- The range is 4 KiB aligned.
- The range is identity mapped before SIPI.
- The trampoline handoff slots are patched before INIT-SIPI-SIPI.
- The backend copies `_wrela_ap_trampoline_blob` into the trampoline page before AP startup.
- AP ready polling is bounded by `apStartupReadyPollLimit = 10_000_000`.
```

Backend constants:

```go
const apStartupReadyPollLimit = 10_000_000

func validateAPStartupContract() []diag.Diagnostic {
	if apTrampolineBase >= 0x100000 {
		return []diag.Diagnostic{{Phase: diagnosticPhase, Code: diag.SEM0074, Message: "AP trampoline must be below 1 MiB"}}
	}
	if apTrampolineBase%4096 != 0 {
		return []diag.Diagnostic{{Phase: diagnosticPhase, Code: diag.SEM0074, Message: "AP trampoline must be 4 KiB aligned"}}
	}
	return nil
}
```

Bounded ready wait:

```go
func emitWaitForVcpuReady(e *Emitter, vcpuID int) {
	loop := e.newLabel("vcpu_ready")
	done := e.newLabel("vcpu_ready_done")
	timeout := e.newLabel("vcpu_ready_timeout")
	emitMovImmToReg(e, asm.MustLookup("rcx"), apStartupReadyPollLimit)
	emitMovDataAddressToReg(e, asm.MustLookup("rax"), fmt.Sprintf("_wrela_vcpu%d_ready", vcpuID))
	e.bindLabel(loop)
	emitLoadMemToReg(e, asm.MustLookup("r10"), asm.MustLookup("rax"), 0, 64)
	emitCmpRegImm(e, asm.MustLookup("r10"), 1)
	e.emitJcc(0x84, done)
	emitSubImm(e, asm.MustLookup("rcx"), 1)
	emitCmpRegImm(e, asm.MustLookup("rcx"), 0)
	e.emitJcc(0x84, timeout)
	e.emitInstruction(asm.Instruction{Mnemonic: "pause"})
	e.emitJmp(loop)
	e.bindLabel(timeout)
	emitCallReloc(e, "_wrela_ap_startup_timeout")
	e.bindLabel(done)
}
```

**Steps:**

- [ ] **Step 1: Add tests**

Add:

```go
func TestAPStartupContractConstants(t *testing.T) {
	if apTrampolineBase != 0x8000 {
		t.Fatalf("apTrampolineBase = %#x, want 0x8000", apTrampolineBase)
	}
	if apTrampolineBase%4096 != 0 || apTrampolineBase >= 0x100000 {
		t.Fatalf("AP trampoline must be 4 KiB aligned below 1 MiB")
	}
	if len(validateAPStartupContract()) != 0 {
		t.Fatalf("AP startup contract diagnostics: %#v", validateAPStartupContract())
	}
}

func TestAPReadyWaitIsBounded(t *testing.T) {
	unit := compileVcpuStartForTest(t)
	code := unit.Bytes
	if !bytes.Contains(code, encodeImm32ForTest(apStartupReadyPollLimit)) {
		t.Fatalf("AP ready wait missing poll limit %d in %x", apStartupReadyPollLimit, code)
	}
	found := false
	for _, reloc := range unit.CallReloc {
		if reloc.Symbol == "_wrela_ap_startup_timeout" {
			found = true
		}
	}
	if !found {
		t.Fatalf("AP ready wait must call timeout trap, relocs = %#v", unit.CallReloc)
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
go test ./compiler/codegen -run 'TestAPStartupContractConstants|TestAPReadyWaitIsBounded' -v
```

Expected: FAIL because the bounded ready wait and contract validator are absent.

- [ ] **Step 3: Implement contract validator and bounded wait**

Add `validateAPStartupContract()` to `Compile()` before unit compilation:

```go
if ds := validateAPStartupContract(); len(ds) != 0 {
	return nil, ds
}
```

Replace the current fixed delay constant:

```go
const apStartupDelayLoopCount = 70_000
```

with the bounded ready-poll constant:

```go
const apStartupReadyPollLimit = 10_000_000
```

Remove the two fixed delay call sites in `emitVcpuStart`:

```go
emitDelayLoop(e, apStartupDelayLoopCount)
```

They currently appear after `emitSendIcr(..., apicICRInitAssert)` and after the first SIPI send. Delete both lines. Do not replace them with another fixed-count sleep; this task moves the enforceable wait to the bounded ready-poll in `emitWaitForVcpuReady`.

After this task, `rg -n "apStartupDelayLoopCount" compiler/codegen` must report no matches.

Add a small trap unit:

```go
func compileAPStartupTimeoutTrapUnit() compiledUnit {
	e := newEmitter(compileContext{})
	e.emitInstruction(asm.Instruction{Mnemonic: "cli"})
	emitHltLoop(e)
	return compiledUnit{Symbol: "_wrela_ap_startup_timeout", Bytes: e.Code}
}
```

- [ ] **Step 4: Add contract doc**

Create `docs/runtime/ap-startup-contract.md` exactly as shown, with a final section:

```markdown
## Out Of Scope

This contract does not implement high-CR3 trampoline relocation, higher-half AP entry, or hardware-specific calibrated SIPI delays.
```

- [ ] **Step 5: Run verification**

Run:

```bash
go test ./compiler/codegen -run 'TestAP' -v
rg -n '0x8000|below `0x100000`|apStartupReadyPollLimit' docs/runtime/ap-startup-contract.md compiler/codegen/vcpu_start.go
! rg -n 'apStartupDelayLoopCount' compiler/codegen
git diff --check
```

Expected: tests PASS; first `rg` prints matching contract lines; second `rg` reports no matches.

- [ ] **Step 6: Commit**

```bash
git add docs/runtime/ap-startup-contract.md compiler/codegen/vcpu_start.go compiler/codegen/vcpu_start_test.go
git commit -m "feat: enforce AP startup trampoline contract -Codex Automated"
```

**Acceptance Criteria:** AP trampoline placement is documented and enforced; AP ready polling is bounded; failure enters a trap instead of spinning forever.

### Task 11: Timer Authority And Periodic Tick Topic

**Description:** Introduce source-visible timer authority that publishes timer events into the same topic/wake system as device interrupts.

**Precondition:** Task 16 must already be committed. Task 16 creates `wrela/machine/x86_64/topic_payload.wrela` and the typed `TopicLayout` payload fields that Task 11 consumes.

**Files:**
- Modify: `wrela/machine/x86_64/timer.wrela`
- Modify: `wrela/machine/x86_64/topic_payload.wrela`
- Create: `compiler/ir/timer_test.go`
- Modify: `compiler/ir/lower.go`
- Create: `compiler/codegen/timer.go`
- Create: `compiler/codegen/timer_test.go`
- Modify: `compiler/codegen/program.go`
- Modify: `compiler/codegen/interrupt_test.go`

**Code Examples:**

Source timer topic:

```wrela
module machine.x86_64.timer

use { ExecutorSlot } from machine.x86_64.cpu_state
use { TopicIdentity } from machine.x86_64.topic_u64
use { TimerTickPayload, TimerTickTopic, TimerTickSubscription } from machine.x86_64.topic_payload
use { BootPanic } from platform.hardware.panic

data TimerSource {
    kind: U32
}

class TimerAuthority {
    source: TimerSource
    period_us: U64
    panic: BootPanic

    fn subscribe(self, subscriber: ExecutorSlot) -> TimerTickSubscription {
        let topic = TimerTickTopic(identity = TopicIdentity(label = "timer.periodic"), id = 3, depth = 64)
        return topic.subscribe(subscriber = subscriber)
    }
}
```

Timer interrupt vector is fixed at `0x43` for this milestone. Add this case to `interruptVectorSymbol` in `compiler/codegen/program.go`:

```go
case 0x43:
	return "_wrela_interrupt_vector43_timer"
```

**Steps:**

- [ ] **Step 1: Add IR timer lowering test**

Add:

```go
const productionTimerSourceForTest = `
module examples.production_timer
use { BootPanic } from platform.hardware.panic
use { PlatformDiscoveryRoot } from platform.hardware.discovery
use { DelegatedHardware } from platform.uefi.transition
use { OwnedHardware, OwnedMemory, IoPortAuthority, MemoryPlan, CpuPlan, HardwarePlan, InterruptRoutingPlan, ClaimedPciPlanBuilder, SlotIdentity } from machine.x86_64.cpu_state
use { MutableBytes, Bytes } from machine.x86_64.executor_memory
use { InterruptVector } from machine.x86_64.interrupts
image TimerImage {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        let panic = BootPanic()
        let discovery = PlatformDiscoveryRoot(panic = panic).from_uefi(hardware = hardware)
        let worker = hardware.executors.claim(identity = SlotIdentity(label = "worker"))
        let timer = discovery.timers.require_periodic(period_us = 1000)
        let ticks = timer.subscribe(subscriber = worker)
        let arena = MutableBytes(address = 0, length = 0)
        return hardware.exit_to_owned_hardware(memory_plan = MemoryPlan(owned_memory = OwnedMemory(arena = arena), executor_arena = arena, io_ports = IoPortAuthority()), cpu_plan = CpuPlan(owned_stack_top = 0, gdt_descriptor = Bytes(address = 0, length = 0), idt_descriptor = Bytes(address = 0, length = 0), cr3 = 0), hardware_plan = HardwarePlan(cpus = discovery.cpus.require_min_count(count = 2), interrupts = InterruptRoutingPlan(local_apic = discovery.interrupts.local_apic, serial_irq4 = discovery.interrupts.route_isa_irq(irq = 4, vector = InterruptVector(value = 0x40))), pci = ClaimedPciPlanBuilder(panic = panic).empty()))
    }
    phase owned_hardware(hardware: OwnedHardware) -> never { while true {} }
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
```

- [ ] **Step 2: Add codegen timer test**

Add:

```go
func timerProgramForCodegenTest(t *testing.T) *ir.Program {
	t.Helper()
	return &ir.Program{
		Types: map[string]ir.TypeInfo{},
		Topics: []ir.TopicLayout{{
			Label: "timer.periodic",
			Kind: "timer_tick",
			Depth: 64,
			PayloadType: ir.Type{Name: "TimerTickPayload", Module: "machine.x86_64.topic_payload", Kind: ir.TypeKindData},
			PayloadSize: 24,
			PayloadAlign: 8,
			Subscribers: []string{"worker"},
		}},
		Timers: []ir.TimerRoute{{
			Label: "periodic.1000us",
			Source: "local_apic_pit_calibrated",
			PeriodUS: 1000,
			Vector: 0x43,
			SubscriberSlots: []string{"worker"},
		}},
		VcpuStarts: []ir.VcpuStartPlan{{VcpuID: 1, APICID: 1, SlotLabel: "worker"}},
	}
}

func TestTimerVectorPublishesTickTopic(t *testing.T) {
	program := timerProgramForCodegenTest(t)
	img, ds := Compile(program)
	if len(ds) != 0 {
		t.Fatalf("Compile diagnostics: %#v", ds)
	}
	code := symbolBytes(t, img, "_wrela_interrupt_vector43_timer")
	if !containsBytes(code, []byte{0x48, 0xCF}) {
		t.Fatalf("timer vector missing iretq: %x", code)
	}
	if !containsBytes(code, []byte{0xB0, 0x00}) {
		t.Fatalf("timer vector must EOI local APIC before return: %x", code)
	}
}
```

- [ ] **Step 3: Run tests to verify failure**

Run:

```bash
go test ./compiler/ir ./compiler/codegen -run 'TestTimerSubscribeLowersToTimerTopic|TestTimerVectorPublishesTickTopic' -v
```

Expected: FAIL because timer IR/codegen does not exist.

- [ ] **Step 4: Implement timer IR and codegen**

Add:

```go
type TimerRoute struct {
	Label string
	Source string
	PeriodUS uint64
	Vector uint8
	SubscriberSlots []string
}
```

to `ir.Program`, lower `TimerAuthority.subscribe(...)`, and compile vector `0x43` using the same topic publication path as device interrupts.

- [ ] **Step 5: Run verification**

Run:

```bash
go test ./compiler/ir ./compiler/codegen -run 'TestTimer|TestInterruptDispatch' -v
git diff --check
```

Expected: all tests PASS.

- [ ] **Step 6: Commit**

```bash
git add wrela/machine/x86_64/timer.wrela wrela/machine/x86_64/topic_payload.wrela compiler/ir/timer_test.go compiler/ir/lower.go compiler/codegen/timer.go compiler/codegen/timer_test.go compiler/codegen/program.go compiler/codegen/interrupt_test.go
git commit -m "feat: publish periodic timer ticks through topics -Codex Automated"
```

**Acceptance Criteria:** Timer authority is source-visible; periodic ticks route through vector `0x43`; timer events use topic/wake infrastructure; no hidden timer callback API is added.

### Task 12: Shared Interrupt Routes And Bounded Interrupt Queues

**Description:** Add source APIs and semantic checks for one hardware route with multiple software source claims, backed by bounded executor-owned queues.

**Files:**
- Create: `wrela/machine/x86_64/interrupt_queue.wrela`
- Modify: `wrela/machine/x86_64/interrupts.wrela`
- Create: `compiler/sem/interrupt_queue_test.go`
- Modify: `compiler/sem/hardware_claim_test.go`
- Modify: `compiler/sem/image_graph.go`
- Modify: `compiler/sem/check.go`

**Code Examples:**

Source API:

```wrela
data InterruptSourceIdentity {
    label: StringLiteral
}

data SharedInterruptSource {
    route: SharedIrqRoute
    identity: InterruptSourceIdentity
}

class SharedIrqRoute {
    route: IoApicRoute
    vector: InterruptVector

    fn claim_source(self, identity: InterruptSourceIdentity) -> SharedInterruptSource {
        return SharedInterruptSource(route = self, identity = identity)
    }
}

fn route_shared_irq(self, irq: U8, vector: InterruptVector) -> SharedIrqRoute {
    let route = self.route_isa_irq(irq = irq, vector = vector)
    return SharedIrqRoute(route = route, vector = vector)
}
```

Queue API:

```wrela
data InterruptOverflowPolicy {
    mode: U64
}
```

Construct overflow policies directly:

```wrela
let drop_newest = InterruptOverflowPolicy(mode = 0)
let drop_oldest = InterruptOverflowPolicy(mode = 1)
let set_flag = InterruptOverflowPolicy(mode = 2)
let fatal = InterruptOverflowPolicy(mode = 3)
```

**Steps:**

- [ ] **Step 1: Add semantic tests**

Add:

```go
func TestInterruptQueueRequiresExplicitOverflowPolicy(t *testing.T) {
	_, ds := checkUEFIModulesWithExtraSource(t, "missing-overflow-policy.wrela", missingOverflowPolicySource)
	if !hasCode(ds, diag.SEM0060) {
		t.Fatalf("expected SEM0060, got %#v", ds)
	}
}

func TestSharedInterruptAllowsMultipleSourceClaims(t *testing.T) {
	_, ds := checkUEFIModulesWithExtraSource(t, "shared-irq-good.wrela", sharedIRQSource)
	if len(ds) != 0 {
		t.Fatalf("shared IRQ diagnostics: %#v", ds)
	}
}

func TestDuplicateSharedInterruptSourceRejected(t *testing.T) {
	_, ds := checkUEFIModulesWithExtraSource(t, "shared-irq-duplicate.wrela", duplicateSharedIRQSource)
	if !hasCode(ds, diag.SEM0062) {
		t.Fatalf("expected SEM0062, got %#v", ds)
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
go test ./compiler/sem -run 'TestInterruptQueue|TestSharedInterrupt' -v
```

Expected: FAIL because queue and shared IRQ source APIs are absent.

- [ ] **Step 3: Implement source contracts**

Create `interrupt_queue.wrela` with the queue and overflow policy types. Add `RootArena.interrupt_queue(...)` and `ChildArena.interrupt_queue(...)` methods in `platform.hardware.memory`.

```wrela
fn interrupt_queue(self, identity: QueueIdentity, owner: ExecutorSlot, capacity: U64, payload: InterruptPayloadKind, overflow: InterruptOverflowPolicy) -> InterruptQueue {
    if capacity == 0 {
        self.region.panic.fail(code = 0xAC070001)
    }
    if payload.size == 0 {
        self.region.panic.fail(code = 0xAC070001)
    }
    if payload.align == 0 {
        self.region.panic.fail(code = 0xAC070001)
    }
    if capacity > (0 - 1) / payload.size {
        self.region.panic.fail(code = 0xAC070001)
    }
    let bytes = capacity * payload.size
    let child = self.child(identity = ArenaIdentity(label = identity.label), length = bytes, align = payload.align)
    let storage = MutableBytes(address = child.base, length = child.length)
    return InterruptQueue(identity = identity, owner = owner, storage = storage, capacity = capacity, payload = payload, overflow = overflow, head = 0, tail = 0, overflowed = false)
}
```

Use the same allocation and overflow checks for `RootArena` and `ChildArena`; `ChildArena` routes the guard failures through `self.root.region.panic`.

- [ ] **Step 4: Implement semantic extraction**

Record:

```go
type InterruptQueueNode struct {
	Label string
	Owner string
	Capacity uint64
	PayloadKind string
	Overflow string
	Span source.Span
}

type SharedInterruptSourceNode struct {
	RouteKey string
	SourceLabel string
	Vector int
	Span source.Span
}
```

Extend `ImageGraph`:

```go
InterruptQueues []InterruptQueueNode
SharedInterruptSources []SharedInterruptSourceNode
```

Define `RouteKey` exactly as:

```go
func sharedIRQRouteKey(irq uint64, vector uint64) string {
	return fmt.Sprintf("isa_irq:%d/vector:0x%02x", irq, vector)
}
```

Reject duplicate `(RouteKey, SourceLabel)` with SEM0062 and missing overflow argument with SEM0060.

Recognize the source calls in the expression checker using receiver type plus method name, not by string-scanning source text:

```go
func (c *checker) recordSharedInterruptCall(call *ast.CallExpr, recvType *Type, resultBinding string, args map[string]constValue) {
	if recvType == nil {
		return
	}
	if recvType.Module == "machine.x86_64.interrupts" && recvType.Name == "InterruptAuthority" && call.Method == "route_shared_irq" {
		irq, irqOK := args["irq"].asUint()
		vector, vectorOK := args["vector"].fieldUint("value")
		if irqOK && vectorOK {
			routeKey := sharedIRQRouteKey(irq, vector)
			c.sharedIRQRoutes[resultBinding] = routeKey
			c.sharedIRQVectors[routeKey] = int(vector)
		}
		return
	}
	if recvType.Module == "machine.x86_64.interrupts" && recvType.Name == "SharedIrqRoute" && call.Method == "claim_source" {
		sourceLabel, labelOK := args["identity"].fieldString("label")
		routeKey := c.routeKeyForReceiver(call.Receiver)
		if labelOK && routeKey != "" {
			node := SharedInterruptSourceNode{
				RouteKey: routeKey,
				SourceLabel: sourceLabel,
				Vector: c.sharedIRQVectors[routeKey],
				Span: call.SpanV,
			}
			if c.seenSharedIRQSource[routeKey+"|"+sourceLabel] {
				c.error(call.SpanV, diag.SEM0062, "duplicate shared interrupt source "+sourceLabel)
				return
			}
			c.seenSharedIRQSource[routeKey+"|"+sourceLabel] = true
			c.graph.SharedInterruptSources = append(c.graph.SharedInterruptSources, node)
		}
	}
}
```

If `constValue` does not already expose field helpers, add these methods beside the call recorder and back them with the existing literal-struct representation used by the checker:

```go
func (v constValue) fieldUint(name string) (uint64, bool) {
	if v.Fields == nil {
		return 0, false
	}
	return v.Fields[name].asUint()
}

func (v constValue) fieldString(name string) (string, bool) {
	if v.Fields == nil {
		return "", false
	}
	return v.Fields[name].asString()
}
```

Use the existing hardware-claim extraction pattern in `compiler/sem/check.go` as the model: store route keys by binding when `route_shared_irq(...)` is assigned to `let route = ...`, then read that binding back when `route.claim_source(...)` is checked. If the current checker uses a differently named call hook, keep the same logic and adapt only the hook name.

- [ ] **Step 5: Run verification**

Run:

```bash
go test ./compiler/sem -run 'TestInterruptQueue|TestSharedInterrupt|TestDuplicateIsaIrqClaimRejected' -v
git diff --check
```

Expected: all tests PASS.

- [ ] **Step 6: Commit**

```bash
git add wrela/machine/x86_64/interrupt_queue.wrela wrela/machine/x86_64/interrupts.wrela wrela/platform/hardware/memory.wrela compiler/sem/interrupt_queue_test.go compiler/sem/hardware_claim_test.go compiler/sem/image_graph.go compiler/sem/check.go
git commit -m "feat: add shared interrupts and bounded queues -Codex Automated"
```

**Acceptance Criteria:** Shared IRQ routes claim hardware once; duplicate source labels under one route fail; interrupt queues require capacity, payload kind, and overflow policy; queue nodes are reportable.

### Task 13: Interrupt Queue Codegen

**Description:** Emit deterministic bounded ring storage for interrupt queues and implement all four overflow policies.

**Files:**
- Create: `compiler/codegen/interrupt_queue.go`
- Create: `compiler/codegen/interrupt_queue_test.go`
- Modify: `compiler/ir/ir.go`
- Modify: `compiler/ir/lower.go`
- Create: `compiler/ir/interrupt_queue_test.go`

**Code Examples:**

IR layout:

```go
type InterruptQueueLayout struct {
	Label string
	Owner string
	Capacity uint64
	PayloadSize uint64
	PayloadAlign uint64
	Overflow string
}
```

Codegen object:

```go
func interruptQueueDataObject(q ir.InterruptQueueLayout) ir.DataObject {
	bytes := 32 + q.Capacity*q.PayloadSize
	return ir.DataObject{
		Symbol: "_wrela_interrupt_queue_" + sanitizeSymbol(q.Label),
		Bytes:  make([]byte, bytes),
		Align:  q.PayloadAlign,
	}
}
```

Overflow test:

```go
func compileInterruptQueuePushForTest(q ir.InterruptQueueLayout) compiledUnit {
	e := newEmitter(compileContext{})
	emitInterruptQueuePush(e, q)
	return compiledUnit{Symbol: "interrupt_queue_push_test", Bytes: e.Code, CallReloc: e.CallReloc}
}

func TestInterruptQueueDropNewestSetsOverflowFlag(t *testing.T) {
	q := ir.InterruptQueueLayout{Label: "irq.serial.rx", Capacity: 1, PayloadSize: 8, PayloadAlign: 8, Overflow: "drop_newest_and_set_flag"}
	unit := compileInterruptQueuePushForTest(q)
	if !containsBytes(unit.Bytes, []byte{0xC6, 0x40, 0x18, 0x01}) {
		t.Fatalf("drop-newest queue push must set overflow flag: %x", unit.Bytes)
	}
}
```

**Steps:**

- [ ] **Step 1: Add IR and codegen tests**

Add the first four tests to `compiler/codegen/interrupt_queue_test.go` and the two bounds tests to `compiler/ir/interrupt_queue_test.go`:

```go
func TestInterruptQueueDropNewestSetsOverflowFlag(t *testing.T) {
	q := ir.InterruptQueueLayout{Label: "irq.serial.rx", Capacity: 1, PayloadSize: 8, PayloadAlign: 8, Overflow: "drop_newest_and_set_flag"}
	unit := compileInterruptQueuePushForTest(q)
	if !containsBytes(unit.Bytes, []byte{0xC6, 0x40, 0x18, 0x01}) {
		t.Fatalf("drop-newest queue push must set overflow flag: %x", unit.Bytes)
	}
}

func TestInterruptQueueDropOldestAdvancesHeadAndSetsOverflowFlag(t *testing.T) {
	q := ir.InterruptQueueLayout{Label: "irq.serial.rx", Capacity: 1, PayloadSize: 8, PayloadAlign: 8, Overflow: "drop_oldest_and_set_flag"}
	unit := compileInterruptQueuePushForTest(q)
	if !containsBytes(unit.Bytes, []byte{0xC6, 0x40, 0x18, 0x01}) {
		t.Fatalf("drop-oldest queue push must set overflow flag: %x", unit.Bytes)
	}
	if !containsBytes(unit.Bytes, []byte{0x48, 0x89}) {
		t.Fatalf("drop-oldest queue push must rewrite head/tail state: %x", unit.Bytes)
	}
}

func TestInterruptQueueSetFlagAndWakeCallsWakeTarget(t *testing.T) {
	q := ir.InterruptQueueLayout{Label: "irq.serial.rx", Capacity: 1, PayloadSize: 8, PayloadAlign: 8, Overflow: "set_flag_and_wake"}
	unit := compileInterruptQueuePushForTest(q)
	if !containsBytes(unit.Bytes, []byte{0xC6, 0x40, 0x18, 0x01}) {
		t.Fatalf("set-flag queue push must set overflow flag: %x", unit.Bytes)
	}
	found := false
	for _, reloc := range unit.CallReloc {
		if reloc.Symbol == "_wrela_interrupt_queue_wake_overflow_owner" {
			found = true
		}
	}
	if !found {
		t.Fatalf("set-flag queue push must wake overflow owner, relocs = %#v", unit.CallReloc)
	}
}

func TestInterruptQueueBootFatalCallsOverflowTrap(t *testing.T) {
	q := ir.InterruptQueueLayout{Label: "irq.serial.rx", Capacity: 1, PayloadSize: 8, PayloadAlign: 8, Overflow: "boot_fatal"}
	unit := compileInterruptQueuePushForTest(q)
	found := false
	for _, reloc := range unit.CallReloc {
		if reloc.Symbol == "_wrela_interrupt_queue_overflow" {
			found = true
		}
	}
	if !found {
		t.Fatalf("boot-fatal queue push must call overflow trap, relocs = %#v", unit.CallReloc)
	}
}

const interruptQueueBoundsSource = `
module examples.interrupt_queue_bounds
use { BootPanic } from platform.hardware.panic
use { PlatformDiscoveryRoot } from platform.hardware.discovery
use { DelegatedHardware } from platform.uefi.transition
use { ArenaIdentity, ArenaPolicy } from platform.hardware.memory
use { OwnedHardware, OwnedMemory, IoPortAuthority, MemoryPlan, CpuPlan, HardwarePlan, InterruptRoutingPlan, ClaimedPciPlanBuilder, SlotIdentity } from machine.x86_64.cpu_state
use { MutableBytes, Bytes } from machine.x86_64.executor_memory
use { InterruptVector } from machine.x86_64.interrupts
use { InterruptOverflowPolicy, InterruptPayloadKind, QueueIdentity } from machine.x86_64.interrupt_queue
image QueueBounds {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        let panic = BootPanic()
        let discovery = PlatformDiscoveryRoot(panic = panic).from_uefi(hardware = hardware)
        let root_region = discovery.memory.require_usable_region(min_base = 0x200000, length = 0x1000000, align = 4096)
        let root = root_region.create_arena(identity = ArenaIdentity(label = "root"), policy = ArenaPolicy(evict_cache_by_default = true))
        let slot = hardware.executors.claim(identity = SlotIdentity(label = "console"))
        let q = root.interrupt_queue(identity = QueueIdentity(label = "irq.serial.rx"), owner = slot, capacity = 64, payload = InterruptPayloadKind(kind = 1, size = 8, align = 8), overflow = InterruptOverflowPolicy(mode = 0))
        let arena = MutableBytes(address = 0, length = 0)
        return hardware.exit_to_owned_hardware(memory_plan = MemoryPlan(owned_memory = OwnedMemory(arena = arena), executor_arena = arena, io_ports = IoPortAuthority()), cpu_plan = CpuPlan(owned_stack_top = 0, gdt_descriptor = Bytes(address = 0, length = 0), idt_descriptor = Bytes(address = 0, length = 0), cr3 = 0), hardware_plan = HardwarePlan(cpus = discovery.cpus.require_min_count(count = 2), interrupts = InterruptRoutingPlan(local_apic = discovery.interrupts.local_apic, serial_irq4 = discovery.interrupts.route_isa_irq(irq = 4, vector = InterruptVector(value = 0x40))), pci = ClaimedPciPlanBuilder(panic = panic).empty()))
    }
    phase owned_hardware(hardware: OwnedHardware) -> never { while true {} }
}
`

func hasDiagCodeForIRTest(ds []diag.Diagnostic, code string) bool {
	for _, d := range ds {
		if d.Code == code {
			return true
		}
	}
	return false
}

func TestInterruptQueueRejectsZeroCapacity(t *testing.T) {
	src := strings.Replace(interruptQueueBoundsSource, "capacity = 64", "capacity = 0", 1)
	checked := checkedProgramFromSourceForTest(t, src)
	_, ds := Lower(checked)
	if !hasDiagCodeForIRTest(ds, diag.SEM0060) {
		t.Fatalf("expected SEM0060 for zero capacity, got %#v", ds)
	}
}

func TestInterruptQueueRejectsZeroPayloadSize(t *testing.T) {
	src := strings.Replace(interruptQueueBoundsSource, "size = 8", "size = 0", 1)
	checked := checkedProgramFromSourceForTest(t, src)
	_, ds := Lower(checked)
	if !hasDiagCodeForIRTest(ds, diag.SEM0060) {
		t.Fatalf("expected SEM0060 for zero payload size, got %#v", ds)
	}
}
```

Add `strings` and `github.com/ryanwible/wrela3/compiler/diag` imports where these tests live. Use SEM0060 for zero capacity or zero payload size.

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
go test ./compiler/ir ./compiler/codegen -run 'TestInterruptQueue' -v
```

Expected: FAIL because queue IR/codegen does not exist.

- [ ] **Step 3: Implement IR lowering**

Lower `arena.interrupt_queue(...)` calls into `Program.InterruptQueues` using literal identity, capacity, payload size, align, and overflow policy. If capacity or payload size is zero, emit SEM0060.

- [ ] **Step 4: Implement codegen data and push helpers**

Queue memory layout is fixed:

```text
offset 0: head U64
offset 8: tail U64
offset 16: capacity U64
offset 24: overflowed U8
offset 32: payload ring bytes
```

Add queue data objects to writable data before topic objects. `boot_fatal` calls `_wrela_interrupt_queue_overflow`.

- [ ] **Step 5: Run verification**

Run:

```bash
go test ./compiler/ir ./compiler/codegen -run 'TestInterruptQueue|TestInterruptDispatch' -v
git diff --check
```

Expected: all tests PASS.

- [ ] **Step 6: Commit**

```bash
git add compiler/codegen/interrupt_queue.go compiler/codegen/interrupt_queue_test.go compiler/ir/ir.go compiler/ir/lower.go compiler/ir/interrupt_queue_test.go
git commit -m "feat: emit bounded interrupt queue storage -Codex Automated"
```

**Acceptance Criteria:** Interrupt queues have deterministic storage, reject invalid bounds, implement each overflow policy, and can be used by interrupt dispatch code.

---

## 8. Phase 4: Executor Runtime Integration

**Description:** Integrate topology-aware placement, locality-aware memory, typed topic payloads, wake strategy selection, and final authority audit report generation without introducing a scheduler.

**Phase Acceptance Criteria:**

- Required placement constraints boot-fatal when unsatisfied.
- Preferred placement constraints are recorded with satisfied/fallback status.
- Executor memory can be allocated near CPU/device facts when known and falls back deterministically when unknown.
- Topic layout supports `TimerTickPayload`, a non-`U64` payload with static size and alignment.
- Wake strategy records monitor/mwait selection and hlt fallback.
- Hidden scheduler constructs are rejected.
- Report authority audit is complete.

**Phase Code Example:**

```wrela
let placement = topology.placement(panic = BootPanic())
placement.require_separate_physical_cores(a = console_slot, b = worker_slot)
let worker_memory = root_arena.executor_memory_near(
    owner = worker_slot,
    near = placement.cpu_for(slot = worker_slot),
    length = 0x200000,
    align = 4096
)
let ticks = timer.subscribe(subscriber = worker_slot)
```

### Task 14: Placement Constraint Graph

**Description:** Add source APIs and semantic graph recording for required and preferred executor placement constraints.

**Files:**
- Create: `wrela/machine/x86_64/placement.wrela`
- Modify: `wrela/machine/x86_64/cpu_state.wrela`
- Create: `compiler/sem/placement.go`
- Create: `compiler/sem/placement_test.go`
- Modify: `compiler/sem/image_graph.go`

**Code Examples:**

Source:

```wrela
data PlacementPreferenceResult {
    satisfied: Bool
    fallback: U64
}

data PlacementTarget {
    kind: U64
    id: U64
    known: Bool
}

data CpuTopology {
    bootstrap: EnabledCpu
    secondary: EnabledCpu

    fn placement(self, panic: BootPanic) -> CpuPlacementPlan {
        return CpuPlacementPlan(topology = self, panic = panic)
    }

    fn can_prove_separate_cores(self) -> Bool {
        return self.bootstrap.apic_id != self.secondary.apic_id
    }

    fn has_known_llc_groups(self) -> Bool {
        return false
    }
}

class CpuPlacementPlan {
    topology: CpuTopology
    panic: BootPanic

    fn require_separate_physical_cores(self, a: ExecutorSlot, b: ExecutorSlot) {
        if self.topology.can_prove_separate_cores() == false {
            self.panic.fail(code = 0xAC040101)
        }
    }

    fn prefer_same_cache_group(self, a: ExecutorSlot, b: ExecutorSlot) -> PlacementPreferenceResult {
        if self.topology.has_known_llc_groups() {
            return PlacementPreferenceResult(satisfied = true, fallback = 0)
        }
        return PlacementPreferenceResult(satisfied = false, fallback = 1)
    }

    fn prefer_near_device(self, slot: ExecutorSlot, device: PciDevice) -> PlacementPreferenceResult {
        return PlacementPreferenceResult(satisfied = false, fallback = 1)
    }

    fn cpu_for(self, slot: ExecutorSlot) -> PlacementTarget {
        return PlacementTarget(kind = 1, id = slot.id, known = false)
    }
}
```

Graph node:

```go
type PlacementConstraintNode struct {
	Kind string
	A string
	B string
	Required bool
	Satisfied bool
	Fallback string
	Span source.Span
}

type PlacementDecisionNode struct {
	SlotLabel string
	Target string
	Satisfied bool
	Fallback string
	Span source.Span
}
```

Extend `ImageGraph`:

```go
PlacementConstraints []PlacementConstraintNode
PlacementDecisions []PlacementDecisionNode
```

**Steps:**

- [ ] **Step 1: Add semantic tests**

Add:

```go
const placementSource = `
module examples.placement_good
use { BootPanic } from platform.hardware.panic
use { PlatformDiscoveryRoot } from platform.hardware.discovery
use { DelegatedHardware } from platform.uefi.transition
use { OwnedHardware, OwnedMemory, IoPortAuthority, MemoryPlan, CpuPlan, HardwarePlan, InterruptRoutingPlan, ClaimedPciPlanBuilder, SlotIdentity } from machine.x86_64.cpu_state
use { MutableBytes, Bytes } from machine.x86_64.executor_memory
use { InterruptVector } from machine.x86_64.interrupts
image PlacementGood {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        let panic = BootPanic()
        let discovery = PlatformDiscoveryRoot(panic = panic).from_uefi(hardware = hardware)
        let topology = discovery.cpus.require_min_count(count = 2)
        let placement = topology.placement(panic = panic)
        let console = hardware.executors.claim(identity = SlotIdentity(label = "console"))
        let worker = hardware.executors.claim(identity = SlotIdentity(label = "worker"))
        placement.require_separate_physical_cores(a = console, b = worker)
        let preferred = placement.prefer_same_cache_group(a = console, b = worker)
        let arena = MutableBytes(address = 0, length = 0)
        return hardware.exit_to_owned_hardware(memory_plan = MemoryPlan(owned_memory = OwnedMemory(arena = arena), executor_arena = arena, io_ports = IoPortAuthority()), cpu_plan = CpuPlan(owned_stack_top = 0, gdt_descriptor = Bytes(address = 0, length = 0), idt_descriptor = Bytes(address = 0, length = 0), cr3 = 0), hardware_plan = HardwarePlan(cpus = topology, interrupts = InterruptRoutingPlan(local_apic = discovery.interrupts.local_apic, serial_irq4 = discovery.interrupts.route_isa_irq(irq = 4, vector = InterruptVector(value = 0x40))), pci = ClaimedPciPlanBuilder(panic = panic).empty()))
    }
    phase owned_hardware(hardware: OwnedHardware) -> never { while true {} }
}
`

func TestPlacementConstraintsRecorded(t *testing.T) {
	checked, ds := checkUEFIModulesWithExtraSource(t, "placement-good.wrela", placementSource)
	if len(ds) != 0 {
		t.Fatalf("diagnostics: %#v", ds)
	}
	if len(checked.ImageGraph.PlacementConstraints) != 2 {
		t.Fatalf("placement constraints = %#v", checked.ImageGraph.PlacementConstraints)
	}
}

func TestHiddenSchedulerConstructRejected(t *testing.T) {
	_, ds := checkModuleForTest(t, hiddenSchedulerSource)
	if !hasCode(ds, diag.SEM0067) {
		t.Fatalf("expected SEM0067, got %#v", ds)
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
go test ./compiler/sem -run 'TestPlacement|TestHiddenScheduler' -v
```

Expected: FAIL because placement graph and scheduler guard do not exist.

- [ ] **Step 3: Implement placement graph extraction**

Record these calls:

```text
require_separate_physical_cores -> Required true, Kind "separate_physical_cores"
prefer_same_cache_group        -> Required false, Kind "same_cache_group"
prefer_near_device             -> Required false, Kind "near_device"
cpu_for                        -> PlacementTarget node with Kind "cpu_for_slot"
```

Reject hidden scheduler constructs by scanning type and method declarations during index/check:

```go
var bannedSchedulerNames = []string{"Scheduler", "RunnableQueue", "migrate", "work_steal", "spawn_on_any_cpu"}
```

Emit SEM0067 with message `hidden scheduler construct is not allowed`.

- [ ] **Step 4: Run verification**

Run:

```bash
go test ./compiler/sem -run 'TestPlacement|TestHiddenScheduler|TestExecutorSlotTopicGraphExtraction' -v
git diff --check
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add wrela/machine/x86_64/placement.wrela wrela/machine/x86_64/cpu_state.wrela compiler/sem/placement.go compiler/sem/placement_test.go compiler/sem/image_graph.go
git commit -m "feat: record executor placement constraints -Codex Automated"
```

**Acceptance Criteria:** Required and preferred placement decisions are source-visible, graph-recorded, and reportable; hidden scheduler vocabulary is rejected.

### Task 15: Locality-Aware Executor Memory

**Description:** Add `executor_memory_near` and deterministic fallback for unknown locality, then report the selected/fallback decision.

**Files:**
- Modify: `wrela/platform/hardware/memory.wrela`
- Modify: `wrela/machine/x86_64/placement.wrela`
- Modify: `compiler/sem/memory_graph.go`
- Modify: `compiler/sem/memory_graph_test.go`
- Modify: `compiler/sem/report.go`

**Code Examples:**

Source:

```wrela
data PlacementTarget {
    kind: U64
    id: U64
    known: Bool
}

fn executor_memory_near(self, owner: ExecutorSlot, near: PlacementTarget, length: U64, align: U64) -> ExecutorMemory {
    if near.known == false {
        self.record_locality_fallback(owner = owner, fallback = 1)
        return self.executor_memory(owner = owner, length = length, align = align)
    }
    self.record_locality_match(owner = owner, target = near)
    return self.executor_memory(owner = owner, length = length, align = align)
}
```

The allocation result is intentionally identical in this milestone because Wrela does not yet have NUMA-aware physical placement. The behavior difference is the recorded placement decision: `near.known == false` must emit fallback `"unknown_locality"`; `near.known == true` must emit the target kind/id in the report. Later NUMA work can change the allocator internals without changing this source API.

Report mapping:

```go
func appendExecutorMemoryAndLocality(r *report.ImageReport, g ImageGraph) {
	for _, arena := range g.Arenas {
		if arena.Kind != "executor_memory" {
			continue
		}
		r.Memory.ExecutorBudgets = append(r.Memory.ExecutorBudgets, report.ExecutorBudgetReport{
			SlotLabel: arena.Owner,
			Bytes: arena.Bytes,
		})
	}
	for _, placement := range g.PlacementDecisions {
		r.Runtime.Placement = append(r.Runtime.Placement, report.PlacementReport{
			SlotLabel: placement.SlotLabel,
			Target: placement.Target,
			Satisfied: placement.Satisfied,
			Fallback: placement.Fallback,
		})
	}
}
```

Wire this helper into `BuildImageReport` after `appendDiscoveryFacts`:

```go
appendDiscoveryFacts(&r, checked.ImageGraph)
appendExecutorMemoryAndLocality(&r, checked.ImageGraph)
return r
```

**Steps:**

- [ ] **Step 1: Add test**

Add:

```go
const executorMemoryNearSource = `
module examples.executor_memory_near
use { BootPanic } from platform.hardware.panic
use { PlatformDiscoveryRoot } from platform.hardware.discovery
use { DelegatedHardware } from platform.uefi.transition
use { ArenaIdentity, ArenaPolicy } from platform.hardware.memory
use { OwnedHardware, OwnedMemory, IoPortAuthority, MemoryPlan, CpuPlan, HardwarePlan, InterruptRoutingPlan, ClaimedPciPlanBuilder, SlotIdentity } from machine.x86_64.cpu_state
use { MutableBytes, Bytes } from machine.x86_64.executor_memory
use { InterruptVector } from machine.x86_64.interrupts
image MemoryNearImage {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        let panic = BootPanic()
        let discovery = PlatformDiscoveryRoot(panic = panic).from_uefi(hardware = hardware)
        let root_region = discovery.memory.require_usable_region(min_base = 0x200000, length = 0x1000000, align = 4096)
        let root = root_region.create_arena(identity = ArenaIdentity(label = "root"), policy = ArenaPolicy(evict_cache_by_default = true))
        let worker = hardware.executors.claim(identity = SlotIdentity(label = "worker"))
        let placement = discovery.cpus.require_min_count(count = 2).placement(panic = panic)
        let memory = root.executor_memory_near(owner = worker, near = placement.cpu_for(slot = worker), length = 0x200000, align = 4096)
        let compat = root_region.bytes()
        return hardware.exit_to_owned_hardware(memory_plan = MemoryPlan(owned_memory = OwnedMemory(arena = compat), executor_arena = compat, io_ports = IoPortAuthority()), cpu_plan = CpuPlan(owned_stack_top = 0, gdt_descriptor = Bytes(address = 0, length = 0), idt_descriptor = Bytes(address = 0, length = 0), cr3 = 0), hardware_plan = HardwarePlan(cpus = discovery.cpus.require_min_count(count = 2), interrupts = InterruptRoutingPlan(local_apic = discovery.interrupts.local_apic, serial_irq4 = discovery.interrupts.route_isa_irq(irq = 4, vector = InterruptVector(value = 0x40))), pci = ClaimedPciPlanBuilder(panic = panic).empty()))
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
	if len(r.Memory.ExecutorBudgets) == 0 {
		t.Fatalf("executor memory budget missing: %#v", r.Memory)
	}
	if !containsPlacementFallback(r.Runtime.Placement, "unknown_locality") {
		t.Fatalf("missing unknown-locality fallback: %#v", r.Runtime.Placement)
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run:

```bash
go test ./compiler/sem -run TestExecutorMemoryNearRecordsFallback -v
```

Expected: FAIL because `executor_memory_near` and fallback reporting do not exist.

- [ ] **Step 3: Implement source, graph recording, and report wiring**

Treat `executor_memory_near` as an executor memory arena allocation. When `near.known` cannot be proven from literals, add a `PlacementDecisionNode{SlotLabel: owner label, Target: "cpu", Satisfied: false, Fallback: "unknown_locality"}`. Add `appendExecutorMemoryAndLocality(&r, checked.ImageGraph)` to `BuildImageReport` immediately after `appendDiscoveryFacts`.

- [ ] **Step 4: Run verification**

Run:

```bash
go test ./compiler/sem -run 'TestExecutorMemoryNearRecordsFallback|TestArenaGraph|TestInlineExecutorArenaOwnerMismatchIsRejectedFromSource' -v
git diff --check
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add wrela/platform/hardware/memory.wrela wrela/machine/x86_64/placement.wrela compiler/sem/memory_graph.go compiler/sem/memory_graph_test.go compiler/sem/report.go
git commit -m "feat: report locality-aware executor memory -Codex Automated"
```

**Acceptance Criteria:** `executor_memory_near` is available; unknown locality falls back deterministically; per-executor memory budget appears in the report.

### Task 16: Typed Topic Payload Layout

**Description:** Generalize topic layout from fixed `U64` payloads to statically known payload types and prove it with `TimerTickPayload`. Execute this task before Task 11; Task 11 consumes the typed timer topic created here.

**Files:**
- Create: `wrela/machine/x86_64/topic_payload.wrela`
- Modify: `wrela/machine/x86_64/topic_u64.wrela`
- Modify: `compiler/sem/image_graph.go`
- Create: `compiler/sem/topic_payload.go`
- Create: `compiler/sem/topic_payload_test.go`
- Modify: `compiler/ir/ir.go`
- Modify: `compiler/ir/lower.go`
- Modify: `compiler/codegen/topic_data.go`
- Modify: `compiler/codegen/topic_test.go`

**Code Examples:**

Source:

```wrela
module machine.x86_64.topic_payload

use { ExecutorSlot } from machine.x86_64.cpu_state
use { TopicIdentity } from machine.x86_64.topic_u64

data TimerTickPayload {
    sequence: U64
    monotonic_us: U64
    source_id: U32
}

data TimerTickNext {
    has_message: Bool
    gap: Bool
    missed: U64
    message: TimerTickPayload
}

class TimerTickTopic {
    identity: TopicIdentity
    id: U64
    depth: U64

    fn publisher(self) -> TimerTickPublisher {
        return TimerTickPublisher(topic = self)
    }

    fn subscribe(self, subscriber: ExecutorSlot) -> TimerTickSubscription {
        return TimerTickSubscription(topic = self, subscriber = subscriber, cursor = 0, armed = false)
    }
}

class TimerTickPublisher {
    topic: TimerTickTopic

    fn publish(self, payload: TimerTickPayload) {
        self.publish_intrinsic(payload = payload)
    }

    asm fn publish_intrinsic(self, payload: TimerTickPayload) {
        ret
    }
}

class TimerTickSubscription {
    topic: TimerTickTopic
    subscriber: ExecutorSlot
    cursor: U64
    armed: Bool

    asm fn try_next(self) -> TimerTickNext {
        ret
    }

    fn arm_wait(self) {
        self.armed = true
    }

    fn is_wait_armed(self) -> Bool {
        return self.armed
    }
}
```

IR layout:

```go
type TopicLayout struct {
	Label string
	Kind string
	Depth uint64
	PayloadType Type
	PayloadSize uint64
	PayloadAlign uint64
	Producers []string
	Subscribers []string
}
```

Slot size:

```go
func topicSlotSize(payloadSize uint64) uint64 {
	return alignUp64(8 + payloadSize)
}
```

The first 8 bytes of each slot remain the sequence number. Payload bytes start at offset 8.

**Steps:**

- [ ] **Step 1: Add semantic payload tests**

Add:

```go
const timerTickTopicSource = `
module examples.timer_tick_topic
use { BootPanic } from platform.hardware.panic
use { PlatformDiscoveryRoot } from platform.hardware.discovery
use { DelegatedHardware } from platform.uefi.transition
use { OwnedHardware, OwnedMemory, IoPortAuthority, MemoryPlan, CpuPlan, HardwarePlan, InterruptRoutingPlan, ClaimedPciPlanBuilder, SlotIdentity } from machine.x86_64.cpu_state
use { MutableBytes, Bytes } from machine.x86_64.executor_memory
use { InterruptVector } from machine.x86_64.interrupts
use { TimerTickTopic } from machine.x86_64.topic_payload
use { TopicIdentity } from machine.x86_64.topic_u64
image TimerTopicImage {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        let panic = BootPanic()
        let discovery = PlatformDiscoveryRoot(panic = panic).from_uefi(hardware = hardware)
        let worker = hardware.executors.claim(identity = SlotIdentity(label = "worker"))
        let topic = TimerTickTopic(identity = TopicIdentity(label = "timer.periodic"), id = 3, depth = 64)
        let sub = topic.subscribe(subscriber = worker)
        let arena = MutableBytes(address = 0, length = 0)
        return hardware.exit_to_owned_hardware(memory_plan = MemoryPlan(owned_memory = OwnedMemory(arena = arena), executor_arena = arena, io_ports = IoPortAuthority()), cpu_plan = CpuPlan(owned_stack_top = 0, gdt_descriptor = Bytes(address = 0, length = 0), idt_descriptor = Bytes(address = 0, length = 0), cr3 = 0), hardware_plan = HardwarePlan(cpus = discovery.cpus.require_min_count(count = 2), interrupts = InterruptRoutingPlan(local_apic = discovery.interrupts.local_apic, serial_irq4 = discovery.interrupts.route_isa_irq(irq = 4, vector = InterruptVector(value = 0x40))), pci = ClaimedPciPlanBuilder(panic = panic).empty()))
    }
    phase owned_hardware(hardware: OwnedHardware) -> never { while true {} }
}
`

func TestTimerTickTopicPayloadLayoutRecorded(t *testing.T) {
	checked, ds := checkUEFIModulesWithExtraSource(t, "timer-topic.wrela", timerTickTopicSource)
	if len(ds) != 0 {
		t.Fatalf("diagnostics: %#v", ds)
	}
	topic := checked.ImageGraph.TopicByLabel("timer.periodic")
	if topic.PayloadType != "machine.x86_64.topic_payload.TimerTickPayload" {
		t.Fatalf("payload type = %q", topic.PayloadType)
	}
	if topic.PayloadSize == 0 || topic.PayloadAlign == 0 {
		t.Fatalf("payload layout not recorded: %#v", topic)
	}
}
```

- [ ] **Step 2: Add codegen layout test**

Add:

```go
func TestTypedTopicDataUsesPayloadSlotSize(t *testing.T) {
	layout := planTopicData(ir.TopicLayout{
		Label: "timer.periodic",
		Kind: "timer_tick",
		Depth: 64,
		PayloadSize: 24,
		PayloadAlign: 8,
		Subscribers: []string{"worker"},
	})
	wantSlot := uint64(64)
	if got := layout.SlotSize; got != wantSlot {
		t.Fatalf("slot size = %d, want %d", got, wantSlot)
	}
}
```

- [ ] **Step 3: Run tests to verify failure**

Run:

```bash
go test ./compiler/sem ./compiler/codegen -run 'TestTimerTickTopicPayloadLayoutRecorded|TestTypedTopicDataUsesPayloadSlotSize' -v
```

Expected: FAIL because topic payload fields are fixed to U64.

- [ ] **Step 4: Implement semantic payload classification**

Classify:

```go
func TopicPayloadTypeForTopic(t *Type) (payload *Type, kind string, ok bool) {
	if t != nil && t.Module == "machine.x86_64.topic_payload" && t.Name == "TimerTickTopic" {
		return resolveBuiltinTopicPayload("machine.x86_64.topic_payload", "TimerTickPayload"), "timer_tick", true
	}
	if t != nil && t.Module == "machine.x86_64.topic_u64" && strings.HasPrefix(t.Name, "U64") {
		return primitiveU64Type(), existingU64TopicKind(t), true
	}
	return nil, "", false
}
```

Add the helper implementations in `compiler/sem/topic_payload.go`:

```go
func resolveBuiltinTopicPayload(moduleName string, typeName string) *Type {
	return &Type{Module: moduleName, Name: typeName, Kind: KindData}
}

func primitiveU64Type() *Type {
	return &Type{Module: "", Name: "U64", Kind: KindPrimitive}
}

func existingU64TopicKind(t *Type) string {
	if t == nil {
		return ""
	}
	switch t.Name {
	case "U64GapTopic":
		return "gap_u64"
	case "U64ReliableTopic":
		return "reliable_u64"
	default:
		return "u64"
	}
}

func payloadLayoutFromType(t *Type) (size uint64, align uint64, ok bool) {
	if t == nil {
		return 0, 0, false
	}
	if t.Kind == KindPrimitive && t.Name == "U64" {
		return 8, 8, true
	}
	if t.Module == "machine.x86_64.topic_payload" && t.Name == "TimerTickPayload" {
		return 24, 8, true
	}
	return 0, 0, false
}
```

Carry payload size/align from `payloadLayoutFromType` into `TopicNode.PayloadType`, `TopicNode.PayloadSize`, and `TopicNode.PayloadAlign`.

- [ ] **Step 5: Implement IR carrier fields**

Add the same payload fields to `ir.TopicLayout`:

```go
PayloadType Type
PayloadSize uint64
PayloadAlign uint64
SlotSize uint64
```

Update lowering so every U64 topic sets `PayloadSize = 8`, `PayloadAlign = 8`, and every `TimerTickTopic` uses the semantic payload layout from `TopicNode`.

- [ ] **Step 6: Implement codegen data layout only**

Update `planTopicData` so `layout.SlotSize = alignUp64(8 + topic.PayloadSize)`. Do not change publish/consume assembly in this task. Timer runtime publishing is owned by Task 11, and U64 topic runtime behavior must remain byte-for-byte compatible for U64 payloads.

- [ ] **Step 7: Run verification**

Run:

```bash
go test ./compiler/sem ./compiler/ir ./compiler/codegen -run 'Test.*Topic' -v
git diff --check
```

Expected: all topic tests PASS.

- [ ] **Step 8: Commit**

```bash
git add wrela/machine/x86_64/topic_payload.wrela wrela/machine/x86_64/topic_u64.wrela compiler/sem/image_graph.go compiler/sem/topic_payload.go compiler/sem/topic_payload_test.go compiler/ir/ir.go compiler/ir/lower.go compiler/codegen/topic_data.go compiler/codegen/topic_test.go
git commit -m "feat: support statically typed topic payloads -Codex Automated"
```

**Acceptance Criteria:** Existing U64 topics still work; `TimerTickPayload` has static layout; topic slot size is derived from payload size and cache-line alignment; unsupported payload layouts fail with SEM0066.

### Task 17: Wake Strategy Selection And Scheduler Guard

**Description:** Select monitor/mwait when discovered and supported, use `sti; hlt` fallback, and make wake strategy reportable.

**Files:**
- Modify: `wrela/machine/x86_64/executor_loop.wrela`
- Modify: `wrela/machine/x86_64/cpu_state.wrela`
- Modify: `compiler/ir/ir.go`
- Modify: `compiler/ir/lower.go`
- Modify: `compiler/codegen/x64.go`
- Modify: `compiler/codegen/wait_test.go`
- Modify: `compiler/sem/report.go`

**Code Examples:**

Source:

```wrela
data WakeStrategy {
    monitor_mwait: Bool
    fallback_hlt: Bool
}

data CpuFeatureFacts {
    monitor_mwait_available: Bool
}

class EventSleepPolicy {
    strategy: WakeStrategy

    fn wait(self) {
        self.wait_intrinsic()
    }

    asm fn wait_intrinsic(self) {
        hlt
        ret
    }
}
```

Construct the policy from discovered CPU facts in `machine.x86_64.cpu_state`:

```wrela
fn wake_strategy(self, features: CpuFeatureFacts) -> WakeStrategy {
    if features.monitor_mwait_available {
        return WakeStrategy(monitor_mwait = true, fallback_hlt = true)
    }
    return WakeStrategy(monitor_mwait = false, fallback_hlt = true)
}
```

IR:

```go
type CpuFeatureFacts struct {
	MonitorMwaitAvailable bool
}

type TopicWait struct {
	SlotLabel string
	Policy string
	UseMonitorMwait bool
	Fallback string
}

type WakeTargetNode struct {
	SlotLabel string
	Owner string
	Strategy string
	Fallback string
	Span source.Span
}
```

Extend `ImageGraph`:

```go
WakeTargets []WakeTargetNode
```

Codegen branch:

```go
if wait.UseMonitorMwait {
	emitMonitorMwait(e, asm.MustLookup("rax"))
} else {
	emitHltWait(e)
}
```

**Steps:**

- [ ] **Step 1: Add tests**

Extend `TestMonitorMwaitBytesAreAvailable`:

```go
func appendWakePathReport(r *report.ImageReport, wait ir.TopicWait) {
	r.Runtime.WakePaths = append(r.Runtime.WakePaths, report.WakePathReport{
		SlotLabel: wait.SlotLabel,
		Strategy: map[bool]string{true: "monitor_mwait", false: "sti_hlt"}[wait.UseMonitorMwait],
		Fallback: wait.Fallback,
	})
}

func topicWaitFromFeaturesForTest(features ir.CpuFeatureFacts) ir.TopicWait {
	wait := ir.TopicWait{SlotLabel: "worker", Fallback: "sti_hlt"}
	if features.MonitorMwaitAvailable {
		wait.UseMonitorMwait = true
	}
	return wait
}

func TestWakeStrategyReportsFallback(t *testing.T) {
	r := report.ImageReport{}
	appendWakePathReport(&r, ir.TopicWait{SlotLabel: "worker", UseMonitorMwait: false, Fallback: "sti_hlt"})
	if len(r.Runtime.WakePaths) != 1 || r.Runtime.WakePaths[0].Fallback != "sti_hlt" {
		t.Fatalf("wake path report = %#v", r.Runtime.WakePaths)
	}
}

func TestWakeStrategyUsesDiscoveredMonitorMwaitFact(t *testing.T) {
	wait := topicWaitFromFeaturesForTest(ir.CpuFeatureFacts{MonitorMwaitAvailable: true})
	if !wait.UseMonitorMwait || wait.Fallback != "sti_hlt" {
		t.Fatalf("wait strategy = %#v, want monitor/mwait with hlt fallback", wait)
	}
}

func TestWakeStrategyReportIncludesMonitorMwaitBranch(t *testing.T) {
	r := report.ImageReport{}
	appendWakePathReport(&r, topicWaitFromFeaturesForTest(ir.CpuFeatureFacts{MonitorMwaitAvailable: true}))
	if len(r.Runtime.WakePaths) != 1 {
		t.Fatalf("wake path report missing: %#v", r.Runtime.WakePaths)
	}
	if r.Runtime.WakePaths[0].Strategy != "monitor_mwait" || r.Runtime.WakePaths[0].Fallback != "sti_hlt" {
		t.Fatalf("wake path report = %#v", r.Runtime.WakePaths)
	}
}
```

Production report wiring:

```go
func appendWakePaths(r *report.ImageReport, g ImageGraph) {
	for _, wake := range g.WakeTargets {
		r.Runtime.WakePaths = append(r.Runtime.WakePaths, report.WakePathReport{
			SlotLabel: wake.SlotLabel,
			Strategy: wake.Strategy,
			Fallback: wake.Fallback,
		})
	}
}
```

Call it in `BuildImageReport` after locality mapping:

```go
appendDiscoveryFacts(&r, checked.ImageGraph)
appendExecutorMemoryAndLocality(&r, checked.ImageGraph)
appendWakePaths(&r, checked.ImageGraph)
return r
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
go test ./compiler/codegen ./compiler/sem -run 'TestWakeStrategyReportsFallback|TestWakeStrategyUsesDiscoveredMonitorMwaitFact|TestWakeStrategyReportIncludesMonitorMwaitBranch|TestMonitorMwaitBytesAreAvailable' -v
```

Expected: FAIL because `ir.CpuFeatureFacts`, wake fields on `ir.TopicWait`, semantic wake-target graph fields, and report mapping are absent.

- [ ] **Step 3: Implement wake policy fields**

Lower `EventSleepPolicy(strategy = WakeStrategy(...))` into `TopicWait.UseMonitorMwait`. The strategy value must come from `CpuFeatureFacts.monitor_mwait_available` when the image constructs `EventSleepPolicy` from discovered CPU facts. If the strategy is dynamic or the fact is unknown, set `UseMonitorMwait = false` and `Fallback = "sti_hlt"` for deterministic first implementation. Record a `WakeTargetNode` for every lowered wait, then call `appendWakePaths(&r, checked.ImageGraph)` from `BuildImageReport`. Always report the fallback, even when monitor/mwait is selected.

- [ ] **Step 4: Run verification**

Run:

```bash
go test ./compiler/ir ./compiler/codegen ./compiler/sem -run 'Test.*Wait|TestWakeStrategy|TestHiddenScheduler' -v
git diff --check
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add wrela/machine/x86_64/executor_loop.wrela wrela/machine/x86_64/cpu_state.wrela compiler/ir/ir.go compiler/ir/lower.go compiler/codegen/x64.go compiler/codegen/wait_test.go compiler/sem/report.go
git commit -m "feat: report executor wake strategy selection -Codex Automated"
```

**Acceptance Criteria:** Wake strategy is explicit, monitor/mwait bytes remain tested, fallback is `sti_hlt`, and report includes per-executor wake paths.

### Task 18: Complete Authority Audit Report

**Description:** Populate every required authority audit section and fail report generation when a required section is missing for an image that uses that authority kind.

**Files:**
- Modify: `compiler/sem/report.go`
- Modify: `compiler/sem/report_test.go`
- Modify: `compiler/build.go`

**Code Examples:**

Audit completeness:

```go
func newAuthorityAuditReport() report.AuthorityAuditReport {
	return report.AuthorityAuditReport{
		MemoryRoots: []report.AuthorityRecord{},
		Arenas: []report.AuthorityRecord{},
		HardwareClaims: []report.AuthorityRecord{},
		Interrupts: []report.AuthorityRecord{},
		Timers: []report.AuthorityRecord{},
		Queues: []report.AuthorityRecord{},
		Topics: []report.AuthorityRecord{},
		WakeTargets: []report.AuthorityRecord{},
		DMABuffers: []report.AuthorityRecord{},
	}
}

func ValidateAuthorityAudit(r report.ImageReport) []diag.Diagnostic {
	var ds []diag.Diagnostic
	require := func(ok bool, name string) {
		if !ok {
			ds = append(ds, diag.Diagnostic{Phase: "sem", Code: diag.SEM0075, Message: "authority audit report missing " + name})
		}
	}
	require(r.AuthorityAudit.MemoryRoots != nil, "memory_roots")
	require(r.AuthorityAudit.Arenas != nil, "arenas")
	require(r.AuthorityAudit.HardwareClaims != nil, "hardware_claims")
	require(r.AuthorityAudit.Interrupts != nil, "interrupts")
	require(r.AuthorityAudit.Timers != nil, "timers")
	require(r.AuthorityAudit.Queues != nil, "queues")
	require(r.AuthorityAudit.Topics != nil, "topics")
	require(r.AuthorityAudit.WakeTargets != nil, "wake_targets")
	require(r.AuthorityAudit.DMABuffers != nil, "dma_buffers")
	return ds
}

func ValidateAuthorityAuditContent(r report.ImageReport) []diag.Diagnostic {
	var ds []diag.Diagnostic
	requireRecord := func(records []report.AuthorityRecord, name string, reportUses bool) {
		if reportUses && len(records) == 0 {
			ds = append(ds, diag.Diagnostic{Phase: "sem", Code: diag.SEM0075, Message: "authority audit report missing records for " + name})
		}
	}
	requireRecord(r.AuthorityAudit.MemoryRoots, "memory_roots", len(r.Memory.RootRegions) != 0)
	requireRecord(r.AuthorityAudit.Arenas, "arenas", len(r.Memory.Arenas) != 0)
	requireRecord(r.AuthorityAudit.HardwareClaims, "hardware_claims", len(r.Hardware.PCI) != 0)
	requireRecord(r.AuthorityAudit.Timers, "timers", len(r.Hardware.Timers) != 0)
	requireRecord(r.AuthorityAudit.Queues, "queues", len(r.Runtime.InterruptQueues) != 0)
	requireRecord(r.AuthorityAudit.Topics, "topics", len(r.Runtime.Topics) != 0)
	requireRecord(r.AuthorityAudit.WakeTargets, "wake_targets", len(r.Runtime.WakePaths) != 0)
	return ds
}
```

At the top of `BuildImageReport`, replace the zero-value report initialization with:

```go
r := report.ImageReport{
	Version: 1,
	Image: imageNameForReport(checked),
	AuthorityAudit: newAuthorityAuditReport(),
}
```

Final audit append helpers owned by Task 18:

```go
func appendInterruptsToAudit(r *report.ImageReport, g ImageGraph) {
	for _, route := range g.InterruptTopicRoutes {
		r.AuthorityAudit.Interrupts = append(r.AuthorityAudit.Interrupts, report.AuthorityRecord{
			Kind: "interrupt_route",
			Label: route.PathLabel,
			Owner: route.TopicLabel,
		})
	}
	for _, source := range g.SharedInterruptSources {
		r.AuthorityAudit.Interrupts = append(r.AuthorityAudit.Interrupts, report.AuthorityRecord{
			Kind: "shared_interrupt_source",
			Label: source.SourceLabel,
			Owner: source.RouteKey,
		})
	}
}

func appendTimersToAudit(r *report.ImageReport, g ImageGraph) {
	for _, timer := range g.TimerFacts {
		r.AuthorityAudit.Timers = append(r.AuthorityAudit.Timers, report.AuthorityRecord{
			Kind: "timer",
			Label: timer.Label,
			Owner: timer.Source,
		})
	}
}

func appendQueuesToAudit(r *report.ImageReport, g ImageGraph) {
	for _, queue := range g.InterruptQueues {
		r.AuthorityAudit.Queues = append(r.AuthorityAudit.Queues, report.AuthorityRecord{
			Kind: "interrupt_queue",
			Label: queue.Label,
			Owner: queue.Owner,
		})
	}
}

func appendTopicsToAudit(r *report.ImageReport, g ImageGraph) {
	for _, topic := range g.Topics {
		r.AuthorityAudit.Topics = append(r.AuthorityAudit.Topics, report.AuthorityRecord{
			Kind: topic.Kind,
			Label: topic.Label,
			Owner: "topic_graph",
		})
	}
}

func appendWakeTargetsToAudit(r *report.ImageReport, g ImageGraph) {
	for _, wake := range g.WakeTargets {
		r.AuthorityAudit.WakeTargets = append(r.AuthorityAudit.WakeTargets, report.AuthorityRecord{
			Kind: "wake_target",
			Label: wake.SlotLabel,
			Owner: wake.Owner,
		})
	}
}

func appendDMABuffersToAudit(r *report.ImageReport, g ImageGraph) {
	for _, dma := range g.DMABuffers {
		r.AuthorityAudit.DMABuffers = append(r.AuthorityAudit.DMABuffers, report.AuthorityRecord{
			Kind: "dma_buffer",
			Label: dma.Label,
			Owner: dma.OwnerDevice,
		})
	}
}
```

Wire them into `BuildImageReport` after the Task 17 helper calls:

```go
appendDiscoveryFacts(&r, checked.ImageGraph)
appendExecutorMemoryAndLocality(&r, checked.ImageGraph)
appendWakePaths(&r, checked.ImageGraph)
appendInterruptsToAudit(&r, checked.ImageGraph)
appendTimersToAudit(&r, checked.ImageGraph)
appendQueuesToAudit(&r, checked.ImageGraph)
appendTopicsToAudit(&r, checked.ImageGraph)
appendWakeTargetsToAudit(&r, checked.ImageGraph)
appendDMABuffersToAudit(&r, checked.ImageGraph)
return r
```

**Steps:**

- [ ] **Step 1: Add report completeness test**

Add:

```go
func TestAuthorityAuditReportCompleteness(t *testing.T) {
	r := report.ImageReport{AuthorityAudit: report.AuthorityAuditReport{
		MemoryRoots: []report.AuthorityRecord{},
		Arenas: []report.AuthorityRecord{},
		HardwareClaims: []report.AuthorityRecord{},
		Interrupts: []report.AuthorityRecord{},
		Timers: []report.AuthorityRecord{},
		Queues: []report.AuthorityRecord{},
		Topics: []report.AuthorityRecord{},
		WakeTargets: []report.AuthorityRecord{},
		DMABuffers: []report.AuthorityRecord{},
	}}
	if ds := ValidateAuthorityAudit(r); len(ds) != 0 {
		t.Fatalf("audit diagnostics: %#v", ds)
	}
}

func TestAuthorityAuditRequiresTimerRecordWhenTimerIsUsed(t *testing.T) {
	r := report.ImageReport{
		Hardware: report.HardwareReport{
			Timers: []report.TimerReport{{Label: "periodic.1000us", Source: "local_apic_pit_calibrated", PeriodUS: 1000}},
		},
		AuthorityAudit: report.AuthorityAuditReport{
			MemoryRoots: []report.AuthorityRecord{},
			Arenas: []report.AuthorityRecord{},
			HardwareClaims: []report.AuthorityRecord{},
			Interrupts: []report.AuthorityRecord{},
			Timers: []report.AuthorityRecord{},
			Queues: []report.AuthorityRecord{},
			Topics: []report.AuthorityRecord{},
			WakeTargets: []report.AuthorityRecord{},
			DMABuffers: []report.AuthorityRecord{},
		},
	}
	if !hasCode(ValidateAuthorityAuditContent(r), diag.SEM0075) {
		t.Fatalf("expected SEM0075 when timer report has no timer authority audit record")
	}
	r.AuthorityAudit.Timers = []report.AuthorityRecord{{Kind: "timer", Label: "periodic.1000us"}}
	if ds := ValidateAuthorityAuditContent(r); len(ds) != 0 {
		t.Fatalf("unexpected content diagnostics: %#v", ds)
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run:

```bash
go test ./compiler/sem -run TestAuthorityAuditReportCompleteness -v
```

Expected: FAIL because validator does not exist.

Then run the content test too:

```bash
go test ./compiler/sem -run 'TestAuthorityAuditReportCompleteness|TestAuthorityAuditRequiresTimerRecordWhenTimerIsUsed' -v
```

Expected: FAIL because content validation and timer audit append wiring do not exist.

- [ ] **Step 3: Populate all report sections**

Replace `BuildImageReport`'s zero-value `report.ImageReport` initialization with `newAuthorityAuditReport()`, then map using the helper functions shown above:

```text
MemoryRoots       <- ImageGraph.MemoryRoots
Arenas            <- ImageGraph.Arenas
HardwareClaims    <- ImageGraph.HardwareClaims
Interrupts        <- InterruptTopicRoutes + SharedInterruptSourceNodes
Timers            <- Program/graph TimerRoute nodes
Queues            <- InterruptQueueNodes
Topics            <- TopicNodes
WakeTargets       <- VcpuPlacements + Topic subscriptions + timer subscribers
DMABuffers        <- DMABufferNodes, empty slice when no DMA buffers exist
```

- [ ] **Step 4: Wire validation into report generation**

When `BuildOptions.ReportPath != ""`, call `sem.ValidateAuthorityAudit(imgReport)` and return `DiagnosticError` if diagnostics exist.

- [ ] **Step 5: Run verification**

Run:

```bash
go test ./compiler/sem ./compiler -run 'TestAuthorityAudit|TestBuildWritesReportWhenRequested' -v
git diff --check
```

Expected: all tests PASS.

- [ ] **Step 6: Commit**

```bash
git add compiler/sem/report.go compiler/sem/report_test.go compiler/build.go
git commit -m "feat: complete authority audit reporting -Codex Automated"
```

**Acceptance Criteria:** All authority audit arrays are present; used authority kinds produce non-empty matching records; report generation fails with SEM0075 if a required section is nil or if an image reports a used authority kind without corresponding audit records.

---

## 9. Phase 5: Migration And Integrated Acceptance

**Description:** Move examples onto the production substrate, add one integrated QEMU fixture, add scans that prevent regression to QEMU constants and raw memory shortcuts, and update docs.

**Phase Acceptance Criteria:**

- Booted examples no longer allocate root executor/device memory from raw address literals outside trusted platform construction.
- QEMU e2e covers AP startup, timer wake, shared IRQ routing, MSI, MSI-X, bounded typed topic payloads, and memory footprint reporting.
- Static scans prevent reintroduction of q35-only placement and raw booted-image memory shortcuts.
- Docs describe the production substrate as the implemented direction.

**Phase Code Example:**

```bash
go run ./cmd/wrela build --mode dev tests/e2e/fixtures/production_substrate/main.wrela \
  -o build/production-substrate.efi \
  --report build/production-substrate.report.json
```

### Task 19: Migrate Hello To Production Substrate Source

**Description:** Rewrite the hello example to use discovery-derived memory, root arenas, placement, timer subscription, shared IRQ source claims, and reportable executor memory.

**Files:**
- Modify: `examples/hello/main.wrela`
- Modify: `examples/hello/program.wrela`
- Modify: `compiler/integration_test.go`
- Modify: `tests/e2e/hello_qemu_test.go`

**Code Examples:**

Expected `main.wrela` shape:

```wrela
let root_region = discovery.memory.require_usable_region(
    min_base = 0x200000,
    length = 0x1000000,
    align = 4096
)
let root_arena = root_region.create_arena(
    identity = ArenaIdentity(label = "boot.root"),
    policy = ArenaPolicy(evict_cache_by_default = true)
)
let console_slot = hardware.executors.claim(identity = SlotIdentity(label = "console"))
let worker_slot = hardware.executors.claim(identity = SlotIdentity(label = "worker"))
let placement = discovery.cpus.require_min_count(count = 2).placement(panic = BootPanic())
placement.require_separate_physical_cores(a = console_slot, b = worker_slot)
let console_memory = root_arena.executor_memory(owner = console_slot, length = 0x200000, align = 4096)
let worker_memory = root_arena.executor_memory_near(
    owner = worker_slot,
    near = placement.cpu_for(slot = worker_slot),
    length = 0x200000,
    align = 4096
)
```

Integration scan:

```go
func TestHelloUsesProductionSubstrate(t *testing.T) {
	main := readRepoFile(t, "examples/hello/main.wrela")
	for _, want := range []string{
		"require_usable_region(",
		"create_arena(",
		"executor_memory(",
		"executor_memory_near(",
		"require_separate_physical_cores(",
		"require_periodic(period_us = 1000)",
		"route_shared_irq(",
	} {
		if !strings.Contains(main, want) {
			t.Fatalf("hello main missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"MutableBytes(address = 0x",
		"arena_base = 0x",
		"two-vCPU",
		"q35",
	} {
		if strings.Contains(main, forbidden) {
			t.Fatalf("hello main contains forbidden shortcut %q", forbidden)
		}
	}
}
```

**Steps:**

- [ ] **Step 1: Add integration test**

Add `TestHelloUsesProductionSubstrate` exactly as shown.

- [ ] **Step 2: Run test to verify failure**

Run:

```bash
go test ./compiler -run TestHelloUsesProductionSubstrate -v
```

Expected: FAIL because hello still has compatibility memory setup.

- [ ] **Step 3: Rewrite hello source**

Use the expected source shape. Keep existing serial, EDU MSI, and ivshmem MSI-X behavior unchanged. Add timer subscription to the worker executor field:

```wrela
worker_ticks: TimerTickSubscription
```

In `program.wrela`, read one tick non-blockingly before entering the existing loop:

```wrela
let tick = self.worker_ticks.try_next()
if tick.has_message {
    self.serial_path.write(self.memory.bytes(value = "timer tick\n"))
}
```

Update `tests/e2e/hello_qemu_test.go` in `TestHelloQEMU` so the expected serial substrings are exactly:

```go
for _, want := range []string{"hello from wrela", "timer tick", "serial interrupt: !", "msi interrupt"} {
	if !strings.Contains(out, want) {
		t.Fatalf("serial output missing %q:\n%s", want, out)
	}
}
```

Do not add `"timer tick"` to `TestHelloInterruptsQEMU`; that test uses `tests/e2e/fixtures/hello_ivshmem/main.wrela`, not `examples/hello/main.wrela`.

- [ ] **Step 4: Run verification**

Run:

```bash
go test ./compiler -run 'TestHelloUsesProductionSubstrate|Integration' -v
go run ./cmd/wrela build --mode dev examples/hello/main.wrela -o build/hello.efi --report build/hello.report.json
rg -n '"authority_audit"|"memory_roots"|"wake_targets"' build/hello.report.json
git diff --check
```

Expected: tests PASS; build writes EFI and report; `rg` prints report keys.

- [ ] **Step 5: Commit**

```bash
git add examples/hello/main.wrela examples/hello/program.wrela compiler/integration_test.go tests/e2e/hello_qemu_test.go
git commit -m "feat: migrate hello to production substrate -Codex Automated"
```

**Acceptance Criteria:** Hello uses discovered physical memory roots and arenas; it has timer and placement wiring; existing serial/MSI/MSI-X behavior remains; report generation succeeds.

### Task 20: Integrated Production Substrate QEMU Fixture

**Description:** Add a dedicated e2e fixture that exercises AP startup, timer wake, shared interrupt routing, MSI, MSI-X, typed payload topics, bounded interrupt queues, and report generation in one image.

**Files:**
- Create: `tests/e2e/fixtures/production_substrate/main.wrela`
- Create: `tests/e2e/fixtures/production_substrate/program.wrela`
- Create: `tests/e2e/production_substrate_qemu_test.go`

**Code Examples:**

Test:

```go
// tests/e2e/production_substrate_qemu_test.go imports:
// bytes
// os
// path/filepath
// strings
// testing
// github.com/ryanwible/wrela3/compiler
// github.com/ryanwible/wrela3/compiler/qemu

func TestProductionSubstrateQEMU(t *testing.T) {
	if testing.Short() {
		t.Skip("QEMU e2e skipped in short mode")
	}
	deps := requireQEMUDeps(t, true)
	dir := t.TempDir()
	vars := filepath.Join(dir, "OVMF_VARS.fd")
	copyFile(t, deps.firmware.Vars, vars)
	efi := filepath.Join(dir, "production-substrate.efi")
	rep := filepath.Join(dir, "production-substrate.report.json")
	_, err := compiler.Build(compiler.BuildOptions{
		Mode: compiler.ModeDev,
		RootPath: "tests/e2e/fixtures/production_substrate/main.wrela",
		OutputPath: efi,
		ReportPath: rep,
		RepoRoot: ".",
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	output, err := qemu.Run(qemu.Options{
		QEMUBinary:          deps.qemuBin,
		OVMFCode:            deps.firmware.Code,
		OVMFVars:            vars,
		ESPDir:              filepath.Join(dir, "esp"),
		ImagePath:           efi,
		UseSerialPipe:       true,
		InputText:           "!",
		KeepInputOpen:       true,
		SuccessText:         "msix interrupt",
		Timeout:             qemuTimeout(),
		SMP:                 2,
		EnableEdu:           true,
		EnableIvshmemMsix:   true,
		IvshmemServerBinary: deps.ivshmemBin,
	})
	if err != nil {
		t.Fatalf("qemu failed: %v\nserial output:\n%s", err, output)
	}
	for _, want := range []string{
		"production substrate",
		"timer tick",
		"shared irq",
		"msi interrupt",
		"msix interrupt",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("serial output missing %q:\n%s", want, output)
		}
	}
	data, err := os.ReadFile(rep)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	for _, want := range []string{`"TimerTickPayload"`, `"interrupt_queues"`, `"wake_targets"`, `"irq.serial.rx"`, `"serial.rx"`} {
		if !bytes.Contains(data, []byte(want)) {
			t.Fatalf("report missing %s:\n%s", want, data)
		}
	}
}
```

Fixture source must include:

```wrela
let root_arena = root_region.create_arena(
    identity = ArenaIdentity(label = "production.root"),
    policy = ArenaPolicy(evict_cache_by_default = true)
)
let console_slot = hardware.executors.claim(identity = SlotIdentity(label = "console"))
let worker_slot = hardware.executors.claim(identity = SlotIdentity(label = "worker"))
let placement = discovery.cpus.require_min_count(count = 2).placement(panic = BootPanic())
placement.require_separate_physical_cores(a = console_slot, b = worker_slot)
let worker_memory = root_arena.executor_memory_near(
    owner = worker_slot,
    near = placement.cpu_for(slot = worker_slot),
    length = 0x200000,
    align = 4096
)
let timer = discovery.timers.require_periodic(period_us = 1000)
let worker_ticks = timer.subscribe(subscriber = worker_slot)
let shared_serial_route = discovery.interrupts.route_shared_irq(
    irq = 4,
    vector = InterruptVector(value = 0x40)
)
let serial_irq_source = shared_serial_route.claim_source(
    identity = InterruptSourceIdentity(label = "serial.rx")
)
let serial_irq_queue = root_arena.interrupt_queue(
    identity = QueueIdentity(label = "irq.serial.rx"),
    owner = console_slot,
    capacity = 64,
    payload = InterruptPayloadKind(kind = 1, size = 8, align = 8),
    overflow = InterruptOverflowPolicy(mode = 0)
)
```

The console executor constructor in `tests/e2e/fixtures/production_substrate/main.wrela` must pass these fields by name:

```wrela
serial_irq_source = serial_irq_source,
serial_irq_queue = serial_irq_queue,
worker_ticks = worker_ticks
```

`tests/e2e/fixtures/production_substrate/program.wrela` must add these fields to the executor type that owns `start fn run`:

```wrela
serial_irq_source: SharedInterruptSource
serial_irq_queue: InterruptQueue
worker_ticks: TimerTickSubscription
```

Import the field types:

```wrela
use { SharedInterruptSource } from machine.x86_64.interrupts
use { InterruptQueue } from machine.x86_64.interrupt_queue
use { TimerTickSubscription } from machine.x86_64.topic_payload
```

`worker_ticks` is consumed by the `try_next()` call below. `serial_irq_source` and `serial_irq_queue` are intentionally field-bound so semantic graph extraction and the report prove the shared route and bounded queue exist; this milestone does not require a runtime queue-drain API in the fixture body.

`tests/e2e/fixtures/production_substrate/program.wrela` must contain these exact serial writes, in addition to the migrated hello serial/MSI/MSI-X setup:

```wrela
start fn run(self) -> never {
    with self.memory.frame(length = 4096) as tick_frame {
        self.serial_path.write(self.memory.bytes(value = "production substrate\n"))
        let tick = self.worker_ticks.try_next()
        if tick.has_message {
            self.serial_path.write(self.memory.bytes(value = "timer tick\n"))
        }
        self.serial_path.write(self.memory.bytes(value = "shared irq\n"))
    }
    self.interrupts.initialize_for_com1_receive()
    self.pci_interrupts.configure_edu_msi_vector41()
    self.pci_interrupts.configure_ivshmem_msix_vector42()
    self.serial_path.enable_receive_interrupts()
    self.interrupts.enable_cpu_interrupts()
    self.edu_path.raise_test_interrupt()
    self.ivshmem_tx.ring_peer(peer_id = self.ivshmem_rx.position(), vector = 0)
    self.memory.halt_forever()
}
```

**Steps:**

- [ ] **Step 1: Create the fixture image source**

Create `tests/e2e/fixtures/production_substrate/main.wrela` by copying the migrated `examples/hello/main.wrela`, then apply only these exact substitutions and additions:

```text
image name: ProductionSubstrate
root arena label: "production.root"
console slot label: "console"
worker slot label: "worker"
interrupt queue label: "irq.serial.rx"
timer period: 1000 us
shared IRQ vector: 0x40
```

The file must contain every declaration in the fixture source snippet shown above. Do not invent alternate labels, vector values, queue capacity, payload size, or constructor field names.

- [ ] **Step 2: Create the fixture program source**

Create `tests/e2e/fixtures/production_substrate/program.wrela` by copying `examples/hello/program.wrela`, then add the exact executor fields, imports, and `start fn run` body shown above. The expected serial strings are not optional; the e2e test asserts them verbatim.

- [ ] **Step 3: Add build-and-report test first**

Before adding the QEMU run, add a test named `TestProductionSubstrateBuildsReport` in `tests/e2e/production_substrate_qemu_test.go` that calls `compiler.Build` with `ReportPath`, reads the report, and asserts these report substrings:

```go
for _, want := range []string{`"TimerTickPayload"`, `"interrupt_queues"`, `"wake_targets"`, `"irq.serial.rx"`, `"serial.rx"`} {
    if !bytes.Contains(data, []byte(want)) {
        t.Fatalf("report missing %s:\n%s", want, data)
    }
}
```

- [ ] **Step 4: Run build-and-report test**

Run:

```bash
go test ./tests/e2e -run TestProductionSubstrateBuildsReport -v
```

Expected: PASS. If it fails, fix only fixture source or report mapping named by the failure before adding QEMU coverage.

- [ ] **Step 5: Add QEMU test and verify failure or dependency**

Run:

```bash
go test ./tests/e2e -run TestProductionSubstrateQEMU -v
```

Expected before implementation is complete: FAIL with compile or missing output assertions. If QEMU/OVMF are unavailable, the failure must name the missing binary or firmware path.

- [ ] **Step 6: Run verification**

Run:

```bash
go test ./tests/e2e -run 'TestProductionSubstrateBuildsReport|TestProductionSubstrateQEMU|Hello' -v
git diff --check
```

Expected: QEMU tests PASS where dependencies are installed.

- [ ] **Step 7: Commit**

```bash
git add tests/e2e/fixtures/production_substrate/main.wrela tests/e2e/fixtures/production_substrate/program.wrela tests/e2e/production_substrate_qemu_test.go
git commit -m "test: add production substrate qemu coverage -Codex Automated"
```

**Acceptance Criteria:** One QEMU fixture covers the integrated substrate; report assertions prove typed payloads, interrupt queues, and wake targets are emitted.

### Task 21: Static Regression Scans And Documentation

**Description:** Add scans and docs that prevent reintroducing raw booted-image memory placement, q35-only assumptions, hidden scheduling, or silent report omissions.

**Files:**
- Modify: `docs/production-deferred-work.md`
- Modify: `docs/design/hello_world_design_doc.md`
- Create: `compiler/integration_substrate_scan_test.go`

**Code Examples:**

Static scan test:

```go
// compiler/integration_substrate_scan_test.go imports:
// io/fs
// os
// path/filepath
// strings
// testing

func TestProductionSubstrateRegressionScans(t *testing.T) {
	scans := []struct {
		name string
		root string
		forbidden []string
	}{
		{name: "raw memory shortcuts", root: "examples", forbidden: []string{"MutableBytes(address = 0x", "arena_base = 0x"}},
		{name: "hidden scheduler", root: "wrela", forbidden: []string{"class Scheduler", "RunnableQueue", "work_steal", "spawn_on_any_cpu"}},
		{name: "q35 assumptions", root: "wrela", forbidden: []string{"q35", "two-vCPU", "static q35"}},
	}
	for _, scan := range scans {
		t.Run(scan.name, func(t *testing.T) {
			text := readTreeForScan(t, scan.root)
			for _, forbidden := range scan.forbidden {
				if strings.Contains(text, forbidden) {
					t.Fatalf("%s contains forbidden %q", scan.root, forbidden)
				}
			}
		})
	}
}

func readTreeForScan(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "build" || d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".wrela") && !strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, ".md") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		b.Write(data)
		b.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatalf("scan %s: %v", root, err)
	}
	return b.String()
}
```

Doc replacement:

```markdown
Production memory is now physical-region authority first. Firmware-derived `PhysicalRegionAuthority` values create named root arenas; executors, queues, caches, AP records, and DMA-intended buffers claim bounded children; frame lifetimes remain checked with `with` frames; the image report is the audit surface for ownership and wake paths.
```

**Steps:**

- [ ] **Step 1: Add scan test**

Add `compiler/integration_substrate_scan_test.go` with `TestProductionSubstrateRegressionScans`.

- [ ] **Step 2: Run test to verify current state**

Run:

```bash
go test ./compiler -run TestProductionSubstrateRegressionScans -v
```

Expected: PASS after Task 19. If it fails, remove only the regression introduced by this milestone.

- [ ] **Step 3: Update docs**

Add the doc replacement paragraph to both docs and remove language that describes physical arena, AP startup, discovery, timer, interrupt, or executor placement work as deferred.

- [ ] **Step 4: Run verification**

Run:

```bash
go test ./compiler -run TestProductionSubstrateRegressionScans -v
rg -n 'physical-region authority first|image report is the audit surface|x2APIC|TimerTickPayload' docs/production-deferred-work.md docs/design/hello_world_design_doc.md
git diff --check
```

Expected: test PASS; `rg` prints doc lines.

- [ ] **Step 5: Commit**

```bash
git add docs/production-deferred-work.md docs/design/hello_world_design_doc.md compiler/integration_substrate_scan_test.go
git commit -m "docs: lock production substrate regression scans -Codex Automated"
```

**Acceptance Criteria:** Regression scans protect examples and source from raw memory shortcuts, hidden scheduler vocabulary, and q35-only text; docs describe the implemented substrate accurately.

### Task 22: Full Plan Acceptance Sweep

**Description:** Confirm every phase composes and produce final verification evidence.

**Files:**
- No source edits expected.

**Code Examples:**

Full command sequence:

```bash
go test ./...
go test ./tests/e2e -run 'Hello|ProductionSubstrate' -v
go run ./cmd/wrela build --mode dev examples/hello/main.wrela -o build/hello.efi --report build/hello.report.json
rg -n '"authority_audit"|"memory_roots"|"arenas"|"hardware_claims"|"interrupts"|"timers"|"queues"|"topics"|"wake_targets"|"dma_buffers"' build/hello.report.json
rg -n 'MutableBytes\(address = 0x|arena_base = 0x|q35|two-vCPU|apStartupDelayLoopCount' examples tests/e2e/fixtures wrela/machine wrela/platform/hardware compiler/codegen docs/runtime docs/production-deferred-work.md docs/design/hello_world_design_doc.md
git diff --check
```

Approved final scan matches:

```text
docs/runtime/ap-startup-contract.md: mentions the low-page trampoline contract
compiler/codegen/vcpu_start.go: may contain apStartupReadyPollLimit
compiler/codegen/vcpu_start_test.go: may contain apStartupReadyPollLimit
```

No other matches are approved. `apStartupDelayLoopCount` is not approved anywhere; Task 10 replaces it with `apStartupReadyPollLimit`. `docs/implementation` and `docs/design/2026-05-17-production-substrate-convergence-design.md` are intentionally excluded because they contain historical examples and plan text.

**Steps:**

- [ ] **Step 1: Run full unit suite**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 2: Run QEMU e2e suite**

Run:

```bash
go test ./tests/e2e -run 'Hello|ProductionSubstrate' -v
```

Expected: PASS where QEMU and OVMF are installed. If unavailable, record the exact missing dependency error in the PR body.

- [ ] **Step 3: Build hello with report**

Run:

```bash
go run ./cmd/wrela build --mode dev examples/hello/main.wrela -o build/hello.efi --report build/hello.report.json
```

Expected: stdout prints `build/hello.efi`; both files exist.

- [ ] **Step 4: Verify report authority sections**

Run:

```bash
rg -n '"authority_audit"|"memory_roots"|"arenas"|"hardware_claims"|"interrupts"|"timers"|"queues"|"topics"|"wake_targets"|"dma_buffers"' build/hello.report.json
```

Expected: matches for every listed key.

- [ ] **Step 5: Verify regression scan manually**

Run:

```bash
rg -n 'MutableBytes\(address = 0x|arena_base = 0x|q35|two-vCPU|apStartupDelayLoopCount' examples tests/e2e/fixtures wrela/machine wrela/platform/hardware compiler/codegen docs/runtime docs/production-deferred-work.md docs/design/hello_world_design_doc.md
```

Expected: only approved final scan matches listed above.

- [ ] **Step 6: Run diff hygiene**

Run:

```bash
git diff --check
```

Expected: no output.

- [ ] **Step 7: Commit acceptance notes only if files changed**

If no files changed during this sweep, no commit is required. If dependency notes were added to docs, commit:

```bash
git add docs/production-deferred-work.md
git commit -m "chore: record production substrate acceptance notes -Codex Automated"
```

**Acceptance Criteria:** Full unit suite passes; QEMU coverage passes where available; report contains every authority audit section; scan output has only approved matches; no diff hygiene errors remain.

---

## 10. Appendix A: Exact Diagnostic Meanings

```text
SEM0056 physical region authority cannot be forged
SEM0057 duplicate arena identity
SEM0058 statically overlapping arena placement
SEM0059 arena allocation exceeds static parent bounds
SEM0060 interrupt queue overflow policy is missing or invalid
SEM0061 timer authority lacks explicit source or boot-fatal path
SEM0062 shared interrupt source identity is invalid or duplicated
SEM0063 APIC mode selection lacks fallback or required-mode fatal
SEM0064 required executor placement cannot be satisfied
SEM0065 preferred executor placement is not reportable
SEM0066 topic payload layout is not statically known
SEM0067 hidden scheduler construct is not allowed
SEM0068 DMA buffer requires explicit device owner
SEM0069 discovery capacity overflow must boot-fatal
SEM0070 duplicate timer claim
SEM0071 PCI bridge walking exceeded bounded recursion
SEM0072 unknown locality must be represented explicitly
SEM0073 x2APIC selection requires xAPIC fallback or required-mode fatal
SEM0074 AP startup contract is not enforced
SEM0075 authority audit report is incomplete
```

---

## 11. Appendix B: Runtime Algorithms

### Arena Allocation

```text
Inputs: parent_base, parent_length, next_offset, requested_length, requested_align
aligned_base = align_up(parent_base + next_offset, requested_align)
end_offset = (aligned_base - parent_base) + requested_length
if end_offset > parent_length: BootPanic.fail(0xAC070001)
next_offset = end_offset
return child/base allocation
```

### PCI Bridge Walk

```text
For each ECAM window:
  scan every bus in [start_bus, end_bus]
  scan every device 0..31
  scan function 0
  if multifunction bit set, scan functions 1..7
  when class 0x06/subclass 0x04 appears:
    read secondary/subordinate bus numbers from offset 0x18
    recursively scan that bounded bus range with depth + 1
  if depth > 8: BootPanic.fail(0xAC060016)
  if device capacity exceeds 16 in this milestone: BootPanic.fail(0xAC060012)
```

### Interrupt Queue Push

```text
next_tail = (tail + 1) % capacity
if next_tail == head:
  if overflow = drop_newest_and_set_flag:
    overflowed = true
    wake owner
    return
  if overflow = drop_oldest_and_set_flag:
    head = (head + 1) % capacity
    overflowed = true
  if overflow = set_flag_and_wake:
    overflowed = true
    wake owner
    return
  if overflow = boot_fatal:
    call _wrela_interrupt_queue_overflow
copy payload bytes into slot[tail]
tail = next_tail
wake owner
```

### Placement Fallback

```text
Required constraint:
  if facts prove success: record satisfied
  if facts prove failure or facts unknown: BootPanic.fail(0xAC040101)

Preferred constraint:
  if facts prove success: record satisfied
  if facts prove failure: record fallback = "constraint_unsatisfied"
  if facts unknown: record fallback = "unknown_locality"
```

---

## 12. Appendix C: Global Acceptance Criteria

- Booted examples no longer allocate root executor/device memory from raw address literals outside trusted platform construction.
- Physical region and arena authorities cannot be forged in ordinary modules.
- Bounded `with` frame values still cannot escape their frame.
- Statically knowable overlapping arena placements are rejected.
- Each booted image can emit a memory footprint report.
- Root executor memory is bounded and reported.
- Cache memory evicts by default.
- Discovery supports multiple ECAM windows and PCI bridge bus walking.
- Runtime placement consumes discovered CPU/APIC facts rather than static q35 CPU assumptions.
- Discovery exposes available CPU/cache/NUMA/device locality facts with explicit unknown values when firmware does not provide them.
- Required executor placement constraints boot-fatal when unsatisfied.
- Preferred executor placement hints appear in the image report with satisfied/fallback status.
- Executor memory can be allocated near a selected CPU/device when locality facts exist and falls back deterministically when unknown.
- AP startup uses the documented low-page trampoline contract with bounded ready polling.
- At least one timer source publishes timer events to an executor subscription.
- Shared interrupt routing is source-visible and preserves single hardware route ownership.
- Interrupt queues are bounded and have explicit overflow policy.
- x2APIC selection has xAPIC fallback or a required-mode boot-fatal path.
- Topic payload layout supports `TimerTickPayload`, a non-`U64` data payload with static layout.
- monitor/mwait selection is based on discovered CPU capability or explicit fallback wait path.
- The executor model still has no hidden scheduler, migration, or work stealing.
- The image report includes authority audit sections for memory roots, arenas, hardware claims, queues, timers, wake targets, and DMA-intended buffers.
- QEMU e2e covers AP startup, timer wake, shared interrupt routing, MSI, MSI-X, bounded topic payloads, and memory footprint reporting.
