package diag_test

import (
	"strings"
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
)

func TestSortDiagnosticsStable(t *testing.T) {
	ds := []diag.Diagnostic{
		{Phase: "sem", FilePath: "b.wrela", Start: 10, End: 11, Code: diag.SEM0009, Sequence: 2},
		{Phase: "parse", FilePath: "a.wrela", Start: 2, End: 3, Code: diag.PAR0001, Sequence: 1},
	}
	diag.Sort(ds)
	if ds[0].Code != diag.PAR0001 || ds[1].Code != diag.SEM0009 {
		t.Fatalf("sorted codes = %s, %s", ds[0].Code, ds[1].Code)
	}
}

func TestRenderIncludesLocationCodeAndMessage(t *testing.T) {
	out := diag.Render([]diag.Diagnostic{{
		Severity: diag.Error,
		Phase:    "parse",
		FilePath: "a.wrela",
		Start:    2,
		End:      3,
		Code:     diag.PAR0001,
		Message:  "unexpected token",
	}})
	for _, want := range []string{"a.wrela:2-3", "error", "PAR0001", "unexpected token"} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q in %q", want, out)
		}
	}
}

func TestRenderSkipsCodeAndFileAndMessageFormatting(t *testing.T) {
	out := diag.Render([]diag.Diagnostic{{
		Severity: diag.Warning,
		Phase:    "parse",
		FilePath: "x.wrela",
		Start:    5,
		End:      7,
		Code:     diag.PAR0001,
		Message:  "needs cleanup",
		Sequence: 99,
	}})
	if !strings.HasPrefix(out, "x.wrela:5-7: warning PAR0001: needs cleanup\n") {
		t.Fatalf("unexpected render output: %q", out)
	}
}

func TestMemoryDiagnosticCodesExist(t *testing.T) {
	codes := []string{
		diag.SEM0021, diag.SEM0022, diag.SEM0023, diag.SEM0024,
		diag.SEM0025, diag.SEM0026, diag.SEM0027, diag.SEM0028,
		diag.SEM0029, diag.SEM0030, diag.SEM0031, diag.SEM0032,
	}
	for _, code := range codes {
		if code == "" {
			t.Fatalf("memory diagnostic code must not be empty")
		}
	}
}
