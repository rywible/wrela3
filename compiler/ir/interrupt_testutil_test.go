package ir

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/parse"
	"github.com/ryanwible/wrela3/compiler/sem"
	"github.com/ryanwible/wrela3/compiler/source"
)

func checkedProgramForTest(t *testing.T, sourceText string) *sem.CheckedProgram {
	t.Helper()
	file := source.NewFile(1, "interrupt_test.wrela", sourceText)
	modules, ds := parse.ParseGraph(source.Graph{Files: []*source.File{file}})
	if len(ds) != 0 {
		t.Fatalf("parse diagnostics: %#v", ds)
	}
	index, ds := sem.BuildIndex(modules)
	ds = filterMissingImageDiagnostic(ds)
	if len(ds) != 0 {
		t.Fatalf("index diagnostics: %#v", ds)
	}
	checked, ds := sem.Check(index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}
	return checked
}

func filterMissingImageDiagnostic(ds []diag.Diagnostic) []diag.Diagnostic {
	out := ds[:0]
	for _, d := range ds {
		if d.Code == diag.SEM0004 {
			continue
		}
		out = append(out, d)
	}
	return out
}
