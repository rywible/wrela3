package qemu

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestArgsMatchAppendixG(t *testing.T) {
	got := Args(Options{
		OVMFCode: "/ovmf/OVMF_CODE.fd",
		OVMFVars: "/ovmf/OVMF_VARS.fd",
		ESPDir:   "build/esp",
	})
	want := []string{
		"-machine", "q35",
		"-cpu", "x86-64-v3",
		"-m", "256M",
		"-drive", "if=pflash,format=raw,readonly=on,file=/ovmf/OVMF_CODE.fd",
		"-drive", "if=pflash,format=raw,file=/ovmf/OVMF_VARS.fd",
		"-drive", "format=raw,file=fat:rw:build/esp",
		"-serial", "stdio",
		"-display", "none",
		"-no-reboot",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Args() = %#v, want %#v", got, want)
	}
}

func TestArgsAllowsMemoryOverride(t *testing.T) {
	got := Args(Options{
		OVMFCode: "/code.fd",
		OVMFVars: "/vars.fd",
		ESPDir:   "esp",
		Memory:   "512M",
	})
	if got[5] != "512M" {
		t.Fatalf("memory arg = %q, want 512M", got[5])
	}
}

func TestArgsAllowsCPUOverride(t *testing.T) {
	got := Args(Options{
		OVMFCode: "/code.fd",
		OVMFVars: "/vars.fd",
		ESPDir:   "esp",
		CPU:      "Haswell-v3",
	})
	if got[3] != "Haswell-v3" {
		t.Fatalf("cpu arg = %q, want Haswell-v3", got[3])
	}
}

func TestRunReturnsSuccessWhenExpectedOutputAppearsBeforeProcessExit(t *testing.T) {
	tmp := t.TempDir()
	fakeQEMU := filepath.Join(tmp, "fake-qemu.sh")
	if err := os.WriteFile(fakeQEMU, []byte("#!/usr/bin/env sh\necho 'booting'\necho 'hello from wrela'\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write fake qemu: %v", err)
	}
	image := filepath.Join(tmp, "hello.efi")
	if err := os.WriteFile(image, []byte("efi"), 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}

	out, err := Run(Options{
		QEMUBinary:  fakeQEMU,
		OVMFCode:    filepath.Join(tmp, "code.fd"),
		OVMFVars:    filepath.Join(tmp, "vars.fd"),
		ESPDir:      filepath.Join(tmp, "esp"),
		ImagePath:   image,
		SuccessText: "hello from wrela",
	})
	if err != nil {
		t.Fatalf("Run() error = %v, output:\n%s", err, out)
	}
	if !strings.Contains(out, "hello from wrela") {
		t.Fatalf("Run() output missing success text: %q", out)
	}
}
