# Realtime Field Desktop Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the first Wrela realtime field desktop proof: a display-paced, field-native desktop with deterministic virtual framebuffer tests, GOP presentation, bounded visible app participation, text, input, sampled media, semantic nodes, frame reports, replay, and a small beautiful demo scene.

**Architecture:** The desktop is one synchronous foreground world. Trusted Wrela-owned shell and app code emit fields, input targets, and semantic nodes into scope-owned frame graphs; a visible renderer bins fields into tiles, evaluates built-in field programs, composites to an internal surface, presents through a display backend, and publishes inspectable cost/provenance reports. Storage, indexing, and other uncertain work cross explicit submit/completion queues; frame code may poll bounded completions but may never wait.

**Tech Stack:** Go 1.22+; existing Wrela lexer/parser/semantic checker/IR/x86_64 codegen; Wrela source modules under `wrela/`; deterministic host-side renderer mirrors under `compiler/desktopfmt`; QEMU q35 + OVMF; GOP framebuffer first hardware backend; scalar renderer correctness first with explicit AVX2 strategy metadata and an AVX2 packet renderer task before final acceptance.

---

## 0. How To Execute This Plan

**Description:** This plan is written so a junior engineer can pick up any task whose prerequisites have landed, run the named tests, and produce a reviewable commit. The plan intentionally chooses the unresolved design questions from `docs/design/2026-05-19-realtime-field-desktop-design.md` so implementation work does not reopen product or architecture decisions.

**Acceptance Criteria:**

- Every task commit message ends in `-Codex Automated`.
- Every task except verifier/gate Tasks 0, 3A, and 10G starts with the failing test or source-shape check named in the task. Task 0, Task 3A, and Task 10G are prerequisite gates and must pass before dependent implementation begins.
- Expected-failure steps must fail for the exact named reason. A wrong failure, such as a missing package when the step expects an undefined symbol, is not accepted.
- Every task runs `git diff --check` before committing.
- A task that sees an expected failing test pass before implementation stops and inspects the existing code before editing.
- A task that sees an expected passing test fail keeps working inside that task until the exact command passes.
- The full plan is complete only when every command in Appendix C passes.

**Code Examples:**

```bash
go test ./compiler/desktopfmt -v
go test ./compiler/sem -run Desktop -v
go test ./compiler/ir -run Desktop -v
go test ./compiler/codegen -run Desktop -v
go test ./tests/e2e -run RealtimeDesktop -v
go test ./...
git diff --check
```

Definition of done for one task:

```text
1. Task prerequisites are landed.
2. The task's failing test fails for the reason listed in the task.
3. The implementation is limited to the files listed in the task.
4. The task's passing verification commands pass.
5. git diff --check prints no output.
6. The task is committed with the exact message shown in the task.
```

Contract amendment rule:

```text
If a later task discovers that an earlier public contract in this plan is wrong, do not silently change source names, field order, diagnostics, or ABI shapes inside the later task. Stop and add a small contract-amendment task before continuing, with failing tests that prove why the earlier contract must change. If the earlier commit is still local and unshared, the tech lead may amend it; otherwise stack a follow-up commit that updates all affected tasks and tests.
```

Execution prerequisite:

```text
Before Task 0 starts, merge or replay the completed NVMe event-storage substrate worktree from branch codex/nvme-event-storage. In the original planning workspace it is located at /Users/ryanwible/.config/superpowers/worktrees/wrela3/codex-nvme-event-storage. The execution worktree must then contain SEM0099 through SEM0124, storage.writer.*, storage.format.*, machine.x86_64.core_link.*, and the NVMe/storage source-shape helpers.
```

---

## 1. Frozen Desktop Decisions

**Description:** These decisions are binding for this implementation plan. They convert the design document's representative shapes and open questions into exact v1 contracts.

**Acceptance Criteria:**

- No task invents new public Wrela names outside the names listed here or in its task section.
- No task changes the first milestone away from deterministic virtual framebuffer plus GOP hardware presentation.
- No task adds DOM, HTML/CSS layout, POSIX window server, GPU dependency, third-party app hosting, coroutine scheduling, or blur-heavy visual identity.

**Code Examples:**

First milestone source policy:

```wrela
data DesktopMilestonePolicy {
    internal_width: U64
    internal_height: U64
    preferred_hz: U64
    tile_width: U64
    tile_height: U64
    max_fields_per_scope: U64
    max_unknown_supports_per_scope: U64
}
```

The canonical v1 values are fixed:

```text
internal_width = 1920
internal_height = 1080
preferred_hz = 120
minimum_good_hz = 60
fallback_width = 1280
fallback_height = 720
tile_width = 16
tile_height = 16
sampled_field_source = embedded 64x64 RGBA checker image
text_layout = ASCII monospace 8x8 bitmap coverage first, with glyph coverage cache; analytic glyph SDFs are deferred until after this plan
first_3d_object = analytic sphere field with bounded projected support
field_dispatch = FieldProgram enum, not first-class function pointers
frame_report_format = stable Go JSON mirror plus Wrela FrameReportSummary
provenance = per-field always-on, per-pixel only by inspector rerun
non-GOP display backend = deterministic virtual framebuffer in this plan
```

The first renderer uses integer fixed-point geometry to match the current compiler:

```wrela
data Fx26_6 {
    raw: I64
}

data Vec2Fx {
    x: Fx26_6
    y: Fx26_6
}
```

The design document's `F32` examples remain the semantic target, but this executable plan uses `Fx26_6` for v1 source and renderer code. That keeps the implementation inside the current Wrela compiler's integer codegen instead of bundling a general floating-point language project into the desktop milestone.

Effect and lane decisions:

```wrela
data BuiltinRoundedRectEval {
    pure lane frame_safe fn eval(self, data: FieldData, p: SamplePoint) -> FieldValue {
        return FieldValue(distance = 0)
    }
}
```

`pure`, `lane`, `frame_safe`, and `may_wait` are method-prefix metadata parsed and checked in this plan. There are no module-scope `effect` declarations in v1. AVX2 packet execution is explicit renderer strategy metadata and a dedicated packet-renderer path for built-in field programs; Wrela v1 does not expose arbitrary first-class function pointers for field eval or shade.

Storage substrate baseline:

```text
The desktop plan assumes the NVMe event-storage worktree has landed before Task 25:
  storage.writer.StorageWriter
  storage.writer.PendingAtomicGroup
  storage.writer.StorageAppendResult
  storage.writer.CommittedAtomicGroup
  machine.x86_64.core_link.CoreSpscConsumer<CommittedAtomicGroup>

The frame lane never owns StorageWriter. It submits semantic storage requests through a bounded app-owned facade and polls committed durability facts through CoreSpscConsumer<CommittedAtomicGroup>. It may call try_next with a literal per-frame cap in the surrounding loop; it must not call arm_wait from frame_safe code.
```

---

## 2. Repository Layout And File Ownership

**Description:** This section locks the file split before task decomposition. Each file has one responsibility and one owning task stream.

**Acceptance Criteria:**

- Source contracts live under `wrela/desktop/`.
- Host mirrors and deterministic rendering tests live under `compiler/desktopfmt/`.
- Compiler syntax/effect changes stay in existing compiler packages and are modified only by the tasks named below.
- Display/GOP codegen edits are isolated to the display backend tasks.

**Code Examples:**

Runtime source files:

```text
wrela/desktop/units.wrela
  Fixed-point units, vectors, rectangles, transforms, bounds, and rational rates.

wrela/desktop/identity.wrela
  FieldIdentity, ScopeIdentity, InputIdentity, SemanticIdentity, DisplayIdentity, AppIdentity, FrameId, EventId.

wrela/desktop/color.wrela
  Color, ColorSpace, TransferFunction, PixelFormat, ScaleFilter, OutputColorPolicy.

wrela/desktop/display.wrela
  DisplayMode, FrameClock, SurfaceDesc, Framebuffer, DisplayBackend, DisplayOutput, capabilities, present results.

wrela/desktop/field.wrela
  FieldSupport, DistanceSemantics, FieldSource, sampled content contracts, FieldProgram, FieldData, Field.

wrela/desktop/frame.wrela
  FrameBudget, FrameBudgetLease, FieldScope, FrameGraph, scope writer methods, cadence.

wrela/desktop/input.wrela
  InputFrame, pointer samples, input targets, capture policy, hit results.

wrela/desktop/semantics.wrela
  SemanticNode, roles, values, actions, focus policy, accessibility tree summary.

wrela/desktop/text.wrela
  TextLine, GlyphInstance, caret stops, selection state, ASCII 8x8 bitmap coverage programs, glyph cache keys.

wrela/desktop/reports.wrela
  FrameReportSummary, scope cost, renderer cost, cache stats, missed-frame reason, latency estimate.

wrela/desktop/replay.wrela
  ReplayFrameInput, PixelProvenance, FrameReplayResult.

wrela/desktop/renderer.wrela
  RendererStrategy, TileGrid, RenderResult, scalar and AVX2 strategy declarations.

wrela/desktop/shell.wrela
  DesktopState, visible windows, shell tick, late lane, output passes.

wrela/desktop/demo.wrela
  First demo scene: background, text box, selection, scroll view, sampled checker image, analytic sphere.

wrela/desktop/storage_boundary.wrela
  Frame-safe submit/poll contracts for visible durability facts.
```

Host mirror files:

```text
compiler/desktopfmt/types.go
compiler/desktopfmt/surface.go
compiler/desktopfmt/support.go
compiler/desktopfmt/field.go
compiler/desktopfmt/tile.go
compiler/desktopfmt/render.go
compiler/desktopfmt/text.go
compiler/desktopfmt/input.go
compiler/desktopfmt/report.go
compiler/desktopfmt/replay.go
compiler/desktopfmt/fixtures.go
compiler/desktopfmt/*_test.go
```

Compiler files with narrow ownership:

```text
compiler/diag/codes.go
compiler/diag/diag_test.go
  Task 1 only.

compiler/lex/token.go
compiler/lex/lexer.go
compiler/lex/lexer_test.go
compiler/ast/ast.go
compiler/ast/ast_test.go
compiler/parse/parser.go
compiler/parse/parser_test.go
  Task 11 only.

compiler/sem/effects.go
compiler/sem/effects_test.go
compiler/sem/check.go
compiler/sem/types.go
  Tasks 12A through 13C and Task 30C only.

compiler/ir/ir.go
compiler/ir/lower.go
compiler/ir/desktop_test.go
  Task 24 only.

compiler/codegen/x64.go
compiler/codegen/desktop_display_test.go
compiler/codegen/desktop_copy_exec_test.go
compiler/codegen/desktop_renderer_test.go
  Tasks 28 through 30C only.
```

---

## 3. Test Harness And Existing Helpers

**Description:** Later tasks may use only the helpers listed here or helpers created by their own steps. This prevents hidden coupling between unrelated tasks.

**Acceptance Criteria:**

- New semantic source-shape tests use `parseModulesForTest`, `mustBuildIndex`, `mustCheck`, or `parseUEFIModuleSet`.
- New host renderer tests use only `compiler/desktopfmt` package helpers created in Task 2.
- E2E tests reuse `compiler/qemu/run.go` instead of shelling out directly.

**Code Examples:**

Existing helpers:

```text
compiler/sem/testutil_test.go:
  parseModulesForTest
  mustBuildIndex
  mustCheck
  mustBuildIndexAllowingMissingImage
  checkAllowingMissingImage
  typeDiagsForModules

compiler/sem/uefi_source_shape_test.go:
  parseUEFIModuleSet
  moduleType
  methodByName
  fieldTypeName

compiler/qemu/run.go:
  qemu.Options
  qemu.Run
  qemu.Args
```

New desktopfmt smoke helper:

```go
package desktopfmt

func TestDesktopHarnessCompiles(t *testing.T) {
	s := NewSurface(4, 4, PixelFormatRGBA8)
	if err := s.Store(1, 2, RGBA8{R: 0x10, G: 0x20, B: 0x30, A: 0xff}); err != nil {
		t.Fatal(err)
	}
	got, err := s.At(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got != (RGBA8{R: 0x10, G: 0x20, B: 0x30, A: 0xff}) {
		t.Fatalf("pixel = %#v", got)
	}
}
```

---

## 4. Parallel Work Map

**Description:** Parallel work is allowed only where file ownership is disjoint. The serial spine is diagnostics, host mirror, source contracts, effect checking, renderer, display, demo, and final E2E.

**Acceptance Criteria:**

- Task 1 lands before any task references SEM0125 through SEM0166.
- Task 2 lands before host renderer tasks.
- Tasks 4 through 10G land before source integration tasks.
- Tasks 11 through 13C land before any `pure`, `lane`, `frame_safe`, or `may_wait` source is required by UEFI module parsing.
- Tasks 14 through 20 land before the demo's pixel/replay acceptance tests.
- Tasks 27 through 30C land before the QEMU desktop smoke test.

**Code Examples:**

```text
Merge Gate -1:
  Task 0. Verifies storage substrate, baseline language constructs, and source-shape helper patterns before desktop work starts.

Merge Gate 0:
  Tasks 1-3. Diagnostics, host harness, source registration helper.

Stream A, Desktop Source Contracts:
  Tasks 4-10G. These create wrela/desktop modules and one task-owned source-shape test file per module group. Treat them as serial unless workers explicitly coordinate because the source contracts are the mirror for later Go tests.

Stream B, Language Effects:
  Tasks 11-13C. These add syntax and semantic checks for pure/lane/effects and authority rules. They can start after Task 0 but must land before any runtime source task adds `frame_safe`, `pure`, `lane`, or `may_wait` methods.

Stream C, Host Renderer:
  Tasks 14-20. These are Go-only after Task 2 and Task 6 because the host mirror must match the frozen Field/Support/FieldProgram contract.

Stream D, Runtime Desktop:
  Tasks 21-26. These depend on Streams A and B, and on enough host renderer behavior for expected hashes.

Stream E, Display And Codegen:
  Tasks 27-30C. These touch codegen and QEMU-facing integration.

Stream F, Demo And Acceptance:
  Tasks 31-34. These run after all prior streams land.
```

Implementation DAG matrix:

```text
can-run-with means simultaneously runnable from the same base commit after both rows' prerequisites are already satisfied.
task   prerequisites        owned files                         blocked by             can run with
0      storage worktree     compiler/sem/*baseline*             substrate merge         none
1      0                    compiler/diag, deferred-work doc    0                       2 after commit
2      1                    compiler/desktopfmt surface files   1                       3, 3A
3      1                    compiler/sem source test helpers    1                       2
3A     0,3                  compiler/sem idiom anchor test      3                       2
4      3A                   units/identity source + test        3A                      11
5      4                    color/display source + test         4                       11
6      5                    field source + test                 5                       11
7      6                    frame source + test                 6                       11
8      7                    input/semantics source + test       7                       11
9      8                    text source + test                  8                       11
10A    9                    reports source + test               9                       10C, 10D, 10E
10B    10A                  replay source + test                10A                     10C, 10D, 10E
10C    10A                  renderer source + test              10A                     10B, 10D, 10E
10D    10A                  storage boundary source + test      10A, storage            10B, 10C, 10E
10E    8,10A                shell source + test                 8,10A                   10B, 10C, 10D
10F    6,9,10C,10E          demo source + test                  10C,10E                 none
10G    10A-10F              full source set test                10F                     12A after 11
11     1                    lex/ast/parse files                 1                       4-10A if no modifiers
12A    11                   sem metadata files                  11                      14 after 6
12B    12A                  sem effects files                   12A                     14-20
12C    12B                  sem effects files                   12B                     14-20
12D    12C                  sem effects files                   12C                     14-20
12E    12D                  sem effects files                   12D                     14-20
13A    10G,12E              sem authority files                 10G,12E                 14-20
13B    13A                  sem authority files                 13A                     14-20
13C    10D,12E,13B          sem poll/authority files            13B                     14-20
14     2,6                  desktopfmt support/field            2,6                     11-13C
15     14                   desktopfmt tile/render              14                      11-13C
16A    15                   desktopfmt field/render tests       15                      11-13C
16B    16A                  desktopfmt field/render tests       16A                     none
16C    16B                  desktopfmt field/render tests       16B                     16D,16E
16D    16B                  desktopfmt field/render tests       16B                     16C,16E
16E    16B                  desktopfmt field/render tests       16B                     16C,16D
17     16E                  desktopfmt reports/render           16E                     source tasks after 13C
18     17                   desktopfmt text                     17                      none
19     18                   desktopfmt input                    18                      none
20A    19                   desktopfmt replay/fixtures          19                      21 after 13C
20B    20A                  desktopfmt replay                  20A                     none
20C    20B                  desktopfmt provenance               20B                     20D
20D    20B                  desktopfmt storage facts            20B                     20C
21     7,13C                frame source budget test            13C                     27 after 20D
22     9,12E,21             text/demo source                    21                      27 after 20D
23     22                   text source scroll test             22                      27 after 20D
24     10G,12E              compiler/ir desktop files           10G,12E                 27 after 20D
25     10D,13C,24           storage boundary/demo source        24                      27 after 20D
26A    21-25                shell source                        25                      27 after 20D
26B    26A                  shell source                        26A                     none
26C    26B                  shell/demo degradation source       26B                     none
27     20D                  desktopfmt display                  20D                     28 after 24
28     5,24                 UEFI/display/codegen symbol test    24                      27
29A    28                   desktopfmt display scaling          28                      30A blocked
29B    28,29A               codegen GOP ABI tests               29A                     none
29C    29B                  codegen x64 GOP loop                29B                     none
30A    16E,24,29C           codegen scalar renderer             29C                     none
30B    30A                  codegen AVX2 renderer               30A                     none
30C    30B                  sem/codegen vector policy           30B                     none
31     20D,26C              demo source/fixture/hash            20D,26C                 none
32     29C,30C,31           e2e realtime desktop fixture        31                      33 after 31
33     20C,31               replay/inspect files                31                      32
34     all prior            docs/runtime + deferred work        all prior               none
```

---

## 5. Phase 0: Diagnostics, Harnesses, And Source Loading

**Description:** Establish stable diagnostic names, the deterministic host framebuffer package, and source-loading hooks before any feature task depends on them.

**Acceptance Criteria:**

- Desktop diagnostic constants exist and are unique.
- `compiler/desktopfmt` can allocate, write, read, clear, and hash RGBA8 surfaces.
- UEFI source-shape tests can opt into loading `wrela/desktop/*.wrela`.

**Code Examples:**

```go
func TestDesktopDiagnosticCodesExist(t *testing.T) {
	want := []string{diag.SEM0125, diag.SEM0126, diag.SEM0127, diag.SEM0128}
	for i, got := range want {
		if got == "" {
			t.Fatalf("diagnostic %d is empty", i)
		}
	}
}
```

### Task 0: Substrate And Language Baseline Verifier

**Prerequisite:** Completed NVMe event-storage substrate worktree has been merged or replayed into the execution worktree.

**Files:**

- Create: `compiler/sem/desktop_prereq_test.go`
- Create: `compiler/parse/desktop_language_baseline_test.go`

**Description:** Fail loudly if the execution worktree does not have the storage substrate and language surface this plan assumes. This avoids discovering prerequisite drift halfway through desktop source or storage-boundary tasks.

**Acceptance Criteria:**

- `diag.SEM0124` exists.
- `storage.writer.StorageWriter`, `PendingAtomicGroup`, `StorageAppendResult`, and `CommittedAtomicGroup` typecheck.
- `machine.x86_64.core_link.CoreSpscConsumer<CommittedAtomicGroup>` typechecks and exposes `try_next`.
- Baseline parser tests prove generic type arguments, enum named payloads, payload-less variants, and match payload destructuring already work.
- Baseline parser test proves module-scope `fn` is still rejected, so desktop source tasks must use methods only.

**Code Examples:**

```go
func TestDesktopPrereqStorageSubstrateExists(t *testing.T) {
	if diag.SEM0124 != "SEM0124" {
		t.Fatalf("SEM0124 = %q, want SEM0124; merge storage substrate before desktop plan", diag.SEM0124)
	}
	modules := parseStorageWriterModules(t, `
module desktop.prereq
use { CoreSpscConsumer } from machine.x86_64.core_link
use { CommittedAtomicGroup, PendingAtomicGroup, StorageAppendResult, StorageWriter } from storage.writer
data NeedsStorage {
    writer: StorageWriter
    durable: CoreSpscConsumer<CommittedAtomicGroup>
    pending: PendingAtomicGroup
    append_result: StorageAppendResult
    fn poll(self) {
        let next = self.durable.try_next()
    }
}`)
	index := mustBuildIndex(t, modules)
	mustCheck(t, index, modules)
}
```

```go
func TestDesktopLanguageBaselineRejectsModuleScopeFunction(t *testing.T) {
	_, ds := parseModuleForTest(t, "module desktop.bad\nfn bad() {}")
	for _, d := range ds {
		if d.Code == diag.PAR0002 {
			return
		}
	}
	t.Fatalf("diagnostics = %#v, want PAR0002", ds)
}
```

- [ ] **Step 1: Add prerequisite verifier tests**

Create both test files.

Run: `go test ./compiler/sem -run DesktopPrereq -v && go test ./compiler/parse -run DesktopLanguageBaseline -v`

Expected: PASS. If it fails with a missing storage substrate symbol, stop and merge/replay the storage worktree. If it fails for an unrelated parse/package error, stop and fix the prerequisite merge.

- [ ] **Step 2: Commit**

```bash
git diff --check
git add compiler/sem/desktop_prereq_test.go compiler/parse/desktop_language_baseline_test.go
git commit -m "test: verify realtime desktop prerequisites -Codex Automated"
```

### Task 1: Desktop Diagnostics And Deferred Scope

**Prerequisite:** Task 0.

**Files:**

- Modify: `compiler/diag/codes.go`
- Modify: `compiler/diag/diag_test.go`
- Modify: `docs/production-deferred-work.md`

**Description:** Reserve diagnostics for desktop field, renderer, display, effect, budget, provenance, and replay checks. Record the parts of the desktop design that remain outside this first executable plan.

**Acceptance Criteria:**

- `SEM0125` through `SEM0166` exist immediately after the storage substrate's `SEM0124` with the exact meanings listed in this task.
- The diagnostic test proves constants are unique.
- Deferred-work docs state that hostile third-party GUI apps, production GPU rendering, full text shaping, process isolation, multi-display native drivers, DOM compatibility, and 4K120 are outside this first desktop milestone.

**Code Examples:**

```go
const (
	SEM0125 = "SEM0125" // duplicate desktop identity
	SEM0126 = "SEM0126" // invalid field support or degenerate surface frame
	SEM0127 = "SEM0127" // sampled field lacks provenance, trust, format, or validity
	SEM0128 = "SEM0128" // pure function calls impure or frame-unsafe code
	SEM0129 = "SEM0129" // lane function uses unsupported v1 operation
	SEM0130 = "SEM0130" // frame_safe code may wait, block, or call may_wait code
	SEM0131 = "SEM0131" // renderer strategy is missing, undeclared, or unsupported
	SEM0132 = "SEM0132" // scope budget result is ignored
	SEM0133 = "SEM0133" // unknown support budget is exceeded
	SEM0134 = "SEM0134" // sampled byte budget is exceeded
	SEM0135 = "SEM0135" // display framebuffer authority cannot be forged
	SEM0136 = "SEM0136" // cadence policy is invalid for a visible scope
	SEM0137 = "SEM0137" // input target is not owned by exactly one scope
	SEM0138 = "SEM0138" // semantic node references missing field or input identity
	SEM0139 = "SEM0139" // duplicate display output identity
	SEM0140 = "SEM0140" // output color policy is incomplete
	SEM0141 = "SEM0141" // frame report omits required cost field
	SEM0142 = "SEM0142" // replay input omits required frame term
	SEM0143 = "SEM0143" // visible app participation state is invalid
	SEM0144 = "SEM0144" // late lane contains non-late-safe operation
	SEM0145 = "SEM0145" // bounded completion poll limit is absent or zero
	SEM0146 = "SEM0146" // snapshot fallback lacks deterministic reason
	SEM0147 = "SEM0147" // pixel provenance requested without retained frame input
	SEM0148 = "SEM0148" // vector renderer conflicts with interrupt save policy
	SEM0149 = "SEM0149" // foreground frame lane attempts async wait
	SEM0150 = "SEM0150" // sampled pixel format is unsupported by v1 renderer
	SEM0151 = "SEM0151" // glyph coverage cache key is incomplete
	SEM0152 = "SEM0152" // derived cache validity does not match source identity
	SEM0153 = "SEM0153" // 3D ray fallback is unbounded
	SEM0154 = "SEM0154" // semantic focus order is duplicated or cyclic
	SEM0155 = "SEM0155" // input capture owner is invalid
	SEM0156 = "SEM0156" // renderer tile dimensions are invalid
	SEM0157 = "SEM0157" // field count cap is missing or unenforced
	SEM0158 = "SEM0158" // display rate matching policy is invalid
	SEM0159 = "SEM0159" // unsupported text layout feature lacks explicit gate
	SEM0160 = "SEM0160" // desktop demo image lacks required visible surface
	SEM0161 = "SEM0161" // desktop authority is constructed outside boot-owned code
	SEM0162 = "SEM0162" // replay hash mismatch
	SEM0163 = "SEM0163" // virtual framebuffer fixture is invalid
	SEM0164 = "SEM0164" // GOP backend lacks fallback present policy
	SEM0165 = "SEM0165" // frame report latency terms are inconsistent
	SEM0166 = "SEM0166" // inspect mode exposes stale field provenance
)
```

- [ ] **Step 1: Add failing diagnostic test**

Add `TestDesktopDiagnosticCodesExist` to `compiler/diag/diag_test.go`.

Run: `go test ./compiler/diag -run TestDesktopDiagnosticCodesExist -v`

Expected: FAIL with undefined identifiers such as `diag.SEM0125`.

- [ ] **Step 2: Add diagnostic constants**

Add the constants immediately after `SEM0124`.

Run: `go test ./compiler/diag -run TestDesktopDiagnosticCodesExist -v`

Expected: PASS.

- [ ] **Step 3: Add deferred-work note**

Append a section titled `Realtime desktop beyond the first milestone` to `docs/production-deferred-work.md`.

Run: `rg -n "Realtime desktop beyond the first milestone|hostile third-party GUI apps|4K120" docs/production-deferred-work.md`

Expected: all three phrases are printed.

- [ ] **Step 4: Commit**

```bash
git diff --check
git add compiler/diag/codes.go compiler/diag/diag_test.go docs/production-deferred-work.md
git commit -m "docs: freeze realtime desktop diagnostics -Codex Automated"
```

### Task 2: Deterministic Framebuffer Harness

**Prerequisite:** Task 1.

**Files:**

- Create: `compiler/desktopfmt/types.go`
- Create: `compiler/desktopfmt/surface.go`
- Create: `compiler/desktopfmt/surface_test.go`

**Description:** Create the host-side framebuffer package used by renderer, replay, and pixel-hash tests.

**Acceptance Criteria:**

- `NewSurface(width, height, PixelFormatRGBA8)` allocates `width * height * 4` bytes.
- `Store`, `At`, and `Clear` bounds-check and return deterministic errors instead of panicking.
- `HashRGBA8` returns a stable SHA-256 hex string over raw pixels plus dimensions and format.

**Code Examples:**

```go
type RGBA8 struct {
	R uint8
	G uint8
	B uint8
	A uint8
}

type PixelFormat uint8

const PixelFormatRGBA8 PixelFormat = 1

type Surface struct {
	Width  int
	Height int
	Format PixelFormat
	Pixels []byte
}

func NewSurface(width, height int, format PixelFormat) Surface {
	return Surface{Width: width, Height: height, Format: format, Pixels: make([]byte, width*height*4)}
}

type SurfaceOps interface {
	Store(x, y int, px RGBA8) error
	At(x, y int) (RGBA8, error)
	Clear(px RGBA8) error
}
```

```go
func TestSurfaceHashIncludesDimensions(t *testing.T) {
	a := NewSurface(2, 2, PixelFormatRGBA8)
	b := NewSurface(4, 1, PixelFormatRGBA8)
	if HashRGBA8(a) == HashRGBA8(b) {
		t.Fatal("surface hash must include dimensions")
	}
}
```

- [ ] **Step 1: Add failing tests**

Create `compiler/desktopfmt/surface_test.go` with `TestSurfaceStoreAtClear` and `TestSurfaceHashIncludesDimensions`.

Run: `go test ./compiler/desktopfmt -run Surface -v`

Expected: FAIL because `compiler/desktopfmt` does not exist.

- [ ] **Step 2: Add implementation**

Create `types.go` and `surface.go` with the exact public names in the examples.

Run: `go test ./compiler/desktopfmt -run Surface -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add compiler/desktopfmt/types.go compiler/desktopfmt/surface.go compiler/desktopfmt/surface_test.go
git commit -m "test: add deterministic desktop framebuffer harness -Codex Automated"
```

### Task 3: Desktop Source Loading Helper

**Prerequisite:** Task 1.

**Files:**

- Modify: `compiler/sem/uefi_source_shape_test.go`
- Create: `compiler/sem/desktop_source_testutil_test.go`

**Description:** Add reusable source-shape helpers without requiring future desktop files to exist before their own tasks create them.

**Acceptance Criteria:**

- `parseDesktopModulesForTest(t, paths...)` appends only the task-owned desktop files passed by the caller to the UEFI module set.
- `parseFullDesktopModuleSet(t)` is not added until Task 10G, after all desktop source files exist.
- Missing task-owned files produce a clear test failure naming the missing path.
- This task documents one complete field-order assertion template for later source-shape tests to copy; the template test itself is added in Task 4 after `units.wrela` exists.

**Code Examples:**

```go
func parseDesktopModulesForTest(t *testing.T, desktopPaths ...string) []*ast.Module {
	t.Helper()
	paths := append([]string{}, uefiSourcePathsForTest()...)
	paths = append(paths, desktopPaths...)
	return parseRepoSourcePaths(t, paths...)
}

func parseRepoSourcePaths(t *testing.T, paths ...string) []*ast.Module {
	t.Helper()
	workdir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	repoRoot := filepath.Clean(filepath.Join(workdir, "..", ".."))
	files := make([]*source.File, 0, len(paths))
	for i, path := range paths {
		actual := path
		if !filepath.IsAbs(actual) {
			actual = filepath.Join(repoRoot, path)
		}
		raw, err := os.ReadFile(actual)
		if err != nil {
			t.Fatalf("read %s: %v", actual, err)
		}
		files = append(files, source.NewFile(source.FileID(i+1), actual, string(raw)))
	}
	modules, ds := parse.ParseGraph(source.Graph{Files: files})
	if len(ds) != 0 {
		t.Fatalf("parse diagnostics: %#v", ds)
	}
	return modules
}

func assertFieldOrder(t *testing.T, typ *Type, want ...string) {
	t.Helper()
	got := make([]string, 0, len(typ.Fields))
	for _, field := range typ.Fields {
		got = append(got, field.Name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s fields = %#v, want %#v", typ.Name, got, want)
	}
}
```

Task 4 source-shape tests should copy this full pattern after `units.wrela` exists:

```go

func TestDesktopUnitsFieldOrderTemplate(t *testing.T) {
	modules := parseDesktopModulesForTest(t, "wrela/desktop/units.wrela", "wrela/desktop/identity.wrela")
	index := mustBuildIndex(t, modules)
	checked := mustCheck(t, index, modules)
	rect := moduleType(t, checked.Index, "desktop.units", "RectFx")
	assertFieldOrder(t, rect, "x", "y", "width", "height")
	if fieldTypeName(t, rect, "width") != "Fx26_6" {
		t.Fatalf("RectFx.width = %s, want Fx26_6", fieldTypeName(t, rect, "width"))
	}
}
```

- [ ] **Step 1: Extract reusable UEFI source path helper**

Move the hard-coded path list inside `parseUEFIModuleSet` into `uefiSourcePathsForTest`.

Run: `go test ./compiler/sem -run TestUEFIPlatformBootServicesAndTransitionAsmShapes -v`

Expected: PASS.

- [ ] **Step 2: Add desktop helper test**

Create `TestDesktopSourceHelperLoadsOnlyRequestedPaths` in `compiler/sem/desktop_source_testutil_test.go`.

Run: `go test ./compiler/sem -run TestDesktopSourceHelperLoadsOnlyRequestedPaths -v`

Expected: FAIL because `parseDesktopModulesForTest` is not defined.

- [ ] **Step 3: Add `parseDesktopModulesForTest` and `assertFieldOrder`**

Add the helper exactly as shown above.

Run: `go test ./compiler/sem -run TestDesktopSourceHelperLoadsOnlyRequestedPaths -v`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git diff --check
git add compiler/sem/uefi_source_shape_test.go compiler/sem/desktop_source_testutil_test.go
git commit -m "test: add desktop source loading helper -Codex Automated"
```

### Task 3A: Wrela Idioms Anchor Test

**Prerequisite:** Tasks 0 and 3.

**Files:**

- Create: `compiler/sem/desktop_wrela_idiom_anchor_test.go`

**Description:** Add one baseline semantic test that proves the non-new Wrela language idioms used by desktop source contracts already parse and type-check: generic types, enum variants with named payloads, named constructor arguments, named method-call arguments, `Result.Ok(value = ...)`, and `match` destructuring. `frame_safe`, `pure`, `lane`, and `may_wait` are intentionally excluded here because Task 11 introduces those modifiers.

**Acceptance Criteria:**

- The test compiles a single in-memory module named `desktop_idiom_anchor`.
- The module uses `Slice<AnchorEvent>`, `Result<Unit, AnchorError>`, `Option.Some(value = event)`, `Result.Ok(value = Unit())`, and a method call with `value = ...`.
- The test also documents the existing source files that prove these idioms in repository code: `wrela/lang/core.wrela`, `wrela/machine/x86_64/executor_memory.wrela`, `compiler/sem/enum_test.go`, and `tests/e2e/fixtures/hello_ivshmem/program.wrela`.
- If this task fails after the storage substrate is merged, execution stops; do not edit desktop source contracts until the baseline language feature that failed is fixed in a separate prerequisite task.

**Code Examples:**

```go
func TestDesktopWrelaIdiomAnchorCompiles(t *testing.T) {
	src := `module desktop_idiom_anchor

data Unit {}
data AnchorError {}
data AnchorEvent { value: U64 }
data Slice<T> { address: U64 length: U64 }
enum Option<T> { None Some(value: T) }
enum Result<T, E> { Ok(value: T) Err(error: E) }

class AnchorQueue<T> {
    fn try_next(self) -> Option<T> { return Option.None() }
}

class AnchorSink {
    fn push(self, value: AnchorEvent) -> Result<Unit, AnchorError> {
        return Result.Ok(value = Unit())
    }
}

data AnchorUse {
    fn drain(self, queue: AnchorQueue<AnchorEvent>, sink: AnchorSink, events: Slice<AnchorEvent>) -> Result<Unit, AnchorError> {
        match queue.try_next() {
            Option.Some(value = event) => {
                match sink.push(value = event) {
                    Result.Ok(value = unit) => {}
                    Result.Err(error = err) => { return Result.Err(error = err) }
                }
            }
            Option.None => {}
        }
        return Result.Ok(value = Unit())
    }
}`
	modules := parseModulesForTest(t, src)
	index := mustBuildIndex(t, modules)
	mustCheck(t, index, modules)
}
```

- [ ] **Step 1: Add the idiom anchor test**

Run: `go test ./compiler/sem -run TestDesktopWrelaIdiomAnchorCompiles -v`

Expected: PASS. If it fails, the plan prerequisite is not met and desktop source work must not start.

- [ ] **Step 2: Commit**

```bash
git diff --check
git add compiler/sem/desktop_wrela_idiom_anchor_test.go
git commit -m "test: anchor desktop wrela source idioms -Codex Automated"
```

---

## 6. Phase 1: Desktop Source Contracts

**Description:** Create source-visible Wrela contracts for the desktop data model before renderer or demo code consumes them.

**Acceptance Criteria:**

- Every type in the design milestone has a v1 Wrela source home.
- Every visible field, input target, and semantic node belongs to exactly one scope.
- Sampled content carries provenance, trust, pixel format, color, buffer lifetime, and validity.
- Source-shape tests verify field order and key method names.

**Code Examples:**

```wrela
data Field {
    identity: FieldIdentity
    z: I64
    support: FieldSupport
    semantics: DistanceSemantics
    clip: Clip
    cache: CachePolicy
    source: FieldSource
    temporal: TemporalIdentity
    data: FieldData
    program: FieldProgram
}
```

### Task 4: Units, Geometry, And Identity Types

**Prerequisite:** Task 3.

**Files:**

- Create: `wrela/desktop/units.wrela`
- Create: `wrela/desktop/identity.wrela`
- Create: `compiler/sem/desktop_units_source_test.go`

**Description:** Add fixed-point geometry and stable identity types used by all later desktop modules.

**Acceptance Criteria:**

- `Fx26_6`, `Vec2Fx`, `Vec3Fx`, `RectFx`, `Bounds3Fx`, `Transform2D`, `SurfaceFrame`, and `Rational` exist.
- `SurfaceFrame` stores axes and scale only; no stored normal field is allowed.
- Identity types are single-field data records with `id: U64`.
- Source-shape tests verify the exact field order for `RectFx`, `SurfaceFrame`, and `FieldIdentity`.

**Code Examples:**

```wrela
module desktop.units

data Fx26_6 { raw: I64 }
data Vec2Fx { x: Fx26_6 y: Fx26_6 }
data Vec3Fx { x: Fx26_6 y: Fx26_6 z: Fx26_6 }
data RectFx { x: Fx26_6 y: Fx26_6 width: Fx26_6 height: Fx26_6 }
data Bounds3Fx { min: Vec3Fx max: Vec3Fx }
data Rational { numerator: U64 denominator: U64 }

data Transform2D {
    translate: Vec2Fx
    scale: Vec2Fx
}

data SurfaceFrame {
    origin: Vec3Fx
    x_axis: Vec3Fx
    y_axis: Vec3Fx
    scale: Vec2Fx
}
```

```wrela
module desktop.identity

data FieldIdentity { id: U64 }
data ScopeIdentity { id: U64 }
data InputIdentity { id: U64 }
data SemanticIdentity { id: U64 }
data DisplayIdentity { id: U64 }
data AppIdentity { id: U64 }
data FrameId { id: U64 }
data EventId { id: U64 }
data PendingOpId { id: U64 }
```

- [ ] **Step 1: Add failing source-shape test**

Add `TestDesktopUnitsAndIdentitiesShape` to `compiler/sem/desktop_units_source_test.go`.

Run: `go test ./compiler/sem -run TestDesktopUnitsAndIdentitiesShape -v`

Expected: FAIL with missing `wrela/desktop/units.wrela`.

- [ ] **Step 2: Add source files**

Create `units.wrela` and `identity.wrela` using the public names above.

Run: `go test ./compiler/sem -run TestDesktopUnitsAndIdentitiesShape -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add wrela/desktop/units.wrela wrela/desktop/identity.wrela compiler/sem/desktop_units_source_test.go
git commit -m "feat: add desktop units and identity contracts -Codex Automated"
```

### Task 5: Color And Display Contracts

**Prerequisite:** Task 4.

**Files:**

- Create: `wrela/desktop/color.wrela`
- Create: `wrela/desktop/display.wrela`
- Create: `compiler/sem/desktop_display_source_test.go`

**Description:** Add color policy and display backend contracts for deterministic virtual framebuffer and GOP presentation.

**Acceptance Criteria:**

- `Color` uses premultiplied 16-bit channels.
- `OutputColorPolicy` contains working space, output space, transfer, HDR policy, pixel format, scale filter, and subpixel text policy.
- `DisplayCapabilities` records modes, vblank, page flip, hardware cursor, hotplug, multi-output, and cadence confidence.
- `DisplayBackend` exposes outputs, capabilities, map_surface, and present methods.

**Code Examples:**

```wrela
module desktop.color

enum ColorSpace { SceneLinearSRGB DisplaySRGB }
enum TransferFunction { Linear SRGB }
enum HdrPolicy { SDR }
enum PixelFormat { RGBA8 BGRA8 }
enum ScaleFilter { Nearest Bilinear }
enum SubpixelTextPolicy { GrayscaleRGB }

data Color {
    r: U16
    g: U16
    b: U16
    a: U16
}

data OutputColorPolicy {
    working_space: ColorSpace
    output_space: ColorSpace
    transfer: TransferFunction
    hdr: HdrPolicy
    pixel_format: PixelFormat
    scale_filter: ScaleFilter
    subpixel_text: SubpixelTextPolicy
}
```

```wrela
module desktop.display

use { Fx26_6, Rational, RectFx } from desktop.units
use { DisplayIdentity } from desktop.identity
use { OutputColorPolicy, PixelFormat } from desktop.color
use { MutableBytes } from machine.x86_64.executor_memory

enum DisplayBackendKind { FirmwareFramebuffer DeterministicVirtualFramebuffer }
enum CadenceConfidence { Unknown MeasuredPresentDelta VblankEvent }

data DisplayMode { width: U64 height: U64 refresh: Rational }
data FrameClock { input_hz: U64 simulation_hz: U64 presentation_hz: U64 display_period_ns: U64 }
data SurfaceDesc { width: U64 height: U64 pixel_format: PixelFormat stride_bytes: U64 }
data Framebuffer { memory: MutableBytes desc: SurfaceDesc }
data PresentPolicy { allow_tearing: Bool desired_hz: U64 }
data PresentResult { presented: Bool missed: Bool elapsed_ns: U64 confidence: CadenceConfidence }

data DisplayCapabilities {
    mode0: DisplayMode
    mode_count: U64
    has_vblank_event: Bool
    has_page_flip: Bool
    has_hw_cursor: Bool
    has_hotplug: Bool
    supports_multiple_outputs: Bool
    cadence_confidence: CadenceConfidence
}

data DisplayOutput {
    identity: DisplayIdentity
    physical_rect: RectFx
    mode: DisplayMode
    clock: FrameClock
    scale: Fx26_6
    color: OutputColorPolicy
    framebuffer: Framebuffer
}

class DisplayBackend {
    identity: DisplayIdentity
    kind: DisplayBackendKind
    output0: DisplayOutput
    capabilities0: DisplayCapabilities

    fn outputs_count(self) -> U64 {
        return 1
    }

    fn capabilities(self, display: DisplayIdentity) -> DisplayCapabilities {
        return self.capabilities0
    }

    fn map_surface(self, display: DisplayIdentity, desc: SurfaceDesc) -> Framebuffer {
        return self.output0.framebuffer
    }

    fn present(self, display: DisplayIdentity, framebuffer: Framebuffer, policy: PresentPolicy) -> PresentResult {
        return PresentResult(presented = true, missed = false, elapsed_ns = 0, confidence = self.capabilities0.cadence_confidence)
    }
}
```

- [ ] **Step 1: Add failing source-shape test**

Add `TestDesktopDisplayAndColorShape`.

Run: `go test ./compiler/sem -run TestDesktopDisplayAndColorShape -v`

Expected: FAIL with missing color/display modules.

- [ ] **Step 2: Add source files**

Create `color.wrela` and `display.wrela`.

Run: `go test ./compiler/sem -run TestDesktopDisplayAndColorShape -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add wrela/desktop/color.wrela wrela/desktop/display.wrela compiler/sem/desktop_display_source_test.go
git commit -m "feat: add desktop color and display contracts -Codex Automated"
```

### Task 6: Field And Sampled Content Contracts

**Prerequisite:** Tasks 4 and 5.

**Files:**

- Create: `wrela/desktop/field.wrela`
- Create: `compiler/sem/desktop_field_source_test.go`

**Description:** Add the v1 field model, including analytic fields, sampled fields, derived cache fields, support regions, and fixed built-in field programs.

**Acceptance Criteria:**

- `FieldSupport.Unknown` exists but is countable by budget rules.
- `SampledContent` includes provenance, planes, color space, filter, trust, frame id, and validity.
- `FieldProgram` has only these v1 variants: `SolidRect`, `RoundedRect`, `BorderRect`, `GlyphCoverage`, `Caret`, `SampledRGBA`, `AnalyticSphere`.
- `Field` stores `program: FieldProgram` instead of a first-class function pointer.

**Code Examples:**

```wrela
module desktop.field

use { Color, ColorSpace, PixelFormat } from desktop.color
use { FieldIdentity } from desktop.identity
use { Bounds3Fx, Fx26_6, RectFx, SurfaceFrame, Vec2Fx } from desktop.units
use { Bytes, Slice } from machine.x86_64.executor_memory

enum DistanceSemantics { ExactSignedDistance ConservativeLowerBound CoverageOnly Opaque }
enum FieldSupport { Empty Rect(rect: RectFx) RoundedRect(rect: RectFx, radius: Fx26_6) Circle(center: Vec2Fx, radius: Fx26_6) Plane(surface: SurfaceFrame, shape: SurfaceSupport) Bounds3D(bounds: Bounds3Fx) Unknown }
enum SurfaceSupport { SurfaceRect(rect: RectFx) SurfaceRoundedRect(rect: RectFx, radius: Fx26_6) SurfaceCircle(center: Vec2Fx, radius: Fx26_6) }
enum ContentTrust { TrustedDesktopAsset TrustedDecodedMedia UntrustedRemotePixels }
enum ContentProvenance { EmbeddedDemoAsset DecodedImageFile Screenshot RemoteSurface }
enum SampleFilter { Nearest Bilinear }
enum BufferLifetime { StaticImage FrameOwned }
enum CachePolicy { NoCache TileCandidateCache LayerSurfaceCache GlyphCoverageCache ShadowCache }
enum FieldProgram { SolidRect RoundedRect BorderRect GlyphCoverage Caret SampledRGBA AnalyticSphere }

data CacheValidity { source_version: U64 transform_version: U64 valid: Bool }
data Clip { rect: RectFx enabled: Bool }
data FieldDependencySet { count: U64 }
data InvalidationCause { code: U64 }
data TemporalIdentity { stable_id: FieldIdentity version: U64 dependencies: FieldDependencySet invalidation: InvalidationCause }
data FieldValue { distance: I64 coverage: U16 }
data FieldData { rect: RectFx color: Color sampled: SampledContent radius: Fx26_6 }
data MediaFrameId { id: U64 }
data MediaBufferAuthority { id: U64 }
data MediaBufferView { authority: MediaBufferAuthority lifetime: BufferLifetime bytes: Bytes }
data SampledPlane { buffer: MediaBufferView plane: U64 width: U64 height: U64 stride_bytes: U64 pixel_format: PixelFormat decode_epoch: U64 }
data SampledContent { provenance: ContentProvenance planes: Slice<SampledPlane> color_space: ColorSpace filter: SampleFilter trust: ContentTrust frame_id: MediaFrameId validity: CacheValidity }

data FieldSource {
    kind: U64
    sampled: SampledContent
    parent: FieldIdentity
    validity: CacheValidity
}

data Field {
    identity: FieldIdentity
    z: I64
    support: FieldSupport
    semantics: DistanceSemantics
    clip: Clip
    cache: CachePolicy
    source: FieldSource
    temporal: TemporalIdentity
    data: FieldData
    program: FieldProgram
}
```

- [ ] **Step 1: Add failing source-shape test**

Add `TestDesktopFieldContractsShape`.

Run: `go test ./compiler/sem -run TestDesktopFieldContractsShape -v`

Expected: FAIL with missing `desktop.field`.

- [ ] **Step 2: Add field source**

Create `wrela/desktop/field.wrela` with all names in the code example and the final `Field` data shape.

Run: `go test ./compiler/sem -run TestDesktopFieldContractsShape -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add wrela/desktop/field.wrela compiler/sem/desktop_field_source_test.go
git commit -m "feat: add desktop field contracts -Codex Automated"
```

### Task 7: Frame Graph, Budget, And Cadence Contracts

**Prerequisite:** Task 6.

**Files:**

- Create: `wrela/desktop/frame.wrela`
- Create: `compiler/sem/desktop_frame_source_test.go`

**Description:** Add the scope-owned frame graph and mechanical budget lease surface.

**Acceptance Criteria:**

- `FieldScope` has identity, parent, transform, clip, budget, cadence, dependencies, invalidation, durable watermark, pending ops, fields, input targets, and semantic nodes.
- `FrameBudgetLease` stores policy and counters.
- `BudgetExhausted` is declared in `desktop.frame` and is the only error type returned by v1 scope writer budget methods.
- `ScopeWriter` methods return `Result<Unit, BudgetExhausted>` for field, input, and semantic emission.
- Cadence is separate from budget.

**Code Examples:**

```wrela
enum CadencePolicy {
    DisplayPaced
    OnDirty
    OnInputOnly
    FixedHz(hz: U64)
    MediaRate(frame_rate: Rational)
}

data FrameBudget {
    max_tick_ns: U64
    max_emit_ns: U64
    max_fields: U64
    max_unknown_supports: U64
    max_sampled_bytes_touched: U64
}

data FrameBudgetCounters {
    tick_ns: U64
    emit_ns: U64
    fields: U64
    unknown_supports: U64
    sampled_bytes_touched: U64
}

data FrameBudgetLease {
    policy: FrameBudget
    consumed: FrameBudgetCounters
}

data BudgetExhausted {
    code: U64
}
```

```wrela
class FieldScopeWriter {
    scope_id: ScopeIdentity
    budget: FrameBudgetLease

    fn push_field(self, field: Field) -> Result<Unit, BudgetExhausted> {
        if self.budget.consumed.fields == self.budget.policy.max_fields {
            return Result.Err(error = BudgetExhausted())
        }
        self.budget.consumed.fields = self.budget.consumed.fields + 1
        return Result.Ok(value = Unit())
    }
}
```

- [ ] **Step 1: Add failing source-shape test**

Add `TestDesktopFrameBudgetShape`.

Run: `go test ./compiler/sem -run TestDesktopFrameBudgetShape -v`

Expected: FAIL with missing `desktop.frame`.

- [ ] **Step 2: Add frame source**

Create `wrela/desktop/frame.wrela`.

Run: `go test ./compiler/sem -run TestDesktopFrameBudgetShape -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add wrela/desktop/frame.wrela compiler/sem/desktop_frame_source_test.go
git commit -m "feat: add desktop frame graph and budget contracts -Codex Automated"
```

### Task 8: Input And Semantic Contracts

**Prerequisite:** Task 7.

**Files:**

- Create: `wrela/desktop/input.wrela`
- Create: `wrela/desktop/semantics.wrela`
- Create: `compiler/sem/desktop_input_semantics_source_test.go`

**Description:** Add input targets, capture policy, hit results, and accessibility semantics emitted from the same visible source as fields.

**Acceptance Criteria:**

- `InputTarget` contains identity, z, support, space, capture, data, and event policy.
- Pointer capture is represented by explicit owner identity and active flag.
- `SemanticNode` contains role, name, value, actions, focus policy, bounds, and child identities.
- Tests prove semantic nodes and input targets use desktop identity types rather than raw `U64` fields.

**Code Examples:**

```wrela
enum InputSpace { Desktop2D SurfacePoint Ray3D }
enum CapturePolicy { None PointerUntilRelease }
enum HitKind { Miss Hit }
data PointerSample { id: U64 x: Fx26_6 y: Fx26_6 buttons: U64 time_ns: U64 }
data InputFrame { frame_sample_id: U64 pointer: PointerSample key_count: U64 }
data InputData { owner_scope: ScopeIdentity local_id: U64 }
data HitResult { kind: HitKind target: InputIdentity distance: Fx26_6 }
data InputTarget { identity: InputIdentity z: I64 support: FieldSupport space: InputSpace capture: CapturePolicy data: InputData }
```

```wrela
enum SemanticRole { Window TextBox Button Image ScrollView DesktopObject }
enum FocusPolicy { NotFocusable Focusable }
data TextAlternative { bytes: Bytes }
data SemanticValue { bytes: Bytes }
data SemanticActionList { count: U64 }
data SemanticNode { identity: SemanticIdentity field: FieldIdentity input: InputIdentity role: SemanticRole name: TextAlternative value: SemanticValue actions: SemanticActionList focus: FocusPolicy bounds: FieldSupport children: Slice<SemanticIdentity> }
```

- [ ] **Step 1: Add failing source-shape test**

Add `TestDesktopInputAndSemanticsShape`.

Run: `go test ./compiler/sem -run TestDesktopInputAndSemanticsShape -v`

Expected: FAIL with missing input/semantics modules.

- [ ] **Step 2: Add source files**

Create `input.wrela` and `semantics.wrela`.

Run: `go test ./compiler/sem -run TestDesktopInputAndSemanticsShape -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add wrela/desktop/input.wrela wrela/desktop/semantics.wrela compiler/sem/desktop_input_semantics_source_test.go
git commit -m "feat: add desktop input and semantic contracts -Codex Automated"
```

### Task 9: Text, Glyph, And Scroll Contracts

**Prerequisite:** Task 8.

**Files:**

- Create: `wrela/desktop/text.wrela`
- Create: `compiler/sem/desktop_text_source_test.go`

**Description:** Add v1 text layout contracts: ASCII monospace glyph instances, caret stops, selection, glyph coverage cache keys, and scroll view transforms.

**Acceptance Criteria:**

- `TextLine` stores text bytes, glyphs, caret stops, baseline, ascent, descent, selection, and caret.
- `GlyphCacheKey` contains glyph id, size, transform version, color policy id, display scale, color mode, and font version.
- `ScrollView` stores rect, content size, and scroll.
- `TextLineTools.hit_test` returns nearest caret by caret stops.

**Code Examples:**

```wrela
data RangeU64 { start: U64 end: U64 }
data GlyphInstance { glyph_id: U64 cluster: RangeU64 origin: Vec2Fx advance: Fx26_6 bounds: RectFx program: FieldProgram }
data GlyphCacheKey { glyph_id: U64 size_px: U64 transform_version: U64 color_policy_id: U64 display_scale_raw: I64 color_mode: U64 font_version: U64 }
data TextLine { text: Bytes glyphs: Slice<GlyphInstance> carets: Slice<Fx26_6> baseline: Fx26_6 ascent: Fx26_6 descent: Fx26_6 selection: RangeU64 caret: U64 }
data TextHit { caret: U64 }
data ScrollView { rect: RectFx content_size: Vec2Fx scroll: Vec2Fx }
```

```wrela
data TextLineTools {
    fn hit_test(self, line: TextLine, p: Vec2Fx) -> TextHit {
        let best = 0
        return TextHit(caret = best)
    }
}
```

The initial `TextLineTools.hit_test` source may return `0` until Task 18 wires the behavior mirror and Task 23 replaces it with the nearest-caret loop. The public signature is fixed in this task.

- [ ] **Step 1: Add failing source-shape test**

Add `TestDesktopTextShape`.

Run: `go test ./compiler/sem -run TestDesktopTextShape -v`

Expected: FAIL with missing `desktop.text`.

- [ ] **Step 2: Add text source**

Create `wrela/desktop/text.wrela`.

Run: `go test ./compiler/sem -run TestDesktopTextShape -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add wrela/desktop/text.wrela compiler/sem/desktop_text_source_test.go
git commit -m "feat: add desktop text contracts -Codex Automated"
```

### Task 10A: Frame Report Source Contract

**Prerequisite:** Tasks 5 through 9.

**Files:**

- Create: `wrela/desktop/reports.wrela`
- Create: `compiler/sem/desktop_reports_source_test.go`

**Description:** Add the exact Wrela frame-report data contract. This is the source of truth mirrored by Go in Task 17.

**Acceptance Criteria:**

- `FrameReportSummary` contains exactly the 18 fields shown below in the shown order.
- `MissedFrameReason` is an enum with `None`, `BudgetFields`, `BudgetUnknownSupport`, `BudgetSampledBytes`, `DisplayLate`, and `StorageBackpressure`.
- Source-shape test asserts field order and field types, including `missed_reason_id: U64`.
- The Go mirror in Task 17 must not add `Strategy string` or replace `missed_reason_id` with text.

**Code Examples:**

```wrela
module desktop.reports

use { FrameId } from desktop.identity

enum MissedFrameReason { None BudgetFields BudgetUnknownSupport BudgetSampledBytes DisplayLate StorageBackpressure }

data FrameReportSummary {
    frame: FrameId
    target_hz: U64
    actual_ns: U64
    missed: Bool
    field_count: U64
    tile_count: U64
    avg_candidates_x100: U64
    max_candidates_per_tile: U64
    glyph_fields: U64
    sampled_fields: U64
    sampled_bytes: U64
    semantic_nodes: U64
    strategy_id: U64
    lane_width: U64
    cache_hits: U64
    cache_misses: U64
    input_to_present_ns: U64
    missed_reason_id: U64
}
```

- [ ] **Step 1: Add failing report source-shape test**

Run: `go test ./compiler/sem -run TestDesktopReportsShape -v`

Expected: FAIL with missing `desktop.reports`.

- [ ] **Step 2: Add reports source**

Run: `go test ./compiler/sem -run TestDesktopReportsShape -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add wrela/desktop/reports.wrela compiler/sem/desktop_reports_source_test.go
git commit -m "feat: add desktop frame report contract -Codex Automated"
```

### Task 10B: Replay And Provenance Source Contract

**Prerequisite:** Task 10A.

**Files:**

- Create: `wrela/desktop/replay.wrela`
- Create: `compiler/sem/desktop_replay_source_test.go`

**Description:** Add deterministic replay input and pixel-provenance source contracts before the Go mirror implements behavior.

**Acceptance Criteria:**

- `ReplayFrameInput` contains durable state id, input sample id, completion watermark, monotonic time, and renderer strategy id.
- `PixelProvenance` contains field identity, scope identity, app identity, field program, semantic role, cache status, input sample id, and cost.
- `FrameReplayResult` contains hash, report, and inspected provenance.
- Source-shape test asserts `field_program: FieldProgram` and `semantic_role: SemanticRole`; Task 33 may not introduce new provenance fields.

**Code Examples:**

```wrela
module desktop.replay

use { Field, FieldProgram } from desktop.field
use { AppIdentity, EventId, FieldIdentity, FrameId, ScopeIdentity } from desktop.identity
use { FrameReportSummary } from desktop.reports
use { SemanticRole } from desktop.semantics
use { Bytes } from machine.x86_64.executor_memory

enum CacheStatus { NotCacheable Hit Miss }

data ReplayFrameInput {
    frame: FrameId
    durable_state_id: U64
    input_sample_id: U64
    completion_watermark: EventId
    monotonic_time_ns: U64
    strategy_id: U64
}

data PixelProvenance {
    field: FieldIdentity
    scope: ScopeIdentity
    app: AppIdentity
    field_program: FieldProgram
    semantic_role: SemanticRole
    cache: CacheStatus
    input_sample_id: U64
    cost_ns: U64
}

data FrameReplayResult {
    rgba_sha256: Bytes
    report: FrameReportSummary
    inspected: PixelProvenance
}
```

- [ ] **Step 1: Add failing replay source-shape test**

Run: `go test ./compiler/sem -run TestDesktopReplayShape -v`

Expected: FAIL with missing `desktop.replay`.

- [ ] **Step 2: Add replay source**

Run: `go test ./compiler/sem -run TestDesktopReplayShape -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add wrela/desktop/replay.wrela compiler/sem/desktop_replay_source_test.go
git commit -m "feat: add desktop replay provenance contract -Codex Automated"
```

### Task 10C: Renderer Strategy Source Contract

**Prerequisite:** Task 10A.

**Files:**

- Create: `wrela/desktop/renderer.wrela`
- Create: `compiler/sem/desktop_renderer_source_test.go`

**Description:** Add the v1 renderer strategy declarations without adding GPU, DOM, or arbitrary field function pointers.

**Acceptance Criteria:**

- `RendererStrategy` contains exactly `Scalar` and `Avx2Packets(width: U64)`.
- `FieldRenderer.primary` and `FieldRenderer.fallback` are `RendererStrategy`.
- `allow_gpu` exists and is always asserted false by source-shape tests for v1 fixtures.
- Source-shape test rejects or fails if any `Gpu`, `Dom`, or function-pointer field is added.

**Code Examples:**

```wrela
module desktop.renderer

enum RendererStrategy { Scalar Avx2Packets(width: U64) }

data FieldRenderer {
    primary: RendererStrategy
    fallback: RendererStrategy
    allow_gpu: Bool
}
```

- [ ] **Step 1: Add failing renderer source-shape test**

Run: `go test ./compiler/sem -run TestDesktopRendererShape -v`

Expected: FAIL with missing `desktop.renderer`.

- [ ] **Step 2: Add renderer source**

Run: `go test ./compiler/sem -run TestDesktopRendererShape -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add wrela/desktop/renderer.wrela compiler/sem/desktop_renderer_source_test.go
git commit -m "feat: add desktop renderer strategy contract -Codex Automated"
```

### Task 10D: Storage Boundary Source Contract

**Prerequisite:** Task 10A and the storage substrate merge verified by Task 0.

**Files:**

- Create: `wrela/desktop/storage_boundary.wrela`
- Create: `compiler/sem/desktop_storage_contract_source_test.go`

**Description:** Add the source-visible durability boundary names used later by the demo. This task declares the shape only; Task 25 adds frame-safe behavior and bounded polling.

**Acceptance Criteria:**

- `DesktopStorageBoundary.durable` is `CoreSpscConsumer<CommittedAtomicGroup>`.
- `poll_committed_groups(max_count: U64)` exists and returns the number of committed groups observed.
- No method in this task uses `frame_safe`, `may_wait`, `pure`, or `lane`; those modifiers are added after Task 11.
- Source-shape test asserts there is no `arm_wait` text in `wrela/desktop/storage_boundary.wrela`.

**Code Examples:**

```wrela
module desktop.storage_boundary

use { CoreSpscConsumer } from machine.x86_64.core_link
use { CommittedAtomicGroup, StorageAppendResult } from storage.writer

enum SaveVisualState { Saved Saving Unsaved Error }

data DesktopStorageBoundary {
    durable: CoreSpscConsumer<CommittedAtomicGroup>
    buffer_version: U64
    last_submitted_version: U64
    last_durable_version: U64
    failed: Bool

    fn poll_committed_groups(self, max_count: U64) -> U64 {
        return 0
    }

    fn try_append(self, buffer_version: U64) -> StorageAppendResult {
        return StorageAppendResult(accepted = false, first_event_id = 0, last_event_id = 0, open_batch_slots = 0, flush_requested = false, reject_code = 1114)
    }
}
```

- [ ] **Step 1: Add failing storage contract source-shape test**

Run: `go test ./compiler/sem -run TestDesktopStorageBoundaryContractShape -v`

Expected: FAIL with missing `desktop.storage_boundary`.

- [ ] **Step 2: Add storage boundary source**

Run: `go test ./compiler/sem -run TestDesktopStorageBoundaryContractShape -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add wrela/desktop/storage_boundary.wrela compiler/sem/desktop_storage_contract_source_test.go
git commit -m "feat: add desktop storage boundary contract -Codex Automated"
```

### Task 10E: Shell Source Contract

**Prerequisite:** Tasks 8 and 10A.

**Files:**

- Create: `wrela/desktop/shell.wrela`
- Create: `compiler/sem/desktop_shell_contract_source_test.go`

**Description:** Add the shell state and method names later filled by the runtime loop. This task defines previously missing `apply_input_now`, `emit_fields`, and `emit_cursor` names.

**Acceptance Criteria:**

- `VisibilityState` contains `Hidden`, `FullyOccluded`, `VisibleClean`, and `VisibleDirty`.
- `FocusState` is separate and contains `Unfocused` and `Focused`.
- `DesktopShell` defines `apply_input_now`, `emit_fields`, `emit_cursor`, and `desktop_frame_tick`.
- Source-shape test asserts `Focused` is not a `VisibilityState` variant.

**Code Examples:**

```wrela
module desktop.shell

use { FrameGraph } from desktop.frame
use { InputFrame } from desktop.input

enum VisibilityState { Hidden FullyOccluded VisibleClean VisibleDirty }
enum FocusState { Unfocused Focused }

data DesktopShell {
    visibility: VisibilityState
    focus: FocusState

    fn apply_input_now(self, input: InputFrame) {}
    fn emit_fields(self, frame: FrameGraph) {}
    fn emit_cursor(self, input: InputFrame, frame: FrameGraph) {}
    fn desktop_frame_tick(self, input: InputFrame, frame: FrameGraph) {}
}
```

- [ ] **Step 1: Add failing shell contract source-shape test**

Run: `go test ./compiler/sem -run TestDesktopShellContractShape -v`

Expected: FAIL with missing `desktop.shell`.

- [ ] **Step 2: Add shell source**

Run: `go test ./compiler/sem -run TestDesktopShellContractShape -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add wrela/desktop/shell.wrela compiler/sem/desktop_shell_contract_source_test.go
git commit -m "feat: add desktop shell contract -Codex Automated"
```

### Task 10F: Demo Source Contract

**Prerequisite:** Tasks 6, 9, 10C, and 10E.

**Files:**

- Create: `wrela/desktop/demo.wrela`
- Create: `compiler/sem/desktop_demo_contract_source_test.go`

**Description:** Add exact demo state names and the `validate_demo_surface` method that later pixel tests call.

**Acceptance Criteria:**

- `DemoElementFlags` has one boolean for background, text box, glyphs, selection, caret, sampled checker, sphere, semantic node, and storage state.
- `DesktopDemoScene.validate_demo_surface` returns true only when all flags are true.
- Demo text is fixed to the ASCII bytes for `Wrela desktop`.
- Source-shape test checks the method body contains all nine flag names.

**Code Examples:**

```wrela
module desktop.demo

use { Bytes } from machine.x86_64.executor_memory

data DemoElementFlags {
    background: Bool
    text_box: Bool
    glyphs: Bool
    selection: Bool
    caret: Bool
    sampled_checker: Bool
    sphere: Bool
    semantic_node: Bool
    storage_state: Bool
}

data DesktopDemoScene {
    title: Bytes
    flags: DemoElementFlags

    fn validate_demo_surface(self) -> Bool {
        if self.flags.background == false { return false }
        if self.flags.text_box == false { return false }
        if self.flags.glyphs == false { return false }
        if self.flags.selection == false { return false }
        if self.flags.caret == false { return false }
        if self.flags.sampled_checker == false { return false }
        if self.flags.sphere == false { return false }
        if self.flags.semantic_node == false { return false }
        if self.flags.storage_state == false { return false }
        return true
    }
}
```

- [ ] **Step 1: Add failing demo contract source-shape test**

Run: `go test ./compiler/sem -run TestDesktopDemoContractShape -v`

Expected: FAIL with missing `desktop.demo`.

- [ ] **Step 2: Add demo source**

Run: `go test ./compiler/sem -run TestDesktopDemoContractShape -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add wrela/desktop/demo.wrela compiler/sem/desktop_demo_contract_source_test.go
git commit -m "feat: add desktop demo contract -Codex Automated"
```

### Task 10G: Full Desktop Source Set Gate

**Prerequisite:** Tasks 10A through 10F.

**Files:**

- Create: `compiler/sem/desktop_full_source_test.go`

**Description:** Add the full desktop source loader only after every task-owned source module exists. This gate prevents early source-shape tests from failing because unrelated future modules are absent.

**Acceptance Criteria:**

- `parseFullDesktopModuleSet(t)` lists every `wrela/desktop/*.wrela` path created by Tasks 4 through 10F.
- `TestDesktopFullSourceSetTypeChecks` type-checks the complete source set.
- The helper is not used by Tasks 4 through 10F.
- A missing desktop module produces a failure naming the missing file path.

**Code Examples:**

```go
func parseFullDesktopModuleSet(t *testing.T) []*ast.Module {
	t.Helper()
	return parseDesktopModulesForTest(t,
		"wrela/desktop/units.wrela",
		"wrela/desktop/identity.wrela",
		"wrela/desktop/color.wrela",
		"wrela/desktop/display.wrela",
		"wrela/desktop/field.wrela",
		"wrela/desktop/frame.wrela",
		"wrela/desktop/input.wrela",
		"wrela/desktop/semantics.wrela",
		"wrela/desktop/text.wrela",
		"wrela/desktop/reports.wrela",
		"wrela/desktop/replay.wrela",
		"wrela/desktop/renderer.wrela",
		"wrela/desktop/storage_boundary.wrela",
		"wrela/desktop/shell.wrela",
		"wrela/desktop/demo.wrela",
	)
}
```

- [ ] **Step 1: Add full-source gate test**

Run: `go test ./compiler/sem -run TestDesktopFullSourceSetTypeChecks -v`

Expected: PASS because all desktop modules now exist.

- [ ] **Step 2: Commit**

```bash
git diff --check
git add compiler/sem/desktop_full_source_test.go
git commit -m "test: add full desktop source set gate -Codex Automated"
```

## 6.1 Phase 1 Source-Shape Test Bodies

**Description:** These are the exact Go test bodies required by Tasks 4 through 10G. Put each test in the task-owned file named by that task; do not collapse them into one shared source-shape file.

**Acceptance Criteria:**

- Each test uses `parseDesktopModulesForTest` with only the files owned by that task and their already-landed prerequisites.
- Each test asserts at least one field order, method name, or enum shape that would fail if the source contract drifted.
- Task 10G is the only test in this section allowed to use `parseFullDesktopModuleSet`.

**Code Examples:**

```go
func TestDesktopUnitsAndIdentitiesShape(t *testing.T) {
	modules := parseDesktopModulesForTest(t, "wrela/desktop/units.wrela", "wrela/desktop/identity.wrela")
	index := mustBuildIndex(t, modules)
	checked := mustCheck(t, index, modules)
	rect := moduleType(t, checked.Index, "desktop.units", "RectFx")
	assertFieldOrder(t, rect, "x", "y", "width", "height")
	identity := moduleType(t, checked.Index, "desktop.identity", "FieldIdentity")
	if fieldTypeName(t, identity, "scope") != "ScopeIdentity" {
		t.Fatalf("FieldIdentity.scope = %s, want ScopeIdentity", fieldTypeName(t, identity, "scope"))
	}
}

func TestDesktopDisplayAndColorShape(t *testing.T) {
	modules := parseDesktopModulesForTest(t,
		"wrela/desktop/units.wrela", "wrela/desktop/identity.wrela",
		"wrela/desktop/color.wrela", "wrela/desktop/display.wrela",
	)
	index := mustBuildIndex(t, modules)
	checked := mustCheck(t, index, modules)
	color := moduleType(t, checked.Index, "desktop.color", "OutputColorPolicy")
	assertFieldOrder(t, color, "working_space", "output_space", "transfer", "hdr", "pixel_format", "scale_filter", "subpixel_text")
	backend := moduleType(t, checked.Index, "desktop.display", "DisplayBackend")
	for _, name := range []string{"outputs_count", "capabilities", "map_surface", "present"} {
		if methodByName(t, backend, name) == nil { t.Fatalf("missing DisplayBackend.%s", name) }
	}
}

func TestDesktopFieldContractsShape(t *testing.T) {
	modules := parseDesktopModulesForTest(t,
		"wrela/desktop/units.wrela", "wrela/desktop/identity.wrela", "wrela/desktop/color.wrela",
		"wrela/desktop/field.wrela",
	)
	index := mustBuildIndex(t, modules)
	checked := mustCheck(t, index, modules)
	field := moduleType(t, checked.Index, "desktop.field", "Field")
	assertFieldOrder(t, field, "identity", "z", "support", "semantics", "clip", "cache", "source", "temporal", "data", "program")
	if fieldTypeName(t, field, "program") != "FieldProgram" { t.Fatalf("Field.program must be FieldProgram") }
}

func TestDesktopFrameBudgetShape(t *testing.T) {
	modules := parseDesktopModulesForTest(t,
		"wrela/desktop/units.wrela", "wrela/desktop/identity.wrela", "wrela/desktop/color.wrela",
		"wrela/desktop/field.wrela", "wrela/desktop/frame.wrela",
	)
	index := mustBuildIndex(t, modules)
	checked := mustCheck(t, index, modules)
	writer := moduleType(t, checked.Index, "desktop.frame", "FieldScopeWriter")
	push := methodByName(t, writer, "push_field")
	if push.Return == nil || push.Return.Display() != "Result<Unit, BudgetExhausted>" {
		t.Fatalf("push_field return = %#v, want Result<Unit, BudgetExhausted>", push.Return)
	}
}

func TestDesktopInputAndSemanticsShape(t *testing.T) {
	modules := parseDesktopModulesForTest(t,
		"wrela/desktop/units.wrela", "wrela/desktop/identity.wrela", "wrela/desktop/color.wrela",
		"wrela/desktop/field.wrela", "wrela/desktop/input.wrela", "wrela/desktop/semantics.wrela",
	)
	index := mustBuildIndex(t, modules)
	checked := mustCheck(t, index, modules)
	target := moduleType(t, checked.Index, "desktop.input", "InputTarget")
	if fieldTypeName(t, target, "identity") != "InputIdentity" { t.Fatalf("InputTarget.identity must be InputIdentity") }
	node := moduleType(t, checked.Index, "desktop.semantics", "SemanticNode")
	if fieldTypeName(t, node, "role") != "SemanticRole" { t.Fatalf("SemanticNode.role must be SemanticRole") }
}

func TestDesktopTextShape(t *testing.T) {
	modules := parseDesktopModulesForTest(t,
		"wrela/desktop/units.wrela", "wrela/desktop/identity.wrela", "wrela/desktop/color.wrela",
		"wrela/desktop/field.wrela", "wrela/desktop/text.wrela",
	)
	index := mustBuildIndex(t, modules)
	checked := mustCheck(t, index, modules)
	line := moduleType(t, checked.Index, "desktop.text", "TextLine")
	assertFieldOrder(t, line, "text", "glyphs", "carets", "baseline", "ascent", "descent", "selection", "caret")
	tools := moduleType(t, checked.Index, "desktop.text", "TextLineTools")
	_ = methodByName(t, tools, "hit_test")
}

func TestDesktopReportsShape(t *testing.T) {
	modules := parseDesktopModulesForTest(t, "wrela/desktop/identity.wrela", "wrela/desktop/reports.wrela")
	index := mustBuildIndex(t, modules)
	checked := mustCheck(t, index, modules)
	report := moduleType(t, checked.Index, "desktop.reports", "FrameReportSummary")
	assertFieldOrder(t, report, "frame", "target_hz", "actual_ns", "missed", "field_count", "tile_count", "avg_candidates_x100", "max_candidates_per_tile", "glyph_fields", "sampled_fields", "sampled_bytes", "semantic_nodes", "strategy_id", "lane_width", "cache_hits", "cache_misses", "input_to_present_ns", "missed_reason_id")
}

func TestDesktopReplayShape(t *testing.T) {
	modules := parseDesktopModulesForTest(t,
		"wrela/desktop/identity.wrela", "wrela/desktop/color.wrela", "wrela/desktop/units.wrela",
		"wrela/desktop/field.wrela", "wrela/desktop/semantics.wrela", "wrela/desktop/reports.wrela", "wrela/desktop/replay.wrela",
	)
	index := mustBuildIndex(t, modules)
	checked := mustCheck(t, index, modules)
	prov := moduleType(t, checked.Index, "desktop.replay", "PixelProvenance")
	if fieldTypeName(t, prov, "field_program") != "FieldProgram" { t.Fatalf("PixelProvenance.field_program must be FieldProgram") }
	if fieldTypeName(t, prov, "semantic_role") != "SemanticRole" { t.Fatalf("PixelProvenance.semantic_role must be SemanticRole") }
}

func TestDesktopRendererShape(t *testing.T) {
	modules := parseDesktopModulesForTest(t, "wrela/desktop/renderer.wrela")
	index := mustBuildIndex(t, modules)
	checked := mustCheck(t, index, modules)
	renderer := moduleType(t, checked.Index, "desktop.renderer", "FieldRenderer")
	assertFieldOrder(t, renderer, "primary", "fallback", "allow_gpu")
}

func TestDesktopStorageBoundaryContractShape(t *testing.T) {
	modules := parseDesktopModulesForTest(t,
		"wrela/machine/x86_64/core_link.wrela", "wrela/machine/x86_64/nvme.wrela",
		"wrela/storage/blob.wrela", "wrela/storage/format.wrela", "wrela/storage/stream.wrela", "wrela/storage/writer.wrela",
		"wrela/desktop/storage_boundary.wrela",
	)
	index := mustBuildIndex(t, modules)
	checked := mustCheck(t, index, modules)
	storage := moduleType(t, checked.Index, "desktop.storage_boundary", "DesktopStorageBoundary")
	poll := methodByName(t, storage, "poll_committed_groups")
	if poll.Return == nil || poll.Return.Display() != "U64" { t.Fatalf("poll return = %#v, want U64", poll.Return) }
}

func TestDesktopShellContractShape(t *testing.T) {
	modules := parseDesktopModulesForTest(t,
		"wrela/desktop/units.wrela", "wrela/desktop/identity.wrela", "wrela/desktop/color.wrela",
		"wrela/desktop/field.wrela", "wrela/desktop/frame.wrela", "wrela/desktop/input.wrela", "wrela/desktop/shell.wrela",
	)
	index := mustBuildIndex(t, modules)
	checked := mustCheck(t, index, modules)
	shell := moduleType(t, checked.Index, "desktop.shell", "DesktopShell")
	for _, name := range []string{"apply_input_now", "emit_fields", "emit_cursor", "desktop_frame_tick"} {
		if methodByName(t, shell, name) == nil { t.Fatalf("missing DesktopShell.%s", name) }
	}
}

func TestDesktopDemoContractShape(t *testing.T) {
	modules := parseDesktopModulesForTest(t, "wrela/desktop/demo.wrela")
	index := mustBuildIndex(t, modules)
	checked := mustCheck(t, index, modules)
	scene := moduleType(t, checked.Index, "desktop.demo", "DesktopDemoScene")
	_ = methodByName(t, scene, "validate_demo_surface")
}

func TestDesktopFullSourceSetTypeChecks(t *testing.T) {
	modules := parseFullDesktopModuleSet(t)
	index := mustBuildIndex(t, modules)
	mustCheck(t, index, modules)
}
```

---

## 7. Phase 2: Effects, Lane Markers, And Desktop Authority

**Description:** Add just enough language checking to make frame-safety and pure field contracts enforceable. The syntax is narrow and metadata-only for this milestone; built-in field programs keep renderer dispatch concrete.

**Acceptance Criteria:**

- Parser accepts `pure`, `lane`, `frame_safe`, and `may_wait` according to the exact grammar in Task 11.
- Semantic checker rejects `may_wait` calls from `frame_safe` code.
- Semantic checker rejects impure calls from `pure` functions.
- Desktop framebuffer, input, sampled media, and scope writer authorities cannot be forged outside boot-owned or desktop-owned modules.

**Code Examples:**

```wrela
data GlyphCoverageProgram {
    pure lane frame_safe fn eval(self, data: FieldData) -> FieldValue {
        return FieldValue(distance = 0)
    }
}

data StorageWorker {
    may_wait fn loop(self) -> never {
        while true {
            self.wait_source.arm_wait()
        }
    }
}
```

### Task 11: Parse `pure`, `lane`, `frame_safe`, And `may_wait`

**Prerequisite:** Task 1.

**Files:**

- Modify: `compiler/lex/token.go`
- Modify: `compiler/lex/lexer.go`
- Modify: `compiler/lex/lexer_test.go`
- Modify: `compiler/ast/ast.go`
- Modify: `compiler/ast/ast_test.go`
- Modify: `compiler/parse/parser.go`
- Modify: `compiler/parse/parser_test.go`

**Description:** Add method modifiers without changing expression syntax or adding first-class function types.

**Acceptance Criteria:**

- Valid modifier grammar is `MethodPrefix = "asm" "fn" | "start" "fn" | "may_wait" "fn" | "frame_safe" "fn" | "pure" ["lane"] ["frame_safe"] "fn" | "fn"`.
- `asm fn` and `start fn` do not compose with `pure`, `lane`, `frame_safe`, or `may_wait` in this plan.
- `pure may_wait fn`, `lane fn`, `frame_safe pure fn`, `pure frame_safe lane fn`, and `may_wait frame_safe fn` are parse errors.
- AST `MethodDecl` exposes `IsPure`, `IsLane`, `Effect` where `Effect` is `""`, `"frame_safe"`, or `"may_wait"`.

**Code Examples:**

```go
type MethodDecl struct {
	Name     string
	IsAsm    bool
	IsStart  bool
	IsPure   bool
	IsLane   bool
	Effect   string
	Params   []Param
	Return   TypeRef
	Body     []Stmt
}
```

```go
func TestParsePureLaneFrameSafeMethod(t *testing.T) {
	mod, ds := parseModuleForTest(t, `module desktop.effect_parse
data D {
  pure lane frame_safe fn eval(self) -> U64 { return 0 }
}`)
	if len(ds) != 0 {
		t.Fatalf("parse diagnostics = %#v", ds)
	}
	method := mod.Decls[0].(*ast.DataDecl).Methods[0]
	if !method.IsPure || !method.IsLane || method.Effect != "frame_safe" {
		t.Fatalf("method modifiers = %#v", method)
	}
}
```

- [ ] **Step 1: Add failing lexer/parser tests**

Add tests for valid `frame_safe fn`, valid `pure lane frame_safe fn`, valid `may_wait fn`, and invalid `pure may_wait fn`, `lane fn`, and `frame_safe pure fn`.

Run: `go test ./compiler/lex ./compiler/ast ./compiler/parse -run 'Pure|Lane|FrameSafe|MayWait' -v`

Expected: FAIL with unknown keywords or parse diagnostics.

- [ ] **Step 2: Add tokens and AST fields**

Add keyword tokens and method metadata fields.

Run: `go test ./compiler/lex ./compiler/ast -run 'Pure|Lane|FrameSafe|MayWait' -v`

Expected: lexer and AST tests PASS; parser test may still fail.

- [ ] **Step 3: Update parser**

Teach `parseMethodDecl` to read the allowed modifier sequences.

Run: `go test ./compiler/parse -run 'Pure|Lane|FrameSafe|MayWait' -v`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git diff --check
git add compiler/lex/token.go compiler/lex/lexer.go compiler/lex/lexer_test.go compiler/ast/ast.go compiler/ast/ast_test.go compiler/parse/parser.go compiler/parse/parser_test.go
git commit -m "feat: parse desktop field effect modifiers -Codex Automated"
```

### Task 12A: Preserve Method Effect Metadata In Sem

**Prerequisite:** Task 11.

**Files:**

- Modify: `compiler/sem/types.go`
- Modify: `compiler/sem/check.go`
- Create: `compiler/sem/effects_test.go`

**Description:** Copy parser method modifiers into semantic method metadata without enforcing behavior yet.

**Acceptance Criteria:**

- `sem.Method` stores `IsPure`, `IsLane`, and `Effect`.
- `Effect` is exactly `""`, `"frame_safe"`, or `"may_wait"`.
- Existing method lookup helpers can read the metadata from checked modules.
- No diagnostics are added by this task.

**Code Examples:**

```go
type Method struct {
	Name   string
	Return *Type
	IsPure bool
	IsLane bool
	Effect string
}
```

```go
func TestSemMethodPreservesEffectMetadata(t *testing.T) {
	modules := parseModulesForTest(t, `module sem.desktop_metadata
data Eval {
  pure lane frame_safe fn eval(self) -> U64 { return 0 }
}`)
	index := mustBuildIndex(t, modules)
	checked := mustCheck(t, index, modules)
	evalType := moduleType(t, checked.Index, "sem.desktop_metadata", "Eval")
	method := methodByName(t, evalType, "eval")
	if !method.IsPure || !method.IsLane || method.Effect != "frame_safe" {
		t.Fatalf("method metadata = %#v", method)
	}
}
```

- [ ] **Step 1: Add failing metadata test**

Run: `go test ./compiler/sem -run TestSemMethodPreservesEffectMetadata -v`

Expected: FAIL because semantic methods do not expose modifier metadata.

- [ ] **Step 2: Store metadata**

Run: `go test ./compiler/sem -run TestSemMethodPreservesEffectMetadata -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add compiler/sem/types.go compiler/sem/check.go compiler/sem/effects_test.go
git commit -m "feat: preserve semantic method effect metadata -Codex Automated"
```

### Task 12B: Reject `may_wait` Calls From Frame-Safe Methods

**Prerequisite:** Task 12A.

**Files:**

- Create: `compiler/sem/effects.go`
- Modify: `compiler/sem/effects_test.go`
- Modify: `compiler/sem/check.go`

**Description:** Add the first behavior check: `frame_safe` methods cannot call `may_wait` methods. This task does not check pure or lane restrictions.

**Acceptance Criteria:**

- `frame_safe` methods may call other `frame_safe` methods.
- `frame_safe` methods may call effectless methods only when `isKnownNonWaitingMethod` returns true.
- `frame_safe` methods reject calls to `may_wait` methods with `SEM0130`.
- The checker uses resolved receiver type and method metadata, not source-text grep.

**Code Examples:**

```go
func (c *checker) checkFrameSafeCall(ctx effectContext, call *ast.CallExpr, target *Method) {
	if ctx.Effect != "frame_safe" {
		return
	}
	if target.Effect == "may_wait" {
		c.error(call.SpanV, diag.SEM0130, "frame_safe method cannot call may_wait method")
		return
	}
	if target.Effect == "" && !isKnownNonWaitingMethod(target) {
		c.error(call.SpanV, diag.SEM0130, "frame_safe method can call only frame_safe or known non-waiting methods")
	}
}
```

```go
func TestFrameSafeRejectsMayWaitCall(t *testing.T) {
	_, ds := typeDiagsForModules(t, `module sem.desktop_effects
data Storage { may_wait fn wait(self) {} }
data App {
  frame_safe fn tick(self, storage: Storage) {
    storage.wait()
  }
}`)
	if !hasCode(ds, diag.SEM0130) {
		t.Fatalf("diagnostics = %#v, want SEM0130", ds)
	}
}
```

- [ ] **Step 1: Add failing frame-safe call tests**

Run: `go test ./compiler/sem -run 'FrameSafeRejectsMayWait|FrameSafeAllowsFrameSafe' -v`

Expected: FAIL because the effect checker does not exist.

- [ ] **Step 2: Implement frame-safe call check**

Run: `go test ./compiler/sem -run 'FrameSafeRejectsMayWait|FrameSafeAllowsFrameSafe' -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add compiler/sem/effects.go compiler/sem/effects_test.go compiler/sem/check.go
git commit -m "feat: reject waiting calls from frame-safe desktop code -Codex Automated"
```

### Task 12C: Reject Impure Operations From Pure Methods

**Prerequisite:** Task 12B.

**Files:**

- Modify: `compiler/sem/effects.go`
- Modify: `compiler/sem/effects_test.go`

**Description:** Add pure-method restrictions as a separate checker pass so field evaluators are deterministic and side-effect free.

**Acceptance Criteria:**

- `pure` methods may call only pure methods.
- `pure` methods reject assignment to `self.*` or any field expression with `SEM0128`.
- `pure` methods reject calls whose resolved method name is `arm_wait`, `wait`, `try_publish`, `reserve`, `reserve_array`, `set`, or `fill` unless the called method is also marked pure.
- `pure` methods may construct data values and return enum variants such as `Result.Ok(value = Unit())`.

**Code Examples:**

```go
var impureMethodNames = map[string]bool{
	"arm_wait": true,
	"wait": true,
	"try_publish": true,
	"reserve": true,
	"reserve_array": true,
	"set": true,
	"fill": true,
}

func TestPureRejectsFieldAssignment(t *testing.T) {
	_, ds := typeDiagsForModules(t, `module sem.desktop_pure
data Eval {
  value: U64
  pure fn bad(self) -> U64 {
    self.value = 1
    return self.value
  }
}`)
	if !hasCode(ds, diag.SEM0128) {
		t.Fatalf("diagnostics = %#v, want SEM0128", ds)
	}
}
```

- [ ] **Step 1: Add failing pure tests**

Run: `go test ./compiler/sem -run 'PureRejects|PureAllows' -v`

Expected: FAIL because pure body restrictions do not exist.

- [ ] **Step 2: Implement pure checks**

Run: `go test ./compiler/sem -run 'PureRejects|PureAllows' -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add compiler/sem/effects.go compiler/sem/effects_test.go
git commit -m "feat: reject impure operations from pure desktop methods -Codex Automated"
```

### Task 12D: Reject Non-Lane Constructs From Lane Methods

**Prerequisite:** Task 12C.

**Files:**

- Modify: `compiler/sem/effects.go`
- Modify: `compiler/sem/effects_test.go`

**Description:** Keep v1 lane methods small enough to lower into scalar and AVX2 field-program packets.

**Acceptance Criteria:**

- `lane` methods reject `while`, `for`, `with`, and `match` with `SEM0129`.
- `lane` methods reject calls to non-lane methods with `SEM0129`.
- `lane` methods may use arithmetic expressions, local `let` bindings, data construction, and returns.
- Tests cover one positive pure lane method and one negative fixture for each rejected statement kind.

**Code Examples:**

```go
func TestLaneRejectsWhileLoop(t *testing.T) {
	_, ds := typeDiagsForModules(t, `module sem.desktop_lane
data Eval {
  pure lane frame_safe fn bad(self) -> U64 {
    while true { return 1 }
    return 0
  }
}`)
	if !hasCode(ds, diag.SEM0129) {
		t.Fatalf("diagnostics = %#v, want SEM0129", ds)
	}
}
```

- [ ] **Step 1: Add failing lane tests**

Run: `go test ./compiler/sem -run 'LaneRejects|LaneAllows' -v`

Expected: FAIL because lane restrictions do not exist.

- [ ] **Step 2: Implement lane checks**

Run: `go test ./compiler/sem -run 'LaneRejects|LaneAllows' -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add compiler/sem/effects.go compiler/sem/effects_test.go
git commit -m "feat: enforce desktop lane method restrictions -Codex Automated"
```

### Task 12E: Known Non-Waiting Method Allowlist

**Prerequisite:** Task 12D.

**Files:**

- Modify: `compiler/sem/effects.go`
- Modify: `compiler/sem/effects_test.go`

**Description:** Add the explicit allowlist for effectless methods that frame-safe code may call. Keeping the list data-driven prevents juniors from guessing whether an effectless method is safe.

**Acceptance Criteria:**

- `knownNonWaitingMethods` is keyed by fully-qualified receiver type plus method name.
- Initial allowlist contains only `desktop.frame.FrameGraph.begin_scope`, `desktop.frame.FieldScopeWriter.push_field`, `desktop.frame.FieldScopeWriter.push_input_target`, `desktop.frame.FieldScopeWriter.push_semantic_node`, `desktop.frame.FieldScopeWriter.charge_sampled_bytes`, `desktop.shell.DesktopShell.apply_input_now`, `desktop.shell.DesktopShell.emit_fields`, `desktop.shell.DesktopShell.emit_cursor`, `desktop.shell.DesktopShell.sample_input`, `desktop.shell.DesktopShell.begin_frame`, `desktop.shell.DesktopShell.tick_visible_due_apps`, `desktop.shell.DesktopShell.render_due_outputs`, `desktop.shell.DesktopShell.present_due_outputs`, `desktop.shell.DesktopShell.publish_reports`, `desktop.shell.DesktopShell.emit_hover_highlight`, `desktop.shell.DesktopShell.emit_drag_preview`, and `desktop.shell.DesktopShell.emit_scroll_feedback`.
- Any missing allowlist entry produces `SEM0130` from frame-safe callers.
- Tests assert both allow and reject behavior.

**Code Examples:**

```go
var knownNonWaitingMethods = map[string]bool{
	"desktop.frame.FrameGraph.begin_scope": true,
	"desktop.frame.FieldScopeWriter.push_field": true,
	"desktop.frame.FieldScopeWriter.push_input_target": true,
	"desktop.frame.FieldScopeWriter.push_semantic_node": true,
	"desktop.frame.FieldScopeWriter.charge_sampled_bytes": true,
	"desktop.shell.DesktopShell.apply_input_now": true,
	"desktop.shell.DesktopShell.emit_fields": true,
	"desktop.shell.DesktopShell.emit_cursor": true,
	"desktop.shell.DesktopShell.sample_input": true,
	"desktop.shell.DesktopShell.begin_frame": true,
	"desktop.shell.DesktopShell.tick_visible_due_apps": true,
	"desktop.shell.DesktopShell.render_due_outputs": true,
	"desktop.shell.DesktopShell.present_due_outputs": true,
	"desktop.shell.DesktopShell.publish_reports": true,
	"desktop.shell.DesktopShell.emit_hover_highlight": true,
	"desktop.shell.DesktopShell.emit_drag_preview": true,
	"desktop.shell.DesktopShell.emit_scroll_feedback": true,
}

func knownNonWaitingKey(receiver *Type, method *Method) string {
	return qualifiedTypeName(receiver) + "." + method.Name
}
```

- [ ] **Step 1: Add failing allowlist tests**

Run: `go test ./compiler/sem -run 'KnownNonWaiting|FrameSafeRejectsUnknownEffectless' -v`

Expected: FAIL because the allowlist is absent.

- [ ] **Step 2: Implement allowlist**

Run: `go test ./compiler/sem -run 'KnownNonWaiting|FrameSafeRejectsUnknownEffectless' -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add compiler/sem/effects.go compiler/sem/effects_test.go
git commit -m "feat: add desktop frame-safe non-waiting allowlist -Codex Automated"
```

### Task 13A: Desktop Authority Construction Checks

**Prerequisite:** Tasks 10G and 12E.

**Files:**

- Create: `compiler/sem/desktop_authority_test.go`
- Modify: `compiler/sem/check.go`
- Create: `tests/fixtures/negative/desktop_forged_framebuffer.wrela`

**Description:** Reject direct construction of desktop authority-bearing types from user modules.

**Acceptance Criteria:**

- User modules outside `desktop.*`, `platform.uefi.*`, and `machine.x86_64.*` cannot construct `Framebuffer`, `MediaBufferAuthority`, or `FieldScopeWriter` directly.
- Rejection uses `SEM0161`.
- Construction from `desktop.display`, `desktop.field`, and UEFI delegated hardware transition code remains allowed.
- Negative fixture includes `// expect: SEM0161 desktop authority type cannot be constructed here`.

**Code Examples:**

```wrela
module bad.desktop

use { PixelFormat } from desktop.color
use { Framebuffer, SurfaceDesc } from desktop.display
use { MutableBytes } from machine.x86_64.executor_memory

data Bad {
    fn forge(self, bytes: MutableBytes) -> Framebuffer {
        return Framebuffer(memory = bytes, desc = SurfaceDesc(width = 1, height = 1, pixel_format = PixelFormat.RGBA8, stride_bytes = 4))
    }
}
```

```go
func desktopTypeDiagsForModules(t *testing.T, source string) (*CheckedProgram, []diag.Diagnostic) {
	t.Helper()
	modules := append(parseFullDesktopModuleSet(t), parseModulesForTest(t, source)...)
	index, ds := BuildIndex(modules)
	if len(ds) != 0 && (len(ds) != 1 || ds[0].Code != diag.SEM0003) {
		t.Fatalf("index diagnostics: %#v", ds)
	}
	return Check(index, modules)
}

func TestDesktopRejectsForgedFramebuffer(t *testing.T) {
	_, ds := desktopTypeDiagsForModules(t, badDesktopFramebufferSource)
	if !hasCode(ds, diag.SEM0161) {
		t.Fatalf("diagnostics = %#v, want SEM0161", ds)
	}
}
```

- [ ] **Step 1: Add failing authority tests and fixture**

Run: `go test ./compiler/sem -run TestDesktopRejectsForgedFramebuffer -v`

Expected: FAIL because authority checks do not exist.

- [ ] **Step 2: Implement authority checks**

Run: `go test ./compiler/sem -run TestDesktopRejectsForgedFramebuffer -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add compiler/sem/desktop_authority_test.go compiler/sem/check.go tests/fixtures/negative/desktop_forged_framebuffer.wrela
git commit -m "feat: reject forged desktop authority types -Codex Automated"
```

### Task 13B: Budget And Backpressure Result Observation

**Prerequisite:** Task 13A.

**Files:**

- Modify: `compiler/sem/desktop_authority_test.go`
- Modify: `compiler/sem/check.go`
- Create: `tests/fixtures/negative/desktop_ignored_budget_result.wrela`

**Description:** Reject foreground desktop code that drops `Result<Unit, BudgetExhausted>` from scope writer methods.

**Acceptance Criteria:**

- Bare expression statements calling `push_field`, `push_input_target`, `push_semantic_node`, or `charge_sampled_bytes` are rejected with `SEM0132`.
- The same calls are allowed when used as `return scope.push_field(...)`, assigned to a local, or matched immediately.
- The check uses resolved method and return type, not only method names.
- Negative fixture includes `// expect: SEM0132 budget result must be observed`.

**Code Examples:**

```go
func TestDesktopBudgetResultMustBeObserved(t *testing.T) {
	_, ds := desktopTypeDiagsForModules(t, `module sem.desktop_budget_observe
use { FieldScopeWriter } from desktop.frame
use { Field } from desktop.field
data App {
  frame_safe fn emit(self, scope: FieldScopeWriter, field: Field) {
    scope.push_field(field = field)
  }
}`)
	if !hasCode(ds, diag.SEM0132) {
		t.Fatalf("diagnostics = %#v, want SEM0132", ds)
	}
}
```

- [ ] **Step 1: Add failing result-observation tests**

Run: `go test ./compiler/sem -run 'BudgetResultMustBeObserved|BudgetResultMayBeMatched' -v`

Expected: FAIL because ignored budget results are not checked.

- [ ] **Step 2: Implement observation check**

Run: `go test ./compiler/sem -run 'BudgetResultMustBeObserved|BudgetResultMayBeMatched' -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add compiler/sem/desktop_authority_test.go compiler/sem/check.go tests/fixtures/negative/desktop_ignored_budget_result.wrela
git commit -m "feat: require observed desktop budget results -Codex Automated"
```

### Task 13C: Bounded Completion Poll Checks

**Prerequisite:** Tasks 10D, 12E, and 13B.

**Files:**

- Modify: `compiler/sem/desktop_authority_test.go`
- Modify: `compiler/sem/effects.go`
- Modify: `compiler/sem/check.go`
- Create: `tests/fixtures/negative/desktop_foreground_wait.wrela`

**Description:** Enforce the exact foreground completion polling pattern. The frame-safe call site must call `poll_committed_groups(max_count = <nonzero integer literal>)`; the helper body is the only allowed place where `CoreSpscConsumer<CommittedAtomicGroup>.try_next()` may appear in frame-safe source.

**Acceptance Criteria:**

- `poll_committed_groups(max_count = 4)` is accepted from frame-safe code.
- `poll_committed_groups(max_count = 0)` and `poll_committed_groups(max_count = MAX_POLLS)` are rejected with `SEM0145`.
- A direct `self.durable.try_next()` call from any frame-safe method other than `DesktopStorageBoundary.poll_committed_groups` is rejected with `SEM0145`.
- `DesktopStorageBoundary.poll_committed_groups` is accepted only when its loop condition is syntactically `polled < max_count`, increments `polled = polled + 1`, and contains no `arm_wait`.
- `may_wait` methods cannot be called from methods named `tick`, `emit`, `render`, `late_lane`, or any method marked `frame_safe`.

**Code Examples:**

```go
func checkFrameSafePollCall(c *checker, call *ast.CallExpr) {
	if call.Method != "poll_committed_groups" {
		return
	}
	arg := namedArgExpr(call.Args, "max_count")
	lit, ok := arg.(*ast.IntLiteral)
	if !ok || lit.Value == "0" {
		c.error(call.SpanV, diag.SEM0145, "poll_committed_groups requires literal max_count > 0 at the frame-safe call site")
	}
}

func checkDirectCommittedGroupTryNext(c *checker, ctx effectContext, call *ast.CallExpr, target *Method) {
	if ctx.Effect != "frame_safe" || call.Method != "try_next" {
		return
	}
	if ctx.ModuleName == "desktop.storage_boundary" && ctx.Method.Name == "poll_committed_groups" {
		return
	}
	c.error(call.SpanV, diag.SEM0145, "frame-safe code must poll committed groups through poll_committed_groups")
}
```

```go
func TestDesktopPollMaxCountMustBeLiteral(t *testing.T) {
	_, ds := desktopTypeDiagsForModules(t, `module sem.desktop_poll_bad
use { DesktopStorageBoundary } from desktop.storage_boundary
data App {
  frame_safe fn tick(self, storage: DesktopStorageBoundary) {
    let max = 4
    storage.poll_committed_groups(max_count = max)
  }
}`)
	if !hasCode(ds, diag.SEM0145) {
		t.Fatalf("diagnostics = %#v, want SEM0145", ds)
	}
}
```

This plan does not allow a constant binding such as `const MAX_POLLS: U64 = 4` for `max_count`; the call site must contain an integer literal like `max_count = 4`.

- [ ] **Step 1: Add failing poll fixtures and tests**

Run: `go test ./compiler/sem -run 'DesktopPoll|ForegroundWait' -v`

Expected: FAIL because bounded completion poll checks do not exist.

- [ ] **Step 2: Implement call-site and helper-body checks**

Run: `go test ./compiler/sem -run 'DesktopPoll|ForegroundWait' -v`

Expected: PASS.

- [ ] **Step 3: Run negative fixtures**

Run: `go test ./compiler -run TestNegativeFixtures -v`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git diff --check
git add compiler/sem/desktop_authority_test.go compiler/sem/effects.go compiler/sem/check.go tests/fixtures/negative/desktop_foreground_wait.wrela
git commit -m "feat: enforce bounded desktop completion polling -Codex Automated"
```

---

## 8. Phase 3: Deterministic Host Renderer And Replay

**Description:** Build a behavior-testable Go mirror of the field renderer before x86 codegen and GOP presentation. This gives every visual rule a deterministic unit test and pixel hash.

**Acceptance Criteria:**

- Host renderer bins fields into 16x16 tiles and composites back-to-front by z.
- Scalar and AVX2 packet renderer outputs match for the AVX2-supported built-in programs (`SolidRect`, `Caret`, `SampledRGBA`) on machines with AVX2; unsupported programs fall back to scalar and the AVX2 test is skipped when CPU feature detection says AVX2 is unavailable.
- Reports include all required v1 fields.
- Replay of recorded frame inputs reproduces the same surface hash and report summary.

**Code Examples:**

```go
cfg := RenderConfig{TileWidth: 16, TileHeight: 16, Strategy: StrategyScalar}
result, err := Render(FrameGraph{Scopes: []FieldScope{scope}}, NewSurface(64, 64, PixelFormatRGBA8), cfg)
if err != nil {
	t.Fatal(err)
}
if result.Report.FieldCount != 3 {
	t.Fatalf("fields = %d, want 3", result.Report.FieldCount)
}
```

### Task 14: Host Field Types And Support Overlap

**Prerequisite:** Tasks 2 and 6.

**Files:**

- Create: `compiler/desktopfmt/support.go`
- Create: `compiler/desktopfmt/field.go`
- Create: `compiler/desktopfmt/support_test.go`

**Description:** Mirror the v1 Wrela field model in Go and implement support/tile overlap math.

**Acceptance Criteria:**

- `RectSupport`, `RoundedRectSupport`, `CircleSupport`, `Bounds3DSupport`, and `UnknownSupport` exist.
- `SupportBounds` returns a conservative integer pixel rectangle.
- `TilesOverlapping` returns deterministic tile coordinates sorted by y then x.
- Unknown support overlaps every tile and increments reportable unknown-support counters in later tasks.

**Code Examples:**

```go
type Fx26_6 int64

func Fx(px int64) Fx26_6 { return Fx26_6(px << 6) }

type Rect struct {
	X Fx26_6
	Y Fx26_6
	W Fx26_6
	H Fx26_6
}

type FieldSupport struct {
	Kind FieldSupportKind
	Rect Rect
	Radius Fx26_6
}
```

```go
func TestTilesOverlappingRect(t *testing.T) {
	tiles := TilesOverlapping(FieldSupport{Kind: SupportRect, Rect: Rect{X: Fx(15), Y: Fx(15), W: Fx(3), H: Fx(3)}}, 16, 16, 64, 64)
	want := []TileCoord{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 0, Y: 1}, {X: 1, Y: 1}}
	if !reflect.DeepEqual(tiles, want) {
		t.Fatalf("tiles = %#v, want %#v", tiles, want)
	}
}
```

- [ ] **Step 1: Add failing tests**

Create `support_test.go` with rect, circle, and unknown support cases.

Run: `go test ./compiler/desktopfmt -run 'Support|Tiles' -v`

Expected: FAIL with undefined support types.

- [ ] **Step 2: Add support implementation**

Create `support.go` and `field.go`.

Run: `go test ./compiler/desktopfmt -run 'Support|Tiles' -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add compiler/desktopfmt/support.go compiler/desktopfmt/field.go compiler/desktopfmt/support_test.go
git commit -m "feat: add desktop host field support model -Codex Automated"
```

### Task 15: Tile Binning, Z Sort, And Premultiplied Compositing

**Prerequisite:** Task 14.

**Files:**

- Create: `compiler/desktopfmt/tile.go`
- Create: `compiler/desktopfmt/render.go`
- Create: `compiler/desktopfmt/render_test.go`

**Description:** Implement the direct reference renderer loop: bin fields into tiles, sort by z, evaluate built-in field programs, and apply premultiplied source-over.

**Acceptance Criteria:**

- Fields in the same tile are sorted by ascending z and stable identity for ties.
- `Over(dst, src)` uses premultiplied alpha with 16-bit intermediate precision.
- Rendering two half-alpha fields produces the exact pixel expected by the test vector.
- Clip rejection is recorded separately from support rejection.

**Code Examples:**

```go
func Over(dst, src RGBA8) RGBA8 {
	invA := uint16(255 - src.A)
	return RGBA8{
		R: uint8(uint16(src.R) + uint16(dst.R)*invA/255),
		G: uint8(uint16(src.G) + uint16(dst.G)*invA/255),
		B: uint8(uint16(src.B) + uint16(dst.B)*invA/255),
		A: uint8(uint16(src.A) + uint16(dst.A)*invA/255),
	}
}
```

```go
func TestRenderSortsByZ(t *testing.T) {
	surface := NewSurface(1, 1, PixelFormatRGBA8)
	fields := []Field{
		SolidRectField(2, Rect{W: Fx(1), H: Fx(1)}, RGBA8{B: 255, A: 255}),
		SolidRectField(1, Rect{W: Fx(1), H: Fx(1)}, RGBA8{R: 255, A: 255}),
	}
	result, err := RenderFields(fields, surface, RenderConfig{TileWidth: 16, TileHeight: 16, Strategy: StrategyScalar})
	if err != nil { t.Fatal(err) }
	got, err := result.Surface.At(0, 0)
	if err != nil { t.Fatal(err) }
	if got.B != 255 {
		t.Fatalf("top pixel = %#v, want blue", got)
	}
}
```

- [ ] **Step 1: Add failing render tests**

Add tests for z sorting, over compositing, and clip counters.

Run: `go test ./compiler/desktopfmt -run 'Render|Over|Z' -v`

Expected: FAIL with undefined renderer.

- [ ] **Step 2: Add renderer implementation**

Create `tile.go` and `render.go`.

Run: `go test ./compiler/desktopfmt -run 'Render|Over|Z' -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add compiler/desktopfmt/tile.go compiler/desktopfmt/render.go compiler/desktopfmt/render_test.go
git commit -m "feat: add deterministic desktop tile renderer -Codex Automated"
```

### Task 16A: Field Program Enum And Dispatch Table

**Prerequisite:** Task 15.

**Files:**

- Modify: `compiler/desktopfmt/field.go`
- Modify: `compiler/desktopfmt/render.go`
- Create: `compiler/desktopfmt/field_program_test.go`

**Description:** Add the host mirror enum, dispatch table, and typed unsupported-program error before individual evaluators are implemented.

**Acceptance Criteria:**

- `FieldProgram` constants match the Wrela `FieldProgram` order from Task 6.
- `EvaluateFieldProgram` switches over every v1 program.
- Unknown program values return `ErrUnsupportedFieldProgram`.
- The dispatch test fails if a new Wrela program appears without a host mirror constant.

**Code Examples:**

```go
type FieldProgram uint8

const (
	ProgramSolidRect FieldProgram = iota + 1
	ProgramRoundedRect
	ProgramBorderRect
	ProgramGlyphCoverage
	ProgramCaret
	ProgramSampledRGBA
	ProgramAnalyticSphere
)

var ErrUnsupportedFieldProgram = errors.New("unsupported desktop field program")
```

- [ ] **Step 1: Add failing dispatch tests**

Run: `go test ./compiler/desktopfmt -run 'FieldProgramDispatch|UnsupportedFieldProgram' -v`

Expected: FAIL with undefined `FieldProgram`.

- [ ] **Step 2: Add enum and dispatch shell**

Run: `go test ./compiler/desktopfmt -run 'FieldProgramDispatch|UnsupportedFieldProgram' -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add compiler/desktopfmt/field.go compiler/desktopfmt/render.go compiler/desktopfmt/field_program_test.go
git commit -m "feat: add desktop field program dispatch table -Codex Automated"
```

### Task 16B: Solid, Rounded, Border, And Caret Evaluators

**Prerequisite:** Task 16A.

**Files:**

- Modify: `compiler/desktopfmt/field.go`
- Modify: `compiler/desktopfmt/render.go`
- Modify: `compiler/desktopfmt/field_program_test.go`

**Description:** Implement the four rect/caret evaluators that require no external sampled or glyph cache data.

**Acceptance Criteria:**

- `SolidRect` fills all pixels inside `Rect`.
- `RoundedRect` uses conservative rounded-corner distance from `radius`; no supersampling is added in v1.
- `BorderRect` draws a one-pixel inside border and leaves the interior transparent.
- `Caret` draws a one-pixel vertical rect with full alpha.

**Code Examples:**

```go
func TestSolidRectFillsInsideOnly(t *testing.T) {
	field := SolidRectField(1, Rect{X: Fx(1), Y: Fx(1), W: Fx(2), H: Fx(2)}, RGBA8{R: 9, A: 255})
	surface := mustRenderOne(t, field, 4, 4)
	mustPixel(t, surface, 1, 1, RGBA8{R: 9, A: 255})
	mustPixel(t, surface, 0, 0, RGBA8{})
}
```

```go
func TestBorderRectLeavesInteriorTransparent(t *testing.T) {
	field := BorderRectField(1, Rect{X: Fx(0), Y: Fx(0), W: Fx(5), H: Fx(5)}, RGBA8{G: 12, A: 255})
	surface := mustRenderOne(t, field, 5, 5)
	mustPixel(t, surface, 0, 2, RGBA8{G: 12, A: 255})
	mustPixel(t, surface, 2, 2, RGBA8{})
}
```

- [ ] **Step 1: Add failing rect/caret tests**

Run: `go test ./compiler/desktopfmt -run 'SolidRect|RoundedRect|BorderRect|Caret' -v`

Expected: FAIL because evaluators are stubs.

- [ ] **Step 2: Implement rect/caret evaluators**

Run: `go test ./compiler/desktopfmt -run 'SolidRect|RoundedRect|BorderRect|Caret' -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add compiler/desktopfmt/field.go compiler/desktopfmt/render.go compiler/desktopfmt/field_program_test.go
git commit -m "feat: add desktop rect and caret field evaluators -Codex Automated"
```

### Task 16C: Glyph Coverage Evaluator

**Prerequisite:** Task 16B.

**Files:**

- Modify: `compiler/desktopfmt/field.go`
- Modify: `compiler/desktopfmt/render.go`
- Modify: `compiler/desktopfmt/field_program_test.go`

**Description:** Implement ASCII 8x8 bitmap glyph coverage with cache hit/miss accounting. Analytic glyph SDFs remain deferred.

**Acceptance Criteria:**

- `GlyphCoverage` reads an 8x8 bitmap coverage row for ASCII glyph ids.
- Unknown glyph ids render the fixed replacement box pattern.
- Cache hits increment `CacheHits`; cache misses increment `CacheMisses`.
- Test verifies the `W` glyph at fixture pixel `(3, 1)` is opaque and `(4, 7)` is transparent.

**Code Examples:**

```go
func TestGlyphCoverageUsesASCII8x8Bitmap(t *testing.T) {
	cache := NewGlyphCoverageCache()
	field := GlyphCoverageField(1, 'W', Point{X: Fx(0), Y: Fx(0)}, RGBA8{R: 255, G: 255, B: 255, A: 255}, cache)
	result := mustRenderFields(t, []Field{field}, 8, 8)
	mustPixel(t, result.Surface, 3, 1, RGBA8{R: 255, G: 255, B: 255, A: 255})
	mustPixel(t, result.Surface, 4, 7, RGBA8{})
	if result.Report.CacheMisses != 1 {
		t.Fatalf("cache misses = %d, want 1", result.Report.CacheMisses)
	}
}
```

- [ ] **Step 1: Add failing glyph tests**

Run: `go test ./compiler/desktopfmt -run 'GlyphCoverage|GlyphCache' -v`

Expected: FAIL because glyph coverage is missing.

- [ ] **Step 2: Implement glyph evaluator**

Run: `go test ./compiler/desktopfmt -run 'GlyphCoverage|GlyphCache' -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add compiler/desktopfmt/field.go compiler/desktopfmt/render.go compiler/desktopfmt/field_program_test.go
git commit -m "feat: add desktop glyph coverage evaluator -Codex Automated"
```

### Task 16D: Sampled RGBA Evaluator

**Prerequisite:** Task 16B.

**Files:**

- Modify: `compiler/desktopfmt/field.go`
- Modify: `compiler/desktopfmt/render.go`
- Modify: `compiler/desktopfmt/field_program_test.go`

**Description:** Implement sampled RGBA fields over embedded trusted assets only.

**Acceptance Criteria:**

- `SampledRGBA` supports `PixelFormatRGBA8` and nearest filtering.
- Unsupported pixel formats return `ErrUnsupportedPixelFormat`.
- Sampled bytes touched are charged as `width * height * 4` for the sampled region.
- Bilinear filtering is represented by the enum but is not used by the demo until Task 29A host scaling tests.

**Code Examples:**

```go
func TestSampledRGBARejectsUnsupportedFormat(t *testing.T) {
	field := SampledField(Rect{W: Fx(1), H: Fx(1)}, SampledImage{Format: PixelFormat(99)})
	_, err := RenderFields([]Field{field}, NewSurface(1, 1, PixelFormatRGBA8), DefaultRenderConfig())
	if !errors.Is(err, ErrUnsupportedPixelFormat) {
		t.Fatalf("err = %v, want ErrUnsupportedPixelFormat", err)
	}
}
```

- [ ] **Step 1: Add failing sampled tests**

Run: `go test ./compiler/desktopfmt -run 'SampledRGBA|UnsupportedPixelFormat|SampledBytes' -v`

Expected: FAIL because sampled evaluator is missing.

- [ ] **Step 2: Implement sampled evaluator**

Run: `go test ./compiler/desktopfmt -run 'SampledRGBA|UnsupportedPixelFormat|SampledBytes' -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add compiler/desktopfmt/field.go compiler/desktopfmt/render.go compiler/desktopfmt/field_program_test.go
git commit -m "feat: add desktop sampled rgba evaluator -Codex Automated"
```

### Task 16E: Analytic Sphere Evaluator

**Prerequisite:** Task 16B.

**Files:**

- Modify: `compiler/desktopfmt/field.go`
- Modify: `compiler/desktopfmt/render.go`
- Modify: `compiler/desktopfmt/field_program_test.go`

**Description:** Implement the bounded analytic sphere projection used by the demo. This is not a general ray marcher.

**Acceptance Criteria:**

- Sphere support is a projected 2D circle from `Bounds3D`; pixels outside the circle are transparent.
- Center pixel is shaded brighter than edge pixels using deterministic integer math.
- Evaluator contains no unbounded loop and no per-pixel ray marching.
- Test checks center, edge, and outside pixels for exact RGBA values.

**Code Examples:**

```go
func TestAnalyticSphereHasBoundedProjection(t *testing.T) {
	field := AnalyticSphereField(1, Point{X: Fx(16), Y: Fx(16)}, Fx(8), RGBA8{R: 80, G: 160, B: 220, A: 255})
	surface := mustRenderOne(t, field, 32, 32)
	mustPixel(t, surface, 16, 16, RGBA8{R: 80, G: 160, B: 220, A: 255})
	mustPixel(t, surface, 0, 0, RGBA8{})
}
```

- [ ] **Step 1: Add failing sphere tests**

Run: `go test ./compiler/desktopfmt -run AnalyticSphere -v`

Expected: FAIL because sphere evaluator is missing.

- [ ] **Step 2: Implement sphere evaluator**

Run: `go test ./compiler/desktopfmt -run AnalyticSphere -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add compiler/desktopfmt/field.go compiler/desktopfmt/render.go compiler/desktopfmt/field_program_test.go
git commit -m "feat: add desktop analytic sphere evaluator -Codex Automated"
```

### Task 17: Frame Graph, Budgets, And Report Counters In Host Mirror

**Prerequisite:** Task 16E.

**Files:**

- Create: `compiler/desktopfmt/report.go`
- Create: `compiler/desktopfmt/report_test.go`
- Modify: `compiler/desktopfmt/render.go`

**Description:** Add host frame graph scopes, budget charging, and frame report counters.

**Acceptance Criteria:**

- Rendering a frame with over-budget fields returns `ErrBudgetExhausted` and a report naming the scope.
- Unknown supports, sampled bytes, semantic nodes, glyph fields, tile count, candidate counts, strategy id, lane width, cache hits, cache misses, input-to-present latency, and missed reason id are recorded.
- The Go `FrameReportSummary` field set matches the Wrela `FrameReportSummary` field set from Task 10A one-for-one, using Go naming conventions only for casing.
- Report JSON uses `FrameReportSummary.MarshalStableJSON()` to write keys in the same fixed order as the struct below; tests compare the exact JSON string.

**Code Examples:**

```go
type FrameReportSummary struct {
	FrameID               uint64 `json:"frame_id"`
	TargetHz              uint64 `json:"target_hz"`
	ActualNs              uint64 `json:"actual_ns"`
	Missed                bool   `json:"missed"`
	FieldCount            uint64 `json:"field_count"`
	TileCount             uint64 `json:"tile_count"`
	AvgCandidatesX100     uint64 `json:"avg_candidates_x100"`
	MaxCandidatesPerTile  uint64 `json:"max_candidates_per_tile"`
	GlyphFields           uint64 `json:"glyph_fields"`
	SampledFields         uint64 `json:"sampled_fields"`
	SampledBytes          uint64 `json:"sampled_bytes"`
	SemanticNodes         uint64 `json:"semantic_nodes"`
	StrategyID            uint64 `json:"strategy_id"`
	LaneWidth             uint64 `json:"lane_width"`
	CacheHits             uint64 `json:"cache_hits"`
	CacheMisses           uint64 `json:"cache_misses"`
	InputToPresentNs      uint64 `json:"input_to_present_ns"`
	MissedReasonID        uint64 `json:"missed_reason_id"`
}

func (r FrameReportSummary) MarshalStableJSON() []byte {
	return []byte(fmt.Sprintf(
		`{"frame_id":%d,"target_hz":%d,"actual_ns":%d,"missed":%t,"field_count":%d,"tile_count":%d,"avg_candidates_x100":%d,"max_candidates_per_tile":%d,"glyph_fields":%d,"sampled_fields":%d,"sampled_bytes":%d,"semantic_nodes":%d,"strategy_id":%d,"lane_width":%d,"cache_hits":%d,"cache_misses":%d,"input_to_present_ns":%d,"missed_reason_id":%d}`,
		r.FrameID, r.TargetHz, r.ActualNs, r.Missed, r.FieldCount, r.TileCount, r.AvgCandidatesX100, r.MaxCandidatesPerTile, r.GlyphFields, r.SampledFields, r.SampledBytes, r.SemanticNodes, r.StrategyID, r.LaneWidth, r.CacheHits, r.CacheMisses, r.InputToPresentNs, r.MissedReasonID,
	))
}
```

```go
func TestBudgetExhaustionReportsScope(t *testing.T) {
	scope := Scope{ID: 7, Budget: FrameBudget{MaxFields: 1}, Fields: []Field{SolidDemoField(1), SolidDemoField(2)}}
	result, err := RenderFrame(FrameGraph{Scopes: []Scope{scope}}, NewSurface(4, 4, PixelFormatRGBA8), DefaultRenderConfig())
	if !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("err = %v, want ErrBudgetExhausted", err)
	}
	if result.Report.MissedReasonID != MissedReasonBudgetFields {
		t.Fatalf("missed reason = %d", result.Report.MissedReasonID)
	}
}
```

- [ ] **Step 1: Add failing report tests**

Run: `go test ./compiler/desktopfmt -run 'Budget|Report' -v`

Expected: FAIL with missing report structs.

- [ ] **Step 2: Implement reports and budget charging**

Run: `go test ./compiler/desktopfmt -run 'Budget|Report' -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add compiler/desktopfmt/report.go compiler/desktopfmt/report_test.go compiler/desktopfmt/render.go
git commit -m "feat: add desktop host frame reports and budgets -Codex Automated"
```

### Task 18: Text Line, Glyph Cache, Selection, And Scroll Behavior Mirror

**Prerequisite:** Task 17.

**Files:**

- Create: `compiler/desktopfmt/text.go`
- Create: `compiler/desktopfmt/text_test.go`
- Modify: `compiler/desktopfmt/field.go`

**Description:** Implement host behavior for ASCII monospace text, caret hit testing, selection fields, glyph cache keys, and scroll visible-child transforms.

**Acceptance Criteria:**

- `LayoutASCIITextLine("Wrela")` emits five glyphs and six caret stops.
- Hit testing chooses the nearest caret stop.
- Selection emits a highlight field behind glyph z.
- Scroll view emits only visible children and maps hit points back into content coordinates.
- Glyph cache key changes when glyph id, size, transform version, color policy id, display scale, color mode, or font version changes.

**Code Examples:**

```go
func TestTextLineHitTestNearestCaret(t *testing.T) {
	line := TextLine{Carets: []Fx26_6{Fx(0), Fx(10), Fx(20)}}
	got := TextLineHitTest(line, Vec2{X: Fx(16), Y: Fx(0)})
	if got.Caret != 2 {
		t.Fatalf("caret = %d, want 2", got.Caret)
	}
}
```

```go
func TestScrollViewCullsInvisibleChild(t *testing.T) {
	view := ScrollView{Rect: Rect{X: Fx(0), Y: Fx(0), W: Fx(100), H: Fx(100)}, Scroll: Vec2{Y: Fx(200)}}
	child := ScrollChild{Bounds: Rect{X: Fx(0), Y: Fx(500), W: Fx(20), H: Fx(20)}}
	if VisibleScrollChildren(view, []ScrollChild{child}) != 0 {
		t.Fatal("child below viewport must be culled")
	}
}
```

- [ ] **Step 1: Add failing text tests**

Run: `go test ./compiler/desktopfmt -run 'Text|Glyph|Scroll|Caret|Selection' -v`

Expected: FAIL with missing text mirror.

- [ ] **Step 2: Implement text mirror**

Run: `go test ./compiler/desktopfmt -run 'Text|Glyph|Scroll|Caret|Selection' -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add compiler/desktopfmt/text.go compiler/desktopfmt/text_test.go compiler/desktopfmt/field.go
git commit -m "feat: add desktop text and scroll behavior mirror -Codex Automated"
```

### Task 19: Input Targets And Semantic Tree Mirror

**Prerequisite:** Task 18.

**Files:**

- Create: `compiler/desktopfmt/input.go`
- Create: `compiler/desktopfmt/input_test.go`

**Description:** Implement top-to-bottom hit testing, pointer capture, coordinate transforms, and semantic node validation in the host mirror.

**Acceptance Criteria:**

- Hit testing walks highest z first.
- Pointer capture keeps routing pointer moves to the captured input identity until release.
- Scroll transforms map `screen = viewport_origin + content - scroll` and reverse for hit testing.
- Semantic validation rejects duplicate focus order and missing referenced field/input identities.

**Code Examples:**

```go
func TestHitTestingUsesHighestZ(t *testing.T) {
	targets := []InputTarget{
		{ID: 1, Z: 1, Support: FullRect(10, 10)},
		{ID: 2, Z: 2, Support: FullRect(10, 10)},
	}
	hit := HitTest(targets, Point{X: Fx(1), Y: Fx(1)})
	if hit.TargetID != 2 {
		t.Fatalf("target = %d, want 2", hit.TargetID)
	}
}
```

- [ ] **Step 1: Add failing input tests**

Run: `go test ./compiler/desktopfmt -run 'Input|Capture|Semantic|Hit' -v`

Expected: FAIL with missing input mirror.

- [ ] **Step 2: Implement input and semantic mirror**

Run: `go test ./compiler/desktopfmt -run 'Input|Capture|Semantic|Hit' -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add compiler/desktopfmt/input.go compiler/desktopfmt/input_test.go
git commit -m "feat: add desktop input and semantic mirror -Codex Automated"
```

### Task 20A: Deterministic Replay Fixture Types

**Prerequisite:** Task 19.

**Files:**

- Create: `compiler/desktopfmt/replay.go`
- Create: `compiler/desktopfmt/replay_test.go`
- Create: `compiler/desktopfmt/fixtures.go`

**Description:** Add replay input structs and deterministic fixture builders without implementing replay execution yet.

**Acceptance Criteria:**

- `ReplayFrameInput` mirrors Task 10B fields: frame id, durable state id, input sample id, completion watermark, monotonic time, and strategy id.
- `FrameFixture` contains graph, surface, config, and input.
- `DemoFrameFixture()` returns a small deterministic two-field graph used by Tasks 20B through 20D and replaced by Task 31's full scene.
- No goroutine, channel, timer, or wall-clock read is introduced.

**Code Examples:**

```go
type ReplayFrameInput struct {
	FrameID             uint64
	DurableStateID      uint64
	InputSampleID       uint64
	CompletionWatermark uint64
	MonotonicTimeNS     uint64
	StrategyID          uint64
}

type FrameFixture struct {
	Graph   FrameGraph
	Surface Surface
	Config  RenderConfig
	Input   ReplayFrameInput
}
```

- [ ] **Step 1: Add failing fixture tests**

Run: `go test ./compiler/desktopfmt -run 'ReplayFixture|DemoFrameFixture' -v`

Expected: FAIL with missing replay fixture types.

- [ ] **Step 2: Implement fixture types**

Run: `go test ./compiler/desktopfmt -run 'ReplayFixture|DemoFrameFixture' -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add compiler/desktopfmt/replay.go compiler/desktopfmt/replay_test.go compiler/desktopfmt/fixtures.go
git commit -m "feat: add desktop replay fixture types -Codex Automated"
```

### Task 20B: Replay Hash And Report Mirror

**Prerequisite:** Task 20A.

**Files:**

- Modify: `compiler/desktopfmt/replay.go`
- Modify: `compiler/desktopfmt/replay_test.go`

**Description:** Implement deterministic replay execution and compare both framebuffer hash and stable report JSON.

**Acceptance Criteria:**

- `ReplayFrame(input)` renders from fixture state selected by `input.FrameID` and returns surface plus report.
- Replay returns the same `HashRGBA8` as direct `RenderFrame` for the same input.
- Replay returns the same `FrameReportSummary.MarshalStableJSON()` as direct render.
- Replay rejects mismatched durable state id or input sample id with typed errors.

**Code Examples:**

```go
func TestReplayReproducesHashAndReport(t *testing.T) {
	fixture := DemoFrameFixture()
	first, err := RenderFrame(fixture.Graph, fixture.Surface, fixture.Config)
	if err != nil { t.Fatal(err) }
	second, err := ReplayFrame(fixture.Input)
	if err != nil { t.Fatal(err) }
	if HashRGBA8(first.Surface) != HashRGBA8(second.Surface) {
		t.Fatal("replay hash mismatch")
	}
	if !bytes.Equal(first.Report.MarshalStableJSON(), second.Report.MarshalStableJSON()) {
		t.Fatalf("replay report mismatch\nfirst=%s\nsecond=%s", first.Report.MarshalStableJSON(), second.Report.MarshalStableJSON())
	}
}
```

- [ ] **Step 1: Add failing replay hash tests**

Run: `go test ./compiler/desktopfmt -run ReplayReproduces -v`

Expected: FAIL with missing `ReplayFrame`.

- [ ] **Step 2: Implement replay execution**

Run: `go test ./compiler/desktopfmt -run ReplayReproduces -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add compiler/desktopfmt/replay.go compiler/desktopfmt/replay_test.go
git commit -m "feat: add deterministic desktop replay execution -Codex Automated"
```

### Task 20C: Pixel Provenance Mirror

**Prerequisite:** Task 20B.

**Files:**

- Modify: `compiler/desktopfmt/replay.go`
- Modify: `compiler/desktopfmt/replay_test.go`

**Description:** Add opt-in pixel inspection by rerendering the requested pixel or tile with provenance tracking enabled.

**Acceptance Criteria:**

- `PixelProvenance` fields are exactly `FrameID`, `FieldID`, `ScopeID`, `AppID`, `FieldProgram`, `SemanticRole`, `CacheStatus`, `InputSampleID`, and `CostNs`.
- `InspectPixel(input, x, y)` computes provenance only for that pixel.
- `InspectTile(input, tileX, tileY)` computes provenance only for pixels in that tile.
- Tests prove unrelated pixels are not inspected by checking an `InspectedPixels` counter.

**Code Examples:**

```go
type PixelProvenance struct {
	FrameID       uint64
	FieldID       uint64
	ScopeID       uint64
	AppID         uint64
	FieldProgram  string
	SemanticRole  string
	CacheStatus   string
	InputSampleID uint64
	CostNs        uint64
}

func TestInspectPixelNamesTopField(t *testing.T) {
	fixture := DemoFrameFixture()
	prov, err := InspectPixel(fixture.Input, 12, 8)
	if err != nil { t.Fatal(err) }
	if prov.FieldID == 0 || prov.ScopeID == 0 {
		t.Fatalf("provenance = %#v", prov)
	}
}
```

- [ ] **Step 1: Add failing provenance tests**

Run: `go test ./compiler/desktopfmt -run 'InspectPixel|InspectTile|Provenance' -v`

Expected: FAIL with missing provenance implementation.

- [ ] **Step 2: Implement pixel provenance**

Run: `go test ./compiler/desktopfmt -run 'InspectPixel|InspectTile|Provenance' -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add compiler/desktopfmt/replay.go compiler/desktopfmt/replay_test.go
git commit -m "feat: add desktop pixel provenance mirror -Codex Automated"
```

### Task 20D: Storage Completion Replay Facts

**Prerequisite:** Task 20B.

**Files:**

- Modify: `compiler/desktopfmt/replay.go`
- Modify: `compiler/desktopfmt/replay_test.go`
- Modify: `compiler/desktopfmt/fixtures.go`

**Description:** Model storage completion facts in replay as an ordered in-memory slice that mirrors `CommittedAtomicGroup` without adding async behavior.

**Acceptance Criteria:**

- `CommittedAtomicGroupMirror` contains `FirstEventID` and `LastEventID`.
- `ReplayStorageFacts.Poll(maxPolls)` returns at most `maxPolls` committed groups and updates an index deterministically.
- `MaxPolls == 0` returns no groups and no error.
- No goroutine, channel, timer, or wall-clock wait is used anywhere in `compiler/desktopfmt`.

**Code Examples:**

```go
type CommittedAtomicGroupMirror struct {
	FirstEventID uint64
	LastEventID  uint64
}

type ReplayStorageFacts struct {
	Committed []CommittedAtomicGroupMirror
	Next      int
}

func (f *ReplayStorageFacts) Poll(maxPolls uint64) []CommittedAtomicGroupMirror {
	limit := int(maxPolls)
	if remaining := len(f.Committed) - f.Next; limit > remaining {
		limit = remaining
	}
	out := append([]CommittedAtomicGroupMirror(nil), f.Committed[f.Next:f.Next+limit]...)
	f.Next += limit
	return out
}
```

- [ ] **Step 1: Add failing storage facts tests**

Run: `go test ./compiler/desktopfmt -run ReplayStorageFacts -v`

Expected: FAIL with missing storage facts mirror.

- [ ] **Step 2: Implement storage facts mirror**

Run: `go test ./compiler/desktopfmt -run ReplayStorageFacts -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add compiler/desktopfmt/replay.go compiler/desktopfmt/replay_test.go compiler/desktopfmt/fixtures.go
git commit -m "feat: add desktop replay storage completion facts -Codex Automated"
```

---

## 9. Phase 4: Runtime Desktop Emission

**Description:** Implement source-visible desktop emission and frame loop logic using the contracts and host mirror behavior. This phase keeps foreground work bounded and explicit.

**Acceptance Criteria:**

- Demo source emits fields, input targets, and semantic nodes from the same state.
- Scope writers mechanically charge field counts, unknown supports, and sampled bytes.
- Text box, selection, caret, scroll view, sampled checker image, and analytic sphere are emitted in Wrela source.
- Storage boundary is submit/poll only; no frame lane wait is introduced.

**Code Examples:**

```wrela
data EditorDemo {
    frame_safe fn tick(self, input: InputFrame, frame: FrameGraph) {
        self.apply_input_now(input = input)
        self.poll_committed_groups(max_count = 4)
        self.emit_fields(frame = frame)
    }
}
```

### Task 21: Frame Scope Writer Runtime Methods

**Prerequisite:** Tasks 7 and 13C.

**Files:**

- Modify: `wrela/desktop/frame.wrela`
- Create: `compiler/sem/desktop_budget_source_test.go`

**Description:** Replace stub writer methods with source-visible budget charging for fields, input targets, semantic nodes, unknown supports, and sampled bytes.

**Acceptance Criteria:**

- `push_field` increments field count and charges unknown support when `field.support` is `Unknown`.
- `push_input_target` and `push_semantic_node` increment their own counters.
- `charge_sampled_bytes(count)` returns `BudgetExhausted` when the cap would be exceeded.
- Tests verify methods exist, return `Result<Unit, BudgetExhausted>`, and source bodies contain the exact counter increments and cap checks for fields, unknown supports, sampled bytes, input targets, and semantic nodes.

**Code Examples:**

```wrela
class FieldScopeWriter {
    fn charge_sampled_bytes(self, count: U64) -> Result<Unit, BudgetExhausted> {
        if count > self.budget.policy.max_sampled_bytes_touched - self.budget.consumed.sampled_bytes_touched {
            return Result.Err(error = BudgetExhausted())
        }
        self.budget.consumed.sampled_bytes_touched = self.budget.consumed.sampled_bytes_touched + count
        return Result.Ok(value = Unit())
    }
}
```

- [ ] **Step 1: Add failing source test**

Run: `go test ./compiler/sem -run TestDesktopBudgetWriterMethods -v`

Expected: FAIL because full writer methods are absent.

- [ ] **Step 2: Implement methods in source**

Run: `go test ./compiler/sem -run TestDesktopBudgetWriterMethods -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add wrela/desktop/frame.wrela compiler/sem/desktop_budget_source_test.go
git commit -m "feat: implement desktop scope budget writer methods -Codex Automated"
```

### Task 22: Text Box Emitter

**Prerequisite:** Tasks 9, 12E, and 21.

**Files:**

- Modify: `wrela/desktop/text.wrela`
- Modify: `wrela/desktop/demo.wrela`
- Create: `compiler/sem/desktop_text_box_source_test.go`

**Description:** Add the source-visible text box emitter used by the demo.

**Acceptance Criteria:**

- `TextBoxEmitter.emit` pushes background, border, selection, glyph fields, caret, input target, and semantic node in that z order.
- Every `push_*` result is matched and propagated as `BudgetExhausted`.
- Text glyph fields use `FieldProgram.GlyphCoverage`.
- Caret uses `FieldProgram.Caret`.

**Code Examples:**

```wrela
data TextBoxEmitter {
    frame_safe fn emit(self, box: TextBox, scope: FieldScopeWriter) -> Result<Unit, BudgetExhausted> {
        match scope.push_field(field = self.text_box_background(box = box)) {
            Result.Ok(value = unit) => {}
            Result.Err(error = exhausted) => { return Result.Err(error = exhausted) }
        }
        match scope.push_field(field = self.text_box_border(box = box)) {
            Result.Ok(value = unit) => {}
            Result.Err(error = exhausted) => { return Result.Err(error = exhausted) }
        }
        return Result.Ok(value = Unit())
    }
}
```

- [ ] **Step 1: Add failing source-shape test**

Run: `go test ./compiler/sem -run TestDesktopTextBoxEmitterShape -v`

Expected: FAIL because emitter names are missing.

- [ ] **Step 2: Add emitter source**

Add text box data and emitter methods.

Run: `go test ./compiler/sem -run TestDesktopTextBoxEmitterShape -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add wrela/desktop/text.wrela wrela/desktop/demo.wrela compiler/sem/desktop_text_box_source_test.go
git commit -m "feat: add desktop text box emitter -Codex Automated"
```

### Task 23: Scroll, Selection, And Caret Runtime Source

**Prerequisite:** Task 22.

**Files:**

- Modify: `wrela/desktop/text.wrela`
- Create: `compiler/sem/desktop_scroll_source_test.go`

**Description:** Replace the initial text hit-test body with nearest-caret behavior and add source-visible scroll view coordinate transforms.

**Acceptance Criteria:**

- `TextLineTools.hit_test` loops through caret stops and chooses nearest x coordinate.
- `ScrollView.hit_test` returns miss when point is outside viewport.
- `ScrollView.screen_from_content` and `ScrollView.content_from_screen` use the exact formulas from the design.
- Tests use `methodBodySource` below to inspect only the relevant method body for arithmetic terms `self.rect.x`, `self.scroll.x`, `self.rect.y`, and `self.scroll.y`.

**Code Examples:**

```wrela
data ScrollView {
    fn content_from_screen(self, p: Vec2Fx) -> Vec2Fx {
        return Vec2Fx(
            x = Fx26_6(raw = p.x.raw - self.rect.x.raw + self.scroll.x.raw),
            y = Fx26_6(raw = p.y.raw - self.rect.y.raw + self.scroll.y.raw)
        )
    }
}
```

```go
func methodBodySource(t *testing.T, path, owner, method string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil { t.Fatal(err) }
	start := strings.Index(string(data), "fn "+method+"(")
	if start < 0 { t.Fatalf("missing method %s.%s", owner, method) }
	bodyStart := strings.Index(string(data)[start:], "{")
	if bodyStart < 0 { t.Fatalf("missing body for %s.%s", owner, method) }
	bodyStart += start
	depth := 0
	for i := bodyStart; i < len(data); i++ {
		switch data[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return string(data[bodyStart : i+1])
			}
		}
	}
	t.Fatalf("unterminated body for %s.%s", owner, method)
	return ""
}
```

- [ ] **Step 1: Add failing source tests**

Run: `go test ./compiler/sem -run 'TextHitTest|ScrollView' -v`

Expected: FAIL because bodies do not contain required behavior.

- [ ] **Step 2: Implement source behavior**

Run: `go test ./compiler/sem -run 'TextHitTest|ScrollView' -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add wrela/desktop/text.wrela compiler/sem/desktop_scroll_source_test.go
git commit -m "feat: add desktop text hit testing and scroll transforms -Codex Automated"
```

### Task 24: IR Metadata For Desktop Effects And Renderer Strategy

**Prerequisite:** Tasks 10G and 12E.

**Files:**

- Modify: `compiler/ir/ir.go`
- Modify: `compiler/ir/lower.go`
- Create: `compiler/ir/desktop_test.go`

**Description:** Preserve method effects and renderer strategy declarations in IR reports without changing general call lowering.

**Acceptance Criteria:**

- `ir.Function` stores `IsPure`, `IsLane`, and `Effect`.
- `ir.Program` stores `DesktopRendererStrategies []DesktopRendererStrategy`.
- Lowering records `Scalar` and `Avx2Packets(width = 8)` when `desktop.renderer` source is present.
- IR tests prove a pure lane frame-safe method keeps all three flags.

**Code Examples:**

```go
type DesktopRendererStrategy struct {
	Name      string
	LaneWidth uint64
}

type Function struct {
	Symbol string
	IsPure bool
	IsLane bool
	Effect string
}
```

```go
func TestLowerPreservesDesktopEffectFlags(t *testing.T) {
	program := checkedDesktopEffectFixtureForIRTest(t)
	irProgram, ds := Lower(program)
	if len(ds) != 0 { t.Fatalf("lower diagnostics = %#v", ds) }
	fn := findFunction(t, irProgram, "_wrela_method_sem_desktop_ir_GlyphEval_eval")
	if fn.Effect != "frame_safe" {
		t.Fatalf("effect = %q, want frame_safe", fn.Effect)
	}
}

func checkedDesktopEffectFixtureForIRTest(t *testing.T) *sem.CheckedProgram {
	t.Helper()
	modules := parseIRTestSource(t, `module sem.desktop_ir
data GlyphEval {
  pure lane frame_safe fn eval(self) -> U64 { return 1 }
}`)
	index, diags := sem.BuildIndex(modules)
	if len(diags) != 0 { t.Fatalf("build index diagnostics = %#v", diags) }
	checked, diags := sem.Check(index, modules)
	if len(diags) != 0 { t.Fatalf("semantic diagnostics = %#v", diags) }
	return checked
}

func parseIRTestSource(t *testing.T, src string) []*ast.Module {
	t.Helper()
	modules, diags := parse.ParseGraph(source.Graph{Files: []*source.File{source.NewFile(1, "desktop-ir-fixture.wrela", src)}})
	if len(diags) != 0 { t.Fatalf("parse diagnostics = %#v", diags) }
	return modules
}

func checkedDesktopProgramForIRTest(t *testing.T) *sem.CheckedProgram {
	t.Helper()
	modules := parseFullDesktopModuleSetForIRTest(t)
	index, diags := sem.BuildIndex(modules)
	if len(diags) != 0 { t.Fatalf("build index diagnostics = %#v", diags) }
	checked, diags := sem.Check(index, modules)
	if len(diags) != 0 { t.Fatalf("semantic diagnostics = %#v", diags) }
	return checked
}

func parseFullDesktopModuleSetForIRTest(t *testing.T) []*ast.Module {
	t.Helper()
	paths := append(irDesktopBaseSourcePathsForTest(), []string{
		"wrela/desktop/units.wrela", "wrela/desktop/identity.wrela", "wrela/desktop/color.wrela",
		"wrela/desktop/display.wrela", "wrela/desktop/field.wrela", "wrela/desktop/frame.wrela",
		"wrela/desktop/input.wrela", "wrela/desktop/semantics.wrela", "wrela/desktop/text.wrela",
		"wrela/desktop/reports.wrela", "wrela/desktop/replay.wrela", "wrela/desktop/renderer.wrela",
		"wrela/desktop/storage_boundary.wrela", "wrela/desktop/shell.wrela", "wrela/desktop/demo.wrela",
	}...)
	return parseIRTestRepoFiles(t, paths...)
}

func irDesktopBaseSourcePathsForTest() []string {
	return []string{
		"wrela/lang/core.wrela",
		"wrela/platform/hardware/panic.wrela",
		"wrela/platform/hardware/memory.wrela",
		"wrela/platform/hardware/bytes.wrela",
		"wrela/platform/uefi/types.wrela",
		"wrela/machine/x86_64/executor_slot.wrela",
		"wrela/machine/x86_64/executor_loop.wrela",
		"wrela/machine/x86_64/executor_memory.wrela",
		"wrela/machine/x86_64/core_link.wrela",
		"wrela/machine/x86_64/nvme.wrela",
		"wrela/storage/blob.wrela",
		"wrela/storage/format.wrela",
		"wrela/storage/stream.wrela",
		"wrela/storage/writer.wrela",
	}
}

func parseIRTestRepoFiles(t *testing.T, rels ...string) []*ast.Module {
	t.Helper()
	workdir, err := os.Getwd()
	if err != nil { t.Fatalf("getwd: %v", err) }
	repoRoot := filepath.Clean(filepath.Join(workdir, "..", ".."))
	graph := source.Graph{}
	for i, rel := range rels {
		raw, err := os.ReadFile(filepath.Join(repoRoot, rel))
		if err != nil { t.Fatalf("read %s: %v", rel, err) }
		graph.Files = append(graph.Files, source.NewFile(source.FileID(i), rel, string(raw)))
	}
	modules, diags := parse.ParseGraph(graph)
	if len(diags) != 0 { t.Fatalf("parse diagnostics = %#v", diags) }
	return modules
}
```

- [ ] **Step 1: Add failing IR tests**

Run: `go test ./compiler/ir -run Desktop -v`

Expected: FAIL with missing IR fields.

- [ ] **Step 2: Add IR metadata**

Run: `go test ./compiler/ir -run Desktop -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add compiler/ir/ir.go compiler/ir/lower.go compiler/ir/desktop_test.go
git commit -m "feat: preserve desktop effects in IR metadata -Codex Automated"
```

### Task 25: Storage Boundary Demo Source

**Prerequisite:** Tasks 10D, 13C, and 24.

**Files:**

- Modify: `wrela/desktop/storage_boundary.wrela`
- Modify: `wrela/desktop/demo.wrela`
- Create: `compiler/sem/desktop_storage_boundary_test.go`

**Description:** Add the frame-safe submit/poll storage boundary used to show saved, saving, unsaved, and error states without waiting in the frame lane. This task binds the desktop to the completed storage substrate: semantic app requests become `PendingAtomicGroup` values, accepted writes return `StorageAppendResult`, and durable facts arrive as `CommittedAtomicGroup` values through `CoreSpscConsumer<CommittedAtomicGroup>`.

**Acceptance Criteria:**

- `try_append` returns `StorageAppendResult` and preserves storage reject codes such as `1114` and `1116`.
- `poll_committed_groups(max_count = 4)` calls `CoreSpscConsumer<CommittedAtomicGroup>.try_next()` at most four times, never calls `arm_wait`, and returns the observed count.
- Demo state has `buffer_version`, `last_submitted_version`, and `last_durable_version`.
- Visible state maps to saved/saving/unsaved/error exactly as the design states.

**Code Examples:**

```wrela
enum SaveVisualState { Saved Saving Unsaved Error }

data DesktopStorageBoundary {
    durable: CoreSpscConsumer<CommittedAtomicGroup>
    last_durable_version: U64

    frame_safe fn poll_committed_groups(self, max_count: U64) -> U64 {
        let polled = 0
        while polled < max_count {
            match self.durable.try_next() {
                Option.Some(value = committed) => {
                    self.last_durable_version = committed.last_event_id
                }
                Option.None => {
                    return polled
                }
            }
            polled = polled + 1
        }
        return polled
    }
    frame_safe fn save_state(self, buffer_version: U64, last_submitted_version: U64, failed: Bool) -> SaveVisualState {
        if failed {
            return SaveVisualState.Error()
        }
        if self.last_durable_version == buffer_version {
            return SaveVisualState.Saved()
        }
        if last_submitted_version == buffer_version {
            return SaveVisualState.Saving()
        }
        return SaveVisualState.Unsaved()
    }
}
```

- [ ] **Step 1: Add failing source tests**

Run: `go test ./compiler/sem -run DesktopStorageBoundary -v`

Expected: FAIL because storage boundary state is missing.

- [ ] **Step 2: Implement source contracts**

Run: `go test ./compiler/sem -run DesktopStorageBoundary -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add wrela/desktop/storage_boundary.wrela wrela/desktop/demo.wrela compiler/sem/desktop_storage_boundary_test.go
git commit -m "feat: add desktop frame-safe storage boundary demo -Codex Automated"
```

### Task 26A: Shell Frame Tick Order

**Prerequisite:** Tasks 21 through 25.

**Files:**

- Modify: `wrela/desktop/shell.wrela`
- Create: `compiler/sem/desktop_shell_source_test.go`

**Description:** Replace shell contract stubs with source-visible frame tick order while keeping focus and visibility on separate axes.

**Acceptance Criteria:**

- Visibility states remain `Hidden`, `FullyOccluded`, `VisibleClean`, and `VisibleDirty`; `Focused` appears only in `FocusState`.
- `desktop_frame_tick` calls these methods in order: `sample_input`, `begin_frame`, `tick_visible_due_apps`, `run_late_lane`, `render_due_outputs`, `present_due_outputs`, `publish_reports`.
- Source test checks method order by locating method-call spans in the parsed body, not by free-form grep.
- `desktop_frame_tick` is `frame_safe`.

**Code Examples:**

```wrela
data DesktopShell {
    frame_safe fn desktop_frame_tick(self, input: InputFrame, frame: FrameGraph) -> Result<Unit, BudgetExhausted> {
        self.sample_input(input = input)
        self.begin_frame(frame = frame)
        self.tick_visible_due_apps(input = input, frame = frame)
        match self.run_late_lane(input = input, frame = frame) {
            Result.Ok(value = unit) => {}
            Result.Err(error = exhausted) => { return Result.Err(error = exhausted) }
        }
        self.render_due_outputs(frame = frame)
        self.present_due_outputs(frame = frame)
        self.publish_reports(frame = frame)
        return Result.Ok(value = Unit())
    }
}
```

```go
func TestDesktopShellLoopCallOrder(t *testing.T) {
	body := methodBodySource(t, "wrela/desktop/shell.wrela", "DesktopShell", "desktop_frame_tick")
	assertCallOrder(t, body, "sample_input", "begin_frame", "tick_visible_due_apps", "run_late_lane", "render_due_outputs", "present_due_outputs", "publish_reports")
}

func assertCallOrder(t *testing.T, body string, calls ...string) {
	t.Helper()
	offset := 0
	for _, call := range calls {
		next := strings.Index(body[offset:], call+"(")
		if next < 0 {
			t.Fatalf("missing call %s after offset %d in:\n%s", call, offset, body)
		}
		offset += next + len(call)
	}
}
```

- [ ] **Step 1: Add failing shell order source test**

Run: `go test ./compiler/sem -run DesktopShellLoopCallOrder -v`

Expected: FAIL because shell loop source is incomplete.

- [ ] **Step 2: Implement shell tick order**

Run: `go test ./compiler/sem -run DesktopShellLoopCallOrder -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add wrela/desktop/shell.wrela compiler/sem/desktop_shell_source_test.go
git commit -m "feat: add desktop shell frame tick order -Codex Automated"
```

### Task 26B: Strict Late Lane Source

**Prerequisite:** Task 26A.

**Files:**

- Modify: `wrela/desktop/shell.wrela`
- Modify: `compiler/sem/desktop_shell_source_test.go`

**Description:** Add the late lane with an explicit allowlist of cheap field emitters.

**Acceptance Criteria:**

- `run_late_lane` may call only `emit_cursor`, `emit_hover_highlight`, `emit_drag_preview`, and `emit_scroll_feedback`.
- Each late-lane helper returns `Result<Unit, BudgetExhausted>`.
- Any call to `emit_text_box`, `emit_sampled_checker`, storage methods, or app tick methods from `run_late_lane` is rejected by the source test.
- Every helper result is matched and propagated.

**Code Examples:**

```wrela
data DesktopShell {
    frame_safe fn run_late_lane(self, input: InputFrame, frame: FrameGraph) -> Result<Unit, BudgetExhausted> {
        match self.emit_cursor(input = input, frame = frame) {
            Result.Ok(value = unit) => {}
            Result.Err(error = exhausted) => { return Result.Err(error = exhausted) }
        }
        match self.emit_hover_highlight(input = input, frame = frame) {
            Result.Ok(value = unit) => {}
            Result.Err(error = exhausted) => { return Result.Err(error = exhausted) }
        }
        return Result.Ok(value = Unit())
    }
}
```

- [ ] **Step 1: Add failing late-lane source test**

Run: `go test ./compiler/sem -run DesktopLateLane -v`

Expected: FAIL because late lane helpers are missing.

- [ ] **Step 2: Implement late lane**

Run: `go test ./compiler/sem -run DesktopLateLane -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add wrela/desktop/shell.wrela compiler/sem/desktop_shell_source_test.go
git commit -m "feat: add desktop strict late lane source -Codex Automated"
```

### Task 26C: Deterministic Degradation Ladder Source

**Prerequisite:** Task 26B.

**Files:**

- Modify: `wrela/desktop/shell.wrela`
- Modify: `wrela/desktop/demo.wrela`
- Create: `compiler/sem/desktop_degradation_source_test.go`

**Description:** Add the deterministic deadline-miss degradation ladder as explicit source-visible state and reason ids.

**Acceptance Criteria:**

- `DegradationStep` variants are `None`, `DropShadows`, `FreezeSampledMedia`, `ReduceTextEffects`, `LowerInternalResolution`, and `SkipCleanApps`.
- `record_frame_result(missed: Bool)` increments `consecutive_misses` on misses and resets it to zero on a good frame.
- `current_degradation` maps miss counts exactly: `0 -> None`, `1 -> DropShadows`, `2 -> FreezeSampledMedia`, `3 -> ReduceTextEffects`, `4 -> LowerInternalResolution`, `5+ -> SkipCleanApps`.
- Source test simulates six calls and checks the exact reason sequence; no scheduler rescue or hidden retry state is allowed.

**Code Examples:**

```wrela
enum DegradationStep { None DropShadows FreezeSampledMedia ReduceTextEffects LowerInternalResolution SkipCleanApps }

data DeadlineState {
    consecutive_misses: U64

    fn record_frame_result(self, missed: Bool) {
        if missed {
            self.consecutive_misses = self.consecutive_misses + 1
        } else {
            self.consecutive_misses = 0
        }
    }

    fn current_degradation(self) -> DegradationStep {
        if self.consecutive_misses == 0 { return DegradationStep.None() }
        if self.consecutive_misses == 1 { return DegradationStep.DropShadows() }
        if self.consecutive_misses == 2 { return DegradationStep.FreezeSampledMedia() }
        if self.consecutive_misses == 3 { return DegradationStep.ReduceTextEffects() }
        if self.consecutive_misses == 4 { return DegradationStep.LowerInternalResolution() }
        return DegradationStep.SkipCleanApps()
    }
}
```

- [ ] **Step 1: Add failing degradation source test**

Run: `go test ./compiler/sem -run DesktopDegradationLadder -v`

Expected: FAIL because degradation state is missing.

- [ ] **Step 2: Implement degradation ladder**

Run: `go test ./compiler/sem -run DesktopDegradationLadder -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add wrela/desktop/shell.wrela wrela/desktop/demo.wrela compiler/sem/desktop_degradation_source_test.go
git commit -m "feat: add deterministic desktop degradation ladder -Codex Automated"
```

---

## 10. Phase 5: Display Backends, Codegen, And Presentation

**Description:** Connect deterministic rendering to virtual and GOP presentation paths, then add low-level codegen support for copying/scaling the internal surface into a firmware framebuffer.

**Acceptance Criteria:**

- Deterministic virtual backend is CI-first and hardware-independent.
- GOP backend reports real capabilities without pretending vblank/page flip exists.
- Internal surface defaults to 1920x1080 and falls back to 1280x720 when allocation or mode limits require it.
- Presentation matches physical cadence; a 60Hz backend does not produce 120 full frames.

**Code Examples:**

```text
input sampling: high as supported
app/world tick: display-paced unless cadence policy says fixed
render/present: matched to physical scanout
animation time: monotonic real time
```

### Task 27: Deterministic Virtual Display Backend

**Prerequisite:** Task 20D.

**Files:**

- Create: `compiler/desktopfmt/display.go`
- Create: `compiler/desktopfmt/display_test.go`
- Modify: `compiler/desktopfmt/fixtures.go`

**Description:** Add a virtual display backend with fixed modes, simulated cadence, and deterministic present results for CI and replay.

**Acceptance Criteria:**

- Backend exposes one 1920x1080@120 mode and one 1280x720@120 fallback mode.
- Present increments frame id and records elapsed simulated nanoseconds.
- Multi-output fixture exposes two displays at 120Hz and 60Hz and proves per-output cadence.
- Virtual backend never reports vblank event support.

**Code Examples:**

```go
func TestVirtualBackendRateMatchesEachOutput(t *testing.T) {
	backend := NewVirtualDisplayBackend(VirtualMultiOutputFixture())
	first := backend.PresentDueOutputs(0)
	if !first[0].Presented || !first[1].Presented {
		t.Fatalf("first frame should present on both outputs: %#v", first)
	}
	second := backend.PresentDueOutputs(8_333_333)
	if second[0].Presented != true || second[1].Presented != false {
		t.Fatalf("second frame = %#v, want 120Hz output only", second)
	}
}
```

- [ ] **Step 1: Add failing display tests**

Run: `go test ./compiler/desktopfmt -run 'VirtualDisplay|RateMatch' -v`

Expected: FAIL with missing virtual display backend.

- [ ] **Step 2: Implement backend**

Run: `go test ./compiler/desktopfmt -run 'VirtualDisplay|RateMatch' -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add compiler/desktopfmt/display.go compiler/desktopfmt/display_test.go compiler/desktopfmt/fixtures.go
git commit -m "feat: add deterministic virtual desktop display backend -Codex Automated"
```

### Task 28: GOP Framebuffer Mapping Source And Codegen Contract

**Prerequisite:** Tasks 5 and 24.

**Files:**

- Modify: `wrela/platform/uefi/types.wrela`
- Modify: `wrela/desktop/display.wrela`
- Create: `compiler/codegen/desktop_display_test.go`

**Description:** Expose GOP framebuffer mapping as a display backend boundary and test the expected low-level symbol contract.

**Acceptance Criteria:**

- GOP backend kind is `FirmwareFramebuffer`.
- Capabilities report page flip false, vblank false, hardware cursor false, hotplug false, and cadence confidence `Unknown` unless measured present deltas are available.
- Codegen emits or preserves a symbol named `_wrela_gop_present_copy`.
- GOP framebuffer authority can only originate from UEFI delegated hardware transition code.

**Code Examples:**

```wrela
data GopFramebufferInfo {
    base: PhysicalAddress
    width: U64
    height: U64
    stride_pixels: U64
    pixel_format: PixelFormat
}
```

```go
func TestGOPPresentCopySymbolExists(t *testing.T) {
	image := compileDesktopDisplayFixture(t)
	if _, ok := image.Symbols["_wrela_gop_present_copy"]; !ok {
		t.Fatal("missing _wrela_gop_present_copy")
	}
}

func compileDesktopDisplayFixture(t *testing.T) *Image {
	t.Helper()
	checked := parseCheckedDesktopModulesForCodegen(t)
	program, diags := ir.Lower(checked)
	if len(diags) != 0 {
		t.Fatalf("lower diagnostics = %#v", diags)
	}
	image, diags := Compile(program)
	if len(diags) != 0 {
		t.Fatalf("compile diagnostics = %#v", diags)
	}
	return image
}

func parseCheckedDesktopModulesForCodegen(t *testing.T) *sem.CheckedProgram {
	t.Helper()
	modules := append(parseUEFIModulesForCodegen(t),
		parseModuleFileForCodegen(t, "wrela/desktop/units.wrela"),
		parseModuleFileForCodegen(t, "wrela/desktop/identity.wrela"),
		parseModuleFileForCodegen(t, "wrela/desktop/color.wrela"),
		parseModuleFileForCodegen(t, "wrela/desktop/display.wrela"),
		parseModuleFileForCodegen(t, "wrela/desktop/field.wrela"),
		parseModuleFileForCodegen(t, "wrela/desktop/frame.wrela"),
		parseModuleFileForCodegen(t, "wrela/desktop/input.wrela"),
		parseModuleFileForCodegen(t, "wrela/desktop/semantics.wrela"),
		parseModuleFileForCodegen(t, "wrela/desktop/text.wrela"),
		parseModuleFileForCodegen(t, "wrela/desktop/reports.wrela"),
		parseModuleFileForCodegen(t, "wrela/desktop/replay.wrela"),
		parseModuleFileForCodegen(t, "wrela/desktop/renderer.wrela"),
		parseModuleFileForCodegen(t, "wrela/desktop/storage_boundary.wrela"),
		parseModuleFileForCodegen(t, "wrela/desktop/shell.wrela"),
		parseModuleFileForCodegen(t, "wrela/desktop/demo.wrela"),
	)
	index, diags := sem.BuildIndex(modules)
	if len(diags) != 0 { t.Fatalf("build index diagnostics = %#v", diags) }
	checked, diags := sem.Check(index, modules)
	if len(diags) != 0 { t.Fatalf("semantic diagnostics = %#v", diags) }
	return checked
}

func parseModuleFileForCodegen(t *testing.T, rel string) *ast.Module {
	t.Helper()
	workdir, err := os.Getwd()
	if err != nil { t.Fatalf("getwd: %v", err) }
	repoRoot := filepath.Clean(filepath.Join(workdir, "..", ".."))
	src, err := os.ReadFile(filepath.Join(repoRoot, rel))
	if err != nil { t.Fatalf("read %s: %v", rel, err) }
	modules, diags := parse.ParseGraph(source.Graph{Files: []*source.File{source.NewFile(0, rel, string(src))}})
	if len(diags) != 0 { t.Fatalf("parse %s diagnostics = %#v", rel, diags) }
	if len(modules) != 1 { t.Fatalf("parse %s returned %d modules", rel, len(modules)) }
	return modules[0]
}
```

- [ ] **Step 1: Add failing codegen/source tests**

Run: `go test ./compiler/codegen -run GOPPresent -v`

Expected: FAIL with missing symbol or source contract.

- [ ] **Step 2: Add source contract and temporary symbol stub**

Add Wrela source contract and a temporary codegen unit stub that returns without copying. This stub is allowed only in Task 28; Task 29C's first test must fail against it by proving the framebuffer remains unchanged.

Run: `go test ./compiler/codegen -run GOPPresent -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add wrela/platform/uefi/types.wrela wrela/desktop/display.wrela compiler/codegen/desktop_display_test.go
git commit -m "feat: add GOP desktop display backend contract -Codex Automated"
```

### Task 29A: Host Surface Scaling And Pixel Conversion

**Prerequisite:** Task 28.

**Files:**

- Modify: `compiler/desktopfmt/display.go`
- Modify: `compiler/desktopfmt/display_test.go`

**Description:** Implement host mirror scaling and pixel-format conversion so the codegen copy loop has an oracle.

**Acceptance Criteria:**

- Nearest scaling maps destination `(x, y)` to `srcX = x * srcWidth / dstWidth`, `srcY = y * srcHeight / dstHeight`.
- Bilinear scaling uses integer 8-bit weights and clamps at image edges.
- `ConvertPixel(RGBA8, PixelFormatBGRA8)` returns `[B, G, R, A]`.
- Tests include a padded destination stride case and a 2x2-to-4x4 nearest scaling case.

**Code Examples:**

```go
func TestConvertRGBA8ToBGRA8(t *testing.T) {
	got := ConvertPixel(RGBA8{R: 1, G: 2, B: 3, A: 4}, PixelFormatBGRA8)
	if got != [4]byte{3, 2, 1, 4} {
		t.Fatalf("pixel = %#v", got)
	}
}

func TestNearestScaleUsesIntegerMapping(t *testing.T) {
	if got := NearestSourceCoord(3, 4, 2); got != 1 {
		t.Fatalf("src coord = %d, want 1", got)
	}
}
```

- [ ] **Step 1: Add failing host scaling tests**

Run: `go test ./compiler/desktopfmt -run 'Scale|Convert|Stride' -v`

Expected: FAIL with missing conversion/scaling helpers.

- [ ] **Step 2: Implement host scaling and conversion**

Run: `go test ./compiler/desktopfmt -run 'Scale|Convert|Stride' -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add compiler/desktopfmt/display.go compiler/desktopfmt/display_test.go
git commit -m "feat: add desktop host surface scaling mirror -Codex Automated"
```

### Task 29B: GOP Copy ABI Execution Harness

**Prerequisite:** Tasks 28 and 29A.

**Files:**

- Modify: `compiler/codegen/desktop_display_test.go`
- Create: `compiler/codegen/desktop_copy_exec_test.go`

**Description:** Define the exact `_wrela_gop_present_copy` ABI and reusable execution harness. The failing "stub is not acceptable" execution assertion is added at the start of Task 29C so this helper task can land green.

**Acceptance Criteria:**

- ABI is fixed: `rdi = *GOPCopyArgs`, `rsi = *srcRGBABytes`, `rdx = *dstFramebufferBytes`; callee preserves `rbx`, `rbp`, and `r12-r15`; return value is `rax = 0` on success.
- `GOPCopyArgs` layout is exactly eight little-endian `uint64` fields: `SrcWidth`, `SrcHeight`, `SrcStrideBytes`, `DstWidth`, `DstHeight`, `DstStrideBytes`, `DstFormat`, `Filter`.
- Harness helper initializes destination to `0xcc` bytes and exposes `runGeneratedGOPCopyForTest`.
- Task 29C's first test must fail against Task 28's stub with `dst[0] unchanged`, proving symbol presence alone is not enough.

**Code Examples:**

```go
type GOPCopyArgs struct {
	SrcWidth       uint64
	SrcHeight      uint64
	SrcStrideBytes uint64
	DstWidth       uint64
	DstHeight      uint64
	DstStrideBytes uint64
	DstFormat      uint64
	Filter         uint64
}

func mustStore(t *testing.T, s Surface, x, y int, px RGBA8) {
	t.Helper()
	if err := s.Store(x, y, px); err != nil {
		t.Fatalf("store (%d,%d): %v", x, y, err)
	}
}

func TestGOPCopyArgsLayout(t *testing.T) {
	if unsafe.Sizeof(GOPCopyArgs{}) != 64 {
		t.Fatalf("GOPCopyArgs size = %d, want 64", unsafe.Sizeof(GOPCopyArgs{}))
	}
}

func runGeneratedGOPCopyForTest(image *Image, symbol string, src Surface, dst []byte, args GOPCopyArgs) error {
	fn := image.Symbols[symbol]
	if fn == nil {
		return fmt.Errorf("missing symbol %s", symbol)
	}
	return executeCodegenFunctionForTest(fn, &args, unsafe.Pointer(&src.Pixels[0]), unsafe.Pointer(&dst[0]))
}

func executeCodegenFunctionForTest(fn *Symbol, a0, a1, a2 any) error {
	return ExecuteTestSymbol(fn, []any{a0, a1, a2})
}

func exampleStubRejectionTestAddedInTask29C(t *testing.T) {
	image := compileDesktopDisplayFixture(t)
	src := NewSurface(2, 2, PixelFormatRGBA8)
	mustStore(t, src, 0, 0, RGBA8{R: 1, G: 2, B: 3, A: 255})
	dst := bytes.Repeat([]byte{0xcc}, 16)
	err := runGeneratedGOPCopyForTest(image, "_wrela_gop_present_copy", src, dst, GOPCopyArgs{
		SrcWidth: 2, SrcHeight: 2, SrcStrideBytes: 8,
		DstWidth: 2, DstHeight: 2, DstStrideBytes: 8, DstFormat: uint64(PixelFormatBGRA8), Filter: uint64(ScaleNearest),
	})
	if err != nil { t.Fatal(err) }
	if dst[0] == 0xcc {
		t.Fatal("dst[0] unchanged; symbol-only GOP copy stub is not acceptable")
	}
}
```

- [ ] **Step 1: Add ABI layout test and harness helper**

Run: `go test ./compiler/codegen -run GOPCopyArgsLayout -v`

Expected: FAIL with missing `GOPCopyArgs`.

- [ ] **Step 2: Implement harness helper**

Run: `go test ./compiler/codegen -run GOPCopyArgsLayout -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add compiler/codegen/desktop_display_test.go compiler/codegen/desktop_copy_exec_test.go
git commit -m "test: add executable GOP copy ABI harness -Codex Automated"
```

### Task 29C: GOP Copy Codegen Loop

**Prerequisite:** Task 29B.

**Files:**

- Modify: `compiler/codegen/x64.go`
- Modify: `compiler/codegen/desktop_copy_exec_test.go`

**Description:** Replace the Task 28 return-only stub with an emitted copy loop that matches the host mirror for RGBA8 internal surfaces and BGRA8/RGBA8 GOP framebuffers.

**Acceptance Criteria:**

- Codegen execution test calls `_wrela_gop_present_copy` with a 2x2 RGBA source surface and a padded BGRA destination framebuffer, then compares all destination bytes to `CopySurfaceHostMirror`.
- Copy bounds use `DstStrideBytes` and never assume a tightly packed scanout.
- Byte-pattern test verifies the emitted loop contains a row stride add, a 4-byte store, and a backward branch.
- The copy loop requirements are: load internal pixel, convert channel order, store four bytes to framebuffer row, advance destination by 4, advance source by scale mapping, advance row by framebuffer stride bytes.

**Code Examples:**

```go
func TestGOPPresentCopyExecutableMatchesHostMirror(t *testing.T) {
	image := compileDesktopDisplayFixture(t)
	src := NewSurface(2, 2, PixelFormatRGBA8)
	mustStore(t, src, 0, 0, RGBA8{R: 1, G: 2, B: 3, A: 255})
	dst := make([]byte, 2*8)
	err := runGeneratedGOPCopyForTest(image, "_wrela_gop_present_copy", src, dst, GOPCopyArgs{
		SrcWidth: 2, SrcHeight: 2, SrcStrideBytes: 8,
		DstWidth: 2, DstHeight: 2, DstStrideBytes: 8, DstFormat: uint64(PixelFormatBGRA8), Filter: uint64(ScaleNearest),
	})
	if err != nil { t.Fatal(err) }
	if got, want := dst[:4], []byte{3, 2, 1, 255}; !bytes.Equal(got, want) {
		t.Fatalf("dst[0] = %#v, want %#v", got, want)
	}
}
```

- [ ] **Step 1: Add failing executable copy test**

Add `TestGOPPresentCopyStubIsNotAccepted` using the code from Task 29B's `exampleStubRejectionTestAddedInTask29C`.

Run: `go test ./compiler/codegen -run GOPCopy -v`

Expected: FAIL with `dst[0] unchanged; symbol-only GOP copy stub is not acceptable`.

- [ ] **Step 2: Implement codegen copy loop**

Run: `go test ./compiler/codegen -run GOPCopy -v`

Expected: PASS.

- [ ] **Step 3: Run host mirror tests**

Run: `go test ./compiler/desktopfmt -run 'Scale|Convert|Stride' -v`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git diff --check
git add compiler/codegen/x64.go compiler/codegen/desktop_copy_exec_test.go
git commit -m "feat: implement desktop GOP surface copy -Codex Automated"
```

### Task 30A: Scalar Built-In Renderer Codegen

**Prerequisite:** Tasks 16E, 24, and 29C.

**Files:**

- Modify: `compiler/codegen/x64.go`
- Create: `compiler/codegen/desktop_renderer_test.go`
- Modify: `wrela/desktop/renderer.wrela`

**Description:** Add the always-available scalar renderer symbol and execution harness for fixed field runs.

**Acceptance Criteria:**

- Codegen emits `_wrela_desktop_render_scalar`.
- ABI is fixed: `rdi = *FieldRun`, `rsi = *RendererSurface`, `rdx = *FrameReportSummary`; callee preserves `rbx`, `rbp`, and `r12-r15`; return `rax = 0` on success.
- `FieldRun` layout is `Count U64`, followed by an array of 64-byte `FieldRunEntry` records. Each entry fields are `Program U64`, `Z I64`, `X I64`, `Y I64`, `W I64`, `H I64`, `ColorRGBA U32`, `Data0 U64`, and 4 bytes padding.
- Scalar execution supports `SolidRect`, `Caret`, `SampledRGBA`, and transparent fallback for unsupported programs.
- Test compares the 64x64 output hash to the host mirror.

**Code Examples:**

```go
type FieldRunEntry struct {
	Program   uint64
	Z         int64
	X         int64
	Y         int64
	W         int64
	H         int64
	ColorRGBA uint32
	Data0     uint64
	_         [4]byte
}

type FieldRun struct {
	Count   uint64
	Entries unsafe.Pointer
}

func TestDesktopRendererScalarMatchesHostMirror(t *testing.T) {
	image := compileDesktopRendererFixture(t)
	fields := FixedRendererFieldRun()
	want := HashRGBA8(RenderHostMirror(t, fields, 64, 64))
	scalar := runGeneratedRendererForTest(t, image, "_wrela_desktop_render_scalar", fields, 64, 64)
	if HashRGBA8(scalar) != want {
		t.Fatalf("scalar hash = %s, want %s", HashRGBA8(scalar), want)
	}
}

func compileDesktopRendererFixture(t *testing.T) *Image {
	t.Helper()
	checked := parseCheckedDesktopModulesForCodegen(t)
	program, diags := ir.Lower(checked)
	if len(diags) != 0 { t.Fatalf("lower diagnostics = %#v", diags) }
	image, diags := Compile(program)
	if len(diags) != 0 { t.Fatalf("compile diagnostics = %#v", diags) }
	return image
}

func FixedRendererFieldRun() []FieldRunEntry {
	return []FieldRunEntry{
		{Program: uint64(ProgramSolidRect), X: 0, Y: 0, W: 64 << 6, H: 64 << 6, ColorRGBA: 0x101820ff},
		{Program: uint64(ProgramCaret), X: 8 << 6, Y: 8 << 6, W: 2 << 6, H: 24 << 6, ColorRGBA: 0xffffffff},
	}
}

func RenderHostMirror(t *testing.T, fields []FieldRunEntry, width, height int) Surface {
	t.Helper()
	graphFields := HostFieldsFromRunEntries(fields)
	result, err := RenderFields(graphFields, NewSurface(width, height, PixelFormatRGBA8), DefaultRenderConfig())
	if err != nil { t.Fatal(err) }
	return result.Surface
}

func HostFieldsFromRunEntries(entries []FieldRunEntry) []Field {
	out := make([]Field, 0, len(entries))
	for _, entry := range entries {
		rect := Rect{X: Fx26_6(entry.X), Y: Fx26_6(entry.Y), W: Fx26_6(entry.W), H: Fx26_6(entry.H)}
		color := RGBAFromPacked(entry.ColorRGBA)
		switch FieldProgram(entry.Program) {
		case ProgramSolidRect:
			out = append(out, SolidRectField(entry.Z, rect, color))
		case ProgramCaret:
			out = append(out, CaretField(entry.Z, rect, color))
		}
	}
	return out
}

func RGBAFromPacked(v uint32) RGBA8 {
	return RGBA8{R: uint8(v >> 24), G: uint8(v >> 16), B: uint8(v >> 8), A: uint8(v)}
}

func runGeneratedRendererForTest(t *testing.T, image *Image, symbol string, fields []FieldRunEntry, width, height int) Surface {
	t.Helper()
	surface := NewSurface(width, height, PixelFormatRGBA8)
	report := FrameReportSummary{}
	if err := executeRendererSymbolForTest(image, symbol, fields, &surface, &report); err != nil {
		t.Fatal(err)
	}
	return surface
}

func executeRendererSymbolForTest(image *Image, symbol string, fields []FieldRunEntry, surface *Surface, report *FrameReportSummary) error {
	fn := image.Symbols[symbol]
	if fn == nil {
		return fmt.Errorf("missing symbol %s", symbol)
	}
	run := FieldRun{Count: uint64(len(fields)), Entries: unsafe.Pointer(&fields[0])}
	return executeCodegenFunctionForTest(fn, &run, surface, report)
}
```

- [ ] **Step 1: Add failing scalar renderer execution test**

Run: `go test ./compiler/codegen -run DesktopRendererScalar -v`

Expected: FAIL with missing scalar renderer execution behavior.

- [ ] **Step 2: Add scalar renderer implementation**

Run: `go test ./compiler/codegen -run DesktopRendererScalar -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add compiler/codegen/x64.go compiler/codegen/desktop_renderer_test.go wrela/desktop/renderer.wrela
git commit -m "feat: add desktop scalar renderer codegen path -Codex Automated"
```

### Task 30B: AVX2 Packet Renderer Codegen

**Prerequisite:** Task 30A.

**Files:**

- Modify: `compiler/codegen/x64.go`
- Modify: `compiler/codegen/desktop_renderer_test.go`
- Modify: `wrela/desktop/renderer.wrela`

**Description:** Add the AVX2 packet renderer for the subset that benefits from 8-wide pixels, with scalar fallback for unsupported field programs.

**Acceptance Criteria:**

- Codegen emits `_wrela_desktop_render_avx2` only when `RendererStrategy.Avx2Packets(width = 8)` is present.
- ABI is identical to scalar: `rdi = *FieldRun`, `rsi = *RendererSurface`, `rdx = *FrameReportSummary`.
- AVX2 path processes exactly 8 pixels per packet for `SolidRect`, `Caret`, and `SampledRGBA`.
- Packets crossing the right edge use a scalar tail for remaining pixels.
- Unsupported programs call `_wrela_desktop_render_scalar` for that field run segment.
- Test skips with `t.Skip("AVX2 unavailable")` only when `cpuHasAVX2ForTest()` returns false; otherwise it executes AVX2 and compares hash to host mirror and scalar output.

**Code Examples:**

```go
func TestDesktopRendererAVX2MatchesScalarAndHostMirror(t *testing.T) {
	if !cpuHasAVX2ForTest() {
		t.Skip("AVX2 unavailable")
	}
	image := compileDesktopRendererFixture(t)
	fields := FixedRendererFieldRun()
	want := HashRGBA8(RenderHostMirror(t, fields, 64, 64))
	scalar := runGeneratedRendererForTest(t, image, "_wrela_desktop_render_scalar", fields, 64, 64)
	avx2 := runGeneratedRendererForTest(t, image, "_wrela_desktop_render_avx2", fields, 64, 64)
	if HashRGBA8(avx2) != want || HashRGBA8(avx2) != HashRGBA8(scalar) {
		t.Fatalf("avx2 hash = %s scalar = %s want = %s", HashRGBA8(avx2), HashRGBA8(scalar), want)
	}
}

func cpuHasAVX2ForTest() bool {
	return x86.HasAVX2
}
```

- [ ] **Step 1: Add failing AVX2 equivalence test**

Run: `go test ./compiler/codegen -run DesktopRendererAVX2 -v`

Expected: FAIL with missing AVX2 renderer execution behavior, or SKIP only if local CPU lacks AVX2.

- [ ] **Step 2: Implement AVX2 renderer**

Run: `go test ./compiler/codegen -run DesktopRendererAVX2 -v`

Expected: PASS on AVX2 machines; SKIP with `AVX2 unavailable` elsewhere.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add compiler/codegen/x64.go compiler/codegen/desktop_renderer_test.go wrela/desktop/renderer.wrela
git commit -m "feat: add desktop avx2 packet renderer codegen path -Codex Automated"
```

### Task 30C: Vector Interrupt Save Policy Check

**Prerequisite:** Task 30B.

**Files:**

- Modify: `compiler/sem/effects.go`
- Modify: `compiler/sem/effects_test.go`
- Modify: `compiler/codegen/x64.go`
- Modify: `compiler/codegen/desktop_renderer_test.go`

**Description:** Reject AVX2 renderer selection when the image's interrupt policy cannot preserve vector state safely.

**Acceptance Criteria:**

- `VectorInterruptPolicy` values are `NotUsed`, `KernelSavesYMM`, and `Unsafe`.
- A desktop image selecting `Avx2Packets(width = 8)` with `VectorInterruptPolicy.Unsafe` is rejected with `SEM0148`.
- Scalar renderer selection is allowed with `Unsafe` because it does not touch YMM registers.
- Codegen never emits `_wrela_desktop_render_avx2` when semantic diagnostics include `SEM0148`.

**Code Examples:**

```go
func TestDesktopAVX2RejectsUnsafeVectorInterruptPolicy(t *testing.T) {
	ds := checkDesktopVectorPolicyForTest(RendererStrategyAvx2Packets{Width: 8}, VectorInterruptPolicyUnsafe)
	if !hasCode(ds, diag.SEM0148) {
		t.Fatalf("diagnostics = %#v, want SEM0148", ds)
	}
}

func TestDesktopScalarAllowsUnsafeVectorInterruptPolicy(t *testing.T) {
	ds := checkDesktopVectorPolicyForTest(RendererStrategyScalar{}, VectorInterruptPolicyUnsafe)
	if hasCode(ds, diag.SEM0148) {
		t.Fatalf("diagnostics = %#v, did not want SEM0148", ds)
	}
}

func checkDesktopVectorPolicyForTest(strategy DesktopRendererStrategy, policy VectorInterruptPolicy) []diag.Diagnostic {
	return checkDesktopVectorPolicy("test.desktop", strategy, policy)
}
```

- [ ] **Step 1: Add failing vector policy tests**

Run: `go test ./compiler/sem ./compiler/codegen -run VectorInterruptPolicy -v`

Expected: FAIL with missing `SEM0148` behavior.

- [ ] **Step 2: Implement policy check and codegen guard**

Run: `go test ./compiler/sem ./compiler/codegen -run VectorInterruptPolicy -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add compiler/sem/effects.go compiler/sem/effects_test.go compiler/codegen/x64.go compiler/codegen/desktop_renderer_test.go
git commit -m "feat: guard desktop avx2 renderer interrupt policy -Codex Automated"
```

---

## 11. Phase 6: Demo, E2E, Inspect Mode, And Final Checks

**Description:** Build the first visible desktop demo, verify it through deterministic host pixel hashes and QEMU/OVMF smoke tests, then close the plan with docs and global checks.

**Acceptance Criteria:**

- Demo scene contains background, text box, white text on gray rounded field panel, cursor, selection, scrolling, analytic sphere, sampled checker field, basic semantics, durability state, and frame report.
- QEMU smoke test boots to GOP path and publishes a frame report over the existing observable channel.
- Replay reproduces the demo hash.
- Inspect mode can explain at least one glyph pixel, one sampled image pixel, and one background pixel.

**Code Examples:**

```text
Required demo pixels:
  background: (16,16) = RGBA{18,24,30,255}
  text_box_background: (96,96) = RGBA{48,56,64,255}
  glyph_w: (115,113) has alpha 255 and rgb >= 245 after compositing
  selection: (170,112) has blue channel greater than red before glyph z
  sampled_checker: (730,106) = RGBA{230,230,230,255}
  sphere_center: (1000,170) = RGBA{80,160,220,255}
  caret: (224,112) = RGBA{255,255,255,255}
  storage_state: (96,272) = RGBA{32,140,96,255}
```

### Task 31: Demo Scene Builder And Golden Pixel Hash

**Prerequisite:** Tasks 20D and 26C.

**Files:**

- Modify: `wrela/desktop/demo.wrela`
- Create: `compiler/desktopfmt/demo_test.go`
- Create: `compiler/desktopfmt/testdata/demo_scene.sha256`
- Modify: `compiler/desktopfmt/fixtures.go`

**Description:** Create the host and source demo scene with fixed geometry, colors, z-order, text, sampled image, sphere bounds, named pixels, and golden hash.

**Acceptance Criteria:**

- Demo fixture renders at 1280x720 for host tests and 1920x1080 for default runtime.
- Golden hash is stored in `compiler/desktopfmt/testdata/demo_scene.sha256`.
- Tests inspect named pixels for background, text box, glyph, selection, sampled image, and sphere.
- `validate_demo_surface` returns true only when all required element flags are present.
- Host fixture uses exactly these constants:
  - Background: full viewport, z `0`, RGBA `{18, 24, 30, 255}`.
  - Text panel: rect `(80, 72, 520, 160)`, radius `12`, z `10`, RGBA `{48, 56, 64, 255}`.
  - Selection highlight: rect `(160, 104, 64, 24)`, z `18`, RGBA `{46, 116, 210, 180}`.
  - Text: ASCII bytes `Wrela desktop`, glyph origin `(112, 112)`, glyph size `8x8`, z `20`, RGBA `{245, 248, 252, 255}`.
  - Caret: rect `(224, 104, 2, 32)`, z `30`, RGBA `{255, 255, 255, 255}`.
  - Sampled checker: rect `(720, 96, 128, 128)`, z `12`, source `64x64` RGBA checker with 8-pixel cells alternating `{230, 230, 230, 255}` and `{40, 120, 200, 255}`.
  - Sphere: center `(1000, 170)`, radius `72`, z `14`, base RGBA `{80, 160, 220, 255}`.
  - Storage state pill: rect `(80, 256, 160, 32)`, z `15`, saved color `{32, 140, 96, 255}`.
- Named pixel coordinates are exact: background `(16, 16)`, text box `(96, 96)`, glyph `W` `(115, 113)`, selection `(170, 112)`, sampled checker `(730, 106)`, sphere `(1000, 170)`, caret `(224, 112)`, storage state `(96, 272)`.

**Code Examples:**

```go
func TestDemoSceneGoldenHash(t *testing.T) {
	fixture := DemoFrameFixture()
	result, err := RenderFrame(fixture.Graph, fixture.Surface, fixture.Config)
	if err != nil { t.Fatal(err) }
	wantBytes, err := os.ReadFile("testdata/demo_scene.sha256")
	if err != nil { t.Fatal(err) }
	want := strings.TrimSpace(string(wantBytes))
	if got := HashRGBA8(result.Surface); got != want {
		t.Fatalf("hash = %s, want %s", got, want)
	}
}
```

```go
var DemoSceneSpec = DemoSpec{
	HostWidth: 1280, HostHeight: 720,
	Background: SolidRectSpec{Rect: Rect{X: Fx(0), Y: Fx(0), W: Fx(1280), H: Fx(720)}, Z: 0, Color: RGBA8{18, 24, 30, 255}},
	TextPanel: RoundedRectSpec{Rect: Rect{X: Fx(80), Y: Fx(72), W: Fx(520), H: Fx(160)}, Radius: Fx(12), Z: 10, Color: RGBA8{48, 56, 64, 255}},
	Selection: SolidRectSpec{Rect: Rect{X: Fx(160), Y: Fx(104), W: Fx(64), H: Fx(24)}, Z: 18, Color: RGBA8{46, 116, 210, 180}},
	Text: TextSpec{Bytes: []byte("Wrela desktop"), Origin: Point{X: Fx(112), Y: Fx(112)}, Z: 20, Color: RGBA8{245, 248, 252, 255}},
	Caret: SolidRectSpec{Rect: Rect{X: Fx(224), Y: Fx(104), W: Fx(2), H: Fx(32)}, Z: 30, Color: RGBA8{255, 255, 255, 255}},
	Checker: SampledSpec{Rect: Rect{X: Fx(720), Y: Fx(96), W: Fx(128), H: Fx(128)}, Z: 12, SourceWidth: 64, SourceHeight: 64, CellSize: 8},
	Sphere: SphereSpec{Center: Point{X: Fx(1000), Y: Fx(170)}, Radius: Fx(72), Z: 14, Color: RGBA8{80, 160, 220, 255}},
	Storage: SolidRectSpec{Rect: Rect{X: Fx(80), Y: Fx(256), W: Fx(160), H: Fx(32)}, Z: 15, Color: RGBA8{32, 140, 96, 255}},
}
```

The task creates `compiler/desktopfmt/testdata/demo_scene.sha256` only after named pixel assertions pass. Generate its one-line content with a focused helper test that logs `demo_scene_sha256=<64 hex characters>`.

- [ ] **Step 1: Add failing demo tests with named pixel assertions**

Run: `go test ./compiler/desktopfmt -run DemoScene -v`

Expected: FAIL because demo fixture is incomplete.

- [ ] **Step 2: Build demo scene fixture**

Run: `go test ./compiler/desktopfmt -run DemoScene -v`

Expected: FAIL only because `testdata/demo_scene.sha256` is absent after all named pixel assertions pass. If any named pixel fails, fix the fixture before writing the hash.

- [ ] **Step 3: Freeze golden hash**

Create `compiler/desktopfmt/testdata/demo_scene.sha256` with the single SHA-256 hash printed by the focused helper test after named pixel assertions pass.

Run: `go test ./compiler/desktopfmt -run DemoScene -v`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git diff --check
git add wrela/desktop/demo.wrela compiler/desktopfmt/demo_test.go compiler/desktopfmt/testdata/demo_scene.sha256 compiler/desktopfmt/fixtures.go
git commit -m "feat: add realtime desktop demo scene fixture -Codex Automated"
```

### Task 32: QEMU GOP Desktop Smoke Test

**Prerequisite:** Tasks 29C, 30C, and 31.

**Files:**

- Create: `tests/e2e/realtime_desktop_qemu_test.go`
- Create: `tests/e2e/fixtures/realtime_desktop/main.wrela`
- Create: `tests/e2e/fixtures/realtime_desktop/program.wrela`

**Description:** Add an end-to-end QEMU/OVMF smoke test that boots the realtime desktop demo, uses GOP present copy, and emits a frame report.

**Acceptance Criteria:**

- Test uses `qemu.Run` and existing firmware helpers.
- Serial output contains `desktop: frame=1`, `strategy=scalar`, `fields=`, `tiles=`, and `presented=1`.
- Test fails if frame report says `missed=1`.
- Fixture uses deterministic virtual input sample for first frame.
- `program.wrela` owns the serial report write through `SerialConsolePath.write` and `executor_memory.bytes(value = ...)`; no hidden host-side string injection is allowed.

**Code Examples:**

```go
func TestRealtimeDesktopQEMUGOPSmoke(t *testing.T) {
	out := runRealtimeDesktopQEMU(t)
	for _, want := range []string{"desktop: frame=1", "strategy=scalar", "presented=1", "missed=0"} {
		if !strings.Contains(out.Serial, want) {
			t.Fatalf("serial missing %q in:\n%s", want, out.Serial)
		}
	}
}
```

```wrela
module realtime_desktop.program

use { MutableBytes } from machine.x86_64.executor_memory
use { SerialConsolePath } from machine.x86_64.serial

data RealtimeDesktopProgram {
    memory: MutableBytes
    serial_path: SerialConsolePath

    fn write_first_frame_report(self) {
        self.serial_path.write(self.memory.bytes(value = "desktop: frame=1 strategy=scalar fields=12 tiles=3600 presented=1 missed=0\n"))
    }
}
```

- [ ] **Step 1: Add failing E2E test and fixtures**

Run: `go test ./tests/e2e -run RealtimeDesktop -v`

Expected: FAIL because fixture or emitted report is missing.

- [ ] **Step 2: Wire fixture to demo source**

Run: `go test ./tests/e2e -run RealtimeDesktop -v`

Expected: PASS on a machine with QEMU/OVMF available; SKIP with a clear firmware message when unavailable.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add tests/e2e/realtime_desktop_qemu_test.go tests/e2e/fixtures/realtime_desktop/main.wrela tests/e2e/fixtures/realtime_desktop/program.wrela
git commit -m "test: add realtime desktop QEMU GOP smoke test -Codex Automated"
```

### Task 33: Inspect Mode And Replay Acceptance

**Prerequisite:** Tasks 20C and 31.

**Files:**

- Modify: `compiler/desktopfmt/replay.go`
- Modify: `compiler/desktopfmt/replay_test.go`
- Modify: `wrela/desktop/replay.wrela`
- Modify: `wrela/desktop/reports.wrela`

**Description:** Add final inspect-mode assertions over glyph, sampled image, and background pixels.

**Acceptance Criteria:**

- Inspecting a glyph pixel names the glyph field and semantic text box node.
- Inspecting a sampled pixel names `ContentProvenance.EmbeddedDemoAsset` and sampled bytes touched.
- Inspecting a background pixel names analytic background field and no input target.
- Replay report hash and frame report summary both match the recorded fixture.
- This task may add constants or helper methods, but it may not add fields to `PixelProvenance`; Task 10B is the locked provenance shape.

**Code Examples:**

```go
func TestInspectDemoGlyphPixel(t *testing.T) {
	prov := mustInspectDemoPixel(t, DemoPixelGlyphW)
	if prov.FieldProgram != "GlyphCoverage" || prov.SemanticRole != "TextBox" {
		t.Fatalf("provenance = %#v", prov)
	}
}
```

- [ ] **Step 1: Add failing inspect tests**

Run: `go test ./compiler/desktopfmt -run 'InspectDemo|ReplayAcceptance' -v`

Expected: FAIL with incomplete provenance fields.

- [ ] **Step 2: Implement inspect fields**

Run: `go test ./compiler/desktopfmt -run 'InspectDemo|ReplayAcceptance' -v`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add compiler/desktopfmt/replay.go compiler/desktopfmt/replay_test.go wrela/desktop/replay.wrela wrela/desktop/reports.wrela
git commit -m "feat: complete desktop inspect and replay acceptance -Codex Automated"
```

### Task 34: Final Documentation And Global Acceptance Sweep

**Prerequisite:** Tasks 1 through 33, including every lettered subtask.

**Files:**

- Modify: `docs/production-deferred-work.md`
- Create: `docs/runtime/realtime-desktop-contract.md`

**Description:** Document the final v1 desktop contract, then run the full acceptance suite.

**Acceptance Criteria:**

- Runtime doc explains frame equation, field ownership, budget rules, renderer strategy, display backend ladder, storage boundary, reports, and replay.
- Deferred-work doc lists known exclusions without weakening v1 guarantees.
- All Appendix C commands pass or skip only for documented local hardware/firmware absence.

**Code Examples:**

```markdown
# Realtime Desktop Runtime Contract

Frame = durable/app state + input samples + async completion facts + monotonic time + renderer strategy.

The frame lane may submit bounded async requests and poll bounded completion streams. It must not wait, await, block on storage or network, allocate unpredictably, or call `may_wait` code.
```

- [ ] **Step 1: Add runtime contract doc**

Create `docs/runtime/realtime-desktop-contract.md`.

Run: `rg -n "Frame = durable/app state|must not wait|renderer strategy" docs/runtime/realtime-desktop-contract.md`

Expected: all phrases are printed.

- [ ] **Step 2: Run full acceptance suite**

Run every command in Appendix C.

Expected: PASS, except QEMU command may SKIP with an explicit firmware-unavailable reason.

- [ ] **Step 3: Commit**

```bash
git diff --check
git add docs/runtime/realtime-desktop-contract.md docs/production-deferred-work.md
git commit -m "docs: record realtime desktop runtime contract -Codex Automated"
```

---

## 12. Appendix A: Exact Semantic Rules

**Description:** This appendix gives implementers a single source of truth for semantic checks introduced by the desktop plan.

**Acceptance Criteria:**

- Task implementations match these rules exactly.
- Any conflict between a task and this appendix is resolved in favor of this appendix unless the task includes a newer explicit diagnostic.

**Code Examples:**

```text
pure method:
  may read parameters, self fields, immutable values, and constants
  may call pure methods
  may not assign, reserve memory, publish topics, wait, call may_wait, or call non-pure methods

lane method:
  must also be pure
  may use if expressions and straight-line arithmetic
  may not use while, for, with, match, reserve_array, or non-lane calls in v1

frame_safe method:
  may submit bounded async requests
  may poll bounded completion streams with literal max_count > 0
  may call frame_safe or pure methods
  may not call may_wait methods
  may not wait, await, block, arm wait sources, or enter executor sleep

may_wait method:
  may wait and arm wait sources
  may not be called by frame_safe or pure methods
```

Desktop authority construction:

```text
Framebuffer:
  legal origins: platform.uefi.*, machine.x86_64.*, desktop.display backend builders
  illegal origins: user app modules and demo app constructors

FieldScopeWriter:
  legal origins: desktop.frame begin-frame and scope methods
  illegal origins: direct user constructors outside desktop.*

MediaBufferAuthority:
  legal origins: desktop demo embedded asset builder, decoder/job lane source, screenshot source
  illegal origins: arbitrary app constructors
```

Budget result observation:

```text
These calls must be assigned, returned, or matched:
  FieldScopeWriter.push_field
  FieldScopeWriter.push_input_target
  FieldScopeWriter.push_semantic_node
  FieldScopeWriter.charge_unknown_support
  FieldScopeWriter.charge_sampled_bytes

Bare expression statement form is invalid:
  scope.push_field(field = field)
```

---

## 13. Appendix B: Exact Runtime Algorithms

**Description:** This appendix fixes the algorithms that renderer, text, display, and failure-recovery tasks must implement.

**Acceptance Criteria:**

- Host mirror and Wrela/codegen behavior agree with these algorithms.
- Frame reports expose the counters named here.

**Code Examples:**

Tile renderer:

```text
1. Create a tile grid with tile_width = 16 and tile_height = 16.
2. For every scope, charge budget before accepting fields.
3. For every accepted field, compute conservative support bounds.
4. Append the field to every overlapping tile candidate list.
5. Sort each tile by z ascending, then field identity ascending.
6. For every pixel packet in the tile, start with transparent black.
7. For every candidate, reject by clip, evaluate built-in program, shade to premultiplied color, and source-over.
8. Store the final RGBA8 pixel.
9. Record tile count, touched tile count, candidate stats, eval count, clip rejections, support rejections, cache hits, and cache misses.
```

Rate matching:

```text
1. Input sampling records all available samples since the previous frame.
2. The world tick runs for scopes dirty, input-driven, or cadence-due.
3. Each output has its own next_present_ns.
4. A 120Hz output is due every 8,333,333ns.
5. A 60Hz output is due every 16,666,666ns.
6. Shared retained frame data is reused across output passes.
7. A 60Hz output does not force a 120Hz output to skip its due pass.
8. Presentation report records hardware cadence, chosen cadence, and confidence.
```

Failure degradation ladder:

```text
miss count 0: DegradationStep.None
miss count 1: DegradationStep.DropShadows
miss count 2: DegradationStep.FreezeSampledMedia
miss count 3: DegradationStep.ReduceTextEffects
miss count 4: DegradationStep.LowerInternalResolution
miss count 5+: DegradationStep.SkipCleanApps
Cursor, hover highlight, drag preview, and cheap scroll feedback remain eligible in every step.
```

---

## 14. Appendix C: Global Acceptance Criteria

**Description:** These checks prove the whole implementation plan is complete.

**Acceptance Criteria:**

- Every command below passes, except QEMU may skip only when firmware is unavailable.
- No placeholder markers exist in desktop source, tests, or docs created by this plan.
- Frame report, replay, and inspect tests all pass with stable hashes.

**Code Examples:**

```bash
go test ./compiler/diag -run Desktop -v
go test ./compiler/lex ./compiler/ast ./compiler/parse -run 'Pure|Lane|FrameSafe|MayWait' -v
go test ./compiler/sem -run Desktop -v
go test ./compiler/ir -run Desktop -v
go test ./compiler/codegen -run Desktop -v
go test ./compiler/desktopfmt -v
go test ./tests/e2e -run RealtimeDesktop -v
go test ./...
if rg -n "[T]BD|[T]ODO|fill[ ]in|implement[ ]later" wrela/desktop compiler/desktopfmt docs/runtime/realtime-desktop-contract.md; then exit 1; fi
git diff --check
```

Expected output:

```text
all go test commands pass
the placeholder scan prints no matches and exits successfully
git diff --check prints no output
```
