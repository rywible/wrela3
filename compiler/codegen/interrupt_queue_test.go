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

func TestInterruptQueueDropNewestSetsOverflowFlag(t *testing.T) {
	q := ir.InterruptQueueLayout{Label: "irq.serial.rx", Capacity: 1, PayloadSize: 8, PayloadAlign: 8, Overflow: "drop_newest_and_set_flag"}
	unit := compileInterruptQueuePushForTest(q)
	if !containsBytes(unit.Bytes, []byte{0xC6, 0x40, 0x18, 0x01}) {
		t.Fatalf("drop-newest queue push must set overflow flag: %x", unit.Bytes)
	}
}

func TestInterruptQueueDropOldestAdvancesHeadAndSetsOverflowFlag(t *testing.T) {
	q := ir.InterruptQueueLayout{Label: "irq.serial.rx", Capacity: 1, PayloadSize: 8, PayloadAlign: 8, Overflow: "drop_oldest_and_set_flag"}
	unit := compileInterruptQueuePushForTest(q)
	if !containsBytes(unit.Bytes, []byte{0xC6, 0x40, 0x18, 0x01}) {
		t.Fatalf("drop-oldest queue push must set overflow flag: %x", unit.Bytes)
	}
	if !containsBytes(unit.Bytes, []byte{0x48, 0x89}) {
		t.Fatalf("drop-oldest queue push must rewrite head/tail state: %x", unit.Bytes)
	}
}

func TestInterruptQueueSetFlagAndWakeCallsWakeTarget(t *testing.T) {
	q := ir.InterruptQueueLayout{Label: "irq.serial.rx", Capacity: 1, PayloadSize: 8, PayloadAlign: 8, Overflow: "set_flag_and_wake"}
	unit := compileInterruptQueuePushForTest(q)
	if !containsBytes(unit.Bytes, []byte{0xC6, 0x40, 0x18, 0x01}) {
		t.Fatalf("set-flag queue push must set overflow flag: %x", unit.Bytes)
	}
	if !hasCallReloc(unit, "_wrela_interrupt_queue_wake_overflow_owner") {
		t.Fatalf("set-flag queue push must wake overflow owner, relocs = %#v", unit.CallReloc)
	}
}

func TestInterruptQueueBootFatalCallsOverflowTrap(t *testing.T) {
	q := ir.InterruptQueueLayout{Label: "irq.serial.rx", Capacity: 1, PayloadSize: 8, PayloadAlign: 8, Overflow: "boot_fatal"}
	unit := compileInterruptQueuePushForTest(q)
	if !hasCallReloc(unit, "_wrela_interrupt_queue_overflow") {
		t.Fatalf("boot-fatal queue push must call overflow trap, relocs = %#v", unit.CallReloc)
	}
}

func hasCallReloc(unit compiledUnit, symbol string) bool {
	for _, reloc := range unit.CallReloc {
		if reloc.Symbol == symbol {
			return true
		}
	}
	return false
}
