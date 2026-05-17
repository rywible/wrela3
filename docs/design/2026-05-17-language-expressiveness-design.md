# Language Expressiveness Design

## Purpose

Wrela is ready for a language expressiveness milestone before broad driver work.
The production substrate now has source-visible hardware discovery, memory
authority, executor placement, interrupt paths, topics, timers, and bounded
frames. The next problem is not another device. It is making driver and runtime
source pleasant, precise, and hard to misuse.

This milestone adds the language surface needed to express reusable driver,
queue, topic, result, and memory patterns directly:

- generics for memory views, topics, queues, results, and capability-bearing
  types
- static traits/interfaces for capability-shaped APIs
- enums/sum types for explicit state and result values
- const values and compile-time expressions
- typed memory views and address-space/region kinds
- pattern matching over sum types
- fixed-capacity typed arena reservations with fast allocation by default

The goal is a coherent language layer, not a minimal feature slice. The
implementation may still be internally sequenced, but the milestone is complete
only when the features work together and the library can use them to replace
the current concrete duplication.

## Current Context

The current language has:

- `data`, `class`, `driver`, `driver path`, `executor`, and `image`
  declarations
- `unique` authority-bearing classes and drivers
- `with` frames over `ExecutorMemory` and `ArenaFrame`
- arena intrinsics for `place` and `reserve`
- hidden lifetime tracking for frame-backed values
- concrete topic/result shapes such as `TimerTickTopic`,
  `TimerTickSubscription`, `TimerTickNext`, `EduInterruptTopic`, and
  `SerialRxTopic`
- physical memory, MMIO, PCI, interrupt, timer, and executor authorities as
  source-visible values

The current shape proves the semantic direction, but it forces too many
one-off concrete types. The same concepts are repeated with different payload
types, and status values are often encoded as booleans plus always-present
payload fields.

## Design Principles

### Make The Machine Shape Clearer

New language features should make ownership, memory layout, and device
authority easier to review. They must not hide allocation, introduce ambient
authority, or make driver wiring feel dynamic when it is still image-static.

### Fast Paths Are Plain

The most common spelling should be the performant one. Typed arena reservations
should reserve contiguous memory without a hidden initialization pass. Explicit
initialization remains available when the program wants an initialized view.

### Static Before Dynamic

Generics and traits are compile-time tools in this milestone. There are no
trait objects, vtables, dynamic dispatch, hidden heap allocation, or runtime
reflection.

### Storage And Policy Are Separate

Arena-reserved slots provide fixed-capacity typed memory. Higher-level
containers such as buffers, rings, queues, and tables own occupancy policy, not
allocation. This keeps capacity visible and lets data structures choose their
own initialization and validity rules.

### Preserve The Authority Model

Typed memory views and region kinds must be derived from existing authority
flows. Ordinary code must not forge MMIO, DMA, firmware, physical RAM, or
executor-local views from raw integers.

## Feature Set

### Generic Types And Methods

Wrela gains type parameters for declaration forms that naturally represent
reusable memory views or capability shapes:

```wrela
data Slice<T> {
    address: PhysicalAddress
    length: U64
}

data FixedBuffer<T> {
    slots: Slots<T>
    length: U64
}

class Topic<T> {
    identity: TopicIdentity
    id: U64
    depth: U64
}
```

Generic parameters are concrete, sized type parameters by default. A generic
type may be instantiated only with a type whose memory layout is known at
compile time. Concrete instantiations are monomorphized before IR lowering, so
layout and codegen see ordinary concrete types.

Generic methods are allowed where they are needed to express containers and
capability adapters:

```wrela
class FixedBuffer<T> {
    slots: Slots<T>
    length: U64

    fn push(self, value: T) -> Result<Unit, BufferFull> {
        if self.length == self.slots.capacity {
            return Result.Err(error = BufferFull())
        }
        self.slots.write(index = self.length, value = value)
        self.length = self.length + 1
        return Result.Ok(value = Unit())
    }
}
```

Type arguments are written in type positions with angle brackets:

```wrela
MutableSlice<Event>
Topic<TimerTickPayload>
Result<Unit, BufferFull>
```

Arena allocation keeps the existing functional style used by `place` and
`reserve`:

```wrela
let event_slots = tick.reserve_array(Event, count = EVENT_CAPACITY)
```

Here `Event` is a compile-time type argument passed in expression position to a
compiler-known arena intrinsic. It is not a runtime value.

`place` remains the typed object placement primitive:

```wrela
let events = tick.place(FixedBuffer<Event>(
    slots = event_slots,
    length = 0
))
```

This bump-allocates enough arena or frame memory for `FixedBuffer<Event>`,
writes the constructed fields into that memory, and returns the placed
`FixedBuffer<Event>` value. The returned value is arena-backed and carries the
same hidden lifetime as the arena or frame receiver.

### Static Traits And Interfaces

Traits describe required capability methods. They are checked statically and do
not create runtime interface values.

```wrela
trait Publisher<T> {
    fn publish(self, value: T)
}

trait Subscription<T> {
    fn try_next(self) -> Option<T>
    fn arm_wait(self)
    fn is_wait_armed(self) -> Bool
}
```

Implementations are explicit:

```wrela
impl Publisher<T> for TopicPublisher<T>
impl Subscription<T> for TopicSubscription<T>
```

Generic declarations may require traits:

```wrela
class DrainLoop<S, T> where S: Subscription<T> {
    input: S

    fn poll(self) -> Option<T> {
        return self.input.try_next()
    }
}
```

Trait method calls are resolved at compile time for each concrete
instantiation. If a generic declaration uses a trait-constrained method, the
compiler verifies that the selected concrete type has a matching `impl` and
then emits a direct call to the concrete method.

Traits do not grant authority. A type can satisfy `Publisher<T>` only if the
concrete value already carries publisher authority through its fields and
constructor flow.

### Enums And Sum Types

Wrela gains tagged union declarations:

```wrela
enum Option<T> {
    None
    Some(value: T)
}

enum Result<T, E> {
    Ok(value: T)
    Err(error: E)
}

enum InterruptNext<T> {
    Empty
    Gap(missed: U64)
    Message(value: T)
}
```

Enum variants are constructed with qualified names:

```wrela
return Option.Some(value = payload)
return Result.Err(error = BufferFull())
```

Enum layout is deterministic:

- a compiler-chosen discriminant field records the active variant
- payload area is the maximum size and alignment required by any variant
- zero-payload variants occupy only the discriminant area plus required
  padding
- generic enum instantiations are monomorphized before layout

Enums replace boolean-plus-payload result shapes. For example,
`TimerTickNext { has_message, gap, missed, message }` becomes an enum whose
payload exists only for the active state.

### Pattern Matching

Wrela gains `if let` and `match` over enum values.

```wrela
if let Option.Some(value = event) = self.rx.try_next() {
    events.push(value = event)
}
```

Full matches are exhaustive:

```wrela
match self.rx.try_next() {
    Option.Some(value = event) => {
        events.push(value = event)
    }
    Option.None => {
        self.rx.arm_wait()
    }
}
```

The checker rejects non-exhaustive `match` expressions unless there is an
explicit wildcard arm. Variant payload bindings are scoped only to their arm.
`if let` is intentionally non-exhaustive and executes no body when the pattern
does not match.

### Const Values And Compile-Time Expressions

Wrela gains module constants:

```wrela
const PAGE_SIZE: U64 = 4096
const EVENT_CAPACITY: U64 = 128
const EVENT_BYTES: U64 = sizeof(Event) * EVENT_CAPACITY
```

Const expressions may include:

- integer, boolean, string-literal, and type-name operands
- arithmetic, bitwise, comparison, and boolean operators already supported by
  the language where their operands are const
- `sizeof(Type)`
- `alignof(Type)`
- references to earlier constants in the same module or imported constants

Const expressions are evaluated with checked integer arithmetic. Overflow is a
compile-time diagnostic.

Static assertions make layout and capacity assumptions reviewable:

```wrela
static_assert(EVENT_BYTES <= PAGE_SIZE, message = "event frame exceeds one page")
```

Constants may be used in slot counts, arena lengths, interrupt vectors, PCI
IDs, register offsets, descriptor sizes, queue depths, and other ordinary
expression positions.

### Typed Memory Views

The generic memory-view family is:

```wrela
data Slice<T> {
    address: PhysicalAddress
    length: U64
}

data MutableSlice<T> {
    address: PhysicalAddress
    length: U64
}

data Slots<T> {
    address: PhysicalAddress
    capacity: U64
}
```

`Slice<T>` is an initialized readable view. `MutableSlice<T>` is an initialized
read/write view. `Slots<T>` is fixed-capacity typed memory reserved for values
of type `T`. `Slots<T>` is the fast arena reservation result and does not imply
that every slot currently holds a readable initialized value.

The API split is intentional:

```wrela
let event_slots = tick.reserve_array(Event, count = EVENT_CAPACITY)
let events = tick.place(FixedBuffer<Event>(
    slots = event_slots,
    length = 0
))
```

`Slots<T>` exposes capacity and write operations:

```wrela
event_slots.write(index = i, value = event)
```

Direct reads belong to initialized views and containers:

```wrela
let initialized = event_slots.fill(value = Event(kind = 0))
let first = initialized.get(index = 0)
```

`fill` explicitly initializes every slot and returns `MutableSlice<T>`.
Containers such as `FixedBuffer<T>`, `Ring<T>`, and `Queue<T>` track their own
valid initialized prefix or ring occupancy and expose reads only within that
valid region.

The existing `Bytes` and `MutableBytes` remain as compatibility and ABI-facing
byte views in this milestone. They are not treated as aliases for `Slice<U8>`
or `MutableSlice<U8>` until a separate compatibility plan makes that safe.

`Frame<T>` is not introduced as a public generic view in this milestone. The
existing `ArenaFrame` remains the allocation authority, and frame lifetime is
tracked as a hidden property of values derived from that authority.

### Region And Address-Space Kinds

Wrela distinguishes memory provenance and access semantics in the type system.
The canonical authority/view families for this milestone are:

- physical RAM authority derived from firmware memory maps
- executor-local arena and frame views
- MMIO regions derived from PCI BAR or platform hardware authority
- firmware pointer/table views derived from UEFI or ACPI roots
- DMA-visible buffers derived from physical memory authority plus device
  ownership
- volatile views for locations whose reads and writes must not be optimized
  away or reordered past required barriers

Representative shapes:

```wrela
data Mmio<T> {
    address: MmioAddress
}

data FirmwareSlice<T> {
    address: FirmwareAddress
    length: U64
}

data Volatile<T> {
    address: PhysicalAddress
}

data DmaBuffer<T> {
    owner: PciDevice
    slots: Slots<T>
}
```

The checker rejects ordinary construction of these views from integers.
Constructors for region-kind views are either authority-restricted or compiler
intrinsics exposed only through trusted platform modules. For example, a PCI
BAR claim can create `Mmio<Registers>`, but ordinary application code cannot
write `Mmio<Registers>(address = 0xFEE00000)` unless it is inside a trusted
authority flow.

Region kinds are not source-visible virtual address spaces. They are typed
provenance and access-mode distinctions over the existing physical-authority
model.

### Typed Arena Array Reservations

Typed arena array reservations are first-class arena intrinsics:

```wrela
let event_slots = tick.reserve_array(Event, count = EVENT_CAPACITY)
```

This matches the existing functional style:

```wrela
tick.place(Event(kind = 1))
tick.reserve(length = 64, align = 8)
tick.reserve_array(Event, count = 64)
```

Semantics:

- `Event` must be a concrete sized type.
- `count` must be a `U64` expression.
- optional `align`, if present, must be a non-zero power of two and at least
  `alignof(Event)`
- allocation size is `count * sizeof(Event)` with checked overflow
- allocation alignment is `alignof(Event)` unless a stricter `align` is given
- allocation uses the same bump cursor as `place` and `reserve`
- out-of-space or overflow traps through the existing memory OOM path
- the returned value is `Slots<Event>`
- the returned value carries the same hidden lifetime as the receiver arena or
  frame
- `Slots<T>` cannot be forged from raw addresses in ordinary code

`reserve_array` does not initialize every slot. This keeps the fast path plain
and makes fixed-capacity typed memory cheap enough to use as the normal way to
express hot working sets.

Readable initialized views are explicit:

```wrela
let initialized = event_slots.fill(value = Event(kind = 0))
let event = initialized.get(index = 0)
```

Containers are the normal way to add occupancy policy:

```wrela
data FixedBuffer<T> {
    slots: Slots<T>
    length: U64
}

data Ring<T> {
    slots: Slots<T>
    head: U64
    tail: U64
    len: U64
}
```

`FixedBuffer<T>` treats `length` as the initialized readable prefix.
`Ring<T>` decides which occupied ring positions are readable. `Slots<T>`
decides only where the fixed capacity lives.

The explicit primitive composition is:

```wrela
let event_slots = tick.reserve_array(Event, count = EVENT_CAPACITY)
let events = tick.place(FixedBuffer<Event>(
    slots = event_slots,
    length = 0
))
```

This is intentionally two operations. `reserve_array` allocates the element
slots. `place` allocates and initializes the small policy object that tracks
which slots are live. Source order remains layout order inside the arena or
frame.

The semantic API is method-shaped:

```wrela
event_slots.write(index = i, value = event)
slice.get(index = i)
slice.set(index = i, value = event)
```

Bounds checks are required at the semantic level. The optimizer may eliminate
redundant checks when loop structure proves `index < length` or
`index < capacity`.

### Generic Topics, Queues, And Results

The platform library should converge concrete topic and queue families into
generic shapes:

```wrela
class Topic<T> {
    identity: TopicIdentity
    id: U64
    depth: U64
}

class TopicPublisher<T> {
    topic: Topic<T>
}

class TopicSubscription<T> {
    topic: Topic<T>
    subscriber: ExecutorSlot
    cursor: U64
    armed: Bool
}
```

Concrete topics become instantiations:

```wrela
let serial_rx = Topic<SerialRxPayload>(
    identity = TopicIdentity(label = "hello.console.rx"),
    id = 0,
    depth = 64
)
```

`try_next` returns an enum:

```wrela
fn try_next(self) -> Option<T>
```

Interrupt queues, reliable topic publish results, timer tick payloads, and
driver-specific command queues should follow the same pattern. This removes
parallel concrete types whose only difference is payload type.

## Compiler Architecture

### Parser And AST

Types can no longer be represented as plain strings. The AST should gain a type
expression node that can represent:

- simple names: `Event`
- qualified names if the language grows them in type positions
- generic instantiations: `Topic<Event>`
- primitive types: `U64`, `Bool`, `never`

The expression parser must also recognize type-name operands in compiler-known
contexts such as `reserve_array(Event, ...)`, `sizeof(Event)`, and
`alignof(Event)`.

### Name Resolution And Indexing

The symbol index records generic declarations separately from concrete
instantiations. It must reject:

- duplicate generic parameter names
- arity mismatches such as `Topic<A, B>` when `Topic<T>` is declared
- use of value names as type arguments
- use of unsized or non-layout-bearing types as generic memory parameters

Trait declarations and `impl` declarations are indexed by trait name, type
arguments, and implemented concrete or generic type pattern.

### Type Checking

The checker validates generic declaration bodies once against their constraints
and validates each concrete instantiation after substitution. It must enforce:

- sized type parameters where memory layout is required
- trait bounds before trait method calls
- enum pattern binding types
- exhaustive `match` arms
- hidden lifetime propagation through generic records and classes
- slot lifetime escape rules equivalent to `place` and `reserve`
- region-kind construction restrictions

Generic containers that hold frame-backed slots or slices carry hidden
lifetimes through their fields. A `FixedBuffer<Event>` backed by frame slots
cannot be stored into executor-root state unless the lifetime rules allow it.

### Layout And IR

Monomorphization produces concrete type names before layout. The exact symbol
encoding is an implementation detail, but it must be deterministic and stable
for diagnostics and reports.

The layout package adds concrete layout for enum instantiations:

- discriminant offset
- active payload area offset
- variant payload field offsets
- total size and alignment

The IR gains explicit operations for:

- enum construction
- variant tests
- payload extraction
- slot reservation
- slot writes
- initialized slice reads/writes

Slot reservation should lower to the same arena bump machinery used by
`ArenaReserve` and `ArenaPlace`, with length and alignment derived from the
element type.

### Codegen

x86_64 codegen must emit:

- checked slot reservation size calculation
- arena bump bounds checks and OOM path calls
- slots/slice bounds checks
- enum discriminant stores and tests
- payload copies using the monomorphized payload layout
- direct calls for trait-constrained generic methods after monomorphization
- volatile/MMIO accesses with the required access width and ordering rules

Global bounds-check elimination is not required by this milestone, but the IR
should preserve enough structure for a future optimizer to remove redundant
checks without changing source semantics.

## Diagnostics

The milestone should add focused diagnostics for:

- generic arity mismatch
- unknown type parameter
- unsized type used where memory layout is required
- missing trait implementation
- trait method signature mismatch
- ambiguous or overlapping `impl`
- non-exhaustive `match`
- impossible enum variant pattern
- const expression overflow or non-const operand
- invalid `sizeof` or `alignof` operand
- slot count overflow or reservation-size overflow
- slots/slice lifetime escape
- raw construction of protected memory-region views
- attempted read from raw `Slots<T>` memory without an initialized view or
  container API

Error messages should point at the source construct that made the bad promise,
not only at the later use site. For example, a missing trait implementation
should identify the generic bound and the concrete type that failed it.

## Testing And Verification

Parser tests should cover:

- generic declarations and instantiations
- trait and impl declarations
- enum declarations and variant constructors
- `if let` and `match`
- const declarations, `sizeof`, `alignof`, and `static_assert`
- arena `reserve_array(Type, count = n)` syntax

Semantic tests should cover:

- generic arity and type-parameter scope
- monomorphized field and method types
- trait satisfaction and missing impls
- enum exhaustiveness and payload binding
- const overflow and invalid const operands
- protected region-kind construction
- frame lifetime propagation through `Slots<T>`, `Slice<T>`,
  `MutableSlice<T>`, `FixedBuffer<T>`, and `TopicSubscription<T>`
- rejection of raw `Slots<T>` reads
- acceptance of container-mediated reads within initialized occupancy

Layout and IR tests should cover:

- generic data/class layout after monomorphization
- enum discriminant and payload layout
- deterministic names for concrete instantiations
- slot reservation lowering to arena bump operations
- generic topic and queue lowering

Codegen tests should cover:

- slot reservation overflow and arena bounds traps
- slot write bounds checks
- slice get/set bounds checks
- enum construction and pattern matching
- trait-constrained calls lowered to direct concrete calls
- volatile/MMIO access code shape

Integration tests should prove:

- hello and production substrate examples can use generic topics/results
- at least one executor uses arena-reserved slots as its hot working set
- concrete `TimerTickTopic`, `EduInterruptTopic`, and `SerialRxTopic`
  duplication is replaced or clearly deprecated
- QEMU e2e behavior remains stable where firmware is available

## Non-Goals

This milestone does not add:

- typestate such as `Device<Discovered> -> Device<Configured>`
- dynamic trait objects, vtables, or runtime interface dispatch
- a general heap
- growable arrays or hidden reallocation
- per-element compiler definite-initialization tracking for raw slots
- uninitialized readable slices
- source-visible virtual address spaces, page tables, W^X, NX, or guard pages
- IOMMU enforcement
- module-scope functions unless they are separately approved as part of generic
  helper ergonomics
- generic specialization
- associated types or higher-kinded types
- operator overloading
- bracket indexing syntax

## Acceptance Criteria

The milestone is complete when:

- generic `data`, `class`, enum, trait, impl, and method surfaces parse and
  type-check
- concrete generic instantiations monomorphize into deterministic layouts and
  symbols
- traits provide static capability contracts with explicit impls and direct
  concrete calls after monomorphization
- `Option<T>` and `Result<T, E>` can replace boolean-plus-payload result shapes
- `if let` and exhaustive `match` work over generic enums
- constants, `sizeof`, `alignof`, and `static_assert` are usable in memory,
  queue, topic, and hardware declarations
- `Slots<T>`, `Slice<T>`, and `MutableSlice<T>` carry hidden lifetimes through
  generic containers
- `tick.reserve_array(Type, count = n)` reserves fixed-capacity typed memory using
  the same bump discipline as `place` and `reserve`
- raw `Slots<T>` memory is fast by default and not directly readable
- initialized reads go through initialized views or container APIs
- MMIO, volatile, DMA, firmware, physical RAM, and executor-local region kinds
  are distinct and cannot be forged from integers by ordinary code
- generic topics, subscriptions, queues, and result values are demonstrated in
  platform source
- existing compiler, semantic, layout, codegen, and relevant QEMU tests pass
