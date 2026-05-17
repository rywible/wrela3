package sem

import (
	"fmt"
	"strconv"
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
	ContextInterruptEvent
	ContextOnHandler
)

type Scope struct {
	parent           *Scope
	types            map[string]*Type
	lifetimes        map[string]Lifetime
	driverPathKeys   map[string]string
	driverPathFields map[string]map[string]string
	origins          map[string]localOrigin
}

type localOrigin struct {
	Type                *Type
	Constructor         *ast.ConstructorExpr
	FieldBindings       map[string]string
	SlotLabel           string
	LoopPolicy          string
	MemoryOwnerLabel    string
	TopicLabel          string
	TopicKind           string
	TopicDepth          uint64
	PathLabel           string
	PublishesInterrupts bool
	EventType           string
	EventFunctionSymbol string
	VcpuID              int
	HasVcpuID           bool
	ExecutorBinding     string
	TerminalVcpu        bool
	PciDeviceKey        string
}

func NewScope(parent *Scope) *Scope {
	return &Scope{
		parent:           parent,
		types:            map[string]*Type{},
		lifetimes:        map[string]Lifetime{},
		driverPathKeys:   map[string]string{},
		driverPathFields: map[string]map[string]string{},
		origins:          map[string]localOrigin{},
	}
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

func (s *Scope) DefineLifetime(name string, lifetime Lifetime) {
	if s == nil {
		return
	}
	s.lifetimes[name] = lifetime
}

func (s *Scope) LookupLifetime(name string) (Lifetime, bool) {
	if s == nil {
		return Lifetime{}, false
	}
	if lifetime, ok := s.lifetimes[name]; ok {
		return lifetime, true
	}
	if s.parent != nil {
		return s.parent.LookupLifetime(name)
	}
	return Lifetime{}, false
}

func (s *Scope) DefineOrigin(name string, origin localOrigin) {
	if s == nil {
		return
	}
	s.origins[name] = origin
}

func (s *Scope) LookupOrigin(name string) (localOrigin, bool) {
	if s == nil {
		return localOrigin{}, false
	}
	if origin, ok := s.origins[name]; ok {
		return origin, true
	}
	if s.parent != nil {
		return s.parent.LookupOrigin(name)
	}
	return localOrigin{}, false
}

func (s *Scope) DefineDriverPath(name, key string) {
	if s == nil || key == "" {
		return
	}
	s.driverPathKeys[name] = key
}

func (s *Scope) LookupDriverPath(name string) (string, bool) {
	if s == nil {
		return "", false
	}
	if key := s.driverPathKeys[name]; key != "" {
		return key, true
	}
	if s.parent != nil {
		return s.parent.LookupDriverPath(name)
	}
	return "", false
}

func (s *Scope) DefineDriverPathFields(name string, fields map[string]string) {
	if s == nil || len(fields) == 0 {
		return
	}
	copied := map[string]string{}
	for field, key := range fields {
		if key != "" {
			copied[field] = key
		}
	}
	if len(copied) != 0 {
		s.driverPathFields[name] = copied
	}
}

func (s *Scope) LookupDriverPathField(name, field string) (string, bool) {
	if s == nil {
		return "", false
	}
	if fields := s.driverPathFields[name]; fields != nil {
		if key := fields[field]; key != "" {
			return key, true
		}
	}
	if s.parent != nil {
		return s.parent.LookupDriverPathField(name, field)
	}
	return "", false
}

func (s *Scope) LookupDriverPathFields(name string) (map[string]string, bool) {
	if s == nil {
		return nil, false
	}
	if fields := s.driverPathFields[name]; fields != nil {
		return fields, true
	}
	if s.parent != nil {
		return s.parent.LookupDriverPathFields(name)
	}
	return nil, false
}

type checker struct {
	index                   *Index
	modules                 []*ast.Module
	currentType             *Type
	currentPhase            string
	diags                   []diag.Diagnostic
	ownedRoot               *Type
	graph                   ImageGraph
	allowFrameCallExpr      bool
	allowPlaceConstructor   *placeConstructorAllowance
	exprLifetimes           map[ast.Expr]Lifetime
	frameLifetimeStack      []int
	frameLifetimeParents    map[int]int
	nextFrameScope          int
	methodLifetimeTargets   map[string]methodLifetimeTarget
	methodLifetimeSummaries map[string]MethodLifetimeSummary
	activeMethodSummaries   map[string]bool
	currentMethodSummary    *MethodLifetimeSummary
}

type driverPathOwner struct {
	executor string
	span     source.Span
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
	c.checkUnresolvedTypes()
	c.checkDeclBodiesAndConstructors()
	c.finalizeInterruptTopicRoutes()
	c.checkDelegatedOnlyCrossing()
	c.checkUniqueConstructors()
	c.checkExecutorWiring()
	c.checkExecutorTopicGraph()
	c.checkHardwareClaims()

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

		if len(image.Transitions) != 1 {
			c.error(image.SpanV, diag.SEM0005, "image must define exactly one transition")
			continue
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
		if len(owned.Params) != 1 {
			c.error(owned.SpanV, diag.SEM0005, "owned_hardware phase must have one parameter")
			continue
		}
		signatureValid := true
		delegatedParamType := c.resolveType(imageModule, delegated.Params[0].Type)
		if delegatedParamType != c.resolveType(imageModule, "DelegatedHardware") {
			c.error(delegated.Params[0].Span, diag.SEM0005, "delegated_hardware phase must accept DelegatedHardware")
			signatureValid = false
		}

		ownedParam := c.mustType(imageModule, delegated.Return)
		if ownedParam == nil {
			c.error(delegated.SpanV, diag.SEM0005, "unknown delegated_hardware return type")
			continue
		}

		if ownedParam != c.resolveType(imageModule, owned.Params[0].Type) {
			c.error(owned.Params[0].Span, diag.SEM0005, "owned_hardware phase must receive the same type returned by delegated_hardware")
			continue
		}
		if c.resolveType(imageModule, owned.Return) != c.resolveType(imageModule, "never") {
			c.error(owned.SpanV, diag.SEM0005, "owned_hardware phase must return never")
			continue
		}
		if !signatureValid {
			continue
		}
		c.ownedRoot = ownedParam
	}
}

func (c *checker) checkUnresolvedTypes() {
	for _, mod := range c.modules {
		for _, decl := range mod.Decls {
			switch d := decl.(type) {
			case *ast.DataDecl:
				c.checkFieldsResolved(mod.Name, d.Fields)
				c.checkMethodTypesResolved(mod.Name, d.Methods)
			case *ast.ClassDecl:
				c.checkFieldsResolved(mod.Name, d.Fields)
				c.checkMethodTypesResolved(mod.Name, d.Methods)
			case *ast.DriverDecl:
				c.checkFieldsResolved(mod.Name, d.Fields)
				c.checkMethodTypesResolved(mod.Name, d.Methods)
			case *ast.DriverPathDecl:
				c.checkFieldsResolved(mod.Name, d.Fields)
				c.checkMethodTypesResolved(mod.Name, d.Methods)
				for _, event := range d.InterruptEvents {
					if c.resolveType(mod.Name, event.EventType) == nil {
						c.error(event.SpanV, diag.SEM0002, "unknown type "+event.EventType)
					}
				}
			case *ast.ExecutorDecl:
				c.checkFieldsResolved(mod.Name, d.Fields)
				c.checkMethodTypesResolved(mod.Name, d.Methods)
				for _, handler := range d.OnHandlers {
					if c.resolveType(mod.Name, handler.ParamType) == nil {
						c.error(handler.SpanV, diag.SEM0002, "unknown type "+handler.ParamType)
					}
				}
			case *ast.ImageDecl:
				for _, phase := range d.Phases {
					c.checkParamsResolved(mod.Name, phase.Params)
					if c.resolveType(mod.Name, phase.Return) == nil {
						c.error(phase.SpanV, diag.SEM0002, "unknown type "+phase.Return)
					}
				}
			}
		}
	}
}

func (c *checker) checkFieldsResolved(moduleName string, fields []ast.Field) {
	for _, field := range fields {
		if c.resolveType(moduleName, field.Type) == nil {
			c.error(field.Span, diag.SEM0002, "unknown type "+field.Type)
		}
	}
}

func (c *checker) checkParamsResolved(moduleName string, params []ast.Param) {
	for _, param := range params {
		if param.Type != "" && c.resolveType(moduleName, param.Type) == nil {
			c.error(param.Span, diag.SEM0002, "unknown type "+param.Type)
		}
	}
}

func (c *checker) checkMethodTypesResolved(moduleName string, methods []ast.MethodDecl) {
	for _, method := range methods {
		c.checkParamsResolved(moduleName, method.Params)
		if method.Return != "" && c.resolveType(moduleName, method.Return) == nil {
			c.error(method.SpanV, diag.SEM0002, "unknown type "+method.Return)
		}
	}
}

func (c *checker) checkDeclBodiesAndConstructors() {
	c.registerMethodLifetimeTargets()
	for _, mod := range c.modules {
		for _, decl := range mod.Decls {
			switch d := decl.(type) {
			case *ast.ImageDecl:
				c.checkImageDecl(mod.Name, d)
			case *ast.DataDecl:
				typ := c.index.resolveInScope(mod.Name, d.Name)
				c.checkMethods(mod.Name, typ, d.Methods)
			case *ast.ClassDecl:
				typ := c.index.resolveInScope(mod.Name, d.Name)
				c.checkMethods(mod.Name, typ, d.Methods)
			case *ast.DriverDecl:
				typ := c.index.resolveInScope(mod.Name, d.Name)
				c.checkMethods(mod.Name, typ, d.Methods)
			case *ast.DriverPathDecl:
				typ := c.index.resolveInScope(mod.Name, d.Name)
				c.checkMethods(mod.Name, typ, d.Methods)
				c.checkInterruptEvents(mod.Name, typ, d.InterruptEvents)
			case *ast.ExecutorDecl:
				typ := c.index.resolveInScope(mod.Name, d.Name)
				c.checkMethods(mod.Name, typ, d.Methods)
				c.checkOnHandlers(mod.Name, typ, d.OnHandlers)
			}
		}
	}
}

func (c *checker) checkInterruptEvents(moduleName string, path *Type, events []ast.InterruptEventDecl) {
	if path == nil {
		return
	}
	for _, event := range events {
		retType := c.resolveType(moduleName, event.EventType)
		if retType == nil {
			c.error(event.SpanV, diag.SEM0002, "unknown type "+event.EventType)
			continue
		}
		if retType.Kind != KindData {
			c.error(event.SpanV, diag.SEM0015, "interrupt event type must be a data record")
			continue
		}
		scope := NewScope(nil)
		scope.Define("self", path)
		prevType := c.currentType
		prevPhase := c.currentPhase
		c.currentType = path
		c.currentPhase = "interrupt receiver"
		terminates := c.checkStmtList(moduleName, event.Body, scope, retType, ContextInterruptEvent)
		c.currentType = prevType
		c.currentPhase = prevPhase
		if retType != nil && !terminates {
			c.error(event.SpanV, diag.SEM0015, "interrupt event must return "+retType.Name)
		}
	}
}

func (c *checker) checkOnHandlers(moduleName string, exec *Type, handlers []ast.OnHandlerDecl) {
	if exec == nil {
		return
	}
	for _, handler := range handlers {
		c.error(handler.SpanV, diag.SEM0042, "executor on interrupt handlers are no longer supported; use path-owned interrupt topics")
		key := handler.PathField + ".interrupt"
		field := c.fieldByName(exec, handler.PathField)
		if field == nil || field.Type == nil || field.Type.Kind != KindDriverPath {
			c.error(handler.SpanV, diag.SEM0018, "on handler must reference a driver path field with an interrupt event")
			continue
		}
		event := c.eventDeclForPath(field.Type)
		if event == nil {
			c.error(handler.SpanV, diag.SEM0018, "on handler must reference a driver path field with an interrupt event")
			continue
		}
		paramType := c.resolveType(moduleName, handler.ParamType)
		if paramType == nil {
			c.error(handler.SpanV, diag.SEM0002, "unknown type "+handler.ParamType)
			continue
		}
		eventType := c.resolveType(field.Type.Module, event.EventType)
		if eventType == nil || !typesCompatible(eventType, paramType) || eventType.Module != paramType.Module || eventType.Name != paramType.Name {
			c.error(handler.SpanV, diag.SEM0016, "on handler parameter type must match interrupt event type")
		}
		scope := NewScope(nil)
		scope.Define("self", exec)
		scope.Define(handler.ParamName, paramType)
		prevType := c.currentType
		prevPhase := c.currentPhase
		c.currentType = exec
		c.currentPhase = "on " + key
		c.checkStmtList(moduleName, handler.Body, scope, nil, ContextOnHandler)
		c.currentType = prevType
		c.currentPhase = prevPhase
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
		terminates := c.checkStmtList(moduleName, phase.Body, scope, retType, ContextImagePhaseDirect)
		c.currentPhase = prevPhase
		if retType != nil && !terminates {
			c.error(phase.SpanV, diag.CG0001, "missing return")
		}
	}
}

func (c *checker) checkMethods(moduleName string, typ *Type, methods []ast.MethodDecl) {
	c.currentType = typ
	for _, method := range methods {
		c.currentType = typ
		if explicitParamCount(method.Params) > 5 {
			c.error(method.SpanV, diag.SEM0013, "too many explicit parameters")
		}
		if method.IsAsm && !c.isAsmAllowedHere(typ) {
			c.error(method.SpanV, diag.SEM0032, "asm raw memory access requires edge-capability module")
		}
		if isCanonicalFrameIntrinsic(moduleName, typ, method) {
			continue
		}

		returnType := c.mustType(moduleName, method.Return)
		if method.IsAsm {
			continue
		}

		key := methodLifetimeKey(typ, method.Name)
		summary := c.ensureMethodLifetimeSummary(key, method.SpanV)
		terminates := summary.Terminates
		if returnType != nil && !terminates {
			c.error(method.SpanV, diag.CG0001, "missing return")
		}
	}
	c.currentType = nil
}

func explicitParamCount(params []ast.Param) int {
	count := 0
	for _, param := range params {
		if param.Name == "self" {
			continue
		}
		count++
	}
	return count
}

func (c *checker) isAsmAllowedHere(typ *Type) bool {
	if typ == nil {
		return false
	}
	return isEdgeCapabilityModule(typ.Module)
}

func isEdgeCapabilityModule(moduleName string) bool {
	return strings.HasPrefix(moduleName, "arch.") ||
		strings.HasPrefix(moduleName, "platform.") ||
		moduleName == "machine.x86_64" ||
		strings.HasPrefix(moduleName, "machine.x86_64.")
}

func (c *checker) checkStmtList(moduleName string, stmts []ast.Stmt, scope *Scope, expectedReturn *Type, ctx ContextKind) bool {
	terminates := false
	afterVcpuEnter := false
	for _, stmt := range stmts {
		if afterVcpuEnter && ctx == ContextImagePhaseDirect {
			c.error(stmt.Span(), diag.SEM0036, "vCPU enter must be the final reachable statement in the phase")
			afterVcpuEnter = false
		}
		if c.checkStmt(moduleName, stmt, scope, expectedReturn, ctx) {
			terminates = true
		}
		if ctx == ContextImagePhaseDirect {
			if isVcpuEnterExprStmt(stmt) {
				afterVcpuEnter = true
				continue
			}
			if span, ok := stmtVcpuEnterSpan(stmt); ok {
				c.error(span, diag.SEM0036, "vCPU enter must be the final reachable statement in the phase")
				afterVcpuEnter = true
			}
		}
	}
	return terminates
}

func isVcpuEnterExprStmt(stmt ast.Stmt) bool {
	exprStmt, ok := stmt.(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := exprStmt.Expr.(*ast.CallExpr)
	return ok && call.Method == "enter"
}

func stmtVcpuEnterSpan(stmt ast.Stmt) (source.Span, bool) {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		return exprVcpuEnterSpan(s.Expr)
	case *ast.AssignStmt:
		if span, ok := exprVcpuEnterSpan(s.Target); ok {
			return span, true
		}
		return exprVcpuEnterSpan(s.Value)
	case *ast.ReturnStmt:
		return exprVcpuEnterSpan(s.Value)
	case *ast.ExprStmt:
		return exprVcpuEnterSpan(s.Expr)
	case *ast.IfStmt:
		if span, ok := exprVcpuEnterSpan(s.Cond); ok {
			return span, true
		}
		if span, ok := stmtListVcpuEnterSpan(s.Then); ok {
			return span, true
		}
		return stmtListVcpuEnterSpan(s.Else)
	case *ast.WhileStmt:
		if span, ok := exprVcpuEnterSpan(s.Cond); ok {
			return span, true
		}
		return stmtListVcpuEnterSpan(s.Body)
	case *ast.ForStmt:
		if span, ok := exprVcpuEnterSpan(s.InExpr); ok {
			return span, true
		}
		return stmtListVcpuEnterSpan(s.Body)
	case *ast.WithStmt:
		if span, ok := exprVcpuEnterSpan(s.Expr); ok {
			return span, true
		}
		return stmtListVcpuEnterSpan(s.Body)
	default:
		return source.Span{}, false
	}
}

func stmtListVcpuEnterSpan(stmts []ast.Stmt) (source.Span, bool) {
	for _, stmt := range stmts {
		if span, ok := stmtVcpuEnterSpan(stmt); ok {
			return span, true
		}
	}
	return source.Span{}, false
}

func exprVcpuEnterSpan(expr ast.Expr) (source.Span, bool) {
	switch e := expr.(type) {
	case nil:
		return source.Span{}, false
	case *ast.CallExpr:
		if e.Method == "enter" {
			return e.SpanV, true
		}
		if span, ok := exprVcpuEnterSpan(e.Receiver); ok {
			return span, true
		}
		for _, arg := range e.Args {
			if span, ok := exprVcpuEnterSpan(arg.Value); ok {
				return span, true
			}
		}
	case *ast.ConstructorExpr:
		for _, arg := range e.Args {
			if span, ok := exprVcpuEnterSpan(arg.Value); ok {
				return span, true
			}
		}
	case *ast.FieldExpr:
		return exprVcpuEnterSpan(e.Base)
	case *ast.BinaryExpr:
		if span, ok := exprVcpuEnterSpan(e.Left); ok {
			return span, true
		}
		return exprVcpuEnterSpan(e.Right)
	}
	return source.Span{}, false
}

func (c *checker) checkStmt(moduleName string, stmt ast.Stmt, scope *Scope, expectedReturn *Type, ctx ContextKind) bool {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		valueType := c.typeExpr(moduleName, s.Expr, scope, ctx)
		if ctx != ContextImagePhaseDirect {
			c.recordReliableTryPublishCall(moduleName, s.Expr, scope, true)
			c.recordSubscriptionMethodCall(moduleName, s.Expr, scope)
		}
		valueLifetime := c.lifetimeOfExpr(s.Expr, scope)
		scope.Define(s.Name, valueType)
		origin := c.originForLetValue(moduleName, s.Expr, valueType, scope)
		scope.DefineOrigin(s.Name, origin)
		if ctx == ContextImagePhaseDirect {
			c.recordGraphFromLet(s.Name, origin, s.SpanV)
		}
		c.rememberLocalLifetime(scope, s.Name, valueLifetime)
		if ctx == ContextImagePhaseDirect && valueType != nil && valueType.Kind == KindDriverPath {
			c.bindDriverPath(s.Name, s.Expr, scope)
		}
		if ctx == ContextImagePhaseDirect {
			scope.DefineDriverPathFields(s.Name, c.driverPathFieldKeysForExpr(moduleName, s.Expr, scope))
		}
		c.checkOwnedDelegatedCrossing(s.Expr.Span(), valueType)
		return isNeverType(valueType)
	case *ast.AssignStmt:
		targetType := c.typeExpr(moduleName, s.Target, scope, ctx)
		valueType := c.typeExpr(moduleName, s.Value, scope, ctx)
		c.checkTypeAssign(s.Target.Span(), targetType, valueType)
		sourceLifetime := c.lifetimeOfExpr(s.Value, scope)
		targetLifetime := c.assignmentTargetLifetime(s.Target, scope)
		if !c.rejectCacheEscape(s.Value.Span(), sourceLifetime, targetLifetime) {
			c.rejectIfLifetimeEscapes(s.Value.Span(), sourceLifetime, targetLifetime)
		}
		c.checkOwnedDelegatedCrossing(s.Value.Span(), valueType)
		return isNeverType(valueType)
	case *ast.IfStmt:
		cond := c.typeExpr(moduleName, s.Cond, scope, ctx)
		c.requireType(cond, c.mustType(moduleName, "Bool"), s.Cond.Span())
		thenTerminates := c.checkStmtList(moduleName, s.Then, NewScope(scope), expectedReturn, ctx)
		if s.Else != nil {
			elseTerminates := c.checkStmtList(moduleName, s.Else, NewScope(scope), expectedReturn, ctx)
			return thenTerminates && elseTerminates
		}
	case *ast.WhileStmt:
		if ctx == ContextOnHandler {
			c.error(s.SpanV, diag.SEM0016, "on handler cannot contain loops")
		}
		cond := c.typeExpr(moduleName, s.Cond, scope, ctx)
		c.requireType(cond, c.mustType(moduleName, "Bool"), s.Cond.Span())
		c.checkStmtList(moduleName, s.Body, NewScope(scope), expectedReturn, ctx)
		return isTrueLiteral(s.Cond)
	case *ast.ForStmt:
		if ctx == ContextOnHandler {
			c.error(s.SpanV, diag.SEM0016, "on handler cannot contain loops")
		}
		inType := c.typeExpr(moduleName, s.InExpr, scope, ctx)
		c.requireBytesIterable(inType, s.InExpr.Span())
		loopScope := NewScope(scope)
		loopScope.Define(s.Var, c.mustType(moduleName, "U8"))
		c.checkStmtList(moduleName, s.Body, loopScope, expectedReturn, ctx)
	case *ast.WithStmt:
		prevAllowFrameCall := c.allowFrameCallExpr
		c.allowFrameCallExpr = true
		frameType := c.typeExpr(moduleName, s.Expr, scope, ctx)
		c.allowFrameCallExpr = prevAllowFrameCall
		if frameType == nil {
			c.checkStmtList(moduleName, s.Body, NewScope(scope), expectedReturn, ctx)
			return false
		}
		if ClassifyMemoryType(frameType) != MemoryKindFrameArena {
			c.error(s.Expr.Span(), diag.SEM0022, "with expression must be arena.frame(length = ...)")
		} else if !c.isFrameCall(moduleName, s.Expr, scope, ctx) {
			c.error(s.Expr.Span(), diag.SEM0022, "with expression must be arena.frame(length = ...)")
		}
		child := NewScope(scope)
		child.Define(s.Name, frameType)
		parentLifetime := c.frameReceiverLifetime(s.Expr, scope)
		childScopeID := c.pushFrameLifetime(parentLifetime)
		child.DefineLifetime(s.Name, Lifetime{Kind: LifetimeFrame, Scope: childScopeID})
		terminates := c.checkStmtList(moduleName, s.Body, child, expectedReturn, ctx)
		c.popFrameLifetime(childScopeID, s.SpanV)
		return terminates
	case *ast.ReturnStmt:
		if expectedReturn == nil {
			if s.Value != nil {
				if ctx == ContextOnHandler {
					c.error(s.SpanV, diag.SEM0016, "on handler cannot return a value")
					return true
				}
				c.error(s.SpanV, diag.CG0001, "cannot return value from void function")
				return true
			}
			return true
		}
		if expectedReturn == c.mustType(moduleName, "never") {
			if s.Value != nil {
				c.error(s.Value.Span(), diag.CG0001, "never function cannot return a value")
				return true
			}
			c.error(s.SpanV, diag.CG0001, "never function cannot return")
			return true
		}
		if s.Value == nil {
			if ctx == ContextInterruptEvent {
				c.error(s.SpanV, diag.SEM0015, "interrupt event must return a value")
			} else {
				c.error(s.SpanV, diag.CG0001, "missing return value")
			}
			return true
		}
		got := c.typeExpr(moduleName, s.Value, scope, ctx)
		if ctx == ContextInterruptEvent {
			if got != nil && expectedReturn != nil && (got.Module != expectedReturn.Module || got.Name != expectedReturn.Name) {
				c.error(s.Value.Span(), diag.SEM0015, "interrupt event return type mismatch")
			}
		} else {
			c.requireType(got, expectedReturn, s.Value.Span())
		}
		lifetime := c.lifetimeOfExpr(s.Value, scope)
		c.recordReturnLifetime(s.Value.Span(), lifetime)
		if c.currentMethodSummary != nil &&
			(lifetime.Kind == LifetimeFrame || lifetime.Kind == LifetimeCacheLookup || lifetime.Kind == LifetimeCacheCopy) &&
			lifetime.Scope < 0 {
			return true
		}
		if lifetime.Kind == LifetimeFrame || lifetime.Kind == LifetimeCacheLookup || lifetime.Kind == LifetimeCacheCopy {
			if c.rejectCacheEscape(s.Value.Span(), lifetime, Lifetime{Kind: LifetimeExecutorRoot}) {
				return true
			}
			c.error(s.Value.Span(), diag.SEM0024, "frame value cannot escape")
		}
		c.checkOwnedDelegatedCrossing(s.Value.Span(), got)
		return true
	case *ast.ExprStmt:
		valueType := c.typeExpr(moduleName, s.Expr, scope, ctx)
		if ctx == ContextImagePhaseDirect {
			c.recordGraphFromExprStmt(moduleName, s.Expr, scope)
		} else {
			c.recordInterruptConfiguratorCall(moduleName, exprStmtCall(s.Expr), scope)
			c.recordReliableTryPublishCall(moduleName, s.Expr, scope, false)
			c.recordSubscriptionMethodCall(moduleName, s.Expr, scope)
		}
		return isNeverType(valueType)
	default:
		c.error(s.Span(), diag.CG0001, "unsupported statement")
	}
	return false
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
	constructedPaths := map[string]DriverPathNode{}
	for _, path := range c.graph.DriverPaths {
		constructedPaths[c.driverPathNodeKey(path)] = path
	}

	pathOwners := map[string]map[string]driverPathOwner{}
	addOwner := func(key string, owner driverPathOwner) bool {
		if key == "" {
			return false
		}
		if pathOwners[key] == nil {
			pathOwners[key] = map[string]driverPathOwner{}
		}
		ownerKey := fmt.Sprintf("%s:%d:%d", owner.executor, owner.span.Start, owner.span.End)
		if _, ok := pathOwners[key][ownerKey]; ok {
			return false
		}
		pathOwners[key][ownerKey] = owner
		return true
	}

	for _, exec := range c.graph.Executors {
		for _, field := range exec.Type.Fields {
			if field.Type == nil {
				continue
			}
			switch field.Type.Kind {
			case KindDriver:
				span := exec.FieldSpans[field.Name]
				if span.End == 0 {
					span = exec.Span
				}
				c.error(span, diag.SEM0010, "root driver "+field.Type.Name+" cannot be passed into executor "+exec.Type.Name)
			case KindDriverPath:
				use, ok := exec.PathUses[field.Name]
				if !ok || use.Key == "" {
					continue
				}
				addOwner(use.Key, driverPathOwner{executor: exec.Type.Name, span: use.spanOr(exec.Span)})
			}
		}
	}

	changed := true
	for changed {
		changed = false
		for parentKey, path := range constructedPaths {
			parentOwners := pathOwners[parentKey]
			if len(parentOwners) == 0 {
				continue
			}
			for _, use := range path.FieldUses {
				for _, owner := range parentOwners {
					if addOwner(use.Key, owner) {
						changed = true
					}
				}
			}
		}
	}

	for key, path := range constructedPaths {
		owners := pathOwners[key]
		if len(owners) == 1 {
			continue
		}
		name := path.Binding
		if name == "" {
			name = path.Type.Name
		}
		if len(owners) == 0 {
			continue
		}
		c.error(secondDriverPathOwnerSpan(owners, path.Span), diag.SEM0038, "driver path "+name+" is assigned to more than one executor")
	}
}

func (c *checker) checkExecutorTopicGraph() {
	c.checkDuplicateGraphLabels()
	c.checkSlotBindingsAndPlacements()
	c.checkSubscriptionSlotBindings()
	c.checkSharedPathUses()
	c.checkVcpuPlacements()
	c.checkPublisherBindings()
	c.checkSubscriptionUses()
	c.checkTopicPolicies()
	c.checkReliableTryPublishCalls()
	c.checkExecutorMemoryOwners()
	c.checkPublishingIdentities()
}

func (c *checker) checkSharedPathUses() {
	uses := map[string][]source.Span{}
	for _, exec := range c.graph.Executors {
		for _, use := range exec.PathUses {
			if use.Key == "" {
				continue
			}
			uses[use.Key] = append(uses[use.Key], nonZeroSpan(use.Span, exec.Span))
		}
	}
	for _, spans := range uses {
		if len(spans) > 1 {
			c.error(spans[1], diag.SEM0038, "driver path is assigned to more than one executor")
		}
	}
}

func (c *checker) checkDuplicateGraphLabels() {
	c.checkDuplicateLabels("executor slot", executorSlotLabels(c.graph.ExecutorSlots))
	c.checkDuplicateLabels("path", pathLabels(c.graph.Paths))
	c.checkDuplicateLabels("topic", topicLabels(c.graph.Topics))
	c.checkDuplicateGeneratedLabels("executor slot", executorSlotLabels(c.graph.ExecutorSlots))
	c.checkDuplicateGeneratedLabels("path", pathLabels(c.graph.Paths))
	c.checkDuplicateGeneratedLabels("topic", topicLabels(c.graph.Topics))
	c.checkCrossKindDuplicateLabels()
}

func (c *checker) checkDuplicateLabels(kind string, labels []graphLabel) {
	seen := map[string]source.Span{}
	for _, label := range labels {
		if label.value == "" {
			continue
		}
		if _, ok := seen[label.value]; ok {
			c.error(label.span, diag.SEM0033, "duplicate "+kind+" label "+label.value)
			continue
		}
		seen[label.value] = label.span
	}
}

func (c *checker) checkDuplicateGeneratedLabels(kind string, labels []graphLabel) {
	seen := map[string]graphLabel{}
	for _, label := range labels {
		if label.value == "" {
			continue
		}
		generated := graphGeneratedLabel(label.value)
		if previous, ok := seen[generated]; ok && previous.value != label.value {
			c.error(label.span, diag.SEM0033, "duplicate "+kind+" label "+label.value+" after symbol sanitization")
			continue
		}
		if _, ok := seen[generated]; !ok {
			seen[generated] = label
		}
	}
}

func graphGeneratedLabel(label string) string {
	var b strings.Builder
	for _, r := range label {
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	return b.String()
}

type graphLabel struct {
	value string
	span  source.Span
}

func executorSlotLabels(slots []ExecutorSlotNode) []graphLabel {
	out := make([]graphLabel, 0, len(slots))
	for _, slot := range slots {
		out = append(out, graphLabel{value: slot.Label, span: slot.Span})
	}
	return out
}

func pathLabels(paths []PathNode) []graphLabel {
	out := make([]graphLabel, 0, len(paths))
	for _, path := range paths {
		out = append(out, graphLabel{value: path.Label, span: path.Span})
	}
	return out
}

func topicLabels(topics []TopicNode) []graphLabel {
	out := make([]graphLabel, 0, len(topics))
	for _, topic := range topics {
		out = append(out, graphLabel{value: topic.Label, span: topic.Span})
	}
	return out
}

func (c *checker) checkCrossKindDuplicateLabels() {
	seen := map[string]string{}
	for _, group := range []struct {
		kind   string
		labels []graphLabel
	}{
		{kind: "executor slot", labels: executorSlotLabels(c.graph.ExecutorSlots)},
		{kind: "path", labels: pathLabels(c.graph.Paths)},
		{kind: "topic", labels: topicLabels(c.graph.Topics)},
	} {
		for _, label := range group.labels {
			if label.value == "" {
				continue
			}
			kind, ok := seen[label.value]
			if ok && kind != group.kind {
				c.error(label.span, diag.SEM0033, "duplicate graph label "+label.value)
				continue
			}
			if !ok {
				seen[label.value] = group.kind
			}
		}
	}
}

func (c *checker) checkSlotBindingsAndPlacements() {
	execCounts := map[string]int{}
	execSpans := map[string]source.Span{}
	for _, exec := range c.graph.Executors {
		if exec.Type != nil && exec.SlotLabel == "" {
			if span, ok := executorSlotFieldSpan(exec); ok {
				c.error(span, diag.SEM0035, "executor "+exec.Type.Name+" uses an unclaimed executor slot")
			}
		}
		if exec.SlotLabel == "" {
			continue
		}
		execCounts[exec.SlotLabel]++
		execSpans[exec.SlotLabel] = exec.Span
	}
	placementCounts := map[string]int{}
	placementSpans := map[string]source.Span{}
	for _, placement := range c.graph.VcpuPlacements {
		if placement.SlotLabel == "" {
			if placement.ExecutorBinding != "" {
				c.error(placement.Span, diag.SEM0035, "executor "+placement.ExecutorBinding+" must declare an ExecutorSlot field")
			}
			continue
		}
		placementCounts[placement.SlotLabel]++
		placementSpans[placement.SlotLabel] = placement.Span
	}
	for _, slot := range c.graph.ExecutorSlots {
		if slot.Label == "" {
			continue
		}
		if execCounts[slot.Label] != 1 {
			c.error(nonZeroSpan(execSpans[slot.Label], slot.Span), diag.SEM0034, "executor slot "+slot.Label+" must be bound to exactly one executor")
		}
		if placementCounts[slot.Label] != 1 {
			c.error(nonZeroSpan(placementSpans[slot.Label], slot.Span), diag.SEM0034, "executor slot "+slot.Label+" must be placed on exactly one vCPU")
		}
	}
}

func executorSlotFieldSpan(exec ExecutorNode) (source.Span, bool) {
	if exec.Type == nil {
		return source.Span{}, false
	}
	for _, field := range exec.Type.Fields {
		if IsExecutorSlotType(field.Type) {
			return nonZeroSpan(exec.FieldSpans[field.Name], exec.Span), true
		}
	}
	return source.Span{}, false
}

func (c *checker) checkSubscriptionSlotBindings() {
	subscriptions := map[string]TopicSubscriptionNode{}
	for _, sub := range c.graph.TopicSubscriptions {
		if sub.Binding != "" {
			subscriptions[sub.Binding] = sub
		}
	}
	for _, exec := range c.graph.Executors {
		for field, binding := range exec.FieldBindings {
			if !IsTopicSubscriptionType(exec.BoundTypes[field]) {
				continue
			}
			sub, ok := subscriptions[binding]
			if !ok || sub.SubscriberLabel == "" || exec.SlotLabel == "" || sub.SubscriberLabel == exec.SlotLabel {
				continue
			}
			span := exec.FieldSpans[field]
			c.error(nonZeroSpan(span, exec.Span), diag.SEM0035, "subscription "+binding+" belongs to slot "+sub.SubscriberLabel+" but executor "+exec.Type.Name+" uses slot "+exec.SlotLabel)
		}
	}
}

func (c *checker) checkVcpuPlacements() {
	byVcpu := map[int]VcpuPlacementNode{}
	hasPlacement := false
	hasBootstrapEnter := false
	var firstSpan source.Span
	for i, placement := range c.graph.VcpuPlacements {
		if !hasPlacement {
			firstSpan = placement.Span
		}
		hasPlacement = true
		if placement.VcpuID == 0 && placement.Terminal {
			hasBootstrapEnter = true
		}
		if i > 0 && c.graph.VcpuPlacements[i-1].Terminal {
			c.error(placement.Span, diag.SEM0036, "vCPU enter must be the final reachable statement in the phase")
		}
		if previous, ok := byVcpu[placement.VcpuID]; ok {
			c.error(nonZeroSpan(placement.Span, previous.Span), diag.SEM0037, fmt.Sprintf("vCPU %d starts more than one executor", placement.VcpuID))
			continue
		}
		if placement.VcpuID == 0 && !placement.Terminal {
			c.error(placement.Span, diag.SEM0036, "vCPU 0 must enter its executor")
		}
		if placement.VcpuID != 0 && placement.Terminal {
			c.error(placement.Span, diag.SEM0036, fmt.Sprintf("vCPU %d must start its executor", placement.VcpuID))
		}
		byVcpu[placement.VcpuID] = placement
	}
	if hasPlacement && !hasBootstrapEnter {
		c.error(firstSpan, diag.SEM0036, "vCPU 0 must enter its executor")
	}
}

func (c *checker) checkPublisherBindings() {
	uses := map[string][]source.Span{}
	publishers := map[string]TopicPublisherNode{}
	for _, pub := range c.graph.TopicPublishers {
		if pub.Binding != "" {
			publishers[pub.Binding] = pub
		}
	}
	topicUses := map[string][]source.Span{}
	for _, exec := range c.graph.Executors {
		for field, binding := range exec.FieldBindings {
			if binding == "" || !IsTopicPublisherType(exec.BoundTypes[field]) {
				continue
			}
			span := nonZeroSpan(exec.FieldSpans[field], exec.Span)
			uses[binding] = append(uses[binding], span)
			if pub := publishers[binding]; pub.TopicLabel != "" {
				topicUses[pub.TopicLabel] = append(topicUses[pub.TopicLabel], span)
			}
		}
	}
	for _, route := range c.graph.InterruptTopicRoutes {
		if route.TopicLabel != "" {
			topicUses[route.TopicLabel] = append(topicUses[route.TopicLabel], route.Span)
		}
	}
	for binding, spans := range uses {
		if len(spans) > 1 {
			c.error(spans[1], diag.SEM0039, "publisher "+binding+" is assigned to more than one producer field")
		}
	}
	for label, spans := range topicUses {
		if len(spans) > 1 {
			c.error(spans[1], diag.SEM0039, "topic "+label+" is assigned to more than one producer field")
		}
	}
}

func (c *checker) checkSubscriptionUses() {
	subscriptions := map[string]TopicSubscriptionNode{}
	for _, sub := range c.graph.TopicSubscriptions {
		if sub.Binding != "" {
			subscriptions[sub.Binding] = sub
		}
	}
	for _, use := range c.graph.SubscriptionUses {
		if use.FieldName == "" || use.CurrentExecutorType == "" {
			continue
		}
		if c.checkExecutorSubscriptionUse(use, subscriptions) {
			continue
		}
		c.error(use.Span, diag.SEM0040, "subscription field "+use.FieldName+" used from executor "+use.CurrentExecutorType)
	}
}

func (c *checker) checkExecutorSubscriptionUse(use SubscriptionUseNode, subscriptions map[string]TopicSubscriptionNode) bool {
	reported := false
	for _, exec := range c.graph.Executors {
		if exec.Type == nil || exec.Type.Name != use.CurrentExecutorType {
			continue
		}
		binding := exec.FieldBindings[use.FieldName]
		sub := subscriptions[binding]
		if sub.SubscriberLabel == "" || exec.SlotLabel == "" {
			continue
		}
		if sub.SubscriberLabel == exec.SlotLabel {
			return true
		}
		c.error(use.Span, diag.SEM0040, "subscription for slot "+sub.SubscriberLabel+" used from executor "+exec.Type.Name)
		reported = true
	}
	return reported
}

func (c *checker) checkTopicPolicies() {
	for _, exec := range c.graph.Executors {
		if exec.LoopPolicy == "EventSleepPolicy" && exec.SlotLabel != "" && !c.graph.HasWakeSource(exec.SlotLabel) {
			c.error(exec.Span, diag.SEM0044, "EventSleepPolicy executor "+exec.Type.Name+" has no wake source")
		}
		if exec.LoopPolicy != "NoGapRequiredPolicy" {
			continue
		}
		for _, sub := range c.graph.TopicSubscriptions {
			if sub.SubscriberLabel != exec.SlotLabel {
				continue
			}
			topic := c.graph.TopicByLabel(sub.TopicLabel)
			if strings.HasPrefix(topic.Kind, "gap") {
				c.error(nonZeroSpan(sub.Span, exec.Span), diag.SEM0041, "gap topic "+sub.TopicLabel+" requires a gap-tolerant executor policy")
			}
		}
	}
}

func (c *checker) checkReliableTryPublishCalls() {
	for _, call := range c.graph.ReliableTryPublishCalls {
		if !call.ResultObserved {
			c.error(call.Span, diag.SEM0045, "reliable try_publish result must be observed")
		}
	}
}

func (c *checker) checkExecutorMemoryOwners() {
	for _, exec := range c.graph.Executors {
		span, hasMemory := executorMemoryFieldSpan(exec)
		if !hasMemory {
			continue
		}
		if exec.SlotLabel != "" && exec.MemoryOwnerLabel == "" {
			c.error(span, diag.SEM0047, "executor "+exec.Type.Name+" memory must be claimed for slot "+exec.SlotLabel)
			continue
		}
		if exec.SlotLabel == "" || exec.MemoryOwnerLabel == "" || exec.SlotLabel == exec.MemoryOwnerLabel {
			continue
		}
		c.error(span, diag.SEM0047, "executor "+exec.Type.Name+" memory is owned by "+exec.MemoryOwnerLabel+" but slot is "+exec.SlotLabel)
	}
}

func executorMemoryFieldSpan(exec ExecutorNode) (source.Span, bool) {
	if exec.Type == nil {
		return source.Span{}, false
	}
	for _, field := range exec.Type.Fields {
		if qualifiedTypeName(field.Type) == "machine.x86_64.executor_memory.ExecutorMemory" {
			return nonZeroSpan(exec.FieldSpans[field.Name], exec.Span), true
		}
	}
	return source.Span{}, false
}

func (c *checker) checkPublishingIdentities() {
	for _, path := range c.graph.Paths {
		if path.PublishesInterrupts && path.Label == "" {
			c.error(path.Span, diag.SEM0048, "publishing path "+path.Binding+" is missing identity")
		}
	}
	for _, path := range c.graph.Paths {
		if !path.PublishesInterrupts || path.Label == "" {
			continue
		}
		hasRoute := false
		for _, route := range c.graph.InterruptTopicRoutes {
			if route.PathBinding == path.Binding {
				hasRoute = true
				break
			}
		}
		if !hasRoute {
			c.error(path.Span, diag.SEM0048, "publishing topic "+path.Binding+" is missing identity")
		}
	}
	for _, pub := range c.graph.TopicPublishers {
		if pub.TopicLabel != "" {
			continue
		}
		name := pub.Binding
		if name == "" {
			name = "topic"
		}
		c.error(pub.Span, diag.SEM0048, "publishing topic "+name+" is missing identity")
	}
}

func nonZeroSpan(span, fallback source.Span) source.Span {
	if span.End == 0 {
		return fallback
	}
	return span
}

func subscriberSlotsForTopic(subscriptions []TopicSubscriptionNode, label string) []string {
	var slots []string
	seen := map[string]bool{}
	for _, sub := range subscriptions {
		if sub.TopicLabel != label || sub.SubscriberLabel == "" || seen[sub.SubscriberLabel] {
			continue
		}
		seen[sub.SubscriberLabel] = true
		slots = append(slots, sub.SubscriberLabel)
	}
	return slots
}

func interruptContextSymbol(pathLabel string) string {
	if pathLabel == "" {
		return ""
	}
	replacer := strings.NewReplacer(".", "_", "-", "_", "/", "_", " ", "_")
	return "_wrela_interrupt_context_" + replacer.Replace(pathLabel)
}

func (c *checker) eventDeclForPath(path *Type) *ast.InterruptEventDecl {
	if path == nil {
		return nil
	}
	return c.index.InterruptEvent(path.Module, path.Name)
}

func qualifiedTypeName(typ *Type) string {
	if typ == nil {
		return ""
	}
	if typ.Module == "" || typ.Module == "builtin" {
		return typ.Name
	}
	return typ.Module + "." + typ.Name
}

func isTrustedHardwareAuthorityModule(moduleName string) bool {
	switch {
	case strings.HasPrefix(moduleName, "platform.uefi."):
		return true
	case strings.HasPrefix(moduleName, "platform.acpi."):
		return true
	case strings.HasPrefix(moduleName, "platform.hardware."):
		return true
	case moduleName == "machine.x86_64.interrupts":
		return true
	case moduleName == "machine.x86_64.pci":
		return true
	case moduleName == "machine.x86_64.cpu_state":
		return true
	case strings.HasPrefix(moduleName, "sem."):
		return true
	}
	return false
}

func isHardwareAuthorityType(typ *Type) bool {
	if typ == nil {
		return false
	}
	switch qualifiedTypeName(typ) {
	case "platform.hardware.bytes.BoundedBytes",
		"platform.hardware.bytes.PhysicalBytes",
		"platform.hardware.bytes.MmioRegion",
		"platform.hardware.bytes.IoPortRegion",
		"platform.acpi.root.AcpiRoot",
		"platform.acpi.tables.AcpiTable",
		"platform.acpi.madt.MadtTable",
		"platform.acpi.mcfg.McfgTable",
		"machine.x86_64.interrupts.LocalApic",
		"machine.x86_64.interrupts.IoApicDiscovered",
		"machine.x86_64.interrupts.IoApicSet",
		"machine.x86_64.interrupts.InterruptOverrideSet",
		"machine.x86_64.interrupts.InterruptAuthority",
		"machine.x86_64.interrupts.IoApicRoute",
		"machine.x86_64.pci.PciDevice",
		"machine.x86_64.pci.PciDeviceIdentity",
		"machine.x86_64.pci.PcieEcamWindow",
		"machine.x86_64.pci.PcieEcamWindows",
		"machine.x86_64.pci.PciDeviceSet",
		"machine.x86_64.pci.MsiCapability",
		"machine.x86_64.pci.MsixCapability",
		"machine.x86_64.cpu_state.Vcpu":
		return true
	}
	return false
}

func secondDriverPathOwnerSpan(owners map[string]driverPathOwner, fallback source.Span) source.Span {
	out := fallback
	for _, owner := range owners {
		if owner.span.Start >= out.Start {
			out = owner.span
		}
	}
	return out
}

func driverPathSpanKey(span source.Span) string {
	return fmt.Sprintf("span:%d:%d", span.Start, span.End)
}

func (c *checker) driverPathExprKey(expr ast.Expr, scope *Scope) string {
	switch e := expr.(type) {
	case *ast.NameExpr:
		if key, ok := scope.LookupDriverPath(e.Name); ok {
			return key
		}
		return ""
	case *ast.FieldExpr:
		return c.driverPathFieldExprKey(e, scope)
	}
	return driverPathSpanKey(expr.Span())
}

func (c *checker) driverPathFieldExprKey(expr *ast.FieldExpr, scope *Scope) string {
	if named, ok := expr.Base.(*ast.NameExpr); ok {
		if key, ok := scope.LookupDriverPathField(named.Name, expr.Field); ok {
			return key
		}
	}
	return ""
}

func (c *checker) driverPathNodeKey(path DriverPathNode) string {
	return driverPathSpanKey(path.Span)
}

func (c *checker) bindDriverPath(name string, expr ast.Expr, scope *Scope) {
	key := c.driverPathExprKey(expr, scope)
	scope.DefineDriverPath(name, key)
	if _, ok := expr.(*ast.NameExpr); ok {
		return
	}

	for i := range c.graph.DriverPaths {
		if driverPathSpanKey(c.graph.DriverPaths[i].Span) == key {
			c.graph.DriverPaths[i].Binding = name
			return
		}
	}
}

func (c *checker) driverPathFieldKeysForExpr(moduleName string, expr ast.Expr, scope *Scope) map[string]string {
	switch e := expr.(type) {
	case *ast.NameExpr:
		if fields, ok := scope.LookupDriverPathFields(e.Name); ok {
			return copyDriverPathFields(fields)
		}
	case *ast.ConstructorExpr:
		return c.driverPathFieldKeysForConstructor(moduleName, e, scope)
	case *ast.CallExpr:
		if c.callReturnsReceiverType(moduleName, e, scope) {
			return c.driverPathFieldKeysForExpr(moduleName, e.Receiver, scope)
		}
	}
	return nil
}

func copyDriverPathFields(fields map[string]string) map[string]string {
	copied := map[string]string{}
	for field, key := range fields {
		copied[field] = key
	}
	return copied
}

func (c *checker) driverPathFieldKeysForConstructor(moduleName string, expr *ast.ConstructorExpr, scope *Scope) map[string]string {
	constructed := c.resolveType(moduleName, expr.Type)
	if constructed == nil {
		return nil
	}
	fields := map[string]string{}
	for _, arg := range expr.Args {
		field, _, _ := c.lookupFieldForArg(constructed, arg.Name)
		if field == nil || field.Type == nil || field.Type.Kind != KindDriverPath {
			continue
		}
		if key := c.driverPathExprKey(arg.Value, scope); key != "" {
			fields[arg.Name] = key
		}
	}
	return fields
}

func (c *checker) callReturnsReceiverType(moduleName string, expr *ast.CallExpr, scope *Scope) bool {
	receiverType := c.exprStaticType(moduleName, expr.Receiver, scope)
	method, _ := c.lookupMethod(receiverType, expr.Method, expr.SpanV)
	return receiverType != nil && method != nil && method.Return == receiverType
}

func (c *checker) exprStaticType(moduleName string, expr ast.Expr, scope *Scope) *Type {
	switch e := expr.(type) {
	case *ast.NameExpr:
		if scope != nil {
			if typ, ok := scope.Lookup(e.Name); ok {
				return typ
			}
		}
		return c.resolveType(moduleName, e.Name)
	case *ast.ConstructorExpr:
		return c.resolveType(moduleName, e.Type)
	case *ast.FieldExpr:
		baseType := c.exprStaticType(moduleName, e.Base, scope)
		if baseType == nil {
			return nil
		}
		for _, field := range baseType.Fields {
			if field.Name == e.Field {
				return field.Type
			}
		}
	case *ast.CallExpr:
		receiverType := c.exprStaticType(moduleName, e.Receiver, scope)
		method, _ := c.lookupMethod(receiverType, e.Method, e.SpanV)
		if method != nil {
			return method.Return
		}
	}
	return nil
}

func stringLiteralArg(expr *ast.ConstructorExpr, name string) (string, bool) {
	for _, arg := range expr.Args {
		if arg.Name != name {
			continue
		}
		lit, ok := arg.Value.(*ast.StringLiteral)
		if !ok {
			return "", false
		}
		return lit.Value, true
	}
	return "", false
}

func namedArgExpr(args []ast.NamedArg, name string) ast.Expr {
	for _, arg := range args {
		if arg.Name == name {
			return arg.Value
		}
	}
	return nil
}

func literalArgKey(expr *ast.CallExpr, name string) string {
	for _, arg := range expr.Args {
		if arg.Name != name {
			continue
		}
		if lit, ok := arg.Value.(*ast.IntLiteral); ok {
			return strings.ToLower(lit.Value)
		}
		return "<nonliteral>"
	}
	return "<missing>"
}

func pciDeviceKeyFromRequireDevice(call *ast.CallExpr) string {
	vendor := literalArgKey(call, "vendor_id")
	device := literalArgKey(call, "device_id")
	occurrence := literalArgKey(call, "occurrence")
	if vendor == "<missing>" || device == "<missing>" || occurrence == "<missing>" {
		return ""
	}
	if vendor == "<nonliteral>" || device == "<nonliteral>" || occurrence == "<nonliteral>" {
		return ""
	}
	return "vendor=" + vendor + "/device=" + device + "/occurrence=" + occurrence
}

func pciOriginKey(receiver ast.Expr, scope *Scope) (string, bool) {
	switch r := receiver.(type) {
	case *ast.NameExpr:
		if scope == nil {
			return "", false
		}
		if origin, ok := scope.LookupOrigin(r.Name); ok && origin.PciDeviceKey != "" {
			return origin.PciDeviceKey, true
		}
	case *ast.CallExpr:
		if r.Method == "require_device" {
			if key := pciDeviceKeyFromRequireDevice(r); key != "" {
				return key, true
			}
		}
	}
	return "", false
}

func interruptVectorArgKey(expr *ast.CallExpr) string {
	arg := namedArgExpr(expr.Args, "vector")
	cons, ok := arg.(*ast.ConstructorExpr)
	if !ok || cons.Type != "InterruptVector" {
		return "<nonliteral>"
	}
	for _, named := range cons.Args {
		if named.Name == "value" {
			if lit, ok := named.Value.(*ast.IntLiteral); ok {
				return strings.ToLower(lit.Value)
			}
		}
	}
	return "<missing>"
}

func (c *checker) originForExprValue(moduleName string, expr ast.Expr, valueType *Type, scope *Scope) localOrigin {
	switch e := expr.(type) {
	case *ast.ConstructorExpr:
		if valueType == nil {
			valueType = c.exprStaticType(moduleName, expr, scope)
		}
		return c.originForConstructor(moduleName, e, valueType, scope)
	case *ast.CallExpr:
		if valueType == nil {
			valueType = c.exprStaticType(moduleName, expr, scope)
		}
		return c.originForCall(moduleName, e, valueType, scope)
	case *ast.NameExpr:
		if origin, ok := scope.LookupOrigin(e.Name); ok {
			return origin
		}
	}
	return localOrigin{Type: valueType}
}

func (c *checker) originForLetValue(moduleName string, expr ast.Expr, valueType *Type, scope *Scope) localOrigin {
	return c.originForExprValue(moduleName, expr, valueType, scope)
}

func (c *checker) originForConstructor(moduleName string, expr *ast.ConstructorExpr, typ *Type, scope *Scope) localOrigin {
	_ = moduleName
	origin := localOrigin{
		Type:          typ,
		Constructor:   expr,
		FieldBindings: map[string]string{},
	}
	for _, arg := range expr.Args {
		if named, ok := arg.Value.(*ast.NameExpr); ok {
			origin.FieldBindings[arg.Name] = named.Name
		}
	}
	if typ == nil {
		return origin
	}
	if IsTopicType(typ) {
		origin.TopicKind = topicKindForType(typ)
		origin.TopicDepth = 64
		if identity := constructorArg(expr, "identity"); identity != nil {
			if identityConstructor, ok := identity.(*ast.ConstructorExpr); ok {
				origin.TopicLabel, _ = stringLiteralArg(identityConstructor, "label")
			}
		}
		if depth := constructorArg(expr, "depth"); depth != nil {
			if value, ok := unsignedIntegerLiteral(depth); ok {
				origin.TopicDepth = value
			} else {
				origin.TopicDepth = 0
			}
		}
	}
	if typ.Kind == KindDriverPath {
		if identity := constructorArg(expr, "identity"); identity != nil {
			if identityConstructor, ok := identity.(*ast.ConstructorExpr); ok {
				origin.PathLabel, _ = stringLiteralArg(identityConstructor, "label")
			}
		}
		origin.TopicKind, origin.EventType, origin.EventFunctionSymbol = pathRouteMetadata(typ)
		if publisher := c.pathPublisherOrigin(moduleName, expr, scope); publisher.Type != nil {
			origin.PublishesInterrupts = true
			origin.TopicLabel = publisher.TopicLabel
			origin.TopicKind = publisher.TopicKind
		}
	}
	if typ.Kind == KindExecutor {
		origin.SlotLabel = c.slotLabelForExpr(moduleName, constructorArg(expr, "slot"), scope)
		origin.MemoryOwnerLabel = c.slotLabelForExpr(moduleName, constructorArg(expr, "memory"), scope)
		if loopType := c.exprStaticType(moduleName, constructorArg(expr, "loop"), scope); loopType != nil {
			origin.LoopPolicy = loopType.Name
		}
	}
	return origin
}

func (c *checker) originForCall(moduleName string, expr *ast.CallExpr, valueType *Type, scope *Scope) localOrigin {
	origin := localOrigin{Type: valueType}
	receiverType := c.exprStaticType(moduleName, expr.Receiver, scope)
	if receiverType == nil {
		return origin
	}
	switch {
	case receiverType.Module == "machine.x86_64.cpu_state" && receiverType.Name == "ExecutorRegistry" && expr.Method == "claim":
		identity := namedArgExpr(expr.Args, "identity")
		if cons, ok := identity.(*ast.ConstructorExpr); ok {
			origin.SlotLabel, _ = stringLiteralArg(cons, "label")
		}
	case receiverType.Module == "machine.x86_64.cpu_state" && receiverType.Name == "OwnedMemory" && expr.Method == "claim_executor_arena":
		origin.SlotLabel = c.slotLabelForExpr(moduleName, namedArgExpr(expr.Args, "owner"), scope)
	case receiverType.Module == "machine.x86_64.pci" &&
		receiverType.Name == "PciDeviceSet" &&
		expr.Method == "require_device":
		origin.PciDeviceKey = pciDeviceKeyFromRequireDevice(expr)
	case IsTopicType(receiverType) && expr.Method == "subscribe":
		receiverOrigin := c.originForExprValue(moduleName, expr.Receiver, receiverType, scope)
		origin.TopicLabel = receiverOrigin.TopicLabel
		origin.TopicKind = receiverOrigin.TopicKind
		origin.SlotLabel = c.slotLabelForExpr(moduleName, namedArgExpr(expr.Args, "subscriber"), scope)
	case IsTopicType(receiverType) && expr.Method == "publisher":
		receiverOrigin := c.originForExprValue(moduleName, expr.Receiver, receiverType, scope)
		origin.TopicLabel = receiverOrigin.TopicLabel
		origin.TopicKind = receiverOrigin.TopicKind
	case receiverType.Module == "machine.x86_64.serial" && receiverType.Name == "SerialDriver" && expr.Method == "create_console_path" && valueType != nil && valueType.Kind == KindDriverPath:
		origin.FieldBindings = map[string]string{}
		if identity := namedArgExpr(expr.Args, "identity"); identity != nil {
			if identityConstructor, ok := identity.(*ast.ConstructorExpr); ok {
				origin.PathLabel, _ = stringLiteralArg(identityConstructor, "label")
			}
		}
		origin.TopicKind, origin.EventType, origin.EventFunctionSymbol = pathRouteMetadata(valueType)
		if publisher := c.publisherOriginForArg(moduleName, namedArgExpr(expr.Args, "rx"), scope); publisher.Type != nil {
			origin.PublishesInterrupts = true
			origin.TopicLabel = publisher.TopicLabel
			origin.TopicKind = publisher.TopicKind
		}
	case IsVcpuType(receiverType) && (expr.Method == "start" || expr.Method == "enter"):
		origin = c.vcpuOrigin(expr, scope)
		origin.Type = valueType
	}
	return origin
}

func (c *checker) pathPublisherOrigin(moduleName string, expr *ast.ConstructorExpr, scope *Scope) localOrigin {
	if origin := c.publisherOriginForArg(moduleName, constructorArg(expr, "rx"), scope); origin.Type != nil {
		return origin
	}
	if origin := c.publisherOriginForArg(moduleName, constructorArg(expr, "irq"), scope); origin.Type != nil {
		return origin
	}
	return c.publisherOriginForArg(moduleName, constructorArg(expr, "interrupt"), scope)
}

func (c *checker) publisherOriginForArg(moduleName string, expr ast.Expr, scope *Scope) localOrigin {
	switch e := expr.(type) {
	case *ast.NameExpr:
		return originForExpr(e, scope)
	case *ast.CallExpr:
		if e.Method == "publisher" {
			receiverType := c.exprStaticType(moduleName, e.Receiver, scope)
			return c.originForExprValue(moduleName, e.Receiver, receiverType, scope)
		}
	}
	return localOrigin{}
}

func (c *checker) recordGraphFromLet(name string, origin localOrigin, span source.Span) {
	if origin.Type == nil {
		return
	}
	switch {
	case origin.HasVcpuID:
		c.graph.VcpuPlacements = append(c.graph.VcpuPlacements, VcpuPlacementNode{
			VcpuID:          origin.VcpuID,
			ExecutorBinding: origin.ExecutorBinding,
			SlotLabel:       origin.SlotLabel,
			Terminal:        origin.TerminalVcpu,
			Span:            span,
		})
	case IsExecutorSlotType(origin.Type):
		c.graph.ExecutorSlots = append(c.graph.ExecutorSlots, ExecutorSlotNode{Label: origin.SlotLabel, Binding: name, Span: span})
	case IsTopicType(origin.Type):
		c.graph.Topics = append(c.graph.Topics, TopicNode{Label: origin.TopicLabel, Kind: origin.TopicKind, Depth: origin.TopicDepth, Binding: name, Span: span})
	case IsTopicPublisherType(origin.Type):
		c.graph.TopicPublishers = append(c.graph.TopicPublishers, TopicPublisherNode{TopicLabel: origin.TopicLabel, Binding: name, Span: span})
	case IsTopicSubscriptionType(origin.Type):
		c.graph.TopicSubscriptions = append(c.graph.TopicSubscriptions, TopicSubscriptionNode{TopicLabel: origin.TopicLabel, SubscriberLabel: origin.SlotLabel, Binding: name, Span: span})
	case origin.Type.Kind == KindDriverPath:
		publishes := origin.PublishesInterrupts || origin.FieldBindings["rx"] != "" || origin.FieldBindings["irq"] != "" || origin.FieldBindings["interrupt"] != "" || origin.TopicLabel != ""
		c.graph.Paths = append(c.graph.Paths, PathNode{Label: origin.PathLabel, Kind: origin.TopicKind, Binding: name, PublishesInterrupts: publishes, Span: span})
		if origin.EventType != "" && origin.TopicLabel != "" {
			c.graph.InterruptTopicRoutes = append(c.graph.InterruptTopicRoutes, InterruptTopicRouteNode{
				PathLabel:           origin.PathLabel,
				PathBinding:         name,
				ContextSymbol:       interruptContextSymbol(origin.PathLabel),
				TopicLabel:          origin.TopicLabel,
				TopicKind:           origin.TopicKind,
				EventType:           origin.EventType,
				EventFunctionSymbol: origin.EventFunctionSymbol,
				Span:                span,
			})
		}
	case origin.Type.Kind == KindExecutor:
		c.updateExecutorGraphNode(origin, span)
	}
}

func (c *checker) finalizeInterruptTopicRoutes() {
	for i := range c.graph.InterruptTopicRoutes {
		route := &c.graph.InterruptTopicRoutes[i]
		route.SubscriberSlots = subscriberSlotsForTopic(c.graph.TopicSubscriptions, route.TopicLabel)
		for _, configurator := range c.graph.InterruptConfigurators {
			if configurator.TopicKind == route.TopicKind {
				route.Vector = configurator.Vector
			}
		}
	}
}

func (c *checker) recordGraphFromExprStmt(moduleName string, expr ast.Expr, scope *Scope) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return
	}
	c.recordInterruptConfiguratorCall(moduleName, call, scope)
	if origin := c.vcpuOrigin(call, scope); origin.HasVcpuID {
		c.graph.VcpuPlacements = append(c.graph.VcpuPlacements, VcpuPlacementNode{
			VcpuID:          origin.VcpuID,
			ExecutorBinding: origin.ExecutorBinding,
			SlotLabel:       origin.SlotLabel,
			Terminal:        origin.TerminalVcpu,
			Span:            call.SpanV,
		})
	}
}

func (c *checker) recordInterruptConfiguratorCall(moduleName string, call *ast.CallExpr, scope *Scope) {
	if call == nil {
		return
	}
	receiverType := c.exprStaticType(moduleName, call.Receiver, scope)
	topicKind, vector, ok := interruptConfiguratorVector(receiverType, call)
	if !ok {
		return
	}
	c.graph.InterruptConfigurators = append(c.graph.InterruptConfigurators, InterruptConfiguratorNode{
		TopicKind: topicKind,
		Vector:    vector,
		Span:      call.SpanV,
	})
}

func (c *checker) recordHardwareClaimCall(moduleName string, call *ast.CallExpr, scope *Scope, ctx ContextKind) {
	if call == nil || (ctx != ContextImagePhaseDirect && isTrustedHardwareAuthorityModule(moduleName)) {
		return
	}
	receiverType := c.exprStaticType(moduleName, call.Receiver, scope)
	switch {
	case qualifiedTypeName(receiverType) == "machine.x86_64.interrupts.InterruptAuthority" && call.Method == "route_isa_irq":
		c.graph.HardwareClaims = append(c.graph.HardwareClaims, HardwareClaimNode{Kind: "isa_irq", Key: literalArgKey(call, "irq"), Span: call.SpanV})
		vectorKey := interruptVectorArgKey(call)
		if strings.HasPrefix(vectorKey, "<") {
			c.error(call.SpanV, diag.SEM0053, "interrupt vectors in hardware claims must be source literals")
			return
		}
		c.graph.HardwareClaims = append(c.graph.HardwareClaims, HardwareClaimNode{Kind: "interrupt_vector", Key: vectorKey, Span: call.SpanV})
	case qualifiedTypeName(receiverType) == "machine.x86_64.pci.PciDevice" && (call.Method == "claim_mmio_bar" || call.Method == "claim_io_bar"):
		key, ok := pciOriginKey(call.Receiver, scope)
		if !ok {
			c.error(call.SpanV, diag.SEM0054, "PCI claims must be made from discovered PciDevice values")
			return
		}
		c.graph.HardwareClaims = append(c.graph.HardwareClaims, HardwareClaimNode{Kind: "pci_bar", Key: key + "." + literalArgKey(call, "index"), Span: call.SpanV})
	case qualifiedTypeName(receiverType) == "machine.x86_64.pci.PciDevice" && call.Method == "claim_msi":
		key, ok := pciOriginKey(call.Receiver, scope)
		if !ok {
			c.error(call.SpanV, diag.SEM0054, "PCI claims must be made from discovered PciDevice values")
			return
		}
		c.graph.HardwareClaims = append(c.graph.HardwareClaims, HardwareClaimNode{Kind: "pci_msi", Key: key, Span: call.SpanV})
	case qualifiedTypeName(receiverType) == "machine.x86_64.pci.PciDevice" && call.Method == "claim_msix":
		key, ok := pciOriginKey(call.Receiver, scope)
		if !ok {
			c.error(call.SpanV, diag.SEM0054, "PCI claims must be made from discovered PciDevice values")
			return
		}
		c.graph.HardwareClaims = append(c.graph.HardwareClaims, HardwareClaimNode{Kind: "pci_bar", Key: key + "." + literalArgKey(call, "table_bar_index"), Span: call.SpanV})
		c.graph.HardwareClaims = append(c.graph.HardwareClaims, HardwareClaimNode{Kind: "pci_msix", Key: key, Span: call.SpanV})
	case (qualifiedTypeName(receiverType) == "machine.x86_64.pci.MsiCapability" && call.Method == "route") ||
		(qualifiedTypeName(receiverType) == "machine.x86_64.pci.MsixCapability" && call.Method == "route_entry"):
		vectorKey := interruptVectorArgKey(call)
		if strings.HasPrefix(vectorKey, "<") {
			c.error(call.SpanV, diag.SEM0053, "interrupt vectors in hardware claims must be source literals")
			return
		}
		c.graph.HardwareClaims = append(c.graph.HardwareClaims, HardwareClaimNode{Kind: "interrupt_vector", Key: vectorKey, Span: call.SpanV})
	}
}

func (c *checker) checkHardwareClaims() {
	seen := map[string]source.Span{}
	for _, claim := range c.graph.HardwareClaims {
		key := claim.Kind + ":" + claim.Key
		if prev, ok := seen[key]; ok {
			_ = prev
			c.error(claim.Span, diag.SEM0050, "duplicate hardware claim "+key)
			continue
		}
		seen[key] = claim.Span
	}
}

func interruptConfiguratorVector(receiverType *Type, call *ast.CallExpr) (string, int, bool) {
	switch qualifiedTypeName(receiverType) + "::" + call.Method {
	case "machine.x86_64.interrupts.ApicInterruptController::initialize_for_com1_receive":
		return "serial_rx", 0x40, true
	case "machine.x86_64.pci.MsiCapability::route":
		if vector, ok := interruptVectorValueArg(call); ok {
			return "edu_interrupt", vector, true
		}
		return "edu_interrupt", 0, true
	case "machine.x86_64.pci.MsixCapability::route_entry":
		if vector, ok := interruptVectorValueArg(call); ok {
			return "ivshmem_doorbell", vector, true
		}
		return "ivshmem_doorbell", 0, true
	default:
		return "", 0, false
	}
}

func interruptVectorValueArg(call *ast.CallExpr) (int, bool) {
	arg := namedArgExpr(call.Args, "vector")
	cons, ok := arg.(*ast.ConstructorExpr)
	if !ok || cons.Type != "InterruptVector" {
		return 0, false
	}
	value := constructorArg(cons, "value")
	literal, ok := value.(*ast.IntLiteral)
	if !ok {
		return 0, false
	}
	parsed, err := strconv.ParseInt(literal.Value, 0, 32)
	if err != nil {
		return 0, false
	}
	return int(parsed), true
}

func exprStmtCall(expr ast.Expr) *ast.CallExpr {
	call, _ := expr.(*ast.CallExpr)
	return call
}

func (c *checker) recordReliableTryPublishCall(moduleName string, expr ast.Expr, scope *Scope, observed bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok || call.Method != "try_publish" {
		return
	}
	receiverType := c.exprStaticType(moduleName, call.Receiver, scope)
	if qualifiedTypeName(receiverType) != "machine.x86_64.topic_u64.U64ReliablePublisher" {
		return
	}
	c.graph.ReliableTryPublishCalls = append(c.graph.ReliableTryPublishCalls, ReliableTryPublishCallNode{ResultObserved: observed, Span: call.SpanV})
}

func (c *checker) recordSubscriptionMethodCall(moduleName string, expr ast.Expr, scope *Scope) {
	call, ok := expr.(*ast.CallExpr)
	if !ok || c.currentType == nil || c.currentType.Kind != KindExecutor {
		return
	}
	receiverType := c.exprStaticType(moduleName, call.Receiver, scope)
	if !IsTopicSubscriptionType(receiverType) {
		return
	}
	field, ok := call.Receiver.(*ast.FieldExpr)
	if !ok {
		return
	}
	base, ok := field.Base.(*ast.NameExpr)
	if !ok || base.Name != "self" {
		return
	}
	c.graph.SubscriptionUses = append(c.graph.SubscriptionUses, SubscriptionUseNode{
		FieldName:           field.Field,
		CurrentExecutorType: c.currentType.Name,
		Span:                call.SpanV,
	})
}

func (c *checker) updateExecutorGraphNode(origin localOrigin, span source.Span) {
	for i := range c.graph.Executors {
		if c.graph.Executors[i].Span.Start == origin.Constructor.SpanV.Start && c.graph.Executors[i].Span.End == origin.Constructor.SpanV.End {
			c.graph.Executors[i].SlotLabel = origin.SlotLabel
			c.graph.Executors[i].LoopPolicy = origin.LoopPolicy
			c.graph.Executors[i].MemoryOwnerLabel = origin.MemoryOwnerLabel
			return
		}
	}
	c.graph.Executors = append(c.graph.Executors, ExecutorNode{
		Type:             origin.Type,
		Span:             span,
		FieldBindings:    origin.FieldBindings,
		SlotLabel:        origin.SlotLabel,
		LoopPolicy:       origin.LoopPolicy,
		MemoryOwnerLabel: origin.MemoryOwnerLabel,
	})
}

func syntheticExecutorFieldBinding(expr *ast.ConstructorExpr, field string, span source.Span) string {
	if expr != nil {
		return fmt.Sprintf("__executor_field_%d_%d_%s", expr.SpanV.Start, span.Start, field)
	}
	return fmt.Sprintf("__executor_field_%d_%s", span.Start, field)
}

func (c *checker) typeVcpuIntrinsicCall(moduleName string, expr *ast.CallExpr, scope *Scope, ctx ContextKind) {
	if executor := vcpuExecutorArg(expr.Args); executor != nil {
		c.typeExpr(moduleName, executor, scope, ctx)
		return
	}
	c.error(expr.SpanV, diag.CG0001, "missing call argument executor")
}

func constructorArg(expr *ast.ConstructorExpr, name string) ast.Expr {
	if expr == nil {
		return nil
	}
	for _, arg := range expr.Args {
		if arg.Name == name {
			return arg.Value
		}
	}
	return nil
}

func originForExpr(expr ast.Expr, scope *Scope) localOrigin {
	switch e := expr.(type) {
	case *ast.NameExpr:
		if origin, ok := scope.LookupOrigin(e.Name); ok {
			return origin
		}
	}
	return localOrigin{}
}

func (c *checker) slotLabelForExpr(moduleName string, expr ast.Expr, scope *Scope) string {
	origin := c.originForExprValue(moduleName, expr, c.exprStaticType(moduleName, expr, scope), scope)
	return origin.SlotLabel
}

func executorBindingForCall(call *ast.CallExpr) string {
	if named, ok := vcpuExecutorArg(call.Args).(*ast.NameExpr); ok {
		return named.Name
	}
	return ""
}

func (c *checker) vcpuOrigin(call *ast.CallExpr, scope *Scope) localOrigin {
	if call == nil || (call.Method != "start" && call.Method != "enter") {
		return localOrigin{}
	}
	vcpuID, ok := vcpuIDForReceiver(call.Receiver)
	if !ok {
		return localOrigin{}
	}
	executorName, ok := vcpuExecutorArg(call.Args).(*ast.NameExpr)
	if !ok {
		return localOrigin{VcpuID: vcpuID, HasVcpuID: true, TerminalVcpu: call.Method == "enter"}
	}
	execOrigin, ok := scope.LookupOrigin(executorName.Name)
	if !ok {
		return localOrigin{VcpuID: vcpuID, HasVcpuID: true, ExecutorBinding: executorName.Name, TerminalVcpu: call.Method == "enter"}
	}
	return localOrigin{VcpuID: vcpuID, HasVcpuID: true, ExecutorBinding: executorName.Name, SlotLabel: execOrigin.SlotLabel, TerminalVcpu: call.Method == "enter"}
}

func vcpuExecutorArg(args []ast.NamedArg) ast.Expr {
	if expr := namedArgExpr(args, "executor"); expr != nil {
		return expr
	}
	if len(args) == 1 && args[0].Name == "" {
		return args[0].Value
	}
	return nil
}

func vcpuIDForReceiver(expr ast.Expr) (int, bool) {
	field, ok := expr.(*ast.FieldExpr)
	if !ok {
		return 0, false
	}
	switch field.Field {
	case "vcpu0":
		return 0, true
	case "vcpu1":
		return 1, true
	default:
		return 0, false
	}
}

func topicKindForType(typ *Type) string {
	switch qualifiedTypeName(typ) {
	case "machine.x86_64.topic_u64.U64GapTopic":
		return "gap_u64"
	case "machine.x86_64.topic_u64.U64ReliableTopic":
		return "reliable_u64"
	case "machine.x86_64.serial.SerialRxTopic":
		return "serial_rx"
	case "machine.x86_64.edu.EduInterruptTopic":
		return "edu_interrupt"
	case "machine.x86_64.ivshmem.IvshmemDoorbellTopic":
		return "ivshmem_doorbell"
	default:
		return ""
	}
}

func pathRouteMetadata(typ *Type) (kind, eventType, eventFunctionSymbol string) {
	switch qualifiedTypeName(typ) {
	case "machine.x86_64.serial.SerialConsolePath":
		return "serial_rx", "machine.x86_64.serial.SerialPathInterrupt", "_wrela_event_fn_machine_x86_64_serial_SerialConsolePath_interrupt"
	case "machine.x86_64.edu.EduMsiPath":
		return "edu_interrupt", "machine.x86_64.edu.EduInterrupt", "_wrela_event_fn_machine_x86_64_edu_EduMsiPath_interrupt"
	case "machine.x86_64.ivshmem.IvshmemDoorbellPath":
		return "ivshmem_doorbell", "machine.x86_64.ivshmem.IvshmemDoorbellInterrupt", "_wrela_event_fn_machine_x86_64_ivshmem_IvshmemDoorbellPath_interrupt"
	default:
		return "", "", ""
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
		fieldType := c.lookupField(baseType, e.Field, e.SpanV)
		if fieldType != nil && !typeCanCarryHiddenLifetime(fieldType) && !typeCanCarryHiddenLifetime(baseType) {
			c.rememberLifetime(e, Lifetime{Kind: LifetimeExecutorRoot})
		}
		return fieldType
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
		lifetime := c.combineLifetime(e.SpanV, c.lifetimeOfExpr(e.Left, scope), c.lifetimeOfExpr(e.Right, scope))
		if isComparisonOp(e.Op) {
			c.requireSame(left, right, e.SpanV)
			c.rememberLifetime(e, Lifetime{Kind: LifetimeExecutorRoot})
			return c.mustType(moduleName, "Bool")
		}
		if (e.Op == "+" || e.Op == "-") && isAddressType(left) && isIntegerType(right) {
			c.rememberLifetime(e, lifetime)
			return left
		}
		c.requireSame(left, right, e.SpanV)
		c.rememberLifetime(e, lifetime)
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
	if ctx == ContextOnHandler && constructed.Kind != KindData {
		c.error(expr.SpanV, diag.SEM0016, "on handler can only construct data values")
	}

	c.checkConstructorPermissions(moduleName, expr, constructed, scope, ctx)
	if c.currentPhase == "owned_hardware" && c.hasDelegatedField(constructed) {
		c.error(expr.SpanV, diag.SEM0009, "delegated-only value cannot be constructed in owned_hardware phase")
	}

	fieldBindings := map[string]string{}
	fieldSpans := map[string]source.Span{}
	boundTypes := map[string]*Type{}
	pathUses := map[string]DriverPathUse{}
	seenFields := map[string]source.Span{}
	constructorLifetime := Lifetime{Kind: LifetimeExecutorRoot}
	for _, arg := range expr.Args {
		field, _, _ := c.lookupFieldForArg(constructed, arg.Name)
		if field == nil {
			c.error(expr.SpanV, diag.CG0001, "unknown constructor field "+arg.Name)
			continue
		}
		if _, ok := seenFields[arg.Name]; ok {
			c.error(arg.SpanV, diag.CG0001, "duplicate constructor field "+arg.Name)
		} else {
			seenFields[arg.Name] = arg.SpanV
		}
		argType := c.typeExpr(moduleName, arg.Value, scope, ctx)
		fieldSpans[arg.Name] = arg.SpanV
		c.checkTypeAssign(arg.SpanV, field.Type, argType)
		argLifetime := c.lifetimeOfExpr(arg.Value, scope)
		if constructed.Kind == KindData || (c.allowPlaceConstructor != nil && c.allowPlaceConstructor.expr == expr && constructed.Kind == KindClass) {
			constructorLifetime = c.combineLifetime(arg.SpanV, constructorLifetime, argLifetime)
		} else if !c.rejectCacheEscape(arg.SpanV, argLifetime, Lifetime{Kind: LifetimeExecutorRoot}) {
			c.rejectIfLifetimeEscapes(arg.SpanV, argLifetime, Lifetime{Kind: LifetimeExecutorRoot})
		}
		if named, ok := arg.Value.(*ast.NameExpr); ok {
			fieldBindings[arg.Name] = named.Name
			boundTypes[arg.Name] = argType
		}
		if constructed.Kind == KindExecutor && IsTopicPublisherType(argType) {
			if _, ok := fieldBindings[arg.Name]; !ok {
				origin := c.originForLetValue(moduleName, arg.Value, argType, scope)
				if origin.TopicLabel != "" {
					binding := syntheticExecutorFieldBinding(expr, arg.Name, arg.SpanV)
					fieldBindings[arg.Name] = binding
					boundTypes[arg.Name] = argType
					c.graph.TopicPublishers = append(c.graph.TopicPublishers, TopicPublisherNode{
						TopicLabel: origin.TopicLabel,
						Binding:    binding,
						Span:       arg.SpanV,
					})
				}
			}
		}
		if constructed.Kind == KindExecutor && IsTopicSubscriptionType(argType) {
			if _, ok := fieldBindings[arg.Name]; !ok {
				origin := c.originForLetValue(moduleName, arg.Value, argType, scope)
				if origin.TopicLabel != "" || origin.SlotLabel != "" {
					binding := syntheticExecutorFieldBinding(expr, arg.Name, arg.SpanV)
					fieldBindings[arg.Name] = binding
					boundTypes[arg.Name] = argType
					c.graph.TopicSubscriptions = append(c.graph.TopicSubscriptions, TopicSubscriptionNode{
						TopicLabel:      origin.TopicLabel,
						SubscriberLabel: origin.SlotLabel,
						Binding:         binding,
						Span:            arg.SpanV,
					})
				}
			}
		}
		if field.Type != nil && field.Type.Kind == KindDriverPath && argType != nil && argType.Kind == KindDriverPath {
			if key := c.driverPathExprKey(arg.Value, scope); key != "" {
				pathUses[arg.Name] = DriverPathUse{Key: key, Span: arg.SpanV}
			}
		}
	}

	if len(seenFields) != len(constructed.Fields) {
		c.error(expr.SpanV, diag.CG0001, "constructor field completeness check failed")
	}
	for _, field := range constructed.Fields {
		if _, ok := seenFields[field.Name]; !ok {
			c.error(expr.SpanV, diag.CG0001, "missing constructor field "+field.Name)
		}
	}

	switch constructed.Kind {
	case KindExecutor:
		c.graph.Executors = append(c.graph.Executors, ExecutorNode{
			Type:          constructed,
			Span:          expr.SpanV,
			FieldBindings: fieldBindings,
			FieldSpans:    fieldSpans,
			BoundTypes:    boundTypes,
			PathUses:      pathUses,
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
		if constructed.Kind == KindDriverPath {
			c.graph.DriverPaths = append(c.graph.DriverPaths, DriverPathNode{Type: constructed, Span: expr.SpanV, FieldUses: pathUses})
		}
	}
	if constructed.Kind == KindData || (c.allowPlaceConstructor != nil && c.allowPlaceConstructor.expr == expr && constructed.Kind == KindClass) {
		c.rememberLifetime(expr, constructorLifetime)
	} else {
		c.rememberLifetime(expr, Lifetime{Kind: LifetimeExecutorRoot})
	}
	return constructed
}

func (c *checker) checkConstructorPermissions(moduleName string, expr *ast.ConstructorExpr, typ *Type, scope *Scope, ctx ContextKind) {
	_ = scope
	if typ.Module == "machine.x86_64.executor_memory" && typ.Name == "ArenaFrame" {
		c.error(expr.SpanV, diag.SEM0029, "ArenaFrame can only be created by with arena.frame(length = ...)")
		return
	}
	if IsTopicPublisherType(typ) {
		if c.topicCapabilityFactoryAllowed("publisher") {
			return
		}
		c.error(expr.SpanV, diag.SEM0039, "topic publisher must be created with topic.publisher()")
		return
	}
	if IsTopicSubscriptionType(typ) {
		if c.topicCapabilityFactoryAllowed("subscribe") {
			return
		}
		c.error(expr.SpanV, diag.SEM0040, "topic subscription must be created with topic.subscribe(...)")
		return
	}
	if typ.Module == "machine.x86_64.executor_memory" && typ.Name == "MutableBytes" {
		if ctx != ContextImagePhaseDirect || c.currentPhase != "delegated_hardware" || !constructorArgsAreIntegerLiterals(expr, "address", "length") {
			c.error(expr.SpanV, diag.SEM0028, "raw physical byte authority can only be created directly in delegated_hardware phase")
			return
		}
	}
	if isHardwareAuthorityType(typ) && !isTrustedHardwareAuthorityModule(moduleName) {
		c.error(expr.SpanV, diag.SEM0049, typ.Name+" must come from hardware discovery authority")
		return
	}
	if typ.Kind == KindData {
		return
	}
	if c.allowPlaceConstructor != nil && c.allowPlaceConstructor.expr != expr && typ.Kind == KindClass && !typ.Unique {
		c.error(expr.SpanV, diag.SEM0006, typ.Kind.String()+" construction is allowed only directly inside image phase bodies")
		return
	}
	if c.allowPlaceConstructor != nil && !c.allowPlaceConstructor.used && c.allowPlaceConstructor.expr == expr && typ.Kind == KindClass && !typ.Unique {
		c.allowPlaceConstructor.used = true
		return
	}
	if c.canMintInContext(ctx, typ) {
		return
	}
	c.error(expr.SpanV, diag.SEM0006, typ.Kind.String()+" construction is allowed only directly inside image phase bodies")
}

func (c *checker) topicCapabilityFactoryAllowed(method string) bool {
	return c.currentPhase == method && IsTopicType(c.currentType)
}

func (c *checker) canMintInContext(ctx ContextKind, typ *Type) bool {
	if typ == nil {
		return false
	}
	if ctx == ContextImagePhaseDirect && typ.Kind != KindData {
		return true
	}
	if c.currentType != nil && isEdgeCapabilityModule(c.currentType.Module) && isEdgeCapabilityModule(typ.Module) {
		return true
	}
	if ctx == ContextOwnershipTransferAuthorityMethod {
		switch qualifiedTypeName(typ) {
		case "machine.x86_64.cpu_state.ExecutorRegistry",
			"machine.x86_64.cpu_state.Vcpu":
			return true
		}
	}
	return ctx == ContextOwnershipTransferAuthorityMethod && typ == c.ownedRoot
}

func (c *checker) typeCallExpr(moduleName string, expr *ast.CallExpr, scope *Scope, ctx ContextKind) *Type {
	if c.isExplicitInterruptBindCall(moduleName, expr, scope) {
		c.error(expr.SpanV, diag.SEM0019, "explicit interrupt binding calls are not allowed")
		return nil
	}
	recvType := c.typeExpr(moduleName, expr.Receiver, scope, ctx)
	if recvType == nil {
		return nil
	}
	if expr.Method == "interrupt" && recvType.Kind == KindDriverPath && c.eventDeclForPath(recvType) != nil {
		c.error(expr.SpanV, diag.SEM0019, "interrupt events cannot be called directly")
		return nil
	}
	if ctx == ContextOnHandler && c.isForbiddenOnHandlerCall(recvType, expr.Method) {
		c.error(expr.SpanV, diag.SEM0016, "on handler cannot call runtime platform APIs")
		return nil
	}
	if IsTopicType(recvType) && (expr.Method == "publisher" || expr.Method == "subscribe") && ctx != ContextImagePhaseDirect {
		if expr.Method == "publisher" {
			c.error(expr.SpanV, diag.SEM0039, "topic publisher must be created in image wiring")
		} else {
			c.error(expr.SpanV, diag.SEM0040, "topic subscription must be created in image wiring")
		}
		return nil
	}
	if IsArenaType(recvType) && (expr.Method == "place" || expr.Method == "reserve") {
		return c.typeArenaIntrinsicCall(moduleName, expr, scope, ctx)
	}
	if IsVcpuType(recvType) && (expr.Method == "start" || expr.Method == "enter") {
		c.typeVcpuIntrinsicCall(moduleName, expr, scope, ctx)
		if expr.Method == "start" {
			return c.mustType("machine.x86_64.cpu_state", "VcpuStartStatus")
		}
		return c.mustType(moduleName, "never")
	}
	method, errSpan := c.lookupMethod(recvType, expr.Method, expr.SpanV)
	if method == nil {
		c.error(errSpan, diag.CG0001, "unknown method "+expr.Method+" on "+recvType.Name)
		return nil
	}

	if expr.Method == "frame" && IsArenaType(recvType) && method.Return != nil && ClassifyMemoryType(method.Return) == MemoryKindFrameArena {
		if !c.allowFrameCallExpr {
			c.error(expr.SpanV, diag.SEM0022, "arena.frame(length = ...) can only appear as a with expression")
		}
		c.typeAndVerifyCallArgs(moduleName, method, expr.Args, scope, ctx)
		return method.Return
	}
	if ClassifyMemoryType(recvType) == MemoryKindCacheArena && method.Name == "get_bytes" {
		c.typeAndVerifyCallArgs(moduleName, method, expr.Args, scope, ctx)
		intoArg := callArgForParam(method, expr.Args, explicitParamIndex(method, "into"))
		if intoArg == nil {
			return method.Return
		}
		intoType := c.typeExpr(moduleName, intoArg, scope, ctx)
		if ClassifyMemoryType(intoType) != MemoryKindFrameArena {
			c.error(expr.SpanV, diag.SEM0030, "cache lookup must copy into ArenaFrame")
		}
		c.rememberLifetime(expr, Lifetime{
			Kind:  LifetimeCacheLookup,
			Scope: c.lifetimeOfExpr(intoArg, scope).Scope,
		})
		return method.Return
	}

	c.typeAndVerifyCallArgs(moduleName, method, expr.Args, scope, ctx)
	c.recordHardwareClaimCall(moduleName, expr, scope, ctx)
	if c.ownedRoot != nil && method.Return == c.ownedRoot && !(c.currentPhase == "delegated_hardware" && c.isOwnershipTransferAuthority(recvType)) {
		c.error(expr.SpanV, diag.SEM0008, c.ownedRoot.Name+" can only be minted through ownership-transfer authority in phase delegated_hardware")
	}
	summary := c.ensureMethodLifetimeSummary(methodLifetimeKey(recvType, method.Name), expr.SpanV)
	if !summary.Invalid {
		if summary.ReturnFromReceiver {
			receiverLifetime := c.lifetimeOfExpr(expr.Receiver, scope)
			if receiverLifetime.Kind == LifetimeUnknown {
				receiverLifetime = Lifetime{Kind: LifetimeExecutorRoot}
			}
			if summary.ReturnKind == LifetimeCacheLookup || summary.ReturnKind == LifetimeCacheCopy {
				c.rememberLifetime(expr, Lifetime{Kind: summary.ReturnKind, Scope: receiverLifetime.Scope})
			} else {
				c.rememberLifetime(expr, receiverLifetime)
			}
		} else if summary.ReturnFromParam >= 0 {
			arg := callArgForParam(method, expr.Args, summary.ReturnFromParam)
			argLifetime := c.lifetimeOfExpr(arg, scope)
			if summary.ReturnKind == LifetimeCacheLookup || summary.ReturnKind == LifetimeCacheCopy {
				c.rememberLifetime(expr, Lifetime{Kind: summary.ReturnKind, Scope: argLifetime.Scope})
			} else {
				c.rememberLifetime(expr, argLifetime)
			}
		} else if summary.ReturnStatic {
			c.rememberLifetime(expr, Lifetime{Kind: LifetimeStatic})
		} else {
			c.rememberLifetime(expr, Lifetime{Kind: LifetimeExecutorRoot})
		}
		c.enforceMethodLifetimeRequirements(summary, method, expr, scope)
	}
	return method.Return
}

func (c *checker) enforceMethodLifetimeRequirements(summary MethodLifetimeSummary, method *Method, expr *ast.CallExpr, scope *Scope) {
	for _, requirement := range summary.RootRequirements {
		valueLifetime := Lifetime{Kind: LifetimeExecutorRoot}
		targetLifetime := Lifetime{Kind: LifetimeExecutorRoot}
		span := expr.SpanV
		if requirement.FromReceiver {
			valueLifetime = c.lifetimeOfExpr(expr.Receiver, scope)
			span = expr.Receiver.Span()
		} else if requirement.FromParam >= 0 {
			arg := callArgForParam(method, expr.Args, requirement.FromParam)
			valueLifetime = c.lifetimeOfExpr(arg, scope)
			if arg != nil {
				span = arg.Span()
			}
		}
		if valueLifetime.Kind == LifetimeUnknown {
			valueLifetime = Lifetime{Kind: LifetimeExecutorRoot}
		}
		switch {
		case requirement.TargetReceiver:
			targetLifetime = c.lifetimeOfExpr(expr.Receiver, scope)
		case requirement.TargetParam >= 0:
			targetArg := callArgForParam(method, expr.Args, requirement.TargetParam)
			targetLifetime = c.lifetimeOfExpr(targetArg, scope)
		case requirement.TargetRoot:
			targetLifetime = Lifetime{Kind: LifetimeExecutorRoot}
		}
		if targetLifetime.Kind == LifetimeUnknown {
			targetLifetime = Lifetime{Kind: LifetimeExecutorRoot}
		}
		if !c.rejectCacheEscape(span, valueLifetime, targetLifetime) {
			c.rejectIfLifetimeEscapes(span, valueLifetime, targetLifetime)
		}
	}
}

func (c *checker) isExplicitInterruptBindCall(moduleName string, expr *ast.CallExpr, scope *Scope) bool {
	if expr == nil || expr.Method != "bind" {
		return false
	}
	recvType := c.exprStaticType(moduleName, expr.Receiver, scope)
	if recvType == nil {
		return false
	}
	return recvType.Module == "machine.x86_64.interrupts" && recvType.Name == "ApicInterruptController"
}

func (c *checker) isForbiddenOnHandlerCall(recvType *Type, method string) bool {
	if recvType == nil {
		return false
	}
	switch recvType.Module + "." + recvType.Name + "::" + method {
	case "machine.x86_64.interrupts.ApicInterruptController::enable_cpu_interrupts",
		"machine.x86_64.interrupts.ApicInterruptController::initialize_for_com1_receive",
		"machine.x86_64.interrupts.LocalApic::enable",
		"machine.x86_64.interrupts.IoApic::route_gsi4_to_vector40",
		"machine.x86_64.pci.PciDevice::write_config32",
		"machine.x86_64.pci.MsiCapability::route",
		"machine.x86_64.pci.MsixCapability::route_entry",
		"machine.x86_64.edu.EduMsiPath::raise_test_interrupt",
		"machine.x86_64.edu.EduMsiPath::write32",
		"machine.x86_64.ivshmem.IvshmemDoorbellPeerPath::ring_peer",
		"machine.x86_64.ivshmem.IvshmemDoorbellPeerPath::write32",
		"machine.x86_64.serial.SerialConsolePath::enable_receive_interrupts",
		"machine.x86_64.executor_memory.ExecutorMemory::halt_forever",
		"arch.x86_64.cpu.CpuControl::halt_forever":
		return true
	default:
		return false
	}
}

func (c *checker) typeAndVerifyCallArgs(moduleName string, method *Method, args []ast.NamedArg, scope *Scope, ctx ContextKind) {
	params := method.Params
	if len(params) > 0 && len(params[0].Name) > 0 && params[0].Name == "self" {
		params = params[1:]
	}
	used := map[string]bool{}
	pos := 0
	seenNamed := false
	for _, arg := range args {
		if arg.Name != "" {
			seenNamed = true
			found := false
			for _, p := range params {
				if p.Name == arg.Name {
					found = true
					if used[p.Name] {
						c.error(arg.SpanV, diag.CG0001, "duplicate call argument "+arg.Name)
						break
					}
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
		if seenNamed {
			c.error(arg.SpanV, diag.CG0001, "positional call argument cannot follow named arguments")
			continue
		}
		if pos >= len(params) {
			c.error(arg.SpanV, diag.CG0001, "too many call arguments")
			return
		}
		p := params[pos]
		pos++
		if p.Name != "" {
			used[p.Name] = true
			c.checkTypeAssign(arg.SpanV, p.Type, c.typeExpr(moduleName, arg.Value, scope, ctx))
		}
	}
	for _, p := range params {
		if p.Name == "" {
			continue
		}
		if _, ok := used[p.Name]; !ok && !strings.HasPrefix(p.Name, "_") {
			c.error(method.Span, diag.CG0001, "missing call argument "+p.Name)
		}
	}
}

func (c *checker) hasDelegatedField(typ *Type) bool {
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

func (c *checker) fieldByName(typ *Type, name string) *Field {
	if typ == nil {
		return nil
	}
	for i := range typ.Fields {
		if typ.Fields[i].Name == name {
			return &typ.Fields[i]
		}
	}
	return nil
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
		return false
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

func isTrueLiteral(expr ast.Expr) bool {
	lit, ok := expr.(*ast.BoolLiteral)
	return ok && lit.Value
}

func isNeverType(t *Type) bool {
	return t != nil && t.Name == "never"
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
