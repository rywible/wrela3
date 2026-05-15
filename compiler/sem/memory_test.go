package sem

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
)

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
