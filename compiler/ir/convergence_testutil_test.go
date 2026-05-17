package ir

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ryanwible/wrela3/compiler/parse"
	"github.com/ryanwible/wrela3/compiler/sem"
	"github.com/ryanwible/wrela3/compiler/source"
)

func checkedProgramFromSourceForTest(t *testing.T, sourceText string) *sem.CheckedProgram {
	t.Helper()
	dir := t.TempDir()
	rootPath := filepath.Join(dir, "main.wrela")
	if err := os.WriteFile(rootPath, []byte(sourceText), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	repoRoot := repoRootForIRTest(t)
	graph, err := source.LoadGraph(source.Options{
		RootPath:    rootPath,
		ImportRoots: []string{repoRoot, filepath.Join(repoRoot, "wrela")},
	})
	if err != nil {
		t.Fatalf("load graph: %v", err)
	}
	modules, ds := parse.ParseGraph(*graph)
	if len(ds) != 0 {
		t.Fatalf("parse diagnostics: %#v", ds)
	}
	index, ds := sem.BuildIndex(modules)
	if len(ds) != 0 {
		t.Fatalf("index diagnostics: %#v", ds)
	}
	checked, ds := sem.Check(index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}
	return checked
}

func repoRootForIRTest(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			return wd
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			t.Fatalf("could not find repo root from %s", wd)
		}
		wd = parent
	}
}
