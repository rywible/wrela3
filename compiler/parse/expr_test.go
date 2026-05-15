package parse

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/ast"
)

func TestBinaryPrecedence(t *testing.T) {
	p := newParser("test", "a + b * c")
	expr, ds := p.parseExpr(0)
	if len(ds) != 0 {
		t.Fatalf("diagnostics = %#v", ds)
	}
	got := ast.DebugExpr(expr)
	want := "(+ a (* b c))"
	if got != want {
		t.Fatalf("expr = %q, want %q", got, want)
	}
}

func TestSlashAndPercentUseMultiplyPrecedence(t *testing.T) {
	p := newParser("test", "a + b / c % d")
	expr, ds := p.parseExpr(0)
	if len(ds) != 0 {
		t.Fatalf("diagnostics = %#v", ds)
	}
	got := ast.DebugExpr(expr)
	want := "(+ a (% (/ b c) d))"
	if got != want {
		t.Fatalf("expr = %q, want %q", got, want)
	}
}

func TestParseNamedArgsUseEquals(t *testing.T) {
	p := newParser("test", "Device(x = 1)")
	expr, ds := p.parseExpr(0)
	if len(ds) != 0 {
		t.Fatalf("diagnostics = %#v", ds)
	}
	con, ok := expr.(*ast.ConstructorExpr)
	if !ok {
		t.Fatalf("expr = %#v, want *ast.ConstructorExpr", expr)
	}
	if con.Type != "Device" || len(con.Args) != 1 || con.Args[0].Name != "x" {
		t.Fatalf("constructor = %#v", con)
	}

	p = newParser("test", "host.run(payload = Bytes)")
	expr, ds = p.parseExpr(0)
	if len(ds) != 0 {
		t.Fatalf("diagnostics = %#v", ds)
	}
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		t.Fatalf("expr = %#v, want *ast.CallExpr", expr)
	}
	if call.Method != "run" || len(call.Args) != 1 {
		t.Fatalf("call = %#v", call)
	}
}

func TestParseNamedArgsRejectColon(t *testing.T) {
	for _, src := range []string{"Device(x: 1)", "host.run(payload: Bytes)"} {
		p := newParser("test", src)
		_, ds := p.parseExpr(0)
		if len(ds) == 0 {
			t.Fatalf("expected diagnostic for %s", src)
		}
	}
}

func TestFieldExprParsing(t *testing.T) {
	p := newParser("test", "obj.field")
	expr, ds := p.parseExpr(0)
	if len(ds) != 0 {
		t.Fatalf("diagnostics = %#v", ds)
	}
	if _, ok := expr.(*ast.FieldExpr); !ok {
		t.Fatalf("expr = %#v, want *ast.FieldExpr", expr)
	}
}

func TestMethodChainMayContinueAfterNewline(t *testing.T) {
	p := newParser("test", "self\n.write()")
	expr, ds := p.parseExpr(0)
	if len(ds) != 0 {
		t.Fatalf("diagnostics = %#v", ds)
	}
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		t.Fatalf("expr = %#v, want *ast.CallExpr", expr)
	}
	if call.Method != "write" {
		t.Fatalf("method = %q, want write", call.Method)
	}
}
