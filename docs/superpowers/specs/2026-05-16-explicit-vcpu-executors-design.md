# Explicit vCPU Executors and SPMC Topics Design

## Purpose

Wrela's next executor step is to move from direct `executor.run()` calls toward explicit multi-vCPU execution without introducing a hidden runtime scheduler.

The design goal is:

- each executor owns one vCPU/core
- executor placement is explicit in source
- communication is explicit publish/subscribe
- wakeups are compiler-planned from the known graph
- device interrupts become normal topic events consumed by executor loops
- topology and cache-line layout are first-class planning inputs

This keeps Wrela aligned with its current direction: static hardware authority, explicit ownership, direct physical memory, and compiler-assisted machine layout.

## Current Problems

The current hello example has several v0 convenience shapes that do not scale to multi-executor execution:

```wrela
let serial_path = SerialConsolePath(owner = hardware.vcpu0, registers = serial_driver.registers)

let hello = HelloWorld(
    memory = hardware.vcpu0.memory,
    interrupts = ApicInterruptController(...),
    pci_interrupts = pci_interrupts,
    serial_path = serial_path,
    edu_path = EduMsiPath(...)
)

hello.run()
```

Problems:

- `owner = hardware.vcpu0` puts executor ownership on a path before the executor exists.
- `memory = hardware.vcpu0.memory` makes memory look vCPU-owned instead of executor-owned.
- root interrupt/Pci/APIC configuration authority is passed into the executor.
- `hello.run()` directly calls the executor start method and does not express vCPU startup.
- interrupt handlers call executor code as hidden control flow.

The new model should make these relationships explicit and derivable:

- passing a path to an executor gives the executor that path capability
- starting an executor on a vCPU gives the compiler executor placement
- memory is claimed for an executor and passed into its constructor
- device paths publish interrupt events to topics
- executor loops consume subscriptions normally

## Core Model

### Driver

A driver owns root hardware authority for a physical device or device function.

Examples:

- COM1 serial registers and IRQ authority
- NIC MMIO BARs, queue registers, MSI-X table entries, DMA rings
- timer hardware
- PCI configuration authority for a specific device

Executors should not receive root drivers.

### Path

A path is a narrowed hardware capability created by a driver.

Examples:

- serial write path
- serial receive path
- NIC RX queue path
- NIC TX queue path
- timer tick path

Path ownership is established by passing the path value into an executor constructor. The path should not carry an `owner = hardware.vcpuN` field.

Path instance identity matters. Two paths of the same type are still distinct capabilities and must have distinct topic identity when they publish events.

### Executor

An executor is:

- a source-declared executor type
- one root `ExecutorMemory` arena
- owned paths and subscriptions
- one explicit vCPU placement
- one start loop

An executor is not scheduled by a hidden runtime. It is explicitly started on a vCPU:

```wrela
let hello_memory = hardware.memory.claim_executor_arena(
    length = 0x200000,
    align = 4096
)

let serial_path = serial_driver.create_console_path()

let hello = HelloWorld(
    memory = hello_memory,
    serial_path = serial_path
)

hardware.vcpu0.enter(executor = hello)
```

For secondary CPUs, `start` means "install this executor context on this vCPU and release that vCPU into the executor start method." Starting a non-current vCPU returns to the caller after the target vCPU has been released or after its startup record has been installed.

For the current bootstrap CPU, starting an executor is a terminal control transfer into that executor's start method. In source, the current-vCPU start must be the last successful action in the phase. A clearer future spelling may split this into:

```wrela
hardware.vcpu1.start(executor = worker)     // release another vCPU and return
hardware.vcpu0.enter(executor = hello)      // enter current vCPU executor, never returns
```

This design allows either spelling, but the semantic distinction must exist.

`start` is a dispatch command, not a runtime scheduler. There is no migration, work stealing, runnable queue, or implicit multiplexing.

### Topic

A topic is a cache-line-aware SPMC stream.

The producer owns the topic. Subscribers hold explicit read capabilities. Topics are the single communication model for:

- executor-to-executor events
- device interrupt events
- timer ticks
- state updates
- command-like messages when paired with an explicit delivery policy

Everything is publish/subscribe, but not every topic has the same delivery policy.

### Subscription

A subscription is a read capability to a topic.

Subscriptions are explicit values passed into executor constructors. The compiler uses the subscription graph to plan:

- topic layout
- subscriber cursors
- waitlines
- wake fanout
- capacity checks
- vCPU placement constraints and warnings

## Delivery Policies

SPMC pub/sub is the universal shape, but delivery policy is explicit.

### Latest

Keeps the latest value. Older values may be overwritten. Good for state snapshots and telemetry.

Guarantee:

- subscriber can observe current state
- intermediate updates may be skipped

### Bounded Gap-Detecting

Uses a bounded ring with monotonically increasing sequence numbers. Producer may overwrite old slots. Subscribers detect gaps when they fall behind.

Guarantee:

- subscriber receives messages while it keeps up
- subscriber detects missed sequence ranges
- producer does not block on slow subscribers

This should be the first implemented policy.

### Reliable Bounded

Retains messages until all required subscribers advance past them, or until an explicit subscriber drop/failure policy applies.

Guarantee:

- at least once delivery within declared subscriber membership
- producer may backpressure, fail publish, or apply an explicit drop policy when retention is exhausted

This is required for command-like protocols where loss is unacceptable.

### Idempotency

Idempotency is a consumer-side protocol layered on top of a delivery policy. It handles duplicate messages, not missing messages.

For at-least-once command behavior, use a reliable bounded topic plus message IDs/idempotency keys.

## Cache-Line Layout

Topics must be laid out to avoid false sharing.

Baseline layout:

- producer sequence on its own cache line
- producer-owned metadata on producer-owned cache lines
- each subscriber cursor on a separate cache line
- each waitline on a separate cache line
- ring slots aligned to cache-line boundaries when message size permits

For small hot messages, prefer fixed-size slots that fit in one cache line or a small integral number of cache lines.

For large payloads, publish descriptors or handles to explicitly owned/shared buffers rather than copying large payloads through the topic ring.

The compiler should know or discover cache line size through the target profile/hardware discovery path.

## Wake Model

Source code expresses waiting on topics/subscriptions. It does not express a specific machine instruction.

The backend chooses the cheapest legal wait implementation.

### Primary: Cache-Line Wait

The preferred x86 implementation is a cache-line wait primitive:

- Intel path: `MONITOR` / `MWAIT`
- AMD path: `MONITOR` / `MWAIT` when exposed, or `MONITORX` / `MWAITX` when exposed

For a single hot subscription, the consumer can monitor the topic's producer sequence cache line directly.

For multiple subscriptions, the compiler may synthesize a per-executor waitline:

```text
producer publishes to topic A
producer stores to consumer waitline
consumer monitors waitline
consumer wakes and drains all subscriptions
```

The waitline store is a wake signal. Its value is not semantically important.

### Fallback: HLT and IPI

If cache-line wait is unavailable or disabled, the backend uses:

- `HLT` for sleeping
- compiler-wired IPI wakeups based on the subscription graph

IPI wakeups must be coalesced. The producer should not send one IPI per publish if a wake is already pending.

### Explicit Loop Safety

Executor loops follow:

```text
drain ready inputs
arm wait sources
re-check ready inputs
sleep if still idle
```

The re-check prevents lost wakeups between "empty" and "sleep."

## vCPU Loop Policies

Each executor owns its loop policy explicitly.

### Hot Poll

Never sleeps. Lowest latency, highest resource use.

Use for dedicated high-rate paths where burning a core is intentional.

### Adaptive

Polls briefly, uses `pause`/backoff, then enters the target wait primitive.

Good default for moderately hot executors.

### Event Sleep

Drains inputs, arms wake sources, re-checks, then sleeps immediately.

Best for mostly idle workers.

### Timer Sleep

Arms subscriptions plus a local deadline timer before sleeping.

Use for periodic work or timeouts.

No core must remain awake by default. If all executors sleep and no timer/device/publisher wake source is armed, the image is quiescent until external hardware wakes it.

The compiler should verify that an executor using a sleep policy has at least one wake source:

- subscription waitline
- IPI fallback path
- device interrupt topic
- timer topic

## Device Interrupts

Device interrupts should not call executor handlers directly.

Instead:

```text
device interrupt fires
generated ISR/driver glue runs
minimal event data is captured
event is published to the path instance's topic
device/APIC is acknowledged as required
target executor is woken through the topic wake path
ISR returns
executor loop later drains the subscription
```

This keeps executor control flow explicit. The executor handles device events from its normal loop.

Interrupt handlers must be tiny and bounded:

- no allocation
- no blocking
- no arbitrary executor calls
- no slow parsing
- no unbounded loops

Interrupt topics usually should not be unconditionally reliable unless the device can be safely masked or backpressured when the topic is full.

Common policies:

- lossy with overflow counter
- coalesced "ready" event
- mask device when ring full
- device-specific backpressure

For devices with multiple hardware queues, each queue path should normally own its own event topic:

```text
NIC RX queue 0 path -> RX topic 0 -> executor 0
NIC RX queue 1 path -> RX topic 1 -> executor 1
```

For devices with one physical interrupt but multiple logical paths, the driver may demux internally into path-specific topics. That demux should be explicit in the driver/module design.

## Memory Model

Memory is executor-owned, not vCPU-owned.

The future shape should move away from:

```wrela
memory = hardware.vcpu0.memory
```

and toward:

```wrela
let worker_memory = hardware.memory.claim_executor_arena(
    length = 0x200000,
    align = 4096
)

let worker = Worker(memory = worker_memory, ...)

hardware.vcpu1.start(executor = worker)
```

The memory planner may place the arena near the target vCPU for topology reasons, but ownership belongs to the executor.

Shared memory across executors is not ambient. Shared regions require explicit capabilities and should be treated as a separate, rare mechanism.

## Topology and Capacity Planning

Wrela should use the whole image graph to produce static capacity checks and placement guidance.

Known inputs:

- number of executors
- vCPU assignments
- topic producer and subscriber graph
- message sizes
- ring depths
- delivery policies
- declared publish rates or burst sizes
- declared subscriber drain cadence or maximum tolerated lag
- cache line size
- SMT/core/cache/NUMA topology when known

The compiler/planner should detect:

- not enough vCPUs for started executors
- more than one executor started on one vCPU
- executor not started
- executor started more than once
- path passed to more than one executor without explicit sharing
- reliable topic without enough retention or backpressure policy
- lossy topic used where the subscriber declares no tolerated gaps
- hot cross-socket producer/subscriber edges
- message slots that span too many cache lines for a hot topic
- ring depth too small for declared bursts

Capacity planning is not a formal performance proof. It is a static physical plausibility check.

## Target Topology Failure

If the target topology is fixed at compile time, insufficient vCPUs is a compile-time error.

Example:

```text
target.vcpus = 2
image starts 3 executors
```

Result:

```text
error: not enough vCPUs: image starts 3 executors but target provides 2
```

No hidden multiplexing fallback is allowed.

If hardware topology is discovered at boot, the image performs the same check during boot planning. If the machine has fewer CPUs than required, boot fails through an explicit failure path.

## Compiler Checks

The compiler should enforce:

- each executor is started exactly once
- each vCPU starts at most one executor
- every started executor has a root memory arena
- executor memory is not shared unless explicitly declared shared
- driver root capabilities are not passed into executors
- path capabilities are owned by the executor that receives them
- path ownership derives from executor constructor flow, not `owner = vcpu`
- topic publisher authority is single-owner
- subscriptions are explicit read capabilities
- interrupt-capable path topics have bounded ISR-safe publish behavior
- sleeping loops have at least one wake source
- waitline/IPI wake fanout is generated only from explicit subscriptions

## Example Shape

Illustrative source direction:

```wrela
phase owned_hardware(hardware: OwnedHardware) -> never {
    let com1 = hardware.io_ports.claim_com1()

    let serial_driver = SerialDriver(
        registers = SerialRegisters(port_base = com1.port_base),
        memory = hardware.memory.claim_driver_region(length = 4096)
    ).initialize()

    let serial_console = serial_driver.create_console_path()

    let producer_topic = Topic(
        message = CounterMessage,
        depth = 1024,
        delivery = bounded_gap_detecting
    )

    let hello_memory = hardware.memory.claim_executor_arena(length = 0x200000, align = 4096)
    let worker_memory = hardware.memory.claim_executor_arena(length = 0x200000, align = 4096)

    let hello = HelloWorld(
        memory = hello_memory,
        serial = serial_console,
        counter_out = producer_topic.publisher()
    )

    let worker = Worker(
        memory = worker_memory,
        counter_in = producer_topic.subscribe()
    )

    hardware.vcpu1.start(executor = worker)
    hardware.vcpu0.enter(executor = hello)
}
```

The compiler derives:

- `hello` is placed on `vcpu0`
- `worker` is placed on `vcpu1`
- `serial_console` is owned by `hello`
- `producer_topic` has one producer and one subscriber
- publishing to `producer_topic` can wake `vcpu1`
- the wake path should use cache-line wait if supported, otherwise IPI fallback

## First Milestone

The first implementation milestone should prove the spine without solving every policy:

- static target with two vCPUs
- two executors, each explicitly started on one vCPU
- executor memory claimed independently from `OwnedHardware.memory`
- one bounded gap-detecting SPMC topic
- one subscription
- cache-line-isolated producer sequence and subscriber cursor
- explicit executor loop using drain, arm, re-check, sleep
- cache-line wait abstraction in source/backend shape
- `HLT + IPI` fallback path
- compiler checks for vCPU count, one executor per vCPU, one start/enter per executor, and terminal current-vCPU entry ordering

Device interrupt topics and reliable bounded topics can follow after the vCPU/topic spine is working, unless interrupt-as-topic is needed to remove the current `on path.interrupt` model first.

## Non-Goals

This design does not include:

- hidden scheduler
- work stealing
- executor migration
- dynamic executor creation
- generic heap allocation
- ambient shared memory
- virtual address-space design
- full ACPI CPU discovery
- complete PCI/MSI-X routing generality
- formal performance proof

## References

- x86-64 psABI v3 feature list: https://gitlab.com/x86-psABIs/x86-64-ABI/-/raw/master/x86-64-ABI/low-level-sys-info.tex
- Intel Software Developer Manuals: https://www.intel.com/content/www/us/en/developer/articles/technical/intel-sdm.html
- Intel MONITOR/UMONITOR performance guidance: https://www.intel.com/content/www/us/en/developer/articles/technical/software-security-guidance/technical-documentation/monitor-umonitor-performance-guidance.html
- AMD CPUID Specification: https://www.amd.com/content/dam/amd/en/documents/archived-tech-docs/design-guides/25481.pdf
