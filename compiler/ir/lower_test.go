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
	executorType := &sem.Type{
		Module: "examples.hello.program",
		Name:   "HelloWorld",
		Kind:   sem.KindExecutor,
		Methods: []sem.Method{{
			Name:   "run",
			Return: &sem.Type{Name: "never", Kind: sem.KindPrimitive},
		}, {
			Name:   "source_marker",
			Return: &sem.Type{Name: "void", Kind: sem.KindPrimitive},
		}},
	}
	checked := &sem.CheckedProgram{
		Modules: []*ast.Module{{
			Name: "examples.hello.program",
			Decls: []ast.Decl{&ast.ExecutorDecl{
				Name: "HelloWorld",
				Methods: []ast.MethodDecl{{
					Name:    "run",
					IsStart: true,
					Return:  "never",
					Body: []ast.Stmt{&ast.ExprStmt{Expr: &ast.CallExpr{
						Receiver: &ast.NameExpr{Name: "self"},
						Method:   "source_marker",
					}}},
				}},
			}},
		}},
		Index: &sem.Index{ByModule: map[string]map[string]*sem.Type{
			"examples.hello.program": {"HelloWorld": executorType},
		}},
	}
	program, diags := Lower(checked)
	if len(diags) != 0 {
		t.Fatalf("diags = %#v", diags)
	}

	executor := findFunction(program, "_wrela_method_examples_hello_program_HelloWorld_run")
	if executor == nil {
		t.Fatal("missing lowered executor start function")
	}
	if !functionCalls(*executor, "_wrela_method_examples_hello_program_HelloWorld_source_marker") {
		t.Fatalf("executor did not lower source call: %#v", executor.Blocks)
	}
	if method := findAsmMethod(program, "_wrela_method_examples_hello_program_HelloWorld_run"); method != nil && strings.Contains(method.Body, "mov dx, 0x03f8") {
		t.Fatalf("executor start still lowered to fixed serial asm: %q", method.Body)
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
