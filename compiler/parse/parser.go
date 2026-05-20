package parse

import (
	"strings"

	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/lex"
	"github.com/ryanwible/wrela3/compiler/source"
)

type Parser struct {
	path     string
	src      string
	toks     []lex.Token
	idx      int
	lexDiags []diag.Diagnostic
}

type compositeKind int

const (
	compositeClass compositeKind = iota
	compositeDriver
	compositeDriverPath
	compositeExecutor
)

func ParseGraph(graph source.Graph) ([]*ast.Module, []diag.Diagnostic) {
	var modules []*ast.Module
	var out []diag.Diagnostic

	for _, file := range graph.Files {
		p := newParser(file.Path, file.Source)
		mod, ds := p.ParseModule()
		out = append(out, ds...)
		if mod != nil {
			modules = append(modules, mod)
		}
	}

	diag.Sort(out)
	return modules, out
}

func newParser(path, src string) *Parser {
	toks, lexDiags := lex.All(src)
	return &Parser{path: path, src: src, toks: toks, idx: 0, lexDiags: lexDiags}
}

func (p *Parser) ParseModule() (*ast.Module, []diag.Diagnostic) {
	ds := append([]diag.Diagnostic(nil), p.lexDiags...)

	p.skipSeparators()
	modTok := p.nextIf(lex.KeywordModule)
	if modTok.Kind != lex.KeywordModule {
		return nil, append(ds, p.err(modTok, diag.PAR0001, "expected module declaration")...)
	}

	name, nameDs := p.parseDottedName()
	ds = append(ds, nameDs...)
	if len(ds) != 0 {
		return nil, ds
	}
	mod := &ast.Module{Name: name, Span: source.Span{Start: modTok.Start, End: len(p.src)}}

	p.skipSeparators()
	for p.peek().Kind == lex.KeywordUse {
		imp, impDs := p.parseImport()
		ds = append(ds, impDs...)
		if len(ds) != 0 {
			return nil, ds
		}
		mod.Imports = append(mod.Imports, imp)
		p.skipSeparators()
	}

	for p.peek().Kind != lex.EOF {
		p.skipSeparators()
		if p.peek().Kind == lex.EOF {
			break
		}
		decl, declDs := p.parseDecl()
		ds = append(ds, declDs...)
		if len(declDs) != 0 {
			return nil, ds
		}
		if decl != nil {
			mod.Decls = append(mod.Decls, decl)
		}
	}
	mod.Span.End = p.previous().End
	return mod, ds
}

func (p *Parser) parseImport() (ast.Import, []diag.Diagnostic) {
	start := p.next()
	if start.Kind != lex.KeywordUse {
		return ast.Import{}, p.err(start, diag.PAR0001, "expected use declaration")
	}

	if _, consumeDs := p.consume(lex.LBrace); len(consumeDs) != 0 {
		return ast.Import{}, p.err(p.peek(), diag.PAR0001, "expected '{' after use")
	}

	var names []string
	p.skipSeparators()
	if p.peek().Kind != lex.RBrace {
		for {
			p.skipSeparators()
			name, ds := p.expectIdentifier("expected imported name")
			if len(ds) != 0 {
				return ast.Import{}, ds
			}
			names = append(names, name.Text)
			p.skipSeparators()
			if !p.match(lex.Comma) {
				break
			}
			p.skipSeparators()
		}
	}
	if _, consumeDs := p.consume(lex.RBrace); len(consumeDs) != 0 {
		return ast.Import{}, p.err(p.peek(), diag.PAR0001, "expected '}' in use declaration")
	}
	if _, ds := p.consumeIdentifier(lex.KeywordFrom, "expected from in use declaration"); len(ds) != 0 {
		return ast.Import{}, ds
	}

	path, ds := p.parseDottedName()
	if len(ds) != 0 {
		return ast.Import{}, ds
	}

	return ast.Import{
		Names: names,
		Path:  path,
		Span:  p.span(start.Start, p.previous().End),
	}, nil
}

func (p *Parser) parseDecl() (ast.Decl, []diag.Diagnostic) {
	unique := p.match(lex.KeywordUnique)
	switch p.peek().Kind {
	case lex.KeywordData:
		if unique {
			return nil, p.err(p.peek(), diag.PAR0002, "unique may not prefix data in v0")
		}
		return p.parseDataDecl()
	case lex.KeywordEnum:
		if unique {
			return nil, p.err(p.peek(), diag.PAR0002, "unique may not prefix enum in v0")
		}
		return p.parseEnumDecl()
	case lex.KeywordTrait:
		if unique {
			return nil, p.err(p.peek(), diag.PAR0002, "trait may not be unique in v0")
		}
		return p.parseTraitDecl()
	case lex.KeywordImpl:
		if unique {
			return nil, p.err(p.peek(), diag.PAR0002, "impl may not be unique in v0")
		}
		return p.parseImplDecl()
	case lex.KeywordClass:
		return p.parseClassDecl(unique)
	case lex.KeywordDriver:
		p.next() // consume `driver`
		if p.match(lex.KeywordPath) {
			if unique {
				return nil, p.err(p.peek(), diag.PAR0002, "driver path is not unique in v0")
			}
			return p.parseDriverPathDecl()
		}
		return p.parseDriverDecl(unique)
	case lex.KeywordExecutor:
		if unique {
			return nil, p.err(p.peek(), diag.PAR0002, "executor may not be unique in v0")
		}
		return p.parseExecutorDecl()
	case lex.KeywordImage:
		if unique {
			return nil, p.err(p.peek(), diag.PAR0002, "image may not be unique")
		}
		return p.parseImageDecl()
	case lex.KeywordEvent:
		if unique {
			return nil, p.err(p.peek(), diag.PAR0002, "event may not be unique")
		}
		return p.parseEventDecl()
	case lex.KeywordProjection:
		if unique {
			return nil, p.err(p.peek(), diag.PAR0002, "projection may not be unique")
		}
		return p.parseProjectionDecl()
	case lex.KeywordConst:
		if unique {
			return nil, p.err(p.peek(), diag.PAR0002, "unique may not prefix const in v0")
		}
		return p.parseConstDecl()
	case lex.KeywordStaticAssert:
		if unique {
			return nil, p.err(p.peek(), diag.PAR0002, "unique may not prefix static_assert in v0")
		}
		return p.parseStaticAssertDecl()
	case lex.KeywordFn:
		return nil, p.err(p.peek(), diag.PAR0002, "module-scope fn is not allowed in v0")
	default:
		return nil, p.err(p.peek(), diag.PAR0002, "expected declaration")
	}
}

func (p *Parser) parseEnumDecl() (ast.Decl, []diag.Diagnostic) {
	start := p.next()
	name, ds := p.expectIdentifier("expected enum name")
	if len(ds) != 0 {
		return nil, ds
	}
	typeParams, ds := p.parseTypeParams()
	if len(ds) != 0 {
		return nil, ds
	}
	if _, ds := p.consume(lex.LBrace); len(ds) != 0 {
		return nil, ds
	}

	var variants []ast.EnumVariant
	for p.peek().Kind != lex.RBrace && p.peek().Kind != lex.EOF {
		p.skipSeparators()
		if p.peek().Kind == lex.RBrace || p.peek().Kind == lex.EOF {
			break
		}
		variantName, ds := p.expectIdentifier("expected enum variant name")
		if len(ds) != 0 {
			return nil, ds
		}
		spanEnd := variantName.End
		var fields []ast.Field
		if p.match(lex.LParen) {
			variantFields, ds := p.parseFieldListUntil(lex.RParen)
			if len(ds) != 0 {
				return nil, ds
			}
			fields = variantFields
			spanEnd = p.previous().End
		}
		variants = append(variants, ast.EnumVariant{
			Name:   variantName.Text,
			Fields: fields,
			Span:   p.span(variantName.Start, spanEnd),
		})
		p.match(lex.Comma)
	}
	if !p.match(lex.RBrace) {
		return nil, p.err(p.peek(), diag.PAR0001, "expected '}' after enum variants")
	}
	return &ast.EnumDecl{
		Name:       name.Text,
		TypeParams: typeParams,
		Variants:   variants,
		SpanV:      p.span(start.Start, p.previous().End),
	}, nil
}

func (p *Parser) parseConstDecl() (ast.Decl, []diag.Diagnostic) {
	start := p.next()
	name, ds := p.expectIdentifier("expected const name")
	if len(ds) != 0 {
		return nil, ds
	}
	if _, ds := p.consume(lex.Colon); len(ds) != 0 {
		return nil, ds
	}
	typeRef, ds := p.parseTypeRef()
	if len(ds) != 0 {
		return nil, ds
	}
	if _, ds := p.consume(lex.Equal); len(ds) != 0 {
		return nil, ds
	}
	value, ds := p.parseExpr(0)
	if len(ds) != 0 {
		return nil, ds
	}
	return &ast.ConstDecl{
		Name:  name.Text,
		Type:  typeRef,
		Value: value,
		SpanV: p.span(start.Start, value.Span().End),
	}, nil
}

func (p *Parser) parseEventDecl() (ast.Decl, []diag.Diagnostic) {
	start := p.next()
	name, ds := p.expectIdentifier("expected event name")
	if len(ds) != 0 {
		return nil, ds
	}
	if _, ds := p.consumeContextualIdentifier("id", "expected id in event declaration"); len(ds) != 0 {
		return nil, ds
	}
	id, ds := p.consume(lex.Integer)
	if len(ds) != 0 {
		return nil, p.err(p.peek(), diag.PAR0001, "expected event id")
	}
	if _, ds := p.consume(lex.LBrace); len(ds) != 0 {
		return nil, ds
	}

	var fields []ast.Field
	var layouts []ast.EventLayoutDecl
	var upcasts []ast.LayoutUpcastDecl
	for p.peek().Kind != lex.RBrace && p.peek().Kind != lex.EOF {
		p.skipSeparators()
		if p.peek().Kind == lex.RBrace || p.peek().Kind == lex.EOF {
			break
		}
		if p.peek().Kind == lex.Identifier && p.peek().Text == "layout" {
			layout, ds := p.parseEventLayoutDecl()
			if len(ds) != 0 {
				return nil, ds
			}
			layouts = append(layouts, layout)
		} else if p.peek().Kind == lex.Identifier && p.peek().Text == "upcast" {
			upcast, ds := p.parseLayoutUpcastDecl()
			if len(ds) != 0 {
				return nil, ds
			}
			upcasts = append(upcasts, upcast)
		} else {
			field, ds := p.parseFieldDecl()
			if len(ds) != 0 {
				return nil, ds
			}
			fields = append(fields, field)
		}
		p.skipSeparators()
	}
	if _, ds := p.consume(lex.RBrace); len(ds) != 0 {
		return nil, ds
	}

	return &ast.EventDecl{
		Name:    name.Text,
		ID:      id.Text,
		Fields:  fields,
		Layouts: layouts,
		Upcasts: upcasts,
		SpanV:   p.span(start.Start, p.previous().End),
	}, nil
}

func (p *Parser) parseEventLayoutDecl() (ast.EventLayoutDecl, []diag.Diagnostic) {
	start, ds := p.consumeContextualIdentifier("layout", "expected layout declaration")
	if len(ds) != 0 {
		return ast.EventLayoutDecl{}, ds
	}
	id, ds := p.consume(lex.Integer)
	if len(ds) != 0 {
		return ast.EventLayoutDecl{}, p.err(p.peek(), diag.PAR0001, "expected layout id")
	}
	current := false
	if p.peek().Kind == lex.Identifier && p.peek().Text == "current" {
		p.next()
		current = true
	}
	if _, ds := p.consume(lex.LBrace); len(ds) != 0 {
		return ast.EventLayoutDecl{}, ds
	}

	var fields []ast.EventLayoutField
	for p.peek().Kind != lex.RBrace && p.peek().Kind != lex.EOF {
		p.skipSeparators()
		if p.peek().Kind == lex.RBrace || p.peek().Kind == lex.EOF {
			break
		}
		field, ds := p.parseEventLayoutField()
		if len(ds) != 0 {
			return ast.EventLayoutDecl{}, ds
		}
		fields = append(fields, field)
		p.skipSeparators()
	}
	if _, ds := p.consume(lex.RBrace); len(ds) != 0 {
		return ast.EventLayoutDecl{}, ds
	}

	return ast.EventLayoutDecl{
		ID:      id.Text,
		Current: current,
		Fields:  fields,
		Span:    p.span(start.Start, p.previous().End),
	}, nil
}

func (p *Parser) parseEventLayoutField() (ast.EventLayoutField, []diag.Diagnostic) {
	name, ds := p.expectIdentifier("expected layout field name")
	if len(ds) != 0 {
		return ast.EventLayoutField{}, ds
	}
	if _, ds := p.consume(lex.Colon); len(ds) != 0 {
		return ast.EventLayoutField{}, ds
	}
	typ, ds := p.parseTypeRef()
	if len(ds) != 0 {
		return ast.EventLayoutField{}, ds
	}
	var encode ast.Expr
	if p.match(lex.Equal) {
		expr, ds := p.parseExpr(0)
		if len(ds) != 0 {
			return ast.EventLayoutField{}, ds
		}
		encode = expr
	}
	return ast.EventLayoutField{Name: name.Text, Type: typ, Encode: encode, Span: p.span(name.Start, p.previous().End)}, nil
}

func (p *Parser) parseLayoutUpcastDecl() (ast.LayoutUpcastDecl, []diag.Diagnostic) {
	start, ds := p.consumeContextualIdentifier("upcast", "expected upcast declaration")
	if len(ds) != 0 {
		return ast.LayoutUpcastDecl{}, ds
	}
	from, ds := p.consume(lex.Integer)
	if len(ds) != 0 {
		return ast.LayoutUpcastDecl{}, p.err(p.peek(), diag.PAR0001, "expected upcast source layout id")
	}
	if _, ds := p.consume(lex.Arrow); len(ds) != 0 {
		return ast.LayoutUpcastDecl{}, ds
	}
	to, ds := p.consume(lex.Integer)
	if len(ds) != 0 {
		return ast.LayoutUpcastDecl{}, p.err(p.peek(), diag.PAR0001, "expected upcast target layout id")
	}
	if _, ds := p.consume(lex.LBrace); len(ds) != 0 {
		return ast.LayoutUpcastDecl{}, ds
	}

	var mappings []ast.LayoutUpcastMapping
	for p.peek().Kind != lex.RBrace && p.peek().Kind != lex.EOF {
		p.skipSeparators()
		if p.peek().Kind == lex.RBrace || p.peek().Kind == lex.EOF {
			break
		}
		fromField, ds := p.expectIdentifier("expected upcast source field")
		if len(ds) != 0 {
			return ast.LayoutUpcastDecl{}, ds
		}
		if _, ds := p.consume(lex.Arrow); len(ds) != 0 {
			return ast.LayoutUpcastDecl{}, ds
		}
		toField, ds := p.expectIdentifier("expected upcast target field")
		if len(ds) != 0 {
			return ast.LayoutUpcastDecl{}, ds
		}
		mappings = append(mappings, ast.LayoutUpcastMapping{
			From: fromField.Text,
			To:   toField.Text,
			Span: p.span(fromField.Start, toField.End),
		})
		p.skipSeparators()
	}
	if _, ds := p.consume(lex.RBrace); len(ds) != 0 {
		return ast.LayoutUpcastDecl{}, ds
	}

	return ast.LayoutUpcastDecl{
		FromID:   from.Text,
		ToID:     to.Text,
		Mappings: mappings,
		Span:     p.span(start.Start, p.previous().End),
	}, nil
}

func (p *Parser) parseProjectionDecl() (ast.Decl, []diag.Diagnostic) {
	start := p.next()
	name, ds := p.expectIdentifier("expected projection name")
	if len(ds) != 0 {
		return nil, ds
	}
	if _, ds := p.consumeContextualIdentifier("id", "expected id in projection declaration"); len(ds) != 0 {
		return nil, ds
	}
	id, ds := p.consume(lex.Integer)
	if len(ds) != 0 {
		return nil, p.err(p.peek(), diag.PAR0001, "expected projection id")
	}
	if _, ds := p.consume(lex.LBrace); len(ds) != 0 {
		return nil, ds
	}

	var layouts []ast.ProjectionLayoutDecl
	var upcasts []ast.LayoutUpcastDecl
	for p.peek().Kind != lex.RBrace && p.peek().Kind != lex.EOF {
		p.skipSeparators()
		if p.peek().Kind == lex.RBrace || p.peek().Kind == lex.EOF {
			break
		}
		if p.peek().Kind == lex.Identifier && p.peek().Text == "layout" {
			layout, ds := p.parseProjectionLayoutDecl()
			if len(ds) != 0 {
				return nil, ds
			}
			layouts = append(layouts, layout)
		} else if p.peek().Kind == lex.Identifier && p.peek().Text == "upcast" {
			upcast, ds := p.parseLayoutUpcastDecl()
			if len(ds) != 0 {
				return nil, ds
			}
			upcasts = append(upcasts, upcast)
		} else {
			return nil, p.err(p.peek(), diag.PAR0001, "unexpected token in projection declaration")
		}
		p.skipSeparators()
	}
	if _, ds := p.consume(lex.RBrace); len(ds) != 0 {
		return nil, ds
	}

	return &ast.ProjectionDecl{
		Name:    name.Text,
		ID:      id.Text,
		Layouts: layouts,
		Upcasts: upcasts,
		SpanV:   p.span(start.Start, p.previous().End),
	}, nil
}

func (p *Parser) parseProjectionLayoutDecl() (ast.ProjectionLayoutDecl, []diag.Diagnostic) {
	start, ds := p.consumeContextualIdentifier("layout", "expected layout declaration")
	if len(ds) != 0 {
		return ast.ProjectionLayoutDecl{}, ds
	}
	id, ds := p.consume(lex.Integer)
	if len(ds) != 0 {
		return ast.ProjectionLayoutDecl{}, p.err(p.peek(), diag.PAR0001, "expected layout id")
	}
	current := false
	if p.peek().Kind == lex.Identifier && p.peek().Text == "current" {
		p.next()
		current = true
	}
	fields, ds := p.parseFieldContainer()
	if len(ds) != 0 {
		return ast.ProjectionLayoutDecl{}, ds
	}

	return ast.ProjectionLayoutDecl{
		ID:      id.Text,
		Current: current,
		Fields:  fields,
		Span:    p.span(start.Start, p.previous().End),
	}, nil
}

func (p *Parser) parseStaticAssertDecl() (ast.Decl, []diag.Diagnostic) {
	start := p.next()
	if !p.match(lex.LParen) {
		return nil, p.err(p.peek(), diag.PAR0001, "expected '(' after static_assert")
	}
	expr, ds := p.parseExpr(0)
	if len(ds) != 0 {
		return nil, ds
	}
	if _, ds := p.consume(lex.Comma); len(ds) != 0 {
		return nil, ds
	}
	name, ds := p.expectIdentifier("expected message argument")
	if len(ds) != 0 {
		return nil, ds
	}
	if name.Text != "message" {
		return nil, p.err(name, diag.PAR0001, "expected message argument")
	}
	if _, ds := p.consume(lex.Equal); len(ds) != 0 {
		return nil, ds
	}
	msgTok := p.nextIf(lex.String)
	if msgTok.Kind != lex.String {
		return nil, p.err(msgTok, diag.PAR0001, "expected message string in static_assert")
	}
	if _, ds := p.consume(lex.RParen); len(ds) != 0 {
		return nil, ds
	}
	return &ast.StaticAssertDecl{
		Expr:    expr,
		Message: msgTok.Text,
		SpanV:   p.span(start.Start, p.previous().End),
	}, nil
}

func (p *Parser) parseDataDecl() (ast.Decl, []diag.Diagnostic) {
	start := p.next()
	name, ds := p.expectIdentifier("expected data name")
	if len(ds) != 0 {
		return nil, ds
	}
	typeParams, ds := p.parseTypeParams()
	if len(ds) != 0 {
		return nil, ds
	}
	where, ds := p.parseWhereClause()
	if len(ds) != 0 {
		return nil, ds
	}
	fields, methods, _, _, _, ds := p.parseCompositeMembers(compositeClass)
	if len(ds) != 0 {
		return nil, ds
	}
	return &ast.DataDecl{
		Name:       name.Text,
		TypeParams: typeParams,
		Where:      where,
		Fields:     fields,
		Methods:    methods,
		SpanV:      p.span(start.Start, p.previous().End),
	}, nil
}

func (p *Parser) parseClassDecl(unique bool) (ast.Decl, []diag.Diagnostic) {
	start := p.next()
	name, ds := p.expectIdentifier("expected class name")
	if len(ds) != 0 {
		return nil, ds
	}
	typeParams, ds := p.parseTypeParams()
	if len(ds) != 0 {
		return nil, ds
	}
	where, ds := p.parseWhereClause()
	if len(ds) != 0 {
		return nil, ds
	}
	fields, methods, _, _, _, ds := p.parseCompositeMembers(compositeClass)
	if len(ds) != 0 {
		return nil, ds
	}
	return &ast.ClassDecl{
		Name:       name.Text,
		TypeParams: typeParams,
		Where:      where,
		Fields:     fields,
		Methods:    methods,
		Unique:     unique,
		SpanV:      p.span(start.Start, p.previous().End),
	}, nil
}

func (p *Parser) parseDriverDecl(unique bool) (ast.Decl, []diag.Diagnostic) {
	start := p.previous()
	name, ds := p.expectIdentifier("expected driver name")
	if len(ds) != 0 {
		return nil, ds
	}
	typeParams, ds := p.parseTypeParams()
	if len(ds) != 0 {
		return nil, ds
	}
	where, ds := p.parseWhereClause()
	if len(ds) != 0 {
		return nil, ds
	}
	fields, methods, _, _, _, ds := p.parseCompositeMembers(compositeDriver)
	if len(ds) != 0 {
		return nil, ds
	}
	return &ast.DriverDecl{
		Name:       name.Text,
		TypeParams: typeParams,
		Where:      where,
		Fields:     fields,
		Methods:    methods,
		Unique:     unique,
		SpanV:      p.span(start.Start, p.previous().End),
	}, nil
}

func (p *Parser) parseDriverPathDecl() (ast.Decl, []diag.Diagnostic) {
	start := p.previous()
	name, ds := p.expectIdentifier("expected driver path name")
	if len(ds) != 0 {
		return nil, ds
	}
	fields, methods, interruptEvents, _, _, ds := p.parseCompositeMembers(compositeDriverPath)
	if len(ds) != 0 {
		return nil, ds
	}
	return &ast.DriverPathDecl{
		Name:            name.Text,
		Fields:          fields,
		Methods:         methods,
		InterruptEvents: interruptEvents,
		SpanV:           p.span(start.Start, p.previous().End),
	}, nil
}

func (p *Parser) parseExecutorDecl() (ast.Decl, []diag.Diagnostic) {
	start := p.next()
	name, ds := p.expectIdentifier("expected executor name")
	if len(ds) != 0 {
		return nil, ds
	}
	fields, methods, _, onHandlers, _, ds := p.parseCompositeMembers(compositeExecutor)
	if len(ds) != 0 {
		return nil, ds
	}
	return &ast.ExecutorDecl{
		Name:       name.Text,
		Fields:     fields,
		Methods:    methods,
		OnHandlers: onHandlers,
		SpanV:      p.span(start.Start, p.previous().End),
	}, nil
}

func (p *Parser) parseImageDecl() (ast.Decl, []diag.Diagnostic) {
	start := p.next()
	name, ds := p.expectIdentifier("expected image name")
	if len(ds) != 0 {
		return nil, ds
	}
	if !p.match(lex.LBrace) {
		return nil, p.err(p.peek(), diag.PAR0001, "expected '{' after image name")
	}

	var transitions []ast.Transition
	var phases []ast.PhaseDecl
	for p.peek().Kind != lex.RBrace && p.peek().Kind != lex.EOF {
		p.skipSeparators()
		switch p.peek().Kind {
		case lex.KeywordTransitions:
			ts, tsDs := p.parseTransitions()
			if len(tsDs) != 0 {
				return nil, tsDs
			}
			transitions = append(transitions, ts...)
		case lex.KeywordPhase:
			pd, pdDs := p.parsePhaseDecl()
			if len(pdDs) != 0 {
				return nil, pdDs
			}
			phases = append(phases, *pd)
		case lex.EOF:
			return nil, p.err(p.peek(), diag.PAR0001, "unterminated image declaration")
		case lex.RBrace:
			break
		default:
			return nil, p.err(p.peek(), diag.PAR0001, "unexpected token in image body")
		}
		p.skipSeparators()
	}
	if !p.match(lex.RBrace) {
		return nil, p.err(p.peek(), diag.PAR0001, "expected '}' after image body")
	}
	for i := range phases {
		phases[i].Parent = nil
	}

	return &ast.ImageDecl{
		Name:        name.Text,
		Transitions: transitions,
		Phases:      phases,
		SpanV:       p.span(start.Start, p.previous().End),
	}, nil
}

func (p *Parser) parseTransitions() ([]ast.Transition, []diag.Diagnostic) {
	p.next() // transitions
	if !p.match(lex.LBrace) {
		return nil, p.err(p.peek(), diag.PAR0001, "expected '{' after transitions")
	}

	var out []ast.Transition
	for p.peek().Kind != lex.RBrace && p.peek().Kind != lex.EOF {
		p.skipSeparators()
		if p.peek().Kind == lex.RBrace {
			break
		}
		from, ds := p.expectIdentifier("expected transition source")
		if len(ds) != 0 {
			return nil, ds
		}
		if _, ds := p.consume(lex.Arrow); len(ds) != 0 {
			return nil, ds
		}
		to, ds := p.expectIdentifier("expected transition destination")
		if len(ds) != 0 {
			return nil, ds
		}
		out = append(out, ast.Transition{From: from.Text, To: to.Text, Span: p.span(from.Start, to.End)})

		p.match(lex.Comma)
		p.skipSeparators()
	}
	if !p.match(lex.RBrace) {
		return nil, p.err(p.peek(), diag.PAR0001, "unterminated transitions")
	}
	return out, nil
}

func (p *Parser) parsePhaseDecl() (*ast.PhaseDecl, []diag.Diagnostic) {
	start := p.next() // phase
	nameTok, ds := p.expectIdentifier("expected phase name")
	if len(ds) != 0 {
		return nil, ds
	}
	if !p.match(lex.LParen) {
		return nil, p.err(p.peek(), diag.PAR0001, "expected '(' after phase name")
	}
	params, ds := p.parseParams()
	if len(ds) != 0 {
		return nil, ds
	}
	if !p.match(lex.RParen) {
		return nil, p.err(p.peek(), diag.PAR0001, "expected ')' after phase params")
	}
	if !p.match(lex.Arrow) {
		return nil, p.err(p.peek(), diag.PAR0001, "expected '->' in phase declaration")
	}
	retType, ds := p.parseTypeRef()
	if len(ds) != 0 {
		return nil, ds
	}

	body, ds := p.parseBlockStmts()
	if len(ds) != 0 {
		return nil, ds
	}

	return &ast.PhaseDecl{
		Name:   nameTok.Text,
		Params: params,
		Return: retType,
		Body:   body,
		SpanV:  p.span(start.Start, p.previous().End),
	}, nil
}

func (p *Parser) parseCompositeMembers(kind compositeKind) ([]ast.Field, []ast.MethodDecl, []ast.InterruptEventDecl, []ast.OnHandlerDecl, source.Span, []diag.Diagnostic) {
	startTok, ds := p.consume(lex.LBrace)
	if len(ds) != 0 {
		return nil, nil, nil, nil, source.Span{}, ds
	}
	bodyStart := startTok.Start

	var fields []ast.Field
	var methods []ast.MethodDecl
	var interruptEvents []ast.InterruptEventDecl
	var onHandlers []ast.OnHandlerDecl
	prevEnd := -1
	prevHasSeparator := true
	for p.peek().Kind != lex.RBrace && p.peek().Kind != lex.EOF {
		sawSep := p.skipSeparators()
		if p.peek().Kind == lex.RBrace || p.peek().Kind == lex.EOF {
			break
		}
		if prevEnd >= 0 && !prevHasSeparator && !sawSep && p.previous().Kind != lex.RBrace && p.lineOf(prevEnd) == p.lineOf(p.peek().Start) {
			return nil, nil, nil, nil, source.Span{}, []diag.Diagnostic{{
				Phase:    "parse",
				Code:     diag.PAR0002,
				FilePath: p.path,
				Start:    p.peek().Start,
				End:      p.peek().End,
				Message:  "declarations must be separated by newline or ';'",
			}}
		}

		switch p.peek().Kind {
		case lex.KeywordAsm, lex.KeywordStart, lex.KeywordFn:
			method, ds := p.parseMethodDecl()
			if len(ds) != 0 {
				return nil, nil, nil, nil, source.Span{}, ds
			}
			methods = append(methods, method)
			prevEnd = method.Span().End
		case lex.KeywordInterrupt:
			if kind != compositeDriverPath {
				return nil, nil, nil, nil, source.Span{}, p.err(p.peek(), diag.PAR0001, "unexpected token in declaration body")
			}
			event, ds := p.parseInterruptEventDecl()
			if len(ds) != 0 {
				return nil, nil, nil, nil, source.Span{}, ds
			}
			interruptEvents = append(interruptEvents, event)
			prevEnd = event.Span().End
		case lex.KeywordOn:
			if kind != compositeExecutor {
				return nil, nil, nil, nil, source.Span{}, p.err(p.peek(), diag.PAR0001, "unexpected token in declaration body")
			}
			handler, ds := p.parseOnHandlerDecl()
			if len(ds) != 0 {
				return nil, nil, nil, nil, source.Span{}, ds
			}
			onHandlers = append(onHandlers, handler)
			prevEnd = handler.Span().End
		case lex.Identifier:
			field, ds := p.parseFieldDecl()
			if len(ds) != 0 {
				return nil, nil, nil, nil, source.Span{}, ds
			}
			fields = append(fields, field)
			prevEnd = field.Span.End
		default:
			return nil, nil, nil, nil, source.Span{}, p.err(p.peek(), diag.PAR0001, "unexpected token in declaration body")
		}
		prevHasSeparator = false
	}
	endTok, ds := p.consume(lex.RBrace)
	if len(ds) != 0 {
		return nil, nil, nil, nil, source.Span{}, ds
	}
	return fields, methods, interruptEvents, onHandlers, p.span(bodyStart, endTok.End), nil
}

func (p *Parser) parseInterruptEventDecl() (ast.InterruptEventDecl, []diag.Diagnostic) {
	start := p.next()
	if _, ds := p.consume(lex.KeywordReceiver); len(ds) != 0 {
		return ast.InterruptEventDecl{}, ds
	}
	if _, ds := p.consume(lex.Arrow); len(ds) != 0 {
		return ast.InterruptEventDecl{}, ds
	}
	eventType, ds := p.parseTypeRef()
	if len(ds) != 0 {
		return ast.InterruptEventDecl{}, ds
	}
	body, ds := p.parseBlockStmts()
	if len(ds) != 0 {
		return ast.InterruptEventDecl{}, ds
	}
	return ast.InterruptEventDecl{
		EventType: eventType,
		Body:      body,
		SpanV:     p.span(start.Start, p.previous().End),
	}, nil
}

func (p *Parser) parseOnHandlerDecl() (ast.OnHandlerDecl, []diag.Diagnostic) {
	start := p.next()
	pathField, ds := p.expectIdentifier("expected interrupt path field name")
	if len(ds) != 0 {
		return ast.OnHandlerDecl{}, ds
	}
	if _, ds := p.consume(lex.Dot); len(ds) != 0 {
		return ast.OnHandlerDecl{}, ds
	}
	if _, ds := p.consume(lex.KeywordInterrupt); len(ds) != 0 {
		return ast.OnHandlerDecl{}, ds
	}
	if _, ds := p.consume(lex.LParen); len(ds) != 0 {
		return ast.OnHandlerDecl{}, ds
	}
	paramName, ds := p.expectIdentifier("expected interrupt event parameter name")
	if len(ds) != 0 {
		return ast.OnHandlerDecl{}, ds
	}
	if _, ds := p.consume(lex.Colon); len(ds) != 0 {
		return ast.OnHandlerDecl{}, ds
	}
	paramType, ds := p.parseTypeRef()
	if len(ds) != 0 {
		return ast.OnHandlerDecl{}, ds
	}
	if _, ds := p.consume(lex.RParen); len(ds) != 0 {
		return ast.OnHandlerDecl{}, ds
	}
	body, ds := p.parseBlockStmts()
	if len(ds) != 0 {
		return ast.OnHandlerDecl{}, ds
	}
	return ast.OnHandlerDecl{
		PathField: pathField.Text,
		ParamName: paramName.Text,
		ParamType: paramType,
		Body:      body,
		SpanV:     p.span(start.Start, p.previous().End),
	}, nil
}

func (p *Parser) parseFieldContainer() ([]ast.Field, []diag.Diagnostic) {
	if _, ds := p.consume(lex.LBrace); len(ds) != 0 {
		return nil, ds
	}
	var fields []ast.Field
	prevEnd := -1
	prevHasSeparator := true
	for p.peek().Kind != lex.RBrace && p.peek().Kind != lex.EOF {
		sawSep := p.skipSeparators()
		if p.peek().Kind == lex.RBrace || p.peek().Kind == lex.EOF {
			break
		}
		if prevEnd >= 0 && !prevHasSeparator && !sawSep && p.lineOf(prevEnd) == p.lineOf(p.peek().Start) {
			return nil, []diag.Diagnostic{{
				Phase:    "parse",
				Code:     diag.PAR0002,
				FilePath: p.path,
				Start:    p.peek().Start,
				End:      p.peek().End,
				Message:  "declarations must be separated by newline or ';'",
			}}
		}
		field, ds := p.parseFieldDecl()
		if len(ds) != 0 {
			return nil, ds
		}
		fields = append(fields, field)
		prevEnd = field.Span.End
		prevHasSeparator = false
	}
	if _, ds := p.consume(lex.RBrace); len(ds) != 0 {
		return nil, ds
	}
	return fields, nil
}

func (p *Parser) parseFieldListUntil(end lex.Kind) ([]ast.Field, []diag.Diagnostic) {
	var fields []ast.Field
	if p.match(end) {
		return fields, nil
	}
	for {
		p.skipSeparators()
		if p.peek().Kind == end {
			p.next()
			return fields, nil
		}
		field, ds := p.parseFieldDecl()
		if len(ds) != 0 {
			return nil, ds
		}
		fields = append(fields, field)
		p.skipSeparators()
		if p.match(lex.Comma) {
			p.skipSeparators()
			if p.peek().Kind == end {
				return nil, p.err(p.peek(), diag.PAR0001, "trailing comma")
			}
			continue
		}
		if p.peek().Kind != end {
			return nil, p.err(p.peek(), diag.PAR0001, "expected ',' or ')' in enum variant fields")
		}
		p.next()
		return fields, nil
	}
}

func (p *Parser) parseFieldDecl() (ast.Field, []diag.Diagnostic) {
	name, ds := p.expectIdentifier("expected field name")
	if len(ds) != 0 {
		return ast.Field{}, ds
	}
	if _, ds := p.consume(lex.Colon); len(ds) != 0 {
		return ast.Field{}, ds
	}
	typ, ds := p.parseTypeRef()
	if len(ds) != 0 {
		return ast.Field{}, ds
	}
	return ast.Field{Name: name.Text, Type: typ, Span: p.span(name.Start, p.previous().End)}, nil
}

func (p *Parser) parseMethodDecl() (ast.MethodDecl, []diag.Diagnostic) {
	start := p.next()
	isAsm := false
	isStart := false

	switch start.Kind {
	case lex.KeywordAsm:
		isAsm = true
		fnTok := p.next()
		if fnTok.Kind != lex.KeywordFn {
			return ast.MethodDecl{}, p.err(fnTok, diag.PAR0001, "expected fn after asm")
		}
	case lex.KeywordStart:
		isStart = true
		fnTok := p.next()
		if fnTok.Kind != lex.KeywordFn {
			return ast.MethodDecl{}, p.err(fnTok, diag.PAR0001, "expected fn after start")
		}
	case lex.KeywordFn:
		// normal method
	default:
		return ast.MethodDecl{}, p.err(start, diag.PAR0001, "expected method declaration")
	}

	name, ds := p.expectIdentifier("expected method name")
	if len(ds) != 0 {
		return ast.MethodDecl{}, ds
	}
	typeParams, ds := p.parseTypeParams()
	if len(ds) != 0 {
		return ast.MethodDecl{}, ds
	}
	if _, ds := p.consume(lex.LParen); len(ds) != 0 {
		return ast.MethodDecl{}, ds
	}
	params, ds := p.parseParams()
	if len(ds) != 0 {
		return ast.MethodDecl{}, ds
	}
	if _, ds := p.consume(lex.RParen); len(ds) != 0 {
		return ast.MethodDecl{}, ds
	}
	where, ds := p.parseWhereClause()
	if len(ds) != 0 {
		return ast.MethodDecl{}, ds
	}

	ret := ast.TypeRef{}
	if p.match(lex.Arrow) {
		retType, ds := p.parseTypeRef()
		if len(ds) != 0 {
			return ast.MethodDecl{}, ds
		}
		ret = retType
	}

	if isAsm {
		body, ds := p.captureAsmBody()
		if len(ds) != 0 {
			return ast.MethodDecl{}, ds
		}
		return ast.MethodDecl{
			Name:       name.Text,
			TypeParams: typeParams,
			Where:      where,
			Params:     params,
			Return:     ret,
			Asm:        &body,
			IsAsm:      true,
			IsStart:    isStart,
			SpanV:      p.span(start.Start, p.previous().End),
		}, nil
	}

	body, ds := p.parseBlockStmts()
	if len(ds) != 0 {
		return ast.MethodDecl{}, ds
	}
	return ast.MethodDecl{
		Name:       name.Text,
		TypeParams: typeParams,
		Where:      where,
		Params:     params,
		Return:     ret,
		Body:       body,
		IsAsm:      false,
		IsStart:    isStart,
		SpanV:      p.span(start.Start, p.previous().End),
	}, nil
}

func (p *Parser) parseParams() ([]ast.Param, []diag.Diagnostic) {
	p.skipSeparators()
	if p.peek().Kind == lex.RParen {
		return nil, nil
	}
	var params []ast.Param
	for {
		p.skipSeparators()
		name, ds := p.expectIdentifier("expected parameter name")
		if len(ds) != 0 {
			return nil, ds
		}
		if name.Text == "self" && len(params) == 0 && p.peek().Kind != lex.Colon {
			params = append(params, ast.Param{Name: name.Text, Type: ast.TypeRef{}, Span: p.span(name.Start, name.End)})
			p.skipSeparators()
			if !p.match(lex.Comma) {
				return params, nil
			}
			p.skipSeparators()
			if p.peek().Kind == lex.RParen {
				return nil, p.err(p.peek(), diag.PAR0001, "trailing comma")
			}
			continue
		}
		if _, ds := p.consume(lex.Colon); len(ds) != 0 {
			return nil, ds
		}
		typ, ds := p.parseTypeRef()
		if len(ds) != 0 {
			return nil, ds
		}
		params = append(params, ast.Param{Name: name.Text, Type: typ, Span: p.span(name.Start, p.previous().End)})
		p.skipSeparators()
		if !p.match(lex.Comma) {
			return params, nil
		}
		p.skipSeparators()
		if p.peek().Kind == lex.RParen {
			return nil, p.err(p.peek(), diag.PAR0001, "trailing comma")
		}
	}
}

func (p *Parser) parseStmt() (ast.Stmt, []diag.Diagnostic) {
	switch p.peek().Kind {
	case lex.KeywordLet:
		return p.parseLetStmt()
	case lex.KeywordReturn:
		return p.parseReturnStmt()
	case lex.KeywordIf:
		if p.peekN(1).Kind == lex.KeywordLet {
			return p.parseIfLetStmt()
		}
		return p.parseIfStmt()
	case lex.KeywordMatch:
		return p.parseMatchStmt()
	case lex.KeywordWhile:
		return p.parseWhileStmt()
	case lex.KeywordFor:
		return p.parseForStmt()
	case lex.KeywordWith:
		return p.parseWithStmt()
	case lex.KeywordAsm:
		return nil, p.err(p.peek(), diag.PAR0001, "inline asm blocks are not allowed in v0")
	default:
		return p.parseExprOrAssignStmt()
	}
}

func (p *Parser) parseLetStmt() (ast.Stmt, []diag.Diagnostic) {
	start := p.next()
	name, ds := p.expectIdentifier("expected variable name")
	if len(ds) != 0 {
		return nil, ds
	}
	if _, ds := p.consume(lex.Equal); len(ds) != 0 {
		return nil, ds
	}
	expr, ds := p.parseExpr(0)
	if len(ds) != 0 {
		return nil, ds
	}
	return &ast.LetStmt{Name: name.Text, Expr: expr, SpanV: p.span(start.Start, expr.Span().End)}, nil
}

func (p *Parser) parseReturnStmt() (ast.Stmt, []diag.Diagnostic) {
	start := p.next()
	p.skipSeparators()
	if p.peek().Kind == lex.RBrace || p.peek().Kind == lex.EOF || p.peek().Kind == lex.Semicolon || p.peek().Kind == lex.Newline {
		return &ast.ReturnStmt{Value: nil, SpanV: p.span(start.Start, start.End)}, nil
	}
	expr, ds := p.parseExpr(0)
	if len(ds) != 0 {
		return nil, ds
	}
	return &ast.ReturnStmt{Value: expr, SpanV: p.span(start.Start, expr.Span().End)}, nil
}

func (p *Parser) parseIfStmt() (ast.Stmt, []diag.Diagnostic) {
	start := p.next()
	cond, ds := p.parseExpr(0)
	if len(ds) != 0 {
		return nil, ds
	}
	thenBody, ds := p.parseBlockStmts()
	if len(ds) != 0 {
		return nil, ds
	}

	var elseBody []ast.Stmt
	if p.match(lex.KeywordElse) {
		elseBody, ds = p.parseBlockStmts()
		if len(ds) != 0 {
			return nil, ds
		}
	}
	return &ast.IfStmt{Cond: cond, Then: thenBody, Else: elseBody, SpanV: p.span(start.Start, p.previous().End)}, nil
}

func (p *Parser) parseIfLetStmt() (ast.Stmt, []diag.Diagnostic) {
	start := p.next()
	if _, ds := p.consume(lex.KeywordLet); len(ds) != 0 {
		return nil, ds
	}
	pattern, ds := p.parsePattern()
	if len(ds) != 0 {
		return nil, ds
	}
	if _, ds := p.consume(lex.Equal); len(ds) != 0 {
		return nil, ds
	}
	value, ds := p.parseExpr(0)
	if len(ds) != 0 {
		return nil, ds
	}
	body, ds := p.parseBlockStmts()
	if len(ds) != 0 {
		return nil, ds
	}
	return &ast.IfLetStmt{
		Pattern: pattern,
		Value:   value,
		Body:    body,
		SpanV:   p.span(start.Start, p.previous().End),
	}, nil
}

func (p *Parser) parseMatchStmt() (ast.Stmt, []diag.Diagnostic) {
	start := p.next()
	value, ds := p.parseExpr(0)
	if len(ds) != 0 {
		return nil, ds
	}
	if _, ds := p.consume(lex.LBrace); len(ds) != 0 {
		return nil, ds
	}
	var arms []ast.MatchArm
	for {
		p.skipSeparators()
		if p.peek().Kind == lex.RBrace {
			p.next()
			return &ast.MatchStmt{Value: value, Arms: arms, SpanV: p.span(start.Start, p.previous().End)}, nil
		}
		armStart := p.peek().Start
		pat, ds := p.parsePattern()
		if len(ds) != 0 {
			return nil, ds
		}
		if _, ds := p.consume(lex.FatArrow); len(ds) != 0 {
			return nil, ds
		}
		body, ds := p.parseBlockStmts()
		if len(ds) != 0 {
			return nil, ds
		}
		arms = append(arms, ast.MatchArm{
			Pattern: pat,
			Body:    body,
			Span:    p.span(armStart, p.previous().End),
		})
	}
}

func (p *Parser) parsePattern() (ast.Pattern, []diag.Diagnostic) {
	if p.peek().Kind == lex.Identifier && p.peek().Text == "_" {
		p.next()
		return ast.WildcardPattern{}, nil
	}
	enumTok, ds := p.expectIdentifier("expected enum name in pattern")
	if len(ds) != 0 {
		return nil, ds
	}
	if _, ds := p.consume(lex.Dot); len(ds) != 0 {
		return nil, p.err(p.peek(), diag.PAR0001, "expected enum variant pattern")
	}
	variantTok, ds := p.expectIdentifier("expected enum variant")
	if len(ds) != 0 {
		return nil, ds
	}
	pattern := ast.VariantPattern{
		Enum:    enumTok.Text,
		Variant: variantTok.Text,
	}
	if !p.match(lex.LParen) {
		return pattern, nil
	}
	for {
		name, ds := p.expectIdentifier("expected pattern field name")
		if len(ds) != 0 {
			return nil, ds
		}
		if _, ds := p.consume(lex.Equal); len(ds) != 0 {
			return nil, ds
		}
		bind, ds := p.expectIdentifier("expected pattern binding name")
		if len(ds) != 0 {
			return nil, ds
		}
		pattern.Bindings = append(pattern.Bindings, ast.PatternBinding{Name: name.Text, Bind: bind.Text})
		if !p.match(lex.Comma) {
			break
		}
		p.skipSeparators()
	}
	if _, ds := p.consume(lex.RParen); len(ds) != 0 {
		return nil, ds
	}
	return pattern, nil
}

func (p *Parser) parseWhileStmt() (ast.Stmt, []diag.Diagnostic) {
	start := p.next()
	cond, ds := p.parseExpr(0)
	if len(ds) != 0 {
		return nil, ds
	}
	body, ds := p.parseBlockStmts()
	if len(ds) != 0 {
		return nil, ds
	}
	return &ast.WhileStmt{Cond: cond, Body: body, SpanV: p.span(start.Start, p.previous().End)}, nil
}

func (p *Parser) parseForStmt() (ast.Stmt, []diag.Diagnostic) {
	start := p.next()
	name, ds := p.expectIdentifier("expected loop variable")
	if len(ds) != 0 {
		return nil, ds
	}
	if !p.match(lex.KeywordIn) {
		return nil, p.err(p.peek(), diag.PAR0001, "expected 'in' in for loop")
	}
	expr, ds := p.parseExpr(0)
	if len(ds) != 0 {
		return nil, ds
	}
	body, ds := p.parseBlockStmts()
	if len(ds) != 0 {
		return nil, ds
	}
	return &ast.ForStmt{Var: name.Text, InExpr: expr, Body: body, SpanV: p.span(start.Start, p.previous().End)}, nil
}

func (p *Parser) parseWithStmt() (ast.Stmt, []diag.Diagnostic) {
	start := p.next()
	expr, ds := p.parseExpr(0)
	if len(ds) != 0 {
		return nil, ds
	}
	asTok, ds := p.expectIdentifier("expected as")
	if len(ds) != 0 {
		return nil, ds
	}
	if asTok.Text != "as" {
		return nil, p.err(asTok, diag.PAR0001, "expected as")
	}
	nameTok, ds := p.expectIdentifier("expected frame name")
	if len(ds) != 0 {
		return nil, ds
	}
	body, ds := p.parseBlockStmts()
	if len(ds) != 0 {
		return nil, ds
	}
	return &ast.WithStmt{
		Expr:  expr,
		Name:  nameTok.Text,
		Body:  body,
		SpanV: p.span(start.Start, p.previous().End),
	}, nil
}

func (p *Parser) parseExprOrAssignStmt() (ast.Stmt, []diag.Diagnostic) {
	left, ds := p.parseExpr(0)
	if len(ds) != 0 {
		return nil, ds
	}
	if p.match(lex.Equal) {
		right, ds := p.parseExpr(0)
		if len(ds) != 0 {
			return nil, ds
		}
		return &ast.AssignStmt{Target: left, Value: right, SpanV: p.span(left.Span().Start, right.Span().End)}, nil
	}
	return &ast.ExprStmt{Expr: left, SpanV: left.Span()}, nil
}

func (p *Parser) parseBlockStmts() ([]ast.Stmt, []diag.Diagnostic) {
	if _, ds := p.consume(lex.LBrace); len(ds) != 0 {
		return nil, ds
	}

	var out []ast.Stmt
	for {
		p.skipSeparators()
		if p.peek().Kind == lex.EOF {
			return nil, p.err(p.peek(), diag.PAR0001, "unterminated block")
		}
		if p.peek().Kind == lex.RBrace {
			p.next()
			return out, nil
		}
		stmt, ds := p.parseStmt()
		if len(ds) != 0 {
			return nil, ds
		}
		out = append(out, stmt)
		p.skipSeparators()
	}
}

func (p *Parser) captureAsmBody() (ast.AsmBody, []diag.Diagnostic) {
	open := p.nextIf(lex.LBrace)
	if open.Kind != lex.LBrace {
		return ast.AsmBody{}, p.err(open, diag.PAR0001, "asm requires '{'")
	}

	depth := 1
	start := open.End
	for depth > 0 && p.peek().Kind != lex.EOF {
		tok := p.next()
		switch tok.Kind {
		case lex.LBrace:
			depth++
		case lex.RBrace:
			depth--
		}
		if depth == 0 {
			return ast.AsmBody{
				Source: p.src[start:tok.Start],
				Span:   p.span(start, tok.Start),
			}, nil
		}
	}
	return ast.AsmBody{}, p.err(open, diag.PAR0001, "unterminated asm body")
}

func (p *Parser) parseDottedName() (string, []diag.Diagnostic) {
	first, ds := p.expectIdentifier("expected identifier")
	if len(ds) != 0 {
		return "", ds
	}
	parts := []string{first.Text}
	for p.match(lex.Dot) {
		part, ds := p.expectIdentifier("expected identifier")
		if len(ds) != 0 {
			return "", ds
		}
		parts = append(parts, part.Text)
	}
	return strings.Join(parts, "."), nil
}

func (p *Parser) parseTypeName() (string, []diag.Diagnostic) {
	if p.peek().Kind == lex.KeywordNever {
		return p.next().Text, nil
	}
	return p.parseDottedName()
}

func (p *Parser) parseTypeRef() (ast.TypeRef, []diag.Diagnostic) {
	start := p.peek().Start
	name, ds := p.parseTypeName()
	if len(ds) != 0 {
		return ast.TypeRef{}, ds
	}

	ref := ast.TypeRef{
		Name:  name,
		SpanV: p.span(start, p.previous().End),
	}
	if !p.match(lex.Less) {
		return ref, nil
	}
	for {
		arg, ds := p.parseTypeRef()
		if len(ds) != 0 {
			return ast.TypeRef{}, ds
		}
		ref.Args = append(ref.Args, arg)
		p.skipSeparators()
		if !p.match(lex.Comma) {
			break
		}
		p.skipSeparators()
	}
	if _, ds := p.consumeTypeGreater(); len(ds) != 0 {
		return ast.TypeRef{}, p.err(p.peek(), diag.PAR0001, "expected '>' after type arguments")
	}
	ref.SpanV.End = p.previous().End
	return ref, nil
}

func (p *Parser) consumeTypeGreater() (lex.Token, []diag.Diagnostic) {
	if p.match(lex.Greater) {
		return p.previous(), nil
	}
	if p.peek().Kind != lex.ShiftRight {
		return lex.Token{}, p.err(p.peek(), diag.PAR0001, "expected '>' after type arguments")
	}

	shiftTok := p.next()
	first := lex.Token{
		Kind:  lex.Greater,
		Text:  ">",
		Start: shiftTok.Start,
		End:   shiftTok.Start + 1,
	}
	second := lex.Token{
		Kind:  lex.Greater,
		Text:  ">",
		Start: shiftTok.Start + 1,
		End:   shiftTok.End,
	}
	p.toks[p.idx-1] = first
	p.toks = append(p.toks, lex.Token{})
	copy(p.toks[p.idx+1:], p.toks[p.idx:])
	p.toks[p.idx] = second
	return first, nil
}

func (p *Parser) parseTypeParams() ([]ast.TypeParam, []diag.Diagnostic) {
	if !p.match(lex.Less) {
		return nil, nil
	}
	var out []ast.TypeParam
	for {
		tok, ds := p.expectIdentifier("expected type parameter")
		if len(ds) != 0 {
			return nil, ds
		}
		out = append(out, ast.TypeParam{Name: tok.Text, Span: p.span(tok.Start, tok.End)})
		p.skipSeparators()
		if !p.match(lex.Comma) {
			break
		}
		p.skipSeparators()
	}
	if _, ds := p.consume(lex.Greater); len(ds) != 0 {
		return nil, p.err(p.peek(), diag.PAR0001, "expected '>' after type parameters")
	}
	return out, nil
}

func (p *Parser) parseWhereClause() ([]ast.TraitBound, []diag.Diagnostic) {
	if !p.match(lex.KeywordWhere) {
		return nil, nil
	}
	var out []ast.TraitBound
	for {
		start := p.peek().Start
		param, ds := p.expectIdentifier("expected type parameter in where clause")
		if len(ds) != 0 {
			return nil, ds
		}
		if _, ds := p.consume(lex.Colon); len(ds) != 0 {
			return nil, ds
		}
		trait, ds := p.parseTypeRef()
		if len(ds) != 0 {
			return nil, ds
		}
		out = append(out, ast.TraitBound{Param: param.Text, Trait: trait, Span: p.span(start, trait.Span().End)})
		p.skipSeparators()
		if !p.match(lex.Comma) {
			break
		}
		p.skipSeparators()
	}
	return out, nil
}

func (p *Parser) parseTraitDecl() (ast.Decl, []diag.Diagnostic) {
	start := p.next()
	name, ds := p.expectIdentifier("expected trait name")
	if len(ds) != 0 {
		return nil, ds
	}
	typeParams, ds := p.parseTypeParams()
	if len(ds) != 0 {
		return nil, ds
	}
	if _, ds := p.consume(lex.LBrace); len(ds) != 0 {
		return nil, ds
	}
	var methods []ast.MethodDecl
	for p.peek().Kind != lex.RBrace && p.peek().Kind != lex.EOF {
		p.skipSeparators()
		if p.peek().Kind == lex.RBrace {
			break
		}
		method, ds := p.parseTraitMethodDecl()
		if len(ds) != 0 {
			return nil, ds
		}
		methods = append(methods, method)
		p.skipSeparators()
	}
	if _, ds := p.consume(lex.RBrace); len(ds) != 0 {
		return nil, ds
	}
	return &ast.TraitDecl{
		Name:       name.Text,
		TypeParams: typeParams,
		Methods:    methods,
		SpanV:      p.span(start.Start, p.previous().End),
	}, nil
}

func (p *Parser) parseTraitMethodDecl() (ast.MethodDecl, []diag.Diagnostic) {
	start, ds := p.consume(lex.KeywordFn)
	if len(ds) != 0 {
		return ast.MethodDecl{}, ds
	}
	name, ds := p.expectIdentifier("expected trait method name")
	if len(ds) != 0 {
		return ast.MethodDecl{}, ds
	}
	typeParams, ds := p.parseTypeParams()
	if len(ds) != 0 {
		return ast.MethodDecl{}, ds
	}
	if _, ds := p.consume(lex.LParen); len(ds) != 0 {
		return ast.MethodDecl{}, ds
	}
	params, ds := p.parseParams()
	if len(ds) != 0 {
		return ast.MethodDecl{}, ds
	}
	if _, ds := p.consume(lex.RParen); len(ds) != 0 {
		return ast.MethodDecl{}, ds
	}
	where, ds := p.parseWhereClause()
	if len(ds) != 0 {
		return ast.MethodDecl{}, ds
	}
	ret := ast.TypeRef{}
	if p.match(lex.Arrow) {
		retType, ds := p.parseTypeRef()
		if len(ds) != 0 {
			return ast.MethodDecl{}, ds
		}
		ret = retType
	}
	return ast.MethodDecl{
		Name:       name.Text,
		TypeParams: typeParams,
		Where:      where,
		Params:     params,
		Return:     ret,
		SpanV:      p.span(start.Start, p.previous().End),
	}, nil
}

func (p *Parser) parseImplDecl() (ast.Decl, []diag.Diagnostic) {
	start := p.next()
	trait, ds := p.parseTypeRef()
	if len(ds) != 0 {
		return nil, ds
	}
	if _, ds := p.consume(lex.KeywordFor); len(ds) != 0 {
		return nil, ds
	}
	implemented, ds := p.parseTypeRef()
	if len(ds) != 0 {
		return nil, ds
	}
	return &ast.ImplDecl{
		Trait: trait,
		For:   implemented,
		SpanV: p.span(start.Start, implemented.Span().End),
	}, nil
}

func (p *Parser) parseNamedArgs() ([]ast.NamedArg, []diag.Diagnostic) {
	p.skipSeparators()
	if p.peek().Kind == lex.RParen {
		return nil, nil
	}
	var args []ast.NamedArg
	for {
		p.skipSeparators()
		name := ""
		start := p.peek().Start
		if isExpressionNameToken(p.peek()) && p.peekN(1).Kind == lex.Equal {
			nameTok := p.next()
			name = nameTok.Text
			p.next()
			start = nameTok.Start
		}
		value, ds := p.parseExpr(0)
		if len(ds) != 0 {
			return nil, ds
		}
		args = append(args, ast.NamedArg{Name: name, Value: value, SpanV: p.span(start, value.Span().End)})
		p.skipSeparators()
		if !p.match(lex.Comma) {
			break
		}
		p.skipSeparators()
		if p.peek().Kind == lex.RParen {
			return nil, p.err(p.peek(), diag.PAR0001, "trailing comma")
		}
	}
	return args, nil
}

func (p *Parser) skipSeparators() bool {
	seen := false
	for p.peek().Kind == lex.Newline || p.peek().Kind == lex.Semicolon {
		p.next()
		seen = true
	}
	return seen
}

func (p *Parser) next() lex.Token {
	if p.idx < len(p.toks) {
		p.idx++
	}
	return p.toks[p.idx-1]
}

func (p *Parser) nextIf(kind lex.Kind) lex.Token {
	if p.peek().Kind == kind {
		return p.next()
	}
	return p.peek()
}

func (p *Parser) expect(kind lex.Kind) lex.Token {
	if p.peek().Kind == kind {
		return p.next()
	}
	return p.peek()
}

func (p *Parser) match(kind lex.Kind) bool {
	if p.peek().Kind == kind {
		p.next()
		return true
	}
	return false
}

func (p *Parser) consume(kind lex.Kind) (lex.Token, []diag.Diagnostic) {
	if p.peek().Kind == kind {
		return p.next(), nil
	}
	return p.peek(), []diag.Diagnostic{{
		Phase:    "parse",
		Code:     diag.PAR0001,
		FilePath: p.path,
		Start:    p.peek().Start,
		End:      p.peek().End,
		Message:  "unexpected token",
	}}
}

func (p *Parser) consumeIdentifier(kind lex.Kind, msg string) (lex.Token, []diag.Diagnostic) {
	tok := p.expect(kind)
	if tok.Kind == kind {
		return tok, nil
	}
	if kind == lex.Identifier {
		return tok, p.err(tok, diag.PAR0001, msg)
	}
	return tok, p.err(tok, diag.PAR0001, msg)
}

func (p *Parser) consumeContextualIdentifier(text, msg string) (lex.Token, []diag.Diagnostic) {
	tok := p.peek()
	if tok.Kind == lex.Identifier && tok.Text == text {
		return p.next(), nil
	}
	return tok, p.err(tok, diag.PAR0001, msg)
}

func (p *Parser) expectIdentifier(msg string) (lex.Token, []diag.Diagnostic) {
	tok := p.peek()
	if isNameToken(tok) {
		return p.next(), nil
	}
	return tok, p.err(tok, diag.PAR0001, msg)
}

func isNameToken(tok lex.Token) bool {
	switch tok.Kind {
	case lex.Identifier, lex.KeywordImage, lex.KeywordPath, lex.KeywordEvent, lex.KeywordProjection:
		return true
	default:
		return false
	}
}

func (p *Parser) err(tok lex.Token, code, msg string) []diag.Diagnostic {
	return []diag.Diagnostic{{
		Phase:    "parse",
		Code:     code,
		FilePath: p.path,
		Start:    tok.Start,
		End:      tok.End,
		Message:  msg,
	}}
}

func (p *Parser) peek() lex.Token {
	if p.idx >= len(p.toks) {
		return lex.Token{Kind: lex.EOF, Start: len(p.src), End: len(p.src)}
	}
	return p.toks[p.idx]
}

func (p *Parser) peekN(n int) lex.Token {
	idx := p.idx + n
	if idx >= len(p.toks) {
		return lex.Token{Kind: lex.EOF, Start: len(p.src), End: len(p.src)}
	}
	return p.toks[idx]
}

func (p *Parser) previous() lex.Token {
	if p.idx <= 0 {
		return p.toks[0]
	}
	return p.toks[p.idx-1]
}

func (p *Parser) span(start, end int) source.Span {
	return source.Span{Start: start, End: end}
}

func (p *Parser) lineOf(offset int) int {
	if offset < 0 {
		offset = 0
	}
	if offset > len(p.src) {
		offset = len(p.src)
	}
	return 1 + strings.Count(p.src[:offset], "\n")
}
