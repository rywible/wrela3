# Networking Stack Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build Wrela's native networking stack from substrate through application protocols: e1000e bring-up, IPv4, DHCP, DNS, TCP, TLS, HTTP/1.1, HTTP/2, QUIC, HTTP/3, endpoint information-flow controls, production crypto validation, hardware QoS, and real e1000e-family hardware support.

**Architecture:** One dedicated `NetworkReactor` executor owns NIC MMIO, descriptor rings, DMA packet buffers, ARP state, IPv4 state, ICMP, UDP endpoints, network timers, drop counters, and the flight recorder. Higher protocols run through bounded endpoint authorities layered on TCP, UDP, TLS, and QUIC; no application receives raw NIC authority, descriptor authority, DMA authority, mutable packet buffers, or secret key material. The first QEMU harness uses rootless socket-backed Ethernet; later hardware tasks add explicit device-family support without changing protocol code.

**Tech Stack:** Go 1.22+; existing Wrela lexer/parser/semantic checker/IR/x86_64 codegen; Wrela modules under `wrela/`; QEMU q35 + OVMF + `-device e1000e`; Go packet trace tests; Go QEMU e2e tests with a socket-backed Ethernet peer.

---

## Hard Start Gate

**Do not start Task 1 from the current base branch unless Task 0 passes.** This plan intentionally depends on the NVMe event storage substrate worktree `codex/nvme-event-storage` at `/Users/ryanwible/.config/superpowers/worktrees/wrela3/codex-nvme-event-storage`. That worktree contributes the shared QEMU `Options.ExtraArgs` substrate and storage diagnostic range `SEM0099-SEM0124`. Networking must consume those landed shared changes; it must not duplicate them.

The expected merge order is:

```text
1. Land codex/nvme-event-storage.
2. Rebase or branch networking from the post-storage target branch.
3. Run Task 0.
4. Start Task 1 only after Task 0 passes.
```

Runtime tasks also require a worker environment with QEMU and OVMF installed. A QEMU-dependent runtime task is not complete if its only runtime proof skips.

## 0. How To Execute This Plan

**Description:** This plan is written so a junior engineer can implement one task without reopening network design choices. Tasks 0-24 deliver `network-substrate-v0`. Tasks 25-41 extend that substrate into DHCP, DNS, TCP, TLS, HTTP/1.1, HTTP/2, QUIC, HTTP/3, endpoint information-flow control, vector crypto, constant-time validation, hardware QoS, and real e1000e-family cards.

**Acceptance Criteria:**

- Every task has exact file ownership, prerequisites, failing-test workflow, implementation examples, verification commands, and commit command.
- Every task commit message ends with `-Codex Automated`.
- No task changes names, constants, diagnostics, report JSON keys, QEMU harness shape, or Wrela public APIs from this document.
- If a task's failing-test step passes before implementation, stop and inspect the current implementation before editing.
- If a task's passing-test step fails, keep working inside that task until the listed command passes.
- Substrate completion requires `go test ./...` plus the network e2e tests listed in Task 24.
- Full stack completion requires the additional protocol and production acceptance commands listed in Task 41.
- Runtime task completion requires QEMU/OVMF availability. A worker may skip local exploration when QEMU is absent, but they cannot check off or commit a runtime task until the listed e2e command runs and passes on a QEMU-capable machine.

**Code Example:**

```bash
go test ./compiler/nettrace -run TestEthernetArpIpv4IcmpTrace -v
go test ./tests/e2e -run NetworkSubstrate -v
git diff --check
git add compiler/nettrace wrela/net wrela/machine/x86_64/e1000e.wrela tests/e2e/network_substrate_helpers_test.go tests/e2e/network_driver_qemu_test.go tests/e2e/network_protocol_qemu_test.go
git commit -m "feat: add network substrate v0 -Codex Automated"
```

Definition of done for one task:

- The failing test step fails with the expected missing symbol, diagnostic, report key, or serial marker.
- The implementation step changes only files listed by the task.
- The passing test step passes.
- `git diff --check` passes.
- The exact commit shown in the task succeeds.

Definition of done for the substrate milestone:

- `go test ./...` passes.
- `go test ./compiler/nettrace -v` passes.
- `go test ./tests/e2e -run NetworkSubstrate -v` passes on a machine with QEMU/OVMF available.
- The generated network report includes NIC identity, BAR ownership, DMA policy, descriptor counts, packet buffer counts, interrupt mode, static IPv4 config, reactor executor placement, lane budgets, drop counters, and flight recorder capacity.
- This placeholder scan exits 0:

Definition of done for the full application-stack milestone:

- Task 41 acceptance commands pass.
- DHCP, DNS, TCP, TLS, HTTP/1.1, HTTP/2, QUIC, HTTP/3, endpoint flow, vector crypto, constant-time validation, QoS, and real e1000e-family hardware profile support are all represented in report JSON.
- The hardware profile test skips by default and passes on a dedicated e1000e-family host with `WRELA_E1000E_HARDWARE_PROFILE=1`.
- No application task changes substrate constants, e1000e QEMU harness shape, or packet authority/lifetime rules from Tasks 0-24.
- This placeholder scan exits 0:

```bash
bad_terms='TO''DO|TB''D|fill'' in|implement'' later|Add'' appropriate|similar'' to'' Task|if'' needed|or'' equivalent'
base=$(git merge-base HEAD main 2>/dev/null || git merge-base HEAD origin/main)
changed=$(git diff --name-only "$base"...HEAD)
if [ -n "$changed" ] && git diff -U0 "$base"...HEAD -- $changed | rg -n "^\+.*($bad_terms)"; then
  exit 1
fi
```

### Task 0: Shared Infrastructure Preconditions

**Files:**

- No edits in this task.

**Prerequisites:** The NVMe event storage substrate worktree `codex/nvme-event-storage` has landed into the target branch before this networking plan begins.

**Description:** Verify shared infrastructure that networking intentionally reuses instead of redefining. This task prevents a junior engineer from making local judgment calls about cross-plan merge order, diagnostic ranges, or Wrela syntax support.

**Acceptance Criteria:**

- `compiler/qemu.Options` already has `ExtraArgs []string`.
- `compiler/qemu.Args` already appends `opts.ExtraArgs` at the end of generated QEMU arguments.
- `go test ./compiler/qemu -run TestArgsAppendsExtraArgs -v` passes.
- `compiler/diag/codes.go` already contains storage diagnostics through `SEM0124`; networking uses `SEM0167-SEM0184`.
- Existing parser tests prove `driver path`, `executor`, `start fn`, and `static_assert` syntax before any networking source uses them.
- Existing generic type and topic tests prove `Topic<T>` and `ReliableTopic<T>` work before `NetworkQueue<T>` is added.
- If any command below fails, stop and land the shared storage substrate prerequisite first. Do not implement `ExtraArgs` inside a networking task.

**Code Example:**

```bash
go test ./compiler/qemu -run TestArgsAppendsExtraArgs -v
go test ./compiler/parse -run 'Parse|DriverPath|Executor' -v
go test ./compiler/sem -run 'StaticAssert|TopicPayload|TopicGraph' -v
rg -n 'SEM0124|ExtraArgs' compiler/diag/codes.go compiler/qemu/run.go
```

**Steps:**

- [ ] Run: `go test ./compiler/qemu -run TestArgsAppendsExtraArgs -v`
  Expected: PASS.
- [ ] Run: `go test ./compiler/parse -run 'Parse|DriverPath|Executor' -v`
  Expected: PASS.
- [ ] Run: `go test ./compiler/sem -run 'StaticAssert|TopicPayload|TopicGraph' -v`
  Expected: PASS.
- [ ] Run: `rg -n 'ExtraArgs' compiler/qemu/run.go`
  Expected: output includes `ExtraArgs []string` and `args = append(args, opts.ExtraArgs...)`.
- [ ] Run: `rg -n 'SEM0124' compiler/diag/codes.go`
  Expected: output includes storage's final reserved diagnostic.
- [ ] Continue to Task 1 only after all checks pass.

---

## 1. Frozen Networking Decisions

**Description:** These decisions are implementation inputs. Do not reopen them during task execution.

**Acceptance Criteria:**

- Tests and source use these exact constants, names, and policies.
- Any proposed change requires a separate design update before implementation continues.
- Later web-stack decisions are implemented only in Tasks 25-41 and must not change Task 24 substrate acceptance.

**Code Example:**

```wrela
const QEMU_E1000E_VENDOR_ID: U16 = 0x8086
const QEMU_E1000E_DEVICE_ID: U16 = 0x10D3
const NETWORK_ARENA_BYTES: U64 = 0x400000
const NETWORK_DMA_BYTES: U64 = 0x100000
const NETWORK_RX_DESC_COUNT: U64 = 64
const NETWORK_TX_DESC_COUNT: U64 = 64
const NETWORK_PACKET_BUFFER_BYTES: U64 = 2048
const NETWORK_ARP_TABLE_ENTRIES: U64 = 16
const NETWORK_UDP_ENDPOINTS: U64 = 8
const NETWORK_FLIGHT_RECORDER_ENTRIES: U64 = 128
```

Fixed decisions:

- Scope for Tasks 0-24 is `network-substrate-v0`: packet trace harness, QEMU `e1000e`, Ethernet II, ARP, static IPv4, ICMP echo, UDP, bounded report/diagnostics.
- Scope for Tasks 25-41 is `network-application-v1`: DHCPv4, DNS over UDP, TCP over IPv4, TLS 1.3, HTTP/1.1, HTTP/2, QUIC v1, HTTP/3, endpoint data-class information flow, vector crypto backend selection, production constant-time validation, hardware QoS, and real e1000e-family cards.
- First NIC is QEMU `e1000e`, PCI vendor `0x8086`, device `0x10D3`.
- First real-hardware device IDs are added only in Task 40 after hardware profile tests exist.
- First network harness is rootless QEMU socket networking:
  `-netdev socket,id=net0,udp=127.0.0.1:<peerPort>,localaddr=127.0.0.1:<guestPort> -device e1000e,netdev=net0,mac=52:54:00:12:34:56`.
- TAP, passt, user-mode NAT, bridge networking, and host OS `ping` are not used by v0 tests.
- The Go test peer is the controlled host. It sends and receives raw Ethernet frames over UDP datagrams.
- Static guest MAC is requested from QEMU as `52:54:00:12:34:56`; the driver still reads the MAC from device registers and fails the e2e test if the report disagrees.
- Static guest IPv4 is `10.10.0.2`.
- Static host peer IPv4 is `10.10.0.1`.
- Static subnet mask is `255.255.255.0`.
- Static gateway is `10.10.0.1`.
- Static DNS server is reported as `10.10.0.1`; DNS protocol is implemented in Task 27.
- One dedicated network executor runs on `ExecutorSlot(id = 1)` in the network fixture.
- The console executor remains `ExecutorSlot(id = 0)` and only receives serial/status output.
- Network reactor loop policy is `HotPollPolicy` for v0. Interrupt delivery is still programmed and reported, but the loop polls RX/TX every iteration so QEMU interrupt timing cannot hide driver bugs.
- First interrupt mode is MSI when the QEMU device exposes MSI. MSI-X is excluded for networking in this plan.
- DMA IOMMU containment is reported as `iommu_enforced = false`, `trusted_dma_device = true` under QEMU.
- DMA memory type is reported as `coherent_wb_qemu`.
- MMIO ordering policy is reported as `x86_strong_mmio_plus_compiler_barrier`.
- All NIC register access uses existing `MmioRegion.read32` and `MmioRegion.write32` asm methods from `platform.hardware.bytes`; do not cache register values across polling-loop iterations.
- RX and TX rings use legacy 16-byte e1000 descriptors.
- RX descriptor count and TX descriptor count are both `64`.
- RX/TX packet buffers are fixed `2048` byte buffers.
- Ethernet MTU is `1500`; jumbo frames, multicast groups, checksum offload, RSS, LRO, TSO, and multiqueue are out of scope. Task 39 adds only VLAN PCP tagging for QoS, not VLAN interface support.
- IPv4 fragmentation and options are dropped.
- UDP zero checksum is accepted only for IPv4 receive; emitted UDP packets always include a checksum.
- ARP table has 16 fixed FIFO slots.
- UDP endpoint table has 8 fixed slots keyed by local port.
- v0 exposes one UDP echo endpoint on port `7` for e2e verification.
- Flight recorder has 128 metadata records and never stores packet payload bytes.
- Boot-fatal network codes reserve `0xAC090000-0xAC09FFFF`.
- New semantic diagnostics reserve `SEM0167-SEM0184`. Storage substrate work uses `SEM0099-SEM0124` and the realtime desktop plan reserves `SEM0125-SEM0166`; do not reuse those codes.

Exact e1000e v0 bit constants:

```wrela
const E1000_CTRL_RST: U32 = 0x04000000
const E1000_STATUS_LU: U32 = 0x00000002
const E1000_RCTL_EN: U32 = 0x00000002
const E1000_RCTL_BAM: U32 = 0x00008000
const E1000_RCTL_BSIZE_2048: U32 = 0x00000000
const E1000_RCTL_SECRC: U32 = 0x04000000
const E1000_TCTL_EN: U32 = 0x00000002
const E1000_TCTL_PSP: U32 = 0x00000008
const E1000_TCTL_CT_16: U32 = 0x00000100
const E1000_TCTL_COLD_64: U32 = 0x00040000
const E1000_ICR_TXDW: U32 = 0x00000001
const E1000_ICR_LSC: U32 = 0x00000004
const E1000_ICR_RXT0: U32 = 0x00000080
const E1000_IMS_TXDW: U32 = 0x00000001
const E1000_IMS_LSC: U32 = 0x00000004
const E1000_IMS_RXT0: U32 = 0x00000080
const E1000_RXD_STAT_DD: U8 = 0x01
const E1000_RXD_STAT_EOP: U8 = 0x02
const E1000_TXD_STAT_DD: U8 = 0x01
const E1000_TX_CMD_EOP: U8 = 0x01
const E1000_TX_CMD_IFCS: U8 = 0x02
const E1000_TX_CMD_RS: U8 = 0x08
const E1000_DESC_BYTES: U64 = 16
```

Descriptor ownership states:

```text
RX host_empty:
  descriptor status = 0, buffer address points at a network DMA packet buffer

RX device_owned:
  descriptor is between hardware RDH and software RDT

RX completed:
  E1000_RXD_STAT_DD is set; E1000_RXD_STAT_EOP must also be set for v0

RX quarantined:
  descriptor bytes are represented by QuarantinedRxBytes and cannot be returned
  to hardware until the reactor releases or recycles the lease

TX free:
  descriptor has never been posted or status has E1000_TXD_STAT_DD set

TX device_owned:
  descriptor was posted with EOP | IFCS | RS and lies between tx_clean_head and
  tx_next_to_use in software ring order

TX completed:
  descriptor status has E1000_TXD_STAT_DD set and can be reclaimed
```

Network arena layout:

```text
root child "network.core" length 0x400000
  network DMA domain length 0x100000
    RX descriptor bytes: 64 * 16 = 1024
    TX descriptor bytes: 64 * 16 = 1024
    RX packet buffers: 64 * 2048 = 131072
    TX packet buffers: 64 * 2048 = 131072
  network executor memory length 0x100000
  remaining bytes reserved for ARP, UDP, recorder, and future protocol tables
```

Boot-fatal network codes:

```text
0xAC090001  e1000e reset did not clear before timeout
0xAC090002  e1000e link did not come up before timeout
0xAC090003  RX descriptor ring address or length is invalid
0xAC090004  TX descriptor ring address or length is invalid
0xAC090005  RX packet buffer allocation exceeds DMA arena
0xAC090006  TX packet buffer allocation exceeds DMA arena
0xAC090007  e1000e MAC address is all zero or multicast
0xAC090008  MSI route for network device is unavailable
0xAC090009  network static IPv4 config is invalid
0xAC09000A  network reactor memory arena is missing
```

Application-stack fixed decisions:

- DHCP is DHCPv4 only. DHCPv6 is out of scope until IPv6 exists.
- DNS is UDP A-record lookup only. CNAME following is one hop. TCP fallback, DNSSEC, DoT, and DoH are out of scope.
- TCP v1 supports one active-open connection and one passive listener per fixture, fixed MSS `1460`, in-order receive, no SACK, no timestamps, no window scaling, and no urgent data.
- TLS is TLS 1.3 only with `TLS_AES_128_GCM_SHA256`, X25519, HKDF-SHA256, and pinned SPKI validation. TLS 1.2 and public WebPKI path building are out of scope.
- HTTP/1.1 supports GET with `Connection: close` and `Content-Length`. Chunked transfer is rejected.
- HTTP/2 supports ALPN `h2`, static-table HPACK, one client stream, SETTINGS, HEADERS, DATA, RST_STREAM, and GOAWAY. Dynamic HPACK and server push are out of scope.
- QUIC is QUIC v1 only, one connection, one bidirectional stream, Initial/Handshake/1-RTT packet protection, ACK, CRYPTO, STREAM, and CONNECTION_CLOSE frames.
- HTTP/3 supports static-table QPACK, SETTINGS, one GET stream, HEADERS, DATA, and FIN. Dynamic QPACK, DATAGRAM, WebTransport, CONNECT-UDP, and push are out of scope.
- Endpoint data classes are `public`, `telemetry`, `internal`, `secret`, `credential`, and `key_material`.
- Secret, credential, and key-material endpoint data must use TLS or QUIC. Cleartext HTTP with those classes is semantically rejected.
- Crypto backends are `scalar`, `aesni_pclmul`, `avx2_aesni_pclmul`, and `avx512_vaes_vpclmul`; selection is based only on CPU feature facts, OS-enabled XCR0 vector state, and is reported.
- Production constant-time validation rejects secret-dependent branches and secret-dependent memory indices in crypto, TLS, and QUIC source. It also validates emitted crypto/TLS/QUIC code for secret-dependent conditional branches, secret-dependent indexed loads, and forbidden variable-latency opcodes.
- Hardware QoS maps endpoint class to four TX classes: `control`, `interactive`, `best_effort`, and `bulk`. VLAN PCP emission is optional and does not imply VLAN interface support.
- Real e1000e-family support is opt-in through hardware profile tests and begins with tested PCI device IDs `0x10D3`, `0x1502`, `0x153A`, and `0x155A`.

Design items beyond `network-application-v1`:

- Hybrid post-quantum TLS key agreement groups, ML-KEM, and ML-DSA are not part of Tasks 25-41. Task 30C rejects those groups with a named unsupported-feature error rather than silently falling back.
- TLS cipher suites beyond `TLS_AES_128_GCM_SHA256`, including AES-256-GCM-SHA384 and ChaCha20-Poly1305, are not part of Tasks 25-41. Task 30E reports the single enabled cipher suite.
- DNSSEC, DoT, DoH, IPv6, DHCPv6, dynamic HPACK/QPACK, QUIC DATAGRAM, WebTransport, and CONNECT-UDP stay outside this plan.

---

## 2. Repository Layout And File Responsibilities

**Description:** Create or modify exactly these files unless a task explicitly narrows the write set further. Each file has one responsibility so workers can take tasks independently.

**Acceptance Criteria:**

- Every file listed here is either created or intentionally modified by a task.
- No task performs unrelated refactors in these files.
- Source shape tests pin the Wrela public API before runtime behavior depends on it.

**Code Example:**

```text
compiler/diag/codes.go
  Adds networking diagnostics SEM0167-SEM0184.

compiler/report/report.go
compiler/report/report_test.go
  Adds network report and network authority audit JSON fields.

compiler/sem/image_graph.go
compiler/sem/network_graph.go
compiler/sem/network_graph_test.go
compiler/sem/report.go
compiler/sem/report_test.go
compiler/sem/hardware_claim_test.go
compiler/sem/endpoint_flow_test.go
compiler/sem/constant_time.go
compiler/sem/constant_time_test.go
compiler/codegen/crypto_backend_test.go
compiler/codegen/constant_time_binary_test.go
  Records network graph facts, rejects duplicate NIC/DMA claims, rejects forged
  network authority values, enforces endpoint data-class flow and constant-time
  crypto boundaries, and emits report data.

compiler/nettrace/packet.go
compiler/nettrace/packet_test.go
compiler/nettrace/trace.go
compiler/nettrace/trace_test.go
compiler/nettrace/checksum.go
compiler/nettrace/checksum_test.go
compiler/nettrace/dhcp.go
compiler/nettrace/dhcp_test.go
compiler/nettrace/dns.go
compiler/nettrace/dns_test.go
compiler/nettrace/tcp.go
compiler/nettrace/tcp_test.go
compiler/nettrace/tls.go
compiler/nettrace/tls_test.go
compiler/nettrace/http1.go
compiler/nettrace/http1_test.go
compiler/nettrace/http2.go
compiler/nettrace/http2_test.go
compiler/nettrace/quic.go
compiler/nettrace/quic_test.go
compiler/nettrace/http3.go
compiler/nettrace/http3_test.go
  Host-side packet trace harness for deterministic Ethernet/ARP/IPv4/ICMP/UDP
  plus DHCP/DNS/TCP/TLS/HTTP/QUIC parser and emitter tests.

compiler/qemu/e1000e_peer.go
compiler/qemu/e1000e_peer_test.go
  Adds QEMU e1000e socket netdev argument helper and a rootless raw Ethernet
  peer. Generic `qemu.Options.ExtraArgs` comes from Task 0's shared substrate.

wrela/net/types.wrela
wrela/net/config.wrela
wrela/net/packet.wrela
wrela/net/ethernet.wrela
wrela/net/arp.wrela
wrela/net/ipv4.wrela
wrela/net/icmp.wrela
wrela/net/udp.wrela
wrela/net/dhcp.wrela
wrela/net/dns.wrela
wrela/net/tcp.wrela
wrela/net/tls.wrela
wrela/net/http1.wrela
wrela/net/http2.wrela
wrela/net/quic.wrela
wrela/net/http3.wrela
wrela/net/endpoint_flow.wrela
wrela/net/qos.wrela
wrela/net/flight_recorder.wrela
wrela/net/reactor.wrela
  Defines generic networking types, static config, packet typestates, protocol
  parsing/emission, bounded tables, application protocols, endpoint policy,
  QoS policy, recorder, and reactor loop.

wrela/crypto/types.wrela
wrela/crypto/sha256.wrela
wrela/crypto/hkdf.wrela
wrela/crypto/aes_gcm.wrela
wrela/crypto/x25519.wrela
wrela/crypto/backend.wrela
  Defines secret byte types, cryptographic primitives, TLS/QUIC key schedule
  support, and scalar/vector backend selection.

wrela/machine/x86_64/e1000e.wrela
wrela/machine/x86_64/e1000e_family.wrela
  Owns e1000e constants, descriptors, MMIO register access, reset/init, rings,
  RX quarantine, TX frame submission, interrupt acknowledgement, hardware QoS,
  and real e1000e-family support table.

wrela/platform/hardware/memory.wrela
  Adds DmaPolicy, DmaDomainAuthority, and DMA byte read/write helpers used by
  e1000e. Existing arena behavior remains unchanged.

wrela/machine/x86_64/cpu_state.wrela
wrela/platform/hardware/discovery.wrela
examples/hello/main.wrela
  Adds network plan fields without changing existing hello behavior.

tests/e2e/network_substrate_helpers_test.go
  Owns shared QEMU dependency discovery, UDP port reservation, fixture build,
  serial waits, report assertions, and peer expectation helpers.

tests/e2e/network_driver_qemu_test.go
  Owns e1000e reset, MAC/link, RX quarantine, TX frame, and MSI e2e tests.

tests/e2e/network_protocol_qemu_test.go
  Owns Ethernet, ARP, IPv4, ICMP, UDP, and consolidated substrate protocol e2e tests.

tests/e2e/network_runtime_qemu_test.go
  Owns reactor, timer, recorder, and network report e2e tests.

tests/e2e/network_dhcp_dns_qemu_test.go
  Owns DHCP and DNS e2e tests.

tests/e2e/network_tcp_http_qemu_test.go
  Owns TCP, TLS, HTTP/1.1, HTTP/2, and HTTP-over-TCP e2e tests.

tests/e2e/network_quic_http3_qemu_test.go
  Owns QUIC and HTTP/3 e2e tests.

tests/e2e/network_qos_qemu_test.go
  Owns QoS e2e tests.

tests/e2e/fixtures/network_substrate/main.wrela
tests/e2e/fixtures/network_substrate/program.wrela
  Builds and boots the network fixture under QEMU with e1000e and exercises
  substrate plus application protocol scenarios. The fixture files are shared;
  tests are split by protocol family to avoid one serial e2e file.

tests/hardware/e1000e_profile_test.go
  Opt-in real e1000e-family hardware profile tests.

docs/network-stack-status.md
  Records substrate and application-stack implementation status.
```

---

## 3. Parallel Work Map

**Description:** The plan is split into a substrate milestone and application-stack tracks. True parallelism exists only where file ownership is disjoint. Host-side trace packages and source-shape shells can run ahead of QEMU e2e integration; edits to `wrela/net/reactor.wrela`, `wrela/machine/x86_64/e1000e.wrela`, and `compiler/sem/report.go` are serial merge points. E2E tests are split by family so protocol workers do not all edit one file.

**Acceptance Criteria:**

- Task 0 passes before Task 1 starts.
- Task 1 lands before tasks that use diagnostics or report fields.
- Tasks 2 and 3 may run in parallel after Task 1.
- Tasks 4, 5, 6, and 7 are serial because they share compiler semantic and source contract files.
- Tasks 8, 9A, 9B, 9C, 10, 11, and 12 are serial because they share `wrela/machine/x86_64/e1000e.wrela`.
- Host-side protocol fixtures in Tasks 13-17 may be developed after Tasks 3 and 4, but Wrela reactor integration for those tasks lands serially.
- Task 25 unlocks the application-stack shell and can begin after Tasks 4 and 17; its source-shape work lets Tracks H-K proceed without naming drift.
- Track H dynamic configuration is Tasks 26 -> 27 and shares `wrela/net/reactor.wrela`, so it merges serially.
- Track I TCP and HTTP-over-TCP is Task 28 -> Tasks 30A-30E -> Tasks 31/32 -> Task 33. Task 29 crypto foundations can run in parallel with Task 28. Task 30A waits for Tasks 28 and 29.
- Track J QUIC and HTTP/3 is Tasks 34A-34F -> Task 35 and depends on UDP plus Task 29 crypto foundations, not on HTTP/1.1 or HTTP/2.
- Track K policy, production crypto, QoS, and hardware is Task 36 plus Tasks 37A-37E -> 38A-38F after Task 21B report substrate, then Task 39 QoS and Task 40 real hardware. Task 40 hardware discovery notes can be drafted after Task 12, but code merges after Task 39 because both touch e1000e/report files.
- Task 41 is the full application-stack sweep and waits for Tasks 26-29, 30A-30E, 31-33, 34A-34F, 35-36, 37A-37E, 38A-38F, and 39-40.
- Tasks touching the same file in the contention table below must not run concurrently.

**Code Example:**

```text
Gate A: Task 0, then Task 1
  Verify shared QEMU substrate, then freeze diagnostics and report JSON.

Gate B: Tasks 2-3 can run after Task 1
  QEMU harness and host packet trace package.

Gate C: Tasks 4 -> 5 -> 6 -> 7
  Wrela network source shell, DMA domain, fixture wiring, semantic guards.

Gate D: Tasks 8 -> 9A -> 9B -> 9C -> 10 -> 11 -> 12
  e1000e driver and QEMU boot/RX/TX proof.

Gate E host fixtures: Tasks 13-17 host-side nettrace portions after Tasks 3 and 4
Gate E runtime integration: Tasks 13 -> 14 -> 15 -> 16 -> 17 after Tasks 10 and 11
  Ethernet, ARP, IPv4, ICMP, UDP. Host packet fixtures can run ahead of e1000e.

Gate F: Tasks 18 -> 19 -> 20 -> 21A -> 21B
  Reactor, timers, epochs, recorder, report.

Gate G: Tasks 22-24 after all earlier tasks
  Substrate e2e, status docs, substrate acceptance sweep.

Gate H: Task 25 after Tasks 4 and 17
  Application-stack source contracts for DHCP, DNS, TCP, TLS, HTTP, QUIC,
  endpoint flow, QoS, and crypto.

Gate I dynamic config: Tasks 26 -> 27 after Tasks 17, 19, 20, 21B, and 25
  DHCP then DNS. Host nettrace can start after Task 25; reactor integration is serial.

Gate J stream and HTTP-over-TCP:
  Task 28 after Tasks 15 and 25
  Task 29 after Task 25, in parallel with Task 28
  Tasks 30A -> 30B -> 30C -> 30D -> 30E after Tasks 28 and 29
  Tasks 31 and 32 after Task 30E

Gate K QUIC and HTTP/3:
  Tasks 34A -> 34B -> 34C -> 34D -> 34E -> 34F after Tasks 17 and 29
  Task 35 after Task 34F

Gate L production policy and hardware:
  Task 36 after Tasks 33 and 35
  Tasks 37A -> 37B -> 37C -> 37D -> 37E after Tasks 21B and 29
  Tasks 38A -> 38B -> 38C -> 38D -> 38E -> 38F after Task 37E
  Task 39 after Tasks 36 and 38F; Task 40 after Tasks 12 and 39 because both touch e1000e/report

Gate M: Task 41 after Tasks 26-29, 30A-30E, 31-33, 34A-34F, 35-36, 37A-37E, 38A-38F, and 39-40
  Full application-stack acceptance sweep.
```

File contention map:

```text
compiler/qemu/e1000e_peer.go
  Task 2 only.

compiler/nettrace/trace.go and compiler/nettrace/trace_test.go
  Tasks 3, 14, 15, 16, 17, 22. Host fixture work may branch in parallel, but
  these files merge one protocol at a time in task order.

compiler/nettrace/dhcp.go, dns.go, tcp.go, tls.go, http1.go, http2.go, quic.go, http3.go
  Tasks 26, 27, 28, 30A-30E, 31, 32, 34A-34F, 35. These are disjoint files and can be
  authored in parallel after Task 25 when their prerequisites are met.

wrela/machine/x86_64/e1000e.wrela
  Tasks 8, 9A, 9B, 9C, 10, 11, 12, 39. Strictly serial.

wrela/net/reactor.wrela
  Tasks 14, 15, 16, 17, 18, 19, 20, 26, 27, 28, 34. Strictly serial.

wrela/crypto/*.wrela
  Tasks 29, 30B, 30D, 34A, 34D, 37A-37D, 38A. Task 29 lands first; Task 38A
  taint roots land after backend selection in Task 37E.

wrela/net/http1.wrela, http2.wrela, http3.wrela
  Tasks 31, 32, 35, 36. Protocol workers can author separate files in parallel;
  endpoint-flow integration in Task 36 touches all three and merges after them.

tests/e2e/network_substrate_helpers_test.go
  Task 9A creates it. Later tasks may add helpers only after the owning protocol
  test file needs them, and helper signature changes require a separate commit.

tests/e2e/network_driver_qemu_test.go
  Tasks 9A, 9B, 9C, 10, 11, 12. Strictly serial.

tests/e2e/network_protocol_qemu_test.go
  Tasks 13, 14, 16, 17, 22. Protocol test edits merge in task order.

tests/e2e/network_runtime_qemu_test.go
  Tasks 18, 19, 20, 21B. Strictly serial.

tests/e2e/network_dhcp_dns_qemu_test.go
  Tasks 26, 27. Serial because both use the DHCP-assigned config path.

tests/e2e/network_tcp_http_qemu_test.go
  Tasks 28, 31, 32, 33. TCP lands first; HTTP/1.1 and HTTP/2 may be authored
  in parallel and merge through Task 33.

tests/e2e/network_quic_http3_qemu_test.go
  Tasks 34F, 35. Serial.

tests/e2e/network_qos_qemu_test.go
  Task 39 only.

compiler/sem/report.go and compiler/sem/network_graph.go
  Tasks 5, 7, 18, 21A, 21B, 26, 27, 28, 29, 30E, 36, 37E, 38F, 39. Serial; feature
  report fields merge in task order.
```

Junior-sized decomposition rule:

```text
Task 7 lands as five reviewable slices:
  7A constructor allowlist -> 7B quarantined-byte flow -> 7C packet lifetime
  -> 7D static table checks -> 7E packet owner checks.

Task 26 lands as three reviewable slices:
  26A DHCP DISCOVER/OFFER nettrace -> 26B REQUEST/ACK lease commit
  -> 26C reactor/report/e2e.

Task 27 lands as three reviewable slices:
  27A DNS query/response nettrace -> 27B compression and cache
  -> 27C reactor/report/e2e.

Task 28 lands as four reviewable slices:
  28A TCP parser/checksum -> 28B handshake -> 28C retransmit/close
  -> 28D reactor/e2e.

Task 32 lands as three reviewable slices:
  32A HTTP/2 preface/settings -> 32B static HPACK HEADERS/DATA
  -> 32C TLS ALPN/e2e.

Task 35 lands as three reviewable slices:
  35A HTTP/3 SETTINGS/control stream -> 35B static QPACK request/response
  -> 35C QUIC stream/e2e/report.

Task 39 lands as three reviewable slices:
  39A QoS policy/report source -> 39B weighted TX dequeue
  -> 39C VLAN PCP e2e.
```

Each slice above follows the same required shape as a top-level task: add the named failing test, run it and observe the expected failure, implement only the slice files, run the slice test plus `NetworkModulesCompile` when Wrela changes, run `git diff --check`, and commit with the parent task's exact `git add` paths narrowed to the files touched by that slice.

Slice code examples:

```go
func TestDHCPDiscoverOfferSlice(t *testing.T) {
	discover := nettrace.EmitDHCPDiscover(nettrace.MustMAC("52:54:00:12:34:56"), 0x11223344)
	offer := nettrace.EmitDHCPOfferFromDiscover(discover, nettrace.MustIPv4("10.10.0.2"), nettrace.MustIPv4("10.10.0.1"), 3600)
	if _, err := nettrace.ParseDHCPOffer(offer, 0x11223344); err != nil {
		t.Fatal(err)
	}
}

func TestDNSCompressionCacheSlice(t *testing.T) {
	cache := nettrace.NewDNSCache(8)
	cache.Put("example.wrela.test", nettrace.MustIPv4("10.10.0.1"), 60)
	if got, ok := cache.Get("example.wrela.test"); !ok || got != nettrace.MustIPv4("10.10.0.1") {
		t.Fatalf("cache lookup = %v %v", got, ok)
	}
}

func TestTCPRetransmitSlice(t *testing.T) {
	conn := nettrace.NewTCPTrace(nettrace.MustIPv4("10.10.0.2"), nettrace.MustIPv4("10.10.0.1")).ActiveOpen(49152, 80)
	conn.AdvanceTicks(200)
	if got := conn.Retransmits(); got != 1 {
		t.Fatalf("retransmits = %d", got)
	}
}

func TestHTTP2StaticHPACKSlice(t *testing.T) {
	headers := nettrace.EncodeHTTP2StaticHeaders("GET", "https", "example.wrela.test", "/hello")
	got, err := nettrace.DecodeHTTP2StaticHeaders(headers)
	if err != nil || got.Path != "/hello" {
		t.Fatalf("headers = %#v err=%v", got, err)
	}
}

func TestHTTP3StaticQPACKSlice(t *testing.T) {
	headers := nettrace.EmitHTTP3Get("example.wrela.test", "/hello")
	if got := nettrace.ParseHTTP3PseudoHeaders(headers); got.Path != "/hello" {
		t.Fatalf("headers = %#v", got)
	}
}

func TestQoSWeightedDequeueSlice(t *testing.T) {
	s := nettrace.NewQoSScheduler(nettrace.QoSWeights{Control: 8, Interactive: 4, BestEffort: 2, Bulk: 1})
	got := s.DrainOrderForTest(15)
	if strings.Join(got[:4], ",") != "control,control,control,control" {
		t.Fatalf("drain order = %#v", got)
	}
}
```

---

## 4. Canonical Networking Source Surface

**Description:** These public Wrela names and fields are fixed. Tests must assert these exact names so driver and protocol tasks can work independently.

**Acceptance Criteria:**

- Source-shape tests read the files and require every type/function/constant below.
- Any spelling change fails before runtime tests execute.
- Protocol modules can be reused by a future NIC driver without importing e1000e.

**Code Example:**

```wrela
module wrela.net.types

data MacAddress {
    b0: U8
    b1: U8
    b2: U8
    b3: U8
    b4: U8
    b5: U8
}

data Ipv4Address {
    value: U32
}

data NetworkCounters {
    rx_packets: U64
    tx_packets: U64
    rx_drops: U64
    tx_drops: U64
    arp_packets: U64
    ipv4_packets: U64
    icmp_packets: U64
    udp_packets: U64
    drop_wrong_mac: U64
    drop_short_ethernet: U64
    drop_malformed_arp: U64
    drop_ipv4_checksum: U64
    drop_ipv4_options: U64
    drop_ipv4_fragment: U64
    drop_icmp_checksum: U64
    drop_udp_checksum: U64
    drop_udp_port_closed: U64
    drop_rx_ring_error: U64
    drop_tx_ring_full: U64
}

data NetworkLaneBudget {
    packets_per_tick: U64
    bytes_per_tick: U64
    expensive_tokens_per_tick: U64
}
```

```wrela
module wrela.net.packet

use { DmaBuffer } from platform.hardware.memory
use { Ipv4Address, MacAddress } from wrela.net.types

data PacketLease {
    id: U64
    epoch: U64
}

data QuarantinedRxBytes {
    lease: PacketLease
    buffer: DmaBuffer<U8>
    length: U64
}

data PacketSlice {
    lease: PacketLease
    buffer: DmaBuffer<U8>
    offset: U64
    length: U64

    fn read_u8(self, relative: U64) -> U8 {
        return self.buffer.read_u8(offset = self.offset + relative)
    }

    fn read_be16(self, relative: U64) -> U16 {
        return (self.read_u8(relative = relative) << 8) | self.read_u8(relative = relative + 1)
    }

    fn read_be32(self, relative: U64) -> U32 {
        return (self.read_be16(relative = relative) << 16) | self.read_be16(relative = relative + 2)
    }

    fn read_mac(self, relative: U64) -> MacAddress {
        return MacAddress(
            b0 = self.read_u8(relative = relative),
            b1 = self.read_u8(relative = relative + 1),
            b2 = self.read_u8(relative = relative + 2),
            b3 = self.read_u8(relative = relative + 3),
            b4 = self.read_u8(relative = relative + 4),
            b5 = self.read_u8(relative = relative + 5)
        )
    }

    fn slice(self, relative: U64, length: U64) -> PacketSlice {
        return PacketSlice(lease = self.lease, buffer = self.buffer, offset = self.offset + relative, length = length)
    }
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

```wrela
module machine.x86_64.e1000e

driver path E1000ePath {
    identity: PathIdentity
    device: PciDevice
    mmio: MmioRegion
    dma: DmaDomainAuthority
    irq: PciInterruptRoute
    mac: MacAddress
    rx: E1000eRxRing
    tx: E1000eTxRing

    fn initialize(self) -> LinkState
    fn poll_rx(self) -> Option<QuarantinedRxBytes>
    fn transmit(self, frame: TxFrameLease) -> Result<Unit, TxFull>
    fn ack_interrupts(self) -> NetworkInterruptStatus
    fn link_state(self) -> LinkState
    fn mac_address(self) -> MacAddress
}
```

### Supporting Types Created By Task 4

These names are used by later tasks and must be created in Task 4 before any runtime implementation begins. Existing repo types are not repeated here: `Option`, `Result`, `Unit`, `ExecutorSlot`, `HotPollPolicy`, `Topic`, `TopicPublisher`, `TopicSubscription`, `PciDevice`, `PciInterruptRoute`, `MsiCapability`, `MmioRegion`, `DmaBuffer`, `Bytes`, `MutableBytes`, `ExecutorMemory`, `ArenaFrame`, `PathIdentity`, and `BootPanic` already exist in the current Wrela source after Task 0 prerequisites.

```wrela
module wrela.net.types

use { Topic } from machine.x86_64.topic

data NetworkQueue<T> {
    topic: Topic<T>

    fn push(self, value: T) {
        self.topic.publisher().publish(value = value)
    }
}

data LinkState {
    up: Bool
    speed_mbps: U64
}

data NetworkInterruptStatus {
    rx: Bool
    tx: Bool
    link: Bool
}

data NetworkStaticConfig {
    mac: MacAddress
    local_ip: Ipv4Address
    subnet_mask: Ipv4Address
    gateway: Ipv4Address
    dns_server: Ipv4Address
}

class NetworkStaticConfigBuilder {
    fn qemu_default(self) -> NetworkStaticConfig {
        return NetworkStaticConfig(
            mac = MacAddress(b0 = 0x52, b1 = 0x54, b2 = 0x00, b3 = 0x12, b4 = 0x34, b5 = 0x56),
            local_ip = Ipv4Address(value = 0x0A0A0002),
            subnet_mask = Ipv4Address(value = 0xFFFFFF00),
            gateway = Ipv4Address(value = 0x0A0A0001),
            dns_server = Ipv4Address(value = 0x0A0A0001)
        )
    }

    fn empty(self) -> NetworkStaticConfig {
        return NetworkStaticConfig(
            mac = MacAddress(b0 = 0, b1 = 0, b2 = 0, b3 = 0, b4 = 0, b5 = 0),
            local_ip = Ipv4Address(value = 0),
            subnet_mask = Ipv4Address(value = 0),
            gateway = Ipv4Address(value = 0),
            dns_server = Ipv4Address(value = 0)
        )
    }
}
```

```wrela
module wrela.net.packet

use { ExecutorSlot } from machine.x86_64.executor_slot
use { DmaBuffer } from platform.hardware.memory
use { Ipv4Address, MacAddress } from wrela.net.types

data TxFull {}

data TxFrameLease {
    buffer: DmaBuffer<U8>
    length: U64
    epoch: U64

    fn write_u8(self, offset: U64, value: U8) {
        self.buffer.write_u8(offset = offset, value = value)
    }

    fn write_be16(self, offset: U64, value: U16) {
        self.buffer.write_u8(offset = offset, value = (value >> 8) & 0xFF)
        self.buffer.write_u8(offset = offset + 1, value = value & 0xFF)
    }

    fn write_be32(self, offset: U64, value: U32) {
        self.write_be16(offset = offset, value = (value >> 16) & 0xFFFF)
        self.write_be16(offset = offset + 2, value = value & 0xFFFF)
    }

    fn write_mac(self, offset: U64, value: MacAddress) {
        self.write_u8(offset = offset, value = value.b0)
        self.write_u8(offset = offset + 1, value = value.b1)
        self.write_u8(offset = offset + 2, value = value.b2)
        self.write_u8(offset = offset + 3, value = value.b3)
        self.write_u8(offset = offset + 4, value = value.b4)
        self.write_u8(offset = offset + 5, value = value.b5)
    }
}

data MalformedEthernetFrame { reason: U64 }
data MalformedArpPacket { reason: U64 }
data MalformedIpv4Packet { reason: U64 }
data MalformedIcmpPacket { reason: U64 }
data MalformedUdpPacket { reason: U64 }

data VerifiedUdpDatagram {
    packet: VerifiedIpv4Packet
    src_ip: Ipv4Address
    dst_ip: Ipv4Address
    src_port: U16
    dst_port: U16
    checksum: U16
    payload: PacketSlice
}

data UdpPortClosed {
    port: U16
}

data NetworkReactorAuthority {
    slot: ExecutorSlot
}
```

Packet byte-order rule:

```text
Descriptor and MMIO access uses native little-endian helpers on DmaBuffer and MmioRegion.
Packet parsing and emission uses PacketSlice.read_be16/read_be32/read_mac and TxFrameLease.write_be16/write_be32/write_mac.
Do not use DmaBuffer.read_u16 for Ethernet, ARP, IPv4, ICMP, or UDP header fields.
Odd-length checksum payloads add the final byte in the high-order half of the last 16-bit word.
```

---

## 5. Phase 1: Host Harness And Report Contracts

**Description:** Establish the non-runtime contracts first: diagnostic codes, report JSON shape, packet trace package, and QEMU socket-backed e1000e harness. These tasks create deterministic tests before Wrela driver code exists.

**Acceptance Criteria:**

- `go test ./compiler/report ./compiler/qemu ./compiler/nettrace -v` passes by the end of the phase.
- Network report JSON has stable empty-array defaults.
- QEMU argument generation can add exactly one e1000e device and socket netdev.
- Packet trace tests parse and emit fixed byte fixtures without booting QEMU.

**Code Example:**

```go
opts := qemu.Options{
	ExtraArgs: qemu.E1000eSocketNetdevArgs(qemu.E1000eSocketNetdevOptions{
		PeerUDPPort:  12000,
		GuestUDPPort: 12001,
		GuestMAC:     "52:54:00:12:34:56",
	}),
}
args := qemu.Args(opts)
// args contains:
// -netdev socket,id=net0,udp=127.0.0.1:12000,localaddr=127.0.0.1:12001
// -device e1000e,netdev=net0,mac=52:54:00:12:34:56
```

### Task 1: Networking Diagnostics And Report Schema

**Files:**

- Modify: `compiler/diag/codes.go`
- Modify: `compiler/report/report.go`
- Modify: `compiler/report/report_test.go`

**Prerequisites:** Task 0.

**Description:** Add reserved networking diagnostics and a `network` section to `ImageReport`. This task only creates schema and empty defaults; later tasks populate the fields.

**Acceptance Criteria:**

- `diag.SEM0167` through `diag.SEM0184` exist with networking comments.
- `report.ImageReport` includes `Network report.NetworkReport` serialized as JSON key `network`.
- `NetworkReport` includes an `Authorities []AuthorityRecord` field serialized as JSON key `authorities`.
- `NetworkReport` includes application-stack fields with stable JSON keys before later tasks populate them: `application`, `crypto`, `endpoint_flows`, `qos`, and `hardware_profile`.
- `report.NewImageReport` initializes all network slices as empty non-nil arrays.
- `TestImageReportJSONShape`, `TestNewImageReportUsesEmptyArrays`, and `TestNetworkingDiagnosticCodesExist` pass.

**Code Examples:**

Diagnostic constants:

```go
SEM0167 = "SEM0167" // network authority construction is not allowed here
SEM0168 = "SEM0168" // duplicate network device or DMA-domain claim
SEM0169 = "SEM0169" // DMA domain owner does not match network device
SEM0170 = "SEM0170" // quarantined RX bytes used outside verifier path
SEM0171 = "SEM0171" // packet lease or slice escapes its lifetime
SEM0172 = "SEM0172" // network table capacity is missing or zero
SEM0173 = "SEM0173" // network reactor executor placement is invalid
SEM0174 = "SEM0174" // network lane budget is missing or zero
SEM0175 = "SEM0175" // static IPv4 configuration is invalid
SEM0176 = "SEM0176" // e1000e descriptor ring capacity is invalid
SEM0177 = "SEM0177" // UDP endpoint table has duplicate local port
SEM0178 = "SEM0178" // unsupported network protocol feature used in v0
SEM0179 = "SEM0179" // packet recorder capacity is missing or zero
SEM0180 = "SEM0180" // network report is missing required authority facts
SEM0181 = "SEM0181" // network egress authority is not declared
SEM0182 = "SEM0182" // packet buffer is not owned by the network reactor
SEM0183 = "SEM0183" // endpoint data class requires encrypted transport
SEM0184 = "SEM0184" // production constant-time validation failed
```

Diagnostic enforcement map:

| Code | First enforcing task | Test or fixture name |
| --- | --- | --- |
| `SEM0167` | Task 7 | `forged_network_authority.wrela` |
| `SEM0168` | Task 21A | `TestNetworkGraphRejectsDuplicateDeviceClaim` |
| `SEM0169` | Task 5 | `TestDmaDomainOwnerMustMatchClaimedDevice` |
| `SEM0170` | Task 7 | `quarantined_rx_escape.wrela` |
| `SEM0171` | Task 7 | `packet_slice_escape.wrela` |
| `SEM0172` | Task 7 | `network_zero_table_capacity.wrela` |
| `SEM0173` | Task 18 | `network_bad_reactor_slot.wrela` |
| `SEM0174` | Task 18 | `network_zero_lane_budget.wrela` |
| `SEM0175` | Task 6 | `TestNetworkFixtureRejectsInvalidStaticIPv4` |
| `SEM0176` | Task 8 | `TestE1000eRejectsInvalidDescriptorRingCapacity` |
| `SEM0177` | Task 7 | `duplicate_udp_endpoint.wrela` |
| `SEM0178` | Task 15 | `TestIPv4DropsFragmentsAndOptions` |
| `SEM0179` | Task 20 | `TestFlightRecorderRejectsZeroCapacity` |
| `SEM0180` | Task 21B | `TestValidateNetworkReportContentRejectsIncompleteReport` |
| `SEM0181` | Task 18 | `network_missing_egress_authority.wrela` |
| `SEM0182` | Task 7 | `foreign_packet_buffer_owner.wrela` |
| `SEM0183` | Task 36 | `network_secret_cleartext_http.wrela` |
| `SEM0184` | Tasks 38B and 38C | `secret_branch.wrela` and `secret_index.wrela` |

Report structs:

```go
type NetworkReport struct {
	NIC            NetworkNICReport              `json:"nic"`
	DMA            NetworkDMAReport              `json:"dma"`
	Reactor        NetworkReactorReport          `json:"reactor"`
	Rings          NetworkRingReport             `json:"rings"`
	StaticIPv4     NetworkStaticIPv4Report       `json:"static_ipv4"`
	Protocols      []string                      `json:"protocols"`
	DropCounters   []NetworkCounterReport        `json:"drop_counters"`
	FlightRecorder NetworkFlightRecorderReport   `json:"flight_recorder"`
	Authorities    []AuthorityRecord             `json:"authorities"`
	Application    NetworkApplicationReport       `json:"application"`
	Crypto         NetworkCryptoReport            `json:"crypto"`
	EndpointFlows  []NetworkEndpointFlowReport    `json:"endpoint_flows"`
	QoS            NetworkQoSReport               `json:"qos"`
	HardwareProfile NetworkHardwareProfileReport  `json:"hardware_profile"`
}

type NetworkNICReport struct {
	Driver   string `json:"driver"`
	VendorID uint16 `json:"vendor_id"`
	DeviceID uint16 `json:"device_id"`
	MAC      string `json:"mac"`
	BAR      string `json:"bar"`
	IRQMode  string `json:"irq_mode"`
}

type NetworkDMAReport struct {
	Policy           string `json:"policy"`
	IOMMUEnforced    bool   `json:"iommu_enforced"`
	TrustedDMADevice bool   `json:"trusted_dma_device"`
	MemoryType       string `json:"memory_type"`
	OrderingPolicy   string `json:"ordering_policy"`
	Bytes            uint64 `json:"bytes"`
}

type NetworkReactorReport struct {
	ExecutorSlot string             `json:"executor_slot"`
	VcpuID       uint64             `json:"vcpu_id"`
	Urgent       NetworkBudgetReport `json:"urgent"`
	Normal       NetworkBudgetReport `json:"normal"`
	Expensive    NetworkBudgetReport `json:"expensive"`
	Epoch        uint64             `json:"epoch"`
}

type NetworkBudgetReport struct {
	PacketsPerTick         uint64 `json:"packets_per_tick"`
	BytesPerTick           uint64 `json:"bytes_per_tick"`
	ExpensiveTokensPerTick uint64 `json:"expensive_tokens_per_tick"`
}

type NetworkRingReport struct {
	RXDescriptors     uint64 `json:"rx_descriptors"`
	TXDescriptors     uint64 `json:"tx_descriptors"`
	RXPacketBuffers   uint64 `json:"rx_packet_buffers"`
	TXPacketBuffers   uint64 `json:"tx_packet_buffers"`
	PacketBufferBytes uint64 `json:"packet_buffer_bytes"`
}

type NetworkStaticIPv4Report struct {
	MAC        string `json:"mac"`
	LocalIP    string `json:"local_ip"`
	SubnetMask string `json:"subnet_mask"`
	Gateway    string `json:"gateway"`
	DNSServer  string `json:"dns_server"`
}

type NetworkCounterReport struct {
	Name  string `json:"name"`
	Value uint64 `json:"value"`
}

type NetworkFlightRecorderReport struct {
	Capacity     uint64 `json:"capacity"`
	RecordBytes  uint64 `json:"record_bytes"`
	StoresPayload bool  `json:"stores_payload"`
}

type NetworkApplicationReport struct {
	DHCP        NetworkDHCPReport  `json:"dhcp"`
	DNS         NetworkDNSReport   `json:"dns"`
	TCP         NetworkTCPReport   `json:"tcp"`
	TLS         NetworkTLSReport   `json:"tls"`
	HTTP        NetworkHTTPReport  `json:"http"`
	QUIC        NetworkQUICReport  `json:"quic"`
}

type NetworkDHCPReport struct {
	Enabled      bool   `json:"enabled"`
	State        string `json:"state"`
	LeaseSeconds uint32 `json:"lease_seconds"`
	ServerID     string `json:"server_id"`
}

type NetworkDNSReport struct {
	Enabled       bool   `json:"enabled"`
	CacheCapacity uint64 `json:"cache_capacity"`
	Server        string `json:"server"`
}

type NetworkTCPReport struct {
	ConnectionCapacity uint64 `json:"connection_capacity"`
	RetransmitLimit    uint64 `json:"retransmit_limit"`
	MSS                uint64 `json:"mss"`
}

type NetworkTLSReport struct {
	Enabled        bool   `json:"enabled"`
	CipherSuite    string `json:"cipher_suite"`
	ALPN           string `json:"alpn"`
	ValidationMode string `json:"validation_mode"`
	PinnedSPKI     bool   `json:"pinned_spki"`
}

type NetworkHTTPReport struct {
	HTTP1 bool `json:"http1"`
	HTTP2 bool `json:"http2"`
	HTTP3 bool `json:"http3"`
}

type NetworkQUICReport struct {
	Enabled              bool   `json:"enabled"`
	Version              uint32 `json:"version"`
	ConnectionCapacity   uint64 `json:"connection_capacity"`
	BidiStreamCapacity   uint64 `json:"bidi_stream_capacity"`
}

type NetworkCryptoReport struct {
	Backend               string `json:"backend"`
	XCR0Enabled           uint64 `json:"xcr0_enabled"`
	ConstantTimeValidated bool   `json:"constant_time_validated"`
	BinaryValidated       bool   `json:"binary_validated"`
}

type NetworkEndpointFlowReport struct {
	Label          string `json:"label"`
	DataClass      string `json:"data_class"`
	Protocol       string `json:"protocol"`
	RemoteHostHash uint64 `json:"remote_host_hash"`
	Encrypted      bool   `json:"encrypted"`
}

type NetworkQoSReport struct {
	ControlWeight    uint64            `json:"control_weight"`
	InteractiveWeight uint64           `json:"interactive_weight"`
	BestEffortWeight uint64            `json:"best_effort_weight"`
	BulkWeight       uint64            `json:"bulk_weight"`
	VLANPCP          map[string]uint8  `json:"vlan_pcp"`
}

type NetworkHardwareProfileReport struct {
	Tested       bool   `json:"tested"`
	DeviceName   string `json:"device_name"`
	VendorID     uint16 `json:"vendor_id"`
	DeviceID     uint16 `json:"device_id"`
	BAR0Length   uint64 `json:"bar0_length"`
	EEPROMPresent bool  `json:"eeprom_present"`
}
```

Failing test:

```go
func TestNetworkingDiagnosticCodesExist(t *testing.T) {
	codes := []string{
		diag.SEM0167, diag.SEM0168, diag.SEM0169, diag.SEM0170,
		diag.SEM0171, diag.SEM0172, diag.SEM0173, diag.SEM0174,
		diag.SEM0175, diag.SEM0176, diag.SEM0177, diag.SEM0178,
		diag.SEM0179, diag.SEM0180, diag.SEM0181, diag.SEM0182,
		diag.SEM0183, diag.SEM0184,
	}
	for _, code := range codes {
		if code == "" {
			t.Fatalf("network diagnostic code must not be empty")
		}
	}
}
```

**Steps:**

- [ ] Add the failing diagnostic/report tests above.
- [ ] Run: `go test ./compiler/report -run 'NetworkingDiagnosticCodesExist|ImageReportJSONShape|NewImageReportUsesEmptyArrays' -v`
  Expected: FAIL with missing `SEM0167` or missing `Network`.
- [ ] Add constants and report structs exactly as shown.
- [ ] Initialize network slices and maps in `NewImageReport`: `Protocols`, `DropCounters`, `Authorities`, `EndpointFlows`, and `QoS.VLANPCP` must be non-nil empty values.
- [ ] Run: `go test ./compiler/report -run 'NetworkingDiagnosticCodesExist|ImageReportJSONShape|NewImageReportUsesEmptyArrays' -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add compiler/diag/codes.go compiler/report/report.go compiler/report/report_test.go
git commit -m "feat: add network report contract -Codex Automated"
```

### Task 2: QEMU e1000e Socket Harness

**Files:**

- Create: `compiler/qemu/e1000e_peer.go`
- Create: `compiler/qemu/e1000e_peer_test.go`

**Prerequisites:** Tasks 0 and 1.

**Description:** Add a QEMU argument helper for a rootless socket-backed e1000e device and a Go peer that exchanges raw Ethernet frames as UDP datagrams. Generic QEMU `ExtraArgs` is a shared substrate prerequisite from Task 0, not networking-owned work.

**Acceptance Criteria:**

- `qemu.E1000eSocketNetdevArgs` returns exactly one `-netdev socket` and one `-device e1000e` argument group.
- The default MAC is `52:54:00:12:34:56`.
- `EthernetPeer` can listen, receive one frame, and send one frame to QEMU's guest port.
- Unit tests do not launch QEMU.

**Code Examples:**

Argument generation:

```go
type E1000eSocketNetdevOptions struct {
	PeerUDPPort  int
	GuestUDPPort int
	GuestMAC     string
}

func E1000eSocketNetdevArgs(opts E1000eSocketNetdevOptions) []string {
	mac := opts.GuestMAC
	if mac == "" {
		mac = "52:54:00:12:34:56"
	}
	peer := opts.PeerUDPPort
	guest := opts.GuestUDPPort
	netdev := fmt.Sprintf("socket,id=net0,udp=127.0.0.1:%d,localaddr=127.0.0.1:%d", peer, guest)
	return []string{"-netdev", netdev, "-device", "e1000e,netdev=net0,mac=" + mac}
}
```

Peer shape:

```go
type EthernetPeer struct {
	conn      *net.UDPConn
	guestUDP *net.UDPAddr
}

func NewEthernetPeer(peerPort, guestPort int) (*EthernetPeer, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: peerPort})
	if err != nil {
		return nil, err
	}
	return &EthernetPeer{
		conn:      conn,
		guestUDP: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: guestPort},
	}, nil
}

func (p *EthernetPeer) Send(frame []byte) error {
	_, err := p.conn.WriteToUDP(frame, p.guestUDP)
	return err
}

func (p *EthernetPeer) Close() error {
	return p.conn.Close()
}

func (p *EthernetPeer) Receive(ctx context.Context) ([]byte, error) {
	deadline, ok := ctx.Deadline()
	if ok {
		_ = p.conn.SetReadDeadline(deadline)
	}
	buf := make([]byte, 2048)
	n, _, err := p.conn.ReadFromUDP(buf)
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), buf[:n]...), nil
}
```

**Steps:**

- [ ] Add failing `TestE1000eSocketNetdevArgs`.
- [ ] Add failing `TestEthernetPeerLoopback`.
- [ ] Run: `go test ./compiler/qemu -run 'E1000e|EthernetPeer' -v`
  Expected: FAIL with missing `E1000eSocketNetdevArgs` or missing peer type.
- [ ] Implement `E1000eSocketNetdevArgs` and peer.
- [ ] Run: `go test ./compiler/qemu -run 'E1000e|EthernetPeer' -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add compiler/qemu/e1000e_peer.go compiler/qemu/e1000e_peer_test.go
git commit -m "feat: add qemu e1000e socket harness -Codex Automated"
```

### Task 3: Packet Trace Harness

**Files:**

- Create: `compiler/nettrace/packet.go`
- Create: `compiler/nettrace/packet_test.go`
- Create: `compiler/nettrace/checksum.go`
- Create: `compiler/nettrace/checksum_test.go`
- Create: `compiler/nettrace/trace.go`
- Create: `compiler/nettrace/trace_test.go`

**Prerequisites:** Task 1.

**Description:** Build a pure Go packet laboratory for fixed Ethernet/ARP/IPv4/ICMP/UDP byte fixtures. This package is host-side test infrastructure; it does not become the runtime stack.

**Acceptance Criteria:**

- Parses Ethernet II header into destination MAC, source MAC, EtherType, and payload.
- Parses ARP request/reply with Ethernet/IPv4 hardware/protocol values.
- Parses IPv4 header and rejects bad version, IHL, length, options, fragments, destination, and checksum.
- Parses ICMP echo request/reply and validates checksum.
- Parses UDP and validates length and checksum where present.
- Emits deterministic ARP reply, ICMP echo reply, and UDP echo frames.
- Includes checksum vectors: IPv4 header checksum and ICMP/UDP one's-complement checksum.

**Code Examples:**

Ethernet parser:

```go
type EthernetFrame struct {
	Dst       MAC
	Src       MAC
	EtherType uint16
	Payload   []byte
}

type MAC [6]byte
type IPv4 uint32

func MustMAC(s string) MAC {
	hw, err := net.ParseMAC(s)
	if err != nil || len(hw) != 6 {
		panic("bad MAC fixture: " + s)
	}
	var out MAC
	copy(out[:], hw)
	return out
}

func MustIPv4(s string) IPv4 {
	ip := net.ParseIP(s).To4()
	if ip == nil {
		panic("bad IPv4 fixture: " + s)
	}
	return IPv4(binary.BigEndian.Uint32(ip))
}

func ParseEthernet(raw []byte) (EthernetFrame, error) {
	if len(raw) < 14 {
		return EthernetFrame{}, ErrShortEthernet
	}
	var dst, src MAC
	copy(dst[:], raw[0:6])
	copy(src[:], raw[6:12])
	return EthernetFrame{
		Dst:       dst,
		Src:       src,
		EtherType: binary.BigEndian.Uint16(raw[12:14]),
		Payload:   raw[14:],
	}, nil
}
```

Checksum:

```go
func OnesComplement16(parts ...[]byte) uint16 {
	var sum uint32
	for _, part := range parts {
		for len(part) >= 2 {
			sum += uint32(binary.BigEndian.Uint16(part[:2]))
			part = part[2:]
		}
		if len(part) == 1 {
			sum += uint32(part[0]) << 8
		}
	}
	for sum > 0xffff {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}
```

Required byte fixtures:

```go
var HostARPRequest = []byte{
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x52, 0x54, 0x00, 0xfe, 0xed, 0x01, 0x08, 0x06,
	0x00, 0x01, 0x08, 0x00, 0x06, 0x04, 0x00, 0x01, 0x52, 0x54, 0x00, 0xfe, 0xed, 0x01,
	0x0a, 0x0a, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x0a, 0x0a, 0x00, 0x02,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00,
}

var HostICMPEchoRequest = []byte{
	0x52, 0x54, 0x00, 0x12, 0x34, 0x56, 0x52, 0x54, 0x00, 0xfe, 0xed, 0x01, 0x08, 0x00,
	0x45, 0x00, 0x00, 0x21, 0x00, 0x01, 0x00, 0x00, 0x40, 0x01, 0x66, 0xc5, 0x0a, 0x0a,
	0x00, 0x01, 0x0a, 0x0a, 0x00, 0x02, 0x08, 0x00, 0xa7, 0xeb, 0x12, 0x34, 0x00, 0x01,
	0x77, 0x72, 0x65, 0x6c, 0x61,
}

var HostUDPEchoRequest = []byte{
	0x52, 0x54, 0x00, 0x12, 0x34, 0x56, 0x52, 0x54, 0x00, 0xfe, 0xed, 0x01, 0x08, 0x00,
	0x45, 0x00, 0x00, 0x1e, 0x00, 0x02, 0x00, 0x00, 0x40, 0x11, 0x66, 0xb7, 0x0a, 0x0a,
	0x00, 0x01, 0x0a, 0x0a, 0x00, 0x02, 0x1e, 0x61, 0x00, 0x07, 0x00, 0x0a, 0x64, 0xf2,
	0x68, 0x69,
}
```

Trace test:

```go
func TestEthernetArpIpv4IcmpTrace(t *testing.T) {
	trace := NewTrace()
	trace.AddHostARPRequest()
	trace.AddGuestARPReply()
	trace.AddHostICMPEchoRequest()
	trace.AddGuestICMPEchoReply()
	if err := trace.Verify(); err != nil {
	t.Fatal(err)
}
}
```

**Steps:**

- [ ] Add failing parser and checksum tests with byte literals for ARP, ICMP, and UDP.
- [ ] Run: `go test ./compiler/nettrace -v`
  Expected: FAIL because package is missing.
- [ ] Implement packet structs, parsers, emitters, and checksum helpers.
- [ ] Run: `go test ./compiler/nettrace -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add compiler/nettrace
git commit -m "test: add network packet trace harness -Codex Automated"
```

---

## 6. Phase 2: Source Contracts And Authority Guards

**Description:** Create the Wrela network API shell, DMA domain model, network fixture wiring, and semantic guardrails before driver behavior depends on them.

**Acceptance Criteria:**

- Source-shape tests pin all network modules and public names.
- Network DMA authority is derived from a claimed PCI device and a child arena.
- Forged network authorities and quarantined packet misuse fail semantically.
- The network fixture can build far enough to fail only on missing e1000e runtime behavior.

**Code Example:**

```wrela
let nic = discovery.pci.require_device(
    vendor_id = QEMU_E1000E_VENDOR_ID,
    device_id = QEMU_E1000E_DEVICE_ID,
    occurrence = 0
)
let net_arena = root.child(identity = ArenaIdentity(label = "network.core"), length = NETWORK_ARENA_BYTES, align = 4096)
let net_dma = net_arena.claim_dma_domain(
    owner = nic,
    policy = DmaPolicy(require_iommu = false, trusted_without_iommu = true)
)
```

### Task 4: Wrela Network Module Shell

**Files:**

- Create: `wrela/net/types.wrela`
- Create: `wrela/net/config.wrela`
- Create: `wrela/net/packet.wrela`
- Create: `wrela/net/ethernet.wrela`
- Create: `wrela/net/arp.wrela`
- Create: `wrela/net/ipv4.wrela`
- Create: `wrela/net/icmp.wrela`
- Create: `wrela/net/udp.wrela`
- Create: `wrela/net/flight_recorder.wrela`
- Create: `wrela/net/reactor.wrela`
- Create: `compiler/sem/network_source_shape_test.go`

**Prerequisites:** Task 1.

**Description:** Add compileable Wrela module shells with fixed constants, data types, and method signatures. Behavior can return empty values in this task, but names and fields are final.

**Acceptance Criteria:**

- `go test ./compiler/sem -run TestNetworkSourceShape -v` passes.
- All modules compile through the `parseUEFIModuleSet` plus `BuildIndex` plus `Check` path shown below.
- Every constant from Section 1 appears in `wrela/net/config.wrela` or `wrela/machine/x86_64/e1000e.wrela`.
- No module imports `machine.x86_64.e1000e` except the fixture and reactor wiring; protocol modules remain NIC-neutral.

**Code Examples:**

Source-shape test:

```go
func TestNetworkSourceShape(t *testing.T) {
	modules := parseUEFIModuleSet(t)
	index, ds := BuildIndex(modules)
	if len(ds) != 0 {
		t.Fatalf("build index diagnostics: %#v", ds)
	}
	requireDataFields(t, index, "wrela.net.types", "MacAddress", []string{"b0", "b1", "b2", "b3", "b4", "b5"})
	requireDataFields(t, index, "wrela.net.packet", "PacketSlice", []string{"lease", "buffer", "offset", "length"})
	requireMethod(t, index, "wrela.net.packet", "PacketSlice", "read_u8", []string{"relative"}, "U8")
	requireMethod(t, index, "wrela.net.packet", "PacketSlice", "read_be16", []string{"relative"}, "U16")
	requireMethod(t, index, "wrela.net.packet", "PacketSlice", "read_be32", []string{"relative"}, "U32")
	requireMethod(t, index, "wrela.net.packet", "PacketSlice", "read_mac", []string{"relative"}, "MacAddress")
	requireMethod(t, index, "wrela.net.packet", "TxFrameLease", "write_be16", []string{"offset", "value"}, "Unit")
	requireMethod(t, index, "wrela.net.packet", "TxFrameLease", "write_be32", []string{"offset", "value"}, "Unit")
	requireMethod(t, index, "wrela.net.packet", "TxFrameLease", "write_mac", []string{"offset", "value"}, "Unit")
	requireDataFields(t, index, "wrela.net.udp", "UdpApplicationDatagram", []string{"src_ip", "dst_ip", "src_port", "dst_port", "payload"})
	requireMethod(t, index, "wrela.net.udp", "UdpEndpointTable", "dispatch", []string{"packet"}, "Result<Unit, UdpPortClosed>")
}

func requireDataFields(t *testing.T, index *Index, module, name string, fields []string) {
	t.Helper()
	typ := lookupDataType(t, index, module, name)
	for _, field := range fields {
		if !typ.HasField(field) {
			t.Fatalf("%s.%s missing field %s", module, name, field)
		}
	}
}

func requireMethod(t *testing.T, index *Index, module, receiver, method string, params []string, result string) {
	t.Helper()
	fn := lookupMethod(t, index, module, receiver, method)
	if got := fn.ResultString(); got != result {
		t.Fatalf("%s.%s.%s result = %s, want %s", module, receiver, method, got, result)
	}
	gotParams := fn.ParamNames()
	if !slices.Equal(gotParams, params) {
		t.Fatalf("%s.%s.%s params = %#v, want %#v", module, receiver, method, gotParams, params)
	}
}

func TestNetworkModulesCompile(t *testing.T) {
	modules := parseUEFIModuleSet(t)
	index, ds := BuildIndex(modules)
	if len(ds) != 0 {
		t.Fatalf("build index diagnostics: %#v", ds)
	}
	if _, ds := Check(index, modules); len(ds) != 0 {
		t.Fatalf("network modules must compile, diagnostics: %#v", ds)
	}
}
```

UDP shell:

```wrela
module wrela.net.udp

use { Option, Result, Unit } from wrela.lang.core
use { NetworkQueue } from wrela.net.types
use { Ipv4Address } from wrela.net.types
use { VerifiedUdpDatagram, UdpPortClosed } from wrela.net.packet

data UdpApplicationDatagram {
    src_ip: Ipv4Address
    dst_ip: Ipv4Address
    src_port: U16
    dst_port: U16
    payload_length: U16
    payload0: U8
    payload1: U8
    payload2: U8
    payload3: U8
    payload4: U8
    payload5: U8
    payload6: U8
    payload7: U8
}

data UdpEndpoint {
    local_port: U16
    rx_queue: NetworkQueue<UdpApplicationDatagram>
}

data UdpEndpointTable {
    count: U64
    endpoint0: UdpEndpoint
    endpoint1: UdpEndpoint
    endpoint2: UdpEndpoint
    endpoint3: UdpEndpoint
    endpoint4: UdpEndpoint
    endpoint5: UdpEndpoint
    endpoint6: UdpEndpoint
    endpoint7: UdpEndpoint

    fn at(self, index: U64) -> UdpEndpoint {
        if index == 0 { return self.endpoint0 }
        if index == 1 { return self.endpoint1 }
        if index == 2 { return self.endpoint2 }
        if index == 3 { return self.endpoint3 }
        if index == 4 { return self.endpoint4 }
        if index == 5 { return self.endpoint5 }
        if index == 6 { return self.endpoint6 }
        if index == 7 { return self.endpoint7 }
        return self.endpoint0
    }
}
```

**Steps:**

- [ ] Add failing source-shape and module-compile tests.
- [ ] Run: `go test ./compiler/sem -run 'TestNetwork(SourceShape|ModulesCompile)' -v`
  Expected: FAIL with missing files.
- [ ] Create the modules with exact types and signatures.
- [ ] Use simple return values only where a function body is required for parsing.
- [ ] Run: `go test ./compiler/sem -run 'TestNetwork(SourceShape|ModulesCompile)' -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/net compiler/sem/network_source_shape_test.go
git commit -m "feat: add network source contracts -Codex Automated"
```

### Task 5: DMA Domain Authority And Byte Helpers

**Files:**

- Modify: `wrela/platform/hardware/memory.wrela`
- Modify: `compiler/sem/hardware_authority_test.go`
- Modify: `compiler/sem/memory_graph.go`
- Modify: `compiler/sem/memory_graph_test.go`
- Modify: `compiler/sem/report.go`
- Modify: `compiler/sem/report_test.go`
- Create: `compiler/codegen/dma_buffer_codegen_test.go`

**Prerequisites:** Task 4.

**Description:** Add source-visible DMA domain policy and bounded DMA byte helpers. e1000e must allocate descriptor rings and packet buffers from this domain; ordinary user modules cannot construct the authority directly.

**Acceptance Criteria:**

- `DmaPolicy` has `require_iommu` and `trusted_without_iommu`.
- `DmaDomainAuthority` records owner device, arena base/length, policy, and `iommu_enforced`.
- `ChildArena.claim_dma_domain(owner: PciDevice, policy: DmaPolicy) -> DmaDomainAuthority` exists.
- `ChildArena.dma_buffer(owner: PciDevice, identity, length, align) -> DmaBuffer<U8>` exists and allocates inside the child arena. Do not call `RootArena.dma_buffer` from the network driver.
- `DmaDomainAuthority.buffer(identity, length, align) -> DmaBuffer<U8>` exists.
- `DmaDomainAuthorityBuilder.empty()` exists only for empty hardware-plan placeholders.
- `DmaBuffer<T>` has bounded `read_u8`, `read_u16`, `read_u32`, `read_u64`, `write_u8`, `write_u16`, `write_u32`, and `write_u64` helpers with offset checks.
- A codegen sanity test proves `DmaBuffer<U8>.write_u32` lowers through the generic receiver without losing `self.slots.address`.
- If that sanity test fails because generic-receiver asm methods do not preserve receiver fields, the same task owns the compiler fix in `compiler/codegen/asm_method.go` and `compiler/codegen/x64.go`. Do not work around the failure by adding non-generic network-only DMA helpers.
- Semantic test rejects a `DmaDomainAuthority` used with a different `PciDevice` owner using `SEM0169`.
- Image graph records DMA domains separately from DMA buffers.
- Report includes DMA authority records.

**Code Examples:**

Wrela surface:

```wrela
data DmaPolicy {
    require_iommu: Bool
    trusted_without_iommu: Bool
}

data DmaDomainAuthority {
    owner: PciDevice
    arena: ChildArena
    policy: DmaPolicy
    iommu_enforced: Bool

    fn buffer(self, identity: ArenaIdentity, length: U64, align: U64) -> DmaBuffer<U8> {
        return self.arena.dma_buffer(owner = self.owner, identity = identity, length = length, align = align)
    }
}

data ChildArena {
    // Existing fields and child/child_at methods stay unchanged.

    fn claim_dma_domain(self, owner: PciDevice, policy: DmaPolicy) -> DmaDomainAuthority {
        return DmaDomainAuthority(owner = owner, arena = self, policy = policy, iommu_enforced = false)
    }

    fn dma_buffer(self, owner: PciDevice, identity: ArenaIdentity, length: U64, align: U64) -> DmaBuffer<U8> {
        let child = self.child(identity = identity, length = length, align = align)
        return DmaBuffer<U8>(
            owner = owner,
            slots = Slots<U8>(address = child.base, capacity = child.length)
        )
    }
}

class DmaDomainAuthorityBuilder {
    panic: BootPanic

    fn empty(self) -> DmaDomainAuthority {
        let empty_window = PcieEcamWindow(base = 0, segment = 0, start_bus = 0, end_bus = 0, panic = self.panic)
        let empty_identity = PciDeviceIdentity(segment = 0, bus = 0, device = 0, function = 0, vendor_id = 0xFFFF, device_id = 0xFFFF, class_code = 0, subclass = 0, prog_if = 0, revision = 0, header_type = 0, interrupt_pin = 0, interrupt_line = 0)
        let empty_device = PciDevice(window = empty_window, identity = empty_identity, panic = self.panic)
        let region = PhysicalRegionAuthority(base = 0, length = 0, align = 4096, provenance = 0, panic = self.panic)
        let root = RootArena(region = region, identity = ArenaIdentity(label = "empty.dma.root"), policy = ArenaPolicy(evict_cache_by_default = false), next_offset = 0)
        let child = ChildArena(root = root, identity = ArenaIdentity(label = "empty.dma"), base = 0, length = 0, next_offset = 0)
        return DmaDomainAuthority(owner = empty_device, arena = child, policy = DmaPolicy(require_iommu = false, trusted_without_iommu = false), iommu_enforced = false)
    }
}
```

Bounded write helper:

```wrela
fn check_range(self, offset: U64, width: U64) {
    if width > self.slots.capacity {
        self.owner.panic.fail(code = 0xAC070001)
    }
    if offset > self.slots.capacity - width {
        self.owner.panic.fail(code = 0xAC070001)
    }
}

fn write_u32(self, offset: U64, value: U32) {
    self.check_range(offset = offset, width = 4)
    self.unchecked_write_u32(offset = offset, value = value)
}

asm fn unchecked_write_u32(self, offset: U64, value: U32) {
    mov r11, self.slots.address
    add r11, offset
    mov eax, value
    mov [r11], eax
    ret
}
```

Semantic test:

```go
func TestDmaDomainAuthorityCannotBeForgedByUserModule(t *testing.T) {
	src := `
module examples.bad_dma_domain
use { DmaDomainAuthority, DmaPolicy } from platform.hardware.memory
class Bad {
    fn forge(self) {
        let domain = DmaDomainAuthority(policy = DmaPolicy(require_iommu = false, trusted_without_iommu = true), iommu_enforced = false)
    }
}`
	_, ds := checkTrustedPlatformSourceForTest(t, "examples.bad_dma_domain", src)
	if !hasCode(ds, diag.SEM0167) {
		t.Fatalf("expected SEM0167, got %#v", ds)
	}
}
```

Generic receiver codegen sanity test:

```go
func TestDmaBufferGenericWriteU32CodegenUsesSlotsAddress(t *testing.T) {
	checked := parseCheckedUEFIModules(t)
	method := asmMethodFromSem(t, checked, "platform.hardware.memory", "DmaBuffer", "unchecked_write_u32")
	instructions, ds, _ := lowerAndEncodeAsmMethod(method)
	if len(ds) != 0 {
		t.Fatalf("lower unchecked_write_u32 diagnostics: %#v", ds)
	}
	if !hasInstructionSequence(instructions,
		"mov r11 self.slots.address",
		"add r11 offset",
		"mov [r11] eax",
	) {
		t.Fatalf("unchecked_write_u32 must address through self.slots.address: %#v", instructionSignatures(instructions))
	}
}
```

**Steps:**

- [ ] Add failing authority, graph, report, and generic codegen sanity tests.
- [ ] Run: `go test ./compiler/sem ./compiler/codegen -run 'DmaDomain|DMABuffer|AuthorityAudit|DmaBufferGeneric' -v`
  Expected: FAIL with missing `DmaDomainAuthority` or `SEM0167`.
- [ ] Add Wrela types and helpers.
- [ ] If `TestDmaBufferGenericWriteU32CodegenUsesSlotsAddress` fails after the Wrela helper exists, patch generic receiver lowering so `self.slots.address` loads from the instantiated receiver layout. The expected emitted instruction sequence must load the receiver pointer, then the `slots.address` field, then store the little-endian value at `address + offset`.
- [ ] Extend semantic hardware authority restrictions to include `DmaDomainAuthority`.
- [ ] Extend memory graph and report wiring for DMA domains.
- [ ] Run: `go test ./compiler/sem ./compiler/codegen -run 'DmaDomain|DMABuffer|AuthorityAudit|DmaBufferGeneric' -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/platform/hardware/memory.wrela compiler/sem/hardware_authority_test.go compiler/sem/memory_graph.go compiler/sem/memory_graph_test.go compiler/sem/report.go compiler/sem/report_test.go compiler/codegen/dma_buffer_codegen_test.go
git commit -m "feat: add explicit dma domain authority -Codex Automated"
```

### Task 6: Network Hardware Plan And Fixture Wiring

**Files:**

- Modify: `wrela/machine/x86_64/cpu_state.wrela`
- Modify: `wrela/platform/hardware/discovery.wrela`
- Modify: `examples/hello/main.wrela`
- Create: `tests/e2e/fixtures/network_substrate/main.wrela`
- Create: `tests/e2e/fixtures/network_substrate/program.wrela`
- Create: `compiler/sem/network_fixture_test.go`

**Prerequisites:** Tasks 4 and 5.

**Description:** Add network plan fields to hardware planning and create a dedicated network fixture that claims the QEMU e1000e device, network arena, DMA domain, MSI route, static IPv4 config, and reactor executor slot.

**Acceptance Criteria:**

- `HardwarePlan` has a `network: NetworkHardwarePlan` field.
- Existing examples compile after initializing `network = NetworkHardwarePlanBuilder(panic = panic).empty()`.
- Network fixture uses `PlatformDiscoveryRoot`, `root.child(identity = "network.core")`, and `claim_dma_domain`.
- Network fixture does not construct raw `MutableBytes` outside existing allowed UEFI memory handoff.
- Network fixture's source-shape test requires static IP values and e1000e constants.
- Invalid static IPv4 config, including local IP `0.0.0.0` or subnet mask `0.0.0.0`, fails with `SEM0175`.

**Code Examples:**

Hardware plan:

```wrela
data NetworkHardwarePlan {
    reactor_slot: ExecutorSlot
    memory: ExecutorMemory
    dma: DmaDomainAuthority
    nic_bar0: MmioRegion
    nic_msi: MsiCapability
    config: NetworkStaticConfig
}

class NetworkHardwarePlanBuilder {
    panic: BootPanic

    fn empty(self) -> NetworkHardwarePlan {
        let empty_window = PcieEcamWindow(base = 0, segment = 0, start_bus = 0, end_bus = 0, panic = self.panic)
        let empty_identity = PciDeviceIdentity(segment = 0, bus = 0, device = 0, function = 0, vendor_id = 0xFFFF, device_id = 0xFFFF, class_code = 0, subclass = 0, prog_if = 0, revision = 0, header_type = 0, interrupt_pin = 0, interrupt_line = 0)
        let empty_device = PciDevice(window = empty_window, identity = empty_identity, panic = self.panic)
        return NetworkHardwarePlan(
            reactor_slot = ExecutorSlot(id = 0),
            memory = ExecutorMemory(arena_base = 0, arena_length = 0, next_offset = 0),
            dma = DmaDomainAuthorityBuilder(panic = self.panic).empty(),
            nic_bar0 = MmioRegion(address = 0, length = 0, panic = self.panic),
            nic_msi = MsiCapability(device = empty_device, capability_offset = 0),
            config = NetworkStaticConfigBuilder().empty()
        )
    }
}
```

Network fixture claim:

```wrela
let network_slot_seed = ExecutorSlot(id = 1)
let nic = discovery.pci.require_device(
    vendor_id = QEMU_E1000E_VENDOR_ID,
    device_id = QEMU_E1000E_DEVICE_ID,
    occurrence = 0
)
let net_arena = root.child(identity = ArenaIdentity(label = "network.core"), length = NETWORK_ARENA_BYTES, align = 4096)
let net_memory = root.executor_memory(owner = network_slot_seed, length = 0x100000, align = 4096)
let net_dma = net_arena.claim_dma_domain(
    owner = nic,
    policy = DmaPolicy(require_iommu = false, trusted_without_iommu = true)
)
```

**Steps:**

- [ ] Add failing fixture source-shape test and invalid static IPv4 semantic test.
- [ ] Run: `go test ./compiler/sem -run 'TestNetworkFixtureSourceShape|TestNetworkFixtureRejectsInvalidStaticIPv4' -v`
  Expected: FAIL with missing fixture or missing `NetworkHardwarePlan`.
- [ ] Add `NetworkHardwarePlan` and builder.
- [ ] Update existing `HardwarePlan` construction call sites with empty network plan.
- [ ] Create network fixture files.
- [ ] Run: `go test ./compiler/sem -run 'TestNetworkFixtureSourceShape|TestNetworkFixtureRejectsInvalidStaticIPv4' -v`
  Expected: PASS.
- [ ] Run: `go test ./compiler -run TestBuild -v`
  Expected: PASS.
- [ ] Run: `go test ./tests/e2e -run Hello -v`
  Expected: PASS on a QEMU/OVMF-capable worker. SKIP is not accepted for task completion. Failure here means the new `HardwarePlan.network` field broke the existing hello fixture and must be fixed before continuing.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/machine/x86_64/cpu_state.wrela wrela/platform/hardware/discovery.wrela examples/hello/main.wrela tests/e2e/fixtures/network_substrate compiler/sem/network_fixture_test.go
git commit -m "feat: wire network hardware plan fixture -Codex Automated"
```

### Task 7: Network Semantic Authority Guards

**Files:**

- Create: `compiler/sem/network_graph.go`
- Create: `compiler/sem/network_graph_test.go`
- Modify: `compiler/sem/check.go`
- Modify: `compiler/sem/image_graph.go`
- Create: `tests/fixtures/negative/forged_network_authority.wrela`
- Create: `tests/fixtures/negative/quarantined_rx_escape.wrela`
- Create: `tests/fixtures/negative/packet_slice_escape.wrela`
- Create: `tests/fixtures/negative/foreign_packet_buffer_owner.wrela`
- Create: `tests/fixtures/negative/network_zero_table_capacity.wrela`
- Create: `tests/fixtures/negative/duplicate_udp_endpoint.wrela`

**Prerequisites:** Tasks 4-6.

**Description:** Compiler-track task split into five guard slices: constructor allowlist, quarantined-byte flow, packet lease lifetime, static table validation, and packet owner validation. Enforce the first security contract: network authorities are created only by an explicit trusted-module allowlist, quarantined RX bytes can only enter verifier functions, packet buffers must be owned by the network reactor, packet slices cannot escape leases, and UDP local ports cannot duplicate in one endpoint table. Runtime protocol work may proceed in host-side `compiler/nettrace` while this task is in progress, but Wrela runtime integration must not land until these diagnostics pass.

**Acceptance Criteria:**

- Forged `QuarantinedRxBytes`, `PacketLease`, `VerifiedEthernetFrame`, `E1000ePath`, and `NetworkReactorAuthority` in user modules fail with `SEM0167`.
- Passing quarantined bytes to a non-verifier function fails with `SEM0170`.
- Storing `PacketSlice` or `Verified*` values into longer-lived executor fields fails with `SEM0171`.
- A `PacketSlice`, `TxFrameLease`, or `QuarantinedRxBytes` whose owner is not the `NetworkReactor` executor fails with `SEM0182`.
- Duplicate UDP endpoint ports in a statically constructed `UdpEndpointTable` fail with `SEM0177`.
- Zero ARP table capacity or UDP endpoint capacity fails with `SEM0172`.
- Existing memory lifetime checks continue to pass.

Lease lifetime rule:

- `PacketLease`, `PacketSlice`, `QuarantinedRxBytes`, `VerifiedEthernetFrame`, `VerifiedIpv4Packet`, `VerifiedUdpDatagram`, `IcmpEcho`, and `TxFrameLease` are stack-frame packet capability values.
- Allowed: pass a capability from the reactor into a verifier, pass verifier output to the next verifier in the same call chain, pass a `TxFrameLease` to an emitter, and return `Result.Ok` from verifier to direct caller.
- Forbidden with `SEM0171`: store any packet capability in a data field, topic, queue, global/static value, executor field, or closure; return it from the reactor to an application executor; assign it to a variable whose lifetime outlives the current packet-processing function.
- Forbidden with `SEM0182`: create or use a packet capability for a buffer whose DMA owner or executor owner is not the dedicated `NetworkReactor`.
- UDP endpoint queues store `UdpApplicationDatagram`, not `VerifiedUdpDatagram`. The reactor copies at most the configured endpoint payload capacity out of `PacketSlice` before queueing; the packet lease is recycled before control returns to the application executor.

**Code Examples:**

Trusted module predicate:

```go
func isTrustedNetworkModule(module string, sourcePath string) bool {
	trusted := map[string]string{
		"wrela.net.packet":          "wrela/net/packet.wrela",
		"wrela.net.ethernet":        "wrela/net/ethernet.wrela",
		"wrela.net.arp":             "wrela/net/arp.wrela",
		"wrela.net.ipv4":            "wrela/net/ipv4.wrela",
		"wrela.net.icmp":            "wrela/net/icmp.wrela",
		"wrela.net.udp":             "wrela/net/udp.wrela",
		"wrela.net.reactor":         "wrela/net/reactor.wrela",
		"machine.x86_64.e1000e":     "wrela/machine/x86_64/e1000e.wrela",
		"platform.hardware.memory":  "wrela/platform/hardware/memory.wrela",
	}
	wantPath, ok := trusted[module]
	return ok && filepath.ToSlash(sourcePath) == wantPath
}
```

Negative fixture:

```wrela
module examples.bad_quarantined_rx_escape

use { QuarantinedRxBytes } from wrela.net.packet

class BadSink {
    saved: QuarantinedRxBytes

    fn store(self, rx: QuarantinedRxBytes) {
        self.saved = rx
    }
}
```

Test:

```go
func TestQuarantinedRxBytesCannotEscapeVerifierPath(t *testing.T) {
	_, ds := checkNegativeFixture(t, "quarantined_rx_escape.wrela")
	if !hasCode(ds, diag.SEM0170) {
		t.Fatalf("expected SEM0170, got %#v", ds)
	}
}

func TestPacketBufferMustBeOwnedByNetworkReactor(t *testing.T) {
	_, ds := checkNegativeFixture(t, "foreign_packet_buffer_owner.wrela")
	if !hasCode(ds, diag.SEM0182) {
		t.Fatalf("expected SEM0182, got %#v", ds)
	}
}
```

**Steps:**

- [ ] Add failing network graph tests and negative fixtures.
- [ ] Run: `go test ./compiler/sem -run 'NetworkAuthority|Quarantined|PacketSlice|UdpEndpoint' -v`
  Expected: FAIL with missing diagnostics.
- [ ] Guard slice 7A: implement network authority type classification in `network_graph.go` and constructor allowlist checks in `check.go`.
- [ ] Guard slice 7B: reject `QuarantinedRxBytes` arguments unless the callee module and source path match the trusted predicate above for `wrela.net.ethernet`, `wrela.net.arp`, `wrela.net.ipv4`, `wrela.net.icmp`, or `wrela.net.udp`.
- [ ] Guard slice 7C: reject assignment, return, queue/topic publish, and executor field stores for packet capability values listed in the lifetime rule.
- [ ] Guard slice 7D: add duplicate UDP endpoint and zero table capacity static analysis for literal endpoint tables.
- [ ] Guard slice 7E: add packet owner analysis that accepts only `NetworkReactor`-owned RX/TX buffers for packet capability constructors.
- [ ] Run: `go test ./compiler/sem -run 'NetworkAuthority|Quarantined|PacketSlice|UdpEndpoint|Memory' -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add compiler/sem/network_graph.go compiler/sem/network_graph_test.go compiler/sem/check.go compiler/sem/image_graph.go tests/fixtures/negative/forged_network_authority.wrela tests/fixtures/negative/quarantined_rx_escape.wrela tests/fixtures/negative/packet_slice_escape.wrela tests/fixtures/negative/foreign_packet_buffer_owner.wrela tests/fixtures/negative/network_zero_table_capacity.wrela tests/fixtures/negative/duplicate_udp_endpoint.wrela
git commit -m "feat: enforce network packet authority guards -Codex Automated"
```

---

## 7. Phase 3: e1000e Driver

**Description:** Bring up QEMU e1000e through PCI BAR0 MMIO, DMA rings, MSI route, RX quarantine, TX submission, and interrupt acknowledgement. This phase proves Wrela can operate the NIC before protocol behavior depends on it.

**Acceptance Criteria:**

- The network fixture boots under QEMU with `-device e1000e`.
- The image prints driver init status, MAC, link state, and ring capacities.
- A host peer receives one transmitted Ethernet frame.
- The guest receives one host-generated Ethernet frame as quarantined bytes.
- Report data identifies e1000e, DMA policy, memory type, descriptor counts, interrupt mode, and reset epoch.

**Code Example:**

```wrela
let path = E1000ePath(
    identity = PathIdentity(label = "network.e1000e"),
    device = nic,
    mmio = hardware.hardware_plan.network.nic_bar0,
    dma = hardware.hardware_plan.network.dma,
    irq = hardware.hardware_plan.network.nic_msi.route(
        vector = InterruptVector(value = 0x43),
        target = hardware.hardware_plan.interrupts.local_apic
    ),
    mac = MacAddress(b0 = 0, b1 = 0, b2 = 0, b3 = 0, b4 = 0, b5 = 0),
    rx = E1000eRxRingBuilder().empty(),
    tx = E1000eTxRingBuilder().empty()
).initialize_path()
```

### Task 8: e1000e Register And Descriptor Contracts

**Files:**

- Create: `wrela/machine/x86_64/e1000e.wrela`
- Create: `compiler/sem/e1000e_source_shape_test.go`

**Prerequisites:** Tasks 4-6.

**Description:** Add e1000e constants, descriptor data types, ring state types, and driver path signatures. This task pins low-level names and register offsets before behavior lands.

**Acceptance Criteria:**

- Source-shape test requires all register offsets listed below and every bit/status constant in Section 1.
- RX/TX descriptor data types are exactly 16 bytes each according to `static_assert`.
- Ring counts are `64` and buffer size is `2048`.
- `E1000ePath` exposes `initialize`, `poll_rx`, `transmit`, `ack_interrupts`, `link_state`, and `mac_address`.
- Semantic test rejects non-power-of-two, zero, or non-64 e1000e descriptor counts with `SEM0176`.

**Code Examples:**

Register constants:

```wrela
const E1000_CTRL: U64 = 0x0000
const E1000_STATUS: U64 = 0x0008
const E1000_EERD: U64 = 0x0014
const E1000_ICR: U64 = 0x00C0
const E1000_IMS: U64 = 0x00D0
const E1000_RCTL: U64 = 0x0100
const E1000_TCTL: U64 = 0x0400
const E1000_RDBAL: U64 = 0x2800
const E1000_RDBAH: U64 = 0x2804
const E1000_RDLEN: U64 = 0x2808
const E1000_RDH: U64 = 0x2810
const E1000_RDT: U64 = 0x2818
const E1000_TDBAL: U64 = 0x3800
const E1000_TDBAH: U64 = 0x3804
const E1000_TDLEN: U64 = 0x3808
const E1000_TDH: U64 = 0x3810
const E1000_TDT: U64 = 0x3818
const E1000_RAL0: U64 = 0x5400
const E1000_RAH0: U64 = 0x5404
```

Descriptor types:

```wrela
data E1000eRxDescriptor {
    buffer_address: U64
    length: U16
    checksum: U16
    status: U8
    errors: U8
    special: U16
}

data E1000eTxDescriptor {
    buffer_address: U64
    length: U16
    cso: U8
    command: U8
    status: U8
    css: U8
    special: U16
}

static_assert(sizeof(E1000eRxDescriptor) == 16, message = "e1000e rx descriptor is 16 bytes")
static_assert(sizeof(E1000eTxDescriptor) == 16, message = "e1000e tx descriptor is 16 bytes")
static_assert((NETWORK_RX_DESC_COUNT & (NETWORK_RX_DESC_COUNT - 1)) == 0, message = "rx descriptor count must be power of two")
static_assert((NETWORK_TX_DESC_COUNT & (NETWORK_TX_DESC_COUNT - 1)) == 0, message = "tx descriptor count must be power of two")
static_assert((NETWORK_FLIGHT_RECORDER_ENTRIES & (NETWORK_FLIGHT_RECORDER_ENTRIES - 1)) == 0, message = "flight recorder entries must be power of two")
```

**Steps:**

- [ ] Add failing source-shape test for constants, descriptor fields, path signatures, and invalid descriptor count.
- [ ] Run: `go test ./compiler/sem -run 'TestE1000e(SourceShape|RejectsInvalidDescriptorRingCapacity)' -v`
  Expected: FAIL with missing file.
- [ ] Create `wrela/machine/x86_64/e1000e.wrela` with constants and empty method bodies.
- [ ] Run: `go test ./compiler/sem -run 'TestE1000e(SourceShape|RejectsInvalidDescriptorRingCapacity)' -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/machine/x86_64/e1000e.wrela compiler/sem/e1000e_source_shape_test.go
git commit -m "feat: add e1000e source contract -Codex Automated"
```

### Network E2E Helper Contract

Create these helpers in `tests/e2e/network_substrate_helpers_test.go` when Task 9A begins. Put driver tests in `tests/e2e/network_driver_qemu_test.go` and protocol tests in their assigned e2e files. Later tasks may add local expectation helpers, but they must not change these signatures.

```go
type qemuDeps struct {
	QEMUPath string
	OVMFCodePath string
	OVMFVarsPath string
}

func requireQEMUDeps(t *testing.T, allowSkip bool) qemuDeps {
	t.Helper()
	deps := discoverQEMUDepsFromPATHAndEnv(t)
	if deps.QEMUPath == "" || deps.OVMFCodePath == "" || deps.OVMFVarsPath == "" {
		if allowSkip {
			t.Skip("QEMU/OVMF required; install qemu and set WRELA_OVMF_CODE/WRELA_OVMF_VARS")
		}
		t.Fatalf("QEMU/OVMF required for this task; install qemu and set WRELA_OVMF_CODE/WRELA_OVMF_VARS")
	}
	return deps
}

type networkRun struct {
	Context    func() context.Context
	Output     string
	ReportPath string
}

func (r networkRun) WaitForSerial(t *testing.T, marker string) string

func reserveUDPPorts(t *testing.T) (peerPort int, guestPort int)

func buildNetworkFixture(t *testing.T, deps qemuDeps, reportPath string) string

func startNetworkFixture(t *testing.T, deps qemuDeps, peerPort int, guestPort int, successText string) networkRun

func buildAndRunNetworkFixture(t *testing.T, deps qemuDeps, opts qemu.Options) string

func assertNetworkSerialMarkers(t *testing.T, out string, markers ...string)

func assertNetworkReport(t *testing.T, path string, want networkReportWant)

func expectARPReply(t *testing.T, peer *qemu.EthernetPeer, ctx context.Context, guestMAC nettrace.MAC, guestIP nettrace.IPv4, hostMAC nettrace.MAC, hostIP nettrace.IPv4) nettrace.ARPPacket

func expectICMPEchoReply(t *testing.T, peer *qemu.EthernetPeer, ctx context.Context, guestMAC nettrace.MAC, hostMAC nettrace.MAC, guestIP nettrace.IPv4, hostIP nettrace.IPv4) nettrace.ICMPEcho

func expectUDPEchoReply(t *testing.T, peer *qemu.EthernetPeer, ctx context.Context, guestPort uint16, hostPort uint16, payload []byte) nettrace.UDPDatagram

type networkReportWant struct {
	Driver         string
	GuestMAC       string
	GuestIP        string
	RXDescriptors  int
	TXDescriptors  int
	FlightRecorderCapacity int
}
```

`startNetworkFixture` must use `qemu.E1000eSocketNetdevArgs(...)` and pass the static MAC `52:54:00:12:34:56`. It waits for only the supplied `successText`; protocol-specific sends and receives stay in the test body so the packet ordering is visible.

QEMU worker setup:

```bash
command -v qemu-system-x86_64
test -f "$WRELA_OVMF_CODE"
test -f "$WRELA_OVMF_VARS"
```

Task completion requires those three commands to pass on the worker that commits any QEMU-dependent task.

### Task 9A: e1000e Reset, MAC, And Link

**Files:**

- Modify: `wrela/machine/x86_64/e1000e.wrela`
- Modify: `tests/e2e/fixtures/network_substrate/program.wrela`
- Create: `tests/e2e/network_substrate_helpers_test.go`
- Create: `tests/e2e/network_driver_qemu_test.go`

**Prerequisites:** Tasks 2, 6, and 8.

**Description:** Implement the first driver checkpoint: QEMU boots, the driver resets the device, reads the MAC from RAL/RAH, waits for link, and emits serial markers. This task stops before descriptor allocation.

**Acceptance Criteria:**

- QEMU boots the network fixture with e1000e enabled.
- Serial output contains `network: e1000e init`, `network: mac 52:54:00:12:34:56`, and `network: link up`.
- Reset timeout is bounded and boot-fatal code is `0xAC090001`.
- Link timeout is bounded and boot-fatal code is `0xAC090002`.

**Code Examples:**

Reset sequence:

```wrela
fn reset(self) {
    let ctrl = self.mmio.read32(offset = E1000_CTRL)
    self.mmio.write32(offset = E1000_CTRL, value = ctrl | E1000_CTRL_RST)
    let spins = NETWORK_E1000E_RESET_SPINS_QEMU
    while spins > 0 {
        let after = self.mmio.read32(offset = E1000_CTRL)
        if (after & E1000_CTRL_RST) == 0 {
            return
        }
        spins = spins - 1
    }
    self.dma.arena.root.region.panic.fail(code = 0xAC090001)
}
```

Link wait:

```wrela
const NETWORK_E1000E_RESET_SPINS_QEMU: U64 = 100000
const NETWORK_E1000E_LINK_SPINS_QEMU: U64 = 100000
const NETWORK_E1000E_RESET_SPINS_REAL: U64 = 5000000
const NETWORK_E1000E_LINK_SPINS_REAL: U64 = 5000000

fn wait_link_up(self) {
    let spins = NETWORK_E1000E_LINK_SPINS_QEMU
    while spins > 0 {
        let status = self.mmio.read32(offset = E1000_STATUS)
        if (status & E1000_STATUS_LU) != 0 {
            return
        }
        spins = spins - 1
    }
    self.dma.arena.root.region.panic.fail(code = 0xAC090002)
}
```

Ring programming sequence:

```wrela
fn initialize_rings(self) {
    self.rx.descriptors = self.dma.buffer(identity = ArenaIdentity(label = "network.e1000e.rx.desc"), length = NETWORK_RX_DESC_COUNT * E1000_DESC_BYTES, align = 16)
    self.tx.descriptors = self.dma.buffer(identity = ArenaIdentity(label = "network.e1000e.tx.desc"), length = NETWORK_TX_DESC_COUNT * E1000_DESC_BYTES, align = 16)

    self.program_rx_descriptors()
    self.program_tx_descriptors()

    self.mmio.write32(offset = E1000_RDBAL, value = self.rx.descriptors.slots.address & 0xFFFFFFFF)
    self.mmio.write32(offset = E1000_RDBAH, value = self.rx.descriptors.slots.address >> 32)
    self.mmio.write32(offset = E1000_RDLEN, value = NETWORK_RX_DESC_COUNT * E1000_DESC_BYTES)
    self.mmio.write32(offset = E1000_RDH, value = 0)
    self.mmio.write32(offset = E1000_RDT, value = NETWORK_RX_DESC_COUNT - 1)

    self.mmio.write32(offset = E1000_TDBAL, value = self.tx.descriptors.slots.address & 0xFFFFFFFF)
    self.mmio.write32(offset = E1000_TDBAH, value = self.tx.descriptors.slots.address >> 32)
    self.mmio.write32(offset = E1000_TDLEN, value = NETWORK_TX_DESC_COUNT * E1000_DESC_BYTES)
    self.mmio.write32(offset = E1000_TDH, value = 0)
    self.mmio.write32(offset = E1000_TDT, value = 0)
    self.tx.clean_head = 0
    self.tx.next_to_use = 0
}

fn enable_rx_tx(self) {
    self.mmio.write32(offset = E1000_RCTL, value = E1000_RCTL_EN | E1000_RCTL_BAM | E1000_RCTL_BSIZE_2048 | E1000_RCTL_SECRC)
    self.mmio.write32(offset = E1000_TCTL, value = E1000_TCTL_EN | E1000_TCTL_PSP | E1000_TCTL_CT_16 | E1000_TCTL_COLD_64)
}
```

MAC read:

```wrela
fn read_mac(self) -> MacAddress {
    let ral = self.mmio.read32(offset = E1000_RAL0)
    let rah = self.mmio.read32(offset = E1000_RAH0)
    return MacAddress(
        b0 = ral & 0xFF,
        b1 = (ral >> 8) & 0xFF,
        b2 = (ral >> 16) & 0xFF,
        b3 = (ral >> 24) & 0xFF,
        b4 = rah & 0xFF,
        b5 = (rah >> 8) & 0xFF
    )
}
```

E2E test skeleton:

```go
func TestNetworkSubstrateE1000eInitQEMU(t *testing.T) {
	deps := requireQEMUDeps(t, false)
	peerPort, guestPort := reserveUDPPorts(t)
	peer, err := qemu.NewEthernetPeer(peerPort, guestPort)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	out := buildAndRunNetworkFixture(t, deps, qemu.Options{
		ExtraArgs: qemu.E1000eSocketNetdevArgs(qemu.E1000eSocketNetdevOptions{
			PeerUDPPort:  peerPort,
			GuestUDPPort: guestPort,
			GuestMAC:     "52:54:00:12:34:56",
		}),
		SuccessText: "network: e1000e init",
	})
	for _, want := range []string{"network: e1000e init", "network: mac 52:54:00:12:34:56", "network: rx=64 tx=64"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
}
```

**Steps:**

- [ ] Add failing e2e test for `network: e1000e init`, `network: mac ...`, and `network: link up`.
- [ ] Run: `go test ./tests/e2e -run NetworkSubstrateE1000eInit -v`
  Expected: FAIL with missing runtime behavior or missing serial marker.
- [ ] Implement reset, MAC read, link-state poll, and serial output.
- [ ] Run: `go test ./tests/e2e -run NetworkSubstrateE1000eInit -v`
  Expected: PASS on a QEMU/OVMF-capable worker. SKIP is not accepted for task completion.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/machine/x86_64/e1000e.wrela tests/e2e/fixtures/network_substrate/program.wrela tests/e2e/network_substrate_helpers_test.go tests/e2e/network_driver_qemu_test.go
git commit -m "feat: bring up qemu e1000e reset path -Codex Automated"
```

### Task 9B: e1000e Descriptor Ring Programming

**Files:**

- Modify: `wrela/machine/x86_64/e1000e.wrela`
- Modify: `tests/e2e/fixtures/network_substrate/program.wrela`
- Modify: `tests/e2e/network_driver_qemu_test.go`

**Prerequisites:** Task 9A.

**Description:** Allocate RX/TX descriptor rings and packet buffers from `DmaDomainAuthority.buffer`, program ring base/length/head/tail registers, and emit `network: rings programmed`. This task stops before RX/TX enable.

**Acceptance Criteria:**

- Driver writes RDBAL/RDBAH/RDLEN/RDH/RDT and TDBAL/TDBAH/TDLEN/TDH/TDT.
- RX ring initialization sets `RDH = 0` and `RDT = NETWORK_RX_DESC_COUNT - 1`. Leaving `RDT = 0` is a known e1000e bring-up failure because hardware will not see any available receive descriptors.
- TX software indices initialize as `tx.clean_head = 0` and `tx.next_to_use = 0`.
- Serial output contains `network: rings programmed`.

**Code Example:** Use the ring programming sequence shown in Task 9A.

**Steps:**

- [ ] Add failing e2e assertion for `network: rings programmed`.
- [ ] Run: `go test ./tests/e2e -run NetworkSubstrateE1000eInit -v`
  Expected: FAIL with missing `network: rings programmed`.
- [ ] Implement descriptor and packet-buffer allocation plus register programming.
- [ ] Run: `go test ./tests/e2e -run NetworkSubstrateE1000eInit -v`
  Expected: PASS on a QEMU/OVMF-capable worker. SKIP is not accepted for task completion.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/machine/x86_64/e1000e.wrela tests/e2e/fixtures/network_substrate/program.wrela tests/e2e/network_driver_qemu_test.go
git commit -m "feat: program e1000e descriptor rings -Codex Automated"
```

### Task 9C: e1000e RX/TX Enable

**Files:**

- Modify: `wrela/machine/x86_64/e1000e.wrela`
- Modify: `tests/e2e/fixtures/network_substrate/program.wrela`
- Modify: `tests/e2e/network_driver_qemu_test.go`

**Prerequisites:** Task 9B.

**Description:** Enable RX/TX with the exact RCTL/TCTL bits from Section 1 and print the final descriptor capacities.

**Acceptance Criteria:**

- The driver enables RX and TX only after descriptor bases are programmed.
- Serial output contains `network: rx=64 tx=64`.
- `TestNetworkSubstrateE1000eInitQEMU` checks init, MAC, link, ring programming, and final RX/TX capacity markers in one boot.

**Code Example:** Use the `enable_rx_tx` function shown in Task 9A.

**Steps:**

- [ ] Add failing e2e assertion for `network: rx=64 tx=64`.
- [ ] Run: `go test ./tests/e2e -run NetworkSubstrateE1000eInit -v`
  Expected: FAIL with missing `network: rx=64 tx=64`.
- [ ] Implement RX/TX enable and final serial marker.
- [ ] Run: `go test ./tests/e2e -run NetworkSubstrateE1000eInit -v`
  Expected: PASS on a QEMU/OVMF-capable worker. SKIP is not accepted for task completion.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/machine/x86_64/e1000e.wrela tests/e2e/fixtures/network_substrate/program.wrela tests/e2e/network_driver_qemu_test.go
git commit -m "feat: enable e1000e rx tx rings -Codex Automated"
```

### Task 10: e1000e RX Quarantine

**Files:**

- Modify: `wrela/machine/x86_64/e1000e.wrela`
- Modify: `wrela/net/packet.wrela`
- Modify: `tests/e2e/fixtures/network_substrate/program.wrela`
- Modify: `tests/e2e/network_driver_qemu_test.go`

**Prerequisites:** Task 9C.

**Description:** Implement RX descriptor polling. Completed descriptors produce `QuarantinedRxBytes` tied to a `PacketLease`; buffers return to the ring only after the reactor releases or promotes them.

**Acceptance Criteria:**

- Host peer sends one Ethernet frame; guest serial output contains `network: rx quarantine len=60`.
- RX descriptor status DD is checked before reading length.
- RX length is bounded to `NETWORK_PACKET_BUFFER_BYTES`.
- RX errors increment drop counters and recycle the descriptor.
- Recycled RX descriptors clear status/errors/length and advance RDT.
- `poll_rx` returns `Option.None` when no descriptor is completed.

**Code Examples:**

Poll shape:

```wrela
fn poll_rx(self) -> Option<QuarantinedRxBytes> {
    let index = self.rx.next_to_check
    let desc_offset = index * E1000_DESC_BYTES
    let status = self.rx.descriptors.read_u8(offset = desc_offset + 12)
    if (status & E1000_RXD_STAT_DD) == 0 {
        return Option.None
    }
    if (status & E1000_RXD_STAT_EOP) == 0 {
        self.counters.drop_rx_ring_error = self.counters.drop_rx_ring_error + 1
        self.recycle_rx(index = index)
        return Option.None
    }
    let length = self.rx.descriptors.read_u16(offset = desc_offset + 8)
    if length > NETWORK_PACKET_BUFFER_BYTES {
        self.recycle_rx(index = index)
        return Option.None
    }
    let lease = PacketLease(id = self.rx.next_lease_id, epoch = self.rx.epoch)
    self.rx.next_lease_id = self.rx.next_lease_id + 1
    return Option.Some(value = QuarantinedRxBytes(lease = lease, buffer = self.rx.buffer_at(index = index), length = length))
}
```

Descriptor-address rule: `self.rx.desc_base` is the physical address written to `RDBAL/RDBAH`. It is never passed as an offset to `self.rx.descriptors.read_*`. All descriptor reads and writes use offsets relative to `self.rx.descriptors`, as shown with `desc_offset`.

Recycle helper:

```wrela
fn recycle_rx(self, index: U64) {
    let desc_offset = index * E1000_DESC_BYTES
    self.rx.descriptors.write_u16(offset = desc_offset + 8, value = 0)
    self.rx.descriptors.write_u16(offset = desc_offset + 10, value = 0)
    self.rx.descriptors.write_u8(offset = desc_offset + 12, value = 0)
    self.rx.descriptors.write_u8(offset = desc_offset + 13, value = 0)
    self.rx.descriptors.write_u16(offset = desc_offset + 14, value = 0)
    self.rx.next_to_check = (index + 1) & (NETWORK_RX_DESC_COUNT - 1)
    self.mmio.write32(offset = E1000_RDT, value = index)
}
```

E2E host send:

```go
frame := nettrace.EmitEthernet(nettrace.EthernetFrame{
	Dst:       nettrace.MustMAC("52:54:00:12:34:56"),
	Src:       nettrace.MustMAC("52:54:00:fe:ed:01"),
	EtherType: 0x88b5,
	Payload:   bytes.Repeat([]byte{0x5a}, 46),
})
if err := peer.Send(frame); err != nil {
	t.Fatal(err)
}
```

**Steps:**

- [ ] Add failing e2e test that sends one raw Ethernet frame and waits for `network: rx quarantine`.
- [ ] Run: `go test ./tests/e2e -run NetworkSubstrateRxQuarantine -v`
  Expected: FAIL with missing marker.
- [ ] Implement RX descriptor status read, quarantine creation, and recycle helper.
- [ ] In `recycle_rx`, clear descriptor length, checksum, status, errors, and special at relative offsets `+8`, `+10`, `+12`, `+13`, and `+14`; then write `RDT = index`.
- [ ] Add serial marker in the fixture after receiving the first quarantined packet.
- [ ] Run: `go test ./tests/e2e -run NetworkSubstrateRxQuarantine -v`
  Expected: PASS on a QEMU/OVMF-capable worker. SKIP is not accepted for task completion.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/machine/x86_64/e1000e.wrela wrela/net/packet.wrela tests/e2e/fixtures/network_substrate/program.wrela tests/e2e/network_driver_qemu_test.go
git commit -m "feat: quarantine e1000e rx packets -Codex Automated"
```

### Task 11: e1000e TX Frame Submission

**Files:**

- Modify: `wrela/machine/x86_64/e1000e.wrela`
- Modify: `wrela/net/packet.wrela`
- Modify: `tests/e2e/fixtures/network_substrate/program.wrela`
- Modify: `tests/e2e/network_driver_qemu_test.go`

**Prerequisites:** Task 10.

**Description:** Implement fixed-buffer TX frame submission. The network reactor owns TX buffers; `transmit` copies or emits bytes into a TX lease, writes descriptor command bits, updates TDT, and reports `TxFull` when the ring is full.

**Acceptance Criteria:**

- Guest transmits one Ethernet frame to the host peer.
- Host peer receives destination `52:54:00:fe:ed:01`, source `52:54:00:12:34:56`, EtherType `0x88b5`.
- Guest emits one broadcast ARP request smoke frame after the raw TX frame; host peer verifies destination `ff:ff:ff:ff:ff:ff`, EtherType `0x0806`, sender IP `10.10.0.2`, and target IP `10.10.0.1`.
- TX descriptor uses EOP, IFCS, and RS command bits.
- Completed TX descriptors are reclaimed by checking DD status.
- `transmit` returns `Result.Err(TxFull)` when the next descriptor is still device-owned.

**Code Examples:**

TX command bits:

```wrela
const E1000_TX_CMD_EOP: U8 = 1
const E1000_TX_CMD_IFCS: U8 = 2
const E1000_TX_CMD_RS: U8 = 8
```

Transmit descriptor write:

```wrela
fn post_tx(self, index: U64, buffer: DmaBuffer<U8>, length: U16) {
    let desc_offset = index * E1000_DESC_BYTES
    self.tx.descriptors.write_u64(offset = desc_offset + 0, value = buffer.slots.address)
    self.tx.descriptors.write_u16(offset = desc_offset + 8, value = length)
    self.tx.descriptors.write_u8(offset = desc_offset + 11, value = E1000_TX_CMD_EOP | E1000_TX_CMD_IFCS | E1000_TX_CMD_RS)
    self.tx.descriptors.write_u8(offset = desc_offset + 12, value = 0)
    self.mmio.write32(offset = E1000_TDT, value = (index + 1) & (NETWORK_TX_DESC_COUNT - 1))
}
```

TX reclaim algorithm:

```wrela
fn reclaim_tx(self) {
    while self.tx.clean_head != self.tx.next_to_use {
        let desc_offset = self.tx.clean_head * E1000_DESC_BYTES
        let status = self.tx.descriptors.read_u8(offset = desc_offset + 12)
        if (status & E1000_TXD_STAT_DD) == 0 {
            return
        }
        self.tx.mark_buffer_free(index = self.tx.clean_head)
        self.tx.clean_head = (self.tx.clean_head + 1) & (NETWORK_TX_DESC_COUNT - 1)
    }
}
```

Descriptor-address rule: `self.tx.desc_base` is the physical address written to `TDBAL/TDBAH`. It is never used as a `DmaBuffer` offset. TX descriptor reads and writes use offsets relative to `self.tx.descriptors`.

Host receive assertion:

```go
raw, err := peer.Receive(ctx)
if err != nil {
	t.Fatal(err)
}
frame, err := nettrace.ParseEthernet(raw)
if err != nil {
	t.Fatal(err)
}
if frame.EtherType != 0x88b5 {
	t.Fatalf("ether type = %#x", frame.EtherType)
}
```

**Steps:**

- [ ] Add failing e2e test that waits for a guest-transmitted frame.
- [ ] Run: `go test ./tests/e2e -run NetworkSubstrateTxFrame -v`
  Expected: FAIL with host peer receive timeout.
- [ ] Implement TX buffer ownership, descriptor posting, and completion reclaim.
- [ ] Track `tx.clean_head` in software and use the reclaim loop above; do not rely on reading `TDH` as the software clean pointer.
- [ ] Emit one raw test frame and one broadcast ARP smoke frame from fixture after driver init.
- [ ] Run: `go test ./tests/e2e -run NetworkSubstrateTxFrame -v`
  Expected: PASS on a QEMU/OVMF-capable worker. SKIP is not accepted for task completion.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/machine/x86_64/e1000e.wrela wrela/net/packet.wrela tests/e2e/fixtures/network_substrate/program.wrela tests/e2e/network_driver_qemu_test.go
git commit -m "feat: transmit e1000e ethernet frames -Codex Automated"
```

### Task 12: e1000e MSI And Interrupt Acknowledgement

**Files:**

- Modify: `wrela/machine/x86_64/e1000e.wrela`
- Modify: `tests/e2e/fixtures/network_substrate/main.wrela`
- Modify: `tests/e2e/fixtures/network_substrate/program.wrela`
- Modify: `tests/e2e/network_driver_qemu_test.go`

**Prerequisites:** Task 11.

**Description:** Route the e1000e MSI vector, enable RX/TX/link interrupts, and acknowledge interrupt cause status after descriptor drain. The v0 loop still polls, but reports and serial output must prove interrupt wiring exists.

MSI ownership rule: use the existing `PciDevice.claim_msi()` and `MsiCapability.route(vector, target)` APIs in `wrela/machine/x86_64/pci.wrela`. The network driver must not manually parse PCI capability lists or write MSI message address/data registers; that logic is already encapsulated by `MsiCapability.route`.

**Acceptance Criteria:**

- Network fixture routes e1000e MSI to vector `0x43`.
- Driver writes IMS bits for RX, TX, and link-status causes.
- `ack_interrupts` reads ICR and returns bounded status flags.
- Serial output contains `network: irq msi vector=0x43`.
- Report graph records e1000e MSI claim as a network hardware authority.

**Code Examples:**

Interrupt status:

```wrela
data NetworkInterruptStatus {
    rx: Bool
    tx: Bool
    link: Bool
}

fn ack_interrupts(self) -> NetworkInterruptStatus {
    let cause = self.mmio.read32(offset = E1000_ICR)
    return NetworkInterruptStatus(
        rx = (cause & 0x80) != 0,
        tx = (cause & 0x1) != 0,
        link = (cause & 0x4) != 0
    )
}
```

Route:

```wrela
let network_irq = hardware.hardware_plan.network.nic_msi.route(
    vector = InterruptVector(value = 0x43),
    target = hardware.hardware_plan.interrupts.local_apic
)
```

**Steps:**

- [ ] Add failing e2e assertion for `network: irq msi vector=0x43`.
- [ ] Run: `go test ./tests/e2e -run NetworkSubstrateE1000eInterrupt -v`
  Expected: FAIL with missing marker.
- [ ] Route MSI, enable IMS bits, and add `ack_interrupts`.
- [ ] Add serial marker after route succeeds.
- [ ] Run: `go test ./tests/e2e -run NetworkSubstrateE1000eInterrupt -v`
  Expected: PASS on a QEMU/OVMF-capable worker. SKIP is not accepted for task completion.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/machine/x86_64/e1000e.wrela tests/e2e/fixtures/network_substrate/main.wrela tests/e2e/fixtures/network_substrate/program.wrela tests/e2e/network_driver_qemu_test.go
git commit -m "feat: route e1000e msi interrupts -Codex Automated"
```

---

## 8. Phase 4: Ethernet, ARP, IPv4, ICMP, And UDP

**Description:** Promote quarantined bytes through structural protocol verification and emit packets through the same protocol modules. Each protocol task has host-side packet trace tests and QEMU e2e checks.

**Acceptance Criteria:**

- Malformed packets are dropped, counted, and recycled.
- Valid packets advance typestate in this order: quarantine, Ethernet, IPv4, transport.
- ARP, ICMP, and UDP output bytes match host-side `compiler/nettrace` expectations.
- The reactor never exposes raw mutable packet memory to application executors.

**Code Example:**

```text
QuarantinedRxBytes
  -> EthernetVerifier.verify(...)
  -> VerifiedEthernetFrame
  -> Ipv4Verifier.verify(...)
  -> VerifiedIpv4Packet
  -> IcmpVerifier.verify(...) or UdpVerifier.verify(...)
```

### Task 13: Ethernet II Parse And Emit

**Files:**

- Modify: `wrela/net/packet.wrela`
- Modify: `wrela/net/ethernet.wrela`
- Modify: `compiler/nettrace/packet_test.go`
- Modify: `tests/e2e/fixtures/network_substrate/program.wrela`
- Modify: `tests/e2e/network_protocol_qemu_test.go`

**Prerequisites:** Host-side `compiler/nettrace` work may start after Tasks 3 and 4. Wrela/e2e integration requires Tasks 10 and 11.

**Description:** Implement Ethernet II structural verification and frame emission. Ethernet accepts broadcast or the NIC's unicast MAC and drops unrelated unicast frames.

**Acceptance Criteria:**

- Ethernet parser rejects frames shorter than 14 bytes.
- Ethernet parser accepts broadcast destination.
- Ethernet parser accepts destination matching local MAC.
- Ethernet parser rejects unrelated unicast destination and increments `drop_wrong_mac`.
- Emitter writes destination, source, EtherType, and payload in big-endian format.
- Host peer receives emitted frame with correct MACs and EtherType.

**Code Examples:**

Verifier:

```wrela
fn verify(self, rx: QuarantinedRxBytes, local_mac: MacAddress) -> Result<VerifiedEthernetFrame, MalformedEthernetFrame> {
    if rx.length < 14 {
        return Result.Err(error = MalformedEthernetFrame(reason = 1))
    }
    let dst = self.read_mac(buffer = rx.buffer, offset = 0)
    if self.accepts_destination(dst = dst, local = local_mac) == false {
        return Result.Err(error = MalformedEthernetFrame(reason = 2))
    }
    let src = self.read_mac(buffer = rx.buffer, offset = 6)
    let ether_type = self.read_be16(buffer = rx.buffer, offset = 12)
    return Result.Ok(value = VerifiedEthernetFrame(
        lease = rx.lease,
        dst = dst,
        src = src,
        ether_type = ether_type,
        payload = PacketSlice(lease = rx.lease, buffer = rx.buffer, offset = 14, length = rx.length - 14)
    ))
}
```

Emitter:

```wrela
fn write_header(self, out: TxFrameLease, dst: MacAddress, src: MacAddress, ether_type: U16) {
    out.write_mac(offset = 0, value = dst)
    out.write_mac(offset = 6, value = src)
    out.write_be16(offset = 12, value = ether_type)
}
```

Host trace tests:

```go
func TestEthernetParseRejectsShortFrame(t *testing.T) {
	_, err := nettrace.ParseEthernet([]byte{0x01, 0x02, 0x03})
	if !errors.Is(err, nettrace.ErrShortEthernet) {
		t.Fatalf("err = %v", err)
	}
}

func TestEthernetParseAcceptsHostICMPRequest(t *testing.T) {
	frame, err := nettrace.ParseEthernet(nettrace.HostICMPEchoRequest)
	if err != nil {
		t.Fatal(err)
	}
	if frame.EtherType != 0x0800 {
		t.Fatalf("ether type = %#x", frame.EtherType)
	}
	if frame.Dst != nettrace.MustMAC("52:54:00:12:34:56") {
		t.Fatalf("dst = %s", frame.Dst)
	}
}
```

**Steps:**

- [ ] Add failing Wrela source-shape and e2e assertions for Ethernet drop/accept.
- [ ] Run: `go test ./compiler/nettrace ./tests/e2e -run 'Ethernet|NetworkSubstrateEthernet' -v`
  Expected: FAIL with missing Ethernet verifier behavior.
- [ ] Implement Ethernet helpers and fixture path.
- [ ] Run: `go test ./compiler/nettrace ./tests/e2e -run 'Ethernet|NetworkSubstrateEthernet' -v`
  Expected: PASS on a QEMU/OVMF-capable worker. SKIP is not accepted for task completion.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/net/packet.wrela wrela/net/ethernet.wrela compiler/nettrace/packet_test.go tests/e2e/fixtures/network_substrate/program.wrela tests/e2e/network_protocol_qemu_test.go
git commit -m "feat: add ethernet frame verification -Codex Automated"
```

### Task 14: ARP Request, Reply, And Cache

**Files:**

- Modify: `wrela/net/arp.wrela`
- Modify: `wrela/net/reactor.wrela`
- Modify: `compiler/nettrace/trace_test.go`
- Modify: `tests/e2e/network_protocol_qemu_test.go`

**Prerequisites:** Host-side `compiler/nettrace` work may start after Task 13 host tests. Wrela/e2e integration requires Task 13 runtime integration.

**Description:** Implement ARP parsing, reply emission for the local static IPv4, ARP request emission, and a 16-entry FIFO ARP table.

**Acceptance Criteria:**

- ARP verifier requires HTYPE Ethernet, PTYPE IPv4, HLEN 6, PLEN 4, and operation 1 or 2.
- Reactor replies to ARP requests for `10.10.0.2`.
- Reactor ignores ARP requests for other IPs.
- ARP table stores sender protocol/hardware mapping from valid requests and replies.
- FIFO replacement overwrites the oldest entry when full.
- E2E host peer receives ARP reply with guest MAC and guest IP.

**Code Examples:**

ARP entry:

```wrela
data ArpEntry {
    ip: Ipv4Address
    mac: MacAddress
    epoch: U64
    valid: Bool
}

data ArpTable {
    count: U64
    next_victim: U64
    entry0: ArpEntry
    entry1: ArpEntry
    entry2: ArpEntry
    entry3: ArpEntry
    entry4: ArpEntry
    entry5: ArpEntry
    entry6: ArpEntry
    entry7: ArpEntry
    entry8: ArpEntry
    entry9: ArpEntry
    entry10: ArpEntry
    entry11: ArpEntry
    entry12: ArpEntry
    entry13: ArpEntry
    entry14: ArpEntry
    entry15: ArpEntry
}
```

ARP reply decision:

```wrela
if packet.operation == 1 {
    if packet.target_ip.value == config.local_ip.value {
        self.arp_table.put(ip = packet.sender_ip, mac = packet.sender_mac, epoch = self.epoch.current)
        self.send_arp_reply(request = packet)
    }
}
```

Host peer assertion:

```go
if err := peer.Send(nettrace.EmitARPRequest(hostMAC, hostIP, guestMAC, guestIP)); err != nil {
	t.Fatal(err)
}
reply := expectARPReply(t, peer, ctx, guestMAC, guestIP, hostMAC, hostIP)
if reply.Operation != 2 {
	t.Fatalf("ARP operation = %d", reply.Operation)
}
```

Host trace test:

```go
func TestARPRequestBytesParseAndReply(t *testing.T) {
	req, err := nettrace.ParseARP(nettrace.HostARPRequest[14:])
	if err != nil {
	t.Fatal(err)
}
	if req.Operation != 1 || req.TargetIP != nettrace.MustIPv4("10.10.0.2") {
		t.Fatalf("arp request = %#v", req)
	}
	reply := nettrace.EmitARPReply(req, nettrace.MustMAC("52:54:00:12:34:56"))
	out, err := nettrace.ParseEthernet(reply)
	if err != nil {
	t.Fatal(err)
}
	if out.EtherType != 0x0806 {
		t.Fatalf("ether type = %#x", out.EtherType)
	}
}
```

**Steps:**

- [ ] Add failing nettrace and e2e ARP tests.
- [ ] Run: `go test ./compiler/nettrace ./tests/e2e -run 'ARP|NetworkSubstrateARP' -v`
  Expected: FAIL with missing ARP behavior.
- [ ] Implement ARP parser, table, request emitter, reply emitter, and reactor dispatch.
- [ ] Run: `go test ./compiler/nettrace ./tests/e2e -run 'ARP|NetworkSubstrateARP' -v`
  Expected: PASS on a QEMU/OVMF-capable worker. SKIP is not accepted for task completion.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/net/arp.wrela wrela/net/reactor.wrela compiler/nettrace/trace_test.go tests/e2e/network_protocol_qemu_test.go
git commit -m "feat: answer arp on static ipv4 -Codex Automated"
```

### Task 15: IPv4 Parse, Emit, Checksum, And Static Routing

**Files:**

- Modify: `wrela/net/ipv4.wrela`
- Modify: `wrela/net/config.wrela`
- Modify: `wrela/net/reactor.wrela`
- Modify: `compiler/nettrace/checksum_test.go`
- Modify: `compiler/nettrace/trace_test.go`

**Prerequisites:** Host-side `compiler/nettrace` work may start after Task 14 host tests. Wrela/reactor integration requires Task 14 runtime integration.

**Description:** Implement IPv4 header verification, header emission, checksum calculation, and static local-subnet/gateway routing.

**Acceptance Criteria:**

- Parser requires version 4, IHL 5, total length within Ethernet payload, no fragments, destination equals local IP or broadcast where protocol allows, and valid header checksum.
- Packets with options, fragments, bad checksum, or wrong destination are dropped with distinct counters.
- Emitter writes IHL 5, TTL 64, protocol, total length, source, destination, and checksum.
- Static route chooses direct ARP for same subnet and gateway ARP for non-local destinations.
- Nettrace and Wrela constants agree on guest/host IP values.

**Code Examples:**

Checksum algorithm:

```wrela
fn checksum_header(self, header: PacketSlice) -> U16 {
    let sum = 0
    let offset = 0
    while offset < header.length {
        if offset != 10 {
            sum = sum + header.read_be16(relative = offset)
        }
        offset = offset + 2
    }
    while sum > 0xFFFF {
        sum = (sum & 0xFFFF) + (sum >> 16)
    }
    return (0xFFFF - sum) & 0xFFFF
}
```

Static route:

```wrela
fn next_hop(self, dst: Ipv4Address) -> Ipv4Address {
    if (dst.value & self.config.subnet_mask.value) == (self.config.local_ip.value & self.config.subnet_mask.value) {
        return dst
    }
    return self.config.gateway
}
```

Host checksum tests:

```go
func TestIPv4HeaderChecksumMatchesFixture(t *testing.T) {
	ip := nettrace.HostICMPEchoRequest[14:34]
	if got := nettrace.OnesComplement16(ip); got != 0 {
		t.Fatalf("icmp request IPv4 checksum verification = %#x", got)
	}
	bad := append([]byte(nil), ip...)
	bad[10], bad[11] = 0, 0
	if got := nettrace.OnesComplement16(bad); got == 0 {
		t.Fatalf("zeroed checksum unexpectedly verified")
	}
}

func TestIPv4DropsFragmentsAndOptions(t *testing.T) {
	for _, raw := range [][]byte{
		nettrace.WithIPv4Flags(nettrace.HostICMPEchoRequest, 0x2000),
		nettrace.WithIPv4IHL(nettrace.HostICMPEchoRequest, 6),
	} {
		_, err := nettrace.ParseIPv4(raw[14:])
		if !errors.Is(err, nettrace.ErrUnsupportedIPv4Feature) {
			t.Fatalf("err = %v", err)
		}
	}
}
```

**Steps:**

- [ ] Add failing IPv4 checksum and route tests.
- [ ] Run: `go test ./compiler/nettrace -run 'IPv4|Checksum' -v`
  Expected: FAIL with missing checksum behavior.
- [ ] Implement Wrela IPv4 parser/emitter and mirror fixture checks in nettrace.
- [ ] Run: `go test ./compiler/nettrace -run 'IPv4|Checksum' -v`
  Expected: PASS.
- [ ] Run: `go test ./compiler/sem -run TestNetworkSourceShape -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/net/ipv4.wrela wrela/net/config.wrela wrela/net/reactor.wrela compiler/nettrace/checksum_test.go compiler/nettrace/trace_test.go
git commit -m "feat: add static ipv4 verification -Codex Automated"
```

### Task 16: ICMP Echo

**Files:**

- Modify: `wrela/net/icmp.wrela`
- Modify: `wrela/net/reactor.wrela`
- Modify: `compiler/nettrace/trace_test.go`
- Modify: `tests/e2e/network_protocol_qemu_test.go`

**Prerequisites:** Host-side `compiler/nettrace` work may start after Task 15 host tests. Wrela/reactor integration requires Task 15 runtime integration.

**Description:** Implement ICMP echo request parsing and echo reply emission. This is the first user-visible network protocol proof.

**Acceptance Criteria:**

- ICMP parser accepts type 8/code 0 echo request and type 0/code 0 echo reply.
- ICMP parser validates checksum.
- Reactor replies to host echo request for `10.10.0.2`.
- Reply preserves identifier, sequence, and payload bytes.
- Serial output contains `network: icmp echo request` and `network: icmp echo reply`.
- Host peer validates the reply through nettrace.

**Code Examples:**

ICMP parser:

```wrela
data IcmpEcho {
    kind: U8
    code: U8
    identifier: U16
    sequence: U16
    payload: PacketSlice
}

fn verify_echo(self, packet: VerifiedIpv4Packet) -> Result<IcmpEcho, MalformedIcmpPacket> {
    if packet.payload.length < 8 {
        return Result.Err(error = MalformedIcmpPacket(reason = 1))
    }
    let kind = packet.payload.read_u8(relative = 0)
    let code = packet.payload.read_u8(relative = 1)
    if code != 0 {
        return Result.Err(error = MalformedIcmpPacket(reason = 2))
    }
    if self.checksum(payload = packet.payload) != 0 {
        return Result.Err(error = MalformedIcmpPacket(reason = 3))
    }
    return Result.Ok(value = IcmpEcho(kind = kind, code = code, identifier = packet.payload.read_be16(relative = 4), sequence = packet.payload.read_be16(relative = 6), payload = packet.payload.slice(relative = 8)))
}
```

Host assertion:

```go
if err := peer.Send(nettrace.EmitICMPEchoRequest(hostMAC, guestMAC, hostIP, guestIP, 0x1234, 1, []byte("wrela"))); err != nil {
	t.Fatal(err)
}
reply := expectICMPEchoReply(t, peer, ctx, guestMAC, hostMAC, guestIP, hostIP)
if reply.Identifier != 0x1234 || reply.Sequence != 1 || string(reply.Payload) != "wrela" {
	t.Fatalf("bad icmp reply: %#v", reply)
}
```

Host trace test:

```go
func TestICMPEchoReplyPreservesIdentifierSequenceAndPayload(t *testing.T) {
	req, err := nettrace.ParseICMPEchoRequest(nettrace.HostICMPEchoRequest)
	if err != nil {
	t.Fatal(err)
}
	reply := nettrace.EmitICMPEchoReply(req)
	got, err := nettrace.ParseICMPEchoReply(reply)
	if err != nil {
	t.Fatal(err)
}
	if got.Identifier != 0x1234 || got.Sequence != 1 || string(got.Payload) != "wrela" {
		t.Fatalf("reply = %#v", got)
	}
}
```

**Steps:**

- [ ] Add failing ICMP nettrace and e2e tests.
- [ ] Run: `go test ./compiler/nettrace ./tests/e2e -run 'ICMP|NetworkSubstrateICMP' -v`
  Expected: FAIL with missing ICMP reply.
- [ ] Implement ICMP verify/reply and reactor dispatch.
- [ ] Run: `go test ./compiler/nettrace ./tests/e2e -run 'ICMP|NetworkSubstrateICMP' -v`
  Expected: PASS on a QEMU/OVMF-capable worker. SKIP is not accepted for task completion.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/net/icmp.wrela wrela/net/reactor.wrela compiler/nettrace/trace_test.go tests/e2e/network_protocol_qemu_test.go
git commit -m "feat: reply to icmp echo requests -Codex Automated"
```

### Task 17: UDP Endpoint Table And Echo

**Files:**

- Modify: `wrela/net/udp.wrela`
- Modify: `wrela/net/reactor.wrela`
- Modify: `compiler/nettrace/trace_test.go`
- Modify: `tests/e2e/network_protocol_qemu_test.go`

**Prerequisites:** Host-side `compiler/nettrace` work may start after Task 16 host tests. Wrela/reactor integration requires Task 16 runtime integration.

**Description:** Implement UDP parsing, checksum emission, endpoint table dispatch, and a UDP echo endpoint on port `7` for e2e verification.

**Acceptance Criteria:**

- UDP parser requires length at least 8 and no longer than IPv4 payload length.
- IPv4 UDP zero checksum is accepted on receive.
- Emitted UDP replies include pseudo-header checksum.
- Endpoint table rejects duplicate ports semantically through Task 7 and returns `UdpPortClosed` at runtime for unopened ports.
- Host peer sends UDP payload to guest port `7` and receives the same payload back from guest port `7`.
- Serial output contains `network: udp echo`.

**Code Examples:**

UDP checksum pseudo-header:

```wrela
fn checksum_ipv4(self, src: Ipv4Address, dst: Ipv4Address, udp_length: U16, payload: PacketSlice) -> U16 {
    let sum = 0
    sum = sum + ((src.value >> 16) & 0xFFFF)
    sum = sum + (src.value & 0xFFFF)
    sum = sum + ((dst.value >> 16) & 0xFFFF)
    sum = sum + (dst.value & 0xFFFF)
    sum = sum + 17
    sum = sum + udp_length
    sum = sum + payload.ones_complement_words()
    while sum > 0xFFFF {
        sum = (sum & 0xFFFF) + (sum >> 16)
    }
    let out = (0xFFFF - sum) & 0xFFFF
    if out == 0 {
        return 0xFFFF
    }
    return out
}
```

Endpoint dispatch:

```wrela
fn dispatch(self, packet: VerifiedUdpDatagram) -> Result<Unit, UdpPortClosed> {
    let index = 0
    while index < self.count {
        let endpoint = self.at(index = index)
        if endpoint.local_port == packet.dst_port {
            let copied = UdpApplicationDatagram(
                src_ip = packet.src_ip,
                dst_ip = packet.dst_ip,
                src_port = packet.src_port,
                dst_port = packet.dst_port,
                payload_length = packet.payload.length,
                payload0 = packet.payload.read_u8(relative = 0),
                payload1 = packet.payload.read_u8(relative = 1),
                payload2 = packet.payload.read_u8(relative = 2),
                payload3 = packet.payload.read_u8(relative = 3),
                payload4 = packet.payload.read_u8(relative = 4),
                payload5 = packet.payload.read_u8(relative = 5),
                payload6 = packet.payload.read_u8(relative = 6),
                payload7 = packet.payload.read_u8(relative = 7)
            )
            endpoint.rx_queue.push(value = copied)
            return Result.Ok(value = Unit())
        }
        index = index + 1
    }
    return Result.Err(error = UdpPortClosed(port = packet.dst_port))
}
```

Host trace test:

```go
func TestUDPEchoFixtureChecksumAndPayload(t *testing.T) {
	req, err := nettrace.ParseUDPDatagram(nettrace.HostUDPEchoRequest)
	if err != nil {
	t.Fatal(err)
}
	if req.DstPort != 7 || string(req.Payload) != "hi" {
		t.Fatalf("udp request = %#v", req)
	}
	reply := nettrace.EmitUDPEchoReply(req)
	got, err := nettrace.ParseUDPDatagram(reply)
	if err != nil {
	t.Fatal(err)
}
	if got.SrcPort != 7 || got.DstPort != req.SrcPort || string(got.Payload) != "hi" {
		t.Fatalf("udp reply = %#v", got)
	}
}

func TestUDPRuntimePortClosedMapsToCounter(t *testing.T) {
	table := nettrace.NewEndpointTable(7)
	err := table.Dispatch(nettrace.UDPDatagram{DstPort: 9})
	if !errors.Is(err, nettrace.ErrUDPPortClosed) {
		t.Fatalf("err = %v", err)
	}
}
```

**Steps:**

- [ ] Add failing UDP nettrace and e2e tests.
- [ ] Run: `go test ./compiler/nettrace ./tests/e2e -run 'UDP|NetworkSubstrateUDP' -v`
  Expected: FAIL with missing UDP echo.
- [ ] Implement UDP parser, checksum, endpoint table, and echo endpoint.
- [ ] Run: `go test ./compiler/nettrace ./tests/e2e -run 'UDP|NetworkSubstrateUDP' -v`
  Expected: PASS on a QEMU/OVMF-capable worker. SKIP is not accepted for task completion.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/net/udp.wrela wrela/net/reactor.wrela compiler/nettrace/trace_test.go tests/e2e/network_protocol_qemu_test.go
git commit -m "feat: add udp endpoint dispatch -Codex Automated"
```

---

## 9. Phase 5: Reactor Budgets, Epochs, Recorder, And Reports

**Description:** Add the operational controls that make the stack reviewable: lane budgets, timer/epoch state, bounded drop counters, flight recorder, and compiler image report integration.

**Acceptance Criteria:**

- Reactor loop charges urgent, normal, and expensive lane budgets.
- Network epoch is initialized and advances on NIC reset.
- Flight recorder stores metadata only.
- Report includes every field required by Section 1 and design success criteria.
- Authority audit includes network device, DMA domain, interrupt, and packet-buffer ownership.

**Code Example:**

```wrela
data NetworkReactorBudgets {
    urgent: NetworkLaneBudget
    normal: NetworkLaneBudget
    expensive: NetworkLaneBudget
}

data NetworkEpoch {
    current: U64

    fn advance_for_nic_reset(self) {
        self.current = self.current + 1
    }
}
```

### Task 18: Network Reactor Loop And Lane Budgets

**Files:**

- Modify: `wrela/net/reactor.wrela`
- Modify: `tests/e2e/fixtures/network_substrate/program.wrela`
- Modify: `compiler/sem/network_graph_test.go`
- Create: `tests/fixtures/negative/network_bad_reactor_slot.wrela`
- Create: `tests/fixtures/negative/network_missing_egress_authority.wrela`

**Prerequisites:** Tasks 12 and 17.

**Description:** Implement the dedicated reactor loop with explicit urgent, normal, and expensive lanes. v0 uses only urgent and normal work, but expensive budget is present and reported for future TLS/HTTP work.

**Acceptance Criteria:**

- `NetworkReactor` has fields `slot`, `loop`, `memory`, `driver`, `config`, `budgets`, `counters`, `arp_table`, `udp_endpoints`, `flight_recorder`, and `epoch`.
- Urgent lane drains RX/TX, ARP, ICMP, and interrupt ack.
- Normal lane handles UDP endpoint dispatch.
- Expensive lane exists with budget tokens but no v0 work items.
- All three lane budgets are non-zero in the network fixture.
- Semantic graph rejects zero lane budget with `SEM0174`.
- Semantic graph rejects reactor placement outside the dedicated network slot with `SEM0173`.
- Semantic graph rejects network transmit paths without a `NetworkReactorAuthority` egress declaration with `SEM0181`.

**Code Examples:**

Reactor skeleton:

```wrela
executor NetworkReactor {
    slot: ExecutorSlot
    loop: HotPollPolicy
    memory: ExecutorMemory
    driver: E1000ePath
    config: NetworkStaticConfig
    budgets: NetworkReactorBudgets
    counters: NetworkCounters
    arp_table: ArpTable
    udp_endpoints: UdpEndpointTable
    flight_recorder: FlightRecorder
    epoch: NetworkEpoch

    start fn run(self) -> never {
        while true {
            self.run_urgent_lane()
            self.run_normal_lane()
            self.run_expensive_lane()
            self.loop.wait()
        }
    }
}
```

Budget check:

```go
func TestNetworkLaneBudgetsRejectZero(t *testing.T) {
	_, ds := checkNegativeFixture(t, "network_zero_lane_budget.wrela")
	if !hasCode(ds, diag.SEM0174) {
		t.Fatalf("expected SEM0174, got %#v", ds)
	}
}
```

**Steps:**

- [ ] Add failing semantic tests for zero lane budget, bad reactor slot, and missing egress authority.
- [ ] Run: `go test ./compiler/sem -run 'NetworkLaneBudgets|NetworkReactorSlot|NetworkEgressAuthority' -v`
  Expected: FAIL with missing `SEM0174`.
- [ ] Implement budget graph extraction and zero checks.
- [ ] Implement reactor slot and egress authority checks.
- [ ] Update reactor fixture to run through lanes.
- [ ] Run: `go test ./compiler/sem -run 'NetworkLaneBudgets|NetworkReactorSlot|NetworkEgressAuthority' -v`
  Expected: PASS.
- [ ] Run: `go test ./tests/e2e -run NetworkSubstrateUDP -v`
  Expected: PASS on a QEMU/OVMF-capable worker. SKIP is not accepted for task completion.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/net/reactor.wrela tests/e2e/fixtures/network_substrate/program.wrela compiler/sem/network_graph_test.go tests/fixtures/negative/network_bad_reactor_slot.wrela tests/fixtures/negative/network_missing_egress_authority.wrela
git commit -m "feat: add network reactor lane budgets -Codex Automated"
```

### Task 19: Network Timer Table And Epoch

**Files:**

- Modify: `wrela/net/reactor.wrela`
- Modify: `wrela/net/arp.wrela`
- Modify: `wrela/net/types.wrela`
- Modify: `tests/e2e/fixtures/network_substrate/main.wrela`
- Modify: `compiler/sem/network_source_shape_test.go`

**Prerequisites:** Task 18.

**Description:** Layer a bounded network timer table and epoch state on top of the existing `TimerAuthority`. v0 uses timers for ARP expiry and periodic status; later DHCP/DNS/TCP can add timer slots without changing the authority model.

**Acceptance Criteria:**

- Network fixture obtains `discovery.timers.require_periodic(period_us = 1000)` and passes it into the network plan.
- `NetworkTimerTable` has fixed capacity `32`.
- ARP entries carry `expires_at_tick` and are invalid after expiry.
- `NetworkEpoch.current` starts at `1`.
- e1000e reset advances epoch once.
- Serial output includes `network: epoch=2` after driver init.

**Code Examples:**

Timer table:

```wrela
const NETWORK_TIMER_SLOTS: U64 = 32

data NetworkTimer {
    kind: U64
    deadline_tick: U64
    active: Bool
}

data NetworkTimerTable {
    tick: U64
    count: U64
    capacity: U64
    slots: Slots<NetworkTimer>
}
```

Fixture allocation:

```wrela
let timer_slots = net_memory.reserve_array(NetworkTimer, count = NETWORK_TIMER_SLOTS)
let timer_table = NetworkTimerTable(tick = 0, count = 0, capacity = NETWORK_TIMER_SLOTS, slots = timer_slots)
```

Epoch reset:

```wrela
fn initialize(self) -> LinkState {
    self.epoch.advance_for_nic_reset()
    self.reset()
    self.initialize_rings()
    return self.link_state()
}
```

**Steps:**

- [ ] Add failing source-shape test for `NetworkTimerTable` and `NetworkEpoch`.
- [ ] Run: `go test ./compiler/sem -run TestNetworkSourceShape -v`
  Expected: FAIL with missing timer/epoch fields.
- [ ] Implement timer and epoch types.
- [ ] Wire timer authority into network fixture and ARP expiry.
- [ ] Run: `go test ./compiler/sem -run TestNetworkSourceShape -v`
  Expected: PASS.
- [ ] Run: `go test ./tests/e2e -run NetworkSubstrateE1000eInit -v`
  Expected: PASS on a QEMU/OVMF-capable worker. SKIP is not accepted for task completion.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/net/reactor.wrela wrela/net/arp.wrela wrela/net/types.wrela tests/e2e/fixtures/network_substrate/main.wrela compiler/sem/network_source_shape_test.go
git commit -m "feat: add network timers and epochs -Codex Automated"
```

### Task 20: Flight Recorder And Drop Counters

**Files:**

- Modify: `wrela/net/flight_recorder.wrela`
- Modify: `wrela/net/reactor.wrela`
- Modify: `wrela/net/types.wrela`
- Modify: `compiler/sem/network_graph_test.go`
- Modify: `tests/e2e/network_runtime_qemu_test.go`

**Prerequisites:** Task 19.

**Description:** Add a fixed-size metadata recorder and distinct drop counters. Recorder entries are small metadata values only; payload bytes are never copied into recorder memory.

**Acceptance Criteria:**

- `FlightRecorder` capacity is exactly `128`.
- Recorder entry fields are `tick`, `epoch`, `kind`, `reason`, `protocol`, `src_ip`, `dst_ip`, `src_port`, `dst_port`, and `length`.
- Recorder writes wrap FIFO style.
- Drops are counted by wrong MAC, short Ethernet, malformed ARP, IPv4 checksum, IPv4 options, IPv4 fragment, ICMP checksum, UDP checksum, UDP port closed, RX ring error, TX ring full.
- Semantic test rejects a recorder with capacity `0` using `SEM0179`.
- E2E sends one wrong-MAC frame and one bad IPv4 checksum frame; serial output reports both counters.

Drop mapping:

| Source error or condition | Counter field | Recorder reason constant |
| --- | --- | --- |
| Ethernet destination does not match local or broadcast | `drop_wrong_mac` | `DROP_WRONG_MAC = 1` |
| Ethernet frame length `< 14` | `drop_short_ethernet` | `DROP_SHORT_ETHERNET = 2` |
| `MalformedArpPacket` | `drop_malformed_arp` | `DROP_MALFORMED_ARP = 3` |
| IPv4 header checksum mismatch | `drop_ipv4_checksum` | `DROP_IPV4_CHECKSUM = 4` |
| IPv4 IHL not equal to 5 | `drop_ipv4_options` | `DROP_IPV4_OPTIONS = 5` |
| IPv4 MF flag or nonzero fragment offset | `drop_ipv4_fragment` | `DROP_IPV4_FRAGMENT = 6` |
| ICMP checksum mismatch | `drop_icmp_checksum` | `DROP_ICMP_CHECKSUM = 7` |
| UDP checksum mismatch | `drop_udp_checksum` | `DROP_UDP_CHECKSUM = 8` |
| `UdpPortClosed` | `drop_udp_port_closed` | `DROP_UDP_PORT_CLOSED = 9` |
| RX descriptor error, oversize packet, or missing EOP | `drop_rx_ring_error` | `DROP_RX_RING_ERROR = 10` |
| `TxFull` | `drop_tx_ring_full` | `DROP_TX_RING_FULL = 11` |

**Code Examples:**

Recorder entry:

```wrela
data FlightRecord {
    tick: U64
    epoch: U64
    kind: U64
    reason: U64
    protocol: U8
    src_ip: Ipv4Address
    dst_ip: Ipv4Address
    src_port: U16
    dst_port: U16
    length: U64
}
```

Record method:

```wrela
fn record_drop(self, reason: U64, protocol: U8, length: U64) {
    let index = self.next_index
    self.write(index = index, value = FlightRecord(
        tick = self.tick,
        epoch = self.epoch,
        kind = 1,
        reason = reason,
        protocol = protocol,
        src_ip = Ipv4Address(value = 0),
        dst_ip = Ipv4Address(value = 0),
        src_port = 0,
        dst_port = 0,
        length = length
    ))
    self.next_index = (index + 1) & (NETWORK_FLIGHT_RECORDER_ENTRIES - 1)
}
```

**Steps:**

- [ ] Add failing e2e test for wrong-MAC and bad-checksum counters and failing semantic test `TestFlightRecorderRejectsZeroCapacity`.
- [ ] Run: `go test ./compiler/sem ./tests/e2e -run 'FlightRecorderRejectsZeroCapacity|NetworkSubstrateDropCounters' -v`
  Expected: FAIL with missing serial counters.
- [ ] Implement recorder storage, counter fields, and reactor calls.
- [ ] Add serial status output for the tested counters.
- [ ] Run: `go test ./compiler/sem ./tests/e2e -run 'FlightRecorderRejectsZeroCapacity|NetworkSubstrateDropCounters' -v`
  Expected: PASS on a QEMU/OVMF-capable worker. SKIP is not accepted for task completion.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/net/flight_recorder.wrela wrela/net/reactor.wrela wrela/net/types.wrela compiler/sem/network_graph_test.go tests/e2e/network_runtime_qemu_test.go
git commit -m "feat: add network flight recorder counters -Codex Automated"
```

### Task 21A: Network Graph Extraction

**Files:**

- Modify: `compiler/sem/image_graph.go`
- Modify: `compiler/sem/network_graph.go`
- Create: `compiler/sem/network_graph_extraction_test.go`

**Prerequisites:** Tasks 7, 12, 18, and 20.

**Description:** Extract network graph facts from the checked Wrela source. This task does not write the report. It only produces typed graph nodes that later report code can consume.

**Acceptance Criteria:**

- `ImageGraph.NetworkDevices` contains one `NetworkDeviceNode` for the fixture's e1000e path.
- `ImageGraph.NetworkDmaDomains` contains the network DMA domain owner, arena label, length, and policy.
- `ImageGraph.NetworkInterrupts` contains vector `0x43` and mode `msi`.
- `ImageGraph.NetworkReactors` contains executor slot, lane budgets, UDP endpoint capacity, ARP capacity, and recorder capacity.
- Graph extraction tests fail if the fixture omits MAC, BAR label, IRQ mode, ring counts, or static IPv4 config.
- Graph extraction rejects two network paths claiming the same PCI identity or DMA domain label with `SEM0168`.

**Code Examples:**

Graph node types:

```go
type NetworkDeviceNode struct {
	Label         string
	Driver        string
	VendorID      uint16
	DeviceID      uint16
	MAC           string
	BARLabel      string
	IRQMode       string
	RXDescriptors int
	TXDescriptors int
	PacketBytes   int
}

type NetworkReactorNode struct {
	Label            string
	ExecutorSlot     int
	UrgentBudget     int
	NormalBudget     int
	ExpensiveBudget  int
	ARPCapacity      int
	UDPEndpoints     int
	RecorderCapacity int
}
```

Extraction test:

```go
func TestExtractsNetworkGraphFactsFromFixture(t *testing.T) {
	checked := checkFixtureForTest(t, "tests/e2e/fixtures/network_substrate/main.wrela")
	graph := BuildImageGraph(checked)
	if len(graph.NetworkDevices) != 1 {
		t.Fatalf("network devices = %#v", graph.NetworkDevices)
	}
	nic := graph.NetworkDevices[0]
	if nic.Driver != "e1000e" || nic.VendorID != 0x8086 || nic.DeviceID != 0x10D3 {
		t.Fatalf("nic = %#v", nic)
	}
	if nic.RXDescriptors != 64 || nic.TXDescriptors != 64 || nic.PacketBytes != 2048 {
		t.Fatalf("ring facts = %#v", nic)
	}
}
```

**Steps:**

- [ ] Add failing graph extraction and duplicate device claim tests.
- [ ] Run: `go test ./compiler/sem -run 'NetworkGraph|ExtractsNetworkGraph|DuplicateDeviceClaim' -v`
  Expected: FAIL with missing graph facts.
- [ ] Add network graph node types.
- [ ] Extract nodes from `E1000ePath(...)`, `claim_dma_domain(...)`, `nic_msi.route(...)`, and `NetworkReactor(...)` calls.
- [ ] Run: `go test ./compiler/sem -run 'NetworkGraph|ExtractsNetworkGraph|DuplicateDeviceClaim' -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add compiler/sem/image_graph.go compiler/sem/network_graph.go compiler/sem/network_graph_extraction_test.go
git commit -m "feat: extract network graph facts -Codex Automated"
```

### Task 21B: Network Report Population And Audit

**Files:**

- Modify: `compiler/sem/report.go`
- Modify: `compiler/sem/report_test.go`
- Modify: `compiler/report/report_test.go`
- Modify: `tests/e2e/network_runtime_qemu_test.go`

**Prerequisites:** Task 21A.

**Description:** Populate `ImageReport.Network` from network graph facts and validate populated network reports using the same top-level report-section pattern as the storage substrate. This makes the image explain its network shape at build time without adding a new authority-audit category.

**Acceptance Criteria:**

- Report includes driver `e1000e`, vendor `0x8086`, device `0x10D3`, BAR label, IRQ mode `msi`, MAC string, DMA policy, memory type, MMIO ordering policy, ring counts, packet buffer count/size, static IPv4 settings, protocols, lane budgets, and flight recorder capacity.
- `NetworkReport.Authorities` includes network NIC, network DMA domain, e1000e MSI, RX packet buffers, TX packet buffers, and reactor executor records.
- Existing generic authority audit sections still record hardware claims, interrupts, DMA buffers, queues, topics, and wake targets.
- `ValidateNetworkReportContent` exists only in this task and fails with `SEM0180` if a populated network report lacks required fields or network authority records.
- E2E build writes report JSON and test reads expected fields.

**Code Examples:**

Report population:

```go
func appendNetworkReport(r *report.ImageReport, g ImageGraph) {
	for _, net := range g.NetworkDevices {
		r.Network.NIC = report.NetworkNICReport{
			Driver:   net.Driver,
			VendorID: net.VendorID,
			DeviceID: net.DeviceID,
			MAC:      net.MAC,
			BAR:      net.BARLabel,
			IRQMode:  net.IRQMode,
		}
	}
	r.Network.Protocols = append(r.Network.Protocols, "ethernet", "arp", "ipv4", "icmp", "udp")
	r.Network.Authorities = append(r.Network.Authorities, report.AuthorityRecord{
		Kind: "network_nic", Label: "network.e1000e", Owner: "network",
	})
}
```

Network report validation:

```go
func ValidateNetworkReportContent(r report.ImageReport) []diag.Diagnostic {
	if !reportHasNetwork(r.Network) {
		return nil
	}
	required := []struct {
		ok   bool
		name string
	}{
		{r.Network.NIC.Driver != "", "nic.driver"},
		{r.Network.NIC.VendorID != 0, "nic.vendor_id"},
		{r.Network.NIC.DeviceID != 0, "nic.device_id"},
		{r.Network.DMA.Policy != "", "dma.policy"},
		{r.Network.Rings.RXDescriptors != 0, "rings.rx_descriptors"},
		{r.Network.Rings.TXDescriptors != 0, "rings.tx_descriptors"},
		{len(r.Network.Protocols) != 0, "protocols"},
		{len(r.Network.Authorities) != 0, "authorities"},
	}
	var ds []diag.Diagnostic
	for _, req := range required {
		if !req.ok {
			ds = append(ds, diag.Diagnostic{Phase: "sem", Code: diag.SEM0180, Severity: diag.Error, Message: "network report missing " + req.name})
		}
	}
	return ds
}
```

Report test:

```go
func TestImageReportIncludesNetworkSubstrate(t *testing.T) {
	checked := &CheckedProgram{ImageGraph: ImageGraph{
		NetworkDevices: []NetworkDeviceNode{{
			Driver: "e1000e", VendorID: 0x8086, DeviceID: 0x10D3,
			MAC: "52:54:00:12:34:56", BARLabel: "network.e1000e.bar0", IRQMode: "msi",
		}},
	}}
	r := BuildImageReport(checked)
	if r.Network.NIC.Driver != "e1000e" || r.Network.NIC.DeviceID != 0x10D3 {
		t.Fatalf("network report = %#v", r.Network)
	}
}
```

**Steps:**

- [ ] Add failing report and network-content validation tests.
- [ ] Run: `go test ./compiler/sem ./compiler/report -run 'Network.*Report|NetworkReportContent|AuthorityAudit' -v`
  Expected: FAIL with missing network population or network report validation.
- [ ] Populate report, network authorities, and existing generic authority audit records from Task 21A graph nodes.
- [ ] Update e2e build helper to write report JSON.
- [ ] Run: `go test ./compiler/sem ./compiler/report -run 'Network.*Report|NetworkReportContent|AuthorityAudit' -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add compiler/sem/report.go compiler/sem/report_test.go compiler/report/report_test.go tests/e2e/network_runtime_qemu_test.go
git commit -m "feat: report network substrate authorities -Codex Automated"
```

---

## 10. Phase 6: End-To-End Acceptance And Documentation

**Description:** Prove the substrate milestone under QEMU, update implementation-status documentation, and run the substrate test sweep. Application protocol tracks continue after this milestone.

**Acceptance Criteria:**

- One e2e test covers init, ARP, ICMP, UDP, drop counters, and report JSON.
- Documentation names exactly what Tasks 0-24 complete and what Tasks 25-41 complete.
- Full-tree tests pass.
- No placeholder terms remain in the plan or new implementation.

**Code Example:**

```bash
go test ./compiler/nettrace -v
go test ./tests/e2e -run NetworkSubstrate -v
go test ./...
```

### Task 22: Full NetworkSubstrate QEMU Scenario

**Files:**

- Modify: `tests/e2e/network_protocol_qemu_test.go`
- Modify: `compiler/nettrace/trace.go`

**Prerequisites:** Tasks 1-21B.

**Description:** Combine the individual e2e checks into one scenario that boots once and verifies driver init, ARP reply, ICMP echo reply, UDP echo reply, drop counters, and report fields.

**Acceptance Criteria:**

- `TestNetworkSubstrateQEMU` boots the fixture once.
- The host peer sends ARP, ICMP echo, UDP echo, wrong-MAC, and bad IPv4 checksum frames.
- The host peer observes ARP reply, ICMP reply, and UDP reply.
- Serial output includes `network: ready`, `network: arp reply`, `network: icmp echo reply`, `network: udp echo`, `network: drop wrong_mac=1`, and `network: drop ipv4_checksum=1`.
- Report JSON assertions check NIC, DMA, rings, static IPv4, protocols, budgets, and flight recorder capacity.

**Code Examples:**

Scenario:

```go
func TestNetworkSubstrateQEMU(t *testing.T) {
	deps := requireQEMUDeps(t, false)
	peerPort, guestPort := reserveUDPPorts(t)
	peer, err := qemu.NewEthernetPeer(peerPort, guestPort)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	wrongMACFrame := nettrace.EmitEthernet(nettrace.EthernetFrame{
		Dst: nettrace.MustMAC("52:54:00:99:99:99"),
		Src: nettrace.MustMAC("52:54:00:fe:ed:01"),
		EtherType: 0x88b5,
		Payload: bytes.Repeat([]byte{0x5a}, 46),
	})
	badIPv4ChecksumFrame := append([]byte(nil), nettrace.HostICMPEchoRequest...)
	badIPv4ChecksumFrame[24] = 0x00
	badIPv4ChecksumFrame[25] = 0x00

	run := startNetworkFixture(t, deps, peerPort, guestPort, "network: ready")
	if err := peer.Send(nettrace.HostARPRequest); err != nil {
		t.Fatal(err)
	}
	expectARPReply(t, peer, run.Context(), nettrace.MustMAC("52:54:00:12:34:56"), nettrace.MustIPv4("10.10.0.2"), nettrace.MustMAC("52:54:00:fe:ed:01"), nettrace.MustIPv4("10.10.0.1"))
	run.WaitForSerial(t, "network: arp reply")
	if err := peer.Send(nettrace.HostICMPEchoRequest); err != nil {
		t.Fatal(err)
	}
	expectICMPEchoReply(t, peer, run.Context(), nettrace.MustMAC("52:54:00:12:34:56"), nettrace.MustMAC("52:54:00:fe:ed:01"), nettrace.MustIPv4("10.10.0.2"), nettrace.MustIPv4("10.10.0.1"))
	run.WaitForSerial(t, "network: icmp echo reply")
	if err := peer.Send(nettrace.HostUDPEchoRequest); err != nil {
		t.Fatal(err)
	}
	expectUDPEchoReply(t, peer, run.Context(), 7, 7777, []byte("hi"))
	run.WaitForSerial(t, "network: udp echo")
	if err := peer.Send(wrongMACFrame); err != nil {
		t.Fatal(err)
	}
	run.WaitForSerial(t, "network: drop wrong_mac=1")
	if err := peer.Send(badIPv4ChecksumFrame); err != nil {
		t.Fatal(err)
	}
	out := run.WaitForSerial(t, "network: drop ipv4_checksum=1")
	assertNetworkSerialMarkers(t, out, "network: ready", "network: arp reply", "network: icmp echo reply", "network: udp echo", "network: drop wrong_mac=1", "network: drop ipv4_checksum=1")
	assertNetworkReport(t, run.ReportPath, networkReportWant{
		Driver: "e1000e", GuestMAC: "52:54:00:12:34:56", GuestIP: "10.10.0.2",
		RXDescriptors: 64, TXDescriptors: 64, FlightRecorderCapacity: 128,
	})
}
```

**Steps:**

- [ ] Add failing consolidated scenario.
- [ ] Run: `go test ./tests/e2e -run NetworkSubstrateQEMU -v`
  Expected: FAIL until helper sequencing and all markers are integrated.
- [ ] Implement peer scenario helpers and report assertions.
- [ ] Run: `go test ./tests/e2e -run NetworkSubstrateQEMU -v`
  Expected: PASS on a QEMU/OVMF-capable worker. SKIP is not accepted for task completion.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add tests/e2e/network_protocol_qemu_test.go compiler/nettrace/trace.go
git commit -m "test: verify network substrate qemu scenario -Codex Automated"
```

### Task 23: Substrate Status Documentation

**Files:**

- Create: `docs/network-stack-status.md`
- Modify: `docs/design/2026-05-18-networking-stack-design.md`

**Prerequisites:** Task 22.

**Description:** Record the exact Task 24 substrate boundary and the concrete post-substrate task order. This task does not mark DHCP, DNS, TCP, TLS, HTTP, QUIC, endpoint data-class flow, vector crypto, constant-time validation, hardware QoS, or real e1000e-family cards as abandoned; it links each one to its implementation task.

**Acceptance Criteria:**

- Status doc says Tasks 0-24 implement static IPv4, ARP, ICMP, UDP echo, e1000e QEMU, and socket-backed L2 e2e.
- Status doc maps DHCP, DNS, TCP, TLS, HTTP/1.1, HTTP/2, QUIC, HTTP/3, endpoint data-class flow, vector crypto, constant-time validation, hardware QoS, and real e1000e-family cards to Tasks 25-41.
- Design doc gains one short implementation-status note pointing to this plan.
- No unrelated design text is reformatted.

**Code Examples:**

Documentation text:

```markdown
## Networking Implementation Status

Tasks 0-24 complete `network-substrate-v0`: QEMU e1000e,
static IPv4, Ethernet II, ARP, ICMP echo, UDP echo, bounded counters, flight
recorder metadata, and network authority reports.

Tasks 25-41 complete `network-application-v1`: DHCPv4, DNS over UDP, TCP over
IPv4, TLS 1.3, HTTP/1.1, HTTP/2, QUIC v1, HTTP/3, endpoint data-class
information flow, vector crypto backend selection, production constant-time
validation, hardware QoS, and real e1000e-family card support.
```

**Steps:**

- [ ] Add the status text.
- [ ] Add one design note linking `docs/implementation/2026-05-18-networking-stack-executable-plan.md`.
- [ ] Run: `git diff --check`
- [ ] Run: `rg -n "network-substrate-v0|network-application-v1|Networking Implementation Status" docs/network-stack-status.md docs/design/2026-05-18-networking-stack-design.md`
  Expected: both files contain the expected text.
- [ ] Commit:

```bash
git add docs/network-stack-status.md docs/design/2026-05-18-networking-stack-design.md
git commit -m "docs: record network stack implementation status -Codex Automated"
```

### Task 24: Substrate Acceptance Sweep

**Files:**

- No source edits unless a listed command exposes a real defect in this plan's work.

**Prerequisites:** Tasks 1-23.

**Description:** Run all substrate acceptance commands and fix only defects introduced by Tasks 0-23. This task is the integration checkpoint for `network-substrate-v0`; application-stack work continues in Tasks 25-41.

**Acceptance Criteria:**

- `go test ./compiler/nettrace -v` passes.
- `go test ./compiler/qemu -v` passes.
- `go test ./compiler/sem -run 'Network|E1000e|DmaDomain|AuthorityAudit' -v` passes.
- `go test ./tests/e2e -run NetworkSubstrate -v` passes on a QEMU/OVMF-capable worker.
- `go test ./...` passes.
- Placeholder scan from Section 0 exits 0.
- `git status --short` shows only intentional committed changes after the final commit.

**Code Examples:**

Acceptance commands:

```bash
go test ./compiler/nettrace -v
go test ./compiler/qemu -v
go test ./compiler/sem -run 'Network|E1000e|DmaDomain|AuthorityAudit' -v
go test ./tests/e2e -run NetworkSubstrate -v
go test ./...
bad_terms='TO''DO|TB''D|fill'' in|implement'' later|Add'' appropriate|similar'' to'' Task|if'' needed|or'' equivalent'
base=$(git merge-base HEAD main 2>/dev/null || git merge-base HEAD origin/main)
changed=$(git diff --name-only "$base"...HEAD)
if [ -n "$changed" ] && git diff -U0 "$base"...HEAD -- $changed | rg -n "^\+.*($bad_terms)"; then
  exit 1
fi
git status --short
```

**Steps:**

- [ ] Run all acceptance commands above.
- [ ] If a command fails because of this plan's work, fix the smallest relevant source area and rerun that command.
- [ ] Rerun `go test ./...` after any fix.
- [ ] Run: `git diff --check`
- [ ] Commit final fixes:

```bash
git add wrela/net wrela/machine/x86_64/e1000e.wrela compiler/nettrace compiler/qemu/e1000e_peer.go compiler/sem/network_graph.go compiler/sem/network_graph_test.go compiler/sem/report.go compiler/sem/report_test.go compiler/report/report.go compiler/report/report_test.go tests/e2e/network_substrate_helpers_test.go tests/e2e/network_driver_qemu_test.go tests/e2e/network_protocol_qemu_test.go tests/e2e/network_runtime_qemu_test.go tests/e2e/fixtures/network_substrate docs/network-stack-status.md docs/design/2026-05-18-networking-stack-design.md
git commit -m "test: complete network substrate acceptance sweep -Codex Automated"
```

---

## 11. Phase 7: Application Protocol Contracts And Dynamic Configuration

**Description:** Add the public Wrela source surface for application protocols, then implement DHCPv4 and DNS. These tasks can run in parallel with hardware-family work once Task 25 lands, but runtime reactor edits merge serially.

**Acceptance Criteria:**

- Protocol source-shape tests define every public type used by DHCP, DNS, TCP, TLS, HTTP/1.1, HTTP/2, QUIC, HTTP/3, endpoint flow, crypto, and QoS tasks.
- DHCP can replace static config with a bounded lease.
- DNS resolves A records through the configured DNS server.

**Code Example:**

```text
Task 25 source contracts
  unlocks Track H: Task 26 -> Task 27
  unlocks Track I: Task 28 and Task 29 in parallel
  unlocks Track K: Task 34A after UDP + crypto
```

### Task 25: Application Protocol Source Contracts

**Files:**

- Create: `wrela/net/dhcp.wrela`
- Create: `wrela/net/dns.wrela`
- Create: `wrela/net/tcp.wrela`
- Create: `wrela/net/tls.wrela`
- Create: `wrela/net/http1.wrela`
- Create: `wrela/net/http2.wrela`
- Create: `wrela/net/quic.wrela`
- Create: `wrela/net/http3.wrela`
- Create: `wrela/net/endpoint_flow.wrela`
- Create: `wrela/net/qos.wrela`
- Create: `wrela/crypto/types.wrela`
- Create: `wrela/crypto/backend.wrela`
- Modify: `compiler/sem/network_source_shape_test.go`

**Prerequisites:** Tasks 4 and 17.

**Description:** Add compileable shells for every post-substrate protocol and policy module. This task freezes names and ownership so DHCP, DNS, TCP, TLS, HTTP, QUIC, endpoint-flow, crypto, QoS, and hardware workers do not invent incompatible APIs.

**Acceptance Criteria:**

- Source-shape test requires all files above and public types in the code examples below.
- All shells compile through the existing `parseUEFIModuleSet` plus `BuildIndex` plus `Check` path.
- No shell imports `machine.x86_64.e1000e`; hardware remains below `NetworkReactor`.
- `TcpStreamAuthority`, `TlsStreamAuthority`, and `QuicStreamAuthority` contain authority fields and do not expose packet buffers.

**Code Examples:**

Endpoint and stream types:

```wrela
module wrela.net.endpoint_flow

data EndpointDataClass { value: U8 }
const ENDPOINT_PUBLIC: U8 = 0
const ENDPOINT_TELEMETRY: U8 = 1
const ENDPOINT_INTERNAL: U8 = 2
const ENDPOINT_SECRET: U8 = 3
const ENDPOINT_CREDENTIAL: U8 = 4
const ENDPOINT_KEY_MATERIAL: U8 = 5

data EndpointFlowPolicy {
    label: StringLiteral
    data_class: EndpointDataClass
    transport: U8
    remote_host_hash: U64
}
```

```wrela
module wrela.net.tcp

use { NetworkReactorAuthority } from wrela.net.packet
use { Ipv4Address } from wrela.net.types

data TcpConnection {
    state: U8
    local_port: U16
    remote_port: U16
    local_ip: Ipv4Address
    remote_ip: Ipv4Address
    send_next: U32
    send_unacked: U32
    recv_next: U32
    retries: U8
    owner: NetworkReactorAuthority
}

data TcpStreamAuthority {
    owner: NetworkReactorAuthority
    local_ip: Ipv4Address
    remote_ip: Ipv4Address
    local_port: U16
    remote_port: U16
    connection_id: U64
}
```

```wrela
module wrela.net.tls

use { SecretBytes } from wrela.crypto.types
use { EndpointDataClass } from wrela.net.endpoint_flow
use { TcpStreamAuthority } from wrela.net.tcp

data TlsClientConfig {
    alpn_hash: U64
    pinned_spki_sha256_hash: U64
    data_class: EndpointDataClass
}

data TlsStreamAuthority {
    tcp: TcpStreamAuthority
    read_key: SecretBytes
    write_key: SecretBytes
    read_iv: SecretBytes
    write_iv: SecretBytes
    cipher_suite: U16
    data_class: EndpointDataClass
}
```

```wrela
module wrela.net.quic

use { EndpointDataClass } from wrela.net.endpoint_flow
use { NetworkReactorAuthority } from wrela.net.packet

data QuicConnection {
    owner: NetworkReactorAuthority
    connection_id: U64
    next_packet_number: U64
    largest_acked: U64
    data_class: EndpointDataClass
}

data QuicStreamAuthority {
    owner: NetworkReactorAuthority
    connection_id: U64
    stream_id: U64
    data_class: EndpointDataClass
}
```

```wrela
module wrela.net.http1

data HttpMethod { value: U8 }
data HttpScheme { value: U8 }
data HttpHost { hash: U64 }
data HttpPath { hash: U64 }
data HttpResponse {
    status: U16
    body_length: U64
    body_hash: U64
}
```

```wrela
module wrela.net.http2

data Http2FrameHeader {
    length: U32
    frame_type: U8
    flags: U8
    stream_id: U32
}
```

```wrela
module wrela.net.http3

use { EndpointDataClass } from wrela.net.endpoint_flow
use { HttpMethod, HttpScheme, HttpHost, HttpPath } from wrela.net.http1

data Http3Request {
    method: HttpMethod
    scheme: HttpScheme
    authority: HttpHost
    path: HttpPath
    data_class: EndpointDataClass
}
```

```wrela
module wrela.net.dhcp

use { Ipv4Address } from wrela.net.types

data DhcpClientState {
    state: U8
    xid: U32
    offered: DhcpLease
    bound: DhcpLease
}

data DhcpLease {
    local_ip: Ipv4Address
    subnet_mask: Ipv4Address
    gateway: Ipv4Address
    dns_server: Ipv4Address
    lease_seconds: U32
    valid: Bool
}
```

```wrela
module wrela.net.dns

use { Ipv4Address } from wrela.net.types

data DnsCacheEntry {
    name_hash: U64
    address: Ipv4Address
    expires_at_tick: U64
    valid: Bool
}

data DnsCache {
    count: U64
    next_victim: U64
    entry0: DnsCacheEntry
    entry1: DnsCacheEntry
    entry2: DnsCacheEntry
    entry3: DnsCacheEntry
    entry4: DnsCacheEntry
    entry5: DnsCacheEntry
    entry6: DnsCacheEntry
    entry7: DnsCacheEntry
}
```

```wrela
module wrela.crypto.types

data SecretBytes {
    address: PhysicalAddress
    length: U64
}

data CryptoBackend {
    kind: U8
}
```

Source-shape test:

```go
func TestNetworkApplicationSourceShape(t *testing.T) {
	modules := parseUEFIModuleSet(t)
	index, ds := BuildIndex(modules)
	if len(ds) != 0 {
		t.Fatalf("build index diagnostics: %#v", ds)
	}
	requireDataFields(t, index, "wrela.net.tcp", "TcpStreamAuthority", []string{"owner", "local_ip", "remote_ip", "local_port", "remote_port", "connection_id"})
	requireDataFields(t, index, "wrela.net.tls", "TlsStreamAuthority", []string{"tcp", "read_key", "write_key", "read_iv", "write_iv", "cipher_suite", "data_class"})
	requireDataFields(t, index, "wrela.net.quic", "QuicStreamAuthority", []string{"owner", "connection_id", "stream_id", "data_class"})
	requireDataFields(t, index, "wrela.net.http1", "HttpResponse", []string{"status", "body_length", "body_hash"})
	requireDataFields(t, index, "wrela.net.http2", "Http2FrameHeader", []string{"length", "frame_type", "flags", "stream_id"})
	requireDataFields(t, index, "wrela.net.http3", "Http3Request", []string{"method", "scheme", "authority", "path", "data_class"})
	requireDataFields(t, index, "wrela.net.dhcp", "DhcpClientState", []string{"state", "xid", "offered", "bound"})
	requireDataFields(t, index, "wrela.net.dns", "DnsCache", []string{"count", "next_victim", "entry0", "entry7"})
	requireDataFields(t, index, "wrela.net.endpoint_flow", "EndpointFlowPolicy", []string{"label", "data_class", "transport", "remote_host_hash"})
	requireDataFields(t, index, "wrela.crypto.types", "SecretBytes", []string{"address", "length"})
}
```

**Steps:**

- [ ] Add failing `TestNetworkApplicationSourceShape` and module compile test.
- [ ] Run: `go test ./compiler/sem -run 'NetworkApplicationSourceShape|NetworkModulesCompile' -v`
  Expected: FAIL with missing application protocol source files.
- [ ] Create shells with exact public types and no behavior beyond parseable empty methods.
- [ ] Run: `go test ./compiler/sem -run 'NetworkApplicationSourceShape|NetworkModulesCompile' -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/net/dhcp.wrela wrela/net/dns.wrela wrela/net/tcp.wrela wrela/net/tls.wrela wrela/net/http1.wrela wrela/net/http2.wrela wrela/net/quic.wrela wrela/net/http3.wrela wrela/net/endpoint_flow.wrela wrela/net/qos.wrela wrela/crypto/types.wrela wrela/crypto/backend.wrela compiler/sem/network_source_shape_test.go
git commit -m "feat: add network application source contracts -Codex Automated"
```

### Task 26: DHCPv4 Client

**Files:**

- Modify: `wrela/net/dhcp.wrela`
- Modify: `wrela/net/config.wrela`
- Modify: `wrela/net/reactor.wrela`
- Create: `compiler/nettrace/dhcp.go`
- Create: `compiler/nettrace/dhcp_test.go`
- Modify: `tests/e2e/network_dhcp_dns_qemu_test.go`
- Modify: `compiler/sem/report.go`
- Modify: `compiler/sem/report_test.go`

**Prerequisites:** Tasks 17, 19, 20, 21B, and 25.

**Description:** Add a DHCPv4 client for one NIC. The client sends DISCOVER from `0.0.0.0:68` to broadcast `255.255.255.255:67`, accepts one OFFER and one ACK for its transaction ID, and updates `NetworkStaticConfig` only after ACK.

**Acceptance Criteria:**

- `DhcpClientState` states are `init`, `selecting`, `requesting`, `bound`, and `renewing`.
- DISCOVER includes options 53, 55 `[1,3,6,51,58,59]`, 61 client identifier, and 12 host name `wrela`.
- ACK parser accepts options 1 subnet mask, 3 router, 6 DNS server, 51 lease seconds, 54 server identifier, 58 renewal seconds, and 59 rebind seconds.
- Invalid xid, wrong server identifier, missing yiaddr, missing subnet mask, or missing lease time increments `drop_malformed_dhcp`.
- E2E serial output includes `network: dhcp bound ip=10.10.0.2 dns=10.10.0.1 lease=3600`.

**Code Examples:**

```wrela
data DhcpLease {
    local_ip: Ipv4Address
    subnet_mask: Ipv4Address
    gateway: Ipv4Address
    dns_server: Ipv4Address
    server_id: Ipv4Address
    lease_seconds: U32
    renew_at_tick: U64
    rebind_at_tick: U64
    valid: Bool
}

data DhcpClientState {
    state: U8
    xid: U32
    offered: DhcpLease
    bound: DhcpLease
}
```

```go
func TestDHCPDiscoverOfferRequestAck(t *testing.T) {
	xid := uint32(0x11223344)
	discover := nettrace.EmitDHCPDiscover(nettrace.MustMAC("52:54:00:12:34:56"), xid)
	msg, err := nettrace.ParseDHCP(discover)
	if err != nil {
		t.Fatal(err)
	}
	offer := nettrace.EmitDHCPOffer(msg, nettrace.MustIPv4("10.10.0.2"), nettrace.MustIPv4("10.10.0.1"), 3600)
	ack := nettrace.EmitDHCPAckFromOffer(offer)
	lease, err := nettrace.ParseDHCPLease(ack)
	if err != nil {
		t.Fatal(err)
	}
	if lease.LocalIP != nettrace.MustIPv4("10.10.0.2") || lease.DNSServer != nettrace.MustIPv4("10.10.0.1") {
		t.Fatalf("lease = %#v", lease)
	}
}
```

**Steps:**

- [ ] Add failing DHCP nettrace, report, and e2e tests.
- [ ] Run: `go test ./compiler/nettrace ./compiler/sem ./tests/e2e -run 'DHCP|NetworkSubstrateDHCP|Network.*Report' -v`
  Expected: FAIL with missing DHCP parser or missing serial marker.
- [ ] Implement DHCP parser/emitter, Wrela state transitions, config update, counters, and report fields.
- [ ] Run: `go test ./compiler/nettrace ./compiler/sem ./tests/e2e -run 'DHCP|NetworkSubstrateDHCP|Network.*Report' -v`
  Expected: PASS on a QEMU/OVMF-capable worker. SKIP is not accepted for task completion.
- [ ] Run: `go test ./compiler/sem -run 'NetworkApplicationSourceShape|NetworkModulesCompile' -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/net/dhcp.wrela wrela/net/config.wrela wrela/net/reactor.wrela compiler/nettrace/dhcp.go compiler/nettrace/dhcp_test.go compiler/sem/report.go compiler/sem/report_test.go tests/e2e/network_dhcp_dns_qemu_test.go
git commit -m "feat: add dhcpv4 client -Codex Automated"
```

### Task 27: DNS A-Record Resolver

**Files:**

- Modify: `wrela/net/dns.wrela`
- Modify: `wrela/net/reactor.wrela`
- Create: `compiler/nettrace/dns.go`
- Create: `compiler/nettrace/dns_test.go`
- Modify: `tests/e2e/network_dhcp_dns_qemu_test.go`
- Modify: `compiler/sem/report.go`
- Modify: `compiler/sem/report_test.go`

**Prerequisites:** Tasks 25 and 26.

**Description:** Add a bounded DNS client for A records over UDP. The resolver sends one outstanding query at a time, accepts only matching ID/name/type/class replies, caches eight positive answers, and reports NXDOMAIN separately from malformed DNS.

**Acceptance Criteria:**

- `DnsCache` capacity is exactly `8`.
- Resolver emits QTYPE `A` and QCLASS `IN`.
- Resolver accepts compressed names in answers but emits uncompressed query names.
- TTL is clamped to `300` seconds for cache retention.
- E2E peer resolves `example.wrela.test` to `10.10.0.1`; serial output includes `network: dns example.wrela.test=10.10.0.1`.

**Code Examples:**

```wrela
data DnsCacheEntry {
    name_hash: U64
    address: Ipv4Address
    expires_at_tick: U64
    valid: Bool
}

data DnsCache {
    count: U64
    next_victim: U64
    entry0: DnsCacheEntry
    entry1: DnsCacheEntry
    entry2: DnsCacheEntry
    entry3: DnsCacheEntry
    entry4: DnsCacheEntry
    entry5: DnsCacheEntry
    entry6: DnsCacheEntry
    entry7: DnsCacheEntry
}
```

```go
func TestDNSARecordQueryAndResponse(t *testing.T) {
	query := nettrace.EmitDNSAQuery(0x2345, "example.wrela.test")
	msg, err := nettrace.ParseDNS(query)
	if err != nil {
		t.Fatal(err)
	}
	reply := nettrace.EmitDNSAResponse(msg, nettrace.MustIPv4("10.10.0.1"), 60)
	got, err := nettrace.ParseDNSAResponse(reply, 0x2345, "example.wrela.test")
	if err != nil {
		t.Fatal(err)
	}
	if got != nettrace.MustIPv4("10.10.0.1") {
		t.Fatalf("A record = %v", got)
	}
}
```

**Steps:**

- [ ] Add failing DNS nettrace, report, and e2e tests.
- [ ] Run: `go test ./compiler/nettrace ./compiler/sem ./tests/e2e -run 'DNS|NetworkSubstrateDNS|Network.*Report' -v`
  Expected: FAIL with missing DNS parser or missing serial marker.
- [ ] Implement DNS query/response parsing, cache, UDP dispatch, counters, and report fields.
- [ ] Run: `go test ./compiler/nettrace ./compiler/sem ./tests/e2e -run 'DNS|NetworkSubstrateDNS|Network.*Report' -v`
  Expected: PASS on a QEMU/OVMF-capable worker. SKIP is not accepted for task completion.
- [ ] Run: `go test ./compiler/sem -run 'NetworkApplicationSourceShape|NetworkModulesCompile' -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/net/dns.wrela wrela/net/reactor.wrela compiler/nettrace/dns.go compiler/nettrace/dns_test.go compiler/sem/report.go compiler/sem/report_test.go tests/e2e/network_dhcp_dns_qemu_test.go
git commit -m "feat: add dns a-record resolver -Codex Automated"
```

## 12. Phase 8: TCP, Crypto, TLS, And HTTP Over TCP

**Description:** Add stream transport and web protocols over TCP. Task 28 TCP and Task 29 crypto foundations can run in parallel after Task 25; TLS waits for both; HTTP/1.1 and HTTP/2 can then run in parallel on separate files.

**Acceptance Criteria:**

- TCP handshake and bounded payload transfer pass host trace and QEMU tests.
- TLS 1.3 uses pinned SPKI validation and never exposes secret bytes to application code.
- HTTP/1.1 and HTTP/2 clients complete one GET each.

**Code Example:**

```text
Task 28 TCP stream authority
Task 29 crypto foundations
  -> Tasks 30A-30E TLS stream authority
  -> Tasks 31 and 32 in parallel
  -> Task 33 consolidated HTTP-over-TCP report/e2e
```

### Task 28: TCP Core

**Files:**

- Modify: `wrela/net/tcp.wrela`
- Modify: `wrela/net/ipv4.wrela`
- Modify: `wrela/net/reactor.wrela`
- Create: `compiler/nettrace/tcp.go`
- Create: `compiler/nettrace/tcp_test.go`
- Modify: `tests/e2e/network_tcp_http_qemu_test.go`

**Prerequisites:** Tasks 15, 19, 20, and 25.

**Description:** Add bounded TCP over IPv4. The first implementation supports one active-open client stream and one passive listener, fixed MSS `1460`, no window scaling, no SACK, no urgent data, no timestamps, and in-order receive only.

**Acceptance Criteria:**

- TCP states are `closed`, `listen`, `syn_sent`, `syn_received`, `established`, `fin_wait_1`, `fin_wait_2`, `close_wait`, `last_ack`, and `time_wait`.
- Initial send sequence number is deterministic in tests: `0x10000000 + connection_id`.
- Parser validates checksum over IPv4 pseudo-header.
- Retransmit timer resends SYN or oldest unacked segment after `200` ticks and stops after `5` retries.
- E2E host completes SYN/SYN-ACK/ACK and receives guest payload `wrela tcp`.

**Code Examples:**

```wrela
data TcpConnection {
    state: U8
    local_port: U16
    remote_port: U16
    local_ip: Ipv4Address
    remote_ip: Ipv4Address
    send_next: U32
    send_unacked: U32
    recv_next: U32
    retries: U8
    owner: NetworkReactorAuthority
}
```

```go
func TestTCPHandshakeAndPayload(t *testing.T) {
	trace := nettrace.NewTCPTrace(nettrace.MustIPv4("10.10.0.2"), nettrace.MustIPv4("10.10.0.1"))
	syn := trace.GuestSYN(49152, 80, 0x10000001)
	synAck := trace.HostSYNACK(syn, 0x20000001)
	ack := trace.GuestACK(synAck)
	data := trace.GuestData(ack, []byte("wrela tcp"))
	if err := trace.Verify(data); err != nil {
		t.Fatal(err)
	}
}
```

**Steps:**

- [ ] Add failing TCP nettrace and e2e tests.
- [ ] Run: `go test ./compiler/nettrace ./tests/e2e -run 'TCP|NetworkSubstrateTCP' -v`
  Expected: FAIL with missing TCP trace support or missing handshake marker.
- [ ] Implement TCP parsing, checksum, state machine, retransmit timer, and reactor dispatch.
- [ ] Run: `go test ./compiler/nettrace ./tests/e2e -run 'TCP|NetworkSubstrateTCP' -v`
  Expected: PASS on a QEMU/OVMF-capable worker. SKIP is not accepted for task completion.
- [ ] Run: `go test ./compiler/sem -run 'NetworkApplicationSourceShape|NetworkModulesCompile' -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/net/tcp.wrela wrela/net/ipv4.wrela wrela/net/reactor.wrela compiler/nettrace/tcp.go compiler/nettrace/tcp_test.go tests/e2e/network_tcp_http_qemu_test.go
git commit -m "feat: add bounded tcp core -Codex Automated"
```

### Task 29: Crypto Foundations

**Files:**

- Create: `wrela/crypto/sha256.wrela`
- Create: `wrela/crypto/hkdf.wrela`
- Create: `wrela/crypto/aes_gcm.wrela`
- Create: `wrela/crypto/x25519.wrela`
- Modify: `wrela/crypto/types.wrela`
- Create: `compiler/nettrace/tls.go`
- Create: `compiler/nettrace/tls_test.go`

**Prerequisites:** Task 25.

**Description:** Add scalar SHA-256, HKDF-SHA256, AES-128-GCM, and X25519 primitives with deterministic test vectors. This task is intentionally independent from TCP so crypto workers can proceed while TCP lands.

**Acceptance Criteria:**

- SHA-256 passes the empty string vector `e3b0c44298fc1c149afbf4c8996fb924...`.
- HKDF-SHA256 passes RFC 5869 test case 1.
- AES-128-GCM passes NIST zero-key/zero-IV empty plaintext tag vector `58e2fccefa7e3061367f1d57a4e7455a`.
- X25519 basepoint multiplication passes RFC 7748 scalar test.
- `SecretBytes` has no method that returns a raw `MutableBytes`.

**Code Examples:**

```go
func TestAES128GCMEmptyPlaintextVector(t *testing.T) {
	key := make([]byte, 16)
	nonce := make([]byte, 12)
	tag := nettrace.AES128GCMTagForTest(key, nonce, nil, nil)
	if got := hex.EncodeToString(tag); got != "58e2fccefa7e3061367f1d57a4e7455a" {
		t.Fatalf("tag = %s", got)
	}
}
```

**Steps:**

- [ ] Add failing crypto vector tests.
- [ ] Run: `go test ./compiler/nettrace -run 'SHA256|HKDF|AES128GCM|X25519' -v`
  Expected: FAIL with missing crypto vector helpers.
- [ ] Implement scalar crypto primitives and Wrela secret-byte surfaces.
- [ ] Run: `go test ./compiler/nettrace -run 'SHA256|HKDF|AES128GCM|X25519' -v`
  Expected: PASS.
- [ ] Run: `go test ./compiler/sem -run 'NetworkApplicationSourceShape|NetworkModulesCompile' -v`
  Expected: PASS; this proves the Wrela crypto files committed by this task parse and type-check.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/crypto/sha256.wrela wrela/crypto/hkdf.wrela wrela/crypto/aes_gcm.wrela wrela/crypto/x25519.wrela wrela/crypto/types.wrela compiler/nettrace/tls.go compiler/nettrace/tls_test.go
git commit -m "feat: add scalar network crypto foundations -Codex Automated"
```

### Task 30A: TLS 1.3 ClientHello

**Files:**

- Modify: `wrela/net/tls.wrela`
- Modify: `compiler/nettrace/tls.go`
- Modify: `compiler/nettrace/tls_test.go`

**Prerequisites:** Tasks 28 and 29.

**Description:** Add the TLS 1.3 ClientHello writer over `TcpStreamAuthority`. This task emits only the first outbound handshake bytes and updates the transcript hash input; it does not derive traffic keys or decrypt server records.

**Acceptance Criteria:**

- ClientHello includes compatibility version `0x0303`, supported_versions `0x0304`, key_share X25519, signature_algorithms, and caller ALPN.
- ClientHello cipher suite list contains only `0x1301`.
- Unsupported hybrid PQ groups are not emitted.
- `TestTLS13ClientHelloShape` passes.
- `NetworkModulesCompile` passes after editing `wrela/net/tls.wrela`.

**Code Examples:**

```wrela
fn write_client_hello(self, out: TlsHandshakeWriter, cfg: TlsClientConfig) {
    out.write_u16(value = 0x0303)
    out.write_cipher_suite(value = 0x1301)
    out.write_supported_version(value = 0x0304)
    out.write_x25519_key_share(public_key = cfg.client_x25519_public)
    out.write_alpn_hash(hash = cfg.alpn_hash)
}
```

```go
func TestTLS13ClientHelloShape(t *testing.T) {
	hello := nettrace.EmitTLS13ClientHello(nettrace.TLSClientHelloOptions{
		ALPN: "http/1.1",
	})
	parsed, err := nettrace.ParseTLS13ClientHello(hello)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.LegacyVersion != 0x0303 || parsed.SupportedVersion != 0x0304 {
		t.Fatalf("versions = %#v", parsed)
	}
	if !slices.Equal(parsed.CipherSuites, []uint16{0x1301}) {
		t.Fatalf("cipher suites = %#v", parsed.CipherSuites)
	}
}
```

**Steps:**

- [ ] Add `TestTLS13ClientHelloShape`.
- [ ] Run: `go test ./compiler/nettrace -run TLS13ClientHelloShape -v`
  Expected: FAIL with missing ClientHello writer.
- [ ] Implement ClientHello writer and transcript hash input.
- [ ] Run: `go test ./compiler/nettrace -run TLS13ClientHelloShape -v`
  Expected: PASS.
- [ ] Run: `go test ./compiler/sem -run 'NetworkApplicationSourceShape|NetworkModulesCompile' -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/net/tls.wrela compiler/nettrace/tls.go compiler/nettrace/tls_test.go
git commit -m "feat: write tls13 client hello -Codex Automated"
```

### Task 30B: TLS 1.3 Traffic Secret Derivation

**Files:**

- Modify: `wrela/net/tls.wrela`
- Modify: `wrela/crypto/hkdf.wrela`
- Modify: `compiler/nettrace/tls.go`
- Modify: `compiler/nettrace/tls_test.go`

**Prerequisites:** Task 30A.

**Description:** Add TLS 1.3 HKDF traffic-secret derivation. This task computes handshake and application secrets from transcript hashes; it does not validate certificates or decrypt records.

**Acceptance Criteria:**

- HKDF-Expand-Label implements labels `derived`, `c hs traffic`, `s hs traffic`, `c ap traffic`, and `s ap traffic`.
- `TestTLS13HKDFTrafficSecrets` passes using Task 29 SHA-256 and HKDF helpers.
- `NetworkModulesCompile` passes after editing Wrela crypto and TLS files.

**Code Examples:**

```wrela
fn expand_label(self, secret: SecretBytes, label: StringLiteral, context_hash: Bytes, length: U16) -> SecretBytes {
    let full_label = TlsLabel(prefix = "tls13 ", label = label)
    return self.hkdf_expand(secret = secret, info = full_label.encode(context_hash = context_hash, length = length), length = length)
}
```

```go
func TestTLS13HKDFTrafficSecrets(t *testing.T) {
	transcript := nettrace.SHA256([]byte("clienthello/serverhello"))
	secrets := nettrace.DeriveTLS13TrafficSecrets(nettrace.TLS13SecretInputs{
		SharedSecret: bytes.Repeat([]byte{0x11}, 32),
		Transcript:  transcript,
	})
	if hex.EncodeToString(secrets.ClientHandshake[:4]) != "b4f2c913" {
		t.Fatalf("client handshake secret prefix = %x", secrets.ClientHandshake[:4])
	}
}
```

**Steps:**

- [ ] Add `TestTLS13HKDFTrafficSecrets`.
- [ ] Run: `go test ./compiler/nettrace -run TLS13HKDFTrafficSecrets -v`
  Expected: FAIL with missing traffic-secret derivation.
- [ ] Implement TLS 1.3 key schedule labels exactly as listed above.
- [ ] Run: `go test ./compiler/nettrace -run TLS13HKDFTrafficSecrets -v`
  Expected: PASS.
- [ ] Run: `go test ./compiler/sem -run 'NetworkApplicationSourceShape|NetworkModulesCompile' -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/net/tls.wrela wrela/crypto/hkdf.wrela compiler/nettrace/tls.go compiler/nettrace/tls_test.go
git commit -m "feat: derive tls13 traffic secrets -Codex Automated"
```

### Task 30C: TLS 1.3 Pinned SPKI Validation

**Files:**

- Modify: `wrela/net/tls.wrela`
- Modify: `compiler/nettrace/tls.go`
- Modify: `compiler/nettrace/tls_test.go`

**Prerequisites:** Task 30B.

**Description:** Add certificate validation for the single supported TLS mode. Certificates are accepted only when the SHA-256 hash of the server SPKI matches the endpoint policy; unsupported cipher suites and PQ groups return `TlsUnsupportedFeature`.

**Acceptance Criteria:**

- Matching pinned SPKI hash is accepted.
- Mismatched pinned SPKI hash returns `TlsBadPinnedSPKI`.
- Any cipher suite other than `TLS_AES_128_GCM_SHA256` returns `TlsUnsupportedFeature`.
- Hybrid PQ groups, ML-KEM, and ML-DSA are rejected with `TlsUnsupportedFeature`.
- `NetworkModulesCompile` passes.

**Code Examples:**

```go
func TestTLS13PinnedSPKIRejectsMismatch(t *testing.T) {
	server := nettrace.NewTLSServerFixture(nettrace.TLSFixtureOptions{CipherSuite: nettrace.TLS_AES_128_GCM_SHA256})
	client := nettrace.NewTLSClientFixture([32]byte{0xaa})
	err := nettrace.ValidateTLS13ServerCertificate(client, server.CertificateMessage())
	if !errors.Is(err, nettrace.ErrTLSBadPinnedSPKI) {
		t.Fatalf("err = %v", err)
	}
}

func TestTLS13UnsupportedCipherRejected(t *testing.T) {
	err := nettrace.ValidateTLS13CipherSuite(0x1302)
	if !errors.Is(err, nettrace.ErrTLSUnsupportedFeature) {
		t.Fatalf("err = %v", err)
	}
}
```

**Steps:**

- [ ] Add `TestTLS13PinnedSPKIRejectsMismatch` and `TestTLS13UnsupportedCipherRejected`.
- [ ] Run: `go test ./compiler/nettrace -run 'TLS13PinnedSPKI|TLS13UnsupportedCipher' -v`
  Expected: FAIL with missing certificate validation.
- [ ] Implement pinned SHA-256 SPKI validation and unsupported feature rejection.
- [ ] Run: `go test ./compiler/nettrace -run 'TLS13PinnedSPKI|TLS13UnsupportedCipher' -v`
  Expected: PASS.
- [ ] Run: `go test ./compiler/sem -run 'NetworkApplicationSourceShape|NetworkModulesCompile' -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/net/tls.wrela compiler/nettrace/tls.go compiler/nettrace/tls_test.go
git commit -m "feat: validate tls13 pinned spki -Codex Automated"
```

### Task 30D: TLS 1.3 Record Protection

**Files:**

- Modify: `wrela/net/tls.wrela`
- Modify: `wrela/crypto/aes_gcm.wrela`
- Modify: `compiler/nettrace/tls.go`
- Modify: `compiler/nettrace/tls_test.go`

**Prerequisites:** Task 30C.

**Description:** Add Finished verification and AES-128-GCM record protection. This task is the first complete TLS data path over the host trace harness.

**Acceptance Criteria:**

- Finished verify-data uses the handshake traffic secret and transcript hash.
- Application records use per-direction key and IV from Task 30B.
- Host trace completes TLS handshake and reads encrypted application data `wrela tls`.
- `NetworkModulesCompile` passes.

**Code Examples:**

```go
func TestTLS13FinishedAndApplicationData(t *testing.T) {
	server := nettrace.NewTLSServerFixture(nettrace.TLSFixtureOptions{CipherSuite: nettrace.TLS_AES_128_GCM_SHA256, ALPN: "http/1.1"})
	client := nettrace.NewTLSClientFixture(server.PinnedSPKIHash())
	if err := nettrace.RunTLS13Handshake(client, server); err != nil {
		t.Fatal(err)
	}
	if got := client.ReadApplicationData(); string(got) != "wrela tls" {
		t.Fatalf("application data = %q", got)
	}
}
```

**Steps:**

- [ ] Add `TestTLS13FinishedAndApplicationData`.
- [ ] Run: `go test ./compiler/nettrace -run TLS13FinishedAndApplicationData -v`
  Expected: FAIL with missing record protection.
- [ ] Implement AES-128-GCM record protection, Finished verification, and encrypted application read/write.
- [ ] Run: `go test ./compiler/nettrace -run TLS13FinishedAndApplicationData -v`
  Expected: PASS.
- [ ] Run: `go test ./compiler/sem -run 'NetworkApplicationSourceShape|NetworkModulesCompile' -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/net/tls.wrela wrela/crypto/aes_gcm.wrela compiler/nettrace/tls.go compiler/nettrace/tls_test.go
git commit -m "feat: protect tls13 records -Codex Automated"
```

### Task 30E: TLS 1.3 Report Fields

**Files:**

- Modify: `wrela/net/tls.wrela`
- Modify: `compiler/nettrace/tls.go`
- Modify: `compiler/nettrace/tls_test.go`
- Modify: `compiler/sem/report.go`
- Modify: `compiler/sem/report_test.go`

**Prerequisites:** Task 30D.

**Description:** Populate the TLS application report fields introduced by Task 1. This task does not add new handshake behavior.

**Acceptance Criteria:**

- Report sets `network.application.tls.enabled=true`.
- Report sets `network.application.tls.cipher_suite="TLS_AES_128_GCM_SHA256"`.
- Report sets `network.application.tls.validation_mode="pinned_spki"`.
- Report records the selected ALPN.
- `NetworkModulesCompile` passes.

**Code Examples:**

```go
func TestNetworkReportIncludesTLS13(t *testing.T) {
	r := BuildImageReport(networkApplicationFixtureForTest(t))
	if !r.Network.Application.TLS.Enabled {
		t.Fatalf("TLS report disabled: %#v", r.Network.Application.TLS)
	}
	if r.Network.Application.TLS.CipherSuite != "TLS_AES_128_GCM_SHA256" {
		t.Fatalf("cipher suite = %q", r.Network.Application.TLS.CipherSuite)
	}
}
```

**Steps:**

- [ ] Add `TestNetworkReportIncludesTLS13`.
- [ ] Run: `go test ./compiler/sem -run 'TLS13|Network.*Report' -v`
  Expected: FAIL with missing TLS report fields.
- [ ] Populate TLS cipher suite, ALPN, validation mode, and pinned-SPKI report fields.
- [ ] Run: `go test ./compiler/nettrace ./compiler/sem -run 'TLS13|Network.*Report' -v`
  Expected: PASS.
- [ ] Run: `go test ./compiler/sem -run 'NetworkApplicationSourceShape|NetworkModulesCompile' -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/net/tls.wrela compiler/nettrace/tls.go compiler/nettrace/tls_test.go compiler/sem/report.go compiler/sem/report_test.go
git commit -m "feat: report tls13 client policy -Codex Automated"
```

### Task 31: HTTP/1.1 Client

**Files:**

- Modify: `wrela/net/http1.wrela`
- Create: `compiler/nettrace/http1.go`
- Create: `compiler/nettrace/http1_test.go`
- Modify: `tests/e2e/network_tcp_http_qemu_test.go`

**Prerequisites:** Task 30E.

**Description:** Add HTTP/1.1 GET over `TlsStreamAuthority`. The client rejects header lines longer than `1024` bytes, caps body reads at caller-provided capacity, and closes the TCP stream after response completion.

**Acceptance Criteria:**

- Request line is `GET <path> HTTP/1.1`.
- Required headers are `Host`, `User-Agent: wrela/1`, and `Connection: close`.
- Parser accepts status `200` and `Content-Length`.
- Chunked transfer is rejected with `HttpUnsupportedTransferEncoding`.
- E2E guest serial output includes `network: http1 status=200 body=wrela-http1`.

**Code Examples:**

```wrela
fn get(self, stream: TlsStreamAuthority, host: HttpHost, path: HttpPath, max_body: U64) -> Result<HttpResponse, HttpError> {
    stream.write_ascii(value = "GET ")
    stream.write_ascii(value = path.value)
    stream.write_ascii(value = " HTTP/1.1\r\nHost: ")
    stream.write_ascii(value = host.value)
    stream.write_ascii(value = "\r\nUser-Agent: wrela/1\r\nConnection: close\r\n\r\n")
    return self.read_response(stream = stream, max_body = max_body)
}
```

```go
func TestHTTP1GetParsesContentLength(t *testing.T) {
	raw := []byte("HTTP/1.1 200 OK\r\nContent-Length: 11\r\n\r\nwrela-http1")
	resp, err := nettrace.ParseHTTP1Response(raw, 64)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || string(resp.Body) != "wrela-http1" {
		t.Fatalf("response = %#v", resp)
	}
}
```

**Steps:**

- [ ] Add failing HTTP/1.1 parser and e2e tests.
- [ ] Run: `go test ./compiler/nettrace ./tests/e2e -run 'HTTP1|NetworkSubstrateHTTP1' -v`
  Expected: FAIL with missing HTTP/1.1 parser or missing serial marker.
- [ ] Implement HTTP/1.1 request writer and response parser.
- [ ] Run: `go test ./compiler/nettrace ./tests/e2e -run 'HTTP1|NetworkSubstrateHTTP1' -v`
  Expected: PASS on a QEMU/OVMF-capable worker. SKIP is not accepted for task completion.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/net/http1.wrela compiler/nettrace/http1.go compiler/nettrace/http1_test.go tests/e2e/network_tcp_http_qemu_test.go
git commit -m "feat: add http1 client -Codex Automated"
```

### Task 32: HTTP/2 Client

**Files:**

- Modify: `wrela/net/http2.wrela`
- Create: `compiler/nettrace/http2.go`
- Create: `compiler/nettrace/http2_test.go`
- Modify: `wrela/net/tls.wrela`
- Modify: `tests/e2e/network_tcp_http_qemu_test.go`

**Prerequisites:** Task 30E.

**Description:** Add HTTP/2 client support over TLS with ALPN `h2`. The first implementation uses stream ID `1`, static-table HPACK only, no dynamic table, no server push, and a maximum frame size of `16384`.

**Acceptance Criteria:**

- Client sends connection preface `PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n`.
- Client sends SETTINGS with initial window size `65535` and ACKs peer SETTINGS.
- Client sends HEADERS for `:method`, `:scheme`, `:authority`, and `:path`.
- Parser accepts HEADERS and DATA on stream `1`.
- E2E guest serial output includes `network: http2 status=200 body=wrela-http2`.

**Code Examples:**

```wrela
data Http2FrameHeader {
    length: U32
    frame_type: U8
    flags: U8
    stream_id: U32
}
```

```go
func TestHTTP2ClientPrefaceAndSettings(t *testing.T) {
	var out bytes.Buffer
	nettrace.WriteHTTP2ClientPreface(&out)
	nettrace.WriteHTTP2Settings(&out)
	if !bytes.HasPrefix(out.Bytes(), []byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n")) {
		t.Fatalf("bad preface: %x", out.Bytes())
	}
	frame, err := nettrace.ParseHTTP2Frame(out.Bytes()[24:])
	if err != nil {
		t.Fatal(err)
	}
	if frame.Type != nettrace.HTTP2FrameSettings {
		t.Fatalf("frame = %#v", frame)
	}
}
```

**Steps:**

- [ ] Add failing HTTP/2 frame and e2e tests.
- [ ] Run: `go test ./compiler/nettrace ./tests/e2e -run 'HTTP2|NetworkSubstrateHTTP2' -v`
  Expected: FAIL with missing HTTP/2 frame support.
- [ ] Implement HTTP/2 frame parser/writer, static HPACK encoder, and client GET flow.
- [ ] Run: `go test ./compiler/nettrace ./tests/e2e -run 'HTTP2|NetworkSubstrateHTTP2' -v`
  Expected: PASS on a QEMU/OVMF-capable worker. SKIP is not accepted for task completion.
- [ ] Run: `go test ./compiler/sem -run 'NetworkApplicationSourceShape|NetworkModulesCompile' -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/net/http2.wrela wrela/net/tls.wrela compiler/nettrace/http2.go compiler/nettrace/http2_test.go tests/e2e/network_tcp_http_qemu_test.go
git commit -m "feat: add http2 client -Codex Automated"
```

### Task 33: HTTP Over TCP Report And E2E Consolidation

**Files:**

- Modify: `compiler/sem/report.go`
- Modify: `compiler/sem/report_test.go`
- Modify: `tests/e2e/network_tcp_http_qemu_test.go`

**Prerequisites:** Tasks 31 and 32.

**Description:** Consolidate TCP, TLS, HTTP/1.1, and HTTP/2 report fields and a single QEMU scenario. This task is the serial merge point for the HTTP-over-TCP track.

**Acceptance Criteria:**

- Report includes TCP connection capacity, retransmit count, TLS cipher suite, TLS validation mode, and HTTP protocols `http/1.1` and `h2`.
- `TestNetworkSubstrateHTTPOverTCPQEMU` performs one HTTP/1.1 GET and one HTTP/2 GET in one boot.
- Serial output includes both `network: http1 status=200` and `network: http2 status=200`.

**Code Examples:**

```go
func TestNetworkReportIncludesHTTPOverTCP(t *testing.T) {
	r := BuildImageReport(networkApplicationFixtureForTest(t))
	if !slices.Contains(r.Network.Protocols, "tcp") || !slices.Contains(r.Network.Protocols, "tls13") || !slices.Contains(r.Network.Protocols, "http2") {
		t.Fatalf("protocols = %#v", r.Network.Protocols)
	}
}
```

**Steps:**

- [ ] Add failing HTTP-over-TCP report and consolidated e2e tests.
- [ ] Run: `go test ./compiler/sem ./tests/e2e -run 'HTTPOverTCP|NetworkSubstrateHTTPOverTCP' -v`
  Expected: FAIL with missing report fields or serial markers.
- [ ] Populate report fields and consolidate e2e helper sequencing.
- [ ] Run: `go test ./compiler/sem ./tests/e2e -run 'HTTPOverTCP|NetworkSubstrateHTTPOverTCP' -v`
  Expected: PASS on a QEMU/OVMF-capable worker. SKIP is not accepted for task completion.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add compiler/sem/report.go compiler/sem/report_test.go tests/e2e/network_tcp_http_qemu_test.go
git commit -m "test: consolidate http over tcp reporting -Codex Automated"
```

## 13. Phase 9: QUIC And HTTP/3

**Description:** Add QUIC v1 over UDP and HTTP/3 over QUIC streams. This track can run after UDP and crypto foundations without waiting for HTTP/1.1 or HTTP/2, then merges with endpoint-flow policy later.

**Acceptance Criteria:**

- QUIC v1 completes Initial, Handshake, and 1-RTT key transitions with deterministic host fixtures.
- HTTP/3 performs one GET on a bidirectional QUIC stream.
- QUIC and TCP reports remain separate.

**Code Example:**

```text
UDP port 443 -> QUIC packet protection -> QuicStreamAuthority -> HTTP/3
```

### Task 34A: QUIC Initial Keys And Header

**Files:**

- Modify: `wrela/net/quic.wrela`
- Create: `compiler/nettrace/quic.go`
- Create: `compiler/nettrace/quic_test.go`
- Modify: `wrela/crypto/hkdf.wrela`

**Prerequisites:** Tasks 17 and 29.

**Description:** Add QUIC v1 Initial key derivation and Initial long-header parse/write support. This task does not add ACKs, CRYPTO frames, stream frames, or e2e behavior.

**Acceptance Criteria:**

- Version is exactly `0x00000001`.
- Initial salt is `0x38762cf7f55934b34d179ae6a4c80cadccbb7f0a`.
- Client destination connection ID length is `8`.
- Initial header writer and parser round-trip packet type, version, DCID, SCID, token length, packet number length, and payload length.
- `NetworkModulesCompile` passes after editing `wrela/net/quic.wrela`.

**Code Examples:**

```wrela
data QuicPacketNumberSpace {
    next_packet_number: U64
    largest_acked: U64
    crypto_offset: U64
}
```

```go
func TestQUICInitialUsesRFC9001Salt(t *testing.T) {
	dcid := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	keys := nettrace.DeriveQUICInitialKeys(dcid)
	if got := hex.EncodeToString(keys.ClientInitialSecret[:4]); got != "c00cf151" {
		t.Fatalf("client initial secret prefix = %s", got)
	}
}
```

**Steps:**

- [ ] Add `TestQUICInitialUsesRFC9001Salt` and `TestQUICInitialPacketHeaderShape`.
- [ ] Run: `go test ./compiler/nettrace -run 'QUICInitial' -v`
  Expected: FAIL with missing Initial key derivation.
- [ ] Implement Initial salt, DCID length `8`, Initial header writer, and Initial header parser.
- [ ] Run: `go test ./compiler/nettrace -run 'QUICInitial' -v`
  Expected: PASS.
- [ ] Run: `go test ./compiler/sem -run 'NetworkApplicationSourceShape|NetworkModulesCompile' -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/net/quic.wrela wrela/crypto/hkdf.wrela compiler/nettrace/quic.go compiler/nettrace/quic_test.go
git commit -m "feat: derive quic initial keys -Codex Automated"
```

### Task 34B: QUIC Packet Number Spaces And ACK

**Files:**

- Modify: `wrela/net/quic.wrela`
- Modify: `compiler/nettrace/quic.go`
- Modify: `compiler/nettrace/quic_test.go`

**Prerequisites:** Task 34A.

**Description:** Add packet number tracking and ACK frame encode/decode for the Initial, Handshake, and 1-RTT packet number spaces.

**Acceptance Criteria:**

- `QuicPacketNumberSpace` tracks `next_packet_number`, `largest_acked`, and `crypto_offset`.
- ACK frame parser accepts one contiguous ACK range.
- ACK frame writer emits largest acknowledged, ACK delay `0`, first range, and range count `0`.
- `NetworkModulesCompile` passes.

**Code Examples:**

```go
func TestQUICPacketNumberSpacesAndACK(t *testing.T) {
	conn := nettrace.NewQUICConnectionForTest()
	pn := conn.Initial.NextPacketNumber()
	ack := nettrace.EmitQUICACK(pn)
	got, err := nettrace.ParseQUICACK(ack)
	if err != nil {
		t.Fatal(err)
	}
	if got.LargestAcknowledged != pn {
		t.Fatalf("ack = %#v, want pn %d", got, pn)
	}
}
```

**Steps:**

- [ ] Add `TestQUICPacketNumberSpacesAndACK`.
- [ ] Run: `go test ./compiler/nettrace -run 'QUICPacketNumber|QUICACK' -v`
  Expected: FAIL with missing packet number or ACK handling.
- [ ] Implement packet number spaces for Initial, Handshake, and 1-RTT plus ACK encode/decode.
- [ ] Run: `go test ./compiler/nettrace -run 'QUICPacketNumber|QUICACK' -v`
  Expected: PASS.
- [ ] Run: `go test ./compiler/sem -run 'NetworkApplicationSourceShape|NetworkModulesCompile' -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/net/quic.wrela compiler/nettrace/quic.go compiler/nettrace/quic_test.go
git commit -m "feat: track quic packet numbers and ack frames -Codex Automated"
```

### Task 34C: QUIC CRYPTO Frames

**Files:**

- Modify: `wrela/net/quic.wrela`
- Modify: `compiler/nettrace/quic.go`
- Modify: `compiler/nettrace/quic_test.go`

**Prerequisites:** Task 34B.

**Description:** Add CRYPTO frame encode/decode and handshake offset tracking. This bridges QUIC packet spaces to TLS handshake bytes without adding 1-RTT protection.

**Acceptance Criteria:**

- CRYPTO frame writer emits offset, length, and payload.
- Parser rejects CRYPTO frames whose length extends past packet payload.
- Handshake crypto offset advances only after accepted CRYPTO bytes.
- `NetworkModulesCompile` passes.

**Code Examples:**

```go
func TestQUICCryptoFrameHandshakeProgress(t *testing.T) {
	frame := nettrace.EmitQUICCryptoFrame(0, []byte("clienthello"))
	got, err := nettrace.ParseQUICCryptoFrame(frame)
	if err != nil {
		t.Fatal(err)
	}
	if got.Offset != 0 || string(got.Payload) != "clienthello" {
		t.Fatalf("crypto frame = %#v", got)
	}
}
```

**Steps:**

- [ ] Add `TestQUICCryptoFrameHandshakeProgress`.
- [ ] Run: `go test ./compiler/nettrace -run QUICCryptoFrameHandshakeProgress -v`
  Expected: FAIL with missing CRYPTO frame handling.
- [ ] Implement CRYPTO frame encode/decode and handshake offset tracking.
- [ ] Run: `go test ./compiler/nettrace -run QUICCryptoFrameHandshakeProgress -v`
  Expected: PASS.
- [ ] Run: `go test ./compiler/sem -run 'NetworkApplicationSourceShape|NetworkModulesCompile' -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/net/quic.wrela compiler/nettrace/quic.go compiler/nettrace/quic_test.go
git commit -m "feat: handle quic crypto frames -Codex Automated"
```

### Task 34D: QUIC Handshake And 1-RTT Protection

**Files:**

- Modify: `wrela/net/quic.wrela`
- Modify: `wrela/crypto/hkdf.wrela`
- Modify: `wrela/crypto/aes_gcm.wrela`
- Modify: `compiler/nettrace/quic.go`
- Modify: `compiler/nettrace/quic_test.go`

**Prerequisites:** Task 34C.

**Description:** Add Handshake and 1-RTT packet protection using HKDF-SHA256 and AES-128-GCM from Task 29.

**Acceptance Criteria:**

- Handshake keys derive from TLS handshake secrets.
- 1-RTT keys derive from TLS application secrets.
- Packet protection authenticates header bytes as associated data.
- `TestQUICOneRTTKeysProtectStreamFrame` passes.
- `NetworkModulesCompile` passes.

**Code Examples:**

```go
func TestQUICOneRTTKeysProtectStreamFrame(t *testing.T) {
	keys := nettrace.QUICOneRTTKeysForTest()
	protected := nettrace.ProtectQUICPacket(keys, nettrace.EmitQUICStreamFrame(0, []byte("wrela quic")))
	clear, err := nettrace.UnprotectQUICPacket(keys, protected)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(clear.Payload, []byte("wrela quic")) {
		t.Fatalf("clear packet = %x", clear.Payload)
	}
}
```

**Steps:**

- [ ] Add `TestQUICOneRTTKeysProtectStreamFrame`.
- [ ] Run: `go test ./compiler/nettrace -run QUICOneRTTKeysProtectStreamFrame -v`
  Expected: FAIL with missing 1-RTT protection.
- [ ] Implement Handshake and 1-RTT AES-GCM packet protection using Task 29 primitives.
- [ ] Run: `go test ./compiler/nettrace -run QUICOneRTTKeysProtectStreamFrame -v`
  Expected: PASS.
- [ ] Run: `go test ./compiler/sem -run 'NetworkApplicationSourceShape|NetworkModulesCompile' -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/net/quic.wrela wrela/crypto/hkdf.wrela wrela/crypto/aes_gcm.wrela compiler/nettrace/quic.go compiler/nettrace/quic_test.go
git commit -m "feat: protect quic handshake and one-rtt packets -Codex Automated"
```

### Task 34E: QUIC Stream And Loss Timer

**Files:**

- Modify: `wrela/net/quic.wrela`
- Modify: `compiler/nettrace/quic.go`
- Modify: `compiler/nettrace/quic_test.go`

**Prerequisites:** Task 34D.

**Description:** Add one bidirectional stream, CONNECTION_CLOSE, and the bounded loss timer. This task remains host-trace only.

**Acceptance Criteria:**

- One bidirectional stream ID `0` can send and receive ordered bytes.
- Loss timer retransmits the oldest unacked packet.
- CONNECTION_CLOSE records an application reason and closes the authority.
- `NetworkModulesCompile` passes.

**Code Examples:**

```go
func TestQUICStreamSendReceiveAndLossTimer(t *testing.T) {
	conn := nettrace.NewQUICConnectionForTest()
	conn.Stream(0).Send([]byte("wrela quic"))
	conn.AdvanceTicks(200)
	if got := conn.Retransmissions(); got != 1 {
		t.Fatalf("retransmissions = %d", got)
	}
}
```

**Steps:**

- [ ] Add `TestQUICStreamSendReceiveAndLossTimer`.
- [ ] Run: `go test ./compiler/nettrace -run 'QUICStream|QUICLossTimer' -v`
  Expected: FAIL with missing stream or retransmit logic.
- [ ] Implement one bidirectional STREAM, CONNECTION_CLOSE, and loss timer retransmission for the oldest unacked packet.
- [ ] Run: `go test ./compiler/nettrace -run 'QUICStream|QUICLossTimer' -v`
  Expected: PASS.
- [ ] Run: `go test ./compiler/sem -run 'NetworkApplicationSourceShape|NetworkModulesCompile' -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/net/quic.wrela compiler/nettrace/quic.go compiler/nettrace/quic_test.go
git commit -m "feat: add quic stream send receive -Codex Automated"
```

### Task 34F: QUIC Reactor And QEMU Integration

**Files:**

- Modify: `wrela/net/quic.wrela`
- Modify: `compiler/nettrace/quic.go`
- Modify: `compiler/nettrace/quic_test.go`
- Modify: `tests/e2e/network_quic_http3_qemu_test.go`

**Prerequisites:** Task 34E.

**Description:** Wire QUIC into the UDP reactor path and add the QEMU proof that the guest completes one QUIC stream exchange.

**Acceptance Criteria:**

- `TestNetworkSubstrateQUIC` sends the host handshake sequence and receives stream payload `wrela quic`.
- Serial output contains `network: quic stream`.
- Report protocol list includes `quic`.
- Runtime proof passes on a QEMU/OVMF-capable worker.

**Code Examples:**

```go
func TestNetworkSubstrateQUIC(t *testing.T) {
	deps := requireQEMUDeps(t, false)
	run := startNetworkQUICFixture(t, deps)
	expectQUICStreamPayload(t, run.Peer, run.Context(), []byte("wrela quic"))
	run.WaitForSerial(t, "network: quic stream")
}
```

**Steps:**

- [ ] Add QEMU e2e test `TestNetworkSubstrateQUIC`.
- [ ] Run: `go test ./tests/e2e -run NetworkSubstrateQUIC -v`
  Expected: FAIL with missing `network: quic stream`.
- [ ] Wire QUIC reactor dispatch and e2e sequence.
- [ ] Run: `go test ./compiler/nettrace ./tests/e2e -run 'QUIC|NetworkSubstrateQUIC' -v`
  Expected: PASS on a QEMU/OVMF-capable worker. SKIP is not accepted for task completion.
- [ ] Run: `go test ./compiler/sem -run 'NetworkApplicationSourceShape|NetworkModulesCompile' -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/net/quic.wrela compiler/nettrace/quic.go compiler/nettrace/quic_test.go tests/e2e/network_quic_http3_qemu_test.go
git commit -m "feat: integrate quic v1 client -Codex Automated"
```

### Task 35: HTTP/3 Client

**Files:**

- Modify: `wrela/net/http3.wrela`
- Create: `compiler/nettrace/http3.go`
- Create: `compiler/nettrace/http3_test.go`
- Modify: `wrela/net/quic.wrela`
- Modify: `tests/e2e/network_quic_http3_qemu_test.go`
- Modify: `compiler/sem/report.go`
- Modify: `compiler/sem/report_test.go`

**Prerequisites:** Task 34F.

**Description:** Add HTTP/3 client support over one QUIC bidirectional stream. The first implementation uses QPACK static table only, sends SETTINGS on the control stream, disables push, and supports one GET response body.

**Acceptance Criteria:**

- Client sends HTTP/3 SETTINGS with `SETTINGS_QPACK_MAX_TABLE_CAPACITY=0` and `SETTINGS_MAX_FIELD_SECTION_SIZE=4096`.
- Client sends one request stream with HEADERS and FIN.
- Parser accepts response HEADERS and DATA on the same stream.
- Unsupported push, dynamic QPACK, DATAGRAM, WebTransport, and CONNECT-UDP return `Http3UnsupportedFeature`.
- E2E serial output includes `network: http3 status=200 body=wrela-http3`.

**Code Examples:**

```wrela
data Http3Request {
    method: HttpMethod
    scheme: HttpScheme
    authority: HttpHost
    path: HttpPath
    data_class: EndpointDataClass
}
```

```go
func TestHTTP3SettingsAndGet(t *testing.T) {
	settings := nettrace.EmitHTTP3Settings(0, 4096)
	if err := nettrace.ParseHTTP3Settings(settings); err != nil {
		t.Fatal(err)
	}
	req := nettrace.EmitHTTP3Get("example.wrela.test", "/hello")
	if got := nettrace.ParseHTTP3PseudoHeaders(req); got.Path != "/hello" {
		t.Fatalf("request = %#v", got)
	}
}
```

**Steps:**

- [ ] Add failing HTTP/3 nettrace, report, and e2e tests.
- [ ] Run: `go test ./compiler/nettrace ./compiler/sem ./tests/e2e -run 'HTTP3|NetworkSubstrateHTTP3|Network.*Report' -v`
  Expected: FAIL with missing HTTP/3 frames.
- [ ] Implement HTTP/3 SETTINGS, static QPACK request headers, response parser, report fields, and e2e GET.
- [ ] Run: `go test ./compiler/nettrace ./compiler/sem ./tests/e2e -run 'HTTP3|NetworkSubstrateHTTP3|Network.*Report' -v`
  Expected: PASS on a QEMU/OVMF-capable worker. SKIP is not accepted for task completion.
- [ ] Run: `go test ./compiler/sem -run 'NetworkApplicationSourceShape|NetworkModulesCompile' -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/net/http3.wrela wrela/net/quic.wrela compiler/nettrace/http3.go compiler/nettrace/http3_test.go compiler/sem/report.go compiler/sem/report_test.go tests/e2e/network_quic_http3_qemu_test.go
git commit -m "feat: add http3 client -Codex Automated"
```

## 14. Phase 10: Policy, Production Crypto, QoS, And Hardware

**Description:** Add enforcement and production surfaces: endpoint data-class information flow, vector crypto backend selection, constant-time validation, hardware QoS, real e1000e-family support, and final full-stack acceptance.

**Acceptance Criteria:**

- Application endpoints declare data class and egress policy before sending.
- Crypto backend selection is reported and tested for scalar and vector paths.
- Constant-time validation rejects secret-dependent branch and memory-index patterns.
- QoS policy maps endpoint data class to TX priority metadata.
- Real e1000e-family cards are listed by PCI ID and exercised through opt-in hardware profile tests.

**Code Example:**

```text
Task 36, Tasks 37A-37E, and Tasks 38A-38F are policy/crypto semantic tracks.
Task 39 is QoS over the existing driver.
Task 40 is real hardware profile support.
Task 41 is the final integrated sweep.
```

### Task 36: Endpoint Data-Class Information Flow

**Files:**

- Modify: `wrela/net/endpoint_flow.wrela`
- Modify: `wrela/net/http1.wrela`
- Modify: `wrela/net/http2.wrela`
- Modify: `wrela/net/http3.wrela`
- Create: `compiler/sem/endpoint_flow_test.go`
- Create: `tests/fixtures/negative/network_secret_cleartext_http.wrela`
- Modify: `compiler/sem/report.go`
- Modify: `compiler/sem/report_test.go`

**Prerequisites:** Tasks 33 and 35.

**Description:** Add endpoint data-class labels and semantic egress checks. Secret, credential, and key-material data classes require TLS or QUIC; public, telemetry, and internal classes require an explicit endpoint policy record.

**Acceptance Criteria:**

- Data classes are `public`, `telemetry`, `internal`, `secret`, `credential`, and `key_material`.
- Cleartext HTTP/1.1 with `secret`, `credential`, or `key_material` fails with `SEM0183`.
- TLS and QUIC endpoints can carry any declared data class after policy declaration.
- Report JSON lists endpoint label, protocol, remote authority, and data class.

**Code Examples:**

```wrela
data EndpointFlowPolicy {
    label: StringLiteral
    data_class: EndpointDataClass
    transport: U8
    remote_host_hash: U64
}
```

```go
func TestSecretDataCannotUseCleartextHTTP(t *testing.T) {
	_, ds := checkNegativeFixture(t, "network_secret_cleartext_http.wrela")
	if !hasCode(ds, diag.SEM0183) {
		t.Fatalf("expected SEM0183, got %#v", ds)
	}
}
```

**Steps:**

- [ ] Add failing endpoint-flow semantic and report tests.
- [ ] Run: `go test ./compiler/sem -run 'EndpointFlow|SecretDataCannotUseCleartextHTTP|Network.*Report' -v`
  Expected: FAIL with missing endpoint flow diagnostics.
- [ ] Implement endpoint flow types, semantic checks, HTTP API policy requirements, and report fields.
- [ ] Run: `go test ./compiler/sem -run 'EndpointFlow|SecretDataCannotUseCleartextHTTP|Network.*Report' -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/net/endpoint_flow.wrela wrela/net/http1.wrela wrela/net/http2.wrela wrela/net/http3.wrela compiler/sem/endpoint_flow_test.go tests/fixtures/negative/network_secret_cleartext_http.wrela compiler/sem/report.go compiler/sem/report_test.go
git commit -m "feat: enforce endpoint data class flow -Codex Automated"
```

### Task 37A: Crypto Backend Scalar Selection

**Files:**

- Modify: `wrela/crypto/backend.wrela`
- Create: `compiler/codegen/crypto_backend_test.go`

**Prerequisites:** Tasks 21B and 29.

**Description:** Add the crypto backend enum and scalar fallback selection. This task does not emit vector instructions.

**Acceptance Criteria:**

- Scalar backend is selected when AES-NI or PCLMUL is absent.
- `CryptoBackend(kind = 0)` is named `scalar` in report helpers.
- Scalar AES-GCM still passes the Task 29 vector.
- `NetworkModulesCompile` passes.

**Code Examples:**

```wrela
fn select_crypto_backend(self, features: CpuFeatureFacts) -> CryptoBackend {
    return CryptoBackend(kind = 0)
}
```

```go
func TestCryptoBackendSelectsScalarWithoutAESNI(t *testing.T) {
	image := compileCryptoFixtureForTest(t, ir.CpuFeatureFacts{AESNI: false, PCLMUL: false})
	if got := selectedCryptoBackend(t, image); got != "scalar" {
		t.Fatalf("backend = %s", got)
	}
}
```

**Steps:**

- [ ] Add failing scalar backend selection test.
- [ ] Run: `go test ./compiler/codegen -run CryptoBackendSelectsScalar -v`
  Expected: FAIL with missing backend selection.
- [ ] Implement `CryptoBackend` naming and scalar fallback.
- [ ] Run: `go test ./compiler/codegen -run CryptoBackendSelectsScalar -v`
  Expected: PASS.
- [ ] Run: `go test ./compiler/sem -run 'NetworkApplicationSourceShape|NetworkModulesCompile' -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/crypto/backend.wrela compiler/codegen/crypto_backend_test.go
git commit -m "feat: select scalar crypto backend -Codex Automated"
```

### Task 37B: AES-NI And PCLMUL Backend

**Files:**

- Modify: `wrela/crypto/backend.wrela`
- Modify: `wrela/crypto/aes_gcm.wrela`
- Modify: `compiler/codegen/crypto_backend_test.go`

**Prerequisites:** Task 37A.

**Description:** Add the AES-NI/PCLMUL backend for CPUs that expose both features.

**Acceptance Criteria:**

- AES-NI/PCLMUL backend is selected when both features exist and AVX2 is absent.
- Codegen test asserts the AES backend emits AES instruction bytes.
- AES-GCM vector from Task 29 still passes.
- `NetworkModulesCompile` passes.

**Code Examples:**

```go
func TestAESGCMVectorBackendEmitsAESNI(t *testing.T) {
	image := compileCryptoFixtureForTest(t, ir.CpuFeatureFacts{AESNI: true, PCLMUL: true, AVX2: false})
	code := symbolBytes(t, image, "_wrela_method_wrela_crypto_aes_gcm_AesGcm_encrypt_block")
	if !bytes.Contains(code, []byte{0x66, 0x0f, 0x38}) {
		t.Fatalf("missing AES-NI opcode prefix in %x", code)
	}
}
```

**Steps:**

- [ ] Add failing AES-NI backend selection and opcode tests.
- [ ] Run: `go test ./compiler/codegen -run 'AESGCMVector|CryptoBackendAESNI' -v`
  Expected: FAIL with missing AES-NI backend.
- [ ] Implement AES-NI/PCLMUL selection and AES-GCM dispatch.
- [ ] Run: `go test ./compiler/codegen -run 'AESGCMVector|CryptoBackendAESNI' -v`
  Expected: PASS.
- [ ] Run: `go test ./compiler/sem -run 'NetworkApplicationSourceShape|NetworkModulesCompile' -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/crypto/backend.wrela wrela/crypto/aes_gcm.wrela compiler/codegen/crypto_backend_test.go
git commit -m "feat: select aesni crypto backend -Codex Automated"
```

### Task 37C: AVX2 Crypto Backend

**Files:**

- Modify: `wrela/crypto/backend.wrela`
- Modify: `wrela/crypto/aes_gcm.wrela`
- Modify: `compiler/codegen/crypto_backend_test.go`

**Prerequisites:** Task 37B.

**Description:** Add AVX2 backend selection for CPUs with AES-NI, PCLMUL, and AVX2.

**Acceptance Criteria:**

- AVX2 backend is selected only when AES-NI, PCLMUL, and AVX2 all exist.
- Missing AES-NI or missing PCLMUL falls back to scalar.
- Missing AVX2 with AES-NI/PCLMUL selects the Task 37B backend.
- `NetworkModulesCompile` passes.

**Code Examples:**

```go
func TestCryptoBackendSelectsAVX2OnlyWithRequiredFeatures(t *testing.T) {
	image := compileCryptoFixtureForTest(t, ir.CpuFeatureFacts{AESNI: true, PCLMUL: true, AVX2: true})
	if got := selectedCryptoBackend(t, image); got != "avx2_aesni_pclmul" {
		t.Fatalf("backend = %s", got)
	}
}
```

**Steps:**

- [ ] Add failing AVX2 backend selection tests.
- [ ] Run: `go test ./compiler/codegen -run 'CryptoBackend.*AVX2' -v`
  Expected: FAIL with missing AVX2 backend.
- [ ] Implement AVX2 feature selection and dispatch name.
- [ ] Run: `go test ./compiler/codegen -run 'CryptoBackend.*AVX2' -v`
  Expected: PASS.
- [ ] Run: `go test ./compiler/sem -run 'NetworkApplicationSourceShape|NetworkModulesCompile' -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/crypto/backend.wrela wrela/crypto/aes_gcm.wrela compiler/codegen/crypto_backend_test.go
git commit -m "feat: select avx2 crypto backend -Codex Automated"
```

### Task 37D: AVX-512 VAES Backend And XCR0 Gate

**Files:**

- Modify: `wrela/crypto/backend.wrela`
- Modify: `wrela/crypto/aes_gcm.wrela`
- Modify: `wrela/machine/x86_64/cpu_state.wrela`
- Modify: `compiler/codegen/crypto_backend_test.go`

**Prerequisites:** Task 37C.

**Description:** Add the AVX-512 fast tier and the OS vector-state gate. This task is codegen-facing and must not select AVX-512 merely because CPUID advertises it.

**Acceptance Criteria:**

- AVX-512 backend is selected only when VAES, VPCLMULQDQ, AVX512F, AVX512VL, and XCR0 ZMM state are all enabled.
- Without XCR0 ZMM state, the same CPUID features select `avx2_aesni_pclmul`.
- Codegen test asserts EVEX-encoded VAES/VPCLMUL bytes only in the AVX-512 case.
- `NetworkModulesCompile` passes.

**Code Examples:**

```go
func TestAESGCMAVX512BackendRequiresXCR0ZMM(t *testing.T) {
	image := compileCryptoFixtureForTest(t, ir.CpuFeatureFacts{
		AESNI: true, PCLMUL: true, AVX2: true, VAES: true, VPCLMULQDQ: true,
		AVX512F: true, AVX512VL: true, XCR0ZMM: false,
	})
	if got := selectedCryptoBackend(t, image); got != "avx2_aesni_pclmul" {
		t.Fatalf("backend without XCR0 ZMM = %s", got)
	}
	image = compileCryptoFixtureForTest(t, ir.CpuFeatureFacts{
		AESNI: true, PCLMUL: true, AVX2: true, VAES: true, VPCLMULQDQ: true,
		AVX512F: true, AVX512VL: true, XCR0ZMM: true,
	})
	if got := selectedCryptoBackend(t, image); got != "avx512_vaes_vpclmul" {
		t.Fatalf("backend with XCR0 ZMM = %s", got)
	}
}
```

**Steps:**

- [ ] Add failing AVX-512/XCR0 backend tests.
- [ ] Run: `go test ./compiler/codegen -run 'AVX512|XCR0' -v`
  Expected: FAIL with missing XCR0-gated backend.
- [ ] Implement XCR0 feature facts and AVX-512 backend selection.
- [ ] Run: `go test ./compiler/codegen -run 'AVX512|XCR0' -v`
  Expected: PASS.
- [ ] Run: `go test ./compiler/sem -run 'NetworkApplicationSourceShape|NetworkModulesCompile' -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/crypto/backend.wrela wrela/crypto/aes_gcm.wrela wrela/machine/x86_64/cpu_state.wrela compiler/codegen/crypto_backend_test.go
git commit -m "feat: gate avx512 crypto backend on xcr0 -Codex Automated"
```

### Task 37E: Crypto Backend Report Fields

**Files:**

- Modify: `compiler/sem/report.go`
- Modify: `compiler/sem/report_test.go`

**Prerequisites:** Task 37D.

**Description:** Populate the `network.crypto` report fields introduced by Task 1.

**Acceptance Criteria:**

- Report JSON includes `network.crypto.backend`.
- Report JSON includes `network.crypto.xcr0_enabled`.
- Backend report value is one of `scalar`, `aesni_pclmul`, `avx2_aesni_pclmul`, or `avx512_vaes_vpclmul`.
- `TestNetworkReportIncludesCryptoBackend` passes.

**Code Examples:**

```go
func TestNetworkReportIncludesCryptoBackend(t *testing.T) {
	r := BuildImageReport(cryptoBackendFixtureForTest(t, "avx2_aesni_pclmul", 0x7))
	if r.Network.Crypto.Backend != "avx2_aesni_pclmul" || r.Network.Crypto.XCR0Enabled != 0x7 {
		t.Fatalf("crypto report = %#v", r.Network.Crypto)
	}
}
```

**Steps:**

- [ ] Add failing crypto backend report tests.
- [ ] Run: `go test ./compiler/sem -run 'CryptoBackend|Network.*Report' -v`
  Expected: FAIL with missing report fields.
- [ ] Populate backend and XCR0 report fields.
- [ ] Run: `go test ./compiler/sem -run 'CryptoBackend|Network.*Report' -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add compiler/sem/report.go compiler/sem/report_test.go
git commit -m "feat: report crypto backend selection -Codex Automated"
```

### Task 38A: Constant-Time Secret Taint Roots

**Files:**

- Create: `compiler/sem/constant_time.go`
- Create: `compiler/sem/constant_time_test.go`
- Modify: `wrela/crypto/types.wrela`

**Prerequisites:** Task 37E.

**Description:** Add the source-level taint data model for production constant-time validation. This task only marks secret roots and records taint facts; it does not reject branches or memory indices.

**Acceptance Criteria:**

- `SecretBytes`, TLS traffic secrets, QUIC packet protection keys, and private X25519 scalars are taint roots.
- `ConstantTimeTaintFact` records source symbol, value ID, taint kind, and codegen symbol name.
- Taint propagates through arithmetic, loads, calls, and return values.
- `TestSecretTaintMarksTLSAndQUICSecrets` passes.
- `NetworkModulesCompile` passes after editing `wrela/crypto/types.wrela`.

**Code Examples:**

```go
type ConstantTimeTaintFact struct {
	SourceSymbol  string
	ValueID       ir.ValueID
	CodegenSymbol string
	TaintKind     string
}
```

```go
func TestSecretTaintMarksTLSAndQUICSecrets(t *testing.T) {
	facts := constantTimeFactsForFixture(t, "network_crypto_secrets.wrela")
	for _, want := range []string{"tls.write_key", "quic.packet_key", "x25519.private_scalar"} {
		if !facts.HasTaintKind(want) {
			t.Fatalf("missing taint kind %s in %#v", want, facts)
		}
	}
}
```

**Steps:**

- [ ] Add `TestSecretTaintMarksTLSAndQUICSecrets`.
- [ ] Run: `go test ./compiler/sem -run SecretTaintMarksTLSAndQUICSecrets -v`
  Expected: FAIL with missing secret taint graph.
- [ ] Implement taint roots and propagation facts.
- [ ] Run: `go test ./compiler/sem -run SecretTaintMarksTLSAndQUICSecrets -v`
  Expected: PASS.
- [ ] Run: `go test ./compiler/sem -run 'NetworkApplicationSourceShape|NetworkModulesCompile' -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add compiler/sem/constant_time.go compiler/sem/constant_time_test.go wrela/crypto/types.wrela
git commit -m "feat: mark network crypto secret taint roots -Codex Automated"
```

### Task 38B: Constant-Time Branch Rejection

**Files:**

- Modify: `compiler/sem/constant_time.go`
- Modify: `compiler/sem/constant_time_test.go`
- Create: `tests/fixtures/negative/secret_branch.wrela`

**Prerequisites:** Task 38A.

**Description:** Reject control-flow decisions that depend on secret-tainted values.

**Acceptance Criteria:**

- Branch condition depending on secret data fails with `SEM0184`.
- Public branches over non-secret values continue to pass.
- Diagnostic message includes the source symbol and branch location.

**Code Examples:**

```wrela
module examples.secret_branch
use { SecretBytes } from wrela.crypto.types
class Bad {
    fn leak(self, secret: SecretBytes) -> U64 {
        if secret.read_u8(offset = 0) == 0 {
            return 1
        }
        return 2
    }
}
```

```go
func TestSecretDependentBranchRejected(t *testing.T) {
	_, ds := checkNegativeFixture(t, "secret_branch.wrela")
	if !hasCode(ds, diag.SEM0184) {
		t.Fatalf("expected SEM0184, got %#v", ds)
	}
}
```

**Steps:**

- [ ] Add `TestSecretDependentBranchRejected`.
- [ ] Run: `go test ./compiler/sem -run SecretDependentBranchRejected -v`
  Expected: FAIL with missing `SEM0184`.
- [ ] Reject secret-tainted branch conditions.
- [ ] Run: `go test ./compiler/sem -run SecretDependentBranchRejected -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add compiler/sem/constant_time.go compiler/sem/constant_time_test.go tests/fixtures/negative/secret_branch.wrela
git commit -m "feat: reject secret dependent branches -Codex Automated"
```

### Task 38C: Constant-Time Memory Index Rejection

**Files:**

- Modify: `compiler/sem/constant_time.go`
- Modify: `compiler/sem/constant_time_test.go`
- Create: `tests/fixtures/negative/secret_index.wrela`

**Prerequisites:** Task 38B.

**Description:** Reject memory addressing where the index or offset depends on secret-tainted values.

**Acceptance Criteria:**

- Memory index depending on secret data fails with `SEM0184`.
- Public loop bounds over fixed vector length are accepted.
- Diagnostic message includes the source symbol and memory expression location.

**Code Examples:**

```wrela
module examples.secret_index
use { SecretBytes } from wrela.crypto.types
class Bad {
    fn leak(self, table: Bytes, secret: SecretBytes) -> U8 {
        let index = secret.read_u8(offset = 0)
        return table.read_u8(offset = index)
    }
}
```

**Steps:**

- [ ] Add `TestSecretDependentIndexRejected`.
- [ ] Run: `go test ./compiler/sem -run SecretDependentIndexRejected -v`
  Expected: FAIL with missing `SEM0184`.
- [ ] Reject secret-tainted memory indices while accepting fixed public loop bounds.
- [ ] Run: `go test ./compiler/sem -run SecretDependentIndexRejected -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add compiler/sem/constant_time.go compiler/sem/constant_time_test.go tests/fixtures/negative/secret_index.wrela
git commit -m "feat: reject secret dependent memory indices -Codex Automated"
```

### Task 38D: Constant-Time Module Coverage

**Files:**

- Modify: `compiler/sem/constant_time.go`
- Modify: `compiler/sem/constant_time_test.go`

**Prerequisites:** Task 38C.

**Description:** Wire source-level constant-time validation to every production crypto-bearing network module.

**Acceptance Criteria:**

- Validation runs on `wrela/crypto/*.wrela`, `wrela/net/tls.wrela`, and `wrela/net/quic.wrela`.
- Validation does not run on packet parsers that do not handle secrets.
- `TestConstantTimeValidationCoversNetworkCryptoModules` passes.

**Code Examples:**

```go
func TestConstantTimeValidationCoversNetworkCryptoModules(t *testing.T) {
	covered := constantTimeCoveredModulesForTest(t)
	for _, want := range []string{"wrela.crypto.aes_gcm", "wrela.crypto.x25519", "wrela.net.tls", "wrela.net.quic"} {
		if !covered[want] {
			t.Fatalf("constant-time validation missing %s", want)
		}
	}
}
```

**Steps:**

- [ ] Add `TestConstantTimeValidationCoversNetworkCryptoModules`.
- [ ] Run: `go test ./compiler/sem -run ConstantTimeValidationCoversNetworkCryptoModules -v`
  Expected: FAIL with missing module coverage.
- [ ] Wire validation to the exact module list above.
- [ ] Run: `go test ./compiler/sem -run ConstantTimeValidationCoversNetworkCryptoModules -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add compiler/sem/constant_time.go compiler/sem/constant_time_test.go
git commit -m "feat: run constant-time validation on network crypto -Codex Automated"
```

### Task 38E: Constant-Time Binary Validation

**Files:**

- Create: `compiler/codegen/constant_time_binary_test.go`
- Modify: `compiler/sem/constant_time.go`
- Modify: `compiler/sem/constant_time_test.go`

**Prerequisites:** Task 38D.

**Description:** Carry constant-time taint facts into codegen and validate emitted crypto/TLS/QUIC symbols. This task defines the data handoff between semantic analysis and binary inspection.

**Acceptance Criteria:**

- Codegen receives `ConstantTimeTaintFact` values keyed by codegen symbol and IR value ID.
- Lowering records machine register assignments for tainted IR values in `ConstantTimeMachineFact`.
- Binary validator rejects secret-tainted conditional branch operands.
- Binary validator rejects secret-tainted memory index registers.
- Binary validator rejects `div`, `idiv`, and source-marked lookup-table loads in validated symbols.

**Code Examples:**

```go
type ConstantTimeMachineFact struct {
	Symbol       string
	Instruction uint64
	Register     string
	TaintKind    string
	Role         string
}

func TestConstantTimeBinaryValidatorRejectsVariableLatencySecretOps(t *testing.T) {
	image := compileSecretDivFixtureForTest(t)
	ds := ValidateConstantTimeMachineCode(image)
	if !hasCode(ds, diag.SEM0184) {
		t.Fatalf("expected SEM0184, got %#v", ds)
	}
}
```

**Steps:**

- [ ] Add `TestConstantTimeBinaryValidatorRejectsVariableLatencySecretOps`.
- [ ] Run: `go test ./compiler/codegen -run ConstantTimeBinaryValidator -v`
  Expected: FAIL with missing binary validator.
- [ ] Add semantic-to-codegen taint fact handoff and machine fact recording.
- [ ] Reject secret-tainted branch operands, secret-tainted memory index registers, `div`, `idiv`, and source-marked table lookup patterns.
- [ ] Run: `go test ./compiler/codegen -run ConstantTimeBinaryValidator -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add compiler/codegen/constant_time_binary_test.go compiler/sem/constant_time.go compiler/sem/constant_time_test.go
git commit -m "feat: validate constant-time emitted crypto code -Codex Automated"
```

### Task 38F: Constant-Time Report Fields

**Files:**

- Modify: `compiler/sem/constant_time.go`
- Modify: `compiler/sem/constant_time_test.go`
- Modify: `compiler/codegen/constant_time_binary_test.go`
- Modify: `compiler/sem/report.go`
- Modify: `compiler/sem/report_test.go`

**Prerequisites:** Task 38E.

**Description:** Populate the `network.crypto.constant_time_validated` and `network.crypto.binary_validated` report fields introduced by Task 1.

**Acceptance Criteria:**

- Report sets `constant_time_validated=true` only after source validation runs.
- Report sets `binary_validated=true` only after binary validation runs.
- Report keeps both fields false when either validation is disabled or fails.

**Code Examples:**

```go
func TestNetworkReportIncludesConstantTimeValidation(t *testing.T) {
	r := BuildImageReport(constantTimeValidatedFixtureForTest(t))
	if !r.Network.Crypto.ConstantTimeValidated || !r.Network.Crypto.BinaryValidated {
		t.Fatalf("crypto validation report = %#v", r.Network.Crypto)
	}
}
```

**Steps:**

- [ ] Add `TestNetworkReportIncludesConstantTimeValidation`.
- [ ] Run: `go test ./compiler/sem -run 'ConstantTime|SecretDependent|Network.*Report' -v`
  Expected: FAIL with missing report flag.
- [ ] Populate the two validation report fields.
- [ ] Run: `go test ./compiler/sem ./compiler/codegen -run 'ConstantTime|SecretDependent|Network.*Report|ConstantTimeBinaryValidator' -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add compiler/sem/constant_time.go compiler/sem/constant_time_test.go compiler/codegen/constant_time_binary_test.go compiler/sem/report.go compiler/sem/report_test.go
git commit -m "feat: report production constant-time validation -Codex Automated"
```

### Task 39: Hardware QoS Policy

**Files:**

- Modify: `wrela/net/qos.wrela`
- Modify: `wrela/machine/x86_64/e1000e.wrela`
- Modify: `wrela/net/reactor.wrela`
- Modify: `compiler/sem/network_graph_test.go`
- Modify: `compiler/sem/report.go`
- Modify: `compiler/sem/report_test.go`
- Modify: `tests/e2e/network_qos_qemu_test.go`

**Prerequisites:** Tasks 11, 36, and 38F.

**Description:** Add bounded QoS classes that map endpoint data class to software TX scheduling and optional VLAN PCP insertion. This task does not add VLAN interfaces; it only sets priority metadata on emitted Ethernet frames when `QosPolicy.enable_vlan_pcp` is true.

**Acceptance Criteria:**

- QoS classes are `control`, `interactive`, `best_effort`, and `bulk`.
- Scheduler weights are `control=8`, `interactive=4`, `best_effort=2`, `bulk=1`.
- VLAN PCP values are `control=6`, `interactive=5`, `best_effort=0`, `bulk=1`.
- E2E emits one control frame and one bulk frame; host peer verifies VLAN PCP values when enabled.
- Report JSON includes QoS weights and VLAN PCP mapping.

**Code Examples:**

```wrela
data QosPolicy {
    enable_vlan_pcp: Bool
    control_weight: U64
    interactive_weight: U64
    best_effort_weight: U64
    bulk_weight: U64
}
```

```go
func TestQoSVLANPCPEncoding(t *testing.T) {
	frame := nettrace.EmitEthernetWithVLANPriority(nettrace.EthernetFrame{
		Dst: nettrace.MustMAC("52:54:00:fe:ed:01"),
		Src: nettrace.MustMAC("52:54:00:12:34:56"),
		EtherType: 0x0800,
		Payload: []byte{0x45, 0x00},
	}, 6)
	got, err := nettrace.ParseVLANPriority(frame)
	if err != nil {
		t.Fatal(err)
	}
	if got != 6 {
		t.Fatalf("pcp = %d", got)
	}
}
```

**Steps:**

- [ ] Add failing QoS semantic, report, and e2e tests.
- [ ] Run: `go test ./compiler/sem ./tests/e2e -run 'QoS|NetworkSubstrateQoS|Network.*Report' -v`
  Expected: FAIL with missing QoS policy.
- [ ] Implement QoS policy, weighted dequeue, VLAN PCP emission, and report fields.
- [ ] Run: `go test ./compiler/sem ./tests/e2e -run 'QoS|NetworkSubstrateQoS|Network.*Report' -v`
  Expected: PASS on a QEMU/OVMF-capable worker. SKIP is not accepted for task completion.
- [ ] Run: `go test ./compiler/sem -run 'NetworkApplicationSourceShape|NetworkModulesCompile' -v`
  Expected: PASS.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/net/qos.wrela wrela/machine/x86_64/e1000e.wrela wrela/net/reactor.wrela compiler/sem/network_graph_test.go compiler/sem/report.go compiler/sem/report_test.go tests/e2e/network_qos_qemu_test.go
git commit -m "feat: add network hardware qos policy -Codex Automated"
```

### Task 40: Real e1000e-Family Card Support

**Files:**

- Create: `wrela/machine/x86_64/e1000e_family.wrela`
- Modify: `wrela/machine/x86_64/e1000e.wrela`
- Create: `compiler/sem/e1000e_family_test.go`
- Create: `tests/hardware/e1000e_profile_test.go`
- Modify: `compiler/sem/report.go`
- Modify: `compiler/sem/report_test.go`

**Prerequisites:** Tasks 12 and 39.

**Description:** Add a real-hardware support table for tested e1000e-family PCI IDs and hardware profile tests. QEMU remains the default e2e path; real cards are opt-in through `WRELA_E1000E_HARDWARE_PROFILE=1`.

**Acceptance Criteria:**

- Supported device table includes `0x10D3 82574L`, `0x1502 82579LM`, `0x153A I217-LM`, and `0x155A I218-LM`.
- Unsupported e1000-family PCI IDs fail with a report diagnostic naming vendor/device ID.
- Hardware profile test reads MAC, link state, MSI capability, BAR0 length, and EEPROM presence.
- Hardware profile test skips unless `WRELA_E1000E_HARDWARE_PROFILE=1`.
- Report JSON includes `hardware_profile.tested=true` only when the hardware profile test ran.

**Code Examples:**

```wrela
data E1000eDeviceInfo {
    device_id: U16
    name: StringLiteral
    requires_phy_workaround: Bool
}

fn e1000e_device_info(device_id: U16) -> Option<E1000eDeviceInfo> {
    if device_id == 0x10D3 { return Option.Some(value = E1000eDeviceInfo(device_id = device_id, name = "82574L", requires_phy_workaround = false)) }
    if device_id == 0x1502 { return Option.Some(value = E1000eDeviceInfo(device_id = device_id, name = "82579LM", requires_phy_workaround = true)) }
    if device_id == 0x153A { return Option.Some(value = E1000eDeviceInfo(device_id = device_id, name = "I217-LM", requires_phy_workaround = true)) }
    if device_id == 0x155A { return Option.Some(value = E1000eDeviceInfo(device_id = device_id, name = "I218-LM", requires_phy_workaround = true)) }
    return Option.None
}
```

```go
func TestE1000eHardwareProfile(t *testing.T) {
	if os.Getenv("WRELA_E1000E_HARDWARE_PROFILE") != "1" {
		t.Skip("set WRELA_E1000E_HARDWARE_PROFILE=1 on a dedicated e1000e-family test host")
	}
	profile := runE1000eHardwareProfile(t)
	if profile.VendorID != 0x8086 || profile.MAC == "00:00:00:00:00:00" || profile.BAR0Length == 0 {
		t.Fatalf("profile = %#v", profile)
	}
}
```

**Steps:**

- [ ] Add failing family source-shape, report, and hardware profile tests.
- [ ] Run: `go test ./compiler/sem ./tests/hardware -run 'E1000eFamily|E1000eHardwareProfile|Network.*Report' -v`
  Expected: semantic test FAILS with missing family table; hardware test SKIPS unless explicitly enabled.
- [ ] Implement family table, QEMU compatibility path, report fields, and opt-in hardware profile harness.
- [ ] Run: `go test ./compiler/sem ./tests/hardware -run 'E1000eFamily|E1000eHardwareProfile|Network.*Report' -v`
  Expected: PASS for semantic tests and SKIP or PASS for hardware profile.
- [ ] Run: `git diff --check`
- [ ] Commit:

```bash
git add wrela/machine/x86_64/e1000e_family.wrela wrela/machine/x86_64/e1000e.wrela compiler/sem/e1000e_family_test.go tests/hardware/e1000e_profile_test.go compiler/sem/report.go compiler/sem/report_test.go
git commit -m "feat: add real e1000e family support table -Codex Automated"
```

### Task 41: Full Network Application Acceptance Sweep

**Files:**

- No source edits unless a listed command exposes a defect introduced by Tasks 25-29, 30A-30E, 31-33, 34A-34F, 35-36, 37A-37E, 38A-38F, and 39-40.

**Prerequisites:** Tasks 25-29, 30A-30E, 31-33, 34A-34F, 35-36, 37A-37E, 38A-38F, and 39-40.

**Description:** Run the full substrate, application protocol, policy, crypto, QoS, and hardware-profile acceptance suite. This is the final checkpoint for `network-application-v1`.

**Acceptance Criteria:**

- `go test ./compiler/nettrace -run 'DHCP|DNS|TCP|TLS13|HTTP1|HTTP2|QUIC|HTTP3' -v` passes.
- `go test ./compiler/sem -run 'EndpointFlow|ConstantTime|CryptoBackend|E1000eFamily|Network.*Report' -v` passes.
- `go test ./compiler/codegen -run 'CryptoBackend|AESGCMVector|AVX512|XCR0|ConstantTimeBinaryValidator' -v` passes.
- `go test ./tests/e2e -run 'NetworkSubstrate(DHCP|DNS|TCP|HTTP1|HTTP2|QUIC|HTTP3|QoS)' -v` passes on a QEMU/OVMF-capable worker.
- `go test ./tests/hardware -run E1000eHardwareProfile -v` skips by default and passes on a dedicated hardware host with `WRELA_E1000E_HARDWARE_PROFILE=1`.
- `go test ./...` passes.

**Code Examples:**

```bash
go test ./compiler/nettrace -run 'DHCP|DNS|TCP|TLS13|HTTP1|HTTP2|QUIC|HTTP3' -v
go test ./compiler/sem -run 'EndpointFlow|ConstantTime|CryptoBackend|E1000eFamily|Network.*Report' -v
go test ./compiler/codegen -run 'CryptoBackend|AESGCMVector|AVX512|XCR0|ConstantTimeBinaryValidator' -v
go test ./tests/e2e -run 'NetworkSubstrate(DHCP|DNS|TCP|HTTP1|HTTP2|QUIC|HTTP3|QoS)' -v
go test ./tests/hardware -run E1000eHardwareProfile -v
go test ./...
```

**Steps:**

- [ ] Run all acceptance commands above.
- [ ] Fix only defects introduced by Tasks 25-29, 30A-30E, 31-33, 34A-34F, 35-36, 37A-37E, 38A-38F, and 39-40.
- [ ] Rerun the failing command after each fix.
- [ ] Rerun `go test ./...` after all fixes.
- [ ] Run: `git diff --check`
- [ ] Commit final fixes:

```bash
git add wrela/net wrela/crypto wrela/machine/x86_64/e1000e.wrela wrela/machine/x86_64/e1000e_family.wrela wrela/machine/x86_64/cpu_state.wrela compiler/nettrace compiler/sem/report.go compiler/sem/report_test.go compiler/sem/endpoint_flow_test.go compiler/sem/constant_time.go compiler/sem/constant_time_test.go compiler/codegen/crypto_backend_test.go compiler/codegen/constant_time_binary_test.go tests/e2e/network_dhcp_dns_qemu_test.go tests/e2e/network_tcp_http_qemu_test.go tests/e2e/network_quic_http3_qemu_test.go tests/e2e/network_qos_qemu_test.go tests/hardware/e1000e_profile_test.go
git commit -m "test: complete network application acceptance sweep -Codex Automated"
```

---

## 15. Appendix A: Exact Runtime Algorithms

**Description:** These algorithms are the reference for task implementations. Use them when translating into Wrela source.

**Acceptance Criteria:**

- Protocol behavior in `wrela/net/*.wrela` matches these algorithms.
- Host-side `compiler/nettrace` fixtures use the same constants.
- E2E tests validate at least one success and one malformed input path per implemented protocol.

**Code Example:**

RX processing:

```text
1. poll_rx obtains a completed descriptor.
2. Descriptor bytes become QuarantinedRxBytes with PacketLease(epoch=current).
3. Ethernet verifier checks length and destination MAC.
4. ARP packets go to ARP verifier and ARP dispatch.
5. IPv4 packets go to IPv4 verifier.
6. ICMP packets go to ICMP verifier and echo reply.
7. UDP packets go to UDP verifier and endpoint dispatch.
8. Every drop records a counter and metadata flight record.
9. Every completed packet releases or recycles the RX descriptor.
```

IPv4 checksum:

```text
sum 16-bit big-endian header words with checksum field treated as zero
fold carries until sum <= 0xffff
checksum = bitwise-not(sum) & 0xffff
valid packet has folded sum over full header equal to 0xffff
```

ARP cache replacement:

```text
if matching valid IP exists:
  update MAC, epoch, expires_at_tick
else if count < 16:
  write next unused entry and increment count
else:
  overwrite next_victim and advance next_victim modulo 16
```

UDP echo:

```text
receive IPv4 protocol 17
verify UDP length and checksum
match endpoint local_port = dst_port
for port 7:
  swap source/destination IPs
  swap source/destination ports
  copy payload into TX frame
  emit Ethernet + IPv4 + UDP
```

DHCPv4 client:

```text
state init:
  xid = deterministic test xid or reactor-generated xid
  send DISCOVER from 0.0.0.0:68 to 255.255.255.255:67
  include options 53=discover, 55=[1,3,6,51,58,59], 61=mac client id, 12=wrela
  transition selecting

state selecting:
  accept OFFER only when xid matches, chaddr matches guest MAC, yiaddr != 0,
  option 54 server_id is present, option 1 subnet mask is present
  store offered lease and send REQUEST naming requested_ip and server_id
  transition requesting

state requesting:
  accept ACK only when xid and server_id match the offered lease
  require options 1 subnet mask and 51 lease seconds
  optional options 3 router, 6 DNS, 58 renewal, 59 rebind use configured defaults when absent
  write bound config atomically and transition bound

drop path:
  invalid xid, wrong server id, missing yiaddr, missing subnet mask, or missing
  lease seconds increments drop_malformed_dhcp and records reason dhcp_malformed
```

DNS A-record resolver:

```text
emit query:
  allocate one outstanding id
  qname is uncompressed labels, qtype A, qclass IN
  send UDP to configured dns_server:53

receive response:
  require matching id, qr=1, opcode=0, rcode=0 or 3, qtype A, qclass IN
  parse compressed answer names with a maximum of 8 pointer hops
  NXDOMAIN increments dns_nxdomain and returns DnsNameNotFound
  first matching A answer becomes cache entry with ttl=min(answer_ttl, 300)
  cache has 8 FIFO slots and overwrites next_victim when full
```

TCP bounded stream:

```text
active open:
  closed -> syn_sent after SYN(seq=0x10000000+connection_id)
  syn_sent -> established only after SYN|ACK with ack=send_next
  send ACK and expose TcpStreamAuthority

receive data:
  accept only seq == recv_next
  copy payload to stream buffer, advance recv_next, ACK immediately
  out-of-order payload increments drop_tcp_out_of_order

retransmit:
  timer after 200 ticks retransmits SYN or oldest unacked segment
  after 5 retries close connection and report TcpRetransmitLimit

close:
  FIN follows fin_wait_1 -> fin_wait_2 -> time_wait, passive close follows
  close_wait -> last_ack -> closed
```

TLS 1.3:

```text
ClientHello:
  legacy_version 0x0303
  cipher_suites [0x1301]
  supported_versions [0x0304]
  key_share X25519 only
  ALPN from caller

handshake:
  transcript = SHA256(ClientHello || ServerHello || EncryptedExtensions || Certificate || CertificateVerify || Finished)
  derive handshake and application secrets with HKDF-Expand-Label
  validate certificate SPKI SHA-256 equals endpoint pinned hash
  verify Finished before exposing TlsStreamAuthority

unsupported:
  any PQ group, TLS 1.2 version, WebPKI chain mode, or non-0x1301 cipher suite
  returns TlsUnsupportedFeature and is reported as rejected
```

HTTP/1.1:

```text
request:
  write "GET <path> HTTP/1.1\r\n"
  write Host, User-Agent: wrela/1, Connection: close
  no request body

response:
  accept status line HTTP/1.1 200
  require Content-Length
  reject chunked transfer
  reject any header line longer than 1024 bytes
  read exactly Content-Length bytes up to caller max_body
```

HTTP/2:

```text
connection:
  send client preface
  send SETTINGS(initial_window_size=65535)
  ACK peer SETTINGS

request:
  stream id 1 only
  encode pseudo headers with static HPACK: :method GET, :scheme https,
  :authority, :path
  dynamic table size is zero

response:
  accept HEADERS and DATA on stream 1
  END_STREAM completes response
  RST_STREAM or GOAWAY returns Http2StreamClosed
```

QUIC v1:

```text
initial:
  version 0x00000001
  dcid length 8
  derive Initial keys from salt 38762cf7f55934b34d179ae6a4c80cadccbb7f0a
  send CRYPTO frames carrying TLS ClientHello

packet spaces:
  Initial, Handshake, and 1-RTT each track next_packet_number and largest_acked
  ACK marks packet numbers delivered; oldest unacked retransmits after loss timer

stream:
  one bidirectional stream id 0
  STREAM frames carry ordered bytes only
  CONNECTION_CLOSE records reason and closes authority
```

HTTP/3:

```text
control stream:
  send SETTINGS with qpack max table capacity 0 and max field section size 4096
  reject push, DATAGRAM, WebTransport, CONNECT-UDP

request stream:
  one bidirectional stream
  static QPACK pseudo headers :method GET, :scheme https, :authority, :path
  FIN after request headers

response:
  accept HEADERS then DATA on same stream
  FIN completes response
```

Endpoint data-class flow:

```text
public, telemetry, internal:
  require declared EndpointFlowPolicy and any declared transport

secret, credential, key_material:
  require declared EndpointFlowPolicy and transport TLS or QUIC
  cleartext HTTP emits SEM0183 at semantic check time

report:
  record label, data_class, protocol, remote_host_hash, and enforcement result
```

Production constant-time validation:

```text
source pass:
  mark SecretBytes, TLS secrets, QUIC keys, and private X25519 scalars secret
  propagate through arithmetic, loads, calls, and packet protection helpers
  reject secret-tainted if/switch conditions and memory indices with SEM0184

binary pass:
  inspect emitted crypto/TLS/QUIC symbols
  reject secret-tainted conditional branch operands
  reject secret-tainted index registers in memory operands
  reject div and idiv inside validated symbols
  set report flag only after source and binary passes both succeed
```

QoS:

```text
class mapping:
  control weight=8 pcp=6
  interactive weight=4 pcp=5
  best_effort weight=2 pcp=0
  bulk weight=1 pcp=1

scheduler:
  each TX tick drains control, interactive, best_effort, bulk by weight
  if vlan pcp is enabled, emit 802.1Q tag with PCP from class
  if disabled, emit ordinary Ethernet II frame and keep class in report only
```

---

## 16. Appendix B: Final Global Acceptance Criteria

**Description:** The full networking stack implementation is complete only when these criteria are true. Task 24 proves the substrate milestone; Task 41 proves the application and production milestone.

**Acceptance Criteria:**

- Wrela boots under QEMU with `-device e1000e`.
- The network reactor owns NIC rings, packet memory, ARP, IPv4, ICMP, UDP, DHCP, DNS, TCP, QUIC, counters, recorder, timers, and epoch state.
- The report names DMA domain policy, descriptor counts, memory type, MMIO/barrier policy, interrupt mode, DHCP/DNS config, TCP/QUIC capacities, TLS cipher, HTTP protocols, endpoint data classes, crypto backend, XCR0 vector-state bits, constant-time validation status, QoS policy, hardware profile, reactor placement, lane budgets, and reset epoch.
- A controlled host peer can ARP for the guest, send ICMP echo to the guest, and receive ICMP echo reply.
- A controlled host peer can send UDP to port `7` and receive the payload back.
- Quarantined packet bytes are rejected outside verifier paths.
- Packet leases and slices cannot escape their bounded lifetime.
- Drop counters and flight recorder metadata record wrong-MAC and IPv4-checksum drops.
- DHCP obtains a bounded IPv4 lease and DNS resolves an A record from the configured DNS server.
- TCP completes a bounded handshake and payload transfer.
- TLS 1.3 completes with pinned SPKI validation and secret bytes remain inside crypto authorities.
- HTTP/1.1, HTTP/2, QUIC v1, and HTTP/3 each complete one GET scenario.
- Endpoint data-class checks reject secret cleartext HTTP.
- Vector crypto backend selection and constant-time validation are reported.
- QoS policy emits tested priority metadata.
- Real e1000e-family support has an opt-in hardware profile test.
- No POSIX socket API, checksum offload, multiqueue, RSS, jumbo frames, full VLAN interfaces, DHCPv6, IPv6, DNSSEC, TLS 1.2, WebPKI path building, dynamic HPACK/QPACK, QUIC DATAGRAM, WebTransport, or CONNECT-UDP support is introduced by this plan.

**Code Example:**

```bash
go test ./...
go test ./tests/e2e -run 'NetworkSubstrate(QEMU|DHCP|DNS|TCP|HTTP1|HTTP2|QUIC|HTTP3|QoS)' -v
```

---

## 17. Appendix C: Design Coverage Map

**Description:** This map ties the design document to executable tasks so a reviewer can verify scope without rereading the whole plan.

**Acceptance Criteria:**

- Every `network-substrate-v0` and `network-application-v1` milestone has at least one implementation task.
- Task 24 substrate acceptance and Task 41 application acceptance are both represented.
- Explicitly out-of-scope items are named as exclusions, not deferred work.

**Code Example:**

```text
Milestone 0 packet trace harness:
  Task 3

Milestone 1 e1000e RX/TX:
  Tasks 2, 5, 8, 9A, 9B, 9C, 10, 11, 12, 22

Milestone 2 Ethernet II:
  Task 13

Milestone 3 ARP:
  Task 14

Milestone 4 IPv4 and static config:
  Tasks 6, 15, 21A, 21B

Milestone 5 ICMP ping:
  Task 16

Milestone 6 UDP:
  Task 17

Bounded memory, packet quarantine, reports, diagnostics, epochs, timers:
  Tasks 5, 7, 18, 19, 20, 21A, 21B

QEMU L2-visible e2e harness:
  Tasks 2, 22

Application source contracts:
  Task 25

DHCP and DNS:
  Tasks 26, 27

TCP, crypto, TLS, HTTP/1.1, HTTP/2:
  Tasks 28, 29, 30A, 30B, 30C, 30D, 30E, 31, 32, 33

QUIC and HTTP/3:
  Tasks 34A, 34B, 34C, 34D, 34E, 34F, 35

Endpoint data-class flow:
  Task 36

Vector crypto and constant-time validation:
  Tasks 37A, 37B, 37C, 37D, 37E, 38A, 38B, 38C, 38D, 38E, 38F

Hardware QoS:
  Task 39

Real e1000e-family cards:
  Task 40

Full application acceptance:
  Task 41

Excluded beyond this plan:
  POSIX socket API, checksum offload, multiqueue, RSS, jumbo frames, full VLAN
  interfaces, IPv6, DHCPv6, DNSSEC, TLS 1.2, WebPKI path building, dynamic
  HPACK/QPACK, QUIC DATAGRAM, WebTransport, and CONNECT-UDP.
```
