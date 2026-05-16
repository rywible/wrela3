package sem

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/diag"
)

func parseFixtureModulesForTest(t *testing.T, path string) []*ast.Module {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	abs := filepath.Join(wd, "..", "..", path)
	src, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read fixture %s: %v", abs, err)
	}
	return parseModulesForTest(t, string(src))
}

func TestFrameEscapeNegativeFixtures(t *testing.T) {
	fixtures := []string{
		"frame_escape_return.wrela",
		"frame_escape_field.wrela",
		"frame_escape_sibling.wrela",
	}
	for _, fixture := range fixtures {
		t.Run(fixture, func(t *testing.T) {
			modules := parseFixtureModulesForTest(t, filepath.Join("tests", "fixtures", "negative", fixture))
			index, ds := BuildIndex(modules)
			ds = filterMissingImageDiagnostic(ds)
			if len(ds) != 0 {
				t.Fatalf("index diagnostics: %#v", ds)
			}
			_, diags := Check(index, modules)
			if !hasCode(diags, diag.SEM0024) && !hasCode(diags, diag.SEM0025) {
				t.Fatalf("expected frame escape diagnostic, got %#v", diags)
			}
		})
	}
}

func TestParentFrameValueCanBeUsedInsideChildFrame(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

data Message { id: U64 }
data Box { msg: Message }
class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame {
        return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0)
    }
}
class ArenaFrame {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame {
        return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0)
    }
}
executor Worker {
    memory: ExecutorMemory
    start fn run(self) -> never {
        with self.memory.frame(length = 128) as parent {
            let msg = parent.place(Message(id = 1))
            with parent.frame(length = 64) as child {
                let box = child.place(Box(msg = Message(id = 0)))
                box.msg = msg
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
	if len(diags) != 0 {
		t.Fatalf("check diagnostics: %#v", diags)
	}
}

func TestArenaPlaceAllowsClassAndSameFrameMutation(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

data Message { id: U64 }
class Box {
    msg: Message
    fn set(self, msg: Message) {
        self.msg = msg
    }
}
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
            let box = tick.place(Box(msg = Message(id = 0)))
            let msg = tick.place(Message(id = 1))
            box.set(msg = msg)
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

func TestArenaPlaceClassAllowsParentFrameValueInChild(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

data Message { id: U64 }
class Box { msg: Message }
class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame {
        return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0)
    }
}
class ArenaFrame {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame {
        return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0)
    }
}
executor Worker {
    memory: ExecutorMemory
    start fn run(self) -> never {
        with self.memory.frame(length = 256) as parent {
            let msg = parent.place(Message(id = 1))
            with parent.frame(length = 128) as child {
                let box = child.place(Box(msg = msg))
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
	if len(diags) != 0 {
		t.Fatalf("check diagnostics: %#v", diags)
	}
}

func TestChildFrameValueCannotBeStoredInParentFrame(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

data Message { id: U64 }
data Box { msg: Message }
class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame {
        return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0)
    }
}
class ArenaFrame {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame {
        return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0)
    }
}
executor Worker {
    memory: ExecutorMemory
    start fn run(self) -> never {
        with self.memory.frame(length = 128) as parent {
            let parent_box = parent.place(Box(msg = Message(id = 0)))
            with parent.frame(length = 64) as child {
                let msg = child.place(Message(id = 1))
                parent_box.msg = msg
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

func TestConstructorRejectsFrameValueStoredInExecutorField(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

data Message { id: U64 }
data Box { msg: Message }
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
    saved: Box
    start fn run(self) -> never {
        with self.memory.frame(length = 128) as tick {
            let msg = tick.place(Message(id = 1))
            self.saved = Box(msg = msg)
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

func TestArenaPlaceConstructorRejectsSiblingFrameValue(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

data Message { id: U64 }
data Box { msg: Message }
class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame {
        return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0)
    }
}
class ArenaFrame {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame {
        return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0)
    }
}
executor Worker {
    memory: ExecutorMemory
    start fn run(self) -> never {
        with self.memory.frame(length = 256) as parent {
            with parent.frame(length = 64) as left {
                let msg = left.place(Message(id = 1))
                with parent.frame(length = 64) as right {
                    let box = right.place(Box(msg = msg))
                }
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

func TestArenaPlaceClassConstructorRejectsSiblingFrameValue(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

data Message { id: U64 }
class Box { msg: Message }
class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame {
        return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0)
    }
}
class ArenaFrame {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame {
        return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0)
    }
}
executor Worker {
    memory: ExecutorMemory
    start fn run(self) -> never {
        with self.memory.frame(length = 256) as parent {
            with parent.frame(length = 64) as left {
                let msg = left.place(Message(id = 1))
                with parent.frame(length = 64) as right {
                    let box = right.place(Box(msg = msg))
                }
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

func TestFramePlacedClassHelperRejectsChildValueMutation(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

data Message { id: U64 }
class Box {
    msg: Message
    fn set(self, msg: Message) {
        self.msg = msg
    }
}
class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame {
        return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0)
    }
}
class ArenaFrame {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame {
        return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0)
    }
}
executor Worker {
    memory: ExecutorMemory
    start fn run(self) -> never {
        with self.memory.frame(length = 256) as parent {
            let box = parent.place(Box(msg = Message(id = 0)))
            with parent.frame(length = 64) as child {
                let msg = child.place(Message(id = 1))
                box.set(msg = msg)
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

func TestArenaPlaceRejectsNestedClassConstructor(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

class Inner { id: U64 }
class Outer { inner: Inner }
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
            let outer = tick.place(Outer(inner = Inner(id = 1)))
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
	if !hasCode(diags, diag.SEM0006) {
		t.Fatalf("expected SEM0006, got %#v", diags)
	}
}

func TestFramePlacedClassFieldsCannotReconstructEscapingBytes(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

data Bytes { address: PhysicalAddress; length: U64 }
class Holder {
    address: PhysicalAddress
    length: U64
    fn set(self, address: PhysicalAddress, length: U64) {
        self.address = address
        self.length = length
    }
}
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
    saved: Bytes
    start fn run(self) -> never {
        with self.memory.frame(length = 128) as tick {
            let holder = tick.place(Holder(address = 0, length = 0))
            holder.set(address = tick.arena_base, length = tick.arena_length)
            self.saved = Bytes(address = holder.address, length = holder.length)
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

func TestMethodReturningReceiverFieldCarriesReceiverLifetime(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

data Message { id: U64 }
class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame {
        return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0, saved = Message(id = 0))
    }
}
class ArenaFrame {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    saved: Message
    fn get(self) -> Message {
        return self.saved
    }
}
executor Worker {
    memory: ExecutorMemory
    saved: Message
    start fn run(self) -> never {
        with self.memory.frame(length = 128) as tick {
            self.saved = tick.get()
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

func TestMethodDataParameterCannotHideFrameEscape(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

data Message { id: U64 }
class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame {
        return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0)
    }
}
class ArenaFrame { arena_base: PhysicalAddress; arena_length: U64; next_offset: U64 }
class Sink {
    saved: Message
    fn save(self, msg: Message) {
        self.saved = msg
    }
}
executor Worker {
    memory: ExecutorMemory
    sink: Sink
    start fn run(self) -> never {
        with self.memory.frame(length = 128) as tick {
            let msg = tick.place(Message(id = 1))
            self.sink.save(msg = msg)
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

func TestMethodCannotReconstructFrameBackedBytesFromFields(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

data Bytes { address: PhysicalAddress; length: U64 }
class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame {
        return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0)
    }
}
class ArenaFrame { arena_base: PhysicalAddress; arena_length: U64; next_offset: U64 }
class Sink {
    saved: Bytes
    fn save(self, bytes: Bytes) {
        self.saved = Bytes(address = bytes.address, length = bytes.length)
    }
}
executor Worker {
    memory: ExecutorMemory
    sink: Sink
    start fn run(self) -> never {
        with self.memory.frame(length = 128) as tick {
            let bytes = tick.place(Bytes(address = 1, length = 1))
            self.sink.save(bytes = bytes)
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

func TestFrameFieldsCannotReconstructEscapingBytes(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

data Bytes { address: PhysicalAddress; length: U64 }
class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame {
        return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0)
    }
}
class ArenaFrame {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
}
executor Worker {
    memory: ExecutorMemory
    saved: Bytes
    start fn run(self) -> never {
        with self.memory.frame(length = 128) as tick {
            self.saved = Bytes(address = tick.arena_base, length = tick.arena_length)
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

func TestFrameFieldArithmeticCannotReconstructEscapingBytes(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

data Bytes { address: PhysicalAddress; length: U64 }
class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame {
        return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0)
    }
}
class ArenaFrame {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
}
executor Worker {
    memory: ExecutorMemory
    saved: Bytes
    start fn run(self) -> never {
        with self.memory.frame(length = 128) as tick {
            self.saved = Bytes(address = tick.arena_base + 0, length = tick.arena_length + 0)
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

func TestPrimitiveHelperCannotReconstructEscapingBytes(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

data Bytes { address: PhysicalAddress; length: U64 }
class ExecutorMemory {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
    fn frame(self, length: U64) -> ArenaFrame {
        return ArenaFrame(arena_base = self.arena_base, arena_length = length, next_offset = 0)
    }
}
class ArenaFrame {
    arena_base: PhysicalAddress
    arena_length: U64
    next_offset: U64
}
class Builder {
    fn rebuild(self, address: PhysicalAddress, length: U64) -> Bytes {
        return Bytes(address = address + 0, length = length)
    }
}
executor Worker {
    memory: ExecutorMemory
    builder: Builder
    saved: Bytes
    start fn run(self) -> never {
        with self.memory.frame(length = 128) as tick {
            self.saved = self.builder.rebuild(address = tick.arena_base + 0, length = tick.arena_length)
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

func TestReserveRejectsLiteralNonPowerOfTwoAlign(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory

data MutableBytes { address: PhysicalAddress; length: U64 }
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
            let bytes = tick.reserve(length = 16, align = 3)
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
	if !hasCode(diags, diag.SEM0027) {
		t.Fatalf("expected SEM0027, got %#v", diags)
	}
}

func TestFrameLifetimeAncestryComparison(t *testing.T) {
	c := &checker{
		frameLifetimeParents: map[int]int{
			1: 0,
			2: 1,
			3: 1,
			4: -1,
		},
	}
	parent := Lifetime{Kind: LifetimeFrame, Scope: 1}
	child := Lifetime{Kind: LifetimeFrame, Scope: 2}
	sibling := Lifetime{Kind: LifetimeFrame, Scope: 3}
	param := Lifetime{Kind: LifetimeFrame, Scope: -1}
	paramChild := Lifetime{Kind: LifetimeFrame, Scope: 4}

	if c.lifetimeShorterThan(parent, child) {
		t.Fatal("parent lifetime must be valid inside child frame")
	}
	if !c.lifetimeShorterThan(child, parent) {
		t.Fatal("child lifetime must not flow into parent frame")
	}
	if !c.lifetimeShorterThan(child, sibling) {
		t.Fatal("sibling frame lifetime must not flow into another sibling")
	}
	if c.lifetimeShorterThan(param, paramChild) {
		t.Fatal("frame parameter lifetime must be valid inside child frame")
	}
	if !c.lifetimeShorterThan(paramChild, param) {
		t.Fatal("child of frame parameter must not flow back into parameter frame")
	}
}
