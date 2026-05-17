# Production Substrate Convergence Design

## Purpose

Wrela's next production milestone should converge four deferred production tracks into one integrated substrate:

- memory and address spaces
- CPU and interrupts
- hardware discovery
- executor runtime

These should be designed and implemented together because they now depend on each other. Drivers cannot become production-shaped until they can allocate bounded memory, claim discovered hardware, receive timer and interrupt events, place executors on discovered CPUs, and report their memory footprint without relying on QEMU-specific constants or ad hoc buffers.

The goal is not to add more device drivers in this milestone. The goal is to make adding drivers after this milestone mostly a matter of:

- claiming discovered device authority
- allocating bounded physical buffers
- wiring interrupts and timers
- publishing typed events to executors
- exposing a driver-owned API without re-solving memory, topology, or wakeup policy

## Current State

The current compiler and Wrela platform source have proven the core end-to-end path:

- UEFI boot into generated x86_64 code
- explicit vCPU placement and AP startup on QEMU lab hardware
- SPMC topics with compiler-planned cache-line layout
- COM1 receive through IOAPIC
- EDU MSI and ivshmem MSI-X interrupt paths
- source-visible UEFI, ACPI, MADT, MCFG, PCI BAR, MSI, and MSI-X discovery authorities
- required hardware boot-fatal paths

This is enough to validate the architecture direction, but not enough for production. The remaining gap is the shared substrate that real drivers and larger images need: deterministic memory authority, richer discovered topology facts, production timer and interrupt delivery, and executor placement/wakeup decisions derived from those facts.

## Why One Plan

These tracks should not be split into independent plans.

Memory policy is needed by interrupt queues, topic buffers, executor stacks, AP startup records, cache memory, MMIO views, and future DMA buffers.

CPU and interrupt policy needs discovered APIC topology, timer facts, route ownership, interrupt queue memory, and executor wake targets.

Executor runtime policy needs discovered CPU topology, typed topic payload layout, bounded memory budgets, and monitor/mwait feature selection.

Hardware discovery needs to expose facts consumed by memory, interrupt, and executor planning, not just produce a report.

The implementation plan may still be phase-gated, but the acceptance criteria should be integrated. A phase is complete only when the later tracks can consume its output.

## Non-Goals

This milestone does not expose source-level virtual address spaces.

The x86_64 backend may still emit the minimal 2 MiB identity map required by long mode, and may adjust that boot glue as needed for AP startup. Wrela source should still model memory primarily as direct physical region authority.

Out of scope for this milestone:

- source-visible higher-half layout
- source-visible page tables
- source-visible page permissions
- W^X and NX policy
- guard pages
- full IOMMU configuration
- storage, network, or framebuffer drivers
- hidden scheduling, migration, or work stealing
- a general CPU exception/trap runtime

The design must not block those features. Hardware-enforced page permissions, guard pages, DMA/IOMMU policy, and higher-half layout should be future backend artifacts derived from the physical-region authority graph, not incompatible replacements for it.

## Design Principles

### Source Authority First

Production platform facts should remain Wrela values. The compiler may enforce authority rules and emit low-level mechanics, but source should decide what hardware is required, how memory is partitioned, and which executor owns each path.

### Deterministic Placement

Memory, executors, queues, and hardware claims should have deterministic placement. If an image's memory footprint or routing graph changes, the compiler should be able to report what changed.

### Bounded By Default

Memory regions, arena frames, queues, cache slots, topic buffers, AP startup records, and interrupt delivery buffers should have explicit bounds. Unbounded growth should not be a hidden runtime behavior.

### No Hidden Runtime Scheduler

Executors remain explicitly placed on vCPUs. This milestone may improve placement planning and wake selection, but it must not add migration, work stealing, runnable queues, or implicit multiplexing.

### Discovery Feeds Policy

Hardware discovery should produce typed facts and authorities consumed by image wiring. It should not remain a standalone report layer.

### Drivers Come After The Substrate

The output of this milestone should make driver plans smaller. A driver should not need bespoke memory ownership, interrupt fanout, timer handling, or executor wakeup machinery.

## Security Model

This milestone is also the right place to make Wrela's security model explicit.

The emerging model is capability-oriented: hardware, memory, interrupts, timers, and executor resources are authority-bearing values, not ambient global resources. Ordinary source should be able to narrow and consume authorities, but should not be able to forge roots from integers or compiler-known constants.

### Security Goals

This milestone should protect against:

- accidental construction of physical memory, MMIO, PCI, interrupt, timer, or executor authorities
- drivers receiving broader authority than they need
- duplicate ownership of hardware routes, PCI BARs, MSI/MSI-X vectors, timer outputs, or executor slots
- memory lifetime bugs where temporary frame-backed memory escapes
- unbounded queues, cache memory, or executor buffers growing outside the image's declared footprint
- accidental overlap between statically placed arenas or device buffers
- cross-executor aliasing that bypasses explicit topics, queues, or path ownership
- hidden QEMU assumptions becoming trusted production policy

These are compile-time and boot-time safety goals. They are not a claim of full malicious-code isolation.

### Explicit Non-Goals For This Milestone

This milestone does not yet protect against:

- malicious firmware lying in UEFI or ACPI tables
- malicious devices performing unrestricted DMA
- speculative execution attacks or microarchitectural side channels
- full process isolation between mutually distrustful executors
- source-visible page permission policy
- secure boot signing or update provenance
- arbitrary unsafe inline assembly inside trusted platform modules

The design should leave clear hooks for later IOMMU policy, page permissions, W^X/NX, guard pages, signed boot artifacts, and authority audit tools.

### Authority Audit Artifacts

Compiler reports should become security audit artifacts, not just debugging output.

The image report should make these relationships reviewable:

- firmware roots that created memory and hardware authorities
- arena tree and memory ownership by executor, driver, queue, topic, and cache
- MMIO and PCI BAR ownership
- IRQ, MSI, MSI-X, timer, and wake-target ownership
- frame-backed values and proof that they do not escape
- DMA-intended buffers, even if IOMMU enforcement is deferred
- required hardware and placement constraints that can boot-fatal

This report gives future security tooling a stable surface: "who owns what, who can signal whom, and which roots were trusted."

## Track 1: Physical Memory Authority

### Physical Region Authority

Wrela source should model usable memory as physical region authority derived from firmware memory maps.

Representative source shape:

```wrela
let usable = discovery.memory.require_usable_region(
    min_base = 0x200000,
    length = 0x800000,
    align = 4096
)

let root_arena = usable.create_arena(
    identity = ArenaIdentity(label = "boot.root"),
    policy = ArenaPolicy(evict_cache_by_default = true)
)
```

A physical region authority should carry:

- base physical address
- length
- alignment guarantees
- provenance from firmware or a parent arena
- boot-panic authority for bounds failures
- optional report identity

Ordinary source must not be able to forge physical region authorities from integer literals. Trusted platform modules may construct roots from UEFI memory descriptors, and ordinary image code may narrow, split, or allocate from those roots.

### Hierarchical Arenas

Arenas should be hierarchical. A parent arena can create child arenas for executors, topics, interrupt queues, device buffers, AP startup records, or cache memory.

Representative shape:

```wrela
let hello_arena = root_arena.child(
    identity = ArenaIdentity(label = "executor.hello"),
    length = 0x200000,
    align = 4096
)

let serial_queue_arena = root_arena.child(
    identity = ArenaIdentity(label = "irq.serial.queue"),
    length = 0x4000,
    align = 64
)
```

The compiler should reject statically knowable overlapping placements. Dynamic allocation inside an arena should remain monotonic or explicitly scoped. No hidden allocator should be introduced.

### Bounded `with` Frames

Temporary memory should use bounded `with` frames.

Representative shape:

```wrela
with scratch = root_arena.frame(length = 4096, align = 64) {
    let table = scratch.bytes()
    parse_table(bytes = table)
}
```

The checker should reject:

- returning a value that borrows from the frame
- storing a frame-borrowed value into a longer-lived object
- publishing a frame-backed buffer to a topic or interrupt queue
- passing frame-backed memory into a driver path that can outlive the frame

The rule is lifetime-based, not name-based. Aliases of frame-backed values must carry the same lifetime.

### Executor Memory Budgets

Executor memory should be claimed from bounded arenas and tied to `ExecutorSlot`.

Representative shape:

```wrela
let console_slot = hardware.executors.claim(
    identity = SlotIdentity(label = "console")
)

let console_memory = root_arena.executor_memory(
    owner = console_slot,
    length = 0x200000,
    align = 4096
)
```

Each executor should have a compiler-visible memory budget:

- root arena
- stack or stack arena
- topic subscriptions
- path-owned queue memory
- cache memory
- explicitly claimed scratch or durable memory

The image report should include per-executor and whole-image memory footprint.

### Cache Memory Evicts By Default

Cache memory should evict by default. Non-evicting cache behavior should be explicit and bounded.

This prevents cache helpers from becoming hidden permanent allocation paths as examples grow into real drivers.

## Track 2: Discovery Facts For Runtime

Hardware discovery should expand only where the runtime substrate needs facts.

### Multiple ECAM Windows And Bridge Walking

PCI discovery should support:

- multiple MCFG ECAM windows
- bus ranges from each window
- PCI-to-PCI bridge walking
- subordinate bus ranges
- discovered functions across bridged buses

The source-visible output remains a `PciDeviceSet` or successor with bounded capacity. If capacity is exceeded, boot should fail with a PCI discovery panic code rather than silently truncating.

### Runtime-Relevant PCI Facts

PCI facts needed by later drivers and interrupt planning should be exposed:

- vendor/device/class/subclass/prog-if/revision
- BAR type and claimed aperture
- MSI and MSI-X capability presence
- interrupt pin/line when present
- bridge bus ranges

This milestone does not need complete coverage of every PCI capability. It should cover the facts needed for memory, interrupt, queue, and future driver plans.

### Timer And APIC Facts

Discovery should expose timer and APIC facts needed by CPU/interrupt planning:

- local APIC mode availability
- x2APIC capability and fallback
- IOAPIC routing facts
- interrupt source overrides
- CPU APIC IDs and enabled status
- timer source availability in selected priority order

The selected initial timer priority should be explicit in the design and plan. A reasonable order is:

1. local APIC timer when calibration is available
2. HPET if discovered and mapped
3. PIT as a calibration or fallback source

QEMU support may drive the first implementation, but the source shape should not encode QEMU as the platform.

### Topology And Locality Facts

Discovery should expose hardware locality facts that can inform executor placement and memory allocation.

Useful facts include:

- logical CPU ID
- APIC or x2APIC ID
- SMT sibling group when discoverable
- physical core group when discoverable
- package/socket group when discoverable
- last-level cache group when discoverable
- NUMA node when discoverable
- memory-region locality when discoverable
- PCI root complex or device locality when discoverable

Not every platform will expose all of these facts. Missing locality information should degrade to explicit fallback policy, not hidden guesses.

The first implementation can use ACPI and CPUID-derived facts that are already available or straightforward to add. More advanced topology sources can be additive.

### Framebuffer Fact Shape

Framebuffer discovery should produce a fact shape, not a full driver.

Representative shape:

```wrela
data FramebufferInfo {
    base: PhysicalAddress
    length: U64
    width: U32
    height: U32
    stride: U32
    format: U32
}
```

This gives later console and graphics driver plans a discovered authority to consume without pulling framebuffer rendering into this milestone.

### DMA And IOMMU Forward Compatibility

This milestone should not implement full IOMMU policy, but memory authority should distinguish ordinary physical memory from buffers intended for device ownership.

Representative future-compatible shape:

```wrela
let rx_ring = device_arena.dma_buffer(
    owner = nic_device,
    length = 4096,
    align = 4096
)
```

The initial implementation may reject or conservatively model DMA buffers. It should not let drivers invent DMA memory from raw literals.

## Track 3: CPU, Timers, And Interrupt Queues

### Hardware-Derived Multiprocessor Routing

The CPU plan should be derived from discovered CPU topology and APIC IDs, not from static q35 two-vCPU assumptions.

Representative shape:

```wrela
let topology = discovery.cpus.require_min_count(count = 2)
let bootstrap = topology.bootstrap()
let worker_cpu = topology.secondary(index = 0)
```

Executor placement should consume discovered CPU facts:

```wrela
hardware.cpus.place(slot = console_slot, cpu = bootstrap)
hardware.cpus.place(slot = worker_slot, cpu = worker_cpu)
```

The exact API can differ, but the plan must remove static lab hardware assumptions from placement.

### AP Startup Contract

Real-hardware AP startup needs a stricter contract than fixed spin loops.

The plan should choose one of these for this milestone:

- calibrated PIT/TSC delay before SIPI retry decisions
- local APIC timer calibration before AP startup
- a documented low-page-table and trampoline contract that is enforced by the backend

If high-CR3 trampoline support is not implemented in this milestone, the low-page-table contract must be explicit and tested.

### Timer Authority

Timers should be source-visible authority values.

Representative shape:

```wrela
let timer = discovery.timers.require_periodic(
    period_us = 1000,
    target = console_slot
)

let ticks = timer.topic.subscribe(subscriber = console_slot)
```

Timer events should enter the same topic/wake system as device interrupts. There should not be a separate hidden timer callback mechanism.

### Shared Interrupts

The interrupt model should allow shared interrupt lines while preserving explicit claims.

A shared interrupt route should produce a dispatcher or topic fanout that is explicit in source:

```wrela
let irq11 = interrupts.route_shared_irq(irq = 11, vector = InterruptVector(value = 0x43))
let nic_irq = irq11.claim_source(identity = InterruptSourceIdentity(label = "nic0"))
let storage_irq = irq11.claim_source(identity = InterruptSourceIdentity(label = "ahci0"))
```

The exact API can be simpler, but the design must distinguish:

- claiming the hardware vector/route once
- registering multiple software interrupt sources
- dispatching to executor-owned queues

### Interrupt Queues

Interrupt delivery should use bounded queues backed by explicit memory.

Representative shape:

```wrela
let serial_irq_queue = root_arena.interrupt_queue(
    identity = QueueIdentity(label = "irq.serial.rx"),
    owner = console_slot,
    capacity = 64,
    payload = InterruptPayloadKind.u64()
)
```

Queue overflow policy must be explicit. Initial policies can be:

- drop newest
- drop oldest
- set overflow flag and wake
- boot fatal for queues that must never overflow

The default should be conservative and observable. Silent loss is not acceptable for required control paths.

### x2APIC

x2APIC support should be added only with a clean xAPIC fallback.

The runtime should select APIC mode from discovered CPU/APIC facts. Source code should not hardcode one mode unless it explicitly requires that mode and boot-fatals otherwise.

### Interrupt Save Policy

If the compiler emits only integer code, the initial interrupt save policy can save general-purpose state only.

If Wrela begins emitting FPU/SSE/AVX instructions, the interrupt save policy must be upgraded before those instructions can appear in interruptible executor code. The compiler should reject that combination rather than silently corrupting vector state.

## Track 4: Executor Runtime Integration

### Topology-Aware Placement

Executor placement should consume discovered topology and locality facts.

The image should be able to express:

- require at least N CPUs
- place executor on bootstrap CPU
- place executor on a secondary CPU
- prefer separate cores when topology facts exist
- prefer or require separate SMT sibling groups
- prefer same last-level cache for executors with high message traffic
- prefer separate cache groups for executors expected to thrash memory
- prefer an executor near the PCI device or interrupt source it services
- allocate executor memory near the CPU or device that primarily uses it when NUMA facts exist
- boot-fatal when required placement cannot be satisfied

This remains explicit placement, not scheduling.

Representative source shape:

```wrela
let placement = topology.placement()

placement.require_separate_physical_cores(a = console_slot, b = worker_slot)
placement.prefer_same_cache_group(a = parser_slot, b = network_rx_slot)
placement.prefer_near_device(slot = nic_slot, device = nic)

let worker_memory = root_arena.executor_memory_near(
    owner = worker_slot,
    near = placement.cpu_for(slot = worker_slot),
    length = 0x200000,
    align = 4096
)
```

Required constraints should boot-fatal when they cannot be satisfied. Preferred constraints should produce a placement report showing whether they were satisfied and what fallback was used.

The compiler should not pretend to solve arbitrary placement optimally in this milestone. The goal is to make locality facts visible, constraints explicit, and the chosen placement auditable.

### Typed Topic Payloads

Topics should move beyond fixed scalar examples toward generalized payload typing while preserving static layout.

The compiler should know:

- payload size
- payload alignment
- cache-line layout
- queue capacity
- producer owner
- subscriber owners
- wake targets

Payloads should be plain Wrela data with statically known layout. This milestone does not require generics if a smaller concrete shape can prove the model, but the design should not block generic topics later.

### Wake Path Selection

Wake paths should be planned from:

- executor placement
- topic subscriptions
- interrupt queues
- timer events
- CPU monitor/mwait capability

The runtime may use monitor/mwait when discovered and supported. It must have a non-monitor fallback such as pause/hlt plus interrupt wake.

Feature selection should be explicit in the generated plan and reportable for debugging.

### No Hidden Scheduler

This milestone must preserve the current executor model:

- no migration
- no work stealing
- no hidden scheduler thread
- no runnable queue
- no implicit executor multiplexing on a vCPU

Executors can block or wait through explicit topic/timer/interrupt wake paths.

## Integrated Source Shape

The final source does not need to match this exactly, but the implementation should converge on this kind of wiring:

```wrela
phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
    let discovery = PlatformDiscoveryRoot(panic = BootPanic()).from_uefi(hardware = hardware)

    let root_region = discovery.memory.require_usable_region(
        min_base = 0x200000,
        length = 0x1000000,
        align = 4096
    )
    let root_arena = root_region.create_arena(identity = ArenaIdentity(label = "root"))

    let topology = discovery.cpus.require_min_count(count = 2)
    let timers = discovery.timers.require_periodic(period_us = 1000)
    let placement = topology.placement()

    let console_slot = hardware.executors.claim(identity = SlotIdentity(label = "console"))
    let worker_slot = hardware.executors.claim(identity = SlotIdentity(label = "worker"))

    placement.require_separate_physical_cores(a = console_slot, b = worker_slot)

    let console_memory = root_arena.executor_memory(owner = console_slot, length = 0x200000, align = 4096)
    let worker_memory = root_arena.executor_memory_near(
        owner = worker_slot,
        near = placement.cpu_for(slot = worker_slot),
        length = 0x200000,
        align = 4096
    )

    let serial = discovery.serial.require_com1()
    let serial_rx_queue = root_arena.interrupt_queue(
        identity = QueueIdentity(label = "serial.rx"),
        owner = console_slot,
        capacity = 64
    )
    let serial_rx = serial.route_receive(queue = serial_rx_queue)

    let timer_ticks = timers.subscribe(subscriber = worker_slot)

    let console = ConsoleExecutor(
        slot = console_slot,
        memory = console_memory,
        serial = serial,
        serial_rx = serial_rx
    )

    let worker = WorkerExecutor(
        slot = worker_slot,
        memory = worker_memory,
        ticks = timer_ticks
    )

    hardware.cpus.start(cpu = topology.secondary(index = 0), executor = worker)
    hardware.cpus.enter(cpu = topology.bootstrap(), executor = console)
}
```

The important properties are:

- memory roots come from discovery
- child allocations are bounded and named
- interrupts and timers use bounded queues/topics
- executors are placed using discovered topology
- placement constraints and fallbacks are reportable
- hardware routes are claimed once
- the compiler can report the resulting memory and runtime graph

## Compiler Responsibilities

The compiler should enforce:

- no forged physical region, arena, MMIO, PCI, interrupt, timer, or executor authorities
- no frame lifetime escape from bounded `with` frames
- no duplicate hardware claims
- no statically knowable overlapping deterministic memory placements
- executor memory belongs to the executor slot that receives it
- subscriptions belong to the executor slot that receives them
- interrupt queues have bounded capacity and explicit overflow policy
- topic payload layout is statically known
- required topology and locality placement constraints are satisfiable or boot-fatal
- required placement and required hardware paths have boot-fatal behavior

The compiler should report:

- image memory footprint
- per-executor memory footprint
- arena tree
- topic and interrupt queue layout
- executor-to-vCPU placement
- satisfied and unsatisfied preferred placement hints
- required placement constraints and their selected CPUs
- CPU/cache/NUMA/device locality facts used for placement
- selected APIC/timer/wake strategy
- claimed PCI/MMIO/IRQ/MSI/MSI-X resources

## Runtime And Platform Responsibilities

Wrela platform source should implement:

- firmware-derived physical memory roots
- arena narrowing and allocation
- bounded parser/runtime byte views
- discovery fact extraction
- APIC/timer/interrupt authority construction
- bounded interrupt queues
- timer event publication
- executor wake primitives

Backend glue should implement:

- UEFI entry ABI mechanics
- minimal long-mode paging setup
- AP trampoline mechanics
- low-level APIC/x2APIC writes
- interrupt entry/exit assembly
- selected monitor/mwait or fallback wait instructions
- image report emission if reports are generated by the compiler

## Acceptance Criteria

The implementation plan should make these verifiable:

- Booted examples no longer allocate root executor/device memory from raw address literals outside trusted platform construction.
- Physical region and arena authorities cannot be forged in ordinary modules.
- Bounded `with` frame values cannot escape their frame.
- Statically knowable overlapping arena placements are rejected.
- Each booted image has a memory footprint report.
- Root executor memory is bounded and reported.
- Cache memory evicts by default.
- Discovery supports multiple ECAM windows and PCI bridge bus walking.
- Runtime placement consumes discovered CPU/APIC facts rather than static q35 CPU assumptions.
- Discovery exposes available CPU/cache/NUMA/device locality facts with explicit unknown values when firmware does not provide them.
- Required executor placement constraints boot-fatal when unsatisfied.
- Preferred executor placement hints appear in the image report with satisfied/fallback status.
- Executor memory can be allocated near a selected CPU/device when locality facts exist, and falls back deterministically when they do not.
- AP startup uses a calibrated or explicitly documented delay/trampoline contract.
- At least one timer source publishes timer events to an executor subscription.
- Shared interrupt routing is source-visible and preserves single hardware route ownership.
- Interrupt queues are bounded and have explicit overflow policy.
- x2APIC selection has xAPIC fallback or a required-mode boot-fatal path.
- Topic payload layout supports at least one non-U64 data payload with static layout.
- monitor/mwait selection is based on discovered CPU capability with a fallback wait path.
- The executor model still has no hidden scheduler, migration, or work stealing.
- The image report includes an authority audit section covering memory roots, arenas, hardware claims, queues, timers, wake targets, and DMA-intended buffers.
- QEMU e2e covers AP startup, timer wake, shared interrupt routing, MSI, MSI-X, bounded topic payloads, and memory footprint reporting.

## Testing Strategy

The implementation plan should include:

- semantic tests for authority forgery, frame lifetime escape, duplicate claims, overlapping placements, and slot/memory/subscription mismatches
- semantic tests for required placement failures and preferred placement fallback reporting
- source-shape tests for platform APIs and report fields
- codegen tests for AP startup, interrupt entry/exit, timer programming, xAPIC/x2APIC selection, and monitor/mwait fallback
- parser/runtime tests for malformed ACPI/PCI discovery inputs where synthetic testing is possible
- QEMU e2e tests for timer wake, shared interrupts, AP startup, MSI/MSI-X, and executor topic payloads
- scans that prevent reintroduction of q35-only literals and raw booted-image memory placement shortcuts
- report tests that verify the security audit section lists authority roots, narrowed owners, and placement decisions

Hardware-in-loop coverage is valuable, but this plan should not depend on it for every acceptance criterion. QEMU remains the fast correctness loop; hardware runs should validate that the substrate has not encoded QEMU as a platform.

## Sequencing

The executable plan should be phase-gated:

1. Memory authority and reports.
2. Discovery facts needed by runtime planning.
3. CPU, timer, interrupt queue, and AP startup integration.
4. Executor placement, wake path, and typed payload integration.
5. Migration of examples and integrated e2e acceptance.

Even though the work is sequenced, the plan should remain one plan. Each phase should leave source and tests in a state that the later phases consume directly.

## After This Milestone

After this substrate exists, the next plans can focus on drivers:

- framebuffer console
- virtio device family
- AHCI or NVMe block storage
- network devices

Those plans should be smaller because they can rely on discovered device authority, bounded memory allocation, interrupt/timer delivery, executor placement, and topic payload layout as already solved substrate.
