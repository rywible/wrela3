package sem

import (
	"fmt"
	"testing"

	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/parse"
	"github.com/ryanwible/wrela3/compiler/source"
)

func hasCode(ds []diag.Diagnostic, code string) bool {
	for _, d := range ds {
		if d.Code == code {
			return true
		}
	}
	return false
}

func parseModulesForTest(t *testing.T, sources ...string) []*ast.Module {
	t.Helper()
	files := make([]*source.File, len(sources))
	for i, sourceText := range sources {
		files[i] = source.NewFile(source.FileID(i+1), fmt.Sprintf("m%d.wrela", i), sourceText)
	}
	modules, ds := parse.ParseGraph(source.Graph{Files: files})
	if len(ds) != 0 {
		t.Fatalf("parse diagnostics: %#v", ds)
	}
	return modules
}

func mustBuildIndex(t *testing.T, modules []*ast.Module) *Index {
	t.Helper()
	index, ds := BuildIndex(modules)
	if len(ds) != 0 {
		t.Fatalf("index diagnostics: %#v", ds)
	}
	return index
}

func mustCheck(t *testing.T, index *Index, modules []*ast.Module) *CheckedProgram {
	t.Helper()
	checked, ds := Check(index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}
	return checked
}

func typeDiagsForModules(t *testing.T, sourceText string) (program *CheckedProgram, diags []diag.Diagnostic) {
	t.Helper()
	modules := parseModulesForTest(t, sourceText)
	index, ds := BuildIndex(modules)
	if len(ds) != 0 && (len(ds) != 1 || ds[0].Code != diag.SEM0003) {
		t.Fatalf("index diagnostics: %#v", ds)
	}
	return Check(index, modules)
}
