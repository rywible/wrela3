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
	Type                      *Type
	AuthorityProvenance       bool
	Constructor               *ast.ConstructorExpr
	FieldBindings             map[string]string
	FieldOrigins              map[string]localOrigin
	SlotLabel                 string
	RecordsExecutorSlot       bool
	ExecutorRegistryAuthority bool
	LoopPolicy                string
	LoopStrategy              string
	LoopFallback              string
	MemoryOwnerLabel          string
	ArenaLabel                string
	ArenaBase                 uint64
	ArenaBytes                uint64
	ArenaAlign                uint64
	TopicLabel                string
	TopicType                 string
	TopicTypeKey              string
	TopicKind                 string
	TopicDepth                uint64
	TopicPayloadType          string
	TopicPayloadKey           string
	TopicPayloadSize          uint64
	TopicPayloadAlign         uint64
	TopicNextType             string
	TopicNextKey              string
	TimerLabel                string
	TimerSource               string
	TimerPeriodUS             uint64
	IsTimerRoute              bool
	PathLabel                 string
	PublishesInterrupts       bool
	EventType                 string
	EventFunctionSymbol       string
	VcpuID                    int
	HasVcpuID                 bool
	ExecutorBinding           string
	TerminalVcpu              bool
	PciDeviceKey              string
	SharedIRQRouteKey         string
	SharedIRQVector           int
	SharedSourceLabel         string
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

func (s *Scope) AssignOrigin(name string, origin localOrigin) {
	if s == nil {
		return
	}
	if _, ok := s.origins[name]; ok {
		s.origins[name] = origin
		return
	}
	if s.parent != nil {
		s.parent.AssignOrigin(name, origin)
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
	currentMethodTypeParams map[string]*Type
	currentMethodWhere      []TraitBound
	seenSharedIRQSource     map[string]bool
	// TODO: scope these discovered plan facts to constructor expression IDs if
	// images ever allow multiple CPU or hardware plan builders.
	cpuFeatureLoopStrategy   string
	cpuFeatureLoopFallback   string
	hardwarePlanWakeStrategy string
	hardwarePlanWakeFallback string
	typeParamMaps            map[*Type]map[string]*Type
}

type driverPathOwner struct {
	executor string
	span     source.Span
}

func Check(index *Index, modules []*ast.Module) (*CheckedProgram, []diag.Diagnostic) {
	c := &checker{
		index:         index,
		modules:       modules,
		currentPhase:  "",
		typeParamMaps: map[*Type]map[string]*Type{},
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

	c.checkHiddenSchedulerVocabulary()
	c.checkImageSignatures()
	c.checkUnresolvedTypes()
	c.checkConstDecls()
	c.checkStaticAsserts()
	c.checkDeclBodiesAndConstructors()
	c.finalizeInterruptTopicRoutes()
	c.checkDelegatedOnlyCrossing()
	c.checkUniqueConstructors()
	c.checkExecutorWiring()
	c.checkExecutorTopicGraph()
	c.checkHardwareClaims()
	c.validateArenaGraph()
	c.checkStorageAuthority()
	storage := c.checkStorageDecls()

	return &CheckedProgram{
		Modules:    modules,
		Index:      index,
		ImageGraph: c.graph,
		OwnedRoot:  c.ownedRoot,
		Storage:    storage,
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
		delegatedParamType := c.resolveType(imageModule, legacyTypeName(delegated.Params[0].Type))
		if delegatedParamType != c.resolveType(imageModule, "DelegatedHardware") {
			c.error(delegated.Params[0].Span, diag.SEM0005, "delegated_hardware phase must accept DelegatedHardware")
			signatureValid = false
		}

		ownedParam := c.mustType(imageModule, legacyTypeName(delegated.Return))
		if ownedParam == nil {
			c.error(delegated.SpanV, diag.SEM0005, "unknown delegated_hardware return type")
			continue
		}

		if ownedParam != c.resolveType(imageModule, legacyTypeName(owned.Params[0].Type)) {
			c.error(owned.Params[0].Span, diag.SEM0005, "owned_hardware phase must receive the same type returned by delegated_hardware")
			continue
		}
		if c.resolveType(imageModule, legacyTypeName(owned.Return)) != c.resolveType(imageModule, "never") {
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
				params := typeParamMapForCheck(d.TypeParams)
				c.checkFieldsResolved(mod.Name, d.Fields, params)
				c.checkMethodTypesResolved(mod.Name, d.Methods, params)
			case *ast.EnumDecl:
				params := typeParamMapForCheck(d.TypeParams)
				for _, variant := range d.Variants {
					c.checkFieldsResolved(mod.Name, variant.Fields, params)
				}
			case *ast.TraitDecl:
				params := typeParamMapForCheck(d.TypeParams)
				c.checkMethodTypesResolved(mod.Name, d.Methods, params)
			case *ast.ImplDecl:
				params := map[string]*Type{}
				for _, name := range freeImplTypeParams(c.index, mod.Name, d.Trait, d.For) {
					params[name] = &Type{Name: name, Kind: KindTypeParam}
				}
				c.checkTraitRefResolved(mod.Name, d.Trait, params, d.SpanV)
				c.checkTypeRefResolved(mod.Name, d.For, params, d.SpanV)
			case *ast.ClassDecl:
				params := typeParamMapForCheck(d.TypeParams)
				c.checkFieldsResolved(mod.Name, d.Fields, params)
				c.checkMethodTypesResolved(mod.Name, d.Methods, params)
			case *ast.DriverDecl:
				params := typeParamMapForCheck(d.TypeParams)
				c.checkFieldsResolved(mod.Name, d.Fields, params)
				c.checkMethodTypesResolved(mod.Name, d.Methods, params)
			case *ast.DriverPathDecl:
				c.checkFieldsResolved(mod.Name, d.Fields, nil)
				c.checkMethodTypesResolved(mod.Name, d.Methods, nil)
				for _, event := range d.InterruptEvents {
					c.checkTypeRefResolved(mod.Name, event.EventType, nil, event.SpanV)
				}
			case *ast.ExecutorDecl:
				c.checkFieldsResolved(mod.Name, d.Fields, nil)
				c.checkMethodTypesResolved(mod.Name, d.Methods, nil)
				for _, handler := range d.OnHandlers {
					c.checkTypeRefResolved(mod.Name, handler.ParamType, nil, handler.SpanV)
				}
			case *ast.ConstDecl:
				c.checkTypeRefResolved(mod.Name, d.Type, nil, d.SpanV)
			case *ast.ImageDecl:
				for _, phase := range d.Phases {
					c.checkParamsResolved(mod.Name, phase.Params, nil)
					c.checkTypeRefResolved(mod.Name, phase.Return, nil, phase.SpanV)
				}
			}
		}
	}
}

func (c *checker) checkConstDecls() {
	done := map[string]bool{}
	active := map[string]bool{}
	for _, mod := range c.modules {
		c.checkModuleConstDecls(mod.Name, done, active)
	}
}

func (c *checker) checkModuleConstDecls(moduleName string, done map[string]bool, active map[string]bool) {
	if done[moduleName] {
		return
	}
	mod := c.index.Modules[moduleName]
	if mod == nil {
		return
	}
	if active[moduleName] {
		c.error(mod.Span, diag.SEM0087, "cyclic const import")
		return
	}
	active[moduleName] = true
	for _, imp := range mod.Imports {
		if c.index.Modules[imp.Path] != nil {
			c.checkModuleConstDecls(imp.Path, done, active)
		}
	}
	delete(active, moduleName)
	c.refreshConstImports(mod)

	scope := map[string]ConstValue{}
	for name, cv := range c.index.Consts[moduleName] {
		if cv.Type != nil {
			scope[name] = cv
		}
	}
	for _, decl := range mod.Decls {
		constDecl, ok := decl.(*ast.ConstDecl)
		if !ok {
			continue
		}
		if cv := c.index.Consts[moduleName][constDecl.Name]; cv.Type != nil {
			scope[constDecl.Name] = cv
			continue
		}
		typ, ds := c.index.LookupTypeRef(mod.Name, constDecl.Type, nil)
		if len(ds) != 0 {
			c.diags = append(c.diags, ds...)
			continue
		}
		if typ == nil {
			continue
		}
		value, valueDiags := c.evalConstExpr(mod.Name, constDecl.Value, scope)
		if len(valueDiags) != 0 {
			c.diags = append(c.diags, valueDiags...)
			continue
		}
		constValue := ConstValue{
			Type:  typ,
			Value: value,
			Span:  constDecl.SpanV,
		}
		c.index.Consts[mod.Name][constDecl.Name] = constValue
		scope[constDecl.Name] = constValue
	}
	c.refreshConstImports(mod)
	done[moduleName] = true
}

func (c *checker) refreshConstImports(mod *ast.Module) {
	if mod == nil || c.index == nil {
		return
	}
	if c.index.ConstImports[mod.Name] == nil {
		c.index.ConstImports[mod.Name] = map[string]ConstValue{}
	}
	for _, imp := range mod.Imports {
		for _, name := range imp.Names {
			if cv, ok := c.index.Consts[imp.Path][name]; ok {
				c.index.ConstImports[mod.Name][name] = cv
			}
		}
	}
}

func (c *checker) checkStaticAsserts() {
	for _, mod := range c.modules {
		scope := map[string]ConstValue{}
		for name, cv := range c.index.Consts[mod.Name] {
			if cv.Type != nil {
				scope[name] = cv
			}
		}
		for _, decl := range mod.Decls {
			assert, ok := decl.(*ast.StaticAssertDecl)
			if !ok {
				continue
			}
			value, ds := c.evalConstExpr(mod.Name, assert.Expr, scope)
			if len(ds) != 0 {
				c.diags = append(c.diags, ds...)
				continue
			}
			if value == 0 {
				c.error(assert.SpanV, diag.SEM0089, "static assertion failed: "+assert.Message)
			}
		}
	}
}

func typeParamMapForCheck(params []ast.TypeParam) map[string]*Type {
	if len(params) == 0 {
		return nil
	}
	out, _ := buildTypeParamMap(params)
	return out
}

func mergeTypeParamMap(base map[string]*Type, params []ast.TypeParam) map[string]*Type {
	return mergeTypeParamMaps(base, typeParamMapForCheck(params))
}

func mergeTypeParamMaps(base map[string]*Type, extra map[string]*Type) map[string]*Type {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	out := map[string]*Type{}
	for name, typ := range base {
		out[name] = typ
	}
	for name, typ := range extra {
		out[name] = typ
	}
	return out
}

func typeParamMapForMethod(typ *Type, params []ast.TypeParam) map[string]*Type {
	var base map[string]*Type
	if typ != nil {
		if typ.GenericOrigin != nil && len(typ.GenericOrigin.TypeParams) == len(typ.TypeArgs) {
			base = map[string]*Type{}
			for i, param := range typ.GenericOrigin.TypeParams {
				base[param.Name] = typ.TypeArgs[i]
			}
		} else if len(typ.TypeParams) != 0 {
			base = map[string]*Type{}
			for _, param := range typ.TypeParams {
				base[param.Name] = &Type{Name: param.Name, Kind: KindTypeParam}
			}
		}
	}
	return mergeTypeParamMap(base, params)
}

func (c *checker) currentTypeParamMap() map[string]*Type {
	if c == nil {
		return nil
	}
	if c.currentType == nil {
		return mergeTypeParamMaps(nil, c.currentMethodTypeParams)
	}
	var base map[string]*Type
	if cached, ok := c.typeParamMaps[c.currentType]; ok {
		base = cached
		return mergeTypeParamMaps(base, c.currentMethodTypeParams)
	}
	if c.currentType.GenericOrigin != nil && len(c.currentType.GenericOrigin.TypeParams) == len(c.currentType.TypeArgs) {
		out := map[string]*Type{}
		for i, param := range c.currentType.GenericOrigin.TypeParams {
			out[param.Name] = c.currentType.TypeArgs[i]
		}
		c.typeParamMaps[c.currentType] = out
		return mergeTypeParamMaps(out, c.currentMethodTypeParams)
	}
	if len(c.currentType.TypeParams) == 0 {
		c.typeParamMaps[c.currentType] = nil
		return mergeTypeParamMaps(nil, c.currentMethodTypeParams)
	}
	out := map[string]*Type{}
	for _, param := range c.currentType.TypeParams {
		out[param.Name] = &Type{Name: param.Name, Kind: KindTypeParam}
	}
	c.typeParamMaps[c.currentType] = out
	return mergeTypeParamMaps(out, c.currentMethodTypeParams)
}

func (c *checker) checkTypeRefResolved(moduleName string, ref ast.TypeRef, params map[string]*Type, span source.Span) {
	if ref.Name == "" {
		return
	}
	typ, ds := c.index.LookupTypeRef(moduleName, ref, params)
	if len(ds) != 0 {
		c.diags = append(c.diags, ds...)
		return
	}
	if typ == nil {
		c.error(span, diag.SEM0002, "unknown type "+ref.String())
	}
}

func (c *checker) checkTraitRefResolved(moduleName string, ref ast.TypeRef, params map[string]*Type, span source.Span) {
	if ref.Name == "" {
		return
	}
	typ, ds := c.index.LookupTraitRef(moduleName, ref, params)
	if len(ds) != 0 {
		c.diags = append(c.diags, ds...)
		return
	}
	if typ == nil {
		c.error(span, diag.SEM0002, "unknown type "+ref.String())
	}
}

func (c *checker) checkFieldsResolved(moduleName string, fields []ast.Field, params map[string]*Type) {
	for _, field := range fields {
		c.checkTypeRefResolved(moduleName, field.Type, params, field.Span)
	}
}

func (c *checker) checkParamsResolved(moduleName string, params []ast.Param, typeParams map[string]*Type) {
	for _, param := range params {
		if param.Type.Name != "" {
			c.checkTypeRefResolved(moduleName, param.Type, typeParams, param.Span)
		}
	}
}

func (c *checker) checkMethodTypesResolved(moduleName string, methods []ast.MethodDecl, ownerParams map[string]*Type) {
	for _, method := range methods {
		params := mergeTypeParamMap(ownerParams, method.TypeParams)
		c.checkParamsResolved(moduleName, method.Params, params)
		if method.Return.Name != "" {
			c.checkTypeRefResolved(moduleName, method.Return, params, method.SpanV)
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
				c.checkAcpiTableAtCallsInMethods(mod.Name, typ, d.Methods)
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
		retType, ds := c.index.LookupTypeRef(moduleName, event.EventType, nil)
		if len(ds) != 0 {
			c.diags = append(c.diags, ds...)
			continue
		}
		if retType == nil {
			c.error(event.SpanV, diag.SEM0002, "unknown type "+event.EventType.String())
			continue
		}
		if retType.Kind != KindData && retType.Kind != KindEnum {
			c.error(event.SpanV, diag.SEM0015, "interrupt event type must be a data record or enum")
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
		paramTypeName := legacyTypeName(handler.ParamType)
		paramType := c.resolveType(moduleName, paramTypeName)
		if paramType == nil {
			c.error(handler.SpanV, diag.SEM0002, "unknown type "+paramTypeName)
			continue
		}
		eventType := c.resolveType(field.Type.Module, legacyTypeName(event.EventType))
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

		retType := c.mustType(moduleName, legacyTypeName(phase.Return))
		scope := NewScope(nil)
		for _, p := range phase.Params {
			if p.Name == "" {
				continue
			}
			paramType := c.mustType(moduleName, legacyTypeName(p.Type))
			scope.Define(p.Name, paramType)
			scope.DefineOrigin(p.Name, localOrigin{
				Type:                paramType,
				AuthorityProvenance: phaseParamHasAuthorityProvenance(phase.Name, paramType),
			})
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

		returnType, _ := c.index.LookupTypeRef(moduleName, method.Return, typeParamMapForMethod(typ, method.TypeParams))
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

func (c *checker) checkAcpiTableAtCallsInMethods(moduleName string, typ *Type, methods []ast.MethodDecl) {
	if len(methods) == 0 {
		return
	}
	prevType := c.currentType
	c.currentType = typ
	for _, method := range methods {
		if method.IsAsm {
			continue
		}
		scope := c.newMethodLifetimeScope(moduleName, typ, method)
		c.checkAcpiTableAtCallsInStmts(moduleName, method.Body, scope)
	}
	c.currentType = prevType
}

func (c *checker) checkAcpiTableAtCallsInStmts(moduleName string, stmts []ast.Stmt, scope *Scope) {
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *ast.LetStmt:
			c.checkAcpiTableAtCallsInExpr(moduleName, s.Expr, scope)
			valueType := c.exprStaticType(moduleName, s.Expr, scope)
			scope.Define(s.Name, valueType)
			scope.DefineOrigin(s.Name, c.originForExprValue(moduleName, s.Expr, valueType, scope))
		case *ast.AssignStmt:
			c.checkAcpiTableAtCallsInExpr(moduleName, s.Value, scope)
			c.assignOriginForTarget(moduleName, s.Target, s.Value, scope)
		case *ast.ReturnStmt:
			c.checkAcpiTableAtCallsInExpr(moduleName, s.Value, scope)
		case *ast.ExprStmt:
			c.checkAcpiTableAtCallsInExpr(moduleName, s.Expr, scope)
		case *ast.IfStmt:
			c.checkAcpiTableAtCallsInExpr(moduleName, s.Cond, scope)
			c.checkAcpiTableAtCallsInStmts(moduleName, s.Then, NewScope(scope))
			c.checkAcpiTableAtCallsInStmts(moduleName, s.Else, NewScope(scope))
		case *ast.IfLetStmt:
			c.checkAcpiTableAtCallsInExpr(moduleName, s.Value, scope)
			child := NewScope(scope)
			c.bindAuthorityScanPattern(moduleName, child, s.Pattern, s.Value, scope, s.SpanV)
			c.checkAcpiTableAtCallsInStmts(moduleName, s.Body, child)
		case *ast.MatchStmt:
			c.checkAcpiTableAtCallsInExpr(moduleName, s.Value, scope)
			for _, arm := range s.Arms {
				child := NewScope(scope)
				c.bindAuthorityScanPattern(moduleName, child, arm.Pattern, s.Value, scope, arm.Span)
				c.checkAcpiTableAtCallsInStmts(moduleName, arm.Body, child)
			}
		case *ast.WhileStmt:
			c.checkAcpiTableAtCallsInExpr(moduleName, s.Cond, scope)
			c.checkAcpiTableAtCallsInStmts(moduleName, s.Body, NewScope(scope))
		case *ast.WithStmt:
			c.checkAcpiTableAtCallsInExpr(moduleName, s.Expr, scope)
			child := NewScope(scope)
			frameType := c.exprStaticType(moduleName, s.Expr, scope)
			child.Define(s.Name, frameType)
			child.DefineOrigin(s.Name, c.originForExprValue(moduleName, s.Expr, frameType, scope))
			c.checkAcpiTableAtCallsInStmts(moduleName, s.Body, child)
		case *ast.ForStmt:
			c.checkAcpiTableAtCallsInExpr(moduleName, s.InExpr, scope)
			c.checkAcpiTableAtCallsInStmts(moduleName, s.Body, NewScope(scope))
		}
	}
}

func (c *checker) assignOriginForTarget(moduleName string, target ast.Expr, value ast.Expr, scope *Scope) {
	valueType := c.exprStaticType(moduleName, value, scope)
	switch t := target.(type) {
	case *ast.NameExpr:
		scope.AssignOrigin(t.Name, c.originForExprValue(moduleName, value, valueType, scope))
	case *ast.FieldExpr:
		c.assignFieldOrigin(moduleName, t, value, valueType, scope)
	}
}

func (c *checker) assignFieldOrigin(moduleName string, target *ast.FieldExpr, value ast.Expr, valueType *Type, scope *Scope) {
	if scope == nil {
		return
	}
	c.assignFieldOriginValue(moduleName, target, c.originForExprValue(moduleName, value, valueType, scope), scope)
}

func (c *checker) assignFieldOriginValue(moduleName string, target *ast.FieldExpr, assigned localOrigin, scope *Scope) {
	base, ok := target.Base.(*ast.NameExpr)
	if !ok {
		if parent, ok := target.Base.(*ast.FieldExpr); ok {
			parentType := c.exprStaticType(moduleName, parent, scope)
			parentOrigin := c.originForExprValue(moduleName, parent, parentType, scope)
			if parentOrigin.FieldOrigins == nil {
				parentOrigin.FieldOrigins = map[string]localOrigin{}
			}
			parentOrigin.FieldOrigins[target.Field] = assigned
			c.assignFieldOriginValue(moduleName, parent, parentOrigin, scope)
		}
		return
	}
	baseOrigin, _ := scope.LookupOrigin(base.Name)
	if baseOrigin.Type == nil {
		baseOrigin.Type = c.exprStaticType(moduleName, target.Base, scope)
	}
	if baseOrigin.FieldOrigins == nil {
		baseOrigin.FieldOrigins = map[string]localOrigin{}
	}
	baseOrigin.FieldOrigins[target.Field] = assigned
	scope.AssignOrigin(base.Name, baseOrigin)
}

func (c *checker) bindAuthorityScanPattern(moduleName string, child *Scope, pattern ast.Pattern, value ast.Expr, parent *Scope, span source.Span) {
	variantPattern, ok := pattern.(ast.VariantPattern)
	if !ok {
		return
	}
	valueType := c.exprStaticType(moduleName, value, parent)
	if valueType == nil || valueType.Kind != KindEnum {
		return
	}
	variant, ok := c.enumVariant(valueType, variantPattern.Variant)
	if !ok || !sameEnumPatternName(valueType, variantPattern.Enum) {
		return
	}
	valueOrigin := c.originForExprValue(moduleName, value, valueType, parent)
	c.bindPatternFieldsFailed(child, variant, variantPattern.Bindings, valueOrigin, span)
}

func (c *checker) checkAcpiTableAtCallsInExpr(moduleName string, expr ast.Expr, scope *Scope) {
	switch e := expr.(type) {
	case nil, *ast.IntLiteral, *ast.BoolLiteral, *ast.StringLiteral, *ast.SizeOfExpr, *ast.AlignOfExpr, *ast.NameExpr:
		return
	case *ast.FieldExpr:
		c.checkAcpiTableAtCallsInExpr(moduleName, e.Base, scope)
	case *ast.BinaryExpr:
		c.checkAcpiTableAtCallsInExpr(moduleName, e.Left, scope)
		c.checkAcpiTableAtCallsInExpr(moduleName, e.Right, scope)
	case *ast.ConstructorExpr:
		for _, arg := range e.Args {
			c.checkAcpiTableAtCallsInExpr(moduleName, arg.Value, scope)
		}
		constructed, ds := c.index.LookupTypeRef(moduleName, e.Type, c.currentTypeParamMap())
		if len(ds) == 0 && isProtectedViewType(constructed) &&
			(!isTrustedAuthorityModule(moduleName) || !c.protectedViewConstructorHasProvenance(moduleName, e, constructed, scope)) {
			c.error(e.SpanV, diag.SEM0092, "protected memory-region view construction is not allowed here")
		}
	case *ast.VariantConstructorExpr:
		for _, arg := range e.Args {
			c.checkAcpiTableAtCallsInExpr(moduleName, arg.Value, scope)
		}
	case *ast.CallExpr:
		c.checkAcpiTableAtCallsInExpr(moduleName, e.Receiver, scope)
		for _, arg := range e.Args {
			c.checkAcpiTableAtCallsInExpr(moduleName, arg.Value, scope)
		}
		receiverType := c.exprStaticType(moduleName, e.Receiver, scope)
		method, _ := c.lookupMethod(receiverType, e.Method, e.SpanV)
		if qualifiedTypeName(receiverType) == "platform.acpi.root.AcpiRoot" {
			callOrigin := c.originForCall(moduleName, e, c.exprStaticType(moduleName, e, scope), scope)
			if !callOrigin.AuthorityProvenance {
				c.error(e.SpanV, diag.SEM0092, "ACPI root methods require firmware table authority")
			}
		}
		if qualifiedTypeName(receiverType) == "platform.acpi.root.AcpiLocator" && e.Method == "find" {
			tablesArg := namedArgExpr(e.Args, "tables")
			if method != nil {
				tablesArg = callArgForParam(method, e.Args, explicitParamIndex(method, "tables"))
			}
			if !c.exprHasAuthorityProvenance(moduleName, tablesArg, scope) {
				c.error(e.SpanV, diag.SEM0092, "ACPI root discovery requires firmware table authority")
			}
		}
		if qualifiedTypeName(receiverType) == "platform.hardware.discovery.PlatformDiscoveryRoot" && e.Method == "from_uefi" {
			hardwareArg := namedArgExpr(e.Args, "hardware")
			if method != nil {
				hardwareArg = callArgForParam(method, e.Args, explicitParamIndex(method, "hardware"))
			}
			if !c.exprHasAuthorityProvenance(moduleName, hardwareArg, scope) {
				c.error(e.SpanV, diag.SEM0092, "hardware discovery requires delegated hardware authority")
			}
		}
		if qualifiedTypeName(receiverType) != "platform.acpi.tables.AcpiHelpers" || e.Method != "table_at" {
			return
		}
		addressArg := namedArgExpr(e.Args, "address")
		if method != nil {
			addressArg = callArgForParam(method, e.Args, explicitParamIndex(method, "address"))
		}
		if !c.exprHasAuthorityProvenance(moduleName, addressArg, scope) {
			c.error(e.SpanV, diag.SEM0092, "ACPI table lookup address must originate from firmware table authority")
		}
	}
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
		valueType := c.typeExprExpected(moduleName, s.Value, scope, ctx, targetType)
		c.checkTypeAssign(s.Target.Span(), targetType, valueType)
		if target, ok := s.Target.(*ast.NameExpr); ok {
			scope.AssignOrigin(target.Name, c.originForExprValue(moduleName, s.Value, valueType, scope))
		} else if target, ok := s.Target.(*ast.FieldExpr); ok {
			c.assignFieldOrigin(moduleName, target, s.Value, valueType, scope)
		}
		sourceLifetime := c.lifetimeOfExpr(s.Value, scope)
		targetLifetime := c.assignmentTargetLifetime(s.Target, scope)
		if c.rejectViewLifetimeEscape(s.Value.Span(), valueType, sourceLifetime, targetLifetime) {
			return isNeverType(valueType)
		}
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
	case *ast.IfLetStmt:
		return c.checkIfLetStmt(moduleName, scope, expectedReturn, ctx, s)
	case *ast.MatchStmt:
		return c.checkMatchStmt(moduleName, scope, expectedReturn, ctx, s)
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
		got := c.typeExprExpected(moduleName, s.Value, scope, ctx, expectedReturn)
		if ctx == ContextInterruptEvent {
			if got != nil && expectedReturn != nil && (got.Module != expectedReturn.Module || got.Name != expectedReturn.Name) {
				c.error(s.Value.Span(), diag.SEM0015, "interrupt event return type mismatch")
			}
		} else {
			c.requireType(got, expectedReturn, s.Value.Span())
		}
		lifetime := c.lifetimeOfExpr(s.Value, scope)
		if c.rejectViewLifetimeEscape(s.Value.Span(), got, lifetime, Lifetime{Kind: LifetimeExecutorRoot}) {
			return true
		}
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
		c.recordStorageAppendCall(moduleName, s.Expr, scope, false)
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
			pt := c.resolveType(c.lookupImageModule(image), legacyTypeName(phase.Params[0].Type))
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
	c.finalizePlacementConstraints()
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
	claimedSlots := map[string]source.Span{}
	for _, slot := range c.graph.ExecutorSlots {
		if slot.Label != "" {
			claimedSlots[slot.Label] = slot.Span
		}
	}
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
		if _, ok := claimedSlots[exec.SlotLabel]; !ok {
			name := "executor"
			if exec.Type != nil && exec.Type.Name != "" {
				name = exec.Type.Name
			}
			c.error(exec.Span, diag.SEM0035, "executor "+name+" uses an unclaimed executor slot")
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
	for _, sub := range c.graph.TopicSubscriptions {
		if sub.SubscriberLabel == "" {
			continue
		}
		if _, ok := claimedSlots[sub.SubscriberLabel]; !ok {
			c.error(sub.Span, diag.SEM0035, "subscription uses an unclaimed executor slot")
		}
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
		if exec.LoopPolicy == "EventSleepPolicy" && exec.SlotLabel != "" && c.graph.HasWakeSource(exec.SlotLabel) {
			strategy := exec.LoopStrategy
			if strategy == "" {
				strategy = "sti_hlt"
			}
			fallback := exec.LoopFallback
			if fallback == "" {
				fallback = "sti_hlt"
			}
			c.graph.WakeTargets = append(c.graph.WakeTargets, WakeTargetNode{
				SlotLabel: exec.SlotLabel,
				Owner:     exec.Type.Name,
				Strategy:  strategy,
				Fallback:  fallback,
				Span:      exec.Span,
			})
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
	case moduleName == "machine.x86_64.executor_memory":
		return true
	case moduleName == "machine.x86_64.interrupt_queue":
		return true
	case moduleName == "machine.x86_64.timer":
		return true
	case strings.HasPrefix(moduleName, "sem."):
		return true
	}
	return false
}

func isTrustedPlatformModule(moduleName string) bool {
	return strings.HasPrefix(moduleName, "platform.") || strings.HasPrefix(moduleName, "machine.x86_64.")
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
		"platform.uefi.transition.DelegatedHardware",
		"platform.acpi.root.AcpiRoot",
		"platform.acpi.tables.AcpiTable",
		"platform.uefi.types.UefiConfigurationTables",
		"platform.acpi.madt.MadtTable",
		"platform.acpi.mcfg.McfgTable",
		"machine.x86_64.interrupts.LocalApic",
		"machine.x86_64.interrupts.ApicModeFacts",
		"machine.x86_64.interrupts.ApicModeSelection",
		"machine.x86_64.interrupts.IoApicDiscovered",
		"machine.x86_64.interrupts.IoApicSet",
		"machine.x86_64.interrupts.InterruptOverrideSet",
		"machine.x86_64.interrupts.InterruptAuthority",
		"machine.x86_64.interrupts.IoApicRoute",
		"machine.x86_64.interrupts.SharedIrqRoute",
		"machine.x86_64.interrupts.SharedInterruptSource",
		"machine.x86_64.executor_memory.ExecutorMemory",
		"machine.x86_64.interrupt_queue.InterruptQueue",
		"machine.x86_64.timer.TimerSource",
		"machine.x86_64.timer.TimerAuthority",
		"machine.x86_64.cpu_state.ExecutorRegistry",
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

func phaseParamHasAuthorityProvenance(phaseName string, typ *Type) bool {
	return phaseName == "delegated_hardware" && qualifiedTypeName(typ) == "platform.uefi.transition.DelegatedHardware"
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
	constructed := c.resolveType(moduleName, legacyTypeName(expr.Type))
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
		return c.resolveType(moduleName, legacyTypeName(e.Type))
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
	case *ast.VariantConstructorExpr:
		return c.staticVariantConstructorType(moduleName, e, scope)
	}
	return nil
}

func (c *checker) recordDiscoveryFactFromField(sel *ast.FieldExpr, recvType *Type) {
	if sel == nil || recvType == nil {
		return
	}
	switch {
	case recvType.Module == "machine.x86_64.cpu_state" && recvType.Name == "CpuLocalityFacts" && sel.Field == "numa_node":
		subject := "unknown_cpu"
		if parent, ok := sel.Base.(*ast.FieldExpr); ok {
			switch parent.Field {
			case "locality0":
				subject = "cpu0"
			case "locality1":
				subject = "cpu1"
			}
		}
		c.graph.LocalityFacts = append(c.graph.LocalityFacts, LocalityFactNode{
			Subject: subject,
			Kind:    "numa_node",
			Value:   "0",
			Known:   false,
			Span:    sel.SpanV,
		})
	case recvType.Module == "platform.hardware.discovery" && recvType.Name == "FramebufferInfo":
		switch sel.Field {
		case "base", "length", "width", "height", "stride", "format", "known":
			c.graph.FramebufferFacts = append(c.graph.FramebufferFacts, FramebufferFactNode{
				Known: false,
				Span:  sel.SpanV,
			})
		}
	}
}

type constValue struct {
	Uint   uint64
	String string
	Bool   bool
	Known  bool
	Fields map[string]constValue
}

func (v constValue) asUint() (uint64, bool) {
	return v.Uint, v.Known
}

func (v constValue) asString() (string, bool) {
	return v.String, v.Known
}

func (v constValue) asBool() (bool, bool) {
	return v.Bool, v.Known
}

func (v constValue) fieldUint(name string) (uint64, bool) {
	if v.Fields == nil {
		return 0, false
	}
	return v.Fields[name].asUint()
}

func (v constValue) fieldString(name string) (string, bool) {
	if v.Fields == nil {
		return "", false
	}
	return v.Fields[name].asString()
}

func (v constValue) fieldBool(name string) (bool, bool) {
	if v.Fields == nil {
		return false, false
	}
	return v.Fields[name].asBool()
}

func callConstArgs(call *ast.CallExpr) map[string]constValue {
	values := map[string]constValue{}
	if call == nil {
		return values
	}
	for _, arg := range call.Args {
		if arg.Name == "" {
			continue
		}
		values[arg.Name] = constValueFromExpr(arg.Value)
	}
	return values
}

func constValueFromExpr(expr ast.Expr) constValue {
	switch e := expr.(type) {
	case *ast.IntLiteral:
		value, ok := unsignedIntegerLiteral(e)
		return constValue{Uint: value, Known: ok}
	case *ast.StringLiteral:
		return constValue{String: e.Value, Known: true}
	case *ast.BoolLiteral:
		return constValue{Bool: e.Value, Known: true}
	case *ast.ConstructorExpr:
		fields := map[string]constValue{}
		for _, field := range e.Args {
			if field.Name == "" {
				continue
			}
			fields[field.Name] = constValueFromExpr(field.Value)
		}
		return constValue{Fields: fields, Known: len(fields) > 0}
	default:
		return constValue{}
	}
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
	if !ok || legacyTypeName(cons.Type) != "InterruptVector" {
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
		return c.withAuthorityProvenance(moduleName, expr, c.originForConstructor(moduleName, e, valueType, scope), valueType, scope)
	case *ast.CallExpr:
		if valueType == nil {
			valueType = c.exprStaticType(moduleName, expr, scope)
		}
		return c.withAuthorityProvenance(moduleName, expr, c.originForCall(moduleName, e, valueType, scope), valueType, scope)
	case *ast.VariantConstructorExpr:
		if valueType == nil {
			valueType = c.exprStaticType(moduleName, expr, scope)
		}
		return c.withAuthorityProvenance(moduleName, expr, c.originForVariantConstructor(moduleName, e, valueType, scope), valueType, scope)
	case *ast.FieldExpr:
		if valueType == nil {
			valueType = c.exprStaticType(moduleName, expr, scope)
		}
		return c.withAuthorityProvenance(moduleName, expr, c.originForField(moduleName, e, valueType, scope), valueType, scope)
	case *ast.NameExpr:
		if origin, ok := scope.LookupOrigin(e.Name); ok {
			return origin
		}
	}
	return c.withAuthorityProvenance(moduleName, expr, localOrigin{Type: valueType}, valueType, scope)
}

func (c *checker) originForLetValue(moduleName string, expr ast.Expr, valueType *Type, scope *Scope) localOrigin {
	return c.originForExprValue(moduleName, expr, valueType, scope)
}

func (c *checker) withAuthorityProvenance(moduleName string, expr ast.Expr, origin localOrigin, valueType *Type, scope *Scope) localOrigin {
	if origin.Type == nil {
		origin.Type = valueType
	}
	origin.AuthorityProvenance = origin.AuthorityProvenance || c.exprHasAuthorityProvenance(moduleName, expr, scope)
	return origin
}

func (c *checker) recordTopicTypeOrigin(origin *localOrigin, topicType *Type, payloadType *Type) {
	if origin == nil || topicType == nil {
		return
	}
	origin.TopicType = topicType.Display()
	origin.TopicTypeKey = topicType.Key()
	if payloadType != nil {
		origin.TopicPayloadKey = payloadType.Key()
	}
	if payloadType != nil {
		if optionType := c.index.instantiateByName("wrela.lang.core", "Option", []*Type{payloadType}); optionType != nil {
			origin.TopicNextType = optionType.Display()
			origin.TopicNextKey = optionType.Key()
		}
	}
}

func (c *checker) originForConstructor(moduleName string, expr *ast.ConstructorExpr, typ *Type, scope *Scope) localOrigin {
	origin := localOrigin{
		Type:          typ,
		Constructor:   expr,
		FieldBindings: map[string]string{},
		FieldOrigins:  map[string]localOrigin{},
	}
	for _, arg := range expr.Args {
		if named, ok := arg.Value.(*ast.NameExpr); ok {
			origin.FieldBindings[arg.Name] = named.Name
		}
		argType := c.exprStaticType(moduleName, arg.Value, scope)
		origin.FieldOrigins[arg.Name] = c.originForExprValue(moduleName, arg.Value, argType, scope)
	}
	if typ == nil {
		return origin
	}
	if IsExecutorSlotType(typ) {
		if id, ok := unsignedIntegerLiteral(constructorArg(expr, "id")); ok {
			origin.SlotLabel = fmt.Sprintf("executor_slot.%d", id)
		}
	}
	if qualifiedTypeName(typ) == "machine.x86_64.cpu_state.CpuFeatureFacts" {
		args := constValueFromExpr(expr)
		if monitor, ok := args.fieldBool("monitor_mwait_available"); ok && monitor {
			origin.LoopStrategy = "monitor_mwait"
		} else {
			origin.LoopStrategy = "sti_hlt"
		}
		origin.LoopFallback = "sti_hlt"
	}
	if qualifiedTypeName(typ) == "machine.x86_64.cpu_state.CpuDiscovery" {
		featureExpr := constructorArg(expr, "features")
		featureType := c.exprStaticType(moduleName, featureExpr, scope)
		featureOrigin := c.originForExprValue(moduleName, featureExpr, featureType, scope)
		origin.LoopStrategy = featureOrigin.LoopStrategy
		origin.LoopFallback = featureOrigin.LoopFallback
		c.rememberCpuFeatureOrigin(origin.LoopStrategy, origin.LoopFallback)
	}
	if qualifiedTypeName(typ) == "machine.x86_64.executor_loop.WakeStrategy" {
		args := constValueFromExpr(expr)
		if monitor, ok := args.fieldBool("monitor_mwait"); ok && monitor {
			origin.LoopStrategy = "monitor_mwait"
		} else {
			origin.LoopStrategy = "sti_hlt"
		}
		if fallback, ok := args.fieldBool("fallback_hlt"); !ok || fallback {
			origin.LoopFallback = "sti_hlt"
		}
	}
	if qualifiedTypeName(typ) == "machine.x86_64.executor_loop.EventSleepPolicy" {
		strategyExpr := constructorArg(expr, "strategy")
		strategyType := c.exprStaticType(moduleName, strategyExpr, scope)
		strategyOrigin := c.originForExprValue(moduleName, strategyExpr, strategyType, scope)
		origin.LoopStrategy = strategyOrigin.LoopStrategy
		origin.LoopFallback = strategyOrigin.LoopFallback
	}
	if qualifiedTypeName(typ) == "machine.x86_64.cpu_state.HardwarePlan" {
		origin.LoopStrategy, origin.LoopFallback = c.hardwarePlanWakeOrigin(moduleName, expr, scope)
		c.rememberHardwarePlanWakeOrigin(origin.LoopStrategy, origin.LoopFallback)
	}
	if qualifiedTypeName(typ) == "platform.hardware.memory.PhysicalRegionAuthority" {
		origin.ArenaBase, _ = unsignedIntegerLiteral(constructorArg(expr, "base"))
		origin.ArenaBytes, _ = unsignedIntegerLiteral(constructorArg(expr, "length"))
		origin.ArenaAlign, _ = unsignedIntegerLiteral(constructorArg(expr, "align"))
	}
	if qualifiedTypeName(typ) == "platform.hardware.bytes.PhysicalBytes" ||
		qualifiedTypeName(typ) == "platform.hardware.bytes.BoundedBytes" {
		origin.AuthorityProvenance = c.exprHasAuthorityProvenance(moduleName, constructorArg(expr, "address"), scope)
	}
	if qualifiedTypeName(typ) == "platform.acpi.root.AcpiRoot" {
		origin.AuthorityProvenance = c.exprHasAuthorityProvenance(moduleName, constructorArg(expr, "root_address"), scope)
	}
	if qualifiedTypeName(typ) == "platform.hardware.memory.RootArena" || qualifiedTypeName(typ) == "platform.hardware.memory.ChildArena" {
		origin.ArenaLabel, _ = arenaIdentityForArg(constructorArg(expr, "identity"))
		if qualifiedTypeName(typ) == "platform.hardware.memory.RootArena" {
			regionExpr := constructorArg(expr, "region")
			regionType := c.exprStaticType(moduleName, regionExpr, scope)
			regionOrigin := c.originForExprValue(moduleName, regionExpr, regionType, scope)
			origin.ArenaBase = regionOrigin.ArenaBase
			origin.ArenaBytes = regionOrigin.ArenaBytes
			origin.ArenaAlign = regionOrigin.ArenaAlign
		} else {
			origin.ArenaBase, _ = unsignedIntegerLiteral(constructorArg(expr, "base"))
		}
		origin.ArenaBytes, _ = unsignedIntegerLiteral(constructorArg(expr, "length"))
	}
	if IsTopicType(typ) {
		if payloadType, kind, ok := TopicPayloadTypeForTopic(typ); ok {
			payloadType = c.resolvePayloadType(moduleName, payloadType)
			c.recordTopicTypeOrigin(&origin, typ, payloadType)
			origin.TopicKind = kind
			origin.TopicPayloadType = qualifiedTypeName(payloadType)
			origin.TopicPayloadKey = payloadType.Key()
			origin.TopicPayloadSize, origin.TopicPayloadAlign, _ = payloadLayoutFromType(payloadType)
		} else {
			origin.TopicKind = topicKindForType(typ)
			if origin.TopicKind != "" {
				origin.TopicPayloadType = "U64"
				origin.TopicPayloadSize = 8
				origin.TopicPayloadAlign = 8
			}
		}
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
	if typ.Module == "machine.x86_64.timer" && typ.Name == "TimerAuthority" {
		if period, ok := unsignedIntegerLiteral(constructorArg(expr, "period_us")); ok {
			origin.TimerLabel = fmt.Sprintf("periodic.%dus", period)
			origin.TimerSource = "local_apic_pit_calibrated"
			origin.TimerPeriodUS = period
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
			if origin.TopicKind == "" {
				origin.TopicKind = publisher.TopicKind
			}
		}
	}
	if typ.Kind == KindExecutor {
		origin.SlotLabel = c.slotLabelForExpr(moduleName, constructorArg(expr, "slot"), scope)
		origin.MemoryOwnerLabel = c.memoryOwnerLabelForExpr(moduleName, constructorArg(expr, "memory"), scope)
		if loopType := c.exprStaticType(moduleName, constructorArg(expr, "loop"), scope); loopType != nil {
			origin.LoopPolicy = loopType.Name
			loopOrigin := c.originForExprValue(moduleName, constructorArg(expr, "loop"), loopType, scope)
			origin.LoopStrategy = loopOrigin.LoopStrategy
			origin.LoopFallback = loopOrigin.LoopFallback
		}
	}
	return origin
}

func (c *checker) originForVariantConstructor(moduleName string, expr *ast.VariantConstructorExpr, typ *Type, scope *Scope) localOrigin {
	origin := localOrigin{
		Type:         typ,
		FieldOrigins: map[string]localOrigin{},
	}
	for _, arg := range expr.Args {
		argType := c.exprStaticType(moduleName, arg.Value, scope)
		origin.FieldOrigins[arg.Name] = c.originForExprValue(moduleName, arg.Value, argType, scope)
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
	case qualifiedTypeName(receiverType) == "platform.uefi.transition.DelegatedHardware" && expr.Method == "uefi_configuration_tables":
		origin.AuthorityProvenance = c.originForExprValue(moduleName, expr.Receiver, receiverType, scope).AuthorityProvenance
	case qualifiedTypeName(receiverType) == "platform.acpi.root.AcpiLocator" && expr.Method == "find":
		origin.AuthorityProvenance = c.exprHasAuthorityProvenance(moduleName, namedArgExpr(expr.Args, "tables"), scope)
	case qualifiedTypeName(receiverType) == "platform.acpi.root.AcpiRoot" &&
		(expr.Method == "root_table" || expr.Method == "require_table" || expr.Method == "require_madt" || expr.Method == "require_mcfg"):
		receiverOrigin := c.originForExprValue(moduleName, expr.Receiver, receiverType, scope)
		origin.AuthorityProvenance = receiverOrigin.AuthorityProvenance && fieldOriginAllowsAuthority(receiverOrigin, "root_address")
	case qualifiedTypeName(receiverType) == "platform.hardware.discovery.PlatformDiscoveryRoot" && expr.Method == "from_uefi":
		origin.AuthorityProvenance = c.exprHasAuthorityProvenance(moduleName, namedArgExpr(expr.Args, "hardware"), scope)
	case qualifiedTypeName(receiverType) == "platform.hardware.bytes.PhysicalBytes" && expr.Method == "bounded":
		origin.AuthorityProvenance = c.originForExprValue(moduleName, expr.Receiver, receiverType, scope).AuthorityProvenance
	case qualifiedTypeName(receiverType) == "platform.hardware.bytes.BoundedBytes" &&
		(expr.Method == "slice" || expr.Method == "read_u8" || expr.Method == "read_u16" || expr.Method == "read_u32" || expr.Method == "read_u64"):
		origin.AuthorityProvenance = c.originForExprValue(moduleName, expr.Receiver, receiverType, scope).AuthorityProvenance
	case qualifiedTypeName(receiverType) == "platform.uefi.types.UefiMemoryMap" && expr.Method == "require_usable_region":
		args := callConstArgs(expr)
		base, _ := args["min_base"].asUint()
		length, _ := args["length"].asUint()
		align, _ := args["align"].asUint()
		return localOrigin{
			Type:       valueType,
			ArenaBase:  base,
			ArenaBytes: length,
			ArenaAlign: align,
		}
	case qualifiedTypeName(receiverType) == "platform.hardware.memory.PhysicalRegionAuthority" && expr.Method == "create_arena":
		receiverOrigin := c.originForExprValue(moduleName, expr.Receiver, receiverType, scope)
		label, _ := arenaIdentityForArg(namedArgExpr(expr.Args, "identity"))
		return localOrigin{
			Type:                valueType,
			AuthorityProvenance: receiverOrigin.AuthorityProvenance,
			ArenaLabel:          label,
			ArenaBase:           receiverOrigin.ArenaBase,
			ArenaBytes:          receiverOrigin.ArenaBytes,
			ArenaAlign:          receiverOrigin.ArenaAlign,
		}
	case qualifiedTypeName(receiverType) == "platform.hardware.memory.RootArena" && (expr.Method == "child" || expr.Method == "child_at"):
		receiverOrigin := c.originForExprValue(moduleName, expr.Receiver, receiverType, scope)
		label, _ := arenaIdentityForArg(namedArgExpr(expr.Args, "identity"))
		offset, hasOffset := arenaUnsignedIntArg(expr, "offset")
		if !hasOffset {
			offset = 0
		}
		if hasOffset || expr.Method == "child" {
			if length, hasLength := arenaUnsignedIntArg(expr, "length"); hasLength {
				if align, hasAlign := arenaUnsignedIntArg(expr, "align"); hasAlign {
					return localOrigin{
						Type:                valueType,
						AuthorityProvenance: receiverOrigin.AuthorityProvenance,
						ArenaLabel:          label,
						ArenaBase:           alignArenaOffset(receiverOrigin.ArenaBase+offset, align),
						ArenaBytes:          length,
						ArenaAlign:          align,
					}
				}
			}
		}
		return localOrigin{Type: valueType, AuthorityProvenance: receiverOrigin.AuthorityProvenance, ArenaLabel: label, ArenaBase: receiverOrigin.ArenaBase}
	case qualifiedTypeName(receiverType) == "platform.hardware.memory.ChildArena" && (expr.Method == "child" || expr.Method == "child_at"):
		receiverOrigin := c.originForExprValue(moduleName, expr.Receiver, receiverType, scope)
		label, _ := arenaIdentityForArg(namedArgExpr(expr.Args, "identity"))
		offset, hasOffset := arenaUnsignedIntArg(expr, "offset")
		if !hasOffset {
			offset = 0
		}
		if hasOffset || expr.Method == "child" {
			if length, hasLength := arenaUnsignedIntArg(expr, "length"); hasLength {
				if align, hasAlign := arenaUnsignedIntArg(expr, "align"); hasAlign {
					return localOrigin{
						Type:                valueType,
						AuthorityProvenance: receiverOrigin.AuthorityProvenance,
						ArenaLabel:          label,
						ArenaBase:           alignArenaOffset(receiverOrigin.ArenaBase+offset, align),
						ArenaBytes:          length,
						ArenaAlign:          align,
					}
				}
			}
		}
		return localOrigin{Type: valueType, AuthorityProvenance: receiverOrigin.AuthorityProvenance, ArenaLabel: label, ArenaBase: receiverOrigin.ArenaBase}
	case receiverType.Module == "machine.x86_64.cpu_state" && receiverType.Name == "ExecutorRegistry" && expr.Method == "claim":
		identity := namedArgExpr(expr.Args, "identity")
		if cons, ok := identity.(*ast.ConstructorExpr); ok {
			origin.SlotLabel, _ = stringLiteralArg(cons, "label")
		}
		receiverOrigin := c.originForExprValue(moduleName, expr.Receiver, receiverType, scope)
		origin.RecordsExecutorSlot = receiverOrigin.ExecutorRegistryAuthority
	case receiverType.Module == "machine.x86_64.cpu_state" && receiverType.Name == "OwnedMemory" && expr.Method == "claim_executor_arena":
		origin.SlotLabel = c.slotLabelForExpr(moduleName, namedArgExpr(expr.Args, "owner"), scope)
		origin.MemoryOwnerLabel = c.resolveExecutorSeedLabel(origin.SlotLabel)
	case qualifiedTypeName(receiverType) == "platform.hardware.memory.RootArena" &&
		(expr.Method == "executor_memory" || expr.Method == "executor_memory_near"):
		origin.SlotLabel = c.slotLabelForExpr(moduleName, namedArgExpr(expr.Args, "owner"), scope)
		origin.MemoryOwnerLabel = c.resolveExecutorSeedLabel(origin.SlotLabel)
	case receiverType.Module == "machine.x86_64.cpu_state" && receiverType.Name == "HardwarePlan" && expr.Method == "executor_memory":
		ownerLabel := c.slotLabelForExpr(moduleName, namedArgExpr(expr.Args, "owner"), scope)
		origin.SlotLabel = c.resolveExecutorSeedLabel(ownerLabel)
		origin.MemoryOwnerLabel = c.memoryOwnerLabelForExpr(moduleName, namedArgExpr(expr.Args, "memory"), scope)
		if origin.MemoryOwnerLabel == "" {
			origin.MemoryOwnerLabel = origin.SlotLabel
		}
	case receiverType.Module == "machine.x86_64.cpu_state" && (receiverType.Name == "CpuDiscovery" || receiverType.Name == "CpuTopology") && expr.Method == "wake_strategy":
		featureOrigin := c.originForExprValue(moduleName, namedArgExpr(expr.Args, "features"), nil, scope)
		features := callConstArgs(expr)["features"]
		if featureOrigin.LoopStrategy != "" {
			origin.LoopStrategy = featureOrigin.LoopStrategy
		} else if monitor, ok := features.fieldBool("monitor_mwait_available"); ok && monitor {
			origin.LoopStrategy = "monitor_mwait"
		} else {
			origin.LoopStrategy = "sti_hlt"
		}
		origin.LoopFallback = "sti_hlt"
	case receiverType.Module == "machine.x86_64.timer" && receiverType.Name == "TimerDiscovery" && expr.Method == "require_periodic":
		if period, ok := callConstArgs(expr)["period_us"].asUint(); ok {
			origin.TimerLabel = fmt.Sprintf("periodic.%dus", period)
			origin.TimerSource = "local_apic_pit_calibrated"
			origin.TimerPeriodUS = period
		}
	case receiverType.Module == "machine.x86_64.timer" && receiverType.Name == "TimerAuthority" && expr.Method == "subscribe":
		receiverOrigin := c.originForExprValue(moduleName, expr.Receiver, receiverType, scope)
		origin.IsTimerRoute = true
		origin.TimerLabel = receiverOrigin.TimerLabel
		origin.TimerSource = receiverOrigin.TimerSource
		origin.TimerPeriodUS = receiverOrigin.TimerPeriodUS
		if origin.TimerLabel == "" || origin.TimerSource == "" || origin.TimerPeriodUS == 0 {
			fact := c.latestTimerFact()
			if origin.TimerLabel == "" {
				origin.TimerLabel = fact.Label
			}
			if origin.TimerSource == "" {
				origin.TimerSource = fact.Source
			}
			if origin.TimerPeriodUS == 0 {
				origin.TimerPeriodUS = fact.PeriodUS
			}
		}
		origin.TopicLabel = "timer.periodic"
		origin.TopicKind = "timer_tick"
		payloadType := c.resolvePayloadType(moduleName, resolveBuiltinTopicPayload("machine.x86_64.topic_payload", "TimerTickPayload"))
		topicType := c.index.instantiateByName("machine.x86_64.topic", "Topic", []*Type{payloadType})
		c.recordTopicTypeOrigin(&origin, topicType, payloadType)
		origin.TopicPayloadType = qualifiedTypeName(payloadType)
		origin.TopicPayloadKey = payloadType.Key()
		origin.TopicPayloadSize, origin.TopicPayloadAlign, _ = payloadLayoutFromType(payloadType)
		origin.SlotLabel = c.slotLabelForExpr(moduleName, namedArgExpr(expr.Args, "subscriber"), scope)
	case receiverType.Module == "machine.x86_64.interrupts" && receiverType.Name == "InterruptAuthority" && expr.Method == "route_shared_irq":
		args := callConstArgs(expr)
		irq, irqOK := args["irq"].asUint()
		vector, vectorOK := args["vector"].fieldUint("value")
		if irqOK && vectorOK {
			origin.SharedIRQRouteKey = sharedIRQRouteKey(irq, vector)
			origin.SharedIRQVector = int(vector)
		}
	case receiverType.Module == "machine.x86_64.interrupts" && receiverType.Name == "SharedIrqRoute" && expr.Method == "claim_source":
		receiverOrigin := c.originForExprValue(moduleName, expr.Receiver, receiverType, scope)
		origin.SharedIRQRouteKey = receiverOrigin.SharedIRQRouteKey
		origin.SharedIRQVector = receiverOrigin.SharedIRQVector
		origin.SharedSourceLabel, _ = callConstArgs(expr)["identity"].fieldString("label")
	case receiverType.Module == "machine.x86_64.pci" &&
		receiverType.Name == "PciDeviceSet" &&
		expr.Method == "require_device":
		origin.PciDeviceKey = pciDeviceKeyFromRequireDevice(expr)
	case IsTopicType(receiverType) && expr.Method == "subscribe":
		receiverOrigin := c.originForExprValue(moduleName, expr.Receiver, receiverType, scope)
		origin.TopicLabel = receiverOrigin.TopicLabel
		origin.TopicType = receiverOrigin.TopicType
		origin.TopicTypeKey = receiverOrigin.TopicTypeKey
		origin.TopicKind = receiverOrigin.TopicKind
		origin.TopicPayloadType = receiverOrigin.TopicPayloadType
		origin.TopicPayloadKey = receiverOrigin.TopicPayloadKey
		origin.TopicPayloadSize = receiverOrigin.TopicPayloadSize
		origin.TopicPayloadAlign = receiverOrigin.TopicPayloadAlign
		origin.TopicNextType = receiverOrigin.TopicNextType
		origin.TopicNextKey = receiverOrigin.TopicNextKey
		origin.SlotLabel = c.slotLabelForExpr(moduleName, namedArgExpr(expr.Args, "subscriber"), scope)
	case IsTopicType(receiverType) && expr.Method == "publisher":
		receiverOrigin := c.originForExprValue(moduleName, expr.Receiver, receiverType, scope)
		origin.TopicLabel = receiverOrigin.TopicLabel
		origin.TopicType = receiverOrigin.TopicType
		origin.TopicTypeKey = receiverOrigin.TopicTypeKey
		origin.TopicKind = receiverOrigin.TopicKind
		origin.TopicPayloadType = receiverOrigin.TopicPayloadType
		origin.TopicPayloadKey = receiverOrigin.TopicPayloadKey
		origin.TopicPayloadSize = receiverOrigin.TopicPayloadSize
		origin.TopicPayloadAlign = receiverOrigin.TopicPayloadAlign
		origin.TopicNextType = receiverOrigin.TopicNextType
		origin.TopicNextKey = receiverOrigin.TopicNextKey
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
			if origin.TopicKind == "" {
				origin.TopicKind = publisher.TopicKind
			}
		}
	case IsVcpuType(receiverType) && (expr.Method == "start" || expr.Method == "enter"):
		origin = c.vcpuOrigin(expr, scope)
		origin.Type = valueType
	}
	return origin
}

func (c *checker) originForField(moduleName string, expr *ast.FieldExpr, valueType *Type, scope *Scope) localOrigin {
	origin := localOrigin{Type: valueType}
	baseType := c.exprStaticType(moduleName, expr.Base, scope)
	baseOrigin := c.originForExprValue(moduleName, expr.Base, baseType, scope)
	if fieldOrigin, ok := baseOrigin.FieldOrigins[expr.Field]; ok {
		fieldOrigin.Type = valueType
		return fieldOrigin
	}
	if authorityFieldCarriesProvenance(baseType, expr.Field) {
		origin.AuthorityProvenance = baseOrigin.AuthorityProvenance
	}
	switch {
	case qualifiedTypeName(valueType) == "machine.x86_64.cpu_state.CpuFeatureFacts" &&
		qualifiedTypeName(baseType) == "machine.x86_64.cpu_state.CpuDiscovery" &&
		expr.Field == "features":
		origin.LoopStrategy = baseOrigin.LoopStrategy
		origin.LoopFallback = baseOrigin.LoopFallback
		if origin.LoopStrategy == "" {
			origin.LoopStrategy = c.cpuFeatureLoopStrategy
			origin.LoopFallback = c.cpuFeatureLoopFallback
		}
		if origin.LoopStrategy == "" {
			origin.LoopStrategy = "sti_hlt"
			origin.LoopFallback = "sti_hlt"
		}
	case qualifiedTypeName(valueType) == "machine.x86_64.cpu_state.ExecutorRegistry" &&
		qualifiedTypeName(baseType) == "machine.x86_64.cpu_state.OwnedHardware" &&
		expr.Field == "executors":
		origin.ExecutorRegistryAuthority = true
	case qualifiedTypeName(valueType) == "machine.x86_64.executor_memory.ExecutorMemory" &&
		qualifiedTypeName(baseType) == "machine.x86_64.cpu_state.HardwarePlan":
		switch expr.Field {
		case "console_memory":
			origin.SlotLabel = "executor_slot.0"
			origin.MemoryOwnerLabel = c.resolveExecutorSeedLabel(origin.SlotLabel)
		case "worker_memory":
			origin.SlotLabel = "executor_slot.1"
			origin.MemoryOwnerLabel = c.resolveExecutorSeedLabel(origin.SlotLabel)
		}
	case qualifiedTypeName(valueType) == "machine.x86_64.cpu_state.HardwarePlan" &&
		qualifiedTypeName(baseType) == "machine.x86_64.cpu_state.OwnedHardware" &&
		expr.Field == "hardware_plan":
		origin.LoopStrategy = c.hardwarePlanWakeStrategy
		origin.LoopFallback = c.hardwarePlanWakeFallback
	case qualifiedTypeName(valueType) == "machine.x86_64.executor_loop.WakeStrategy" &&
		qualifiedTypeName(baseType) == "machine.x86_64.cpu_state.HardwarePlan" &&
		expr.Field == "wake_strategy":
		origin.LoopStrategy = baseOrigin.LoopStrategy
		origin.LoopFallback = baseOrigin.LoopFallback
		if origin.LoopStrategy == "" {
			origin.LoopStrategy = c.hardwarePlanWakeStrategy
			origin.LoopFallback = c.hardwarePlanWakeFallback
		}
		if origin.LoopStrategy == "" {
			origin.LoopStrategy = "sti_hlt"
			origin.LoopFallback = "sti_hlt"
		}
	case qualifiedTypeName(valueType) == "machine.x86_64.timer.TimerAuthority" &&
		qualifiedTypeName(baseType) == "machine.x86_64.cpu_state.HardwarePlan" &&
		expr.Field == "timer":
		fact := c.latestTimerFact()
		origin.TimerLabel = fact.Label
		origin.TimerSource = fact.Source
		origin.TimerPeriodUS = fact.PeriodUS
	}
	return origin
}

func (c *checker) latestTimerFact() TimerFactNode {
	if c == nil {
		return TimerFactNode{}
	}
	for i := len(c.graph.TimerFacts) - 1; i >= 0; i-- {
		if c.graph.TimerFacts[i].Label != "" || c.graph.TimerFacts[i].Source != "" || c.graph.TimerFacts[i].PeriodUS != 0 {
			return c.graph.TimerFacts[i]
		}
	}
	return TimerFactNode{}
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
	case IsExecutorSlotType(origin.Type) && origin.RecordsExecutorSlot:
		c.graph.ExecutorSlots = append(c.graph.ExecutorSlots, ExecutorSlotNode{Label: origin.SlotLabel, Binding: name, Span: span})
	case IsTopicType(origin.Type):
		c.graph.Topics = append(c.graph.Topics, TopicNode{Label: origin.TopicLabel, Type: origin.TopicType, TypeKey: origin.TopicTypeKey, Kind: origin.TopicKind, Depth: origin.TopicDepth, PayloadType: origin.TopicPayloadType, PayloadKey: origin.TopicPayloadKey, PayloadSize: origin.TopicPayloadSize, PayloadAlign: origin.TopicPayloadAlign, NextType: origin.TopicNextType, NextKey: origin.TopicNextKey, Binding: name, Span: span})
	case IsTopicPublisherType(origin.Type):
		c.graph.TopicPublishers = append(c.graph.TopicPublishers, TopicPublisherNode{TopicLabel: origin.TopicLabel, Binding: name, Span: span})
	case IsTopicSubscriptionType(origin.Type):
		if origin.IsTimerRoute && c.graph.TopicByLabel(origin.TopicLabel).Label == "" {
			c.graph.Topics = append(c.graph.Topics, TopicNode{
				Label:        origin.TopicLabel,
				Type:         origin.TopicType,
				TypeKey:      origin.TopicTypeKey,
				Kind:         origin.TopicKind,
				Depth:        64,
				PayloadType:  origin.TopicPayloadType,
				PayloadKey:   origin.TopicPayloadKey,
				PayloadSize:  origin.TopicPayloadSize,
				PayloadAlign: origin.TopicPayloadAlign,
				NextType:     origin.TopicNextType,
				NextKey:      origin.TopicNextKey,
				Binding:      name + ".topic",
				Span:         span,
			})
		}
		c.graph.TopicSubscriptions = append(c.graph.TopicSubscriptions, TopicSubscriptionNode{TopicLabel: origin.TopicLabel, SubscriberLabel: origin.SlotLabel, Binding: name, Span: span})
		if origin.IsTimerRoute {
			c.recordTimerRoute(TimerRouteNode{
				Label:           origin.TimerLabel,
				Source:          origin.TimerSource,
				PeriodUS:        origin.TimerPeriodUS,
				Vector:          0x43,
				TopicLabel:      origin.TopicLabel,
				SubscriberSlots: []string{origin.SlotLabel},
				Span:            span,
			})
		}
	case origin.SharedIRQRouteKey != "" && origin.SharedSourceLabel != "":
		c.recordSharedInterruptSourceOrigin(origin, span)
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

func (c *checker) recordTimerRoute(route TimerRouteNode) {
	for i := range c.graph.TimerRoutes {
		existing := &c.graph.TimerRoutes[i]
		if existing.Label != route.Label || existing.Vector != route.Vector || existing.TopicLabel != route.TopicLabel {
			continue
		}
		// A timer route can be observed through multiple subscriptions; preserve
		// one route row and merge subscriber slots.
		existing.SubscriberSlots = appendUniqueString(existing.SubscriberSlots, route.SubscriberSlots...)
		return
	}
	c.graph.TimerRoutes = append(c.graph.TimerRoutes, route)
}

func appendUniqueString(out []string, values ...string) []string {
	for _, value := range values {
		seen := false
		for _, existing := range out {
			if existing == value {
				seen = true
				break
			}
		}
		if !seen {
			out = append(out, value)
		}
	}
	return out
}

func (c *checker) resolvePayloadType(moduleName string, typ *Type) *Type {
	if typ == nil || len(typ.Fields) != 0 {
		return typ
	}
	if resolved := c.resolveType(moduleName, qualifiedTypeName(typ)); resolved != nil {
		return resolved
	}
	if resolved := c.resolveType(typ.Module, typ.Name); resolved != nil {
		return resolved
	}
	return typ
}

func (c *checker) recordSharedInterruptSourceOrigin(origin localOrigin, span source.Span) {
	if origin.SharedIRQRouteKey == "" || origin.SharedSourceLabel == "" {
		return
	}
	if c.seenSharedIRQSource == nil {
		c.seenSharedIRQSource = map[string]bool{}
	}
	key := origin.SharedIRQRouteKey + "|" + origin.SharedSourceLabel
	if c.seenSharedIRQSource[key] {
		c.error(span, diag.SEM0062, "duplicate shared interrupt source "+origin.SharedSourceLabel)
		return
	}
	c.seenSharedIRQSource[key] = true
	c.graph.SharedInterruptSources = append(c.graph.SharedInterruptSources, SharedInterruptSourceNode{
		RouteKey:    origin.SharedIRQRouteKey,
		SourceLabel: origin.SharedSourceLabel,
		Vector:      origin.SharedIRQVector,
		Span:        span,
	})
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
	valueType := c.exprStaticType(moduleName, expr, scope)
	c.recordSharedInterruptSourceOrigin(c.originForExprValue(moduleName, expr, valueType, scope), call.SpanV)
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
			c.error(call.SpanV, diag.SEM0055, "interrupt vectors in hardware claims must be source literals")
			return
		}
		c.graph.HardwareClaims = append(c.graph.HardwareClaims, HardwareClaimNode{Kind: "interrupt_vector", Key: vectorKey, Span: call.SpanV})
	case qualifiedTypeName(receiverType) == "machine.x86_64.interrupts.InterruptAuthority" && call.Method == "route_shared_irq":
		c.graph.HardwareClaims = append(c.graph.HardwareClaims, HardwareClaimNode{Kind: "isa_irq", Key: literalArgKey(call, "irq"), Span: call.SpanV})
		vectorKey := interruptVectorArgKey(call)
		if strings.HasPrefix(vectorKey, "<") {
			c.error(call.SpanV, diag.SEM0055, "interrupt vectors in hardware claims must be source literals")
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
			c.error(call.SpanV, diag.SEM0055, "interrupt vectors in hardware claims must be source literals")
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

func (c *checker) recordDiscoveryFactFromCall(call *ast.CallExpr, recvType *Type, args map[string]constValue) {
	if call == nil || recvType == nil {
		return
	}
	switch {
	case recvType.Module == "machine.x86_64.interrupts" && recvType.Name == "InterruptAuthority" && call.Method == "select_apic_mode":
		c.graph.APICFacts = append(c.graph.APICFacts, APICFactNode{
			Mode:            "x2apic_preferred",
			Fallback:        "xapic",
			XAPICAvailable:  true,
			X2APICAvailable: true,
			Span:            call.SpanV,
		})
	case recvType.Module == "machine.x86_64.interrupts" && recvType.Name == "InterruptAuthority" && call.Method == "require_x2apic":
		c.graph.APICFacts = append(c.graph.APICFacts, APICFactNode{
			Mode:            "x2apic_required",
			Required:        true,
			XAPICAvailable:  true,
			X2APICAvailable: true,
			Span:            call.SpanV,
		})
	case recvType.Module == "machine.x86_64.interrupts" && recvType.Name == "ApicModeSelection" && call.Method == "with_xapic_fallback":
		c.graph.APICFacts = append(c.graph.APICFacts, APICFactNode{
			Mode:            "x2apic_with_xapic_fallback",
			Fallback:        "xapic",
			XAPICAvailable:  true,
			X2APICAvailable: true,
			Span:            call.SpanV,
		})
	}
	if recvType.Module == "machine.x86_64.timer" && recvType.Name == "TimerDiscovery" && call.Method == "require_periodic" {
		period, ok := args["period_us"].asUint()
		if !ok {
			return
		}
		c.graph.TimerFacts = append(c.graph.TimerFacts, TimerFactNode{
			Label:    fmt.Sprintf("periodic.%dus", period),
			Source:   "local_apic_pit_calibrated",
			PeriodUS: period,
			Span:     call.SpanV,
		})
	}
	if recvType.Module == "machine.x86_64.cpu_state" && recvType.Name == "CpuPlacementPlan" && call.Method == "cpu_for" {
		c.graph.LocalityFacts = append(c.graph.LocalityFacts, LocalityFactNode{
			Subject: "executor",
			Kind:    "cpu_locality",
			Value:   "unknown",
			Known:   false,
			Span:    call.SpanV,
		})
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
	if !ok || legacyTypeName(cons.Type) != "InterruptVector" {
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
	if qualifiedTypeName(receiverType) != "machine.x86_64.topic.ReliablePublisher" || len(receiverType.TypeArgs) != 1 {
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
			c.graph.Executors[i].LoopStrategy = origin.LoopStrategy
			c.graph.Executors[i].LoopFallback = origin.LoopFallback
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
		LoopStrategy:     origin.LoopStrategy,
		LoopFallback:     origin.LoopFallback,
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

func (c *checker) memoryOwnerLabelForExpr(moduleName string, expr ast.Expr, scope *Scope) string {
	origin := c.originForExprValue(moduleName, expr, c.exprStaticType(moduleName, expr, scope), scope)
	if origin.MemoryOwnerLabel != "" {
		return c.resolveExecutorSeedLabel(origin.MemoryOwnerLabel)
	}
	return c.resolveExecutorSeedLabel(origin.SlotLabel)
}

func (c *checker) resolveExecutorSeedLabel(label string) string {
	const prefix = "executor_slot."
	if !strings.HasPrefix(label, prefix) {
		return label
	}
	id, err := strconv.Atoi(strings.TrimPrefix(label, prefix))
	if err != nil || id < 0 || id >= len(c.graph.ExecutorSlots) {
		return label
	}
	if claimed := c.graph.ExecutorSlots[id].Label; claimed != "" {
		return claimed
	}
	return label
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
	if typ != nil && qualifiedTypeName(typ) == "machine.x86_64.topic.Topic" && len(typ.TypeArgs) == 1 {
		return "topic"
	}
	if typ != nil && qualifiedTypeName(typ) == "machine.x86_64.topic.ReliableTopic" && len(typ.TypeArgs) == 1 {
		return "reliable"
	}
	return ""
}

func pathRouteMetadata(typ *Type) (kind, eventType, eventFunctionSymbol string) {
	switch qualifiedTypeName(typ) {
	case "machine.x86_64.serial.SerialConsolePath":
		return "serial_rx", "wrela.lang.core.Option[U8]", "_wrela_event_fn_machine_x86_64_serial_SerialConsolePath_interrupt"
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
	return c.typeExprExpected(moduleName, expr, scope, ctx, nil)
}

func (c *checker) typeExprExpected(moduleName string, expr ast.Expr, scope *Scope, ctx ContextKind, expected *Type) *Type {
	if expr == nil {
		return nil
	}
	if e, ok := expr.(*ast.VariantConstructorExpr); ok {
		return c.typeVariantConstructorExpr(moduleName, e, scope, ctx, expected)
	}
	return c.typeExprNoExpected(moduleName, expr, scope, ctx)
}

func (c *checker) typeExprNoExpected(moduleName string, expr ast.Expr, scope *Scope, ctx ContextKind) *Type {
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
	case *ast.SizeOfExpr:
		if _, ds := c.index.LookupTypeRef(moduleName, e.Type, c.currentTypeParamMap()); len(ds) != 0 {
			c.diags = append(c.diags, ds...)
		}
		return c.mustType(moduleName, "U64")
	case *ast.AlignOfExpr:
		if _, ds := c.index.LookupTypeRef(moduleName, e.Type, c.currentTypeParamMap()); len(ds) != 0 {
			c.diags = append(c.diags, ds...)
		}
		return c.mustType(moduleName, "U64")
	case *ast.FieldExpr:
		baseType := c.typeExpr(moduleName, e.Base, scope, ctx)
		c.recordDiscoveryFactFromField(e, baseType)
		if isSlotsType(baseType) && e.Field == "address" && !isTrustedAuthorityModule(moduleName) {
			c.error(e.SpanV, diag.SEM0096, "Slots.address is protected")
		}
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
		if e.Op == "-" && isAddressType(left) && isAddressType(right) {
			c.rememberLifetime(e, lifetime)
			return c.mustType(moduleName, "U64")
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
	typeName := expr.Type.String()
	constructed, typeDiags := c.index.LookupTypeRef(moduleName, expr.Type, c.currentTypeParamMap())
	for _, d := range typeDiags {
		c.diags = append(c.diags, d)
	}
	if constructed == nil {
		c.error(expr.SpanV, diag.SEM0002, "unknown constructor type "+typeName)
		return nil
	}
	if constructed.GenericOrigin != nil {
		for _, d := range c.index.completeInstantiation(constructed.Key(), map[string]bool{}) {
			c.diags = append(c.diags, d)
		}
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
		argType := c.typeExprExpected(moduleName, arg.Value, scope, ctx, field.Type)
		fieldSpans[arg.Name] = arg.SpanV
		c.checkTypeAssign(arg.SpanV, field.Type, argType)
		argLifetime := c.lifetimeOfExpr(arg.Value, scope)
		if (constructed.Kind == KindData && !IsDMABufferAuthorityType(constructed)) || (c.allowPlaceConstructor != nil && c.allowPlaceConstructor.expr == expr && constructed.Kind == KindClass) {
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
	c.recordInterruptQueueConstructor(moduleName, expr, constructed, scope, ctx)
	if qualifiedTypeName(constructed) == "machine.x86_64.cpu_state.HardwarePlan" {
		strategy, fallback := c.hardwarePlanWakeOrigin(moduleName, expr, scope)
		c.rememberHardwarePlanWakeOrigin(strategy, fallback)
	}
	if qualifiedTypeName(constructed) == "machine.x86_64.cpu_state.CpuDiscovery" {
		featureExpr := constructorArg(expr, "features")
		featureType := c.exprStaticType(moduleName, featureExpr, scope)
		featureOrigin := c.originForExprValue(moduleName, featureExpr, featureType, scope)
		c.rememberCpuFeatureOrigin(featureOrigin.LoopStrategy, featureOrigin.LoopFallback)
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
	c.recordStorageWriterConstructor(moduleName, expr, constructed, scope)
	c.recordStoragePathConstructor(moduleName, expr, constructed, scope)
	if constructed.Kind == KindData || (c.allowPlaceConstructor != nil && c.allowPlaceConstructor.expr == expr && constructed.Kind == KindClass) {
		c.rememberLifetime(expr, constructorLifetime)
	} else {
		c.rememberLifetime(expr, Lifetime{Kind: LifetimeExecutorRoot})
	}
	return constructed
}

func (c *checker) hardwarePlanWakeOrigin(moduleName string, expr *ast.ConstructorExpr, scope *Scope) (string, string) {
	strategyExpr := constructorArg(expr, "wake_strategy")
	strategyType := c.exprStaticType(moduleName, strategyExpr, scope)
	strategyOrigin := c.originForExprValue(moduleName, strategyExpr, strategyType, scope)
	return strategyOrigin.LoopStrategy, strategyOrigin.LoopFallback
}

func (c *checker) rememberHardwarePlanWakeOrigin(strategy string, fallback string) {
	if strategy == "" {
		return
	}
	c.hardwarePlanWakeStrategy = strategy
	c.hardwarePlanWakeFallback = fallback
}

func (c *checker) rememberCpuFeatureOrigin(strategy string, fallback string) {
	if strategy == "" {
		return
	}
	c.cpuFeatureLoopStrategy = strategy
	c.cpuFeatureLoopFallback = fallback
}

func (c *checker) checkConstructorPermissions(moduleName string, expr *ast.ConstructorExpr, typ *Type, scope *Scope, ctx ContextKind) {
	if typ.Module == "machine.x86_64.executor_memory" && typ.Name == "ArenaFrame" {
		c.error(expr.SpanV, diag.SEM0029, "ArenaFrame can only be created by with arena.frame(length = ...)")
		return
	}
	if isProtectedViewType(typ) {
		if !isTrustedAuthorityModule(moduleName) || !c.protectedViewConstructorHasProvenance(moduleName, expr, typ, scope) {
			c.error(expr.SpanV, diag.SEM0092, "protected memory-region view construction is not allowed here")
			return
		}
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
		if moduleName == "platform.hardware.memory" {
			return
		}
		if ctx != ContextImagePhaseDirect || c.currentPhase != "delegated_hardware" || !constructorArgsAreIntegerLiterals(expr, "address", "length") {
			c.error(expr.SpanV, diag.SEM0028, "raw physical byte authority can only be created directly in delegated_hardware phase")
			return
		}
	}
	if IsPhysicalRegionAuthorityType(typ) || IsArenaAuthorityType(typ) {
		if !isTrustedPlatformModule(moduleName) {
			c.error(expr.SpanV, diag.SEM0056, "physical region and arena authorities cannot be forged")
			return
		}
	}
	if isInterruptQueueType(typ) && ctx == ContextImagePhaseDirect {
		return
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

func (c *checker) protectedViewConstructorHasProvenance(moduleName string, expr *ast.ConstructorExpr, typ *Type, scope *Scope) bool {
	switch qualifiedTypeName(typ) {
	case "platform.hardware.bytes.Mmio", "platform.hardware.bytes.Volatile":
		return c.exprHasAuthorityProvenance(moduleName, constructorArg(expr, "address"), scope)
	case "platform.uefi.types.FirmwareSlice":
		if c.exprHasAuthorityProvenance(moduleName, constructorArg(expr, "address"), scope) &&
			c.exprHasAuthorityProvenance(moduleName, constructorArg(expr, "length"), scope) {
			return true
		}
		return c.acpiFirmwareSliceHasBoundedBytesSource(moduleName, expr, scope)
	case "platform.hardware.memory.DmaBuffer":
		return c.exprHasAuthorityProvenance(moduleName, constructorArg(expr, "owner"), scope) &&
			c.exprHasAuthorityProvenance(moduleName, constructorArg(expr, "slots"), scope)
	default:
		if isSlotsType(typ) {
			return c.exprHasAuthorityProvenance(moduleName, constructorArg(expr, "address"), scope) &&
				c.exprHasAuthorityProvenance(moduleName, constructorArg(expr, "capacity"), scope)
		}
		if isSliceType(typ) {
			return c.exprHasAuthorityProvenance(moduleName, constructorArg(expr, "address"), scope) &&
				c.exprHasAuthorityProvenance(moduleName, constructorArg(expr, "length"), scope)
		}
		return false
	}
}

func (c *checker) acpiFirmwareSliceHasBoundedBytesSource(moduleName string, expr *ast.ConstructorExpr, scope *Scope) bool {
	if moduleName != "platform.acpi.tables" {
		return false
	}
	addressCons, ok := constructorArg(expr, "address").(*ast.ConstructorExpr)
	if !ok {
		return false
	}
	addressType, ds := c.index.LookupTypeRef(moduleName, addressCons.Type, c.currentTypeParamMap())
	if len(ds) != 0 || qualifiedTypeName(addressType) != "platform.uefi.types.FirmwareAddress" {
		return false
	}
	addressValue, ok := constructorArg(addressCons, "value").(*ast.FieldExpr)
	if !ok || addressValue.Field != "address" {
		return false
	}
	lengthValue, ok := constructorArg(expr, "length").(*ast.FieldExpr)
	if !ok || lengthValue.Field != "length" {
		return false
	}
	addressBase, ok := addressValue.Base.(*ast.NameExpr)
	if !ok {
		return false
	}
	lengthBase, ok := lengthValue.Base.(*ast.NameExpr)
	if !ok || lengthBase.Name != addressBase.Name {
		return false
	}
	baseType := c.exprStaticType(moduleName, addressValue.Base, scope)
	if qualifiedTypeName(baseType) != "platform.hardware.bytes.BoundedBytes" {
		return false
	}
	return c.exprHasAuthorityProvenance(moduleName, addressValue, scope) &&
		c.exprHasAuthorityProvenance(moduleName, lengthValue, scope)
}

func (c *checker) exprHasAuthorityProvenance(moduleName string, expr ast.Expr, scope *Scope) bool {
	if expr == nil {
		return false
	}
	switch e := expr.(type) {
	case *ast.IntLiteral, *ast.BoolLiteral, *ast.StringLiteral, *ast.SizeOfExpr, *ast.AlignOfExpr:
		return false
	case *ast.NameExpr:
		if scope == nil {
			return false
		}
		if origin, ok := scope.LookupOrigin(e.Name); ok {
			return origin.AuthorityProvenance
		}
		typ, ok := scope.Lookup(e.Name)
		return ok && isAuthorityProvenanceType(typ)
	case *ast.FieldExpr:
		baseType := c.exprStaticType(moduleName, e.Base, scope)
		baseOrigin := c.originForExprValue(moduleName, e.Base, baseType, scope)
		if fieldOrigin, ok := baseOrigin.FieldOrigins[e.Field]; ok {
			return fieldOrigin.AuthorityProvenance
		}
		return authorityFieldCarriesProvenance(baseType, e.Field) && baseOrigin.AuthorityProvenance
	case *ast.CallExpr:
		valueType := c.exprStaticType(moduleName, e, scope)
		return c.originForCall(moduleName, e, valueType, scope).AuthorityProvenance
	case *ast.ConstructorExpr:
		if len(e.Args) == 0 {
			return false
		}
		for _, arg := range e.Args {
			if !c.exprHasAuthorityProvenance(moduleName, arg.Value, scope) {
				return false
			}
		}
		return true
	case *ast.VariantConstructorExpr:
		if len(e.Args) == 0 {
			return false
		}
		for _, arg := range e.Args {
			if !c.exprHasAuthorityProvenance(moduleName, arg.Value, scope) {
				return false
			}
		}
		return true
	case *ast.BinaryExpr:
		return (c.exprHasAuthorityProvenance(moduleName, e.Left, scope) && c.exprHasAuthorityProvenance(moduleName, e.Right, scope)) ||
			c.authoritySideSafeIntegerOffset(moduleName, e, scope)
	default:
		return false
	}
}

func (c *checker) authoritySideSafeIntegerOffset(moduleName string, expr *ast.BinaryExpr, scope *Scope) bool {
	switch expr.Op {
	case "+", "-":
		return c.authoritySideHasIntegerOffset(moduleName, expr.Left, expr.Right, scope) ||
			c.authoritySideHasIntegerOffset(moduleName, expr.Right, expr.Left, scope)
	default:
		return false
	}
}

func (c *checker) authoritySideHasIntegerOffset(moduleName string, authorityExpr, offsetExpr ast.Expr, scope *Scope) bool {
	return c.exprHasAuthorityProvenance(moduleName, authorityExpr, scope) &&
		c.exprIsIntegerConstantExpr(moduleName, offsetExpr, scope)
}

func (c *checker) exprIsIntegerConstantExpr(moduleName string, expr ast.Expr, scope *Scope) bool {
	exprType := c.exprStaticType(moduleName, expr, scope)
	if !isIntegerType(exprType) {
		return false
	}
	_, ok := c.constValueOfExpr(moduleName, expr)
	return ok
}

func isAuthorityProvenanceType(typ *Type) bool {
	return isProtectedViewType(typ) ||
		IsPhysicalRegionAuthorityType(typ) ||
		IsArenaAuthorityType(typ) ||
		IsDMABufferAuthorityType(typ) ||
		isHardwareAuthorityType(typ) ||
		qualifiedTypeName(typ) == "platform.uefi.types.FirmwareAddress"
}

func authorityFieldCarriesProvenance(baseType *Type, field string) bool {
	switch qualifiedTypeName(baseType) {
	case "platform.hardware.bytes.BoundedBytes",
		"platform.hardware.bytes.PhysicalBytes":
		return field == "address" || field == "length"
	case "platform.acpi.root.AcpiRoot":
		return field == "root_address"
	case "platform.acpi.tables.AcpiTable":
		return field == "address" || field == "length" || field == "view"
	case "platform.acpi.tables.AcpiTableView":
		return field == "bytes" || field == "typed"
	case "platform.uefi.types.AcpiRsdpSearchResult":
		return field == "address"
	case "platform.hardware.memory.RootArena":
		return field == "region"
	case "platform.hardware.memory.ChildArena":
		return field == "root" || field == "base" || field == "length"
	case "platform.hardware.discovery.DiscoveredHardware":
		switch field {
		case "memory", "acpi", "interrupts", "cpus", "timers", "pci":
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func fieldOriginAllowsAuthority(origin localOrigin, field string) bool {
	fieldOrigin, ok := origin.FieldOrigins[field]
	return !ok || fieldOrigin.AuthorityProvenance
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
	if isSlotsType(recvType) && (expr.Method == "get" || expr.Method == "read") {
		c.error(expr.SpanV, diag.SEM0093, "raw Slots memory cannot be read directly")
		return nil
	}
	if expr.Method == "reserve_array" {
		return c.typeArenaIntrinsicCall(moduleName, expr, scope, ctx)
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

	if qualifiedTypeName(recvType) == "platform.acpi.root.AcpiRoot" {
		callOrigin := c.originForCall(moduleName, expr, method.Return, scope)
		if !callOrigin.AuthorityProvenance {
			c.error(expr.SpanV, diag.SEM0092, "ACPI root methods require firmware table authority")
		}
	}
	if qualifiedTypeName(recvType) == "platform.acpi.root.AcpiLocator" && expr.Method == "find" {
		tablesArg := callArgForParam(method, expr.Args, explicitParamIndex(method, "tables"))
		if !c.exprHasAuthorityProvenance(moduleName, tablesArg, scope) {
			c.error(expr.SpanV, diag.SEM0092, "ACPI root discovery requires firmware table authority")
		}
	}
	if qualifiedTypeName(recvType) == "platform.hardware.discovery.PlatformDiscoveryRoot" && expr.Method == "from_uefi" {
		hardwareArg := callArgForParam(method, expr.Args, explicitParamIndex(method, "hardware"))
		if !c.exprHasAuthorityProvenance(moduleName, hardwareArg, scope) {
			c.error(expr.SpanV, diag.SEM0092, "hardware discovery requires delegated hardware authority")
		}
	}
	if qualifiedTypeName(recvType) == "platform.acpi.tables.AcpiHelpers" && expr.Method == "table_at" {
		addressArg := callArgForParam(method, expr.Args, explicitParamIndex(method, "address"))
		if !c.exprHasAuthorityProvenance(moduleName, addressArg, scope) {
			c.error(expr.SpanV, diag.SEM0092, "ACPI table lookup address must originate from firmware table authority")
		}
	}
	c.typeAndVerifyCallArgs(moduleName, method, expr.Args, scope, ctx)
	c.recordDiscoveryFactFromCall(expr, recvType, callConstArgs(expr))
	c.recordHardwareClaimCall(moduleName, expr, scope, ctx)
	c.recordArenaGraphCall(moduleName, expr, recvType, scope, ctx)
	c.recordPlacementGraphCall(moduleName, expr, recvType, scope)
	c.checkStoragePathSubmitCall(moduleName, expr, recvType, scope)
	c.checkProjectionAdvanceCall(moduleName, expr, recvType, scope)
	c.checkBlobTruthMutation(expr, recvType)
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
					c.checkTypeAssign(arg.SpanV, p.Type, c.typeExprExpected(moduleName, arg.Value, scope, ctx, p.Type))
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
			c.checkTypeAssign(arg.SpanV, p.Type, c.typeExprExpected(moduleName, arg.Value, scope, ctx, p.Type))
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
	if typ.Kind == KindTypeParam {
		if method, methodSpan := c.lookupTraitBoundMethod(typ.Name, name, span); method != nil {
			return method, methodSpan
		}
	}
	return nil, span
}

func (c *checker) lookupTraitBoundMethod(paramName string, methodName string, span source.Span) (*Method, source.Span) {
	if c == nil || c.currentType == nil {
		return nil, span
	}
	for _, bound := range c.currentType.Where {
		if bound.Param != paramName || bound.Trait == nil {
			continue
		}
		for i := range bound.Trait.Methods {
			method := &bound.Trait.Methods[i]
			if method.Name == methodName {
				return method, method.Span
			}
		}
	}
	for _, bound := range c.currentMethodWhere {
		if bound.Param != paramName || bound.Trait == nil {
			continue
		}
		for i := range bound.Trait.Methods {
			method := &bound.Trait.Methods[i]
			if method.Name == methodName {
				return method, method.Span
			}
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
	if target.Key() != "" && target.Key() == value.Key() {
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
