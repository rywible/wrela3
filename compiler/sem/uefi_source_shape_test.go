package sem

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/parse"
	"github.com/ryanwible/wrela3/compiler/source"
)

func TestUEFIPlatformBootServicesAndTransitionAsmShapes(t *testing.T) {
	modules := parseUEFIModuleSet(t)
	index, ds := BuildIndex(modules)
	if len(ds) != 0 {
		t.Fatalf("build index diagnostics: %#v", ds)
	}
	checked, ds := Check(index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}

	bootServices := moduleType(t, checked.Index, "platform.uefi.boot_services", "UefiBootServicesCalls")
	getMap := methodByName(t, bootServices, "get_memory_map")
	exitBS := methodByName(t, bootServices, "exit_boot_services")

	for _, want := range []string{
		"sub rsp, 48",
		"mov rcx, [rbp - 8]",
		"add rcx, 16",
		"mov rax, [rsi + 8]",
		"mov [rcx], rax",
		"add r11, 24",
		"mov [rax], r11",
		"add r11, 32",
		"mov [rax + 8], r11",
		"add r11, 64",
		"mov [rax + 32], r11",
		"mov [rax + 64], r10",
		"mov [rax + 72], r10",
		"mov rdx, [rsi]",
		"mov r8, [rbp - 8]",
		"add r8, 56",
		"mov r9, [rbp - 8]",
		"add r9, 40",
		"add r11, 48",
		"mov [rsp + 32], r11",
		"mov rax, [rax + UefiBootServices.get_memory_map]",
		"call rax",
		"mov r10, [rax]",
		"mov [r10], r11",
		"mov r10, [rax + 16]",
		"mov r11, [rax + 32]",
		"mov [r11 + 8], r10",
	} {
		if !strings.Contains(getMap.AsmBody.Source, want) {
			t.Fatalf("get_memory_map body missing %q in:\n%s", want, getMap.AsmBody.Source)
		}
	}
	if strings.Contains(getMap.AsmBody.Source, "mov rax, [rax]\n        mov rdx") {
		t.Fatalf("get_memory_map should use the firmware BootServices table handle directly: %s", getMap.AsmBody.Source)
	}

	if !strings.Contains(exitBS.AsmBody.Source, "sub rsp, 48") {
		t.Fatalf("exit_boot_services body missing aligned shadow/spill frame: %s", exitBS.AsmBody.Source)
	}
	if !strings.Contains(exitBS.AsmBody.Source, "mov rax, [rax + UefiBootServices.exit_boot_services]") {
		t.Fatalf("exit_boot_services body missing source-declared UEFI table offset: %s", exitBS.AsmBody.Source)
	}
	if strings.Contains(exitBS.AsmBody.Source, "mov rax, [rax]\n        mov rax, [rax + 232]") {
		t.Fatalf("exit_boot_services should use the firmware BootServices table handle directly: %s", exitBS.AsmBody.Source)
	}
	if !strings.Contains(exitBS.AsmBody.Source, "mov rcx, [rsi]") {
		t.Fatalf("exit_boot_services body missing image arg move: %s", exitBS.AsmBody.Source)
	}
	if !strings.Contains(exitBS.AsmBody.Source, "mov [r11], rax") || !strings.Contains(exitBS.AsmBody.Source, "mov rax, r11") {
		t.Fatalf("exit_boot_services must materialize and return UefiStatus handle: %s", exitBS.AsmBody.Source)
	}

	transition := moduleType(t, checked.Index, "platform.uefi.transition", "DelegatedHardware")
	activate := methodByName(t, transition, "activate_owned_hardware")
	for _, want := range []string{
		"cli",
		"mov rax, [rsp]",
		"mov rsp, owned_stack_top",
		"push rax",
		"push rax",
		"push rax",
		"mov cr3, cr3_value",
		"lgdt [r11]",
		"call reload_cs",
		"lidt [r11]",
		"mov ds, ax",
		"mov es, ax",
		"mov ss, ax",
		"mov fs, ax",
		"mov gs, ax",
		"retfq",
		"pop rax",
		"push 0x08",
	} {
		if !strings.Contains(activate.AsmBody.Source, want) {
			t.Fatalf("activate_owned_hardware body missing %q in:\n%s", want, activate.AsmBody.Source)
		}
	}
	capture := methodByName(t, transition, "capture_fatal_idt_handler")
	if !capture.IsAsm || capture.AsmBody == nil {
		t.Fatalf("missing capture_fatal_idt_handler asm method")
	}
	if !strings.Contains(capture.AsmBody.Source, "call fatal_idt_capture_return") {
		t.Fatalf("capture_fatal_idt_handler missing capture call: %s", capture.AsmBody.Source)
	}
	if !strings.Contains(capture.AsmBody.Source, "fatal_idt_capture_return") {
		t.Fatalf("capture_fatal_idt_handler missing capture label: %s", capture.AsmBody.Source)
	}
	if !strings.Contains(capture.AsmBody.Source, "hlt") {
		t.Fatalf("capture_fatal_idt_handler must define fallback handler path: %s", capture.AsmBody.Source)
	}
}

func TestUEFIPlatformBuildersAreNonPlaceholder(t *testing.T) {
	modules := parseUEFIModuleSet(t)
	index, ds := BuildIndex(modules)
	if len(ds) != 0 {
		t.Fatalf("build index diagnostics: %#v", ds)
	}
	checked, ds := Check(index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}

	memory := moduleType(t, checked.Index, "platform.uefi.types", "DelegatedMemory")
	identity := methodByName(t, memory, "build_identity_paging")
	gdt := methodByName(t, memory, "build_owned_gdt")
	idt := methodByName(t, memory, "build_interrupt_idt")
	fatalHandler := methodByName(t, memory, "fatal_idt_handler")

	if !identity.IsAsm || identity.AsmBody == nil {
		t.Fatalf("build_identity_paging must be asm and non-empty")
	}
	if !gdt.IsAsm || gdt.AsmBody == nil {
		t.Fatalf("build_owned_gdt must be asm and non-empty")
	}
	if !idt.IsAsm || idt.AsmBody == nil {
		t.Fatalf("build_interrupt_idt must be asm and non-empty")
	}

	for _, want := range []string{
		"self.next_offset",
		"self.arena_base",
		"push rbx",
		"push r12",
		"push r13",
		"push r14",
		"push r15",
		"add r10, 4095",
		"and r10, rax",
		"mov rbx, r11",
		"memory_map",
		"descriptor_size",
		"descriptor_loop",
		"map_descriptor_pages",
		"mov r8, [rsi]",
		"mov r8, [r8]",
		"mov r9, [rsi]",
		"mov r9, [r9 + 8]",
		"mov rax, [r8 + 8]",
		"mov rcx, [r8 + 24]",
		"mov [rbx], rax",
		"pdpt_slot_loop",
		"zero_new_pd_loop",
		"pdpt_slot_after_alloc_loop",
		"reduce_pd_index",
		"mov self.next_offset, r14",
		"jne zero_loop",
		"pop r15",
		"pop r14",
		"pop r13",
		"pop r12",
		"pop rbx",
	} {
		if !strings.Contains(identity.AsmBody.Source, want) {
			t.Fatalf("build_identity_paging asm body missing %q in:\n%s", want, identity.AsmBody.Source)
		}
	}
	if strings.Contains(identity.AsmBody.Source, "cmp r14, 512\n        jge advance_descriptor") {
		t.Fatalf("build_identity_paging must not skip all mappings above first 1GiB:\n%s", identity.AsmBody.Source)
	}
	for _, want := range []string{
		"self.next_offset",
		"push r12",
		"push r14",
		"0x00AF9A000000FFFF",
		"0x00CF92000000FFFF",
		"mov [r10], r11",
		"mov [r10 + 8], 40",
		"mov rax, r10",
		"pop r14",
		"pop r12",
	} {
		if !strings.Contains(gdt.AsmBody.Source, want) {
			t.Fatalf("build_owned_gdt asm body missing %q in:\n%s", want, gdt.AsmBody.Source)
		}
	}
	for _, want := range []string{
		"self.next_offset",
		"push r12",
		"push r13",
		"push r14",
		"push r15",
		"fatal_handler",
		"vector40_handler",
		"vector41_handler",
		"vector42_handler",
		"256",
		"jne idt_gate_loop",
		"1040",
		"1056",
		"1072",
		"mov [r10 + 0], r11",
		"mov [r10 + 8], 4112",
		"mov rax, r10",
		"pop r15",
		"pop r14",
		"pop r13",
		"pop r12",
	} {
		if !strings.Contains(idt.AsmBody.Source, want) {
			t.Fatalf("build_interrupt_idt asm body missing %q in:\n%s", want, idt.AsmBody.Source)
		}
	}
	if fatalHandler.AsmBody == nil {
		t.Fatalf("missing fatal_idt_handler asm method")
	}
	if !strings.Contains(fatalHandler.AsmBody.Source, "hlt") {
		t.Fatalf("fatal_idt_handler must include hlt: %s", fatalHandler.AsmBody.Source)
	}
}

func TestUEFIMemoryMapFieldOrderMatchesPlan(t *testing.T) {
	modules := parseUEFIModuleSet(t)
	index, ds := BuildIndex(modules)
	if len(ds) != 0 {
		t.Fatalf("build index diagnostics: %#v", ds)
	}
	memoryMap := moduleType(t, index, "platform.uefi.types", "UefiMemoryMap")
	got := make([]string, 0, len(memoryMap.Fields))
	for _, field := range memoryMap.Fields {
		got = append(got, field.Name)
	}
	want := []string{"descriptors", "descriptor_size", "descriptor_version", "key"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("UefiMemoryMap fields = %#v, want %#v", got, want)
	}
}

func TestMachineX64InterruptSupportAsmIsAllowed(t *testing.T) {
	_, ds := checkModuleForTest(t, `
module machine.x86_64.interrupts
class ApicInterruptController {
    asm fn enable_cpu_interrupts(self) {
        sti
    }
}`)
	if len(ds) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", ds)
	}
}

func parseUEFIModuleSet(t *testing.T) []*ast.Module {
	t.Helper()
	workdir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	repoRoot := filepath.Clean(filepath.Join(workdir, "..", ".."))
	paths := []string{
		filepath.Join(repoRoot, "wrela/platform/uefi/boot_services.wrela"),
		filepath.Join(repoRoot, "wrela/platform/uefi/transition.wrela"),
		filepath.Join(repoRoot, "wrela/platform/uefi/types.wrela"),
		filepath.Join(repoRoot, "wrela/machine/x86_64/cpu_state.wrela"),
		filepath.Join(repoRoot, "wrela/machine/x86_64/executor_memory.wrela"),
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
module sem.uefi_test_harness
use { DelegatedHardware } from platform.uefi.transition
use { OwnedHardware, OwnedMemory, ExecutorPlacement, IoPortAuthority } from machine.x86_64.cpu_state
use { MemoryPlan, VirtualMemoryPlan, CpuPlan } from machine.x86_64.cpu_state
use { MutableBytes, ExecutorMemory, Bytes } from machine.x86_64.executor_memory

image UefiSourceHarness {
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
        let virtual_memory_plan = VirtualMemoryPlan(pml4 = 0)
        let cpu_plan = CpuPlan(
            vcpu0 = vcpu0,
            owned_stack_top = 0,
            gdt_descriptor = Bytes(address = 0, length = 0),
            idt_descriptor = Bytes(address = 0, length = 0),
            cr3 = 0
        )
        return hardware.exit_to_owned_hardware(
            memory_plan = memory_plan,
            virtual_memory_plan = virtual_memory_plan,
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

func moduleType(t *testing.T, index *Index, moduleName, typeName string) *Type {
	t.Helper()
	typ, ok := index.Lookup(moduleName, typeName)
	if !ok {
		t.Fatalf("missing %s.%s", moduleName, typeName)
	}
	return typ
}

func methodByName(t *testing.T, typ *Type, name string) *Method {
	t.Helper()
	for i := range typ.Methods {
		if typ.Methods[i].Name == name {
			return &typ.Methods[i]
		}
	}
	t.Fatalf("missing %s.%s method", typ.Name, name)
	return nil
}
