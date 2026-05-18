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

	_, ds = parseModuleForTest(t, "module m\nunique trait Bad {}\n")
	if len(ds) != 1 || ds[0].Code != diag.PAR0002 {
		t.Fatalf("unique trait diagnostics = %#v, want PAR0002", ds)
	}

	_, ds = parseModuleForTest(t, "module m\nunique impl Publisher<U64> for TopicPublisher<U64>\n")
	if len(ds) != 1 || ds[0].Code != diag.PAR0002 {
		t.Fatalf("unique impl diagnostics = %#v, want PAR0002", ds)
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
    for b in bytes { self.write_byte(byte = b) }
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

func TestParseWithStatement(t *testing.T) {
	src := `
module parser.with_stmt

class Memory {}

executor Worker {
    memory: Memory

    start fn run(self) -> never {
        with self.memory.frame(length = 65536) as tick {
            let raw = tick.reserve(length = 32, align = 8)
        }
        while true {}
    }
}
`
	mod, ds := parseModuleForTest(t, src)
	if len(ds) != 0 {
		t.Fatalf("parse diagnostics: %#v", ds)
	}
	exec := mod.Decls[1].(*ast.ExecutorDecl)
	stmt := exec.Methods[0].Body[0]
	with, ok := stmt.(*ast.WithStmt)
	if !ok {
		t.Fatalf("first statement = %T, want *ast.WithStmt", stmt)
	}
	if with.Name != "tick" || len(with.Body) != 1 {
		t.Fatalf("with = %#v, want bound tick with one body statement", with)
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
	        self.registers.write8(offset = 0, value = byte)
	        self.pause()
	    }
	}

executor HelloWorld {
    start fn run(self) -> never {
        self.serial_path.write(self.memory.bytes(value = "hello"))
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
	if got := exec.Methods[0].Return.Name; got != "never" {
		t.Fatalf("start fn return = %q, want never", got)
	}
	expr := exec.Methods[0].Body[0].(*ast.ExprStmt).Expr.(*ast.CallExpr)
	if len(expr.Args) != 1 || expr.Args[0].Name != "" {
		t.Fatalf("positional call args = %#v, want one unnamed arg", expr.Args)
	}
}

func TestParseDriverPathInterruptEvent(t *testing.T) {
	mod, ds := parseModuleForTest(t, `
module test.interrupt_event
data SerialPathInterrupt { byte: U8 }
driver path SerialConsolePath {
    interrupt receiver -> SerialPathInterrupt {
        return SerialPathInterrupt(byte = 0)
    }
}`)
	if len(ds) != 0 {
		t.Fatalf("diagnostics = %#v", ds)
	}
	path := mod.Decls[1].(*ast.DriverPathDecl)
	if len(path.InterruptEvents) != 1 {
		t.Fatalf("events = %d, want 1", len(path.InterruptEvents))
	}
	ev := path.InterruptEvents[0]
	if ev.EventType.Name != "SerialPathInterrupt" || len(ev.Body) != 1 {
		t.Fatalf("event = %#v", ev)
	}
}

func TestInterruptEventRejectedOutsideDriverPath(t *testing.T) {
	cases := []string{
		"class C { interrupt receiver -> Event { return Event() } }",
		"driver D { interrupt receiver -> Event { return Event() } }",
		"executor E { interrupt receiver -> Event { return Event() } }",
	}
	for _, body := range cases {
		_, ds := parseModuleForTest(t, "module test.bad_event\ndata Event {}\n"+body)
		if len(ds) == 0 {
			t.Fatalf("expected parse diagnostic for %s", body)
		}
	}
}

func TestParseExecutorOnHandler(t *testing.T) {
	mod, ds := parseModuleForTest(t, `
module test.on_handler
executor HelloWorld {
    serial_path: SerialConsolePath
    on serial_path.interrupt(event: SerialPathInterrupt) {
        self.serial_path.ack_receive(event = event)
    }
}`)
	if len(ds) != 0 {
		t.Fatalf("diagnostics = %#v", ds)
	}
	exec := mod.Decls[0].(*ast.ExecutorDecl)
	if len(exec.OnHandlers) != 1 {
		t.Fatalf("on handlers = %d, want 1", len(exec.OnHandlers))
	}
	got := exec.OnHandlers[0]
	if got.PathField != "serial_path" || got.ParamName != "event" || got.ParamType.Name != "SerialPathInterrupt" {
		t.Fatalf("on handler = %#v", got)
	}
}

func TestOnHandlerRejectsMissingParamType(t *testing.T) {
	_, ds := parseModuleForTest(t, `
module test.bad_on
executor HelloWorld {
    serial_path: SerialConsolePath
    on serial_path.interrupt(event) {
    }
}`)
	if len(ds) == 0 {
		t.Fatalf("expected parse diagnostic")
	}
}

func TestOnHandlerRejectsNonInterruptSelector(t *testing.T) {
	_, ds := parseModuleForTest(t, `
module test.bad_on_selector
executor HelloWorld {
    serial_path: SerialConsolePath
    on serial_path.receive(event: SerialPathInterrupt) {
    }
}`)
	if len(ds) == 0 {
		t.Fatalf("expected parse diagnostic")
	}
}

func TestOnHandlerRejectedOutsideExecutor(t *testing.T) {
	_, ds := parseModuleForTest(t, `
module test.bad_on_placement
class C {
    on serial_path.interrupt(event: SerialPathInterrupt) {
    }
}`)
	if len(ds) == 0 {
		t.Fatalf("expected parse diagnostic")
	}
}

func TestParseGenericDeclsAndTypes(t *testing.T) {
	mod, ds := parseModuleForTest(t, `
module parser.generics

data FixedBuffer<T> where T: Copyable {
    slots: Slots<T>
    length: U64
}

trait Subscription<T> {
    fn try_next(self) -> Option<T>
}

trait Publisher<T> {
    fn publish(self, value: T)
}

class DrainLoop<S, T> where S: Subscription<T> {
    input: S
    field: Topic<TimerTickPayload>
    fn poll(self, topic: Topic<TimerTickPayload>) -> Topic<TimerTickPayload> {
        return self.input.try_next()
    }
}

impl Publisher<T> for TopicPublisher<T>
`)
	if len(ds) != 0 {
		t.Fatalf("diagnostics: %#v", ds)
	}
	data := mod.Decls[0].(*ast.DataDecl)
	if data.TypeParams[0].Name != "T" || data.Fields[0].Type.String() != "Slots<T>" {
		t.Fatalf("generic data parsed incorrectly: %#v", data)
	}
	trait := mod.Decls[1].(*ast.TraitDecl)
	if trait.Name != "Subscription" || trait.Methods[0].Return.String() != "Option<T>" {
		t.Fatalf("trait parsed incorrectly: %#v", trait)
	}
	class := mod.Decls[3].(*ast.ClassDecl)
	if len(class.Where) != 1 || class.Where[0].Trait.String() != "Subscription<T>" {
		t.Fatalf("where bounds = %#v", class.Where)
	}
	if class.Methods[0].Params[1].Type.String() != "Topic<TimerTickPayload>" {
		t.Fatalf("method parameter type = %#v", class.Methods[0].Params[1].Type)
	}
	if class.Methods[0].Return.String() != "Topic<TimerTickPayload>" {
		t.Fatalf("method return type = %q", class.Methods[0].Return)
	}
	if class.Fields[1].Type.String() != "Topic<TimerTickPayload>" {
		t.Fatalf("class field = %#v", class.Fields[1])
	}
	impl := mod.Decls[4].(*ast.ImplDecl)
	if impl.Trait.String() != "Publisher<T>" || impl.For.String() != "TopicPublisher<T>" {
		t.Fatalf("impl = %#v", impl)
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
