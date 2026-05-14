# Production Deferred Work Register

## Memory and address spaces
- This is required for production and not optional because memory safety, isolation, and deterministic mapping break quickly as images and drivers grow beyond a toy shape.
- v0 exclusion reason: this work is intentionally deferred to keep the first compiler iteration small and to validate the core end-to-end flow before hardening layout policy.
- v0 must not block: moving to a managed address-space model, higher-half VA layout, and W^X/NX policy should be possible without changing PE emission contracts.

## CPU and interrupts
- This is required for production and not optional because AP startup, interrupts, and timer behavior are mandatory for reliable execution beyond single-core lab demos.
- v0 exclusion reason: this release intentionally limits execution to one owned core and a minimal, owned-hardware bootstrap to keep the surface area stable.
- v0 must not block: richer interrupt routing, AP initialization, and timer/IRQ models should slot in as independent modules on top of existing CPU section/relocation plumbing.

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
