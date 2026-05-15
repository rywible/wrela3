# Wrela Interrupt Events Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add source-visible device interrupt events to Wrela, extend the existing hello-world example with IOAPIC, PCI MSI, and PCI MSI-X interrupt paths, and prove all three end-to-end in QEMU.

**Architecture:** Driver paths declare one `interrupt receiver -> Type` conversion body. Executors declare `on path.interrupt(param: Type)` handlers for the typed interrupt event produced by each driver path they own. The compiler derives bindings from executor fields, enforces that every owned interrupt-capable path receiver is handled exactly once with the exact event type, and lowers the first runtime backend to three x86_64 interrupt routes on QEMU q35: COM1 receive through IOAPIC, QEMU EDU through PCI MSI, and QEMU ivshmem-doorbell through PCI MSI-X.

**Tech Stack:** Go 1.22+; existing hand-written lexer/parser/semantic checker; existing IR and direct x86_64 codegen; Wrela platform source under `wrela/`; QEMU q35 + OVMF + `ivshmem-server` e2e tests.

**Hardware References:** QEMU EDU device docs define the `1234:11e8` PCI test device, its BAR0 MMIO registers, interrupt raise/ack registers, and MSI support: <https://www.qemu.org/docs/master/specs/edu.html>. QEMU ivshmem docs define the shared-memory server, socket chardev setup, and `ivshmem-doorbell` interrupt path: <https://qemu-project.gitlab.io/qemu/system/devices/ivshmem.html>. This plan does not hardcode MSI/MSI-X capability offsets; `Q35PciInterruptConfigurator` walks PCI capabilities at runtime and only hardcodes q35 lab BDFs.

---

## 0. How To Execute This Plan

This plan is an implementation contract. Do not reopen the source model during task execution.

For junior execution:

- Follow tasks in numeric order unless a tech lead explicitly assigns a parallel stream from Section 3.
- Write the failing test first for every code task.
- Run the exact command listed by the task.
- Implement only the code needed for that task.
- Run `git diff --check`.
- Commit with the exact commit message listed by the task.

For parallel execution:

- Frontend and docs tasks can run before runtime codegen tasks.
- Runtime tasks depend on the IR contracts from Tasks 9-10.
- Only the tech lead may change syntax, symbol formats, diagnostics, or e2e success text.

Definition of done for any task:

- All checkbox steps are complete.
- The intended failing test fails before implementation.
- The intended passing test passes after implementation.
- No task introduces source-level CPU trap handlers.
- `git diff --check` passes.
- A commit is created with a message ending in `-Codex Automated`.

Rollback rule for oversized tasks:

- If a task exposes missing infrastructure larger than the task describes, stop at the failing test and split the infrastructure into a new task before implementing it.
- Do not hide new parser, semantic, IR, assembler, or runtime mechanisms inside a broad task.
- Update this plan before proceeding so the next worker can reproduce the decision.

### 0.5 Test Helpers Used By This Plan

Existing helpers:

| Helper | File | Signature |
| --- | --- | --- |
| `parseModuleForTest` | `compiler/parse/parser_test.go` | `func parseModuleForTest(t *testing.T, src string) (*ast.Module, []diag.Diagnostic)` |
| `parseModulesForTest` | `compiler/sem/testutil_test.go` | `func parseModulesForTest(t *testing.T, sources ...string) []*ast.Module` |
| `mustBuildIndex` | `compiler/sem/testutil_test.go` | `func mustBuildIndex(t *testing.T, modules []*ast.Module) *Index` |
| `mustCheck` | `compiler/sem/testutil_test.go` | `func mustCheck(t *testing.T, index *Index, modules []*ast.Module) *CheckedProgram` |
| `typeDiagsForModules` | `compiler/sem/testutil_test.go` | `func typeDiagsForModules(t *testing.T, sourceText string) (*CheckedProgram, []diag.Diagnostic)` |
| `hasCode` | `compiler/sem/testutil_test.go` | `func hasCode(ds []diag.Diagnostic, code string) bool` |
| `symbolBytes` | `compiler/integration_test.go` | `func symbolBytes(t *testing.T, image *codegen.Image, symbol string) []byte` |
| `containsBytes` | `compiler/integration_test.go` | `func containsBytes(haystack, needle []byte) bool` |
| `copyFile` | `tests/e2e/hello_qemu_test.go` | `func copyFile(t *testing.T, src, dst string)` |

Task 0 adds the missing helpers used by later tests:

| Helper | New file | Signature |
| --- | --- | --- |
| `buildIndexForTest` | `compiler/sem/interrupt_testutil_test.go` | `func buildIndexForTest(t *testing.T, sourceText string) (*Index, []diag.Diagnostic)` |
| `checkModuleForTest` | `compiler/sem/interrupt_testutil_test.go` | `func checkModuleForTest(t *testing.T, sourceText string) (*CheckedProgram, []diag.Diagnostic)` |
| `checkedProgramForTest` | `compiler/ir/interrupt_testutil_test.go` | `func checkedProgramForTest(t *testing.T, sourceText string) *sem.CheckedProgram` |
| `symbolBytes` | `compiler/codegen/interrupt_testutil_test.go` | `func symbolBytes(t *testing.T, image *Image, symbol string) []byte` |
| `containsBytes` | `compiler/codegen/interrupt_testutil_test.go` | `func containsBytes(haystack, needle []byte) bool` |
| `ivshmemServer` | `tests/e2e/hello_qemu_test.go` | `var ivshmemServer = "ivshmem-server"` |

Task 15 adds `interruptProgramForCodegenTest` after `ir.InterruptBinding` exists. Do not add that helper in Task 0 because it would not compile before Task 9.

Pre-existing internals this plan calls:

| Internal | File | Used by |
| --- | --- | --- |
| `sem.Type`, `sem.KindDriverPath`, `sem.KindExecutor`, `sem.Method.IsStart` | `compiler/sem/types.go` | Tasks 7, 8, 16 |
| `sem.ImageGraph`, `sem.ExecutorNode`, `sem.DriverPathNode` | `compiler/sem/image_graph.go` | Tasks 7, 8 |
| `ir.TypeKind*`, `ir.TypeInfo`, `ir.FieldInfo`, `ir.DataObject`, `ir.AsmMethod`, `ir.ConstInt`, `ir.Return`, `ir.Operation` | `compiler/ir/ir.go` | Tasks 9, 15, 16 |
| `ir.Program.Types` | `compiler/ir/ir.go` | Tasks 9, 15, 16 |
| `codegen.Emitter`, `codegen.Frame`, `emitValueAddress`, `emitMovDataAddressToReg`, `internalReloc`, `compileAsmMethodUnit`, `diagnosticPhase` | `compiler/codegen/x64.go`, `compiler/codegen/asm_method.go` | Tasks 14A, 15, 16 |
| `lowerAndEncodeAsmMethod` | `compiler/codegen/asm_method.go` | Task 14A |
| `asm.MustLookup` | `compiler/asm/regs.go` | Tasks 11, 15, 16 |
| `parse.ParseGraph`, `source.Graph`, `source.NewFile` | `compiler/parse/parser.go`, `compiler/source/graph.go`, `compiler/source/file.go` | Task 0 |

Existing syntax used in semantic tests:

- `unique class` already exists and is used by ownership-transfer tests.
- `image`, `transitions`, `phase delegated_hardware`, and `phase owned_hardware` already exist and are checked by `compiler/sem/phase_test.go`.
- Image phase construction is the only place normal construction of non-data runtime values is allowed.

---

## 1. Frozen Decisions

Do not change these decisions during implementation.

- Wrela source does not expose CPU traps.
- CPU exceptions remain platform fatal machinery.
- Device interrupts are source-visible only as driver-path interrupt events.
- The first runtime backend supports exactly three hardware interrupt sources:
  - COM1 receive routed through IOAPIC GSI 4 to vector `0x40`
  - QEMU EDU PCI device MSI routed to vector `0x41`
  - QEMU ivshmem-doorbell PCI device MSI-X vector 0 routed to vector `0x42`
- The first runtime backend uses the x86_64 Local APIC, IOAPIC, PCI MSI, and PCI MSI-X paths used by modern PC-class hardware.
- The legacy 8259 PIC is not used by Wrela source or generated runtime code.
- Shared interrupt lines, timers, IPIs, AP startup, x2APIC, ACPI MADT discovery, full PCI enumeration, and interrupt queues are deferred.
- The COM1 interrupt vector is fixed at `0x40`.
- The QEMU EDU MSI vector is fixed at `0x41`.
- The QEMU ivshmem-doorbell MSI-X vector is fixed at `0x42`.
- The first e2e uses QEMU q35's standard APIC MMIO addresses: Local APIC `0xFEE00000`, IOAPIC `0xFEC00000`.
- The first MSI e2e uses QEMU EDU at BDF `00:05.0`, vendor/device `1234:11e8`, BAR0 read from PCI config space, and one MSI message to Local APIC ID `0`.
- The first MSI-X e2e uses QEMU ivshmem-doorbell receiver at BDF `00:06.0`, sender at BDF `00:07.0`, vendor/device `1af4:1110`, BAR0/BAR1 read from PCI config space, one vector, and an `ivshmem-server` socket.
- Production hardware discovery must replace those q35 constants with ACPI MADT parsing in a later plan.
- The serial path enables COM1 receive interrupts by setting IER bit 0 and MCR OUT2.
- The EDU MSI path enables MSI for BDF `00:05.0` by walking the PCI capability list for capability ID `0x05`, then writing message address `0xFEE00000`, message data `0x41`, and MSI Enable.
- The ivshmem MSI-X path enables MSI-X for BDF `00:06.0` by walking the PCI capability list for capability ID `0x11`, then writing BAR1 table entry 0 with message address `0xFEE00000`, message data `0x42`, vector control `0`, and MSI-X Enable.
- Interrupt delivery in this phase is direct: the generated vector stub calls the path event body, then the executor `on` handler, then writes Local APIC EOI, then executes `iretq`.
- Direct delivery is intentionally strict at the handler source boundary: interrupt handlers may not contain loops, allocate, call `halt_forever`, reconfigure interrupt hardware, or enable CPU interrupts. The hello handler may call existing serial write helpers for lab observability even though those helpers poll internally; production delivery queues are deferred.
- Future executor queues may replace direct delivery without changing the source syntax.

Source syntax is fixed:

```wrela
driver path SerialConsolePath {
    interrupt receiver -> SerialPathInterrupt {
        let status = self.registers.read8(offset = 5)
        if (status & 0x01) != 0 {
            return SerialPathInterrupt(byte = self.registers.read8(offset = 0))
        }
        return SerialPathInterrupt(byte = 0)
    }
}

executor HelloWorld {
    serial_path: SerialConsolePath

    on serial_path.interrupt(event: SerialPathInterrupt) {
        self.serial_path.write(self.memory.static_bytes("serial interrupt: "))
        self.serial_path.write_byte(value = event.byte)
        self.serial_path.write(self.memory.static_bytes("\n"))
        self.serial_path.ack_receive(event = event)
    }
}
```

There is no `using` keyword, no separate decoder function, no `interrupt handler` method, and no explicit `.interrupts.bind(...)` call.

Completeness rule:

```text
For every executor E:
  For every direct field F on E:
    If F's type is a driver path with interrupt events:
      E must declare exactly one `on F.interrupt(param: EventType) { ... }` handler.
```

The compiler must reject:

- an executor that owns an interrupt-capable path but omits an event handler
- two handlers for the same owned path receiver
- an `on` handler for a field that is not an executor field
- an `on` handler for a path receiver that does not exist
- an `on` handler whose event parameter type does not exactly match the path event return type
- a normal call to an interrupt event body
- explicit `.interrupts.bind(...)`

The driver path event body converts hardware state into a typed event object. The executor handler receives that typed object with a normal explicit parameter type. This is legal:

```wrela
driver path SerialConsolePath {
    interrupt receiver -> SerialPathInterrupt {
        return SerialPathInterrupt(byte = self.registers.read8(offset = 0))
    }
}

on serial_path.interrupt(event: SerialPathInterrupt) {
    self.serial_path.ack_receive(event = event)
}
```

This is illegal:

```wrela
on serial_path.interrupt(event: OtherInterrupt) {
}
```

---

## 2. Package Contracts

### `compiler/lex`

Add keywords:

```go
KeywordInterrupt
KeywordReceiver
KeywordOn
```

Do not add `KeywordUsing`.

### `compiler/ast`

Add path interrupt events:

```go
type InterruptEventDecl struct {
    EventType string
    Body      []Stmt
    SpanV     source.Span
}

func (d *InterruptEventDecl) Span() source.Span { return d.SpanV }

type DriverPathDecl struct {
    Name            string
    Fields          []Field
    Methods         []MethodDecl
    InterruptEvents []InterruptEventDecl
    SpanV           source.Span
}
```

Add executor `on` handlers:

```go
type OnHandlerDecl struct {
    PathField string
    ParamName string
    ParamType string
    Body      []Stmt
    SpanV     source.Span
}

func (d *OnHandlerDecl) Span() source.Span { return d.SpanV }

type ExecutorDecl struct {
    Name       string
    Fields     []Field
    Methods    []MethodDecl
    OnHandlers []OnHandlerDecl
    SpanV      source.Span
}
```

Do not model `on` handlers as methods. They are interrupt continuations with stricter rules than normal executor methods.

### `compiler/parse`

Driver path member grammar:

```text
driver_path_member =
    field_decl
  | method_decl
  | interrupt_event_decl

interrupt_event_decl =
  "interrupt" "receiver" "->" type_name block
```

Executor member grammar:

```text
executor_member =
    field_decl
  | method_decl
  | on_handler_decl

on_handler_decl =
  "on" identifier "." "interrupt" "(" identifier ":" type_name ")" block
```

The parser must reject `on serial_path.receive(event: Type)` and `on serial_path.interrupt(event)`; `interrupt` is the only legal selector after the path field and handler payload types are explicit at the executor boundary.

Named call and constructor arguments use `=`, not `:`:

```text
named_arg =
  identifier "=" expr
```

This is legal:

```wrela
SerialPathInterrupt(byte = 0)
self.serial_path.ack_receive(event = event)
```

This is illegal:

```wrela
SerialPathInterrupt(byte: 0)
self.serial_path.ack_receive(event: event)
```

Keep `:` only for declarations and type annotations: fields, function parameters, image phase parameters, `on path.interrupt(event: Type)`, and data/class/driver/executor member declarations.

Parser architecture decision:

- Keep one shared member parser, but make the composite context explicit.
- Add a small parser-local kind enum in `compiler/parse/parser.go`:

```go
type compositeKind int

const (
    compositeClass compositeKind = iota
    compositeDriver
    compositeDriverPath
    compositeExecutor
)
```

- Change `parseCompositeMembers()` to:

```go
func (p *Parser) parseCompositeMembers(kind compositeKind) ([]ast.Field, []ast.MethodDecl, []ast.InterruptEventDecl, []ast.OnHandlerDecl, source.Span, []diag.Diagnostic)
```

- Update the four existing call sites:

```go
fields, methods, _, _, _, ds := p.parseCompositeMembers(compositeClass)
fields, methods, _, _, _, ds := p.parseCompositeMembers(compositeDriver)
fields, methods, interruptEvents, _, _, ds := p.parseCompositeMembers(compositeDriverPath)
fields, methods, _, onHandlers, _, ds := p.parseCompositeMembers(compositeExecutor)
```

- In the member loop:
- `lex.KeywordInterrupt` is legal only when `kind == compositeDriverPath` or as the selector in `on path.interrupt(...)`.
- `lex.KeywordReceiver` is legal only immediately after `interrupt` in an `interrupt_event_decl`.
- `lex.KeywordOn` is legal only when `kind == compositeExecutor`; it parses `on_handler_decl`.
  - `lex.KeywordAsm`, `lex.KeywordStart`, and `lex.KeywordFn` keep parsing normal methods.
  - `lex.Identifier` keeps parsing fields.
  - Any misplaced `interrupt` or `on` returns the same `diag.PAR0001` unexpected-token style used by current invalid declaration-body syntax.

No three-way `interrupt` lookahead exists in this design. `interrupt` always introduces a driver-path receiver declaration and the next token must be `receiver`. `on` always introduces an executor handler and the selector must be `field.interrupt`.

Expression selectors are the one exception: after a dot, `interrupt` may be accepted as a selector token so `self.path.interrupt()` and old bind-style expressions can parse and receive `SEM0019`. This does not make `interrupt` a legal declaration name or bare identifier.

### `compiler/diag`

Add new semantic codes. Do not reuse existing semantic codes for interrupt-specific failures.
Continue using existing diagnostics only when their current meaning already matches the failure. For this plan, an unresolved interrupt event type uses the existing unknown-type diagnostic `SEM0002`; do not redefine any `SEM0001` through `SEM0009` code.

```go
const (
    SEM0014 = "SEM0014" // duplicate interrupt event or duplicate on handler
    SEM0015 = "SEM0015" // invalid interrupt event declaration/body
    SEM0016 = "SEM0016" // invalid on handler declaration/body
    SEM0017 = "SEM0017" // executor missing required on handler
    SEM0018 = "SEM0018" // on handler references invalid field or event
    SEM0019 = "SEM0019" // illegal normal use of interrupt event/on handler
    SEM0020 = "SEM0020" // unsupported interrupt runtime shape
)
```

### `compiler/sem`

Add the checked interrupt binding model:

```go
type InterruptBinding struct {
    ExecutorModule string
    ExecutorType   string
    PathField      string
    PathType       string
    EventType      *Type
    Vector         uint8
    Span           source.Span
}
```

Extend checked program:

```go
type CheckedProgram struct {
    Modules           []*ast.Module
    Index             *Index
    ImageGraph        ImageGraph
    OwnedRoot         *Type
    InterruptBindings []InterruptBinding
}
```

`CheckedProgram.InterruptBindings` means reachable runtime bindings only. The checker may collect all type-level required handlers internally while checking completeness, but only bindings reachable from the image graph and accepted by `checkInterruptRuntimeSupport` are exposed to IR lowering.

No method-reference feature is added in this plan. `on` handlers are AST declarations, not values. `.interrupts.bind(...)`, `hello.on_serial`, and `hello.serial_path.interrupt` must not be interpreted as method references or event-source values. If old bind-style source appears, semantic checking recognizes the full `CallExpr{Receiver: FieldExpr{Field: "interrupts"}, Method: "bind"}` shape only to reject it as `SEM0019`; the compiler must not introduce a synthetic `.interrupts` bind namespace. This does not reserve the field name `interrupts`: ordinary executor fields like `self.interrupts.enable_cpu_interrupts()` still type-check through normal field and method resolution.

Semantic check order:

```text
1. existing image phase signature checks
2. existing construction placement checks
3. existing expression and return type checks
4. existing delegated-only propagation checks
5. existing unique cardinality checks
6. existing owned-root minting checks
7. existing driver/path/executor graph checks
8. interrupt event declaration checks
9. executor on-handler checks
10. executor interrupt completeness checks
11. interrupt runtime support checks
```

Runtime support check:

```text
The first runtime assigns vectors for every reachable binding whose path type is one of the supported shapes below. It emits unsupported-runtime diagnostics only for bindings reachable from the image graph.
The first runtime accepts exactly these reachable bindings:
  path type: machine.x86_64.serial.SerialConsolePath
  handler selector: interrupt
  vector: 0x40
  hardware route: IOAPIC GSI 4 to Local APIC ID 0

  path type: machine.x86_64.edu.EduMsiPath
  handler selector: interrupt
  vector: 0x41
  hardware route: PCI MSI message to Local APIC ID 0

  path type: machine.x86_64.ivshmem.IvshmemMsixPath
  handler selector: interrupt
  vector: 0x42
  hardware route: PCI MSI-X table entry 0 message to Local APIC ID 0

Any other reachable interrupt binding emits SEM0020.
```

Reachability means: only bindings for executor instances present in `c.graph.Executors` are runtime-checked. Executor types declared but never constructed by the image graph do not emit `SEM0020`.

### `compiler/ir`

Add metadata and lowered functions:

```go
type InterruptEvent struct {
    Symbol        string
    PathType      Type
    EventType     Type
    FunctionSymbol string
}

type OnHandler struct {
    Symbol        string
    ExecutorType  Type
    PathField     string
    EventType     Type
    FunctionSymbol string
}

type InterruptBinding struct {
    EventSymbol           string
    HandlerSymbol         string
    EventFunctionSymbol   string
    HandlerFunctionSymbol string
    ExecutorType          Type
    PathField             string
    PathFieldOffset       int
    ContextSymbol         string
    EventStorageSymbol    string
    EventStorageSize      int
    Vector                uint8
}

type InterruptContextPathField struct {
    FieldName string
    Offset    int
    Type      Type
}

type InterruptContext struct {
    Symbol       string
    ExecutorType Type
    Size         int
    PathFields   []InterruptContextPathField
}

type InterruptContextStore struct {
    ContextSymbol string
    Source        Value
    SourceType    Type
    Size          int
}

func (InterruptContextStore) isOperation() {}

type Program struct {
    Functions         []Function
    AsmMethods        []AsmMethod
    Data              []DataObject
    WritableData      []DataObject
    Entry             EntryAdapter
    Types             map[string]TypeInfo
    InterruptEvents   []InterruptEvent
    OnHandlers        []OnHandler
    InterruptBindings []InterruptBinding
    InterruptContexts []InterruptContext
}
```

Symbol formats:

```text
interrupt_event::<module>::<PathType>::interrupt
on_handler::<module>::<ExecutorType>::<path_field>::interrupt
method::<module>::<ReceiverType>::<method_name>
```

The separator is exactly `::`. Dots inside module names are preserved inside a segment, so `machine.x86_64.serial` is one module segment and must not be split on `.` when parsing these symbols.

These are logical metadata symbols. Actual code symbols stored in `ir.Function.Symbol` and emitted into the PE image must continue to use the existing `compiler/ir/lower.go` `symbolName(...)` sanitizer, for example `_wrela_event_fn_machine_x86_64_serial_SerialConsolePath_interrupt`. Dispatch stubs call actual function symbols, never the logical `::` strings.

Event bodies and `on` handlers lower to ordinary IR functions using these `symbolName(...)` part lists:

```text
event_fn::<module>::<PathType>::interrupt
on_fn::<module>::<ExecutorType>::<path_field>::interrupt
```

For example, `symbolName("event_fn", "machine.x86_64.serial", "SerialConsolePath", "interrupt")` produces `_wrela_event_fn_machine_x86_64_serial_SerialConsolePath_interrupt`. The `event_fn::...` and `on_fn::...` strings are documentation shorthand only; tests must assert the sanitized symbol stored in IR.

Codegen metadata intentionally does not carry `EventType` as a rendered string. Type-sensitive validation finishes in semantic checking and IR lowering; codegen consumes only `EventSymbol`, `HandlerSymbol`, vector, and context-layout data.

`Program.Data` remains read-only and is emitted into `.rdata`. Interrupt contexts and interrupt event return slots must use `Program.WritableData`, emitted into a writable `.data` PE section with characteristics `0xC0000040` (`IMAGE_SCN_CNT_INITIALIZED_DATA | IMAGE_SCN_MEM_READ | IMAGE_SCN_MEM_WRITE`). Do not append interrupt runtime storage to `Program.Data`.

Interrupt dispatch ABI:

```text
1. The generated vector dispatch stub preserves all general-purpose registers it clobbers.
2. The interrupt receiver function uses the existing data-return ABI. Before calling it, the stub sets r10 = address of binding.EventStorageSymbol.
3. The interrupt receiver function is called with rdi = the driver-path handle loaded from `[executor interrupt context + binding.PathFieldOffset]`.
4. The interrupt receiver returns the event object address in rax, exactly like normal Wrela data-return functions.
5. The generated dispatch stub copies rax into rsi before calling the executor handler.
6. The executor on-handler function is called with rdi = address of the executor interrupt context and rsi = event object address.
7. The dispatch stub, not the IDT wrapper, executes the final iretq.
```

This ABI intentionally reuses the backend's existing record-return path instead of inventing an interrupt-only event representation. Every binding gets one writable event-storage object sized from the event type's `TypeInfo.StorageSize`.

### `compiler/codegen`

Add image metadata:

```go
type InterruptBinding struct {
    EventSymbol           string
    HandlerSymbol         string
    EventFunctionSymbol   string
    HandlerFunctionSymbol string
    PathFieldOffset       int
    ContextSymbol         string
    EventStorageSymbol    string
    EventStorageSize      int
    Vector                uint8
}

type Image struct {
    EntrySymbol       string
    Sections          []Section
    Symbols           map[string]uint64
    Relocs            []Reloc
    InterruptBindings []InterruptBinding
}
```

Add these generated runtime symbols when their matching bindings exist:

```text
_wrela_interrupt_vector40_serial
_wrela_interrupt_vector41_edu_msi
_wrela_interrupt_vector42_ivshmem_msix
```

Each generated symbol must:

1. Save general-purpose registers used by the compiler.
2. Call the lowered event function for its binding.
3. Call the lowered `on` handler for the owning executor.
4. Send EOI to the Local APIC by writing `0` to `local_apic_base + 0xB0`.
5. Restore registers.
6. Return with `iretq`.

### `compiler/qemu`

Extend QEMU options:

```go
type Options struct {
    QEMUBinary          string
    IvshmemServerBinary string
    OVMFCode            string
    OVMFVars            string
    ESPDir              string
    ImagePath           string
    Memory              string
    CPU                 string
    Timeout             time.Duration
    SuccessText         string
    InputText           string
    EnableEdu           bool
    EnableIvshmemMsix   bool
    IvshmemSocketPath   string
    IvshmemStartupTimeout time.Duration
}
```

`Run` must set `cmd.Stdin = strings.NewReader(opts.InputText)` when `InputText != ""`.
When `EnableEdu` is true, QEMU args must include `-device edu,addr=0x5`.
When `EnableIvshmemMsix` is true, `Run` must start one `ivshmem-server` with `-n 1`, create two socket chardevs connected to `IvshmemSocketPath`, and add two `ivshmem-doorbell` devices at `addr=0x6` and `addr=0x7`. If `IvshmemSocketPath` is empty, `Run` creates one inside its temporary directory.

---

## 3. Parallel Work Map

This is not seven fully parallel streams. The language pipeline is mostly sequential. The real parallelism is in isolated infrastructure that can start before semantic work finishes.

| Start Window | Tasks | Owner | Dependencies | Notes |
| --- | --- | --- | --- | --- |
| Hour 0 | 0 | test helpers | none | Must complete before later copied tests compile. |
| Hour 0 | 1, 1A, 2-4 | `lex`, `ast`, `parse` | none | Sequential inside the stream; Task 1A updates named argument syntax before interrupt parser tests. |
| Hour 0 | 13 | `compiler/asm` | none | `iretq` is independent exact-byte assembler work. |
| After Task 0 | 10A | `compiler/ir`, `compiler/codegen` | Task 0 | Binary shift/or support is independent compiler plumbing required before Task 11 platform source can compile. |
| Hour 0 | 17 | `compiler/qemu` | none | Pure QEMU argument/process work; can merge before compiler support. |
| Hour 0 | 19 | docs | syntax shape frozen | Docs can draft early, but final verification waits for Task 18. |
| After Task 4 and 10A | 11 | `wrela/*`, source-codegen tests | Tasks 1, 1A, 2-4, 10A | Source modules can be written and parser/codegen tests can fail forward while sem/IR work proceeds. |
| After Task 4 | 5-8 | `sem`, `diag` | Tasks 1, 1A, 2-4 | Sequential semantic model. |
| After Task 8 | 9-10 | `ir`, `codegen`, `compiler` | Tasks 5-8 | Sequential metadata handoff. |
| After Tasks 10-11 | 12 | `examples/hello/*`, integration | Tasks 0-11 | Full hello build exercises test helpers, frontend, semantics, IR, source modules, and build metadata. |
| After Tasks 10, 10A, 13 | 14A-16 | `asm`, `codegen`, `platform.uefi` | Tasks 10, 10A, 13 | Runtime codegen is sequential; 14A isolates external branch relocations before IDT source work. |
| After Tasks 12, 14A-17 | 18 | `tests/e2e` | Tasks 12, 14A-17 | E2E needs the image, runtime stubs, and QEMU devices. |
| Final | 20 | all | Tasks 0-19 | Verification only. |

Integration checkpoints:

- After Task 4, parser accepts the new source syntax and rejects misplaced `interrupt`/`on` members.
- After Task 8, semantic checks enforce handler completeness for every owned interrupt-capable path.
- After Task 8, old `.interrupts.bind(...)` source is rejected as an illegal interrupt use.
- After Task 10, `compiler.Build` exposes checked interrupt metadata through `BuildResult.Image`.
- After Task 12, the existing hello source uses `SerialConsolePath`, `EduMsiPath`, `IvshmemMsixPath`, and matching `on` handlers.
- After Task 16, generated PE images contain real vectors `0x40`, `0x41`, and `0x42` interrupt stubs and IDT gates.
- After Task 18, QEMU observes serial output from the IOAPIC, MSI, and MSI-X handlers.

Codegen ownership warning: Tasks 14A, 15, and 16 all touch `compiler/codegen/x64.go` and related relocation flow. Do not run those tasks concurrently in one worktree. Parallel workers may do them only in separate worktrees with one integration owner merging in task order.

---

## 4. Canonical Source Shape

### `wrela/machine/x86_64/serial.wrela`

Keep `SerialWriterRegisters` and `SerialDriver`. Add `SerialConsolePath`; do not delete `SerialWritePath` in this plan.

```wrela
data SerialPathInterrupt {
    byte: U8
}

driver path SerialConsolePath {
    owner: ExecutorPlacement
    registers: SerialWriterRegisters

    interrupt receiver -> SerialPathInterrupt {
        let status = self.registers.read8(offset = 5)
        if (status & 0x01) != 0 {
            return SerialPathInterrupt(byte = self.registers.read8(offset = 0))
        }
        return SerialPathInterrupt(byte = 0)
    }

    fn enable_receive_interrupts(self) {
        self.registers.write8(offset = 1, value = 0x01)
        self.registers.write8(offset = 4, value = 0x0B)
    }

    fn ack_receive(self, event: SerialPathInterrupt) {
        // COM1 receive is acknowledged by reading offset 0 above; this method exists so all interrupt paths expose an explicit ack hook.
    }

    fn write(self, bytes: Bytes) {
        for byte in bytes {
            self.wait_until_ready()
            self.write_byte(value = byte)
        }
    }

    fn write_byte(self, value: U8) {
        self.wait_until_ready()
        self.registers.write8(offset = 0, value = value)
    }

    fn wait_until_ready(self) {
        while (self.registers.read8(offset = 5) & 0x20) == 0 {
            self.pause()
        }
    }

    asm fn pause(self) {
        pause
        ret
    }
}
```

### `wrela/machine/x86_64/interrupts.wrela`

Create this module:

```wrela
module machine.x86_64.interrupts

class LocalApic {
    base: VirtualAddress

    asm fn enable(self) {
        mov r11, self.base
        mov eax, 0x1FF
        mov [r11 + 0xF0], eax
        ret
    }

    asm fn eoi(self) {
        mov r11, self.base
        mov eax, 0
        mov [r11 + 0xB0], eax
        ret
    }
}

class IoApic {
    base: VirtualAddress

    asm fn route_gsi4_to_vector40(self) {
        mov r11, self.base
        mov eax, 0x18
        mov [r11], eax
        mov eax, 0x40
        mov [r11 + 0x10], eax

        mov eax, 0x19
        mov [r11], eax
        mov eax, 0
        mov [r11 + 0x10], eax
        ret
    }
}

class ApicInterruptController {
    local_apic: LocalApic
    io_apic: IoApic

    asm fn enable_cpu_interrupts(self) {
        sti
        ret
    }

    fn initialize_for_com1_receive(self) {
        self.local_apic.enable()
        self.io_apic.route_gsi4_to_vector40()
    }

    fn eoi(self) {
        self.local_apic.eoi()
    }
}
```

### `wrela/machine/x86_64/edu.wrela`

Create this module for the QEMU EDU MSI proof path:

```wrela
module machine.x86_64.edu

data EduInterrupt {
    status: U32
}

driver path EduMsiPath {
    mmio_base: VirtualAddress

    interrupt receiver -> EduInterrupt {
        return EduInterrupt(status = self.read32(offset = 0x24))
    }

    asm fn read32(self, offset: U64) -> U32 {
        mov r11, self.mmio_base
        add r11, offset
        mov eax, [r11]
        ret
    }

    asm fn write32(self, offset: U64, value: U32) {
        mov r11, self.mmio_base
        add r11, offset
        mov eax, value
        mov [r11], eax
        ret
    }

    fn raise_test_interrupt(self) {
        self.write32(offset = 0x60, value = 0x100)
    }

    fn ack_completed(self, event: EduInterrupt) {
        self.write32(offset = 0x64, value = event.status)
    }
}
```

### `wrela/machine/x86_64/ivshmem.wrela`

Create this module for the QEMU ivshmem-doorbell MSI-X proof path:

```wrela
module machine.x86_64.ivshmem

data IvshmemDoorbellInterrupt {
    vector: U32
}

driver path IvshmemMsixPath {
    registers_base: VirtualAddress

    interrupt receiver -> IvshmemDoorbellInterrupt {
        return IvshmemDoorbellInterrupt(vector = 0)
    }

    asm fn read32(self, offset: U64) -> U32 {
        mov r11, self.registers_base
        add r11, offset
        mov eax, [r11]
        ret
    }

    fn position(self) -> U32 {
        return self.read32(offset = 8)
    }

    fn ack_doorbell(self, event: IvshmemDoorbellInterrupt) {
    }
}

driver path IvshmemDoorbellPeerPath {
    registers_base: VirtualAddress

    asm fn write32(self, offset: U64, value: U32) {
        mov r11, self.registers_base
        add r11, offset
        mov eax, value
        mov [r11], eax
        ret
    }

    fn ring_peer(self, peer_id: U32, vector: U32) {
        let value = (peer_id << 16) | vector
        self.write32(offset = 12, value = value)
    }
}
```

### `wrela/machine/x86_64/pci.wrela`

Create this module for fixed q35 lab-device programming. The BDFs are fixed for the lab devices, but MSI/MSI-X capability offsets are discovered by walking the PCI capability list.

```wrela
module machine.x86_64.pci

class PciConfigPorts {
    asm fn write32(self, address: U32, value: U32) {
        mov dx, 0x0CF8
        mov eax, address
        out dx, eax
        mov dx, 0x0CFC
        mov eax, value
        out dx, eax
        ret
    }

    asm fn read32(self, address: U32) -> U32 {
        mov dx, 0x0CF8
        mov eax, address
        out dx, eax
        mov dx, 0x0CFC
        in eax, dx
        ret
    }
}

class MsixTable {
    base: VirtualAddress

    asm fn write_entry0(self, message_address: U32, message_data: U32) {
        mov r11, self.base
        mov eax, message_address
        mov [r11 + 0], eax
        mov eax, 0
        mov [r11 + 4], eax
        mov eax, message_data
        mov [r11 + 8], eax
        mov eax, 0
        mov [r11 + 12], eax
        ret
    }
}

class Q35PciInterruptConfigurator {
    config: PciConfigPorts
    ivshmem_msix_table: MsixTable

    fn config_address(self, slot: U32, offset: U32) -> U32 {
        return 0x80000000 | (slot << 11) | (offset & 0xFC)
    }

    fn read_config32(self, slot: U32, offset: U32) -> U32 {
        return self.config.read32(address = self.config_address(slot = slot, offset = offset))
    }

    fn write_config32(self, slot: U32, offset: U32, value: U32) {
        self.config.write32(address = self.config_address(slot = slot, offset = offset), value = value)
    }

    fn find_capability(self, slot: U32, capability_id: U32) -> U32 {
        let ptr = self.read_config32(slot = slot, offset = 0x34) & 0xFC
        let remaining = 48
        while remaining != 0 {
            if ptr == 0 {
                return 0
            }
            let header = self.read_config32(slot = slot, offset = ptr)
            if (header & 0xFF) == capability_id {
                return ptr
            }
            ptr = (header >> 8) & 0xFC
            remaining = remaining - 1
        }
        return 0
    }

    fn edu_bar0(self) -> VirtualAddress {
        return self.read_config32(slot = 5, offset = 0x10) & 0xFFFFFFF0
    }

    fn ivshmem_rx_bar0(self) -> VirtualAddress {
        return self.read_config32(slot = 6, offset = 0x10) & 0xFFFFFFF0
    }

    fn ivshmem_rx_bar1(self) -> VirtualAddress {
        return self.read_config32(slot = 6, offset = 0x14) & 0xFFFFFFF0
    }

    fn ivshmem_tx_bar0(self) -> VirtualAddress {
        return self.read_config32(slot = 7, offset = 0x10) & 0xFFFFFFF0
    }

    fn configure_edu_msi_vector41(self) {
        let cap = self.find_capability(slot = 5, capability_id = 0x05)
        let header = self.read_config32(slot = 5, offset = cap)
        let control = (header >> 16) & 0xFFFF
        self.write_config32(slot = 5, offset = cap + 4, value = 0xFEE00000)
        if (control & 0x80) != 0 {
            self.write_config32(slot = 5, offset = cap + 8, value = 0)
            self.write_config32(slot = 5, offset = cap + 12, value = 0x00000041)
        } else {
            self.write_config32(slot = 5, offset = cap + 8, value = 0x00000041)
        }
        self.write_config32(slot = 5, offset = cap + 0, value = header | 0x00010000)
    }

    fn configure_ivshmem_msix_vector42(self) {
        let cap = self.find_capability(slot = 6, capability_id = 0x11)
        let header = self.read_config32(slot = 6, offset = cap)
        self.ivshmem_msix_table.write_entry0(message_address = 0xFEE00000, message_data = 0x42)
        self.write_config32(slot = 6, offset = cap + 0, value = header | 0x80000000)
    }
}
```

The fixed PCI device slots are q35 lab constants for this plan: EDU at slot 5, ivshmem receiver at slot 6, and ivshmem sender at slot 7. BAR base addresses and MSI/MSI-X capability offsets are read from PCI config space at runtime. Production PCI enumeration is deferred to a later plan.

MSI layout rule baked into `configure_edu_msi_vector41`: read Message Control from `(cap + 2)` via the high 16 bits of `read_config32(cap)`. If bit 7 is set, the device uses 64-bit MSI and message data is at `cap + 12`; otherwise message data is at `cap + 8`. Always preserve the original capability header and set MSI Enable by OR-ing `0x00010000`.

MSI-X layout rule baked into `configure_ivshmem_msix_vector42`: table entry 0 lives in the BAR1 table wrapper for this lab device, and MSI-X Enable is bit 31 of the dword at the capability header. Preserve the original capability header and set enable by OR-ing `0x80000000`.

### `examples/hello/program.wrela`

Update the existing hello executor:

```wrela
module examples.hello.program

use { ExecutorMemory } from machine.x86_64.executor_memory
use { EduInterrupt, EduMsiPath } from machine.x86_64.edu
use { ApicInterruptController, IoApic, LocalApic } from machine.x86_64.interrupts
use { IvshmemDoorbellInterrupt, IvshmemDoorbellPeerPath, IvshmemMsixPath } from machine.x86_64.ivshmem
use { MsixTable, PciConfigPorts, Q35PciInterruptConfigurator } from machine.x86_64.pci
use { SerialConsolePath, SerialPathInterrupt } from machine.x86_64.serial

executor HelloWorld {
    memory: ExecutorMemory
    interrupts: ApicInterruptController
    pci_interrupts: Q35PciInterruptConfigurator
    serial_path: SerialConsolePath
    edu_path: EduMsiPath
    ivshmem_rx: IvshmemMsixPath
    ivshmem_tx: IvshmemDoorbellPeerPath

    on serial_path.interrupt(event: SerialPathInterrupt) {
        self.serial_path.write(self.memory.static_bytes("serial interrupt: "))
        self.serial_path.write_byte(value = event.byte)
        self.serial_path.write(self.memory.static_bytes("\n"))
        self.serial_path.ack_receive(event = event)
    }

    on edu_path.interrupt(event: EduInterrupt) {
        self.serial_path.write(self.memory.static_bytes("msi interrupt\n"))
        self.edu_path.ack_completed(event = event)
    }

    on ivshmem_rx.interrupt(event: IvshmemDoorbellInterrupt) {
        self.serial_path.write(self.memory.static_bytes("msix interrupt\n"))
        self.ivshmem_rx.ack_doorbell(event = event)
    }

    start fn run(self) -> never {
        self.serial_path.write(self.memory.static_bytes("hello from wrela\n"))
        self.interrupts.initialize_for_com1_receive()
        self.pci_interrupts.configure_edu_msi_vector41()
        self.pci_interrupts.configure_ivshmem_msix_vector42()
        self.serial_path.enable_receive_interrupts()
        self.interrupts.enable_cpu_interrupts()
        self.edu_path.raise_test_interrupt()
        self.ivshmem_tx.ring_peer(peer_id = self.ivshmem_rx.position(), vector = 0)
        self.memory.halt_forever()
    }
}
```

### `examples/hello/main.wrela`

Update only the owned phase's serial construction:

```wrela
phase owned_hardware(hardware: OwnedHardware) -> never {
    let com1 = hardware.io_ports.claim_com1()
    let registers = SerialWriterRegisters(port_base = com1.port_base)
    let serial_driver = SerialDriver(
        registers = registers,
        memory = DriverMemory(region = hardware.memory.arena)
        ).initialize()
    let serial_path = SerialConsolePath(owner = hardware.vcpu0, registers = serial_driver.registers)
    let pci_config = PciConfigPorts()
    let pci_probe = Q35PciInterruptConfigurator(
        config = pci_config,
        ivshmem_msix_table = MsixTable(base = 0)
    )
    let pci_interrupts = Q35PciInterruptConfigurator(
        config = pci_config,
        ivshmem_msix_table = MsixTable(base = pci_probe.ivshmem_rx_bar1())
    )
    let hello = HelloWorld(
        memory = hardware.vcpu0.memory,
        interrupts = ApicInterruptController(
            local_apic = LocalApic(base = 0xFEE00000),
            io_apic = IoApic(base = 0xFEC00000)
        ),
        pci_interrupts = pci_interrupts,
        serial_path = serial_path,
        edu_path = EduMsiPath(mmio_base = pci_probe.edu_bar0()),
        ivshmem_rx = IvshmemMsixPath(registers_base = pci_probe.ivshmem_rx_bar0()),
        ivshmem_tx = IvshmemDoorbellPeerPath(registers_base = pci_probe.ivshmem_tx_bar0())
    )
    hello.run()
}
```

`pci_probe` is a temporary helper used only to read BAR addresses before the MSI-X table wrapper can be constructed with BAR1. The executor receives `pci_interrupts`, which has the real MSI-X table base. `Q35PciInterruptConfigurator` and `MsixTable` are ordinary non-unique classes, so constructing the probe and the real configurator in the owned phase is permitted.

Expected e2e serial output after QEMU receives input byte `!`:

```text
hello from wrela
serial interrupt: !
msi interrupt
msix interrupt
```

---

## 5. Tasks

### Task 0: Add Interrupt Test Helpers

**Files:** `compiler/sem/interrupt_testutil_test.go`, `compiler/ir/interrupt_testutil_test.go`, `compiler/codegen/interrupt_testutil_test.go`, `tests/e2e/hello_qemu_test.go`

**You may need to know:** `compiler/sem/testutil_test.go` already has `parseModulesForTest`, `mustBuildIndex`, `mustCheck`, and `hasCode`. `compiler/integration_test.go` has helper implementations for symbol byte slicing that can be copied into the codegen package.

- [ ] **Step 1:** Add semantic helpers.

```go
package sem

import (
    "testing"

    "github.com/ryanwible/wrela3/compiler/diag"
)

func buildIndexForTest(t *testing.T, sourceText string) (*Index, []diag.Diagnostic) {
    t.Helper()
    modules := parseModulesForTest(t, sourceText)
    index, ds := BuildIndex(modules)
    return index, filterMissingImageDiagnostic(ds)
}

func checkModuleForTest(t *testing.T, sourceText string) (*CheckedProgram, []diag.Diagnostic) {
    t.Helper()
    modules := parseModulesForTest(t, sourceText)
    index, ds := BuildIndex(modules)
    ds = filterMissingImageDiagnostic(ds)
    if len(ds) != 0 {
        return nil, ds
    }
    return Check(index, modules)
}

func filterMissingImageDiagnostic(ds []diag.Diagnostic) []diag.Diagnostic {
    out := ds[:0]
    for _, d := range ds {
        if d.Code == diag.SEM0004 {
            continue
        }
        out = append(out, d)
    }
    return out
}
```

- [ ] **Step 2:** Add IR helper.

```go
package ir

import (
    "testing"

    "github.com/ryanwible/wrela3/compiler/diag"
    "github.com/ryanwible/wrela3/compiler/parse"
    "github.com/ryanwible/wrela3/compiler/sem"
    "github.com/ryanwible/wrela3/compiler/source"
)

func checkedProgramForTest(t *testing.T, sourceText string) *sem.CheckedProgram {
    t.Helper()
    file := source.NewFile(1, "interrupt_test.wrela", sourceText)
    modules, ds := parse.ParseGraph(source.Graph{Files: []*source.File{file}})
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

func filterMissingImageDiagnostic(ds []diag.Diagnostic) []diag.Diagnostic {
    out := ds[:0]
    for _, d := range ds {
        if d.Code == diag.SEM0004 {
            continue
        }
        out = append(out, d)
    }
    return out
}
```

- [ ] **Step 3:** Add codegen helpers.

```go
package codegen

import (
    "sort"
    "testing"
)

func symbolBytes(t *testing.T, image *Image, symbol string) []byte {
    t.Helper()
    rva, ok := image.Symbols[symbol]
    if !ok {
        t.Fatalf("missing symbol %s", symbol)
    }
    text := image.Sections[0]
    start := int(rva - text.RVA)
    end := len(text.Data)
    var starts []int
    for _, other := range image.Symbols {
        if other > rva {
            starts = append(starts, int(other-text.RVA))
        }
    }
    if len(starts) != 0 {
        sort.Ints(starts)
        end = starts[0]
    }
    if start < 0 || start > len(text.Data) || end < start || end > len(text.Data) {
        t.Fatalf("invalid symbol span for %s: %d..%d in %d bytes", symbol, start, end, len(text.Data))
    }
    return text.Data[start:end]
}

func containsBytes(haystack, needle []byte) bool {
    if len(needle) == 0 {
        return true
    }
    for i := 0; i+len(needle) <= len(haystack); i++ {
        if string(haystack[i:i+len(needle)]) == string(needle) {
            return true
        }
    }
    return false
}
```

- [ ] **Step 4:** Add this package-level default in `tests/e2e/hello_qemu_test.go`.

```go
var ivshmemServer = "ivshmem-server"
```

- [ ] **Step 5:** Run helper package tests.

```sh
go test ./compiler/sem ./compiler/ir ./compiler/codegen ./tests/e2e -run '^$'
```

Expected: `PASS`.

- [ ] **Step 6:** Commit with `git commit -m "test: add interrupt test helpers -Codex Automated"`.

**Acceptance Criteria:** Every helper referenced by later interrupt tasks exists before those tasks begin.

---

### Task 1: Reserve Interrupt Syntax Keywords

**Files:** `compiler/lex/token.go`, `compiler/lex/lexer.go`, `compiler/lex/lexer_test.go`

- [ ] **Step 1:** Check for source collisions.

```sh
rg -nw 'interrupt|on|using|interrupts' wrela examples tests --glob '*.wrela'
```

Expected before implementation: no use of `interrupt`, `on`, or `using` as identifiers. Existing `interrupts` field names are allowed.

- [ ] **Step 2:** Add lexer test.

```go
func TestInterruptEventKeywords(t *testing.T) {
    toks, ds := lex.All("interrupt receiver on using interruptfoo receiverfoo oncall")
    if len(ds) != 0 {
        t.Fatalf("diagnostics = %#v", ds)
    }
    got := []lex.Kind{toks[0].Kind, toks[1].Kind, toks[2].Kind, toks[3].Kind, toks[4].Kind, toks[5].Kind, toks[6].Kind}
    want := []lex.Kind{lex.KeywordInterrupt, lex.KeywordReceiver, lex.KeywordOn, lex.Identifier, lex.Identifier, lex.Identifier, lex.Identifier}
    if !reflect.DeepEqual(got, want) {
        t.Fatalf("kinds = %#v, want %#v", got, want)
    }
}
```

- [ ] **Step 3:** Run `go test ./compiler/lex`; expect compile failure for missing token constants.
- [ ] **Step 4:** Add `KeywordInterrupt`, `KeywordReceiver`, and `KeywordOn`; do not add `KeywordUsing`.
- [ ] **Step 5:** Run `go test ./compiler/lex`; expect `PASS`.
- [ ] **Step 6:** Commit with `git commit -m "feat: reserve interrupt event keywords -Codex Automated"`.

**Acceptance Criteria:** `interrupt`, `receiver`, and `on` are keywords; `using` remains an identifier.

---

### Task 1A: Change Named Call And Constructor Arguments To Equals

**Files:** `compiler/parse/parser.go`, `compiler/parse/expr_test.go`, `compiler/parse/parser_test.go`, `compiler/ast/ast.go`, `compiler/ast/ast_test.go`, `examples/**/*.wrela`, `wrela/**/*.wrela`, `tests/**/*.wrela`

This is a standalone prerequisite migration. Land it as its own commit before any interrupt source modules are edited; do not combine it with Task 11 or Task 12.

- [ ] **Step 1:** Add parser tests for `=` named arguments and old `:` rejection.

```go
func TestParseNamedArgsUseEquals(t *testing.T) {
    p := newParser("test", "Device(x = 1)")
    expr, ds := p.parseExpr(0)
    if len(ds) != 0 {
        t.Fatalf("diagnostics = %#v", ds)
    }
    con := expr.(*ast.ConstructorExpr)
    if len(con.Args) != 1 || con.Args[0].Name != "x" {
        t.Fatalf("constructor args = %#v", con.Args)
    }

    p = newParser("test", "host.run(payload = Bytes)")
    expr, ds = p.parseExpr(0)
    if len(ds) != 0 {
        t.Fatalf("diagnostics = %#v", ds)
    }
    call := expr.(*ast.CallExpr)
    if len(call.Args) != 1 || call.Args[0].Name != "payload" {
        t.Fatalf("call args = %#v", call.Args)
    }
}

func TestParseNamedArgsRejectColon(t *testing.T) {
    for _, src := range []string{"Device(x: 1)", "host.run(payload: Bytes)"} {
        p := newParser("test", src)
        _, ds := p.parseExpr(0)
        if len(ds) == 0 {
            t.Fatalf("expected diagnostic for %s", src)
        }
    }
}
```

- [ ] **Step 2:** Run `go test ./compiler/parse -run NamedArgs`; expect failure.
- [ ] **Step 3:** Change `parseNamedArgs` to detect `identifier = expr` instead of `identifier : expr`.

```go
if isNameToken(p.peek()) && p.peekN(1).Kind == lex.Equal {
    nameTok := p.next()
    name = nameTok.Text
    p.next()
    start = nameTok.Start
}
```

- [ ] **Step 4:** Keep all declaration/type-annotation parsing on `lex.Colon`. Do not change field declarations, function parameters, phase parameters, or `on path.interrupt(event: Type)`.
- [ ] **Step 5:** Update AST debug rendering for named args so debugging output follows source syntax.

```go
func TestDebugExprNamedArgsUseEquals(t *testing.T) {
    expr := &CallExpr{
        Receiver: &NameExpr{Name: "host"},
        Method:   "run",
        Args: []NamedArg{{
            Name:  "payload",
            Value: &NameExpr{Name: "Bytes"},
        }},
    }
    if got, want := DebugExpr(expr), "host.run(payload = Bytes)"; got != want {
        t.Fatalf("DebugExpr = %q, want %q", got, want)
    }
}
```

Change the `NamedArg` branch in `compiler/ast/ast.go` from `name+": "` to `name+" = "`.

- [ ] **Step 6:** Update every Wrela source file and fixture call/constructor named argument from `name: value` to `name = value`.
- [ ] **Step 7:** Run this sweep:

```sh
rg -n '[A-Za-z_][A-Za-z0-9_]*\([^)\n]*[A-Za-z_][A-Za-z0-9_]*:' wrela examples tests --glob '*.wrela'
```

Expected: no results except typed parameters in declarations, which should not appear inside call/constructor argument lists.

- [ ] **Step 8:** Run `go test ./compiler/ast ./compiler/parse ./compiler/sem`; expect `PASS`.
- [ ] **Step 9:** Commit with `git commit -m "feat: use equals for named call arguments -Codex Automated"`.

**Acceptance Criteria:** Named call and constructor arguments use `=`, and `:` remains reserved for type-bearing syntax.

---

### Task 2: Add AST Nodes For Path Events And Executor On Handlers

**Files:** `compiler/ast/ast.go`, `compiler/ast/ast_test.go`

- [ ] **Step 1:** Add AST contract test.

```go
func TestInterruptEventASTContracts(t *testing.T) {
    path := &DriverPathDecl{
        Name: "SerialConsolePath",
        InterruptEvents: []InterruptEventDecl{
            {EventType: "SerialPathInterrupt"},
        },
    }
    exec := &ExecutorDecl{
        Name: "HelloWorld",
        OnHandlers: []OnHandlerDecl{
            {PathField: "serial_path", ParamName: "event", ParamType: "SerialPathInterrupt"},
        },
    }
    if path.InterruptEvents[0].EventType != "SerialPathInterrupt" {
        t.Fatalf("interrupt event not stored")
    }
    if exec.OnHandlers[0].PathField != "serial_path" || exec.OnHandlers[0].ParamType != "SerialPathInterrupt" {
        t.Fatalf("on handler not stored")
    }
}
```

- [ ] **Step 2:** Run `go test ./compiler/ast`; expect compile failure.
- [ ] **Step 3:** Add `InterruptEventDecl`, `OnHandlerDecl`, extend `DriverPathDecl`, extend `ExecutorDecl`.
- [ ] **Step 4:** Run `go test ./compiler/ast`; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: add interrupt event AST nodes -Codex Automated"`.

**Acceptance Criteria:** AST distinguishes interrupt events and `on` handlers from normal methods.

---

### Task 3: Parse Driver Path Interrupt Events

**Files:** `compiler/parse/parser.go`, `compiler/parse/parser_test.go`

- [ ] **Step 1:** Add parser test.

```go
func TestParseDriverPathInterruptEvent(t *testing.T) {
    mod, ds := parseModuleForTest(t, `
module test.interrupt_event
data SerialPathInterrupt { byte: U8 }
driver path SerialConsolePath {
    interrupt receiver -> SerialPathInterrupt {
        return SerialPathInterrupt(byte = 0)
    }
}`)
    if len(ds) != 0 {
        t.Fatalf("diagnostics = %#v", ds)
    }
    path := mod.Decls[1].(*ast.DriverPathDecl)
    if len(path.InterruptEvents) != 1 {
        t.Fatalf("events = %d, want 1", len(path.InterruptEvents))
    }
    ev := path.InterruptEvents[0]
    if ev.EventType != "SerialPathInterrupt" || len(ev.Body) != 1 {
        t.Fatalf("event = %#v", ev)
    }
}
```

- [ ] **Step 2:** Add parser rejection test.

```go
func TestInterruptEventRejectedOutsideDriverPath(t *testing.T) {
    cases := []string{
        "class C { interrupt receiver -> Event { return Event() } }",
        "driver D { interrupt receiver -> Event { return Event() } }",
        "executor E { interrupt receiver -> Event { return Event() } }",
    }
    for _, body := range cases {
        _, ds := parseModuleForTest(t, "module test.bad_event\ndata Event {}\n"+body)
        if len(ds) == 0 {
            t.Fatalf("expected parse diagnostic for %s", body)
        }
    }
}
```

- [ ] **Step 3:** Run `go test ./compiler/parse -run InterruptEvent`; expect failure.
- [ ] **Step 4:** Thread composite context through the shared member parser.

```go
type compositeKind int

const (
    compositeClass compositeKind = iota
    compositeDriver
    compositeDriverPath
    compositeExecutor
)
```

Replace the existing `parseCompositeMembers()` signature with this exact shape:

```go
func (p *Parser) parseCompositeMembers(kind compositeKind) ([]ast.Field, []ast.MethodDecl, []ast.InterruptEventDecl, []ast.OnHandlerDecl, source.Span, []diag.Diagnostic)
```

Update the current call sites in `parseClassDecl`, `parseDriverDecl`, `parseDriverPathDecl`, and `parseExecutorDecl`:

```go
fields, methods, _, _, _, ds := p.parseCompositeMembers(compositeClass)
fields, methods, _, _, _, ds := p.parseCompositeMembers(compositeDriver)
fields, methods, interruptEvents, _, _, ds := p.parseCompositeMembers(compositeDriverPath)
fields, methods, _, _, _, ds := p.parseCompositeMembers(compositeExecutor)
```

At this point `parseDriverPathDecl` must assign `InterruptEvents: interruptEvents` in the returned `ast.DriverPathDecl`. `parseExecutorDecl` keeps discarding the fourth return value until Task 4 so Task 3 compiles cleanly.

- [ ] **Step 5:** Parse `interrupt receiver -> Type { ... }` only in driver path bodies.

```go
case lex.KeywordInterrupt:
    if kind != compositeDriverPath {
        return nil, nil, nil, nil, source.Span{}, p.err(p.peek(), diag.PAR0001, "unexpected token in declaration body")
    }
    event, ds := p.parseInterruptEventDecl()
    if len(ds) != 0 {
        return nil, nil, nil, nil, source.Span{}, ds
    }
    interruptEvents = append(interruptEvents, event)
    prevEnd = event.Span().End
```

Add `parseInterruptEventDecl` with this exact contract:

```go
func (p *Parser) parseInterruptEventDecl() (ast.InterruptEventDecl, []diag.Diagnostic) {
    start := p.next() // interrupt
    if _, ds := p.consume(lex.KeywordReceiver); len(ds) != 0 {
        return ast.InterruptEventDecl{}, ds
    }
    if _, ds := p.consume(lex.Arrow); len(ds) != 0 {
        return ast.InterruptEventDecl{}, ds
    }
    eventType, ds := p.parseTypeName()
    if len(ds) != 0 {
        return ast.InterruptEventDecl{}, ds
    }
    body, ds := p.parseBlockStmts()
    if len(ds) != 0 {
        return ast.InterruptEventDecl{}, ds
    }
    return ast.InterruptEventDecl{
        EventType: eventType,
        Body:      body,
        SpanV:     p.span(start.Start, p.previous().End),
    }, nil
}
```

- [ ] **Step 6:** Run `go test ./compiler/parse`; expect `PASS`.
- [ ] **Step 7:** Commit with `git commit -m "feat: parse driver path interrupt events -Codex Automated"`.

**Acceptance Criteria:** Driver paths can declare inline typed interrupt event bodies.

---

### Task 4: Parse Executor On Handlers

**Files:** `compiler/parse/parser.go`, `compiler/parse/parser_test.go`

- [ ] **Step 1:** Add parser test.

```go
func TestParseExecutorOnHandler(t *testing.T) {
    mod, ds := parseModuleForTest(t, `
module test.on_handler
executor HelloWorld {
    serial_path: SerialConsolePath
    on serial_path.interrupt(event: SerialPathInterrupt) {
        self.serial_path.ack_receive(event = event)
    }
}`)
    if len(ds) != 0 {
        t.Fatalf("diagnostics = %#v", ds)
    }
    exec := mod.Decls[0].(*ast.ExecutorDecl)
    if len(exec.OnHandlers) != 1 {
        t.Fatalf("on handlers = %d, want 1", len(exec.OnHandlers))
    }
    got := exec.OnHandlers[0]
    if got.PathField != "serial_path" || got.ParamName != "event" || got.ParamType != "SerialPathInterrupt" {
        t.Fatalf("on handler = %#v", got)
    }
}
```

- [ ] **Step 2:** Add missing typed-parameter rejection test.

```go
func TestOnHandlerRejectsMissingParamType(t *testing.T) {
    _, ds := parseModuleForTest(t, `
module test.bad_on
executor HelloWorld {
    serial_path: SerialConsolePath
    on serial_path.interrupt(event) {
    }
}`)
    if len(ds) == 0 {
        t.Fatalf("expected parse diagnostic")
    }
}
```

- [ ] **Step 3:** Add non-`.interrupt` selector rejection test. This is a parse-level rejection, not a semantic one, because `on` handlers only accept the fixed `path_field.interrupt` selector.

```go
func TestOnHandlerRejectsNonInterruptSelector(t *testing.T) {
    _, ds := parseModuleForTest(t, `
module test.bad_on_selector
executor HelloWorld {
    serial_path: SerialConsolePath
    on serial_path.receive(event: SerialPathInterrupt) {
    }
}`)
    if len(ds) == 0 {
        t.Fatalf("expected parse diagnostic")
    }
}
```

- [ ] **Step 4:** Add non-executor placement rejection test.

```go
func TestOnHandlerRejectedOutsideExecutor(t *testing.T) {
    _, ds := parseModuleForTest(t, `
module test.bad_on_placement
class C {
    on serial_path.interrupt(event: SerialPathInterrupt) {
    }
}`)
    if len(ds) == 0 {
        t.Fatalf("expected parse diagnostic")
    }
}
```

- [ ] **Step 5:** Run `go test ./compiler/parse -run OnHandler`; expect failure.
- [ ] **Step 6:** Parse `on field.interrupt(param: Type) { ... }` only in executor bodies by extending the `parseCompositeMembers(kind)` switch from Task 3.

```go
case lex.KeywordOn:
    if kind != compositeExecutor {
        return nil, nil, nil, nil, source.Span{}, p.err(p.peek(), diag.PAR0001, "unexpected token in declaration body")
    }
    handler, ds := p.parseOnHandlerDecl()
    if len(ds) != 0 {
        return nil, nil, nil, nil, source.Span{}, ds
    }
    onHandlers = append(onHandlers, handler)
    prevEnd = handler.Span().End
```

At this point `parseExecutorDecl` must assign `OnHandlers: onHandlers` in the returned `ast.ExecutorDecl`.

```go
fields, methods, _, onHandlers, _, ds := p.parseCompositeMembers(compositeExecutor)
if len(ds) != 0 {
    return nil, ds
}
return &ast.ExecutorDecl{
    Name:       name.Text,
    Fields:     fields,
    Methods:    methods,
    OnHandlers: onHandlers,
    SpanV:      p.span(start.Start, p.previous().End),
}, nil
```

- [ ] **Step 7:** Add `parseOnHandlerDecl` with this exact contract.

```go
func (p *Parser) parseOnHandlerDecl() (ast.OnHandlerDecl, []diag.Diagnostic) {
    start := p.next() // on
    pathField, ds := p.expectIdentifier("expected interrupt path field name")
    if len(ds) != 0 {
        return ast.OnHandlerDecl{}, ds
    }
    if _, ds := p.consume(lex.Dot); len(ds) != 0 {
        return ast.OnHandlerDecl{}, ds
    }
    if _, ds := p.consume(lex.KeywordInterrupt); len(ds) != 0 {
        return ast.OnHandlerDecl{}, ds
    }
    if _, ds := p.consume(lex.LParen); len(ds) != 0 {
        return ast.OnHandlerDecl{}, ds
    }
    paramName, ds := p.expectIdentifier("expected interrupt event parameter name")
    if len(ds) != 0 {
        return ast.OnHandlerDecl{}, ds
    }
    if _, ds := p.consume(lex.Colon); len(ds) != 0 {
        return ast.OnHandlerDecl{}, ds
    }
    paramType, ds := p.parseTypeName()
    if len(ds) != 0 {
        return ast.OnHandlerDecl{}, ds
    }
    if _, ds := p.consume(lex.RParen); len(ds) != 0 {
        return ast.OnHandlerDecl{}, ds
    }
    body, ds := p.parseBlockStmts()
    if len(ds) != 0 {
        return ast.OnHandlerDecl{}, ds
    }
    return ast.OnHandlerDecl{
        PathField: pathField.Text,
        ParamName: paramName.Text,
        ParamType: paramType,
        Body:      body,
        SpanV:     p.span(start.Start, p.previous().End),
    }, nil
}
```

- [ ] **Step 8:** Run `go test ./compiler/parse`; expect `PASS`.
- [ ] **Step 9:** Commit with `git commit -m "feat: parse executor on handlers -Codex Automated"`.

**Acceptance Criteria:** Executors declare event handlers without explicit bind calls.

---

### Task 5: Add Interrupt Diagnostic Codes And Indexing

**Files:** `compiler/diag/codes.go`, `compiler/sem/symbols.go`, `compiler/sem/symbols_test.go`

**You may need to know:** `compiler/sem/symbols.go` owns `Index`, `BuildIndex`, `buildFields`, and `buildMethods`. Add interrupt maps next to `ByModule`/`ByImport`, then populate them in the existing decl switch that already handles `DriverPathDecl` and `ExecutorDecl`.

- [ ] **Step 1:** Add constants `SEM0014` through `SEM0020` exactly as Section 2 defines them.
- [ ] **Step 2:** Add index test for one path event and one executor on handler.

```go
func TestInterruptEventsAndOnHandlersIndexed(t *testing.T) {
    index, ds := buildIndexForTest(t, `
module test.interrupt_index
data SerialPathInterrupt { byte: U8 }
driver path SerialConsolePath {
    interrupt receiver -> SerialPathInterrupt {
        return SerialPathInterrupt(byte = 0)
    }
}
executor HelloWorld {
    serial_path: SerialConsolePath
    on serial_path.interrupt(event: SerialPathInterrupt) {
    }
}`)
    if len(ds) != 0 {
        t.Fatalf("diagnostics = %#v", ds)
    }
    if index.InterruptEvent("test.interrupt_index", "SerialConsolePath") == nil {
        t.Fatalf("missing interrupt event")
    }
    if index.OnHandler("test.interrupt_index", "HelloWorld", "serial_path") == nil {
        t.Fatalf("missing on handler")
    }
}
```

- [ ] **Step 3:** Run `go test ./compiler/sem -run InterruptEventsAndOnHandlersIndexed`; expect failure.
- [ ] **Step 4:** Extend the semantic index with event and handler maps using this shape.

```go
type Index struct {
    Modules  map[string]*ast.Module
    ByModule map[string]map[string]*Type
    ByImport map[string]map[string]*Type
    Images   []*ast.ImageDecl

    InterruptEvents map[string]map[string]*ast.InterruptEventDecl
    OnHandlers      map[string]map[string]map[string]*ast.OnHandlerDecl

    primitives map[string]*Type
}

func NewIndex() *Index {
    return &Index{
        Modules:         map[string]*ast.Module{},
        ByModule:        map[string]map[string]*Type{},
        ByImport:        map[string]map[string]*Type{},
        InterruptEvents: map[string]map[string]*ast.InterruptEventDecl{},
        OnHandlers:      map[string]map[string]map[string]*ast.OnHandlerDecl{},
        primitives:      map[string]*Type{},
    }
}

func (idx *Index) InterruptEvent(moduleName, pathType string) *ast.InterruptEventDecl {
    if idx == nil || idx.InterruptEvents[moduleName] == nil {
        return nil
    }
    return idx.InterruptEvents[moduleName][pathType]
}

func (idx *Index) OnHandler(moduleName, executorType, pathField string) *ast.OnHandlerDecl {
    if idx == nil || idx.OnHandlers[moduleName] == nil || idx.OnHandlers[moduleName][executorType] == nil {
        return nil
    }
    return idx.OnHandlers[moduleName][executorType][pathField]
}
```

- [ ] **Step 5:** Populate event and handler maps in `BuildIndex` inside the existing second-pass decl switch.

```go
case *ast.DriverPathDecl:
    typ.Fields = buildFields(idx, mod.Name, d.Fields)
    typ.Methods = buildMethods(idx, mod.Name, d.Methods)
    if idx.InterruptEvents[mod.Name] == nil {
        idx.InterruptEvents[mod.Name] = map[string]*ast.InterruptEventDecl{}
    }
    for i := range d.InterruptEvents {
        ev := &d.InterruptEvents[i]
        if idx.InterruptEvents[mod.Name][d.Name] != nil {
            diagOut = append(diagOut, diag.Diagnostic{Phase: "sem", Code: diag.SEM0014, Severity: diag.Error, Start: ev.Span().Start, End: ev.Span().End, Message: "duplicate interrupt receiver"})
            continue
        }
        idx.InterruptEvents[mod.Name][d.Name] = ev
    }
case *ast.ExecutorDecl:
    typ.Fields = buildFields(idx, mod.Name, d.Fields)
    typ.Methods = buildMethods(idx, mod.Name, d.Methods)
    if idx.OnHandlers[mod.Name] == nil {
        idx.OnHandlers[mod.Name] = map[string]map[string]*ast.OnHandlerDecl{}
    }
    byPath := map[string]*ast.OnHandlerDecl{}
    for i := range d.OnHandlers {
        h := &d.OnHandlers[i]
        if byPath[h.PathField] != nil {
            diagOut = append(diagOut, diag.Diagnostic{Phase: "sem", Code: diag.SEM0014, Severity: diag.Error, Start: h.Span().Start, End: h.Span().End, Message: "duplicate on handler " + h.PathField + ".interrupt"})
            continue
        }
        byPath[h.PathField] = h
    }
    idx.OnHandlers[mod.Name][d.Name] = byPath
```
- [ ] **Step 6:** Run `go test ./compiler/sem`; expect `PASS`.
- [ ] **Step 7:** Commit with `git commit -m "feat: index interrupt events and handlers -Codex Automated"`.

**Acceptance Criteria:** Semantic index exposes events and on handlers by stable names.

---

### Task 6: Check Event Body Contracts

**Files:** `compiler/sem/check.go`, `compiler/sem/types_test.go`

**You may need to know:** `checkDeclBodiesAndConstructors` already walks declaration bodies. `checkMethods` shows how to bind `self` and call `checkStmtList`. Reuse `checkStmtList`; do not create a second expression checker.

- [ ] **Step 1:** Add semantic tests.

```go
func TestInterruptEventBodyContracts(t *testing.T) {
    cases := []struct {
        name string
        src string
        want string
    }{
        {
            name: "valid",
            src: `data Event { byte: U8 }
driver path Path {
    interrupt receiver -> Event { return Event(byte = 0) }
}`,
            want: "",
        },
        {
            name: "unknown event type",
            src: `driver path Path {
    interrupt receiver -> Missing { return Missing() }
}`,
            want: diag.SEM0002,
        },
        {
            name: "wrong return type",
            src: `data Event { byte: U8 }
data Other { byte: U8 }
driver path Path {
    interrupt receiver -> Event { return Other(byte = 0) }
}`,
            want: diag.SEM0015,
        },
        {
            name: "missing return",
            src: `data Event { byte: U8 }
driver path Path {
    interrupt receiver -> Event { }
}`,
            want: diag.SEM0015,
        },
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            _, diags := checkModuleForTest(t, "module test.event_contract\n"+tc.src)
            if tc.want == "" && len(diags) != 0 {
                t.Fatalf("unexpected diagnostics: %#v", diags)
            }
            if tc.want != "" && !hasCode(diags, tc.want) {
                t.Fatalf("expected %s, got %#v", tc.want, diags)
            }
        })
    }
}
```

- [ ] **Step 2:** Run `go test ./compiler/sem -run InterruptEventBodyContracts`; expect failure.
- [ ] **Step 3:** Add an interrupt-event pass after normal method body checks.

```go
func (c *checker) checkDeclBodiesAndConstructors() {
    for _, mod := range c.modules {
        for _, decl := range mod.Decls {
            switch d := decl.(type) {
            case *ast.ImageDecl:
                c.checkImageDecl(mod.Name, d)
            case *ast.ClassDecl:
                typ := c.index.resolveInScope(mod.Name, d.Name)
                c.checkMethods(mod.Name, typ, d.Methods)
            case *ast.DriverDecl:
                typ := c.index.resolveInScope(mod.Name, d.Name)
                c.checkMethods(mod.Name, typ, d.Methods)
            case *ast.DriverPathDecl:
                typ := c.index.resolveInScope(mod.Name, d.Name)
                c.checkMethods(mod.Name, typ, d.Methods)
                c.checkInterruptEvents(mod.Name, typ, d.InterruptEvents)
            case *ast.ExecutorDecl:
                typ := c.index.resolveInScope(mod.Name, d.Name)
                c.checkMethods(mod.Name, typ, d.Methods)
            }
        }
    }
}
```

- [ ] **Step 4:** Implement `checkInterruptEvents` by binding `self` to the driver path type and using the declared event type as the expected return type.

```go
func (c *checker) checkInterruptEvents(moduleName string, pathType *Type, events []ast.InterruptEventDecl) {
    if pathType == nil {
        return
    }
    for i := range events {
        event := &events[i]
        eventType := c.resolveType(moduleName, event.EventType)
        if eventType == nil {
            c.error(event.Span(), diag.SEM0002, "unknown type "+event.EventType)
            continue
        }
        scope := NewScope(nil)
        scope.Define("self", pathType)
        prevType := c.currentType
        prevPhase := c.currentPhase
        c.currentType = pathType
        c.currentPhase = "interrupt receiver"
        terminates := c.checkStmtList(moduleName, event.Body, scope, eventType, ContextNormalMethod)
        c.currentType = prevType
        c.currentPhase = prevPhase
        if !terminates {
            c.error(event.Span(), diag.SEM0015, "interrupt receiver must return "+eventType.Name)
        }
    }
}
```

- [ ] **Step 5:** Ensure wrong return types in event bodies report `SEM0015`, not generic `CG0001`, by adding an interrupt-specific return context.

```go
type ContextKind int

const (
    ContextNormalMethod ContextKind = iota
    ContextImagePhaseDirect
    ContextOwnershipTransferAuthorityMethod
    ContextInterruptEvent
)
```

Call `checkStmtList(..., ContextInterruptEvent)` from `checkInterruptEvents`. In `checkStmt`, change the `ReturnStmt` type mismatch branch for this context:

```go
got := c.typeExpr(moduleName, s.Value, scope, ctx)
if ctx == ContextInterruptEvent && !typesCompatible(expectedReturn, got) {
    c.error(s.Value.Span(), diag.SEM0015, fmt.Sprintf("interrupt event must return %s", expectedReturn.Name))
    return true
}
c.requireType(got, expectedReturn, s.Value.Span())
```
- [ ] **Step 6:** Run `go test ./compiler/sem`; expect `PASS`.
- [ ] **Step 7:** Commit with `git commit -m "feat: check interrupt event bodies -Codex Automated"`.

**Acceptance Criteria:** Every interrupt event explicitly constructs the typed event value.

---

### Task 7: Check Executor On Handler Contracts And Completeness

**Files:** `compiler/sem/check.go`, `compiler/sem/path_graph_test.go`, `compiler/sem/types_test.go`

**You may need to know:** Direct executor fields are already available as `typ.Fields`. Driver-path ownership graph checks live in `checkExecutorWiring`; handler completeness is type-level and should run before runtime support checks.

- [ ] **Step 1:** Add semantic completeness tests.

```go
func TestExecutorMustHandleEveryOwnedPathInterruptEvent(t *testing.T) {
    src := `
module test.complete_handlers
data Event { byte: U8 }
driver path Path {
    interrupt receiver -> Event { return Event(byte = 0) }
}
executor Ex {
    path: Path
}`
    _, diags := checkModuleForTest(t, src)
    if !hasCode(diags, diag.SEM0017) {
        t.Fatalf("expected SEM0017, got %#v", diags)
    }
}

func TestExecutorCompleteInterruptHandlersPass(t *testing.T) {
    src := `
module test.complete_handlers_ok
data Event { byte: U8 }
driver path Path {
    interrupt receiver -> Event { return Event(byte = 0) }
}
executor Ex {
    path: Path
    on path.interrupt(event: Event) { }
}`
    _, diags := checkModuleForTest(t, src)
    if len(diags) != 0 {
        t.Fatalf("unexpected diagnostics: %#v", diags)
    }
}
```

- [ ] **Step 2:** Add invalid-reference tests.

```go
func TestOnHandlerMustReferenceOwnedPathEvent(t *testing.T) {
    cases := []struct {
        name string
        body string
    }{
        {"unknown field", "on missing.interrupt(event: Event) { }"},
        {"non path field", "on memory.interrupt(event: Event) { }"},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            src := `
module test.bad_on_ref
data Event { byte: U8 }
class Memory {}
driver path Path {
    interrupt receiver -> Event { return Event(byte = 0) }
}
executor Ex {
    path: Path
    memory: Memory
    ` + tc.body + `
}`
            _, diags := checkModuleForTest(t, src)
            if !hasCode(diags, diag.SEM0018) {
                t.Fatalf("expected SEM0018, got %#v", diags)
            }
        })
    }
}
```

- [ ] **Step 3:** Add explicit handler payload type mismatch test.

```go
func TestOnHandlerParamTypeMustMatchEventReturnType(t *testing.T) {
    src := `
module test.bad_on_type
data Event { byte: U8 }
data Other { byte: U8 }
driver path Path {
    interrupt receiver -> Event { return Event(byte = 0) }
}
executor Ex {
    path: Path
    on path.interrupt(event: Other) { }
}`
    _, diags := checkModuleForTest(t, src)
    if !hasCode(diags, diag.SEM0016) {
        t.Fatalf("expected SEM0016, got %#v", diags)
    }
}
```

- [ ] **Step 4:** Add direct-handler restriction tests.

```go
func TestOnHandlerRejectsDirectLoopsReturnsAndRuntimeConstruction(t *testing.T) {
    cases := []struct {
        name string
        body string
    }{
        {"while loop", "while true { }"},
        {"return value", "return event"},
        {"runtime construction", "let other = Path()"},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            src := `
module test.bad_on_body
data Event { byte: U8 }
driver path Path {
    interrupt receiver -> Event { return Event(byte = 0) }
}
executor Ex {
    path: Path
    on path.interrupt(event: Event) {
        ` + tc.body + `
    }
}`
            _, diags := checkModuleForTest(t, src)
            if !hasCode(diags, diag.SEM0016) {
                t.Fatalf("expected SEM0016, got %#v", diags)
            }
        })
    }
}

func TestOnHandlerRejectsForbiddenPlatformCalls(t *testing.T) {
    modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory
class ExecutorMemory {
    fn halt_forever(self) -> never {
        while true {
        }
    }
}`, `
module machine.x86_64.interrupts
class ApicInterruptController {
    fn enable_cpu_interrupts(self) {
    }
}`, `
module test.bad_on_platform_calls
use { ExecutorMemory } from machine.x86_64.executor_memory
use { ApicInterruptController } from machine.x86_64.interrupts
data Event { byte: U8 }
driver path Path {
    interrupt receiver -> Event { return Event(byte = 0) }
}
executor Ex {
    path: Path
    memory: ExecutorMemory
    interrupts: ApicInterruptController
    on path.interrupt(event: Event) {
        self.memory.halt_forever()
        self.interrupts.enable_cpu_interrupts()
    }
}`)
    index, ds := BuildIndex(modules)
    ds = filterMissingImageDiagnostic(ds)
    if len(ds) != 0 {
        t.Fatalf("index diagnostics: %#v", ds)
    }
    _, ds = Check(index, modules)
    if !hasCode(ds, diag.SEM0016) {
        t.Fatalf("expected SEM0016, got %#v", ds)
    }
}
```

- [ ] **Step 5:** Run `go test ./compiler/sem -run 'ExecutorMustHandle|OnHandlerMust|OnHandlerParamTypeMustMatch|OnHandlerRejects'`; expect failure.
- [ ] **Step 6:** Add a handler pass in the `ExecutorDecl` branch of `checkDeclBodiesAndConstructors`.

```go
case *ast.ExecutorDecl:
    typ := c.index.resolveInScope(mod.Name, d.Name)
    c.checkMethods(mod.Name, typ, d.Methods)
    c.checkOnHandlers(mod.Name, typ, d.OnHandlers)
```

- [ ] **Step 7:** Implement direct-field and event resolution helpers.

```go
func fieldByName(fields []Field, name string) *Field {
    for i := range fields {
        if fields[i].Name == name {
            return &fields[i]
        }
    }
    return nil
}

func (c *checker) eventDeclForPath(pathType *Type) *ast.InterruptEventDecl {
    if pathType == nil {
        return nil
    }
    return c.index.InterruptEvent(pathType.Module, pathType.Name)
}
```

- [ ] **Step 8:** Implement `checkOnHandlers` with explicit parameter type validation and direct-field-only validation.

```go
func (c *checker) checkOnHandlers(moduleName string, executorType *Type, handlers []ast.OnHandlerDecl) {
    if executorType == nil {
        return
    }
    seen := map[string]source.Span{}
    for i := range handlers {
        handler := &handlers[i]
        key := handler.PathField + ".interrupt"
        if first, ok := seen[key]; ok {
            _ = first
            c.error(handler.Span(), diag.SEM0014, "duplicate on handler "+key)
            continue
        }
        seen[key] = handler.Span()

        field := fieldByName(executorType.Fields, handler.PathField)
        if field == nil || field.Type == nil || field.Type.Kind != KindDriverPath {
            c.error(handler.Span(), diag.SEM0018, "on handler must reference a direct driver path field")
            continue
        }
        event := c.eventDeclForPath(field.Type)
        if event == nil {
            c.error(handler.Span(), diag.SEM0018, "unknown interrupt receiver on "+field.Type.Name)
            continue
        }
        eventType := c.resolveType(field.Type.Module, event.EventType)
        handlerType := c.resolveType(moduleName, handler.ParamType)
        if handlerType == nil {
            c.error(handler.Span(), diag.SEM0002, "unknown type "+handler.ParamType)
            continue
        }
        if eventType == nil || handlerType.Module != eventType.Module || handlerType.Name != eventType.Name {
            c.error(handler.Span(), diag.SEM0016, "on handler parameter type must match "+event.EventType)
            continue
        }
        scope := NewScope(nil)
        scope.Define("self", executorType)
        scope.Define(handler.ParamName, handlerType)
        prevType := c.currentType
        prevPhase := c.currentPhase
        c.currentType = executorType
        c.currentPhase = "on " + key
        c.checkStmtList(moduleName, handler.Body, scope, nil, ContextOnHandler)
        c.currentType = prevType
        c.currentPhase = prevPhase
    }
    c.checkExecutorInterruptCompleteness(executorType, seen)
}
```

Add the context:

```go
const (
    ContextNormalMethod ContextKind = iota
    ContextImagePhaseDirect
    ContextOwnershipTransferAuthorityMethod
    ContextInterruptEvent
    ContextOnHandler
)
```

- [ ] **Step 9:** Implement completeness over every direct driver-path field.

```go
func (c *checker) checkExecutorInterruptCompleteness(executorType *Type, seen map[string]source.Span) {
    for _, field := range executorType.Fields {
        if field.Type == nil || field.Type.Kind != KindDriverPath {
            continue
        }
        event := c.index.InterruptEvent(field.Type.Module, field.Type.Name)
        if event == nil {
            continue
        }
        key := field.Name + ".interrupt"
        if _, ok := seen[key]; !ok {
            c.error(field.Span, diag.SEM0017, "missing on handler for "+key)
            continue
        }
        c.bindings = append(c.bindings, InterruptBinding{
            ExecutorModule: executorType.Module,
            ExecutorType:   executorType.Name,
            PathField:      field.Name,
            PathType:       field.Type.Module + "." + field.Type.Name,
            EventType:      c.resolveType(field.Type.Module, event.EventType),
            Span:           field.Span,
        })
    }
}
```

- [ ] **Step 10:** Add direct-delivery restrictions for `ContextOnHandler` in `checkStmt`, `typeConstructorExpr`, and `typeCallExpr`.

```go
case *ast.WhileStmt, *ast.ForStmt:
    if ctx == ContextOnHandler {
        c.error(stmt.Span(), diag.SEM0016, "on handlers cannot loop")
        return false
    }
case *ast.ReturnStmt:
    if ctx == ContextOnHandler && s.Value != nil {
        c.error(s.SpanV, diag.SEM0016, "on handlers cannot return values")
        return true
    }
```

In `typeConstructorExpr`, reject runtime construction while still allowing data value construction:

```go
if ctx == ContextOnHandler && constructed.Kind != KindData {
    c.error(expr.SpanV, diag.SEM0016, "on handlers cannot construct runtime values")
    return nil
}
```

```go
func (c *checker) typeCallExpr(moduleName string, expr *ast.CallExpr, scope *Scope, ctx ContextKind) *Type {
    recvType := c.typeExpr(moduleName, expr.Receiver, scope, ctx)
    if recvType == nil {
        return nil
    }
    if ctx == ContextOnHandler && isForbiddenOnHandlerCall(recvType, expr.Method) {
        c.error(expr.Span(), diag.SEM0016, "call is not allowed inside direct interrupt handler")
        return nil
    }
    // existing method lookup and argument checks follow, reusing recvType.
}

func isForbiddenOnHandlerCall(receiverType *Type, method string) bool {
    if receiverType == nil {
        return false
    }
    switch receiverType.Module + "." + receiverType.Name + "::" + method {
    case "machine.x86_64.interrupts.ApicInterruptController::enable_cpu_interrupts",
        "machine.x86_64.interrupts.ApicInterruptController::initialize_for_com1_receive",
        "machine.x86_64.pci.Q35PciInterruptConfigurator::configure_edu_msi_vector41",
        "machine.x86_64.pci.Q35PciInterruptConfigurator::configure_ivshmem_msix_vector42",
        "machine.x86_64.executor_memory.ExecutorMemory::halt_forever",
        "arch.x86_64.cpu.CpuControl::halt_forever":
        return true
    default:
        return false
    }
}
```

- [ ] **Step 11:** Emit `SEM0017` for missing handler, `SEM0014` for duplicate handler, `SEM0018` for bad field or event, and `SEM0016` for handler parameter type mismatch or direct-delivery restriction violations.
- [ ] **Step 12:** Run `go test ./compiler/sem`; expect `PASS`.
- [ ] **Step 13:** Commit with `git commit -m "feat: enforce executor interrupt handler completeness -Codex Automated"`.

**Acceptance Criteria:** An executor cannot own an interrupt-capable path unless it handles all of that path's events.

---

### Task 8: Reject Illegal Interrupt Uses And Unsupported Runtime Shapes

**Files:** `compiler/parse/parser.go`, `compiler/sem/check.go`, `compiler/sem/types_test.go`, `tests/fixtures/negative/*.wrela`

**You may need to know:** `ast.CallExpr` has `Receiver ast.Expr` and `Method string`; there is no method-reference expression. Old `.interrupts.bind(...)` syntax is rejected by pattern-matching the whole call expression before ordinary receiver typing. The expression parser must accept `KeywordInterrupt` after a dot selector so `self.path.interrupt()` can be parsed and rejected semantically; `interrupt` remains illegal as a declaration name or bare identifier.

- [ ] **Step 1:** Add normal-use rejection test.

```go
func TestInterruptEventCannotBeCalledNormally(t *testing.T) {
    src := `
module test.bad_interrupt_call
data Event { byte: U8 }
driver path Path {
    interrupt receiver -> Event { return Event(byte = 0) }
}
executor Ex {
    path: Path
    on path.interrupt(event: Event) { }
    start fn run(self) -> never {
        self.path.interrupt()
    }
}`
    _, diags := checkModuleForTest(t, src)
    if !hasCode(diags, diag.SEM0019) {
        t.Fatalf("expected SEM0019, got %#v", diags)
    }
}
```

- [ ] **Step 2:** Add explicit bind rejection test.

```go
func TestExplicitInterruptBindRejectedSemantically(t *testing.T) {
    src := `
module test.bad_bind
data Event { byte: U8 }
driver path Path {
    interrupt receiver -> Event { return Event(byte = 0) }
}
executor Ex {
    path: Path
    on path.interrupt(event: Event) { }
    start fn run(self) -> never {
        self.interrupts.bind(self.path.interrupt, self.on_receive)
    }
}`
    _, diags := checkModuleForTest(t, src)
    if !hasCode(diags, diag.SEM0019) {
        t.Fatalf("expected SEM0019, got %#v", diags)
    }
}
```

- [ ] **Step 3:** Add runtime support test.

```go
func TestOnlySupportedInterruptRuntimeShapesAreAccepted(t *testing.T) {
    src := `
module test.unsupported_interrupt_runtime
data Event { byte: U8 }
unique class DelegatedHardware {
    fn claim(self) -> OwnedHardware {
        return OwnedHardware()
    }
}
unique class OwnedHardware {
    fn halt(self) -> never {
        while true {
        }
    }
}
driver path OtherPath {
    interrupt receiver -> Event { return Event(byte = 0) }
}
executor Ex {
    path: OtherPath
    on path.interrupt(event: Event) { }
    start fn run(self) -> never {
        while true {
        }
    }
}
image Img {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.claim()
    }
    phase owned_hardware(hardware: OwnedHardware) -> never {
        let ex = Ex(path = OtherPath())
        ex.run()
    }
}`
    _, diags := checkModuleForTest(t, src)
    if !hasCode(diags, diag.SEM0020) {
        t.Fatalf("expected SEM0020, got %#v", diags)
    }
}
```

- [ ] **Step 4:** Add reachability guard test.

```go
func TestUnsupportedInterruptRuntimeShapeIgnoredWhenUnreachable(t *testing.T) {
    src := `
module test.unreachable_interrupt_runtime
data Event { byte: U8 }
unique class DelegatedHardware {
    fn claim(self) -> OwnedHardware {
        return OwnedHardware()
    }
}
unique class OwnedHardware {
    fn halt(self) -> never {
        while true {
        }
    }
}
driver path OtherPath {
    interrupt receiver -> Event { return Event(byte = 0) }
}
executor UnusedEx {
    path: OtherPath
    on path.interrupt(event: Event) { }
    start fn run(self) -> never {
        while true {
        }
    }
}
image Img {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.claim()
    }
    phase owned_hardware(hardware: OwnedHardware) -> never {
        hardware.halt()
    }
}`
    program, diags := checkModuleForTest(t, src)
    if hasCode(diags, diag.SEM0020) {
        t.Fatalf("unreachable interrupt binding must not emit SEM0020: %#v", diags)
    }
    if len(program.InterruptBindings) != 0 {
        t.Fatalf("unreachable interrupt binding must not reach IR metadata: %#v", program.InterruptBindings)
    }
}
```

- [ ] **Step 5:** Run `go test ./compiler/sem -run 'InterruptEventCannot|ExplicitInterruptBind|OnlySupportedInterruptRuntime|UnsupportedInterruptRuntimeShapeIgnored'`; expect failure.
- [ ] **Step 6:** Permit `.interrupt` only as a selector token in the expression parser.

```go
func isSelectorNameToken(tok lex.Token) bool {
    return isNameToken(tok) || tok.Kind == lex.KeywordInterrupt
}
```

Use `isSelectorNameToken` only for the token immediately after `.` in `FieldExpr` and `CallExpr` parsing. Keep `isNameToken` unchanged for declarations and bare names.

- [ ] **Step 7:** Add loadable negative fixture files.

Create `tests/fixtures/negative/interrupt_event_call.wrela`:

```wrela
// expect: SEM0019: interrupt receiver cannot be called normally
module negative.interrupt_event_call

data Event { byte: U8 }

driver path Path {
    interrupt receiver -> Event { return Event(byte = 0) }
}

executor Ex {
    path: Path
    on path.interrupt(event: Event) { }

    start fn run(self) -> never {
        self.path.interrupt()
    }
}
```

Create `tests/fixtures/negative/interrupt_bind_removed.wrela`:

```wrela
// expect: SEM0019: explicit interrupt binding is not supported
module negative.interrupt_bind_removed

data Event { byte: U8 }

driver path Path {
    interrupt receiver -> Event { return Event(byte = 0) }
}

executor Ex {
    path: Path
    on path.interrupt(event: Event) { }

    start fn run(self) -> never {
        self.interrupts.bind(self.path.receive, self.on_receive)
    }
}
```

- [ ] **Step 8:** Reject normal method-call syntax that targets an interrupt event at the start of `typeCallExpr`. Use this final merged shape so the Task 7 on-handler forbidden-call branch is preserved.

```go
func (c *checker) typeCallExpr(moduleName string, expr *ast.CallExpr, scope *Scope, ctx ContextKind) *Type {
    if c.isExplicitInterruptBind(expr) {
        c.error(expr.Span(), diag.SEM0019, "explicit interrupt binding is not supported; use on path.interrupt handlers")
        return nil
    }

    recvType := c.typeExpr(moduleName, expr.Receiver, scope, ctx)
    if recvType == nil {
        return nil
    }
    if ctx == ContextOnHandler && isForbiddenOnHandlerCall(recvType, expr.Method) {
        c.error(expr.Span(), diag.SEM0016, "call is not allowed inside direct interrupt handler")
        return nil
    }
    if recvType.Kind == KindDriverPath && expr.Method == "interrupt" && c.index.InterruptEvent(recvType.Module, recvType.Name) != nil {
        c.error(expr.Span(), diag.SEM0019, "interrupt receiver cannot be called normally")
        return nil
    }

    // existing method lookup and call argument checks follow, reusing recvType.
}
```

- [ ] **Step 9:** Reject the old `.interrupts.bind(...)` call shape as `SEM0019`; do not add a synthetic `.interrupts` bind namespace. Ordinary fields named `interrupts` remain legal and type-check normally.

```go
func (c *checker) isExplicitInterruptBind(expr *ast.CallExpr) bool {
    if expr == nil || expr.Method != "bind" {
        return false
    }
    recv, ok := expr.Receiver.(*ast.FieldExpr)
    return ok && recv.Field == "interrupts"
}
```

- [ ] **Step 10:** Add the runtime support check from Section 2 by validating the derived semantic bindings after `checkExecutorWiring`.

Add storage to the checker:

```go
type checker struct {
    index           *Index
    modules         []*ast.Module
    currentType     *Type
    currentPhase    string
    diags           []diag.Diagnostic
    ownedRoot       *Type
    graph           ImageGraph
    bindings        []InterruptBinding
    runtimeBindings []InterruptBinding
}
```

```go
func (c *checker) vectorForInterruptBinding(binding InterruptBinding) (uint8, bool) {
    switch binding.PathType {
    case "machine.x86_64.serial.SerialConsolePath":
        return 0x40, true
    case "machine.x86_64.edu.EduMsiPath":
        return 0x41, true
    case "machine.x86_64.ivshmem.IvshmemMsixPath":
        return 0x42, true
    default:
        return 0, false
    }
}

func (c *checker) checkInterruptRuntimeSupport() {
    c.runtimeBindings = c.runtimeBindings[:0]
    for i := range c.bindings {
        if !c.isReachableInterruptBinding(c.bindings[i]) {
            continue
        }
        vector, ok := c.vectorForInterruptBinding(c.bindings[i])
        if !ok {
            c.error(c.bindings[i].Span, diag.SEM0020, "unsupported interrupt runtime shape "+c.bindings[i].PathType+".interrupt")
            continue
        }
        c.bindings[i].Vector = vector
        c.runtimeBindings = append(c.runtimeBindings, c.bindings[i])
    }
}

func (c *checker) isReachableInterruptBinding(binding InterruptBinding) bool {
    for _, exec := range c.graph.Executors {
        if exec.Type == nil || exec.Type.Module != binding.ExecutorModule || exec.Type.Name != binding.ExecutorType {
            continue
        }
        if exec.FieldBindings[binding.PathField] != "" || exec.PathUses[binding.PathField].Key != "" {
            return true
        }
    }
    return false
}
```

Wire the check into `Check` after declaration bodies have been checked and immediately after `checkExecutorWiring()`. Do not run runtime support before executor wiring; reachability depends on `c.graph.Executors`. Keep the full final order below so existing delegated-only and unique checks remain in place:

```go
c.checkImageSignatures()
c.checkUnresolvedTypes()
c.checkDeclBodiesAndConstructors()
c.checkDelegatedOnlyCrossing()
c.checkUniqueConstructors()
c.checkExecutorWiring()
c.checkInterruptRuntimeSupport()
```

Populate `CheckedProgram.InterruptBindings` from `c.runtimeBindings` in `Check`; do not expose unreachable unsupported bindings to IR lowering.

```go
return &CheckedProgram{
    Modules:           modules,
    Index:             index,
    ImageGraph:        c.graph,
    OwnedRoot:         c.ownedRoot,
    InterruptBindings: c.runtimeBindings,
}, c.diags
```
- [ ] **Step 11:** Run `go test ./compiler/sem`; expect `PASS`.
- [ ] **Step 12:** Commit with `git commit -m "feat: reject unsupported interrupt runtime shapes -Codex Automated"`.

**Acceptance Criteria:** Runtime-supported source shapes are limited to `SerialConsolePath.interrupt`, `EduMsiPath.interrupt`, and `IvshmemMsixPath.interrupt`.

---

### Task 9: Lower Interrupt Events And On Handlers To IR

**Files:** `compiler/ir/ir.go`, `compiler/ir/lower.go`, `compiler/ir/lower_test.go`

- [ ] **Step 1:** Add IR lowering test.

```go
func TestLowerInterruptEventsAndOnHandlers(t *testing.T) {
    checked := checkedProgramForTest(t, `
module machine.x86_64.serial
data SerialPathInterrupt { byte: U8 }
driver path SerialConsolePath {
    interrupt receiver -> SerialPathInterrupt { return SerialPathInterrupt(byte = 0) }
}
executor HelloWorld {
    serial_path: SerialConsolePath
    on serial_path.interrupt(event: SerialPathInterrupt) { }
}`)
    program, ds := Lower(checked)
    if len(ds) != 0 {
        t.Fatalf("Lower diagnostics = %#v", ds)
    }
    if len(program.InterruptEvents) != 1 || len(program.OnHandlers) != 1 || len(program.InterruptBindings) != 1 {
        t.Fatalf("interrupt metadata = events %d handlers %d bindings %d", len(program.InterruptEvents), len(program.OnHandlers), len(program.InterruptBindings))
    }
    if program.InterruptBindings[0].Vector != 0x40 {
        t.Fatalf("vector = %#x, want 0x40", program.InterruptBindings[0].Vector)
    }
    event := program.InterruptEvents[0]
    if event.PathType.Name != "SerialConsolePath" || event.EventType.Name != "SerialPathInterrupt" || event.FunctionSymbol != "_wrela_event_fn_machine_x86_64_serial_SerialConsolePath_interrupt" {
        t.Fatalf("event metadata = %#v", event)
    }
    handler := program.OnHandlers[0]
    if handler.ExecutorType.Name != "HelloWorld" || handler.PathField != "serial_path" || handler.EventType.Name != "SerialPathInterrupt" {
        t.Fatalf("handler metadata = %#v", handler)
    }
}
```

- [ ] **Step 2:** Run `go test ./compiler/ir -run InterruptEvents`; expect failure.
- [ ] **Step 3:** Add only the Task 9 IR surface from Section 2: `InterruptEvent`, `OnHandler`, `InterruptBinding`, and the `Program.InterruptEvents`, `Program.OnHandlers`, and `Program.InterruptBindings` fields. Do not add `WritableData`, `InterruptContext`, `InterruptContextStore`, or `Program.InterruptContexts` in this task; Task 16 owns those runtime-context additions.
- [ ] **Step 4:** Call interrupt lowering after `lowerSourceMethods` in `Lower`.

```go
ctx.lowerSourceMethods()
ctx.lowerInterruptEventsAndHandlers()
```

- [ ] **Step 5:** Lower receiver bodies to `symbolName("event_fn", module, pathType, "interrupt")` using the same statement lowering machinery as normal methods. When lowering bindings, `PathFieldOffset` must use `executorInfo.Fields[binding.PathField].Offset`, not `StorageOffset`; driver-path handles are not data fields and their `StorageOffset` is `-1` in real IR layouts.

```go
func (ctx *lowerContext) lowerInterruptEventsAndHandlers() {
    for _, mod := range ctx.checked.Modules {
        for _, decl := range mod.Decls {
            switch d := decl.(type) {
            case *ast.DriverPathDecl:
                pathType := ctx.resolveType(mod.Name, d.Name)
                for i := range d.InterruptEvents {
                    ctx.lowerInterruptEvent(mod.Name, pathType, &d.InterruptEvents[i])
                }
            case *ast.ExecutorDecl:
                execType := ctx.resolveType(mod.Name, d.Name)
                for i := range d.OnHandlers {
                    ctx.lowerOnHandler(mod.Name, execType, &d.OnHandlers[i])
                }
            }
        }
    }
    for _, binding := range ctx.checked.InterruptBindings {
        eventType := ctx.irType(binding.EventType)
        eventInfo, ok := typeInfoFor(ctx.program.Types, eventType)
        if !ok {
            ctx.errorf("missing type info for interrupt event %s.%s", eventType.Module, eventType.Name)
            continue
        }
        eventStorageSize := eventInfo.StorageSize
        if eventStorageSize == 0 {
            eventStorageSize = eventInfo.Size
        }
        if eventStorageSize == 0 {
            eventStorageSize = 8
        }
        executorType := Type{Name: binding.ExecutorType, Module: binding.ExecutorModule, Kind: TypeKindExecutor}
        executorInfo, ok := typeInfoFor(ctx.program.Types, executorType)
        if !ok {
            ctx.errorf("missing type info for interrupt executor %s.%s", executorType.Module, executorType.Name)
            continue
        }
        pathField, ok := executorInfo.Fields[binding.PathField]
        if !ok {
            ctx.errorf("missing interrupt path field %s on executor %s.%s", binding.PathField, executorType.Module, executorType.Name)
            continue
        }
        ctx.program.InterruptBindings = append(ctx.program.InterruptBindings, InterruptBinding{
            EventSymbol:           logicalSymbol("interrupt_event", pathModule(binding.PathType), pathName(binding.PathType), "interrupt"),
            HandlerSymbol:         logicalSymbol("on_handler", binding.ExecutorModule, binding.ExecutorType, binding.PathField, "interrupt"),
            EventFunctionSymbol:   symbolName("event_fn", pathModule(binding.PathType), pathName(binding.PathType), "interrupt"),
            HandlerFunctionSymbol: symbolName("on_fn", binding.ExecutorModule, binding.ExecutorType, binding.PathField, "interrupt"),
            ExecutorType:          executorType,
            PathField:             binding.PathField,
            PathFieldOffset:       pathField.Offset,
            EventStorageSymbol:    fmt.Sprintf("_wrela_interrupt_event_%02x", binding.Vector),
            EventStorageSize:      eventStorageSize,
            Vector:                binding.Vector,
        })
    }
    sort.Slice(ctx.program.InterruptBindings, func(i, j int) bool {
        if ctx.program.InterruptBindings[i].Vector != ctx.program.InterruptBindings[j].Vector {
            return ctx.program.InterruptBindings[i].Vector < ctx.program.InterruptBindings[j].Vector
        }
        return ctx.program.InterruptBindings[i].HandlerSymbol < ctx.program.InterruptBindings[j].HandlerSymbol
    })
}
```

Add helpers that preserve dots inside module names:

The file must import `fmt`, `sort`, and `strings`.

```go
func logicalSymbol(parts ...string) string {
    return strings.Join(parts, "::")
}

func pathModule(pathType string) string {
    parts := strings.Split(pathType, ".")
    if len(parts) <= 1 {
        return ""
    }
    return strings.Join(parts[:len(parts)-1], ".")
}

func pathName(pathType string) string {
    parts := strings.Split(pathType, ".")
    return parts[len(parts)-1]
}

func typeInfoFor(types map[string]TypeInfo, typ Type) (TypeInfo, bool) {
    if typ.Module != "" {
        if info, ok := types[typ.Module+"."+typ.Name]; ok {
            return info, true
        }
    }
    info, ok := types[typ.Name]
    return info, ok
}
```

- [ ] **Step 6:** Implement event lowering.

```go
func (ctx *lowerContext) lowerInterruptEvent(moduleName string, pathType *sem.Type, event *ast.InterruptEventDecl) {
    eventType := ctx.resolveType(moduleName, event.EventType)
    self := &Param{Symbol: "self", Type: ctx.irType(pathType)}
    scope := newLowerScope(nil)
    scope.define("self", lowerBinding{value: self, typ: pathType})
    ops := ctx.lowerStmtList(moduleName, pathType, scope, assignedNames(event.Body), event.Body)
    fnSymbol := symbolName("event_fn", moduleName, pathType.Name, "interrupt")
    ctx.program.Functions = append(ctx.program.Functions, Function{
        Symbol: fnSymbol,
        Return: ctx.irType(eventType),
        Params: []Value{self},
        Blocks: []Block{{Label: "entry", Ops: ops}},
    })
    ctx.program.InterruptEvents = append(ctx.program.InterruptEvents, InterruptEvent{
        Symbol: logicalSymbol("interrupt_event", moduleName, pathType.Name, "interrupt"),
        PathType: ctx.irType(pathType),
        EventType: ctx.irType(eventType),
        FunctionSymbol: fnSymbol,
    })
}
```

- [ ] **Step 7:** Implement `on` handler lowering with the explicit, semantically-validated event parameter type.

```go
func (ctx *lowerContext) lowerOnHandler(moduleName string, execType *sem.Type, handler *ast.OnHandlerDecl) {
    eventType := ctx.resolveType(moduleName, handler.ParamType)

    self := &Param{Symbol: "self", Type: ctx.irType(execType)}
    eventParam := &Param{Symbol: handler.ParamName, Type: ctx.irType(eventType)}
    scope := newLowerScope(nil)
    scope.define("self", lowerBinding{value: self, typ: execType})
    scope.define(handler.ParamName, lowerBinding{value: eventParam, typ: eventType})

    ops := ctx.lowerStmtList(moduleName, execType, scope, assignedNames(handler.Body), handler.Body)
    fnSymbol := symbolName("on_fn", moduleName, execType.Name, handler.PathField, "interrupt")
    ctx.program.Functions = append(ctx.program.Functions, Function{
        Symbol: fnSymbol,
        Return: Type{Name: "void", Module: "builtin", Kind: TypeKindPrimitive},
        Params: []Value{self, eventParam},
        Blocks: []Block{{Label: "entry", Ops: ops}},
    })
    ctx.program.OnHandlers = append(ctx.program.OnHandlers, OnHandler{
        Symbol: logicalSymbol("on_handler", moduleName, execType.Name, handler.PathField, "interrupt"),
        ExecutorType: ctx.irType(execType),
        PathField: handler.PathField,
        EventType: ctx.irType(eventType),
        FunctionSymbol: fnSymbol,
    })
}

```
- [ ] **Step 8:** Run `go test ./compiler/ir`; expect `PASS`.
- [ ] **Step 9:** Commit with `git commit -m "feat: lower interrupt events to ir -Codex Automated"`.

**Acceptance Criteria:** IR contains lowered event construction, handler body, and derived binding metadata.

---

### Task 10: Preserve Interrupt Metadata Through Build And Codegen

**Files:** `compiler/codegen/program.go`, `compiler/codegen/basic_test.go`, `compiler/build.go`

- [ ] **Step 1:** Extend `compiler.BuildResult` exactly:

```go
type BuildResult struct {
    OutputPath string
    Image      *codegen.Image
}
```

Return it exactly:

```go
return BuildResult{OutputPath: outputPath, Image: image}, nil
```

- [ ] **Step 2:** Add codegen metadata test.

```go
func TestCompilePreservesInterruptBindings(t *testing.T) {
    eventFn := ir.Function{
        Symbol: "_wrela_test_interrupt_event",
        Return: ir.Type{Name: "void", Module: "builtin", Kind: ir.TypeKindPrimitive},
        Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{&ir.Return{}}}},
    }
    handlerFn := ir.Function{
        Symbol: "_wrela_test_interrupt_handler",
        Return: ir.Type{Name: "void", Module: "builtin", Kind: ir.TypeKindPrimitive},
        Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{&ir.Return{}}}},
    }
    img, ds := Compile(&ir.Program{
        Functions: []ir.Function{eventFn, handlerFn},
        InterruptContexts: []ir.InterruptContext{{
            Symbol: "_wrela_test_interrupt_context",
            Size:   8,
        }},
        InterruptBindings: []ir.InterruptBinding{
            {EventSymbol: "interrupt_event::machine.x86_64.serial::SerialConsolePath::interrupt", HandlerSymbol: "on_handler::examples.hello.program::HelloWorld::serial_path::interrupt", EventFunctionSymbol: eventFn.Symbol, HandlerFunctionSymbol: handlerFn.Symbol, ContextSymbol: "_wrela_test_interrupt_context", EventStorageSymbol: "_wrela_test_interrupt_event_40", EventStorageSize: 8, Vector: 0x40},
            {EventSymbol: "interrupt_event::machine.x86_64.edu::EduMsiPath::interrupt", HandlerSymbol: "on_handler::examples.hello.program::HelloWorld::edu_path::interrupt", EventFunctionSymbol: eventFn.Symbol, HandlerFunctionSymbol: handlerFn.Symbol, ContextSymbol: "_wrela_test_interrupt_context", EventStorageSymbol: "_wrela_test_interrupt_event_41", EventStorageSize: 8, Vector: 0x41},
            {EventSymbol: "interrupt_event::machine.x86_64.ivshmem::IvshmemMsixPath::interrupt", HandlerSymbol: "on_handler::examples.hello.program::HelloWorld::ivshmem_rx::interrupt", EventFunctionSymbol: eventFn.Symbol, HandlerFunctionSymbol: handlerFn.Symbol, ContextSymbol: "_wrela_test_interrupt_context", EventStorageSymbol: "_wrela_test_interrupt_event_42", EventStorageSize: 8, Vector: 0x42},
        },
    })
    if len(ds) != 0 {
        t.Fatalf("Compile diagnostics = %#v", ds)
    }
    got := map[uint8]bool{}
    for _, binding := range img.InterruptBindings {
        got[binding.Vector] = true
    }
    if len(img.InterruptBindings) != 3 || !got[0x40] || !got[0x41] || !got[0x42] {
        t.Fatalf("interrupt bindings = %#v", img.InterruptBindings)
    }
}
```

- [ ] **Step 3:** Run `go test ./compiler/codegen ./compiler -run Interrupt`; expect failure.
- [ ] **Step 4:** Add `codegen.InterruptBinding` and `Image.InterruptBindings`.

```go
type InterruptBinding struct {
    EventSymbol           string
    HandlerSymbol         string
    EventFunctionSymbol   string
    HandlerFunctionSymbol string
    PathFieldOffset       int
    ContextSymbol         string
    EventStorageSymbol    string
    EventStorageSize      int
    Vector                uint8
}

type Image struct {
    EntrySymbol       string
    Sections          []Section
    Symbols           map[string]uint64
    Relocs            []Reloc
    InterruptBindings []InterruptBinding
}
```

- [ ] **Step 5:** Copy IR bindings into image metadata in `Compile`.

```go
func codegenInterruptBindings(in []ir.InterruptBinding) []InterruptBinding {
    out := make([]InterruptBinding, 0, len(in))
    for _, b := range in {
        out = append(out, InterruptBinding{
            EventSymbol:           b.EventSymbol,
            HandlerSymbol:         b.HandlerSymbol,
            EventFunctionSymbol:   b.EventFunctionSymbol,
            HandlerFunctionSymbol: b.HandlerFunctionSymbol,
            PathFieldOffset:       b.PathFieldOffset,
            ContextSymbol:         b.ContextSymbol,
            EventStorageSymbol:    b.EventStorageSymbol,
            EventStorageSize:      b.EventStorageSize,
            Vector:                b.Vector,
        })
    }
    return out
}
```

Use it in the image return:

```go
return &Image{
    EntrySymbol:       program.Entry.Symbol,
    Sections:          sections,
    Symbols:           symbols,
    Relocs:            relocs,
    InterruptBindings: codegenInterruptBindings(program.InterruptBindings),
}, nil
```

- [ ] **Step 6:** Populate `BuildResult.Image` in `compiler/build.go`.

```go
return BuildResult{OutputPath: outputPath, Image: image}, nil
```
- [ ] **Step 7:** Run `go test ./compiler/codegen ./compiler`; expect `PASS`.
- [ ] **Step 8:** Commit with `git commit -m "feat: preserve interrupt metadata through build -Codex Automated"`.

**Acceptance Criteria:** Build callers can assert interrupt metadata before QEMU e2e tests run.

---

### Task 10A: Lower And Emit Shift/Or Binary Operators

**Files:** `compiler/ir/lower.go`, `compiler/ir/lower_test.go`, `compiler/codegen/x64.go`, `compiler/codegen/control_test.go`

**You may need to know:** The lexer and parser already accept `<<`, `>>`, and `|`. Without this task, Task 11's PCI source compiles through parsing and semantic checking but fails in codegen with `CG0001 unsupported binary op`.

- [ ] **Step 1:** Add IR lowering test for the operators used by PCI helpers.

```go
func TestLowerShiftAndBitOrOperators(t *testing.T) {
    checked := checkedProgramForTest(t, `
module test.bit_ops
class BitOps {
    fn config_address(self, slot: U32, header: U32) -> U32 {
        return (0x80000000 | (slot << 11)) | ((header >> 8) & 0xFC)
    }
}`)
    program, ds := Lower(checked)
    if len(ds) != 0 {
        t.Fatalf("Lower diagnostics = %#v", ds)
    }
    fn := findFunctionForTest(program, "_wrela_method_test_bit_ops_BitOps_config_address")
    for _, want := range []string{"or", "shl", "shr", "and"} {
        if !functionHasBinaryOp(fn, want) {
            t.Fatalf("lowered function missing %s op: %#v", want, fn)
        }
    }
}

func findFunctionForTest(program *Program, symbol string) *Function {
    for i := range program.Functions {
        if program.Functions[i].Symbol == symbol {
            return &program.Functions[i]
        }
    }
    return nil
}

func functionHasBinaryOp(fn *Function, op string) bool {
    if fn == nil {
        return false
    }
    for _, block := range fn.Blocks {
        for _, operation := range block.Ops {
            if binary, ok := operation.(*Binary); ok && binary.Op == op {
                return true
            }
        }
    }
    return false
}
```

- [ ] **Step 2:** Run `go test ./compiler/ir -run ShiftAndBitOr`; expect failure.
- [ ] **Step 3:** Extend `lowerBinaryOp` in `compiler/ir/lower.go`.

```go
case "|":
    return "or"
case "<<":
    return "shl"
case ">>":
    return "shr"
```

- [ ] **Step 4:** Add codegen test proving the new IR ops compile and produce shift opcodes.

```go
func TestCompileShiftAndBitOrBinaryOps(t *testing.T) {
    u32 := ir.Type{Name: "U32", Module: "builtin", Kind: ir.TypeKindPrimitive}
    slot := &ir.Param{Symbol: "slot", Type: u32}
    header := &ir.Param{Symbol: "header", Type: u32}
    shift11 := &ir.ConstInt{Symbol: "shift11", Value: 11, Type: u32}
    shift8 := &ir.ConstInt{Symbol: "shift8", Value: 8, Type: u32}
    mask := &ir.ConstInt{Symbol: "mask", Value: 0xFC, Type: u32}
    base := &ir.ConstInt{Symbol: "base", Value: 0x80000000, Type: u32}
    slotShift := &ir.Binary{Op: "shl", Left: slot, Right: shift11, Type: u32}
    headerShift := &ir.Binary{Op: "shr", Left: header, Right: shift8, Type: u32}
    headerMask := &ir.Binary{Op: "and", Left: headerShift, Right: mask, Type: u32}
    baseOrSlot := &ir.Binary{Op: "or", Left: base, Right: slotShift, Type: u32}
    result := &ir.Binary{Op: "or", Left: baseOrSlot, Right: headerMask, Type: u32}
    fn := ir.Function{
        Symbol: "_wrela_test_shift_or",
        Params: []ir.Value{slot, header},
        Return: u32,
        Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
            shift11, shift8, mask, base, slotShift, headerShift, headerMask, baseOrSlot, result, &ir.Return{Value: result},
        }}},
    }

    img, ds := Compile(&ir.Program{Functions: []ir.Function{fn}})
    if len(ds) != 0 {
        t.Fatalf("Compile diagnostics = %#v", ds)
    }
    code := symbolBytes(t, img, fn.Symbol)
    for _, want := range [][]byte{
        {0x48, 0xC1, 0xE0, 0x0B}, // shl rax, 11
        {0x48, 0xC1, 0xE8, 0x08}, // shr rax, 8
    } {
        if !containsBytes(code, want) {
            t.Fatalf("compiled shift/or function missing bytes %#x in %#x", want, code)
        }
    }
}
```

- [ ] **Step 5:** Run `go test ./compiler/codegen -run ShiftAndBitOr`; expect failure.
- [ ] **Step 6:** Add immediate-shift support and register OR support in `emitBinary`.

```go
func constShiftAmount(value ir.Value) (uint8, bool) {
    c, ok := value.(*ir.ConstInt)
    if !ok || c.Value > 63 {
        return 0, false
    }
    return uint8(c.Value), true
}

func emitShiftImm(e *Emitter, reg asm.Reg, subop byte, amount uint8) {
    rex := byte(0x48)
    if reg.High {
        rex |= 0x01
    }
    e.emit(rex, 0xC1, encodeModRM(3, subop, reg.Low3), amount)
}
```

Use them in `emitBinary`:

```go
case "or":
    emitRegRegOp(e, 0x09, scratchRegs[0], scratchRegs[1])
case "shl", "shr":
    amount, ok := constShiftAmount(op.Right)
    if !ok {
        e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "shift amount must be a constant 0..63"})
        return
    }
    subop := byte(4)
    if op.Op == "shr" {
        subop = 5
    }
    emitShiftImm(e, scratchRegs[0], subop, amount)
```

- [ ] **Step 7:** Run `go test ./compiler/ir ./compiler/codegen -run 'ShiftAndBitOr|Interrupt'`; expect `PASS`.
- [ ] **Step 8:** Commit with `git commit -m "feat: lower shift and bitwise or operators -Codex Automated"`.

**Acceptance Criteria:** Task 11 PCI source can use `<<`, `>>`, and `|` without codegen `CG0001` diagnostics.

---

### Task 11: Add APIC, MSI, And MSI-X Support Modules

**Files:** `wrela/machine/x86_64/interrupts.wrela`, `wrela/machine/x86_64/pci.wrela`, `wrela/machine/x86_64/edu.wrela`, `wrela/machine/x86_64/ivshmem.wrela`, `compiler/sem/check.go`, `compiler/sem/uefi_source_shape_test.go`, `compiler/asm/encode_core_test.go`, `compiler/asm/encode.go`, `compiler/codegen/uefi_source_codegen_test.go`

**You may need to know:** `compiler/asm/encode_core_test.go` is where exact-byte assembler tests live. `compiler/codegen/uefi_source_codegen_test.go` already parses Wrela platform source and inspects lowered asm instructions.

- [ ] **Step 1:** Add source modules exactly as Section 4 defines `ApicInterruptController`, `EduMsiPath`, `IvshmemMsixPath`, `IvshmemDoorbellPeerPath`, and `Q35PciInterruptConfigurator`.
- [ ] **Step 1a:** Extend `parseUEFIModuleFiles` in `compiler/codegen/uefi_source_codegen_test.go` to load the new platform modules.

```go
paths := []string{
    filepath.Join(repoRoot, "wrela/platform/uefi/boot_services.wrela"),
    filepath.Join(repoRoot, "wrela/platform/uefi/transition.wrela"),
    filepath.Join(repoRoot, "wrela/platform/uefi/types.wrela"),
    filepath.Join(repoRoot, "wrela/machine/x86_64/cpu_state.wrela"),
    filepath.Join(repoRoot, "wrela/machine/x86_64/executor_memory.wrela"),
    filepath.Join(repoRoot, "wrela/machine/x86_64/serial.wrela"),
    filepath.Join(repoRoot, "wrela/machine/x86_64/interrupts.wrela"),
    filepath.Join(repoRoot, "wrela/machine/x86_64/pci.wrela"),
    filepath.Join(repoRoot, "wrela/machine/x86_64/edu.wrela"),
    filepath.Join(repoRoot, "wrela/machine/x86_64/ivshmem.wrela"),
}
```
- [ ] **Step 1b:** Extend the semantic asm allowlist for machine x86_64 edge-capability modules.

Add this semantic test:

```go
func TestMachineX64InterruptSupportAsmIsAllowed(t *testing.T) {
    _, ds := checkModuleForTest(t, `
module machine.x86_64.interrupts
class ApicInterruptController {
    asm fn enable_cpu_interrupts(self) {
        sti
    }
}`)
    if len(ds) != 0 {
        t.Fatalf("unexpected diagnostics: %#v", ds)
    }
}
```

Then update `isAsmAllowedHere` in `compiler/sem/check.go` to allow ordinary classes in modules whose names start with `machine.x86_64.`. Keep the existing `ExecutorMemory`, `arch.*`, `platform.*`, and `machine.x86_64.serial*` cases.

- [ ] **Step 2:** Add exact-byte assembler tests for 32-bit port I/O and `iretq` prerequisites not covered elsewhere.

```go
{
    name: "out dx, eax",
    code: []Instruction{{Mnemonic: "out", Operands: []Operand{RegOperand{MustLookup("dx")}, RegOperand{MustLookup("eax")}}}},
    want: []byte{0xEF},
},
{
    name: "in eax, dx",
    code: []Instruction{{Mnemonic: "in", Operands: []Operand{RegOperand{MustLookup("eax")}, RegOperand{MustLookup("dx")}}}},
    want: []byte{0xED},
},
{
    name: "mov mem32 eax",
    code: []Instruction{{Mnemonic: "mov", Operands: []Operand{MemOperand{Base: MustLookup("r11"), Disp: 0x10, Width: 32}, RegOperand{MustLookup("eax")}}}},
    want: []byte{0x41, 0x89, 0x43, 0x10},
},
{
    name: "mov eax mem32",
    code: []Instruction{{Mnemonic: "mov", Operands: []Operand{RegOperand{MustLookup("eax")}, MemOperand{Base: MustLookup("r11"), Disp: 0x10, Width: 32}}}},
    want: []byte{0x41, 0x8B, 0x43, 0x10},
},
```

- [ ] **Step 3:** Implement `out dx, eax` and `in eax, dx` in `encodeOut` and `encodeIn`.

```go
if strings.ToLower(a.Reg.Name) == "dx" && strings.ToLower(b.Reg.Name) == "eax" {
    return []byte{0xEF}, true
}
```

```go
if strings.ToLower(a.Reg.Name) == "eax" && strings.ToLower(b.Reg.Name) == "dx" {
    return []byte{0xED}, true
}
```

`encodeMovMemReg` and `encodeMovRegMem` already accept `MemOperand.Width`; only change them if the Step 2 exact-byte tests fail.

- [ ] **Step 4:** Add codegen test for APIC/MMIO source instructions.

```go
func TestInterruptPlatformSourceCodegen(t *testing.T) {
    checked := parseCheckedUEFIModules(t)
    program, ds := ir.Lower(checked)
    if len(ds) != 0 {
        t.Fatalf("Lower diagnostics: %#v", ds)
    }
    img, ds := Compile(program)
    if len(ds) != 0 {
        t.Fatalf("Compile diagnostics: %#v", ds)
    }
    allText := img.Sections[0].Data
    for _, want := range [][]byte{
        {0xFB}, // sti
        {0xEF}, // out dx,eax
        {0xED}, // in eax,dx
    } {
        if !containsBytes(allText, want) {
            t.Fatalf("compiled platform source missing bytes %#x", want)
        }
    }
}
```

- [ ] **Step 5:** Add source-shape tests checking that `Q35PciInterruptConfigurator` walks PCI capabilities instead of hardcoding capability offsets.

```go
func TestPciInterruptConfiguratorWalksCapabilities(t *testing.T) {
    checked := parseCheckedUEFIModules(t)
    _ = asmMethodFromSem(t, checked, "machine.x86_64.pci", "PciConfigPorts", "read32")
    program, ds := ir.Lower(checked)
    if len(ds) != 0 {
        t.Fatalf("Lower diagnostics: %#v", ds)
    }
    findCap := findIRFunction(program, "_wrela_method_machine_x86_64_pci_Q35PciInterruptConfigurator_find_capability")
    if findCap == nil {
        t.Fatalf("missing find_capability lowering")
    }
    edu := findIRFunction(program, "_wrela_method_machine_x86_64_pci_Q35PciInterruptConfigurator_configure_edu_msi_vector41")
    if !functionCalls(edu, "_wrela_method_machine_x86_64_pci_Q35PciInterruptConfigurator_find_capability") {
        t.Fatalf("configure_edu_msi_vector41 must call find_capability")
    }
    for _, want := range []uint64{0xFEE00000, 0x41, 0x80, 0x00010000} {
        if !functionHasConstInt(edu, want) {
            t.Fatalf("configure_edu_msi_vector41 missing constant %#x", want)
        }
    }
    msix := findIRFunction(program, "_wrela_method_machine_x86_64_pci_Q35PciInterruptConfigurator_configure_ivshmem_msix_vector42")
    if !functionCalls(msix, "_wrela_method_machine_x86_64_pci_Q35PciInterruptConfigurator_find_capability") {
        t.Fatalf("configure_ivshmem_msix_vector42 must call find_capability")
    }
    for _, want := range []uint64{0xFEE00000, 0x42, 0x80000000} {
        if !functionHasConstInt(msix, want) {
            t.Fatalf("configure_ivshmem_msix_vector42 missing constant %#x", want)
        }
    }
}

func findIRFunction(program *ir.Program, symbol string) *ir.Function {
    for i := range program.Functions {
        if program.Functions[i].Symbol == symbol {
            return &program.Functions[i]
        }
    }
    return nil
}

func functionHasConstInt(fn *ir.Function, value uint64) bool {
    if fn == nil {
        return false
    }
    for _, block := range fn.Blocks {
        for _, op := range block.Ops {
            if c, ok := op.(*ir.ConstInt); ok && c.Value == value {
                return true
            }
        }
    }
    return false
}

func functionCalls(fn *ir.Function, symbol string) bool {
    if fn == nil {
        return false
    }
    for _, block := range fn.Blocks {
        for _, op := range block.Ops {
            if call, ok := op.(*ir.Call); ok && call.Symbol == symbol {
                return true
            }
        }
    }
    return false
}
```

- [ ] **Step 6:** Run `go test ./compiler/asm -run EncodeExactInstructions`; expect failure until encoder support is added.
- [ ] **Step 7:** Run `go test ./compiler/codegen -run 'InterruptPlatform|PciInterrupt'`; expect failure until the modules exist.
- [ ] **Step 8:** Run `go test ./compiler/asm ./compiler/codegen`; expect `PASS`.
- [ ] **Step 9:** Commit with `git commit -m "feat: add apic msi and msix support modules -Codex Automated"`.

**Acceptance Criteria:** Wrela source can initialize Local APIC/IOAPIC, program EDU MSI, program ivshmem MSI-X table entry 0, and execute `sti`.

---

### Task 12: Extend Existing Hello Serial Source

**Files:** `wrela/machine/x86_64/serial.wrela`, `examples/hello/program.wrela`, `examples/hello/main.wrela`, `compiler/integration_test.go`

- [ ] **Step 1:** Add `SerialPathInterrupt` and `SerialConsolePath` exactly as Section 4 defines them.
- [ ] **Step 2:** Update `examples/hello/program.wrela` exactly as Section 4 defines it.
- [ ] **Step 3:** Update `examples/hello/main.wrela` owned phase exactly as Section 4 defines it; keep the delegated phase unchanged.
- [ ] **Step 4:** Add integration test.

```go
func TestBuildHelloContainsInterruptBinding(t *testing.T) {
    tmp := t.TempDir()
    out := filepath.Join(tmp, "hello.efi")
    result, err := Build(BuildOptions{
        Mode: ModeDev,
        RootPath: "examples/hello/main.wrela",
        OutputPath: out,
        RepoRoot: ".",
    })
    if err != nil {
        t.Fatalf("Build hello: %v", err)
    }
    if result.Image == nil {
        t.Fatalf("BuildResult.Image is nil")
    }
    if got := len(result.Image.InterruptBindings); got != 3 {
        t.Fatalf("interrupt bindings = %d, want 3", got)
    }
    gotVectors := map[uint8]bool{}
    for _, binding := range result.Image.InterruptBindings {
        gotVectors[binding.Vector] = true
    }
    for _, want := range []uint8{0x40, 0x41, 0x42} {
        if !gotVectors[want] {
            t.Fatalf("missing vector %#x in bindings %#v", want, result.Image.InterruptBindings)
        }
    }
}
```

- [ ] **Step 5:** Run `go test ./compiler -run BuildHelloContainsInterruptBinding`; expect failure before frontend/semantics are complete.
- [ ] **Step 6:** Run `go test ./compiler`; expect `PASS`.
- [ ] **Step 7:** Commit with `git commit -m "feat: extend hello with serial interrupt source -Codex Automated"`.

**Acceptance Criteria:** The existing hello example owns serial IOAPIC, EDU MSI, and ivshmem MSI-X interrupt-capable paths and has complete `on` handlers for all three.

---

### Task 13: Add Interrupt Return Instruction Support

**Files:** `compiler/asm/parse.go`, `compiler/asm/encode.go`, `compiler/asm/encode_core_test.go`

- [ ] **Step 1:** Add exact-byte test.

```go
{
    name: "iretq",
    code: []Instruction{{Mnemonic: "iretq"}},
    want: []byte{0x48, 0xCF},
}
```

- [ ] **Step 2:** Run `go test ./compiler/asm -run EncodeExactInstructions`; expect failure.
- [ ] **Step 3:** Add `iretq` to parser known mnemonics.
- [ ] **Step 4:** Encode `iretq` as `48 CF`.
- [ ] **Step 5:** Run `go test ./compiler/asm`; expect `PASS`.
- [ ] **Step 6:** Commit with `git commit -m "feat: encode iretq instruction -Codex Automated"`.

**Acceptance Criteria:** Generated interrupt stubs can return from hardware interrupts.

---

### Task 14A: Add External Branch Relocations For Asm Methods

**Files:** `compiler/codegen/asm_method.go`, `compiler/codegen/x64.go`, `compiler/codegen/interrupt_test.go`

**You may need to know:** Normal IR calls already use `compiledUnit.CallReloc` and are patched in `Compile`. Asm methods currently return `compiledUnit{Symbol: method.Symbol, Bytes: code}` with no branch relocations. This task extends asm-method codegen only for `call _wrela_*` and `jmp _wrela_*` targets; local labels remain handled by the assembler.

- [ ] **Step 1:** Add codegen test for an asm method calling an external generated symbol.

```go
func TestAsmMethodExternalBranchRelocation(t *testing.T) {
    method := ir.AsmMethod{
        Symbol: "_wrela_method_platform_uefi_transition_DelegatedHardware_capture_vector40_serial_handler",
        Body:   "call _wrela_interrupt_vector40_serial\njmp _wrela_interrupt_vector41_edu_msi\nret",
    }
    unit, ds := compileAsmMethodUnit(method)
    if len(ds) != 0 {
        t.Fatalf("compileAsmMethodUnit diagnostics: %#v", ds)
    }
    if len(unit.CallReloc) != 2 {
        t.Fatalf("branch relocs = %#v, want two", unit.CallReloc)
    }
    if unit.CallReloc[0].Symbol != "_wrela_interrupt_vector40_serial" {
        t.Fatalf("reloc symbol = %q", unit.CallReloc[0].Symbol)
    }
    if unit.CallReloc[1].Symbol != "_wrela_interrupt_vector41_edu_msi" {
        t.Fatalf("reloc symbol = %q", unit.CallReloc[1].Symbol)
    }
    if !containsBytes(unit.Bytes, []byte{0xE8, 0, 0, 0, 0}) {
        t.Fatalf("external call must encode as zero rel32 before relocation: %#x", unit.Bytes)
    }
    if !containsBytes(unit.Bytes, []byte{0xE9, 0, 0, 0, 0}) {
        t.Fatalf("external jmp must encode as zero rel32 before relocation: %#x", unit.Bytes)
    }
}
```

- [ ] **Step 2:** Run `go test ./compiler/codegen -run AsmMethodExternalBranchRelocation`; expect failure.

- [ ] **Step 3:** Add external-call support to the assembler without changing `asm.Encode` callers.

```go
// compiler/asm/encode.go
type ExternalCallReloc struct {
    Offset uint64
    Symbol string
}

type branchFixup struct {
    relPos   int
    target   string
    nextPC   int
    mnemonic string
}

func EncodeWithExternalCalls(instructions []Instruction) ([]byte, []ExternalCallReloc, []diag.Diagnostic) {
    return encode(instructions, true)
}

func Encode(instructions []Instruction) ([]byte, []diag.Diagnostic) {
    code, _, ds := encode(instructions, false)
    return code, ds
}
```

Move the current `Encode` body into a new private `encode` helper with this signature:

```go
func encode(instructions []Instruction, allowExternalCalls bool) ([]byte, []ExternalCallReloc, []diag.Diagnostic)
```

Inside that helper, add `var external []ExternalCallReloc` next to the existing `diags` declaration and return `out, external, diags` at the end.

In the moved final fixup loop, use this exact branch:

```go
target, ok := labels[fix.target]
if !ok {
    if allowExternalCalls && (fix.mnemonic == "call" || fix.mnemonic == "jmp") && strings.HasPrefix(fix.target, "_wrela_") {
        external = append(external, ExternalCallReloc{Offset: uint64(fix.relPos), Symbol: fix.target})
        continue
    }
    diags = append(diags, diag.Diagnostic{Phase: "asm", Code: diag.ASM0002, Message: "unknown label: " + fix.target})
    continue
}
```

Set `mnemonic` when adding fixups:

```go
*fixups = append(*fixups, branchFixup{
    relPos:   start + 1,
    target:   target.Name,
    nextPC:   start + size,
    mnemonic: strings.ToLower(ins.Mnemonic),
})
```

- [ ] **Step 4:** Update `compileAsmMethodUnit` to return call relocs from `asm.EncodeWithExternalCalls`.

```go
instructions, ds := lowerAsmMethodInstructions(method)
if len(ds) != 0 {
    return compiledUnit{}, ds
}
code, externalCalls, ds := asm.EncodeWithExternalCalls(instructions)
if len(ds) != 0 {
    return compiledUnit{}, ds
}
callRelocs := make([]internalReloc, 0, len(externalCalls))
for _, rel := range externalCalls {
    callRelocs = append(callRelocs, internalReloc{Offset: rel.Offset, Symbol: rel.Symbol})
}
return compiledUnit{Symbol: method.Symbol, Bytes: code, CallReloc: callRelocs}, nil
```

Split `lowerAndEncodeAsmMethod` into these two functions so `compileAsmMethodUnit` can access lowered instructions before encoding:

```go
func lowerAsmMethodInstructions(method ir.AsmMethod) ([]asm.Instruction, []diag.Diagnostic)
func lowerAndEncodeAsmMethod(method ir.AsmMethod) ([]asm.Instruction, []diag.Diagnostic, []byte)
```

- [ ] **Step 5:** Run `go test ./compiler/codegen -run AsmMethodExternalBranchRelocation`; expect `PASS`.
- [ ] **Step 6:** Commit with `git commit -m "feat: relocate external asm branches -Codex Automated"`.

**Acceptance Criteria:** UEFI transition asm can call or tail-jump to generated `_wrela_interrupt_*` symbols without inventing a method-reference language feature.

---

### Task 14: Build IDT With Vector 0x40, 0x41, And 0x42 Overrides

**Files:** `wrela/platform/uefi/types.wrela`, `wrela/platform/uefi/transition.wrela`, `compiler/codegen/uefi_source_codegen_test.go`

- [ ] **Step 1:** Replace `DelegatedMemory.build_fatal_idt` with this compatible superset:

```wrela
asm fn build_interrupt_idt(
    self,
    fatal_handler: VirtualAddress,
    vector40_handler: VirtualAddress,
    vector41_handler: VirtualAddress,
    vector42_handler: VirtualAddress
) -> DelegatedBytes {
    push r12
    push r13
    push r14
    push r15

    mov r14, self.next_offset
    mov r11, self.arena_base
    add r11, r14
    add r14, 4112
    mov self.next_offset, r14

    mov ax, 4095
    mov [r11], ax
    mov rax, r11
    add rax, 16
    mov [r11 + 2], rax

    mov r12, r11
    add r12, 16
    mov rcx, 256
    mov r13, fatal_handler
idt_gate_loop:
    call write_idt_gate
    add r12, 16
    sub rcx, 1
    jne idt_gate_loop

    mov r12, r11
    add r12, 1040
    mov r13, vector40_handler
    call write_idt_gate

    mov r12, r11
    add r12, 1056
    mov r13, vector41_handler
    call write_idt_gate

    mov r12, r11
    add r12, 1072
    mov r13, vector42_handler
    call write_idt_gate

    mov [r10 + 0], r11
    mov [r10 + 8], 4112
    mov rax, r10
    pop r15
    pop r14
    pop r13
    pop r12
    ret

write_idt_gate:
    mov rax, r13
    mov [r12], ax
    mov ax, 8
    mov [r12 + 2], ax
    mov ax, 0
    mov [r12 + 4], ax
    mov al, 0x8E
    mov [r12 + 5], al
    mov rax, r13
    shr rax, 16
    mov [r12 + 6], ax
    mov rax, r13
    shr rax, 32
    mov [r12 + 8], eax
    mov rax, 0
    mov [r12 + 12], eax
    ret
}
```

The implementation still allocates 4112 bytes: 16-byte IDTR-like descriptor plus 256 16-byte gates. Vector gate offsets are `16 + vector*16`, so `0x40`, `0x41`, and `0x42` patch offsets `1040`, `1056`, and `1072`.

- [ ] **Step 2:** Update transition source:

```wrela
let idt = self.delegated_memory.build_interrupt_idt(
    fatal_handler = self.capture_fatal_idt_handler(),
    vector40_handler = self.capture_vector40_serial_handler(),
    vector41_handler = self.capture_vector41_edu_msi_handler(),
    vector42_handler = self.capture_vector42_ivshmem_msix_handler()
)
```

- [ ] **Step 3:** Update every `build_fatal_idt` call site.

```sh
rg -n 'build_fatal_idt|build_interrupt_idt' wrela compiler
```

Expected after this step: no remaining `build_fatal_idt(` calls outside deleted or historical docs; `wrela/platform/uefi/transition.wrela` calls `build_interrupt_idt(...)`.

- [ ] **Step 4:** Add capture methods. The `call` pushes the next instruction address; the `pop rax` after the handler label returns that pushed address to Wrela as a `VirtualAddress`. The interrupt handler block itself is not executed during capture. The handler labels must `jmp` to generated dispatch stubs, not `call`; the dispatch stub owns the final `iretq`.

```wrela
asm fn capture_vector40_serial_handler(self) -> VirtualAddress {
    // Capture the address of vector40_serial_handler without executing it.
    call vector40_capture_return
vector40_serial_handler:
    jmp _wrela_interrupt_vector40_serial
vector40_capture_return:
    pop rax
    ret
}

asm fn capture_vector41_edu_msi_handler(self) -> VirtualAddress {
    // Capture the address of vector41_edu_msi_handler without executing it.
    call vector41_capture_return
vector41_edu_msi_handler:
    jmp _wrela_interrupt_vector41_edu_msi
vector41_capture_return:
    pop rax
    ret
}

asm fn capture_vector42_ivshmem_msix_handler(self) -> VirtualAddress {
    // Capture the address of vector42_ivshmem_msix_handler without executing it.
    call vector42_capture_return
vector42_ivshmem_msix_handler:
    jmp _wrela_interrupt_vector42_ivshmem_msix
vector42_capture_return:
    pop rax
    ret
}
```

- [ ] **Step 5:** Confirm Task 14A is complete by running:

```sh
go test ./compiler/codegen -run AsmMethodExternalBranchRelocation
```

Expected: `PASS`.

- [ ] **Step 6:** Add codegen test asserting the IDT builder writes the fixed gate offsets for vectors `0x40`, `0x41`, and `0x42` from their specific handler parameters, and transition source calls `build_interrupt_idt`.

```go
func TestInterruptIDTSourceShape(t *testing.T) {
    checked := parseCheckedUEFIModules(t)
    build := asmMethodFromSem(t, checked, "platform.uefi.types", "DelegatedMemory", "build_interrupt_idt")
    for _, want := range []string{
        "1040", "1056", "1072",
        "vector40_handler", "vector41_handler", "vector42_handler",
    } {
        if !strings.Contains(build.Body, want) {
            t.Fatalf("build_interrupt_idt missing %s:\n%s", want, build.Body)
        }
    }

    transition := methodFromSem(t, checked, "platform.uefi.transition", "DelegatedHardware", "exit_to_owned_hardware")
    if transition == nil {
        t.Fatalf("missing exit_to_owned_hardware")
    }
    if !stmtListContainsCall(transition.Body, "build_interrupt_idt") {
        t.Fatalf("exit_to_owned_hardware must call build_interrupt_idt")
    }
}
```

If `methodFromSem` or `stmtListContainsCall` do not exist, add them next to `asmMethodFromSem` in `compiler/codegen/uefi_source_codegen_test.go`:

```go
func methodFromSem(t *testing.T, checked *sem.CheckedProgram, moduleName, typeName, methodName string) *sem.Method {
    t.Helper()
    typ, ok := checked.Index.Lookup(moduleName, typeName)
    if !ok || typ == nil {
        t.Fatalf("missing type %s.%s", moduleName, typeName)
    }
    for i := range typ.Methods {
        if typ.Methods[i].Name == methodName {
            return &typ.Methods[i]
        }
    }
    return nil
}

func stmtListContainsCall(stmts []ast.Stmt, method string) bool {
    for _, stmt := range stmts {
        if expr, ok := stmt.(*ast.ExprStmt); ok {
            if call, ok := expr.Expr.(*ast.CallExpr); ok && call.Method == method {
                return true
            }
        }
        if let, ok := stmt.(*ast.LetStmt); ok {
            if call, ok := let.Expr.(*ast.CallExpr); ok && call.Method == method {
                return true
            }
        }
    }
    return false
}
```
- [ ] **Step 7:** Run `go test ./compiler/codegen -run 'IDT|Interrupt'`; expect failure before implementation.
- [ ] **Step 8:** Implement only the source edits from Steps 1-4; assembler external-branch support belongs to Task 14A and must not be rediscovered here.
- [ ] **Step 9:** Run `go test ./compiler/codegen ./compiler/asm`; expect `PASS`.
- [ ] **Step 10:** Commit with `git commit -m "feat: add apic msi msix idt gates -Codex Automated"`.

**Acceptance Criteria:** The loaded IDT routes vectors `0x40`, `0x41`, and `0x42` to their generated dispatch symbols; all other vectors stay fatal.

---

### Task 15: Generate Serial, MSI, And MSI-X Dispatch Stubs

**Files:** `compiler/codegen/x64.go`, `compiler/codegen/interrupt_test.go`

**You may need to know:** Dispatch stubs are generated codegen units, not Wrela functions. Use `compiledUnit{Symbol, Bytes, CallReloc}` and let the existing `Compile` relocation pass patch `call rel32` targets.
Task 15 proves symbol generation, register preservation, EOI, and final `iretq`; the stubs are not runtime-correct until Task 16 adds context and event-return storage. Do not "fix" missing context setup in this task.

- [ ] **Step 1:** Add `interruptProgramForCodegenTest`.

```go
func interruptProgramForCodegenTest(t *testing.T) *ir.Program {
    t.Helper()
    eventByte := &ir.ConstInt{Symbol: "event_byte", Value: 0, Type: ir.Type{Name: "U8", Module: "builtin", Kind: ir.TypeKindPrimitive}}
    eventRet := &ir.Construct{
        Symbol: "event_value",
        Type:   ir.Type{Name: "SerialPathInterrupt", Module: "machine.x86_64.serial", Kind: ir.TypeKindData},
        Fields: []ir.FieldValue{{Name: "byte", Value: eventByte}},
    }
    eventFn := ir.Function{
        Symbol: "_wrela_event_fn_machine_x86_64_serial_SerialConsolePath_interrupt",
        Return: ir.Type{Name: "SerialPathInterrupt", Module: "machine.x86_64.serial", Kind: ir.TypeKindData},
        Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{eventByte, eventRet, &ir.Return{Value: eventRet}}}},
    }
    handlerFn := ir.Function{
        Symbol: "_wrela_on_fn_examples_hello_program_HelloWorld_serial_path_interrupt",
        Return: ir.Type{Name: "void", Module: "builtin", Kind: ir.TypeKindPrimitive},
        Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{&ir.Return{}}}},
    }
    return &ir.Program{
        Functions: []ir.Function{eventFn, handlerFn},
        Types: map[string]ir.TypeInfo{
            "SerialPathInterrupt": {
                Name:        "SerialPathInterrupt",
                Module:      "machine.x86_64.serial",
                Kind:        ir.TypeKindData,
                Size:        8,
                Align:       8,
                StorageSize: 8,
                Fields: map[string]ir.FieldInfo{
                    "byte": {
                        Name:          "byte",
                        Type:          ir.Type{Name: "U8", Module: "builtin", Kind: ir.TypeKindPrimitive},
                        Offset:        0,
                        StorageOffset: 0,
                        Size:          1,
                        StorageSize:   1,
                        Align:         1,
                    },
                },
                FieldOrder: []string{"byte"},
            },
            "HelloWorld": {
                Name:        "HelloWorld",
                Module:      "examples.hello.program",
                Kind:        ir.TypeKindExecutor,
                Size:        32,
                Align:       8,
                StorageSize: 32,
                Fields: map[string]ir.FieldInfo{
                    "serial_path": {
                        Name:          "serial_path",
                        Type:          ir.Type{Name: "SerialConsolePath", Module: "machine.x86_64.serial", Kind: ir.TypeKindDriverPath},
                        Offset:        16,
                        StorageOffset: -1,
                        Size:          16,
                        StorageSize:   0,
                        Align:         8,
                    },
                },
                FieldOrder: []string{"serial_path"},
            },
        },
        InterruptBindings: []ir.InterruptBinding{
            {
                EventSymbol:           "interrupt_event::machine.x86_64.serial::SerialConsolePath::interrupt",
                HandlerSymbol:         "on_handler::examples.hello.program::HelloWorld::serial_path::interrupt",
                EventFunctionSymbol:   eventFn.Symbol,
                HandlerFunctionSymbol: handlerFn.Symbol,
                ExecutorType:          ir.Type{Name: "HelloWorld", Module: "examples.hello.program", Kind: ir.TypeKindExecutor},
                PathField:             "serial_path",
                PathFieldOffset:       16,
                ContextSymbol:         "_wrela_interrupt_context_0",
                EventStorageSymbol:    "_wrela_interrupt_event_40",
                EventStorageSize:      8,
                Vector:                0x40,
            },
            {
                EventSymbol:           "interrupt_event::machine.x86_64.edu::EduMsiPath::interrupt",
                HandlerSymbol:         "on_handler::examples.hello.program::HelloWorld::edu_path::interrupt",
                EventFunctionSymbol:   eventFn.Symbol,
                HandlerFunctionSymbol: handlerFn.Symbol,
                ExecutorType:          ir.Type{Name: "HelloWorld", Module: "examples.hello.program", Kind: ir.TypeKindExecutor},
                PathField:             "edu_path",
                PathFieldOffset:       16,
                ContextSymbol:         "_wrela_interrupt_context_0",
                EventStorageSymbol:    "_wrela_interrupt_event_41",
                EventStorageSize:      8,
                Vector:                0x41,
            },
            {
                EventSymbol:           "interrupt_event::machine.x86_64.ivshmem::IvshmemMsixPath::interrupt",
                HandlerSymbol:         "on_handler::examples.hello.program::HelloWorld::ivshmem_rx::interrupt",
                EventFunctionSymbol:   eventFn.Symbol,
                HandlerFunctionSymbol: handlerFn.Symbol,
                ExecutorType:          ir.Type{Name: "HelloWorld", Module: "examples.hello.program", Kind: ir.TypeKindExecutor},
                PathField:             "ivshmem_rx",
                PathFieldOffset:       16,
                ContextSymbol:         "_wrela_interrupt_context_0",
                EventStorageSymbol:    "_wrela_interrupt_event_42",
                EventStorageSize:      8,
                Vector:                0x42,
            },
        },
        InterruptContexts: []ir.InterruptContext{{
            Symbol:       "_wrela_interrupt_context_0",
            ExecutorType: ir.Type{Name: "HelloWorld", Module: "examples.hello.program", Kind: ir.TypeKindExecutor},
            Size:         32,
            PathFields: []ir.InterruptContextPathField{{
                FieldName: "serial_path",
                Offset:    16,
                Type:      ir.Type{Name: "SerialConsolePath", Module: "machine.x86_64.serial", Kind: ir.TypeKindDriverPath},
            }},
        }},
    }
}
```

- [ ] **Step 2:** Add codegen test.

```go
func TestCompileGeneratesInterruptDispatchStubs(t *testing.T) {
    program := interruptProgramForCodegenTest(t)
    img, ds := Compile(program)
    if len(ds) != 0 {
        t.Fatalf("Compile diagnostics = %#v", ds)
    }
    for _, symbol := range []string{
        "_wrela_interrupt_vector40_serial",
        "_wrela_interrupt_vector41_edu_msi",
        "_wrela_interrupt_vector42_ivshmem_msix",
    } {
        if _, ok := img.Symbols[symbol]; !ok {
            t.Fatalf("missing %s symbol", symbol)
        }
        code := symbolBytes(t, img, symbol)
        if !containsBytes(code, []byte{0x48, 0xCF}) {
            t.Fatalf("%s missing iretq", symbol)
        }
    }
}
```

- [ ] **Step 3:** Run `go test ./compiler/codegen -run InterruptDispatchStubs`; expect failure.
- [ ] **Step 4:** Generate one dispatch stub per binding for vectors `0x40`, `0x41`, and `0x42`.

```go
func compileInterruptDispatchUnits(program *ir.Program) []compiledUnit {
    units := make([]compiledUnit, 0, len(program.InterruptBindings))
    for _, binding := range program.InterruptBindings {
        symbol := interruptVectorSymbol(binding.Vector)
        if symbol == "" {
            continue
        }
        unit := buildInterruptDispatchUnit(symbol, binding)
        units = append(units, unit)
    }
    return units
}

func interruptVectorSymbol(vector uint8) string {
    switch vector {
    case 0x40:
        return "_wrela_interrupt_vector40_serial"
    case 0x41:
        return "_wrela_interrupt_vector41_edu_msi"
    case 0x42:
        return "_wrela_interrupt_vector42_ivshmem_msix"
    default:
        return ""
    }
}
```

Append these units before laying out symbols:

```go
units = append(units, compileInterruptDispatchUnits(program)...)
```

- [ ] **Step 5:** Stub must save and restore these registers in this exact order:

```text
push rax, rcx, rdx, rbx, rbp, rsi, rdi, r8, r9, r10, r11, r12, r13, r14, r15
pop r15, r14, r13, r12, r11, r10, r9, r8, rdi, rsi, rbp, rbx, rdx, rcx, rax
```

- [ ] **Step 6:** Build the dispatch unit with call relocations to actual function symbols, not logical `::` metadata symbols. Move `rax` to `rsi` after the receiver call so the event handoff site is explicit. Task 16 adds the `r10` event-return slot and `rdi` context-address setup required for runtime correctness.

```go
func buildInterruptDispatchUnit(symbol string, binding ir.InterruptBinding) compiledUnit {
    e := &Emitter{Labels: map[string]int{}}
    for _, reg := range []string{"rax", "rcx", "rdx", "rbx", "rbp", "rsi", "rdi", "r8", "r9", "r10", "r11", "r12", "r13", "r14", "r15"} {
        e.emitInstruction(asm.Instruction{Mnemonic: "push", Operands: []asm.Operand{asm.RegOperand{Reg: asm.MustLookup(reg)}}})
    }
    emitCallReloc(e, binding.EventFunctionSymbol)
    e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{asm.RegOperand{Reg: asm.MustLookup("rsi")}, asm.RegOperand{Reg: asm.MustLookup("rax")}}})
    emitCallReloc(e, binding.HandlerFunctionSymbol)
    e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{asm.RegOperand{Reg: asm.MustLookup("r11")}, asm.ImmOperand{Value: 0xFEE00000}}})
    e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{asm.RegOperand{Reg: asm.MustLookup("eax")}, asm.ImmOperand{Value: 0}}})
    e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{asm.MemOperand{Base: asm.MustLookup("r11"), Disp: 0xB0, Width: 32}, asm.RegOperand{Reg: asm.MustLookup("eax")}}})
    for _, reg := range []string{"r15", "r14", "r13", "r12", "r11", "r10", "r9", "r8", "rdi", "rsi", "rbp", "rbx", "rdx", "rcx", "rax"} {
        e.emitInstruction(asm.Instruction{Mnemonic: "pop", Operands: []asm.Operand{asm.RegOperand{Reg: asm.MustLookup(reg)}}})
    }
    e.emitInstruction(asm.Instruction{Mnemonic: "iretq"})
    return compiledUnit{Symbol: symbol, Bytes: e.Code, CallReloc: e.CallReloc}
}

func emitCallReloc(e *Emitter, symbol string) {
    e.Code = append(e.Code, 0xE8, 0, 0, 0, 0)
    e.CallReloc = append(e.CallReloc, internalReloc{Offset: uint64(len(e.Code) - 4), Symbol: symbol})
}
```

- [ ] **Step 7:** Each stub must send Local APIC EOI:

```asm
mov r11, 0xFEE00000
mov eax, 0
mov [r11 + 0xB0], eax
```

- [ ] **Step 8:** Each stub must end with `iretq`.
- [ ] **Step 9:** Run `go test ./compiler/codegen`; expect `PASS`.
- [ ] **Step 10:** Commit with `git commit -m "feat: generate apic msi msix dispatch stubs -Codex Automated"`.

**Acceptance Criteria:** Codegen emits real dispatch symbols for IOAPIC, MSI, and MSI-X interrupt events and preserves interrupt metadata.

---

### Task 16: Wire Interrupt Context For The Active Executor

**Files:** `compiler/ir/lower.go`, `compiler/ir/lower_test.go`, `compiler/codegen/x64.go`, `compiler/codegen/interrupt_test.go`

**You may need to know:** Interrupt stubs run outside the owned-phase stack frame. They must use a global snapshot of the active executor record. Use `program.Types` field offsets; do not guess path offsets from source order.
The context size, the `InterruptContextStore.Size`, and the dispatch path-field offsets must all come from the same `typeInfoFor(program.Types, executorType)` `TypeInfo` record.

- [ ] **Step 1:** Add IR/codegen test asserting a global context symbol exists.

```go
func TestInterruptContextSymbolStoresActiveExecutor(t *testing.T) {
    program := interruptProgramForCodegenTest(t)
    img, ds := Compile(program)
    if len(ds) != 0 {
        t.Fatalf("Compile diagnostics = %#v", ds)
    }
    if _, ok := img.Symbols["_wrela_interrupt_context_0"]; !ok {
        t.Fatalf("missing interrupt context symbol")
    }
    data := sectionByName(img, ".data")
    if data == nil || data.Characteristics&0x80000000 == 0 || data.Characteristics&0x40000000 == 0 {
        t.Fatalf("interrupt context must live in writable .data section: %#v", data)
    }
    img2, ds := Compile(program)
    if len(ds) != 0 {
        t.Fatalf("second Compile diagnostics = %#v", ds)
    }
    data2 := sectionByName(img2, ".data")
    if len(program.WritableData) != 0 {
        t.Fatalf("Compile must not mutate Program.WritableData: %#v", program.WritableData)
    }
    if data2 == nil || len(data2.Data) != len(data.Data) {
        t.Fatalf("Compile must be idempotent for interrupt runtime data: first %d second %d", len(data.Data), len(data2.Data))
    }
}

func sectionByName(img *Image, name string) *Section {
    for i := range img.Sections {
        if img.Sections[i].Name == name {
            return &img.Sections[i]
        }
    }
    return nil
}
```

- [ ] **Step 2:** Add IR lowering test proving the context store is inserted before the executor `start fn` call.

```go
func TestLowerInsertsInterruptContextStoreBeforeExecutorStart(t *testing.T) {
    checked := checkedProgramForTest(t, `
module machine.x86_64.serial
data SerialPathInterrupt { byte: U8 }
driver path SerialConsolePath {
    interrupt receiver -> SerialPathInterrupt {
        return SerialPathInterrupt(byte = 0)
    }
}
executor HelloWorld {
    serial_path: SerialConsolePath
    on serial_path.interrupt(event: SerialPathInterrupt) { }
    start fn run(self) -> never {
        while true {
        }
    }
}
unique class DelegatedHardware {
    fn claim(self) -> OwnedHardware {
        return OwnedHardware()
    }
}
unique class OwnedHardware {}
image Img {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.claim()
    }
    phase owned_hardware(hardware: OwnedHardware) -> never {
        let hello = HelloWorld(serial_path = SerialConsolePath())
        hello.run()
    }
}`)
    program, ds := Lower(checked)
    if len(ds) != 0 {
        t.Fatalf("Lower diagnostics = %#v", ds)
    }
    phase := findFunctionForTest(program, "_wrela_phase_machine_x86_64_serial_Img_owned_hardware")
    storeIndex, callIndex := -1, -1
    seq := 0
    for _, block := range phase.Blocks {
        for _, op := range block.Ops {
            if _, ok := op.(*InterruptContextStore); ok && storeIndex < 0 {
                storeIndex = seq
            }
            if call, ok := op.(*Call); ok && call.Symbol == "_wrela_method_machine_x86_64_serial_HelloWorld_run" && callIndex < 0 {
                callIndex = seq
            }
            seq++
        }
    }
    if storeIndex < 0 || callIndex < 0 || storeIndex > callIndex {
        t.Fatalf("context store index %d must precede start call index %d in %#v", storeIndex, callIndex, phase.Blocks)
    }
}
```

- [ ] **Step 3:** Run `go test ./compiler/ir ./compiler/codegen -run InterruptContext`; expect failure.
- [ ] **Step 4:** During lowering, create one interrupt context for each executor type that owns interrupt bindings. This must run before image phases are lowered, because `lowerExpr` must see the context while lowering `ex.run()`.

```go
func (ctx *lowerContext) lowerInterruptContexts() {
    byExecutor := map[string][]int{}
    for i, binding := range ctx.program.InterruptBindings {
        key := binding.ExecutorType.Module + "." + binding.ExecutorType.Name
        byExecutor[key] = append(byExecutor[key], i)
    }
    keys := make([]string, 0, len(byExecutor))
    for key := range byExecutor {
        keys = append(keys, key)
    }
    sort.Strings(keys)
    seq := 0
    for _, key := range keys {
        bindingIndexes := byExecutor[key]
        executorType := ctx.program.InterruptBindings[bindingIndexes[0]].ExecutorType
        info, ok := typeInfoFor(ctx.program.Types, executorType)
        if !ok {
            ctx.errorf("missing type info for interrupt executor %s.%s", executorType.Module, executorType.Name)
            continue
        }
        context := InterruptContext{
            Symbol:       fmt.Sprintf("_wrela_interrupt_context_%d", seq),
            ExecutorType: executorType,
            Size:         info.StorageSize,
        }
        for _, index := range bindingIndexes {
            binding := ctx.program.InterruptBindings[index]
            ctx.program.InterruptBindings[index].ContextSymbol = context.Symbol
            field, ok := info.Fields[binding.PathField]
            if !ok {
                ctx.errorf("missing interrupt path field %s on executor %s.%s", binding.PathField, executorType.Module, executorType.Name)
                continue
            }
            context.PathFields = append(context.PathFields, InterruptContextPathField{
                FieldName: binding.PathField,
                Offset:    field.Offset,
                Type:      field.Type,
            })
        }
        ctx.program.InterruptContexts = append(ctx.program.InterruptContexts, context)
        seq++
    }
}
```

Reorder `Lower` so source methods and interrupt metadata are ready before image phases are lowered:

```go
ctx.lowerSourceMethods()
ctx.lowerInterruptEventsAndHandlers()
ctx.lowerInterruptContexts()
ctx.lowerImagePhases(imageModule, imageName, imageDecl, delegatedSymbol, ownedSymbol)
ctx.program.AsmMethods = append(ctx.program.AsmMethods, ctx.lowerAsmMethods()...)
```

Move the existing `if imageDecl != nil { for i := range imageDecl.Phases { ... } }` block into `lowerImagePhases`. Do not leave the old image-phase lowering block before `lowerInterruptContexts`, or the context-store insertion in Step 6 will never run.

- [ ] **Step 5a:** Extract `buildDataSection(name, objects, characteristics)` from the existing `.rdata` layout code and run `go test ./compiler/codegen -run DataRelocation`.

- [ ] **Step 5b:** Add `Program.WritableData` to the IR program struct and run `go test ./compiler/ir ./compiler/codegen -run '^$'`.

- [ ] **Step 5c:** Emit a writable `.data` PE section for caller-provided `Program.WritableData` and run `go test ./compiler/codegen -run InterruptContext`.

- [ ] **Step 5d:** Add interrupt runtime data through local slices only and rerun `go test ./compiler/codegen -run 'InterruptContext|DataRelocation'`.

During these four checkpoints, emit zeroed writable `.data` for each context symbol. Do not append interrupt contexts to `program.Data`, because that is emitted into read-only `.rdata`.

```go
func interruptRuntimeData(program *ir.Program) []ir.DataObject {
    out := make([]ir.DataObject, 0, len(program.InterruptContexts)+len(program.InterruptBindings))
    for _, context := range program.InterruptContexts {
        out = append(out, ir.DataObject{
            Symbol: context.Symbol,
            Bytes:  make([]byte, context.Size),
        })
    }
    for _, binding := range program.InterruptBindings {
        out = append(out, ir.DataObject{
            Symbol: binding.EventStorageSymbol,
            Bytes:  make([]byte, binding.EventStorageSize),
        })
    }
    return out
}
```

This function emits both executor context snapshots and per-binding event return slots without mutating `program`. Call it before section construction in `Compile`, then add a `.data` section builder for caller-provided `program.WritableData` plus the runtime data:

```go
func buildData(program *ir.Program) (Section, map[string]uint64) {
    writable := append([]ir.DataObject{}, program.WritableData...)
    writable = append(writable, interruptRuntimeData(program)...)
    return buildDataSection(".data", writable, 0xC0000040)
}
```

Extract the shared data-object layout code into `buildDataSection(name string, objects []ir.DataObject, characteristics uint32) (Section, map[string]uint64)` and call it from both `.rdata` and `.data`.

Update section layout so it no longer assumes there is only one data section. Make the builder return offsets:

```go
type builtDataSection struct {
    Section Section
    Offsets map[string]uint64
}

var dataSections []builtDataSection
if len(program.Data) > 0 {
    section, offsets := buildDataSection(".rdata", program.Data, 0x40000040)
    dataSections = append(dataSections, builtDataSection{Section: section, Offsets: offsets})
}
if len(program.WritableData) > 0 || len(program.InterruptContexts) > 0 || len(program.InterruptBindings) > 0 {
    section, offsets := buildData(program)
    dataSections = append(dataSections, builtDataSection{Section: section, Offsets: offsets})
}

alignedTextSize := alignUpLen(uint64(len(sections[0].Data)), 0x1000)
sections[0].Data = append(sections[0].Data, make([]byte, alignedTextSize-uint64(len(sections[0].Data)))...)
nextRVA := sections[0].RVA + alignedTextSize
for _, built := range dataSections {
    section := built.Section
    section.RVA = nextRVA
    for symbol, offset := range built.Offsets {
        symbols[symbol] = section.RVA + offset
    }
    sections = append(sections, section)
    nextRVA += alignUpLen(uint64(len(section.Data)), 0x1000)
}
```

The important fixed rule is that both `.rdata` and `.data` symbols must enter the shared `symbols` table before data relocations are patched.

- [ ] **Step 6:** Add an IR operation that stores the active executor record into `_wrela_interrupt_context_0` immediately before the executor `start fn` call.

Add the operation to `compiler/ir/ir.go` and include it in `valuesDefinedBy` as no defined values:

```go
type InterruptContextStore struct {
    ContextSymbol string
    Source        Value
    SourceType    Type
    Size           int
}

func (InterruptContextStore) isOperation() {}
```

```go
case *InterruptContextStore:
    return nil
```

In `lowerExpr` for `ast.CallExpr`, insert the store after the existing `method := ctx.lookupMethod(recvType, e.Method)` line and immediately before `ops = append(ops, call)` when the receiver type is an executor with an interrupt context and the method is a start method:

```go
if recvType.Kind == sem.KindExecutor && method != nil && method.IsStart {
    if context := ctx.interruptContextForExecutor(recvType); context != nil {
        ops = append(ops, &InterruptContextStore{
            ContextSymbol: context.Symbol,
            Source:        receiver,
            SourceType:    ctx.irType(recvType),
            Size:          context.Size,
        })
    }
}
ops = append(ops, call)
```

Add the lookup helper:

```go
func (ctx *lowerContext) interruptContextForExecutor(executorType *sem.Type) *InterruptContext {
    for i := range ctx.program.InterruptContexts {
        candidate := &ctx.program.InterruptContexts[i]
        if candidate.ExecutorType.Module == executorType.Module && candidate.ExecutorType.Name == executorType.Name {
            return candidate
        }
    }
    return nil
}
```

- [ ] **Step 7:** In codegen, emit the context store by copying from the source value address to `_wrela_interrupt_context_0`.

```go
case *ir.InterruptContextStore:
    emitInterruptContextStore(e, frame, v)
```

```go
func emitInterruptContextStore(e *Emitter, frame Frame, store *ir.InterruptContextStore) {
    srcBase, srcDisp, ok := emitValueAddress(e, frame, store.Source)
    if !ok {
        e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "cannot address interrupt context source"})
        return
    }
    emitMovDataAddressToReg(e, asm.MustLookup("rax"), store.ContextSymbol)
    e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{asm.RegOperand{Reg: asm.MustLookup("rdi")}, asm.RegOperand{Reg: asm.MustLookup("rax")}}})
    emitCopyBytes(e, asm.MustLookup("rdi"), 0, srcBase, srcDisp, store.Size)
}

func emitCopyBytes(e *Emitter, dstBase asm.Reg, dstDisp int64, srcBase asm.Reg, srcDisp int64, size int) {
    offset := 0
    for size-offset >= 8 {
        emitCopyWidth(e, dstBase, dstDisp+int64(offset), srcBase, srcDisp+int64(offset), 64, "rax")
        offset += 8
    }
    if size-offset >= 4 {
        emitCopyWidth(e, dstBase, dstDisp+int64(offset), srcBase, srcDisp+int64(offset), 32, "eax")
        offset += 4
    }
    if size-offset >= 2 {
        emitCopyWidth(e, dstBase, dstDisp+int64(offset), srcBase, srcDisp+int64(offset), 16, "ax")
        offset += 2
    }
    if size-offset == 1 {
        emitCopyWidth(e, dstBase, dstDisp+int64(offset), srcBase, srcDisp+int64(offset), 8, "al")
    }
}

func emitCopyWidth(e *Emitter, dstBase asm.Reg, dstDisp int64, srcBase asm.Reg, srcDisp int64, width int, regName string) {
    reg := asm.MustLookup(regName)
    e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{asm.RegOperand{Reg: reg}, asm.MemOperand{Base: srcBase, Disp: srcDisp, Width: width}}})
    e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{asm.MemOperand{Base: dstBase, Disp: dstDisp, Width: width}, asm.RegOperand{Reg: reg}}})
}
```

- [ ] **Step 8:** Each vector stub must load its executor from `_wrela_interrupt_context_0` before calling the handler.

```go
emitMovDataAddressToReg(e, asm.MustLookup("rax"), binding.ContextSymbol)
e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{asm.RegOperand{Reg: asm.MustLookup("rdi")}, asm.MemOperand{Base: asm.MustLookup("rax"), Disp: int64(binding.PathFieldOffset), Width: 64}}})
emitMovDataAddressToReg(e, asm.MustLookup("rax"), binding.EventStorageSymbol)
e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{asm.RegOperand{Reg: asm.MustLookup("r10")}, asm.RegOperand{Reg: asm.MustLookup("rax")}}})
emitCallReloc(e, binding.EventFunctionSymbol)
e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{asm.RegOperand{Reg: asm.MustLookup("rsi")}, asm.RegOperand{Reg: asm.MustLookup("rax")}}})
emitMovDataAddressToReg(e, asm.MustLookup("rax"), binding.ContextSymbol)
e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{asm.RegOperand{Reg: asm.MustLookup("rdi")}, asm.RegOperand{Reg: asm.MustLookup("rax")}}})
emitCallReloc(e, binding.HandlerFunctionSymbol)
```

`rdi` is the receiver pointer convention for this backend. The event function receives the driver-path handle loaded from the active executor context, and `r10` points to the writable event return slot required by the existing Wrela data-return ABI. The event function returns the event object address in `rax`, the stub moves that address to `rsi`, and the handler receives the executor context address in `rdi` plus the event object address in `rsi`.

- [ ] **Step 9:** Add a byte-level test that the dispatch stub has data relocations to `_wrela_interrupt_context_0` and `_wrela_interrupt_event_40`. Add `encoding/binary` to the test imports.

```go
func TestInterruptDispatchUsesContextRelocation(t *testing.T) {
    img, ds := Compile(interruptProgramForCodegenTest(t))
    if len(ds) != 0 {
        t.Fatalf("Compile diagnostics = %#v", ds)
    }
    found := map[string]bool{}
    for _, rel := range img.Relocs {
        if rel.Symbol != "_wrela_interrupt_vector40_serial" {
            continue
        }
        locationRVA := img.Symbols[rel.Symbol] + rel.Offset
        start := int(locationRVA - img.Sections[0].RVA)
        if start < 0 || start+8 > len(img.Sections[0].Data) {
            t.Fatalf("relocation outside .text: %#v", rel)
        }
        got := binary.LittleEndian.Uint64(img.Sections[0].Data[start : start+8])
        for _, target := range []string{"_wrela_interrupt_context_0", "_wrela_interrupt_event_40"} {
            if got == uint64(runtimeImageBase+img.Symbols[target]) {
                found[target] = true
            }
        }
    }
    if !found["_wrela_interrupt_context_0"] || !found["_wrela_interrupt_event_40"] {
        t.Fatalf("missing context/event relocation: found %#v relocs %#v", found, img.Relocs)
    }
}
```

- [ ] **Step 10:** Run `go test ./compiler/ir ./compiler/codegen`; expect `PASS`.
- [ ] **Step 11:** Commit with `git commit -m "feat: store active executor interrupt context -Codex Automated"`.

**Acceptance Criteria:** Interrupt dispatch does not depend on stale owned-phase stack locals.

---

### Task 17: Add QEMU Serial Input, EDU, And ivshmem-doorbell Support

**Files:** `compiler/qemu/run.go`, `compiler/qemu/run_test.go`

**You may need to know:** QEMU documents the server command as `ivshmem-server -p pidfile -S path -m shm-name -l shm-size -n vectors`, so the lowercase `-m` in this task is intentional. The two `-chardev socket,path=...` entries are two independent client connections to the same server socket; QEMU assigns them separate ivshmem peer IDs.

- [ ] **Step 1:** Add test.

```go
func TestRunWritesInputTextToQEMUStdin(t *testing.T) {
    tmp := t.TempDir()
    seen := filepath.Join(tmp, "stdin.txt")
    fakeQEMU := filepath.Join(tmp, "fake-qemu.sh")
    script := "#!/usr/bin/env sh\ncat > " + seen + "\necho 'serial interrupt: !'\n"
    if err := os.WriteFile(fakeQEMU, []byte(script), 0o755); err != nil {
        t.Fatalf("write fake qemu: %v", err)
    }
    image := filepath.Join(tmp, "hello.efi")
    if err := os.WriteFile(image, []byte("efi"), 0o644); err != nil {
        t.Fatalf("write image: %v", err)
    }
    out, err := Run(Options{
        QEMUBinary: fakeQEMU,
        OVMFCode: filepath.Join(tmp, "code.fd"),
        OVMFVars: filepath.Join(tmp, "vars.fd"),
        ESPDir: filepath.Join(tmp, "esp"),
        ImagePath: image,
        InputText: "!",
        SuccessText: "serial interrupt: !",
    })
    if err != nil {
        t.Fatalf("Run error = %v, output:\n%s", err, out)
    }
    data, err := os.ReadFile(seen)
    if err != nil {
        t.Fatalf("read stdin capture: %v", err)
    }
    if string(data) != "!" {
        t.Fatalf("stdin = %q, want !", data)
    }
}
```

- [ ] **Step 2:** Add QEMU args test for EDU and ivshmem.

```go
func TestArgsAddsEduAndIvshmemDevices(t *testing.T) {
    got := strings.Join(Args(Options{
        OVMFCode: "/code.fd",
        OVMFVars: "/vars.fd",
        ESPDir: "esp",
        EnableEdu: true,
        EnableIvshmemMsix: true,
        IvshmemSocketPath: "/tmp/ivshmem.sock",
    }), " ")
    for _, want := range []string{
        "-device edu,addr=0x5",
        "-chardev socket,path=/tmp/ivshmem.sock,id=ivshmem0",
        "-chardev socket,path=/tmp/ivshmem.sock,id=ivshmem1",
        "-device ivshmem-doorbell,vectors=1,chardev=ivshmem0,addr=0x6",
        "-device ivshmem-doorbell,vectors=1,chardev=ivshmem1,addr=0x7",
    } {
        if !strings.Contains(got, want) {
            t.Fatalf("QEMU args missing %q:\n%s", want, got)
        }
    }
}
```

- [ ] **Step 3:** Add ivshmem-server command-construction test.

```go
func TestIvshmemServerArgs(t *testing.T) {
    got := IvshmemServerArgs(IvshmemServerOptions{
        SocketPath: "/tmp/ivshmem.sock",
        PidPath: "/tmp/ivshmem.pid",
        ShmName: "wrela-ivshmem",
        Size: "1M",
        Vectors: 1,
    })
    want := []string{"-S", "/tmp/ivshmem.sock", "-p", "/tmp/ivshmem.pid", "-m", "wrela-ivshmem", "-l", "1M", "-n", "1"}
    if !reflect.DeepEqual(got, want) {
        t.Fatalf("IvshmemServerArgs() = %#v, want %#v", got, want)
    }
}
```

- [ ] **Step 4:** Add ivshmem startup wait/cleanup test. Add `net`, `strconv`, and `syscall` to `compiler/qemu/run_test.go` imports if they are not already present; this test also uses existing `os`, `path/filepath`, `strings`, and `time` imports.

```go
func TestRunDoesNotStartQEMUWhenIvshmemServerSocketIsMissing(t *testing.T) {
    tmp := t.TempDir()
    fakeServer := filepath.Join(tmp, "fake-ivshmem-server.sh")
    pidFile := filepath.Join(tmp, "server.pid")
    serverScript := "#!/usr/bin/env sh\necho $$ > " + pidFile + "\nsleep 30\n"
    if err := os.WriteFile(fakeServer, []byte(serverScript), 0o755); err != nil {
        t.Fatalf("write fake server: %v", err)
    }
    qemuRan := filepath.Join(tmp, "qemu-ran")
    fakeQEMU := filepath.Join(tmp, "fake-qemu.sh")
    qemuScript := "#!/usr/bin/env sh\ntouch " + qemuRan + "\n"
    if err := os.WriteFile(fakeQEMU, []byte(qemuScript), 0o755); err != nil {
        t.Fatalf("write fake qemu: %v", err)
    }
    image := filepath.Join(tmp, "hello.efi")
    if err := os.WriteFile(image, []byte("efi"), 0o644); err != nil {
        t.Fatalf("write image: %v", err)
    }

    _, err := Run(Options{
        QEMUBinary: fakeQEMU,
        OVMFCode: filepath.Join(tmp, "code.fd"),
        OVMFVars: filepath.Join(tmp, "vars.fd"),
        ESPDir: filepath.Join(tmp, "esp"),
        ImagePath: image,
        EnableIvshmemMsix: true,
        IvshmemServerBinary: fakeServer,
        IvshmemSocketPath: filepath.Join(tmp, "missing.sock"),
        IvshmemStartupTimeout: 20 * time.Millisecond,
    })
    if err == nil {
        t.Fatalf("expected ivshmem startup error")
    }
    if _, statErr := os.Stat(qemuRan); !os.IsNotExist(statErr) {
        t.Fatalf("QEMU must not start before ivshmem socket is ready")
    }
    rawPID, readErr := os.ReadFile(pidFile)
    if readErr == nil {
        pid, _ := strconv.Atoi(strings.TrimSpace(string(rawPID)))
        if processRunning(pid) {
            t.Fatalf("ivshmem server pid %d still running after startup failure", pid)
        }
    }
}

func processRunning(pid int) bool {
    if pid <= 0 {
        return false
    }
    proc, err := os.FindProcess(pid)
    if err != nil {
        return false
    }
    return proc.Signal(syscall.Signal(0)) == nil
}
```

- [ ] **Step 5:** Run `go test ./compiler/qemu -run 'InputText|EduAndIvshmem|IvshmemServer|IvshmemServerSocket'`; expect failure.
- [ ] **Step 6:** Add `InputText`, `EnableEdu`, `EnableIvshmemMsix`, `IvshmemServerBinary`, `IvshmemSocketPath`, and `IvshmemStartupTimeout` to `qemu.Options`.
- [ ] **Step 7:** In `Run`, set `cmd.Stdin = strings.NewReader(opts.InputText)` when non-empty.
- [ ] **Step 8:** In `Args`, append `-device edu,addr=0x5` when `EnableEdu` is true.
- [ ] **Step 9:** Add a socket wait helper.

```go
func waitForUnixSocket(path string, timeout time.Duration) error {
    if timeout == 0 {
        timeout = 2 * time.Second
    }
    deadline := time.Now().Add(timeout)
    for time.Now().Before(deadline) {
        conn, err := net.DialTimeout("unix", path, 20*time.Millisecond)
        if err == nil {
            _ = conn.Close()
            return nil
        }
        time.Sleep(10 * time.Millisecond)
    }
    return fmt.Errorf("timed out waiting for ivshmem socket %s", path)
}
```

- [ ] **Step 10:** In `Run`, when `EnableIvshmemMsix` is true, start `ivshmem-server`, wait for `IvshmemSocketPath` with `waitForUnixSocket`, and kill/wait the server process if the socket never becomes ready. Use `IvshmemServerArgs` exactly as tested.
- [ ] **Step 11:** In `Args`, append two socket chardevs and two ivshmem-doorbell devices when `EnableIvshmemMsix` is true.
- [ ] **Step 12:** Run `go test ./compiler/qemu`; expect `PASS`.
- [ ] **Step 13:** Commit with `git commit -m "feat: add qemu edu and ivshmem interrupt devices -Codex Automated"`.

**Acceptance Criteria:** E2E tests can send a byte to COM1, instantiate EDU for MSI, and instantiate two ivshmem-doorbell devices for MSI-X.

---

### Task 18: Add IOAPIC, MSI, And MSI-X QEMU E2E Test

**Files:** `tests/e2e/hello_qemu_test.go`

- [ ] **Step 1:** Extend the existing e2e test or add a second test.

```go
func TestHelloInterruptsQEMU(t *testing.T) {
    qemuBin, err := exec.LookPath("qemu-system-x86_64")
    if err != nil {
        t.Skipf("qemu-system-x86_64 not found in PATH: %v", err)
    }
    ivshmemBin, err := exec.LookPath(ivshmemServer)
    if err != nil {
        t.Skipf("%s not found in PATH: %v", ivshmemServer, err)
    }
    firmware, err := qemu.ResolveFirmware(qemuBin)
    if err != nil {
        t.Skipf("resolve QEMU firmware: %v", err)
    }

    tmp := t.TempDir()
    vars := filepath.Join(tmp, "OVMF_VARS.fd")
    copyFile(t, firmware.Vars, vars)
    image := filepath.Join(tmp, "hello-interrupt.efi")
    _, err = compiler.Build(compiler.BuildOptions{
        Mode: compiler.ModeDev,
        RootPath: "examples/hello/main.wrela",
        OutputPath: image,
        RepoRoot: ".",
    })
    if err != nil {
        t.Fatalf("build hello image: %v", err)
    }

    out, err := qemu.Run(qemu.Options{
        QEMUBinary: qemuBin,
        OVMFCode: firmware.Code,
        OVMFVars: vars,
        ESPDir: filepath.Join(tmp, "esp"),
        ImagePath: image,
        InputText: "!",
        SuccessText: "msix interrupt",
        Timeout: 20 * time.Second,
        EnableEdu: true,
        EnableIvshmemMsix: true,
        IvshmemServerBinary: ivshmemBin,
    })
    if err != nil {
        t.Fatalf("qemu failed: %v\nserial output:\n%s", err, out)
    }
    for _, want := range []string{"hello from wrela", "serial interrupt: !", "msi interrupt", "msix interrupt"} {
        if !strings.Contains(out, want) {
            t.Fatalf("serial output missing %q:\n%s", want, out)
        }
    }
}
```

- [ ] **Step 2:** Run `go test ./tests/e2e -run HelloInterrupts -v`; expect failure before runtime tasks are complete.
- [ ] **Step 3:** Ensure the original `TestHelloQEMU` still passes using the same extended hello image.
- [ ] **Step 4:** Run `go test ./tests/e2e -v`; expect `PASS` on machines with QEMU, OVMF, and `ivshmem-server`; expect `SKIP` for this test when those local dependencies are missing.
- [ ] **Step 5:** Commit with `git commit -m "test: add ioapic msi msix qemu e2e -Codex Automated"`.

**Acceptance Criteria:** QEMU proves the serial IOAPIC handler, EDU MSI handler, and ivshmem MSI-X handler all write their expected serial lines.

---

### Task 19: Update Deferred Work And Design Docs

**Files:** `docs/production-deferred-work.md`, `docs/design/hello_world_design_doc.md`

- [ ] **Step 1:** Update CPU and interrupts deferred work:

```markdown
The current interrupt implementation proves three QEMU lab paths: COM1 receive through IOAPIC, EDU through MSI, and ivshmem-doorbell through MSI-X. Wrela source still exposes no CPU traps. Production work remains for ACPI discovery, PCI enumeration, shared interrupts, timers, interrupt queues, x2APIC, and multiprocessor routing.
```

- [ ] **Step 2:** Update the hello-world design doc to show the new source shape:

```wrela
on serial_path.interrupt(event: SerialPathInterrupt) {
    self.serial_path.write_byte(value = event.byte)
}
```

- [ ] **Step 3:** Run `git diff --check`; expect `PASS`.
- [ ] **Step 4:** Commit with `git commit -m "docs: document serial interrupt event model -Codex Automated"`.

**Acceptance Criteria:** Docs distinguish the proven IOAPIC/MSI/MSI-X lab paths from production interrupt routing.

---

### Task 20: Final Verification Sweep

**Files:** all touched files

- [ ] **Step 1:** Run frontend and semantic tests:

```sh
go test ./compiler/lex ./compiler/ast ./compiler/parse ./compiler/sem ./compiler/ir
```

Expected: `PASS`.

- [ ] **Step 2:** Run backend and build tests:

```sh
go test ./compiler/asm ./compiler/codegen ./compiler/pecoff ./compiler/qemu ./compiler
```

Expected: `PASS`.

- [ ] **Step 3:** Run full tests:

```sh
go test ./...
```

Expected: `PASS`. The interrupt QEMU e2e test may report `SKIP` when QEMU, OVMF, or `ivshmem-server` is missing.

- [ ] **Step 4:** Search for old syntax:

```sh
rg -n "interrupts\\.bind|interrupt handler|interrupt [A-Za-z_][A-Za-z0-9_]*:\\s*[A-Za-z_][A-Za-z0-9_]*(\\s+using)?|on [A-Za-z_][A-Za-z0-9_]*\\.[A-Za-z_][A-Za-z0-9_]*\\([A-Za-z_][A-Za-z0-9_]*\\)" compiler wrela examples docs --glob '!compiler/**/*_test.go' --glob '!docs/implementation/2026-05-14-*'
rg -n '[A-Za-z_][A-Za-z0-9_]*\([^)\n]*[A-Za-z_][A-Za-z0-9_]*:' wrela examples tests --glob '*.wrela'
```

Expected: first command has no production source or docs recommending the old bind/decoder model, old `interrupt name: Type` syntax, or untyped `on path.event(event)` handlers. Negative fixtures and parser tests may still contain intentionally rejected syntax. The second command may show declarations with typed parameters; it must not show constructor or method-call named arguments.

- [ ] **Step 5:** Run `git diff --check`; expect `PASS`.
- [ ] **Step 6:** Commit with `git commit -m "chore: verify serial interrupt event implementation -Codex Automated"`.

**Acceptance Criteria:** The repo uses the unified event/handler syntax, tests pass, and IOAPIC, MSI, and MSI-X interrupt e2e coverage exists.

---

## Appendix A: Binding Derivation Algorithm

For each executor declaration:

1. Build a map of direct fields.
2. For every field whose type is `driver path`, load that path type's `InterruptEvents`.
3. For each receiver, require exactly one `OnHandlerDecl` where `PathField == field.Name`.
4. Resolve the handler parameter type and require it to exactly match the event declaration return type.
5. Type-check the handler body with:
   - `self` bound to the executor type
   - the handler parameter bound to the resolved handler parameter type
6. Append one internal semantic `InterruptBinding`.
7. If the binding is unreachable from the image graph, do not emit a runtime diagnostic and do not expose it through `CheckedProgram.InterruptBindings`.
8. For each reachable binding, assign vectors by supported runtime shape and expose it through `CheckedProgram.InterruptBindings`:
   - `machine.x86_64.serial.SerialConsolePath.interrupt` -> `0x40`
   - `machine.x86_64.edu.EduMsiPath.interrupt` -> `0x41`
   - `machine.x86_64.ivshmem.IvshmemMsixPath.interrupt` -> `0x42`
9. Otherwise emit `SEM0020`.

Rejected shapes:

```wrela
on missing.interrupt(event: SerialPathInterrupt) {}
on serial_path.missing(event: SerialPathInterrupt) {}
on serial_path.interrupt(event: OtherInterrupt) {}
hello.interrupts.bind(hello.serial_path.interrupt, hello.on_serial)
```

---

## Appendix B: Direct Interrupt Handler Restrictions

Because v1 delivery is direct from generated vector stubs, `on` handlers may:

- read the event parameter
- access `self`
- call normal methods on executor-owned fields
- call serial write and ack methods
- construct small data values

The loop ban is intentionally non-transitive in this lab plan: the handler body itself may not contain `while` or `for`, but it may call the existing serial write helpers for observable output even though those helpers poll internally.

`on` handlers may not:

- call `halt_forever`
- call `enable_cpu_interrupts`
- call `initialize_for_com1_receive`
- call `configure_edu_msi_vector41`
- call `configure_ivshmem_msix_vector42`
- contain `while` or `for`
- construct `driver`, `driver path`, `executor`, or `unique class` values
- call another `on` handler

These restrictions are semantic errors with `SEM0016`.

---

## Appendix C: Runtime Byte-Level Facts

APIC addresses:

```text
Local APIC base: 0xFEE00000
Local APIC SVR:  base + 0xF0
Local APIC EOI:  base + 0xB0
IOAPIC base:     0xFEC00000
IOAPIC selector: base + 0x00
IOAPIC window:   base + 0x10
```

IOAPIC COM1 route:

```text
GSI: 4
IOREDTBL low selector:  0x10 + 2*4 = 0x18
IOREDTBL high selector: 0x19
Low dword:  0x00000040
High dword: 0x00000000
```

COM1 ports:

```text
base: 0x3F8
RBR/THR: base + 0
IER:     base + 1
FCR:     base + 2
LCR:     base + 3
MCR:     base + 4
LSR:     base + 5
```

COM1 receive interrupt enable:

```text
IER = 0x01
MCR = 0x0B
```

MSI and MSI-X messages:

```text
Message address for Local APIC ID 0: 0xFEE00000
EDU MSI message data:              0x41
ivshmem MSI-X message data:        0x42
```

MSI-X table entry layout:

```text
entry + 0x00: message address low
entry + 0x04: message address high
entry + 0x08: message data
entry + 0x0C: vector control, bit 0 masks when set
```

Local APIC EOI:

```asm
mov r11, 0xFEE00000
mov eax, 0
mov [r11 + 0xB0], eax
```
