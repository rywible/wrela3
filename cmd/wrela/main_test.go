package main

import "testing"

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
	code := run([]string{"build", "--mode", "dev", "missing.wrela", "-o", "out.efi"})
	if code == 2 {
		t.Fatalf("documented flag order returned usage error")
	}
}
