# Real Hardware Discovery Design

## Purpose

Wrela should move from a QEMU q35 lab shape to explicit, source-visible hardware discovery for modern x86_64 UEFI systems.

The goal is not to add a hidden platform oracle. The goal is to make real machine facts explicit Wrela values:

- usable physical memory regions
- ACPI roots and tables
- enabled CPUs and APIC IDs
- local APIC and IOAPIC routing facts
- PCIe ECAM regions
- discovered PCI devices, BARs, and MSI/MSI-X capabilities
- typed authorities that drivers and image wiring can claim

The compiler may still implement ABI mechanics and low-level instruction emission, but discovery policy belongs in Wrela source. If Wrela lacks a primitive needed to express discovery, this milestone should add the smallest explicit intrinsic or `asm fn` surface for that primitive instead of hiding the behavior in compiler magic.

## Target Platform Scope

This milestone targets modern x86_64 hardware only.

Required:

- UEFI boot
- ACPI
- XSDT or RSDT discovery through the UEFI configuration table
- MADT for CPU/APIC topology
- local APIC and IOAPIC
- PCIe ECAM via MCFG for PCIe enumeration

Intentionally out of scope:

- legacy BIOS boot
- Intel MP tables
- 32-bit boot paths
- non-x86_64 architectures
- non-ACPI platforms
- storage, network, or framebuffer drivers
- full memory hardening, IOMMU policy, and production timer runtime

QEMU q35 remains the main verification platform, but q35 should be a discovered platform, not an implicit compiler worldview.

## Design Principles

### Discovery Is Source Visible

Image source should explicitly ask for platform facts and choose policy.

Example shape:

```wrela
phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
    let tables = hardware.uefi_configuration_tables()
    let memory_map = hardware.memory_map()

    let acpi = AcpiRoot.find(tables = tables)
    let madt = acpi.require_madt()
    let mcfg = acpi.require_mcfg()

    let cpus = madt.enabled_cpus()
    let io_apics = madt.io_apics()
    let pci = PcieEcam.enumerate(mcfg = mcfg)

    let memory_region = memory_map.require_usable_region(
        min_base = 0x200000,
        length = 0x400000,
        align = 4096
    )
    let cpu_plan = cpus.require_count(count = 2).cpu_plan()
    let interrupt_plan = io_apics.route_isa_irq(irq = 4)

    return hardware.exit_to_owned_hardware(
        memory_plan = MemoryPlan(...),
        cpu_plan = cpu_plan,
        hardware_plan = HardwarePlan(...)
    )
}
```

The exact API can evolve, but this is the intended load-bearing idea: Wrela code performs discovery steps and handles absence explicitly.

### No Hidden Discovery Magic

Do not add a compiler-known `hardware.discover()` that secretly parses ACPI, PCIe, and memory maps.

Compiler support is appropriate for:

- emitting `asm fn` bodies
- UEFI entry ABI mechanics
- PE/COFF layout
- physical/volatile load and store codegen
- enforcing authority construction and consumption rules

Compiler support is not appropriate for:

- deciding which CPUs are required
- choosing memory regions
- matching PCI devices
- assigning device policy
- deciding whether missing hardware is fatal or optional

### Add Missing Wrela Primitives

If discovery needs an operation Wrela cannot express, add an explicit primitive.

Examples:

```wrela
class PhysicalBytes {
    address: PhysicalAddress
    length: U64

    asm fn read_u8(self, offset: U64) -> U8
    asm fn read_u16(self, offset: U64) -> U16
    asm fn read_u32(self, offset: U64) -> U32
    asm fn read_u64(self, offset: U64) -> U64
}

class MmioRegion {
    address: PhysicalAddress
    length: U64

    asm fn read8(self, offset: U64) -> U8
    asm fn read16(self, offset: U64) -> U16
    asm fn read32(self, offset: U64) -> U32
    asm fn write8(self, offset: U64, value: U8)
    asm fn write16(self, offset: U64, value: U16)
    asm fn write32(self, offset: U64, value: U32)
}
```

These primitives should require authority-bearing receiver values. They should not become ambient free functions for arbitrary physical memory access.

### Capabilities Must Not Be Forgeable

Source-visible does not mean freely constructible.

Values such as `AcpiTable`, `PciDevice`, `MmioRegion`, `MsiCapability`, `IoApicRoute`, and `EnabledCpu` should come from authority-bearing discovery methods. Direct construction in ordinary user modules should be rejected or restricted to trusted platform modules.

The checker should preserve the existing Wrela direction:

- physical authority comes from firmware/platform roots
- narrowed capabilities are created by consuming or slicing broader authority
- device paths and interrupt publishers are claimed exactly once
- duplicate claims are semantic errors

## Platform Primitive Layer

The first layer should make firmware table parsing expressible in Wrela.

### UEFI Inputs

`DelegatedHardware` should expose:

- configuration table entries
- memory map descriptors
- image/system-table pointers only through typed views

Possible source shape:

```wrela
class DelegatedHardware {
    fn uefi_configuration_tables(self) -> UefiConfigurationTables
    fn memory_map(self) -> UefiMemoryMap
}

class UefiConfigurationTables {
    fn find_acpi_rsdp(self) -> AcpiRsdpSearchResult
}

class UefiMemoryMap {
    fn require_usable_region(self, min_base: PhysicalAddress, length: U64, align: U64) -> PhysicalBytes
}
```

The initial implementation can keep the result types concrete and small. It does not need generic iterators.

### Bounded Byte Views

ACPI and PCI parsing need bounded reads and slices.

```wrela
class BoundedBytes {
    address: PhysicalAddress
    length: U64

    asm fn read_u8(self, offset: U64) -> U8
    asm fn read_u16(self, offset: U64) -> U16
    asm fn read_u32(self, offset: U64) -> U32
    asm fn read_u64(self, offset: U64) -> U64

    fn slice(self, offset: U64, length: U64) -> BoundedBytes
}
```

Reads must be bounds-checked or semantically restricted to bounded parser code. An out-of-bounds firmware-table read is a boot-fatal platform error, not undefined behavior.

### Checksums and Signatures

ACPI parsing requires table signature checks and checksums. This can be ordinary Wrela loop code once byte reads exist.

Useful helpers:

```wrela
fn acpi_checksum_ok(bytes: BoundedBytes) -> Bool
fn signature4(bytes: BoundedBytes, offset: U64) -> U32
```

These helpers should live in platform source modules, not compiler code.

## ACPI Discovery

ACPI discovery should be implemented as Wrela platform source over bounded physical views.

### RSDP and XSDT/RSDT

The platform should:

- locate RSDP from UEFI configuration tables
- validate RSDP checksum
- prefer XSDT on x86_64
- validate the root table checksum
- expose table lookup by signature

Possible source shape:

```wrela
class AcpiRoot {
    fn find(tables: UefiConfigurationTables) -> AcpiRoot
    fn require_table(self, signature: U32) -> AcpiTable
    fn require_madt(self) -> MadtTable
    fn require_mcfg(self) -> McfgTable
}

class AcpiTable {
    bytes: BoundedBytes
    signature: U32
    length: U32
}
```

Missing required tables should go through an explicit boot-fatal path in source. Optional tables can return result values later, but the first milestone can keep required APIs for MADT and MCFG.

### MADT

MADT parsing should expose:

- local APIC base
- enabled local APIC or x2APIC CPU entries
- CPU UID to APIC ID mapping
- IOAPIC entries
- interrupt source overrides
- local APIC NMIs if needed for later trap work

Possible source shape:

```wrela
class MadtTable {
    fn local_apic_base(self) -> PhysicalAddress
    fn enabled_cpus(self) -> EnabledCpuSet
    fn io_apics(self) -> IoApicSet
    fn interrupt_source_overrides(self) -> InterruptOverrideSet
}

class EnabledCpuSet {
    fn require_count(self, count: U64) -> CpuTopology
}

class CpuTopology {
    bootstrap_apic_id: U32
    secondary_apic_id: U32

    fn vcpu0(self) -> Vcpu
    fn vcpu1(self) -> Vcpu
}
```

The first implementation may remain bounded to the current two-vCPU examples while deriving APIC IDs from MADT instead of assuming `0` and `1`.

### Interrupt Routing

Interrupt routing should consume MADT-derived IOAPIC facts and interrupt source overrides.

The source should no longer assume:

- COM1 maps directly to IRQ4 without override handling
- IOAPIC base is always `0xFEC00000`
- LAPIC base is always `0xFEE00000`
- vectors are fixed because q35 happens to work

Possible source shape:

```wrela
let irq4 = interrupts.route_isa_irq(
    irq = 4,
    vector = InterruptVector(value = 0x40)
)

let serial_path = serial_driver.create_console_path(
    identity = PathIdentity(label = "hello.console"),
    route = irq4,
    rx = serial_rx_topic.publisher()
)
```

Vector choice can remain explicit in source for now. The important change is that physical IOAPIC routing comes from discovered authority.

## PCIe Discovery

PCIe discovery should use ACPI MCFG and ECAM MMIO reads.

### MCFG

MCFG parsing should expose ECAM windows:

```wrela
class McfgTable {
    fn ecam_windows(self) -> PcieEcamWindows
}

class PcieEcamWindow {
    base: PhysicalAddress
    segment: U16
    start_bus: U8
    end_bus: U8
}
```

### Enumeration

Enumeration should walk buses, devices, and functions through bounded ECAM config authority.

Possible source shape:

```wrela
class PcieEcam {
    fn enumerate(windows: PcieEcamWindows) -> PciDeviceSet
}

class PciDeviceSet {
    fn require_device(self, vendor_id: U16, device_id: U16) -> PciDevice
    fn require_class(self, class_code: U8, subclass: U8) -> PciDevice
}

class PciDevice {
    identity: PciDeviceIdentity

    fn claim_bar(self, index: U8) -> MmioRegion
    fn claim_msi(self) -> MsiCapability
    fn claim_msix(self) -> MsixCapability
}
```

The first milestone should discover the existing QEMU EDU and ivshmem devices through PCI enumeration rather than hardcoded config paths.

### BAR and Capability Parsing

BAR claiming should:

- read BAR type and size
- distinguish MMIO from IO BARs
- reject unsupported 32-bit/64-bit shapes explicitly
- return typed `MmioRegion` or `IoPortRegion`
- prevent claiming the same BAR twice

MSI/MSI-X claiming should:

- discover capability list entries
- expose table/PBA BAR and offsets for MSI-X
- program message address/data from interrupt-route authority
- prevent duplicate vector/table claims

## Hardware Plan Handoff

Discovery should happen during `delegated_hardware`. The owned phase should receive planned, narrowed authorities.

Reasoning:

- boot planning remains deterministic
- firmware table authority does not leak into ordinary driver code
- resource claims happen before executors start
- owned code receives only the device and memory capabilities it needs

Possible handoff shape:

```wrela
data HardwarePlan {
    cpus: CpuTopology
    interrupts: InterruptRoutingPlan
    pci: ClaimedPciPlan
}

return hardware.exit_to_owned_hardware(
    memory_plan = memory_plan,
    cpu_plan = cpu_plan,
    hardware_plan = hardware_plan
)
```

This may require extending `OwnedHardware` with discovered/planned authorities:

```wrela
class OwnedHardware {
    memory: OwnedMemory
    io_ports: IoPortAuthority
    executors: ExecutorRegistry
    vcpu0: Vcpu
    vcpu1: Vcpu
    interrupts: InterruptAuthority
    pci: PciAuthority
}
```

The exact shape should stay small and driven by the examples. Avoid a giant universal hardware object.

## Example Migration

The hello and multi-vCPU examples should stop encoding q35 facts directly.

Current literals to remove or isolate behind discovered authorities:

- `MutableBytes(address = 0x200000, ...)`
- `LocalApic(base = 0xFEE00000)`
- `IoApic(base = 0xFEC00000)`
- fixed q35 PCI paths inside source-facing APIs
- hardcoded APIC ID assumptions for secondary vCPU startup

Target shape:

```wrela
let discovery = PlatformDiscovery.from_uefi(hardware = hardware)
let memory_region = discovery.memory.require_usable_region(...)
let cpus = discovery.acpi.madt.enabled_cpus().require_count(count = 2)
let interrupts = discovery.acpi.madt.interrupt_authority()
let pci = discovery.acpi.mcfg.pcie().enumerate()

let edu = pci.require_device(vendor_id = 0x1234, device_id = 0x11E8)
let edu_bar0 = edu.claim_bar(index = 0)
let edu_msi = edu.claim_msi()

let serial_route = interrupts.route_isa_irq(irq = 4, vector = InterruptVector(value = 0x40))
let edu_route = edu_msi.route(vector = InterruptVector(value = 0x41))
```

This keeps policy explicit while making hardware facts discovered.

## Error Handling

Hardware discovery failures should be explicit boot failures.

Examples:

- missing ACPI RSDP
- bad ACPI checksum
- missing MADT
- no enabled secondary CPU when the image requires one
- missing MCFG when PCIe enumeration is required
- required PCI device not found
- unsupported BAR shape
- MSI/MSI-X capability missing when required

Until Wrela has a first-class panic mechanism, source should use a clear boot-fatal path:

```wrela
platform.panic(message = "missing MADT")
```

If `platform.panic` does not exist yet, add it as an explicit platform intrinsic or `asm fn` surface rather than open-coding ambiguous serial writes everywhere.

The panic path should:

- write a diagnostic string when a serial/debug path is available
- halt forever
- never silently continue with reduced hardware

## Testing Strategy

### Unit Tests

Add source-shape and semantic tests for:

- UEFI configuration table accessors
- bounded physical byte read APIs
- direct construction rejection for forged hardware capabilities
- ACPI table result types and method shapes
- PCI device/BAR/MSI result types and method shapes

Add Go tests for parser/checker behavior where new source constructs or authority rules are required.

### Platform Parser Tests

Use small synthetic table blobs where possible:

- RSDP checksum pass/fail
- XSDT table lookup
- MADT CPU/APIC/IOAPIC entries
- MCFG ECAM entries
- PCI capability list walking

These should exercise Wrela source code if the compiler can execute enough of it in generated images. Where not practical yet, keep parser logic small and verify via compiled source/IR shape.

### QEMU E2E Tests

QEMU q35 should prove discovery, not hardcoding.

Required e2e outcomes:

- hello boots using memory selected from the UEFI memory map
- local APIC and IOAPIC bases come from MADT
- secondary vCPU APIC ID comes from MADT
- EDU and ivshmem are found by PCI enumeration
- MSI/MSI-X routes are programmed from discovered PCI capabilities
- serial, EDU, ivshmem, and multi-vCPU topic tests still pass

Tests should fail if the examples reintroduce q35 literals for APIC bases, PCI devices, or hand-selected memory arenas.

### Inspection Tests

Add an internal debug or test-only way to report the discovered plan:

- memory region selected
- enabled CPUs and APIC IDs
- IOAPIC base and GSI ranges
- interrupt source overrides
- PCI devices discovered
- BARs claimed
- MSI/MSI-X vectors claimed

This can later become a real image-inspection/developer-experience tool.

## Acceptance Criteria

- Discovery APIs are source-visible Wrela values, not a hidden compiler `discover()` path.
- Missing discovery primitives are added as explicit Wrela intrinsics or `asm fn`s.
- ACPI RSDP/XSDT/MADT/MCFG parsing lives in Wrela platform modules where practical.
- Direct construction of hardware capabilities is rejected outside trusted authority flows.
- Hello and multi-vCPU examples no longer hardcode q35 memory arenas, APIC bases, PCI device addresses, or secondary APIC IDs.
- QEMU q35 e2e tests pass by discovering q35 facts.
- Required hardware absence fails through an explicit boot-fatal path.
- Existing executor slot, topic, interrupt, and vCPU placement semantics remain intact.

## Deferred From This Design

This design intentionally does not implement:

- storage, network, framebuffer, or input drivers
- full timer runtime
- interrupt queues and shared interrupt demultiplexing
- page permission hardening, guard pages, W^X, or NX
- IOMMU/DMA remapping
- generic traits or source-level generics
- non-UEFI or non-x86_64 targets

Those become cleaner follow-on plans once hardware facts are explicit source-visible authority.
