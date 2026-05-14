package compiler

import "testing"

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
