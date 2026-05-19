# Production Deferred Work Register

## Memory and address spaces
- This is required for production and not optional because memory safety, isolation, deterministic placement, and memory-footprint predictability break quickly as images and drivers grow beyond a toy shape.
- Production memory is now physical-region authority first. Firmware-derived `PhysicalRegionAuthority` values create named root arenas; executors, queues, caches, AP records, and DMA-intended buffers claim bounded children; frame lifetimes remain checked with `with` frames; the image report is the audit surface for ownership and wake paths.
- x86_64 paging remains target boot glue only: the backend emits a minimal 2 MiB identity map required by long mode, but Wrela source does not expose virtual address spaces, higher-half layout, page permissions, W^X, NX, or guard pages in this stage.
- This stage must not block adding hardware-enforced page permissions, guard pages, DMA/IOMMU policy, or higher-half layout as backend artifacts generated from the physical-region authority graph.

## CPU and interrupts
- This is required for production and not optional because AP startup, interrupts, and timer behavior are mandatory for reliable execution beyond single-core lab demos.
- Implemented direction: COM1 receive via IOAPIC, EDU via MSI, ivshmem-doorbell via MSI-X, AP startup, explicit vCPU placement, shared interrupt source claims, bounded interrupt queues, local-APIC timer routes, x2APIC selection with xAPIC fallback, and typed `TimerTickPayload` topics are source-visible and reportable.
- v0 still exposes no CPU traps.
- Remaining production work: richer topology heuristics, hardware-derived multiprocessor routing beyond the current lab fixtures, explicit high-CR3 trampoline support, and an interrupt save policy if Wrela ever emits FPU/SSE/AVX instructions.

## Hardware discovery
- This is required for production and not optional because ACPI, PCIe, and framebuffer discovery are expected for realistic boots and platform integration.
- Implemented direction: UEFI roots, ACPI RSDP/RSDT/XSDT lookup, MADT CPU/interrupt facts, MCFG ECAM windows, PCI bridge bus walking, PCI BAR/MSI/MSI-X claims, timer/locality/framebuffer fact shapes, and required-hardware boot fatal paths are source-visible Wrela authorities.
- Remaining production work: richer PCI capability coverage, IOMMU/DMA policy, robust firmware quirk handling, and broader hardware-in-loop coverage.

## Drivers and IO
- This is required for production and not optional because storage, network, and frame-buffer paths depend on robust driver abstractions, not toy I/O.
- v0 exclusion reason: only serial COM1 is provided as a placeholder path so we can validate end-to-end emitted executables quickly.
- v0 must not block: adding virtio, storage, network, and typed MMIO helpers must not require redesigning the driver/path graph semantics or relocation layout.

## Executor runtime
- Implemented direction: executors are explicitly assigned to vCPUs through source-visible `ExecutorSlot` values and vCPU `start`/`enter` dispatch. Communication uses SPMC topics with compiler-planned cache-line layout and wake paths. There is no hidden scheduler, migration, or work stealing.
- Implemented substrate work now includes required/preferred placement constraints, locality-aware executor memory fallback reporting, generalized topic payload layout, `TimerTickPayload`, and explicit wake strategy reporting with `monitor/mwait` bytes available and `sti_hlt` fallback.
- Remaining production work: richer topology placement, broader CPU feature calibration, and production-grade monitor/mwait policy selection.

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

## Storage beyond the first NVMe event-store milestone
- POSIX filesystem compatibility remains out of scope. The first storage surface is events, blobs, checkpoints, projections, and file-like entity streams.
- SQL, relational query planning, general secondary indexes, multi-writer event-log sharding, and network replication remain out of scope.
- Production command inboxes, idempotency records, full-disk encryption, IOMMU-backed DMA isolation, SGL-heavy NVMe transfers, tuned compression codec selection, and destructive retention policies remain deferred.
- The first blob cipher used by QEMU tests is a named development passthrough mode behind the final blob manifest API. Production images must not construct it without explicit development-storage opt in.
