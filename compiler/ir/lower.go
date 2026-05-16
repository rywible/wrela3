package ir

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/layout"
	"github.com/ryanwible/wrela3/compiler/sem"
	"github.com/ryanwible/wrela3/compiler/source"
)

type lowerBinding struct {
	value       Value
	typ         *sem.Type
	bindingName string
}

type lowerScope struct {
	parent *lowerScope
	values map[string]lowerBinding
}

func newLowerScope(parent *lowerScope) *lowerScope {
	return &lowerScope{parent: parent, values: map[string]lowerBinding{}}
}

func (s *lowerScope) define(name string, binding lowerBinding) {
	if s != nil && name != "" {
		if binding.bindingName == "" {
			binding.bindingName = name
		}
		s.values[name] = binding
	}
}

func (s *lowerScope) lookup(name string) (lowerBinding, bool) {
	if s == nil {
		return lowerBinding{}, false
	}
	if binding, ok := s.values[name]; ok {
		return binding, true
	}
	if s.parent == nil {
		return lowerBinding{}, false
	}
	return s.parent.lookup(name)
}

type lowerContext struct {
	checked *sem.CheckedProgram
	program *Program

	modules map[string]*ast.Module
	types   map[string]*sem.Type
	pseudo  map[string]*sem.Type

	valueBindings   map[Value]string
	currentExecutor *sem.ExecutorNode

	stringSeq int
	tempSeq   int
	diags     []diag.Diagnostic

	activeFrames []*FrameBegin
}

func Lower(checked *sem.CheckedProgram) (*Program, []diag.Diagnostic) {
	if checked == nil || checked.Index == nil {
		return nil, []diag.Diagnostic{{
			Phase:   "cg",
			Code:    diag.CG0001,
			Message: "lowering requires a checked semantic program",
		}}
	}

	ctx := newLowerContext(checked)
	imageModule, imageDecl, imageName := ctx.findImage()
	if imageName == "" {
		imageName = "image"
	}
	delegatedSymbol := symbolName("phase", imageModule, imageName, "delegated_hardware")
	ownedSymbol := symbolName("phase", imageModule, imageName, "owned_hardware")

	ctx.program.Entry = EntryAdapter{
		Symbol:                "_wrela_efi_entry",
		DelegatedPhaseSymbol:  delegatedSymbol,
		OwnedPhaseSymbol:      ownedSymbol,
		DelegatedHardwareType: "DelegatedHardware",
		OwnedHardwareType:     typeName(checked.OwnedRoot),
	}
	if ctx.program.Entry.OwnedHardwareType == "" {
		ctx.program.Entry.OwnedHardwareType = "OwnedHardware"
	}

	ctx.lowerInterruptEventsAndHandlers()
	ctx.lowerInterruptContexts()
	ctx.lowerTopicLayouts()
	ctx.lowerVcpuStartPlans()
	ctx.lowerSourceMethods()
	ctx.lowerImagePhases(imageModule, imageName, imageDecl, delegatedSymbol, ownedSymbol)
	ctx.program.AsmMethods = append(ctx.program.AsmMethods, ctx.lowerAsmMethods()...)
	if len(ctx.diags) != 0 {
		return nil, ctx.diags
	}
	return ctx.program, nil
}

func newLowerContext(checked *sem.CheckedProgram) *lowerContext {
	ctx := &lowerContext{
		checked:       checked,
		program:       &Program{Types: map[string]TypeInfo{}},
		modules:       map[string]*ast.Module{},
		types:         map[string]*sem.Type{},
		pseudo:        map[string]*sem.Type{},
		valueBindings: map[Value]string{},
	}
	for _, mod := range checked.Modules {
		ctx.modules[mod.Name] = mod
	}
	ctx.addPrimitiveTypes()
	if checked.Index != nil {
		for moduleName, byName := range checked.Index.ByModule {
			for name, typ := range byName {
				if typ == nil {
					continue
				}
				ctx.types[typeKey(moduleName, name)] = typ
				ctx.types[name] = typ
			}
		}
	}
	for _, typ := range ctx.types {
		ctx.ensureTypeInfo(typ, map[string]bool{})
	}
	return ctx
}

func (ctx *lowerContext) addPrimitiveTypes() {
	for _, primitive := range []struct {
		name  string
		size  int
		align int
	}{
		{"Bool", 1, 1},
		{"U8", 1, 1},
		{"U16", 2, 2},
		{"U32", 4, 4},
		{"U64", 8, 8},
		{"I64", 8, 8},
		{"PhysicalAddress", 8, 8},
		{"VirtualAddress", 8, 8},
		{"never", 0, 1},
		{"void", 0, 1},
	} {
		ctx.program.Types[primitive.name] = TypeInfo{Name: primitive.name, Kind: TypeKindPrimitive, Size: primitive.size, Align: primitive.align, StorageSize: primitive.size, Fields: map[string]FieldInfo{}}
	}
	ctx.program.Types["StringLiteral"] = TypeInfo{
		Name:        "StringLiteral",
		Kind:        TypeKindPrimitive,
		Size:        16,
		Align:       8,
		StorageSize: 16,
		Fields: map[string]FieldInfo{
			"address": {Name: "address", Type: Type{Name: "PhysicalAddress", Kind: TypeKindPrimitive}, Offset: 0, Size: 8, Align: 8},
			"length":  {Name: "length", Type: Type{Name: "U64", Kind: TypeKindPrimitive}, Offset: 8, Size: 8, Align: 8},
		},
		FieldOrder: []string{"address", "length"},
	}
}

func (ctx *lowerContext) ensureTypeInfo(typ *sem.Type, visiting map[string]bool) TypeInfo {
	if typ == nil {
		return TypeInfo{Name: "U64", Kind: TypeKindPrimitive, Size: 8, Align: 8, StorageSize: 8, Fields: map[string]FieldInfo{}}
	}
	if typ.Kind == sem.KindPrimitive {
		if info, ok := ctx.program.Types[typ.Name]; ok {
			return info
		}
	}
	infoKey := typeInfoKey(typ.Module, typ.Name)
	if typ.Module != "" {
		if info, ok := ctx.program.Types[infoKey]; ok && typeInfoReady(info, typ.Name) {
			return info
		}
	} else {
		if info, ok := ctx.program.Types[typ.Name]; ok && typeInfoReady(info, typ.Name) {
			return info
		}
	}
	key := typeKey(typ.Module, typ.Name)
	if visiting[key] {
		return TypeInfo{Name: typ.Name, Module: typ.Module, Kind: semKindToIR(typ.Kind), Size: 8, Align: 8, StorageSize: 8, Fields: map[string]FieldInfo{}}
	}
	visiting[key] = true

	info := TypeInfo{
		Name:   typ.Name,
		Module: typ.Module,
		Kind:   semKindToIR(typ.Kind),
		Fields: map[string]FieldInfo{},
		Align:  1,
	}
	offset := 0
	for _, field := range typ.Fields {
		fieldInfo := ctx.ensureTypeInfo(field.Type, visiting)
		fieldType := ctx.irType(field.Type)
		size := fieldInfo.Size
		align := fieldInfo.Align
		if isHandleRecordKind(fieldType.Kind) {
			size = 8
			align = 8
		}
		if align == 0 {
			align = 8
		}
		offset = layout.AlignUp(offset, align)
		info.Fields[field.Name] = FieldInfo{
			Name:          field.Name,
			Type:          fieldType,
			Offset:        offset,
			Size:          size,
			Align:         align,
			StorageOffset: -1,
		}
		info.FieldOrder = append(info.FieldOrder, field.Name)
		offset += size
		if align > info.Align {
			info.Align = align
		}
	}
	if info.Align == 0 {
		info.Align = 1
	}
	info.Size = layout.AlignUp(offset, info.Align)
	if info.Kind != TypeKindPrimitive && info.Size == 0 {
		info.Size = 8
		info.Align = 8
	}
	storageOffset := info.Size
	for _, semField := range typ.Fields {
		fieldName := semField.Name
		field := info.Fields[fieldName]
		if field.Type.Kind != TypeKindData {
			continue
		}
		fieldInfo := ctx.ensureTypeInfo(semField.Type, visiting)
		storageAlign := fieldInfo.Align
		if storageAlign == 0 {
			storageAlign = 8
		}
		storageOffset = layout.AlignUp(storageOffset, storageAlign)
		field.StorageOffset = storageOffset
		field.StorageSize = fieldInfo.StorageSize
		if field.StorageSize == 0 {
			field.StorageSize = fieldInfo.Size
		}
		info.Fields[fieldName] = field
		storageOffset += field.StorageSize
	}
	info.StorageSize = layout.AlignUp(storageOffset, info.Align)
	if info.StorageSize == 0 {
		info.StorageSize = info.Size
	}
	if typ.Module != "" {
		ctx.program.Types[infoKey] = info
	}
	ctx.program.Types[typ.Name] = info
	delete(visiting, key)
	return info
}

func typeInfoReady(info TypeInfo, name string) bool {
	return info.Size != 0 || info.Align != 0 || len(info.Fields) != 0 || name == "never" || name == "void"
}

func (ctx *lowerContext) findImage() (string, *ast.ImageDecl, string) {
	for _, mod := range ctx.checked.Modules {
		for _, decl := range mod.Decls {
			if image, ok := decl.(*ast.ImageDecl); ok {
				return mod.Name, image, image.Name
			}
		}
	}
	moduleName, imageName := firstImageType(ctx.checked)
	return moduleName, nil, imageName
}

func (ctx *lowerContext) lowerPhase(moduleName, imageName, symbol string, phase *ast.PhaseDecl) Function {
	params := make([]Value, 0, len(phase.Params))
	scope := newLowerScope(nil)
	for _, param := range phase.Params {
		typ := ctx.resolveType(moduleName, param.Type)
		p := &Param{Symbol: param.Name, Type: ctx.irType(typ)}
		params = append(params, p)
		scope.define(param.Name, lowerBinding{value: p, typ: typ})
	}
	assigned := assignedNames(phase.Body)
	ops := ctx.lowerStmtList(moduleName, nil, scope, assigned, phase.Body)
	return Function{
		Symbol:              symbol,
		Return:              ctx.irType(ctx.resolveType(moduleName, phase.Return)),
		Params:              params,
		Blocks:              []Block{{Label: "entry", Ops: ops}},
		PreserveStackReturn: phase.Name == "delegated_hardware",
	}
}

func (ctx *lowerContext) lowerImagePhases(moduleName, imageName string, imageDecl *ast.ImageDecl, delegatedSymbol, ownedSymbol string) {
	if imageDecl == nil {
		return
	}
	for i := range imageDecl.Phases {
		phase := &imageDecl.Phases[i]
		switch phase.Name {
		case "delegated_hardware":
			ctx.program.Functions = append(ctx.program.Functions, ctx.lowerPhase(moduleName, imageName, delegatedSymbol, phase))
		case "owned_hardware":
			ctx.program.Functions = append(ctx.program.Functions, ctx.lowerPhase(moduleName, imageName, ownedSymbol, phase))
		}
	}
}

func (ctx *lowerContext) lowerSourceMethods() {
	for _, mod := range ctx.checked.Modules {
		for _, decl := range mod.Decls {
			typeName, methods, ok := compositeMethods(decl)
			if !ok {
				continue
			}
			receiverType := ctx.resolveType(mod.Name, typeName)
			for i := range methods {
				method := &methods[i]
				if method.IsAsm {
					continue
				}
				if receiverType != nil && receiverType.Kind == sem.KindExecutor {
					placements := ctx.executorPlacementsForType(receiverType)
					if len(placements) > 1 {
						for _, exec := range placements {
							ctx.currentExecutor = exec
							ctx.program.Functions = append(ctx.program.Functions, ctx.lowerMethodWithSymbol(mod.Name, receiverType, method, executorMethodSymbolForSlot(receiverType, method.Name, exec.SlotLabel)))
						}
						ctx.currentExecutor = nil
						continue
					}
				}
				ctx.currentExecutor = nil
				ctx.program.Functions = append(ctx.program.Functions, ctx.lowerMethod(mod.Name, receiverType, method))
			}
		}
	}
}

func (ctx *lowerContext) executorPlacementsForType(receiverType *sem.Type) []*sem.ExecutorNode {
	if ctx == nil || ctx.checked == nil || receiverType == nil {
		return nil
	}
	placed := map[string]bool{}
	for _, placement := range ctx.checked.ImageGraph.VcpuPlacements {
		if placement.SlotLabel != "" {
			placed[placement.SlotLabel] = true
		}
	}
	var out []*sem.ExecutorNode
	for i := range ctx.checked.ImageGraph.Executors {
		exec := &ctx.checked.ImageGraph.Executors[i]
		if exec.SlotLabel == "" || !placed[exec.SlotLabel] || !sameSemType(exec.Type, receiverType) {
			continue
		}
		out = append(out, exec)
	}
	return out
}

func (ctx *lowerContext) lowerInterruptEventsAndHandlers() {
	for _, mod := range ctx.checked.Modules {
		for _, decl := range mod.Decls {
			switch d := decl.(type) {
			case *ast.DriverPathDecl:
				pathType := ctx.resolveType(mod.Name, d.Name)
				for i := range d.InterruptEvents {
					ctx.lowerInterruptEvent(mod.Name, pathType, &d.InterruptEvents[i])
				}
			case *ast.ExecutorDecl:
				executorType := ctx.resolveType(mod.Name, d.Name)
				for i := range d.OnHandlers {
					ctx.lowerOnHandler(mod.Name, executorType, &d.OnHandlers[i])
				}
			}
		}
	}
	ctx.lowerInterruptBindings()
}

func (ctx *lowerContext) lowerInterruptEvent(moduleName string, pathType *sem.Type, event *ast.InterruptEventDecl) {
	scope := newLowerScope(nil)
	self := &Param{Symbol: "self", Type: ctx.irType(pathType)}
	scope.define("self", lowerBinding{value: self, typ: pathType})
	retType := ctx.resolveType(moduleName, event.EventType)
	ops := ctx.lowerStmtList(moduleName, pathType, scope, assignedNames(event.Body), event.Body)
	fnSymbol := symbolName("event_fn", moduleName, pathType.Name, "interrupt")
	ctx.program.Functions = append(ctx.program.Functions, Function{
		Symbol: fnSymbol,
		Return: ctx.irType(retType),
		Params: []Value{self},
		Blocks: []Block{{Label: "entry", Ops: ops}},
	})
	ctx.program.InterruptEvents = append(ctx.program.InterruptEvents, InterruptEvent{
		Symbol:         logicalSymbol("interrupt_event", moduleName, pathType.Name, "interrupt"),
		PathType:       ctx.irType(pathType),
		EventType:      ctx.irType(retType),
		FunctionSymbol: fnSymbol,
	})
}

func (ctx *lowerContext) lowerOnHandler(moduleName string, executorType *sem.Type, handler *ast.OnHandlerDecl) {
	scope := newLowerScope(nil)
	self := &Param{Symbol: "self", Type: ctx.irType(executorType)}
	scope.define("self", lowerBinding{value: self, typ: executorType})
	eventType := ctx.resolveType(moduleName, handler.ParamType)
	event := &Param{Symbol: handler.ParamName, Type: ctx.irType(eventType)}
	scope.define(handler.ParamName, lowerBinding{value: event, typ: eventType})
	ops := ctx.lowerStmtList(moduleName, executorType, scope, assignedNames(handler.Body), handler.Body)
	fnSymbol := symbolName("on_fn", moduleName, executorType.Name, handler.PathField, "interrupt")
	ctx.program.Functions = append(ctx.program.Functions, Function{
		Symbol: fnSymbol,
		Return: Type{Name: "void", Module: "builtin", Kind: TypeKindPrimitive},
		Params: []Value{self, event},
		Blocks: []Block{{Label: "entry", Ops: ops}},
	})
	ctx.program.OnHandlers = append(ctx.program.OnHandlers, OnHandler{
		Symbol:         logicalSymbol("on_handler", moduleName, executorType.Name, handler.PathField, "interrupt"),
		ExecutorType:   ctx.irType(executorType),
		PathField:      handler.PathField,
		EventType:      ctx.irType(eventType),
		FunctionSymbol: fnSymbol,
	})
}

func (ctx *lowerContext) lowerInterruptBindings() {
	vectorRoutes := map[int]int{}
	for _, route := range ctx.checked.ImageGraph.InterruptTopicRoutes {
		if route.Vector != 0 {
			vectorRoutes[route.Vector]++
		}
	}
	ambiguousVectors := map[int]bool{}
	reportedAmbiguous := map[int]bool{}
	for vector, count := range vectorRoutes {
		if count > 1 {
			ambiguousVectors[vector] = true
		}
	}
	for _, route := range ctx.checked.ImageGraph.InterruptTopicRoutes {
		if route.Vector == 0 {
			ctx.addDiag(route.Span, diag.SEM0043, "interrupt route missing vector")
			continue
		}
		if ambiguousVectors[route.Vector] {
			if !reportedAmbiguous[route.Vector] {
				ctx.addDiag(route.Span, diag.SEM0043, "interrupt route vector is ambiguous")
				reportedAmbiguous[route.Vector] = true
			}
			continue
		}
		eventType := ctx.irType(ctx.resolveType("", route.EventType))
		eventInfo, ok := typeInfoFor(ctx.program.Types, eventType)
		if !ok {
			ctx.errorf("missing type info for interrupt event %s.%s", eventType.Module, eventType.Name)
			continue
		}
		ctx.program.InterruptBindings = append(ctx.program.InterruptBindings, InterruptBinding{
			EventSymbol:         logicalSymbol("interrupt_event", pathModule(route.EventType), pathName(route.EventType), "interrupt"),
			EventFunctionSymbol: route.EventFunctionSymbol,
			PathFieldOffset:     route.PathFieldOffset,
			ContextSymbol:       route.ContextSymbol,
			EventStorageSymbol:  fmt.Sprintf("_wrela_interrupt_event_%02x", route.Vector),
			EventStorageSize:    storageSizeOrEight(eventInfo),
			Vector:              uint8(route.Vector),
			TopicLabel:          route.TopicLabel,
			TopicKind:           route.TopicKind,
			PublisherOwnerKind:  "driver_path",
			PublisherOwnerLabel: route.PathLabel,
			SubscriberSlots:     append([]string{}, route.SubscriberSlots...),
		})
	}
	sort.Slice(ctx.program.InterruptBindings, func(i, j int) bool {
		return ctx.program.InterruptBindings[i].Vector < ctx.program.InterruptBindings[j].Vector
	})
}

func (ctx *lowerContext) lowerInterruptContexts() {
	if len(ctx.checked.ImageGraph.InterruptTopicRoutes) > 0 {
		return
	}
	byExecutor := map[string][]int{}
	for i, binding := range ctx.program.InterruptBindings {
		key := binding.ExecutorType.Module + "." + binding.ExecutorType.Name
		byExecutor[key] = append(byExecutor[key], i)
	}
	keys := make([]string, 0, len(byExecutor))
	for key := range byExecutor {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for seq, key := range keys {
		bindingIndexes := byExecutor[key]
		executorType := ctx.program.InterruptBindings[bindingIndexes[0]].ExecutorType
		info, ok := typeInfoFor(ctx.program.Types, executorType)
		if !ok {
			ctx.errorf("missing type info for interrupt executor %s.%s", executorType.Module, executorType.Name)
			continue
		}
		context := InterruptContext{
			Symbol:       fmt.Sprintf("_wrela_interrupt_context_%d", seq),
			ExecutorType: executorType,
			Size:         info.StorageSize,
		}
		for _, index := range bindingIndexes {
			binding := ctx.program.InterruptBindings[index]
			ctx.program.InterruptBindings[index].ContextSymbol = context.Symbol
			field, ok := info.Fields[binding.PathField]
			if !ok {
				ctx.errorf("missing interrupt path field %s on executor %s.%s", binding.PathField, executorType.Module, executorType.Name)
				continue
			}
			context.PathFields = append(context.PathFields, InterruptContextPathField{
				FieldName: binding.PathField,
				Offset:    field.Offset,
				Type:      field.Type,
			})
		}
		ctx.program.InterruptContexts = append(ctx.program.InterruptContexts, context)
	}
}

func (ctx *lowerContext) lowerMethod(moduleName string, receiverType *sem.Type, method *ast.MethodDecl) Function {
	return ctx.lowerMethodWithSymbol(moduleName, receiverType, method, symbolName("method", moduleName, receiverType.Name, method.Name))
}

func (ctx *lowerContext) lowerMethodWithSymbol(moduleName string, receiverType *sem.Type, method *ast.MethodDecl, symbol string) Function {
	params := []Value{}
	scope := newLowerScope(nil)

	self := &Param{Symbol: "self", Type: ctx.irType(receiverType)}
	params = append(params, self)
	scope.define("self", lowerBinding{value: self, typ: receiverType})
	ctx.rememberValueBinding(self, "self")

	for _, param := range method.Params {
		if param.Name == "self" {
			continue
		}
		typ := ctx.resolveType(moduleName, param.Type)
		p := &Param{Symbol: param.Name, Type: ctx.irType(typ)}
		params = append(params, p)
		scope.define(param.Name, lowerBinding{value: p, typ: typ})
		ctx.rememberValueBinding(p, param.Name)
	}

	assigned := assignedNames(method.Body)
	ops := ctx.lowerStmtList(moduleName, receiverType, scope, assigned, method.Body)
	return Function{
		Symbol:              symbol,
		Return:              ctx.irType(ctx.methodDeclReturn(moduleName, method)),
		Params:              params,
		Blocks:              []Block{{Label: "entry", Ops: ops}},
		PreserveStackReturn: ctx.isOwnershipTransferMethod(receiverType, method),
	}
}

func (ctx *lowerContext) isOwnershipTransferMethod(receiverType *sem.Type, method *ast.MethodDecl) bool {
	if receiverType == nil || method == nil || ctx.checked == nil || ctx.checked.OwnedRoot == nil {
		return false
	}
	if receiverType.Kind != sem.KindClass || !receiverType.Unique || !receiverType.DelegatedOnly {
		return false
	}
	if !receiverHasMethodReturning(receiverType, ctx.checked.OwnedRoot) {
		return false
	}
	return sameSemType(ctx.methodDeclReturn(receiverType.Module, method), ctx.checked.OwnedRoot)
}

func (ctx *lowerContext) lowerStmtList(moduleName string, receiverType *sem.Type, scope *lowerScope, assigned map[string]bool, stmts []ast.Stmt) []Operation {
	var ops []Operation
	for _, stmt := range stmts {
		ops = append(ops, ctx.lowerStmt(moduleName, receiverType, scope, assigned, stmt)...)
	}
	return ops
}

func (ctx *lowerContext) lowerStmt(moduleName string, receiverType *sem.Type, scope *lowerScope, assigned map[string]bool, stmt ast.Stmt) []Operation {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		value, valueOps, typ := ctx.lowerExpr(moduleName, receiverType, scope, s.Expr)
		ops := append([]Operation{}, valueOps...)
		if assigned[s.Name] {
			local := &Local{Symbol: s.Name, Type: ctx.irType(typ)}
			ops = append(ops, &Copy{Target: local, Source: value, Type: local.Type})
			scope.define(s.Name, lowerBinding{value: local, typ: typ})
			ctx.rememberValueBinding(local, s.Name)
			ops = append(ops, ctx.interruptContextStoresForPathBinding(s.Name, local, typ)...)
			return ops
		}
		scope.define(s.Name, lowerBinding{value: value, typ: typ})
		ctx.rememberValueBinding(value, s.Name)
		ops = append(ops, ctx.interruptContextStoresForPathBinding(s.Name, value, typ)...)
		return ops
	case *ast.AssignStmt:
		value, valueOps, typ := ctx.lowerExpr(moduleName, receiverType, scope, s.Value)
		ops := append([]Operation{}, valueOps...)
		switch target := s.Target.(type) {
		case *ast.NameExpr:
			binding, ok := scope.lookup(target.Name)
			if !ok {
				ctx.errorf("unknown assignment target %q", target.Name)
				return ops
			}
			ops = append(ops, &Copy{Target: binding.value, Source: value, Type: ctx.irType(binding.typ)})
		case *ast.FieldExpr:
			object, objectOps, objectType := ctx.lowerExpr(moduleName, receiverType, scope, target.Base)
			ops = append(objectOps, ops...)
			field := ctx.lookupField(objectType, target.Field)
			if field == nil {
				ctx.errorf("unknown field %s", target.Field)
				return ops
			}
			ops = append(ops, &FieldStore{Object: object, ObjectType: objectType.Name, Field: target.Field, Value: value, Type: ctx.irType(typ), Offset: ctx.irFieldOffset(objectType, target.Field, field.Offset)})
		default:
			ctx.errorf("unsupported assignment target %T", s.Target)
		}
		return ops
	case *ast.ReturnStmt:
		if s.Value == nil {
			return ctx.lowerReturn(nil, nil)
		}
		value, valueOps, _ := ctx.lowerExpr(moduleName, receiverType, scope, s.Value)
		return ctx.lowerReturn(value, valueOps)
	case *ast.ExprStmt:
		call, ok := s.Expr.(*ast.CallExpr)
		if !ok {
			ctx.errorf("unsupported expression statement %T", s.Expr)
			return nil
		}
		_, ops, _ := ctx.lowerExpr(moduleName, receiverType, scope, call)
		return ops
	case *ast.WhileStmt:
		condition, conditionOps, _ := ctx.lowerExpr(moduleName, receiverType, scope, s.Cond)
		bodyScope := newLowerScope(scope)
		body := ctx.lowerStmtList(moduleName, receiverType, bodyScope, assigned, s.Body)
		return []Operation{&While{ConditionOps: conditionOps, Condition: condition, Body: body}}
	case *ast.ForStmt:
		iterable, iterableOps, _ := ctx.lowerExpr(moduleName, receiverType, scope, s.InExpr)
		loopScope := newLowerScope(scope)
		index := &Local{Symbol: s.Var + "_index", Type: Type{Name: "U64", Kind: TypeKindPrimitive}}
		byteValue := &Local{Symbol: s.Var, Type: Type{Name: "U8", Kind: TypeKindPrimitive}}
		loopScope.define(s.Var, lowerBinding{value: byteValue, typ: ctx.resolveType(moduleName, "U8")})
		body := ctx.lowerStmtList(moduleName, receiverType, loopScope, assigned, s.Body)
		return []Operation{&ForBytes{IterableOps: iterableOps, Iterable: iterable, Index: index, ByteValue: byteValue, Body: body}}
	case *ast.IfStmt:
		if wait, waitOps, ok := ctx.lowerTopicWaitIfArmed(moduleName, receiverType, scope, s); ok {
			return append(waitOps, wait)
		}
		condition, conditionOps, _ := ctx.lowerExpr(moduleName, receiverType, scope, s.Cond)
		thenOps := ctx.lowerStmtList(moduleName, receiverType, newLowerScope(scope), assigned, s.Then)
		elseOps := ctx.lowerStmtList(moduleName, receiverType, newLowerScope(scope), assigned, s.Else)
		return []Operation{&If{ConditionOps: conditionOps, Condition: condition, Then: thenOps, Else: elseOps}}
	case *ast.WithStmt:
		parent, frameOps, length := ctx.lowerFrameCall(moduleName, receiverType, scope, s.Expr)
		frameType := ctx.resolveType("machine.x86_64.executor_memory", "ArenaFrame")
		frame := &FrameBegin{
			Symbol: s.Name,
			Parent: parent,
			Length: length,
			Type:   ctx.irType(frameType),
		}

		child := newLowerScope(scope)
		child.define(s.Name, lowerBinding{value: frame, typ: frameType})

		ctx.activeFrames = append(ctx.activeFrames, frame)
		body := ctx.lowerStmtList(moduleName, receiverType, child, assigned, s.Body)
		ctx.activeFrames = ctx.activeFrames[:len(ctx.activeFrames)-1]

		ops := append([]Operation{}, frameOps...)
		ops = append(ops, frame)
		ops = append(ops, body...)
		ops = append(ops, &FrameEnd{Frame: frame})
		return ops
	default:
		ctx.errorf("unsupported statement %T", stmt)
		return nil
	}
}

func (ctx *lowerContext) lowerTopicWaitIfArmed(moduleName string, receiverType *sem.Type, scope *lowerScope, stmt *ast.IfStmt) (*TopicWaitIfArmed, []Operation, bool) {
	guardCalls, ok := topicWaitGuardCalls(stmt)
	if !ok {
		return nil, nil, false
	}
	var ops []Operation
	var guards []TopicWaitGuard
	for _, guardCall := range guardCalls {
		subscription, subscriptionOps, subscriptionType := ctx.lowerExpr(moduleName, receiverType, scope, guardCall.Receiver)
		if !sem.IsTopicSubscriptionType(subscriptionType) {
			return nil, nil, false
		}
		label, _ := ctx.topicLabelAndKindForValue(subscription)
		if label == "" {
			return nil, nil, false
		}
		ops = append(ops, subscriptionOps...)
		guards = append(guards, TopicWaitGuard{
			TopicLabel:     label,
			SubscriberSlot: ctx.subscriberSlotForValue(subscription, receiverType),
			Subscription:   subscription,
		})
	}
	wait := &TopicWaitIfArmed{
		TopicLabel:     guards[0].TopicLabel,
		SubscriberSlot: guards[0].SubscriberSlot,
		Subscription:   guards[0].Subscription,
		Guards:         guards,
	}
	return wait, ops, true
}

func topicWaitGuardCalls(stmt *ast.IfStmt) ([]*ast.CallExpr, bool) {
	if stmt == nil || len(stmt.Else) != 0 || len(stmt.Then) != 1 {
		return nil, false
	}
	condCall, ok := stmt.Cond.(*ast.CallExpr)
	if !ok || condCall.Method != "is_wait_armed" {
		return nil, false
	}
	switch then := stmt.Then[0].(type) {
	case *ast.ExprStmt:
		thenCall, ok := then.Expr.(*ast.CallExpr)
		if !ok || thenCall.Method != "wait" {
			return nil, false
		}
		return []*ast.CallExpr{condCall}, true
	case *ast.IfStmt:
		nested, ok := topicWaitGuardCalls(then)
		if !ok {
			return nil, false
		}
		return append([]*ast.CallExpr{condCall}, nested...), true
	default:
		return nil, false
	}
}

func (ctx *lowerContext) lowerTopicFactoryCall(moduleName string, receiverType *sem.Type, scope *lowerScope, call *ast.CallExpr, receiver Value, receiverOps []Operation, recvType *sem.Type) (Value, []Operation, *sem.Type, bool) {
	if !sem.IsTopicType(recvType) || (call.Method != "publisher" && call.Method != "subscribe") {
		return nil, nil, nil, false
	}
	method := ctx.lookupMethod(recvType, call.Method)
	ret := ctx.methodReturn(moduleName, method)
	if ret == nil {
		return nil, nil, nil, false
	}
	ops := append([]Operation{}, receiverOps...)
	fields := []FieldValue{{Name: "topic", Value: receiver}}
	if call.Method == "subscribe" {
		subscriber, subscriberOps, _ := ctx.lowerExpr(moduleName, receiverType, scope, namedArgExpr(call.Args, "subscriber"))
		cursor := &ConstInt{Symbol: "topic_cursor", Value: 0, Type: ctx.irType(ctx.resolveType(moduleName, "U64"))}
		armed := &ConstInt{Symbol: "topic_armed", Value: 0, Type: ctx.irType(ctx.resolveType(moduleName, "Bool"))}
		ops = append(ops, subscriberOps...)
		ops = append(ops, cursor, armed)
		fields = append(fields,
			FieldValue{Name: "subscriber", Value: subscriber},
			FieldValue{Name: "cursor", Value: cursor},
			FieldValue{Name: "armed", Value: armed},
		)
	}
	construct := &Construct{Symbol: ret.Name, Type: ctx.irType(ret), Fields: fields}
	ops = append(ops, construct)
	return construct, ops, ret, true
}

func (ctx *lowerContext) lowerSerialConsolePathFactoryCall(moduleName string, receiverType *sem.Type, scope *lowerScope, call *ast.CallExpr, receiver Value, receiverOps []Operation, recvType *sem.Type) (Value, []Operation, *sem.Type, bool) {
	if qualifiedSemTypeName(recvType) != "machine.x86_64.serial.SerialDriver" || call.Method != "create_console_path" {
		return nil, nil, nil, false
	}
	method := ctx.lookupMethod(recvType, call.Method)
	ret := ctx.methodReturn(moduleName, method)
	if ret == nil {
		return nil, nil, nil, false
	}
	identity, identityOps, _ := ctx.lowerExpr(moduleName, receiverType, scope, namedArgExpr(call.Args, "identity"))
	rx, rxOps, _ := ctx.lowerExpr(moduleName, receiverType, scope, namedArgExpr(call.Args, "rx"))
	registersField := ctx.lookupField(recvType, "registers")
	if registersField == nil {
		ctx.errorf("SerialDriver.create_console_path requires registers field")
		return receiver, receiverOps, recvType, true
	}
	registers := &FieldLoad{
		Object:     receiver,
		ObjectType: recvType.Name,
		Field:      "registers",
		Type:       registersField.Type,
		Offset:     ctx.irFieldOffset(recvType, "registers", registersField.Offset),
	}
	construct := &Construct{
		Symbol: "SerialConsolePath",
		Type:   ctx.irType(ret),
		Fields: []FieldValue{
			{Name: "identity", Value: identity},
			{Name: "registers", Value: registers},
			{Name: "rx", Value: rx},
		},
	}
	enable := &Call{
		Symbol:   symbolName("method", ret.Module, ret.Name, "enable_receive_interrupts"),
		Receiver: construct,
		Type:     Type{Name: "void", Module: "builtin", Kind: TypeKindPrimitive},
	}
	ops := append([]Operation{}, receiverOps...)
	ops = append(ops, identityOps...)
	ops = append(ops, rxOps...)
	ops = append(ops, registers, construct, enable)
	return construct, ops, ret, true
}

func qualifiedSemTypeName(typ *sem.Type) string {
	if typ == nil {
		return ""
	}
	if typ.Module == "" {
		return typ.Name
	}
	return typ.Module + "." + typ.Name
}

func (ctx *lowerContext) lowerReturn(value Value, prefix []Operation) []Operation {
	ops := append([]Operation{}, prefix...)
	for i := len(ctx.activeFrames) - 1; i >= 0; i-- {
		ops = append(ops, &FrameEnd{Frame: ctx.activeFrames[i]})
	}
	if value == nil {
		ops = append(ops, &Return{})
	} else {
		ops = append(ops, &Return{Value: value})
	}
	return ops
}

func (ctx *lowerContext) lowerFrameCall(moduleName string, receiverType *sem.Type, scope *lowerScope, expr ast.Expr) (Value, []Operation, Value) {
	call, ok := expr.(*ast.CallExpr)
	if !ok || call.Method != "frame" {
		ctx.errorf("with expression was not a frame call")
		zero := &ConstInt{Value: 0, Type: Type{Name: "U64", Kind: TypeKindPrimitive}}
		return zero, []Operation{zero}, zero
	}

	parent, parentOps, _ := ctx.lowerExpr(moduleName, receiverType, scope, call.Receiver)
	lengthExpr := namedArgExpr(call.Args, "length")
	length, lengthOps, _ := ctx.lowerExpr(moduleName, receiverType, scope, lengthExpr)
	ops := append([]Operation{}, parentOps...)
	ops = append(ops, lengthOps...)
	return parent, ops, length
}

func namedArgExpr(args []ast.NamedArg, name string) ast.Expr {
	for _, arg := range args {
		if arg.Name == name {
			return arg.Value
		}
	}
	if name == "value" && len(args) == 1 && args[0].Name == "" {
		return args[0].Value
	}
	if name == "executor" && len(args) == 1 && args[0].Name == "" {
		return args[0].Value
	}
	return nil
}

func (ctx *lowerContext) rememberValueBinding(value Value, name string) {
	if ctx == nil || value == nil || name == "" {
		return
	}
	ctx.valueBindings[value] = name
}

func (ctx *lowerContext) lowerTopicLayouts() {
	if ctx == nil || ctx.checked == nil {
		return
	}
	for _, topic := range ctx.checked.ImageGraph.Topics {
		layout := TopicLayout{Label: topic.Label, Kind: topic.Kind, Depth: topic.Depth}
		for _, sub := range ctx.checked.ImageGraph.TopicSubscriptions {
			if sub.TopicLabel == topic.Label {
				layout.Subscribers = append(layout.Subscribers, sub.SubscriberLabel)
			}
		}
		layout.Producers = ctx.publisherSlotsForTopic(topic.Label)
		ctx.program.Topics = append(ctx.program.Topics, layout)
	}
}

func (ctx *lowerContext) lowerVcpuStartPlans() {
	if ctx == nil || ctx.checked == nil {
		return
	}
	for _, placement := range ctx.checked.ImageGraph.VcpuPlacements {
		plan := VcpuStartPlan{
			VcpuID:    placement.VcpuID,
			SlotLabel: placement.SlotLabel,
			Terminal:  placement.Terminal,
		}
		if exec := ctx.checked.ImageGraph.ExecutorBySlot(placement.SlotLabel); exec.Type != nil {
			plan.ExecutorType = ctx.irType(exec.Type)
			if len(ctx.executorPlacementsForType(exec.Type)) > 1 {
				plan.EntrySymbol = executorMethodSymbolForSlot(exec.Type, "run", placement.SlotLabel)
			}
		}
		ctx.program.VcpuStarts = append(ctx.program.VcpuStarts, plan)
	}
}

func executorMethodSymbolForSlot(executorType *sem.Type, methodName, slotLabel string) string {
	return symbolName("method", executorType.Module, executorType.Name, methodName, sanitizeSymbolName(slotLabel))
}

func sanitizeSymbolName(label string) string {
	replacer := strings.NewReplacer(".", "_", "/", "_", "-", "_", " ", "_", ":", "_")
	return replacer.Replace(label)
}

func (ctx *lowerContext) topicLabelAndKindForValue(value Value) (string, string) {
	label := ctx.topicLabelForValue(value)
	if ctx == nil || ctx.checked == nil {
		return label, ""
	}
	topic := ctx.checked.ImageGraph.TopicByLabel(label)
	return label, topic.Kind
}

func (ctx *lowerContext) publisherSlotsForTopic(topicLabel string) []string {
	if ctx == nil || ctx.checked == nil || topicLabel == "" {
		return nil
	}
	publisherBindings := map[string]bool{}
	for _, publisher := range ctx.checked.ImageGraph.TopicPublishers {
		if publisher.TopicLabel == topicLabel && publisher.Binding != "" {
			publisherBindings[publisher.Binding] = true
		}
	}
	if len(publisherBindings) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, exec := range ctx.checked.ImageGraph.Executors {
		for _, binding := range exec.FieldBindings {
			if !publisherBindings[binding] || exec.SlotLabel == "" || seen[exec.SlotLabel] {
				continue
			}
			seen[exec.SlotLabel] = true
			out = append(out, exec.SlotLabel)
		}
	}
	sort.Strings(out)
	return out
}

func (ctx *lowerContext) publisherSlotForValue(value Value, currentExecutor *sem.Type) string {
	if ctx == nil || ctx.checked == nil {
		return ""
	}
	binding := ctx.bindingNameForValue(value)
	if binding == "" {
		if load, ok := value.(*FieldLoad); ok {
			binding = ctx.fieldBindingForLoad(load, currentExecutor)
		}
	}
	if binding == "" {
		return ctx.currentExecutorSlotLabel(currentExecutor)
	}
	for _, exec := range ctx.checked.ImageGraph.Executors {
		for _, fieldBinding := range exec.FieldBindings {
			if fieldBinding == binding {
				return exec.SlotLabel
			}
		}
	}
	return ""
}

func (ctx *lowerContext) executorForValue(value Value) *sem.ExecutorNode {
	if ctx == nil || ctx.checked == nil {
		return nil
	}
	binding := ctx.bindingNameForValue(value)
	if binding == "" {
		return nil
	}
	for _, placement := range ctx.checked.ImageGraph.VcpuPlacements {
		if placement.ExecutorBinding == binding {
			for i := range ctx.checked.ImageGraph.Executors {
				if ctx.checked.ImageGraph.Executors[i].SlotLabel == placement.SlotLabel {
					return &ctx.checked.ImageGraph.Executors[i]
				}
			}
		}
	}
	return nil
}

func (ctx *lowerContext) subscriberSlotForValue(value Value, currentExecutor *sem.Type) string {
	if ctx == nil || ctx.checked == nil {
		return ""
	}
	if binding := ctx.bindingNameForValue(value); binding != "" {
		for _, sub := range ctx.checked.ImageGraph.TopicSubscriptions {
			if sub.Binding == binding {
				return sub.SubscriberLabel
			}
		}
	}
	if load, ok := value.(*FieldLoad); ok {
		binding := ctx.fieldBindingForLoad(load, currentExecutor)
		for _, sub := range ctx.checked.ImageGraph.TopicSubscriptions {
			if sub.Binding == binding {
				return sub.SubscriberLabel
			}
		}
	}
	return ""
}

func (ctx *lowerContext) fieldBindingForLoad(load *FieldLoad, currentExecutor *sem.Type) string {
	if ctx == nil || ctx.checked == nil || load == nil {
		return ""
	}
	if self, ok := load.Object.(*Param); !ok || self.Symbol != "self" {
		return ""
	}
	if ctx.currentExecutor != nil && sameSemType(ctx.currentExecutor.Type, currentExecutor) {
		return ctx.currentExecutor.FieldBindings[load.Field]
	}
	for _, exec := range ctx.checked.ImageGraph.Executors {
		if !sameSemType(exec.Type, currentExecutor) {
			continue
		}
		return exec.FieldBindings[load.Field]
	}
	return ""
}

func (ctx *lowerContext) topicLabelForValue(value Value) string {
	if ctx == nil || ctx.checked == nil || value == nil {
		return ""
	}
	if binding := ctx.bindingNameForValue(value); binding != "" {
		for _, topic := range ctx.checked.ImageGraph.Topics {
			if topic.Binding == binding {
				return topic.Label
			}
		}
		for _, publisher := range ctx.checked.ImageGraph.TopicPublishers {
			if publisher.Binding == binding {
				return publisher.TopicLabel
			}
		}
		for _, sub := range ctx.checked.ImageGraph.TopicSubscriptions {
			if sub.Binding == binding {
				return sub.TopicLabel
			}
		}
	}
	switch v := value.(type) {
	case *FieldLoad:
		if self, ok := v.Object.(*Param); ok && self.Symbol == "self" {
			if ctx.currentExecutor != nil {
				if binding := ctx.currentExecutor.FieldBindings[v.Field]; binding != "" {
					return ctx.topicLabelForBinding(binding)
				}
			}
			for _, exec := range ctx.checked.ImageGraph.Executors {
				if exec.Type == nil || exec.Type.Name != v.ObjectType {
					continue
				}
				if binding := exec.FieldBindings[v.Field]; binding != "" {
					return ctx.topicLabelForBinding(binding)
				}
			}
		}
	case *Call:
		if strings.HasSuffix(v.Symbol, "_publisher") {
			return ctx.topicLabelForValue(v.Receiver)
		}
	case *Construct:
		for _, field := range v.Fields {
			if field.Name == "topic" {
				return ctx.topicLabelForValue(field.Value)
			}
		}
	}
	return ""
}

func (ctx *lowerContext) topicLabelForBinding(binding string) string {
	for _, publisher := range ctx.checked.ImageGraph.TopicPublishers {
		if publisher.Binding == binding {
			return publisher.TopicLabel
		}
	}
	for _, sub := range ctx.checked.ImageGraph.TopicSubscriptions {
		if sub.Binding == binding {
			return sub.TopicLabel
		}
	}
	for _, topic := range ctx.checked.ImageGraph.Topics {
		if topic.Binding == binding {
			return topic.Label
		}
	}
	return ""
}

func (ctx *lowerContext) interruptContextStoresForPathBinding(binding string, value Value, typ *sem.Type) []Operation {
	if ctx == nil || ctx.checked == nil || binding == "" || typ == nil || typ.Kind != sem.KindDriverPath {
		return nil
	}
	var ops []Operation
	for _, route := range ctx.checked.ImageGraph.InterruptTopicRoutes {
		if route.PathBinding != binding || route.ContextSymbol == "" {
			continue
		}
		ops = append(ops, &InterruptContextStore{
			ContextSymbol: route.ContextSymbol,
			ContextOffset: route.PathFieldOffset,
			Source:        value,
			SourceType:    ctx.irType(typ),
			Size:          8,
		})
	}
	return ops
}

func (ctx *lowerContext) slotLabelForExecutorValue(value Value) string {
	if ctx == nil || ctx.checked == nil {
		return ""
	}
	binding := ctx.bindingNameForValue(value)
	for _, placement := range ctx.checked.ImageGraph.VcpuPlacements {
		if placement.ExecutorBinding == binding {
			return placement.SlotLabel
		}
	}
	for _, exec := range ctx.checked.ImageGraph.Executors {
		for _, fieldBinding := range exec.FieldBindings {
			if fieldBinding == binding {
				return exec.SlotLabel
			}
		}
	}
	return ""
}

func (ctx *lowerContext) vcpuFieldsForValue(value Value) (int, uint32, uint64) {
	id := uint64(0)
	apicID := uint64(0)
	localApicBase := uint64(0)
	if v, ok := constUintFieldValue(value, "id"); ok {
		id = v
	} else if load, ok := value.(*FieldLoad); ok {
		switch load.Field {
		case "vcpu0":
			id = 0
		case "vcpu1":
			id = 1
		}
	}
	if v, ok := constUintFieldValue(value, "apic_id"); ok {
		apicID = v
	}
	if v, ok := constUintFieldValue(value, "local_apic_base"); ok {
		localApicBase = v
	}
	return int(id), uint32(apicID), localApicBase
}

func constUintFieldValue(value Value, fieldName string) (uint64, bool) {
	construct, ok := value.(*Construct)
	if !ok {
		return 0, false
	}
	for _, field := range construct.Fields {
		if field.Name != fieldName {
			continue
		}
		return constUintValue(field.Value)
	}
	return 0, false
}

func constUintValue(value Value) (uint64, bool) {
	switch v := value.(type) {
	case *ConstInt:
		return v.Value, true
	case ConstInt:
		return v.Value, true
	default:
		return 0, false
	}
}

func (ctx *lowerContext) vcpuIDForValue(value Value) int {
	if load, ok := value.(*FieldLoad); ok {
		switch load.Field {
		case "vcpu0":
			return 0
		case "vcpu1":
			return 1
		}
	}
	return 0
}

func (ctx *lowerContext) currentExecutorSlotLabel(receiverType *sem.Type) string {
	if ctx == nil || ctx.checked == nil || receiverType == nil {
		return ""
	}
	if ctx.currentExecutor != nil && sameSemType(ctx.currentExecutor.Type, receiverType) {
		return ctx.currentExecutor.SlotLabel
	}
	for _, exec := range ctx.checked.ImageGraph.Executors {
		if sameSemType(exec.Type, receiverType) {
			return exec.SlotLabel
		}
	}
	return ""
}

func (ctx *lowerContext) bindingNameForValue(value Value) string {
	if ctx == nil || value == nil {
		return ""
	}
	if name := ctx.valueBindings[value]; name != "" {
		return name
	}
	switch v := value.(type) {
	case *Local:
		return v.Symbol
	case Local:
		return v.Symbol
	case *Param:
		return v.Symbol
	case Param:
		return v.Symbol
	}
	return ""
}

func (ctx *lowerContext) lowerExpr(moduleName string, receiverType *sem.Type, scope *lowerScope, expr ast.Expr) (Value, []Operation, *sem.Type) {
	switch e := expr.(type) {
	case *ast.NameExpr:
		if binding, ok := scope.lookup(e.Name); ok {
			return binding.value, nil, binding.typ
		}
		typ := ctx.resolveType(moduleName, e.Name)
		if typ != nil {
			value := &Local{Symbol: e.Name, Type: ctx.irType(typ)}
			return value, nil, typ
		}
		ctx.errorf("unknown name %q", e.Name)
		return &ConstInt{Value: 0, Type: Type{Name: "U64", Kind: TypeKindPrimitive}}, nil, ctx.resolveType(moduleName, "U64")
	case *ast.IntLiteral:
		value, err := strconv.ParseUint(e.Value, 0, 64)
		if err != nil {
			ctx.errorf("invalid integer literal %q", e.Value)
			value = 0
		}
		typ := ctx.resolveType(moduleName, "U64")
		c := &ConstInt{Symbol: "const", Value: value, Type: ctx.irType(typ)}
		return c, []Operation{c}, typ
	case *ast.BoolLiteral:
		var raw uint64
		if e.Value {
			raw = 1
		}
		typ := ctx.resolveType(moduleName, "Bool")
		c := &ConstInt{Symbol: "bool", Value: raw, Type: ctx.irType(typ)}
		return c, []Operation{c}, typ
	case *ast.StringLiteral:
		typ := ctx.resolveType(moduleName, "StringLiteral")
		dataSymbol := symbolName("str", moduleName, fmt.Sprintf("%d", ctx.stringSeq))
		ctx.stringSeq++
		ctx.program.Data = append(ctx.program.Data, DataObject{Symbol: dataSymbol, Bytes: append([]byte(e.Value), 0)})
		value := &StringLiteral{Symbol: "string", Value: e.Value, DataSymbol: dataSymbol, Type: ctx.irType(typ)}
		return value, []Operation{value}, typ
	case *ast.FieldExpr:
		object, objectOps, objectType := ctx.lowerExpr(moduleName, receiverType, scope, e.Base)
		field := ctx.lookupField(objectType, e.Field)
		if field == nil {
			ctx.errorf("unknown field %s", e.Field)
			return object, objectOps, objectType
		}
		load := &FieldLoad{
			Object:     object,
			ObjectType: objectType.Name,
			Field:      e.Field,
			Type:       field.Type,
			Offset:     ctx.irFieldOffset(objectType, e.Field, field.Offset),
		}
		return load, append(objectOps, load), ctx.resolveType(field.Type.Module, field.Type.Name)
	case *ast.ConstructorExpr:
		typ := ctx.resolveType(moduleName, e.Type)
		var ops []Operation
		fields := make([]FieldValue, 0, len(e.Args))
		for _, arg := range e.Args {
			value, valueOps, _ := ctx.lowerExpr(moduleName, receiverType, scope, arg.Value)
			ops = append(ops, valueOps...)
			fields = append(fields, FieldValue{Name: arg.Name, Value: value})
		}
		construct := &Construct{Symbol: e.Type, Type: ctx.irType(typ), Fields: fields}
		ops = append(ops, construct)
		return construct, ops, typ
	case *ast.CallExpr:
		receiver, receiverOps, recvType := ctx.lowerExpr(moduleName, receiverType, scope, e.Receiver)
		if construct, ops, typ, ok := ctx.lowerTopicFactoryCall(moduleName, receiverType, scope, e, receiver, receiverOps, recvType); ok {
			return construct, ops, typ
		}
		if construct, ops, typ, ok := ctx.lowerSerialConsolePathFactoryCall(moduleName, receiverType, scope, e, receiver, receiverOps, recvType); ok {
			return construct, ops, typ
		}
		if sem.IsTopicPublisherType(recvType) {
			switch e.Method {
			case "publish":
				value, valueOps, _ := ctx.lowerExpr(moduleName, receiverType, scope, namedArgExpr(e.Args, "value"))
				label, kind := ctx.topicLabelAndKindForValue(receiver)
				if label == "" {
					break
				}
				publish := TopicPublish{TopicLabel: label, Kind: kind, Value: value}
				ops := append([]Operation{}, receiverOps...)
				ops = append(ops, valueOps...)
				ops = append(ops, publish)
				return receiver, ops, ctx.resolveType(moduleName, "void")
			case "try_publish":
				value, valueOps, _ := ctx.lowerExpr(moduleName, receiverType, scope, namedArgExpr(e.Args, "value"))
				method := ctx.lookupMethod(recvType, e.Method)
				ret := ctx.methodReturn(moduleName, method)
				label, _ := ctx.topicLabelAndKindForValue(receiver)
				if label == "" {
					break
				}
				tryPublish := ReliableTopicTryPublish{TopicLabel: label, Value: value, Type: ctx.irType(ret)}
				ops := append([]Operation{}, receiverOps...)
				ops = append(ops, valueOps...)
				ops = append(ops, tryPublish)
				return tryPublish, ops, ret
			case "publish_or_wait":
				value, valueOps, _ := ctx.lowerExpr(moduleName, receiverType, scope, namedArgExpr(e.Args, "value"))
				method := ctx.lookupMethod(recvType, "try_publish")
				ret := ctx.methodReturn(moduleName, method)
				label, _ := ctx.topicLabelAndKindForValue(receiver)
				if label == "" {
					break
				}
				fullField := ctx.lookupField(ret, "full")
				if fullField == nil {
					ctx.errorf("publish_or_wait requires U64PublishResult.full")
					break
				}
				publisherSlot := ctx.publisherSlotForValue(receiver, receiverType)
				result := ctx.tempLocal("publish_or_wait_result", ctx.irType(ret))
				initialTry := &ReliableTopicTryPublish{TopicLabel: label, Value: value, Type: ctx.irType(ret)}
				initialCopy := &Copy{Target: result, Source: initialTry, Type: ctx.irType(ret)}
				condition := &FieldLoad{
					Object:     result,
					ObjectType: ret.Name,
					Field:      "full",
					Type:       fullField.Type,
					Offset:     fullField.Offset,
				}
				wait := &ReliableTopicWaitForAdvance{TopicLabel: label, PublisherSlot: publisherSlot}
				retryTry := &ReliableTopicTryPublish{TopicLabel: label, Value: value, Type: ctx.irType(ret)}
				retryCopy := &Copy{Target: result, Source: retryTry, Type: ctx.irType(ret)}
				loop := &While{
					ConditionOps: []Operation{condition},
					Condition:    condition,
					Body:         []Operation{wait, retryTry, retryCopy},
				}
				ops := append([]Operation{}, receiverOps...)
				ops = append(ops, valueOps...)
				ops = append(ops, initialTry, initialCopy, loop)
				return receiver, ops, ctx.resolveType(moduleName, "void")
			case "wait_for_subscriber_advance":
				label, _ := ctx.topicLabelAndKindForValue(receiver)
				if label == "" {
					break
				}
				wait := ReliableTopicWaitForAdvance{TopicLabel: label, PublisherSlot: ctx.publisherSlotForValue(receiver, receiverType)}
				ops := append([]Operation{}, receiverOps...)
				ops = append(ops, wait)
				return receiver, ops, ctx.resolveType(moduleName, "void")
			}
		}
		if sem.IsTopicSubscriptionType(recvType) {
			switch e.Method {
			case "try_next":
				method := ctx.lookupMethod(recvType, e.Method)
				ret := ctx.methodReturn(moduleName, method)
				label, _ := ctx.topicLabelAndKindForValue(receiver)
				if label == "" {
					break
				}
				next := TopicTryNext{TopicLabel: label, SubscriberSlot: ctx.subscriberSlotForValue(receiver, receiverType), Subscription: receiver, Type: ctx.irType(ret)}
				ops := append([]Operation{}, receiverOps...)
				ops = append(ops, next)
				return next, ops, ret
			case "arm_wait":
				label, _ := ctx.topicLabelAndKindForValue(receiver)
				if label == "" {
					break
				}
				arm := TopicArmWait{TopicLabel: label, SubscriberSlot: ctx.subscriberSlotForValue(receiver, receiverType), Subscription: receiver}
				ops := append([]Operation{}, receiverOps...)
				ops = append(ops, arm)
				return receiver, ops, ctx.resolveType(moduleName, "void")
			case "is_wait_armed":
				method := ctx.lookupMethod(recvType, e.Method)
				ret := ctx.methodReturn(moduleName, method)
				label, _ := ctx.topicLabelAndKindForValue(receiver)
				if label == "" {
					break
				}
				armed := TopicIsWaitArmed{TopicLabel: label, SubscriberSlot: ctx.subscriberSlotForValue(receiver, receiverType), Subscription: receiver, Type: ctx.irType(ret)}
				ops := append([]Operation{}, receiverOps...)
				ops = append(ops, armed)
				return armed, ops, ret
			}
		}
		if sem.IsLoopPolicyType(recvType) && e.Method == "wait" {
			wait := TopicWait{SlotLabel: ctx.currentExecutorSlotLabel(receiverType), Policy: recvType.Name}
			ops := append([]Operation{}, receiverOps...)
			ops = append(ops, wait)
			return receiver, ops, ctx.resolveType(moduleName, "void")
		}
		if sem.IsVcpuType(recvType) && (e.Method == "start" || e.Method == "enter") {
			executor, executorOps, _ := ctx.lowerExpr(moduleName, receiverType, scope, namedArgExpr(e.Args, "executor"))
			vcpuID, apicID, localApicBase := ctx.vcpuFieldsForValue(receiver)
			slotLabel := ctx.slotLabelForExecutorValue(executor)
			ops := append([]Operation{}, receiverOps...)
			ops = append(ops, executorOps...)
			if e.Method == "start" {
				ret := ctx.resolveType("machine.x86_64.cpu_state", "VcpuStartStatus")
				start := VcpuStart{VcpuID: vcpuID, APICID: apicID, LocalApicBase: localApicBase, Vcpu: receiver, Executor: executor, SlotLabel: slotLabel, Type: ctx.irType(ret)}
				ops = append(ops, start)
				return start, ops, ret
			}
			enter := VcpuEnter{VcpuID: vcpuID, APICID: apicID, LocalApicBase: localApicBase, Vcpu: receiver, Executor: executor, SlotLabel: slotLabel}
			ops = append(ops, enter)
			return receiver, ops, ctx.resolveType(moduleName, "never")
		}
		if recvType != nil && recvType.Module == "machine.x86_64.cpu_state" && recvType.Name == "OwnedMemory" && e.Method == "claim_executor_arena" {
			_, ownerOps, _ := ctx.lowerExpr(moduleName, receiverType, scope, namedArgExpr(e.Args, "owner"))
			length, lengthOps, _ := ctx.lowerExpr(moduleName, receiverType, scope, namedArgExpr(e.Args, "length"))
			align, alignOps, _ := ctx.lowerExpr(moduleName, receiverType, scope, namedArgExpr(e.Args, "align"))
			mutableBytesType := ctx.resolveType("machine.x86_64.executor_memory", "MutableBytes")
			executorMemoryType := ctx.resolveType("machine.x86_64.executor_memory", "ExecutorMemory")
			u64Type := ctx.resolveType(moduleName, "U64")
			arenaField := ctx.lookupField(recvType, "arena")
			addressField := ctx.lookupField(mutableBytesType, "address")
			lengthField := ctx.lookupField(mutableBytesType, "length")
			if arenaField == nil || addressField == nil || lengthField == nil {
				ctx.errorf("claim_executor_arena missing memory arena fields")
				return receiver, receiverOps, recvType
			}
			arena := &FieldLoad{
				Object:     receiver,
				ObjectType: recvType.Name,
				Field:      "arena",
				Type:       arenaField.Type,
				Offset:     arenaField.Offset,
			}
			arenaBase := &FieldLoad{
				Object:     arena,
				ObjectType: mutableBytesType.Name,
				Field:      "address",
				Type:       addressField.Type,
				Offset:     addressField.Offset,
			}
			arenaLength := &FieldLoad{
				Object:     arena,
				ObjectType: mutableBytesType.Name,
				Field:      "length",
				Type:       lengthField.Type,
				Offset:     lengthField.Offset,
			}
			zero := &ConstInt{Symbol: "const", Value: 0, Type: ctx.irType(u64Type)}
			one := &ConstInt{Symbol: "const", Value: 1, Type: ctx.irType(u64Type)}
			alignMinusOne := &Binary{Op: "sub", Left: align, Right: one, Type: ctx.irType(u64Type)}
			adjustedBase := &Binary{Op: "add", Left: arenaBase, Right: alignMinusOne, Type: arenaBase.Type}
			alignmentMask := &Binary{Op: "sub", Left: zero, Right: align, Type: ctx.irType(u64Type)}
			arenaClaimBase := &Binary{Op: "and", Left: adjustedBase, Right: alignmentMask, Type: arenaBase.Type}
			nextArenaBase := &Binary{Op: "add", Left: arenaClaimBase, Right: length, Type: arenaBase.Type}
			consumed := &Binary{Op: "sub", Left: nextArenaBase, Right: arenaBase, Type: ctx.irType(u64Type)}
			remainingLength := &Binary{Op: "sub", Left: arenaLength, Right: consumed, Type: arenaLength.Type}
			arenaBaseStore := &FieldStore{
				Object:     arena,
				ObjectType: mutableBytesType.Name,
				Field:      "address",
				Value:      nextArenaBase,
				Type:       arenaBase.Type,
				Offset:     addressField.Offset,
			}
			arenaLengthStore := &FieldStore{
				Object:     arena,
				ObjectType: mutableBytesType.Name,
				Field:      "length",
				Value:      remainingLength,
				Type:       arenaLength.Type,
				Offset:     lengthField.Offset,
			}
			construct := &Construct{
				Symbol: "ExecutorMemory",
				Type:   ctx.irType(executorMemoryType),
				Fields: []FieldValue{
					{Name: "arena_base", Value: arenaClaimBase},
					{Name: "arena_length", Value: length},
					{Name: "next_offset", Value: zero},
				},
			}
			ops := append([]Operation{}, receiverOps...)
			ops = append(ops, ownerOps...)
			ops = append(ops, lengthOps...)
			ops = append(ops, alignOps...)
			ops = append(ops, arena, arenaBase, arenaLength, zero, one, alignMinusOne, adjustedBase, alignmentMask, arenaClaimBase, nextArenaBase, consumed, remainingLength, arenaBaseStore, arenaLengthStore, construct)
			return construct, ops, executorMemoryType
		}
		if sem.IsArenaType(recvType) {
			switch e.Method {
			case "reserve":
				length, lengthOps, _ := ctx.lowerExpr(moduleName, receiverType, scope, namedArgExpr(e.Args, "length"))
				align, alignOps, _ := ctx.lowerExpr(moduleName, receiverType, scope, namedArgExpr(e.Args, "align"))
				mutableType := ctx.resolveType("machine.x86_64.executor_memory", "MutableBytes")
				reserve := &ArenaReserve{Arena: receiver, Length: length, Align: align, Type: ctx.irType(mutableType)}
				ops := append([]Operation{}, receiverOps...)
				ops = append(ops, lengthOps...)
				ops = append(ops, alignOps...)
				ops = append(ops, reserve)
				return reserve, ops, mutableType
			case "place":
				cons, ok := e.Args[0].Value.(*ast.ConstructorExpr)
				if !ok {
					ctx.errorf("place argument was not a constructor")
					return receiver, receiverOps, recvType
				}
				placedType := ctx.resolveType(moduleName, cons.Type)
				fields := make([]FieldValue, 0, len(cons.Args))
				ops := append([]Operation{}, receiverOps...)
				for _, arg := range cons.Args {
					value, valueOps, _ := ctx.lowerExpr(moduleName, receiverType, scope, arg.Value)
					ops = append(ops, valueOps...)
					fields = append(fields, FieldValue{Name: arg.Name, Value: value})
				}
				place := &ArenaPlace{Arena: receiver, Type: ctx.irType(placedType), Fields: fields}
				ops = append(ops, place)
				return place, ops, placedType
			}
		}
		method := ctx.lookupMethod(recvType, e.Method)
		args, argOps := ctx.lowerCallArgs(moduleName, receiverType, scope, method, e.Args)
		ret := ctx.methodReturn(moduleName, method)
		symbol := symbolName("method", recvType.Module, recvType.Name, e.Method)
		if recvType.Kind == sem.KindExecutor && len(ctx.executorPlacementsForType(recvType)) > 1 {
			if ctx.currentExecutor != nil && sameSemType(ctx.currentExecutor.Type, recvType) {
				symbol = executorMethodSymbolForSlot(recvType, e.Method, ctx.currentExecutor.SlotLabel)
			} else if exec := ctx.executorForValue(receiver); exec != nil && sameSemType(exec.Type, recvType) {
				symbol = executorMethodSymbolForSlot(recvType, e.Method, exec.SlotLabel)
			}
		}
		call := &Call{
			Symbol:   symbol,
			Receiver: receiver,
			Args:     args,
			Type:     ctx.irType(ret),
		}
		ops := append(receiverOps, argOps...)
		if recvType.Kind == sem.KindExecutor && method != nil && method.IsStart {
			if context := ctx.interruptContextForExecutor(recvType); context != nil {
				ops = append(ops, &InterruptContextStore{
					ContextSymbol: context.Symbol,
					Source:        receiver,
					SourceType:    ctx.irType(recvType),
					Size:          context.Size,
				})
			}
		}
		ops = append(ops, call)
		return call, ops, ret
	case *ast.BinaryExpr:
		left, leftOps, leftType := ctx.lowerExpr(moduleName, receiverType, scope, e.Left)
		right, rightOps, _ := ctx.lowerExpr(moduleName, receiverType, scope, e.Right)
		resultType := leftType
		op := lowerBinaryOp(e.Op)
		if isLowerComparison(op) {
			resultType = ctx.resolveType(moduleName, "Bool")
		}
		binary := &Binary{Op: op, Left: left, Right: right, Type: ctx.irType(resultType)}
		ops := append(leftOps, rightOps...)
		ops = append(ops, binary)
		return binary, ops, resultType
	default:
		ctx.errorf("unsupported expression %T", expr)
		typ := ctx.resolveType(moduleName, "U64")
		return &ConstInt{Value: 0, Type: ctx.irType(typ)}, nil, typ
	}
}

func (ctx *lowerContext) irFieldOffset(objectType *sem.Type, fieldName string, fallback int) int {
	if objectType == nil {
		return fallback
	}
	info, ok := ctx.program.Types[typeInfoKey(objectType.Module, objectType.Name)]
	if !ok {
		info, ok = ctx.program.Types[objectType.Name]
	}
	if !ok {
		return fallback
	}
	if field, ok := info.Fields[fieldName]; ok {
		return field.Offset
	}
	return fallback
}

func (ctx *lowerContext) lowerCallArgs(moduleName string, receiverType *sem.Type, scope *lowerScope, method *sem.Method, args []ast.NamedArg) ([]Value, []Operation) {
	_ = receiverType
	var values []Value
	var ops []Operation
	if method == nil {
		for _, arg := range args {
			value, valueOps, _ := ctx.lowerExpr(moduleName, nil, scope, arg.Value)
			ops = append(ops, valueOps...)
			values = append(values, value)
		}
		return values, ops
	}
	params := method.Params
	if len(params) > 0 && params[0].Name == "self" {
		params = params[1:]
	}
	for i, param := range params {
		var argExpr ast.Expr
		for j := range args {
			if args[j].Name == param.Name || (args[j].Name == "" && j == i) {
				argExpr = args[j].Value
				break
			}
		}
		if argExpr == nil {
			continue
		}
		value, valueOps, _ := ctx.lowerExpr(moduleName, nil, scope, argExpr)
		ops = append(ops, valueOps...)
		values = append(values, value)
	}
	return values, ops
}

func (ctx *lowerContext) lookupField(typ *sem.Type, fieldName string) *FieldInfo {
	if typ == nil {
		return nil
	}
	info := ctx.ensureTypeInfo(typ, map[string]bool{})
	field, ok := info.Fields[fieldName]
	if !ok {
		return nil
	}
	return &field
}

func (ctx *lowerContext) tempLocal(prefix string, typ Type) *Local {
	ctx.tempSeq++
	return &Local{
		Symbol: fmt.Sprintf("__%s_%d", prefix, ctx.tempSeq),
		Type:   typ,
	}
}

func (ctx *lowerContext) lookupMethod(typ *sem.Type, methodName string) *sem.Method {
	if typ == nil {
		return nil
	}
	for i := range typ.Methods {
		if typ.Methods[i].Name == methodName {
			return &typ.Methods[i]
		}
	}
	return nil
}

func (ctx *lowerContext) interruptContextForExecutor(executorType *sem.Type) *InterruptContext {
	for i := range ctx.program.InterruptContexts {
		candidate := &ctx.program.InterruptContexts[i]
		if candidate.ExecutorType.Module == executorType.Module && candidate.ExecutorType.Name == executorType.Name {
			return candidate
		}
	}
	return nil
}

func (ctx *lowerContext) methodReturn(moduleName string, method *sem.Method) *sem.Type {
	if method == nil || method.Return == nil {
		return ctx.resolveType(moduleName, "void")
	}
	return method.Return
}

func (ctx *lowerContext) methodDeclReturn(moduleName string, method *ast.MethodDecl) *sem.Type {
	if method == nil || method.Return == "" {
		return ctx.resolveType(moduleName, "void")
	}
	return ctx.resolveType(moduleName, method.Return)
}

func (ctx *lowerContext) resolveType(moduleName, raw string) *sem.Type {
	if raw == "" {
		return ctx.resolveType(moduleName, "void")
	}
	if ctx.checked != nil && ctx.checked.Index != nil {
		if typ, ok := ctx.checked.Index.Lookup(moduleName, raw); ok && typ != nil {
			ctx.ensureTypeInfo(typ, map[string]bool{})
			return typ
		}
		if typ := ctx.checked.Index.MustType(raw); typ != nil {
			ctx.ensureTypeInfo(typ, map[string]bool{})
			return typ
		}
	}
	if typ := ctx.types[typeKey(moduleName, raw)]; typ != nil {
		ctx.ensureTypeInfo(typ, map[string]bool{})
		return typ
	}
	if typ := ctx.types[raw]; typ != nil {
		ctx.ensureTypeInfo(typ, map[string]bool{})
		return typ
	}
	key := typeKey(moduleName, raw)
	if typ := ctx.pseudo[key]; typ != nil {
		return typ
	}
	typ := &sem.Type{Module: moduleName, Name: raw, Kind: sem.KindClass}
	if info, ok := ctx.program.Types[raw]; ok && info.Kind == TypeKindPrimitive {
		typ.Kind = sem.KindPrimitive
	}
	ctx.pseudo[key] = typ
	ctx.ensureTypeInfo(typ, map[string]bool{})
	return typ
}

func (ctx *lowerContext) irType(typ *sem.Type) Type {
	if typ == nil {
		return Type{Name: "void", Kind: TypeKindPrimitive}
	}
	return Type{Name: typ.Name, Module: typ.Module, Kind: semKindToIR(typ.Kind)}
}

func (ctx *lowerContext) errorf(format string, args ...any) {
	ctx.diags = append(ctx.diags, diag.Diagnostic{
		Phase:   "cg",
		Code:    diag.CG0001,
		Message: fmt.Sprintf(format, args...),
	})
}

func (ctx *lowerContext) addDiag(span source.Span, code string, message string) {
	ctx.diags = append(ctx.diags, diag.Diagnostic{
		Phase:    "sem",
		Code:     code,
		Severity: diag.Error,
		Start:    span.Start,
		End:      span.End,
		Message:  message,
	})
}

func storageSizeOrEight(info TypeInfo) int {
	if info.StorageSize != 0 {
		return info.StorageSize
	}
	if info.Size != 0 {
		return info.Size
	}
	return 8
}

func assignedNames(stmts []ast.Stmt) map[string]bool {
	out := map[string]bool{}
	var walk func([]ast.Stmt)
	walk = func(stmts []ast.Stmt) {
		for _, stmt := range stmts {
			switch s := stmt.(type) {
			case *ast.AssignStmt:
				if name, ok := s.Target.(*ast.NameExpr); ok {
					out[name.Name] = true
				}
			case *ast.IfStmt:
				walk(s.Then)
				walk(s.Else)
			case *ast.WhileStmt:
				walk(s.Body)
			case *ast.ForStmt:
				walk(s.Body)
			case *ast.WithStmt:
				walk(s.Body)
			}
		}
	}
	walk(stmts)
	return out
}

func compositeMethods(decl ast.Decl) (string, []ast.MethodDecl, bool) {
	switch d := decl.(type) {
	case *ast.DataDecl:
		return d.Name, d.Methods, true
	case *ast.ClassDecl:
		return d.Name, d.Methods, true
	case *ast.DriverDecl:
		return d.Name, d.Methods, true
	case *ast.DriverPathDecl:
		return d.Name, d.Methods, true
	case *ast.ExecutorDecl:
		return d.Name, d.Methods, true
	default:
		return "", nil, false
	}
}

func lowerBinaryOp(op string) string {
	switch op {
	case "+":
		return "add"
	case "-":
		return "sub"
	case "&":
		return "and"
	case "|":
		return "or"
	case "<<":
		return "shl"
	case ">>":
		return "shr"
	case "==":
		return "eq"
	case "!=":
		return "ne"
	case "<":
		return "lt"
	case "<=":
		return "le"
	case ">":
		return "gt"
	case ">=":
		return "ge"
	default:
		return op
	}
}

func isLowerComparison(op string) bool {
	switch op {
	case "eq", "ne", "lt", "le", "gt", "ge":
		return true
	default:
		return false
	}
}

func isHandleRecordKind(kind TypeKind) bool {
	switch kind {
	case TypeKindData, TypeKindClass, TypeKindDriver, TypeKindDriverPath, TypeKindExecutor:
		return true
	default:
		return false
	}
}

func semKindToIR(kind sem.Kind) TypeKind {
	switch kind {
	case sem.KindPrimitive:
		return TypeKindPrimitive
	case sem.KindData:
		return TypeKindData
	case sem.KindClass:
		return TypeKindClass
	case sem.KindDriver:
		return TypeKindDriver
	case sem.KindDriverPath:
		return TypeKindDriverPath
	case sem.KindExecutor:
		return TypeKindExecutor
	case sem.KindImage:
		return TypeKindImage
	default:
		return TypeKindUnknown
	}
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

func (ctx *lowerContext) lowerAsmMethods() []AsmMethod {
	var out []AsmMethod
	if ctx == nil || ctx.checked == nil || ctx.checked.Index == nil {
		return out
	}
	var modules []string
	for moduleName := range ctx.checked.Index.ByModule {
		modules = append(modules, moduleName)
	}
	sort.Strings(modules)
	for _, moduleName := range modules {
		byName := ctx.checked.Index.ByModule[moduleName]
		var typeNames []string
		for typeName := range byName {
			typeNames = append(typeNames, typeName)
		}
		sort.Strings(typeNames)
		for _, name := range typeNames {
			if typ := byName[name]; typ != nil {
				ctx.ensureTypeInfo(typ, map[string]bool{})
			}
		}
	}
	typeFieldOffsets := ctx.typeFieldOffsets()
	for _, moduleName := range modules {
		byName := ctx.checked.Index.ByModule[moduleName]
		var typeNames []string
		for typeName := range byName {
			typeNames = append(typeNames, typeName)
		}
		sort.Strings(typeNames)
		for _, name := range typeNames {
			typ := byName[name]
			if typ == nil {
				continue
			}
			typeInfo := ctx.ensureTypeInfo(typ, map[string]bool{})
			offsets, widths := receiverLayout(typeInfo)
			methods := append([]sem.Method(nil), typ.Methods...)
			sort.Slice(methods, func(i, j int) bool {
				return methods[i].Name < methods[j].Name
			})
			for _, method := range methods {
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
					TypeFieldOffsets:     typeFieldOffsets,
				})
			}
		}
	}
	return out
}

func (ctx *lowerContext) typeFieldOffsets() map[string]map[string]int {
	out := map[string]map[string]int{}
	for _, info := range ctx.program.Types {
		fields := map[string]int{}
		for _, fieldName := range info.FieldOrder {
			field := info.Fields[fieldName]
			fields[strings.ToLower(fieldName)] = field.Offset
		}
		if len(fields) == 0 {
			continue
		}
		out[strings.ToLower(info.Name)] = fields
		if info.Module != "" {
			out[strings.ToLower(info.Module+"."+info.Name)] = fields
		}
	}
	return out
}

func receiverLayout(info TypeInfo) (map[string]int, map[string]int) {
	offsets := map[string]int{}
	widths := map[string]int{}
	for _, fieldName := range info.FieldOrder {
		field := info.Fields[fieldName]
		offsets[fieldName] = field.Offset
		widths[fieldName] = field.Size * 8
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

func logicalSymbol(parts ...string) string {
	return strings.Join(parts, "::")
}

func pathModule(pathType string) string {
	parts := strings.Split(pathType, ".")
	if len(parts) <= 1 {
		return ""
	}
	return strings.Join(parts[:len(parts)-1], ".")
}

func pathName(pathType string) string {
	parts := strings.Split(pathType, ".")
	return parts[len(parts)-1]
}

func typeInfoFor(types map[string]TypeInfo, typ Type) (TypeInfo, bool) {
	if typ.Module != "" {
		if info, ok := types[typ.Module+"."+typ.Name]; ok {
			return info, true
		}
	}
	info, ok := types[typ.Name]
	return info, ok
}

func typeName(typ *sem.Type) string {
	if typ == nil {
		return ""
	}
	return typ.Name
}

func sameSemType(a, b *sem.Type) bool {
	if a == nil || b == nil {
		return false
	}
	if a.Name != b.Name {
		return false
	}
	return a.Module == "" || b.Module == "" || a.Module == b.Module
}

func receiverHasMethodReturning(receiver *sem.Type, ret *sem.Type) bool {
	if receiver == nil || ret == nil {
		return false
	}
	for _, method := range receiver.Methods {
		if sameSemType(method.Return, ret) {
			return true
		}
	}
	return false
}

func typeKey(moduleName, name string) string {
	return moduleName + "." + name
}

func typeInfoKey(moduleName, name string) string {
	if moduleName == "" {
		return name
	}
	return moduleName + "." + name
}
