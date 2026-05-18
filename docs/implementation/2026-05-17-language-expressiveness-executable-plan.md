# Language Expressiveness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Wrela's language expressiveness milestone: generics, static traits, enums, pattern matching, constants, typed memory views, typed arena array reservations, and platform-library migrations that remove current topic/result/queue duplication.

**Architecture:** The implementation keeps Wrela static and monomorphized. The parser records structured type expressions, the semantic index records generic declarations plus concrete instantiations, monomorphization produces deterministic concrete layouts before IR lowering, traits resolve to direct concrete method calls, enums lower to explicit discriminant/payload operations, and typed arena reservations reuse the existing arena bump path while preserving hidden lifetime rules. Existing concrete topic/result/memory code is migrated to generic `Topic<T>`, `TopicSubscription<T>`, `Option<T>`, `Result<T, E>`, `Slots<T>`, `Slice<T>`, and `MutableSlice<T>` where that removes duplication without adding dynamic behavior.

**Tech Stack:** Go 1.22+; existing hand-written lexer/parser; existing semantic checker and source graph; existing `layout`, `ir`, and direct x86_64 codegen packages; Wrela source modules under `wrela/`; Go unit tests, negative Wrela fixtures, integration tests, and QEMU e2e tests.

---

## 0. How To Execute This Plan

This plan is written so a junior engineer can pick up any task after its prerequisites land. Do not reinterpret the syntax, type names, diagnostics, or migration target names during implementation.

Definition of done for a task:

- All checkbox steps in the task are complete.
- The task's failing test fails before the implementation step unless the repository already contains equivalent behavior.
- The task's verification commands pass after implementation.
- `git diff --check` passes.
- A commit is created with the exact message shown in the task, ending in `-Codex Automated`.

Definition of done for the full milestone:

- `go test ./...` passes.
- `go test ./tests/e2e -run 'Hello|ProductionSubstrate' -v` passes on a machine with QEMU and OVMF available.
- `rg -n "TimerTickNext|SerialRxNext|EduInterruptNext|IvshmemDoorbellNext|U64TopicNext|U64PublishResult" wrela examples tests compiler` returns no source-level compatibility uses except tests explicitly named `compat` or comments explaining removed aliases.
- `rg -n "\\b(has_message|published|full)\\b" wrela examples tests/e2e/fixtures` returns no matches in migrated runtime source. Result state must be expressed with enums and matches.
- `rg -n "\\b(Topic|Option|Result|Slots|MutableSlice|Slice)<|\\breserve_array\\b|\\bmatch\\b|\\bif let\\b" wrela examples tests/e2e/fixtures` shows real platform/example usage.
- `docs/design/2026-05-17-language-expressiveness-design.md` remains unchanged; this implementation plan is the execution artifact derived from it.

Do not run `go test ./...` between Tasks 4 and 19 unless the task explicitly says to. During the migration, parser, semantic, IR, and source-library compatibility will intentionally be partial. Each task lists the narrower command that must pass.

---

## 1. Frozen Language Decisions

Do not reopen these decisions during task execution.

- Generic declarations use angle brackets: `data Slice<T>`, `class Topic<T>`, `enum Result<T, E>`, `trait Subscription<T>`, `fn clone_into<U>(...)`.
- Generic arguments are concrete, sized, layout-bearing type arguments unless a task explicitly says a type parameter is used only in a trait-only position.
- There is no type inference for generic type names in type positions. `Topic` where `Topic<T>` is required is a semantic error.
- `data` and `class` constructors are source-visible when their fields are source-visible. Generic constructors use the same field constructor rule with a generic `TypeRef`, for example `Topic<TimerTickPayload>(identity = id, id = 3, depth = 64)`.
- Enum variant constructors use `Enum.Variant(...)` syntax, not `Enum<T>.Variant(...)`. Generic type arguments are inferred from the expected return type or from payload field types. If both sources are insufficient or disagree, the checker emits SEM0079.
- `match` and `if let` are statements in this milestone. They do not yield expression values.
- `match` arms use `Pattern => { statements }`. A wildcard arm is `_ => { ... }`.
- `if let Pattern = expr { statements }` is intentionally non-exhaustive and has no `else` form in this milestone.
- Trait declarations contain method signatures only. `impl Publisher<T> for TopicPublisher<T>` does not contain method bodies; it states that the implemented type's existing methods satisfy the trait.
- Traits are compile-time only. There are no trait objects, vtables, dynamic dispatch, runtime reflection, or hidden heap allocation.
- Monomorphization happens before layout and IR lowering. IR and codegen see concrete type names.
- Concrete instantiation display strings are source-shaped, for example `Topic<TimerTickPayload>`. Fully qualified semantic keys are `module.Type[module.Arg]`, for example `machine.x86_64.topic.Topic[machine.x86_64.topic_payload.TimerTickPayload]`.
- IR type-info keys use the fully qualified semantic key. Method and function symbols use the owner module plus the deterministic display mangle from `Type.MangledName()`, for example `_wrela_method_ir_generics_Holder_Event_read`.
- `Unit`, `Option<T>`, `Result<T, E>`, `Publisher<T>`, and `Subscription<T>` live in a new explicit module `wrela.lang.core`. There is no implicit prelude in this milestone.
- `Slice<T>`, `MutableSlice<T>`, `Slots<T>`, `FixedBuffer<T>`, and `Ring<T>` live in `machine.x86_64.executor_memory`.
- `Bytes` and `MutableBytes` stay as ABI-facing byte views. They are not aliases for `Slice<U8>` or `MutableSlice<U8>`.
- `reserve_array(Type, count = n)` is a compiler-recognized arena intrinsic on `ExecutorMemory` and `ArenaFrame`. The first positional argument is a type operand; it is not a runtime value.
- `reserve_array` returns `Slots<T>`, does not initialize the memory, and carries the receiver arena/frame hidden lifetime.
- `Slots<T>` exposes `capacity`, `write`, and `fill`. It does not expose readable `get`; readable views are `Slice<T>`, `MutableSlice<T>`, or containers such as `FixedBuffer<T>` and `Ring<T>`.
- `Slots<T>.address` is a protected field. User modules cannot read it directly; trusted platform modules can use it to implement containers and codegen intrinsics.
- `Slots<T>.write`, `Slots<T>.fill`, `Slice<T>.get`, `MutableSlice<T>.get`, and `MutableSlice<T>.set` are compiler intrinsics despite having source-visible signatures. IR lowering must produce `SlotWrite`, fill-loop, `SliceGet`, or `SliceSet`; x86_64 codegen must not emit the source stub body for these methods.
- Compiler intrinsics are recognized by fully qualified owner type plus method name, never by inspecting arbitrary asm text. The intrinsic set for this milestone is `machine.x86_64.executor_memory.Slots<T>.write`, `machine.x86_64.executor_memory.Slots<T>.fill`, `machine.x86_64.executor_memory.Slice<T>.get`, `machine.x86_64.executor_memory.MutableSlice<T>.get`, `machine.x86_64.executor_memory.MutableSlice<T>.set`, `machine.x86_64.topic.TopicPublisher<T>.publish`, `machine.x86_64.topic.ReliablePublisher<T>.try_publish`, `machine.x86_64.topic.TopicSubscription<T>.try_next`, `machine.x86_64.topic.ReliableSubscription<T>.try_next`, and `arm_wait` / `is_wait_armed` on both subscription types.
- Bounds checks for `Slots.write`, `Slice.get`, `MutableSlice.get`, and `MutableSlice.set` are required semantically. Optimization is deferred.
- Region-kind views are protected authority views. Ordinary modules cannot construct `Mmio<T>`, `FirmwareSlice<T>`, `Volatile<T>`, `DmaBuffer<T>`, `Slots<T>`, `Slice<T>`, or `MutableSlice<T>` from raw integers.
- Trusted authority modules are modules whose name starts with `platform.hardware.`, `platform.uefi.`, `platform.acpi.`, or `machine.x86_64.`. These modules may construct protected views only when the constructor arguments originate from existing authority values or compiler intrinsics.
- Existing `with`, `place`, `reserve`, frame lifetime, and raw-address asm guards remain in force. New generic memory views must flow through the same hidden lifetime system.
- Static constants are module-scoped declarations: `const NAME: U64 = expr`. Constants may reference earlier constants from the same module and imported constants.
- `static_assert(expr, message = "text")` is a module-scoped declaration. The message argument is required and must be a string literal.
- Const evaluation uses checked unsigned integer arithmetic for `U64` values. Overflow is a compile-time diagnostic.
- `sizeof(Type)` and `alignof(Type)` are compile-time expressions only.
- This milestone does not add associated types, higher-kinded types, generic specialization, operator overloading, bracket indexing, source-visible virtual address spaces, a general heap, growable arrays, or module-scope functions.

---

## 2. Repository Layout And File Responsibilities

Create or modify exactly these files unless a task explicitly narrows the list.

```text
compiler/diag/codes.go
  Adds language-expressiveness diagnostics SEM0076-SEM0096.

compiler/lex/token.go
compiler/lex/lexer_test.go
  Adds enum, trait, impl, for, where, const, static_assert, match, alignof, sizeof keywords and the `=>` fat-arrow token.

compiler/ast/ast.go
compiler/ast/ast_test.go
  Adds TypeRef, generic declaration metadata, enum/trait/impl/const/static_assert decls, pattern AST, match/if-let statements, and deterministic debug helpers.

compiler/parse/parser.go
compiler/parse/expr.go
compiler/parse/parser_test.go
compiler/parse/expr_test.go
compiler/parse/core_source_test.go
  Parses generic type expressions, generic constructor expressions, generic declarations, traits, impls, enums, constants, static assertions, enum variant constructors, if-let, and match.

compiler/sem/types.go
compiler/sem/symbols.go
compiler/sem/check.go
compiler/sem/symbols_test.go
  Extends Type, Field, Method, and Index to structured types, generic declarations, instantiations, trait declarations, impl declarations, enum variants, constants, and protected type metadata.

compiler/sem/generic.go
compiler/sem/generic_test.go
  Owns generic parameter scope checks, arity checks, type substitution, deterministic instantiation keys, and monomorphization registry.

compiler/sem/const.go
compiler/sem/const_test.go
  Owns const expression evaluation, sizeof, alignof, static_assert, and checked arithmetic diagnostics.

compiler/sem/trait.go
compiler/sem/trait_test.go
  Owns trait signature indexing, impl validation, overlap checks, and direct-call resolution for trait-constrained methods.

compiler/sem/enum.go
compiler/sem/enum_test.go
  Owns enum constructor typing, payload bindings, if-let checks, match exhaustiveness, and variant diagnostics.

compiler/sem/memory.go
compiler/sem/memory_test.go
  Extends existing lifetime and arena rules for Slots/Slice/MutableSlice, reserve_array, protected view construction, and raw Slots read rejection.

compiler/layout/record.go
compiler/layout/record_test.go
compiler/layout/enum.go
compiler/layout/enum_test.go
  Adds generic layout lookup and deterministic enum discriminant/payload layout.

compiler/ir/ir.go
compiler/ir/lower.go
compiler/ir/generic_test.go
compiler/ir/enum_test.go
compiler/ir/memory_test.go
  Adds concrete generic type-info keys, enum operations, variant tests/extractions, arena array reservations, slot writes, slice reads/writes, and match lowering.

compiler/codegen/x64.go
compiler/codegen/enum_test.go
compiler/codegen/memory_test.go
compiler/codegen/topic_test.go
  Emits enum construction/matching, checked typed arena array reservation, slots/slice bounds checks, and generic topic/result code shapes.

compiler/sem/topic_graph.go
compiler/sem/topic_payload.go
compiler/sem/topic_graph_test.go
compiler/sem/topic_payload_test.go
compiler/ir/topic_test.go
compiler/codegen/topic_test.go
compiler/codegen/topic_data.go
compiler/codegen/timer.go
  Migrates topic recognition and topic codegen from hard-coded concrete topic classes to generic `Topic<T>` metadata.

compiler/negative_fixtures_test.go
compiler/build_test.go
  Keeps test harness AST construction and build import-root coverage aligned with TypeRef/core module changes.

wrela/lang/core.wrela
  Defines Unit, Option<T>, Result<T, E>, Publisher<T>, and Subscription<T>.

wrela/machine/x86_64/executor_memory.wrela
  Adds Slice<T>, MutableSlice<T>, Slots<T>, FixedBuffer<T>, Ring<T>, and source-visible signatures for compiler intrinsics.

wrela/machine/x86_64/topic.wrela
  Defines generic Topic<T>, TopicPublisher<T>, TopicSubscription<T>, ReliableTopic<T>, ReliablePublisher<T>, ReliableSubscription<T>, and trait impls.

wrela/machine/x86_64/topic_u64.wrela
wrela/machine/x86_64/topic_payload.wrela
wrela/machine/x86_64/timer.wrela
wrela/machine/x86_64/serial.wrela
wrela/machine/x86_64/edu.wrela
wrela/machine/x86_64/ivshmem.wrela
wrela/machine/x86_64/interrupt_queue.wrela
  Removes or deprecates concrete duplicated topics/results and imports generic equivalents.

wrela/platform/hardware/bytes.wrela
wrela/platform/hardware/memory.wrela
wrela/platform/uefi/types.wrela
wrela/platform/acpi/*.wrela
  Adds region-kind generic views; migrates MMIO construction in hardware discovery, firmware table slices in UEFI/ACPI readers, and DMA buffer ownership in hardware memory helpers.

examples/hello/*.wrela
examples/multi_vcpu_topics/main.wrela
tests/e2e/fixtures/**/*.wrela
tests/fixtures/negative/*.wrela
compiler/integration_test.go
tests/e2e/*_test.go
  Migrates source examples and checks to generic topics, enums, constants, typed slots, and match/if-let.
```

---

## 3. Parallel Work Map

This milestone has a mostly serial spine. The syntax, semantic type model, monomorphization registry, layout, IR, and codegen layers must land in order before broad source migration begins. Parallelism is useful only after the generic topic/memory libraries exist, where independent source modules can be migrated in separate branches.

```text
Merge Gate 0: Tasks 1-4
  Freezes diagnostics, AST contracts, and parser syntax.

Task 4.5
  Adds core source definitions and missing test helpers before semantic tests use Option/Result.

Stream A: Semantic Language Model, serial
  Tasks 5A-9
  Owns generic indexing, monomorphization, consts, traits, enums, and memory-view semantics.

Stream B: Layout/IR/Codegen, serial
  Tasks 10-12E, then Task 13
  Starts after Tasks 5-9. Owns concrete layouts and runtime code shape.

Stream C: Platform Library And Source Migration
  Tasks 14-16 are serial library setup.
  Tasks 17A-17C can run in parallel after Task 16 because they edit separate source modules.
  Task 17D is a merge gate after 17A-17C because it updates shared topic compiler metadata.
  Task 17E runs after 17D because interrupt queues rely on the shared generic topic metadata.
  Tasks 18A-18C can run in parallel after Task 17E because each migrates separate examples/fixtures.

Stream D: Region Views And Final Sweep
  Tasks 19-21
  Runs after source migration and owns protected region views, report cleanup, and acceptance checks.
```

Dependency rules:

- Task 1 lands first.
- Tasks 2-4 are serial.
- Task 4.5 lands before Task 5A because semantic tests use the image-tolerant helpers and the core source file must exist before Task 14 loads it.
- Tasks 5A-9 are serial because later checks depend on the type/index and monomorphization model established earlier.
- Tasks 10-12E are serial by compiler layer and emitter dependency; Task 13 follows once monomorphized method calls and x86_64 call emission are stable.
- Tasks 14-16 are serial.
- Tasks 17A-17C may run in parallel after Task 16.
- Task 17D runs after Tasks 17A-17C have merged.
- Task 17E runs after Task 17D.
- Tasks 18A-18C may run in parallel after Task 17E.
- Tasks 19-21 are serial.

---

## 4. Canonical Source Surface

The source spelling below is fixed. Tests and examples must use these names exactly.

```wrela
module wrela.lang.core

data Unit {}

enum Option<T> {
    None
    Some(value: T)
}

enum Result<T, E> {
    Ok(value: T)
    Err(error: E)
}

trait Publisher<T> {
    fn publish(self, value: T)
}

trait Subscription<T> {
    fn try_next(self) -> Option<T>
    fn arm_wait(self)
    fn is_wait_armed(self) -> Bool
}
```

```wrela
module machine.x86_64.executor_memory

data Slice<T> {
    address: PhysicalAddress
    length: U64
}

data MutableSlice<T> {
    address: PhysicalAddress
    length: U64
}

data Slots<T> {
    address: PhysicalAddress
    capacity: U64

    asm fn write(self, index: U64, value: T) {
        ret
    }

    asm fn fill(self, value: T) -> MutableSlice<T> {
        ret
    }
}

data FixedBuffer<T> {
    slots: Slots<T>
    length: U64

    fn push(self, value: T) -> Result<Unit, BufferFull> {
        if self.length == self.slots.capacity {
            return Result.Err(error = BufferFull())
        }
        self.slots.write(index = self.length, value = value)
        self.length = self.length + 1
        return Result.Ok(value = Unit())
    }
}
```

```wrela
let slots = tick.reserve_array(Event, count = EVENT_CAPACITY)
let events = tick.place(FixedBuffer<Event>(slots = slots, length = 0))

match self.rx.try_next() {
    Option.Some(value = event) => {
        events.push(value = event)
    }
    Option.None => {
        self.rx.arm_wait()
    }
}
```

---

## 5. Phase 1: Syntax, AST, And Parser Contracts

**Description:** This phase makes the language surface parseable and represented structurally. It does not try to type-check generics or emit code yet.

**Phase Acceptance Criteria:**

- Lexer recognizes all new keywords without changing existing token text.
- AST no longer represents new type positions as raw strings.
- Parser accepts canonical examples for generic declarations, enum declarations, trait declarations, impl declarations, constants, static assertions, `if let`, `match`, enum variant construction, `sizeof`, `alignof`, and `reserve_array(Event, count = N)`.
- Existing parser tests still pass.

**Phase Code Example:**

```wrela
enum Option<T> { None Some(value: T) }
trait Subscription<T> { fn try_next(self) -> Option<T> }
impl Subscription<Event> for EventSubscription
const EVENT_BYTES: U64 = sizeof(Event) * 16
static_assert(EVENT_BYTES <= 4096, message = "event frame fits")
```

### Task 1: Reserve Diagnostics And Keyword Tokens

**Description:** Add stable diagnostic code constants and lexical keywords used by later tasks.

**Files:**
- Modify: `compiler/diag/codes.go`
- Modify: `compiler/lex/token.go`
- Modify: `compiler/lex/lexer_test.go`

**Acceptance Criteria:**
- SEM0076-SEM0096 exist with the exact meanings below.
- New keywords tokenize as keyword tokens.
- `=>` tokenizes as `FatArrow`; existing `->` continues to tokenize as `Arrow`.
- Existing words that are legal type/value names remain identifiers unless listed as keywords.

- [ ] **Step 1: Add failing lexer and diagnostic tests**

Add to `compiler/lex/lexer_test.go`:

```go
func TestLanguageExpressivenessKeywords(t *testing.T) {
    src := "enum trait impl for where const static_assert match sizeof alignof"
    toks, ds := All(src)
    if len(ds) != 0 {
        t.Fatalf("diagnostics: %#v", ds)
    }
    wants := []Kind{
        KeywordEnum, KeywordTrait, KeywordImpl, KeywordFor, KeywordWhere,
        KeywordConst, KeywordStaticAssert, KeywordMatch, KeywordSizeof, KeywordAlignof,
    }
    for i, want := range wants {
        if toks[i].Kind != want {
            t.Fatalf("token %d = %#v, want %v", i, toks[i], want)
        }
    }
}

func TestFatArrowToken(t *testing.T) {
    toks, ds := All("Option.None => { }")
    if len(ds) != 0 {
        t.Fatalf("diagnostics: %#v", ds)
    }
    got := []Kind{toks[0].Kind, toks[1].Kind, toks[2].Kind, toks[3].Kind}
    want := []Kind{Identifier, Dot, Identifier, FatArrow}
    if !reflect.DeepEqual(got, want) {
        t.Fatalf("kinds = %#v, want %#v", got, want)
    }
}
```

Add to `compiler/diag/codes_test.go`:

```go
func TestLanguageExpressivenessDiagnosticCodesExist(t *testing.T) {
    codes := []string{
        SEM0076, SEM0077, SEM0078, SEM0079, SEM0080, SEM0081, SEM0082,
        SEM0083, SEM0084, SEM0085, SEM0086, SEM0087, SEM0088, SEM0089,
        SEM0090, SEM0091, SEM0092, SEM0093, SEM0094, SEM0095, SEM0096,
    }
    for _, code := range codes {
        if code == "" {
            t.Fatal("language expressiveness diagnostic code must not be empty")
        }
    }
}
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
go test ./compiler/lex -run TestLanguageExpressivenessKeywords -v
go test ./compiler/lex -run TestFatArrowToken -v
go test ./compiler/diag -run TestLanguageExpressivenessDiagnosticCodesExist -v
```

Expected: FAIL with undefined keyword and diagnostic identifiers.

- [ ] **Step 3: Add diagnostic constants**

Add after SEM0075 in `compiler/diag/codes.go`:

```go
    SEM0076 = "SEM0076" // generic declaration has duplicate type parameter
    SEM0077 = "SEM0077" // generic type arity mismatch
    SEM0078 = "SEM0078" // unknown type parameter or type argument
    SEM0079 = "SEM0079" // generic or enum type arguments cannot be inferred
    SEM0080 = "SEM0080" // unsized type used where layout is required
    SEM0081 = "SEM0081" // missing trait implementation
    SEM0082 = "SEM0082" // trait method signature mismatch
    SEM0083 = "SEM0083" // ambiguous or overlapping impl
    SEM0084 = "SEM0084" // non-exhaustive match
    SEM0085 = "SEM0085" // impossible enum variant pattern
    SEM0086 = "SEM0086" // const expression overflow
    SEM0087 = "SEM0087" // non-const operand in const expression
    SEM0088 = "SEM0088" // invalid sizeof or alignof operand
    SEM0089 = "SEM0089" // static assertion failed
    SEM0090 = "SEM0090" // slot count or reservation size overflow
    SEM0091 = "SEM0091" // slots or slice lifetime escape
    SEM0092 = "SEM0092" // protected memory-region view construction is not allowed here
    SEM0093 = "SEM0093" // raw Slots memory cannot be read directly
    SEM0094 = "SEM0094" // enum variant constructor is invalid
    SEM0095 = "SEM0095" // match or if-let pattern binding is invalid
    SEM0096 = "SEM0096" // protected view field access is not allowed here
```

- [ ] **Step 4: Add keyword tokens**

Add token kinds after `KeywordIn` in `compiler/lex/token.go`:

```go
    KeywordEnum
    KeywordTrait
    KeywordImpl
    KeywordFor
    KeywordWhere
    KeywordConst
    KeywordStaticAssert
    KeywordMatch
    KeywordSizeof
    KeywordAlignof
```

Add `FatArrow` immediately after `Arrow` so statement parsing can distinguish `=>` match arms from existing `->` return arrows:

```go
    Arrow
    FatArrow
```

Keep the existing `KeywordFor` if it already exists; add only the missing names. Add keyword map entries:

```go
    "enum":          KeywordEnum,
    "trait":         KeywordTrait,
    "impl":          KeywordImpl,
    "for":           KeywordFor,
    "where":         KeywordWhere,
    "const":         KeywordConst,
    "static_assert": KeywordStaticAssert,
    "match":         KeywordMatch,
    "sizeof":        KeywordSizeof,
    "alignof":       KeywordAlignof,
```

Add the two-character operator:

```go
    "=>": FatArrow,
```

- [ ] **Step 5: Verify**

Run:

```bash
go test ./compiler/lex -run TestLanguageExpressivenessKeywords -v
go test ./compiler/lex -run TestFatArrowToken -v
go test ./compiler/diag -run TestLanguageExpressivenessDiagnosticCodesExist -v
git diff --check
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add compiler/diag/codes.go compiler/diag/codes_test.go compiler/lex/token.go compiler/lex/lexer_test.go
git commit -m "feat: reserve language expressiveness diagnostics and keywords -Codex Automated"
```

### Task 2: Add Structured AST Contracts

**Description:** Replace raw type strings for new syntax with `ast.TypeRef` and add AST nodes for generics, enums, traits, impls, consts, static assertions, patterns, if-let, and match.

**Files:**
- Modify: `compiler/ast/ast.go`
- Modify: `compiler/ast/ast_test.go`
- Modify: `compiler/parse/parser.go`
- Modify: `compiler/parse/expr.go`
- Modify: `compiler/sem/symbols.go`
- Modify: `compiler/sem/check.go`
- Modify: `compiler/ir/lower.go`
- Modify: `compiler/negative_fixtures_test.go`

**Acceptance Criteria:**
- `ast.TypeRef.String()` produces deterministic source-shaped names.
- Existing debug output still works for old expressions/statements.
- New AST debug helpers can render the canonical snippets in this task.

- [ ] **Step 1: Add failing AST tests**

Add to `compiler/ast/ast_test.go`:

```go
func TestTypeRefString(t *testing.T) {
    typ := TypeRef{
        Name: "Result",
        Args: []TypeRef{
            {Name: "Unit"},
            {Name: "BufferFull"},
        },
    }
    if got, want := typ.String(), "Result<Unit, BufferFull>"; got != want {
        t.Fatalf("TypeRef.String() = %q, want %q", got, want)
    }
}

func TestDebugMatchStmt(t *testing.T) {
    stmt := &MatchStmt{
        Value: &CallExpr{Receiver: &NameExpr{Name: "rx"}, Method: "try_next"},
        Arms: []MatchArm{
            {
                Pattern: VariantPattern{Enum: "Option", Variant: "Some", Bindings: []PatternBinding{{Name: "value", Bind: "event"}}},
                Body: []Stmt{&ExprStmt{Expr: &CallExpr{Receiver: &NameExpr{Name: "events"}, Method: "push", Args: []NamedArg{{Name: "value", Value: &NameExpr{Name: "event"}}}}}},
            },
            {
                Pattern: VariantPattern{Enum: "Option", Variant: "None"},
                Body: []Stmt{&ExprStmt{Expr: &CallExpr{Receiver: &NameExpr{Name: "rx"}, Method: "arm_wait"}}},
            },
        },
    }
    want := "match rx.try_next() { Option.Some(value = event) => { events.push(value = event) } Option.None => { rx.arm_wait() } }"
    if got := DebugStmt(stmt); got != want {
        t.Fatalf("DebugStmt = %q, want %q", got, want)
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./compiler/ast -run 'TestTypeRefString|TestDebugMatchStmt' -v`

Expected: FAIL with undefined `TypeRef` and `MatchStmt`.

- [ ] **Step 3: Add AST types**

Add these types to `compiler/ast/ast.go` near the current type-bearing structs:

```go
type TypeRef struct {
    Name  string
    Args  []TypeRef
    SpanV source.Span
}

func (t TypeRef) Span() source.Span { return t.SpanV }

func (t TypeRef) String() string {
    if len(t.Args) == 0 {
        return t.Name
    }
    parts := make([]string, 0, len(t.Args))
    for _, arg := range t.Args {
        parts = append(parts, arg.String())
    }
    return t.Name + "<" + strings.Join(parts, ", ") + ">"
}

type TypeParam struct {
    Name string
    Span source.Span
}

type TraitBound struct {
    Param string
    Trait TypeRef
    Span  source.Span
}
```

Extend declaration structs with `TypeParams []TypeParam` and `Where []TraitBound` where applicable: `DataDecl`, `ClassDecl`, `DriverDecl`, `MethodDecl`, `EnumDecl`, and `TraitDecl`.

Add new declarations:

```go
type EnumDecl struct {
    Name       string
    TypeParams []TypeParam
    Variants   []EnumVariant
    SpanV      source.Span
}

func (d *EnumDecl) Span() source.Span { return d.SpanV }

type EnumVariant struct {
    Name   string
    Fields []Field
    Span   source.Span
}

type TraitDecl struct {
    Name       string
    TypeParams []TypeParam
    Methods    []MethodDecl
    SpanV      source.Span
}

func (d *TraitDecl) Span() source.Span { return d.SpanV }

type ImplDecl struct {
    Trait TypeRef
    For   TypeRef
    SpanV source.Span
}

func (d *ImplDecl) Span() source.Span { return d.SpanV }

type ConstDecl struct {
    Name  string
    Type  TypeRef
    Value Expr
    SpanV source.Span
}

func (d *ConstDecl) Span() source.Span { return d.SpanV }

type StaticAssertDecl struct {
    Expr    Expr
    Message string
    SpanV   source.Span
}

func (d *StaticAssertDecl) Span() source.Span { return d.SpanV }
```

Change `Field.Type`, `Param.Type`, `MethodDecl.Return`, `OnHandlerDecl.ParamType`, `PhaseDecl.Return`, and `InterruptEventDecl.EventType` to `TypeRef`. During the migration, use `TypeRef{Name: oldString}` at existing test construction sites.

Change constructor expressions at the same time. This is required for `FixedBuffer<RunEvent>(...)`, `Topic<TimerTickPayload>(...)`, and `Slots<Event>(...)` source:

```go
type ConstructorExpr struct {
    Type  TypeRef
    Args  []NamedArg
    SpanV source.Span
}
```

Update `DebugExpr` for constructors:

```go
case *ConstructorExpr:
    return e.Type.String() + "(" + debugNamedArgs(e.Args) + ")"
```

Existing tests that construct `&ConstructorExpr{Type: "Bytes"}` must become:

```go
&ConstructorExpr{Type: TypeRef{Name: "Bytes"}}
```

This task must keep the whole repository compiling before generic parser support exists. Migrate current string-producing parser and string-consuming semantic/IR code to wrap or unwrap `TypeRef` without parsing generic arguments yet.

In `compiler/parse/parser.go` and `compiler/parse/expr.go`, keep the existing `parseTypeName() string` helper for this task, but wrap every assignment into a type-bearing AST field:

```go
typName, ds := p.parseTypeName()
if len(ds) != 0 {
    return nil, ds
}
field := ast.Field{
    Name: name.Text,
    Type: ast.TypeRef{Name: typName},
    Span: p.span(name.Start, p.previous().End),
}
```

Apply the same wrapper for `ast.Param.Type`, `ast.MethodDecl.Return`, `ast.OnHandlerDecl.ParamType`, `ast.PhaseDecl.Return`, `ast.InterruptEventDecl.EventType`, and `ast.ConstructorExpr.Type`.

In semantic code, add this temporary helper in `compiler/sem/symbols.go`:

```go
func legacyTypeName(ref ast.TypeRef) string {
    return ref.Name
}
```

Use `legacyTypeName` wherever current semantic code expects a raw type string. Example:

```go
fieldType := s.resolveType(module.Name, legacyTypeName(field.Type), field.Span)
```

Apply that unwrap mechanically to the current semantic lifetime and memory helpers too. These exact current sites are required in Task 2, not deferred:

```go
// compiler/sem/memory.go
return len(params) == 1 && params[0].Name == "length" && legacyTypeName(params[0].Type) == "U64"
returnType := c.mustType(mod.Name, legacyTypeName(method.Return))

// compiler/sem/check.go
retType := c.mustType(moduleName, legacyTypeName(phase.Return))
eventType := c.mustType(moduleName, legacyTypeName(handler.ParamType))
eventType := c.mustType(moduleName, legacyTypeName(interrupt.EventType))
```

In `compiler/ir/lower.go`, keep the current `resolveType(moduleName, raw string)` implementation and unwrap AST type refs at call sites until Task 5C installs `resolveTypeRef` for structured generic types:

```go
typ := ctx.resolveType(moduleName, param.Type.Name)
ret := ctx.resolveType(moduleName, method.Return.Name)
```

Before marking Task 2 complete, run this checklist and fix every compile-time type mismatch it reveals:

```bash
rg -n "\\.(Type|Return|ParamType|EventType)\\b|mustType\\([^\\n]*method\\.Return|resolveType\\([^\\n]*\\.Type|== \\\"U64\\\"" compiler/sem compiler/ir compiler/parse compiler/ast
go test ./compiler/... -run 'TestTypeRefString|TestDebugMatchStmt|TestSourceTypes|TestLowerReturnsCG0001ForNilProgram' -v
```

Expected: no remaining direct string comparisons or string-only calls for type-bearing AST fields, except inside `legacyTypeName(...)`, `resolveType(moduleName, raw string)`, and tests that intentionally assert legacy wrapper behavior.

No generic `TypeRef.Args` should be consumed by parser, semantic, or IR code in Task 2. Task 3 is the first task that populates `Args`.

Update the negative fixture harness prelude in `compiler/negative_fixtures_test.go` in the same change. Its manually constructed AST must use `ast.TypeRef` for field, parameter, return, and constructor types:

```go
&ast.DataDecl{
    Name: "ExecutorPlacement",
    Fields: []ast.Field{{
        Name: "id",
        Type: ast.TypeRef{Name: "U64"},
        Span: source.Span{},
    }},
    SpanV: source.Span{},
}
```

```go
&ast.MethodDecl{
    Name: "exit_to_owned_hardware",
    Params: []ast.Param{{
        Name: "self",
        Type: ast.TypeRef{Name: "DelegatedHardware"},
        Span: source.Span{},
    }},
    Return: ast.TypeRef{Name: "OwnedHardware"},
    Body: []ast.Stmt{&ast.ReturnStmt{
        Value: &ast.ConstructorExpr{
            Type:  ast.TypeRef{Name: "OwnedHardware"},
            Args:  nil,
            SpanV: source.Span{},
        },
        SpanV: source.Span{},
    }},
    SpanV: source.Span{},
}
```

Add new expressions and statements:

```go
type VariantConstructorExpr struct {
    Enum    string
    Variant string
    Args    []NamedArg
    SpanV   source.Span
}

func (e *VariantConstructorExpr) Span() source.Span { return e.SpanV }

type SizeOfExpr struct {
    Type  TypeRef
    SpanV source.Span
}

func (e *SizeOfExpr) Span() source.Span { return e.SpanV }

type AlignOfExpr struct {
    Type  TypeRef
    SpanV source.Span
}

func (e *AlignOfExpr) Span() source.Span { return e.SpanV }

type TypeOperandExpr struct {
    Type  TypeRef
    SpanV source.Span
}

func (e *TypeOperandExpr) Span() source.Span { return e.SpanV }

type Pattern interface {
    patternString() string
}

type VariantPattern struct {
    Enum     string
    Variant  string
    Bindings []PatternBinding
}

func (p VariantPattern) patternString() string { return debugPattern(p) }

type WildcardPattern struct{}

func (p WildcardPattern) patternString() string { return "_" }

type PatternBinding struct {
    Name string
    Bind string
}

type IfLetStmt struct {
    Pattern Pattern
    Value   Expr
    Body    []Stmt
    SpanV   source.Span
}

func (s *IfLetStmt) Span() source.Span { return s.SpanV }

type MatchStmt struct {
    Value Expr
    Arms  []MatchArm
    SpanV source.Span
}

func (s *MatchStmt) Span() source.Span { return s.SpanV }

type MatchArm struct {
    Pattern Pattern
    Body    []Stmt
    Span    source.Span
}
```

- [ ] **Step 4: Add debug rendering**

Update `DebugExpr` and `DebugStmt` with these cases:

```go
case *VariantConstructorExpr:
    return e.Enum + "." + e.Variant + "(" + debugNamedArgs(e.Args) + ")"
case *SizeOfExpr:
    return "sizeof(" + e.Type.String() + ")"
case *AlignOfExpr:
    return "alignof(" + e.Type.String() + ")"
case *TypeOperandExpr:
    return e.Type.String()
```

```go
case *IfLetStmt:
    return "if let " + debugPattern(s.Pattern) + " = " + DebugExpr(s.Value) + " { " + debugStmtList(s.Body) + " }"
case *MatchStmt:
    parts := make([]string, 0, len(s.Arms))
    for _, arm := range s.Arms {
        parts = append(parts, debugPattern(arm.Pattern)+" => { "+debugStmtList(arm.Body)+" }")
    }
    return "match " + DebugExpr(s.Value) + " { " + strings.Join(parts, " ") + " }"
```

Add helper:

```go
func debugPattern(pattern Pattern) string {
    switch p := pattern.(type) {
    case VariantPattern:
        if len(p.Bindings) == 0 {
            return p.Enum + "." + p.Variant
        }
        parts := make([]string, 0, len(p.Bindings))
        for _, binding := range p.Bindings {
            parts = append(parts, binding.Name+" = "+binding.Bind)
        }
        return p.Enum + "." + p.Variant + "(" + strings.Join(parts, ", ") + ")"
    case WildcardPattern:
        return "_"
    default:
        return "<pattern>"
    }
}
```

- [ ] **Step 5: Verify**

Run:

```bash
go test ./compiler/ast -v
go test ./compiler/parse -v
go test ./compiler/sem -run TestTypeIndex -v
go test ./compiler/ir -run TestLowerReturnsCG0001ForNilProgram -v
go test ./compiler -run TestNegativeFixtures -v
git diff --check
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add compiler/ast/ast.go compiler/ast/ast_test.go compiler/parse/parser.go compiler/parse/expr.go compiler/sem/symbols.go compiler/sem/check.go compiler/ir/lower.go compiler/negative_fixtures_test.go
git commit -m "feat: add language expressiveness AST contracts -Codex Automated"
```

### Task 3: Parse Type Expressions And Generic Declarations

**Description:** Parse `TypeRef` everywhere a type appears and accept generic declarations/methods with optional `where` bounds.

**Files:**
- Modify: `compiler/parse/parser.go`
- Modify: `compiler/parse/expr.go`
- Modify: `compiler/parse/parser_test.go`
- Modify: `compiler/parse/expr_test.go`
- Modify: tests touched by `ast.TypeRef` migration.

**Acceptance Criteria:**
- `Topic<TimerTickPayload>` parses in field, parameter, return, and constructor type positions.
- `FixedBuffer<RunEvent>(slots = slots, length = 0)` parses as `ConstructorExpr{Type: TypeRef{Name:"FixedBuffer", Args: []TypeRef{{Name:"RunEvent"}}}}`.
- `data FixedBuffer<T> where T: Copyable { slots: Slots<T> }` records type params and bounds.
- `trait Subscription<T> { fn try_next(self) -> Option<T> }` parses as an `ast.TraitDecl`.
- `impl Publisher<T> for TopicPublisher<T>` parses as an `ast.ImplDecl`.

- [ ] **Step 1: Add failing parser tests**

Add to `compiler/parse/parser_test.go`:

```go
func TestParseGenericDeclsAndTypes(t *testing.T) {
    mod, ds := parseModuleForTest(t, `
module parser.generics

data FixedBuffer<T> {
    slots: Slots<T>
    length: U64
}

trait Subscription<T> {
    fn try_next(self) -> Option<T>
}

trait Publisher<T> {
    fn publish(self, value: T)
}

class DrainLoop<S, T> where S: Subscription<T> {
    input: S
    fn poll(self) -> Option<T> {
        return self.input.try_next()
    }
}

impl Publisher<T> for TopicPublisher<T>
`)
    if len(ds) != 0 {
        t.Fatalf("diagnostics: %#v", ds)
    }
    data := mod.Decls[0].(*ast.DataDecl)
    if data.TypeParams[0].Name != "T" || data.Fields[0].Type.String() != "Slots<T>" {
        t.Fatalf("generic data parsed incorrectly: %#v", data)
    }
    trait := mod.Decls[1].(*ast.TraitDecl)
    if trait.Name != "Subscription" || trait.Methods[0].Return.String() != "Option<T>" {
        t.Fatalf("trait parsed incorrectly: %#v", trait)
    }
    class := mod.Decls[3].(*ast.ClassDecl)
    if len(class.Where) != 1 || class.Where[0].Trait.String() != "Subscription<T>" {
        t.Fatalf("where bounds = %#v", class.Where)
    }
    impl := mod.Decls[4].(*ast.ImplDecl)
    if impl.Trait.String() != "Publisher<T>" || impl.For.String() != "TopicPublisher<T>" {
        t.Fatalf("impl = %#v", impl)
    }
}
```

Add to `compiler/parse/expr_test.go`:

```go
func TestParseGenericConstructorExpression(t *testing.T) {
    p := newParser("expr.wrela", "FixedBuffer<RunEvent>(slots = slots, length = 0)")
    expr, ds := p.parseExpr(0)
    if len(ds) != 0 {
        t.Fatalf("diagnostics = %#v", ds)
    }
    con, ok := expr.(*ast.ConstructorExpr)
    if !ok {
        t.Fatalf("expr = %T, want ConstructorExpr", expr)
    }
    if got, want := con.Type.String(), "FixedBuffer<RunEvent>"; got != want {
        t.Fatalf("constructor type = %q, want %q", got, want)
    }
    if len(con.Args) != 2 {
        t.Fatalf("args = %#v, want two args", con.Args)
    }
}

func TestGenericConstructorLookaheadDoesNotStealComparison(t *testing.T) {
    p := newParser("expr.wrela", "a < b")
    expr, ds := p.parseExpr(0)
    if len(ds) != 0 {
        t.Fatalf("diagnostics = %#v", ds)
    }
    bin, ok := expr.(*ast.BinaryExpr)
    if !ok || bin.Op != "<" {
        t.Fatalf("expr = %#v, want binary comparison", expr)
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run:

```bash
go test ./compiler/parse -run TestParseGenericDeclsAndTypes -v
go test ./compiler/parse -run 'TestParseGenericConstructorExpression|TestGenericConstructorLookaheadDoesNotStealComparison' -v
```

Expected: FAIL because generic type syntax is not parsed.

- [ ] **Step 3: Implement type parsing helpers**

Reuse the existing `parseDottedName()` helper and replace `parseTypeName() (string, []diag.Diagnostic)` call sites with `parseTypeRef() (ast.TypeRef, []diag.Diagnostic)`:

```go
func (p *Parser) parseTypeRef() (ast.TypeRef, []diag.Diagnostic) {
    start := p.peek().Start
    name := ""
    if p.peek().Kind == lex.KeywordNever {
        name = p.next().Text
    } else {
        parsed, ds := p.parseDottedName()
        if len(ds) != 0 {
            return ast.TypeRef{}, ds
        }
        name = parsed
    }

    ref := ast.TypeRef{Name: name, SpanV: p.span(start, p.previous().End)}
    if p.match(lex.Less) {
        for {
            arg, ds := p.parseTypeRef()
            if len(ds) != 0 {
                return ast.TypeRef{}, ds
            }
            ref.Args = append(ref.Args, arg)
            p.skipSeparators()
            if !p.match(lex.Comma) {
                break
            }
            p.skipSeparators()
        }
        if _, ds := p.consumeTypeGreater(); len(ds) != 0 {
            return ast.TypeRef{}, p.err(p.peek(), diag.PAR0001, "expected '>' after type arguments")
        }
        ref.SpanV.End = p.previous().End
    }
    return ref, nil
}
```

Add a type-only greater-than consumer so nested generic refs such as `Pair<Box<Event>>` do not collide with the existing expression `ShiftRight` token:

```go
func (p *Parser) consumeTypeGreater() (lex.Token, []diag.Diagnostic) {
    if p.match(lex.Greater) {
        return p.previous(), nil
    }
    if p.peek().Kind != lex.ShiftRight {
        return lex.Token{}, p.err(p.peek(), diag.PAR0001, "expected '>' after type arguments")
    }
    tok := p.next()
    first := lex.Token{Kind: lex.Greater, Text: ">", Start: tok.Start, End: tok.Start + 1}
    second := lex.Token{Kind: lex.Greater, Text: ">", Start: tok.Start + 1, End: tok.End}
    p.toks = append(p.toks[:p.idx], append([]lex.Token{second}, p.toks[p.idx:]...)...)
    p.toks[p.idx-1] = first
    return first, nil
}
```

Add:

```go
func (p *Parser) parseTypeParams() ([]ast.TypeParam, []diag.Diagnostic) {
    if !p.match(lex.Less) {
        return nil, nil
    }
    var out []ast.TypeParam
    for {
        tok, ds := p.expectIdentifier("expected type parameter")
        if len(ds) != 0 {
            return nil, ds
        }
        out = append(out, ast.TypeParam{Name: tok.Text, Span: p.span(tok.Start, tok.End)})
        p.skipSeparators()
        if !p.match(lex.Comma) {
            break
        }
        p.skipSeparators()
    }
    if _, ds := p.consume(lex.Greater); len(ds) != 0 {
        return nil, p.err(p.peek(), diag.PAR0001, "expected '>' after type parameters")
    }
    return out, nil
}

func (p *Parser) parseWhereClause() ([]ast.TraitBound, []diag.Diagnostic) {
    if !p.match(lex.KeywordWhere) {
        return nil, nil
    }
    var out []ast.TraitBound
    for {
        start := p.peek().Start
        param, ds := p.expectIdentifier("expected type parameter in where clause")
        if len(ds) != 0 {
            return nil, ds
        }
        if _, ds := p.consume(lex.Colon); len(ds) != 0 {
            return nil, ds
        }
        trait, ds := p.parseTypeRef()
        if len(ds) != 0 {
            return nil, ds
        }
        out = append(out, ast.TraitBound{Param: param.Text, Trait: trait, Span: p.span(start, trait.Span().End)})
        p.skipSeparators()
        if !p.match(lex.Comma) {
            break
        }
        p.skipSeparators()
    }
    return out, nil
}
```

- [ ] **Step 4: Wire declarations**

In `parseDataDecl`, `parseClassDecl`, `parseDriverDecl`, and `parseMethodDecl`, parse type params immediately after the declaration or method name, then parse `where` after params and before `{`:

```go
name, ds := p.expectIdentifier("expected data name")
typeParams, ds := p.parseTypeParams()
where, ds := p.parseWhereClause()
```

Add explicit `parseDecl` cases for traits and impls. Do not rely on the default declaration parser:

```go
case lex.KeywordTrait:
    return p.parseTraitDecl()
case lex.KeywordImpl:
    return p.parseImplDecl()
```

Add `parseTraitDecl` before `parseImplDecl` because `wrela/lang/core.wrela` in Task 4.5 will not parse without it:

```go
func (p *Parser) parseTraitDecl() (ast.Decl, []diag.Diagnostic) {
    start := p.next() // trait
    name, ds := p.expectIdentifier("expected trait name")
    if len(ds) != 0 {
        return nil, ds
    }
    typeParams, ds := p.parseTypeParams()
    if len(ds) != 0 {
        return nil, ds
    }
    if _, ds := p.consume(lex.LBrace); len(ds) != 0 {
        return nil, ds
    }
    var methods []ast.MethodDecl
    for p.peek().Kind != lex.RBrace && p.peek().Kind != lex.EOF {
        p.skipSeparators()
        if p.peek().Kind == lex.RBrace {
            break
        }
        method, ds := p.parseTraitMethodDecl()
        if len(ds) != 0 {
            return nil, ds
        }
        methods = append(methods, method)
        p.skipSeparators()
    }
    if _, ds := p.consume(lex.RBrace); len(ds) != 0 {
        return nil, ds
    }
    return &ast.TraitDecl{Name: name.Text, TypeParams: typeParams, Methods: methods, SpanV: p.span(start.Start, p.previous().End)}, nil
}

func (p *Parser) parseTraitMethodDecl() (ast.MethodDecl, []diag.Diagnostic) {
    start, ds := p.consume(lex.KeywordFn)
    if len(ds) != 0 {
        return ast.MethodDecl{}, ds
    }
    name, ds := p.expectIdentifier("expected trait method name")
    if len(ds) != 0 {
        return ast.MethodDecl{}, ds
    }
    if _, ds := p.consume(lex.LParen); len(ds) != 0 {
        return ast.MethodDecl{}, ds
    }
    params, ds := p.parseParams()
    if len(ds) != 0 {
        return ast.MethodDecl{}, ds
    }
    if _, ds := p.consume(lex.RParen); len(ds) != 0 {
        return ast.MethodDecl{}, ds
    }
    ret := ast.TypeRef{}
    if p.match(lex.Arrow) {
        ret, ds = p.parseTypeRef()
        if len(ds) != 0 {
            return ast.MethodDecl{}, ds
        }
    }
    return ast.MethodDecl{Name: name.Text, Params: params, Return: ret, SpanV: p.span(start.Start, p.previous().End)}, nil
}
```

For `impl`, add a `parseImplDecl` invoked from `parseDecl`:

```go
func (p *Parser) parseImplDecl() (ast.Decl, []diag.Diagnostic) {
    start := p.next() // impl
    trait, ds := p.parseTypeRef()
    if len(ds) != 0 {
        return nil, ds
    }
    if _, ds := p.consume(lex.KeywordFor); len(ds) != 0 {
        return nil, p.err(p.peek(), diag.PAR0001, "expected for in impl declaration")
    }
    implemented, ds := p.parseTypeRef()
    if len(ds) != 0 {
        return nil, ds
    }
    return &ast.ImplDecl{Trait: trait, For: implemented, SpanV: p.span(start.Start, implemented.Span().End)}, nil
}
```

- [ ] **Step 5: Wire generic constructors in `expr.go`**

In `compiler/parse/expr.go`, replace the identifier constructor branch in `parsePrimary` with a non-consuming type-ref lookahead. The parser must parse a `TypeRef` before the opening parenthesis only when the token sequence is either `Identifier(` or `Identifier<...>(`. Plain comparisons such as `a < b` must stay binary expressions.

Use this implementation shape:

```go
case lex.Identifier, lex.KeywordNever:
    if tok.Kind == lex.Identifier && p.peek().Kind == lex.Less && !p.looksLikeGenericConstructor() {
        return &ast.NameExpr{Name: tok.Text, SpanV: p.span(tok.Start, tok.End)}, nil
    }
    if tok.Kind == lex.Identifier && p.peek().Kind != lex.LParen && p.peek().Kind != lex.Less {
        return &ast.NameExpr{Name: tok.Text, SpanV: p.span(tok.Start, tok.End)}, nil
    }
    p.idx-- // put tok back so parseTypeRef consumes the same token
    typ, ds := p.parseTypeRef()
    if len(ds) != 0 {
        return nil, ds
    }
    if p.match(lex.LParen) {
        args, ds := p.parseNamedArgs()
        if len(ds) != 0 {
            return nil, ds
        }
        close, ds := p.consume(lex.RParen)
        if len(ds) != 0 {
            return nil, ds
        }
        return &ast.ConstructorExpr{
            Type:  typ,
            Args:  args,
            SpanV: p.span(tok.Start, close.End),
        }, nil
    }
    if len(typ.Args) != 0 {
        return nil, p.err(tok, diag.PAR0001, "generic type arguments are only valid in constructor or type positions")
    }
    return &ast.NameExpr{Name: typ.Name, SpanV: p.span(tok.Start, tok.End)}, nil
```

Add this helper to `compiler/parse/expr.go` or `compiler/parse/parser.go` near the other parser helpers:

```go
func (p *Parser) looksLikeGenericConstructor() bool {
    if p.peek().Kind != lex.Less {
        return false
    }
    depth := 0
    for i := p.idx; i < len(p.toks); i++ {
        switch p.toks[i].Kind {
        case lex.Less:
            depth++
        case lex.Greater:
            depth--
            if depth == 0 {
                return i+1 < len(p.toks) && p.toks[i+1].Kind == lex.LParen
            }
        case lex.ShiftRight:
            depth--
            if depth == 0 {
                return i+1 < len(p.toks) && p.toks[i+1].Kind == lex.LParen
            }
            depth--
            if depth == 0 {
                return i+1 < len(p.toks) && p.toks[i+1].Kind == lex.LParen
            }
            if depth < 0 {
                return false
            }
        case lex.EOF, lex.Newline, lex.Semicolon, lex.RBrace:
            return false
        }
    }
    return false
}
```

This branch deliberately keeps `NameExpr` non-generic. Type operands such as `reserve_array(Event, count = 4)` are handled by the `reserve_array` semantic intrinsic, not by creating runtime values.

- [ ] **Step 6: Verify**

Run:

```bash
go test ./compiler/parse -run 'TestParseGenericDeclsAndTypes|TestParseDecls|TestParseStatements' -v
go test ./compiler/parse -run 'TestParseGenericConstructorExpression|TestGenericConstructorLookaheadDoesNotStealComparison' -v
git diff --check
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add compiler/parse/parser.go compiler/parse/expr.go compiler/parse/parser_test.go compiler/parse/expr_test.go compiler/ast/ast.go compiler/ast/ast_test.go
git commit -m "feat: parse generic type expressions and declarations -Codex Automated"
```

### Task 4: Parse Enums, Consts, Patterns, And Match

**Description:** Complete parser support for the rest of the language surface.

**Files:**
- Modify: `compiler/parse/parser.go`
- Modify: `compiler/parse/expr.go`
- Modify: `compiler/parse/parser_test.go`
- Modify: `compiler/ast/ast.go`

**Acceptance Criteria:**
- `enum Option<T>`, `const`, and `static_assert` parse as declarations.
- `sizeof(Type)` and `alignof(Type)` parse as dedicated expressions.
- `Option.Some(value = event)` parses as `VariantConstructorExpr`.
- `if let` and `match` parse into dedicated statements with patterns and scoped arm bodies.

- [ ] **Step 1: Add failing parser tests**

Add to `compiler/parse/parser_test.go`:

```go
func TestParseEnumsConstsAndMatches(t *testing.T) {
    mod, ds := parseModuleForTest(t, `
module parser.enums

enum Option<T> {
    None
    Some(value: T)
}

const EVENT_CAPACITY: U64 = 128
const EVENT_BYTES: U64 = sizeof(Event) * EVENT_CAPACITY
static_assert(EVENT_BYTES <= 4096, message = "event frame exceeds one page")

data Event { kind: U64 }

class Worker {
    rx: Subscription<Event>
    fn run(self) {
        if let Option.Some(value = event) = self.rx.try_next() {
            self.rx.arm_wait()
        }
        match self.rx.try_next() {
            Option.Some(value = event) => {
                self.rx.arm_wait()
            }
            Option.None => {
                self.rx.arm_wait()
            }
        }
    }
}
`)
    if len(ds) != 0 {
        t.Fatalf("diagnostics: %#v", ds)
    }
    if _, ok := mod.Decls[0].(*ast.EnumDecl); !ok {
        t.Fatalf("decl0 = %T, want enum", mod.Decls[0])
    }
    if _, ok := mod.Decls[1].(*ast.ConstDecl); !ok {
        t.Fatalf("decl1 = %T, want const", mod.Decls[1])
    }
    if _, ok := mod.Decls[3].(*ast.StaticAssertDecl); !ok {
        t.Fatalf("decl3 = %T, want static assert", mod.Decls[3])
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./compiler/parse -run TestParseEnumsConstsAndMatches -v`

Expected: FAIL because enum/const/match syntax is not parsed.

- [ ] **Step 3: Add enum, const, and static assertion parsing**

Add `parseEnumDecl`, `parseConstDecl`, and `parseStaticAssertDecl`. Use this exact enum shape:

Wire them explicitly in `parseDecl` before the default error path:

```go
case lex.KeywordEnum:
    return p.parseEnumDecl()
case lex.KeywordConst:
    return p.parseConstDecl()
case lex.KeywordStaticAssert:
    return p.parseStaticAssertDecl()
```

```go
func (p *Parser) parseEnumDecl() (ast.Decl, []diag.Diagnostic) {
    start := p.next() // enum
    name, ds := p.expectIdentifier("expected enum name")
    if len(ds) != 0 {
        return nil, ds
    }
    typeParams, ds := p.parseTypeParams()
    if len(ds) != 0 {
        return nil, ds
    }
    if _, ds := p.consume(lex.LBrace); len(ds) != 0 {
        return nil, ds
    }
    var variants []ast.EnumVariant
    for p.peek().Kind != lex.RBrace && p.peek().Kind != lex.EOF {
        p.skipSeparators()
        if p.peek().Kind == lex.RBrace {
            break
        }
        variantStart := p.peek().Start
        variantName, ds := p.expectIdentifier("expected enum variant")
        if len(ds) != 0 {
            return nil, ds
        }
        var fields []ast.Field
        if p.match(lex.LParen) {
            fields, ds = p.parseFieldListUntil(lex.RParen)
            if len(ds) != 0 {
                return nil, ds
            }
        }
        variants = append(variants, ast.EnumVariant{Name: variantName.Text, Fields: fields, Span: p.span(variantStart, p.previous().End)})
        p.skipSeparators()
        p.match(lex.Comma)
    }
    if _, ds := p.consume(lex.RBrace); len(ds) != 0 {
        return nil, ds
    }
    return &ast.EnumDecl{Name: name.Text, TypeParams: typeParams, Variants: variants, SpanV: p.span(start.Start, p.previous().End)}, nil
}
```

Add the helper used above:

```go
func (p *Parser) parseFieldListUntil(end lex.Kind) ([]ast.Field, []diag.Diagnostic) {
    var fields []ast.Field
    p.skipSeparators()
    if p.peek().Kind == end {
        p.next()
        return fields, nil
    }
    for {
        name, ds := p.expectIdentifier("expected field name")
        if len(ds) != 0 {
            return nil, ds
        }
        if _, ds := p.consume(lex.Colon); len(ds) != 0 {
            return nil, ds
        }
        typ, ds := p.parseTypeRef()
        if len(ds) != 0 {
            return nil, ds
        }
        fields = append(fields, ast.Field{Name: name.Text, Type: typ, Span: p.span(name.Start, typ.Span().End)})
        p.skipSeparators()
        if !p.match(lex.Comma) {
            break
        }
        p.skipSeparators()
    }
    if _, ds := p.consume(end); len(ds) != 0 {
        return nil, ds
    }
    return fields, nil
}
```

For const and static assertions, require these forms:

```wrela
const PAGE_SIZE: U64 = 4096
static_assert(PAGE_SIZE == 4096, message = "page size fixed")
```

Implement them with this parser shape:

```go
func (p *Parser) parseConstDecl() (ast.Decl, []diag.Diagnostic) {
    start := p.next() // const
    name, ds := p.expectIdentifier("expected const name")
    if len(ds) != 0 {
        return nil, ds
    }
    if _, ds := p.consume(lex.Colon); len(ds) != 0 {
        return nil, ds
    }
    typ, ds := p.parseTypeRef()
    if len(ds) != 0 {
        return nil, ds
    }
    if _, ds := p.consume(lex.Equal); len(ds) != 0 {
        return nil, ds
    }
    value, ds := p.parseExpr(0)
    if len(ds) != 0 {
        return nil, ds
    }
    return &ast.ConstDecl{Name: name.Text, Type: typ, Value: value, SpanV: p.span(start.Start, value.Span().End)}, nil
}

func (p *Parser) parseStaticAssertDecl() (ast.Decl, []diag.Diagnostic) {
    start := p.next() // static_assert
    if _, ds := p.consume(lex.LParen); len(ds) != 0 {
        return nil, ds
    }
    expr, ds := p.parseExpr(0)
    if len(ds) != 0 {
        return nil, ds
    }
    if _, ds := p.consume(lex.Comma); len(ds) != 0 {
        return nil, ds
    }
    name, ds := p.expectIdentifier("expected message argument")
    if len(ds) != 0 {
        return nil, ds
    }
    if name.Text != "message" {
        return nil, p.err(name, diag.PAR0001, "expected message argument")
    }
    if _, ds := p.consume(lex.Equal); len(ds) != 0 {
        return nil, ds
    }
    message := p.nextIf(lex.String)
    if message.Kind != lex.String {
        return nil, p.err(message, diag.PAR0001, "static_assert message must be a string literal")
    }
    text, err := strconv.Unquote(message.Text)
    if err != nil {
        return nil, p.err(message, diag.PAR0001, "invalid static_assert message")
    }
    if _, ds := p.consume(lex.RParen); len(ds) != 0 {
        return nil, ds
    }
    return &ast.StaticAssertDecl{Expr: expr, Message: text, SpanV: p.span(start.Start, p.previous().End)}, nil
}
```

Add `strconv` to the `compiler/parse/parser.go` imports for `strconv.Unquote`.

- [ ] **Step 4: Add pattern and match parsing**

Add cases to `parseStmt`:

```go
case lex.KeywordIf:
    if p.peekN(1).Kind == lex.KeywordLet {
        return p.parseIfLetStmt()
    }
    return p.parseIfStmt()
case lex.KeywordMatch:
    return p.parseMatchStmt()
```

Add the missing statement parsers in full:

```go
func (p *Parser) parseIfLetStmt() (ast.Stmt, []diag.Diagnostic) {
    start := p.next() // if
    if _, ds := p.consume(lex.KeywordLet); len(ds) != 0 {
        return nil, ds
    }
    pattern, ds := p.parsePattern()
    if len(ds) != 0 {
        return nil, ds
    }
    if _, ds := p.consume(lex.Equal); len(ds) != 0 {
        return nil, ds
    }
    value, ds := p.parseExpr(0)
    if len(ds) != 0 {
        return nil, ds
    }
    body, ds := p.parseBlockStmts()
    if len(ds) != 0 {
        return nil, ds
    }
    return &ast.IfLetStmt{Pattern: pattern, Value: value, Body: body, SpanV: p.span(start.Start, p.previous().End)}, nil
}

func (p *Parser) parseMatchStmt() (ast.Stmt, []diag.Diagnostic) {
    start := p.next() // match
    value, ds := p.parseExpr(0)
    if len(ds) != 0 {
        return nil, ds
    }
    if _, ds := p.consume(lex.LBrace); len(ds) != 0 {
        return nil, ds
    }
    var arms []ast.MatchArm
    for {
        p.skipSeparators()
        if p.peek().Kind == lex.RBrace {
            p.next()
            break
        }
        if p.peek().Kind == lex.EOF {
            return nil, p.err(p.peek(), diag.PAR0001, "unterminated match")
        }
        armStart := p.peek().Start
        pattern, ds := p.parsePattern()
        if len(ds) != 0 {
            return nil, ds
        }
        if _, ds := p.consume(lex.FatArrow); len(ds) != 0 {
            return nil, p.err(p.peek(), diag.PAR0001, "expected '=>' after match pattern")
        }
        body, ds := p.parseBlockStmts()
        if len(ds) != 0 {
            return nil, ds
        }
        arms = append(arms, ast.MatchArm{Pattern: pattern, Body: body, Span: p.span(armStart, p.previous().End)})
    }
    return &ast.MatchStmt{Value: value, Arms: arms, SpanV: p.span(start.Start, p.previous().End)}, nil
}
```

Add parser helpers:

```go
func (p *Parser) parsePattern() (ast.Pattern, []diag.Diagnostic) {
    if p.peek().Kind == lex.Identifier && p.peek().Text == "_" {
        p.next()
        return ast.WildcardPattern{}, nil
    }
    enumTok, ds := p.expectIdentifier("expected enum name in pattern")
    if len(ds) != 0 {
        return nil, ds
    }
    if _, ds := p.consume(lex.Dot); len(ds) != 0 {
        return nil, p.err(p.peek(), diag.PAR0001, "expected enum variant pattern")
    }
    variantTok, ds := p.expectIdentifier("expected enum variant name")
    if len(ds) != 0 {
        return nil, ds
    }
    pattern := ast.VariantPattern{Enum: enumTok.Text, Variant: variantTok.Text}
    if p.match(lex.LParen) {
        for {
            name, ds := p.expectIdentifier("expected pattern field name")
            if len(ds) != 0 {
                return nil, ds
            }
            if _, ds := p.consume(lex.Equal); len(ds) != 0 {
                return nil, ds
            }
            bind, ds := p.expectIdentifier("expected pattern binding name")
            if len(ds) != 0 {
                return nil, ds
            }
            pattern.Bindings = append(pattern.Bindings, ast.PatternBinding{Name: name.Text, Bind: bind.Text})
            if !p.match(lex.Comma) {
                break
            }
        }
        if _, ds := p.consume(lex.RParen); len(ds) != 0 {
            return nil, ds
        }
    }
    return pattern, nil
}
```

- [ ] **Step 5: Add sizeof/alignof and variant constructor parsing**

In primary expression parsing, add:

```go
case lex.KeywordSizeof:
    if _, ds := p.consume(lex.LParen); len(ds) != 0 {
        return nil, ds
    }
    typ, ds := p.parseTypeRef()
    if len(ds) != 0 {
        return nil, ds
    }
    if _, ds := p.consume(lex.RParen); len(ds) != 0 {
        return nil, ds
    }
    return &ast.SizeOfExpr{Type: typ, SpanV: p.span(tok.Start, p.previous().End)}, nil
case lex.KeywordAlignof:
    if _, ds := p.consume(lex.LParen); len(ds) != 0 {
        return nil, ds
    }
    typ, ds := p.parseTypeRef()
    if len(ds) != 0 {
        return nil, ds
    }
    if _, ds := p.consume(lex.RParen); len(ds) != 0 {
        return nil, ds
    }
    return &ast.AlignOfExpr{Type: typ, SpanV: p.span(tok.Start, p.previous().End)}, nil
```

When parsing a dotted call with a capitalized receiver and capitalized method, produce a `VariantConstructorExpr`:

```go
if recv, ok := expr.(*ast.NameExpr); ok && startsUpper(recv.Name) && startsUpper(method.Text) {
    expr = &ast.VariantConstructorExpr{Enum: recv.Name, Variant: method.Text, Args: args, SpanV: p.span(recv.Span().Start, p.previous().End)}
} else {
    expr = &ast.CallExpr{Receiver: expr, Method: method.Text, Args: args, SpanV: p.span(expr.Span().Start, p.previous().End)}
}
```

Add the helper:

```go
func startsUpper(s string) bool {
    if s == "" {
        return false
    }
    r := rune(s[0])
    return r >= 'A' && r <= 'Z'
}
```

- [ ] **Step 6: Verify**

Run:

```bash
go test ./compiler/parse -run 'TestParseEnumsConstsAndMatches|TestParseGenericDeclsAndTypes' -v
go test ./compiler/lex -run TestFatArrowToken -v
go test ./compiler/parse -v
git diff --check
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add compiler/parse/parser.go compiler/parse/expr.go compiler/parse/parser_test.go compiler/parse/expr_test.go compiler/ast/ast.go
git commit -m "feat: parse enums consts patterns and matches -Codex Automated"
```

### Task 4.5: Core Module And Test Helper Inventory

**Description:** Add the core source definitions and shared test helpers before semantic tasks reference `Option<T>`, `Result<T, E>`, or shared compiler test helpers. This prevents false failures from missing images or missing test scaffolding.

**Files:**
- Create: `wrela/lang/core.wrela`
- Create: `compiler/parse/core_source_test.go`
- Modify: `compiler/sem/testutil_test.go`
- Modify: `compiler/ir/convergence_testutil_test.go`

**Acceptance Criteria:**
- `wrela.lang.core` exists before semantic generic/enum/trait tests run.
- `wrela/lang/core.wrela` parses before any semantic loader imports it.
- Semantic unit tests that intentionally do not declare an image can use a helper that tolerates only SEM0004.
- IR tests have one local `lowerSourceForTest` helper that accepts image-free single-module snippets and use existing `findFunction`, `containsOp`, and `functionCalls`.
- `parseUEFIModuleSet` is not changed in this task; Task 14 adds core to the full source loader after enum, trait, and generic semantic support exists.

- [ ] **Step 1: Add core source file**

Create `wrela/lang/core.wrela`:

```wrela
module wrela.lang.core

data Unit {}

enum Option<T> {
    None
    Some(value: T)
}

enum Result<T, E> {
    Ok(value: T)
    Err(error: E)
}

trait Publisher<T> {
    fn publish(self, value: T)
}

trait Subscription<T> {
    fn try_next(self) -> Option<T>
    fn arm_wait(self)
    fn is_wait_armed(self) -> Bool
}
```

- [ ] **Step 2: Add parser smoke test for core source**

Add a parser smoke test in `compiler/parse/core_source_test.go`:

```go
package parse

import (
    "os"
    "path/filepath"
    "testing"

    "github.com/ryanwible/wrela3/compiler/source"
)

func TestCoreLanguageSourceParses(t *testing.T) {
    repoRoot := repoRootForParseTest(t)
    path := filepath.Join(repoRoot, "wrela/lang/core.wrela")
    raw, err := os.ReadFile(path)
    if err != nil {
        t.Fatalf("read core source: %v", err)
    }
    modules, ds := ParseGraph(source.Graph{Files: []*source.File{
        source.NewFile(1, path, string(raw)),
    }})
    if len(ds) != 0 {
        t.Fatalf("parse diagnostics: %#v", ds)
    }
    if len(modules) != 1 || modules[0].Name != "wrela.lang.core" {
        t.Fatalf("modules = %#v, want wrela.lang.core", modules)
    }
}
```

Add this helper in the same file:

```go
func repoRootForParseTest(t *testing.T) string {
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

- [ ] **Step 3: Add semantic helper for image-free unit tests**

Add to `compiler/sem/testutil_test.go`:

```go
func mustBuildIndexAllowingMissingImage(t *testing.T, modules []*ast.Module) *Index {
    t.Helper()
    index, ds := BuildIndex(modules)
    filtered := ds[:0]
    for _, d := range ds {
        if d.Code == diag.SEM0004 {
            continue
        }
        filtered = append(filtered, d)
    }
    if len(filtered) != 0 {
        t.Fatalf("index diagnostics: %#v", filtered)
    }
    return index
}

func checkAllowingMissingImage(t *testing.T, index *Index, modules []*ast.Module) (*CheckedProgram, []diag.Diagnostic) {
    t.Helper()
    checked, ds := Check(index, modules)
    filtered := ds[:0]
    for _, d := range ds {
        if d.Code == diag.SEM0004 {
            continue
        }
        filtered = append(filtered, d)
    }
    return checked, filtered
}
```

Use this helper only for compiler unit tests whose source intentionally omits an `image`. Full source-shape tests and integration tests must still use `mustBuildIndex`.

- [ ] **Step 4: Add IR lower-source helper**

In `compiler/ir/convergence_testutil_test.go`, add:

```go
func lowerSourceForTest(t *testing.T, sourceText string) *Program {
    t.Helper()
    checked := checkedProgramForTest(t, sourceText)
    program, ds := Lower(checked)
    if len(ds) != 0 {
        t.Fatalf("lower diagnostics: %#v", ds)
    }
    return program
}
```

The current repository already defines `findFunction`, `containsOp`, and `functionCalls` in `compiler/ir/lower_test.go`. Reuse those exact helpers. Do not add duplicate helper names.

- [ ] **Step 5: Verify**

Run:

```bash
go test ./compiler/sem -run TestSourceTypes -v
go test ./compiler/parse -run TestCoreLanguageSourceParses -v
go test ./compiler/ir -run TestLowerReturnsCG0001ForNilProgram -v
git diff --check
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add wrela/lang/core.wrela compiler/parse/core_source_test.go compiler/sem/testutil_test.go compiler/ir/convergence_testutil_test.go
git commit -m "test: add core module and shared language test helpers -Codex Automated"
```

---

## 6. Phase 2: Semantic Index, Generics, Consts, Traits, And Enums

**Description:** This phase makes the new syntax meaningful. It indexes generic declarations, resolves concrete instantiations, evaluates constants, enforces static traits, and validates enum construction/pattern matching.

**Phase Acceptance Criteria:**

- Generic arity, duplicate type parameters, and unknown type arguments report stable diagnostics.
- Constants, `sizeof`, `alignof`, and `static_assert` evaluate during semantic checking.
- Traits and impls are checked statically and trait-constrained calls type-check against bound methods.
- Enum constructors, if-let, and match validate payload bindings and exhaustiveness.
- Phase smoke command after Task 9: `go test ./compiler/sem -run 'TestGeneric|TestConst|TestTrait|TestEnum|TestReserveArray|TestSlots' -v`.

**Phase Code Example:**

```wrela
class Drain<S, T> where S: Subscription<T> {
    input: S
    fn poll(self) -> Option<T> {
        return self.input.try_next()
    }
}
```

### Task 5A: Index TypeRefs And Generic Declaration Contracts

**Description:** Extend the semantic type system from plain names to structured generic types and validate generic declaration contracts. This task does not clone method bodies yet; it creates the data structures and resolver that later monomorphization tasks use.

**Files:**
- Modify: `compiler/sem/types.go`
- Modify: `compiler/sem/symbols.go`
- Create: `compiler/sem/generic.go`
- Create: `compiler/sem/generic_test.go`
- Modify: `compiler/sem/symbols_test.go`

**Acceptance Criteria:**
- `Index.LookupTypeRef(module, ast.TypeRef{Name:"Topic", Args: []ast.TypeRef{{Name:"TimerTickPayload"}}})` returns a concrete `*Type`.
- Duplicate type parameters emit SEM0076.
- Arity mismatches emit SEM0077.
- Unknown type arguments emit SEM0078.
- Instantiation keys are deterministic and fully qualified.

- [ ] **Step 1: Add failing tests**

Create `compiler/sem/generic_test.go`:

```go
package sem

import (
    "testing"

    "github.com/ryanwible/wrela3/compiler/diag"
)

func TestGenericInstantiationKeyIsDeterministic(t *testing.T) {
    modules := parseModulesForTest(t, `
module sem.generics
data Payload { value: U64 }
data Box<T> { value: T }
data UsesBox { box: Box<Payload> }
`)
    index := mustBuildIndexAllowingMissingImage(t, modules)
    typ, ok := index.Lookup("sem.generics", "UsesBox")
    if !ok {
        t.Fatal("UsesBox not indexed")
    }
    got := typ.Fields[0].Type.Key()
    want := "sem.generics.Box[sem.generics.Payload]"
    if got != want {
        t.Fatalf("field type key = %q, want %q", got, want)
    }
}

func TestGenericArityMismatch(t *testing.T) {
    modules := parseModulesForTest(t, `
module sem.generics
data Box<T> { value: T }
data Bad { value: Box<U64, U64> }
`)
    index, indexDiags := BuildIndex(modules)
    _, checkDiags := Check(index, modules)
    ds := append(indexDiags, checkDiags...)
    if !hasCode(ds, diag.SEM0077) {
        t.Fatalf("diagnostics = %#v, want SEM0077", ds)
    }
}

func TestDuplicateGenericTypeParameterDiagnostic(t *testing.T) {
    modules := parseModulesForTest(t, `
module sem.generics
data Bad<T, T> { value: T }
`)
    index, indexDiags := BuildIndex(modules)
    _, checkDiags := Check(index, modules)
    ds := append(indexDiags, checkDiags...)
    if !hasCode(ds, diag.SEM0076) {
        t.Fatalf("diagnostics = %#v, want SEM0076", ds)
    }
}

func TestUnknownGenericTypeArgumentDiagnostic(t *testing.T) {
    modules := parseModulesForTest(t, `
module sem.generics
data Box<T> { value: T }
data Bad { value: Box<MissingPayload> }
`)
    index, indexDiags := BuildIndex(modules)
    _, checkDiags := Check(index, modules)
    ds := append(indexDiags, checkDiags...)
    if !hasCode(ds, diag.SEM0078) {
        t.Fatalf("diagnostics = %#v, want SEM0078", ds)
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./compiler/sem -run 'TestGenericInstantiationKeyIsDeterministic|TestGenericArityMismatch|TestDuplicateGenericTypeParameterDiagnostic|TestUnknownGenericTypeArgumentDiagnostic' -v`

Expected: FAIL because `Type.Key` and generic lookup do not exist.

- [ ] **Step 3: Extend semantic types**

In `compiler/sem/types.go`, update `Field`, `Method`, and `Type`:

```go
type Field struct {
    Name string
    Type *Type
    Span source.Span
}

type TypeParam struct {
    Name string
    Span source.Span
}

type TraitBound struct {
    Param string
    Trait *Type
    Span  source.Span
}

type Type struct {
    Module        string
    Name          string
    Kind          Kind
    Unique        bool
    DelegatedOnly bool
    Fields        []Field
    Methods       []Method
    TypeParams    []TypeParam
    TypeArgs      []*Type
    Where         []TraitBound
    GenericOrigin *Type
    InstantiationComplete bool
}

func (t *Type) Key() string {
    if t == nil {
        return ""
    }
    base := qualifiedTypeName(t)
    if len(t.TypeArgs) == 0 {
        return base
    }
    parts := make([]string, 0, len(t.TypeArgs))
    for _, arg := range t.TypeArgs {
        parts = append(parts, arg.Key())
    }
    return base + "[" + strings.Join(parts, ",") + "]"
}

func (t *Type) Display() string {
    if t == nil {
        return ""
    }
    if len(t.TypeArgs) == 0 {
        return t.Name
    }
    parts := make([]string, 0, len(t.TypeArgs))
    for _, arg := range t.TypeArgs {
        parts = append(parts, arg.Display())
    }
    return t.Name + "<" + strings.Join(parts, ", ") + ">"
}
```

- [ ] **Step 4: Add generic resolver**

Create `compiler/sem/generic.go` with:

```go
func (idx *Index) LookupTypeRef(moduleName string, ref ast.TypeRef, params map[string]*Type) (*Type, []diag.Diagnostic) {
    if params != nil {
        if typ := params[ref.Name]; typ != nil {
            if len(ref.Args) != 0 {
                return nil, []diag.Diagnostic{{Phase: "sem", Code: diag.SEM0077, Severity: diag.Error, Start: ref.Span().Start, End: ref.Span().End, Message: "type parameter "+ref.Name+" does not take type arguments"}}
            }
            return typ, nil
        }
    }
    base, ok := idx.lookupBaseType(moduleName, ref.Name)
    if !ok {
        return nil, []diag.Diagnostic{{Phase: "sem", Code: diag.SEM0078, Severity: diag.Error, Start: ref.Span().Start, End: ref.Span().End, Message: "unknown type "+ref.Name}}
    }
    if len(base.TypeParams) != len(ref.Args) {
        return nil, []diag.Diagnostic{{Phase: "sem", Code: diag.SEM0077, Severity: diag.Error, Start: ref.Span().Start, End: ref.Span().End, Message: base.Name+" expects "+strconv.Itoa(len(base.TypeParams))+" type arguments"}}
    }
    if len(ref.Args) == 0 {
        return base, nil
    }
    args := make([]*Type, 0, len(ref.Args))
    for _, argRef := range ref.Args {
        arg, ds := idx.LookupTypeRef(moduleName, argRef, params)
        if len(ds) != 0 {
            return nil, ds
        }
        args = append(args, arg)
    }
    return idx.registerInstantiation(base, args), nil
}

func (idx *Index) lookupBaseType(moduleName, name string) (*Type, bool) {
    if typ, ok := idx.Lookup(moduleName, name); ok {
        return typ, true
    }
    if typ := idx.resolveInScope(moduleName, name); typ != nil {
        return typ, true
    }
    return nil, false
}
```

`Index.Lookup` already sees primitive names in the current repository, but keep `lookupBaseType` as the single resolver entry point for generic parsing. It prevents future drift between primitive lookup, local lookup, and imported type lookup.

Update `buildFields` and `buildMethods` in `compiler/sem/symbols.go` to return diagnostics instead of silently manufacturing pseudo-types when a structured `TypeRef` fails:

```go
func buildFields(idx *Index, moduleName string, fields []ast.Field, params map[string]*Type) ([]Field, []diag.Diagnostic) {
    out := make([]Field, 0, len(fields))
    var ds []diag.Diagnostic
    for _, field := range fields {
        typ, fieldDs := idx.LookupTypeRef(moduleName, field.Type, params)
        ds = append(ds, fieldDs...)
        if typ == nil {
            continue
        }
        out = append(out, Field{Name: field.Name, Type: typ, Span: field.Span})
    }
    return out, ds
}

func buildMethods(idx *Index, moduleName string, methods []ast.MethodDecl, params map[string]*Type) ([]Method, []diag.Diagnostic) {
    out := make([]Method, 0, len(methods))
    var ds []diag.Diagnostic
    for _, method := range methods {
        built := Method{Name: method.Name, IsAsm: method.IsAsm, IsStart: method.IsStart, Span: method.SpanV, Body: method.Body, AsmBody: method.Asm}
        methodParams, paramDs := buildParams(idx, moduleName, method.Params, params)
        ds = append(ds, paramDs...)
        built.Params = methodParams
        if method.Return.Name == "" {
            built.Return = idx.MustType("void")
        } else {
            ret, retDs := idx.LookupTypeRef(moduleName, method.Return, params)
            ds = append(ds, retDs...)
            built.Return = ret
        }
        out = append(out, built)
    }
    return out, ds
}

func buildParams(idx *Index, moduleName string, paramsIn []ast.Param, params map[string]*Type) ([]Field, []diag.Diagnostic) {
    out := make([]Field, 0, len(paramsIn))
    var ds []diag.Diagnostic
    for _, param := range paramsIn {
        if param.Name == "self" && param.Type.Name == "" {
            out = append(out, Field{Name: "self", Type: nil, Span: param.Span})
            continue
        }
        typ, paramDs := idx.LookupTypeRef(moduleName, param.Type, params)
        ds = append(ds, paramDs...)
        if typ == nil {
            continue
        }
        out = append(out, Field{Name: param.Name, Type: typ, Span: param.Span})
    }
    return out, ds
}
```

Every `BuildIndex` call site that previously assigned `typ.Fields = buildFields(...)` must now append the returned diagnostics. Generic declarations pass a `params` map containing their type parameters; non-generic declarations pass `nil`.

When indexing a generic declaration, build the params map and reject duplicates before fields or methods are resolved:

```go
func buildTypeParamMap(params []ast.TypeParam) (map[string]*Type, []diag.Diagnostic) {
    out := map[string]*Type{}
    var ds []diag.Diagnostic
    for _, param := range params {
        if out[param.Name] != nil {
            ds = append(ds, diag.Diagnostic{Phase: "sem", Code: diag.SEM0076, Severity: diag.Error, Start: param.Span.Start, End: param.Span.End, Message: "duplicate type parameter "+param.Name})
            continue
        }
        out[param.Name] = &Type{Name: param.Name, Kind: KindTypeParam}
    }
    return out, ds
}
```

Add `KindTypeParam` to `Kind`; it is only a resolver placeholder and must never appear in final IR type info.

`registerInstantiation` creates or returns an empty concrete type shell. It does not fill fields or methods in this task:

```go
func (idx *Index) registerInstantiation(base *Type, args []*Type) *Type {
    if idx.Instantiations == nil {
        idx.Instantiations = map[string]*Type{}
    }
    concrete := &Type{
        Module:        base.Module,
        Name:          base.Name,
        Kind:          base.Kind,
        Unique:        base.Unique,
        DelegatedOnly: base.DelegatedOnly,
        TypeArgs:      append([]*Type(nil), args...),
        GenericOrigin: base,
    }
    key := concrete.Key()
    if existing := idx.Instantiations[key]; existing != nil {
        return existing
    }
    idx.Instantiations[key] = concrete
    idx.InstantiationOrder = append(idx.InstantiationOrder, key)
    if idx.ByModule[base.Module] == nil {
        idx.ByModule[base.Module] = map[string]*Type{}
    }
    idx.ByModule[base.Module][concrete.Display()] = concrete
    return concrete
}
```

Add to `Index`:

```go
Instantiations map[string]*Type
InstantiationOrder []string
```

The snippet above appends the key to `InstantiationOrder` only on first registration. Later tasks fill registered instantiations in sorted deterministic order.

Also add this convenience wrapper because later memory and topic tasks use it:

```go
func (idx *Index) instantiateByName(moduleName, name string, args []*Type) *Type {
    if moduleName == "" {
        return nil
    }
    base, ok := idx.Lookup(moduleName, name)
    if !ok {
        return nil
    }
    if len(args) == 0 {
        return base
    }
    return idx.registerInstantiation(base, args)
}
```

`instantiateByName` is only for named generic declarations in a known module. Do not call it for primitives or with a generic display string such as `"Slots<Event>"`; structured references must go through `LookupTypeRef`.

- [ ] **Step 5: Verify**

Run:

```bash
go test ./compiler/sem -run 'TestGenericInstantiationKeyIsDeterministic|TestGenericArityMismatch|TestDuplicateGenericTypeParameterDiagnostic|TestUnknownGenericTypeArgumentDiagnostic|TestTypeIndex' -v
git diff --check
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add compiler/sem/types.go compiler/sem/symbols.go compiler/sem/symbols_test.go compiler/sem/generic.go compiler/sem/generic_test.go
git commit -m "feat: index generic type references and declarations -Codex Automated"
```

### Task 5B: Monomorphize Generic Data And Class Fields

**Description:** Fill registered generic instantiations with substituted concrete fields, trait bounds, and method signatures. This task keeps method bodies on the generic origin; Task 5C clones and lowers method bodies. Enum variants are deliberately not part of this task because `KindEnum` and enum metadata are introduced in Task 8.

**Files:**
- Modify: `compiler/sem/generic.go`
- Modify: `compiler/sem/generic_test.go`
- Modify: `compiler/sem/symbols.go`
- Modify: `compiler/sem/types.go`

**Acceptance Criteria:**
- `Box<Payload>` has field `value: Payload`, not `T`.
- Nested instantiations such as `Pair<Box<Event>>` substitute recursively.
- Recursive registration is cached by key and does not infinite-loop.
- Instantiations are completed in deterministic key order.

- [ ] **Step 1: Add failing tests**

Append to `compiler/sem/generic_test.go`:

```go
func TestGenericFieldSubstitutionIsRecursive(t *testing.T) {
    modules := parseModulesForTest(t, `
module sem.generics
data Event { kind: U64 }
data Box<T> { value: T }
data Pair<T> { first: T; second: T }
data Ring<T> { current: Box<T> }
data Root {
    box: Box<Event>
    ring: Ring<Event>
    pair: Pair<Box<Event>>
}
`)
    index := mustBuildIndexAllowingMissingImage(t, modules)
    mustCompleteGenericInstantiations(t, index)
    box := index.Instantiations["sem.generics.Box[sem.generics.Event]"]
    if box.Fields[0].Type.Key() != "sem.generics.Event" {
        t.Fatalf("Box<Event>.value = %s", box.Fields[0].Type.Key())
    }
    ring := index.Instantiations["sem.generics.Ring[sem.generics.Event]"]
    if ring.Fields[0].Type.Key() != "sem.generics.Box[sem.generics.Event]" {
        t.Fatalf("Ring<Event>.current = %s", ring.Fields[0].Type.Key())
    }
    pair := index.Instantiations["sem.generics.Pair[sem.generics.Box[sem.generics.Event]]"]
    if pair.Fields[0].Type.Key() != "sem.generics.Box[sem.generics.Event]" {
        t.Fatalf("Pair<Box<Event>>.first = %s", pair.Fields[0].Type.Key())
    }
}
```

- [ ] **Step 2: Implement substitution maps**

Add:

```go
type substitution map[string]*Type

func substitutionFor(base *Type, args []*Type) substitution {
    out := substitution{}
    for i, param := range base.TypeParams {
        out[param.Name] = args[i]
    }
    return out
}

func (idx *Index) substituteType(t *Type, subst substitution) *Type {
    if t == nil {
        return nil
    }
    if repl := subst[t.Name]; repl != nil && t.Module == "" && len(t.TypeArgs) == 0 {
        return repl
    }
    if len(t.TypeArgs) == 0 {
        return t
    }
    args := make([]*Type, 0, len(t.TypeArgs))
    for _, arg := range t.TypeArgs {
        args = append(args, idx.substituteType(arg, subst))
    }
    origin := t.GenericOrigin
    if origin == nil {
        origin = t
    }
    return idx.registerInstantiation(origin, args)
}
```

- [ ] **Step 3: Complete registered instantiations deterministically**

Add:

```go
func (idx *Index) CompleteGenericInstantiations() []diag.Diagnostic {
    var out []diag.Diagnostic
    for {
        before := len(idx.InstantiationOrder)
        keys := append([]string(nil), idx.InstantiationOrder...)
        sort.Strings(keys)
        for _, key := range keys {
            out = append(out, idx.completeInstantiation(key, map[string]bool{})...)
        }
        if len(idx.InstantiationOrder) == before {
            return out
        }
    }
}

func (idx *Index) completeInstantiation(key string, visiting map[string]bool) []diag.Diagnostic {
    var out []diag.Diagnostic
    concrete := idx.Instantiations[key]
    if concrete == nil || concrete.GenericOrigin == nil || concrete.InstantiationComplete || visiting[key] {
        return nil
    }
    visiting[key] = true
    base := concrete.GenericOrigin
    subst := substitutionFor(base, concrete.TypeArgs)
    concrete.Fields = substituteFields(idx, base.Fields, subst)
    concrete.Methods = substituteMethods(idx, base.Methods, subst, concrete)
    concrete.Where = substituteBounds(idx, base.Where, subst)
    for _, field := range concrete.Fields {
        if field.Type != nil && field.Type.GenericOrigin != nil {
            out = append(out, idx.completeInstantiation(field.Type.Key(), visiting)...)
        }
    }
    out = append(out, idx.checkConcreteBounds(concrete)...)
    concrete.InstantiationComplete = true
    return out
}

func (idx *Index) checkConcreteBounds(concrete *Type) []diag.Diagnostic {
    return nil
}
```

Call `CompleteGenericInstantiations` after all modules are indexed and before semantic body checking begins. Task 5B leaves `checkConcreteBounds` empty so field/signature monomorphization can land independently. Task 7 fills it with trait-impl diagnostics and uses the `[]diag.Diagnostic` return path already added here.

Add this local test helper in `compiler/sem/generic_test.go`:

```go
func mustCompleteGenericInstantiations(t *testing.T, index *Index) {
    t.Helper()
    if ds := index.CompleteGenericInstantiations(); len(ds) != 0 {
        t.Fatalf("generic instantiation diagnostics: %#v", ds)
    }
}
```

- [ ] **Step 4: Verify**

Run:

```bash
go test ./compiler/sem -run 'TestGenericFieldSubstitutionIsRecursive|TestGenericInstantiationKeyIsDeterministic' -v
git diff --check
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add compiler/sem/generic.go compiler/sem/generic_test.go compiler/sem/symbols.go compiler/sem/types.go
git commit -m "feat: monomorphize generic fields and signatures -Codex Automated"
```

### Task 5C: Clone Generic Method Bodies For Concrete Instantiations

**Description:** Make concrete generic methods executable by cloning the generic method body into a concrete semantic method context. This task proves the body exists and lowers for ordinary generic fields; Task 13 adds trait-bound call resolution inside those bodies.

**Files:**
- Modify: `compiler/sem/types.go`
- Modify: `compiler/sem/generic.go`
- Modify: `compiler/ir/lower.go`
- Create: `compiler/ir/generic_test.go`

**Acceptance Criteria:**
- Concrete instantiations keep `GenericOrigin` for diagnostics and hold substituted `Methods`.
- Each substituted method retains the original `Body` AST and has a `MonomorphizedOwner *Type`.
- IR lowering emits one function per concrete method actually referenced by source.
- Concrete method symbols use deterministic mangled type names.

- [ ] **Step 1: Add failing IR test**

Create `compiler/ir/generic_test.go`:

```go
func TestGenericMethodBodyIsLoweredForConcreteInstantiation(t *testing.T) {
    program := lowerSourceForTest(t, `
module ir.generics
data Event { kind: U64 }
class Holder<T> {
    value: T
    fn read(self) -> T {
        return self.value
    }
}
executor Worker {
    holder: Holder<Event>
    start fn run(self) -> never {
        let event = self.holder.read()
        while true {}
    }
}
`)
    fn := findFunction(program, "_wrela_method_ir_generics_Holder_Event_read")
    if fn == nil {
        t.Fatal("missing concrete Holder<Event>.read function")
    }
    if !containsOp[*FieldLoad](*fn) {
        t.Fatalf("read body did not lower the cloned field load: %#v", fn.Blocks)
    }
}
```

- [ ] **Step 2: Add method owner metadata**

In `compiler/sem/types.go`, extend `Method`:

```go
type Method struct {
    Name    string
    Params  []Field
    Return  *Type
    IsAsm   bool
    IsStart bool
    Span    source.Span
    Body    []ast.Stmt
    AsmBody *ast.AsmBody

    GenericOrigin      *Method
    MonomorphizedOwner *Type
}
```

- [ ] **Step 3: Substitute method bodies without rewriting AST**

When `substituteMethods` creates each concrete method, copy the original `Body` and `AsmBody`, set `GenericOrigin` to the original method, and set `MonomorphizedOwner` to the concrete type. Do not mutate the AST. Type substitution happens through the lower/checker context using the concrete owner and substituted parameter types.

```go
func substituteMethods(idx *Index, methods []Method, subst substitution, owner *Type) []Method {
    out := make([]Method, 0, len(methods))
    for i := range methods {
        m := methods[i]
        concrete := m
        concrete.Params = substituteFields(idx, m.Params, subst)
        concrete.Return = idx.substituteType(m.Return, subst)
        concrete.Body = append([]ast.Stmt(nil), m.Body...)
        concrete.GenericOrigin = &methods[i]
        concrete.MonomorphizedOwner = owner
        out = append(out, concrete)
    }
    return out
}
```

- [ ] **Step 4: Lower concrete methods on demand**

Add `MangledName()` to semantic `Type` before wiring call symbols:

```go
func (t *Type) MangledName() string {
    return strings.NewReplacer("<", "_", ">", "", ", ", "_", ".", "_", "[", "_", "]", "").Replace(t.Display())
}
```

Add `strings` to `compiler/sem/types.go` imports for this helper.

In `compiler/ir/lower.go`, when lowering source methods, include concrete instantiations whose methods are referenced from a call site. Use a deterministic worklist:

```go
type concreteMethodRef struct {
    Owner      *sem.Type
    MethodName string
}

type lowerContext struct {
    // existing fields...
    concreteMethodQueue  []concreteMethodRef
    queuedConcreteMethod map[string]bool
    emittedConcreteMethod map[string]bool
}
```

Initialize the maps in `newLowerContext`:

```go
ctx := &lowerContext{
    checked:               checked,
    program:               &Program{Types: map[string]TypeInfo{}},
    modules:               map[string]*ast.Module{},
    types:                 map[string]*sem.Type{},
    pseudo:                map[string]*sem.Type{},
    valueBindings:         map[Value]string{},
    queuedConcreteMethod:  map[string]bool{},
    emittedConcreteMethod: map[string]bool{},
}
```

Replace the legacy `resolveType(moduleName, raw string)` dependency for structured AST types before lowering concrete generic methods:

```go
func (ctx *lowerContext) resolveTypeRef(moduleName string, ref ast.TypeRef) *sem.Type {
    if ref.Name == "" {
        return ctx.resolveType(moduleName, "void")
    }
    typ, ds := ctx.checked.Index.LookupTypeRef(moduleName, ref, nil)
    if len(ds) != 0 || typ == nil {
        ctx.errorf("could not resolve type %s in %s", ref.String(), moduleName)
        return ctx.resolveType(moduleName, "void")
    }
    return typ
}
```

Migrate IR call sites that receive AST declarations to use `resolveTypeRef`:

```go
typ := ctx.resolveTypeRef(moduleName, param.Type)
ret := ctx.resolveTypeRef(moduleName, method.Return)
eventType := ctx.resolveTypeRef(moduleName, handler.ParamType)
placedType := ctx.resolveTypeRef(moduleName, cons.Type)
```

Keep `resolveType(moduleName, raw string)` only for primitive names, existing pseudo-types, and legacy generated helper types such as `"void"` and `"never"`. It must never receive a generic display string such as `"Slots<Event>"`; that path must use `resolveTypeRef` so `Index.LookupTypeRef` can register or reuse the concrete instantiation.

Add the enqueue helper:

```go
func (ctx *lowerContext) enqueueConcreteMethod(owner *sem.Type, methodName string) {
    key := owner.Key() + "." + methodName
    if ctx.queuedConcreteMethod[key] || ctx.emittedConcreteMethod[key] {
        return
    }
    ctx.queuedConcreteMethod[key] = true
    ctx.concreteMethodQueue = append(ctx.concreteMethodQueue, concreteMethodRef{Owner: owner, MethodName: methodName})
    sort.Slice(ctx.concreteMethodQueue, func(i, j int) bool {
        return ctx.concreteMethodQueue[i].Owner.Key()+"."+ctx.concreteMethodQueue[i].MethodName <
            ctx.concreteMethodQueue[j].Owner.Key()+"."+ctx.concreteMethodQueue[j].MethodName
    })
}
```

Drain the queue after `lowerSourceMethods` and before `lowerAsmMethods`:

```go
ctx.lowerSourceMethods()
ctx.lowerConcreteMethodQueue()
ctx.lowerImagePhases(imageModule, imageName, imageDecl, delegatedSymbol, ownedSymbol)
ctx.program.AsmMethods = append(ctx.program.AsmMethods, ctx.lowerAsmMethods()...)
```

Update `lowerSourceMethods` so it does not emit executable functions for generic origin types:

```go
if receiverType != nil && len(receiverType.TypeParams) != 0 {
    continue
}
```

Add the drain:

```go
func (ctx *lowerContext) lowerConcreteMethodQueue() {
    for len(ctx.concreteMethodQueue) != 0 {
        ref := ctx.concreteMethodQueue[0]
        ctx.concreteMethodQueue = ctx.concreteMethodQueue[1:]
        key := ref.Owner.Key() + "." + ref.MethodName
        if ctx.emittedConcreteMethod[key] {
            continue
        }
        method := semMethodByName(ref.Owner, ref.MethodName)
        if method == nil {
            ctx.errorf("missing concrete method %s.%s", ref.Owner.Display(), ref.MethodName)
            continue
        }
        symbol := symbolName("method", ref.Owner.Module, ref.Owner.MangledName(), ref.MethodName)
        ctx.program.Functions = append(ctx.program.Functions, ctx.lowerSemanticMethodWithSymbol(ref.Owner.Module, ref.Owner, method, symbol))
        ctx.emittedConcreteMethod[key] = true
    }
}

func semMethodByName(owner *sem.Type, name string) *sem.Method {
    for i := range owner.Methods {
        if owner.Methods[i].Name == name {
            return &owner.Methods[i]
        }
    }
    return nil
}
```

Add a semantic-method lowering entry point instead of trying to force `sem.Method` through the existing `*ast.MethodDecl` function:

```go
func (ctx *lowerContext) lowerSemanticMethodWithSymbol(moduleName string, receiverType *sem.Type, method *sem.Method, symbol string) Function {
    params := []Value{}
    scope := newLowerScope(nil)

    self := &Param{Symbol: "self", Type: ctx.irType(receiverType)}
    params = append(params, self)
    scope.define("self", lowerBinding{value: self, typ: receiverType})
    ctx.rememberValueBinding(self, "self")

    for _, param := range method.Params {
        if param.Name == "self" {
            continue
        }
        p := &Param{Symbol: param.Name, Type: ctx.irType(param.Type)}
        params = append(params, p)
        scope.define(param.Name, lowerBinding{value: p, typ: param.Type})
        ctx.rememberValueBinding(p, param.Name)
    }

    assigned := assignedNames(method.Body)
    ops := ctx.lowerStmtList(moduleName, receiverType, scope, assigned, method.Body)
    return Function{
        Symbol: symbol,
        Return: ctx.irType(method.Return),
        Params: params,
        Blocks: []Block{{Label: "entry", Ops: ops}},
    }
}
```

When a `CallExpr` resolves to a method on a concrete generic type, enqueue that method and emit a normal `ir.Call` to:

```go
owner := recvType
ctx.enqueueConcreteMethod(owner, e.Method)
symbol := symbolName("method", owner.Module, owner.MangledName(), e.Method)
```

Use this condition at the normal call-lowering site:

```go
if recvType != nil && recvType.GenericOrigin != nil {
    ctx.enqueueConcreteMethod(recvType, e.Method)
}
```

- [ ] **Step 5: Verify**

Run:

```bash
go test ./compiler/ir -run TestGenericMethodBodyIsLoweredForConcreteInstantiation -v
git diff --check
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add compiler/sem/types.go compiler/sem/generic.go compiler/ir/lower.go compiler/ir/generic_test.go
git commit -m "feat: lower monomorphized generic method bodies -Codex Automated"
```

### Task 6: Evaluate Constants, Sizeof, Alignof, And Static Assertions

**Description:** Add compile-time constant evaluation with checked arithmetic and layout queries.

**Files:**
- Create: `compiler/sem/const.go`
- Create: `compiler/sem/const_test.go`
- Modify: `compiler/sem/symbols.go`
- Modify: `compiler/sem/check.go`
- Modify: `compiler/sem/topic_payload.go`

**Acceptance Criteria:**
- Constants are indexed by module/import scope.
- Constants can reference earlier constants.
- `sizeof(Type)` and `alignof(Type)` use the same layout sizes as IR.
- Overflow emits SEM0086.
- Non-const operands emit SEM0087.
- Invalid layout operands emit SEM0088.
- Failing static assertions emit SEM0089 with the assertion message.

- [ ] **Step 1: Add failing tests**

Create `compiler/sem/const_test.go`:

```go
package sem

import (
    "testing"

    "github.com/ryanwible/wrela3/compiler/diag"
)

func TestConstSizeofAlignofAndStaticAssert(t *testing.T) {
    modules := parseModulesForTest(t, `
module sem.consts
data Event { kind: U64; ready: Bool }
const EVENT_CAPACITY: U64 = 128
const EVENT_BYTES: U64 = sizeof(Event) * EVENT_CAPACITY
const EVENT_ALIGN: U64 = alignof(Event)
static_assert(EVENT_BYTES == 2048, message = "event byte size")
static_assert(EVENT_ALIGN == 8, message = "event align")
`)
    index := mustBuildIndexAllowingMissingImage(t, modules)
    _, ds := checkAllowingMissingImage(t, index, modules)
    if len(ds) != 0 {
        t.Fatalf("semantic diagnostics: %#v", ds)
    }
    if got := index.ConstValue("sem.consts", "EVENT_BYTES"); got != 2048 {
        t.Fatalf("EVENT_BYTES = %d, want 2048", got)
    }
}

func TestConstOverflowDiagnostic(t *testing.T) {
    modules := parseModulesForTest(t, `
module sem.consts
const BAD: U64 = 18446744073709551615 + 1
`)
    index, indexDiags := BuildIndex(modules)
    _, checkDiags := Check(index, modules)
    ds := append(indexDiags, checkDiags...)
    if !hasCode(ds, diag.SEM0086) {
        t.Fatalf("diagnostics = %#v, want SEM0086", ds)
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./compiler/sem -run 'TestConstSizeofAlignofAndStaticAssert|TestConstOverflowDiagnostic' -v`

Expected: FAIL.

- [ ] **Step 3: Add const index and evaluator**

Add to `Index`:

```go
Consts map[string]map[string]ConstValue

type ConstValue struct {
    Type  *Type
    Value uint64
    Span  source.Span
}

func (idx *Index) ConstValue(moduleName, name string) uint64 {
    if idx == nil || idx.Consts[moduleName] == nil {
        return 0
    }
    return idx.Consts[moduleName][name].Value
}
```

Create `compiler/sem/const.go`:

```go
import (
    "math/bits"
    "strconv"
)

func (c *checker) evalConstExpr(moduleName string, expr ast.Expr, scope map[string]ConstValue) (uint64, []diag.Diagnostic) {
    switch e := expr.(type) {
    case *ast.IntLiteral:
        value, err := strconv.ParseUint(e.Value, 0, 64)
        if err != nil {
            return 0, []diag.Diagnostic{{Phase: "sem", Code: diag.SEM0086, Severity: diag.Error, Start: e.Span().Start, End: e.Span().End, Message: "const integer overflows U64"}}
        }
        return value, nil
    case *ast.NameExpr:
        if v, ok := scope[e.Name]; ok {
            return v.Value, nil
        }
        if v, ok := c.index.LookupConst(moduleName, e.Name); ok {
            return v.Value, nil
        }
        return 0, []diag.Diagnostic{{Phase: "sem", Code: diag.SEM0087, Severity: diag.Error, Start: e.Span().Start, End: e.Span().End, Message: "non-const operand "+e.Name}}
    case *ast.SizeOfExpr:
        typ, ds := c.index.LookupTypeRef(moduleName, e.Type, nil)
        if len(ds) != 0 {
            return 0, ds
        }
        size, _, ok := semanticSizeAlign(typ)
        if !ok {
            return 0, []diag.Diagnostic{{Phase: "sem", Code: diag.SEM0088, Severity: diag.Error, Start: e.Span().Start, End: e.Span().End, Message: "sizeof requires a sized type"}}
        }
        return size, nil
    case *ast.AlignOfExpr:
        typ, ds := c.index.LookupTypeRef(moduleName, e.Type, nil)
        if len(ds) != 0 {
            return 0, ds
        }
        _, align, ok := semanticSizeAlign(typ)
        if !ok {
            return 0, []diag.Diagnostic{{Phase: "sem", Code: diag.SEM0088, Severity: diag.Error, Start: e.Span().Start, End: e.Span().End, Message: "alignof requires a sized type"}}
        }
        return align, nil
    }
    return 0, []diag.Diagnostic{{Phase: "sem", Code: diag.SEM0087, Severity: diag.Error, Start: expr.Span().Start, End: expr.Span().End, Message: "expression is not constant"}}
}
```

Add `BinaryExpr` inside the same switch:

```go
case *ast.BinaryExpr:
    left, ds := c.evalConstExpr(moduleName, e.Left, scope)
    if len(ds) != 0 {
        return 0, ds
    }
    right, ds := c.evalConstExpr(moduleName, e.Right, scope)
    if len(ds) != 0 {
        return 0, ds
    }
    overflow := func() (uint64, []diag.Diagnostic) {
        return 0, []diag.Diagnostic{{Phase: "sem", Code: diag.SEM0086, Severity: diag.Error, Start: e.Span().Start, End: e.Span().End, Message: "const expression overflows U64"}}
    }
    switch e.Op {
    case "+":
        sum, carry := bits.Add64(left, right, 0)
        if carry != 0 {
            return overflow()
        }
        return sum, nil
    case "-":
        if right > left {
            return overflow()
        }
        return left - right, nil
    case "*":
        hi, lo := bits.Mul64(left, right)
        if hi != 0 {
            return overflow()
        }
        return lo, nil
    case "<<":
        if right >= 64 {
            return overflow()
        }
        return left << right, nil
    case ">>":
        if right >= 64 {
            return overflow()
        }
        return left >> right, nil
    case "&":
        return left & right, nil
    case "|":
        return left | right, nil
    case "==":
        if left == right {
            return 1, nil
        }
        return 0, nil
    case "!=":
        if left != right {
            return 1, nil
        }
        return 0, nil
    case "<":
        if left < right {
            return 1, nil
        }
        return 0, nil
    case "<=":
        if left <= right {
            return 1, nil
        }
        return 0, nil
    case ">":
        if left > right {
            return 1, nil
        }
        return 0, nil
    case ">=":
        if left >= right {
            return 1, nil
        }
        return 0, nil
    default:
        return 0, []diag.Diagnostic{{Phase: "sem", Code: diag.SEM0087, Severity: diag.Error, Start: e.Span().Start, End: e.Span().End, Message: "operator "+e.Op+" is not allowed in const expressions"}}
    }
```

Reuse the current semantic payload-size oracle instead of creating a second layout implementation. Move or export the existing `payloadLayoutFromType`, `primitivePayloadLayout`, and `alignPayloadOffset` helpers from `compiler/sem/topic_payload.go` so both topic payload extraction and const evaluation call the same logic. Then add this wrapper used by `sizeof` and `alignof`:

```go
func semanticSizeAlign(t *Type) (size uint64, align uint64, ok bool) {
    if t == nil {
        return 0, 0, false
    }
    if t.Kind == KindPrimitive {
        return primitivePayloadLayout(t.Name)
    }
    if t.Kind != KindData && t.Kind != KindClass {
        return 0, 0, false
    }
    var offset uint64
    var maxAlign uint64 = 1
    for _, field := range t.Fields {
        fieldSize, fieldAlign, ok := semanticSizeAlign(field.Type)
        if !ok {
            return 0, 0, false
        }
        offset = alignPayloadOffset(offset, fieldAlign)
        offset += fieldSize
        if fieldAlign > maxAlign {
            maxAlign = fieldAlign
        }
    }
    return alignPayloadOffset(offset, maxAlign), maxAlign, true
}
```

Task 6 intentionally supports only primitives, data, and class records. Task 8 extends `semanticSizeAlign` for `KindEnum` after enum metadata exists.

Add cross-module const lookup through local declarations first, then imports:

```go
func (idx *Index) LookupConst(moduleName, name string) (ConstValue, bool) {
    if idx == nil {
        return ConstValue{}, false
    }
    if m := idx.Consts[moduleName]; m != nil {
        if v, ok := m[name]; ok {
            return v, true
        }
    }
    if imports := idx.ConstImports[moduleName]; imports != nil {
        if v, ok := imports[name]; ok {
            return v, true
        }
    }
    return ConstValue{}, false
}
```

Add `ConstImports map[string]map[string]ConstValue` to `Index` and populate it using the same import traversal as `ByImport`.

- [ ] **Step 4: Wire const declarations**

During `BuildIndex`, collect `ConstDecl` in source order per module. During `Check`, evaluate each const before method bodies and evaluate `StaticAssertDecl` after its referenced constants are available.

Use this diagnostic for failed assertions:

```go
c.error(assert.SpanV, diag.SEM0089, "static assertion failed: "+assert.Message)
```

- [ ] **Step 5: Verify**

Run:

```bash
go test ./compiler/sem -run 'TestConstSizeofAlignofAndStaticAssert|TestConstOverflowDiagnostic' -v
git diff --check
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add compiler/sem/const.go compiler/sem/const_test.go compiler/sem/symbols.go compiler/sem/check.go compiler/sem/topic_payload.go
git commit -m "feat: evaluate language constants and static assertions -Codex Automated"
```

### Task 7: Check Static Traits And Impl Declarations

**Description:** Implement compile-time trait contracts and explicit impl validation.

**Files:**
- Create: `compiler/sem/trait.go`
- Create: `compiler/sem/trait_test.go`
- Modify: `compiler/sem/types.go`
- Modify: `compiler/sem/symbols.go`
- Modify: `compiler/sem/check.go`

**Acceptance Criteria:**
- Trait declarations are indexed with method signatures.
- Impl declarations are explicit and checked against existing methods on the implemented type.
- Generic impl type parameters are the free type names appearing in the trait ref or implemented type ref; no `impl<T>` syntax is introduced.
- Missing impl emits SEM0081.
- Signature mismatch emits SEM0082.
- Overlapping impl emits SEM0083.
- Trait-constrained generic method calls type-check against bound trait method signatures. Direct IR calls are emitted in Task 13.

- [ ] **Step 1: Add failing tests**

Create `compiler/sem/trait_test.go`:

```go
package sem

import (
    "testing"

    "github.com/ryanwible/wrela3/compiler/diag"
)

func TestTraitImplSatisfiesGenericBound(t *testing.T) {
    modules := parseModulesForTest(t, `
module sem.traits
trait Producer<T> {
    fn next(self) -> T
}
data Event { kind: U64 }
class EventSub {
    fn next(self) -> Event {
        return Event(kind = 1)
    }
}
impl Producer<Event> for EventSub
class Drain<S, T> where S: Producer<T> {
    input: S
    fn poll(self) -> T {
        return self.input.next()
    }
}
data Root { drain: Drain<EventSub, Event> }
`)
    index := mustBuildIndexAllowingMissingImage(t, modules)
    _, ds := checkAllowingMissingImage(t, index, modules)
    if len(ds) != 0 {
        t.Fatalf("semantic diagnostics: %#v", ds)
    }
}

func TestMissingTraitImplDiagnostic(t *testing.T) {
    modules := parseModulesForTest(t, `
module sem.traits
trait Producer<T> { fn next(self) -> T }
data Event { kind: U64 }
class BadSub {}
class Drain<S, T> where S: Producer<T> { input: S }
data Root { drain: Drain<BadSub, Event> }
`)
    index, indexDiags := BuildIndex(modules)
    _, checkDiags := Check(index, modules)
    ds := append(indexDiags, checkDiags...)
    if !hasCode(ds, diag.SEM0081) {
        t.Fatalf("diagnostics = %#v, want SEM0081", ds)
    }
}

func TestOverlappingGenericImplDiagnostic(t *testing.T) {
    modules := parseModulesForTest(t, `
module sem.traits
trait Publisher<T> { fn publish(self, value: T) }
data Event { kind: U64 }
class EventPublisher { fn publish(self, value: Event) {} }
impl Publisher<Event> for EventPublisher
impl Publisher<Event> for EventPublisher
`)
    index, indexDiags := BuildIndex(modules)
    _, checkDiags := Check(index, modules)
    ds := append(indexDiags, checkDiags...)
    if !hasCode(ds, diag.SEM0083) {
        t.Fatalf("diagnostics = %#v, want SEM0083", ds)
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./compiler/sem -run 'TestTraitImplSatisfiesGenericBound|TestMissingTraitImplDiagnostic' -v`

Expected: FAIL.

- [ ] **Step 3: Add trait registry**

Add to `Index`:

```go
Traits map[string]*Trait
Impls  []Impl

type Trait struct {
    Module     string
    Name       string
    TypeParams []TypeParam
    Methods    []Method
    Span       source.Span
}

type Impl struct {
    Trait *Type
    For   *Type
    TypeParams map[string]bool
    Span  source.Span
}
```

Create `compiler/sem/trait.go` with:

```go
func freeImplTypeParams(idx *Index, moduleName string, refs ...ast.TypeRef) []string {
    seen := map[string]bool{}
    var out []string
    var walk func(ast.TypeRef)
    walk = func(ref ast.TypeRef) {
        if len(ref.Args) == 0 && ref.Name != "" && ref.Name[0] >= 'A' && ref.Name[0] <= 'Z' {
            if _, ok := idx.Lookup(moduleName, ref.Name); ok {
                return
            }
            if !seen[ref.Name] {
                seen[ref.Name] = true
                out = append(out, ref.Name)
            }
            return
        }
        for _, arg := range ref.Args {
            walk(arg)
        }
    }
    for _, ref := range refs {
        walk(ref)
    }
    sort.Strings(out)
    return out
}

func (idx *Index) hasImpl(trait *Type, forType *Type) bool {
    for _, impl := range idx.Impls {
        subst := map[string]*Type{}
        if matchImplPattern(impl.Trait, trait, impl.TypeParams, subst) &&
            matchImplPattern(impl.For, forType, impl.TypeParams, subst) {
            return true
        }
    }
    return false
}
```

When indexing `impl Publisher<T> for TopicPublisher<T>`, bind `T` as an impl type parameter because it appears free in the trait and implemented type refs. When matching against concrete `Publisher<Event>` and `TopicPublisher<Event>`, build a substitution map by unifying both refs:

```go
func matchImplPattern(pattern *Type, concrete *Type, typeParams map[string]bool, subst map[string]*Type) bool {
    if pattern == nil || concrete == nil {
        return false
    }
    if pattern.Module == "" && len(pattern.TypeArgs) == 0 && typeParams[pattern.Name] {
        if existing := subst[pattern.Name]; existing != nil {
            return existing.Key() == concrete.Key()
        }
        subst[pattern.Name] = concrete
        return true
    }
    if qualifiedTypeName(pattern) != qualifiedTypeName(concrete) || len(pattern.TypeArgs) != len(concrete.TypeArgs) {
        return false
    }
    for i := range pattern.TypeArgs {
        if !matchImplPattern(pattern.TypeArgs[i], concrete.TypeArgs[i], typeParams, subst) {
            return false
        }
    }
    return true
}
```

Build `typeParams` from `freeImplTypeParams` when indexing the impl:

```go
names := freeImplTypeParams(idx, moduleName, implDecl.Trait, implDecl.For)
typeParams := map[string]bool{}
for _, name := range names {
    typeParams[name] = true
}
```

Overlap rule for this milestone: two impls overlap if the same concrete trait and concrete implemented type can match both impl patterns using their own `TypeParams` sets. Emit SEM0083 on the second impl span. There is no specialization, so overlap is always rejected.

- [ ] **Step 4: Validate impl method signatures**

For each impl, find every trait method on the implemented type. The `self` parameter type is ignored for comparison. All other param counts, param types, and return type must match after substituting trait type parameters.

Use this example comparison:

```go
func methodSignatureMatches(have Method, want Method) bool {
    if len(have.Params) != len(want.Params) {
        return false
    }
    for i := range have.Params {
        if have.Params[i].Name == "self" && want.Params[i].Name == "self" {
            continue
        }
        if have.Params[i].Type.Key() != want.Params[i].Type.Key() {
            return false
        }
    }
    if have.Return == nil || want.Return == nil {
        return have.Return == want.Return
    }
    return have.Return.Key() == want.Return.Key()
}
```

- [ ] **Step 5: Enforce bounds during generic instantiation**

When `completeInstantiation` substitutes `Where` bounds, call `idx.hasImpl(boundTrait, concreteArg)` for each concrete bound such as `S: Producer<T>` becoming `EventSub: Producer<Event>`. Emit:

```go
diag.SEM0081, "missing impl "+boundTrait.Display()+" for "+concreteArg.Display()
```

Fill the `checkConcreteBounds` hook added in Task 5B:

```go
func (idx *Index) checkConcreteBounds(concrete *Type) []diag.Diagnostic {
    if concrete == nil || concrete.GenericOrigin == nil {
        return nil
    }
    subst := substitutionFor(concrete.GenericOrigin, concrete.TypeArgs)
    var out []diag.Diagnostic
    for _, bound := range concrete.GenericOrigin.Where {
        concreteArg := subst[bound.Param]
        boundTrait := idx.substituteType(bound.Trait, subst)
        if concreteArg == nil || boundTrait == nil {
            continue
        }
        if !idx.hasImpl(boundTrait, concreteArg) {
            out = append(out, diag.Diagnostic{
                Phase: "sem", Code: diag.SEM0081, Severity: diag.Error,
                Start: bound.Span.Start, End: bound.Span.End,
                Message: "missing impl "+boundTrait.Display()+" for "+concreteArg.Display(),
            })
        }
    }
    return out
}
```

- [ ] **Step 6: Verify**

Run:

```bash
go test ./compiler/sem -run 'TestTraitImplSatisfiesGenericBound|TestMissingTraitImplDiagnostic' -v
go test ./compiler/sem -run TestOverlappingGenericImplDiagnostic -v
git diff --check
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add compiler/sem/trait.go compiler/sem/trait_test.go compiler/sem/types.go compiler/sem/symbols.go compiler/sem/check.go
git commit -m "feat: check static trait implementations -Codex Automated"
```

### Task 8: Check Enums, Variant Constructors, If-Let, And Match

**Description:** Implement enum typing, variant constructor inference, payload binding types, and exhaustive match checking.

**Files:**
- Create: `compiler/sem/enum.go`
- Create: `compiler/sem/enum_test.go`
- Modify: `compiler/sem/types.go`
- Modify: `compiler/sem/check.go`

**Acceptance Criteria:**
- Enum variants are indexed on the enum `Type`.
- Variant constructors return concrete enum types.
- `Option.None()` requires an expected type unless all type args can be inferred elsewhere; without one it emits SEM0079.
- Impossible patterns emit SEM0085.
- Non-exhaustive matches emit SEM0084.
- Duplicate concrete variant arms emit SEM0095.
- Missing payload fields, unknown payload fields, and duplicate binding names emit SEM0095.
- Invalid pattern binding names emit SEM0095.
- `if let` validates the same pattern rules as `match` and type-checks its body scope.
- Payload bindings are scoped only to their arm/body.

- [ ] **Step 1: Add failing tests**

Create `compiler/sem/enum_test.go`:

```go
package sem

import (
    "testing"

    "github.com/ryanwible/wrela3/compiler/diag"
)

func TestEnumMatchExhaustiveAndBindings(t *testing.T) {
    modules := parseModulesForTest(t, `
module sem.enums
enum Option<T> { None Some(value: T) }
data Event { kind: U64 }
class Worker {
    fn handle(self, next: Option<Event>) {
        match next {
            Option.Some(value = event) => {
                let k = event.kind
            }
            Option.None => {
                let z = 0
            }
        }
    }
}
`)
    index := mustBuildIndexAllowingMissingImage(t, modules)
    _, ds := checkAllowingMissingImage(t, index, modules)
    if len(ds) != 0 {
        t.Fatalf("semantic diagnostics: %#v", ds)
    }
}

func TestNonExhaustiveMatchDiagnostic(t *testing.T) {
    modules := parseModulesForTest(t, `
module sem.enums
enum Option<T> { None Some(value: T) }
data Event { kind: U64 }
class Worker {
    fn handle(self, next: Option<Event>) {
        match next {
            Option.Some(value = event) => {
                let k = event.kind
            }
        }
    }
}
`)
    index, indexDiags := BuildIndex(modules)
    _, checkDiags := Check(index, modules)
    ds := append(indexDiags, checkDiags...)
    if !hasCode(ds, diag.SEM0084) {
        t.Fatalf("diagnostics = %#v, want SEM0084", ds)
    }
}

func TestIfLetAndInvalidPatternDiagnostics(t *testing.T) {
    modules := parseModulesForTest(t, `
module sem.enums
enum Option<T> { None Some(value: T) }
data Event { kind: U64 }
class Worker {
    fn handle(self, next: Option<Event>) {
        if let Option.Some(value = event) = next {
            let k = event.kind
        }
        match next {
            Option.Some(value = one, value = two) => {}
            Option.None => {}
        }
    }
}
`)
    index, indexDiags := BuildIndex(modules)
    _, checkDiags := Check(index, modules)
    ds := append(indexDiags, checkDiags...)
    if !hasCode(ds, diag.SEM0095) {
        t.Fatalf("diagnostics = %#v, want SEM0095", ds)
    }
}

func TestVariantConstructorExpectedTypeInference(t *testing.T) {
    modules := parseModulesForTest(t, `
module sem.enums
enum Option<T> { None Some(value: T) }
data Event { kind: U64 }
class Worker {
    fn none(self) -> Option<Event> {
        return Option.None()
    }
    fn some(self) -> Option<Event> {
        return Option.Some(value = Event(kind = 1))
    }
}
`)
    index := mustBuildIndexAllowingMissingImage(t, modules)
    _, ds := checkAllowingMissingImage(t, index, modules)
    if len(ds) != 0 {
        t.Fatalf("semantic diagnostics: %#v", ds)
    }
}

func TestVariantConstructorMissingInferenceDiagnostic(t *testing.T) {
    modules := parseModulesForTest(t, `
module sem.enums
enum Option<T> { None Some(value: T) }
class Worker {
    fn bad(self) {
        let none = Option.None()
    }
}
`)
    index, indexDiags := BuildIndex(modules)
    _, checkDiags := Check(index, modules)
    ds := append(indexDiags, checkDiags...)
    if !hasCode(ds, diag.SEM0079) {
        t.Fatalf("diagnostics = %#v, want SEM0079", ds)
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./compiler/sem -run 'TestEnumMatchExhaustiveAndBindings|TestNonExhaustiveMatchDiagnostic|TestVariantConstructorExpectedTypeInference|TestVariantConstructorMissingInferenceDiagnostic' -v`

Expected: FAIL.

- [ ] **Step 3: Add enum metadata**

In `compiler/sem/types.go`:

```go
type EnumVariant struct {
    Name   string
    Fields []Field
    Span   source.Span
}

type Type struct {
    // existing fields...
    EnumVariants []EnumVariant
}
```

After indexing enum declarations, extend the Task 5B instantiation completion path so concrete generic enums carry substituted variant payload types:

```go
if base.Kind == KindEnum {
    concrete.EnumVariants = substituteEnumVariants(idx, base.EnumVariants, subst)
}
```

Use the same `substituteFields` helper used for data fields:

```go
func substituteEnumVariants(idx *Index, variants []EnumVariant, subst substitution) []EnumVariant {
    out := make([]EnumVariant, 0, len(variants))
    for _, variant := range variants {
        out = append(out, EnumVariant{Name: variant.Name, Fields: substituteFields(idx, variant.Fields, subst), Span: variant.Span})
    }
    return out
}
```

When indexing `ast.EnumDecl`, create `KindEnum` in `Kind`:

```go
const (
    KindPrimitive Kind = iota
    KindData
    KindClass
    KindDriver
    KindDriverPath
    KindExecutor
    KindImage
    KindTypeParam
    KindEnum
    KindTrait
)
```

- [ ] **Step 4: Add enum checker helpers**

Create `compiler/sem/enum.go`:

```go
func (c *checker) enumVariant(enumType *Type, name string) (EnumVariant, bool) {
    if enumType == nil || enumType.Kind != KindEnum {
        return EnumVariant{}, false
    }
    origin := enumType
    if enumType.GenericOrigin != nil {
        origin = enumType.GenericOrigin
    }
    for _, variant := range origin.EnumVariants {
        if variant.Name == name {
            return c.substituteEnumVariant(enumType, variant), true
        }
    }
    return EnumVariant{}, false
}

func (c *checker) enumVariants(enumType *Type) []EnumVariant {
    if enumType == nil {
        return nil
    }
    if len(enumType.EnumVariants) != 0 {
        return enumType.EnumVariants
    }
    if enumType.GenericOrigin != nil {
        return enumType.GenericOrigin.EnumVariants
    }
    return nil
}

func (c *checker) substituteEnumVariant(enumType *Type, variant EnumVariant) EnumVariant {
    for _, concrete := range enumType.EnumVariants {
        if concrete.Name == variant.Name {
            return concrete
        }
    }
    return variant
}

func (c *checker) checkMatchStmt(moduleName string, scope *Scope, expectedReturn *Type, ctx ContextKind, stmt *ast.MatchStmt) bool {
    valueType := c.typeExpr(moduleName, stmt.Value, scope, ctx)
    if valueType == nil || valueType.Kind != KindEnum {
        c.error(stmt.Value.Span(), diag.SEM0085, "match requires enum value")
        return false
    }
    seen := map[string]source.Span{}
    wildcard := false
    terminates := false
    for _, arm := range stmt.Arms {
        switch p := arm.Pattern.(type) {
        case ast.WildcardPattern:
            wildcard = true
            if c.checkStmtList(moduleName, arm.Body, NewScope(scope), expectedReturn, ctx) {
                terminates = true
            }
        case ast.VariantPattern:
            variant, ok := c.enumVariant(valueType, p.Variant)
            if !ok || !sameEnumPatternName(valueType, p.Enum) {
                c.error(arm.Span, diag.SEM0085, "impossible enum variant pattern "+p.Enum+"."+p.Variant)
                continue
            }
            if first := seen[variant.Name]; first.Start != 0 || first.End != 0 {
                c.error(arm.Span, diag.SEM0095, "duplicate match arm for "+p.Enum+"."+p.Variant)
                continue
            }
            seen[variant.Name] = arm.Span
            armScope := NewScope(scope)
            if c.bindPatternFields(armScope, variant, p.Bindings, arm.Span) {
                continue
            }
            if c.checkStmtList(moduleName, arm.Body, armScope, expectedReturn, ctx) {
                terminates = true
            }
        }
    }
    if !wildcard && len(seen) != len(c.enumVariants(valueType)) {
        c.error(stmt.SpanV, diag.SEM0084, "non-exhaustive match for "+valueType.Display())
    }
    return terminates
}
```

Extend the Task 6 `semanticSizeAlign` helper with enum sizing now that `KindEnum` and `EnumVariants` exist:

```go
if t.Kind == KindEnum {
    enumSize, enumAlign := uint64(8), uint64(8)
    for _, variant := range t.EnumVariants {
        var offset uint64
        var maxAlign uint64 = 1
        for _, field := range variant.Fields {
            fieldSize, fieldAlign, ok := semanticSizeAlign(field.Type)
            if !ok {
                return 0, 0, false
            }
            offset = alignPayloadOffset(offset, fieldAlign)
            offset += fieldSize
            if fieldAlign > maxAlign {
                maxAlign = fieldAlign
            }
        }
        payload := alignPayloadOffset(offset, maxAlign)
        if 8+payload > enumSize {
            enumSize = 8 + payload
        }
        if maxAlign > enumAlign {
            enumAlign = maxAlign
        }
    }
    return alignPayloadOffset(enumSize, enumAlign), enumAlign, true
}
```

`bindPatternFields` must enforce payload shape exactly:

```go
func (c *checker) bindPatternFields(scope *Scope, variant EnumVariant, bindings []ast.PatternBinding, span source.Span) bool {
    byName := map[string]Field{}
    for _, field := range variant.Fields {
        byName[field.Name] = field
    }
    seenFields := map[string]bool{}
    seenBinds := map[string]bool{}
    failed := false
    for _, binding := range bindings {
        field, ok := byName[binding.Name]
        if !ok {
            c.error(span, diag.SEM0095, "unknown payload field "+binding.Name)
            failed = true
            continue
        }
        if seenFields[binding.Name] {
            c.error(span, diag.SEM0095, "duplicate payload field "+binding.Name)
            failed = true
        }
        if seenBinds[binding.Bind] {
            c.error(span, diag.SEM0095, "duplicate pattern binding "+binding.Bind)
            failed = true
        }
        seenFields[binding.Name] = true
        seenBinds[binding.Bind] = true
        scope.Define(binding.Bind, field.Type)
    }
    for _, field := range variant.Fields {
        if !seenFields[field.Name] {
            c.error(span, diag.SEM0095, "missing payload field "+field.Name)
            failed = true
        }
    }
    return failed
}
```

Add `checkIfLetStmt`:

```go
func (c *checker) checkIfLetStmt(moduleName string, scope *Scope, expectedReturn *Type, ctx ContextKind, stmt *ast.IfLetStmt) bool {
    valueType := c.typeExpr(moduleName, stmt.Value, scope, ctx)
    pattern, ok := stmt.Pattern.(ast.VariantPattern)
    if !ok {
        c.error(stmt.SpanV, diag.SEM0095, "if let requires enum variant pattern")
        return false
    }
    variant, found := c.enumVariant(valueType, pattern.Variant)
    if !found || !sameEnumPatternName(valueType, pattern.Enum) {
        c.error(stmt.SpanV, diag.SEM0085, "impossible enum variant pattern "+pattern.Enum+"."+pattern.Variant)
        return false
    }
    bodyScope := NewScope(scope)
    if c.bindPatternFields(bodyScope, variant, pattern.Bindings, stmt.SpanV) {
        return false
    }
    c.checkStmtList(moduleName, stmt.Body, bodyScope, expectedReturn, ctx)
    return false
}
```

`sameEnumPatternName` accepts either the source enum name (`Option`) or the concrete display base (`Option<Event>` stripped to `Option`) so imports and generic instantiations do not break pattern matching.

Wire both statement forms in `checkStmt` using the existing function signature:

```go
case *ast.MatchStmt:
    return c.checkMatchStmt(moduleName, scope, expectedReturn, ctx, s)
case *ast.IfLetStmt:
    return c.checkIfLetStmt(moduleName, scope, expectedReturn, ctx, s)
```

- [ ] **Step 5: Wire expected typing and variant constructor typing**

Keep the existing checker call shape intact by making the current `typeExpr(moduleName, expr, scope, ctx)` delegate to a new expected-type-aware helper:

```go
func (c *checker) typeExpr(moduleName string, expr ast.Expr, scope *Scope, ctx ContextKind) *Type {
    return c.typeExprExpected(moduleName, expr, scope, ctx, nil)
}

func (c *checker) typeExprExpected(moduleName string, expr ast.Expr, scope *Scope, ctx ContextKind, expected *Type) *Type {
    switch e := expr.(type) {
    case *ast.VariantConstructorExpr:
        return c.typeVariantConstructorExpr(moduleName, e, scope, ctx, expected)
    default:
        return c.typeExprNoExpected(moduleName, expr, scope, ctx)
    }
}
```

Move the current body of `typeExpr` into `typeExprNoExpected` unchanged, except recursive expression calls should use `typeExprExpected(moduleName, child, scope, ctx, nil)` until a later bullet in this step says a concrete expected type is available.

Update only the call sites that have a real expected type:

```go
// return statement
got := c.typeExprExpected(moduleName, s.Value, scope, ctx, expectedReturn)
c.requireType(got, expectedReturn, s.Value.Span())

// assignment
targetType := c.typeExpr(moduleName, s.Target, scope, ctx)
valueType := c.typeExprExpected(moduleName, s.Value, scope, ctx, targetType)
c.checkTypeAssign(s.Target.Span(), targetType, valueType)

// constructor arguments in typeConstructorExpr, where constructed is the current constructor type
for _, field := range constructed.Fields {
    value := namedArgValue(expr.Args, field.Name)
    argType := c.typeExprExpected(moduleName, value, scope, ctx, field.Type)
    c.checkTypeAssign(value.Span(), field.Type, argType)
}
```

`typeVariantConstructorExpr` finds enum candidates named `e.Enum`, matches `e.Variant`, and returns either the expected type or an inferred concrete enum type. Emit SEM0079 if type arguments cannot be inferred.

Use this inference rule:

- If the expression is checked against an expected return/assignment type, use that enum type.
- Else infer type args from payload fields whose field type is a type parameter and whose argument expression has a concrete type.
- If the variant has no payload fields, such as `Option.None()`, the expression must have an expected enum type. Without an expected type, emit SEM0079.
- If payload inference produces conflicting concrete types for the same type parameter, emit SEM0079.
- If any type parameter remains unknown after payload inference, emit SEM0079.

Use this helper shape:

```go
func (c *checker) inferEnumTypeArgs(moduleName string, enum *Type, variant EnumVariant, args []ast.NamedArg, expected *Type, scope *Scope, ctx ContextKind) ([]*Type, bool) {
    if expected != nil && (expected == enum || expected.GenericOrigin == enum) {
        return expected.TypeArgs, true
    }
    inferred := map[string]*Type{}
    for _, field := range variant.Fields {
        if len(field.Type.TypeArgs) != 0 || field.Type.Module != "" {
            continue
        }
        argExpr := namedArgValue(args, field.Name)
        if argExpr == nil {
            continue
        }
        concrete := c.typeExpr(moduleName, argExpr, scope, ctx)
        if concrete == nil {
            continue
        }
        if existing := inferred[field.Type.Name]; existing != nil && existing.Key() != concrete.Key() {
            return nil, false
        }
        inferred[field.Type.Name] = concrete
    }
    out := make([]*Type, 0, len(enum.TypeParams))
    for _, param := range enum.TypeParams {
        concrete := inferred[param.Name]
        if concrete == nil {
            return nil, false
        }
        out = append(out, concrete)
    }
    return out, true
}
```

- [ ] **Step 6: Verify**

Run:

```bash
go test ./compiler/sem -run 'TestEnumMatchExhaustiveAndBindings|TestNonExhaustiveMatchDiagnostic|TestVariantConstructorExpectedTypeInference|TestVariantConstructorMissingInferenceDiagnostic' -v
go test ./compiler/sem -run TestIfLetAndInvalidPatternDiagnostics -v
git diff --check
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add compiler/sem/enum.go compiler/sem/enum_test.go compiler/sem/types.go compiler/sem/check.go
git commit -m "feat: check enums and pattern matches -Codex Automated"
```

### Task 9: Add Typed Memory View Semantics And Reserve Array

**Description:** Extend the existing arena/lifetime checker to cover generic `Slots<T>`, `Slice<T>`, `MutableSlice<T>`, `reserve_array`, and protected region-kind views.

**Files:**
- Modify: `compiler/sem/memory.go`
- Modify: `compiler/sem/memory_test.go`
- Modify: `compiler/sem/check.go`
- Add negative fixtures under `tests/fixtures/negative/`

**Acceptance Criteria:**
- `tick.reserve_array(Event, count = EVENT_CAPACITY)` returns `Slots<Event>` with the receiver lifetime.
- `count * sizeof(Event)` overflow emits SEM0090.
- `Slots<T>` values cannot escape their frame lifetime.
- `Slots<T>.get(...)` and raw reads from slots emit SEM0093.
- `Slots<T>.address` access outside trusted modules emits SEM0096.
- Protected region-kind constructors outside authority modules emit SEM0092.
- `reserve_array(Event, count = 18446744073709551615)` where `sizeof(Event) > 1` emits SEM0090.
- Returning or storing frame-backed `Slots<T>` into executor-root state emits SEM0091.

- [ ] **Step 1: Add failing semantic tests**

Append to `compiler/sem/memory_test.go`:

```go
func memoryViewPreludeForTest() string {
    return `
module machine.x86_64.executor_memory
data BufferFull {}
data ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame {
        return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0)
    }
}
data ArenaFrame {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
}
data Slots<T> {
    address: PhysicalAddress
    capacity: U64
    fn write(self, index: U64, value: T) {}
}
data Slice<T> { address: PhysicalAddress; length: U64 }
data MutableSlice<T> { address: PhysicalAddress; length: U64 }
`
}

func TestReserveArrayReturnsFrameLifetimeSlots(t *testing.T) {
    modules := parseModulesForTest(t, memoryViewPreludeForTest(), `
module sem.slots
use { Slots, ExecutorMemory } from machine.x86_64.executor_memory
data Event { kind: U64 }
executor Worker {
    memory: ExecutorMemory
    start fn run(self) -> never {
        with self.memory.frame(length = 4096) as tick {
            let slots = tick.reserve_array(Event, count = 16)
            slots.write(index = 0, value = Event(kind = 1))
        }
        while true {}
    }
}
`)
    index := mustBuildIndexAllowingMissingImage(t, modules)
    _, ds := checkAllowingMissingImage(t, index, modules)
    if len(ds) != 0 {
        t.Fatalf("semantic diagnostics: %#v", ds)
    }
}

func TestRawSlotsReadRejected(t *testing.T) {
    modules := parseModulesForTest(t, memoryViewPreludeForTest(), `
module sem.slots
use { ExecutorMemory } from machine.x86_64.executor_memory
data Event { kind: U64 }
executor Worker {
    memory: ExecutorMemory
    start fn run(self) -> never {
        with self.memory.frame(length = 4096) as tick {
            let slots = tick.reserve_array(Event, count = 16)
            let bad = slots.get(index = 0)
        }
        while true {}
    }
}
`)
    index, indexDiags := BuildIndex(modules)
    _, checkDiags := Check(index, modules)
    ds := append(indexDiags, checkDiags...)
    if !hasCode(ds, diag.SEM0093) {
        t.Fatalf("diagnostics = %#v, want SEM0093", ds)
    }
}

func TestReserveArraySizeOverflowDiagnostic(t *testing.T) {
    modules := parseModulesForTest(t, memoryViewPreludeForTest(), `
module sem.slots
use { ExecutorMemory } from machine.x86_64.executor_memory
data Event { a: U64; b: U64 }
executor Worker {
    memory: ExecutorMemory
    start fn run(self) -> never {
        with self.memory.frame(length = 4096) as tick {
            let slots = tick.reserve_array(Event, count = 18446744073709551615)
        }
        while true {}
    }
}
`)
    index, indexDiags := BuildIndex(modules)
    _, checkDiags := Check(index, modules)
    ds := append(indexDiags, checkDiags...)
    if !hasCode(ds, diag.SEM0090) {
        t.Fatalf("diagnostics = %#v, want SEM0090", ds)
    }
}

func TestSlotsLifetimeEscapeDiagnostic(t *testing.T) {
    modules := parseModulesForTest(t, memoryViewPreludeForTest(), `
module sem.slots
use { ExecutorMemory, Slots } from machine.x86_64.executor_memory
data Event { kind: U64 }
executor Worker {
    memory: ExecutorMemory
    escaped: Slots<Event>
    start fn run(self) -> never {
        with self.memory.frame(length = 4096) as tick {
            let slots = tick.reserve_array(Event, count = 16)
            self.escaped = slots
        }
        while true {}
    }
}
`)
    index, indexDiags := BuildIndex(modules)
    _, checkDiags := Check(index, modules)
    ds := append(indexDiags, checkDiags...)
    if !hasCode(ds, diag.SEM0091) {
        t.Fatalf("diagnostics = %#v, want SEM0091", ds)
    }
}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./compiler/sem -run 'TestReserveArrayReturnsFrameLifetimeSlots|TestRawSlotsReadRejected|TestReserveArraySizeOverflowDiagnostic|TestSlotsLifetimeEscapeDiagnostic' -v`

Expected: FAIL.

- [ ] **Step 3: Recognize memory view types**

Add helpers to `compiler/sem/memory.go`:

```go
func isSlotsType(t *Type) bool {
    return t != nil && t.Name == "Slots" && t.Module == "machine.x86_64.executor_memory" && len(t.TypeArgs) == 1
}

func isSliceType(t *Type) bool {
    return t != nil && (t.Name == "Slice" || t.Name == "MutableSlice") && t.Module == "machine.x86_64.executor_memory" && len(t.TypeArgs) == 1
}

func isProtectedViewType(t *Type) bool {
    if isSlotsType(t) || isSliceType(t) {
        return true
    }
    switch qualifiedTypeName(t) {
    case "platform.hardware.bytes.Mmio",
        "platform.uefi.types.FirmwareSlice",
        "platform.hardware.bytes.Volatile",
        "platform.hardware.memory.DmaBuffer":
        return true
    default:
        return false
    }
}
```

Add these helper shapes in the same file:

```go
func (c *checker) firstArgAsType(args []ast.NamedArg) (*Type, bool) {
    if len(args) == 0 || args[0].Name != "" {
        return nil, false
    }
    switch value := args[0].Value.(type) {
    case *ast.NameExpr:
        typ, ok := c.index.Lookup(c.currentModuleName(), value.Name)
        return typ, ok
    case *ast.TypeOperandExpr:
        typ, ds := c.index.LookupTypeRef(c.currentModuleName(), value.Type, nil)
        if len(ds) != 0 {
            c.diags = append(c.diags, ds...)
            return nil, false
        }
        return typ, true
    default:
        return nil, false
    }
}

func typeHasKnownLayout(t *Type) bool {
    _, _, ok := semanticSizeAlign(t)
    return ok
}

func (c *checker) currentModuleName() string {
    if c.currentType != nil && c.currentType.Module != "" {
        return c.currentType.Module
    }
    return ""
}

func isTrustedAuthorityModule(moduleName string) bool {
    return strings.HasPrefix(moduleName, "platform.hardware.") ||
        strings.HasPrefix(moduleName, "platform.uefi.") ||
        strings.HasPrefix(moduleName, "platform.acpi.") ||
        strings.HasPrefix(moduleName, "machine.x86_64.")
}
```

- [ ] **Step 4: Type-check reserve_array**

In call expression checking, before normal method lookup:

```go
if expr.Method == "reserve_array" {
    receiverType := c.typeExpr(moduleName, expr.Receiver, scope, ctx)
    if !isArenaReceiver(receiverType) {
        c.error(expr.SpanV, diag.SEM0021, "reserve_array receiver must be ExecutorMemory or ArenaFrame")
        return nil
    }
    elemType, ok := c.firstArgAsType(expr.Args)
    if !ok {
        c.error(expr.SpanV, diag.SEM0078, "reserve_array first argument must be a type")
        return nil
    }
    if !typeHasKnownLayout(elemType) {
        c.error(expr.SpanV, diag.SEM0080, "reserve_array element type must have known layout")
        return nil
    }
    slotsType := c.index.instantiateByName("machine.x86_64.executor_memory", "Slots", []*Type{elemType})
    c.rememberLifetime(expr, c.lifetimeOfExpr(expr.Receiver, scope))
    return slotsType
}
```

Add the compile-time overflow check when `count` is const:

```go
countArg := namedArgValue(expr.Args, "count")
if countValue, ok := c.constValueOfExpr(countArg); ok {
    elemSize, _, _ := semanticSizeAlign(elemType)
    const maxUint64 = ^uint64(0)
    if elemSize != 0 && countValue > maxUint64/elemSize {
        c.error(expr.SpanV, diag.SEM0090, "slot count overflows reservation size")
    }
}
```

Add the small helper:

```go
func namedArgValue(args []ast.NamedArg, name string) ast.Expr {
    for _, arg := range args {
        if arg.Name == name {
            return arg.Value
        }
    }
    return nil
}
```

For hidden lifetime propagation, treat `Slots<T>`, `Slice<T>`, and `MutableSlice<T>` as lifetime-carrying types:

```go
func typeCanCarryHiddenLifetime(t *Type) bool {
    if isSlotsType(t) || isSliceType(t) {
        return true
    }
    // keep existing cases below this line
}
```

When `rejectIfLifetimeEscapes` sees a source or target type that is slots/slice-backed, emit SEM0091 instead of the older frame escape code:

```go
if (isSlotsType(sourceType) || isSliceType(sourceType)) && sourceLifetime.shorterThan(targetLifetime, c.frameLifetimeParents) {
    c.error(span, diag.SEM0091, "slots or slice lifetime escapes")
    return true
}
```

- [ ] **Step 5: Enforce protected view construction and field access**

Reject protected constructors outside trusted modules:

```go
if isProtectedViewType(constructedType) && !isTrustedAuthorityModule(c.currentModuleName()) {
    c.error(expr.SpanV, diag.SEM0092, "protected memory-region view construction is not allowed here")
}
```

Reject direct `Slots<T>.address` outside trusted modules:

```go
if isSlotsType(baseType) && e.Field == "address" && !isTrustedAuthorityModule(c.currentModuleName()) {
    c.error(e.SpanV, diag.SEM0096, "Slots.address is protected")
}
```

Reject raw read calls:

```go
if isSlotsType(receiverType) && (expr.Method == "get" || expr.Method == "read") {
    c.error(expr.SpanV, diag.SEM0093, "raw Slots memory cannot be read directly")
}
```

- [ ] **Step 6: Add negative fixtures**

Create `tests/fixtures/negative/raw_slots_read.wrela`:

```wrela
// expect: SEM0093: raw Slots memory cannot be read directly
module machine.x86_64.executor_memory
data ExecutorMemory {
    fn frame(self, length: U64) -> ArenaFrame {
        return ArenaFrame()
    }
}
data ArenaFrame {}
data Slots<T> {
    address: U64
    capacity: U64
}

module negative.raw_slots_read
use { ExecutorMemory } from machine.x86_64.executor_memory
data Event { kind: U64 }
executor Worker {
    memory: ExecutorMemory
    start fn run(self) -> never {
        with self.memory.frame(length = 4096) as tick {
            let slots = tick.reserve_array(Event, count = 16)
            let bad = slots.get(index = 0)
        }
        while true {}
    }
}
```

Create `tests/fixtures/negative/forged_slots.wrela`:

```wrela
// expect: SEM0092: protected memory-region view construction is not allowed here
module machine.x86_64.executor_memory
data Slots<T> {
    address: U64
    capacity: U64
}

module negative.forged_slots
use { Slots } from machine.x86_64.executor_memory
data Event { kind: U64 }
data Holder {
    slots: Slots<Event>
}
executor Worker {
    start fn run(self) -> never {
        let slots = Slots<Event>(address = 0x1000, capacity = 4)
        while true {}
    }
}
```

- [ ] **Step 7: Verify**

Run:

```bash
go test ./compiler/sem -run 'TestReserveArrayReturnsFrameLifetimeSlots|TestRawSlotsReadRejected' -v
go test ./compiler -run TestNegativeFixtures/raw_slots_read.wrela -v
go test ./compiler -run TestNegativeFixtures/forged_slots.wrela -v
git diff --check
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add compiler/sem/memory.go compiler/sem/memory_test.go compiler/sem/check.go tests/fixtures/negative/raw_slots_read.wrela tests/fixtures/negative/forged_slots.wrela
git commit -m "feat: enforce typed memory view semantics -Codex Automated"
```

---

## 7. Phase 3: Layout, IR, And Codegen

**Description:** This phase turns checked language constructs into deterministic concrete layouts, IR operations, and x86_64 code.

**Phase Acceptance Criteria:**

- Generic instantiations appear in `ir.Program.Types` with deterministic keys and concrete field layouts.
- Enum layouts include discriminant offset, payload offset, variant field offsets, total size, and alignment.
- IR has operations for enum construction, variant tests, payload extraction, arena array reservation, slot writes, and slice get/set.
- Codegen emits checked reservation math, bounds checks, enum discriminant stores/tests, payload copies, and direct calls for monomorphized trait methods.
- Phase smoke command after Task 13: `go test ./compiler/layout ./compiler/ir ./compiler/codegen -run 'Test.*(Generic|Enum|ReserveArray|Slot|Slice|Trait)' -v`.

**Phase Code Example:**

```go
&ir.ArenaReserveArray{
    Arena:   frame,
    Element: ir.Type{Name: "Event"},
    Count:   ir.ConstInt{Value: 16, Type: ir.Type{Name: "U64"}},
    Type:    ir.Type{Name: "Slots<Event>", Module: "machine.x86_64.executor_memory"},
}
```

### Task 10: Add Generic And Enum Layouts

**Description:** Teach the primitive layout package to compute tagged-union shapes and teach IR lowering to compute semantic generic enum layouts from `*sem.Type`. The layout package remains string-typed and primitive-oriented; real enum payloads in source must be sized through semantic type info so `Option<BigRecord>` is not treated as an 8-byte payload.

**Files:**
- Modify: `compiler/layout/record.go`
- Modify: `compiler/layout/record_test.go`
- Create: `compiler/layout/enum.go`
- Create: `compiler/layout/enum_test.go`
- Modify: `compiler/ir/ir.go`
- Modify: `compiler/ir/lower.go`

**Acceptance Criteria:**
- `Result<Unit, BufferFull>` layout is deterministic.
- Enum discriminant is at offset 0, uses `U64`, and payload starts at offset 8.
- Payload area size is max variant payload size rounded up to max payload alignment.
- Zero-payload variants do not add payload fields.
- IR type info records enum variant order and discriminant values for codegen.
- IR enum layout uses semantic field sizes for record payloads larger than one machine word.
- `program.Types` keys use the fully qualified semantic `Type.Key()` for generic instantiations.

- [ ] **Step 1: Add failing layout tests**

Create `compiler/layout/enum_test.go`:

```go
package layout

import "testing"

func TestEnumLayoutUsesDiscriminantAndPrimitiveMaxPayload(t *testing.T) {
    rec, err := ComputeEnum([]EnumVariant{
        {Name: "None"},
        {Name: "Some", Fields: []Field{{Name: "value", Type: "U64"}}},
    })
    if err != nil {
        t.Fatalf("ComputeEnum: %v", err)
    }
    if rec.DiscriminantOffset != 0 || rec.PayloadOffset != 8 {
        t.Fatalf("offsets = discr %d payload %d, want 0 and 8", rec.DiscriminantOffset, rec.PayloadOffset)
    }
    if rec.Size != 16 || rec.Align != 8 {
        t.Fatalf("size/align = %d/%d, want 16/8", rec.Size, rec.Align)
    }
    some := rec.Variants["Some"]
    if some.Fields["value"].Offset != 8 {
        t.Fatalf("Some.value offset = %d, want 8", some.Fields["value"].Offset)
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./compiler/layout -run TestEnumLayoutUsesDiscriminantAndPrimitiveMaxPayload -v`

Expected: FAIL.

- [ ] **Step 3: Implement enum layout**

Create `compiler/layout/enum.go`:

```go
type EnumVariant struct {
    Name   string
    Fields []Field
}

type EnumVariantLayout struct {
    Fields map[string]FieldLayout
    Size   int
    Align  int
}

type EnumRecord struct {
    DiscriminantOffset int
    PayloadOffset      int
    Variants           map[string]EnumVariantLayout
    Size               int
    Align              int
}

func ComputeEnum(variants []EnumVariant) (EnumRecord, error) {
    out := EnumRecord{
        DiscriminantOffset: 0,
        PayloadOffset:      8,
        Variants:           map[string]EnumVariantLayout{},
        Align:              8,
    }
    maxPayloadSize := 0
    maxPayloadAlign := 1
    for _, variant := range variants {
        rec, err := Compute(variant.Fields)
        if err != nil {
            return EnumRecord{}, err
        }
        shifted := map[string]FieldLayout{}
        for name, field := range rec.Fields {
            field.Offset += out.PayloadOffset
            shifted[name] = field
        }
        out.Variants[variant.Name] = EnumVariantLayout{Fields: shifted, Size: rec.Size, Align: rec.Align}
        if rec.Size > maxPayloadSize {
            maxPayloadSize = rec.Size
        }
        if rec.Align > maxPayloadAlign {
            maxPayloadAlign = rec.Align
        }
    }
    out.Align = max(8, maxPayloadAlign)
    out.Size = AlignUp(out.PayloadOffset+maxPayloadSize, out.Align)
    return out, nil
}
```

- [ ] **Step 4: Wire IR type info**

In `compiler/ir/ir.go`, add enum type kind support:

```go
const (
    TypeKindEnum TypeKind = "enum"
)

type EnumVariantInfo struct {
    Name         string
    Discriminant uint64
    Fields       []string
}

type TypeInfo struct {
    Name         string
    Module       string
    Kind         TypeKind
    Size         int
    Align        int
    StorageSize  int
    Fields       map[string]FieldInfo
    FieldOrder   []string
    EnumVariants []EnumVariantInfo
}
```

Update IR type keys before adding enum info. `program.Types` must be keyed by semantic keys so `Box<U64>` and `Box<Event>` never share one `"Box"` entry:

```go
func (ctx *lowerContext) typeInfoKey(typ *sem.Type) string {
    if typ == nil {
        return ""
    }
    if typ.Kind == sem.KindPrimitive {
        return typ.Name
    }
    return typ.Key()
}

func (ctx *lowerContext) irType(typ *sem.Type) Type {
    if typ == nil {
        return Type{Name: "void", Kind: TypeKindPrimitive}
    }
    name := ctx.typeInfoKey(typ)
    return Type{Name: name, Module: typ.Module, Kind: ctx.irKind(typ)}
}
```

Every `ensureTypeInfo`, `program.Types[...]` writer, and `program.Types[...]` reader in IR lowering must use `ctx.typeInfoKey(typ)` or `ir.Type.Name` from the lowered value. Do not keep the old `typeInfoKey(module, name)` helper for semantic types.

When `ensureTypeInfo` sees `sem.KindEnum`, compute layout from semantic fields, not by passing string names through `layout.ComputeEnum`. Create `TypeInfo` where:

- `Fields["$tag"]` is `U64` at offset 0.
- Each variant payload field is recorded with key `Variant.field`, for example `Some.value`.
- `FieldOrder` starts with `$tag`, then variant fields in declaration order.
- `EnumVariants` is appended in enum declaration order with discriminants starting at `0`.

Example:

```go
info.Fields["$tag"] = FieldInfo{Name: "$tag", Type: Type{Name: "U64", Kind: TypeKindPrimitive}, Offset: 0, Size: 8, Align: 8, StorageOffset: 0, StorageSize: 8}
info.Fields["Some.value"] = FieldInfo{Name: "Some.value", Type: ctx.irType(field.Type), Offset: 8, Size: 8, Align: 8, StorageOffset: 8, StorageSize: 8}
info.EnumVariants = append(info.EnumVariants, EnumVariantInfo{Name: "Some", Discriminant: 1, Fields: []string{"Some.value"}})
```

Use a semantic field-layout helper inside `compiler/ir/lower.go`:

```go
func (ctx *lowerContext) semanticFieldLayout(fields []sem.Field, baseOffset int, visiting map[string]bool) (map[string]FieldInfo, []string, int, int) {
    out := map[string]FieldInfo{}
    order := []string{}
    offset := baseOffset
    maxAlign := 1
    for _, field := range fields {
        fieldInfo := ctx.ensureTypeInfo(field.Type, visiting)
        align := fieldInfo.Align
        offset = alignUp(offset, align)
        out[field.Name] = FieldInfo{
            Name: field.Name, Type: ctx.irType(field.Type),
            Offset: offset, Size: fieldInfo.Size, Align: align,
            StorageOffset: offset, StorageSize: fieldInfo.StorageSize,
        }
        order = append(order, field.Name)
        offset += fieldInfo.StorageSize
        maxAlign = max(maxAlign, align)
    }
    return out, order, offset - baseOffset, maxAlign
}
```

For enum variants, call this helper from inside `ensureTypeInfo(typ, visiting)` with the same `visiting` map, `baseOffset = 8`, prefix payload keys as `Variant.field`, and set enum `Size = alignUp(8+maxPayloadStorageSize, max(8, maxPayloadAlign))`.

Add this IR layout test to `compiler/ir/generic_test.go`:

```go
func TestLowerGenericEnumTypeInfo(t *testing.T) {
    program := lowerSourceForTest(t, `
module ir.enum_layout
enum Option<T> { None Some(value: T) }
data Event {
    first: U64
    second: U64
    kind: U32
}
executor Worker {
    start fn run(self, next: Option<Event>) -> never {
        while true {}
    }
}
`)
    info, ok := program.Types["ir.enum_layout.Option[ir.enum_layout.Event]"]
    if !ok {
        t.Fatalf("missing concrete Option<Event> type info: %#v", program.Types)
    }
    if info.Fields["$tag"].Offset != 0 || info.Fields["Some.value"].Offset != 8 {
        t.Fatalf("enum field offsets = %#v", info.Fields)
    }
    if info.Fields["Some.value"].StorageSize <= 8 || info.StorageSize != 32 {
        t.Fatalf("enum did not use semantic payload size: info=%#v", info)
    }
    if len(info.EnumVariants) != 2 || info.EnumVariants[0].Name != "None" || info.EnumVariants[1].Discriminant != 1 {
        t.Fatalf("enum variants = %#v", info.EnumVariants)
    }
}
```

- [ ] **Step 5: Verify**

Run:

```bash
go test ./compiler/layout -run TestEnumLayoutUsesDiscriminantAndPrimitiveMaxPayload -v
go test ./compiler/ir -run TestLowerGenericEnumTypeInfo -v
git diff --check
```

Expected: layout and IR tests PASS.

- [ ] **Step 6: Commit**

```bash
git add compiler/layout/record.go compiler/layout/record_test.go compiler/layout/enum.go compiler/layout/enum_test.go compiler/ir/ir.go compiler/ir/lower.go compiler/ir/generic_test.go
git commit -m "feat: lay out generic records and enums -Codex Automated"
```

### Task 11: Add IR Operations For Enums And Typed Slots

**Description:** Add IR values/ops that represent the new constructs explicitly before x86_64 emission.

**Files:**
- Modify: `compiler/ir/ir.go`
- Modify: `compiler/ir/lower.go`
- Create: `compiler/ir/enum_test.go`
- Modify: `compiler/ir/memory_test.go`

**Acceptance Criteria:**
- Variant constructors lower to `EnumConstruct`.
- `match` lowers to variant tests and arm bodies.
- `reserve_array` lowers to `ArenaReserveArray`.
- `Slots.write` lowers to `SlotWrite`.
- `Slots.fill` lowers to `SlotFill`.
- `Slice.get`, `MutableSlice.get`, and `MutableSlice.set` lower to slice ops.
- Source-visible compiler intrinsic methods are recognized by qualified owner and method name and do not lower to normal asm-method calls.

- [ ] **Step 1: Add failing IR tests**

Create `compiler/ir/enum_test.go`:

```go
package ir

import "testing"

func TestLowerEnumMatch(t *testing.T) {
    src := `
module ir.enums
enum Option<T> { None Some(value: T) }
data Event { kind: U64 }
executor Worker {
    start fn run(self, next: Option<Event>) -> never {
        match next {
            Option.Some(value = event) => { let k = event.kind }
            Option.None => { let z = 0 }
        }
        while true {}
    }
}
`
    program := lowerSourceForTest(t, src)
    fn := findFunction(program, "_wrela_method_ir_enums_Worker_run")
    if fn == nil {
        t.Fatal("missing Worker.run")
    }
    if !containsOp[*EnumVariantTest](*fn) || !containsOp[*EnumPayloadExtract](*fn) {
        t.Fatalf("lowered ops missing enum match operations: %#v", fn.Blocks[0].Ops)
    }
}

func TestLowerMatchBindsPayloadBeforeArmBody(t *testing.T) {
    program := lowerSourceForTest(t, `
module ir.enums
enum Option<T> { None Some(value: T) }
data Event { kind: U64 }
class Worker {
    fn consume(self, event: Event) {}
    start fn run(self, next: Option<Event>) -> never {
        match next {
            Option.Some(value = event) => { self.consume(event = event) }
            Option.None => {}
        }
        while true {}
    }
}
`)
    fn := findFunction(program, "_wrela_method_ir_enums_Worker_run")
    if fn == nil {
        t.Fatal("missing Worker.run")
    }
    extractAt := enumPayloadExtractIndex(*fn)
    callAt := callIndex(*fn, "_wrela_method_ir_enums_Worker_consume")
    if extractAt < 0 || callAt < 0 || extractAt > callAt {
        t.Fatalf("payload extract must happen before consume call: extract=%d call=%d blocks=%#v", extractAt, callAt, fn.Blocks)
    }
}

func enumPayloadExtractIndex(fn Function) int {
    ordinal := 0
    for bi := range fn.Blocks {
        if at := enumPayloadExtractIndexOps(fn.Blocks[bi].Ops, &ordinal); at >= 0 {
            return at
        }
    }
    return -1
}

func enumPayloadExtractIndexOps(ops []Operation, ordinal *int) int {
    for _, op := range ops {
        current := *ordinal
        *ordinal = *ordinal + 1
        if _, ok := op.(*EnumPayloadExtract); ok {
            return current
        }
        if branch, ok := op.(*If); ok {
            if at := enumPayloadExtractIndexOps(branch.Then, ordinal); at >= 0 {
                return at
            }
            if at := enumPayloadExtractIndexOps(branch.Else, ordinal); at >= 0 {
                return at
            }
        }
    }
    return -1
}

func callIndex(fn Function, symbol string) int {
    ordinal := 0
    for bi := range fn.Blocks {
        if at := callIndexOps(fn.Blocks[bi].Ops, symbol, &ordinal); at >= 0 {
            return at
        }
    }
    return -1
}

func callIndexOps(ops []Operation, symbol string, ordinal *int) int {
    for _, op := range ops {
        current := *ordinal
        *ordinal = *ordinal + 1
        call, ok := op.(*Call)
        if ok && call.Symbol == symbol {
            return current
        }
        if branch, ok := op.(*If); ok {
            if at := callIndexOps(branch.Then, symbol, ordinal); at >= 0 {
                return at
            }
            if at := callIndexOps(branch.Else, symbol, ordinal); at >= 0 {
                return at
            }
        }
    }
    return -1
}
```

Append to `compiler/ir/memory_test.go`:

```go
func TestLowerReserveArrayAndSlotWrite(t *testing.T) {
    src := `
module machine.x86_64.executor_memory
data Event { kind: U64 }
data Slots<T> {
    address: PhysicalAddress
    capacity: U64
    fn write(self, index: U64, value: T) {}
}
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
}
executor Worker {
    memory: ExecutorMemory
    start fn run(self) -> never {
        with self.memory.frame(length = 4096) as tick {
            let slots = tick.reserve_array(Event, count = 4)
            slots.write(index = 0, value = Event(kind = 7))
        }
        while true {}
    }
}
`
    program := lowerSourceForTest(t, src)
    fn := findFunction(program, "_wrela_method_machine_x86_64_executor_memory_Worker_run")
    if fn == nil {
        t.Fatal("missing Worker.run")
    }
    if !containsOp[*ArenaReserveArray](*fn) || !containsOp[*SlotWrite](*fn) {
        t.Fatalf("lowered ops missing reserve array or slot write: %#v", fn.Blocks[0].Ops)
    }
    if functionCalls(*fn, "_wrela_method_machine_x86_64_executor_memory_Slots_Event_write") {
        t.Fatalf("Slots<Event>.write lowered as a normal method call instead of SlotWrite: %#v", fn.Blocks)
    }
}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./compiler/ir -run 'TestLowerEnumMatch|TestLowerMatchBindsPayloadBeforeArmBody|TestLowerReserveArrayAndSlotWrite' -v`

Expected: FAIL.

- [ ] **Step 3: Add IR operation types**

Add to `compiler/ir/ir.go`:

```go
type EnumConstruct struct {
    Symbol  string
    Type    Type
    Variant string
    Fields  []FieldValue
}

func (*EnumConstruct) isValue()     {}
func (*EnumConstruct) isOperation() {}

type EnumVariantTest struct {
    Value   Value
    Type    Type
    Variant string
}

func (*EnumVariantTest) isValue()     {}
func (*EnumVariantTest) isOperation() {}

type EnumPayloadExtract struct {
    Value   Value
    Type    Type
    Variant string
    Field   string
}

func (*EnumPayloadExtract) isValue()     {}
func (*EnumPayloadExtract) isOperation() {}

type ArenaReserveArray struct {
    Arena   Value
    Element Type
    Count   Value
    Align   Value
    Type    Type
}

func (*ArenaReserveArray) isValue()     {}
func (*ArenaReserveArray) isOperation() {}

type SlotWrite struct {
    Slots Value
    Index Value
    Value Value
}

func (*SlotWrite) isOperation() {}

type SlotFill struct {
    Slots   Value
    Value   Value
    Element Type
    Type    Type
}

func (*SlotFill) isValue()     {}
func (*SlotFill) isOperation() {}

type SliceGet struct {
    Slice Value
    Index Value
    Type  Type
}

func (*SliceGet) isValue()     {}
func (*SliceGet) isOperation() {}

type SliceSet struct {
    Slice Value
    Index Value
    Value Value
}

func (*SliceSet) isOperation() {}
```

Update `valuesDefinedBy` for value-producing ops.

- [ ] **Step 4: Lower new constructs**

Add a small intrinsic registry in `compiler/ir/lower.go` before modifying call lowering. This extends the current hard-coded intrinsic style (`isCanonicalFrameIntrinsic`) to every source-visible generic intrinsic, and prevents calls to asm stubs whose body is only `ret`:

```go
type intrinsicKind int

const (
    intrinsicNone intrinsicKind = iota
    intrinsicSlotsWrite
    intrinsicSlotsFill
    intrinsicSliceGet
    intrinsicMutableSliceGet
    intrinsicMutableSliceSet
    intrinsicTopicPublish
    intrinsicReliableTryPublish
    intrinsicTopicTryNext
)

func (ctx *lowerContext) intrinsicForCall(receiverType *sem.Type, method string) intrinsicKind {
    if receiverType == nil {
        return intrinsicNone
    }
    q := qualifiedTypeName(receiverType)
    switch {
    case q == "machine.x86_64.executor_memory.Slots" && method == "write":
        return intrinsicSlotsWrite
    case q == "machine.x86_64.executor_memory.Slots" && method == "fill":
        return intrinsicSlotsFill
    case q == "machine.x86_64.executor_memory.Slice" && method == "get":
        return intrinsicSliceGet
    case q == "machine.x86_64.executor_memory.MutableSlice" && method == "get":
        return intrinsicMutableSliceGet
    case q == "machine.x86_64.executor_memory.MutableSlice" && method == "set":
        return intrinsicMutableSliceSet
    case q == "machine.x86_64.topic.TopicPublisher" && method == "publish":
        return intrinsicTopicPublish
    case q == "machine.x86_64.topic.ReliablePublisher" && method == "try_publish":
        return intrinsicReliableTryPublish
    case (q == "machine.x86_64.topic.TopicSubscription" || q == "machine.x86_64.topic.ReliableSubscription") && method == "try_next":
        return intrinsicTopicTryNext
    default:
        return intrinsicNone
    }
}
```

`qualifiedTypeName` must use the generic origin when present:

```go
func qualifiedTypeName(t *sem.Type) string {
    if t == nil {
        return ""
    }
    if t.GenericOrigin != nil {
        t = t.GenericOrigin
    }
    if t.Module == "" {
        return t.Name
    }
    return t.Module + "." + t.Name
}
```

At the start of `lowerExpr`'s `*ast.CallExpr` branch, after the receiver is lowered and `receiverType` is known but before normal method-call symbol construction, switch on `ctx.intrinsicForCall(receiverType, e.Method)`. Every non-`intrinsicNone` case must return IR operations directly and must not emit an `ir.Call` to the asm method symbol.

In `lowerExpr`, handle `VariantConstructorExpr` by creating `EnumConstruct`. In statement lowering, lower `MatchStmt` to an `If` chain using `EnumVariantTest` conditions. The first matching arm executes; wildcard becomes the final else body.

Use this helper shape so arm scopes, payload bindings, wildcard bodies, and operation ordering are explicit:

```go
func (ctx *lowerContext) lowerMatchStmt(moduleName string, receiverType *sem.Type, scope *lowerScope, assigned map[string]bool, stmt *ast.MatchStmt) []Operation {
    value, valueOps, valueType := ctx.lowerExpr(moduleName, receiverType, scope, stmt.Value)
    out := append([]Operation{}, valueOps...)
    out = append(out, ctx.lowerMatchArms(moduleName, receiverType, scope, assigned, value, valueType, stmt.Arms, 0)...)
    return out
}

func (ctx *lowerContext) lowerMatchArms(moduleName string, receiverType *sem.Type, scope *lowerScope, assigned map[string]bool, value Value, valueType *sem.Type, arms []ast.MatchArm, index int) []Operation {
    if index >= len(arms) {
        return nil
    }
    arm := arms[index]
    if _, ok := arm.Pattern.(ast.WildcardPattern); ok {
        return ctx.lowerStmtList(moduleName, receiverType, newLowerScope(scope), assigned, arm.Body)
    }
    pattern := arm.Pattern.(ast.VariantPattern)
    variant, ok := ctx.enumVariantForType(valueType, pattern.Variant)
    if !ok {
        ctx.errorf("unknown enum variant %s on %s", pattern.Variant, valueType.Display())
        return nil
    }
    test := &EnumVariantTest{Value: value, Type: ctx.irType(valueType), Variant: variant.Name}
    armScope := newLowerScope(scope)
    thenOps := []Operation{}
    for _, binding := range pattern.Bindings {
        extract := &EnumPayloadExtract{Value: value, Type: ctx.irType(valueType), Variant: variant.Name, Field: binding.Name}
        thenOps = append(thenOps, extract)
        armScope.define(binding.Bind, lowerBinding{value: extract, typ: enumVariantFieldType(variant, binding.Name)})
    }
    thenOps = append(thenOps, ctx.lowerStmtList(moduleName, receiverType, armScope, assigned, arm.Body)...)
    elseOps := ctx.lowerMatchArms(moduleName, receiverType, scope, assigned, value, valueType, arms, index+1)
    return []Operation{&If{ConditionOps: []Operation{test}, Condition: test, Then: thenOps, Else: elseOps}}
}

func (ctx *lowerContext) enumVariantForType(valueType *sem.Type, name string) (sem.EnumVariant, bool) {
    if valueType == nil {
        return sem.EnumVariant{}, false
    }
    variants := valueType.EnumVariants
    if len(variants) == 0 && valueType.GenericOrigin != nil {
        variants = valueType.GenericOrigin.EnumVariants
    }
    for _, variant := range variants {
        if variant.Name == name {
            return variant, true
        }
    }
    return sem.EnumVariant{}, false
}

func enumVariantFieldType(variant sem.EnumVariant, fieldName string) *sem.Type {
    for _, field := range variant.Fields {
        if field.Name == fieldName {
            return field.Type
        }
    }
    return nil
}
```

`EnumPayloadExtract` operations must be appended before lowering the arm body so calls and field loads in the body use the bound payload value, not the whole enum.

Wire `MatchStmt` in `lowerStmt` with the existing lowerer signature:

```go
case *ast.MatchStmt:
    return ctx.lowerMatchStmt(moduleName, receiverType, scope, assigned, s)
case *ast.IfLetStmt:
    return ctx.lowerIfLetStmt(moduleName, receiverType, scope, assigned, s)
```

Lower `if let` as the same variant test with an empty else:

```go
func (ctx *lowerContext) lowerIfLetStmt(moduleName string, receiverType *sem.Type, scope *lowerScope, assigned map[string]bool, stmt *ast.IfLetStmt) []Operation {
    value, valueOps, valueType := ctx.lowerExpr(moduleName, receiverType, scope, stmt.Value)
    pattern := stmt.Pattern.(ast.VariantPattern)
    variant, ok := ctx.enumVariantForType(valueType, pattern.Variant)
    if !ok {
        return valueOps
    }
    test := &EnumVariantTest{Value: value, Type: ctx.irType(valueType), Variant: variant.Name}
    armScope := newLowerScope(scope)
    thenOps := []Operation{}
    for _, binding := range pattern.Bindings {
        extract := &EnumPayloadExtract{Value: value, Type: ctx.irType(valueType), Variant: variant.Name, Field: binding.Name}
        thenOps = append(thenOps, extract)
        armScope.define(binding.Bind, lowerBinding{value: extract, typ: enumVariantFieldType(variant, binding.Name)})
    }
    thenOps = append(thenOps, ctx.lowerStmtList(moduleName, receiverType, armScope, assigned, stmt.Body)...)
    out := append([]Operation{}, valueOps...)
    out = append(out, &If{ConditionOps: []Operation{test}, Condition: test, Then: thenOps})
    return out
}
```

For `reserve_array`, compute the concrete `Element` from semantic type info and emit:

```go
reserve := &ArenaReserveArray{
    Arena:   receiver,
    Element: ctx.irType(elementType),
    Count:   countValue,
    Align:   nil,
    Type:    ctx.irType(slotsType),
}
```

For `slots.write(index = i, value = v)`, emit `SlotWrite`.

For source-visible intrinsic methods, lower the method call directly to the intrinsic operation and do not emit a normal `ir.Call`:

```go
switch {
case isSlotsType(receiverType) && expr.Method == "write":
    return nil, []Operation{&SlotWrite{Slots: receiver, Index: indexValue, Value: valueValue}}
case isSlotsType(receiverType) && expr.Method == "fill":
    return &SlotFill{Slots: receiver, Value: valueValue, Element: ctx.irType(receiverType.TypeArgs[0]), Type: ctx.irType(callReturnType)}, nil
case isSliceType(receiverType) && expr.Method == "get":
    return &SliceGet{Slice: receiver, Index: indexValue, Type: ctx.irType(receiverType.TypeArgs[0])}, nil
case receiverType.Name == "MutableSlice" && expr.Method == "set":
    return nil, []Operation{&SliceSet{Slice: receiver, Index: indexValue, Value: valueValue}}
}
```

`callReturnType` in the `fill` case is the semantic return type already resolved for `Slots<T>.fill`, namely `MutableSlice<T>`.

- [ ] **Step 5: Verify**

Run:

```bash
go test ./compiler/ir -run 'TestLowerEnumMatch|TestLowerMatchBindsPayloadBeforeArmBody|TestLowerReserveArrayAndSlotWrite' -v
git diff --check
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add compiler/ir/ir.go compiler/ir/lower.go compiler/ir/enum_test.go compiler/ir/memory_test.go
git commit -m "feat: lower enums and typed slot operations -Codex Automated"
```

### Task 12A: Emit x86_64 For Enum Operations

**Description:** Emit concrete machine code for enum construction, variant testing, and payload extraction.

**Files:**
- Modify: `compiler/codegen/x64.go`
- Create: `compiler/codegen/enum_test.go`

**Acceptance Criteria:**
- `EnumConstruct` writes the discriminant at `$tag` offset `0`.
- `EnumConstruct` copies each payload field to the variant payload offset from `ir.TypeInfo.Fields`.
- `EnumVariantTest` compares the `$tag` value against the variant discriminant.
- `EnumPayloadExtract` loads from the exact payload field offset.
- Tests assert discriminant value and payload offset bytes in addition to opcode presence.

- [ ] **Step 1: Add failing enum codegen test**

Create `compiler/codegen/enum_test.go`:

```go
package codegen

import (
    "bytes"
    "testing"

    "github.com/ryanwible/wrela3/compiler/ir"
)

func testEnumTypeInfos() map[string]ir.TypeInfo {
    u64 := ir.Type{Name: "U64", Kind: ir.TypeKindPrimitive}
    event := ir.Type{Name: "Event", Kind: ir.TypeKindData}
    return map[string]ir.TypeInfo{
        "Event": {Name: "Event", Kind: ir.TypeKindData, Size: 8, Align: 8, StorageSize: 8, Fields: map[string]ir.FieldInfo{
            "kind": {Name: "kind", Type: u64, Offset: 0, Size: 8, Align: 8, StorageOffset: 0, StorageSize: 8},
        }},
        "Option<Event>": {
            Name: "Option<Event>", Kind: ir.TypeKindEnum, Size: 16, Align: 8, StorageSize: 16,
            Fields: map[string]ir.FieldInfo{
                "$tag":       {Name: "$tag", Type: u64, Offset: 0, Size: 8, Align: 8, StorageOffset: 0, StorageSize: 8},
                "Some.value": {Name: "Some.value", Type: event, Offset: 8, Size: 8, Align: 8, StorageOffset: 8, StorageSize: 8},
            },
            EnumVariants: []ir.EnumVariantInfo{
                {Name: "None", Discriminant: 0},
                {Name: "Some", Discriminant: 1, Fields: []string{"Some.value"}},
            },
        },
    }
}

func TestEnumConstructStoresDiscriminantAndPayload(t *testing.T) {
    event := ir.Local{Symbol: "event", Type: ir.Type{Name: "Event"}}
    program := &ir.Program{Types: testEnumTypeInfos(), Functions: []ir.Function{{
        Symbol: "_wrela_test_enum_construct",
        Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
            event,
            &ir.EnumConstruct{
                Symbol:  "next",
                Type:    ir.Type{Name: "Option<Event>"},
                Variant: "Some",
                Fields:  []ir.FieldValue{{Name: "value", Value: event}},
            },
        }}},
    }}}
    image, ds := Compile(program)
    if len(ds) != 0 {
        t.Fatalf("Compile diagnostics: %#v", ds)
    }
    code := symbolBytes(t, image, "_wrela_test_enum_construct")
    if !bytes.Contains(code, []byte{0x48, 0xC7}) {
        t.Fatalf("enum constructor must emit an immediate discriminant store, got %#x", code)
    }
    if !bytes.Contains(code, []byte{0x89}) && !bytes.Contains(code, []byte{0x8B}) {
        t.Fatalf("enum constructor must copy payload bytes, got %#x", code)
    }
    if !containsBytes(code, []byte{0x01, 0x00, 0x00, 0x00}) {
        t.Fatalf("enum constructor must store Some discriminant 1, got %#x", code)
    }
    if !containsBytes(code, []byte{0x08, 0x00, 0x00, 0x00}) {
        t.Fatalf("enum constructor must use Some.value payload offset 8, got %#x", code)
    }
}

func TestEnumVariantTestComparesTag(t *testing.T) {
    next := ir.Local{Symbol: "next", Type: ir.Type{Name: "Option<Event>"}}
    program := &ir.Program{Types: testEnumTypeInfos(), Functions: []ir.Function{{
        Symbol: "_wrela_test_enum_variant_test",
        Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
            next,
            &ir.EnumVariantTest{Value: next, Type: ir.Type{Name: "Option<Event>"}, Variant: "Some"},
        }}},
    }}}
    image, ds := Compile(program)
    if len(ds) != 0 {
        t.Fatalf("Compile diagnostics: %#v", ds)
    }
    code := symbolBytes(t, image, "_wrela_test_enum_variant_test")
    if !bytes.Contains(code, []byte{0x48, 0x83}) && !bytes.Contains(code, []byte{0x48, 0x81}) {
        t.Fatalf("variant test must emit a 64-bit compare against the tag, got %#x", code)
    }
    if !containsBytes(code, []byte{0x01, 0x00, 0x00, 0x00}) {
        t.Fatalf("variant test must compare against Some discriminant 1, got %#x", code)
    }
}

func TestEnumPayloadExtractLoadsPayloadOffset(t *testing.T) {
    next := ir.Local{Symbol: "next", Type: ir.Type{Name: "Option<Event>"}}
    program := &ir.Program{Types: testEnumTypeInfos(), Functions: []ir.Function{{
        Symbol: "_wrela_test_enum_payload_extract",
        Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
            next,
            &ir.EnumPayloadExtract{Value: next, Type: ir.Type{Name: "Option<Event>"}, Variant: "Some", Field: "value"},
        }}},
    }}}
    image, ds := Compile(program)
    if len(ds) != 0 {
        t.Fatalf("Compile diagnostics: %#v", ds)
    }
    code := symbolBytes(t, image, "_wrela_test_enum_payload_extract")
    if !bytes.Contains(code, []byte{0x8B}) && !bytes.Contains(code, []byte{0x8D}) && !bytes.Contains(code, []byte{0x89}) {
        t.Fatalf("payload extract must emit a load or address calculation, got %#x", code)
    }
    if !containsBytes(code, []byte{0x08, 0x00, 0x00, 0x00}) {
        t.Fatalf("payload extract must use Some.value offset 8, got %#x", code)
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./compiler/codegen -run 'TestEnumConstructStoresDiscriminantAndPayload|TestEnumVariantTestComparesTag|TestEnumPayloadExtractLoadsPayloadOffset' -v`

Expected: FAIL because enum codegen is missing.

- [ ] **Step 3: Add enum dispatch and emitters**

In the operation dispatch in `compiler/codegen/x64.go`, add only these enum cases:

```go
case *ir.EnumConstruct:
    emitEnumConstruct(e, v, frame, ctx)
case *ir.EnumVariantTest:
    emitEnumVariantTest(e, v, frame, ctx)
case *ir.EnumPayloadExtract:
    emitEnumPayloadExtract(e, v, frame, ctx)
```

Implementation rules:
- Variant discriminants are read from `ctx.program.Types[v.Type.Name].EnumVariants`; do not rederive them in codegen.
- The `$tag` field offset is read from `ctx.program.Types[v.Type.Name].Fields["$tag"]`.
- Payload field offsets use keys of the form `VariantName.fieldName`, for example `Some.value`.
- Store and load sizes use `FieldInfo.StorageSize`; do not assume every payload is `U64`.

- [ ] **Step 4: Verify**

Run:

```bash
go test ./compiler/codegen -run 'TestEnumConstructStoresDiscriminantAndPayload|TestEnumVariantTestComparesTag|TestEnumPayloadExtractLoadsPayloadOffset' -v
git diff --check
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add compiler/codegen/x64.go compiler/codegen/enum_test.go
git commit -m "feat: emit enum operations -Codex Automated"
```

### Task 12B: Emit x86_64 For ArenaReserveArray

**Description:** Emit checked array reservations for `ArenaReserveArray` using the existing arena bump cursor and `_wrela_memory_oom` trap path.

**Files:**
- Modify: `compiler/codegen/x64.go`
- Modify: `compiler/codegen/memory_test.go`

**Acceptance Criteria:**
- `ArenaReserveArray` computes `byte_count = count * sizeof(element)`.
- Multiplication overflow branches or calls to `_wrela_memory_oom`.
- Arena end overflow or exhaustion branches or calls to `_wrela_memory_oom`.
- Returned value is a `Slots<T>` pair with address and `capacity = count`, not byte length.
- Tests assert field-offset constants for `ArenaFrame` and `Slots<T>` layout, not only broad opcode presence.

- [ ] **Step 1: Add failing reserve-array test and helpers**

Append to `compiler/codegen/memory_test.go`:

```go
func genericMemoryTypeInfos() map[string]ir.TypeInfo {
    event := ir.Type{Name: "Event", Kind: ir.TypeKindData}
    u64 := ir.Type{Name: "U64", Kind: ir.TypeKindPrimitive}
    phys := ir.Type{Name: "PhysicalAddress", Kind: ir.TypeKindPrimitive}
    return map[string]ir.TypeInfo{
        "Event": {Name: "Event", Kind: ir.TypeKindData, Size: 8, Align: 8, StorageSize: 8, Fields: map[string]ir.FieldInfo{
            "kind": {Name: "kind", Type: u64, Offset: 0, Size: 8, Align: 8, StorageOffset: 0, StorageSize: 8},
        }},
        "ArenaFrame": {Name: "ArenaFrame", Kind: ir.TypeKindClass, Size: 24, Align: 8, StorageSize: 24, Fields: map[string]ir.FieldInfo{
            "arena_base":   {Name: "arena_base", Type: phys, Offset: 0, Size: 8, Align: 8, StorageOffset: 0, StorageSize: 8},
            "arena_length": {Name: "arena_length", Type: u64, Offset: 8, Size: 8, Align: 8, StorageOffset: 8, StorageSize: 8},
            "next_offset":  {Name: "next_offset", Type: u64, Offset: 16, Size: 8, Align: 8, StorageOffset: 16, StorageSize: 8},
        }, FieldOrder: []string{"arena_base", "arena_length", "next_offset"}},
        "Slots<Event>": {Name: "Slots<Event>", Kind: ir.TypeKindData, Size: 16, Align: 8, StorageSize: 16, Fields: map[string]ir.FieldInfo{
            "address":  {Name: "address", Type: phys, Offset: 0, Size: 8, Align: 8, StorageOffset: 0, StorageSize: 8},
            "capacity": {Name: "capacity", Type: u64, Offset: 8, Size: 8, Align: 8, StorageOffset: 8, StorageSize: 8},
        }, FieldOrder: []string{"address", "capacity"}},
    }
}

func testProgramWithArenaReserveArray(t *testing.T) *ir.Program {
    t.Helper()
    arena := ir.Local{Symbol: "arena", Type: ir.Type{Name: "ArenaFrame"}}
    count := ir.ConstInt{Symbol: "count", Value: 3, Type: ir.Type{Name: "U64"}}
    return &ir.Program{Types: genericMemoryTypeInfos(), Functions: []ir.Function{{
        Symbol: "_wrela_test_reserve_array",
        Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
            arena,
            count,
            &ir.ArenaReserveArray{Arena: arena, Element: ir.Type{Name: "Event"}, Count: count, Type: ir.Type{Name: "Slots<Event>"}},
        }}},
    }}}
}

func TestArenaReserveArrayEmitsOverflowAndBoundsTrap(t *testing.T) {
    program := testProgramWithArenaReserveArray(t)
    image, ds := Compile(program)
    if len(ds) != 0 {
        t.Fatalf("Compile diagnostics: %#v", ds)
    }
    code := symbolBytes(t, image, "_wrela_test_reserve_array")
    if !bytes.Contains(code, []byte{0x48, 0xF7}) {
        t.Fatalf("reserve_array must multiply count by sizeof(element), got %#x", code)
    }
    for name, want := range map[string][]byte{
        "ArenaFrame.arena_length offset": {0x08, 0x00, 0x00, 0x00},
        "ArenaFrame.next_offset offset":  {0x10, 0x00, 0x00, 0x00},
        "Slots.capacity offset":          {0x08, 0x00, 0x00, 0x00},
        "Event storage size":             {0x08, 0x00, 0x00, 0x00},
        "requested capacity count":        {0x03, 0x00, 0x00, 0x00},
    } {
        if !containsBytes(code, want) {
            t.Fatalf("reserve_array missing %s constant %x in %x", name, want, code)
        }
    }
    if got := countBytes(code, []byte{0x0F, 0x83}); got < 2 {
        t.Fatalf("reserve_array must include unsigned overflow/bounds branches, got %d jae branches in %x", got, code)
    }
    if !codeCallsSymbol(t, image, "_wrela_test_reserve_array", "_wrela_memory_oom") {
        t.Fatal("reserve_array must branch/call to _wrela_memory_oom on overflow or arena exhaustion")
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./compiler/codegen -run TestArenaReserveArrayEmitsOverflowAndBoundsTrap -v`

Expected: FAIL because `ArenaReserveArray` is not emitted.

- [ ] **Step 3: Add reserve-array dispatch and emitter**

In the operation dispatch, add:

```go
case *ir.ArenaReserveArray:
    emitArenaReserveArray(e, v, frame, ctx)
```

`emitArenaReserveArray` must be a new emitter. Do not call `emitArenaReserve`, because that emitter stores `{address, length_bytes}` for `Bytes`/`MutableBytes`; `Slots<T>` must store `{address, capacity_count}`.

`emitArenaReserveArray` must compute:

```text
size = count * sizeof(element)
align = max(alignof(element), explicit align if provided)
```

Use `mul` for multiplication, branch to `_wrela_memory_oom` when the high half is nonzero, then call `emitArenaBump(e, frame, op.Arena, byteCountReg, alignReg)`. Store the returned `Slots<T>` address at offset `0` and the original count register at offset `8`.

Use this register discipline:

```go
countReg := asm.MustLookup("r12")     // original element count; preserve until final capacity store
byteCountReg := asm.MustLookup("r10") // count * element storage size
alignReg := asm.MustLookup("r9")
elementReg := asm.MustLookup("rbx")

emitLoadValue(e, frame, op.Count, countReg)
emitRegRegMove(e, byteCountReg, countReg)
emitMovImmToReg(e, elementReg, int64(elementStorageSize))
emitUnsignedMulInto(e, byteCountReg, elementReg) // leaves product in byteCountReg, traps on high half
emitMovImmToReg(e, alignReg, int64(elementAlign))
address, ok := emitArenaBump(e, frame, op.Arena, byteCountReg, alignReg)
if !ok {
    return
}
emitStoreSlotFromReg(e, address, objectSlot, 64)        // Slots.address
emitStoreSlotFromReg(e, countReg, objectSlot+8, 64)     // Slots.capacity, not byteCountReg
emitSlotFromBase(e, asm.MustLookup("rdi"), asm.MustLookup("rbp"), objectSlot)
emitStoreSlotFromReg(e, asm.MustLookup("rdi"), slot, 64)
```

If `r12` is not available in the local emitter helper set, use any non-conflicting scratch register already used by nearby codegen helpers, but the implementation must preserve the original count across the byte-count multiplication.

Add `emitUnsignedMulInto` in `compiler/codegen/x64.go` if no equivalent helper exists:

```go
func emitUnsignedMulInto(e *Emitter, dst asm.Reg, rhs asm.Reg) {
    rax := asm.MustLookup("rax")
    rdx := asm.MustLookup("rdx")
    emitRegRegMove(e, rax, dst)
    e.emitInstruction(asm.Instruction{Mnemonic: "mul", Operands: []asm.Operand{asm.RegOperand{Reg: rhs}}}) // RDX:RAX = RAX * rhs
    ok := e.newLabel("mul_no_overflow")
    emitMovImmToReg(e, rhs, 0)
    emitCmpRegReg(e, rdx, rhs)
    e.emitJcc(0x84, ok) // je
    emitCallReloc(e, "_wrela_memory_oom")
    e.bindLabel(ok)
    emitRegRegMove(e, dst, rax)
}
```

- [ ] **Step 4: Verify**

Run:

```bash
go test ./compiler/codegen -run TestArenaReserveArrayEmitsOverflowAndBoundsTrap -v
git diff --check
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add compiler/codegen/x64.go compiler/codegen/memory_test.go
git commit -m "feat: emit checked arena array reservations -Codex Automated"
```

### Task 12C: Emit x86_64 For SlotWrite

**Description:** Emit bounds-checked writes through `Slots<T>` values.

**Files:**
- Modify: `compiler/codegen/x64.go`
- Modify: `compiler/codegen/memory_test.go`

**Acceptance Criteria:**
- `SlotWrite` loads slots address from offset `0`.
- `SlotWrite` loads slots capacity from offset `8`.
- `SlotWrite` compares `index >= capacity` and traps through `_wrela_memory_oom`.
- `SlotWrite` stores `value` at `address + index*sizeof(T)`.
- Tests assert the capacity offset, element size, bounds branch, and `_wrela_memory_oom` call target.

- [ ] **Step 1: Add failing slot-write test**

Append to `compiler/codegen/memory_test.go`, reusing `genericMemoryTypeInfos` from Task 12B:

```go
func testProgramWithSlotWrite(t *testing.T) *ir.Program {
    t.Helper()
    slots := ir.Local{Symbol: "slots", Type: ir.Type{Name: "Slots<Event>"}}
    index := ir.ConstInt{Symbol: "index", Value: 0, Type: ir.Type{Name: "U64"}}
    value := ir.Local{Symbol: "event", Type: ir.Type{Name: "Event"}}
    return &ir.Program{Types: genericMemoryTypeInfos(), Functions: []ir.Function{{
        Symbol: "_wrela_test_slot_write",
        Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
            slots,
            index,
            value,
            &ir.SlotWrite{Slots: slots, Index: index, Value: value},
        }}},
    }}}
}

func TestSlotWriteBounds(t *testing.T) {
    program := testProgramWithSlotWrite(t)
    image, ds := Compile(program)
    if len(ds) != 0 {
        t.Fatalf("Compile diagnostics: %#v", ds)
    }
    code := symbolBytes(t, image, "_wrela_test_slot_write")
    if !bytes.Contains(code, []byte{0x0F, 0x83}) && !bytes.Contains(code, []byte{0x73}) {
        t.Fatalf("slot write must emit jae/jnc style bounds branch, got %#x", code)
    }
    for name, want := range map[string][]byte{
        "Slots.capacity offset": {0x08, 0x00, 0x00, 0x00},
        "Event storage size":    {0x08, 0x00, 0x00, 0x00},
    } {
        if !containsBytes(code, want) {
            t.Fatalf("slot write missing %s constant %x in %x", name, want, code)
        }
    }
    if !codeCallsSymbol(t, image, "_wrela_test_slot_write", "_wrela_memory_oom") {
        t.Fatal("slot write must branch/call to _wrela_memory_oom when index >= capacity")
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./compiler/codegen -run TestSlotWriteBounds -v`

Expected: FAIL because `SlotWrite` is not emitted.

- [ ] **Step 3: Add shared index bounds check and slot emitter**

Add:

```go
func emitIndexBoundsCheck(e *Emitter, indexReg Reg64, lengthReg Reg64, trapLabel string) {
    e.emitREX(0x48)
    e.emitBytes(0x39, modRM(3, indexReg.low3(), lengthReg.low3())) // cmp length, index
    e.emitJcc(0x83, trapLabel) // jae trap
}
```

In the operation dispatch, add:

```go
case *ir.SlotWrite:
    emitSlotWrite(e, v, frame, ctx)
```

Use `_wrela_memory_oom` as the trap path for this milestone. Do not introduce a second runtime trap symbol.

- [ ] **Step 4: Verify**

Run:

```bash
go test ./compiler/codegen -run TestSlotWriteBounds -v
git diff --check
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add compiler/codegen/x64.go compiler/codegen/memory_test.go
git commit -m "feat: emit checked slot writes -Codex Automated"
```

### Task 12D: Emit x86_64 For Slice Get And Set

**Description:** Emit bounds-checked loads and stores through `Slice<T>` and `MutableSlice<T>`.

**Files:**
- Modify: `compiler/codegen/x64.go`
- Modify: `compiler/codegen/memory_test.go`

**Acceptance Criteria:**
- `SliceGet` loads address from offset `0` and length from offset `8`.
- `SliceGet` traps when `index >= length`.
- `SliceSet` uses the same bounds check.
- `SliceSet` stores `value` at `address + index*sizeof(T)`.
- Tests assert length offset, element size, bounds branch, and `_wrela_memory_oom` call target.

- [ ] **Step 1: Add failing slice tests**

Append the slice type infos to `genericMemoryTypeInfos` from Task 12B:

```go
"Slice<Event>": {Name: "Slice<Event>", Kind: ir.TypeKindData, Size: 16, Align: 8, StorageSize: 16, Fields: map[string]ir.FieldInfo{
    "address": {Name: "address", Type: phys, Offset: 0, Size: 8, Align: 8, StorageOffset: 0, StorageSize: 8},
    "length":  {Name: "length", Type: u64, Offset: 8, Size: 8, Align: 8, StorageOffset: 8, StorageSize: 8},
}, FieldOrder: []string{"address", "length"}},
"MutableSlice<Event>": {Name: "MutableSlice<Event>", Kind: ir.TypeKindData, Size: 16, Align: 8, StorageSize: 16, Fields: map[string]ir.FieldInfo{
    "address": {Name: "address", Type: phys, Offset: 0, Size: 8, Align: 8, StorageOffset: 0, StorageSize: 8},
    "length":  {Name: "length", Type: u64, Offset: 8, Size: 8, Align: 8, StorageOffset: 8, StorageSize: 8},
}, FieldOrder: []string{"address", "length"}},
```

Append:

```go
func testProgramWithSliceGetSet(t *testing.T) *ir.Program {
    t.Helper()
    slice := ir.Local{Symbol: "slice", Type: ir.Type{Name: "MutableSlice<Event>"}}
    index := ir.ConstInt{Symbol: "index", Value: 0, Type: ir.Type{Name: "U64"}}
    value := ir.Local{Symbol: "event", Type: ir.Type{Name: "Event"}}
    return &ir.Program{Types: genericMemoryTypeInfos(), Functions: []ir.Function{{
        Symbol: "_wrela_test_slice_bounds",
        Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
            slice,
            index,
            value,
            &ir.SliceGet{Slice: slice, Index: index, Type: ir.Type{Name: "Event"}},
            &ir.SliceSet{Slice: slice, Index: index, Value: value},
        }}},
    }}}
}

func TestSliceGetSetBounds(t *testing.T) {
    program := testProgramWithSliceGetSet(t)
    image, ds := Compile(program)
    if len(ds) != 0 {
        t.Fatalf("Compile diagnostics: %#v", ds)
    }
    code := symbolBytes(t, image, "_wrela_test_slice_bounds")
    if !bytes.Contains(code, []byte{0x0F, 0x83}) && !bytes.Contains(code, []byte{0x73}) {
        t.Fatalf("slice get/set must emit bounds branch, got %#x", code)
    }
    for name, want := range map[string][]byte{
        "MutableSlice.length offset": {0x08, 0x00, 0x00, 0x00},
        "Event storage size":         {0x08, 0x00, 0x00, 0x00},
    } {
        if !containsBytes(code, want) {
            t.Fatalf("slice get/set missing %s constant %x in %x", name, want, code)
        }
    }
    if !codeCallsSymbol(t, image, "_wrela_test_slice_bounds", "_wrela_memory_oom") {
        t.Fatal("slice get/set must branch/call to _wrela_memory_oom when index >= length")
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./compiler/codegen -run TestSliceGetSetBounds -v`

Expected: FAIL because `SliceGet` and `SliceSet` are not emitted.

- [ ] **Step 3: Add slice dispatch and emitters**

In the operation dispatch, add:

```go
case *ir.SliceGet:
    emitSliceGet(e, v, frame, ctx)
case *ir.SliceSet:
    emitSliceSet(e, v, frame, ctx)
```

Both emitters must call `emitIndexBoundsCheck` from Task 12C before computing the element address.

- [ ] **Step 4: Verify**

Run:

```bash
go test ./compiler/codegen -run TestSliceGetSetBounds -v
git diff --check
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add compiler/codegen/x64.go compiler/codegen/memory_test.go
git commit -m "feat: emit checked slice accessors -Codex Automated"
```

### Task 12E: Emit x86_64 For SlotFill

**Description:** Emit the `Slots<T>.fill(value)` intrinsic as an initialization loop that writes every slot and returns a `MutableSlice<T>` over the initialized memory.

**Files:**
- Modify: `compiler/codegen/x64.go`
- Modify: `compiler/codegen/memory_test.go`

**Acceptance Criteria:**
- `SlotFill` loads slots address from offset `0`.
- `SlotFill` loads slots capacity from offset `8`.
- The emitted loop writes `value` to every element from index `0` through `capacity - 1`.
- The returned `MutableSlice<T>` stores address at offset `0` and length/capacity at offset `8`.
- Tests assert the loop compare, payload store, capacity offset, and element size constants.

- [ ] **Step 1: Add failing fill test**

Append to `compiler/codegen/memory_test.go`, reusing `genericMemoryTypeInfos` from Task 12B and the `MutableSlice<Event>` type info from Task 12D:

```go
func testProgramWithSlotFill(t *testing.T) *ir.Program {
    t.Helper()
    slots := ir.Local{Symbol: "slots", Type: ir.Type{Name: "Slots<Event>"}}
    value := ir.Local{Symbol: "event", Type: ir.Type{Name: "Event"}}
    return &ir.Program{Types: genericMemoryTypeInfos(), Functions: []ir.Function{{
        Symbol: "_wrela_test_slot_fill",
        Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
            slots,
            value,
            &ir.SlotFill{Slots: slots, Value: value, Element: ir.Type{Name: "Event"}, Type: ir.Type{Name: "MutableSlice<Event>"}},
        }}},
    }}}
}

func TestSlotFillEmitsInitializationLoop(t *testing.T) {
    program := testProgramWithSlotFill(t)
    image, ds := Compile(program)
    if len(ds) != 0 {
        t.Fatalf("Compile diagnostics: %#v", ds)
    }
    code := symbolBytes(t, image, "_wrela_test_slot_fill")
    if !bytes.Contains(code, []byte{0x48, 0x39}) {
        t.Fatalf("slot fill must compare loop index against capacity, got %#x", code)
    }
    if !bytes.Contains(code, []byte{0x89}) && !bytes.Contains(code, []byte{0x88}) {
        t.Fatalf("slot fill must store the payload value inside the loop, got %#x", code)
    }
    for name, want := range map[string][]byte{
        "Slots.capacity offset": {0x08, 0x00, 0x00, 0x00},
        "Event storage size":    {0x08, 0x00, 0x00, 0x00},
    } {
        if !containsBytes(code, want) {
            t.Fatalf("slot fill missing %s constant %x in %x", name, want, code)
        }
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./compiler/codegen -run TestSlotFillEmitsInitializationLoop -v`

Expected: FAIL because `SlotFill` is not emitted.

- [ ] **Step 3: Add fill dispatch and emitter**

In the operation dispatch, add:

```go
case *ir.SlotFill:
    emitSlotFill(e, v, frame, ctx)
```

Implementation rules:
- Element size comes from `ctx.program.Types[v.Element.Name].StorageSize`.
- Loop index starts at `0` in a scratch register.
- Stop when `index == capacity`.
- Element address is `slots.address + index * element_size`.
- After the loop, construct the returned `MutableSlice<T>` using the same address and capacity.

- [ ] **Step 4: Verify**

Run:

```bash
go test ./compiler/codegen -run TestSlotFillEmitsInitializationLoop -v
git diff --check
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add compiler/codegen/x64.go compiler/codegen/memory_test.go
git commit -m "feat: emit slot fill initialization loops -Codex Automated"
```

### Task 13: Lower Trait-Constrained Generic Calls To Direct Calls

**Description:** Ensure trait-bound method calls in generic code become direct concrete calls after monomorphization.

**Files:**
- Modify: `compiler/ir/lower.go`
- Modify: `compiler/ir/generic_test.go`

**Acceptance Criteria:**
- A generic `Drain<S, T> where S: Subscription<T>` calling `self.input.try_next()` lowers to the concrete `EventSub.try_next` symbol for `Drain<EventSub, Event>`.
- No IR operation represents a runtime trait call.
- Codegen sees ordinary `ir.Call`.

- [ ] **Step 1: Add failing IR test**

Create or append to `compiler/ir/generic_test.go`:

```go
func TestTraitConstrainedGenericCallLowersToDirectConcreteCall(t *testing.T) {
    src := `
module ir.traits
enum Option<T> { None Some(value: T) }
trait Subscription<T> { fn try_next(self) -> Option<T> }
data Event { kind: U64 }
class EventSub {
    fn try_next(self) -> Option<Event> {
        return Option.None()
    }
}
impl Subscription<Event> for EventSub
class Drain<S, T> where S: Subscription<T> {
    input: S
    fn poll(self) -> Option<T> {
        return self.input.try_next()
    }
}
executor Worker {
    drain: Drain<EventSub, Event>
    start fn run(self) -> never {
        let next = self.drain.poll()
        while true {}
    }
}
`
    program := lowerSourceForTest(t, src)
    fn := findFunction(program, "_wrela_method_ir_traits_Drain_EventSub_Event_poll")
    if fn == nil {
        t.Fatal("missing Drain<EventSub, Event>.poll")
    }
    if !functionCalls(*fn, "_wrela_method_ir_traits_EventSub_try_next") {
        t.Fatalf("poll did not lower to direct EventSub.try_next call: %#v", fn.Blocks)
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./compiler/ir -run TestTraitConstrainedGenericCallLowersToDirectConcreteCall -v`

Expected: FAIL.

- [ ] **Step 3: Add direct-call resolution**

In lowering, when a method is called on a generic type parameter with a trait bound, resolve the concrete receiver type from the instantiated method context:

```go
func (ctx *lowerContext) resolveMethodSymbol(receiverType *sem.Type, methodName string) string {
    concreteMethod := findMethod(receiverType, methodName)
    if concreteMethod == nil {
        ctx.diag(diag.CG0001, "missing concrete method "+receiverType.Display()+"."+methodName)
        return ""
    }
    return symbolName("method", receiverType.Module, receiverType.MangledName(), methodName)
}
```

Use the `Type.MangledName()` helper added in Task 5C for the direct-call symbol.

- [ ] **Step 4: Verify**

Run:

```bash
go test ./compiler/ir -run TestTraitConstrainedGenericCallLowersToDirectConcreteCall -v
git diff --check
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add compiler/ir/lower.go compiler/ir/generic_test.go
git commit -m "feat: lower trait constrained calls directly -Codex Automated"
```

---

## 8. Phase 4: Core Library And Memory View Source

**Description:** This phase adds the source library surface that makes the compiler features usable from Wrela code.

**Phase Acceptance Criteria:**

- `wrela.lang.core` defines `Unit`, `Option`, `Result`, `Publisher`, and `Subscription`.
- `executor_memory.wrela` defines generic memory views and containers.
- Region-kind views are present in platform modules and protected by semantic rules.
- Existing byte-view APIs remain source-compatible.
- Phase smoke command after Task 16: `go test ./compiler/sem -run 'TestCoreLanguageModuleTypes|TestFixedBufferPushUsesSlotsAndResult|TestGenericTopicPayloadLayoutRecorded|TestSourceTypes' -v`.

**Phase Code Example:**

```wrela
let slots = tick.reserve_array(Event, count = EVENT_CAPACITY)
let events = tick.place(FixedBuffer<Event>(slots = slots, length = 0))
events.push(value = Event(kind = 1))
```

### Task 14: Verify Core Module In Full Source Suite

**Description:** Verify the core module added in Task 4.5 is loaded by the full source harness and exposed through the semantic index. This task intentionally does not recreate `wrela/lang/core.wrela`.

**Files:**
- Modify: `compiler/sem/uefi_source_shape_test.go`
- Modify: `compiler/sem/types_test.go`
- Modify: `compiler/build_test.go`

**Acceptance Criteria:**
- `wrela.lang.core` parses and type-checks.
- `Option<T>` and `Result<T, E>` are enums.
- `Publisher<T>` and `Subscription<T>` are traits.
- No implicit imports are required.
- The compiler build import-root configuration resolves `use { Option } from wrela.lang.core`; no auto-prelude is added.

- [ ] **Step 1: Add core module to source loader**

In `compiler/sem/uefi_source_shape_test.go`, add the core module to `parseUEFIModuleSet` before modules that import it:

```go
filepath.Join(repoRoot, "wrela/lang/core.wrela"),
```

- [ ] **Step 2: Add index test**

Add to `compiler/sem/types_test.go`:

```go
func TestCoreLanguageModuleTypes(t *testing.T) {
    modules := parseUEFIModuleSet(t)
    index := mustBuildIndex(t, modules)
    _ = mustCheck(t, index, modules)
    option, ok := index.Lookup("wrela.lang.core", "Option")
    if !ok || option.Kind != KindEnum || len(option.TypeParams) != 1 {
        t.Fatalf("Option = %#v", option)
    }
    result, ok := index.Lookup("wrela.lang.core", "Result")
    if !ok || result.Kind != KindEnum || len(result.TypeParams) != 2 {
        t.Fatalf("Result = %#v", result)
    }
    publisher, ok := index.Lookup("wrela.lang.core", "Publisher")
    if !ok || publisher.Kind != KindTrait {
        t.Fatalf("Publisher = %#v", publisher)
    }
}
```

- [ ] **Step 3: Verify**

Add these imports to `compiler/build_test.go`:

```go
"github.com/ryanwible/wrela3/compiler/diag"
"github.com/ryanwible/wrela3/compiler/parse"
"github.com/ryanwible/wrela3/compiler/sem"
"github.com/ryanwible/wrela3/compiler/source"
```

Add this test to `compiler/build_test.go`:

```go
func TestBuildImportRootsLoadCoreLanguageImports(t *testing.T) {
    dir := t.TempDir()
    repoRoot := resolveRepoRoot(".")
    root := filepath.Join(dir, "main.wrela")
    if err := os.WriteFile(root, []byte(`
module test.core_build
use { Option } from wrela.lang.core
data Event { kind: U64 }
data Holder { next: Option<Event> }
`), 0o644); err != nil {
        t.Fatalf("write source: %v", err)
    }
    graph, err := source.LoadGraph(source.Options{
        RootPath: root,
        ImportRoots: []string{
            repoRoot,
            filepath.Join(repoRoot, "wrela"),
        },
    })
    if err != nil {
        t.Fatalf("LoadGraph: %v", err)
    }
    modules, ds := parse.ParseGraph(*graph)
    if len(ds) != 0 {
        t.Fatalf("parse diagnostics: %#v", ds)
    }
    index, ds := sem.BuildIndex(modules)
    filtered := ds[:0]
    for _, d := range ds {
        if d.Code != diag.SEM0004 {
            filtered = append(filtered, d)
        }
    }
    if len(filtered) != 0 {
        t.Fatalf("index diagnostics: %#v", ds)
    }
    if _, ok := index.Lookup("wrela.lang.core", "Option"); !ok {
        t.Fatalf("core Option was not loaded through build import roots")
    }
}
```

- [ ] **Step 4: Verify**

Run:

```bash
go test ./compiler/sem -run TestCoreLanguageModuleTypes -v
go test ./compiler -run TestBuildImportRootsLoadCoreLanguageImports -v
git diff --check
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add compiler/sem/uefi_source_shape_test.go compiler/sem/types_test.go compiler/build_test.go
git commit -m "test: verify core generic language module -Codex Automated"
```

### Task 15: Add Generic Memory Views And Containers

**Description:** Add `Slice<T>`, `MutableSlice<T>`, `Slots<T>`, `FixedBuffer<T>`, and `Ring<T>` to `executor_memory.wrela`.

**Files:**
- Modify: `wrela/machine/x86_64/executor_memory.wrela`
- Modify: `compiler/sem/memory_test.go`
- Modify: `compiler/ir/memory_test.go`

**Acceptance Criteria:**
- Existing `Bytes`, `MutableBytes`, `ExecutorMemory`, and `ArenaFrame` stay source-compatible.
- `Slots<T>.write`, `Slots<T>.fill`, `Slice<T>.get`, `MutableSlice<T>.get`, and `MutableSlice<T>.set` have source-visible signatures.
- `FixedBuffer<T>.push` returns `Result<Unit, BufferFull>`.
- `Ring<T>` tracks `head`, `tail`, and `len`.

- [ ] **Step 1: Update source file**

Add this import immediately after the `module machine.x86_64.executor_memory` line:

```wrela
use { Unit, Result } from wrela.lang.core
```

Then append the generic memory declarations after `MutableBytes`:

```wrela
data BufferFull {}

data Slice<T> {
    address: PhysicalAddress
    length: U64

    asm fn get(self, index: U64) -> T {
        ret
    }
}

data MutableSlice<T> {
    address: PhysicalAddress
    length: U64

    asm fn get(self, index: U64) -> T {
        ret
    }

    asm fn set(self, index: U64, value: T) {
        ret
    }
}

data Slots<T> {
    address: PhysicalAddress
    capacity: U64

    asm fn write(self, index: U64, value: T) {
        ret
    }

    asm fn fill(self, value: T) -> MutableSlice<T> {
        ret
    }
}

data FixedBuffer<T> {
    slots: Slots<T>
    length: U64

    fn push(self, value: T) -> Result<Unit, BufferFull> {
        if self.length == self.slots.capacity {
            return Result.Err(error = BufferFull())
        }
        self.slots.write(index = self.length, value = value)
        self.length = self.length + 1
        return Result.Ok(value = Unit())
    }
}

data Ring<T> {
    slots: Slots<T>
    head: U64
    tail: U64
    len: U64
}
```

The `asm fn` bodies above are source-visible compiler intrinsic stubs. Task 11 must lower calls to these methods to IR intrinsic operations before normal asm-method emission, and Task 12 must emit those IR operations directly.

Add compiler-intrinsic comments near `ExecutorMemory` and `ArenaFrame`:

```wrela
    // Compiler intrinsic receiver. The parser accepts the first argument as a type operand.
    // tick.reserve_array(Event, count = 64) returns Slots<Event>.
```

- [ ] **Step 2: Add semantic source test**

Add to `compiler/sem/memory_test.go`:

```go
func TestFixedBufferPushUsesSlotsAndResult(t *testing.T) {
    modules := parseUEFIModuleSet(t)
    index := mustBuildIndex(t, modules)
    fixed, ok := index.Lookup("machine.x86_64.executor_memory", "FixedBuffer")
    if !ok || len(fixed.TypeParams) != 1 {
        t.Fatalf("FixedBuffer = %#v", fixed)
    }
    push := methodByName(t, fixed, "push")
    if push.Return == nil || push.Return.Display() != "Result<Unit, BufferFull>" {
        t.Fatalf("FixedBuffer.push return = %#v, want Result<Unit, BufferFull>", push.Return)
    }
}
```

- [ ] **Step 3: Verify**

Run:

```bash
go test ./compiler/sem -run 'TestFixedBufferPushUsesSlotsAndResult|TestReserveArrayReturnsFrameLifetimeSlots' -v
go test ./compiler/ir -run TestLowerReserveArrayAndSlotWrite -v
git diff --check
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add wrela/machine/x86_64/executor_memory.wrela compiler/sem/memory_test.go compiler/ir/memory_test.go
git commit -m "feat: add generic typed memory views -Codex Automated"
```

### Task 16: Add Generic Topic Library

**Description:** Replace the reusable parts of concrete topic classes with generic topic, publisher, subscription, and reliable topic definitions.

**Files:**
- Create: `wrela/machine/x86_64/topic.wrela`
- Modify: `compiler/sem/uefi_source_shape_test.go`
- Modify: `compiler/sem/topic_graph.go`
- Modify: `compiler/sem/topic_payload.go`
- Modify: `compiler/sem/topic_graph_test.go`
- Modify: `compiler/sem/topic_payload_test.go`

**Acceptance Criteria:**
- `Topic<T>` has `identity`, `id`, and `depth`.
- `Topic<T>.publisher()` returns `TopicPublisher<T>`.
- `Topic<T>.subscribe(...)` returns `TopicSubscription<T>`.
- `TopicSubscription<T>.try_next()` returns `Option<T>`.
- `ReliablePublisher<T>.try_publish(...)` returns `Result<Unit, TopicFull>`.
- `impl Publisher<T> for TopicPublisher<T>` and `impl Subscription<T> for TopicSubscription<T>` type-check.
- Topic graph extraction obtains payload type from generic type args, not hard-coded class names.
- Topic intrinsics keep the existing ring-buffer storage behavior but return enum layouts: `Option.None` tag `0`, `Option.Some` tag `1`, `Result.Ok` tag `0`, and `Result.Err` tag `1`.

- [ ] **Step 1: Add source file**

Create `wrela/machine/x86_64/topic.wrela`:

```wrela
module machine.x86_64.topic

use { Option, Result, Unit, Publisher, Subscription } from wrela.lang.core
use { ExecutorSlot } from machine.x86_64.executor_slot
use { TopicIdentity } from machine.x86_64.topic_u64

data TopicFull {}

class Topic<T> {
    identity: TopicIdentity
    id: U64
    depth: U64

    fn publisher(self) -> TopicPublisher<T> {
        return TopicPublisher<T>(topic = self)
    }

    fn subscribe(self, subscriber: ExecutorSlot) -> TopicSubscription<T> {
        return TopicSubscription<T>(topic = self, subscriber = subscriber, cursor = 0, armed = false)
    }
}

class TopicPublisher<T> {
    topic: Topic<T>

    asm fn publish(self, value: T) {
        ret
    }
}

class TopicSubscription<T> {
    topic: Topic<T>
    subscriber: ExecutorSlot
    cursor: U64
    armed: Bool

    asm fn try_next(self) -> Option<T> {
        ret
    }

    fn arm_wait(self) {
        self.armed = true
    }

    fn is_wait_armed(self) -> Bool {
        return self.armed
    }
}

impl Publisher<T> for TopicPublisher<T>
impl Subscription<T> for TopicSubscription<T>

class ReliableTopic<T> {
    identity: TopicIdentity
    id: U64
    depth: U64

    fn publisher(self) -> ReliablePublisher<T> {
        return ReliablePublisher<T>(topic = self)
    }

    fn subscribe(self, subscriber: ExecutorSlot) -> ReliableSubscription<T> {
        return ReliableSubscription<T>(topic = self, subscriber = subscriber, cursor = 0, armed = false)
    }
}

class ReliablePublisher<T> {
    topic: ReliableTopic<T>

    asm fn try_publish(self, value: T) -> Result<Unit, TopicFull> {
        ret
    }
}

class ReliableSubscription<T> {
    topic: ReliableTopic<T>
    subscriber: ExecutorSlot
    cursor: U64
    armed: Bool

    asm fn try_next(self) -> Option<T> {
        ret
    }

    fn arm_wait(self) {
        self.armed = true
    }

    fn is_wait_armed(self) -> Bool {
        return self.armed
    }
}

impl Subscription<T> for ReliableSubscription<T>
```

- [ ] **Step 2: Add topic module to source test loader**

In `compiler/sem/uefi_source_shape_test.go`, add the new topic module to `parseUEFIModuleSet` immediately after `topic_u64.wrela`:

```go
filepath.Join(repoRoot, "wrela/machine/x86_64/topic.wrela"),
```

- [ ] **Step 3: Update topic semantic helpers**

Replace hard-coded topic type checks with generic recognition:

```go
func IsTopicType(t *Type) bool {
    if t == nil {
        return false
    }
    q := qualifiedTypeName(t)
    return (q == "machine.x86_64.topic.Topic" || q == "machine.x86_64.topic.ReliableTopic") && len(t.TypeArgs) == 1
}

func TopicPayloadTypeForTopic(t *Type) (payload *Type, kind string, ok bool) {
    if !IsTopicType(t) {
        return nil, "", false
    }
    kind = "topic"
    if t.Name == "ReliableTopic" {
        kind = "reliable"
    }
    return t.TypeArgs[0], kind, true
}
```

Do this replacement everywhere the current compiler asks topic-shape questions, not only in `topic_graph.go`. Use this audit command and update each reported call site to accept both old concrete names and new generic types until Task 20 removes compatibility:

```bash
rg -n "IsTopicType|IsTopicPublisherType|IsTopicSubscriptionType|TopicPayloadTypeForTopic" compiler/sem compiler/ir
```

Expected current call-site groups to inspect:

```text
compiler/sem/check.go       topic construction, topic wait, subscription, publish, and report checks
compiler/sem/topic_graph.go topic graph extraction
compiler/sem/topic_payload.go payload layout extraction
compiler/ir/lower.go        topic publish/subscribe/wait lowering
```

For every branch that keeps an old concrete topic name, add:

```go
// Compatibility branch removed by Task 20 after source migration.
```

Add this test to `compiler/sem/topic_payload_test.go`:

```go
func TestGenericTopicPayloadLayoutRecorded(t *testing.T) {
    modules := parseUEFIModuleSet(t)
    index := mustBuildIndex(t, modules)
    payload := moduleType(t, index, "machine.x86_64.topic_payload", "TimerTickPayload")
    topic := index.instantiateByName("machine.x86_64.topic", "Topic", []*Type{payload})
    gotPayload, kind, ok := TopicPayloadTypeForTopic(topic)
    if !ok {
        t.Fatal("generic topic payload was not recognized")
    }
    if gotPayload.Key() != payload.Key() || kind != "topic" {
        t.Fatalf("payload/kind = %s/%s, want %s/topic", gotPayload.Key(), kind, payload.Key())
    }
}
```

Keep compatibility branches for old concrete topics only until Task 20. Mark them with the comment:

```go
// Compatibility branch removed by Task 20 after source migration.
```

- [ ] **Step 4: Verify**

Run:

```bash
go test ./compiler/sem -run 'TestGenericTopicPayloadLayoutRecorded|TestTopicGraph' -v
git diff --check
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add wrela/machine/x86_64/topic.wrela compiler/sem/uefi_source_shape_test.go compiler/sem/topic_graph.go compiler/sem/topic_payload.go compiler/sem/topic_graph_test.go compiler/sem/topic_payload_test.go
git commit -m "feat: add generic topic source library -Codex Automated"
```

---

## 9. Phase 5: Source Migration And Protected Region Views

**Description:** This phase updates existing Wrela source, semantic helpers, IR topic lowering, codegen topic data, examples, and platform authority views to use the better generic/enumerated features.

**Phase Acceptance Criteria:**

- Current concrete topic/result duplications are removed or converted to thin compatibility aliases that no runtime source uses.
- Existing examples use generic topics, `Option<T>`, `Result<T, E>`, and `match`/`if let`.
- At least one executor uses `reserve_array` and `FixedBuffer<T>` as a hot working set.
- QEMU-visible behavior remains stable.
- Phase smoke command after Task 19: `go test ./compiler/sem ./compiler/ir ./compiler/codegen -run 'TestTopic|TestInterruptQueue|TestReport|TestSourceTypes' -v`.

**Phase Code Example:**

```wrela
match self.serial_rx.try_next() {
    Option.Some(value = event) => {
        self.serial.ack_receive(event = event)
    }
    Option.None => {
        self.serial_rx.arm_wait()
    }
}
```

### Task 17A: Migrate U64 And Timer Topics

**Description:** Replace U64 and timer concrete topic families with `Topic<U64>`, `ReliableTopic<U64>`, and `Topic<TimerTickPayload>`.

**Files:**
- Modify: `wrela/machine/x86_64/topic_u64.wrela`
- Modify: `wrela/machine/x86_64/topic_payload.wrela`
- Modify: `wrela/machine/x86_64/timer.wrela`

**Acceptance Criteria:**
- `TimerAuthority.subscribe` returns `TopicSubscription<TimerTickPayload>`.
- U64 gap/reliable topics use `Topic<U64>` and `ReliableTopic<U64>`.
- `U64TopicNext`, `TimerTickNext`, and `U64PublishResult` are removed from `wrela/machine/x86_64/topic_u64.wrela` and `wrela/machine/x86_64/topic_payload.wrela`.

- [ ] **Step 1: Update timer imports and subscribe type**

Example migration for `wrela/machine/x86_64/timer.wrela`:

```wrela
use { Topic, TopicSubscription } from machine.x86_64.topic
use { TopicIdentity } from machine.x86_64.topic_u64

fn subscribe(self, subscriber: ExecutorSlot) -> TopicSubscription<TimerTickPayload> {
    let topic = Topic<TimerTickPayload>(identity = TopicIdentity(label = "timer.periodic"), id = 3, depth = 64)
    return topic.subscribe(subscriber = subscriber)
}
```

- [ ] **Step 2: Update U64 source aliases**

In `wrela/machine/x86_64/topic_u64.wrela`, keep only `TopicIdentity` and update all call sites to import generic topic types from `machine.x86_64.topic`. The target source shape is:

```wrela
module machine.x86_64.topic_u64

data TopicIdentity {
    label: StringLiteral
}
```

- [ ] **Step 3: Verify**

Run:

```bash
go test ./compiler/sem -run 'TestTimerTickTopicPayloadLayoutRecorded|TestSourceTypes' -v
rg -n "TimerTickNext|U64TopicNext|U64PublishResult|U64GapTopic|U64ReliableTopic" wrela/machine/x86_64/topic_u64.wrela wrela/machine/x86_64/topic_payload.wrela wrela/machine/x86_64/timer.wrela
git diff --check
```

Expected: tests PASS; `rg` prints no matches.

- [ ] **Step 4: Commit**

```bash
git add wrela/machine/x86_64/topic_u64.wrela wrela/machine/x86_64/topic_payload.wrela wrela/machine/x86_64/timer.wrela
git commit -m "feat: migrate u64 and timer topics to generics -Codex Automated"
```

### Task 17B: Migrate Serial Topic Source

**Description:** Replace serial RX concrete topic types with `Topic<SerialPathInterrupt>`, `TopicPublisher<SerialPathInterrupt>`, and `TopicSubscription<SerialPathInterrupt>`.

**Files:**
- Modify: `wrela/machine/x86_64/serial.wrela`
- Modify: `compiler/sem/types_test.go`

**Acceptance Criteria:**
- `SerialConsolePath.rx` is `TopicPublisher<SerialPathInterrupt>`.
- `SerialRxNext`, `SerialRxTopic`, `SerialRxPublisher`, and `SerialRxSubscription` are removed.
- Source-shape tests expect generic topic fields/methods.

- [ ] **Step 1: Replace serial topic definitions**

In `wrela/machine/x86_64/serial.wrela`, add:

```wrela
use { Topic, TopicPublisher, TopicSubscription } from machine.x86_64.topic
```

Delete the declarations named `SerialRxNext`, `SerialRxTopic`, `SerialRxPublisher`, and `SerialRxSubscription`. Do not leave aliases or wrappers; every consumer must import the generic topic types from `machine.x86_64.topic`.

Change the path constructor and field exactly:

```wrela
fn create_console_path(self, identity: PathIdentity, route: IoApicRoute, rx: TopicPublisher<SerialPathInterrupt>) -> SerialConsolePath {
    let console_path = SerialConsolePath(identity = identity, registers = self.registers, route = route, rx = rx)
    console_path.enable_receive_interrupts()
    return console_path
}

driver path SerialConsolePath {
    identity: PathIdentity
    registers: SerialWriterRegisters
    route: IoApicRoute
    rx: TopicPublisher<SerialPathInterrupt>
}
```

- [ ] **Step 2: Update source-shape tests**

In `compiler/sem/types_test.go`, replace serial concrete type assertions with:

```go
serialPath := moduleType(t, index, "machine.x86_64.serial", "SerialConsolePath")
assertTypeFields(t, serialPath, map[string]string{
    "identity":  "PathIdentity",
    "registers": "SerialWriterRegisters",
    "route":     "IoApicRoute",
    "rx":        "TopicPublisher<SerialPathInterrupt>",
})
```

- [ ] **Step 3: Verify**

Run:

```bash
go test ./compiler/sem -run TestSourceTypes -v
rg -n "SerialRxNext|SerialRxTopic|SerialRxPublisher|SerialRxSubscription" wrela/machine/x86_64/serial.wrela compiler/sem/types_test.go
git diff --check
```

Expected: PASS; `rg` prints no matches.

- [ ] **Step 4: Commit**

```bash
git add wrela/machine/x86_64/serial.wrela compiler/sem/types_test.go
git commit -m "feat: migrate serial rx topic to generics -Codex Automated"
```

### Task 17C: Migrate EDU And Ivshmem Topic Source

**Description:** Replace EDU and ivshmem concrete interrupt topic types with generic topics.

**Files:**
- Modify: `wrela/machine/x86_64/edu.wrela`
- Modify: `wrela/machine/x86_64/ivshmem.wrela`
- Modify: `compiler/sem/types_test.go`

`wrela/machine/x86_64/interrupts.wrela` is intentionally not listed. Before editing, confirm it still has no concrete topic/result references:

```bash
rg -n "Topic|Next|PublishResult|has_message|try_next|try_publish" wrela/machine/x86_64/interrupts.wrela
```

Expected: no matches. If this command finds a topic/result reference in a future branch, add that exact edit to this task before running verification.

**Acceptance Criteria:**
- `EduMsiPath.irq` is `TopicPublisher<EduInterrupt>`.
- `IvshmemDoorbellPath.irq` is `TopicPublisher<IvshmemDoorbellInterrupt>`.
- `EduInterruptNext`, `EduInterruptTopic`, `EduInterruptPublisher`, `EduInterruptSubscription`, `IvshmemDoorbellNext`, `IvshmemDoorbellTopic`, `IvshmemDoorbellPublisher`, and `IvshmemDoorbellSubscription` are removed.

- [ ] **Step 1: Replace EDU declarations**

In `wrela/machine/x86_64/edu.wrela`, import generic topics:

```wrela
use { TopicPublisher } from machine.x86_64.topic
```

Delete the concrete EDU next/topic/publisher/subscription declarations and change:

```wrela
driver path EduMsiPath {
    identity: PathIdentity
    mmio: MmioRegion
    irq: TopicPublisher<EduInterrupt>
}
```

- [ ] **Step 2: Replace ivshmem declarations**

In `wrela/machine/x86_64/ivshmem.wrela`, import generic topics:

```wrela
use { TopicPublisher } from machine.x86_64.topic
```

Delete the concrete ivshmem next/topic/publisher/subscription declarations and change:

```wrela
driver path IvshmemDoorbellPath {
    identity: PathIdentity
    registers: MmioRegion
    irq: TopicPublisher<IvshmemDoorbellInterrupt>
}
```

- [ ] **Step 3: Verify**

Run:

```bash
go test ./compiler/sem -run TestSourceTypes -v
rg -n "EduInterruptNext|EduInterruptTopic|EduInterruptPublisher|EduInterruptSubscription|IvshmemDoorbellNext|IvshmemDoorbellTopic|IvshmemDoorbellPublisher|IvshmemDoorbellSubscription" wrela/machine/x86_64 compiler/sem/types_test.go
git diff --check
```

Expected: PASS; `rg` prints no matches.

- [ ] **Step 4: Commit**

```bash
git add wrela/machine/x86_64/edu.wrela wrela/machine/x86_64/ivshmem.wrela compiler/sem/types_test.go
git commit -m "feat: migrate device interrupt topics to generics -Codex Automated"
```

### Task 17D: Migrate Topic Graph, IR, And Codegen Metadata

**Description:** Remove hard-coded concrete topic recognition from compiler semantic graphing and codegen tests after source topics are generic.

**Files:**
- Modify: `compiler/sem/topic_graph.go`
- Modify: `compiler/sem/topic_payload.go`
- Modify: `compiler/sem/topic_graph_test.go`
- Modify: `compiler/sem/topic_payload_test.go`
- Modify: `compiler/ir/topic_test.go`
- Modify: `compiler/codegen/topic_test.go`

**Acceptance Criteria:**
- Topic graph recognizes `Topic<T>`, `ReliableTopic<T>`, `TopicPublisher<T>`, and `TopicSubscription<T>`.
- Payload-specific codegen kind is derived from the payload type key.
- Topic IR/codegen tests use generic source/type names.
- Topic `try_next` codegen writes the generic `Option<T>` layout, not the old `U64TopicNext` boolean/result layout.
- Reliable publish codegen writes the generic `Result<Unit, TopicFull>` layout, not the old `U64PublishResult` boolean/result layout.

- [ ] **Step 1: Update topic codegen metadata**

In `compiler/sem/topic_graph.go`, delete the old concrete type switches and use `IsTopicType`, `IsTopicPublisherType`, and `IsTopicSubscriptionType` generic checks:

```go
func IsTopicPublisherType(t *Type) bool {
    return t != nil && qualifiedTypeName(t) == "machine.x86_64.topic.TopicPublisher" && len(t.TypeArgs) == 1
}

func IsTopicSubscriptionType(t *Type) bool {
    return t != nil && qualifiedTypeName(t) == "machine.x86_64.topic.TopicSubscription" && len(t.TypeArgs) == 1
}
```

For legacy codegen `Kind`, use payload identity:

```go
func topicKindFromPayload(payload *Type) string {
    switch payload.Key() {
    case "U64":
        return "gap_u64"
    case "machine.x86_64.topic_payload.TimerTickPayload":
        return "timer_tick"
    case "machine.x86_64.serial.SerialPathInterrupt":
        return "serial_rx"
    case "machine.x86_64.edu.EduInterrupt":
        return "edu_interrupt"
    case "machine.x86_64.ivshmem.IvshmemDoorbellInterrupt":
        return "ivshmem_doorbell"
    default:
        return "topic"
    }
}
```

Update topic lowering so generic source-visible methods still lower to the existing topic IR operations:

```go
case sem.IsTopicPublisherType(recvType) && e.Method == "publish":
    publish := TopicPublish{TopicLabel: label, Kind: kind, Value: value}
case sem.IsReliableTopicPublisherType(recvType) && e.Method == "try_publish":
    tryPublish := ReliableTopicTryPublish{TopicLabel: label, Value: value, Type: ctx.irType(ret)}
case sem.IsTopicSubscriptionType(recvType) && e.Method == "try_next":
    next := TopicTryNext{TopicLabel: label, SubscriberSlot: ctx.subscriberSlotForValue(receiver, receiverType), Subscription: receiver, Type: ctx.irType(ret)}
```

`ret` for `try_next` is now `Option<T>`. Codegen must write tag `1` plus payload for a message and tag `0` for empty. `ret` for `try_publish` is now `Result<Unit, TopicFull>`. Codegen must write tag `0` for published and tag `1` for full. Do not reuse field names `has_message`, `published`, or `full` after this task.

Add a codegen regression test in `compiler/codegen/topic_test.go`:

```go
func TestTopicTryNextWritesOptionLayout(t *testing.T) {
    program := topicProgramForCodegenTest()
    sub := ir.Local{Symbol: "sub", Type: ir.Type{Name: "TopicSubscription<U64>"}}
    next := &ir.TopicTryNext{
        TopicLabel: "counter",
        SubscriberSlot: "worker",
        Subscription: sub,
        Type: ir.Type{Name: "Option<U64>", Kind: ir.TypeKindEnum},
    }
    program.Types["Option<U64>"] = ir.TypeInfo{
        Name: "Option<U64>", Kind: ir.TypeKindEnum, Size: 16, Align: 8, StorageSize: 16,
        Fields: map[string]ir.FieldInfo{
            "$tag":       {Name: "$tag", Type: ir.Type{Name: "U64"}, Offset: 0, Size: 8, Align: 8, StorageOffset: 0, StorageSize: 8},
            "Some.value": {Name: "Some.value", Type: ir.Type{Name: "U64"}, Offset: 8, Size: 8, Align: 8, StorageOffset: 8, StorageSize: 8},
        },
        EnumVariants: []ir.EnumVariantInfo{{Name: "None", Discriminant: 0}, {Name: "Some", Discriminant: 1, Fields: []string{"Some.value"}}},
    }
    program.Functions = []ir.Function{{
        Symbol: "try_counter_option",
        Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
            sub,
            next,
            &ir.Return{},
        }}},
    }}
    image, ds := Compile(program)
    if len(ds) != 0 {
        t.Fatalf("Compile diagnostics: %#v", ds)
    }
    code := symbolBytes(t, image, "try_counter_option")
    if !containsBytes(code, []byte{0x01, 0x00, 0x00, 0x00}) || !containsBytes(code, []byte{0x08, 0x00, 0x00, 0x00}) {
        t.Fatalf("try_next must write Option.Some tag and payload offset 8, got %x", code)
    }
}
```

- [ ] **Step 2: Verify**

Run:

```bash
go test ./compiler/sem -run 'TestTopicGraph|TestTimerTickTopicPayloadLayoutRecorded|TestSourceTypes' -v
go test ./compiler/ir -run TestTopic -v
go test ./compiler/codegen -run TestTopic -v
rg -n "TimerTickNext|SerialRxNext|EduInterruptNext|IvshmemDoorbellNext|U64TopicNext|U64PublishResult" wrela
git diff --check
```

Expected: tests PASS; `rg` prints no matches in `wrela/`.

- [ ] **Step 3: Commit**

```bash
git add compiler/sem/topic_graph.go compiler/sem/topic_payload.go compiler/sem/topic_graph_test.go compiler/sem/topic_payload_test.go compiler/ir/topic_test.go compiler/codegen/topic_test.go
git commit -m "feat: derive topic metadata from generic payloads -Codex Automated"
```

### Task 17E: Migrate Interrupt Queue Payload Shape

**Description:** Update interrupt queue source and compiler tests so queue payload storage is expressed with generic typed slots instead of byte-only metadata where source code owns fixed-capacity typed storage.

**Files:**
- Modify: `wrela/machine/x86_64/interrupt_queue.wrela`
- Modify: `compiler/sem/interrupt_queue_test.go`
- Modify: `compiler/ir/interrupt_queue_test.go`
- Modify: `compiler/codegen/interrupt_queue_test.go`

**Acceptance Criteria:**
- `InterruptQueue<T>` stores `slots: Slots<T>` for typed payload storage.
- Queue identity, owner, capacity, overflow, head, tail, and overflowed fields remain unchanged.
- Compiler tests still verify bounded queue capacity and overflow policy.

- [ ] **Step 1: Update source shape**

In `wrela/machine/x86_64/interrupt_queue.wrela`, import slots and replace the untyped storage field:

```wrela
use { Slots } from machine.x86_64.executor_memory

data InterruptQueue<T> {
    identity: QueueIdentity
    owner: ExecutorSlot
    slots: Slots<T>
    capacity: U64
    payload: InterruptPayloadKind
    overflow: InterruptOverflowPolicy
    head: U64
    tail: U64
    overflowed: Bool
}
```

Keep `InterruptPayloadKind` for report/codegen compatibility in this milestone; it must be derived from `T` by semantic checks when an interrupt queue is constructed.

- [ ] **Step 2: Update semantic and IR tests**

Replace expected type names in queue tests:

```go
queue := moduleType(t, index, "machine.x86_64.interrupt_queue", "InterruptQueue")
if len(queue.TypeParams) != 1 {
    t.Fatalf("InterruptQueue type params = %#v, want T", queue.TypeParams)
}
```

Where tests construct queue source, use:

```wrela
let serial_queue_slots = root_arena.reserve_array(SerialPathInterrupt, count = 64)
let serial_queue = InterruptQueue<SerialPathInterrupt>(
    identity = QueueIdentity(label = "irq.serial.rx"),
    owner = console_slot_seed,
    slots = serial_queue_slots,
    capacity = 64,
    payload = InterruptPayloadKind(kind = 1, size = sizeof(SerialPathInterrupt), align = alignof(SerialPathInterrupt)),
    overflow = InterruptOverflowPolicy(mode = 0),
    head = 0,
    tail = 0,
    overflowed = false
)
```

- [ ] **Step 3: Verify**

Run:

```bash
go test ./compiler/sem -run TestInterruptQueue -v
go test ./compiler/ir -run TestInterruptQueue -v
go test ./compiler/codegen -run TestInterruptQueue -v
git diff --check
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add wrela/machine/x86_64/interrupt_queue.wrela compiler/sem/interrupt_queue_test.go compiler/ir/interrupt_queue_test.go compiler/codegen/interrupt_queue_test.go
git commit -m "feat: migrate interrupt queues to typed slots -Codex Automated"
```

### Task 18A: Migrate Hello Example And Hot Working Set

**Description:** Update the primary hello example to use generic topics, enum matching, constants, and typed arena slots.

**Files:**
- Modify: `examples/hello/main.wrela`
- Modify: `examples/hello/program.wrela`
- Modify: `tests/e2e/fixtures/hello_ivshmem/*.wrela`
- Modify: `compiler/integration_test.go`

**Acceptance Criteria:**
- No example checks `.has_message` on topic results.
- At least one executor reserves `Slots<RunEvent>` and uses `FixedBuffer<RunEvent>`.
- Existing serial/timer/EDU/ivshmem output behavior remains unchanged.

- [ ] **Step 1: Add event buffer in hello program**

In `examples/hello/program.wrela`, add:

```wrela
use { FixedBuffer } from machine.x86_64.executor_memory
use { Option, Result } from wrela.lang.core

const EVENT_CAPACITY: U64 = 16
static_assert(sizeof(RunEvent) * EVENT_CAPACITY <= 4096, message = "run event buffer fits frame")

data RunEvent {
    kind: U64
    value: U64
}
```

Inside the main frame:

```wrela
let event_slots = tick.reserve_array(RunEvent, count = EVENT_CAPACITY)
let events = tick.place(FixedBuffer<RunEvent>(slots = event_slots, length = 0))
```

When receiving a serial event:

```wrela
match self.serial_rx.try_next() {
    Option.Some(value = serial_event) => {
        events.push(value = RunEvent(kind = 1, value = serial_event.byte))
        if serial_event.has_byte {
            self.serial.ack_receive(event = serial_event)
        }
    }
    Option.None => {
        self.serial_rx.arm_wait()
    }
}
```

- [ ] **Step 2: Replace hello `.has_message` checks**

Use this shape in `examples/hello/program.wrela` and `tests/e2e/fixtures/hello_ivshmem/program.wrela`:

```wrela
match subscription.try_next() {
    Option.Some(value = event) => {
        path.ack_completed(event = event)
    }
    Option.None => {
        subscription.arm_wait()
    }
}
```

For retry paths that previously checked several subscriptions before sleeping, use `if let`:

```wrela
if let Option.Some(value = tick) = self.console_ticks.try_next() {
    events.push(value = RunEvent(kind = 3, value = tick.sequence))
}
```

- [ ] **Step 3: Update integration tests**

In `compiler/integration_test.go`, replace expected old result names with generic enum names:

```go
for _, forbidden := range []string{"TimerTickNext", "SerialRxNext", "EduInterruptNext", "IvshmemDoorbellNext"} {
    if strings.Contains(report, forbidden) {
        t.Fatalf("report still contains concrete next type %s", forbidden)
    }
}
for _, want := range []string{"Topic<TimerTickPayload>", "Option<TimerTickPayload>", "FixedBuffer<RunEvent>"} {
    if !strings.Contains(report, want) {
        t.Fatalf("report missing %s", want)
    }
}
```

- [ ] **Step 4: Verify**

Run:

```bash
go test ./compiler -run 'TestIntegration|TestNegativeFixtures' -v
rg -n "\\bhas_message\\b|TimerTickNext|SerialRxNext|EduInterruptNext|IvshmemDoorbellNext|U64TopicNext" examples/hello tests/e2e/fixtures/hello_ivshmem
rg -n "\\breserve_array\\b|FixedBuffer<|\\bmatch\\b|\\bif let\\b|Option<" examples/hello tests/e2e/fixtures/hello_ivshmem
git diff --check
```

Expected: Go tests PASS; first `rg` prints no matches; second `rg` prints migrated usages.

- [ ] **Step 5: Commit**

```bash
git add examples/hello/main.wrela examples/hello/program.wrela tests/e2e/fixtures/hello_ivshmem compiler/integration_test.go
git commit -m "feat: migrate hello example to expressive features -Codex Automated"
```

### Task 18B: Migrate Multi-vCPU Topic Example

**Description:** Update `examples/multi_vcpu_topics/main.wrela` to use generic U64 topics, `Option<U64>`, and match/if-let result handling.

**Files:**
- Modify: `examples/multi_vcpu_topics/main.wrela`
- Modify: `tests/e2e/hello_qemu_test.go`

**Acceptance Criteria:**
- Producers use `TopicPublisher<U64>` or `ReliablePublisher<U64>`.
- Consumers use `TopicSubscription<U64>` or `ReliableSubscription<U64>`.
- No `.has_message`, `U64TopicNext`, `U64GapTopic`, or `U64ReliableTopic` remains in the example.
- Existing expected output strings remain unchanged.

- [ ] **Step 1: Replace imports and field types**

Use these imports:

```wrela
use { Option } from wrela.lang.core
use { Topic, TopicPublisher, TopicSubscription, ReliableTopic, ReliablePublisher, ReliableSubscription } from machine.x86_64.topic
```

Change fields like:

```wrela
publisher: TopicPublisher<U64>
subscription: TopicSubscription<U64>
commands: ReliablePublisher<U64>
command_rx: ReliableSubscription<U64>
```

- [ ] **Step 2: Replace receive loops**

Replace each old loop:

```wrela
let next = self.counter_rx.try_next()
while next.has_message {
    self.handle(value = next.message.value)
    next = self.counter_rx.try_next()
}
```

with:

```wrela
let keep_polling = true
while keep_polling {
    match self.counter_rx.try_next() {
        Option.Some(value = value) => {
            self.handle(value = value)
        }
        Option.None => {
            keep_polling = false
        }
    }
}
```

- [ ] **Step 3: Verify**

Run:

```bash
go test ./tests/e2e -run MultiVcpu -v
rg -n "\\bhas_message\\b|U64TopicNext|U64GapTopic|U64ReliableTopic" examples/multi_vcpu_topics/main.wrela
git diff --check
```

Expected: test PASS or environment skip; `rg` prints no matches.

- [ ] **Step 4: Commit**

```bash
git add examples/multi_vcpu_topics/main.wrela tests/e2e/hello_qemu_test.go
git commit -m "feat: migrate multi vcpu topic example to generics -Codex Automated"
```

### Task 18C: Migrate Production And Arena Fixtures

**Description:** Update production-substrate and arena-memory e2e fixtures after generic topics and typed interrupt queues land.

**Files:**
- Modify: `tests/e2e/fixtures/production_substrate/main.wrela`
- Modify: `tests/e2e/fixtures/production_substrate/program.wrela`
- Modify: `tests/e2e/fixtures/arena_memory/main.wrela`
- Modify: `tests/e2e/production_substrate_qemu_test.go`

**Acceptance Criteria:**
- Production fixture imports generic topic types.
- Production fixture uses `match`/`if let` for timer, serial, EDU, and ivshmem subscription results.
- Arena fixture demonstrates `reserve_array` with a typed `FixedBuffer<T>` or remains focused on existing arena place/reserve if hello already owns the typed hot set.
- QEMU report expectations use generic topic names.

- [ ] **Step 1: Replace production subscription checks**

Use this exact shape for every production subscription poll:

```wrela
match self.timer_rx.try_next() {
    Option.Some(value = tick) => {
        self.record_tick(tick = tick)
    }
    Option.None => {
        self.timer_rx.arm_wait()
    }
}
```

For optional retry checks, use:

```wrela
if let Option.Some(value = event) = self.edu_interrupts.try_next() {
    self.edu.ack_completed(event = event)
}
```

- [ ] **Step 2: Update QEMU report expectations**

Replace old report fragments:

```go
`"TimerTickPayload"`, `"irq.serial.rx"`, `"serial.rx"`
```

with generic fragments:

```go
`"Topic<TimerTickPayload>"`, `"Option<TimerTickPayload>"`, `"irq.serial.rx"`, `"serial.rx"`
```

- [ ] **Step 3: Verify**

Run:

```bash
go test ./tests/e2e -run ProductionSubstrate -v
rg -n "\\bhas_message\\b|TimerTickNext|SerialRxNext|EduInterruptNext|IvshmemDoorbellNext" tests/e2e/fixtures/production_substrate tests/e2e/fixtures/arena_memory
rg -n "\\bmatch\\b|\\bif let\\b|Option<|Topic<|reserve_array" tests/e2e/fixtures/production_substrate tests/e2e/fixtures/arena_memory
git diff --check
```

Expected: test PASS or environment skip; first `rg` prints no matches; second `rg` prints migrated usages.

- [ ] **Step 4: Commit**

```bash
git add tests/e2e/fixtures/production_substrate tests/e2e/fixtures/arena_memory tests/e2e/production_substrate_qemu_test.go
git commit -m "feat: migrate production fixtures to expressive features -Codex Automated"
```

### Task 19: Add Region-Kind Generic Views

**Description:** Add typed MMIO, firmware, volatile, and DMA view shapes and migrate authority modules to use them where source-visible authority is currently raw.

**Files:**
- Modify: `wrela/platform/hardware/bytes.wrela`
- Modify: `wrela/platform/hardware/memory.wrela`
- Modify: `wrela/platform/uefi/types.wrela`
- Modify: `wrela/platform/acpi/root.wrela`
- Modify: `wrela/platform/acpi/tables.wrela`
- Modify: `compiler/sem/hardware_authority_test.go`
- Add negative fixture: `tests/fixtures/negative/forged_mmio_generic.wrela`

**Acceptance Criteria:**
- `Mmio<T>`, `FirmwareSlice<T>`, `Volatile<T>`, and `DmaBuffer<T>` source definitions exist.
- Ordinary modules cannot construct these from integers.
- Existing `MmioRegion` remains as a compatibility wrapper where codegen still expects `read32/write32`.

- [ ] **Step 1: Add source definitions**

In `wrela/platform/hardware/bytes.wrela`, add:

```wrela
data Mmio<T> {
    address: PhysicalAddress
}

data Volatile<T> {
    address: PhysicalAddress
}
```

In `wrela/platform/hardware/memory.wrela`, add DMA buffers because this module already owns physical memory authority:

```wrela
use { Slots } from machine.x86_64.executor_memory
use { PciDevice } from machine.x86_64.pci

data DmaBuffer<T> {
    owner: PciDevice
    slots: Slots<T>
}
```

In `wrela/platform/uefi/types.wrela`, add:

```wrela
data FirmwareAddress {
    value: PhysicalAddress
}

data FirmwareSlice<T> {
    address: FirmwareAddress
    length: U64
}
```

Keep the existing `MmioRegion` declaration unchanged in this task. It remains the compatibility wrapper for current `read32` and `write32` call sites until a later compiler/codegen migration removes that dependency.

- [ ] **Step 2: Migrate firmware table views**

In `wrela/platform/acpi/root.wrela` and `wrela/platform/acpi/tables.wrela`, keep existing byte-reader helpers but add typed firmware slice fields for table spans. Use this shape wherever a table currently stores a raw `BoundedBytes` plus table length:

```wrela
data AcpiTableView<T> {
    bytes: BoundedBytes
    typed: FirmwareSlice<T>
}
```

When constructing ACPI table views from UEFI configuration tables, derive the firmware address from the existing UEFI/ACPI authority value, never from a literal:

```wrela
let view = AcpiTableView<MadtHeader>(
    bytes = table_bytes,
    typed = FirmwareSlice<MadtHeader>(
        address = FirmwareAddress(value = table_bytes.address),
        length = table_bytes.length
    )
)
```

This construction is legal only in `platform.acpi.*` and `platform.uefi.*` modules because Task 9 marks `FirmwareSlice<T>` as protected.

- [ ] **Step 3: Add negative fixture**

Create `tests/fixtures/negative/forged_mmio_generic.wrela`:

```wrela
// expect: SEM0092: protected memory-region view construction is not allowed here
module platform.hardware.bytes
data Mmio<T> {
    address: U64
}

module negative.forged_mmio_generic
use { Mmio } from platform.hardware.bytes
data Registers { control: U32 }
executor Worker {
    start fn run(self) -> never {
        let mmio = Mmio<Registers>(address = 0xFEE00000)
        while true {}
    }
}
```

- [ ] **Step 4: Verify**

Run:

```bash
go test ./compiler/sem -run 'TestHardwareAuthority|TestProtected' -v
go test ./compiler -run TestNegativeFixtures/forged_mmio_generic.wrela -v
git diff --check
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add wrela/platform/hardware/bytes.wrela wrela/platform/hardware/memory.wrela wrela/platform/uefi/types.wrela wrela/platform/acpi/root.wrela wrela/platform/acpi/tables.wrela compiler/sem/hardware_authority_test.go tests/fixtures/negative/forged_mmio_generic.wrela
git commit -m "feat: add protected generic region views -Codex Automated"
```

---

## 10. Phase 6: Final Integration And Verification

**Description:** This phase removes compatibility branches, verifies reports and e2e behavior, and closes the milestone against the original design acceptance criteria.

**Phase Acceptance Criteria:**

- Full Go test suite passes.
- QEMU hello and production substrate tests pass where firmware is available.
- Reports show generic topics/results and deterministic instantiated type names.
- Compatibility branches for old topic/result types are removed.

**Phase Code Example:**

```go
for _, want := range []string{"Topic<TimerTickPayload>", "Option<TimerTickPayload>", "FixedBuffer<RunEvent>"} {
    if !strings.Contains(report, want) {
        t.Fatalf("report missing %s", want)
    }
}
```

### Task 20: Remove Compatibility Branches And Update Reports

**Description:** Delete transitional handling for old concrete topic/result types and make reports display generic instantiations clearly.

**Files:**
- Modify: `compiler/sem/topic_graph.go`
- Modify: `compiler/sem/topic_payload.go`
- Modify: `compiler/sem/report.go`
- Modify: `compiler/sem/report_test.go`
- Modify: `compiler/report/report.go`
- Modify: `compiler/report/report_test.go`

**Acceptance Criteria:**
- No semantic helper recognizes removed concrete topic classes.
- Reports display `Topic<TimerTickPayload>` and payload type keys.
- Report tests assert generic names.

- [ ] **Step 1: Delete compatibility branches**

Remove every branch marked:

```go
// Compatibility branch removed by Task 18 after source migration.
```

The final generic topic recognition should be:

```go
func IsTopicType(t *Type) bool {
    return t != nil &&
        (qualifiedTypeName(t) == "machine.x86_64.topic.Topic" ||
            qualifiedTypeName(t) == "machine.x86_64.topic.ReliableTopic") &&
        len(t.TypeArgs) == 1
}
```

- [ ] **Step 2: Update report rendering**

Use `Type.Display()` for user-facing type names and `Type.Key()` for unique report IDs:

```go
TopicReport{
    Type:        topic.Type.Display(),
    TypeKey:     topic.Type.Key(),
    PayloadType: topic.Payload.Display(),
    PayloadKey:  topic.Payload.Key(),
}
```

- [ ] **Step 3: Verify**

Run:

```bash
go test ./compiler/sem -run 'TestReport|TestTopic' -v
go test ./compiler/report -v
rg -n "TimerTickTopic|SerialRxTopic|EduInterruptTopic|IvshmemDoorbellTopic|TimerTickNext|SerialRxNext|EduInterruptNext|IvshmemDoorbellNext" compiler/sem compiler/ir compiler/codegen wrela examples tests/e2e/fixtures
git diff --check
```

Expected: tests PASS; `rg` prints no matches except historical test comments explicitly containing `removed concrete topic`.

- [ ] **Step 4: Commit**

```bash
git add compiler/sem/topic_graph.go compiler/sem/topic_payload.go compiler/sem/report.go compiler/sem/report_test.go compiler/report/report.go compiler/report/report_test.go
git commit -m "refactor: remove concrete topic compatibility paths -Codex Automated"
```

### Task 21: Full Acceptance Sweep

**Description:** Run the complete local verification suite and document any environment-limited checks.

**Files:**
- Modify only files needed to fix failures found by this task.

**Acceptance Criteria:**
- `go test ./...` passes.
- QEMU hello and production substrate tests pass or are explicitly skipped by environment checks already present in the tests.
- All design-doc acceptance criteria are mapped to implemented behavior.

- [ ] **Step 1: Run full Go suite**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 2: Run e2e checks**

Run:

```bash
go test ./tests/e2e -run 'Hello|ProductionSubstrate' -v
```

Expected: PASS on machines with QEMU/OVMF. If the tests skip because firmware or QEMU is unavailable, record the exact skip line in the commit body.

- [ ] **Step 3: Run source acceptance scans**

Run:

```bash
rg -n "\\b(TimerTickNext|SerialRxNext|EduInterruptNext|IvshmemDoorbellNext|U64TopicNext|U64PublishResult)\\b" wrela examples tests compiler
rg -n "\\b(has_message|published|full)\\b" wrela examples tests/e2e/fixtures
rg -n "\\b(Topic|Option|Result|Slots|MutableSlice|Slice)<|\\breserve_array\\b|\\bmatch\\b|\\bif let\\b" wrela examples tests/e2e/fixtures
git diff --check
```

Expected: first two scans print no active source matches; third scan prints migrated feature usage; `git diff --check` prints nothing.

- [ ] **Step 4: Fix failures surgically**

If any command fails, fix only the files required by the failure. Example fixes:

```go
// If a report test still expects TimerTickTopic:
want := "Topic<TimerTickPayload>"
if !strings.Contains(got, want) {
    t.Fatalf("report missing %s:\n%s", want, got)
}
```

```wrela
// If a fixture still uses has_message:
match self.rx.try_next() {
    Option.Some(value = event) => {
        self.handle(event = event)
    }
    Option.None => {
        self.rx.arm_wait()
    }
}
```

- [ ] **Step 5: Commit**

```bash
git add compiler wrela examples tests docs
git commit -m "test: verify language expressiveness milestone -Codex Automated"
```

---

## 11. Appendix A: Diagnostic Contract

```text
SEM0076 duplicate generic type parameter
SEM0077 generic type arity mismatch
SEM0078 unknown type parameter or type argument
SEM0079 generic or enum type arguments cannot be inferred
SEM0080 unsized type used where layout is required
SEM0081 missing trait implementation
SEM0082 trait method signature mismatch
SEM0083 ambiguous or overlapping impl
SEM0084 non-exhaustive match
SEM0085 impossible enum variant pattern
SEM0086 const expression overflow
SEM0087 non-const operand in const expression
SEM0088 invalid sizeof or alignof operand
SEM0089 static assertion failed
SEM0090 slot count or reservation size overflow
SEM0091 slots or slice lifetime escape
SEM0092 protected memory-region view construction is not allowed here
SEM0093 raw Slots memory cannot be read directly
SEM0094 enum variant constructor is invalid
SEM0095 match or if-let pattern binding is invalid
SEM0096 protected view field access is not allowed here
```

---

## 12. Appendix B: Exact Runtime Algorithms

### `reserve_array`

```text
input: arena, element type T, count U64, optional align U64
element_size = sizeof(T)
element_align = alignof(T)
requested_align = optional align or element_align
reject if requested_align is zero, not a power of two, or less than element_align
byte_len = checked_mul(count, element_size)
cursor = align_up(arena.next_offset, requested_align)
end = checked_add(cursor, byte_len)
if end > arena.arena_length: call _wrela_memory_oom
address = arena.arena_base + cursor
arena.next_offset = end
return Slots<T>(address = address, capacity = count)
```

### `Slots<T>.write`

```text
input: slots, index, value
if index >= slots.capacity: call _wrela_memory_oom
address = slots.address + index * sizeof(T)
copy value bytes to address using monomorphized T layout
return Unit
```

### `Slots<T>.fill`

```text
input: slots, value
i = 0
while i < slots.capacity:
    address = slots.address + i * sizeof(T)
    copy value bytes to address using monomorphized T layout
    i = i + 1
return MutableSlice<T>(address = slots.address, length = slots.capacity)
```

### Enum Layout

```text
tag: U64 at offset 0
payload offset: 8
variant ordinal: declaration order starting at 0
payload area: max size/alignment of all variant payload records
total size: align_up(8 + max_payload_size, max(8, max_payload_align))
```

### Match Lowering

```text
for each arm except wildcard:
    test enum tag == variant ordinal
    if true:
        bind each payload field by loading from payload offset
        execute arm body
        skip remaining arms
wildcard:
    execute body if no previous arm matched
semantic checker rejects non-exhaustive match unless wildcard exists
```

---

## 13. Appendix C: Full Milestone Acceptance Criteria

- Generic `data`, `class`, `enum`, `trait`, `impl`, and method surfaces parse and type-check.
- Concrete generic instantiations monomorphize into deterministic layouts and symbols.
- Traits provide static capability contracts with explicit impls and direct concrete calls after monomorphization.
- `Option<T>` and `Result<T, E>` replace boolean-plus-payload result shapes in platform source and examples.
- `if let` and exhaustive `match` work over generic enums.
- Constants, `sizeof`, `alignof`, and `static_assert` are used in memory, queue, topic, or hardware declarations.
- `Slots<T>`, `Slice<T>`, and `MutableSlice<T>` carry hidden lifetimes through generic containers.
- `tick.reserve_array(Type, count = n)` reserves fixed-capacity typed memory using the same bump discipline as `place` and `reserve`.
- Raw `Slots<T>` memory is not directly readable.
- `Slots<T>.fill(value)` initializes the whole slots region and returns a `MutableSlice<T>` view.
- Initialized reads go through `Slice<T>`, `MutableSlice<T>`, or container APIs.
- MMIO, volatile, DMA, firmware, physical RAM, and executor-local region kinds are distinct and cannot be forged from integers by ordinary code.
- Generic topics, subscriptions, queues, and result values are demonstrated in platform source.
- Existing compiler, semantic, layout, codegen, integration, and relevant QEMU tests pass.
