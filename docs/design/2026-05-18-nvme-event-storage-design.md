# NVMe Event Storage Design

## Purpose

Wrela's first storage milestone should not be a traditional filesystem with a
database layered on top. The storage primitive should be a direct NVMe-backed
event store with encrypted blob storage and rebuildable projections.

The goal is to make durable application state feel native to the platform:

- commands append durable semantic events
- entity streams are addressed by cheap store-assigned integer IDs
- file and large object bytes live in encrypted blob extents
- projections store read models in the physical shape their queries need
- background maintenance repairs and improves storage layout without changing
  the event truth

This milestone should prove the whole storage shape with the smallest useful
NVMe driver and event-store surface. It should not try to build a general POSIX
filesystem, a SQL engine, a full query planner, or a universal logging system.

## Current Context

The production substrate already establishes the ingredients that storage needs:

- PCI discovery and PCI BAR authority
- MSI/MSI-X interrupt routes and driver-path interrupt receivers
- physical memory authority and DMA-intended buffers
- bounded executor memory and explicit executor placement
- permanent executor placement on separate physical cores
- monitor/mwait-capable wake paths with IPI wake fallback
- topics, queues, and timers for later async integration
- upcoming language support for generics, enums, `Result<T, E>`,
  `Option<T>`, typed slices, typed slots, constants, and pattern matching

The storage design assumes that the language expressiveness milestone lands
first. The storage implementation should rely on typed DMA buffers, explicit
results, typed memory views, and compile-time constants rather than adding
ad hoc storage-specific language machinery.

## Design Principles

### Events Are Truth

The event log is the durable source of truth. Stream checkpoints, projection
checkpoints, directory pages, blob allocator state, and indexes are repairable
acceleration structures. If derived state disagrees with the event log, derived
state loses.

### Storage Is Not Ambient Logging

The event log is for durable user and system actions. It is not the place for
high-volume telemetry, tracing, IO completions, scheduler activity, or debug
messages. Those can use separate rolling buffers or diagnostic regions later.

This keeps the event log small enough that fixed-size event slots are a
reasonable tradeoff.

### One Event Is One Durable Storage Quantum

The hot event format uses fixed 512-byte slots. A 512-byte event aligns with
the smallest common NVMe logical block size. On a 4 KiB namespace, eight hot
event slots pack into one logical block.

Packing does not mean later rewriting an acknowledged logical block. If a
durable batch underfills a 4 KiB LBA, the remaining slots in that LBA are
sealed as reserved empty slots. Those empty slots consume event ID positions so
direct `event_id -> LBA` arithmetic remains true. The writer never acknowledges
one slot and later rewrites the same LBA to fill another slot.

This spends storage to buy simple arithmetic lookup, direct replay, simple
recovery, and low CPU overhead.

### Hot And Cold Formats Are Different

The 512-byte slot is the foreground commit format, not necessarily the forever
storage format. Recent events stay in the hot fixed-slot region. Old sealed
segments can be packed and compressed by maintenance without changing stable
event IDs.

This keeps append and recovery simple while avoiding unbounded storage waste.
Compression preserves history, so destructive event truncation is not part of
the v1 design.

### Interrupts Are The Completion Path

NVMe command completion should use Wrela's existing interrupt receiver
machinery from the first driver. The driver may perform bounded controller-ready
waits during reset and enable, but admin and IO command completion is
interrupt-driven, not polled.

### Truth Has One Writer

The foreground/app core owns the single `StorageWriter` authority. It assigns
event IDs, advances the durable frontier, mutates stream heads, publishes blob
and projection roots, and accepts or rejects background proposals.

Single writer does not mean single storage worker. Expensive storage work runs
on permanently isolated background cores and returns small commit proposals to
the writer.

### Workloads Are Permanently Isolated

Core assignment is an ownership boundary, not a temporary scheduling hint. The
desktop shape is a small set of long-lived core workloads that communicate
through typed queues and explicit wake paths. Work should not migrate between
cores to chase load.

### Blobs Are The Data Plane

Large or sensitive bytes do not belong in the event log. File contents,
attachments, large event payloads, names that need privacy, snapshots, and
projection state blobs live in the blob store.

The blob store is encrypted by default and supports copy-on-write relocation,
orphan collection, extent reclamation, and later defragmentation.

### Projections Are Durable Layouts

A projection is not a reactive logic object and not a table plus generic
indexes. A projection declaration is durable ABI for derived state: projection
ID, layout IDs, owned containers, root shape, and upcast paths.

Maintenance workers own projection behavior. They are ordinary imperative code
running on the maintenance core: subscribe to committed atomic groups, read
events, mutate projection containers copy-on-write, and propose new roots.

## Non-Goals

This milestone does not add:

- POSIX filesystem semantics
- SQL
- a universal query engine or query optimizer
- general-purpose secondary indexes
- automatic schema migration or mandatory upcasting
- multi-writer or sharded event log semantics
- destructive event truncation or retention deletion
- secure erase guarantees for event metadata
- full-disk encryption
- IOMMU enforcement
- hot-plug NVMe support
- multi-controller striping or mirroring
- a production async scheduler
- computational storage or in-drive execution

Except for computational storage, the design should not block those future
features. In-drive execution is intentionally out of scope; Wrela treats the
NVMe controller as storage, not as an application or projection runtime.

## Consistency Model

The storage layer needs a small set of laws before the physical layout matters:

```text
event append acknowledged:
  every acknowledged atomic event group is durable and recoverable

atomic group visible:
  every semantic event in the group is visible, or none are visible

blob commit acknowledged:
  blob bytes, extent metadata, and key metadata are durable enough for any
  later event that references the blob

projection root visible:
  projection is queryable through its advertised event watermark

command accepted:
  all events produced by the command are one durable atomic group, or none are
  acknowledged

after crash:
  acknowledged events are recoverable, unacknowledged events may disappear,
  derived state may lag, but derived state must not claim a false watermark
```

The durable atomic-group frontier is the truth boundary. Durable event slots
that do not form a complete valid group are bytes, not facts. Stream
directories, projection roots, blob allocator summaries, compressed segment
maps, and checkpoint records become visible only through writer-published roots
or writer-accepted proposals.

The frontier is a hot slot frontier. On 4 KiB namespaces it may advance across
reserved empty slots created by underfilled durable batches. Those slots
preserve addressing math but do not represent semantic events.

Every read model root carries a watermark. A file handle, directory listing,
search result, timeline chunk, or projection query should be able to report the
latest `event_id` it reflects. Read-your-write means waiting for the relevant
root to reach at least the caller's accepted atomic group.

## Performance Ambition

Wrela storage should compete directly with dedicated event stores by removing
the filesystem/database boundary. The v1 target is not feature parity with
distributed log systems. It is proving that direct NVMe event append,
foreground/background driver paths, and native background projections can beat
general-purpose stacks for local durable state.

Durable append targets:

```text
baseline target: 100k durable events/sec
strong target:   500k durable events/sec
stretch target:  1M+ durable events/sec
hero target:     multiple M events/sec with large group commit
```

At 512 bytes per hot event, 1M events/sec is about 512 MB/sec of append
bandwidth before metadata and flush overhead. Sustained for a full day, that is
about 44 TB/day of host writes before SSD-internal write amplification. That is
a benchmark and upper-bound design target, not an assumption that normal
desktop workloads should write at that rate forever.

The limiting factors should be flush cadence, writer CPU work, NVMe bandwidth,
and write-endurance budget, not generic serialization, heap allocation, or
synchronous projection/index maintenance.

Comparable event stores and streaming logs usually regain throughput through
batching, asynchronous projections/index maintenance, chunked append files,
background compaction or index merging, and sharding when one global order is
too expensive. Wrela should use the same good ideas while cutting out the
application/database/filesystem boundary for local storage.

These targets assume local single-writer durability, no synchronous projection
updates, no network replication, no generic secondary indexes on append, and no
filesystem indirection. Adding those semantics later should be measured as
explicit costs.

### Endurance Budget

The fixed 512-byte hot slot spends write endurance before it spends capacity.
The storage engine must report:

```text
host bytes written per durable event
events per committed LBA
underfilled committed LBA count
foreground write amplification from slot padding
device-reported media writes when available
estimated drive writes per day at current rate
```

Cold compression saves retained capacity and later reads; it does not undo the
initial hot write. If measured workloads mostly emit 80-140 byte events and
underfill many 4 KiB LBAs, the design should prefer one of these before raising
the sustained throughput target:

- use a 512-byte active LBA namespace when hardware and deployment allow it
- increase group commit fill under bursty workloads
- add the deferred sparse sync spill log
- rely on ZNS-capable hardware for the storage role when available

### Latency Budget

Group commit is the latency story. The writer should make batch policy explicit
instead of hiding it behind "fast append":

```text
target batch size
maximum group-commit timer
p50 append-ack latency
p99 append-ack latency
p99.9 append-ack latency
foreground IO queue depth
background interference time
```

At high event rates, doorbell and completion overhead must be amortized across
batches. At low event rates, the group-commit timer and underfilled durable
blocks dominate latency and endurance.

## Core Workload Model

The target desktop shape uses permanent isolated workloads:

```text
foreground/app/display core:
  business logic
  display
  StorageWriter
  ForegroundStoragePath

networking core:
  NIC driver
  protocol state
  packet queues

maintenance/background core:
  projection workers
  blob relocation and defragmentation
  orphan scanning
  checkpoint rebuild
  BackgroundStoragePath

AI inference core:
  model runtime
  tensor memory / accelerator work
  model and cache loading paths
```

The foreground core owns truth because user actions and display benefit from
locality. The maintenance core owns effort because projections, checkpoint
rebuilds, blob relocation, orphan scanning, and verification can be expensive.

The `StorageWriter` hot path stays small:

```text
assign event IDs
append event batches
track foreground NVMe durability completion
advance atomic_group_frontier
publish committed atomic groups
mutate stream directory heads
publish blob/projection/checkpoint roots
accept or reject maintenance proposals
```

It must not run expensive maintenance loops:

```text
no blob copy loops
no orphan scans
no projection rebuild loops
no large hash/verify passes
no walking giant derived structures
```

### Paired Core Links

SPMC topics are useful for broadcast and loose observation. Tightly coupled
permanent core pairs need a smaller primitive: a typed paired-core link.

```text
CoreLink<A, B>:
  A -> B SPSC descriptor ring
  B -> A SPSC descriptor ring
  credits / backpressure
  wait lines using the existing monitor/mwait wake strategy
  IPI wake fallback for sleeping peers
```

Messages are small descriptors, not bulk payloads. Event bytes, blobs, and
projection chunks stay in NVMe regions or core-local caches and are read through
the receiver's own storage path.

Send/receive shape:

```text
send:
  write descriptor into peer ring
  release-store ring tail
  if peer wait is armed, wake peer through the existing IPI path

receive:
  drain available descriptors
  if empty, arm wait
  recheck once
  sleep using monitor/mwait when available, with sti/hlt fallback
```

The foreground/maintenance link carries:

```text
ForegroundToMaintenance:
  DirectoryProjectionGroups:
    CommittedAtomicGroup(
      first_event_id,
      last_event_id,
      slot_range_ref,
      semantic_event_count,
      affected_streams_ref
    )

  OtherProjectionOrMaintenanceFeed:
    CommittedAtomicGroup(...)

  MaintenanceControl:
    ProjectionInvalidated(projection_id)
    MaintenanceBudgetChanged(budget)

MaintenanceToForeground:
  AdvanceProjection(projection_id, through_event_id, root_refs)
  InstallCheckpoint(stream_id, through_sequence, checkpoint_ref)
  RelocateBlob(blob_id, old_ref, new_ref, observed_version)
  ReclaimExtents(extents, reason)
  MaintenanceHealth(status)
```

Each named feed is a boot-wired SPSC descriptor ring with one producer and one
consumer. There is no ambient event bus and no projection auto-registration.
The boot image decides which maintenance worker owns which feed.

Representative boot wiring:

```wrela
core foreground {
    writer: StorageWriter
    directory_groups_out: SpscProducer<CommittedAtomicGroup>
}

core maintenance {
    directory_groups_in: SpscConsumer<CommittedAtomicGroup>
    directory_projection: DirectoryProjectionWorker(
        source = directory_groups_in,
        projection = DirectoryChildren
    )
}

connect foreground.directory_groups_out -> maintenance.directory_groups_in
```

`StorageWriter` routes committed group descriptors into the explicitly connected
producer. The maintenance worker chooses which events in the group it cares
about by normal imperative code.

A feed can receive all committed groups or a boot-declared event-type filter.
That filter belongs to the core wiring, not to hidden projection registration.

Descriptor shape:

```text
CommittedAtomicGroup(
    first_event_id,
    last_event_id,
    slot_range_ref,
    semantic_event_count,
    event_type_summary_ref,
    affected_streams_ref
)
```

Foreground messages publish truth. Maintenance messages propose derived-state
updates. Only the foreground `StorageWriter` commits truth.

## NVMe Driver Scope

### Hardware Claim

The storage stack claims an NVMe PCI function discovered through the existing
PCI authority model. The device match should use the NVMe class shape:

- base class `0x01` for mass storage
- subclass `0x08` for non-volatile memory
- programming interface `0x02` for NVM Express

The driver claims BAR0 as the controller MMIO region, enables PCI memory space
and bus mastering, and allocates controller-owned DMA buffers from explicit
DMA-capable memory authority. It also claims an MSI-X route when available, or
an MSI route if that is the only supported interrupt shape.

### Driver And Driver Path Shape

The NVMe device should follow the same driver and driver path scheme as the
existing serial, EDU, and ivshmem drivers. The event store should consume an
NVMe IO path capability, not a generic free-standing block device class.

Representative shape:

```wrela
data NvmeNamespace {
    namespace_id: U32
    logical_block_size: U64
    block_count: U64
    zone_size_blocks: U64
    supports_zns: Bool
    supports_fua: Bool
    atomic_write_unit_blocks: U32
    power_fail_atomic_write_unit_blocks: U32
}

data NvmeCompletionEntry {
    command_id: U16
    status: U16
    result: U64
}

data NvmeSubmission {
    command_id: U16
}

data NvmeCompletionInterrupt {
    queue_id: U16
    completed_count: U16
}

data NvmePathRole {
    role: U64
}

unique driver NvmeDriver {
    registers: NvmeControllerRegisters
    memory: DriverMemory
    namespace: NvmeNamespace

    fn initialize(self, device: PciDevice) -> NvmeDriver
    fn create_io_path(
        self,
        identity: PathIdentity,
        owner: ExecutorSlot,
        role: NvmePathRole,
        route: PciInterruptRoute,
        irq: TopicPublisher<NvmeCompletionInterrupt>
    ) -> NvmeIoPath
}

driver path NvmeIoPath {
    identity: PathIdentity
    owner: ExecutorSlot
    role: NvmePathRole
    registers: NvmeControllerRegisters
    submission: NvmeSubmissionQueue
    completion: NvmeCompletionQueue
    route: PciInterruptRoute
    irq: TopicPublisher<NvmeCompletionInterrupt>

    interrupt receiver -> NvmeCompletionInterrupt

    fn submit_read(
        self,
        namespace_id: U32,
        start_lba: U64,
        block_count: U64,
        into: DmaBuffer<U8>
    ) -> NvmeSubmission

    fn submit_write(
        self,
        namespace_id: U32,
        start_lba: U64,
        block_count: U64,
        from: DmaBuffer<U8>
    ) -> NvmeSubmission

    fn submit_flush(self, namespace_id: U32) -> NvmeSubmission
    fn submit_zone_append(
        self,
        namespace_id: U32,
        zone_start_lba: U64,
        block_count: U64,
        from: DmaBuffer<U8>
    ) -> NvmeSubmission
    fn ack_completed(self, event: NvmeCompletionInterrupt)
}
```

One path owns one queue pair and its interrupt route. The interrupt receiver
drains completed entries into a path-owned completion buffer, advances the
completion queue head, and publishes a typed completion event. Higher storage
layers drain those entries, correlate them by command ID, and decide when an
append batch is durable.

The first storage image should create separate paths for foreground and
maintenance work:

```text
ForegroundStoragePath:
  owner = foreground/app/display executor
  event-log writes
  durability/group commit
  hot foreground reads

BackgroundStoragePath:
  owner = maintenance/background executor
  projection storage reads/writes
  blob relocation IO
  orphan scan IO
  checkpoint rebuild IO
```

Multiple NVMe paths do not imply multiple event writers. They isolate hardware
queues, interrupt handling, DMA buffers, and backpressure so maintenance IO does
not poison foreground append latency.

### Controller Initialization

The first driver should implement the minimum NVMe path needed for block reads
and writes:

- read controller capability and version registers
- reset or disable the controller before queue setup
- allocate and initialize admin submission and completion queues
- program `AQA`, `ASQ`, and `ACQ`
- configure interrupt delivery for admin completions
- enable the controller and wait for ready
- submit Identify Controller and Identify Namespace commands
- read the active namespace LBA format and logical block size
- detect conventional namespace vs Zoned Namespace support
- detect usable durability features such as FUA, volatile write cache behavior,
  atomic write units, and power-fail atomic write units
- create foreground and background IO submission/completion queue pairs
- configure one completion interrupt vector per IO path
- submit read, write, flush, zone append when supported, and identify-style
  commands

Controller-ready waits are acceptable during initialization. Command completion
is not a polling path.

### Durability Modes

The storage writer chooses the strongest simple durability mode exposed by the
namespace:

```text
preferred:
  batch fits inside the namespace power-fail atomic write unit
  command form is durable on completion through FUA or non-volatile cache
  acknowledge on successful durable completion

fallback:
  write batch
  issue the required flush or FUA-backed durability sequence
  acknowledge only after the durability command completes
```

The implementation must account for volatile write cache, completion ordering,
and torn-write behavior reported by Identify data. A write completion alone is
not enough unless the namespace contract says the data is already durable for
the chosen command form.

Doorbells are batch-level, not event-level. A group commit should prepare all
submissions for the batch, ring the foreground queue doorbell once when
possible, and acknowledge only after the batch's durability condition is met.

### LBA Size

The event layer prefers a 512-byte active namespace LBA because one event slot
then maps to one LBA. The driver must still support 4 KiB LBAs by packing eight
512-byte slots into one LBA within a single durability batch.

The first implementation must not assume that a production namespace can be
reformatted. In QEMU tests, the namespace may be configured with a 512-byte LBA
to exercise the ideal path. On real hardware, the driver uses the active LBA
format reported by Identify Namespace.

On a 4 KiB namespace, an acknowledged LBA is immutable. If the writer commits
one event in a 4 KiB block, the other seven 512-byte slots are sealed empty and
may be reclaimed only after cold compression rewrites the sealed segment.

### Namespace Mode

Most ordinary NVMe devices should be treated as conventional namespaces. When a
device exposes a usable Zoned Namespace, Wrela should target it for the event
log because the segment lifecycle maps cleanly onto zones:

```text
OpenHotSegment:
  open zone

append:
  sequential zone write or zone append

SegmentSealed:
  finish zone

reclaim fixed hot copy after compression:
  reset zone
```

ZNS is an optimization and deployment preference, not a v1 requirement. The
same event and segment abstractions must run on a conventional namespace with
ordinary reads, writes, flushes, and copy-on-write segment-map updates.

### DMA And Transfer Shape

The NVMe layer owns DMA buffer setup and PRP construction. The first driver can
conservatively restrict transfers to bounded, aligned DMA buffers that fit the
implemented PRP path. Larger blob transfers can be split into multiple NVMe
commands.

SGL support is planned but not required for the first milestone. PRP-only
bounded transfers keep the first driver small; SGLs become useful once blob IO,
projection containers, and compressed segment reads need high-IOPS
scatter/gather without copying.

The foreground `StorageWriter` submits event-log IO through
`ForegroundStoragePath`. Maintenance workers submit derived-state and blob IO
through `BackgroundStoragePath`. Both receive completion events through the
existing interrupt/topic machinery. Neither should spin on a completion
register or depend on a polling helper.

The public storage abstraction should not expose arbitrary user block writes as
the main programming model. Block IO is the private backend for events, blobs,
checkpoints, and projection storage.

## Disk Regions

The storage image is divided into explicit regions:

```text
superblock / region map
hot event slot region
event segment map region
sealed event segment extent region
stream directory region
blob extent region
blob manifest / key metadata region
projection checkpoint and projection storage region
maintenance metadata region
```

The region map records offsets, sizes, format versions, active LBA size, event
slot size, and recovery state. Region layout should be deterministic and
auditable.

Superblock and region-map updates must be atomic enough for crash recovery. The
first format should use double-buffered superblocks with:

```text
store_uuid
format_version
generation
active_namespace_mode
active_lba_size
root pointers for region map and segment map
atomic group frontier
checksum
```

Recovery picks the highest valid generation. Later versions can replace or
augment this with a tiny metadata event log, but v1 should not rely on a single
mutable superblock write.

## Event Log

### Event Slot

The hot event log stores fixed 512-byte event slots.

Representative slot fields:

```text
event_id
event_type_id
payload_layout_id
stream_id
stream_sequence
atomic_group_len
atomic_group_index
payload_length
checksum32
payload
```

No magic field is required in the hot event header. Recovery uses sequential
`event_id`, `event_type_id`, and `checksum32` to identify the valid prefix.
`event_type_id = 0` is reserved for an empty committed slot. Empty slots may
exist only as padding inside an acknowledged underfilled LBA and are never
delivered to streams or projections. A committed empty slot still stores the
expected `event_id` and checksum so recovery can distinguish intentional
padding from a torn tail.

The inline payload budget is whatever remains after the fixed header. If the
payload does not fit, the event stores a blob reference and the payload bytes
live in encrypted blob storage.

The event encoder pads the whole `header + payload` layout to exactly 512
bytes. Unused payload bytes are zeroed.

`checksum32` is a cheap hot-path integrity field, not a cryptographic
guarantee. Use CRC32C when hardware support exists; otherwise use a fast
non-cryptographic checksum. Sealed compressed segments add their own segment
checksum over the packed/compressed bytes.

The hot slot stores compact IDs, not full hashes. `payload_layout_id` is a
developer-authored small integer scoped to the event type. The pair
`(event_type_id, payload_layout_id)` identifies the payload byte layout for
compiled user code. Storage preserves the ID and payload bytes but never parses
payload fields.

`event_type_id` is also durable ABI, not declaration-order compiler output. The
compiler must not renumber event types because source order changed. V1 should
require a stable event ID in source or an equivalent compiler-maintained source
constant that is reviewed like any other durable ABI change.

### Event IDs

`event_id` is a store-assigned sequential `U64` for hot slot positions. While a
slot is in the hot fixed-slot region, it gives direct physical addressing:

```text
slot_size = 512
slots_per_lba = active_lba_size / slot_size
lba = event_region_base + event_id / slots_per_lba
slot = event_id % slots_per_lba
```

For a 512-byte LBA, one event is one LBA. For a 4 KiB LBA, one LBA contains
eight event slots. If a 4 KiB batch underfills, unused slots are reserved empty
positions and `next_event_id` advances past them. Once a sealed segment is
compressed, `event_id` remains stable but physical lookup goes through the
segment map.

Semantic event streams ignore reserved empty slots. A durable range can contain
fewer semantic events than slot positions; projection readers skip
`event_type_id = 0`.

Events emitted by one command form one atomic group. Group events receive
contiguous event IDs. `atomic_group_len` records the number of semantic events
in the group, and `atomic_group_index` records the event's zero-based position
inside the group:

```text
single-event command:
  atomic_group_len = 1
  atomic_group_index = 0

two-event command:
  first event:  atomic_group_len = 2, atomic_group_index = 0
  second event: atomic_group_len = 2, atomic_group_index = 1
```

Reserved empty slots created by 4 KiB underfill are not part of any atomic
group.

V1 chooses one global event ID order because it gives simple recovery,
snapshots, projection watermarks, and deterministic replay. The semantic event
identity is still `(stream_id, stream_sequence)`. Storing both leaves room for
future per-stream or causal-merge designs without changing event payloads.

### Durability Contract

Append success means every event slot in the command's atomic group is durable
on NVMe. The event store may use a small group-commit window:

```text
append request enters pending batch
batch fills or timer expires
write pending LBA or LBAs, using power-fail atomic write when available
issue flush, FUA, or the required durability command sequence
acknowledge all atomic groups in the durable batch
```

The system must not acknowledge an event that only exists in RAM. It also must
not expose a partial atomic group as semantic truth.

Low-volume workloads may underfill 4 KiB LBAs. That is an accepted v1 tradeoff
for honest durability. The cost is bounded by immutability: once any slot in a
4 KiB LBA is acknowledged, the rest of that LBA is sealed with reserved empty
slots, those event ID positions are consumed, and the LBA is no longer
available for later hot appends. Metrics should make the cost visible.

### Atomic Groups And Batch Overflow

An atomic group is the semantic visibility unit. It may contain one event or
many events, including events across multiple streams. The writer must not split
one atomic group across acknowledged durable batches.

Batch size is a scheduling policy, not a semantic boundary:

```text
target_batch_slots:
  soft performance target

max_overflow_slots:
  extra slots allowed so a group can finish in the current batch

max_batch_slots:
  hard foreground latency and DMA-buffer cap

max_atomic_group_slots:
  largest command/transaction v1 accepts
```

Representative enqueue policy:

```text
enqueue_atomic_group(group):
  if group.slots > max_atomic_group_slots:
    reject TransactionTooLarge

  if open_batch.slots + group.slots <= target_batch_slots:
    add group to open batch
    return

  if open_batch.slots + group.slots <= max_batch_slots:
    add group using overflow allowance
    flush batch after group
    return

  flush current open batch
  start new batch with group

  if group.slots >= target_batch_slots:
    flush batch after group
```

For example, if the target batch is 64 slots and a two-event command arrives
when the open batch already has 63 slots, the writer may commit 65 slots
together and flush after the two-event group. That avoids a cliff where the
target size accidentally splits an atomic command.

Crash behavior:

```text
complete group durable:
  whole group visible

partial group at tail:
  no event in the group is visible

group length/index invalid:
  recovery stops before the group
```

Large transactions need a hard cap. V1 should reject groups that exceed
`max_atomic_group_slots` rather than silently blowing the foreground latency
budget.

### Recovery

Hot recovery scans event slots in order and validates atomic groups. Scanning
stops at the first invalid tail:

- checksum32 mismatch
- unexpected event ID
- torn or incomplete block
- reserved empty event_type_id where the committed LBA padding rules do not
  allow it
- incomplete atomic group
- mismatched atomic_group_len or atomic_group_index

The valid atomic-group prefix plus durable segment map is the event truth. Later
maintenance can reclaim any data that is not reachable from that truth.

### Segment Lifecycle

The hot log is divided into mechanical segments:

```text
OpenHotSegment:
  writer is appending here

SealedHotSegment:
  fixed 512-byte slots, no future append can land here

CompressibleSegment:
  sealed, outside the hot window, not pinned, maintenance budget available

CompressedSegment:
  packed/compressed bytes, stable event IDs preserved through segment map
```

The writer seals segments by committing exact event ranges:

```text
SegmentSealed {
    segment_id
    first_event_id
    last_event_id
    fixed_slot_ref
    checksum
}
```

Because the log is append-only, a sealed segment is closed forever. Maintenance
compresses only explicit sealed segments, not arbitrary partial ranges behind
`next_event_id`.

On a ZNS namespace, a hot segment should normally be one zone or an integral
number of zones. Segment sealing maps to finishing the zone, and reclaim after
successful compression maps to resetting the old zone. On a conventional
namespace, sealing is represented by segment metadata and copy-on-write segment
map updates.

### Cold Segment Compression

Cold compression replaces truncation for v1. The active hot segment and the
most recent sealed segments keep direct arithmetic lookup. Older sealed
segments can move to a segment-map lookup:

```text
hot event:
  event_id -> LBA math -> fixed 512-byte slot

cold event:
  event_id -> segment map -> compressed extent -> packed event decode
```

The lab alignment strategy remains true for the hot append path. Hot events are
512-byte aligned slots. Sealed compressed segments are extent-aligned rather
than event-aligned and pay a segment-map lookup only after they are cold.

Compression flow:

```text
A. writer commits SegmentSealed for a fixed-slot range
B. maintenance reads fixed slots through BackgroundStoragePath
C. maintenance strips zero padding and packs header + payload bytes
D. maintenance builds a segment-local index
E. maintenance compresses packed bytes
F. maintenance writes compressed bytes and index to sealed extents
G. maintenance proposes SegmentCompressed
H. StorageWriter validates and publishes segment map update
I. old fixed-slot extents become reclaimable
```

Crash behavior follows the same copy-on-write rule as blob relocation. Before
H, the compressed segment is an orphan and hot fixed slots remain truth. After
H but before I, both copies exist and the segment map points to compressed
truth.

Representative metadata:

```text
EventSegment {
    first_event_id
    last_event_id
    encoding
    compressed_ref
    compressed_bytes
    uncompressed_bytes
    index_stride
    index_ref
    checksum
}

SegmentIndexEntry {
    event_id_delta
    uncompressed_offset
}
```

The first implementation can use a no-op codec or simple packed-only codec
behind the final API. The storage shape should still be the final shape:
fixed-slot hot segments and segment-map-addressed sealed segments.

Expected cold density:

```text
hot slot:                 512 bytes/event
packed canonical event:    80-140 bytes/event typical
compressed event:          64-128 bytes/event typical
typical savings:            4x-8x
normal range:               4x-12x
bad hash/blob-ref-heavy:     2x-4x
```

Large, random, encrypted, or already-compressed bytes should live in blobs, not
inline event payloads. That keeps event segments structured and compressible.

### Cold Eligibility

Coldness should be mechanical, not clever. A segment is eligible for
compression when it is:

```text
sealed
outside the configured hot window
not pinned
maintenance budget is available
```

The hot window can combine event count, byte count, and time:

```text
keep last N events hot
keep last M raw bytes hot
keep last T time hot
never compress the active segment
never compress the last K sealed segments
```

Pins prevent compression while old events need fast direct access:

```text
PinEventRange(reason, first_event_id, last_event_id)
UnpinEventRange(pin_id)
```

Common pin reasons include debug sessions, export, audit, rebuild, benchmarks,
and application retention policies. Checkpoint coverage is a performance hint,
not a correctness gate, because compression preserves the old events.

## Streams

### Sequential Stream IDs

`stream_id` is a store-assigned sequential `U64`. This makes stream existence
and lookup cheap:

```text
exists if stream_id < next_stream_id
entry address = stream_directory_base + stream_id * sizeof(StreamEntry)
```

There is no hash table or binary search in the primary stream lookup path.

### Stream Directory

The stream directory stores one compact entry per stream:

```text
latest_sequence
latest_event_id
latest_checkpoint_ref
flags
```

The directory may be cached in fixed contiguous chunks. A chunk is just a group
of adjacent stream entries moved between NVMe and RAM together. The lookup math
remains direct:

```text
chunk_id = stream_id / entries_per_chunk
slot = stream_id % entries_per_chunk
```

For small systems, the directory can be fully memory-resident. For larger
stores, a bounded chunk cache with a simple clock replacement policy is enough.

### Append Flow

Appending to an existing stream:

```text
load StreamEntry by direct index
check expected sequence
allocate next event ID
encode 512-byte slot
write event slot
write durable atomic group
update stream directory entry after durable atomic group
```

Creating a new stream:

```text
stream_id = next_stream_id
next_stream_id = next_stream_id + 1
initialize StreamEntry
append first event with sequence 1
```

The stream directory is acceleration. If it is stale or damaged, the event log
can rebuild it.

## Commands

Commands are intent. Events are facts.

The first command flow is:

```text
load stream state from latest checkpoint plus tail replay
validate command against state
append zero or more events with expected stream sequence
encode one atomic group for all events emitted by the command
acknowledge only after the atomic group is durable
```

Durable command inboxes, idempotency records, and command replay are future
features. V1 commands may be ephemeral as long as accepted events are never
acknowledged before durability.

Multi-event commands are atomic at the group boundary. If a command renames and
moves a file, both events are part of one atomic group. Readers and
projections either observe both events or neither event.

### Database Semantics On Top

A database layer built on Wrela should treat an atomic group as its transaction
record:

```text
begin at event frontier E
read streams/projections at E
declare expected stream sequences
emit one or more events
StorageWriter validates expected sequences
StorageWriter encodes one atomic group
return CommitToken(last_event_id)
```

Read-your-writes is a token and watermark rule:

```text
read projection P after CommitToken T:
  if P.last_event_id_applied >= T.last_event_id:
    read P directly
  else:
    wait for P, or read P plus committed tail events as an overlay
```

The overlay path lets a caller observe its own committed transaction without
forcing every projection update into the foreground append path.

Serializable constraints should be represented in streams the writer can
validate, not only in async projections. For example, a unique email constraint
can use a deterministic `UniqueEmail(email_hash)` stream and append an
`EmailClaimed` event only when that stream's expected sequence is still zero.
The projection may answer queries, but the stream expectation enforces the
constraint.

## Storage Writer

`StorageWriter` is the single commit authority for the event store. In the
desktop core model, it lives on the foreground/app/display core.

It owns:

```text
next_event_id
next_stream_id
slot_durable_frontier
atomic_group_frontier
pending append batches
stream directory head mutations
blob allocator publication
projection root publication
checkpoint publication
maintenance proposal admission
```

Append success is decided only by the writer after the foreground NVMe path has
completed the required durability sequence for the atomic group. The writer then
publishes a committed group to maintenance:

```text
CommittedAtomicGroup {
    first_event_id
    last_event_id
    slot_range_ref
    semantic_event_count
    event_type_summary_ref
    affected_streams_ref
}
```

Background workers never mutate truth directly. They prepare data using
background IO and submit proposals back to the writer. The writer validates each
proposal against current truth, commits it if still valid, and rejects it if the
world changed.

### Hot Append CPU Path

The writer should be optimized around the 1M events/sec target. The hot append
path should be compiler-generated, preallocated, and batch-oriented:

```text
reserve contiguous event_id range
load stream directory entries by direct index
check expected stream sequences
write fixed header fields
copy payload bytes once into pre-zeroed slots
write compile-time resolved event_type_id and payload_layout_id
write atomic_group_len and atomic_group_index
compute cheap checksum32
submit contiguous NVMe write commands from templates
ring foreground queue doorbell once per batch when possible
ack after required durability completion
```

The hot path must avoid:

```text
reflection
dynamic maps
string lookup
layout lookup
heap allocation per event
per-event NVMe commands
synchronous projection updates
large blob hashing
generic secondary index maintenance
```

Checksum work is not expected to be the dominant cost for 512-byte events.
Generic encoding, allocation, cache misses, stream bookkeeping, command setup,
flush cadence, and accidental projection work are more likely to dominate.

## Schema Evolution

The storage envelope is stable forever. Event payload semantics belong to the
event declaration and compiled user/projection code.

An `event` is a durable payload ABI declaration, not just a record type. The
event owns its layout IDs, current encoder, historical decoders, and upcasts.
The storage engine only sees:

```text
event_type_id
payload_layout_id
payload bytes
```

It never parses payload fields and never decides whether a layout changed.

### Event Layouts

Representative event declaration:

```wrela
event FileRenamed id 17 {
    file_id: FileId
    directory_id: FileId
    name_ref: BlobRef

    layout 1 {
        file_id: U64
        directory_id: U64
        name_ref: BlobRefPayload
    }

    layout 2 current {
        file_id: U64 = self.file_id.value
        directory_id: U64 = self.directory_id.value
        name: BlobRefPayload = self.name_ref
    }

    upcast 1 -> 2 {
        name_ref -> name
    }
}
```

Top-level event fields are the current semantic API. The `current` layout
defines the payload byte ABI and how current fields encode into it. Historical
layouts define old byte ABIs for decoding. If an event has exactly one layout,
that layout is current by default. If it has multiple layouts, exactly one must
be marked `current`.

`event_type_id` is the durable event ID from the declaration. `payload_layout_id`
is the literal layout number, scoped to the event type. The compiler emits
constants and match arms:

```text
FileRenamed.EVENT_TYPE_ID
FileRenamed.LAYOUT_1_ID = 1
FileRenamed.LAYOUT_2_ID = 2
FileRenamed.CURRENT_LAYOUT_ID = 2
```

Hot append writes constants:

```text
slot.event_type_id = FileRenamed.EVENT_TYPE_ID
slot.payload_layout_id = FileRenamed.CURRENT_LAYOUT_ID
```

Read dispatch is compiled code:

```text
match raw.header.event_type_id:
  FileRenamed.EVENT_TYPE_ID ->
    match raw.header.payload_layout_id:
      2 -> decode FileRenamed layout 2
      1 -> decode FileRenamed layout 1, then upcast 1 -> 2
      _ -> UnknownEvent(raw)
```

There is no boot registry, persisted layout catalog, first-use declaration,
layout fingerprint, or store-open compatibility step in v1. Storage guarantees
bytes. The language gives tools for layout history. The developer chooses
whether history matters.

### Greenfield And History

Greenfield editing is intentionally low friction. A developer may edit an
existing layout in place:

```wrela
event NoteRenamed {
    note_id: NoteId
    title_ref: BlobRef

    layout 1 {
        note_id: U64 = self.note_id.value
        title: BlobRefPayload = self.title_ref
    }
}
```

If persisted events already exist for `(event_type_id, payload_layout_id)`,
editing that layout may make old payload bytes decode incorrectly. V1 allows
that footgun. If the developer cares about old events, they add a new layout ID
and an upcast:

```wrela
event NoteRenamed {
    note_id: NoteId
    title_ref: BlobRef

    layout 1 {
        note_id: U64
        old_title: BlobRefPayload
    }

    layout 2 current {
        note_id: U64 = self.note_id.value
        title: BlobRefPayload = self.title_ref
    }

    upcast 1 -> 2 {
        old_title -> title
    }
}
```

The rename shorthand is sugar for same-type field mapping. More complex upcasts
can assign target fields explicitly.

Compiler responsibilities:

- require layout IDs to be unique within an event
- require exactly one current layout when multiple layouts exist
- typecheck current layout encode expressions against `self`
- generate encoders for current layouts
- generate decoders for historical and current layouts
- generate upcast functions and reject missing upcast endpoints
- generate compiled `event_type_id` and `payload_layout_id` dispatch

V1 semantics:

```text
known event_type_id + current payload_layout_id -> typed decode
known old payload_layout_id with upcast -> decode old layout, transform current
unknown payload_layout_id -> preserved UnknownEvent
corrupt slot -> recovery boundary
```

Future versions can add projection coverage diagnostics that warn when a
projection requires events whose historical layouts have no upcast path.

## Checkpoints

Stream checkpoints accelerate entity reconstruction:

```text
stream_id
through_sequence
state_layout_id
state_blob_ref
```

If a checkpoint state layout does not match current code, ignore it and replay
from the initial state. Checkpoints are never required for correctness.

Projection checkpoints accelerate read-model recovery:

```text
projection_id
projection_layout_id
projection_layout_hash
worker_code_hash
last_event_id_applied
projection_root_refs
```

If the projection layout changes and no compatible upcast exists, discard the
old checkpoint and rebuild the projection from events. If worker code changes,
the worker may also choose to rebuild so derived bytes match the new
deterministic logic.

Projection workers may write checkpoint data on the maintenance core, but a
checkpoint becomes visible only when the foreground `StorageWriter` accepts an
`AdvanceProjection` or checkpoint install proposal.

## Blob Storage

### Blob References

Blobs store large payloads, file contents, projection state blobs, manifests,
and sensitive details that should not live inline in the event log.

```text
BlobRef {
    blob_id
    byte_length
    content_hash
    key_id
    extent_count
    inline_extents_or_manifest_ref
}
```

Small or moderately sized blobs should usually have one extent. Larger or
fragmented blobs can have multiple extents. A blob with too many extents stores
its extent list in a manifest blob.

### Extents

An extent is a contiguous run of LBAs:

```text
Extent {
    start_lba
    block_count
    logical_offset
}
```

Loading a file means reading each extent in logical order, decrypting into the
destination buffer, and optionally verifying the content hash.

### Allocation

The first allocator can use free extent lists sorted by address and size:

- allocate a contiguous extent when possible
- split larger free extents as needed
- free by reinserting and coalescing adjacent extents
- use size classes later if fragmentation becomes visible

### Encryption And Delete

Blob payloads are encrypted by default. Each blob or object can have its own
content key wrapped by a broader store or user key.

Blob encryption should be authenticated encryption, not encryption-only. The
blob manifest records the AEAD algorithm, nonce material, key ID, extent list,
and content length. Nonces must be unique per key and blob extent; reusing a
nonce with the same content key is a storage corruption bug.

Crash order for a new referenced blob:

```text
A. write encrypted blob bytes
B. persist key manifest / wrapped content key metadata
C. append event that references BlobRef
```

If the system crashes before C, the blob is an orphan. If C is acknowledged,
the blob bytes and key metadata must already be recoverable.

Deletion has separate meanings:

```text
logical delete: append a delete event
space reclaim: free blob extents
privacy delete: destroy the blob key
```

Per-blob keys give a realistic delete story on SSDs: destroying the key makes
old physical copies unreadable even if wear leveling leaves stale pages behind.
Full-disk encryption can still layer underneath later for powered-off theft
protection.

The event log itself is not private in v1. Event headers expose metadata such
as event IDs, event type IDs, stream IDs, sequences, and payload lengths. If
that metadata becomes sensitive, the answer is a stronger whole-store
encryption layer, not pretending blob encryption hides event metadata.

Later anti-rollback support should hash sealed event segments and seal the
latest trusted tip hash with platform trust hardware when available. That is a
tamper-evidence feature, not required for the first executable milestone.

## Background Maintenance

Maintenance jobs are part of the storage engine, not one-off repair scripts.

### Orphan Collection

Events decide liveness. Allocator metadata is repairable.

An orphan collector builds the set of live blob extents from current blob refs
and marks allocated but unreachable extents reclaimable. It runs after unclean
shutdowns and opportunistically while idle.

### Blob Relocation And Defragmentation

Blob defragmentation uses copy-on-write relocation:

```text
A. copy blob to new extents
B. submit RelocateBlob(old_ref, new_ref, observed_version)
C. StorageWriter validates and appends durable BlobRelocated
D. free old extents after the new ref is published
```

Failure before C leaves the new copy orphaned. Failure after C but before D
leaves the old extents orphaned. Both are recoverable by orphan collection.

The defragmenter should prioritize fragmented, hot, or large blobs and stop
under foreground IO pressure.

### Directory And Projection Repair

Stream directories, projection checkpoints, and allocator summaries can be
rebuilt from events and blob manifests. Background repair should prefer
conservative rebuilding over clever in-place mutation.

### Event Segment Compression

The maintenance core owns cold event compression. It reads sealed hot segments
through `BackgroundStoragePath`, writes packed/compressed sealed segments, and
submits `SegmentCompressed` proposals to the foreground writer.

Foreground append latency has priority. Segment compression pauses or backs off
when foreground IO pressure, core-link backlog, or writer proposal latency rises.

## Projections

### Projection Declarations

A `projection` declaration is durable layout, not update logic. It names the
projection ID, owned containers, layout IDs, and upcasts for persisted derived
state.

Representative declaration:

```wrela
projection DirectoryChildren id 12 {
    layout 1 current {
        children: OrderedPages<
            partition: FileId,
            order: FileNameKey,
            value: DirectoryChild
        >
    }

    upcast 1 -> 2 {
        ...
    }
}
```

A projection declaration does not say which events it consumes and does not
contain `apply` functions. That is worker code. The declaration is the durable
ABI for roots, container pages, checkpoint compatibility, and future layout
evolution.

The first reusable container set should stay small:

```text
StateCell<T>:
  one compact value

DenseEntityMap<Id, T>:
  direct ID-addressed entity state

OrderedPages<Partition, SortKey, Row>:
  chunked sorted rows for range/list/timeline queries
```

Later specialized containers can add search posting segments, vector/HNSW
graphs, or other domain-specific shapes. They should be added as explicit
containers, not as a general query planner.

### Projection Workers

Projection workers are ordinary imperative maintenance-core code. A worker owns
one or more projection roots and explicitly applies events to their containers.

Representative worker shape:

```wrela
worker DirectoryProjectionWorker {
    source: SpscConsumer<CommittedAtomicGroup>
    events: EventReader
    projection: ProjectionWriter<DirectoryChildren>

    fn run(self) {
        loop {
            group = self.source.recv()
            events = self.events.read_group(group)

            for event in events {
                match event {
                    FileCreated -> self.apply_file_created(event)
                    FileRenamed -> self.apply_file_renamed(event)
                    FileMoved -> self.apply_file_moved(event)
                    FileDeleted -> self.apply_file_deleted(event)
                    _ -> {}
                }
            }

            self.projection.publish(group.last_event_id)
        }
    }
}
```

This keeps projection semantics explicit. The compiler can add sugar later, but
the primitive is worker code plus SPSC input plus projection writer.

### Projection Subscription

Projection workers do not subscribe to NVMe DMA interrupts. NVMe interrupts are
physical completion signals. Projection workers consume committed atomic groups
from explicit boot-wired SPSC rings on the foreground/maintenance `CoreLink`.

```text
ForegroundStoragePath interrupt
-> StorageWriter observes durability completion
-> StorageWriter advances atomic_group_frontier
-> StorageWriter writes CommittedAtomicGroup descriptor to configured SPSC producer
-> DirectoryProjectionWorker receives descriptor on maintenance core
-> DirectoryProjectionWorker reads complete group through BackgroundStoragePath/EventReader
-> DirectoryProjectionWorker writes new projection chunks
-> DirectoryProjectionWorker submits AdvanceProjection proposal
-> StorageWriter validates and publishes new projection root
```

The durable atomic-group frontier is the source-of-truth boundary. Projection
roots are separate derived-state boundaries. A projection worker that is not
boot-wired to a feed receives nothing and owns no live root, even if its
projection layout declaration exists in compiled code.

```text
append ack:
  atomic group is durable

projection root advanced:
  projection is queryable through event_id N

read-your-write projection query:
  wait until projection.last_event_id_applied >= accepted_group_last_event_id
```

Foreground writes do not wait for projection updates unless the caller
explicitly asks for a projection watermark.

### Read Plane

Projection updates run on the maintenance core. Projection queries run on the
core that owns the caller, usually the foreground/app core for UI work. A query
does not ask the projection worker for permission; it reads the latest
writer-published projection root and its watermark.

```text
query:
  load projection root pointer
  read root event watermark
  traverse immutable projection chunks through caller's storage path/cache
  return records plus watermark
```

Maintenance publishes new roots copy-on-write. Existing queries can finish on
the old root while new queries see the newer root. If a caller requires
read-your-write, it waits until the root watermark reaches the event ID it
cares about.

### Native Query Shape

The projection's physical layout is its access path.

For a query like "all records before X date", the projection should store
records in date-ordered chunks:

```text
chunk header:
  min_date
  max_date
  record_count
  next_chunk_ref

chunk body:
  records sorted by date
```

The query skips chunks whose date range is outside the filter and scans only the
boundary chunk.

### Storage Roots

Each projection checkpoint stores root refs for its owned containers:

```text
ProjectionCheckpoint {
    projection_id
    projection_layout_id
    projection_layout_hash
    worker_code_hash
    last_event_id_applied
    root_refs
}
```

Projection roots may point to state blobs, ordered chunks, dense arrays, keyed
pages, or other projection-specific structures. Nodes and chunks are cached
individually, so large projections do not need to load fully into RAM before
use.

## File Model

Files are entity streams plus blob refs.

Representative file events:

```text
FileCreated(file_id, parent_id, name_ref)
FileRenamed(file_id, parent_id, name_ref)
FileContentCommitted(file_id, blob_ref)
FileDeleted(file_id)
BlobRelocated(blob_id, old_ref, new_ref)
```

Opening a file usually does not replay the file stream from zero. It uses
materialized file state:

```text
FileState {
    file_id
    current_blob_ref
    name_ref
    parent_id
    deleted
    stream_sequence
}
```

If file state is missing or stale, the storage engine loads a checkpoint and
replays only the tail of the file stream.

## Snapshots, Backup, And Replay

Because events are truth and blobs are referenced by events, a snapshot is a
logical frontier:

```text
Snapshot {
    through_event_id
    reachable_blob_set
    projection_watermarks
}
```

Incremental backup is all complete atomic groups after the previous snapshot
frontier plus any new reachable blob extents and key metadata. Projection bytes
are optional cache; they may be included for faster restore, but restore
correctness depends on events and blobs.

The same frontier model gives time travel. A projection can be rebuilt or
queried as of an older `event_id` when the relevant events, blobs, and code are
available. For deterministic projection workers, `(event range, blob contents,
projection_layout_hash, worker_code_hash)` should produce the same projection
bytes across rebuilds. That makes replay useful for debugging, sync validation,
and later audit tools.

## Metrics

V1 should report enough metrics to validate the design:

- active NVMe LBA size
- active namespace mode: conventional or ZNS
- selected durability mode: power-fail atomic write plus FUA, FUA, or
  write-plus-flush
- event slot size
- atomic groups per second
- events per atomic group
- batch overflow slots used
- rejected oversized atomic groups
- events per committed LBA
- sealed or underfilled event blocks
- reserved empty hot slots
- bytes written per durable event
- estimated drive writes per day
- device-reported media writes when available
- durable events per second
- writer CPU cycles per event
- group commit latency p50/p99/p99.9
- foreground append-ack latency p50/p99/p99.9
- payload utilization and overflow rate
- hot bytes per event
- packed bytes per event
- compressed bytes per event
- compression ratio by event type
- compression CPU time
- compressed segment lookup latency
- cold compression backlog
- foreground/background NVMe path queue depth
- foreground/background NVMe path completion latency
- core link queue depth and wake count
- blob extent count per file
- orphaned extent bytes
- projection lag by event ID
- projection rebuild time
- projection SPSC depth and backpressure count
- projection layout upcast/rebuild count
- stream directory cache hit rate

These metrics decide whether later work needs a sparse spill log, compaction for
event slots, smaller payload conventions, more aggressive cold compression,
ZNS-only appliance hardware, or more aggressive projection checkpointing.

## First Milestone Shape

The first executable plan should prove:

- NVMe controller discovery, initialization, Identify, read, write, and flush
  through the driver and driver path scheme
- interrupt-driven NVMe admin and IO completions
- separate foreground and background NVMe IO paths
- permanent foreground and maintenance core ownership
- paired-core foreground/maintenance link using the existing wake protocol
- active LBA size detection with 512-byte preferred path and immutable
  underfilled 4 KiB packing path
- conventional namespace support, plus ZNS segment mapping when available
- explicit durability-mode selection from Identify data
- fixed 512-byte event slots
- reserved empty slots for underfilled 4 KiB durable batches
- atomic event groups as the semantic visibility boundary
- target/max batch policy with bounded overflow for atomic groups
- cheap hot checksum32
- stable event header with developer-authored `payload_layout_id`
- first-class event layout declarations and optional upcasts
- first-class projection layout declarations and optional upcasts
- sequential hot slot event IDs, including reserved empty slots when needed
- sequential stream IDs and dense stream directory lookup
- append success only after durable NVMe power-fail atomic write plus FUA, FUA,
  or write/flush
- single foreground `StorageWriter` commit authority
- compiler-generated append path without per-event allocation
- sealed event segment metadata and segment map lookup
- packed/no-op cold segment codec behind the final compression API
- replay after reboot
- one simple stream checkpoint
- encrypted blob extent write/read/delete shape, even if encryption is initially
  stubbed behind the final API
- one file-like entity stream whose current content is a blob ref
- one maintenance-core projection worker with explicit SPSC input and a
  projection layout stored in a native query shape, not a generic index
- orphan collection for blob extents after interrupted relocation

This is enough to validate the architecture without building a full filesystem.

## Deferred Work

- projection coverage diagnostics for missing upcast paths
- durable command inboxes and idempotency records
- advanced multi-queue NVMe scheduling beyond foreground/background paths
- SGL support for high-IOPS scatter/gather IO
- networking and AI storage paths
- IOMMU-backed DMA isolation
- full per-blob key management and key destruction
- full-disk encryption
- event-log anti-rollback and TPM-sealed tip hash
- sparse sync spill log for low-volume immediate durability
- large transaction protocol for groups beyond the foreground batch cap
- tuned compression codec selection
- destructive event retention/truncation policies
- multi-writer or sharded event log semantics
- SQL or relational query layer built on projections
- broad filesystem compatibility layer
