package compiler

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
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
