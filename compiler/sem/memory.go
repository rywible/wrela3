package sem

import (
	"strconv"
	"strings"

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
	ReturnFromParam    int
	ReturnFromReceiver bool
	ReturnKind         LifetimeKind
	ReturnStatic       bool
	ReturnRoot         bool
	RootRequirements   []LifetimeRequirement
	Terminates         bool
	Invalid            bool
}

type LifetimeRequirement struct {
	FromParam      int
	FromReceiver   bool
	TargetParam    int
	TargetReceiver bool
	TargetRoot     bool
	Kind           LifetimeKind
}

type methodLifetimeTarget struct {
	ModuleName     string
	Type           *Type
	Method         ast.MethodDecl
	SemanticMethod *Method
	ReturnType     *Type
	Context        ContextKind
}

// Method-summary checking uses negative synthetic scopes for abstract lifetimes.
// Keep the receiver sentinel well away from explicit parameter sentinels
// (which start at -1 and count downward).
const methodReceiverLifetimeScope = -1 << 30

type placeConstructorAllowance struct {
	expr *ast.ConstructorExpr
	used bool
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

func isSlotsType(t *Type) bool {
	return t != nil && t.Module == "machine.x86_64.executor_memory" && t.Name == "Slots" && len(t.TypeArgs) == 1
}

func isInterruptQueueType(t *Type) bool {
	return t != nil && t.Module == "machine.x86_64.interrupt_queue" && t.Name == "InterruptQueue" && len(t.TypeArgs) == 1
}

func isSliceType(t *Type) bool {
	return t != nil &&
		t.Module == "machine.x86_64.executor_memory" &&
		(t.Name == "Slice" || t.Name == "MutableSlice") &&
		len(t.TypeArgs) == 1
}

func isProtectedViewType(t *Type) bool {
	if isSlotsType(t) || isSliceType(t) {
		return true
	}
	switch qualifiedTypeName(t) {
	case "platform.hardware.bytes.Mmio",
		"platform.uefi.types.FirmwareSlice",
		"platform.hardware.bytes.Volatile",
		"platform.hardware.memory.DmaBuffer":
		return true
	default:
		return false
	}
}

func typeHasKnownLayout(t *Type) bool {
	_, _, ok := semanticSizeAlign(t)
	return ok
}

func isTrustedAuthorityModule(moduleName string) bool {
	return strings.HasPrefix(moduleName, "platform.hardware.") ||
		strings.HasPrefix(moduleName, "platform.uefi.") ||
		strings.HasPrefix(moduleName, "platform.acpi.") ||
		strings.HasPrefix(moduleName, "machine.x86_64.")
}

func IsPhysicalRegionAuthorityType(t *Type) bool {
	return t != nil && t.Module == "platform.hardware.memory" && t.Name == "PhysicalRegionAuthority"
}

func IsArenaAuthorityType(t *Type) bool {
	if t == nil || t.Module != "platform.hardware.memory" {
		return false
	}
	return t.Name == "RootArena" || t.Name == "ChildArena"
}

func IsDMABufferAuthorityType(t *Type) bool {
	return t != nil && t.Module == "platform.hardware.memory" && t.Name == "DmaBuffer"
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
	return len(params) == 1 && params[0].Name == "length" && legacyTypeName(params[0].Type) == "U64"
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
			case *ast.DataDecl:
				if len(d.TypeParams) == 0 && !hasGenericMethodDecl(d.Methods) {
					continue
				}
				typeName, methods = d.Name, d.Methods
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
				var returnType *Type
				if method.Return.Name != "" {
					returnType, _ = c.index.LookupTypeRef(mod.Name, method.Return, typeParamMapForMethod(typ, method.TypeParams))
				}
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
	keys := append([]string(nil), c.index.InstantiationOrder...)
	for _, key := range keys {
		typ := c.index.Instantiations[key]
		if typ == nil {
			continue
		}
		for i := range typ.Methods {
			method := &typ.Methods[i]
			if method.IsAsm || len(method.TypeParams) != 0 {
				continue
			}
			marker := ContextNormalMethod
			if c.isOwnershipTransferAuthority(typ) && method.Return == c.ownedRoot {
				marker = ContextOwnershipTransferAuthorityMethod
			}
			c.methodLifetimeTargets[methodLifetimeKey(typ, method.Name)] = methodLifetimeTarget{
				ModuleName:     typ.Module,
				Type:           typ,
				SemanticMethod: method,
				ReturnType:     method.Return,
				Context:        marker,
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
	checkType := methodReceiverTypeForCheck(typ)
	method := target.Method
	methodName := method.Name
	body := method.Body
	scope := c.newMethodLifetimeScope(moduleName, checkType, method)
	if target.SemanticMethod != nil {
		methodName = target.SemanticMethod.Name
		body = target.SemanticMethod.Body
		checkType = methodReceiverTypeForCheck(typ)
		scope = c.newSemanticMethodLifetimeScope(checkType, target.SemanticMethod)
	}
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

	prev := c.currentMethodSummary
	prevPhase := c.currentPhase
	prevType := c.currentType
	prevMethodTypeParams := c.currentMethodTypeParams
	prevMethodWhere := c.currentMethodWhere
	c.currentMethodSummary = &summary
	c.currentPhase = methodName
	c.currentType = checkType
	c.currentMethodTypeParams = typeParamMapForCheck(method.TypeParams)
	c.currentMethodWhere = c.methodWhereBounds(moduleName, checkType, method)
	if target.SemanticMethod != nil {
		c.currentMethodWhere = target.SemanticMethod.Where
	}
	c.activeMethodSummaries[key] = true
	terminates := c.checkStmtList(moduleName, body, scope, target.ReturnType, target.Context)
	summary.Terminates = terminates
	delete(c.activeMethodSummaries, key)
	c.currentMethodWhere = prevMethodWhere
	c.currentMethodTypeParams = prevMethodTypeParams
	c.currentType = prevType
	c.currentPhase = prevPhase
	c.currentMethodSummary = prev
	c.methodLifetimeSummaries[key] = summary
	return summary
}

func (c *checker) methodWhereBounds(moduleName string, typ *Type, method ast.MethodDecl) []TraitBound {
	if len(method.Where) == 0 || c == nil || c.index == nil {
		return nil
	}
	where, ds := buildWhereBounds(c.index, moduleName, method.Where, typeParamMapForMethod(typ, method.TypeParams))
	c.diags = append(c.diags, ds...)
	return where
}

func methodReceiverTypeForCheck(typ *Type) *Type {
	if typ == nil || len(typ.TypeArgs) != 0 || len(typ.TypeParams) == 0 {
		return typ
	}
	args := make([]*Type, 0, len(typ.TypeParams))
	for _, param := range typ.TypeParams {
		args = append(args, &Type{Name: param.Name, Kind: KindTypeParam})
	}
	receiver := *typ
	receiver.TypeArgs = args
	receiver.GenericOrigin = typ
	receiver.TypeParams = nil
	receiver.keyCache = ""
	return &receiver
}

func methodLifetimeKey(typ *Type, methodName string) string {
	if typ == nil {
		return "::" + methodName
	}
	return typ.Key() + "::" + methodName
}

func (c *checker) newMethodLifetimeScope(moduleName string, typ *Type, method ast.MethodDecl) *Scope {
	scope := NewScope(nil)
	if len(method.Params) > 0 && method.Params[0].Name == "self" {
		scope.Define("self", typ)
		scope.DefineOrigin("self", localOrigin{
			Type:                typ,
			AuthorityProvenance: c.methodReceiverHasAuthorityProvenance(moduleName, typ, method.Name),
		})
		scope.DefineLifetime("self", Lifetime{Kind: LifetimeFrame, Scope: methodReceiverLifetimeScope})
	}
	explicitIndex := 0
	for _, p := range method.Params {
		if p.Name == "self" {
			continue
		}
		paramType, _ := c.index.LookupTypeRef(moduleName, p.Type, typeParamMapForMethod(typ, method.TypeParams))
		if paramType == nil {
			paramType = c.mustType(moduleName, legacyTypeName(p.Type))
		}
		scope.Define(p.Name, paramType)
		scope.DefineOrigin(p.Name, localOrigin{
			Type:                paramType,
			AuthorityProvenance: c.methodParamHasAuthorityProvenance(moduleName, typ, method, p),
		})
		if ClassifyMemoryType(paramType) == MemoryKindFrameArena {
			scope.DefineLifetime(p.Name, Lifetime{Kind: LifetimeFrame, Scope: -(explicitIndex + 1)})
		} else if parameterCanCarryHiddenLifetime(paramType) {
			scope.DefineLifetime(p.Name, Lifetime{Kind: LifetimeFrame, Scope: -(explicitIndex + 1)})
		} else {
			scope.DefineLifetime(p.Name, Lifetime{Kind: LifetimeExecutorRoot})
		}
		explicitIndex++
	}
	return scope
}

func (c *checker) newSemanticMethodLifetimeScope(typ *Type, method *Method) *Scope {
	scope := NewScope(nil)
	if method == nil {
		return scope
	}
	if len(method.Params) > 0 && method.Params[0].Name == "self" {
		scope.Define("self", typ)
		scope.DefineOrigin("self", localOrigin{
			Type:                typ,
			AuthorityProvenance: c.methodReceiverHasAuthorityProvenance(typ.Module, typ, method.Name),
		})
		scope.DefineLifetime("self", Lifetime{Kind: LifetimeFrame, Scope: methodReceiverLifetimeScope})
	}
	explicitIndex := 0
	for _, p := range method.Params {
		if p.Name == "self" {
			continue
		}
		scope.Define(p.Name, p.Type)
		scope.DefineOrigin(p.Name, localOrigin{Type: p.Type})
		if ClassifyMemoryType(p.Type) == MemoryKindFrameArena {
			scope.DefineLifetime(p.Name, Lifetime{Kind: LifetimeFrame, Scope: -(explicitIndex + 1)})
		} else if parameterCanCarryHiddenLifetime(p.Type) {
			scope.DefineLifetime(p.Name, Lifetime{Kind: LifetimeFrame, Scope: -(explicitIndex + 1)})
		} else {
			scope.DefineLifetime(p.Name, Lifetime{Kind: LifetimeExecutorRoot})
		}
		explicitIndex++
	}
	return scope
}

func (c *checker) methodParamHasAuthorityProvenance(moduleName string, typ *Type, method ast.MethodDecl, param ast.Param) bool {
	if c == nil || param.Name == "" {
		return false
	}
	switch {
	case qualifiedTypeName(typ) == "platform.acpi.tables.AcpiHelpers" &&
		method.Name == "table_at" &&
		param.Name == "address":
		return true
	case qualifiedTypeName(typ) == "platform.acpi.root.AcpiLocator" &&
		method.Name == "find" &&
		param.Name == "tables":
		return true
	case qualifiedTypeName(typ) == "platform.hardware.discovery.PlatformDiscoveryRoot" &&
		method.Name == "from_uefi" &&
		param.Name == "hardware":
		return true
	case qualifiedTypeName(typ) == "platform.hardware.memory.RootArena" &&
		method.Name == "dma_buffer" &&
		param.Name == "owner":
		return true
	default:
		return false
	}
}

func hasGenericMethodDecl(methods []ast.MethodDecl) bool {
	for _, method := range methods {
		if len(method.TypeParams) != 0 {
			return true
		}
	}
	return false
}

func (c *checker) methodReceiverHasAuthorityProvenance(moduleName string, typ *Type, methodName string) bool {
	if IsPhysicalRegionAuthorityType(typ) || IsArenaAuthorityType(typ) || IsDMABufferAuthorityType(typ) {
		return true
	}
	switch qualifiedTypeName(typ) {
	case "platform.uefi.transition.DelegatedHardware",
		"platform.uefi.types.UefiConfigurationTables",
		"platform.acpi.root.AcpiRoot":
		return true
	default:
		return false
	}
}

func parameterCanCarryHiddenLifetime(typ *Type) bool {
	return typeCanCarryHiddenLifetime(typ) || primitiveCanCarryHiddenLifetime(typ)
}

func typeCanCarryHiddenLifetime(typ *Type) bool {
	if isValueOnlyAuthorityRecord(typ) {
		return false
	}
	if isSlotsType(typ) || isSliceType(typ) {
		return true
	}
	return typ != nil && (typ.Kind == KindData || typ.Kind == KindClass || ClassifyMemoryType(typ) == MemoryKindFrameArena)
}

func isValueOnlyAuthorityRecord(typ *Type) bool {
	switch qualifiedTypeName(typ) {
	case "machine.x86_64.executor_slot.ExecutorSlot",
		"machine.x86_64.cpu_state.ExecutorSlot",
		"machine.x86_64.interrupt_queue.QueueIdentity",
		"machine.x86_64.interrupt_queue.InterruptOverflowPolicy",
		"machine.x86_64.interrupts.InterruptSourceIdentity",
		"machine.x86_64.interrupts.InterruptVector":
		return true
	default:
		return false
	}
}

func primitiveCanCarryHiddenLifetime(typ *Type) bool {
	return typ != nil && typ.Kind == KindPrimitive && (typ.Name == "PhysicalAddress" || typ.Name == "VirtualAddress")
}

func (c *checker) recordReturnLifetime(span source.Span, lifetime Lifetime) {
	if c.currentMethodSummary == nil {
		return
	}
	summary := c.currentMethodSummary
	switch {
	case lifetime.Kind == LifetimeStatic:
		if summary.ReturnFromParam >= 0 || summary.ReturnFromReceiver {
			c.error(span, diag.SEM0024, "method returns incompatible frame lifetimes")
			summary.Invalid = true
			return
		}
		summary.ReturnStatic = true
	case isMethodReceiverLifetime(lifetime):
		if summary.ReturnRoot || summary.ReturnStatic || summary.ReturnFromParam >= 0 {
			c.error(span, diag.SEM0024, "method returns incompatible frame lifetimes")
			summary.Invalid = true
			return
		}
		if !summary.ReturnFromReceiver {
			summary.ReturnFromReceiver = true
			summary.ReturnKind = lifetime.Kind
			return
		}
		if summary.ReturnKind != lifetime.Kind {
			c.error(span, diag.SEM0024, "method returns incompatible frame lifetimes")
			summary.Invalid = true
		}
	case (lifetime.Kind == LifetimeFrame || lifetime.Kind == LifetimeCacheLookup || lifetime.Kind == LifetimeCacheCopy) && lifetime.Scope < 0:
		paramIndex := -lifetime.Scope - 1
		if summary.ReturnRoot || summary.ReturnStatic || summary.ReturnFromReceiver {
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
		if summary.ReturnFromParam >= 0 || summary.ReturnFromReceiver {
			c.error(span, diag.SEM0024, "method returns incompatible frame lifetimes")
			summary.Invalid = true
			return
		}
		summary.ReturnRoot = true
	}
}

func isMethodReceiverLifetime(lifetime Lifetime) bool {
	return (lifetime.Kind == LifetimeFrame || lifetime.Kind == LifetimeCacheLookup || lifetime.Kind == LifetimeCacheCopy) &&
		lifetime.Scope == methodReceiverLifetimeScope
}

func isAbstractParamLifetime(lifetime Lifetime) bool {
	return (lifetime.Kind == LifetimeFrame || lifetime.Kind == LifetimeCacheLookup || lifetime.Kind == LifetimeCacheCopy) &&
		lifetime.Scope < 0 &&
		lifetime.Scope != methodReceiverLifetimeScope
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
			if c.currentMethodSummary != nil {
				return c.lifetimeOfExpr(target.Base, scope)
			}
			return Lifetime{Kind: LifetimeExecutorRoot}
		}
		return c.lifetimeOfExpr(target.Base, scope)
	default:
		return Lifetime{Kind: LifetimeExecutorRoot}
	}
}

func (c *checker) rejectIfLifetimeEscapes(span source.Span, value, target Lifetime) {
	if c.lifetimeShorterThan(value, target) {
		if c.recordLifetimeRequirement(value, target) {
			return
		}
		c.error(span, diag.SEM0025, "frame value cannot be stored")
	}
}

func (c *checker) rejectViewLifetimeEscape(span source.Span, typ *Type, value, target Lifetime) bool {
	if !typeCanCarryHiddenLifetimeThroughFieldsOrArgs(typ) {
		return false
	}
	if c.lifetimeShorterThan(value, target) {
		c.error(span, diag.SEM0091, "slots or slice lifetime escapes")
		return true
	}
	return false
}

func (c *checker) recordLifetimeRequirement(value, target Lifetime) bool {
	if c.currentMethodSummary == nil {
		return false
	}
	requirement := LifetimeRequirement{FromParam: -1, TargetParam: -1, Kind: value.Kind}
	switch {
	case isMethodReceiverLifetime(value):
		requirement.FromReceiver = true
	case isAbstractParamLifetime(value):
		requirement.FromParam = -value.Scope - 1
	default:
		return false
	}
	switch {
	case target.Kind == LifetimeExecutorRoot:
		requirement.TargetRoot = true
	case isMethodReceiverLifetime(target):
		requirement.TargetReceiver = true
	case isAbstractParamLifetime(target):
		requirement.TargetParam = -target.Scope - 1
	default:
		return false
	}
	for _, existing := range c.currentMethodSummary.RootRequirements {
		if existing == requirement {
			return true
		}
	}
	c.currentMethodSummary.RootRequirements = append(c.currentMethodSummary.RootRequirements, requirement)
	return true
}

func typeCanCarryHiddenLifetimeThroughFieldsOrArgs(typ *Type) bool {
	return typeCanCarryHiddenLifetimeThroughFieldsOrArgsWithSeen(typ, map[*Type]bool{})
}

func typeCanCarryHiddenLifetimeThroughFieldsOrArgsWithSeen(typ *Type, seen map[*Type]bool) bool {
	if isValueOnlyAuthorityRecord(typ) {
		return false
	}
	if isSlotsType(typ) || isSliceType(typ) {
		return true
	}
	if typ == nil {
		return false
	}
	if typ.Kind != KindData && typ.Kind != KindClass {
		return false
	}
	if seen[typ] {
		return false
	}
	seen[typ] = true
	for _, arg := range typ.TypeArgs {
		if typeCanCarryHiddenLifetimeThroughFieldsOrArgsWithSeen(arg, seen) {
			return true
		}
	}
	for _, field := range typ.Fields {
		if typeCanCarryHiddenLifetimeThroughFieldsOrArgsWithSeen(field.Type, seen) {
			return true
		}
	}
	return false
}

func (c *checker) rejectCacheEscape(span source.Span, lifetime Lifetime, target Lifetime) bool {
	if (lifetime.Kind == LifetimeCacheLookup || lifetime.Kind == LifetimeCacheCopy) && c.lifetimeShorterThan(lifetime, target) {
		c.error(span, diag.SEM0031, "cache lookup result cannot escape destination frame")
		return true
	}
	return false
}

func (c *checker) pushFrameLifetime(parentLifetime Lifetime) int {
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

func (c *checker) popFrameLifetime(id int, span source.Span) {
	if len(c.frameLifetimeStack) == 0 {
		return
	}
	if top := c.frameLifetimeStack[len(c.frameLifetimeStack)-1]; top != id {
		c.error(span, diag.CG0001, "frame lifetime stack mismatch")
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

func (c *checker) combineLifetime(span source.Span, current, next Lifetime) Lifetime {
	if !isScopedLifetime(next) {
		return current
	}
	if !isScopedLifetime(current) {
		return next
	}
	if current.Scope == next.Scope {
		return stricterSameScopeLifetime(current, next)
	}
	currentFlowsToNext := !c.lifetimeShorterThan(current, next)
	nextFlowsToCurrent := !c.lifetimeShorterThan(next, current)
	switch {
	case currentFlowsToNext && !nextFlowsToCurrent:
		return next
	case nextFlowsToCurrent && !currentFlowsToNext:
		return current
	case currentFlowsToNext && nextFlowsToCurrent:
		return stricterSameScopeLifetime(current, next)
	default:
		c.error(span, diag.SEM0025, "frame value cannot be stored")
		return current
	}
}

func isScopedLifetime(lifetime Lifetime) bool {
	return lifetime.Kind == LifetimeFrame || lifetime.Kind == LifetimeCacheLookup || lifetime.Kind == LifetimeCacheCopy
}

func stricterSameScopeLifetime(a, b Lifetime) Lifetime {
	if a.Kind == LifetimeCacheCopy || b.Kind == LifetimeCacheCopy {
		return Lifetime{Kind: LifetimeCacheCopy, Scope: a.Scope}
	}
	if a.Kind == LifetimeCacheLookup || b.Kind == LifetimeCacheLookup {
		return Lifetime{Kind: LifetimeCacheLookup, Scope: a.Scope}
	}
	return a
}

func (c *checker) typeArenaIntrinsicCall(moduleName string, expr *ast.CallExpr, scope *Scope, ctx ContextKind) *Type {
	recvType := c.typeExpr(moduleName, expr.Receiver, scope, ctx)
	if !IsArenaType(recvType) {
		if expr.Method == "reserve_array" {
			c.error(expr.SpanV, diag.SEM0021, "reserve_array receiver must be ExecutorMemory or ArenaFrame")
		}
		return nil
	}
	receiverLifetime := c.lifetimeOfExpr(expr.Receiver, scope)
	if receiverLifetime.Kind == LifetimeUnknown {
		receiverLifetime = Lifetime{Kind: LifetimeExecutorRoot}
	}
	if ctx == ContextOnHandler && (expr.Method == "place" || expr.Method == "reserve" || expr.Method == "reserve_array") {
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
		prevAllowPlaceConstructor := c.allowPlaceConstructor
		c.allowPlaceConstructor = &placeConstructorAllowance{expr: cons}
		typ := c.typeConstructorExpr(moduleName, cons, scope, ctx)
		c.allowPlaceConstructor = prevAllowPlaceConstructor
		valueLifetime := c.lifetimeOfExpr(cons, scope)
		if !c.rejectCacheEscape(cons.SpanV, valueLifetime, receiverLifetime) {
			c.rejectIfLifetimeEscapes(cons.SpanV, valueLifetime, receiverLifetime)
		}
		c.rememberLifetime(expr, receiverLifetime)
		return typ
	case "reserve":
		c.requireReserveArgs(moduleName, expr, scope, ctx)
		c.rememberLifetime(expr, receiverLifetime)
		return c.resolveType("machine.x86_64.executor_memory", "MutableBytes")
	case "reserve_array":
		elemType, ok := c.firstArgAsType(moduleName, expr.Args)
		if !ok {
			c.error(expr.SpanV, diag.SEM0078, "reserve_array first argument must be a type")
			return nil
		}
		if !typeHasKnownLayout(elemType) {
			c.error(expr.SpanV, diag.SEM0080, "reserve_array element type must have known layout")
			return nil
		}
		c.requireReserveArrayArgs(moduleName, expr, scope, ctx, elemType)
		slotsType := c.index.instantiateByName("machine.x86_64.executor_memory", "Slots", []*Type{elemType})
		if slotsType == nil {
			c.error(expr.SpanV, diag.SEM0002, "unknown type Slots")
			return nil
		}
		for _, d := range c.index.completeInstantiation(slotsType.Key(), map[string]bool{}) {
			c.diags = append(c.diags, d)
		}
		c.rememberLifetime(expr, receiverLifetime)
		return slotsType
	default:
		return nil
	}
}

func (c *checker) firstArgAsType(moduleName string, args []ast.NamedArg) (*Type, bool) {
	if len(args) == 0 || args[0].Name != "" {
		return nil, false
	}
	params := c.currentTypeParamMap()
	switch value := args[0].Value.(type) {
	case *ast.NameExpr:
		if typ := params[value.Name]; typ != nil {
			return typ, true
		}
		typ, ok := c.index.lookupBaseType(moduleName, value.Name)
		return typ, ok
	case *ast.TypeOperandExpr:
		typ, ds := c.index.LookupTypeRef(moduleName, value.Type, params)
		if len(ds) != 0 {
			c.diags = append(c.diags, ds...)
			return nil, false
		}
		return typ, typ != nil
	default:
		return nil, false
	}
}

func (c *checker) requireReserveArrayArgs(moduleName string, expr *ast.CallExpr, scope *Scope, ctx ContextKind, elemType *Type) {
	if len(expr.Args) != 2 || expr.Args[0].Name != "" || expr.Args[1].Name != "count" {
		c.error(expr.SpanV, diag.SEM0090, "reserve_array expects a type and count")
		return
	}
	u64 := c.mustType(moduleName, "U64")
	countType := c.typeExpr(moduleName, expr.Args[1].Value, scope, ctx)
	if countType != nil && !typesCompatible(u64, countType) {
		c.error(expr.Args[1].Value.Span(), diag.SEM0090, "reserve_array count must be U64")
	}
	countValue, ok := c.constValueOfExpr(moduleName, expr.Args[1].Value)
	if !ok {
		return
	}
	elemSize, _, ok := semanticSizeAlign(elemType)
	if !ok || elemSize == 0 {
		return
	}
	if countValue > ^uint64(0)/elemSize {
		c.error(expr.SpanV, diag.SEM0090, "slot count overflows reservation size")
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
	if value, ok := unsignedIntegerLiteral(expr.Args[1].Value); ok && !isPowerOfTwo(value) {
		c.error(expr.Args[1].Value.Span(), diag.SEM0027, "reserve align must be a non-zero power of two")
	}
}

func (c *checker) constValueOfExpr(moduleName string, expr ast.Expr) (uint64, bool) {
	switch e := expr.(type) {
	case *ast.IntLiteral:
		return unsignedIntegerLiteral(e)
	case *ast.NameExpr:
		value, ok := c.index.LookupConst(moduleName, e.Name)
		return value.Value, ok && value.Type != nil
	default:
		scope := map[string]ConstValue{}
		for name, value := range c.index.Consts[moduleName] {
			if value.Type != nil {
				scope[name] = value
			}
		}
		value, ds := c.evalConstExpr(moduleName, expr, scope)
		return value, len(ds) == 0
	}
}

func unsignedIntegerLiteral(expr ast.Expr) (uint64, bool) {
	lit, ok := expr.(*ast.IntLiteral)
	if !ok {
		return 0, false
	}
	value, err := strconv.ParseUint(lit.Value, 0, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

func isPowerOfTwo(value uint64) bool {
	return value != 0 && value&(value-1) == 0
}
