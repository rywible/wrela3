package sem

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
)

func mustCompleteGenericInstantiations(t *testing.T, index *Index) {
	t.Helper()
	if ds := index.CompleteGenericInstantiations(); len(ds) != 0 {
		t.Fatalf("generic instantiation diagnostics: %#v", ds)
	}
}

func TestGenericInstantiationKeyIsDeterministic(t *testing.T) {
	modules := parseModulesForTest(t, `
module sem.generics
data Payload { value: U64 }
data Box<T> { value: T }
data UsesBox { box: Box<Payload> }
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	typ, ok := index.Lookup("sem.generics", "UsesBox")
	if !ok {
		t.Fatal("UsesBox not indexed")
	}
	got := typ.Fields[0].Type.Key()
	want := "sem.generics.Box[sem.generics.Payload]"
	if got != want {
		t.Fatalf("field type key = %q, want %q", got, want)
	}
}

func TestGenericFieldSubstitutionIsRecursive(t *testing.T) {
	modules := parseModulesForTest(t, `
module sem.generics
data Event { kind: U64 }
data Box<T> { value: T }
data Pair<T> { first: T; second: T }
data Ring<T> { current: Box<T> }
data Root {
    box: Box<Event>
    ring: Ring<Event>
    pair: Pair<Box<Event>>
}
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	mustCompleteGenericInstantiations(t, index)
	box := index.Instantiations["sem.generics.Box[sem.generics.Event]"]
	if box.Fields[0].Type.Key() != "sem.generics.Event" {
		t.Fatalf("Box<Event>.value = %s", box.Fields[0].Type.Key())
	}
	ring := index.Instantiations["sem.generics.Ring[sem.generics.Event]"]
	if ring.Fields[0].Type.Key() != "sem.generics.Box[sem.generics.Event]" {
		t.Fatalf("Ring<Event>.current = %s", ring.Fields[0].Type.Key())
	}
	pair := index.Instantiations["sem.generics.Pair[sem.generics.Box[sem.generics.Event]]"]
	if pair.Fields[0].Type.Key() != "sem.generics.Box[sem.generics.Event]" {
		t.Fatalf("Pair<Box<Event>>.first = %s", pair.Fields[0].Type.Key())
	}
}

func TestGenericMethodReturnInstantiationCompletesMethods(t *testing.T) {
	modules := parseModulesForTest(t, `
module machine.x86_64.executor_memory
data Event { kind: U64 }
data MutableSlice<T> {
    address: U64
    length: U64
    asm fn get(self, index: U64) -> T {}
}
data Slots<T> {
    asm fn fill(self, value: T) -> MutableSlice<T> {}
}
class Worker {
    fn run(self, slots: Slots<Event>) {
        let mutable = slots.fill(value = Event(kind = 1))
        let event = mutable.get(index = 0)
    }
}
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	_, ds := checkAllowingMissingImage(t, index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}
}

func TestGenericArityMismatch(t *testing.T) {
	modules := parseModulesForTest(t, `
module sem.generics
data Box<T> { value: T }
data Bad { value: Box<U64, U64> }
`)
	index, indexDiags := BuildIndex(modules)
	_, checkDiags := Check(index, modules)
	ds := append(indexDiags, checkDiags...)
	if !hasCode(ds, diag.SEM0077) {
		t.Fatalf("diagnostics = %#v, want SEM0077", ds)
	}
}

func TestDuplicateGenericTypeParameterDiagnostic(t *testing.T) {
	modules := parseModulesForTest(t, `
module sem.generics
data Bad<T, T> { value: T }
`)
	index, indexDiags := BuildIndex(modules)
	_, checkDiags := Check(index, modules)
	ds := append(indexDiags, checkDiags...)
	if !hasCode(ds, diag.SEM0076) {
		t.Fatalf("diagnostics = %#v, want SEM0076", ds)
	}
}

func TestUnknownGenericTypeArgumentDiagnostic(t *testing.T) {
	modules := parseModulesForTest(t, `
module sem.generics
data Box<T> { value: T }
data Bad { value: Box<MissingPayload> }
`)
	index, indexDiags := BuildIndex(modules)
	_, checkDiags := Check(index, modules)
	ds := append(indexDiags, checkDiags...)
	if !hasCode(ds, diag.SEM0078) {
		t.Fatalf("diagnostics = %#v, want SEM0078", ds)
	}
}
