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
	KindEnum
	KindTrait
	KindTypeParam
	KindEvent
	KindProjection
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
	case KindEnum:
		return "enum"
	case KindTrait:
		return "trait"
	case KindTypeParam:
		return "type param"
	case KindEvent:
		return "event"
	case KindProjection:
		return "projection"
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

type EnumVariant struct {
	Name   string
	Fields []Field
	Span   source.Span
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
	EnumVariants          []EnumVariant
	Methods               []Method
	TypeParams            []TypeParam
	TypeArgs              []*Type
	Where                 []TraitBound
	GenericOrigin         *Type
	InstantiationComplete bool
	keyCache              string
}

func (t *Type) Key() string {
	if t == nil {
		return ""
	}
	if t.keyCache != "" {
		return t.keyCache
	}
	base := qualifiedTypeName(t)
	if len(t.TypeArgs) == 0 {
		t.keyCache = base
		return base
	}
	parts := make([]string, 0, len(t.TypeArgs))
	for _, arg := range t.TypeArgs {
		parts = append(parts, arg.Key())
	}
	t.keyCache = base + "[" + strings.Join(parts, ",") + "]"
	return t.keyCache
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
	name := mangleSymbolPart(t.Name)
	if len(t.TypeArgs) == 0 {
		return name
	}
	parts := make([]string, 0, len(t.TypeArgs))
	for _, arg := range t.TypeArgs {
		if arg != nil && len(arg.TypeArgs) == 0 && (arg.Module == "" || arg.Module == t.Module) {
			parts = append(parts, arg.Name)
			continue
		}
		parts = append(parts, arg.Key())
	}
	return name + "_g" + mangleSymbolPart("["+strings.Join(parts, ",")+"]")
}

func mangleSymbolPart(s string) string {
	const hex = "0123456789abcdef"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
			b.WriteByte(c)
			continue
		}
		b.WriteString("_x")
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&0x0f])
		b.WriteByte('_')
	}
	return b.String()
}

type CheckedProgram struct {
	Modules    []*ast.Module
	Index      *Index
	ImageGraph ImageGraph
	OwnedRoot  *Type
	Storage    StorageIndex
}
