package parse

import (
	"fmt"
	"testing"

	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/source"
)

func parseModuleForTest(t *testing.T, src string) (*ast.Module, []diag.Diagnostic) {
	t.Helper()
	p := newParser("mod.wrela", src)
	return p.ParseModule()
}

func TestParseDecls(t *testing.T) {
	_, ds := parseModuleForTest(t, "module m\nunique data Bad {}")
	if len(ds) != 1 || ds[0].Code != diag.PAR0002 {
		t.Fatalf("diagnostics = %#v, want PAR0002", ds)
	}

	_, ds = parseModuleForTest(t, "module m\ndriver path Path {\n  field: U8\n}")
	if len(ds) != 0 {
		t.Fatalf("driver path diagnostics = %#v", ds)
	}

	_, ds = parseModuleForTest(t, "module m\nunique executor Exe {\n}")
	if len(ds) != 1 || ds[0].Code != diag.PAR0002 {
		t.Fatalf("executor diagnostics = %#v, want PAR0002", ds)
	}

	_, ds = parseModuleForTest(t, "module m\nunique driver Ex {}\n")
	if len(ds) != 0 {
		t.Fatalf("driver diagnostics = %#v", ds)
	}

	mod, ds := parseModuleForTest(t, "module m\nfn bad() {}")
	if len(ds) != 1 || ds[0].Code != diag.PAR0002 {
		t.Fatalf("module-scope fn diagnostics = %#v, want PAR0002", ds)
	}
	if mod != nil {
		t.Fatalf("module should not be returned on module-scope fn error")
	}
}

func TestParseImageDeclTransitionsAndPhases(t *testing.T) {
	mod, ds := parseModuleForTest(t, `
module m
image Boot {
  transitions {
    DelegatedHardware -> OwnedHardware
  }
  phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {}
  phase owned_hardware(hardware: OwnedHardware) -> never {}
}
`)
	if len(ds) != 0 {
		t.Fatalf("diagnostics = %#v", ds)
	}
	if len(mod.Decls) != 1 {
		t.Fatalf("decls = %d, want 1", len(mod.Decls))
	}
	img, ok := mod.Decls[0].(*ast.ImageDecl)
	if !ok {
		t.Fatalf("decl = %#v, want *ast.ImageDecl", mod.Decls[0])
	}
	if len(img.Transitions) != 1 || img.Transitions[0].From != "DelegatedHardware" || img.Transitions[0].To != "OwnedHardware" {
		t.Fatalf("transitions = %#v", img.Transitions)
	}
	if len(img.Phases) != 2 {
		t.Fatalf("phases = %d, want 2", len(img.Phases))
	}
}

func TestParseStatements(t *testing.T) {
	mod, ds := parseModuleForTest(t, `
module m
class Writer {
  fn write_byte(value: Byte) {
    let byte = 1
    byte = 2
    if true { return byte }
    while false { byte = 2 }
    for b in bytes { self.write_byte(byte: b) }
  }
}`)
	if len(ds) != 0 {
		t.Fatalf("diagnostics = %#v", ds)
	}
	if len(mod.Decls) != 1 {
		t.Fatalf("declarations = %d, want 1", len(mod.Decls))
	}
	cl := mod.Decls[0].(*ast.ClassDecl)
	if len(cl.Methods) != 1 {
		t.Fatalf("methods = %d, want 1", len(cl.Methods))
	}
	body := cl.Methods[0].Body
	if len(body) != 5 {
		t.Fatalf("body statements = %d, want 5", len(body))
	}
	if _, ok := body[0].(*ast.LetStmt); !ok {
		t.Fatalf("stmt0 = %#v, want *ast.LetStmt", body[0])
	}
	assign, ok := body[1].(*ast.AssignStmt)
	if !ok {
		t.Fatalf("stmt1 = %#v, want *ast.AssignStmt", body[1])
	}
	if _, ok := assign.Target.(*ast.NameExpr); !ok {
		t.Fatalf("assignment target = %#v, want *ast.NameExpr", assign.Target)
	}
	if _, ok := body[2].(*ast.IfStmt); !ok {
		t.Fatalf("stmt2 = %#v, want *ast.IfStmt", body[2])
	}
	if _, ok := body[4].(*ast.ForStmt); !ok {
		t.Fatalf("stmt4 = %#v, want *ast.ForStmt", body[4])
	}
}

func TestParseStmtAsmAndCapture(t *testing.T) {
	mod, ds := parseModuleForTest(t, `
module m
class Writer {
  asm fn boot() {
    mov ax, 1
    add ax, 2
  }
}`)
	if len(ds) != 0 {
		t.Fatalf("diagnostics = %#v", ds)
	}
	m, ok := mod.Decls[0].(*ast.ClassDecl)
	if !ok {
		t.Fatalf("decl = %#v", mod.Decls[0])
	}
	method := m.Methods[0]
	if !method.IsAsm || method.Asm == nil {
		t.Fatalf("method = %#v", method)
	}
	if got, want := method.Asm.Source, "\n    mov ax, 1\n    add ax, 2\n  "; got != want {
		t.Fatalf("asm source = %q, want %q", got, want)
	}

	_, ds = parseModuleForTest(t, `
module m
class Writer {
  fn boot() {
    asm { hlt }
  }
}`)
	if len(ds) != 1 || ds[0].Code != diag.PAR0001 {
		t.Fatalf("inline asm diagnostics = %#v, want PAR0001", ds)
	}
}

func TestParseCanonicalMethodShapes(t *testing.T) {
	src := `module parser.methods

driver path SerialWritePath {
    port_base: U16

    asm fn write8(self, offset: U16, value: U8) {
        out dx, al
        ret
    }

    fn write(self, bytes: Bytes) {
        self.registers.write8(offset: 0, value: byte)
        self.pause()
    }
}

executor HelloWorld {
    start fn run(self) -> never {
        self.serial_path.write(self.memory.static_bytes("hello"))
    }
}`
	mod, ds := parseModuleForTest(t, src)
	if len(ds) != 0 {
		t.Fatalf("diagnostics: %#v", ds)
	}
	if len(mod.Decls) != 2 {
		t.Fatalf("decl count = %d, want 2", len(mod.Decls))
	}
	path, ok := mod.Decls[0].(*ast.DriverPathDecl)
	if !ok {
		t.Fatalf("decl 0 = %T, want DriverPathDecl", mod.Decls[0])
	}
	if len(path.Methods) != 2 {
		t.Fatalf("driver path methods = %d, want 2", len(path.Methods))
	}
	exec := mod.Decls[1].(*ast.ExecutorDecl)
	if got := exec.Methods[0].Return; got != "never" {
		t.Fatalf("start fn return = %q, want never", got)
	}
	expr := exec.Methods[0].Body[0].(*ast.ExprStmt).Expr.(*ast.CallExpr)
	if len(expr.Args) != 1 || expr.Args[0].Name != "" {
		t.Fatalf("positional call args = %#v, want one unnamed arg", expr.Args)
	}
}

func TestAdjacentMethodsMayShareLineWhenBraceSeparates(t *testing.T) {
	mod, ds := parseModuleForTest(t, `module m
class C { fn a() {} fn b() {} }`)
	if len(ds) != 0 {
		t.Fatalf("diagnostics = %#v", ds)
	}
	cl := mod.Decls[0].(*ast.ClassDecl)
	if got, want := len(cl.Methods), 2; got != want {
		t.Fatalf("methods = %d, want %d", got, want)
	}
}

func TestParseGraphSortsDiagnostics(t *testing.T) {
	f1 := source.NewFile(1, "z.wrela", "module z\nunique executor Exe {}")
	f2 := source.NewFile(2, "a.wrela", "module a\nfn bad() {}")
	_, ds := ParseGraph(source.Graph{Files: []*source.File{f1, f2}})
	if len(ds) != 2 {
		t.Fatalf("diagnostics = %#v", ds)
	}
	if ds[0].FilePath != "a.wrela" || ds[1].FilePath != "z.wrela" {
		t.Fatalf("sorted file paths = %s, %s", ds[0].FilePath, ds[1].FilePath)
	}
}

func TestParseGraphParsesAllFiles(t *testing.T) {
	mod := parseGraphFromFiles(t, []string{"module a", "module b"})
	if len(mod) != 2 {
		t.Fatalf("modules = %d, want 2", len(mod))
	}
}

func parseGraphFromFiles(t *testing.T, sources []string) []*ast.Module {
	t.Helper()
	files := make([]*source.File, len(sources))
	for i, sourceText := range sources {
		files[i] = source.NewFile(source.FileID(i+1), fmt.Sprintf("f%d.wrela", i), sourceText)
	}
	modules, ds := ParseGraph(source.Graph{Files: files})
	if len(ds) != 0 {
		t.Fatalf("diagnostics = %#v", ds)
	}
	return modules
}
