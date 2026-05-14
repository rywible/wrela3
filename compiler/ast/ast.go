package ast

import "github.com/ryanwible/wrela3/compiler/source"

type Module struct {
	Name    string
	Imports []Import
	Decls   []Decl
	Span    source.Span
}

type Import struct {
	Names []string
	Path  string
	Span  source.Span
}

type Decl interface {
	Span() source.Span
}

type DataDecl struct {
	Name   string
	Fields []Field
	SpanV  source.Span
}

func (d *DataDecl) Span() source.Span { return d.SpanV }

type ClassDecl struct {
	Name    string
	Fields  []Field
	Methods []MethodDecl
	Unique  bool
	SpanV   source.Span
}

func (d *ClassDecl) Span() source.Span { return d.SpanV }

type DriverDecl struct {
	Name    string
	Fields  []Field
	Methods []MethodDecl
	Unique  bool
	SpanV   source.Span
}

func (d *DriverDecl) Span() source.Span { return d.SpanV }

type DriverPathDecl struct {
	Name    string
	Fields  []Field
	Methods []MethodDecl
	SpanV   source.Span
}

func (d *DriverPathDecl) Span() source.Span { return d.SpanV }

type ExecutorDecl struct {
	Name    string
	Fields  []Field
	Methods []MethodDecl
	SpanV   source.Span
}

func (d *ExecutorDecl) Span() source.Span { return d.SpanV }

type ImageDecl struct {
	Name        string
	Transitions []Transition
	Phases      []PhaseDecl
	SpanV       source.Span
}

func (d *ImageDecl) Span() source.Span { return d.SpanV }

type Transition struct {
	From string
	To   string
	Span source.Span
}

type PhaseDecl struct {
	Name   string
	Params []Param
	Return string
	Body   []Stmt
	SpanV  source.Span
	Parent *ImageDecl
}

func (d *PhaseDecl) Span() source.Span { return d.SpanV }

type Field struct {
	Name string
	Type string
	Span source.Span
}

type Param struct {
	Name string
	Type string
	Span source.Span
}

type MethodDecl struct {
	Receiver string
	Name     string
	Params   []Param
	Return   string
	Body     []Stmt
	Asm      *AsmBody
	IsAsm    bool
	IsStart  bool
	SpanV    source.Span
}

func (d *MethodDecl) Span() source.Span { return d.SpanV }

type AsmBody struct {
	Source string
	Span   source.Span
}

type Stmt interface {
	Span() source.Span
}

type LetStmt struct {
	Name  string
	Expr  Expr
	SpanV source.Span
}

func (s *LetStmt) Span() source.Span { return s.SpanV }

type ReturnStmt struct {
	Value Expr
	SpanV source.Span
}

func (s *ReturnStmt) Span() source.Span { return s.SpanV }

type IfStmt struct {
	Cond  Expr
	Then  []Stmt
	Else  []Stmt
	SpanV source.Span
}

func (s *IfStmt) Span() source.Span { return s.SpanV }

type WhileStmt struct {
	Cond  Expr
	Body  []Stmt
	SpanV source.Span
}

func (s *WhileStmt) Span() source.Span { return s.SpanV }

type ForStmt struct {
	Var    string
	InExpr Expr
	Body   []Stmt
	SpanV  source.Span
}

func (s *ForStmt) Span() source.Span { return s.SpanV }

type AssignStmt struct {
	Target Expr
	Value  Expr
	SpanV  source.Span
}

func (s *AssignStmt) Span() source.Span { return s.SpanV }

type ExprStmt struct {
	Expr  Expr
	SpanV source.Span
}

func (s *ExprStmt) Span() source.Span { return s.SpanV }

type Expr interface {
	Span() source.Span
}

type NameExpr struct {
	Name  string
	SpanV source.Span
}

func (e *NameExpr) Span() source.Span { return e.SpanV }

type IntLiteral struct {
	Value string
	SpanV source.Span
}

func (e *IntLiteral) Span() source.Span { return e.SpanV }

type StringLiteral struct {
	Value string
	SpanV source.Span
}

func (e *StringLiteral) Span() source.Span { return e.SpanV }

type BoolLiteral struct {
	Value bool
	SpanV source.Span
}

func (e *BoolLiteral) Span() source.Span { return e.SpanV }

type ConstructorExpr struct {
	Type  string
	Args  []NamedArg
	SpanV source.Span
}

func (e *ConstructorExpr) Span() source.Span { return e.SpanV }

type CallExpr struct {
	Receiver Expr
	Method   string
	Args     []NamedArg
	SpanV    source.Span
}

func (e *CallExpr) Span() source.Span { return e.SpanV }

type FieldExpr struct {
	Base  Expr
	Field string
	SpanV source.Span
}

func (e *FieldExpr) Span() source.Span { return e.SpanV }

type BinaryExpr struct {
	Op    string
	Left  Expr
	Right Expr
	SpanV source.Span
}

func (e *BinaryExpr) Span() source.Span { return e.SpanV }

type NamedArg struct {
	Name  string
	Value Expr
	SpanV source.Span
}

func DebugExpr(expr Expr) string {
	switch e := expr.(type) {
	case *NameExpr:
		return e.Name
	case *IntLiteral:
		return e.Value
	case *StringLiteral:
		return "\"" + e.Value + "\""
	case *BoolLiteral:
		if e.Value {
			return "true"
		}
		return "false"
	case *FieldExpr:
		return ".(" + DebugExpr(e.Base) + " " + e.Field + ")"
	case *ConstructorExpr:
		return "(" + e.Type + " " + debugNamedArgs(e.Args) + ")"
	case *CallExpr:
		return "(" + DebugExpr(e.Receiver) + "." + e.Method + " " + debugNamedArgs(e.Args) + ")"
	case *BinaryExpr:
		return "(" + e.Op + " " + DebugExpr(e.Left) + " " + DebugExpr(e.Right) + ")"
	default:
		return "<expr>"
	}
}

func debugNamedArgs(args []NamedArg) string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		out = append(out, arg.Name+": "+DebugExpr(arg.Value))
	}
	return stringsJoin(out, " ")
}

func stringsJoin(vals []string, sep string) string {
	if len(vals) == 0 {
		return ""
	}
	acc := vals[0]
	for i := 1; i < len(vals); i++ {
		acc += sep + vals[i]
	}
	return acc
}
