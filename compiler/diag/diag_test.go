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

func TestHardwareDiscoveryDiagnosticCodesExist(t *testing.T) {
	codes := []string{
		diag.SEM0049, diag.SEM0050, diag.SEM0051,
		diag.SEM0052, diag.SEM0053, diag.SEM0054, diag.SEM0055,
	}
	for _, code := range codes {
		if code == "" {
			t.Fatalf("hardware diagnostic code must not be empty")
		}
	}
}

func TestStorageDiagnosticCodesExist(t *testing.T) {
	codes := []struct {
		name string
		got  string
		want string
	}{
		{"SEM0099", diag.SEM0099, "SEM0099"},
		{"SEM0100", diag.SEM0100, "SEM0100"},
		{"SEM0101", diag.SEM0101, "SEM0101"},
		{"SEM0102", diag.SEM0102, "SEM0102"},
		{"SEM0103", diag.SEM0103, "SEM0103"},
		{"SEM0104", diag.SEM0104, "SEM0104"},
		{"SEM0105", diag.SEM0105, "SEM0105"},
		{"SEM0106", diag.SEM0106, "SEM0106"},
		{"SEM0107", diag.SEM0107, "SEM0107"},
		{"SEM0108", diag.SEM0108, "SEM0108"},
		{"SEM0109", diag.SEM0109, "SEM0109"},
		{"SEM0110", diag.SEM0110, "SEM0110"},
		{"SEM0111", diag.SEM0111, "SEM0111"},
		{"SEM0112", diag.SEM0112, "SEM0112"},
		{"SEM0113", diag.SEM0113, "SEM0113"},
		{"SEM0114", diag.SEM0114, "SEM0114"},
		{"SEM0115", diag.SEM0115, "SEM0115"},
		{"SEM0116", diag.SEM0116, "SEM0116"},
		{"SEM0117", diag.SEM0117, "SEM0117"},
		{"SEM0118", diag.SEM0118, "SEM0118"},
		{"SEM0119", diag.SEM0119, "SEM0119"},
		{"SEM0120", diag.SEM0120, "SEM0120"},
		{"SEM0121", diag.SEM0121, "SEM0121"},
		{"SEM0122", diag.SEM0122, "SEM0122"},
		{"SEM0123", diag.SEM0123, "SEM0123"},
		{"SEM0124", diag.SEM0124, "SEM0124"},
	}
	seen := map[string]string{}
	for _, code := range codes {
		if code.got != code.want {
			t.Fatalf("%s = %q, want %q", code.name, code.got, code.want)
		}
		if previous, ok := seen[code.got]; ok {
			t.Fatalf("%s duplicates %s with value %q", code.name, previous, code.got)
		}
		seen[code.got] = code.name
	}
}
