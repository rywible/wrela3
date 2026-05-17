package codegen

import (
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

func hasCallReloc(unit compiledUnit, symbol string) bool {
	for _, reloc := range unit.CallReloc {
		if reloc.Symbol == symbol {
			return true
		}
	}
	return false
}
