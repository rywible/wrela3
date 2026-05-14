package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryanwible/wrela3/compiler"
	"github.com/ryanwible/wrela3/compiler/qemu"
)

func TestHelloQEMU(t *testing.T) {
	qemuBin, err := exec.LookPath("qemu-system-x86_64")
	if err != nil {
		t.Skip("qemu-system-x86_64 not found in PATH")
	}
	code := os.Getenv("WRELA_OVMF_CODE")
	vars := os.Getenv("WRELA_OVMF_VARS")
	if code == "" || vars == "" {
		t.Skip("WRELA_OVMF_CODE and WRELA_OVMF_VARS must be set")
	}

	tmp := t.TempDir()
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
		QEMUBinary: qemuBin,
		OVMFCode:   code,
		OVMFVars:   vars,
		ESPDir:     filepath.Join(tmp, "esp"),
		ImagePath:  image,
	})
	if err != nil {
		t.Fatalf("qemu failed: %v\nserial output:\n%s", err, out)
	}
	if !strings.Contains(out, "hello from wrela") {
		t.Fatalf("serial output missing hello line:\n%s", out)
	}
}
