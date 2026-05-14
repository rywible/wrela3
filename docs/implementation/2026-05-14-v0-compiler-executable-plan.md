# Wrela V0 Compiler Executable Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the first Go implementation of the Wrela compiler: one root Wrela source file becomes one x86_64-v3 UEFI `.efi` image that performs a real delegated-to-owned transition, starts one executor, writes `hello from wrela` to COM1 through a driver path, and halts.

**Architecture:** This plan supersedes `docs/superpowers/plans/2026-05-13-v0-compiler-implementation-plan.md` as the executable plan. The previous file remains useful as design background; this file is split into small TDD tasks with explicit contracts, concrete code, acceptance criteria, expected command output, and appendices for x86 encoding, PE32+ EFI layout, system tables, Microsoft x64 ABI bridging, and Wrela record layout.

**Tech Stack:** Go 1.22+; hand-written lexer/parser; direct x86_64-v3 assembler/encoder; direct PE32+ EFI writer; QEMU q35 + OVMF; Wrela platform source under `wrela/`; Go tests and Wrela fixtures under `tests/`.

---

## 0. How To Execute This Plan

This is a production-grade compiler plan, but the work is still intentionally split into small pieces.

For junior execution:

- Follow tasks in numeric order unless a tech lead explicitly assigns independent streams.
- Do not change public contracts without updating every dependent task in this file first.
- Each task ends in a commit step. Use the exact commit message format shown.
- Run the verification commands exactly as written.
- If a command output differs, stop and investigate before continuing.

For parallel execution:

- The tech lead owns contract arbitration.
- Safe parallel streams are listed in Section 3.
- If a task depends on a package contract that changes, the contract owner must update this plan before dependent workers proceed.

Definition of done for a task:

- All checkbox steps are complete.
- The expected failing test fails before implementation.
- The expected passing test passes after implementation.
- `git diff --check` passes.
- A commit is created with a message ending in `-Codex Automated`.

---

## 1. Frozen V0 Decisions

Do not reopen these decisions during task execution.

- Compiler implementation language: Go.
- Target: `x86_64-v3-uefi`.
- Output: direct PE32+ `.efi`; no external assembler and no external linker.
- CLI: `wrela build --mode dev <root.wrela> -o <out.efi>`.
- `--mode` is required.
- `--mode release` fails with `CLI0002`.
- Root file import closure is the entire universe.
- Module names resolve against two default import roots in order: `repo_root`, then `repo_root/wrela`.
- No manifest, TOML file, hidden package scan, or implicit dependency sweep.
- One compile has exactly one image and emits exactly one `.efi`.
- Image phases exist only on `image` in v0.
- Exactly one transition exists: `delegated_hardware -> owned_hardware`.
- `phase delegated_hardware` receives `DelegatedHardware` and returns `OwnedHardware`.
- `phase owned_hardware` receives `OwnedHardware` and returns `never`.
- The generated UEFI entry adapter supplies the initial `DelegatedHardware`; Wrela source does not construct it directly.
- The generated UEFI entry adapter is outside the source-level constructor graph and creates the initial delegated authority exactly once.
- The ownership-transfer method calls UEFI `ExitBootServices` source-visibly.
- `OwnedHardware` can only be minted through ownership-transfer authority.
- `class`, `driver`, `driver path`, and `executor` construction is syntactically visible in image phase bodies only.
- Source helper methods may not hide `class`, `driver`, `driver path`, or `executor` construction.
- `data` construction is free in normal code.
- `unique` means only one live instance of that declaration type can exist in the image/call graph.
- `driver path` is delegated capability, not globally unique.
- Assembly is a method body kind on edge-capability declarations; no top-level asm functions.
- V0 edge-capability declarations are `driver`, `driver path`, `ExecutorMemory`, and classes in `arch.*`, `platform.*`, or `machine.x86_64.serial`.
- Assembly uses Wrela-bound symbolic operands and is encoded by the Go compiler.
- Executor memory is stable-address arena memory; no GC, no free, no compaction.
- V0 uses identity-mapped owned-hardware virtual memory, but `PhysicalAddress` and `VirtualAddress` remain distinct types.

Lexer vocabulary:

```text
keywords: module, use, from, data, class, unique, driver, path, executor, image, transitions, phase, fn, asm, start, let, return, if, else, while, for, in, true, false, never
phase names: delegated_hardware and owned_hardware are identifiers, not keywords
single-character punctuation: { } ( ) : , . + - * / % < > = ! & | ^ [ ]
multi-character operators: -> == != <= >= << >>
comments: // line comments
strings: double-quoted UTF-8 source text, emitted as bytes plus trailing zero
integers: decimal or 0x-prefixed hexadecimal, non-negative in source
```

---

## 2. Repository Layout And Package Contracts

Create exactly this layout. Each package owns the contract listed here.

```text
cmd/wrela/main.go
compiler/build.go
compiler/errors.go
compiler/mode.go
compiler/integration_test.go

compiler/source/file.go
compiler/source/graph.go

compiler/diag/codes.go
compiler/diag/diag.go
compiler/diag/render.go

compiler/lex/token.go
compiler/lex/lexer.go

compiler/ast/ast.go
compiler/ast/walk.go

compiler/parse/parser.go
compiler/parse/expr.go

compiler/sem/symbols.go
compiler/sem/types.go
compiler/sem/check.go
compiler/sem/authority.go
compiler/sem/image_graph.go

compiler/layout/record.go

compiler/ir/ir.go
compiler/ir/lower.go

compiler/asm/regs.go
compiler/asm/ast.go
compiler/asm/parse.go
compiler/asm/encode.go

compiler/codegen/program.go
compiler/codegen/abi.go
compiler/codegen/x64.go
compiler/codegen/asm_method.go
compiler/codegen/entry_adapter.go
compiler/codegen/entry_adapter_test.go

compiler/pecoff/image.go
compiler/pecoff/write.go
compiler/pecoff/reloc.go

compiler/qemu/run.go
compiler/qemu/run_test.go

wrela/platform/uefi/*.wrela
wrela/machine/x86_64/*.wrela
wrela/arch/x86_64/*.wrela
examples/hello/*.wrela
cmd/wrela/main_test.go
compiler/negative_fixtures_test.go
tests/fixtures/negative/*.wrela
tests/e2e/hello_qemu_test.go
scripts/run-hello-qemu.sh
docs/production-deferred-work.md
```

### Cross-Package Type Contracts

These names are fixed.

```go
// compiler/errors.go
package compiler

type CodeError struct {
    Code    string
    Message string
}

func (e CodeError) Error() string { return e.Code + ": " + e.Message }
func NewCodeError(code, message string) CodeError {
    return CodeError{Code: code, Message: message}
}
```

```go
// compiler/build.go, added when diagnostics are wired
package compiler

import "github.com/ryanwible/wrela3/compiler/diag"

type DiagnosticError struct {
    Diagnostics []diag.Diagnostic
}

func (e DiagnosticError) Error() string {
    return diag.Render(e.Diagnostics)
}
```

```go
// compiler/parse/parser.go
package parse

func ParseGraph(graph source.Graph) ([]*ast.Module, []diag.Diagnostic)
```

```go
// compiler/sem/check.go
package sem

type Type struct {
    Module string
    Name string
    Kind Kind
    Unique bool
    DelegatedOnly bool
    Fields []Field
    Methods []Method
}

type CheckedProgram struct {
    Modules    []*ast.Module
    Index      *Index
    ImageGraph ImageGraph
    OwnedRoot *Type
}

func BuildIndex(modules []*ast.Module) (*Index, []diag.Diagnostic)
func Check(index *Index, modules []*ast.Module) (*CheckedProgram, []diag.Diagnostic)
```

`Check` composes sub-checkers in this fixed order and accumulates diagnostics instead of early-exiting on the first error:

```text
1. image phase signature checks
2. construction placement checks
3. expression and return type checks
4. delegated-only propagation checks
5. unique cardinality checks
6. owned-root minting checks
7. driver/path/executor graph checks
```

`OwnedRoot` is derived structurally from the image phase signatures: it is the type returned by `phase delegated_hardware` and accepted by `phase owned_hardware`. V0 examples name it `OwnedHardware`, but semantic checks must use the derived `OwnedRoot` pointer rather than hard-coding the string `OwnedHardware`.

```go
// compiler/asm/parse.go
package asm

func ParseBody(source string, params []string) ([]Instruction, []diag.Diagnostic)
```

`ParseBody` receives method parameter names so bare identifiers in operand position can resolve to parameters before falling through to `ASM0002`.

```go
// compiler/ir/lower.go
package ir

type Program struct {
    Functions []Function
    AsmMethods []AsmMethod
    Data []DataObject
    Entry EntryAdapter
}

type EntryAdapter struct {
    Symbol string
    DelegatedPhaseSymbol string
    OwnedPhaseSymbol string
    DelegatedHardwareType string
    OwnedHardwareType string
}

type Function struct {
    Symbol string
    Params []Value
    Blocks []Block
}

func (f Function) ValuesInDeterministicOrder() []Value

type AsmMethod struct {
    Symbol string
    ReceiverType string
    Params []Value
    Return Type
    Body string
}

func Lower(checked *sem.CheckedProgram) (*Program, []diag.Diagnostic)
```

`ir.Lower` synthesizes one `EntryAdapter` for the image. The adapter's generated code receives the UEFI image handle and system table, constructs the initial `DelegatedHardware`, calls the image's `delegated_hardware` phase, then calls the image's `owned_hardware` phase with the returned `OwnedHardware`.

```go
// compiler/codegen/program.go
package codegen

type Image struct {
    EntrySymbol string
    Sections []Section
    Symbols map[string]uint64
    Relocs []Reloc
}

func Compile(program *ir.Program) (*Image, []diag.Diagnostic)
```

```go
// compiler/pecoff/image.go
package pecoff

func WriteEFI(img *codegen.Image) ([]byte, error)
```

`codegen.Image` is the single bridge type into PE emission. The PE writer may not import semantic or IR packages.
`codegen.Compile` sets `Image.EntrySymbol` from `program.Entry.Symbol`; in v0 that symbol is `_wrela_efi_entry`.

---

## 3. Parallel Work Map

For junior execution, follow dependency order. For parallel execution, use this map only with tech-lead arbitration.

| Stream | Tasks | Contract Owner | Dependencies |
| --- | --- | --- | --- |
| CLI/source/diagnostics | 1-5 | `compiler`, `source`, `diag` | none |
| Lexer/parser frontend | 6-12 | `lex`, `ast`, `parse` | diagnostics |
| Semantics/authority | 13-20 | `sem`, `layout` | parser contracts |
| Assembler/backend | 21-30 | `asm`, `ir`, `codegen` | layout, AST, and ABI contracts |
| PE writer | 31-33 | `pecoff` | `codegen.Image` contract |
| Platform source/runtime | 34-40e | `wrela/*`, `codegen` | parser, semantic, asm, and codegen contracts |
| E2E/QEMU | 41-42 | `qemu`, `tests/e2e` | build pipeline and runtime |
| Production backlog | 43 | docs | none |

### Integration Checkpoints

- After Task 5: source graph tests prove root imports define the universe.
- After Task 10: parser accepts the canonical source modules in Section 4.
- After Task 20: semantic negative fixtures lock the authority model.
- After Task 24: assembler exact-byte tests cover every fixed machine instruction used by v0 edge code.
- After Task 30: normal Wrela code and Wrela-bound assembly methods share one codegen symbol model.
- After Task 33: synthetic code/data can be emitted as PE32+ EFI.
- After Task 40e: transition codegen includes UEFI `ExitBootServices`, paging, GDT/segment reload, IDT, stack switch, entry adapter, and halt path.
- After Task 42: QEMU observes post-owned-hardware serial output.

---

## 4. Canonical Wrela Source Library

This section closes the missing-type gap. Tasks 34-40d create these modules.

### `wrela/platform/uefi/types.wrela`

```wrela
module platform.uefi.types

data UefiHandle {
    address: U64
}

data UefiStatus {
    value: U64
}

class UefiSystemTable {
    boot_services: UefiBootServices
    configuration_tables: UefiConfigurationTables
}

class UefiBootServices {
    table_address: VirtualAddress
}

class UefiConfigurationTables {
    table_address: VirtualAddress
    count: U64
}

data UefiMemoryDescriptor {
    kind: U32
    physical_start: PhysicalAddress
    virtual_start: VirtualAddress
    number_of_pages: U64
    attributes: U64
}

data DelegatedBytes {
    address: VirtualAddress
    length: U64
}

data DelegatedMutableBytes {
    address: VirtualAddress
    length: U64
}

data UefiMemoryMap {
    descriptors: DelegatedBytes
    descriptor_size: U64
    descriptor_version: U32
    key: U64
}

data UefiMemoryMapResult {
    status: UefiStatus
    memory_map: UefiMemoryMap
    required_size: U64
}

class DelegatedMemory {
    arena_base: VirtualAddress
    arena_length: U64
    next_offset: U64
    last_memory_map: UefiMemoryMap

    fn allocate(self, length: U64) -> DelegatedMutableBytes {
        let address = self.arena_base + self.next_offset
        self.next_offset = self.next_offset + length
        return DelegatedMutableBytes(address: address, length: length)
    }

    fn final_memory_map(self) -> UefiMemoryMap {
        return self.last_memory_map
    }

    asm fn build_identity_paging(self, memory_map: UefiMemoryMap) -> PhysicalAddress {
        ret
    }

    asm fn build_owned_gdt(self) -> DelegatedBytes {
        ret
    }

    asm fn build_fatal_idt(self, fatal_handler: VirtualAddress) -> DelegatedBytes {
        ret
    }
}
```

### `wrela/platform/uefi/boot_services.wrela`

```wrela
module platform.uefi.boot_services

use {
    DelegatedMutableBytes,
    UefiBootServices,
    UefiHandle,
    UefiMemoryMapResult,
    UefiStatus
} from platform.uefi.types

class UefiBootServicesCalls {
    boot_services: UefiBootServices

    asm fn get_memory_map(self, buffer: DelegatedMutableBytes) -> UefiMemoryMapResult {
        mov rax, self.boot_services
        ret
    }

    asm fn exit_boot_services(self, image: UefiHandle, map_key: U64) -> UefiStatus {
        mov rax, self.boot_services
        ret
    }
}
```

The assembly bodies above are the source-level shape. Task 39 replaces the placeholder bodies with the real Microsoft x64 ABI table calls from Appendix D.

### `wrela/platform/uefi/transition.wrela`

```wrela
module platform.uefi.transition

use { DelegatedMemory, UefiHandle } from platform.uefi.types
use { UefiBootServicesCalls } from platform.uefi.boot_services
use { CpuPlan, OwnedHardware, MemoryPlan, VirtualMemoryPlan } from machine.x86_64.cpu_state

unique class DelegatedHardware {
    image_handle: UefiHandle
    boot_services: UefiBootServicesCalls
    delegated_memory: DelegatedMemory

    fn exit_to_owned_hardware(
        self,
        memory_plan: MemoryPlan,
        virtual_memory_plan: VirtualMemoryPlan,
        cpu_plan: CpuPlan
    ) -> OwnedHardware {
        let final_map = self.delegated_memory.final_memory_map()
        self.boot_services.exit_boot_services(
            image: self.image_handle,
            map_key: final_map.key
        )
        return OwnedHardware(
            memory: memory_plan.owned_memory,
            io_ports: memory_plan.io_ports,
            vcpu0: cpu_plan.vcpu0
        )
    }
}
```

This source is intentionally small. The generated UEFI entry adapter constructs the initial `DelegatedHardware` from the UEFI image handle and system table, then calls the image's `delegated_hardware` phase. Task 40a replaces the simplified final-map path with the real GetMemoryMap retry loop, and Tasks 40b-40d add paging, source-owned descriptor-table builders, fatal-handler, and stack-switch code.

### `wrela/machine/x86_64/cpu_state.wrela`

```wrela
module machine.x86_64.cpu_state

use { Bytes, ExecutorMemory, MutableBytes } from machine.x86_64.executor_memory

data ExecutorPlacement {
    id: U64
    memory: ExecutorMemory
}

data MemoryPlan {
    owned_memory: OwnedMemory
    executor_arena: MutableBytes
    io_ports: IoPortAuthority
}

data VirtualMemoryPlan {
    pml4: PhysicalAddress
}

data CpuPlan {
    vcpu0: ExecutorPlacement
    owned_stack_top: VirtualAddress
    gdt_descriptor: Bytes
    idt_descriptor: Bytes
    cr3: PhysicalAddress
}

class PhysicalMemoryPlanner {
    memory_plan: MemoryPlan

    fn plan(self) -> MemoryPlan {
        return self.memory_plan
    }
}

class VirtualMemoryPlanner {
    virtual_memory_plan: VirtualMemoryPlan

    fn identity_map(self) -> VirtualMemoryPlan {
        return self.virtual_memory_plan
    }
}

class CpuPlanner {
    cpu_plan: CpuPlan

    fn plan(self) -> CpuPlan {
        return self.cpu_plan
    }
}

class OwnedMemory {
    arena: MutableBytes
}

class DriverMemory {
    region: MutableBytes
}

data Com1IoPortClaim {
    port_base: U16
}

class IoPortAuthority {
    fn claim_com1(self) -> Com1IoPortClaim {
        return Com1IoPortClaim(port_base: 0x03f8)
    }
}

unique class OwnedHardware {
    memory: OwnedMemory
    io_ports: IoPortAuthority
    vcpu0: ExecutorPlacement
}
```

Executor launch is source-visible in the image phase. The `owned_hardware` phase constructs an executor and calls its `start fn` directly:

```wrela
let hello = HelloWorld(memory: hardware.vcpu0.memory, serial_path: serial_path)
hello.run()
```

The compiler gives special meaning to `start fn` only when resolving `hello.run()` as an executor entry. There is no `OwnedHardware.start(HelloWorld)` method in v0 because that would make the machine module depend on the example executor type.

### `wrela/machine/x86_64/executor_memory.wrela`

```wrela
module machine.x86_64.executor_memory

data Bytes {
    address: VirtualAddress
    length: U64
}

data MutableBytes {
    address: VirtualAddress
    length: U64
}

class ExecutorMemory {
    arena_base: VirtualAddress
    arena_length: U64
    next_offset: U64

    fn static_bytes(self, value: StringLiteral) -> Bytes {
        return Bytes(address: value.address, length: value.length)
    }

    fn allocate_bytes(self, length: U64) -> MutableBytes {
        let address = self.arena_base + self.next_offset
        self.next_offset = self.next_offset + length
        return MutableBytes(address: address, length: length)
    }

    asm fn halt_forever(self) -> never {
    loop:
        hlt
        jmp loop
    }
}
```

`StringLiteral.address` and `StringLiteral.length` are compiler-provided fields. Every string literal is emitted into `.rdata` as bytes plus a trailing zero byte. The expression type `StringLiteral` has:

```text
address: VirtualAddress
length: U64
```

`length` excludes the trailing zero byte.

### `wrela/arch/x86_64/cpu.wrela`

```wrela
module arch.x86_64.cpu

class CpuControl {
    asm fn pause(self) {
        pause
        ret
    }

    asm fn halt_forever(self) -> never {
    loop:
        hlt
        jmp loop
    }

    asm fn load_cr3(self, pml4: PhysicalAddress) {
        mov rax, pml4
        mov cr3, rax
        ret
    }
}
```

### `wrela/arch/x86_64/io.wrela`

```wrela
module arch.x86_64.io

class PortIo {
    asm fn out8(self, port: U16, value: U8) {
        mov dx, port
        mov al, value
        out dx, al
        ret
    }

    asm fn in8(self, port: U16) -> U8 {
        mov dx, port
        in al, dx
        ret
    }
}
```

### `wrela/machine/x86_64/serial.wrela`

```wrela
module machine.x86_64.serial

use { Bytes } from machine.x86_64.executor_memory
use { DriverMemory, ExecutorPlacement } from machine.x86_64.cpu_state

driver path SerialWriterRegisters {
    port_base: U16

    asm fn write8(self, offset: U16, value: U8) {
        mov dx, self.port_base
        add dx, offset
        mov al, value
        out dx, al
        ret
    }

    asm fn read8(self, offset: U16) -> U8 {
        mov dx, self.port_base
        add dx, offset
        in al, dx
        ret
    }
}

unique driver SerialDriver {
    registers: SerialWriterRegisters
    memory: DriverMemory

    fn initialize(self) -> SerialDriver {
        self.registers.write8(offset: 1, value: 0x00)
        self.registers.write8(offset: 3, value: 0x80)
        self.registers.write8(offset: 0, value: 0x03)
        self.registers.write8(offset: 1, value: 0x00)
        self.registers.write8(offset: 3, value: 0x03)
        self.registers.write8(offset: 2, value: 0xC7)
        self.registers.write8(offset: 4, value: 0x03)
        return self
    }
}

driver path SerialWritePath {
    owner: ExecutorPlacement
    registers: SerialWriterRegisters

    fn write(self, bytes: Bytes) {
        for byte in bytes {
            self.wait_until_ready()
            self.registers.write8(offset: 0, value: byte)
        }
    }

    fn wait_until_ready(self) {
        while (self.registers.read8(offset: 5) & 0x20) == 0 {
            self.pause()
        }
    }

    asm fn pause(self) {
        pause
        ret
    }
}

```

### `examples/hello/program.wrela`

```wrela
module examples.hello.program

use { ExecutorMemory } from machine.x86_64.executor_memory
use { SerialWritePath } from machine.x86_64.serial

executor HelloWorld {
    memory: ExecutorMemory
    serial_path: SerialWritePath

    start fn run(self) -> never {
        self.serial_path.write(self.memory.static_bytes("hello from wrela\n"))
        self.memory.halt_forever()
    }
}
```

### `examples/hello/main.wrela`

```wrela
module examples.hello.main

use { HelloWorld } from examples.hello.program
use { DelegatedHardware } from platform.uefi.transition
use { CpuPlan, DriverMemory, ExecutorPlacement, IoPortAuthority, OwnedHardware, OwnedMemory, MemoryPlan, VirtualMemoryPlan } from machine.x86_64.cpu_state
use { Bytes, ExecutorMemory, MutableBytes } from machine.x86_64.executor_memory
use { SerialDriver, SerialWriterRegisters, SerialWritePath } from machine.x86_64.serial

image HelloSerial {
    transitions {
        delegated_hardware -> owned_hardware
    }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        let arena = MutableBytes(address: 0x200000, length: 0x200000)
        let executor_memory = ExecutorMemory(arena_base: arena.address, arena_length: arena.length, next_offset: 0)
        let memory_plan = MemoryPlan(
            owned_memory: OwnedMemory(arena: arena),
            executor_arena: arena,
            io_ports: IoPortAuthority()
        )
        let virtual_memory_plan = VirtualMemoryPlan(pml4: arena.address)
        let cpu_plan = CpuPlan(
            vcpu0: ExecutorPlacement(id: 0, memory: executor_memory),
            owned_stack_top: arena.address + arena.length,
            gdt_descriptor: Bytes(address: 0, length: 0),
            idt_descriptor: Bytes(address: 0, length: 0),
            cr3: virtual_memory_plan.pml4
        )
        return hardware.exit_to_owned_hardware(
            memory_plan: memory_plan,
            virtual_memory_plan: virtual_memory_plan,
            cpu_plan: cpu_plan
        )
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        let com1 = hardware.io_ports.claim_com1()
        let registers = SerialWriterRegisters(port_base: com1.port_base)
        let serial_driver = SerialDriver(
            registers: registers,
            memory: DriverMemory(region: hardware.memory.arena)
        ).initialize()
        let serial_path = SerialWritePath(owner: hardware.vcpu0, registers: serial_driver.registers)
        let hello = HelloWorld(memory: hardware.vcpu0.memory, serial_path: serial_path)
        hello.run()
    }
}
```

---

## 5. Task List

### Task 1: Go Module, CodeError, And Mode Parsing

**Files:**

- Create: `go.mod`
- Create: `compiler/errors.go`
- Create: `compiler/mode.go`
- Create: `compiler/mode_test.go`

**Why:** The first plan had CLI error classification bugs. `ParseMode` must return `CLI0001`, not an unprefixed error.

- [ ] **Step 1: Write failing mode tests**

Create `compiler/mode_test.go`:

```go
package compiler

import "testing"

func TestParseModeDevAndRelease(t *testing.T) {
    dev, err := ParseMode("dev")
    if err != nil {
        t.Fatalf("dev returned error: %v", err)
    }
    if dev != ModeDev {
        t.Fatalf("dev = %q, want %q", dev, ModeDev)
    }

    release, err := ParseMode("release")
    if err != nil {
        t.Fatalf("release returned error: %v", err)
    }
    if release != ModeRelease {
        t.Fatalf("release = %q, want %q", release, ModeRelease)
    }
}

func TestParseModeInvalidReturnsCLI0001(t *testing.T) {
    _, err := ParseMode("fast")
    if err == nil {
        t.Fatal("ParseMode(fast) succeeded, want error")
    }
    ce, ok := err.(CodeError)
    if !ok {
        t.Fatalf("error type = %T, want compiler.CodeError", err)
    }
    if ce.Code != "CLI0001" {
        t.Fatalf("code = %s, want CLI0001", ce.Code)
    }
    if err.Error() != "CLI0001: invalid mode \"fast\"; expected dev or release" {
        t.Fatalf("message = %q", err.Error())
    }
}
```

- [ ] **Step 2: Run test and verify failure**

Run:

```bash
go test ./compiler -run TestParseMode -v
```

Expected output:

```text
compiler/mode_test.go:12: undefined: ParseMode
FAIL
```

- [ ] **Step 3: Add module and implementation**

Create `go.mod`:

```go
module github.com/ryanwible/wrela3

go 1.22
```

Create `compiler/errors.go`:

```go
package compiler

type CodeError struct {
    Code    string
    Message string
}

func NewCodeError(code, message string) CodeError {
    return CodeError{Code: code, Message: message}
}

func (e CodeError) Error() string {
    return e.Code + ": " + e.Message
}
```

Create `compiler/mode.go`:

```go
package compiler

import "fmt"

type Mode string

const (
    ModeDev     Mode = "dev"
    ModeRelease Mode = "release"
)

func ParseMode(raw string) (Mode, error) {
    switch raw {
    case string(ModeDev):
        return ModeDev, nil
    case string(ModeRelease):
        return ModeRelease, nil
    default:
        return "", NewCodeError("CLI0001", fmt.Sprintf("invalid mode %q; expected dev or release", raw))
    }
}
```

- [ ] **Step 4: Run test and verify pass**

Run:

```bash
go test ./compiler -run TestParseMode -v
```

Expected output:

```text
=== RUN   TestParseModeDevAndRelease
--- PASS: TestParseModeDevAndRelease
=== RUN   TestParseModeInvalidReturnsCLI0001
--- PASS: TestParseModeInvalidReturnsCLI0001
PASS
```

- [ ] **Step 5: Commit**

```bash
git add go.mod compiler/errors.go compiler/mode.go compiler/mode_test.go
git commit -m "chore: bootstrap compiler mode contract -Codex Automated"
```

**Acceptance Criteria:**

- Invalid modes produce `compiler.CodeError{Code:"CLI0001"}`.
- `go test ./compiler -run TestParseMode -v` passes.

---

### Task 2: CLI Build Command

**Files:**

- Create: `cmd/wrela/main.go`
- Create: `compiler/build.go`
- Create: `cmd/wrela/main_test.go`
- Create: `compiler/build_test.go`

- [ ] **Step 1: Write failing build contract tests**

Create `compiler/build_test.go`:

```go
package compiler

import "testing"

func TestBuildRejectsReleaseMode(t *testing.T) {
    _, err := Build(BuildOptions{
        Mode:       ModeRelease,
        RootPath:   "examples/hello/main.wrela",
        OutputPath: "build/hello.efi",
        RepoRoot:   ".",
    })
    ce, ok := err.(CodeError)
    if !ok {
        t.Fatalf("error = %T, want CodeError", err)
    }
    if ce.Code != "CLI0002" {
        t.Fatalf("code = %s, want CLI0002", ce.Code)
    }
}

func TestBuildRequiresRootAndOutput(t *testing.T) {
    _, err := Build(BuildOptions{Mode: ModeDev, OutputPath: "build/out.efi", RepoRoot: "."})
    if ce := err.(CodeError); ce.Code != "CLI0003" {
        t.Fatalf("code = %s, want CLI0003", ce.Code)
    }

    _, err = Build(BuildOptions{Mode: ModeDev, RootPath: "main.wrela", RepoRoot: "."})
    if ce := err.(CodeError); ce.Code != "CLI0004" {
        t.Fatalf("code = %s, want CLI0004", ce.Code)
    }
}
```

Create `cmd/wrela/main_test.go`:

```go
package main

import "testing"

func TestRunUsageAndInvalidModeAreUsageErrors(t *testing.T) {
    if code := run(nil); code != 2 {
        t.Fatalf("run(nil) = %d, want 2", code)
    }
    code := run([]string{"build", "--mode", "fast", "main.wrela", "-o", "out.efi"})
    if code != 2 {
        t.Fatalf("invalid mode exit = %d, want 2", code)
    }
}
```

- [ ] **Step 2: Run tests and verify failure**

Run:

```bash
go test ./compiler ./cmd/wrela -run 'TestBuild|TestRun' -v
```

Expected output:

```text
undefined: Build
FAIL
```

- [ ] **Step 3: Implement Build and CLI**

Create `compiler/build.go`:

```go
package compiler

type BuildOptions struct {
    Mode       Mode
    RootPath   string
    OutputPath string
    RepoRoot   string
}

type BuildResult struct {
    OutputPath string
}

func Build(opts BuildOptions) (BuildResult, error) {
    if opts.Mode == ModeRelease {
        return BuildResult{}, NewCodeError("CLI0002", "release mode is not implemented in v0")
    }
    if opts.RootPath == "" {
        return BuildResult{}, NewCodeError("CLI0003", "root source path is required")
    }
    if opts.OutputPath == "" {
        return BuildResult{}, NewCodeError("CLI0004", "output path is required")
    }
    return BuildResult{}, NewCodeError("INT0001", "build pipeline is not wired yet")
}
```

Create `cmd/wrela/main.go`:

```go
package main

import (
    "flag"
    "fmt"
    "os"
    "strings"

    "github.com/ryanwible/wrela3/compiler"
)

func main() {
    os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
    if len(args) == 0 || args[0] != "build" {
        fmt.Fprintln(os.Stderr, "usage: wrela build --mode dev <root.wrela> -o <out.efi>")
        return 2
    }

    fs := flag.NewFlagSet("build", flag.ContinueOnError)
    fs.SetOutput(os.Stderr)
    modeRaw := fs.String("mode", "", "compile mode: dev or release")
    output := fs.String("o", "", "output .efi path")
    repoRoot := fs.String("repo-root", ".", "repository root containing wrela/")
    if err := fs.Parse(args[1:]); err != nil {
        return 2
    }
    if fs.NArg() != 1 {
        fmt.Fprintln(os.Stderr, "usage: wrela build --mode dev <root.wrela> -o <out.efi>")
        return 2
    }

    mode, err := compiler.ParseMode(*modeRaw)
    if err != nil {
        fmt.Fprintln(os.Stderr, err)
        return 2
    }

    _, err = compiler.Build(compiler.BuildOptions{
        Mode:       mode,
        RootPath:   fs.Arg(0),
        OutputPath: *output,
        RepoRoot:   *repoRoot,
    })
    if err != nil {
        fmt.Fprintln(os.Stderr, err)
        if ce, ok := err.(compiler.CodeError); ok && strings.HasPrefix(ce.Code, "CLI") {
            return 2
        }
        if ce, ok := err.(compiler.CodeError); ok && strings.HasPrefix(ce.Code, "INT") {
            return 3
        }
        return 1
    }
    return 0
}
```

- [ ] **Step 4: Run tests and verify pass**

Run:

```bash
go test ./compiler ./cmd/wrela -run 'TestBuild|TestRun' -v
```

Expected output:

```text
PASS
```

- [ ] **Step 5: Commit**

```bash
git add cmd/wrela/main.go cmd/wrela/main_test.go compiler/build.go compiler/build_test.go
git commit -m "feat: add build CLI contract -Codex Automated"
```

**Acceptance Criteria:**

- Invalid mode exits as usage error.
- Release mode fails with `CLI0002`.
- No substring slicing on error strings exists.

---

## 6. Executable Task Index

Tasks 1-2 above show the full format. Every task below follows the same five-step ladder: write the failing test or fixture, run it and observe failure, implement the minimal code, run it and observe pass, commit. The implementation code examples are intentionally scoped to the task; appendices provide byte layouts and algorithms that would otherwise require external research.

### Task 3: Diagnostic Code Constants And Sorting

**Files:** `compiler/diag/codes.go`, `compiler/diag/diag.go`, `compiler/diag/render.go`, `compiler/diag/diag_test.go`

- [ ] **Step 1: Write the failing test**

```go
package diag_test

import (
    "strings"
    "testing"

    "github.com/ryanwible/wrela3/compiler/diag"
)

func TestSortDiagnosticsStable(t *testing.T) {
    ds := []diag.Diagnostic{
        {Phase: "sem", FilePath: "b.wrela", Start: 10, End: 11, Code: diag.SEM0009, Sequence: 2},
        {Phase: "parse", FilePath: "a.wrela", Start: 2, End: 3, Code: diag.PAR0001, Sequence: 1},
    }
    diag.Sort(ds)
    if ds[0].Code != diag.PAR0001 || ds[1].Code != diag.SEM0009 {
        t.Fatalf("sorted codes = %s, %s", ds[0].Code, ds[1].Code)
    }
}

func TestRenderIncludesLocationCodeAndMessage(t *testing.T) {
    out := diag.Render([]diag.Diagnostic{{
        Severity: diag.Error,
        Phase: "parse",
        FilePath: "a.wrela",
        Start: 2,
        End: 3,
        Code: diag.PAR0001,
        Message: "unexpected token",
    }})
    for _, want := range []string{"a.wrela:2-3", "error", "PAR0001", "unexpected token"} {
        if !strings.Contains(out, want) {
            t.Fatalf("render missing %q in %q", want, out)
        }
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./compiler/diag -v`

Expected: `package github.com/ryanwible/wrela3/compiler/diag is not in std` or `undefined: diag.Diagnostic`.

- [ ] **Step 3: Implement diagnostics**

```go
package diag

import (
    "fmt"
    "sort"
    "strings"
)

type Severity string

const (
    Error Severity = "error"
    Warning Severity = "warning"
)

const (
    CLI0001 = "CLI0001"; CLI0002 = "CLI0002"; CLI0003 = "CLI0003"; CLI0004 = "CLI0004"
    SRC0001 = "SRC0001"; SRC0002 = "SRC0002"; SRC0003 = "SRC0003"; SRC0004 = "SRC0004"; SRC0005 = "SRC0005"
    PAR0001 = "PAR0001"; PAR0002 = "PAR0002"
    SEM0001 = "SEM0001"; SEM0002 = "SEM0002"; SEM0003 = "SEM0003"; SEM0004 = "SEM0004"; SEM0005 = "SEM0005"; SEM0006 = "SEM0006"; SEM0007 = "SEM0007"; SEM0008 = "SEM0008"; SEM0009 = "SEM0009"; SEM0010 = "SEM0010"; SEM0011 = "SEM0011"; SEM0012 = "SEM0012"; SEM0013 = "SEM0013"
    ASM0001 = "ASM0001"; ASM0002 = "ASM0002"; ASM0003 = "ASM0003"
    CG0001 = "CG0001"; PE0001 = "PE0001"; QEMU0001 = "QEMU0001"; INT0001 = "INT0001"
)

type Diagnostic struct {
    Phase string
    Code string
    Severity Severity
    FilePath string
    Start int
    End int
    Message string
    Sequence int
}

func Sort(ds []Diagnostic) {
    sort.SliceStable(ds, func(i, j int) bool {
        a, b := ds[i], ds[j]
        if a.Phase != b.Phase { return a.Phase < b.Phase }
        if a.FilePath != b.FilePath { return a.FilePath < b.FilePath }
        if a.Start != b.Start { return a.Start < b.Start }
        if a.End != b.End { return a.End < b.End }
        if a.Code != b.Code { return a.Code < b.Code }
        return a.Sequence < b.Sequence
    })
}

func Render(ds []Diagnostic) string {
    Sort(ds)
    var b strings.Builder
    for _, d := range ds {
        fmt.Fprintf(&b, "%s:%d-%d: %s %s: %s\n", d.FilePath, d.Start, d.End, d.Severity, d.Code, d.Message)
    }
    return b.String()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./compiler/diag -v`

Expected: `--- PASS: TestSortDiagnosticsStable`, `--- PASS: TestRenderIncludesLocationCodeAndMessage`, `PASS`.

- [ ] **Step 5: Commit**

```bash
git add compiler/diag
git commit -m "feat: add diagnostic model -Codex Automated"
```

**Acceptance Criteria:** All Appendix F codes exist as constants; sorting order matches Section 2; render includes file span, severity, code, and message.

### Task 4: Source File Line Maps

**Files:** `compiler/source/file.go`, `compiler/source/file_test.go`

- [ ] **Step 1: Write failing tests**

```go
package source_test

import (
    "testing"
    "github.com/ryanwible/wrela3/compiler/source"
)

func TestLineColumn(t *testing.T) {
    f := source.NewFile(1, "main.wrela", "a\nbc\n")
    line, col := f.LineColumn(3)
    if line != 2 || col != 2 {
        t.Fatalf("LineColumn(3) = %d:%d, want 2:2", line, col)
    }
}
```

- [ ] **Step 2:** Run `go test ./compiler/source -run TestLineColumn -v`; expect `undefined: source.NewFile`.
- [ ] **Step 3:** Implement `FileID`, `Span`, `File`, `NewFile`, and 1-based `LineColumn` using byte offsets for line starts.
- [ ] **Step 4:** Run `go test ./compiler/source -run TestLineColumn -v`; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: add source file line maps -Codex Automated"`.

**Acceptance Criteria:** Empty files, trailing newline files, and offsets at line starts return stable 1-based positions.

### Task 5: Import Header Scanner And Source Graph

**Files:** `compiler/source/graph.go`, `compiler/source/graph_test.go`

- [ ] **Step 1: Write failing tests for headers, missing modules, cycles, duplicate modules**

```go
func TestExtractHeader(t *testing.T) {
    module, imports, err := source.ExtractHeader(`module examples.hello.main
use { HelloWorld } from examples.hello.program
use { SerialDriver, SerialWritePath } from machine.x86_64.serial
image HelloSerial {}`)
    if err != nil { t.Fatal(err) }
    if module != "examples.hello.main" { t.Fatalf("module = %s", module) }
    want := []string{"examples.hello.program", "machine.x86_64.serial"}
    if !reflect.DeepEqual(imports, want) { t.Fatalf("imports = %#v", imports) }
}
```

- [ ] **Step 2:** Run `go test ./compiler/source -run 'TestExtractHeader|TestLoadGraph' -v`; expect `undefined: source.ExtractHeader`.
- [ ] **Step 3:** Implement `ExtractHeader`, `Options`, `Graph`, `LoadGraph`, module-to-path resolution, cycle detection, duplicate module detection.
- [ ] **Step 4:** Run `go test ./compiler/source -v`; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: add root import graph -Codex Automated"`.

**Acceptance Criteria:** Only transitive imports are loaded; unimported sibling files are ignored; errors use `SRC0002`-`SRC0005`.

### Task 6: Lexer

**Files:** `compiler/lex/token.go`, `compiler/lex/lexer.go`, `compiler/lex/lexer_test.go`

- [ ] **Step 1: Write failing lexer tests**

```go
func TestLexPhaseHeader(t *testing.T) {
    toks, ds := lex.All("phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {}")
    if len(ds) != 0 { t.Fatalf("diagnostics: %#v", ds) }
    got := kinds(toks)
    want := []lex.Kind{lex.KeywordPhase, lex.Identifier, lex.LParen, lex.Identifier, lex.Colon, lex.Identifier, lex.RParen, lex.Arrow, lex.Identifier, lex.LBrace, lex.RBrace, lex.EOF}
    if !reflect.DeepEqual(got, want) { t.Fatalf("kinds = %#v", got) }
}
```

- [ ] **Step 2:** Run `go test ./compiler/lex -v`; expect `undefined: lex.All`.
- [ ] **Step 3:** Implement token kinds, keyword map, comments, strings, integers, operators, byte spans.
- [ ] **Step 4:** Run `go test ./compiler/lex -v`; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: add Wrela lexer -Codex Automated"`.

**Acceptance Criteria:** Keywords listed in Section 1 tokenize as keywords; `0x03f8` tokenizes as integer literal; bad strings produce `PAR0001`.

### Task 7: AST Declaration And Node Contracts

**Files:** `compiler/ast/ast.go`, `compiler/ast/walk.go`, `compiler/ast/ast_test.go`

- [ ] **Step 1:** Write compile-time interface assertion tests for `ImageDecl`, `DriverPathDecl`, `ForStmt`, `CallExpr`.
- [ ] **Step 2:** Run `go test ./compiler/ast -v`; expect undefined types.
- [ ] **Step 3:** Implement AST structs from Section 2 and `DebugExpr` for parser tests.
- [ ] **Step 4:** Run `go test ./compiler/ast -v`; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: add Wrela AST contracts -Codex Automated"`.

**Acceptance Criteria:** AST distinguishes `driver`, `driver path`, `executor`, `image`, `phase`, `asm fn`, and `start fn`.

### Task 8: Declaration Parser

**Files:** `compiler/parse/parser.go`, `compiler/parse/parser_test.go`

- [ ] **Step 1:** Write parser tests for `unique driver`, `driver path`, `executor`, `image` with `delegated_hardware -> owned_hardware`.
- [ ] **Step 2:** Run `go test ./compiler/parse -run TestParseDecls -v`; expect parser missing.
- [ ] **Step 3:** Implement recursive descent for module/import/declarations/method signatures.

```go
func (p *Parser) ParseModule() (*ast.Module, []diag.Diagnostic) {
    p.expect(lex.KeywordModule)
    name := p.parseDottedName()
    mod := &ast.Module{Name: name}
    for p.peek().Kind == lex.KeywordUse {
        mod.Imports = append(mod.Imports, p.parseImport())
    }
    for p.peek().Kind != lex.EOF {
        decl, ds := p.parseDecl()
        if len(ds) != 0 { return nil, ds }
        mod.Decls = append(mod.Decls, decl)
    }
    return mod, nil
}

func (p *Parser) parseDecl() (ast.Decl, []diag.Diagnostic) {
    unique := p.match(lex.KeywordUnique)
    switch p.peek().Kind {
    case lex.KeywordData:
        if unique { return nil, p.err(p.peek(), diag.PAR0002, "unique may not prefix data in v0") }
        return p.parseDataDecl(), nil
    case lex.KeywordClass:
        return p.parseClassDecl(unique), nil
    case lex.KeywordDriver:
        p.next()
        if p.match(lex.KeywordPath) {
            if unique { return nil, p.err(p.peek(), diag.PAR0002, "driver path is not unique in v0") }
            return p.finishDriverPathDecl(), nil
        }
        return p.finishDriverDecl(unique), nil
    case lex.KeywordExecutor:
        if unique { return nil, p.err(p.peek(), diag.PAR0002, "executor may not be unique in v0") }
        return p.parseExecutorDecl(), nil
    case lex.KeywordImage:
        if unique { return nil, p.err(p.peek(), diag.PAR0002, "image may not be unique") }
        return p.parseImageDecl(), nil
    default:
        return nil, p.err(p.peek(), diag.PAR0002, "expected declaration")
    }
}
```
- [ ] **Step 4:** Run `go test ./compiler/parse -run TestParseDecls -v`; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: parse Wrela declarations -Codex Automated"`.

**Acceptance Criteria:** Module-scope functions fail with `PAR0002`; `unique data` fails with `PAR0002`; `parse.ParseGraph(graph)` iterates graph files in order, parses each file with `Parser.ParseModule`, and returns all parse diagnostics sorted by `diag.Sort`.

### Task 9: Pratt Expression Parser

**Files:** `compiler/parse/expr.go`, `compiler/parse/expr_test.go`

- [ ] **Step 1:** Write failing precedence test: `a + b * c` renders as `(+ a (* b c))`.
- [ ] **Step 2:** Run `go test ./compiler/parse -run TestBinaryPrecedence -v`; expect failure.
- [ ] **Step 3:** Implement Pratt parser with this exact precedence table and loop.

```go
var precedence = map[lex.Kind]int{
    lex.Dot: 90, lex.LParen: 90,
    lex.Star: 80,
    lex.Plus: 70, lex.Minus: 70,
    lex.ShiftLeft: 60, lex.ShiftRight: 60,
    lex.Less: 50, lex.LessEqual: 50, lex.Greater: 50, lex.GreaterEqual: 50,
    lex.EqualEqual: 40, lex.BangEqual: 40,
    lex.Amp: 30,
    lex.Caret: 20,
    lex.Pipe: 10,
}

func (p *Parser) parseExpr(minPrec int) (ast.Expr, []diag.Diagnostic) {
    left, ds := p.parsePrimary()
    if len(ds) != 0 {
        return nil, ds
    }
    for {
        tok := p.peek()
        prec, ok := precedence[tok.Kind]
        if !ok || prec < minPrec {
            break
        }
        op := p.next()
        if op.Kind == lex.Dot {
            name := p.expect(lex.Identifier)
            if p.match(lex.LParen) {
                args := p.parseNamedArgs()
                p.expect(lex.RParen)
                left = &ast.CallExpr{Receiver: left, Method: name.Text, Args: args}
            } else {
                left = &ast.FieldExpr{Base: left, Field: name.Text}
            }
            continue
        }
        right, rds := p.parseExpr(prec + 1)
        if len(rds) != 0 {
            return nil, rds
        }
        left = &ast.BinaryExpr{Op: op.Text, Left: left, Right: right}
    }
    return left, nil
}
```
- [ ] **Step 4:** Run parser expression tests; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: parse Wrela expressions -Codex Automated"`.

**Acceptance Criteria:** Constructor calls and method calls are distinct AST forms.

### Task 10: Statement Parser And Asm Body Capture

**Files:** `compiler/parse/parser.go`, `compiler/parse/parser_test.go`

- [ ] **Step 1:** Write tests for `let`, `return`, `if`, `while`, `for`, assignment, and raw asm body capture.
- [ ] **Step 2:** Run `go test ./compiler/parse -run TestParseStatements -v`; expect failure.
- [ ] **Step 3:** Implement statement parser; capture `asm fn` body source between braces; reject inline `asm { hlt }`.

```go
func (p *Parser) parseStmt() (ast.Stmt, []diag.Diagnostic) {
    switch p.peek().Kind {
    case lex.KeywordLet:
        return p.parseLet(), nil
    case lex.KeywordReturn:
        return p.parseReturn(), nil
    case lex.KeywordIf:
        return p.parseIf(), nil
    case lex.KeywordWhile:
        return p.parseWhile(), nil
    case lex.KeywordFor:
        return p.parseFor(), nil
    case lex.KeywordAsm:
        return nil, p.err(p.peek(), diag.PAR0001, "inline asm blocks are not allowed in v0")
    default:
        return p.parseExprOrAssignStmt()
    }
}

func (p *Parser) captureAsmBody() (ast.AsmBody, []diag.Diagnostic) {
    open := p.expect(lex.LBrace)
    depth := 1
    start := open.End
    for depth > 0 && p.peek().Kind != lex.EOF {
        tok := p.next()
        if tok.Kind == lex.LBrace { depth++ }
        if tok.Kind == lex.RBrace { depth-- }
        if depth == 0 {
            return ast.AsmBody{Source: p.source[start:tok.Start], Span: source.Span{Start: start, End: tok.Start}}, nil
        }
    }
    return ast.AsmBody{}, p.err(open, diag.PAR0001, "unterminated asm body")
}
```

Statement syntax rules for v0:

```text
newline, semicolon, or closing brace separates statements and declaration members
let name = expr creates a local binding
name = expr reassigns an existing local binding
self.field = expr assigns a field on the receiver
field declarations and method declarations may not abut on one physical line without a separator
```

- [ ] **Step 4:** Run statement tests; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: parse Wrela statements and asm bodies -Codex Automated"`.

**Acceptance Criteria:** `for byte in bytes { self.write_byte(byte) }` parses as `ForStmt`; inline asm fails with `PAR0001`.

### Task 11: Wrela Record Layout

**Files:** `compiler/layout/record.go`, `compiler/layout/record_test.go`

- [ ] **Step 1:** Write failing padding test for fields `U8` then `U64`.
- [ ] **Step 2:** Run `go test ./compiler/layout -v`; expect undefined package.
- [ ] **Step 3:** Implement Appendix E exactly.
- [ ] **Step 4:** Run layout tests; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: add record layout algorithm -Codex Automated"`.

**Acceptance Criteria:** Layout output is used by both codegen and asm symbolic operand materialization.

### Task 12: Declaration Index

**Files:** `compiler/sem/symbols.go`, `compiler/sem/types.go`, `compiler/sem/symbols_test.go`

- [ ] **Step 1:** Write tests for same-module lookup, imported name lookup, duplicate declaration, duplicate imported name, missing image (`SEM0004`), and multiple images (`SEM0003`).
- [ ] **Step 2:** Run `go test ./compiler/sem -run TestIndex -v`; expect failure.
- [ ] **Step 3:** Implement `Type`, `Index`, primitive types, imported-name table, one-image detection, and `sem.BuildIndex(modules)`.
- [ ] **Step 4:** Run index tests; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: add semantic declaration index -Codex Automated"`.

**Acceptance Criteria:** `StringLiteral` has fields `address: VirtualAddress` and `length: U64`; `sem.BuildIndex(modules)` is the only exported index-construction entrypoint used by `compiler.Build`; missing image fails with `SEM0004`; multiple images fail with `SEM0003`.

### Task 13: Image Phase Signature Checks

**Files:** `compiler/sem/check.go`, `compiler/sem/phase_test.go`

- [ ] **Step 1:** Write fixture tests for one image missing `delegated_hardware -> owned_hardware`, wrong `delegated_hardware` parameter type, wrong `owned_hardware` parameter type, and `owned_hardware` returning anything other than `never`; each expects `SEM0005`.
- [ ] **Step 2:** Run phase tests; expect missing checker.
- [ ] **Step 3:** Implement exact phase rules from Section 1.
- [ ] **Step 4:** Run `go test ./compiler/sem -run TestPhase -v`; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: validate image phase signatures -Codex Automated"`.

**Acceptance Criteria:** Invalid phase structure fails with `SEM0005`; `phase delegated_hardware` must accept `DelegatedHardware` and return `OwnedHardware`; `phase owned_hardware` must accept that same `OwnedHardware` type and return `never`; `CheckedProgram.OwnedRoot` is set from the validated `delegated_hardware -> owned_hardware` signatures.

### Task 14: Construction Placement Checks

**Files:** `compiler/sem/check.go`, `compiler/sem/construction_test.go`

- [ ] **Step 1:** Write tests: `Other()` inside normal method fails `SEM0006`; `Bytes(address: value.address, length: value.length)` inside method passes.
- [ ] **Step 2:** Run construction tests; expect failure.
- [ ] **Step 3:** Implement context tracking for direct image phase body construction.

```go
type ContextKind int

const (
    ContextNormalMethod ContextKind = iota
    ContextImagePhaseDirect
    ContextOwnershipTransferAuthorityMethod
)

func (c *checker) canMintInContext(ctx ContextKind, typ *Type) bool {
    return ctx == ContextOwnershipTransferAuthorityMethod && typ == c.ownedRoot
}

func (c *checker) checkConstructor(ctx ContextKind, typ *Type, span source.Span) {
    if typ.Kind == KindData {
        return
    }
    if ctx == ContextImagePhaseDirect {
        return
    }
    if c.canMintInContext(ctx, typ) {
        return
    }
    c.error(span, diag.SEM0006, typ.Kind.String()+" construction is allowed only directly inside image phase bodies")
}
```

Constructor context propagation:

```text
ContextImagePhaseDirect propagates through constructor arguments inside the same expression tree.
MemoryPlan(owned_memory: OwnedMemory(...), io_ports: IoPortAuthority()) is valid when the outer expression appears directly in an image phase body.
ContextImagePhaseDirect does not propagate through a method call boundary.
SerialDriver.create_path() hiding SerialWritePath(...) inside the method body is invalid in v0.
ContextOwnershipTransferAuthorityMethod is assigned per method body, not per receiver type. A method body gets this context only when its receiver type is an ownership-transfer authority and the method's own return type is the derived OwnedRoot.
```

- [ ] **Step 4:** Run construction tests; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: enforce DI construction placement -Codex Automated"`.

**Acceptance Criteria:** Helper methods cannot hide `class`, `driver`, `driver path`, or `executor` construction.

### Task 15: Expression Type Checker

**Files:** `compiler/sem/check.go`, `compiler/sem/types_test.go`

- [ ] **Step 1:** Write tests for `while 1 {}` failure, constructor field completeness, method return matching, `for byte in bytes` type, and a method with more than five explicit parameters expecting `SEM0013`.
- [ ] **Step 2:** Run type tests; expect failure.
- [ ] **Step 3:** Implement static method lookup, field lookup, constructor checking, return checking, and `never` checking.

```go
func (c *checker) typeExpr(scope *Scope, expr ast.Expr) (*Type, []diag.Diagnostic) {
    switch e := expr.(type) {
    case *ast.NameExpr:
        return scope.Lookup(e.Name)
    case *ast.FieldExpr:
        base, ds := c.typeExpr(scope, e.Base)
        if len(ds) != 0 { return nil, ds }
        return c.lookupField(base, e.Field, e.Span)
    case *ast.CallExpr:
        recv, ds := c.typeExpr(scope, e.Receiver)
        if len(ds) != 0 { return nil, ds }
        method := c.lookupMethod(recv, e.Method, e.Span)
        c.checkArgs(scope, method.Params, e.Args)
        return method.Return, nil
    case *ast.IntLiteral:
        return c.mustType("U64"), nil
    case *ast.StringLiteral:
        return c.mustType("StringLiteral"), nil
    case *ast.BinaryExpr:
        left, _ := c.typeExpr(scope, e.Left)
        right, _ := c.typeExpr(scope, e.Right)
        c.requireSame(left, right, e.Span)
        if isComparison(e.Op) { return c.mustType("Bool"), nil }
        return left, nil
    default:
        return nil, []diag.Diagnostic{c.diag(e.Span(), diag.CG0001, "unsupported expression")}
    }
}
```
- [ ] **Step 4:** Run type tests; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: typecheck Wrela expressions -Codex Automated"`.

**Acceptance Criteria:** Infinite `while true {}` typechecks as `never` only when the containing function returns `never`; v0 rejects any method, phase, start function, or asm method with more than five explicit parameters with `SEM0013`.

### Task 16: Delegated-Only Propagation

**Files:** `compiler/sem/check.go`, `compiler/sem/delegated_only_test.go`

- [ ] **Step 1:** Write test returning delegated-only wrapper from `phase delegated_hardware`; expect `SEM0009`.
- [ ] **Step 2:** Run delegated-only tests; expect failure.
- [ ] **Step 3:** Implement recursive delegated-only marking: explicit delegated-only types and any type containing delegated-only fields.

```go
var explicitDelegatedOnly = map[string]bool{
    "DelegatedHardware": true,
    "UefiBootServices": true,
    "DelegatedMemory": true,
    "DelegatedBytes": true,
    "DelegatedMutableBytes": true,
    "UefiMemoryMap": true,
    "UefiMemoryMapResult": true,
}

func (idx *Index) IsDelegatedOnly(t *Type, seen map[string]bool) bool {
    if t == nil { return false }
    if explicitDelegatedOnly[t.Name] { return true }
    key := t.Module + "." + t.Name
    if seen[key] { return false }
    seen[key] = true
    for _, f := range t.Fields {
        if idx.IsDelegatedOnly(f.Type, seen) {
            return true
        }
    }
    return false
}
```
- [ ] **Step 4:** Run delegated-only tests; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: enforce delegated-only propagation -Codex Automated"`.

**Acceptance Criteria:** `DelegatedHardware`, `UefiBootServices`, `DelegatedMemory`, `DelegatedBytes`, `DelegatedMutableBytes`, `UefiMemoryMap`, and `UefiMemoryMapResult` are delegated-only.

### Task 17: Unique Cardinality

**Files:** `compiler/sem/authority.go`, `compiler/sem/unique_test.go`

- [ ] **Step 1:** Write test constructing `SerialDriver` twice; expect `SEM0007`.
- [ ] **Step 2:** Run unique tests; expect failure.
- [ ] **Step 3:** Count constructor sites per `unique` declaration in the image graph.

```go
func (c *checker) checkUniqueConstructors(graph ImageGraph) {
    counts := map[*Type][]source.Span{}
    for _, node := range graph.Constructed {
        if node.Type.Unique {
            counts[node.Type] = append(counts[node.Type], node.Span)
        }
    }
    for typ, spans := range counts {
        if len(spans) <= 1 {
            continue
        }
        for _, span := range spans[1:] {
            c.error(span, diag.SEM0007, "unique type "+typ.Name+" is constructed more than once")
        }
    }
}
```

`ImageGraph.Constructed` is populated by walking only syntactically visible constructors in image phase bodies. It does not inspect arbitrary helper methods for hidden construction; those are rejected earlier by Task 14.
- [ ] **Step 4:** Run unique tests; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: validate unique singleton cardinality -Codex Automated"`.

**Acceptance Criteria:** Multiple `driver path` values are allowed unless they are declared `unique`.

### Task 18: OwnedHardware Minting

**Files:** `compiler/sem/authority.go`, `compiler/sem/owned_mint_test.go`

- [ ] **Step 1:** Write test for direct `OwnedHardware(memory: memory, io_ports: io_ports, vcpu0: vcpu0)` in `phase owned_hardware`; expect `SEM0008`.
- [ ] **Step 2:** Run mint tests; expect failure.
- [ ] **Step 3:** Permit the derived owned root only as return from a method on ownership-transfer authority called in `phase delegated_hardware`.

```go
func (c *checker) isOwnershipTransferAuthority(typ *Type) bool {
    return typ != nil && typ.DelegatedOnly && typ.Unique && typ.Kind == KindClass && hasMethodReturning(typ, c.ownedRoot)
}

func (c *checker) checkOwnedMint(call ast.CallExpr, recvType *Type, returnType *Type, phase string) {
    if returnType != c.ownedRoot {
        return
    }
    if phase == "delegated_hardware" && c.isOwnershipTransferAuthority(recvType) {
        return
    }
    c.error(call.Span, diag.SEM0008, c.ownedRoot.Name+" can only be minted through ownership-transfer authority in phase delegated_hardware")
}
```

This check is structural. It uses the derived `c.ownedRoot` type pointer and the receiver's ownership-transfer authority properties; it must not compare a type name string to `OwnedHardware`.
- [ ] **Step 4:** Run mint tests; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: validate owned hardware minting -Codex Automated"`.

**Acceptance Criteria:** Method name is not magic; receiver authority and return type drive the rule.

### Task 19: Driver Path Ownership Graph

**Files:** `compiler/sem/image_graph.go`, `compiler/sem/authority.go`, `compiler/sem/path_graph_test.go`

- [ ] **Step 1:** Write tests for root driver into executor (`SEM0010`) and path assigned twice (`SEM0011`).
- [ ] **Step 2:** Run graph tests; expect failure.
- [ ] **Step 3:** Extract graph nodes from image phase constructors and validate executor fields.

```go
func (c *checker) checkExecutorWiring(graph ImageGraph) {
    pathOwners := map[string]string{}
    for _, exec := range graph.Executors {
        for _, field := range exec.Type.Fields {
            if field.Type.Kind == KindDriver {
                c.error(field.Span, diag.SEM0010, "root driver "+field.Type.Name+" cannot be passed into executor "+exec.Type.Name)
            }
            if field.Type.Kind != KindDriverPath {
                continue
            }
            pathName := exec.FieldBindings[field.Name]
            if prev, exists := pathOwners[pathName]; exists {
                c.error(exec.Span, diag.SEM0011, "driver path "+pathName+" is assigned to more than one executor: "+prev+" and "+exec.Type.Name)
                continue
            }
            pathOwners[pathName] = exec.Type.Name
        }
    }
}
```

`FieldBindings` maps executor field names to the source variable used in the constructor, such as `serial_path` in `HelloWorld(serial_path: serial_path)`.
- [ ] **Step 4:** Run graph tests; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: validate driver path ownership graph -Codex Automated"`.

**Acceptance Criteria:** Every `driver path` instance has exactly one executor owner.

### Task 20: Negative Fixture Harness

**Files:** `compiler/negative_fixtures_test.go`, `tests/fixtures/negative/*.wrela`

- [ ] **Step 1:** Add fixtures listed below with first line `// expect: CODE: exact message`.
- [ ] **Step 2:** Run `go test ./compiler -run TestNegativeFixtures -v`; expect harness missing.
- [ ] **Step 3:** Implement harness that reads expected code/message, compiles through semantic checking, and asserts diagnostics.
- [ ] **Step 4:** Run harness; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "test: add semantic negative fixtures -Codex Automated"`.

**Exact fixtures:** `multiple_images`, `constructor_outside_phase`, `duplicate_unique`, `owned_hardware_illegal_mint`, `delegated_only_escape`, `root_driver_to_executor`, `path_assigned_twice`, `illegal_asm_placement`.

The harness prepends a common semantic-test prelude unless a fixture deliberately redeclares one of those names:

```wrela
unique class OwnedHardware {}
data ExecutorPlacement { id: U64 }
unique class DelegatedHardware {
    fn exit_to_owned_hardware(self) -> OwnedHardware {
        return OwnedHardware()
    }
}
```

Prelude merge rule: before prepending the prelude, scan the fixture's top-level declaration names. Drop any prelude declaration with the same name, then concatenate the remaining prelude declarations before the fixture body. This prevents intentional fixture redeclarations from being masked by `SEM0001`.

Fixture bodies:

```wrela
// expect: SEM0003: import graph contains more than one image
module negative.multiple_images

image A {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.exit_to_owned_hardware()
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        while true {}
    }
}

image B {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.exit_to_owned_hardware()
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        while true {}
    }
}
```

```wrela
// expect: SEM0006: class construction is allowed only directly inside image phase bodies
module negative.constructor_outside_phase

class Other {}

class Helper {
    fn make(self) -> Other {
        return Other()
    }
}

image Bad {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.exit_to_owned_hardware()
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        while true {}
    }
}
```

```wrela
// expect: SEM0007: unique type SerialDriver is constructed more than once
module negative.duplicate_unique

unique driver SerialDriver {}

image Bad {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.exit_to_owned_hardware()
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        let a = SerialDriver()
        let b = SerialDriver()
        while true {}
    }
}
```

```wrela
// expect: SEM0008: OwnedHardware can only be minted through ownership-transfer authority in phase delegated_hardware
module negative.owned_hardware_illegal_mint

unique class OwnedHardware {}

image Bad {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.exit_to_owned_hardware()
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        let other = OwnedHardware()
        while true {}
    }
}
```

```wrela
// expect: SEM0009: delegated-only value DelegatedHardware cannot cross into owned_hardware phase
module negative.delegated_only_escape

data DelegatedLeak { hardware: DelegatedHardware }

image Bad {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> DelegatedLeak {
        return DelegatedLeak(hardware: hardware)
    }

    phase owned_hardware(leak: DelegatedLeak) -> never {
        while true {}
    }
}
```

```wrela
// expect: SEM0010: root driver SerialDriver cannot be passed into executor HelloWorld
module negative.root_driver_to_executor

unique driver SerialDriver {}

executor HelloWorld {
    serial: SerialDriver

    start fn run(self) -> never {
        while true {}
    }
}

image Bad {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.exit_to_owned_hardware()
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        let serial = SerialDriver()
        let hello = HelloWorld(serial: serial)
        hello.run()
    }
}
```

```wrela
// expect: SEM0011: driver path serial_path is assigned to more than one executor
module negative.path_assigned_twice

driver path SerialWritePath { owner: ExecutorPlacement }

executor A {
    serial_path: SerialWritePath

    start fn run(self) -> never {
        while true {}
    }
}

executor B {
    serial_path: SerialWritePath

    start fn run(self) -> never {
        while true {}
    }
}

image Bad {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.exit_to_owned_hardware()
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        let serial_path = SerialWritePath(owner: ExecutorPlacement(id: 0))
        let a = A(serial_path: serial_path)
        let b = B(serial_path: serial_path)
        a.run()
    }
}
```

```wrela
// expect: SEM0012: asm methods are only allowed on edge-capability declarations
module negative.illegal_asm_placement

class Plain {
    value: U64

    asm fn bad(self) {
        hlt
        ret
    }
}

image Bad {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        return hardware.exit_to_owned_hardware()
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        while true {}
    }
}
```

### Task 21: Asm Registers And Operand AST

**Files:** `compiler/asm/regs.go`, `compiler/asm/ast.go`, `compiler/asm/regs_test.go`

- [ ] **Step 1:** Write tests for register lookup: `rax`, `al`, `r8`, `r15b`, `cr3`.
- [ ] **Step 2:** Run asm tests; expect failure.
- [ ] **Step 3:** Implement Appendix A register table and operand AST types.
- [ ] **Step 4:** Run `go test ./compiler/asm -run TestRegister -v`; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: add x64 asm register model -Codex Automated"`.

**Acceptance Criteria:** Registers expose width, low 3-bit code, and high-register extension bit.

### Task 22: Asm Parser

**Files:** `compiler/asm/parse.go`, `compiler/asm/parse_test.go`

- [ ] **Step 1:** Write parser test for `mov dx, self.port_base; add dx, offset; out dx, al; ret`.
- [ ] **Step 2:** Run parser test; expect failure.
- [ ] **Step 3:** Implement instruction, label, register, immediate, field operand, param operand parsing.

Disambiguation rules:

```text
identifier followed by ":" at start of asm line -> label declaration
identifier used as branch target in jmp/je/jne/jl/jle/jg/jge -> label reference
"self" "." identifier -> field operand
identifier matching a method parameter -> parameter operand
identifier matching a register name -> register operand
integer literal -> immediate operand
any other bare identifier in operand position -> ASM0002
```

Example:

```asm
loop:
    mov al, value      ; value is a parameter operand
    out dx, al         ; dx/al are register operands
    jmp loop           ; loop is a label reference
```
- [ ] **Step 4:** Run parser tests; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: parse x64 asm methods -Codex Automated"`.

**Acceptance Criteria:** Unknown instruction returns `ASM0001`; invalid operand returns `ASM0002`; the exported parser entrypoint is `asm.ParseBody(source, params)`.

### Task 23: REX And ModRM Encoding Core

**Files:** `compiler/asm/encode.go`, `compiler/asm/encode_core_test.go`

- [ ] **Step 1:** Write exact-byte tests from Appendix A for `ret`, `hlt`, `pause`, `out dx, al`, `mov cr3, rax`, `lgdt [rax]`.
- [ ] **Step 2:** Run encode tests; expect failure.
- [ ] **Step 3:** Implement REX, ModRM, and fixed opcode encoders.
- [ ] **Step 4:** Run encode tests; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: encode core x64 instructions -Codex Automated"`.

**Acceptance Criteria:** No instruction test relies on external assembler output.

### Task 24: Branch Labels And Fixups

**Files:** `compiler/asm/encode.go`, `compiler/asm/branch_test.go`

- [ ] **Step 1:** Write test for `loop: hlt; jmp loop` and assert `F4 E9 <rel32>`.
- [ ] **Step 2:** Run branch tests; expect failure.
- [ ] **Step 3:** Implement label table and rel32 fixups.
- [ ] **Step 4:** Run branch tests; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: encode asm branch fixups -Codex Automated"`.

**Acceptance Criteria:** Conditional branches use near rel32 forms in Appendix A.

### Task 25: Typed IR Data Structures

**Files:** `compiler/ir/ir.go`, `compiler/ir/ir_test.go`

- [ ] **Step 1:** Write compile-time tests for `Function`, `Block`, `ConstInt`, `Binary`, `Call`, `Branch`, `ForBytes`.
- [ ] **Step 2:** Run IR tests; expect failure.
- [ ] **Step 3:** Implement IR structs from Section 2 contract.
- [ ] **Step 4:** Run IR tests; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: add typed IR model -Codex Automated"`.

**Acceptance Criteria:** `AsmMethod` is represented but not lowered through normal IR; `EntryAdapter` records the generated UEFI entry symbol plus delegated and owned phase symbols; `Function.ValuesInDeterministicOrder()` walks blocks in declaration order and returns unique value definitions by first appearance.

### Task 26: Lower Expressions And Statements To IR

**Files:** `compiler/ir/lower.go`, `compiler/ir/lower_test.go`

- [ ] **Step 1:** Write snapshot tests for lowering `for byte in bytes` to index loop and for synthesizing the image `EntryAdapter`.
- [ ] **Step 2:** Run lower tests; expect failure.
- [ ] **Step 3:** Implement lowering for constants, field access, calls, if, while, return, ForBytes, and the generated image entry adapter.
- [ ] **Step 4:** Run lower tests; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: lower Wrela AST to IR -Codex Automated"`.

**Acceptance Criteria:** Unsupported AST returns `CG0001`; the generated adapter calls `delegated_hardware` first, passes its returned `OwnedHardware` into `owned_hardware`, and uses `_wrela_efi_entry` as its symbol.

### Task 27: Codegen Prologue, Epilogue, Constants, Binary Ops

**Files:** `compiler/codegen/abi.go`, `compiler/codegen/x64.go`, `compiler/codegen/basic_test.go`

- [ ] **Step 1:** Write test compiling `return 42` and assert prologue plus `mov rax, 42`.
- [ ] **Step 2:** Run codegen tests; expect failure.
- [ ] **Step 3:** Implement Appendix H stack-frame codegen and scratch registers `rax`, `r10`, `r11`; reserve `r10` while preparing calls that return `data`.

```go
type Frame struct {
    Slots map[ir.Value]int
    Size int
}

func buildFrame(fn ir.Function) Frame {
    offset := 0
    slots := map[ir.Value]int{}
    for _, v := range fn.ValuesInDeterministicOrder() {
        offset += 8
        slots[v] = -offset
    }
    return Frame{Slots: slots, Size: layout.AlignUp(offset, 16)}
}

func emitPrologue(e *Emitter, frame Frame) {
    e.Bytes(0x55)                         // push rbp
    e.Bytes(0x48, 0x89, 0xE5)             // mov rbp, rsp
    if frame.Size != 0 {
        e.Bytes(0x48, 0x81, 0xEC)
        e.Uint32(uint32(frame.Size))      // sub rsp, frame_size
    }
}

func emitEpilogue(e *Emitter) {
    e.Bytes(0x48, 0x89, 0xEC)             // mov rsp, rbp
    e.Bytes(0x5D)                         // pop rbp
    e.Bytes(0xC3)                         // ret
}
```
- [ ] **Step 4:** Run basic codegen tests; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: generate basic x64 functions -Codex Automated"`.

**Acceptance Criteria:** Stack is 16-byte aligned before calls.

### Task 28: Codegen Field Loads And Record Offsets

**Files:** `compiler/codegen/x64.go`, `compiler/codegen/field_test.go`

- [ ] **Step 1:** Write test loading field `port_base` at offset 0 from `self`.
- [ ] **Step 2:** Run field tests; expect failure.
- [ ] **Step 3:** Use `compiler/layout` offsets to emit memory operands.
- [ ] **Step 4:** Run field tests; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: generate field access code -Codex Automated"`.

**Acceptance Criteria:** Field layout is not duplicated in codegen.

### Task 29: Codegen Calls, Branches, While, ForBytes

**Files:** `compiler/codegen/x64.go`, `compiler/codegen/control_test.go`

- [ ] **Step 1:** Write tests for static call relocation, `while`, and `ForBytes`.
- [ ] **Step 2:** Run control tests; expect failure.
- [ ] **Step 3:** Implement ABI arg moves, rel32 call relocations, branch labels, and byte loads from `Bytes.address`.

```go
var argRegs = []asm.Reg{asm.RDI, asm.RSI, asm.RDX, asm.RCX, asm.R8, asm.R9}

func emitCall(e *Emitter, call ir.Call, sym string) []diag.Diagnostic {
    values := append([]ir.Value{call.Receiver}, call.Args...)
    if len(values) > len(argRegs) {
        return []diag.Diagnostic{e.Diag(call.Span, diag.SEM0013, "v0 ABI supports at most five explicit parameters")}
    }
    for i, v := range values {
        emitLoadValue(e, asm.RAX, v)
        emitMovRegReg(e, argRegs[i], asm.RAX)
    }
    relocOffset := e.Len() + 1
    e.Bytes(0xE8, 0, 0, 0, 0) // call rel32
    e.Relocs = append(e.Relocs, Reloc{Kind: RelocCallRel32, Offset: relocOffset, Symbol: sym})
    return nil
}

func emitForBytes(e *Emitter, loop ir.ForBytes) {
    // bytes layout: address at +0, length at +8
    // index slot is U64; byte result is U8 zero-extended into rax.
    emitConst(e, loop.Index, 0)
    start := e.NewLabel("for_bytes_start")
    done := e.NewLabel("for_bytes_done")
    e.Bind(start)
    emitCompareIndexToLength(e, loop.Index, loop.Iterable)
    emitJGE(e, done)
    emitLoadByteAtAddressPlusIndex(e, asm.RAX, loop.Iterable, loop.Index)
    emitStoreValue(e, loop.ByteValue, asm.RAX)
    emitOps(e, loop.Body)
    emitInc(e, loop.Index)
    emitJMP(e, start)
    e.Bind(done)
}
```

Rel32 call relocations are internal compiler relocations resolved during code layout. They do not become PE `.reloc` entries. PE `.reloc` only records absolute image-base relocations such as DIR64.
- [ ] **Step 4:** Run control tests; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: generate calls and control flow -Codex Automated"`.

**Acceptance Criteria:** `ForBytes` loads `U8` and increments a `U64` index; normal Wrela calls never push stack arguments in v0 because `SEM0013` rejects signatures that exceed the register ABI.

### Task 30: Lower Asm Methods With Wrela-Bound Operands

**Files:** `compiler/codegen/asm_method.go`, `compiler/codegen/asm_method_test.go`

- [ ] **Step 1:** Write test compiling `SerialWriterRegisters.write8` and assert output contains byte `EE`.
- [ ] **Step 2:** Run asm method tests; expect failure.
- [ ] **Step 3:** Materialize params and `self.field` operands before handing instructions to encoder.

```go
func lowerBoundOperand(ctx MethodContext, dst asm.Reg, op asm.Operand) ([]asm.Instruction, error) {
    switch o := op.(type) {
    case asm.ParamOperand:
        loc := ctx.ParamLocation(o.Name)
        return moveLocationToReg(dst, loc), nil
    case asm.FieldOperand:
        if o.Base != "self" {
            return nil, fmt.Errorf("ASM0002: only self.field is supported in v0 asm")
        }
        off := ctx.RecordLayout.Offset(o.Field)
        return []asm.Instruction{asm.Mov(dst, asm.Mem{Base: asm.RDI, Disp: off, Width: ctx.FieldWidth(o.Field)})}, nil
    default:
        return nil, nil
    }
}
```

Task 30 depends on Task 27 for ABI parameter locations and Task 11 for field offsets.
- [ ] **Step 4:** Run asm method tests; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: lower Wrela-bound asm methods -Codex Automated"`.

**Acceptance Criteria:** `mov dx, self.port_base` uses record layout offset for `port_base`.

### Task 31: PE DOS Header And COFF Header

**Files:** `compiler/pecoff/image.go`, `compiler/pecoff/write.go`, `compiler/pecoff/header_test.go`

- [ ] **Step 1:** Write test asserting `MZ`, `e_lfanew=0x80`, `PE\0\0`, machine `0x8664`.
- [ ] **Step 2:** Run PE tests; expect failure.
- [ ] **Step 3:** Implement DOS and COFF headers from Appendix B.
- [ ] **Step 4:** Run header tests; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: emit PE COFF headers -Codex Automated"`.

**Acceptance Criteria:** Timestamp is zero for deterministic builds.

### Task 32: PE Sections And Optional Header

**Files:** `compiler/pecoff/write.go`, `compiler/pecoff/sections_test.go`

- [ ] **Step 1:** Write test asserting subsystem 10, optional magic `0x20B`, `.text` RVA `0x1000`.
- [ ] **Step 2:** Run section tests; expect failure.
- [ ] **Step 3:** Implement optional header and section table from Appendix B.
- [ ] **Step 4:** Run section tests; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: emit EFI PE sections -Codex Automated"`.

**Acceptance Criteria:** File alignment `0x200`; section alignment `0x1000`.

### Task 33: PE Relocation Table And Entry Wiring

**Files:** `compiler/pecoff/reloc.go`, `compiler/pecoff/reloc_test.go`

- [ ] **Step 1:** Write test for one DIR64 relocation in `.text`.
- [ ] **Step 2:** Run reloc tests; expect failure.
- [ ] **Step 3:** Implement `.reloc` blocks and entry symbol RVA wiring.
- [ ] **Step 4:** Run reloc tests; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: emit EFI relocations -Codex Automated"`.

**Acceptance Criteria:** `.reloc` is omitted only when there are no relocations; PE `AddressOfEntryPoint` points at the generated `_wrela_efi_entry` adapter.

### Task 34: Platform UEFI Types Source

**Files:** `wrela/platform/uefi/types.wrela`

- [ ] **Step 1:** Add parser test loading the exact source from Section 4.
- [ ] **Step 2:** Run parser test; expect missing file.
- [ ] **Step 3:** Create `types.wrela` exactly as Section 4 specifies.
- [ ] **Step 4:** Run parser/sem tests; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: add UEFI Wrela types -Codex Automated"`.

**Acceptance Criteria:** No UEFI magic names in compiler code; source declares the types.

### Task 35: Executor Memory Source

**Files:** `wrela/machine/x86_64/executor_memory.wrela`

- [ ] **Step 1:** Add parser/sem fixture for `Bytes`, `MutableBytes`, `ExecutorMemory`.
- [ ] **Step 2:** Run fixture test; expect missing file.
- [ ] **Step 3:** Create source from Section 4.
- [ ] **Step 4:** Run parser/sem tests; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: add executor memory source -Codex Automated"`.

**Acceptance Criteria:** `static_bytes` uses `StringLiteral.address` and `.length`.

### Task 36: CPU And Port IO Assembly Source

**Files:** `wrela/arch/x86_64/cpu.wrela`, `wrela/arch/x86_64/io.wrela`

- [ ] **Step 1:** Add fixture requiring `pause`, `halt_forever`, `out8`, `in8`.
- [ ] **Step 2:** Run fixture test; expect missing files.
- [ ] **Step 3:** Create assembly methods using Appendix A instructions.
- [ ] **Step 4:** Run parser/asm tests; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: add x64 CPU and IO source -Codex Automated"`.

**Acceptance Criteria:** No top-level asm functions exist.

### Task 37: Serial Driver Source

**Files:** `wrela/machine/x86_64/serial.wrela`

- [ ] **Step 1:** Add fixture for `SerialDriver.initialize` and `SerialWritePath.write`.
- [ ] **Step 2:** Run fixture test; expect missing file.
- [ ] **Step 3:** Create source with COM1 initialization and polling loop.
- [ ] **Step 4:** Run parser/sem/codegen asm tests; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: add COM1 serial driver source -Codex Automated"`.

**Acceptance Criteria:** `SerialDriver` is `unique driver`; `SerialWritePath` is `driver path`; serial source does not hide `SerialWritePath` or `SerialWriterRegisters` construction inside helper/factory methods.

### Task 38: Machine CPU State Source

**Files:** `wrela/machine/x86_64/cpu_state.wrela`

- [ ] **Step 1:** Add fixture for `OwnedHardware`, `OwnedMemory`, `MemoryPlan.io_ports`, `IoPortAuthority`, and `ExecutorPlacement`.
- [ ] **Step 2:** Run fixture test; expect missing file.
- [ ] **Step 3:** Create source from Section 4 and import serial/executor memory modules as needed.
- [ ] **Step 4:** Run sem tests; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: add machine CPU state source -Codex Automated"`.

**Acceptance Criteria:** `OwnedHardware` has no dependency on example executors; `MemoryPlan` carries `io_ports` so ownership transfer does not construct `IoPortAuthority`; executor launch is `hello.run()` from the image `owned_hardware` phase.

### Task 39: UEFI Boot Services MS-ABI Bridge

**Files:** `wrela/platform/uefi/boot_services.wrela`

- [ ] **Step 1:** Add asm parse/codegen fixture for `get_memory_map` and `exit_boot_services`.
- [ ] **Step 2:** Run fixture; expect missing file.
- [ ] **Step 3:** Implement bridge using Appendix D: shadow space, `rcx`, `rdx`, `r8`, `r9`, 16-byte stack alignment.
- [ ] **Step 4:** Run asm/codegen tests; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: add UEFI boot services bridge -Codex Automated"`.

**Acceptance Criteria:** UEFI ABI details live in Wrela assembly methods, not compiler intrinsics; `get_memory_map` writes its `UefiMemoryMapResult` into the Appendix H `r10` data-return slot and returns that handle in `rax`.

### Task 40a: Delegated Memory Map Retry And Ownership Transfer

**Files:** `wrela/platform/uefi/types.wrela`, `wrela/platform/uefi/boot_services.wrela`, `wrela/platform/uefi/transition.wrela`, `wrela/platform/uefi/transition_memory_test.wrela`

- [ ] **Step 1:** Add a fixture compiling `DelegatedHardware.exit_to_owned_hardware` with a GetMemoryMap retry loop and expected `OwnedHardware` return.
- [ ] **Step 2:** Run the fixture; expect missing transition implementation.
- [ ] **Step 3:** Implement the source-visible sequence: allocate a first buffer, call `get_memory_map`, grow the buffer using the returned required size until `EFI_BUFFER_TOO_SMALL` clears, call `exit_boot_services` with the returned `map_key`, and repeat `GetMemoryMap` plus `ExitBootServices` if the map key is stale.

Required Wrela shape:

```wrela
fn exit_to_owned_hardware(self, memory_plan: MemoryPlan, virtual_memory_plan: VirtualMemoryPlan, cpu_plan: CpuPlan) -> OwnedHardware {
    let map_buffer = self.delegated_memory.allocate(length: 16384)
    let active_buffer = map_buffer
    let map_result = self.boot_services.get_memory_map(buffer: active_buffer)

    while map_result.status.value == 0x8000000000000005 {
        active_buffer = self.delegated_memory.allocate(length: map_result.required_size + 4096)
        map_result = self.boot_services.get_memory_map(buffer: active_buffer)
    }

    let final_map = map_result.memory_map
    let exit_status = self.boot_services.exit_boot_services(image: self.image_handle, map_key: final_map.key)

    while exit_status.value == 0x8000000000000002 {
        map_result = self.boot_services.get_memory_map(buffer: active_buffer)
        while map_result.status.value == 0x8000000000000005 {
            active_buffer = self.delegated_memory.allocate(length: map_result.required_size + 4096)
            map_result = self.boot_services.get_memory_map(buffer: active_buffer)
        }
        let retry_map = map_result.memory_map
        exit_status = self.boot_services.exit_boot_services(image: self.image_handle, map_key: retry_map.key)
    }

    return OwnedHardware(memory: memory_plan.owned_memory, io_ports: memory_plan.io_ports, vcpu0: cpu_plan.vcpu0)
}
```

Status values:

```text
EFI_SUCCESS = 0
EFI_INVALID_PARAMETER = 0x8000000000000002
EFI_BUFFER_TOO_SMALL = 0x8000000000000005
```

No allocation may occur between the final successful `get_memory_map` and its paired `exit_boot_services` call. If `exit_boot_services` returns `EFI_INVALID_PARAMETER`, the code calls `get_memory_map` again, grows the buffer only if `EFI_BUFFER_TOO_SMALL` requires it, and retries with the newly returned key. `get_memory_map` returns a `UefiMemoryMapResult`; the map key and required buffer size always come from that explicit result, not from hidden compiler state.

- [ ] **Step 4:** Run semantic tests; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: add delegated ownership transfer path -Codex Automated"`.

**Acceptance Criteria:** `OwnedHardware` minting remains valid only inside this ownership-transfer authority path.

### Task 40b: Identity Paging And GDT Segment Reload

**Files:** `wrela/platform/uefi/types.wrela`, `wrela/platform/uefi/transition.wrela`, `compiler/codegen/transition_test.go`

- [ ] **Step 1:** Add a codegen test that compiles the transition and asserts generated bytes contain `mov cr3`, `lgdt`, `retfq`, and segment reload instructions from Appendix C.
- [ ] **Step 2:** Run the test; expect missing transition assembly.
- [ ] **Step 3:** Add source-owned builders for identity page tables and GDT bytes, plus assembly methods for loading `cr3`, loading `gdtr`, reloading `cs` via `push selector; push target; retfq`, and reloading `ds/es/ss/fs/gs`.

The identity page tables are built by `DelegatedMemory.build_identity_paging(memory_map: UefiMemoryMap)`, not by compiler magic. The builder writes a PML4, PDPT, and PD using 2 MiB PDEs with Appendix C's `P|RW|PS` flags, maps all ranges needed by the loaded image and runtime structures, and returns the PML4 physical address used as `cr3`.

The GDT bytes are built by `DelegatedMemory.build_owned_gdt()`, not by compiler magic. The builder writes the null descriptor, owned code descriptor, and owned data descriptor using Appendix C's descriptor values, then returns a `DelegatedBytes` view used as the GDTR base/limit source.
- [ ] **Step 4:** Run transition codegen tests; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: add identity paging and GDT transition -Codex Automated"`.

**Acceptance Criteria:** The plan includes a segment-cache reload after `lgdt`; `lgdt` alone is not accepted.

### Task 40c: Fatal IDT And Exception Handler

**Files:** `wrela/platform/uefi/types.wrela`, `wrela/platform/uefi/transition.wrela`, `compiler/codegen/fatal_idt_test.go`

- [ ] **Step 1:** Add a test asserting the IDT builder emits 256 gates and a fatal handler containing `hlt`.
- [ ] **Step 2:** Run the test; expect missing IDT builder.
- [ ] **Step 3:** Implement IDT gate construction using Appendix C and one fatal handler for all vectors.

The IDT bytes are built by `DelegatedMemory.build_fatal_idt(fatal_handler: VirtualAddress)`, not by compiler magic. The builder writes 256 16-byte gates pointing at the fatal handler and returns a `DelegatedBytes` view used as the IDTR base/limit source.
- [ ] **Step 4:** Run fatal IDT tests; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: add fatal IDT transition handler -Codex Automated"`.

**Acceptance Criteria:** All CPU exception vectors point to the fatal handler; device interrupts remain disabled.

### Task 40d: Owned Stack Switch And Final Transition Assembly

**Files:** `wrela/platform/uefi/transition.wrela`, `compiler/codegen/transition_test.go`

- [ ] **Step 1:** Add a codegen test asserting the final transition contains `cli`, stack switch to `cpu_plan.owned_stack_top`, `mov cr3`, `lgdt`, `lidt`, and the segment reload sequence.
- [ ] **Step 2:** Run the test; expect missing final stack switch.
- [ ] **Step 3:** Implement final assembly method that disables interrupts, switches `rsp`, loads paging and descriptor tables, reloads segments, and returns `OwnedHardware`.
- [ ] **Step 4:** Run sem/codegen tests; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: complete delegated to owned transition -Codex Automated"`.

**Acceptance Criteria:** Generated transition contains `cli`, `mov cr3`, `lgdt`, `retfq`, `lidt`, segment register reloads, and an owned stack switch.

### Task 40e: UEFI Entry Adapter

**Files:** `compiler/codegen/entry_adapter.go`, `compiler/codegen/entry_adapter_test.go`

- [ ] **Step 1:** Add a codegen test asserting `_wrela_efi_entry` receives `rcx` image handle and `rdx` system table, materializes `DelegatedHardware`, calls `delegated_hardware`, passes the returned `OwnedHardware` to `owned_hardware`, and emits a halt path if `owned_hardware` returns.
- [ ] **Step 2:** Run the test; expect missing entry adapter codegen.
- [ ] **Step 3:** Implement the `ir.EntryAdapter` lowering using Appendix D for the UEFI incoming ABI and Appendix H for calls into Wrela phase functions.
- [ ] **Step 4:** Run entry adapter tests; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: add UEFI entry adapter -Codex Automated"`.

**Acceptance Criteria:** PE `AddressOfEntryPoint` targets `_wrela_efi_entry`; no Wrela source phase receives raw UEFI entry arguments; the adapter is the only place that converts firmware entry state into `DelegatedHardware`.

### Task 41: Build Pipeline Integration

**Files:** `compiler/build.go`, `compiler/integration_test.go`, `examples/hello/main.wrela`, `examples/hello/program.wrela`

- [ ] **Step 1:** Write integration test building `examples/hello/main.wrela` to a temp `.efi`.
- [ ] **Step 2:** Run integration test; expect `INT0001`.
- [ ] **Step 3:** Wire source graph, parser, sem, IR, codegen, PE writer in `Build`.

```go
func Build(opts BuildOptions) (BuildResult, error) {
    if opts.Mode == ModeRelease {
        return BuildResult{}, NewCodeError("CLI0002", "release mode is not implemented in v0")
    }
    graph, err := source.LoadGraph(source.Options{
        RootPath: opts.RootPath,
        ImportRoots: []string{opts.RepoRoot, filepath.Join(opts.RepoRoot, "wrela")},
    })
    if err != nil { return BuildResult{}, err }
    modules, ds := parse.ParseGraph(graph)
    if len(ds) != 0 { return BuildResult{}, DiagnosticError{Diagnostics: ds} }
    index, ds := sem.BuildIndex(modules)
    if len(ds) != 0 { return BuildResult{}, DiagnosticError{Diagnostics: ds} }
    checked, ds := sem.Check(index, modules)
    if len(ds) != 0 { return BuildResult{}, DiagnosticError{Diagnostics: ds} }
    program, ds := ir.Lower(checked)
    if len(ds) != 0 { return BuildResult{}, DiagnosticError{Diagnostics: ds} }
    image, ds := codegen.Compile(program)
    if len(ds) != 0 { return BuildResult{}, DiagnosticError{Diagnostics: ds} }
    bytes, err := pecoff.WriteEFI(image)
    if err != nil { return BuildResult{}, err }
    if err := os.WriteFile(opts.OutputPath, bytes, 0o644); err != nil { return BuildResult{}, err }
    return BuildResult{OutputPath: opts.OutputPath}, nil
}
```
- [ ] **Step 4:** Run `go test ./compiler -run TestBuildHello -v`; expect `PASS`.
- [ ] **Step 5:** Commit with `git commit -m "feat: wire compiler build pipeline -Codex Automated"`.

**Acceptance Criteria:** `Build` calls source graph, parse, semantic check, IR lowering, codegen, and PE writing in order; diagnostics propagate without being string-parsed; release mode still fails with `CLI0002`.

### Task 42: QEMU Harness

**Files:** `compiler/qemu/run.go`, `compiler/qemu/run_test.go`, `tests/e2e/hello_qemu_test.go`, `scripts/run-hello-qemu.sh`

- [ ] **Step 1:** Write command-construction unit test and e2e skip test.
- [ ] **Step 2:** Run `go test ./compiler/qemu ./tests/e2e -v`; expect missing package.
- [ ] **Step 3:** Implement QEMU command, ESP staging, serial capture, clean skips when env vars are absent.
- [ ] **Step 4:** Run tests; without env vars expect `SKIP`, with env vars expect serial line.
- [ ] **Step 5:** Commit with `git commit -m "test: add QEMU hello image test -Codex Automated"`.

**Acceptance Criteria:** Success signal is post-owned-hardware serial output `hello from wrela`; command arguments match Appendix G exactly unless the test overrides memory size.

### Task 43: Production Deferred Work Register

**Files:** `docs/production-deferred-work.md`

- [ ] **Step 1:** Write a doc existence test or shell check in the task PR description.
- [ ] **Step 2:** Run `test -f docs/production-deferred-work.md`; expect failure.
- [ ] **Step 3:** Create the document with every section in Section 8.
- [ ] **Step 4:** Run `rg -n "required for production|not optional" docs/production-deferred-work.md`; expect matches.
- [ ] **Step 5:** Commit with `git commit -m "docs: capture production deferred work -Codex Automated"`.

**Acceptance Criteria:** Every deferred item states production need, v0 exclusion reason, and what v0 must not block.

---

## 7. Appendices

### Appendix A: x86_64-v3 Encoding Table

Register low 3-bit codes:

```text
rax/eax/ax/al=0 rcx/ecx/cx/cl=1 rdx/edx/dx/dl=2 rbx/ebx/bx/bl=3
rsp/esp/sp/spl=4 rbp/ebp/bp/bpl=5 rsi/esi/si/sil=6 rdi/edi/di/dil=7
r8=0+r8bit r9=1+r8bit r10=2+r8bit r11=3+r8bit r12=4+r8bit r13=5+r8bit r14=6+r8bit r15=7+r8bit
```

REX:

```text
0100WRXB
W=64-bit operand
R=extends ModRM reg
X=extends SIB index
B=extends ModRM r/m or opcode reg
```

Exact required encodings:

```text
ret                         C3
hlt                         F4
pause                       F3 90
cli                         FA
sti                         FB
out dx, al                  EE
in al, dx                   EC
mov cr3, rax                0F 22 D8
mov rax, cr3                0F 20 D8
lgdt [rax]                  0F 01 10
lidt [rax]                  0F 01 18
push rbp                    55
pop rbp                     5D
push imm8                   6A <imm8>
retfq                       48 CB
mov rbp, rsp                48 89 E5
mov rsp, rbp                48 89 EC
mov ds, ax                  8E D8
mov es, ax                  8E C0
mov ss, ax                  8E D0
mov fs, ax                  8E E0
mov gs, ax                  8E E8
sub rsp, imm32              48 81 EC <imm32le>
add rsp, imm32              48 81 C4 <imm32le>
mov r64, imm64              48 B8+rd <imm64le> with REX.B for r8-r15
mov r16, imm16              66 B8+rw <imm16le>
mov r/m64, r64              48 89 /r
mov r64, r/m64              48 8B /r
mov r/m16, r16              66 89 /r
mov r16, r/m16              66 8B /r
mov r/m8, r8                88 /r
mov r8, r/m8                8A /r
add r/m16, r16              66 01 /r
add r16, imm16              66 81 /0 <imm16le>
cmp r/m64, r64              48 39 /r
cmp r64, imm32              48 81 /7 <imm32le>
jmp rel32                   E9 <rel32le>
je rel32                    0F 84 <rel32le>
jne rel32                   0F 85 <rel32le>
jl rel32                    0F 8C <rel32le>
jle rel32                   0F 8E <rel32le>
jg rel32                    0F 8F <rel32le>
jge rel32                   0F 8D <rel32le>
call rel32                  E8 <rel32le>
call r/m64                  FF /2
lea r64, [rip+disp32]       48 8D 05 <disp32le> for rax; use /r for others
```

ModRM:

```text
mod reg r/m
mod=11 register-direct
mod=00 memory no displacement except rbp/r13 require disp32
mod=01 disp8
mod=10 disp32
```

Register-direct ModRM algorithm:

```go
func modRMRegReg(reg, rm Register) (rexR bool, rexB bool, modrm byte) {
    rexR = reg.High
    rexB = rm.High
    modrm = 0b11000000 | (reg.Low3 << 3) | rm.Low3
    return rexR, rexB, modrm
}
```

For `mov r/m64, r64` (`48 89 /r`), `reg` is the source register and `rm` is the destination register or memory base. For `mov r64, r/m64` (`48 8B /r`), `reg` is the destination register and `rm` is the source register or memory base.

Example:

```text
mov rax, rbx
opcode 48 8B /r
reg = rax low3 0
rm = rbx low3 3
modrm = 11 000 011 = C3
bytes = 48 8B C3
```

RIP-relative `lea r64, [rip+disp32]` algorithm:

```text
opcode: 48 8D /r
mod = 00
r/m = 101 means RIP-relative
reg = destination low3
REX.R = destination high bit
disp32 = target_rva - next_instruction_rva
```

### Appendix B: PE32+ EFI Layout

DOS header:

```text
MZ magic at 0x00: 4D 5A
e_lfanew at 0x3c: 0x80
DOS stub bytes from 0x40 to 0x7f may be zero
PE signature at 0x80: 50 45 00 00
```

COFF header:

```text
Machine: 0x8664
NumberOfSections: count
TimeDateStamp: 0 for deterministic builds
PointerToSymbolTable: 0
NumberOfSymbols: 0
SizeOfOptionalHeader: 0xF0
Characteristics: 0x2022 (executable, large-address-aware, dll bit clear)
```

Optional header PE32+:

```text
Magic: 0x20B
MajorLinkerVersion: 0
MinorLinkerVersion: 0
SizeOfCode: aligned .text virtual size
SizeOfInitializedData: aligned .rdata + .data + .reloc virtual sizes
AddressOfEntryPoint: RVA of EntrySymbol
BaseOfCode: .text RVA
ImageBase: 0x100000
SectionAlignment: 0x1000
FileAlignment: 0x200
MajorOperatingSystemVersion: 0
MinorOperatingSystemVersion: 0
MajorImageVersion: 0
MinorImageVersion: 0
MajorSubsystemVersion: 2
MinorSubsystemVersion: 0
Win32VersionValue: 0
SizeOfImage: end of last section aligned to SectionAlignment
SizeOfHeaders: headers aligned to FileAlignment
CheckSum: 0
Subsystem: 10 (EFI application)
DllCharacteristics: 0
SizeOfStackReserve: 0x100000
SizeOfStackCommit: 0x1000
SizeOfHeapReserve: 0
SizeOfHeapCommit: 0
LoaderFlags: 0
NumberOfRvaAndSizes: 16
DataDirectory[5]: base relocation table RVA/size
```

Section flags:

```text
.text  0x60000020 code | execute | read
.rdata 0x40000040 initialized data | read
.data  0xC0000040 initialized data | read | write
.reloc 0x42000040 initialized data | read | discardable
```

Relocations:

```text
Block page RVA: 4 bytes
Block size: 4 bytes
Entries: uint16 values, type in high 4 bits, offset low 12 bits
DIR64 type: 10
ABSOLUTE padding type: 0
```

### Appendix C: x86_64 System Tables And Paging

GDT entries:

```text
Null descriptor: 0
Owned code selector: 0x08
Owned data selector: 0x10
Code descriptor: access 0x9A, flags 0xA
Data descriptor: access 0x92, flags 0xC
```

GDTR:

```text
limit: uint16(size_bytes - 1)
base: uint64(address)
```

IDT gate:

```text
offset_low: uint16
selector: uint16 = 0x08
ist: uint8 = 0
type_attr: uint8 = 0x8E
offset_mid: uint16
offset_high: uint32
zero: uint32
```

IDTR:

```text
limit: uint16(256 * 16 - 1)
base: uint64(address)
```

Page table flags:

```text
P   0x001 present
RW  0x002 writable
US  0x004 user, not used in v0
PWT 0x008
PCD 0x010
A   0x020
D   0x040
PS  0x080 2 MiB page at PDE level
G   0x100
NX  bit 63, deferred in v0
```

V0 identity paging:

- Use 4 KiB PML4 and PDPT pages.
- Use 2 MiB pages at PD level.
- Map all memory ranges needed by loaded image, page tables, GDT, IDT, owned stack, executor arena.
- PDE entry = physical base | `P|RW|PS`.
- Load `cr3` with PML4 physical address.

GDT activation sequence:

```asm
cli
lgdt [gdt_descriptor]
lea rax, [rip + reload_cs_done]
push 0x08
push rax
retfq                         ; reloads CS cache
reload_cs_done:
    mov ax, 0x10
    mov ds, ax
    mov es, ax
    mov ss, ax
    mov fs, ax
    mov gs, ax
```

The far jump or an equivalent `retfq` sequence is required after `lgdt`; loading the GDTR alone does not refresh the hidden segment caches.

### Appendix D: Microsoft x64 ABI Bridge For UEFI Calls

UEFI function pointers use Microsoft x64 ABI.

Rules:

- Args 1-4: `rcx`, `rdx`, `r8`, `r9`.
- Caller allocates 32 bytes of shadow space before call.
- Stack must be 16-byte aligned at call.
- Return value: `rax`.
- Caller-saved: `rax`, `rcx`, `rdx`, `r8`, `r9`, `r10`, `r11`.
- Callee-saved: `rbx`, `rbp`, `rdi`, `rsi`, `r12`, `r13`, `r14`, `r15`.

Bridge shape from Wrela internal ABI:

```asm
; Wrela internal:
; self in rdi
; image handle in rsi
; map key in rdx
push rbp
mov rbp, rsp
sub rsp, 32          ; 32-byte shadow space; after push rbp, rsp is 16-byte aligned
mov rcx, rsi         ; arg1 image handle
; rdx already arg2 map key
mov rax, [rdi + exit_boot_services_offset]
call rax
add rsp, 32
pop rbp
ret
```

If the bridge needs local spill space, subtract `48`, not forty bytes, after `push rbp`; forty bytes misaligns the stack at the firmware call boundary.

### Appendix E: Wrela Record Layout Algorithm

Primitive sizes and alignments:

```text
Bool: size 1 align 1
U8: size 1 align 1
U16: size 2 align 2
U32: size 4 align 4
U64/I64: size 8 align 8
PhysicalAddress: size 8 align 8
VirtualAddress: size 8 align 8
StringLiteral: size 16 align 8 (address, length)
data/class/driver/path/executor value: pointer-sized handle, size 8 align 8
```

Layout:

```text
offset = 0
record_align = 1
for field in declaration order:
    align = field.align
    offset = align_up(offset, align)
    field.offset = offset
    offset += field.size
    record_align = max(record_align, align)
size = align_up(offset, record_align)
```

This algorithm is used by both normal codegen and Wrela-bound assembly operand materialization.

Go helper shape:

```go
type Field struct {
    Name string
    Type string
}

type Record struct {
    Fields map[string]FieldLayout
    Size int
    Align int
}

type FieldLayout struct {
    Offset int
    Size int
    Align int
}

func AlignUp(value, align int) int {
    if align <= 1 {
        return value
    }
    rem := value % align
    if rem == 0 {
        return value
    }
    return value + align - rem
}

func Compute(fields []Field) (Record, error) {
    offset := 0
    recordAlign := 1
    out := Record{Fields: map[string]FieldLayout{}}
    for _, f := range fields {
        size, align, err := SizeAlign(f.Type)
        if err != nil {
            return Record{}, err
        }
        offset = AlignUp(offset, align)
        out.Fields[f.Name] = FieldLayout{Offset: offset, Size: size, Align: align}
        offset += size
        if align > recordAlign {
            recordAlign = align
        }
    }
    out.Size = AlignUp(offset, recordAlign)
    out.Align = recordAlign
    return out, nil
}
```

### Appendix F: Diagnostic Codes

```text
CLI0001 invalid mode
CLI0002 release mode not implemented
CLI0003 root source path required
CLI0004 output path required
SRC0001 source read failed
SRC0002 invalid module header
SRC0003 module not found
SRC0004 import cycle
SRC0005 duplicate module
PAR0001 unexpected token
PAR0002 expected declaration
SEM0001 duplicate declaration
SEM0002 unknown type
SEM0003 multiple images
SEM0004 missing image
SEM0005 invalid image phases
SEM0006 illegal construction outside image phase
SEM0007 duplicate unique construction
SEM0008 illegal OwnedHardware mint
SEM0009 delegated-only value crosses owned_hardware phase
SEM0010 root driver passed to executor
SEM0011 driver path assigned to multiple executors
SEM0012 illegal assembly placement
SEM0013 too many parameters for v0 ABI
ASM0001 unknown instruction
ASM0002 invalid operand
ASM0003 unsupported x86_64-v3 instruction form
CG0001 unsupported IR operation
PE0001 invalid PE layout
QEMU0001 qemu failed
INT0001 internal build pipeline not wired
```

### Appendix G: QEMU Command And ESP Staging

Environment variables:

```text
WRELA_OVMF_CODE=/absolute/path/to/OVMF_CODE.fd
WRELA_OVMF_VARS=/absolute/path/to/writable/OVMF_VARS.fd
```

ESP staging:

```text
build/esp/
  EFI/
    BOOT/
      BOOTX64.EFI
```

The harness copies the compiler output to:

```text
build/esp/EFI/BOOT/BOOTX64.EFI
```

Command:

```bash
qemu-system-x86_64 \
  -machine q35 \
  -cpu x86-64-v3 \
  -m 256M \
  -drive if=pflash,format=raw,readonly=on,file="$WRELA_OVMF_CODE" \
  -drive if=pflash,format=raw,file="$WRELA_OVMF_VARS" \
  -drive format=raw,file=fat:rw:build/esp \
  -serial stdio \
  -display none \
  -no-reboot
```

Go command builder shape:

```go
func Args(opts Options) []string {
    return []string{
        "-machine", "q35",
        "-cpu", "x86-64-v3",
        "-m", "256M",
        "-drive", "if=pflash,format=raw,readonly=on,file=" + opts.OVMFCode,
        "-drive", "if=pflash,format=raw,file=" + opts.OVMFVars,
        "-drive", "format=raw,file=fat:rw:" + opts.ESPDir,
        "-serial", "stdio",
        "-display", "none",
        "-no-reboot",
    }
}
```

The e2e test must skip when `qemu-system-x86_64` is not in `PATH` or either OVMF environment variable is unset. On failure, it must print captured serial output.

### Appendix H: Wrela Internal ABI

Normal Wrela calls use this ABI. UEFI calls use Appendix D instead.

Registers:

```text
self / receiver: rdi
explicit arg 1:  rsi
explicit arg 2:  rdx
explicit arg 3:  rcx
explicit arg 4:  r8
explicit arg 5:  r9
return value:    rax
```

V0 has no stack-passed Wrela arguments. The semantic checker rejects any method, phase, `start fn`, or `asm fn` with more than five explicit parameters using `SEM0013`. This keeps stack alignment and call lowering simple for the first compiler.

Stack and save rules:

```text
caller aligns rsp to 16 bytes before call
caller-saved: rax, rcx, rdx, rsi, rdi, r8, r9, r10, r11
callee-saved: rbx, rbp, r12, r13, r14, r15
```

Return representation:

```text
Bool/U8/U16/U32/U64/I64/PhysicalAddress/VirtualAddress -> value in rax
StringLiteral -> pointer-sized handle in rax
data -> pointer-sized handle in rax
class/driver/driver path/executor -> pointer-sized handle in rax
never -> does not return
```

Composite records are not returned inline. For a function or method returning `data`, the caller allocates a return record slot using Appendix E layout, passes that slot address in `r10` as a hidden out pointer, and the callee returns the same pointer-sized handle in `rax`. The hidden `r10` out pointer does not shift `self` or explicit argument registers. Constructors also return a pointer-sized handle in `rax` after materializing their record storage. This means `UefiMemoryMapResult`, even though its record layout is larger than one register, returns as one handle. Field loads use the handle plus Appendix E offsets.

The generated `_wrela_efi_entry` adapter is the only V0 function that receives Microsoft x64 ABI arguments from firmware and calls into normal Wrela ABI. It receives `image_handle` in `rcx` and `system_table` in `rdx`, materializes `DelegatedHardware`, calls `delegated_hardware`, passes the returned `OwnedHardware` to `owned_hardware`, and halts if `owned_hardware` ever returns.

---

## 8. Production Deferred Work Register

Create `docs/production-deferred-work.md` in Task 43. Every item must say why it matters for production, why v0 excludes it, and what v0 must not block.

Required sections:

- Memory and address spaces: higher-half layout, permissions, W^X/NX, guard pages, allocator, VM manager, DMA-safe memory, IOMMU.
- CPU and interrupts: AP startup, x2APIC, IOAPIC, MSI/MSI-X, timer, trap authority.
- Hardware discovery: ACPI, PCIe ECAM, framebuffer/GOP, firmware-provided assets.
- Drivers and IO: virtio, storage, network, MMIO typed access.
- Executor runtime: multiple executors, queues, scheduler, cross-executor movement.
- Language/type system: generics, traits, wider phases/typestate, richer ownership.
- Compiler backend: release optimizations, register allocation, debug symbols, multi-target.
- Diagnostics/tooling: explanations, formatter, language server, build traces.
- Security/trust: Secure Boot signing, provenance, authority audit tooling.
- Testing/verification: parser fuzzing, assembler fuzzing, hardware tests, formal authority checks.
- Portability: hosted Linux, AArch64 UEFI, non-UEFI entry paths.
- Developer experience: incremental cache, compiler daemon, image inspection tools.

---

## 9. Global Acceptance Criteria

V0 is complete only when:

```bash
go test ./...
mkdir -p build
go run ./cmd/wrela build --mode dev examples/hello/main.wrela -o build/hello.efi
```

Expected build output:

```text
build/hello.efi
```

On a machine with QEMU and OVMF configured:

```bash
go test ./tests/e2e -run TestHelloQEMU -v
```

Expected serial output contains:

```text
hello from wrela
```

The generated `.efi` must:

- be PE32+ for AMD64;
- have EFI application subsystem;
- include `.text`, `.rdata`, `.data`, and `.reloc` as needed;
- enter through the UEFI application ABI;
- perform a real `ExitBootServices`;
- install image-owned identity page tables, stack, GDT, and fatal IDT;
- write serial through COM1 port IO from the executor path;
- halt without returning to firmware.
