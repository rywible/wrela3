package qemu

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveFirmwarePrefersEnvironment(t *testing.T) {
	tmp := t.TempDir()
	code := filepath.Join(tmp, "OVMF_CODE.fd")
	vars := filepath.Join(tmp, "OVMF_VARS.fd")
	if err := os.WriteFile(code, []byte("code"), 0o644); err != nil {
		t.Fatalf("write code: %v", err)
	}
	if err := os.WriteFile(vars, []byte("vars"), 0o644); err != nil {
		t.Fatalf("write vars: %v", err)
	}
	t.Setenv("WRELA_OVMF_CODE", code)
	t.Setenv("WRELA_OVMF_VARS", vars)

	firmware, err := ResolveFirmware("does-not-need-to-exist")
	if err != nil {
		t.Fatalf("ResolveFirmware() error = %v", err)
	}
	if firmware.Code != code || firmware.Vars != vars {
		t.Fatalf("firmware = %#v, want env paths", firmware)
	}
}

func TestResolveFirmwareFindsQEMUFirmwareMetadata(t *testing.T) {
	tmp := t.TempDir()
	qemuBin := filepath.Join(tmp, "Cellar", "qemu", "11.0.0", "bin", "qemu-system-x86_64")
	shareDir := filepath.Join(tmp, "Cellar", "qemu", "11.0.0", "share", "qemu")
	firmwareDir := filepath.Join(shareDir, "firmware")
	code := filepath.Join(shareDir, "edk2-x86_64-code.fd")
	vars := filepath.Join(shareDir, "edk2-i386-vars.fd")
	if err := os.MkdirAll(filepath.Dir(qemuBin), 0o755); err != nil {
		t.Fatalf("mkdir qemu bin: %v", err)
	}
	if err := os.MkdirAll(firmwareDir, 0o755); err != nil {
		t.Fatalf("mkdir firmware: %v", err)
	}
	if err := os.WriteFile(qemuBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write qemu bin: %v", err)
	}
	if err := os.WriteFile(code, []byte("code"), 0o644); err != nil {
		t.Fatalf("write code: %v", err)
	}
	if err := os.WriteFile(vars, []byte("vars"), 0o644); err != nil {
		t.Fatalf("write vars: %v", err)
	}
	metadata := `{
		"description": "UEFI firmware for x86_64",
		"mapping": {
			"device": "flash",
			"executable": {"filename": "` + code + `", "format": "raw"},
			"nvram-template": {"filename": "` + vars + `", "format": "raw"}
		},
		"targets": [{"architecture": "x86_64", "machines": ["pc-q35-*"]}]
	}`
	if err := os.WriteFile(filepath.Join(firmwareDir, "60-edk2-x86_64.json"), []byte(metadata), 0o644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	firmware, err := ResolveFirmware(qemuBin)
	if err != nil {
		t.Fatalf("ResolveFirmware() error = %v", err)
	}
	if firmware.Code != code || firmware.Vars != vars {
		t.Fatalf("firmware = %#v, want metadata paths", firmware)
	}
}
