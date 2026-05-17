package sem

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/report"
)

func checkTrustedPlatformSourceForTest(t *testing.T, name string, sourceText string) (*CheckedProgram, []diag.Diagnostic) {
	t.Helper()
	if !strings.HasPrefix(sourceText, "module platform.") && !strings.HasPrefix(sourceText, "\nmodule platform.") {
		sourceText = strings.Replace(sourceText, "module examples.", "module platform.test.", 1)
	}
	modules := append(parseUEFIModuleSet(t), parseModulesForTest(t, sourceText)...)
	index, ds := BuildIndex(modules)
	if len(ds) != 0 {
		t.Fatalf("%s index diagnostics: %#v", name, ds)
	}
	return Check(index, modules)
}

func checkedProgramFromSourceForTest(t *testing.T, sourceText string) *CheckedProgram {
	t.Helper()
	modules := append(parseUEFIModuleSet(t), parseModulesForTest(t, sourceText)...)
	index, ds := BuildIndex(modules)
	if len(ds) != 0 {
		t.Fatalf("index diagnostics: %#v", ds)
	}
	checked, ds := Check(index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}
	return checked
}

func parseNegativeFixtureForConvergenceTest(t *testing.T, fixture string) []*ast.Module {
	t.Helper()
	return parseFixtureModulesForTest(t, filepath.Join("tests", "fixtures", "negative", fixture))
}

func containsPlacementFallback(placements []report.PlacementReport, fallback string) bool {
	for _, placement := range placements {
		if placement.Fallback == fallback {
			return true
		}
	}
	return false
}
