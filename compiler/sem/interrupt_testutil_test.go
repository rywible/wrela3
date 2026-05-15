package sem

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
)

func buildIndexForTest(t *testing.T, sourceText string) (*Index, []diag.Diagnostic) {
	t.Helper()
	modules := parseModulesForTest(t, sourceText)
	index, ds := BuildIndex(modules)
	return index, filterMissingImageDiagnostic(ds)
}

func checkModuleForTest(t *testing.T, sourceText string) (*CheckedProgram, []diag.Diagnostic) {
	t.Helper()
	modules := parseModulesForTest(t, sourceText)
	index, ds := BuildIndex(modules)
	ds = filterMissingImageDiagnostic(ds)
	if len(ds) != 0 {
		return nil, ds
	}
	return Check(index, modules)
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
