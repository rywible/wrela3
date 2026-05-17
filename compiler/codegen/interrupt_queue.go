package codegen

import (
	"encoding/binary"
	"sort"

	"github.com/ryanwible/wrela3/compiler/asm"
	"github.com/ryanwible/wrela3/compiler/ir"
)

const interruptQueueHeaderSize = 32
const interruptQueueHeadOffset int64 = 0
const interruptQueueTailOffset int64 = 8
const interruptQueueCapacityOffset int64 = 16
const interruptQueueOverflowOffset int64 = 24

func interruptQueueDataSymbol(label string) string {
	return "_wrela_interrupt_queue_" + sanitizeSymbol(label)
}

func interruptQueueDataObject(q ir.InterruptQueueLayout) ir.DataObject {
	size := uint64(interruptQueueHeaderSize) + q.Capacity*q.PayloadSize
	data := make([]byte, size)
	binary.LittleEndian.PutUint64(data[interruptQueueCapacityOffset:], q.Capacity)
	align := q.PayloadAlign
	if align == 0 {
		align = 8
	}
	return ir.DataObject{
		Symbol: interruptQueueDataSymbol(q.Label),
		Bytes:  data,
		Align:  align,
	}
}

func interruptQueueDataObjects(program *ir.Program) []ir.DataObject {
	if program == nil || len(program.InterruptQueues) == 0 {
		return nil
	}
	queues := append([]ir.InterruptQueueLayout{}, program.InterruptQueues...)
	sort.Slice(queues, func(i, j int) bool {
		return queues[i].Label < queues[j].Label
	})
	out := make([]ir.DataObject, 0, len(queues))
	for _, queue := range queues {
		out = append(out, interruptQueueDataObject(queue))
	}
	return out
}

func emitInterruptQueuePush(e *Emitter, q ir.InterruptQueueLayout) {
	base := asm.MustLookup("rax")
	head := asm.MustLookup("rcx")
	tail := asm.MustLookup("rdx")
	capacity := asm.MustLookup("rsi")
	used := asm.MustLookup("rdi")
	notFull := e.newLabel("interrupt_queue_not_full")
	done := e.newLabel("interrupt_queue_done")

	emitMovDataAddressToReg(e, base, interruptQueueDataSymbol(q.Label))
	emitLoadMemToReg(e, head, base, interruptQueueHeadOffset, 64)
	emitLoadMemToReg(e, tail, base, interruptQueueTailOffset, 64)
	emitLoadMemToReg(e, capacity, base, interruptQueueCapacityOffset, 64)
	emitRegRegMove(e, used, tail)
	emitRegRegOp(e, 0x29, used, head)
	emitCmpRegReg(e, used, capacity)
	e.emitJcc(0x82, notFull)
	emitInterruptQueueOverflowPolicy(e, q, base, head, tail)
	e.emitJmp(done)
	e.bindLabel(notFull)
	emitAddImm(e, tail, 1)
	emitStoreMemFromReg(e, base, interruptQueueTailOffset, tail, 64)
	e.bindLabel(done)
}

func emitInterruptQueueOverflowPolicy(e *Emitter, q ir.InterruptQueueLayout, base asm.Reg, head asm.Reg, tail asm.Reg) {
	switch q.Overflow {
	case "drop_newest", "drop_newest_and_set_flag":
		emitStoreImm8ToMem(e, base, interruptQueueOverflowOffset, 1)
	case "drop_oldest", "drop_oldest_and_set_flag":
		emitStoreImm8ToMem(e, base, interruptQueueOverflowOffset, 1)
		emitAddImm(e, head, 1)
		emitStoreMemFromReg(e, base, interruptQueueHeadOffset, head, 64)
		emitStoreMemFromReg(e, base, interruptQueueTailOffset, tail, 64)
	case "set_flag", "set_flag_and_wake":
		emitStoreImm8ToMem(e, base, interruptQueueOverflowOffset, 1)
		emitCallReloc(e, "_wrela_interrupt_queue_wake_overflow_owner")
	case "fatal", "boot_fatal":
		emitCallReloc(e, "_wrela_interrupt_queue_overflow")
	default:
		emitStoreImm8ToMem(e, base, interruptQueueOverflowOffset, 1)
	}
}

func emitStoreImm8ToMem(e *Emitter, base asm.Reg, disp int64, value byte) {
	if !base.High && base.Low3 == asm.MustLookup("rax").Low3 && disp >= -128 && disp <= 127 {
		e.emit(0xC6, 0x40|byte(base.Low3), byte(int8(disp)), value)
		return
	}
	imm := asm.MustLookup("rcx")
	emitMovImmToReg(e, imm, int64(value))
	emitStoreMemFromReg(e, base, disp, imm, 8)
}
