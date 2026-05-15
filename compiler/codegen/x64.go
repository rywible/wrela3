package codegen

import (
	"encoding/binary"
	"fmt"

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
	types map[string]ir.TypeInfo
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

	ctx := compileContext{types: program.Types}
	if ctx.types == nil {
		ctx.types = map[string]ir.TypeInfo{}
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
	units = append(units, compileInterruptDispatchUnits(program)...)
	if program.Entry.Symbol != "" {
		unit, ds := compileEntryAdapterUnit(program.Entry, ctx)
		if len(ds) != 0 {
			return nil, ds
		}
		units = append(units, unit)
	}
	units = append(units, compileMemoryTrapUnit())

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
	if len(program.WritableData) > 0 || len(program.InterruptContexts) > 0 || len(program.InterruptBindings) > 0 {
		section, offsets := buildData(program)
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
		})
	}
	return out
}

func compileInterruptDispatchUnits(program *ir.Program) []compiledUnit {
	units := make([]compiledUnit, 0, len(program.InterruptBindings))
	for _, binding := range program.InterruptBindings {
		symbol := interruptVectorSymbol(binding.Vector)
		if symbol == "" {
			continue
		}
		units = append(units, buildInterruptDispatchUnit(symbol, binding))
	}
	return units
}

func interruptVectorSymbol(vector uint8) string {
	switch vector {
	case 0x40:
		return "_wrela_interrupt_vector40_serial"
	case 0x41:
		return "_wrela_interrupt_vector41_edu_msi"
	case 0x42:
		return "_wrela_interrupt_vector42_ivshmem_msix"
	default:
		return ""
	}
}

func buildInterruptDispatchUnit(symbol string, binding ir.InterruptBinding) compiledUnit {
	e := &Emitter{Labels: map[string]int{}}
	for _, reg := range []string{"rax", "rcx", "rdx", "rbx", "rbp", "rsi", "rdi", "r8", "r9", "r10", "r11", "r12", "r13", "r14", "r15"} {
		e.emitInstruction(asm.Instruction{Mnemonic: "push", Operands: []asm.Operand{asm.RegOperand{Reg: asm.MustLookup(reg)}}})
	}
	emitMovDataAddressToReg(e, asm.MustLookup("rax"), binding.ContextSymbol)
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("rdi")},
		asm.MemOperand{Base: asm.MustLookup("rax"), Disp: int64(binding.PathFieldOffset), Width: 64},
	}})
	emitMovDataAddressToReg(e, asm.MustLookup("rax"), binding.EventStorageSymbol)
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("r10")},
		asm.RegOperand{Reg: asm.MustLookup("rax")},
	}})
	emitCallReloc(e, binding.EventFunctionSymbol)
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("rsi")},
		asm.RegOperand{Reg: asm.MustLookup("rax")},
	}})
	emitMovDataAddressToReg(e, asm.MustLookup("rax"), binding.ContextSymbol)
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("rdi")},
		asm.RegOperand{Reg: asm.MustLookup("rax")},
	}})
	emitCallReloc(e, binding.HandlerFunctionSymbol)
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("r11")},
		asm.ImmOperand{Value: 0xFEE00000},
	}})
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: asm.MustLookup("eax")},
		asm.ImmOperand{Value: 0},
	}})
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.MemOperand{Base: asm.MustLookup("r11"), Disp: 0xB0, Width: 32},
		asm.RegOperand{Reg: asm.MustLookup("eax")},
	}})
	for _, reg := range []string{"r15", "r14", "r13", "r12", "r11", "r10", "r9", "r8", "rdi", "rsi", "rbp", "rbx", "rdx", "rcx", "rax"} {
		e.emitInstruction(asm.Instruction{Mnemonic: "pop", Operands: []asm.Operand{asm.RegOperand{Reg: asm.MustLookup(reg)}}})
	}
	e.emitInstruction(asm.Instruction{Mnemonic: "iretq"})
	return compiledUnit{Symbol: symbol, Bytes: e.Code, CallReloc: e.CallReloc, DataReloc: e.DataReloc}
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

type builtDataSection struct {
	Section Section
	Offsets map[string]uint64
}

func buildData(program *ir.Program) (Section, map[string]uint64) {
	writable := append([]ir.DataObject{}, program.WritableData...)
	writable = append(writable, interruptRuntimeData(program)...)
	return buildDataSection(".data", writable, 0xC0000040)
}

func interruptRuntimeData(program *ir.Program) []ir.DataObject {
	out := make([]ir.DataObject, 0, len(program.InterruptContexts)+len(program.InterruptBindings))
	for _, context := range program.InterruptContexts {
		out = append(out, ir.DataObject{
			Symbol: context.Symbol,
			Bytes:  make([]byte, context.Size),
		})
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
			case *ir.FrameBegin:
				emitFrameBegin(e, v, frame)
			case *ir.ArenaReserve:
				emitArenaReserve(e, v, frame)
			case *ir.FrameEnd:
				emitFrameEnd(e, v, frame)
			case *ir.Copy:
				emitCopy(e, v, frame)
			case *ir.Binary:
				emitBinary(e, v, frame)
			case *ir.Call:
				emitCall(e, v, frame)
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
	return compiledUnit{Symbol: entry.Symbol, Bytes: e.Code, CallReloc: e.CallReloc}, nil
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

func needsObjectSlot(ctx compileContext, value ir.Value) bool {
	switch v := value.(type) {
	case *ir.Construct, *ir.StringLiteral:
		return true
	case *ir.FrameBegin, *ir.ArenaReserve, *ir.ArenaPlace:
		return true
	case *ir.Call:
		return ctx.shouldPassRecordReturn(v.Type)
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
	case *ir.StringLiteral:
		return ctx.representationSize(v.Type.Name)
	case *ir.FrameBegin:
		return ctx.representationSize(v.Type.Name)
	case *ir.ArenaReserve:
		return ctx.representationSize(v.Type.Name)
	case *ir.ArenaPlace:
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
	case ir.TypeKindData, ir.TypeKindClass, ir.TypeKindDriver, ir.TypeKindDriverPath, ir.TypeKindExecutor:
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
		return info.Kind == ir.TypeKindData
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
		emitLoadSlotToReg(e, dst, slot, valueWidthBits(e.ctx, value))
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
		case *ir.FrameBegin:
			emitFrameBegin(e, v, frame)
		case *ir.ArenaReserve:
			emitArenaReserve(e, v, frame)
		case *ir.FrameEnd:
			emitFrameEnd(e, v, frame)
		case *ir.Copy:
			emitCopy(e, v, frame)
		case *ir.Binary:
			emitBinary(e, v, frame)
		case *ir.Call:
			emitCall(e, v, frame)
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
		emitCopyValueToStackRange(e, frame, field.Value, objectSlot+fieldInfo.Offset, fieldInfo.Size)
	}
	emitSlotFromBase(e, scratchRegs[0], asm.MustLookup("rbp"), objectSlot)
	emitStoreSlotFromReg(e, scratchRegs[0], slot, 64)
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
	emitLoadMemToReg(e, limit, parent, 8, 64)
	emitTrapIfAbove(e, end, limit)

	emitLoadMemToReg(e, base, parent, 0, 64)
	emitRegRegOp(e, 0x01, base, saved)
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
	next := asm.MustLookup("rax")
	length := asm.MustLookup("r10")
	end := asm.MustLookup("r11")
	limit := asm.MustLookup("rcx")
	align := asm.MustLookup("r9")
	base := asm.MustLookup("rsi")

	if !emitValueAddressToReg(e, frame, op.Arena, arena) {
		return
	}
	emitLoadMemToReg(e, next, arena, 16, 64)
	emitLoadValue(e, frame, op.Align, align)
	emitTrapIfZero(e, align)
	emitRegRegMove(e, end, align)
	e.emitInstruction(asm.Instruction{Mnemonic: "sub", Operands: []asm.Operand{
		asm.RegOperand{Reg: end},
		asm.ImmOperand{Value: 1},
	}})
	emitRegRegOp(e, 0x01, next, end)
	emitNegReg(e, align)
	emitRegRegOp(e, 0x21, next, align)
	emitLoadValue(e, frame, op.Length, length)
	emitRegRegMove(e, end, next)
	emitRegRegOp(e, 0x01, end, length)
	emitLoadMemToReg(e, limit, arena, 8, 64)
	emitTrapIfAbove(e, end, limit)

	emitLoadMemToReg(e, base, arena, 0, 64)
	emitRegRegOp(e, 0x01, base, next)
	emitStoreSlotFromReg(e, base, objectSlot, 64)
	emitStoreSlotFromReg(e, length, objectSlot+8, 64)
	emitStoreMemFromReg(e, arena, 16, end, 64)
	emitSlotFromBase(e, next, asm.MustLookup("rbp"), objectSlot)
	emitStoreSlotFromReg(e, next, slot, 64)
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

func emitTrapIfZero(e *Emitter, value asm.Reg) {
	ok := e.newLabel("arena_nonzero_ok")
	zero := asm.MustLookup("rcx")
	emitMovImmToReg(e, zero, 0)
	emitCmpRegReg(e, value, zero)
	e.emitJcc(0x85, ok)
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
	base, disp, ok := emitObjectAddress(e, frame, op.Object, op.Offset)
	if !ok {
		return
	}
	size := e.ctx.representationSize(op.Type.Name)
	emitCopyMemoryToStackRange(e, base, disp, outSlot, size)
}

func emitFieldStore(e *Emitter, op *ir.FieldStore, frame Frame) {
	base, disp, ok := emitObjectAddress(e, frame, op.Object, op.Offset)
	if !ok {
		return
	}
	size := e.ctx.representationSize(op.Type.Name)
	emitCopyValueToMemoryRange(e, frame, op.Value, base, disp, size)
}

func emitInterruptContextStore(e *Emitter, frame Frame, store *ir.InterruptContextStore) {
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
	emitCopyBytes(e, asm.MustLookup("rdi"), 0, srcBase, srcDisp, store.Size)
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
	emitCopyMemoryToMemoryRange(e, srcBase, srcDisp, dstBase, dstDisp, e.ctx.objectSize(typeName))
	info, ok := e.ctx.types[typeName]
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
		emitDeepCopyObjectToAddress(e, field.Type.Name, asm.MustLookup("r11"), 0, asm.MustLookup("r10"), 0)
		emitPopReg(e, dstBase)
		emitPopReg(e, srcBase)
	}
}

func emitStoreNestedDestinationHandle(e *Emitter, dstBase asm.Reg, fieldDisp int64, storageDisp int64) {
	emitRegRegMove(e, scratchRegs[0], dstBase)
	if storageDisp != 0 {
		e.emitInstruction(asm.Instruction{Mnemonic: "add", Operands: []asm.Operand{
			asm.RegOperand{Reg: scratchRegs[0]},
			asm.ImmOperand{Value: storageDisp},
		}})
	}
	emitStoreMemFromReg(e, dstBase, fieldDisp, scratchRegs[0], 64)
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
	case *ir.StringLiteral:
		return v.Type
	case *ir.FrameBegin:
		return v.Type
	case *ir.ArenaReserve:
		return v.Type
	case *ir.ArenaPlace:
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
