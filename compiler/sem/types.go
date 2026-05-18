package sem

import (
	"strings"

	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/source"
)

type Kind int

const (
	KindPrimitive Kind = iota
	KindData
	KindClass
	KindDriver
	KindDriverPath
	KindExecutor
	KindImage
	KindTypeParam
)

func (k Kind) String() string {
	switch k {
	case KindPrimitive:
		return "primitive"
	case KindData:
		return "data"
	case KindClass:
		return "class"
	case KindDriver:
		return "driver"
	case KindDriverPath:
		return "driver path"
	case KindExecutor:
		return "executor"
	case KindImage:
		return "image"
	case KindTypeParam:
		return "type param"
	default:
		return "type"
	}
}

type TypeParam struct {
	Name string
	Span source.Span
}

type TraitBound struct {
	Param string
	Trait *Type
	Span  source.Span
}

type Field struct {
	Name string
	Type *Type
	Span source.Span
}

type Method struct {
	Name               string
	TypeParams         []TypeParam
	Where              []TraitBound
	Params             []Field
	Return             *Type
	IsAsm              bool
	IsStart            bool
	Span               source.Span
	Body               []ast.Stmt
	AsmBody            *ast.AsmBody
	GenericOrigin      *Method
	MonomorphizedOwner *Type
}

type Type struct {
	Module                string
	Name                  string
	Kind                  Kind
	Unique                bool
	DelegatedOnly         bool
	Fields                []Field
	Methods               []Method
	TypeParams            []TypeParam
	TypeArgs              []*Type
	Where                 []TraitBound
	GenericOrigin         *Type
	InstantiationComplete bool
}

func (t *Type) Key() string {
	if t == nil {
		return ""
	}
	base := qualifiedTypeName(t)
	if len(t.TypeArgs) == 0 {
		return base
	}
	parts := make([]string, 0, len(t.TypeArgs))
	for _, arg := range t.TypeArgs {
		parts = append(parts, arg.Key())
	}
	return base + "[" + strings.Join(parts, ",") + "]"
}

func (t *Type) Display() string {
	if t == nil {
		return ""
	}
	if len(t.TypeArgs) == 0 {
		return t.Name
	}
	parts := make([]string, 0, len(t.TypeArgs))
	for _, arg := range t.TypeArgs {
		parts = append(parts, arg.Display())
	}
	return t.Name + "<" + strings.Join(parts, ", ") + ">"
}

func (t *Type) MangledName() string {
	return strings.NewReplacer("<", "_", ">", "", ", ", "_", ".", "_", "[", "_", "]", "").Replace(t.Display())
}

type CheckedProgram struct {
	Modules    []*ast.Module
	Index      *Index
	ImageGraph ImageGraph
	OwnedRoot  *Type
}
