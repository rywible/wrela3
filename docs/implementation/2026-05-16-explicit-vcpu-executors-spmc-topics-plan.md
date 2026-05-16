# Explicit vCPU Executors And SPMC Topics Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace direct executor calls and direct interrupt callbacks with explicit vCPU placement, executor slots, cache-line-aware SPMC topics, reliable bounded topics, and path-owned interrupt topics.

**Architecture:** Wrela keeps execution explicit: image code claims executor slots, wires memory/subscriptions/topics to those slots, then starts or enters one executor per vCPU. Topics are compiler-known static graph resources with generated cache-line-aligned writable storage. Interrupts are hard-cut over from executor `on path.interrupt` handlers to path-owned interrupt topics; generated ISR glue publishes events and wakes placed subscriber slots.

**Tech Stack:** Go 1.22+; existing hand-written lexer/parser; existing semantic checker and image graph; existing IR and direct x86_64 codegen; Wrela source modules under `wrela/`; Go unit tests; QEMU q35 + OVMF e2e tests with `-smp 2`.

---

## 0. How To Execute This Plan

This plan is written for implementation by engineers who know Go but may not know Wrela internals.

Execution rules:

- Follow task order unless the Parallel Work Map explicitly allows parallel work.
- Every task ends with a commit whose message ends in `-Codex Automated`.
- If a failing-test step passes unexpectedly, stop and inspect the existing implementation before editing.
- If a passing-test step fails, keep working inside the task until the command passes.
- Do not preserve backwards compatibility with the old `on path.interrupt` runtime model. This is a hard cutover.
- Do not invent generic syntax. Wrela has no generics in this plan.
- Do not add a hidden runtime scheduler. `start` and `enter` are dispatch operations only.
- Do not implement shared paths or ambient shared memory.

Existing test helpers used by this plan:

- `compiler/sem/testutil_test.go`: `parseModulesForTest`, `mustBuildIndex`, `mustCheck`, `hasCode`, `hasMessage`.
- `compiler/sem/uefi_source_shape_test.go`: `parseUEFIModuleSet`, `moduleType`, `methodByName`.
- `compiler/ir/interrupt_testutil_test.go`: `checkedProgramForTest`.
- `compiler/ir/lower_test.go`: `findFunction`.
- `compiler/codegen/*_test.go`: `symbolBytes` helpers already exist in codegen tests. When a target test file lacks that helper, add a local `symbolBytes(t, image, symbol)` helper in that file.

Definition of done for a task:

- All checkbox steps are complete.
- New tests fail before implementation.
- New tests pass after implementation.
- `git diff --check` passes.
- A commit is created with the exact message shown in the task.

Definition of done for the full plan:

- `go test ./...` passes.
- `go test ./tests/e2e -run 'MultiVcpu|Hello' -v` passes on a machine with QEMU/OVMF.
- `rg -nP "owner\\s*=\\s*hardware\\.vcpu|hardware\\.vcpu0\\.memory|^\\s*[A-Za-z_][A-Za-z0-9_]*\\.run\\(|^\\s*on\\s+[A-Za-z_][A-Za-z0-9_]*\\.interrupt\\b" examples tests/e2e/fixtures wrela` reports no old executor/interrupt wiring, except tests that intentionally assert rejection.
- The final examples use `ExecutorSlot`, typed identities, topic publishers/subscriptions, and `hardware.vcpuN.start/enter`.

---

## 1. Frozen Decisions

Do not reopen these decisions during implementation.

- One executor is placed on one vCPU.
- A vCPU starts at most one executor.
- An executor is started or entered exactly once.
- The current bootstrap vCPU uses terminal `enter(executor = ...)`.
- Non-current vCPUs use `start(executor = ...)`, which releases another vCPU and returns a `VcpuStartStatus`.
- There is no scheduler, migration, work stealing, runnable queue, or implicit multiplexing.
- `ExecutorSlot` is the source-visible identity and wiring handle for an executor before the executor value exists.
- Executor memory is claimed for a slot and may only be passed to the executor carrying that slot.
- Subscriptions name a subscriber slot and may only be passed to the executor carrying that slot.
- Path ownership is established by passing the path value into an executor constructor.
- Path capabilities are not shared.
- Path labels and slot labels are image-global. Duplicate labels are semantic errors.
- Topic storage is compiler-generated writable data for this plan. It is cache-line aligned and named from the required `TopicIdentity.label` field on each topic constructor.
- Wrela has no generic syntax in this plan. Implement concrete `U64GapTopic`, `U64ReliableTopic`, and concrete interrupt topic types such as `SerialRxTopic`.
- `Topic.publisher()` creates one unique publisher capability.
- Publisher authority may be owned by an executor slot or by a driver/path capability.
- Gap-detecting topics may overwrite old messages and subscribers detect gaps.
- Reliable bounded topics retain messages until required subscribers advance or producer backpressure is handled explicitly.
- Reliable producer backpressure waits on subscriber cursor advancement, not on new producer messages.
- Interrupts are hard-cut over to path-owned topics. Executor `on path.interrupt` handlers are rejected.
- Generated ISR glue must be tiny: capture event, publish topic event, acknowledge device/APIC, wake subscriber slot, return.
- AP trampoline byte generation is a separate artifact prerequisite. This plan consumes `compiler/codegen/testdata/ap_trampoline.bin`, embeds it, installs it, and verifies its contract; it does not ask a junior worker to invent real-mode trampoline bytes.
- Cache-line wait is the preferred backend abstraction. `HLT + IPI` is the required fallback.

---

## 2. Repository Layout And File Responsibilities

Create or modify the files listed below. Individual tasks repeat the exact subset they own.

```text
compiler/diag/codes.go
  Adds semantic diagnostics SEM0033-SEM0048 and codegen diagnostics for vCPU/topic work.

wrela/machine/x86_64/cpu_state.wrela
  Adds SlotIdentity, PathIdentity, ExecutorSlot, ExecutorRegistry, Vcpu, VcpuStartStatus, and OwnedHardware.executors/vcpu1.

wrela/machine/x86_64/executor_loop.wrela
  Adds loop policy marker classes HotPollPolicy, EventSleepPolicy, AdaptiveLoopPolicy, and TimerSleepPolicy.

wrela/machine/x86_64/topic_u64.wrela
  Adds TopicIdentity, U64GapTopic, U64GapPublisher, U64GapSubscription, U64ReliableTopic, U64ReliablePublisher, U64ReliableSubscription, and result data types.

wrela/machine/x86_64/serial.wrela
  Removes `owner: ExecutorPlacement` and direct interrupt receiver callback usage. Keeps existing SerialPathInterrupt as the serial receive event type. Adds SerialRxTopic, SerialRxPublisher, SerialRxSubscription, and path-owned interrupt topic declarations.

wrela/machine/x86_64/edu.wrela
wrela/machine/x86_64/ivshmem.wrela
  Migrates MSI/MSI-X paths from direct interrupt receivers to path-owned interrupt topics.

wrela/platform/uefi/types.wrela
compiler/codegen/entry_adapter.go
  Carries vCPU startup records, AP trampoline install, and per-vCPU entry metadata through owned-hardware transition.

compiler/sem/types.go
compiler/sem/image_graph.go
compiler/sem/check.go
compiler/sem/topic_graph.go
compiler/sem/topic_graph_test.go
compiler/sem/path_graph_test.go
compiler/sem/types_test.go
compiler/sem/interrupt_topic_test.go
  Adds slot/topic/path identity graph extraction and semantic checks.

compiler/ir/ir.go
compiler/ir/lower.go
compiler/ir/topic_test.go
compiler/ir/vcpu_test.go
  Adds IR operations for topic publish/try-next/arm/wait, reliable backpressure, vCPU start, and vCPU enter.

compiler/codegen/program.go
compiler/codegen/x64.go
compiler/codegen/topic_data.go
compiler/codegen/topic_test.go
compiler/codegen/vcpu_start.go
compiler/codegen/testdata/ap_trampoline.bin
compiler/codegen/vcpu_start_test.go
compiler/codegen/interrupt_test.go
  Emits topic storage, topic operations, wait primitives, AP startup/trampoline code, vCPU start/enter dispatch, and interrupt-topic dispatch.

compiler/qemu/run.go
compiler/qemu/run_test.go
  Adds SMP option and tests `-smp 2`.

examples/hello/main.wrela
examples/hello/program.wrela
tests/e2e/fixtures/multi_vcpu_topics/main.wrela
tests/e2e/fixtures/hello_ivshmem/main.wrela
tests/e2e/fixtures/hello_ivshmem/program.wrela
tests/e2e/hello_qemu_test.go
  Hard-cut examples and e2e fixtures to slots, topics, vCPU start/enter, and interrupt topics.

tests/fixtures/negative/on_interrupt_rejected.wrela
tests/fixtures/negative/duplicate_slot_label.wrela
tests/fixtures/negative/subscription_slot_mismatch.wrela
tests/fixtures/negative/vcpu_overcommit.wrela
tests/fixtures/negative/publisher_used_twice.wrela
tests/fixtures/negative/invalid_vcpu_enter_order.wrela
tests/fixtures/negative/missing_topic_identity.wrela
tests/fixtures/negative/reliable_try_publish_ignored.wrela
tests/fixtures/negative/subscription_used_by_wrong_executor.wrela
tests/fixtures/negative/lossy_topic_without_gap_tolerance.wrela
tests/fixtures/negative/sleeping_loop_without_wake_source.wrela
tests/fixtures/negative/executor_memory_slot_mismatch.wrela
compiler/negative_fixtures_test.go
  Adds negative fixtures for old `on`, duplicate labels, slot mismatch, publisher duplication, shared paths, vCPU overcommit, and invalid current-vCPU start order.
```

---

## 3. Parallel Work Map

This plan has one interface-freeze gate, then five mostly independent workstreams. Workers must not cross file ownership without a merge note in their commit message.

```text
Merge Gate A: Tasks 1, 2, and 7
  Freeze diagnostics, source/API shapes, and IR structs.

Semantic Graph Lane: Tasks 3, 4, 5, 6
  Owns compiler/sem/*.go, compiler/sem/*test.go, and negative fixtures.

Topic Codegen Lane: Tasks 8, 9, 10, 11, 12
  Owns compiler/codegen/topic_data.go, topic tests, and topic op emission in compiler/codegen/x64.go.

vCPU Codegen Lane: Tasks 13, 14A-14E
  Owns compiler/codegen/vcpu_start.go, vcpu tests, QEMU SMP, and UEFI AP trampoline install hooks.

Interrupt Topic Lane: Tasks 15A and 15B
  Owns interrupt binding structs, lowering, interrupt dispatch codegen, and serial/EDU/ivshmem interrupt source contracts.

Example/E2E Lane: Tasks 16, 17
  Owns examples/ and tests/e2e/ after Merge Gate D.
```

Merge gates:

- **Gate A:** Tasks 1, 2, and 7 complete. All lanes now share stable names for diagnostics, source contracts, and IR structs.
- **Gate B:** Semantic Graph Lane complete. Topic and interrupt lanes can rely on `ImageGraph.Topics`, `TopicSubscriptions`, `TopicPublishers`, and `VcpuPlacements`.
- **Gate C:** Topic Codegen Lane and vCPU Codegen Lane complete. E2E can start multi-vCPU fixture execution.
- **Gate D:** Interrupt Topic Lane complete. Hello and ivshmem fixture cutover can start.
- **Gate E:** Tasks 16 and 17 complete. Run Task 18 final verification.

Disjoint parallel starts:

- Task 1 and Task 13 can start immediately.
- Task 2 can split into four sub-workers after Task 1: `cpu_state`, `executor_loop`, `topic_u64`, and serial/interrupt source contracts. Merge as one commit.
- Task 4 starts after Task 3. Task 7 starts after Task 1. Task 14A starts after Task 13.
- Tasks 10 and 11 can run in parallel after Task 9 because gap and reliable op emission use different IR op types. Both touch `compiler/codegen/x64.go`, so merge through one owner.
- Task 16 fixture authoring can start after Task 3 as a compile-failing fixture, but it cannot be made passing until Gates C and D.

Per-task dependency table:

```text
Task 1:   depends on none
Task 2:   depends on Task 1
Task 3:   depends on Task 2
Task 4:   depends on Task 3
Task 5:   depends on Task 4
Task 6:   depends on Task 3; can run beside Task 5 with one check.go merge owner
Task 7:   depends on Task 1
Task 8:   depends on Tasks 4 and 7
Task 9:   depends on Task 7
Task 10:  depends on Tasks 8 and 9
Task 11:  depends on Tasks 8 and 9
Task 12:  depends on Tasks 8 and 9
Task 13:  depends on none
Task 14A: depends on Tasks 7 and 13
Task 14B: depends on Tasks 8 and 14A
Task 14C: depends on Task 14A
Task 14D: depends on Tasks 12 and 14C
Task 14E: depends on Tasks 14B and 14D
Task 15A: depends on Tasks 2 and 5
Task 15B: depends on Tasks 4, 8, 10, 12, and 15A
Task 16:  depends on Tasks 11, 14E, and 15B
Task 17:  depends on Tasks 15B and 16
Task 18:  depends on Task 17
```

---

## 4. Canonical Source Shape

This is the source shape the final implementation must compile.

```wrela
module tests.e2e.fixtures.multi_vcpu_topics.main

use { OwnedHardware, MemoryPlan, OwnedMemory, IoPortAuthority, CpuPlan, ExecutorSlot, PathIdentity } from machine.x86_64.cpu_state
use { ExecutorMemory, MutableBytes } from machine.x86_64.executor_memory
use { EventSleepPolicy } from machine.x86_64.executor_loop
use { U64GapPublisher, U64GapSubscription, U64ReliablePublisher, U64ReliableSubscription } from machine.x86_64.topic_u64
use { SerialDriver, SerialRegisters, SerialConsolePath } from machine.x86_64.serial
use { DelegatedHardware } from platform.uefi.transition

executor Producer {
    slot: ExecutorSlot
    loop: EventSleepPolicy
    memory: ExecutorMemory
    serial: SerialConsolePath
    out: U64GapPublisher
    result_in: U64ReliableSubscription

    start fn run(self) -> never {
        let value = 0
        while value < 64 {
            self.out.publish(value = value)
            value = value + 1
        }
        self.serial.write(self.memory.bytes(value = "producer published 64\n"))

        let done = false
        while done == false {
            let result = self.result_in.try_next()
            if result.has_message {
                if result.message.value == 64 {
                    self.serial.write(self.memory.bytes(value = "consumer received 64\n"))
                    done = true
                }
            }
            if done == false {
                self.result_in.arm_wait()
                let retry = self.result_in.try_next()
                if retry.has_message {
                    if retry.message.value == 64 {
                        self.serial.write(self.memory.bytes(value = "consumer received 64\n"))
                        done = true
                    }
                }
                if done == false {
                    if self.result_in.is_wait_armed() {
                        self.loop.wait()
                    }
                }
            }
        }
        self.memory.halt_forever()
    }
}

executor Consumer {
    slot: ExecutorSlot
    loop: EventSleepPolicy
    memory: ExecutorMemory
    input: U64GapSubscription
    result_out: U64ReliablePublisher

    start fn run(self) -> never {
        let received = 0
        while received < 64 {
            let next = self.input.try_next()
            while next.has_message {
                received = received + 1
                next = self.input.try_next()
            }
            self.input.arm_wait()
            let retry = self.input.try_next()
            while retry.has_message {
                received = received + 1
                retry = self.input.try_next()
            }
            if received < 64 {
                if self.input.is_wait_armed() {
                    self.loop.wait()
                }
            }
        }
        self.result_out.publish_or_wait(value = received)
        self.memory.halt_forever()
    }
}
```

Notes:

- The producer owns the only serial path in this fixture.
- The consumer reports completion through a reliable bounded result topic.
- `try_next()` is assigned to locals because current Wrela has no pattern matching and no `while let` syntax.
- `U64GapPublisher`, `U64GapSubscription`, and reliable variants are concrete v1 types, not generics.

---

## 5. Phase 1: Contracts And Source Surface

**Phase Description:** Establish the source-visible vocabulary and fixed diagnostic names before compiler behavior changes. This phase intentionally creates contracts that later phases lower as compiler intrinsics.

**Phase Acceptance Criteria:** The source index can resolve executor slots, vCPUs, loop policies, concrete U64 gap topics, concrete U64 reliable topics, serial path identity, and serial RX topic types.

### Task 1: Diagnostics For Slots, Topics, And vCPUs

**Description:** Reserve exact diagnostics for this plan so all later semantic tasks use stable codes.

**Files:**
- Modify: `compiler/diag/codes.go`
- Test: `compiler/diag/codes_test.go`

- [ ] **Step 1: Write the failing diagnostic test**

Append this test to `compiler/diag/codes_test.go`:

```go
func TestExecutorTopicDiagnosticCodesExist(t *testing.T) {
	codes := []string{
		SEM0033, // duplicate graph identity label
		SEM0034, // executor slot is unbound, rebound, or unplaced
		SEM0035, // executor slot mismatch
		SEM0036, // invalid vCPU start or enter
		SEM0037, // vCPU overcommit or insufficient target vCPUs
		SEM0038, // path shared across executors
		SEM0039, // topic publisher authority violation
		SEM0040, // topic subscription authority violation
		SEM0041, // topic delivery policy mismatch
		SEM0042, // old executor on-interrupt syntax is not supported
		SEM0043, // interrupt topic route is missing or ambiguous
		SEM0044, // sleeping loop has no wake source
		SEM0045, // reliable topic publish requires explicit backpressure handling
		SEM0046, // topic depth or cache-line layout is invalid
		SEM0047, // executor memory must be owned by executor slot
		SEM0048, // path identity required for publishing path
	}
	for _, code := range codes {
		if code == "" {
			t.Fatal("empty diagnostic code")
		}
	}
}
```

- [ ] **Step 2: Run the focused test and verify failure**

Run: `go test ./compiler/diag -run TestExecutorTopicDiagnosticCodesExist -v`

Expected: FAIL with undefined identifiers such as `SEM0033`.

- [ ] **Step 3: Add the diagnostic constants**

Add these constants after `SEM0032` in `compiler/diag/codes.go`:

```go
	SEM0033  = "SEM0033" // duplicate graph identity label
	SEM0034  = "SEM0034" // executor slot is unbound, rebound, or unplaced
	SEM0035  = "SEM0035" // executor slot mismatch
	SEM0036  = "SEM0036" // invalid vCPU start or enter
	SEM0037  = "SEM0037" // vCPU overcommit or insufficient target vCPUs
	SEM0038  = "SEM0038" // path shared across executors
	SEM0039  = "SEM0039" // topic publisher authority violation
	SEM0040  = "SEM0040" // topic subscription authority violation
	SEM0041  = "SEM0041" // topic delivery policy mismatch
	SEM0042  = "SEM0042" // old executor on-interrupt syntax is not supported
	SEM0043  = "SEM0043" // interrupt topic route is missing or ambiguous
	SEM0044  = "SEM0044" // sleeping loop has no wake source
	SEM0045  = "SEM0045" // reliable topic publish requires explicit backpressure handling
	SEM0046  = "SEM0046" // topic depth or cache-line layout is invalid
	SEM0047  = "SEM0047" // executor memory must be owned by executor slot
	SEM0048  = "SEM0048" // path identity required for publishing path
```

- [ ] **Step 4: Verify**

Run:

```bash
go test ./compiler/diag -run TestExecutorTopicDiagnosticCodesExist -v
git diff --check
```

Expected: PASS; `git diff --check` prints nothing.

- [ ] **Step 5: Commit**

```bash
git add compiler/diag/codes.go compiler/diag/codes_test.go
git commit -m "feat: reserve executor topic diagnostics -Codex Automated"
```

**Acceptance Criteria:** Diagnostics SEM0033-SEM0048 exist with the exact meanings above.

Diagnostic coverage is mandatory. Later tasks must add a failing test for each code before implementing the checker path:

```text
SEM0033 Task 5: duplicate slot/path/topic label
SEM0034 Task 5: slot unbound, rebound, or unplaced
SEM0035 Task 5: executor slot mismatch
SEM0036 Task 5: invalid vCPU start/enter ordering
SEM0037 Task 5: vCPU overcommit or insufficient target vCPUs
SEM0038 Task 5: shared path
SEM0039 Task 5: duplicate publisher authority
SEM0040 Task 5: subscription used outside subscriber executor
SEM0041 Task 5: lossy topic used by no-gap subscriber
SEM0042 Task 6: old executor on-interrupt syntax
SEM0043 Task 15B: interrupt route missing or ambiguous
SEM0044 Task 5: sleeping loop without wake source
SEM0045 Task 5: reliable publish result ignored
SEM0046 Task 9: topic depth/layout invalid
SEM0047 Task 5: executor memory owner slot mismatch
SEM0048 Task 5: publishing path missing explicit identity
```

### Task 2: Wrela Source Contracts For Slots, vCPUs, Loop Policies, And Topics

**Description:** Add the canonical Wrela source types used by the semantic graph and examples. These methods are source contracts; many become compiler intrinsics in later tasks.

**Files:**
- Modify: `wrela/machine/x86_64/cpu_state.wrela`
- Create: `wrela/machine/x86_64/executor_loop.wrela`
- Create: `wrela/machine/x86_64/topic_u64.wrela`
- Modify: `wrela/machine/x86_64/serial.wrela`
- Test: `compiler/sem/types_test.go`
- Test: `compiler/sem/uefi_source_shape_test.go`

- [ ] **Step 1: Write source-shape tests**

Append to `compiler/sem/types_test.go`:

```go
func TestExecutorTopicSourceSurface(t *testing.T) {
	modules := parseUEFIModuleSet(t)
	index := mustBuildIndex(t, modules)
	assertTypeFields(t, moduleType(t, index, "machine.x86_64.cpu_state", "SlotIdentity"), map[string]string{
		"label": "StringLiteral",
	})
	assertTypeFields(t, moduleType(t, index, "machine.x86_64.cpu_state", "ExecutorSlot"), map[string]string{
		"id": "U64",
	})
	assertTypeFields(t, moduleType(t, index, "machine.x86_64.cpu_state", "Vcpu"), map[string]string{
		"id": "U64",
	})
	assertMethodExists(t, moduleType(t, index, "machine.x86_64.cpu_state", "ExecutorRegistry"), "claim")
	assertTypeFields(t, moduleType(t, index, "machine.x86_64.topic_u64", "TopicIdentity"), map[string]string{
		"label": "StringLiteral",
	})
	assertMethodExists(t, moduleType(t, index, "machine.x86_64.topic_u64", "U64GapTopic"), "publisher")
	assertTypeFields(t, moduleType(t, index, "machine.x86_64.topic_u64", "U64GapTopic"), map[string]string{
		"identity": "TopicIdentity",
		"depth": "U64",
	})
	assertMethodExists(t, moduleType(t, index, "machine.x86_64.topic_u64", "U64GapTopic"), "subscribe")
	assertMethodExists(t, moduleType(t, index, "machine.x86_64.topic_u64", "U64ReliableTopic"), "publisher")
	assertTypeFields(t, moduleType(t, index, "machine.x86_64.topic_u64", "U64ReliableTopic"), map[string]string{
		"identity": "TopicIdentity",
		"depth": "U64",
	})
	assertMethodExists(t, moduleType(t, index, "machine.x86_64.topic_u64", "U64ReliablePublisher"), "try_publish")
	assertMethodExists(t, moduleType(t, index, "machine.x86_64.topic_u64", "U64ReliablePublisher"), "publish_or_wait")
	assertMethodExists(t, moduleType(t, index, "machine.x86_64.topic_u64", "U64ReliableSubscription"), "try_next")
	assertTypeFields(t, moduleType(t, index, "machine.x86_64.cpu_state", "PathIdentity"), map[string]string{
		"label": "StringLiteral",
	})
	assertTypeFields(t, moduleType(t, index, "machine.x86_64.serial", "SerialRxTopic"), map[string]string{
		"identity": "TopicIdentity",
		"id": "U64",
	})
	assertMethodExists(t, moduleType(t, index, "machine.x86_64.serial", "SerialRxTopic"), "publisher")
	assertMethodExists(t, moduleType(t, index, "machine.x86_64.serial", "SerialRxSubscription"), "try_next")
}
```

Also update `parseUEFIModuleSet` in `compiler/sem/uefi_source_shape_test.go` to include these paths:

```go
filepath.Join(repoRoot, "wrela/machine/x86_64/executor_loop.wrela"),
filepath.Join(repoRoot, "wrela/machine/x86_64/topic_u64.wrela"),
```

If `assertTypeFields` and `assertMethodExists` do not exist in the file, add helpers:

```go
func assertMethodExists(t *testing.T, typ *Type, name string) {
	t.Helper()
	if typ == nil {
		t.Fatalf("nil type, missing method %s", name)
	}
	for _, method := range typ.Methods {
		if method.Name == name {
			return
		}
	}
	t.Fatalf("%s.%s missing method %s", typ.Module, typ.Name, name)
}

func assertTypeFields(t *testing.T, typ *Type, want map[string]string) {
	t.Helper()
	if typ == nil {
		t.Fatal("nil type")
	}
	got := map[string]string{}
	for _, field := range typ.Fields {
		if field.Type != nil {
			got[field.Name] = field.Type.Name
		}
	}
	for name, wantType := range want {
		if got[name] != wantType {
			t.Fatalf("%s.%s field %s = %q, want %q", typ.Module, typ.Name, name, got[name], wantType)
		}
	}
}
```

- [ ] **Step 2: Run focused test and verify failure**

Run: `go test ./compiler/sem -run TestExecutorTopicSourceSurface -v`

Expected: FAIL because the new source types do not exist.

- [ ] **Step 3: Update `cpu_state.wrela`**

Add these source contracts to `wrela/machine/x86_64/cpu_state.wrela`:

```wrela
data SlotIdentity {
    label: StringLiteral
}

data PathIdentity {
    label: StringLiteral
}

data ExecutorSlot {
    id: U64
}

data VcpuStartStatus {
    started: Bool
    id: U64
}

class ExecutorRegistry {
    next_id: U64

    fn claim(self, identity: SlotIdentity) -> ExecutorSlot {
        let id = self.next_id
        self.next_id = self.next_id + 1
        return ExecutorSlot(id = id)
    }
}

class Vcpu {
    id: U64
}
```

Modify `OwnedHardware` to include:

```wrela
unique class OwnedHardware {
    memory: OwnedMemory
    io_ports: IoPortAuthority
    executors: ExecutorRegistry
    vcpu0: Vcpu
    vcpu1: Vcpu
}
```

Leave the old `ExecutorPlacement` declaration in `cpu_state.wrela` during Task 2 so existing fixtures still parse. Do not add fields or methods to it. Task 17 removes production uses and leaves the type unused outside negative tests.

- [ ] **Step 4: Create `executor_loop.wrela`**

```wrela
module machine.x86_64.executor_loop

class HotPollPolicy {
    fn wait(self) {
        self.pause()
    }

    asm fn pause(self) {
        pause
        ret
    }
}

class EventSleepPolicy {
    asm fn wait(self) {
        hlt
        ret
    }
}

class AdaptiveLoopPolicy {
    spins_before_sleep: U64

    fn wait(self) {
        let spins = self.spins_before_sleep
        while spins > 0 {
            self.pause()
            spins = spins - 1
        }
        self.sleep()
    }

    asm fn pause(self) {
        pause
        ret
    }

    asm fn sleep(self) {
        hlt
        ret
    }
}

class TimerSleepPolicy {
    deadline_ticks: U64

    asm fn wait(self) {
        hlt
        ret
    }
}
```

- [ ] **Step 5: Create `topic_u64.wrela`**

```wrela
module machine.x86_64.topic_u64

use { ExecutorSlot } from machine.x86_64.cpu_state

data TopicIdentity {
    label: StringLiteral
}

data U64TopicMessage {
    sequence: U64
    value: U64
}

data U64TopicNext {
    has_message: Bool
    gap: Bool
    missed: U64
    message: U64TopicMessage
}

data U64PublishResult {
    published: Bool
    full: Bool
}

class U64GapTopic {
    identity: TopicIdentity
    id: U64
    depth: U64

    fn publisher(self) -> U64GapPublisher {
        return U64GapPublisher(topic = self)
    }

    fn subscribe(self, subscriber: ExecutorSlot) -> U64GapSubscription {
        return U64GapSubscription(topic = self, subscriber = subscriber, cursor = 0, armed = false)
    }
}

class U64GapPublisher {
    topic: U64GapTopic

    fn publish(self, value: U64) {
        self.publish_intrinsic(value = value)
    }

    asm fn publish_intrinsic(self, value: U64) {
        ret
    }
}

class U64GapSubscription {
    topic: U64GapTopic
    subscriber: ExecutorSlot
    cursor: U64
    armed: Bool

    asm fn try_next(self) -> U64TopicNext {
        ret
    }

    fn arm_wait(self) {
        self.armed = true
    }

    fn is_wait_armed(self) -> Bool {
        return self.armed
    }
}

class U64ReliableTopic {
    identity: TopicIdentity
    id: U64
    depth: U64

    fn publisher(self) -> U64ReliablePublisher {
        return U64ReliablePublisher(topic = self)
    }

    fn subscribe(self, subscriber: ExecutorSlot) -> U64ReliableSubscription {
        return U64ReliableSubscription(topic = self, subscriber = subscriber, cursor = 0, armed = false)
    }
}

class U64ReliablePublisher {
    topic: U64ReliableTopic

    asm fn try_publish(self, value: U64) -> U64PublishResult {
        ret
    }

    fn publish_or_wait(self, value: U64) {
        let result = self.try_publish(value = value)
        while result.full {
            self.wait_for_subscriber_advance()
            result = self.try_publish(value = value)
        }
    }

    asm fn wait_for_subscriber_advance(self) {
        hlt
        ret
    }
}

class U64ReliableSubscription {
    topic: U64ReliableTopic
    subscriber: ExecutorSlot
    cursor: U64
    armed: Bool

    asm fn try_next(self) -> U64TopicNext {
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

Tasks 8, 10, and 11 lower these topic methods as compiler intrinsics. The source-level asm bodies are inert method declarations used only so the parser and semantic index know the public method contracts.

- [ ] **Step 6: Update `serial.wrela` source contracts**

Add:

```wrela
use { ExecutorSlot, PathIdentity } from machine.x86_64.cpu_state
use { TopicIdentity } from machine.x86_64.topic_u64

// Keep SerialPathInterrupt as the one serial receive event type. Include
// has_byte so the interrupt receiver can represent "no byte pending" without
// treating a real NUL byte as an empty event.
data SerialPathInterrupt {
    has_byte: Bool
    byte: U8
}

data SerialRxNext {
    has_message: Bool
    message: SerialPathInterrupt
}

class SerialRxTopic {
    identity: TopicIdentity
    id: U64

    fn publisher(self) -> SerialRxPublisher {
        return SerialRxPublisher(topic = self)
    }

    fn subscribe(self, subscriber: ExecutorSlot) -> SerialRxSubscription {
        return SerialRxSubscription(topic = self, subscriber = subscriber, cursor = 0, armed = false)
    }
}

class SerialRxPublisher {
    topic: SerialRxTopic
}

class SerialRxSubscription {
    topic: SerialRxTopic
    subscriber: ExecutorSlot
    cursor: U64
    armed: Bool

    asm fn try_next(self) -> SerialRxNext {
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

Modify `SerialConsolePath` to:

```wrela
driver path SerialConsolePath {
    identity: PathIdentity
    registers: SerialWriterRegisters
    rx: SerialRxPublisher
    ...
}
```

Remove `owner: ExecutorPlacement` from all serial path types in this task.

- [ ] **Step 7: Verify**

Run:

```bash
go test ./compiler/sem -run TestExecutorTopicSourceSurface -v
git diff --check
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add wrela/machine/x86_64/cpu_state.wrela wrela/machine/x86_64/executor_loop.wrela wrela/machine/x86_64/topic_u64.wrela wrela/machine/x86_64/serial.wrela compiler/sem/types_test.go compiler/sem/uefi_source_shape_test.go
git commit -m "feat: add executor slot topic source contracts -Codex Automated"
```

**Acceptance Criteria:** Source modules define typed slot/path/topic identities, concrete U64 gap and reliable topics, loop policies, vCPU contracts, and serial paths without `owner: ExecutorPlacement`.

---

## 6. Phase 2: Semantic Graph And Hard Cutover

**Phase Description:** Teach the semantic checker to extract the static executor/topic/vCPU graph and reject invalid ownership or placement before IR lowering. This phase also hard-rejects the old executor interrupt callback syntax.

**Phase Acceptance Criteria:** Invalid graph shapes produce SEM0033-SEM0048 diagnostics, `on path.interrupt` is rejected everywhere, and no compatibility path creates old executor interrupt bindings.

### Task 3: Type Classification For Slots, vCPUs, Topics, Publishers, And Subscriptions

**Description:** Add module-qualified helpers so later checks cannot be fooled by user-defined types with matching names.

**Files:**
- Modify: `compiler/sem/memory.go` or create `compiler/sem/topic_graph.go`
- Test: `compiler/sem/topic_graph_test.go`

- [ ] **Step 1: Write failing classification tests**

Create `compiler/sem/topic_graph_test.go`:

```go
package sem

import "testing"

func TestExecutorTopicKindClassification(t *testing.T) {
	cases := []struct {
		module string
		name   string
		fn     func(*Type) bool
	}{
		{"machine.x86_64.cpu_state", "ExecutorSlot", IsExecutorSlotType},
		{"machine.x86_64.cpu_state", "Vcpu", IsVcpuType},
		{"machine.x86_64.topic_u64", "U64GapTopic", IsTopicType},
		{"machine.x86_64.topic_u64", "U64ReliableTopic", IsTopicType},
		{"machine.x86_64.topic_u64", "U64GapPublisher", IsTopicPublisherType},
		{"machine.x86_64.topic_u64", "U64ReliablePublisher", IsTopicPublisherType},
		{"machine.x86_64.topic_u64", "U64GapSubscription", IsTopicSubscriptionType},
		{"machine.x86_64.topic_u64", "U64ReliableSubscription", IsTopicSubscriptionType},
		{"machine.x86_64.serial", "SerialRxTopic", IsTopicType},
		{"machine.x86_64.serial", "SerialRxPublisher", IsTopicPublisherType},
		{"machine.x86_64.serial", "SerialRxSubscription", IsTopicSubscriptionType},
		{"machine.x86_64.edu", "EduInterruptTopic", IsTopicType},
		{"machine.x86_64.edu", "EduInterruptPublisher", IsTopicPublisherType},
		{"machine.x86_64.edu", "EduInterruptSubscription", IsTopicSubscriptionType},
		{"machine.x86_64.ivshmem", "IvshmemDoorbellTopic", IsTopicType},
		{"machine.x86_64.ivshmem", "IvshmemDoorbellPublisher", IsTopicPublisherType},
		{"machine.x86_64.ivshmem", "IvshmemDoorbellSubscription", IsTopicSubscriptionType},
	}
	for _, tc := range cases {
		typ := &Type{Module: tc.module, Name: tc.name, Kind: KindClass}
		if !tc.fn(typ) {
			t.Fatalf("%s.%s was not classified", tc.module, tc.name)
		}
	}
	user := &Type{Module: "user.module", Name: "ExecutorSlot", Kind: KindClass}
	if IsExecutorSlotType(user) || IsTopicType(user) {
		t.Fatal("user shadow type must not gain executor/topic semantics")
	}
}
```

- [ ] **Step 2: Run test and verify failure**

Run: `go test ./compiler/sem -run TestExecutorTopicKindClassification -v`

Expected: FAIL with undefined helper functions.

- [ ] **Step 3: Implement helpers**

Create `compiler/sem/topic_graph.go` with:

```go
package sem

func qualifiedTypeName(t *Type) string {
	if t == nil {
		return ""
	}
	if t.Module == "" {
		return t.Name
	}
	return t.Module + "." + t.Name
}

func IsExecutorSlotType(t *Type) bool {
	return qualifiedTypeName(t) == "machine.x86_64.cpu_state.ExecutorSlot"
}

func IsVcpuType(t *Type) bool {
	return qualifiedTypeName(t) == "machine.x86_64.cpu_state.Vcpu"
}

func IsTopicType(t *Type) bool {
	switch qualifiedTypeName(t) {
	case "machine.x86_64.topic_u64.U64GapTopic",
		"machine.x86_64.topic_u64.U64ReliableTopic",
		"machine.x86_64.serial.SerialRxTopic",
		"machine.x86_64.edu.EduInterruptTopic",
		"machine.x86_64.ivshmem.IvshmemDoorbellTopic":
		return true
	default:
		return false
	}
}

func IsTopicPublisherType(t *Type) bool {
	switch qualifiedTypeName(t) {
	case "machine.x86_64.topic_u64.U64GapPublisher",
		"machine.x86_64.topic_u64.U64ReliablePublisher",
		"machine.x86_64.serial.SerialRxPublisher",
		"machine.x86_64.edu.EduInterruptPublisher",
		"machine.x86_64.ivshmem.IvshmemDoorbellPublisher":
		return true
	default:
		return false
	}
}

func IsTopicSubscriptionType(t *Type) bool {
	switch qualifiedTypeName(t) {
	case "machine.x86_64.topic_u64.U64GapSubscription",
		"machine.x86_64.topic_u64.U64ReliableSubscription",
		"machine.x86_64.serial.SerialRxSubscription",
		"machine.x86_64.edu.EduInterruptSubscription",
		"machine.x86_64.ivshmem.IvshmemDoorbellSubscription":
		return true
	default:
		return false
	}
}

func IsLoopPolicyType(t *Type) bool {
	switch qualifiedTypeName(t) {
	case "machine.x86_64.executor_loop.HotPollPolicy",
		"machine.x86_64.executor_loop.EventSleepPolicy",
		"machine.x86_64.executor_loop.AdaptiveLoopPolicy",
		"machine.x86_64.executor_loop.TimerSleepPolicy":
		return true
	default:
		return false
	}
}
```

If `qualifiedTypeName` already exists in another file, reuse the existing function and do not duplicate it.

- [ ] **Step 4: Verify**

Run:

```bash
go test ./compiler/sem -run TestExecutorTopicKindClassification -v
git diff --check
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add compiler/sem/topic_graph.go compiler/sem/topic_graph_test.go
git commit -m "feat: classify executor topic types -Codex Automated"
```

**Acceptance Criteria:** Topic/slot/vCPU/loop semantics are module-qualified and user shadow types do not gain special behavior.

### Task 4: Extract Executor Slots, Topic Labels, Subscriptions, And vCPU Placement

**Description:** Extend `ImageGraph` so semantic checks, lowering, and codegen can see the whole static wiring graph.

**Files:**
- Modify: `compiler/sem/image_graph.go`
- Modify: `compiler/sem/check.go`
- Test: `compiler/sem/topic_graph_test.go`

- [ ] **Step 1: Write failing graph extraction test**

Append:

```go
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
    fn exit_to_owned_hardware(self) -> OwnedHardware {
        return OwnedHardware(executors = ExecutorRegistry(next_id = 0), vcpu0 = Vcpu(id = 0), vcpu1 = Vcpu(id = 1))
    }
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
    fn subscribe(self, subscriber: ExecutorSlot) -> U64GapSubscription { return U64GapSubscription(topic = self, subscriber = subscriber, cursor = 0, armed = false) }
}
class U64GapSubscription { topic: U64GapTopic; subscriber: ExecutorSlot; cursor: U64; armed: Bool }
`)
	src := `
module test.graph
use { ExecutorSlot, SlotIdentity, OwnedHardware } from machine.x86_64.cpu_state
use { U64GapSubscription, U64GapTopic, TopicIdentity } from machine.x86_64.topic_u64
use { EventSleepPolicy } from machine.x86_64.executor_loop
use { ExecutorMemory } from machine.x86_64.executor_memory

executor Worker {
    slot: ExecutorSlot
    loop: EventSleepPolicy
    memory: ExecutorMemory
    input: U64GapSubscription
    start fn run(self) -> never { while true {} }
}

image Img {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware { return hardware.exit_to_owned_hardware() }
    phase owned_hardware(hardware: OwnedHardware) -> never {
        let worker_slot = hardware.executors.claim(identity = SlotIdentity(label = "worker"))
        let topic = U64GapTopic(identity = TopicIdentity(label = "counter"), id = 0, depth = 64)
        let input = topic.subscribe(subscriber = worker_slot)
        let worker = Worker(slot = worker_slot, loop = EventSleepPolicy(), memory = ExecutorMemory(arena_base = 0, arena_length = 4096, next_offset = 0), input = input)
        hardware.vcpu1.start(executor = worker)
        while true {}
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
	if len(checked.ImageGraph.VcpuPlacements) != 1 || checked.ImageGraph.VcpuPlacements[0].VcpuID != 1 {
		t.Fatalf("vcpu placements = %#v", checked.ImageGraph.VcpuPlacements)
	}
}
```

- [ ] **Step 2: Run and verify failure**

Run: `go test ./compiler/sem -run TestExecutorSlotTopicGraphExtraction -v`

Expected: FAIL because `ImageGraph.ExecutorSlots`, `TopicSubscriptions`, and `VcpuPlacements` do not exist.

- [ ] **Step 3: Extend `ImageGraph`**

Add to `compiler/sem/image_graph.go`:

```go
type ExecutorSlotNode struct {
	Label string
	Binding string
	Span source.Span
}

type TopicNode struct {
	Label string
	Kind string // "gap_u64", "reliable_u64", "serial_rx"
	Binding string
	Span source.Span
}

type TopicPublisherNode struct {
	TopicLabel string
	OwnerKind string // "executor_slot" or "driver_path"
	OwnerLabel string
	Binding string
	Span source.Span
}

type TopicSubscriptionNode struct {
	TopicLabel string
	SubscriberLabel string
	Binding string
	Span source.Span
}

type SubscriptionUseNode struct {
	TopicLabel string
	SubscriberLabel string
	CurrentSlotLabel string
	Span source.Span
}

type ReliableTryPublishCallNode struct {
	ResultObserved bool
	Span source.Span
}

type PathNode struct {
	Label string
	Kind string
	Binding string
	PublishesInterrupts bool
	Span source.Span
}

type InterruptTopicRouteNode struct {
	Vector int
	PathLabel string
	PathBinding string
	ContextSymbol string
	PathFieldOffset int
	TopicLabel string
	TopicKind string
	EventType string
	EventFunctionSymbol string
	SubscriberSlots []string
	Span source.Span
}

type VcpuPlacementNode struct {
	VcpuID int
	ExecutorBinding string
	SlotLabel string
	Terminal bool
	Span source.Span
}
```

Extend `ImageGraph`:

```go
// Keep the existing Executors []ExecutorNode field and add these fields to
// the existing ExecutorNode struct:
//   SlotLabel string
//   LoopPolicy string
//   MemoryOwnerLabel string

	ExecutorSlots []ExecutorSlotNode
	Topics []TopicNode
	TopicPublishers []TopicPublisherNode
	TopicSubscriptions []TopicSubscriptionNode
	SubscriptionUses []SubscriptionUseNode
	ReliableTryPublishCalls []ReliableTryPublishCallNode
	Paths []PathNode
	InterruptTopicRoutes []InterruptTopicRouteNode
	VcpuPlacements []VcpuPlacementNode
```

Add lookup helpers in `compiler/sem/image_graph.go`:

```go
func (g ImageGraph) TopicByLabel(label string) TopicNode {
	for _, topic := range g.Topics {
		if topic.Label == label {
			return topic
		}
	}
	return TopicNode{}
}

func (g ImageGraph) ExecutorBySlot(slot string) ExecutorNode {
	for _, exec := range g.Executors {
		if exec.SlotLabel == slot {
			return exec
		}
	}
	return ExecutorNode{}
}

func (g ImageGraph) HasWakeSource(slot string) bool {
	for _, sub := range g.TopicSubscriptions {
		if sub.SubscriberLabel == slot {
			return true
		}
	}
	for _, route := range g.InterruptTopicRoutes {
		for _, subscriber := range route.SubscriberSlots {
			if subscriber == slot {
				return true
			}
		}
	}
	return false
}
```

- [ ] **Step 4: Implement extraction helpers**

In `compiler/sem/check.go`, when typing `LetStmt` and `CallExpr` inside `ContextImagePhaseDirect`, detect:

```text
hardware.executors.claim(identity = SlotIdentity(label = "..."))
topic.subscribe(subscriber = slotName)
topic.publisher()
hardware.vcpuN.start(executor = executorName)
hardware.vcpu0.enter(executor = executorName)
```

`Vcpu.start` and `Vcpu.enter` are semantic intrinsics. The checker must accept those two calls on `machine.x86_64.cpu_state.Vcpu` even though there is no source-declared generic method that can name every executor type. Return types are fixed: `start` returns `machine.x86_64.cpu_state.VcpuStartStatus`; `enter` returns `never`.

Use these helper shapes:

```go
func stringLiteralArg(expr *ast.ConstructorExpr, name string) (string, bool) {
	for _, arg := range expr.Args {
		if arg.Name != name {
			continue
		}
		lit, ok := arg.Value.(*ast.StringLiteral)
		if !ok {
			return "", false
		}
		return lit.Value, true
	}
	return "", false
}

func namedArgExpr(args []ast.NamedArg, name string) ast.Expr {
	for _, arg := range args {
		if arg.Name == name {
			return arg.Value
		}
	}
	return nil
}
```

When extracting `start/enter`, resolve the executor value through a new local-origin table on `Scope`. Add this exact metadata type in `compiler/sem/check.go`:

```go
type localOrigin struct {
	Type *Type
	Constructor *ast.ConstructorExpr
	FieldBindings map[string]string
	SlotLabel string
	TopicLabel string
	TopicKind string
	PathLabel string
	EventType string
	EventFunctionSymbol string
	VcpuID int
	HasVcpuID bool
}
```

Add these exact `Scope` helpers. Origins inherit through nested scopes the same way type and lifetime bindings do; do not store origins globally because shadowed locals inside `if`/`while` bodies must not rewrite outer bindings.

```go
func (s *Scope) DefineOrigin(name string, origin localOrigin) {
	if s == nil {
		return
	}
	s.origins[name] = origin
}

func (s *Scope) LookupOrigin(name string) (localOrigin, bool) {
	if s == nil {
		return localOrigin{}, false
	}
	if origin, ok := s.origins[name]; ok {
		return origin, true
	}
	if s.parent != nil {
		return s.parent.LookupOrigin(name)
	}
	return localOrigin{}, false
}
```

Add `origins map[string]localOrigin` to `Scope` and initialize it in `NewScope`.

Split extraction into these helpers and add one focused test for each helper before writing the helper:

```go
func (c *checker) originForLetValue(moduleName string, expr ast.Expr, valueType *Type, scope *Scope) localOrigin
func (c *checker) originForConstructor(moduleName string, expr *ast.ConstructorExpr, typ *Type, scope *Scope) localOrigin
func (c *checker) originForCall(moduleName string, expr *ast.CallExpr, valueType *Type, scope *Scope) localOrigin
func (c *checker) recordGraphFromLet(name string, origin localOrigin, span source.Span)
func (c *checker) recordGraphFromExprStmt(expr ast.Expr, scope *Scope)
```

Add these focused tests in `compiler/sem/topic_graph_test.go` before implementing each helper. Each test may use `checkedImageGraphForSource(t, src)` as a local helper that parses, indexes, checks, and returns `checked.ImageGraph`. `executorGraphFixture(body)` must wrap `body` in the same contract modules from Step 1, add a `Worker` executor with `slot`, `loop`, and `memory` fields, and define `registers = SerialWriterRegisters(port_base = 0x03f8)` before inserting the body.

```go
func TestOriginForConstructorRecordsExecutorFields(t *testing.T) {
	graph := checkedImageGraphForSource(t, executorGraphFixture(`
let slot = hardware.executors.claim(identity = SlotIdentity(label = "worker"))
let memory = hardware.memory.claim_executor_arena(owner = slot, length = 4096, align = 4096)
let worker = Worker(slot = slot, loop = EventSleepPolicy(), memory = memory)
`))
	exec := graph.ExecutorBySlot("worker")
	if exec.SlotLabel != "worker" || exec.MemoryOwnerLabel != "worker" || exec.LoopPolicy != "EventSleepPolicy" {
		t.Fatalf("bad executor origin: %#v", exec)
	}
}

func TestOriginForClaimRecordsSlotLabel(t *testing.T) {
	graph := checkedImageGraphForSource(t, executorGraphFixture(`
let slot = hardware.executors.claim(identity = SlotIdentity(label = "worker"))
`))
	if len(graph.ExecutorSlots) != 1 || graph.ExecutorSlots[0].Label != "worker" {
		t.Fatalf("slots = %#v", graph.ExecutorSlots)
	}
}

func TestOriginForTopicAndPathIdentity(t *testing.T) {
	graph := checkedImageGraphForSource(t, executorGraphFixture(`
let topic = U64GapTopic(identity = TopicIdentity(label = "counter"), id = 0, depth = 64)
let rx = SerialRxTopic(identity = TopicIdentity(label = "console.rx"), id = 1)
let path = SerialConsolePath(identity = PathIdentity(label = "console"), registers = registers, rx = rx.publisher())
`))
	if graph.TopicByLabel("counter").Label == "" || graph.TopicByLabel("console.rx").Label == "" {
		t.Fatalf("topics = %#v", graph.Topics)
	}
	if len(graph.Paths) != 1 || graph.Paths[0].Label != "console" {
		t.Fatalf("paths = %#v", graph.Paths)
	}
}

func TestOriginForSubscriptionAndPublisherRoutes(t *testing.T) {
	graph := checkedImageGraphForSource(t, executorGraphFixture(`
let slot = hardware.executors.claim(identity = SlotIdentity(label = "worker"))
let topic = U64GapTopic(identity = TopicIdentity(label = "counter"), id = 0, depth = 64)
let sub = topic.subscribe(subscriber = slot)
let pub = topic.publisher()
`))
	if len(graph.TopicSubscriptions) != 1 || graph.TopicSubscriptions[0].SubscriberLabel != "worker" {
		t.Fatalf("subscriptions = %#v", graph.TopicSubscriptions)
	}
	if len(graph.TopicPublishers) != 1 || graph.TopicPublishers[0].TopicLabel != "counter" {
		t.Fatalf("publishers = %#v", graph.TopicPublishers)
	}
}

func TestRecordGraphFromVcpuStartResolvesExecutorSlot(t *testing.T) {
	graph := checkedImageGraphForSource(t, executorGraphFixture(`
let slot = hardware.executors.claim(identity = SlotIdentity(label = "worker"))
let memory = hardware.memory.claim_executor_arena(owner = slot, length = 4096, align = 4096)
let worker = Worker(slot = slot, loop = EventSleepPolicy(), memory = memory)
hardware.vcpu1.start(executor = worker)
`))
	if len(graph.VcpuPlacements) != 1 || graph.VcpuPlacements[0].SlotLabel != "worker" || graph.VcpuPlacements[0].VcpuID != 1 {
		t.Fatalf("placements = %#v", graph.VcpuPlacements)
	}
}
```

The helper behavior is fixed:

- `originForConstructor` stores `Constructor`, `Type`, and `FieldBindings` for every named constructor argument whose value is a `NameExpr`.
- `originForConstructor` stores `TopicLabel` for `U64GapTopic`, `U64ReliableTopic`, `SerialRxTopic`, `EduInterruptTopic`, and `IvshmemDoorbellTopic` by reading `identity = TopicIdentity(label = "...")`. Missing identity on a publishing topic emits SEM0048.
- `originForConstructor` stores `PathLabel` for `SerialConsolePath`, `EduMsiPath`, and `IvshmemDoorbellPath` by reading `identity = PathIdentity(label = "...")`. Missing identity on a path that has an interrupt publisher emits SEM0048.
- `recordGraphFromLet` appends `ExecutorNode` when the let value constructs an executor. Resolve `slot`, `loop`, and `memory` constructor fields through local origins; set `LoopPolicy` to the concrete loop policy type name and `MemoryOwnerLabel` from the memory origin.
- `recordGraphFromLet` appends `PathNode` when the let value constructs a driver path. Set `PublishesInterrupts = true` for paths with a `rx` or `interrupt` publisher field.
- `originForCall` stores `SlotLabel` for `hardware.executors.claim(identity = SlotIdentity(label = "..."))`.
- `originForCall` stores `MemoryOwnerLabel` for `hardware.memory.claim_executor_arena(owner = slot_name, ...)` by resolving the owner slot origin.
- `originForCall` stores `SubscriberLabel` in `TopicSubscriptionNode` for `.subscribe(subscriber = slot_name)` by resolving the subscriber local origin.
- `originForCall` stores `TopicPublisherNode` for `.publisher()` and records the bound local name when the call is assigned.
- `recordGraphFromExprStmt` and expression lowering visitors append `SubscriptionUseNode` whenever `.try_next()`, `.arm_wait()`, or `.is_wait_armed()` is called on a subscription. Set `CurrentSlotLabel` from the executor currently being checked.
- The same visitors append `ReliableTryPublishCallNode` for `.try_publish(...)`; set `ResultObserved = true` only when the call is assigned to a local, returned, or used as the condition/source of an `if`/`while` expression.
- `recordGraphFromLet` appends `InterruptTopicRouteNode` for interrupt-capable path constructors. For `SerialConsolePath(identity = ..., rx = serial_rx_topic.publisher())`, set `PathLabel` from the path identity, `TopicLabel` from the publisher origin, `TopicKind = "serial_rx"`, `EventType = "machine.x86_64.serial.SerialPathInterrupt"`, and `EventFunctionSymbol = "_wrela_event_machine_x86_64_serial_SerialConsolePath_interrupt"`. For `EduMsiPath(..., interrupt = edu_interrupt_topic.publisher())`, set `TopicKind = "edu_interrupt"` and `EventType = "machine.x86_64.edu.EduInterrupt"`. For ivshmem, set `TopicKind = "ivshmem_doorbell"` and the concrete ivshmem event type.
- `recordGraphFromExprStmt` fills route vectors from explicit interrupt configurator calls: `initialize_for_com1_receive()` assigns vector `0x40` to the route whose path kind is `serial_rx`; `configure_edu_msi_vector41()` assigns vector `0x41` to `edu_interrupt`; the ivshmem MSI-X configurator assigns its explicit vector argument. If a route reaches lowering without a vector, Task 15B emits SEM0043.
- `recordGraphFromExprStmt` handles `hardware.vcpuN.start(executor = name)` and `hardware.vcpuN.enter(executor = name)`. It reads the executor origin, resolves `FieldBindings["slot"]`, resolves that slot origin, and appends `VcpuPlacementNode`.
- `hardware.vcpu0` and `hardware.vcpu1` are recognized directly from `FieldExpr{NameExpr("hardware"), "vcpu0|vcpu1"}` inside an owned_hardware phase; they do not need a local binding.

- [ ] **Step 5: Verify**

Run:

```bash
go test ./compiler/sem -run TestExecutorSlotTopicGraphExtraction -v
git diff --check
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add compiler/sem/image_graph.go compiler/sem/check.go compiler/sem/topic_graph_test.go
git commit -m "feat: extract executor topic graph -Codex Automated"
```

**Acceptance Criteria:** The semantic graph records executor slots, topic subscriptions, publishers, and vCPU placements with labels and source spans.

### Task 5: Enforce Slot, Path, Topic, And vCPU Wiring

**Description:** Reject invalid static graphs before lowering.

**Files:**
- Modify: `compiler/sem/check.go`
- Test: `compiler/sem/topic_graph_test.go`
- Create: `tests/fixtures/negative/duplicate_slot_label.wrela`
- Create: `tests/fixtures/negative/subscription_slot_mismatch.wrela`
- Create: `tests/fixtures/negative/vcpu_overcommit.wrela`
- Create: `tests/fixtures/negative/publisher_used_twice.wrela`
- Create: `tests/fixtures/negative/invalid_vcpu_enter_order.wrela`
- Create: `tests/fixtures/negative/missing_topic_identity.wrela`
- Create: `tests/fixtures/negative/reliable_try_publish_ignored.wrela`
- Create: `tests/fixtures/negative/subscription_used_by_wrong_executor.wrela`
- Create: `tests/fixtures/negative/lossy_topic_without_gap_tolerance.wrela`
- Create: `tests/fixtures/negative/sleeping_loop_without_wake_source.wrela`
- Create: `tests/fixtures/negative/executor_memory_slot_mismatch.wrela`

- [ ] **Step 1: Add graph diagnostic tests**

Update the `compiler/sem/topic_graph_test.go` import block to include `github.com/ryanwible/wrela3/compiler/diag`, then append this helper and test:

```go
func executorTopicGraphDiags(t *testing.T, body string) []diag.Diagnostic {
	t.Helper()
	modules := parseModulesForTest(t, `
module machine.x86_64.cpu_state

data SlotIdentity { label: StringLiteral }
data PathIdentity { label: StringLiteral }
data ExecutorSlot { id: U64 }
data VcpuStartStatus { started: Bool; id: U64 }
class ExecutorRegistry {
    next_id: U64
    fn claim(self, identity: SlotIdentity) -> ExecutorSlot { return ExecutorSlot(id = 0) }
}
class Vcpu {
    id: U64
}
class OwnedHardware {
    executors: ExecutorRegistry
    vcpu0: Vcpu
    vcpu1: Vcpu
}
unique class DelegatedHardware {
    fn exit_to_owned_hardware(self) -> OwnedHardware {
        return OwnedHardware(executors = ExecutorRegistry(next_id = 0), vcpu0 = Vcpu(id = 0), vcpu1 = Vcpu(id = 1))
    }
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
data U64TopicMessage { sequence: U64; value: U64 }
data U64TopicNext { has_message: Bool; gap: Bool; missed: U64; message: U64TopicMessage }
data U64PublishResult { published: Bool; full: Bool }
class U64GapTopic {
    identity: TopicIdentity
    id: U64
    depth: U64
    fn publisher(self) -> U64GapPublisher { return U64GapPublisher(topic = self) }
    fn subscribe(self, subscriber: ExecutorSlot) -> U64GapSubscription { return U64GapSubscription(topic = self, subscriber = subscriber, cursor = 0, armed = false) }
}
class U64GapPublisher { topic: U64GapTopic }
class U64GapSubscription { topic: U64GapTopic; subscriber: ExecutorSlot; cursor: U64; armed: Bool }
`, `
module machine.x86_64.serial
use { PathIdentity } from machine.x86_64.cpu_state
driver path SerialConsolePath { identity: PathIdentity }
`, `
module test.invalid_graph
use { ExecutorSlot, SlotIdentity, PathIdentity, OwnedHardware } from machine.x86_64.cpu_state
use { ExecutorMemory } from machine.x86_64.executor_memory
use { EventSleepPolicy } from machine.x86_64.executor_loop
use { U64GapPublisher, U64GapSubscription, U64GapTopic, TopicIdentity } from machine.x86_64.topic_u64
use { SerialConsolePath } from machine.x86_64.serial

executor Worker {
    slot: ExecutorSlot
    loop: EventSleepPolicy
    memory: ExecutorMemory
    input: U64GapSubscription
    output: U64GapPublisher
    serial: SerialConsolePath
    start fn run(self) -> never { while true {} }
}

image Bad {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.exit_to_owned_hardware()
    }
    phase owned_hardware(hardware: OwnedHardware) -> never {
`+body+`
        while true {}
    }
}
`)
	index := mustBuildIndex(t, modules)
	_, diags := Check(index, modules)
	return diags
}

func TestExecutorTopicGraphDiagnostics(t *testing.T) {
	cases := []struct {
		name string
		body string
		code string
	}{
		{
			name: "duplicate_slot_label",
			code: diag.SEM0033,
			body: `
        let first = hardware.executors.claim(identity = SlotIdentity(label = "worker"))
        let second = hardware.executors.claim(identity = SlotIdentity(label = "worker"))
`,
		},
		{
			name: "subscription_slot_mismatch",
			code: diag.SEM0035,
			body: `
        let slot_a = hardware.executors.claim(identity = SlotIdentity(label = "a"))
        let slot_b = hardware.executors.claim(identity = SlotIdentity(label = "b"))
        let topic = U64GapTopic(identity = TopicIdentity(label = "topic.mismatch"), id = 0, depth = 64)
        let input = topic.subscribe(subscriber = slot_a)
        let output = topic.publisher()
        let serial = SerialConsolePath(identity = PathIdentity(label = "console"))
        let worker = Worker(slot = slot_b, loop = EventSleepPolicy(), memory = ExecutorMemory(arena_base = 0, arena_length = 4096, next_offset = 0), input = input, output = output, serial = serial)
        hardware.vcpu1.start(executor = worker)
`,
		},
		{
			name: "path_shared_between_executors",
			code: diag.SEM0038,
			body: `
        let slot_a = hardware.executors.claim(identity = SlotIdentity(label = "a"))
        let slot_b = hardware.executors.claim(identity = SlotIdentity(label = "b"))
        let topic_a = U64GapTopic(identity = TopicIdentity(label = "topic.a"), id = 0, depth = 64)
        let topic_b = U64GapTopic(identity = TopicIdentity(label = "topic.b"), id = 1, depth = 64)
        let serial = SerialConsolePath(identity = PathIdentity(label = "console"))
        let a = Worker(slot = slot_a, loop = EventSleepPolicy(), memory = ExecutorMemory(arena_base = 0, arena_length = 4096, next_offset = 0), input = topic_a.subscribe(subscriber = slot_a), output = topic_a.publisher(), serial = serial)
        let b = Worker(slot = slot_b, loop = EventSleepPolicy(), memory = ExecutorMemory(arena_base = 4096, arena_length = 4096, next_offset = 0), input = topic_b.subscribe(subscriber = slot_b), output = topic_b.publisher(), serial = serial)
        hardware.vcpu0.enter(executor = a)
        hardware.vcpu1.start(executor = b)
`,
		},
		{
			name: "vcpu_overcommit",
			code: diag.SEM0037,
			body: `
        let slot_a = hardware.executors.claim(identity = SlotIdentity(label = "a"))
        let slot_b = hardware.executors.claim(identity = SlotIdentity(label = "b"))
        let topic_a = U64GapTopic(identity = TopicIdentity(label = "topic.a"), id = 0, depth = 64)
        let topic_b = U64GapTopic(identity = TopicIdentity(label = "topic.b"), id = 1, depth = 64)
        let serial_a = SerialConsolePath(identity = PathIdentity(label = "console.a"))
        let serial_b = SerialConsolePath(identity = PathIdentity(label = "console.b"))
        let a = Worker(slot = slot_a, loop = EventSleepPolicy(), memory = ExecutorMemory(arena_base = 0, arena_length = 4096, next_offset = 0), input = topic_a.subscribe(subscriber = slot_a), output = topic_a.publisher(), serial = serial_a)
        let b = Worker(slot = slot_b, loop = EventSleepPolicy(), memory = ExecutorMemory(arena_base = 4096, arena_length = 4096, next_offset = 0), input = topic_b.subscribe(subscriber = slot_b), output = topic_b.publisher(), serial = serial_b)
        hardware.vcpu1.start(executor = a)
        hardware.vcpu1.start(executor = b)
`,
		},
		{
			name: "publisher_used_twice",
			code: diag.SEM0039,
			body: `
        let slot_a = hardware.executors.claim(identity = SlotIdentity(label = "a"))
        let slot_b = hardware.executors.claim(identity = SlotIdentity(label = "b"))
        let topic = U64GapTopic(identity = TopicIdentity(label = "topic.shared"), id = 0, depth = 64)
        let publisher = topic.publisher()
        let serial_a = SerialConsolePath(identity = PathIdentity(label = "console.a"))
        let serial_b = SerialConsolePath(identity = PathIdentity(label = "console.b"))
        let a = Worker(slot = slot_a, loop = EventSleepPolicy(), memory = ExecutorMemory(arena_base = 0, arena_length = 4096, next_offset = 0), input = topic.subscribe(subscriber = slot_a), output = publisher, serial = serial_a)
        let b = Worker(slot = slot_b, loop = EventSleepPolicy(), memory = ExecutorMemory(arena_base = 4096, arena_length = 4096, next_offset = 0), input = topic.subscribe(subscriber = slot_b), output = publisher, serial = serial_b)
        hardware.vcpu0.enter(executor = a)
        hardware.vcpu1.start(executor = b)
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diags := executorTopicGraphDiags(t, tc.body)
			if !hasCode(diags, tc.code) {
				t.Fatalf("expected %s, got %#v", tc.code, diags)
			}
		})
	}
}
```

- [ ] **Step 2: Run graph diagnostics test and verify failure**

Run: `go test ./compiler/sem -run TestExecutorTopicGraphDiagnostics -v`

Expected: FAIL because the invalid graph cases do not yet produce the expected diagnostics.

- [ ] **Step 3: Add negative fixtures matching the graph diagnostics**

Create one complete `.wrela` fixture for each row below. Each fixture must include the same source-contract modules used by `executorTopicGraphDiags` in Step 1, then the invalid image body for that row, plus an exact expectation header:

```wrela
// expect: SEM0033: duplicate executor slot label "worker"
```

Use these fixture/header pairs:

```text
tests/fixtures/negative/duplicate_slot_label.wrela -> SEM0033 duplicate executor slot label "worker"
tests/fixtures/negative/subscription_slot_mismatch.wrela -> SEM0035 subscription subscriber slot does not match executor slot
tests/fixtures/negative/vcpu_overcommit.wrela -> SEM0037 vCPU 1 starts more than one executor
tests/fixtures/negative/publisher_used_twice.wrela -> SEM0039 topic publisher is assigned to more than one producer
tests/fixtures/negative/invalid_vcpu_enter_order.wrela -> SEM0036 current vCPU enter must be terminal
tests/fixtures/negative/missing_topic_identity.wrela -> SEM0048 publishing topic requires TopicIdentity(label = ...)
tests/fixtures/negative/reliable_try_publish_ignored.wrela -> SEM0045 reliable try_publish result must be inspected or use publish_or_wait
tests/fixtures/negative/subscription_used_by_wrong_executor.wrela -> SEM0040 subscription value used outside subscriber executor
tests/fixtures/negative/lossy_topic_without_gap_tolerance.wrela -> SEM0041 lossy topic requires gap-tolerant subscriber policy
tests/fixtures/negative/sleeping_loop_without_wake_source.wrela -> SEM0044 sleeping executor has no wake source
tests/fixtures/negative/executor_memory_slot_mismatch.wrela -> SEM0047 executor memory owner slot does not match executor slot
```

Run: `go test ./compiler -run TestNegativeFixtures -v`

Expected: FAIL before checker implementation, PASS after Step 4.

- [ ] **Step 4: Implement graph validation**

Add `checkExecutorTopicGraph()` and call it from `Check` after `checkExecutorWiring()`:

```go
func (c *checker) checkExecutorTopicGraph() {
	c.checkDuplicateLabels()
	c.checkSlotBindings()
	c.checkVcpuPlacements()
	c.checkSubscriptionSlotMatches()
	c.checkTopicPublisherUniqueness()
	c.checkSubscriptionUseSites()
	c.checkDeliveryPolicies()
	c.checkLoopWakeSources()
	c.checkReliablePublishResults()
	c.checkExecutorMemoryOwners()
	c.checkPublishingIdentities()
}
```

Required behavior:

- Duplicate slot/path/topic labels produce SEM0033.
- Slot not bound to exactly one executor produces SEM0034.
- Slot placed on no vCPU or more than one vCPU produces SEM0034.
- Subscription passed to executor with different slot produces SEM0035.
- More than one executor on one vCPU produces SEM0037.
- Publisher value passed to more than one producer field produces SEM0039.
- Current vCPU `enter` with any reachable source statement after it produces SEM0036.
- Subscription method calls outside the executor whose `slot` equals `SubscriberLabel` produce SEM0040.
- `U64GapSubscription` used by an executor whose loop policy is not gap-tolerant produces SEM0041. For this milestone, `HotPollPolicy` and `EventSleepPolicy` are gap-tolerant; `NoGapRequiredPolicy` in tests is not.
- Sleeping loop policy with no subscription, timer, or interrupt route for its slot produces SEM0044.
- Reliable `try_publish(...)` whose `U64PublishResult` is not assigned to a local or used in a conditional produces SEM0045.
- `ExecutorMemory` passed to an executor must have `owner` equal to the executor slot; mismatch produces SEM0047.
- Any publishing topic/path constructor without `TopicIdentity`/`PathIdentity` produces SEM0048.

Implement these helpers in `compiler/sem/check.go` with one failing test case from Step 1 before each helper body:

```go
func (c *checker) checkSubscriptionUseSites() {
	for _, use := range c.imageGraph.SubscriptionUses {
		if use.CurrentSlotLabel != use.SubscriberLabel {
			c.addDiag(use.Span, diag.SEM0040, "subscription value used outside subscriber executor")
		}
	}
}

func (c *checker) checkDeliveryPolicies() {
	for _, sub := range c.imageGraph.TopicSubscriptions {
		topic := c.imageGraph.TopicByLabel(sub.TopicLabel)
		exec := c.imageGraph.ExecutorBySlot(sub.SubscriberLabel)
		if topic.Kind == "gap_u64" && exec.LoopPolicy == "NoGapRequiredPolicy" {
			c.addDiag(sub.Span, diag.SEM0041, "lossy topic requires gap-tolerant subscriber policy")
		}
	}
}

func (c *checker) checkLoopWakeSources() {
	for _, exec := range c.imageGraph.Executors {
		if exec.LoopPolicy != "EventSleepPolicy" {
			continue
		}
		if !c.imageGraph.HasWakeSource(exec.SlotLabel) {
			c.addDiag(exec.Span, diag.SEM0044, "sleeping executor has no wake source")
		}
	}
}

func (c *checker) checkReliablePublishResults() {
	for _, call := range c.imageGraph.ReliableTryPublishCalls {
		if !call.ResultObserved {
			c.addDiag(call.Span, diag.SEM0045, "reliable try_publish result must be inspected or use publish_or_wait")
		}
	}
}

func (c *checker) checkExecutorMemoryOwners() {
	for _, exec := range c.imageGraph.Executors {
		if exec.MemoryOwnerLabel != exec.SlotLabel {
			c.addDiag(exec.Span, diag.SEM0047, "executor memory owner slot does not match executor slot")
		}
	}
}

func (c *checker) checkPublishingIdentities() {
	for _, topic := range c.imageGraph.Topics {
		if topic.Label == "" {
			c.addDiag(topic.Span, diag.SEM0048, "publishing topic requires TopicIdentity(label = ...)")
		}
	}
	for _, path := range c.imageGraph.Paths {
		if path.PublishesInterrupts && path.Label == "" {
			c.addDiag(path.Span, diag.SEM0048, "publishing path requires PathIdentity(label = ...)")
		}
	}
}
```

SEM0011 migration is not a search-and-replace. Existing SEM0011 checks split this way:

```text
"driver path is assigned to more than one executor" -> SEM0038 shared path
"driver path is not assigned to an executor" -> delete; ownership is now established by passing the path into exactly one executor constructor
"driver path assigned to executor was not constructed in the image phase" -> SEM0038 only if the same path value crosses two executor constructors; otherwise delete the old owner-field premise
```

Update `compiler/sem/path_graph_test.go` accordingly in this task.

- [ ] **Step 5: Verify**

Run:

```bash
go test ./compiler/sem -run 'TestExecutorTopicGraphDiagnostics|TestExecutorSlotTopicGraphExtraction|TestExecutorTopicKindClassification' -v
go test ./compiler -run TestNegativeFixtures -v
git diff --check
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add compiler/sem/check.go compiler/sem/topic_graph_test.go tests/fixtures/negative/duplicate_slot_label.wrela tests/fixtures/negative/subscription_slot_mismatch.wrela tests/fixtures/negative/vcpu_overcommit.wrela tests/fixtures/negative/publisher_used_twice.wrela tests/fixtures/negative/invalid_vcpu_enter_order.wrela tests/fixtures/negative/missing_topic_identity.wrela tests/fixtures/negative/reliable_try_publish_ignored.wrela tests/fixtures/negative/subscription_used_by_wrong_executor.wrela tests/fixtures/negative/lossy_topic_without_gap_tolerance.wrela tests/fixtures/negative/sleeping_loop_without_wake_source.wrela tests/fixtures/negative/executor_memory_slot_mismatch.wrela
git commit -m "feat: enforce executor topic graph -Codex Automated"
```

**Acceptance Criteria:** Invalid slot/topic/vCPU/path graphs fail with SEM0033-SEM0039 before IR lowering.

### Task 6: Hard Reject Executor `on path.interrupt`

**Description:** Remove the old source-level executor interrupt callback model. Interrupts must be modeled as path-owned topics.

**Files:**
- Modify: `compiler/sem/check.go`
- Test: `compiler/sem/interrupt_topic_test.go`
- Create: `tests/fixtures/negative/on_interrupt_rejected.wrela`

- [ ] **Step 1: Add fixture**

`tests/fixtures/negative/on_interrupt_rejected.wrela`:

```wrela
// expect: SEM0042: executor on interrupt handlers are no longer supported; use path-owned interrupt topics
module negative.on_interrupt_rejected

data Event { value: U64 }
driver path P {
    interrupt receiver -> Event { return Event(value = 0) }
}
executor E {
    p: P
    on p.interrupt(event: Event) { }
    start fn run(self) -> never { while true {} }
}
unique class DelegatedHardware { fn claim(self) -> OwnedHardware { return OwnedHardware() } }
unique class OwnedHardware {}
image Img {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware { return hardware.claim() }
    phase owned_hardware(hardware: OwnedHardware) -> never { while true {} }
}
```

- [ ] **Step 2: Run and verify failure**

Run: `go test ./compiler -run TestNegativeFixtures -v`

Expected: FAIL because old `on` handlers are still accepted.

- [ ] **Step 3: Add focused semantic test**

Create `compiler/sem/interrupt_topic_test.go`:

```go
package sem

import "testing"

func TestExecutorOnInterruptRejected(t *testing.T) {
	src := `
module test.on_interrupt
data Event { value: U64 }
driver path P { interrupt receiver -> Event { return Event(value = 0) } }
executor E {
    p: P
    on p.interrupt(event: Event) { }
    start fn run(self) -> never { while true {} }
}
`
	modules := parseModulesForTest(t, src)
	index := mustBuildIndex(t, modules)
	_, ds := Check(index, modules)
	if !hasCode(ds, "SEM0042") {
		t.Fatalf("expected SEM0042, got %#v", ds)
	}
}
```

- [ ] **Step 4: Reject `OnHandlerDecl`**

In semantic checking for executor declarations, emit SEM0042 for every `OnHandlerDecl`:

```go
for _, handler := range exec.OnHandlers {
	c.error(handler.SpanV, diag.SEM0042, "executor on interrupt handlers are no longer supported; use path-owned interrupt topics")
}
```

Remove or bypass creation of `OnHandlers` and old `InterruptBindings` from executor handlers. Leave parser support in place only so the semantic diagnostic can point at the old syntax.

- [ ] **Step 5: Verify**

Run:

```bash
go test ./compiler -run TestNegativeFixtures -v
go test ./compiler/sem -run Interrupt -v
git diff --check
```

Expected: negative fixture PASSes. Update every semantic test that currently expects successful `on path.interrupt` checking so it now expects SEM0042. Do not leave a test that asserts old `on` success.

- [ ] **Step 6: Commit**

```bash
git add compiler/sem/check.go compiler/sem/interrupt_topic_test.go tests/fixtures/negative/on_interrupt_rejected.wrela
git commit -m "feat: reject executor interrupt handlers -Codex Automated"
```

**Acceptance Criteria:** Executor `on path.interrupt` source is a semantic error everywhere.

---

## 7. Phase 3: IR And Lowering

**Phase Description:** Represent topic operations and vCPU dispatch as explicit IR nodes rather than ordinary method calls. This keeps codegen deterministic and prevents runtime behavior from being inferred from source method names.

**Phase Acceptance Criteria:** Gap publish, reliable publish, subscription drain, wait arming, vCPU start, and vCPU enter all lower to dedicated IR operations.

### Task 7: IR Operations For Topics And vCPU Dispatch

**Description:** Add explicit IR nodes for topic operations and vCPU start/enter so codegen does not infer behavior from ordinary calls.

**Files:**
- Modify: `compiler/ir/ir.go`
- Test: `compiler/ir/topic_test.go`
- Test: `compiler/ir/vcpu_test.go`

- [ ] **Step 1: Write failing IR shape tests**

Create `compiler/ir/topic_test.go`:

```go
package ir

import "testing"

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
```

Create `compiler/ir/vcpu_test.go`:

```go
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
```

- [ ] **Step 2: Run and verify failure**

Run: `go test ./compiler/ir -run 'TestTopicOpsDefineExpectedValues|TestVcpuDispatchOpsDefineShape' -v`

Expected: FAIL with undefined IR types.

- [ ] **Step 3: Add IR types**

Add to `compiler/ir/ir.go`:

```go
type TopicPublish struct {
	TopicLabel string
	Kind       string
	Value      Value
}
func (TopicPublish) isOperation() {}

type ReliableTopicTryPublish struct {
	TopicLabel string
	Value      Value
	Type       Type
}
func (ReliableTopicTryPublish) isValue() {}
func (ReliableTopicTryPublish) isOperation() {}

type ReliableTopicWaitForAdvance struct {
	TopicLabel string
}
func (ReliableTopicWaitForAdvance) isOperation() {}

type TopicTryNext struct {
	TopicLabel    string
	Subscription  Value
	Type          Type
}
func (TopicTryNext) isValue() {}
func (TopicTryNext) isOperation() {}

type TopicArmWait struct {
	TopicLabel   string
	Subscription Value
}
func (TopicArmWait) isOperation() {}

type TopicWait struct {
	SlotLabel string
	Policy    string
}
func (TopicWait) isOperation() {}

type VcpuStart struct {
	VcpuID    int
	Executor  Value
	SlotLabel string
	Type      Type
}
func (VcpuStart) isValue() {}
func (VcpuStart) isOperation() {}

type VcpuEnter struct {
	VcpuID    int
	Executor  Value
	SlotLabel string
}
func (VcpuEnter) isOperation() {}
```

Update `valuesDefinedBy` to return values for `ReliableTopicTryPublish`, `TopicTryNext`, and `VcpuStart`. `VcpuEnter` does not define a value because it is terminal.

Extend `Program`:

```go
	Topics []TopicLayout
	VcpuStarts []VcpuStartPlan
```

Add:

```go
type TopicLayout struct {
	Label string
	Kind string
	Depth uint64
	Subscribers []string
}

type VcpuStartPlan struct {
	VcpuID int
	SlotLabel string
	ExecutorType Type
	Terminal bool
}
```

- [ ] **Step 4: Verify**

Run:

```bash
go test ./compiler/ir -run 'TestTopicOpsDefineExpectedValues|TestVcpuDispatchOpsDefineShape' -v
git diff --check
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add compiler/ir/ir.go compiler/ir/topic_test.go compiler/ir/vcpu_test.go
git commit -m "feat: add topic vcpu ir ops -Codex Automated"
```

**Acceptance Criteria:** IR explicitly represents topic operations, reliable backpressure wait, vCPU start, and vCPU enter.

### Task 8: Lower Topic Calls And vCPU start/enter To IR

**Description:** Replace ordinary method-call lowering for topic and vCPU intrinsic calls with the IR operations from Task 7.

**Files:**
- Modify: `compiler/ir/lower.go`
- Test: `compiler/ir/topic_test.go`
- Test: `compiler/ir/vcpu_test.go`

- [ ] **Step 1: Add lowering tests**

Append to `compiler/ir/topic_test.go`:

```go
// Add fmt plus compiler/parse, compiler/sem, and compiler/source to the import block.
func checkedProgramFromSourcesForTest(t *testing.T, sources ...string) *sem.CheckedProgram {
	t.Helper()
	files := make([]*source.File, len(sources))
	for i, text := range sources {
		files[i] = source.NewFile(source.FileID(i+1), fmt.Sprintf("m%d.wrela", i), text)
	}
	modules, ds := parse.ParseGraph(source.Graph{Files: files})
	if len(ds) != 0 {
		t.Fatalf("parse diagnostics: %#v", ds)
	}
	index, ds := sem.BuildIndex(modules)
	ds = filterMissingImageDiagnostic(ds)
	if len(ds) != 0 {
		t.Fatalf("index diagnostics: %#v", ds)
	}
	checked, ds := sem.Check(index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}
	return checked
}

func TestLowerTopicPublishAndTryNext(t *testing.T) {
	checked := checkedProgramFromSourcesForTest(t, `
module machine.x86_64.cpu_state
data ExecutorSlot { id: U64 }
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
data U64TopicMessage { sequence: U64; value: U64 }
data U64TopicNext { has_message: Bool; gap: Bool; missed: U64; message: U64TopicMessage }
class U64GapTopic { identity: TopicIdentity; id: U64; depth: U64 }
class U64GapSubscription {
    topic: U64GapTopic
    subscriber: ExecutorSlot
    cursor: U64
    armed: Bool
    asm fn try_next(self) -> U64TopicNext { ret }
    fn arm_wait(self) { self.armed = true }
}
`, `
module test.lower_topic
use { ExecutorSlot } from machine.x86_64.cpu_state
use { ExecutorMemory } from machine.x86_64.executor_memory
use { EventSleepPolicy } from machine.x86_64.executor_loop
use { U64GapSubscription } from machine.x86_64.topic_u64
executor Worker {
    slot: ExecutorSlot
    loop: EventSleepPolicy
    memory: ExecutorMemory
    input: U64GapSubscription
    start fn run(self) -> never {
        let next = self.input.try_next()
        self.input.arm_wait()
        while true {}
    }
}
`)
	checked.ImageGraph = sem.ImageGraph{
		Topics: []sem.TopicNode{{Label: "counter", Kind: "gap_u64", Binding: "topic"}},
		TopicSubscriptions: []sem.TopicSubscriptionNode{{
			TopicLabel: "counter",
			SubscriberLabel: "worker",
			Binding: "Worker.input",
		}},
		Executors: []sem.ExecutorNode{{
			SlotLabel: "worker",
			LoopPolicy: "EventSleepPolicy",
			MemoryOwnerLabel: "worker",
		}},
	}
	if checked.ImageGraph.TopicSubscriptions[0].TopicLabel != "counter" {
		t.Fatalf("test must seed topic graph metadata")
	}
	program, ds := Lower(checked)
	if len(ds) != 0 { t.Fatalf("Lower diagnostics: %#v", ds) }
	fn := findFunction(program, "_wrela_method_test_lower_topic_Worker_run")
	if fn == nil { t.Fatal("missing Worker.run") }
	if !functionHasOp[TopicTryNext](fn) || !functionHasOp[TopicArmWait](fn) {
		t.Fatalf("missing topic ops: %#v", fn.Blocks)
	}
}
```

Append helper:

```go
func functionHasOp[T any](fn *Function) bool {
	for _, block := range fn.Blocks {
		for _, op := range block.Ops {
			if _, ok := op.(T); ok {
				return true
			}
		}
	}
	return false
}
```

- [ ] **Step 2: Run and verify failure**

Run: `go test ./compiler/ir -run TestLowerTopicPublishAndTryNext -v`

Expected: FAIL because calls lower as normal `Call` operations.

- [ ] **Step 3: Implement lowering special cases**

In `lowerExpr` after receiver type is known and before normal method lookup:

```go
if isIRTopicPublisher(recvType) && e.Method == "publish" {
	value, valueOps, _ := ctx.lowerExpr(moduleName, receiverType, scope, namedArgExpr(e.Args, "value"))
	ops := append(receiverOps, valueOps...)
	ops = append(ops, TopicPublish{TopicLabel: ctx.topicLabelForValue(receiver), Kind: "gap_u64", Value: value})
	return value, ops, ctx.resolveType(moduleName, "void")
}
if isIRReliablePublisher(recvType) && e.Method == "try_publish" {
	value, valueOps, _ := ctx.lowerExpr(moduleName, receiverType, scope, namedArgExpr(e.Args, "value"))
	resultType := ctx.resolveType("machine.x86_64.topic_u64", "U64PublishResult")
	op := ReliableTopicTryPublish{TopicLabel: ctx.topicLabelForValue(receiver), Value: value, Type: ctx.irType(resultType)}
	ops := append(receiverOps, valueOps...)
	ops = append(ops, op)
	return op, ops, resultType
}
if isIRTopicSubscription(recvType) && e.Method == "try_next" {
	resultType := ctx.resolveType("machine.x86_64.topic_u64", "U64TopicNext")
	op := TopicTryNext{TopicLabel: ctx.topicLabelForValue(receiver), Subscription: receiver, Type: ctx.irType(resultType)}
	ops := append(receiverOps, op)
	return op, ops, resultType
}
if isIRTopicSubscription(recvType) && e.Method == "arm_wait" {
	op := TopicArmWait{TopicLabel: ctx.topicLabelForValue(receiver), Subscription: receiver}
	ops := append(receiverOps, op)
	return receiver, ops, recvType
}
```

For vCPU calls:

```go
if isIRVcpu(recvType) && (e.Method == "start" || e.Method == "enter") {
	executor, executorOps, executorType := ctx.lowerExpr(moduleName, receiverType, scope, namedArgExpr(e.Args, "executor"))
	slotLabel := ctx.slotLabelForExecutorValue(executor)
	vcpuID := ctx.vcpuIDForValue(receiver)
	ops := append(receiverOps, executorOps...)
	if e.Method == "start" {
		statusType := ctx.resolveType("machine.x86_64.cpu_state", "VcpuStartStatus")
		op := VcpuStart{VcpuID: vcpuID, Executor: executor, SlotLabel: slotLabel, Type: ctx.irType(statusType)}
		ops = append(ops, op)
		return op, ops, statusType
	}
	ops = append(ops, VcpuEnter{VcpuID: vcpuID, Executor: executor, SlotLabel: slotLabel})
	return executor, ops, ctx.resolveType(moduleName, "never")
}
```

Add lowering cases for reliable producer wait and loop wait:

```go
if isIRReliablePublisher(recvType) && e.Method == "wait_for_subscriber_advance" {
	op := ReliableTopicWaitForAdvance{TopicLabel: ctx.topicLabelForValue(receiver)}
	ops := append(receiverOps, op)
	return receiver, ops, recvType
}
if isIRLoopPolicy(recvType) && e.Method == "wait" {
	slotLabel := ctx.currentExecutorSlotLabel(receiverType)
	op := TopicWait{SlotLabel: slotLabel, Policy: recvType.Name}
	ops := append(receiverOps, op)
	return receiver, ops, recvType
}
```

Do not lower `is_wait_armed()` specially. It remains a normal field-backed method from the Wrela source contract and compiles through ordinary call/field lowering until a later optimizer inlines it.

Implement `topicLabelForValue`, `slotLabelForExecutorValue`, `vcpuIDForValue`, and `currentExecutorSlotLabel` using metadata from `checked.ImageGraph`.

- [ ] **Step 4: Verify**

Run:

```bash
go test ./compiler/ir -run 'TestLowerTopicPublishAndTryNext|TestVcpuDispatchOpsDefineShape' -v
git diff --check
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add compiler/ir/lower.go compiler/ir/topic_test.go compiler/ir/vcpu_test.go
git commit -m "feat: lower topic vcpu intrinsics -Codex Automated"
```

**Acceptance Criteria:** Topic and vCPU intrinsic calls lower to dedicated IR, not ordinary method calls.

---

## 8. Phase 4: Topic Storage, Gap Topics, Reliable Topics, And Wait Primitives

**Phase Description:** Generate cache-line-aligned topic storage and emit concrete x86_64 behavior for bounded gap topics, reliable bounded topics, and wait primitives.

**Phase Acceptance Criteria:** Gap topics detect missed messages, reliable topics refuse to overwrite unread messages, producers can wait on subscriber cursor advancement, and the backend has both `hlt` fallback and monitor/mwait emission support.

### Task 9: Generate Cache-Line-Aligned Topic Storage

**Description:** Add compiler-generated `.data` layout for topics with producer sequence, subscriber cursor lines, waitlines, and ring slots.

**Files:**
- Create: `compiler/codegen/topic_data.go`
- Modify: `compiler/codegen/x64.go`
- Test: `compiler/codegen/topic_test.go`

- [ ] **Step 1: Write failing data layout test**

Create `compiler/codegen/topic_test.go`:

```go
package codegen

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/ir"
)

func TestTopicDataLayoutIsCacheLineAligned(t *testing.T) {
	program := &ir.Program{
		Topics: []ir.TopicLayout{{
			Label: "counter",
			Kind: "gap_u64",
			Depth: 64,
			Subscribers: []string{"worker"},
		}},
	}
	layouts := planTopicData(program)
	counter := layouts["counter"]
	if counter.ProducerSequenceOffset%64 != 0 {
		t.Fatalf("producer sequence offset = %d", counter.ProducerSequenceOffset)
	}
	if counter.SubscriberCursorOffsets["worker"]%64 != 0 {
		t.Fatalf("subscriber cursor offset = %d", counter.SubscriberCursorOffsets["worker"])
	}
	if counter.RingOffset%64 != 0 {
		t.Fatalf("ring offset = %d", counter.RingOffset)
	}
	if counter.TotalSize < 64+64+64*64 {
		t.Fatalf("topic size too small: %d", counter.TotalSize)
	}
}

func TestTopicDataLayoutOrderIsDeterministic(t *testing.T) {
	program := &ir.Program{
		Topics: []ir.TopicLayout{
			{Label: "z.topic", Kind: "gap_u64", Depth: 64, Subscribers: []string{"worker"}},
			{Label: "a.topic", Kind: "gap_u64", Depth: 64, Subscribers: []string{"worker"}},
		},
	}
	ordered := orderedTopicDataLayouts(program)
	if ordered[0].Label != "a.topic" || ordered[1].Label != "z.topic" {
		t.Fatalf("layouts not sorted: %#v", ordered)
	}
}

func TestTopicDataObjectStartsAligned(t *testing.T) {
	objects := []ir.DataObject{
		{Symbol: "prefix", Bytes: []byte{1}},
		{Symbol: "topic", Bytes: make([]byte, 64), Align: 64},
	}
	data := []byte{}
	offsets := appendDataObjects(&data, objects)
	if offsets["topic"]%64 != 0 {
		t.Fatalf("topic offset = %d, want 64-byte aligned", offsets["topic"])
	}
}

func TestTopicDataRejectsNonPowerOfTwoDepth(t *testing.T) {
	program := &ir.Program{
		Topics: []ir.TopicLayout{{Label: "bad.depth", Kind: "gap_u64", Depth: 63}},
	}
	_, ds := planTopicDataChecked(program)
	if !hasCode(ds, "SEM0046") {
		t.Fatalf("expected SEM0046, got %#v", ds)
	}
}
```

If `compiler/codegen/topic_test.go` does not already have `hasCode`, add:

```go
func hasCode(ds []diag.Diagnostic, code string) bool {
	for _, d := range ds {
		if string(d.Code) == code {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run and verify failure**

Run: `go test ./compiler/codegen -run TestTopicDataLayoutIsCacheLineAligned -v`

Expected: FAIL with undefined `planTopicData`.

- [ ] **Step 3: Implement topic layout**

Create `compiler/codegen/topic_data.go`:

```go
package codegen

import (
	"sort"

	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/ir"
)

const cacheLineSize = 64

type topicDataLayout struct {
	Label string
	Kind string
	Depth uint64
	ProducerSequenceOffset uint64
	RingOffset uint64
	SubscriberCursorOffsets map[string]uint64
	SubscriberWaitlineOffsets map[string]uint64
	TotalSize uint64
}

func alignUp64(v uint64) uint64 {
	if v%cacheLineSize == 0 {
		return v
	}
	return v + cacheLineSize - (v % cacheLineSize)
}

func planTopicData(program *ir.Program) map[string]topicDataLayout {
	layouts, ds := planTopicDataChecked(program)
	if len(ds) != 0 {
		return map[string]topicDataLayout{}
	}
	return layouts
}

func planTopicDataChecked(program *ir.Program) (map[string]topicDataLayout, []diag.Diagnostic) {
	out := map[string]topicDataLayout{}
	var ds []diag.Diagnostic
	for _, layout := range orderedTopicDataLayouts(program) {
		if layout.Depth == 0 || layout.Depth&(layout.Depth-1) != 0 {
			ds = append(ds, diag.Diagnostic{Phase: "cg", Code: diag.SEM0046, Message: "topic depth must be a power of two"})
			continue
		}
		out[layout.Label] = layout
	}
	return out, ds
}

func orderedTopicDataLayouts(program *ir.Program) []topicDataLayout {
	out := []topicDataLayout{}
	topics := append([]ir.TopicLayout{}, program.Topics...)
	sort.Slice(topics, func(i, j int) bool {
		return topics[i].Label < topics[j].Label
	})
	for _, topic := range topics {
		offset := uint64(0)
		layout := topicDataLayout{
			Label: topic.Label,
			Kind: topic.Kind,
			Depth: topic.Depth,
			SubscriberCursorOffsets: map[string]uint64{},
			SubscriberWaitlineOffsets: map[string]uint64{},
		}
		layout.ProducerSequenceOffset = offset
		offset += cacheLineSize
		for _, sub := range topic.Subscribers {
			offset = alignUp64(offset)
			layout.SubscriberCursorOffsets[sub] = offset
			offset += cacheLineSize
			offset = alignUp64(offset)
			layout.SubscriberWaitlineOffsets[sub] = offset
			offset += cacheLineSize
		}
		offset = alignUp64(offset)
		layout.RingOffset = offset
		offset += topic.Depth * cacheLineSize
		layout.TotalSize = alignUp64(offset)
		out = append(out, layout)
	}
	return out
}
```

- [ ] **Step 4: Wire generated data into `.data`**

Extend `buildData(program)` so every topic emits a zeroed writable data object:

```go
for _, layout := range orderedTopicDataLayouts(program) {
	symbol := "_wrela_topic_" + sanitizeSymbol(layout.Label)
	data := make([]byte, layout.TotalSize)
	objects = append(objects, ir.DataObject{Symbol: symbol, Bytes: data, Align: 64})
}
```

Add `Align uint64` to `ir.DataObject`. Update `appendDataObjects` so every object starts at its requested alignment:

```go
func appendDataObjects(out *[]byte, objects []ir.DataObject) map[string]uint64 {
	offsets := map[string]uint64{}
	for _, obj := range objects {
		align := obj.Align
		if align == 0 {
			align = 1
		}
		for uint64(len(*out))%align != 0 {
			*out = append(*out, 0)
		}
		offsets[obj.Symbol] = uint64(len(*out))
		*out = append(*out, obj.Bytes...)
	}
	return offsets
}
```

In the current `buildData(program)` implementation, append topic data objects after existing `program.WritableData` objects and before interrupt context/event objects so topic symbols have deterministic addresses before generated interrupt storage.

- [ ] **Step 5: Verify**

Run:

```bash
go test ./compiler/codegen -run TestTopicDataLayoutIsCacheLineAligned -v
git diff --check
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add compiler/codegen/topic_data.go compiler/codegen/x64.go compiler/codegen/topic_test.go
git commit -m "feat: emit topic data layout -Codex Automated"
```

**Acceptance Criteria:** Topic data is generated in writable data with 64-byte-aligned producer sequence, subscriber cursors, waitlines, and ring slots.

### Task 10: Emit Gap-Detecting U64 Topic Operations

**Description:** Implement publish, try-next, arm, and waitline wake for bounded gap-detecting topics.

**Files:**
- Modify: `compiler/codegen/x64.go`
- Modify: `compiler/codegen/topic_data.go`
- Test: `compiler/codegen/topic_test.go`

- [ ] **Step 1: Add codegen byte-shape test**

Append:

```go
func TestGapTopicPublishStoresSequenceAndValue(t *testing.T) {
	program := topicProgramForCodegenTest("gap_u64")
	image, ds := Compile(program)
	if len(ds) != 0 { t.Fatalf("Compile diagnostics: %#v", ds) }
	publish := symbolBytes(t, image, "publish_counter")
	for _, want := range [][]byte{
		{0x48, 0x89}, // mov store shape exists
	} {
		if !bytes.Contains(publish, want) {
			t.Fatalf("publish missing byte shape %x in %x", want, publish)
		}
	}
}
```

Add `topicProgramForCodegenTest` in the test file:

```go
func topicProgramForCodegenTest(kind string) *ir.Program {
	u64 := ir.Type{Name: "U64", Kind: ir.TypeKindPrimitive}
	var op ir.Operation
	if kind == "reliable_u64" {
		op = &ir.ReliableTopicTryPublish{TopicLabel: "counter", Value: &ir.Param{Symbol: "value", Type: u64}, Type: ir.Type{Name: "U64PublishResult", Kind: ir.TypeKindData}}
	} else {
		op = &ir.TopicPublish{TopicLabel: "counter", Kind: kind, Value: &ir.Param{Symbol: "value", Type: u64}}
	}
	return &ir.Program{
		Topics: []ir.TopicLayout{{Label: "counter", Kind: kind, Depth: 64, Subscribers: []string{"worker"}}},
		Functions: []ir.Function{{
			Symbol: "publish_counter",
			Return: ir.Type{Name: "void", Kind: ir.TypeKindPrimitive},
			Params: []ir.Value{&ir.Param{Symbol: "value", Type: u64}},
			Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
				op,
			}}},
		}},
	}
}
```

- [ ] **Step 2: Run and verify failure**

Run: `go test ./compiler/codegen -run TestGapTopicPublishStoresSequenceAndValue -v`

Expected: FAIL because topic ops are not emitted.

- [ ] **Step 3: Implement gap publish**

Add these switch cases inside `compileFunction` in `compiler/codegen/x64.go`, next to the existing IR operation cases:

```go
case ir.TopicPublish:
	emitTopicPublish(e, v, frame, ctx)
case ir.TopicTryNext:
	emitTopicTryNext(e, v, frame, ctx)
case ir.TopicArmWait:
	emitTopicArmWait(e, v, frame, ctx)
```

Add these helper signatures below the existing arena helpers in `compiler/codegen/x64.go`:

```go
func emitTopicPublish(e *Emitter, op ir.TopicPublish, frame Frame, ctx compileContext) {
	layout := ctx.TopicLayouts[op.TopicLabel]
	base := topicBaseSymbol(op.TopicLabel)
	emitMovDataAddressToReg(e, asm.MustLookup("rax"), base)
	emitRegRegMove(e, asm.MustLookup("r11"), asm.MustLookup("rax"))
	emitLoadMemToReg(e, asm.MustLookup("rax"), asm.MustLookup("r11"), int64(layout.ProducerSequenceOffset), 64)
	emitRegRegMove(e, asm.MustLookup("rbx"), asm.MustLookup("rax"))
	e.emitInstruction(asm.Instruction{Mnemonic: "add", Operands: []asm.Operand{asm.RegOperand{Reg: asm.MustLookup("rbx")}, asm.ImmOperand{Value: 1}}})
	emitTopicSlotAddress(e, layout, asm.MustLookup("rax"), asm.MustLookup("r12"))
	emitStoreMemFromReg(e, asm.MustLookup("r12"), 0, asm.MustLookup("rbx"), 64)
	emitLoadValue(e, frame, op.Value, asm.MustLookup("r10"))
	emitStoreMemFromReg(e, asm.MustLookup("r12"), 8, asm.MustLookup("r10"), 64)
	emitMFence(e)
	emitStoreMemFromReg(e, asm.MustLookup("r11"), int64(layout.ProducerSequenceOffset), asm.MustLookup("rbx"), 64)
	for _, subscriber := range layout.Subscribers {
		emitStoreMemFromReg(e, asm.MustLookup("r11"), int64(layout.SubscriberWaitlineOffsets[subscriber]), asm.MustLookup("rbx"), 64)
	}
}

func topicBaseSymbol(label string) string {
	return "_wrela_topic_" + sanitizeSymbol(label)
}

func emitTopicSlotAddress(e *Emitter, layout topicDataLayout, sequenceReg asm.Reg, dst asm.Reg) {
	mask := int64(layout.Depth - 1)
	emitRegRegMove(e, dst, sequenceReg)
	e.emitInstruction(asm.Instruction{Mnemonic: "and", Operands: []asm.Operand{asm.RegOperand{Reg: dst}, asm.ImmOperand{Value: mask}}})
	e.emitInstruction(asm.Instruction{Mnemonic: "shl", Operands: []asm.Operand{asm.RegOperand{Reg: dst}, asm.ImmOperand{Value: 6}}})
	e.emitInstruction(asm.Instruction{Mnemonic: "add", Operands: []asm.Operand{asm.RegOperand{Reg: dst}, asm.RegOperand{Reg: asm.MustLookup("r11")}}})
	e.emitInstruction(asm.Instruction{Mnemonic: "add", Operands: []asm.Operand{asm.RegOperand{Reg: dst}, asm.ImmOperand{Value: int64(layout.RingOffset)}}})
}

func emitMFence(e *Emitter) {
	e.emit(0x0F, 0xAE, 0xF0)
}
```

If `compileContext` does not yet carry topic layouts, add `TopicLayouts map[string]topicDataLayout` and populate it once from `planTopicData(program)` before compiling functions.

The helper must emit this exact behavior:

```text
base = &_wrela_topic_counter
seq = [base + producer_sequence]
next = seq + 1
slot = ring + ((seq % depth) * 64)
[slot + 0] = next sequence
[slot + 8] = value
release fence
[base + producer_sequence] = next
for each subscriber waitline:
    [waitline] = next
```

Use x86 instructions:

```asm
mov r11, topic_base
mov rax, [r11 + producer_sequence_offset]
mov rbx, rax
add rbx, 1
; compute slot index with div or mask if depth is power of two
; store sequence and value
mfence
mov [r11 + producer_sequence_offset], rbx
```

Depths must be powers of two for v1. Task 9 emits SEM0046 if a non-power-of-two depth appears while planning topic data.

- [ ] **Step 4: Implement gap try-next**

Emit this algorithm:

```text
expected = subscription.cursor + 1
producer = [topic.producer_sequence]
if producer < expected: return has_message=false
slot = ring + ((expected - 1) % depth) * 64
slot_seq = [slot + 0]
if slot_seq != expected:
    missed = producer - subscription.cursor
    subscription.cursor = producer
    return gap=true, has_message=false, missed=missed
value = [slot + 8]
subscription.cursor = expected
[subscriber_cursor_line] = expected
subscription.armed = false
return has_message=true, gap=false, message={sequence=expected,value=value}
```

Store the `U64TopicNext` result through this helper. It writes into the value slot for the `TopicTryNext` op; it does not use the function-level `RecordReturnSlot`.

```go
type topicNextResultRegs struct {
	HasMessage bool
	Gap        bool
	Missed     asm.Reg
	Sequence   asm.Reg
	Value      asm.Reg
}

func emitStoreTopicNextResult(e *Emitter, op ir.TopicTryNext, frame Frame, regs topicNextResultRegs) {
	slot, ok := frame.Slots[op]
	if !ok {
		panic("missing result slot for TopicTryNext")
	}
	nextInfo, ok := e.ctx.typeInfo(op.Type)
	if !ok {
		panic("missing TypeInfo for U64TopicNext")
	}
	msgInfo, ok := e.ctx.typeInfo(ir.Type{Module: "machine.x86_64.topic_u64", Name: "U64TopicMessage"})
	if !ok {
		panic("missing TypeInfo for U64TopicMessage")
	}

	emitStoreBoolField(e, slot+nextInfo.Fields["has_message"].Offset, regs.HasMessage)
	emitStoreBoolField(e, slot+nextInfo.Fields["gap"].Offset, regs.Gap)
	emitStoreMemFromReg(e, asm.MustLookup("rbp"), int64(slot+nextInfo.Fields["missed"].Offset), regs.Missed, 64)

	msgOffset := slot + nextInfo.Fields["message"].Offset
	emitStoreMemFromReg(e, asm.MustLookup("rbp"), int64(msgOffset+msgInfo.Fields["sequence"].Offset), regs.Sequence, 64)
	emitStoreMemFromReg(e, asm.MustLookup("rbp"), int64(msgOffset+msgInfo.Fields["value"].Offset), regs.Value, 64)
}

func emitStoreBoolField(e *Emitter, offset int, value bool) {
	imm := int64(0)
	if value {
		imm = 1
	}
	emitMovImmToReg(e, asm.MustLookup("rax"), imm)
	emitStoreMemFromReg(e, asm.MustLookup("rbp"), int64(offset), asm.MustLookup("rax"), 8)
}
```

For the `has_message=false` paths, pass zeroed scratch registers for `Missed`, `Sequence`, and `Value` unless the path returns `gap=true`; the gap path must pass the computed `missed` register and zeroed message registers.

- [ ] **Step 5: Implement arm**

`TopicArmWait` sets the subscription `armed` field to true and stores the current producer sequence into the subscriber waitline. The waitline value is a wake token, not semantic data.

- [ ] **Step 6: Verify**

Run:

```bash
go test ./compiler/codegen -run 'TestGapTopicPublishStoresSequenceAndValue|TestTopicDataLayoutIsCacheLineAligned' -v
git diff --check
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add compiler/codegen/x64.go compiler/codegen/topic_data.go compiler/codegen/topic_test.go
git commit -m "feat: emit gap topic operations -Codex Automated"
```

**Acceptance Criteria:** Gap topics publish U64 values, subscribers read in sequence, gaps are detected, cursors advance, and waitlines are touched on publish.

### Task 11: Emit Reliable Bounded U64 Topic Operations

**Description:** Implement reliable topic backpressure using subscriber cursor lines and explicit producer wait on cursor advancement.

**Files:**
- Modify: `compiler/codegen/x64.go`
- Modify: `compiler/codegen/topic_data.go`
- Test: `compiler/codegen/topic_test.go`

- [ ] **Step 1: Add reliable tests**

Append:

```go
func TestReliableTopicPublishChecksSlowestSubscriber(t *testing.T) {
	program := topicProgramForCodegenTest("reliable_u64")
	image, ds := Compile(program)
	if len(ds) != 0 { t.Fatalf("Compile diagnostics: %#v", ds) }
	publish := symbolBytes(t, image, "publish_counter")
	if !bytes.Contains(publish, []byte{0x48, 0x3B}) { // cmp r64, r/m64 shape
		t.Fatalf("reliable publish must compare against subscriber cursor: %x", publish)
	}
}

```

- [ ] **Step 2: Run and verify failure**

Run:

```bash
go test ./compiler/codegen -run TestReliableTopicPublishChecksSlowestSubscriber -v
```

Expected: FAIL because reliable publish does not inspect subscriber cursors.

- [ ] **Step 3: Implement reliable try_publish**

Add a compile switch case:

```go
case ir.ReliableTopicTryPublish:
	emitReliableTopicTryPublish(e, v, frame, ctx)
case ir.ReliableTopicWaitForAdvance:
	emitReliableTopicWaitForAdvance(e, v, frame, ctx)
```

Add this helper:

```go
func emitReliableTopicTryPublish(e *Emitter, op ir.ReliableTopicTryPublish, frame Frame, ctx compileContext) {
	layout := ctx.TopicLayouts[op.TopicLabel]
	base := topicBaseSymbol(op.TopicLabel)
	emitMovDataAddressToReg(e, asm.MustLookup("rax"), base)
	emitRegRegMove(e, asm.MustLookup("r11"), asm.MustLookup("rax"))
	emitLoadMemToReg(e, asm.MustLookup("rax"), asm.MustLookup("r11"), int64(layout.ProducerSequenceOffset), 64)
	emitMinSubscriberCursor(e, layout, asm.MustLookup("r13"))
	emitRegRegMove(e, asm.MustLookup("r14"), asm.MustLookup("rax"))
	e.emitInstruction(asm.Instruction{Mnemonic: "sub", Operands: []asm.Operand{asm.RegOperand{Reg: asm.MustLookup("r14")}, asm.RegOperand{Reg: asm.MustLookup("r13")}}})
	e.emitInstruction(asm.Instruction{Mnemonic: "cmp", Operands: []asm.Operand{asm.RegOperand{Reg: asm.MustLookup("r14")}, asm.ImmOperand{Value: int64(layout.Depth)}}})
	fullLabel := e.newLabel("reliable_full")
	doneLabel := e.newLabel("reliable_done")
	e.emitJcc(0x83, fullLabel) // jae

	emitRegRegMove(e, asm.MustLookup("rbx"), asm.MustLookup("rax"))
	e.emitInstruction(asm.Instruction{Mnemonic: "add", Operands: []asm.Operand{asm.RegOperand{Reg: asm.MustLookup("rbx")}, asm.ImmOperand{Value: 1}}})
	emitTopicSlotAddress(e, layout, asm.MustLookup("rax"), asm.MustLookup("r12"))
	emitStoreMemFromReg(e, asm.MustLookup("r12"), 0, asm.MustLookup("rbx"), 64)
	emitLoadValue(e, frame, op.Value, asm.MustLookup("r10"))
	emitStoreMemFromReg(e, asm.MustLookup("r12"), 8, asm.MustLookup("r10"), 64)
	emitMFence(e)
	emitStoreMemFromReg(e, asm.MustLookup("r11"), int64(layout.ProducerSequenceOffset), asm.MustLookup("rbx"), 64)
	for _, subscriber := range layout.Subscribers {
		emitStoreMemFromReg(e, asm.MustLookup("r11"), int64(layout.SubscriberWaitlineOffsets[subscriber]), asm.MustLookup("rbx"), 64)
	}
	emitStorePublishResult(e, op, frame, true, false)
	e.emitJmp(doneLabel)

	e.bindLabel(fullLabel)
	emitStorePublishResult(e, op, frame, false, true)
	e.bindLabel(doneLabel)
}

func emitMinSubscriberCursor(e *Emitter, layout topicDataLayout, dst asm.Reg) {
	first := layout.Subscribers[0]
	emitLoadMemToReg(e, dst, asm.MustLookup("r11"), int64(layout.SubscriberCursorOffsets[first]), 64)
	for _, subscriber := range layout.Subscribers[1:] {
		emitLoadMemToReg(e, asm.MustLookup("r10"), asm.MustLookup("r11"), int64(layout.SubscriberCursorOffsets[subscriber]), 64)
		e.emitInstruction(asm.Instruction{Mnemonic: "cmp", Operands: []asm.Operand{asm.RegOperand{Reg: dst}, asm.RegOperand{Reg: asm.MustLookup("r10")}}})
		keepLabel := e.newLabel("min_cursor_keep")
		e.emitJcc(0x86, keepLabel) // jbe
		emitRegRegMove(e, dst, asm.MustLookup("r10"))
		e.bindLabel(keepLabel)
	}
}
```

Add this helper next to the other value-result emitters:

```go
func emitStorePublishResult(e *Emitter, op ir.ReliableTopicTryPublish, frame Frame, published bool, full bool) {
	slot, ok := frame.Slots[op]
	if !ok {
		panic("missing result slot for ReliableTopicTryPublish")
	}
	publishedValue := int64(0)
	if published {
		publishedValue = 1
	}
	fullValue := int64(0)
	if full {
		fullValue = 1
	}
	emitMovImmToReg(e, asm.MustLookup("rax"), publishedValue)
	emitStoreMemFromReg(e, asm.MustLookup("rbp"), int64(slot+0), asm.MustLookup("rax"), 8)
	emitMovImmToReg(e, asm.MustLookup("rax"), fullValue)
	emitStoreMemFromReg(e, asm.MustLookup("rbp"), int64(slot+1), asm.MustLookup("rax"), 8)
}
```

Use the same `frame.Slots` lookup shape as `emitConstruct`. The two bool fields are stored at byte offsets 0 and 1 for `U64PublishResult`; `8` is the width in bits, not bytes. Add a focused unit assertion that the emitted stores for `published=true, full=false` write bytes `01 00`, not two eight-byte words.

This helper emits:

```text
producer = [producer_sequence]
min_cursor = min(all subscriber cursor lines)
if producer - min_cursor >= depth:
    return U64PublishResult(published=false, full=true)
next = producer + 1
slot = ring + ((producer % depth) * 64)
[slot + 0] = next
[slot + 8] = value
mfence
[producer_sequence] = next
wake subscriber waitlines
return U64PublishResult(published=true, full=false)
```

For one subscriber, `min_cursor` is that subscriber's cursor line. For multiple subscribers, emit a fixed unrolled minimum scan from `TopicLayout.Subscribers`.

- [ ] **Step 4: Implement wait_for_subscriber_advance**

Add a producer waitline to `topicDataLayout`:

```go
ProducerWaitlineOffset uint64
```

Allocate it after `ProducerSequenceOffset` and before subscriber cursor lines. Then implement:

```go
func emitReliableTopicWaitForAdvance(e *Emitter, op ir.ReliableTopicWaitForAdvance, frame Frame, ctx compileContext) {
	layout := ctx.TopicLayouts[op.TopicLabel]
	base := topicBaseSymbol(op.TopicLabel)
	emitMovDataAddressToReg(e, asm.MustLookup("rax"), base)
	emitRegRegMove(e, asm.MustLookup("r11"), asm.MustLookup("rax"))
	emitMinSubscriberCursor(e, layout, asm.MustLookup("r13"))
	emitStoreMemFromReg(e, asm.MustLookup("r11"), int64(layout.ProducerWaitlineOffset), asm.MustLookup("r13"), 64)
	emitHltWait(e)
}
```

The helper emits:

```text
arm producer waitline for this topic
re-read min_cursor
if capacity exists: return
hlt fallback wait
return
```

This task uses `hlt` fallback. Task 12 adds cache-line wait instruction selection.

- [ ] **Step 5: Verify**

Run:

```bash
go test ./compiler/codegen -run 'ReliableTopic|TopicData' -v
git diff --check
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add compiler/codegen/x64.go compiler/codegen/topic_data.go compiler/codegen/topic_test.go
git commit -m "feat: emit reliable topic operations -Codex Automated"
```

**Acceptance Criteria:** Reliable topics refuse to overwrite unread messages, report full, and expose explicit waiting on subscriber cursor advancement.

### Task 12: Wait Primitive Selection And HLT/IPI Fallback

**Description:** Add backend support for cache-line wait instructions when selected and required fallback support with `hlt` plus IPI wake.

**Files:**
- Modify: `compiler/codegen/x64.go`
- Create: `compiler/codegen/lapic.go`
- Create: `compiler/codegen/wait_test.go`

- [ ] **Step 1: Add wait byte tests**

Create `compiler/codegen/wait_test.go`:

```go
package codegen

import (
	"bytes"
	"testing"
)

func TestWaitFallbackEmitsHlt(t *testing.T) {
	unit := compileWaitFallbackUnitForTest()
	if !bytes.Contains(unit.Bytes, []byte{0xF4}) {
		t.Fatalf("fallback wait must emit hlt: %x", unit.Bytes)
	}
}

func TestMonitorMwaitBytesAreAvailable(t *testing.T) {
	unit := compileMonitorMwaitUnitForTest()
	for _, want := range [][]byte{
		{0x0F, 0x01, 0xC8}, // monitor
		{0x0F, 0x01, 0xC9}, // mwait
	} {
		if !bytes.Contains(unit.Bytes, want) {
			t.Fatalf("monitor/mwait unit missing %x in %x", want, unit.Bytes)
		}
	}
}
```

- [ ] **Step 2: Run and verify failure**

Run: `go test ./compiler/codegen -run 'TestWaitFallbackEmitsHlt|TestMonitorMwaitBytesAreAvailable' -v`

Expected: FAIL because helper units do not exist.

- [ ] **Step 3: Implement wait helpers**

Add helpers in `compiler/codegen/x64.go`:

```go
func emitHltWait(e *Emitter) {
	e.emit(0xF4) // hlt
}

func emitMonitorMwait(e *Emitter, addressReg asm.Reg) {
	// monitor uses rax/eax address, ecx extensions, edx hints.
	if addressReg.Name != "rax" {
		e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
			asm.RegOperand{Reg: asm.MustLookup("rax")},
			asm.RegOperand{Reg: addressReg},
		}})
	}
	e.emitInstruction(asm.Instruction{Mnemonic: "xor", Operands: []asm.Operand{asm.RegOperand{Reg: asm.MustLookup("ecx")}, asm.RegOperand{Reg: asm.MustLookup("ecx")}}})
	e.emitInstruction(asm.Instruction{Mnemonic: "xor", Operands: []asm.Operand{asm.RegOperand{Reg: asm.MustLookup("edx")}, asm.RegOperand{Reg: asm.MustLookup("edx")}}})
	e.emit(0x0F, 0x01, 0xC8) // monitor
	e.emitInstruction(asm.Instruction{Mnemonic: "xor", Operands: []asm.Operand{asm.RegOperand{Reg: asm.MustLookup("ecx")}, asm.RegOperand{Reg: asm.MustLookup("ecx")}}})
	e.emitInstruction(asm.Instruction{Mnemonic: "xor", Operands: []asm.Operand{asm.RegOperand{Reg: asm.MustLookup("eax")}, asm.RegOperand{Reg: asm.MustLookup("eax")}}})
	e.emit(0x0F, 0x01, 0xC9) // mwait
}
```

Use existing assembler support for register setup and emit the two monitor/mwait opcodes as raw bytes exactly as shown. This task does not add new assembler features.

- [ ] **Step 4: Wire `TopicWait`**

For `TopicWait`, use fallback `hlt` by default. Add `SlotVcpu map[string]int` to `compileContext`, populated from `program.VcpuStarts`.

Create `compiler/codegen/lapic.go`:

```go
package codegen

import "github.com/ryanwible/wrela3/compiler/asm"

const (
	lapicBase    = 0xFEE00000
	lapicICRLow  = 0x300
	lapicICRHigh = 0x310
)

func emitLapicWrite(e *Emitter, offset uint32, value uint32) {
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("r11")},
		asm.ImmOperand{Value: lapicBase},
	}})
	emitMovImmToReg(e, asm.MustLookup("rax"), int64(value))
	emitStoreMemFromReg(e, asm.MustLookup("r11"), int64(offset), asm.MustLookup("rax"), 32)
}
```

Add this helper for producer wake fanout in `compiler/codegen/x64.go`:

```go
func emitWakeSubscriberSlots(e *Emitter, subscribers []string, ctx compileContext) {
	for _, slot := range subscribers {
		vcpuID := ctx.SlotVcpu[slot]
		if vcpuID == 0 {
			continue
		}
		emitLapicWrite(e, lapicICRHigh, uint32(vcpuID)<<24)
		emitLapicWrite(e, lapicICRLow, 0x00004000|0xF0) // fixed delivery IPI vector 0xF0
	}
}
```

Put the LAPIC constants and `emitLapicWrite` in `compiler/codegen/lapic.go`; Task 14D reuses the same helper for AP startup. Call `emitWakeSubscriberSlots` after publishing to topic waitlines. Coalescing uses the subscriber waitline value: when the waitline already equals the new sequence, skip the IPI. This task emits the comparison inline before the LAPIC write.

- [ ] **Step 5: Verify**

Run:

```bash
go test ./compiler/codegen -run 'Wait|Topic' -v
git diff --check
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add compiler/codegen/x64.go compiler/codegen/lapic.go compiler/codegen/wait_test.go
git commit -m "feat: add topic wait primitives -Codex Automated"
```

**Acceptance Criteria:** Backend can emit `hlt` fallback and monitor/mwait byte sequences; topic waits use fallback by default and are structured for cache-line wait selection.

---

## 9. Phase 5: vCPU Topology And Startup

**Phase Description:** Add static two-vCPU topology support for q35/QEMU and implement explicit `start`/`enter` dispatch without a scheduler.

**Phase Acceptance Criteria:** QEMU can run with `-smp 2`, `hardware.vcpu1.start` releases APIC ID 1 into its executor, and `hardware.vcpu0.enter` is terminal.

### Task 13: QEMU SMP Option

**Description:** Allow e2e runs to request multiple virtual CPUs.

**Files:**
- Modify: `compiler/qemu/run.go`
- Test: `compiler/qemu/run_test.go`

- [ ] **Step 1: Add failing QEMU args test**

Append:

```go
func TestArgsIncludesSMPWhenRequested(t *testing.T) {
	args := Args(Options{ImagePath: "x.efi", ESPDir: "esp", OVMFCode: "code.fd", OVMFVars: "vars.fd", SMP: 2})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-smp 2") {
		t.Fatalf("args missing smp: %s", joined)
	}
}
```

- [ ] **Step 2: Run and verify failure**

Run: `go test ./compiler/qemu -run TestArgsIncludesSMPWhenRequested -v`

Expected: FAIL because `Options.SMP` does not exist.

- [ ] **Step 3: Add `SMP`**

Add to `qemu.Options`:

```go
SMP int
```

In `Args`:

```go
if opts.SMP > 0 {
	args = append(args, "-smp", strconv.Itoa(opts.SMP))
}
```

- [ ] **Step 4: Verify**

Run:

```bash
go test ./compiler/qemu -run TestArgsIncludesSMPWhenRequested -v
git diff --check
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add compiler/qemu/run.go compiler/qemu/run_test.go
git commit -m "feat: add qemu smp option -Codex Automated"
```

**Acceptance Criteria:** QEMU tests can request `-smp 2`.

### Task 14A: vCPU Startup Data Records

**Description:** Add deterministic startup data symbols for vCPU0 and vCPU1. This task is junior-safe because it only creates data layout and tests.

**Files:**
- Create: `compiler/codegen/vcpu_start.go`
- Create: `compiler/codegen/vcpu_start_test.go`
- Modify: `compiler/ir/ir.go`

- [ ] **Step 1: Add startup data tests**

Create `compiler/codegen/vcpu_start_test.go`:

```go
package codegen

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/ryanwible/wrela3/compiler/ir"
)

func TestVcpuStartupDataSymbolsAreDeterministic(t *testing.T) {
	program := &ir.Program{
		VcpuStarts: []ir.VcpuStartPlan{
			{VcpuID: 0, SlotLabel: "hello", Terminal: true},
			{VcpuID: 1, SlotLabel: "worker", Terminal: false},
		},
	}
	objects := vcpuStartupData(program)
	got := []string{}
	for _, obj := range objects {
		got = append(got, obj.Symbol)
		if obj.Align != 64 {
			t.Fatalf("%s align = %d, want 64", obj.Symbol, obj.Align)
		}
	}
	want := []string{
		"_wrela_vcpu0_ready",
		"_wrela_vcpu0_entry",
		"_wrela_vcpu0_stack_top",
		"_wrela_vcpu0_context",
		"_wrela_vcpu1_ready",
		"_wrela_vcpu1_entry",
		"_wrela_vcpu1_stack_top",
		"_wrela_vcpu1_context",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("symbols = %#v, want %#v", got, want)
	}
}
```

- [ ] **Step 2: Run and verify failure**

Run: `go test ./compiler/codegen -run TestVcpuStartupDataSymbolsAreDeterministic -v`

Expected: FAIL with undefined `VcpuStartPlan` or `vcpuStartupData`.

- [ ] **Step 3: Confirm IR plan structs**

Confirm Task 7 already added `ir.VcpuStartPlan` and `Program.VcpuStarts`. Do not define a duplicate type in codegen.

- [ ] **Step 4: Add startup data helpers**

Create `compiler/codegen/vcpu_start.go`:

```go
package codegen

import (
	"fmt"
	"sort"

	"github.com/ryanwible/wrela3/compiler/ir"
)

const (
	apTrampolineBase = 0x8000
)

func vcpuStartupData(program *ir.Program) []ir.DataObject {
	plans := append([]ir.VcpuStartPlan{}, program.VcpuStarts...)
	sort.Slice(plans, func(i, j int) bool { return plans[i].VcpuID < plans[j].VcpuID })
	out := []ir.DataObject{}
	for _, plan := range plans {
		for _, suffix := range []string{"ready", "entry", "stack_top", "context"} {
			out = append(out, ir.DataObject{
				Symbol: fmt.Sprintf("_wrela_vcpu%d_%s", plan.VcpuID, suffix),
				Bytes: make([]byte, 8),
				Align: 64,
			})
		}
	}
	return out
}
```

- [ ] **Step 5: Wire data into `.data`**

In `buildData(program)`, append `vcpuStartupData(program)` after topic data and before interrupt runtime data.

- [ ] **Step 6: Verify and commit**

Run:

```bash
go test ./compiler/codegen -run TestVcpuStartupDataSymbolsAreDeterministic -v
git diff --check
git add compiler/ir/ir.go compiler/codegen/vcpu_start.go compiler/codegen/vcpu_start_test.go compiler/codegen/x64.go
git commit -m "feat: add vcpu startup data records -Codex Automated"
```

Expected: PASS.

**Acceptance Criteria:** vCPU startup data exists for vCPU0 and vCPU1, is sorted by vCPU ID, and is 64-byte aligned.

### Task 14B: Terminal vCPU Enter Codegen

**Description:** Emit `hardware.vcpu0.enter(executor = hello)` as a terminal call into the executor start function. This task does not start secondary CPUs.

**Files:**
- Modify: `compiler/codegen/x64.go`
- Modify: `compiler/codegen/vcpu_start.go`
- Test: `compiler/codegen/vcpu_start_test.go`

- [ ] **Step 1: Add vCPU enter test**

Append to `compiler/codegen/vcpu_start_test.go`:

```go
func TestVcpuEnterCallsExecutorStartAndHaltsIfReturned(t *testing.T) {
	execType := ir.Type{Name: "Hello", Module: "test", Kind: ir.TypeKindExecutor}
	program := &ir.Program{
		VcpuStarts: []ir.VcpuStartPlan{{VcpuID: 0, SlotLabel: "hello", ExecutorType: execType, Terminal: true}},
		Functions: []ir.Function{{
			Symbol: "enter_hello",
			Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
				&ir.VcpuEnter{VcpuID: 0, SlotLabel: "hello", Executor: &ir.Local{Symbol: "hello"}},
			}}},
		}},
	}
	image, ds := Compile(program)
	if len(ds) != 0 { t.Fatalf("Compile diagnostics: %#v", ds) }
	code := symbolBytes(t, image, "enter_hello")
	if !bytes.Contains(code, []byte{0xF4}) {
		t.Fatalf("enter must contain hlt fallback if executor returns: %x", code)
	}
	if !hasRelocTo(image, "enter_hello", "_wrela_method_test_Hello_run") {
		t.Fatalf("enter_hello missing relocation to executor run")
	}
}

func hasRelocTo(image *Image, ownerSymbol string, targetSymbol string) bool {
	ownerRVA := image.Symbols[ownerSymbol]
	for _, reloc := range image.Relocs {
		if reloc.Symbol == targetSymbol && reloc.Offset >= ownerRVA {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run and verify failure**

Run: `go test ./compiler/codegen -run TestVcpuEnterCallsExecutorStartAndHaltsIfReturned -v`

Expected: FAIL because `VcpuEnter` is not emitted.

- [ ] **Step 3: Add codegen hook**

In the `compileFunction` operation switch:

```go
case ir.VcpuEnter:
	emitVcpuEnter(e, v, frame, ctx)
	hasReturn = true
```

Add:

```go
func emitVcpuEnter(e *Emitter, op ir.VcpuEnter, frame Frame, ctx compileContext) {
	emitAddressOfValue(e, frame, op.Executor, asm.MustLookup("rdi"))
	e.emitCall(executorStartSymbol(ctx.VcpuPlans[op.SlotLabel].ExecutorType))
	emitHltLoop(e)
}

func executorStartSymbol(typ ir.Type) string {
	return symbolName("method", typ.Module, typ.Name, "run")
}

func emitHltLoop(e *Emitter) {
	loop := e.newLabel("hlt_loop")
	e.bindLabel(loop)
	e.emit(0xF4)
	e.emitJmp(loop)
}
```

Add `VcpuPlans map[string]ir.VcpuStartPlan` to `compileContext`.

- [ ] **Step 4: Verify and commit**

Run:

```bash
go test ./compiler/codegen -run TestVcpuEnterCallsExecutorStartAndHaltsIfReturned -v
git diff --check
git add compiler/codegen/x64.go compiler/codegen/vcpu_start.go compiler/codegen/vcpu_start_test.go
git commit -m "feat: emit terminal vcpu enter -Codex Automated"
```

Expected: PASS.

**Acceptance Criteria:** `VcpuEnter` calls the placed executor start method and cannot fall through to later source code.

### Task 14C: AP Trampoline Artifact Wiring

**Description:** Wire a checked-in AP trampoline artifact into the image and install it at the SIPI page. This junior plan consumes the trampoline bytes as a repository artifact; generating or reviewing those bytes belongs to a separate AP-trampoline artifact plan and is not assigned inside this task.

**Files:**
- Modify: `compiler/codegen/vcpu_start.go`
- Requires: `compiler/codegen/testdata/ap_trampoline.bin`
- Test: `compiler/codegen/vcpu_start_test.go`
- Modify: `wrela/platform/uefi/types.wrela`
- Modify: `compiler/codegen/entry_adapter.go`

- [ ] **Step 1: Add trampoline blob tests**

Append:

```go
func TestAPTrampolineBlobContract(t *testing.T) {
	blob := apTrampolineBlob()
	if len(blob) > 4096 {
		t.Fatalf("AP trampoline must fit in one 4KiB SIPI page, got %d bytes", len(blob))
	}
	for _, want := range [][]byte{
		{0xFA},       // cli
		{0x0F, 0x22}, // mov to control register shape
		{0x0F, 0x30}, // wrmsr
		{0xF4},       // hlt fallback
	} {
		if !bytes.Contains(blob, want) {
			t.Fatalf("trampoline missing byte shape %x in %x", want, blob)
		}
	}
}
```

- [ ] **Step 2: Run and verify failure**

Run: `go test ./compiler/codegen -run TestAPTrampolineBlobContract -v`

Expected: FAIL with undefined `apTrampolineBlob`.

- [ ] **Step 3: Add artifact-backed implementation**

Add `func apTrampolineBlob() []byte` in `compiler/codegen/vcpu_start.go`. The function returns a copy of the checked-in artifact bytes from `compiler/codegen/testdata/ap_trampoline.bin`. Do not synthesize trampoline bytes in this task.

```go
import _ "embed"

//go:embed testdata/ap_trampoline.bin
var apTrampolineBlobBytes []byte

func apTrampolineBlob() []byte {
	out := make([]byte, len(apTrampolineBlobBytes))
	copy(out, apTrampolineBlobBytes)
	return out
}
```

Append the blob as image data with a stable symbol:

```go
func apTrampolineDataObject() ir.DataObject {
	return ir.DataObject{
		Symbol: "_wrela_ap_trampoline_blob",
		Bytes: apTrampolineBlob(),
		Align: 4096,
	}
}
```

- [ ] **Step 4: Add install hook source**

Add an asm method to the delegated memory authority in `wrela/platform/uefi/types.wrela`:

```wrela
asm fn install_ap_trampoline(self, trampoline_base: PhysicalAddress, source: PhysicalAddress, length: U64) {
    mov rdi, trampoline_base
    mov rsi, source
    mov rcx, length
copy:
    cmp rcx, 0
    je done
    mov al, [rsi]
    mov [rdi], al
    add rsi, 1
    add rdi, 1
    sub rcx, 1
    jmp copy
done:
    ret
}
```

Do not make Wrela source name `_wrela_ap_trampoline_blob`. Codegen injects the install call in `compiler/codegen/entry_adapter.go` immediately before the transition returns `OwnedHardware`.

Add this helper:

```go
func emitInstallAPTrampoline(e *Emitter) {
	emitMovImmToReg(e, asm.MustLookup("rdi"), apTrampolineBase)
	emitMovDataAddressToReg(e, asm.MustLookup("rax"), "_wrela_ap_trampoline_blob")
	emitRegRegMove(e, asm.MustLookup("rsi"), asm.MustLookup("rax"))
	emitMovImmToReg(e, asm.MustLookup("rcx"), int64(len(apTrampolineBlob())))
	emitCallReloc(e, "_wrela_method_platform_uefi_types_DelegatedMemory_install_ap_trampoline")
}
```

Call `emitInstallAPTrampoline(e)` in the same unit that prepares delegated memory for `exit_to_owned_hardware`. This is the complete symbol mechanism: Wrela never refers to the static data symbol; the code generator owns the relocation.

- [ ] **Step 5: Verify and commit**

Run:

```bash
go test ./compiler/codegen -run TestAPTrampolineBlobContract -v
git diff --check
git add compiler/codegen/vcpu_start.go compiler/codegen/testdata/ap_trampoline.bin compiler/codegen/vcpu_start_test.go compiler/codegen/entry_adapter.go wrela/platform/uefi/types.wrela
git commit -m "feat: add ap trampoline contract -Codex Automated"
```

Expected: PASS after the checked-in trampoline artifact is present.

**Acceptance Criteria:** AP trampoline artifact is embedded as `_wrela_ap_trampoline_blob`, installed at `apTrampolineBase`, and tested for byte-shape and 4KiB SIPI-page size.

### Task 14D: LAPIC INIT/SIPI Emission

**Description:** Emit xAPIC INIT/SIPI commands for `VcpuStart{VcpuID: 1}` and return `VcpuStartStatus`.

**Files:**
- Modify: `compiler/codegen/vcpu_start.go`
- Modify: `compiler/codegen/x64.go`
- Test: `compiler/codegen/vcpu_start_test.go`

- [ ] **Step 1: Add LAPIC emission test**

Append:

```go
func TestVcpuStartEmitsLapicIcrWrites(t *testing.T) {
	statusType := ir.Type{Name: "VcpuStartStatus", Module: "machine.x86_64.cpu_state", Kind: ir.TypeKindData}
	program := &ir.Program{
		VcpuStarts: []ir.VcpuStartPlan{{VcpuID: 1, SlotLabel: "worker", Terminal: false}},
		Functions: []ir.Function{{
			Symbol: "start_worker",
			Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
				&ir.VcpuStart{VcpuID: 1, SlotLabel: "worker", Type: statusType, Executor: &ir.Local{Symbol: "worker"}},
			}}},
		}},
	}
	image, ds := Compile(program)
	if len(ds) != 0 { t.Fatalf("Compile diagnostics: %#v", ds) }
	code := symbolBytes(t, image, "start_worker")
	for _, want := range [][]byte{
		u32le(0x000C4500), // INIT level assert
		u32le(0x000C4608), // SIPI vector 0x08
	} {
		if !bytes.Contains(code, want) {
			t.Fatalf("start missing LAPIC command %x in %x", want, code)
		}
	}
}

func u32le(v uint32) []byte {
	out := make([]byte, 4)
	binary.LittleEndian.PutUint32(out, v)
	return out
}
```

- [ ] **Step 2: Implement helper**

Add the compile switch case in `compiler/codegen/x64.go`:

```go
case ir.VcpuStart:
	emitVcpuStart(e, v, frame, ctx)
```

Add:

```go
func emitVcpuStart(e *Emitter, op ir.VcpuStart, frame Frame, ctx compileContext) {
	emitLapicWrite(e, lapicICRHigh, uint32(op.VcpuID)<<24)
	emitLapicWrite(e, lapicICRLow, 0x000C4500)
	emitDelayLoop(e, 10000)
	emitLapicWrite(e, lapicICRHigh, uint32(op.VcpuID)<<24)
	emitLapicWrite(e, lapicICRLow, 0x000C4600|uint32(apTrampolineBase>>12))
	emitDelayLoop(e, 10000)
	emitLapicWrite(e, lapicICRHigh, uint32(op.VcpuID)<<24)
	emitLapicWrite(e, lapicICRLow, 0x000C4600|uint32(apTrampolineBase>>12))
	emitWaitForVcpuReady(e, op.VcpuID)
	emitStoreVcpuStartStatus(e, op, frame)
}
```

Reuse `emitLapicWrite` from `compiler/codegen/lapic.go`. `emitStoreVcpuStartStatus` writes `started=true` and `id=op.VcpuID` into the value slot for `op`.

- [ ] **Step 3: Verify and commit**

Run:

```bash
go test ./compiler/codegen -run TestVcpuStartEmitsLapicIcrWrites -v
git diff --check
git add compiler/codegen/x64.go compiler/codegen/vcpu_start.go compiler/codegen/vcpu_start_test.go
git commit -m "feat: emit lapic vcpu start -Codex Automated"
```

Expected: PASS.

**Acceptance Criteria:** `VcpuStart` writes INIT/SIPI commands, waits for ready, and produces `VcpuStartStatus`.

### Task 14E: vCPU Startup Integration Check

**Description:** Prove the vCPU start data, trampoline contract, LAPIC command path, and terminal enter path compile together.

**Files:**
- Modify: `compiler/codegen/vcpu_start_test.go`

- [ ] **Step 1: Add integration test**

Append:

```go
func TestVcpuStartAndEnterCompileTogether(t *testing.T) {
	program := twoVcpuProgramForCodegenTest()
	image, ds := Compile(program)
	if len(ds) != 0 { t.Fatalf("Compile diagnostics: %#v", ds) }
	for _, symbol := range []string{
		"_wrela_vcpu0_context",
		"_wrela_vcpu1_context",
		"_wrela_vcpu1_ready",
	} {
		if _, ok := image.Symbols[symbol]; !ok {
			t.Fatalf("missing symbol %s", symbol)
		}
	}
}

func twoVcpuProgramForCodegenTest() *ir.Program {
	execType := ir.Type{Name: "Worker", Module: "test", Kind: ir.TypeKindExecutor}
	statusType := ir.Type{Name: "VcpuStartStatus", Module: "machine.x86_64.cpu_state", Kind: ir.TypeKindData}
	return &ir.Program{
		VcpuStarts: []ir.VcpuStartPlan{
			{VcpuID: 0, SlotLabel: "hello", ExecutorType: execType, Terminal: true},
			{VcpuID: 1, SlotLabel: "worker", ExecutorType: execType, Terminal: false},
		},
		Functions: []ir.Function{{
			Symbol: "start_and_enter",
			Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
				&ir.VcpuStart{VcpuID: 1, SlotLabel: "worker", Type: statusType, Executor: &ir.Local{Symbol: "worker"}},
				&ir.VcpuEnter{VcpuID: 0, SlotLabel: "hello", Executor: &ir.Local{Symbol: "hello"}},
			}}},
		}},
	}
}
```

- [ ] **Step 2: Verify and commit**

Run:

```bash
go test ./compiler/codegen -run TestVcpuStartAndEnterCompileTogether -v
git diff --check
git add compiler/codegen/vcpu_start_test.go
git commit -m "test: cover two vcpu startup codegen -Codex Automated"
```

Expected: PASS.

**Acceptance Criteria:** vCPU codegen has one integration test covering data symbols, secondary start, and bootstrap enter.

---

## 10. Phase 6: Interrupt Topic Hard Cutover

**Phase Description:** Replace direct interrupt-to-executor callbacks with generated ISR glue that publishes path events into path-owned topics.

**Phase Acceptance Criteria:** Serial, EDU MSI, and ivshmem MSI-X interrupts publish topic events, wake subscriber slots, acknowledge hardware/APIC, and never call executor `on` handlers.

### Task 15A: Add Source APIs For Executor Arenas And Console Paths

**Description:** Add the source methods used by examples before rewriting those examples. This task also makes COM1 receive interrupts explicit by enabling them when the console path is created.

**Files:**
- Modify: `wrela/machine/x86_64/cpu_state.wrela`
- Modify: `wrela/machine/x86_64/serial.wrela`
- Test: `compiler/sem/uefi_source_shape_test.go`

- [ ] **Step 1: Add source-shape test**

Append to `compiler/sem/uefi_source_shape_test.go`:

```go
func TestExecutorArenaAndConsolePathFactoriesExist(t *testing.T) {
	modules := parseUEFIModuleSet(t)
	index := mustBuildIndex(t, modules)
	ownedMemory := moduleType(t, index, "machine.x86_64.cpu_state", "OwnedMemory")
	assertMethodExists(t, ownedMemory, "claim_executor_arena")
	serialDriver := moduleType(t, index, "machine.x86_64.serial", "SerialDriver")
	assertMethodExists(t, serialDriver, "create_console_path")
}
```

- [ ] **Step 2: Run and verify failure**

Run: `go test ./compiler/sem -run TestExecutorArenaAndConsolePathFactoriesExist -v`

Expected: FAIL because the methods do not exist.

- [ ] **Step 3: Add `claim_executor_arena`**

In `wrela/machine/x86_64/cpu_state.wrela`, add this method to `class OwnedMemory`:

```wrela
fn claim_executor_arena(self, owner: ExecutorSlot, length: U64, align: U64) -> ExecutorMemory {
    return ExecutorMemory(
        arena_base = self.arena.address,
        arena_length = length,
        next_offset = 0
    )
}
```

The `owner` parameter is intentionally not stored in `ExecutorMemory`. Task 4 records the returned value's owner in `localOrigin`, and Task 5 raises SEM0047 if an executor receives memory claimed for another slot.

- [ ] **Step 4: Add `create_console_path`**

In `wrela/machine/x86_64/serial.wrela`, add this method to `unique driver SerialDriver`:

```wrela
fn create_console_path(self, identity: PathIdentity, rx: SerialRxPublisher) -> SerialConsolePath {
    let path = SerialConsolePath(identity = identity, registers = self.registers, rx = rx)
    path.enable_receive_interrupts()
    return path
}
```

This is where COM1 receive interrupts are enabled in the new model. Executors do not call `enable_receive_interrupts()` directly.

- [ ] **Step 5: Verify and commit**

Run:

```bash
go test ./compiler/sem -run TestExecutorArenaAndConsolePathFactoriesExist -v
git diff --check
git add wrela/machine/x86_64/cpu_state.wrela wrela/machine/x86_64/serial.wrela compiler/sem/uefi_source_shape_test.go
git commit -m "feat: add explicit executor path source APIs -Codex Automated"
```

Expected: PASS.

**Acceptance Criteria:** Source APIs used by examples exist, executor memory owner is graph metadata, and serial receive interrupts are enabled by path construction.

### Task 15B: Generate Interrupt Topic Dispatch Instead Of Executor Callbacks

**Description:** Replace old interrupt dispatch units that call executor handlers with ISR glue that publishes into path-owned topics.

**Files:**
- Modify: `compiler/ir/ir.go`
- Modify: `compiler/ir/lower.go`
- Modify: `compiler/codegen/x64.go`
- Modify: `compiler/codegen/interrupt_test.go`
- Modify: `wrela/machine/x86_64/serial.wrela`
- Modify: `wrela/machine/x86_64/edu.wrela`
- Modify: `wrela/machine/x86_64/ivshmem.wrela`

- [ ] **Step 1: Add interrupt-topic test**

In `compiler/codegen/interrupt_test.go`, add:

```go
func TestInterruptDispatchPublishesTopicInsteadOfCallingOnHandler(t *testing.T) {
	program := &ir.Program{
		Topics: []ir.TopicLayout{{Label: "console.com1.rx", Kind: "serial_rx", Depth: 64, Subscribers: []string{"console"}}},
		InterruptBindings: []ir.InterruptBinding{{
			EventFunctionSymbol: "_wrela_event_machine_x86_64_serial_SerialConsolePath_rx",
			EventStorageSymbol: "_wrela_interrupt_event_40",
			EventStorageSize: 8,
			Vector: 0x40,
			TopicLabel: "console.com1.rx",
		}},
	}
	units := compileInterruptDispatchUnits(program)
	unit := findCompiledUnit(units, "_wrela_interrupt_vector40_serial")
	if unit == nil { t.Fatal("missing vector40 unit") }
	for _, rel := range unit.CallReloc {
		if strings.Contains(rel.Symbol, "_wrela_on_fn_") {
			t.Fatalf("interrupt dispatch must not call executor on handler: %#v", unit.CallReloc)
		}
	}
}
```

Add these fields to `ir.InterruptBinding`:

```go
TopicLabel string
TopicKind string
PublisherOwnerKind string // "driver_path"
PublisherOwnerLabel string
SubscriberSlots []string
ContextSymbol string
PathFieldOffset int
```

- [ ] **Step 2: Run and verify failure**

Run: `go test ./compiler/codegen -run TestInterruptDispatchPublishesTopicInsteadOfCallingOnHandler -v`

Expected: FAIL because old dispatch calls handler functions.

- [ ] **Step 3: Lower path interrupt publisher bindings**

Replace `lowerInterruptBindings` with topic-based binding construction. The core loop is:

```go
for _, route := range ctx.checked.ImageGraph.InterruptTopicRoutes {
	if route.Vector == 0 {
		ctx.addDiag(route.Span, diag.SEM0043, "interrupt route missing vector")
		continue
	}
	eventType := ctx.irType(route.EventType)
	eventInfo := mustTypeInfo(ctx.program.Types, eventType)
	ctx.program.InterruptBindings = append(ctx.program.InterruptBindings, ir.InterruptBinding{
		EventSymbol: route.EventSymbol,
		EventFunctionSymbol: route.EventFunctionSymbol,
		EventStorageSymbol: fmt.Sprintf("_wrela_interrupt_event_%02x", route.Vector),
		EventStorageSize: storageSizeOrEight(eventInfo),
		Vector: route.Vector,
		TopicLabel: route.TopicLabel,
		TopicKind: route.TopicKind,
		PublisherOwnerKind: "driver_path",
		PublisherOwnerLabel: route.PathLabel,
		SubscriberSlots: append([]string{}, route.SubscriberSlots...),
		ContextSymbol: route.ContextSymbol,
		PathFieldOffset: route.PathFieldOffset,
	})
}
sort.Slice(ctx.program.InterruptBindings, func(i, j int) bool {
	return ctx.program.InterruptBindings[i].Vector < ctx.program.InterruptBindings[j].Vector
})
```

Do not append to `program.OnHandlers`. Do not set `HandlerFunctionSymbol`.

- [ ] **Step 4: Emit ISR topic publish**

Replace `buildInterruptDispatchUnit` with this exact call sequence:

```go
func buildInterruptDispatchUnit(symbol string, binding ir.InterruptBinding) compiledUnit {
	e := &Emitter{Labels: map[string]int{}}
	emitInterruptSave(e)
	emitLoadInterruptPathReceiver(e, binding)
	e.emitCall(binding.EventFunctionSymbol)
	emitInterruptEventStore(e, binding)
	emitInterruptTopicPublish(e, binding)
	emitLocalApicEOI(e)
	emitInterruptRestore(e)
	e.emit(0x48, 0xCF) // iretq
	return compiledUnit{Symbol: symbol, Bytes: e.Code, CallReloc: e.CallReloc, DataReloc: e.DataReloc}
}
```

Add the receiver-loading helper before the call:

```go
func emitLoadInterruptPathReceiver(e *Emitter, binding ir.InterruptBinding) {
	emitMovDataAddressToReg(e, asm.MustLookup("rax"), binding.ContextSymbol)
	emitLoadMemToReg(e, asm.MustLookup("rdi"), asm.MustLookup("rax"), int64(binding.PathFieldOffset), 64)
}
```

`emitInterruptTopicPublish` is not allowed to call a Wrela executor method. It must call the same topic-ring helpers as Task 10, using `binding.TopicLabel` and `binding.TopicKind`.

For serial rx, publish event as:

```text
slot sequence at +0
byte at +8
```

For EDU/ivshmem, publish status as U32 widened in a 64-byte slot. All interrupt topic slot layouts remain:

```text
slot + 0: U64 sequence
slot + 8: payload field 0 widened to U64
```

- [ ] **Step 5: Update Wrela interrupt source**

In `serial.wrela`, path event source becomes:

```wrela
driver path SerialConsolePath {
    identity: PathIdentity
    registers: SerialWriterRegisters
    rx: SerialRxPublisher

    interrupt receiver -> SerialPathInterrupt {
        let status = self.registers.read8(offset = 5)
        if (status & 0x01) != 0 {
            return SerialPathInterrupt(has_byte = true, byte = self.registers.read8(offset = 0))
        }
        return SerialPathInterrupt(has_byte = false, byte = 0)
    }
}
```

Do the same for EDU and ivshmem with their concrete event topics.

For EDU, the resulting source contract is:

```wrela
use { ExecutorSlot, PathIdentity } from machine.x86_64.cpu_state
use { TopicIdentity } from machine.x86_64.topic_u64

data EduInterrupt {
    status: U32
}

data EduInterruptNext {
    has_message: Bool
    message: EduInterrupt
}

class EduInterruptTopic {
    identity: TopicIdentity
    id: U64
    fn publisher(self) -> EduInterruptPublisher {
        return EduInterruptPublisher(topic = self)
    }
    fn subscribe(self, subscriber: ExecutorSlot) -> EduInterruptSubscription {
        return EduInterruptSubscription(topic = self, subscriber = subscriber, cursor = 0, armed = false)
    }
}

class EduInterruptPublisher {
    topic: EduInterruptTopic
}

class EduInterruptSubscription {
    topic: EduInterruptTopic
    subscriber: ExecutorSlot
    cursor: U64
    armed: Bool

    asm fn try_next(self) -> EduInterruptNext {
        ret
    }

    fn arm_wait(self) {
        self.armed = true
    }

    fn is_wait_armed(self) -> Bool {
        return self.armed
    }
}

driver path EduMsiPath {
    identity: PathIdentity
    mmio_base: VirtualAddress
    interrupt: EduInterruptPublisher

    interrupt receiver -> EduInterrupt {
        return EduInterrupt(status = self.read32(offset = 0x24))
    }
}
```

For ivshmem, mirror the same shape with `IvshmemDoorbellTopic`, `IvshmemDoorbellPublisher`, `IvshmemDoorbellSubscription`, and `IvshmemDoorbellNext`. `IvshmemDoorbellTopic` must include `identity: TopicIdentity`; all example constructors must pass `identity = TopicIdentity(label = "...")`.

- [ ] **Step 6: Verify**

Run:

```bash
go test ./compiler/codegen -run Interrupt -v
go test ./compiler/ir -run Interrupt -v
git diff --check
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add compiler/ir/ir.go compiler/ir/lower.go compiler/codegen/x64.go compiler/codegen/interrupt_test.go wrela/machine/x86_64/serial.wrela wrela/machine/x86_64/edu.wrela wrela/machine/x86_64/ivshmem.wrela
git commit -m "feat: publish interrupts to topics -Codex Automated"
```

**Acceptance Criteria:** Interrupt dispatch no longer calls executor `on` handlers; it publishes path events into topics and wakes subscriber slots.

---

## 11. Phase 7: Examples, e2e, And Hard Cutover

**Phase Description:** Rewrite executable Wrela sources to use the new explicit graph and prove cross-vCPU communication plus interrupt-topic delivery in QEMU.

**Phase Acceptance Criteria:** Production examples and e2e fixtures use slots, path identities, subscriptions, topics, and `start`/`enter`; old owner/vCPU-memory/direct-run/on-handler shapes remain only in negative fixtures.

### Task 16: Rewrite Hello And Add Multi-vCPU Topic Fixture

**Description:** Convert examples and e2e fixtures to slots, topics, vCPU start/enter, and interrupt topics.

**Files:**
- Modify: `examples/hello/main.wrela`
- Modify: `examples/hello/program.wrela`
- Create: `tests/e2e/fixtures/multi_vcpu_topics/main.wrela`
- Modify: `tests/e2e/hello_qemu_test.go`

Do not modify `wrela/machine/x86_64/cpu_state.wrela` or `wrela/machine/x86_64/serial.wrela` in this task. If `claim_executor_arena` or `create_console_path` is missing, stop and complete Task 15A first.

- [ ] **Step 1: Add e2e test**

Append to `tests/e2e/hello_qemu_test.go`:

```go
func TestMultiVcpuTopicsQEMU(t *testing.T) {
	qemuBin, err := exec.LookPath("qemu-system-x86_64")
	if err != nil { t.Fatalf("qemu-system-x86_64 not found in PATH: %v", err) }
	firmware, err := qemu.ResolveFirmware(qemuBin)
	if err != nil { t.Fatalf("resolve QEMU firmware: %v", err) }
	tmp := t.TempDir()
	vars := filepath.Join(tmp, "OVMF_VARS.fd")
	copyFile(t, firmware.Vars, vars)
	image := filepath.Join(tmp, "multi-vcpu-topics.efi")
	_, err = compiler.Build(compiler.BuildOptions{
		Mode: compiler.ModeDev,
		RootPath: "tests/e2e/fixtures/multi_vcpu_topics/main.wrela",
		OutputPath: image,
		RepoRoot: ".",
	})
	if err != nil { t.Fatalf("build multi vcpu image: %v", err) }
	out, err := qemu.Run(qemu.Options{
		QEMUBinary: qemuBin,
		OVMFCode: firmware.Code,
		OVMFVars: vars,
		ESPDir: filepath.Join(tmp, "esp"),
		ImagePath: image,
		SuccessText: "consumer received 64",
		Timeout: qemuTimeout(),
		SMP: 2,
	})
	if err != nil { t.Fatalf("qemu failed: %v\nserial output:\n%s", err, out) }
	if !strings.Contains(out, "producer published 64") || !strings.Contains(out, "consumer received 64") {
		t.Fatalf("missing multi-vcpu topic output:\n%s", out)
	}
}
```

- [ ] **Step 2: Run and verify failure**

Run: `go test ./tests/e2e -run TestMultiVcpuTopicsQEMU -v`

Expected: FAIL because fixture does not exist or feature is incomplete.

- [ ] **Step 3: Create fixture**

Create `tests/e2e/fixtures/multi_vcpu_topics/main.wrela` using the executor bodies from Section 4 and this exact image wiring shape:

```wrela
phase owned_hardware(hardware: OwnedHardware) -> never {
    let com1 = hardware.io_ports.claim_com1()
    let serial_driver = SerialDriver(
        registers = SerialWriterRegisters(port_base = com1.port_base),
        memory = DriverMemory(region = hardware.memory.arena)
    ).initialize()
    let serial_rx_topic = SerialRxTopic(
        identity = TopicIdentity(label = "multi.console.rx"),
        id = 2
    )
    let serial_path = serial_driver.create_console_path(
        identity = PathIdentity(label = "multi.console"),
        rx = serial_rx_topic.publisher()
    )

    let producer_slot = hardware.executors.claim(
        identity = SlotIdentity(label = "producer")
    )
    let consumer_slot = hardware.executors.claim(
        identity = SlotIdentity(label = "consumer")
    )

    let producer_memory = hardware.memory.claim_executor_arena(
        owner = producer_slot,
        length = 0x200000,
        align = 4096
    )
    let consumer_memory = hardware.memory.claim_executor_arena(
        owner = consumer_slot,
        length = 0x200000,
        align = 4096
    )

    let counter_topic = U64GapTopic(identity = TopicIdentity(label = "multi.counter"), id = 0, depth = 64)
    let result_topic = U64ReliableTopic(identity = TopicIdentity(label = "multi.result"), id = 1, depth = 4)

    let producer = Producer(
        slot = producer_slot,
        loop = EventSleepPolicy(),
        memory = producer_memory,
        serial = serial_path,
        out = counter_topic.publisher(),
        result_in = result_topic.subscribe(subscriber = producer_slot)
    )
    let consumer = Consumer(
        slot = consumer_slot,
        loop = EventSleepPolicy(),
        memory = consumer_memory,
        input = counter_topic.subscribe(subscriber = consumer_slot),
        result_out = result_topic.publisher()
    )

    let status = hardware.vcpu1.start(executor = consumer)
    if status.started == false {
        producer.serial.write(producer.memory.bytes(value = "vcpu1 start failed\n"))
        producer.memory.halt_forever()
    }
    hardware.vcpu0.enter(executor = producer)
}
```

Only `producer` receives `serial_path`. The consumer publishes `64` through `result_out`; the producer drains `result_in` and writes `"consumer received 64\n"` over serial.

- [ ] **Step 4: Rewrite hello**

Rewrite `examples/hello/main.wrela` and `examples/hello/program.wrela`:

These source APIs already exist before this plan and keep their behavior:

```text
hardware.io_ports.claim_com1()
SerialDriver(...).initialize()
ExecutorMemory.bytes(value = ...)
ExecutorMemory.halt_forever()
SerialConsolePath.write(...)
SerialConsolePath.write_byte(...)
SerialConsolePath.ack_receive(...)
EduMsiPath.ack_completed(...)
Q35PciInterruptConfigurator.edu_bar0()
```

These signatures were added in Task 15A and are used here:

```wrela
fn create_console_path(self, identity: PathIdentity, rx: SerialRxPublisher) -> SerialConsolePath
fn claim_executor_arena(self, owner: ExecutorSlot, length: U64, align: U64) -> ExecutorMemory
```

Use this owned-hardware wiring shape:

```wrela
let hello_slot = hardware.executors.claim(
    identity = SlotIdentity(label = "hello")
)
let hello_memory = hardware.memory.claim_executor_arena(
    owner = hello_slot,
    length = 0x200000,
    align = 4096
)
let serial_rx_topic = SerialRxTopic(
    identity = TopicIdentity(label = "hello.console.rx"),
    id = 0
)
let serial_path = serial_driver.create_console_path(
    identity = PathIdentity(label = "hello.console"),
    rx = serial_rx_topic.publisher()
)
let edu_interrupt_topic = EduInterruptTopic(
    identity = TopicIdentity(label = "hello.edu.interrupt"),
    id = 1
)
let edu_path = EduMsiPath(
    identity = PathIdentity(label = "hello.edu"),
    mmio_base = pci_interrupts.edu_bar0(),
    interrupt = edu_interrupt_topic.publisher()
)
let hello = HelloWorld(
    slot = hello_slot,
    loop = EventSleepPolicy(),
    memory = hello_memory,
    serial_path = serial_path,
    serial_rx = serial_rx_topic.subscribe(subscriber = hello_slot),
    edu_path = edu_path,
    edu_interrupts = edu_interrupt_topic.subscribe(subscriber = hello_slot)
)
hardware.vcpu0.enter(executor = hello)
```

Use this executor loop shape in `HelloWorld.run`:

```wrela
while true {
    let serial_event = self.serial_rx.try_next()
    if serial_event.has_message {
        self.serial_path.write(self.memory.bytes(value = "serial interrupt: "))
        self.serial_path.write_byte(value = serial_event.message.byte)
        self.serial_path.write(self.memory.bytes(value = "\n"))
        self.serial_path.ack_receive(event = serial_event.message)
    }

    let edu_event = self.edu_interrupts.try_next()
    if edu_event.has_message {
        self.serial_path.write(self.memory.bytes(value = "msi interrupt\n"))
        self.edu_path.ack_completed(event = edu_event.message)
    }

    self.serial_rx.arm_wait()
    self.edu_interrupts.arm_wait()

    let serial_retry = self.serial_rx.try_next()
    let edu_retry = self.edu_interrupts.try_next()
    if serial_retry.has_message == false {
        if edu_retry.has_message == false {
            self.loop.wait()
        }
    }
}
```

Delete both `on serial_path.interrupt` and `on edu_path.interrupt` blocks.

- [ ] **Step 5: Verify**

Run:

```bash
go test ./compiler -run 'TestBuildHello|TestBuildHelloContainsInterruptBinding|TestHelloSourceUsesArenaFrames' -v
go test ./tests/e2e -run 'TestMultiVcpuTopicsQEMU|TestHelloQEMU' -v
git diff --check
```

Expected: PASS on machines with QEMU/OVMF.

- [ ] **Step 6: Commit**

```bash
git add examples/hello/main.wrela examples/hello/program.wrela tests/e2e/fixtures/multi_vcpu_topics/main.wrela tests/e2e/hello_qemu_test.go
git commit -m "test: verify explicit vcpu topics in qemu -Codex Automated"
```

**Acceptance Criteria:** Multi-vCPU QEMU fixture proves producer publishes 64 messages, consumer receives 64, and serial output confirms both. Hello uses hard-cut topic interrupts and vCPU enter.

### Task 17: Remove Old Wiring From Fixtures And Tests

**Description:** Finish the hard cutover by removing old owner/vcpu memory/direct run shapes.

**Files:**
- Modify: `tests/e2e/fixtures/hello_ivshmem/main.wrela`
- Modify: `tests/e2e/fixtures/hello_ivshmem/program.wrela`
- Modify: `tests/e2e/fixtures/arena_memory/main.wrela`
- Modify: `tests/e2e/fixtures/cache_memory/main.wrela`
- Modify: `compiler/integration_test.go`
- Modify: `compiler/sem/types_test.go`
- Modify: `compiler/ir/lower_test.go`
- Modify: `compiler/codegen/interrupt_test.go`

- [ ] **Step 1: Run old-shape search**

Run:

```bash
rg -nP "ExecutorPlacement|owner\\s*=\\s*hardware\\.vcpu|hardware\\.vcpu0\\.memory|^\\s*[A-Za-z_][A-Za-z0-9_]*\\.run\\(|^\\s*on\\s+[A-Za-z_][A-Za-z0-9_]*\\.interrupt\\b" compiler tests examples wrela
```

Expected: matches exist before this task.

Classify every match before editing:

```text
examples/hello/main.wrela -> already rewritten by Task 16; no match should remain
examples/hello/program.wrela -> already rewritten by Task 16; no match should remain
tests/e2e/fixtures/hello_ivshmem/main.wrela -> rewrite in this task
tests/e2e/fixtures/hello_ivshmem/program.wrela -> rewrite in this task
tests/e2e/fixtures/arena_memory/main.wrela -> rewrite in this task
tests/e2e/fixtures/cache_memory/main.wrela -> rewrite in this task
compiler/negative_fixtures_test.go -> leave only fixtures that intentionally assert old syntax rejection
compiler/sem/path_graph_test.go -> migrate to slot/path/topic graph tests from Task 5
compiler/sem/uefi_source_shape_test.go -> migrate imports from ExecutorPlacement to ExecutorSlot/PathIdentity
compiler/sem/symbols_test.go -> keep parser/index rejection tests, update expected diagnostic to SEM0042
compiler/sem/symbols.go -> remove old duplicate on-handler success path after SEM0042 rejection lands
compiler/sem/types_test.go -> migrate old on-handler tests to SEM0042 rejection or delete success cases
compiler/sem/memory_test.go -> migrate ExecutorPlacement snippets to ExecutorSlot plus claim_executor_arena
compiler/codegen/uefi_source_codegen_test.go -> migrate source snippets to claim_executor_arena and vcpu0.enter
compiler/parse/parser_test.go -> keep parser coverage for old syntax if it remains syntactically valid; semantic rejection is Task 6
compiler/ir/lower_test.go -> migrate old on-handler lowering tests to interrupt-topic lowering from Task 15B
tests/fixtures/negative/interrupt_event_call.wrela -> replace with on_interrupt_rejected.wrela or update expected SEM0042
tests/fixtures/negative/root_driver_to_executor.wrela -> delete if it only tests old owner-field routing
tests/fixtures/negative/path_assigned_twice.wrela -> rewrite to shared path passed to two executor constructors and expect SEM0038
```

- [ ] **Step 2: Rewrite fixtures**

Rewrite these files exactly:

```text
tests/e2e/fixtures/arena_memory/main.wrela
  Replace ExecutorPlacement construction with:
    let slot = hardware.executors.claim(identity = SlotIdentity(label = "arena"))
    let memory = hardware.memory.claim_executor_arena(owner = slot, length = 0x200000, align = 4096)
    let probe = ArenaProbe(slot = slot, memory = memory, ...)
    hardware.vcpu0.enter(executor = probe)

tests/e2e/fixtures/cache_memory/main.wrela
  Replace ExecutorPlacement construction with:
    let slot = hardware.executors.claim(identity = SlotIdentity(label = "cache"))
    let memory = hardware.memory.claim_executor_arena(owner = slot, length = 0x200000, align = 4096)
    let probe = CacheProbe(slot = slot, memory = memory, ...)
    hardware.vcpu0.enter(executor = probe)

tests/e2e/fixtures/hello_ivshmem/main.wrela
  Use the hello wiring from Task 16 plus an ivshmem topic:
    let ivshmem_topic = IvshmemDoorbellTopic(identity = TopicIdentity(label = "hello.ivshmem.doorbell"), id = 2)
    let ivshmem_rx = IvshmemDoorbellPath(identity = PathIdentity(label = "hello.ivshmem"), ..., interrupt = ivshmem_topic.publisher())
    let hello = HelloWorld(..., ivshmem_rx = ivshmem_rx, ivshmem_events = ivshmem_topic.subscribe(subscriber = hello_slot))

tests/e2e/fixtures/hello_ivshmem/program.wrela
  Delete all `on serial_path.interrupt`, `on edu_path.interrupt`, and `on ivshmem_rx.interrupt` blocks.
  Add fields to HelloWorld:
    serial_rx: SerialRxSubscription
    edu_interrupts: EduInterruptSubscription
    ivshmem_events: IvshmemDoorbellSubscription
  In run(), drain each subscription with try_next(), ack through the path, then arm_wait(), re-check, and call loop.wait().
```

- [ ] **Step 3: Update compiler tests**

Any test that asserts old call graph symbols for direct `hello.run()` should now assert `VcpuEnter`/entry lowering or final emitted executor method presence.

Update source scans to reject old wiring:

```go
for _, forbidden := range []string{"owner = hardware.vcpu0", "hardware.vcpu0.memory", "hello.run()", "on serial_path.interrupt"} {
	if strings.Contains(source, forbidden) {
		t.Fatalf("source contains old executor wiring %q", forbidden)
	}
}
```

- [ ] **Step 4: Verify**

Run:

```bash
rg -nP "owner\\s*=\\s*hardware\\.vcpu|hardware\\.vcpu0\\.memory|^\\s*[A-Za-z_][A-Za-z0-9_]*\\.run\\(|^\\s*on\\s+[A-Za-z_][A-Za-z0-9_]*\\.interrupt\\b" examples tests/e2e/fixtures wrela
go test ./compiler ./tests/e2e -run 'TestBuild|Test.*QEMU' -v
git diff --check
```

Expected: `rg` prints no matches except negative fixtures; tests PASS where QEMU is available.

- [ ] **Step 5: Commit**

```bash
git add tests/e2e/fixtures/hello_ivshmem/main.wrela tests/e2e/fixtures/hello_ivshmem/program.wrela tests/e2e/fixtures/arena_memory/main.wrela tests/e2e/fixtures/cache_memory/main.wrela compiler
git commit -m "chore: hard cut executor topic wiring -Codex Automated"
```

**Acceptance Criteria:** No production example or e2e fixture uses old vCPU ownership, vCPU memory, direct executor run, or executor `on` handlers.

---

## 12. Phase 8: Final Verification And Documentation

**Phase Description:** Run full-tree acceptance checks and align project docs with the implemented runtime direction.

**Phase Acceptance Criteria:** Go tests pass, QEMU hello and multi-vCPU topic tests pass in a configured environment, old wiring searches are clean outside negative fixtures, and deferred-work docs name remaining production work without reopening this design.

### Task 18: Final Full-Tree Verification

**Description:** Run the complete acceptance suite and document any environment-only QEMU limitations.

**Files:**
- Modify: `docs/production-deferred-work.md`
- Modify: `docs/superpowers/specs/2026-05-16-explicit-vcpu-executors-design.md`

- [ ] **Step 1: Update deferred work**

In `docs/production-deferred-work.md`, change Executor runtime section to say:

```markdown
## Executor runtime
- Implemented direction: executors are explicitly assigned to vCPUs through source-visible `ExecutorSlot` values and vCPU `start`/`enter` dispatch. Communication uses SPMC topics with compiler-planned cache-line layout and wake paths. There is no hidden scheduler, migration, or work stealing.
- Remaining production work: dynamic hardware discovery beyond static q35 two-vCPU startup, richer topology placement, generalized topic payload typing, and production-grade monitor/mwait feature selection.
```

- [ ] **Step 2: Update design spec status**

Append this note under `## First Milestone` in `docs/superpowers/specs/2026-05-16-explicit-vcpu-executors-design.md`:

```markdown
Implementation note: the implementation plan hard-cuts interrupt topics and reliable bounded topics into the main milestone instead of deferring them. The old `on path.interrupt` executor callback surface is intentionally removed without backwards compatibility.
```

- [ ] **Step 3: Run verification**

Run:

```bash
go test ./...
go test ./tests/e2e -run 'MultiVcpu|Hello' -v
rg -nP "owner\\s*=\\s*hardware\\.vcpu|hardware\\.vcpu0\\.memory|^\\s*[A-Za-z_][A-Za-z0-9_]*\\.run\\(|^\\s*on\\s+[A-Za-z_][A-Za-z0-9_]*\\.interrupt\\b" examples tests/e2e/fixtures wrela
git diff --check
```

Expected:

- `go test ./...` PASSes.
- e2e tests PASS where QEMU/OVMF are installed.
- `rg` prints no production-source matches for old wiring. Negative fixtures may be excluded or inspected manually.
- `git diff --check` prints nothing.

- [ ] **Step 4: Commit**

```bash
git add docs/production-deferred-work.md docs/superpowers/specs/2026-05-16-explicit-vcpu-executors-design.md
git commit -m "docs: record explicit vcpu topic runtime -Codex Automated"
```

**Acceptance Criteria:** Full Go tests pass; QEMU multi-vCPU topic and hello tests pass where environment supports them; docs reflect the implemented executor/topic direction.

---

## 13. Self-Review Checklist For Implementers

Before marking this plan complete, verify:

- [ ] No old executor `on path.interrupt` handlers remain outside negative fixtures.
- [ ] No old `owner = hardware.vcpuN` path ownership remains outside negative fixtures.
- [ ] No old `hardware.vcpu0.memory` executor memory ownership remains outside negative fixtures.
- [ ] Every executor has `slot`, `loop`, and `memory` fields.
- [ ] Every subscription is created with `subscriber = some_slot`.
- [ ] Every executor memory arena is claimed with `owner = some_slot`.
- [ ] Every non-current vCPU uses `start`.
- [ ] The current bootstrap vCPU uses terminal `enter`.
- [ ] Gap topics detect overflow gaps.
- [ ] Reliable topics refuse to overwrite unread messages.
- [ ] Interrupt dispatch publishes topics and does not call executor handlers.
- [ ] QEMU multi-vCPU fixture proves cross-vCPU communication.
