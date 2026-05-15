package ast

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/source"
)

func TestASTContractAssertions(t *testing.T) {
	var _ Decl = (*ImageDecl)(nil)
	var _ Decl = (*DriverPathDecl)(nil)
	var _ Stmt = (*ForStmt)(nil)
	var _ Expr = (*CallExpr)(nil)
}

func TestDebugExprBinary(t *testing.T) {
	e := &BinaryExpr{
		Op: "+",
		Left: &NameExpr{
			Name:  "a",
			SpanV: source.Span{Start: 0, End: 1},
		},
		Right: &BinaryExpr{
			Op: "*",
			Left: &NameExpr{
				Name:  "b",
				SpanV: source.Span{Start: 4, End: 5},
			},
			Right: &NameExpr{
				Name:  "c",
				SpanV: source.Span{Start: 8, End: 9},
			},
			SpanV: source.Span{Start: 4, End: 9},
		},
		SpanV: source.Span{Start: 0, End: 9},
	}
	if got, want := DebugExpr(e), "(+ a (* b c))"; got != want {
		t.Fatalf("DebugExpr(e) = %q, want %q", got, want)
	}
}

func TestDebugExprConstructorAndCall(t *testing.T) {
	_ = DebugExpr(&ConstructorExpr{Type: "Bytes", SpanV: source.Span{}})
	_ = DebugExpr(&CallExpr{Receiver: &NameExpr{SpanV: source.Span{}}, SpanV: source.Span{}})
	for _, v := range []Expr{
		&IntLiteral{Value: "1", SpanV: source.Span{}},
		&StringLiteral{Value: "x", SpanV: source.Span{}},
		&BoolLiteral{Value: true, SpanV: source.Span{}},
		&FieldExpr{Base: &NameExpr{SpanV: source.Span{}}, Field: "x", SpanV: source.Span{}},
	} {
		got := DebugExpr(v)
		if got == "<expr>" {
			t.Fatalf("DebugExpr(%T) should not be placeholder", v)
		}
	}
}

func TestDebugExprNamedArgsUseEquals(t *testing.T) {
	expr := &CallExpr{
		Receiver: &NameExpr{Name: "host"},
		Method:   "run",
		Args: []NamedArg{{
			Name:  "payload",
			Value: &NameExpr{Name: "Bytes"},
		}},
	}
	if got, want := DebugExpr(expr), "host.run(payload = Bytes)"; got != want {
		t.Fatalf("DebugExpr = %q, want %q", got, want)
	}
}

func TestInterruptEventASTContracts(t *testing.T) {
	path := &DriverPathDecl{
		Name: "SerialConsolePath",
		InterruptEvents: []InterruptEventDecl{
			{EventType: "SerialPathInterrupt"},
		},
	}
	exec := &ExecutorDecl{
		Name: "HelloWorld",
		OnHandlers: []OnHandlerDecl{
			{PathField: "serial_path", ParamName: "event", ParamType: "SerialPathInterrupt"},
		},
	}
	if path.InterruptEvents[0].EventType != "SerialPathInterrupt" {
		t.Fatalf("interrupt event not stored")
	}
	if exec.OnHandlers[0].PathField != "serial_path" || exec.OnHandlers[0].ParamType != "SerialPathInterrupt" {
		t.Fatalf("on handler not stored")
	}
}
