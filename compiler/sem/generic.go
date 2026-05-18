package sem

import (
	"strconv"

	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/diag"
)

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
