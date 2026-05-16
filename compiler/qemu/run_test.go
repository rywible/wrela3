package qemu

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
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

func TestArgsIncludesSMPWhenRequested(t *testing.T) {
	args := Args(Options{
		ImagePath: "x.efi",
		ESPDir:   "esp",
		OVMFCode: "code.fd",
		OVMFVars: "vars.fd",
		SMP:      2,
	})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-smp 2") {
		t.Fatalf("QEMU args missing -smp 2:\n%s", joined)
	}
}

func TestRunWritesInputTextToQEMUStdin(t *testing.T) {
	tmp := t.TempDir()
	seen := filepath.Join(tmp, "stdin.txt")
	fakeQEMU := filepath.Join(tmp, "fake-qemu.sh")
	script := "#!/usr/bin/env sh\ncat > " + seen + "\necho 'serial interrupt: !'\n"
	if err := os.WriteFile(fakeQEMU, []byte(script), 0o755); err != nil {
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
		InputText:   "!",
		SuccessText: "serial interrupt: !",
	})
	if err != nil {
		t.Fatalf("Run error = %v, output:\n%s", err, out)
	}
	data, err := os.ReadFile(seen)
	if err != nil {
		t.Fatalf("read stdin capture: %v", err)
	}
	if string(data) != "!" {
		t.Fatalf("stdin = %q, want !", data)
	}
}

func TestArgsAddsEduAndIvshmemDevices(t *testing.T) {
	got := strings.Join(Args(Options{
		OVMFCode:          "/code.fd",
		OVMFVars:          "/vars.fd",
		ESPDir:            "esp",
		EnableEdu:         true,
		EnableIvshmemMsix: true,
		IvshmemSocketPath: "/tmp/ivshmem.sock",
	}), " ")
	for _, want := range []string{
		"-device edu,addr=0x5",
		"-chardev socket,path=/tmp/ivshmem.sock,id=ivshmem0",
		"-chardev socket,path=/tmp/ivshmem.sock,id=ivshmem1",
		"-device ivshmem-doorbell,vectors=1,chardev=ivshmem0,addr=0x6",
		"-device ivshmem-doorbell,vectors=1,chardev=ivshmem1,addr=0x7",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("QEMU args missing %q:\n%s", want, got)
		}
	}
}

func TestIvshmemServerArgs(t *testing.T) {
	got := IvshmemServerArgs(IvshmemServerOptions{
		SocketPath: "/tmp/ivshmem.sock",
		PidPath:    "/tmp/ivshmem.pid",
		ShmName:    "wrela-ivshmem",
		Size:       "1M",
		Vectors:    1,
	})
	want := []string{"-S", "/tmp/ivshmem.sock", "-p", "/tmp/ivshmem.pid", "-M", "wrela-ivshmem", "-l", "1M", "-n", "1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("IvshmemServerArgs() = %#v, want %#v", got, want)
	}
}

func TestRunDoesNotStartQEMUWhenIvshmemServerSocketIsMissing(t *testing.T) {
	tmp := t.TempDir()
	fakeServer := filepath.Join(tmp, "fake-ivshmem-server.sh")
	pidFile := filepath.Join(tmp, "server.pid")
	serverScript := "#!/usr/bin/env sh\necho $$ > " + pidFile + "\nsleep 30\n"
	if err := os.WriteFile(fakeServer, []byte(serverScript), 0o755); err != nil {
		t.Fatalf("write fake server: %v", err)
	}
	qemuRan := filepath.Join(tmp, "qemu-ran")
	fakeQEMU := filepath.Join(tmp, "fake-qemu.sh")
	qemuScript := "#!/usr/bin/env sh\ntouch " + qemuRan + "\n"
	if err := os.WriteFile(fakeQEMU, []byte(qemuScript), 0o755); err != nil {
		t.Fatalf("write fake qemu: %v", err)
	}
	image := filepath.Join(tmp, "hello.efi")
	if err := os.WriteFile(image, []byte("efi"), 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}

	_, err := Run(Options{
		QEMUBinary:            fakeQEMU,
		OVMFCode:              filepath.Join(tmp, "code.fd"),
		OVMFVars:              filepath.Join(tmp, "vars.fd"),
		ESPDir:                filepath.Join(tmp, "esp"),
		ImagePath:             image,
		EnableIvshmemMsix:     true,
		IvshmemServerBinary:   fakeServer,
		IvshmemSocketPath:     filepath.Join(tmp, "missing.sock"),
		IvshmemStartupTimeout: 20 * time.Millisecond,
	})
	if err == nil {
		t.Fatalf("expected ivshmem startup error")
	}
	if _, statErr := os.Stat(qemuRan); !os.IsNotExist(statErr) {
		t.Fatalf("QEMU must not start before ivshmem socket is ready")
	}
	rawPID, readErr := os.ReadFile(pidFile)
	if readErr == nil {
		pid, _ := strconv.Atoi(strings.TrimSpace(string(rawPID)))
		if processRunning(pid) {
			t.Fatalf("ivshmem server pid %d still running after startup failure", pid)
		}
	}
}

func TestRunRemovesImplicitIvshmemTempDirOnStartupFailure(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	fakeServer := filepath.Join(tmp, "fake-ivshmem-server.sh")
	serverScript := "#!/usr/bin/env sh\nsleep 30\n"
	if err := os.WriteFile(fakeServer, []byte(serverScript), 0o755); err != nil {
		t.Fatalf("write fake server: %v", err)
	}
	fakeQEMU := filepath.Join(tmp, "fake-qemu.sh")
	if err := os.WriteFile(fakeQEMU, []byte("#!/usr/bin/env sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake qemu: %v", err)
	}
	image := filepath.Join(tmp, "hello.efi")
	if err := os.WriteFile(image, []byte("efi"), 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}

	_, err := Run(Options{
		QEMUBinary:            fakeQEMU,
		OVMFCode:              filepath.Join(tmp, "code.fd"),
		OVMFVars:              filepath.Join(tmp, "vars.fd"),
		ESPDir:                filepath.Join(tmp, "esp"),
		ImagePath:             image,
		EnableIvshmemMsix:     true,
		IvshmemServerBinary:   fakeServer,
		IvshmemStartupTimeout: 200 * time.Millisecond,
	})
	if err == nil {
		t.Fatalf("expected ivshmem startup error")
	}
	matches, globErr := filepath.Glob(filepath.Join(tmp, "ivshmem-*"))
	if globErr != nil {
		t.Fatalf("glob implicit temp dirs: %v", globErr)
	}
	if len(matches) != 0 {
		t.Fatalf("implicit ivshmem temp dirs still exist: %#v", matches)
	}
}

func TestRunRemovesStaleIvshmemSocketBeforeStartingServer(t *testing.T) {
	tmp := t.TempDir()
	socketPath := filepath.Join(tmp, "ivshmem.sock")
	if err := os.WriteFile(socketPath, []byte("stale"), 0o644); err != nil {
		t.Fatalf("write stale socket placeholder: %v", err)
	}
	observed := filepath.Join(tmp, "observed")
	fakeServer := filepath.Join(tmp, "fake-ivshmem-server.sh")
	serverScript := "#!/usr/bin/env sh\nif [ -e " + socketPath + " ]; then echo stale > " + observed + "; exit 1; fi\ntouch " + observed + "\nsleep 30\n"
	if err := os.WriteFile(fakeServer, []byte(serverScript), 0o755); err != nil {
		t.Fatalf("write fake server: %v", err)
	}
	fakeQEMU := filepath.Join(tmp, "fake-qemu.sh")
	if err := os.WriteFile(fakeQEMU, []byte("#!/usr/bin/env sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake qemu: %v", err)
	}
	image := filepath.Join(tmp, "hello.efi")
	if err := os.WriteFile(image, []byte("efi"), 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}

	_, err := Run(Options{
		QEMUBinary:            fakeQEMU,
		OVMFCode:              filepath.Join(tmp, "code.fd"),
		OVMFVars:              filepath.Join(tmp, "vars.fd"),
		ESPDir:                filepath.Join(tmp, "esp"),
		ImagePath:             image,
		EnableIvshmemMsix:     true,
		IvshmemServerBinary:   fakeServer,
		IvshmemSocketPath:     socketPath,
		IvshmemStartupTimeout: 200 * time.Millisecond,
	})
	if err == nil {
		t.Fatalf("expected ivshmem startup error")
	}
	data, readErr := os.ReadFile(observed)
	if readErr != nil {
		t.Fatalf("read observed marker: %v", readErr)
	}
	if strings.TrimSpace(string(data)) == "stale" {
		t.Fatalf("server saw stale socket placeholder")
	}
}

func processRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
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
