package sem

import (
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
	default:
		return "type"
	}
}

type Field struct {
	Name string
	Type *Type
	Span source.Span
}

type Method struct {
	Name    string
	Params  []Field
	Return  *Type
	IsAsm   bool
	IsStart bool
	Span    source.Span
	Body    []ast.Stmt
	AsmBody *ast.AsmBody
}

type Type struct {
	Module        string
	Name          string
	Kind          Kind
	Unique        bool
	DelegatedOnly bool
	Fields        []Field
	Methods       []Method
}

type InterruptBinding struct {
	ExecutorModule string
	ExecutorType   string
	PathField      string
	PathType       string
	EventType      *Type
	Vector         uint8
	Span           source.Span
}

type CheckedProgram struct {
	Modules           []*ast.Module
	Index             *Index
	ImageGraph        ImageGraph
	OwnedRoot         *Type
	InterruptBindings []InterruptBinding
}
