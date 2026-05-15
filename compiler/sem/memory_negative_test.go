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
