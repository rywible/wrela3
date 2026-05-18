package codegen

import (
	"fmt"

	"github.com/ryanwible/wrela3/compiler/asm"
	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/ir"
)

func compileTimerUnits(program *ir.Program, ctx compileContext) ([]compiledUnit, []diag.Diagnostic) {
	if program == nil || len(program.Timers) == 0 {
		return nil, nil
	}
	routes := map[uint8]ir.TimerRoute{}
	var ds []diag.Diagnostic
	for _, route := range program.Timers {
		if _, exists := routes[route.Vector]; exists {
			ds = append(ds, diag.Diagnostic{
				Phase:   diagnosticPhase,
				Code:    diag.CG0001,
				Message: fmt.Sprintf("duplicate timer vector 0x%02x", route.Vector),
			})
			continue
		}
		routes[route.Vector] = route
	}
	if len(ds) != 0 {
		return nil, ds
	}
	units := make([]compiledUnit, 0, len(routes))
	for vector, route := range routes {
		unit, unitDiags := buildTimerUnit(interruptVectorSymbol(vector), route, ctx)
		if len(unitDiags) != 0 {
			ds = append(ds, unitDiags...)
			continue
		}
		units = append(units, unit)
	}
	return units, ds
}

func buildTimerUnit(symbol string, route ir.TimerRoute, ctx compileContext) (compiledUnit, []diag.Diagnostic) {
	layout, ok := ctx.topicLayouts["timer.periodic"]
	if !ok {
		return compiledUnit{}, []diag.Diagnostic{{
			Phase:   diagnosticPhase,
			Code:    diag.CG0001,
			Message: "missing topic layout: timer.periodic",
		}}
	}
	if symbol == "" {
		return compiledUnit{}, []diag.Diagnostic{{
			Phase:   diagnosticPhase,
			Code:    diag.CG0001,
			Message: fmt.Sprintf("missing timer vector symbol 0x%02x", route.Vector),
		}}
	}
	e := &Emitter{Labels: map[string]int{}, ctx: ctx}
	emitInterruptSave(e)
	emitTimerPayloadPublish(e, layout, route, ctx)
	emitLocalApicEOI(e)
	emitInterruptRestore(e)
	e.emitInstruction(asm.Instruction{Mnemonic: "iretq"})
	e.resolveJumps()
	if len(e.Diags) != 0 {
		return compiledUnit{}, e.Diags
	}
	return compiledUnit{Symbol: symbol, Bytes: e.Code, DataReloc: e.DataReloc}, nil
}

func emitTimerInit(e *Emitter, frame Frame, init *ir.TimerInit) {
	if init == nil {
		return
	}
	if init.Source != "" && init.Source != "local_apic_pit_calibrated" {
		emitCallReloc(e, "_wrela_timer_unsupported_source")
		return
	}
	base := asm.MustLookup("r11")
	emitLoadTimerLocalApicBase(e, frame, init.LocalApic, base)
	emitStoreLocalApicBase(e, base)
	emitLapicWriteWithBaseReg(e, base, lapicTimerDivideConfig, 0x3)
	vector := init.Vector
	if vector == 0 {
		vector = 0x43
	}
	emitLapicWriteWithBaseReg(e, base, lapicLVTTimer, 0x10000|uint32(vector))
	emitLapicWriteWithBaseReg(e, base, lapicTimerInitialCount, 0xFFFFFFFF)
	emitPITCalibrationSample(e)
	emitLoadMemToReg(e, asm.MustLookup("rax"), base, lapicTimerCurrentCount, 32)
	emitMovImmToReg(e, asm.MustLookup("r10"), 0xFFFFFFFF)
	emitRegRegOp(e, 0x29, asm.MustLookup("r10"), asm.MustLookup("rax"))
	emitRegRegMove(e, asm.MustLookup("rax"), asm.MustLookup("r10"))
	emitMovImmToReg(e, asm.MustLookup("r10"), int64(timerPeriodUS(init.PeriodUS)))
	emitRegRegIMul(e, asm.MustLookup("rax"), asm.MustLookup("r10"))
	emitMovImmToReg(e, asm.MustLookup("r10"), int64(timerCalibrationUS))
	emitRegRegOp(e, 0x31, asm.MustLookup("rdx"), asm.MustLookup("rdx"))
	emitUnsignedDivReg(e, asm.MustLookup("r10"))
	nonzero := e.newLabel("timer_initial_count_nonzero")
	emitMovImmToReg(e, asm.MustLookup("r10"), 0)
	emitCmpRegReg(e, asm.MustLookup("rax"), asm.MustLookup("r10"))
	e.emitJcc(0x85, nonzero)
	emitMovImmToReg(e, asm.MustLookup("rax"), 1)
	e.bindLabel(nonzero)
	emitLapicWriteRegWithBaseReg(e, base, lapicTimerInitialCount, asm.MustLookup("rax"))
	emitLapicWriteWithBaseReg(e, base, lapicLVTTimer, 0x20000|uint32(vector))
}

func emitLoadTimerLocalApicBase(e *Emitter, frame Frame, localApic ir.Value, dst asm.Reg) {
	if localApic != nil {
		if info, ok := e.ctx.typeInfo(valueType(localApic)); ok {
			if field, ok := info.Fields["base"]; ok {
				if base, disp, ok := emitValueAddress(e, frame, localApic); ok {
					emitLoadMemToReg(e, dst, base, disp+int64(field.Offset), 64)
					return
				}
			}
		}
	}
	emitLoadLocalApicBase(e, dst)
}

func emitPITCalibrationSample(e *Emitter) {
	e.emit(0xB0, 0x34) // mov al, 0x34: channel 0, lobyte/hibyte, mode 2
	e.emit(0xE6, 0x43) // out 0x43, al
	e.emit(0xB0, 0xFF)
	e.emit(0xE6, 0x40)
	e.emit(0xB0, 0xFF)
	e.emit(0xE6, 0x40)
	e.emit(0xE4, 0x40) // in al, 0x40; keeps the PIT programming observable
}

const timerCalibrationUS = 10000

func timerPeriodUS(periodUS uint64) uint64 {
	if periodUS == 0 {
		return 1
	}
	return periodUS
}

func emitUnsignedDivReg(e *Emitter, divisor asm.Reg) {
	rex := byte(0x48)
	if divisor.High {
		rex |= 0x01
	}
	e.emit(rex, 0xF7, encodeModRM(3, 6, divisor.Low3))
}

func compileTimerUnsupportedSourceTrapUnit() compiledUnit {
	e := &Emitter{Labels: map[string]int{}}
	e.emitInstruction(asm.Instruction{Mnemonic: "cli"})
	emitHltLoop(e)
	return compiledUnit{Symbol: "_wrela_timer_unsupported_source", Bytes: e.Code}
}

func emitTimerPayloadPublish(e *Emitter, layout topicDataLayout, route ir.TimerRoute, ctx compileContext) {
	base := asm.MustLookup("r12")
	seq := asm.MustLookup("r10")
	slot := asm.MustLookup("r11")
	mask := asm.MustLookup("rdx")

	emitMovDataAddressToReg(e, asm.MustLookup("rax"), topicDataSymbol("timer.periodic"))
	emitRegRegMove(e, base, asm.MustLookup("rax"))
	emitLoadMemToReg(e, seq, base, int64(layout.HeadOffset), 64)
	emitRegRegMove(e, slot, seq)
	emitMovImmToReg(e, mask, int64(layout.Depth-1))
	emitRegRegOp(e, 0x21, slot, mask)
	emitMovImmToReg(e, mask, int64(layout.SlotSize))
	emitRegRegIMul(e, slot, mask)
	emitAddImm(e, slot, int64(layout.SlotsOffset))
	emitRegRegOp(e, 0x01, slot, base)
	emitAddImm(e, seq, 1)
	emitStoreMemFromReg(e, slot, 8, seq, 64)
	emitRegRegMove(e, asm.MustLookup("rax"), seq)
	emitMovImmToReg(e, asm.MustLookup("rcx"), int64(timerPeriodUS(route.PeriodUS)))
	emitRegRegIMul(e, asm.MustLookup("rax"), asm.MustLookup("rcx"))
	emitStoreMemFromReg(e, slot, 16, asm.MustLookup("rax"), 64)
	emitMovImmToReg(e, asm.MustLookup("rcx"), 1)
	emitStoreMemFromReg(e, slot, 24, asm.MustLookup("rcx"), 32)
	emitMfence(e)
	emitStoreMemFromReg(e, slot, 0, seq, 64)
	emitMfence(e)
	emitStoreMemFromReg(e, base, int64(layout.HeadOffset), seq, 64)
	emitTouchSubscriberWaitlinesAndWakeSkippingVcpu(e, layout, base, seq, route.SubscriberSlots, ctx, 0)
}
