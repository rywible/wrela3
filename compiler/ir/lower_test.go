package ir

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/sem"
)

func TestLowerReturnsCG0001ForNilProgram(t *testing.T) {
	_, diags := Lower(nil)
	if len(diags) != 1 || diags[0].Code != diag.CG0001 {
		t.Fatalf("diags = %#v, want one CG0001", diags)
	}
}

func TestLowerSynthesizesEntryAdapterFromImage(t *testing.T) {
	image := &sem.Type{Module: "m", Name: "Boot", Kind: sem.KindImage}
	checked := &sem.CheckedProgram{
		Index: &sem.Index{ByModule: map[string]map[string]*sem.Type{
			"m": {"Boot": image},
		}},
	}
	program, diags := Lower(checked)
	if len(diags) != 0 {
		t.Fatalf("diags = %#v", diags)
	}
	if program.Entry.Symbol != "_wrela_efi_entry" {
		t.Fatalf("entry symbol = %q", program.Entry.Symbol)
	}
	if program.Entry.DelegatedPhaseSymbol == "" || program.Entry.OwnedPhaseSymbol == "" {
		t.Fatalf("entry phase symbols not set: %#v", program.Entry)
	}
}
