package sem

import "github.com/ryanwible/wrela3/compiler/source"

type StoragePathNode struct {
	Label   string
	Role    string
	Owner   string
	QueueID uint16
	Vector  uint8
	Span    source.Span
}

type CoreLinkEndpointNode struct {
	Label     string
	Direction string
	Role      string
	Owner     string
	Peer      string
	Depth     uint64
	Span      source.Span
}

type ProjectionFeedNode struct {
	Projection  string
	SourceLabel string
	Owner       string
	Span        source.Span
}

type StorageWriterNode struct {
	Phase        string
	DirectFields map[string]string
	Span         source.Span
}

type StorageAppendCallNode struct {
	ResultObserved bool
	Span           source.Span
}
