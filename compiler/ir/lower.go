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
)

type lowerBinding struct {
	value Value
	typ   *sem.Type
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

	stringSeq int
	diags     []diag.Diagnostic
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

	if imageDecl != nil {
		for i := range imageDecl.Phases {
			phase := &imageDecl.Phases[i]
			switch phase.Name {
			case "delegated_hardware":
				ctx.program.Functions = append(ctx.program.Functions, ctx.lowerPhase(imageModule, imageName, delegatedSymbol, phase))
			case "owned_hardware":
				ctx.program.Functions = append(ctx.program.Functions, ctx.lowerPhase(imageModule, imageName, ownedSymbol, phase))
			}
		}
	}

	ctx.lowerSourceMethods()
	ctx.lowerInterruptEventsAndHandlers()
	ctx.program.AsmMethods = append(ctx.program.AsmMethods, ctx.lowerAsmMethods()...)
	if len(ctx.diags) != 0 {
		return nil, ctx.diags
	}
	return ctx.program, nil
}

func newLowerContext(checked *sem.CheckedProgram) *lowerContext {
	ctx := &lowerContext{
		checked: checked,
		program: &Program{Types: map[string]TypeInfo{}},
		modules: map[string]*ast.Module{},
		types:   map[string]*sem.Type{},
		pseudo:  map[string]*sem.Type{},
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
			"address": {Name: "address", Type: Type{Name: "VirtualAddress", Kind: TypeKindPrimitive}, Offset: 0, Size: 8, Align: 8},
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
				ctx.program.Functions = append(ctx.program.Functions, ctx.lowerMethod(mod.Name, receiverType, method))
			}
		}
	}
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
	for _, binding := range ctx.checked.InterruptBindings {
		eventType := ctx.irType(binding.EventType)
		eventInfo, ok := typeInfoFor(ctx.program.Types, eventType)
		if !ok {
			ctx.errorf("missing type info for interrupt event %s.%s", eventType.Module, eventType.Name)
			continue
		}
		eventStorageSize := eventInfo.StorageSize
		if eventStorageSize == 0 {
			eventStorageSize = eventInfo.Size
		}
		if eventStorageSize == 0 {
			eventStorageSize = 8
		}
		executorType := Type{Name: binding.ExecutorType, Module: binding.ExecutorModule, Kind: TypeKindExecutor}
		executorInfo, ok := typeInfoFor(ctx.program.Types, executorType)
		if !ok {
			ctx.errorf("missing type info for interrupt executor %s.%s", executorType.Module, executorType.Name)
			continue
		}
		pathField, ok := executorInfo.Fields[binding.PathField]
		if !ok {
			ctx.errorf("missing interrupt path field %s on executor %s.%s", binding.PathField, executorType.Module, executorType.Name)
			continue
		}
		ctx.program.InterruptBindings = append(ctx.program.InterruptBindings, InterruptBinding{
			EventSymbol:           logicalSymbol("interrupt_event", pathModule(binding.PathType), pathName(binding.PathType), "interrupt"),
			HandlerSymbol:         logicalSymbol("on_handler", binding.ExecutorModule, binding.ExecutorType, binding.PathField, "interrupt"),
			EventFunctionSymbol:   symbolName("event_fn", pathModule(binding.PathType), pathName(binding.PathType), "interrupt"),
			HandlerFunctionSymbol: symbolName("on_fn", binding.ExecutorModule, binding.ExecutorType, binding.PathField, "interrupt"),
			ExecutorType:          executorType,
			PathField:             binding.PathField,
			PathFieldOffset:       pathField.Offset,
			EventStorageSymbol:    fmt.Sprintf("_wrela_interrupt_event_%02x", binding.Vector),
			EventStorageSize:      eventStorageSize,
			Vector:                binding.Vector,
		})
	}
	sort.Slice(ctx.program.InterruptBindings, func(i, j int) bool {
		if ctx.program.InterruptBindings[i].Vector != ctx.program.InterruptBindings[j].Vector {
			return ctx.program.InterruptBindings[i].Vector < ctx.program.InterruptBindings[j].Vector
		}
		return ctx.program.InterruptBindings[i].HandlerSymbol < ctx.program.InterruptBindings[j].HandlerSymbol
	})
}

func (ctx *lowerContext) lowerMethod(moduleName string, receiverType *sem.Type, method *ast.MethodDecl) Function {
	symbol := symbolName("method", moduleName, receiverType.Name, method.Name)
	params := []Value{}
	scope := newLowerScope(nil)

	self := &Param{Symbol: "self", Type: ctx.irType(receiverType)}
	params = append(params, self)
	scope.define("self", lowerBinding{value: self, typ: receiverType})

	for _, param := range method.Params {
		if param.Name == "self" {
			continue
		}
		typ := ctx.resolveType(moduleName, param.Type)
		p := &Param{Symbol: param.Name, Type: ctx.irType(typ)}
		params = append(params, p)
		scope.define(param.Name, lowerBinding{value: p, typ: typ})
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
			return ops
		}
		scope.define(s.Name, lowerBinding{value: value, typ: typ})
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
			ops = append(ops, &FieldStore{Object: object, ObjectType: objectType.Name, Field: target.Field, Value: value, Type: ctx.irType(typ), Offset: field.Offset})
		default:
			ctx.errorf("unsupported assignment target %T", s.Target)
		}
		return ops
	case *ast.ReturnStmt:
		if s.Value == nil {
			return []Operation{&Return{}}
		}
		value, valueOps, _ := ctx.lowerExpr(moduleName, receiverType, scope, s.Value)
		return append(valueOps, &Return{Value: value})
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
		condition, conditionOps, _ := ctx.lowerExpr(moduleName, receiverType, scope, s.Cond)
		thenOps := ctx.lowerStmtList(moduleName, receiverType, newLowerScope(scope), assigned, s.Then)
		elseOps := ctx.lowerStmtList(moduleName, receiverType, newLowerScope(scope), assigned, s.Else)
		return []Operation{&If{ConditionOps: conditionOps, Condition: condition, Then: thenOps, Else: elseOps}}
	default:
		ctx.errorf("unsupported statement %T", stmt)
		return nil
	}
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
			Offset:     field.Offset,
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
		method := ctx.lookupMethod(recvType, e.Method)
		args, argOps := ctx.lowerCallArgs(moduleName, receiverType, scope, method, e.Args)
		ret := ctx.methodReturn(moduleName, method)
		call := &Call{
			Symbol:   symbolName("method", recvType.Module, recvType.Name, e.Method),
			Receiver: receiver,
			Args:     args,
			Type:     ctx.irType(ret),
		}
		ops := append(receiverOps, argOps...)
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

func (ctx *lowerContext) methodReturn(moduleName string, method *sem.Method) *sem.Type {
	if method == nil || method.Return == nil {
		if ctx.checked != nil && ctx.checked.OwnedRoot != nil {
			return ctx.checked.OwnedRoot
		}
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
			}
		}
	}
	walk(stmts)
	return out
}

func compositeMethods(decl ast.Decl) (string, []ast.MethodDecl, bool) {
	switch d := decl.(type) {
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
