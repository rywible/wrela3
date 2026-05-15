package sem

import (
	"fmt"
	"strings"

	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/source"
)

type Index struct {
	Modules  map[string]*ast.Module
	ByModule map[string]map[string]*Type
	ByImport map[string]map[string]*Type
	Images   []*ast.ImageDecl

	InterruptEvents map[string]map[string]*ast.InterruptEventDecl
	OnHandlers      map[string]map[string]map[string]*ast.OnHandlerDecl
	primitives      map[string]*Type
}

func NewIndex() *Index {
	return &Index{
		Modules:         map[string]*ast.Module{},
		ByModule:        map[string]map[string]*Type{},
		ByImport:        map[string]map[string]*Type{},
		InterruptEvents: map[string]map[string]*ast.InterruptEventDecl{},
		OnHandlers:      map[string]map[string]map[string]*ast.OnHandlerDecl{},
		primitives:      map[string]*Type{},
	}
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
	for _, m := range idx.ByModule {
		if t, ok := m[name]; ok {
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
		if _, ok := idx.ByModule[mod.Name]; ok {
			continue
		}
		idx.ByModule[mod.Name] = map[string]*Type{}
		idx.ByImport[mod.Name] = map[string]*Type{}
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
				continue
			}
			typ := &Type{Module: mod.Name, Name: name, Kind: kind}
			switch d := decl.(type) {
			case *ast.ClassDecl:
				typ.Unique = d.Unique
			case *ast.DriverDecl:
				typ.Unique = d.Unique
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
				if idx.ByModule[mod.Name][name] != nil || seenImports[name] {
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
			if typ == nil {
				continue
			}
			switch d := decl.(type) {
			case *ast.DataDecl:
				typ.Fields = buildFields(idx, mod.Name, d.Fields)
			case *ast.ClassDecl:
				typ.Fields = buildFields(idx, mod.Name, d.Fields)
				typ.Methods = buildMethods(idx, mod.Name, d.Methods)
			case *ast.DriverDecl:
				typ.Fields = buildFields(idx, mod.Name, d.Fields)
				typ.Methods = buildMethods(idx, mod.Name, d.Methods)
			case *ast.DriverPathDecl:
				typ.Fields = buildFields(idx, mod.Name, d.Fields)
				typ.Methods = buildMethods(idx, mod.Name, d.Methods)
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
				typ.Fields = buildFields(idx, mod.Name, d.Fields)
				typ.Methods = buildMethods(idx, mod.Name, d.Methods)
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
	case *ast.DriverDecl:
		return d.Name
	case *ast.DriverPathDecl:
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

func buildFields(idx *Index, moduleName string, fields []ast.Field) []Field {
	out := make([]Field, 0, len(fields))
	for _, field := range fields {
		out = append(out, Field{
			Name: field.Name,
			Type: idx.lookupType(moduleName, field.Type),
			Span: field.Span,
		})
	}
	return out
}

func buildMethods(idx *Index, moduleName string, methods []ast.MethodDecl) []Method {
	out := make([]Method, 0, len(methods))
	for _, m := range methods {
		out = append(out, Method{
			Name:    m.Name,
			Params:  buildFields(idx, moduleName, convertParams(m.Params)),
			Return:  idx.lookupType(moduleName, m.Return),
			IsAsm:   m.IsAsm,
			IsStart: m.IsStart,
			Span:    m.SpanV,
			Body:    m.Body,
			AsmBody: m.Asm,
		})
	}
	return out
}

func convertParams(params []ast.Param) []ast.Field {
	out := make([]ast.Field, 0, len(params))
	for _, p := range params {
		out = append(out, ast.Field{Name: p.Name, Type: p.Type, Span: p.Span})
	}
	return out
}
