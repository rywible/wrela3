package codegen

import (
	"testing"

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
