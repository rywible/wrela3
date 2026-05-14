package compiler

import "testing"

func TestParseModeDevAndRelease(t *testing.T) {
	dev, err := ParseMode("dev")
	if err != nil {
		t.Fatalf("dev returned error: %v", err)
	}
	if dev != ModeDev {
		t.Fatalf("dev = %q, want %q", dev, ModeDev)
	}

	release, err := ParseMode("release")
	if err != nil {
		t.Fatalf("release returned error: %v", err)
	}
	if release != ModeRelease {
		t.Fatalf("release = %q, want %q", release, ModeRelease)
	}
}

func TestParseModeInvalidReturnsCLI0001(t *testing.T) {
	_, err := ParseMode("fast")
	if err == nil {
		t.Fatal("ParseMode(fast) succeeded, want error")
	}
	ce, ok := err.(CodeError)
	if !ok {
		t.Fatalf("error type = %T, want compiler.CodeError", err)
	}
	if ce.Code != "CLI0001" {
		t.Fatalf("code = %s, want CLI0001", ce.Code)
	}
	if err.Error() != "CLI0001: invalid mode \"fast\"; expected dev or release" {
		t.Fatalf("message = %q", err.Error())
	}
}
