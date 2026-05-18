package sem

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
)

func TestEnumMatchExhaustiveAndBindings(t *testing.T) {
	modules := parseModulesForTest(t, `
module sem.enums
enum Option<T> { None Some(value: T) }
data Event { kind: U64 }
class Worker {
    fn handle(self, next: Option<Event>) {
        match next {
            Option.Some(value = event) => {
                let k = event.kind
            }
            Option.None => {
                let z = 0
            }
        }
    }
}
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	_, ds := checkAllowingMissingImage(t, index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}
}

func TestNonExhaustiveMatchDiagnostic(t *testing.T) {
	modules := parseModulesForTest(t, `
module sem.enums
enum Option<T> { None Some(value: T) }
data Event { kind: U64 }
class Worker {
    fn handle(self, next: Option<Event>) {
        match next {
            Option.Some(value = event) => {
                let k = event.kind
            }
        }
    }
}
`)
	index, indexDiags := BuildIndex(modules)
	_, checkDiags := Check(index, modules)
	ds := append(indexDiags, checkDiags...)
	if !hasCode(ds, diag.SEM0084) {
		t.Fatalf("diagnostics = %#v, want SEM0084", ds)
	}
}

func TestIfLetAndInvalidPatternDiagnostics(t *testing.T) {
	modules := parseModulesForTest(t, `
module sem.enums
enum Option<T> { None Some(value: T) }
data Event { kind: U64 }
class Worker {
    fn handle(self, next: Option<Event>) {
        if let Option.Some(value = event) = next {
            let k = event.kind
        }
        match next {
            Option.Some(value = one, value = two) => {}
            Option.None => {}
        }
    }
}
`)
	index, indexDiags := BuildIndex(modules)
	_, checkDiags := Check(index, modules)
	ds := append(indexDiags, checkDiags...)
	if !hasCode(ds, diag.SEM0095) {
		t.Fatalf("diagnostics = %#v, want SEM0095", ds)
	}
}

func TestVariantConstructorExpectedTypeInference(t *testing.T) {
	modules := parseModulesForTest(t, `
module sem.enums
enum Option<T> { None Some(value: T) }
data Event { kind: U64 }
class Worker {
    fn none(self) -> Option<Event> {
        return Option.None()
    }
    fn some(self) -> Option<Event> {
        return Option.Some(value = Event(kind = 1))
    }
}
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	_, ds := checkAllowingMissingImage(t, index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}
}

func TestVariantConstructorMissingInferenceDiagnostic(t *testing.T) {
	modules := parseModulesForTest(t, `
module sem.enums
enum Option<T> { None Some(value: T) }
class Worker {
    fn bad(self) {
        let none = Option.None()
    }
}
`)
	index, indexDiags := BuildIndex(modules)
	_, checkDiags := Check(index, modules)
	ds := append(indexDiags, checkDiags...)
	if !hasCode(ds, diag.SEM0079) {
		t.Fatalf("diagnostics = %#v, want SEM0079", ds)
	}
}
