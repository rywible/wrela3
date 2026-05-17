package codegen

import (
	"bytes"
	"testing"

	"github.com/ryanwible/wrela3/compiler/asm"
	"github.com/ryanwible/wrela3/compiler/ir"
)

func timerProgramForCodegenTest(t *testing.T) *ir.Program {
	t.Helper()
	return &ir.Program{
		Types: map[string]ir.TypeInfo{},
		Topics: []ir.TopicLayout{{
			Label: "timer.periodic",
			Kind:  "timer_tick",
			Depth: 64,
			PayloadType: ir.Type{
				Name:   "TimerTickPayload",
				Module: "machine.x86_64.topic_payload",
				Kind:   ir.TypeKindData,
			},
			PayloadSize:  24,
			PayloadAlign: 8,
			Subscribers:  []string{"worker"},
		}},
		Timers: []ir.TimerRoute{{
			Label:           "periodic.1000us",
			Source:          "local_apic_pit_calibrated",
			PeriodUS:        1000,
			Vector:          0x43,
			SubscriberSlots: []string{"worker"},
		}},
		VcpuStarts: []ir.VcpuStartPlan{{VcpuID: 1, APICID: 1, SlotLabel: "worker"}},
	}
}

func timerProgramForCodegenTestWithPayloadSize(t *testing.T, payloadSize uint64) *ir.Program {
	t.Helper()
	program := timerProgramForCodegenTest(t)
	program.Topics[0].PayloadSize = payloadSize
	return program
}

func TestTimerVectorPublishesTickTopic(t *testing.T) {
	program := timerProgramForCodegenTest(t)
	img, ds := Compile(program)
	if len(ds) != 0 {
		t.Fatalf("Compile diagnostics: %#v", ds)
	}
	code := symbolBytes(t, img, "_wrela_interrupt_vector43_timer")
	if !containsBytes(code, []byte{0x48, 0xCF}) {
		t.Fatalf("timer vector missing iretq: %x", code)
	}
	if !containsBytes(code, []byte{0xB0, 0x00}) {
		t.Fatalf("timer vector must EOI local APIC before return: %x", code)
	}
}

func TestTimerPublishWritesPayloadBeforeSequenceStampAndHeadAfterSequence(t *testing.T) {
	program := timerProgramForCodegenTestWithPayloadSize(t, 120)
	layout, ds := planTopicDataChecked(program.Topics[0])
	if len(ds) != 0 {
		t.Fatalf("plan topic layout diagnostics = %#v", ds)
	}
	if layout.SlotSize != 128 {
		t.Fatalf("slot size = %d, want 128", layout.SlotSize)
	}
	img, ds := Compile(program)
	if len(ds) != 0 {
		t.Fatalf("Compile diagnostics: %#v", ds)
	}
	code := symbolBytes(t, img, "_wrela_interrupt_vector43_timer")

	payloadSequenceAtOffset := bytes.Index(code, mustEncode(t, asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.MemOperand{Base: asm.MustLookup("r11"), Disp: 8, Width: 64},
		asm.RegOperand{Reg: asm.MustLookup("r10")},
	}}))
	payloadMonotonicAtOffset := bytes.Index(code, mustEncode(t, asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.MemOperand{Base: asm.MustLookup("r11"), Disp: 16, Width: 64},
		asm.RegOperand{Reg: asm.MustLookup("rax")},
	}}))
	payloadSourceIdAtOffset := bytes.Index(code, mustEncode(t, asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.MemOperand{Base: asm.MustLookup("r11"), Disp: 24, Width: 32},
		asm.RegOperand{Reg: asm.MustLookup("rcx")},
	}}))
	sequenceOffset0 := bytes.Index(code, mustEncode(t, asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.MemOperand{Base: asm.MustLookup("r11"), Disp: 0, Width: 64},
		asm.RegOperand{Reg: asm.MustLookup("r10")},
	}}))
	if payloadSequenceAtOffset < 0 || payloadMonotonicAtOffset < 0 || payloadSourceIdAtOffset < 0 || sequenceOffset0 < 0 {
		t.Fatalf("timer publish missing sequence/payload stores: payload_seq=%d mono=%d source=%d stamp=%d", payloadSequenceAtOffset, payloadMonotonicAtOffset, payloadSourceIdAtOffset, sequenceOffset0)
	}
	if payloadSequenceAtOffset > sequenceOffset0 || payloadMonotonicAtOffset > sequenceOffset0 || payloadSourceIdAtOffset > sequenceOffset0 {
		t.Fatalf("timer publish wrote sequence stamp before payload bytes: stamp=%d payload_seq=%d mono=%d source=%d", sequenceOffset0, payloadSequenceAtOffset, payloadMonotonicAtOffset, payloadSourceIdAtOffset)
	}

	publishHeadOffset := bytes.Index(code, mustEncode(t, asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.MemOperand{Base: asm.MustLookup("r12"), Disp: int64(layout.HeadOffset), Width: 64},
		asm.RegOperand{Reg: asm.MustLookup("r10")},
	}}))
	if publishHeadOffset < 0 {
		t.Fatalf("timer publish missing head update")
	}
	if publishHeadOffset < sequenceOffset0 {
		t.Fatalf("timer publish updates head before sequence stamp: head=%d seq=%d", publishHeadOffset, sequenceOffset0)
	}
}

func TestTimerPublishDerivesSlotStrideFromTopicLayoutSlotSize(t *testing.T) {
	program := timerProgramForCodegenTestWithPayloadSize(t, 184)
	layout, ds := planTopicDataChecked(program.Topics[0])
	if len(ds) != 0 {
		t.Fatalf("plan topic layout diagnostics = %#v", ds)
	}
	img, ds := Compile(program)
	if len(ds) != 0 {
		t.Fatalf("Compile diagnostics: %#v", ds)
	}
	code := symbolBytes(t, img, "_wrela_interrupt_vector43_timer")

	if layout.SlotSize != 192 {
		t.Fatalf("slot size = %d, want 192", layout.SlotSize)
	}
	strideLoad := mustEncode(t, asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("rdx")},
		asm.ImmOperand{Value: int64(layout.SlotSize)},
	}})
	if !containsBytes(code, strideLoad) {
		t.Fatalf("timer publish missing slot-size load for stride %d: %#x", layout.SlotSize, code)
	}
	strideMultiply := []byte{0x4C, 0x0F, 0xAF, 0xDA}
	if !containsBytes(code, strideMultiply) {
		t.Fatalf("timer publish missing slot scaling multiply by layout slot size: %#x", code)
	}
	legacyShift := []byte{0x49, 0xC1, 0xE3, 6}
	if containsBytes(code, legacyShift) {
		t.Fatalf("timer publish still hard-codes slot size 64-byte stride")
	}
}

func TestTimerPublishMonotonicIsSequenceBased(t *testing.T) {
	program := timerProgramForCodegenTest(t)
	img, ds := Compile(program)
	if len(ds) != 0 {
		t.Fatalf("Compile diagnostics: %#v", ds)
	}
	code := symbolBytes(t, img, "_wrela_interrupt_vector43_timer")
	periodLoad := mustEncode(t, asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("rcx")},
		asm.ImmOperand{Value: int64(timerPeriodUS(program.Timers[0].PeriodUS))},
	}})
	if !containsBytes(code, periodLoad) {
		t.Fatalf("timer publish missing period immediate load")
	}
	multiply := []byte{0x48, 0x0F, 0xAF, 0xC1}
	if !containsBytes(code, multiply) {
		t.Fatalf("timer publish missing sequence-based monotonic multiplication")
	}
	multiplyOffset := bytes.Index(code, multiply)
	if multiplyOffset < 0 {
		t.Fatalf("timer publish missing monotonic multiplication offset")
	}
	monotonicStore := mustEncode(t, asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.MemOperand{Base: asm.MustLookup("r11"), Disp: 16, Width: 64},
		asm.RegOperand{Reg: asm.MustLookup("rax")},
	}})
	if !containsBytes(code, monotonicStore) {
		t.Fatalf("timer publish missing monotonic_us store")
	}
	monotonicStoreOffset := bytes.Index(code, monotonicStore)
	if monotonicStoreOffset < multiplyOffset {
		t.Fatalf("timer publish stores monotonic before multiplication: mono=%d mul=%d", monotonicStoreOffset, multiplyOffset)
	}
}

func TestTimerInitProgramsPitAndLapicTimer(t *testing.T) {
	program := &ir.Program{
		Functions: []ir.Function{{
			Symbol: "timer_init",
			Blocks: []ir.Block{{Label: "entry", Ops: []ir.Operation{
				&ir.TimerInit{
					Source:   "local_apic_pit_calibrated",
					PeriodUS: 1000,
					Vector:   0x43,
				},
				&ir.Return{},
			}}},
		}},
	}
	img, ds := Compile(program)
	if len(ds) != 0 {
		t.Fatalf("Compile diagnostics: %#v", ds)
	}
	code := symbolBytes(t, img, "timer_init")
	for _, want := range [][]byte{
		{0xE6, 0x43}, // PIT command port
		{0xE6, 0x40}, // PIT channel 0 data port
		u32le(0x20000 | 0x43),
		u32le(0xFFFFFFFF),
		u32le(1000),
		u32le(timerCalibrationUS),
	} {
		if !containsBytes(code, want) {
			t.Fatalf("timer init missing %x in %x", want, code)
		}
	}
}
