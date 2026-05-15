# Wrela Hello World Design Doc

## Motivation

The previous Wrela direction made self-hosting the compiler central too early.
That was elegant, but it made iteration expensive. Every compiler or language
change carried bootstrap, seed, provenance, and VM ceremony. That was the wrong
center of gravity while the language and kernel model were still being
discovered.

The restart chooses faster feedback over self-hosting purity.

Wrela's strongest idea is not that its compiler is written in Wrela. Wrela's
strongest idea is that the compiler can see the whole appliance:

- the source graph
- the imports
- the dependency injection graph
- the capability graph
- the memory authorities
- the core topology
- the actor placements
- the queues
- the device bindings
- the interrupt paths
- the final executable image

The compiler should use those facts brutally. Wrela should specialize the whole
computer, not just optimize individual functions.

## Product Thesis

Wrela is an appliance-image language.

A Wrela program is not a Unix process. A Wrela program is a complete,
single-purpose executable image. That image might be a tiny kernel appliance, a
network service, a storage node, a firewall, a controller, or another
freestanding system.

The unit of compilation is the image.

`image` is the composition root for the appliance. It wires capabilities,
memory, devices, actors, queues, and runtime roots.

## Initial Technical Stack

The compiler is written in Go.

Go is chosen because it keeps the compiler boring:

- fast cold builds
- good local test loop
- single native compiler binary
- simple file and manifest tooling
- useful standard library
- straightforward profiling
- enough type safety
- low ceremony for AST and compiler pipeline changes

The first target is x86_64 UEFI under QEMU/OVMF.

The developer can run the Go compiler locally on macOS arm64, while the compiler
emits x86_64 freestanding artifacts. The kernel path runs under QEMU using a
known machine model:

- x86_64
- UEFI
- QEMU q35
- OVMF
- PCIe ECAM
- virtio devices
- COM1 serial
- debug-exit
- Local APIC/IOAPIC with QEMU lab MSI and MSI-X interrupt paths

This keeps the compiler iteration local while keeping the kernel substrate
familiar and testable.

## Repository Shape

The new repository should make the product shape obvious.

```text
/cmd/wrela
  CLI entry point

/compiler
  lexer, parser, syntax, diagnostics, type checking, lowering,
  capability analysis, memory planning, topology planning, codegen

/wrela
  Wrela source modules for freestanding appliance support

/tests
  Go compiler tests and fixture inputs

/docs
  language, compiler, memory, topology, and platform design
```

The compiler implementation should be plain. Prefer simple data structures over
frameworks. Prefer explicit passes over clever abstractions.

## Compiler Architecture

The compiler is speed-first and image-first.

Cold compile speed is a correctness-adjacent feature. If the compiler is slow,
the language becomes painful to discover. Caches may be added later, but V0
should be fast without depending on persistent state.

The rough pipeline is:

```text
manifests
  -> SourceGraph
  -> ParsedUnit[]
  -> DeclarationIndex
  -> TypeDb
  -> ImagePlan
  -> CapabilityGraph
  -> MemoryPlan
  -> TopologyPlan
  -> Reachability
  -> ConcreteFunctions
  -> IR
  -> local optimizations
  -> target codegen
  -> link/image emission
```

The compiler has exactly two modes:

- dev
- release

The compile mode must be specified explicitly. `dev` keeps all semantic checks,
reachability, DI validation, memory planning, topology lowering, and required
image specialization, but skips expensive non-semantic optimizations. `release`
runs the full production optimization path.

## Non-Goals And Deferred Work

The MVP deliberately excludes several tempting pieces:

- self-hosting the compiler
- a hosted Linux production target
- Wrela-authored unit or integration tests
- user-defined traps
- interrupt-capable driver paths
- explicit vCPU placement policies
- multi-architecture backends
- a compiler daemon or incremental cache as a foundation

## Diagnostics

Diagnostics should be stable, deterministic, and specific.

The compiler should sort diagnostics by:

1. phase
2. source file identity
3. source span
4. diagnostic code
5. stable sequence number

Diagnostics should teach the language model:

- "this capability is too broad for this dependency"
- "this value cannot cross from core 0 to core 1"
- "this allocation requires a memory authority"
- "this boot-only region escapes into runtime"
- "this DMA buffer is still owned by the device"
- "this MMIO value cannot be treated as regular memory"

## Language Shape

Wrela should be small, explicit, and image-oriented.

The language should have:

- modules and imports
- records/data
- classes or resource types for stateful components
- methods
- interfaces/traits only if needed
- generics only where they pay for themselves
- explicit setup/runtime distinction
- explicit capabilities
- explicit memory authority types
- explicit topology constructs
- first-class static image plan validation

The language should avoid:

- ambient globals with authority
- hidden allocation
- reflection
- runtime class loading
- dynamic linking
- exceptions as a control-flow foundation
- broad implicit sharing

## Example: HelloWorld

This is the first rough example of what we need to compile and run on QEMU.

```wrela
// below is illustrative of what an import should look like
// obviously there would be many more imports in the real file
use { DelegatedHardware } from drivers.interfaces

driver SerialDriver {
    device = SerialDeviceDescription
    registers = IoPortRegisters
    memory = DriverMemory

    fn initialize(self) -> SerialDriver {
        self.device.verify_compatible()

        self.registers.write8(offset = 1, value = 0x00)
        self.registers.write8(offset = 3, value = 0x80)
        self.registers.write8(offset = 0, value = 0x03)
        self.registers.write8(offset = 1, value = 0x00)
        self.registers.write8(offset = 3, value = 0x03)
        self.registers.write8(offset = 2, value = 0xC7)
        self.registers.write8(offset = 4, value = 0x03)

        return self
    }

    fn create_write_path(self, owner: ExecutorPlacement) -> SerialWritePath {
        return SerialWritePath(
            owner = owner,
            registers = self.registers.writer_view(),
            memory = self.memory.path_region("write")
        )
    }
}

driver path SerialWritePath {
    owner = ExecutorPlacement
    registers = SerialWriterRegisters
    memory = DriverPathMemory

    fn write(self, bytes: String) {
        for byte in bytes {
            self.wait_until_ready()
            self.registers.write8(offset = 0, value = byte)
        }
    }

    fn wait_until_ready(self) {
        while (self.registers.read_status(offset = 5) & 0x20) == 0 {
            cpu.pause()
        }
    }
}

executor HelloWorld {
    execution = ExecutionContext
    memory = ExecutorMemory
    serial_path = SerialWritePath

    start fn run(self) -> never {
        self.serial_path.write("hello from wrela\n")
        self.execution.halt_forever()
    }

    on serial_path.interrupt(event: SerialPathInterrupt) {
        self.serial_path.write_byte(value = event.byte)
    }
}

image HelloSerial {
    fn run(uefi: UefiExecutionContext) -> never {
        let delegated_hardware = DelegatedHardware.from_uefi(uefi)

        let firmware_caps = delegated_hardware.split_firmware_capabilities()

        let memory_map = MemoryMapReader(
            firmware_caps.memory_map_access
        ).read()

        let acpi_tables = AcpiTableReader(
            firmware_caps.configuration_tables
        ).read()

        let framebuffer = FramebufferClaimer(
            firmware_caps.graphics_protocols
        ).claim()

        let boot_assets = BootAssetReader(
            firmware_caps.boot_filesystem
        ).read()

        let hardware_description = HardwareDescriptionBuilder(
            memory_map = memory_map,
            acpi_tables = acpi_tables,
            framebuffer_info = framebuffer.info,
            boot_assets_manifest = boot_assets.manifest
        ).build()

        let memory_plan = PhysicalMemoryPlanner(
            memory = hardware_description.memory,
            framebuffer_region = framebuffer.memory_region
        ).plan()

        let virtual_memory_plan = VirtualMemoryPlanner(
            kernel_regions = memory_plan.kernel_regions,
            mmio_regions = memory_plan.mmio_regions,
            framebuffer_region = memory_plan.framebuffer_region
        ).plan()

        let interrupt_plan = InterruptPlanner(
            cpus = hardware_description.cpus,
            apic = hardware_description.apic,
            devices = hardware_description.devices
        ).plan()

        let pci_plan = PciPlanner(
            pci = hardware_description.pci,
            mmio_regions = memory_plan.mmio_regions,
            interrupts = interrupt_plan.device_interrupts
        ).plan()

        let driver_plan = DriverPlanner(
            devices = hardware_description.devices,
            pci = pci_plan,
            interrupts = interrupt_plan,
            dma_regions = memory_plan.dma_regions
        ).plan()

        let kernel_hardware = delegated_hardware.assume_kernel_ownership(
            memory_plan = memory_plan,
            virtual_memory_plan = virtual_memory_plan,
            interrupt_plan = interrupt_plan,
            pci_plan = pci_plan,
            driver_plan = driver_plan,
            framebuffer = framebuffer,
            boot_assets = boot_assets
        )

        let serial_driver = SerialDriver(
            device = kernel_hardware.devices.serial.primary(),
            registers = kernel_hardware.io_ports.claim(
                driver_plan.serial.primary.registers
            ),
            memory = kernel_hardware.memory.driver_region(
                driver_plan.serial.primary.memory
            )
        ).initialize()

        let vcpu_0 = kernel_hardware.vcpus.next()

        let serial_path = serial_driver.create_write_path(
            owner = vcpu_0
        )

        let hello_world = HelloWorld(
            execution = vcpu_0.execution,
            memory = vcpu_0.memory,
            serial_path = serial_path
        )

        kernel_hardware.start(hello_world)
    }
}
```

The compiler should enforce:

- each physical root driver is unique
- every created driver path has one owner
- multiple executors may not receive one root driver; driver paths must be used
- local managed memory does not cross executor boundaries
- degraded executor placement changes performance, not correctness

Interrupts and traps are deliberately outside the MVP surface. Device
interrupts will later belong to interrupt-capable driver paths. CPU traps remain
platform panic machinery unless a future subsystem explicitly claims trap
authority.

Other random thoughts:

- no module-scope functions in V0. Behavior lives in classes, drivers, driver
  paths, executors, and image roots.
- `data` is a first-class semantic. It represents a typed bundle of attributes.
- no special syntax except where required to specify unikernel abstractions
- the whole image should compile down to a `.efi` file to be executed by UEFI
