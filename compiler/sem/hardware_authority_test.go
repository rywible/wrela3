package sem

import (
	"os"
	"path/filepath"
	"strings"

	"testing"

	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/parse"
	"github.com/ryanwible/wrela3/compiler/source"
)

func checkUEFIModulesWithExtraSource(t *testing.T, name string, sourceText string) (*CheckedProgram, []diag.Diagnostic) {
	t.Helper()
	modules := parseUEFIModuleSet(t)
	for i := range modules {
		if modules[i].Name == "sem.uefi_test_harness" {
			modules = append(modules[:i], modules[i+1:]...)
			break
		}
	}
	extra, pds := parse.ParseGraph(source.Graph{
		Files: []*source.File{source.NewFile(source.FileID(9000), name, sourceText)},
	})
	if len(pds) != 0 {
		t.Fatalf("parse extra source: %#v", pds)
	}
	modules = append(modules, extra...)
	index, ds := BuildIndex(modules)
	if len(ds) != 0 {
		return nil, ds
	}
	return Check(index, modules)
}

func TestForgedHardwareAuthorityRejected(t *testing.T) {
	_, ds := checkUEFIModulesWithExtraSource(t, "forged-mmio-test.wrela", `
module examples.bad
use { BootPanic } from platform.hardware.panic
use { PlatformDiscoveryRoot } from platform.hardware.discovery
use { MmioRegion } from platform.hardware.bytes
use { DelegatedHardware } from platform.uefi.transition
use { OwnedHardware, OwnedMemory, IoPortAuthority, MemoryPlan, CpuPlan } from machine.x86_64.cpu_state
use { HardwarePlan, InterruptRoutingPlan, ClaimedPciPlanBuilder } from machine.x86_64.cpu_state
use { MutableBytes, Bytes } from machine.x86_64.executor_memory
use { InterruptVector } from machine.x86_64.interrupts

image BadForgedMmio {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        let panic = BootPanic()
        let fake = MmioRegion(address = 0xFEC00000, length = 4096, panic = panic)
        let arena = MutableBytes(address = 0, length = 0)
        let owned_memory = OwnedMemory(arena = arena)
        let memory_plan = MemoryPlan(
            owned_memory = owned_memory,
            executor_arena = MutableBytes(address = 0, length = 0),
            io_ports = IoPortAuthority()
        )
        let cpu_plan = CpuPlan(
            owned_stack_top = 0,
            gdt_descriptor = Bytes(address = 0, length = 0),
            idt_descriptor = Bytes(address = 0, length = 0),
            cr3 = 0
        )
        let discovery = PlatformDiscoveryRoot(panic = panic).from_uefi(hardware = hardware)
        let interrupts = discovery.interrupts
        let hardware_plan = HardwarePlan(cpus = discovery.acpi.require_madt().enabled_cpus().require_count(count = 2),
            interrupts = InterruptRoutingPlan(
                local_apic = interrupts.local_apic,
                serial_irq4 = interrupts.route_isa_irq(irq = 4, vector = InterruptVector(value = 0x40))
            ),
            pci = ClaimedPciPlanBuilder(panic = panic).empty()
        )
        return hardware.exit_to_owned_hardware(memory_plan = memory_plan, cpu_plan = cpu_plan, hardware_plan = hardware_plan)
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        while true {}
    }
}`)
	if !hasCode(ds, diag.SEM0049) {
		t.Fatalf("expected SEM0049, got %#v", ds)
	}
}

func TestPhysicalRegionAndArenaAuthorityForgeryRejected(t *testing.T) {
	for _, fixture := range []string{
		"forged_physical_region_authority.wrela",
		"forged_root_arena.wrela",
	} {
		t.Run(fixture, func(t *testing.T) {
			modules := parseAuthorityFixtureModulesForTest(t, filepath.Join("tests", "fixtures", "negative", fixture))
			index, ds := BuildIndex(modules)
			ds = filterMissingImageDiagnostic(ds)
			if len(ds) != 0 {
				t.Fatalf("index diagnostics: %#v", ds)
			}
			_, ds = Check(index, modules)
			if !hasCode(ds, diag.SEM0056) {
				t.Fatalf("expected SEM0056, got %#v", ds)
			}
		})
	}
}

func TestHardwareAuthorityRegionKindSourceDefinitions(t *testing.T) {
	modules := parseUEFIModuleSet(t)
	index, ds := BuildIndex(modules)
	if len(ds) != 0 {
		t.Fatalf("build index diagnostics: %#v", ds)
	}

	for _, tc := range []struct {
		module string
		name   string
	}{
		{"platform.hardware.bytes", "Mmio"},
		{"platform.hardware.bytes", "Volatile"},
		{"platform.uefi.types", "FirmwareSlice"},
		{"platform.hardware.memory", "DmaBuffer"},
		{"platform.acpi.tables", "AcpiTableView"},
	} {
		typ := moduleType(t, index, tc.module, tc.name)
		if len(typ.TypeParams) != 1 || typ.TypeParams[0].Name != "T" {
			t.Fatalf("%s.%s type params = %#v, want T", tc.module, tc.name, typ.TypeParams)
		}
	}

	firmware := moduleType(t, index, "platform.uefi.types", "FirmwareSlice")
	if fieldTypeDisplay(t, firmware, "address") != "FirmwareAddress" {
		t.Fatalf("FirmwareSlice.address = %s, want FirmwareAddress", fieldTypeDisplay(t, firmware, "address"))
	}
	if fieldTypeDisplay(t, firmware, "length") != "U64" {
		t.Fatalf("FirmwareSlice.length = %s, want U64", fieldTypeDisplay(t, firmware, "length"))
	}

	dma := moduleType(t, index, "platform.hardware.memory", "DmaBuffer")
	if fieldTypeDisplay(t, dma, "slots") != "Slots<T>" {
		t.Fatalf("DmaBuffer.slots = %s, want Slots<T>", fieldTypeDisplay(t, dma, "slots"))
	}

	table := moduleType(t, index, "platform.acpi.tables", "AcpiTable")
	if fieldTypeDisplay(t, table, "view") != "AcpiTableView<U8>" {
		t.Fatalf("AcpiTable.view = %s, want AcpiTableView<U8>", fieldTypeDisplay(t, table, "view"))
	}
	view := moduleType(t, index, "platform.acpi.tables", "AcpiTableView")
	if fieldTypeDisplay(t, view, "typed") != "FirmwareSlice<T>" {
		t.Fatalf("AcpiTableView.typed = %s, want FirmwareSlice<T>", fieldTypeDisplay(t, view, "typed"))
	}

	mmio := moduleType(t, index, "platform.hardware.bytes", "MmioRegion")
	assertMethodExists(t, mmio, "read32")
	assertMethodExists(t, mmio, "write32")
}

func TestProtectedGenericRegionViewForgeryRejected(t *testing.T) {
	modules := parseAuthorityFixtureModulesForTest(t, filepath.Join("tests", "fixtures", "negative", "forged_mmio_generic.wrela"))
	index, ds := BuildIndex(modules)
	ds = filterMissingImageDiagnostic(ds)
	if len(ds) != 0 {
		t.Fatalf("index diagnostics: %#v", ds)
	}
	_, ds = Check(index, modules)
	if !hasCode(ds, diag.SEM0092) {
		t.Fatalf("expected SEM0092, got %#v", ds)
	}
}

func fieldTypeDisplay(t *testing.T, typ *Type, field string) string {
	t.Helper()
	for _, f := range typ.Fields {
		if f.Name == field {
			return f.Type.Display()
		}
	}
	t.Fatalf("missing field %s on %s", field, typ.Name)
	return ""
}

func parseAuthorityFixtureModulesForTest(t *testing.T, path string) []*ast.Module {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	abs := filepath.Join(wd, "..", "..", path)
	raw, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read fixture %s: %v", abs, err)
	}
	src := string(raw)
	if strings.HasPrefix(src, "// expect:") {
		if _, rest, ok := strings.Cut(src, "\n"); ok {
			src = rest
		}
	}
	parts := strings.Split(src, "\nmodule ")
	files := make([]*source.File, 0, len(parts))
	for i, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		if i != 0 {
			part = "module " + part
		}
		files = append(files, source.NewFile(source.FileID(9100+i), path, part))
	}
	modules, ds := parse.ParseGraph(source.Graph{Files: files})
	if len(ds) != 0 {
		t.Fatalf("parse fixture %s: %#v", path, ds)
	}
	return modules
}
