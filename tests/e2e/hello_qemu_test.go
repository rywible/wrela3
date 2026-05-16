package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ryanwible/wrela3/compiler"
	"github.com/ryanwible/wrela3/compiler/qemu"
)

var ivshmemServer = "ivshmem-server"

func qemuTimeout() time.Duration {
	seconds := os.Getenv("WRELA_QEMU_TIMEOUT_SECONDS")
	if seconds == "" {
		return 20 * time.Second
	}
	n, err := strconv.Atoi(seconds)
	if err != nil || n <= 0 {
		return 20 * time.Second
	}
	return time.Duration(n) * time.Second
}

func TestHelloQEMU(t *testing.T) {
	qemuBin, err := exec.LookPath("qemu-system-x86_64")
	if err != nil {
		t.Fatalf("qemu-system-x86_64 not found in PATH: %v", err)
	}
	firmware, err := qemu.ResolveFirmware(qemuBin)
	if err != nil {
		t.Fatalf("resolve QEMU firmware: %v", err)
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
		QEMUBinary:    qemuBin,
		OVMFCode:      firmware.Code,
		OVMFVars:      vars,
		ESPDir:        filepath.Join(tmp, "esp"),
		ImagePath:     image,
		UseSerialPipe: true,
		InputText:     "!",
		KeepInputOpen: true,
		SuccessText:   "serial interrupt: !",
		Timeout:       qemuTimeout(),
		EnableEdu:     true,
	})
	if err != nil {
		t.Fatalf("qemu failed: %v\nserial output:\n%s", err, out)
	}
	for _, want := range []string{"hello from wrela", "serial interrupt: !", "msi interrupt"} {
		if !strings.Contains(out, want) {
			t.Fatalf("serial output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "serial interrupt: \x00") {
		t.Fatalf("serial output contains spurious NUL receive event:\n%s", out)
	}
}

func TestArenaMemoryQEMU(t *testing.T) {
	qemuBin, err := exec.LookPath("qemu-system-x86_64")
	if err != nil {
		t.Fatalf("qemu-system-x86_64 not found in PATH: %v", err)
	}
	firmware, err := qemu.ResolveFirmware(qemuBin)
	if err != nil {
		t.Fatalf("resolve QEMU firmware: %v", err)
	}

	tmp := t.TempDir()
	vars := filepath.Join(tmp, "OVMF_VARS.fd")
	copyFile(t, firmware.Vars, vars)
	image := filepath.Join(tmp, "arena-memory.efi")
	_, err = compiler.Build(compiler.BuildOptions{
		Mode:       compiler.ModeDev,
		RootPath:   "tests/e2e/fixtures/arena_memory/main.wrela",
		OutputPath: image,
		RepoRoot:   ".",
	})
	if err != nil {
		t.Fatalf("build arena memory image: %v", err)
	}

	out, err := qemu.Run(qemu.Options{
		QEMUBinary:  qemuBin,
		OVMFCode:    firmware.Code,
		OVMFVars:    vars,
		ESPDir:      filepath.Join(tmp, "esp"),
		ImagePath:   image,
		SuccessText: "arena memory ok",
		Timeout:     qemuTimeout(),
	})
	if err != nil {
		t.Fatalf("qemu failed: %v\nserial output:\n%s", err, out)
	}
	if !strings.Contains(out, "arena memory ok") {
		t.Fatalf("serial output missing arena memory line:\n%s", out)
	}
}

func TestCacheMemoryQEMU(t *testing.T) {
	qemuBin, err := exec.LookPath("qemu-system-x86_64")
	if err != nil {
		t.Fatalf("qemu-system-x86_64 not found in PATH: %v", err)
	}
	firmware, err := qemu.ResolveFirmware(qemuBin)
	if err != nil {
		t.Fatalf("resolve QEMU firmware: %v", err)
	}

	tmp := t.TempDir()
	vars := filepath.Join(tmp, "OVMF_VARS.fd")
	copyFile(t, firmware.Vars, vars)
	image := filepath.Join(tmp, "cache-memory.efi")
	_, err = compiler.Build(compiler.BuildOptions{
		Mode:       compiler.ModeDev,
		RootPath:   "tests/e2e/fixtures/cache_memory/main.wrela",
		OutputPath: image,
		RepoRoot:   ".",
	})
	if err != nil {
		t.Fatalf("build cache memory image: %v", err)
	}

	out, err := qemu.Run(qemu.Options{
		QEMUBinary:  qemuBin,
		OVMFCode:    firmware.Code,
		OVMFVars:    vars,
		ESPDir:      filepath.Join(tmp, "esp"),
		ImagePath:   image,
		SuccessText: "cache memory ok",
		Timeout:     qemuTimeout(),
	})
	if err != nil {
		t.Fatalf("qemu failed: %v\nserial output:\n%s", err, out)
	}
	if !strings.Contains(out, "cache memory ok") {
		t.Fatalf("serial output missing cache memory line:\n%s", out)
	}
	for _, unexpected := range []string{
		"empty cache hit",
		"first put evicted",
		"second put missed eviction",
		"empty value put failed",
		"empty value missed",
		"zero slot stored",
		"zero slot hit",
		"short cache stored",
		"short cache hit",
		"stale cache",
	} {
		if strings.Contains(out, unexpected) {
			t.Fatalf("cache fixture reported %q:\n%s", unexpected, out)
		}
	}
}

func TestHelloInterruptsQEMU(t *testing.T) {
	tmp := t.TempDir()
	image := filepath.Join(tmp, "hello-interrupt.efi")
	_, err := compiler.Build(compiler.BuildOptions{
		Mode:       compiler.ModeDev,
		RootPath:   "tests/e2e/fixtures/hello_ivshmem/main.wrela",
		OutputPath: image,
		RepoRoot:   ".",
	})
	if err != nil {
		t.Fatalf("build hello ivshmem image: %v", err)
	}

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

	vars := filepath.Join(tmp, "OVMF_VARS.fd")
	copyFile(t, firmware.Vars, vars)

	out, err := qemu.Run(qemu.Options{
		QEMUBinary:          qemuBin,
		OVMFCode:            firmware.Code,
		OVMFVars:            vars,
		ESPDir:              filepath.Join(tmp, "esp"),
		ImagePath:           image,
		UseSerialPipe:       true,
		InputText:           "!",
		KeepInputOpen:       true,
		SuccessText:         "msix interrupt",
		Timeout:             qemuTimeout(),
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

func TestMultiVcpuTopicsQEMU(t *testing.T) {
	qemuBin, err := exec.LookPath("qemu-system-x86_64")
	if err != nil {
		t.Fatalf("qemu-system-x86_64 not found in PATH: %v", err)
	}
	firmware, err := qemu.ResolveFirmware(qemuBin)
	if err != nil {
		t.Fatalf("resolve QEMU firmware: %v", err)
	}
	tmp := t.TempDir()
	vars := filepath.Join(tmp, "OVMF_VARS.fd")
	copyFile(t, firmware.Vars, vars)
	image := filepath.Join(tmp, "multi-vcpu-topics.efi")
	_, err = compiler.Build(compiler.BuildOptions{
		Mode:       compiler.ModeDev,
		RootPath:   "tests/e2e/fixtures/multi_vcpu_topics/main.wrela",
		OutputPath: image,
		RepoRoot:   ".",
	})
	if err != nil {
		t.Fatalf("build multi vcpu image: %v", err)
	}
	out, err := qemu.Run(qemu.Options{
		QEMUBinary:  qemuBin,
		OVMFCode:    firmware.Code,
		OVMFVars:    vars,
		ESPDir:      filepath.Join(tmp, "esp"),
		ImagePath:   image,
		SuccessText: "consumer received 64",
		Timeout:     qemuTimeout(),
		SMP:         2,
	})
	if err != nil {
		t.Fatalf("qemu failed: %v\nserial output:\n%s", err, out)
	}
	if !strings.Contains(out, "producer published 64") || !strings.Contains(out, "consumer received 64") {
		t.Fatalf("missing multi-vcpu topic output:\n%s", out)
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
