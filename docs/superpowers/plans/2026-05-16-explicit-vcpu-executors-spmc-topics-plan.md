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

Definition of done for a task:

- All checkbox steps are complete.
- New tests fail before implementation.
- New tests pass after implementation.
- `git diff --check` passes.
- A commit is created with the exact message shown in the task.

Definition of done for the full plan:

- `go test ./...` passes.
- `go test ./tests/e2e -run 'MultiVcpu|Hello' -v` passes on a machine with QEMU/OVMF.
- `rg -n "on .*\\.interrupt|owner = hardware\\.vcpu|hardware\\.vcpu0\\.memory|\\.run\\(\\)" examples tests/e2e/fixtures wrela` reports no old executor/interrupt wiring, except tests that intentionally assert rejection.
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
- Topic storage is compiler-generated writable data for this plan. It is cache-line aligned and named from topic identity labels.
- Wrela has no generic syntax in this plan. Implement concrete `U64GapTopic`, `U64ReliableTopic`, and concrete interrupt topic types such as `SerialRxTopic`.
- `Topic.publisher()` creates one unique publisher capability.
- Publisher authority may be owned by an executor slot or by a driver/path capability.
- Gap-detecting topics may overwrite old messages and subscribers detect gaps.
- Reliable bounded topics retain messages until required subscribers advance or producer backpressure is handled explicitly.
- Reliable producer backpressure waits on subscriber cursor advancement, not on new producer messages.
- Interrupts are hard-cut over to path-owned topics. Executor `on path.interrupt` handlers are rejected.
- Generated ISR glue must be tiny: capture event, publish topic event, acknowledge device/APIC, wake subscriber slot, return.
- Cache-line wait is the preferred backend abstraction. `HLT + IPI` is the required fallback.

---

## 2. Repository Layout And File Responsibilities

Create or modify exactly these files.

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
  Removes `owner: ExecutorPlacement` and direct interrupt receiver callback usage. Adds SerialRxTopic, SerialRxPublisher, SerialRxSubscription, SerialRxEvent, and path-owned interrupt topic declarations.

wrela/machine/x86_64/edu.wrela
wrela/machine/x86_64/ivshmem.wrela
  Migrates MSI/MSI-X paths from direct interrupt receivers to path-owned interrupt topics.

wrela/platform/uefi/transition.wrela
wrela/platform/uefi/types.wrela
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

tests/fixtures/negative/*.wrela
compiler/negative_fixtures_test.go
  Adds negative fixtures for old `on`, duplicate labels, slot mismatch, publisher duplication, shared paths, vCPU overcommit, and invalid current-vCPU start order.
```

---

## 3. Parallel Work Map

Serial spine:

```text
Task 1 diagnostics
Task 2 source surface
Task 3 semantic classification
Task 4 graph extraction
Task 5 slot/topic semantic checks
Task 6 hard reject old interrupt model
Task 7 IR operations
Task 8 lowering
Task 9 topic data/codegen
Task 10 gap topic codegen
Task 11 reliable topic codegen
Task 12 wait primitives
Task 13 QEMU SMP
Task 14 vCPU start/AP startup
Task 15 interrupt-topic dispatch
Task 16 examples/e2e
Task 17 fixture/test hard cutover
Task 18 final verification
```

Parallel allowance:

- Task 13 may run after Task 1 because QEMU args are independent.
- Task 2 Wrela source edits can be split by module, but the task must land as one commit.
- Task 15 interrupt-topic dispatch must wait for Tasks 7-12.
- Task 16 examples/e2e must wait for Tasks 2, 5, 11, 14, and 15.

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
	assertMethodExists(t, moduleType(t, index, "machine.x86_64.topic_u64", "U64GapTopic"), "subscribe")
	assertMethodExists(t, moduleType(t, index, "machine.x86_64.topic_u64", "U64ReliableTopic"), "publisher")
assertMethodExists(t, moduleType(t, index, "machine.x86_64.topic_u64", "U64ReliablePublisher"), "try_publish")
assertMethodExists(t, moduleType(t, index, "machine.x86_64.topic_u64", "U64ReliablePublisher"), "publish_or_wait")
assertMethodExists(t, moduleType(t, index, "machine.x86_64.topic_u64", "U64ReliableSubscription"), "try_next")
assertTypeFields(t, moduleType(t, index, "machine.x86_64.cpu_state", "PathIdentity"), map[string]string{
	"label": "StringLiteral",
})
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

Keep the old `ExecutorPlacement` declaration only while intermediate focused tests are landing. Task 17 removes all production call sites and leaves no compatibility behavior attached to the old type.

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

data SerialRxEvent {
    byte: U8
}

data SerialRxNext {
    has_message: Bool
    message: SerialRxEvent
}

class SerialRxTopic {
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
		{"machine.x86_64.serial", "SerialRxPublisher", IsTopicPublisherType},
		{"machine.x86_64.serial", "SerialRxSubscription", IsTopicSubscriptionType},
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
		"machine.x86_64.serial.SerialRxTopic":
		return true
	default:
		return false
	}
}

func IsTopicPublisherType(t *Type) bool {
	switch qualifiedTypeName(t) {
	case "machine.x86_64.topic_u64.U64GapPublisher",
		"machine.x86_64.topic_u64.U64ReliablePublisher",
		"machine.x86_64.serial.SerialRxPublisher":
		return true
	default:
		return false
	}
}

func IsTopicSubscriptionType(t *Type) bool {
	switch qualifiedTypeName(t) {
	case "machine.x86_64.topic_u64.U64GapSubscription",
		"machine.x86_64.topic_u64.U64ReliableSubscription",
		"machine.x86_64.serial.SerialRxSubscription":
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
class U64GapTopic {
    id: U64
    depth: U64
    fn subscribe(self, subscriber: ExecutorSlot) -> U64GapSubscription { return U64GapSubscription(topic = self, subscriber = subscriber, cursor = 0, armed = false) }
}
class U64GapSubscription { topic: U64GapTopic; subscriber: ExecutorSlot; cursor: U64; armed: Bool }
`)
	src := `
module test.graph
use { ExecutorSlot, SlotIdentity, OwnedHardware } from machine.x86_64.cpu_state
use { U64GapSubscription, U64GapTopic } from machine.x86_64.topic_u64
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
        let topic = U64GapTopic(id = 0, depth = 64)
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
	ExecutorSlots []ExecutorSlotNode
	Topics []TopicNode
	TopicPublishers []TopicPublisherNode
	TopicSubscriptions []TopicSubscriptionNode
	VcpuPlacements []VcpuPlacementNode
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
	VcpuID int
	HasVcpuID bool
}
```

On every `LetStmt`, after `valueType := c.typeExpr(...)`, compute and store `localOrigin` for the bound name. Constructor origins store `FieldBindings`; `hardware.executors.claim(...)` stores `SlotLabel`; `U64GapTopic(...)`, `U64ReliableTopic(...)`, and `SerialRxTopic(...)` store `TopicLabel`; `hardware.vcpu0` and `hardware.vcpu1` field origins store `VcpuID`. The `start/enter` extractor must read the executor local origin, then read `FieldBindings["slot"]`, then look up that slot local origin to fill `VcpuPlacementNode.SlotLabel`.

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
use { U64GapPublisher, U64GapSubscription, U64GapTopic } from machine.x86_64.topic_u64
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
        let topic = U64GapTopic(id = 0, depth = 64)
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
        let topic_a = U64GapTopic(id = 0, depth = 64)
        let topic_b = U64GapTopic(id = 1, depth = 64)
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
        let topic_a = U64GapTopic(id = 0, depth = 64)
        let topic_b = U64GapTopic(id = 1, depth = 64)
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
        let topic = U64GapTopic(id = 0, depth = 64)
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

- [ ] **Step 3: Implement graph validation**

Add `checkExecutorTopicGraph()` and call it from `Check` after `checkExecutorWiring()`:

```go
func (c *checker) checkExecutorTopicGraph() {
	c.checkDuplicateLabels()
	c.checkSlotBindings()
	c.checkVcpuPlacements()
	c.checkSubscriptionSlotMatches()
	c.checkTopicPublisherUniqueness()
}
```

Required behavior:

- Duplicate slot/path/topic labels produce SEM0033.
- Slot not bound to exactly one executor produces SEM0034.
- Slot placed on no vCPU or more than one vCPU produces SEM0034.
- Subscription passed to executor with different slot produces SEM0035.
- More than one executor on one vCPU produces SEM0037.
- Shared path produces SEM0038. Update old shared-path tests from SEM0011 to SEM0038 in the same commit.
- Publisher value passed to more than one producer field produces SEM0039.

- [ ] **Step 4: Verify**

Run:

```bash
go test ./compiler/sem -run 'TestExecutorTopicGraphDiagnostics|TestExecutorSlotTopicGraphExtraction|TestExecutorTopicKindClassification' -v
git diff --check
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add compiler/sem/check.go compiler/sem/topic_graph_test.go
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

- [ ] **Step 3: Reject `OnHandlerDecl`**

In semantic checking for executor declarations, emit SEM0042 for every `OnHandlerDecl`:

```go
for _, handler := range exec.OnHandlers {
	c.error(handler.SpanV, diag.SEM0042, "executor on interrupt handlers are no longer supported; use path-owned interrupt topics")
}
```

Remove or bypass creation of `OnHandlers` and old `InterruptBindings` from executor handlers. Leave parser support in place only so the semantic diagnostic can point at the old syntax.

- [ ] **Step 4: Verify**

Run:

```bash
go test ./compiler -run TestNegativeFixtures -v
go test ./compiler/sem -run Interrupt -v
git diff --check
```

Expected: negative fixture PASSes. Update every semantic test that currently expects successful `on path.interrupt` checking so it now expects SEM0042. Do not leave a test that asserts old `on` success.

- [ ] **Step 5: Commit**

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
	publish := &TopicPublish{TopicLabel: "counter", Kind: "gap_u64", Value: &ConstInt{Value: 1}}
	tryNext := &TopicTryNext{TopicLabel: "counter", Subscription: &Local{Symbol: "sub"}, Type: Type{Name: "U64TopicNext"}}
	arm := &TopicArmWait{TopicLabel: "counter", Subscription: &Local{Symbol: "sub"}}
	reliable := &ReliableTopicTryPublish{TopicLabel: "commands", Value: &ConstInt{Value: 7}, Type: Type{Name: "U64PublishResult"}}
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
	start := &VcpuStart{VcpuID: 1, Executor: &Local{Symbol: "worker"}, SlotLabel: "worker"}
	enter := &VcpuEnter{VcpuID: 0, Executor: &Local{Symbol: "main"}, SlotLabel: "main"}
	if start.VcpuID != 1 || enter.VcpuID != 0 {
		t.Fatalf("bad vcpu ids")
	}
	if _, ok := any(start).(Operation); !ok {
		t.Fatal("VcpuStart must be operation")
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
func (*TopicPublish) isOperation() {}

type ReliableTopicTryPublish struct {
	TopicLabel string
	Value      Value
	Type       Type
}
func (*ReliableTopicTryPublish) isValue() {}
func (*ReliableTopicTryPublish) isOperation() {}

type ReliableTopicWaitForAdvance struct {
	TopicLabel string
}
func (*ReliableTopicWaitForAdvance) isOperation() {}

type TopicTryNext struct {
	TopicLabel    string
	Subscription  Value
	Type          Type
}
func (*TopicTryNext) isValue() {}
func (*TopicTryNext) isOperation() {}

type TopicArmWait struct {
	TopicLabel   string
	Subscription Value
}
func (*TopicArmWait) isOperation() {}

type TopicWait struct {
	SlotLabel string
	Policy    string
}
func (*TopicWait) isOperation() {}

type VcpuStart struct {
	VcpuID    int
	Executor  Value
	SlotLabel string
}
func (*VcpuStart) isOperation() {}

type VcpuEnter struct {
	VcpuID    int
	Executor  Value
	SlotLabel string
}
func (*VcpuEnter) isOperation() {}
```

Update `valuesDefinedBy` to return values for `ReliableTopicTryPublish` and `TopicTryNext`.

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
func TestLowerTopicPublishAndTryNext(t *testing.T) {
	checked := checkedProgramForTest(t, `
module test.lower_topic
use { ExecutorSlot, SlotIdentity, OwnedHardware } from machine.x86_64.cpu_state
use { ExecutorMemory } from machine.x86_64.executor_memory
use { EventSleepPolicy } from machine.x86_64.executor_loop
use { U64GapTopic, TopicIdentity } from machine.x86_64.topic_u64
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
	program, ds := Lower(checked)
	if len(ds) != 0 { t.Fatalf("Lower diagnostics: %#v", ds) }
	fn := findFunction(program, "_wrela_method_test_lower_topic_Worker_run")
	if fn == nil { t.Fatal("missing Worker.run") }
	if !functionHasOp[*TopicTryNext](fn) || !functionHasOp[*TopicArmWait](fn) {
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
	ops = append(ops, &TopicPublish{TopicLabel: ctx.topicLabelForValue(receiver), Kind: "gap_u64", Value: value})
	return value, ops, ctx.resolveType(moduleName, "void")
}
if isIRReliablePublisher(recvType) && e.Method == "try_publish" {
	value, valueOps, _ := ctx.lowerExpr(moduleName, receiverType, scope, namedArgExpr(e.Args, "value"))
	resultType := ctx.resolveType("machine.x86_64.topic_u64", "U64PublishResult")
	op := &ReliableTopicTryPublish{TopicLabel: ctx.topicLabelForValue(receiver), Value: value, Type: ctx.irType(resultType)}
	ops := append(receiverOps, valueOps...)
	ops = append(ops, op)
	return op, ops, resultType
}
if isIRTopicSubscription(recvType) && e.Method == "try_next" {
	resultType := ctx.resolveType("machine.x86_64.topic_u64", "U64TopicNext")
	op := &TopicTryNext{TopicLabel: ctx.topicLabelForValue(receiver), Subscription: receiver, Type: ctx.irType(resultType)}
	ops := append(receiverOps, op)
	return op, ops, resultType
}
if isIRTopicSubscription(recvType) && e.Method == "arm_wait" {
	op := &TopicArmWait{TopicLabel: ctx.topicLabelForValue(receiver), Subscription: receiver}
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
		ops = append(ops, &VcpuStart{VcpuID: vcpuID, Executor: executor, SlotLabel: slotLabel})
		statusType := ctx.resolveType("machine.x86_64.cpu_state", "VcpuStartStatus")
		return executor, ops, statusType
	}
	ops = append(ops, &VcpuEnter{VcpuID: vcpuID, Executor: executor, SlotLabel: slotLabel})
	return executor, ops, ctx.resolveType(moduleName, "never")
}
```

Implement `topicLabelForValue`, `slotLabelForExecutorValue`, and `vcpuIDForValue` using metadata from `checked.ImageGraph`.

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
	"bytes"
	"testing"
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
```

- [ ] **Step 2: Run and verify failure**

Run: `go test ./compiler/codegen -run TestTopicDataLayoutIsCacheLineAligned -v`

Expected: FAIL with undefined `planTopicData`.

- [ ] **Step 3: Implement topic layout**

Create `compiler/codegen/topic_data.go`:

```go
package codegen

import "github.com/ryanwible/wrela3/compiler/ir"

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
	out := map[string]topicDataLayout{}
	for _, topic := range program.Topics {
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
		out[topic.Label] = layout
	}
	return out
}
```

- [ ] **Step 4: Wire generated data into `.data`**

Extend `buildData(program)` so every topic emits a zeroed writable data object:

```go
for _, layout := range planTopicData(program) {
	symbol := "_wrela_topic_" + sanitizeSymbol(layout.Label)
	data := make([]byte, layout.TotalSize)
	objects = append(objects, ir.DataObject{Symbol: symbol, Bytes: data})
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

Emit this algorithm:

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

Depths must be powers of two for v1. Add SEM0046 in Task 5 if a non-power-of-two depth appears.

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

Construct `U64TopicNext` into the hidden return record using existing record-return conventions.

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

Run: `go test ./compiler/codegen -run TestReliableTopicPublishChecksSlowestSubscriber -v`

Expected: FAIL because reliable publish does not inspect subscriber cursors.

- [ ] **Step 3: Implement reliable try_publish**

Emit this algorithm:

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

Emit this algorithm for `ReliableTopicWaitForAdvance`:

```text
arm producer waitline for this topic
re-read min_cursor
if capacity exists: return
hlt fallback wait
return
```

The first implementation may use `hlt` fallback. Task 12 adds cache-line wait instruction selection.

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

For `TopicWait`, use fallback `hlt` by default. Add a compile option or internal constant for monitor/mwait selection; keep the default fallback until CPUID/profile plumbing exists.

- [ ] **Step 5: Verify**

Run:

```bash
go test ./compiler/codegen -run 'Wait|Topic' -v
git diff --check
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add compiler/codegen/x64.go compiler/codegen/wait_test.go compiler/asm/encode.go compiler/asm/*test.go
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

### Task 14: vCPU Start And Bootstrap AP Trampoline

**Description:** Implement `hardware.vcpu1.start(executor = worker)` and `hardware.vcpu0.enter(executor = hello)` for a static two-vCPU q35 target.

**Files:**
- Create: `compiler/codegen/vcpu_start.go`
- Create: `compiler/codegen/vcpu_start_test.go`
- Modify: `compiler/codegen/x64.go`
- Modify: `wrela/platform/uefi/types.wrela`
- Modify: `wrela/platform/uefi/transition.wrela`

- [ ] **Step 1: Add vCPU start codegen tests**

Create `compiler/codegen/vcpu_start_test.go`:

```go
package codegen

import (
	"bytes"
	"testing"
	"github.com/ryanwible/wrela3/compiler/ir"
)

func TestVcpuStartEmitsLapicIcrWrites(t *testing.T) {
	program := &ir.Program{
		VcpuStarts: []ir.VcpuStartPlan{{VcpuID: 1, SlotLabel: "worker", Terminal: false}},
		Functions: []ir.Function{{
			Symbol: "start_worker",
			Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
				&ir.VcpuStart{VcpuID: 1, SlotLabel: "worker", Executor: &ir.Local{Symbol: "worker"}},
			}}},
		}},
	}
	image, ds := Compile(program)
	if len(ds) != 0 { t.Fatalf("Compile diagnostics: %#v", ds) }
	code := symbolBytes(t, image, "start_worker")
	if !bytes.Contains(code, []byte{0x00, 0x03, 0x00, 0x00}) {
		t.Fatalf("start must contain INIT/SIPI style APIC command immediates: %x", code)
	}
}
```

- [ ] **Step 2: Run and verify failure**

Run: `go test ./compiler/codegen -run TestVcpuStartEmitsLapicIcrWrites -v`

Expected: FAIL because `VcpuStart` is not emitted.

- [ ] **Step 3: Define startup records**

Create `compiler/codegen/vcpu_start.go`:

```go
package codegen

const (
	apTrampolineBase = 0x8000
	lapicBase        = 0xFEE00000
	lapicICRLow      = 0x300
	lapicICRHigh     = 0x310
)

type vcpuStartupRecord struct {
	ReadyOffset uint64
	EntryOffset uint64
	StackTopOffset uint64
	ContextOffset uint64
	Cr3Offset uint64
}
```

Generate writable data symbols:

```text
_wrela_vcpu1_ready
_wrela_vcpu1_entry
_wrela_vcpu1_stack_top
_wrela_vcpu1_context
```

- [ ] **Step 4: Install AP trampoline during owned transition**

In `transition.wrela`, after paging/GDT/IDT are built and before returning `OwnedHardware`, call a new delegated memory asm helper:

```wrela
self.delegated_memory.install_ap_trampoline(
    trampoline_base = 0x8000,
    cr3_value = pml4,
    gdt_base = gdt.address
)
```

The helper copies a generated trampoline byte blob to `0x8000`.

Trampoline required behavior:

```text
real mode entry at 0x8000
cli
load temporary GDT descriptor
enable PAE
load shared CR3
enable long mode in EFER
enable paging/protected mode
far jump to 64-bit label
load vcpu1 stack top
load vcpu1 context pointer
call vcpu1 entry
hlt loop if entry returns
```

Use a generated byte blob in codegen for the first implementation. Do not hand-assemble through Wrela parser if operand support is missing.

- [ ] **Step 5: Emit APIC INIT/SIPI**

For `VcpuStart{VcpuID: 1}`, emit:

```text
write APIC ID 1 to ICR high
write INIT to ICR low
short delay loop
write SIPI vector apTrampolineBase >> 12 to ICR low
short delay loop
write SIPI again
wait until _wrela_vcpu1_ready == 1 or timeout loop expires
return VcpuStartStatus(started=true,id=1)
```

Use xAPIC MMIO at `0xFEE00000`. This plan targets static QEMU q35 with APIC ID 1 for vCPU1.

- [ ] **Step 6: Emit current vCPU enter**

For `VcpuEnter{VcpuID: 0}`, emit:

```text
copy executor context into _wrela_vcpu0_context
call executor start function with context receiver
if it returns, hlt loop
```

This operation is terminal at source level.

- [ ] **Step 7: Verify**

Run:

```bash
go test ./compiler/codegen -run 'TestVcpuStartEmitsLapicIcrWrites|Vcpu' -v
git diff --check
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add compiler/codegen/vcpu_start.go compiler/codegen/vcpu_start_test.go compiler/codegen/x64.go wrela/platform/uefi/types.wrela wrela/platform/uefi/transition.wrela
git commit -m "feat: emit static two vcpu startup -Codex Automated"
```

**Acceptance Criteria:** Static q35 vCPU1 startup exists, vCPU0 enter is terminal, startup records are generated, and QEMU can be run with two vCPUs in later e2e tasks.

---

## 10. Phase 6: Interrupt Topic Hard Cutover

**Phase Description:** Replace direct interrupt-to-executor callbacks with generated ISR glue that publishes path events into path-owned topics.

**Phase Acceptance Criteria:** Serial, EDU MSI, and ivshmem MSI-X interrupts publish topic events, wake subscriber slots, acknowledge hardware/APIC, and never call executor `on` handlers.

### Task 15: Generate Interrupt Topic Dispatch Instead Of Executor Callbacks

**Description:** Replace old interrupt dispatch units that call executor handlers with ISR glue that publishes into path-owned topics.

**Files:**
- Modify: `compiler/ir/ir.go`
- Modify: `compiler/ir/lower.go`
- Modify: `compiler/codegen/x64.go`
- Modify: `compiler/codegen/interrupt_test.go`
- Modify: `wrela/machine/x86_64/serial.wrela`
- Modify: `wrela/machine/x86_64/edu.wrela`

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

Add `TopicLabel string` to `ir.InterruptBinding`.

- [ ] **Step 2: Run and verify failure**

Run: `go test ./compiler/codegen -run TestInterruptDispatchPublishesTopicInsteadOfCallingOnHandler -v`

Expected: FAIL because old dispatch calls handler functions.

- [ ] **Step 3: Lower path interrupt publisher bindings**

Use graph data from Task 4 to create interrupt bindings:

```text
path interrupt event function
topic label
event storage
vector
subscriber slots
```

Do not create `OnHandler`.

- [ ] **Step 4: Emit ISR topic publish**

Replace old dispatch algorithm with:

```text
save registers
load path receiver
call path event function
publish returned event to topic ring
wake subscriber waitlines / IPI fallback
write local APIC EOI
restore registers
iretq
```

For serial rx, publish event as:

```text
slot sequence at +0
byte at +8
```

For EDU/ivshmem, publish status as U32 widened in a 64-byte slot.

- [ ] **Step 5: Update Wrela interrupt source**

In `serial.wrela`, path event source becomes:

```wrela
driver path SerialConsolePath {
    identity: PathIdentity
    registers: SerialWriterRegisters
    rx: SerialRxPublisher

    interrupt receiver -> SerialRxEvent {
        let status = self.registers.read8(offset = 5)
        if (status & 0x01) != 0 {
            return SerialRxEvent(byte = self.registers.read8(offset = 0))
        }
        return SerialRxEvent(byte = 0)
    }
}
```

Do the same for EDU and ivshmem with their concrete event topics.

For EDU, the resulting source contract is:

```wrela
data EduInterrupt {
    status: U32
}

data EduInterruptNext {
    has_message: Bool
    message: EduInterrupt
}

class EduInterruptTopic {
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

For ivshmem, mirror the same shape with `IvshmemDoorbellTopic`, `IvshmemDoorbellPublisher`, `IvshmemDoorbellSubscription`, and `IvshmemDoorbellNext`.

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
git add compiler/ir/ir.go compiler/ir/lower.go compiler/codegen/x64.go compiler/codegen/interrupt_test.go wrela/machine/x86_64/serial.wrela wrela/machine/x86_64/edu.wrela
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
    let serial_path = serial_driver.create_console_path(
        identity = PathIdentity(label = "multi.console")
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

    let counter_topic = U64GapTopic(id = 0, depth = 64)
    let result_topic = U64ReliableTopic(id = 1, depth = 4)

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
let serial_rx_topic = SerialRxTopic(id = 0)
let serial_path = serial_driver.create_console_path(
    identity = PathIdentity(label = "hello.console"),
    rx = serial_rx_topic.publisher()
)
let edu_interrupt_topic = EduInterruptTopic(id = 1)
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
- Modify: affected compiler tests

- [ ] **Step 1: Run old-shape search**

Run:

```bash
rg -n "owner = hardware\\.vcpu|hardware\\.vcpu0\\.memory|\\.run\\(\\)|on .*\\.interrupt" examples tests/e2e/fixtures compiler
```

Expected: matches exist before this task.

- [ ] **Step 2: Rewrite fixtures**

For each fixture:

- claim one executor slot
- claim executor memory with `owner = slot`
- construct executor with `slot = slot`
- replace `executor.run()` with `hardware.vcpu0.enter(executor = executor)`
- replace path `owner = hardware.vcpu0` with `identity = PathIdentity(...)`
- migrate `on` handlers to topic-draining loops

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
rg -n "owner = hardware\\.vcpu|hardware\\.vcpu0\\.memory|\\.run\\(\\)|on .*\\.interrupt" examples tests/e2e/fixtures wrela
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
rg -n "owner = hardware\\.vcpu|hardware\\.vcpu0\\.memory|\\.run\\(\\)|on .*\\.interrupt" examples tests/e2e/fixtures wrela
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
