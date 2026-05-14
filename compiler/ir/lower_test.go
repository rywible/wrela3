package ir

import (
	"strings"
	"testing"

	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/sem"
)

func TestLowerReturnsCG0001ForNilProgram(t *testing.T) {
	_, diags := Lower(nil)
	if len(diags) != 1 || diags[0].Code != diag.CG0001 {
		t.Fatalf("diags = %#v, want one CG0001", diags)
	}
}

func TestLowerSynthesizesEntryAdapterFromImage(t *testing.T) {
	image := &sem.Type{Module: "m", Name: "Boot", Kind: sem.KindImage}
	checked := &sem.CheckedProgram{
		Index: &sem.Index{ByModule: map[string]map[string]*sem.Type{
			"m": {"Boot": image},
		}},
	}
	program, diags := Lower(checked)
	if len(diags) != 0 {
		t.Fatalf("diags = %#v", diags)
	}
	if program.Entry.Symbol != "_wrela_efi_entry" {
		t.Fatalf("entry symbol = %q", program.Entry.Symbol)
	}
	if program.Entry.DelegatedPhaseSymbol == "" || program.Entry.OwnedPhaseSymbol == "" {
		t.Fatalf("entry phase symbols not set: %#v", program.Entry)
	}
}

func TestLowerUsesSourceVisiblePhaseAndExecutorPath(t *testing.T) {
	ownedRoot := &sem.Type{Name: "OwnedHardware"}
	image := &sem.Type{Module: "examples.hello.main", Name: "HelloSerial", Kind: sem.KindImage}
	checked := &sem.CheckedProgram{
		Modules: []*ast.Module{{
			Name: "examples.hello.program",
			Decls: []ast.Decl{&ast.ExecutorDecl{
				Name: "HelloWorld",
				Methods: []ast.MethodDecl{{
					Name:    "run",
					IsStart: true,
					Body: []ast.Stmt{&ast.ExprStmt{Expr: &ast.StringLiteral{
						Value: "hello from wrela\n",
					}}},
				}},
			}},
		}},
		Index: &sem.Index{ByModule: map[string]map[string]*sem.Type{
			"examples.hello.main": {"HelloSerial": image},
		}},
		OwnedRoot: ownedRoot,
	}
	program, diags := Lower(checked)
	if len(diags) != 0 {
		t.Fatalf("diags = %#v", diags)
	}

	delegated := findFunction(program, program.Entry.DelegatedPhaseSymbol)
	if delegated == nil {
		t.Fatalf("delegated phase %q was not lowered as a function", program.Entry.DelegatedPhaseSymbol)
	}
	if !functionCalls(*delegated, "_wrela_method_platform_uefi_transition_DelegatedHardware_exit_to_owned_hardware") {
		t.Fatalf("delegated phase does not call source-visible ownership transfer: %#v", delegated.Blocks)
	}
	if method := findAsmMethod(program, program.Entry.DelegatedPhaseSymbol); method != nil && strings.Contains(method.Body, "mov rax, 0") {
		t.Fatalf("delegated phase still has placeholder asm body: %q", method.Body)
	}
	transfer := findAsmMethod(program, "_wrela_method_platform_uefi_transition_DelegatedHardware_exit_to_owned_hardware")
	if transfer == nil {
		t.Fatal("missing generated ownership transfer method")
	}
	for _, want := range []string{"fill_identity_pd:", "idt_gate_loop:", "shr rax, 16", "shr rax, 32", "fatal_halt:", "mov cr3, rax"} {
		if !strings.Contains(transfer.Body, want) {
			t.Fatalf("ownership transfer body missing %q in:\n%s", want, transfer.Body)
		}
	}

	owned := findFunction(program, program.Entry.OwnedPhaseSymbol)
	if owned == nil {
		t.Fatalf("owned phase %q was not lowered as a function", program.Entry.OwnedPhaseSymbol)
	}
	if !functionCalls(*owned, "_wrela_method_examples_hello_program_HelloWorld_run") {
		t.Fatalf("owned phase does not call executor start path: %#v", owned.Blocks)
	}

	executor := findAsmMethod(program, "_wrela_method_examples_hello_program_HelloWorld_run")
	if executor == nil {
		t.Fatal("missing lowered executor start method")
	}
	for _, want := range []string{"mov al, 104", "mov al, 10", "out dx, al", "hlt"} {
		if !strings.Contains(executor.Body, want) {
			t.Fatalf("executor body missing %q in:\n%s", want, executor.Body)
		}
	}
}

func TestLowerKeepsAsmMethodsThatUseUEFIBridgeInstructions(t *testing.T) {
	bootServices := &sem.Type{
		Module: "platform.uefi.boot_services",
		Name:   "UefiBootServicesCalls",
		Kind:   sem.KindClass,
		Methods: []sem.Method{{
			Name:    "exit_boot_services",
			IsAsm:   true,
			AsmBody: &ast.AsmBody{Source: "sub rsp, 32\nmov rax, self.boot_services\ncall rax\nadd rsp, 32\nret"},
		}},
	}
	checked := &sem.CheckedProgram{
		Index: &sem.Index{ByModule: map[string]map[string]*sem.Type{
			"platform.uefi.boot_services": {"UefiBootServicesCalls": bootServices},
		}},
	}
	program, diags := Lower(checked)
	if len(diags) != 0 {
		t.Fatalf("diags = %#v", diags)
	}
	method := findAsmMethod(program, "_wrela_method_platform_uefi_boot_services_UefiBootServicesCalls_exit_boot_services")
	if method == nil {
		t.Fatal("asm method using call rax was skipped")
	}
	if !strings.Contains(method.Body, "call rax") {
		t.Fatalf("asm body changed unexpectedly: %q", method.Body)
	}
}

func findFunction(program *Program, symbol string) *Function {
	for i := range program.Functions {
		if program.Functions[i].Symbol == symbol {
			return &program.Functions[i]
		}
	}
	return nil
}

func functionCalls(fn Function, symbol string) bool {
	for _, block := range fn.Blocks {
		for _, op := range block.Ops {
			if call, ok := op.(*Call); ok && call.Symbol == symbol {
				return true
			}
		}
	}
	return false
}

func findAsmMethod(program *Program, symbol string) *AsmMethod {
	for i := range program.AsmMethods {
		if program.AsmMethods[i].Symbol == symbol {
			return &program.AsmMethods[i]
		}
	}
	return nil
}
