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
