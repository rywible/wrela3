# Physical Arena Memory Model Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace Wrela's toy executor-memory surface with a direct-physical, statically enforced, hierarchical arena model where durable executor state lives in root arenas, temporary work lives in bounded `with` frames, and cache memory evicts by default instead of crashing the program.

**Architecture:** Wrela has no source-level virtual-memory model in this plan. The language and compiler reason about physical regions and memory authorities directly; the x86_64 backend keeps only the minimal 2 MiB identity paging required by long mode as target boot glue. Arena lifetime is source-visible through `with arena.frame(length = N) as heap { ... }`, values are placed with `arena.place(T(...))`, raw bytes are reserved with `arena.reserve(...)`, and the semantic checker rejects frame escapes, invalid region flows, and hidden memory authority creation.

**Tech Stack:** Go 1.22+; existing hand-written lexer/parser; existing semantic checker; existing IR and direct x86_64 codegen; Wrela source modules under `wrela/`; Go tests plus Wrela negative fixtures; QEMU q35 + OVMF for end-to-end checks.

---

## 0. How To Execute This Plan

This plan is written for parallel implementation by engineers who know Go but may not know Wrela's compiler internals.

For junior execution:

- Follow task prerequisites exactly.
- Do not change public names, diagnostic codes, or Wrela syntax from this document.
- Every task ends with a commit whose message ends in `-Codex Automated`.
- When a task says to run a command, run exactly that command.
- If a failing-test step passes unexpectedly, stop and inspect the existing implementation before editing.
- If a passing-test step fails, keep working inside that task until the command passes.
- Do not run `go test ./...` between Tasks 5 and 19. The plan intentionally breaks full-tree compatibility while old example/checker/integration call sites are migrated task-by-task; Task 23 is the first full-tree acceptance sweep.

Definition of done for a task:

- All checkbox steps are complete.
- New failing tests fail before implementation.
- New passing tests pass after implementation.
- `git diff --check` passes.
- A commit is created with the exact message shown in the task.

Definition of done for the full plan:

- `go test ./...` passes.
- `go test ./tests/e2e -run Hello -v` passes on a machine with QEMU/OVMF available.
- `rg -n "allocate_bytes|static_bytes|VirtualMemoryPlan|virtual_memory_plan" wrela examples compiler/sem compiler/ir compiler/codegen` reports no matches.
- `docs/production-deferred-work.md` says the production memory path is physical-region capability first, with x86 paging treated as target glue.

---

## 1. Frozen Memory Decisions

Do not reopen these decisions during task execution.

- Wrela source code does not expose virtual memory, address spaces, page permissions, higher-half layout, W^X, NX, or guard pages in this plan.
- x86_64 long mode still requires paging. The backend emits a minimal identity map using 2 MiB pages. That identity map is target boot glue, not the Wrela memory model.
- `PhysicalAddress` is the canonical address type for RAM regions, MMIO regions, byte views, arena bases, frame bases, cache bases, and DMA buffers.
- `VirtualAddress` remains valid only for UEFI firmware table pointers, function/code pointers, and existing ABI bridge fields that represent firmware-provided addresses. It must not be used for executor RAM byte views after this plan.
- `Bytes` and `MutableBytes` move from `VirtualAddress` to `PhysicalAddress`.
- The word for creating arena-backed typed values is `place`, not allocate.
- The word for raw byte spans is `reserve`, not allocate.
- `ExecutorMemory` is the durable root arena for one executor.
- `ArenaFrame` is a bounded child arena claimed from a parent arena by a `with` statement.
- `with parent.frame(length = N) as child { ... }` claims exactly `N` bytes from `parent`, gives `child` a fresh offset starting at zero, runs the block, and rewinds `parent.next_offset` when the block exits.
- Nested frames are legal and must unwind in stack order.
- Values placed in a frame carry a hidden semantic lifetime for that frame.
- A value with a shorter lifetime cannot be stored into longer-lived state, returned from the frame, assigned to a variable declared outside the frame, or captured by an executor, driver, driver path, cache, interrupt event storage, or shared region.
- Parent-lifetime values can be read inside child frames.
- Child-lifetime values cannot be stored in parent-lifetime values.
- Raw `MutableBytes(address = literal, length = literal)` construction is allowed only directly inside an image `delegated_hardware` phase in this plan. That exception preserves the current hand-authored boot arena until hardware discovery lands. All other user-module raw physical byte authority construction fails with SEM0028.
- `ArenaFrame(...)` construction is never allowed in user source. The only legal source form for a frame is `with arena.frame(length = N) as child { ... }`; direct `ArenaFrame` construction fails with SEM0029 except inside the skipped canonical intrinsic method bodies.
- `arena.place(T(...))` is a compiler-recognized memory intrinsic on `ExecutorMemory` and `ArenaFrame`. It places a typed `data` or `class` value in arena-owned storage and returns a source-level value of type `T` with hidden lifetime metadata.
- `arena.reserve(length = N, align = A)` returns `MutableBytes` with the receiver arena's hidden lifetime.
- `arena.bytes(value = "literal")` returns immutable `Bytes` pointing at compiler-emitted static data and has static lifetime.
- Frame OOM is fatal in this plan. It calls the generated memory trap symbol `_wrela_memory_oom`.
- Durable root OOM is fatal in this plan. Root memory is for bounded executor state planned by the image.
- Cache memory does not use fatal OOM for ordinary insertion. `CacheArena` evicts by default.
- Cache entries do not produce stable references. Cache lookup copies bytes into a caller-provided frame and returns a `CacheLookup` value whose `bytes` field belongs to that frame.
- `CacheArena` v1 is a fixed-slot FIFO byte cache keyed by `U64`. Slot count and slot size are explicit when the cache region is created.
- General-purpose individual free is not part of this plan.
- Compaction is not part of this plan.
- Garbage collection is not part of this plan.
- Shared memory across executors is not ambient. Cross-executor shared regions require an explicit `SharedMemory` capability and are documented as outside this implementation plan.
- Assembly may touch raw addresses only inside existing edge-capability modules: `arch.*`, `platform.*`, and `machine.x86_64.*`. Semantic checking must reject user modules that attempt to bypass arena APIs with asm.

Canonical source example:

```wrela
executor Worker {
    memory: ExecutorMemory
    cache: ResponseCache

    start fn run(self) -> never {
        while true {
            with self.memory.frame(length = 65536) as tick {
                let request = tick.place(Request(id = 1))
                let cached = self.cache.get(key = request.id, into = tick)

                if cached.hit {
                    self.send(bytes = cached.bytes)
                } else {
                    let response = self.build_response(heap = tick, request = request)
                    self.cache.put(key = request.id, bytes = response)
                    self.send(bytes = response)
                }
            }
        }
    }
}
```

---

## 2. Repository Layout And File Responsibilities

Create or modify exactly these files.

```text
compiler/diag/codes.go
  Adds memory diagnostics SEM0021-SEM0032.

compiler/lex/token.go
compiler/lex/lexer_test.go
  Adds the `with` keyword.

compiler/ast/ast.go
compiler/ast/ast_test.go
  Adds WithStmt and keeps debug output deterministic.

compiler/parse/parser.go
compiler/parse/parser_test.go
  Parses `with <call> as <name> { ... }`.

compiler/sem/memory.go
compiler/sem/memory_test.go
  Owns memory-kind classification, lifetime values, arena receiver checks, and method lifetime summaries.

compiler/sem/check.go
compiler/sem/types.go
compiler/sem/types_test.go
  Wires memory analysis into normal semantic checking.

compiler/sem/symbols.go
compiler/sem/symbols_test.go
  Updates `StringLiteral.address` to be a `PhysicalAddress` because this memory model treats image string data as identity-mapped physical bytes.

compiler/sem/memory_negative_test.go
tests/fixtures/negative/*.wrela
  Tests all illegal memory flows.

compiler/ir/ir.go
compiler/ir/lower.go
compiler/ir/memory_test.go
  Adds FrameBegin, FrameEnd, ArenaReserve, and ArenaPlace operations.

compiler/codegen/x64.go
compiler/codegen/memory_test.go
compiler/codegen/uefi_source_codegen_test.go
  Emits bump placement, frame claiming, rewinds, bounds checks, and `_wrela_memory_oom`.

compiler/integration_test.go
  Updates hello-program source scans and expected IR call graph symbols after the example moves to `bytes` and arena frames.

wrela/machine/x86_64/executor_memory.wrela
  Defines Bytes, MutableBytes, ExecutorMemory, ArenaFrame, and source-visible methods that are not compiler intrinsics.

wrela/machine/x86_64/cache_memory.wrela
  Defines CacheArena, CacheLookup, and ResponseCache examples for fixed-slot FIFO cache behavior.

wrela/machine/x86_64/cpu_state.wrela
wrela/platform/uefi/types.wrela
wrela/platform/uefi/transition.wrela
examples/hello/main.wrela
examples/hello/program.wrela
  Replaces source-level virtual memory and `allocate_bytes` usage with physical arena APIs.

docs/production-deferred-work.md
  Records that the selected production memory direction is direct physical region authority plus static enforcement.
```

---

## 3. Parallel Work Map

The tech lead owns contract arbitration. This plan has a real serial spine because syntax, semantic lifetime analysis, IR, and codegen depend on each other. Parallelism is still useful, but only at the merge gates listed below.

```text
Merge Gate 0: Task 1
  Freezes diagnostics and docs. No implementation stream starts before this lands.

Stream A: Syntax Spine
  Tasks 2-4
  Owns lexer, AST, parser.

Stream B: Wrela Source Contracts And Paging Glue
  Tasks 5, 15
  Owns wrela/machine/x86_64/*.wrela, examples, source shape tests.

Stream C: Semantic Memory Checker
  Tasks 6-10
  Owns compiler/sem memory kinds, lifetimes, with validation, method summaries, and fixtures.

Stream D: IR And Codegen
  Tasks 11-14
  Owns compiler/ir and compiler/codegen lowering for frame/place/reserve.

Stream E: Cache Memory
  Tasks 16-18
  Owns CacheArena source, semantic rules, and generated/asm behavior.

Stream F: Integration And Docs
  Tasks 19-23
  Owns hello rewrite, QEMU verification, raw-asm guard, and documentation.
```

Dependency rules:

- Task 1 must land first.
- Tasks 2-4 are serial and must land before any semantic test uses `with`.
- Task 5 may run in parallel with Tasks 2-4, but it must land before Tasks 8, 12, 13, 14, 16, and 19 rely on canonical memory type names.
- Tasks 6-10 are serial. Task 6 must land before Task 7; Task 7 before Task 8; Task 8 before Tasks 9-10.
- Tasks 11-14 are serial and start only after Tasks 4, 5, 8, and 10 land.
- Task 15 can run after Task 5 and does not depend on Tasks 6-14.
- Tasks 16-18 are serial and can run after Tasks 5 and 6; Task 17 also depends on Task 10's lifetime storage.
- Task 19 starts after Tasks 14, 15, and 18 land.
- Task 20 starts after Task 19.
- Task 21 can run after Task 9. It does not depend on paging, IR/codegen, or cache work, but it reuses Task 9's negative-fixture helper and memory helper file.
- Tasks 22-23 run after Tasks 19-21.

---

## 4. Canonical Wrela Memory Surface

These source names and signatures are fixed.

`StringLiteral.address` is also part of this contract: in this model, image string data is emitted into identity-mapped physical memory, so the primitive field type is `PhysicalAddress`, not `VirtualAddress`.

```wrela
module machine.x86_64.executor_memory

data Bytes {
    address: PhysicalAddress
    length: U64
}

data MutableBytes {
    address: PhysicalAddress
    length: U64
}

class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64

    fn bytes(self, value: StringLiteral) -> Bytes {
        return Bytes(address = value.address, length = value.length)
    }

    // Compiler intrinsic receiver. Source-visible signature is documented
    // by semantic checker tests; this method body is not called by codegen.
    fn frame(self, length: U64) -> ArenaFrame {
        return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0)
    }

    asm fn halt_forever(self) -> never {
    loop:
        hlt
        jmp loop
    }
}

class ArenaFrame {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64

    fn frame(self, length: U64) -> ArenaFrame {
        return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0)
    }
}
```

Compiler-recognized calls:

```wrela
with self.memory.frame(length = 65536) as tick {
    let msg = tick.place(Message(id = 1))
    let raw = tick.reserve(length = 4096, align = 16)
    let text = self.memory.bytes(value = "hello\n")
}
```

The source library declares `frame` and `bytes`. The compiler owns `place` and `reserve`; no Wrela method body exists for those names. The `frame` methods are canonical compiler intrinsics in `machine.x86_64.executor_memory`; semantic checking must skip normal constructor-permission checks for these two method bodies because `ArenaFrame` is an authority class that ordinary user code must not construct directly.

Cache source contracts:

```wrela
module machine.x86_64.cache_memory

use { Bytes, MutableBytes, ArenaFrame, ExecutorMemory } from machine.x86_64.executor_memory

data CacheLookup {
    hit: Bool
    bytes: Bytes
}

data CachePutResult {
    stored: Bool
    evicted: U64
}

class CacheArena {
    storage: MutableBytes
    slot_count: U64
    slot_size: U64
    next_victim: U64
    initialized: U64
}

class ResponseCache {
    memory: CacheArena

    fn get(self, key: U64, into: ArenaFrame) -> CacheLookup {
        return self.memory.get_bytes(key = key, into = into)
    }

    fn put(self, key: U64, bytes: Bytes) -> CachePutResult {
        return self.memory.put_bytes(key = key, bytes = bytes)
    }
}
```

---

## 5. Phase 1: Freeze Contracts And Syntax

### Task 1: Diagnostic Codes And Deferred-Work Contract

**Files:**
- Modify: `compiler/diag/codes.go`
- Modify: `docs/production-deferred-work.md`
- Test: `compiler/diag/diag_test.go`

**Purpose:** Reserve stable diagnostics for physical arena memory and document the selected direction before workers split across parser, semantic, and codegen streams.

- [ ] **Step 1: Add diagnostic-code test**

Add this test to `compiler/diag/diag_test.go`:

```go
func TestMemoryDiagnosticCodesExist(t *testing.T) {
    codes := []string{
        diag.SEM0021, diag.SEM0022, diag.SEM0023, diag.SEM0024,
        diag.SEM0025, diag.SEM0026, diag.SEM0027, diag.SEM0028,
        diag.SEM0029, diag.SEM0030, diag.SEM0031, diag.SEM0032,
    }
    for _, code := range codes {
        if code == "" {
            t.Fatalf("memory diagnostic code must not be empty")
        }
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./compiler/diag -run TestMemoryDiagnosticCodesExist -v`

Expected: FAIL with undefined identifiers such as `SEM0021`.

- [ ] **Step 3: Add codes**

Add these constants after `SEM0020` in `compiler/diag/codes.go`:

```go
    SEM0021 = "SEM0021" // invalid arena receiver
    SEM0022 = "SEM0022" // frame expression must be arena.frame(length = ...)
    SEM0023 = "SEM0023" // frame length must be U64
    SEM0024 = "SEM0024" // frame lifetime escapes with block value
    SEM0025 = "SEM0025" // frame value stored in longer-lived state
    SEM0026 = "SEM0026" // place argument must be constructor expression
    SEM0027 = "SEM0027" // reserve length and align must be U64
    SEM0028 = "SEM0028" // raw physical byte authority construction is not allowed here
    SEM0029 = "SEM0029" // ArenaFrame cannot be constructed directly
    SEM0030 = "SEM0030" // cache lookup must copy into frame
    SEM0031 = "SEM0031" // cache entry cannot escape lookup scope
    SEM0032 = "SEM0032" // asm raw memory access requires edge-capability module
```

- [ ] **Step 4: Update deferred-work document**

Replace the `Memory and address spaces` bullets in `docs/production-deferred-work.md` with:

```markdown
## Memory and address spaces
- This is required for production and not optional because memory safety, isolation, deterministic placement, and memory-footprint predictability break quickly as images and drivers grow beyond a toy shape.
- Selected direction: Wrela source models memory as direct physical region authority with hierarchical arenas, explicit `with` frame boundaries, statically checked lifetimes, bounded root executor memory, and default-evicting cache regions.
- x86_64 paging remains target boot glue only: the backend emits a minimal 2 MiB identity map required by long mode, but Wrela source does not expose virtual address spaces, higher-half layout, page permissions, W^X, NX, or guard pages in this stage.
- v0 exclusion reason: this work was intentionally deferred to keep the first compiler iteration small and to validate the core end-to-end flow before hardening memory policy.
- This stage must not block adding hardware-enforced page permissions, guard pages, DMA/IOMMU policy, or higher-half layout as backend artifacts generated from the physical-region authority graph.
```

- [ ] **Step 5: Run verification**

Run:

```bash
go test ./compiler/diag -run TestMemoryDiagnosticCodesExist -v
rg -n "hierarchical arenas|2 MiB identity map" docs/production-deferred-work.md
git diff --check
```

Expected: PASS for Go test; `rg` prints two matching lines; `git diff --check` prints nothing.

- [ ] **Step 6: Commit**

```bash
git add compiler/diag/codes.go compiler/diag/diag_test.go docs/production-deferred-work.md
git commit -m "docs: freeze physical arena memory contracts -Codex Automated"
```

**Acceptance Criteria:** Memory diagnostics SEM0021-SEM0032 exist; deferred-work doc states direct physical arena authority as the selected direction; the doc explicitly demotes x86_64 paging to target boot glue.

### Task 2: Lexer Keyword For `with`

**Files:**
- Modify: `compiler/lex/token.go`
- Modify: `compiler/lex/lexer_test.go`

**Purpose:** Reserve `with` as a language keyword so frame boundaries parse unambiguously.

- [ ] **Step 1: Write failing lexer test**

Add to `compiler/lex/lexer_test.go`:

```go
func TestWithKeyword(t *testing.T) {
    toks, diags := All("with memory.frame(length = 64) as tick {}")
    if len(diags) != 0 {
        t.Fatalf("lex diagnostics: %#v", diags)
    }
    if toks[0].Kind != KeywordWith || toks[0].Text != "with" {
        t.Fatalf("first token = %#v, want KeywordWith", toks[0])
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./compiler/lex -run TestWithKeyword -v`

Expected: FAIL with `undefined: KeywordWith`.

- [ ] **Step 3: Add token**

Add `KeywordWith` after `KeywordWhile` in `compiler/lex/token.go`, and add the keyword map entry:

```go
    KeywordWhile
    KeywordWith
    KeywordFor
```

```go
    "with":        KeywordWith,
```

- [ ] **Step 4: Run verification**

Run:

```bash
go test ./compiler/lex -run TestWithKeyword -v
go test ./compiler/lex -v
git diff --check
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add compiler/lex/token.go compiler/lex/lexer_test.go
git commit -m "feat: tokenize with frame keyword -Codex Automated"
```

**Acceptance Criteria:** `with` tokenizes as `KeywordWith` and no existing keyword token values are removed.

### Task 3: AST WithStmt Contract

**Files:**
- Modify: `compiler/ast/ast.go`
- Modify: `compiler/ast/ast_test.go`

**Purpose:** Represent frame scopes as statements with explicit frame expression, bound name, body, and span.

- [ ] **Step 1: Write failing AST test**

Add to `compiler/ast/ast_test.go`:

```go
func TestDebugWithStmt(t *testing.T) {
    stmt := &WithStmt{
        Expr: &CallExpr{
            Receiver: &NameExpr{Name: "memory"},
            Method: "frame",
            Args: []NamedArg{{
                Name:  "length",
                Value: &IntLiteral{Value: "64"},
            }},
        },
        Name: "tick",
        Body: []Stmt{&LetStmt{
            Name: "x",
            Expr: &NameExpr{Name: "tick"},
        }},
    }
    got := DebugStmt(stmt)
    want := "with memory.frame(length = 64) as tick { let x = tick }"
    if got != want {
        t.Fatalf("DebugStmt = %q, want %q", got, want)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./compiler/ast -run TestDebugWithStmt -v`

Expected: FAIL with undefined `WithStmt` or missing debug support.

- [ ] **Step 3: Add AST node**

Add to `compiler/ast/ast.go` near other statements:

```go
type WithStmt struct {
    Expr  Expr
    Name  string
    Body  []Stmt
    SpanV source.Span
}

func (s *WithStmt) Span() source.Span { return s.SpanV }
```

Add this complete `DebugStmt` helper to `compiler/ast/ast.go` below `DebugExpr`. The repository currently has `DebugExpr` but no statement debug helper, so add all handled statement cases instead of only adding the `WithStmt` case:

```go
func DebugStmt(stmt Stmt) string {
    switch s := stmt.(type) {
    case *LetStmt:
        return "let " + s.Name + " = " + DebugExpr(s.Expr)
    case *ReturnStmt:
        if s.Value == nil {
            return "return"
        }
        return "return " + DebugExpr(s.Value)
    case *AssignStmt:
        return DebugExpr(s.Target) + " = " + DebugExpr(s.Value)
    case *ExprStmt:
        return DebugExpr(s.Expr)
    case *IfStmt:
        return "if " + DebugExpr(s.Cond) + " { " + debugStmtList(s.Then) + " }"
    case *WhileStmt:
        return "while " + DebugExpr(s.Cond) + " { " + debugStmtList(s.Body) + " }"
    case *ForStmt:
        return "for " + s.Var + " in " + DebugExpr(s.InExpr) + " { " + debugStmtList(s.Body) + " }"
    case *WithStmt:
        return "with " + DebugExpr(s.Expr) + " as " + s.Name + " { " + debugStmtList(s.Body) + " }"
    default:
        return "<stmt>"
    }
}

func debugStmtList(stmts []Stmt) string {
    parts := make([]string, 0, len(stmts))
    for _, stmt := range stmts {
        parts = append(parts, DebugStmt(stmt))
    }
    return strings.Join(parts, " ")
}
```

- [ ] **Step 4: Run verification**

Run:

```bash
go test ./compiler/ast -run TestDebugWithStmt -v
go test ./compiler/ast -v
git diff --check
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add compiler/ast/ast.go compiler/ast/ast_test.go
git commit -m "feat: add with statement ast -Codex Automated"
```

**Acceptance Criteria:** `WithStmt` is a first-class statement node; debug output is deterministic and includes the bound arena name.

### Task 4: Parser For `with arena.frame(...) as name`

**Files:**
- Modify: `compiler/parse/parser.go`
- Modify: `compiler/parse/parser_test.go`

**Purpose:** Parse frame boundaries without interpreting memory semantics in the parser.

- [ ] **Step 1: Write failing parser test**

Add to `compiler/parse/parser_test.go`:

```go
func TestParseWithStatement(t *testing.T) {
    src := `
module parser.with_stmt

class Memory {}

executor Worker {
    memory: Memory

    start fn run(self) -> never {
        with self.memory.frame(length = 65536) as tick {
            let raw = tick.reserve(length = 32, align = 8)
        }
        while true {}
    }
}
`
    mod, diags := parseModuleForTest(t, src)
    if len(diags) != 0 {
        t.Fatalf("parse diagnostics: %#v", diags)
    }
    exec := mod.Decls[1].(*ast.ExecutorDecl)
    stmt := exec.Methods[0].Body[0]
    with, ok := stmt.(*ast.WithStmt)
    if !ok {
        t.Fatalf("first statement = %T, want *ast.WithStmt", stmt)
    }
    if with.Name != "tick" || len(with.Body) != 1 {
        t.Fatalf("with = %#v, want bound tick with one body statement", with)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./compiler/parse -run TestParseWithStatement -v`

Expected: FAIL because `with` is not parsed as a statement.

- [ ] **Step 3: Parse the statement**

In `parseStmt`, add a `KeywordWith` branch that calls `parseWithStmt`.

Implement:

```go
func (p *Parser) parseWithStmt() (ast.Stmt, []diag.Diagnostic) {
    start := p.next()
    expr, ds := p.parseExpr(0)
    if len(ds) != 0 {
        return nil, ds
    }
    asTok, ds := p.expectIdentifier("expected as")
    if len(ds) != 0 {
        return nil, ds
    }
    if asTok.Text != "as" {
        return nil, p.err(asTok, diag.PAR0001, "expected as")
    }
    nameTok, ds := p.expectIdentifier("expected frame name")
    if len(ds) != 0 {
        return nil, ds
    }
    body, ds := p.parseBlockStmts()
    if len(ds) != 0 {
        return nil, ds
    }
    return &ast.WithStmt{
        Expr:  expr,
        Name:  nameTok.Text,
        Body:  body,
        SpanV: p.span(start.Start, p.previous().End),
    }, nil
}
```

Do not add `as` as a keyword. It remains an identifier used by this grammar production.

- [ ] **Step 4: Run verification**

Run:

```bash
go test ./compiler/parse -run TestParseWithStatement -v
go test ./compiler/parse -v
git diff --check
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add compiler/parse/parser.go compiler/parse/parser_test.go
git commit -m "feat: parse with arena frames -Codex Automated"
```

**Acceptance Criteria:** `with self.memory.frame(length = 65536) as tick { ... }` parses into `ast.WithStmt`; `as` is not a global keyword.

---

## 6. Phase 2: Source-Level Memory Contracts

### Task 5: Replace Executor Memory Source With Physical Arena Surface

**Files:**
- Modify: `wrela/machine/x86_64/executor_memory.wrela`
- Modify: `compiler/sem/uefi_source_shape_test.go`
- Modify: `compiler/sem/symbols.go`
- Modify: `compiler/sem/symbols_test.go`
- Modify: `compiler/codegen/uefi_source_codegen_test.go`
- Modify: `compiler/parse/parser_test.go`
- Do not modify: `examples/hello/*.wrela` in this task; Task 19 rewrites the executable example after parser, semantic, IR, and codegen support exists.

**Purpose:** Make the canonical Wrela source use physical arena names and remove `allocate_bytes`.

- [ ] **Step 1: Write failing source-shape test**

Add to `compiler/sem/uefi_source_shape_test.go`:

```go
func TestExecutorMemoryPhysicalArenaShape(t *testing.T) {
    modules := parseUEFIModuleSet(t)
    index := mustBuildIndex(t, modules)

    memory := moduleType(t, index, "machine.x86_64.executor_memory", "ExecutorMemory")
    arenaFrame := moduleType(t, index, "machine.x86_64.executor_memory", "ArenaFrame")
    bytes := moduleType(t, index, "machine.x86_64.executor_memory", "Bytes")
    mutable := moduleType(t, index, "machine.x86_64.executor_memory", "MutableBytes")

    if fieldTypeName(t, bytes, "address") != "PhysicalAddress" {
        t.Fatalf("Bytes.address must be PhysicalAddress")
    }
    if fieldTypeName(t, mutable, "address") != "PhysicalAddress" {
        t.Fatalf("MutableBytes.address must be PhysicalAddress")
    }
    if methodByName(t, memory, "bytes") == nil || methodByName(t, memory, "frame") == nil {
        t.Fatalf("ExecutorMemory must expose bytes and frame methods")
    }
    if optionalMethodByName(memory, "allocate_bytes") != nil {
        t.Fatalf("ExecutorMemory must not expose allocate_bytes")
    }
    if arenaFrame == nil || methodByName(t, arenaFrame, "frame") == nil {
        t.Fatalf("ArenaFrame must expose nested frame method")
    }
}
```

Add these helpers if they do not exist:

```go
func fieldTypeName(t *testing.T, typ *Type, field string) string {
    t.Helper()
    for _, f := range typ.Fields {
        if f.Name == field {
            return f.Type.Name
        }
    }
    t.Fatalf("missing field %s on %s", field, typ.Name)
    return ""
}

func optionalMethodByName(typ *Type, name string) *Method {
    for i := range typ.Methods {
        if typ.Methods[i].Name == name {
            return &typ.Methods[i]
        }
    }
    return nil
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./compiler/sem -run TestExecutorMemoryPhysicalArenaShape -v`

Expected: FAIL because `Bytes.address` is still `VirtualAddress` and `allocate_bytes` exists.

- [ ] **Step 3: Replace source**

Rewrite `wrela/machine/x86_64/executor_memory.wrela` to match Section 4 exactly, preserving `halt_forever`.

Do not run full UEFI semantic verification in this task. The canonical `frame` method bodies intentionally contain `ArenaFrame(...)` construction, and the checker exception for those intrinsic signatures is added in Task 7.

- [ ] **Step 4: Update source-shape and codegen tests that directly inspect executor memory**

Replace direct executor-memory source-shape assertions as follows:

```text
compiler/sem/uefi_source_shape_test.go:
  add TestExecutorMemoryPhysicalArenaShape from Step 1
  remove or update any methodByName(..., "allocate_bytes") assertion

compiler/codegen/uefi_source_codegen_test.go:
  if a source-codegen test expects _wrela_method_machine_x86_64_executor_memory_ExecutorMemory_static_bytes, change it to _wrela_method_machine_x86_64_executor_memory_ExecutorMemory_bytes

compiler/sem/symbols.go:
  in buildPrimitives, change StringLiteral.address from VirtualAddress to PhysicalAddress

compiler/sem/symbols_test.go:
  in the "string literal fields" subtest, change the first expected field type from VirtualAddress to PhysicalAddress

compiler/parse/parser_test.go:
  in TestParseCanonicalMethodShapes, replace self.memory.static_bytes("hello") with self.memory.bytes(value = "hello")

compiler/sem/types_test.go on-handler allocation fixture -> leave until Task 8, where arena intrinsics exist
compiler/sem/check.go forbidden-call table -> leave until Task 8 replaces the intrinsic table
compiler/integration_test.go hello call graph symbol -> leave until Task 19 rewrites the executable example
examples/hello/program.wrela call sites -> leave until Task 19
```

Do not add a compatibility wrapper for `allocate_bytes`. This task is allowed to leave old vocabulary in example source, the hello integration call-graph expectation, and the generic checker deny-list because those call sites are not exercised by this task's focused verification command. Task 8 owns the checker deny-list. Task 19 owns the executable hello rewrite and integration expected symbol.

- [ ] **Step 5: Run verification**

Run:

```bash
go test ./compiler/sem -run TestExecutorMemoryPhysicalArenaShape -v
rg -n "allocate_bytes|static_bytes" wrela/machine/x86_64/executor_memory.wrela compiler/sem/uefi_source_shape_test.go compiler/codegen/uefi_source_codegen_test.go compiler/parse/parser_test.go compiler/sem/symbols.go compiler/sem/symbols_test.go
git diff --check
```

Expected: the focused source-shape test PASSes; `rg` reports no matches in the files owned by this task. Other old vocabulary references remain until Tasks 8 and 19. `go test ./compiler/sem -run UefiSource -v` is intentionally deferred until Task 7 adds the canonical frame intrinsic body skip.

- [ ] **Step 6: Commit**

```bash
git add wrela/machine/x86_64/executor_memory.wrela compiler/sem/uefi_source_shape_test.go compiler/sem/symbols.go compiler/sem/symbols_test.go compiler/codegen/uefi_source_codegen_test.go compiler/parse/parser_test.go
git commit -m "feat: define physical executor arena source -Codex Automated"
```

**Acceptance Criteria:** In files owned by this task, `Bytes` and `MutableBytes` carry `PhysicalAddress`; `ExecutorMemory` exposes `bytes` and `frame`; `ArenaFrame` exists; no `allocate_bytes` or `static_bytes` remains.

### Task 6: Memory Type Classification

**Files:**
- Create: `compiler/sem/memory.go`
- Create: `compiler/sem/memory_test.go`
- Modify: `compiler/sem/types.go`

**Purpose:** Give semantic analysis one focused place to classify memory authorities, byte views, cache regions, and hidden lifetimes.

- [ ] **Step 1: Write failing classification test**

Create `compiler/sem/memory_test.go`:

```go
package sem

import "testing"

func TestMemoryKindClassification(t *testing.T) {
    modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

data Bytes { address: PhysicalAddress length: U64 }
data MutableBytes { address: PhysicalAddress length: U64 }
class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame { return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0) }
}
class ArenaFrame { arena_base: PhysicalAddress arena_length: U64 next_offset: U64 }
`, `
module machine.x86_64.cache_memory

use { MutableBytes } from machine.x86_64.executor_memory

class CacheArena { storage: MutableBytes slot_count: U64 slot_size: U64 next_victim: U64 initialized: U64 }
`, `
module user.shadow

class ExecutorMemory {}
class ArenaFrame {}
data Bytes {}
data MutableBytes {}
class CacheArena {}
`)
    index, ds := BuildIndex(modules)
    ds = filterMissingImageDiagnostic(ds)
    if len(ds) != 0 {
        t.Fatalf("index diagnostics: %#v", ds)
    }
    cases := []struct {
        module string
        name   string
        want   MemoryKind
    }{
        {"machine.x86_64.executor_memory", "ExecutorMemory", MemoryKindRootArena},
        {"machine.x86_64.executor_memory", "ArenaFrame", MemoryKindFrameArena},
        {"machine.x86_64.executor_memory", "Bytes", MemoryKindBytes},
        {"machine.x86_64.executor_memory", "MutableBytes", MemoryKindMutableBytes},
        {"machine.x86_64.cache_memory", "CacheArena", MemoryKindCacheArena},
    }
    for _, tc := range cases {
        typ, ok := index.Lookup(tc.module, tc.name)
        if !ok {
            t.Fatalf("missing type %s.%s", tc.module, tc.name)
        }
        got := ClassifyMemoryType(typ)
        if got != tc.want {
            t.Fatalf("ClassifyMemoryType(%s.%s) = %v, want %v", tc.module, tc.name, got, tc.want)
        }
    }
    for _, name := range []string{"ExecutorMemory", "ArenaFrame", "Bytes", "MutableBytes", "CacheArena"} {
        typ, ok := index.Lookup("user.shadow", name)
        if !ok {
            t.Fatalf("missing user.shadow.%s", name)
        }
        if got := ClassifyMemoryType(typ); got != MemoryKindNone {
            t.Fatalf("user.shadow.%s classified as %v, want MemoryKindNone", name, got)
        }
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./compiler/sem -run TestMemoryKindClassification -v`

Expected: FAIL with undefined `MemoryKind`.

- [ ] **Step 3: Add classification code**

Create `compiler/sem/memory.go`:

```go
package sem

type MemoryKind uint8

const (
    MemoryKindNone MemoryKind = iota
    MemoryKindRootArena
    MemoryKindFrameArena
    MemoryKindBytes
    MemoryKindMutableBytes
    MemoryKindCacheArena
)

type LifetimeKind uint8

const (
    LifetimeUnknown LifetimeKind = iota
    LifetimeStatic
    LifetimeExecutorRoot
    LifetimeFrame
    LifetimeCacheLookup
    LifetimeCacheCopy
)

type Lifetime struct {
    Kind  LifetimeKind
    Scope int
}

func ClassifyMemoryType(t *Type) MemoryKind {
    if t == nil {
        return MemoryKindNone
    }
    switch t.Module + "." + t.Name {
    case "machine.x86_64.executor_memory.ExecutorMemory":
        return MemoryKindRootArena
    case "machine.x86_64.executor_memory.ArenaFrame":
        return MemoryKindFrameArena
    case "machine.x86_64.executor_memory.Bytes":
        return MemoryKindBytes
    case "machine.x86_64.executor_memory.MutableBytes":
        return MemoryKindMutableBytes
    case "machine.x86_64.cache_memory.CacheArena":
        return MemoryKindCacheArena
    }
    return MemoryKindNone
}

func IsArenaType(t *Type) bool {
    kind := ClassifyMemoryType(t)
    return kind == MemoryKindRootArena || kind == MemoryKindFrameArena
}
```

- [ ] **Step 4: Run verification**

Run:

```bash
go test ./compiler/sem -run TestMemoryKindClassification -v
go test ./compiler/sem -v
git diff --check
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add compiler/sem/memory.go compiler/sem/memory_test.go compiler/sem/types.go
git commit -m "feat: classify memory authority types -Codex Automated"
```

**Acceptance Criteria:** Semantic code has a single module-qualified classification API for root arenas, frame arenas, byte views, mutable byte views, and cache arenas. A user module cannot gain memory semantics merely by defining a type named `ExecutorMemory`, `ArenaFrame`, `Bytes`, `MutableBytes`, or `CacheArena`.

---

## 7. Phase 3: Static Frame And Lifetime Enforcement

### Task 7: Semantic Validation For With Frame Expressions

**Files:**
- Modify: `compiler/sem/check.go`
- Modify: `compiler/sem/memory.go`
- Modify: `compiler/sem/memory_test.go`
- Modify: `compiler/sem/types_test.go`

**Purpose:** Ensure every `with` statement binds a child arena created by `.frame(length = ...)` on an arena receiver.

- [ ] **Step 1: Write failing valid-frame test**

Add to `compiler/sem/memory_test.go`:

```go
func TestWithFrameTypechecks(t *testing.T) {
    modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame { return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0) }
}
class ArenaFrame {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
}
unique class OwnedHardware {}
unique class DelegatedHardware { fn exit_to_owned_hardware(self) -> OwnedHardware { return OwnedHardware() } }
executor Worker {
    memory: ExecutorMemory
    start fn run(self) -> never {
        with self.memory.frame(length = 64) as tick {
        }
        while true {}
    }
}
image App {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware { return hardware.exit_to_owned_hardware() }
    phase owned_hardware(hardware: OwnedHardware) -> never {
        let worker = Worker(memory = ExecutorMemory(arena_base = 0, arena_length = 4096, next_offset = 0))
        worker.run()
    }
}
`)
    index, ds := BuildIndex(modules)
    ds = filterMissingImageDiagnostic(ds)
    if len(ds) != 0 {
        t.Fatalf("index diagnostics: %#v", ds)
    }
    _, diags := Check(index, modules)
    if len(diags) != 0 {
        t.Fatalf("check diagnostics: %#v", diags)
    }
}
```

- [ ] **Step 2: Write failing invalid-frame test**

Add:

```go
func TestWithRejectsNonFrameExpression(t *testing.T) {
    modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

data NotFrame { value: U64 }
unique class OwnedHardware {}
unique class DelegatedHardware { fn exit_to_owned_hardware(self) -> OwnedHardware { return OwnedHardware() } }
executor Worker {
    start fn run(self) -> never {
        with NotFrame(value = 1) as tick {
        }
        while true {}
    }
}
image App {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware { return hardware.exit_to_owned_hardware() }
    phase owned_hardware(hardware: OwnedHardware) -> never {
        let worker = Worker()
        worker.run()
    }
}
`)
    index, ds := BuildIndex(modules)
    ds = filterMissingImageDiagnostic(ds)
    if len(ds) != 0 {
        t.Fatalf("index diagnostics: %#v", ds)
    }
    _, diags := Check(index, modules)
    if !hasCode(diags, diag.SEM0022) {
        t.Fatalf("expected SEM0022, got %#v", diags)
    }
}
```

Add a frame-length test:

```go
func TestWithRejectsNonU64FrameLength(t *testing.T) {
    modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame {
        return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0)
    }
}
class ArenaFrame { arena_base: PhysicalAddress arena_length: U64 next_offset: U64 }
executor Worker {
    memory: ExecutorMemory
    start fn run(self) -> never {
        with self.memory.frame(length = true) as tick {
        }
        while true {}
    }
}
`)
    index, ds := BuildIndex(modules)
    ds = filterMissingImageDiagnostic(ds)
    if len(ds) != 0 {
        t.Fatalf("index diagnostics: %#v", ds)
    }
    _, diags := Check(index, modules)
    if !hasCode(diags, diag.SEM0023) {
        t.Fatalf("expected SEM0023, got %#v", diags)
    }
}
```

Add direct-constructor rejection:

```go
func TestDirectArenaFrameConstructionRejected(t *testing.T) {
    modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

class ArenaFrame { arena_base: PhysicalAddress arena_length: U64 next_offset: U64 }
class Bad {
    fn make(self) -> ArenaFrame {
        return ArenaFrame(arena_base = 0, arena_length = 64, next_offset = 0)
    }
}
`)
    index, ds := BuildIndex(modules)
    ds = filterMissingImageDiagnostic(ds)
    if len(ds) != 0 {
        t.Fatalf("index diagnostics: %#v", ds)
    }
    _, diags := Check(index, modules)
    if !hasCode(diags, diag.SEM0029) {
        t.Fatalf("expected SEM0029, got %#v", diags)
    }
}
```

Add direct `.frame(...)` rejection outside a `with` boundary:

```go
func TestDirectFrameCallRejectedOutsideWith(t *testing.T) {
    modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame {
        return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0)
    }
}
class ArenaFrame { arena_base: PhysicalAddress arena_length: U64 next_offset: U64 }
executor Worker {
    memory: ExecutorMemory
    start fn run(self) -> never {
        let tick = self.memory.frame(length = 64)
        while true {}
    }
}
`)
    index, ds := BuildIndex(modules)
    ds = filterMissingImageDiagnostic(ds)
    if len(ds) != 0 {
        t.Fatalf("index diagnostics: %#v", ds)
    }
    _, diags := Check(index, modules)
    if !hasCode(diags, diag.SEM0022) {
        t.Fatalf("expected SEM0022, got %#v", diags)
    }
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./compiler/sem -run 'TestWith(FrameTypechecks|RejectsNonFrameExpression|RejectsNonU64FrameLength)|TestDirect(ArenaFrameConstruction|FrameCall)Rejected' -v`

Expected: FAIL because `WithStmt` is not checked.

- [ ] **Step 4: Implement canonical frame intrinsic skip and checker branch**

First add this helper in `compiler/sem/memory.go`:

```go
func isCanonicalFrameIntrinsic(moduleName string, typ *Type, method ast.MethodDecl) bool {
    if moduleName != "machine.x86_64.executor_memory" || method.Name != "frame" {
        return false
    }
    if typ == nil || (typ.Name != "ExecutorMemory" && typ.Name != "ArenaFrame") {
        return false
    }
    params := method.Params
    if len(params) > 0 && params[0].Name == "self" {
        params = params[1:]
    }
    return len(params) == 1 && params[0].Name == "length" && params[0].Type == "U64"
}
```

In `checkMethods`, after the asm-allowed check and before method body checking, skip these canonical intrinsic bodies:

```go
if isCanonicalFrameIntrinsic(moduleName, typ, method) {
    continue
}
```

This exception is required because `ArenaFrame` is an authority class. Ordinary user code must not construct it directly, but the canonical source file still declares the source-visible intrinsic signature.

Task 10 must also exclude these canonical frame methods from method-summary registration:

```go
if method.IsAsm || isCanonicalFrameIntrinsic(mod.Name, typ, method) {
    continue
}
```

Then update `checkConstructorPermissions` before the generic `KindData` allowance:

```go
if typ.Module == "machine.x86_64.executor_memory" && typ.Name == "ArenaFrame" {
    c.error(expr.SpanV, diag.SEM0029, "ArenaFrame can only be created by with arena.frame(length = ...)")
    return
}
```

In `checkStmt`, add:

```go
case *ast.WithStmt:
    prevAllowFrameCall := c.allowFrameCallExpr
    c.allowFrameCallExpr = true
    frameType := c.typeExpr(moduleName, s.Expr, scope, ctx)
    c.allowFrameCallExpr = prevAllowFrameCall
    if frameType == nil {
        c.checkStmtList(moduleName, s.Body, NewScope(scope), expectedReturn, ctx)
        return false
    }
    if ClassifyMemoryType(frameType) != MemoryKindFrameArena {
        c.error(s.Expr.Span(), diag.SEM0022, "with expression must be arena.frame(length = ...)")
    } else if !c.isFrameCall(moduleName, s.Expr, scope, ctx) {
        c.error(s.Expr.Span(), diag.SEM0022, "with expression must be arena.frame(length = ...)")
    }
    child := NewScope(scope)
    child.Define(s.Name, frameType)
    parentLifetime := c.frameReceiverLifetime(s.Expr, scope)
    childScopeID := c.pushFrameLifetime(s.Name, s.SpanV, parentLifetime)
    child.DefineLifetime(s.Name, Lifetime{Kind: LifetimeFrame, Scope: childScopeID})
    c.checkStmtList(moduleName, s.Body, child, expectedReturn, ctx)
    c.popFrameLifetime(childScopeID)
```

Add `allowFrameCallExpr bool` to `checker`. In `typeCallExpr`, handle canonical `.frame(...)` before generic method-summary propagation:

```go
if expr.Method == "frame" && IsArenaType(recvType) && method.Return != nil && ClassifyMemoryType(method.Return) == MemoryKindFrameArena {
    if !c.allowFrameCallExpr {
        c.error(expr.SpanV, diag.SEM0022, "arena.frame(length = ...) can only appear as a with expression")
    }
    c.typeAndVerifyCallArgs(moduleName, method, expr.Args, scope, ctx)
    return method.Return
}
```

This prevents canonical frame calls from being summarized as normal methods and enforces the frozen rule that the only legal source form is `with arena.frame(length = ...) as child { ... }`.

Add helper skeletons in `memory.go`:

```go
func (c *checker) isFrameCall(moduleName string, expr ast.Expr, scope *Scope, ctx ContextKind) bool {
    call, ok := expr.(*ast.CallExpr)
    if !ok || call.Method != "frame" {
        return false
    }
    recvType := c.typeExpr(moduleName, call.Receiver, scope, ctx)
    if !IsArenaType(recvType) {
        c.error(call.Receiver.Span(), diag.SEM0021, "frame receiver must be ExecutorMemory or ArenaFrame")
        return false
    }
    if len(call.Args) != 1 || call.Args[0].Name != "length" {
        return false
    }
    lengthType := c.typeExpr(moduleName, call.Args[0].Value, scope, ctx)
    u64 := c.mustType(moduleName, "U64")
    if lengthType != nil && !typesCompatible(u64, lengthType) {
        c.error(call.Args[0].Value.Span(), diag.SEM0023, "frame length must be U64")
        return false
    }
    return true
}

func (c *checker) frameReceiverLifetime(expr ast.Expr, scope *Scope) Lifetime {
    call, ok := expr.(*ast.CallExpr)
    if !ok || call.Method != "frame" {
        return Lifetime{Kind: LifetimeExecutorRoot}
    }
    return c.lifetimeOfExpr(call.Receiver, scope)
}
```

`pushFrameLifetime` and `popFrameLifetime` may initially maintain an integer stack on `checker`; Task 8 consumes it.

- [ ] **Step 5: Run verification**

Run:

```bash
go test ./compiler/sem -run 'TestWith(FrameTypechecks|RejectsNonFrameExpression|RejectsNonU64FrameLength)|TestDirect(ArenaFrameConstruction|FrameCall)Rejected' -v
go test ./compiler/sem -v
git diff --check
```

Expected: all tests PASS.

- [ ] **Step 6: Commit**

```bash
git add compiler/sem/check.go compiler/sem/memory.go compiler/sem/memory_test.go
git commit -m "feat: validate with frame expressions -Codex Automated"
```

**Acceptance Criteria:** `with` accepts only `.frame(length = <U64 expression>)` calls on `ExecutorMemory` or `ArenaFrame`; direct `.frame(...)` calls outside `with` produce SEM0022; direct `ArenaFrame(...)` construction produces SEM0029; canonical frame method bodies are skipped and are not method-summary targets.

### Task 8: Arena Intrinsics `place` And `reserve`

**Files:**
- Modify: `compiler/sem/check.go`
- Modify: `compiler/sem/memory.go`
- Modify: `compiler/sem/memory_test.go`
- Modify: `compiler/sem/types_test.go`

**Purpose:** Typecheck `arena.place(T(...))` and `arena.reserve(length = ..., align = ...)` as compiler intrinsics without adding Wrela method declarations.

- [ ] **Step 1: Write failing tests**

Add to `compiler/sem/memory_test.go`:

```go
func TestArenaPlaceAndReserveIntrinsics(t *testing.T) {
    modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

data Message { id: U64 }
data MutableBytes { address: PhysicalAddress length: U64 }
class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame { return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0) }
}
class ArenaFrame { arena_base: PhysicalAddress arena_length: U64 next_offset: U64 }
unique class OwnedHardware {}
unique class DelegatedHardware { fn exit_to_owned_hardware(self) -> OwnedHardware { return OwnedHardware() } }
executor Worker {
    memory: ExecutorMemory
    start fn run(self) -> never {
        with self.memory.frame(length = 128) as tick {
            let msg = tick.place(Message(id = 7))
            let raw = tick.reserve(length = 64, align = 8)
        }
        while true {}
    }
}
image App {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware { return hardware.exit_to_owned_hardware() }
    phase owned_hardware(hardware: OwnedHardware) -> never {
        let worker = Worker(memory = ExecutorMemory(arena_base = 0, arena_length = 4096, next_offset = 0))
        worker.run()
    }
}
`)
    index, ds := BuildIndex(modules)
    if len(ds) != 0 {
        t.Fatalf("index diagnostics: %#v", ds)
    }
    _, diags := Check(index, modules)
    if len(diags) != 0 {
        t.Fatalf("check diagnostics: %#v", diags)
    }
}

func TestPlaceRejectsNonConstructor(t *testing.T) {
    modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame {
        return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0)
    }
}
class ArenaFrame { arena_base: PhysicalAddress arena_length: U64 next_offset: U64 }
executor Worker {
    memory: ExecutorMemory
    start fn run(self) -> never {
        with self.memory.frame(length = 128) as tick {
            let bad = tick.place(7)
        }
        while true {}
    }
}
`)
    index, ds := BuildIndex(modules)
    ds = filterMissingImageDiagnostic(ds)
    if len(ds) != 0 {
        t.Fatalf("index diagnostics: %#v", ds)
    }
    _, diags := Check(index, modules)
    if !hasCode(diags, diag.SEM0026) {
        t.Fatalf("expected SEM0026, got %#v", diags)
    }
}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./compiler/sem -run 'TestArenaPlaceAndReserveIntrinsics|TestPlaceRejectsNonConstructor' -v`

Expected: FAIL because `place` and `reserve` are unknown methods.

- [ ] **Step 3: Special-case intrinsic calls**

In `typeCallExpr`, before normal method lookup, add:

```go
if typ := c.typeArenaIntrinsicCall(moduleName, expr, scope, ctx); typ != nil {
    return typ
}
```

Update the existing `isForbiddenOnHandlerCall` switch in `compiler/sem/check.go`. Remove the old allocation method from the `case` list and keep halt:

```go
case "machine.x86_64.interrupts.ApicInterruptController::enable_cpu_interrupts",
    "machine.x86_64.interrupts.ApicInterruptController::initialize_for_com1_receive",
    "machine.x86_64.interrupts.LocalApic::enable",
    "machine.x86_64.interrupts.IoApic::route_gsi4_to_vector40",
    "machine.x86_64.pci.Q35PciInterruptConfigurator::configure_edu_msi_vector41",
    "machine.x86_64.pci.Q35PciInterruptConfigurator::configure_ivshmem_msix_vector42",
    "machine.x86_64.pci.Q35PciInterruptConfigurator::write_config32",
    "machine.x86_64.pci.PciConfigPorts::write32",
    "machine.x86_64.pci.MsixTable::write_entry0",
    "machine.x86_64.edu.EduMsiPath::raise_test_interrupt",
    "machine.x86_64.edu.EduMsiPath::write32",
    "machine.x86_64.ivshmem.IvshmemDoorbellPeerPath::ring_peer",
    "machine.x86_64.ivshmem.IvshmemDoorbellPeerPath::write32",
    "machine.x86_64.serial.SerialConsolePath::enable_receive_interrupts",
    "machine.x86_64.executor_memory.ExecutorMemory::halt_forever",
    "arch.x86_64.cpu.CpuControl::halt_forever":
    return true
```

`place` and `reserve` are not normal methods, so on-handler rejection for those calls belongs inside `typeArenaIntrinsicCall`:

```go
if ctx == ContextOnHandler && (expr.Method == "place" || expr.Method == "reserve") {
    c.error(expr.SpanV, diag.SEM0016, "on handler cannot place or reserve arena memory")
    return nil
}
```

Also update the existing `TestOnHandlerRejectsAllocationAndInterruptReconfigurationCalls` fixture in `compiler/sem/types_test.go`. Replace the first `machine.x86_64.executor_memory` source string exactly with this source:

```wrela
module machine.x86_64.executor_memory

data MutableBytes { address: PhysicalAddress length: U64 }
class ArenaFrame { arena_base: PhysicalAddress arena_length: U64 next_offset: U64 }
class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame {
        return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0)
    }
}
```

Then replace the old handler call:

```wrela
self.memory.allocate_bytes(length = 8)
```

with:

```wrela
with self.memory.frame(length = 64) as tick {
    let raw = tick.reserve(length = 8, align = 8)
}
```

Add in `memory.go`:

```go
func (c *checker) typeArenaIntrinsicCall(moduleName string, expr *ast.CallExpr, scope *Scope, ctx ContextKind) *Type {
    recvType := c.typeExpr(moduleName, expr.Receiver, scope, ctx)
    if !IsArenaType(recvType) {
        return nil
    }
    if ctx == ContextOnHandler && (expr.Method == "place" || expr.Method == "reserve") {
        c.error(expr.SpanV, diag.SEM0016, "on handler cannot place or reserve arena memory")
        return nil
    }
    switch expr.Method {
    case "place":
        if len(expr.Args) != 1 || expr.Args[0].Name != "" {
            c.error(expr.SpanV, diag.SEM0026, "place expects one constructor argument")
            return nil
        }
        cons, ok := expr.Args[0].Value.(*ast.ConstructorExpr)
        if !ok {
            c.error(expr.Args[0].Value.Span(), diag.SEM0026, "place argument must be a constructor expression")
            return nil
        }
        return c.typeConstructorExpr(moduleName, cons, scope, ctx)
    case "reserve":
        c.requireReserveArgs(moduleName, expr, scope, ctx)
        return c.mustType(moduleName, "MutableBytes")
    default:
        return nil
    }
}

func (c *checker) requireReserveArgs(moduleName string, expr *ast.CallExpr, scope *Scope, ctx ContextKind) {
    if len(expr.Args) != 2 || expr.Args[0].Name != "length" || expr.Args[1].Name != "align" {
        c.error(expr.SpanV, diag.SEM0027, "reserve expects length and align")
        return
    }
    u64 := c.mustType(moduleName, "U64")
    c.requireType(c.typeExpr(moduleName, expr.Args[0].Value, scope, ctx), u64, expr.Args[0].Value.Span())
    c.requireType(c.typeExpr(moduleName, expr.Args[1].Value, scope, ctx), u64, expr.Args[1].Value.Span())
}
```

- [ ] **Step 4: Run verification**

Run:

```bash
go test ./compiler/sem -run 'TestArenaPlaceAndReserveIntrinsics|TestPlaceRejectsNonConstructor' -v
go test ./compiler/sem -v
git diff --check
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add compiler/sem/check.go compiler/sem/memory.go compiler/sem/memory_test.go compiler/sem/types_test.go
git commit -m "feat: typecheck arena place and reserve -Codex Automated"
```

**Acceptance Criteria:** `place` and `reserve` work only on arena receivers; `place` requires a constructor expression; `reserve` requires `length` and `align` as U64 values.

### Task 9: Frame Lifetime Escape Checks

**Files:**
- Modify: `compiler/sem/memory.go`
- Modify: `compiler/sem/check.go`
- Create: `compiler/sem/memory_negative_test.go`
- Create: `tests/fixtures/negative/frame_escape_return.wrela`
- Create: `tests/fixtures/negative/frame_escape_field.wrela`

**Purpose:** Prevent frame-backed values from escaping their `with` block.

- [ ] **Step 1: Add negative fixture for return escape**

Create `tests/fixtures/negative/frame_escape_return.wrela`:

```wrela
// expect: SEM0024: frame value cannot escape
module machine.x86_64.executor_memory

data Message { id: U64 }
class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame {
        return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0)
    }
}
class ArenaFrame { arena_base: PhysicalAddress arena_length: U64 next_offset: U64 }
executor Worker {
    memory: ExecutorMemory
    fn bad(self) -> Message {
        with self.memory.frame(length = 64) as tick {
            let msg = tick.place(Message(id = 1))
            return msg
        }
    }
    start fn run(self) -> never { while true {} }
}
```

- [ ] **Step 2: Add negative fixture for field escape**

Create `tests/fixtures/negative/frame_escape_field.wrela`:

```wrela
// expect: SEM0025: frame value cannot be stored
module machine.x86_64.executor_memory

data Message { id: U64 }
class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame {
        return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0)
    }
}
class ArenaFrame { arena_base: PhysicalAddress arena_length: U64 next_offset: U64 }
executor Worker {
    memory: ExecutorMemory
    saved: Message
    start fn run(self) -> never {
        with self.memory.frame(length = 64) as tick {
            let msg = tick.place(Message(id = 1))
            self.saved = msg
        }
        while true {}
    }
}
```

- [ ] **Step 3: Add Go assertions**

Create `compiler/sem/memory_negative_test.go`:

```go
package sem

import (
    "os"
    "path/filepath"
    "testing"

    "github.com/ryanwible/wrela3/compiler/ast"
    "github.com/ryanwible/wrela3/compiler/diag"
)

func parseFixtureModulesForTest(t *testing.T, path string) []*ast.Module {
    t.Helper()
    wd, err := os.Getwd()
    if err != nil {
        t.Fatalf("getwd: %v", err)
    }
    abs := filepath.Join(wd, "..", "..", path)
    src, err := os.ReadFile(abs)
    if err != nil {
        t.Fatalf("read fixture %s: %v", abs, err)
    }
    return parseModulesForTest(t, string(src))
}

func TestFrameEscapeNegativeFixtures(t *testing.T) {
    fixtures := []string{
        "frame_escape_return.wrela",
        "frame_escape_field.wrela",
    }
    for _, fixture := range fixtures {
        t.Run(fixture, func(t *testing.T) {
            modules := parseFixtureModulesForTest(t, filepath.Join("tests", "fixtures", "negative", fixture))
            index, ds := BuildIndex(modules)
            ds = filterMissingImageDiagnostic(ds)
            if len(ds) != 0 {
                t.Fatalf("index diagnostics: %#v", ds)
            }
            _, diags := Check(index, modules)
            if !hasCode(diags, diag.SEM0024) && !hasCode(diags, diag.SEM0025) {
                t.Fatalf("expected frame escape diagnostic, got %#v", diags)
            }
        })
    }
}

func TestParentFrameValueCanBeUsedInsideChildFrame(t *testing.T) {
    modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

data Message { id: U64 }
data Box { msg: Message }
class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame {
        return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0)
    }
}
class ArenaFrame {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame {
        return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0)
    }
}
executor Worker {
    memory: ExecutorMemory
    start fn run(self) -> never {
        with self.memory.frame(length = 128) as parent {
            let msg = parent.place(Message(id = 1))
            with parent.frame(length = 64) as child {
                let box = child.place(Box(msg = Message(id = 0)))
                box.msg = msg
            }
        }
        while true {}
    }
}
`)
    index, ds := BuildIndex(modules)
    ds = filterMissingImageDiagnostic(ds)
    if len(ds) != 0 {
        t.Fatalf("index diagnostics: %#v", ds)
    }
    _, diags := Check(index, modules)
    if len(diags) != 0 {
        t.Fatalf("check diagnostics: %#v", diags)
    }
}

func TestChildFrameValueCannotBeStoredInParentFrame(t *testing.T) {
    modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

data Message { id: U64 }
data Box { msg: Message }
class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame {
        return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0)
    }
}
class ArenaFrame {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame {
        return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0)
    }
}
executor Worker {
    memory: ExecutorMemory
    start fn run(self) -> never {
        with self.memory.frame(length = 128) as parent {
            let parent_box = parent.place(Box(msg = Message(id = 0)))
            with parent.frame(length = 64) as child {
                let msg = child.place(Message(id = 1))
                parent_box.msg = msg
            }
        }
        while true {}
    }
}
`)
    index, ds := BuildIndex(modules)
    ds = filterMissingImageDiagnostic(ds)
    if len(ds) != 0 {
        t.Fatalf("index diagnostics: %#v", ds)
    }
    _, diags := Check(index, modules)
    if !hasCode(diags, diag.SEM0025) {
        t.Fatalf("expected SEM0025, got %#v", diags)
    }
}

func TestFrameLifetimeAncestryComparison(t *testing.T) {
    c := &checker{
        frameLifetimeParents: map[int]int{
            1: 0,
            2: 1,
            3: 1,
            4: -1,
        },
    }
    parent := Lifetime{Kind: LifetimeFrame, Scope: 1}
    child := Lifetime{Kind: LifetimeFrame, Scope: 2}
    sibling := Lifetime{Kind: LifetimeFrame, Scope: 3}
    param := Lifetime{Kind: LifetimeFrame, Scope: -1}
    paramChild := Lifetime{Kind: LifetimeFrame, Scope: 4}

    if c.lifetimeShorterThan(parent, child) {
        t.Fatal("parent lifetime must be valid inside child frame")
    }
    if !c.lifetimeShorterThan(child, parent) {
        t.Fatal("child lifetime must not flow into parent frame")
    }
    if !c.lifetimeShorterThan(child, sibling) {
        t.Fatal("sibling frame lifetime must not flow into another sibling")
    }
    if c.lifetimeShorterThan(param, paramChild) {
        t.Fatal("frame parameter lifetime must be valid inside child frame")
    }
    if !c.lifetimeShorterThan(paramChild, param) {
        t.Fatal("child of frame parameter must not flow back into parameter frame")
    }
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `go test ./compiler/sem -run 'TestFrameEscapeNegativeFixtures|TestParentFrameValueCanBeUsedInsideChildFrame|TestChildFrameValueCannotBeStoredInParentFrame|TestFrameLifetimeAncestryComparison' -v`

Expected: FAIL because lifetimes are not tracked. The ancestry comparison test may fail to compile until the helper becomes a checker method in Step 5.

- [ ] **Step 5: Implement scoped lifetime tracking**

Add to `memory.go`:

```go
func (c *checker) rememberLifetime(expr ast.Expr, lifetime Lifetime) {
    if c.exprLifetimes == nil {
        c.exprLifetimes = map[ast.Expr]Lifetime{}
    }
    c.exprLifetimes[expr] = lifetime
}

func (c *checker) lifetimeOfExpr(expr ast.Expr, scope *Scope) Lifetime {
    if expr == nil {
        return Lifetime{Kind: LifetimeUnknown}
    }
    if c.exprLifetimes != nil {
        if lifetime, ok := c.exprLifetimes[expr]; ok {
            return lifetime
        }
    }
    switch e := expr.(type) {
    case *ast.NameExpr:
        if lifetime, ok := scope.LookupLifetime(e.Name); ok {
            return lifetime
        }
    case *ast.FieldExpr:
        return c.lifetimeOfExpr(e.Base, scope)
    case *ast.StringLiteral:
        return Lifetime{Kind: LifetimeStatic}
    case *ast.IntLiteral, *ast.BoolLiteral:
        return Lifetime{Kind: LifetimeStatic}
    }
    return Lifetime{Kind: LifetimeUnknown}
}

func (c *checker) rememberLocalLifetime(scope *Scope, name string, lifetime Lifetime) {
    if lifetime.Kind == LifetimeUnknown {
        lifetime = Lifetime{Kind: LifetimeExecutorRoot}
    }
    scope.DefineLifetime(name, lifetime)
}

func (c *checker) assignmentTargetLifetime(expr ast.Expr, scope *Scope) Lifetime {
    switch target := expr.(type) {
    case *ast.NameExpr:
        if lifetime, ok := scope.LookupLifetime(target.Name); ok {
            return lifetime
        }
        return Lifetime{Kind: LifetimeExecutorRoot}
    case *ast.FieldExpr:
        if name, ok := target.Base.(*ast.NameExpr); ok && name.Name == "self" {
            return Lifetime{Kind: LifetimeExecutorRoot}
        }
        return c.lifetimeOfExpr(target.Base, scope)
    default:
        return Lifetime{Kind: LifetimeExecutorRoot}
    }
}

func (c *checker) rejectIfLifetimeEscapes(span source.Span, value, target Lifetime) {
    if c.lifetimeShorterThan(value, target) {
        c.error(span, diag.SEM0025, "frame value cannot be stored in longer-lived state")
    }
}

func (c *checker) pushFrameLifetime(name string, span source.Span, parentLifetime Lifetime) int {
    c.nextFrameScope++
    id := c.nextFrameScope
    parent := 0
    if parentLifetime.Kind == LifetimeFrame || parentLifetime.Kind == LifetimeCacheLookup || parentLifetime.Kind == LifetimeCacheCopy {
        parent = parentLifetime.Scope
    }
    if c.frameLifetimeParents == nil {
        c.frameLifetimeParents = map[int]int{}
    }
    c.frameLifetimeParents[id] = parent
    c.frameLifetimeStack = append(c.frameLifetimeStack, id)
    return id
}

func (c *checker) popFrameLifetime(id int) {
    if len(c.frameLifetimeStack) == 0 {
        return
    }
    c.frameLifetimeStack = c.frameLifetimeStack[:len(c.frameLifetimeStack)-1]
}

func (c *checker) currentFrameLifetime() Lifetime {
    if len(c.frameLifetimeStack) == 0 {
        return Lifetime{Kind: LifetimeExecutorRoot}
    }
    return Lifetime{Kind: LifetimeFrame, Scope: c.frameLifetimeStack[len(c.frameLifetimeStack)-1]}
}

func (c *checker) frameIsAncestorOf(ancestor, descendant int) bool {
    if ancestor == 0 || descendant == 0 {
        return false
    }
    for current := descendant; current != 0; current = c.frameLifetimeParents[current] {
        if current == ancestor {
            return true
        }
    }
    return false
}

func (c *checker) lifetimeShorterThan(value, target Lifetime) bool {
    if value.Kind != LifetimeFrame && value.Kind != LifetimeCacheLookup && value.Kind != LifetimeCacheCopy {
        return false
    }
    if target.Kind == LifetimeFrame {
        if target.Scope == value.Scope {
            return false
        }
        if c.frameIsAncestorOf(value.Scope, target.Scope) {
            return false
        }
    }
    return true
}
```

Extend `Scope` in `compiler/sem/check.go`:

```go
type Scope struct {
    parent           *Scope
    types            map[string]*Type
    lifetimes        map[string]Lifetime
    driverPathKeys   map[string]string
    driverPathFields map[string]map[string]string
}

func NewScope(parent *Scope) *Scope {
    return &Scope{
        parent:           parent,
        types:            map[string]*Type{},
        lifetimes:        map[string]Lifetime{},
        driverPathKeys:   map[string]string{},
        driverPathFields: map[string]map[string]string{},
    }
}

func (s *Scope) DefineLifetime(name string, lifetime Lifetime) {
    if s != nil {
        s.lifetimes[name] = lifetime
    }
}

func (s *Scope) LookupLifetime(name string) (Lifetime, bool) {
    if s == nil {
        return Lifetime{}, false
    }
    if lifetime, ok := s.lifetimes[name]; ok {
        return lifetime
    }
    if s.parent != nil {
        return s.parent.LookupLifetime(name)
    }
    return Lifetime{}, false
}
```

Add these fields to the `checker` struct in `compiler/sem/check.go`:

```go
exprLifetimes      map[ast.Expr]Lifetime
frameLifetimeStack []int
frameLifetimeParents map[int]int
nextFrameScope     int
```

When `typeArenaIntrinsicCall` handles `place` or `reserve`, call `rememberLifetime(expr, currentFrameLifetime())`.

Inside the existing `case *ast.LetStmt:` branch in `checkStmt` in `compiler/sem/check.go`, after the current `scope.Define(s.Name, valueType)` call, store the expression lifetime:

```go
valueLifetime := c.lifetimeOfExpr(s.Expr, scope)
scope.Define(s.Name, valueType)
c.rememberLocalLifetime(scope, s.Name, valueLifetime)
```

When `ReturnStmt` has a value, reject frame lifetime:

```go
if lifetime := c.lifetimeOfExpr(s.Value, scope); lifetime.Kind == LifetimeFrame || lifetime.Kind == LifetimeCacheCopy {
    c.error(s.Value.Span(), diag.SEM0024, "frame value cannot escape with block")
}
```

When `AssignStmt` stores a value, compare the source and target lifetimes:

```go
sourceLifetime := c.lifetimeOfExpr(s.Value, scope)
targetLifetime := c.assignmentTargetLifetime(s.Target, scope)
c.rejectIfLifetimeEscapes(s.Value.Span(), sourceLifetime, targetLifetime)
```

- [ ] **Step 6: Run verification**

Run:

```bash
go test ./compiler/sem -run 'TestFrameEscapeNegativeFixtures|TestParentFrameValueCanBeUsedInsideChildFrame|TestChildFrameValueCannotBeStoredInParentFrame|TestFrameLifetimeAncestryComparison' -v
go test ./compiler/sem -v
go test ./compiler -run NegativeFixtures -v
git diff --check
```

Expected: all tests PASS.

- [ ] **Step 7: Commit**

```bash
git add compiler/sem/memory.go compiler/sem/check.go compiler/sem/memory_negative_test.go tests/fixtures/negative/frame_escape_return.wrela tests/fixtures/negative/frame_escape_field.wrela
git commit -m "feat: reject frame lifetime escapes -Codex Automated"
```

**Acceptance Criteria:** Returning a frame-placed value, assigning it to a longer-lived local or field, or storing it through `self` fails with SEM0024 or SEM0025. Parent-frame values can flow into child-frame state; child-frame values cannot flow into parent-frame state; sibling frame lifetimes are incompatible. Lifetime storage uses `map[ast.Expr]Lifetime` plus scoped local lifetimes, not source-span keys.

### Task 10: Method Lifetime Summaries

**Files:**
- Modify: `compiler/sem/memory.go`
- Modify: `compiler/sem/check.go`
- Modify: `compiler/sem/memory_test.go`

**Purpose:** Allow helper methods to receive frame memory while still rejecting returned or stored frame values.

- [ ] **Step 1: Write failing helper-method test**

Add:

```go
func TestFrameLifetimeFlowsThroughHelperMethod(t *testing.T) {
    modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

data Message { id: U64 }
class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame { return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0) }
}
class ArenaFrame { arena_base: PhysicalAddress arena_length: U64 next_offset: U64 }
class Parser {
    fn parse(self, heap: ArenaFrame) -> Message {
        let msg = heap.place(Message(id = 1))
        return msg
    }
}
executor Worker {
    memory: ExecutorMemory
    parser: Parser
    start fn run(self) -> never {
        with self.memory.frame(length = 64) as tick {
            let msg = self.parser.parse(heap = tick)
        }
        while true {}
    }
}
`)
    index, ds := BuildIndex(modules)
    ds = filterMissingImageDiagnostic(ds)
    if len(ds) != 0 {
        t.Fatalf("index diagnostics: %#v", ds)
    }
    _, diags := Check(index, modules)
    if len(diags) != 0 {
        t.Fatalf("check diagnostics: %#v", diags)
    }
}
```

- [ ] **Step 2: Write failing illegal helper test**

Add:

```go
func TestHelperCannotHideFrameEscape(t *testing.T) {
    modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

data Message { id: U64 }
class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame { return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0) }
}
class ArenaFrame { arena_base: PhysicalAddress arena_length: U64 next_offset: U64 }
class Parser {
    saved: Message
    fn parse(self, heap: ArenaFrame) -> Message {
        let msg = heap.place(Message(id = 1))
        self.saved = msg
        return msg
    }
}
`)
    index, ds := BuildIndex(modules)
    ds = filterMissingImageDiagnostic(ds)
    if len(ds) != 0 {
        t.Fatalf("index diagnostics: %#v", ds)
    }
    _, diags := Check(index, modules)
    if !hasCode(diags, diag.SEM0025) {
        t.Fatalf("expected SEM0025, got %#v", diags)
    }
}
```

Add a named-argument mapping regression. This test must fail with SEM0025: `Parser.parse` returns the `right` frame's value, and the call intentionally passes `right` before `left`. If the implementation indexes directly into `expr.Args`, it will incorrectly treat the result as coming from `left_frame` and allow the parent-frame field store.

```go
func TestMethodLifetimeNamedArgumentsUseParameterMapping(t *testing.T) {
    modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

data Message { id: U64 }
data Box { msg: Message }
class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame { return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0) }
}
class ArenaFrame {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame { return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0) }
}
class Parser {
    fn parse(self, left: ArenaFrame, right: ArenaFrame) -> Message {
        let msg = right.place(Message(id = 1))
        return msg
    }
}
executor Worker {
    memory: ExecutorMemory
    parser: Parser
    start fn run(self) -> never {
        with self.memory.frame(length = 128) as left_frame {
            let parent_box = left_frame.place(Box(msg = Message(id = 0)))
            with left_frame.frame(length = 64) as child_frame {
                parent_box.msg = self.parser.parse(right = child_frame, left = left_frame)
            }
        }
        while true {}
    }
}
`)
    index, ds := BuildIndex(modules)
    ds = filterMissingImageDiagnostic(ds)
    if len(ds) != 0 {
        t.Fatalf("index diagnostics: %#v", ds)
    }
    _, diags := Check(index, modules)
    if !hasCode(diags, diag.SEM0025) {
        t.Fatalf("expected SEM0025, got %#v", diags)
    }
}
```

Add a forward-reference regression. This test must pass after implementation: `Worker` appears before `Parser`, so method summaries must be registered for all types before any body is checked. If summaries are registered only inside the current `checkMethods` call, the call to `Parser.parse` falls back to root lifetime and this test loses coverage.

```go
func TestMethodLifetimeForwardReferenceAcrossTypes(t *testing.T) {
    modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

data Message { id: U64 }
class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame { return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0) }
}
class ArenaFrame { arena_base: PhysicalAddress arena_length: U64 next_offset: U64 }
executor Worker {
    memory: ExecutorMemory
    parser: Parser
    start fn run(self) -> never {
        with self.memory.frame(length = 64) as tick {
            let msg = self.parser.parse(heap = tick)
        }
        while true {}
    }
}
class Parser {
    fn parse(self, heap: ArenaFrame) -> Message {
        let msg = heap.place(Message(id = 1))
        return msg
    }
}
`)
    index, ds := BuildIndex(modules)
    ds = filterMissingImageDiagnostic(ds)
    if len(ds) != 0 {
        t.Fatalf("index diagnostics: %#v", ds)
    }
    _, diags := Check(index, modules)
    if len(diags) != 0 {
        t.Fatalf("check diagnostics: %#v", diags)
    }
}
```

Add a frame-parameter child-frame regression. This test must pass after implementation: the `heap` parameter has a negative method-summary scope, and `heap.frame(...)` must record that parameter scope as the parent of the child frame. If `pushFrameLifetime` only reads the lexical frame stack, the parent value `msg` looks unrelated to `child` and is rejected incorrectly.

```go
func TestFrameParameterParentsNestedFrame(t *testing.T) {
    modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

data Message { id: U64 }
data Box { msg: Message }
class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame { return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0) }
}
class ArenaFrame {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame { return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0) }
}
class Parser {
    fn fill_child(self, heap: ArenaFrame) -> Message {
        let msg = heap.place(Message(id = 1))
        with heap.frame(length = 64) as child {
            let box = child.place(Box(msg = Message(id = 0)))
            box.msg = msg
        }
        return msg
    }
}
executor Worker {
    memory: ExecutorMemory
    parser: Parser
    start fn run(self) -> never {
        with self.memory.frame(length = 128) as tick {
            let msg = self.parser.fill_child(heap = tick)
        }
        while true {}
    }
}
`)
    index, ds := BuildIndex(modules)
    ds = filterMissingImageDiagnostic(ds)
    if len(ds) != 0 {
        t.Fatalf("index diagnostics: %#v", ds)
    }
    _, diags := Check(index, modules)
    if len(diags) != 0 {
        t.Fatalf("check diagnostics: %#v", diags)
    }
}
```

- [ ] **Step 3: Run tests to verify failure**

Run: `go test ./compiler/sem -run 'TestFrameLifetimeFlowsThroughHelperMethod|TestHelperCannotHideFrameEscape|TestMethodLifetimeNamedArgumentsUseParameterMapping|TestMethodLifetimeForwardReferenceAcrossTypes|TestFrameParameterParentsNestedFrame' -v`

Expected: FAIL because method calls do not propagate arena argument lifetime to return values.

- [ ] **Step 4: Implement summaries**

Add:

```go
type MethodLifetimeSummary struct {
    ReturnFromParam int          // -1 when not parameter-derived
    ReturnKind      LifetimeKind // valid only when ReturnFromParam >= 0
    ReturnStatic    bool
    ReturnRoot      bool
    Terminates      bool
    Invalid         bool
}
```

Initialize `ReturnFromParam` to `-1` and `ReturnKind` to `LifetimeUnknown`.

Add these fields to `checker`:

```go
methodLifetimeTargets   map[string]methodLifetimeTarget
methodLifetimeSummaries map[string]MethodLifetimeSummary
activeMethodSummaries   map[string]bool
currentMethodSummary    *MethodLifetimeSummary
```

Summaries are keyed by `typ.Module + "." + typ.Name + "::" + method.Name`.

Add this small target record near the summary type:

```go
type methodLifetimeTarget struct {
    ModuleName string
    Type       *Type
    Method     ast.MethodDecl
    ReturnType *Type
    Context    ContextKind
}
```

Register every non-asm method target for the entire program before checking any method body. This is required so a method can safely call another method declared later in the same type or in a type declared later in another module.

Add this helper near `checkDeclBodiesAndConstructors`:

```go
func (c *checker) registerMethodLifetimeTargets() {
    if c.methodLifetimeTargets == nil {
        c.methodLifetimeTargets = map[string]methodLifetimeTarget{}
    }
    for _, mod := range c.modules {
        for _, decl := range mod.Decls {
            var typeName string
            var methods []ast.MethodDecl
            switch d := decl.(type) {
            case *ast.ClassDecl:
                typeName, methods = d.Name, d.Methods
            case *ast.DriverDecl:
                typeName, methods = d.Name, d.Methods
            case *ast.DriverPathDecl:
                typeName, methods = d.Name, d.Methods
            case *ast.ExecutorDecl:
                typeName, methods = d.Name, d.Methods
            default:
                continue
            }
            typ := c.index.resolveInScope(mod.Name, typeName)
            for _, method := range methods {
                if method.IsAsm || isCanonicalFrameIntrinsic(mod.Name, typ, method) {
                    continue
                }
                marker := ContextNormalMethod
                returnType := c.mustType(mod.Name, method.Return)
                if c.isOwnershipTransferAuthority(typ) && returnType == c.ownedRoot {
                    marker = ContextOwnershipTransferAuthorityMethod
                }
                c.methodLifetimeTargets[methodLifetimeKey(typ, method.Name)] = methodLifetimeTarget{
                    ModuleName: mod.Name,
                    Type:       typ,
                    Method:     method,
                    ReturnType: returnType,
                    Context:    marker,
                }
            }
        }
    }
}
```

Call it once at the start of `checkDeclBodiesAndConstructors`, before the existing loop over modules and declarations:

```go
func (c *checker) checkDeclBodiesAndConstructors() {
    c.registerMethodLifetimeTargets()
    for _, mod := range c.modules {
        // existing body checking loop stays here
    }
}
```

Do not register targets inside `checkMethods`; that misses methods on later types. `checkMethods` should only verify parameter count, asm placement, and non-asm method bodies via `ensureMethodLifetimeSummary`.

Then replace the existing direct `checkStmtList(...)` body check in `checkMethods` with:

```go
key := methodLifetimeKey(typ, method.Name)
summary := c.ensureMethodLifetimeSummary(key, method.SpanV)
if returnType != nil && !summary.Terminates {
    c.error(method.SpanV, diag.CG0001, "missing return")
}
```

Use this exact algorithm when checking a non-asm method. It performs the normal semantic body check and records the lifetime summary in the same pass; do not add separate `beginMethodLifetimeSummary` or `finishMethodLifetimeSummary` helpers.

```go
func (c *checker) ensureMethodLifetimeSummary(key string, span source.Span) MethodLifetimeSummary {
    if summary, ok := c.methodLifetimeSummaries[key]; ok {
        return summary
    }
    target, ok := c.methodLifetimeTargets[key]
    if !ok {
        return MethodLifetimeSummary{ReturnFromParam: -1, ReturnRoot: true, Terminates: true}
    }
    return c.checkMethodWithLifetimeSummary(key, target, span)
}

func (c *checker) checkMethodWithLifetimeSummary(key string, target methodLifetimeTarget, span source.Span) MethodLifetimeSummary {
    moduleName := target.ModuleName
    typ := target.Type
    method := target.Method
    summary := MethodLifetimeSummary{ReturnFromParam: -1}

    if c.methodLifetimeSummaries == nil {
        c.methodLifetimeSummaries = map[string]MethodLifetimeSummary{}
    }
    if c.activeMethodSummaries == nil {
        c.activeMethodSummaries = map[string]bool{}
    }
    if c.activeMethodSummaries[key] {
        c.error(span, diag.SEM0024, "recursive frame lifetime summary is not supported")
        summary.Invalid = true
        c.methodLifetimeSummaries[key] = summary
        return summary
    }

    scope := c.newMethodLifetimeScope(moduleName, typ, method)
    prev := c.currentMethodSummary
    prevPhase := c.currentPhase
    c.currentMethodSummary = &summary
    c.currentPhase = method.Name
    c.activeMethodSummaries[key] = true
    terminates := c.checkStmtList(moduleName, method.Body, scope, target.ReturnType, target.Context)
    summary.Terminates = terminates
    delete(c.activeMethodSummaries, key)
    c.currentPhase = prevPhase
    c.currentMethodSummary = prev
    c.methodLifetimeSummaries[key] = summary
    return summary
}

func methodLifetimeKey(typ *Type, methodName string) string {
    if typ == nil {
        return "::" + methodName
    }
    return typ.Module + "." + typ.Name + "::" + methodName
}
```

Use this helper for method scopes. It replaces the current inline parameter `scope.Define(...)` loop inside the method body checker:

```go
func (c *checker) newMethodLifetimeScope(moduleName string, typ *Type, method ast.MethodDecl) *Scope {
    scope := NewScope(nil)
    if len(method.Params) > 0 && method.Params[0].Name == "self" {
        scope.Define("self", typ)
        scope.DefineLifetime("self", Lifetime{Kind: LifetimeExecutorRoot})
    }

explicitIndex := 0
for _, p := range method.Params {
    if p.Name == "self" {
        continue
    }
    paramType := c.mustType(moduleName, p.Type)
    scope.Define(p.Name, paramType)
    if ClassifyMemoryType(paramType) == MemoryKindFrameArena {
        scope.DefineLifetime(p.Name, Lifetime{Kind: LifetimeFrame, Scope: -(explicitIndex + 1)})
    } else {
        scope.DefineLifetime(p.Name, Lifetime{Kind: LifetimeExecutorRoot})
    }
    explicitIndex++
}
    return scope
}
```

Return analysis:

```text
For each ReturnStmt with value:
  lifetime = lifetimeOfExpr(value, scope)
  if lifetime.Kind == LifetimeStatic:
      summary.ReturnStatic = true
  else if lifetime.Kind in {LifetimeFrame, LifetimeCacheLookup, LifetimeCacheCopy} and lifetime.Scope < 0:
      paramIndex = -lifetime.Scope - 1
      if summary.ReturnFromParam == -1:
          summary.ReturnFromParam = paramIndex
          summary.ReturnKind = lifetime.Kind
      else if summary.ReturnFromParam != paramIndex or summary.ReturnKind != lifetime.Kind:
          emit SEM0024 and set summary.Invalid
  else if lifetime.Kind == LifetimeFrame or lifetime.Kind == LifetimeCacheLookup or lifetime.Kind == LifetimeCacheCopy:
      emit SEM0024 and set summary.Invalid
  else:
      summary.ReturnRoot = true
```

Implement the return hook in the existing `ReturnStmt` branch, before the normal escape rejection:

```go
func (c *checker) recordReturnLifetime(span source.Span, lifetime Lifetime) {
    if c.currentMethodSummary == nil {
        return
    }
    summary := c.currentMethodSummary
    switch {
    case lifetime.Kind == LifetimeStatic:
        if summary.ReturnFromParam >= 0 {
            c.error(span, diag.SEM0024, "method returns incompatible frame lifetimes")
            summary.Invalid = true
            return
        }
        summary.ReturnStatic = true
    case (lifetime.Kind == LifetimeFrame || lifetime.Kind == LifetimeCacheLookup || lifetime.Kind == LifetimeCacheCopy) && lifetime.Scope < 0:
        paramIndex := -lifetime.Scope - 1
        if summary.ReturnRoot || summary.ReturnStatic {
            c.error(span, diag.SEM0024, "method returns incompatible frame lifetimes")
            summary.Invalid = true
            return
        }
        if summary.ReturnFromParam == -1 {
            summary.ReturnFromParam = paramIndex
            summary.ReturnKind = lifetime.Kind
            return
        }
        if summary.ReturnFromParam != paramIndex || summary.ReturnKind != lifetime.Kind {
            c.error(span, diag.SEM0024, "method returns incompatible frame lifetimes")
            summary.Invalid = true
        }
    case lifetime.Kind == LifetimeFrame || lifetime.Kind == LifetimeCacheLookup || lifetime.Kind == LifetimeCacheCopy:
        c.error(span, diag.SEM0024, "frame value cannot escape with block")
        summary.Invalid = true
    default:
        if summary.ReturnFromParam >= 0 {
            c.error(span, diag.SEM0024, "method returns incompatible frame lifetimes")
            summary.Invalid = true
            return
        }
        summary.ReturnRoot = true
    }
}
```

Then update the existing `ReturnStmt` branch so parameter-derived method returns are recorded but not rejected as escaping. This is the critical distinction between "a method returns a value tied to its frame parameter" and "a frame value escapes to executor root":

```go
lifetime := c.lifetimeOfExpr(s.Value, scope)
c.recordReturnLifetime(s.Value.Span(), lifetime)
if c.currentMethodSummary != nil &&
    (lifetime.Kind == LifetimeFrame || lifetime.Kind == LifetimeCacheLookup || lifetime.Kind == LifetimeCacheCopy) &&
    lifetime.Scope < 0 {
    return true
}
if lifetime.Kind == LifetimeFrame {
    c.error(s.Value.Span(), diag.SEM0024, "frame value cannot escape with block")
}
```

Without that `Scope < 0` short-circuit, `Parser.parse(heap: ArenaFrame) -> Message` and `ResponseCache.get(into: ArenaFrame) -> CacheLookup` summarize correctly and then fail the generic root-escape rule anyway.

Branch rule:

```text
If one branch returns from frame parameter 0 and another returns from frame parameter 1, the method is invalid with SEM0024. If one branch returns a frame parameter and another returns root/static, the method is invalid with SEM0024. Junior implementers must not silently choose one branch.
```

Assignment rule:

```text
Reuse Task 9's assignmentTargetLifetime/rejectIfLifetimeEscapes logic while checking method bodies. This catches self.saved = msg and also field writes through any root-lifetime receiver.
```

At call sites, if `ReturnFromParam >= 0`, copy the matched argument expression lifetime to the call expression. `ReturnFromParam` is an explicit method-parameter index after removing `self`; it is not necessarily the same index as `expr.Args` because callers can use named arguments.

```go
summary := c.ensureMethodLifetimeSummary(methodLifetimeKey(recvType, method.Name), expr.SpanV)
if !summary.Invalid {
    if summary.ReturnFromParam >= 0 {
        arg := callArgForParam(method, expr.Args, summary.ReturnFromParam)
        argLifetime := c.lifetimeOfExpr(arg, scope)
        if summary.ReturnKind == LifetimeCacheLookup || summary.ReturnKind == LifetimeCacheCopy {
            c.rememberLifetime(expr, Lifetime{Kind: summary.ReturnKind, Scope: argLifetime.Scope})
        } else {
            c.rememberLifetime(expr, argLifetime)
        }
    } else if summary.ReturnStatic {
        c.rememberLifetime(expr, Lifetime{Kind: LifetimeStatic})
    } else {
        c.rememberLifetime(expr, Lifetime{Kind: LifetimeExecutorRoot})
    }
}
```

Do not use a bare `return` inside `typeCallExpr` for invalid summaries; `typeCallExpr` returns `*Type`. Invalid summaries should skip lifetime propagation while the normal call type checking still returns `method.Return`.

This rule is also what makes the Section 4 wrapper legal:

```wrela
fn get(self, key: U64, into: ArenaFrame) -> CacheLookup {
    return self.memory.get_bytes(key = key, into = into)
}
```

The wrapper summarizes as `ReturnFromParam = into`, `ReturnKind = LifetimeCacheLookup`. At each call site the returned `CacheLookup` receives the caller's `into` frame scope. It is still rejected by Task 17 if that whole `CacheLookup` or its `.bytes` field escapes the frame.

Add this helper next to the method-summary code. It must mirror the matching behavior in `typeAndVerifyCallArgs`:

```go
func callArgForParam(method *Method, args []ast.NamedArg, paramIndex int) ast.Expr {
    if method == nil || paramIndex < 0 {
        return nil
    }
    params := method.Params
    if len(params) > 0 && params[0].Name == "self" {
        params = params[1:]
    }
    if paramIndex >= len(params) {
        return nil
    }
    param := params[paramIndex]
    positional := 0
    for _, arg := range args {
        if arg.Name != "" {
            if arg.Name == param.Name {
                return arg.Value
            }
            continue
        }
        if positional == paramIndex {
            return arg.Value
        }
        positional++
    }
    return nil
}
```

Unknown method summary rule:

```text
Call-site lifetime propagation must read summaries only through ensureMethodLifetimeSummary. If a method has not been summarized yet, ensureMethodLifetimeSummary checks it before the call result is classified. Do not assume unknown in-tree methods return root lifetime. Recursive methods that return frame-derived values are out of scope for this plan; if recursion is detected while summarizing, emit SEM0024 on the recursive call.
```

- [ ] **Step 5: Run verification**

Run:

```bash
go test ./compiler/sem -run 'TestFrameLifetimeFlowsThroughHelperMethod|TestHelperCannotHideFrameEscape|TestMethodLifetimeNamedArgumentsUseParameterMapping|TestMethodLifetimeForwardReferenceAcrossTypes|TestFrameParameterParentsNestedFrame' -v
go test ./compiler/sem -v
git diff --check
```

Expected: all tests PASS.

- [ ] **Step 6: Commit**

```bash
git add compiler/sem/memory.go compiler/sem/check.go compiler/sem/memory_test.go
git commit -m "feat: infer arena method lifetimes -Codex Automated"
```

**Acceptance Criteria:** Helper methods can place values into caller-provided frame arenas; returned values carry the caller's frame lifetime; hidden storage into longer-lived fields is rejected.

---

## 8. Phase 4: IR And Codegen For Frames, Place, And Reserve

### Task 11: IR Memory Operations

**Files:**
- Modify: `compiler/ir/ir.go`
- Create: `compiler/ir/memory_test.go`

**Purpose:** Represent memory-frame operations explicitly before lowering reaches x64 codegen.

- [ ] **Step 1: Write failing IR test**

Create `compiler/ir/memory_test.go`:

```go
package ir

import "testing"

func TestMemoryOpsDefineExpectedValues(t *testing.T) {
    frame := &FrameBegin{Symbol: "tick", Parent: Local{Symbol: "memory"}, Length: ConstInt{Value: 64}}
    reserve := &ArenaReserve{Arena: frame, Length: ConstInt{Value: 32}, Align: ConstInt{Value: 8}}
    place := &ArenaPlace{Arena: frame, Type: Type{Name: "Message"}}
    fn := Function{Blocks: []Block{{Label: "entry", Ops: []Operation{frame, reserve, place, &FrameEnd{Frame: frame}}}}}
    values := fn.ValuesInDeterministicOrder()
    if len(values) != 3 {
        t.Fatalf("values = %#v, want frame reserve place", values)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./compiler/ir -run TestMemoryOpsDefineExpectedValues -v`

Expected: FAIL with undefined memory operation types.

- [ ] **Step 3: Add IR types**

Add to `compiler/ir/ir.go`:

```go
type FrameBegin struct {
    Symbol string
    Parent Value
    Length Value
    Type   Type
}

func (*FrameBegin) isValue()     {}
func (*FrameBegin) isOperation() {}

type FrameEnd struct {
    Frame *FrameBegin
}

func (*FrameEnd) isOperation() {}

type ArenaReserve struct {
    Arena  Value
    Length Value
    Align  Value
    Type   Type
}

func (*ArenaReserve) isValue()     {}
func (*ArenaReserve) isOperation() {}

type ArenaPlace struct {
    Arena  Value
    Type   Type
    Fields []FieldValue
}

func (*ArenaPlace) isValue()     {}
func (*ArenaPlace) isOperation() {}

```

Update `valuesDefinedBy` for these operations:

```go
case *FrameBegin:
    return []Value{v}
case *ArenaReserve:
    return []Value{v}
case *ArenaPlace:
    return []Value{v}
```

- [ ] **Step 4: Run verification**

Run:

```bash
go test ./compiler/ir -run TestMemoryOpsDefineExpectedValues -v
go test ./compiler/ir -v
git diff --check
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add compiler/ir/ir.go compiler/ir/memory_test.go
git commit -m "feat: add arena memory ir operations -Codex Automated"
```

**Acceptance Criteria:** IR has explicit operations for frame begin/end, reserve, and place; the backend emits the `_wrela_memory_oom` trap target.

### Task 12: Lower WithStmt And Arena Intrinsics To IR

**Files:**
- Modify: `compiler/ir/lower.go`
- Modify: `compiler/ir/lower_test.go`

**Purpose:** Convert source memory constructs into explicit IR operations.

- [ ] **Step 1: Write failing lower test**

Add to `compiler/ir/lower_test.go`:

```go
func TestLowerWithFrameReserveAndPlace(t *testing.T) {
    checked := checkedProgramForSource(t, `
module machine.x86_64.executor_memory

data Message { id: U64 }
data MutableBytes { address: PhysicalAddress length: U64 }
class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame { return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0) }
}
class ArenaFrame { arena_base: PhysicalAddress arena_length: U64 next_offset: U64 }
executor Worker {
    memory: ExecutorMemory
    start fn run(self) -> never {
        with self.memory.frame(length = 64) as tick {
            let msg = tick.place(Message(id = 1))
            let raw = tick.reserve(length = 32, align = 8)
        }
        while true {}
    }
}
`)
    program, diags := Lower(checked)
    if len(diags) != 0 {
        t.Fatalf("lower diagnostics: %#v", diags)
    }
    fn := findFunction(program, "_wrela_method_machine_x86_64_executor_memory_Worker_run")
    if fn == nil {
        t.Fatal("missing Worker.run")
    }
    if !containsOp[*FrameBegin](*fn) || !containsOp[*ArenaPlace](*fn) || !containsOp[*ArenaReserve](*fn) || !containsOp[*FrameEnd](*fn) {
        t.Fatalf("lowered function missing arena ops: %#v", fn.Blocks)
    }
}
```

Add this helper to `compiler/ir/lower_test.go` if it does not already exist:

```go
func checkedProgramForSource(t *testing.T, src string) *sem.CheckedProgram {
    t.Helper()
    files := []*source.File{
        source.NewFile(source.FileID(1), "memory-lower-test.wrela", src),
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

func containsOp[T any](fn Function) bool {
    for _, block := range fn.Blocks {
        for _, op := range block.Ops {
            if _, ok := any(op).(T); ok {
                return true
            }
        }
    }
    return false
}
```

Reuse the existing `filterMissingImageDiagnostic` helper already present in the `compiler/ir` test package; do not add a duplicate. Add imports for `compiler/parse` and `compiler/source` in the same file:

```go
    "github.com/ryanwible/wrela3/compiler/parse"
    "github.com/ryanwible/wrela3/compiler/source"
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./compiler/ir -run TestLowerWithFrameReserveAndPlace -v`

Expected: FAIL because `WithStmt` and intrinsics are not lowered.

- [ ] **Step 3: Lower with**

In the existing `lowerContext` struct in `compiler/ir/lower.go`, add the active-frame stack:

```go
activeFrames []*FrameBegin
```

In the existing `lowerStmt(moduleName string, receiverType *sem.Type, scope *lowerScope, assigned map[string]bool, stmt ast.Stmt)` switch, add this case:

```go
case *ast.WithStmt:
    parent, frameOps, length := ctx.lowerFrameCall(moduleName, receiverType, scope, s.Expr)
    frameType := ctx.resolveType("machine.x86_64.executor_memory", "ArenaFrame")
    frame := &FrameBegin{
        Symbol: s.Name,
        Parent: parent,
        Length: length,
        Type:   ctx.irType(frameType),
    }

    child := newLowerScope(scope)
    child.define(s.Name, lowerBinding{value: frame, typ: frameType})

    ctx.activeFrames = append(ctx.activeFrames, frame)
    body := ctx.lowerStmtList(moduleName, receiverType, child, assigned, s.Body)
    ctx.activeFrames = ctx.activeFrames[:len(ctx.activeFrames)-1]

    ops := append([]Operation{}, frameOps...)
    ops = append(ops, frame)
    ops = append(ops, body...)
    ops = append(ops, &FrameEnd{Frame: frame})
    return ops
}
```

Add this helper near `lowerStmt`:

```go
func (ctx *lowerContext) lowerFrameCall(moduleName string, receiverType *sem.Type, scope *lowerScope, expr ast.Expr) (Value, []Operation, Value) {
    call, ok := expr.(*ast.CallExpr)
    if !ok || call.Method != "frame" {
        ctx.errorf("with expression was not a frame call")
        zero := &ConstInt{Value: 0, Type: Type{Name: "U64", Kind: TypeKindPrimitive}}
        return zero, []Operation{zero}, zero
    }

    parent, parentOps, _ := ctx.lowerExpr(moduleName, receiverType, scope, call.Receiver)
    lengthExpr := namedArgExpr(call.Args, "length")
    length, lengthOps, _ := ctx.lowerExpr(moduleName, receiverType, scope, lengthExpr)
    ops := append([]Operation{}, parentOps...)
    ops = append(ops, lengthOps...)
    return parent, ops, length
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

For `return` inside a `with` body, replace the existing `case *ast.ReturnStmt:` branch with:

```go
case *ast.ReturnStmt:
    if s.Value == nil {
        return ctx.lowerReturn(nil, nil)
    }
    value, valueOps, _ := ctx.lowerExpr(moduleName, receiverType, scope, s.Value)
    return ctx.lowerReturn(value, valueOps)
```

Then add:

```go
func (ctx *lowerContext) lowerReturn(value Value, prefix []Operation) []Operation {
    ops := append([]Operation{}, prefix...)
    for i := len(ctx.activeFrames) - 1; i >= 0; i-- {
        ops = append(ops, &FrameEnd{Frame: ctx.activeFrames[i]})
    }
    if value == nil {
        ops = append(ops, &Return{})
    } else {
        ops = append(ops, &Return{Value: value})
    }
    return ops
}
```

When lowering `WithStmt`, push the new frame before lowering the body and pop it afterward. Wrela does not have `break`, `continue`, exceptions, or closures in this plan, so `return` is the only non-fallthrough unwind path.

- [ ] **Step 4: Lower intrinsics**

In the existing `case *ast.CallExpr:` branch of `lowerExpr`, lower the receiver first, then special-case arena intrinsics before normal method lookup:

```go
receiver, receiverOps, recvType := ctx.lowerExpr(moduleName, receiverType, scope, e.Receiver)
if sem.ClassifyMemoryType(recvType) == sem.MemoryKindFrameArena || sem.ClassifyMemoryType(recvType) == sem.MemoryKindRootArena {
    switch e.Method {
    case "reserve":
        length, lengthOps, _ := ctx.lowerExpr(moduleName, receiverType, scope, namedArgExpr(e.Args, "length"))
        align, alignOps, _ := ctx.lowerExpr(moduleName, receiverType, scope, namedArgExpr(e.Args, "align"))
        mutableType := ctx.resolveType("machine.x86_64.executor_memory", "MutableBytes")
        reserve := &ArenaReserve{Arena: receiver, Length: length, Align: align, Type: ctx.irType(mutableType)}
        ops := append([]Operation{}, receiverOps...)
        ops = append(ops, lengthOps...)
        ops = append(ops, alignOps...)
        ops = append(ops, reserve)
        return reserve, ops, mutableType
    case "place":
        cons, ok := e.Args[0].Value.(*ast.ConstructorExpr)
        if !ok {
            ctx.errorf("place argument was not a constructor")
            return receiver, receiverOps, recvType
        }
        placedType := ctx.resolveType(moduleName, cons.Type)
        fields := make([]FieldValue, 0, len(cons.Args))
        ops := append([]Operation{}, receiverOps...)
        for _, arg := range cons.Args {
            value, valueOps, _ := ctx.lowerExpr(moduleName, receiverType, scope, arg.Value)
            ops = append(ops, valueOps...)
            fields = append(fields, FieldValue{Name: arg.Name, Value: value})
        }
        place := &ArenaPlace{Arena: receiver, Type: ctx.irType(placedType), Fields: fields}
        ops = append(ops, place)
        return place, ops, placedType
    }
}
```

After this block, keep the existing normal-method lowering path, but reuse the already-lowered `receiver`, `receiverOps`, and `recvType`; do not lower the receiver a second time.

- [ ] **Step 5: Run verification**

Run:

```bash
go test ./compiler/ir -run TestLowerWithFrameReserveAndPlace -v
go test ./compiler/ir -v
git diff --check
```

Expected: all tests PASS.

- [ ] **Step 6: Commit**

```bash
git add compiler/ir/lower.go compiler/ir/lower_test.go
git commit -m "feat: lower arena frames to ir -Codex Automated"
```

**Acceptance Criteria:** `WithStmt` lowers to `FrameBegin`, body ops, and `FrameEnd`; `place` and `reserve` lower to arena IR operations; early returns unwind active frames.

### Task 13: Codegen For Bounded Frames And Reserve

**Files:**
- Modify: `compiler/codegen/x64.go`
- Create: `compiler/codegen/memory_test.go`

**Purpose:** Emit direct physical-address bump behavior with bounds checks and frame rewinds.

- [ ] **Step 1: Write failing byte-level test**

Create `compiler/codegen/memory_test.go`:

```go
package codegen

import (
    "encoding/binary"
    "testing"

    "github.com/ryanwible/wrela3/compiler/ir"
)

func TestArenaReserveEmitsBoundsTrapAndBump(t *testing.T) {
    program := testProgramWithArenaReserve(t)
    image, diags := Compile(program)
    if len(diags) != 0 {
        t.Fatalf("compile diagnostics: %#v", diags)
    }
    code := symbolBytes(t, image, "_wrela_method_test_Worker_run")
    if _, ok := image.Symbols["_wrela_memory_oom"]; !ok {
        t.Fatalf("missing memory trap symbol")
    }
    if !codeCallsSymbol(t, image, "_wrela_method_test_Worker_run", "_wrela_memory_oom") {
        t.Fatalf("reserve/frame code must call _wrela_memory_oom on bounds failure: %x", code)
    }
    for name, want := range map[string][]byte{
        "frame length 64":  {0x40, 0x00, 0x00, 0x00},
        "reserve length 32": {0x20, 0x00, 0x00, 0x00},
        "reserve align 8": {0x08, 0x00, 0x00, 0x00},
    } {
        if !containsBytes(code, want) {
            t.Fatalf("reserve code missing %s constant %x in %x", name, want, code)
        }
    }
}

func codeCallsSymbol(t *testing.T, image *Image, caller, target string) bool {
    t.Helper()
    callerRVA := image.Symbols[caller]
    targetRVA := image.Symbols[target]
    code := symbolBytes(t, image, caller)
    for i := 0; i+5 <= len(code); i++ {
        if code[i] != 0xE8 {
            continue
        }
        rel := int32(binary.LittleEndian.Uint32(code[i+1 : i+5]))
        got := uint64(int64(callerRVA) + int64(i) + 5 + int64(rel))
        if got == targetRVA {
            return true
        }
    }
    return false
}

func testProgramWithArenaReserve(t *testing.T) *ir.Program {
    t.Helper()
    memoryType := ir.Type{Name: "ExecutorMemory", Module: "machine.x86_64.executor_memory", Kind: ir.TypeKindClass}
    frameType := ir.Type{Name: "ArenaFrame", Module: "machine.x86_64.executor_memory", Kind: ir.TypeKindClass}
    mutableBytes := ir.Type{Name: "MutableBytes", Module: "machine.x86_64.executor_memory", Kind: ir.TypeKindData}
    memory := &ir.Local{Symbol: "memory", Type: memoryType}
    frameLen := &ir.ConstInt{Symbol: "frame_len", Value: 64, Type: ir.Type{Name: "U64"}}
    reserveLen := &ir.ConstInt{Symbol: "reserve_len", Value: 32, Type: ir.Type{Name: "U64"}}
    reserveAlign := &ir.ConstInt{Symbol: "reserve_align", Value: 8, Type: ir.Type{Name: "U64"}}
    frame := &ir.FrameBegin{Symbol: "tick", Parent: memory, Length: frameLen, Type: frameType}
    reserve := &ir.ArenaReserve{Arena: frame, Length: reserveLen, Align: reserveAlign, Type: mutableBytes}
    return &ir.Program{
        Types: arenaTestTypes(),
        Functions: []ir.Function{{
            Symbol: "_wrela_method_test_Worker_run",
            Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
                memory,
                frameLen,
                reserveLen,
                reserveAlign,
                frame,
                reserve,
                &ir.FrameEnd{Frame: frame},
                &ir.Return{},
            }}},
        }},
    }
}

func arenaTestTypes() map[string]ir.TypeInfo {
    return map[string]ir.TypeInfo{
        "machine.x86_64.executor_memory.ExecutorMemory": {
            Name: "ExecutorMemory", Module: "machine.x86_64.executor_memory", Kind: ir.TypeKindClass, Size: 24, Align: 8, StorageSize: 24,
            Fields: map[string]ir.FieldInfo{
                "arena_base":   {Name: "arena_base", Offset: 0, Size: 8, Align: 8, StorageOffset: 0, StorageSize: 8, Type: ir.Type{Name: "PhysicalAddress"}},
                "arena_length": {Name: "arena_length", Offset: 8, Size: 8, Align: 8, StorageOffset: 8, StorageSize: 8, Type: ir.Type{Name: "U64"}},
                "next_offset":  {Name: "next_offset", Offset: 16, Size: 8, Align: 8, StorageOffset: 16, StorageSize: 8, Type: ir.Type{Name: "U64"}},
            },
            FieldOrder: []string{"arena_base", "arena_length", "next_offset"},
        },
        "machine.x86_64.executor_memory.ArenaFrame": {
            Name: "ArenaFrame", Module: "machine.x86_64.executor_memory", Kind: ir.TypeKindClass, Size: 24, Align: 8, StorageSize: 24,
            Fields: map[string]ir.FieldInfo{
                "arena_base":   {Name: "arena_base", Offset: 0, Size: 8, Align: 8, StorageOffset: 0, StorageSize: 8, Type: ir.Type{Name: "PhysicalAddress"}},
                "arena_length": {Name: "arena_length", Offset: 8, Size: 8, Align: 8, StorageOffset: 8, StorageSize: 8, Type: ir.Type{Name: "U64"}},
                "next_offset":  {Name: "next_offset", Offset: 16, Size: 8, Align: 8, StorageOffset: 16, StorageSize: 8, Type: ir.Type{Name: "U64"}},
            },
            FieldOrder: []string{"arena_base", "arena_length", "next_offset"},
        },
        "machine.x86_64.executor_memory.MutableBytes": {
            Name: "MutableBytes", Module: "machine.x86_64.executor_memory", Kind: ir.TypeKindData, Size: 16, Align: 8, StorageSize: 16,
            Fields: map[string]ir.FieldInfo{
                "address": {Name: "address", Offset: 0, Size: 8, Align: 8, StorageOffset: 0, StorageSize: 8, Type: ir.Type{Name: "PhysicalAddress"}},
                "length":  {Name: "length", Offset: 8, Size: 8, Align: 8, StorageOffset: 8, StorageSize: 8, Type: ir.Type{Name: "U64"}},
            },
            FieldOrder: []string{"address", "length"},
        },
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./compiler/codegen -run TestArenaReserveEmitsBoundsTrapAndBump -v`

Expected: FAIL because arena ops are not emitted.

- [ ] **Step 3: Emit frame begin**

Represent arenas in codegen this way:

```text
ExecutorMemory and ArenaFrame values are address-backed records with:
  arena_base   at +0
  arena_length at +8
  next_offset  at +16

FrameBegin result:
  stack slot containing an ArenaFrame record.

ArenaReserve result:
  stack slot containing MutableBytes { address, length }.

Frame save metadata:
  Emitter has map[*ir.FrameBegin]frameSave.
  frameSave stores Parent ir.Value and SavedOffsetLocal stack slot.
```

Update frame layout before emitting instructions:

```go
// needsObjectSlot:
case *ir.FrameBegin, *ir.ArenaReserve, *ir.ArenaPlace:
    return true

// valueSize:
case *ir.FrameBegin, *ir.ArenaReserve, *ir.ArenaPlace:
    return ctx.representationSize(v.Type.Name)
```

`FrameBegin` and `ArenaReserve` must also get normal `Frame.Slots` entries because their handles are passed to later operations. `ArenaPlace` gets an object slot only for compatibility with existing value maps; its bound address is the arena address returned by `emitArenaBump`.

Implement:

```go
type frameSave struct {
    Parent ir.Value
    SavedOffsetSlot int64
}

func (e *Emitter) emitFrameBegin(op *ir.FrameBegin) {
    // Load parent.next_offset into a scratch register and store it in a
    // compiler-created local slot keyed by op.
    // Write frame fields into op's stack slot.
    // Write parent.next_offset back through the parent record address.
}
```

Frame begin algorithm:

```text
saved = parent.next_offset
new_parent_next = saved + requested_length
if new_parent_next > parent.arena_length: call _wrela_memory_oom
frame.arena_base = parent.arena_base + saved
frame.arena_length = requested_length
frame.next_offset = 0
parent.next_offset = new_parent_next
remember saved for FrameEnd
```

- [ ] **Step 4: Emit reserve**

Reserve algorithm:

```text
aligned = (arena.next_offset + (align - 1)) & ~(align - 1)
end = aligned + length
if end > arena.arena_length: call _wrela_memory_oom
result.address = arena.arena_base + aligned
result.length = length
arena.next_offset = end
```

- [ ] **Step 5: Emit frame end**

Frame end algorithm:

```text
parent.next_offset = saved_parent_offset
```

- [ ] **Step 6: Emit memory trap**

Add a generated code symbol by appending a runtime compiled unit inside `Compile`, next to the existing generated interrupt/runtime data plumbing:

```go
func compileMemoryTrapUnit() compiledUnit {
    return compiledUnit{
        Symbol: "_wrela_memory_oom",
        Bytes: []byte{
            0xFA,       // cli
            0xF4,       // hlt
            0xEB, 0xFD, // jmp -3, back to hlt
        },
    }
}
```

Append this unit unconditionally so tests can assert the symbol exists even before a function uses it.

- [ ] **Step 7: Run verification**

Run:

```bash
go test ./compiler/codegen -run TestArenaReserveEmitsBoundsTrapAndBump -v
go test ./compiler/codegen -v
git diff --check
```

Expected: all tests PASS.

- [ ] **Step 8: Commit**

```bash
git add compiler/codegen/x64.go compiler/codegen/memory_test.go
git commit -m "feat: emit bounded arena reserve -Codex Automated"
```

**Acceptance Criteria:** Frame begin claims bounded memory; reserve aligns and bumps; OOM branches to `_wrela_memory_oom`; frame end rewinds the parent; `_wrela_memory_oom` is appended unconditionally as an intentional 4-byte runtime trap symbol even in images that do not currently reserve memory.

### Task 14: Codegen For Typed Place

**Files:**
- Modify: `compiler/codegen/x64.go`
- Modify: `compiler/codegen/memory_test.go`

**Purpose:** Place typed records/classes into arena storage and initialize fields in place.

- [ ] **Step 1: Write failing codegen test**

Add:

```go
func TestArenaPlaceWritesConstructedFields(t *testing.T) {
    program := testProgramWithArenaPlace(t)
    image, diags := Compile(program)
    if len(diags) != 0 {
        t.Fatalf("compile diagnostics: %#v", diags)
    }
    code := symbolBytes(t, image, "_wrela_method_test_Worker_run")
    if !containsBytes(code, []byte{0x39, 0x30, 0x00, 0x00}) {
        t.Fatalf("place must store Message.id immediate 12345 into arena storage: %x", code)
    }
    if !containsBytes(code, []byte{0x10, 0x00, 0x00, 0x00}) {
        t.Fatalf("place must reserve Message storage size 16: %x", code)
    }
}

func testProgramWithArenaPlace(t *testing.T) *ir.Program {
    t.Helper()
    program := testProgramWithArenaReserve(t)
    messageType := ir.Type{Name: "Message", Module: "test", Kind: ir.TypeKindData}
    program.Types["test.Message"] = ir.TypeInfo{
        Name: "Message", Module: "test", Kind: ir.TypeKindData, Size: 16, Align: 8, StorageSize: 16,
        Fields: map[string]ir.FieldInfo{
            "id": {Name: "id", Offset: 0, Size: 8, Align: 8, StorageOffset: 0, StorageSize: 8, Type: ir.Type{Name: "U64"}},
        },
        FieldOrder: []string{"id"},
    }
    id := &ir.ConstInt{Symbol: "message_id", Value: 12345, Type: ir.Type{Name: "U64"}}
    frame := program.Functions[0].Blocks[0].Ops[4].(*ir.FrameBegin)
    place := &ir.ArenaPlace{
        Arena: frame,
        Type:  messageType,
        Fields: []ir.FieldValue{{
            Name:  "id",
            Value: id,
        }},
    }
    ops := []ir.Operation{
        program.Functions[0].Blocks[0].Ops[0],
        program.Functions[0].Blocks[0].Ops[1],
        id,
        frame,
        place,
        &ir.FrameEnd{Frame: frame},
        &ir.Return{},
    }
    program.Functions[0].Blocks[0].Ops = ops
    return program
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./compiler/codegen -run TestArenaPlaceWritesConstructedFields -v`

Expected: FAIL because `ArenaPlace` is not emitted.

- [ ] **Step 3: Emit place**

Algorithm:

```text
size = layout.StorageSize(type)
align = layout.Align(type)
address = reserve(arena, size, align)
for each constructor field:
    emit field value into [address + field.StorageOffset]
result value is an address-backed record handle; existing field load/store code must use the bound arena address instead of assuming the value lives in a compiler stack slot.
```

Implementation note:

```go
// Add these helpers in this task. emitArenaBump centralizes the reserve
// algorithm from Task 13 and returns the physical address register for the
// start of the claimed span. bindValueAddress records that a value is backed
// by that address so existing field loads/stores use arena storage instead of
// a compiler stack slot.
func (e *Emitter) emitArenaBump(arena ir.Value, size uint64, align uint64) asm.Reg { ... }
func (e *Emitter) bindValueAddress(value ir.Value, address asm.Reg) { ... }

func (e *Emitter) emitArenaPlace(op *ir.ArenaPlace) {
    layout := e.program.Types[op.Type.Module+"."+op.Type.Name]
    addr := e.emitArenaBump(op.Arena, uint64(layout.StorageSize), uint64(layout.Align))
    for _, field := range op.Fields {
        info := layout.Fields[field.Name]
        e.emitStoreValueAt(addr, int64(info.StorageOffset), field.Value, info.Type)
    }
    e.bindValueAddress(op, addr)
}
```

- [ ] **Step 4: Run verification**

Run:

```bash
go test ./compiler/codegen -run TestArenaPlaceWritesConstructedFields -v
go test ./compiler/codegen -v
git diff --check
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add compiler/codegen/x64.go compiler/codegen/memory_test.go
git commit -m "feat: emit typed arena place -Codex Automated"
```

**Acceptance Criteria:** `place` uses record layout for size, alignment, and field offsets; placed values are initialized in arena storage.

### Task 15: Preserve 2 MiB Identity Paging As Target Glue

**Files:**
- Modify: `wrela/platform/uefi/types.wrela`
- Modify: `wrela/platform/uefi/transition.wrela`
- Modify: `compiler/codegen/uefi_source_codegen_test.go`

**Purpose:** Keep x86_64 long-mode paging honest while removing source-level virtual-memory planning from examples.

- [ ] **Step 1: Write assertion for 2 MiB page-size bit**

Add to `compiler/codegen/uefi_source_codegen_test.go`:

```go
func TestIdentityPagingUsesTwoMiBPagesAsTargetGlue(t *testing.T) {
    checked := parseCheckedUEFIModules(t)
    method := asmMethodFromSem(t, checked, "platform.uefi.types", "DelegatedMemory", "build_identity_paging")
    if !strings.Contains(method.Body, "add rax, 0x83") {
        t.Fatalf("identity paging must add P|RW|PS flags to each PDE:\n%s", method.Body)
    }
    if !strings.Contains(method.Body, "add r13, 0x200000") || !strings.Contains(method.Body, "add r15, 0x200000") {
        t.Fatalf("identity paging must advance mappings in 2 MiB increments:\n%s", method.Body)
    }
}
```

- [ ] **Step 2: Run test**

Run: `go test ./compiler/codegen -run TestIdentityPagingUsesTwoMiBPagesAsTargetGlue -v`

Expected: PASS on current source. If it fails, inspect existing `build_identity_paging`; the implementation must still set `PS`.

- [ ] **Step 3: Rename source-level plan names**

Remove `VirtualMemoryPlan` from `wrela/machine/x86_64/cpu_state.wrela`. Replace any owned-hardware phase parameter named `virtual_memory_plan` with no parameter. Keep `build_identity_paging` inside `DelegatedMemory` and call it directly from `exit_to_owned_hardware`.

The transition call shape becomes:

```wrela
let pml4 = self.delegated_memory.build_identity_paging(memory_map = final_map)
self.activate_owned_hardware(
    owned_stack_top = cpu_plan.owned_stack_top,
    cr3_value = pml4,
    gdt_base = gdt.address,
    idt_base = idt.address
)
```

- [ ] **Step 4: Run verification**

Run:

```bash
go test ./compiler/sem -run UefiSource -v
go test ./compiler/codegen -run 'IdentityPaging|UefiSource' -v
rg -n "VirtualMemoryPlan|virtual_memory_plan" wrela examples
git diff --check
```

Expected: Go tests PASS; `rg` reports no matches in Wrela source or examples.

- [ ] **Step 5: Commit**

```bash
git add wrela/platform/uefi/types.wrela wrela/platform/uefi/transition.wrela wrela/machine/x86_64/cpu_state.wrela compiler/codegen/uefi_source_codegen_test.go
git commit -m "feat: keep identity paging as target glue -Codex Automated"
```

**Acceptance Criteria:** Source-level `VirtualMemoryPlan` is gone; `build_identity_paging` remains platform target glue; tests assert 2 MiB page flags.

---

## 9. Phase 5: Cache Regions With Default Eviction

### Task 16: CacheArena Source Surface

**Files:**
- Create/modify: `wrela/machine/x86_64/cache_memory.wrela`
- Modify: `compiler/sem/uefi_source_shape_test.go`
- Modify: `compiler/codegen/uefi_source_codegen_test.go`

**Purpose:** Add an explicit bounded cache memory personality separate from root and frame arenas.

`CacheArena` owns a bounded `MutableBytes` region and exposes fixed-slot FIFO behavior. Its slot layout is:

```text
valid: U64 at +0
key: U64 at +8
length: U64 at +16
payload bytes at +24
```

The source surface is:

```wrela
module machine.x86_64.cache_memory

use { ArenaFrame, Bytes, MutableBytes } from machine.x86_64.executor_memory

data CacheLookup { hit: Bool bytes: Bytes }
data CachePutResult { stored: Bool evicted: U64 }

class CacheArena {
    storage: MutableBytes
    slot_count: U64
    slot_size: U64
    next_victim: U64
    initialized: U64

    asm fn clear(self)
    asm fn put_bytes(self, key: U64, bytes: Bytes) -> CachePutResult
    asm fn get_bytes(self, key: U64, into: ArenaFrame) -> CacheLookup
}
```

`initialized` prevents fresh or uninitialized cache memory from false-hitting. `clear`, `put_bytes`, and `get_bytes` all establish valid-bit metadata before lookup can succeed.

**Acceptance Criteria:** Cache memory has explicit source types; cache storage is a bounded `MutableBytes`; lookup returns copied `Bytes`, not stable cache references; empty or uninitialized slots cannot hit.

### Task 17: Semantic Rules For Cache Lookup And Put

**Files:**
- Modify: `compiler/sem/memory.go`
- Modify: `compiler/sem/check.go`
- Modify: `compiler/sem/memory_test.go`
- Modify: `compiler/sem/memory_negative_test.go`

**Purpose:** Make cache lookup results carry the destination frame lifetime and reject stable references to cache-backed bytes.

Rules:
- `CacheArena.get_bytes(key, into)` requires `into` to be an `ArenaFrame`.
- Named and positional calls use the same argument mapping as normal method calls.
- The returned `CacheLookup` and its `.bytes` field carry the `into` frame lifetime.
- A cache lookup result or its bytes cannot be stored into executor/root state or a longer-lived frame.
- `put_bytes` accepts `Bytes` but does not preserve a stable reference to cache storage.

**Acceptance Criteria:** Cache lookup destination must be `ArenaFrame`; named and positional `get_bytes` calls both map the `into` argument correctly; cache lookup bytes carry the destination frame lifetime; stable cache references are impossible in source.

### Task 18: Fixed-Slot FIFO Cache Assembly

**Files:**
- Modify: `wrela/machine/x86_64/cache_memory.wrela`
- Modify: `compiler/asm/*` as needed for unsigned branches
- Modify: `compiler/codegen/uefi_source_codegen_test.go`
- Modify: `tests/e2e/fixtures/cache_memory/main.wrela`
- Modify: `tests/e2e/hello_qemu_test.go`

**Purpose:** Implement the fixed-slot cache and prove it under QEMU.

Assembly behavior:
- `clear()` validates that `storage.length` covers `slot_count * (slot_size + 24)`, zeroes valid/key/length metadata for every slot, resets `next_victim`, and marks the cache initialized. A zero-slot cache clears without touching storage.
- `put_bytes` validates the same storage span, returns `stored = false` for zero-slot, undersized, or oversize values, lazily initializes the cache if needed, writes the FIFO victim slot, and reports `evicted = 1` only when the victim slot was already valid.
- `get_bytes` validates the same storage span, returns a miss for zero-slot or undersized caches, lazily initializes the cache if needed, requires `valid == 1` before comparing keys, copies hits into the provided frame, writes a nested `Bytes` handle in the return slot, and returns a zero-length miss handle on miss.
- Frame reservation in `get_bytes` checks unsigned overflow for alignment, length addition, arena capacity, and base-plus-offset before writing.

Verification:

```bash
go test ./compiler/codegen -run TestCacheArena -v
go test ./tests/e2e -run TestCacheMemoryQEMU -v
git diff --check
```

**Acceptance Criteria:** Cache put never calls memory OOM for normal full-cache insertion; it evicts the FIFO victim; cache get copies into the provided frame; empty cache lookup for key 0 does not hit; zero-length values round-trip; stale evicted entries are not returned; zero-slot and undersized cache regions do not write past backing storage.

---

## 10. Phase 6: Integration

### Task 19: Rewrite Hello Example To Use Frames And Place

**Files:**
- Modify: `examples/hello/main.wrela`
- Modify: `examples/hello/program.wrela`
- Modify: `compiler/integration_test.go`

**Purpose:** Prove the current example uses the new memory vocabulary end to end.

- [ ] **Step 1: Write failing integration assertion**

Add to `compiler/integration_test.go`:

```go
func TestHelloSourceUsesArenaFrames(t *testing.T) {
    source := readRepoFile(t, "examples/hello/program.wrela")
    for _, want := range []string{
        "with self.memory.frame(length =",
        ".place(",
        ".bytes(",
    } {
        if !strings.Contains(source, want) {
            t.Fatalf("hello program missing %q", want)
        }
    }
    if strings.Contains(source, "allocate_bytes") || strings.Contains(source, "static_bytes") {
        t.Fatalf("hello program must not use old memory vocabulary")
    }
}
```

Add this helper in the same file if it does not already exist:

```go
func readRepoFile(t *testing.T, path string) string {
    t.Helper()
    wd, err := os.Getwd()
    if err != nil {
        t.Fatalf("getwd: %v", err)
    }
    root := filepath.Clean(filepath.Join(wd, ".."))
    data, err := os.ReadFile(filepath.Join(root, path))
    if err != nil {
        t.Fatalf("read %s: %v", path, err)
    }
    return string(data)
}
```

Add `os` and `path/filepath` to the test imports.

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./compiler -run TestHelloSourceUsesArenaFrames -v`

Expected: FAIL because hello still uses old vocabulary.

- [ ] **Step 3: Rewrite program**

Use this pattern in `examples/hello/program.wrela`:

```wrela
use { Bytes, ExecutorMemory } from machine.x86_64.executor_memory

data RunScratch {
    hello: Bytes
}

start fn run(self) -> never {
    with self.memory.frame(length = 4096) as tick {
        let scratch = tick.place(RunScratch(hello = self.memory.bytes(value = "hello from wrela\n")))
        self.serial_path.write(scratch.hello)
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

Also replace interrupt-handler `self.memory.static_bytes(...)` calls with `self.memory.bytes(value = ...)`. Do not use `place` or `reserve` in interrupt handlers; Task 8 makes those calls illegal in on-handlers.

- [ ] **Step 4: Rewrite main arena setup**

In `examples/hello/main.wrela`, create executor memory with physical addresses directly inside the image `delegated_hardware` phase. Do not move this construction into a helper method; Task 21 rejects raw `MutableBytes(address = literal, ...)` authority creation outside that direct phase body.

```wrela
let arena = MutableBytes(address = 0x200000, length = 0x200000)
let executor_memory = ExecutorMemory(
    arena_base = arena.address,
    arena_length = arena.length,
    next_offset = 0
)
```

Remove `VirtualMemoryPlan` construction.

- [ ] **Step 5: Run verification**

Run:

```bash
go test ./compiler -run TestHelloSourceUsesArenaFrames -v
go test ./compiler -run Integration -v
rg -n "allocate_bytes|static_bytes|VirtualMemoryPlan" examples wrela
git diff --check
```

Expected: tests PASS; `rg` reports no matches.

- [ ] **Step 6: Commit**

```bash
git add examples/hello/main.wrela examples/hello/program.wrela compiler/integration_test.go
git commit -m "feat: use arena frames in hello example -Codex Automated"
```

**Acceptance Criteria:** Hello compiles using `frame`, `place`, and `bytes`; old vocabulary is gone from examples.

### Task 20: End-To-End QEMU Verification

**Files:**
- Modify: `tests/e2e/hello_qemu_test.go`

**Purpose:** Prove direct physical arena memory still boots and handles interrupt examples.

- [ ] **Step 1: Add expected serial output assertion**

Ensure `tests/e2e/hello_qemu_test.go` still expects:

```text
hello from wrela
serial interrupt:
msi interrupt
msix interrupt
```

Do not change the success text to mention arenas.

- [ ] **Step 2: Run e2e test**

Run: `go test ./tests/e2e -run Hello -v`

Expected: PASS on machines with QEMU and OVMF. If QEMU is unavailable, record the exact missing binary or firmware error in the task notes and continue only after unit tests pass.

- [ ] **Step 3: Run full Go tests**

Run:

```bash
go test ./...
git diff --check
```

Expected: all available tests PASS; `git diff --check` prints nothing.

- [ ] **Step 4: Commit**

```bash
git add tests/e2e/hello_qemu_test.go
git commit -m "test: verify physical arena hello boot -Codex Automated"
```

**Acceptance Criteria:** The generated image still boots under QEMU; serial/interrupt behavior is preserved; the memory model rewrite does not regress executable output.

### Task 21: Static Raw-Address Asm Guard

**Files:**
- Modify: `compiler/sem/check.go`
- Modify: `compiler/sem/memory.go`
- Modify: `compiler/sem/memory_test.go`
- Create: `tests/fixtures/negative/user_raw_memory_asm.wrela`
- Create: `tests/fixtures/negative/user_raw_memory_authority.wrela`
- Modify: `tests/fixtures/negative/illegal_asm_placement.wrela`

**Purpose:** Ensure static enforcement is not bypassed by user-module asm or raw physical byte authority construction.

- [ ] **Step 1: Add negative fixture**

Create `tests/fixtures/negative/user_raw_memory_asm.wrela`:

```wrela
// expect: SEM0032: asm raw memory access requires edge-capability module
module negative.user_raw_memory_asm

class Bad {
    asm fn write_raw(self) {
        mov rax, 0x200000
        mov [rax], rax
    }
}
```

Create `tests/fixtures/negative/user_raw_memory_authority.wrela`:

```wrela
// expect: SEM0028: raw physical byte authority can only be created directly
module machine.x86_64.executor_memory

data MutableBytes { address: PhysicalAddress length: U64 }

class Bad {
    fn mint(self) -> MutableBytes {
        return MutableBytes(address = 0x200000, length = 4096)
    }
}
```

Update the expectation in `tests/fixtures/negative/illegal_asm_placement.wrela`:

```text
// expect: SEM0032: asm raw memory access requires edge-capability module
```

- [ ] **Step 2: Add tests**

Add:

```go
func TestUserRawMemoryAuthorityRejected(t *testing.T) {
    cases := []struct {
        fixture string
        code    string
    }{
        {"user_raw_memory_asm.wrela", diag.SEM0032},
        {"user_raw_memory_authority.wrela", diag.SEM0028},
    }
    for _, tc := range cases {
        t.Run(tc.fixture, func(t *testing.T) {
            modules := parseFixtureModulesForTest(t, filepath.Join("tests", "fixtures", "negative", tc.fixture))
            index, ds := BuildIndex(modules)
            ds = filterMissingImageDiagnostic(ds)
            if len(ds) != 0 {
                t.Fatalf("index diagnostics: %#v", ds)
            }
            _, diags := Check(index, modules)
            if !hasCode(diags, tc.code) {
                t.Fatalf("expected %s, got %#v", tc.code, diags)
            }
        })
    }
}
```

Use the `parseFixtureModulesForTest` helper from Task 9. Task 21 depends on Task 9 specifically so this helper already exists; do not create a duplicate helper.

- [ ] **Step 3: Run test to verify failure**

Run: `go test ./compiler/sem -run TestUserRawMemoryAuthorityRejected -v`

Expected: FAIL because current asm rejection uses SEM0012 and raw `MutableBytes` construction is not guarded.

- [ ] **Step 4: Emit memory-specific diagnostics**

In `checkMethods`, when `method.IsAsm` and `!isAsmAllowedHere(typ)`, emit:

```go
c.error(method.SpanV, diag.SEM0032, "asm raw memory access requires edge-capability module")
```

Keep existing asm-allowed behavior for `arch.*`, `platform.*`, and `machine.x86_64.*`. This intentionally replaces SEM0012 for illegal asm placement; update the existing negative fixture expectation in this task.

In `checkConstructorPermissions`, reject raw physical byte authorities before the generic `KindData` allowance. This is deliberately stricter than "any code while the current phase is delegated": helper methods, executor methods, and user modules cannot mint byte authority.

```go
if typ.Module == "machine.x86_64.executor_memory" && typ.Name == "MutableBytes" {
    if ctx != ContextImagePhaseDirect || c.currentPhase != "delegated_hardware" || !constructorArgsAreIntegerLiterals(expr, "address", "length") {
        c.error(expr.SpanV, diag.SEM0028, "raw physical byte authority can only be created directly in delegated_hardware phase")
        return
    }
}
```

Add the helper in `memory.go`:

```go
func constructorArgsAreIntegerLiterals(expr *ast.ConstructorExpr, names ...string) bool {
    wanted := map[string]bool{}
    for _, name := range names {
        wanted[name] = false
    }
    for _, arg := range expr.Args {
        if _, ok := wanted[arg.Name]; !ok {
            continue
        }
        if _, ok := arg.Value.(*ast.IntLiteral); !ok {
            return false
        }
        wanted[arg.Name] = true
    }
    for _, seen := range wanted {
        if !seen {
            return false
        }
    }
    return true
}
```

This keeps the current `examples/hello/main.wrela` boot arena legal while rejecting helper-method and user-module hidden authority creation.

Hex literals such as `0x200000` tokenize and parse as `*ast.IntLiteral`, so the current hello arena construction remains covered by `constructorArgsAreIntegerLiterals`.

- [ ] **Step 5: Run verification**

Run:

```bash
go test ./compiler/sem -run TestUserRawMemoryAuthorityRejected -v
go test ./compiler/sem -v
go test ./compiler -run NegativeFixtures -v
git diff --check
```

Expected: all tests PASS.

- [ ] **Step 6: Commit**

```bash
git add compiler/sem/check.go compiler/sem/memory.go compiler/sem/memory_test.go tests/fixtures/negative/user_raw_memory_asm.wrela tests/fixtures/negative/user_raw_memory_authority.wrela tests/fixtures/negative/illegal_asm_placement.wrela
git commit -m "fix: guard raw memory asm authority -Codex Automated"
```

**Acceptance Criteria:** User modules cannot introduce asm as a raw memory escape hatch; edge-capability modules remain allowed; raw `MutableBytes(address = literal, ...)` construction is legal only in the direct `delegated_hardware` image phase.

---

## 11. Phase 7: Final Documentation And Global Checks

### Task 22: Add Memory Model Notes To Design Docs

**Files:**
- Modify: `docs/design/hello_world_design_doc.md`
- Modify: `docs/production-deferred-work.md`

**Purpose:** Keep design docs aligned with the implemented physical arena model.

- [ ] **Step 1: Update design doc language**

In `docs/design/hello_world_design_doc.md`, replace memory-planning language that implies virtual memory is the source-level model with:

```markdown
Wrela memory is physical-region authority first. Executor memory is a durable root arena. Temporary work is expressed with bounded `with` frames that claim child slices and rewind at block exit. Typed values are placed into arena storage with `place`; raw spans are reserved with `reserve`; cache memory is bounded and evicts by default. The x86_64 backend may emit identity paging because long mode requires it, but source-level Wrela code does not model virtual address spaces in this stage.
```

- [ ] **Step 2: Run doc verification**

Run:

```bash
rg -n 'physical-region authority|bounded `with` frames|evicts by default' docs/design/hello_world_design_doc.md docs/production-deferred-work.md
git diff --check
```

Expected: `rg` prints matching lines from both docs; `git diff --check` prints nothing.

- [ ] **Step 3: Commit**

```bash
git add docs/design/hello_world_design_doc.md docs/production-deferred-work.md
git commit -m "docs: describe physical arena memory model -Codex Automated"
```

**Acceptance Criteria:** Design docs explain direct physical arena memory, `with` frames, `place`, bounded caches, and identity paging as target glue.

### Task 23: Full Plan Acceptance Sweep

**Files:**
- No source edits expected.

**Purpose:** Confirm every phase composes.

- [ ] **Step 1: Run package tests**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 2: Run executable hello test**

Run:

```bash
go test ./tests/e2e -run Hello -v
```

Expected: PASS where QEMU/OVMF are available. If unavailable, save the exact missing dependency output in the PR body.

- [ ] **Step 3: Run source vocabulary scan**

Run:

```bash
rg -n "allocate_bytes|static_bytes|VirtualMemoryPlan|virtual_memory_plan" wrela examples compiler/sem compiler/ir compiler/codegen
```

Expected: no matches.

- [ ] **Step 4: Run paging-source scan**

Run:

```bash
rg -n "build_identity_paging|mov cr3|add rax, 0x83|0x200000" wrela/platform/uefi docs compiler/codegen/uefi_source_codegen_test.go
```

Expected: matches exist only in platform transition/source text, docs, and UEFI source-codegen tests that describe x86_64 target glue. Do not scan all of `compiler/codegen` for bare `0x83`; generic x64 instruction tests and encoders also contain that byte.

- [ ] **Step 5: Run diff hygiene**

Run:

```bash
git diff --check
```

Expected: no output.

- [ ] **Step 6: Commit acceptance notes if any docs changed**

If this task only runs commands, no commit is required. If documentation or test harness notes were updated, commit:

```bash
git add <changed-files>
git commit -m "chore: verify physical arena memory model -Codex Automated"
```

**Acceptance Criteria:** All unit tests pass; QEMU hello passes where available; old memory vocabulary is absent; identity paging remains only as x86_64 boot glue.

---

## 12. Appendix A: Exact Semantic Rules

Frame expression rules:

```text
with <expr> as <name> { <body> }
```

- `<expr>` must be a method call named `frame`.
- The receiver must have memory kind `ExecutorMemory` or `ArenaFrame`.
- The call must have exactly one named argument: `length`.
- `length` must typecheck as `U64`.
- The call type must be `ArenaFrame`.
- `<name>` is defined only inside `<body>`.

Lifetime rules:

```text
Static lifetime:
  string literals and immutable compiler data.

Executor root lifetime:
  values placed in ExecutorMemory outside a frame.

Frame lifetime:
  values placed or reserved in an ArenaFrame.

Cache copy lifetime:
  bytes copied from CacheArena into an ArenaFrame; treated as that frame lifetime.

Cache lookup lifetime:
  the temporary CacheLookup value returned by get_bytes; only its .bytes field becomes a cache copy lifetime.
```

Escape rejection matrix:

```text
source value lifetime   target location                 result
static                  any                              allowed
executor root           executor root                    allowed
executor root           frame                            allowed
frame N                 same frame N local               allowed
frame N                 nested child frame               allowed for reads
frame N                 parent frame/root/self field     SEM0025
frame N                 method return crossing frame     SEM0024
frame N                 executor/driver/path field       SEM0025
cache lookup frame N    .bytes field access              yields cache copy frame N
cache lookup frame N    any longer-lived target          SEM0031
cache copy frame N      any longer-lived target          SEM0031
```

Arena intrinsic rules:

```wrela
let value = arena.place(T(...))
let bytes = arena.reserve(length = n, align = a)
```

- `arena` must be `ExecutorMemory` or `ArenaFrame`.
- `place` requires one positional constructor expression.
- `place` returns the constructed type with the receiver arena's lifetime.
- `reserve` requires `length: U64` and `align: U64`.
- `reserve` returns `MutableBytes` with the receiver arena's lifetime.

OOM rules:

```text
ExecutorMemory.place/reserve OOM:
  jump to _wrela_memory_oom.

ArenaFrame.place/reserve OOM:
  jump to _wrela_memory_oom.

CacheArena.put_bytes when full:
  evict next FIFO slot and return stored = true.

CacheArena.put_bytes when bytes.length > slot_size:
  return stored = false, evicted = 0.
```

---

## 13. Appendix B: Exact Runtime Algorithms

Frame begin:

```go
func frameBegin(parent *ArenaHeader, length uint64) ArenaHeader {
    saved := parent.nextOffset
    end := saved + length
    if end < saved || end > parent.length {
        memoryOOM()
    }
    child := ArenaHeader{
        base:       parent.base + saved,
        length:     length,
        nextOffset: 0,
    }
    parent.nextOffset = end
    rememberSavedOffset(child, parent, saved)
    return child
}
```

Frame end:

```go
func frameEnd(parent *ArenaHeader, saved uint64) {
    parent.nextOffset = saved
}
```

Reserve:

```go
func reserve(arena *ArenaHeader, length, align uint64) MutableBytes {
    mask := align - 1
    aligned := (arena.nextOffset + mask) & ^mask
    end := aligned + length
    if end < aligned || end > arena.length {
        memoryOOM()
    }
    arena.nextOffset = end
    return MutableBytes{address: arena.base + aligned, length: length}
}
```

Place:

```go
func place[T any](arena *ArenaHeader, value T, size, align uint64) *T {
    bytes := reserve(arena, size, align)
    writeValue(bytes.address, value)
    return (*T)(bytes.address)
}
```

Cache put:

```go
func cacheStorageFits(cache *CacheArena) bool {
    if cache.slotCount == 0 {
        return false
    }
    entrySize := cache.slotSize + 24
    if entrySize < cache.slotSize {
        return false
    }
    required := uint64(0)
    for slot := uint64(0); slot < cache.slotCount; slot++ {
        next := required + entrySize
        if next < required {
            return false
        }
        required = next
    }
    return required <= cache.storage.length
}

func cachePut(cache *CacheArena, key uint64, bytes Bytes) CachePutResult {
    if !cacheStorageFits(cache) {
        return CachePutResult{stored: false, evicted: 0}
    }
    if cache.initialized != 1 {
        clearCache(cache)
    }
    if bytes.length > cache.slotSize {
        return CachePutResult{stored: false, evicted: 0}
    }
    slot := cache.nextVictim
    if slot >= cache.slotCount {
        slot = 0
    }
    storageAddress := cache.storage.address // source-level view; asm first loads the MutableBytes handle, then its address field.
    address := storageAddress + slot*(cache.slotSize+24)
    evicted := loadU64(address+0) == 1
    storeU64(address+0, 1)
    storeU64(address+8, key)
    storeU64(address+16, bytes.length)
    copyBytes(address+24, bytes.address, bytes.length)
    cache.nextVictim = (cache.nextVictim + 1) % cache.slotCount
    if evicted {
        return CachePutResult{stored: true, evicted: 1}
    }
    return CachePutResult{stored: true, evicted: 0}
}

func cacheGet(cache *CacheArena, key uint64, into *ArenaFrame) CacheLookup {
    if !cacheStorageFits(cache) {
        return CacheLookup{hit: false, bytes: Bytes{address: 0, length: 0}}
    }
    if cache.initialized != 1 {
        clearCache(cache)
    }
    storageAddress := cache.storage.address // source-level view; asm first loads the MutableBytes handle, then its address field.
    for slot := uint64(0); slot < cache.slotCount; slot++ {
        address := storageAddress + slot*(cache.slotSize+24)
        if loadU64(address) == 1 && loadU64(address+8) == key {
            length := loadU64(address + 16)
            out := reserve(into, length, 8)
            copyBytes(out.address, address+24, length)
            return CacheLookup{hit: true, bytes: Bytes{address: out.address, length: length}}
        }
    }
    return CacheLookup{hit: false, bytes: Bytes{address: 0, length: 0}}
}
```

---

## 14. Appendix C: Global Acceptance Criteria

- `with` is parsed and represented in AST.
- `ExecutorMemory`, `ArenaFrame`, `Bytes`, and `MutableBytes` use physical-address source contracts.
- `allocate_bytes` and `static_bytes` are removed from active Wrela source.
- `VirtualMemoryPlan` is removed from active Wrela source.
- x86_64 identity paging remains as platform glue and uses 2 MiB page entries.
- `place` and `reserve` are compiler-recognized arena intrinsics.
- Frame values cannot escape their frame.
- Helper methods can receive frame arenas and return values tied to the caller's frame lifetime.
- Bounded frame claim, frame rewind, reserve, and typed place emit executable code.
- OOM in root/frame memory reaches `_wrela_memory_oom`.
- Cache insertion evicts by default.
- Cache lookup copies into a frame.
- Cache entries cannot be used as stable references.
- Hello still boots under QEMU and prints the expected serial output.
- Documentation says Wrela's memory model is direct physical region authority with hierarchical arenas.
