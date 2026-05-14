package qemu

import (
	"reflect"
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
		"-cpu", "x86-64-v3",
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
