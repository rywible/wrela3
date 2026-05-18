package codegen

import (
	"encoding/binary"
	"fmt"
	"sort"

	"github.com/ryanwible/wrela3/compiler/asm"
	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/ir"
	"github.com/ryanwible/wrela3/compiler/layout"
)

const diagnosticPhase = "cg"

type Frame struct {
	Slots            map[ir.Value]int
	ObjectSlots      map[ir.Value]int
	FrameSaves       map[*ir.FrameBegin]frameSave
	Size             int
	ReturnType       ir.Type
	ContinuationSlot int
	RecordReturnSlot int
	HasRecordReturn  bool
	PreserveStackRet bool
}

type frameSave struct {
	Parent          ir.Value
	SavedOffsetSlot int64
}

type compileContext struct {
	types           map[string]ir.TypeInfo
	topicLayouts    map[string]topicDataLayout
	interruptQueues map[uint8]ir.InterruptQueueLayout
	SlotVcpu        map[string]int
	VcpuPlans       map[string]ir.VcpuStartPlan
	APICMode        string
}

type internalReloc struct {
	Offset uint64
	Symbol string
}

type dataReloc struct {
	Offset uint64
	Symbol string
}

type compiledUnit struct {
	Symbol    string
	Bytes     []byte
	CallReloc []internalReloc
	DataReloc []dataReloc
}

type jumpFixup struct {
	Pos    int
	Target string
	NextIP int
}

type Emitter struct {
	Code      []byte
	Labels    map[string]int
	CallReloc []internalReloc
	DataReloc []dataReloc
	Jumps     []jumpFixup
	Diags     []diag.Diagnostic
	ctx       compileContext
}

const runtimeImageBase = 0x100000

func Compile(program *ir.Program) (*Image, []diag.Diagnostic) {
	if program == nil {
		return nil, []diag.Diagnostic{{
			Phase:   diagnosticPhase,
			Code:    diag.CG0001,
			Message: "nil program",
		}}
	}

	topicLayouts, ds := topicLayoutMap(program)
	if len(ds) != 0 {
		return nil, ds
	}
	ctx := compileContext{
		types:           program.Types,
		topicLayouts:    topicLayouts,
		interruptQueues: interruptQueueByVector(program),
		SlotVcpu:        slotVcpuMap(program),
		VcpuPlans:       vcpuPlanMap(program),
		APICMode:        program.APICMode,
	}
	if ctx.types == nil {
		ctx.types = map[string]ir.TypeInfo{}
	}
	if ds := validateAPStartupContract(); len(ds) != 0 {
		return nil, ds
	}

	units := make([]compiledUnit, 0, len(program.Functions)+len(program.AsmMethods)+len(program.InterruptBindings)+1)
	for _, fn := range program.Functions {
		unit, ds := compileFunction(fn, ctx)
		if len(ds) != 0 {
			return nil, ds
		}
		units = append(units, unit)
	}
	for _, method := range program.AsmMethods {
		unit, ds := compileAsmMethodUnit(method)
		if len(ds) != 0 {
			return nil, ds
		}
		units = append(units, unit)
	}
	if !hasAPTrampolineInstallMethod(program) {
		unit, ds := compileAPTrampolineInstallUnit()
		if len(ds) != 0 {
			return nil, ds
		}
		units = append(units, unit)
	}
	interruptUnits, ds := compileInterruptDispatchUnits(program, ctx)
	if len(ds) != 0 {
		return nil, ds
	}
	units = append(units, interruptUnits...)
	timerUnits, ds := compileTimerUnits(program, ctx)
	if len(ds) != 0 {
		return nil, ds
	}
	units = append(units, timerUnits...)
	if program.Entry.Symbol != "" {
		unit, ds := compileEntryAdapterUnit(program.Entry, ctx)
		if len(ds) != 0 {
			return nil, ds
		}
		units = append(units, unit)
	}
	units = append(units, compileMemoryTrapUnit())
	units = append(units, compileAPStartupTimeoutTrapUnit())
	units = append(units, compileInterruptQueueOverflowTrapUnit())
	units = append(units, compileTimerUnsupportedSourceTrapUnit())

	sections := []Section{{Name: ".text", Data: nil, Characteristics: 0x60000020}}
	symbols := map[string]uint64{}
	unitOffsets := map[string]uint64{}
	for _, unit := range units {
		if unit.Symbol == "" {
			continue
		}
		unitOffsets[unit.Symbol] = uint64(len(sections[0].Data))
		symbols[unit.Symbol] = uint64(0x1000 + len(sections[0].Data))
		sections[0].Data = append(sections[0].Data, unit.Bytes...)
	}
	sections[0].RVA = 0x1000

	var dataSections []builtDataSection
	if len(program.Data) > 0 {
		section, offsets := buildDataSection(".rdata", program.Data, 0x40000040)
		dataSections = append(dataSections, builtDataSection{Section: section, Offsets: offsets})
	}
	if len(program.WritableData) > 0 || len(program.InterruptQueues) > 0 || len(program.Topics) > 0 || len(program.InterruptContexts) > 0 || len(program.InterruptBindings) > 0 || len(program.VcpuStarts) > 0 || program.Entry.Symbol != "" || len(units) > 0 {
		section, offsets, ds := buildData(program)
		if len(ds) != 0 {
			return nil, ds
		}
		dataSections = append(dataSections, builtDataSection{Section: section, Offsets: offsets})
	}
	if len(dataSections) > 0 {
		alignedTextSize := alignUpLen(uint64(len(sections[0].Data)), 0x1000)
		sections[0].Data = append(sections[0].Data, make([]byte, alignedTextSize-uint64(len(sections[0].Data)))...)
		nextRVA := sections[0].RVA + alignedTextSize
		for _, built := range dataSections {
			section := built.Section
			section.RVA = nextRVA
			for symbol, offset := range built.Offsets {
				symbols[symbol] = section.RVA + offset
			}
			sections = append(sections, section)
			nextRVA += alignUpLen(uint64(len(section.Data)), 0x1000)
		}
	}

	var relocs []Reloc
	for _, unit := range units {
		unitStart := unitOffsets[unit.Symbol]
		for _, rel := range unit.CallReloc {
			target, ok := symbols[rel.Symbol]
			if !ok {
				return nil, []diag.Diagnostic{{
					Phase:   diagnosticPhase,
					Code:    diag.CG0001,
					Message: "unknown call symbol: " + rel.Symbol,
				}}
			}
			callPos := sections[0].RVA + unitStart + rel.Offset
			rel32 := int64(target) - int64(callPos+4)
			binary.LittleEndian.PutUint32(sections[0].Data[unitStart+rel.Offset:unitStart+rel.Offset+4], uint32(int32(rel32)))
		}
		for _, rel := range unit.DataReloc {
			target, ok := symbols[rel.Symbol]
			if !ok {
				return nil, []diag.Diagnostic{{
					Phase:   diagnosticPhase,
					Code:    diag.CG0001,
					Message: "unknown data symbol: " + rel.Symbol,
				}}
			}
			location := unitStart + rel.Offset
			binary.LittleEndian.PutUint64(sections[0].Data[location:location+8], uint64(runtimeImageBase)+target)
			relocs = append(relocs, Reloc{Kind: RelocKindDIR64, Offset: rel.Offset, Symbol: unit.Symbol})
		}
	}

	return &Image{
		EntrySymbol:       program.Entry.Symbol,
		Sections:          sections,
		Symbols:           symbols,
		Relocs:            relocs,
		InterruptBindings: compileInterruptBindings(program.InterruptBindings),
	}, nil
}

func topicLayoutMap(program *ir.Program) (map[string]topicDataLayout, []diag.Diagnostic) {
	layouts, ds := orderedTopicDataLayouts(program)
	if len(ds) != 0 {
		return nil, ds
	}
	out := make(map[string]topicDataLayout, len(layouts))
	for _, layout := range layouts {
		out[layout.Label] = layout
	}
	return out, nil
}

func slotVcpuMap(program *ir.Program) map[string]int {
	out := map[string]int{}
	for _, start := range program.VcpuStarts {
		if start.SlotLabel != "" {
			out[start.SlotLabel] = start.VcpuID
		}
	}
	return out
}

func vcpuPlanMap(program *ir.Program) map[string]ir.VcpuStartPlan {
	out := map[string]ir.VcpuStartPlan{}
	for _, start := range program.VcpuStarts {
		if start.SlotLabel != "" {
			out[start.SlotLabel] = start
		}
	}
	return out
}

func compileInterruptBindings(bindings []ir.InterruptBinding) []InterruptBinding {
	out := make([]InterruptBinding, 0, len(bindings))
	for _, binding := range bindings {
		out = append(out, InterruptBinding{
			EventSymbol:           binding.EventSymbol,
			HandlerSymbol:         binding.HandlerSymbol,
			EventFunctionSymbol:   binding.EventFunctionSymbol,
			HandlerFunctionSymbol: binding.HandlerFunctionSymbol,
			PathFieldOffset:       binding.PathFieldOffset,
			ContextSymbol:         binding.ContextSymbol,
			EventStorageSymbol:    binding.EventStorageSymbol,
			EventStorageSize:      binding.EventStorageSize,
			Vector:                binding.Vector,
			TopicLabel:            binding.TopicLabel,
			TopicKind:             binding.TopicKind,
			PublisherOwnerKind:    binding.PublisherOwnerKind,
			PublisherOwnerLabel:   binding.PublisherOwnerLabel,
			SubscriberSlots:       append([]string{}, binding.SubscriberSlots...),
		})
	}
	return out
}

func compileInterruptDispatchUnits(program *ir.Program, ctx compileContext) ([]compiledUnit, []diag.Diagnostic) {
	bindings := map[uint8]ir.InterruptBinding{}
	var ds []diag.Diagnostic
	for _, binding := range program.InterruptBindings {
		if _, exists := bindings[binding.Vector]; exists {
			ds = append(ds, diag.Diagnostic{
				Phase:   diagnosticPhase,
				Code:    diag.CG0001,
				Message: fmt.Sprintf("duplicate interrupt binding vector 0x%02x", binding.Vector),
			})
			continue
		}
		bindings[binding.Vector] = binding
	}

	known := []uint8{0x40, 0x41, 0x42, 0x43, 0xF0}
	units := make([]compiledUnit, 0, len(known))
	for _, vector := range known {
		symbol := interruptVectorSymbol(vector)
		if vector == 0x43 && len(program.Timers) > 0 {
			continue
		}
		if vector == 0xF0 {
			units = append(units, buildInterruptWakeUnit(symbol, ctx))
			continue
		}
		if binding, ok := bindings[vector]; ok {
			unit, unitDiags := buildInterruptDispatchUnit(symbol, binding, ctx)
			if len(unitDiags) != 0 {
				ds = append(ds, unitDiags...)
				continue
			}
			units = append(units, unit)
			continue
		}
		if queue, ok := ctx.interruptQueues[vector]; ok {
			unit, unitDiags := buildInterruptQueueOnlyUnit(symbol, queue, ctx)
			if len(unitDiags) != 0 {
				ds = append(ds, unitDiags...)
				continue
			}
			units = append(units, unit)
			continue
		}
		units = append(units, buildInterruptTrapUnit(symbol))
	}

	return units, ds
}

func interruptVectorSymbol(vector uint8) string {
	switch vector {
	case 0x40:
		return "_wrela_interrupt_vector40_serial"
	case 0x41:
		return "_wrela_interrupt_vector41_edu_msi"
	case 0x42:
		return "_wrela_interrupt_vector42_ivshmem_msix"
	case 0x43:
		return "_wrela_interrupt_vector43_timer"
	case 0xF0:
		return "_wrela_interrupt_vectorf0_wake"
	default:
		return ""
	}
}

func buildInterruptDispatchUnit(symbol string, binding ir.InterruptBinding, ctx compileContext) (compiledUnit, []diag.Diagnostic) {
	if binding.TopicLabel == "" {
		return compiledUnit{}, []diag.Diagnostic{{
			Phase:   diagnosticPhase,
			Code:    diag.CG0001,
			Message: "interrupt binding missing topic route",
		}}
	}
	e := &Emitter{Labels: map[string]int{}, ctx: ctx}
	emitInterruptSave(e)
	emitLoadInterruptPathReceiver(e, binding)
	emitMovDataAddressToReg(e, asm.MustLookup("rax"), binding.EventStorageSymbol)
	emitRegRegMove(e, asm.MustLookup("r10"), asm.MustLookup("rax"))
	emitCallReloc(e, binding.EventFunctionSymbol)
	emitInterruptTopicPublish(e, binding, ctx)
	if queue, ok := ctx.interruptQueues[binding.Vector]; ok {
		emitInterruptQueuePushPayload(e, queue, binding.EventStorageSymbol, uint64(binding.EventStorageSize))
	}
	emitLocalApicEOI(e)
	emitInterruptRestore(e)
	e.emitInstruction(asm.Instruction{Mnemonic: "iretq"})
	e.resolveJumps()
	if len(e.Diags) != 0 {
		return compiledUnit{}, e.Diags
	}
	return compiledUnit{Symbol: symbol, Bytes: e.Code, CallReloc: e.CallReloc, DataReloc: e.DataReloc}, nil
}

func buildInterruptQueueOnlyUnit(symbol string, queue ir.InterruptQueueLayout, ctx compileContext) (compiledUnit, []diag.Diagnostic) {
	if symbol == "" {
		return compiledUnit{}, []diag.Diagnostic{{
			Phase:   diagnosticPhase,
			Code:    diag.CG0001,
			Message: fmt.Sprintf("missing interrupt vector symbol 0x%02x", queue.Vector),
		}}
	}
	e := &Emitter{Labels: map[string]int{}, ctx: ctx}
	emitInterruptSave(e)
	emitInterruptQueuePush(e, queue)
	emitLocalApicEOI(e)
	emitInterruptRestore(e)
	e.emitInstruction(asm.Instruction{Mnemonic: "iretq"})
	e.resolveJumps()
	if len(e.Diags) != 0 {
		return compiledUnit{}, e.Diags
	}
	return compiledUnit{Symbol: symbol, Bytes: e.Code, CallReloc: e.CallReloc, DataReloc: e.DataReloc}, nil
}

func emitInterruptSave(e *Emitter) {
	for _, reg := range []string{"rax", "rcx", "rdx", "rbx", "rbp", "rsi", "rdi", "r8", "r9", "r10", "r11", "r12", "r13", "r14", "r15"} {
		e.emitInstruction(asm.Instruction{Mnemonic: "push", Operands: []asm.Operand{asm.RegOperand{Reg: asm.MustLookup(reg)}}})
	}
}

func emitInterruptRestore(e *Emitter) {
	for _, reg := range []string{"r15", "r14", "r13", "r12", "r11", "r10", "r9", "r8", "rdi", "rsi", "rbp", "rbx", "rdx", "rcx", "rax"} {
		e.emitInstruction(asm.Instruction{Mnemonic: "pop", Operands: []asm.Operand{asm.RegOperand{Reg: asm.MustLookup(reg)}}})
	}
}

func emitLoadInterruptPathReceiver(e *Emitter, binding ir.InterruptBinding) {
	if binding.ContextSymbol == "" {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "interrupt binding missing context symbol"})
		return
	}
	emitMovDataAddressToReg(e, asm.MustLookup("rax"), binding.ContextSymbol)
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("rdi")},
		asm.MemOperand{Base: asm.MustLookup("rax"), Disp: int64(binding.PathFieldOffset), Width: 64},
	}})
}

func emitInterruptTopicPublish(e *Emitter, binding ir.InterruptBinding, ctx compileContext) {
	layout, ok := ctx.topicLayouts[binding.TopicLabel]
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "missing topic layout: " + binding.TopicLabel})
		return
	}
	if binding.EventStorageSize > cacheLineSize-8 {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "interrupt event payload exceeds topic slot capacity"})
		return
	}

	base := asm.MustLookup("r12")
	seq := asm.MustLookup("r10")
	slot := asm.MustLookup("r11")
	mask := asm.MustLookup("rdx")
	src := asm.MustLookup("rsi")
	skipPublish := e.newLabel("interrupt_topic_publish_skip")

	emitMovDataAddressToReg(e, asm.MustLookup("rax"), topicDataSymbol(binding.TopicLabel))
	emitRegRegMove(e, base, asm.MustLookup("rax"))
	emitMovDataAddressToReg(e, asm.MustLookup("rax"), binding.EventStorageSymbol)
	emitRegRegMove(e, src, asm.MustLookup("rax"))
	if binding.TopicKind == "serial_rx" {
		eventInfo, ok := ctx.typeInfo(ir.Type{Name: "SerialPathInterrupt", Module: "machine.x86_64.serial", Kind: ir.TypeKindData})
		if !ok {
			e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "missing serial interrupt event type"})
			return
		}
		hasByte, ok := eventInfo.Fields["has_byte"]
		if !ok {
			e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "missing serial interrupt has_byte field"})
			return
		}
		emitMovImmToReg(e, mask, 0)
		emitLoadMemToReg(e, mask, src, int64(hasByte.Offset), 8)
		emitMovImmToReg(e, asm.MustLookup("rdi"), 0)
		emitCmpRegReg(e, mask, asm.MustLookup("rdi"))
		e.emitJcc(0x84, skipPublish)
	}
	emitLoadMemToReg(e, seq, base, int64(layout.HeadOffset), 64)
	emitRegRegMove(e, slot, seq)
	emitMovImmToReg(e, mask, int64(layout.Depth-1))
	emitRegRegOp(e, 0x21, slot, mask)
	emitScaleTopicSlot(e, slot, mask, layout)
	emitAddImm(e, slot, int64(layout.SlotsOffset))
	emitRegRegOp(e, 0x01, slot, base)
	emitAddImm(e, seq, 1)
	emitStoreMemFromReg(e, slot, 0, seq, 64)
	emitCopyBytes(e, slot, 8, src, 0, binding.EventStorageSize)
	emitMfence(e)
	emitStoreMemFromReg(e, base, int64(layout.HeadOffset), seq, 64)
	emitTouchSubscriberWaitlinesAndWakeSkippingVcpu(e, layout, base, seq, binding.SubscriberSlots, ctx, 0)
	e.bindLabel(skipPublish)
}

func emitLocalApicEOI(e *Emitter) {
	if usesX2APIC(e.ctx.APICMode) {
		emitMovImmToReg(e, asm.MustLookup("r10"), 0)
		emitX2APICWriteMSR(e, x2apicEOIMSR, asm.MustLookup("r10"))
		return
	}
	if usesRuntimeX2APICFallback(e.ctx.APICMode) {
		// Fallback mode keeps interrupt completion on the xAPIC contract used by
		// the AP trampoline; x2APIC EOI is reserved for explicitly enabled modes.
		emitLocalApicMMIOEOI(e)
		return
	}
	emitLocalApicMMIOEOI(e)
}

func emitLocalApicMMIOEOI(e *Emitter) {
	emitLoadLocalApicBase(e, asm.MustLookup("r11"))
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("eax")},
		asm.ImmOperand{Value: 0},
	}})
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.MemOperand{Base: asm.MustLookup("r11"), Disp: lapicEOI, Width: 32},
		asm.RegOperand{Reg: asm.MustLookup("eax")},
	}})
}

func buildInterruptTrapUnit(symbol string) compiledUnit {
	return compiledUnit{
		Symbol: symbol,
		Bytes: []byte{
			0xFA,
			0xF4,
			0xEB, 0xFD,
		},
	}
}

func buildInterruptWakeUnit(symbol string, ctx compileContext) compiledUnit {
	e := &Emitter{Labels: map[string]int{}, ctx: ctx}
	emitInterruptSave(e)
	emitLocalApicEOI(e)
	emitInterruptRestore(e)
	e.emitInstruction(asm.Instruction{Mnemonic: "iretq"})
	return compiledUnit{Symbol: symbol, Bytes: e.Code, DataReloc: e.DataReloc}
}

func emitCallReloc(e *Emitter, symbol string) {
	e.Code = append(e.Code, 0xE8, 0, 0, 0, 0)
	e.CallReloc = append(e.CallReloc, internalReloc{Offset: uint64(len(e.Code) - 4), Symbol: symbol})
}

func compileMemoryTrapUnit() compiledUnit {
	return compiledUnit{
		Symbol: "_wrela_memory_oom",
		Bytes: []byte{
			0xFA,
			0xF4,
			0xEB, 0xFD,
		},
	}
}

func compileAPStartupTimeoutTrapUnit() compiledUnit {
	e := &Emitter{Labels: map[string]int{}}
	e.emitInstruction(asm.Instruction{Mnemonic: "cli"})
	emitHltLoop(e)
	return compiledUnit{Symbol: "_wrela_ap_startup_timeout", Bytes: e.Code}
}

func compileInterruptQueueOverflowTrapUnit() compiledUnit {
	e := &Emitter{Labels: map[string]int{}}
	e.emitInstruction(asm.Instruction{Mnemonic: "cli"})
	emitHltLoop(e)
	return compiledUnit{Symbol: "_wrela_interrupt_queue_overflow", Bytes: e.Code}
}

type builtDataSection struct {
	Section Section
	Offsets map[string]uint64
}

func buildData(program *ir.Program) (Section, map[string]uint64, []diag.Diagnostic) {
	writable := append([]ir.DataObject{}, program.WritableData...)
	queueObjects, ds := interruptQueueDataObjects(program)
	if len(ds) != 0 {
		return Section{}, nil, ds
	}
	writable = append(writable, queueObjects...)
	topicObjects, ds := topicDataObjects(program)
	if len(ds) != 0 {
		return Section{}, nil, ds
	}
	writable = append(writable, topicObjects...)
	writable = append(writable, localApicBaseDataObject())
	writable = append(writable, monitorMwaitWaitlineDataObjects(program)...)
	writable = append(writable, vcpuStartupData(program)...)
	writable = append(writable, interruptRuntimeData(program)...)
	section, offsets := buildDataSection(".data", writable, 0xC0000040)
	return section, offsets, nil
}

func monitorMwaitWaitlineSymbol(slot string) string {
	return "_wrela_monitor_mwait_waitline_" + sanitizeSymbol(slot)
}

func monitorMwaitWaitlineDataObjects(program *ir.Program) []ir.DataObject {
	if program == nil {
		return nil
	}
	labels := map[string]bool{}
	for _, start := range program.VcpuStarts {
		if start.SlotLabel != "" {
			labels[start.SlotLabel] = true
		}
	}
	for _, fn := range program.Functions {
		for _, block := range fn.Blocks {
			for _, op := range block.Ops {
				switch wait := op.(type) {
				case ir.TopicWait:
					if wait.UseMonitorMwait && wait.SlotLabel != "" {
						labels[wait.SlotLabel] = true
					}
				case *ir.TopicWait:
					if wait != nil && wait.UseMonitorMwait && wait.SlotLabel != "" {
						labels[wait.SlotLabel] = true
					}
				}
			}
		}
	}
	if len(labels) == 0 {
		return nil
	}
	ordered := make([]string, 0, len(labels))
	for label := range labels {
		ordered = append(ordered, label)
	}
	sort.Strings(ordered)
	out := make([]ir.DataObject, 0, len(ordered))
	for _, label := range ordered {
		out = append(out, ir.DataObject{
			Symbol: monitorMwaitWaitlineSymbol(label),
			Bytes:  make([]byte, 8),
			Align:  8,
		})
	}
	return out
}

func interruptRuntimeData(program *ir.Program) []ir.DataObject {
	out := make([]ir.DataObject, 0, len(program.InterruptContexts)+len(program.InterruptBindings))
	seenContexts := map[string]bool{}
	for _, context := range program.InterruptContexts {
		out = append(out, ir.DataObject{
			Symbol: context.Symbol,
			Bytes:  make([]byte, context.Size),
		})
		seenContexts[context.Symbol] = true
	}
	for _, binding := range program.InterruptBindings {
		if binding.ContextSymbol == "" || seenContexts[binding.ContextSymbol] {
			continue
		}
		size := binding.PathFieldOffset + 8
		if size < 8 {
			size = 8
		}
		out = append(out, ir.DataObject{
			Symbol: binding.ContextSymbol,
			Bytes:  make([]byte, size),
		})
		seenContexts[binding.ContextSymbol] = true
	}
	for _, binding := range program.InterruptBindings {
		out = append(out, ir.DataObject{
			Symbol: binding.EventStorageSymbol,
			Bytes:  make([]byte, binding.EventStorageSize),
		})
	}
	return out
}

func buildDataSection(name string, objects []ir.DataObject, characteristics uint32) (Section, map[string]uint64) {
	out := []byte{}
	offsets := appendDataObjects(&out, objects)
	return Section{Name: name, Data: out, Characteristics: characteristics}, offsets
}

func appendDataObjects(out *[]byte, objects []ir.DataObject) map[string]uint64 {
	offsets := map[string]uint64{}
	for _, obj := range objects {
		if obj.Align > 1 {
			aligned := layout.AlignUp(len(*out), int(obj.Align))
			*out = append(*out, make([]byte, aligned-len(*out))...)
		}
		offsets[obj.Symbol] = uint64(len(*out))
		*out = append(*out, obj.Bytes...)
	}
	return offsets
}

func compileFunction(fn ir.Function, ctx compileContext) (compiledUnit, []diag.Diagnostic) {
	frame := buildFrame(fn, ctx)
	e := &Emitter{Labels: map[string]int{}, ctx: ctx}

	emitPrologue(e, fn.Params, frame)
	hasReturn := false
	for _, block := range fn.Blocks {
		if block.Label != "" {
			e.bindLabel(block.Label)
		}
		for _, op := range block.Ops {
			switch v := op.(type) {
			case *ir.ConstInt:
				emitConst(e, v, frame)
			case *ir.Local:
				// Local slots are allocated by the frame builder.
				continue
			case *ir.StringLiteral:
				emitStringLiteral(e, v, frame)
			case *ir.Construct:
				emitConstruct(e, v, frame)
			case *ir.EnumConstruct:
				emitEnumConstruct(e, v, frame)
			case *ir.EnumVariantTest:
				emitEnumVariantTest(e, v, frame)
			case *ir.EnumPayloadExtract:
				emitEnumPayloadExtract(e, v, frame)
			case *ir.FrameBegin:
				emitFrameBegin(e, v, frame)
			case *ir.ArenaReserve:
				emitArenaReserve(e, v, frame)
			case *ir.ArenaReserveArray:
				emitArenaReserveArray(e, v, frame)
			case *ir.ArenaPlace:
				emitArenaPlace(e, v, frame)
			case *ir.SlotWrite:
				emitSlotWrite(e, v, frame)
			case *ir.SliceGet:
				emitSliceGet(e, v, frame)
			case *ir.SliceSet:
				emitSliceSet(e, v, frame)
			case *ir.FrameEnd:
				emitFrameEnd(e, v, frame)
			case *ir.Copy:
				emitCopy(e, v, frame)
			case *ir.Binary:
				emitBinary(e, v, frame)
			case *ir.Call:
				emitCall(e, v, frame)
			case *ir.TimerInit:
				emitTimerInit(e, frame, v)
			case *ir.Return:
				hasReturn = true
				emitReturn(e, v, frame)
			case *ir.If:
				emitIf(e, v, frame)
			case *ir.While:
				emitWhile(e, v, frame)
			case *ir.ForBytes:
				emitForBytes(e, v, frame)
			case *ir.Branch:
				emitBranch(e, v, frame)
			case *ir.FieldLoad:
				emitFieldLoad(e, v, frame)
			case *ir.FieldStore:
				emitFieldStore(e, v, frame)
			case *ir.InterruptContextStore:
				emitInterruptContextStore(e, frame, v)
			case *ir.TopicPublish:
				emitTopicPublish(e, frame, v)
			case ir.TopicPublish:
				vv := v
				emitTopicPublish(e, frame, &vv)
			case *ir.ReliableTopicTryPublish:
				emitReliableTopicTryPublish(e, frame, v)
			case ir.ReliableTopicTryPublish:
				vv := v
				emitReliableTopicTryPublish(e, frame, &vv)
			case *ir.ReliableTopicWaitForAdvance:
				emitReliableTopicWaitForAdvance(e, v)
			case ir.ReliableTopicWaitForAdvance:
				vv := v
				emitReliableTopicWaitForAdvance(e, &vv)
			case *ir.TopicTryNext:
				emitTopicTryNext(e, frame, v)
			case ir.TopicTryNext:
				vv := v
				emitTopicTryNext(e, frame, &vv)
			case *ir.TopicArmWait:
				emitTopicArmWait(e, frame, v)
			case ir.TopicArmWait:
				vv := v
				emitTopicArmWait(e, frame, &vv)
			case *ir.TopicIsWaitArmed:
				emitTopicIsWaitArmed(e, frame, v)
			case ir.TopicIsWaitArmed:
				vv := v
				emitTopicIsWaitArmed(e, frame, &vv)
			case *ir.TopicWaitIfArmed:
				emitTopicWaitIfArmed(e, frame, v)
			case ir.TopicWaitIfArmed:
				vv := v
				emitTopicWaitIfArmed(e, frame, &vv)
			case *ir.TopicWait:
				emitTopicWait(e, *v)
			case ir.TopicWait:
				emitTopicWait(e, v)
			case *ir.VcpuStart:
				emitVcpuStart(e, v, frame, ctx)
			case ir.VcpuStart:
				vv := v
				emitVcpuStart(e, &vv, frame, ctx)
			case *ir.VcpuEnter:
				emitVcpuEnter(e, v, frame, ctx)
				hasReturn = true
			case ir.VcpuEnter:
				vv := v
				emitVcpuEnter(e, &vv, frame, ctx)
				hasReturn = true
			case ir.TimerInit:
				vv := v
				emitTimerInit(e, frame, &vv)
			}
		}
	}
	if !hasReturn {
		emitEpilogue(e)
	}

	e.resolveJumps()
	if len(e.Diags) != 0 {
		return compiledUnit{}, e.Diags
	}
	return compiledUnit{Symbol: fn.Symbol, Bytes: e.Code, CallReloc: e.CallReloc, DataReloc: e.DataReloc}, nil
}

func compileAsmMethodUnit(method ir.AsmMethod) (compiledUnit, []diag.Diagnostic) {
	instructions, diags := lowerAsmMethodInstructions(method)
	if len(diags) != 0 {
		for i := range diags {
			if method.Symbol != "" {
				diags[i].Message = method.Symbol + ": " + diags[i].Message
			}
		}
		return compiledUnit{}, diags
	}
	code, externalRelocs, asDiags := asm.EncodeWithExternalCalls(instructions)
	if len(asDiags) != 0 {
		diags = convertAsmDiagnostics(asDiags)
		for i := range diags {
			if method.Symbol != "" {
				diags[i].Message = method.Symbol + ": " + diags[i].Message
			}
		}
		return compiledUnit{}, diags
	}
	callRelocs := make([]internalReloc, 0, len(externalRelocs))
	for _, rel := range externalRelocs {
		callRelocs = append(callRelocs, internalReloc{
			Offset: rel.Offset,
			Symbol: rel.Symbol,
		})
	}
	return compiledUnit{Symbol: method.Symbol, Bytes: code, CallReloc: callRelocs}, nil
}

func compileEntryAdapterUnit(entry ir.EntryAdapter, ctx compileContext) (compiledUnit, []diag.Diagnostic) {
	e := &Emitter{Labels: map[string]int{}}
	emitEntryAdapter(e, entry, ctx)
	e.resolveJumps()
	if len(e.Diags) != 0 {
		return compiledUnit{}, e.Diags
	}
	return compiledUnit{Symbol: entry.Symbol, Bytes: e.Code, CallReloc: e.CallReloc, DataReloc: e.DataReloc}, nil
}

func buildFrame(fn ir.Function, ctx compileContext) Frame {
	offset := 0
	slots := map[ir.Value]int{}
	objectSlots := map[ir.Value]int{}
	frameSaves := map[*ir.FrameBegin]frameSave{}
	hasRecordReturn := false
	continuationSlot := 0
	recordReturnSlot := 0
	returnType := functionReturnType(fn)
	if fn.PreserveStackReturn {
		offset += 8
		continuationSlot = -offset
	}
	if ctx.shouldPassRecordReturn(returnType) {
		offset += 8
		recordReturnSlot = -offset
		hasRecordReturn = true
	}
	for _, p := range fn.Params {
		size := valueSize(ctx, p)
		offset += size
		slots[p] = -offset
	}
	for _, v := range fn.ValuesInDeterministicOrder() {
		if _, ok := slots[v]; ok {
			continue
		}
		if frameBegin, ok := v.(*ir.FrameBegin); ok {
			offset += 8
			frameSaves[frameBegin] = frameSave{
				Parent:          frameBegin.Parent,
				SavedOffsetSlot: int64(-offset),
			}
		}
		if needsObjectSlot(ctx, v) {
			size := objectStorageSize(ctx, v)
			offset += size
			objectSlots[v] = -offset
		}
		size := valueSize(ctx, v)
		offset += size
		slots[v] = -offset
	}
	return Frame{
		Slots:            slots,
		ObjectSlots:      objectSlots,
		FrameSaves:       frameSaves,
		Size:             layout.AlignUp(offset, 16),
		ReturnType:       returnType,
		ContinuationSlot: continuationSlot,
		RecordReturnSlot: recordReturnSlot,
		HasRecordReturn:  hasRecordReturn,
		PreserveStackRet: fn.PreserveStackReturn,
	}
}

func frameSlot(frame Frame, value ir.Value) (int, bool) {
	if slot, ok := frame.Slots[value]; ok {
		return slot, true
	}
	switch v := value.(type) {
	case *ir.ReliableTopicTryPublish:
		slot, ok := frame.Slots[*v]
		return slot, ok
	case *ir.TopicTryNext:
		slot, ok := frame.Slots[*v]
		return slot, ok
	case *ir.TopicIsWaitArmed:
		slot, ok := frame.Slots[*v]
		return slot, ok
	case *ir.VcpuStart:
		slot, ok := frame.Slots[*v]
		return slot, ok
	default:
		return 0, false
	}
}

func frameObjectSlot(frame Frame, value ir.Value) (int, bool) {
	if slot, ok := frame.ObjectSlots[value]; ok {
		return slot, true
	}
	switch v := value.(type) {
	case *ir.ReliableTopicTryPublish:
		slot, ok := frame.ObjectSlots[*v]
		return slot, ok
	case *ir.TopicTryNext:
		slot, ok := frame.ObjectSlots[*v]
		return slot, ok
	case *ir.VcpuStart:
		slot, ok := frame.ObjectSlots[*v]
		return slot, ok
	default:
		return 0, false
	}
}

func needsObjectSlot(ctx compileContext, value ir.Value) bool {
	switch v := value.(type) {
	case *ir.Construct, *ir.StringLiteral:
		return true
	case *ir.EnumConstruct:
		return true
	case *ir.EnumPayloadExtract:
		return ctx.isHandleTypeKind(valueType(v).Kind) || ctx.isHandleType(valueType(v).Name)
	case *ir.SliceGet:
		return ctx.isDataType(v.Type)
	case *ir.FrameBegin, *ir.ArenaReserve, *ir.ArenaReserveArray, *ir.ArenaPlace:
		return true
	case *ir.Call:
		return ctx.shouldPassRecordReturn(v.Type)
	case *ir.FieldLoad:
		return ctx.isDataType(v.Type)
	case *ir.ReliableTopicTryPublish:
		return ctx.isDataType(v.Type)
	case ir.ReliableTopicTryPublish:
		return ctx.isDataType(v.Type)
	case *ir.TopicTryNext:
		return ctx.isDataType(v.Type)
	case ir.TopicTryNext:
		return ctx.isDataType(v.Type)
	case *ir.VcpuStart:
		return ctx.isDataType(v.Type)
	case ir.VcpuStart:
		return ctx.isDataType(v.Type)
	default:
		return false
	}
}

func objectStorageSize(ctx compileContext, value ir.Value) int {
	typ := valueType(value)
	if info, ok := ctx.typeInfo(typ); ok {
		if info.StorageSize > 0 {
			return info.StorageSize
		}
		if info.Size > 0 {
			return info.Size
		}
	}
	size := ctx.storageSize(typ.Name)
	if size <= 0 {
		return 8
	}
	return size
}

func functionReturnType(fn ir.Function) ir.Type {
	if fn.Return.Name != "" {
		return fn.Return
	}
	if typ, ok := firstReturnType(fn.Blocks); ok {
		return typ
	}
	return ir.Type{Name: "void"}
}

func firstReturnType(blocks []ir.Block) (ir.Type, bool) {
	for _, block := range blocks {
		if typ, ok := firstReturnTypeInOps(block.Ops); ok {
			return typ, true
		}
	}
	return ir.Type{}, false
}

func firstReturnTypeInOps(ops []ir.Operation) (ir.Type, bool) {
	for _, op := range ops {
		switch v := op.(type) {
		case *ir.Return:
			if v.Value != nil {
				return valueType(v.Value), true
			}
		case *ir.If:
			if typ, ok := firstReturnTypeInOps(v.Then); ok {
				return typ, true
			}
			if typ, ok := firstReturnTypeInOps(v.Else); ok {
				return typ, true
			}
		case *ir.While:
			if typ, ok := firstReturnTypeInOps(v.Body); ok {
				return typ, true
			}
		case *ir.ForBytes:
			if typ, ok := firstReturnTypeInOps(v.Body); ok {
				return typ, true
			}
		}
	}
	return ir.Type{}, false
}

func valueSize(ctx compileContext, value ir.Value) int {
	switch v := value.(type) {
	case *ir.Param:
		return ctx.representationSize(v.Type.Name)
	case *ir.Local:
		return ctx.representationSize(v.Type.Name)
	case *ir.ConstInt:
		return ctx.representationSize(v.Type.Name)
	case *ir.Binary:
		return ctx.representationSize(v.Type.Name)
	case *ir.Call:
		return ctx.representationSize(v.Type.Name)
	case *ir.FieldLoad:
		return ctx.representationSize(v.Type.Name)
	case *ir.Construct:
		return ctx.representationSize(v.Type.Name)
	case *ir.EnumConstruct:
		return ctx.representationSize(v.Type.Name)
	case *ir.EnumVariantTest:
		return 1
	case *ir.EnumPayloadExtract:
		return ctx.representationSize(v.Type.Name)
	case *ir.StringLiteral:
		return ctx.representationSize(v.Type.Name)
	case *ir.FrameBegin:
		return ctx.representationSize(v.Type.Name)
	case *ir.ArenaReserve:
		return ctx.representationSize(v.Type.Name)
	case *ir.ArenaReserveArray:
		return ctx.representationSize(v.Type.Name)
	case *ir.ArenaPlace:
		return ctx.representationSize(v.Type.Name)
	case *ir.SliceGet:
		return ctx.representationSize(v.Type.Name)
	case *ir.ReliableTopicTryPublish:
		return ctx.representationSize(v.Type.Name)
	case *ir.TopicTryNext:
		return ctx.representationSize(v.Type.Name)
	case ir.TopicTryNext:
		return ctx.representationSize(v.Type.Name)
	case *ir.TopicIsWaitArmed:
		return ctx.representationSize(v.Type.Name)
	case ir.TopicIsWaitArmed:
		return ctx.representationSize(v.Type.Name)
	case *ir.VcpuStart:
		return ctx.representationSize(v.Type.Name)
	default:
		return 8
	}
}

func (ctx compileContext) representationSize(name string) int {
	if ctx.isHandleType(name) {
		return 8
	}
	return ctx.objectSize(name)
}

func (ctx compileContext) objectSize(name string) int {
	if info, ok := ctx.types[name]; ok && info.Size > 0 {
		return info.Size
	}
	switch name {
	case "U8", "I8", "Bool":
		return 1
	case "U16", "I16":
		return 2
	case "U32", "I32":
		return 4
	case "StringLiteral", "Bytes", "MutableBytes", "DelegatedBytes", "DelegatedMutableBytes":
		return 16
	case "VcpuStartStatus":
		return 16
	default:
		s, _, err := layout.SizeAlign(name)
		if err != nil {
			return 8
		}
		return s
	}
}

func (ctx compileContext) storageSize(name string) int {
	if info, ok := ctx.types[name]; ok {
		if info.StorageSize > 0 {
			return info.StorageSize
		}
		if info.Size > 0 {
			return info.Size
		}
	}
	return ctx.objectSize(name)
}

func (ctx compileContext) storageSizeForType(typ ir.Type) int {
	if info, ok := ctx.typeInfo(typ); ok {
		if info.StorageSize > 0 {
			return info.StorageSize
		}
		if info.Size > 0 {
			return info.Size
		}
	}
	return ctx.storageSize(typ.Name)
}

func (ctx compileContext) isDataType(typ ir.Type) bool {
	if typ.Kind == ir.TypeKindData {
		return true
	}
	info, ok := ctx.typeInfo(typ)
	return ok && info.Kind == ir.TypeKindData
}

func (ctx compileContext) isHandleTypeKind(kind ir.TypeKind) bool {
	return isHandleTypeKind(kind)
}

func (ctx compileContext) isHandleType(name string) bool {
	if name == "StringLiteral" {
		return true
	}
	info, ok := ctx.types[name]
	if ok {
		return isHandleTypeKind(info.Kind)
	}
	switch name {
	case "Bytes", "MutableBytes", "DelegatedBytes", "DelegatedMutableBytes", "UefiHandle", "UefiStatus", "UefiMemoryMap", "UefiMemoryMapResult":
		return true
	case "", "Bool", "U8", "I8", "U16", "I16", "U32", "I32", "U64", "I64", "PhysicalAddress", "VirtualAddress", "never", "void":
		return false
	default:
		return true
	}
}

func isHandleTypeKind(kind ir.TypeKind) bool {
	switch kind {
	case ir.TypeKindData, ir.TypeKindClass, ir.TypeKindDriver, ir.TypeKindDriverPath, ir.TypeKindExecutor, ir.TypeKindEnum:
		return true
	default:
		return false
	}
}

func (ctx compileContext) shouldPassRecordReturn(typ ir.Type) bool {
	if typ.Name == "" || typ.Name == "never" {
		return false
	}
	if info, ok := ctx.typeInfo(typ); ok {
		return info.Kind == ir.TypeKindData || info.Kind == ir.TypeKindEnum
	}
	return false
}

func (ctx compileContext) typeInfo(typ ir.Type) (ir.TypeInfo, bool) {
	if typ.Module != "" {
		if info, ok := ctx.types[typ.Module+"."+typ.Name]; ok {
			return info, true
		}
	}
	info, ok := ctx.types[typ.Name]
	return info, ok
}

func valueWidthBits(ctx compileContext, value ir.Value) int {
	size := valueSize(ctx, value)
	switch size {
	case 1:
		return 8
	case 2:
		return 16
	case 4:
		return 32
	default:
		return 64
	}
}

func valueWidthBitsFromType(name string) int {
	switch name {
	case "U8", "I8", "Bool":
		return 8
	case "U16", "I16":
		return 16
	case "U32", "I32":
		return 32
	default:
		return 64
	}
}

func (e *Emitter) emit(b ...byte) {
	e.Code = append(e.Code, b...)
}

func (e *Emitter) emitUint32(v uint32) {
	e.emit(byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}

func (e *Emitter) emitInt32(v int32) {
	e.emit(byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}

func (e *Emitter) newLabel(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, len(e.Labels))
}

func (e *Emitter) bindLabel(label string) {
	e.Labels[label] = len(e.Code)
}

func (e *Emitter) emitJmp(label string) {
	start := len(e.Code)
	e.emit(0xE9, 0, 0, 0, 0)
	e.Jumps = append(e.Jumps, jumpFixup{Pos: start + 1, Target: label, NextIP: start + 5})
}

func (e *Emitter) emitJcc(cond byte, label string) {
	start := len(e.Code)
	e.emit(0x0F, cond, 0, 0, 0, 0)
	e.Jumps = append(e.Jumps, jumpFixup{Pos: start + 2, Target: label, NextIP: start + 6})
}

func (e *Emitter) emitInt32At(pos int, v int32) {
	e.Code[pos] = byte(v)
	e.Code[pos+1] = byte(v >> 8)
	e.Code[pos+2] = byte(v >> 16)
	e.Code[pos+3] = byte(v >> 24)
}

func (e *Emitter) resolveJumps() {
	for _, f := range e.Jumps {
		target, ok := e.Labels[f.Target]
		if !ok {
			e.Diags = append(e.Diags, diag.Diagnostic{
				Phase:   diagnosticPhase,
				Code:    diag.CG0001,
				Message: "unknown label: " + f.Target,
			})
			continue
		}
		rel := target - f.NextIP
		if rel < -0x80000000 || rel > 0x7fffffff {
			e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "branch out of range"})
			continue
		}
		e.emitInt32At(f.Pos, int32(rel))
	}
}

func (e *Emitter) emitInstruction(instruction asm.Instruction) {
	bytes, diags := asm.Encode([]asm.Instruction{instruction})
	if len(diags) != 0 {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diags[0].Code, Message: diags[0].Message})
		return
	}
	e.Code = append(e.Code, bytes...)
}

func emitHltWait(e *Emitter) {
	e.emit(0xF4)
}

func emitStiHltWait(e *Emitter) {
	e.emit(0xFB)
	emitHltWait(e)
}

func emitTopicWait(e *Emitter, wait ir.TopicWait) {
	if wait.UseMonitorMwait {
		emitMonitorMwaitWait(e, wait)
		return
	}
	emitFallbackWait(e, wait.Fallback)
}

func emitFallbackWait(e *Emitter, fallback string) {
	if fallback == "sti_hlt" {
		emitStiHltWait(e)
		return
	}
	emitHltWait(e)
}

func emitMonitorMwait(e *Emitter, addressReg asm.Reg) {
	emitMonitorInstruction(e, addressReg)
	emitMwaitInstruction(e)
}

func emitMonitorInstruction(e *Emitter, addressReg asm.Reg) {
	if addressReg.Name != "rax" {
		e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
			asm.RegOperand{Reg: asm.MustLookup("rax")},
			asm.RegOperand{Reg: addressReg},
		}})
	}
	e.emit(0x31, 0xC9)
	e.emit(0x31, 0xD2)
	e.emit(0x0F, 0x01, 0xC8)
}

func emitMwaitInstruction(e *Emitter) {
	e.emit(0x31, 0xC9)
	e.emit(0x31, 0xC0)
	e.emit(0x0F, 0x01, 0xC9)
}

func emitMonitorMwaitWait(e *Emitter, wait ir.TopicWait) {
	fallback := e.newLabel("monitor_mwait_fallback")
	done := e.newLabel("monitor_mwait_done")

	e.emit(0x53)
	emitMovImmToReg(e, asm.MustLookup("rax"), 1)
	e.emit(0x0F, 0xA2)
	emitMovImmToReg(e, asm.MustLookup("r10"), 8)
	emitRegRegOp(e, 0x21, asm.MustLookup("rcx"), asm.MustLookup("r10"))
	emitMovImmToReg(e, asm.MustLookup("r10"), 0)
	emitCmpRegReg(e, asm.MustLookup("rcx"), asm.MustLookup("r10"))
	e.emit(0x5B)
	e.emitJcc(0x84, fallback)

	emitMovDataAddressToReg(e, asm.MustLookup("rax"), monitorMwaitWaitlineSymbol(wait.SlotLabel))
	emitLoadMemToReg(e, asm.MustLookup("r10"), asm.MustLookup("rax"), 0, 64)
	emitMonitorInstruction(e, asm.MustLookup("rax"))
	emitCmpRegMem(e, asm.MustLookup("r10"), asm.MustLookup("rax"), 0, 64)
	e.emitJcc(0x85, done)
	emitMwaitInstruction(e)
	e.emitJmp(done)

	e.bindLabel(fallback)
	emitFallbackWait(e, wait.Fallback)
	e.bindLabel(done)
}

func compileWaitFallbackUnitForTest() compiledUnit {
	e := &Emitter{Labels: map[string]int{}}
	emitStiHltWait(e)
	return compiledUnit{Symbol: "wait_fallback_test", Bytes: e.Code}
}

func compileMonitorMwaitUnitForTest() compiledUnit {
	e := &Emitter{Labels: map[string]int{}}
	emitMonitorMwait(e, asm.MustLookup("rax"))
	return compiledUnit{Symbol: "monitor_mwait_test", Bytes: e.Code}
}

func compileTopicWaitUnitForTest(wait ir.TopicWait) compiledUnit {
	e := &Emitter{Labels: map[string]int{}}
	emitTopicWait(e, wait)
	e.resolveJumps()
	return compiledUnit{Symbol: "topic_wait_test", Bytes: e.Code, DataReloc: e.DataReloc}
}

func emitPrologue(e *Emitter, params []ir.Value, frame Frame) {
	e.emit(0x55)
	e.emit(0x48, 0x89, 0xE5)
	if frame.Size != 0 {
		e.emit(0x48, 0x81, 0xEC)
		e.emitInt32(int32(frame.Size))
	}
	if frame.RecordReturnSlot != 0 {
		emitStoreSlotFromReg(e, asm.MustLookup("r10"), frame.RecordReturnSlot, 64)
	}
	if frame.ContinuationSlot != 0 {
		emitLoadMemToReg(e, scratchRegs[0], asm.MustLookup("rbp"), 8, 64)
		emitStoreSlotFromReg(e, scratchRegs[0], frame.ContinuationSlot, 64)
	}
	for i, p := range params {
		if i >= len(argRegs) {
			break
		}
		slot, ok := frame.Slots[p]
		if !ok {
			continue
		}
		emitStoreSlotFromReg(e, argRegs[i], slot, valueWidthBits(e.ctx, p))
	}
}

func emitEpilogue(e *Emitter) {
	e.emit(0x48, 0x89, 0xEC)
	e.emit(0x5D)
	e.emit(0xC3)
}

func emitStackPreservingEpilogue(e *Emitter, frame Frame) {
	if frame.ContinuationSlot != 0 {
		emitLoadSlotToReg(e, scratchRegs[1], frame.ContinuationSlot, 64)
		emitLoadMemToReg(e, asm.MustLookup("rbp"), asm.MustLookup("rbp"), 0, 64)
		e.emit(0x41, 0x52)
	}
	e.emit(0xC3)
}

func emitConst(e *Emitter, c *ir.ConstInt, frame Frame) {
	slot, ok := frame.Slots[c]
	if !ok {
		return
	}
	emitMovImmToReg(e, scratchRegs[0], int64(c.Value))
	emitStoreSlotFromReg(e, scratchRegs[0], slot, valueWidthBits(e.ctx, c))
}

func emitBinary(e *Emitter, op *ir.Binary, frame Frame) {
	emitLoadValue(e, frame, op.Left, scratchRegs[0])
	switch op.Op {
	case "or":
		emitLoadValue(e, frame, op.Right, scratchRegs[1])
		emitRegRegOp(e, 0x09, scratchRegs[0], scratchRegs[1])
	case "add":
		emitLoadValue(e, frame, op.Right, scratchRegs[1])
		emitRegRegOp(e, 0x01, scratchRegs[0], scratchRegs[1])
	case "sub":
		emitLoadValue(e, frame, op.Right, scratchRegs[1])
		emitRegRegOp(e, 0x29, scratchRegs[0], scratchRegs[1])
	case "and":
		emitLoadValue(e, frame, op.Right, scratchRegs[1])
		emitRegRegOp(e, 0x21, scratchRegs[0], scratchRegs[1])
	case "mul", "*":
		emitLoadValue(e, frame, op.Right, scratchRegs[1])
		emitRegRegIMul(e, scratchRegs[0], scratchRegs[1])
	case "/":
		if op.Type.Name != "U64" {
			e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "unsupported binary op: /"})
			return
		}
		emitLoadValue(e, frame, op.Right, scratchRegs[1])
		emitRegRegOp(e, 0x31, asm.MustLookup("rdx"), asm.MustLookup("rdx"))
		emitUnsignedDivReg(e, scratchRegs[1])
	case "shl", "shr":
		constValue, ok := op.Right.(*ir.ConstInt)
		if !ok || constValue.Value > 63 {
			e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "unsupported binary op: " + op.Op})
			return
		}
		opcode := byte(0x05)
		if op.Op == "shl" {
			opcode = 0x04
		}
		emitShiftImm(e, opcode, scratchRegs[0], byte(constValue.Value))
	case "eq", "ne", "lt", "le", "gt", "ge":
		emitLoadValue(e, frame, op.Right, scratchRegs[1])
		emitCmpRegReg(e, scratchRegs[0], scratchRegs[1])
		emitMovImmToReg(e, scratchRegs[0], 0)
		emitSetccAl(e, setccOpcode(op.Op))
	default:
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "unsupported binary op: " + op.Op})
		return
	}

	slot, ok := frame.Slots[op]
	if !ok {
		return
	}
	emitStoreSlotFromReg(e, scratchRegs[0], slot, valueWidthBits(e.ctx, op))
}

func emitShiftImm(e *Emitter, opcode byte, reg asm.Reg, amount byte) {
	rex := byte(0x48)
	if reg.High {
		rex |= 0x01
	}
	e.emit(rex, 0xC1, 0xC0|(opcode<<3)|byte(reg.Low3), amount)
}

func emitCall(e *Emitter, call *ir.Call, frame Frame) {
	values := make([]ir.Value, 0, 1+len(call.Args))
	if call.Receiver != nil {
		values = append(values, call.Receiver)
	}
	values = append(values, call.Args...)

	if len(call.Args) > len(argRegs)-1 {
		e.Diags = append(e.Diags, diag.Diagnostic{
			Phase:   diagnosticPhase,
			Code:    diag.SEM0013,
			Message: "v0 ABI supports at most five explicit parameters",
		})
		return
	}
	if e.ctx.shouldPassRecordReturn(call.Type) {
		slot, ok := frame.ObjectSlots[call]
		if !ok {
			e.Diags = append(e.Diags, diag.Diagnostic{
				Phase:   diagnosticPhase,
				Code:    diag.CG0001,
				Message: "missing slot for data-return call",
			})
			return
		}
		emitSlotFromBase(e, asm.MustLookup("r10"), asm.MustLookup("rbp"), slot)
	}

	for i, value := range values {
		emitLoadValue(e, frame, value, scratchRegs[0])
		width := valueWidthBits(e.ctx, value)
		emitRegRegMove(e, registerForWidth(argRegs[i], width), registerForWidth(scratchRegs[0], width))
	}

	e.emit(0xE8, 0, 0, 0, 0)
	rel := uint64(len(e.Code) - 4)
	e.CallReloc = append(e.CallReloc, internalReloc{Offset: rel, Symbol: call.Symbol})

	if slot, ok := frame.Slots[call]; ok && call.Type.Name != "void" && call.Type.Name != "never" {
		emitStoreSlotFromReg(e, scratchRegs[0], slot, valueWidthBits(e.ctx, call))
	}
}

func isRegisterReturnedType(typeName string) bool {
	switch typeName {
	case "Bool", "U8", "U16", "U32", "U64", "I64", "PhysicalAddress", "VirtualAddress", "StringLiteral", "String", "Data":
		return true
	}
	return false
}

func emitLoadValue(e *Emitter, frame Frame, value ir.Value, dst asm.Reg) {
	if value == nil {
		return
	}
	slot, ok := frame.Slots[value]
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "missing slot"})
		return
	}
	switch c := value.(type) {
	case *ir.ConstInt:
		emitMovImmToReg(e, dst, int64(c.Value))
	default:
		width := valueWidthBits(e.ctx, value)
		if width < 64 {
			emitMovImmToReg(e, dst, 0)
		}
		emitLoadSlotToReg(e, dst, slot, width)
	}
}

func emitReturn(e *Emitter, r *ir.Return, frame Frame) {
	if r.Value != nil {
		if frame.HasRecordReturn {
			emitLoadSlotToReg(e, scratchRegs[1], frame.RecordReturnSlot, 64)
			emitCopyValueToAddressAsType(e, frame, r.Value, scratchRegs[1], frame.ReturnType.Name)
			emitRegRegMove(e, scratchRegs[0], scratchRegs[1])
		} else {
			emitLoadValue(e, frame, r.Value, scratchRegs[0])
		}
	}
	if frame.PreserveStackRet {
		emitStackPreservingEpilogue(e, frame)
		return
	}
	emitEpilogue(e)
}

func emitBranch(e *Emitter, branch *ir.Branch, frame Frame) {
	emitConditionJump(e, branch.Condition, branch.False, frame)
	e.emitJmp(branch.True)
}

func emitIf(e *Emitter, cond *ir.If, frame Frame) {
	done := e.newLabel("if_done")
	elseLabel := e.newLabel("if_else")
	emitOperations(e, cond.ConditionOps, frame)
	emitMaterializedConditionJump(e, cond.Condition, elseLabel, frame)
	emitOperations(e, cond.Then, frame)
	e.emitJmp(done)
	e.bindLabel(elseLabel)
	emitOperations(e, cond.Else, frame)
	e.bindLabel(done)
}

func emitWhile(e *Emitter, wh *ir.While, frame Frame) {
	start := e.newLabel("while_start")
	done := e.newLabel("while_done")
	e.bindLabel(start)
	emitOperations(e, wh.ConditionOps, frame)
	emitMaterializedConditionJump(e, wh.Condition, done, frame)
	emitOperations(e, wh.Body, frame)
	e.emitJmp(start)
	e.bindLabel(done)
}

func emitForBytes(e *Emitter, loop *ir.ForBytes, frame Frame) {
	if loop.Index == nil {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "forbytes index is nil"})
		return
	}
	indexSlot, ok := frame.Slots[loop.Index]
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "missing forbytes index slot"})
		return
	}
	if _, ok := frame.Slots[loop.Iterable]; !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "missing forbytes iterable slot"})
		return
	}
	byteSlot, ok := frame.Slots[loop.ByteValue]
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "missing forbytes byte slot"})
		return
	}

	emitOperations(e, loop.IterableOps, frame)
	emitMovImmToReg(e, scratchRegs[0], 0)
	emitStoreSlotFromReg(e, scratchRegs[0], indexSlot, valueWidthBits(e.ctx, loop.Index))

	start := e.newLabel("for_bytes_start")
	done := e.newLabel("for_bytes_done")
	e.bindLabel(start)

	iterBase, iterDisp, ok := emitValueAddress(e, frame, loop.Iterable)
	if !ok {
		return
	}
	emitLoadMemToReg(e, scratchRegs[1], iterBase, iterDisp+8, 64) // length
	emitLoadSlotToReg(e, scratchRegs[0], indexSlot, 64)
	emitCmpRegReg(e, scratchRegs[0], scratchRegs[1])
	e.emitJcc(0x8D, done)

	emitLoadMemToReg(e, asm.MustLookup("rdi"), iterBase, iterDisp, 64) // bytes.address
	emitLoadSlotToReg(e, asm.MustLookup("rcx"), indexSlot, 64)
	e.emit(0x48, 0x0F, 0xB6, 0x04, 0x0F)
	emitStoreSlotFromReg(e, scratchRegs[0], byteSlot, valueWidthBits(e.ctx, loop.ByteValue))

	emitOperations(e, loop.Body, frame)
	emitLoadSlotToReg(e, asm.MustLookup("rcx"), indexSlot, 64)
	e.emit(0x48, 0x83, 0xC1, 0x01)
	emitStoreSlotFromReg(e, asm.MustLookup("rcx"), indexSlot, 64)
	e.emitJmp(start)
	e.bindLabel(done)
}

func emitOperations(e *Emitter, ops []ir.Operation, frame Frame) {
	for _, op := range ops {
		switch v := op.(type) {
		case *ir.ConstInt:
			emitConst(e, v, frame)
		case *ir.Local:
			// Local slots are allocated by the frame builder.
			continue
		case *ir.StringLiteral:
			emitStringLiteral(e, v, frame)
		case *ir.Construct:
			emitConstruct(e, v, frame)
		case *ir.EnumConstruct:
			emitEnumConstruct(e, v, frame)
		case *ir.EnumVariantTest:
			emitEnumVariantTest(e, v, frame)
		case *ir.EnumPayloadExtract:
			emitEnumPayloadExtract(e, v, frame)
		case *ir.FrameBegin:
			emitFrameBegin(e, v, frame)
		case *ir.ArenaReserve:
			emitArenaReserve(e, v, frame)
		case *ir.ArenaReserveArray:
			emitArenaReserveArray(e, v, frame)
		case *ir.ArenaPlace:
			emitArenaPlace(e, v, frame)
		case *ir.SlotWrite:
			emitSlotWrite(e, v, frame)
		case *ir.SliceGet:
			emitSliceGet(e, v, frame)
		case *ir.SliceSet:
			emitSliceSet(e, v, frame)
		case *ir.FrameEnd:
			emitFrameEnd(e, v, frame)
		case *ir.Copy:
			emitCopy(e, v, frame)
		case *ir.Binary:
			emitBinary(e, v, frame)
		case *ir.Call:
			emitCall(e, v, frame)
		case *ir.TimerInit:
			emitTimerInit(e, frame, v)
		case *ir.Return:
			emitReturn(e, v, frame)
		case *ir.If:
			emitIf(e, v, frame)
		case *ir.While:
			emitWhile(e, v, frame)
		case *ir.ForBytes:
			emitForBytes(e, v, frame)
		case *ir.Branch:
			emitBranch(e, v, frame)
		case *ir.FieldLoad:
			emitFieldLoad(e, v, frame)
		case *ir.FieldStore:
			emitFieldStore(e, v, frame)
		case *ir.InterruptContextStore:
			emitInterruptContextStore(e, frame, v)
		case *ir.TopicPublish:
			emitTopicPublish(e, frame, v)
		case ir.TopicPublish:
			vv := v
			emitTopicPublish(e, frame, &vv)
		case *ir.ReliableTopicTryPublish:
			emitReliableTopicTryPublish(e, frame, v)
		case ir.ReliableTopicTryPublish:
			vv := v
			emitReliableTopicTryPublish(e, frame, &vv)
		case *ir.ReliableTopicWaitForAdvance:
			emitReliableTopicWaitForAdvance(e, v)
		case ir.ReliableTopicWaitForAdvance:
			vv := v
			emitReliableTopicWaitForAdvance(e, &vv)
		case *ir.TopicTryNext:
			emitTopicTryNext(e, frame, v)
		case ir.TopicTryNext:
			vv := v
			emitTopicTryNext(e, frame, &vv)
		case *ir.TopicArmWait:
			emitTopicArmWait(e, frame, v)
		case ir.TopicArmWait:
			vv := v
			emitTopicArmWait(e, frame, &vv)
		case *ir.TopicIsWaitArmed:
			emitTopicIsWaitArmed(e, frame, v)
		case ir.TopicIsWaitArmed:
			vv := v
			emitTopicIsWaitArmed(e, frame, &vv)
		case *ir.TopicWaitIfArmed:
			emitTopicWaitIfArmed(e, frame, v)
		case ir.TopicWaitIfArmed:
			vv := v
			emitTopicWaitIfArmed(e, frame, &vv)
		case *ir.TopicWait:
			emitTopicWait(e, *v)
		case ir.TopicWait:
			emitTopicWait(e, v)
		case *ir.VcpuStart:
			emitVcpuStart(e, v, frame, e.ctx)
		case ir.VcpuStart:
			vv := v
			emitVcpuStart(e, &vv, frame, e.ctx)
		case *ir.VcpuEnter:
			emitVcpuEnter(e, v, frame, e.ctx)
		case ir.VcpuEnter:
			vv := v
			emitVcpuEnter(e, &vv, frame, e.ctx)
		case ir.TimerInit:
			vv := v
			emitTimerInit(e, frame, &vv)
		}
	}
}

func emitConditionJump(e *Emitter, cond ir.Value, falseTarget string, frame Frame) {
	emitConditionJumpMode(e, cond, falseTarget, frame, false)
}

func emitMaterializedConditionJump(e *Emitter, cond ir.Value, falseTarget string, frame Frame) {
	emitConditionJumpMode(e, cond, falseTarget, frame, true)
}

func emitConditionJumpMode(e *Emitter, cond ir.Value, falseTarget string, frame Frame, materialized bool) {
	switch c := cond.(type) {
	case *ir.ConstInt:
		if c.Value == 0 {
			e.emitJmp(falseTarget)
		}
	case *ir.Binary:
		if isComparisonOp(c.Op) && !materialized {
			emitLoadValue(e, frame, c.Left, scratchRegs[0])
			emitLoadValue(e, frame, c.Right, scratchRegs[1])
			emitCmpRegReg(e, scratchRegs[0], scratchRegs[1])
			e.emitJcc(negateConditionOpcode(c.Op), falseTarget)
			return
		}
		emitStoredConditionJump(e, cond, falseTarget, frame)
	default:
		emitStoredConditionJump(e, cond, falseTarget, frame)
	}
}

func emitStoredConditionJump(e *Emitter, cond ir.Value, falseTarget string, frame Frame) {
	emitLoadValue(e, frame, cond, scratchRegs[0])
	emitMovImmToReg(e, scratchRegs[1], 0)
	emitCmpRegReg(e, scratchRegs[0], scratchRegs[1])
	e.emitJcc(0x84, falseTarget)
}

func emitStringLiteral(e *Emitter, op *ir.StringLiteral, frame Frame) {
	slot, ok := frame.Slots[op]
	if !ok {
		return
	}
	objectSlot, ok := frame.ObjectSlots[op]
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "missing string literal object slot"})
		return
	}
	emitMovDataAddressToReg(e, scratchRegs[0], op.DataSymbol)
	emitStoreSlotFromReg(e, scratchRegs[0], objectSlot, 64)
	emitMovImmToReg(e, scratchRegs[0], int64(len(op.Value)))
	emitStoreSlotFromReg(e, scratchRegs[0], objectSlot+8, 64)
	emitSlotFromBase(e, scratchRegs[0], asm.MustLookup("rbp"), objectSlot)
	emitStoreSlotFromReg(e, scratchRegs[0], slot, 64)
}

func emitConstruct(e *Emitter, op *ir.Construct, frame Frame) {
	slot, ok := frame.Slots[op]
	if !ok {
		return
	}
	objectSlot, ok := frame.ObjectSlots[op]
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "missing constructor object slot"})
		return
	}
	size := e.ctx.storageSize(op.Type.Name)
	emitZeroSlotRange(e, objectSlot, size)
	info := e.ctx.types[op.Type.Name]
	for _, field := range op.Fields {
		fieldInfo, ok := info.Fields[field.Name]
		if !ok {
			e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "unknown constructor field: " + field.Name})
			continue
		}
		if e.ctx.isDataType(fieldInfo.Type) && fieldInfo.StorageOffset >= 0 {
			emitStoreNestedDestinationHandle(e, asm.MustLookup("rbp"), int64(objectSlot+fieldInfo.Offset), int64(objectSlot+fieldInfo.StorageOffset))
			emitDeepCopyValueToTypedStorage(e, frame, field.Value, fieldInfo.Type, asm.MustLookup("rbp"), int64(objectSlot+fieldInfo.StorageOffset))
			continue
		}
		emitCopyValueToStackRange(e, frame, field.Value, objectSlot+fieldInfo.Offset, fieldInfo.Size)
	}
	emitSlotFromBase(e, scratchRegs[0], asm.MustLookup("rbp"), objectSlot)
	emitStoreSlotFromReg(e, scratchRegs[0], slot, 64)
}

func emitEnumConstruct(e *Emitter, op *ir.EnumConstruct, frame Frame) {
	slot, ok := frame.Slots[op]
	if !ok {
		return
	}
	objectSlot, ok := frame.ObjectSlots[op]
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "missing enum constructor object slot"})
		return
	}
	info, ok := e.ctx.typeInfo(op.Type)
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "unknown enum constructor type: " + op.Type.Name})
		return
	}
	variant, ok := enumVariantInfo(info, op.Variant)
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "unknown enum variant: " + op.Variant})
		return
	}
	tag, ok := info.Fields["$tag"]
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "enum type missing $tag field: " + op.Type.Name})
		return
	}
	size := info.StorageSize
	if size <= 0 {
		size = info.Size
	}
	if size <= 0 {
		size = e.ctx.storageSizeForType(op.Type)
	}
	emitZeroSlotRange(e, objectSlot, size)
	emitStoreSlotImm(e, objectSlot+fieldStorageOffset(tag), int64(variant.Discriminant), fieldStorageWidthBits(e.ctx, tag))
	for _, field := range op.Fields {
		fieldInfo, ok := info.Fields[op.Variant+"."+field.Name]
		if !ok {
			e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "unknown enum payload field: " + op.Variant + "." + field.Name})
			continue
		}
		emitMovImmToReg(e, scratchRegs[1], int64(fieldStorageOffset(fieldInfo)))
		offset := objectSlot + fieldStorageOffset(fieldInfo)
		size := fieldStorageSize(e.ctx, fieldInfo)
		if e.ctx.isDataType(fieldInfo.Type) {
			emitDeepCopyValueToTypedStorage(e, frame, field.Value, fieldInfo.Type, asm.MustLookup("rbp"), int64(offset))
			continue
		}
		emitCopyValueToStackRange(e, frame, field.Value, offset, size)
	}
	emitSlotFromBase(e, scratchRegs[0], asm.MustLookup("rbp"), objectSlot)
	emitStoreSlotFromReg(e, scratchRegs[0], slot, 64)
}

func emitEnumVariantTest(e *Emitter, op *ir.EnumVariantTest, frame Frame) {
	outSlot, ok := frame.Slots[op]
	if !ok {
		return
	}
	info, ok := e.ctx.typeInfo(op.Type)
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "unknown enum test type: " + op.Type.Name})
		return
	}
	variant, ok := enumVariantInfo(info, op.Variant)
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "unknown enum variant: " + op.Variant})
		return
	}
	tag, ok := info.Fields["$tag"]
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "enum type missing $tag field: " + op.Type.Name})
		return
	}
	base, disp, ok := emitValueAddress(e, frame, op.Value)
	if !ok {
		return
	}
	tagReg := asm.MustLookup("rcx")
	emitLoadMemToReg(e, tagReg, base, disp+int64(fieldStorageOffset(tag)), fieldStorageWidthBits(e.ctx, tag))
	emitMovImmToReg(e, scratchRegs[0], 0)
	emitCmpRegImm(e, tagReg, int64(variant.Discriminant))
	done := e.newLabel("enum_variant_test_done")
	e.emitJcc(0x85, done)
	emitMovImmToReg(e, scratchRegs[0], 1)
	e.bindLabel(done)
	emitStoreSlotFromReg(e, scratchRegs[0], outSlot, 8)
}

func emitEnumPayloadExtract(e *Emitter, op *ir.EnumPayloadExtract, frame Frame) {
	outSlot, ok := frame.Slots[op]
	if !ok {
		return
	}
	enumType := valueType(op.Value)
	info, ok := e.ctx.typeInfo(enumType)
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "unknown enum payload type: " + enumType.Name})
		return
	}
	fieldInfo, ok := info.Fields[op.Variant+"."+op.Field]
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "unknown enum payload field: " + op.Variant + "." + op.Field})
		return
	}
	base, disp, ok := emitValueAddress(e, frame, op.Value)
	if !ok {
		return
	}
	offset := disp + int64(fieldStorageOffset(fieldInfo))
	size := fieldStorageSize(e.ctx, fieldInfo)
	emitMovImmToReg(e, scratchRegs[1], int64(fieldStorageOffset(fieldInfo)))
	if e.ctx.isDataType(fieldInfo.Type) {
		objectSlot, ok := frame.ObjectSlots[op]
		if !ok {
			e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "missing enum payload object slot"})
			return
		}
		emitDeepCopyObjectToAddressAsType(e, fieldInfo.Type, base, offset, asm.MustLookup("rbp"), int64(objectSlot))
		emitSlotFromBase(e, scratchRegs[0], asm.MustLookup("rbp"), objectSlot)
		emitStoreSlotFromReg(e, scratchRegs[0], outSlot, 64)
		return
	}
	emitCopyMemoryToStackRange(e, base, offset, outSlot, size)
}

func emitFrameBegin(e *Emitter, op *ir.FrameBegin, frame Frame) {
	save, ok := frame.FrameSaves[op]
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "missing frame save slot"})
		return
	}
	slot, ok := frame.Slots[op]
	if !ok {
		return
	}
	objectSlot, ok := frame.ObjectSlots[op]
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "missing frame object slot"})
		return
	}

	parent := asm.MustLookup("rdi")
	saved := asm.MustLookup("rax")
	length := asm.MustLookup("r10")
	end := asm.MustLookup("r11")
	limit := asm.MustLookup("rcx")
	base := asm.MustLookup("rsi")

	if !emitValueAddressToReg(e, frame, op.Parent, parent) {
		return
	}
	emitLoadMemToReg(e, saved, parent, 16, 64)
	emitStoreSlotFromReg(e, saved, int(save.SavedOffsetSlot), 64)
	emitLoadValue(e, frame, op.Length, length)
	emitRegRegMove(e, end, saved)
	emitRegRegOp(e, 0x01, end, length)
	emitTrapOnCarry(e)
	emitLoadMemToReg(e, limit, parent, 8, 64)
	emitTrapIfAbove(e, end, limit)

	emitLoadMemToReg(e, base, parent, 0, 64)
	emitRegRegOp(e, 0x01, base, saved)
	emitTrapOnCarry(e)
	emitStoreSlotFromReg(e, base, objectSlot, 64)
	emitStoreSlotFromReg(e, length, objectSlot+8, 64)
	emitMovImmToReg(e, saved, 0)
	emitStoreSlotFromReg(e, saved, objectSlot+16, 64)
	emitStoreMemFromReg(e, parent, 16, end, 64)
	emitSlotFromBase(e, saved, asm.MustLookup("rbp"), objectSlot)
	emitStoreSlotFromReg(e, saved, slot, 64)
}

func emitArenaReserve(e *Emitter, op *ir.ArenaReserve, frame Frame) {
	slot, ok := frame.Slots[op]
	if !ok {
		return
	}
	objectSlot, ok := frame.ObjectSlots[op]
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "missing reserve object slot"})
		return
	}

	arena := asm.MustLookup("rdi")
	length := asm.MustLookup("r10")
	align := asm.MustLookup("r9")

	emitLoadValue(e, frame, op.Align, align)
	emitLoadValue(e, frame, op.Length, length)
	address, ok := emitArenaBump(e, frame, op.Arena, length, align)
	if !ok {
		return
	}
	emitStoreSlotFromReg(e, address, objectSlot, 64)
	emitStoreSlotFromReg(e, length, objectSlot+8, 64)
	emitSlotFromBase(e, arena, asm.MustLookup("rbp"), objectSlot)
	emitStoreSlotFromReg(e, arena, slot, 64)
}

func emitArenaReserveArray(e *Emitter, op *ir.ArenaReserveArray, frame Frame) {
	slot, ok := frame.Slots[op]
	if !ok {
		return
	}
	objectSlot, ok := frame.ObjectSlots[op]
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "missing reserve_array object slot"})
		return
	}
	elementInfo, elementOK := e.ctx.typeInfo(op.Element)
	elementStorageSize := e.ctx.storageSizeForType(op.Element)
	if elementStorageSize <= 0 {
		elementStorageSize = 1
	}
	elementAlign := 8
	if elementOK && elementInfo.Align > 0 {
		elementAlign = elementInfo.Align
	}
	slotsInfo, ok := e.ctx.typeInfo(op.Type)
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "unknown reserve_array slots type: " + op.Type.Name})
		return
	}
	addressOffset := 0
	if field, ok := slotsInfo.Fields["address"]; ok {
		addressOffset = fieldStorageOffset(field)
	}
	capacityOffset := 8
	if field, ok := slotsInfo.Fields["capacity"]; ok {
		capacityOffset = fieldStorageOffset(field)
	}

	countReg := asm.MustLookup("r8")
	byteCountReg := asm.MustLookup("r10")
	alignReg := asm.MustLookup("r9")
	elementReg := asm.MustLookup("rcx")

	emitLoadValue(e, frame, op.Count, countReg)
	emitRegRegMove(e, byteCountReg, countReg)
	emitMovImmToReg(e, elementReg, int64(elementStorageSize))
	emitUnsignedMulInto(e, byteCountReg, elementReg)
	if op.Align != nil {
		emitLoadValue(e, frame, op.Align, alignReg)
	} else {
		emitMovImmToReg(e, alignReg, int64(elementAlign))
	}
	address, ok := emitArenaBump(e, frame, op.Arena, byteCountReg, alignReg)
	if !ok {
		return
	}
	emitStoreSlotFromReg(e, address, objectSlot+addressOffset, 64)
	emitStoreSlotFromReg(e, countReg, objectSlot+capacityOffset, 64)
	emitMovImmToReg(e, elementReg, int64(capacityOffset))
	if info, ok := e.ctx.typeInfo(valueType(op.Arena)); ok {
		if field, ok := info.Fields["next_offset"]; ok {
			emitMovImmToReg(e, elementReg, int64(fieldStorageOffset(field)))
		}
	}
	emitSlotFromBase(e, asm.MustLookup("rdi"), asm.MustLookup("rbp"), objectSlot)
	emitStoreSlotFromReg(e, asm.MustLookup("rdi"), slot, 64)
}

func emitArenaPlace(e *Emitter, op *ir.ArenaPlace, frame Frame) {
	slot, ok := frame.Slots[op]
	if !ok {
		return
	}
	info, ok := e.ctx.typeInfo(op.Type)
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "unknown arena place type: " + op.Type.Name})
		return
	}
	size := info.StorageSize
	if size <= 0 {
		size = info.Size
	}
	if size <= 0 {
		size = e.ctx.storageSize(op.Type.Name)
	}
	align := info.Align
	if align <= 0 {
		align = 8
	}

	lengthReg := asm.MustLookup("r10")
	alignReg := asm.MustLookup("r9")
	emitMovImmToReg(e, lengthReg, int64(size))
	emitMovImmToReg(e, alignReg, int64(align))
	address, ok := emitArenaBump(e, frame, op.Arena, lengthReg, alignReg)
	if !ok {
		return
	}
	emitStoreSlotFromReg(e, address, slot, 64)

	for _, field := range op.Fields {
		fieldInfo, ok := info.Fields[field.Name]
		if !ok {
			e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "unknown arena place field: " + field.Name})
			continue
		}
		if e.ctx.isDataType(fieldInfo.Type) && fieldInfo.StorageOffset >= 0 {
			emitStoreNestedDestinationHandle(e, address, int64(fieldInfo.Offset), int64(fieldInfo.StorageOffset))
			emitDeepCopyValueToTypedStorage(e, frame, field.Value, fieldInfo.Type, address, int64(fieldInfo.StorageOffset))
			continue
		}
		offset := fieldInfo.StorageOffset
		if offset < 0 {
			offset = fieldInfo.Offset
		}
		fieldSize := fieldInfo.StorageSize
		if fieldSize <= 0 {
			fieldSize = fieldInfo.Size
		}
		if fieldSize <= 0 {
			fieldSize = e.ctx.representationSize(fieldInfo.Type.Name)
		}
		emitCopyValueToMemoryRange(e, frame, field.Value, address, int64(offset), fieldSize)
	}
}

func emitSlotWrite(e *Emitter, op *ir.SlotWrite, frame Frame) {
	slotsBase := asm.MustLookup("rsi")
	address := asm.MustLookup("rdi")
	index := asm.MustLookup("r10")
	capacity := asm.MustLookup("r11")
	elementReg := asm.MustLookup("rcx")

	if !emitValueAddressToReg(e, frame, op.Slots, slotsBase) {
		return
	}
	slotsInfo, ok := e.ctx.typeInfo(valueType(op.Slots))
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "unknown slots type: " + valueTypeName(op.Slots)})
		return
	}
	addressField, ok := slotsInfo.Fields["address"]
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "slots type missing address field: " + slotsInfo.Name})
		return
	}
	capacityField, ok := slotsInfo.Fields["capacity"]
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "slots type missing capacity field: " + slotsInfo.Name})
		return
	}
	emitLoadMemToReg(e, address, slotsBase, int64(fieldStorageOffset(addressField)), 64)
	emitLoadMemToReg(e, capacity, slotsBase, int64(fieldStorageOffset(capacityField)), 64)
	emitLoadValue(e, frame, op.Index, index)
	emitIndexBoundsCheck(e, index, capacity)

	valueType := valueType(op.Value)
	elementSize := e.ctx.storageSizeForType(valueType)
	if elementSize <= 0 {
		elementSize = 1
	}
	emitMovImmToReg(e, elementReg, int64(elementSize))
	emitUnsignedMulInto(e, index, elementReg)
	emitRegRegOp(e, 0x01, address, index)
	if e.ctx.isDataType(valueType) {
		emitDeepCopyValueToTypedStorage(e, frame, op.Value, valueType, address, 0)
		return
	}
	emitCopyValueToMemoryRange(e, frame, op.Value, address, 0, elementSize)
}

func emitSliceGet(e *Emitter, op *ir.SliceGet, frame Frame) {
	outSlot, ok := frame.Slots[op]
	if !ok {
		return
	}
	elementSize := e.ctx.storageSizeForType(op.Type)
	if elementSize <= 0 {
		elementSize = 1
	}
	address, ok := emitSliceElementAddress(e, frame, op.Slice, op.Index, elementSize)
	if !ok {
		return
	}
	if e.ctx.isDataType(op.Type) {
		objectSlot, ok := frame.ObjectSlots[op]
		if !ok {
			e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "missing slice get object slot"})
			return
		}
		emitDeepCopyObjectToAddressAsType(e, op.Type, address, 0, asm.MustLookup("rbp"), int64(objectSlot))
		emitSlotFromBase(e, scratchRegs[0], asm.MustLookup("rbp"), objectSlot)
		emitStoreSlotFromReg(e, scratchRegs[0], outSlot, 64)
		return
	}
	emitCopyMemoryToStackRange(e, address, 0, outSlot, elementSize)
}

func emitSliceSet(e *Emitter, op *ir.SliceSet, frame Frame) {
	valueType := valueType(op.Value)
	elementSize := e.ctx.storageSizeForType(valueType)
	if elementSize <= 0 {
		elementSize = 1
	}
	address, ok := emitSliceElementAddress(e, frame, op.Slice, op.Index, elementSize)
	if !ok {
		return
	}
	if e.ctx.isDataType(valueType) {
		emitDeepCopyValueToTypedStorage(e, frame, op.Value, valueType, address, 0)
		return
	}
	emitCopyValueToMemoryRange(e, frame, op.Value, address, 0, elementSize)
}

func emitSliceElementAddress(e *Emitter, frame Frame, slice ir.Value, indexValue ir.Value, elementSize int) (asm.Reg, bool) {
	sliceBase := asm.MustLookup("rsi")
	address := asm.MustLookup("rdi")
	index := asm.MustLookup("r10")
	length := asm.MustLookup("r11")
	elementReg := asm.MustLookup("rcx")

	if !emitValueAddressToReg(e, frame, slice, sliceBase) {
		return asm.Reg{}, false
	}
	sliceInfo, ok := e.ctx.typeInfo(valueType(slice))
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "unknown slice type: " + valueTypeName(slice)})
		return asm.Reg{}, false
	}
	addressField, ok := sliceInfo.Fields["address"]
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "slice type missing address field: " + sliceInfo.Name})
		return asm.Reg{}, false
	}
	lengthField, ok := sliceInfo.Fields["length"]
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "slice type missing length field: " + sliceInfo.Name})
		return asm.Reg{}, false
	}
	emitLoadMemToReg(e, address, sliceBase, int64(fieldStorageOffset(addressField)), 64)
	emitLoadMemToReg(e, length, sliceBase, int64(fieldStorageOffset(lengthField)), 64)
	emitLoadValue(e, frame, indexValue, index)
	emitIndexBoundsCheck(e, index, length)
	emitMovImmToReg(e, elementReg, int64(elementSize))
	emitUnsignedMulInto(e, index, elementReg)
	emitRegRegOp(e, 0x01, address, index)
	return address, true
}

func emitArenaBump(e *Emitter, frame Frame, arenaValue ir.Value, length asm.Reg, align asm.Reg) (asm.Reg, bool) {
	arena := asm.MustLookup("rdi")
	next := asm.MustLookup("rax")
	end := asm.MustLookup("r11")
	limit := asm.MustLookup("rcx")
	base := asm.MustLookup("rsi")

	if !emitValueAddressToReg(e, frame, arenaValue, arena) {
		return asm.Reg{}, false
	}
	emitLoadMemToReg(e, next, arena, 16, 64)
	emitTrapIfZero(e, align)
	emitTrapIfNonPowerOfTwo(e, align)
	emitRegRegMove(e, end, align)
	e.emitInstruction(asm.Instruction{Mnemonic: "sub", Operands: []asm.Operand{
		asm.RegOperand{Reg: end},
		asm.ImmOperand{Value: 1},
	}})
	emitRegRegOp(e, 0x01, next, end)
	emitTrapOnCarry(e)
	emitNegReg(e, align)
	emitRegRegOp(e, 0x21, next, align)
	emitRegRegMove(e, end, next)
	emitRegRegOp(e, 0x01, end, length)
	emitTrapOnCarry(e)
	emitLoadMemToReg(e, limit, arena, 8, 64)
	emitTrapIfAbove(e, end, limit)

	emitLoadMemToReg(e, base, arena, 0, 64)
	emitRegRegOp(e, 0x01, base, next)
	emitTrapOnCarry(e)
	emitStoreMemFromReg(e, arena, 16, end, 64)
	return base, true
}

func emitFrameEnd(e *Emitter, op *ir.FrameEnd, frame Frame) {
	save, ok := frame.FrameSaves[op.Frame]
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "missing frame end save slot"})
		return
	}
	parent := asm.MustLookup("rdi")
	saved := asm.MustLookup("rax")
	if !emitValueAddressToReg(e, frame, save.Parent, parent) {
		return
	}
	emitLoadSlotToReg(e, saved, int(save.SavedOffsetSlot), 64)
	emitStoreMemFromReg(e, parent, 16, saved, 64)
}

func emitTrapIfAbove(e *Emitter, value asm.Reg, limit asm.Reg) {
	ok := e.newLabel("arena_bounds_ok")
	emitCmpRegReg(e, value, limit)
	e.emitJcc(0x86, ok)
	emitCallReloc(e, "_wrela_memory_oom")
	e.bindLabel(ok)
}

func emitTrapOnCarry(e *Emitter) {
	ok := e.newLabel("arena_carry_ok")
	e.emitJcc(0x83, ok)
	emitCallReloc(e, "_wrela_memory_oom")
	e.bindLabel(ok)
}

func emitIndexBoundsCheck(e *Emitter, index asm.Reg, length asm.Reg) {
	trap := e.newLabel("index_bounds_trap")
	done := e.newLabel("index_bounds_ok")
	emitCmpRegReg(e, index, length)
	e.emitJcc(0x83, trap)
	e.emitJmp(done)
	e.bindLabel(trap)
	emitCallReloc(e, "_wrela_memory_oom")
	e.bindLabel(done)
}

func emitTrapIfZero(e *Emitter, value asm.Reg) {
	ok := e.newLabel("arena_nonzero_ok")
	zero := asm.MustLookup("rcx")
	emitMovImmToReg(e, zero, 0)
	emitCmpRegReg(e, value, zero)
	e.emitJcc(0x85, ok)
	emitCallReloc(e, "_wrela_memory_oom")
	e.bindLabel(ok)
}

func emitTrapIfNonPowerOfTwo(e *Emitter, value asm.Reg) {
	// Clobbers r11 and rcx. Callers must schedule this before either register
	// holds a live value.
	ok := e.newLabel("arena_power_of_two_ok")
	tmp := asm.MustLookup("r11")
	emitRegRegMove(e, tmp, value)
	e.emitInstruction(asm.Instruction{Mnemonic: "sub", Operands: []asm.Operand{
		asm.RegOperand{Reg: tmp},
		asm.ImmOperand{Value: 1},
	}})
	emitRegRegOp(e, 0x21, tmp, value)
	zero := asm.MustLookup("rcx")
	emitMovImmToReg(e, zero, 0)
	emitCmpRegReg(e, tmp, zero)
	e.emitJcc(0x84, ok)
	emitCallReloc(e, "_wrela_memory_oom")
	e.bindLabel(ok)
}

func emitValueAddressToReg(e *Emitter, frame Frame, value ir.Value, dst asm.Reg) bool {
	base, disp, ok := emitValueAddress(e, frame, value)
	if !ok {
		return false
	}
	emitRegRegMove(e, dst, base)
	if disp != 0 {
		e.emitInstruction(asm.Instruction{Mnemonic: "add", Operands: []asm.Operand{
			asm.RegOperand{Reg: dst},
			asm.ImmOperand{Value: disp},
		}})
	}
	return true
}

func emitCopy(e *Emitter, op *ir.Copy, frame Frame) {
	if op.Target == nil || op.Source == nil {
		return
	}
	slot, ok := frame.Slots[op.Target]
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "missing copy target slot"})
		return
	}
	emitCopyValueToStackRange(e, frame, op.Source, slot, e.ctx.representationSize(op.Type.Name))
}

func emitFieldLoad(e *Emitter, op *ir.FieldLoad, frame Frame) {
	outSlot, ok := frame.Slots[op]
	if !ok {
		return
	}
	if e.ctx.isDataType(op.Type) {
		objectSlot, ok := frame.ObjectSlots[op]
		if !ok {
			e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "missing field load object slot"})
			return
		}
		base, disp, ok := emitObjectAddress(e, frame, op.Object, op.Offset)
		if !ok {
			return
		}
		emitLoadMemToReg(e, asm.MustLookup("r11"), base, disp, 64)
		emitDeepCopyObjectToAddressAsType(e, op.Type, asm.MustLookup("r11"), 0, asm.MustLookup("rbp"), int64(objectSlot))
		emitSlotFromBase(e, scratchRegs[0], asm.MustLookup("rbp"), objectSlot)
		emitStoreSlotFromReg(e, scratchRegs[0], outSlot, 64)
		return
	}
	base, disp, ok := emitObjectAddress(e, frame, op.Object, op.Offset)
	if !ok {
		return
	}
	size := e.ctx.representationSize(op.Type.Name)
	emitCopyMemoryToStackRange(e, base, disp, outSlot, size)
}

func emitFieldStore(e *Emitter, op *ir.FieldStore, frame Frame) {
	info, infoOK := e.ctx.types[op.ObjectType]
	if infoOK {
		if fieldInfo, ok := info.Fields[op.Field]; ok && e.ctx.isDataType(fieldInfo.Type) && fieldInfo.StorageOffset >= 0 {
			base, disp, ok := emitValueAddress(e, frame, op.Object)
			if !ok {
				return
			}
			emitStoreNestedDestinationHandle(e, base, disp+int64(fieldInfo.Offset), disp+int64(fieldInfo.StorageOffset))
			if base.Name == "r11" {
				emitPushReg(e, base)
				srcBase, srcDisp, ok := emitValueAddress(e, frame, op.Value)
				if !ok {
					emitPopReg(e, asm.MustLookup("rdi"))
					return
				}
				emitPopReg(e, asm.MustLookup("rdi"))
				emitDeepCopyObjectToAddressAsType(e, fieldInfo.Type, srcBase, srcDisp, asm.MustLookup("rdi"), disp+int64(fieldInfo.StorageOffset))
				return
			}
			emitDeepCopyValueToTypedStorage(e, frame, op.Value, fieldInfo.Type, base, disp+int64(fieldInfo.StorageOffset))
			return
		}
	}
	base, disp, ok := emitObjectAddress(e, frame, op.Object, op.Offset)
	if !ok {
		return
	}
	size := e.ctx.representationSize(op.Type.Name)
	emitCopyValueToMemoryRange(e, frame, op.Value, base, disp, size)
}

func emitInterruptContextStore(e *Emitter, frame Frame, store *ir.InterruptContextStore) {
	if store.Size == 8 && e.ctx.isHandleType(valueType(store.Source).Name) {
		emitLoadValue(e, frame, store.Source, asm.MustLookup("rcx"))
		emitMovDataAddressToReg(e, asm.MustLookup("rax"), store.ContextSymbol)
		emitStoreMemFromReg(e, asm.MustLookup("rax"), int64(store.ContextOffset), asm.MustLookup("rcx"), 64)
		return
	}
	srcBase, srcDisp, ok := emitValueAddress(e, frame, store.Source)
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "cannot address interrupt context source"})
		return
	}
	emitMovDataAddressToReg(e, asm.MustLookup("rax"), store.ContextSymbol)
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("rdi")},
		asm.RegOperand{Reg: asm.MustLookup("rax")},
	}})
	emitCopyBytes(e, asm.MustLookup("rdi"), int64(store.ContextOffset), srcBase, srcDisp, store.Size)
}

func emitTopicPublish(e *Emitter, frame Frame, publish *ir.TopicPublish) {
	layout, ok := e.ctx.topicLayouts[publish.TopicLabel]
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "missing topic layout: " + publish.TopicLabel})
		return
	}
	if (publish.Kind != "" && publish.Kind != "gap_u64") || (layout.Kind != "" && layout.Kind != "gap_u64") {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "unsupported topic kind: " + publish.Kind})
		return
	}

	base := asm.MustLookup("rax")
	seq := asm.MustLookup("r10")
	slot := asm.MustLookup("r11")
	value := asm.MustLookup("rcx")

	emitMovDataAddressToReg(e, base, topicDataSymbol(publish.TopicLabel))
	emitLoadMemToReg(e, seq, base, int64(layout.HeadOffset), 64)
	emitRegRegMove(e, slot, seq)
	emitMovImmToReg(e, asm.MustLookup("rdx"), int64(layout.Depth-1))
	emitRegRegOp(e, 0x21, slot, asm.MustLookup("rdx"))
	emitScaleTopicSlot(e, slot, asm.MustLookup("rdx"), layout)
	emitAddImm(e, slot, int64(layout.SlotsOffset))
	emitRegRegOp(e, 0x01, slot, base)
	emitAddImm(e, seq, 1)
	emitStoreMemFromReg(e, slot, 0, seq, 64)
	emitLoadValue(e, frame, publish.Value, value)
	emitStoreMemFromReg(e, slot, 8, value, 64)
	emitMfence(e)
	emitStoreMemFromReg(e, base, int64(layout.HeadOffset), seq, 64)
	wakeSlots := make([]string, 0, len(layout.Subscribers))
	for _, subscriber := range layout.Subscribers {
		wakeSlots = append(wakeSlots, subscriber.Label)
	}
	emitTouchSubscriberWaitlinesAndWake(e, layout, base, seq, wakeSlots, e.ctx)
}

func emitReliableTopicTryPublish(e *Emitter, frame Frame, publish *ir.ReliableTopicTryPublish) {
	layout, ok := e.ctx.topicLayouts[publish.TopicLabel]
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "missing topic layout: " + publish.TopicLabel})
		return
	}
	if layout.Kind != "reliable_u64" {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "unsupported topic kind: " + layout.Kind})
		return
	}
	valueSlot, ok := frameSlot(frame, publish)
	if !ok {
		return
	}
	outSlot, ok := frameObjectSlot(frame, publish)
	if !ok {
		return
	}
	publishedOffset := outSlot
	fullOffset := outSlot + 1
	if info, ok := e.ctx.typeInfo(publish.Type); ok {
		if field, ok := info.Fields["published"]; ok {
			publishedOffset = outSlot + field.Offset
		}
		if field, ok := info.Fields["full"]; ok {
			fullOffset = outSlot + field.Offset
		}
	}

	base := asm.MustLookup("rax")
	producer := asm.MustLookup("r10")
	minCursor := asm.MustLookup("r11")
	candidate := asm.MustLookup("rcx")
	slot := asm.MustLookup("rdx")
	value := asm.MustLookup("rdi")
	full := e.newLabel("reliable_topic_publish_full")
	done := e.newLabel("reliable_topic_publish_done")

	emitSlotFromBase(e, asm.MustLookup("r8"), asm.MustLookup("rbp"), outSlot)
	emitStoreSlotFromReg(e, asm.MustLookup("r8"), valueSlot, 64)
	emitZeroSlotRange(e, outSlot, e.ctx.storageSizeForType(publish.Type))
	emitMovDataAddressToReg(e, base, topicDataSymbol(publish.TopicLabel))
	emitLoadMemToReg(e, producer, base, int64(layout.HeadOffset), 64)
	emitReliableTopicMinCursor(e, layout, base, minCursor, candidate)
	emitRegRegMove(e, candidate, producer)
	emitRegRegOp(e, 0x29, candidate, minCursor)
	emitMovImmToReg(e, value, int64(layout.Depth))
	emitCmpRegReg(e, candidate, value)
	e.emitJcc(0x83, full)

	emitRegRegMove(e, slot, producer)
	emitMovImmToReg(e, value, int64(layout.Depth-1))
	emitRegRegOp(e, 0x21, slot, value)
	emitScaleTopicSlot(e, slot, value, layout)
	emitAddImm(e, slot, int64(layout.SlotsOffset))
	emitRegRegOp(e, 0x01, slot, base)
	emitAddImm(e, producer, 1)
	emitStoreMemFromReg(e, slot, 0, producer, 64)
	emitLoadValue(e, frame, publish.Value, value)
	emitStoreMemFromReg(e, slot, 8, value, 64)
	emitMfence(e)
	emitStoreMemFromReg(e, base, int64(layout.HeadOffset), producer, 64)
	emitMovImmToReg(e, value, topicWaitlineDisarmed)
	emitStoreMemFromReg(e, base, int64(layout.ProducerWaitlineOffset), value, 64)
	wakeSlots := make([]string, 0, len(layout.Subscribers))
	for _, subscriber := range layout.Subscribers {
		wakeSlots = append(wakeSlots, subscriber.Label)
	}
	emitTouchSubscriberWaitlinesAndWake(e, layout, base, producer, wakeSlots, e.ctx)
	emitStoreSlotBool(e, publishedOffset, 1)
	e.emitJmp(done)

	e.bindLabel(full)
	emitStoreSlotBool(e, fullOffset, 1)
	e.bindLabel(done)
}

func emitReliableTopicWaitForAdvance(e *Emitter, wait *ir.ReliableTopicWaitForAdvance) {
	layout, ok := e.ctx.topicLayouts[wait.TopicLabel]
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "missing topic layout: " + wait.TopicLabel})
		return
	}
	base := asm.MustLookup("rax")
	producer := asm.MustLookup("r10")
	minCursor := asm.MustLookup("r11")
	candidate := asm.MustLookup("rcx")
	notFull := e.newLabel("reliable_topic_wait_not_full")
	armedNotFull := e.newLabel("reliable_topic_wait_armed_not_full")

	emitMovDataAddressToReg(e, base, topicDataSymbol(wait.TopicLabel))
	emitLoadMemToReg(e, producer, base, int64(layout.HeadOffset), 64)
	emitReliableTopicMinCursor(e, layout, base, minCursor, candidate)
	emitRegRegMove(e, candidate, producer)
	emitRegRegOp(e, 0x29, candidate, minCursor)
	emitMovImmToReg(e, asm.MustLookup("rdi"), int64(layout.Depth))
	emitCmpRegReg(e, candidate, asm.MustLookup("rdi"))
	e.emitJcc(0x82, notFull)
	e.emit(0xFA)
	emitStoreMemFromReg(e, base, int64(layout.ProducerWaitlineOffset), minCursor, 64)
	emitReliableTopicMinCursor(e, layout, base, minCursor, candidate)
	emitRegRegMove(e, candidate, producer)
	emitRegRegOp(e, 0x29, candidate, minCursor)
	emitMovImmToReg(e, asm.MustLookup("rdi"), int64(layout.Depth))
	emitCmpRegReg(e, candidate, asm.MustLookup("rdi"))
	e.emitJcc(0x82, armedNotFull)
	e.emit(0xFB)
	emitHltWait(e)
	e.bindLabel(armedNotFull)
	e.emit(0xFB)
	e.bindLabel(notFull)
}

func emitWakeSubscriberSlots(e *Emitter, subscribers []string, ctx compileContext) {
	for _, slot := range subscribers {
		emitWakeSlot(e, slot, ctx)
	}
}

func emitTouchSubscriberWaitlinesAndWake(e *Emitter, layout topicDataLayout, base asm.Reg, seq asm.Reg, subscribers []string, ctx compileContext) {
	emitTouchSubscriberWaitlinesAndWakeSkippingVcpu(e, layout, base, seq, subscribers, ctx, -1)
}

func emitTouchSubscriberWaitlinesAndWakeSkippingVcpu(e *Emitter, layout topicDataLayout, base asm.Reg, seq asm.Reg, subscribers []string, ctx compileContext, skipVcpuID int) {
	for _, label := range subscribers {
		subscriber, ok := topicSubscriberLayoutByLabel(layout, label)
		if !ok {
			e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "missing topic subscriber layout: " + label})
			continue
		}
		skip := e.newLabel("topic_wake_skip")
		waitline := asm.MustLookup("r11")
		emitLoadMemToReg(e, waitline, base, int64(subscriber.WaitlineOffset), 64)
		emitMovImmToReg(e, asm.MustLookup("rdi"), topicWaitlineDisarmed)
		emitCmpRegReg(e, waitline, asm.MustLookup("rdi"))
		e.emitJcc(0x84, skip)
		emitCmpRegReg(e, seq, waitline)
		e.emitJcc(0x84, skip)
		emitStoreMemFromReg(e, base, int64(subscriber.WaitlineOffset), seq, 64)
		if vcpuID, ok := ctx.SlotVcpu[label]; !ok || vcpuID != skipVcpuID {
			emitWakeSlot(e, label, ctx)
		}
		e.bindLabel(skip)
	}
}

func emitWakeProducerSlots(e *Emitter, layout topicDataLayout, cursor asm.Reg, ctx compileContext) {
	if len(layout.Producers) == 0 {
		return
	}
	base := asm.MustLookup("rax")
	emitMovDataAddressToReg(e, base, topicDataSymbol(layout.Label))
	skip := e.newLabel("topic_producer_wake_skip")
	waitline := asm.MustLookup("rdx")
	emitLoadMemToReg(e, waitline, base, int64(layout.ProducerWaitlineOffset), 64)
	emitMovImmToReg(e, asm.MustLookup("rdi"), topicWaitlineDisarmed)
	emitCmpRegReg(e, waitline, asm.MustLookup("rdi"))
	e.emitJcc(0x84, skip)
	emitCmpRegReg(e, cursor, waitline)
	e.emitJcc(0x84, skip)
	emitStoreMemFromReg(e, base, int64(layout.ProducerWaitlineOffset), cursor, 64)
	for _, slot := range layout.Producers {
		emitWakeSlot(e, slot, ctx)
	}
	e.bindLabel(skip)
}

func emitWakeSlot(e *Emitter, slot string, ctx compileContext) {
	vcpuID, ok := ctx.SlotVcpu[slot]
	if !ok {
		if len(ctx.SlotVcpu) != 0 {
			e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "missing vCPU placement for slot: " + slot})
		}
		return
	}
	emitTouchMonitorMwaitWaitline(e, slot)
	emitLoadLocalApicBase(e, asm.MustLookup("r11"))
	emitMovDataAddressToReg(e, asm.MustLookup("rax"), vcpuAPICIDCommandSymbol(vcpuID))
	emitLoadMemToReg(e, asm.MustLookup("rax"), asm.MustLookup("rax"), 0, 64)
	emitSendIcrForMode(e, asm.MustLookup("r11"), asm.MustLookup("rax"), 0x00004000|0xF0, ctx.APICMode)
}

func emitTouchMonitorMwaitWaitline(e *Emitter, slot string) {
	if slot == "" {
		return
	}
	emitMovDataAddressToReg(e, asm.MustLookup("rax"), monitorMwaitWaitlineSymbol(slot))
	emitLoadMemToReg(e, asm.MustLookup("r11"), asm.MustLookup("rax"), 0, 64)
	emitAddImm(e, asm.MustLookup("r11"), 1)
	emitStoreMemFromReg(e, asm.MustLookup("rax"), 0, asm.MustLookup("r11"), 64)
	emitMfence(e)
}

func emitReliableTopicMinCursor(e *Emitter, layout topicDataLayout, base asm.Reg, dst asm.Reg, tmp asm.Reg) {
	if len(layout.Subscribers) == 0 {
		emitLoadMemToReg(e, dst, base, int64(layout.HeadOffset), 64)
		return
	}
	emitLoadMemToReg(e, dst, base, int64(layout.Subscribers[0].CursorOffset), 64)
	for _, subscriber := range layout.Subscribers[1:] {
		skip := e.newLabel("reliable_topic_min_cursor_skip")
		emitCmpRegMem(e, dst, base, int64(subscriber.CursorOffset), 64)
		e.emitJcc(0x86, skip)
		emitLoadMemToReg(e, tmp, base, int64(subscriber.CursorOffset), 64)
		emitRegRegMove(e, dst, tmp)
		e.bindLabel(skip)
	}
}

func emitScaleTopicSlot(e *Emitter, slot asm.Reg, scratch asm.Reg, layout topicDataLayout) {
	emitMovImmToReg(e, scratch, int64(layout.SlotSize))
	emitRegRegIMul(e, slot, scratch)
}

func emitTopicTryNext(e *Emitter, frame Frame, next *ir.TopicTryNext) {
	layout, ok := e.ctx.topicLayouts[next.TopicLabel]
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "missing topic layout: " + next.TopicLabel})
		return
	}
	valueSlot, ok := frameSlot(frame, next)
	if !ok {
		return
	}
	outSlot, ok := frameObjectSlot(frame, next)
	if !ok {
		return
	}

	base := asm.MustLookup("rax")
	head := asm.MustLookup("r10")
	cursor := asm.MustLookup("r11")
	slot := asm.MustLookup("rcx")
	tmp := asm.MustLookup("rdx")
	done := e.newLabel("topic_try_next_done")
	noMessage := e.newLabel("topic_try_next_empty")
	gap := e.newLabel("topic_try_next_gap")
	resultInfo, hasResultInfo := e.ctx.typeInfo(next.Type)
	messageField := resultInfo.Fields["message"]
	hasMessageField := hasResultInfo && messageField.StorageOffset >= 0
	hasMessageOffset := outSlot
	if hasResultInfo {
		if field, ok := resultInfo.Fields["has_message"]; ok {
			hasMessageOffset = outSlot + field.Offset
		}
	}
	gapOffset := outSlot + 1
	if hasResultInfo {
		if field, ok := resultInfo.Fields["gap"]; ok {
			gapOffset = outSlot + field.Offset
		}
	}
	missedOffset := outSlot + 8
	if hasResultInfo {
		if field, ok := resultInfo.Fields["missed"]; ok {
			missedOffset = outSlot + field.Offset
		}
	}

	emitSlotFromBase(e, asm.MustLookup("r8"), asm.MustLookup("rbp"), outSlot)
	emitStoreSlotFromReg(e, asm.MustLookup("r8"), valueSlot, 64)
	emitZeroSlotRange(e, outSlot, e.ctx.storageSizeForType(next.Type))
	if hasMessageField {
		emitStoreNestedDestinationHandle(e, asm.MustLookup("rbp"), int64(outSlot+messageField.Offset), int64(outSlot+messageField.StorageOffset))
	}
	emitMovDataAddressToReg(e, base, topicDataSymbol(next.TopicLabel))
	emitLoadSubscriptionCursor(e, frame, next.Subscription, next.SubscriberSlot, cursor, layout)
	emitLoadMemToReg(e, head, base, int64(layout.HeadOffset), 64)
	emitCmpRegReg(e, cursor, head)
	e.emitJcc(0x83, noMessage)
	emitRegRegMove(e, slot, cursor)
	emitMovImmToReg(e, asm.MustLookup("rdi"), int64(layout.Depth-1))
	emitRegRegOp(e, 0x21, slot, asm.MustLookup("rdi"))
	emitScaleTopicSlot(e, slot, asm.MustLookup("rdi"), layout)
	emitAddImm(e, slot, int64(layout.SlotsOffset))
	emitRegRegOp(e, 0x01, slot, base)
	emitAddImm(e, cursor, 1)
	emitLoadMemToReg(e, tmp, slot, 0, 64)
	emitCmpRegReg(e, tmp, cursor)
	e.emitJcc(0x85, gap)
	emitMfence(e)
	emitMovImmToReg(e, tmp, 1)
	emitStoreSlotFromReg(e, tmp, hasMessageOffset, 8)
	if hasMessageField {
		if messageField.Type.Name == "U64TopicMessage" {
			messageInfo, _ := e.ctx.typeInfo(messageField.Type)
			sequenceOffset := messageField.StorageOffset
			if field, ok := messageInfo.Fields["sequence"]; ok {
				sequenceOffset += field.Offset
			}
			valueOffset := messageField.StorageOffset + 8
			if field, ok := messageInfo.Fields["value"]; ok {
				valueOffset = messageField.StorageOffset + field.Offset
			}
			emitStoreSlotFromReg(e, cursor, outSlot+sequenceOffset, 64)
			emitLoadMemToReg(e, tmp, slot, 8, 64)
			emitStoreSlotFromReg(e, tmp, outSlot+valueOffset, 64)
		} else {
			emitCopyBytes(e, asm.MustLookup("rbp"), int64(outSlot+messageField.StorageOffset), slot, 8, messageField.StorageSize)
		}
	} else {
		emitStoreSlotFromReg(e, tmp, outSlot+16, 64)
		emitLoadMemToReg(e, tmp, slot, 8, 64)
		emitStoreSlotFromReg(e, tmp, outSlot+24, 64)
	}
	emitStoreSubscriptionCursor(e, frame, next.Subscription, next.SubscriberSlot, cursor, layout)
	emitDisarmSubscriptionWait(e, frame, next.Subscription, next.SubscriberSlot, layout)
	if layout.Kind == "reliable_u64" {
		emitWakeProducerSlots(e, layout, cursor, e.ctx)
	}
	e.emitJmp(done)
	e.bindLabel(gap)
	emitRegRegMove(e, tmp, head)
	emitRegRegOp(e, 0x29, tmp, cursor)
	emitAddImm(e, tmp, 1)
	emitStoreSlotFromReg(e, tmp, missedOffset, 64)
	emitMovImmToReg(e, tmp, 1)
	emitStoreSlotFromReg(e, tmp, gapOffset, 8)
	emitStoreSubscriptionCursor(e, frame, next.Subscription, next.SubscriberSlot, head, layout)
	emitDisarmSubscriptionWait(e, frame, next.Subscription, next.SubscriberSlot, layout)
	if layout.Kind == "reliable_u64" {
		emitWakeProducerSlots(e, layout, head, e.ctx)
	}
	e.emitJmp(done)
	e.bindLabel(noMessage)
	e.bindLabel(done)
}

func emitTopicArmWait(e *Emitter, frame Frame, arm *ir.TopicArmWait) {
	layout, ok := e.ctx.topicLayouts[arm.TopicLabel]
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "missing topic layout: " + arm.TopicLabel})
		return
	}
	subscriber, ok := topicSubscriberLayoutForSubscription(layout, arm.Subscription, arm.SubscriberSlot)
	if !ok {
		emitStoreSubscriptionArmedValue(e, frame, arm.Subscription, arm.SubscriberSlot, layout, 1)
		return
	}
	base := asm.MustLookup("rax")
	head := asm.MustLookup("r10")
	emitMovDataAddressToReg(e, base, topicDataSymbol(layout.Label))
	emitLoadMemToReg(e, head, base, int64(layout.HeadOffset), 64)
	emitStoreMemFromReg(e, base, int64(subscriber.WaitlineOffset), head, 64)
}

func emitTopicIsWaitArmed(e *Emitter, frame Frame, armed *ir.TopicIsWaitArmed) {
	outSlot, ok := frameSlot(frame, armed)
	if !ok {
		return
	}
	layout, ok := e.ctx.topicLayouts[armed.TopicLabel]
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "missing topic layout: " + armed.TopicLabel})
		return
	}
	if subscriber, ok := topicSubscriberLayoutForSubscription(layout, armed.Subscription, armed.SubscriberSlot); ok {
		base := asm.MustLookup("rax")
		cursor := asm.MustLookup("r10")
		waitline := asm.MustLookup("r11")
		store := e.newLabel("topic_is_wait_armed_store")
		emitMovDataAddressToReg(e, base, topicDataSymbol(layout.Label))
		emitLoadMemToReg(e, cursor, base, int64(subscriber.CursorOffset), 64)
		emitLoadMemToReg(e, waitline, base, int64(subscriber.WaitlineOffset), 64)
		emitMovImmToReg(e, asm.MustLookup("rcx"), topicWaitlineDisarmed)
		emitCmpRegReg(e, waitline, asm.MustLookup("rcx"))
		emitMovImmToReg(e, asm.MustLookup("rax"), 0)
		e.emitJcc(0x84, store)
		emitCmpRegReg(e, cursor, waitline)
		emitSetccAl(e, setccOpcode("eq"))
		e.bindLabel(store)
		emitStoreSlotFromReg(e, asm.MustLookup("rax"), outSlot, 8)
		return
	}
	base, disp, ok := emitSubscriptionFieldAddress(e, frame, armed.Subscription, "armed")
	if !ok {
		emitStoreSlotBool(e, outSlot, 0)
		return
	}
	emitLoadMemToReg(e, asm.MustLookup("rax"), base, disp, 8)
	emitStoreSlotFromReg(e, asm.MustLookup("rax"), outSlot, 8)
}

func emitTopicWaitIfArmed(e *Emitter, frame Frame, wait *ir.TopicWaitIfArmed) {
	guards := wait.Guards
	if len(guards) == 0 {
		guards = []ir.TopicWaitGuard{{
			TopicLabel:     wait.TopicLabel,
			SubscriberSlot: wait.SubscriberSlot,
			Subscription:   wait.Subscription,
		}}
	}
	base := asm.MustLookup("rax")
	cursor := asm.MustLookup("r10")
	waitline := asm.MustLookup("r11")
	skip := e.newLabel("topic_wait_if_armed_skip")
	done := e.newLabel("topic_wait_if_armed_done")
	e.emit(0xFA)
	for _, guard := range guards {
		layout, ok := e.ctx.topicLayouts[guard.TopicLabel]
		if !ok {
			e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "missing topic layout: " + guard.TopicLabel})
			return
		}
		subscriber, ok := topicSubscriberLayoutForSubscription(layout, guard.Subscription, guard.SubscriberSlot)
		if !ok {
			e.emit(0xFB)
			emitHltWait(e)
			return
		}
		emitMovDataAddressToReg(e, base, topicDataSymbol(layout.Label))
		emitLoadMemToReg(e, cursor, base, int64(subscriber.CursorOffset), 64)
		emitLoadMemToReg(e, waitline, base, int64(subscriber.WaitlineOffset), 64)
		emitMovImmToReg(e, asm.MustLookup("rcx"), topicWaitlineDisarmed)
		emitCmpRegReg(e, waitline, asm.MustLookup("rcx"))
		e.emitJcc(0x84, skip)
		emitCmpRegReg(e, cursor, waitline)
		e.emitJcc(0x85, skip)
	}
	e.emit(0xFB)
	emitHltWait(e)
	e.emitJmp(done)
	e.bindLabel(skip)
	e.emit(0xFB)
	e.bindLabel(done)
}

func emitLoadSubscriptionCursor(e *Emitter, frame Frame, sub ir.Value, subscriberSlot string, dst asm.Reg, layout topicDataLayout) {
	if subscriber, ok := topicSubscriberLayoutForSubscription(layout, sub, subscriberSlot); ok {
		emitMovDataAddressToReg(e, asm.MustLookup("rax"), topicDataSymbol(layout.Label))
		emitLoadMemToReg(e, dst, asm.MustLookup("rax"), int64(subscriber.CursorOffset), 64)
		return
	}
	base, disp, ok := emitSubscriptionFieldAddress(e, frame, sub, "cursor")
	if !ok {
		emitMovImmToReg(e, dst, 0)
		return
	}
	emitLoadMemToReg(e, dst, base, disp, 64)
}

func emitStoreSubscriptionCursor(e *Emitter, frame Frame, sub ir.Value, subscriberSlot string, src asm.Reg, layout topicDataLayout) {
	if subscriber, ok := topicSubscriberLayoutForSubscription(layout, sub, subscriberSlot); ok {
		emitMovDataAddressToReg(e, asm.MustLookup("rax"), topicDataSymbol(layout.Label))
		emitStoreMemFromReg(e, asm.MustLookup("rax"), int64(subscriber.CursorOffset), src, 64)
		return
	}
	storeSrc := src
	if src.Name == "r11" {
		storeSrc = asm.MustLookup("r10")
		emitRegRegMove(e, storeSrc, src)
	}
	base, disp, ok := emitSubscriptionFieldAddress(e, frame, sub, "cursor")
	if ok {
		emitStoreMemFromReg(e, base, disp, storeSrc, 64)
	}
}

func emitStoreSubscriptionArmedValue(e *Emitter, frame Frame, sub ir.Value, subscriberSlot string, layout topicDataLayout, value int64) {
	if _, ok := topicSubscriberLayoutForSubscription(layout, sub, subscriberSlot); ok {
		return
	}
	base, disp, ok := emitSubscriptionFieldAddress(e, frame, sub, "armed")
	if !ok {
		return
	}
	valueReg := asm.MustLookup("rdi")
	if base.Name == valueReg.Name {
		valueReg = asm.MustLookup("r10")
	}
	emitMovImmToReg(e, valueReg, value)
	emitStoreMemFromReg(e, base, disp, valueReg, 8)
}

func emitDisarmSubscriptionWait(e *Emitter, frame Frame, sub ir.Value, subscriberSlot string, layout topicDataLayout) {
	if subscriber, ok := topicSubscriberLayoutForSubscription(layout, sub, subscriberSlot); ok {
		emitMovDataAddressToReg(e, asm.MustLookup("rax"), topicDataSymbol(layout.Label))
		emitMovImmToReg(e, asm.MustLookup("rdi"), topicWaitlineDisarmed)
		emitStoreMemFromReg(e, asm.MustLookup("rax"), int64(subscriber.WaitlineOffset), asm.MustLookup("rdi"), 64)
		return
	}
	emitStoreSubscriptionArmedValue(e, frame, sub, subscriberSlot, layout, 0)
}

func emitSubscriptionFieldAddress(e *Emitter, frame Frame, sub ir.Value, field string) (asm.Reg, int64, bool) {
	info, ok := e.ctx.typeInfo(valueType(sub))
	if !ok {
		return asm.Reg{}, 0, false
	}
	fieldInfo, ok := info.Fields[field]
	if !ok {
		return asm.Reg{}, 0, false
	}
	base, disp, ok := emitValueAddress(e, frame, sub)
	if !ok {
		return asm.Reg{}, 0, false
	}
	return base, disp + int64(fieldInfo.Offset), true
}

func topicSubscriberLayout(layout topicDataLayout, sub ir.Value) (topicDataSubscriberLayout, bool) {
	local, ok := sub.(*ir.Local)
	if !ok {
		return topicDataSubscriberLayout{}, false
	}
	return topicSubscriberLayoutByLabel(layout, local.Symbol)
}

func topicSubscriberLayoutForSubscription(layout topicDataLayout, sub ir.Value, subscriberSlot string) (topicDataSubscriberLayout, bool) {
	if subscriberSlot != "" {
		return topicSubscriberLayoutByLabel(layout, subscriberSlot)
	}
	return topicSubscriberLayout(layout, sub)
}

func topicSubscriberLayoutByLabel(layout topicDataLayout, label string) (topicDataSubscriberLayout, bool) {
	for _, subscriber := range layout.Subscribers {
		if subscriber.Label == label {
			return subscriber, true
		}
	}
	return topicDataSubscriberLayout{}, false
}

func emitStoreSlotBool(e *Emitter, slot int, value int64) {
	emitMovImmToReg(e, scratchRegs[0], value)
	emitStoreSlotFromReg(e, scratchRegs[0], slot, 8)
}

func topicDataSymbol(label string) string {
	return "_wrela_topic_" + sanitizeSymbol(label)
}

func emitAddImm(e *Emitter, reg asm.Reg, value int64) {
	e.emitInstruction(asm.Instruction{Mnemonic: "add", Operands: []asm.Operand{
		asm.RegOperand{Reg: reg},
		asm.ImmOperand{Value: value},
	}})
}

func emitMfence(e *Emitter) {
	e.emit(0x0F, 0xAE, 0xF0)
}

func emitCopyBytes(e *Emitter, dstBase asm.Reg, dstDisp int64, srcBase asm.Reg, srcDisp int64, size int) {
	offset := 0
	for size-offset >= 8 {
		emitCopyWidth(e, dstBase, dstDisp+int64(offset), srcBase, srcDisp+int64(offset), 64, "rax")
		offset += 8
	}
	if size-offset >= 4 {
		emitCopyWidth(e, dstBase, dstDisp+int64(offset), srcBase, srcDisp+int64(offset), 32, "eax")
		offset += 4
	}
	if size-offset >= 2 {
		emitCopyWidth(e, dstBase, dstDisp+int64(offset), srcBase, srcDisp+int64(offset), 16, "ax")
		offset += 2
	}
	if size-offset == 1 {
		emitCopyWidth(e, dstBase, dstDisp+int64(offset), srcBase, srcDisp+int64(offset), 8, "al")
	}
}

func emitCopyWidth(e *Emitter, dstBase asm.Reg, dstDisp int64, srcBase asm.Reg, srcDisp int64, width int, regName string) {
	reg := asm.MustLookup(regName)
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: reg},
		asm.MemOperand{Base: srcBase, Disp: srcDisp, Width: width},
	}})
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.MemOperand{Base: dstBase, Disp: dstDisp, Width: width},
		asm.RegOperand{Reg: reg},
	}})
}

func emitZeroSlotRange(e *Emitter, slot int, size int) {
	emitMovImmToReg(e, scratchRegs[0], 0)
	for offset := 0; offset < size; {
		width := copyWidth(size - offset)
		emitStoreSlotFromReg(e, scratchRegs[0], slot+offset, width)
		offset += width / 8
	}
}

func emitCopyValueToStackRange(e *Emitter, frame Frame, value ir.Value, dstSlot int, size int) {
	if e.ctx.isHandleType(valueTypeName(value)) && size != 8 {
		base, disp, ok := emitValueAddress(e, frame, value)
		if !ok {
			return
		}
		emitCopyMemoryToStackRange(e, base, disp, dstSlot, size)
		return
	}
	emitLoadValue(e, frame, value, scratchRegs[0])
	emitStoreSlotFromReg(e, scratchRegs[0], dstSlot, valueWidthBits(e.ctx, value))
}

func emitCopyValueToMemoryRange(e *Emitter, frame Frame, value ir.Value, dstBase asm.Reg, dstDisp int64, size int) {
	if e.ctx.isHandleType(valueTypeName(value)) && size != 8 {
		srcBase, srcDisp, ok := emitValueAddress(e, frame, value)
		if !ok {
			return
		}
		emitCopyMemoryToMemoryRange(e, srcBase, srcDisp, dstBase, dstDisp, size)
		return
	}
	emitLoadValue(e, frame, value, scratchRegs[0])
	emitStoreMemFromReg(e, dstBase, dstDisp, scratchRegs[0], valueWidthBits(e.ctx, value))
}

func emitCopyValueToAddress(e *Emitter, frame Frame, value ir.Value, dstBase asm.Reg) {
	emitCopyValueToAddressAsType(e, frame, value, dstBase, valueTypeName(value))
}

func emitCopyValueToAddressAsType(e *Emitter, frame Frame, value ir.Value, dstBase asm.Reg, typeName string) {
	if e.ctx.isHandleType(typeName) {
		srcBase, srcDisp, ok := emitValueAddress(e, frame, value)
		if !ok {
			return
		}
		emitDeepCopyObjectToAddress(e, typeName, srcBase, srcDisp, dstBase, 0)
		return
	}
	emitCopyValueToMemoryRange(e, frame, value, dstBase, 0, e.ctx.representationSize(typeName))
}

func emitDeepCopyObjectToAddress(e *Emitter, typeName string, srcBase asm.Reg, srcDisp int64, dstBase asm.Reg, dstDisp int64) {
	emitDeepCopyObjectToAddressAsType(e, ir.Type{Name: typeName}, srcBase, srcDisp, dstBase, dstDisp)
}

func emitDeepCopyValueToTypedStorage(e *Emitter, frame Frame, value ir.Value, typ ir.Type, dstBase asm.Reg, dstDisp int64) {
	srcBase, srcDisp, ok := emitValueAddress(e, frame, value)
	if !ok {
		return
	}
	emitDeepCopyObjectToAddressAsType(e, typ, srcBase, srcDisp, dstBase, dstDisp)
}

func emitDeepCopyObjectToAddressAsType(e *Emitter, typ ir.Type, srcBase asm.Reg, srcDisp int64, dstBase asm.Reg, dstDisp int64) {
	emitCopyMemoryToMemoryRange(e, srcBase, srcDisp, dstBase, dstDisp, e.ctx.storageSizeForType(typ))
	info, ok := e.ctx.typeInfo(typ)
	if !ok {
		return
	}
	for _, fieldName := range info.FieldOrder {
		field := info.Fields[fieldName]
		if field.Type.Kind != ir.TypeKindData || field.StorageOffset < 0 {
			continue
		}
		emitStoreNestedDestinationHandle(e, dstBase, dstDisp+int64(field.Offset), dstDisp+int64(field.StorageOffset))
		emitPushReg(e, srcBase)
		emitPushReg(e, dstBase)
		emitLoadMemToReg(e, asm.MustLookup("r11"), srcBase, srcDisp+int64(field.Offset), 64)
		emitRegRegMove(e, asm.MustLookup("r10"), dstBase)
		if childDisp := dstDisp + int64(field.StorageOffset); childDisp != 0 {
			e.emitInstruction(asm.Instruction{Mnemonic: "add", Operands: []asm.Operand{
				asm.RegOperand{Reg: asm.MustLookup("r10")},
				asm.ImmOperand{Value: childDisp},
			}})
		}
		emitDeepCopyObjectToAddressAsType(e, field.Type, asm.MustLookup("r11"), 0, asm.MustLookup("r10"), 0)
		emitPopReg(e, dstBase)
		emitPopReg(e, srcBase)
	}
}

func emitStoreNestedDestinationHandle(e *Emitter, dstBase asm.Reg, fieldDisp int64, storageDisp int64) {
	tmp := asm.MustLookup("r10")
	if dstBase.Name == tmp.Name {
		tmp = scratchRegs[0]
	}
	if dstBase.Name == tmp.Name {
		tmp = asm.MustLookup("r11")
	}
	emitRegRegMove(e, tmp, dstBase)
	if storageDisp != 0 {
		e.emitInstruction(asm.Instruction{Mnemonic: "add", Operands: []asm.Operand{
			asm.RegOperand{Reg: tmp},
			asm.ImmOperand{Value: storageDisp},
		}})
	}
	emitStoreMemFromReg(e, dstBase, fieldDisp, tmp, 64)
}

func emitCopyMemoryToStackRange(e *Emitter, srcBase asm.Reg, srcDisp int64, dstSlot int, size int) {
	for offset := 0; offset < size; {
		width := copyWidth(size - offset)
		emitLoadMemToReg(e, scratchRegs[0], srcBase, srcDisp+int64(offset), width)
		emitStoreSlotFromReg(e, scratchRegs[0], dstSlot+offset, width)
		offset += width / 8
	}
}

func emitCopyMemoryToMemoryRange(e *Emitter, srcBase asm.Reg, srcDisp int64, dstBase asm.Reg, dstDisp int64, size int) {
	for offset := 0; offset < size; {
		width := copyWidth(size - offset)
		emitLoadMemToReg(e, scratchRegs[0], srcBase, srcDisp+int64(offset), width)
		emitStoreMemFromReg(e, dstBase, dstDisp+int64(offset), scratchRegs[0], width)
		offset += width / 8
	}
}

func emitAddressOfValue(e *Emitter, frame Frame, value ir.Value, dst asm.Reg) {
	base, disp, ok := emitValueAddress(e, frame, value)
	if !ok {
		return
	}
	if base.Name == "rbp" {
		emitSlotFromBase(e, dst, asm.MustLookup("rbp"), int(disp))
		return
	}
	emitRegRegMove(e, dst, base)
	if disp != 0 {
		e.emitInstruction(asm.Instruction{Mnemonic: "add", Operands: []asm.Operand{
			asm.RegOperand{Reg: dst},
			asm.ImmOperand{Value: disp},
		}})
	}
}

func emitValueAddress(e *Emitter, frame Frame, value ir.Value) (asm.Reg, int64, bool) {
	slot, ok := frame.Slots[value]
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "missing value slot"})
		return asm.Reg{}, 0, false
	}
	if e.ctx.isHandleType(valueTypeName(value)) {
		emitLoadSlotToReg(e, asm.MustLookup("r11"), slot, 64)
		return asm.MustLookup("r11"), 0, true
	}
	return asm.MustLookup("rbp"), int64(slot), true
}

func emitObjectAddress(e *Emitter, frame Frame, object ir.Value, offset int) (asm.Reg, int64, bool) {
	base, disp, ok := emitValueAddress(e, frame, object)
	if !ok {
		return asm.Reg{}, 0, false
	}
	return base, disp + int64(offset), true
}

func emitMovDataAddressToReg(e *Emitter, reg asm.Reg, symbol string) {
	if reg.Name != "rax" {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "data address loads require rax"})
		return
	}
	e.emit(0x48, 0xB8)
	e.DataReloc = append(e.DataReloc, dataReloc{Offset: uint64(len(e.Code)), Symbol: symbol})
	e.emit(0, 0, 0, 0, 0, 0, 0, 0)
}

func emitLoadMemToReg(e *Emitter, reg asm.Reg, base asm.Reg, disp int64, width int) {
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: registerForWidth(reg, width)},
		asm.MemOperand{Base: base, Disp: disp, Width: width},
	}})
}

func emitCmpRegMem(e *Emitter, reg asm.Reg, base asm.Reg, disp int64, width int) {
	e.emitInstruction(asm.Instruction{Mnemonic: "cmp", Operands: []asm.Operand{
		asm.RegOperand{Reg: registerForWidth(reg, width)},
		asm.MemOperand{Base: base, Disp: disp, Width: width},
	}})
}

func emitStoreMemFromReg(e *Emitter, base asm.Reg, disp int64, reg asm.Reg, width int) {
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.MemOperand{Base: base, Disp: disp, Width: width},
		asm.RegOperand{Reg: registerForWidth(reg, width)},
	}})
}

func copyWidth(remainingBytes int) int {
	switch {
	case remainingBytes >= 8:
		return 64
	case remainingBytes >= 4:
		return 32
	case remainingBytes >= 2:
		return 16
	default:
		return 8
	}
}

func valueTypeName(value ir.Value) string {
	return valueType(value).Name
}

func valueType(value ir.Value) ir.Type {
	switch v := value.(type) {
	case *ir.Param:
		return v.Type
	case *ir.Local:
		return v.Type
	case *ir.ConstInt:
		return v.Type
	case *ir.Binary:
		return v.Type
	case *ir.Call:
		return v.Type
	case *ir.FieldLoad:
		return v.Type
	case *ir.Construct:
		return v.Type
	case *ir.EnumConstruct:
		return v.Type
	case *ir.EnumVariantTest:
		return ir.Type{Name: "Bool", Kind: ir.TypeKindPrimitive}
	case *ir.EnumPayloadExtract:
		return v.Type
	case *ir.StringLiteral:
		return v.Type
	case *ir.FrameBegin:
		return v.Type
	case *ir.ArenaReserve:
		return v.Type
	case *ir.ArenaReserveArray:
		return v.Type
	case *ir.ArenaPlace:
		return v.Type
	case *ir.SliceGet:
		return v.Type
	case *ir.ReliableTopicTryPublish:
		return v.Type
	case ir.ReliableTopicTryPublish:
		return v.Type
	case *ir.TopicTryNext:
		return v.Type
	case ir.TopicTryNext:
		return v.Type
	case *ir.TopicIsWaitArmed:
		return v.Type
	case ir.TopicIsWaitArmed:
		return v.Type
	case *ir.VcpuStart:
		return v.Type
	case ir.VcpuStart:
		return v.Type
	default:
		return ir.Type{}
	}
}

func isComparisonOp(op string) bool {
	switch op {
	case "eq", "ne", "lt", "le", "gt", "ge":
		return true
	default:
		return false
	}
}

func emitLoadSlotToReg(e *Emitter, reg asm.Reg, slot int, width int) {
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: registerForWidth(reg, width)},
		asm.MemOperand{Base: asm.MustLookup("rbp"), Disp: int64(slot), Width: width},
	}})
}

func emitLoadSlotOffsetToReg(e *Emitter, reg asm.Reg, slot int, offset int, width int) {
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: registerForWidth(reg, width)},
		asm.MemOperand{Base: asm.MustLookup("rbp"), Disp: int64(slot + offset), Width: width},
	}})
}

func emitStoreSlotFromReg(e *Emitter, reg asm.Reg, slot int, width int) {
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.MemOperand{Base: asm.MustLookup("rbp"), Disp: int64(slot), Width: width},
		asm.RegOperand{Reg: registerForWidth(reg, width)},
	}})
}

func emitStoreSlotImm(e *Emitter, slot int, value int64, width int) {
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.MemOperand{Base: asm.MustLookup("rbp"), Disp: int64(slot), Width: width},
		asm.ImmOperand{Value: value},
	}})
}

func emitPushReg(e *Emitter, reg asm.Reg) {
	e.emitInstruction(asm.Instruction{Mnemonic: "push", Operands: []asm.Operand{
		asm.RegOperand{Reg: reg},
	}})
}

func emitPopReg(e *Emitter, reg asm.Reg) {
	e.emitInstruction(asm.Instruction{Mnemonic: "pop", Operands: []asm.Operand{
		asm.RegOperand{Reg: reg},
	}})
}

func emitMovImmToReg(e *Emitter, reg asm.Reg, value int64) {
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: reg},
		asm.ImmOperand{Value: value},
	}})
}

func emitRegRegMove(e *Emitter, dst asm.Reg, src asm.Reg) {
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: dst},
		asm.RegOperand{Reg: src},
	}})
}

func emitNegReg(e *Emitter, reg asm.Reg) {
	rex := byte(0x48)
	if reg.High {
		rex |= 0x01
	}
	e.emit(rex, 0xF7, encodeModRM(3, 3, reg.Low3))
}

func registerForWidth(reg asm.Reg, width int) asm.Reg {
	switch width {
	case 8:
		if alias, ok := asm.Lookup(registerAlias(reg.Name, 8)); ok {
			return alias
		}
	case 16:
		if alias, ok := asm.Lookup(registerAlias(reg.Name, 16)); ok {
			return alias
		}
	case 32:
		if alias, ok := asm.Lookup(registerAlias(reg.Name, 32)); ok {
			return alias
		}
	}
	return reg
}

func emitCmpRegReg(e *Emitter, left asm.Reg, right asm.Reg) {
	emitRegRegOp(e, 0x39, left, right)
}

func emitCmpRegImm(e *Emitter, left asm.Reg, value int64) {
	e.emitInstruction(asm.Instruction{Mnemonic: "cmp", Operands: []asm.Operand{
		asm.RegOperand{Reg: left},
		asm.ImmOperand{Value: value},
	}})
}

func emitUnsignedMulInto(e *Emitter, dst asm.Reg, rhs asm.Reg) {
	rax := asm.MustLookup("rax")
	rdx := asm.MustLookup("rdx")
	emitRegRegMove(e, rax, dst)
	emitUnsignedMulReg(e, rhs)
	ok := e.newLabel("mul_no_overflow")
	emitMovImmToReg(e, rhs, 0)
	emitCmpRegReg(e, rdx, rhs)
	e.emitJcc(0x84, ok)
	emitCallReloc(e, "_wrela_memory_oom")
	e.bindLabel(ok)
	emitRegRegMove(e, dst, rax)
}

func emitUnsignedMulReg(e *Emitter, rhs asm.Reg) {
	rex := byte(0x48)
	if rhs.High {
		rex |= 0x01
	}
	e.emit(rex, 0xF7, encodeModRM(3, 4, rhs.Low3))
}

func emitRegRegOp(e *Emitter, opcode byte, left asm.Reg, right asm.Reg) {
	rex := byte(0x48)
	if right.High {
		rex |= 0x04
	}
	if left.High {
		rex |= 0x01
	}
	e.emit(rex, opcode, encodeModRM(3, right.Low3, left.Low3))
}

func emitRegRegIMul(e *Emitter, dst asm.Reg, src asm.Reg) {
	rex := byte(0x48)
	if dst.High {
		rex |= 0x04
	}
	if src.High {
		rex |= 0x01
	}
	e.emit(rex, 0x0F, 0xAF, encodeModRM(3, dst.Low3, src.Low3))
}

func emitSetccAl(e *Emitter, opcode byte) {
	e.emit(0x0F, opcode, 0xC0)
}

func setccOpcode(op string) byte {
	switch op {
	case "eq":
		return 0x94
	case "ne":
		return 0x95
	case "lt":
		return 0x9C
	case "le":
		return 0x9E
	case "gt":
		return 0x9F
	case "ge":
		return 0x9D
	default:
		return 0x94
	}
}

func negateConditionOpcode(op string) byte {
	switch op {
	case "eq":
		return 0x85
	case "ne":
		return 0x84
	case "lt":
		return 0x8D
	case "le":
		return 0x8F
	case "gt":
		return 0x8E
	case "ge":
		return 0x8C
	default:
		return 0x85
	}
}

func encodeModRM(mod, reg, rm int) byte {
	return byte((mod << 6) | (reg << 3) | rm)
}

func alignUpLen(value, align uint64) uint64 {
	if align == 0 {
		return value
	}
	return (value + align - 1) &^ (align - 1)
}

func enumVariantInfo(info ir.TypeInfo, name string) (ir.EnumVariantInfo, bool) {
	for _, variant := range info.EnumVariants {
		if variant.Name == name {
			return variant, true
		}
	}
	return ir.EnumVariantInfo{}, false
}

func fieldStorageOffset(field ir.FieldInfo) int {
	if field.StorageOffset >= 0 {
		return field.StorageOffset
	}
	return field.Offset
}

func fieldStorageSize(ctx compileContext, field ir.FieldInfo) int {
	if field.StorageSize > 0 {
		return field.StorageSize
	}
	if field.Size > 0 {
		return field.Size
	}
	return ctx.storageSizeForType(field.Type)
}

func fieldStorageWidthBits(ctx compileContext, field ir.FieldInfo) int {
	switch fieldStorageSize(ctx, field) {
	case 1:
		return 8
	case 2:
		return 16
	case 4:
		return 32
	default:
		return 64
	}
}
