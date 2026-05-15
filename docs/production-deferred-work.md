# Production Deferred Work Register

## Memory and address spaces
- This is required for production and not optional because memory safety, isolation, deterministic placement, and memory-footprint predictability break quickly as images and drivers grow beyond a toy shape.
- Selected direction: Wrela source models memory as physical-region authority first with hierarchical arenas, bounded `with` frames, statically checked lifetimes, bounded root executor memory, and cache memory that evicts by default.
- x86_64 paging remains target boot glue only: the backend emits a minimal 2 MiB identity map required by long mode, but Wrela source does not expose virtual address spaces, higher-half layout, page permissions, W^X, NX, or guard pages in this stage.
- v0 exclusion reason: this work was intentionally deferred to keep the first compiler iteration small and to validate the core end-to-end flow before hardening memory policy.
- This stage must not block adding hardware-enforced page permissions, guard pages, DMA/IOMMU policy, or higher-half layout as backend artifacts generated from the physical-region authority graph.

## CPU and interrupts
- This is required for production and not optional because AP startup, interrupts, and timer behavior are mandatory for reliable execution beyond single-core lab demos.
- v0 implementation now proves COM1 receive via IOAPIC, EDU via MSI, and ivshmem-doorbell via MSI-X on QEMU lab hardware.
- v0 still exposes no CPU traps.
- Production work remains for ACPI discovery, PCI enumeration, shared interrupts, timers, interrupt queues, x2APIC, and multiprocessor routing.

## Hardware discovery
- This is required for production and not optional because ACPI, PCIe, and framebuffer discovery are expected for realistic boots and platform integration.
- v0 exclusion reason: the current implementation focuses on identity-mapped execution and serial output only, with no platform-agnostic probe graph yet.
- v0 must not block: the deferred discovery path should consume the same `platform.uefi.types` and machine/module boundaries as this release so future modules can be added without rewrites.

## Drivers and IO
- This is required for production and not optional because storage, network, and frame-buffer paths depend on robust driver abstractions, not toy I/O.
- v0 exclusion reason: only serial COM1 is provided as a placeholder path so we can validate end-to-end emitted executables quickly.
- v0 must not block: adding virtio, storage, network, and typed MMIO helpers must not require redesigning the driver/path graph semantics or relocation layout.

## Executor runtime
- This is required for production and not optional because multi-executor scheduling and movement are core for practical systems workloads.
- v0 exclusion reason: one executor path is sufficient to validate compiler and PE plumbing before adding runtime scheduling complexity.
- v0 must not block: queueing, migration, and cross-executor movement should be introduced as additional runtime modules rather than replacing the current `executor` contract.

## Language/type system
- This is required for production and not optional because richer ownership, traits, and phase modeling are essential for maintainable system code.
- v0 exclusion reason: static capability checks and minimal ownership are intentionally scoped so the first compiler can lock in bytecode and ABI behavior.
- v0 must not block: generics, traits, richer phases, and wider typestate checks should layer onto the existing declarations and symbol model.

## Compiler backend
- This is required for production and not optional because optimization, register allocation, and broader target support define whether the compiler scales beyond toy binaries.
- v0 exclusion reason: backend maturity is deferred while validating correct PE emission, x86_64 instruction support, and transition behavior.
- v0 must not block: optimization and multi-target support should reuse the current `compiler/codegen` and `compiler/pecoff` bridge types without forced data model churn.

## Diagnostics/tooling
- This is required for production and not optional because diagnostics quality and traceability define developer confidence and iteration speed.
- v0 exclusion reason: this stage focuses on functional contracts and intentionally keeps analysis depth low.
- v0 must not block: richer diagnostics and tracing should be added as new diagnostics phases and formatter options, not by replacing existing build result shapes.

## Security/trust
- This is required for production and not optional because signing and provenance are foundational for secure boot and update workflows.
- v0 exclusion reason: the first target is functional correctness of firmware entry transition and codegen behavior, not trust-chain integration.
- v0 must not block: secure boot hooks, signature handling, and authority audit tools should be additive to the image and PE output path.

## Testing/verification
- This is required for production and not optional because parser/assembler/IR/codegen fuzzing and hardware tests are essential to prevent silent regressions.
- v0 exclusion reason: only a minimal set of unit tests is practical in the bootstrap stage while contracts are still stabilizing.
- v0 must not block: property-based tests and hardware-in-loop suites should execute against the same exported interfaces, especially `WriteEFI` and generated image behavior.

## Portability
- This is required for production and not optional because platform options (Linux-hosted compiler flows, other architectures, and non-UEFI paths) determine real adoption.
- v0 exclusion reason: initial delivery is x86_64-UEFI only to keep the boot and ABI path tractable.
- v0 must not block: the split between `wrela/*` modules and platform glue should make additional targets additive.

## Developer experience
- This is required for production and not optional because iterative builds, caching, and inspection tooling are what keep the compiler usable day-to-day.
- v0 exclusion reason: this milestone prioritizes correctness over productivity niceties so the first end-to-end artifact can be proven.
- v0 must not block: cache/incremental mode, compiler daemon, and image-inspection tooling should consume the current command and result contracts.
