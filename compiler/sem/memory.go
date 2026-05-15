package sem

import (
	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/source"
)

type MemoryKind uint8

const (
	MemoryKindNone MemoryKind = iota
	MemoryKindRootArena
	MemoryKindFrameArena
	MemoryKindBytes
	MemoryKindMutableBytes
	MemoryKindCacheArena
)

type LifetimeKind uint8

const (
	LifetimeUnknown LifetimeKind = iota
	LifetimeStatic
	LifetimeExecutorRoot
	LifetimeFrame
	LifetimeCacheLookup
	LifetimeCacheCopy
)

type Lifetime struct {
	Kind  LifetimeKind
	Scope int
}

type MethodLifetimeSummary struct {
	ReturnFromParam int
	ReturnKind      LifetimeKind
	ReturnStatic    bool
	ReturnRoot      bool
	Terminates      bool
	Invalid         bool
}

type methodLifetimeTarget struct {
	ModuleName string
	Type       *Type
	Method     ast.MethodDecl
	ReturnType *Type
	Context    ContextKind
}

func ClassifyMemoryType(t *Type) MemoryKind {
	if t == nil {
		return MemoryKindNone
	}
	switch t.Module + "." + t.Name {
	case "machine.x86_64.executor_memory.ExecutorMemory":
		return MemoryKindRootArena
	case "machine.x86_64.executor_memory.ArenaFrame":
		return MemoryKindFrameArena
	case "machine.x86_64.executor_memory.Bytes":
		return MemoryKindBytes
	case "machine.x86_64.executor_memory.MutableBytes":
		return MemoryKindMutableBytes
	case "machine.x86_64.cache_memory.CacheArena":
		return MemoryKindCacheArena
	}
	return MemoryKindNone
}

func IsArenaType(t *Type) bool {
	kind := ClassifyMemoryType(t)
	return kind == MemoryKindRootArena || kind == MemoryKindFrameArena
}

func isCanonicalFrameIntrinsic(moduleName string, typ *Type, method ast.MethodDecl) bool {
	if moduleName != "machine.x86_64.executor_memory" || method.Name != "frame" {
		return false
	}
	if typ == nil || (typ.Name != "ExecutorMemory" && typ.Name != "ArenaFrame") {
		return false
	}
	params := method.Params
	if len(params) > 0 && params[0].Name == "self" {
		params = params[1:]
	}
	return len(params) == 1 && params[0].Name == "length" && params[0].Type == "U64"
}

func (c *checker) registerMethodLifetimeTargets() {
	if c.methodLifetimeTargets == nil {
		c.methodLifetimeTargets = map[string]methodLifetimeTarget{}
	}
	for _, mod := range c.modules {
		for _, decl := range mod.Decls {
			var typeName string
			var methods []ast.MethodDecl
			switch d := decl.(type) {
			case *ast.ClassDecl:
				typeName, methods = d.Name, d.Methods
			case *ast.DriverDecl:
				typeName, methods = d.Name, d.Methods
			case *ast.DriverPathDecl:
				typeName, methods = d.Name, d.Methods
			case *ast.ExecutorDecl:
				typeName, methods = d.Name, d.Methods
			default:
				continue
			}
			typ := c.index.resolveInScope(mod.Name, typeName)
			for _, method := range methods {
				if method.IsAsm || isCanonicalFrameIntrinsic(mod.Name, typ, method) {
					continue
				}
				marker := ContextNormalMethod
				returnType := c.mustType(mod.Name, method.Return)
				if c.isOwnershipTransferAuthority(typ) && returnType == c.ownedRoot {
					marker = ContextOwnershipTransferAuthorityMethod
				}
				c.methodLifetimeTargets[methodLifetimeKey(typ, method.Name)] = methodLifetimeTarget{
					ModuleName: mod.Name,
					Type:       typ,
					Method:     method,
					ReturnType: returnType,
					Context:    marker,
				}
			}
		}
	}
}

func (c *checker) ensureMethodLifetimeSummary(key string, span source.Span) MethodLifetimeSummary {
	if summary, ok := c.methodLifetimeSummaries[key]; ok {
		return summary
	}
	target, ok := c.methodLifetimeTargets[key]
	if !ok {
		return MethodLifetimeSummary{ReturnFromParam: -1, ReturnRoot: true, Terminates: true}
	}
	return c.checkMethodWithLifetimeSummary(key, target, span)
}

func (c *checker) checkMethodWithLifetimeSummary(key string, target methodLifetimeTarget, span source.Span) MethodLifetimeSummary {
	moduleName := target.ModuleName
	typ := target.Type
	method := target.Method
	summary := MethodLifetimeSummary{ReturnFromParam: -1}

	if c.methodLifetimeSummaries == nil {
		c.methodLifetimeSummaries = map[string]MethodLifetimeSummary{}
	}
	if c.activeMethodSummaries == nil {
		c.activeMethodSummaries = map[string]bool{}
	}
	if c.activeMethodSummaries[key] {
		c.error(span, diag.SEM0024, "recursive frame lifetime summary is not supported")
		summary.Invalid = true
		c.methodLifetimeSummaries[key] = summary
		return summary
	}

	scope := c.newMethodLifetimeScope(moduleName, typ, method)
	prev := c.currentMethodSummary
	prevPhase := c.currentPhase
	c.currentMethodSummary = &summary
	c.currentPhase = method.Name
	c.activeMethodSummaries[key] = true
	terminates := c.checkStmtList(moduleName, method.Body, scope, target.ReturnType, target.Context)
	summary.Terminates = terminates
	delete(c.activeMethodSummaries, key)
	c.currentPhase = prevPhase
	c.currentMethodSummary = prev
	c.methodLifetimeSummaries[key] = summary
	return summary
}

func methodLifetimeKey(typ *Type, methodName string) string {
	if typ == nil {
		return "::" + methodName
	}
	return typ.Module + "." + typ.Name + "::" + methodName
}

func (c *checker) newMethodLifetimeScope(moduleName string, typ *Type, method ast.MethodDecl) *Scope {
	scope := NewScope(nil)
	if len(method.Params) > 0 && method.Params[0].Name == "self" {
		scope.Define("self", typ)
		scope.DefineLifetime("self", Lifetime{Kind: LifetimeExecutorRoot})
	}
	explicitIndex := 0
	for _, p := range method.Params {
		if p.Name == "self" {
			continue
		}
		paramType := c.mustType(moduleName, p.Type)
		scope.Define(p.Name, paramType)
		if ClassifyMemoryType(paramType) == MemoryKindFrameArena {
			scope.DefineLifetime(p.Name, Lifetime{Kind: LifetimeFrame, Scope: -(explicitIndex + 1)})
		} else {
			scope.DefineLifetime(p.Name, Lifetime{Kind: LifetimeExecutorRoot})
		}
		explicitIndex++
	}
	return scope
}

func (c *checker) recordReturnLifetime(span source.Span, lifetime Lifetime) {
	if c.currentMethodSummary == nil {
		return
	}
	summary := c.currentMethodSummary
	switch {
	case lifetime.Kind == LifetimeStatic:
		if summary.ReturnFromParam >= 0 {
			c.error(span, diag.SEM0024, "method returns incompatible frame lifetimes")
			summary.Invalid = true
			return
		}
		summary.ReturnStatic = true
	case (lifetime.Kind == LifetimeFrame || lifetime.Kind == LifetimeCacheLookup || lifetime.Kind == LifetimeCacheCopy) && lifetime.Scope < 0:
		paramIndex := -lifetime.Scope - 1
		if summary.ReturnRoot || summary.ReturnStatic {
			c.error(span, diag.SEM0024, "method returns incompatible frame lifetimes")
			summary.Invalid = true
			return
		}
		if summary.ReturnFromParam == -1 {
			summary.ReturnFromParam = paramIndex
			summary.ReturnKind = lifetime.Kind
			return
		}
		if summary.ReturnFromParam != paramIndex || summary.ReturnKind != lifetime.Kind {
			c.error(span, diag.SEM0024, "method returns incompatible frame lifetimes")
			summary.Invalid = true
		}
	case lifetime.Kind == LifetimeFrame || lifetime.Kind == LifetimeCacheLookup || lifetime.Kind == LifetimeCacheCopy:
		c.error(span, diag.SEM0024, "frame value cannot escape")
		summary.Invalid = true
	default:
		if summary.ReturnFromParam >= 0 {
			c.error(span, diag.SEM0024, "method returns incompatible frame lifetimes")
			summary.Invalid = true
			return
		}
		summary.ReturnRoot = true
	}
}

func callArgForParam(method *Method, args []ast.NamedArg, paramIndex int) ast.Expr {
	if method == nil || paramIndex < 0 {
		return nil
	}
	params := method.Params
	if len(params) > 0 && params[0].Name == "self" {
		params = params[1:]
	}
	if paramIndex >= len(params) {
		return nil
	}
	param := params[paramIndex]
	positional := 0
	for _, arg := range args {
		if arg.Name != "" {
			if arg.Name == param.Name {
				return arg.Value
			}
			continue
		}
		if positional == paramIndex {
			return arg.Value
		}
		positional++
	}
	return nil
}

func constructorArgsAreIntegerLiterals(expr *ast.ConstructorExpr, names ...string) bool {
	wanted := map[string]bool{}
	for _, name := range names {
		wanted[name] = false
	}
	for _, arg := range expr.Args {
		if _, ok := wanted[arg.Name]; !ok {
			continue
		}
		if _, ok := arg.Value.(*ast.IntLiteral); !ok {
			return false
		}
		wanted[arg.Name] = true
	}
	for _, seen := range wanted {
		if !seen {
			return false
		}
	}
	return true
}

func explicitParamIndex(method *Method, name string) int {
	if method == nil {
		return -1
	}
	params := method.Params
	if len(params) > 0 && params[0].Name == "self" {
		params = params[1:]
	}
	for i, param := range params {
		if param.Name == name {
			return i
		}
	}
	return -1
}

func (c *checker) isFrameCall(moduleName string, expr ast.Expr, scope *Scope, ctx ContextKind) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok || call.Method != "frame" {
		return false
	}
	recvType := c.typeExpr(moduleName, call.Receiver, scope, ctx)
	if !IsArenaType(recvType) {
		c.error(call.Receiver.Span(), diag.SEM0021, "frame receiver must be ExecutorMemory or ArenaFrame")
		return false
	}
	if len(call.Args) != 1 || call.Args[0].Name != "length" {
		return false
	}
	lengthType := c.typeExpr(moduleName, call.Args[0].Value, scope, ctx)
	u64 := c.mustType(moduleName, "U64")
	if lengthType != nil && !typesCompatible(u64, lengthType) {
		c.error(call.Args[0].Value.Span(), diag.SEM0023, "frame length must be U64")
		return false
	}
	return true
}

func (c *checker) frameReceiverLifetime(expr ast.Expr, scope *Scope) Lifetime {
	call, ok := expr.(*ast.CallExpr)
	if !ok || call.Method != "frame" {
		return Lifetime{Kind: LifetimeExecutorRoot}
	}
	return c.lifetimeOfExpr(call.Receiver, scope)
}

func (c *checker) lifetimeOfExpr(expr ast.Expr, scope *Scope) Lifetime {
	if expr == nil {
		return Lifetime{Kind: LifetimeUnknown}
	}
	if c.exprLifetimes != nil {
		if lifetime, ok := c.exprLifetimes[expr]; ok {
			return lifetime
		}
	}
	switch e := expr.(type) {
	case *ast.NameExpr:
		if lifetime, ok := scope.LookupLifetime(e.Name); ok {
			return lifetime
		}
	case *ast.FieldExpr:
		baseLifetime := c.lifetimeOfExpr(e.Base, scope)
		if baseLifetime.Kind == LifetimeCacheLookup && e.Field == "bytes" {
			lifetime := Lifetime{Kind: LifetimeCacheCopy, Scope: baseLifetime.Scope}
			c.rememberLifetime(e, lifetime)
			return lifetime
		}
		return baseLifetime
	case *ast.StringLiteral:
		return Lifetime{Kind: LifetimeStatic}
	case *ast.IntLiteral, *ast.BoolLiteral:
		return Lifetime{Kind: LifetimeStatic}
	}
	return Lifetime{Kind: LifetimeUnknown}
}

func (c *checker) rememberLifetime(expr ast.Expr, lifetime Lifetime) {
	if c.exprLifetimes == nil {
		c.exprLifetimes = map[ast.Expr]Lifetime{}
	}
	c.exprLifetimes[expr] = lifetime
}

func (c *checker) rememberLocalLifetime(scope *Scope, name string, lifetime Lifetime) {
	if lifetime.Kind == LifetimeUnknown {
		lifetime = Lifetime{Kind: LifetimeExecutorRoot}
	}
	scope.DefineLifetime(name, lifetime)
}

func (c *checker) assignmentTargetLifetime(expr ast.Expr, scope *Scope) Lifetime {
	switch target := expr.(type) {
	case *ast.NameExpr:
		if lifetime, ok := scope.LookupLifetime(target.Name); ok {
			return lifetime
		}
		return Lifetime{Kind: LifetimeExecutorRoot}
	case *ast.FieldExpr:
		if name, ok := target.Base.(*ast.NameExpr); ok && name.Name == "self" {
			return Lifetime{Kind: LifetimeExecutorRoot}
		}
		return c.lifetimeOfExpr(target.Base, scope)
	default:
		return Lifetime{Kind: LifetimeExecutorRoot}
	}
}

func (c *checker) rejectIfLifetimeEscapes(span source.Span, value, target Lifetime) {
	if c.lifetimeShorterThan(value, target) {
		c.error(span, diag.SEM0025, "frame value cannot be stored")
	}
}

func (c *checker) rejectCacheEscape(span source.Span, lifetime Lifetime, target Lifetime) bool {
	if (lifetime.Kind == LifetimeCacheLookup || lifetime.Kind == LifetimeCacheCopy) && c.lifetimeShorterThan(lifetime, target) {
		c.error(span, diag.SEM0031, "cache lookup result cannot escape destination frame")
		return true
	}
	return false
}

func (c *checker) pushFrameLifetime(name string, span source.Span, parentLifetime Lifetime) int {
	_ = name
	_ = span
	c.nextFrameScope++
	id := c.nextFrameScope
	parent := 0
	if parentLifetime.Kind == LifetimeFrame || parentLifetime.Kind == LifetimeCacheLookup || parentLifetime.Kind == LifetimeCacheCopy {
		parent = parentLifetime.Scope
	}
	if c.frameLifetimeParents == nil {
		c.frameLifetimeParents = map[int]int{}
	}
	c.frameLifetimeParents[id] = parent
	c.frameLifetimeStack = append(c.frameLifetimeStack, id)
	return id
}

func (c *checker) popFrameLifetime(id int) {
	_ = id
	if len(c.frameLifetimeStack) == 0 {
		return
	}
	c.frameLifetimeStack = c.frameLifetimeStack[:len(c.frameLifetimeStack)-1]
}

func (c *checker) currentFrameLifetime() Lifetime {
	if len(c.frameLifetimeStack) == 0 {
		return Lifetime{Kind: LifetimeExecutorRoot}
	}
	return Lifetime{Kind: LifetimeFrame, Scope: c.frameLifetimeStack[len(c.frameLifetimeStack)-1]}
}

func (c *checker) frameIsAncestorOf(ancestor, descendant int) bool {
	if ancestor == 0 || descendant == 0 {
		return false
	}
	for current := descendant; current != 0; current = c.frameLifetimeParents[current] {
		if current == ancestor {
			return true
		}
	}
	return false
}

func (c *checker) lifetimeShorterThan(value, target Lifetime) bool {
	if value.Kind != LifetimeFrame && value.Kind != LifetimeCacheLookup && value.Kind != LifetimeCacheCopy {
		return false
	}
	if target.Kind == LifetimeFrame {
		if target.Scope == value.Scope {
			return false
		}
		if c.frameIsAncestorOf(value.Scope, target.Scope) {
			return false
		}
	}
	return true
}

func (c *checker) typeArenaIntrinsicCall(moduleName string, expr *ast.CallExpr, scope *Scope, ctx ContextKind) *Type {
	recvType := c.typeExpr(moduleName, expr.Receiver, scope, ctx)
	if !IsArenaType(recvType) {
		return nil
	}
	receiverLifetime := c.lifetimeOfExpr(expr.Receiver, scope)
	if receiverLifetime.Kind == LifetimeUnknown {
		receiverLifetime = Lifetime{Kind: LifetimeExecutorRoot}
	}
	if ctx == ContextOnHandler && (expr.Method == "place" || expr.Method == "reserve") {
		c.error(expr.SpanV, diag.SEM0016, "on handler cannot place or reserve arena memory")
		return nil
	}
	switch expr.Method {
	case "place":
		if len(expr.Args) != 1 || expr.Args[0].Name != "" {
			c.error(expr.SpanV, diag.SEM0026, "place expects one constructor argument")
			return nil
		}
		cons, ok := expr.Args[0].Value.(*ast.ConstructorExpr)
		if !ok {
			c.error(expr.Args[0].Value.Span(), diag.SEM0026, "place argument must be a constructor expression")
			return nil
		}
		typ := c.typeConstructorExpr(moduleName, cons, scope, ctx)
		c.rememberLifetime(expr, receiverLifetime)
		return typ
	case "reserve":
		c.requireReserveArgs(moduleName, expr, scope, ctx)
		c.rememberLifetime(expr, receiverLifetime)
		return c.resolveType("machine.x86_64.executor_memory", "MutableBytes")
	default:
		return nil
	}
}

func (c *checker) requireReserveArgs(moduleName string, expr *ast.CallExpr, scope *Scope, ctx ContextKind) {
	if len(expr.Args) != 2 || expr.Args[0].Name != "length" || expr.Args[1].Name != "align" {
		c.error(expr.SpanV, diag.SEM0027, "reserve expects length and align")
		return
	}
	u64 := c.mustType(moduleName, "U64")
	lengthType := c.typeExpr(moduleName, expr.Args[0].Value, scope, ctx)
	if lengthType != nil && !typesCompatible(u64, lengthType) {
		c.error(expr.Args[0].Value.Span(), diag.SEM0027, "reserve length and align must be U64")
	}
	alignType := c.typeExpr(moduleName, expr.Args[1].Value, scope, ctx)
	if alignType != nil && !typesCompatible(u64, alignType) {
		c.error(expr.Args[1].Value.Span(), diag.SEM0027, "reserve length and align must be U64")
	}
}
