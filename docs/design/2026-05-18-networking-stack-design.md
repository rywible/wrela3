# Wrela Networking Stack Design

## Purpose

Wrela should grow a native networking stack in the same style as the rest of
the platform: source-visible authority, bounded memory, explicit executor
placement, and no hidden operating-system substrate.

The long-term stack is:

```text
0. Packet trace harness
1. e1000e RX/TX
2. Ethernet II
3. ARP
4. IPv4
5. ICMP ping
6. UDP
7. DHCP or static config
8. DNS
9. TCP
10. TLS
11. HTTP/1.1
12. HTTP/2
13. QUIC
14. HTTP/3
```

The first implementation milestone is deliberately smaller:

```text
packet trace harness + QEMU e1000e + static IPv4 + ARP + ICMP echo
```

That proves PCI device discovery, BAR ownership, DMA rings, interrupt delivery,
packet buffers, Ethernet parsing, IPv4 checksums, and one visible network
behavior before TCP, TLS, DNS, or HTTP enter the system.

The design should be read as three related scopes, not one giant milestone:

```text
network-substrate-v0:
  packet harness, e1000e, Ethernet, ARP, IPv4, ICMP, UDP

network-flow-authority-model:
  declared flows, endpoint authority, egress policy, budgets, epochs, reports

secure-web-stack:
  TCP, TLS, HTTP, crypto, trust, QUIC, HTTP/3
```

The first scope is the implementation target. The other two scopes define the
shape that substrate work must not block.

## Assumptions

- The first NIC target is QEMU `e1000e`.
- The likely real-hardware follow-up is one Intel e1000e-family PCIe gigabit
  controller, preferably an 82574L-class card if available.
- The first image uses static IPv4 configuration. DHCP is a later step.
- The first runtime milestone gives networking one dedicated executor on its own
  core. Later protocol work can split per-flow authorities onto additional
  executors without moving NIC ring ownership.
- Networking targets modern x86_64 machines with AVX2-class SIMD and crypto
  instructions as the supported fallback tier.
- The preferred performance target is AVX-512 with VAES, VPCLMULQDQ, and SHA
  extensions.
- Applications do not directly touch NIC MMIO, descriptor rings, DMA buffers,
  or raw mutable packet memory.
- Wrela owns the TLS crypto ABI, trust policy, endpoint policy, test corpus,
  constant-time rules, key lifecycle, and reports. Primitive implementations can
  mature behind that boundary.
- Wrela-controlled TLS endpoints should default to hybrid TLS 1.3 key exchange
  using X25519 and ML-KEM-768 once TLS work begins.
- Bespoke TLS crypto is treated as a serious security project: it needs known
  test vectors, differential tests, transcript tests, negative tests, and
  timing-aware implementation review before any production claim.
- Endpoint identity is name, SNI, and trust-policy first. IP addresses are route
  facts with TTL and provenance, not stable endpoint identity unless the endpoint
  explicitly declares static addressing.
- Real-hardware networking should require an explicit DMA domain policy. If the
  target lacks IOMMU containment, the image report must say that the NIC is a
  trusted DMA device.

## Non-Goals

This design does not add:

- a POSIX socket API
- a hidden scheduler or network thread pool
- IPv6 in the first milestones
- VLANs in the first milestones
- jumbo frames in the first milestones
- NIC checksum offload in the first milestones
- TCP segmentation offload, LRO, RSS, or multiqueue in the first milestones
- Wi-Fi
- full packet capture tooling
- production networking crypto on CPUs below the AVX2 fallback tier
- multiple NICs, multiple gateways, or multihomed routing in the first
  milestones
- QUIC before HTTP/1.1 compatibility exists
- a socket-style application API as the primary app surface

The design should not block those features. It should make each one an explicit
capability or protocol extension instead of a rewrite of the stack.

## Design Principles

### Whole-Machine Network Shape

Wrela should not expose ambient network access by default. The image source
declares the network shape it needs, and the compiler reports that shape.

The default model is closed:

- remote hostnames and ports are source-visible declarations
- local listening ports are source-visible declarations
- dynamic destination patterns require explicit authority
- connection, request, and stream budgets are derived from declarations and
  call-graph use where possible
- endpoint-specific TLS policy is compiled into the image

Dynamic web access is still allowed, but it is a named authority rather than a
silent escape hatch. A browser-like image can hold broad authorities such as:

```text
GeneralWebClientAuthority:
  user-entered or app-computed HTTPS destinations are allowed

DynamicDnsAuthority:
  runtime DNS can resolve names outside the closed endpoint set

PublicTrustAuthority:
  a declared public trust store or public-root policy can validate arbitrary
  web endpoints
```

The compiler cannot enumerate every host for a general browser, but it can still
report that the image has dynamic outbound authority, what schemes and ports are
allowed, maximum concurrent connections, DNS policy, trust policy, and memory
budgets. Closed endpoint authority is the default for appliances; dynamic web
authority is the explicit shape for free browsing.

Network authority should also compose with data classification. A value carrying
secret, credential, personal, or regulated data should not be sendable through a
general web authority unless the endpoint authority explicitly permits that data
flow. This turns "no ambient networking" into exfiltration resistance that the
compiler and image report can explain.

That policy should eventually be type-level information flow control, not only a
runtime flag. Values can carry labels such as:

```wrela
data Classified<T, Label> {
    value: T
}
```

An endpoint authority declares the labels it may transmit. Derived values inherit
the most restrictive label of their inputs unless source passes through an
explicit declassification authority that is named in the image report. A public
metrics endpoint can therefore accept `Public` counters but reject a value
derived from `Secret<ApiKey>` before code generation.

Conceptual source shape:

```wrela
data RemoteHttpsEndpoint {
    host: StringLiteral
    port: U16
    spki_hash: Bytes32
    allowed_group: TlsGroup
    allowed_cipher: TlsCipherSuite
    max_connections: U64
    egress: EgressPolicy
}
```

A request through a closed endpoint authority should not iterate a general cipher
list or trust arbitrary certificate roots. It should use the declared endpoint,
declared trust material, declared TLS group, declared cipher suite, declared
buffer budget, and declared egress policy. A request to a non-declared endpoint
is a type error unless the image explicitly holds dynamic network authority.

DNS can be mostly compile-time for closed endpoints, but build-time DNS is not
endpoint truth. The compiler can resolve declared names during the build, embed
the resolved addresses as route hints, and report the resolution time, TTL, and
resolver provenance. Runtime DNS refreshes or verifies route facts. Fully dynamic
DNS remains possible, but it is an explicit authority with its own budget and
attack surface.

### Declared Flows Are The Spine

Protocols are implementation stages. Flows are the Wrela-visible network unit.
The image declares which traffic classes can exist, and the compiler lowers each
declared flow into the smallest packet-to-event or request-to-packet pipeline
that satisfies that declaration.

Conceptual source shape:

```wrela
data DeclaredFlow {
    identity: FlowIdentity
    direction: FlowDirection
    local: LocalEndpointPattern
    remote: RemoteEndpointPattern
    protocol: ProtocolShape
    executor: ExecutorSlot
    arena: NetworkArena
    trust: PeerTrustPolicy
    budgets: FlowBudgets
    egress: EgressPolicy
    epoch: EpochPolicy
}
```

For a closed outbound HTTPS endpoint, that flow can include host, SNI, SPKI pin,
TLS group, cipher suite, route policy, maximum connections, memory budget,
congestion policy, and executor binding. For an inbound route, it can include
local port, peer policy, method/path shape, body schema, response authority, and
admission budget.

The runtime should therefore dispatch by declared flow before it performs
expensive work:

```text
RX bytes
  -> quarantine
  -> structural Ethernet/IP/transport verification
  -> declared-flow match
  -> generated flow pipeline
  -> typed semantic event or drop reason
```

Reusable protocol modules still exist for correctness and tests. The generated
runtime path is specialized to the declared flow rather than exposing one
general socket-shaped protocol stack to applications.

### Single-Core Network Reactor First

The default architecture is a single dedicated network reactor core. The goal is
to keep all networking on that core until measurements prove a split is needed.

The network reactor owns:

- NIC driver path authority
- RX and TX descriptor rings
- DMA packet buffers
- link state
- ARP cache
- IPv4 address state
- ICMP
- UDP
- DNS
- DHCP
- TCP
- TLS
- HTTP/1.1
- QUIC
- HTTP/3
- network timers
- retransmits
- semantic network event emission

Other executors communicate with networking through typed request queues, topics,
endpoint authorities, or flow-specific response authorities. This keeps ownership
reviewable and avoids cross-core mutation of descriptor rings or protocol state.

The reactor loop should have budgeted lanes so expensive work cannot starve
urgent packet maintenance:

```text
urgent lane:
  RX descriptor reclaim, TX completion, ARP, ICMP, TCP ACK/RST, retransmit
  deadlines

normal lane:
  established TCP/TLS records, UDP endpoints, DNS, DHCP, HTTP request parsing

expensive lane:
  TLS handshakes, ML-KEM, certificate validation, large JSON bodies, QUIC crypto
  bursts
```

Those lanes need mechanical budgets, not only names. The image report should
show packet budgets, queue-slot budgets, cycle or tick budgets, timer deadlines,
and interrupt moderation policy for each lane. Expensive work consumes declared
budget tokens before it begins so remote peers cannot mint unbounded work by
sending handshakes, parser-deep bodies, or RX storms.

The escape hatch is measured and explicit. If one core is not enough for a
declared target, Wrela can split selected flows or crypto work onto helper
executors:

```text
NetworkReactor:
  owns NIC rings, RX quarantine, TX completion, ARP, routing, and packet dispatch

Flow executors:
  own declared flow state machines and their narrowed packet/buffer authorities

Crypto executors, optional:
  own expensive handshake or bulk crypto work when the image declares them
```

The reactor remains the truth owner for NIC queues and packet memory. If flow
execution splits across cores, the source declaration must choose a transfer
mechanic: static flow-to-executor affinity, drain-and-transfer at an epoch
boundary, or a specific queue contract. The default answer is no handoff:
declared flow affinity decides which executor owns the state. Helper cores are a
performance escape valve, not the baseline architecture.

### Cross-Executor Queue Contract

Network communication between executors is a performance-critical API, not an
implementation detail.

The first queue shapes should be explicit:

- SPSC queues when the compiler can prove one producer and one consumer
- MPSC queues only where source-visible fan-in requires them
- cache-line separated head and tail fields
- fixed slot count and fixed payload layout
- explicit owner for each queued buffer or borrowed packet view
- poll or wake policy declared per queue

The compiler should reject an SPSC queue if call-graph analysis finds multiple
producers. The image report should name each network queue, its producer set,
consumer, capacity, payload size, wake policy, and memory owner.

### Device DMA And Ordering Are Explicit

The e1000e driver is not just parser code with an MMIO object. It is a
CPU/device concurrency boundary. The driver design must specify:

- descriptor ownership states for host-owned, device-owned, completed, and
  recycled descriptors
- RX and TX buffer lifecycle from DMA allocation through quarantine, lease, and
  return to the ring
- MMIO register ordering and required barriers around ring setup, tail updates,
  interrupt masking, and interrupt acknowledgement
- cache coherency assumptions for descriptor rings and packet buffers
- DDIO or NIC-to-LLC placement assumptions when the target exposes them
- reset epochs for descriptors, packet leases, ARP entries, flows, and pending
  app response authorities
- interrupt acknowledgement order relative to descriptor drain and device status
  reads

QEMU may be forgiving, but the source model should not teach the wrong contract.
The first implementation can conservatively use strongly ordered MMIO helpers,
architecture-specific ordering helpers, and uncached or explicitly coherent DMA
buffers. On x86, those helpers may lower to PAT or MTRR memory-type choices,
compiler barriers, `sfence`, `mfence`, or ordinary ordered stores depending on
whether the access is normal memory, MMIO, write-combining memory, or a device
doorbell. The source should request semantics such as "descriptor writes visible
before tail update" instead of spelling raw fences everywhere. The report should
say which memory type, cache-placement assumption, and barrier policy were
selected.

Real hardware also needs a DMA containment answer:

```wrela
data DmaDomainAuthority {
    device: PciDevice
    allowed_regions: BoundedDmaRegionSet
    iommu_enforced: Bool
}
```

For QEMU, Wrela may run with `iommu_enforced = false` while reporting trusted DMA
device status. For production hardware, network images should either require an
IOMMU-backed domain for NIC DMA or loudly report that the NIC can DMA outside the
network arena and is therefore part of the trusted computing base.

### Bounded Memory Everywhere

All packet buffers, descriptor rings, protocol tables, retransmission queues,
TLS transcript buffers, DNS transaction tables, and HTTP stream buffers are
allocated from named arenas.

The first stack should use fixed capacities:

- RX descriptor count
- TX descriptor count
- RX packet buffer count
- TX packet buffer count
- ARP table entries
- UDP endpoint count
- DNS in-flight query count
- TCP connection count
- TCP send and receive window sizes
- TLS session count
- HTTP request and response buffer limits

If a capacity is exhausted, the result is an explicit error or dropped packet
according to source-visible policy. There is no hidden heap growth.

### Packet Ownership Starts In Quarantine

RX DMA bytes are hostile input. They start as quarantined memory, not as an
Ethernet frame.

Conceptual typestate:

```wrela
data QuarantinedRxBytes {
    owner: NetworkReactorSlot
    buffer: DmaBufferLease<U8>
    length: U64
}

data VerifiedEthernetFrame {
    lease: PacketLease
    dst: MacAddress
    src: MacAddress
    ether_type: U16
    payload: PacketSlice
}

data VerifiedIpv4Packet {
    lease: PacketLease
    src: Ipv4Address
    dst: Ipv4Address
    protocol: U8
    payload: PacketSlice
}
```

Each parser performs the smallest checks needed to promote one typestate to the
next. `Verified*` means structurally valid for that layer, not trustworthy:

```text
QuarantinedRxBytes
  -> verify Ethernet bounds and destination
  -> VerifiedEthernetFrame
  -> verify IPv4 header, length, destination, checksum, no unsupported options
  -> VerifiedIpv4Packet
  -> verify UDP/TCP/ICMP length and checksum
  -> Verified transport payload
```

Later states should distinguish structure, authority, identity, and application
meaning:

```text
VerifiedTransportPacket:
  structurally valid packet for a transport protocol

MatchedDeclaredFlow:
  packet belongs to a declared flow and has an admission budget

AuthenticatedPeer:
  peer identity has been established by the declared policy, such as TLS SPKI

AuthorizedRequest:
  authenticated peer, route, method, body, and egress/ingress policy agree

DomainEvent:
  protocol details have been lowered into a typed application event
```

Malformed packets never become typed frames. They are dropped from quarantine,
their buffer lease is returned to the RX pool, and bounded counters record the
reason.

Zero-copy is allowed when ownership and lifetime prove it. A packet payload may
be borrowed by a protocol or application only through a `PacketSlice` tied to
the underlying lease. The compiler rejects storing that slice beyond the lease,
publishing it to a longer-lived topic, or returning it after the buffer is
released.

Cross-executor transfer has two allowed shapes:

- copy into the receiving executor's owned memory
- move a narrowed packet lease to the receiving executor

Borrowing quarantined or verified RX memory across executors without a lease
transfer is rejected. Disk-to-NIC or storage-to-TLS-to-NIC zero-copy can become
a proof obligation over compatible region authorities, not a best-effort
runtime optimization.

### Fused SIMD Copy And Checksums

NIC checksum offload remains deferred, but software checksums should not be
treated as slow scalar cleanup work.

Whenever the stack must copy packet bytes, the copy should be a fused SIMD pass
that also computes the next required checksum or hashable transcript fragment.
When the compiler can prove a zero-copy path, the stack should avoid the copy
and perform the checksum over the borrowed slice. The design goal is:

```text
copy only when ownership requires it;
when copying, fuse validation and checksum work into the pass
```

This is a Wrela-specific optimization because packet ownership, destination
executor, and lifetime are source-visible instead of hidden behind arbitrary
user pointers.

### Time, Timers, And Entropy Are Authorities

Networking needs both monotonic time and entropy. TLS certificate validation
also needs wall-clock policy.

The image must declare the authorities it uses:

```text
TimeAuthority:
  monotonic ticks for ARP expiry, DNS retries, TCP retransmits, TIME_WAIT,
  DHCP leases, TLS timers, and QUIC loss detection

ClockAuthority:
  wall-clock source for certificate validity, signed policy freshness, and
  optional NTP synchronization

EntropyAuthority:
  TCP initial sequence numbers, ephemeral ports, TLS nonces, key generation,
  QUIC connection IDs, and randomized admission tokens
```

Possible clock policies:

```text
RtcClock:
  use platform RTC or firmware clock and report its provenance

NtpClock:
  start with a build-time or RTC bound, then require authenticated NTP policy

BuildTimestampMonotonic:
  accept certificates valid at build time plus monotonic age bounds; useful for
  tightly controlled appliances, not general browsing
```

A TLS-using image without a declared `ClockAuthority` and `EntropyAuthority`
should fail to build. TCP and QUIC should similarly require entropy authority.

Timers should be represented as bounded state-machine resources. Each protocol
declares its timer slots and maximum events per tick. The network report should
include ARP, DNS, DHCP, TCP, TLS, and QUIC timer capacities.

Wrela already has a hardware-derived `TimerAuthority` and
`TimerDiscovery.require_periodic(...)` for periodic ticks. Networking should
consume that authority instead of inventing a parallel timer driver. The
networking work is the bounded timer wheel/table layered on top of the existing
timer authority.

Entropy should also be explicit. Candidate entropy sources include:

- `RDSEED`
- `RDRAND`
- UEFI RNG protocol during boot
- TPM RNG
- `virtio-rng` under QEMU

These sources feed a Wrela-owned CSPRNG with health checks and provenance in the
image report. Hardware entropy is an input authority, not ambient randomness.

### Network Epochs

Network state should be tied to explicit epochs. The reactor advances an epoch
when the facts that made existing state valid may have changed:

- NIC reset
- link flap
- DHCP lease acquire, renew, or loss
- static IP conflict detection
- route or gateway change
- DNS policy update
- TLS trust or endpoint policy update
- clock policy update that affects certificate validity

Each lease or state table declares whether it survives an epoch transition.
Packet leases, ARP entries, TCP control blocks, QUIC connections, DNS answers,
TLS sessions, and pending `ResponseAuthority` values must either carry the
epoch they were created in or be explicitly epoch-stable. This makes reset and
renumbering behavior reviewable instead of relying on scattered cleanup code.

### Attack Surface And Exhaustion Policy

Networking is hostile input. The stack should report its exposed parser and
state surfaces at build time.

The image report should include:

- listening ports
- unauthenticated parser entry points
- maximum ARP entries
- maximum DNS transactions
- maximum half-open TCP handshakes
- maximum established TCP connections
- maximum TLS handshakes
- maximum HTTP requests in flight
- packet parser byte budgets
- drop and admission policies

Expensive inbound work should be charged before it starts:

```wrela
data BudgetTokenAuthority {
    bytes: U64
    packets: U64
    cycles_or_ticks: U64
    handshake_slots: U64
    parser_depth: U64
    response_bytes: U64
}
```

The exact units can evolve, but the principle should not: unauthenticated peers
do not receive unlimited CPU, memory, parser recursion, handshake slots, or
response amplification. A declared flow decides how tokens are minted, refilled,
and consumed.

The first TCP listener must not be merely "bounded table or fail." It needs an
admission policy such as token-bucket SYN admission, SYN cookies, or an explicit
"not internet-facing without upstream filtering" declaration.

### Protocol Modules Are Reusable, Flow Pipelines Are Specialized

The e1000e driver exposes an Ethernet device path. ARP, IPv4, ICMP, UDP, TCP,
TLS, HTTP, QUIC, and HTTP/3 should still have reusable modules, tests, and
conformance fixtures.

The runtime path for a declared endpoint should be a specialized flow pipeline,
not a general socket API. This keeps the second NIC driver from contaminating the
protocol modules while still letting the compiler emit per-flow dispatch,
budgets, trust checks, route parsers, and semantic app events. Later NIC families
such as Intel `igb`, Realtek `r8169`, or `virtio-net` should only need to
implement the Ethernet device boundary or a richer hardware-flow-steering
boundary.

### Prefer Source-Visible Software Paths Before Offloads

The first stack computes checksums and performs protocol work in Wrela source.
NIC offloads are intentionally deferred. This makes packet bytes, checksums,
and protocol state visible during bring-up and reduces the number of device
features that can hide bugs.

### SIMD Is Part Of The Platform Contract

Networking is allowed to rely on a modern SIMD baseline. The supported fallback
tier is AVX2 plus AES-NI, PCLMULQDQ, and SHA extensions. The preferred fast tier
is AVX-512 with VAES, VPCLMULQDQ, and SHA extensions.

This requires Wrela to make vector state explicit before TLS is considered
complete:

- discover CPU SIMD and crypto features
- enable OSXSAVE and the required XCR0 state bits
- define which executors may execute vector code
- define interrupt save/restore behavior for XMM, YMM, opmask, and ZMM state
- report the selected crypto backend in the image report

The dedicated networking core makes this tractable: the network executor owns
long-lived crypto state on one core. Interrupt handlers still need a clear rule.
They should not execute vector crypto code unless the interrupt path has an
explicit vector-state save policy.

Crypto backend selection should not stop at AVX2 and AVX-512. QAT, platform
crypto engines, future post-quantum accelerators, or a DPU-hosted network
executor can satisfy the same Wrela crypto ABI when declared as hardware-backed
authorities and reported with their trust and side-channel assumptions. If the
network core shares last-level cache with app cores, that side-channel surface is
also a reported platform fact rather than an invisible property.

### Hardware Enforcement And QoS Are Backend Capabilities

The source model should leave room for hardware enforcement without depending on
it in the first implementation.

Possible backend capabilities:

- page-table permissions, PKEY/PKU, PKS, or another protection mechanism for
  quarantine and packet lease arenas
- cache-line wait using `MONITOR`/`MWAIT`, `MONITORX`/`MWAITX`, or related
  target-specific primitives for Wrela-owned waitlines
- CAT or equivalent cache partitioning for network executors, packet rings, and
  app cores
- DDIO or target-specific DMA cache-placement controls when the platform exposes
  them

These are reportable backend choices, not semantic requirements. PKEY-style
quarantine is page-granular, so it only protects a packet lease if the containing
page holds data that the target executor may see. Directly monitoring a NIC-owned
descriptor line is also hardware-dependent; the safe baseline is a Wrela-owned
waitline plus MSI/MSI-X or polling fallback. CAT and DDIO should be selected from
discovered hardware facts and reported as performance and side-channel policy.

### Compatibility Before Fancy Protocols

HTTP/1.1 over TLS is still the universal web compatibility path. HTTP/3 is
valuable, but an HTTP/3-only stack would miss a large part of the web. Wrela
should therefore build the boring universal path before QUIC/HTTP/3.

## Architecture

```text
Application executors
  |
  | endpoint requests, typed semantic events, response commands
  v
DeclaredFlow table and generated flow modules
  |
  +-- inbound POST /events:
  |     Ethernet/IP/TCP/TLS/HTTP/JSON -> EventAppendRequested
  |
  +-- outbound api.example.com HTTPS:
  |     Request<EventBatch> -> DNS route fact -> TCP/TLS/HTTP
  |
  +-- DNS, DHCP, ICMP, UDP service flows
  |
  v
Network reactor on dedicated core
  |
  +-- quarantine, structural parse, declared-flow dispatch
  |
  +-- ARP, IPv4 routing, timers, retransmits, admissions
  |
  +-- TCP/TLS/HTTP/QUIC stages as generated flow code
  |
  v
Ethernet II frame boundary
  |
  v
e1000e Ethernet device path
  |
  v
PCI BAR + DMA rings + MSI/MSI-X interrupt receiver
```

The baseline path does not move packet or protocol work off the reactor. It
matches structurally verified packets to declared flows, runs the generated
pipeline for that flow, emits semantic app events, and accepts semantic response
commands. Optional flow or crypto helper executors can be added later only when
the image declares them and measurement justifies the added cross-core traffic.

For later NICs with useful filters or steering, the same declared-flow table
should become a hardware programming artifact. The compiler can emit RX queue
affinity, hardware drop filters, RSS or flow-steering rules, interrupt target
selection, and executor placement from the source graph. The e1000e baseline may
emulate that in software because the design shape matters more than early
hardware richness.

### Network Authority

The image phase claims networking authority explicitly:

```wrela
let nic = pci.require_device(
    vendor_id = 0x8086,
    device_id = QEMU_E1000E_DEVICE_ID,
    occurrence = 0
)
let nic_mmio = nic.claim_mmio_bar(index = 0)
let nic_irq = nic.claim_msi()
let net_arena = root_arena.child(
    identity = ArenaIdentity(label = "network.core"),
    length = NETWORK_ARENA_BYTES,
    align = 4096
)
let net_dma = nic.claim_dma_domain(
    arena = net_arena,
    policy = DmaPolicy(require_iommu = target.production)
)
```

`QEMU_E1000E_DEVICE_ID` is a source constant added during implementation after
the QEMU device identity is observed in the test fixture. The driver should
start with that one known QEMU ID and only add real hardware IDs as they are
tested.

The resulting driver path is the only value that can read or write e1000e MMIO
registers or mutate descriptor rings. The DMA domain is the only authority that
can allocate network DMA buffers for that device.

### Driver Boundary

The first driver boundary should look conceptually like:

```wrela
driver path E1000ePath {
    identity: PathIdentity
    mmio: MmioRegion
    rx: E1000eRxRing
    tx: E1000eTxRing
    irq: TopicPublisher<NetworkInterrupt>

    interrupt receiver -> NetworkInterrupt
    fn initialize(self) -> LinkState
    fn poll_rx(self) -> Option<QuarantinedRxBytes>
    fn transmit(self, frame: TxFrameLease) -> Result<Unit, TxFull>
    fn link_state(self) -> LinkState
    fn mac_address(self) -> MacAddress
}
```

The actual API can be smaller in the first implementation. The important
boundary is that the driver exposes quarantined receive bytes, transmit leases,
and link facts, not PCI registers or descriptor ownership.

## Milestone 0: Packet Trace Harness

Before the e1000e driver, Wrela should have a pure packet laboratory.

Required behavior:

- parse Ethernet, ARP, IPv4, ICMP, and UDP from fixed byte fixtures
- emit Ethernet, ARP, IPv4, ICMP, and UDP into fixed byte buffers
- replay small pcap-derived traces through the quarantine and verification
  pipeline
- fuzz parser entry points with bounded byte slices
- verify checksum implementations against known packets
- produce deterministic packet-engine tests without QEMU

This milestone lets the packet engine mature before MMIO, DMA rings, interrupts,
and QEMU harness behavior complicate debugging. The e1000e driver then becomes
one producer and consumer of the same verified packet engine.

## Milestone 1: e1000e RX/TX

The e1000e milestone proves that Wrela can operate a PCIe Ethernet controller
through MMIO, DMA, and interrupts.

Required behavior:

- discover the QEMU e1000e PCI function
- claim BAR0 MMIO
- enable PCI memory access and bus mastering
- reset the controller
- read the MAC address from device state or a controlled fallback path
- allocate RX descriptors from a DMA arena
- allocate TX descriptors from a DMA arena
- allocate fixed-size packet buffers from a DMA arena
- model descriptor ownership transitions explicitly
- use explicit MMIO and DMA ordering helpers for ring setup and tail updates
- program one RX queue
- program one TX queue
- record the selected DMA memory type and IOMMU/trusted-device policy
- enable MSI or MSI-X when supported by the selected device
- provide an interrupt receiver for RX/TX/link events
- transmit one Ethernet frame
- receive one Ethernet frame

Out of scope:

- multiple queues
- RSS
- VLAN filtering
- multicast filtering beyond broadcast
- checksum offload
- jumbo frames
- power management
- wake-on-LAN

Verification:

- a QEMU e2e test boots a Wrela image with `-device e1000e`
- the image initializes the NIC and prints link/MAC/driver status
- the host sees a transmitted frame
- the image receives a host-generated frame
- the image does not touch raw memory outside its network DMA arena
- the image report names descriptor counts, DMA domain policy, memory type,
  barrier policy, interrupt mode, and reset epoch

## Milestone 2: Ethernet II

The Ethernet layer defines the common frame format used above all NIC drivers.

Required support:

- destination MAC
- source MAC
- EtherType
- payload length bounds
- broadcast destination
- unicast destination matching the NIC MAC
- minimum and maximum frame payload checks for MTU 1500

Deferred:

- VLAN tags
- LLC/SNAP
- jumbo frames
- multicast group management

The NIC hardware handles the frame check sequence. Wrela does not construct or
validate Ethernet FCS in software for the first milestones.

## Milestone 3: ARP

ARP maps IPv4 addresses to MAC addresses on the local link.

Required support:

- parse Ethernet ARP packets
- answer ARP requests for Wrela's configured IPv4 address
- send ARP requests for a target IPv4 address
- store a bounded ARP table
- expire or overwrite entries according to explicit policy
- reject malformed ARP packets without panicking

The first ARP cache can be tiny, such as 8 or 16 entries. Cache replacement is
policy, not hidden allocation.

## Milestone 4: IPv4

IPv4 provides the first routed packet layer.

Required support:

- parse IPv4 headers
- validate version, IHL, total length, protocol, destination address, and header
  checksum
- emit IPv4 headers
- compute IPv4 header checksums
- support static local IP, subnet mask, gateway, and DNS server configuration
- route local-subnet traffic directly through ARP
- route non-local traffic through the configured gateway MAC

Deferred:

- IPv4 fragmentation and reassembly
- source routing
- options
- multicast
- PMTU discovery

The first implementation should drop fragmented packets and packets with IPv4
options.

## Milestone 5: ICMP Ping

ICMP echo is the first user-visible protocol proof.

Required support:

- receive ICMP echo request
- send ICMP echo reply
- send ICMP echo request to a configured target
- receive ICMP echo reply
- validate ICMP checksum
- report success over serial or a typed network status topic

Verification:

- host can ping the Wrela image under QEMU
- Wrela can ping the QEMU gateway or a controlled host endpoint

## Milestone 6: UDP

UDP is the shared substrate for DHCP, DNS, and later QUIC.

Required support:

- parse UDP headers
- emit UDP headers
- validate length
- compute IPv4 UDP checksums, or explicitly allow zero checksum for IPv4 only
  where the protocol permits it
- bounded endpoint table keyed by local port
- explicit receive queue per endpoint
- explicit send API from the network reactor

The first endpoint API should be simple and appliance-shaped, not POSIX sockets.

Conceptual shape:

```wrela
data UdpEndpoint {
    local_port: U16
    rx_queue: NetworkQueue<UdpDatagram>
}
```

## Milestone 7: Static Config, Then DHCP

Static IPv4 configuration should be the first path.

```text
mac: read from NIC
ip: configured in image source
subnet: configured in image source
gateway: configured in image source
dns: configured in image source
```

DHCP follows once UDP and timers exist.

Required DHCP support:

- DHCPDISCOVER
- DHCPOFFER
- DHCPREQUEST
- DHCPACK
- lease timers
- retry timers
- renewal before expiry
- static fallback policy if configured

Deferred:

- DHCPv6
- complex option set
- multiple interfaces
- dynamic hostname registration

## Milestone 8: DNS

DNS maps hostnames to IP addresses for HTTP and HTTPS.

Required first support:

- UDP DNS client
- A record lookup
- CNAME following within a bounded depth
- transaction ID tracking
- timeout and retry policy
- bounded response parser
- static DNS server from image config or DHCP

Deferred:

- AAAA records until IPv6 exists
- TCP DNS fallback
- EDNS0
- DNSSEC validation
- DoT, DoH, and DoQ
- full resolver cache policy

The first DNS client should be an appliance resolver, not a general recursive
resolver.

## Milestone 9: TCP

TCP unlocks universal HTTPS compatibility.

Required first support:

- active open
- passive open if Wrela serves HTTP
- SYN, SYN-ACK, ACK handshake
- sequence and acknowledgment numbers
- send and receive windows
- retransmission timer
- out-of-order receive handling within a bounded window
- FIN close
- RST handling
- TIME_WAIT policy
- checksum validation and emission
- bounded connection table
- bounded send and receive buffers
- backpressure to application code

Deferred:

- large congestion-control sophistication
- selective acknowledgments
- TCP fast open
- keepalive
- urgent data
- window scaling until needed
- timestamps until needed

The first TCP stack should choose a small, correct congestion-control story
instead of pretending to be a mature internet stack. It can begin with
conservative slow start and loss response suitable for QEMU and local network
tests. Because IPv4 fragmentation is deferred, TCP must clamp MSS during SYN
handling to the configured MTU and should treat PMTU discovery as a later
explicit feature.

Congestion control, retransmit timers, and TIME_WAIT are flow policy, not hidden
globals. A local appliance flow, LAN service flow, WAN client flow, and
public-web dynamic flow can choose different conservative defaults once measured.
The first implementation can use one simple policy, but the declared-flow report
should leave room for per-flow congestion policy, retransmit timer shape, and
connection-retention budget.

The transport strategy should split local/fleet traffic from public WAN traffic.
Wrela-controlled LAN or fleet flows can use simple credit-based or bounded-window
transport policies because both endpoints are compiled with the same budget and
schema assumptions. Public WAN TCP should be an isolated transport module whose
congestion controller, RTT estimator, loss recovery, ACK behavior, and TIME_WAIT
policy can be upgraded without changing the reactor, declared-flow API, or
application event surface.

## Milestone 10: TLS With Wrela Crypto Policy

TLS gives HTTPS its security properties. Wrela will own its TLS crypto instead
of exposing a foreign TLS stack as the platform contract.

This is intentionally ambitious and must be treated as a security-critical
subsystem, not just another parser.

Wrela ownership means:

- source-visible endpoint trust policy
- source-visible group and cipher policy
- a stable crypto ABI owned by Wrela
- known-answer, differential, transcript, parser, and negative test corpora
- constant-time implementation rules for every primitive and backend
- key lifecycle and zeroization rules
- image reports that say which backend, trust roots, pins, and groups are active

Primitive implementations can mature behind that boundary. Early interop or test
work may use a non-production backend if the image report says so plainly. A
production claim requires Wrela-reviewed primitive implementations or a
separately declared hardware/backend authority that satisfies the same ABI and
verification gates.

### TLS Version

The first supported version should be TLS 1.3 only.

TLS 1.2 and older versions are out of scope unless compatibility forces them
later. Starting at TLS 1.3 avoids legacy cipher suites, renegotiation, and many
old protocol hazards.

### Hybrid Post-Quantum Key Agreement

The Wrela-controlled endpoint target includes hybrid post-quantum key agreement.
The default Wrela-controlled endpoint group is:

```text
X25519MLKEM768
```

This combines classical X25519 with ML-KEM-768 so the connection is not relying
only on elliptic-curve discrete log assumptions. The implementation should name
the exact IETF TLS hybrid ECDHE-ML-KEM draft or RFC version it targets, including
hybrid shared-secret concatenation and KDF binding, and should track NIST ML-KEM
semantics.

Endpoint policy is source-visible:

```text
Wrela-controlled endpoint:
  require X25519MLKEM768

public-web compatibility endpoint:
  prefer X25519MLKEM768; allow explicit legacy group only when declared

test endpoint:
  may pin a single group for deterministic transcript tests
```

A non-PQ TLS group should never be an ambient fallback. If an endpoint allows a
classical group such as X25519 alone, the endpoint declaration and image report
must say so.

Post-quantum authentication is also part of the design. Closed Wrela endpoints
can start with pinned SPKI hashes and build-time trust checks, while the crypto
package grows ML-DSA verification for endpoints and certificate chains that use
post-quantum signatures.

### Vector State And Crypto Backends

TLS depends on a CPU feature and vector-state contract.

Required CPU feature tiers:

```text
fallback tier:
  AVX2
  AES-NI
  PCLMULQDQ
  SHA extensions
  OSXSAVE/XGETBV support for XMM and YMM state

fast tier:
  AVX-512F
  AVX-512BW
  AVX-512VL
  VAES
  VPCLMULQDQ
  SHA extensions
  OSXSAVE/XGETBV support for opmask and ZMM state
```

The compiler and platform source should expose these as source-visible CPU
feature facts. The image chooses a required tier for the networking stack. If
the required tier is absent, the image boot-fails with a clear report instead
of silently falling back to slow or untested crypto.

The runtime must also define vector-state ownership:

- the network executor may use vector crypto on its dedicated core
- ordinary interrupt receivers do not use vector instructions by default
- any interrupt path that uses vector instructions must opt into explicit
  XSAVE/XRSTOR or equivalent save policy
- the image report records enabled XCR0 bits and selected crypto backend

### First Cipher Suites

The first recommended cipher suite is:

```text
TLS_AES_128_GCM_SHA256
```

Reason: the networking target explicitly assumes modern x86_64 crypto
acceleration. AES-GCM maps well to AES-NI/PCLMULQDQ on the AVX2 fallback tier
and to VAES/VPCLMULQDQ on the AVX-512 fast tier.

The first TLS implementation should also implement:

```text
TLS_AES_256_GCM_SHA384
TLS_CHACHA20_POLY1305_SHA256
```

`TLS_AES_256_GCM_SHA384` verifies the wider SHA-384/HMAC path. ChaCha20-Poly1305
is still useful for comparison, interop, and CPUs where AES acceleration is not
the chosen policy, but it is not the primary performance target for this design.

### Required Crypto Primitives

First production-trusted TLS backend:

- X25519 written to avoid secret-dependent branches and memory lookups
- ML-KEM-768 key generation, encapsulation, and decapsulation
- hybrid X25519MLKEM768 shared secret construction
- HKDF-SHA256
- SHA-256
- SHA-384
- HMAC-SHA256
- HMAC-SHA384
- AES-128
- AES-256
- GHASH
- AEAD AES-128-GCM
- AEAD AES-256-GCM
- ChaCha20
- Poly1305
- AEAD ChaCha20-Poly1305
- transcript hashing
- TLS 1.3 key schedule
- certificate signature verification for one chosen signature family
- ML-DSA verification for Wrela-controlled post-quantum endpoints when the
  endpoint declares it

All production-trusted implementations must avoid secret-dependent branches and
memory lookups, including AES, GHASH, Poly1305, HMAC, ML-KEM decapsulation, and
failure handling. Side-channel review is part of the backend acceptance criteria,
not an X25519-only requirement.

### Binary-Level Constant-Time Validation

Source-level constant-time review is not enough once Wrela owns lowering,
optimization, register allocation, and assembly emission. Secret taint should
survive from source values into IR, machine operations, registers, spills, and
emitted assembly.

The compiler should reject a production-trusted crypto backend if a secret-tainted
value influences:

- a conditional branch or indirect branch target
- a load or store address
- a variable-latency instruction forbidden by the target profile
- a call into an unverified helper

For Wrela-generated crypto, this can become a compile-time codegen check. For
handwritten assembly, hardware crypto, or imported verified objects, the image
must declare a backend authority with disassembly, object metadata, or an
external proof artifact that satisfies the same reporting surface. Timing tests
remain useful, but they are evidence on top of binary structure checks, not a
replacement for them.

The crypto package should contain a small scalar reference path for known-answer
tests and differential checks. That reference path is not the production
networking target.

### Build-Time Trust And Certificates

The first TLS trust model should use Wrela's whole-image compilation instead of
copying a general-purpose OS CA-bundle model.

Closed endpoints declare their trust material:

- pinned SPKI hash
- optional pinned certificate
- optional root or intermediate certificate set
- allowed TLS group
- allowed cipher suite
- validity window policy

The compiler should validate as much as possible at build time. For a declared
endpoint, it can check the configured chain, SPKI hash, certificate validity
against the declared clock policy, and whether the endpoint policy permits
classical fallback. A build should fail if a closed TLS endpoint has no trust
declaration.

Runtime certificate validation still exists, but it is endpoint-specialized.
The runtime checks the presented chain or SPKI against the compiled policy; it
does not search a broad ambient trust store unless the image explicitly declares
that it is a general web client.

A broad public CA store is therefore a separate dynamic authority, not the
default TLS posture.

### Verification Requirements

Bespoke crypto is allowed only with hard verification gates:

- known-answer tests for every primitive
- RFC test vectors where available
- negative tests for malformed TLS records and handshakes
- transcript tests against a known server implementation
- differential tests against at least one mature implementation during
  development
- fuzz tests for parsers
- code review focused on secret-dependent branches, table lookups, rejection
  sampling, cache behavior, and zeroization
- binary-level secret-taint checks for production-trusted generated crypto
- disassembly or proof artifacts for production-trusted non-generated crypto
- explicit statement of what is and is not production-trusted

The first TLS implementation can be excellent engineering without being declared
production-safe on day one.

## Milestone 11: HTTP/1.1

HTTP/1.1 is the broad compatibility layer.

Required client support:

- GET
- POST with fixed content length
- Host header
- Connection close
- response status line
- response headers with bounded sizes
- fixed-length response body
- chunked transfer decoding

Required server support, if serving:

- parse request line
- parse bounded headers
- route a small fixed set of paths
- emit fixed-length responses
- close connection cleanly

Deferred:

- compression
- cookies beyond raw header pass-through
- redirects beyond app-visible response handling
- proxy support
- range requests
- WebSocket upgrade
- streaming large bodies

## Milestone 12: HTTP/2

HTTP/2 improves multiplexing over TCP/TLS.

Required later support:

- ALPN negotiation for `h2`
- HTTP/2 connection preface
- SETTINGS frames
- HEADERS frames
- DATA frames
- stream IDs
- per-stream state
- connection and stream flow control
- HPACK decoding and encoding

Deferred:

- server push
- priority tree complexity
- aggressive header compression tuning

HTTP/2 should wait until HTTP/1.1 over TLS is boring and reliable.

## Milestone 13: QUIC

QUIC is the modern encrypted transport over UDP. It should share UDP, DNS, and
lower networking code with DHCP and DNS, but it replaces TCP and most of the
TLS-over-TCP transport shape.

Required first support:

- QUIC version negotiation behavior for the selected version
- connection IDs
- packet number spaces
- ACK frames
- loss detection
- retransmission
- stream frames
- stream flow control
- connection flow control
- TLS 1.3 handshake integration
- packet protection
- idle timeout
- conservative congestion control

Deferred:

- 0-RTT
- session resumption
- connection migration
- multipath
- datagram extension
- advanced congestion control
- broad version support

QUIC should not be sold as "skipping TCP." It is building a different reliable
transport with encryption integrated into the transport.

## Milestone 14: HTTP/3

HTTP/3 maps HTTP semantics onto QUIC streams.

Required support:

- ALPN `h3`
- HTTP/3 control stream
- request stream
- response stream
- QPACK with conservative bounded dynamic table policy, or static-table-only
  restrictions for the first client tests if interop allows it
- DATA frames
- HEADERS frames
- SETTINGS frames

Deferred:

- server push
- WebTransport
- datagram use
- advanced prioritization
- large dynamic QPACK table tuning

HTTP/3 is not required for broad web reach, but it is valuable for Wrela's
long-term modern networking story.

## Application API Shape

The first app-facing API should not mimic POSIX sockets. Wrela should expose
endpoint authorities, request/response futures, event streams, and typed
semantic events:

```wrela
data EndpointAuthority<F> {}
data EndpointRequestAuthority<F, Request, Response> {}
data NetworkEventStream<Event> {}
data ResponseAuthority<F> {}
```

TCP connections, TLS sessions, HTTP streams, and QUIC connections are reactor
state, not ordinary application objects. Compatibility modules may expose narrow
byte-stream authorities for special cases, but that is not the primary appliance
API.

For server-side routes, apps should not need to know HTTP exists. The network
reactor owns Ethernet, IP, TCP or QUIC, TLS, HTTP parsing, routing, and body
framing. Apps receive typed domain events.

Conceptual source shape:

```wrela
route POST "/events" body Json<EventAppendRequest>
    -> EventAppendRequested

route GET "/health"
    -> HealthCheckRequested
```

The compiler can then generate route-specialized parsing:

- match only declared methods and paths
- accept only declared content types
- parse JSON directly into typed event fields
- borrow body slices through leases where safe
- reject missing, extra, or wrong-type fields according to route policy
- emit semantic app events into declared queues

Example event shape:

```wrela
data EventAppendRequested {
    auth: AuthenticatedPeer
    stream: StreamId
    body: HttpBodyLease
    response: ResponseAuthority<AppendEventsFlow>
}
```

The app responds with semantic commands:

```wrela
data SendJsonResponse<T> {
    response: ResponseAuthority<F>
    status: HttpStatus
    body: T
}

data SendBlobResponse {
    response: ResponseAuthority<F>
    status: HttpStatus
    body: BlobReadLease
}
```

The network reactor turns response commands back into HTTP, TLS, TCP or QUIC,
and Ethernet frames. This keeps HTTP as a reactor implementation detail for
declared routes.

Blob responses are the place where Wrela can eventually beat conventional
`sendfile` plus kTLS. A `BlobReadLease`, `TlsSessionAuthority`, and `TxBufferLease`
can form a compile-time-proved chain of compatible region authorities. When the
backend can prove nonce, lifetime, alignment, and buffer ownership, it may encrypt
directly into the TX buffer instead of copying through a user-visible byte stream.

Client-side code similarly uses explicit endpoint authorities. The authority
determines destination, trust, request type, response type, body limits, egress
classification, and runtime budget.

Examples:

```text
EndpointRequestAuthority<MetricsFlow> can send MetricsBatch and receive Ack.
EndpointAuthority<GeneralWebFlow> can issue declared dynamic-web requests.
UdpEndpoint can send datagrams from one local port.
DnsClient can resolve names through configured resolver authority.
```

This preserves Wrela's capability model and avoids ambient global networking.

### JSON-To-Event Performance Goal

The heaviest compatibility path is:

```text
NIC RX
  -> Ethernet/IP/TCP
  -> TLS decrypt/authenticate
  -> HTTP route
  -> JSON parse
  -> typed Wrela event
  -> app queue
```

For `e1000e` gigabit, the target is line-rate JSON-to-event for bounded request
schemas. The network reactor should not build a general JSON DOM. It should
compile each declared route into a schema-specialized parser that scans the body
once, validates expected fields, and writes or borrows directly into the typed
event.

For public-web compatibility, full JSON behavior may be required. For
Wrela-controlled clients, a typed binary frame can avoid JSON entirely and
promote a verified body lease directly into typed views.

Wrela-to-Wrela traffic should not be forced through JSON over HTTP when both
ends are compiled with known schemas. A later `wrela-binary-v1` ALPN can use
length-prefixed, schema-compiled frames with the same endpoint trust and flow
budget model. Public web gets HTTP and JSON. Wrela fleets get compiler-emitted
encoders and decoders for declared types.

## Error Handling

Networking errors should be explicit values wherever possible.

Examples:

- `RxRingEmpty`
- `TxRingFull`
- `MalformedEthernetFrame`
- `ArpCacheMiss`
- `Ipv4ChecksumInvalid`
- `UdpPortClosed`
- `DnsTimeout`
- `TcpRetransmitLimit`
- `TlsAlert`
- `CertificateRejected`
- `HttpHeaderTooLarge`

Driver-fatal errors are reserved for cases where the image cannot continue
meaningfully, such as a required NIC being absent or a DMA ring failing to
initialize.

Malformed network packets are not boot-fatal. They are dropped, counted, and
reported through bounded diagnostics.

## Reports And Diagnostics

The image report should include:

- selected NIC PCI identity
- BAR ownership
- DMA domain policy and whether IOMMU containment is enforced
- DMA memory type, cache-placement assumption, and MMIO/barrier policy
- interrupt vector and mode
- network reactor executor slot and CPU placement
- hardware enforcement choices for quarantine, packet leases, waitlines, cache
  partitioning, and DMA cache placement
- reactor loop budgets for urgent, normal, and expensive lanes
- network arena size and subdivisions
- RX/TX descriptor counts
- packet buffer counts and sizes
- packet quarantine pool size and verified packet lease budget
- cross-executor queue producers, consumers, capacities, and wake policies
- declared flows, executor affinity, arena, route, trust, and budget policy
- hardware flow-steering or software dispatch policy
- declared remote endpoints and local listening ports
- dynamic web authorities, if present
- egress data-classification policy by endpoint authority
- declassification authorities, if present
- computed or declared connection budgets
- inbound budget token policy
- typed route-to-event mapping
- endpoint trust policy and pinned SPKI or certificate identities
- declared time, clock, and entropy authorities
- entropy source provenance and CSPRNG policy
- timer wheel or timer table capacities
- network epoch sources and survival policies
- attack-surface summary and admission policies
- static or DHCP configuration mode
- supported protocols
- supported TLS groups
- supported TLS cipher suites
- post-quantum TLS policy
- required SIMD tier
- selected crypto backend
- hardware-backed crypto authorities and reported side-channel assumptions
- constant-time validation mode for each production-trusted crypto backend
- enabled XCR0 vector-state bits
- supported HTTP versions

Runtime diagnostics should include bounded counters:

- RX packets
- TX packets
- RX drops by reason
- TX drops by reason
- quarantine promotion failures by reason
- ARP hits and misses
- IPv4 checksum failures
- UDP packets by endpoint
- TCP retransmits
- TCP admission drops
- TLS handshake failures
- HTTP request count
- typed semantic events emitted by route
- epoch advances by reason
- budget-token denials by flow

Runtime diagnostics should also include a tiny bounded flight recorder. This is
not full packet capture. It is an always-on ring of metadata records:

- packet metadata without full payloads
- flow-match results
- drop reasons
- state transitions
- timer firings
- descriptor ring pressure
- queue pressure
- budget-token denials
- epoch changes

The recorder should have a fixed memory budget and redact payload bytes by
default. Network bugs are otherwise too hard to understand on a freestanding
image.

## Testing Strategy

### Compiler And Semantic Tests

- reject forged NIC, DMA, and network authority values
- reject duplicate NIC claims
- reject packet buffers escaping their arena lifetime
- reject network tables without bounded capacities
- reject use of quarantined bytes outside verifier functions
- reject cross-executor packet borrows without lease transfer
- reject declared-flow executor ownership mismatches
- reject packet delivery to a flow without an admission budget
- reject closed endpoint requests to undeclared hostnames or ports
- allow dynamic web requests only with explicit dynamic web authority
- reject secret-bearing data sent through an endpoint whose egress policy does
  not allow that data classification
- reject implicit declassification of secret-bearing data before network egress
- verify derived values preserve restrictive labels unless source holds an
  explicit declassification authority
- verify declared routes emit typed semantic events with bounded payloads
- reject TLS endpoints without clock and entropy authority
- reject TLS endpoints without declared trust material
- reject stale build-time DNS facts used as endpoint identity without an explicit
  static-address policy
- report network arena ownership in the image report
- report DMA domain, IOMMU/trusted-device status, declared flows, budgets,
  epochs, and flight recorder capacity in the image report
- report hardware enforcement capabilities and fallbacks for quarantine,
  waitlines, cache partitioning, and DMA cache placement
- reject production-trusted generated crypto whose emitted code uses
  secret-tainted values in branch conditions or memory addresses

### Unit Tests

- Ethernet frame parse and emit
- ARP parse and emit
- IPv4 checksum
- ICMP checksum
- UDP checksum
- DNS message parser bounds
- TCP state transitions
- TLS primitive test vectors
- ML-KEM-768 known-answer tests
- X25519MLKEM768 transcript tests
- build-time endpoint trust validation tests
- information-flow egress and declassification tests
- packet quarantine promotion tests
- packet lease lifetime negative tests
- declared-flow match and drop-reason tests
- budget-token admission tests
- network epoch invalidation tests
- typed route-to-event parser tests
- dynamic web authority negative tests
- SIMD backend selection tests
- scalar-versus-AVX2 crypto differential tests
- AVX2-versus-AVX-512 crypto differential tests when host support exists
- emitted-assembly secret-taint tests for generated crypto backends
- HTTP parser bounds

### QEMU End-to-End Tests

The ARP and ICMP milestones require an L2-visible harness. QEMU user networking
is useful later for outbound compatibility checks, but it should not be the
first proof because it can hide Ethernet and ARP behavior behind host NAT.

The first implementation plan must choose one harness that lets tests observe
guest Ethernet frames directly, such as TAP, passt with appropriate visibility,
or a socket-backed peer process.

- boot image with QEMU `e1000e`
- initialize NIC
- receive host packet
- transmit packet to host
- host pings Wrela
- Wrela pings host or gateway
- UDP echo
- DNS query against controlled resolver
- TCP echo
- HTTPS request against controlled server

### Interop Tests

Later milestones should test against common host tools:

- `ping`
- `arp`
- `tcpdump`
- `socat` or `nc`
- `dnsmasq`
- `nginx` or `caddy`
- `openssl s_server`
- a QUIC/HTTP3 server when that milestone arrives

## Implementation Order

The stack should be implemented in small gates:

1. Add enough e1000e QEMU test plumbing to boot with the NIC.
2. Add the packet trace harness and parser fuzz fixtures.
3. Add quarantined packet bytes and verified packet typestates.
4. Add DMA ring memory shapes, descriptor ownership states, DMA domain policy,
   ordering helpers, and reports.
5. Bring up e1000e reset/init/link/MAC reporting.
6. Prove RX/TX with raw Ethernet frames.
7. Add Ethernet II parse/emit helpers.
8. Add ARP request/reply and cache.
9. Add IPv4 parse/emit/checksum.
10. Add ICMP echo.
11. Add UDP.
12. Add time, clock, entropy, and bounded timer authorities over the existing
    `TimerAuthority`.
13. Add network epochs and epoch-carrying packet/state leases.
14. Add the bounded flight recorder and drop/state-transition diagnostics.
15. Add compile-time declared-flow shapes and reports, including dynamic web
    authority, egress policy, and budget-token policy.
16. Add type-level information-flow labels and explicit declassification
    authority for network egress.
17. Add DHCP, or keep static config if the next target is DNS/TCP.
18. Add DNS as route-fact resolution with TTL/provenance.
19. Add TCP inside declared-flow state machines with transport policy separated
    by LAN/fleet and public-WAN flow class.
20. Add hardware enforcement capability reports for quarantine, waitlines, cache
    partitioning, and DMA cache placement.
21. Add CPU feature discovery and vector-state policy for networking crypto.
22. Add Wrela crypto ABI, trust-policy reports, and test backend boundaries.
23. Add binary-level secret-taint validation for production-trusted generated
    crypto.
24. Add AVX2 fallback crypto backends.
25. Add AVX-512 fast crypto backends.
26. Add ML-KEM-768 and X25519MLKEM768 for Wrela-controlled TLS endpoints.
27. Add TLS 1.3 with Wrela crypto policy and build-time trust policy.
28. Add HTTP/1.1 route-specialized semantic events.
29. Add schema-specialized JSON-to-event parsing.
30. Add Wrela-to-Wrela binary framing when a fleet use case needs it.
31. Add HTTP/2 if the use case requires it.
32. Add QUIC.
33. Add HTTP/3.

Every gate must have a QEMU or unit-test success criterion before the next layer
depends on it.

## Open Decisions

- Whether the first e1000e interrupt mode is MSI, MSI-X, or fallback-driven.
- What DMA memory type, MMIO ordering helpers, and descriptor ownership states
  the first e1000e driver exposes.
- Whether real-hardware networking requires IOMMU-backed `DmaDomainAuthority` or
  permits an explicit trusted-DMA-device report.
- Whether the first test network uses QEMU user networking, socket networking,
  TAP, or passt.
- Whether DHCP comes before DNS/TCP or static config remains the only path until
  HTTPS works.
- Which `DeclaredFlow` syntax best expresses local/inbound flows, closed remote
  endpoints, and dynamic web authority.
- Which egress data-classification labels are built into the first network
  authority checker.
- Which information-flow labels are type-level, and which operations require
  explicit declassification authority.
- Which budget-token units are required for the first listener and first outbound
  client.
- Which transport policy classes distinguish LAN/fleet flows from public-WAN TCP
  flows.
- Which network epoch transitions invalidate packet leases, ARP entries, TCP
  flows, TLS sessions, and response authorities.
- How large the always-on flight recorder should be and which fields it stores.
- Whether PKEY/PKU, PKS, page permissions, cache-line wait, CAT, or DDIO are
  available on the first real hardware target and what fallback each uses.
- Which first real hardware target satisfies the AVX-512 fast tier.
- Whether vector code is permitted only in the network executor or also inside
  selected interrupt handlers with explicit XSAVE/XRSTOR policy.
- Whether HTTP/2 is still worth implementing before QUIC/HTTP/3 for the first
  real Wrela appliance target.
- Which closed endpoint policy syntax best expresses static endpoints and
  explicitly dynamic patterns.
- Which dynamic web authority shape is acceptable for free browsing.
- Which clock authority is acceptable for the first TLS image.
- Which entropy source is required before TCP, TLS, and QUIC leave local tests.
- What reactor loop budgets should be used for urgent, normal, and expensive
  lanes on the first six-core target.
- Whether the first TLS verification target is a pinned local test server or a
  known external endpoint.
- What binary-level constant-time validation is required before a generated
  crypto backend can be production-trusted.
- Which real e1000e-family card is the first hardware target after QEMU.

## Success Criteria

The `network-substrate-v0` scope is successful when:

- Wrela can boot under QEMU with `e1000e`.
- The network reactor owns all baseline networking state on its dedicated core.
- Wrela reports DMA domain policy, descriptor counts, memory type, MMIO/barrier
  policy, interrupt mode, and reset epoch.
- A host can ping the Wrela image.
- Wrela can complete a UDP exchange with a controlled host service.
- Wrela rejects quarantined packet bytes outside the verifier path.
- Wrela records bounded drop reasons, state transitions, queue pressure, and
  epoch changes in the flight recorder.

The `network-flow-authority-model` scope is successful when:

- Wrela reports declared flows with executor affinity, arena, route, trust,
  egress, budget, and epoch policy.
- Wrela rejects closed endpoint requests to undeclared hostnames or ports.
- Wrela rejects secret-bearing values sent through endpoint authorities whose
  egress policy does not allow that classification.
- Wrela preserves information-flow labels through derived values and requires
  explicit declassification authority for label downgrades before egress.
- Wrela treats DNS answers as route facts with TTL and provenance, not endpoint
  identity unless static addressing is declared.
- Wrela reports typed route-to-event mappings and dynamic web authority when
  present.
- Wrela denies expensive inbound work when budget tokens are exhausted.

The `secure-web-stack` scope is successful when:

- Wrela can resolve a hostname through DNS.
- Wrela can complete a TCP exchange with a controlled host service.
- Wrela reports the selected AVX2 or AVX-512 crypto backend.
- Wrela reports which crypto backend is production-trusted, test-only, or
  hardware-authority-backed.
- Wrela rejects production-trusted generated crypto whose emitted code branches
  on secret-tainted values or uses them as memory addresses.
- Wrela has passing AES-GCM, SHA, HKDF, ML-KEM, X25519MLKEM768, and TLS
  key-schedule tests for each supported production backend.
- Wrela can complete a TLS 1.3 handshake using Wrela-owned hybrid
  post-quantum crypto.
- Wrela can perform an HTTP/1.1 HTTPS request.
- Later, Wrela can negotiate HTTP/2.
- Later, Wrela can run QUIC and HTTP/3 without replacing the lower stack.
