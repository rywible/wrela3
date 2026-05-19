package compiler

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/parse"
	"github.com/ryanwible/wrela3/compiler/sem"
	"github.com/ryanwible/wrela3/compiler/source"
)

func TestBuildRejectsReleaseMode(t *testing.T) {
	_, err := Build(BuildOptions{
		Mode:       ModeRelease,
		RootPath:   "examples/hello/main.wrela",
		OutputPath: "build/hello.efi",
		RepoRoot:   ".",
	})
	ce, ok := err.(CodeError)
	if !ok {
		t.Fatalf("error = %T, want CodeError", err)
	}
	if ce.Code != "CLI0002" {
		t.Fatalf("code = %s, want CLI0002", ce.Code)
	}
}

func TestBuildRequiresRootAndOutput(t *testing.T) {
	_, err := Build(BuildOptions{Mode: ModeDev, OutputPath: "build/out.efi", RepoRoot: "."})
	if ce := err.(CodeError); ce.Code != "CLI0003" {
		t.Fatalf("code = %s, want CLI0003", ce.Code)
	}

	_, err = Build(BuildOptions{Mode: ModeDev, RootPath: "main.wrela", RepoRoot: "."})
	if ce := err.(CodeError); ce.Code != "CLI0004" {
		t.Fatalf("code = %s, want CLI0004", ce.Code)
	}
}

func TestBuildWritesReportWhenRequested(t *testing.T) {
	dir := t.TempDir()
	result, err := Build(BuildOptions{
		Mode:       ModeDev,
		RootPath:   "examples/hello/main.wrela",
		OutputPath: filepath.Join(dir, "hello.efi"),
		ReportPath: filepath.Join(dir, "hello.report.json"),
		RepoRoot:   ".",
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if result.ReportPath == "" || result.Report == nil {
		t.Fatalf("BuildResult missing report: %#v", result)
	}
	data, err := os.ReadFile(result.ReportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if !bytes.Contains(data, []byte(`"authority_audit"`)) {
		t.Fatalf("report missing authority audit:\n%s", data)
	}
}

func TestBuildImportRootsLoadCoreLanguageImports(t *testing.T) {
	dir := t.TempDir()
	repoRoot := resolveRepoRoot(".")
	root := filepath.Join(dir, "main.wrela")
	if err := os.WriteFile(root, []byte(`
module test.core_build
use { Option } from wrela.lang.core
data Event { kind: U64 }
data Holder { next: Option<Event> }
`), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	graph, err := source.LoadGraph(source.Options{
		RootPath: root,
		ImportRoots: []string{
			repoRoot,
			filepath.Join(repoRoot, "wrela"),
		},
	})
	if err != nil {
		t.Fatalf("LoadGraph: %v", err)
	}
	modules, ds := parse.ParseGraph(*graph)
	if len(ds) != 0 {
		t.Fatalf("parse diagnostics: %#v", ds)
	}
	index, ds := sem.BuildIndex(modules)
	filtered := ds[:0]
	for _, d := range ds {
		if d.Code != diag.SEM0004 {
			filtered = append(filtered, d)
		}
	}
	if len(filtered) != 0 {
		t.Fatalf("index diagnostics: %#v", ds)
	}
	if _, ok := index.Lookup("wrela.lang.core", "Option"); !ok {
		t.Fatalf("core Option was not loaded through build import roots")
	}
}
