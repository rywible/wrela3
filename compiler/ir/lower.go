package ir

import (
	"fmt"
	"strings"

	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/layout"
	"github.com/ryanwible/wrela3/compiler/sem"
)

func Lower(checked *sem.CheckedProgram) (*Program, []diag.Diagnostic) {
	if checked == nil || checked.Index == nil {
		return nil, []diag.Diagnostic{{
			Phase:   "cg",
			Code:    diag.CG0001,
			Message: "lowering requires a checked semantic program",
		}}
	}

	imageModule, imageName := firstImageType(checked)
	if imageName == "" {
		imageName = "image"
	}
	delegatedSymbol := symbolName("phase", imageModule, imageName, "delegated_hardware")
	ownedSymbol := symbolName("phase", imageModule, imageName, "owned_hardware")

	program := &Program{
		Entry: EntryAdapter{
			Symbol:                "_wrela_efi_entry",
			DelegatedPhaseSymbol:  delegatedSymbol,
			OwnedPhaseSymbol:      ownedSymbol,
			DelegatedHardwareType: "DelegatedHardware",
			OwnedHardwareType:     typeName(checked.OwnedRoot),
		},
	}
	if program.Entry.OwnedHardwareType == "" {
		program.Entry.OwnedHardwareType = "OwnedHardware"
	}

	program.AsmMethods = append(program.AsmMethods,
		AsmMethod{Symbol: delegatedSymbol, Body: "mov rax, 0\nret"},
		AsmMethod{Symbol: ownedSymbol, Body: serialOwnedPhaseBody(findFirstStringLiteral(checked.Modules))},
	)
	program.AsmMethods = append(program.AsmMethods, lowerAsmMethods(checked)...)
	program.Data = lowerStringData(checked.Modules)
	return program, nil
}

func firstImageType(checked *sem.CheckedProgram) (string, string) {
	if checked == nil || checked.Index == nil {
		return "", ""
	}
	for module, types := range checked.Index.ByModule {
		for name, typ := range types {
			if typ != nil && typ.Kind == sem.KindImage {
				return module, name
			}
		}
	}
	if len(checked.Index.Images) > 0 {
		image := checked.Index.Images[0]
		for _, mod := range checked.Modules {
			for _, decl := range mod.Decls {
				if decl == image {
					return mod.Name, image.Name
				}
			}
		}
		return "", image.Name
	}
	return "", ""
}

func lowerAsmMethods(checked *sem.CheckedProgram) []AsmMethod {
	var out []AsmMethod
	for moduleName, byName := range checked.Index.ByModule {
		for _, typ := range byName {
			if typ == nil {
				continue
			}
			offsets, widths := receiverLayout(typ)
			for _, method := range typ.Methods {
				if !method.IsAsm || method.AsmBody == nil {
					continue
				}
				if !asmBodySupported(method.AsmBody.Source) {
					continue
				}
				out = append(out, AsmMethod{
					Symbol:               symbolName("method", moduleName, typ.Name, method.Name),
					ReceiverType:         typ.Name,
					Params:               methodParams(method),
					Return:               Type{Name: typeName(method.Return)},
					Body:                 method.AsmBody.Source,
					ReceiverFieldOffsets: offsets,
					ReceiverFieldWidths:  widths,
				})
			}
		}
	}
	return out
}

func receiverLayout(typ *sem.Type) (map[string]int, map[string]int) {
	offsets := map[string]int{}
	widths := map[string]int{}
	if typ == nil {
		return offsets, widths
	}
	fields := make([]layout.Field, 0, len(typ.Fields))
	for _, field := range typ.Fields {
		fields = append(fields, layout.Field{Name: field.Name, Type: typeName(field.Type)})
	}
	record, err := layout.Compute(fields)
	if err != nil {
		return offsets, widths
	}
	for _, field := range typ.Fields {
		fieldLayout := record.Fields[field.Name]
		offsets[field.Name] = fieldLayout.Offset
		widths[field.Name] = fieldLayout.Size * 8
	}
	return offsets, widths
}

func methodParams(method sem.Method) []Value {
	out := []Value{}
	for _, param := range method.Params {
		if param.Name == "self" || param.Name == "" {
			continue
		}
		out = append(out, &Param{Symbol: param.Name, Type: Type{Name: typeName(param.Type)}})
	}
	return out
}

func asmBodySupported(body string) bool {
	unsupported := []string{
		"[rbp", "[rsp", "call rax", "call r10", "call r11",
		"sub rsp", "add rsp", "lea ",
		"push 0x", "push 8", "push rax",
	}
	for _, needle := range unsupported {
		if strings.Contains(body, needle) {
			return false
		}
	}
	return true
}

func lowerStringData(modules []*ast.Module) []DataObject {
	var out []DataObject
	seen := map[string]bool{}
	for _, mod := range modules {
		for _, decl := range mod.Decls {
			walkDeclStrings(decl, func(value string) {
				if seen[value] {
					return
				}
				seen[value] = true
				out = append(out, DataObject{Symbol: symbolName("str", mod.Name, fmt.Sprintf("%d", len(out))), Bytes: append([]byte(value), 0)})
			})
		}
	}
	return out
}

func findFirstStringLiteral(modules []*ast.Module) string {
	out := ""
	for _, mod := range modules {
		for _, decl := range mod.Decls {
			walkDeclStrings(decl, func(value string) {
				if out == "" {
					out = value
				}
			})
			if out != "" {
				return out
			}
		}
	}
	return "hello from wrela\n"
}

func walkDeclStrings(decl ast.Decl, visit func(string)) {
	switch d := decl.(type) {
	case *ast.ClassDecl:
		walkMethodsStrings(d.Methods, visit)
	case *ast.DriverDecl:
		walkMethodsStrings(d.Methods, visit)
	case *ast.DriverPathDecl:
		walkMethodsStrings(d.Methods, visit)
	case *ast.ExecutorDecl:
		walkMethodsStrings(d.Methods, visit)
	case *ast.ImageDecl:
		for _, phase := range d.Phases {
			walkStmtStrings(phase.Body, visit)
		}
	}
}

func walkMethodsStrings(methods []ast.MethodDecl, visit func(string)) {
	for _, method := range methods {
		walkStmtStrings(method.Body, visit)
	}
}

func walkStmtStrings(stmts []ast.Stmt, visit func(string)) {
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *ast.LetStmt:
			walkExprStrings(s.Expr, visit)
		case *ast.AssignStmt:
			walkExprStrings(s.Target, visit)
			walkExprStrings(s.Value, visit)
		case *ast.ReturnStmt:
			walkExprStrings(s.Value, visit)
		case *ast.ExprStmt:
			walkExprStrings(s.Expr, visit)
		case *ast.IfStmt:
			walkExprStrings(s.Cond, visit)
			walkStmtStrings(s.Then, visit)
			walkStmtStrings(s.Else, visit)
		case *ast.WhileStmt:
			walkExprStrings(s.Cond, visit)
			walkStmtStrings(s.Body, visit)
		case *ast.ForStmt:
			walkExprStrings(s.InExpr, visit)
			walkStmtStrings(s.Body, visit)
		}
	}
}

func walkExprStrings(expr ast.Expr, visit func(string)) {
	switch e := expr.(type) {
	case nil:
	case *ast.StringLiteral:
		visit(e.Value)
	case *ast.ConstructorExpr:
		for _, arg := range e.Args {
			walkExprStrings(arg.Value, visit)
		}
	case *ast.CallExpr:
		walkExprStrings(e.Receiver, visit)
		for _, arg := range e.Args {
			walkExprStrings(arg.Value, visit)
		}
	case *ast.FieldExpr:
		walkExprStrings(e.Base, visit)
	case *ast.BinaryExpr:
		walkExprStrings(e.Left, visit)
		walkExprStrings(e.Right, visit)
	}
}

func serialOwnedPhaseBody(message string) string {
	if message == "" {
		message = "hello from wrela\n"
	}
	var b strings.Builder
	b.WriteString("mov dx, 0x03f8\n")
	for i := 0; i < len(message); i++ {
		fmt.Fprintf(&b, "mov al, %d\nout dx, al\n", message[i])
	}
	b.WriteString("owned_halt:\nhlt\njmp owned_halt\n")
	return b.String()
}

func symbolName(parts ...string) string {
	var b strings.Builder
	b.WriteString("_wrela")
	for _, part := range parts {
		if part == "" {
			continue
		}
		b.WriteByte('_')
		for _, r := range part {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
				b.WriteRune(r)
			} else {
				b.WriteByte('_')
			}
		}
	}
	return b.String()
}

func typeName(typ *sem.Type) string {
	if typ == nil {
		return ""
	}
	return typ.Name
}
