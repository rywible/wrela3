package sem

import (
	"sort"
	"strconv"

	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/diag"
)

type substitution map[string]*Type

func substitutionFor(base *Type, args []*Type) substitution {
	out := substitution{}
	if base == nil {
		return out
	}
	for i, param := range base.TypeParams {
		if i < len(args) {
			out[param.Name] = args[i]
		}
	}
	return out
}

func (idx *Index) substituteType(t *Type, subst substitution) *Type {
	if t == nil {
		return nil
	}
	if repl := subst[t.Name]; repl != nil && t.Module == "" && len(t.TypeArgs) == 0 {
		return repl
	}
	if len(t.TypeArgs) == 0 {
		return t
	}
	args := make([]*Type, 0, len(t.TypeArgs))
	for _, arg := range t.TypeArgs {
		args = append(args, idx.substituteType(arg, subst))
	}
	origin := t.GenericOrigin
	if origin == nil {
		origin = t
	}
	return idx.registerInstantiation(origin, args)
}

func (idx *Index) LookupTypeRef(moduleName string, ref ast.TypeRef, params map[string]*Type) (*Type, []diag.Diagnostic) {
	return idx.lookupTypeRef(moduleName, ref, params, false)
}

func (idx *Index) lookupTypeRef(moduleName string, ref ast.TypeRef, params map[string]*Type, inTypeArg bool) (*Type, []diag.Diagnostic) {
	if params != nil {
		if typ := params[ref.Name]; typ != nil {
			if len(ref.Args) != 0 {
				return nil, []diag.Diagnostic{{
					Phase:    "sem",
					Code:     diag.SEM0077,
					Severity: diag.Error,
					Start:    ref.Span().Start,
					End:      ref.Span().End,
					Message:  "type parameter " + ref.Name + " does not take type arguments",
				}}
			}
			return typ, nil
		}
	}

	base, ok := idx.lookupBaseType(moduleName, ref.Name)
	if !ok {
		if !inTypeArg {
			return nil, nil
		}
		return nil, []diag.Diagnostic{{
			Phase:    "sem",
			Code:     diag.SEM0078,
			Severity: diag.Error,
			Start:    ref.Span().Start,
			End:      ref.Span().End,
			Message:  "unknown type " + ref.Name,
		}}
	}
	if len(base.TypeParams) != len(ref.Args) {
		return nil, []diag.Diagnostic{{
			Phase:    "sem",
			Code:     diag.SEM0077,
			Severity: diag.Error,
			Start:    ref.Span().Start,
			End:      ref.Span().End,
			Message:  base.Name + " expects " + strconv.Itoa(len(base.TypeParams)) + " type arguments",
		}}
	}
	if len(ref.Args) == 0 {
		return base, nil
	}
	args := make([]*Type, 0, len(ref.Args))
	for _, arg := range ref.Args {
		argType, argDiags := idx.lookupTypeRef(moduleName, arg, params, true)
		if len(argDiags) != 0 {
			return nil, argDiags
		}
		args = append(args, argType)
	}
	return idx.registerInstantiation(base, args), nil
}

func (idx *Index) lookupBaseType(moduleName, name string) (*Type, bool) {
	if typ, ok := idx.Lookup(moduleName, name); ok {
		return typ, true
	}
	if typ := idx.resolveInScope(moduleName, name); typ != nil {
		return typ, true
	}
	return nil, false
}

func (idx *Index) registerInstantiation(base *Type, args []*Type) *Type {
	if idx.Instantiations == nil {
		idx.Instantiations = map[string]*Type{}
	}
	concrete := &Type{
		Module:        base.Module,
		Name:          base.Name,
		Kind:          base.Kind,
		Unique:        base.Unique,
		DelegatedOnly: base.DelegatedOnly,
		TypeArgs:      append([]*Type(nil), args...),
		GenericOrigin: base,
	}
	key := concrete.Key()
	if existing := idx.Instantiations[key]; existing != nil {
		return existing
	}
	idx.Instantiations[key] = concrete
	idx.InstantiationOrder = append(idx.InstantiationOrder, key)
	if idx.ByModule[base.Module] == nil {
		idx.ByModule[base.Module] = map[string]*Type{}
	}
	idx.ByModule[base.Module][concrete.Display()] = concrete
	return concrete
}

func (idx *Index) instantiateByName(moduleName, name string, args []*Type) *Type {
	if moduleName == "" {
		return nil
	}
	base, ok := idx.Lookup(moduleName, name)
	if !ok {
		return nil
	}
	if len(args) == 0 {
		return base
	}
	return idx.registerInstantiation(base, args)
}

func buildTypeParamMap(params []ast.TypeParam) (map[string]*Type, []diag.Diagnostic) {
	out := map[string]*Type{}
	var diags []diag.Diagnostic
	for _, param := range params {
		if _, ok := out[param.Name]; ok {
			diags = append(diags, diag.Diagnostic{
				Phase:    "sem",
				Code:     diag.SEM0076,
				Severity: diag.Error,
				Start:    param.Span.Start,
				End:      param.Span.End,
				Message:  "duplicate type parameter " + param.Name,
			})
			continue
		}
		out[param.Name] = &Type{
			Name: param.Name,
			Kind: KindTypeParam,
		}
	}
	return out, diags
}

func (idx *Index) CompleteGenericInstantiations() []diag.Diagnostic {
	var out []diag.Diagnostic
	if idx == nil {
		return out
	}
	for {
		before := len(idx.InstantiationOrder)
		keys := append([]string(nil), idx.InstantiationOrder...)
		sort.Strings(keys)
		for _, key := range keys {
			out = append(out, idx.completeInstantiation(key, map[string]bool{})...)
		}
		if len(idx.InstantiationOrder) == before {
			return out
		}
	}
}

func (idx *Index) completeInstantiation(key string, visiting map[string]bool) []diag.Diagnostic {
	var out []diag.Diagnostic
	concrete := idx.Instantiations[key]
	if concrete == nil || concrete.GenericOrigin == nil || concrete.InstantiationComplete || visiting[key] {
		return nil
	}
	visiting[key] = true
	base := concrete.GenericOrigin
	subst := substitutionFor(base, concrete.TypeArgs)
	concrete.Fields = substituteFields(idx, base.Fields, subst)
	if base.Kind == KindEnum {
		concrete.EnumVariants = substituteEnumVariants(idx, base.EnumVariants, subst)
	}
	concrete.Methods = substituteMethods(idx, base.Methods, subst, concrete)
	concrete.Where = substituteBounds(idx, base.Where, subst)
	for _, field := range concrete.Fields {
		if field.Type != nil && field.Type.GenericOrigin != nil {
			out = append(out, idx.completeInstantiation(field.Type.Key(), visiting)...)
		}
	}
	for _, variant := range concrete.EnumVariants {
		for _, field := range variant.Fields {
			if field.Type != nil && field.Type.GenericOrigin != nil {
				out = append(out, idx.completeInstantiation(field.Type.Key(), visiting)...)
			}
		}
	}
	for _, method := range concrete.Methods {
		if method.Return != nil && method.Return.GenericOrigin != nil {
			out = append(out, idx.completeInstantiation(method.Return.Key(), visiting)...)
		}
		for _, param := range method.Params {
			if param.Type != nil && param.Type.GenericOrigin != nil {
				out = append(out, idx.completeInstantiation(param.Type.Key(), visiting)...)
			}
		}
	}
	out = append(out, idx.checkConcreteBounds(concrete)...)
	concrete.InstantiationComplete = true
	return out
}

func substituteEnumVariants(idx *Index, variants []EnumVariant, subst substitution) []EnumVariant {
	out := make([]EnumVariant, 0, len(variants))
	for _, variant := range variants {
		out = append(out, EnumVariant{Name: variant.Name, Fields: substituteFields(idx, variant.Fields, subst), Span: variant.Span})
	}
	return out
}

func substituteFields(idx *Index, fields []Field, subst substitution) []Field {
	out := make([]Field, 0, len(fields))
	for _, field := range fields {
		out = append(out, Field{
			Name: field.Name,
			Type: idx.substituteType(field.Type, subst),
			Span: field.Span,
		})
	}
	return out
}

func substituteMethods(idx *Index, methods []Method, subst substitution, owner *Type) []Method {
	out := make([]Method, 0, len(methods))
	for i := range methods {
		method := methods[i]
		outMethod := Method{
			Name:       method.Name,
			TypeParams: append([]TypeParam(nil), method.TypeParams...),
			Where:      substituteBounds(idx, method.Where, subst),
			IsAsm:      method.IsAsm,
			IsStart:    method.IsStart,
			Span:       method.Span,
			Body:       append([]ast.Stmt(nil), method.Body...),
			AsmBody:    method.AsmBody,
			Return:     idx.substituteType(method.Return, subst),
		}
		outMethod.GenericOrigin = &methods[i]
		outMethod.MonomorphizedOwner = owner
		outMethod.Params = append([]Field(nil), method.Params...)
		for i := range outMethod.Params {
			outMethod.Params[i].Type = idx.substituteType(outMethod.Params[i].Type, subst)
		}
		out = append(out, outMethod)
	}
	return out
}

func substituteBounds(idx *Index, bounds []TraitBound, subst substitution) []TraitBound {
	out := make([]TraitBound, 0, len(bounds))
	for _, bound := range bounds {
		out = append(out, TraitBound{
			Param: bound.Param,
			Trait: idx.substituteType(bound.Trait, subst),
			Span:  bound.Span,
		})
	}
	return out
}

func (idx *Index) checkConcreteBounds(concrete *Type) []diag.Diagnostic {
	if concrete == nil || concrete.GenericOrigin == nil {
		return nil
	}
	subst := substitutionFor(concrete.GenericOrigin, concrete.TypeArgs)
	var out []diag.Diagnostic
	for _, bound := range concrete.GenericOrigin.Where {
		concreteArg := subst[bound.Param]
		boundTrait := idx.substituteType(bound.Trait, subst)
		if concreteArg == nil || boundTrait == nil {
			continue
		}
		if !idx.hasImpl(boundTrait, concreteArg) {
			out = append(out, diag.Diagnostic{
				Phase:    "sem",
				Code:     diag.SEM0081,
				Severity: diag.Error,
				Start:    bound.Span.Start,
				End:      bound.Span.End,
				Message:  "missing impl " + boundTrait.Display() + " for " + concreteArg.Display(),
			})
		}
	}
	return out
}
