package codegen

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryanwible/wrela3/compiler/asm"
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
	instructions, ds, _ := lowerAndEncodeAsmMethod(getMap)
	if len(ds) != 0 {
		t.Fatalf("lowerAndEncodeAsmMethod get_memory_map diagnostics: %#v", ds)
	}
	if !hasInstructionSequence(instructions,
		"mov r10 [rax+16]",
		"mov r11 [rax+32]",
		"mov [r11+8] r10",
	) {
		t.Fatalf("get_memory_map must copy returned MemoryMapSize into descriptors.length: %#v", instructionSignatures(instructions))
	}

	exitBS := asmMethodFromSem(t, checked, "platform.uefi.boot_services", "UefiBootServicesCalls", "exit_boot_services")
	exitUnit, ds := compileAsmMethodUnit(exitBS)
	if len(ds) != 0 {
		t.Fatalf("compileAsmMethodUnit exit_boot_services diagnostics: %#v", ds)
	}
	if !bytes.Contains(exitUnit.Bytes, []byte{0x48, 0x83, 0xEC, 0x30}) {
		t.Fatalf("missing 48 83 EC 30 shadow/spill-frame setup in exit_boot_services: %x", exitUnit.Bytes)
	}
	if !bytes.Contains(exitUnit.Bytes, []byte{0xFF, 0xD0}) {
		t.Fatalf("missing call rax in exit_boot_services: %x", exitUnit.Bytes)
	}
	if !bytes.Contains(exitUnit.Bytes, []byte{0x49, 0x89, 0x03}) {
		t.Fatalf("exit_boot_services must store firmware status into hidden UefiStatus return slot: %x", exitUnit.Bytes)
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
	instructions, ds, _ := lowerAndEncodeAsmMethod(activate)
	if len(ds) != 0 {
		t.Fatalf("lowerAndEncodeAsmMethod activate_owned_hardware diagnostics: %#v", ds)
	}
	assertInstructionOrder(t, instructions,
		"mov rax [rsp]",
		"mov rsp rsi",
		"push rax",
		"push rax",
		"push rax",
		"ret",
	)
}

func TestUEFISerialWrite8PreservesValueBeforePortRegisterSetup(t *testing.T) {
	checked := parseCheckedUEFIModules(t)
	write8 := asmMethodFromSem(t, checked, "machine.x86_64.serial", "SerialWriterRegisters", "write8")
	unit, ds := compileAsmMethodUnit(write8)
	if len(ds) != 0 {
		t.Fatalf("compileAsmMethodUnit write8 diagnostics: %#v", ds)
	}
	valueMove := bytes.Index(unit.Bytes, []byte{0x8A, 0xC2})      // mov al, dl
	portLoad := bytes.Index(unit.Bytes, []byte{0x66, 0x8B, 0x17}) // mov dx, [rdi]
	if valueMove < 0 || portLoad < 0 {
		t.Fatalf("write8 missing value move or port load: %x", unit.Bytes)
	}
	if valueMove > portLoad {
		t.Fatalf("write8 must move value into al before dx is reused for the port: %x", unit.Bytes)
	}
}

func TestUEFIBuilderAsmMethodCompilation(t *testing.T) {
	checked := parseCheckedUEFIModules(t)
	identity := asmMethodFromSem(t, checked, "platform.uefi.types", "DelegatedMemory", "build_identity_paging")
	if identity.Body == "" {
		t.Fatalf("build_identity_paging is not an asm method")
	}
	gdt := asmMethodFromSem(t, checked, "platform.uefi.types", "DelegatedMemory", "build_owned_gdt")
	if gdt.Body == "" {
		t.Fatalf("build_owned_gdt is not an asm method")
	}
	idt := asmMethodFromSem(t, checked, "platform.uefi.types", "DelegatedMemory", "build_fatal_idt")
	if idt.Body == "" {
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

	identityInstructions, ds, _ := lowerAndEncodeAsmMethod(identity)
	if len(ds) != 0 {
		t.Fatalf("lowerAndEncodeAsmMethod build_identity_paging diagnostics: %#v", ds)
	}
	for _, want := range []string{
		"descriptor_loop",
		"map_descriptor_pages",
		"advance_descriptor",
	} {
		if !hasAsmLabel(identityInstructions, want) {
			t.Fatalf("build_identity_paging lowered asm missing label %q", want)
		}
	}
	for _, want := range []asm.MemOperand{
		{Base: asm.MustLookup("rsi"), Disp: 0},
		{Base: asm.MustLookup("rsi"), Disp: 8},
		{Base: asm.MustLookup("r8"), Disp: 8},
		{Base: asm.MustLookup("r8"), Disp: 24},
	} {
		if !hasAsmMemoryLoad(identityInstructions, want) {
			t.Fatalf("build_identity_paging lowered asm missing memory_map/descriptor load %#v", want)
		}
	}
	assertInstructionOrder(t, identityInstructions,
		"push rbx",
		"push r12",
		"push r13",
		"push r14",
		"push r15",
		"add r10 imm",
		"and r10 rax",
		"pop r15",
		"pop r14",
		"pop r13",
		"pop r12",
		"pop rbx",
		"ret",
	)

	gdtUnit, ds := compileAsmMethodUnit(gdt)
	if len(ds) != 0 {
		t.Fatalf("compileAsmMethodUnit build_owned_gdt diagnostics: %#v", ds)
	}
	for name, want := range map[string][]byte{
		"code descriptor": {0x48, 0xB8, 0xFF, 0xFF, 0x00, 0x00, 0x00, 0x9A, 0xAF, 0x00},
		"data descriptor": {0x48, 0xB8, 0xFF, 0xFF, 0x00, 0x00, 0x00, 0x92, 0xCF, 0x00},
	} {
		if !bytes.Contains(gdtUnit.Bytes, want) {
			t.Fatalf("build_owned_gdt missing %s immediate %x in %x", name, want, gdtUnit.Bytes)
		}
	}
	gdtInstructions, ds, _ := lowerAndEncodeAsmMethod(gdt)
	if len(ds) != 0 {
		t.Fatalf("lowerAndEncodeAsmMethod build_owned_gdt diagnostics: %#v", ds)
	}
	assertInstructionOrder(t, gdtInstructions, "push r12", "push r14", "pop r14", "pop r12", "ret")

	idtInstructions, ds, _ := lowerAndEncodeAsmMethod(idt)
	if len(ds) != 0 {
		t.Fatalf("lowerAndEncodeAsmMethod build_fatal_idt diagnostics: %#v", ds)
	}
	assertInstructionOrder(t, idtInstructions, "push r12", "push r13", "push r14", "pop r14", "pop r13", "pop r12", "ret")
}

func hasAsmLabel(instructions []asm.Instruction, label string) bool {
	for _, in := range instructions {
		if in.Label == label {
			return true
		}
	}
	return false
}

func hasAsmMemoryLoad(instructions []asm.Instruction, want asm.MemOperand) bool {
	for _, in := range instructions {
		if in.Mnemonic != "mov" || len(in.Operands) != 2 {
			continue
		}
		mem, ok := in.Operands[1].(asm.MemOperand)
		if !ok {
			continue
		}
		if mem.Base.Name == want.Base.Name && mem.Disp == want.Disp && mem.Width == want.Width {
			return true
		}
	}
	return false
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

func assertInstructionOrder(t *testing.T, instructions []asm.Instruction, want ...string) {
	t.Helper()
	next := 0
	for _, instruction := range instructions {
		if next == len(want) {
			return
		}
		if instructionSignature(instruction) == want[next] {
			next++
		}
	}
	if next != len(want) {
		t.Fatalf("missing instruction order item %q in %#v", want[next], instructionSignatures(instructions))
	}
}

func hasInstructionSequence(instructions []asm.Instruction, want ...string) bool {
	next := 0
	for _, instruction := range instructions {
		if next == len(want) {
			return true
		}
		if instructionSignature(instruction) == want[next] {
			next++
		}
	}
	return next == len(want)
}

func instructionSignatures(instructions []asm.Instruction) []string {
	out := make([]string, 0, len(instructions))
	for _, instruction := range instructions {
		signature := instructionSignature(instruction)
		if signature != "" {
			out = append(out, signature)
		}
	}
	return out
}

func instructionSignature(instruction asm.Instruction) string {
	if instruction.Mnemonic == "" {
		return ""
	}
	parts := []string{instruction.Mnemonic}
	for _, operand := range instruction.Operands {
		parts = append(parts, operandSignature(operand))
	}
	return strings.Join(parts, " ")
}

func operandSignature(operand asm.Operand) string {
	switch op := operand.(type) {
	case asm.RegOperand:
		return op.Reg.Name
	case asm.MemOperand:
		if op.Disp == 0 {
			return "[" + op.Base.Name + "]"
		}
		if op.Disp > 0 {
			return fmt.Sprintf("[%s+%d]", op.Base.Name, op.Disp)
		}
		return fmt.Sprintf("[%s%d]", op.Base.Name, op.Disp)
	case asm.ImmOperand:
		return "imm"
	case asm.LabelRef:
		return op.Name
	default:
		return "operand"
	}
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
		filepath.Join(repoRoot, "wrela/machine/x86_64/serial.wrela"),
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
