package sem

import (
	"math/bits"
	"strconv"

	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/diag"
)

func (c *checker) evalConstExpr(moduleName string, expr ast.Expr, scope map[string]ConstValue) (uint64, []diag.Diagnostic) {
	switch e := expr.(type) {
	case *ast.IntLiteral:
		value, err := strconv.ParseUint(e.Value, 0, 64)
		if err != nil {
			return 0, []diag.Diagnostic{{
				Phase:    "sem",
				Code:     diag.SEM0086,
				Severity: diag.Error,
				Start:    e.Span().Start,
				End:      e.Span().End,
				Message:  "const integer overflows U64",
			}}
		}
		return value, nil
	case *ast.NameExpr:
		if value, ok := scope[e.Name]; ok {
			return value.Value, nil
		}
		if value, ok := c.index.LookupConst(moduleName, e.Name); ok && value.Type != nil {
			return value.Value, nil
		}
		return 0, []diag.Diagnostic{{
			Phase:    "sem",
			Code:     diag.SEM0087,
			Severity: diag.Error,
			Start:    e.Span().Start,
			End:      e.Span().End,
			Message:  "non-const operand " + e.Name,
		}}
	case *ast.SizeOfExpr:
		typ, ds := c.index.LookupTypeRef(moduleName, e.Type, nil)
		if len(ds) != 0 {
			return 0, ds
		}
		size, _, ok := semanticSizeAlign(typ)
		if !ok {
			return 0, []diag.Diagnostic{{
				Phase:    "sem",
				Code:     diag.SEM0088,
				Severity: diag.Error,
				Start:    e.Span().Start,
				End:      e.Span().End,
				Message:  "sizeof requires a sized type",
			}}
		}
		return size, nil
	case *ast.AlignOfExpr:
		typ, ds := c.index.LookupTypeRef(moduleName, e.Type, nil)
		if len(ds) != 0 {
			return 0, ds
		}
		_, align, ok := semanticSizeAlign(typ)
		if !ok {
			return 0, []diag.Diagnostic{{
				Phase:    "sem",
				Code:     diag.SEM0088,
				Severity: diag.Error,
				Start:    e.Span().Start,
				End:      e.Span().End,
				Message:  "alignof requires a sized type",
			}}
		}
		return align, nil
	case *ast.BinaryExpr:
		left, ds := c.evalConstExpr(moduleName, e.Left, scope)
		if len(ds) != 0 {
			return 0, ds
		}
		right, ds := c.evalConstExpr(moduleName, e.Right, scope)
		if len(ds) != 0 {
			return 0, ds
		}
		overflow := func() (uint64, []diag.Diagnostic) {
			return 0, []diag.Diagnostic{{
				Phase:    "sem",
				Code:     diag.SEM0086,
				Severity: diag.Error,
				Start:    e.Span().Start,
				End:      e.Span().End,
				Message:  "const expression overflows U64",
			}}
		}
		switch e.Op {
		case "+":
			sum, carry := bits.Add64(left, right, 0)
			if carry != 0 {
				return overflow()
			}
			return sum, nil
		case "-":
			if right > left {
				return overflow()
			}
			return left - right, nil
		case "*":
			hi, lo := bits.Mul64(left, right)
			if hi != 0 {
				return overflow()
			}
			return lo, nil
		case "<<":
			if right >= 64 {
				return overflow()
			}
			return left << right, nil
		case ">>":
			if right >= 64 {
				return overflow()
			}
			return left >> right, nil
		case "&":
			return left & right, nil
		case "|":
			return left | right, nil
		case "==":
			if left == right {
				return 1, nil
			}
			return 0, nil
		case "!=":
			if left != right {
				return 1, nil
			}
			return 0, nil
		case "<":
			if left < right {
				return 1, nil
			}
			return 0, nil
		case "<=":
			if left <= right {
				return 1, nil
			}
			return 0, nil
		case ">":
			if left > right {
				return 1, nil
			}
			return 0, nil
		case ">=":
			if left >= right {
				return 1, nil
			}
			return 0, nil
		default:
			return 0, []diag.Diagnostic{{
				Phase:    "sem",
				Code:     diag.SEM0087,
				Severity: diag.Error,
				Start:    e.Span().Start,
				End:      e.Span().End,
				Message:  "operator " + e.Op + " is not allowed in const expressions",
			}}
		}
	}
	return 0, []diag.Diagnostic{{
		Phase:    "sem",
		Code:     diag.SEM0087,
		Severity: diag.Error,
		Start:    expr.Span().Start,
		End:      expr.Span().End,
		Message:  "expression is not constant",
	}}
}
