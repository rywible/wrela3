package e2e

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryanwible/wrela3/compiler"
	"github.com/ryanwible/wrela3/compiler/qemu"
)

func TestProductionSubstrateBuildsReport(t *testing.T) {
	dir := t.TempDir()
	efi := filepath.Join(dir, "production-substrate.efi")
	rep := filepath.Join(dir, "production-substrate.report.json")
	_, err := compiler.Build(compiler.BuildOptions{
		Mode:       compiler.ModeDev,
		RootPath:   "tests/e2e/fixtures/production_substrate/main.wrela",
		OutputPath: efi,
		ReportPath: rep,
		RepoRoot:   ".",
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	data, err := os.ReadFile(rep)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	for _, want := range []string{`"Topic<TimerTickPayload>"`, `"Option<TimerTickPayload>"`, `"selected_apic_mode"`, `"interrupt_queues"`, `"wake_targets"`, `"irq.serial.rx"`, `"serial.rx"`} {
		if !bytes.Contains(data, []byte(want)) {
			t.Fatalf("report missing %s:\n%s", want, data)
		}
	}
}

func TestProductionSubstrateQEMU(t *testing.T) {
	if testing.Short() {
		t.Skip("QEMU e2e skipped in short mode")
	}
	deps := requireQEMUDeps(t, true)
	dir := t.TempDir()
	vars := filepath.Join(dir, "OVMF_VARS.fd")
	copyFile(t, deps.firmware.Vars, vars)
	efi := filepath.Join(dir, "production-substrate.efi")
	rep := filepath.Join(dir, "production-substrate.report.json")
	_, err := compiler.Build(compiler.BuildOptions{
		Mode:       compiler.ModeDev,
		RootPath:   "tests/e2e/fixtures/production_substrate/main.wrela",
		OutputPath: efi,
		ReportPath: rep,
		RepoRoot:   ".",
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	output, err := qemu.Run(qemu.Options{
		QEMUBinary:          deps.qemuBin,
		OVMFCode:            deps.firmware.Code,
		OVMFVars:            vars,
		ESPDir:              filepath.Join(dir, "esp"),
		ImagePath:           efi,
		UseSerialPipe:       true,
		InputText:           "!",
		KeepInputOpen:       true,
		SuccessText:         "msix interrupt",
		Timeout:             qemuTimeout(),
		SMP:                 2,
		EnableEdu:           true,
		EnableIvshmemMsix:   true,
		IvshmemServerBinary: deps.ivshmemBin,
	})
	if err != nil {
		t.Fatalf("qemu failed: %v\nserial output:\n%s", err, output)
	}
	for _, want := range []string{
		"production substrate",
		"timer tick",
		"shared irq",
		"serial interrupt: !",
		"msi interrupt",
		"msix interrupt",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("serial output missing %q:\n%s", want, output)
		}
	}
	data, err := os.ReadFile(rep)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	for _, want := range []string{`"Topic<TimerTickPayload>"`, `"Option<TimerTickPayload>"`, `"selected_apic_mode"`, `"interrupt_queues"`, `"wake_targets"`, `"irq.serial.rx"`, `"serial.rx"`} {
		if !bytes.Contains(data, []byte(want)) {
			t.Fatalf("report missing %s:\n%s", want, data)
		}
	}
}
