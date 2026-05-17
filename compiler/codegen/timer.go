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
	emitTimerTickTopicPublish(e, layout, route, ctx)
	emitLocalApicEOI(e)
	emitInterruptRestore(e)
	e.emitInstruction(asm.Instruction{Mnemonic: "iretq"})
	e.resolveJumps()
	if len(e.Diags) != 0 {
		return compiledUnit{}, e.Diags
	}
	return compiledUnit{Symbol: symbol, Bytes: e.Code, DataReloc: e.DataReloc}, nil
}

func emitTimerTickTopicPublish(e *Emitter, layout topicDataLayout, route ir.TimerRoute, ctx compileContext) {
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
	emitShiftImm(e, 0x04, slot, 6)
	emitAddImm(e, slot, int64(layout.SlotsOffset))
	emitRegRegOp(e, 0x01, slot, base)
	emitAddImm(e, seq, 1)
	emitStoreMemFromReg(e, slot, 0, seq, 64)
	emitStoreMemFromReg(e, slot, 8, seq, 64)
	emitMovImmToReg(e, asm.MustLookup("rcx"), int64(route.PeriodUS))
	emitStoreMemFromReg(e, slot, 16, asm.MustLookup("rcx"), 64)
	emitMovImmToReg(e, asm.MustLookup("rcx"), 1)
	emitStoreMemFromReg(e, slot, 24, asm.MustLookup("rcx"), 32)
	emitMfence(e)
	emitStoreMemFromReg(e, base, int64(layout.HeadOffset), seq, 64)
	emitTouchSubscriberWaitlinesAndWakeSkippingVcpu(e, layout, base, seq, route.SubscriberSlots, ctx, 0)
}
