package sem

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
)

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
