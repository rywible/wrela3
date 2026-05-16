package codegen

import (
	"sort"
	"strings"

	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/ir"
)

const cacheLineSize = 64

type topicDataSubscriberLayout struct {
	Label          string
	CursorOffset   uint64
	WaitlineOffset uint64
}

type topicDataLayout struct {
	Label       string
	Kind        string
	Depth       uint64
	HeadOffset  uint64
	SlotsOffset uint64
	Subscribers []topicDataSubscriberLayout
	TotalSize   uint64
}

func alignUp64(value uint64) uint64 {
	if value%cacheLineSize == 0 {
		return value
	}
	return value + cacheLineSize - value%cacheLineSize
}

func planTopicData(topic ir.TopicLayout) topicDataLayout {
	layout := topicDataLayout{
		Label:      topic.Label,
		Kind:       topic.Kind,
		Depth:      topic.Depth,
		HeadOffset: 0,
	}

	next := uint64(cacheLineSize)
	layout.Subscribers = make([]topicDataSubscriberLayout, 0, len(topic.Subscribers))
	for _, subscriber := range topic.Subscribers {
		layout.Subscribers = append(layout.Subscribers, topicDataSubscriberLayout{
			Label:          subscriber,
			CursorOffset:   next,
			WaitlineOffset: next + cacheLineSize,
		})
		next += 2 * cacheLineSize
	}
	layout.SlotsOffset = alignUp64(next)
	layout.TotalSize = alignUp64(layout.SlotsOffset + topic.Depth*cacheLineSize)
	return layout
}

func planTopicDataChecked(topic ir.TopicLayout) (topicDataLayout, []diag.Diagnostic) {
	if topic.Depth == 0 || topic.Depth&(topic.Depth-1) != 0 {
		return topicDataLayout{}, []diag.Diagnostic{{
			Phase:   diagnosticPhase,
			Code:    diag.SEM0046,
			Message: "topic depth must be a power of two",
		}}
	}
	return planTopicData(topic), nil
}

func orderedTopicDataLayouts(program *ir.Program) ([]topicDataLayout, []diag.Diagnostic) {
	if program == nil || len(program.Topics) == 0 {
		return nil, nil
	}

	topics := append([]ir.TopicLayout{}, program.Topics...)
	sort.Slice(topics, func(i, j int) bool {
		return topics[i].Label < topics[j].Label
	})

	layouts := make([]topicDataLayout, 0, len(topics))
	for _, topic := range topics {
		layout, ds := planTopicDataChecked(topic)
		if len(ds) != 0 {
			return nil, ds
		}
		layouts = append(layouts, layout)
	}
	return layouts, nil
}

func topicDataObjects(program *ir.Program) ([]ir.DataObject, []diag.Diagnostic) {
	layouts, ds := orderedTopicDataLayouts(program)
	if len(ds) != 0 {
		return nil, ds
	}

	objects := make([]ir.DataObject, 0, len(layouts))
	for _, layout := range layouts {
		objects = append(objects, ir.DataObject{
			Symbol: "_wrela_topic_" + sanitizeSymbol(layout.Label),
			Bytes:  make([]byte, layout.TotalSize),
			Align:  cacheLineSize,
		})
	}
	return objects, nil
}

func sanitizeSymbol(label string) string {
	var b strings.Builder
	for _, r := range label {
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	return b.String()
}
