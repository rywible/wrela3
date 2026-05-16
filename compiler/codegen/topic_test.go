package codegen

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/ir"
)

func TestGapTopicPublishStoresSequenceAndValue(t *testing.T) {
	program := topicProgramForCodegenTest()

	image, diags := Compile(program)
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}

	code := symbolBytes(t, image, "publish_counter")
	topicAddress, ok := image.Symbols["_wrela_topic_counter"]
	if !ok {
		t.Fatal("Compile() symbols missing _wrela_topic_counter")
	}
	addressBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(addressBytes, runtimeImageBase+topicAddress)
	if !bytes.Contains(code, addressBytes) || !bytes.Contains(code, []byte{0x48, 0x89}) {
		t.Fatalf("publish_counter missing topic data address and 64-bit mov store shape: %#x", code)
	}
}

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

func topicProgramForCodegenTest() *ir.Program {
	value := &ir.ConstInt{Symbol: "value", Value: 42, Type: ir.Type{Name: "U64"}}
	return &ir.Program{
		Topics: []ir.TopicLayout{{
			Label:       "counter",
			Kind:        "gap_u64",
			Depth:       8,
			Subscribers: []string{"worker"},
		}},
		Types: map[string]ir.TypeInfo{
			"U64TopicMessage": {
				Name: "U64TopicMessage", Kind: ir.TypeKindData, Size: 16, Align: 8, StorageSize: 16,
				Fields: map[string]ir.FieldInfo{
					"sequence": {Name: "sequence", Type: ir.Type{Name: "U64"}, Offset: 0, Size: 8, Align: 8, StorageOffset: 0, StorageSize: 8},
					"value":    {Name: "value", Type: ir.Type{Name: "U64"}, Offset: 8, Size: 8, Align: 8, StorageOffset: 8, StorageSize: 8},
				},
				FieldOrder: []string{"sequence", "value"},
			},
			"U64TopicNext": {
				Name: "U64TopicNext", Kind: ir.TypeKindData, Size: 40, Align: 8, StorageSize: 40,
				Fields: map[string]ir.FieldInfo{
					"has_message": {Name: "has_message", Type: ir.Type{Name: "Bool"}, Offset: 0, Size: 1, Align: 1, StorageOffset: 0, StorageSize: 1},
					"gap":         {Name: "gap", Type: ir.Type{Name: "Bool"}, Offset: 1, Size: 1, Align: 1, StorageOffset: 1, StorageSize: 1},
					"missed":      {Name: "missed", Type: ir.Type{Name: "U64"}, Offset: 8, Size: 8, Align: 8, StorageOffset: 8, StorageSize: 8},
					"message":     {Name: "message", Type: ir.Type{Name: "U64TopicMessage", Kind: ir.TypeKindData}, Offset: 16, Size: 16, Align: 8, StorageOffset: 16, StorageSize: 16},
				},
				FieldOrder: []string{"has_message", "gap", "missed", "message"},
			},
		},
		Functions: []ir.Function{{
			Symbol: "publish_counter",
			Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
				value,
				&ir.TopicPublish{TopicLabel: "counter", Kind: "gap_u64", Value: value},
				&ir.Return{},
			}}},
		}},
	}
}
