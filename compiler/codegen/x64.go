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
	Slots map[ir.Value]int
	Size  int
}

type internalReloc struct {
	Offset uint64
	Symbol string
}

type compiledUnit struct {
	Symbol    string
	Bytes     []byte
	CallReloc []internalReloc
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
	Jumps     []jumpFixup
	Diags     []diag.Diagnostic
}

func Compile(program *ir.Program) (*Image, []diag.Diagnostic) {
	if program == nil {
		return nil, []diag.Diagnostic{{
			Phase:   diagnosticPhase,
			Code:    diag.CG0001,
			Message: "nil program",
		}}
	}

	units := make([]compiledUnit, 0, len(program.Functions)+len(program.AsmMethods)+1)
	for _, fn := range program.Functions {
		unit, ds := compileFunction(fn)
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
	if program.Entry.Symbol != "" {
		unit, ds := compileEntryAdapterUnit(program.Entry)
		if len(ds) != 0 {
			return nil, ds
		}
		units = append(units, unit)
	}

	sections := []Section{{Name: ".text", Data: nil, Characteristics: 0x60000020}}
	if len(program.Data) > 0 {
		sections = append(sections, Section{Name: ".rdata", Data: buildRData(program), Characteristics: 0x40000040})
	}

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
	}

	if len(sections) > 1 {
		alignedSize := alignUpLen(uint64(len(sections[0].Data)), 0x1000)
		sections[0].Data = append(sections[0].Data, make([]byte, alignedSize-uint64(len(sections[0].Data)))...)
		sections[1].RVA = 0x1000 + uint64(len(sections[0].Data))
	}

	return &Image{
		EntrySymbol: program.Entry.Symbol,
		Sections:    sections,
		Symbols:     symbols,
	}, nil
}

func buildRData(program *ir.Program) []byte {
	out := []byte{}
	for _, obj := range program.Data {
		out = append(out, obj.Bytes...)
	}
	return out
}

func compileFunction(fn ir.Function) (compiledUnit, []diag.Diagnostic) {
	frame := buildFrame(fn)
	e := &Emitter{Labels: map[string]int{}}

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
	return compiledUnit{Symbol: fn.Symbol, Bytes: e.Code, CallReloc: e.CallReloc}, nil
}

func compileAsmMethodUnit(method ir.AsmMethod) (compiledUnit, []diag.Diagnostic) {
	_, diags, code := lowerAndEncodeAsmMethod(method)
	if len(diags) != 0 {
		for i := range diags {
			if method.Symbol != "" {
				diags[i].Message = method.Symbol + ": " + diags[i].Message
			}
		}
		return compiledUnit{}, diags
	}
	return compiledUnit{Symbol: method.Symbol, Bytes: code}, nil
}

func compileEntryAdapterUnit(entry ir.EntryAdapter) (compiledUnit, []diag.Diagnostic) {
	e := &Emitter{Labels: map[string]int{}}
	emitEntryAdapter(e, entry)
	e.resolveJumps()
	if len(e.Diags) != 0 {
		return compiledUnit{}, e.Diags
	}
	return compiledUnit{Symbol: entry.Symbol, Bytes: e.Code, CallReloc: e.CallReloc}, nil
}

func buildFrame(fn ir.Function) Frame {
	offset := 0
	slots := map[ir.Value]int{}
	for _, p := range fn.Params {
		size := valueSize(p)
		offset += size
		slots[p] = -offset
	}
	for _, v := range fn.ValuesInDeterministicOrder() {
		if _, ok := slots[v]; ok {
			continue
		}
		size := valueSize(v)
		offset += size
		slots[v] = -offset
	}
	return Frame{Slots: slots, Size: layout.AlignUp(offset, 16)}
}

func valueSize(value ir.Value) int {
	switch v := value.(type) {
	case *ir.Param:
		return maxValueSize(v.Type.Name)
	case *ir.ConstInt:
		return maxValueSize(v.Type.Name)
	case *ir.Binary:
		return maxValueSize(v.Type.Name)
	case *ir.Call:
		return maxValueSize(v.Type.Name)
	case *ir.FieldLoad:
		return maxValueSize(v.Type.Name)
	default:
		return 8
	}
}

func maxValueSize(name string) int {
	switch name {
	case "U8", "I8", "Bool":
		return 1
	case "U16", "I16":
		return 2
	case "U32", "I32":
		return 4
	case "Bytes", "MutableBytes", "StringLiteral", "data", "class", "driver", "path", "executor":
		return 16
	default:
		s, _, err := layout.SizeAlign(name)
		if err != nil {
			return 8
		}
		return s
	}
}

func valueWidthBits(value ir.Value) int {
	size := valueSize(value)
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
	for i, p := range params {
		if i >= len(argRegs) {
			break
		}
		slot, ok := frame.Slots[p]
		if !ok {
			continue
		}
		emitStoreSlotFromReg(e, argRegs[i], slot, valueWidthBits(p))
	}
}

func emitEpilogue(e *Emitter) {
	e.emit(0x48, 0x89, 0xEC)
	e.emit(0x5D)
	e.emit(0xC3)
}

func emitConst(e *Emitter, c *ir.ConstInt, frame Frame) {
	slot, ok := frame.Slots[c]
	if !ok {
		return
	}
	emitMovImmToReg(e, scratchRegs[0], int64(c.Value))
	emitStoreSlotFromReg(e, scratchRegs[0], slot, valueWidthBits(c))
}

func emitBinary(e *Emitter, op *ir.Binary, frame Frame) {
	emitLoadValue(e, frame, op.Left, scratchRegs[0])
	emitLoadValue(e, frame, op.Right, scratchRegs[1])
	if op.Op == "add" {
		emitRegRegOp(e, 0x01, scratchRegs[0], scratchRegs[1])
	} else if op.Op == "sub" {
		emitRegRegOp(e, 0x29, scratchRegs[0], scratchRegs[1])
	} else {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "unsupported binary op: " + op.Op})
		return
	}

	slot, ok := frame.Slots[op]
	if !ok {
		return
	}
	emitStoreSlotFromReg(e, scratchRegs[0], slot, valueWidthBits(op))
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

	for i, value := range values {
		emitLoadValue(e, frame, value, scratchRegs[0])
		emitRegRegMove(e, argRegs[i], scratchRegs[0])
	}

	e.emit(0xE8, 0, 0, 0, 0)
	rel := uint64(len(e.Code) - 4)
	e.CallReloc = append(e.CallReloc, internalReloc{Offset: rel, Symbol: call.Symbol})

	if slot, ok := frame.Slots[call]; ok {
		emitStoreSlotFromReg(e, scratchRegs[0], slot, valueWidthBits(call))
	}
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
		emitLoadSlotToReg(e, dst, slot, valueWidthBits(value))
	}
}

func emitReturn(e *Emitter, r *ir.Return, frame Frame) {
	if r.Value != nil {
		emitLoadValue(e, frame, r.Value, scratchRegs[0])
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
	emitConditionJump(e, cond.Condition, elseLabel, frame)
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
	emitConditionJump(e, wh.Condition, done, frame)
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
	iterSlot, ok := frame.Slots[loop.Iterable]
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "missing forbytes iterable slot"})
		return
	}
	byteSlot, ok := frame.Slots[loop.ByteValue]
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "missing forbytes byte slot"})
		return
	}

	emitMovImmToReg(e, scratchRegs[0], 0)
	emitStoreSlotFromReg(e, scratchRegs[0], indexSlot, valueWidthBits(loop.Index))

	start := e.newLabel("for_bytes_start")
	done := e.newLabel("for_bytes_done")
	e.bindLabel(start)

	emitLoadSlotOffsetToReg(e, scratchRegs[1], iterSlot, 8, 64) // length
	emitLoadSlotToReg(e, scratchRegs[0], indexSlot, 64)
	emitCmpRegReg(e, scratchRegs[0], scratchRegs[1])
	e.emitJcc(0x8D, done)

	emitLoadSlotToReg(e, asm.MustLookup("rdi"), iterSlot, 64) // bytes.address
	emitLoadSlotToReg(e, scratchRegs[1], indexSlot, 64)
	e.emit(0x48, 0x0F, 0xB6, 0x04, 0x0F)
	emitStoreSlotFromReg(e, scratchRegs[0], byteSlot, valueWidthBits(loop.ByteValue))

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
		}
	}
}

func emitConditionJump(e *Emitter, cond ir.Value, falseTarget string, frame Frame) {
	switch c := cond.(type) {
	case *ir.ConstInt:
		if c.Value == 0 {
			e.emitJmp(falseTarget)
		}
	case *ir.Binary:
		emitLoadValue(e, frame, c.Left, scratchRegs[0])
		emitLoadValue(e, frame, c.Right, scratchRegs[1])
		emitCmpRegReg(e, scratchRegs[0], scratchRegs[1])
		e.emitJcc(negateConditionOpcode(c.Op), falseTarget)
	default:
		emitLoadValue(e, frame, cond, scratchRegs[0])
		emitMovImmToReg(e, scratchRegs[1], 0)
		emitCmpRegReg(e, scratchRegs[0], scratchRegs[1])
		e.emitJcc(0x84, falseTarget)
	}
}

func emitFieldLoad(e *Emitter, op *ir.FieldLoad, frame Frame) {
	baseSlot, ok := frame.Slots[op.Object]
	if !ok {
		e.Diags = append(e.Diags, diag.Diagnostic{Phase: diagnosticPhase, Code: diag.CG0001, Message: "missing object slot for field load"})
		return
	}
	emitLoadSlotToReg(e, asm.MustLookup("rdi"), baseSlot, 64)
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: scratchRegs[0]},
		asm.MemOperand{Base: asm.MustLookup("rdi"), Disp: int64(op.Offset), Width: valueWidthBitsFromType(op.Type.Name)},
	}})
	outSlot, ok := frame.Slots[op]
	if !ok {
		return
	}
	emitStoreSlotFromReg(e, scratchRegs[0], outSlot, valueWidthBitsFromType(op.Type.Name))
}

func emitLoadSlotToReg(e *Emitter, reg asm.Reg, slot int, width int) {
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: reg},
		asm.MemOperand{Base: asm.MustLookup("rbp"), Disp: int64(slot), Width: width},
	}})
}

func emitLoadSlotOffsetToReg(e *Emitter, reg asm.Reg, slot int, offset int, width int) {
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.RegOperand{Reg: reg},
		asm.MemOperand{Base: asm.MustLookup("rbp"), Disp: int64(slot + offset), Width: width},
	}})
}

func emitStoreSlotFromReg(e *Emitter, reg asm.Reg, slot int, width int) {
	e.emitInstruction(asm.Instruction{Mnemonic: "mov", Operands: []asm.Operand{
		asm.MemOperand{Base: asm.MustLookup("rbp"), Disp: int64(slot), Width: width},
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

func emitCmpRegReg(e *Emitter, left asm.Reg, right asm.Reg) {
	emitRegRegOp(e, 0x39, left, right)
}

func emitRegRegOp(e *Emitter, opcode byte, left asm.Reg, right asm.Reg) {
	rex := byte(0x48)
	if right.High {
		rex |= 0x01
	}
	if left.High {
		rex |= 0x04
	}
	e.emit(rex, opcode, encodeModRM(3, right.Low3, left.Low3))
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
