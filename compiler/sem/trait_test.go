package sem

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
)

func TestTraitImplSatisfiesGenericBound(t *testing.T) {
	modules := parseModulesForTest(t, `
module sem.traits
trait Producer<T> {
	fn next(self) -> T
}
data Event { kind: U64 }
class EventSub {
	fn next(self) -> Event {
		return Event(kind = 1)
	}
}
impl Producer<Event> for EventSub
class Drain<S, T> where S: Producer<T> {
	input: S
	fn poll(self) -> T {
		return self.input.next()
	}
}
data Root { drain: Drain<EventSub, Event> }
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	_, ds := checkAllowingMissingImage(t, index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}
}

func TestMissingTraitImplDiagnostic(t *testing.T) {
	modules := parseModulesForTest(t, `
module sem.traits
trait Producer<T> { fn next(self) -> T }
data Event { kind: U64 }
class BadSub {}
class Drain<S, T> where S: Producer<T> { input: S }
data Root { drain: Drain<BadSub, Event> }
`)
	index, indexDiags := BuildIndex(modules)
	_, checkDiags := Check(index, modules)
	ds := append(indexDiags, checkDiags...)
	if !hasCode(ds, diag.SEM0081) {
		t.Fatalf("diagnostics = %#v, want SEM0081", ds)
	}
}

func TestOverlappingGenericImplDiagnostic(t *testing.T) {
	modules := parseModulesForTest(t, `
module sem.traits
trait Publisher<T> { fn publish(self, value: T) }
data Event { kind: U64 }
class EventPublisher { fn publish(self, value: Event) {} }
impl Publisher<Event> for EventPublisher
impl Publisher<Event> for EventPublisher
`)
	index, indexDiags := BuildIndex(modules)
	_, checkDiags := Check(index, modules)
	ds := append(indexDiags, checkDiags...)
	if !hasCode(ds, diag.SEM0083) {
		t.Fatalf("diagnostics = %#v, want SEM0083", ds)
	}
}

func TestTraitImplSignatureMismatchDiagnostic(t *testing.T) {
	modules := parseModulesForTest(t, `
module sem.traits
trait Producer<T> { fn next(self) -> T }
data Event { kind: U64 }
class BadSub {
	fn next(self) -> U64 {
		return 1
	}
}
impl Producer<Event> for BadSub
`)
	index, indexDiags := BuildIndex(modules)
	_, checkDiags := Check(index, modules)
	ds := append(indexDiags, checkDiags...)
	if !hasCode(ds, diag.SEM0082) {
		t.Fatalf("diagnostics = %#v, want SEM0082", ds)
	}
}

func TestGenericImplPatternSatisfiesConcreteBound(t *testing.T) {
	modules := parseModulesForTest(t, `
module sem.traits
trait Producer<T> { fn next(self) -> T }
data Event { kind: U64 }
class Box<T> {
	value: T
	fn next(self) -> T {
		return self.value
	}
}
impl Producer<T> for Box<T>
class Drain<S, T> where S: Producer<T> { input: S }
data Root { drain: Drain<Box<Event>, Event> }
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	_, ds := checkAllowingMissingImage(t, index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}
}
