package sem

import "github.com/ryanwible/wrela3/compiler/source"

type ConstructedNode struct {
	Type *Type
	Span source.Span
}

type ExecutorNode struct {
	Type          *Type
	Span          source.Span
	FieldBindings map[string]string
	BoundTypes    map[string]*Type
}

type ImageGraph struct {
	Constructed []ConstructedNode
	Executors   []ExecutorNode
}
