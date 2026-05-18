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
