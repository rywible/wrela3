package sem

import (
	"fmt"
	"strings"

	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/source"
)

type ContextKind int

const (
	ContextNormalMethod ContextKind = iota
	ContextImagePhaseDirect
	ContextOwnershipTransferAuthorityMethod
)

type Scope struct {
	parent *Scope
	types  map[string]*Type
}

func NewScope(parent *Scope) *Scope {
	return &Scope{parent: parent, types: map[string]*Type{}}
}

func (s *Scope) Define(name string, typ *Type) {
	if s == nil {
		return
	}
	s.types[name] = typ
}

func (s *Scope) Lookup(name string) (*Type, bool) {
	if s == nil {
		return nil, false
	}
	if typ := s.types[name]; typ != nil {
		return typ, true
	}
	if s.parent != nil {
		return s.parent.Lookup(name)
	}
	return nil, false
}

type checker struct {
	index        *Index
	modules      []*ast.Module
	currentType  *Type
	currentPhase string
	diags        []diag.Diagnostic
	ownedRoot    *Type
	graph        ImageGraph
}

func Check(index *Index, modules []*ast.Module) (*CheckedProgram, []diag.Diagnostic) {
	c := &checker{
		index:        index,
		modules:      modules,
		currentPhase: "",
	}
	if c.index == nil {
		return &CheckedProgram{
				Modules: modules,
				Index:   index,
			}, []diag.Diagnostic{{
				Phase:    "sem",
				Code:     diag.SEM0005,
				Severity: diag.Error,
				Message:  "missing index",
			}}
	}

	c.checkImageSignatures()
	c.checkDeclBodiesAndConstructors()
	c.checkDelegatedOnlyCrossing()
	c.checkUniqueConstructors()
	c.checkExecutorWiring()

	return &CheckedProgram{
		Modules:    modules,
		Index:      index,
		ImageGraph: c.graph,
		OwnedRoot:  c.ownedRoot,
	}, c.diags
}

func (c *checker) phaseByName(img *ast.ImageDecl, name string) *ast.PhaseDecl {
	for i := range img.Phases {
		if img.Phases[i].Name == name {
			return &img.Phases[i]
		}
	}
	return nil
}

func (c *checker) checkImageSignatures() {
	for _, image := range c.index.Images {
		imageModule := c.lookupImageModule(image)
		if imageModule == "" {
			imageModule = image.Name
		}

		foundTransition := false
		for _, transition := range image.Transitions {
			if transition.From == "delegated_hardware" && transition.To == "owned_hardware" {
				foundTransition = true
				break
			}
		}
		if !foundTransition {
			c.error(image.SpanV, diag.SEM0005, "exact transition delegated_hardware -> owned_hardware is required")
			continue
		}

		delegated := c.phaseByName(image, "delegated_hardware")
		owned := c.phaseByName(image, "owned_hardware")
		if delegated == nil || owned == nil {
			c.error(image.SpanV, diag.SEM0005, "image must define delegated_hardware and owned_hardware phases")
			continue
		}

		if len(delegated.Params) > 5 {
			c.error(delegated.SpanV, diag.SEM0013, "too many explicit parameters")
		}
		if len(owned.Params) > 5 {
			c.error(owned.SpanV, diag.SEM0013, "too many explicit parameters")
		}

		if len(delegated.Params) != 1 {
			c.error(delegated.SpanV, diag.SEM0005, "delegated_hardware phase must have one parameter")
			continue
		}
		if delegated.Params[0].Type != "DelegatedHardware" {
			c.error(delegated.Params[0].Span, diag.SEM0005, "delegated_hardware phase must accept DelegatedHardware")
		}

		ownedParam := c.mustType(imageModule, delegated.Return)
		if ownedParam == nil {
			c.error(delegated.SpanV, diag.SEM0005, "unknown delegated_hardware return type")
			continue
		}
		c.ownedRoot = ownedParam

		if ownedParam != c.resolveType(imageModule, owned.Params[0].Type) {
			c.error(owned.Params[0].Span, diag.SEM0005, "owned_hardware phase must receive the same type returned by delegated_hardware")
		}
		if owned.Return != "never" {
			c.error(owned.SpanV, diag.SEM0005, "owned_hardware phase must return never")
		}
	}
}

func (c *checker) checkDeclBodiesAndConstructors() {
	for _, mod := range c.modules {
		for _, decl := range mod.Decls {
			switch d := decl.(type) {
			case *ast.ImageDecl:
				c.checkImageDecl(mod.Name, d)
			case *ast.ClassDecl:
				typ := c.index.resolveInScope(mod.Name, d.Name)
				c.checkMethods(mod.Name, typ, d.Methods)
			case *ast.DriverDecl:
				typ := c.index.resolveInScope(mod.Name, d.Name)
				c.checkMethods(mod.Name, typ, d.Methods)
			case *ast.DriverPathDecl:
				typ := c.index.resolveInScope(mod.Name, d.Name)
				c.checkMethods(mod.Name, typ, d.Methods)
			case *ast.ExecutorDecl:
				typ := c.index.resolveInScope(mod.Name, d.Name)
				c.checkMethods(mod.Name, typ, d.Methods)
			}
		}
	}
}

func (c *checker) checkImageDecl(moduleName string, image *ast.ImageDecl) {
	for _, phase := range image.Phases {
		if len(phase.Params) > 5 {
			c.error(phase.SpanV, diag.SEM0013, "too many explicit parameters")
		}

		retType := c.mustType(moduleName, phase.Return)
		scope := NewScope(nil)
		for _, p := range phase.Params {
			if p.Name == "" {
				continue
			}
			scope.Define(p.Name, c.mustType(moduleName, p.Type))
		}
		prevPhase := c.currentPhase
		c.currentPhase = phase.Name
		c.checkStmtList(moduleName, phase.Body, scope, retType, ContextImagePhaseDirect)
		c.currentPhase = prevPhase
	}
}

func (c *checker) checkMethods(moduleName string, typ *Type, methods []ast.MethodDecl) {
	c.currentType = typ
	for _, method := range methods {
		c.currentType = typ
		if len(method.Params) > 5 {
			c.error(method.SpanV, diag.SEM0013, "too many explicit parameters")
		}
		if method.IsAsm && !c.isAsmAllowedHere(typ) {
			c.error(method.SpanV, diag.SEM0012, "asm methods are only allowed on edge-capability declarations")
		}

		marker := ContextNormalMethod
		returnType := c.mustType(moduleName, method.Return)
		if c.isOwnershipTransferAuthority(typ) && returnType == c.ownedRoot {
			marker = ContextOwnershipTransferAuthorityMethod
		}
		if method.IsAsm {
			continue
		}

		scope := NewScope(nil)
		if len(method.Params) > 0 && method.Params[0].Name == "self" {
			scope.Define("self", typ)
		}
		for _, p := range method.Params {
			if p.Name == "self" {
				continue
			}
			scope.Define(p.Name, c.mustType(moduleName, p.Type))
		}
		prevPhase := c.currentPhase
		c.currentPhase = method.Name
		c.checkStmtList(moduleName, method.Body, scope, returnType, marker)
		c.currentPhase = prevPhase
	}
	c.currentType = nil
}

func (c *checker) isAsmAllowedHere(typ *Type) bool {
	if typ == nil {
		return false
	}

	switch typ.Kind {
	case KindDriver, KindDriverPath:
		return true
	case KindClass:
		if typ.Name == "ExecutorMemory" {
			return true
		}
		if strings.HasPrefix(typ.Module, "arch.") ||
			strings.HasPrefix(typ.Module, "platform.") ||
			typ.Module == "machine.x86_64.serial" ||
			strings.HasPrefix(typ.Module, "machine.x86_64.serial.") {
			return true
		}
		return false
	default:
		return false
	}
}

func (c *checker) checkStmtList(moduleName string, stmts []ast.Stmt, scope *Scope, expectedReturn *Type, ctx ContextKind) {
	for _, stmt := range stmts {
		c.checkStmt(moduleName, stmt, scope, expectedReturn, ctx)
	}
}

func (c *checker) checkStmt(moduleName string, stmt ast.Stmt, scope *Scope, expectedReturn *Type, ctx ContextKind) {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		valueType := c.typeExpr(moduleName, s.Expr, scope, ctx)
		scope.Define(s.Name, valueType)
		c.checkOwnedDelegatedCrossing(s.Expr.Span(), valueType)
	case *ast.AssignStmt:
		targetType := c.typeExpr(moduleName, s.Target, scope, ctx)
		valueType := c.typeExpr(moduleName, s.Value, scope, ctx)
		c.checkTypeAssign(s.Target.Span(), targetType, valueType)
		c.checkOwnedDelegatedCrossing(s.Value.Span(), valueType)
	case *ast.IfStmt:
		cond := c.typeExpr(moduleName, s.Cond, scope, ctx)
		c.requireType(cond, c.mustType(moduleName, "Bool"), s.Cond.Span())
		c.checkStmtList(moduleName, s.Then, NewScope(scope), expectedReturn, ctx)
		if s.Else != nil {
			c.checkStmtList(moduleName, s.Else, NewScope(scope), expectedReturn, ctx)
		}
	case *ast.WhileStmt:
		cond := c.typeExpr(moduleName, s.Cond, scope, ctx)
		c.requireType(cond, c.mustType(moduleName, "Bool"), s.Cond.Span())
		c.checkStmtList(moduleName, s.Body, NewScope(scope), expectedReturn, ctx)
	case *ast.ForStmt:
		inType := c.typeExpr(moduleName, s.InExpr, scope, ctx)
		c.requireBytesIterable(inType, s.InExpr.Span())
		loopScope := NewScope(scope)
		loopScope.Define(s.Var, c.mustType(moduleName, "U8"))
		c.checkStmtList(moduleName, s.Body, loopScope, expectedReturn, ctx)
	case *ast.ReturnStmt:
		if expectedReturn == nil {
			if s.Value != nil {
				c.error(s.SpanV, diag.CG0001, "cannot return value from void function")
				return
			}
			return
		}
		if expectedReturn == c.mustType(moduleName, "never") {
			if s.Value != nil {
				c.error(s.Value.Span(), diag.CG0001, "never function cannot return a value")
			}
			return
		}
		if s.Value == nil {
			c.error(s.SpanV, diag.CG0001, "missing return value")
			return
		}
		got := c.typeExpr(moduleName, s.Value, scope, ctx)
		c.requireType(got, expectedReturn, s.Value.Span())
		c.checkOwnedDelegatedCrossing(s.Value.Span(), got)
	case *ast.ExprStmt:
		c.typeExpr(moduleName, s.Expr, scope, ctx)
	default:
		c.error(s.Span(), diag.CG0001, "unsupported statement")
	}
}

func (c *checker) checkDelegatedOnlyCrossing() {
	for _, image := range c.index.Images {
		for _, phase := range image.Phases {
			if phase.Name != "owned_hardware" {
				continue
			}
			if len(phase.Params) != 1 {
				continue
			}
			pt := c.resolveType(c.lookupImageModule(image), phase.Params[0].Type)
			if pt == nil {
				continue
			}
			if c.index.IsDelegatedOnly(pt, map[string]bool{}) {
				c.error(phase.Params[0].Span, diag.SEM0009, fmt.Sprintf("delegated-only value %s cannot cross into owned_hardware phase", c.index.DelegatedOnlyOffender(pt, map[string]bool{})))
			}
		}
	}
}

func (c *checker) checkUniqueConstructors() {
	counts := map[*Type][]source.Span{}
	for _, node := range c.graph.Constructed {
		if node.Type == nil || !node.Type.Unique {
			continue
		}
		counts[node.Type] = append(counts[node.Type], node.Span)
	}
	for typ, spans := range counts {
		if len(spans) <= 1 {
			continue
		}
		for _, span := range spans[1:] {
			c.error(span, diag.SEM0007, "unique type "+typ.Name+" is constructed more than once")
		}
	}
}

func (c *checker) checkExecutorWiring() {
	pathOwners := map[string]string{}
	for _, exec := range c.graph.Executors {
		for _, field := range exec.Type.Fields {
			boundTo := ""
			if exec.FieldBindings != nil {
				boundTo = exec.FieldBindings[field.Name]
			}
			if field.Type == nil {
				continue
			}
			switch field.Type.Kind {
			case KindDriver:
				c.error(exec.Span, diag.SEM0010, "root driver "+field.Type.Name+" cannot be passed into executor "+exec.Type.Name)
			case KindDriverPath:
				if boundTo == "" {
					continue
				}
				if _, ok := pathOwners[boundTo]; ok {
					c.error(exec.Span, diag.SEM0011, "driver path "+boundTo+" is assigned to more than one executor")
					continue
				}
				pathOwners[boundTo] = exec.Type.Name
			}
		}
	}
}

func (c *checker) checkTypeAssign(span source.Span, targetType, valueType *Type) {
	if targetType == nil || valueType == nil {
		return
	}
	if !typesCompatible(targetType, valueType) {
		c.error(span, diag.CG0001, fmt.Sprintf("type mismatch: %s cannot assign %s", targetType.Name, valueType.Name))
	}
}

func (c *checker) checkOwnedDelegatedCrossing(span source.Span, valueType *Type) {
	if c.currentPhase != "owned_hardware" {
		return
	}
	if valueType == nil {
		return
	}
	if valueType == c.ownedRoot {
		return
	}
	if c.index.IsDelegatedOnly(valueType, map[string]bool{}) {
		c.error(span, diag.SEM0009, fmt.Sprintf("delegated-only value %s cannot cross into owned_hardware phase", c.index.DelegatedOnlyOffender(valueType, map[string]bool{})))
	}
}

func (c *checker) typeExpr(moduleName string, expr ast.Expr, scope *Scope, ctx ContextKind) *Type {
	switch e := expr.(type) {
	case *ast.NameExpr:
		if scope != nil {
			if t, ok := scope.Lookup(e.Name); ok {
				return t
			}
		}
		return c.mustType(moduleName, e.Name)
	case *ast.IntLiteral:
		return c.mustType(moduleName, "U64")
	case *ast.StringLiteral:
		return c.mustType(moduleName, "StringLiteral")
	case *ast.BoolLiteral:
		return c.mustType(moduleName, "Bool")
	case *ast.FieldExpr:
		baseType := c.typeExpr(moduleName, e.Base, scope, ctx)
		return c.lookupField(baseType, e.Field, e.SpanV)
	case *ast.ConstructorExpr:
		return c.typeConstructorExpr(moduleName, e, scope, ctx)
	case *ast.CallExpr:
		return c.typeCallExpr(moduleName, e, scope, ctx)
	case *ast.BinaryExpr:
		left := c.typeExpr(moduleName, e.Left, scope, ctx)
		right := c.typeExpr(moduleName, e.Right, scope, ctx)
		if left == nil || right == nil {
			return nil
		}
		if isComparisonOp(e.Op) {
			c.requireSame(left, right, e.SpanV)
			return c.mustType(moduleName, "Bool")
		}
		if (e.Op == "+" || e.Op == "-") && isAddressType(left) && isIntegerType(right) {
			return left
		}
		c.requireSame(left, right, e.SpanV)
		return left
	default:
		c.error(expr.Span(), diag.CG0001, "unsupported expression")
		return nil
	}
}

func (c *checker) typeConstructorExpr(moduleName string, expr *ast.ConstructorExpr, scope *Scope, ctx ContextKind) *Type {
	constructed := c.resolveType(moduleName, expr.Type)
	if constructed == nil {
		c.error(expr.SpanV, diag.SEM0002, "unknown constructor type "+expr.Type)
		return nil
	}

	c.checkConstructorPermissions(moduleName, expr, constructed, scope, ctx)
	if c.currentPhase == "owned_hardware" && c.hasDelegatedField(moduleName, constructed) {
		c.error(expr.SpanV, diag.SEM0009, "delegated-only value cannot be constructed in owned_hardware phase")
	}

	fieldBindings := map[string]string{}
	boundTypes := map[string]*Type{}
	for _, arg := range expr.Args {
		field, _, _ := c.lookupFieldForArg(constructed, arg.Name)
		if field == nil {
			c.error(expr.SpanV, diag.CG0001, "unknown constructor field "+arg.Name)
			continue
		}
		argType := c.typeExpr(moduleName, arg.Value, scope, ctx)
		c.checkTypeAssign(arg.SpanV, field.Type, argType)
		if named, ok := arg.Value.(*ast.NameExpr); ok {
			fieldBindings[arg.Name] = named.Name
			boundTypes[arg.Name] = argType
		}
	}

	if len(expr.Args) != len(constructed.Fields) {
		c.error(expr.SpanV, diag.CG0001, "constructor field completeness check failed")
	}
	seen := map[string]bool{}
	for _, arg := range expr.Args {
		seen[arg.Name] = true
	}
	for _, field := range constructed.Fields {
		if !seen[field.Name] {
			c.error(expr.SpanV, diag.CG0001, "missing constructor field "+field.Name)
		}
	}

	switch constructed.Kind {
	case KindExecutor:
		c.graph.Executors = append(c.graph.Executors, ExecutorNode{
			Type:          constructed,
			Span:          expr.SpanV,
			FieldBindings: fieldBindings,
			BoundTypes:    boundTypes,
		})
	case KindData:
		break
	default:
		// only constructors in image phase are part of the syntactic constructor graph
	}

	if c.ownedRoot != nil && constructed == c.ownedRoot &&
		ctx != ContextOwnershipTransferAuthorityMethod &&
		!(c.currentPhase == "delegated_hardware" && methodReceiverIsOwnershipAuthority(c.currentType, c.ownedRoot)) {
		c.error(expr.SpanV, diag.SEM0008, c.ownedRoot.Name+" can only be minted through ownership-transfer authority in phase delegated_hardware")
	}

	if ctx == ContextImagePhaseDirect {
		c.graph.Constructed = append(c.graph.Constructed, ConstructedNode{Type: constructed, Span: expr.SpanV})
	}
	return constructed
}

func (c *checker) checkConstructorPermissions(moduleName string, expr *ast.ConstructorExpr, typ *Type, scope *Scope, ctx ContextKind) {
	_ = moduleName
	_ = scope
	if typ.Kind == KindData {
		return
	}
	if c.canMintInContext(ctx, typ) {
		return
	}
	c.error(expr.SpanV, diag.SEM0006, typ.Kind.String()+" construction is allowed only directly inside image phase bodies")
}

func (c *checker) canMintInContext(ctx ContextKind, typ *Type) bool {
	if typ == nil {
		return false
	}
	if ctx == ContextImagePhaseDirect && typ.Kind != KindData {
		return true
	}
	return ctx == ContextOwnershipTransferAuthorityMethod && typ == c.ownedRoot
}

func (c *checker) typeCallExpr(moduleName string, expr *ast.CallExpr, scope *Scope, ctx ContextKind) *Type {
	recvType := c.typeExpr(moduleName, expr.Receiver, scope, ctx)
	if recvType == nil {
		return nil
	}
	method, errSpan := c.lookupMethod(recvType, expr.Method, expr.SpanV)
	if method == nil {
		c.error(errSpan, diag.CG0001, "unknown method "+expr.Method+" on "+recvType.Name)
		return nil
	}

	c.typeAndVerifyCallArgs(moduleName, method, expr.Args, scope, ctx)
	if method.Return == c.ownedRoot && !(c.currentPhase == "delegated_hardware" && c.isOwnershipTransferAuthority(recvType)) {
		c.error(expr.SpanV, diag.SEM0008, c.ownedRoot.Name+" can only be minted through ownership-transfer authority in phase delegated_hardware")
	}
	return method.Return
}

func (c *checker) typeAndVerifyCallArgs(moduleName string, method *Method, args []ast.NamedArg, scope *Scope, ctx ContextKind) {
	params := method.Params
	if len(params) > 0 && len(params[0].Name) > 0 && params[0].Name == "self" {
		params = params[1:]
	}
	used := map[string]bool{}
	for i, arg := range args {
		if i >= len(params) {
			c.error(arg.SpanV, diag.CG0001, "too many call arguments")
			return
		}
		if arg.Name != "" {
			found := false
			for _, p := range params {
				if p.Name == arg.Name {
					found = true
					used[p.Name] = true
					c.checkTypeAssign(arg.SpanV, p.Type, c.typeExpr(moduleName, arg.Value, scope, ctx))
					break
				}
			}
			if !found {
				c.error(arg.SpanV, diag.CG0001, "unknown call argument "+arg.Name)
			}
			continue
		}
		p := params[i]
		if p.Name != "" {
			used[p.Name] = true
			c.checkTypeAssign(arg.SpanV, p.Type, c.typeExpr(moduleName, arg.Value, scope, ctx))
		}
	}
	for _, p := range params {
		if p.Name == "self" {
			continue
		}
		if p.Name == "" {
			continue
		}
		if p.Name == "self" {
			continue
		}
		if _, ok := used[p.Name]; !ok && !strings.HasPrefix(p.Name, "_") {
			c.error(method.Span, diag.CG0001, "missing call argument "+p.Name)
		}
	}
	_ = ctx
}

func (c *checker) hasDelegatedField(moduleName string, typ *Type) bool {
	_ = moduleName
	return c.index.IsDelegatedOnly(typ, map[string]bool{})
}

func (c *checker) lookupFieldForArg(typ *Type, name string) (*Field, *Scope, bool) {
	if typ == nil {
		return nil, nil, false
	}
	for _, field := range typ.Fields {
		if field.Name == name {
			return &field, nil, true
		}
	}
	return nil, nil, false
}

func (c *checker) lookupField(base *Type, name string, span source.Span) *Type {
	if base == nil {
		c.error(span, diag.CG0001, "unknown receiver in field lookup")
		return nil
	}
	for _, field := range base.Fields {
		if field.Name == name {
			return field.Type
		}
	}
	c.error(span, diag.CG0001, "unknown field "+name)
	return nil
}

func (c *checker) lookupMethod(typ *Type, name string, span source.Span) (*Method, source.Span) {
	if typ == nil {
		return nil, span
	}
	for i := range typ.Methods {
		method := &typ.Methods[i]
		if method.Name == name {
			return method, method.Span
		}
	}
	return nil, span
}

func (c *checker) resolveType(moduleName, raw string) *Type {
	if raw == "" {
		return nil
	}
	return c.index.lookupType(moduleName, raw)
}

func (c *checker) mustType(moduleName, name string) *Type {
	return c.resolveType(moduleName, name)
}

func (c *checker) isOwnershipTransferAuthority(typ *Type) bool {
	if typ == nil || typ.Kind != KindClass || !typ.Unique || !typ.DelegatedOnly {
		return false
	}
	return c.hasMethodReturning(typ, c.ownedRoot)
}

func (c *checker) hasMethodReturning(typ *Type, ret *Type) bool {
	if typ == nil || ret == nil {
		return false
	}
	for _, m := range typ.Methods {
		if m.Return == ret {
			return true
		}
	}
	return false
}

func methodReceiverIsOwnershipAuthority(receiver *Type, ownedRoot *Type) bool {
	if receiver == nil || ownedRoot == nil {
		return false
	}
	if !receiver.DelegatedOnly || !receiver.Unique || receiver.Kind != KindClass {
		return false
	}
	for _, m := range receiver.Methods {
		if m.Return == ownedRoot {
			return true
		}
	}
	return false
}

func (c *checker) requireType(got, want *Type, span source.Span) {
	if got == nil || want == nil {
		return
	}
	if !typesCompatible(want, got) {
		c.error(span, diag.CG0001, fmt.Sprintf("type mismatch: got %s want %s", got.Name, want.Name))
	}
}

func (c *checker) requireSame(left, right *Type, span source.Span) {
	if left == nil || right == nil {
		return
	}
	if !typesCompatible(left, right) {
		c.error(span, diag.CG0001, "incompatible types")
	}
}

func typesCompatible(target, value *Type) bool {
	if target == nil || value == nil {
		return true
	}
	if target == value {
		return true
	}
	if isIntegerType(target) && isIntegerType(value) {
		return true
	}
	if isAddressType(target) && (isAddressType(value) || isIntegerType(value)) {
		return true
	}
	return false
}

func isIntegerType(t *Type) bool {
	if t == nil {
		return false
	}
	switch t.Name {
	case "U8", "U16", "U32", "U64", "I64":
		return true
	default:
		return false
	}
}

func isAddressType(t *Type) bool {
	if t == nil {
		return false
	}
	return t.Name == "PhysicalAddress" || t.Name == "VirtualAddress"
}

func (c *checker) requireBytesIterable(typ *Type, span source.Span) {
	if typ == nil {
		return
	}
	if typ.Name != "Bytes" && typ.Name != "MutableBytes" {
		c.error(span, diag.CG0001, "for loop expects Bytes")
	}
}

func (c *checker) checkOwnedDelegatedCrossingInExpr(expr ast.Expr, typ *Type) {
	_ = expr
	c.checkOwnedDelegatedCrossing(expr.Span(), typ)
}

func isComparisonOp(op string) bool {
	switch op {
	case "==", "!=", "<", ">", "<=", ">=", "&&", "||":
		return true
	default:
		return false
	}
}

func (c *checker) lookupImageModule(image *ast.ImageDecl) string {
	for _, mod := range c.modules {
		for _, decl := range mod.Decls {
			if decl == image {
				return mod.Name
			}
		}
	}
	return ""
}

func (c *checker) error(span source.Span, code, msg string) {
	c.diags = append(c.diags, diag.Diagnostic{
		Phase:    "sem",
		Code:     code,
		Severity: diag.Error,
		Start:    span.Start,
		End:      span.End,
		Message:  msg,
	})
}
