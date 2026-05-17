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

func interruptQueueByVector(program *ir.Program) map[uint8]ir.InterruptQueueLayout {
	out := map[uint8]ir.InterruptQueueLayout{}
	if program == nil {
		return out
	}
	for _, queue := range program.InterruptQueues {
		if queue.Vector == 0 {
			continue
		}
		out[queue.Vector] = queue
	}
	return out
}

func emitInterruptQueuePush(e *Emitter, q ir.InterruptQueueLayout) {
	emitInterruptQueuePushPayload(e, q, "", 0)
}

func emitInterruptQueuePushPayload(e *Emitter, q ir.InterruptQueueLayout, payloadSymbol string, payloadSize uint64) {
	base := asm.MustLookup("r8")
	head := asm.MustLookup("rcx")
	tail := asm.MustLookup("rdx")
	capacity := asm.MustLookup("rsi")
	used := asm.MustLookup("rdi")
	enqueue := e.newLabel("interrupt_queue_enqueue")
	done := e.newLabel("interrupt_queue_done")

	emitMovDataAddressToReg(e, asm.MustLookup("rax"), interruptQueueDataSymbol(q.Label))
	emitRegRegMove(e, base, asm.MustLookup("rax"))
	emitLoadMemToReg(e, head, base, interruptQueueHeadOffset, 64)
	emitLoadMemToReg(e, tail, base, interruptQueueTailOffset, 64)
	emitLoadMemToReg(e, capacity, base, interruptQueueCapacityOffset, 64)
	emitRegRegMove(e, used, tail)
	emitRegRegOp(e, 0x29, used, head)
	emitCmpRegReg(e, used, capacity)
	e.emitJcc(0x82, enqueue)
	emitInterruptQueueFullPolicy(e, q, base, head, tail, enqueue, done)
	e.bindLabel(enqueue)
	emitInterruptQueueStoreEntry(e, q, base, tail, capacity, payloadSymbol, payloadSize)
	emitMfence(e)
	emitAddImm(e, tail, 1)
	emitStoreMemFromReg(e, base, interruptQueueTailOffset, tail, 64)
	emitMfence(e)
	emitWakeSlot(e, q.Owner, e.ctx)
	e.bindLabel(done)
}

func emitInterruptQueueFullPolicy(e *Emitter, q ir.InterruptQueueLayout, base asm.Reg, head asm.Reg, tail asm.Reg, enqueue string, done string) {
	switch q.Overflow {
	case "drop_newest":
		emitStoreImm8ToMem(e, base, interruptQueueOverflowOffset, 1)
		e.emitJmp(done)
	case "drop_newest_and_set_flag":
		emitStoreImm8ToMem(e, base, interruptQueueOverflowOffset, 1)
		emitWakeSlot(e, q.Owner, e.ctx)
		e.emitJmp(done)
	case "drop_oldest", "drop_oldest_and_set_flag":
		emitStoreImm8ToMem(e, base, interruptQueueOverflowOffset, 1)
		emitAddImm(e, head, 1)
		emitStoreMemFromReg(e, base, interruptQueueHeadOffset, head, 64)
		e.emitJmp(enqueue)
	case "set_flag", "set_flag_and_wake":
		emitStoreImm8ToMem(e, base, interruptQueueOverflowOffset, 1)
		emitWakeSlot(e, q.Owner, e.ctx)
		e.emitJmp(done)
	case "fatal", "boot_fatal":
		emitCallReloc(e, "_wrela_interrupt_queue_overflow")
		e.emitJmp(done)
	default:
		emitStoreImm8ToMem(e, base, interruptQueueOverflowOffset, 1)
		e.emitJmp(done)
	}
}

func emitInterruptQueueStoreEntry(e *Emitter, q ir.InterruptQueueLayout, base asm.Reg, tail asm.Reg, capacity asm.Reg, payloadSymbol string, payloadSize uint64) {
	slot := asm.MustLookup("r9")
	originalTail := asm.MustLookup("r11")
	emitRegRegMove(e, originalTail, tail)
	emitRegRegMove(e, asm.MustLookup("rax"), tail)
	emitRegRegOp(e, 0x31, asm.MustLookup("rdx"), asm.MustLookup("rdx"))
	emitUnsignedDivReg(e, capacity)
	emitRegRegMove(e, slot, asm.MustLookup("rdx"))
	emitMovImmToReg(e, asm.MustLookup("r10"), int64(q.PayloadSize))
	emitRegRegIMul(e, slot, asm.MustLookup("r10"))
	emitAddImm(e, slot, interruptQueueHeaderSize)
	emitRegRegOp(e, 0x01, slot, base)
	if payloadSymbol != "" && payloadSize != 0 {
		emitMovDataAddressToReg(e, asm.MustLookup("rax"), payloadSymbol)
		emitRegRegMove(e, asm.MustLookup("r10"), asm.MustLookup("rax"))
		emitCopyBytes(e, slot, 0, asm.MustLookup("r10"), 0, int(minUint64(q.PayloadSize, payloadSize)))
		if payloadSize < q.PayloadSize {
			emitZeroInterruptQueuePayloadRemainder(e, slot, payloadSize, q.PayloadSize-payloadSize)
		}
	} else if q.PayloadSize != 0 {
		emitMovImmToReg(e, asm.MustLookup("r10"), int64(q.Vector))
		emitStoreMemFromReg(e, slot, 0, asm.MustLookup("r10"), 8)
		if q.PayloadSize > 1 {
			emitZeroInterruptQueuePayloadRemainder(e, slot, 1, q.PayloadSize-1)
		}
	}
	emitRegRegMove(e, tail, originalTail)
}

func emitZeroInterruptQueuePayloadRemainder(e *Emitter, slot asm.Reg, offset uint64, bytes uint64) {
	emitMovImmToReg(e, asm.MustLookup("r10"), 0)
	for bytes >= 8 {
		emitStoreMemFromReg(e, slot, int64(offset), asm.MustLookup("r10"), 64)
		offset += 8
		bytes -= 8
	}
	for bytes > 0 {
		emitStoreMemFromReg(e, slot, int64(offset), asm.MustLookup("r10"), 8)
		offset++
		bytes--
	}
}

func minUint64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}

func emitStoreImm8ToMem(e *Emitter, base asm.Reg, disp int64, value byte) {
	if !base.High && base.Low3 == asm.MustLookup("rax").Low3 && disp >= -128 && disp <= 127 {
		e.emit(0xC6, 0x40|byte(base.Low3), byte(int8(disp)), value)
		return
	}
	imm := asm.MustLookup("r10")
	emitMovImmToReg(e, imm, int64(value))
	emitStoreMemFromReg(e, base, disp, imm, 8)
}
