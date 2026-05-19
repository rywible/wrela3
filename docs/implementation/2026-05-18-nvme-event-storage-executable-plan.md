# NVMe Event Storage Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build Wrela's first direct-NVMe durable event store: interrupt-driven NVMe IO paths, fixed 512-byte event slots, atomic event groups, dense stream directories, encrypted-API blob extents, rebuildable projections, and replay after reboot.

**Architecture:** The foreground/app/display core owns the only `StorageWriter`, event ID allocation, durable frontier publication, stream directory heads, and acceptance of maintenance proposals. A separate maintenance core owns projection work, blob relocation, orphan collection, and cold event segment packing through a background NVMe path. NVMe is a private storage backend; application state is expressed as durable events, blob refs, checkpoints, and projection roots.

**Tech Stack:** Go 1.22+ compiler; existing Wrela lexer/parser/semantic checker/IR/x86_64 codegen; Wrela source modules under `wrela/`; QEMU q35 + OVMF + NVMe device for end-to-end tests; Go unit tests for compiler contracts and byte-format algorithms.

---

## 0. How To Execute This Plan

**Description:** This plan is written so a junior engineer can take any task, understand the exact contract, add the failing test first, implement the smallest matching change, and verify it without reopening design questions.

**Acceptance Criteria:**

- Every task ends with a commit whose message ends in `-Codex Automated`.
- Each task's new tests fail before implementation and pass after implementation.
- `git diff --check` passes before every commit.
- Full-plan completion requires `go test ./...` and the storage QEMU tests listed in Task 24.
- No task introduces POSIX filesystem semantics, SQL, ambient logging, multi-writer event logs, general query planning, production command inboxes, real key destruction, or full-disk encryption.

**Code Example:**

```bash
go test ./compiler/parse -run TestParseEventDeclaration -v
go test ./compiler/sem -run TestEventLayoutSemanticContracts -v
git diff --check
git add compiler/parse/parser.go compiler/parse/parser_test.go compiler/ast/ast.go
git commit -m "feat: parse durable event declarations -Codex Automated"
```

Assumptions frozen for execution:

- The physical arena memory, production substrate convergence, and language expressiveness implementation plans have landed before this plan is executed.
- `event` and `projection` become top-level declaration keywords.
- `id`, `layout`, `current`, and `upcast` are contextual identifiers, not global keywords.
- There is no `worker` keyword in this milestone. Projection workers are ordinary executors or classes pinned to the maintenance core by boot wiring.
- Blob payload encryption uses the final metadata/API shape in this milestone, but QEMU tests use an explicitly named `DevelopmentPassthroughAead`. Production constructors reject that mode unless the image opts into development storage.
- V1 accepts active NVMe LBAs of 512 bytes or 4096 bytes. Other active LBA sizes fail during storage initialization with a boot-fatal panic.
- V1 supports conventional namespaces fully. Zoned Namespace support is detected and represented in metadata; zone append is implemented as an optional path and exercised by unit tests with synthetic Identify data.

Definition of done for one task:

- The task's files changed only where listed.
- The task's tests have the expected fail-before/pass-after loop.
- The task's acceptance criteria are true.
- The commit message matches the exact message shown by the task.

Definition of done for the whole plan:

- `go test ./...` passes.
- `go test ./tests/e2e -run NvmeEventStorage -v` passes on a machine with QEMU and OVMF.
- `go test ./tests/e2e -run NvmeEventStorageReplay -v` proves replay after reboot.
- The placeholder phrase scan in Task 25 prints no matches.
- The storage audit report contains foreground/background NVMe paths, the selected durability mode, event-slot metrics, blob orphan bytes, projection lag, and stream directory cache hit rate.

---

## 1. Frozen Storage Decisions

**Description:** These are implementation decisions. Do not reinterpret them during task execution.

**Acceptance Criteria:**

- All code, tests, docs, and examples use these names, sizes, constants, and boundaries.
- Any change to this section requires a separate design update before implementation work continues.

**Code Example:**

```wrela
const STORAGE_EVENT_SLOT_SIZE: U64 = 512
const STORAGE_EVENT_HEADER_SIZE: U64 = 64
const STORAGE_EVENT_PAYLOAD_BYTES: U64 = STORAGE_EVENT_SLOT_SIZE - STORAGE_EVENT_HEADER_SIZE
const STORAGE_TARGET_BATCH_SLOTS: U64 = 64
const STORAGE_MAX_OVERFLOW_SLOTS: U64 = 8
const STORAGE_MAX_BATCH_SLOTS: U64 = 72
const STORAGE_MAX_ATOMIC_GROUP_SLOTS: U64 = 32
const STORAGE_GROUP_COMMIT_TIMER_US: U64 = 2000
const STORAGE_HOT_SEGMENT_SLOTS: U64 = 1048576
```

On-disk byte order is little-endian.

Hot event slot layout is exactly 512 bytes:

```text
offset  size  field
0       8     event_id
8       8     stream_id
16      8     stream_sequence
24      4     event_type_id
28      4     payload_layout_id
32      4     atomic_group_len
36      4     atomic_group_index
40      4     payload_length
44      4     flags
48      4     checksum32
52      2     header_version
54      2     reserved16
56      8     reserved64
64      448   payload
```

Checksum rule:

```text
checksum32 = crc32c(slot bytes 0..511 with bytes 48..51 set to zero)
```

Reserved empty slot rule:

```text
event_type_id = 0
payload_layout_id = 0
stream_id = 0
stream_sequence = 0
atomic_group_len = 0
atomic_group_index = 0
payload_length = 0
flags has STORAGE_SLOT_RESERVED_EMPTY set
event_id stores the consumed event ID position
checksum32 is valid
```

Batch policy constants:

```text
target_batch_slots = 64
max_overflow_slots = 8
max_batch_slots = 72
max_atomic_group_slots = 32
group_commit_timer_us = 2000
```

Superblock layout:

```text
superblock copy A byte offset = 0
superblock copy B byte offset = 4096
superblock copy size = 4096 bytes
region map byte offset = 8192
```

Stream directory entry layout is exactly 32 bytes:

```text
offset  size  field
0       8     latest_sequence
8       8     latest_event_id
16      8     latest_checkpoint_ref
24      8     flags
```

NVMe queue depths:

```text
admin_queue_depth = 32
foreground_io_queue_depth = 256
background_io_queue_depth = 128
max_prp_transfer_bytes = 131072
```

Segment lifecycle constants:

```text
hot_segment_slots = 1048576
hot_window_min_sealed_segments = 4
cold_codec_packed_v1 = 1
```

Blob constants:

```text
blob_inline_extent_count = 4
blob_manifest_extent_limit = 128
blob_allocator_free_extent_limit = 1024
blob_cipher_development_passthrough = 1
```

---

## 2. Repository Layout And File Responsibilities

**Description:** The implementation must create or modify only these files unless a task explicitly says to add a test fixture. File ownership is intentionally narrow so parallel workers do not step on each other.

**Acceptance Criteria:**

- The final implementation's storage-related files match this map.
- Any extra file created during a task is listed in that task's commit message body with the reason.

**Code Example:**

```text
wrela/machine/x86_64/nvme.wrela
  NVMe register view, controller initialization API, queue pair types, command submission, interrupt receiver shape, Identify result parsing, durability mode selection.

wrela/machine/x86_64/core_link.wrela
  Paired foreground/maintenance SPSC descriptors with credits, wait arming, monitor/mwait-compatible wake metadata, and IPI fallback hooks.

wrela/storage/format.wrela
  Fixed storage constants, superblock, region map, event slot header, stream directory entry, segment metadata, metrics records.

wrela/storage/event_log.wrela
  Event slot encoding, checksum, batch packing, reserved empty slots, recovery scan, segment sealing, packed cold segment codec.

wrela/storage/stream.wrela
  Dense stream directory, expected sequence checks, new stream allocation, directory rebuild from recovered events.

wrela/storage/blob.wrela
  Blob refs, extents, manifests, free extent allocator, development AEAD adapter, write/read/delete shape, orphan scanner, relocation proposals.

wrela/storage/projection.wrela
  Projection root refs, checkpoints, StateCell, DenseEntityMap, OrderedPages, ProjectionWriter, read-plane watermark behavior.

wrela/storage/writer.wrela
  Single StorageWriter authority, append request batching, NVMe durability completion handling, maintenance proposal validation, metrics.

wrela/storage/file_model.wrela
  First file-like entity stream events and a small file state projection used by QEMU tests.

compiler/lex/token.go
compiler/lex/lexer_test.go
  Adds event/projection keywords.

compiler/ast/ast.go
compiler/ast/ast_test.go
  Adds EventDecl, ProjectionDecl, layout declarations, and deterministic debug output.

compiler/parse/parser.go
compiler/parse/parser_test.go
  Parses event and projection durable ABI declarations.

compiler/sem/storage.go
compiler/sem/storage_test.go
compiler/sem/storage_negative_test.go
tests/fixtures/negative/*.wrela
  Validates stable event IDs, layout IDs, current layout rules, upcast endpoints, projection IDs, storage authority, NVMe path ownership, and core-link endpoint ownership.

compiler/ir/ir.go
compiler/ir/lower.go
compiler/ir/storage_test.go
  Adds event/projection metadata to IR and lowers compiler-generated event encoders.

compiler/codegen/x64.go
compiler/codegen/storage_test.go
  Emits event-slot field stores, CRC32C fallback calls, generated event encoders, and storage-format data objects.

compiler/report/report.go
compiler/report/report_test.go
compiler/sem/report.go
compiler/sem/report_test.go
  Adds storage/NVMe/core-link/blob/projection metrics and audit entries.

tests/e2e/nvme_event_storage_qemu_test.go
tests/e2e/fixtures/nvme_event_storage/main.wrela
tests/e2e/fixtures/nvme_event_storage/program.wrela
  End-to-end append, replay, blob, projection, and metrics validation.

docs/production-deferred-work.md
  Records out-of-scope storage work that this milestone intentionally does not implement.
```

---

## 3. Parallel Work Map

**Description:** The plan has a serial spine for syntax, semantic metadata, storage source contracts, and QEMU integration. Workers may run in parallel only at the merge gates below.

**Acceptance Criteria:**

- No task begins before its listed prerequisite lands.
- Workers do not modify files outside their stream without coordinating through the tech lead.

**Code Example:**

```text
Merge Gate 0:
  Task 1 lands first.

Stream A, Compiler Declarations:
  Tasks 2-6, serial.

Stream B, Core Link And NVMe:
  Tasks 7-11, serial after Task 1.

Stream C, Disk Format And Writer:
  Tasks 12-17, serial after Tasks 6 and 11.

Stream D, Blob And Maintenance:
  Tasks 18-20, serial after Tasks 12-17.

Stream E, Projections And File Model:
  Tasks 21-22, serial after Tasks 6, 7, and 17.

Stream F, Integration And Reports:
  Tasks 23-25, serial after Streams B, C, D, and E land.
```

Dependency rules:

- Task 1 lands before all other tasks.
- Tasks 2-6 are serial because parser, semantic, IR, and codegen contracts build on one another.
- Task 7 may run after Task 1 and before NVMe work because it owns only core-link source and graph checks.
- Tasks 8-11 are serial because NVMe source shape, Identify parsing, queue submission, and interrupt completion contracts build progressively.
- Tasks 12-17 start only after Tasks 6 and 11 because event encoders and NVMe paths are prerequisites for storage writer behavior.
- Tasks 18-20 start after Task 17 because blobs and maintenance proposals depend on writer publication rules.
- Tasks 21-22 start after Tasks 6, 7, and 17 because projections need declarations, core links, and committed group feeds.
- Tasks 23-25 are final integration and reporting.

---

## 4. Phase 1: Freeze Diagnostics And Durable ABI Syntax

**Description:** This phase reserves diagnostics and teaches the compiler to recognize durable event/projection declarations before any runtime storage code depends on them.

**Acceptance Criteria:**

- Storage diagnostics are stable.
- `event` and `projection` tokenize as keywords.
- Event/projection AST nodes preserve every source field needed by semantic checking.
- Parser tests cover current layout, historical layout, upcast mapping, projection containers, and contextual `id/layout/current/upcast` usage.

**Phase Code Example:**

```wrela
event FileRenamed id 17 {
    file_id: FileId
    directory_id: FileId
    name_ref: BlobRef

    layout 1 current {
        file_id: U64 = self.file_id.value
        directory_id: U64 = self.directory_id.value
        name_ref: BlobRefPayload = self.name_ref.payload
    }
}
```

### Task 1: Storage Diagnostics And Deferred-Work Contract

**Files:**

- Modify: `compiler/diag/codes.go`
- Modify: `compiler/diag/diag_test.go`
- Modify: `docs/production-deferred-work.md`

**Description:** Reserve stable diagnostic codes for storage declarations, storage authority, NVMe ownership, and core-link misuse. Record the exact storage scope so future work does not treat this milestone as a filesystem or database layer.

**Acceptance Criteria:**

- `SEM0099` through `SEM0124` exist with the exact comments below.
- `docs/production-deferred-work.md` lists storage work outside this milestone.
- The diagnostic test fails before codes are added and passes after codes are added.

**Code Examples:**

Add this test to `compiler/diag/diag_test.go`:

```go
func TestStorageDiagnosticCodesExist(t *testing.T) {
	codes := []string{
		diag.SEM0099, diag.SEM0100, diag.SEM0101, diag.SEM0102,
		diag.SEM0103, diag.SEM0104, diag.SEM0105, diag.SEM0106,
		diag.SEM0107, diag.SEM0108, diag.SEM0109, diag.SEM0110,
		diag.SEM0111, diag.SEM0112, diag.SEM0113, diag.SEM0114,
		diag.SEM0115, diag.SEM0116, diag.SEM0117, diag.SEM0118,
		diag.SEM0119, diag.SEM0120, diag.SEM0121, diag.SEM0122,
		diag.SEM0123, diag.SEM0124,
	}
	for _, code := range codes {
		if code == "" {
			t.Fatalf("storage diagnostic code must not be empty")
		}
	}
}
```

Add these constants after `SEM0098` in `compiler/diag/codes.go`:

```go
	SEM0099  = "SEM0099"  // duplicate durable event type id
	SEM0100  = "SEM0100"  // invalid durable event type id
	SEM0101  = "SEM0101"  // invalid event layout current marker
	SEM0102  = "SEM0102"  // duplicate event layout id
	SEM0103  = "SEM0103"  // invalid event layout field
	SEM0104  = "SEM0104"  // invalid event upcast endpoint
	SEM0105  = "SEM0105"  // missing event upcast field mapping
	SEM0106  = "SEM0106"  // duplicate projection id
	SEM0107  = "SEM0107"  // invalid projection layout current marker
	SEM0108  = "SEM0108"  // unsupported projection container shape
	SEM0109  = "SEM0109"  // invalid projection upcast endpoint
	SEM0110  = "SEM0110"  // storage disk region overlap or invalid size
	SEM0111  = "SEM0111"  // NVMe queue path ownership mismatch
	SEM0112  = "SEM0112"  // core link endpoint ownership mismatch
	SEM0113  = "SEM0113"  // StorageWriter authority cannot be forged or shared
	SEM0114  = "SEM0114"  // atomic event group exceeds configured maximum
	SEM0115  = "SEM0115"  // unstable event id source is not allowed
	SEM0116  = "SEM0116"  // storage append must observe durability result
	SEM0117  = "SEM0117"  // blob ref references unpublished blob bytes
	SEM0118  = "SEM0118"  // maintenance proposal mutates truth directly
	SEM0119  = "SEM0119"  // projection root watermark is invalid
	SEM0120  = "SEM0120"  // projection worker feed is not boot wired
	SEM0121  = "SEM0121"  // event payload exceeds inline slot budget
	SEM0122  = "SEM0122"  // active NVMe LBA size is unsupported by v1 storage
	SEM0123  = "SEM0123"  // development blob cipher used without explicit opt in
	SEM0124  = "SEM0124"  // storage metric publication is incomplete
```

Add this section to `docs/production-deferred-work.md`:

```markdown
## Storage beyond the first NVMe event-store milestone
- POSIX filesystem compatibility remains out of scope. The first storage surface is events, blobs, checkpoints, projections, and file-like entity streams.
- SQL, relational query planning, general secondary indexes, and multi-writer event-log sharding remain out of scope.
- Production command inboxes, idempotency records, replication, full-disk encryption, IOMMU-backed DMA isolation, SGL-heavy NVMe transfers, tuned compression codec selection, and destructive retention policies remain deferred.
- The first blob cipher used by QEMU tests is a named development passthrough mode behind the final blob manifest API. Production images must not construct it without an explicit development-storage opt in.
```

Verification:

```bash
go test ./compiler/diag -run TestStorageDiagnosticCodesExist -v
rg -n "Storage beyond the first NVMe event-store milestone|development passthrough" docs/production-deferred-work.md
git diff --check
```

Expected: the Go test passes, `rg` prints the new storage section, and `git diff --check` prints nothing.

Commit:

```bash
git add compiler/diag/codes.go compiler/diag/diag_test.go docs/production-deferred-work.md
git commit -m "docs: freeze nvme event storage contracts -Codex Automated"
```

### Task 2: Lexer Keywords For Durable Declarations

**Files:**

- Modify: `compiler/lex/token.go`
- Modify: `compiler/lex/lexer_test.go`

**Description:** Add only the top-level keywords needed to enter durable declaration parsing. Keep `id`, `layout`, `current`, and `upcast` contextual so ordinary field and method names are not broken.

**Acceptance Criteria:**

- `event` tokenizes as `KeywordEvent`.
- `projection` tokenizes as `KeywordProjection`.
- `layout`, `current`, `upcast`, and `id` continue to tokenize as identifiers.
- All existing lexer tests pass.

**Code Examples:**

Add this test to `compiler/lex/lexer_test.go`:

```go
func TestStorageDeclarationKeywords(t *testing.T) {
	toks, diags := All("event FileCreated id 1001 { layout 1 current {} }\nprojection DirectoryChildren id 12 {}")
	if len(diags) != 0 {
		t.Fatalf("lex diagnostics: %#v", diags)
	}
	if toks[0].Kind != KeywordEvent || toks[0].Text != "event" {
		t.Fatalf("first token = %#v, want KeywordEvent", toks[0])
	}
	if toks[3].Kind != Identifier || toks[3].Text != "id" {
		t.Fatalf("id must remain contextual identifier, got %#v", toks[3])
	}
	if toks[6].Kind != Identifier || toks[6].Text != "layout" {
		t.Fatalf("layout must remain contextual identifier, got %#v", toks[6])
	}
	foundProjection := false
	for _, tok := range toks {
		if tok.Kind == KeywordProjection {
			foundProjection = true
		}
	}
	if !foundProjection {
		t.Fatalf("projection keyword not found in %#v", toks)
	}
}
```

Add token constants after `KeywordStaticAssert`:

```go
	KeywordEvent
	KeywordProjection
```

Add keyword map entries:

```go
	"event":        KeywordEvent,
	"projection":   KeywordProjection,
```

Verification:

```bash
go test ./compiler/lex -run TestStorageDeclarationKeywords -v
go test ./compiler/lex -v
git diff --check
```

Expected: all tests pass after implementation.

Commit:

```bash
git add compiler/lex/token.go compiler/lex/lexer_test.go
git commit -m "feat: tokenize storage declaration keywords -Codex Automated"
```

### Task 3: AST Contracts For Events And Projections

**Files:**

- Modify: `compiler/ast/ast.go`
- Modify: `compiler/ast/ast_test.go`

**Description:** Add durable ABI declaration nodes. The AST stores source IDs as strings so semantic checking can produce precise diagnostics for overflow, zero IDs, and duplicate IDs.

**Acceptance Criteria:**

- `EventDecl` and `ProjectionDecl` implement `ast.Decl`.
- Event layouts preserve field encode expressions.
- Projection layouts preserve container fields.
- Upcast mappings preserve `from -> to` names.
- Debug output is deterministic and includes IDs, current markers, and upcasts.

**Code Examples:**

Add these AST types to `compiler/ast/ast.go`:

```go
type EventDecl struct {
	Name    string
	ID      string
	Fields  []Field
	Layouts []EventLayoutDecl
	Upcasts []LayoutUpcastDecl
	SpanV   source.Span
}

func (d *EventDecl) Span() source.Span { return d.SpanV }

type EventLayoutDecl struct {
	ID      string
	Current bool
	Fields  []EventLayoutField
	Span    source.Span
}

type EventLayoutField struct {
	Name   string
	Type   TypeRef
	Encode Expr
	Span   source.Span
}

type LayoutUpcastDecl struct {
	FromID   string
	ToID     string
	Mappings []LayoutUpcastMapping
	Span     source.Span
}

type LayoutUpcastMapping struct {
	From string
	To   string
	Span source.Span
}

type ProjectionDecl struct {
	Name    string
	ID      string
	Layouts []ProjectionLayoutDecl
	Upcasts []LayoutUpcastDecl
	SpanV   source.Span
}

func (d *ProjectionDecl) Span() source.Span { return d.SpanV }

type ProjectionLayoutDecl struct {
	ID      string
	Current bool
	Fields  []Field
	Span    source.Span
}
```

Add deterministic debug helpers:

```go
func DebugDecl(decl Decl) string {
	switch d := decl.(type) {
	case *EventDecl:
		return "event " + d.Name + " id " + d.ID
	case *ProjectionDecl:
		return "projection " + d.Name + " id " + d.ID
	default:
		return "<decl>"
	}
}
```

Add this test to `compiler/ast/ast_test.go`:

```go
func TestStorageASTContracts(t *testing.T) {
	event := &EventDecl{
		Name: "FileRenamed",
		ID:   "17",
		Fields: []Field{
			{Name: "file_id", Type: TypeRef{Name: "FileId"}},
		},
		Layouts: []EventLayoutDecl{{
			ID:      "1",
			Current: true,
			Fields: []EventLayoutField{{
				Name:   "file_id",
				Type:   TypeRef{Name: "U64"},
				Encode: &FieldExpr{Base: &NameExpr{Name: "self"}, Field: "file_id"},
			}},
		}},
	}
	if event.Span().End != 0 {
		t.Fatalf("zero-value span contract changed")
	}
	if got, want := DebugDecl(event), "event FileRenamed id 17"; got != want {
		t.Fatalf("DebugDecl() = %q, want %q", got, want)
	}

	projection := &ProjectionDecl{
		Name: "DirectoryChildren",
		ID:   "12",
		Layouts: []ProjectionLayoutDecl{{
			ID:      "1",
			Current: true,
			Fields: []Field{{Name: "children", Type: TypeRef{Name: "OrderedPages", Args: []TypeRef{{Name: "FileId"}, {Name: "FileNameKey"}, {Name: "DirectoryChild"}}}}},
		}},
	}
	if got, want := DebugDecl(projection), "projection DirectoryChildren id 12"; got != want {
		t.Fatalf("DebugDecl() = %q, want %q", got, want)
	}
}
```

Verification:

```bash
go test ./compiler/ast -run TestStorageASTContracts -v
go test ./compiler/ast -v
git diff --check
```

Expected: all tests pass after implementation.

Commit:

```bash
git add compiler/ast/ast.go compiler/ast/ast_test.go
git commit -m "feat: add storage declaration ast contracts -Codex Automated"
```

### Task 4: Parser For Event And Projection Declarations

**Files:**

- Modify: `compiler/parse/parser.go`
- Modify: `compiler/parse/parser_test.go`

**Description:** Parse durable event and projection ABI declarations. Use contextual identifiers for `id`, `layout`, `current`, and `upcast`.

**Acceptance Criteria:**

- `event Name id N { layout 1 current {} }` parses at module scope.
- `projection Name id N { layout 1 current {} }` parses at module scope.
- Event top-level fields use existing field syntax.
- Event layout fields allow `name: Type` and `name: Type = expr`.
- Projection layout fields use existing field syntax and normal generic type refs.
- `upcast 1 -> 2 { old_name -> new_name }` parses for events and projections.
- Parser rejects module-scope `layout` and `upcast` outside an event/projection with `PAR0002`.

**Code Examples:**

Add parser switch cases:

```go
	case lex.KeywordEvent:
		if unique {
			return nil, p.err(p.peek(), diag.PAR0002, "event may not be unique")
		}
		return p.parseEventDecl()
	case lex.KeywordProjection:
		if unique {
			return nil, p.err(p.peek(), diag.PAR0002, "projection may not be unique")
		}
		return p.parseProjectionDecl()
```

Add contextual helper:

```go
func (p *Parser) consumeContextualIdentifier(text string, message string) (lex.Token, []diag.Diagnostic) {
	tok := p.peek()
	if tok.Kind != lex.Identifier || tok.Text != text {
		return tok, p.err(tok, diag.PAR0001, message)
	}
	p.next()
	return tok, nil
}
```

The event parser must follow this shape:

```go
func (p *Parser) parseEventDecl() (ast.Decl, []diag.Diagnostic) {
	start := p.next()
	name, ds := p.expectIdentifier("expected event name")
	if len(ds) != 0 {
		return nil, ds
	}
	if _, ds := p.consumeContextualIdentifier("id", "expected id after event name"); len(ds) != 0 {
		return nil, ds
	}
	idTok, ds := p.consume(lex.Integer)
	if len(ds) != 0 {
		return nil, ds
	}
	if _, ds := p.consume(lex.LBrace); len(ds) != 0 {
		return nil, ds
	}

	var fields []ast.Field
	var layouts []ast.EventLayoutDecl
	var upcasts []ast.LayoutUpcastDecl
	for p.peek().Kind != lex.RBrace && p.peek().Kind != lex.EOF {
		p.skipSeparators()
		if p.peek().Kind == lex.RBrace {
			break
		}
		if p.peek().Kind == lex.Identifier && p.peek().Text == "layout" {
			layout, layoutDs := p.parseEventLayoutDecl()
			if len(layoutDs) != 0 {
				return nil, layoutDs
			}
			layouts = append(layouts, layout)
			continue
		}
		if p.peek().Kind == lex.Identifier && p.peek().Text == "upcast" {
			upcast, upcastDs := p.parseLayoutUpcastDecl()
			if len(upcastDs) != 0 {
				return nil, upcastDs
			}
			upcasts = append(upcasts, upcast)
			continue
		}
		field, fieldDs := p.parseField()
		if len(fieldDs) != 0 {
			return nil, fieldDs
		}
		fields = append(fields, field)
	}
	if _, ds := p.consume(lex.RBrace); len(ds) != 0 {
		return nil, ds
	}
	return &ast.EventDecl{Name: name.Text, ID: idTok.Text, Fields: fields, Layouts: layouts, Upcasts: upcasts, SpanV: p.span(start.Start, p.previous().End)}, nil
}
```

Add this parser test:

```go
func TestParseEventDeclaration(t *testing.T) {
	mod := parseModuleOK(t, `
module storage.test
event FileRenamed id 17 {
    file_id: FileId
    directory_id: FileId
    name_ref: BlobRef

    layout 1 {
        file_id: U64
        old_name_ref: BlobRefPayload
    }

    layout 2 current {
        file_id: U64 = self.file_id.value
        directory_id: U64 = self.directory_id.value
        name_ref: BlobRefPayload = self.name_ref.payload
    }

    upcast 1 -> 2 {
        old_name_ref -> name_ref
    }
}`)
	if len(mod.Decls) != 1 {
		t.Fatalf("decls = %d, want 1", len(mod.Decls))
	}
	ev, ok := mod.Decls[0].(*ast.EventDecl)
	if !ok {
		t.Fatalf("decl = %#v, want EventDecl", mod.Decls[0])
	}
	if ev.ID != "17" || len(ev.Layouts) != 2 || !ev.Layouts[1].Current || len(ev.Upcasts) != 1 {
		t.Fatalf("event parsed incorrectly: %#v", ev)
	}
}
```

Add projection parser test:

```go
func TestParseProjectionDeclaration(t *testing.T) {
	mod := parseModuleOK(t, `
module storage.test
projection DirectoryChildren id 12 {
    layout 1 current {
        children: OrderedPages<FileId, FileNameKey, DirectoryChild>
    }
}`)
	proj, ok := mod.Decls[0].(*ast.ProjectionDecl)
	if !ok {
		t.Fatalf("decl = %#v, want ProjectionDecl", mod.Decls[0])
	}
	if proj.ID != "12" || len(proj.Layouts) != 1 || !proj.Layouts[0].Current {
		t.Fatalf("projection parsed incorrectly: %#v", proj)
	}
}
```

Verification:

```bash
go test ./compiler/parse -run 'TestParse(Event|Projection)Declaration' -v
go test ./compiler/parse -v
git diff --check
```

Expected: all tests pass after implementation.

Commit:

```bash
git add compiler/parse/parser.go compiler/parse/parser_test.go
git commit -m "feat: parse durable storage declarations -Codex Automated"
```

---

## 5. Phase 2: Semantic And IR Support For Durable Layouts

**Description:** This phase validates durable ABI declarations and exposes stable metadata to IR/codegen. Storage sees compact IDs and bytes; typed user code gets generated constants, encoders, decoders, and upcasts.

**Acceptance Criteria:**

- Duplicate event IDs and projection IDs fail.
- Event ID zero is rejected because `event_type_id = 0` is reserved for empty slots.
- Event layout IDs are nonzero and unique within the event.
- Multiple layouts require exactly one `current`.
- A single layout without `current` becomes current.
- Upcast endpoints must name existing layouts.
- Current layout encode expressions typecheck against `self`.
- Projection containers are limited to `StateCell<T>`, `DenseEntityMap<Id, T>`, and `OrderedPages<Partition, SortKey, Row>`.
- IR contains event/projection metadata with stable ordering.

**Phase Code Example:**

```go
type EventLayoutInfo struct {
	Module          string
	Name            string
	EventTypeID     uint64
	LayoutID        uint64
	Current         bool
	PayloadSize     uint64
	PayloadAlign    uint64
	EncoderSymbol   string
	DecoderSymbol   string
	UpcastToCurrent string
}
```

### Task 5: Semantic Validation For Event And Projection ABI

**Files:**

- Create: `compiler/sem/storage.go`
- Create: `compiler/sem/storage_test.go`
- Create: `compiler/sem/storage_negative_test.go`
- Add fixtures under: `tests/fixtures/negative/`
- Modify: `compiler/sem/types.go`
- Modify: `compiler/sem/check.go`

**Description:** Add semantic storage declarations without changing ordinary data/class semantics. Events become `KindEvent`; projections become `KindProjection`.

**Acceptance Criteria:**

- `sem.KindEvent` and `sem.KindProjection` exist.
- `CheckedProgram` exposes `Storage StorageIndex`.
- Duplicate event IDs emit `SEM0099`.
- Event ID `0` emits `SEM0100`.
- Duplicate layout IDs emit `SEM0102`.
- Missing current layout in a multi-layout event emits `SEM0101`.
- Duplicate projection IDs emit `SEM0106`.
- Unsupported projection container emits `SEM0108`.
- Negative fixtures fail with the exact expected codes.

**Code Examples:**

Add kinds:

```go
const (
	KindPrimitive Kind = iota
	KindData
	KindClass
	KindDriver
	KindDriverPath
	KindExecutor
	KindImage
	KindEnum
	KindTrait
	KindTypeParam
	KindEvent
	KindProjection
)
```

Add storage index structs:

```go
type StorageIndex struct {
	EventsByID      map[uint64]EventInfo
	EventsByKey     map[string]EventInfo
	ProjectionsByID map[uint64]ProjectionInfo
	ProjectionsByKey map[string]ProjectionInfo
}

type EventInfo struct {
	Module          string
	Name            string
	ID              uint64
	Fields          []Field
	Layouts         []EventLayoutInfo
	CurrentLayoutID uint64
	Upcasts         []LayoutUpcastInfo
	Span            source.Span
}

type EventLayoutInfo struct {
	ID      uint64
	Current bool
	Fields  []EventLayoutFieldInfo
	Span    source.Span
}

type EventLayoutFieldInfo struct {
	Name   string
	Type   *Type
	Encode ast.Expr
	Span   source.Span
}

type LayoutUpcastInfo struct {
	FromID   uint64
	ToID     uint64
	Mappings []LayoutUpcastMappingInfo
	Span     source.Span
}

type LayoutUpcastMappingInfo struct {
	From string
	To   string
	Span source.Span
}

type ProjectionInfo struct {
	Module          string
	Name            string
	ID              uint64
	Layouts         []ProjectionLayoutInfo
	CurrentLayoutID uint64
	Upcasts         []LayoutUpcastInfo
	Span            source.Span
}

type ProjectionLayoutInfo struct {
	ID      uint64
	Current bool
	Fields  []Field
	Span    source.Span
}
```

Add test source:

```go
const validStorageDeclarations = `
module storage.valid
data FileId { value: U64 }
data BlobRefPayload { blob_id: U64; byte_length: U64 }
data BlobRef { payload: BlobRefPayload }
data FileNameKey { hash: U64 }
data DirectoryChild { file_id: FileId; name: BlobRefPayload }
data StateCell<T> { value: T }
data DenseEntityMap<Id, T> { root_ref: U64 }
data OrderedPages<Partition, SortKey, Row> { root_ref: U64 }

event FileRenamed id 17 {
    file_id: FileId
    directory_id: FileId
    name_ref: BlobRef

    layout 1 current {
        file_id: U64 = self.file_id.value
        directory_id: U64 = self.directory_id.value
        name_ref: BlobRefPayload = self.name_ref.payload
    }
}

projection DirectoryChildren id 12 {
    layout 1 current {
        children: OrderedPages<FileId, FileNameKey, DirectoryChild>
    }
}`
```

Add positive test:

```go
func TestEventLayoutSemanticContracts(t *testing.T) {
	checked, ds := checkSource(t, validStorageDeclarations)
	if len(ds) != 0 {
		t.Fatalf("diagnostics: %#v", ds)
	}
	ev := checked.Storage.EventsByID[17]
	if ev.Name != "FileRenamed" || ev.CurrentLayoutID != 1 {
		t.Fatalf("event info = %#v", ev)
	}
	proj := checked.Storage.ProjectionsByID[12]
	if proj.Name != "DirectoryChildren" || proj.CurrentLayoutID != 1 {
		t.Fatalf("projection info = %#v", proj)
	}
}
```

Add negative fixture `tests/fixtures/negative/duplicate_event_id.wrela`:

```wrela
// expect: SEM0099: duplicate durable event type id 17
module negative.duplicate_event_id
event A id 17 { layout 1 current {} }
event B id 17 { layout 1 current {} }
```

Add negative fixture `tests/fixtures/negative/unsupported_projection_container.wrela`:

```wrela
// expect: SEM0108: projection Unsupported layout 1 field bad uses unsupported container HashMap
module negative.unsupported_projection_container
data HashMap<K, V> { root: U64 }
data FileId { value: U64 }
data Row { value: U64 }
projection Unsupported id 9 {
    layout 1 current {
        bad: HashMap<FileId, Row>
    }
}
```

Verification:

```bash
go test ./compiler/sem -run 'TestEventLayoutSemanticContracts|TestNegativeFixtures' -v
go test ./compiler/sem -v
git diff --check
```

Expected: all semantic tests pass after implementation.

Commit:

```bash
git add compiler/sem/storage.go compiler/sem/storage_test.go compiler/sem/storage_negative_test.go compiler/sem/types.go compiler/sem/check.go tests/fixtures/negative/duplicate_event_id.wrela tests/fixtures/negative/unsupported_projection_container.wrela
git commit -m "feat: validate durable storage declarations -Codex Automated"
```

### Task 6: IR Metadata And Generated Event Encoders

**Files:**

- Modify: `compiler/ir/ir.go`
- Modify: `compiler/ir/lower.go`
- Create: `compiler/ir/storage_test.go`
- Modify: `compiler/codegen/x64.go`
- Create: `compiler/codegen/storage_test.go`

**Description:** Lower semantic event/projection metadata into IR and emit compiler-generated encoder functions for current event layouts. Generated encoders write the 64-byte header and the current payload layout into caller-provided slot bytes without reflection, maps, or heap allocation.

**Acceptance Criteria:**

- `ir.Program` includes `StorageEvents []EventLayout` and `StorageProjections []ProjectionLayout`.
- Event metadata ordering is deterministic by `(module, name, layout_id)`.
- Each current event layout gets an encoder symbol named `_wrela_event_encode_<module>_<event>_layout_<id>`.
- Codegen emits stores for event header offsets defined in Section 1.
- Payload writes begin at offset `64`.
- Payload size greater than `448` emits `SEM0121`.
- Codegen tests prove event type ID and payload layout ID immediates are present in generated bytes.

**Code Examples:**

Add IR structs:

```go
type EventLayout struct {
	Module        string
	Name          string
	EventTypeID   uint64
	LayoutID      uint64
	Current       bool
	PayloadSize   uint64
	PayloadAlign  uint64
	EncoderSymbol string
	DecoderSymbol string
}

type ProjectionLayout struct {
	Module       string
	Name         string
	ProjectionID uint64
	LayoutID     uint64
	Current      bool
	Containers   []ProjectionContainer
}

type ProjectionContainer struct {
	Name string
	Kind string
	Type Type
}

type StorageSlotStore struct {
	Slot   Value
	Offset uint64
	Value  Value
	Type   Type
}

func (*StorageSlotStore) isOperation() {}
```

Add to `Program`:

```go
type Program struct {
	Entry              EntryAdapter
	Functions          []Function
	AsmMethods         []AsmMethod
	Types              map[string]TypeInfo
	StorageEvents      []EventLayout
	StorageProjections []ProjectionLayout
}
```

Lowering example:

```go
func (ctx *lowerContext) lowerStorageEvents() {
	events := make([]sem.EventInfo, 0, len(ctx.checked.Storage.EventsByKey))
	for _, event := range ctx.checked.Storage.EventsByKey {
		events = append(events, event)
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].Module != events[j].Module {
			return events[i].Module < events[j].Module
		}
		return events[i].Name < events[j].Name
	})
	for _, event := range events {
		for _, layout := range event.Layouts {
			symbol := symbolName("event_encode", event.Module, event.Name, "layout_"+strconv.FormatUint(layout.ID, 10))
			ctx.program.StorageEvents = append(ctx.program.StorageEvents, EventLayout{
				Module: event.Module, Name: event.Name, EventTypeID: event.ID,
				LayoutID: layout.ID, Current: layout.Current, EncoderSymbol: symbol,
			})
			if layout.Current {
				ctx.program.Functions = append(ctx.program.Functions, ctx.lowerEventEncoder(event, layout, symbol))
			}
		}
	}
}
```

Encoder IR body example:

```go
func (ctx *lowerContext) lowerEventEncoder(event sem.EventInfo, layout sem.EventLayoutInfo, symbol string) Function {
	slot := Param{Symbol: "slot", Type: Type{Name: "MutableBytes", Module: "machine.x86_64.executor_memory", Kind: TypeKindData}}
	eventValue := Param{Symbol: "event", Type: Type{Name: event.Name, Module: event.Module, Kind: TypeKindData}}
	eventType := &ConstInt{Symbol: "event_type_id", Value: event.ID, Type: Type{Name: "U64", Kind: TypeKindPrimitive}}
	layoutID := &ConstInt{Symbol: "payload_layout_id", Value: layout.ID, Type: Type{Name: "U64", Kind: TypeKindPrimitive}}
	return Function{
		Symbol: symbol,
		Return: Type{Name: "void", Kind: TypeKindPrimitive},
		Params: []Value{slot, eventValue},
		Blocks: []Block{{Label: "entry", Ops: []Operation{
			eventType,
			layoutID,
			&StorageSlotStore{Slot: slot, Offset: 24, Value: eventType, Type: Type{Name: "U32", Kind: TypeKindPrimitive}},
			&StorageSlotStore{Slot: slot, Offset: 28, Value: layoutID, Type: Type{Name: "U32", Kind: TypeKindPrimitive}},
			&Return{},
		}}},
	}
}
```

Add IR test:

```go
func TestLowerStorageEventMetadata(t *testing.T) {
	checked := checkedStorageProgramForTest(t)
	program, ds := Lower(checked)
	if len(ds) != 0 {
		t.Fatalf("lower diagnostics: %#v", ds)
	}
	if len(program.StorageEvents) != 1 {
		t.Fatalf("storage events = %#v, want one", program.StorageEvents)
	}
	ev := program.StorageEvents[0]
	if ev.EventTypeID != 17 || ev.LayoutID != 1 || !ev.Current || ev.EncoderSymbol == "" {
		t.Fatalf("event metadata = %#v", ev)
	}
}
```

Add codegen test:

```go
func TestStorageEventEncoderWritesStableIds(t *testing.T) {
	program := storageEncoderProgramForCodegenTest()
	img, ds := Compile(program)
	if len(ds) != 0 {
		t.Fatalf("Compile diagnostics: %#v", ds)
	}
	unit := findTextUnit(t, img, "_wrela_event_encode_storage_test_FileRenamed_layout_1")
	if !bytes.Contains(unit.Bytes, []byte{0x11, 0x00, 0x00, 0x00}) {
		t.Fatalf("encoder must contain event_type_id 17 immediate: %#x", unit.Bytes)
	}
	if !bytes.Contains(unit.Bytes, []byte{0x01, 0x00, 0x00, 0x00}) {
		t.Fatalf("encoder must contain layout id 1 immediate: %#x", unit.Bytes)
	}
}
```

Verification:

```bash
go test ./compiler/ir -run TestLowerStorageEventMetadata -v
go test ./compiler/codegen -run TestStorageEventEncoderWritesStableIds -v
go test ./compiler/ir ./compiler/codegen -v
git diff --check
```

Expected: all listed tests pass after implementation.

Commit:

```bash
git add compiler/ir/ir.go compiler/ir/lower.go compiler/ir/storage_test.go compiler/codegen/x64.go compiler/codegen/storage_test.go
git commit -m "feat: lower durable event metadata -Codex Automated"
```

---

## 6. Phase 3: Paired-Core Link And NVMe Driver

**Description:** This phase adds the permanent foreground/maintenance communication primitive and the minimal interrupt-driven NVMe driver. The event store consumes `NvmeIoPath` capabilities; it never exposes a generic ambient block device API.

**Acceptance Criteria:**

- Core links are SPSC descriptor pairs with explicit endpoint ownership and wake metadata.
- NVMe claims class `0x01`, subclass `0x08`, programming interface `0x02`.
- Foreground and background storage paths each own one NVMe IO queue pair.
- Completions are interrupt-driven through driver path interrupt receivers.
- Identify parsing chooses namespace mode, active LBA size, durability mode, and queue features.

**Phase Code Example:**

```wrela
let foreground_path = nvme.create_io_path(
    identity = PathIdentity(label = "storage.foreground"),
    owner = foreground_slot,
    role = NvmePathRole(role = NVME_PATH_FOREGROUND),
    route = foreground_route,
    irq = foreground_completion_topic.publisher()
)
```

### Task 7: CoreLink Source And Ownership Checks

**Files:**

- Create: `wrela/machine/x86_64/core_link.wrela`
- Create: `compiler/sem/core_link_test.go`
- Modify: `compiler/sem/image_graph.go`
- Modify: `compiler/sem/check.go`
- Modify: `compiler/sem/report.go`
- Modify: `compiler/report/report.go`
- Modify: `compiler/report/report_test.go`

**Description:** Add a typed paired-core link for foreground/maintenance descriptor passing. This is not a broadcast topic. Each direction has one producer and one consumer, and both endpoints are boot-wired to executor slots.

**Acceptance Criteria:**

- `CoreLink<A, B>` exposes `a_to_b_producer`, `a_to_b_consumer`, `b_to_a_producer`, and `b_to_a_consumer`.
- Endpoint construction records owner slot labels in the image graph.
- Using one producer from two executors emits `SEM0112`.
- Using one consumer from the wrong executor emits `SEM0112`.
- Report output includes core-link labels, endpoint owners, depths, and wake strategies.

**Code Examples:**

Create `wrela/machine/x86_64/core_link.wrela`:

```wrela
module machine.x86_64.core_link

use { Option, Result, Unit } from wrela.lang.core
use { ExecutorSlot } from machine.x86_64.executor_slot
use { WakeStrategy } from machine.x86_64.executor_loop
use { Slots } from machine.x86_64.executor_memory

data CoreLinkIdentity {
    label: StringLiteral
}

data CoreLinkEndpointIdentity {
    label: StringLiteral
}

data CoreLinkFull {}

class CoreSpscProducer<T> {
    identity: CoreLinkEndpointIdentity
    owner: ExecutorSlot
    peer: ExecutorSlot
    slots: Slots<T>
    capacity: U64
    head: U64
    tail: U64
    credits: U64
    wake_strategy: WakeStrategy

    asm fn try_send(self, value: T) -> Result<Unit, CoreLinkFull> {
        ret
    }
}

class CoreSpscConsumer<T> {
    identity: CoreLinkEndpointIdentity
    owner: ExecutorSlot
    peer: ExecutorSlot
    slots: Slots<T>
    capacity: U64
    head: U64
    tail: U64
    wait_armed: Bool
    wake_strategy: WakeStrategy

    asm fn try_recv(self) -> Option<T> {
        ret
    }

    fn arm_wait(self) {
        self.wait_armed = true
    }

    fn is_wait_armed(self) -> Bool {
        return self.wait_armed
    }
}

data CoreLink<A, B> {
    identity: CoreLinkIdentity
    a_slot: ExecutorSlot
    b_slot: ExecutorSlot
    a_to_b_producer: CoreSpscProducer<A>
    a_to_b_consumer: CoreSpscConsumer<A>
    b_to_a_producer: CoreSpscProducer<B>
    b_to_a_consumer: CoreSpscConsumer<B>
}
```

Add graph node:

```go
type CoreLinkEndpointNode struct {
	Label     string
	Direction string
	Role      string
	Owner     string
	Peer      string
	Depth     uint64
	Span      source.Span
}
```

Add semantic test:

```go
func TestCoreLinkEndpointsRecordOwners(t *testing.T) {
	checked, ds := checkUEFIModulesWithExtraSource(t, "core-link-storage.wrela", `
module examples.core_link_storage
use { CoreLinkIdentity, CoreLinkEndpointIdentity, CoreSpscProducer, CoreSpscConsumer } from machine.x86_64.core_link
use { ExecutorSlot } from machine.x86_64.executor_slot
use { WakeStrategy } from machine.x86_64.executor_loop
use { Slots } from machine.x86_64.executor_memory
data CommittedAtomicGroup { first_event_id: U64; last_event_id: U64 }
data MaintenanceProposal { kind: U64 }
data Wiring {
    fg: CoreSpscProducer<CommittedAtomicGroup>
    bg: CoreSpscConsumer<CommittedAtomicGroup>
}
`)
	if len(ds) != 0 {
		t.Fatalf("diagnostics: %#v", ds)
	}
	if checked == nil {
		t.Fatalf("checked program must not be nil")
	}
}
```

Verification:

```bash
go test ./compiler/sem -run TestCoreLinkEndpointsRecordOwners -v
go test ./compiler/report -run CoreLink -v
go test ./compiler/sem ./compiler/report -v
git diff --check
```

Expected: all listed tests pass after implementation.

Commit:

```bash
git add wrela/machine/x86_64/core_link.wrela compiler/sem/core_link_test.go compiler/sem/image_graph.go compiler/sem/check.go compiler/sem/report.go compiler/report/report.go compiler/report/report_test.go
git commit -m "feat: add paired core storage links -Codex Automated"
```

### Task 8: NVMe Source Surface And PCI Claim Shape

**Files:**

- Create: `wrela/machine/x86_64/nvme.wrela`
- Create: `compiler/sem/nvme_contract_test.go`
- Modify: `compiler/sem/check.go`
- Modify: `compiler/sem/image_graph.go`
- Modify: `compiler/sem/report.go`

**Description:** Add the NVMe driver and driver path source surface. The driver claims a PCI NVMe function and returns path capabilities for foreground and background storage IO.

**Acceptance Criteria:**

- `NvmeDriver.initialize(device: PciDevice) -> NvmeDriver` exists.
- `NvmeDriver.create_io_path` returns `NvmeIoPath`.
- `NvmeIoPath` is a `driver path` with an `interrupt receiver -> NvmeCompletionInterrupt`.
- `NvmeNamespace` stores namespace ID, LBA size, block count, ZNS support, FUA support, and atomic write unit fields.
- Source contains class match constants for NVMe PCI: base class `0x01`, subclass `0x08`, prog-if `0x02`.
- Semantic graph records hardware claim key `pci:nvme:<segment>:<bus>:<device>:<function>`.

**Code Examples:**

Create the opening of `wrela/machine/x86_64/nvme.wrela`:

```wrela
module machine.x86_64.nvme

use { DmaBuffer } from platform.hardware.memory
use { MmioRegion } from platform.hardware.bytes
use { BootPanic } from platform.hardware.panic
use { PciDevice, PciInterruptRoute } from machine.x86_64.pci
use { PathIdentity, DriverMemory } from machine.x86_64.cpu_state
use { ExecutorSlot } from machine.x86_64.executor_slot
use { TopicPublisher } from machine.x86_64.topic
use { Result, Unit } from wrela.lang.core

const NVME_CLASS_MASS_STORAGE: U64 = 0x01
const NVME_SUBCLASS_NVM: U64 = 0x08
const NVME_PROGIF_EXPRESS: U64 = 0x02
const NVME_PATH_FOREGROUND: U64 = 1
const NVME_PATH_BACKGROUND: U64 = 2

data NvmeNamespace {
    namespace_id: U32
    logical_block_size: U64
    block_count: U64
    zone_size_blocks: U64
    supports_zns: Bool
    supports_fua: Bool
    atomic_write_unit_blocks: U32
    power_fail_atomic_write_unit_blocks: U32
    volatile_write_cache: Bool
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
```

Add driver/path shape:

```wrela
data NvmeControllerRegisters {
    mmio: MmioRegion
    panic: BootPanic

    fn cap_low(self) -> U32 { return self.mmio.read32(offset = 0x00) }
    fn cap_high(self) -> U32 { return self.mmio.read32(offset = 0x04) }
    fn version(self) -> U32 { return self.mmio.read32(offset = 0x08) }
    fn controller_config(self) -> U32 { return self.mmio.read32(offset = 0x14) }
    fn write_controller_config(self, value: U32) { self.mmio.write32(offset = 0x14, value = value) }
    fn controller_status(self) -> U32 { return self.mmio.read32(offset = 0x1C) }
    fn write_admin_queue_attrs(self, value: U32) { self.mmio.write32(offset = 0x24, value = value) }
    fn write_admin_submission_queue(self, low: U32, high: U32) {
        self.mmio.write32(offset = 0x28, value = low)
        self.mmio.write32(offset = 0x2C, value = high)
    }
    fn write_admin_completion_queue(self, low: U32, high: U32) {
        self.mmio.write32(offset = 0x30, value = low)
        self.mmio.write32(offset = 0x34, value = high)
    }
    fn ring_submission_doorbell(self, queue_id: U16, tail: U16) {
        self.mmio.write32(offset = 0x1000 + (queue_id << 3), value = tail)
    }
    fn ring_completion_doorbell(self, queue_id: U16, head: U16) {
        self.mmio.write32(offset = 0x1000 + (queue_id << 3) + 4, value = head)
    }
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
    namespace: NvmeNamespace
    route: PciInterruptRoute
    irq: TopicPublisher<NvmeCompletionInterrupt>

    interrupt receiver -> NvmeCompletionInterrupt

    fn submit_read(self, namespace_id: U32, start_lba: U64, block_count: U64, into: DmaBuffer<U8>) -> NvmeSubmission
    fn submit_write(self, namespace_id: U32, start_lba: U64, block_count: U64, from: DmaBuffer<U8>, fua: Bool) -> NvmeSubmission
    fn submit_flush(self, namespace_id: U32) -> NvmeSubmission
    fn submit_zone_append(self, namespace_id: U32, zone_start_lba: U64, block_count: U64, from: DmaBuffer<U8>) -> NvmeSubmission
    fn ack_completed(self, event: NvmeCompletionInterrupt)
}
```

Add semantic source-shape test:

```go
func TestNvmeSourceContract(t *testing.T) {
	modules := parseUEFIModuleSet(t)
	index, ds := BuildIndex(modules)
	if len(ds) != 0 {
		t.Fatalf("build index diagnostics: %#v", ds)
	}
	driver := moduleType(t, index, "machine.x86_64.nvme", "NvmeDriver")
	assertMethodExists(t, driver, "initialize")
	assertMethodExists(t, driver, "create_io_path")
	path := moduleType(t, index, "machine.x86_64.nvme", "NvmeIoPath")
	assertMethodExists(t, path, "submit_read")
	assertMethodExists(t, path, "submit_write")
	assertMethodExists(t, path, "submit_flush")
	assertMethodExists(t, path, "submit_zone_append")
	ns := moduleType(t, index, "machine.x86_64.nvme", "NvmeNamespace")
	for _, field := range []string{"logical_block_size", "supports_zns", "supports_fua", "power_fail_atomic_write_unit_blocks", "volatile_write_cache"} {
		_ = fieldTypeName(t, ns, field)
	}
	source := readRepoFile(t, "wrela/machine/x86_64/nvme.wrela")
	for _, want := range []string{"0x01", "0x08", "0x02", "interrupt receiver -> NvmeCompletionInterrupt"} {
		if !strings.Contains(source, want) {
			t.Fatalf("nvme source missing %q", want)
		}
	}
}
```

Verification:

```bash
go test ./compiler/sem -run TestNvmeSourceContract -v
go test ./compiler/sem -v
git diff --check
```

Expected: all listed tests pass after implementation.

Commit:

```bash
git add wrela/machine/x86_64/nvme.wrela compiler/sem/nvme_contract_test.go compiler/sem/check.go compiler/sem/image_graph.go compiler/sem/report.go
git commit -m "feat: add nvme driver source contract -Codex Automated"
```

### Task 9: NVMe Identify Parsing And Durability Mode Selection

**Files:**

- Modify: `wrela/machine/x86_64/nvme.wrela`
- Create: `compiler/sem/nvme_identify_test.go`
- Create: `compiler/ir/nvme_test.go`

**Description:** Implement the minimum controller initialization metadata path: controller reset/ready waits, admin queue setup shape, Identify Controller, Identify Namespace, active LBA size parsing, namespace mode parsing, and durability mode selection.

**Acceptance Criteria:**

- Active LBA size 512 selects `NVME_LBA_SIZE_512`.
- Active LBA size 4096 selects `NVME_LBA_SIZE_4096`.
- Any active LBA size below 512 or not divisible by 512 records unsupported v1 storage and maps to `SEM0122` in compile-time source checks or boot panic in runtime paths.
- Volatile write cache plus no FUA selects write-plus-flush mode.
- FUA support selects FUA mode.
- Power-fail atomic write unit large enough for the batch is recorded separately from FUA/flush.
- Identify parsing does not poll completion queues for command completion outside bounded controller-ready waits.

**Code Examples:**

Add durability constants:

```wrela
const NVME_DURABILITY_WRITE_PLUS_FLUSH: U64 = 1
const NVME_DURABILITY_FUA: U64 = 2
const NVME_DURABILITY_PFAIL_ATOMIC_FUA: U64 = 3
const NVME_NAMESPACE_CONVENTIONAL: U64 = 1
const NVME_NAMESPACE_ZONED: U64 = 2
const NVME_LBA_SIZE_512: U64 = 512
const NVME_LBA_SIZE_4096: U64 = 4096

data NvmeDurabilityMode {
    mode: U64
    requires_flush: Bool
    use_fua: Bool
    power_fail_atomic_write_unit_blocks: U32
}

data NvmeIdentifyFacts {
    namespace: NvmeNamespace
    durability: NvmeDurabilityMode
    namespace_mode: U64
}
```

Add mode selection:

```wrela
data NvmeDurabilitySelector {
    panic: BootPanic

    fn choose(self, namespace: NvmeNamespace, target_batch_blocks: U32) -> NvmeDurabilityMode {
        if namespace.logical_block_size != 512 {
            if namespace.logical_block_size != 4096 {
                self.panic.fail(code = 0xAC080122)
            }
        }
        let pf_atomic = namespace.power_fail_atomic_write_unit_blocks >= target_batch_blocks
        if pf_atomic {
            if namespace.supports_fua {
                return NvmeDurabilityMode(mode = NVME_DURABILITY_PFAIL_ATOMIC_FUA, requires_flush = false, use_fua = true, power_fail_atomic_write_unit_blocks = namespace.power_fail_atomic_write_unit_blocks)
            }
        }
        if namespace.supports_fua {
            return NvmeDurabilityMode(mode = NVME_DURABILITY_FUA, requires_flush = false, use_fua = true, power_fail_atomic_write_unit_blocks = namespace.power_fail_atomic_write_unit_blocks)
        }
        return NvmeDurabilityMode(mode = NVME_DURABILITY_WRITE_PLUS_FLUSH, requires_flush = true, use_fua = false, power_fail_atomic_write_unit_blocks = namespace.power_fail_atomic_write_unit_blocks)
    }
}
```

Add semantic test:

```go
func TestNvmeDurabilityModeSourceShape(t *testing.T) {
	source := readRepoFile(t, "wrela/machine/x86_64/nvme.wrela")
	for _, want := range []string{
		"NVME_DURABILITY_WRITE_PLUS_FLUSH",
		"NVME_DURABILITY_FUA",
		"NVME_DURABILITY_PFAIL_ATOMIC_FUA",
		"namespace.logical_block_size != 512",
		"namespace.logical_block_size != 4096",
		"namespace.supports_fua",
		"requires_flush = true",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("nvme identify source missing %q", want)
		}
	}
}
```

Add IR test for no polling helper:

```go
func TestNvmeCompletionContractHasNoPollingPath(t *testing.T) {
	source := readRepoFile(t, "wrela/machine/x86_64/nvme.wrela")
	for _, forbidden := range []string{"poll_completion", "spin_until_complete", "completion_status_loop"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("nvme source must not contain polling completion helper %q", forbidden)
		}
	}
}
```

Verification:

```bash
go test ./compiler/sem -run TestNvmeDurabilityModeSourceShape -v
go test ./compiler/ir -run TestNvmeCompletionContractHasNoPollingPath -v
go test ./compiler/sem ./compiler/ir -v
git diff --check
```

Expected: all listed tests pass after implementation.

Commit:

```bash
git add wrela/machine/x86_64/nvme.wrela compiler/sem/nvme_identify_test.go compiler/ir/nvme_test.go
git commit -m "feat: select nvme namespace durability mode -Codex Automated"
```

### Task 10: NVMe Queue Pair Submission And Interrupt Completion

**Files:**

- Modify: `wrela/machine/x86_64/nvme.wrela`
- Create: `compiler/codegen/nvme_test.go`
- Modify: `compiler/sem/nvme_contract_test.go`

**Description:** Implement PRP-only bounded queue submission and interrupt-driven completion publication. Larger blob transfers are split by higher storage code into chunks no larger than `131072` bytes.

**Acceptance Criteria:**

- Foreground and background queue pair structs include submission/completion DMA buffers, head/tail, phase bit, queue ID, depth, and next command ID.
- `submit_read`, `submit_write`, `submit_flush`, and `submit_zone_append` allocate command IDs and ring the submission doorbell.
- `submit_write` with `fua = true` sets the NVMe FUA bit in command dword 12.
- Interrupt receiver drains completion entries until phase mismatch, advances completion head, rings completion doorbell, publishes one `NvmeCompletionInterrupt` with count.
- Completion correlation is by `command_id`.
- No code path exposes arbitrary user block writes as application API.

**Code Examples:**

Add queue pair shape:

```wrela
data NvmeQueuePair {
    queue_id: U16
    depth: U16
    submission: DmaBuffer<U8>
    completion: DmaBuffer<U8>
    submission_tail: U16
    completion_head: U16
    completion_phase: Bool
    next_command_id: U16
}

data NvmeCommandStatus {
    command_id: U16
    completed: Bool
    status: U16
    result: U64
}
```

Add command ID helper:

```wrela
data NvmeQueuePairOps {
    panic: BootPanic

    fn next_command(self, queue: NvmeQueuePair) -> NvmeSubmission {
        let id = queue.next_command_id
        queue.next_command_id = queue.next_command_id + 1
        if queue.next_command_id == 0 {
            queue.next_command_id = 1
        }
        return NvmeSubmission(command_id = id)
    }
}
```

Write submission shape:

```wrela
fn submit_write(self, namespace_id: U32, start_lba: U64, block_count: U64, from: DmaBuffer<U8>, fua: Bool) -> NvmeSubmission {
    let submission = NvmeQueuePairOps(panic = self.registers.panic).next_command(queue = self.queue)
    let command_offset = self.queue.submission_tail * 64
    self.write_command_header(offset = command_offset, opcode = 0x01, command_id = submission.command_id, namespace_id = namespace_id)
    self.write_prp(offset = command_offset, buffer = from)
    let flags = block_count - 1
    if fua {
        flags = flags | (1 << 30)
    }
    self.write_command_dword(offset = command_offset, dword = 10, value = start_lba & 0xFFFFFFFF)
    self.write_command_dword(offset = command_offset, dword = 11, value = start_lba >> 32)
    self.write_command_dword(offset = command_offset, dword = 12, value = flags)
    self.queue.submission_tail = (self.queue.submission_tail + 1) % self.queue.depth
    self.registers.ring_submission_doorbell(queue_id = self.queue.queue_id, tail = self.queue.submission_tail)
    return submission
}
```

Interrupt receiver shape:

```wrela
interrupt receiver -> NvmeCompletionInterrupt {
    let completed = self.drain_completion_queue()
    if completed != 0 {
        self.registers.ring_completion_doorbell(queue_id = self.queue.queue_id, head = self.queue.completion_head)
    }
    return NvmeCompletionInterrupt(queue_id = self.queue.queue_id, completed_count = completed)
}
```

Add codegen test:

```go
func TestNvmeSubmitWriteRingsSubmissionDoorbell(t *testing.T) {
	source := readRepoFile(t, "wrela/machine/x86_64/nvme.wrela")
	for _, want := range []string{
		"opcode = 0x01",
		"flags = flags | (1 << 30)",
		"ring_submission_doorbell",
		"ring_completion_doorbell",
		"interrupt receiver -> NvmeCompletionInterrupt",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("nvme submission source missing %q", want)
		}
	}
}
```

Verification:

```bash
go test ./compiler/codegen -run TestNvmeSubmitWriteRingsSubmissionDoorbell -v
go test ./compiler/sem -run TestNvmeSourceContract -v
go test ./compiler/codegen ./compiler/sem -v
git diff --check
```

Expected: all listed tests pass after implementation.

Commit:

```bash
git add wrela/machine/x86_64/nvme.wrela compiler/codegen/nvme_test.go compiler/sem/nvme_contract_test.go
git commit -m "feat: add interrupt driven nvme queue pairs -Codex Automated"
```

### Task 11: Foreground And Background Storage Path Wiring

**Files:**

- Modify: `wrela/machine/x86_64/nvme.wrela`
- Modify: `wrela/platform/hardware/discovery.wrela`
- Create: `compiler/sem/storage_path_test.go`
- Modify: `compiler/sem/check.go`
- Modify: `compiler/sem/report.go`
- Modify: `compiler/report/report.go`

**Description:** Boot images can wire two NVMe paths: foreground for event append and hot reads, background for maintenance IO. The semantic checker enforces that the path owner matches the executor slot using it.

**Acceptance Criteria:**

- `ForegroundStoragePath` and `BackgroundStoragePath` are named role constants or wrappers over `NvmeIoPath`.
- A foreground `StorageWriter` cannot be constructed with a background path.
- A maintenance worker cannot submit maintenance IO through the foreground path.
- Wrong owner slot emits `SEM0111`.
- Report includes both path labels, queue IDs, role, owner slot, and interrupt vector.

**Code Examples:**

Add role wrappers:

```wrela
data ForegroundStoragePath {
    path: NvmeIoPath
}

data BackgroundStoragePath {
    path: NvmeIoPath
}

data NvmeStoragePaths {
    foreground: ForegroundStoragePath
    background: BackgroundStoragePath
}
```

Add construction helpers:

```wrela
fn foreground_storage_path(self, path: NvmeIoPath) -> ForegroundStoragePath {
    if path.role.role != NVME_PATH_FOREGROUND {
        path.registers.panic.fail(code = 0xAC080111)
    }
    return ForegroundStoragePath(path = path)
}

fn background_storage_path(self, path: NvmeIoPath) -> BackgroundStoragePath {
    if path.role.role != NVME_PATH_BACKGROUND {
        path.registers.panic.fail(code = 0xAC080112)
    }
    return BackgroundStoragePath(path = path)
}
```

Add semantic negative fixture `tests/fixtures/negative/storage_wrong_nvme_path_owner.wrela`:

```wrela
// expect: SEM0111: NVMe path storage.foreground is owned by foreground but used by maintenance
module negative.storage_wrong_nvme_path_owner
use { ExecutorSlot } from machine.x86_64.executor_slot
use { ForegroundStoragePath } from machine.x86_64.nvme
executor Maintenance {
    slot: ExecutorSlot
    path: ForegroundStoragePath
    start fn run(self) -> never {
        while true {}
    }
}
```

Add report assertion:

```go
func TestStoragePathsAppearInReport(t *testing.T) {
	report := report.ImageReport{
		Storage: report.StorageReport{
			NvmePaths: []report.NvmePathReport{{
				Label: "storage.foreground",
				Role: "foreground",
				Owner: "foreground",
				QueueID: 1,
				Vector: 0x50,
			}},
		},
	}
	if report.Storage.NvmePaths[0].Role != "foreground" {
		t.Fatalf("storage path report = %#v", report.Storage.NvmePaths[0])
	}
}
```

Verification:

```bash
go test ./compiler/sem -run 'TestStoragePaths|TestNegativeFixtures' -v
go test ./compiler/report -run TestStoragePathsAppearInReport -v
go test ./compiler/sem ./compiler/report -v
git diff --check
```

Expected: all listed tests pass after implementation.

Commit:

```bash
git add wrela/machine/x86_64/nvme.wrela wrela/platform/hardware/discovery.wrela compiler/sem/storage_path_test.go compiler/sem/check.go compiler/sem/report.go compiler/report/report.go tests/fixtures/negative/storage_wrong_nvme_path_owner.wrela
git commit -m "feat: wire foreground and background nvme paths -Codex Automated"
```

---

## 7. Phase 4: Disk Format, Event Log, Streams, And Writer

**Description:** This phase adds the actual event-store storage format and the single-writer append path. The writer acknowledges only after the NVMe durability condition is satisfied.

**Acceptance Criteria:**

- Disk regions are deterministic and non-overlapping.
- Superblocks are double-buffered and checksummed.
- Hot event slots are exactly 512 bytes.
- 4 KiB underfilled LBAs seal unused slots as reserved empty slots.
- Atomic groups are never split across acknowledged durable batches.
- Recovery stops at the first invalid tail and exposes only complete valid atomic groups.
- Stream directory is rebuildable from events.

**Phase Code Example:**

```wrela
let result = writer.append_atomic_group(group = group)
match result {
    StorageAppendResult.Accepted(token = token) -> self.last_commit = token.last_event_id
    StorageAppendResult.Rejected(error = error) -> self.last_error = error.code
}
```

### Task 12: Storage Format Source Contracts

**Files:**

- Create: `wrela/storage/format.wrela`
- Create: `compiler/sem/storage_format_test.go`
- Create: `compiler/ir/storage_format_test.go`

**Description:** Define fixed source-level storage structs and constants for superblocks, regions, event slots, stream directory entries, segment metadata, and metrics.

**Acceptance Criteria:**

- Constants from Section 1 exist in `wrela/storage/format.wrela`.
- `StorageSuperblock` includes store UUID, format version, generation, active namespace mode, active LBA size, region map root, segment map root, atomic group frontier, and checksum.
- `StorageRegionMap` supports at least 16 entries.
- `EventSlotHeader` field names match Section 1.
- `StorageMetrics` contains every metric listed in the design's metrics section that is in v1 scope, including device media writes, estimated drive writes per day, payload utilization, payload overflow count, compressed lookup latency, and projection layout upcast count.
- Source-shape tests verify constants and field names.

**Code Examples:**

Create `wrela/storage/format.wrela`:

```wrela
module storage.format

const STORAGE_FORMAT_VERSION: U64 = 1
const STORAGE_EVENT_SLOT_SIZE: U64 = 512
const STORAGE_EVENT_HEADER_SIZE: U64 = 64
const STORAGE_EVENT_PAYLOAD_BYTES: U64 = 448
const STORAGE_TARGET_BATCH_SLOTS: U64 = 64
const STORAGE_MAX_OVERFLOW_SLOTS: U64 = 8
const STORAGE_MAX_BATCH_SLOTS: U64 = 72
const STORAGE_MAX_ATOMIC_GROUP_SLOTS: U64 = 32
const STORAGE_GROUP_COMMIT_TIMER_US: U64 = 2000
const STORAGE_HOT_SEGMENT_SLOTS: U64 = 1048576
const STORAGE_SLOT_RESERVED_EMPTY: U64 = 1

data StorageUuid {
    low: U64
    high: U64
}

data StorageSuperblock {
    store_uuid: StorageUuid
    format_version: U64
    generation: U64
    active_namespace_mode: U64
    active_lba_size: U64
    region_map_root_lba: U64
    segment_map_root_lba: U64
    atomic_group_frontier: U64
    checksum32: U32
}

data StorageRegionEntry {
    kind: U64
    start_lba: U64
    block_count: U64
    format_version: U64
    flags: U64
    checksum32: U32
}

data StorageRegionMap {
    generation: U64
    entry_count: U64
    entry0: StorageRegionEntry
    entry1: StorageRegionEntry
    entry2: StorageRegionEntry
    entry3: StorageRegionEntry
    entry4: StorageRegionEntry
    entry5: StorageRegionEntry
    entry6: StorageRegionEntry
    entry7: StorageRegionEntry
    entry8: StorageRegionEntry
    entry9: StorageRegionEntry
    entry10: StorageRegionEntry
    entry11: StorageRegionEntry
    entry12: StorageRegionEntry
    entry13: StorageRegionEntry
    entry14: StorageRegionEntry
    entry15: StorageRegionEntry
}

data EventSlotHeader {
    event_id: U64
    stream_id: U64
    stream_sequence: U64
    event_type_id: U32
    payload_layout_id: U32
    atomic_group_len: U32
    atomic_group_index: U32
    payload_length: U32
    flags: U32
    checksum32: U32
    header_version: U16
    reserved16: U16
    reserved64: U64
}

data StreamDirectoryEntry {
    latest_sequence: U64
    latest_event_id: U64
    latest_checkpoint_ref: U64
    flags: U64
}
```

Add metrics struct:

```wrela
data StorageMetrics {
    active_lba_size: U64
    namespace_mode: U64
    durability_mode: U64
    event_slot_size: U64
    atomic_groups_per_second: U64
    events_per_atomic_group: U64
    batch_overflow_slots_used: U64
    rejected_oversized_atomic_groups: U64
    events_per_committed_lba: U64
    sealed_event_block_count: U64
    underfilled_lba_count: U64
    reserved_empty_hot_slots: U64
    bytes_written_per_durable_event: U64
    estimated_drive_writes_per_day: U64
    device_reported_media_writes: U64
    durable_events_per_second: U64
    writer_cpu_cycles_per_event: U64
    group_commit_latency_p50_us: U64
    group_commit_latency_p99_us: U64
    group_commit_latency_p999_us: U64
    append_ack_latency_p50_us: U64
    append_ack_latency_p99_us: U64
    append_ack_latency_p999_us: U64
    payload_utilization_x1000: U64
    payload_overflow_count: U64
    hot_bytes_per_event: U64
    packed_bytes_per_event: U64
    compressed_bytes_per_event: U64
    compression_ratio_x1000: U64
    compression_cpu_time_us: U64
    compressed_segment_lookup_latency_us: U64
    cold_compression_backlog: U64
    foreground_queue_depth: U64
    background_queue_depth: U64
    foreground_completion_latency_us: U64
    background_completion_latency_us: U64
    core_link_queue_depth: U64
    core_link_wake_count: U64
    blob_extent_count_per_file: U64
    orphaned_extent_bytes: U64
    projection_lag_events: U64
    projection_rebuild_time_us: U64
    projection_spsc_depth: U64
    projection_backpressure_count: U64
    projection_layout_upcast_count: U64
    projection_layout_rebuild_count: U64
    stream_directory_cache_hit_rate_x1000: U64
}
```

Add test:

```go
func TestStorageFormatSourceConstants(t *testing.T) {
	source := readRepoFile(t, "wrela/storage/format.wrela")
	for _, want := range []string{
		"STORAGE_EVENT_SLOT_SIZE: U64 = 512",
		"STORAGE_EVENT_HEADER_SIZE: U64 = 64",
		"STORAGE_EVENT_PAYLOAD_BYTES: U64 = 448",
		"atomic_group_frontier",
		"segment_map_root_lba",
		"reserved_empty_hot_slots",
		"projection_lag_events",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("storage format source missing %q", want)
		}
	}
}
```

Verification:

```bash
go test ./compiler/sem -run TestStorageFormatSourceConstants -v
go test ./compiler/ir -run StorageFormat -v
go test ./compiler/sem ./compiler/ir -v
git diff --check
```

Expected: all listed tests pass after implementation.

Commit:

```bash
git add wrela/storage/format.wrela compiler/sem/storage_format_test.go compiler/ir/storage_format_test.go
git commit -m "feat: define nvme storage disk format -Codex Automated"
```

### Task 13: Superblock And Region Map

**Files:**

- Modify: `wrela/storage/format.wrela`
- Create: `wrela/storage/event_log.wrela`
- Create: `compiler/sem/storage_region_test.go`
- Create: `compiler/codegen/storage_region_test.go`

**Description:** Implement deterministic region layout and double-buffered superblock selection. Recovery picks the highest valid superblock generation with a valid checksum.

**Acceptance Criteria:**

- Region layout has fixed order: superblock, hot event slots, segment map, sealed segment extents, stream directory, blob extents, blob manifest/key metadata, projection storage, maintenance metadata.
- Region entries cannot overlap; overlap emits `SEM0110` in compile-time tests for static examples and boot panic for runtime discovery.
- Superblock copy A is at byte 0 and copy B is at byte 4096.
- Highest valid generation wins.
- Invalid checksum copy is ignored.

**Code Examples:**

Add region constants:

```wrela
const STORAGE_REGION_SUPERBLOCK: U64 = 1
const STORAGE_REGION_HOT_EVENT_SLOTS: U64 = 2
const STORAGE_REGION_SEGMENT_MAP: U64 = 3
const STORAGE_REGION_SEALED_SEGMENTS: U64 = 4
const STORAGE_REGION_STREAM_DIRECTORY: U64 = 5
const STORAGE_REGION_BLOB_EXTENTS: U64 = 6
const STORAGE_REGION_BLOB_MANIFESTS: U64 = 7
const STORAGE_REGION_PROJECTION_STORAGE: U64 = 8
const STORAGE_REGION_MAINTENANCE_METADATA: U64 = 9
```

Create region validator:

```wrela
module storage.event_log

use { BootPanic } from platform.hardware.panic
use { StorageRegionEntry, StorageRegionMap, StorageSuperblock } from storage.format

data StorageRegionValidator {
    panic: BootPanic

    fn validate_pair(self, a: StorageRegionEntry, b: StorageRegionEntry) {
        let a_end = a.start_lba + a.block_count
        let b_end = b.start_lba + b.block_count
        if a.block_count == 0 {
            self.panic.fail(code = 0xAC090110)
        }
        if b.block_count == 0 {
            self.panic.fail(code = 0xAC090110)
        }
        if a.start_lba < b_end {
            if b.start_lba < a_end {
                self.panic.fail(code = 0xAC090110)
            }
        }
    }
}
```

Superblock choice:

```wrela
data SuperblockPair {
    a: StorageSuperblock
    b: StorageSuperblock

    fn choose(self) -> StorageSuperblock {
        let a_valid = self.a.format_version == STORAGE_FORMAT_VERSION
        let b_valid = self.b.format_version == STORAGE_FORMAT_VERSION
        if a_valid {
            if b_valid {
                if self.b.generation > self.a.generation {
                    return self.b
                }
            }
            return self.a
        }
        if b_valid {
            return self.b
        }
        return self.a
    }
}
```

Add test:

```go
func TestStorageRegionOrderIsFrozen(t *testing.T) {
	source := readRepoFile(t, "wrela/storage/format.wrela")
	order := []string{
		"STORAGE_REGION_SUPERBLOCK",
		"STORAGE_REGION_HOT_EVENT_SLOTS",
		"STORAGE_REGION_SEGMENT_MAP",
		"STORAGE_REGION_SEALED_SEGMENTS",
		"STORAGE_REGION_STREAM_DIRECTORY",
		"STORAGE_REGION_BLOB_EXTENTS",
		"STORAGE_REGION_BLOB_MANIFESTS",
		"STORAGE_REGION_PROJECTION_STORAGE",
		"STORAGE_REGION_MAINTENANCE_METADATA",
	}
	last := -1
	for _, name := range order {
		idx := strings.Index(source, name)
		if idx < 0 {
			t.Fatalf("missing region constant %s", name)
		}
		if idx <= last {
			t.Fatalf("region constant %s is out of order", name)
		}
		last = idx
	}
}
```

Verification:

```bash
go test ./compiler/sem -run TestStorageRegionOrderIsFrozen -v
go test ./compiler/codegen -run StorageRegion -v
go test ./compiler/sem ./compiler/codegen -v
git diff --check
```

Expected: all listed tests pass after implementation.

Commit:

```bash
git add wrela/storage/format.wrela wrela/storage/event_log.wrela compiler/sem/storage_region_test.go compiler/codegen/storage_region_test.go
git commit -m "feat: add storage superblock region map -Codex Automated"
```

### Task 14: Event Slot Encoding, CRC32C, And 4 KiB Packing

**Files:**

- Modify: `wrela/storage/event_log.wrela`
- Create: `compiler/codegen/event_slot_test.go`
- Create: `compiler/ir/event_slot_test.go`

**Description:** Encode hot event slots with exact header offsets, zeroed payload padding, CRC32C checksum, and immutable 4 KiB underfill behavior.

**Acceptance Criteria:**

- Encoder writes exactly 512 bytes per slot.
- Payload length greater than 448 rejects the append with `StorageAppendError(code = STORAGE_APPEND_PAYLOAD_TOO_LARGE)`.
- CRC32C computes with checksum bytes zeroed.
- For 512-byte LBA, one slot maps to one LBA.
- For 4096-byte LBA, eight slots map to one LBA.
- Underfilled 4096-byte durable batch fills remaining LBA slots with reserved empty slots and consumes event IDs.
- Reserved empty slots are never delivered to streams or projections.

**Code Examples:**

Add event slot writer:

```wrela
data EventSlotWriter {
    active_lba_size: U64

    fn slots_per_lba(self) -> U64 {
        return self.active_lba_size / STORAGE_EVENT_SLOT_SIZE
    }

    fn lba_for_event(self, event_region_base_lba: U64, event_id: U64) -> U64 {
        return event_region_base_lba + (event_id / self.slots_per_lba())
    }

    fn slot_in_lba(self, event_id: U64) -> U64 {
        return event_id % self.slots_per_lba()
    }

    fn payload_fits(self, payload_length: U64) -> Bool {
        return payload_length <= STORAGE_EVENT_PAYLOAD_BYTES
    }
}
```

Add reserved empty constructor:

```wrela
data ReservedEmptySlot {
    event_id: U64

    fn header(self) -> EventSlotHeader {
        return EventSlotHeader(
            event_id = self.event_id,
            stream_id = 0,
            stream_sequence = 0,
            event_type_id = 0,
            payload_layout_id = 0,
            atomic_group_len = 0,
            atomic_group_index = 0,
            payload_length = 0,
            flags = STORAGE_SLOT_RESERVED_EMPTY,
            checksum32 = 0,
            header_version = 1,
            reserved16 = 0,
            reserved64 = 0
        )
    }
}
```

Batch fill algorithm:

```wrela
data BatchPackingResult {
    semantic_slots: U64
    reserved_empty_slots: U64
    total_slot_positions: U64
}

data BatchPacker {
    active_lba_size: U64

    fn finish_batch(self, semantic_slots: U64) -> BatchPackingResult {
        let slots_per_lba = self.active_lba_size / STORAGE_EVENT_SLOT_SIZE
        let remainder = semantic_slots % slots_per_lba
        if remainder == 0 {
            return BatchPackingResult(semantic_slots = semantic_slots, reserved_empty_slots = 0, total_slot_positions = semantic_slots)
        }
        let empty = slots_per_lba - remainder
        return BatchPackingResult(semantic_slots = semantic_slots, reserved_empty_slots = empty, total_slot_positions = semantic_slots + empty)
    }
}
```

Add test:

```go
func TestFourKiBUnderfillConsumesEmptySlots(t *testing.T) {
	packer := batchPackerForTest{activeLBASize: 4096}
	got := packer.finishBatch(3)
	if got.reservedEmptySlots != 5 || got.totalSlotPositions != 8 {
		t.Fatalf("finishBatch(3) = %#v, want 5 empty and 8 total", got)
	}
}
```

Add codegen byte-offset test:

```go
func TestEventSlotHeaderOffsetsAreStable(t *testing.T) {
	offsets := map[string]int{
		"event_id": 0, "stream_id": 8, "stream_sequence": 16,
		"event_type_id": 24, "payload_layout_id": 28,
		"atomic_group_len": 32, "atomic_group_index": 36,
		"payload_length": 40, "flags": 44, "checksum32": 48,
	}
	for name, want := range offsets {
		if got := eventSlotHeaderOffsetForTest(name); got != want {
			t.Fatalf("%s offset = %d, want %d", name, got, want)
		}
	}
}
```

Verification:

```bash
go test ./compiler/ir -run TestFourKiBUnderfillConsumesEmptySlots -v
go test ./compiler/codegen -run TestEventSlotHeaderOffsetsAreStable -v
go test ./compiler/ir ./compiler/codegen -v
git diff --check
```

Expected: all listed tests pass after implementation.

Commit:

```bash
git add wrela/storage/event_log.wrela compiler/codegen/event_slot_test.go compiler/ir/event_slot_test.go
git commit -m "feat: encode fixed storage event slots -Codex Automated"
```

### Task 15: Atomic Group Batch Policy And StorageWriter Authority

**Files:**

- Create: `wrela/storage/writer.wrela`
- Create: `compiler/sem/storage_writer_test.go`
- Create: `tests/fixtures/negative/forged_storage_writer.wrela`
- Create: `tests/fixtures/negative/oversized_atomic_group.wrela`
- Modify: `compiler/sem/check.go`

**Description:** Implement the single foreground `StorageWriter` authority and the exact batch overflow policy from the design. The writer is the only source of durable append acknowledgement.

**Acceptance Criteria:**

- `StorageWriter` is a unique authority that cannot be directly constructed in user source outside the owned-hardware boot phase.
- Forged writer construction emits `SEM0113`.
- Atomic group size greater than `32` emits `SEM0114` for statically known groups and returns rejection for dynamic groups.
- The writer never splits one atomic group across acknowledged batches.
- A two-event group arriving at 63 open slots is admitted into a 65-slot overflow batch and flushes immediately.
- Append result must be observed; ignored durable append result emits `SEM0116`.

**Code Examples:**

Create writer surface:

```wrela
module storage.writer

use { Result } from wrela.lang.core
use { ForegroundStoragePath } from machine.x86_64.nvme
use { CoreSpscProducer, CoreSpscConsumer } from machine.x86_64.core_link
use { EventSlotHeader, StorageMetrics } from storage.format

const STORAGE_APPEND_OK: U64 = 0
const STORAGE_APPEND_TRANSACTION_TOO_LARGE: U64 = 1
const STORAGE_APPEND_PAYLOAD_TOO_LARGE: U64 = 2

data CommitToken {
    first_event_id: U64
    last_event_id: U64
}

data StorageAppendError {
    code: U64
}

enum StorageAppendResult {
    Accepted(token: CommitToken)
    Rejected(error: StorageAppendError)
}

data PendingAtomicGroup {
    first_stream_id: U64
    semantic_event_count: U64
    payload_bytes: U64
}

data CommittedAtomicGroup {
    first_event_id: U64
    last_event_id: U64
    slot_range_start_lba: U64
    slot_range_block_count: U64
    semantic_event_count: U64
    event_type_summary_ref: U64
    affected_streams_ref: U64
}

unique class StorageWriter {
    path: ForegroundStoragePath
    next_event_id: U64
    next_stream_id: U64
    slot_durable_frontier: U64
    atomic_group_frontier: U64
    open_batch_slots: U64
    metrics: StorageMetrics
    committed_groups: CoreSpscProducer<CommittedAtomicGroup>

    fn enqueue_atomic_group(self, group: PendingAtomicGroup) -> StorageAppendResult
    fn on_durability_completed(self, completion_command_id: U16)
}
```

Batch policy implementation:

```wrela
fn enqueue_atomic_group(self, group: PendingAtomicGroup) -> StorageAppendResult {
    if group.semantic_event_count > STORAGE_MAX_ATOMIC_GROUP_SLOTS {
        self.metrics.rejected_oversized_atomic_groups = self.metrics.rejected_oversized_atomic_groups + 1
        return StorageAppendResult.Rejected(error = StorageAppendError(code = STORAGE_APPEND_TRANSACTION_TOO_LARGE))
    }
    if self.open_batch_slots + group.semantic_event_count <= STORAGE_TARGET_BATCH_SLOTS {
        return self.add_group_to_open_batch(group = group, flush_after = false)
    }
    if self.open_batch_slots + group.semantic_event_count <= STORAGE_MAX_BATCH_SLOTS {
        self.metrics.batch_overflow_slots_used = self.metrics.batch_overflow_slots_used + ((self.open_batch_slots + group.semantic_event_count) - STORAGE_TARGET_BATCH_SLOTS)
        return self.add_group_to_open_batch(group = group, flush_after = true)
    }
    self.flush_open_batch()
    let result = self.add_group_to_open_batch(group = group, flush_after = group.semantic_event_count >= STORAGE_TARGET_BATCH_SLOTS)
    return result
}
```

Negative fixture for forged writer:

```wrela
// expect: SEM0113: StorageWriter authority cannot be constructed here
module negative.forged_storage_writer
use { StorageWriter } from storage.writer
use { ForegroundStoragePath } from machine.x86_64.nvme
executor Bad {
    path: ForegroundStoragePath
    start fn run(self) -> never {
        let writer = StorageWriter(path = self.path, next_event_id = 0, next_stream_id = 0)
        while true {}
    }
}
```

Batch policy test:

```go
func TestStorageWriterBatchOverflowDoesNotSplitGroup(t *testing.T) {
	writer := storageWriterPolicyForTest{openBatchSlots: 63}
	result := writer.enqueueAtomicGroup(2)
	if !result.accepted || writer.openBatchSlots != 65 || !writer.flushRequested {
		t.Fatalf("two-event overflow group result = %#v writer=%#v", result, writer)
	}
}
```

Verification:

```bash
go test ./compiler/sem -run 'TestStorageWriterBatchOverflowDoesNotSplitGroup|TestNegativeFixtures' -v
go test ./compiler/sem -v
git diff --check
```

Expected: all listed tests pass after implementation.

Commit:

```bash
git add wrela/storage/writer.wrela compiler/sem/storage_writer_test.go compiler/sem/check.go tests/fixtures/negative/forged_storage_writer.wrela tests/fixtures/negative/oversized_atomic_group.wrela
git commit -m "feat: add single storage writer authority -Codex Automated"
```

### Task 16: Dense Stream Directory And Append Validation

**Files:**

- Create: `wrela/storage/stream.wrela`
- Create: `compiler/sem/stream_directory_test.go`
- Modify: `wrela/storage/writer.wrela`

**Description:** Add store-assigned sequential stream IDs, direct stream directory lookup, expected sequence checks, and directory rebuild inputs. The directory is acceleration; event recovery remains truth.

**Acceptance Criteria:**

- New stream ID is `next_stream_id`; first stream ID is `0`.
- Stream existence check is `stream_id < next_stream_id`.
- Directory entry address uses `base + stream_id * 32`.
- Expected sequence mismatch rejects append before event slots are encoded.
- Creating a stream appends first event with sequence `1`.
- `StreamCheckpoint` records `stream_id`, `through_sequence`, `state_layout_id`, and `state_blob_ref`.
- A checkpoint whose state layout does not match current code is ignored and the stream replays from its initial state.
- Directory updates happen only after the atomic group is durable.

**Code Examples:**

Create stream source:

```wrela
module storage.stream

use { StreamDirectoryEntry } from storage.format

data StreamAppendExpectation {
    stream_id: U64
    expected_sequence: U64
}

data StreamAppendDecision {
    accepted: Bool
    next_sequence: U64
}

data StreamCheckpoint {
    stream_id: U64
    through_sequence: U64
    state_layout_id: U64
    state_blob_ref: U64
}

class StreamDirectory {
    base_lba: U64
    entry_count: U64
    next_stream_id: U64

    fn exists(self, stream_id: U64) -> Bool {
        return stream_id < self.next_stream_id
    }

    fn entry_byte_offset(self, stream_id: U64) -> U64 {
        return stream_id * 32
    }

    fn check_append(self, entry: StreamDirectoryEntry, expected: StreamAppendExpectation) -> StreamAppendDecision {
        if entry.latest_sequence != expected.expected_sequence {
            return StreamAppendDecision(accepted = false, next_sequence = entry.latest_sequence)
        }
        return StreamAppendDecision(accepted = true, next_sequence = entry.latest_sequence + 1)
    }

    fn allocate_stream_id(self) -> U64 {
        let id = self.next_stream_id
        self.next_stream_id = self.next_stream_id + 1
        return id
    }

    fn checkpoint_usable(self, checkpoint: StreamCheckpoint, current_state_layout_id: U64) -> Bool {
        if checkpoint.state_layout_id != current_state_layout_id {
            return false
        }
        return checkpoint.stream_id < self.next_stream_id
    }
}
```

Add source-shape test:

```go
func TestStreamDirectoryUsesDenseMath(t *testing.T) {
	source := readRepoFile(t, "wrela/storage/stream.wrela")
	for _, want := range []string{
		"stream_id < self.next_stream_id",
		"return stream_id * 32",
		"entry.latest_sequence != expected.expected_sequence",
		"self.next_stream_id = self.next_stream_id + 1",
		"data StreamCheckpoint",
		"checkpoint.state_layout_id != current_state_layout_id",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("stream directory source missing %q", want)
		}
	}
}
```

Writer integration example:

```wrela
fn publish_stream_head(self, stream_id: U64, sequence: U64, event_id: U64) {
    let entry = StreamDirectoryEntry(
        latest_sequence = sequence,
        latest_event_id = event_id,
        latest_checkpoint_ref = 0,
        flags = 0
    )
    self.write_stream_directory_entry(stream_id = stream_id, entry = entry)
}
```

Verification:

```bash
go test ./compiler/sem -run TestStreamDirectoryUsesDenseMath -v
go test ./compiler/sem -v
git diff --check
```

Expected: all listed tests pass after implementation.

Commit:

```bash
git add wrela/storage/stream.wrela wrela/storage/writer.wrela compiler/sem/stream_directory_test.go
git commit -m "feat: add dense stream directory -Codex Automated"
```

### Task 17: Recovery Scanner And Segment Lifecycle

**Files:**

- Modify: `wrela/storage/event_log.wrela`
- Modify: `wrela/storage/writer.wrela`
- Create: `compiler/sem/event_recovery_test.go`
- Create: `compiler/ir/segment_lifecycle_test.go`

**Description:** Recover the valid atomic-group prefix, seal hot segments, and add packed/no-op cold segment metadata behind the final compression API.

**Acceptance Criteria:**

- Recovery scans slots in event ID order.
- Recovery stops at checksum mismatch, unexpected event ID, invalid reserved empty slot, incomplete group, or mismatched group index.
- `event_type_id = 0` is legal only for reserved empty slots created by underfilled committed LBA padding.
- Segment states are `OpenHotSegment`, `SealedHotSegment`, `CompressibleSegment`, and `CompressedSegment`.
- Segment seal records first/last event ID, fixed slot ref, and checksum.
- On ZNS namespaces, sealed hot segments record `zone_start_lba` and `zone_block_count`; reclaim after accepted compression resets the old zone.
- Cold reads use segment map lookup before decoding packed segment bytes.
- Packed cold codec strips zero padding and builds segment-local index entries.
- Writer validates `SegmentCompressed` proposals before publishing segment map updates.

**Code Examples:**

Add recovery result:

```wrela
data RecoveryResult {
    valid: Bool
    next_event_id: U64
    atomic_group_frontier: U64
    stopped_reason: U64
}

const RECOVERY_STOP_CHECKSUM: U64 = 1
const RECOVERY_STOP_UNEXPECTED_EVENT_ID: U64 = 2
const RECOVERY_STOP_INVALID_EMPTY_SLOT: U64 = 3
const RECOVERY_STOP_INCOMPLETE_GROUP: U64 = 4
const RECOVERY_STOP_GROUP_INDEX: U64 = 5
```

Recovery validation shape:

```wrela
data EventRecoveryScanner {
    active_lba_size: U64

    fn validate_group_member(self, header: EventSlotHeader, expected_event_id: U64, group_len: U32, group_index: U32) -> Bool {
        if header.event_id != expected_event_id {
            return false
        }
        if header.event_type_id == 0 {
            return header.flags == STORAGE_SLOT_RESERVED_EMPTY
        }
        if header.atomic_group_len != group_len {
            return false
        }
        if header.atomic_group_index != group_index {
            return false
        }
        return true
    }
}
```

Segment metadata:

```wrela
const SEGMENT_STATE_OPEN_HOT: U64 = 1
const SEGMENT_STATE_SEALED_HOT: U64 = 2
const SEGMENT_STATE_COMPRESSIBLE: U64 = 3
const SEGMENT_STATE_COMPRESSED: U64 = 4

data EventSegment {
    segment_id: U64
    first_event_id: U64
    last_event_id: U64
    state: U64
    fixed_slot_ref: U64
    zone_start_lba: U64
    zone_block_count: U64
    compressed_ref: U64
    compressed_bytes: U64
    uncompressed_bytes: U64
    index_stride: U64
    index_ref: U64
    checksum32: U32
}

data SegmentIndexEntry {
    event_id_delta: U64
    uncompressed_offset: U64
}

data EventSegmentMapLookup {
    found: Bool
    segment: EventSegment
}

class EventSegmentMap {
    fn lookup(self, event_id: U64) -> EventSegmentMapLookup {
        let segment = self.find_segment(event_id = event_id)
        if event_id < segment.first_event_id {
            return EventSegmentMapLookup(found = false, segment = segment)
        }
        if event_id > segment.last_event_id {
            return EventSegmentMapLookup(found = false, segment = segment)
        }
        return EventSegmentMapLookup(found = true, segment = segment)
    }
}
```

Packed codec shape:

```wrela
data PackedSegmentCodec {
    fn packed_event_bytes(self, header: EventSlotHeader) -> U64 {
        return STORAGE_EVENT_HEADER_SIZE + header.payload_length
    }

    fn should_index(self, event_id_delta: U64, stride: U64) -> Bool {
        return event_id_delta % stride == 0
    }
}
```

Add test:

```go
func TestRecoveryRejectsEmptySlotOutsidePadding(t *testing.T) {
	header := EventSlotHeaderForTest{
		EventID: 7,
		EventTypeID: 0,
		Flags: 0,
	}
	if validateRecoveredHeaderForTest(header, 7) {
		t.Fatalf("empty event type without reserved flag must be invalid")
	}
}
```

Verification:

```bash
go test ./compiler/sem -run TestRecoveryRejectsEmptySlotOutsidePadding -v
go test ./compiler/ir -run SegmentLifecycle -v
go test ./compiler/sem ./compiler/ir -v
git diff --check
```

Expected: all listed tests pass after implementation.

Commit:

```bash
git add wrela/storage/event_log.wrela wrela/storage/writer.wrela compiler/sem/event_recovery_test.go compiler/ir/segment_lifecycle_test.go
git commit -m "feat: recover and seal storage event segments -Codex Automated"
```

---

## 8. Phase 5: Blob Storage And Maintenance Proposals

**Description:** This phase adds blob refs, extents, manifests, development AEAD wiring, orphan collection, relocation, and writer-validated maintenance proposals. Blob bytes are the large/sensitive data plane; event slots store compact refs.

**Acceptance Criteria:**

- New blob bytes and key metadata are durable before an event can reference the blob.
- `BlobRef` records blob ID, byte length, content hash, key ID, extent count, and inline extents or manifest ref.
- Free extent allocation splits and coalesces extents.
- Orphan collection derives liveness from acknowledged events, not allocator summaries.
- Relocation is copy-on-write and truth changes only when the foreground writer accepts a proposal.

**Phase Code Example:**

```wrela
let blob = blob_store.write_blob(bytes = content, policy = BlobWritePolicy(cipher_mode = BLOB_CIPHER_DEVELOPMENT_PASSTHROUGH))
let group = file_commands.commit_content(file_id = file_id, blob = blob.ref)
let result = writer.enqueue_atomic_group(group = group)
```

### Task 18: Blob Refs, Extents, Manifests, And Allocator

**Files:**

- Create: `wrela/storage/blob.wrela`
- Create: `compiler/sem/blob_storage_test.go`
- Create: `compiler/ir/blob_allocator_test.go`

**Description:** Define blob references and a first free-extent allocator. The allocator is repairable acceleration; events and blob manifests decide liveness.

**Acceptance Criteria:**

- `BlobRef` has `blob_id`, `byte_length`, `content_hash`, `key_id`, `extent_count`, and `inline_extents_or_manifest_ref`.
- `Extent` has `start_lba`, `block_count`, and `logical_offset`.
- Inline extents support four extents.
- Manifest extents support 128 extents.
- Free list supports 1024 extents, sorted by address.
- Allocation uses first-fit by address, splits larger extents, and records remaining free extent.
- Free coalesces adjacent extents.

**Code Examples:**

Create blob source:

```wrela
module storage.blob

use { Result, Unit } from wrela.lang.core
use { BackgroundStoragePath } from machine.x86_64.nvme

const BLOB_INLINE_EXTENT_COUNT: U64 = 4
const BLOB_MANIFEST_EXTENT_LIMIT: U64 = 128
const BLOB_ALLOCATOR_FREE_EXTENT_LIMIT: U64 = 1024

data BlobContentHash {
    low: U64
    high: U64
}

data BlobRef {
    blob_id: U64
    byte_length: U64
    content_hash: BlobContentHash
    key_id: U64
    extent_count: U64
    inline_extents_or_manifest_ref: U64
}

data Extent {
    start_lba: U64
    block_count: U64
    logical_offset: U64
}

data BlobManifest {
    blob_id: U64
    extent_count: U64
    extent0: Extent
    extent1: Extent
    extent2: Extent
    extent3: Extent
    manifest_ref: U64
    cipher_mode: U64
    nonce_low: U64
    nonce_high: U64
    auth_tag_low: U64
    auth_tag_high: U64
}

data FreeExtentList {
    count: U64
    extent0: Extent
    extent1: Extent
    extent2: Extent
    extent3: Extent
}
```

Allocator shape:

```wrela
class BlobExtentAllocator {
    free: FreeExtentList

    fn allocate(self, block_count: U64) -> Extent {
        let index = 0
        while index < self.free.count {
            let extent = self.free.at(index = index)
            if extent.block_count >= block_count {
                let allocated = Extent(start_lba = extent.start_lba, block_count = block_count, logical_offset = 0)
                let remaining = extent.block_count - block_count
                if remaining == 0 {
                    self.free.remove(index = index)
                } else {
                    self.free.set(index = index, extent = Extent(start_lba = extent.start_lba + block_count, block_count = remaining, logical_offset = 0))
                }
                return allocated
            }
            index = index + 1
        }
        return Extent(start_lba = 0, block_count = 0, logical_offset = 0)
    }

    fn free_extent(self, extent: Extent) {
        self.free.insert_sorted(extent = extent)
        self.free.coalesce_adjacent()
    }
}
```

Add source test:

```go
func TestBlobRefShape(t *testing.T) {
	source := readRepoFile(t, "wrela/storage/blob.wrela")
	for _, want := range []string{
		"blob_id: U64",
		"byte_length: U64",
		"content_hash: BlobContentHash",
		"key_id: U64",
		"extent_count: U64",
		"inline_extents_or_manifest_ref: U64",
		"coalesce_adjacent",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("blob source missing %q", want)
		}
	}
}
```

Verification:

```bash
go test ./compiler/sem -run TestBlobRefShape -v
go test ./compiler/ir -run BlobAllocator -v
go test ./compiler/sem ./compiler/ir -v
git diff --check
```

Expected: all listed tests pass after implementation.

Commit:

```bash
git add wrela/storage/blob.wrela compiler/sem/blob_storage_test.go compiler/ir/blob_allocator_test.go
git commit -m "feat: add blob extent storage contracts -Codex Automated"
```

### Task 19: Blob Write, Read, Delete Shape And Development AEAD

**Files:**

- Modify: `wrela/storage/blob.wrela`
- Create: `compiler/sem/blob_cipher_test.go`
- Modify: `compiler/sem/check.go`
- Create: `tests/fixtures/negative/development_blob_cipher_without_opt_in.wrela`

**Description:** Add the final blob manifest cipher fields and a named development passthrough AEAD for QEMU tests. Production images must explicitly opt in before constructing that mode.

**Acceptance Criteria:**

- `BlobCipherPolicy` has mode, key ID, nonce, and development opt-in fields.
- `BLOB_CIPHER_DEVELOPMENT_PASSTHROUGH = 1`.
- `BlobStore.write_blob` writes bytes first, writes manifest/key metadata second, and returns `UnpublishedBlobRef`.
- `StorageWriter` accepts events referencing only published blob refs.
- Using development passthrough without opt-in emits `SEM0123`.
- Logical delete appends delete event; space reclaim frees extents; privacy delete marks key destruction requested but does not claim secure erase.

**Code Examples:**

Add cipher policy:

```wrela
const BLOB_CIPHER_DEVELOPMENT_PASSTHROUGH: U64 = 1

data BlobCipherPolicy {
    mode: U64
    key_id: U64
    nonce_low: U64
    nonce_high: U64
    development_opt_in: Bool
}

data UnpublishedBlobRef {
    ref: BlobRef
    manifest_lba: U64
}

data PublishedBlobRef {
    ref: BlobRef
}

class DevelopmentPassthroughAead {
    fn seal(self, extent: Extent, policy: BlobCipherPolicy) -> BlobManifest {
        return BlobManifest(
            blob_id = 0,
            extent_count = 1,
            extent0 = extent,
            extent1 = extent,
            extent2 = extent,
            extent3 = extent,
            manifest_ref = 0,
            cipher_mode = policy.mode,
            nonce_low = policy.nonce_low,
            nonce_high = policy.nonce_high,
            auth_tag_low = extent.start_lba,
            auth_tag_high = extent.block_count
        )
    }
}
```

Add write order shape:

```wrela
class BlobStore {
    path: BackgroundStoragePath
    allocator: BlobExtentAllocator

    fn panic_development_cipher(self) {
        self.path.path.registers.panic.fail(code = 0xAC090123)
    }

    fn write_blob(self, byte_length: U64, policy: BlobCipherPolicy) -> UnpublishedBlobRef {
        if policy.mode == BLOB_CIPHER_DEVELOPMENT_PASSTHROUGH {
            if policy.development_opt_in == false {
                self.panic_development_cipher()
            }
        }
        let blocks = self.blocks_for_bytes(byte_length = byte_length)
        let extent = self.allocator.allocate(block_count = blocks)
        self.write_extent_bytes(extent = extent)
        self.write_manifest(extent = extent, policy = policy)
        return UnpublishedBlobRef(ref = BlobRef(blob_id = self.next_blob_id(), byte_length = byte_length, content_hash = BlobContentHash(low = 0, high = 0), key_id = policy.key_id, extent_count = 1, inline_extents_or_manifest_ref = extent.start_lba), manifest_lba = extent.start_lba)
    }
}
```

Negative fixture:

```wrela
// expect: SEM0123: development blob cipher requires explicit development opt in
module negative.development_blob_cipher_without_opt_in
use { BlobCipherPolicy, BLOB_CIPHER_DEVELOPMENT_PASSTHROUGH } from storage.blob
data UsesCipher {
    policy: BlobCipherPolicy
}
const BAD_MODE: U64 = BLOB_CIPHER_DEVELOPMENT_PASSTHROUGH
```

Verification:

```bash
go test ./compiler/sem -run 'TestBlobCipher|TestNegativeFixtures' -v
go test ./compiler/sem -v
git diff --check
```

Expected: all listed tests pass after implementation.

Commit:

```bash
git add wrela/storage/blob.wrela compiler/sem/blob_cipher_test.go compiler/sem/check.go tests/fixtures/negative/development_blob_cipher_without_opt_in.wrela
git commit -m "feat: add blob write manifest cipher shape -Codex Automated"
```

### Task 20: Orphan Collection And Blob Relocation Proposals

**Files:**

- Modify: `wrela/storage/blob.wrela`
- Modify: `wrela/storage/writer.wrela`
- Create: `compiler/sem/blob_maintenance_test.go`
- Create: `tests/fixtures/negative/maintenance_mutates_blob_truth.wrela`

**Description:** Add background orphan collection and copy-on-write blob relocation. Maintenance workers propose changes; only `StorageWriter` publishes truth.

**Acceptance Criteria:**

- Orphan collector builds live extent set from acknowledged events and blob manifests.
- Allocated but unreachable extents become reclaimable.
- Relocation copies blob bytes to new extents before proposing `RelocateBlob`.
- `StorageWriter` validates `old_ref`, `new_ref`, and `observed_version`.
- Maintenance code directly mutating writer-published roots emits `SEM0118`.
- Failure before proposal acceptance leaves new copy orphaned.
- Failure after proposal acceptance but before old extent free leaves old copy orphaned.

**Code Examples:**

Add proposal types:

```wrela
data RelocateBlobProposal {
    blob_id: U64
    old_ref: BlobRef
    new_ref: BlobRef
    observed_version: U64
}

data ReclaimExtentsProposal {
    extent_count: U64
    reason: U64
    first_extent: Extent
}

enum MaintenanceProposal {
    RelocateBlob(value: RelocateBlobProposal)
    ReclaimExtents(value: ReclaimExtentsProposal)
}
```

Writer validation:

```wrela
fn accept_relocate_blob(self, proposal: RelocateBlobProposal) -> Bool {
    let current = self.current_blob_ref(blob_id = proposal.blob_id)
    if current.blob_id != proposal.old_ref.blob_id {
        return false
    }
    if proposal.observed_version != self.blob_version(blob_id = proposal.blob_id) {
        return false
    }
    self.append_blob_relocated_event(old_ref = proposal.old_ref, new_ref = proposal.new_ref)
    return true
}
```

Orphan collector shape:

```wrela
class BlobOrphanCollector {
    path: BackgroundStoragePath

    fn mark_live_from_event(self, blob: BlobRef) {
        self.mark_blob_extents_live(blob = blob)
    }

    fn reclaim_unmarked_allocations(self) -> ReclaimExtentsProposal {
        let extent = self.first_unmarked_allocated_extent()
        return ReclaimExtentsProposal(extent_count = 1, reason = 1, first_extent = extent)
    }
}
```

Negative fixture:

```wrela
// expect: SEM0118: maintenance proposal must not mutate blob truth directly
module negative.maintenance_mutates_blob_truth
use { BlobRef } from storage.blob
class BadMaintenance {
    current: BlobRef
    fn run(self, next: BlobRef) {
        self.current = next
    }
}
```

Verification:

```bash
go test ./compiler/sem -run 'TestBlobMaintenance|TestNegativeFixtures' -v
go test ./compiler/sem -v
git diff --check
```

Expected: all listed tests pass after implementation.

Commit:

```bash
git add wrela/storage/blob.wrela wrela/storage/writer.wrela compiler/sem/blob_maintenance_test.go tests/fixtures/negative/maintenance_mutates_blob_truth.wrela
git commit -m "feat: add blob orphan relocation proposals -Codex Automated"
```

---

## 9. Phase 6: Projections, Checkpoints, And File Model

**Description:** This phase adds durable projection root shapes, explicit maintenance-core workers, read-plane watermarks, checkpoints, and the first file-like entity stream whose content is a blob ref.

**Acceptance Criteria:**

- Projection declarations are durable ABI only.
- Projection workers consume committed atomic groups from explicit core-link SPSC feeds.
- Projection roots are copy-on-write and writer-published.
- Queries return records plus watermark.
- File-like events prove events plus blobs can model file content without POSIX.

**Phase Code Example:**

```wrela
projection DirectoryChildren id 12 {
    layout 1 current {
        children: OrderedPages<FileId, FileNameKey, DirectoryChild>
    }
}
```

### Task 21: Projection Containers, Roots, And Read Plane

**Files:**

- Create: `wrela/storage/projection.wrela`
- Create: `compiler/sem/projection_storage_test.go`
- Modify: `wrela/storage/writer.wrela`
- Modify: `compiler/sem/check.go`

**Description:** Add the small projection container set and writer-published projection roots. Query code reads immutable roots and checks watermarks; it does not call projection workers.

**Acceptance Criteria:**

- `StateCell<T>`, `DenseEntityMap<Id, T>`, and `OrderedPages<Partition, SortKey, Row>` exist.
- `ProjectionCheckpoint` includes projection ID, layout ID, layout hash, worker code hash, last event ID applied, and root refs.
- `ProjectionWriter.publish(last_event_id)` creates an `AdvanceProjection` proposal.
- `StorageWriter` accepts `AdvanceProjection` only when `through_event_id <= atomic_group_frontier`.
- Invalid projection watermark emits `SEM0119`.
- Projection worker without boot-wired feed emits `SEM0120`.

**Code Examples:**

Create projection source:

```wrela
module storage.projection

use { CoreSpscConsumer, CoreSpscProducer } from machine.x86_64.core_link
use { CommittedAtomicGroup, MaintenanceProposal } from storage.writer

data RootRef {
    lba: U64
    byte_length: U64
    checksum32: U32
}

data ProjectionCheckpoint {
    projection_id: U64
    projection_layout_id: U64
    projection_layout_hash: U64
    worker_code_hash: U64
    last_event_id_applied: U64
    root_ref0: RootRef
    root_ref1: RootRef
    root_ref2: RootRef
    root_ref3: RootRef
}

data StateCell<T> {
    root: RootRef
}

data DenseEntityMap<Id, T> {
    root: RootRef
    chunk_count: U64
}

data OrderedPages<Partition, SortKey, Row> {
    root: RootRef
    first_page_ref: RootRef
}

data AdvanceProjectionProposal {
    projection_id: U64
    through_event_id: U64
    checkpoint: ProjectionCheckpoint
}

class ProjectionWriter<P> {
    projection_id: U64
    proposal_out: CoreSpscProducer<MaintenanceProposal>

    fn publish(self, through_event_id: U64, checkpoint: ProjectionCheckpoint) {
        let proposal = AdvanceProjectionProposal(projection_id = self.projection_id, through_event_id = through_event_id, checkpoint = checkpoint)
        self.submit_advance(proposal = proposal)
    }
}

data ProjectionQueryResult<T> {
    watermark_event_id: U64
    value: T
}
```

Read-plane shape:

```wrela
class ProjectionReader<P> {
    projection_id: U64
    latest: ProjectionCheckpoint

    fn watermark(self) -> U64 {
        return self.latest.last_event_id_applied
    }
}
```

Add semantic test:

```go
func TestProjectionContainersAreLimitedAndRooted(t *testing.T) {
	source := readRepoFile(t, "wrela/storage/projection.wrela")
	for _, want := range []string{
		"data StateCell<T>",
		"data DenseEntityMap<Id, T>",
		"data OrderedPages<Partition, SortKey, Row>",
		"last_event_id_applied",
		"AdvanceProjectionProposal",
		"through_event_id",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("projection source missing %q", want)
		}
	}
}
```

Verification:

```bash
go test ./compiler/sem -run TestProjectionContainersAreLimitedAndRooted -v
go test ./compiler/sem -v
git diff --check
```

Expected: all listed tests pass after implementation.

Commit:

```bash
git add wrela/storage/projection.wrela wrela/storage/writer.wrela compiler/sem/projection_storage_test.go compiler/sem/check.go
git commit -m "feat: add projection storage roots -Codex Automated"
```

### Task 22: File-Like Entity Stream And Directory Projection Worker

**Files:**

- Create: `wrela/storage/file_model.wrela`
- Modify: `wrela/storage/projection.wrela`
- Create: `compiler/sem/file_model_test.go`
- Create: `tests/e2e/fixtures/nvme_event_storage/program.wrela`

**Description:** Add one file-like model on top of events and blobs. This proves file content, names, directory membership, and deletion without implementing POSIX.

**Acceptance Criteria:**

- File events have stable IDs starting at `1001`.
- File content event stores `BlobRef`, not inline bytes.
- Rename and move can be encoded as one atomic group by writer code.
- `FileState` stores current blob ref, name ref, parent ID, deleted flag, and stream sequence.
- `DirectoryProjectionWorker` consumes `CommittedAtomicGroup` from core link and publishes `DirectoryChildren`.
- Worker ignores events it does not understand.
- Query result includes watermark.

**Code Examples:**

Create file model:

```wrela
module storage.file_model

use { BlobRef } from storage.blob
use { OrderedPages, ProjectionWriter, ProjectionQueryResult } from storage.projection
use { CommittedAtomicGroup } from storage.writer
use { CoreSpscConsumer } from machine.x86_64.core_link
use { Option } from wrela.lang.core

data FileId {
    value: U64
}

data FileNameKey {
    hash: U64
}

data DirectoryChild {
    file_id: FileId
    name_ref: BlobRef
    deleted: Bool
}

data FileState {
    file_id: FileId
    current_blob_ref: BlobRef
    name_ref: BlobRef
    parent_id: FileId
    deleted: Bool
    stream_sequence: U64
}

event FileCreated id 1001 {
    file_id: FileId
    parent_id: FileId
    name_ref: BlobRef
    layout 1 current {
        file_id: U64 = self.file_id.value
        parent_id: U64 = self.parent_id.value
        name_ref: BlobRef = self.name_ref
    }
}

event FileRenamed id 1002 {
    file_id: FileId
    parent_id: FileId
    name_ref: BlobRef
    layout 1 current {
        file_id: U64 = self.file_id.value
        parent_id: U64 = self.parent_id.value
        name_ref: BlobRef = self.name_ref
    }
}

event FileContentCommitted id 1003 {
    file_id: FileId
    blob_ref: BlobRef
    layout 1 current {
        file_id: U64 = self.file_id.value
        blob_ref: BlobRef = self.blob_ref
    }
}

event FileDeleted id 1004 {
    file_id: FileId
    layout 1 current {
        file_id: U64 = self.file_id.value
    }
}

projection DirectoryChildren id 12 {
    layout 1 current {
        children: OrderedPages<FileId, FileNameKey, DirectoryChild>
    }
}
```

Projection worker:

```wrela
class DirectoryProjectionWorker {
    source: CoreSpscConsumer<CommittedAtomicGroup>
    projection: ProjectionWriter<DirectoryChildren>

    fn run_once(self) {
        let next = self.source.try_recv()
        match next {
            Option.Some(value = group) -> self.apply_group(group = group)
            Option.None -> self.source.arm_wait()
        }
    }

    fn apply_group(self, group: CommittedAtomicGroup) {
        self.apply_events(first_event_id = group.first_event_id, last_event_id = group.last_event_id)
        self.projection.publish(through_event_id = group.last_event_id, checkpoint = self.current_checkpoint())
    }
}
```

Add source test:

```go
func TestFileModelUsesEventsAndBlobRefs(t *testing.T) {
	source := readRepoFile(t, "wrela/storage/file_model.wrela")
	for _, want := range []string{
		"event FileCreated id 1001",
		"event FileRenamed id 1002",
		"event FileContentCommitted id 1003",
		"event FileDeleted id 1004",
		"blob_ref: BlobRef",
		"projection DirectoryChildren id 12",
		"DirectoryProjectionWorker",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("file model source missing %q", want)
		}
	}
}
```

Verification:

```bash
go test ./compiler/sem -run TestFileModelUsesEventsAndBlobRefs -v
go test ./compiler/sem -v
git diff --check
```

Expected: all listed tests pass after implementation.

Commit:

```bash
git add wrela/storage/file_model.wrela wrela/storage/projection.wrela compiler/sem/file_model_test.go tests/e2e/fixtures/nvme_event_storage/program.wrela
git commit -m "feat: add file model on event storage -Codex Automated"
```

---

## 10. Phase 7: End-To-End Storage Image, Replay, And Metrics

**Description:** This phase wires the pieces into a bootable QEMU image and proves append, replay, blob refs, projection advancement, orphan collection, and metrics.

**Acceptance Criteria:**

- QEMU launches with an NVMe device and persistent raw disk image.
- First boot writes file-like events and blob bytes.
- Second boot recovers acknowledged events and rebuilds stale derived state.
- Projection root watermark reaches the committed event ID.
- Metrics report validates durability mode, LBA size, underfilled slots, foreground/background queue depth, blob orphan bytes, and projection lag.

**Phase Code Example:**

```bash
go test ./tests/e2e -run NvmeEventStorage -v
go test ./tests/e2e -run NvmeEventStorageReplay -v
```

### Task 23: Storage Image Boot Wiring

**Files:**

- Create: `tests/e2e/fixtures/nvme_event_storage/main.wrela`
- Modify: `tests/e2e/fixtures/nvme_event_storage/program.wrela`
- Create: `tests/e2e/nvme_event_storage_qemu_test.go`
- Modify: `compiler/integration_test.go`

**Description:** Add the boot image that discovers NVMe, creates foreground/background paths, creates a foreground `StorageWriter`, creates a maintenance projection worker, writes a file-like entity, and prints a deterministic serial marker.

**Acceptance Criteria:**

- Image requires at least two executor slots: `foreground` and `maintenance`.
- NVMe PCI device is discovered by class shape, not vendor/device ID.
- Foreground path owner is foreground slot.
- Background path owner is maintenance slot.
- Foreground writes one file create event and one content commit event in one atomic group.
- Maintenance worker receives a committed group descriptor and advances `DirectoryChildren`.
- Serial output includes `NVME_STORAGE_APPEND_OK`.

**Code Examples:**

Boot wiring shape:

```wrela
image NvmeEventStorageImage {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        let panic = BootPanic()
        let discovery = PlatformDiscoveryRoot(panic = panic).from_uefi(hardware = hardware)
        let nvme_device = discovery.pci.require_class(base = 0x01, subclass = 0x08, prog_if = 0x02, occurrence = 0)
        let foreground_slot = ExecutorSlot(id = 0)
        let maintenance_slot = ExecutorSlot(id = 1)
        let nvme = NvmeDriver(registers = NvmeControllerRegisters(mmio = nvme_device.claim_mmio_bar(index = 0), panic = panic), memory = DriverMemory(), namespace = NvmeNamespace(namespace_id = 1, logical_block_size = 512, block_count = 0, zone_size_blocks = 0, supports_zns = false, supports_fua = true, atomic_write_unit_blocks = 1, power_fail_atomic_write_unit_blocks = 1, volatile_write_cache = false)).initialize(device = nvme_device)
        let foreground_path = nvme.create_io_path(identity = PathIdentity(label = "storage.foreground"), owner = foreground_slot, role = NvmePathRole(role = NVME_PATH_FOREGROUND), route = foreground_route, irq = foreground_irq.publisher())
        let background_path = nvme.create_io_path(identity = PathIdentity(label = "storage.background"), owner = maintenance_slot, role = NvmePathRole(role = NVME_PATH_BACKGROUND), route = background_route, irq = background_irq.publisher())
        return hardware.exit_to_owned_hardware(memory_plan = memory_plan, cpu_plan = cpu_plan, hardware_plan = hardware_plan)
    }
}
```

QEMU test skeleton:

```go
func TestNvmeEventStorageQEMU(t *testing.T) {
	disk := t.TempDir() + "/storage.raw"
	createRawDisk(t, disk, 64*1024*1024)
	result := runWrelaQEMU(t, qemuRunConfig{
		Fixture: "nvme_event_storage",
		ExtraArgs: []string{
			"-drive", "file=" + disk + ",if=none,id=nvme0,format=raw",
			"-device", "nvme,drive=nvme0,serial=wrela-storage-0",
		},
	})
	if !strings.Contains(result.Serial, "NVME_STORAGE_APPEND_OK") {
		t.Fatalf("serial output missing append marker:\n%s", result.Serial)
	}
}
```

Verification:

```bash
go test ./compiler -run TestIntegration -v
go test ./tests/e2e -run TestNvmeEventStorageQEMU -v
git diff --check
```

Expected: integration and QEMU tests pass on a machine with QEMU/OVMF.

Commit:

```bash
git add tests/e2e/fixtures/nvme_event_storage/main.wrela tests/e2e/fixtures/nvme_event_storage/program.wrela tests/e2e/nvme_event_storage_qemu_test.go compiler/integration_test.go
git commit -m "test: boot nvme event storage image -Codex Automated"
```

### Task 24: Replay After Reboot And Orphan Collection E2E

**Files:**

- Modify: `tests/e2e/nvme_event_storage_qemu_test.go`
- Modify: `tests/e2e/fixtures/nvme_event_storage/program.wrela`
- Modify: `wrela/storage/event_log.wrela`
- Modify: `wrela/storage/blob.wrela`

**Description:** Prove acknowledged events survive reboot, unaccepted blob relocation leaves recoverable orphans, and projection roots never claim a false watermark.

**Acceptance Criteria:**

- First QEMU boot appends acknowledged file events and prints last event ID.
- Second QEMU boot uses the same disk image, scans event slots, and prints the same last event ID.
- Recovery rebuilds stream directory when directory metadata is marked stale.
- Injected relocation crash before writer acceptance leaves new extents counted as orphan bytes.
- Projection watermark after replay is less than or equal to atomic group frontier and reaches the committed group after maintenance runs.
- Serial output includes `NVME_STORAGE_REPLAY_OK` and `NVME_ORPHAN_COLLECTION_OK`.

**Code Examples:**

Replay test:

```go
func TestNvmeEventStorageReplayQEMU(t *testing.T) {
	disk := t.TempDir() + "/storage.raw"
	createRawDisk(t, disk, 64*1024*1024)
	first := runStorageQEMU(t, disk, "first")
	if !strings.Contains(first.Serial, "NVME_STORAGE_APPEND_OK last_event_id=1") {
		t.Fatalf("first boot missing append marker:\n%s", first.Serial)
	}
	second := runStorageQEMU(t, disk, "replay")
	for _, want := range []string{
		"NVME_STORAGE_REPLAY_OK last_event_id=1",
		"NVME_ORPHAN_COLLECTION_OK",
		"projection_watermark=1",
	} {
		if !strings.Contains(second.Serial, want) {
			t.Fatalf("second boot missing %q:\n%s", want, second.Serial)
		}
	}
}
```

Runtime replay marker:

```wrela
fn print_replay_result(self, recovered: RecoveryResult, projection_watermark: U64) {
    if recovered.atomic_group_frontier == 1 {
        self.serial.write(bytes = "NVME_STORAGE_REPLAY_OK last_event_id=1\n")
    }
    if projection_watermark == recovered.atomic_group_frontier {
        self.serial.write(bytes = "projection_watermark=1\n")
    }
}
```

Verification:

```bash
go test ./tests/e2e -run TestNvmeEventStorageReplayQEMU -v
git diff --check
```

Expected: QEMU replay test passes on a machine with QEMU/OVMF.

Commit:

```bash
git add tests/e2e/nvme_event_storage_qemu_test.go tests/e2e/fixtures/nvme_event_storage/program.wrela wrela/storage/event_log.wrela wrela/storage/blob.wrela
git commit -m "test: verify nvme event storage replay -Codex Automated"
```

### Task 25: Storage Metrics And Final Acceptance Sweep

**Files:**

- Modify: `compiler/report/report.go`
- Modify: `compiler/report/report_test.go`
- Modify: `compiler/sem/report.go`
- Modify: `wrela/storage/format.wrela`
- Modify: `tests/e2e/nvme_event_storage_qemu_test.go`
- Modify: `docs/production-deferred-work.md`

**Description:** Publish the metrics needed to judge the design and run the full acceptance sweep.

**Acceptance Criteria:**

- Report includes active LBA size, namespace mode, durability mode, event slot size, atomic groups per second, events per group, overflow slots, rejected oversized groups, events per committed LBA, sealed event block count, underfilled LBA count, reserved empty slots, bytes per durable event, estimated drive writes per day, device media writes, latency percentiles, payload utilization, payload overflow count, hot/packed/compressed bytes per event, compression ratio, compression time, compressed segment lookup latency, compression backlog, foreground/background queue depth, completion latency, core-link queue depth, core-link wake count, blob orphan bytes, projection lag, projection rebuild time, projection SPSC depth, projection backpressure, projection layout upcast/rebuild counts, and stream directory cache hit rate.
- E2E serial output includes `NVME_STORAGE_METRICS_OK`.
- Deferred-work docs retain all out-of-scope storage items from Task 1.
- Final commands pass.

**Code Examples:**

Report structs:

```go
type StorageReport struct {
	ActiveLBASize      uint64
	NamespaceMode      string
	DurabilityMode     string
	EventSlotSize      uint64
	AtomicGroupsPerSec uint64
	EventsPerGroup     uint64
	BatchOverflowSlots uint64
	RejectedOversizedGroups uint64
	EventsPerCommittedLBA uint64
	SealedEventBlocks uint64
	UnderfilledLBAs    uint64
	ReservedEmptySlots uint64
	BytesPerEvent      uint64
	EstimatedDriveWritesPerDay uint64
	DeviceReportedMediaWrites uint64
	GroupCommitLatencyP99US uint64
	AppendAckLatencyP99US uint64
	PayloadUtilizationX1000 uint64
	PayloadOverflowCount uint64
	HotBytesPerEvent uint64
	PackedBytesPerEvent uint64
	CompressedBytesPerEvent uint64
	CompressionRatioX1000 uint64
	CompressionCPUTimeUS uint64
	CompressedSegmentLookupLatencyUS uint64
	ColdCompressionBacklog uint64
	ForegroundQueueDepth uint64
	BackgroundQueueDepth uint64
	ForegroundCompletionLatencyUS uint64
	BackgroundCompletionLatencyUS uint64
	CoreLinkQueueDepth uint64
	CoreLinkWakeCount  uint64
	BlobOrphanBytes    uint64
	ProjectionLagEvents uint64
	ProjectionRebuildTimeUS uint64
	ProjectionSPSCDepth uint64
	ProjectionBackpressureCount uint64
	ProjectionLayoutUpcastCount uint64
	ProjectionLayoutRebuildCount uint64
	StreamDirectoryCacheHitRateX1000 uint64
	NvmePaths          []NvmePathReport
}

type NvmePathReport struct {
	Label   string
	Role    string
	Owner   string
	QueueID uint16
	Vector  uint8
}
```

Report test:

```go
func TestStorageMetricsReport(t *testing.T) {
	checked := &sem.CheckedProgram{ImageGraph: sem.ImageGraph{}}
	r := sem.BuildImageReport(checked)
	r.Storage.ActiveLBASize = 512
	r.Storage.DurabilityMode = "fua"
	r.Storage.EventSlotSize = 512
	r.Storage.NvmePaths = []report.NvmePathReport{{Label: "storage.foreground", Role: "foreground", Owner: "foreground", QueueID: 1, Vector: 0x50}}
	if r.Storage.ActiveLBASize != 512 || r.Storage.EventSlotSize != 512 {
		t.Fatalf("storage metrics missing fixed sizes: %#v", r.Storage)
	}
	if len(r.Storage.NvmePaths) != 1 || r.Storage.NvmePaths[0].Role != "foreground" {
		t.Fatalf("storage paths missing: %#v", r.Storage.NvmePaths)
	}
}
```

Final verification:

```bash
go test ./...
go test ./tests/e2e -run NvmeEventStorage -v
go test ./tests/e2e -run NvmeEventStorageReplay -v
bad_terms='TO''DO|TB''D|fill'' in|implement'' later|Add'' appropriate|similar'' to'' Task'
rg -n "$bad_terms" wrela compiler tests docs/implementation/2026-05-18-nvme-event-storage-executable-plan.md
git diff --check
```

Expected: `go test ./...` passes; QEMU tests pass where QEMU/OVMF are available; the placeholder phrase scan prints no matches; `git diff --check` prints nothing.

Commit:

```bash
git add compiler/report/report.go compiler/report/report_test.go compiler/sem/report.go wrela/storage/format.wrela tests/e2e/nvme_event_storage_qemu_test.go docs/production-deferred-work.md
git commit -m "test: complete nvme event storage acceptance -Codex Automated"
```

---

## 11. Appendix A: Exact Append And Recovery Algorithms

**Description:** These algorithms are normative for Tasks 14-17. Use these names and state transitions.

**Acceptance Criteria:**

- Implementations match these branches exactly.
- Tests cover every branch listed here.

**Code Example:**

```text
enqueue_atomic_group(group):
  if group.semantic_event_count > 32:
    reject TransactionTooLarge

  if open_batch_slots + group.semantic_event_count <= 64:
    add group
    return pending

  if open_batch_slots + group.semantic_event_count <= 72:
    add group
    flush batch
    return pending

  flush current batch
  add group to new batch
  if group.semantic_event_count >= 64:
    flush batch
  return pending
```

```text
finish_lba_padding(active_lba_size, semantic_slots):
  slots_per_lba = active_lba_size / 512
  remainder = semantic_slots % slots_per_lba
  if remainder == 0:
    reserved_empty_slots = 0
  else:
    reserved_empty_slots = slots_per_lba - remainder
  total_slot_positions = semantic_slots + reserved_empty_slots
```

```text
recover_hot_slots:
  expected_event_id = first_uncompressed_event_id
  while true:
    read slot for expected_event_id
    if checksum invalid: stop before slot
    if header.event_id != expected_event_id: stop before slot
    if header.event_type_id == 0:
      if header.flags != STORAGE_SLOT_RESERVED_EMPTY: stop before slot
      expected_event_id += 1
      continue
    group_len = header.atomic_group_len
    if group_len == 0 or group_len > 32: stop before slot
    for group_index in 0..group_len-1:
      validate event_id, checksum, group_len, group_index
      if any invalid: stop before first group slot
    publish group through last event
    expected_event_id += group_len
```

```text
durability_ack:
  write command completion alone is enough only when selected durability mode says FUA/non-volatile completion is durable
  write-plus-flush mode acknowledges after the flush command completion
  acknowledged group advances atomic_group_frontier
  only then publish CommittedAtomicGroup to maintenance link
```

---

## 12. Appendix B: Exact Storage Event And Projection Syntax

**Description:** These syntax forms are final for this plan. Do not add alternate spellings.

**Acceptance Criteria:**

- Parser and docs accept these forms.
- Parser and docs do not introduce a `worker` keyword.

**Code Example:**

```wrela
event NoteRenamed id 21 {
    note_id: FileId
    title_ref: BlobRef

    layout 1 {
        note_id: U64
        old_title_ref: BlobRef
    }

    layout 2 current {
        note_id: U64 = self.note_id.value
        title_ref: BlobRef = self.title_ref
    }

    upcast 1 -> 2 {
        old_title_ref -> title_ref
    }
}

projection DirectoryChildren id 12 {
    layout 1 current {
        children: OrderedPages<FileId, FileNameKey, DirectoryChild>
    }
}

class DirectoryProjectionWorker {
    source: CoreSpscConsumer<CommittedAtomicGroup>
    projection: ProjectionWriter<DirectoryChildren>
}
```

Rules:

- `event_type_id = 0` is reserved for committed empty slots.
- Event IDs must be positive `U64` literals.
- Projection IDs must be positive `U64` literals.
- Layout IDs must be positive `U64` literals scoped to one event or projection.
- A declaration with one layout may omit `current`; semantic checking marks it current.
- A declaration with more than one layout must mark exactly one layout `current`.
- Upcast mappings are same-type field renames. Complex computed upcasts are not in this plan.
- Projection containers are only `StateCell<T>`, `DenseEntityMap<Id, T>`, and `OrderedPages<Partition, SortKey, Row>`.

---

## 13. Appendix C: Full Plan Acceptance Criteria

**Description:** The full plan is complete only when these checks pass. Do not claim completion from a subset.

**Acceptance Criteria:**

- Compiler storage syntax, semantic, IR, and codegen tests pass.
- NVMe source contract tests pass.
- Event slot, batch policy, stream directory, blob, projection, and report tests pass.
- QEMU append and replay tests pass where QEMU/OVMF are available.
- Storage audit report shows the selected durability mode and both NVMe paths.
- The final implementation preserves the non-goals from the design.

**Code Example:**

```bash
go test ./compiler/lex ./compiler/ast ./compiler/parse
go test ./compiler/sem -run 'Storage|Nvme|Blob|Projection|CoreLink|NegativeFixtures' -v
go test ./compiler/ir -run 'Storage|Nvme|Blob|Segment|Event' -v
go test ./compiler/codegen -run 'Storage|Nvme|EventSlot' -v
go test ./compiler/report -run Storage -v
go test ./...
go test ./tests/e2e -run NvmeEventStorage -v
go test ./tests/e2e -run NvmeEventStorageReplay -v
git diff --check
```

Expected: every command succeeds. If QEMU/OVMF are not installed, record the local skip output and run the compiler/unit suite before handing off to hardware-capable CI.
