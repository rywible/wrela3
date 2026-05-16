package codegen

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/ir"
)

func TestTopicDataLayoutIsCacheLineAligned(t *testing.T) {
	layout := planTopicData(ir.TopicLayout{
		Label:       "telemetry",
		Kind:        "gap_u64",
		Depth:       8,
		Subscribers: []string{"display", "logger"},
	})

	if layout.TotalSize%cacheLineSize != 0 {
		t.Fatalf("TotalSize = %d, want cache-line multiple", layout.TotalSize)
	}
	if layout.SlotsOffset%cacheLineSize != 0 {
		t.Fatalf("SlotsOffset = %d, want cache-line aligned", layout.SlotsOffset)
	}
	for _, subscriber := range layout.Subscribers {
		if subscriber.CursorOffset%cacheLineSize != 0 {
			t.Fatalf("subscriber %q CursorOffset = %d, want cache-line aligned", subscriber.Label, subscriber.CursorOffset)
		}
	}
}

func TestTopicDataLayoutOrderIsDeterministic(t *testing.T) {
	program := &ir.Program{
		Topics: []ir.TopicLayout{
			{Label: "zeta", Depth: 2},
			{Label: "alpha", Depth: 2},
			{Label: "middle", Depth: 2},
		},
	}

	objects, ds := orderedTopicDataLayouts(program)
	if len(ds) != 0 {
		t.Fatalf("orderedTopicDataLayouts diagnostics = %#v, want none", ds)
	}

	want := []string{"alpha", "middle", "zeta"}
	if len(objects) != len(want) {
		t.Fatalf("len(orderedTopicDataLayouts) = %d, want %d", len(objects), len(want))
	}
	for i := range want {
		if objects[i].Label != want[i] {
			t.Fatalf("layout %d label = %q, want %q", i, objects[i].Label, want[i])
		}
	}
}

func TestTopicDataObjectStartsAligned(t *testing.T) {
	program := &ir.Program{
		WritableData: []ir.DataObject{{Symbol: "prefix", Bytes: []byte{0xAA}}},
		Topics:       []ir.TopicLayout{{Label: "sensor/value", Depth: 4}},
	}

	img, ds := Compile(program)
	if len(ds) != 0 {
		t.Fatalf("Compile diagnostics = %#v, want none", ds)
	}

	symbol := "_wrela_topic_sensor_value"
	rva, ok := img.Symbols[symbol]
	if !ok {
		t.Fatalf("Compile() symbols missing %q", symbol)
	}
	data := sectionByName(img, ".data")
	if data == nil {
		t.Fatal("Compile() missing .data section")
	}
	if (rva-data.RVA)%cacheLineSize != 0 {
		t.Fatalf("%s offset = %d, want cache-line aligned", symbol, rva-data.RVA)
	}
}

func TestTopicDataRejectsNonPowerOfTwoDepth(t *testing.T) {
	_, ds := planTopicDataChecked(ir.TopicLayout{Label: "bad", Depth: 3})

	if !hasCode(ds, diag.SEM0046) {
		t.Fatalf("planTopicDataChecked diagnostics = %#v, want SEM0046", ds)
	}
}

func hasCode(ds []diag.Diagnostic, code string) bool {
	for _, d := range ds {
		if d.Code == code {
			return true
		}
	}
	return false
}
