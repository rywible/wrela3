package sem

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/source"
)

type Index struct {
	Modules      map[string]*ast.Module
	ByModule     map[string]map[string]*Type
	ByImport     map[string]map[string]*Type
	Consts       map[string]map[string]ConstValue
	ConstImports map[string]map[string]ConstValue
	Traits       map[string]*Trait
	Impls        []Impl
	Images       []*ast.ImageDecl

	InterruptEvents            map[string]map[string]*ast.InterruptEventDecl
	OnHandlers                 map[string]map[string]map[string]*ast.OnHandlerDecl
	Instantiations             map[string]*Type
	InstantiationOrder         []string
	InstantiationDiags         []diag.Diagnostic
	InstantiationDepthExceeded bool
	primitives                 map[string]*Type
}

type ConstValue struct {
	Type  *Type
	Value uint64
	Span  source.Span
}

func NewIndex() *Index {
	return &Index{
		Modules:            map[string]*ast.Module{},
		ByModule:           map[string]map[string]*Type{},
		ByImport:           map[string]map[string]*Type{},
		Consts:             map[string]map[string]ConstValue{},
		ConstImports:       map[string]map[string]ConstValue{},
		Traits:             map[string]*Trait{},
		Impls:              []Impl{},
		InterruptEvents:    map[string]map[string]*ast.InterruptEventDecl{},
		OnHandlers:         map[string]map[string]map[string]*ast.OnHandlerDecl{},
		Instantiations:     map[string]*Type{},
		InstantiationOrder: []string{},
		primitives:         map[string]*Type{},
	}
}

func (idx *Index) LookupConst(moduleName, name string) (ConstValue, bool) {
	if idx == nil {
		return ConstValue{}, false
	}
	if m := idx.Consts[moduleName]; m != nil {
		if v, ok := m[name]; ok {
			return v, true
		}
	}
	if m := idx.ConstImports[moduleName]; m != nil {
		if v, ok := m[name]; ok {
			return v, true
		}
	}
	return ConstValue{}, false
}

func (idx *Index) ConstValue(moduleName, name string) uint64 {
	v, ok := idx.LookupConst(moduleName, name)
	if !ok {
		return 0
	}
	return v.Value
}

func (idx *Index) Lookup(moduleName, name string) (*Type, bool) {
	if idx == nil {
		return nil, false
	}
	if m, ok := idx.ByModule[moduleName]; ok {
		if out, ok := m[name]; ok {
			return out, true
		}
	}
	if im, ok := idx.ByImport[moduleName]; ok {
		if out, ok := im[name]; ok {
			return out, true
		}
	}
	if p, ok := idx.primitives[name]; ok {
		return p, true
	}
	return nil, false
}

func (idx *Index) InterruptEvent(moduleName, pathType string) *ast.InterruptEventDecl {
	if idx == nil || idx.InterruptEvents[moduleName] == nil {
		return nil
	}
	return idx.InterruptEvents[moduleName][pathType]
}

func (idx *Index) OnHandler(moduleName, executorType, pathField string) *ast.OnHandlerDecl {
	if idx == nil || idx.OnHandlers[moduleName] == nil || idx.OnHandlers[moduleName][executorType] == nil {
		return nil
	}
	return idx.OnHandlers[moduleName][executorType][pathField]
}

func (idx *Index) MustType(name string) *Type {
	if idx == nil {
		return nil
	}
	modules := make([]string, 0, len(idx.ByModule))
	for module := range idx.ByModule {
		modules = append(modules, module)
	}
	sort.Strings(modules)
	for _, module := range modules {
		if t, ok := idx.ByModule[module][name]; ok {
			return t
		}
	}
	if p, ok := idx.primitives[name]; ok {
		return p
	}
	return nil
}

var explicitDelegatedOnly = map[string]bool{
	"DelegatedHardware":     true,
	"UefiBootServices":      true,
	"DelegatedMemory":       true,
	"DelegatedBytes":        true,
	"DelegatedMutableBytes": true,
	"UefiMemoryMap":         true,
	"UefiMemoryMapResult":   true,
}

func (idx *Index) IsDelegatedOnly(t *Type, seen map[string]bool) bool {
	if t == nil {
		return false
	}
	if explicitDelegatedOnly[t.Name] {
		return true
	}
	key := t.Module + "." + t.Name
	if seen[key] {
		return false
	}
	seen[key] = true
	for _, field := range t.Fields {
		if idx.IsDelegatedOnly(field.Type, seen) {
			return true
		}
	}
	return false
}

func (idx *Index) DelegatedOnlyOffender(t *Type, seen map[string]bool) string {
	if t == nil {
		return ""
	}
	if explicitDelegatedOnly[t.Name] {
		return t.Name
	}
	key := t.Module + "." + t.Name
	if seen[key] {
		return ""
	}
	seen[key] = true
	for _, field := range t.Fields {
		if out := idx.DelegatedOnlyOffender(field.Type, seen); out != "" {
			return out
		}
	}
	return ""
}

func BuildIndex(modules []*ast.Module) (*Index, []diag.Diagnostic) {
	idx := NewIndex()
	diagOut := make([]diag.Diagnostic, 0, 8)

	buildPrimitives(idx)

	for _, mod := range modules {
		idx.Modules[mod.Name] = mod
		if _, ok := idx.ByModule[mod.Name]; !ok {
			idx.ByModule[mod.Name] = map[string]*Type{}
		}
		if _, ok := idx.ByImport[mod.Name]; !ok {
			idx.ByImport[mod.Name] = map[string]*Type{}
		}
		if _, ok := idx.Consts[mod.Name]; !ok {
			idx.Consts[mod.Name] = map[string]ConstValue{}
		}
		if _, ok := idx.ConstImports[mod.Name]; !ok {
			idx.ConstImports[mod.Name] = map[string]ConstValue{}
		}
	}

	for _, mod := range modules {
		seen := map[string]bool{}
		for _, decl := range mod.Decls {
			name := declarationName(decl)
			if name == "" {
				continue
			}
			if seen[name] {
				diagOut = append(diagOut, diag.Diagnostic{
					Phase:    "sem",
					Code:     diag.SEM0001,
					Severity: diag.Error,
					Start:    decl.Span().Start,
					End:      decl.Span().End,
					Message:  fmt.Sprintf("duplicate declaration %q", name),
				})
				continue
			}
			seen[name] = true

			kind := typeKind(decl)
			if kind == -1 {
				if d, ok := decl.(*ast.ConstDecl); ok {
					idx.Consts[mod.Name][d.Name] = ConstValue{Span: d.SpanV}
				}
				continue
			}
			typ := &Type{Module: mod.Name, Name: name, Kind: kind}
			switch d := decl.(type) {
			case *ast.ClassDecl:
				typ.Unique = d.Unique
				typ.TypeParams = toTypeParams(d.TypeParams)
			case *ast.DriverDecl:
				typ.Unique = d.Unique
				typ.TypeParams = toTypeParams(d.TypeParams)
			case *ast.DataDecl:
				typ.TypeParams = toTypeParams(d.TypeParams)
			case *ast.EnumDecl:
				typ.TypeParams = toTypeParams(d.TypeParams)
			case *ast.TraitDecl:
				typ.TypeParams = toTypeParams(d.TypeParams)
			}
			idx.ByModule[mod.Name][name] = typ
		}
	}

	for _, mod := range modules {
		imported := idx.ByImport[mod.Name]
		seenImports := map[string]bool{}
		for _, imp := range mod.Imports {
			importedMod, ok := idx.Modules[imp.Path]
			if !ok {
				diagOut = append(diagOut, diag.Diagnostic{
					Phase:    "sem",
					Code:     diag.SEM0002,
					Severity: diag.Error,
					Start:    imp.Span.Start,
					End:      imp.Span.End,
					Message:  "unknown imported module: " + imp.Path,
				})
				continue
			}
			for _, name := range imp.Names {
				if _, ok := idx.ByModule[mod.Name][name]; ok || seenImports[name] {
					diagOut = append(diagOut, diag.Diagnostic{
						Phase:    "sem",
						Code:     diag.SEM0001,
						Severity: diag.Error,
						Start:    imp.Span.Start,
						End:      imp.Span.End,
						Message:  "duplicate declaration " + name,
					})
					continue
				}
				if _, ok := idx.Consts[mod.Name][name]; ok {
					diagOut = append(diagOut, diag.Diagnostic{
						Phase:    "sem",
						Code:     diag.SEM0001,
						Severity: diag.Error,
						Start:    imp.Span.Start,
						End:      imp.Span.End,
						Message:  "duplicate declaration " + name,
					})
					continue
				}
				seenImports[name] = true
				if typ, ok := idx.ByModule[importedMod.Name][name]; ok && typ != nil {
					imported[name] = typ
					continue
				}
				if cv, ok := idx.Consts[importedMod.Name][name]; ok {
					idx.ConstImports[mod.Name][name] = cv
					continue
				}
				diagOut = append(diagOut, diag.Diagnostic{
					Phase:    "sem",
					Code:     diag.SEM0002,
					Severity: diag.Error,
					Start:    imp.Span.Start,
					End:      imp.Span.End,
					Message:  "unknown imported declaration " + name,
				})
			}
		}
	}

	for _, mod := range modules {
		for _, decl := range mod.Decls {
			typ := idx.resolveInScope(mod.Name, declarationName(decl))
			processImpl := func(d *ast.ImplDecl) {
				paramNames := freeImplTypeParams(idx, mod.Name, d.Trait, d.For)
				implTypeParams := map[string]*Type{}
				for _, name := range paramNames {
					implTypeParams[name] = &Type{Name: name, Kind: KindTypeParam}
				}
				implTrait, traitDiags := idx.LookupTraitRef(mod.Name, d.Trait, implTypeParams)
				diagOut = append(diagOut, traitDiags...)
				implFor, forDiags := idx.LookupTypeRef(mod.Name, d.For, implTypeParams)
				diagOut = append(diagOut, forDiags...)
				if implTrait == nil || implFor == nil {
					return
				}
				implTypeParamSet := map[string]bool{}
				for _, name := range paramNames {
					implTypeParamSet[name] = true
				}
				implDecl := Impl{
					Trait:      implTrait,
					For:        implFor,
					TypeParams: implTypeParamSet,
					Span:       d.SpanV,
				}
				overlap := false
				for _, existing := range idx.Impls {
					if implsOverlap(&existing, &implDecl) {
						overlap = true
						diagOut = append(diagOut, diag.Diagnostic{
							Phase:    "sem",
							Code:     diag.SEM0083,
							Severity: diag.Error,
							Start:    d.SpanV.Start,
							End:      d.SpanV.End,
							Message:  "overlapping impl",
						})
						break
					}
				}
				idx.Impls = append(idx.Impls, implDecl)
				if !overlap {
					diagOut = append(diagOut, idx.validateImplSignatures(implDecl)...)
				}
			}
			if typ == nil {
				if d, ok := decl.(*ast.ImplDecl); ok {
					processImpl(d)
				}
				continue
			}
			var params map[string]*Type
			var localDiags []diag.Diagnostic
			switch d := decl.(type) {
			case *ast.DataDecl:
				params, localDiags = buildTypeParamMap(d.TypeParams)
				diagOut = append(diagOut, localDiags...)
				typ.TypeParams = toTypeParams(d.TypeParams)
				typ.Where, localDiags = buildWhereBounds(idx, mod.Name, d.Where, params)
				diagOut = append(diagOut, localDiags...)
				typ.Fields, localDiags = buildFields(idx, mod.Name, d.Fields, params)
				diagOut = append(diagOut, localDiags...)
				typ.Methods, localDiags = buildMethods(idx, mod.Name, d.Methods, params)
				diagOut = append(diagOut, localDiags...)
			case *ast.EnumDecl:
				params, localDiags = buildTypeParamMap(d.TypeParams)
				diagOut = append(diagOut, localDiags...)
				typ.TypeParams = toTypeParams(d.TypeParams)
				typ.EnumVariants, localDiags = buildEnumVariants(idx, mod.Name, d.Variants, params)
				diagOut = append(diagOut, localDiags...)
			case *ast.TraitDecl:
				params, localDiags = buildTypeParamMap(d.TypeParams)
				diagOut = append(diagOut, localDiags...)
				typ.TypeParams = toTypeParams(d.TypeParams)
				typ.Methods, localDiags = buildMethods(idx, mod.Name, d.Methods, params)
				diagOut = append(diagOut, localDiags...)
				idx.Traits[qualifiedTypeName(typ)] = &Trait{
					Module:     mod.Name,
					Name:       typ.Name,
					TypeParams: typ.TypeParams,
					Methods:    typ.Methods,
					Span:       d.SpanV,
				}
			case *ast.ClassDecl:
				params, localDiags = buildTypeParamMap(d.TypeParams)
				diagOut = append(diagOut, localDiags...)
				typ.TypeParams = toTypeParams(d.TypeParams)
				typ.Where, localDiags = buildWhereBounds(idx, mod.Name, d.Where, params)
				diagOut = append(diagOut, localDiags...)
				typ.Fields, localDiags = buildFields(idx, mod.Name, d.Fields, params)
				diagOut = append(diagOut, localDiags...)
				typ.Methods, localDiags = buildMethods(idx, mod.Name, d.Methods, params)
				diagOut = append(diagOut, localDiags...)
			case *ast.DriverDecl:
				params, localDiags = buildTypeParamMap(d.TypeParams)
				diagOut = append(diagOut, localDiags...)
				typ.TypeParams = toTypeParams(d.TypeParams)
				typ.Where, localDiags = buildWhereBounds(idx, mod.Name, d.Where, params)
				diagOut = append(diagOut, localDiags...)
				typ.Fields, localDiags = buildFields(idx, mod.Name, d.Fields, params)
				diagOut = append(diagOut, localDiags...)
				typ.Methods, localDiags = buildMethods(idx, mod.Name, d.Methods, params)
				diagOut = append(diagOut, localDiags...)
			case *ast.DriverPathDecl:
				typ.Fields, localDiags = buildFields(idx, mod.Name, d.Fields, nil)
				diagOut = append(diagOut, localDiags...)
				typ.Methods, localDiags = buildMethods(idx, mod.Name, d.Methods, nil)
				diagOut = append(diagOut, localDiags...)
				if idx.InterruptEvents[mod.Name] == nil {
					idx.InterruptEvents[mod.Name] = map[string]*ast.InterruptEventDecl{}
				}
				for i := range d.InterruptEvents {
					event := &d.InterruptEvents[i]
					if idx.InterruptEvents[mod.Name][d.Name] != nil {
						diagOut = append(diagOut, diag.Diagnostic{
							Phase:    "sem",
							Code:     diag.SEM0014,
							Severity: diag.Error,
							Start:    event.Span().Start,
							End:      event.Span().End,
							Message:  "duplicate interrupt receiver",
						})
						continue
					}
					idx.InterruptEvents[mod.Name][d.Name] = event
				}
			case *ast.ExecutorDecl:
				typ.Fields, localDiags = buildFields(idx, mod.Name, d.Fields, nil)
				diagOut = append(diagOut, localDiags...)
				typ.Methods, localDiags = buildMethods(idx, mod.Name, d.Methods, nil)
				diagOut = append(diagOut, localDiags...)
				if idx.OnHandlers[mod.Name] == nil {
					idx.OnHandlers[mod.Name] = map[string]map[string]*ast.OnHandlerDecl{}
				}
				if idx.OnHandlers[mod.Name][d.Name] == nil {
					idx.OnHandlers[mod.Name][d.Name] = map[string]*ast.OnHandlerDecl{}
				}
				for i := range d.OnHandlers {
					handler := &d.OnHandlers[i]
					if idx.OnHandlers[mod.Name][d.Name][handler.PathField] != nil {
						diagOut = append(diagOut, diag.Diagnostic{
							Phase:    "sem",
							Code:     diag.SEM0014,
							Severity: diag.Error,
							Start:    handler.Span().Start,
							End:      handler.Span().End,
							Message:  "duplicate on handler " + handler.PathField + ".interrupt",
						})
						continue
					}
					idx.OnHandlers[mod.Name][d.Name][handler.PathField] = handler
				}
			case *ast.ImageDecl:
				idx.Images = append(idx.Images, d)
			}
		}
	}

	for _, mod := range modules {
		if types, ok := idx.ByModule[mod.Name]; ok {
			for _, typ := range types {
				if typ == nil {
					continue
				}
				typ.DelegatedOnly = idx.IsDelegatedOnly(typ, map[string]bool{})
			}
		}
	}

	switch len(idx.Images) {
	case 0:
		diagOut = append(diagOut, diag.Diagnostic{
			Phase:    "sem",
			Code:     diag.SEM0004,
			Severity: diag.Error,
			Message:  "missing image declaration",
		})
	case 1:
		// nothing
	default:
		diagOut = append(diagOut, diag.Diagnostic{
			Phase:    "sem",
			Code:     diag.SEM0003,
			Severity: diag.Error,
			Message:  "import graph contains more than one image",
			Start:    idx.Images[1].SpanV.Start,
			End:      idx.Images[1].SpanV.End,
		})
	}
	diagOut = append(diagOut, idx.CompleteGenericInstantiations()...)

	return idx, diagOut
}

func (idx *Index) resolveInScope(moduleName, raw string) *Type {
	if idx == nil {
		return nil
	}
	if mod, ok := idx.ByModule[moduleName]; ok {
		if typ := mod[raw]; typ != nil {
			return typ
		}
	}
	if imported, ok := idx.ByImport[moduleName]; ok {
		if typ := imported[raw]; typ != nil {
			return typ
		}
	}
	if t, ok := idx.primitives[raw]; ok {
		return t
	}
	if parts := strings.Split(raw, "."); len(parts) > 1 {
		modName := strings.Join(parts[:len(parts)-1], ".")
		typeName := parts[len(parts)-1]
		if mod, ok := idx.ByModule[modName]; ok {
			if t, ok := mod[typeName]; ok {
				return t
			}
		}
		if mod, ok := idx.ByImport[modName]; ok {
			if t, ok := mod[typeName]; ok {
				return t
			}
		}
		if t, ok := idx.primitives[typeName]; ok {
			return t
		}
	}
	return nil
}

func (idx *Index) lookupType(moduleName string, raw string) *Type {
	if raw == "" {
		return nil
	}
	if t := idx.Instantiations[raw]; t != nil {
		return t
	}
	if t := idx.resolveInScope(moduleName, raw); t != nil {
		return t
	}
	if parts := strings.Split(raw, "."); len(parts) > 1 {
		modName := strings.Join(parts[:len(parts)-1], ".")
		typeName := parts[len(parts)-1]
		if t, ok := idx.ByModule[modName][typeName]; ok {
			return t
		}
		if t, ok := idx.ByImport[modName][typeName]; ok {
			return t
		}
		if t, ok := idx.primitives[typeName]; ok {
			return t
		}
	}
	return nil
}

func typeKind(decl ast.Decl) Kind {
	switch decl.(type) {
	case *ast.DataDecl:
		return KindData
	case *ast.ClassDecl:
		return KindClass
	case *ast.DriverDecl:
		return KindDriver
	case *ast.DriverPathDecl:
		return KindDriverPath
	case *ast.EnumDecl:
		return KindEnum
	case *ast.TraitDecl:
		return KindTrait
	case *ast.EventDecl:
		return KindEvent
	case *ast.ProjectionDecl:
		return KindProjection
	case *ast.ExecutorDecl:
		return KindExecutor
	case *ast.ImageDecl:
		return KindImage
	default:
		return -1
	}
}

func declarationName(decl ast.Decl) string {
	switch d := decl.(type) {
	case *ast.DataDecl:
		return d.Name
	case *ast.ClassDecl:
		return d.Name
	case *ast.ConstDecl:
		return d.Name
	case *ast.DriverDecl:
		return d.Name
	case *ast.DriverPathDecl:
		return d.Name
	case *ast.EnumDecl:
		return d.Name
	case *ast.TraitDecl:
		return d.Name
	case *ast.EventDecl:
		return d.Name
	case *ast.ProjectionDecl:
		return d.Name
	case *ast.ExecutorDecl:
		return d.Name
	case *ast.ImageDecl:
		return d.Name
	default:
		_ = d
		return ""
	}
}

func buildPrimitives(idx *Index) {
	for _, p := range []string{"Bool", "U8", "U16", "U32", "U64", "I64", "PhysicalAddress", "VirtualAddress", "never"} {
		idx.primitives[p] = &Type{Module: "builtin", Name: p, Kind: KindPrimitive}
	}
	idx.primitives["StringLiteral"] = &Type{
		Module: "builtin",
		Name:   "StringLiteral",
		Kind:   KindPrimitive,
		Fields: []Field{
			{Name: "address", Type: idx.primitives["PhysicalAddress"], Span: source.Span{}},
			{Name: "length", Type: idx.primitives["U64"], Span: source.Span{}},
		},
	}
}

func buildFields(idx *Index, moduleName string, fields []ast.Field, params map[string]*Type) ([]Field, []diag.Diagnostic) {
	out := make([]Field, 0, len(fields))
	var diags []diag.Diagnostic
	for _, field := range fields {
		typ, fieldDiags := idx.LookupTypeRef(moduleName, field.Type, params)
		diags = append(diags, fieldDiags...)
		if typ == nil {
			continue
		}
		out = append(out, Field{
			Name: field.Name,
			Type: typ,
			Span: field.Span,
		})
	}
	return out, diags
}

func buildEnumVariants(idx *Index, moduleName string, variants []ast.EnumVariant, params map[string]*Type) ([]EnumVariant, []diag.Diagnostic) {
	out := make([]EnumVariant, 0, len(variants))
	var diags []diag.Diagnostic
	for _, variant := range variants {
		fields, fieldDiags := buildFields(idx, moduleName, variant.Fields, params)
		diags = append(diags, fieldDiags...)
		out = append(out, EnumVariant{Name: variant.Name, Fields: fields, Span: variant.Span})
	}
	return out, diags
}

func buildMethods(idx *Index, moduleName string, methods []ast.MethodDecl, params map[string]*Type) ([]Method, []diag.Diagnostic) {
	out := make([]Method, 0, len(methods))
	var diags []diag.Diagnostic
	for _, m := range methods {
		methodParams, methodDiags := buildTypeParamMap(m.TypeParams)
		diags = append(diags, methodDiags...)
		methodScope := map[string]*Type{}
		for k, v := range params {
			methodScope[k] = v
		}
		for k, v := range methodParams {
			methodScope[k] = v
		}
		where, whereDiags := buildWhereBounds(idx, moduleName, m.Where, methodScope)
		diags = append(diags, whereDiags...)

		out = append(out, Method{
			Name:       m.Name,
			TypeParams: toTypeParams(m.TypeParams),
			Where:      where,
			IsAsm:      m.IsAsm,
			IsStart:    m.IsStart,
			Span:       m.SpanV,
			Body:       m.Body,
			AsmBody:    m.Asm,
		})
		method := &out[len(out)-1]
		methodParamsOut, methodParamDiags := buildParams(idx, moduleName, convertParams(m.Params), methodScope)
		diags = append(diags, methodParamDiags...)
		method.Params = methodParamsOut
		if m.Return.Name == "" {
			method.Return = idx.MustType("void")
		} else {
			returnType, returnDiags := idx.LookupTypeRef(moduleName, m.Return, methodScope)
			diags = append(diags, returnDiags...)
			method.Return = returnType
		}
	}
	return out, diags
}

func convertParams(params []ast.Param) []ast.Field {
	out := make([]ast.Field, 0, len(params))
	for _, p := range params {
		out = append(out, ast.Field{Name: p.Name, Type: p.Type, Span: p.Span})
	}
	return out
}

func legacyTypeName(ref ast.TypeRef) string {
	return ref.Name
}

func buildParams(idx *Index, moduleName string, params []ast.Field, typeParams map[string]*Type) ([]Field, []diag.Diagnostic) {
	out := make([]Field, 0, len(params))
	var diags []diag.Diagnostic
	for _, param := range params {
		if param.Name == "self" && param.Type.Name == "" {
			out = append(out, Field{Name: "self", Type: nil, Span: param.Span})
			continue
		}
		typ, paramDiags := idx.LookupTypeRef(moduleName, param.Type, typeParams)
		diags = append(diags, paramDiags...)
		if typ == nil {
			continue
		}
		out = append(out, Field{Name: param.Name, Type: typ, Span: param.Span})
	}
	return out, diags
}

func buildWhereBounds(idx *Index, moduleName string, where []ast.TraitBound, typeParams map[string]*Type) ([]TraitBound, []diag.Diagnostic) {
	out := make([]TraitBound, 0, len(where))
	var diags []diag.Diagnostic
	for _, bound := range where {
		trait, traitDiags := idx.LookupTraitRef(moduleName, bound.Trait, typeParams)
		diags = append(diags, traitDiags...)
		out = append(out, TraitBound{
			Param: bound.Param,
			Trait: trait,
			Span:  bound.Span,
		})
	}
	return out, diags
}

func toTypeParams(params []ast.TypeParam) []TypeParam {
	out := make([]TypeParam, 0, len(params))
	for _, param := range params {
		out = append(out, TypeParam{Name: param.Name, Span: param.Span})
	}
	return out
}
