package parse

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ryanwible/wrela3/compiler/source"
)

func TestCoreLanguageSourceParses(t *testing.T) {
	repoRoot := repoRootForParseTest(t)
	path := filepath.Join(repoRoot, "wrela/lang/core.wrela")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read core source: %v", err)
	}
	modules, ds := ParseGraph(source.Graph{Files: []*source.File{
		source.NewFile(1, path, string(raw)),
	}})
	if len(ds) != 0 {
		t.Fatalf("parse diagnostics: %#v", ds)
	}
	if len(modules) != 1 || modules[0].Name != "wrela.lang.core" {
		t.Fatalf("modules = %#v, want wrela.lang.core", modules)
	}
}

func repoRootForParseTest(t *testing.T) string {
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
