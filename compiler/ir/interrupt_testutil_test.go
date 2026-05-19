package ir

import (
	"strings"
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/parse"
	"github.com/ryanwible/wrela3/compiler/sem"
	"github.com/ryanwible/wrela3/compiler/source"
)

func checkedProgramForTest(t *testing.T, sourceText string) *sem.CheckedProgram {
	t.Helper()
	files := make([]*source.File, 0, 1)
	for _, moduleSource := range splitSourceModulesForIRTest(sourceText) {
		files = append(files, source.NewFile(source.FileID(len(files)+1), "interrupt_test.wrela", moduleSource))
	}
	modules, ds := parse.ParseGraph(source.Graph{Files: files})
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

func splitSourceModulesForIRTest(sourceText string) []string {
	parts := strings.Split(sourceText, "\nmodule ")
	out := make([]string, 0, len(parts))
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if i != 0 {
			part = "module " + part
		}
		out = append(out, part)
	}
	return out
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
