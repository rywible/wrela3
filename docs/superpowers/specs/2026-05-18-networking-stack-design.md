# Wrela Networking Stack Design

## Purpose

Wrela should grow a native networking stack in the same style as the rest of
the platform: source-visible authority, bounded memory, explicit executor
placement, and no hidden operating-system substrate.

The long-term stack is:

```text
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
QEMU e1000e + static IPv4 + ARP + ICMP echo
```

That proves PCI device discovery, BAR ownership, DMA rings, interrupt delivery,
packet buffers, Ethernet parsing, IPv4 checksums, and one visible network
behavior before TCP, TLS, DNS, or HTTP enter the system.

## Assumptions

- The first NIC target is QEMU `e1000e`.
- The likely real-hardware follow-up is one Intel e1000e-family PCIe gigabit
  controller, preferably an 82574L-class card if available.
- The first image uses static IPv4 configuration. DHCP is a later step.
- Networking owns a dedicated executor on its own core.
- Applications do not directly touch NIC MMIO, descriptor rings, DMA buffers,
  or raw mutable packet memory.
- TLS crypto is Wrela-owned, not imported from a C TLS stack.
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
- production CA-bundle management in the first TLS milestone
- QUIC before HTTP/1.1 compatibility exists

The design should not block those features. It should make each one an explicit
capability or protocol extension instead of a rewrite of the stack.

## Design Principles

### One Core Owns Networking

A `NetworkExecutor` is permanently placed on a dedicated executor slot and CPU.
It owns:

- NIC driver path authority
- RX and TX descriptor rings
- DMA packet buffers
- link state
- ARP cache
- IPv4 address state
- UDP endpoints
- DNS transaction state
- TCP connection state
- TLS sessions
- HTTP client/server state
- QUIC connections later

Other executors communicate with networking through typed request queues,
topics, or connection authorities. This keeps ownership reviewable and avoids
cross-core mutation of descriptor rings or protocol state.

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

### Protocols Are Layered, Drivers Are Replaceable

The e1000e driver exposes an Ethernet device path. ARP, IPv4, ICMP, UDP, TCP,
TLS, HTTP, QUIC, and HTTP/3 consume generic packet and endpoint abstractions.

This keeps the second NIC driver from contaminating the protocol stack. Later
NIC families such as Intel `igb`, Realtek `r8169`, or `virtio-net` should only
need to implement the Ethernet device boundary.

### Prefer Correct Scalar Paths Before Offloads

The first stack computes checksums and performs protocol work in Wrela source.
NIC offloads are intentionally deferred. This makes packet bytes, checksums,
and protocol state visible during bring-up and reduces the number of device
features that can hide bugs.

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
NetworkExecutor on dedicated core
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
- zero-copy across app boundaries

The first TCP stack should choose a small, correct congestion-control story
instead of pretending to be a mature internet stack. It can begin with
conservative slow start and loss response suitable for QEMU and local network
tests.

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

### First Cipher Suite

The first recommended cipher suite is:

```text
TLS_CHACHA20_POLY1305_SHA256
```

Reason: it can be implemented as a fast scalar path without requiring XMM state
management. Wrela's current runtime has not yet made a broad interrupt save
policy for FPU/SSE/AVX state. AES-NI and PCLMULQDQ are desirable later, but
they use vector registers and should wait until the CPU/interrupt policy can
save and restore that state safely.

Later cipher suites:

```text
TLS_AES_128_GCM_SHA256
TLS_AES_256_GCM_SHA384
```

These become attractive after Wrela has an explicit SIMD/AES state policy.

### Required Crypto Primitives

First TLS milestone:

- X25519 written to avoid secret-dependent branches and memory lookups on the
  supported backend
- HKDF-SHA256
- SHA-256
- HMAC-SHA256
- ChaCha20
- Poly1305
- AEAD ChaCha20-Poly1305
- transcript hashing
- TLS 1.3 key schedule
- certificate signature verification for one chosen signature family

Certificate verification can start with a pinned certificate or pinned public
key. A public CA trust store is a separate milestone.

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
- static or DHCP configuration mode
- supported protocols
- supported TLS cipher suites
- supported HTTP versions

Runtime diagnostics should include bounded counters:

- RX packets
- TX packets
- RX drops by reason
- TX drops by reason
- ARP hits and misses
- IPv4 checksum failures
- UDP packets by endpoint
- TCP retransmits
- TLS handshake failures
- HTTP request count

## Testing Strategy

### Compiler And Semantic Tests

- reject forged NIC, DMA, and network authority values
- reject duplicate NIC claims
- reject packet buffers escaping their arena lifetime
- reject network tables without bounded capacities
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
- HTTP parser bounds

### QEMU End-to-End Tests

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
2. Add DMA ring memory shapes and reports.
3. Bring up e1000e reset/init/link/MAC reporting.
4. Prove RX/TX with raw Ethernet frames.
5. Add Ethernet II parse/emit helpers.
6. Add ARP request/reply and cache.
7. Add IPv4 parse/emit/checksum.
8. Add ICMP echo.
9. Add UDP.
10. Add DHCP, or keep static config if the next target is DNS/TCP.
11. Add DNS.
12. Add TCP.
13. Add TLS 1.3 with Wrela crypto.
14. Add HTTP/1.1.
15. Add HTTP/2.
16. Add QUIC.
17. Add HTTP/3.

Every gate must have a QEMU or unit-test success criterion before the next layer
depends on it.

## Open Decisions

- Whether the first e1000e interrupt mode is MSI, MSI-X, or fallback-driven.
- Whether the first test network uses QEMU user networking, socket networking,
  TAP, or passt.
- Whether DHCP comes before DNS/TCP or static config remains the only path until
  HTTPS works.
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
- Wrela can complete a TLS 1.3 handshake using Wrela-owned crypto.
- Wrela can perform an HTTP/1.1 HTTPS request.
- Later, Wrela can negotiate HTTP/2.
- Later, Wrela can run QUIC and HTTP/3 without replacing the lower stack.
