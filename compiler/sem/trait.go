package sem

import (
	"sort"

	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/source"
)

type Trait struct {
	Module     string
	Name       string
	TypeParams []TypeParam
	Methods    []Method
	Span       source.Span
}

type Impl struct {
	Trait      *Type
	For        *Type
	TypeParams map[string]bool
	Span       source.Span
}

func freeImplTypeParams(idx *Index, moduleName string, refs ...ast.TypeRef) []string {
	seen := map[string]bool{}
	var out []string
	var walk func(ast.TypeRef)
	walk = func(ref ast.TypeRef) {
		if len(ref.Args) == 0 && ref.Name != "" && ref.Name[0] >= 'A' && ref.Name[0] <= 'Z' {
			if _, ok := idx.Lookup(moduleName, ref.Name); ok {
				return
			}
			if !seen[ref.Name] {
				seen[ref.Name] = true
				out = append(out, ref.Name)
			}
			return
		}
		for _, arg := range ref.Args {
			walk(arg)
		}
	}
	for _, ref := range refs {
		walk(ref)
	}
	sort.Strings(out)
	return out
}

func (idx *Index) hasImpl(trait *Type, forType *Type) bool {
	if trait == nil || forType == nil {
		return false
	}
	for _, impl := range idx.Impls {
		subst := map[string]*Type{}
		if matchImplPattern(impl.Trait, trait, impl.TypeParams, subst) &&
			matchImplPattern(impl.For, forType, impl.TypeParams, subst) {
			return true
		}
	}
	return false
}

func implsOverlap(left *Impl, right *Impl) bool {
	if left == nil || right == nil {
		return false
	}
	if left.Trait == nil || left.For == nil || right.Trait == nil || right.For == nil {
		return false
	}
	subst := map[string]*Type{}
	if matchImplPattern(left.Trait, right.Trait, left.TypeParams, subst) &&
		matchImplPattern(left.For, right.For, left.TypeParams, subst) {
		return true
	}
	subst = map[string]*Type{}
	if matchImplPattern(right.Trait, left.Trait, right.TypeParams, subst) &&
		matchImplPattern(right.For, left.For, right.TypeParams, subst) {
		return true
	}
	return false
}

func matchImplPattern(pattern *Type, concrete *Type, typeParams map[string]bool, subst map[string]*Type) bool {
	if pattern == nil || concrete == nil {
		return false
	}
	if pattern.Module == "" && len(pattern.TypeArgs) == 0 && typeParams[pattern.Name] {
		existing := subst[pattern.Name]
		if existing != nil {
			return existing.Key() == concrete.Key()
		}
		subst[pattern.Name] = concrete
		return true
	}
	if qualifiedTypeName(pattern) != qualifiedTypeName(concrete) || len(pattern.TypeArgs) != len(concrete.TypeArgs) {
		return false
	}
	for i := range pattern.TypeArgs {
		if !matchImplPattern(pattern.TypeArgs[i], concrete.TypeArgs[i], typeParams, subst) {
			return false
		}
	}
	return true
}

func (idx *Index) validateImplSignatures(impl Impl) []diag.Diagnostic {
	var out []diag.Diagnostic
	if impl.Trait == nil || impl.For == nil || impl.Trait.Kind != KindTrait {
		return out
	}
	concreteTrait := impl.Trait
	baseTrait := concreteTrait
	if concreteTrait.GenericOrigin != nil {
		baseTrait = concreteTrait.GenericOrigin
	}
	baseTraitDecl, ok := idx.Traits[qualifiedTypeName(baseTrait)]
	if !ok {
		return out
	}
	subst := substitutionFor(baseTrait, concreteTrait.TypeArgs)
	wantMethods := substituteMethods(idx, baseTraitDecl.Methods, subst, nil)
	forMethods := implMethods(idx, impl.For)
	for _, want := range wantMethods {
		have := traitMethodByName(forMethods, want.Name)
		if have == nil || !methodSignatureMatches(*have, want) {
			out = append(out, diag.Diagnostic{
				Phase:    "sem",
				Code:     diag.SEM0082,
				Severity: diag.Error,
				Start:    impl.Span.Start,
				End:      impl.Span.End,
				Message:  "trait method signature mismatch for " + want.Name,
			})
		}
	}
	return out
}

func implMethods(idx *Index, implFor *Type) []Method {
	if implFor == nil {
		return nil
	}
	if implFor.GenericOrigin == nil {
		return implFor.Methods
	}
	subst := substitutionFor(implFor.GenericOrigin, implFor.TypeArgs)
	return substituteMethods(idx, implFor.GenericOrigin.Methods, subst, implFor)
}

func traitMethodByName(methods []Method, name string) *Method {
	for i := range methods {
		if methods[i].Name == name {
			return &methods[i]
		}
	}
	return nil
}

func methodSignatureMatches(have Method, want Method) bool {
	if len(have.Params) != len(want.Params) {
		return false
	}
	for i := range have.Params {
		if have.Params[i].Name == "self" && want.Params[i].Name == "self" {
			continue
		}
		if have.Params[i].Type == nil || want.Params[i].Type == nil {
			if have.Params[i].Type != want.Params[i].Type {
				return false
			}
			continue
		}
		if have.Params[i].Type.Key() != want.Params[i].Type.Key() {
			return false
		}
	}
	if have.Return == nil || want.Return == nil {
		return have.Return == want.Return
	}
	return have.Return.Key() == want.Return.Key()
}
