package lex

import (
	"reflect"
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
)

func kinds(toks []Token) []Kind {
	out := make([]Kind, 0, len(toks))
	for _, tok := range toks {
		out = append(out, tok.Kind)
	}
	return out
}

func TestLexPhaseHeader(t *testing.T) {
	input := "phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {}"
	toks, ds := All(input)
	if len(ds) != 0 {
		t.Fatalf("diagnostics = %#v", ds)
	}
	got := kinds(toks)
	want := []Kind{
		KeywordPhase, Identifier, LParen, Identifier, Colon, Identifier,
		RParen, Arrow, Identifier, LBrace, RBrace, EOF,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("kinds = %#v, want %#v", got, want)
	}
}

func TestLexHexInteger(t *testing.T) {
	toks, ds := All("0x03f8")
	if len(ds) != 0 {
		t.Fatalf("diagnostics = %#v", ds)
	}
	if got, want := toks[0].Kind, Integer; got != want {
		t.Fatalf("kind = %v, want %v", got, want)
	}
	if toks[0].Text != "0x03f8" {
		t.Fatalf("literal = %q, want %q", toks[0].Text, "0x03f8")
	}
}

func TestLexCommentsSkipped(t *testing.T) {
	toks, ds := All("a // comment\nb")
	if len(ds) != 0 {
		t.Fatalf("diagnostics = %#v", ds)
	}
	got := kinds(toks)
	want := []Kind{Identifier, Newline, Identifier, EOF}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("kinds = %#v, want %#v", got, want)
	}
}

func TestLexBadString(t *testing.T) {
	_, ds := All(`"unterminated`)
	if len(ds) == 0 {
		t.Fatalf("expected diagnostic")
	}
	if ds[0].Code != diag.PAR0001 {
		t.Fatalf("code = %s, want %s", ds[0].Code, diag.PAR0001)
	}
}

func TestLexStringEscapesNewline(t *testing.T) {
	toks, ds := All(`"hello\n"`)
	if len(ds) != 0 {
		t.Fatalf("diagnostics = %#v", ds)
	}
	if got, want := toks[0].Text, "hello\n"; got != want {
		t.Fatalf("string text = %q, want %q", got, want)
	}
}

func TestLexUnicodeIdentifierAdvancesByRune(t *testing.T) {
	toks, ds := All("café_β 1")
	if len(ds) != 0 {
		t.Fatalf("diagnostics = %#v", ds)
	}
	if got, want := toks[0].Text, "café_β"; got != want {
		t.Fatalf("identifier text = %q, want %q", got, want)
	}
	if got, want := toks[0].End, len("café_β"); got != want {
		t.Fatalf("identifier end = %d, want %d", got, want)
	}
	if got, want := toks[1].Kind, Integer; got != want {
		t.Fatalf("next token kind = %v, want %v", got, want)
	}
}

func TestInterruptEventKeywords(t *testing.T) {
	toks, ds := All("interrupt receiver on using interruptfoo receiverfoo oncall")
	if len(ds) != 0 {
		t.Fatalf("diagnostics = %#v", ds)
	}
	got := []Kind{toks[0].Kind, toks[1].Kind, toks[2].Kind, toks[3].Kind, toks[4].Kind, toks[5].Kind, toks[6].Kind}
	want := []Kind{KeywordInterrupt, KeywordReceiver, KeywordOn, Identifier, Identifier, Identifier, Identifier}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("kinds = %#v, want %#v", got, want)
	}
}

func TestWithKeyword(t *testing.T) {
	toks, ds := All("with memory.frame(length = 64) as tick {}")
	if len(ds) != 0 {
		t.Fatalf("lex diagnostics: %#v", ds)
	}
	if toks[0].Kind != KeywordWith || toks[0].Text != "with" {
		t.Fatalf("first token = %#v, want KeywordWith", toks[0])
	}
}

func TestLexInvalidUnicodeCharacterAdvancesByRune(t *testing.T) {
	toks, ds := All("→")
	if len(ds) != 1 {
		t.Fatalf("diagnostics = %#v, want 1 diagnostic", ds)
	}
	if got, want := ds[0].Start, 0; got != want {
		t.Fatalf("diagnostic start = %d, want %d", got, want)
	}
	if got, want := ds[0].End, len("→"); got != want {
		t.Fatalf("diagnostic end = %d, want %d", got, want)
	}
	if got, want := toks[0].Start, len("→"); got != want {
		t.Fatalf("EOF start = %d, want %d", got, want)
	}
}

func TestLanguageExpressivenessKeywords(t *testing.T) {
	src := "enum trait impl for where const static_assert match sizeof alignof"
	toks, ds := All(src)
	if len(ds) != 0 {
		t.Fatalf("diagnostics: %#v", ds)
	}
	want := []Kind{
		KeywordEnum,
		KeywordTrait,
		KeywordImpl,
		KeywordFor,
		KeywordWhere,
		KeywordConst,
		KeywordStaticAssert,
		KeywordMatch,
		KeywordSizeof,
		KeywordAlignof,
	}
	for i, want := range want {
		if toks[i].Kind != want {
			t.Fatalf("token %d = %#v, want %v", i, toks[i], want)
		}
	}
}

func TestStorageDeclarationKeywords(t *testing.T) {
	src := `event FileCreated id 1001 { layout 1 current {} }
projection DirectoryChildren id 12 {}`
	toks, ds := All(src)
	if len(ds) != 0 {
		t.Fatalf("diagnostics: %#v", ds)
	}
	if toks[0].Kind != KeywordEvent {
		t.Fatalf("token 0 = %#v, want KeywordEvent", toks[0])
	}
	if toks[2].Kind != Identifier || toks[2].Text != "id" {
		t.Fatalf("id token = %#v, want Identifier", toks[2])
	}
	if toks[5].Kind != Identifier || toks[5].Text != "layout" {
		t.Fatalf("layout token = %#v, want Identifier", toks[5])
	}
	if toks[7].Kind != Identifier || toks[7].Text != "current" {
		t.Fatalf("current token = %#v, want Identifier", toks[7])
	}
	if toks[12].Kind != KeywordProjection {
		t.Fatalf("projection token = %#v, want KeywordProjection", toks[12])
	}
	if toks[12].Text != "projection" {
		t.Fatalf("projection text = %q, want projection", toks[12].Text)
	}

	contextual, ds := All("id layout current upcast")
	if len(ds) != 0 {
		t.Fatalf("contextual diagnostics: %#v", ds)
	}
	for i, tok := range contextual[:4] {
		if tok.Kind != Identifier {
			t.Fatalf("contextual token %d = %#v, want Identifier", i, tok)
		}
	}
}

func TestFatArrowToken(t *testing.T) {
	toks, ds := All("Option.None => { }")
	if len(ds) != 0 {
		t.Fatalf("diagnostics: %#v", ds)
	}
	got := []Kind{toks[0].Kind, toks[1].Kind, toks[2].Kind, toks[3].Kind}
	want := []Kind{Identifier, Dot, Identifier, FatArrow}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("kinds = %#v, want %#v", got, want)
	}
}
