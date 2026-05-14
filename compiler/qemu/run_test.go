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
		"-cpu", "Haswell-v3",
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

func TestRunUsesPortableDefaultCPUModelWithoutFallback(t *testing.T) {
	tmp := t.TempDir()
	invocations := filepath.Join(tmp, "invocations.txt")
	fakeQEMU := filepath.Join(tmp, "fake-qemu.sh")
	script := "#!/usr/bin/env sh\n" +
		"echo \"$*\" >> " + invocations + "\n" +
		"echo \"boot failed\" >&2\n" +
		"exit 1\n"
	if err := os.WriteFile(fakeQEMU, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake qemu: %v", err)
	}
	image := filepath.Join(tmp, "hello.efi")
	if err := os.WriteFile(image, []byte("efi"), 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}

	out, err := Run(Options{
		QEMUBinary: fakeQEMU,
		OVMFCode:   filepath.Join(tmp, "code.fd"),
		OVMFVars:   filepath.Join(tmp, "vars.fd"),
		ESPDir:     filepath.Join(tmp, "esp"),
		ImagePath:  image,
	})
	if err == nil {
		t.Fatalf("Run() error = nil, output:\n%s", out)
	}
	if !strings.Contains(out, "boot failed") {
		t.Fatalf("Run() output missing fake QEMU error: %q", out)
	}
	data, err := os.ReadFile(invocations)
	if err != nil {
		t.Fatalf("read invocations: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("qemu invocations = %d, want 1; log:\n%s", len(lines), data)
	}
	if !strings.Contains(lines[0], "-cpu Haswell-v3") {
		t.Fatalf("qemu args = %q, want default Haswell-v3", lines[0])
	}
	if strings.Contains(lines[0], "x86-64-v3") {
		t.Fatalf("qemu args used target name as CPU model: %q", lines[0])
	}
}
