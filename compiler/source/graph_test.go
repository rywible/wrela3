package source_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ryanwible/wrela3/compiler"
	"github.com/ryanwible/wrela3/compiler/source"
)

func TestExtractHeader(t *testing.T) {
	module, imports, err := source.ExtractHeader(`module examples.hello.main
use { HelloWorld } from examples.hello.program
use { SerialDriver, SerialWritePath } from machine.x86_64.serial
image HelloSerial {}`)
	if err != nil {
		t.Fatal(err)
	}
	if module != "examples.hello.main" {
		t.Fatalf("module = %s", module)
	}
	want := []string{"examples.hello.program", "machine.x86_64.serial"}
	if !reflect.DeepEqual(imports, want) {
		t.Fatalf("imports = %#v", imports)
	}
}

func writeTempSource(t *testing.T, dir, path, sourceText string) {
	t.Helper()
	full := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(sourceText), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestLoadGraphMissingModule(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "main.wrela")
	if err := os.WriteFile(root, []byte("module root\nuse { Missing } from missing.mod"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := source.LoadGraph(source.Options{
		RootPath:    root,
		ImportRoots: []string{dir},
	})
	ce, ok := err.(compiler.CodeError)
	if !ok {
		t.Fatalf("err = %T, want compiler.CodeError", err)
	}
	if ce.Code != "SRC0003" {
		t.Fatalf("code = %s, want SRC0003", ce.Code)
	}
}

func TestLoadGraphAllowsSharedImports(t *testing.T) {
	dir := t.TempDir()
	writeTempSource(t, dir, "a/b/c.wrela", `module a.b.c`)
	writeTempSource(t, dir, "a/b/d.wrela", `module a.b.d
use { } from a.b.c`)
	writeTempSource(t, dir, "main.wrela", `module root.main
use { C } from a.b.c
use { C2 } from a.b.d`)
	graph, err := source.LoadGraph(source.Options{
		RootPath:    filepath.Join(dir, "main.wrela"),
		ImportRoots: []string{dir},
	})
	if err != nil {
		t.Fatalf("LoadGraph: %v", err)
	}
	if len(graph.Files) != 3 {
		t.Fatalf("files = %d, want 3", len(graph.Files))
	}
}

func TestLoadGraphDuplicateModule(t *testing.T) {
	dir := t.TempDir()
	writeTempSource(t, dir, "dep/b.wrela", `module root.main`)
	root := filepath.Join(dir, "main.wrela")
	if err := os.WriteFile(root, []byte(`module root.main
use { B } from dep.b`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := source.LoadGraph(source.Options{
		RootPath:    root,
		ImportRoots: []string{dir},
	})
	ce, ok := err.(compiler.CodeError)
	if !ok {
		t.Fatalf("err = %T, want compiler.CodeError", err)
	}
	if ce.Code != "SRC0005" {
		t.Fatalf("code = %s, want SRC0005", ce.Code)
	}
}

func TestLoadGraphDetectsImportCycle(t *testing.T) {
	dir := t.TempDir()
	writeTempSource(t, dir, "cycle/a.wrela", `module cycle.a
use { B } from cycle.b`)
	writeTempSource(t, dir, "cycle/b.wrela", `module cycle.b
use { A } from cycle.a`)
	_, err := source.LoadGraph(source.Options{
		RootPath:    filepath.Join(dir, "cycle/a.wrela"),
		ImportRoots: []string{dir},
	})
	ce, ok := err.(compiler.CodeError)
	if !ok {
		t.Fatalf("err = %T, want compiler.CodeError", err)
	}
	if ce.Code != "SRC0004" {
		t.Fatalf("code = %s, want SRC0004", ce.Code)
	}
}

func TestLoadGraphIgnoresUnimportedSiblingFiles(t *testing.T) {
	dir := t.TempDir()
	writeTempSource(t, dir, "a.wrela", `module root.main
use { B } from dep.b`)
	writeTempSource(t, dir, "dep/b.wrela", `module dep.b`)
	writeTempSource(t, dir, "unimported.wrela", `module ignored.sibling`)
	graph, err := source.LoadGraph(source.Options{
		RootPath:    filepath.Join(dir, "a.wrela"),
		ImportRoots: []string{dir},
	})
	if err != nil {
		t.Fatalf("LoadGraph: %v", err)
	}
	if len(graph.Files) != 2 {
		t.Fatalf("graph files = %d, want 2", len(graph.Files))
	}
	if graph.Files[0].Path != filepath.Join(dir, "a.wrela") || graph.Files[1].Path != filepath.Join(dir, "dep/b.wrela") {
		t.Fatalf("unexpected graph load order: %#v", graph.Files)
	}
}
