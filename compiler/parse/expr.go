package parse

import (
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
	case lex.Identifier, lex.KeywordNever:
		if p.match(lex.LParen) {
			args, ds := p.parseNamedArgs()
			if len(ds) != 0 {
				return nil, ds
			}
			close, ds := p.consume(lex.RParen)
			if len(ds) != 0 {
				return nil, ds
			}
			return &ast.ConstructorExpr{
				Type:  ast.TypeRef{Name: tok.Text},
				Args:  args,
				SpanV: p.span(tok.Start, close.End),
			}, nil
		}
		return &ast.NameExpr{Name: tok.Text, SpanV: p.span(tok.Start, tok.End)}, nil
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
