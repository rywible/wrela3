package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ryanwible/wrela3/compiler"
	"github.com/ryanwible/wrela3/compiler/qemu"
)

var ivshmemServer = "ivshmem-server"

func TestHelloQEMU(t *testing.T) {
	qemuBin, err := exec.LookPath("qemu-system-x86_64")
	if err != nil {
		t.Fatalf("qemu-system-x86_64 not found in PATH: %v", err)
	}
	firmware, err := qemu.ResolveFirmware(qemuBin)
	if err != nil {
		t.Fatalf("resolve QEMU firmware: %v", err)
	}
	ivshmemBin, err := exec.LookPath(ivshmemServer)
	if err != nil {
		t.Skipf("%s not found in PATH for extended hello image: %v", ivshmemServer, err)
	}

	tmp := t.TempDir()
	vars := filepath.Join(tmp, "OVMF_VARS.fd")
	copyFile(t, firmware.Vars, vars)
	image := filepath.Join(tmp, "hello.efi")
	_, err = compiler.Build(compiler.BuildOptions{
		Mode:       compiler.ModeDev,
		RootPath:   "examples/hello/main.wrela",
		OutputPath: image,
		RepoRoot:   ".",
	})
	if err != nil {
		t.Fatalf("build hello image: %v", err)
	}

	out, err := qemu.Run(qemu.Options{
		QEMUBinary:          qemuBin,
		OVMFCode:            firmware.Code,
		OVMFVars:            vars,
		ESPDir:              filepath.Join(tmp, "esp"),
		ImagePath:           image,
		SuccessText:         "hello from wrela",
		Timeout:             20 * time.Second,
		EnableEdu:           true,
		EnableIvshmemMsix:   true,
		IvshmemServerBinary: ivshmemBin,
	})
	if err != nil {
		t.Fatalf("qemu failed: %v\nserial output:\n%s", err, out)
	}
	if !strings.Contains(out, "hello from wrela") {
		t.Fatalf("serial output missing hello line:\n%s", out)
	}
}

func TestHelloInterruptsQEMU(t *testing.T) {
	qemuBin, err := exec.LookPath("qemu-system-x86_64")
	if err != nil {
		t.Skipf("qemu-system-x86_64 not found in PATH: %v", err)
	}
	ivshmemBin, err := exec.LookPath(ivshmemServer)
	if err != nil {
		t.Skipf("%s not found in PATH: %v", ivshmemServer, err)
	}
	firmware, err := qemu.ResolveFirmware(qemuBin)
	if err != nil {
		t.Skipf("resolve QEMU firmware: %v", err)
	}

	tmp := t.TempDir()
	vars := filepath.Join(tmp, "OVMF_VARS.fd")
	copyFile(t, firmware.Vars, vars)
	image := filepath.Join(tmp, "hello-interrupt.efi")
	_, err = compiler.Build(compiler.BuildOptions{
		Mode:       compiler.ModeDev,
		RootPath:   "examples/hello/main.wrela",
		OutputPath: image,
		RepoRoot:   ".",
	})
	if err != nil {
		t.Fatalf("build hello image: %v", err)
	}

	out, err := qemu.Run(qemu.Options{
		QEMUBinary:          qemuBin,
		OVMFCode:            firmware.Code,
		OVMFVars:            vars,
		ESPDir:              filepath.Join(tmp, "esp"),
		ImagePath:           image,
		InputText:           "!",
		SuccessText:         "msix interrupt",
		Timeout:             20 * time.Second,
		EnableEdu:           true,
		EnableIvshmemMsix:   true,
		IvshmemServerBinary: ivshmemBin,
	})
	if err != nil {
		t.Fatalf("qemu failed: %v\nserial output:\n%s", err, out)
	}
	for _, want := range []string{"hello from wrela", "serial interrupt: !", "msi interrupt", "msix interrupt"} {
		if !strings.Contains(out, want) {
			t.Fatalf("serial output missing %q:\n%s", want, out)
		}
	}
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", dst, err)
	}
}
