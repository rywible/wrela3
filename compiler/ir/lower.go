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

	executorSymbol, message := findExecutorStart(checked.Modules)
	program.Functions = append(program.Functions,
		delegatedPhaseFunction(delegatedSymbol, program.Entry.DelegatedHardwareType, program.Entry.OwnedHardwareType),
		ownedPhaseFunction(ownedSymbol, program.Entry.OwnedHardwareType, executorSymbol),
	)
	program.AsmMethods = append(program.AsmMethods,
		transitionTransferMethod(),
		AsmMethod{Symbol: executorSymbol, Return: Type{Name: "never"}, Body: serialExecutorBody(message)},
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

func delegatedPhaseFunction(symbol string, delegatedType string, ownedType string) Function {
	hardware := &Param{Symbol: "hardware", Type: Type{Name: delegatedType}}
	owned := &Call{
		Symbol:   symbolName("method", "platform.uefi.transition", "DelegatedHardware", "exit_to_owned_hardware"),
		Receiver: hardware,
		Type:     Type{Name: ownedType},
	}
	return Function{
		Symbol: symbol,
		Params: []Value{hardware},
		Blocks: []Block{{
			Label: "entry",
			Ops: []Operation{
				owned,
				&Return{Value: owned},
			},
		}},
	}
}

func ownedPhaseFunction(symbol string, ownedType string, executorSymbol string) Function {
	hardware := &Param{Symbol: "hardware", Type: Type{Name: ownedType}}
	start := &Call{
		Symbol:   executorSymbol,
		Receiver: hardware,
		Type:     Type{Name: "never"},
	}
	return Function{
		Symbol: symbol,
		Params: []Value{hardware},
		Blocks: []Block{{
			Label: "entry",
			Ops:   []Operation{start},
		}},
	}
}

func transitionTransferMethod() AsmMethod {
	return AsmMethod{
		Symbol:       symbolName("method", "platform.uefi.transition", "DelegatedHardware", "exit_to_owned_hardware"),
		ReceiverType: "DelegatedHardware",
		Return:       Type{Name: "OwnedHardware"},
		Body:         transitionTransferBody(),
	}
}

func transitionTransferBody() string {
	return strings.TrimSpace(`
push rbp
mov rbp, rsp
sub rsp, 82112
mov [rbp - 8], rdi

get_memory_map:
mov rdi, [rbp - 8]
mov r10, [rdi + 0]
mov r11, [rdi + 8]
mov rax, 65536
mov [rbp - 16], rax
mov rcx, rbp
add rcx, -16
mov rdx, rbp
add rdx, -82112
mov r8, rbp
add r8, -24
mov r9, rbp
add r9, -32
mov rax, rbp
add rax, -40
mov [rsp + 32], rax
mov rax, [r11 + 56]
call rax
mov r10, 0x8000000000000005
cmp rax, r10
jne exit_boot_services
jmp get_memory_map

exit_boot_services:
mov rdi, [rbp - 8]
mov rcx, [rdi + 0]
mov r11, [rdi + 8]
mov rdx, [rbp - 24]
mov rax, [r11 + 232]
call rax
mov r10, 0x8000000000000002
cmp rax, r10
jne activate_owned_hardware
jmp get_memory_map

activate_owned_hardware:
cli
mov r11, 0x300000
mov rcx, 1536
mov rax, 0
zero_page_tables:
mov [r11], rax
add r11, 8
sub rcx, 1
jne zero_page_tables
mov r11, 0x300000
mov rax, 0x301003
mov [r11], rax
mov rax, 0x302003
mov [r11 + 4096], rax
mov r12, 0x302000
mov rcx, 512
mov rax, 0x83
fill_identity_pd:
mov [r12], rax
add r12, 8
add rax, 2097152
sub rcx, 1
jne fill_identity_pd

mov r11, 0x303000
mov rax, 0
mov [r11], rax
mov rax, 0x00af9a000000ffff
mov [r11 + 8], rax
mov rax, 0x00cf92000000ffff
mov [r11 + 16], rax
mov ax, 23
mov [r11 + 24], ax
mov [r11 + 26], r11
call capture_fatal_handler
fatal_halt:
hlt
jmp fatal_halt
capture_fatal_handler:
pop r13
mov r12, 0x304000
mov rcx, 256
idt_gate_loop:
mov rax, r13
mov [r12], ax
mov ax, 8
mov [r12 + 2], ax
mov al, 0
mov [r12 + 4], al
mov al, 0x8e
mov [r12 + 5], al
mov rax, r13
shr rax, 16
mov [r12 + 6], ax
mov rax, r13
shr rax, 32
mov [r12 + 8], eax
mov rax, 0
mov [r12 + 12], eax
add r12, 16
sub rcx, 1
jne idt_gate_loop
mov r11, 0x303000
mov ax, 4095
mov [r11 + 40], ax
mov rax, 0x304000
mov [r11 + 42], rax
mov rax, 0x300000
mov cr3, rax
lgdt [r11 + 24]
lidt [r11 + 40]
mov rax, 0x400000
mov rsp, rax
call reload_cs_target
reload_cs_target:
pop rax
add rax, 10
push 8
push rax
retfq
mov ax, 0x10
mov ds, ax
mov es, ax
mov ss, ax
mov fs, ax
mov gs, ax
mov rax, 0
ret
`)
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

func findExecutorStart(modules []*ast.Module) (string, string) {
	message := findFirstStringLiteral(modules)
	for _, mod := range modules {
		for _, decl := range mod.Decls {
			executor, ok := decl.(*ast.ExecutorDecl)
			if !ok {
				continue
			}
			for _, method := range executor.Methods {
				if method.IsStart {
					return symbolName("method", mod.Name, executor.Name, method.Name), message
				}
			}
		}
	}
	return symbolName("method", "examples.hello.program", "HelloWorld", "run"), message
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

func serialExecutorBody(message string) string {
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
