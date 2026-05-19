package ir

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/parse"
	"github.com/ryanwible/wrela3/compiler/sem"
	"github.com/ryanwible/wrela3/compiler/source"
)

func TestGenericMethodBodyIsLoweredForConcreteInstantiation(t *testing.T) {
	program := lowerSourceForTest(t, `
module ir.generics
data Event { kind: U64 }
class Holder<T> {
    value: T
    fn read(self) -> T {
        return self.value
    }
}
executor Worker {
    holder: Holder<Event>
    start fn run(self) -> never {
        let event = self.holder.read()
        while true {}
    }
}
`)
	holderEvent := &sem.Type{Module: "ir.generics", Name: "Holder", TypeArgs: []*sem.Type{{Module: "ir.generics", Name: "Event"}}}
	fn := findFunction(program, symbolName("method", "ir.generics", holderEvent.MangledName(), "read"))
	if fn == nil {
		t.Fatal("missing concrete Holder<Event>.read function")
	}
	if !containsOp[*FieldLoad](*fn) {
		t.Fatalf("read body did not lower the cloned field load: %#v", fn.Blocks)
	}
}

func TestLowerGenericEnumTypeInfo(t *testing.T) {
	program := lowerSourceForTest(t, `
module ir.enum_layout
enum Option<T> { None Some(value: T) }
data Event {
    first: U64
    second: U64
    kind: U32
}
executor Worker {
    start fn run(self, next: Option<Event>) -> never {
        while true {}
    }
}
`)
	info, ok := program.Types["ir.enum_layout.Option[ir.enum_layout.Event]"]
	if !ok {
		t.Fatalf("missing concrete Option<Event> type info: %#v", program.Types)
	}
	if info.Fields["$tag"].Offset != 0 || info.Fields["Some.value"].Offset != 8 {
		t.Fatalf("enum field offsets = %#v", info.Fields)
	}
	if info.Fields["Some.value"].StorageSize <= 8 || info.StorageSize != 32 {
		t.Fatalf("enum did not use semantic payload size: info=%#v", info)
	}
	if len(info.EnumVariants) != 2 || info.EnumVariants[0].Name != "None" || info.EnumVariants[1].Discriminant != 1 {
		t.Fatalf("enum variants = %#v", info.EnumVariants)
	}
}

func TestGenericIRTypeKeepsQualifiedTypeArgs(t *testing.T) {
	program := lowerSourcesForGenericIdentityTest(t, []genericIdentitySource{
		{path: "common.wrela", text: `
module common
data Slots<T> { value: T }
`},
		{path: "a.wrela", text: `
module a
use { Slots } from common
data Event { small: U64 }
data Root { slots: Slots<Event> }
`},
		{path: "b.wrela", text: `
module b
use { Slots } from common
data Event {
    first: U64
    second: U64
}
data Root { slots: Slots<Event> }
`},
	})
	aSlots := program.Types["a.Root"].Fields["slots"].Type
	bSlots := program.Types["b.Root"].Fields["slots"].Type
	if aSlots == bSlots {
		t.Fatalf("generic IR types conflated: a=%#v b=%#v", aSlots, bSlots)
	}
	if aSlots.Name != "common.Slots[a.Event]" || bSlots.Name != "common.Slots[b.Event]" {
		t.Fatalf("generic IR type names = %q and %q, want canonical type arguments", aSlots.Name, bSlots.Name)
	}
}

type genericIdentitySource struct {
	path string
	text string
}

func lowerSourcesForGenericIdentityTest(t *testing.T, files []genericIdentitySource) *Program {
	t.Helper()
	graph := source.Graph{}
	for i, file := range files {
		graph.Files = append(graph.Files, source.NewFile(source.FileID(i+1), file.path, file.text))
	}
	modules, ds := parse.ParseGraph(graph)
	if len(ds) != 0 {
		t.Fatalf("parse diagnostics: %#v", ds)
	}
	index, ds := sem.BuildIndex(modules)
	ds = filterMissingImageDiagnostic(ds)
	if len(ds) != 0 {
		t.Fatalf("index diagnostics: %#v", ds)
	}
	checked, ds := sem.Check(index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}
	program, ds := Lower(checked)
	if len(ds) != 0 {
		t.Fatalf("lower diagnostics: %#v", ds)
	}
	return program
}

func TestTraitConstrainedGenericCallLowersToDirectConcreteCall(t *testing.T) {
	src := `
module ir.traits
enum Option<T> { None Some(value: T) }
trait Subscription<T> { fn try_next(self) -> Option<T> }
data Event { kind: U64 }
class EventSub {
    fn try_next(self) -> Option<Event> {
        return Option.None()
    }
}
impl Subscription<Event> for EventSub
class Drain<S, T> where S: Subscription<T> {
    input: S
    fn poll(self) -> Option<T> {
        return self.input.try_next()
    }
}
executor Worker {
    drain: Drain<EventSub, Event>
    start fn run(self) -> never {
        let next = self.drain.poll()
        while true {}
    }
}
`
	program := lowerSourceForTest(t, src)
	drainEvent := &sem.Type{
		Module: "ir.traits",
		Name:   "Drain",
		TypeArgs: []*sem.Type{
			{Module: "ir.traits", Name: "EventSub"},
			{Module: "ir.traits", Name: "Event"},
		},
	}
	fn := findFunction(program, symbolName("method", "ir.traits", drainEvent.MangledName(), "poll"))
	if fn == nil {
		t.Fatal("missing Drain<EventSub, Event>.poll")
	}
	if !functionCalls(*fn, "_wrela_method_ir_traits_EventSub_try_next") {
		t.Fatalf("poll did not lower to direct EventSub.try_next call: %#v", fn.Blocks)
	}
}
