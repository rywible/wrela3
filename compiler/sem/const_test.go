package sem

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
)

func TestConstSizeofAlignofAndStaticAssert(t *testing.T) {
	modules := parseModulesForTest(t, `
module sem.consts
data Event { kind: U64; ready: Bool }
const EVENT_CAPACITY: U64 = 128
const EVENT_BYTES: U64 = sizeof(Event) * EVENT_CAPACITY
const EVENT_ALIGN: U64 = alignof(Event)
static_assert(EVENT_BYTES == 2048, message = "event byte size")
static_assert(EVENT_ALIGN == 8, message = "event align")
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	_, ds := checkAllowingMissingImage(t, index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}
	if got := index.ConstValue("sem.consts", "EVENT_BYTES"); got != 2048 {
		t.Fatalf("EVENT_BYTES = %d, want 2048", got)
	}
}

func TestConstSizeofAlignofEnum(t *testing.T) {
	modules := parseModulesForTest(t, `
module sem.consts
enum Maybe<T> {
    None
    Some(value: T)
}
const MAYBE_SIZE: U64 = sizeof(Maybe<U8>)
const MAYBE_ALIGN: U64 = alignof(Maybe<U8>)
static_assert(MAYBE_SIZE >= 8, message = "enum size")
static_assert(MAYBE_ALIGN == 8, message = "enum align")
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	_, ds := checkAllowingMissingImage(t, index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}
}

func TestConstSizeofEmptyEnumMatchesIRStorageLayout(t *testing.T) {
	modules := parseModulesForTest(t, `
module sem.consts
enum Flag {
    Off
    On
}
const FLAG_SIZE: U64 = sizeof(Flag)
const FLAG_ALIGN: U64 = alignof(Flag)
static_assert(FLAG_SIZE == 8, message = "empty enum is tag-sized")
static_assert(FLAG_ALIGN == 8, message = "empty enum align")
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	_, ds := checkAllowingMissingImage(t, index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}
	if got := index.ConstValue("sem.consts", "FLAG_SIZE"); got != 8 {
		t.Fatalf("FLAG_SIZE = %d, want 8", got)
	}
}

func TestConstSizeofAlignofEmptyAndNestedData(t *testing.T) {
	modules := parseModulesForTest(t, `
module sem.consts
data Empty {}
data Inner {
    payload: U64
}
data Middle {
    inner: Inner
}
data Outer {
    middle: Middle
    marker: U8
}
const EMPTY_SIZE: U64 = sizeof(Empty)
const EMPTY_ALIGN: U64 = alignof(Empty)
const NESTED_SIZE: U64 = sizeof(Outer)
const NESTED_ALIGN: U64 = alignof(Outer)
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	_, ds := checkAllowingMissingImage(t, index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}
	if got := index.ConstValue("sem.consts", "EMPTY_SIZE"); got != 8 {
		t.Fatalf("EMPTY_SIZE = %d, want 8", got)
	}
	if got := index.ConstValue("sem.consts", "EMPTY_ALIGN"); got != 8 {
		t.Fatalf("EMPTY_ALIGN = %d, want 8", got)
	}
	if got := index.ConstValue("sem.consts", "NESTED_SIZE"); got != 32 {
		t.Fatalf("NESTED_SIZE = %d, want 32", got)
	}
	if got := index.ConstValue("sem.consts", "NESTED_ALIGN"); got != 8 {
		t.Fatalf("NESTED_ALIGN = %d, want 8", got)
	}
}

func TestConstOverflowDiagnostic(t *testing.T) {
	modules := parseModulesForTest(t, `
module sem.consts
const BAD: U64 = 18446744073709551615 + 1
`)
	index, indexDiags := BuildIndex(modules)
	_, checkDiags := Check(index, modules)
	ds := append(indexDiags, checkDiags...)
	if !hasCode(ds, diag.SEM0086) {
		t.Fatalf("diagnostics = %#v, want SEM0086", ds)
	}
}

func TestImportedConstReference(t *testing.T) {
	modules := parseModulesForTest(t, `
module sem.consts.consumer
use { BASE } from sem.consts.source
const DOUBLE: U64 = BASE * 2
static_assert(DOUBLE == 128, message = "imported const")
`, `
module sem.consts.source
const BASE: U64 = 64
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	_, ds := checkAllowingMissingImage(t, index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}
	if got := index.ConstValue("sem.consts.consumer", "DOUBLE"); got != 128 {
		t.Fatalf("DOUBLE = %d, want 128", got)
	}
}
