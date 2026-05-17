# Wrela

Wrela is an appliance-image language: it compiles a whole, single-purpose
machine image instead of a program that expects an operating system to assemble
the machine around it.

A Wrela image describes the source graph, hardware discovery, memory authority,
driver paths, executor placement, queues, topics, interrupt routes, and final
executable artifact in one compiler-visible shape. The goal is to make the
important systems questions reviewable at build time:

- who owns this memory?
- who is allowed to touch this device?
- which executor can receive this event?
- which vCPU will wake when this interrupt fires?
- can this temporary buffer outlive its frame?

Wrela is for small, freestanding systems where knowing the whole machine is the
point: kernel appliances, controllers, firewalls, storage nodes, embedded
services, lab kernels, and other images that should boot into one explicit job.

## The Pitch

Most systems languages compile functions and leave the final machine shape to an
OS, runtime, linker script, driver model, and convention. Wrela makes the image
the unit of compilation.

The `image` block is the composition root. It discovers platform facts, claims
memory, narrows hardware authority, wires drivers into paths, assigns executors
to vCPUs, connects publish/subscribe topics, and enters the final runtime loop.
The compiler can then reason about the whole appliance instead of optimizing
isolated code in a vacuum.

That is Wrela's core bet: if the compiler can see the machine, it can reject
entire classes of mistakes before the image boots.

## What Wrela Looks Like

Wrela is intentionally small and explicit. The current language surface includes:

- modules and `use` imports
- `data` records and `class` types
- `unique` authority-bearing roots such as delegated and owned hardware
- `driver` and `driver path` declarations for broad hardware roots and narrowed
  capabilities
- `image` and `phase` declarations for boot-time ownership transitions
- `executor` declarations with `start fn` entry points
- explicit `ExecutorSlot` values and `hardware.vcpuN.start/enter` placement
- SPMC topic publishers and subscriptions for executor and interrupt events
- direct physical memory authority, executor arenas, and bounded `with` frames
- restricted `asm fn` hooks for trusted platform primitives

Example image code currently wires discovered x86_64 UEFI hardware into a serial
driver, PCI MSI/MSI-X devices, executor memory, topic subscriptions, and an
executor entered on a concrete vCPU.

## Current Status

Wrela is a v0 research compiler and platform substrate. It is real enough to
build and boot generated x86_64 UEFI images under QEMU/OVMF, but it is not a
production language or operating system yet.

Implemented and exercised today:

- a Go compiler with lexer, parser, semantic checks, IR lowering, x86_64 codegen,
  and PE/COFF EFI emission
- `dev` builds through `wrela build --mode dev`
- x86_64 UEFI boot under QEMU q35 with OVMF
- source-visible UEFI, ACPI, MADT, MCFG, PCI BAR, MSI, and MSI-X discovery
- COM1 serial output and receive interrupts through IOAPIC
- EDU MSI and ivshmem MSI-X interrupt paths in QEMU fixtures
- explicit vCPU placement with AP startup and two-vCPU topic examples
- SPMC topics with compiler-planned routing and cache-line-aware layout work
- physical/executor memory checks, bounded arena frames, cache-memory fixtures,
  and negative tests for authority and lifetime mistakes

Actively being shaped:

- the production substrate that unifies memory authority, hardware discovery,
  CPU/interrupt/timer policy, executor runtime, and audit reports
- hierarchical arenas, interrupt queues, wake-target reporting, timer events,
  and deterministic memory/placement reports
- richer diagnostics and image-report artifacts for answering "who owns what,
  who can signal whom, and what hardware did this image require?"

Not implemented or not production-grade yet:

- release mode optimization
- self-hosting
- source-visible virtual address spaces, page permissions, W^X/NX, or guard
  pages
- full IOMMU/DMA policy
- storage, network, framebuffer, or broad real-hardware driver coverage
- secure boot signing, update provenance, and a complete trust chain
- a hidden scheduler, migration, work stealing, or process model

The short version: Wrela has proven the end-to-end image path and several of the
hard semantic ideas. The project is now converging those proofs into a substrate
that can support real drivers without re-solving memory, interrupt, timer, and
executor policy for every device.

## Try It

Build the hello image:

```sh
go run ./cmd/wrela build --mode dev examples/hello/main.wrela -o build/hello.efi
```

Run the fast compiler tests:

```sh
go test ./compiler/... ./cmd/wrela
```

The QEMU end-to-end tests require `qemu-system-x86_64`, OVMF firmware, and, for
some fixtures, `ivshmem-server`.

## Repository Map

- `cmd/wrela`: CLI entry point
- `compiler`: compiler pipeline, semantic checks, IR, codegen, PE/COFF writer,
  QEMU helpers, diagnostics, and report contracts
- `wrela`: Wrela platform and machine source modules
- `examples`: bootable example images
- `tests`: compiler fixtures and QEMU end-to-end coverage
- `docs/design`: design notes for major language and platform decisions
- `docs/implementation`: executable implementation plans
- `docs/production-deferred-work.md`: current production gap register

## Deeper Reading

- `docs/design/hello_world_design_doc.md`: the restart thesis and image-first
  compiler shape
- `docs/design/2026-05-16-real-hardware-discovery-design.md`: source-visible
  UEFI, ACPI, and PCI discovery
- `docs/design/2026-05-16-explicit-vcpu-executors-design.md`: explicit vCPU
  executors and SPMC topics
- `docs/design/2026-05-17-production-substrate-convergence-design.md`: current
  production substrate direction

## License

MIT.
