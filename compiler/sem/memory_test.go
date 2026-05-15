package sem

import (
	"path/filepath"
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
)

func TestUserRawMemoryAuthorityRejected(t *testing.T) {
	cases := []struct {
		fixture string
		code    string
	}{
		{"user_raw_memory_asm.wrela", diag.SEM0032},
		{"user_raw_memory_authority.wrela", diag.SEM0028},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			modules := parseFixtureModulesForTest(t, filepath.Join("tests", "fixtures", "negative", tc.fixture))
			index, ds := BuildIndex(modules)
			ds = filterMissingImageDiagnostic(ds)
			if len(ds) != 0 {
				t.Fatalf("index diagnostics: %#v", ds)
			}
			_, diags := Check(index, modules)
			if !hasCode(diags, tc.code) {
				t.Fatalf("expected %s, got %#v", tc.code, diags)
			}
		})
	}
}

func TestUserModuleAsmBypassShapesRejected(t *testing.T) {
	cases := []struct {
		name   string
		source string
	}{
		{
			name: "driver",
			source: `
module negative.user_driver_asm

unique driver BadDriver {
    asm fn write_raw(self) {
        mov rax, 0x200000
        mov [rax], rax
    }
}
`,
		},
		{
			name: "driver_path",
			source: `
module negative.user_driver_path_asm

driver path BadPath {
    asm fn write_raw(self) {
        mov rax, 0x200000
        mov [rax], rax
    }
}
`,
		},
		{
			name: "shadow_executor_memory",
			source: `
module negative.user_shadow_executor_memory

class ExecutorMemory {
    asm fn halt_forever(self) -> never {
        hlt
    }
}
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			modules := parseModulesForTest(t, tc.source)
			index, ds := BuildIndex(modules)
			ds = filterMissingImageDiagnostic(ds)
			if len(ds) != 0 {
				t.Fatalf("index diagnostics: %#v", ds)
			}
			_, diags := Check(index, modules)
			if !hasCode(diags, diag.SEM0032) {
				t.Fatalf("expected SEM0032, got %#v", diags)
			}
		})
	}
}

func TestMemoryKindClassification(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

data Bytes { address: PhysicalAddress; length: U64 }
data MutableBytes { address: PhysicalAddress; length: U64 }
class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame { return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0) }
}
class ArenaFrame { arena_base: PhysicalAddress; arena_length: U64; next_offset: U64 }
`, `
module machine.x86_64.cache_memory

use { MutableBytes } from machine.x86_64.executor_memory

class CacheArena { storage: MutableBytes; slot_count: U64; slot_size: U64; next_victim: U64 }
`, `
module user.shadow

class ExecutorMemory {}
class ArenaFrame {}
data Bytes {}
data MutableBytes {}
class CacheArena {}
`)
	index, ds := BuildIndex(modules)
	ds = filterMissingImageDiagnostic(ds)
	if len(ds) != 0 {
		t.Fatalf("index diagnostics: %#v", ds)
	}
	cases := []struct {
		module string
		name   string
		want   MemoryKind
	}{
		{"machine.x86_64.executor_memory", "ExecutorMemory", MemoryKindRootArena},
		{"machine.x86_64.executor_memory", "ArenaFrame", MemoryKindFrameArena},
		{"machine.x86_64.executor_memory", "Bytes", MemoryKindBytes},
		{"machine.x86_64.executor_memory", "MutableBytes", MemoryKindMutableBytes},
		{"machine.x86_64.cache_memory", "CacheArena", MemoryKindCacheArena},
	}
	for _, tc := range cases {
		typ, ok := index.Lookup(tc.module, tc.name)
		if !ok {
			t.Fatalf("missing type %s.%s", tc.module, tc.name)
		}
		got := ClassifyMemoryType(typ)
		if got != tc.want {
			t.Fatalf("ClassifyMemoryType(%s.%s) = %v, want %v", tc.module, tc.name, got, tc.want)
		}
	}
	for _, name := range []string{"ExecutorMemory", "ArenaFrame", "Bytes", "MutableBytes", "CacheArena"} {
		typ, ok := index.Lookup("user.shadow", name)
		if !ok {
			t.Fatalf("missing user.shadow.%s", name)
		}
		if got := ClassifyMemoryType(typ); got != MemoryKindNone {
			t.Fatalf("user.shadow.%s classified as %v, want MemoryKindNone", name, got)
		}
	}
}

func TestWithFrameTypechecks(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame { return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0) }
}
class ArenaFrame {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
}
unique class OwnedHardware {}
unique class DelegatedHardware { fn exit_to_owned_hardware(self) -> OwnedHardware { return OwnedHardware() } }
executor Worker {
    memory: ExecutorMemory
    start fn run(self) -> never {
        with self.memory.frame(length = 64) as tick {
        }
        while true {}
    }
}
image App {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware { return hardware.exit_to_owned_hardware() }
    phase owned_hardware(hardware: OwnedHardware) -> never {
        let worker = Worker(memory = ExecutorMemory(arena_base = 0, arena_length = 4096, next_offset = 0))
        worker.run()
    }
}
`)
	index, ds := BuildIndex(modules)
	ds = filterMissingImageDiagnostic(ds)
	if len(ds) != 0 {
		t.Fatalf("index diagnostics: %#v", ds)
	}
	_, diags := Check(index, modules)
	if len(diags) != 0 {
		t.Fatalf("check diagnostics: %#v", diags)
	}
}

func TestWithRejectsNonFrameExpression(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

data NotFrame { value: U64 }
unique class OwnedHardware {}
unique class DelegatedHardware { fn exit_to_owned_hardware(self) -> OwnedHardware { return OwnedHardware() } }
executor Worker {
    start fn run(self) -> never {
        with NotFrame(value = 1) as tick {
        }
        while true {}
    }
}
image App {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware { return hardware.exit_to_owned_hardware() }
    phase owned_hardware(hardware: OwnedHardware) -> never {
        let worker = Worker()
        worker.run()
    }
}
`)
	index, ds := BuildIndex(modules)
	ds = filterMissingImageDiagnostic(ds)
	if len(ds) != 0 {
		t.Fatalf("index diagnostics: %#v", ds)
	}
	_, diags := Check(index, modules)
	if !hasCode(diags, diag.SEM0022) {
		t.Fatalf("expected SEM0022, got %#v", diags)
	}
}

func TestWithRejectsNonU64FrameLength(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame {
        return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0)
    }
}
class ArenaFrame { arena_base: PhysicalAddress; arena_length: U64; next_offset: U64 }
executor Worker {
    memory: ExecutorMemory
    start fn run(self) -> never {
        with self.memory.frame(length = true) as tick {
        }
        while true {}
    }
}
`)
	index, ds := BuildIndex(modules)
	ds = filterMissingImageDiagnostic(ds)
	if len(ds) != 0 {
		t.Fatalf("index diagnostics: %#v", ds)
	}
	_, diags := Check(index, modules)
	if !hasCode(diags, diag.SEM0023) {
		t.Fatalf("expected SEM0023, got %#v", diags)
	}
}

func TestDirectArenaFrameConstructionRejected(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

class ArenaFrame { arena_base: PhysicalAddress; arena_length: U64; next_offset: U64 }
class Bad {
    fn make(self) -> ArenaFrame {
        return ArenaFrame(arena_base = 0, arena_length = 64, next_offset = 0)
    }
}
`)
	index, ds := BuildIndex(modules)
	ds = filterMissingImageDiagnostic(ds)
	if len(ds) != 0 {
		t.Fatalf("index diagnostics: %#v", ds)
	}
	_, diags := Check(index, modules)
	if !hasCode(diags, diag.SEM0029) {
		t.Fatalf("expected SEM0029, got %#v", diags)
	}
}

func TestDirectFrameCallRejectedOutsideWith(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame {
        return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0)
    }
}
class ArenaFrame { arena_base: PhysicalAddress; arena_length: U64; next_offset: U64 }
executor Worker {
    memory: ExecutorMemory
    start fn run(self) -> never {
        let tick = self.memory.frame(length = 64)
        while true {}
    }
}
`)
	index, ds := BuildIndex(modules)
	ds = filterMissingImageDiagnostic(ds)
	if len(ds) != 0 {
		t.Fatalf("index diagnostics: %#v", ds)
	}
	_, diags := Check(index, modules)
	if !hasCode(diags, diag.SEM0022) {
		t.Fatalf("expected SEM0022, got %#v", diags)
	}
}

func TestArenaPlaceAndReserveIntrinsics(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

data Message { id: U64 }
data MutableBytes { address: PhysicalAddress; length: U64 }
class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame { return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0) }
}
class ArenaFrame { arena_base: PhysicalAddress; arena_length: U64; next_offset: U64 }
unique class OwnedHardware {}
unique class DelegatedHardware { fn exit_to_owned_hardware(self) -> OwnedHardware { return OwnedHardware() } }
executor Worker {
    memory: ExecutorMemory
    start fn run(self) -> never {
        with self.memory.frame(length = 128) as tick {
            let msg = tick.place(Message(id = 7))
            let raw = tick.reserve(length = 64, align = 8)
        }
        while true {}
    }
}
image App {
    transitions { delegated_hardware -> owned_hardware }
    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware { return hardware.exit_to_owned_hardware() }
    phase owned_hardware(hardware: OwnedHardware) -> never {
        let worker = Worker(memory = ExecutorMemory(arena_base = 0, arena_length = 4096, next_offset = 0))
        worker.run()
    }
}
`)
	index, ds := BuildIndex(modules)
	if len(ds) != 0 {
		t.Fatalf("index diagnostics: %#v", ds)
	}
	_, diags := Check(index, modules)
	if len(diags) != 0 {
		t.Fatalf("check diagnostics: %#v", diags)
	}
}

func TestPlaceRejectsNonConstructor(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame {
        return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0)
    }
}
class ArenaFrame { arena_base: PhysicalAddress; arena_length: U64; next_offset: U64 }
executor Worker {
    memory: ExecutorMemory
    start fn run(self) -> never {
        with self.memory.frame(length = 128) as tick {
            let bad = tick.place(7)
        }
        while true {}
    }
}
`)
	index, ds := BuildIndex(modules)
	ds = filterMissingImageDiagnostic(ds)
	if len(ds) != 0 {
		t.Fatalf("index diagnostics: %#v", ds)
	}
	_, diags := Check(index, modules)
	if !hasCode(diags, diag.SEM0026) {
		t.Fatalf("expected SEM0026, got %#v", diags)
	}
}

func TestFrameLifetimeFlowsThroughHelperMethod(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

data Message { id: U64 }
class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame { return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0) }
}
class ArenaFrame { arena_base: PhysicalAddress; arena_length: U64; next_offset: U64 }
class Parser {
    fn parse(self, heap: ArenaFrame) -> Message {
        let msg = heap.place(Message(id = 1))
        return msg
    }
}
executor Worker {
    memory: ExecutorMemory
    parser: Parser
    start fn run(self) -> never {
        with self.memory.frame(length = 64) as tick {
            let msg = self.parser.parse(heap = tick)
        }
        while true {}
    }
}
`)
	index, ds := BuildIndex(modules)
	ds = filterMissingImageDiagnostic(ds)
	if len(ds) != 0 {
		t.Fatalf("index diagnostics: %#v", ds)
	}
	_, diags := Check(index, modules)
	if len(diags) != 0 {
		t.Fatalf("check diagnostics: %#v", diags)
	}
}

func TestHelperCannotHideFrameEscape(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

data Message { id: U64 }
class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame { return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0) }
}
class ArenaFrame { arena_base: PhysicalAddress; arena_length: U64; next_offset: U64 }
class Parser {
    saved: Message
    fn parse(self, heap: ArenaFrame) -> Message {
        let msg = heap.place(Message(id = 1))
        self.saved = msg
        return msg
    }
}
`)
	index, ds := BuildIndex(modules)
	ds = filterMissingImageDiagnostic(ds)
	if len(ds) != 0 {
		t.Fatalf("index diagnostics: %#v", ds)
	}
	_, diags := Check(index, modules)
	if !hasCode(diags, diag.SEM0025) {
		t.Fatalf("expected SEM0025, got %#v", diags)
	}
}

func TestMethodLifetimeNamedArgumentsUseParameterMapping(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

data Message { id: U64 }
data Box { msg: Message }
class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame { return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0) }
}
class ArenaFrame {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame { return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0) }
}
class Parser {
    fn parse(self, left: ArenaFrame, right: ArenaFrame) -> Message {
        let msg = right.place(Message(id = 1))
        return msg
    }
}
executor Worker {
    memory: ExecutorMemory
    parser: Parser
    start fn run(self) -> never {
        with self.memory.frame(length = 128) as left_frame {
            let parent_box = left_frame.place(Box(msg = Message(id = 0)))
            with left_frame.frame(length = 64) as child_frame {
                parent_box.msg = self.parser.parse(right = child_frame, left = left_frame)
            }
        }
        while true {}
    }
}
`)
	index, ds := BuildIndex(modules)
	ds = filterMissingImageDiagnostic(ds)
	if len(ds) != 0 {
		t.Fatalf("index diagnostics: %#v", ds)
	}
	_, diags := Check(index, modules)
	if !hasCode(diags, diag.SEM0025) {
		t.Fatalf("expected SEM0025, got %#v", diags)
	}
}

func TestMethodLifetimeForwardReferenceAcrossTypes(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

data Message { id: U64 }
class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame { return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0) }
}
class ArenaFrame { arena_base: PhysicalAddress; arena_length: U64; next_offset: U64 }
executor Worker {
    memory: ExecutorMemory
    parser: Parser
    start fn run(self) -> never {
        with self.memory.frame(length = 64) as tick {
            let msg = self.parser.parse(heap = tick)
        }
        while true {}
    }
}
class Parser {
    fn parse(self, heap: ArenaFrame) -> Message {
        let msg = heap.place(Message(id = 1))
        return msg
    }
}
`)
	index, ds := BuildIndex(modules)
	ds = filterMissingImageDiagnostic(ds)
	if len(ds) != 0 {
		t.Fatalf("index diagnostics: %#v", ds)
	}
	_, diags := Check(index, modules)
	if len(diags) != 0 {
		t.Fatalf("check diagnostics: %#v", diags)
	}
}

func TestFrameParameterParentsNestedFrame(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

data Message { id: U64 }
data Box { msg: Message }
class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame { return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0) }
}
class ArenaFrame {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame { return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0) }
}
class Parser {
    fn fill_child(self, heap: ArenaFrame) -> Message {
        let msg = heap.place(Message(id = 1))
        with heap.frame(length = 64) as child {
            let box = child.place(Box(msg = Message(id = 0)))
            box.msg = msg
        }
        return msg
    }
}
executor Worker {
    memory: ExecutorMemory
    parser: Parser
    start fn run(self) -> never {
        with self.memory.frame(length = 128) as tick {
            let msg = self.parser.fill_child(heap = tick)
        }
        while true {}
    }
}
`)
	index, ds := BuildIndex(modules)
	ds = filterMissingImageDiagnostic(ds)
	if len(ds) != 0 {
		t.Fatalf("index diagnostics: %#v", ds)
	}
	_, diags := Check(index, modules)
	if len(diags) != 0 {
		t.Fatalf("check diagnostics: %#v", diags)
	}
}

func TestCacheLookupRequiresFrameDestination(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

data Bytes { address: PhysicalAddress; length: U64 }
data MutableBytes { address: PhysicalAddress; length: U64 }
class ArenaFrame { arena_base: PhysicalAddress; arena_length: U64; next_offset: U64 }
class ExecutorMemory { arena_base: PhysicalAddress; arena_length: U64; next_offset: U64 }
`, `
module machine.x86_64.cache_memory

use { Bytes, MutableBytes, ArenaFrame, ExecutorMemory } from machine.x86_64.executor_memory

data CacheLookup { hit: Bool; bytes: Bytes }
data CachePutResult { stored: Bool; evicted: U64 }
class CacheArena {
    storage: MutableBytes
    slot_count: U64
    slot_size: U64
    next_victim: U64
    fn get_bytes(self, key: U64, into: ArenaFrame) -> CacheLookup { return CacheLookup(hit = false, bytes = Bytes(address = 0, length = 0)) }
    fn put_bytes(self, key: U64, bytes: Bytes) -> CachePutResult { return CachePutResult(stored = true, evicted = 0) }
}
class Bad {
    cache: CacheArena
    memory: ExecutorMemory
    fn bad(self) -> CacheLookup {
        return self.cache.get_bytes(key = 1, into = self.memory)
    }
}
`)
	index, ds := BuildIndex(modules)
	ds = filterMissingImageDiagnostic(ds)
	if len(ds) != 0 {
		t.Fatalf("index diagnostics: %#v", ds)
	}
	_, diags := Check(index, modules)
	if !hasCode(diags, diag.SEM0030) {
		t.Fatalf("expected SEM0030, got %#v", diags)
	}
}

func TestCacheLookupBytesCannotEscapeFrame(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

data Bytes { address: PhysicalAddress; length: U64 }
data MutableBytes { address: PhysicalAddress; length: U64 }
class ArenaFrame { arena_base: PhysicalAddress; arena_length: U64; next_offset: U64 }
class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame { return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0) }
}
`, `
module machine.x86_64.cache_memory

use { Bytes, MutableBytes, ArenaFrame, ExecutorMemory } from machine.x86_64.executor_memory

data CacheLookup { hit: Bool; bytes: Bytes }
data CachePutResult { stored: Bool; evicted: U64 }
class CacheArena {
    storage: MutableBytes
    slot_count: U64
    slot_size: U64
    next_victim: U64
    fn get_bytes(self, key: U64, into: ArenaFrame) -> CacheLookup { return CacheLookup(hit = false, bytes = Bytes(address = 0, length = 0)) }
}
class Bad {
    cache: CacheArena
    memory: ExecutorMemory
    saved: Bytes
    fn bad(self) {
        with self.memory.frame(length = 64) as tick {
            let found = self.cache.get_bytes(key = 1, into = tick)
            self.saved = found.bytes
        }
    }
}
`)
	index, ds := BuildIndex(modules)
	ds = filterMissingImageDiagnostic(ds)
	if len(ds) != 0 {
		t.Fatalf("index diagnostics: %#v", ds)
	}
	_, diags := Check(index, modules)
	if !hasCode(diags, diag.SEM0031) {
		t.Fatalf("expected SEM0031, got %#v", diags)
	}
}

func TestCacheLookupValueCannotEscapeFrame(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

data Bytes { address: PhysicalAddress; length: U64 }
data MutableBytes { address: PhysicalAddress; length: U64 }
class ArenaFrame { arena_base: PhysicalAddress; arena_length: U64; next_offset: U64 }
class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame { return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0) }
}
`, `
module machine.x86_64.cache_memory

use { Bytes, MutableBytes, ArenaFrame, ExecutorMemory } from machine.x86_64.executor_memory

data CacheLookup { hit: Bool; bytes: Bytes }
class CacheArena {
    storage: MutableBytes
    slot_count: U64
    slot_size: U64
    next_victim: U64
    fn get_bytes(self, key: U64, into: ArenaFrame) -> CacheLookup { return CacheLookup(hit = false, bytes = Bytes(address = 0, length = 0)) }
}
class Bad {
    cache: CacheArena
    memory: ExecutorMemory
    fn bad(self) -> CacheLookup {
        with self.memory.frame(length = 64) as tick {
            return self.cache.get_bytes(key = 1, into = tick)
        }
    }
}
`)
	index, ds := BuildIndex(modules)
	ds = filterMissingImageDiagnostic(ds)
	if len(ds) != 0 {
		t.Fatalf("index diagnostics: %#v", ds)
	}
	_, diags := Check(index, modules)
	if !hasCode(diags, diag.SEM0031) {
		t.Fatalf("expected SEM0031, got %#v", diags)
	}
}

func TestCacheLookupPositionalIntoUsesFrameLifetime(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

data Bytes { address: PhysicalAddress; length: U64 }
data MutableBytes { address: PhysicalAddress; length: U64 }
class ArenaFrame { arena_base: PhysicalAddress; arena_length: U64; next_offset: U64 }
class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame { return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0) }
}
`, `
module machine.x86_64.cache_memory

use { Bytes, MutableBytes, ArenaFrame, ExecutorMemory } from machine.x86_64.executor_memory

data CacheLookup { hit: Bool; bytes: Bytes }
class CacheArena {
    storage: MutableBytes
    slot_count: U64
    slot_size: U64
    next_victim: U64
    fn get_bytes(self, key: U64, into: ArenaFrame) -> CacheLookup { return CacheLookup(hit = false, bytes = Bytes(address = 0, length = 0)) }
}
class Worker {
    cache: CacheArena
    memory: ExecutorMemory
    fn ok(self) -> never {
        with self.memory.frame(length = 64) as tick {
            let found = self.cache.get_bytes(1, tick)
            let bytes = found.bytes
        }
        while true {}
    }
}
`)
	index, ds := BuildIndex(modules)
	ds = filterMissingImageDiagnostic(ds)
	if len(ds) != 0 {
		t.Fatalf("index diagnostics: %#v", ds)
	}
	_, diags := Check(index, modules)
	if len(diags) != 0 {
		t.Fatalf("check diagnostics: %#v", diags)
	}
}
