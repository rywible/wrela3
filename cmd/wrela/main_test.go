package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunUsageAndInvalidModeAreUsageErrors(t *testing.T) {
	if code := run(nil); code != 2 {
		t.Fatalf("run(nil) = %d, want 2", code)
	}
	code := run([]string{"build", "--mode", "fast", "main.wrela", "-o", "out.efi"})
	if code != 2 {
		t.Fatalf("invalid mode exit = %d, want 2", code)
	}
}

func TestRunAcceptsOutputFlagAfterRoot(t *testing.T) {
	var code int
	stderr := captureStderr(t, func() {
		code = run([]string{"build", "--mode", "dev", "missing.wrela", "-o", "out.efi"})
	})
	if code != 1 {
		t.Fatalf("exit = %d, want non-usage build error exit 1; stderr:\n%s", code, stderr)
	}
	if strings.Contains(stderr, "usage:") {
		t.Fatalf("documented flag order returned usage error:\n%s", stderr)
	}
	if !strings.Contains(stderr, "missing.wrela") {
		t.Fatalf("stderr missing source path context:\n%s", stderr)
	}

	out := t.TempDir() + "/out.efi"
	code = run([]string{"build", "--mode", "dev", "examples/hello/main.wrela", "-o", out})
	if code != 0 {
		t.Fatalf("success path exit = %d, want 0", code)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("expected output file %q to exist: %v", out, err)
	}
}

func TestRunAcceptsReportFlag(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "hello.efi")
	rep := filepath.Join(dir, "hello.report.json")
	code := run([]string{"build", "--mode", "dev", "examples/hello/main.wrela", "-o", out, "--report", rep})
	if code != 0 {
		t.Fatalf("run exit = %d, want 0", code)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("expected EFI output: %v", err)
	}
	if _, err := os.Stat(rep); err != nil {
		t.Fatalf("expected report output: %v", err)
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stderr: %v", err)
	}
	os.Stderr = w
	defer func() {
		os.Stderr = old
	}()

	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close stderr writer: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close stderr reader: %v", err)
	}
	return string(out)
}
