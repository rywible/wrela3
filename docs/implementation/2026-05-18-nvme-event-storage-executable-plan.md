# NVMe Event Storage Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build Wrela's first direct-NVMe durable event store: interrupt-driven NVMe paths, fixed 512-byte event slots, atomic event groups, dense stream directories, encrypted-API blob extents, rebuildable projections, and replay after reboot.

**Architecture:** The foreground/app/display core owns the only `StorageWriter`, append-log `event_id` allocation, durable frontier publication, stream directory heads, and maintenance proposal acceptance. A maintenance core owns projection updates, blob relocation, orphan collection, checkpoint rebuild, and cold segment packing through a separate background NVMe path. NVMe is a private backend for events, blobs, checkpoints, and projections; it is not exposed as an ambient block device or filesystem.

**Tech Stack:** Go 1.22+; existing Wrela lexer/parser/semantic checker/IR/x86_64 codegen; Wrela modules under `wrela/`; QEMU q35 + OVMF + NVMe device for end-to-end tests; Go unit tests for compiler contracts and byte-format algorithms.

---

## 0. Execution Rules

**Description:** This plan is written so a junior engineer can implement one task without inventing missing contracts, test helpers, or storage behavior. Tasks are intentionally small. If a task appears to require a design decision not present here, stop and ask.

**Acceptance Criteria:**

- Every task has exact prerequisites, file ownership, step-by-step failing-test workflow, implementation code examples, verification commands, and commit command.
- No task depends on a helper unless the helper is listed in Section 2 as existing or created by an earlier task.
- Every task commit message ends with `-Codex Automated`.
- Each task runs `git diff --check` before commit.
- Full plan completion requires `go test ./...`, `go test ./tests/e2e -run NvmeEventStorage -v`, and a placeholder scan command that exits successfully only when no red-flag phrases are present.

**Code Example:**

```bash
go test ./compiler/storagefmt -run TestCRC32CVector -v
git diff --check
git add compiler/storagefmt/format.go compiler/storagefmt/format_test.go
git commit -m "test: add storage format harness -Codex Automated"
```

Assumptions:

- The physical arena memory, production substrate convergence, and language expressiveness plans have landed before this plan executes.
- `event` and `projection` become top-level declaration keywords.
- `id`, `layout`, `current`, and `upcast` are contextual identifiers, not global keywords.
- There is no `worker` keyword in this milestone; projection workers are ordinary classes/executors pinned by boot wiring.
- `match` arms use the existing Wrela parser syntax `Pattern => { statements }`. The `->` token is valid for phase/function return types and storage upcast field mappings, not for `match` arms.
- V1 accepts active NVMe LBAs of 512 or 4096 bytes. Other LBA sizes fail at storage initialization.
- Conventional namespaces are required. Zoned Namespace support is detected and represented; zone append is optional and covered by unit tests with synthetic Identify data.
- Stream IDs are never reused in v1, including after delete.
- `event_type_id` means the durable event declaration ID from source, for example `event FileCreated id 1001`.
- `event_id` means the append-log sequence number assigned by the `StorageWriter`.
- The first append-log `event_id` is `0`. A two-event first atomic group therefore has `first_event_id = 0` and `last_event_id = 1`.

Definition of done for one task:

- The failing test step fails with the expected missing symbol, diagnostic, or assertion.
- The implementation step changes only the files listed in the task.
- The passing test step passes.
- `git diff --check` passes.
- The exact commit command in the task succeeds.

Definition of done for the full plan:

- `go test ./...` passes.
- `go test ./tests/e2e -run NvmeEventStorage -v` passes on a QEMU/OVMF-capable machine.
- Storage replay proves `last_event_id=1` for the first two-event atomic group.
- Storage reports include foreground/background NVMe paths, selected durability mode, event-slot metrics, blob orphan bytes, projection lag, stream directory cache hit rate, and device media-write counters.
- This command exits 0:

```bash
bad_terms='TO''DO|TB''D|fill'' in|implement'' later|Add'' appropriate|similar'' to'' Task|if'' needed|or'' equivalent'
if rg -n "$bad_terms" wrela compiler tests docs/implementation/2026-05-18-nvme-event-storage-executable-plan.md; then
  exit 1
fi
bad_match=$(printf '%s%s' 'match .*-' '>')
if rg -n "$bad_match" docs/implementation/2026-05-18-nvme-event-storage-executable-plan.md tests/fixtures tests/e2e wrela; then
  exit 1
fi
```

---

## 1. Frozen Storage Contracts

**Description:** These contracts are implementation inputs, not suggestions. Do not reopen them inside task work.

**Acceptance Criteria:**

- Tests use these constants and values.
- Runtime source mirrors these constants exactly.
- Any proposed change to this section requires a separate design update.

**Code Example:**

```wrela
const STORAGE_EVENT_SLOT_SIZE: U64 = 512
const STORAGE_EVENT_HEADER_SIZE: U64 = 64
const STORAGE_EVENT_PAYLOAD_BYTES: U64 = 448
const STORAGE_TARGET_BATCH_SLOTS: U64 = 64
const STORAGE_MAX_OVERFLOW_SLOTS: U64 = 8
const STORAGE_MAX_BATCH_SLOTS: U64 = 72
const STORAGE_MAX_ATOMIC_GROUP_SLOTS: U64 = 32
const STORAGE_GROUP_COMMIT_TIMER_US: U64 = 2000
const STORAGE_HOT_SEGMENT_SLOTS: U64 = 1048576
const STORAGE_HOT_SEGMENT_BYTES: U64 = 536870912
```

Hot event slot layout is exactly 512 bytes, little-endian:

```text
offset  size  field
0       8     event_id
8       8     stream_id
16      8     stream_sequence
24      4     event_type_id
28      4     payload_layout_id
32      4     atomic_group_len
36      4     atomic_group_index
40      4     payload_length
44      4     flags
48      4     checksum32
52      2     header_version
54      2     reserved16
56      8     reserved64
64      448   payload
```

CRC32C:

```text
name: CRC32C / Castagnoli
normal polynomial: 0x1EDC6F41
reflected polynomial used by Go hash/crc32.MakeTable(crc32.Castagnoli): 0x82F63B78
input for slot checksum: bytes 0..511 with bytes 48..51 set to zero
digest byte order in slot: little-endian U32
test vector: CRC32C("123456789") = 0xE3069283
```

Reserved empty slot rule:

```text
event_type_id = 0
payload_layout_id = 0
stream_id = 0
stream_sequence = 0
atomic_group_len = 0
atomic_group_index = 0
payload_length = 0
flags has STORAGE_SLOT_RESERVED_EMPTY set
event_id stores the consumed append-log sequence position
checksum32 is valid
```

Batch policy:

```text
target_batch_slots = 64
max_overflow_slots = 8
max_batch_slots = 72
max_atomic_group_slots = 32
```

Default 4 GiB QEMU storage layout. The raw disk file must be sparse-created with `os.Truncate`, not filled with zeros:

```text
offset bytes      size bytes        region
0                 8192              double-buffered superblock copies
8192              1048576           region map
1056768           536870912         hot event slot region
537927680         67108864          segment map
605036544         536870912         sealed segment extents
1141907456        268435456         stream directory and cache chunks
1410342912        1610612736        blob extents
3020955648        268435456         blob manifests and key metadata
3289391104        536870912         projection storage
3826262016        268435456         maintenance metadata
4094697472        72159232          reserved tail
```

This layout fits one 512 MiB hot segment and leaves room for blobs and projections. `hot_window_min_sealed_segments = 4` remains a production policy constant, not a requirement that the QEMU fixture preallocates four hot segments.

Stream directory entry layout is exactly 32 bytes:

```text
offset  size  field
0       8     latest_sequence
8       8     latest_event_id
16      8     latest_checkpoint_ref
24      8     flags
```

NVMe queue depths:

```text
admin_queue_depth = 32
foreground_io_queue_depth = 256
background_io_queue_depth = 128
max_prp_transfer_bytes = 131072
```

Blob limits:

```text
blob_inline_extent_count = 4
blob_manifest_extent_limit = 128
blob_allocator_free_extent_limit = 1024
blob_cipher_development_passthrough = 1
```

NVMe controller bring-up sequence:

```text
1. Read CAP, VS, and doorbell stride from CAP.DSTRD.
2. Write CC.EN = 0.
3. Wait for CSTS.RDY = 0 with bounded reset timeout.
4. Allocate admin SQ/CQ DMA memory.
5. Program AQA, ASQ, and ACQ.
6. Program CC with IOSQES=6, IOCQES=4, selected command set, and EN=1.
7. Wait for CSTS.RDY = 1 with bounded ready timeout.
8. Submit Identify Controller opcode 0x06, CNS=1.
9. Submit Identify Namespace opcode 0x06, CNS=0, NSID=1.
10. Create foreground and background IO completion queues.
11. Create foreground and background IO submission queues.
12. Route one MSI-X vector per IO completion queue when possible; fall back to MSI only when MSI-X is unavailable.
```

Identify Namespace byte offsets used by v1 tests:

```text
0..7      NSZE
26       FLBAS active LBA format low nibble
128..191 LBAF table entries, each 4 bytes
LBAF[i] byte 2 contains LBADS, so logical_block_size = 1 << LBADS
```

FLBAS byte 26 follows the NVMe Identify Namespace layout and is the implementation contract now.

Identify Controller byte offsets used by v1 tests:

```text
256      VWC volatile write cache bit 0
512..513 AWUN
514..515 AWUPF
521      ONCS bit 3 means write zeroes, not FUA; FUA is command flag support in v1 policy source
```

Durability acknowledgement rule:

```text
selected mode FUA or PFAIL_ATOMIC_FUA:
  acknowledge after all batch write completions succeed

selected mode WRITE_PLUS_FLUSH:
  acknowledge only after all batch write completions and the following flush completion succeed
```

---

## 2. Test Harness And Existing Helpers

**Description:** This section names every helper a later task may use. If a helper is not listed here, the task must create it before using it.

**Acceptance Criteria:**

- Later tasks reference only helpers from this section or helpers created by their own steps.
- Helper file ownership is exclusive to the task that creates it.

**Code Example:**

```go
func TestStorageHarnessCompiles(t *testing.T) {
	if CRC32C([]byte("123456789")) != 0xE3069283 {
		t.Fatal("bad CRC32C vector")
	}
}
```

Existing helpers:

```text
compiler/sem/testutil_test.go:
  parseModulesForTest
  mustBuildIndex
  mustCheck
  mustBuildIndexAllowingMissingImage
  checkAllowingMissingImage
  typeDiagsForModules

compiler/sem/hardware_authority_test.go:
  checkUEFIModulesWithExtraSource

compiler/sem/uefi_source_shape_test.go:
  parseUEFIModuleSet
  moduleType
  fieldTypeName

compiler/sem/types_test.go:
  assertMethodExists

compiler/negative_fixtures_test.go:
  TestNegativeFixtures

compiler/qemu/run.go:
  qemu.Options
  qemu.Run
  qemu.Args
```

Source module loading rule:

```text
Tests that call parseUEFIModuleSet must update its hard-coded file list in the same task that creates a new machine-level runtime module.
Tasks 27 and 29 do this for core_link.wrela and nvme.wrela.
Storage modules under wrela/storage/* must use explicit parseModulesForTest/checkAllowingMissingImage source graphs in their own sem tests unless that task also updates parseUEFIModuleSet.
```

New helper packages created by this plan:

```text
compiler/storagefmt
  Host-side mirror of fixed storage byte formats, CRC32C, batch packing, superblock selection, stream directory math, region layout, blob free-list logic, and recovery validation.

compiler/nvmefmt
  Host-side NVMe Identify parsing, durability mode selection, command dword construction, queue phase handling, and acknowledgement state machine.

compiler/ir/storage_testutil_test.go
  checkedStorageProgramForTest for IR metadata tests.

compiler/codegen/storage_testutil_test.go
  storageEncoderProgramForCodegenTest and findTextUnit for codegen tests.

tests/e2e/nvme_testutil_test.go
  createSparseRawDisk, runStorageQEMU, and qemu.Options NVMe argument extension.
```

Mirror contract:

```text
Every task that has a Go host mirror plus Wrela runtime source must prove the two stay aligned.
If the behavior is tested only in compiler/storagefmt or compiler/nvmefmt, the paired Wrela task must also:
  1. include the exact Wrela implementation block in the task,
  2. compile and typecheck that Wrela source, and
  3. add a mirror-contract test that compares exported names and constant values against the host helper.

Compile-only Wrela tests are allowed for language wiring, but they are not sufficient for storage format, NVMe, stream directory, writer, blob, projection, or replay behavior.
```

---

## 3. Repository Layout And File Ownership

**Description:** These file ownership boundaries prevent the merge conflicts called out in review. Shared compiler files get narrow owner tasks; later tasks add storage-specific files instead of repeatedly editing `compiler/sem/check.go`.

**Acceptance Criteria:**

- `compiler/sem/check.go` is modified only in Tasks 9 and 12.
- `compiler/ir/lower.go` is modified only in Tasks 14, 15, and 18.
- `compiler/codegen/x64.go` is modified only in Tasks 17 and 18.
- `compiler/sem/uefi_source_shape_test.go` module lists are modified only by tasks that create runtime modules used by `parseUEFIModuleSet`.
- Runtime source tasks create or modify only their named `wrela/...` files plus any task-listed source compile tests or `parseUEFIModuleSet` registration edits.

**Code Example:**

```text
compiler/sem/storage.go
  All storage declaration validation and storage authority checks after Task 12.

compiler/sem/storage_graph.go
  Storage image graph nodes, NVMe path ownership, core-link endpoints, projection feeds.

compiler/storagefmt/*.go
  Behavior-testable host mirror for disk format algorithms.

compiler/nvmefmt/*.go
  Behavior-testable host mirror for NVMe identify, commands, and completion logic.
```

Runtime files:

```text
wrela/machine/x86_64/core_link.wrela
wrela/machine/x86_64/nvme.wrela
wrela/storage/format.wrela
wrela/storage/event_log.wrela
wrela/storage/stream.wrela
wrela/storage/blob.wrela
wrela/storage/projection.wrela
wrela/storage/writer.wrela
wrela/storage/file_model.wrela
```

Test files:

```text
compiler/storagefmt/*_test.go
compiler/nvmefmt/*_test.go
compiler/lex/lexer_test.go
compiler/ast/ast_test.go
compiler/parse/parser_test.go
compiler/sem/storage_*_test.go
compiler/ir/storage_*_test.go
compiler/codegen/storage_*_test.go
tests/e2e/nvme_event_storage_qemu_test.go
tests/e2e/nvme_testutil_test.go
```

---

## 4. Parallel Work Map

**Description:** Parallel work is allowed only where file ownership is disjoint. The map below names the merge owner for shared files.

**Acceptance Criteria:**

- A worker starts a task only after its prerequisites have landed.
- If a task needs a shared file outside its ownership, it stops and asks the merge owner.

**Code Example:**

```text
Merge Gate 0:
  Tasks 1-4 land first. They create diagnostics and test harnesses.

Stream A, Durable Syntax:
  Tasks 5-18.
  Task 5 and Task 6 may run in parallel.
  Task 7 waits for Tasks 5 and 6.
  Tasks 8-12 are serial.
  Tasks 13-18 are serial because they share IR and codegen ownership.

Stream B, Runtime Format/Event/Stream:
  Tasks 19-26.
  Tasks 19 and 21 may run in parallel after Tasks 2, 12, and 18.
  Task 20 waits for Task 19.
  Tasks 22-26 are serial because each builds on the previous storage writer surface.

Stream C, CoreLink And NVMe:
  Tasks 27-34.
  Task 27 may start after Task 4.
  Task 29 waits for Tasks 3 and 27 because both source tasks update `compiler/sem/uefi_source_shape_test.go`.
  Task 28 waits for Tasks 12 and 27.
  Tasks 30-34 are serial after Task 29 because they share `wrela/machine/x86_64/nvme.wrela`.

Stream D, Blob:
  Tasks 35-38.
  Task 35 waits for Tasks 19 and 26.
  Tasks 36-38 are serial.

Stream E, Projections/File:
  Tasks 39-42.
  Starts after Tasks 12, 26, 34, and 38.

Stream F, E2E/Metrics:
  Tasks 43-46.
  Starts after Streams B-E land.
```

---

## 5. Phase 0: Diagnostics And Test Harnesses

**Description:** This phase creates the stable diagnostic and helper foundation. Later tasks must not invent test helpers.

**Acceptance Criteria:**

- Storage diagnostic codes exist.
- Host-side storage and NVMe behavior packages exist with vector tests.
- QEMU helper APIs exist before E2E tasks reference them.

### Task 1: Storage Diagnostics And Deferred Scope

**Prerequisite:** None.

**Files:**

- Modify: `compiler/diag/codes.go`
- Modify: `compiler/diag/diag_test.go`
- Modify: `docs/production-deferred-work.md`

**Description:** Reserve storage diagnostics and record what this milestone deliberately excludes.

**Acceptance Criteria:**

- `SEM0099` through `SEM0124` exist with exact meanings below.
- Deferred-work docs say POSIX, SQL, replication, production command inboxes, IOMMU isolation, full-disk encryption, SGL, tuned compression, and destructive retention remain outside this milestone.

**Code Examples:**

```go
func TestStorageDiagnosticCodesExist(t *testing.T) {
	want := map[string]string{
		"SEM0099": diag.SEM0099,
		"SEM0100": diag.SEM0100,
		"SEM0101": diag.SEM0101,
		"SEM0102": diag.SEM0102,
		"SEM0103": diag.SEM0103,
		"SEM0104": diag.SEM0104,
		"SEM0105": diag.SEM0105,
		"SEM0106": diag.SEM0106,
		"SEM0107": diag.SEM0107,
		"SEM0108": diag.SEM0108,
		"SEM0109": diag.SEM0109,
		"SEM0110": diag.SEM0110,
		"SEM0111": diag.SEM0111,
		"SEM0112": diag.SEM0112,
		"SEM0113": diag.SEM0113,
		"SEM0114": diag.SEM0114,
		"SEM0115": diag.SEM0115,
		"SEM0116": diag.SEM0116,
		"SEM0117": diag.SEM0117,
		"SEM0118": diag.SEM0118,
		"SEM0119": diag.SEM0119,
		"SEM0120": diag.SEM0120,
		"SEM0121": diag.SEM0121,
		"SEM0122": diag.SEM0122,
		"SEM0123": diag.SEM0123,
		"SEM0124": diag.SEM0124,
	}
	if len(want) != 26 {
		t.Fatalf("diagnostic table size = %d, want 26", len(want))
	}
	seen := map[string]string{}
	for expected, got := range want {
		if got != expected {
			t.Fatalf("%s constant = %q", expected, got)
		}
		if previous, ok := seen[got]; ok {
			t.Fatalf("%s and %s both use %q", previous, expected, got)
		}
		seen[got] = expected
	}
}
```

```go
SEM0099  = "SEM0099"  // duplicate durable event type id
SEM0100  = "SEM0100"  // invalid durable event type id
SEM0101  = "SEM0101"  // invalid event layout current marker
SEM0102  = "SEM0102"  // duplicate event layout id
SEM0103  = "SEM0103"  // invalid event layout field
SEM0104  = "SEM0104"  // invalid event upcast endpoint
SEM0105  = "SEM0105"  // missing event upcast field mapping
SEM0106  = "SEM0106"  // duplicate projection id
SEM0107  = "SEM0107"  // invalid projection layout current marker
SEM0108  = "SEM0108"  // unsupported projection container shape
SEM0109  = "SEM0109"  // invalid projection upcast endpoint
SEM0110  = "SEM0110"  // storage disk region overlap or invalid size
SEM0111  = "SEM0111"  // NVMe queue path ownership mismatch
SEM0112  = "SEM0112"  // core link endpoint ownership mismatch
SEM0113  = "SEM0113"  // StorageWriter authority cannot be forged or shared
SEM0114  = "SEM0114"  // atomic event group exceeds configured maximum
SEM0115  = "SEM0115"  // unstable append-log event_id source is not allowed
SEM0116  = "SEM0116"  // storage append must observe durability result
SEM0117  = "SEM0117"  // blob ref references unpublished blob bytes
SEM0118  = "SEM0118"  // maintenance proposal mutates truth directly
SEM0119  = "SEM0119"  // projection root watermark is invalid
SEM0120  = "SEM0120"  // projection worker feed is not boot wired
SEM0121  = "SEM0121"  // event payload exceeds inline slot budget
SEM0122  = "SEM0122"  // active NVMe LBA size is unsupported by v1 storage
SEM0123  = "SEM0123"  // development blob cipher used without explicit opt in
SEM0124  = "SEM0124"  // storage metric publication is incomplete
```

- [ ] **Step 1: Add failing diagnostic test**

Add `TestStorageDiagnosticCodesExist` to `compiler/diag/diag_test.go`.

Run: `go test ./compiler/diag -run TestStorageDiagnosticCodesExist -v`

Expected: FAIL with undefined identifiers such as `diag.SEM0099`.

- [ ] **Step 2: Add diagnostic constants**

Add constants after `SEM0098` in `compiler/diag/codes.go`.

Run: `go test ./compiler/diag -run TestStorageDiagnosticCodesExist -v`

Expected: PASS.

- [ ] **Step 3: Add deferred-work section**

Append this section to `docs/production-deferred-work.md`:

```markdown
## Storage beyond the first NVMe event-store milestone
- POSIX filesystem compatibility remains out of scope. The first storage surface is events, blobs, checkpoints, projections, and file-like entity streams.
- SQL, relational query planning, general secondary indexes, multi-writer event-log sharding, and network replication remain out of scope.
- Production command inboxes, idempotency records, full-disk encryption, IOMMU-backed DMA isolation, SGL-heavy NVMe transfers, tuned compression codec selection, and destructive retention policies remain deferred.
- The first blob cipher used by QEMU tests is a named development passthrough mode behind the final blob manifest API. Production images must not construct it without explicit development-storage opt in.
```

Run:

```bash
rg -n "Storage beyond the first NVMe event-store milestone|POSIX filesystem compatibility remains out of scope" docs/production-deferred-work.md
git diff --check
```

Expected: `rg` prints both matches and `git diff --check` prints nothing.

- [ ] **Step 4: Commit**

```bash
git diff --check
git add compiler/diag/codes.go compiler/diag/diag_test.go docs/production-deferred-work.md
git commit -m "docs: freeze nvme event storage diagnostics -Codex Automated"
```

### Task 2: Storage Format Behavior Harness

**Prerequisite:** Task 1.

**Files:**

- Create: `compiler/storagefmt/format.go`
- Create: `compiler/storagefmt/format_test.go`

**Description:** Create a Go package that behavior-tests byte-format algorithms before Wrela runtime code mirrors them.

**Acceptance Criteria:**

- CRC32C Castagnoli vector passes.
- Event slot header offsets are constants, not inferred by string scanning.
- Batch packing for 512 and 4096 byte LBAs is behavior-tested.
- Region layout sums to less than or equal to 4 GiB and has no overlaps.

**Code Examples:**

```go
package storagefmt

import "hash/crc32"

const (
	EventSlotSize          = 512
	EventHeaderSize        = 64
	EventPayloadBytes      = 448
	EventTypeIDOffset      = 24
	PayloadLayoutIDOffset  = 28
	Checksum32Offset       = 48
	StorageDiskBytes       = 4 * 1024 * 1024 * 1024
	StorageHotSegmentBytes = 536870912
)

var crcTable = crc32.MakeTable(crc32.Castagnoli)

func CRC32C(data []byte) uint32 {
	return crc32.Checksum(data, crcTable)
}

type BatchPacking struct {
	SemanticSlots      uint64
	ReservedEmptySlots uint64
	TotalSlotPositions uint64
}

func FinishBatch(activeLBASize, semanticSlots uint64) BatchPacking {
	slotsPerLBA := activeLBASize / EventSlotSize
	remainder := semanticSlots % slotsPerLBA
	empty := uint64(0)
	if remainder != 0 {
		empty = slotsPerLBA - remainder
	}
	return BatchPacking{SemanticSlots: semanticSlots, ReservedEmptySlots: empty, TotalSlotPositions: semanticSlots + empty}
}
```

```go
func TestCRC32CVector(t *testing.T) {
	if got, want := CRC32C([]byte("123456789")), uint32(0xE3069283); got != want {
		t.Fatalf("CRC32C vector = %#x, want %#x", got, want)
	}
}

func TestFourKiBUnderfillConsumesEmptySlots(t *testing.T) {
	got := FinishBatch(4096, 3)
	if got.ReservedEmptySlots != 5 || got.TotalSlotPositions != 8 {
		t.Fatalf("FinishBatch(4096, 3) = %#v, want 5 empty and 8 total", got)
	}
}
```

- [ ] **Step 1: Add failing tests**

Create `compiler/storagefmt/format_test.go` with `TestCRC32CVector`, `TestFourKiBUnderfillConsumesEmptySlots`, and `TestRegionLayoutFitsSparse4GiBDisk`.

Run: `go test ./compiler/storagefmt -v`

Expected: FAIL because package files do not exist or symbols are undefined.

- [ ] **Step 2: Add package implementation**

Create `compiler/storagefmt/format.go` with constants, `CRC32C`, `FinishBatch`, `Region`, and `DefaultRegions`.

Run: `go test ./compiler/storagefmt -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add compiler/storagefmt/format.go compiler/storagefmt/format_test.go
git commit -m "test: add storage format behavior harness -Codex Automated"
```

### Task 3: NVMe Behavior Harness

**Prerequisite:** Task 1.

**Files:**

- Create: `compiler/nvmefmt/nvme.go`
- Create: `compiler/nvmefmt/nvme_test.go`

**Description:** Create behavior-testable NVMe helpers for Identify parsing, durability selection, FUA command dword construction, completion phase handling, and acknowledgement state.

**Acceptance Criteria:**

- Identify Namespace parser returns 512 or 4096 from synthetic LBAF bytes.
- Unsupported LBA size returns an explicit error.
- Durability selection chooses FUA when supported and write-plus-flush otherwise.
- Write command dword 12 sets bit 30 when FUA is requested.
- Completion phase toggles when head wraps.

**Code Examples:**

```go
package nvmefmt

import "encoding/binary"

type NamespaceFacts struct {
	LogicalBlockSize uint64
	SupportsFUA bool
	VolatileWriteCache bool
	PowerFailAtomicWriteUnitBlocks uint32
}

func ParseIdentifyNamespace(data []byte) (NamespaceFacts, error) {
	flbas := data[26] & 0x0f
	lbads := data[128+int(flbas)*4+2]
	size := uint64(1) << lbads
	if size != 512 && size != 4096 {
		return NamespaceFacts{}, ErrUnsupportedLBA
	}
	return NamespaceFacts{LogicalBlockSize: size}, nil
}

func WriteCommandDword12(blockCount uint32, fua bool) uint32 {
	value := blockCount - 1
	if fua {
		value |= 1 << 30
	}
	return value
}

func PutLE64(dst []byte, off int, value uint64) {
	binary.LittleEndian.PutUint64(dst[off:off+8], value)
}
```

- [ ] **Step 1: Add failing NVMe behavior tests**

Create tests for `ParseIdentifyNamespace`, `WriteCommandDword12`, and completion phase wrap.

Run: `go test ./compiler/nvmefmt -v`

Expected: FAIL because package files do not exist or symbols are undefined.

- [ ] **Step 2: Implement the minimal helpers**

Create `compiler/nvmefmt/nvme.go` with the exact exported functions referenced by tests.

Run: `go test ./compiler/nvmefmt -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add compiler/nvmefmt/nvme.go compiler/nvmefmt/nvme_test.go
git commit -m "test: add nvme behavior harness -Codex Automated"
```

### Task 4: E2E NVMe QEMU Harness

**Prerequisite:** Task 1.

**Files:**

- Modify: `compiler/qemu/run.go`
- Modify: `compiler/qemu/run_test.go`
- Create: `tests/e2e/nvme_testutil_test.go`

**Description:** Add real QEMU helper APIs before storage E2E tasks use them. The current `qemu.Options` has no generic extra-arg field, so this task adds one.

**Acceptance Criteria:**

- `qemu.Options.ExtraArgs []string` exists and is appended at the end of `qemu.Args`.
- `createSparseRawDisk(t, path, bytes)` uses `os.Truncate`.
- `runStorageQEMU(t, disk, mode)` builds `tests/e2e/fixtures/nvme_event_storage/main.wrela`, runs QEMU with an NVMe drive, and returns serial output.
- Disk size is `4 * 1024 * 1024 * 1024` bytes.

**Code Examples:**

```go
type Options struct {
	QEMUBinary string
	OVMFCode string
	OVMFVars string
	ESPDir string
	ImagePath string
	SuccessText string
	Timeout time.Duration
	SMP int
	ExtraArgs []string
}

func Args(opts Options) []string {
	args := []string{
		"-machine", "q35",
		"-drive", "if=pflash,format=raw,readonly=on,file=" + opts.OVMFCode,
		"-drive", "if=pflash,format=raw,file=" + opts.OVMFVars,
		"-drive", "format=raw,file=fat:rw:" + opts.ESPDir,
		"-serial", "stdio",
		"-display", "none",
		"-no-reboot",
	}
	args = append(args, opts.ExtraArgs...)
	return args
}
```

```go
func createSparseRawDisk(t *testing.T, path string, bytes int64) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := f.Truncate(bytes); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 1: Add failing qemu.Args test**

Add a test in `compiler/qemu/run_test.go`:

```go
func TestArgsAppendsExtraArgs(t *testing.T) {
	args := Args(Options{ImagePath: "boot.efi", ExtraArgs: []string{"-device", "nvme,serial=test"}})
	if got := strings.Join(args, " "); !strings.Contains(got, "-device nvme,serial=test") {
		t.Fatalf("QEMU args missing extra args: %s", got)
	}
}
```

Run: `go test ./compiler/qemu -run TestArgsAppendsExtraArgs -v`

Expected: FAIL because `Options.ExtraArgs` is undefined.

- [ ] **Step 2: Implement ExtraArgs**

Modify `compiler/qemu/run.go` as shown.

Run: `go test ./compiler/qemu -run TestArgsAppendsExtraArgs -v`

Expected: PASS.

- [ ] **Step 3: Add E2E helper file**

Create `tests/e2e/nvme_testutil_test.go`:

```go
package e2e

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ryanwible/wrela3/compiler"
	"github.com/ryanwible/wrela3/compiler/qemu"
)

const nvmeStorageDiskBytes int64 = 4 * 1024 * 1024 * 1024

func createSparseRawDisk(t *testing.T, path string, bytes int64) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := f.Truncate(bytes); err != nil {
		t.Fatal(err)
	}
}

func runStorageQEMU(t *testing.T, disk string, mode string) string {
	t.Helper()
	deps := requireQEMUDeps(t, false)
	tmp := t.TempDir()
	vars := filepath.Join(tmp, "OVMF_VARS.fd")
	copyFile(t, deps.firmware.Vars, vars)
	image := filepath.Join(tmp, "nvme-storage.efi")
	_, err := compiler.Build(compiler.BuildOptions{
		Mode: compiler.ModeDev,
		RootPath: "tests/e2e/fixtures/nvme_event_storage/main.wrela",
		OutputPath: image,
		RepoRoot: ".",
	})
	if err != nil {
		t.Fatalf("build nvme storage image: %v", err)
	}
	out, err := qemu.Run(qemu.Options{
		QEMUBinary: deps.qemuBin,
		OVMFCode: deps.firmware.Code,
		OVMFVars: vars,
		ESPDir: filepath.Join(tmp, "esp"),
		ImagePath: image,
		SuccessText: "NVME_STORAGE_DONE",
		Timeout: qemuTimeout(),
		SMP: 2,
		ExtraArgs: []string{
			"-drive", "file=" + disk + ",if=none,id=nvme0,format=raw",
			"-device", "nvme,drive=nvme0,serial=wrela-storage-0",
			"-fw_cfg", "name=wrela.storage.mode,string=" + mode,
		},
	})
	if err != nil {
		t.Fatalf("qemu failed: %v\nserial output:\n%s", err, out)
	}
	return out
}
```

Run: `go test ./tests/e2e -run TestNonexistentNvmeHarnessCompile -count=0`

Expected: PASS compile-only.

- [ ] **Step 4: Commit**

```bash
git diff --check
git add compiler/qemu/run.go compiler/qemu/run_test.go tests/e2e/nvme_testutil_test.go
git commit -m "test: add nvme qemu harness -Codex Automated"
```

---

## 6. Phase 1: Durable Declaration Syntax

**Description:** This phase adds event/projection syntax in small compiler slices: token, AST, parser, semantic IDs, layout rules, projection rules, IR metadata, and codegen hooks.

**Acceptance Criteria:**

- Lexer, AST, parser, semantic, IR, and codegen work each land in separate commits.
- Parser tests assert AST fields, not source text.
- Semantic tests assert diagnostics from compiler APIs.

### Task 5: Lexer Keywords

**Prerequisite:** Task 1.

**Files:**

- Modify: `compiler/lex/token.go`
- Modify: `compiler/lex/lexer_test.go`
- Modify: `compiler/parse/parser_test.go`
- Modify: `wrela/machine/x86_64/edu.wrela`
- Modify: `wrela/machine/x86_64/ivshmem.wrela`
- Modify: `wrela/machine/x86_64/serial.wrela`
- Modify: `tests/fixtures/negative/on_interrupt_rejected.wrela`
- Modify: `tests/fixtures/negative/interrupt_event_call.wrela`

**Description:** Rename existing source and parser-test identifiers named `event`, then add `event` and `projection` as keywords. Keep `id`, `layout`, `current`, and `upcast` contextual identifiers.

**Acceptance Criteria:**

- Existing runtime source, parser fixtures, and negative fixtures no longer use `event` as an identifier before the lexer reserves it.
- `event` tokenizes as `KeywordEvent`.
- `projection` tokenizes as `KeywordProjection`.
- `id`, `layout`, `current`, and `upcast` still tokenize as identifiers.

**Code Examples:**

```go
func TestStorageDeclarationKeywords(t *testing.T) {
	toks, diags := All("event FileCreated id 1001 { layout 1 current {} }\nprojection DirectoryChildren id 12 {}")
	if len(diags) != 0 {
		t.Fatalf("lex diagnostics: %#v", diags)
	}
	if toks[0].Kind != KeywordEvent {
		t.Fatalf("first token = %#v, want KeywordEvent", toks[0])
	}
	if toks[3].Kind != Identifier || toks[3].Text != "id" {
		t.Fatalf("id must remain contextual identifier, got %#v", toks[3])
	}
	if toks[6].Kind != Identifier || toks[6].Text != "layout" {
		t.Fatalf("layout must remain contextual identifier, got %#v", toks[6])
	}
}
```

- [ ] **Step 1: Confirm and remove current keyword collisions**

Run this collision scan:

```bash
rg -n '(\bevent:|value = event\b|Bind != "event"|ParamName != "event"|event = event\b|\(event\))' \
  wrela/machine/x86_64/edu.wrela \
  wrela/machine/x86_64/ivshmem.wrela \
  wrela/machine/x86_64/serial.wrela \
  compiler/parse/parser_test.go \
  tests/fixtures/negative/on_interrupt_rejected.wrela \
  tests/fixtures/negative/interrupt_event_call.wrela
```

Expected before this step: matches in the three runtime files, `compiler/parse/parser_test.go`, and the two negative fixtures.

Rename these identifiers exactly:

```wrela
fn ack_completed(self, completion: EduInterrupt)
fn ack_doorbell(self, doorbell: IvshmemDoorbellInterrupt)
fn ack_receive(self, received: U8)
Option.Some(value = rx_event) => {
on serial_path.interrupt(serial_event: Option<U8>) {
on serial_path.receive(serial_event: Option<U8>) {
on p.interrupt(interrupt_payload: Event) {}
on serial.interrupt(serial_payload: SerialInterrupt) {
```

Update parser assertions from `Bind != "event"` to `Bind != "rx_event"` and from `ParamName != "event"` to `ParamName != "serial_event"`.

Run:

```bash
if rg -n '(\bevent:|value = event\b|Bind != "event"|ParamName != "event"|event = event\b|\(event\))' \
  wrela/machine/x86_64/edu.wrela \
  wrela/machine/x86_64/ivshmem.wrela \
  wrela/machine/x86_64/serial.wrela \
  compiler/parse/parser_test.go \
  tests/fixtures/negative/on_interrupt_rejected.wrela \
  tests/fixtures/negative/interrupt_event_call.wrela; then
  exit 1
fi
```

Expected: no output.

- [ ] **Step 2: Add failing lexer test**

Run: `go test ./compiler/lex -run TestStorageDeclarationKeywords -v`

Expected: FAIL with `undefined: KeywordEvent`.

- [ ] **Step 3: Add token constants and keyword map entries**

Add `KeywordEvent` and `KeywordProjection` after `KeywordStaticAssert`. Add `"event": KeywordEvent` and `"projection": KeywordProjection`.

Run: `go test ./compiler/lex -run TestStorageDeclarationKeywords -v`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git diff --check
git add compiler/lex/token.go compiler/lex/lexer_test.go compiler/parse/parser_test.go wrela/machine/x86_64/edu.wrela wrela/machine/x86_64/ivshmem.wrela wrela/machine/x86_64/serial.wrela tests/fixtures/negative/on_interrupt_rejected.wrela tests/fixtures/negative/interrupt_event_call.wrela
git commit -m "feat: tokenize storage declarations -Codex Automated"
```

### Task 6: AST Nodes

**Prerequisite:** Task 1.

**Files:**

- Modify: `compiler/ast/ast.go`
- Modify: `compiler/ast/ast_test.go`

**Description:** Add AST nodes for event/projection declarations. Store numeric IDs as strings for precise semantic diagnostics.

**Acceptance Criteria:**

- `EventDecl` and `ProjectionDecl` implement `Decl`.
- Layout fields preserve type and optional encode expression.
- Upcast mappings preserve source and destination names.
- `DebugDecl` has deterministic output for storage declarations.

**Code Examples:**

```go
type EventDecl struct {
	Name string
	ID string
	Fields []Field
	Layouts []EventLayoutDecl
	Upcasts []LayoutUpcastDecl
	SpanV source.Span
}

func (d *EventDecl) Span() source.Span { return d.SpanV }

type EventLayoutDecl struct {
	ID string
	Current bool
	Fields []EventLayoutField
	Span source.Span
}

type EventLayoutField struct {
	Name string
	Type TypeRef
	Encode Expr
	Span source.Span
}

type ProjectionDecl struct {
	Name string
	ID string
	Layouts []ProjectionLayoutDecl
	Upcasts []LayoutUpcastDecl
	SpanV source.Span
}
```

- [ ] **Step 1: Add failing AST test**

Add `TestStorageASTContracts` that constructs an `EventDecl` and `ProjectionDecl` and asserts `DebugDecl(event) == "event FileRenamed id 17"`.

Run: `go test ./compiler/ast -run TestStorageASTContracts -v`

Expected: FAIL with undefined `EventDecl`.

- [ ] **Step 2: Add AST structs and debug branch**

Implement the structs and `DebugDecl` branches shown above.

Run: `go test ./compiler/ast -run TestStorageASTContracts -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add compiler/ast/ast.go compiler/ast/ast_test.go
git commit -m "feat: add storage declaration ast -Codex Automated"
```

### Task 7: Parser For Event Declarations

**Prerequisites:** Tasks 5 and 6.

**Files:**

- Modify: `compiler/parse/parser.go`
- Modify: `compiler/parse/parser_test.go`

**Description:** Parse `event Name id N` declarations, top-level semantic fields, layout fields, current marker, and upcast mappings.

**Acceptance Criteria:**

- Event declarations parse into `*ast.EventDecl`.
- Layout field `name: Type = expr` preserves `Encode`.
- `upcast 1 -> 2 { old -> new }` parses.
- `id`, `layout`, `current`, and `upcast` remain contextual.

**Code Examples:**

```go
func TestParseEventDeclaration(t *testing.T) {
	mod := parseModuleOK(t, `
module storage.test
event FileRenamed id 17 {
    file_id: FileId
    layout 1 {
        old_name_ref: BlobRefPayload
    }
    layout 2 current {
        file_id: U64 = self.file_id.value
    }
    upcast 1 -> 2 {
        old_name_ref -> name_ref
    }
}`)
	ev := mod.Decls[0].(*ast.EventDecl)
	if ev.ID != "17" || len(ev.Layouts) != 2 || !ev.Layouts[1].Current || len(ev.Upcasts) != 1 {
		t.Fatalf("event parsed incorrectly: %#v", ev)
	}
}
```

- [ ] **Step 1: Add failing parser test**

Run: `go test ./compiler/parse -run TestParseEventDeclaration -v`

Expected: FAIL with parser diagnostic `expected declaration` or undefined AST fields.

- [ ] **Step 2: Add parser switch case and contextual helper**

Add `case lex.KeywordEvent: return p.parseEventDecl()`. Add `consumeContextualIdentifier`.

Run: `go test ./compiler/parse -run TestParseEventDeclaration -v`

Expected: FAIL later in event body parsing.

- [ ] **Step 3: Implement event body parser**

Add `parseEventDecl`, `parseEventLayoutDecl`, `parseEventLayoutField`, and `parseLayoutUpcastDecl`.

Run: `go test ./compiler/parse -run TestParseEventDeclaration -v`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git diff --check
git add compiler/parse/parser.go compiler/parse/parser_test.go
git commit -m "feat: parse storage event declarations -Codex Automated"
```

### Task 8: Parser For Projection Declarations

**Prerequisites:** Tasks 5 and 6.

**Files:**

- Modify: `compiler/parse/parser.go`
- Modify: `compiler/parse/parser_test.go`

**Description:** Parse projection declarations separately from events. Projection layouts use normal field syntax and optional upcasts.

**Acceptance Criteria:**

- `projection Name id N { layout 1 current { children: OrderedPages<A, B, C> } }` parses into `*ast.ProjectionDecl`.
- Projection layout fields preserve generic type refs.
- Projection upcasts reuse `LayoutUpcastDecl`.

**Code Examples:**

```go
func TestParseProjectionDeclaration(t *testing.T) {
	mod := parseModuleOK(t, `
module storage.test
projection DirectoryChildren id 12 {
    layout 1 current {
        children: OrderedPages<FileId, FileNameKey, DirectoryChild>
    }
}`)
	proj := mod.Decls[0].(*ast.ProjectionDecl)
	if proj.ID != "12" || len(proj.Layouts) != 1 || !proj.Layouts[0].Current {
		t.Fatalf("projection parsed incorrectly: %#v", proj)
	}
	if got := proj.Layouts[0].Fields[0].Type.String(); got != "OrderedPages<FileId, FileNameKey, DirectoryChild>" {
		t.Fatalf("projection field type = %q", got)
	}
}
```

- [ ] **Step 1: Add failing projection parser test**

Run: `go test ./compiler/parse -run TestParseProjectionDeclaration -v`

Expected: FAIL with parser diagnostic `expected declaration`.

- [ ] **Step 2: Implement projection parser**

Add `case lex.KeywordProjection`, `parseProjectionDecl`, and `parseProjectionLayoutDecl`.

Run: `go test ./compiler/parse -run TestParseProjectionDeclaration -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add compiler/parse/parser.go compiler/parse/parser_test.go
git commit -m "feat: parse storage projection declarations -Codex Automated"
```

### Task 9: Semantic Storage Index

**Prerequisites:** Tasks 7 and 8.

**Files:**

- Create: `compiler/sem/storage.go`
- Modify: `compiler/sem/types.go`
- Modify: `compiler/sem/check.go`
- Create: `compiler/sem/storage_decl_test.go`

**Description:** Register events and projections in semantic storage metadata with stable ID maps. This is the only task that wires storage declarations into `Check`.

**Acceptance Criteria:**

- `KindEvent` and `KindProjection` exist.
- `CheckedProgram.Storage` exists.
- Duplicate durable `event_type_id` values emit `SEM0099`.
- Durable `event_type_id = 0` emits `SEM0100`.
- Duplicate projection IDs emit `SEM0106`.

**Code Examples:**

```go
type StorageIndex struct {
	EventsByTypeID map[uint64]EventInfo
	EventsByKey map[string]EventInfo
	ProjectionsByID map[uint64]ProjectionInfo
	ProjectionsByKey map[string]ProjectionInfo
}

type EventInfo struct {
	Module string
	Name string
	EventTypeID uint64
	Span source.Span
}
```

- [ ] **Step 1: Add failing semantic positive test**

Add `TestStorageIndexRecordsEventsAndProjections` using `parseModulesForTest`, `mustBuildIndexAllowingMissingImage`, and `checkAllowingMissingImage`.

Run: `go test ./compiler/sem -run TestStorageIndexRecordsEventsAndProjections -v`

Expected: FAIL with missing `CheckedProgram.Storage` or parse/check diagnostics.

- [ ] **Step 2: Add storage types and check wiring**

Add storage structs in `compiler/sem/storage.go`, add `Storage StorageIndex` to `CheckedProgram`, and call `checkStorageDecls` from `Check`.

Run: `go test ./compiler/sem -run TestStorageIndexRecordsEventsAndProjections -v`

Expected: PASS.

- [ ] **Step 3: Add failing duplicate durable ID tests**

Add `TestStorageRejectsDuplicateEventTypeID` and `TestStorageRejectsDuplicateProjectionID`.

Run: `go test ./compiler/sem -run 'TestStorageRejectsDuplicate(Event|Projection)ID' -v`

Expected: FAIL because duplicate diagnostics are not emitted.

- [ ] **Step 4: Implement duplicate and zero durable ID diagnostics**

Use `strconv.ParseUint`; reject `event_type_id` `0`; reject projection ID `0` with `SEM0106` message `invalid projection id 0`.

Run: `go test ./compiler/sem -run 'TestStorage(Index|Rejects)' -v`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git diff --check
git add compiler/sem/storage.go compiler/sem/types.go compiler/sem/check.go compiler/sem/storage_decl_test.go
git commit -m "feat: index durable storage declarations -Codex Automated"
```

### Task 10: Event Layout Semantics

**Prerequisite:** Task 9.

**Files:**

- Modify: `compiler/sem/storage.go`
- Create: `compiler/sem/storage_event_layout_test.go`
- Add fixtures: `tests/fixtures/negative/duplicate_event_layout_id.wrela`, `tests/fixtures/negative/missing_current_event_layout.wrela`, `tests/fixtures/negative/event_payload_layout_zero.wrela`

**Description:** Validate event layout IDs, current layout rules, upcast endpoints, encode expression types, and inline payload budget.

**Acceptance Criteria:**

- Duplicate layout IDs emit `SEM0102`.
- Layout ID `0` emits `SEM0102` with message `layout id 0 is reserved`.
- Multi-layout event without exactly one `current` emits `SEM0101`.
- Upcast endpoints must exist or emit `SEM0104`.
- Missing upcast target field emits `SEM0105`.
- Payload layout size greater than 448 emits `SEM0121`.
- `payload_layout_id = 0` is reserved and never accepted for semantic events.

**Code Examples:**

```go
func TestEventLayoutCurrentRules(t *testing.T) {
	_, ds := typeDiagsForModules(t, `
module storage.bad
event A id 1 {
    layout 1 {}
    layout 2 {}
}`)
	if !hasCode(ds, diag.SEM0101) {
		t.Fatalf("diagnostics = %#v, want SEM0101", ds)
	}
}
```

- [ ] **Step 1: Add failing layout current test**

Run: `go test ./compiler/sem -run TestEventLayoutCurrentRules -v`

Expected: FAIL because `SEM0101` is not emitted.

- [ ] **Step 2: Implement current layout validation**

Count layouts per event; if one layout and no current marker, mark it current in `EventInfo.CurrentLayoutID`; if multiple, require exactly one current marker.

Run: `go test ./compiler/sem -run TestEventLayoutCurrentRules -v`

Expected: PASS.

- [ ] **Step 3: Add failing duplicate and zero layout tests**

Run: `go test ./compiler/sem -run 'TestEventLayout(Duplicate|Zero)' -v`

Expected: FAIL because duplicate/zero diagnostics are not emitted.

- [ ] **Step 4: Implement layout ID validation**

Reject duplicate and zero layout IDs in `checkEventLayouts`.

Run: `go test ./compiler/sem -run 'TestEventLayout' -v`

Expected: PASS.

- [ ] **Step 5: Add negative fixtures and run global negative suite**

Run: `go test ./compiler -run TestNegativeFixtures -v`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git diff --check
git add compiler/sem/storage.go compiler/sem/storage_event_layout_test.go tests/fixtures/negative/duplicate_event_layout_id.wrela tests/fixtures/negative/missing_current_event_layout.wrela tests/fixtures/negative/event_payload_layout_zero.wrela
git commit -m "feat: validate storage event layouts -Codex Automated"
```

### Task 11: Projection Layout Semantics

**Prerequisite:** Task 9.

**Files:**

- Modify: `compiler/sem/storage.go`
- Create: `compiler/sem/storage_projection_layout_test.go`
- Add fixture: `tests/fixtures/negative/unsupported_projection_container.wrela`

**Description:** Validate projection layout IDs, current marker rules, upcast endpoints, and supported container set.

**Acceptance Criteria:**

- Duplicate projection layout IDs emit `SEM0107`.
- Multiple projection layouts require exactly one current marker.
- The semantic checker instantiates a three-parameter generic data type before `OrderedPages<Partition, SortKey, Row>` is accepted as a projection container.
- Containers are only `StateCell<T>`, `DenseEntityMap<Id, T>`, and `OrderedPages<Partition, SortKey, Row>`.
- Unsupported container emits `SEM0108`.

**Code Examples:**

```go
func TestProjectionRejectsUnsupportedContainer(t *testing.T) {
	_, ds := typeDiagsForModules(t, `
module storage.bad_projection
data HashMap<K, V> { root: U64 }
data FileId { value: U64 }
data Row { value: U64 }
projection Bad id 3 {
    layout 1 current {
        bad: HashMap<FileId, Row>
    }
}`)
	if !hasCode(ds, diag.SEM0108) {
		t.Fatalf("diagnostics = %#v, want SEM0108", ds)
	}
}
```

- [ ] **Step 1: Add three-generic semantic smoke test**

Add `TestProjectionContainerThreeGenericInstantiation`:

```go
func TestProjectionContainerThreeGenericInstantiation(t *testing.T) {
	modules := parseModulesForTest(t, `
module storage.three_generic
data Triple<A, B, C> { a: A; b: B; c: C }
data FileId { value: U64 }
data FileNameKey { value: U64 }
data DirectoryChild { value: U64 }
data Holder { rows: Triple<FileId, FileNameKey, DirectoryChild> }
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	_, ds := checkAllowingMissingImage(t, index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}
	mustCompleteGenericInstantiations(t, index)
	if _, ok := index.Instantiations["storage.three_generic.Triple[storage.three_generic.FileId,storage.three_generic.FileNameKey,storage.three_generic.DirectoryChild]"]; !ok {
		t.Fatalf("three-parameter generic instantiation missing from index")
	}
}
```

Run: `go test ./compiler/sem -run TestProjectionContainerThreeGenericInstantiation -v`

Expected: PASS. If this fails, fix generic instantiation before implementing projection container validation.

- [ ] **Step 2: Add failing projection container test**

Run: `go test ./compiler/sem -run TestProjectionRejectsUnsupportedContainer -v`

Expected: FAIL because `SEM0108` is not emitted.

- [ ] **Step 3: Implement container validation**

Add `projectionContainerKind(*Type) (string, bool)` and call it for every projection layout field.

Run: `go test ./compiler/sem -run TestProjectionRejectsUnsupportedContainer -v`

Expected: PASS.

- [ ] **Step 4: Add current/duplicate tests and fixture**

Run: `go test ./compiler/sem -run TestProjectionLayout -v && go test ./compiler -run TestNegativeFixtures -v`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git diff --check
git add compiler/sem/storage.go compiler/sem/storage_projection_layout_test.go tests/fixtures/negative/unsupported_projection_container.wrela
git commit -m "feat: validate storage projection layouts -Codex Automated"
```

### Task 12: Storage Authority Semantic Hooks

**Prerequisites:** Tasks 9-11.

**Files:**

- Create: `compiler/sem/storage_graph.go`
- Create: `compiler/sem/storage_authority_test.go`
- Modify: `compiler/sem/storage.go`
- Modify: `compiler/sem/image_graph.go`
- Modify: `compiler/sem/check.go`
- Add fixtures: `tests/fixtures/negative/forged_storage_writer.wrela`, `tests/fixtures/negative/ignored_storage_append_result.wrela`

**Description:** Add storage-specific semantic graph nodes and authority checks without spreading logic through general checker files.

**Acceptance Criteria:**

- `StorageWriter` construction outside owned-hardware boot wiring emits `SEM0113`.
- `StorageWriter` construction inside `phase owned_hardware` is allowed only when the constructor directly receives `ForegroundStoragePath`, `BackgroundStoragePath`, `StreamDirectory`, and `StorageMetrics` values created in that phase.
- Ignored `StorageAppendResult` emits `SEM0116`.
- Storage image graph can record NVMe path, core-link endpoint, and projection feed nodes for later tasks.

**Code Examples:**

```go
type StoragePathNode struct {
	Label string
	Role string
	Owner string
	QueueID uint16
	Vector uint8
	Span source.Span
}

type ProjectionFeedNode struct {
	Projection string
	SourceLabel string
	Owner string
	Span source.Span
}
```

Legal construction shape:

```wrela
phase owned_hardware(hardware: OwnedHardware) -> never {
    let foreground = foreground_storage_path(path = foreground_nvme_path)
    let background = background_storage_path(path = background_nvme_path)
    let writer = StorageWriter(
        foreground = foreground,
        background = background,
        stream_directory = StreamDirectory(next_stream_id = 0),
        metrics = StorageMetrics()
    )
    while true {}
}
```

- [ ] **Step 1: Add failing authority tests**

Run: `go test ./compiler/sem -run 'TestStorageWriter(CannotBeForged|ConstructsInsideOwnedHardware)' -v`

Expected: FAIL because `SEM0113` is not emitted.

- [ ] **Step 2: Add legal construction positive test and storage graph hook**

Add `TestStorageWriterConstructsInsideOwnedHardware` with the legal construction shape above. Add storage graph structs, append them to `ImageGraph`, and call `checkStorageAuthority` from `Check`. `checkStorageAuthority` must reject `StorageWriter(...)` unless `currentPhase == "owned_hardware"` and constructor arguments include direct same-phase `foreground`, `background`, `stream_directory`, and `metrics` values.

Run: `go test ./compiler/sem -run 'TestStorageWriter(CannotBeForged|ConstructsInsideOwnedHardware)' -v`

Expected: PASS.

- [ ] **Step 3: Add ignored append test**

Run: `go test ./compiler/sem -run TestStorageAppendResultMustBeObserved -v`

Expected: FAIL because `SEM0116` is not emitted.

- [ ] **Step 4: Implement append-result observation**

Mirror reliable topic result handling: expression statements that call `StorageWriter.enqueue_atomic_group` fail unless the result is matched, assigned, or returned.

Run: `go test ./compiler/sem -run 'TestStorage(Writer|Append)' -v`

Expected: PASS.

- [ ] **Step 5: Add fixtures and commit**

```bash
go test ./compiler -run TestNegativeFixtures -v
git diff --check
git add compiler/sem/storage_graph.go compiler/sem/storage_authority_test.go compiler/sem/storage.go compiler/sem/image_graph.go compiler/sem/check.go tests/fixtures/negative/forged_storage_writer.wrela tests/fixtures/negative/ignored_storage_append_result.wrela
git commit -m "feat: enforce storage writer authority -Codex Automated"
```

### Task 13: IR Storage Metadata

**Prerequisites:** Tasks 9-11.

**Files:**

- Modify: `compiler/ir/ir.go`
- Create: `compiler/ir/storage_testutil_test.go`
- Create: `compiler/ir/storage_metadata_test.go`

**Description:** Add storage event/projection metadata to IR without lowering encoder bodies yet.

**Acceptance Criteria:**

- `ir.Program.StorageEvents` and `ir.Program.StorageProjections` exist.
- `checkedStorageProgramForTest` is defined in `compiler/ir/storage_testutil_test.go`.
- Shape tests can construct an `ir.Program` with event and projection metadata without calling `Lower`.
- Helper tests prove `checkedStorageProgramForTest` returns a semantic program with one current event layout and one current projection layout.

**Code Examples:**

```go
type EventLayout struct {
	Module string
	Name string
	EventTypeID uint64
	LayoutID uint64
	Current bool
	PayloadSize uint64
	PayloadAlign uint64
	EncoderSymbol string
}
```

- [ ] **Step 1: Add failing IR metadata shape test**

Add `TestStorageIRMetadataShape`:

```go
func TestStorageIRMetadataShape(t *testing.T) {
	program := Program{
		StorageEvents: []EventLayout{{
			Module: "app",
			Name: "FileCreated",
			EventTypeID: 1001,
			LayoutID: 1,
			Current: true,
			PayloadSize: 40,
			PayloadAlign: 8,
			EncoderSymbol: "_wrela_storage_event_app_FileCreated_layout_1_encode",
		}},
		StorageProjections: []ProjectionLayout{{
			Module: "app",
			Name: "DirectoryChildren",
			ProjectionID: 12,
			LayoutID: 1,
			Current: true,
		}},
	}
	if got := program.StorageEvents[0].EventTypeID; got != 1001 {
		t.Fatalf("event_type_id = %d", got)
	}
	if got := program.StorageProjections[0].ProjectionID; got != 12 {
		t.Fatalf("projection id = %d", got)
	}
}
```

Run: `go test ./compiler/ir -run TestStorageIRMetadataShape -v`

Expected: FAIL with undefined `StorageEvents`.

- [ ] **Step 2: Add IR structs and test helper**

Implement `EventLayout`, `ProjectionLayout`, and `checkedStorageProgramForTest`.

Run: `go test ./compiler/ir -run TestStorageIRMetadataShape -v`

Expected: PASS.

- [ ] **Step 3: Commit IR shape only**

```bash
git diff --check
git add compiler/ir/ir.go compiler/ir/storage_testutil_test.go compiler/ir/storage_metadata_test.go
git commit -m "feat: add storage metadata ir shape -Codex Automated"
```

### Task 14: Lower Storage Metadata

**Prerequisite:** Task 13.

**Files:**

- Modify: `compiler/ir/lower.go`
- Modify: `compiler/ir/storage_metadata_test.go`

**Description:** Populate IR storage metadata from semantic storage declarations.

**Acceptance Criteria:**

- Event metadata includes event type ID, layout ID, current marker, payload size, and encoder symbol.
- Projection metadata includes projection ID, layout ID, current marker, and container kinds.
- Ordering is deterministic.

**Code Examples:**

```go
func (ctx *lowerContext) lowerStorageMetadata() {
	events := sortedStorageEvents(ctx.checked.Storage)
	for _, event := range events {
		for _, layout := range event.Layouts {
			ctx.program.StorageEvents = append(ctx.program.StorageEvents, EventLayout{
				Module: event.Module,
				Name: event.Name,
				EventTypeID: event.EventTypeID,
				LayoutID: layout.ID,
				Current: layout.Current,
				EncoderSymbol: symbolName("event_encode", event.Module, event.Name, "layout_"+strconv.FormatUint(layout.ID, 10)),
			})
		}
	}
}
```

- [ ] **Step 1: Add failing lowering metadata test**

Add `TestLowerStorageEventMetadata` to `compiler/ir/storage_metadata_test.go`. It must call `checkedStorageProgramForTest`, lower it, and assert the first event layout is `app.FileCreated`, `event_type_id=1001`, `layout_id=1`, `current=true`, and encoder symbol `_wrela_storage_event_app_FileCreated_layout_1_encode`.

Run: `go test ./compiler/ir -run TestLowerStorageEventMetadata -v`

Expected: FAIL because `program.StorageEvents` is empty.

- [ ] **Step 2: Implement lowering**

Call `ctx.lowerStorageMetadata()` in `Lower` after type info initialization and before source methods.

Run: `go test ./compiler/ir -run TestLowerStorageEventMetadata -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add compiler/ir/lower.go compiler/ir/storage_metadata_test.go
git commit -m "feat: lower storage metadata -Codex Automated"
```

### Task 15: Event Encoder IR

**Prerequisite:** Task 14.

**Files:**

- Modify: `compiler/ir/ir.go`
- Modify: `compiler/ir/lower.go`
- Create: `compiler/ir/storage_encoder_test.go`

**Description:** Generate encoder IR for current event layouts. This task creates IR operations only; x64 emission lands in Task 18.

**Acceptance Criteria:**

- `StorageSlotStore` operation exists.
- Encoder functions write all fixed header fields except checksum.
- Payload writes start at offset `64`.
- Encoder emits zero-fill operation for unused payload bytes.

**Code Examples:**

```go
type StorageSlotStore struct {
	Slot Value
	Offset uint64
	Value Value
	Type Type
}

func (*StorageSlotStore) isOperation() {}

type StoragePayloadZero struct {
	Slot Value
	Offset uint64
	Length uint64
}

func (*StoragePayloadZero) isOperation() {}
```

- [ ] **Step 1: Add failing encoder IR test**

Assert generated encoder contains stores at offsets `0, 8, 16, 24, 28, 32, 36, 40, 44, 52, 54, 56`.

Run: `go test ./compiler/ir -run TestStorageEventEncoderIRStoresHeaderFields -v`

Expected: FAIL with missing operation type or empty encoder body.

- [ ] **Step 2: Add IR operations**

Add `StorageSlotStore` and `StoragePayloadZero` to `compiler/ir/ir.go`. Extend the deterministic value traversal helper so it visits `StorageSlotStore.Slot`, `StorageSlotStore.Value`, and `StoragePayloadZero.Slot` in that order.

Run: same command.

Expected: FAIL because lowering still lacks operations.

- [ ] **Step 3: Generate encoder functions**

Implement `lowerEventEncoder` and append functions for current layouts.

Run: `go test ./compiler/ir -run TestStorageEventEncoderIRStoresHeaderFields -v`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git diff --check
git add compiler/ir/ir.go compiler/ir/lower.go compiler/ir/storage_encoder_test.go
git commit -m "feat: generate storage event encoder ir -Codex Automated"
```

### Task 16: Codegen Test Harness For Storage Encoders

**Prerequisite:** Task 15.

**Files:**

- Create: `compiler/codegen/storage_testutil_test.go`
- Create: `compiler/codegen/storage_encoder_test.go`

**Description:** Create codegen helpers only. The production codegen behavior test is added in Task 17 so this task can commit with a green repository.

**Acceptance Criteria:**

- `storageEncoderProgramForCodegenTest` builds an `ir.Program` with one encoder function.
- `findTextUnit` returns a compiled text unit by symbol.
- Harness tests pass without requiring `StorageSlotStore` x64 emission.

**Code Examples:**

```go
func findTextUnit(t *testing.T, image Image, symbol string) Section {
	t.Helper()
	for _, section := range image.Sections {
		if section.Name == ".text."+symbol {
			return section
		}
	}
	t.Fatalf("missing text unit %s", symbol)
	return Section{}
}
```

- [ ] **Step 1: Add failing harness test**

Add `TestStorageEncoderCodegenHarnessBuildsProgram`:

```go
func TestStorageEncoderCodegenHarnessBuildsProgram(t *testing.T) {
	program := storageEncoderProgramForCodegenTest()
	if len(program.Functions) != 1 {
		t.Fatalf("functions = %d, want 1", len(program.Functions))
	}
	if program.Functions[0].Symbol == "" {
		t.Fatalf("encoder symbol must not be empty")
	}
}
```

Run: `go test ./compiler/codegen -run TestStorageEncoderCodegenHarnessBuildsProgram -v`

Expected: FAIL with undefined `storageEncoderProgramForCodegenTest`.

- [ ] **Step 2: Implement helpers**

Implement `storageEncoderProgramForCodegenTest` and `findTextUnit`.

Run: `go test ./compiler/codegen -run TestStorageEncoderCodegenHarnessBuildsProgram -v`

Expected: PASS.

- [ ] **Step 3: Commit green harness**

```bash
git diff --check
git add compiler/codegen/storage_testutil_test.go compiler/codegen/storage_encoder_test.go
git commit -m "test: add storage encoder codegen harness -Codex Automated"
```

### Task 17: Codegen For Event Encoder Header Stores

**Prerequisite:** Task 16.

**Files:**

- Modify: `compiler/codegen/x64.go`
- Modify: `compiler/codegen/storage_encoder_test.go`

**Description:** Emit little-endian stores for `StorageSlotStore` and zero fill for `StoragePayloadZero`.

**Acceptance Criteria:**

- Generated code uses field offsets from IR.
- Test asserts stores are emitted for offsets 24 and 28 by decoding instructions or checking relocation-free generated operation metadata from a helper, not by searching for common byte patterns.
- Payload zero fill emits stores or a bounded loop over bytes 64..511.

**Code Examples:**

```go
case *ir.StorageSlotStore:
	e.emitValueToReg(v.Value, asm.MustLookup("rax"), frame)
	e.emitValueToReg(v.Slot, asm.MustLookup("r11"), frame)
	emitStoreRegAtOffset(e, asm.MustLookup("r11"), int64(v.Offset), asm.MustLookup("rax"), v.Type)
```

- [ ] **Step 1: Add failing codegen test**

Add `TestStorageSlotStoreCodegen` to `compiler/codegen/storage_encoder_test.go`. The test must compile `storageEncoderProgramForCodegenTest`, call `findTextUnit`, and assert the generated metadata contains slot stores at offsets `24` and `28`.

Run: `go test ./compiler/codegen -run TestStorageSlotStoreCodegen -v`

Expected: FAIL with unsupported operation.

- [ ] **Step 2: Implement `StorageSlotStore` emitter**

Add a `compileFunction` operation case and this helper signature in `compiler/codegen/x64.go`:

```go
func emitStoreRegAtOffset(e *Emitter, base asm.Reg, offset int64, value asm.Reg, typ ir.Type) {
	width := e.ctx.storageSizeForType(typ) * 8
	if width == 0 {
		width = valueWidthBitsFromType(typ.Name)
	}
	emitStoreMemFromReg(e, base, offset, value, width)
}
```

Run: `go test ./compiler/codegen -run TestStorageSlotStoreCodegen -v`

Expected: PASS for header stores, FAIL for zero fill if not implemented.

- [ ] **Step 3: Implement `StoragePayloadZero` emitter**

Emit a fixed bounded loop or unrolled stores for the requested length.

Run: `go test ./compiler/codegen -run TestStorageSlotStoreCodegen -v`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git diff --check
git add compiler/codegen/x64.go compiler/codegen/storage_encoder_test.go
git commit -m "feat: emit storage event encoder stores -Codex Automated"
```

### Task 18: Event Encoder CRC Operation

**Prerequisites:** Tasks 2 and 17.

**Files:**

- Modify: `compiler/ir/ir.go`
- Modify: `compiler/ir/lower.go`
- Modify: `compiler/codegen/x64.go`
- Modify: `compiler/ir/storage_encoder_test.go`
- Modify: `compiler/codegen/storage_encoder_test.go`

**Description:** Add checksum calculation as an explicit IR/codegen operation. Use Castagnoli CRC32C and write the little-endian digest at offset 48.

**Acceptance Criteria:**

- Encoder zeroes checksum bytes before computing CRC.
- CRC32C vector matches `0xE3069283`.
- Encoded slot with fixed fields has the same checksum in `compiler/storagefmt` and generated code test harness.

**Code Examples:**

```go
type StorageCRC32C struct {
	Slot Value
	Length uint64
	Type Type
}

func (*StorageCRC32C) isValue() {}
func (*StorageCRC32C) isOperation() {}
```

- [ ] **Step 1: Add failing IR CRC test**

Run: `go test ./compiler/ir -run TestStorageEventEncoderComputesCRC32C -v`

Expected: FAIL because `StorageCRC32C` is undefined.

- [ ] **Step 2: Add IR operation and lowering**

Insert checksum zero stores, `StorageCRC32C`, and checksum field store.

Run: `go test ./compiler/ir -run TestStorageEventEncoderComputesCRC32C -v`

Expected: PASS.

- [ ] **Step 3: Add failing codegen CRC test**

Run: `go test ./compiler/codegen -run TestStorageCRC32CCodegen -v`

Expected: FAIL because codegen does not emit CRC.

- [ ] **Step 4: Implement codegen CRC fallback**

Use a small generated helper symbol `_wrela_crc32c_castagnoli` with table-free bitwise fallback for v1. The helper must use reflected polynomial `0x82F63B78`.

Run:

```bash
go test ./compiler/codegen -run 'TestStorage(CRC32C|SlotStore)' -v
go test ./compiler/storagefmt -run TestCRC32CVector -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git diff --check
git add compiler/ir/ir.go compiler/ir/lower.go compiler/codegen/x64.go compiler/ir/storage_encoder_test.go compiler/codegen/storage_encoder_test.go
git commit -m "feat: emit storage crc32c checksum -Codex Automated"
```

---

## 7. Phase 2: Runtime Format, Event Log, Streams, And Writer

**Description:** This phase mirrors the behavior-tested Go contracts into Wrela source and adds the single-writer append path.

**Acceptance Criteria:**

- Runtime source constants match `compiler/storagefmt`.
- Append-log `event_id` values start at `0`.
- A first two-event group reports `last_event_id = 1`.

### Task 19: Wrela Storage Format Source

**Prerequisites:** Tasks 2 and 14.

**Files:**

- Create: `wrela/storage/format.wrela`
- Create: `compiler/sem/storage_format_test.go`

**Description:** Add Wrela source constants and structs for superblocks, regions, event slots, stream entries, segments, and metrics. Behavior remains covered by `compiler/storagefmt`; this task's compiler test verifies that Wrela code can import and typecheck the runtime format module.

**Acceptance Criteria:**

- Constants match Section 1.
- `StorageMetrics` includes every metric from Section 1 and the design metrics list.
- `StreamDirectoryEntry` is 32 bytes by field shape.
- Mirror-contract test compares exported Wrela constants against `compiler/storagefmt` constants by name and value.

**Code Examples:**

```wrela
module storage.format

const STORAGE_EVENT_SLOT_SIZE: U64 = 512
const STORAGE_EVENT_HEADER_SIZE: U64 = 64
const STORAGE_EVENT_PAYLOAD_BYTES: U64 = 448
const STORAGE_SLOT_RESERVED_EMPTY: U64 = 1

data EventSlotHeader {
    event_id: U64
    stream_id: U64
    stream_sequence: U64
    event_type_id: U32
    payload_layout_id: U32
    atomic_group_len: U32
    atomic_group_index: U32
    payload_length: U32
    flags: U32
    checksum32: U32
    header_version: U16
    reserved16: U16
    reserved64: U64
}
```

- [ ] **Step 1: Add failing semantic source compile test**

Test imports `storage.format` from a tiny Wrela source and uses `sizeof(EventSlotHeader)`.

The test must include a table of host/Wrela constant names:

```go
func assertWrelaConstU64(t *testing.T, index *Index, moduleName, constName string, want uint64) {
	t.Helper()
	got, ok := index.LookupConst(moduleName, constName)
	if !ok {
		t.Fatalf("missing const %s.%s", moduleName, constName)
	}
	if got.Value != want {
		t.Fatalf("%s.%s = %d, want %d", moduleName, constName, got.Value, want)
	}
}

for name, want := range map[string]uint64{
	"STORAGE_EVENT_SLOT_SIZE": storagefmt.EventSlotSize,
	"STORAGE_EVENT_HEADER_SIZE": storagefmt.EventHeaderSize,
	"STORAGE_EVENT_PAYLOAD_BYTES": storagefmt.EventPayloadBytes,
} {
	assertWrelaConstU64(t, index, "storage.format", name, want)
}
```

Run: `go test ./compiler/sem -run TestStorageFormatSourceCompiles -v`

Expected: FAIL because module does not exist.

- [ ] **Step 2: Add `wrela/storage/format.wrela`**

Implement constants and structs.

Run: `go test ./compiler/sem -run TestStorageFormatSourceCompiles -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add wrela/storage/format.wrela compiler/sem/storage_format_test.go
git commit -m "feat: add storage format source -Codex Automated"
```

### Task 20: Event Slot Encoding Algorithms

**Prerequisites:** Tasks 2, 18, and 19.

**Files:**

- Create: `wrela/storage/event_log.wrela`
- Create: `compiler/sem/event_log_source_test.go`
- Modify: `compiler/storagefmt/format_test.go`

**Description:** Add Wrela event slot writer, reserved empty slot constructor, and batch packer. Behavior must match `compiler/storagefmt`.

**Acceptance Criteria:**

- `EventSlotWriter.slots_per_lba()` returns 1 for 512 and 8 for 4096.
- `BatchPacker.finish_batch(3)` on 4096 returns 5 empty slots and 8 total positions.
- Reserved empty slot has valid append-log `event_id`, `event_type_id = 0`, `payload_layout_id = 0`, and reserved flag.
- Mirror contract: the Wrela `BatchPacker.finish_batch` body is exactly the code block below, and host tests use the same exported constants.

**Code Examples:**

```wrela
data BatchPackingResult {
    semantic_slots: U64
    reserved_empty_slots: U64
    total_slot_positions: U64
}

data BatchPacker {
    active_lba_size: U64

    fn finish_batch(self, semantic_slots: U64) -> BatchPackingResult {
        let slots_per_lba = self.active_lba_size / STORAGE_EVENT_SLOT_SIZE
        let remainder = semantic_slots % slots_per_lba
        if remainder == 0 {
            return BatchPackingResult(semantic_slots = semantic_slots, reserved_empty_slots = 0, total_slot_positions = semantic_slots)
        }
        let empty = slots_per_lba - remainder
        return BatchPackingResult(semantic_slots = semantic_slots, reserved_empty_slots = empty, total_slot_positions = semantic_slots + empty)
    }
}
```

- [ ] **Step 1: Add failing compile test**

Run: `go test ./compiler/sem -run TestEventLogSourceCompiles -v`

Expected: FAIL because module does not exist.

- [ ] **Step 2: Add event log source and mirror contract**

Create `wrela/storage/event_log.wrela` with `EventSlotWriter`, `ReservedEmptySlot`, and `BatchPacker`.

Add `TestEventLogBatchPackerMirrorContract` to `compiler/sem/event_log_source_test.go`; it must assert `BatchPacker.finish_batch`, `EventSlotWriter.slots_per_lba`, and `ReservedEmptySlot.header` exist and that `STORAGE_EVENT_SLOT_SIZE` equals `storagefmt.EventSlotSize`.

Run: `go test ./compiler/sem -run 'TestEventLog(SourceCompiles|BatchPackerMirrorContract)' -v`

Expected: PASS.

- [ ] **Step 3: Strengthen host behavior tests**

Add `TestReservedEmptySlotHeader` to `compiler/storagefmt/format_test.go`.

Run: `go test ./compiler/storagefmt -run 'Test(ReservedEmpty|FourKiB)' -v`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git diff --check
git add wrela/storage/event_log.wrela compiler/sem/event_log_source_test.go compiler/storagefmt/format_test.go
git commit -m "feat: add storage event slot algorithms -Codex Automated"
```

### Task 21: Superblock And Region Map Behavior

**Prerequisite:** Task 2.

**Files:**

- Modify: `compiler/storagefmt/format.go`
- Modify: `compiler/storagefmt/format_test.go`
- Modify: `wrela/storage/event_log.wrela`

**Description:** Implement double-buffered superblock choice and region overlap validation in the host harness, then mirror the API in Wrela.

**Acceptance Criteria:**

- Highest valid generation wins.
- Invalid checksum copy is ignored.
- Overlapping region entries return `ErrRegionOverlap`.
- Default region table matches Section 1.
- Mirror contract: Wrela methods `SuperblockPair.choose` and `StorageRegionValidator.validate_pair` use the same names and return fields as host helpers `ChooseSuperblock` and `ValidateRegions`.

**Code Examples:**

```go
func ChooseSuperblock(a, b Superblock) (Superblock, error) {
	av := a.Valid()
	bv := b.Valid()
	if av && bv && b.Generation > a.Generation {
		return b, nil
	}
	if av {
		return a, nil
	}
	if bv {
		return b, nil
	}
	return Superblock{}, ErrNoValidSuperblock
}
```

- [ ] **Step 1: Add failing superblock tests**

Run: `go test ./compiler/storagefmt -run 'TestChooseSuperblock|TestRegionOverlap' -v`

Expected: FAIL because helpers are undefined.

- [ ] **Step 2: Implement host behavior**

Add `Superblock`, `ChooseSuperblock`, `ValidateRegions`, and errors.

Run: same command.

Expected: PASS.

- [ ] **Step 3: Mirror Wrela API**

Add `SuperblockPair.choose()` and `StorageRegionValidator.validate_pair()` to `wrela/storage/event_log.wrela`.

Add `TestEventLogSuperblockMirrorContract`; it must assert methods `SuperblockPair.choose`, `StorageRegionValidator.validate_pair`, and result fields `selected_generation` and `valid` exist.

Run: `go test ./compiler/sem -run 'TestEventLog(SourceCompiles|SuperblockMirrorContract)' -v`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git diff --check
git add compiler/storagefmt/format.go compiler/storagefmt/format_test.go wrela/storage/event_log.wrela
git commit -m "feat: add storage superblock region behavior -Codex Automated"
```

### Task 22: Recovery Scanner Behavior

**Prerequisite:** Task 20.

**Files:**

- Modify: `compiler/storagefmt/format.go`
- Modify: `compiler/storagefmt/format_test.go`
- Modify: `wrela/storage/event_log.wrela`

**Description:** Add recovery validation for complete atomic groups and invalid tails.

**Acceptance Criteria:**

- Recovery stops before checksum mismatch.
- Recovery stops before incomplete atomic group.
- Empty event type without reserved-empty flag is invalid.
- Reserved empty slots are skipped and not delivered.
- Mirror contract: Wrela `RecoveryResult` and stop-reason constants match the host `RecoveryResult` fields and stop-reason names one-for-one.

**Code Examples:**

```go
type RecoveryStopReason uint8

const (
	StopCleanEOF RecoveryStopReason = iota
	StopChecksumMismatch
	StopIncompleteAtomicGroup
	StopInvalidEmptySlot
)

type Slot struct {
	Header EventSlotHeader
	Payload [EventPayloadBytes]byte
}

type RecoveryResult struct {
	VisibleEvents uint64
	NextEventID uint64
	LastCommittedGroupEnd uint64
	StopReason RecoveryStopReason
}

func RecoverSlots(slots []Slot) RecoveryResult {
	// Implementation walks expected event_id in order and validates each group.
}

func TestRecoveryRejectsEmptySlotOutsidePadding(t *testing.T) {
	slot := ValidSlotForTest(7)
	slot.Header.EventTypeID = 0
	slot.Header.Flags = 0
	got := RecoverSlots([]Slot{slot})
	if got.StopReason != StopInvalidEmptySlot || got.VisibleEvents != 0 {
		t.Fatalf("recovery = %#v", got)
	}
}
```

- [ ] **Step 1: Add failing recovery tests**

Run: `go test ./compiler/storagefmt -run TestRecovery -v`

Expected: FAIL because recovery symbols are undefined.

- [ ] **Step 2: Implement host recovery**

Add exactly the `RecoveryStopReason`, `Slot`, `RecoveryResult`, and `RecoverSlots(slots []Slot) RecoveryResult` API shown above. `RecoverSlots` must increment `NextEventID` across reserved empty slots, must not increment `VisibleEvents` for reserved empty slots, and must stop before returning an incomplete atomic group.

Run: `go test ./compiler/storagefmt -run TestRecovery -v`

Expected: PASS.

- [ ] **Step 3: Mirror Wrela recovery structs**

Add `RecoveryResult`, stop reason constants, and `EventRecoveryScanner.validate_group_member`. Also add `TestEventLogRecoveryMirrorContract` to `compiler/sem/event_log_source_test.go`; it must assert the Wrela source exports `RecoveryResult.visible_events`, `RecoveryResult.next_event_id`, `RecoveryResult.last_committed_group_end`, `RecoveryResult.stop_reason`, and constants `STORAGE_RECOVERY_STOP_CLEAN_EOF`, `STORAGE_RECOVERY_STOP_CHECKSUM_MISMATCH`, `STORAGE_RECOVERY_STOP_INCOMPLETE_ATOMIC_GROUP`, and `STORAGE_RECOVERY_STOP_INVALID_EMPTY_SLOT`.

Run: `go test ./compiler/sem -run 'TestEventLog(SourceCompiles|RecoveryMirrorContract)' -v`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git diff --check
git add compiler/storagefmt/format.go compiler/storagefmt/format_test.go wrela/storage/event_log.wrela
git commit -m "feat: add event recovery scanner -Codex Automated"
```

### Task 23: Segment Lifecycle And Packed Codec

**Prerequisite:** Task 22.

**Files:**

- Modify: `compiler/storagefmt/format.go`
- Modify: `compiler/storagefmt/format_test.go`
- Modify: `wrela/storage/event_log.wrela`

**Description:** Add sealed segment metadata, segment-map lookup, ZNS metadata, and packed/no-op codec behavior.

**Acceptance Criteria:**

- Segment states are open hot, sealed hot, compressible, and compressed.
- ZNS fields `zone_start_lba` and `zone_block_count` exist.
- Packed codec removes zero padding and indexes every configured stride.
- Segment map lookup preserves stable append-log `event_id` values.
- Mirror contract: Wrela `EventSegment`, `SegmentIndexEntry`, and `PackedSegmentCodec` expose the same fields as host `EventSegment`, `SegmentIndexEntry`, and `PackedSegment`.

**Code Examples:**

```go
func TestPackedSegmentCodecStripsPadding(t *testing.T) {
	slot := ValidSlotForTest(0)
	slot.Header.PayloadLength = 12
	packed := PackSlots([]Slot{slot}, 16)
	if got, want := len(packed.Bytes), EventHeaderSize+12; got != want {
		t.Fatalf("packed bytes = %d, want %d", got, want)
	}
	if len(packed.Index) != 1 || packed.Index[0].EventIDDelta != 0 {
		t.Fatalf("packed index = %#v", packed.Index)
	}
}
```

- [ ] **Step 1: Add failing packed codec test**

Run: `go test ./compiler/storagefmt -run TestPackedSegmentCodecStripsPadding -v`

Expected: FAIL because `PackSlots` is undefined.

- [ ] **Step 2: Implement host packed codec**

Add `EventSegment`, `SegmentIndexEntry`, `PackedSegment`, and `PackSlots`.

Run: same command.

Expected: PASS.

- [ ] **Step 3: Mirror Wrela segment structs**

Add `EventSegment`, `SegmentIndexEntry`, `EventSegmentMap`, `PackedSegmentCodec` to `wrela/storage/event_log.wrela`.

Add `TestEventLogSegmentMirrorContract`; it must assert `EventSegment.state`, `EventSegment.zone_start_lba`, `EventSegment.zone_block_count`, `SegmentIndexEntry.event_id_delta`, and `PackedSegmentCodec.pack_slots` exist.

Run: `go test ./compiler/sem -run 'TestEventLog(SourceCompiles|SegmentMirrorContract)' -v`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git diff --check
git add compiler/storagefmt/format.go compiler/storagefmt/format_test.go wrela/storage/event_log.wrela
git commit -m "feat: add storage segment lifecycle -Codex Automated"
```

### Task 24: Stream Directory And Checkpoints

**Prerequisites:** Tasks 2 and 19.

**Files:**

- Create: `wrela/storage/stream.wrela`
- Modify: `compiler/storagefmt/format.go`
- Modify: `compiler/storagefmt/format_test.go`
- Create: `compiler/sem/stream_source_test.go`

**Description:** Add dense stream directory math, expected sequence validation, non-reused stream IDs, simple stream checkpoints, and a direct chunk cache metric.

**Acceptance Criteria:**

- `entry_byte_offset(stream_id) = stream_id * 32`.
- `exists(stream_id) = stream_id < next_stream_id`.
- Stream IDs are never reused.
- Checkpoint with stale `state_layout_id` is ignored.
- Cache hit rate metric is backed by `StreamDirectoryCache`.
- Mirror contract: `stream.wrela` exports `StreamDirectory`, `StreamDirectoryEntry`, `StreamCheckpoint`, and `StreamDirectoryCache` with names matching host helpers.

**Code Examples:**

```go
func TestStreamDirectoryMath(t *testing.T) {
	dir := StreamDirectory{NextStreamID: 8}
	if !dir.Exists(7) || dir.Exists(8) {
		t.Fatalf("stream existence broken")
	}
	if got := dir.EntryOffset(5); got != 160 {
		t.Fatalf("entry offset = %d, want 160", got)
	}
}
```

- [ ] **Step 1: Add failing host stream tests**

Run: `go test ./compiler/storagefmt -run TestStreamDirectory -v`

Expected: FAIL because stream helpers are undefined.

- [ ] **Step 2: Implement host stream helpers**

Add `StreamDirectory`, `StreamCheckpoint`, and `StreamDirectoryCache`.

Run: same command.

Expected: PASS.

- [ ] **Step 3: Add Wrela stream source and mirror test**

Create `wrela/storage/stream.wrela`; add compile test importing it. Add `TestStreamSourceMirrorContract` to `compiler/sem/stream_source_test.go`:

```go
func TestStreamSourceMirrorContract(t *testing.T) {
	index := checkedStreamSourceIndex(t)
	assertMethodExists(t, moduleType(t, index, "storage.stream", "StreamDirectory"), "entry_byte_offset")
	assertMethodExists(t, moduleType(t, index, "storage.stream", "StreamDirectory"), "exists")
	assertMethodExists(t, moduleType(t, index, "storage.stream", "StreamDirectoryCache"), "record_hit")
	assertMethodExists(t, moduleType(t, index, "storage.stream", "StreamDirectoryCache"), "hit_rate_x1000")
}
```

Run: `go test ./compiler/sem -run 'TestStreamSource(Compiles|MirrorContract)' -v`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git diff --check
git add wrela/storage/stream.wrela compiler/storagefmt/format.go compiler/storagefmt/format_test.go compiler/sem/stream_source_test.go
git commit -m "feat: add dense stream directory -Codex Automated"
```

### Task 25: Storage Writer Batch Policy

**Prerequisites:** Tasks 12, 20, and 24.

**Files:**

- Create: `wrela/storage/writer.wrela`
- Modify: `compiler/storagefmt/format.go`
- Modify: `compiler/storagefmt/format_test.go`
- Create: `compiler/sem/storage_writer_source_test.go`

**Description:** Add single-writer source shape and behavior-tested batch policy.

**Acceptance Criteria:**

- `StorageWriter` owns `next_event_id`, `next_stream_id`, durable frontier, open batch slots, metrics, and committed group producer.
- `EnqueueAtomicGroup` rejects groups larger than 32.
- A two-event group at 63 open slots produces 65-slot overflow and requests flush.
- First two-event atomic group gets append-log `event_id` values 0 and 1.
- Mirror contract: Wrela `StorageWriter.enqueue_atomic_group` field updates match host `WriterPolicy.EnqueueAtomicGroup` for accept/reject, open-slot count, flush request, and assigned `event_id` range.

**Code Examples:**

```go
type WriterPolicy struct {
	NextEventID uint64
	NextStreamID uint64
	OpenBatchSlots uint64
	DurableFrontier uint64
}

type EnqueueResult struct {
	Accepted bool
	FirstEventID uint64
	LastEventID uint64
	OpenBatchSlots uint64
	FlushRequested bool
	RejectCode string
}

func (w WriterPolicy) EnqueueAtomicGroup(semanticSlots uint64) EnqueueResult {
	if semanticSlots > StorageMaxAtomicGroupSlots {
		return EnqueueResult{Accepted: false, RejectCode: "SEM0114"}
	}
	first := w.NextEventID
	last := first + semanticSlots - 1
	open := w.OpenBatchSlots + semanticSlots
	return EnqueueResult{
		Accepted: true,
		FirstEventID: first,
		LastEventID: last,
		OpenBatchSlots: open,
		FlushRequested: open >= StorageTargetBatchSlots,
	}
}

func TestStorageWriterBatchOverflowDoesNotSplitGroup(t *testing.T) {
	writer := WriterPolicy{OpenBatchSlots: 63}
	got := writer.EnqueueAtomicGroup(2)
	if !got.Accepted || got.OpenBatchSlots != 65 || !got.FlushRequested {
		t.Fatalf("enqueue = %#v", got)
	}
}
```

- [ ] **Step 1: Add failing host writer policy tests**

Run: `go test ./compiler/storagefmt -run TestStorageWriter -v`

Expected: FAIL because `WriterPolicy` is undefined.

- [ ] **Step 2: Implement host writer policy**

Add exactly `WriterPolicy`, `EnqueueResult`, and `(WriterPolicy).EnqueueAtomicGroup(semanticSlots uint64) EnqueueResult` as shown above. Add `AssignEventIDs(first uint64, count uint64) (firstEventID uint64, lastEventID uint64)` and make it return `(first, first+count-1)`; callers must reject `count == 0` before calling it.

Run: same command.

Expected: PASS.

- [ ] **Step 3: Add Wrela writer source and mirror test**

Create `wrela/storage/writer.wrela` with `StorageWriter`, `PendingAtomicGroup`, `CommittedAtomicGroup`, `CommitToken`, and `StorageAppendResult`.

Add `TestStorageWriterSourceMirrorContract` to assert `StorageWriter.enqueue_atomic_group`, `StorageWriter.on_durability_completed`, `StorageWriter.publish_committed_group`, and `StorageWriter.publish_blob_ref` exist with those exact names.

Run: `go test ./compiler/sem -run 'TestStorageWriterSource(Compiles|MirrorContract)' -v`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git diff --check
git add wrela/storage/writer.wrela compiler/storagefmt/format.go compiler/storagefmt/format_test.go compiler/sem/storage_writer_source_test.go
git commit -m "feat: add storage writer batch policy -Codex Automated"
```

### Task 26: Durability Completion State Machine

**Prerequisites:** Tasks 3 and 25.

**Files:**

- Modify: `compiler/nvmefmt/nvme.go`
- Modify: `compiler/nvmefmt/nvme_test.go`
- Modify: `wrela/storage/writer.wrela`

**Description:** Acknowledge appended groups only after the selected durability condition is met.

**Acceptance Criteria:**

- FUA mode acknowledges after all writes complete.
- Write-plus-flush mode acknowledges after writes and flush complete.
- Failed write or flush rejects the batch.
- Writer publishes `CommittedAtomicGroup` only after acknowledgement.
- Mirror contract: Wrela `StorageWriter.on_durability_completed` and `publish_committed_group` use the same acknowledgement states as host `DurabilityState`.

**Code Examples:**

```go
func TestWritePlusFlushAcknowledgesAfterFlushOnly(t *testing.T) {
	sm := DurabilityState{Mode: DurabilityWritePlusFlush, PendingWrites: 2}
	sm.CompleteWrite(1, true)
	sm.CompleteWrite(2, true)
	if sm.Acknowledged() {
		t.Fatal("must not ack before flush")
	}
	sm.CompleteFlush(true)
	if !sm.Acknowledged() {
		t.Fatal("must ack after flush")
	}
}
```

- [ ] **Step 1: Add failing durability state tests**

Run: `go test ./compiler/nvmefmt -run TestWritePlusFlushAcknowledgesAfterFlushOnly -v`

Expected: FAIL because `DurabilityState` is undefined.

- [ ] **Step 2: Implement durability state**

Add `DurabilityState`, modes, write completion, and flush completion.

Run: `go test ./compiler/nvmefmt -run TestWritePlusFlushAcknowledgesAfterFlushOnly -v`

Expected: PASS.

- [ ] **Step 3: Mirror writer source hook**

Add `StorageWriter.on_durability_completed` and `StorageWriter.publish_committed_group`.

Add `TestStorageWriterDurabilityMirrorContract`; it must assert methods `on_durability_completed`, `publish_committed_group`, and fields `pending_write_count`, `flush_required`, and `durability_failed` exist.

Run: `go test ./compiler/sem -run 'TestStorageWriter(SourceCompiles|DurabilityMirrorContract)' -v`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git diff --check
git add compiler/nvmefmt/nvme.go compiler/nvmefmt/nvme_test.go wrela/storage/writer.wrela
git commit -m "feat: gate storage ack on nvme durability -Codex Automated"
```

---

## 8. Phase 3: CoreLink And NVMe Runtime

**Description:** This phase adds foreground/maintenance links and the interrupt-driven NVMe driver in small slices.

**Acceptance Criteria:**

- Foreground and maintenance use explicit SPSC core links.
- NVMe completions use interrupt receivers.
- Path ownership is semantically checked.

### Task 27: CoreLink Source

**Prerequisite:** Task 4.

**Files:**

- Create: `wrela/machine/x86_64/core_link.wrela`
- Create: `compiler/sem/core_link_source_test.go`
- Modify: `compiler/sem/uefi_source_shape_test.go`

**Description:** Add typed paired-core SPSC endpoints with owner/peer slots and wake metadata.

**Acceptance Criteria:**

- `CoreSpscProducer<T>` has owner, peer, slots, capacity, head, tail, credits, and wake strategy.
- `CoreSpscConsumer<T>` has owner, peer, slots, capacity, head, tail, wait-armed flag, and wake strategy.
- Source compiles with existing generics.
- `parseUEFIModuleSet` includes `wrela/machine/x86_64/core_link.wrela` before any source-shape test looks it up.

**Code Examples:**

```wrela
class CoreSpscProducer<T> {
    owner: ExecutorSlot
    peer: ExecutorSlot
    slots: Slots<T>
    capacity: U64
    head: U64
    tail: U64
    credits: U64
    wake_strategy: WakeStrategy
    asm fn try_send(self, value: T) -> Result<Unit, CoreLinkFull> { ret }
}
```

- [ ] **Step 1: Add failing source compile test**

Run: `go test ./compiler/sem -run TestCoreLinkSourceCompiles -v`

Expected: FAIL because module does not exist.

- [ ] **Step 2: Add source and register module**

Create `wrela/machine/x86_64/core_link.wrela` and add it to the hard-coded module list in `parseUEFIModuleSet`.

Run: same command.

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add wrela/machine/x86_64/core_link.wrela compiler/sem/core_link_source_test.go compiler/sem/uefi_source_shape_test.go
git commit -m "feat: add paired core link source -Codex Automated"
```

### Task 28: CoreLink Ownership Semantics

**Prerequisites:** Tasks 12 and 27.

**Files:**

- Modify: `compiler/sem/storage_graph.go`
- Create: `compiler/sem/core_link_ownership_test.go`
- Add fixture: `tests/fixtures/negative/core_link_wrong_owner.wrela`

**Description:** Enforce SPSC endpoint ownership.

**Acceptance Criteria:**

- Producer used by a non-owner executor emits `SEM0112`.
- Consumer used by a non-owner executor emits `SEM0112`.
- Graph records endpoint label, direction, role, owner, peer, and depth.

**Code Examples:**

```go
func TestCoreLinkWrongOwnerFails(t *testing.T) {
	_, ds := typeDiagsForModules(t, coreLinkWrongOwnerSource)
	if !hasCode(ds, diag.SEM0112) {
		t.Fatalf("diagnostics = %#v, want SEM0112", ds)
	}
}
```

- [ ] **Step 1: Add failing ownership test**

Run: `go test ./compiler/sem -run TestCoreLinkWrongOwnerFails -v`

Expected: FAIL because `SEM0112` is not emitted.

- [ ] **Step 2: Implement ownership check**

Add graph extraction for `CoreSpscProducer`/`CoreSpscConsumer` construction and compare `owner` to current executor slot.

Run: same command.

Expected: PASS.

- [ ] **Step 3: Add fixture and commit**

```bash
go test ./compiler -run TestNegativeFixtures -v
git diff --check
git add compiler/sem/storage_graph.go compiler/sem/core_link_ownership_test.go tests/fixtures/negative/core_link_wrong_owner.wrela
git commit -m "feat: enforce core link ownership -Codex Automated"
```

### Task 29: NVMe Source Surface

**Prerequisite:** Task 3.

**Files:**

- Create: `wrela/machine/x86_64/nvme.wrela`
- Create: `compiler/sem/nvme_source_test.go`
- Modify: `compiler/sem/uefi_source_shape_test.go`

**Description:** Add NVMe source types, controller registers, driver, driver path, namespace facts, and completion interrupt shape.

**Acceptance Criteria:**

- `NvmeDriver.initialize(device: PciDevice) -> NvmeDriver` exists.
- `NvmeDriver.claim_controller(devices: PciDeviceSet, occurrence: U64) -> PciDevice` exists and selects by base class `0x01`, subclass `0x08`, and prog-if `0x02`.
- `NvmeIoPath` is a `driver path`.
- `NvmeIoPath` has `interrupt receiver -> NvmeCompletionInterrupt`.
- `NvmeNamespace` carries LBA size, ZNS support, FUA support, AWUN, AWUPF, and volatile write cache.
- `parseUEFIModuleSet` includes `wrela/machine/x86_64/nvme.wrela` before any source-shape test looks up `NvmeDriver`.

**Code Examples:**

```wrela
const NVME_CLASS_MASS_STORAGE: U64 = 0x01
const NVME_SUBCLASS_NVM: U64 = 0x08
const NVME_PROGIF_EXPRESS: U64 = 0x02

data NvmeNamespace {
    namespace_id: U32
    logical_block_size: U64
    block_count: U64
    zone_size_blocks: U64
    supports_zns: Bool
    supports_fua: Bool
    atomic_write_unit_blocks: U32
    power_fail_atomic_write_unit_blocks: U32
    volatile_write_cache: Bool
}
```

- [ ] **Step 1: Add failing source compile test**

Run: `go test ./compiler/sem -run TestNvmeSourceCompiles -v`

Expected: FAIL because module does not exist.

- [ ] **Step 2: Add source types, method signatures, and module registration**

Create `wrela/machine/x86_64/nvme.wrela` with concrete stub bodies that boot-panic for operations implemented by Tasks 30-33:

```wrela
fn submit_flush(self, namespace_id: U32) -> NvmeSubmission {
    self.registers.panic.fail(code = 0xAC080030)
}
```

Add `wrela/machine/x86_64/nvme.wrela` to the hard-coded module list in `parseUEFIModuleSet`.

Run: same command.

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add wrela/machine/x86_64/nvme.wrela compiler/sem/nvme_source_test.go compiler/sem/uefi_source_shape_test.go
git commit -m "feat: add nvme source surface -Codex Automated"
```

### Task 30: NVMe Identify And Durability Source

**Prerequisites:** Tasks 3 and 29.

**Files:**

- Modify: `wrela/machine/x86_64/nvme.wrela`
- Modify: `compiler/nvmefmt/nvme_test.go`

**Description:** Mirror Identify parsing and durability selection contracts in Wrela source.

**Acceptance Criteria:**

- Wrela constants exist for 512, 4096, conventional, ZNS, FUA, PFAIL atomic FUA, and write-plus-flush.
- `NvmeDurabilitySelector.choose` matches `compiler/nvmefmt.SelectDurability`.
- Unsupported LBA calls boot panic code `0xAC080122`.
- Mirror contract: the Wrela selector body is exactly the code block below except for local variable names.

**Code Examples:**

```wrela
fn choose(self, namespace: NvmeNamespace, target_batch_blocks: U32) -> NvmeDurabilityMode {
    if namespace.logical_block_size != 512 {
        if namespace.logical_block_size != 4096 {
            self.panic.fail(code = 0xAC080122)
        }
    }
    if namespace.supports_fua {
        return NvmeDurabilityMode(mode = NVME_DURABILITY_FUA, requires_flush = false, use_fua = true)
    }
    return NvmeDurabilityMode(mode = NVME_DURABILITY_WRITE_PLUS_FLUSH, requires_flush = true, use_fua = false)
}
```

- [ ] **Step 1: Add failing behavior table in `compiler/nvmefmt`**

Run: `go test ./compiler/nvmefmt -run TestSelectDurabilityModes -v`

Expected: FAIL if selector is not implemented.

- [ ] **Step 2: Implement or fix host selector**

Run: same command.

Expected: PASS.

- [ ] **Step 3: Add Wrela selector**

Modify `wrela/machine/x86_64/nvme.wrela`.

Add `TestNvmeDurabilityMirrorContract`; it must assert constants `NVME_DURABILITY_FUA`, `NVME_DURABILITY_WRITE_PLUS_FLUSH`, method `NvmeDurabilitySelector.choose`, and fields `requires_flush` and `use_fua` exist.

Run: `go test ./compiler/sem -run 'TestNvme(SourceCompiles|DurabilityMirrorContract)' -v`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git diff --check
git add wrela/machine/x86_64/nvme.wrela compiler/nvmefmt/nvme_test.go
git commit -m "feat: select nvme durability mode -Codex Automated"
```

### Task 31: NVMe Controller Initialization Sequence

**Prerequisite:** Task 30.

**Files:**

- Modify: `wrela/machine/x86_64/nvme.wrela`
- Create: `compiler/nvmefmt/controller_test.go`

**Description:** Spell out and test the exact controller initialization order from Section 1.

**Acceptance Criteria:**

- Initialization disables CC.EN and waits for CSTS.RDY=0.
- Programs AQA, ASQ, ACQ before enabling CC.EN.
- Enables CC.EN and waits for CSTS.RDY=1.
- Submits Identify Controller and Identify Namespace through admin queue.
- Ready waits are bounded by timeout counters.
- Mirror contract: Wrela `NvmeDriver.initialize` performs the exact operation order returned by host `PlannedControllerInitOps`.

**Code Examples:**

```go
func TestControllerInitSequence(t *testing.T) {
	got := PlannedControllerInitOps()
	want := []string{"read CAP", "write CC.EN=0", "wait RDY=0", "write AQA", "write ASQ", "write ACQ", "write CC.EN=1", "wait RDY=1", "identify controller", "identify namespace"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("init sequence = %#v, want %#v", got, want)
	}
}
```

- [ ] **Step 1: Add failing sequence test**

Run: `go test ./compiler/nvmefmt -run TestControllerInitSequence -v`

Expected: FAIL because `PlannedControllerInitOps` is undefined.

- [ ] **Step 2: Implement host sequence**

Add `PlannedControllerInitOps` in `compiler/nvmefmt`.

Run: same command.

Expected: PASS.

- [ ] **Step 3: Add Wrela initialization code**

Implement `NvmeDriver.initialize` in source using the same order.

Add `TestNvmeInitMirrorContract`; it must assert `NvmeDriver.initialize` calls through operations named `disable_controller`, `program_admin_queues`, `enable_controller`, `identify_controller`, and `identify_namespace`.

Run: `go test ./compiler/sem -run 'TestNvme(SourceCompiles|InitMirrorContract)' -v`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git diff --check
git add wrela/machine/x86_64/nvme.wrela compiler/nvmefmt/controller_test.go compiler/nvmefmt/nvme.go
git commit -m "feat: define nvme controller initialization -Codex Automated"
```

### Task 32: NVMe Queue Commands

**Prerequisite:** Task 31.

**Files:**

- Modify: `compiler/nvmefmt/nvme.go`
- Modify: `compiler/nvmefmt/nvme_test.go`
- Modify: `wrela/machine/x86_64/nvme.wrela`

**Description:** Add command builders for read, write, flush, and zone append.

**Acceptance Criteria:**

- Write opcode is `0x01`, read opcode is `0x02`, flush opcode is `0x00`, zone append opcode is `0x7D`.
- Write FUA sets command dword 12 bit 30.
- Commands include namespace ID, start LBA, block count minus one, and PRP1.
- Transfer length greater than `131072` is rejected before command construction.
- Mirror contract: Wrela `submit_read`, `submit_write`, `submit_flush`, and `submit_zone_append` use the same opcode and CDW field layout as host command builders.

**Code Examples:**

```go
func TestWriteCommandSetsFUA(t *testing.T) {
	cmd := BuildWriteCommand(1, 99, 8, 0x200000, true)
	if cmd.Opcode != 0x01 || cmd.CDW12&(1<<30) == 0 {
		t.Fatalf("write command = %#v", cmd)
	}
}
```

- [ ] **Step 1: Add failing command builder tests**

Run: `go test ./compiler/nvmefmt -run TestWriteCommandSetsFUA -v`

Expected: FAIL because `BuildWriteCommand` is undefined or incomplete.

- [ ] **Step 2: Implement command builders**

Add command structs and builders in `compiler/nvmefmt`.

Run: `go test ./compiler/nvmefmt -run 'Test.*Command' -v`

Expected: PASS.

- [ ] **Step 3: Mirror Wrela submit methods**

Implement `submit_read`, `submit_write`, `submit_flush`, and `submit_zone_append` in `NvmeIoPath`.

Add `TestNvmeCommandMirrorContract`; it must assert the four submit methods exist and exported constants `NVME_OPCODE_WRITE`, `NVME_OPCODE_READ`, `NVME_OPCODE_FLUSH`, `NVME_OPCODE_ZONE_APPEND`, and `NVME_COMMAND_FUA_BIT` match host constants.

Run: `go test ./compiler/sem -run 'TestNvme(SourceCompiles|CommandMirrorContract)' -v`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git diff --check
git add compiler/nvmefmt/nvme.go compiler/nvmefmt/nvme_test.go wrela/machine/x86_64/nvme.wrela
git commit -m "feat: build nvme queue commands -Codex Automated"
```

### Task 33: NVMe Completion Phase And Interrupt Receiver

**Prerequisite:** Task 32.

**Files:**

- Modify: `compiler/nvmefmt/nvme.go`
- Modify: `compiler/nvmefmt/nvme_test.go`
- Modify: `wrela/machine/x86_64/nvme.wrela`

**Description:** Implement completion queue draining by phase tag and publish one typed completion interrupt per drain.

**Acceptance Criteria:**

- Drain stops at phase mismatch.
- Completion head wraps at queue depth and toggles expected phase.
- Completion doorbell is rung after draining at least one entry.
- Interrupt receiver returns `NvmeCompletionInterrupt(queue_id, completed_count)`.
- Mirror contract: Wrela completion draining uses the same head/phase transition table as host `CompletionQueue.Advance`.

**Code Examples:**

```go
func TestCompletionHeadWrapTogglesPhase(t *testing.T) {
	q := CompletionQueue{Depth: 2, Head: 1, Phase: true}
	q.Advance(1)
	if q.Head != 0 || q.Phase != false {
		t.Fatalf("queue = %#v, want head 0 phase false", q)
	}
}
```

- [ ] **Step 1: Add failing completion tests**

Run: `go test ./compiler/nvmefmt -run TestCompletion -v`

Expected: FAIL because completion helpers are missing.

- [ ] **Step 2: Implement completion helpers**

Add `CompletionQueue`, `Advance`, and `DrainCompletions`.

Run: same command.

Expected: PASS.

- [ ] **Step 3: Mirror Wrela interrupt receiver**

Implement `drain_completion_queue` and `interrupt receiver -> NvmeCompletionInterrupt`.

Add `TestNvmeCompletionMirrorContract`; it must assert `NvmeCompletionQueue.advance`, `NvmeCompletionQueue.drain`, `NvmeIoPath.drain_completion_queue`, and the interrupt receiver return type `NvmeCompletionInterrupt` exist.

Run: `go test ./compiler/sem -run 'TestNvme(SourceCompiles|CompletionMirrorContract)' -v`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git diff --check
git add compiler/nvmefmt/nvme.go compiler/nvmefmt/nvme_test.go wrela/machine/x86_64/nvme.wrela
git commit -m "feat: drain nvme completions by interrupt -Codex Automated"
```

### Task 34: Foreground And Background Path Ownership

**Prerequisites:** Tasks 12, 29, and 33.

**Files:**

- Modify: `wrela/machine/x86_64/nvme.wrela`
- Modify: `compiler/sem/storage_graph.go`
- Create: `compiler/sem/storage_path_test.go`
- Add fixture: `tests/fixtures/negative/storage_wrong_nvme_path_owner.wrela`

**Description:** Add foreground/background path wrappers and enforce executor ownership.

**Acceptance Criteria:**

- Foreground writer cannot be constructed with background path.
- Maintenance worker cannot submit through foreground path.
- Wrong owner emits `SEM0111`.
- Graph records path label, role, owner, queue ID, vector.

**Code Examples:**

```wrela
data ForegroundStoragePath { path: NvmeIoPath }
data BackgroundStoragePath { path: NvmeIoPath }

fn foreground_storage_path(self, path: NvmeIoPath) -> ForegroundStoragePath {
    if path.role.role != NVME_PATH_FOREGROUND {
        path.registers.panic.fail(code = 0xAC080111)
    }
    return ForegroundStoragePath(path = path)
}
```

- [ ] **Step 1: Add failing path owner test**

Run: `go test ./compiler/sem -run TestStoragePathWrongOwnerFails -v`

Expected: FAIL because `SEM0111` is not emitted.

- [ ] **Step 2: Add wrappers and semantic checks**

Modify `nvme.wrela` and `storage_graph.go`.

Run: `go test ./compiler/sem -run TestStoragePathWrongOwnerFails -v`

Expected: PASS.

- [ ] **Step 3: Add fixture and commit**

```bash
go test ./compiler -run TestNegativeFixtures -v
git diff --check
git add wrela/machine/x86_64/nvme.wrela compiler/sem/storage_graph.go compiler/sem/storage_path_test.go tests/fixtures/negative/storage_wrong_nvme_path_owner.wrela
git commit -m "feat: enforce storage nvme path ownership -Codex Automated"
```

---

## 9. Phase 4: Blob Storage And Maintenance

**Description:** This phase adds blob data-plane structures, allocator behavior, write-order guarantees, orphan collection, and copy-on-write relocation proposals.

**Acceptance Criteria:**

- Blob allocator tests exercise 1024-entry behavior.
- Events can reference only published blob refs.
- Maintenance proposals never mutate truth directly.

### Task 35: Blob Ref And 1024-Entry Allocator

**Prerequisites:** Tasks 2 and 19.

**Files:**

- Create: `wrela/storage/blob.wrela`
- Modify: `compiler/storagefmt/format.go`
- Modify: `compiler/storagefmt/format_test.go`
- Create: `compiler/sem/blob_source_test.go`

**Description:** Add blob refs, extents, manifests, and first-fit free extent allocator.

**Acceptance Criteria:**

- `BlobRef` fields match the design.
- Free list supports 1024 entries in host behavior.
- Allocation splits larger extents.
- Free coalesces adjacent extents.
- Mirror contract: Wrela `BlobExtentAllocator` exposes the same allocator operations as host `FreeExtentList`, including the 1024-entry capacity.

**Code Examples:**

```go
func TestBlobAllocatorSplitsAndCoalesces(t *testing.T) {
	a := NewFreeExtentList(1024)
	a.Free(Extent{StartLBA: 100, BlockCount: 10})
	got := a.Allocate(4)
	if got.StartLBA != 100 || got.BlockCount != 4 {
		t.Fatalf("allocated = %#v", got)
	}
	a.Free(Extent{StartLBA: 104, BlockCount: 6})
	if len(a.Extents()) != 1 || a.Extents()[0].BlockCount != 10 {
		t.Fatalf("free list did not coalesce: %#v", a.Extents())
	}
}
```

- [ ] **Step 1: Add failing allocator test**

Run: `go test ./compiler/storagefmt -run TestBlobAllocatorSplitsAndCoalesces -v`

Expected: FAIL because allocator is undefined.

- [ ] **Step 2: Implement host allocator**

Add `Extent`, `FreeExtentList`, `Allocate`, `Free`, and `Extents`.

Run: same command.

Expected: PASS.

- [ ] **Step 3: Add Wrela blob source and mirror test**

Create `wrela/storage/blob.wrela` with `BlobRef`, `Extent`, `BlobManifest`, and `BlobExtentAllocator` source shape.

Add `TestBlobSourceMirrorContract`; it must assert `BlobExtentAllocator.allocate`, `BlobExtentAllocator.free`, `BlobExtentAllocator.extents`, and constant `BLOB_ALLOCATOR_FREE_EXTENT_LIMIT = 1024` exist.

Run: `go test ./compiler/sem -run 'TestBlobSource(Compiles|MirrorContract)' -v`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git diff --check
git add wrela/storage/blob.wrela compiler/storagefmt/format.go compiler/storagefmt/format_test.go compiler/sem/blob_source_test.go
git commit -m "feat: add blob refs and extent allocator -Codex Automated"
```

### Task 36: Blob Write Order And Development AEAD

**Prerequisites:** Tasks 12, 25, and 35.

**Files:**

- Modify: `wrela/storage/blob.wrela`
- Modify: `compiler/sem/storage.go`
- Create: `compiler/sem/blob_cipher_test.go`
- Add fixture: `tests/fixtures/negative/development_blob_cipher_without_opt_in.wrela`

**Description:** Add unpublished/published blob refs, final manifest fields, and named development passthrough cipher guarded by explicit opt-in.

**Acceptance Criteria:**

- `write_blob` returns `UnpublishedBlobRef`.
- `StorageWriter.publish_blob_ref` converts only durable blobs to `PublishedBlobRef`.
- Event payload encode expressions cannot reference `UnpublishedBlobRef`; violation emits `SEM0117`.
- Development passthrough without opt-in emits `SEM0123`.

**Code Examples:**

```wrela
data BlobCipherPolicy {
    mode: U64
    key_id: U64
    nonce_low: U64
    nonce_high: U64
    development_opt_in: Bool
}

data UnpublishedBlobRef { ref: BlobRef; manifest_lba: U64 }
data PublishedBlobRef { ref: BlobRef }
```

- [ ] **Step 1: Add failing cipher opt-in test**

Run: `go test ./compiler/sem -run TestDevelopmentBlobCipherRequiresOptIn -v`

Expected: FAIL because `SEM0123` is not emitted.

- [ ] **Step 2: Implement semantic opt-in check**

Add `checkBlobCipherPolicy`.

Run: same command.

Expected: PASS.

- [ ] **Step 3: Add unpublished ref test**

Run: `go test ./compiler/sem -run TestEventCannotReferenceUnpublishedBlob -v`

Expected: FAIL because `SEM0117` is not emitted.

- [ ] **Step 4: Implement unpublished ref check and source**

Modify `blob.wrela` and semantic validation.

Run: `go test ./compiler/sem -run 'Test(DevelopmentBlobCipher|EventCannotReferenceUnpublishedBlob)' -v`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git diff --check
git add wrela/storage/blob.wrela compiler/sem/storage.go compiler/sem/blob_cipher_test.go tests/fixtures/negative/development_blob_cipher_without_opt_in.wrela
git commit -m "feat: guard blob publication and cipher mode -Codex Automated"
```

### Task 37: Orphan Collection

**Prerequisites:** Tasks 25 and 35.

**Files:**

- Modify: `compiler/storagefmt/format.go`
- Modify: `compiler/storagefmt/format_test.go`
- Modify: `wrela/storage/blob.wrela`

**Description:** Implement orphan collection from acknowledged events and manifests.

**Acceptance Criteria:**

- Allocated but unreferenced extent is reclaimable.
- Referenced extent remains live.
- Collector ignores unacknowledged event refs.
- Mirror contract: Wrela `BlobOrphanCollector` uses the same acknowledged-ref rule as host `OrphanCollector`.

**Code Examples:**

```go
func TestOrphanCollectorUsesAcknowledgedBlobRefs(t *testing.T) {
	collector := NewOrphanCollector([]Extent{{StartLBA: 10, BlockCount: 2}, {StartLBA: 20, BlockCount: 2}})
	collector.MarkAcknowledged(BlobRefForExtent(10, 2))
	got := collector.Reclaimable()
	if len(got) != 1 || got[0].StartLBA != 20 {
		t.Fatalf("reclaimable = %#v", got)
	}
}
```

- [ ] **Step 1: Add failing orphan collector test**

Run: `go test ./compiler/storagefmt -run TestOrphanCollectorUsesAcknowledgedBlobRefs -v`

Expected: FAIL because collector is undefined.

- [ ] **Step 2: Implement host collector**

Add `OrphanCollector`.

Run: same command.

Expected: PASS.

- [ ] **Step 3: Mirror Wrela source**

Add `BlobOrphanCollector` methods.

Add `TestBlobOrphanMirrorContract`; it must assert `BlobOrphanCollector.mark_acknowledged`, `BlobOrphanCollector.reclaimable`, and field `acknowledged_refs` exist.

Run: `go test ./compiler/sem -run 'TestBlob(SourceCompiles|OrphanMirrorContract)' -v`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git diff --check
git add compiler/storagefmt/format.go compiler/storagefmt/format_test.go wrela/storage/blob.wrela
git commit -m "feat: collect orphaned blob extents -Codex Automated"
```

### Task 38: Blob Relocation Proposals

**Prerequisites:** Tasks 12, 35, and 37.

**Files:**

- Modify: `wrela/storage/blob.wrela`
- Modify: `wrela/storage/writer.wrela`
- Modify: `compiler/storagefmt/format.go`
- Modify: `compiler/storagefmt/format_test.go`
- Modify: `compiler/sem/blob_source_test.go`
- Add fixture: `tests/fixtures/negative/maintenance_mutates_blob_truth.wrela`

**Description:** Add copy-on-write relocation proposals validated by `StorageWriter`.

**Acceptance Criteria:**

- Proposal includes blob ID, old ref, new ref, and observed version.
- Writer rejects proposal if current ref or version changed.
- Maintenance direct mutation of truth emits `SEM0118`.
- Mirror contract: Wrela relocation proposal fields match host `RelocateBlobProposal` exactly.

**Code Examples:**

```go
func TestRelocateBlobRejectsStaleVersion(t *testing.T) {
	writer := BlobTruth{Version: 4, Ref: BlobRef{BlobID: 9}}
	ok := writer.AcceptRelocate(RelocateBlobProposal{BlobID: 9, OldRef: writer.Ref, NewRef: BlobRef{BlobID: 9}, ObservedVersion: 3})
	if ok {
		t.Fatal("stale relocation must be rejected")
	}
}
```

- [ ] **Step 1: Add failing relocation behavior test**

Run: `go test ./compiler/storagefmt -run TestRelocateBlobRejectsStaleVersion -v`

Expected: FAIL because relocation helpers are undefined.

- [ ] **Step 2: Implement host relocation validation**

Add `BlobTruth` and `AcceptRelocate`.

Run: same command.

Expected: PASS.

- [ ] **Step 3: Add Wrela proposal source and semantic fixture**

Modify `blob.wrela`, `writer.wrela`, and add fixture.

Add `TestBlobRelocationMirrorContract`; it must assert Wrela fields `RelocateBlobProposal.blob_id`, `old_ref`, `new_ref`, and `observed_version` exist.

Run: `go test ./compiler/sem -run TestBlobRelocationMirrorContract -v && go test ./compiler -run TestNegativeFixtures -v`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git diff --check
git add wrela/storage/blob.wrela wrela/storage/writer.wrela compiler/storagefmt/format.go compiler/storagefmt/format_test.go compiler/sem/blob_source_test.go tests/fixtures/negative/maintenance_mutates_blob_truth.wrela
git commit -m "feat: validate blob relocation proposals -Codex Automated"
```

---

## 10. Phase 5: Projections And File Model

**Description:** This phase adds durable projection roots, explicit feed wiring, read watermarks, and a file-like entity model.

**Acceptance Criteria:**

- Projection declarations remain ABI only.
- Projection workers use explicit core-link feeds.
- File content is a blob ref.

### Task 39: Projection Source And Watermark Behavior

**Prerequisites:** Tasks 11, 12, and 25.

**Files:**

- Create: `wrela/storage/projection.wrela`
- Modify: `compiler/storagefmt/format.go`
- Modify: `compiler/storagefmt/format_test.go`
- Create: `compiler/sem/projection_source_test.go`

**Description:** Add projection containers, checkpoints, root refs, query results, and watermark validation behavior.

**Acceptance Criteria:**

- `StateCell<T>`, `DenseEntityMap<Id, T>`, and `OrderedPages<Partition, SortKey, Row>` exist.
- `ProjectionCheckpoint` contains projection ID, layout ID, layout hash, worker code hash, last append-log `event_id`, and root refs.
- Writer accepts `AdvanceProjection` only through current frontier.
- Invalid watermark emits `SEM0119`.
- Mirror contract: Wrela `AdvanceProjection` fields and frontier rule match host `ProjectionTruth.AcceptAdvance`.

**Code Examples:**

```go
func TestProjectionCannotAdvancePastFrontier(t *testing.T) {
	state := ProjectionTruth{AtomicGroupFrontier: 10}
	ok := state.AcceptAdvance(AdvanceProjection{ProjectionID: 12, ThroughEventID: 11})
	if ok {
		t.Fatal("projection advanced past frontier")
	}
}
```

- [ ] **Step 1: Add failing projection behavior test**

Run: `go test ./compiler/storagefmt -run TestProjectionCannotAdvancePastFrontier -v`

Expected: FAIL because projection helpers are undefined.

- [ ] **Step 2: Implement host projection truth**

Add `ProjectionTruth` and `AcceptAdvance`.

Run: same command.

Expected: PASS.

- [ ] **Step 3: Add Wrela projection source and mirror test**

Create `wrela/storage/projection.wrela`.

Add `TestProjectionSourceMirrorContract`; it must assert `AdvanceProjection.projection_id`, `AdvanceProjection.through_event_id`, `ProjectionTruth.accept_advance`, and containers `StateCell`, `DenseEntityMap`, and `OrderedPages` exist.

Run: `go test ./compiler/sem -run 'TestProjectionSource(Compiles|MirrorContract)' -v`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git diff --check
git add wrela/storage/projection.wrela compiler/storagefmt/format.go compiler/storagefmt/format_test.go compiler/sem/projection_source_test.go
git commit -m "feat: add projection storage roots -Codex Automated"
```

### Task 40: Projection Feed Wiring

**Prerequisites:** Tasks 28 and 39.

**Files:**

- Modify: `compiler/sem/storage_graph.go`
- Create: `compiler/sem/projection_feed_test.go`
- Add fixture: `tests/fixtures/negative/projection_worker_without_feed.wrela`

**Description:** Enforce that projection workers receive committed groups only from explicit boot-wired core-link feeds.

**Acceptance Criteria:**

- Worker without feed emits `SEM0120`.
- Worker feed owner must be maintenance slot.
- Foreground writer publishes descriptors to configured producer.

**Code Examples:**

```go
func TestProjectionWorkerRequiresFeed(t *testing.T) {
	_, ds := typeDiagsForModules(t, projectionWorkerWithoutFeedSource)
	if !hasCode(ds, diag.SEM0120) {
		t.Fatalf("diagnostics = %#v, want SEM0120", ds)
	}
}
```

- [ ] **Step 1: Add failing feed test**

Run: `go test ./compiler/sem -run TestProjectionWorkerRequiresFeed -v`

Expected: FAIL because `SEM0120` is not emitted.

- [ ] **Step 2: Implement feed graph check**

Record `ProjectionFeedNode` from boot wiring and check projection worker fields.

Run: same command.

Expected: PASS.

- [ ] **Step 3: Add fixture and commit**

```bash
go test ./compiler -run TestNegativeFixtures -v
git diff --check
git add compiler/sem/storage_graph.go compiler/sem/projection_feed_test.go tests/fixtures/negative/projection_worker_without_feed.wrela
git commit -m "feat: enforce projection feed wiring -Codex Automated"
```

### Task 41: File-Like Entity Stream

**Prerequisites:** Tasks 10, 35, and 39.

**Files:**

- Create: `wrela/storage/file_model.wrela`
- Create: `compiler/sem/file_model_source_test.go`

**Description:** Add file-like events and state on top of streams and blobs. This is not POSIX.

**Acceptance Criteria:**

- File events have IDs 1001-1004.
- `FileContentCommitted` stores `PublishedBlobRef`.
- `FileState` stores current blob ref, name ref, parent ID, deleted flag, and stream sequence.
- `DirectoryChildren` projection uses `OrderedPages<FileId, FileNameKey, DirectoryChild>`.

**Code Examples:**

```wrela
event FileContentCommitted id 1003 {
    file_id: FileId
    blob_ref: PublishedBlobRef
    layout 1 current {
        file_id: U64 = self.file_id.value
        blob_ref: PublishedBlobRef = self.blob_ref
    }
}
```

- [ ] **Step 1: Add failing file model compile test**

Run: `go test ./compiler/sem -run TestFileModelSourceCompiles -v`

Expected: FAIL because module does not exist.

- [ ] **Step 2: Add file model source**

Create events, projection declaration, `FileState`, and `DirectoryProjectionWorker`.

Run: same command.

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add wrela/storage/file_model.wrela compiler/sem/file_model_source_test.go
git commit -m "feat: add file model on event storage -Codex Automated"
```

### Task 42: Directory Projection Worker Behavior

**Prerequisites:** Tasks 25, 39, 40, and 41.

**Files:**

- Modify: `wrela/storage/file_model.wrela`
- Modify: `compiler/storagefmt/format.go`
- Modify: `compiler/storagefmt/format_test.go`

**Description:** Add behavior-testable directory projection updates.

**Acceptance Criteria:**

- Worker ignores unknown event type IDs.
- `FileCreated`, `FileRenamed`, `FileContentCommitted`, and `FileDeleted` update file state.
- Worker publishes watermark equal to group `last_event_id`.
- Mirror contract: Wrela `DirectoryProjectionWorker.apply_group` applies the same event-type table as host `DirectoryProjection.ApplyGroup`.

**Code Examples:**

```go
func TestDirectoryProjectionPublishesGroupWatermark(t *testing.T) {
	p := DirectoryProjection{}
	p.ApplyGroup(CommittedGroup{LastEventID: 9}, []Event{{TypeID: 1001}})
	if p.Watermark != 9 {
		t.Fatalf("watermark = %d, want 9", p.Watermark)
	}
}
```

- [ ] **Step 1: Add failing directory projection behavior test**

Run: `go test ./compiler/storagefmt -run TestDirectoryProjectionPublishesGroupWatermark -v`

Expected: FAIL because behavior helper is undefined.

- [ ] **Step 2: Implement host projection behavior**

Add `DirectoryProjection` with fields `Files map[uint64]FileState`, `Watermark uint64`, and method `ApplyGroup(group CommittedGroup, events []Event)`. The method must ignore unknown `Event.TypeID`, update file state for type IDs `1001..1004`, and set `Watermark = group.LastEventID` after processing the group.

Run: same command.

Expected: PASS.

- [ ] **Step 3: Mirror Wrela worker code**

Update `DirectoryProjectionWorker.apply_group`.

Add `TestDirectoryProjectionWorkerMirrorContract`; it must assert `DirectoryProjectionWorker.apply_group` handles `FILE_EVENT_CREATED = 1001`, `FILE_EVENT_RENAMED = 1002`, `FILE_EVENT_CONTENT_COMMITTED = 1003`, and `FILE_EVENT_DELETED = 1004`.

Run: `go test ./compiler/sem -run 'Test(FileModelSourceCompiles|DirectoryProjectionWorkerMirrorContract)' -v`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git diff --check
git add wrela/storage/file_model.wrela compiler/storagefmt/format.go compiler/storagefmt/format_test.go
git commit -m "feat: apply directory projection groups -Codex Automated"
```

---

## 11. Phase 6: E2E And Metrics

**Description:** This phase wires the boot image, replay test, orphan collection test, and reports. It starts only after all compiler/runtime storage pieces land.

**Acceptance Criteria:**

- QEMU disk is a sparse 4 GiB raw disk.
- First boot appends two events in one atomic group and prints `last_event_id=1`.
- Replay boot recovers `last_event_id=1`.
- Metrics command uses the corrected placeholder scan.

### Task 43: Storage Report Metrics

**Prerequisites:** Tasks 12, 25, 34, 38, 39, and 40.

**Files:**

- Modify: `compiler/report/report.go`
- Modify: `compiler/report/report_test.go`
- Modify: `compiler/sem/report.go`
- Create: `compiler/sem/storage_report_test.go`

**Description:** Publish storage audit and metrics data from the semantic graph.

**Acceptance Criteria:**

- Report includes active LBA size, namespace mode, durability mode, event slot size, batch metrics, latency metrics, media-write metrics, queue depths, core-link metrics, blob orphan bytes, projection lag, projection upcast/rebuild counts, and stream directory cache hit rate.
- Missing required storage metrics in a storage image emits `SEM0124`.

**Code Examples:**

```go
type StorageReport struct {
	ActiveLBASize uint64
	NamespaceMode string
	DurabilityMode string
	EventSlotSize uint64
	ReservedEmptySlots uint64
	DeviceReportedMediaWrites uint64
	ProjectionLagEvents uint64
	StreamDirectoryCacheHitRateX1000 uint64
	NvmePaths []NvmePathReport
}
```

- [ ] **Step 1: Add failing report test**

Run: `go test ./compiler/report -run TestStorageMetricsReportShape -v`

Expected: FAIL because `StorageReport` fields are undefined.

- [ ] **Step 2: Add report structs**

Modify `compiler/report/report.go`.

Run: same command.

Expected: PASS.

- [ ] **Step 3: Add semantic report population test**

Run: `go test ./compiler/sem -run TestStorageMetricsReportPopulation -v`

Expected: FAIL because sem report does not populate storage fields.

- [ ] **Step 4: Populate report**

Modify `compiler/sem/report.go`.

Run:

```bash
go test ./compiler/report -run TestStorageMetricsReportShape -v
go test ./compiler/sem -run TestStorageMetricsReportPopulation -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git diff --check
git add compiler/report/report.go compiler/report/report_test.go compiler/sem/report.go compiler/sem/storage_report_test.go
git commit -m "feat: report nvme event storage metrics -Codex Automated"
```

### Task 44: NVMe Storage Boot Fixture

**Prerequisites:** Tasks 4, 26, 34, 41, and 43.

**Files:**

- Create: `tests/e2e/fixtures/nvme_event_storage/main.wrela`
- Create: `tests/e2e/fixtures/nvme_event_storage/program.wrela`
- Create: `tests/e2e/nvme_event_storage_qemu_test.go`

**Description:** Boot a storage image, discover NVMe by class code, create foreground/background paths, append a two-event atomic group, and print deterministic serial markers.

**Acceptance Criteria:**

- NVMe PCI function is selected by base class `0x01`, subclass `0x08`, prog-if `0x02`.
- Foreground path owner is foreground slot.
- Background path owner is maintenance slot.
- `main.wrela` defines both `phase delegated_hardware(...) -> OwnedHardware` and `phase owned_hardware(hardware: OwnedHardware) -> never`.
- `StorageWriter` is constructed only in the owned-hardware phase using the legal constructor shape from Task 12.
- First command appends `FileCreated` and `FileContentCommitted` as one atomic group.
- Serial output includes `NVME_STORAGE_APPEND_OK last_event_id=1` and `NVME_STORAGE_DONE`.

**Code Examples:**

```go
func TestNvmeEventStorageQEMU(t *testing.T) {
	disk := filepath.Join(t.TempDir(), "storage.raw")
	createSparseRawDisk(t, disk, nvmeStorageDiskBytes)
	out := runStorageQEMU(t, disk, "first")
	for _, want := range []string{"NVME_STORAGE_APPEND_OK last_event_id=1", "NVME_STORAGE_DONE"} {
		if !strings.Contains(out, want) {
			t.Fatalf("serial output missing %q:\n%s", want, out)
		}
	}
}
```

`tests/e2e/fixtures/nvme_event_storage/main.wrela` must contain this owned phase shape:

```wrela
image NvmeEventStorageImage {
    transitions {
        delegated_hardware -> owned_hardware
    }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.exit_to_owned_hardware()
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        let nvme_device = NvmeDriver.claim_controller(devices = hardware.hardware_plan.pci.devices, occurrence = 0)
        let driver = NvmeDriver.initialize(device = nvme_device)
        let foreground_path = foreground_storage_path(path = driver.create_io_path(role = NVME_PATH_FOREGROUND, queue_id = 1, owner = ExecutorSlot(id = 0), vector = 0x50))
        let background_path = background_storage_path(path = driver.create_io_path(role = NVME_PATH_BACKGROUND, queue_id = 2, owner = ExecutorSlot(id = 1), vector = 0x51))
        let writer = StorageWriter(
            foreground = foreground_path,
            background = background_path,
            stream_directory = StreamDirectory(next_stream_id = 0),
            metrics = StorageMetrics()
        )
        let created = FileCreated(file_id = FileId(value = 1), parent_id = FileId(value = 0), name_ref = PublishedBlobRef(ref = BlobRef(blob_id = 1)))
        let committed = FileContentCommitted(file_id = FileId(value = 1), blob_ref = PublishedBlobRef(ref = BlobRef(blob_id = 2)))
        let group = PendingAtomicGroup.empty().push_file_created(value = created).push_file_content_committed(value = committed)
        match writer.enqueue_atomic_group(group = group) {
            StorageAppendResult.Committed(last_event_id = last_event_id) => {
                hardware.serial.write_line(value = "NVME_STORAGE_APPEND_OK last_event_id=1")
            }
            StorageAppendResult.Failed(code = code) => {
                hardware.panic.fail(code = code)
            }
        }
        hardware.serial.write_line(value = "NVME_STORAGE_DONE")
        while true {
            hardware.cpu.halt_forever()
        }
    }
}
```

- [ ] **Step 1: Add failing E2E test**

Run: `go test ./tests/e2e -run TestNvmeEventStorageQEMU -v`

Expected: FAIL because fixture files do not exist.

- [ ] **Step 2: Add fixture source**

Create `main.wrela` with the full two-phase image shape above. Create `program.wrela` with the `FileCreated`, `FileContentCommitted`, and `DirectoryProjectionWorker` definitions used by the owned phase. The fixture must use `match` arms with fat arrows and braced blocks exactly as shown.

Run: same command.

Expected: PASS on QEMU/OVMF machine, SKIP if QEMU is unavailable.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add tests/e2e/fixtures/nvme_event_storage/main.wrela tests/e2e/fixtures/nvme_event_storage/program.wrela tests/e2e/nvme_event_storage_qemu_test.go
git commit -m "test: boot nvme event storage image -Codex Automated"
```

### Task 45: Replay And Orphan E2E

**Prerequisites:** Tasks 37, 38, and 44.

**Files:**

- Modify: `tests/e2e/nvme_event_storage_qemu_test.go`
- Modify: `tests/e2e/fixtures/nvme_event_storage/program.wrela`

**Description:** Reuse the same sparse disk across two boots to prove recovery, projection watermark, and orphan collection.

**Acceptance Criteria:**

- First boot prints `NVME_STORAGE_APPEND_OK last_event_id=1`.
- Second boot prints `NVME_STORAGE_REPLAY_OK last_event_id=1`.
- Second boot prints `projection_watermark=1`.
- Interrupted relocation before writer acceptance prints `NVME_ORPHAN_COLLECTION_OK`.

**Code Examples:**

```go
func TestNvmeEventStorageReplayQEMU(t *testing.T) {
	disk := filepath.Join(t.TempDir(), "storage.raw")
	createSparseRawDisk(t, disk, nvmeStorageDiskBytes)
	first := runStorageQEMU(t, disk, "first")
	if !strings.Contains(first, "last_event_id=1") {
		t.Fatalf("first boot missing last_event_id:\n%s", first)
	}
	second := runStorageQEMU(t, disk, "replay")
	for _, want := range []string{"NVME_STORAGE_REPLAY_OK last_event_id=1", "projection_watermark=1", "NVME_ORPHAN_COLLECTION_OK"} {
		if !strings.Contains(second, want) {
			t.Fatalf("second boot missing %q:\n%s", want, second)
		}
	}
}
```

- [ ] **Step 1: Add failing replay test**

Run: `go test ./tests/e2e -run TestNvmeEventStorageReplayQEMU -v`

Expected: FAIL until replay marker code exists.

- [ ] **Step 2: Implement replay/orphan markers**

Modify fixture program to branch on fw_cfg mode string and print replay/orphan markers.

Run: same command.

Expected: PASS on QEMU/OVMF machine, SKIP if QEMU is unavailable.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add tests/e2e/nvme_event_storage_qemu_test.go tests/e2e/fixtures/nvme_event_storage/program.wrela
git commit -m "test: verify nvme storage replay and orphan collection -Codex Automated"
```

### Task 46: Final Acceptance Sweep

**Prerequisites:** Tasks 1-45.

**Files:**

- None.

**Description:** Run full verification and ensure deferred-work docs still match non-goals. This task is a no-commit verification gate.

**Acceptance Criteria:**

- Full compiler and unit suite passes.
- QEMU storage tests pass or skip only for missing QEMU/OVMF.
- Placeholder scan command exits 0.
- Deferred-work docs retain all storage non-goals.
- `git status --short` shows no uncommitted plan-task changes after verification.

**Code Examples:**

```bash
go test ./...
go test ./tests/e2e -run NvmeEventStorage -v
bad_terms='TO''DO|TB''D|fill'' in|implement'' later|Add'' appropriate|similar'' to'' Task|if'' needed|or'' equivalent'
if rg -n "$bad_terms" wrela compiler tests docs/implementation/2026-05-18-nvme-event-storage-executable-plan.md; then
  exit 1
fi
bad_match=$(printf '%s%s' 'match .*-' '>')
if rg -n "$bad_match" docs/implementation/2026-05-18-nvme-event-storage-executable-plan.md tests/fixtures tests/e2e wrela; then
  exit 1
fi
git diff --check
```

- [ ] **Step 1: Run full suite**

Run: `go test ./...`

Expected: PASS.

- [ ] **Step 2: Run QEMU storage suite**

Run: `go test ./tests/e2e -run NvmeEventStorage -v`

Expected: PASS on QEMU/OVMF machine; SKIP is acceptable only when QEMU/OVMF is unavailable.

- [ ] **Step 3: Run placeholder and diff checks**

Run the command in the code example above.

Expected: no output from `rg`; `git diff --check` prints nothing.

- [ ] **Step 4: Confirm no commit is needed**

```bash
git status --short
```

Expected: no output. Do not create an empty commit for this verification-only task.

---

## 12. Appendix A: Exact Append And Recovery Algorithms

**Description:** These algorithms are normative for Tasks 20, 22, 25, and 26.

**Acceptance Criteria:**

- Host behavior tests and Wrela source follow these branches exactly.
- Append-log `event_id` values begin at `0`.

**Code Example:**

```text
enqueue_atomic_group(group):
  if group.semantic_event_count > 32:
    reject TransactionTooLarge

  first_event_id = next_event_id
  last_event_id = next_event_id + group.semantic_event_count - 1

  if open_batch_slots + group.semantic_event_count < 64:
    add group
    return pending(first_event_id, last_event_id)

  if open_batch_slots + group.semantic_event_count <= 72:
    add group
    flush batch
    return pending(first_event_id, last_event_id)

  flush current batch
  add group to new batch
  if group.semantic_event_count >= 64:
    flush batch
  return pending(first_event_id, last_event_id)
```

```text
finish_lba_padding(active_lba_size, semantic_slots):
  slots_per_lba = active_lba_size / 512
  remainder = semantic_slots % slots_per_lba
  if remainder == 0:
    reserved_empty_slots = 0
  else:
    reserved_empty_slots = slots_per_lba - remainder
  total_slot_positions = semantic_slots + reserved_empty_slots
```

```text
recover_hot_slots:
  expected_event_id = first_uncompressed_event_id
  while true:
    read slot for expected_event_id
    if checksum invalid: stop before slot
    if header.event_id != expected_event_id: stop before slot
    if header.event_type_id == 0:
      if header.flags != STORAGE_SLOT_RESERVED_EMPTY: stop before slot
      expected_event_id += 1
      continue
    group_len = header.atomic_group_len
    if group_len == 0 or group_len > 32: stop before slot
    for group_index in 0..group_len-1:
      validate event_id, checksum, group_len, group_index
      if any invalid: stop before first group slot
    publish group through last event
    expected_event_id += group_len
```

```text
durability_ack:
  FUA or PFAIL_ATOMIC_FUA:
    acknowledge after all write completions succeed
  WRITE_PLUS_FLUSH:
    acknowledge after all write completions and following flush completion succeed
  after acknowledgement:
    advance atomic_group_frontier
    publish CommittedAtomicGroup to maintenance core link
```

---

## 13. Appendix B: Exact Syntax Forms

**Description:** These syntax forms are final for this plan.

**Acceptance Criteria:**

- Parser accepts these forms.
- Parser does not introduce a `worker` keyword.

**Code Example:**

```wrela
event NoteRenamed id 21 {
    note_id: FileId
    title_ref: PublishedBlobRef

    layout 1 {
        note_id: U64
        old_title_ref: PublishedBlobRef
    }

    layout 2 current {
        note_id: U64 = self.note_id.value
        title_ref: PublishedBlobRef = self.title_ref
    }

    upcast 1 -> 2 {
        old_title_ref -> title_ref
    }
}

projection DirectoryChildren id 12 {
    layout 1 current {
        children: OrderedPages<FileId, FileNameKey, DirectoryChild>
    }
}

class DirectoryProjectionWorker {
    source: CoreSpscConsumer<CommittedAtomicGroup>
    projection: ProjectionWriter<DirectoryChildren>
}
```

Rules:

- `event_type_id = 0` is reserved for committed empty slots.
- Durable event type IDs must be positive `U64` literals.
- Projection IDs must be positive `U64` literals.
- Layout IDs must be positive `U64` literals scoped to one event or projection.
- Layout ID `0` is reserved.
- A declaration with one layout may omit `current`; semantic checking marks it current.
- A declaration with more than one layout must mark exactly one layout `current`.
- Upcast mappings are same-type field renames.
- Projection containers are only `StateCell<T>`, `DenseEntityMap<Id, T>`, and `OrderedPages<Partition, SortKey, Row>`.
