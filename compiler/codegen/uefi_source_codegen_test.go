package codegen

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/ir"
	"github.com/ryanwible/wrela3/compiler/layout"
	"github.com/ryanwible/wrela3/compiler/parse"
	"github.com/ryanwible/wrela3/compiler/sem"
	"github.com/ryanwible/wrela3/compiler/source"
)

func TestUEFIAmd64BootServiceAsmMethodCodegen(t *testing.T) {
	checked := parseCheckedUEFIModules(t)

	getMap := asmMethodFromSem(t, checked, "platform.uefi.boot_services", "UefiBootServicesCalls", "get_memory_map")
	unit, ds := compileAsmMethodUnit(getMap)
	if len(ds) != 0 {
		t.Fatalf("compileAsmMethodUnit get_memory_map diagnostics: %#v", ds)
	}
	if !bytes.Contains(unit.Bytes, []byte{0x48, 0x83, 0xEC, 0x30}) {
		t.Fatalf("missing 48 83 EC 30 shadow-frame setup in get_memory_map: %x", unit.Bytes)
	}
	if !bytes.Contains(unit.Bytes, []byte{0xFF, 0xD0}) {
		t.Fatalf("missing call rax in get_memory_map: %x", unit.Bytes)
	}

	exitBS := asmMethodFromSem(t, checked, "platform.uefi.boot_services", "UefiBootServicesCalls", "exit_boot_services")
	exitUnit, ds := compileAsmMethodUnit(exitBS)
	if len(ds) != 0 {
		t.Fatalf("compileAsmMethodUnit exit_boot_services diagnostics: %#v", ds)
	}
	if !bytes.Contains(exitUnit.Bytes, []byte{0x48, 0x83, 0xEC, 0x20}) {
		t.Fatalf("missing 48 83 EC 20 shadow-frame setup in exit_boot_services: %x", exitUnit.Bytes)
	}
	if !bytes.Contains(exitUnit.Bytes, []byte{0xFF, 0xD0}) {
		t.Fatalf("missing call rax in exit_boot_services: %x", exitUnit.Bytes)
	}
}

func TestUEFITransitionActivateAsmMethodCodegen(t *testing.T) {
	checked := parseCheckedUEFIModules(t)
	activate := asmMethodFromSem(t, checked, "platform.uefi.transition", "DelegatedHardware", "activate_owned_hardware")
	unit, ds := compileAsmMethodUnit(activate)
	if len(ds) != 0 {
		t.Fatalf("compileAsmMethodUnit activate_owned_hardware diagnostics: %#v", ds)
	}
	if !bytes.Contains(unit.Bytes, []byte{0xFA}) {
		t.Fatalf("missing cli in activate_owned_hardware")
	}
	if !bytes.Contains(unit.Bytes, []byte{0x41, 0x0f, 0x01, 0x13}) {
		t.Fatalf("missing lgdt in activate_owned_hardware")
	}
	if !bytes.Contains(unit.Bytes, []byte{0x41, 0x0f, 0x01, 0x1b}) {
		t.Fatalf("missing lidt in activate_owned_hardware")
	}
	if !bytes.Contains(unit.Bytes, []byte{0x6a, 0x08}) {
		t.Fatalf("missing push cs selector reload in activate_owned_hardware")
	}
	if !bytes.Contains(unit.Bytes, []byte{0x48, 0xcb}) {
		t.Fatalf("missing retfq in activate_owned_hardware")
	}
}

func TestUEFIBuilderAsmMethodCompilation(t *testing.T) {
	checked := parseCheckedUEFIModules(t)
	identity := asmMethodFromSem(t, checked, "platform.uefi.types", "DelegatedMemory", "build_identity_paging")
	if identity.AsmBody == nil {
		t.Fatalf("build_identity_paging is not an asm method")
	}
	gdt := asmMethodFromSem(t, checked, "platform.uefi.types", "DelegatedMemory", "build_owned_gdt")
	if gdt.AsmBody == nil {
		t.Fatalf("build_owned_gdt is not an asm method")
	}
	idt := asmMethodFromSem(t, checked, "platform.uefi.types", "DelegatedMemory", "build_fatal_idt")
	if idt.AsmBody == nil {
		t.Fatalf("build_fatal_idt is not an asm method")
	}

	for _, method := range []ir.AsmMethod{identity, gdt, idt} {
		unit, ds := compileAsmMethodUnit(method)
		if len(ds) != 0 {
			t.Fatalf("compileAsmMethodUnit %q diagnostics: %#v", method.Symbol, ds)
		}
		if len(unit.Bytes) == 0 {
			t.Fatalf("compiled asm unit for %q is empty", method.Symbol)
		}
	}
}

func parseCheckedUEFIModules(t *testing.T) *sem.CheckedProgram {
	t.Helper()
	modules := parseUEFIModulesForCodegen(t)
	index, ds := sem.BuildIndex(modules)
	if len(ds) != 0 {
		t.Fatalf("build index diagnostics: %#v", ds)
	}

	checked, ds := sem.Check(index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}
	if checked.Index == nil {
		t.Fatalf("checked program has no index")
	}
	if len(checked.Index.Images) == 0 {
		t.Fatalf("expected one image in checked program")
	}
	return checked
}

func asmMethodFromSem(t *testing.T, checked *sem.CheckedProgram, moduleName, typeName, methodName string) ir.AsmMethod {
	t.Helper()
	module, ok := checked.Index.Lookup(moduleName, typeName)
	if !ok {
		t.Fatalf("missing %s.%s", moduleName, typeName)
	}
	var target *sem.Method
	for i := range module.Methods {
		if module.Methods[i].Name == methodName {
			target = &module.Methods[i]
			break
		}
	}
	if target == nil {
		t.Fatalf("missing method %s in %s.%s", methodName, moduleName, typeName)
	}
	if !target.IsAsm || target.AsmBody == nil {
		t.Fatalf("%s.%s.%s must be asm", moduleName, typeName, methodName)
	}
	offsets, widths := asmReceiverLayout(module)
	params := make([]ir.Value, 0, len(target.Params))
	for _, p := range target.Params {
		if p.Name == "self" {
			continue
		}
		returnType := "void"
		if p.Type != nil {
			returnType = p.Type.Name
		}
		params = append(params, &ir.Param{Symbol: p.Name, Type: ir.Type{Name: returnType}})
	}
	returnName := "void"
	if target.Return != nil {
		returnName = target.Return.Name
	}
	return ir.AsmMethod{
		Symbol:               asmMethodSymbol(moduleName, typeName, methodName),
		ReceiverType:         typeName,
		Params:               params,
		Return:               ir.Type{Name: returnName},
		Body:                 target.AsmBody.Source,
		ReceiverFieldOffsets: offsets,
		ReceiverFieldWidths:  widths,
	}
}

func asmReceiverLayout(typ *sem.Type) (map[string]int, map[string]int) {
	offsets := map[string]int{}
	widths := map[string]int{}
	fields := make([]layout.Field, 0, len(typ.Fields))
	for _, field := range typ.Fields {
		fields = append(fields, layout.Field{Name: field.Name, Type: field.Type.Name})
	}
	record, err := layout.Compute(fields)
	if err != nil {
		return offsets, widths
	}
	for _, field := range typ.Fields {
		layoutField := record.Fields[field.Name]
		offsets[field.Name] = layoutField.Offset
		widths[field.Name] = layoutField.Size * 8
	}
	return offsets, widths
}

func asmMethodSymbol(moduleName, typeName, methodName string) string {
	return "_wrela_method_" + strings.ReplaceAll(moduleName, ".", "_") + "_" + typeName + "_" + methodName
}

func parseUEFIModulesForCodegen(t *testing.T) []*ast.Module {
	t.Helper()
	workdir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	repoRoot := filepath.Clean(filepath.Join(workdir, "..", ".."))
	return parseUEFIModuleFiles(t, repoRoot)
}

func parseUEFIModuleFiles(t *testing.T, repoRoot string) []*ast.Module {
	t.Helper()
	paths := []string{
		filepath.Join(repoRoot, "wrela/platform/uefi/boot_services.wrela"),
		filepath.Join(repoRoot, "wrela/platform/uefi/transition.wrela"),
		filepath.Join(repoRoot, "wrela/platform/uefi/types.wrela"),
		filepath.Join(repoRoot, "wrela/machine/x86_64/cpu_state.wrela"),
		filepath.Join(repoRoot, "wrela/machine/x86_64/executor_memory.wrela"),
	}
	files := make([]*source.File, 0, len(paths)+1)
	for i, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		files = append(files, source.NewFile(source.FileID(i+1), path, string(raw)))
	}
	files = append(files, source.NewFile(source.FileID(len(files)+1), "uefi-test-harness.wrela", `
module codegen.uefi_test_harness
use { DelegatedHardware } from platform.uefi.transition
use { OwnedHardware, OwnedMemory, ExecutorPlacement, IoPortAuthority } from machine.x86_64.cpu_state
use { MemoryPlan, VirtualMemoryPlan, CpuPlan } from machine.x86_64.cpu_state
use { MutableBytes, ExecutorMemory, Bytes } from machine.x86_64.executor_memory

image UefiCodegenHarness {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        let arena = MutableBytes(address: 0, length: 0)
        let owned_memory = OwnedMemory(arena: arena)
        let exec_memory = ExecutorMemory(
            arena_base: 0,
            arena_length: 0,
            next_offset: 0
        )
        let vcpu0 = ExecutorPlacement(id: 0, memory: exec_memory)
        let memory_plan = MemoryPlan(
            owned_memory: owned_memory,
            executor_arena: MutableBytes(address: 0, length: 0),
            io_ports: IoPortAuthority()
        )
        let virtual_memory_plan = VirtualMemoryPlan(pml4: 0)
        let cpu_plan = CpuPlan(
            vcpu0: vcpu0,
            owned_stack_top: 0,
            gdt_descriptor: Bytes(address: 0, length: 0),
            idt_descriptor: Bytes(address: 0, length: 0),
            cr3: 0
        )
        return hardware.exit_to_owned_hardware(
            memory_plan: memory_plan,
            virtual_memory_plan: virtual_memory_plan,
            cpu_plan: cpu_plan
        )
    }

    phase owned_hardware(hardware: OwnedHardware) -> never {
        while true {}
    }
}
`))
	modules, ds := parse.ParseGraph(source.Graph{Files: files})
	if len(ds) != 0 {
		t.Fatalf("parse diagnostics: %#v", ds)
	}
	return modules
}
