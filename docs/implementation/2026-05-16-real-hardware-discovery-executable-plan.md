# Real Hardware Discovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make x86_64 UEFI hardware discovery source-visible in Wrela so memory regions, ACPI tables, APIC topology, interrupt routes, PCIe ECAM devices, BARs, and MSI/MSI-X capabilities are discovered and narrowed through explicit authority values instead of q35 literals or compiler-hidden platform knowledge.

**Architecture:** UEFI supplies only typed roots: system table/configuration tables, boot services, and a memory map snapshot. Wrela platform modules parse ACPI and PCIe through bounded physical byte/MMIO views, construct non-forgeable authorities inside trusted `platform.*` and `machine.x86_64.*` modules, and hand a small `HardwarePlan` into owned hardware. The compiler enforces authority construction and duplicate claim rules, but it does not decide discovery policy.

**Tech Stack:** Go 1.22+; existing hand-written lexer/parser and semantic checker; existing IR and direct x86_64 codegen; Wrela platform modules under `wrela/`; Go unit/source-shape tests; QEMU q35 + OVMF e2e tests.

---

## 0. How To Execute This Plan

**Description:** This plan is written for junior engineers who can work independently on one task at a time. Each task includes the files, tests, implementation shape, verification commands, and commit message. Do not change public names, diagnostic codes, method signatures, or module names from this document.

**Acceptance Criteria:**
- Every task is completed with its specified tests passing.
- Each task commit message ends with `-Codex Automated`.
- Full acceptance commands in Phase 9 pass on a machine with QEMU and OVMF available.
- `rg -n -e "Q35PciInterruptConfigurator" -e "0xFEC00000" -e "MutableBytes\\(address = 0x200000" -e "PciConfigPorts" examples tests/e2e/fixtures wrela compiler/codegen` returns no production-source q35 hardware assumptions.
- `rg -n "LocalApic\\(base = 0xFEE00000|lapicBase|emitLapicWrite\\(e, lapic" examples tests/e2e/fixtures wrela compiler/codegen` returns no hardcoded LAPIC MMIO-base use. The architectural MSI message-address prefix is allowed only inside `LocalApic.message_address`.

**Code Example:**

```bash
go test ./compiler/sem -run TestHardwareDiscoverySourceShape -v
go test ./compiler/codegen -run 'TestAcpiRootRequireTableLowersChecksumAndOffsets|TestPciCapabilitiesWalkedFromDiscoveredDevices' -v
go test ./tests/e2e -run 'Hello|MultiVcpu' -v
git diff --check
```

Definition of done for a task:

- New failing tests fail before implementation.
- New passing tests pass after implementation.
- `git diff --check` passes.
- The exact commit shown in the task is created.

Definition of done for the full plan:

- `go test ./...` passes.
- `go test ./tests/e2e -v` passes or skips only local dependency checks for missing QEMU/OVMF/ivshmem-server.
- The hello, hello ivshmem, arena memory, cache memory, and multi-vCPU images use discovered memory, APIC, and PCI facts.
- Required missing hardware uses a source-visible boot-fatal path.

---

## 1. Frozen Hardware Discovery Decisions

**Description:** These decisions are fixed for this implementation. Do not reopen them while executing tasks.

**Acceptance Criteria:**
- Every source module and test uses the exact names in this section.
- No task introduces a hidden `discover()` compiler primitive.
- No task reintroduces q35 literals as source-facing policy.

**Code Example:**

```wrela
let root = PlatformDiscoveryRoot(panic = BootPanic())
let discovery = root.from_uefi(hardware = hardware)
let memory_region = discovery.memory.require_usable_region(
    min_base = 0x200000,
    length = 0x600000,
    align = 4096
)
let cpus = discovery.acpi.require_madt().enabled_cpus().require_count(count = 2)
let interrupts = discovery.acpi.require_madt().interrupt_authority()
let pci = discovery.acpi.require_mcfg().ecam_windows().enumerate()
```

Fixed decisions:

- Target platform is x86_64 UEFI + ACPI + PCIe ECAM only.
- BIOS boot, Intel MP tables, 32-bit boot, non-x86_64, non-ACPI, storage, network, framebuffer, IOMMU, and production timer runtime are out of scope.
- Discovery policy lives in Wrela source. The compiler may enforce authority rules and emit low-level instructions.
- ACPI table parsing lives in `wrela/platform/acpi/*.wrela`.
- PCIe ECAM enumeration and BAR/MSI/MSI-X parsing lives in `wrela/machine/x86_64/pci.wrela`.
- The first milestone supports one MADT IOAPIC, up to eight MADT interrupt source overrides, two enabled CPUs for current examples, one MCFG ECAM window, and up to sixteen discovered PCI functions.
- PCI device selection uses `(vendor_id, device_id, occurrence)` so two identical ivshmem-doorbell functions are selectable without hardcoded BDFs.
- Interrupt vectors remain source-selected: COM1 uses `0x40`, EDU MSI uses `0x41`, ivshmem MSI-X uses `0x42`, and wake uses `0xF0`.
- LAPIC MMIO base comes from MADT, not from `0xFEE00000` literals.
- IOAPIC MMIO base and GSI base come from MADT, not from `0xFEC00000` literals.
- Secondary vCPU APIC ID comes from MADT, not from `Vcpu.id`.
- `MutableBytes(address = literal, length = literal)` is no longer allowed in example or fixture image phases. Executor memory comes from `UefiMemoryMap.require_usable_region`.
- Required absence is boot-fatal through `BootPanic`, never silent fallback.
- `HardwarePlan` is intentionally small and example-driven. It carries CPU topology, interrupt plan, and claimed PCI authorities needed by current examples.

Boot-fatal code ranges are reserved by subsystem:

```text
0xAC010000-0xAC01FFFF  ACPI root/table discovery
0xAC020000-0xAC02FFFF  UEFI memory map and usable-region selection
0xAC030000-0xAC03FFFF  Bounded physical/MMIO byte access
0xAC040000-0xAC04FFFF  MADT CPU topology
0xAC050000-0xAC05FFFF  Interrupt routing and APIC programming
0xAC060000-0xAC06FFFF  PCIe ECAM, BAR, MSI, and MSI-X
0xAC070000-0xAC07FFFF  Discovery report validation hooks
```

---

## 2. Repository Layout And File Responsibilities

**Description:** Create or modify exactly these files unless a task explicitly says a file has already moved because of a prior task in this plan.

**Acceptance Criteria:**
- Each listed file has the responsibility described here.
- No unrelated refactors are included in task commits.

**Code Example:**

```text
wrela/platform/acpi/tables.wrela
  ACPI table headers and checksum helpers.

wrela/platform/acpi/root.wrela
  RSDP, RSDT/XSDT, and required table lookup.

wrela/platform/acpi/madt.wrela
  MADT parsing, enabled CPU set, IOAPIC set, interrupt overrides, and CPU topology.

wrela/platform/acpi/mcfg.wrela
  MCFG parsing and PCIe ECAM window authority.

wrela/platform/hardware/bytes.wrela
  BoundedBytes, PhysicalBytes, MmioRegion, IoPortRegion, and checked read/write helpers.

wrela/platform/hardware/discovery.wrela
  PlatformDiscoveryRoot and DiscoveredHardware orchestration over UEFI roots.

wrela/platform/hardware/panic.wrela
  BootPanic fatal methods.

wrela/platform/uefi/types.wrela
  UEFI system table/configuration table fields, memory descriptor access, and usable-region selection.

wrela/platform/uefi/transition.wrela
  DelegatedHardware typed roots and exit_to_owned_hardware handoff with HardwarePlan.

wrela/machine/x86_64/interrupts.wrela
  LocalApic, IoApic, InterruptAuthority, route_isa_irq, and route programming from discovered facts.

wrela/machine/x86_64/pci.wrela
  PCIe ECAM enumeration, PciDeviceSet, BAR claiming, MSI, and MSI-X.

wrela/machine/x86_64/cpu_state.wrela
  CpuPlan, CpuTopology, HardwarePlan, InterruptRoutingPlan, ClaimedPciPlan, Vcpu APIC ID fields, and OwnedHardware hardware_plan field.

wrela/machine/x86_64/{serial,edu,ivshmem}.wrela
  Driver/path constructors consume discovered route and MMIO authorities.

examples/hello/main.wrela
examples/multi_vcpu_topics/main.wrela
tests/e2e/fixtures/{hello_ivshmem,arena_memory,cache_memory}/main.wrela
  Images use PlatformDiscoveryRoot and no q35 literals.

compiler/diag/codes.go
compiler/sem/{check.go,image_graph.go,types.go,uefi_source_shape_test.go,hardware_discovery_test.go}
  Authority restrictions, duplicate claim graph, and source-shape tests.

compiler/ir/{ir.go,lower.go,lower_source_test.go}
compiler/codegen/{entry_adapter.go,lapic.go,vcpu_start.go,uefi_source_codegen_test.go,hardware_discovery_test.go}
  UEFI root handoff, discovered LAPIC base/APIC ID lowering, and anti-hardcode tests.

compiler/integration_test.go
tests/e2e/hello_qemu_test.go
  Full-image and QEMU discovery acceptance.
```

---

## 3. Execution Batches And File Ownership

**Description:** Execute this as green commit batches. A task may add a failing test inside its own branch, but the task commit must include enough implementation for the task's listed verification commands to pass. Do not merge or hand off a task that intentionally leaves its own tests failing.

**Acceptance Criteria:**
- Each batch below lands in order.
- Tasks marked `parallel ok` have non-overlapping write sets and may run concurrently.
- Tasks marked `serial` must not run concurrently with the neighboring task because they share files or depend on the previous task's exact source shape.
- If a worker must edit a file outside the listed write set, stop and coordinate before editing.

**Code Example:**

```text
Batch 0A: Diagnostics (serial)
  Task 1 owner: compiler/diag
    Files: compiler/diag/codes.go, compiler/diag/diag_test.go
    Depends on: none

Batch 0B: Source-shape shell baseline (serial)
  Task 2 owner: sem source-shape harness and shell modules
    Files: compiler/sem/uefi_source_shape_test.go, compiler/sem/hardware_discovery_test.go,
      wrela/platform/hardware/{panic,bytes,discovery}.wrela,
      wrela/platform/acpi/{tables,root,madt,mcfg}.wrela,
      wrela/platform/uefi/transition.wrela,
      wrela/machine/x86_64/pci.wrela
    Depends on: Task 1
    Merge condition: TestHardwareDiscoverySourceShape passes with shell modules.

Batch 1A: UEFI roots (serial)
  Task 3 owner: UEFI roots and entry adapter
    Files: wrela/platform/uefi/{types,transition}.wrela,
      compiler/codegen/{entry_adapter.go,entry_adapter_test.go},
      compiler/sem/hardware_discovery_test.go
    Depends on: Task 2

Batch 1B: Bounded bytes (serial)
  Task 4 owner: bounded hardware bytes
    Files: wrela/platform/hardware/bytes.wrela,
      compiler/sem/hardware_discovery_test.go,
      compiler/codegen/uefi_source_codegen_test.go
    Depends on: Task 3

Batch 2A: ACPI root (serial)
  Task 5 owner: ACPI root and checksum parsing
    Files: wrela/platform/acpi/{tables,root}.wrela,
      compiler/sem/hardware_discovery_test.go,
      compiler/codegen/uefi_source_codegen_test.go
    Depends on: Tasks 3-4

Batch 2B: MADT topology (serial)
  Task 6 owner: MADT topology
    Files: wrela/platform/acpi/madt.wrela,
      wrela/machine/x86_64/{cpu_state,interrupts}.wrela,
      compiler/sem/hardware_discovery_test.go
    Depends on: Task 5

Batch 2C: MCFG windows (serial)
  Task 7 owner: MCFG windows
    Files: wrela/platform/acpi/mcfg.wrela,
      wrela/machine/x86_64/pci.wrela,
      compiler/sem/hardware_discovery_test.go
    Depends on: Task 6

Batch 3A: Interrupt routes (serial)
  Task 8 owner: interrupt routing
    Files: wrela/machine/x86_64/{interrupts,serial}.wrela,
      compiler/sem/hardware_discovery_test.go,
      compiler/codegen/uefi_source_codegen_test.go
    Depends on: Task 7

Batch 3B: PCI ECAM enumeration (serial)
  Task 10 owner: PCI ECAM enumeration
    Files: wrela/machine/x86_64/pci.wrela,
      compiler/sem/hardware_discovery_test.go,
      compiler/sem/pci_ecam_contract_test.go
    Depends on: Task 8

Batch 3C: CPU and BAR claims (parallel ok)
  Task 9 owner: vCPU lowering/codegen
    Files: wrela/machine/x86_64/cpu_state.wrela,
      compiler/ir/{ir.go,lower.go,lower_source_test.go},
      compiler/codegen/{lapic.go,vcpu_start.go,vcpu_start_test.go}
    Depends on: Tasks 6 and 8
  Task 11 owner: PCI BAR claiming
    Files: wrela/machine/x86_64/pci.wrela,
      compiler/sem/{hardware_discovery_test.go,pci_bar_contract_test.go}
    Depends on: Task 10

Batch 3D: PCI MSI/MSI-X (serial)
  Task 12 owner: PCI MSI/MSI-X claiming and routing
    Files: wrela/machine/x86_64/{pci,edu,ivshmem}.wrela,
      compiler/codegen/uefi_source_codegen_test.go
    Depends on: Tasks 9 and 11

Batch 4A: Non-forgeable authorities (serial)
  Task 13 owner: non-forgeable hardware authorities
    Files: compiler/sem/{check.go,hardware_authority_test.go},
      tests/fixtures/negative/{forged_mmio_region,forged_pci_device}.wrela
    Depends on: Task 12

Batch 4B: Duplicate claim graph (serial)
  Task 14 owner: duplicate claim graph
    Files: compiler/sem/{image_graph.go,check.go,hardware_claim_test.go},
      tests/fixtures/negative/{duplicate_pci_bar_claim,duplicate_interrupt_vector}.wrela
    Depends on: Task 13

Batch 5A: HardwarePlan handoff (serial)
  Task 15 owner: HardwarePlan handoff
    Files: wrela/platform/uefi/transition.wrela,
      wrela/platform/hardware/discovery.wrela,
      wrela/machine/x86_64/cpu_state.wrela,
      compiler/sem/{hardware_discovery_test.go,uefi_source_shape_test.go}
    Depends on: Tasks 9, 12, and 14

Batch 5B: Example migration (parallel ok)
  Task 16 owner: hello examples
    Files: examples/hello/{main,program}.wrela,
      tests/e2e/fixtures/hello_ivshmem/{main,program}.wrela,
      compiler/integration_test.go
    Depends on: Task 15
  Task 17 owner: remaining e2e fixtures
    Files: examples/multi_vcpu_topics/main.wrela,
      tests/e2e/fixtures/{arena_memory,cache_memory}/main.wrela,
      tests/e2e/hello_qemu_test.go
    Depends on: Task 15

Batch 6A: Discovery report (serial)
  Task 18 owner: discovery report
    Files: wrela/platform/hardware/discovery.wrela,
      compiler/sem/hardware_discovery_test.go,
      compiler/integration_hardware_discovery_test.go
    Depends on: Tasks 16-17

Batch 6B: Legacy removal (serial)
  Task 19 owner: legacy removal
    Files: wrela/machine/x86_64/pci.wrela,
      compiler/codegen/uefi_source_codegen_test.go,
      compiler/integration_test.go,
      docs/production-deferred-work.md
    Depends on: Task 18

Batch 6C: QEMU acceptance (serial)
  Task 20 owner: QEMU discovery acceptance
    Files: tests/e2e/hello_qemu_test.go
    Depends on: Task 19

Batch 6D: Full sweep (serial)
  Task 21 owner: full sweep
    Files: only files changed by failed verification commands
    Depends on: Task 20
```

---

## 4. Canonical Source Surface

**Description:** This is the exact Wrela API shape the plan builds. Implementation may add private helpers, but public names and signatures stay fixed.

**Acceptance Criteria:**
- `compiler/sem/uefi_source_shape_test.go` asserts every public type, field, and method in this section.
- Examples import these modules and do not import `Q35PciInterruptConfigurator` or `PciConfigPorts`.

**Code Example:**

```wrela
module platform.hardware.discovery

use { DelegatedHardware } from platform.uefi.transition
use { UefiMemoryMap } from platform.uefi.types
use { AcpiLocator, AcpiRoot } from platform.acpi.root
use { BootPanic } from platform.hardware.panic
use { InterruptAuthority } from machine.x86_64.interrupts
use { PciDeviceSet } from machine.x86_64.pci

class PlatformDiscoveryRoot {
    panic: BootPanic

    fn from_uefi(self, hardware: DelegatedHardware) -> DiscoveredHardware {
        let tables = hardware.uefi_configuration_tables()
        let memory = hardware.memory_map()
        let acpi = AcpiLocator(panic = self.panic).find(tables = tables)
        let madt = acpi.require_madt()
        let mcfg = acpi.require_mcfg()
        return DiscoveredHardware(
            memory = memory,
            acpi = acpi,
            interrupts = madt.interrupt_authority(),
            pci = mcfg.ecam_windows().enumerate(),
            panic = self.panic
        )
    }
}

class DiscoveredHardware {
    memory: UefiMemoryMap
    acpi: AcpiRoot
    interrupts: InterruptAuthority
    pci: PciDeviceSet
    panic: BootPanic
}
```

Canonical constants:

```text
ACPI 2.0 GUID low/high: 0x11D3E4F18868E871 / 0x81883CC7800022BC
ACPI 1.0 GUID low/high: 0x11D32D88EB9D2D30 / 0x4DC13F279000169A
ACPI table signatures: APIC=0x43495041, MCFG=0x4746434D, XSDT=0x54445358, RSDT=0x54445352
PCI vendors/devices: QEMU EDU 0x1234:0x11E8, ivshmem-doorbell 0x1AF4:0x1110
Vectors: serial=0x40, EDU MSI=0x41, ivshmem MSI-X=0x42, wake=0xF0
```

Canonical delegated phase shape:

```wrela
phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
    let root = PlatformDiscoveryRoot(panic = BootPanic())
    let discovery = root.from_uefi(hardware = hardware)
    let memory_region = discovery.memory.require_usable_region(
        min_base = 0x200000,
        length = 0x600000,
        align = 4096
    )
    let cpus = discovery.acpi.require_madt().enabled_cpus().require_count(count = 2)
    let interrupts = discovery.interrupts
    let pci = discovery.pci

    let edu = pci.require_device(vendor_id = 0x1234, device_id = 0x11E8, occurrence = 0)
    let ivshmem_rx = pci.require_device(vendor_id = 0x1AF4, device_id = 0x1110, occurrence = 0)
    let ivshmem_tx = pci.require_device(vendor_id = 0x1AF4, device_id = 0x1110, occurrence = 1)

    let hardware_plan = HardwarePlan(
        cpus = cpus,
        interrupts = InterruptRoutingPlan(
            local_apic = interrupts.local_apic,
            serial_irq4 = interrupts.route_isa_irq(irq = 4, vector = InterruptVector(value = 0x40))
        ),
        pci = ClaimedPciPlan(
            edu_bar0 = edu.claim_mmio_bar(index = 0),
            edu_msi = edu.claim_msi(),
            ivshmem_rx_bar0 = ivshmem_rx.claim_mmio_bar(index = 0),
            ivshmem_rx_msix = ivshmem_rx.claim_msix(table_bar_index = 1),
            ivshmem_tx_bar0 = ivshmem_tx.claim_mmio_bar(index = 0)
        )
    )
    let memory_plan = MemoryPlan(
        owned_memory = OwnedMemory(arena = memory_region),
        executor_arena = memory_region,
        io_ports = IoPortAuthority()
    )
    let cpu_plan = CpuPlan(
        owned_stack_top = memory_region.address + memory_region.length,
        gdt_descriptor = Bytes(address = 0, length = 0),
        idt_descriptor = Bytes(address = 0, length = 0),
        cr3 = memory_region.address
    )
    return hardware.exit_to_owned_hardware(
        memory_plan = memory_plan,
        cpu_plan = cpu_plan,
        hardware_plan = hardware_plan
    )
}
```

---

## 5. Phase 1: Contracts, Diagnostics, And Source Harness

**Description:** Reserve compiler diagnostics and make the source-shape harness load all new platform modules before implementation branches diverge.

**Acceptance Criteria:**
- Hardware discovery diagnostics exist.
- `parseUEFIModuleSet` includes all new source modules.
- Source-shape tests fail because the modules/types are not implemented yet.

**Code Example:**

```go
codes := []string{diag.SEM0049, diag.SEM0050, diag.SEM0051, diag.SEM0052, diag.SEM0053, diag.SEM0054}
```

### Task 1: Reserve Hardware Discovery Diagnostics

**Description:** Add stable semantic diagnostics for hardware authority construction, duplicate claims, and discovered-plan misuse.

**Files:**
- Modify: `compiler/diag/codes.go`
- Modify: `compiler/diag/diag_test.go`

- [ ] **Step 1: Add failing diagnostic test**

Add to `compiler/diag/diag_test.go`:

```go
func TestHardwareDiscoveryDiagnosticCodesExist(t *testing.T) {
    codes := []string{
        diag.SEM0049, diag.SEM0050, diag.SEM0051,
        diag.SEM0052, diag.SEM0053, diag.SEM0054,
    }
    for _, code := range codes {
        if code == "" {
            t.Fatalf("hardware diagnostic code must not be empty")
        }
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./compiler/diag -run TestHardwareDiscoveryDiagnosticCodesExist -v`

Expected: FAIL with undefined identifiers such as `SEM0049`.

- [ ] **Step 3: Add codes after SEM0048**

```go
SEM0049 = "SEM0049" // hardware capability construction is not allowed here
SEM0050 = "SEM0050" // duplicate hardware claim
SEM0051 = "SEM0051" // discovered hardware authority cannot cross this phase boundary
SEM0052 = "SEM0052" // required hardware discovery call lacks boot-fatal path
SEM0053 = "SEM0053" // unsupported discovered hardware shape
SEM0054 = "SEM0054" // PCI claims must be made from discovered PciDevice values
```

- [ ] **Step 4: Verify**

Run:

```bash
go test ./compiler/diag -run TestHardwareDiscoveryDiagnosticCodesExist -v
git diff --check
```

Expected: PASS and no whitespace errors.

- [ ] **Step 5: Commit**

```bash
git add compiler/diag/codes.go compiler/diag/diag_test.go
git commit -m "feat: reserve hardware discovery diagnostics -Codex Automated"
```

**Acceptance Criteria:** SEM0049-SEM0054 are defined and covered by a focused unit test.

### Task 2: Add Hardware Discovery Source-Shape Harness

**Description:** Extend the UEFI source-shape test harness and add green contract-shell source modules. This task intentionally starts with a failing source-shape test, then adds enough shell implementation for the task commit to pass.

**Files:**
- Modify: `compiler/sem/uefi_source_shape_test.go`
- Add: `compiler/sem/hardware_discovery_test.go`
- Add: `wrela/platform/hardware/panic.wrela`
- Add: `wrela/platform/hardware/bytes.wrela`
- Add: `wrela/platform/hardware/discovery.wrela`
- Add: `wrela/platform/acpi/tables.wrela`
- Add: `wrela/platform/acpi/root.wrela`
- Add: `wrela/platform/acpi/madt.wrela`
- Add: `wrela/platform/acpi/mcfg.wrela`
- Modify: `wrela/platform/uefi/transition.wrela`
- Modify: `wrela/machine/x86_64/pci.wrela`

- [ ] **Step 1: Add expected module paths to `parseUEFIModuleSet`**

Insert these paths after `wrela/platform/uefi/types.wrela`:

```go
filepath.Join(repoRoot, "wrela/platform/hardware/panic.wrela"),
filepath.Join(repoRoot, "wrela/platform/hardware/bytes.wrela"),
filepath.Join(repoRoot, "wrela/platform/acpi/tables.wrela"),
filepath.Join(repoRoot, "wrela/platform/acpi/root.wrela"),
filepath.Join(repoRoot, "wrela/platform/acpi/madt.wrela"),
filepath.Join(repoRoot, "wrela/platform/acpi/mcfg.wrela"),
filepath.Join(repoRoot, "wrela/platform/hardware/discovery.wrela"),
```

- [ ] **Step 2: Add failing source-shape test**

Create `compiler/sem/hardware_discovery_test.go`:

```go
package sem

import "testing"

func TestHardwareDiscoverySourceShape(t *testing.T) {
    modules := parseUEFIModuleSet(t)
    index, ds := BuildIndex(modules)
    if len(ds) != 0 {
        t.Fatalf("build index diagnostics: %#v", ds)
    }

    assertMethodExists(t, moduleType(t, index, "platform.hardware.discovery", "PlatformDiscoveryRoot"), "from_uefi")
    assertMethodExists(t, moduleType(t, index, "platform.uefi.transition", "DelegatedHardware"), "uefi_configuration_tables")
    assertMethodExists(t, moduleType(t, index, "platform.uefi.transition", "DelegatedHardware"), "memory_map")
    assertMethodExists(t, moduleType(t, index, "platform.acpi.root", "AcpiRoot"), "require_madt")
    assertMethodExists(t, moduleType(t, index, "platform.acpi.root", "AcpiRoot"), "require_mcfg")
    assertMethodExists(t, moduleType(t, index, "machine.x86_64.pci", "PciDeviceSet"), "require_device")
}
```

- [ ] **Step 3: Run test to verify it fails before shell modules**

Run: `go test ./compiler/sem -run TestHardwareDiscoverySourceShape -v`

Expected: FAIL reading missing source files or missing types.

- [ ] **Step 4: Add green shell modules**

Create `wrela/platform/hardware/panic.wrela`:

```wrela
module platform.hardware.panic

class BootPanic {
    asm fn fail(self, code: U64) -> never {
        cli
    panic_loop:
        hlt
        jmp panic_loop
    }
}
```

Create `wrela/platform/hardware/bytes.wrela`:

```wrela
module platform.hardware.bytes

use { BootPanic } from platform.hardware.panic

class BoundedBytes {
    address: PhysicalAddress
    length: U64
    panic: BootPanic
}

class PhysicalBytes {
    address: PhysicalAddress
    length: U64
    panic: BootPanic
}

class MmioRegion {
    address: PhysicalAddress
    length: U64
    panic: BootPanic
}

class IoPortRegion {
    port_base: U32
    length: U32
}
```

Create `wrela/platform/acpi/tables.wrela`:

```wrela
module platform.acpi.tables

use { BoundedBytes } from platform.hardware.bytes

data AcpiTable {
    bytes: BoundedBytes
    signature: U32
    length: U32
}
```

Create `wrela/platform/acpi/madt.wrela`:

```wrela
module platform.acpi.madt

use { AcpiTable } from platform.acpi.tables
use { BootPanic } from platform.hardware.panic

class MadtTable {
    table: AcpiTable
    panic: BootPanic
}
```

Create `wrela/platform/acpi/mcfg.wrela`:

```wrela
module platform.acpi.mcfg

use { AcpiTable } from platform.acpi.tables
use { BootPanic } from platform.hardware.panic

class McfgTable {
    table: AcpiTable
    panic: BootPanic
}
```

Create `wrela/platform/acpi/root.wrela`:

```wrela
module platform.acpi.root

use { AcpiTable } from platform.acpi.tables
use { MadtTable } from platform.acpi.madt
use { McfgTable } from platform.acpi.mcfg
use { BoundedBytes } from platform.hardware.bytes
use { BootPanic } from platform.hardware.panic
use { UefiConfigurationTables } from platform.uefi.types

class AcpiLocator {
    panic: BootPanic

    fn find(self, tables: UefiConfigurationTables) -> AcpiRoot {
        return AcpiRoot(root_address = 0, use_xsdt = true, panic = self.panic)
    }
}

class AcpiRoot {
    root_address: PhysicalAddress
    use_xsdt: Bool
    panic: BootPanic

    fn require_table(self, signature: U32) -> AcpiTable {
        return AcpiTable(bytes = BoundedBytes(address = 0, length = 0, panic = self.panic), signature = signature, length = 0)
    }

    fn require_madt(self) -> MadtTable {
        return MadtTable(table = self.require_table(signature = 0x43495041), panic = self.panic)
    }

    fn require_mcfg(self) -> McfgTable {
        return McfgTable(table = self.require_table(signature = 0x4746434D), panic = self.panic)
    }
}
```

Append these shell methods to `DelegatedHardware` in `wrela/platform/uefi/transition.wrela`:

First replace the existing UEFI types import:

```wrela
use { DelegatedMemory, UefiHandle } from platform.uefi.types
```

with:

```wrela
use { DelegatedMemory, UefiConfigurationTables, UefiHandle, UefiMemoryMap } from platform.uefi.types
```

```wrela
fn uefi_configuration_tables(self) -> UefiConfigurationTables {
    return UefiConfigurationTables(table_address = 0, count = 0)
}

fn memory_map(self) -> UefiMemoryMap {
    return self.delegated_memory.last_memory_map
}
```

Create `wrela/platform/hardware/discovery.wrela`:

```wrela
module platform.hardware.discovery

use { AcpiLocator, AcpiRoot } from platform.acpi.root
use { BootPanic } from platform.hardware.panic
use { DelegatedHardware } from platform.uefi.transition
use { UefiMemoryMap } from platform.uefi.types
use { PciDeviceSet, PciDeviceSetBuilder } from machine.x86_64.pci

class DiscoveredHardware {
    memory: UefiMemoryMap
    acpi: AcpiRoot
    pci: PciDeviceSet
    panic: BootPanic
}

class PlatformDiscoveryRoot {
    panic: BootPanic

    fn from_uefi(self, hardware: DelegatedHardware) -> DiscoveredHardware {
        let acpi = AcpiLocator(panic = self.panic).find(tables = hardware.uefi_configuration_tables())
        return DiscoveredHardware(
            memory = hardware.memory_map(),
            acpi = acpi,
            pci = PciDeviceSetBuilder(panic = self.panic).empty(),
            panic = self.panic
        )
    }
}
```

Add `use { BootPanic } from platform.hardware.panic` with the other imports at the top of `wrela/machine/x86_64/pci.wrela`, then append this shell PCI surface to the same file. Do not remove legacy q35 helpers in this task:

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
}

class PcieEcamWindow {
    base: PhysicalAddress
    segment: U16
    start_bus: U8
    end_bus: U8
    panic: BootPanic
}

class PciDevice {
    window: PcieEcamWindow
    identity: PciDeviceIdentity
    panic: BootPanic
}

class PciDeviceSet {
    count: U64
    device0: PciDevice
    panic: BootPanic

    fn require_device(self, vendor_id: U16, device_id: U16, occurrence: U64) -> PciDevice {
        return self.device0
    }
}

class PciDeviceSetBuilder {
    panic: BootPanic

    fn empty(self) -> PciDeviceSet {
        let window = PcieEcamWindow(base = 0, segment = 0, start_bus = 0, end_bus = 0, panic = self.panic)
        let identity = PciDeviceIdentity(segment = 0, bus = 0, device = 0, function = 0, vendor_id = 0xFFFF, device_id = 0xFFFF, class_code = 0, subclass = 0, prog_if = 0)
        let device = PciDevice(window = window, identity = identity, panic = self.panic)
        return PciDeviceSet(count = 0, device0 = device, panic = self.panic)
    }
}

```

- [ ] **Step 5: Run test to verify it passes**

Run:

```bash
go test ./compiler/sem -run TestHardwareDiscoverySourceShape -v
git diff --check
```

Expected: PASS and no whitespace errors.

- [ ] **Step 6: Commit harness and shell modules**

```bash
git add compiler/sem/uefi_source_shape_test.go compiler/sem/hardware_discovery_test.go wrela/platform/hardware/panic.wrela wrela/platform/hardware/bytes.wrela wrela/platform/hardware/discovery.wrela wrela/platform/acpi/tables.wrela wrela/platform/acpi/root.wrela wrela/platform/acpi/madt.wrela wrela/platform/acpi/mcfg.wrela wrela/platform/uefi/transition.wrela wrela/machine/x86_64/pci.wrela
git commit -m "test: load hardware discovery source modules -Codex Automated"
```

**Acceptance Criteria:** The test harness names every new platform module, every new module exists, and `TestHardwareDiscoverySourceShape` passes at the task commit boundary.

---

## 6. Phase 2: UEFI Roots, Bounded Bytes, And Boot Fatal

**Description:** Expose firmware inputs as typed Wrela values and provide explicit read/write primitives over bounded physical and MMIO authorities.

**Acceptance Criteria:**
- `DelegatedHardware` exposes configuration tables and memory map snapshots.
- ACPI and PCI code can read bounded physical bytes without ambient free functions.
- Boot-fatal is an explicit source-visible platform authority.

**Code Example:**

```wrela
let tables = hardware.uefi_configuration_tables()
let map = hardware.memory_map()
let bytes = PhysicalBytes(address = rsdp_address, length = 36, panic = panic).bounded()
if bytes.read_u8(offset = 0) != 0x52 {
    panic.fail(code = 0xAC010001)
}
```

### Task 3: Expose UEFI Configuration Tables And Memory Map Snapshot

**Description:** Add UEFI configuration table entry access and a reusable pre-exit memory map snapshot method.

**Files:**
- Modify: `wrela/platform/uefi/types.wrela`
- Modify: `wrela/platform/uefi/transition.wrela`
- Modify: `compiler/codegen/entry_adapter.go`
- Modify: `compiler/codegen/entry_adapter_test.go`
- Modify: `compiler/sem/hardware_discovery_test.go`

- [ ] **Step 1: Add failing source-shape checks**

Extend `TestHardwareDiscoverySourceShape`:

```go
tables := moduleType(t, index, "platform.uefi.types", "UefiConfigurationTables")
assertMethodExists(t, tables, "entry_at")
assertMethodExists(t, tables, "find_acpi_rsdp")
assertMethodExists(t, moduleType(t, index, "platform.uefi.types", "UefiMemoryMap"), "descriptor_at")
assertMethodExists(t, moduleType(t, index, "platform.uefi.types", "UefiMemoryMap"), "require_usable_region")
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./compiler/sem -run TestHardwareDiscoverySourceShape -v`

Expected: FAIL on missing methods.

- [ ] **Step 3: Replace UEFI shell contracts with real UEFI roots**

Add these declarations to `wrela/platform/uefi/types.wrela`:

```wrela
use { MutableBytes } from machine.x86_64.executor_memory
use { BootPanic } from platform.hardware.panic

data UefiConfigurationTableEntry {
    vendor_guid_low: U64
    vendor_guid_high: U64
    table_address: PhysicalAddress
}

data AcpiRsdpSearchResult {
    found: Bool
    address: PhysicalAddress
}

class UefiConfigurationTables {
    table_address: VirtualAddress
    count: U64

    asm fn entry_at(self, index: U64) -> UefiConfigurationTableEntry {
        mov r11, self.table_address
        mov r10, index
        imul r10, 24
        add r11, r10
        mov r10, [r11]
        mov [rax + 0], r10
        mov r10, [r11 + 8]
        mov [rax + 8], r10
        mov r10, [r11 + 16]
        mov [rax + 16], r10
        ret
    }

    fn find_acpi_rsdp(self) -> AcpiRsdpSearchResult {
        let index = 0
        while index < self.count {
            let entry = self.entry_at(index = index)
            if entry.vendor_guid_low == 0x11D3E4F18868E871 {
                if entry.vendor_guid_high == 0x81883CC7800022BC {
                    return AcpiRsdpSearchResult(found = true, address = entry.table_address)
                }
            }
            if entry.vendor_guid_low == 0x11D32D88EB9D2D30 {
                if entry.vendor_guid_high == 0x4DC13F279000169A {
                    return AcpiRsdpSearchResult(found = true, address = entry.table_address)
                }
            }
            index = index + 1
        }
        return AcpiRsdpSearchResult(found = false, address = 0)
    }
}
```

Replace the existing duplicate `class UefiConfigurationTables` declaration rather than adding a second one.

Add to `UefiMemoryMap`:

```wrela
asm fn descriptor_at(self, index: U64) -> UefiMemoryDescriptor {
    mov r11, self.descriptors.address
    mov r10, index
    imul r10, self.descriptor_size
    add r11, r10
    mov r10d, [r11 + 0]
    mov [rax + 0], r10d
    mov r10, [r11 + 8]
    mov [rax + 8], r10
    mov r10, [r11 + 16]
    mov [rax + 16], r10
    mov r10, [r11 + 24]
    mov [rax + 24], r10
    mov r10, [r11 + 32]
    mov [rax + 32], r10
    ret
}

fn require_usable_region(self, min_base: PhysicalAddress, length: U64, align: U64) -> MutableBytes {
    let index = 0
    let descriptor_count = self.descriptors.length / self.descriptor_size
    while index < descriptor_count {
        let descriptor = self.descriptor_at(index = index)
        let bytes = descriptor.number_of_pages << 12
        let aligned = (descriptor.physical_start + align - 1) & (0 - align)
        let end = aligned + length
        if descriptor.kind == 7 {
            if aligned >= min_base {
                if end <= descriptor.physical_start + bytes {
                    return MutableBytes(address = aligned, length = length)
                }
            }
        }
        index = index + 1
    }
    BootPanic().fail(code = 0xAC020001)
}
```

- [ ] **Step 4: Add DelegatedHardware roots**

In `wrela/platform/uefi/transition.wrela`, replace the UEFI types import from Task 2:

```wrela
use { DelegatedMemory, UefiConfigurationTables, UefiHandle, UefiMemoryMap } from platform.uefi.types
```

with:

```wrela
use { DelegatedMemory, UefiConfigurationTables, UefiHandle, UefiMemoryMap, UefiSystemTable } from platform.uefi.types
```

Then add `system_table: UefiSystemTable` to `DelegatedHardware` immediately after `boot_services: UefiBootServicesCalls`, and replace the Task 2 shell methods with:

```wrela
fn uefi_configuration_tables(self) -> UefiConfigurationTables {
    return self.system_table.configuration_tables
}

fn memory_map(self) -> UefiMemoryMap {
    let map_buffer = self.delegated_memory.allocate(length = 16384)
    let active_buffer = map_buffer
    let map_result = self.boot_services.get_memory_map(buffer = active_buffer)
    while map_result.status.value == 0x8000000000000005 {
        active_buffer = self.delegated_memory.allocate(length = map_result.required_size + 4096)
        map_result = self.boot_services.get_memory_map(buffer = active_buffer)
    }
    self.delegated_memory.last_memory_map = map_result.memory_map
    return map_result.memory_map
}
```

- [ ] **Step 5: Update entry adapter to store system table**

Add a `DelegatedHardwareSystemTableOffset` layout slot and store the UEFI `rdx` system table pointer into it. The generated layout must set:

```go
emitStoreSlotFromReg(e, asm.MustLookup("rdx"), adapterLayout.UefiSystemTableOffset, 64)
emitStoreSlotAddress(e, adapterLayout.DelegatedHardwareSystemTableOffset, adapterLayout.UefiSystemTableOffset)
```

Add an entry adapter test assertion mirroring the existing boot-services assertions:

```go
assertStoresSlotAddress(t, entry, adapterLayout.DelegatedHardwareSystemTableOffset, adapterLayout.UefiSystemTableOffset)
```

- [ ] **Step 6: Verify**

Run:

```bash
go test ./compiler/sem -run TestHardwareDiscoverySourceShape -v
go test ./compiler/codegen -run TestEntryAdapter -v
git diff --check
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add wrela/platform/uefi/types.wrela wrela/platform/uefi/transition.wrela compiler/codegen/entry_adapter.go compiler/codegen/entry_adapter_test.go compiler/sem/hardware_discovery_test.go
git commit -m "feat: expose uefi discovery roots -Codex Automated"
```

**Acceptance Criteria:** `DelegatedHardware` exposes typed configuration tables and a pre-exit memory map snapshot; UEFI table entries can find ACPI RSDP by GUID; memory selection uses descriptor type 7 and alignment.

### Task 4: Add BoundedBytes, MmioRegion, And IoPortRegion

**Description:** Add explicit source-visible primitives for checked physical reads and MMIO/IO authorities.

**Files:**
- Modify: `wrela/platform/hardware/bytes.wrela`
- Modify: `compiler/sem/hardware_discovery_test.go`
- Modify: `compiler/codegen/uefi_source_codegen_test.go`

- [ ] **Step 1: Add failing source-shape checks**

Extend `TestHardwareDiscoverySourceShape`:

```go
assertMethodExists(t, moduleType(t, index, "platform.hardware.panic", "BootPanic"), "fail")
assertMethodExists(t, moduleType(t, index, "platform.hardware.bytes", "BoundedBytes"), "read_u32")
assertMethodExists(t, moduleType(t, index, "platform.hardware.bytes", "MmioRegion"), "read32")
assertMethodExists(t, moduleType(t, index, "platform.hardware.bytes", "MmioRegion"), "write32")
```

- [ ] **Step 2: Replace Task 2 bounded byte/MMIO shell**

Replace the entire Task 2 shell in `wrela/platform/hardware/bytes.wrela` with:

```wrela
module platform.hardware.bytes

use { BootPanic } from platform.hardware.panic

class BoundedBytes {
    address: PhysicalAddress
    length: U64
    panic: BootPanic

    fn require_range(self, offset: U64, width: U64) -> U64 {
        if offset > self.length {
            self.panic.fail(code = 0xAC030001)
        }
        if width > self.length - offset {
            self.panic.fail(code = 0xAC030002)
        }
        return offset
    }

    fn slice(self, offset: U64, length: U64) -> BoundedBytes {
        self.require_range(offset = offset, width = length)
        return BoundedBytes(address = self.address + offset, length = length, panic = self.panic)
    }

    fn read_u8(self, offset: U64) -> U8 {
        self.require_range(offset = offset, width = 1)
        return self.unchecked_read_u8(offset = offset)
    }

    fn read_u16(self, offset: U64) -> U16 {
        self.require_range(offset = offset, width = 2)
        return self.unchecked_read_u16(offset = offset)
    }

    fn read_u32(self, offset: U64) -> U32 {
        self.require_range(offset = offset, width = 4)
        return self.unchecked_read_u32(offset = offset)
    }

    fn read_u64(self, offset: U64) -> U64 {
        self.require_range(offset = offset, width = 8)
        return self.unchecked_read_u64(offset = offset)
    }

    asm fn unchecked_read_u8(self, offset: U64) -> U8 {
        mov r11, self.address
        add r11, offset
        mov al, [r11]
        ret
    }

    asm fn unchecked_read_u16(self, offset: U64) -> U16 {
        mov r11, self.address
        add r11, offset
        mov ax, [r11]
        ret
    }

    asm fn unchecked_read_u32(self, offset: U64) -> U32 {
        mov r11, self.address
        add r11, offset
        mov eax, [r11]
        ret
    }

    asm fn unchecked_read_u64(self, offset: U64) -> U64 {
        mov r11, self.address
        add r11, offset
        mov rax, [r11]
        ret
    }
}

class PhysicalBytes {
    address: PhysicalAddress
    length: U64
    panic: BootPanic

    fn bounded(self) -> BoundedBytes {
        return BoundedBytes(address = self.address, length = self.length, panic = self.panic)
    }
}

class MmioRegion {
    address: PhysicalAddress
    length: U64
    panic: BootPanic

    fn bytes(self) -> BoundedBytes {
        return BoundedBytes(address = self.address, length = self.length, panic = self.panic)
    }

    asm fn read32(self, offset: U64) -> U32 {
        mov r11, self.address
        add r11, offset
        mov eax, [r11]
        ret
    }

    asm fn write32(self, offset: U64, value: U32) {
        mov r11, self.address
        add r11, offset
        mov eax, value
        mov [r11], eax
        ret
    }
}

class IoPortRegion {
    port_base: U32
    length: U32
}
```

- [ ] **Step 3: Add codegen check for non-empty asm**

Add to `compiler/codegen/uefi_source_codegen_test.go`:

```go
func TestHardwareBytesAsmCodegen(t *testing.T) {
    checked := parseCheckedUEFIModules(t)
    methods := []ir.AsmMethod{
        asmMethodFromSem(t, checked, "platform.hardware.panic", "BootPanic", "fail"),
        asmMethodFromSem(t, checked, "platform.hardware.bytes", "BoundedBytes", "unchecked_read_u32"),
        asmMethodFromSem(t, checked, "platform.hardware.bytes", "MmioRegion", "write32"),
    }
    for _, method := range methods {
        unit, ds := compileAsmMethodUnit(method)
        if len(ds) != 0 {
            t.Fatalf("compileAsmMethodUnit %q diagnostics: %#v", method.Symbol, ds)
        }
        if len(unit.Bytes) == 0 {
            t.Fatalf("%s compiled to empty bytes", method.Symbol)
        }
    }
}
```

- [ ] **Step 4: Verify**

Run:

```bash
go test ./compiler/sem -run TestHardwareDiscoverySourceShape -v
go test ./compiler/codegen -run TestHardwareBytesAsmCodegen -v
git diff --check
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add wrela/platform/hardware/bytes.wrela compiler/sem/hardware_discovery_test.go compiler/codegen/uefi_source_codegen_test.go
git commit -m "feat: add bounded hardware byte authorities -Codex Automated"
```

**Acceptance Criteria:** Physical and MMIO reads require authority-bearing receiver values; out-of-bounds physical reads call `BootPanic.fail`; no ambient physical read free function exists.

---

## 7. Phase 3: ACPI Root, MADT, And MCFG Parsing

**Description:** Parse ACPI roots and required tables in Wrela source using bounded bytes and explicit boot-fatal errors.

**Acceptance Criteria:**
- RSDP checksum and XSDT/RSDT checksums are validated.
- MADT exposes LAPIC base, enabled CPUs, IOAPIC, and overrides.
- MCFG exposes ECAM windows.

**Code Example:**

```wrela
let acpi = AcpiLocator(panic = panic).find(tables = tables)
let madt = acpi.require_madt()
let mcfg = acpi.require_mcfg()
let cpus = madt.enabled_cpus().require_count(count = 2)
```

### Task 5: Implement ACPI RSDP And Root Table Lookup

**Description:** Add ACPI table header parsing, checksums, and required table lookup by signature.

**Files:**
- Modify: `wrela/platform/acpi/tables.wrela`
- Modify: `wrela/platform/acpi/root.wrela`
- Modify: `wrela/platform/acpi/madt.wrela`
- Modify: `wrela/platform/acpi/mcfg.wrela`
- Add: `compiler/sem/acpi_parser_contract_test.go`
- Modify: `compiler/sem/hardware_discovery_test.go`
- Modify: `compiler/codegen/uefi_source_codegen_test.go`

- [ ] **Step 1: Add failing shape checks**

```go
root := moduleType(t, index, "platform.acpi.root", "AcpiRoot")
assertMethodExists(t, root, "require_table")
assertMethodExists(t, root, "require_madt")
assertMethodExists(t, root, "require_mcfg")
assertMethodExists(t, moduleType(t, index, "platform.acpi.root", "AcpiLocator"), "find")
assertMethodExists(t, moduleType(t, index, "platform.acpi.tables", "AcpiHelpers"), "checksum_ok")
```

- [ ] **Step 2: Replace Task 2 ACPI shells with root-table source**

Replace the entire Task 2 shell in `wrela/platform/acpi/tables.wrela` with:

```wrela
module platform.acpi.tables

use { BoundedBytes, PhysicalBytes } from platform.hardware.bytes
use { BootPanic } from platform.hardware.panic

data AcpiTable {
    bytes: BoundedBytes
    signature: U32
    length: U32
}

class AcpiHelpers {
    panic: BootPanic

    fn table_at(self, address: PhysicalAddress) -> AcpiTable {
        let header = PhysicalBytes(address = address, length = 36, panic = self.panic).bounded()
        let length = header.read_u32(offset = 4)
        let bytes = PhysicalBytes(address = address, length = length, panic = self.panic).bounded()
        return AcpiTable(bytes = bytes, signature = bytes.read_u32(offset = 0), length = length)
    }

    fn checksum_ok(self, bytes: BoundedBytes) -> Bool {
        let sum = 0
        let index = 0
        while index < bytes.length {
            sum = (sum + bytes.read_u8(offset = index)) & 0xFF
            index = index + 1
        }
        return sum == 0
    }
}
```

Replace the entire Task 2 shell in `wrela/platform/acpi/root.wrela` with:

```wrela
module platform.acpi.root

use { AcpiHelpers, AcpiTable } from platform.acpi.tables
use { MadtTable } from platform.acpi.madt
use { McfgTable } from platform.acpi.mcfg
use { PhysicalBytes } from platform.hardware.bytes
use { BootPanic } from platform.hardware.panic
use { UefiConfigurationTables } from platform.uefi.types

class AcpiLocator {
    panic: BootPanic

    fn find(self, tables: UefiConfigurationTables) -> AcpiRoot {
        let rsdp = tables.find_acpi_rsdp()
        if rsdp.found == false {
            self.panic.fail(code = 0xAC010001)
        }
        let helpers = AcpiHelpers(panic = self.panic)
        let rsdp_bytes = PhysicalBytes(address = rsdp.address, length = 36, panic = self.panic).bounded()
        if helpers.checksum_ok(bytes = rsdp_bytes.slice(offset = 0, length = 20)) == false {
            self.panic.fail(code = 0xAC010002)
        }
        let revision = rsdp_bytes.read_u8(offset = 15)
        if revision != 0 {
            if helpers.checksum_ok(bytes = rsdp_bytes.slice(offset = 0, length = rsdp_bytes.read_u32(offset = 20))) == false {
                self.panic.fail(code = 0xAC010003)
            }
            return AcpiRoot(root_address = rsdp_bytes.read_u64(offset = 24), use_xsdt = true, panic = self.panic)
        }
        return AcpiRoot(root_address = rsdp_bytes.read_u32(offset = 16), use_xsdt = false, panic = self.panic)
    }
}

class AcpiRoot {
    root_address: PhysicalAddress
    use_xsdt: Bool
    panic: BootPanic

    fn root_table(self) -> AcpiTable {
        return AcpiHelpers(panic = self.panic).table_at(address = self.root_address)
    }

    fn require_table(self, signature: U32) -> AcpiTable {
        let helpers = AcpiHelpers(panic = self.panic)
        let root = self.root_table()
        if helpers.checksum_ok(bytes = root.bytes) == false {
            self.panic.fail(code = 0xAC010004)
        }
        let entry_size = 4
        if self.use_xsdt {
            entry_size = 8
        }
        let count = (root.length - 36) / entry_size
        let index = 0
        while index < count {
            let address = root.bytes.read_u32(offset = 36 + (index * entry_size))
            if self.use_xsdt {
                address = root.bytes.read_u64(offset = 36 + (index * entry_size))
            }
            let table = helpers.table_at(address = address)
            if table.signature == signature {
                if helpers.checksum_ok(bytes = table.bytes) == false {
                    self.panic.fail(code = 0xAC010005)
                }
                return table
            }
            index = index + 1
        }
        self.panic.fail(code = 0xAC010006)
    }

    fn require_madt(self) -> MadtTable {
        return MadtTable(table = self.require_table(signature = 0x43495041), panic = self.panic)
    }

    fn require_mcfg(self) -> McfgTable {
        return McfgTable(table = self.require_table(signature = 0x4746434D), panic = self.panic)
    }
}
```

Replace the entire Task 2 shell in `wrela/platform/acpi/madt.wrela` with this compileable shell. Task 6 replaces this shell again with real MADT parsing:

```wrela
module platform.acpi.madt

use { AcpiTable } from platform.acpi.tables
use { BootPanic } from platform.hardware.panic

class MadtTable {
    table: AcpiTable
    panic: BootPanic
}
```

Replace the entire Task 2 shell in `wrela/platform/acpi/mcfg.wrela` with this compileable shell. Task 7 replaces this shell again with real MCFG parsing:

```wrela
module platform.acpi.mcfg

use { AcpiTable } from platform.acpi.tables
use { BootPanic } from platform.hardware.panic

class McfgTable {
    table: AcpiTable
    panic: BootPanic
}
```

- [ ] **Step 3: Add synthetic ACPI contract tests**

Create `compiler/sem/acpi_parser_contract_test.go`:

```go
package sem

import (
    "encoding/binary"
    "os"
    "path/filepath"
    "strings"
    "testing"
)

func TestAcpiSyntheticRootTableContract(t *testing.T) {
    rsdp := make([]byte, 36)
    copy(rsdp[0:8], []byte("RSD PTR "))
    rsdp[15] = 2
    binary.LittleEndian.PutUint32(rsdp[20:24], uint32(len(rsdp)))
    binary.LittleEndian.PutUint64(rsdp[24:32], 0x12345000)
    rsdp[8] = checksumByte(rsdp[:20])
    rsdp[32] = checksumByte(rsdp)
    if checksumSum(rsdp[:20]) != 0 || checksumSum(rsdp) != 0 {
        t.Fatalf("synthetic RSDP checksums are invalid")
    }

    xsdt := make([]byte, 44)
    copy(xsdt[0:4], []byte("XSDT"))
    binary.LittleEndian.PutUint32(xsdt[4:8], uint32(len(xsdt)))
    binary.LittleEndian.PutUint64(xsdt[36:44], 0xfeedbeef)
    xsdt[9] = checksumByte(xsdt)
    if got := binary.LittleEndian.Uint64(xsdt[36:44]); got != 0xfeedbeef {
        t.Fatalf("XSDT entry offset = %#x", got)
    }

    sourceText := readRepoFile(t, "wrela/platform/acpi/root.wrela")
    for _, want := range []string{
        "read_u8(offset = 15)",
        "read_u32(offset = 20)",
        "read_u64(offset = 24)",
        "root.length - 36",
        "entry_size = 8",
        "panic.fail(code = 0xAC010006)",
    } {
        if !strings.Contains(sourceText, want) {
            t.Fatalf("ACPI root source missing %q", want)
        }
    }
}

func checksumByte(data []byte) byte {
    return byte(0 - checksumSum(data))
}

func checksumSum(data []byte) byte {
    var sum byte
    for _, b := range data {
        sum += b
    }
    return sum
}

func readRepoFile(t *testing.T, rel string) string {
    t.Helper()
    wd, err := os.Getwd()
    if err != nil {
        t.Fatal(err)
    }
    raw, err := os.ReadFile(filepath.Join(wd, "..", "..", rel))
    if err != nil {
        t.Fatal(err)
    }
    return string(raw)
}
```

Add this codegen test to `compiler/codegen/uefi_source_codegen_test.go`:

`findIRFunction`, `functionHasConstInt`, and `functionCalls` already exist in `compiler/codegen/uefi_source_codegen_test.go`; reuse those helpers and do not create duplicates.

```go
func TestAcpiRootRequireTableLowersChecksumAndOffsets(t *testing.T) {
    checked := parseCheckedUEFIModules(t)
    program, ds := ir.Lower(checked)
    if len(ds) != 0 {
        t.Fatalf("Lower diagnostics: %#v", ds)
    }
    requireTable := findIRFunction(program, "_wrela_method_platform_acpi_root_AcpiRoot_require_table")
    if requireTable == nil {
        t.Fatalf("missing AcpiRoot.require_table")
    }
    for _, want := range []uint64{36, 8} {
        if !functionHasConstInt(requireTable, want) {
            t.Fatalf("AcpiRoot.require_table missing constant %#x", want)
        }
    }
    if !functionCalls(requireTable, "_wrela_method_platform_acpi_tables_AcpiHelpers_checksum_ok") {
        t.Fatalf("AcpiRoot.require_table must validate table checksums")
    }
    requireMadt := findIRFunction(program, "_wrela_method_platform_acpi_root_AcpiRoot_require_madt")
    if requireMadt == nil || !functionHasConstInt(requireMadt, 0x43495041) {
        t.Fatalf("AcpiRoot.require_madt must pass APIC signature 0x43495041")
    }
    requireMcfg := findIRFunction(program, "_wrela_method_platform_acpi_root_AcpiRoot_require_mcfg")
    if requireMcfg == nil || !functionHasConstInt(requireMcfg, 0x4746434D) {
        t.Fatalf("AcpiRoot.require_mcfg must pass MCFG signature 0x4746434D")
    }
}
```

- [ ] **Step 4: Verify**

Run:

```bash
go test ./compiler/sem -run 'TestHardwareDiscoverySourceShape|TestAcpiSyntheticRootTableContract' -v
go test ./compiler/codegen -run TestAcpiRootRequireTableLowersChecksumAndOffsets -v
git diff --check
```

- [ ] **Step 5: Commit**

```bash
git add wrela/platform/acpi/tables.wrela wrela/platform/acpi/root.wrela wrela/platform/acpi/madt.wrela wrela/platform/acpi/mcfg.wrela compiler/sem/acpi_parser_contract_test.go compiler/sem/hardware_discovery_test.go compiler/codegen/uefi_source_codegen_test.go
git commit -m "feat: add acpi root table discovery -Codex Automated"
```

**Acceptance Criteria:** ACPI RSDP is found through UEFI configuration tables; RSDP and root table checksums are validated; required table lookup is source code and boot-fatal on absence.

### Task 6: Implement MADT CPU, LAPIC, IOAPIC, And Override Parsing

**Description:** Parse MADT entries into bounded first-milestone sets for two enabled CPUs, one IOAPIC, and eight interrupt overrides.

**Files:**
- Modify: `wrela/platform/acpi/madt.wrela`
- Modify: `wrela/machine/x86_64/cpu_state.wrela`
- Modify: `wrela/machine/x86_64/interrupts.wrela`
- Add: `compiler/sem/madt_parser_contract_test.go`
- Modify: `compiler/sem/hardware_discovery_test.go`

- [ ] **Step 1: Add failing shape checks**

```go
madt := moduleType(t, index, "platform.acpi.madt", "MadtTable")
assertMethodExists(t, madt, "local_apic_base")
assertMethodExists(t, madt, "enabled_cpus")
assertMethodExists(t, madt, "io_apics")
assertMethodExists(t, madt, "interrupt_source_overrides")
assertMethodExists(t, madt, "interrupt_authority")
```

- [ ] **Step 2: Replace Task 5 MADT shell with public source**

Replace the entire Task 5 shell in `wrela/platform/acpi/madt.wrela` with:

```wrela
module platform.acpi.madt

use { AcpiTable } from platform.acpi.tables
use { BootPanic } from platform.hardware.panic
use { CpuTopology, EnabledCpu, EnabledCpuSet } from machine.x86_64.cpu_state
use { InterruptAuthority, InterruptOverrideSet, IoApicDiscovered, IoApicSet, LocalApic } from machine.x86_64.interrupts

class MadtTable {
    table: AcpiTable
    panic: BootPanic

    fn local_apic_base(self) -> PhysicalAddress {
        return self.table.bytes.read_u32(offset = 36)
    }

    fn enabled_cpus(self) -> EnabledCpuSet {
        let out = EnabledCpuSet(count = 0, cpu0 = EnabledCpu(uid = 0, apic_id = 0), cpu1 = EnabledCpu(uid = 0, apic_id = 0))
        let offset = 44
        while offset < self.table.length {
            let entry_type = self.table.bytes.read_u8(offset = offset)
            let entry_length = self.table.bytes.read_u8(offset = offset + 1)
            if entry_type == 0 {
                let flags = self.table.bytes.read_u32(offset = offset + 4)
                if (flags & 1) != 0 {
                    out.append(uid = self.table.bytes.read_u8(offset = offset + 2), apic_id = self.table.bytes.read_u8(offset = offset + 3))
                }
            }
            if entry_type == 9 {
                let flags = self.table.bytes.read_u32(offset = offset + 8)
                if (flags & 1) != 0 {
                    out.append(uid = self.table.bytes.read_u32(offset = offset + 12), apic_id = self.table.bytes.read_u32(offset = offset + 4))
                }
            }
            offset = offset + entry_length
        }
        return out
    }

    fn io_apics(self) -> IoApicSet {
        let out = IoApicSet(count = 0, io_apic0 = IoApicDiscovered(id = 0, address = 0, gsi_base = 0, panic = self.panic))
        let offset = 44
        while offset < self.table.length {
            let entry_type = self.table.bytes.read_u8(offset = offset)
            let entry_length = self.table.bytes.read_u8(offset = offset + 1)
            if entry_type == 1 {
                out.append(id = self.table.bytes.read_u8(offset = offset + 2), address = self.table.bytes.read_u32(offset = offset + 4), gsi_base = self.table.bytes.read_u32(offset = offset + 8), panic = self.panic)
            }
            offset = offset + entry_length
        }
        return out
    }

    fn interrupt_source_overrides(self) -> InterruptOverrideSet {
        let zero = InterruptOverride(bus = 0, source = 0, gsi = 0, flags = 0)
        let out = InterruptOverrideSet(count = 0, override0 = zero, override1 = zero, override2 = zero, override3 = zero, override4 = zero, override5 = zero, override6 = zero, override7 = zero)
        let offset = 44
        while offset < self.table.length {
            let entry_type = self.table.bytes.read_u8(offset = offset)
            let entry_length = self.table.bytes.read_u8(offset = offset + 1)
            if entry_type == 2 {
                out.append(
                    bus = self.table.bytes.read_u8(offset = offset + 2),
                    source = self.table.bytes.read_u8(offset = offset + 3),
                    gsi = self.table.bytes.read_u32(offset = offset + 4),
                    flags = self.table.bytes.read_u16(offset = offset + 8)
                )
            }
            offset = offset + entry_length
        }
        return out
    }

    fn interrupt_authority(self) -> InterruptAuthority {
        let cpus = self.enabled_cpus().require_count(count = 1)
        return InterruptAuthority(
            local_apic = LocalApic(base = self.local_apic_base(), apic_id = cpus.bootstrap.apic_id, panic = self.panic),
            io_apics = self.io_apics(),
            overrides = self.interrupt_source_overrides(),
            panic = self.panic
        )
    }
}
```

- [ ] **Step 3: Add MADT synthetic offset test**

Create `compiler/sem/madt_parser_contract_test.go`:

```go
package sem

import (
    "encoding/binary"
    "strings"
    "testing"
)

func TestMadtSyntheticEntryOffsetContract(t *testing.T) {
    localApic := []byte{0, 8, 3, 7, 1, 0, 0, 0}
    if localApic[2] != 3 || localApic[3] != 7 || binary.LittleEndian.Uint32(localApic[4:8]) != 1 {
        t.Fatalf("local APIC synthetic entry corrupt")
    }

    x2apic := make([]byte, 16)
    x2apic[0] = 9
    x2apic[1] = 16
    binary.LittleEndian.PutUint32(x2apic[4:8], 0x123)
    binary.LittleEndian.PutUint32(x2apic[8:12], 1)
    binary.LittleEndian.PutUint32(x2apic[12:16], 0x55)
    if binary.LittleEndian.Uint32(x2apic[4:8]) != 0x123 ||
        binary.LittleEndian.Uint32(x2apic[8:12]) != 1 ||
        binary.LittleEndian.Uint32(x2apic[12:16]) != 0x55 {
        t.Fatalf("x2APIC synthetic entry corrupt")
    }

    sourceText := readRepoFile(t, "wrela/platform/acpi/madt.wrela")
    for _, want := range []string{
        "entry_type == 9",
        "read_u32(offset = offset + 4)",
        "read_u32(offset = offset + 8)",
        "read_u32(offset = offset + 12)",
        "out.append(uid = self.table.bytes.read_u32(offset = offset + 12), apic_id = self.table.bytes.read_u32(offset = offset + 4))",
    } {
        if !strings.Contains(sourceText, want) {
            t.Fatalf("MADT source missing %q", want)
        }
    }
}
```

- [ ] **Step 4: Add CPU topology source**

In `wrela/machine/x86_64/cpu_state.wrela`, add:

```wrela
use { BootPanic } from platform.hardware.panic

data EnabledCpu {
    uid: U32
    apic_id: U32
}

class EnabledCpuSet {
    count: U64
    cpu0: EnabledCpu
    cpu1: EnabledCpu

    fn append(self, uid: U32, apic_id: U32) {
        if self.count == 0 {
            self.cpu0 = EnabledCpu(uid = uid, apic_id = apic_id)
            self.count = 1
            return
        }
        if self.count == 1 {
            self.cpu1 = EnabledCpu(uid = uid, apic_id = apic_id)
            self.count = 2
            return
        }
    }

    fn require_count(self, count: U64) -> CpuTopology {
        if self.count < count {
            BootPanic().fail(code = 0xAC040001)
        }
        return CpuTopology(bootstrap = self.cpu0, secondary = self.cpu1)
    }
}

data CpuTopology {
    bootstrap: EnabledCpu
    secondary: EnabledCpu
}
```

Add these shell authority records to `wrela/machine/x86_64/interrupts.wrela`; Task 8 fills in route programming methods:

```wrela
use { BootPanic } from platform.hardware.panic

data InterruptVector {
    value: U32
}

data InterruptOverride {
    bus: U8
    source: U8
    gsi: U32
    flags: U16
}

class InterruptOverrideSet {
    count: U64
    override0: InterruptOverride
    override1: InterruptOverride
    override2: InterruptOverride
    override3: InterruptOverride
    override4: InterruptOverride
    override5: InterruptOverride
    override6: InterruptOverride
    override7: InterruptOverride

    fn append(self, bus: U8, source: U8, gsi: U32, flags: U16) {
        let entry = InterruptOverride(bus = bus, source = source, gsi = gsi, flags = flags)
        if self.count == 0 { self.override0 = entry }
        if self.count == 1 { self.override1 = entry }
        if self.count == 2 { self.override2 = entry }
        if self.count == 3 { self.override3 = entry }
        if self.count == 4 { self.override4 = entry }
        if self.count == 5 { self.override5 = entry }
        if self.count == 6 { self.override6 = entry }
        if self.count == 7 { self.override7 = entry }
        if self.count < 8 { self.count = self.count + 1 }
    }
}

class IoApicDiscovered {
    id: U8
    address: PhysicalAddress
    gsi_base: U32
    panic: BootPanic
}

class IoApicSet {
    count: U64
    io_apic0: IoApicDiscovered

    fn append(self, id: U8, address: PhysicalAddress, gsi_base: U32, panic: BootPanic) {
        if self.count == 0 {
            self.io_apic0 = IoApicDiscovered(id = id, address = address, gsi_base = gsi_base, panic = panic)
            self.count = 1
        }
    }
}

class LocalApic {
    base: PhysicalAddress
    apic_id: U32
    panic: BootPanic
}

class InterruptAuthority {
    local_apic: LocalApic
    io_apics: IoApicSet
    overrides: InterruptOverrideSet
    panic: BootPanic
}
```

- [ ] **Step 5: Verify**

Run:

```bash
go test ./compiler/sem -run 'TestHardwareDiscoverySourceShape|TestMadtSyntheticEntryOffsetContract' -v
git diff --check
```

- [ ] **Step 6: Commit**

```bash
git add wrela/platform/acpi/madt.wrela wrela/machine/x86_64/cpu_state.wrela wrela/machine/x86_64/interrupts.wrela compiler/sem/madt_parser_contract_test.go compiler/sem/hardware_discovery_test.go
git commit -m "feat: parse madt topology in source -Codex Automated"
```

**Acceptance Criteria:** MADT parsing derives LAPIC base, two enabled APIC IDs, one IOAPIC authority, and up to eight ISA interrupt source overrides.

### Task 7: Implement MCFG And ECAM Window Parsing

**Description:** Parse ACPI MCFG entries into a bounded ECAM window set used by PCIe enumeration.

**Files:**
- Modify: `wrela/platform/acpi/mcfg.wrela`
- Modify: `wrela/machine/x86_64/pci.wrela`
- Add: `compiler/sem/mcfg_parser_contract_test.go`
- Modify: `compiler/sem/hardware_discovery_test.go`

- [ ] **Step 1: Add failing source-shape checks**

```go
assertMethodExists(t, moduleType(t, index, "platform.acpi.mcfg", "McfgTable"), "ecam_windows")
_ = moduleType(t, index, "machine.x86_64.pci", "PcieEcamWindows")
```

- [ ] **Step 2: Replace Task 2 ECAM window shell and add window set**

In `wrela/machine/x86_64/pci.wrela`, replace the Task 2 `PcieEcamWindow` shell with the definition below and add `PcieEcamWindows`. Do not add a duplicate `PcieEcamWindow` class. Do not delete the legacy q35 helpers yet:

```wrela
use { BootPanic } from platform.hardware.panic

class PcieEcamWindow {
    base: PhysicalAddress
    segment: U16
    start_bus: U8
    end_bus: U8
    panic: BootPanic
}

class PcieEcamWindows {
    count: U64
    window0: PcieEcamWindow
    panic: BootPanic

    fn append(self, window: PcieEcamWindow) {
        if self.count == 0 {
            self.window0 = window
            self.count = 1
        }
    }
}
```

- [ ] **Step 3: Replace Task 5 MCFG shell**

Replace the entire Task 5 shell in `wrela/platform/acpi/mcfg.wrela` with:

```wrela
module platform.acpi.mcfg

use { AcpiTable } from platform.acpi.tables
use { BootPanic } from platform.hardware.panic
use { PcieEcamWindow, PcieEcamWindows } from machine.x86_64.pci

class McfgTable {
    table: AcpiTable
    panic: BootPanic

    fn ecam_windows(self) -> PcieEcamWindows {
        let empty_window = PcieEcamWindow(base = 0, segment = 0, start_bus = 0, end_bus = 0, panic = self.panic)
        let windows = PcieEcamWindows(count = 0, window0 = empty_window, panic = self.panic)
        let offset = 44
        while offset < self.table.length {
            windows.append(window = PcieEcamWindow(
                base = self.table.bytes.read_u64(offset = offset),
                segment = self.table.bytes.read_u16(offset = offset + 8),
                start_bus = self.table.bytes.read_u8(offset = offset + 10),
                end_bus = self.table.bytes.read_u8(offset = offset + 11),
                panic = self.panic
            ))
            offset = offset + 16
        }
        return windows
    }
}
```

- [ ] **Step 4: Add MCFG synthetic offset test**

Create `compiler/sem/mcfg_parser_contract_test.go`:

```go
package sem

import (
    "encoding/binary"
    "strings"
    "testing"
)

func TestMcfgSyntheticEcamWindowContract(t *testing.T) {
    entry := make([]byte, 16)
    binary.LittleEndian.PutUint64(entry[0:8], 0xE0000000)
    binary.LittleEndian.PutUint16(entry[8:10], 0)
    entry[10] = 0
    entry[11] = 255
    if binary.LittleEndian.Uint64(entry[0:8]) != 0xE0000000 || entry[10] != 0 || entry[11] != 255 {
        t.Fatalf("synthetic MCFG entry corrupt")
    }

    sourceText := readRepoFile(t, "wrela/platform/acpi/mcfg.wrela")
    for _, want := range []string{
        "offset = 44",
        "read_u64(offset = offset)",
        "read_u16(offset = offset + 8)",
        "read_u8(offset = offset + 10)",
        "read_u8(offset = offset + 11)",
        "offset = offset + 16",
    } {
        if !strings.Contains(sourceText, want) {
            t.Fatalf("MCFG source missing %q", want)
        }
    }
}
```

- [ ] **Step 5: Verify**

Run:

```bash
go test ./compiler/sem -run 'TestHardwareDiscoverySourceShape|TestMcfgSyntheticEcamWindowContract' -v
git diff --check
```

- [ ] **Step 6: Commit**

```bash
git add wrela/platform/acpi/mcfg.wrela wrela/machine/x86_64/pci.wrela compiler/sem/mcfg_parser_contract_test.go compiler/sem/hardware_discovery_test.go
git commit -m "feat: parse mcfg ecam windows in source -Codex Automated"
```

**Acceptance Criteria:** MCFG parsing returns a bounded ECAM window set; missing MCFG remains boot-fatal through `AcpiRoot.require_mcfg`.

---

## 8. Phase 4: Interrupt Routing And CPU Startup From Discovered APIC Facts

**Description:** Replace LAPIC/IOAPIC q35 literals with MADT-derived authorities and route ISA IRQs through interrupt source overrides.

**Acceptance Criteria:**
- COM1 route uses MADT IOAPIC base and override mapping.
- LAPIC base used for EOI and vCPU startup comes from `HardwarePlan`.
- Secondary vCPU startup targets MADT APIC ID.

**Code Example:**

```wrela
let serial_route = hardware.hardware_plan.interrupts.serial_irq4
let controller = ApicInterruptController(
    local_apic = hardware.hardware_plan.interrupts.local_apic
)
```

### Task 8: Refactor Interrupt Authorities And Serial Route Programming

**Description:** Change APIC types to carry `PhysicalAddress` and program IOAPIC routes from `IoApicRoute` values.

**Files:**
- Modify: `wrela/machine/x86_64/interrupts.wrela`
- Modify: `wrela/machine/x86_64/serial.wrela`
- Modify: `compiler/sem/hardware_discovery_test.go`
- Modify: `compiler/codegen/uefi_source_codegen_test.go`

- [ ] **Step 1: Add failing shape checks**

```go
interrupts := moduleType(t, index, "machine.x86_64.interrupts", "InterruptAuthority")
assertMethodExists(t, interrupts, "route_isa_irq")
assertMethodExists(t, moduleType(t, index, "machine.x86_64.interrupts", "IoApicRoute"), "program")
route := moduleType(t, index, "machine.x86_64.interrupts", "IoApicRoute")
if fieldTypeName(t, route, "destination_apic_id") != "U32" || fieldTypeName(t, route, "flags") != "U16" {
    t.Fatalf("IoApicRoute must carry destination APIC ID and MADT override flags")
}
source := readRepoFile(t, "wrela/machine/x86_64/interrupts.wrela")
for _, want := range []string{"self.destination_apic_id << 24", "flags & 0x0003", "flags & 0x000C", "flags_for_isa_irq"} {
    if !strings.Contains(source, want) {
        t.Fatalf("interrupt route source missing %q", want)
    }
}
```

- [ ] **Step 2: Replace interrupt source**

Replace the entire `wrela/machine/x86_64/interrupts.wrela` module body with this public shape. This intentionally replaces the Task 6 shell declarations for `LocalApic`, `IoApicDiscovered`, `InterruptOverrideSet`, `IoApicSet`, and `InterruptAuthority`; do not keep duplicate shell classes.

```wrela
module machine.x86_64.interrupts

use { MmioRegion } from platform.hardware.bytes
use { BootPanic } from platform.hardware.panic

data InterruptVector {
    value: U32
}

data InterruptOverride {
    bus: U8
    source: U8
    gsi: U32
    flags: U16
}

class InterruptOverrideSet {
    count: U64
    override0: InterruptOverride
    override1: InterruptOverride
    override2: InterruptOverride
    override3: InterruptOverride
    override4: InterruptOverride
    override5: InterruptOverride
    override6: InterruptOverride
    override7: InterruptOverride

    fn append(self, bus: U8, source: U8, gsi: U32, flags: U16) {
        let entry = InterruptOverride(bus = bus, source = source, gsi = gsi, flags = flags)
        if self.count == 0 { self.override0 = entry }
        if self.count == 1 { self.override1 = entry }
        if self.count == 2 { self.override2 = entry }
        if self.count == 3 { self.override3 = entry }
        if self.count == 4 { self.override4 = entry }
        if self.count == 5 { self.override5 = entry }
        if self.count == 6 { self.override6 = entry }
        if self.count == 7 { self.override7 = entry }
        if self.count < 8 { self.count = self.count + 1 }
    }

    fn at(self, index: U64) -> InterruptOverride {
        if index == 0 { return self.override0 }
        if index == 1 { return self.override1 }
        if index == 2 { return self.override2 }
        if index == 3 { return self.override3 }
        if index == 4 { return self.override4 }
        if index == 5 { return self.override5 }
        if index == 6 { return self.override6 }
        return self.override7
    }

    fn gsi_for_isa_irq(self, irq: U8) -> U32 {
        let index = 0
        while index < self.count {
            let entry = self.at(index = index)
            if entry.bus == 0 {
                if entry.source == irq {
                    return entry.gsi
                }
            }
            index = index + 1
        }
        return irq
    }

    fn flags_for_isa_irq(self, irq: U8) -> U16 {
        let index = 0
        while index < self.count {
            let entry = self.at(index = index)
            if entry.bus == 0 {
                if entry.source == irq {
                    return entry.flags
                }
            }
            index = index + 1
        }
        return 0
    }
}

class LocalApic {
    base: PhysicalAddress
    apic_id: U32
    panic: BootPanic

    fn mmio(self) -> MmioRegion {
        return MmioRegion(address = self.base, length = 4096, panic = self.panic)
    }

    fn enable(self) {
        self.mmio().write32(offset = 0xF0, value = 0x1FF)
    }

    fn eoi(self) {
        self.mmio().write32(offset = 0xB0, value = 0)
    }

    fn message_address(self) -> U32 {
        return 0xFEE00000 | (self.apic_id << 12)
    }
}

class IoApicDiscovered {
    id: U8
    address: PhysicalAddress
    gsi_base: U32
    panic: BootPanic

    fn mmio(self) -> MmioRegion {
        return MmioRegion(address = self.address, length = 4096, panic = self.panic)
    }

    fn redirection_count(self) -> U32 {
        self.mmio().write32(offset = 0, value = 1)
        let version = self.mmio().read32(offset = 0x10)
        return ((version >> 16) & 0xFF) + 1
    }
}

class IoApicSet {
    count: U64
    io_apic0: IoApicDiscovered

    fn append(self, id: U8, address: PhysicalAddress, gsi_base: U32, panic: BootPanic) {
        if self.count == 0 {
            self.io_apic0 = IoApicDiscovered(id = id, address = address, gsi_base = gsi_base, panic = panic)
            self.count = 1
        }
    }
}

class IoApicRoute {
    io_apic: IoApicDiscovered
    gsi: U32
    flags: U16
    vector: InterruptVector
    destination_apic_id: U32

    fn program(self) {
        let redir = (self.gsi - self.io_apic.gsi_base) * 2
        let low = self.vector.value
        if (self.flags & 0x0003) == 0x0003 {
            low = low | (1 << 13)
        }
        if (self.flags & 0x000C) == 0x000C {
            low = low | (1 << 15)
        }
        self.io_apic.mmio().write32(offset = 0, value = 0x10 + redir)
        self.io_apic.mmio().write32(offset = 0x10, value = low)
        self.io_apic.mmio().write32(offset = 0, value = 0x11 + redir)
        self.io_apic.mmio().write32(offset = 0x10, value = self.destination_apic_id << 24)
    }
}

class InterruptAuthority {
    local_apic: LocalApic
    io_apics: IoApicSet
    overrides: InterruptOverrideSet
    panic: BootPanic

    fn route_isa_irq(self, irq: U8, vector: InterruptVector) -> IoApicRoute {
        let gsi = self.overrides.gsi_for_isa_irq(irq = irq)
        let flags = self.overrides.flags_for_isa_irq(irq = irq)
        let io_apic = self.io_apics.io_apic0
        if gsi < io_apic.gsi_base {
            self.panic.fail(code = 0xAC050001)
        }
        if gsi >= io_apic.gsi_base + io_apic.redirection_count() {
            self.panic.fail(code = 0xAC050002)
        }
        return IoApicRoute(io_apic = io_apic, gsi = gsi, flags = flags, vector = vector, destination_apic_id = self.local_apic.apic_id)
    }
}

class ApicInterruptController {
    local_apic: LocalApic

    asm fn enable_cpu_interrupts(self) {
        sti
        ret
    }

    fn initialize_for_com1_receive(self) {
        self.local_apic.enable()
    }

    fn eoi(self) {
        self.local_apic.eoi()
    }
}
```

- [ ] **Step 3: Update serial driver path**

Change `SerialDriver.create_console_path` to accept a route and program it:

```wrela
fn create_console_path(self, identity: PathIdentity, route: IoApicRoute, rx: SerialRxPublisher) -> SerialConsolePath {
    let console_path = SerialConsolePath(identity = identity, registers = self.registers, route = route, rx = rx)
    console_path.enable_receive_interrupts()
    return console_path
}
```

Add `route: IoApicRoute` to `SerialConsolePath` and call `self.route.program()` inside `enable_receive_interrupts` before enabling UART interrupts.

- [ ] **Step 4: Verify**

Run:

```bash
go test ./compiler/sem -run TestHardwareDiscoverySourceShape -v
go test ./compiler/codegen -run TestInterruptPlatformSourceCodegen -v
git diff --check
```

- [ ] **Step 5: Commit**

```bash
git add wrela/machine/x86_64/interrupts.wrela wrela/machine/x86_64/serial.wrela compiler/sem/hardware_discovery_test.go compiler/codegen/uefi_source_codegen_test.go
git commit -m "feat: route interrupts from madt authority -Codex Automated"
```

**Acceptance Criteria:** Serial interrupt routing comes from `InterruptAuthority.route_isa_irq`; `LocalApic` and `IoApicDiscovered` store `PhysicalAddress`; no source-visible APIC base literal remains in `interrupts.wrela`.

### Task 9: Use Discovered LAPIC Base And APIC IDs For vCPU Start

**Description:** Extend CPU/Vcpu plans and codegen so AP startup uses MADT APIC IDs and discovered LAPIC base.

**Files:**
- Modify: `wrela/machine/x86_64/cpu_state.wrela`
- Modify: `compiler/ir/ir.go`
- Modify: `compiler/ir/lower.go`
- Modify: `compiler/ir/lower_source_test.go`
- Modify: `compiler/codegen/lapic.go`
- Modify: `compiler/codegen/vcpu_start.go`
- Modify: `compiler/codegen/vcpu_start_test.go`

- [ ] **Step 1: Add failing lowering/codegen tests**

Use direct Go IR construction in `compiler/codegen/vcpu_start_test.go`. Do not write Wrela source that constructs `Vcpu`; `Vcpu` is an edge-module capability class after Task 13.

Update `TestVcpuStartEmitsLapicIcrWrites` so the test program carries the discovered APIC ID and LAPIC base:

```go
program := &ir.Program{
    VcpuStarts: []ir.VcpuStartPlan{{
        VcpuID: 1,
        APICID: 7,
        LocalApicBase: 0xfee01000,
        SlotLabel: "worker",
        Terminal: false,
    }},
    Functions: []ir.Function{{
        Symbol: "start_worker",
        Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
            worker,
            &ir.VcpuStart{
                VcpuID: 1,
                APICID: 7,
                LocalApicBase: 0xfee01000,
                SlotLabel: "worker",
                Type: statusType,
                Executor: worker,
            },
        }}},
    }, {
        Symbol: "_wrela_method_test_Worker_run",
        Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
            &ir.Return{},
        }}},
    }},
}
image, ds := Compile(program)
if len(ds) != 0 {
    t.Fatalf("Compile diagnostics: %#v", ds)
}
code := symbolBytes(t, image, "start_worker")
if !bytes.Contains(code, u32le(7 << 24)) {
    t.Fatalf("start_worker must target APIC ID 7 in ICR high dword: %x", code)
}
if !bytes.Contains(code, u32le(0xfee01000)) {
    t.Fatalf("start_worker must use discovered LAPIC base 0xfee01000: %x", code)
}
if bytes.Contains(code, u32le(0xFEE00000)) {
    t.Fatalf("start_worker must not embed the default LAPIC base: %x", code)
}
```

- [ ] **Step 2: Update Wrela CPU state**

Change `Vcpu`:

```wrela
class Vcpu {
    id: U64
    apic_id: U32
    local_apic_base: PhysicalAddress
}
```

Add methods:

```wrela
fn vcpu0(self, local_apic_base: PhysicalAddress) -> Vcpu {
    return Vcpu(id = 0, apic_id = self.bootstrap.apic_id, local_apic_base = local_apic_base)
}

fn vcpu1(self, local_apic_base: PhysicalAddress) -> Vcpu {
    return Vcpu(id = 1, apic_id = self.secondary.apic_id, local_apic_base = local_apic_base)
}
```

- [ ] **Step 3: Update IR**

Extend `ir.VcpuStart` and `ir.VcpuEnter`:

```go
type VcpuStart struct {
    VcpuID int
    APICID uint32
    LocalApicBase uint64
    Executor Value
    Type Type
    SlotLabel string
}
```

Lower `hardware.vcpu1.start` by loading fields `id`, `apic_id`, and `local_apic_base` from the receiver origin instead of deriving APIC ID from `id`.

- [ ] **Step 4: Update codegen**

Replace `lapicBase` constant use with a parameter:

```go
func emitLapicWrite(e *Emitter, base uint64, offset uint32, value uint32) {
    e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
        asm.RegOperand{Reg: asm.MustLookup("r11")},
        asm.ImmOperand{Value: int64(base)},
    }})
    emitMovImmToReg(e, asm.MustLookup("rax"), int64(value))
    emitStoreMemFromReg(e, asm.MustLookup("r11"), int64(offset), asm.MustLookup("rax"), 32)
}
```

In `emitVcpuStart`, send INIT/SIPI to `op.APICID`:

```go
emitLapicWrite(e, op.LocalApicBase, lapicICRHigh, op.APICID<<24)
emitLapicWrite(e, op.LocalApicBase, lapicICRLow, 0x00004500)
```

- [ ] **Step 5: Verify**

Run:

```bash
go test ./compiler/ir -run TestLower.*Vcpu -v
go test ./compiler/codegen -run TestVcpu -v
git diff --check
```

- [ ] **Step 6: Commit**

```bash
git add wrela/machine/x86_64/cpu_state.wrela compiler/ir/ir.go compiler/ir/lower.go compiler/ir/lower_source_test.go compiler/codegen/lapic.go compiler/codegen/vcpu_start.go compiler/codegen/vcpu_start_test.go
git commit -m "feat: start vcpus with discovered apic ids -Codex Automated"
```

**Acceptance Criteria:** vCPU startup no longer uses `Vcpu.id` as APIC ID; LAPIC writes use the discovered base carried by the vCPU receiver.

---

## 9. Phase 5: PCIe ECAM Enumeration, BARs, MSI, And MSI-X

**Description:** Replace q35 CF8/CFC slot probing with source-visible PCIe ECAM enumeration and authority-based claims.

**Acceptance Criteria:**
- EDU and two ivshmem-doorbell devices are found by vendor/device occurrence.
- BAR base and size are read through ECAM.
- MSI/MSI-X capability offsets are discovered by walking capability lists.
- Message address uses discovered LAPIC APIC ID/base.

**Code Example:**

```wrela
let edu = pci.require_device(vendor_id = 0x1234, device_id = 0x11E8, occurrence = 0)
let edu_bar0 = edu.claim_mmio_bar(index = 0)
let edu_msi = edu.claim_msi()
```

### Task 10: Implement PCIe ECAM Window Enumeration

**Description:** Add ECAM config read/write and bounded device-set enumeration.

**Files:**
- Modify: `wrela/machine/x86_64/pci.wrela`
- Modify: `compiler/sem/hardware_discovery_test.go`
- Add: `compiler/sem/pci_bar_contract_test.go`
- Add: `compiler/sem/pci_ecam_contract_test.go`

- [ ] **Step 1: Add failing source-shape checks**

```go
pci := moduleType(t, index, "machine.x86_64.pci", "PciDeviceSet")
assertMethodExists(t, pci, "require_device")
assertMethodExists(t, moduleType(t, index, "machine.x86_64.pci", "PcieEcamWindow"), "read_config32")
```

Create `compiler/sem/pci_ecam_contract_test.go`:

```go
package sem

import (
    "strings"
    "testing"
)

func TestPciEcamEnumerationSourceContract(t *testing.T) {
    source := readRepoFile(t, "wrela/machine/x86_64/pci.wrela")
    required := []string{
        "(bus - self.start_bus) << 20",
        "device << 15",
        "function << 12",
        "offset & 0x0FFC",
        "while bus <= self.window0.end_bus",
        "while device < 32",
        "while function < 8",
        "vendor_device & 0xFFFF",
        "self.panic.fail(code = 0xAC060012)",
        "self.panic.fail(code = 0xAC060010)",
    }
    for _, needle := range required {
        if !strings.Contains(source, needle) {
            t.Fatalf("PCI ECAM contract missing %q", needle)
        }
    }
}
```

- [ ] **Step 2: Replace legacy PCI source surface**

Keep the module name `machine.x86_64.pci`. Keep the legacy `Q35PciInterruptConfigurator` and `PciConfigPorts` declarations in this task so existing examples and tests remain green; Task 16 stops using them and Task 19 deletes them. Replace the Task 2/Task 7 shell declarations for `PcieEcamWindow`, `PcieEcamWindows`, `PciDeviceIdentity`, `PciDevice`, `PciDeviceSetBuilder`, and `PciDeviceSet` with the declarations below. Do not add duplicate classes with the same names.

```wrela
class PcieEcamWindow {
    base: PhysicalAddress
    segment: U16
    start_bus: U8
    end_bus: U8
    panic: BootPanic

    fn config_address(self, bus: U8, device: U8, function: U8, offset: U16) -> PhysicalAddress {
        // This milestone uses standard PCI capability space only; extended
        // capability offsets above 0x0FFF are out of scope and intentionally
        // masked to one ECAM function page.
        return self.base + ((bus - self.start_bus) << 20) + (device << 15) + (function << 12) + (offset & 0x0FFC)
    }

    fn mmio(self, bus: U8, device: U8, function: U8) -> MmioRegion {
        return MmioRegion(address = self.config_address(bus = bus, device = device, function = function, offset = 0), length = 4096, panic = self.panic)
    }

    fn read_config32(self, bus: U8, device: U8, function: U8, offset: U16) -> U32 {
        return self.mmio(bus = bus, device = device, function = function).read32(offset = offset)
    }

    fn write_config32(self, bus: U8, device: U8, function: U8, offset: U16, value: U32) {
        self.mmio(bus = bus, device = device, function = function).write32(offset = offset, value = value)
    }
}

class PcieEcamWindows {
    count: U64
    window0: PcieEcamWindow
    panic: BootPanic

    fn enumerate(self) -> PciDeviceSet {
        let devices = PciDeviceSetBuilder(panic = self.panic).empty()
        let bus = self.window0.start_bus
        while bus <= self.window0.end_bus {
            let device = 0
            while device < 32 {
                let function = 0
                while function < 8 {
                    let vendor_device = self.window0.read_config32(bus = bus, device = device, function = function, offset = 0)
                    if (vendor_device & 0xFFFF) != 0xFFFF {
                        devices.append(window = self.window0, bus = bus, device = device, function = function)
                    }
                    function = function + 1
                }
                device = device + 1
            }
            bus = bus + 1
        }
        return devices
    }
}
```

Define `PciDeviceIdentity`, `PciDevice`, `PciDeviceSetBuilder`, and `PciDeviceSet` exactly as shown below. `PciDeviceSet` has sixteen explicit slots because Wrela does not have dynamic arrays in this surface yet. Do not replace this with a map, list, or compiler helper.

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
}

class PciDevice {
    window: PcieEcamWindow
    identity: PciDeviceIdentity
    panic: BootPanic

    fn read_config32(self, offset: U16) -> U32 {
        return self.window.read_config32(bus = self.identity.bus, device = self.identity.device, function = self.identity.function, offset = offset)
    }

    fn write_config32(self, offset: U16, value: U32) {
        self.window.write_config32(bus = self.identity.bus, device = self.identity.device, function = self.identity.function, offset = offset, value = value)
    }
}

class PciDeviceSetBuilder {
    panic: BootPanic

    fn empty(self) -> PciDeviceSet {
        let empty_window = PcieEcamWindow(base = 0, segment = 0, start_bus = 0, end_bus = 0, panic = self.panic)
        let empty_identity = PciDeviceIdentity(segment = 0, bus = 0, device = 0, function = 0, vendor_id = 0xFFFF, device_id = 0xFFFF, class_code = 0, subclass = 0, prog_if = 0)
        let empty = PciDevice(window = empty_window, identity = empty_identity, panic = self.panic)
        return PciDeviceSet(count = 0, device0 = empty, device1 = empty, device2 = empty, device3 = empty, device4 = empty, device5 = empty, device6 = empty, device7 = empty, device8 = empty, device9 = empty, device10 = empty, device11 = empty, device12 = empty, device13 = empty, device14 = empty, device15 = empty, panic = self.panic)
    }
}

class PciDeviceSet {
    count: U64
    device0: PciDevice
    device1: PciDevice
    device2: PciDevice
    device3: PciDevice
    device4: PciDevice
    device5: PciDevice
    device6: PciDevice
    device7: PciDevice
    device8: PciDevice
    device9: PciDevice
    device10: PciDevice
    device11: PciDevice
    device12: PciDevice
    device13: PciDevice
    device14: PciDevice
    device15: PciDevice
    panic: BootPanic

    fn at(self, index: U64) -> PciDevice {
        if index == 0 { return self.device0 }
        if index == 1 { return self.device1 }
        if index == 2 { return self.device2 }
        if index == 3 { return self.device3 }
        if index == 4 { return self.device4 }
        if index == 5 { return self.device5 }
        if index == 6 { return self.device6 }
        if index == 7 { return self.device7 }
        if index == 8 { return self.device8 }
        if index == 9 { return self.device9 }
        if index == 10 { return self.device10 }
        if index == 11 { return self.device11 }
        if index == 12 { return self.device12 }
        if index == 13 { return self.device13 }
        if index == 14 { return self.device14 }
        if index == 15 { return self.device15 }
        self.panic.fail(code = 0xAC060011)
    }

    fn append(self, window: PcieEcamWindow, bus: U8, device: U8, function: U8) {
        if self.count >= 16 {
            self.panic.fail(code = 0xAC060012)
        }
        let vendor_device = window.read_config32(bus = bus, device = device, function = function, offset = 0)
        let class_reg = window.read_config32(bus = bus, device = device, function = function, offset = 8)
        let id = PciDeviceIdentity(
            segment = window.segment,
            bus = bus,
            device = device,
            function = function,
            vendor_id = vendor_device & 0xFFFF,
            device_id = vendor_device >> 16,
            class_code = class_reg >> 24,
            subclass = (class_reg >> 16) & 0xFF,
            prog_if = (class_reg >> 8) & 0xFF
        )
        let value = PciDevice(window = window, identity = id, panic = self.panic)
        if self.count == 0 { self.device0 = value }
        if self.count == 1 { self.device1 = value }
        if self.count == 2 { self.device2 = value }
        if self.count == 3 { self.device3 = value }
        if self.count == 4 { self.device4 = value }
        if self.count == 5 { self.device5 = value }
        if self.count == 6 { self.device6 = value }
        if self.count == 7 { self.device7 = value }
        if self.count == 8 { self.device8 = value }
        if self.count == 9 { self.device9 = value }
        if self.count == 10 { self.device10 = value }
        if self.count == 11 { self.device11 = value }
        if self.count == 12 { self.device12 = value }
        if self.count == 13 { self.device13 = value }
        if self.count == 14 { self.device14 = value }
        if self.count == 15 { self.device15 = value }
        self.count = self.count + 1
    }

    fn require_device(self, vendor_id: U16, device_id: U16, occurrence: U64) -> PciDevice {
        let seen = 0
        let index = 0
        while index < self.count {
            let candidate = self.at(index = index)
            if candidate.identity.vendor_id == vendor_id {
                if candidate.identity.device_id == device_id {
                    if seen == occurrence {
                        return candidate
                    }
                    seen = seen + 1
                }
            }
            index = index + 1
        }
        self.panic.fail(code = 0xAC060010)
    }
}
```

- [ ] **Step 3: Verify**

Run:

```bash
go test ./compiler/sem -run 'TestHardwareDiscoverySourceShape|TestPciEcamEnumerationSourceContract' -v
git diff --check
```

- [ ] **Step 4: Commit**

```bash
git add wrela/machine/x86_64/pci.wrela compiler/sem/hardware_discovery_test.go compiler/sem/pci_ecam_contract_test.go
git commit -m "feat: enumerate pci devices through ecam -Codex Automated"
```

**Acceptance Criteria:** PCI enumeration walks ECAM bus/device/function space; device selection uses vendor/device occurrence; the legacy CF8/CFC path remains only for old examples until Task 19 removes it.

### Task 11: Implement BAR Claiming

**Description:** Claim MMIO and IO BARs from discovered PCI devices and reject unsupported BAR shapes through boot-fatal paths.

**Files:**
- Modify: `wrela/machine/x86_64/pci.wrela`
- Modify: `compiler/sem/hardware_discovery_test.go`

- [ ] **Step 1: Add failing shape checks**

```go
dev := moduleType(t, index, "machine.x86_64.pci", "PciDevice")
assertMethodExists(t, dev, "claim_mmio_bar")
assertMethodExists(t, dev, "claim_io_bar")
```

- [ ] **Step 2: Add BAR behavior contract test**

Create `compiler/sem/pci_bar_contract_test.go`. This is a source-contract test, not a boot test. It prevents a shallow implementation that only adds method names.

```go
package sem

import (
    "strings"
    "testing"
)

func TestPciBarClaimSourceContract(t *testing.T) {
    source := readRepoFile(t, "wrela/machine/x86_64/pci.wrela")
    required := []string{
        "fn claim_mmio_bar(self, index: U8) -> MmioRegion",
        "fn claim_io_bar(self, index: U8) -> IoPortRegion",
        "self.write_config32(offset = offset, value = 0xFFFFFFFF)",
        "let mask = self.read_config32(offset = offset)",
        "original & 0xFFFFFFF0",
        "mask & 0xFFFFFFF0",
        "bar_type == 2",
        "0xAC060004",
        "original & 0xFFFC",
        "mask & 0xFFFC",
        "0xAC060001",
        "0xAC060002",
        "0xAC060003",
    }
    for _, needle := range required {
        if !strings.Contains(source, needle) {
            t.Fatalf("pci BAR contract missing %q", needle)
        }
    }
}
```

- [ ] **Step 3: Add BAR methods**

Implement:

```wrela
fn claim_mmio_bar(self, index: U8) -> MmioRegion {
    let offset = 0x10 + (index * 4)
    let original = self.read_config32(offset = offset)
    if (original & 1) != 0 {
        self.panic.fail(code = 0xAC060001)
    }
    self.write_config32(offset = offset, value = 0xFFFFFFFF)
    let mask = self.read_config32(offset = offset)
    self.write_config32(offset = offset, value = original)
    let bar_type = (original >> 1) & 3
    if bar_type == 0 {
        let base = original & 0xFFFFFFF0
        let size = (0 - (mask & 0xFFFFFFF0)) & 0xFFFFFFFF
        return MmioRegion(address = base, length = size, panic = self.panic)
    }
    if bar_type == 2 {
        self.panic.fail(code = 0xAC060004)
    }
    self.panic.fail(code = 0xAC060002)
}
```

64-bit BARs boot-fatal with `0xAC060004` in this milestone. Do not combine the high BAR dword into a 64-bit address until Wrela has an explicit U32-to-U64 widening pattern in source; silent truncation would be worse than rejecting the device.

Add this `claim_io_bar` method:

```wrela
fn claim_io_bar(self, index: U8) -> IoPortRegion {
    let offset = 0x10 + (index * 4)
    let original = self.read_config32(offset = offset)
    if (original & 1) == 0 {
        self.panic.fail(code = 0xAC060003)
    }
    self.write_config32(offset = offset, value = 0xFFFFFFFF)
    let mask = self.read_config32(offset = offset)
    self.write_config32(offset = offset, value = original)
    let base = original & 0xFFFC
    let size = (0 - (mask & 0xFFFC)) & 0xFFFF
    return IoPortRegion(port_base = base, length = size)
}
```

- [ ] **Step 4: Verify**

Run:

```bash
go test ./compiler/sem -run 'TestHardwareDiscoverySourceShape|TestPciBarClaimSourceContract' -v
git diff --check
```

- [ ] **Step 5: Commit**

```bash
git add wrela/machine/x86_64/pci.wrela compiler/sem/hardware_discovery_test.go compiler/sem/pci_bar_contract_test.go
git commit -m "feat: claim pci bars from config space -Codex Automated"
```

**Acceptance Criteria:** 32-bit MMIO BAR and IO BAR base/size are read from config space; 64-bit and unsupported BAR types call `BootPanic.fail`; callers receive `MmioRegion` or `IoPortRegion` authorities.

### Task 12: Implement MSI And MSI-X Capability Claiming

**Description:** Discover PCI capabilities and program MSI/MSI-X routes from source-visible capabilities.

**Files:**
- Modify: `wrela/machine/x86_64/pci.wrela`
- Modify: `wrela/machine/x86_64/edu.wrela`
- Modify: `wrela/machine/x86_64/ivshmem.wrela`
- Modify: `compiler/codegen/uefi_source_codegen_test.go`

- [ ] **Step 1: Add failing tests**

Replace `TestPciInterruptConfiguratorWalksCapabilities` with `TestPciCapabilitiesWalkedFromDiscoveredDevices`:

Use the existing `findIRFunction` and `functionHasConstInt` helpers in `compiler/codegen/uefi_source_codegen_test.go`.

```go
func TestPciCapabilitiesWalkedFromDiscoveredDevices(t *testing.T) {
    checked := parseCheckedUEFIModules(t)
    program, ds := ir.Lower(checked)
    if len(ds) != 0 {
        t.Fatalf("Lower diagnostics: %#v", ds)
    }
    msi := findIRFunction(program, "_wrela_method_machine_x86_64_pci_PciDevice_claim_msi")
    if msi == nil || !functionHasConstInt(msi, 0x05) {
        t.Fatalf("claim_msi must search capability id 0x05")
    }
    msix := findIRFunction(program, "_wrela_method_machine_x86_64_pci_PciDevice_claim_msix")
    if msix == nil || !functionHasConstInt(msix, 0x11) {
        t.Fatalf("claim_msix must search capability id 0x11")
    }
    for _, fn := range []*ir.Function{msi, msix} {
        if !functionHasConstInt(fn, 0x04) || !functionHasConstInt(fn, 0x00000006) {
            t.Fatalf("%s must enable PCI command memory and bus-master bits", fn.Symbol)
        }
    }
}
```

- [ ] **Step 2: Add MSI/MSI-X source**

In `pci.wrela`, define:

```wrela
data PciInterruptRoute {
    vector: InterruptVector
}
```

Add the following methods inside the existing `class PciDevice` declaration from Task 10, after `claim_io_bar`. Do not add a second `PciDevice` class and do not place these methods at module top level.

```wrela
fn find_capability(self, capability_id: U8) -> U16 {
    let status = self.read_config32(offset = 0x04)
    if (status & 0x00100000) == 0 {
        self.panic.fail(code = 0xAC060020)
    }
    let ptr = self.read_config32(offset = 0x34) & 0xFC
    let remaining = 48
    while remaining != 0 {
        if ptr == 0 {
            self.panic.fail(code = 0xAC060021)
        }
        let header = self.read_config32(offset = ptr)
        if (header & 0xFF) == capability_id {
            return ptr
        }
        ptr = (header >> 8) & 0xFC
        remaining = remaining - 1
    }
    self.panic.fail(code = 0xAC060022)
}

fn enable_mmio_and_bus_master(self) {
    let command_status = self.read_config32(offset = 0x04)
    self.write_config32(offset = 0x04, value = command_status | 0x00000006)
}

fn claim_msi(self) -> MsiCapability {
    self.enable_mmio_and_bus_master()
    return MsiCapability(device = self, capability_offset = self.find_capability(capability_id = 0x05))
}

fn claim_msix(self, table_bar_index: U8) -> MsixCapability {
    self.enable_mmio_and_bus_master()
    let cap = self.find_capability(capability_id = 0x11)
    let table_info = self.read_config32(offset = cap + 4)
    let bir = table_info & 0x7
    if bir != table_bar_index {
        self.panic.fail(code = 0xAC060023)
    }
    let table_offset = table_info & 0xFFFFFFF8
    let table = self.claim_mmio_bar(index = table_bar_index)
    return MsixCapability(device = self, capability_offset = cap, table = table, table_offset = table_offset)
}
```

Then add these capability classes at module top level after `class PciDevice`:

```wrela
class MsiCapability {
    device: PciDevice
    capability_offset: U16

    fn route(self, vector: InterruptVector, target: LocalApic) -> PciInterruptRoute {
        self.device.enable_mmio_and_bus_master()
        let header = self.device.read_config32(offset = self.capability_offset)
        let control = (header >> 16) & 0xFFFF
        let message_address = target.message_address()
        self.device.write_config32(offset = self.capability_offset + 4, value = message_address)
        if (control & 0x80) != 0 {
            self.device.write_config32(offset = self.capability_offset + 8, value = 0)
            self.device.write_config32(offset = self.capability_offset + 12, value = vector.value)
        } else {
            self.device.write_config32(offset = self.capability_offset + 8, value = vector.value)
        }
        self.device.write_config32(offset = self.capability_offset, value = header | 0x00010000)
        return PciInterruptRoute(vector = vector)
    }
}

class MsixCapability {
    device: PciDevice
    capability_offset: U16
    table: MmioRegion
    table_offset: U32

    fn route_entry(self, entry: U16, vector: InterruptVector, target: LocalApic) -> PciInterruptRoute {
        self.device.enable_mmio_and_bus_master()
        let base = self.table_offset + (entry * 16)
        self.table.write32(offset = base + 0, value = target.message_address())
        self.table.write32(offset = base + 4, value = 0)
        self.table.write32(offset = base + 8, value = vector.value)
        self.table.write32(offset = base + 12, value = 0)
        let header = self.device.read_config32(offset = self.capability_offset)
        self.device.write_config32(offset = self.capability_offset, value = header | 0x80000000)
        return PciInterruptRoute(vector = vector)
    }
}
```

`claim_msix(table_bar_index = 1)` must reject a capability whose MSI-X Table BIR does not match the requested BAR index; this prevents silently using the wrong BAR as the MSI-X table.

Use `LocalApic.message_address()` from Task 8. That method is the only allowed source location for the architectural MSI message-address prefix `0xFEE00000`, and it must include the target APIC ID with `0xFEE00000 | (self.apic_id << 12)`.

- [ ] **Step 3: Update drivers to consume `MmioRegion`**

Change `EduMsiPath.mmio_base: VirtualAddress` to `mmio: MmioRegion`, and update reads/writes through `self.mmio.read32/write32`.

Change ivshmem paths from `registers_base: VirtualAddress` to `registers: MmioRegion`.

- [ ] **Step 4: Verify**

Run:

```bash
go test ./compiler/codegen -run TestPciCapabilitiesWalkedFromDiscoveredDevices -v
go test ./compiler/sem -run TestHardwareDiscoverySourceShape -v
git diff --check
```

- [ ] **Step 5: Commit**

```bash
git add wrela/machine/x86_64/pci.wrela wrela/machine/x86_64/edu.wrela wrela/machine/x86_64/ivshmem.wrela compiler/codegen/uefi_source_codegen_test.go
git commit -m "feat: claim pci msi and msix capabilities -Codex Automated"
```

**Acceptance Criteria:** MSI and MSI-X capability offsets are discovered by capability-list walking; EDU and ivshmem drivers receive MMIO authorities rather than virtual address literals.

---

## 10. Phase 6: Semantic Authority Enforcement

**Description:** Make discovered hardware capabilities non-forgeable and reject duplicate resource claims at compile time when source wiring duplicates a claim.

**Acceptance Criteria:**
- User modules cannot directly construct hardware capability classes.
- Trusted `platform.uefi.*`, `platform.acpi.*`, `platform.hardware.*`, `machine.x86_64.interrupts`, `machine.x86_64.pci`, and `machine.x86_64.cpu_state` modules can construct narrowed capabilities.
- Duplicate BAR, MSI, MSI-X, IRQ route, and vector claims produce stable diagnostics.

**Code Example:**

```wrela
// Rejected in user modules:
let fake = MmioRegion(address = 0xFEC00000, length = 4096, panic = BootPanic())
```

### Task 13: Reject Forged Hardware Capability Construction

**Description:** Add semantic restrictions for hardware authority classes outside trusted platform modules and image-phase discovery flows.

**Files:**
- Modify: `compiler/sem/check.go`
- Add: `compiler/sem/hardware_authority_test.go`
- Add: `tests/fixtures/negative/forged_mmio_region.wrela`
- Add: `tests/fixtures/negative/forged_pci_device.wrela`

- [ ] **Step 1: Add failing tests**

Create `compiler/sem/hardware_authority_test.go`:

```go
package sem

import (
    "testing"
    "github.com/ryanwible/wrela3/compiler/parse"
    "github.com/ryanwible/wrela3/compiler/diag"
    "github.com/ryanwible/wrela3/compiler/source"
)

func checkUEFIModulesWithExtraSource(t *testing.T, name string, sourceText string) (*CheckedProgram, []diag.Diagnostic) {
    t.Helper()
    modules := parseUEFIModuleSet(t)
    extra, pds := parse.ParseGraph(source.Graph{
        Files: []*source.File{source.NewFile(source.FileID(9000), name, sourceText)},
    })
    if len(pds) != 0 {
        t.Fatalf("parse extra source: %#v", pds)
    }
    modules = append(modules, extra...)
    index, ds := BuildIndex(modules)
    if len(ds) != 0 {
        return nil, ds
    }
    return Check(index, modules)
}

func TestForgedHardwareAuthorityRejected(t *testing.T) {
    _, ds := checkUEFIModulesWithExtraSource(t, "forged-mmio-test.wrela", `
module examples.bad
use { BootPanic } from platform.hardware.panic
use { MmioRegion } from platform.hardware.bytes
use { DelegatedHardware } from platform.uefi.transition

image BadForgedMmio {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> never {
        let fake = MmioRegion(address = 0xFEC00000, length = 4096, panic = BootPanic())
        while true {}
    }
}`)
    if !hasCode(ds, diag.SEM0049) {
        t.Fatalf("expected SEM0049, got %#v", ds)
    }
}
```

Create `tests/fixtures/negative/forged_mmio_region.wrela`:

```wrela
// expect: SEM0049: MmioRegion must come from hardware discovery authority
module platform.hardware.panic
class BootPanic {
    asm fn fail(self, code: U64) -> never { hlt }
}

module platform.hardware.bytes
use { BootPanic } from platform.hardware.panic
class MmioRegion {
    address: PhysicalAddress
    length: U64
    panic: BootPanic
}

module tests.fixtures.negative.forged_mmio_region

use { BootPanic } from platform.hardware.panic
use { MmioRegion } from platform.hardware.bytes

image BadForgedMmio {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> never {
        let fake = MmioRegion(address = 0xFEC00000, length = 4096, panic = BootPanic())
        while true {}
    }
}
```

Create `tests/fixtures/negative/forged_pci_device.wrela`:

```wrela
// expect: SEM0049: PciDevice must come from hardware discovery authority
module platform.hardware.panic
class BootPanic {
    asm fn fail(self, code: U64) -> never { hlt }
}

module machine.x86_64.pci
use { BootPanic } from platform.hardware.panic

class PcieEcamWindow {
    base: PhysicalAddress
    segment: U16
    start_bus: U8
    end_bus: U8
    panic: BootPanic
}

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
}

class PciDevice {
    window: PcieEcamWindow
    identity: PciDeviceIdentity
    panic: BootPanic
}

class PciFixtureFactory {
    panic: BootPanic

    fn window(self) -> PcieEcamWindow {
        return PcieEcamWindow(base = 0, segment = 0, start_bus = 0, end_bus = 0, panic = self.panic)
    }

    fn identity(self) -> PciDeviceIdentity {
        return PciDeviceIdentity(segment = 0, bus = 0, device = 0, function = 0, vendor_id = 0x1234, device_id = 0x11E8, class_code = 0, subclass = 0, prog_if = 0)
    }
}

module tests.fixtures.negative.forged_pci_device

use { BootPanic } from platform.hardware.panic
use { PciDevice, PciFixtureFactory } from machine.x86_64.pci

image BadForgedPci {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> never {
        let panic = BootPanic()
        let factory = PciFixtureFactory(panic = panic)
        let device = PciDevice(window = factory.window(), identity = factory.identity(), panic = panic)
        while true {}
    }
}
```

- [ ] **Step 2: Implement authority predicate**

In `compiler/sem/check.go`:

```go
func isTrustedHardwareAuthorityModule(moduleName string) bool {
    switch {
    case strings.HasPrefix(moduleName, "platform.uefi."):
        return true
    case strings.HasPrefix(moduleName, "platform.acpi."):
        return true
    case strings.HasPrefix(moduleName, "platform.hardware."):
        return true
    case moduleName == "machine.x86_64.interrupts":
        return true
    case moduleName == "machine.x86_64.pci":
        return true
    case moduleName == "machine.x86_64.cpu_state":
        return true
    }
    return false
}

func isHardwareAuthorityType(typ *Type) bool {
    if typ == nil {
        return false
    }
    q := qualifiedTypeName(typ)
    switch q {
    case "platform.hardware.bytes.BoundedBytes",
        "platform.hardware.bytes.PhysicalBytes",
        "platform.hardware.bytes.MmioRegion",
        "platform.hardware.bytes.IoPortRegion",
        "platform.acpi.root.AcpiRoot",
        "platform.acpi.tables.AcpiTable",
        "platform.acpi.madt.MadtTable",
        "platform.acpi.mcfg.McfgTable",
        "machine.x86_64.interrupts.LocalApic",
        "machine.x86_64.interrupts.IoApicDiscovered",
        "machine.x86_64.interrupts.IoApicSet",
        "machine.x86_64.interrupts.InterruptOverrideSet",
        "machine.x86_64.interrupts.InterruptAuthority",
        "machine.x86_64.interrupts.IoApicRoute",
        "machine.x86_64.pci.PciDevice",
        "machine.x86_64.pci.PciDeviceIdentity",
        "machine.x86_64.pci.PcieEcamWindow",
        "machine.x86_64.pci.PcieEcamWindows",
        "machine.x86_64.pci.PciDeviceSet",
        "machine.x86_64.pci.MsiCapability",
        "machine.x86_64.pci.MsixCapability",
        "machine.x86_64.cpu_state.Vcpu":
        return true
    }
    return false
}
```

In `checkConstructorPermissions`, before the data-type return:

```go
if isHardwareAuthorityType(typ) && !isTrustedHardwareAuthorityModule(moduleName) {
    c.error(expr.SpanV, diag.SEM0049, typ.Name+" must come from hardware discovery authority")
    return
}
```

This allowlist is exact for this milestone. Do not allow all `machine.x86_64.*` modules: drivers such as `machine.x86_64.serial`, `machine.x86_64.edu`, and `machine.x86_64.ivshmem` consume authorities but must not mint them.

- [ ] **Step 3: Verify**

Run:

```bash
go test ./compiler/sem -run TestForgedHardwareAuthorityRejected -v
go test ./compiler -run TestNegativeFixtures -v
git diff --check
```

- [ ] **Step 4: Commit**

```bash
git add compiler/sem/check.go compiler/sem/hardware_authority_test.go tests/fixtures/negative/forged_mmio_region.wrela tests/fixtures/negative/forged_pci_device.wrela
git commit -m "feat: reject forged hardware authorities -Codex Automated"
```

**Acceptance Criteria:** User modules cannot mint ACPI, MMIO, interrupt, PCI, BAR, MSI, or MSI-X authorities by constructor.

### Task 14: Track Duplicate Hardware Claims

**Description:** Extend the semantic graph to detect duplicate claims for BARs, MSI/MSI-X, ISA IRQ routes, and interrupt vectors.

**Files:**
- Modify: `compiler/sem/image_graph.go`
- Modify: `compiler/sem/check.go`
- Add: `compiler/sem/hardware_claim_test.go`
- Add: `tests/fixtures/negative/duplicate_pci_bar_claim.wrela`
- Add: `tests/fixtures/negative/duplicate_interrupt_vector.wrela`

- [ ] **Step 1: Add failing duplicate-claim tests**

```go
func TestDuplicateHardwareClaimsRejected(t *testing.T) {
    _, ds := checkUEFIModulesWithExtraSource(t, "duplicate-bar-test.wrela", duplicateHardwareClaimSource)
    if !hasCode(ds, diag.SEM0050) {
        t.Fatalf("expected SEM0050, got %#v", ds)
    }
}

func TestDuplicateInterruptVectorRejected(t *testing.T) {
    _, ds := checkUEFIModulesWithExtraSource(t, "duplicate-vector-test.wrela", duplicateInterruptVectorSource)
    if !hasCode(ds, diag.SEM0050) {
        t.Fatalf("expected SEM0050, got %#v", ds)
    }
}
```

Use this exact duplicate BAR source in the test:

```go
const duplicateHardwareClaimSource = `
module examples.bad_duplicate_bar
use { BootPanic } from platform.hardware.panic
use { PlatformDiscoveryRoot } from platform.hardware.discovery
use { DelegatedHardware } from platform.uefi.transition

image BadDuplicateBar {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> never {
        let discovery = PlatformDiscoveryRoot(panic = BootPanic()).from_uefi(hardware = hardware)
        let edu = discovery.pci.require_device(vendor_id = 0x1234, device_id = 0x11E8, occurrence = 0)
        let first = edu.claim_mmio_bar(index = 0)
        let second = edu.claim_mmio_bar(index = 0)
        while true {}
    }
}
`
```

Use this exact duplicate interrupt-vector source in the second test. It uses `require_madt().interrupt_authority()` because `DiscoveredHardware.interrupts` is not added until Task 15.

```go
const duplicateInterruptVectorSource = `
module examples.bad_duplicate_vector
use { BootPanic } from platform.hardware.panic
use { PlatformDiscoveryRoot } from platform.hardware.discovery
use { DelegatedHardware } from platform.uefi.transition
use { InterruptVector } from machine.x86_64.interrupts

image BadDuplicateVector {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> never {
        let discovery = PlatformDiscoveryRoot(panic = BootPanic()).from_uefi(hardware = hardware)
        let interrupts = discovery.acpi.require_madt().interrupt_authority()
        let first = interrupts.route_isa_irq(irq = 4, vector = InterruptVector(value = 0x40))
        let second = interrupts.route_isa_irq(irq = 5, vector = InterruptVector(value = 0x40))
        while true {}
    }
}
`
```

Create `tests/fixtures/negative/duplicate_pci_bar_claim.wrela`:

```wrela
// expect: SEM0050: duplicate hardware claim pci_bar:vendor=0x1234/device=0x11e8/occurrence=0.0
module platform.hardware.panic
class BootPanic { asm fn fail(self, code: U64) -> never { hlt } }

module platform.hardware.bytes
use { BootPanic } from platform.hardware.panic
class MmioRegion { address: PhysicalAddress; length: U64; panic: BootPanic }

module machine.x86_64.pci
use { MmioRegion } from platform.hardware.bytes
use { BootPanic } from platform.hardware.panic

class PciDevice {
    panic: BootPanic
    fn claim_mmio_bar(self, index: U8) -> MmioRegion {
        return MmioRegion(address = 0, length = 0, panic = self.panic)
    }
}

class PciDeviceSet {
    panic: BootPanic
    fn require_device(self, vendor_id: U16, device_id: U16, occurrence: U64) -> PciDevice {
        return PciDevice(panic = self.panic)
    }
}

class PciFixtureFactory {
    panic: BootPanic
    fn devices(self) -> PciDeviceSet {
        return PciDeviceSet(panic = self.panic)
    }
}

module tests.fixtures.negative.duplicate_pci_bar_claim
use { BootPanic } from platform.hardware.panic
use { PciFixtureFactory } from machine.x86_64.pci

image BadDuplicateBar {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> never {
        let devices = PciFixtureFactory(panic = BootPanic()).devices()
        let edu = devices.require_device(vendor_id = 0x1234, device_id = 0x11e8, occurrence = 0)
        let first = edu.claim_mmio_bar(index = 0)
        let second = edu.claim_mmio_bar(index = 0)
        while true {}
    }
}
```

Create `tests/fixtures/negative/duplicate_interrupt_vector.wrela`:

```wrela
// expect: SEM0050: duplicate hardware claim interrupt_vector:0x40
module platform.hardware.panic
class BootPanic { asm fn fail(self, code: U64) -> never { hlt } }

module machine.x86_64.interrupts
use { BootPanic } from platform.hardware.panic

data InterruptVector { value: U32 }
class IoApicRoute { vector: InterruptVector }

class InterruptAuthority {
    panic: BootPanic
    fn route_isa_irq(self, irq: U8, vector: InterruptVector) -> IoApicRoute {
        return IoApicRoute(vector = vector)
    }
}

class InterruptFixtureFactory {
    panic: BootPanic
    fn authority(self) -> InterruptAuthority {
        return InterruptAuthority(panic = self.panic)
    }
}

module tests.fixtures.negative.duplicate_interrupt_vector

use { BootPanic } from platform.hardware.panic
use { InterruptFixtureFactory, InterruptVector } from machine.x86_64.interrupts

image DuplicateInterruptVector {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> never {
        let interrupts = InterruptFixtureFactory(panic = BootPanic()).authority()
        let first = interrupts.route_isa_irq(irq = 4, vector = InterruptVector(value = 0x40))
        let second = interrupts.route_isa_irq(irq = 5, vector = InterruptVector(value = 0x40))
        while true {}
    }
}
```

- [ ] **Step 2: Add graph nodes**

In `compiler/sem/image_graph.go`:

```go
type HardwareClaimNode struct {
    Kind string
    Key string
    Span source.Span
}

type ImageGraph struct {
    // existing fields...
    HardwareClaims []HardwareClaimNode
}
```

- [ ] **Step 3: Record claims in `typeCallExpr`**

Add these helpers to `compiler/sem/check.go` near `namedArgExpr`. The key is deliberately based on the static `require_device(vendor_id, device_id, occurrence)` origin, not on BDF, because BDF is discovered at boot time.

```go
func literalArgKey(expr *ast.CallExpr, name string) string {
    for _, arg := range expr.Args {
        if arg.Name != name {
            continue
        }
        if lit, ok := arg.Value.(*ast.IntLiteral); ok {
            return strings.ToLower(lit.Value)
        }
        return "<nonliteral>"
    }
    return "<missing>"
}

func pciDeviceKeyFromRequireDevice(call *ast.CallExpr) string {
    vendor := literalArgKey(call, "vendor_id")
    device := literalArgKey(call, "device_id")
    occurrence := literalArgKey(call, "occurrence")
    if vendor == "<missing>" || device == "<missing>" || occurrence == "<missing>" {
        return ""
    }
    if vendor == "<nonliteral>" || device == "<nonliteral>" || occurrence == "<nonliteral>" {
        return ""
    }
    return "vendor=" + vendor + "/device=" + device + "/occurrence=" + occurrence
}

func pciOriginKey(receiver ast.Expr, scope *Scope) (string, bool) {
    switch r := receiver.(type) {
    case *ast.NameExpr:
        if origin, ok := scope.LookupOrigin(r.Name); ok && origin.PciDeviceKey != "" {
            return origin.PciDeviceKey, true
        }
    case *ast.CallExpr:
        if r.Method == "require_device" {
            if key := pciDeviceKeyFromRequireDevice(r); key != "" {
                return key, true
            }
        }
    }
    return "", false
}

func interruptVectorArgKey(expr *ast.CallExpr) string {
    arg := namedArgExpr(expr.Args, "vector")
    cons, ok := arg.(*ast.ConstructorExpr)
    if !ok || cons.Type != "InterruptVector" {
        return "<nonliteral>"
    }
    for _, named := range cons.Args {
        if named.Name == "value" {
            if lit, ok := named.Value.(*ast.IntLiteral); ok {
                return strings.ToLower(lit.Value)
            }
        }
    }
    return "<missing>"
}
```

Extend the existing `localOrigin` struct with a PCI device key:

```go
type localOrigin struct {
    // existing fields...
    PciDeviceKey string
}
```

`originForCall` already exists in `compiler/sem/check.go`. Add this case in that function before the final `return origin`:

```go
case receiverType.Module == "machine.x86_64.pci" && receiverType.Name == "PciDeviceSet" && expr.Method == "require_device":
    origin.PciDeviceKey = pciDeviceKeyFromRequireDevice(expr)
```

No additional alias code is needed: `originForExprValue` already preserves origins for `let alias = edu` through `Scope.LookupOrigin`.

Then, when a call method matches:

```go
case "route_isa_irq":
    c.graph.HardwareClaims = append(c.graph.HardwareClaims, HardwareClaimNode{Kind: "isa_irq", Key: literalArgKey(expr, "irq"), Span: expr.SpanV})
    vectorKey := interruptVectorArgKey(expr)
    if strings.HasPrefix(vectorKey, "<") {
        c.error(expr.SpanV, diag.SEM0053, "interrupt vectors in hardware claims must be source literals")
        return
    }
    c.graph.HardwareClaims = append(c.graph.HardwareClaims, HardwareClaimNode{Kind: "interrupt_vector", Key: vectorKey, Span: expr.SpanV})
case "claim_mmio_bar", "claim_io_bar":
    key, ok := pciOriginKey(expr.Receiver, scope)
    if !ok {
        c.error(expr.SpanV, diag.SEM0054, "PCI claims must be made from discovered PciDevice values")
        return
    }
    c.graph.HardwareClaims = append(c.graph.HardwareClaims, HardwareClaimNode{Kind: "pci_bar", Key: key+"."+literalArgKey(expr, "index"), Span: expr.SpanV})
case "claim_msi":
    key, ok := pciOriginKey(expr.Receiver, scope)
    if !ok {
        c.error(expr.SpanV, diag.SEM0054, "PCI claims must be made from discovered PciDevice values")
        return
    }
    c.graph.HardwareClaims = append(c.graph.HardwareClaims, HardwareClaimNode{Kind: "pci_msi", Key: key, Span: expr.SpanV})
case "claim_msix":
    key, ok := pciOriginKey(expr.Receiver, scope)
    if !ok {
        c.error(expr.SpanV, diag.SEM0054, "PCI claims must be made from discovered PciDevice values")
        return
    }
    c.graph.HardwareClaims = append(c.graph.HardwareClaims, HardwareClaimNode{Kind: "pci_msix", Key: key, Span: expr.SpanV})
case "route", "route_entry":
    receiverType := c.exprStaticType(moduleName, expr.Receiver, scope)
    if qualifiedTypeName(receiverType) == "machine.x86_64.pci.MsiCapability" || qualifiedTypeName(receiverType) == "machine.x86_64.pci.MsixCapability" {
        vectorKey := interruptVectorArgKey(expr)
        if strings.HasPrefix(vectorKey, "<") {
            c.error(expr.SpanV, diag.SEM0053, "interrupt vectors in hardware claims must be source literals")
            return
        }
        c.graph.HardwareClaims = append(c.graph.HardwareClaims, HardwareClaimNode{Kind: "interrupt_vector", Key: vectorKey, Span: expr.SpanV})
    }
```

If `pciOriginKey` cannot resolve to a `require_device` origin, emit SEM0054: `"PCI claims must be made from discovered PciDevice values"`.

- [ ] **Step 4: Finalize duplicates**

Add `checkHardwareClaims()` after existing graph finalizers:

```go
func (c *checker) checkHardwareClaims() {
    seen := map[string]source.Span{}
    for _, claim := range c.graph.HardwareClaims {
        key := claim.Kind + ":" + claim.Key
        if prev, ok := seen[key]; ok {
            _ = prev
            c.error(claim.Span, diag.SEM0050, "duplicate hardware claim "+key)
            continue
        }
        seen[key] = claim.Span
    }
}
```

- [ ] **Step 5: Verify**

Run:

```bash
go test ./compiler/sem -run TestDuplicateHardwareClaimsRejected -v
go test ./compiler -run TestNegativeFixtures -v
git diff --check
```

- [ ] **Step 6: Commit**

```bash
git add compiler/sem/image_graph.go compiler/sem/check.go compiler/sem/hardware_claim_test.go tests/fixtures/negative/duplicate_pci_bar_claim.wrela tests/fixtures/negative/duplicate_interrupt_vector.wrela
git commit -m "feat: reject duplicate hardware claims -Codex Automated"
```

**Acceptance Criteria:** Duplicate BAR, MSI, MSI-X, ISA IRQ route, and interrupt vector claims are semantic errors with SEM0050.

---

## 11. Phase 7: HardwarePlan Handoff And Example Migration

**Description:** Discovery runs in `delegated_hardware`; owned code receives only narrowed authorities needed by examples and fixtures.

**Acceptance Criteria:**
- `DelegatedHardware.exit_to_owned_hardware` accepts `hardware_plan`.
- `OwnedHardware` exposes `hardware_plan`.
- Examples and e2e fixtures no longer hardcode memory, APIC bases, BDF slots, or PCI BAR addresses.

**Code Example:**

```wrela
return hardware.exit_to_owned_hardware(
    memory_plan = memory_plan,
    cpu_plan = cpu_plan,
    hardware_plan = hardware_plan
)
```

### Task 15: Add HardwarePlan And OwnedHardware Handoff

**Description:** Add small discovered hardware plan records and thread them through the UEFI transition into owned hardware.

**Files:**
- Modify: `wrela/platform/uefi/transition.wrela`
- Modify: `wrela/platform/hardware/discovery.wrela`
- Modify: `wrela/machine/x86_64/cpu_state.wrela`
- Modify: `compiler/sem/hardware_discovery_test.go`
- Modify: `compiler/sem/uefi_source_shape_test.go`

- [ ] **Step 1: Add failing shape checks**

```go
plan := moduleType(t, index, "machine.x86_64.cpu_state", "HardwarePlan")
if fieldTypeName(t, plan, "cpus") != "CpuTopology" {
    t.Fatalf("HardwarePlan.cpus must be CpuTopology")
}
owned := moduleType(t, index, "machine.x86_64.cpu_state", "OwnedHardware")
if fieldTypeName(t, owned, "hardware_plan") != "HardwarePlan" {
    t.Fatalf("OwnedHardware.hardware_plan must be HardwarePlan")
}
```

- [ ] **Step 2: Add plan source to cpu_state**

Add these declarations to `wrela/machine/x86_64/cpu_state.wrela`:

```wrela
use { IoApicRoute, LocalApic } from machine.x86_64.interrupts
use { BootPanic } from platform.hardware.panic
use { MmioRegion } from platform.hardware.bytes
use { MsiCapability, MsixCapability, PciDevice, PciDeviceIdentity, PcieEcamWindow } from machine.x86_64.pci

data InterruptRoutingPlan {
    local_apic: LocalApic
    serial_irq4: IoApicRoute
}

data ClaimedPciPlan {
    edu_bar0: MmioRegion
    edu_msi: MsiCapability
    ivshmem_rx_bar0: MmioRegion
    ivshmem_rx_msix: MsixCapability
    ivshmem_tx_bar0: MmioRegion
}

class ClaimedPciPlanBuilder {
    panic: BootPanic

    fn empty(self) -> ClaimedPciPlan {
        let empty_mmio = MmioRegion(address = 0, length = 0, panic = self.panic)
        let empty_window = PcieEcamWindow(base = 0, segment = 0, start_bus = 0, end_bus = 0, panic = self.panic)
        let empty_identity = PciDeviceIdentity(segment = 0, bus = 0, device = 0, function = 0, vendor_id = 0xFFFF, device_id = 0xFFFF, class_code = 0, subclass = 0, prog_if = 0)
        let empty_device = PciDevice(window = empty_window, identity = empty_identity, panic = self.panic)
        return ClaimedPciPlan(
            edu_bar0 = empty_mmio,
            edu_msi = MsiCapability(device = empty_device, capability_offset = 0),
            ivshmem_rx_bar0 = empty_mmio,
            ivshmem_rx_msix = MsixCapability(device = empty_device, capability_offset = 0, table = empty_mmio, table_offset = 0),
            ivshmem_tx_bar0 = empty_mmio
        )
    }
}

data HardwarePlan {
    cpus: CpuTopology
    interrupts: InterruptRoutingPlan
    pci: ClaimedPciPlan
}
```

- [ ] **Step 3: Expand PlatformDiscoveryRoot**

Replace the shell in `wrela/platform/hardware/discovery.wrela` with these imports and classes:

```wrela
module platform.hardware.discovery

use { AcpiLocator, AcpiRoot } from platform.acpi.root
use { BootPanic } from platform.hardware.panic
use { DelegatedHardware } from platform.uefi.transition
use { UefiMemoryMap } from platform.uefi.types
use { InterruptAuthority } from machine.x86_64.interrupts
use { PciDeviceSet } from machine.x86_64.pci

class DiscoveredHardware {
    memory: UefiMemoryMap
    acpi: AcpiRoot
    interrupts: InterruptAuthority
    pci: PciDeviceSet
    panic: BootPanic
}
```

Use this exact `PlatformDiscoveryRoot` implementation:

```wrela
class PlatformDiscoveryRoot {
    panic: BootPanic

    fn from_uefi(self, hardware: DelegatedHardware) -> DiscoveredHardware {
        let tables = hardware.uefi_configuration_tables()
        let memory = hardware.memory_map()
        let acpi = AcpiLocator(panic = self.panic).find(tables = tables)
        let madt = acpi.require_madt()
        let mcfg = acpi.require_mcfg()
        return DiscoveredHardware(
            memory = memory,
            acpi = acpi,
            interrupts = madt.interrupt_authority(),
            pci = mcfg.ecam_windows().enumerate(),
            panic = self.panic
        )
    }
}
```

- [ ] **Step 4: Update transition**

Change `exit_to_owned_hardware` signature:

```wrela
fn exit_to_owned_hardware(
    self,
    memory_plan: MemoryPlan,
    cpu_plan: CpuPlan,
    hardware_plan: HardwarePlan
) -> OwnedHardware
```

Construct:

```wrela
return OwnedHardware(
    memory = memory_plan.owned_memory,
    io_ports = memory_plan.io_ports,
    executors = ExecutorRegistry(next_id = 0),
    hardware_plan = hardware_plan,
    vcpu0 = hardware_plan.cpus.vcpu0(local_apic_base = hardware_plan.interrupts.local_apic.base),
    vcpu1 = hardware_plan.cpus.vcpu1(local_apic_base = hardware_plan.interrupts.local_apic.base)
)
```

- [ ] **Step 5: Update the UEFI source-shape harness**

The existing harness in `compiler/sem/uefi_source_shape_test.go` calls `exit_to_owned_hardware` with only `memory_plan` and `cpu_plan`. In the embedded `uefi-test-harness.wrela` source string, replace the cpu-state and interrupt imports with:

```wrela
use { DelegatedHardware } from platform.uefi.transition
use { BootPanic } from platform.hardware.panic
use { PlatformDiscoveryRoot } from platform.hardware.discovery
use { OwnedHardware, OwnedMemory, IoPortAuthority } from machine.x86_64.cpu_state
use { MemoryPlan, CpuPlan, HardwarePlan, InterruptRoutingPlan, ClaimedPciPlanBuilder } from machine.x86_64.cpu_state
use { InterruptVector } from machine.x86_64.interrupts
use { MutableBytes, Bytes } from machine.x86_64.executor_memory
```

Then replace the `return hardware.exit_to_owned_hardware(...)` call-site fixture with this exact hardware plan construction:

```wrela
let panic = BootPanic()
let discovery = PlatformDiscoveryRoot(panic = panic).from_uefi(hardware = hardware)
let interrupts = discovery.interrupts
let hardware_plan = HardwarePlan(
    cpus = discovery.acpi.require_madt().enabled_cpus().require_count(count = 2),
    interrupts = InterruptRoutingPlan(
        local_apic = interrupts.local_apic,
        serial_irq4 = interrupts.route_isa_irq(irq = 4, vector = InterruptVector(value = 0x40))
    ),
    pci = ClaimedPciPlanBuilder(panic = panic).empty()
)
return hardware.exit_to_owned_hardware(
    memory_plan = memory_plan,
    cpu_plan = cpu_plan,
    hardware_plan = hardware_plan
)
```

- [ ] **Step 6: Verify**

Run:

```bash
go test ./compiler/sem -run TestHardwareDiscoverySourceShape -v
go test ./compiler -run TestHello -v
git diff --check
```

- [ ] **Step 7: Commit**

```bash
git add wrela/platform/uefi/transition.wrela wrela/platform/hardware/discovery.wrela wrela/machine/x86_64/cpu_state.wrela compiler/sem/hardware_discovery_test.go compiler/sem/uefi_source_shape_test.go
git commit -m "feat: hand discovered hardware plan to owned phase -Codex Automated"
```

**Acceptance Criteria:** Owned phase receives CPU topology, interrupt plan, and PCI claims through `OwnedHardware.hardware_plan`.

### Task 16: Migrate Hello And Hello ivshmem To Discovery

**Description:** Replace hardcoded hello q35 facts with `PlatformDiscoveryRoot` and `HardwarePlan`.

**Files:**
- Modify: `examples/hello/main.wrela`
- Modify: `examples/hello/program.wrela`
- Modify: `tests/e2e/fixtures/hello_ivshmem/main.wrela`
- Modify: `tests/e2e/fixtures/hello_ivshmem/program.wrela`
- Modify: `compiler/integration_test.go`

- [ ] **Step 1: Add anti-literal integration assertions**

In `compiler/integration_test.go`, add:

```go
func TestHelloUsesHardwareDiscoverySource(t *testing.T) {
    raw, err := os.ReadFile("examples/hello/main.wrela")
    if err != nil {
        t.Fatal(err)
    }
    text := string(raw)
    for _, forbidden := range []string{
        "Q35PciInterruptConfigurator",
        "PciConfigPorts",
        "0xFEE00000",
        "0xFEC00000",
        "MutableBytes(address = 0x200000",
    } {
        if strings.Contains(text, forbidden) {
            t.Fatalf("hello source still contains %q", forbidden)
        }
    }
    for _, required := range []string{"PlatformDiscoveryRoot", "require_usable_region", "require_device", "claim_msi"} {
        if !strings.Contains(text, required) {
            t.Fatalf("hello source missing %q", required)
        }
    }
}
```

- [ ] **Step 2: Rewrite hello delegated phase**

Use this delegated phase in `examples/hello/main.wrela`:

```wrela
phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
    let root = PlatformDiscoveryRoot(panic = BootPanic())
    let discovery = root.from_uefi(hardware = hardware)
    let memory_region = discovery.memory.require_usable_region(
        min_base = 0x200000,
        length = 0x600000,
        align = 4096
    )
    let cpus = discovery.acpi.require_madt().enabled_cpus().require_count(count = 2)
    let interrupts = discovery.interrupts
    let pci = discovery.pci
    let edu = pci.require_device(vendor_id = 0x1234, device_id = 0x11E8, occurrence = 0)
    let ivshmem_rx = pci.require_device(vendor_id = 0x1AF4, device_id = 0x1110, occurrence = 0)
    let ivshmem_tx = pci.require_device(vendor_id = 0x1AF4, device_id = 0x1110, occurrence = 1)
    let hardware_plan = HardwarePlan(
        cpus = cpus,
        interrupts = InterruptRoutingPlan(
            local_apic = interrupts.local_apic,
            serial_irq4 = interrupts.route_isa_irq(irq = 4, vector = InterruptVector(value = 0x40))
        ),
        pci = ClaimedPciPlan(
            edu_bar0 = edu.claim_mmio_bar(index = 0),
            edu_msi = edu.claim_msi(),
            ivshmem_rx_bar0 = ivshmem_rx.claim_mmio_bar(index = 0),
            ivshmem_rx_msix = ivshmem_rx.claim_msix(table_bar_index = 1),
            ivshmem_tx_bar0 = ivshmem_tx.claim_mmio_bar(index = 0)
        )
    )
    let memory_plan = MemoryPlan(
        owned_memory = OwnedMemory(arena = memory_region),
        executor_arena = memory_region,
        io_ports = IoPortAuthority()
    )
    let cpu_plan = CpuPlan(
        owned_stack_top = memory_region.address + memory_region.length,
        gdt_descriptor = Bytes(address = 0, length = 0),
        idt_descriptor = Bytes(address = 0, length = 0),
        cr3 = memory_region.address
    )
    return hardware.exit_to_owned_hardware(memory_plan = memory_plan, cpu_plan = cpu_plan, hardware_plan = hardware_plan)
}
```

- [ ] **Step 3: Rewrite hello owned phase**

Replace serial and EDU setup:

```wrela
let serial_path = serial_driver.create_console_path(
    identity = PathIdentity(label = "hello.console"),
    route = hardware.hardware_plan.interrupts.serial_irq4,
    rx = serial_rx_topic.publisher()
)
serial_path.write(hello_memory.bytes(value = "discovery: memory=selected lapic=selected ioapic=selected pci=selected\n"))
let edu_path = EduMsiPath(
    identity = PathIdentity(label = "hello.edu"),
    mmio = hardware.hardware_plan.pci.edu_bar0,
    irq = edu_interrupt_topic.publisher()
)
let hello = HelloWorld(
    slot = hello_slot,
    loop = EventSleepPolicy(),
    memory = hello_memory,
    interrupts = ApicInterruptController(local_apic = hardware.hardware_plan.interrupts.local_apic),
    edu_msi = hardware.hardware_plan.pci.edu_msi,
    serial_path = serial_path,
    serial_rx = serial_rx,
    edu_path = edu_path,
    edu_interrupts = edu_interrupts
)
```

In `HelloWorld.run`, replace `self.pci_interrupts.configure_edu_msi_vector41()` with:

```wrela
self.edu_msi.route(vector = InterruptVector(value = 0x41), target = self.interrupts.local_apic)
```

- [ ] **Step 4: Rewrite hello ivshmem fixture**

Use `hardware.hardware_plan.pci.ivshmem_rx_bar0`, `ivshmem_rx_msix`, and `ivshmem_tx_bar0`. Program MSI-X in the executor:

```wrela
self.ivshmem_msix.route_entry(entry = 0, vector = InterruptVector(value = 0x42), target = self.interrupts.local_apic)
```

Immediately after the fixture creates its `serial_path`, write the same discovery marker so Task 20 has a concrete e2e signal:

```wrela
serial_path.write(hello_memory.bytes(value = "discovery: memory=selected lapic=selected ioapic=selected pci=selected\n"))
```

- [ ] **Step 5: Verify**

Run:

```bash
go test ./compiler -run TestHelloUsesHardwareDiscoverySource -v
go test ./compiler -run TestBuildHello -v
git diff --check
```

- [ ] **Step 6: Commit**

```bash
git add examples/hello/main.wrela examples/hello/program.wrela tests/e2e/fixtures/hello_ivshmem/main.wrela tests/e2e/fixtures/hello_ivshmem/program.wrela compiler/integration_test.go
git commit -m "feat: migrate hello images to hardware discovery -Codex Automated"
```

**Acceptance Criteria:** Hello and hello_ivshmem build from discovered memory, APIC, and PCI authorities with no q35 platform literals.

### Task 17: Migrate Multi-vCPU, Arena, And Cache Fixtures

**Description:** Remove hardcoded memory and APIC assumptions from remaining booted examples and fixtures.

**Files:**
- Modify: `examples/multi_vcpu_topics/main.wrela`
- Modify: `tests/e2e/fixtures/arena_memory/main.wrela`
- Modify: `tests/e2e/fixtures/cache_memory/main.wrela`
- Modify: `tests/e2e/hello_qemu_test.go`

- [ ] **Step 1: Add anti-literal e2e source test**

Add to `tests/e2e/hello_qemu_test.go`:

```go
func TestE2EFixturesUseHardwareDiscoverySource(t *testing.T) {
    paths := []string{
        "examples/multi_vcpu_topics/main.wrela",
        "tests/e2e/fixtures/arena_memory/main.wrela",
        "tests/e2e/fixtures/cache_memory/main.wrela",
    }
    for _, path := range paths {
        raw, err := os.ReadFile(path)
        if err != nil {
            t.Fatal(err)
        }
        text := string(raw)
        for _, forbidden := range []string{"0xFEE00000", "0xFEC00000", "MutableBytes(address = 0x200000"} {
            if strings.Contains(text, forbidden) {
                t.Fatalf("%s still contains %q", path, forbidden)
            }
        }
        if !strings.Contains(text, "PlatformDiscoveryRoot") {
            t.Fatalf("%s must use PlatformDiscoveryRoot", path)
        }
    }
}
```

- [ ] **Step 2: Rewrite each delegated phase**

Use this discovery flow in each delegated phase. Replace only the `length` value with the table below.

```text
multi_vcpu_topics: 0x800000
arena_memory: 0x400000
cache_memory: 0x400000
```

Use this per-fixture PCI policy. Do not infer it from imports.

```text
examples/multi_vcpu_topics/main.wrela
  Needs PCI: no
  PCI plan: ClaimedPciPlanBuilder(panic = discovery.panic).empty()

tests/e2e/fixtures/arena_memory/main.wrela
  Needs PCI: no
  PCI plan: ClaimedPciPlanBuilder(panic = discovery.panic).empty()

tests/e2e/fixtures/cache_memory/main.wrela
  Needs PCI: no
  PCI plan: ClaimedPciPlanBuilder(panic = discovery.panic).empty()
```

Every fixture still builds a `HardwarePlan` with CPU topology and serial route:

```wrela
let root = PlatformDiscoveryRoot(panic = BootPanic())
let discovery = root.from_uefi(hardware = hardware)
let memory_region = discovery.memory.require_usable_region(
    min_base = 0x200000,
    length = 0x400000,
    align = 4096
)
let hardware_plan = HardwarePlan(
    cpus = discovery.acpi.require_madt().enabled_cpus().require_count(count = 2),
    interrupts = InterruptRoutingPlan(
        local_apic = discovery.interrupts.local_apic,
        serial_irq4 = discovery.interrupts.route_isa_irq(irq = 4, vector = InterruptVector(value = 0x40))
    ),
    pci = ClaimedPciPlanBuilder(panic = discovery.panic).empty()
)
let memory_plan = MemoryPlan(
    owned_memory = OwnedMemory(arena = memory_region),
    executor_arena = memory_region,
    io_ports = IoPortAuthority()
)
let cpu_plan = CpuPlan(
    owned_stack_top = memory_region.address + memory_region.length,
    gdt_descriptor = Bytes(address = 0, length = 0),
    idt_descriptor = Bytes(address = 0, length = 0),
    cr3 = memory_region.address
)
return hardware.exit_to_owned_hardware(memory_plan = memory_plan, cpu_plan = cpu_plan, hardware_plan = hardware_plan)
```

- [ ] **Step 3: Rewrite owned APIC construction**

Use:

```wrela
interrupts = ApicInterruptController(
    local_apic = hardware.hardware_plan.interrupts.local_apic
)
```

Pass `hardware.hardware_plan.interrupts.serial_irq4` to every `create_console_path` call.

Emit the discovery marker after the executor memory used for the write exists:

```wrela
// examples/multi_vcpu_topics/main.wrela, after producer_memory is claimed:
serial_path.write(producer_memory.bytes(value = "discovery: memory=selected lapic=selected ioapic=selected pci=selected\n"))

// tests/e2e/fixtures/arena_memory/main.wrela, after memory is claimed:
serial_path.write(memory.bytes(value = "discovery: memory=selected lapic=selected ioapic=selected pci=selected\n"))

// tests/e2e/fixtures/cache_memory/main.wrela, after executor_memory is claimed:
serial_path.write(executor_memory.bytes(value = "discovery: memory=selected lapic=selected ioapic=selected pci=selected\n"))
```

- [ ] **Step 4: Verify**

Run:

```bash
go test ./tests/e2e -run TestE2EFixturesUseHardwareDiscoverySource -v
go test ./compiler -run TestBuild -v
git diff --check
```

- [ ] **Step 5: Commit**

```bash
git add examples/multi_vcpu_topics/main.wrela tests/e2e/fixtures/arena_memory/main.wrela tests/e2e/fixtures/cache_memory/main.wrela tests/e2e/hello_qemu_test.go
git commit -m "feat: migrate remaining fixtures to hardware discovery -Codex Automated"
```

**Acceptance Criteria:** Multi-vCPU startup uses MADT APIC IDs; arena/cache fixtures use UEFI memory map selection; no booted fixture constructs literal `MutableBytes` arenas.

---

## 12. Phase 8: Inspection And Legacy Removal

**Description:** Add an internal discovery report and remove stale q35 helper surfaces so future code cannot accidentally use them.

**Acceptance Criteria:**
- Tests can inspect selected memory, APIC IDs, IOAPIC base, overrides, PCI devices, BARs, and interrupt vectors.
- Legacy q35 helper types and codegen constants are gone or confined to QEMU harness tests.

**Code Example:**

```wrela
let report = discovery.report()
serial.write(memory.bytes(value = "discovery ok\n"))
```

### Task 18: Add Discovery Report Source And Tests

**Description:** Provide a small source-level report value for tests and developer inspection.

**Files:**
- Modify: `wrela/platform/hardware/discovery.wrela`
- Modify: `examples/hello/main.wrela`
- Modify: `compiler/sem/hardware_discovery_test.go`
- Add: `compiler/integration_hardware_discovery_test.go`

- [ ] **Step 1: Add report shape**

In `platform.hardware.discovery`, add these imports:

```wrela
use { HardwarePlan } from machine.x86_64.cpu_state
use { MutableBytes } from machine.x86_64.executor_memory
```

Then add:

```wrela
data DiscoveryReport {
    memory_base: PhysicalAddress
    memory_length: U64
    bootstrap_apic_id: U32
    secondary_apic_id: U32
    local_apic_base: PhysicalAddress
    io_apic_base: PhysicalAddress
    serial_gsi: U32
    pci_device_count: U64
    edu_bar0: PhysicalAddress
    ivshmem_rx_bar0: PhysicalAddress
}
```

Add method:

```wrela
fn report(self, memory: MutableBytes, hardware_plan: HardwarePlan) -> DiscoveryReport {
    return DiscoveryReport(
        memory_base = memory.address,
        memory_length = memory.length,
        bootstrap_apic_id = hardware_plan.cpus.bootstrap.apic_id,
        secondary_apic_id = hardware_plan.cpus.secondary.apic_id,
        local_apic_base = hardware_plan.interrupts.local_apic.base,
        io_apic_base = hardware_plan.interrupts.serial_irq4.io_apic.address,
        serial_gsi = hardware_plan.interrupts.serial_irq4.gsi,
        pci_device_count = self.pci.count,
        edu_bar0 = hardware_plan.pci.edu_bar0.address,
        ivshmem_rx_bar0 = hardware_plan.pci.ivshmem_rx_bar0.address
    )
}
```

- [ ] **Step 2: Use the report in hello delegated hardware**

In `examples/hello/main.wrela`, after `hardware_plan` is constructed and before `memory_plan`, add:

```wrela
let report = discovery.report(memory = memory_region, hardware_plan = hardware_plan)
if report.memory_length < 0x600000 {
    discovery.panic.fail(code = 0xAC070001)
}
if report.pci_device_count == 0 {
    discovery.panic.fail(code = 0xAC070002)
}
```

This keeps the e2e serial marker compact while still forcing the report fields to be built and consumed by source code.

- [ ] **Step 3: Add report shape and integration source scan**

In `compiler/sem/hardware_discovery_test.go`, extend `TestHardwareDiscoverySourceShape`:

```go
report := moduleType(t, index, "platform.hardware.discovery", "DiscoveryReport")
for _, field := range []string{
    "memory_base",
    "memory_length",
    "bootstrap_apic_id",
    "secondary_apic_id",
    "local_apic_base",
    "io_apic_base",
    "serial_gsi",
    "pci_device_count",
    "edu_bar0",
    "ivshmem_rx_bar0",
} {
    _ = fieldTypeName(t, report, field)
}
assertMethodExists(t, moduleType(t, index, "platform.hardware.discovery", "DiscoveredHardware"), "report")
```

Create `compiler/integration_hardware_discovery_test.go` and assert the hello build has no q35 helper names:

```go
func TestDiscoveryReportShapeLowers(t *testing.T) {
    raw, err := os.ReadFile("examples/hello/main.wrela")
    if err != nil {
        t.Fatal(err)
    }
    if !strings.Contains(string(raw), "discovery.report(memory = memory_region, hardware_plan = hardware_plan)") {
        t.Fatalf("hello must exercise DiscoveryReport fields")
    }
    result, err := compiler.Build(compiler.BuildOptions{
        Mode: compiler.ModeDev,
        RootPath: "examples/hello/main.wrela",
        OutputPath: filepath.Join(t.TempDir(), "hello.efi"),
        RepoRoot: ".",
    })
    if err != nil {
        t.Fatalf("build hello: %v", err)
    }
    if strings.Contains(fmt.Sprintf("%#v", result), "Q35PciInterruptConfigurator") {
        t.Fatalf("legacy q35 configurator leaked into build result")
    }
}
```

- [ ] **Step 4: Verify**

Run:

```bash
go test ./compiler -run TestDiscoveryReportShapeLowers -v
go test ./compiler/sem -run TestHardwareDiscoverySourceShape -v
git diff --check
```

- [ ] **Step 5: Commit**

```bash
git add wrela/platform/hardware/discovery.wrela examples/hello/main.wrela compiler/sem/hardware_discovery_test.go compiler/integration_hardware_discovery_test.go
git commit -m "feat: expose hardware discovery report -Codex Automated"
```

**Acceptance Criteria:** A test-visible `DiscoveryReport` shape exists, hello source calls it, and hello source consumes report fields for memory length and PCI device-count validation.

### Task 19: Remove Legacy q35 Hardware Assumptions

**Description:** Delete or rewrite stale q35 helper types and tests that no longer represent source-facing hardware policy.

**Files:**
- Modify: `wrela/machine/x86_64/pci.wrela`
- Modify: `compiler/codegen/uefi_source_codegen_test.go`
- Modify: `compiler/integration_test.go`
- Modify: `docs/production-deferred-work.md`

- [ ] **Step 1: Add repository scan test**

Add:

```go
func TestNoLegacyQ35DiscoveryAssumptions(t *testing.T) {
    paths := []string{"examples", "tests/e2e/fixtures", "wrela", "compiler/codegen"}
    forbidden := []string{"Q35PciInterruptConfigurator", "PciConfigPorts", "0xFEC00000", "MutableBytes(address = 0x200000"}
    for _, root := range paths {
        filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
            if err != nil || d.IsDir() {
                return err
            }
            raw, readErr := os.ReadFile(path)
            if readErr != nil {
                return readErr
            }
            text := string(raw)
            for _, needle := range forbidden {
                if strings.Contains(text, needle) {
                    t.Fatalf("%s contains legacy discovery assumption %q", path, needle)
                }
            }
            return nil
        })
    }
}
```

- [ ] **Step 2: Remove legacy tests**

Delete expectations for `_wrela_method_machine_x86_64_pci_Q35PciInterruptConfigurator_*` and replace them with `PciDevice.claim_msi`, `PciDevice.claim_msix`, and `PcieEcamWindows.enumerate` checks.

- [ ] **Step 3: Update deferred work**

Append:

```markdown
## Hardware discovery
- Selected direction: x86_64 UEFI images discover ACPI, MADT, MCFG, APIC, IOAPIC, PCIe ECAM, BAR, MSI, and MSI-X facts through Wrela source-visible platform authorities.
- q35 remains the main verification target, but q35 facts are discovered through ACPI/PCIe rather than hardcoded in image source or compiler codegen.
- Deferred: non-ACPI platforms, IOMMU/DMA remapping, production timer runtime, storage, network, framebuffer, and non-x86_64 discovery.
```

- [ ] **Step 4: Verify**

Run:

```bash
go test ./compiler -run TestNoLegacyQ35DiscoveryAssumptions -v
rg -n -e "Q35PciInterruptConfigurator" -e "PciConfigPorts" -e "0xFEC00000" -e "MutableBytes\\(address = 0x200000" examples tests/e2e/fixtures wrela compiler/codegen
git diff --check
```

Expected: Go test PASS; `rg` exits with no matches in production source.

- [ ] **Step 5: Commit**

```bash
git add wrela/machine/x86_64/pci.wrela compiler/codegen/uefi_source_codegen_test.go compiler/integration_test.go docs/production-deferred-work.md
git commit -m "chore: remove legacy q35 hardware assumptions -Codex Automated"
```

**Acceptance Criteria:** Legacy q35 PCI configurator and APIC/memory literals are gone from source-facing implementation paths.

---

## 13. Phase 9: End-To-End Acceptance

**Description:** Prove q35 still works because it is discovered, not because examples or compiler code assume it.

**Acceptance Criteria:**
- All Go tests pass.
- QEMU hello, hello ivshmem, arena, cache, and multi-vCPU tests pass on a machine with local dependencies.
- Discovery failures produce boot-fatal output or halt, never reduced hardware continuation.

**Code Example:**

```bash
go test ./...
go test ./tests/e2e -v
```

### Task 20: Add QEMU Discovery Acceptance Assertions

**Description:** Extend e2e tests to validate discovery-dependent behavior and fail on q35 literal regressions.

**Files:**
- Modify: `tests/e2e/hello_qemu_test.go`

- [ ] **Step 1: Add serial output expectations**

Each booted image should already print this compact marker from Tasks 16 and 17:

```text
discovery: memory=selected lapic=selected ioapic=selected pci=selected
```

This e2e marker proves the boot path reached source-visible discovery wiring. Numeric report-field use is covered by Task 18's `discovery.report(...)` source validation.

Update tests:

```go
for _, want := range []string{"discovery:", "lapic=", "ioapic=", "pci="} {
    if !strings.Contains(out, want) {
        t.Fatalf("serial output missing discovery field %q:\n%s", want, out)
    }
}
```

- [ ] **Step 2: Add negative QEMU fixture for missing PCI requirement**

Run the normal hello image without `EnableEdu`. It requires EDU through `pci.require_device(...)`, so the image must not reach the hello success text.

```go
func TestRequiredPciDeviceMissingFailsBoot(t *testing.T) {
    qemuBin, err := exec.LookPath("qemu-system-x86_64")
    if err != nil {
        t.Skipf("qemu-system-x86_64 not found in PATH: %v", err)
    }
    firmware, err := qemu.ResolveFirmware(qemuBin)
    if err != nil {
        t.Skipf("resolve QEMU firmware: %v", err)
    }
    tmp := t.TempDir()
    vars := filepath.Join(tmp, "OVMF_VARS.fd")
    copyFile(t, firmware.Vars, vars)
    image := filepath.Join(tmp, "hello-missing-edu.efi")
    _, err = compiler.Build(compiler.BuildOptions{
        Mode: compiler.ModeDev,
        RootPath: "examples/hello/main.wrela",
        OutputPath: image,
        RepoRoot: ".",
    })
    if err != nil {
        t.Fatalf("build hello image: %v", err)
    }
    out, runErr := qemu.Run(qemu.Options{
        QEMUBinary: qemuBin,
        OVMFCode: firmware.Code,
        OVMFVars: vars,
        ESPDir: filepath.Join(tmp, "esp"),
        ImagePath: image,
        UseSerialPipe: true,
        SuccessText: "hello from wrela",
        Timeout: 5 * time.Second,
        EnableEdu: false,
    })
    if strings.Contains(out, "hello from wrela") {
        t.Fatalf("missing EDU boot unexpectedly reached hello output:\n%s", out)
    }
    if runErr == nil && !strings.Contains(out, "panic:") {
        t.Fatalf("missing EDU boot exited without success text and without panic marker:\n%s", out)
    }
}
```

The test passes when the image either prints `panic:` or QEMU times out before `SuccessText`; it fails if hello output appears.

- [ ] **Step 3: Verify**

Run:

```bash
go test ./tests/e2e -run TestHelloQEMU -v
go test ./tests/e2e -run TestHelloInterruptsQEMU -v
go test ./tests/e2e -run TestMultiVcpuTopicsQEMU -v
git diff --check
```

- [ ] **Step 4: Commit**

```bash
git add tests/e2e/hello_qemu_test.go
git commit -m "test: verify qemu hardware discovery end to end -Codex Automated"
```

**Acceptance Criteria:** E2E tests prove discovered memory/APIC/PCI paths are used and required missing PCI hardware does not silently boot.

### Task 21: Full Acceptance Sweep

**Description:** Run the complete verification matrix and fix only regressions caused by this plan.

**Files:**
- Modify only files touched by failing checks.

- [ ] **Step 1: Run full Go tests**

Run: `go test ./...`

Expected: PASS.

- [ ] **Step 2: Run e2e tests**

Run: `go test ./tests/e2e -v`

Expected: PASS on machines with QEMU/OVMF/ivshmem-server; SKIP only for local dependency checks already present in tests.

- [ ] **Step 3: Run legacy scan**

Run:

```bash
rg -n -e "Q35PciInterruptConfigurator" -e "PciConfigPorts" -e "0xFEC00000" -e "MutableBytes\\(address = 0x200000" examples tests/e2e/fixtures wrela compiler/codegen
```

Expected: no matches.

- [ ] **Step 4: Run diff check**

Run: `git diff --check`

Expected: no output.

- [ ] **Step 5: Commit final fixes**

```bash
git status --short
git add <only files changed to fix failures from Steps 1-4>
git commit -m "test: complete hardware discovery acceptance sweep -Codex Automated"
```

Do not run a blanket stage-all command. If `git status --short` shows files unrelated to the hardware-discovery implementation, leave them unstaged and mention them in the handoff.

**Acceptance Criteria:** Full repository tests pass, q35 literal scans are clean, and the implementation satisfies every acceptance criterion from the design doc.

---

## 14. Design Coverage Checklist

**Description:** Use this checklist before marking the plan complete.

**Acceptance Criteria:**
- Every design-doc requirement maps to at least one task below.
- No item is left to a future milestone unless the design explicitly deferred it.

**Code Example:**

```bash
rg -n "PlatformDiscoveryRoot|require_madt|require_mcfg|route_isa_irq|claim_msi|claim_msix" wrela examples tests/e2e/fixtures
```

Coverage:

- Source-visible discovery: Tasks 3-7, 15-17.
- No hidden compiler discovery oracle: Tasks 3-7 keep parsing in Wrela source; Tasks 13-14 only enforce authority rules.
- Missing primitives: Task 4 adds `BoundedBytes`, `PhysicalBytes`, `MmioRegion`, and `IoPortRegion`.
- Non-forgeable capabilities: Tasks 13-14.
- UEFI configuration tables and memory map: Task 3.
- ACPI RSDP/XSDT/RSDT and table checksums: Task 5.
- MADT CPU/APIC/IOAPIC/override parsing: Task 6.
- Interrupt routing from MADT facts: Task 8.
- MCFG and PCIe ECAM: Tasks 7 and 10.
- PCI devices, BARs, MSI, MSI-X: Tasks 10-12.
- Hardware plan handoff: Task 15.
- Example migration away from q35 literals: Tasks 16-17 and 19.
- Explicit boot-fatal failures: Tasks 4-7, 10-12, and 20.
- QEMU q35 discovered-platform verification: Tasks 20-21.
- Deferred items unchanged: Task 19 documents storage, network, framebuffer, IOMMU, timers, non-ACPI, and non-x86_64 as deferred.
