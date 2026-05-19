package parse

import (
	"strings"

	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/lex"
)

var precedence = map[lex.Kind]int{
	lex.Dot:          90,
	lex.LParen:       90,
	lex.Star:         80,
	lex.Slash:        80,
	lex.Percent:      80,
	lex.Plus:         70,
	lex.Minus:        70,
	lex.ShiftLeft:    60,
	lex.ShiftRight:   60,
	lex.Less:         50,
	lex.LessEqual:    50,
	lex.Greater:      50,
	lex.GreaterEqual: 50,
	lex.EqualEqual:   40,
	lex.BangEqual:    40,
	lex.Amp:          30,
	lex.Caret:        20,
	lex.Pipe:         10,
}

func (p *Parser) parseExpr(minPrec int) (ast.Expr, []diag.Diagnostic) {
	left, ds := p.parsePrimary()
	if len(ds) != 0 {
		return nil, ds
	}

	for {
		if p.peek().Kind == lex.Newline && p.peekN(1).Kind == lex.Dot {
			p.next()
		}
		tok := p.peek()
		prec, ok := precedence[tok.Kind]
		if !ok || prec < minPrec {
			break
		}

		op := p.next()
		if op.Kind == lex.Dot {
			name, ds := p.expectSelectorName("expected field or method name")
			if len(ds) != 0 {
				return nil, ds
			}
			if p.match(lex.LParen) {
				args, ds := p.parseNamedArgs()
				if len(ds) != 0 {
					return nil, ds
				}
				close, ds := p.consume(lex.RParen)
				if len(ds) != 0 {
					return nil, ds
				}
				left = &ast.CallExpr{
					Receiver: left,
					Method:   name.Text,
					Args:     args,
					SpanV:    p.span(op.Start, close.End),
				}
			} else {
				left = &ast.FieldExpr{
					Base:  left,
					Field: name.Text,
					SpanV: p.span(op.Start, name.End),
				}
			}
			continue
		}

		right, ds := p.parseExpr(prec + 1)
		if len(ds) != 0 {
			return nil, ds
		}
		left = &ast.BinaryExpr{
			Op:    op.Text,
			Left:  left,
			Right: right,
			SpanV: p.span(left.Span().Start, right.Span().End),
		}
	}

	return left, nil
}

func (p *Parser) parsePrimary() (ast.Expr, []diag.Diagnostic) {
	tok := p.next()
	switch tok.Kind {
	case lex.KeywordSizeof, lex.KeywordAlignof:
		isSizeOf := tok.Kind == lex.KeywordSizeof
		if !p.match(lex.LParen) {
			return nil, p.err(tok, diag.PAR0001, "expected '(' after operator")
		}
		typ, ds := p.parseTypeRef()
		if len(ds) != 0 {
			return nil, ds
		}
		if _, ds := p.consume(lex.RParen); len(ds) != 0 {
			return nil, ds
		}
		if isSizeOf {
			return &ast.SizeOfExpr{Type: typ, SpanV: p.span(tok.Start, p.previous().End)}, nil
		}
		return &ast.AlignOfExpr{Type: typ, SpanV: p.span(tok.Start, p.previous().End)}, nil
	case lex.Identifier, lex.KeywordNever:
		if tok.Kind == lex.Identifier && p.peek().Kind == lex.Less && (!startsUpper(tok.Text) || !p.looksLikeGenericConstructor()) {
			return &ast.NameExpr{Name: tok.Text, SpanV: p.span(tok.Start, tok.End)}, nil
		}
		if tok.Kind == lex.Identifier &&
			p.peek().Kind != lex.LParen &&
			p.peek().Kind != lex.Less &&
			!(p.peek().Kind == lex.Dot &&
				p.peekN(1).Kind == lex.Identifier &&
				startsUpper(tok.Text) &&
				startsUpper(p.peekN(1).Text) &&
				p.peekN(2).Kind == lex.LParen) {
			return &ast.NameExpr{Name: tok.Text, SpanV: p.span(tok.Start, tok.End)}, nil
		}
		p.idx--
		typ, ds := p.parseTypeRef()
		if len(ds) != 0 {
			return nil, ds
		}
		if p.match(lex.LParen) {
			args, ds := p.parseNamedArgs()
			if len(ds) != 0 {
				return nil, ds
			}
			close, ds := p.consume(lex.RParen)
			if len(ds) != 0 {
				return nil, ds
			}
			parts := strings.Split(typ.Name, ".")
			if len(parts) == 2 && startsUpper(parts[0]) && startsUpper(parts[1]) {
				return &ast.VariantConstructorExpr{
					Enum:    parts[0],
					Variant: parts[1],
					Args:    args,
					SpanV:   p.span(tok.Start, close.End),
				}, nil
			}
			return &ast.ConstructorExpr{
				Type:  typ,
				Args:  args,
				SpanV: p.span(tok.Start, close.End),
			}, nil
		}
		if len(typ.Args) != 0 {
			return nil, p.err(tok, diag.PAR0001, "generic type arguments are only valid in constructor or type positions")
		}
		return &ast.NameExpr{Name: typ.Name, SpanV: p.span(tok.Start, tok.End)}, nil
	case lex.Integer:
		return &ast.IntLiteral{Value: tok.Text, SpanV: p.span(tok.Start, tok.End)}, nil
	case lex.String:
		return &ast.StringLiteral{Value: tok.Text, SpanV: p.span(tok.Start, tok.End)}, nil
	case lex.KeywordTrue:
		return &ast.BoolLiteral{Value: true, SpanV: p.span(tok.Start, tok.End)}, nil
	case lex.KeywordFalse:
		return &ast.BoolLiteral{Value: false, SpanV: p.span(tok.Start, tok.End)}, nil
	case lex.LParen:
		expr, ds := p.parseExpr(0)
		if len(ds) != 0 {
			return nil, ds
		}
		if _, ds := p.consume(lex.RParen); len(ds) != 0 {
			return nil, ds
		}
		return expr, nil
	default:
		return nil, p.err(tok, diag.PAR0001, "unexpected token in expression")
	}
}

func (p *Parser) looksLikeGenericConstructor() bool {
	if p.peek().Kind != lex.Less {
		return false
	}
	depth := 0
	for i := p.idx; i < len(p.toks); i++ {
		switch p.toks[i].Kind {
		case lex.Less:
			depth++
		case lex.Greater:
			depth--
			if depth == 0 {
				return i+1 < len(p.toks) && p.toks[i+1].Kind == lex.LParen
			}
		case lex.ShiftRight:
			depth--
			if depth == 0 {
				return i+1 < len(p.toks) && p.toks[i+1].Kind == lex.LParen
			}
			depth--
			if depth == 0 {
				return i+1 < len(p.toks) && p.toks[i+1].Kind == lex.LParen
			}
			if depth < 0 {
				return false
			}
		case lex.EOF, lex.Newline, lex.Semicolon, lex.RBrace:
			return false
		}
	}
	return false
}

func (p *Parser) expectSelectorName(msg string) (lex.Token, []diag.Diagnostic) {
	tok := p.peek()
	if isSelectorNameToken(tok) {
		return p.next(), nil
	}
	return tok, p.err(tok, diag.PAR0001, msg)
}

func isSelectorNameToken(tok lex.Token) bool {
	return isExpressionNameToken(tok)
}

func isExpressionNameToken(tok lex.Token) bool {
	return isNameToken(tok) || tok.Kind == lex.KeywordInterrupt || tok.Kind == lex.KeywordStart || tok.Kind == lex.KeywordExecutor
}

func startsUpper(s string) bool {
	if s == "" {
		return false
	}
	r := rune(s[0])
	return r >= 'A' && r <= 'Z'
}
