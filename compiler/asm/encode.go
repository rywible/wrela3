package asm

import (
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/ryanwible/wrela3/compiler/diag"
)

type branchFixup struct {
	relPos int
	target string
	nextPC int
}

var condOpcode = map[string]byte{
	"je":  0x84,
	"jne": 0x85,
	"jl":  0x8c,
	"jle": 0x8e,
	"jg":  0x8f,
	"jge": 0x8d,
}

func Encode(instructions []Instruction) ([]byte, []diag.Diagnostic) {
	var out []byte
	var diags []diag.Diagnostic
	labels := map[string]int{}
	var fixups []branchFixup

	for _, ins := range instructions {
		if ins.Label != "" {
			labels[ins.Label] = len(out)
		}

		start := len(out)
		switch strings.ToLower(ins.Mnemonic) {
		case "":
		case "hlt":
			out = append(out, 0xF4)
		case "pause":
			out = append(out, 0xF3, 0x90)
		case "ret":
			out = append(out, 0xC3)
		case "retfq":
			out = append(out, 0x48, 0xCB)
		case "cli":
			out = append(out, 0xFA)
		case "sti":
			out = append(out, 0xFB)
		case "out":
			b, ok := encodeOut(ins)
			if ok {
				out = append(out, b...)
				break
			}
			diags = append(diags, diag.Diagnostic{
				Phase:   "asm",
				Code:    diag.ASM0002,
				Message: "unsupported out form",
			})
		case "in":
			b, ok := encodeIn(ins)
			if ok {
				out = append(out, b...)
				break
			}
			diags = append(diags, diag.Diagnostic{
				Phase:   "asm",
				Code:    diag.ASM0002,
				Message: "unsupported in form",
			})
		case "mov":
			b, ok := encodeMov(ins)
			if ok {
				out = append(out, b...)
				break
			}
			diags = append(diags, diag.Diagnostic{
				Phase:   "asm",
				Code:    diag.ASM0002,
				Message: "unsupported mov form",
			})
		case "add":
			if b, ok := encodeBinaryRegReg(ins, 0x01); ok {
				out = append(out, b...)
				break
			}
			b, ok := encodeBinaryImm(ins, 0, 0x83, 0x81)
			if ok {
				out = append(out, b...)
				break
			}
			diags = append(diags, diag.Diagnostic{
				Phase:   "asm",
				Code:    diag.ASM0002,
				Message: "unsupported add form",
			})
		case "cmp":
			b, ok := encodeBinaryImm(ins, 7, 0x83, 0x81)
			if ok {
				out = append(out, b...)
				break
			}
			diags = append(diags, diag.Diagnostic{
				Phase:   "asm",
				Code:    diag.ASM0002,
				Message: "unsupported cmp form",
			})
		case "call":
			b, ok := encodeCall(ins, start, &fixups)
			if ok {
				out = append(out, b...)
				break
			}
			diags = append(diags, diag.Diagnostic{
				Phase:   "asm",
				Code:    diag.ASM0002,
				Message: "unsupported call form",
			})
		case "jmp":
			b, ok := encodeJmp(ins, start, &fixups)
			if ok {
				out = append(out, b...)
				break
			}
			diags = append(diags, diag.Diagnostic{
				Phase:   "asm",
				Code:    diag.ASM0002,
				Message: "unsupported jmp form",
			})
		case "je", "jne", "jl", "jle", "jg", "jge":
			b, ok := encodeCondJmp(ins, start, &fixups)
			if ok {
				out = append(out, b...)
				break
			}
			diags = append(diags, diag.Diagnostic{
				Phase:   "asm",
				Code:    diag.ASM0002,
				Message: "unsupported conditional branch form",
			})
		case "push":
			b, ok := encodePushPop(ins, true)
			if ok {
				out = append(out, b...)
				break
			}
			diags = append(diags, diag.Diagnostic{
				Phase:   "asm",
				Code:    diag.ASM0002,
				Message: "unsupported push form",
			})
		case "pop":
			b, ok := encodePushPop(ins, false)
			if ok {
				out = append(out, b...)
				break
			}
			diags = append(diags, diag.Diagnostic{
				Phase:   "asm",
				Code:    diag.ASM0002,
				Message: "unsupported pop form",
			})
		case "lgdt":
			b, ok := encodeDesc(ins, 2)
			if ok {
				out = append(out, b...)
				break
			}
			diags = append(diags, diag.Diagnostic{
				Phase:   "asm",
				Code:    diag.ASM0002,
				Message: "unsupported lgdt form",
			})
		case "lidt":
			b, ok := encodeDesc(ins, 3)
			if ok {
				out = append(out, b...)
				break
			}
			diags = append(diags, diag.Diagnostic{
				Phase:   "asm",
				Code:    diag.ASM0002,
				Message: "unsupported lidt form",
			})
		default:
			diags = append(diags, diag.Diagnostic{
				Phase:   "asm",
				Code:    diag.ASM0001,
				Message: "unknown instruction: " + ins.Mnemonic,
			})
		}
		_ = start
	}

	for _, fix := range fixups {
		target, ok := labels[fix.target]
		if !ok {
			diags = append(diags, diag.Diagnostic{
				Phase:   "asm",
				Code:    diag.ASM0002,
				Message: "unknown label: " + fix.target,
			})
			continue
		}
		rel := target - fix.nextPC
		if rel < -0x80000000 || rel > 0x7fffffff {
			diags = append(diags, diag.Diagnostic{
				Phase:   "asm",
				Code:    diag.ASM0002,
				Message: fmt.Sprintf("branch out of range: %q", fix.target),
			})
			continue
		}
		binary.LittleEndian.PutUint32(out[fix.relPos:fix.relPos+4], uint32(int32(rel)))
	}

	return out, diags
}

func encodeOut(ins Instruction) ([]byte, bool) {
	if len(ins.Operands) != 2 {
		return nil, false
	}
	a, ok := ins.Operands[0].(RegOperand)
	if !ok {
		return nil, false
	}
	b, ok := ins.Operands[1].(RegOperand)
	if !ok {
		return nil, false
	}
	if strings.ToLower(a.Reg.Name) == "dx" && strings.ToLower(b.Reg.Name) == "al" {
		return []byte{0xEE}, true
	}
	return nil, false
}

func encodeIn(ins Instruction) ([]byte, bool) {
	if len(ins.Operands) != 2 {
		return nil, false
	}
	a, ok := ins.Operands[0].(RegOperand)
	if !ok {
		return nil, false
	}
	b, ok := ins.Operands[1].(RegOperand)
	if !ok {
		return nil, false
	}
	if strings.ToLower(a.Reg.Name) == "al" && strings.ToLower(b.Reg.Name) == "dx" {
		return []byte{0xEC}, true
	}
	return nil, false
}

func encodeMov(ins Instruction) ([]byte, bool) {
	if len(ins.Operands) != 2 {
		return nil, false
	}
	left, right := ins.Operands[0], ins.Operands[1]
	if dst, ok := left.(RegOperand); ok {
		if dst.Reg.Segment {
			if src, ok := right.(RegOperand); ok {
				return encodeMovSegmentReg(dst.Reg, src.Reg)
			}
			return nil, false
		}
		if dst.Reg.Control {
			if src, ok := right.(RegOperand); ok {
				return encodeMovControlReg(dst.Reg, src.Reg)
			}
			return nil, false
		}
		if src, ok := right.(RegOperand); ok && src.Reg.Control {
			return encodeMovRegControl(dst.Reg, src.Reg)
		}
		if src, ok := right.(RegOperand); ok {
			return encodeMovRegReg(dst.Reg, src.Reg)
		}
		if src, ok := right.(ImmOperand); ok {
			return encodeMovRegImm(dst.Reg, src.Value)
		}
		if src, ok := right.(MemOperand); ok {
			return encodeMovRegMem(dst.Reg, src)
		}
	}
	if dst, ok := left.(MemOperand); ok {
		if src, ok := right.(RegOperand); ok {
			return encodeMovMemReg(dst, src.Reg)
		}
	}
	return nil, false
}

func encodeMovSegmentReg(dst, src Reg) ([]byte, bool) {
	if !dst.Segment || strings.ToLower(src.Name) != "ax" {
		return nil, false
	}
	return []byte{0x8E, modrm(3, dst.Low3, src.Low3)}, true
}

func encodeMovRegReg(dst, src Reg) ([]byte, bool) {
	opcode := byte(0x8B)
	width := max(dst.Width, src.Width)
	p := rexForOperand(width == 64, src, dst)
	switch width {
	case 8:
		return append(p, opcode-1, modrm(3, dst.Low3, src.Low3)), true
	case 16:
		return append([]byte{0x66}, append(p, opcode, modrm(3, dst.Low3, src.Low3))...), true
	case 32:
		return append(p, opcode, modrm(3, dst.Low3, src.Low3)), true
	case 64:
		return append(p, opcode, modrm(3, dst.Low3, src.Low3)), true
	default:
		return nil, false
	}
}

func encodeMovRegControl(dst, src Reg) ([]byte, bool) {
	if !src.Control {
		return nil, false
	}
	// mov rax, cr3 => opcode 0F20 /r, reg field is control register
	return []byte{0x0F, 0x20, modrm(3, src.Low3, dst.Low3)}, true
}

func encodeMovControlReg(dst Reg, src Reg) ([]byte, bool) {
	if !dst.Control {
		return nil, false
	}
	// mov cr3, rax => opcode 0F22 /r, reg field is control register
	return []byte{0x0F, 0x22, modrm(3, dst.Low3, src.Low3)}, true
}

func encodeMovRegImm(dst Reg, value int64) ([]byte, bool) {
	width := dst.Width
	switch width {
	case 8:
		return append([]byte{0xB0 + byte(dst.Low3)}, byte(value)), true
	case 16:
		return []byte{0x66, 0xB8 + byte(dst.Low3), byte(value), byte(value >> 8)}, true
	case 32:
		return []byte{0xB8 + byte(dst.Low3),
			byte(value), byte(value >> 8), byte(value >> 16), byte(value >> 24)}, true
	case 64:
		p := rexForOperand(true, Reg{}, dst)
		return append(p, 0xB8+byte(dst.Low3), byte(value), byte(value>>8), byte(value>>16), byte(value>>24),
			byte(value>>32), byte(value>>40), byte(value>>48), byte(value>>56)), true
	default:
		return nil, false
	}
}

func encodeMovMemReg(mem MemOperand, reg Reg) ([]byte, bool) {
	width := defaultWidth(mem.Width, reg.Width)
	p := rexForOperand(width == 64, reg, mem.Base)
	mod, disp := encodeMemDisp(mem)
	m := modrm(int(mod), reg.Low3, mem.Base.Low3)
	modrmBytes := append([]byte{m}, disp...)
	switch width {
	case 8:
		return append(append(p, 0x88), modrmBytes...), true
	case 16:
		return append(append(append([]byte{0x66}, p...), 0x89), modrmBytes...), true
	case 32:
		return append(append(p, 0x89), modrmBytes...), true
	case 64:
		return append(append(p, 0x89), modrmBytes...), true
	default:
		return nil, false
	}
}

func encodeMovRegMem(reg Reg, mem MemOperand) ([]byte, bool) {
	width := defaultWidth(mem.Width, reg.Width)
	p := rexForOperand(width == 64, reg, mem.Base)
	mod, disp := encodeMemDisp(mem)
	m := modrm(int(mod), reg.Low3, mem.Base.Low3)
	modrmBytes := append([]byte{m}, disp...)
	switch width {
	case 8:
		return append(append(p, 0x8A), modrmBytes...), true
	case 16:
		return append(append(append([]byte{0x66}, p...), 0x8B), modrmBytes...), true
	case 32:
		return append(append(p, 0x8B), modrmBytes...), true
	case 64:
		return append(append(p, 0x8B), modrmBytes...), true
	default:
		return nil, false
	}
}

func encodeBinaryImm(ins Instruction, ext, op8, op32 byte) ([]byte, bool) {
	if len(ins.Operands) != 2 {
		return nil, false
	}
	reg, ok := ins.Operands[0].(RegOperand)
	if !ok {
		return nil, false
	}
	imm, ok := ins.Operands[1].(ImmOperand)
	if !ok {
		return nil, false
	}
	width := reg.Reg.Width
	opcode := op32
	emit := []byte{}
	if imm.Value >= -128 && imm.Value <= 127 {
		opcode = op8
		emit = []byte{byte(imm.Value)}
	} else {
		emit = []byte{byte(imm.Value), byte(imm.Value >> 8), byte(imm.Value >> 16), byte(imm.Value >> 24)}
	}
	p := rexForOperand(width == 64, Reg{}, reg.Reg)
	switch width {
	case 16:
		p = append([]byte{0x66}, p...)
	case 32:
	case 64:
	default:
		return nil, false
	}
	op := []byte{opcode, modrm(3, int(ext), reg.Reg.Low3)}
	op = append(op, emit...)
	return append(p, op...), true
}

func encodeBinaryRegReg(ins Instruction, opcode byte) ([]byte, bool) {
	if len(ins.Operands) != 2 {
		return nil, false
	}
	dst, ok := ins.Operands[0].(RegOperand)
	if !ok {
		return nil, false
	}
	src, ok := ins.Operands[1].(RegOperand)
	if !ok {
		return nil, false
	}
	width := max(dst.Reg.Width, src.Reg.Width)
	p := rexForOperand(width == 64, src.Reg, dst.Reg)
	switch width {
	case 16:
		return append([]byte{0x66}, append(p, opcode, modrm(3, src.Reg.Low3, dst.Reg.Low3))...), true
	case 32, 64:
		return append(p, opcode, modrm(3, src.Reg.Low3, dst.Reg.Low3)), true
	default:
		return nil, false
	}
}

func encodePushPop(ins Instruction, isPush bool) ([]byte, bool) {
	if len(ins.Operands) != 1 {
		return nil, false
	}
	r, ok := ins.Operands[0].(RegOperand)
	if !ok {
		return nil, false
	}
	if strings.ToLower(r.Reg.Name) != "rbp" {
		return nil, false
	}
	if isPush {
		return []byte{0x55}, true
	}
	return []byte{0x5D}, true
}

func encodeDesc(ins Instruction, regField int) ([]byte, bool) {
	if len(ins.Operands) != 1 {
		return nil, false
	}
	mem, ok := ins.Operands[0].(MemOperand)
	if !ok {
		return nil, false
	}
	p := rexForOperand(mem.Width == 64, Reg{}, mem.Base)
	mod, disp := encodeMemDisp(mem)
	m := modrm(int(mod), regField, mem.Base.Low3)
	bytes := []byte{0x0F, 0x01, m}
	bytes = append(bytes, disp...)
	return append(p, bytes...), true
}

func encodeJmp(ins Instruction, start int, fixups *[]branchFixup) ([]byte, bool) {
	return encodeNearBranch(ins, start, 5, 0xE9, false, fixups)
}

func encodeCall(ins Instruction, start int, fixups *[]branchFixup) ([]byte, bool) {
	return encodeNearBranch(ins, start, 5, 0xE8, false, fixups)
}

func encodeCondJmp(ins Instruction, start int, fixups *[]branchFixup) ([]byte, bool) {
	op, ok := condOpcode[strings.ToLower(ins.Mnemonic)]
	if !ok {
		return nil, false
	}
	return encodeNearBranch(ins, start, 6, op, true, fixups)
}

func encodeNearBranch(ins Instruction, start int, size int, opcode byte, cond bool, fixups *[]branchFixup) ([]byte, bool) {
	if len(ins.Operands) != 1 {
		return nil, false
	}
	switch target := ins.Operands[0].(type) {
	case LabelRef:
		if cond {
			*fixups = append(*fixups, branchFixup{
				relPos: start + 2,
				target: target.Name,
				nextPC: start + size,
			})
			return []byte{0x0F, opcode, 0, 0, 0, 0}, true
		}
		*fixups = append(*fixups, branchFixup{
			relPos: start + 1,
			target: target.Name,
			nextPC: start + size,
		})
		return []byte{opcode, 0, 0, 0, 0}, true
	case ImmOperand:
		_ = target
		return nil, false
	default:
		return nil, false
	}
}

func encodeMemDisp(mem MemOperand) (byte, []byte) {
	disp := int64(mem.Disp)
	if disp == 0 && mem.Base.Low3 != 5 {
		return 0, nil
	}
	if disp >= -128 && disp <= 127 {
		return 1, []byte{byte(disp)}
	}
	out := make([]byte, 4)
	out[0] = byte(uint64(mem.Disp) >> 0)
	out[1] = byte(uint64(mem.Disp) >> 8)
	out[2] = byte(uint64(mem.Disp) >> 16)
	out[3] = byte(uint64(mem.Disp) >> 24)
	return 2, out
}

func modrm(mod, reg, rm int) byte {
	return byte((mod << 6) | (reg << 3) | rm)
}

func rexForOperand(w bool, reg, rm Reg) []byte {
	if !w && !reg.High && !rm.High {
		return nil
	}
	p := byte(0x40)
	if w {
		p |= 1 << 3
	}
	if reg.High {
		p |= 1 << 2
	}
	if rm.High {
		p |= 1
	}
	return []byte{p}
}

func defaultWidth(a, b int) int {
	if a != 0 {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
