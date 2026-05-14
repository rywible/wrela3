package asm

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/ryanwible/wrela3/compiler/diag"
)

var validInstructions = map[string]struct{}{
	"hlt":   {},
	"pause": {},
	"ret":   {},
	"retfq": {},
	"cli":   {},
	"sti":   {},
	"out":   {},
	"in":    {},
	"mov":   {},
	"add":   {},
	"cmp":   {},
	"call":  {},
	"jmp":   {},
	"je":    {},
	"jne":   {},
	"jl":    {},
	"jle":   {},
	"jg":    {},
	"jge":   {},
	"push":  {},
	"pop":   {},
	"lgdt":  {},
	"lidt":  {},
}

var branchOps = map[string]struct{}{
	"jmp":  {},
	"je":   {},
	"jne":  {},
	"jl":   {},
	"jle":  {},
	"jg":   {},
	"jge":  {},
	"call": {},
}

func ParseBody(source string, params []string) ([]Instruction, []diag.Diagnostic) {
	parsedParams := make(map[string]struct{}, len(params))
	for _, p := range params {
		parsedParams[strings.ToLower(strings.TrimSpace(p))] = struct{}{}
	}

	stmts := splitStatements(source)
	var out []Instruction
	var diagnostics []diag.Diagnostic
	for _, stmt := range stmts {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}

		label, remainder, hadLabel := splitLabel(stmt)
		if hadLabel {
			out = append(out, Instruction{Label: label})
			stmt = strings.TrimSpace(remainder)
			if stmt == "" {
				continue
			}
		}

		mnemonic, operandsText := splitOpcode(stmt)
		if mnemonic == "" {
			diagnostics = append(diagnostics, diag.Diagnostic{
				Phase:   "asm",
				Code:    diag.ASM0001,
				Message: "malformed instruction",
			})
			continue
		}
		mnemonic = strings.ToLower(mnemonic)
		if _, ok := validInstructions[mnemonic]; !ok {
			diagnostics = append(diagnostics, diag.Diagnostic{
				Phase:   "asm",
				Code:    diag.ASM0001,
				Message: "unknown instruction: " + mnemonic,
			})
			continue
		}

		rawOps := splitOperands(operandsText)
		ops := make([]Operand, 0, len(rawOps))
		for _, raw := range rawOps {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}
			op, ok := parseOperand(raw, parsedParams, isBranchInstr(mnemonic))
			if !ok {
				diagnostics = append(diagnostics, diag.Diagnostic{
					Phase:   "asm",
					Code:    diag.ASM0002,
					Message: "invalid operand: " + raw,
				})
				ops = nil
				break
			}
			ops = append(ops, op)
		}
		out = append(out, Instruction{Mnemonic: mnemonic, Operands: ops})
	}
	return out, diagnostics
}

func splitStatements(source string) []string {
	lines := strings.Split(source, "\n")
	var stmts []string
	for _, line := range lines {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		for _, piece := range strings.Split(line, ";") {
			piece = strings.TrimSpace(piece)
			if piece == "" {
				continue
			}
			stmts = append(stmts, piece)
		}
	}
	return stmts
}

func splitLabel(stmt string) (string, string, bool) {
	parts := strings.SplitN(stmt, ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	label := strings.TrimSpace(parts[0])
	if !isIdentifier(label) {
		return "", "", false
	}
	rest := strings.TrimSpace(parts[1])
	return label, rest, true
}

func splitOperands(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	var out []string
	var cur strings.Builder
	depth := 0
	for _, c := range text {
		switch c {
		case '[':
			depth++
		case ']':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(cur.String()))
				cur.Reset()
				continue
			}
		}
		cur.WriteRune(c)
	}
	if strings.TrimSpace(cur.String()) != "" {
		out = append(out, strings.TrimSpace(cur.String()))
	}
	return out
}

func parseOperand(raw string, params map[string]struct{}, branchTarget bool) (Operand, bool) {
	if raw == "" {
		return nil, false
	}

	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "self.") {
		field := strings.TrimSpace(lower[5:])
		if field == "" || !isIdentifier(field) {
			return nil, false
		}
		return FieldOperand{
			Base:  "self",
			Field: field,
		}, true
	}

	if strings.HasPrefix(lower, "[") && strings.HasSuffix(lower, "]") {
		inside := strings.TrimSpace(lower[1 : len(lower)-1])
		mem, ok := parseMemOperand(inside)
		if !ok {
			return nil, false
		}
		return mem, true
	}

	if r, ok := Lookup(lower); ok {
		return RegOperand{Reg: r}, true
	}

	if _, ok := params[lower]; ok {
		return ParamOperand{Name: lower}, true
	}

	if branchTarget && isIdentifier(lower) {
		return LabelRef{Name: lower}, true
	}

	if i, err := parseIntLiteral(raw); err == nil {
		return ImmOperand{Value: i}, true
	}

	return nil, false
}

func parseMemOperand(text string) (MemOperand, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return MemOperand{}, false
	}

	if !strings.ContainsAny(text, "+-") {
		r, ok := Lookup(text)
		if !ok {
			return MemOperand{}, false
		}
		return MemOperand{Base: r, Width: r.Width}, true
	}

	baseText := ""
	dispText := ""
	for i := 0; i < len(text); i++ {
		if text[i] == '+' || text[i] == '-' {
			baseText = strings.TrimSpace(text[:i])
			dispText = strings.TrimSpace(text[i:])
			break
		}
	}
	if baseText == "" {
		return MemOperand{}, false
	}
	base, ok := Lookup(strings.TrimSpace(baseText))
	if !ok {
		return MemOperand{}, false
	}
	delta, err := strconv.ParseInt(strings.TrimSpace(dispText), 0, 64)
	if err != nil {
		return MemOperand{}, false
	}
	return MemOperand{Base: base, Disp: delta, Width: base.Width}, true
}

func parseIntLiteral(raw string) (int64, error) {
	return strconv.ParseInt(raw, 0, 64)
}

func isBranchInstr(mnemonic string) bool {
	_, ok := branchOps[mnemonic]
	return ok
}

func isIdentifier(s string) bool {
	if s == "" {
		return false
	}
	first := rune(s[0])
	if !(first == '_' || unicode.IsLetter(first)) {
		return false
	}
	for _, c := range s[1:] {
		if c == '_' || unicode.IsLetter(c) || unicode.IsDigit(c) {
			continue
		}
		return false
	}
	return true
}

func splitOpcode(stmt string) (string, string) {
	space := strings.IndexAny(stmt, " \t")
	if space < 0 {
		return stmt, ""
	}
	return strings.TrimSpace(stmt[:space]), strings.TrimSpace(stmt[space+1:])
}
