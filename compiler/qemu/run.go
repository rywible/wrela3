package qemu

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Options struct {
	QEMUBinary  string
	OVMFCode    string
	OVMFVars    string
	ESPDir      string
	ImagePath   string
	Memory      string
	CPU         string
	Timeout     time.Duration
	SuccessText string

	IvshmemServerBinary   string
	InputText             string
	EnableEdu             bool
	EnableIvshmemMsix     bool
	IvshmemSocketPath     string
	IvshmemStartupTimeout time.Duration
	InputDelay            time.Duration
}

type IvshmemServerOptions struct {
	SocketPath string
	PidPath    string
	ShmName    string
	Size       string
	Vectors    int
}

func IvshmemServerArgs(opts IvshmemServerOptions) []string {
	return []string{
		"-S", opts.SocketPath,
		"-p", opts.PidPath,
		// QEMU's ivshmem-server uses -M for the POSIX shm object name;
		// -m is the shm directory in current upstream/server builds.
		"-M", opts.ShmName,
		"-l", opts.Size,
		"-n", strconv.Itoa(opts.Vectors),
	}
}

func Args(opts Options) []string {
	memory := opts.Memory
	if memory == "" {
		memory = "256M"
	}
	cpu := opts.CPU
	if cpu == "" {
		cpu = "Haswell-v3"
	}
	args := []string{
		"-machine", "q35",
		"-cpu", cpu,
		"-m", memory,
		"-drive", "if=pflash,format=raw,readonly=on,file=" + opts.OVMFCode,
		"-drive", "if=pflash,format=raw,file=" + opts.OVMFVars,
		"-drive", "format=raw,file=fat:rw:" + opts.ESPDir,
		"-serial", "stdio",
		"-display", "none",
		"-no-reboot",
	}

	if opts.EnableEdu {
		args = append(args, "-device", "edu,addr=0x5")
	}
	if opts.EnableIvshmemMsix {
		args = append(args,
			"-chardev", "socket,path="+opts.IvshmemSocketPath+",id=ivshmem0",
			"-chardev", "socket,path="+opts.IvshmemSocketPath+",id=ivshmem1",
			"-device", "ivshmem-doorbell,vectors=1,chardev=ivshmem0,addr=0x6",
			"-device", "ivshmem-doorbell,vectors=1,chardev=ivshmem1,addr=0x7",
		)
	}

	return args
}

func waitForUnixSocket(path string, timeout time.Duration) error {
	if timeout == 0 {
		timeout = 2 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("unix", path, 50*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for ivshmem socket %s", path)
}

func stopIvshmemServer(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
}

func Run(opts Options) (string, error) {
	ivshmemPath := opts.IvshmemSocketPath
	var ivshmemTmpDir string
	if opts.EnableIvshmemMsix && ivshmemPath == "" {
		tmpDir, err := os.MkdirTemp("", "ivshmem-")
		if err != nil {
			return "", err
		}
		ivshmemTmpDir = tmpDir
		defer os.RemoveAll(ivshmemTmpDir)
		ivshmemPath = filepath.Join(tmpDir, "ivshmem.sock")
	}
	if ivshmemPath != "" {
		opts.IvshmemSocketPath = ivshmemPath
	}

	if err := StageESP(opts.ImagePath, opts.ESPDir); err != nil {
		return "", err
	}
	bin := opts.QEMUBinary
	if bin == "" {
		bin = "qemu-system-x86_64"
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var ivshmemServer *exec.Cmd
	if opts.EnableIvshmemMsix {
		ivshmemServerBinary := opts.IvshmemServerBinary
		if ivshmemServerBinary == "" {
			ivshmemServerBinary = "ivshmem-server"
		}
		if err := os.Remove(ivshmemPath); err != nil && !os.IsNotExist(err) {
			return "", err
		}
		ivshmemServerCmd := exec.CommandContext(ctx, ivshmemServerBinary, IvshmemServerArgs(IvshmemServerOptions{
			SocketPath: ivshmemPath,
			PidPath:    filepath.Join(filepath.Dir(ivshmemPath), "ivshmem.pid"),
			ShmName:    "wrela-ivshmem",
			Size:       "1M",
			Vectors:    1,
		})...)

		if err := ivshmemServerCmd.Start(); err != nil {
			return "", err
		}
		ivshmemServer = ivshmemServerCmd

		if err := waitForUnixSocket(ivshmemPath, opts.IvshmemStartupTimeout); err != nil {
			stopIvshmemServer(ivshmemServer)
			return "", err
		}
	}

	cmd := exec.CommandContext(ctx, bin, Args(opts)...)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	var stdin io.WriteCloser
	if opts.InputText != "" {
		pipe, err := cmd.StdinPipe()
		if err != nil {
			return "", err
		}
		stdin = pipe
	}
	err := cmd.Start()
	if err == nil && stdin != nil {
		delay := opts.InputDelay
		if delay == 0 {
			delay = 2 * time.Second
		}
		go func() {
			select {
			case <-ctx.Done():
				_ = stdin.Close()
			case <-time.After(delay):
				_, _ = stdin.Write([]byte(opts.InputText))
				_ = stdin.Close()
			}
		}()
	}
	if err == nil {
		err = cmd.Wait()
	}
	if ivshmemServer != nil {
		stopIvshmemServer(ivshmemServer)
	}
	out := output.String()
	if opts.SuccessText != "" && strings.Contains(out, opts.SuccessText) {
		return out, nil
	}
	return out, err
}

func StageESP(imagePath, espDir string) error {
	target := filepath.Join(espDir, "EFI", "BOOT", "BOOTX64.EFI")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	image, err := os.ReadFile(imagePath)
	if err != nil {
		return err
	}
	return os.WriteFile(target, image, 0o644)
}
