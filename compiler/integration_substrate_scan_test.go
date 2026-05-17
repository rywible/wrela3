package compiler

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProductionSubstrateRegressionScans(t *testing.T) {
	scans := []struct {
		name      string
		root      string
		forbidden []string
	}{
		{name: "raw memory shortcuts", root: "examples", forbidden: []string{"MutableBytes(address = 0x", "arena_base = 0x"}},
		{name: "hidden scheduler", root: "wrela", forbidden: []string{"class Scheduler", "RunnableQueue", "work_steal", "spawn_on_any_cpu"}},
		{name: "q35 assumptions", root: "wrela", forbidden: []string{"q35", "two-vCPU", "static q35"}},
	}
	for _, scan := range scans {
		t.Run(scan.name, func(t *testing.T) {
			text := readTreeForScan(t, scan.root)
			for _, forbidden := range scan.forbidden {
				if strings.Contains(text, forbidden) {
					t.Fatalf("%s contains forbidden %q", scan.root, forbidden)
				}
			}
		})
	}
}

func readTreeForScan(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	repoRoot := filepath.Clean(filepath.Join(".."))
	err := filepath.WalkDir(filepath.Join(repoRoot, root), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "build" || d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".wrela") && !strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, ".md") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		b.Write(data)
		b.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatalf("scan %s: %v", root, err)
	}
	return b.String()
}
