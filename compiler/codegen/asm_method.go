package codegen

import (
	"fmt"

	"github.com/ryanwible/wrela3/compiler/asm"
	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/ir"
)

func lowerAndEncodeAsmMethod(method ir.AsmMethod) ([]asm.Instruction, []diag.Diagnostic, []byte) {
	paramNames := make([]string, 0, len(method.Params)+1)
	paramNames = append(paramNames, "self")
	for _, p := range method.Params {
		if p1, ok := p.(*ir.Param); ok {
			paramNames = append(paramNames, p1.Symbol)
		}
	}

	parsed, diags := asm.ParseBody(method.Body, paramNames)
	if len(diags) != 0 {
		return nil, diags, nil
	}

	paramLoc := map[string]asm.Reg{
		"self": asm.MustLookup("rdi"),
	}
	for i, p := range method.Params {
		if param, ok := p.(*ir.Param); ok {
			if i < len(argRegs)-1 {
				paramLoc[param.Symbol] = argRegs[i+1]
			}
		}
	}

	lower := make([]asm.Instruction, 0, len(parsed))
	for _, in := range parsed {
		next := asm.Instruction{
			Mnemonic: in.Mnemonic,
			Label:    in.Label,
			Operands: make([]asm.Operand, 0, len(in.Operands)),
		}
		for _, operand := range in.Operands {
			nextOp, mapErr := lowerBoundOperand(method, operand, paramLoc)
			if mapErr != nil {
				return nil, []diag.Diagnostic{*mapErr}, nil
			}
			if nextOp == nil {
				nextOp = operand
			}
			next.Operands = append(next.Operands, nextOp)
		}
		lower = append(lower, next)
	}

	emitted, asDiags := asm.Encode(lower)
	if len(asDiags) != 0 {
		return nil, convertAsmDiagnostics(asDiags), nil
	}
	return lower, nil, emitted
}

func lowerBoundOperand(method ir.AsmMethod, op asm.Operand, paramLoc map[string]asm.Reg) (asm.Operand, *diag.Diagnostic) {
	switch o := op.(type) {
	case asm.ParamOperand:
		loc, ok := paramLoc[o.Name]
		if !ok {
			return nil, &diag.Diagnostic{
				Phase:   "asm",
				Code:    diag.ASM0002,
				Message: fmt.Sprintf("unknown asm parameter %q in %s", o.Name, method.Symbol),
			}
		}
		return asm.RegOperand{Reg: loc}, nil
	case asm.FieldOperand:
		if o.Base != "self" {
			return nil, &diag.Diagnostic{
				Phase:   "asm",
				Code:    diag.ASM0002,
				Message: "unsupported field base in asm method: " + o.Base,
			}
		}
		offset := method.ReceiverFieldOffsets[o.Field]
		width, ok := method.ReceiverFieldWidths[o.Field]
		if !ok {
			width = 8
		}
		return asm.MemOperand{Base: asm.MustLookup("rdi"), Disp: int64(offset), Width: width}, nil
	case asm.LabelRef, asm.RegOperand, asm.MemOperand, asm.ImmOperand:
		return o, nil
	default:
		return op, nil
	}
}

func convertAsmDiagnostics(input []diag.Diagnostic) []diag.Diagnostic {
	out := make([]diag.Diagnostic, 0, len(input))
	for _, d := range input {
		out = append(out, diag.Diagnostic{
			Phase:    d.Phase,
			Code:     d.Code,
			Message:  d.Message,
			FilePath: d.FilePath,
			Start:    d.Start,
			End:      d.End,
			Severity: d.Severity,
		})
	}
	return out
}
