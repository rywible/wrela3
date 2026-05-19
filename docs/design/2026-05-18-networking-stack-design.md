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
- TLS crypto is Wrela-owned, not imported from a C TLS stack.
- TLS key exchange is quantum-safe from the first TLS milestone through a hybrid
  TLS 1.3 group using X25519 and ML-KEM-768.
- Bespoke TLS crypto is treated as a serious security project: it needs known
  test vectors, differential tests, transcript tests, negative tests, and
  timing-aware implementation review before any production claim.

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
- packet capture tooling beyond small debug reports
- production networking crypto on CPUs below the AVX2 fallback tier
- multiple NICs, multiple gateways, or multihomed routing in the first
  milestones
- QUIC before HTTP/1.1 compatibility exists

The design should not block those features. It should make each one an explicit
capability or protocol extension instead of a rewrite of the stack.

## Design Principles

### Whole-Machine Network Shape

Wrela should not expose ambient network access. The image source declares the
network shape it needs, and the compiler reports that shape.

The default model is closed:

- remote hostnames and ports are source-visible declarations
- local listening ports are source-visible declarations
- dynamic destination patterns require explicit authority
- connection, request, and stream budgets are derived from declarations and
  call-graph use where possible
- endpoint-specific TLS policy is compiled into the image

Conceptual source shape:

```wrela
data RemoteHttpsEndpoint {
    host: StringLiteral
    port: U16
    spki_hash: Bytes32
    allowed_group: TlsGroup
    allowed_cipher: TlsCipherSuite
    max_connections: U64
}
```

An `HttpClient` for a closed endpoint should not iterate a general cipher list
or trust arbitrary certificate roots. It should expect the declared endpoint,
declared trust material, declared TLS group, declared cipher suite, and declared
buffer budget. A request to a non-declared endpoint is a type error unless the
image explicitly holds dynamic network authority.

DNS can also be mostly compile-time for closed endpoints. The compiler can
resolve declared names during the build, embed the resolved addresses as
fallbacks, and report the resolution. Runtime DNS then refreshes or verifies the
route instead of being the first source of truth. Fully dynamic DNS remains
possible, but it is an explicit authority with its own budget and attack
surface.

### Network IO Core Owns Hardware

The first milestone uses a `NetworkExecutor` permanently placed on a dedicated
executor slot and CPU. It owns NIC hardware state:

- NIC driver path authority
- RX and TX descriptor rings
- DMA packet buffers
- link state
- ARP cache
- IPv4 address state

Other executors communicate with networking through typed request queues,
topics, or connection authorities. This keeps ownership reviewable and avoids
cross-core mutation of descriptor rings or protocol state.

The long-term model should not require one core to own all TCP, TLS, HTTP/2,
QUIC, and HTTP/3 work forever. The intended evolution is:

```text
NetworkIoExecutor:
  owns NIC rings, RX quarantine, TX completion, ARP, routing, and packet dispatch

Flow executors:
  own bounded TCP connections, TLS sessions, HTTP streams, or QUIC connections

Crypto executors, optional:
  own expensive handshake or bulk crypto work when the image declares them
```

The IO executor remains the truth owner for NIC queues and packet memory. Per-flow
executors receive narrowed authorities for specific flows and buffer leases. This
keeps the first core model simple while leaving a path to multiqueue, RSS, and
per-flow scaling without replacing the protocol stack.

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
    owner: NetworkIoExecutorSlot
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
next:

```text
QuarantinedRxBytes
  -> verify Ethernet bounds and destination
  -> VerifiedEthernetFrame
  -> verify IPv4 header, length, destination, checksum, no unsupported options
  -> VerifiedIpv4Packet
  -> verify UDP/TCP/ICMP length and checksum
  -> Verified transport payload
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

The first TCP listener must not be merely "bounded table or fail." It needs an
admission policy such as token-bucket SYN admission, SYN cookies, or an explicit
"not internet-facing without upstream filtering" declaration.

### Protocols Are Layered, Drivers Are Replaceable

The e1000e driver exposes an Ethernet device path. ARP, IPv4, ICMP, UDP, TCP,
TLS, HTTP, QUIC, and HTTP/3 consume generic packet and endpoint abstractions.

This keeps the second NIC driver from contaminating the protocol stack. Later
NIC families such as Intel `igb`, Realtek `r8169`, or `virtio-net` should only
need to implement the Ethernet device boundary.

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

### Compatibility Before Fancy Protocols

HTTP/1.1 over TLS is still the universal web compatibility path. HTTP/3 is
valuable, but an HTTP/3-only stack would miss a large part of the web. Wrela
should therefore build the boring universal path before QUIC/HTTP/3.

## Architecture

```text
Application executors
  |
  | typed network requests and responses
  v
Flow executors or NetworkExecutor-owned flows
  |
  +-- HTTP/3 over QUIC over UDP later
  |
  +-- HTTP/2 over TLS over TCP later
  |
  +-- HTTP/1.1 over TLS over TCP
  |
  +-- DNS over UDP
  |
  +-- DHCP over UDP
  |
  +-- ICMP over IPv4
  |
  v
NetworkIoExecutor on dedicated core
  |
  +-- ARP + IPv4 + UDP/TCP
  |
  v
Ethernet II frame layer
  |
  v
e1000e Ethernet device path
  |
  v
PCI BAR + DMA rings + MSI/MSI-X interrupt receiver
```

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
```

`QEMU_E1000E_DEVICE_ID` is a source constant added during implementation after
the QEMU device identity is observed in the test fixture. The driver should
start with that one known QEMU ID and only add real hardware IDs as they are
tested.

The resulting driver path is the only value that can read or write e1000e MMIO
registers or mutate descriptor rings.

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
    fn poll_rx(self) -> Option<EthernetFrame>
    fn transmit(self, frame: EthernetFrame) -> Result<Unit, TxFull>
    fn link_state(self) -> LinkState
    fn mac_address(self) -> MacAddress
}
```

The actual API can be smaller in the first implementation. The important
boundary is that the driver exposes Ethernet frames and link facts, not PCI
registers or descriptor ownership.

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
- program one RX queue
- program one TX queue
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
- explicit send API from the networking executor

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

## Milestone 10: TLS With Wrela Crypto

TLS gives HTTPS its security properties. Wrela will own its TLS crypto instead
of linking a foreign TLS library.

This is intentionally ambitious and must be treated as a security-critical
subsystem, not just another parser.

### TLS Version

The first supported version should be TLS 1.3 only.

TLS 1.2 and older versions are out of scope unless compatibility forces them
later. Starting at TLS 1.3 avoids legacy cipher suites, renegotiation, and many
old protocol hazards.

### Hybrid Post-Quantum Key Agreement

The first TLS milestone includes hybrid post-quantum key agreement. The default
Wrela-controlled endpoint group is:

```text
X25519MLKEM768
```

This combines classical X25519 with ML-KEM-768 so the connection is not relying
only on elliptic-curve discrete log assumptions. The implementation should track
the IETF TLS hybrid ECDHE-ML-KEM wire format and NIST ML-KEM semantics.

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

The first TLS milestone should also implement:

```text
TLS_AES_256_GCM_SHA384
TLS_CHACHA20_POLY1305_SHA256
```

`TLS_AES_256_GCM_SHA384` verifies the wider SHA-384/HMAC path. ChaCha20-Poly1305
is still useful for comparison, interop, and CPUs where AES acceleration is not
the chosen policy, but it is not the primary performance target for this design.

### Required Crypto Primitives

First TLS milestone:

- X25519 written to avoid secret-dependent branches and memory lookups on the
  supported backend
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
- code review focused on secret-dependent branches and table lookups
- explicit statement of what is and is not production-trusted

The first TLS milestone can be excellent engineering without being declared
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
authority-bearing network values:

```wrela
data NetworkEndpointAuthority {}
data TcpConnection {}
data TlsConnection {}
data HttpClient {}
data HttpRequest {}
data HttpResponse {}
```

Applications ask the networking executor to open or serve explicit endpoints.
The returned authority determines what the app can do.

Examples:

```text
HttpClient can issue requests through the network executor.
TcpConnection can send and receive bounded byte slices.
UdpEndpoint can send datagrams from one local port.
DnsClient can resolve names through configured resolver authority.
```

This preserves Wrela's capability model and avoids ambient global networking.

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
- interrupt vector and mode
- network executor slot and CPU placement
- network arena size and subdivisions
- RX/TX descriptor counts
- packet buffer counts and sizes
- packet quarantine pool size and verified packet lease budget
- cross-executor queue producers, consumers, capacities, and wake policies
- declared remote endpoints and local listening ports
- computed or declared connection budgets
- endpoint trust policy and pinned SPKI or certificate identities
- declared time, clock, and entropy authorities
- timer wheel or timer table capacities
- attack-surface summary and admission policies
- static or DHCP configuration mode
- supported protocols
- supported TLS groups
- supported TLS cipher suites
- post-quantum TLS policy
- required SIMD tier
- selected crypto backend
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

## Testing Strategy

### Compiler And Semantic Tests

- reject forged NIC, DMA, and network authority values
- reject duplicate NIC claims
- reject packet buffers escaping their arena lifetime
- reject network tables without bounded capacities
- reject use of quarantined bytes outside verifier functions
- reject cross-executor packet borrows without lease transfer
- reject closed endpoint requests to undeclared hostnames or ports
- reject TLS endpoints without clock and entropy authority
- reject TLS endpoints without declared trust material
- report network arena ownership in the image report

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
- packet quarantine promotion tests
- packet lease lifetime negative tests
- SIMD backend selection tests
- scalar-versus-AVX2 crypto differential tests
- AVX2-versus-AVX-512 crypto differential tests when host support exists
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
4. Add DMA ring memory shapes and reports.
5. Bring up e1000e reset/init/link/MAC reporting.
6. Prove RX/TX with raw Ethernet frames.
7. Add Ethernet II parse/emit helpers.
8. Add ARP request/reply and cache.
9. Add IPv4 parse/emit/checksum.
10. Add ICMP echo.
11. Add UDP.
12. Add time, clock, entropy, and bounded timer authorities.
13. Add DHCP, or keep static config if the next target is DNS/TCP.
14. Add DNS.
15. Add TCP.
16. Add compile-time network shape declarations and reports.
17. Add CPU feature discovery and vector-state policy for networking crypto.
18. Add AVX2 fallback crypto backends.
19. Add AVX-512 fast crypto backends.
20. Add ML-KEM-768 and X25519MLKEM768.
21. Add TLS 1.3 with Wrela crypto and build-time trust policy.
22. Add HTTP/1.1.
23. Add HTTP/2 if the use case requires it.
24. Add QUIC.
25. Add HTTP/3.

Every gate must have a QEMU or unit-test success criterion before the next layer
depends on it.

## Open Decisions

- Whether the first e1000e interrupt mode is MSI, MSI-X, or fallback-driven.
- Whether the first test network uses QEMU user networking, socket networking,
  TAP, or passt.
- Whether DHCP comes before DNS/TCP or static config remains the only path until
  HTTPS works.
- Which first real hardware target satisfies the AVX-512 fast tier.
- Whether vector code is permitted only in the network executor or also inside
  selected interrupt handlers with explicit XSAVE/XRSTOR policy.
- Whether HTTP/2 is still worth implementing before QUIC/HTTP/3 for the first
  real Wrela appliance target.
- Which closed endpoint policy syntax best expresses static endpoints and
  explicitly dynamic patterns.
- Which clock authority is acceptable for the first TLS image.
- Which entropy source is required before TCP, TLS, and QUIC leave local tests.
- Whether the first TLS verification target is a pinned local test server or a
  known external endpoint.
- Which real e1000e-family card is the first hardware target after QEMU.

## Success Criteria

The design is successful when:

- Wrela can boot under QEMU with `e1000e`.
- The networking executor owns all NIC and protocol state on its dedicated core.
- A host can ping the Wrela image.
- Wrela can complete a UDP exchange with a controlled host service.
- Wrela can resolve a hostname through DNS.
- Wrela can complete a TCP exchange with a controlled host service.
- Wrela rejects quarantined packet bytes outside the verifier path.
- Wrela reports declared endpoints, connection budgets, trust policy, and timer
  budgets.
- Wrela reports the selected AVX2 or AVX-512 crypto backend.
- Wrela has passing AES-GCM, SHA, HKDF, ML-KEM, X25519MLKEM768, and TLS
  key-schedule tests for each supported backend.
- Wrela can complete a TLS 1.3 handshake using Wrela-owned hybrid
  post-quantum crypto.
- Wrela can perform an HTTP/1.1 HTTPS request.
- Later, Wrela can negotiate HTTP/2.
- Later, Wrela can run QUIC and HTTP/3 without replacing the lower stack.
