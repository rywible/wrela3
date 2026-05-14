package qemu

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
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
}

func Args(opts Options) []string {
	memory := opts.Memory
	if memory == "" {
		memory = "256M"
	}
	cpu := opts.CPU
	if cpu == "" {
		cpu = "x86-64-v3"
	}
	return []string{
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
}

func Run(opts Options) (string, error) {
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

	cmd := exec.CommandContext(ctx, bin, Args(opts)...)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	out := output.String()
	if opts.SuccessText != "" && strings.Contains(out, opts.SuccessText) {
		return out, nil
	}
	if err != nil && opts.CPU == "" && strings.Contains(out, "unable to find CPU model 'x86-64-v3'") {
		opts.CPU = "Haswell-v3"
		return runStaged(ctx, bin, opts)
	}
	return out, err
}

func runStaged(ctx context.Context, bin string, opts Options) (string, error) {
	cmd := exec.CommandContext(ctx, bin, Args(opts)...)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
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
