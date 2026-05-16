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

func TestUEFISourceDeclaredFirmwareTableOffsets(t *testing.T) {
	checked := parseCheckedUEFIModules(t)
	program, ds := ir.Lower(checked)
	if len(ds) != 0 {
		t.Fatalf("Lower() diagnostics: %#v", ds)
	}

	system := program.Types["UefiSystemTable"]
	if system.Fields["boot_services"].Offset != 96 ||
		system.Fields["number_of_table_entries"].Offset != 104 ||
		system.Fields["configuration_tables"].Offset != 112 {
		t.Fatalf("UefiSystemTable layout = %#v, want BootServices at +96", system)
	}
	boot := program.Types["UefiBootServices"]
	if boot.Fields["get_memory_map"].Offset != 56 ||
		boot.Fields["exit_boot_services"].Offset != 232 {
		t.Fatalf("UefiBootServices layout = %#v, want GetMemoryMap +56 and ExitBootServices +232", boot)
	}
	result := program.Types["UefiMemoryMapResult"]
	if result.StorageSize < 80 ||
		result.Fields["status"].StorageOffset != 24 ||
		result.Fields["memory_map"].StorageOffset != 32 {
		t.Fatalf("UefiMemoryMapResult storage layout = %#v, want nested backing through +79", result)
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
	idt := asmMethodFromSem(t, checked, "platform.uefi.types", "DelegatedMemory", "build_interrupt_idt")
	if idt.Body == "" {
		t.Fatalf("build_interrupt_idt is not an asm method")
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
		"pdpt_slot_loop",
		"zero_new_pd_loop",
		"pdpt_slot_after_alloc_loop",
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
		t.Fatalf("lowerAndEncodeAsmMethod build_interrupt_idt diagnostics: %#v", ds)
	}
	assertInstructionOrder(t, idtInstructions, "push r12", "push r13", "push r14", "push r15", "pop r15", "pop r14", "pop r13", "pop r12", "ret")
	assertInstructionOrder(t, idtInstructions,
		"mov r14 rcx",
		"mov rcx imm",
		"mov r13 r14",
		"mov r13 r15",
	)
}

func TestIdentityPagingUsesTwoMiBPagesAsTargetGlue(t *testing.T) {
	checked := parseCheckedUEFIModules(t)
	identity := asmMethodFromSem(t, checked, "platform.uefi.types", "DelegatedMemory", "build_identity_paging")
	if identity.Body == "" {
		t.Fatalf("build_identity_paging is not an asm method")
	}
	for _, want := range []string{"add rax, 0x83", "add r13, 0x200000", "add r15, 0x200000"} {
		if !strings.Contains(identity.Body, want) {
			t.Fatalf("build_identity_paging must contain %q", want)
		}
	}
}

func TestInterruptIDTSourceShape(t *testing.T) {
	checked := parseCheckedUEFIModules(t)
	build := asmMethodFromSem(t, checked, "platform.uefi.types", "DelegatedMemory", "build_interrupt_idt")
	for _, want := range []string{
		"1040", "1056", "1072",
		"vector40_handler", "vector41_handler", "vector42_handler",
	} {
		if !strings.Contains(build.Body, want) {
			t.Fatalf("build_interrupt_idt missing %s:\n%s", want, build.Body)
		}
	}

	transition := methodFromSem(t, checked, "platform.uefi.transition", "DelegatedHardware", "exit_to_owned_hardware")
	if transition == nil {
		t.Fatalf("missing exit_to_owned_hardware")
	}
	if !stmtListContainsCall(transition.Body, "build_interrupt_idt") {
		t.Fatalf("exit_to_owned_hardware must call build_interrupt_idt")
	}
}

func TestInterruptPlatformSourceCodegen(t *testing.T) {
	checked := parseCheckedUEFIModules(t)
	methods := []ir.AsmMethod{
		asmMethodFromSem(t, checked, "machine.x86_64.interrupts", "ApicInterruptController", "enable_cpu_interrupts"),
		asmMethodFromSem(t, checked, "machine.x86_64.pci", "PciConfigPorts", "write32"),
		asmMethodFromSem(t, checked, "machine.x86_64.pci", "PciConfigPorts", "read32"),
	}
	var allText []byte
	for _, method := range methods {
		unit, ds := compileAsmMethodUnit(method)
		if len(ds) != 0 {
			t.Fatalf("compileAsmMethodUnit %q diagnostics: %#v", method.Symbol, ds)
		}
		allText = append(allText, unit.Bytes...)
	}
	for _, want := range [][]byte{{0xFB}, {0xEF}, {0xED}} {
		if !containsBytes(allText, want) {
			t.Fatalf("compiled platform source asm missing bytes %#x", want)
		}
	}
}

func TestPciInterruptConfiguratorWalksCapabilities(t *testing.T) {
	checked := parseCheckedUEFIModules(t)
	_ = asmMethodFromSem(t, checked, "machine.x86_64.pci", "PciConfigPorts", "read32")
	program, ds := ir.Lower(checked)
	if len(ds) != 0 {
		t.Fatalf("Lower diagnostics: %#v", ds)
	}
	findCap := findIRFunction(program, "_wrela_method_machine_x86_64_pci_Q35PciInterruptConfigurator_find_capability")
	if findCap == nil {
		t.Fatalf("missing find_capability lowering")
	}
	edu := findIRFunction(program, "_wrela_method_machine_x86_64_pci_Q35PciInterruptConfigurator_configure_edu_msi_vector41")
	if !functionCalls(edu, "_wrela_method_machine_x86_64_pci_Q35PciInterruptConfigurator_find_capability") {
		t.Fatalf("configure_edu_msi_vector41 must call find_capability")
	}
	for _, want := range []uint64{0xFEE00000, 0x41, 0x80, 0x00010000} {
		if !functionHasConstInt(edu, want) {
			t.Fatalf("configure_edu_msi_vector41 missing constant %#x", want)
		}
	}
	msix := findIRFunction(program, "_wrela_method_machine_x86_64_pci_Q35PciInterruptConfigurator_configure_ivshmem_msix_vector42")
	if !functionCalls(msix, "_wrela_method_machine_x86_64_pci_Q35PciInterruptConfigurator_find_capability") {
		t.Fatalf("configure_ivshmem_msix_vector42 must call find_capability")
	}
	for _, want := range []uint64{0xFEE00000, 0x42, 0x80000000} {
		if !functionHasConstInt(msix, want) {
			t.Fatalf("configure_ivshmem_msix_vector42 missing constant %#x", want)
		}
	}
}

func TestCacheArenaPutBytesContainsEvictionBump(t *testing.T) {
	checked := parseCheckedUEFIModules(t)
	method := asmMethodFromSem(t, checked, "machine.x86_64.cache_memory", "CacheArena", "put_bytes")
	instructions, ds, code := lowerAndEncodeAsmMethod(method)
	if len(ds) != 0 {
		t.Fatalf("lower/encode cache put diagnostics: %#v", ds)
	}
	if len(code) == 0 {
		t.Fatal("cache put compiled to empty code")
	}
	for _, label := range []string{"put_capacity_loop", "put_capacity_ok", "put_victim_in_range", "slot_offset_loop", "slot_ready", "copy_loop", "copy_done", "victim_ready", "put_fail"} {
		if !hasAsmLabel(instructions, label) {
			t.Fatalf("cache put missing label %q in:\n%s", label, method.Body)
		}
	}
	signatures := strings.Join(instructionSignatures(instructions), "\n")
	for _, want := range []string{
		"je put_fail",
		"cmp r14 rax",
		"jb put_fail",
		"jbe put_fits",
		"jb put_victim_in_range",
		"add r13 imm",
		"mov r11 [r11]",
		"mov [r11] imm",
		"mov [r11+8] rsi",
		"mov [r11+16] r12",
		"mov [rdi+24] r11",
		"mov [r10+8] r14",
		"mov [r10] imm",
	} {
		if !strings.Contains(signatures, want) {
			t.Fatalf("cache put missing lowered instruction %q in:\n%s", want, signatures)
		}
	}
}

func TestCacheArenaClearBoundsStorage(t *testing.T) {
	checked := parseCheckedUEFIModules(t)
	method := asmMethodFromSem(t, checked, "machine.x86_64.cache_memory", "CacheArena", "clear")
	instructions, ds := lowerAsmMethodInstructions(method)
	if len(ds) != 0 {
		t.Fatalf("lower cache clear diagnostics: %#v", ds)
	}
	for _, label := range []string{"clear_capacity_loop", "clear_loop", "clear_done", "clear_invalid"} {
		if !hasAsmLabel(instructions, label) {
			t.Fatalf("cache clear missing label %q in:\n%s", label, method.Body)
		}
	}
	signatures := strings.Join(instructionSignatures(instructions), "\n")
	for _, want := range []string{
		"mov r14 [r11+8]",
		"cmp r14 rax",
		"jb clear_invalid",
		"mov [rdi+32] imm",
	} {
		if !strings.Contains(signatures, want) {
			t.Fatalf("cache clear missing lowered instruction %q in:\n%s", want, signatures)
		}
	}
}

func TestCacheArenaGetBytesCopiesIntoFrame(t *testing.T) {
	checked := parseCheckedUEFIModules(t)
	method := asmMethodFromSem(t, checked, "machine.x86_64.cache_memory", "CacheArena", "get_bytes")
	instructions, ds := lowerAsmMethodInstructions(method)
	if len(ds) != 0 {
		t.Fatalf("lower cache get diagnostics: %#v", ds)
	}
	for _, label := range []string{"get_capacity_loop", "get_capacity_ok", "get_slot_loop", "get_hit", "frame_has_space", "get_copy_loop", "get_copy_done", "get_miss"} {
		if !hasAsmLabel(instructions, label) {
			t.Fatalf("cache get missing label %q in:\n%s", label, method.Body)
		}
	}

	unit, ds := compileAsmMethodUnit(method)
	if len(ds) != 0 {
		t.Fatalf("compile cache get diagnostics: %#v", ds)
	}
	foundOOM := false
	for _, rel := range unit.CallReloc {
		if rel.Symbol == "_wrela_memory_oom" {
			foundOOM = true
			break
		}
	}
	if !foundOOM {
		t.Fatalf("cache get must call _wrela_memory_oom on frame reserve failure: %#v", unit.CallReloc)
	}

	signatures := strings.Join(instructionSignatures(instructions), "\n")
	for _, want := range []string{
		"mov r8 [r8]",
		"cmp rax imm",
		"cmp r14 rax",
		"jb get_miss",
		"jb get_oom",
		"jbe frame_has_space",
		"mov [rdx+16] r15",
		"mov [r10] imm",
		"add r12 imm",
		"mov [r10+8] r12",
		"mov [r10+16] r11",
		"mov [r10+24] r14",
		"mov [r10+16] imm",
		"mov [r10+24] imm",
	} {
		if !strings.Contains(signatures, want) {
			t.Fatalf("cache get missing lowered instruction %q in:\n%s", want, signatures)
		}
	}
	if got := strings.Count(signatures, "mov [r10+8] r12"); got < 2 {
		t.Fatalf("cache get must install nested Bytes handles on hit and miss paths, got %d in:\n%s", got, signatures)
	}
}

func hasAsmLabel(instructions []asm.Instruction, label string) bool {
	for _, in := range instructions {
		if in.Label == label {
			return true
		}
	}
	return false
}

func findIRFunction(program *ir.Program, symbol string) *ir.Function {
	for i := range program.Functions {
		if program.Functions[i].Symbol == symbol {
			return &program.Functions[i]
		}
	}
	return nil
}

func functionHasConstInt(fn *ir.Function, value uint64) bool {
	if fn == nil {
		return false
	}
	for _, block := range fn.Blocks {
		if opsHaveConstInt(block.Ops, value) {
			return true
		}
	}
	return false
}

func functionCalls(fn *ir.Function, symbol string) bool {
	if fn == nil {
		return false
	}
	for _, block := range fn.Blocks {
		if opsCall(block.Ops, symbol) {
			return true
		}
	}
	return false
}

func opsHaveConstInt(ops []ir.Operation, value uint64) bool {
	for _, op := range ops {
		switch v := op.(type) {
		case *ir.ConstInt:
			if v.Value == value {
				return true
			}
		case *ir.If:
			if opsHaveConstInt(v.ConditionOps, value) || opsHaveConstInt(v.Then, value) || opsHaveConstInt(v.Else, value) {
				return true
			}
		case *ir.While:
			if opsHaveConstInt(v.ConditionOps, value) || opsHaveConstInt(v.Body, value) {
				return true
			}
		case *ir.ForBytes:
			if opsHaveConstInt(v.IterableOps, value) || opsHaveConstInt(v.Body, value) {
				return true
			}
		}
	}
	return false
}

func opsCall(ops []ir.Operation, symbol string) bool {
	for _, op := range ops {
		switch v := op.(type) {
		case *ir.Call:
			if v.Symbol == symbol {
				return true
			}
		case *ir.If:
			if opsCall(v.ConditionOps, symbol) || opsCall(v.Then, symbol) || opsCall(v.Else, symbol) {
				return true
			}
		case *ir.While:
			if opsCall(v.ConditionOps, symbol) || opsCall(v.Body, symbol) {
				return true
			}
		case *ir.ForBytes:
			if opsCall(v.IterableOps, symbol) || opsCall(v.Body, symbol) {
				return true
			}
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
	program, ds := ir.Lower(checked)
	if len(ds) != 0 {
		t.Fatalf("Lower() diagnostics: %#v", ds)
	}
	symbol := asmMethodSymbol(moduleName, typeName, methodName)
	for _, method := range program.AsmMethods {
		if method.Symbol == symbol {
			return method
		}
	}
	t.Fatalf("missing lowered asm method %s", symbol)
	return ir.AsmMethod{}
}

func methodFromSem(t *testing.T, checked *sem.CheckedProgram, moduleName, typeName, methodName string) *sem.Method {
	t.Helper()
	typ, ok := checked.Index.Lookup(moduleName, typeName)
	if !ok || typ == nil {
		t.Fatalf("missing type %s.%s", moduleName, typeName)
	}
	for i := range typ.Methods {
		if typ.Methods[i].Name == methodName {
			return &typ.Methods[i]
		}
	}
	return nil
}

func stmtListContainsCall(stmts []ast.Stmt, method string) bool {
	for _, stmt := range stmts {
		if expr, ok := stmt.(*ast.ExprStmt); ok {
			if call, ok := expr.Expr.(*ast.CallExpr); ok && call.Method == method {
				return true
			}
		}
		if let, ok := stmt.(*ast.LetStmt); ok {
			if call, ok := let.Expr.(*ast.CallExpr); ok && call.Method == method {
				return true
			}
		}
	}
	return false
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
		filepath.Join(repoRoot, "wrela/machine/x86_64/cache_memory.wrela"),
		filepath.Join(repoRoot, "wrela/machine/x86_64/serial.wrela"),
		filepath.Join(repoRoot, "wrela/machine/x86_64/interrupts.wrela"),
		filepath.Join(repoRoot, "wrela/machine/x86_64/pci.wrela"),
		filepath.Join(repoRoot, "wrela/machine/x86_64/edu.wrela"),
		filepath.Join(repoRoot, "wrela/machine/x86_64/ivshmem.wrela"),
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
use { MemoryPlan, CpuPlan } from machine.x86_64.cpu_state
use { MutableBytes, ExecutorMemory, Bytes } from machine.x86_64.executor_memory

image UefiCodegenHarness {
    transitions { delegated_hardware -> owned_hardware }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        let arena = MutableBytes(address = 0, length = 0)
        let owned_memory = OwnedMemory(arena = arena)
        let exec_memory = ExecutorMemory(
            arena_base = 0,
            arena_length = 0,
            next_offset = 0
        )
        let vcpu0 = ExecutorPlacement(id = 0, memory = exec_memory)
        let memory_plan = MemoryPlan(
            owned_memory = owned_memory,
            executor_arena = MutableBytes(address = 0, length = 0),
            io_ports = IoPortAuthority()
        )
        let cpu_plan = CpuPlan(
            vcpu0 = vcpu0,
            owned_stack_top = 0,
            gdt_descriptor = Bytes(address = 0, length = 0),
            idt_descriptor = Bytes(address = 0, length = 0),
            cr3 = 0
        )
        return hardware.exit_to_owned_hardware(
            memory_plan = memory_plan,
            cpu_plan = cpu_plan
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
