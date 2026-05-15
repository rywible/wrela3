package sem

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/diag"
)

func TestIndex(t *testing.T) {
	t.Run("same-module lookup", func(t *testing.T) {
		modules := parseModulesForTest(t, `
module index.same

data Foo {
    value: U64
}
`)
		idx, ds := BuildIndex(modules)
		if len(ds) != 1 || !hasCode(ds, diag.SEM0004) {
			t.Fatalf("expected only SEM0004 for missing image, got %#v", ds)
		}
		if _, ok := idx.Lookup("index.same", "Foo"); !ok {
			t.Fatalf("missing local declaration Foo")
		}
	})

	t.Run("imported-name lookup", func(t *testing.T) {
		base := parseModulesForTest(t, `
module index.base

data Base { value: U64 }
`)
		user := parseModulesForTest(t, `
module index.user
use { Base } from index.base

data User {
    base: Base
}
`)
		modules := append(base, user...)
		idx, ds := BuildIndex(modules)
		if len(ds) != 1 || !hasCode(ds, diag.SEM0004) {
			t.Fatalf("expected only SEM0004 for missing image, got %#v", ds)
		}
		if _, ok := idx.Lookup("index.user", "Base"); !ok {
			t.Fatalf("missing imported declaration Base")
		}
	})

	t.Run("duplicate declaration", func(t *testing.T) {
		modules := parseModulesForTest(t, `
module index.dup

class Foo {}
class Foo {}
`)
		_, ds := BuildIndex(modules)
		if !hasCode(ds, diag.SEM0001) {
			t.Fatalf("expected SEM0001, got %#v", ds)
		}
	})

	t.Run("duplicate imported name", func(t *testing.T) {
		base := parseModulesForTest(t, `
module index.dupbase

class Base {}
`)
		imp := parseModulesForTest(t, `
module index.dupimport
use { Base } from index.dupbase

class Base {}
`)
		modules := []*ast.Module{
			base[0],
			imp[0],
		}
		_, ds := BuildIndex(modules)
		if !hasCode(ds, diag.SEM0001) {
			t.Fatalf("expected SEM0001, got %#v", ds)
		}
	})

	t.Run("missing image", func(t *testing.T) {
		modules := parseModulesForTest(t, `
module index.noimage
data OnlyType {}
`)
		_, ds := BuildIndex(modules)
		if len(ds) != 1 || ds[0].Code != diag.SEM0004 {
			t.Fatalf("expected SEM0004, got %#v", ds)
		}
	})

	t.Run("multiple images", func(t *testing.T) {
		modules := append(
			parseModulesForTest(t, `
module index.image_one
image One {}
`),
			parseModulesForTest(t, `
module index.image_two
image Two {}
`)...,
		)
		_, ds := BuildIndex(modules)
		if !hasCode(ds, diag.SEM0003) {
			t.Fatalf("expected SEM0003, got %#v", ds)
		}
	})

	t.Run("string literal fields", func(t *testing.T) {
		modules := parseModulesForTest(t, `
module index.stringliteral
`)
		idx, ds := BuildIndex(modules)
		if len(ds) != 1 || !hasCode(ds, diag.SEM0004) {
			t.Fatalf("expected only SEM0004, got %#v", ds)
		}
		st, ok := idx.primitives["StringLiteral"]
		if !ok {
			t.Fatalf("missing StringLiteral primitive")
		}
		if len(st.Fields) != 2 || st.Fields[0].Name != "address" || st.Fields[1].Name != "length" {
			t.Fatalf("unexpected StringLiteral fields %#v", st.Fields)
		}
		if st.Fields[0].Type.Name != "VirtualAddress" || st.Fields[1].Type.Name != "U64" {
			t.Fatalf("unexpected StringLiteral field types %#v", st.Fields)
		}
	})
}

func TestInterruptEventsAndOnHandlersIndexed(t *testing.T) {
	modules := parseModulesForTest(t, `
module index.interrupt_symbols

data SerialInterrupt { vector: U8 }

driver path SerialConsolePath {
    interrupt receiver -> SerialInterrupt {
        return SerialInterrupt(vector = 1)
    }
}

executor ConsoleExec {
    serial: SerialConsolePath

    on serial.interrupt(event: SerialInterrupt) {}
}
`)
	idx, ds := BuildIndex(modules)
	ds = filterMissingImageDiagnostic(ds)
	if len(ds) != 0 {
		t.Fatalf("index diagnostics: %#v", ds)
	}
	if got := idx.InterruptEvent("index.interrupt_symbols", "SerialConsolePath"); got == nil || got.EventType != "SerialInterrupt" {
		t.Fatalf("interrupt event = %#v, want SerialInterrupt", got)
	}
	if got := idx.OnHandler("index.interrupt_symbols", "ConsoleExec", "serial"); got == nil || got.ParamType != "SerialInterrupt" {
		t.Fatalf("on handler = %#v, want SerialInterrupt", got)
	}
}

func TestInterruptEventsAndOnHandlersDuplicateRejected(t *testing.T) {
	modules := parseModulesForTest(t, `
module index.interrupt_duplicates

data SerialInterrupt { vector: U8 }

driver path SerialConsolePath {
    interrupt receiver -> SerialInterrupt {
        return SerialInterrupt(vector = 1)
    }

    interrupt receiver -> SerialInterrupt {
        return SerialInterrupt(vector = 2)
    }
}

executor ConsoleExec {
    serial: SerialConsolePath

    on serial.interrupt(event: SerialInterrupt) {}
    on serial.interrupt(event: SerialInterrupt) {}
}
`)
	_, ds := BuildIndex(modules)
	ds = filterMissingImageDiagnostic(ds)
	if !hasCode(ds, diag.SEM0014) {
		t.Fatalf("expected SEM0014, got %#v", ds)
	}
}
