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
