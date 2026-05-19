package e2e

import (
	"os"
	"path/filepath"
	"testing"

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

	out, err := qemu.Run(qemu.Options{
		QEMUBinary:  deps.qemuBin,
		OVMFCode:    deps.firmware.Code,
		OVMFVars:    vars,
		ESPDir:      filepath.Join(tmp, "esp"),
		ImagePath:   image,
		SMP:         2,
		SuccessText: "NVME_STORAGE_DONE",
		Timeout:     qemuTimeout(),
		ExtraArgs: []string{
			"-drive", "file=" + disk + ",if=none,id=nvme0,format=raw",
			"-device", "nvme,drive=nvme0,serial=wrela-storage-0",
			"-fw_cfg", "name=wrela.storage.mode,string=" + mode,
		},
	})
	if err != nil {
		t.Fatalf("qemu failed: %v\nserial output:\n%s", err, out)
	}
	return out
}
