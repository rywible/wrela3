package sem

import "github.com/ryanwible/wrela3/compiler/source"

type ConstructedNode struct {
	Type *Type
	Span source.Span
}

type DriverPathNode struct {
	Type      *Type
	Span      source.Span
	Binding   string
	FieldUses map[string]DriverPathUse
}

type DriverPathUse struct {
	Key  string
	Span source.Span
}

func (u DriverPathUse) spanOr(fallback source.Span) source.Span {
	if u.Span.End == 0 {
		return fallback
	}
	return u.Span
}

type ExecutorNode struct {
	Type          *Type
	Span          source.Span
	FieldBindings map[string]string
	FieldSpans    map[string]source.Span
	BoundTypes    map[string]*Type
	PathUses      map[string]DriverPathUse
}

type ImageGraph struct {
	Constructed []ConstructedNode
	DriverPaths []DriverPathNode
	Executors   []ExecutorNode
}
