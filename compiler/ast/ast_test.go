package ast

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/source"
)

func TestASTContractAssertions(t *testing.T) {
	var _ Decl = (*ImageDecl)(nil)
	var _ Decl = (*DriverPathDecl)(nil)
	var _ Stmt = (*ForStmt)(nil)
	var _ Stmt = (*WithStmt)(nil)
	var _ Expr = (*CallExpr)(nil)
}

func TestStorageASTContracts(t *testing.T) {
	var _ Decl = (*EventDecl)(nil)
	var _ Decl = (*ProjectionDecl)(nil)

	encode := &FieldExpr{
		Base:  &NameExpr{Name: "self"},
		Field: "file_id",
	}
	event := &EventDecl{
		Name: "FileRenamed",
		ID:   "17",
		Fields: []Field{{
			Name: "file_id",
			Type: TypeRef{Name: "FileId"},
		}},
		Layouts: []EventLayoutDecl{{
			ID:      "2",
			Current: true,
			Fields: []EventLayoutField{{
				Name:   "file_id",
				Type:   TypeRef{Name: "U64"},
				Encode: encode,
			}},
		}},
		Upcasts: []LayoutUpcastDecl{{
			FromID: "1",
			ToID:   "2",
			Mappings: []LayoutUpcastMapping{{
				From: "old_name_ref",
				To:   "name_ref",
			}},
		}},
	}
	projection := &ProjectionDecl{
		Name: "DirectoryChildren",
		ID:   "12",
		Layouts: []ProjectionLayoutDecl{{
			ID:      "1",
			Current: true,
			Fields: []Field{{
				Name: "children",
				Type: TypeRef{
					Name: "OrderedPages",
					Args: []TypeRef{{Name: "FileId"}, {Name: "FileNameKey"}, {Name: "DirectoryChild"}},
				},
			}},
		}},
	}

	if got, want := DebugDecl(event), "event FileRenamed id 17"; got != want {
		t.Fatalf("DebugDecl(event) = %q, want %q", got, want)
	}
	if got, want := DebugDecl(projection), "projection DirectoryChildren id 12"; got != want {
		t.Fatalf("DebugDecl(projection) = %q, want %q", got, want)
	}
	if got := event.Layouts[0].Fields[0].Type.String(); got != "U64" {
		t.Fatalf("event layout field type = %q", got)
	}
	if event.Layouts[0].Fields[0].Encode != encode {
		t.Fatalf("event layout encode expression was not preserved")
	}
	if got := event.Upcasts[0].Mappings[0]; got.From != "old_name_ref" || got.To != "name_ref" {
		t.Fatalf("upcast mapping = %#v", got)
	}
	if got := projection.Layouts[0].Fields[0].Type.String(); got != "OrderedPages<FileId, FileNameKey, DirectoryChild>" {
		t.Fatalf("projection field type = %q", got)
	}
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
	_ = DebugExpr(&ConstructorExpr{Type: TypeRef{Name: "Bytes"}, SpanV: source.Span{}})
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

func TestDebugWithStmt(t *testing.T) {
	stmt := &WithStmt{
		Expr: &CallExpr{
			Receiver: &NameExpr{Name: "memory"},
			Method:   "frame",
			Args: []NamedArg{{
				Name:  "length",
				Value: &IntLiteral{Value: "64"},
			}},
		},
		Name: "tick",
		Body: []Stmt{&LetStmt{
			Name: "x",
			Expr: &NameExpr{Name: "tick"},
		}},
	}
	got := DebugStmt(stmt)
	want := "with memory.frame(length = 64) as tick { let x = tick }"
	if got != want {
		t.Fatalf("DebugStmt = %q, want %q", got, want)
	}
}

func TestInterruptEventASTContracts(t *testing.T) {
	path := &DriverPathDecl{
		Name: "SerialConsolePath",
		InterruptEvents: []InterruptEventDecl{
			{EventType: TypeRef{Name: "Option", Args: []TypeRef{{Name: "U8"}}}},
		},
	}
	exec := &ExecutorDecl{
		Name: "HelloWorld",
		OnHandlers: []OnHandlerDecl{
			{PathField: "serial_path", ParamName: "event", ParamType: TypeRef{Name: "Option", Args: []TypeRef{{Name: "U8"}}}},
		},
	}
	if path.InterruptEvents[0].EventType.String() != "Option<U8>" {
		t.Fatalf("interrupt event not stored")
	}
	if exec.OnHandlers[0].PathField != "serial_path" || exec.OnHandlers[0].ParamType.String() != "Option<U8>" {
		t.Fatalf("on handler not stored")
	}
}

func TestTypeRefString(t *testing.T) {
	ref := TypeRef{Name: "Result", Args: []TypeRef{{Name: "Unit"}, {Name: "BufferFull"}}}
	if got, want := ref.String(), "Result<Unit, BufferFull>"; got != want {
		t.Fatalf("TypeRef.String() = %q, want %q", got, want)
	}
}

func TestDebugMatchStmt(t *testing.T) {
	stmt := &MatchStmt{
		Value: &CallExpr{
			Receiver: &NameExpr{Name: "rx"},
			Method:   "try_next",
		},
		Arms: []MatchArm{
			{
				Pattern: VariantPattern{
					Enum:     "Option",
					Variant:  "Some",
					Bindings: []PatternBinding{{Name: "value", Bind: "event"}},
				},
				Body: []Stmt{
					&ExprStmt{
						Expr: &CallExpr{
							Receiver: &NameExpr{Name: "events"},
							Method:   "push",
							Args: []NamedArg{
								{
									Name:  "value",
									Value: &NameExpr{Name: "event"},
								},
							},
						},
					},
				},
			},
			{
				Pattern: VariantPattern{Enum: "Option", Variant: "None"},
				Body: []Stmt{
					&ExprStmt{
						Expr: &CallExpr{
							Receiver: &NameExpr{Name: "rx"},
							Method:   "arm_wait",
						},
					},
				},
			},
		},
	}
	if got, want := DebugStmt(stmt), "match rx.try_next() { Option.Some(value = event) => { events.push(value = event) } Option.None => { rx.arm_wait() } }"; got != want {
		t.Fatalf("DebugStmt(match) = %q, want %q", got, want)
	}
}
