package e2e

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ryanwible/wrela3/compiler"
	"github.com/ryanwible/wrela3/compiler/qemu"
)

const nvmeStorageDiskBytes int64 = 4 * 1024 * 1024 * 1024

func createSparseRawDisk(t *testing.T, path string, bytes int64) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("create sparse raw disk: %v", err)
	}
	defer f.Close()
	if err := f.Truncate(bytes); err != nil {
		t.Fatalf("truncate sparse raw disk: %v", err)
	}
}

func runStorageQEMU(t *testing.T, disk, mode string) string {
	t.Helper()
	out, err := runStorageQEMUResultWithBlockSize(t, disk, mode, 0)
	if err != nil {
		t.Fatalf("qemu failed: %v\nserial output:\n%s", err, out)
	}
	return out
}

func runStorageQEMUWithBlockSize(t *testing.T, disk, mode string, blockSize int64) string {
	t.Helper()
	out, err := runStorageQEMUResultWithBlockSize(t, disk, mode, blockSize)
	if err != nil {
		t.Fatalf("qemu failed: %v\nserial output:\n%s", err, out)
	}
	return out
}

func runStorageQEMUResult(t *testing.T, disk, mode string) (string, error) {
	t.Helper()
	return runStorageQEMUResultWithBlockSize(t, disk, mode, 0)
}

func runStorageQEMUResultWithBlockSize(t *testing.T, disk, mode string, blockSize int64) (string, error) {
	t.Helper()
	deps := requireQEMUDeps(t, false)

	tmp := t.TempDir()
	vars := filepath.Join(tmp, "OVMF_VARS.fd")
	copyFile(t, deps.firmware.Vars, vars)
	image := filepath.Join(tmp, "nvme-storage.efi")
	_, err := compiler.Build(compiler.BuildOptions{
		Mode:       compiler.ModeDev,
		RootPath:   "tests/e2e/fixtures/nvme_event_storage/main.wrela",
		OutputPath: image,
		RepoRoot:   ".",
	})
	if err != nil {
		t.Fatalf("build nvme storage image: %v", err)
	}

	nvmeDevice := "nvme,drive=nvme0,serial=wrela-storage-0,bootindex=-1"
	if blockSize != 0 {
		nvmeDevice += ",logical_block_size=4096,physical_block_size=4096"
	}
	out, err := qemu.Run(qemu.Options{
		QEMUBinary:  deps.qemuBin,
		OVMFCode:    deps.firmware.Code,
		OVMFVars:    vars,
		ESPDir:      filepath.Join(tmp, "esp"),
		ImagePath:   image,
		CPU:         "Haswell-v3,phys-bits=48",
		SMP:         2,
		SuccessText: "NVME_STORAGE_DONE",
		Timeout:     nvmeStorageQEMUTimeout(mode),
		ExtraArgs: []string{
			"-global", "q35-pcihost.pci-hole64-size=0",
			"-drive", "file=" + disk + ",if=none,id=nvme0,format=raw",
			"-device", nvmeDevice,
			"-fw_cfg", "name=opt/wrela.storage.mode,string=wrela:" + mode,
		},
	})
	return out, err
}

func nvmeStorageQEMUTimeout(mode string) time.Duration {
	timeout := qemuTimeout()
	if mode != "first" && mode != "replay" {
		return timeout
	}
	if timeout < 60*time.Second {
		return 60 * time.Second
	}
	return timeout
}
