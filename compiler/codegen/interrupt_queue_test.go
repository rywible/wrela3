package codegen

import (
	"bytes"
	"testing"

	"github.com/ryanwible/wrela3/compiler/ir"
)

func compileInterruptQueuePushForTest(q ir.InterruptQueueLayout) compiledUnit {
	e := &Emitter{Labels: map[string]int{}, ctx: compileContext{}}
	emitInterruptQueuePush(e, q)
	e.resolveJumps()
	return compiledUnit{Symbol: "interrupt_queue_push_test", Bytes: e.Code, CallReloc: e.CallReloc, DataReloc: e.DataReloc}
}

func compileInterruptQueuePushWithContextForTest(q ir.InterruptQueueLayout, ctx compileContext) compiledUnit {
	e := &Emitter{Labels: map[string]int{}, ctx: ctx}
	emitInterruptQueuePush(e, q)
	e.resolveJumps()
	return compiledUnit{Symbol: "interrupt_queue_push_test", Bytes: e.Code, CallReloc: e.CallReloc, DataReloc: e.DataReloc}
}

func TestInterruptQueueDropNewestSetsOverflowFlag(t *testing.T) {
	q := ir.InterruptQueueLayout{Label: "irq.serial.rx", Capacity: 1, PayloadSize: 8, PayloadAlign: 8, Overflow: "drop_newest_and_set_flag"}
	unit := compileInterruptQueuePushForTest(q)
	if !containsBytes(unit.Bytes, []byte{0x45, 0x88, 0x50, 0x18}) {
		t.Fatalf("drop-newest queue push must set overflow flag: %x", unit.Bytes)
	}
}

func TestInterruptQueueDropNewestAndSetFlagWakesOwnerOnOverflow(t *testing.T) {
	q := ir.InterruptQueueLayout{Label: "irq.serial.rx", Owner: "console", Capacity: 1, PayloadSize: 8, PayloadAlign: 8, Overflow: "drop_newest_and_set_flag"}
	unit := compileInterruptQueuePushWithContextForTest(q, compileContext{SlotVcpu: map[string]int{"console": 1}})
	if got := countDataRelocs(unit, "_wrela_vcpu1_apic_id_command"); got < 2 {
		t.Fatalf("drop-newest-and-set-flag must wake on overflow and after successful enqueue, got %d data relocs: %#v", got, unit.DataReloc)
	}
}

func TestInterruptQueuePublishesPayloadBeforeTail(t *testing.T) {
	q := ir.InterruptQueueLayout{Label: "irq.serial.rx", Capacity: 4, PayloadSize: 8, PayloadAlign: 8, Vector: 0x41, Overflow: "drop_newest"}
	unit := compileInterruptQueuePushForTest(q)

	payloadStore := []byte{0x45, 0x88, 0x11}
	tailStore := []byte{0x49, 0x89, 0x50, 0x08}
	fence := []byte{0x0F, 0xAE, 0xF0}
	payloadAt := bytes.Index(unit.Bytes, payloadStore)
	fenceAt := bytes.Index(unit.Bytes, fence)
	tailAt := bytes.Index(unit.Bytes, tailStore)
	if payloadAt < 0 || fenceAt < 0 || tailAt < 0 {
		t.Fatalf("queue push missing payload store/fence/tail store: payload=%d fence=%d tail=%d code=%#x", payloadAt, fenceAt, tailAt, unit.Bytes)
	}
	if !(payloadAt < fenceAt && fenceAt < tailAt) {
		t.Fatalf("queue push must publish payload before fenced tail store: payload=%d fence=%d tail=%d code=%#x", payloadAt, fenceAt, tailAt, unit.Bytes)
	}
}

func TestInterruptQueueDropOldestAdvancesHeadAndSetsOverflowFlag(t *testing.T) {
	q := ir.InterruptQueueLayout{Label: "irq.serial.rx", Capacity: 1, PayloadSize: 8, PayloadAlign: 8, Overflow: "drop_oldest_and_set_flag"}
	unit := compileInterruptQueuePushForTest(q)
	if !containsBytes(unit.Bytes, []byte{0x45, 0x88, 0x50, 0x18}) {
		t.Fatalf("drop-oldest queue push must set overflow flag: %x", unit.Bytes)
	}
	if !containsBytes(unit.Bytes, []byte{0x49, 0x89, 0x08}) {
		t.Fatalf("drop-oldest queue push must rewrite head state: %x", unit.Bytes)
	}
}

func TestInterruptQueueSetFlagAndWakeCallsWakeTarget(t *testing.T) {
	q := ir.InterruptQueueLayout{Label: "irq.serial.rx", Owner: "console", Capacity: 1, PayloadSize: 8, PayloadAlign: 8, Overflow: "set_flag_and_wake"}
	unit := compileInterruptQueuePushWithContextForTest(q, compileContext{SlotVcpu: map[string]int{"console": 1}})
	if !containsBytes(unit.Bytes, []byte{0x45, 0x88, 0x50, 0x18}) {
		t.Fatalf("set-flag queue push must set overflow flag: %x", unit.Bytes)
	}
	if !hasDataReloc(unit, "_wrela_vcpu1_apic_id_command") {
		t.Fatalf("set-flag queue push must wake overflow owner, data relocs = %#v", unit.DataReloc)
	}
}

func TestInterruptQueueBootFatalCallsOverflowTrap(t *testing.T) {
	q := ir.InterruptQueueLayout{Label: "irq.serial.rx", Capacity: 1, PayloadSize: 8, PayloadAlign: 8, Overflow: "boot_fatal"}
	unit := compileInterruptQueuePushForTest(q)
	if !hasCallReloc(unit, "_wrela_interrupt_queue_overflow") {
		t.Fatalf("boot-fatal queue push must call overflow trap, relocs = %#v", unit.CallReloc)
	}
}

func hasDataReloc(unit compiledUnit, symbol string) bool {
	for _, reloc := range unit.DataReloc {
		if reloc.Symbol == symbol {
			return true
		}
	}
	return false
}

func countDataRelocs(unit compiledUnit, symbol string) int {
	count := 0
	for _, reloc := range unit.DataReloc {
		if reloc.Symbol == symbol {
			count++
		}
	}
	return count
}

func hasCallReloc(unit compiledUnit, symbol string) bool {
	for _, reloc := range unit.CallReloc {
		if reloc.Symbol == symbol {
			return true
		}
	}
	return false
}
