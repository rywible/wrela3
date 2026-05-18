package codegen

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/ryanwible/wrela3/compiler/asm"
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
	slotFromProducer := mustEncode(t, asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("r11")},
		asm.RegOperand{Reg: asm.MustLookup("r10")},
	}})
	incrementProducer := mustEncode(t, asm.Instruction{Mnemonic: "add", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("r10")},
		asm.ImmOperand{Value: 1},
	}})
	slotAt := bytes.Index(code, slotFromProducer)
	incrementAt := bytes.Index(code, incrementProducer)
	if slotAt < 0 || incrementAt < 0 || slotAt > incrementAt {
		t.Fatalf("publish_counter must choose ring slot from old producer sequence before increment: %#x", code)
	}
}

func TestGapTopicPublishPreservesTopicBaseAcrossPayloadCopy(t *testing.T) {
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
	if got := bytes.Count(code, addressBytes); got < 2 {
		t.Fatalf("publish_counter must reload topic base after generic payload copy, address loads = %d: %#x", got, code)
	}
}

func TestGapTopicPublishDerivesSlotStrideFromTopicLayoutSlotSize(t *testing.T) {
	program := topicProgramForCodegenTest()
	program.Topics[0].PayloadSize = 184
	program.Topics[0].PayloadAlign = 8
	layout := planTopicData(program.Topics[0])
	if layout.SlotSize != 192 {
		t.Fatalf("slot size = %d, want 192", layout.SlotSize)
	}

	image, diags := Compile(program)
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}

	code := symbolBytes(t, image, "publish_counter")
	loadStride := mustEncode(t, asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("rdx")},
		asm.ImmOperand{Value: int64(layout.SlotSize)},
	}})
	scaleSlot := []byte{0x4C, 0x0F, 0xAF, 0xDA}
	if !bytes.Contains(code, loadStride) || !bytes.Contains(code, scaleSlot) {
		t.Fatalf("publish_counter must scale ring slot by layout slot size %d: %#x", layout.SlotSize, code)
	}
}

func TestGapTopicPublishCoalescesSubscriberWake(t *testing.T) {
	program := topicProgramForCodegenTest()
	program.VcpuStarts = []ir.VcpuStartPlan{{SlotLabel: "worker", VcpuID: 1}}

	image, diags := Compile(program)
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}

	code := symbolBytes(t, image, "publish_counter")
	if !bytes.Contains(code, []byte{0x0F, 0x84}) {
		t.Fatalf("publish_counter missing JE wake coalescing branch: %#x", code)
	}
	vector := make([]byte, 4)
	binary.LittleEndian.PutUint32(vector, 0x00004000|0xF0)
	if !bytes.Contains(code, vector) {
		t.Fatalf("publish_counter missing subscriber wake IPI vector: %#x", code)
	}
}

func TestTopicWaitlinesStartDisarmed(t *testing.T) {
	objects, diags := topicDataObjects(topicProgramForCodegenTest())
	if len(diags) != 0 {
		t.Fatalf("topicDataObjects diagnostics = %#v", diags)
	}
	if len(objects) != 1 {
		t.Fatalf("topic data objects = %#v, want one", objects)
	}
	layout := planTopicData(topicProgramForCodegenTest().Topics[0])
	if got := binary.LittleEndian.Uint64(objects[0].Bytes[layout.ProducerWaitlineOffset:]); got != topicWaitlineDisarmedBits {
		t.Fatalf("producer waitline = %#x, want disarmed sentinel", got)
	}
	if got := binary.LittleEndian.Uint64(objects[0].Bytes[layout.Subscribers[0].WaitlineOffset:]); got != topicWaitlineDisarmedBits {
		t.Fatalf("subscriber waitline = %#x, want disarmed sentinel", got)
	}
}

func TestTopicPublishDoesNotWakeDisarmedSubscriber(t *testing.T) {
	program := topicProgramForCodegenTest()
	program.VcpuStarts = []ir.VcpuStartPlan{{SlotLabel: "worker", VcpuID: 1}}

	image, diags := Compile(program)
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}

	code := symbolBytes(t, image, "publish_counter")
	sentinel := make([]byte, 8)
	binary.LittleEndian.PutUint64(sentinel, topicWaitlineDisarmedBits)
	if !bytes.Contains(code, sentinel) {
		t.Fatalf("publish_counter must check disarmed waitline sentinel before waking: %#x", code)
	}
	vector := make([]byte, 4)
	binary.LittleEndian.PutUint32(vector, 0x00004000|0xF0)
	sentinelAt := bytes.Index(code, sentinel)
	vectorAt := bytes.Index(code, vector)
	if sentinelAt < 0 || vectorAt < 0 || sentinelAt > vectorAt {
		t.Fatalf("publish_counter must test disarmed sentinel before IPI: sentinel=%d vector=%d code=%#x", sentinelAt, vectorAt, code)
	}
}

func TestWakeSlotAllowsBSPVcpuZero(t *testing.T) {
	e := &Emitter{Labels: map[string]int{}}
	emitWakeSlot(e, "producer", compileContext{SlotVcpu: map[string]int{"producer": 0}})

	vector := make([]byte, 4)
	binary.LittleEndian.PutUint32(vector, 0x00004000|0xF0)
	if !bytes.Contains(e.Code, vector) {
		t.Fatalf("BSP slot wake missing IPI vector: %#x", e.Code)
	}
}

func TestWakeSlotRejectsIncompletePlacementMap(t *testing.T) {
	e := &Emitter{Labels: map[string]int{}}
	emitWakeSlot(e, "consumer", compileContext{SlotVcpu: map[string]int{"producer": 0}})

	if len(e.Diags) != 1 || e.Diags[0].Code != diag.CG0001 {
		t.Fatalf("missing vCPU placement should emit CG0001, got %#v", e.Diags)
	}
}

func TestReliableTopicPublishChecksSlowestSubscriber(t *testing.T) {
	program := topicProgramForCodegenTest()
	program.Topics[0].Kind = "reliable_u64"
	program.Topics[0].Subscribers = []string{"worker", "logger"}
	program.Functions[0].Blocks[0].Ops = []ir.Operation{
		program.Functions[0].Blocks[0].Ops[0],
		&ir.ReliableTopicTryPublish{
			TopicLabel: "counter",
			Value:      program.Functions[0].Blocks[0].Ops[0].(ir.Value),
			Type:       ir.Type{Name: "Result<Unit, TopicFull>", Kind: ir.TypeKindEnum},
		},
		&ir.Return{},
	}

	image, diags := Compile(program)
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}

	code := symbolBytes(t, image, "publish_counter")
	if !bytes.Contains(code, []byte{0x48, 0x3B}) && !bytes.Contains(code, []byte{0x4C, 0x3B}) {
		t.Fatalf("publish_counter missing 64-bit subscriber cursor compare shape: %#x", code)
	}
}

func TestReliableTopicPublishRejectsLegacyDataResultLayout(t *testing.T) {
	program := topicProgramForCodegenTest()
	program.Topics[0].Kind = "reliable_u64"
	program.Types["LegacyPublishResult"] = ir.TypeInfo{
		Name: "LegacyPublishResult", Kind: ir.TypeKindData, Size: 2, Align: 1, StorageSize: 2,
		Fields: map[string]ir.FieldInfo{
			"published": {Name: "published", Type: ir.Type{Name: "Bool"}, Offset: 0, Size: 1, Align: 1, StorageOffset: 0, StorageSize: 1},
			"full":      {Name: "full", Type: ir.Type{Name: "Bool"}, Offset: 1, Size: 1, Align: 1, StorageOffset: 1, StorageSize: 1},
		},
	}
	value := &ir.ConstInt{Symbol: "value", Value: 42, Type: ir.Type{Name: "U64"}}
	program.Functions = []ir.Function{{
		Symbol: "publish_counter_legacy_result",
		Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
			value,
			&ir.ReliableTopicTryPublish{TopicLabel: "counter", Value: value, Type: ir.Type{Name: "LegacyPublishResult", Kind: ir.TypeKindData}},
			&ir.Return{},
		}}},
	}}

	_, ds := Compile(program)
	if !hasCode(ds, diag.CG0001) {
		t.Fatalf("legacy reliable publish data result diagnostics = %#v, want CG0001", ds)
	}
}

func TestReliableTopicWaitRechecksCapacityAfterArmingWaitline(t *testing.T) {
	program := topicProgramForCodegenTest()
	program.Topics[0].Kind = "reliable_u64"
	program.Topics[0].Subscribers = []string{"worker", "logger"}
	program.Functions[0] = ir.Function{
		Symbol: "wait_counter_producer",
		Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
			&ir.ReliableTopicWaitForAdvance{TopicLabel: "counter", PublisherSlot: "producer"},
			&ir.Return{},
		}}},
	}

	image, diags := Compile(program)
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}

	code := symbolBytes(t, image, "wait_counter_producer")
	storeWaitline := mustEncode(t, asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.MemOperand{Base: asm.MustLookup("rax"), Disp: int64(cacheLineSize), Width: 64},
		asm.RegOperand{Reg: asm.MustLookup("r11")},
	}})
	storeAt := bytes.Index(code, storeWaitline)
	if storeAt < 0 {
		t.Fatalf("wait_counter_producer missing producer waitline store %x in %x", storeWaitline, code)
	}
	recheckLoad := mustEncode(t, asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("r11")},
		asm.MemOperand{Base: asm.MustLookup("rax"), Disp: int64(2 * cacheLineSize), Width: 64},
	}})
	if !bytes.Contains(code[storeAt+len(storeWaitline):], recheckLoad) {
		t.Fatalf("wait_counter_producer must re-read subscriber cursor after arming waitline: %#x", code)
	}
	hltAt := bytes.Index(code, []byte{0xFB, 0xF4})
	if hltAt < 0 || hltAt < storeAt {
		t.Fatalf("wait_counter_producer must use sti/hlt after recheck: %#x", code)
	}
}

func TestTopicArmWaitStoresStaticSubscriberWaitline(t *testing.T) {
	sub := &ir.Param{Symbol: "input", Type: ir.Type{Name: "TopicSubscription<U64>", Kind: ir.TypeKindClass}}
	program := topicProgramForCodegenTest()
	program.Functions[0] = ir.Function{
		Symbol: "arm_counter",
		Params: []ir.Value{sub},
		Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
			&ir.TopicArmWait{TopicLabel: "counter", SubscriberSlot: "worker", Subscription: sub},
			&ir.Return{},
		}}},
	}

	image, diags := Compile(program)
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}

	code := symbolBytes(t, image, "arm_counter")
	topicAddress := make([]byte, 8)
	binary.LittleEndian.PutUint64(topicAddress, runtimeImageBase+image.Symbols["_wrela_topic_counter"])
	if !bytes.Contains(code, topicAddress) {
		t.Fatalf("arm_counter missing topic data address: %#x", code)
	}

	frame := buildFrame(program.Functions[0], compileContext{types: program.Types})
	subscriptionLoad := mustEncode(t, asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("r11")},
		asm.MemOperand{Base: asm.MustLookup("rbp"), Disp: int64(frame.Slots[sub]), Width: 64},
	}})
	if bytes.Contains(code, subscriptionLoad) {
		t.Fatalf("static arm_wait must not write through runtime subscription object: %#x", code)
	}
}

func TestTopicIsWaitArmedComparesStaticCursorAndWaitline(t *testing.T) {
	sub := &ir.Param{Symbol: "input", Type: ir.Type{Name: "TopicSubscription<U64>", Kind: ir.TypeKindClass}}
	program := topicProgramForCodegenTest()
	program.Functions[0] = ir.Function{
		Symbol: "is_counter_armed",
		Params: []ir.Value{sub},
		Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
			&ir.TopicIsWaitArmed{TopicLabel: "counter", SubscriberSlot: "worker", Subscription: sub, Type: ir.Type{Name: "Bool", Kind: ir.TypeKindPrimitive}},
			&ir.Return{},
		}}},
	}

	image, diags := Compile(program)
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}

	code := symbolBytes(t, image, "is_counter_armed")
	topicAddress := make([]byte, 8)
	binary.LittleEndian.PutUint64(topicAddress, runtimeImageBase+image.Symbols["_wrela_topic_counter"])
	sentinel := make([]byte, 8)
	binary.LittleEndian.PutUint64(sentinel, topicWaitlineDisarmedBits)
	if !bytes.Contains(code, topicAddress) || !bytes.Contains(code, sentinel) || !bytes.Contains(code, []byte{0x0F, 0x94, 0xC0}) {
		t.Fatalf("is_counter_armed must reject disarmed sentinel, compare static cursor/waitline, and materialize equality: %#x", code)
	}
}

func TestTopicIsWaitArmedValueFormGetsFrameSlot(t *testing.T) {
	sub := &ir.Param{Symbol: "input", Type: ir.Type{Name: "TopicSubscription<U64>", Kind: ir.TypeKindClass}}
	armed := ir.TopicIsWaitArmed{TopicLabel: "counter", SubscriberSlot: "worker", Subscription: sub, Type: ir.Type{Name: "Bool", Kind: ir.TypeKindPrimitive}}
	program := topicProgramForCodegenTest()
	program.Functions[0] = ir.Function{
		Symbol: "is_counter_armed_value_form",
		Params: []ir.Value{sub},
		Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
			armed,
			&ir.Return{Value: armed},
		}}},
	}

	image, diags := Compile(program)
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}

	code := symbolBytes(t, image, "is_counter_armed_value_form")
	if !bytes.Contains(code, []byte{0x0F, 0x94, 0xC0}) {
		t.Fatalf("value-form is_wait_armed must emit equality materialization: %#x", code)
	}
}

func TestTopicWaitIfArmedUsesCliStiHltSequence(t *testing.T) {
	sub := &ir.Param{Symbol: "input", Type: ir.Type{Name: "TopicSubscription<U64>", Kind: ir.TypeKindClass}}
	program := topicProgramForCodegenTest()
	program.Functions[0] = ir.Function{
		Symbol: "wait_counter_if_armed",
		Params: []ir.Value{sub},
		Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
			&ir.TopicWaitIfArmed{TopicLabel: "counter", SubscriberSlot: "worker", Subscription: sub},
			&ir.Return{},
		}}},
	}

	image, diags := Compile(program)
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}

	code := symbolBytes(t, image, "wait_counter_if_armed")
	if !bytes.Contains(code, []byte{0xFA}) || !bytes.Contains(code, []byte{0xFB, 0xF4}) {
		t.Fatalf("wait_counter_if_armed must use cli plus sti/hlt atomic wait: %#x", code)
	}
}

func TestTopicWaitIfArmedChecksAllGuardsBeforeHlt(t *testing.T) {
	counterSub := &ir.Param{Symbol: "counter_input", Type: ir.Type{Name: "TopicSubscription<U64>", Kind: ir.TypeKindClass}}
	alertSub := &ir.Param{Symbol: "alert_input", Type: ir.Type{Name: "TopicSubscription<U64>", Kind: ir.TypeKindClass}}
	program := topicProgramForCodegenTest()
	program.Topics = append(program.Topics, ir.TopicLayout{
		Label:       "alerts",
		Kind:        "gap_u64",
		Depth:       8,
		Subscribers: []string{"worker"},
	})
	program.Functions[0] = ir.Function{
		Symbol: "wait_all_if_armed",
		Params: []ir.Value{counterSub, alertSub},
		Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
			&ir.TopicWaitIfArmed{
				TopicLabel:     "counter",
				SubscriberSlot: "worker",
				Subscription:   counterSub,
				Guards: []ir.TopicWaitGuard{
					{TopicLabel: "counter", SubscriberSlot: "worker", Subscription: counterSub},
					{TopicLabel: "alerts", SubscriberSlot: "worker", Subscription: alertSub},
				},
			},
			&ir.Return{},
		}}},
	}

	image, diags := Compile(program)
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}

	code := symbolBytes(t, image, "wait_all_if_armed")
	firstTopic := make([]byte, 8)
	binary.LittleEndian.PutUint64(firstTopic, runtimeImageBase+image.Symbols["_wrela_topic_counter"])
	secondTopic := make([]byte, 8)
	binary.LittleEndian.PutUint64(secondTopic, runtimeImageBase+image.Symbols["_wrela_topic_alerts"])
	hltAt := bytes.Index(code, []byte{0xFB, 0xF4})
	if hltAt < 0 {
		t.Fatalf("wait_all_if_armed missing sti/hlt sequence: %#x", code)
	}
	if !bytes.Contains(code[:hltAt], firstTopic) || !bytes.Contains(code[:hltAt], secondTopic) {
		t.Fatalf("wait_all_if_armed must check both topic waitlines before hlt: %#x", code)
	}
}

func TestTopicTryNextWritesOptionLayout(t *testing.T) {
	program := topicProgramForCodegenTest()
	sub := &ir.Local{Symbol: "sub", Type: ir.Type{Name: "TopicSubscription<U64>"}}
	next := &ir.TopicTryNext{
		TopicLabel:     "counter",
		SubscriberSlot: "worker",
		Subscription:   sub,
		Type:           ir.Type{Name: "Option<U64>", Kind: ir.TypeKindEnum},
	}
	program.Types["Option<U64>"] = ir.TypeInfo{
		Name: "Option<U64>", Kind: ir.TypeKindEnum, Size: 16, Align: 8, StorageSize: 16,
		Fields: map[string]ir.FieldInfo{
			"$tag":       {Name: "$tag", Type: ir.Type{Name: "U64"}, Offset: 0, Size: 8, Align: 8, StorageOffset: 0, StorageSize: 8},
			"Some.value": {Name: "Some.value", Type: ir.Type{Name: "U64"}, Offset: 8, Size: 8, Align: 8, StorageOffset: 8, StorageSize: 8},
		},
		EnumVariants: []ir.EnumVariantInfo{{Name: "None", Discriminant: 0}, {Name: "Some", Discriminant: 1, Fields: []string{"Some.value"}}},
	}
	program.Functions = []ir.Function{{
		Symbol: "try_counter_option",
		Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
			sub,
			next,
			&ir.Return{},
		}}},
	}}
	image, ds := Compile(program)
	if len(ds) != 0 {
		t.Fatalf("Compile diagnostics: %#v", ds)
	}
	code := symbolBytes(t, image, "try_counter_option")
	if !containsBytes(code, []byte{0x01, 0x00, 0x00, 0x00}) || !containsBytes(code, []byte{0x48, 0x8B, 0x41, 0x08}) {
		t.Fatalf("try_next must write Option.Some tag and copy payload from ring slot offset 8, got %x", code)
	}
}

func TestTopicTryNextRejectsLegacyDataResultLayout(t *testing.T) {
	program := topicProgramForCodegenTest()
	program.Types["LegacyNextResult"] = ir.TypeInfo{
		Name: "LegacyNextResult", Kind: ir.TypeKindData, Size: 32, Align: 8, StorageSize: 32,
		Fields: map[string]ir.FieldInfo{
			"has_message": {Name: "has_message", Type: ir.Type{Name: "Bool"}, Offset: 0, Size: 1, Align: 1, StorageOffset: 0, StorageSize: 1},
			"gap":         {Name: "gap", Type: ir.Type{Name: "Bool"}, Offset: 1, Size: 1, Align: 1, StorageOffset: 1, StorageSize: 1},
			"missed":      {Name: "missed", Type: ir.Type{Name: "U64"}, Offset: 8, Size: 8, Align: 8, StorageOffset: 8, StorageSize: 8},
			"message":     {Name: "message", Type: ir.Type{Name: "U64"}, Offset: 16, Size: 8, Align: 8, StorageOffset: 16, StorageSize: 8},
		},
	}
	sub := &ir.Local{Symbol: "sub", Type: ir.Type{Name: "TopicSubscription<U64>"}}
	next := &ir.TopicTryNext{
		TopicLabel:     "counter",
		SubscriberSlot: "worker",
		Subscription:   sub,
		Type:           ir.Type{Name: "LegacyNextResult", Kind: ir.TypeKindData},
	}
	program.Functions = []ir.Function{{
		Symbol: "try_counter_legacy_result",
		Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
			sub,
			next,
			&ir.Return{},
		}}},
	}}

	_, ds := Compile(program)
	if !hasCode(ds, diag.CG0001) {
		t.Fatalf("legacy try_next data result diagnostics = %#v, want CG0001", ds)
	}
}

func TestTopicTryNextPreservesTopicBaseForSlotCopy(t *testing.T) {
	sub := &ir.Param{Symbol: "input", Type: ir.Type{Name: "TopicSubscription<U64>", Kind: ir.TypeKindClass}}
	next := &ir.TopicTryNext{TopicLabel: "counter", SubscriberSlot: "worker", Subscription: sub, Type: ir.Type{Name: "Option<U64>", Kind: ir.TypeKindEnum}}
	program := topicProgramForCodegenTest()
	program.Functions[0] = ir.Function{
		Symbol: "try_counter",
		Params: []ir.Value{sub},
		Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
			next,
			&ir.Return{},
		}}},
	}

	image, diags := Compile(program)
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}

	code := symbolBytes(t, image, "try_counter")
	topicAddress, ok := image.Symbols["_wrela_topic_counter"]
	if !ok {
		t.Fatal("Compile() symbols missing _wrela_topic_counter")
	}
	addressLoad := []byte{0x48, 0xB8}
	addressBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(addressBytes, runtimeImageBase+topicAddress)
	addressLoad = append(addressLoad, addressBytes...)
	topicBaseAt := bytes.Index(code, addressLoad)
	if topicBaseAt < 0 {
		t.Fatalf("try_counter missing topic base load %x in %x", addressLoad, code)
	}

	addSlotToBase := mustEncode(t, asm.Instruction{Mnemonic: "add", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("rcx")},
		asm.RegOperand{Reg: asm.MustLookup("rax")},
	}})
	slotAddressAt := bytes.Index(code[topicBaseAt:], addSlotToBase)
	if slotAddressAt < 0 {
		t.Fatalf("try_counter missing ring slot address add %x in %x", addSlotToBase, code)
	}

	clobberBase := mustEncode(t, asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("rax")},
		asm.ImmOperand{Value: 1},
	}})
	if clobberAt := bytes.Index(code[topicBaseAt:topicBaseAt+slotAddressAt], clobberBase); clobberAt >= 0 {
		t.Fatalf("try_counter clobbers topic base in rax before slot copy at byte %d: %#x", topicBaseAt+clobberAt, code)
	}
}

func TestGapTopicTryNextComparesSlotSequenceBeforePayload(t *testing.T) {
	sub := &ir.Param{Symbol: "input", Type: ir.Type{Name: "TopicSubscription<U64>", Kind: ir.TypeKindClass}}
	next := &ir.TopicTryNext{TopicLabel: "counter", SubscriberSlot: "worker", Subscription: sub, Type: ir.Type{Name: "Option<U64>", Kind: ir.TypeKindEnum}}
	program := topicProgramForCodegenTest()
	program.Functions[0] = ir.Function{
		Symbol: "try_counter",
		Params: []ir.Value{sub},
		Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
			next,
			&ir.Return{},
		}}},
	}

	image, diags := Compile(program)
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}

	code := symbolBytes(t, image, "try_counter")
	sequenceCompare := mustEncode(t, asm.Instruction{Mnemonic: "cmp", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("rdx")},
		asm.RegOperand{Reg: asm.MustLookup("r11")},
	}})
	compareAt := bytes.Index(code, sequenceCompare)
	if compareAt < 0 {
		t.Fatalf("try_counter must compare ring slot sequence with expected sequence %x in %x", sequenceCompare, code)
	}
	if !bytes.Contains(code[compareAt:], []byte{0x0F, 0x85}) {
		t.Fatalf("try_counter must branch to gap result on slot sequence mismatch: %#x", code)
	}
	rewindCursor := []byte{0x49, 0x29, 0xFB}
	if bytes.Contains(code, rewindCursor) {
		t.Fatalf("gap overflow must advance cursor to producer, not rewind and return a payload: %#x", code)
	}
}

func TestGapTopicTryNextDerivesSlotStrideFromTopicLayoutSlotSize(t *testing.T) {
	sub := &ir.Param{Symbol: "input", Type: ir.Type{Name: "TopicSubscription<U64>", Kind: ir.TypeKindClass}}
	next := &ir.TopicTryNext{TopicLabel: "counter", SubscriberSlot: "worker", Subscription: sub, Type: ir.Type{Name: "Option<U64>", Kind: ir.TypeKindEnum}}
	program := topicProgramForCodegenTest()
	program.Topics[0].PayloadSize = 184
	program.Topics[0].PayloadAlign = 8
	program.Functions[0] = ir.Function{
		Symbol: "try_counter",
		Params: []ir.Value{sub},
		Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
			next,
			&ir.Return{},
		}}},
	}
	layout := planTopicData(program.Topics[0])
	if layout.SlotSize != 192 {
		t.Fatalf("slot size = %d, want 192", layout.SlotSize)
	}

	image, diags := Compile(program)
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}

	code := symbolBytes(t, image, "try_counter")
	loadStride := mustEncode(t, asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("rdi")},
		asm.ImmOperand{Value: int64(layout.SlotSize)},
	}})
	scaleSlot := []byte{0x48, 0x0F, 0xAF, 0xCF}
	if !bytes.Contains(code, loadStride) || !bytes.Contains(code, scaleSlot) {
		t.Fatalf("try_counter must scale ring slot by layout slot size %d: %#x", layout.SlotSize, code)
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
	if layout.ProducerWaitlineOffset%cacheLineSize != 0 {
		t.Fatalf("ProducerWaitlineOffset = %d, want cache-line aligned", layout.ProducerWaitlineOffset)
	}
	if layout.TotalSize < 2*cacheLineSize+2*cacheLineSize+8*cacheLineSize {
		t.Fatalf("TotalSize = %d, want producer line, producer waitline, cursor+waitline, and cache-line ring slots", layout.TotalSize)
	}
	for _, subscriber := range layout.Subscribers {
		if subscriber.CursorOffset%cacheLineSize != 0 {
			t.Fatalf("subscriber %q CursorOffset = %d, want cache-line aligned", subscriber.Label, subscriber.CursorOffset)
		}
		if subscriber.WaitlineOffset%cacheLineSize != 0 {
			t.Fatalf("subscriber %q WaitlineOffset = %d, want cache-line aligned", subscriber.Label, subscriber.WaitlineOffset)
		}
	}
}

func TestTypedTopicDataUsesPayloadSlotSize(t *testing.T) {
	layout := planTopicData(ir.TopicLayout{
		Label:        "timer.periodic",
		Kind:         "timer_tick",
		Depth:        64,
		PayloadSize:  24,
		PayloadAlign: 8,
		Subscribers:  []string{"worker"},
	})
	wantSlot := uint64(64)
	if got := layout.SlotSize; got != wantSlot {
		t.Fatalf("slot size = %d, want %d", got, wantSlot)
	}
}

func TestReliableTopicRequiresSubscriber(t *testing.T) {
	_, diags := planTopicDataChecked(ir.TopicLayout{
		Label: "commands",
		Kind:  "reliable_u64",
		Depth: 8,
	})
	if len(diags) != 1 || diags[0].Code != diag.CG0001 {
		t.Fatalf("subscriberless reliable topic diagnostics = %#v, want CG0001", diags)
	}
}

func TestTopicDataLayoutOrderIsDeterministic(t *testing.T) {
	program := &ir.Program{
		Topics: []ir.TopicLayout{
			{Label: "zeta", Depth: 2, PayloadSize: 8, PayloadAlign: 8},
			{Label: "alpha", Depth: 2, PayloadSize: 8, PayloadAlign: 8},
			{Label: "middle", Depth: 2, PayloadSize: 8, PayloadAlign: 8},
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
		Topics:       []ir.TopicLayout{{Label: "sensor/value", Depth: 4, PayloadSize: 8, PayloadAlign: 8}},
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
	_, ds := planTopicDataChecked(ir.TopicLayout{Label: "bad", Depth: 3, PayloadSize: 8, PayloadAlign: 8})

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
			"Option<U64>": {
				Name: "Option<U64>", Kind: ir.TypeKindEnum, Size: 16, Align: 8, StorageSize: 16,
				Fields: map[string]ir.FieldInfo{
					"$tag":       {Name: "$tag", Type: ir.Type{Name: "U64"}, Offset: 0, Size: 8, Align: 8, StorageOffset: 0, StorageSize: 8},
					"Some.value": {Name: "Some.value", Type: ir.Type{Name: "U64"}, Offset: 8, Size: 8, Align: 8, StorageOffset: 8, StorageSize: 8},
				},
				EnumVariants: []ir.EnumVariantInfo{{Name: "None", Discriminant: 0}, {Name: "Some", Discriminant: 1, Fields: []string{"Some.value"}}},
			},
			"Result<Unit, TopicFull>": {
				Name: "Result<Unit, TopicFull>", Kind: ir.TypeKindEnum, Size: 16, Align: 8, StorageSize: 16,
				Fields: map[string]ir.FieldInfo{
					"$tag":     {Name: "$tag", Type: ir.Type{Name: "U64"}, Offset: 0, Size: 8, Align: 8, StorageOffset: 0, StorageSize: 8},
					"Ok.value": {Name: "Ok.value", Type: ir.Type{Name: "Unit", Kind: ir.TypeKindData}, Offset: 8, Size: 0, Align: 1, StorageOffset: 8, StorageSize: 0},
				},
				EnumVariants: []ir.EnumVariantInfo{{Name: "Ok", Discriminant: 0, Fields: []string{"Ok.value"}}, {Name: "Err", Discriminant: 1}},
			},
			"TopicSubscription<U64>": {
				Name: "TopicSubscription<U64>", Kind: ir.TypeKindClass, Size: 32, Align: 8, StorageSize: 32,
				Fields: map[string]ir.FieldInfo{
					"topic":      {Name: "topic", Type: ir.Type{Name: "Topic<U64>", Kind: ir.TypeKindClass}, Offset: 0, Size: 8, Align: 8, StorageOffset: 0, StorageSize: 8},
					"subscriber": {Name: "subscriber", Type: ir.Type{Name: "ExecutorSlot", Kind: ir.TypeKindData}, Offset: 8, Size: 8, Align: 8, StorageOffset: 8, StorageSize: 8},
					"cursor":     {Name: "cursor", Type: ir.Type{Name: "U64"}, Offset: 16, Size: 8, Align: 8, StorageOffset: 16, StorageSize: 8},
					"armed":      {Name: "armed", Type: ir.Type{Name: "Bool"}, Offset: 24, Size: 1, Align: 1, StorageOffset: 24, StorageSize: 1},
				},
				FieldOrder: []string{"topic", "subscriber", "cursor", "armed"},
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
