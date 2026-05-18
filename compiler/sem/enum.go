package sem

import (
	"fmt"
	"strings"

	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/source"
)

func (c *checker) enumVariant(enumType *Type, name string) (EnumVariant, bool) {
	if enumType == nil || enumType.Kind != KindEnum {
		return EnumVariant{}, false
	}
	origin := enumType
	if enumType.GenericOrigin != nil {
		origin = enumType.GenericOrigin
	}
	for _, variant := range origin.EnumVariants {
		if variant.Name == name {
			return c.substituteEnumVariant(enumType, variant), true
		}
	}
	return EnumVariant{}, false
}

func (c *checker) enumVariants(enumType *Type) []EnumVariant {
	if enumType == nil {
		return nil
	}
	if len(enumType.EnumVariants) != 0 {
		return enumType.EnumVariants
	}
	if enumType.GenericOrigin != nil {
		return enumType.GenericOrigin.EnumVariants
	}
	return nil
}

func (c *checker) substituteEnumVariant(enumType *Type, variant EnumVariant) EnumVariant {
	for _, concrete := range enumType.EnumVariants {
		if concrete.Name == variant.Name {
			return concrete
		}
	}
	return variant
}

func (c *checker) checkMatchStmt(moduleName string, scope *Scope, expectedReturn *Type, ctx ContextKind, stmt *ast.MatchStmt) bool {
	valueType := c.typeExpr(moduleName, stmt.Value, scope, ctx)
	if valueType == nil || valueType.Kind != KindEnum {
		c.error(stmt.Value.Span(), diag.SEM0085, "match requires enum value")
		return false
	}

	seen := map[string]source.Span{}
	wildcard := false
	allArmsTerminate := len(stmt.Arms) > 0
	for _, arm := range stmt.Arms {
		switch p := arm.Pattern.(type) {
		case ast.WildcardPattern:
			if wildcard {
				c.error(arm.Span, diag.SEM0095, "duplicate wildcard match arm")
				allArmsTerminate = false
				continue
			}
			wildcard = true
			if !c.checkStmtList(moduleName, arm.Body, NewScope(scope), expectedReturn, ctx) {
				allArmsTerminate = false
			}
		case ast.VariantPattern:
			variant, ok := c.enumVariant(valueType, p.Variant)
			if !ok || !sameEnumPatternName(valueType, p.Enum) {
				c.error(arm.Span, diag.SEM0085, "impossible enum variant pattern "+p.Enum+"."+p.Variant)
				allArmsTerminate = false
				continue
			}
			if _, ok := seen[variant.Name]; ok {
				c.error(arm.Span, diag.SEM0095, "duplicate match arm for "+p.Enum+"."+p.Variant)
				allArmsTerminate = false
				continue
			}
			seen[variant.Name] = arm.Span
			armScope := NewScope(scope)
			if c.bindPatternFields(armScope, variant, p.Bindings, arm.Span) {
				allArmsTerminate = false
				continue
			}
			if !c.checkStmtList(moduleName, arm.Body, armScope, expectedReturn, ctx) {
				allArmsTerminate = false
			}
		default:
			c.error(arm.Span, diag.SEM0095, "unsupported match pattern")
			allArmsTerminate = false
		}
	}

	exhaustive := wildcard || len(seen) == len(c.enumVariants(valueType))
	if !exhaustive {
		c.error(stmt.SpanV, diag.SEM0084, "non-exhaustive match for "+valueType.Display())
	}
	return exhaustive && allArmsTerminate
}

func (c *checker) checkIfLetStmt(moduleName string, scope *Scope, expectedReturn *Type, ctx ContextKind, stmt *ast.IfLetStmt) bool {
	valueType := c.typeExpr(moduleName, stmt.Value, scope, ctx)
	pattern, ok := stmt.Pattern.(ast.VariantPattern)
	if !ok {
		c.error(stmt.SpanV, diag.SEM0095, "if let requires enum variant pattern")
		return false
	}
	variant, found := c.enumVariant(valueType, pattern.Variant)
	if !found || !sameEnumPatternName(valueType, pattern.Enum) {
		c.error(stmt.SpanV, diag.SEM0085, "impossible enum variant pattern "+pattern.Enum+"."+pattern.Variant)
		return false
	}
	bodyScope := NewScope(scope)
	if c.bindPatternFields(bodyScope, variant, pattern.Bindings, stmt.SpanV) {
		return false
	}
	c.checkStmtList(moduleName, stmt.Body, bodyScope, expectedReturn, ctx)
	return false
}

func (c *checker) bindPatternFields(scope *Scope, variant EnumVariant, bindings []ast.PatternBinding, span source.Span) bool {
	byName := map[string]Field{}
	for _, field := range variant.Fields {
		byName[field.Name] = field
	}

	seenFields := map[string]bool{}
	seenBinds := map[string]bool{}
	failed := false
	for _, binding := range bindings {
		field, ok := byName[binding.Name]
		if !ok {
			c.error(span, diag.SEM0095, "unknown payload field "+binding.Name)
			failed = true
			continue
		}
		if seenFields[binding.Name] {
			c.error(span, diag.SEM0095, "duplicate payload field "+binding.Name)
			failed = true
		}
		if binding.Bind == "" || binding.Bind == "_" {
			c.error(span, diag.SEM0095, "invalid pattern binding "+binding.Bind)
			failed = true
			continue
		}
		if seenBinds[binding.Bind] {
			c.error(span, diag.SEM0095, "duplicate pattern binding "+binding.Bind)
			failed = true
		}
		seenFields[binding.Name] = true
		seenBinds[binding.Bind] = true
		scope.Define(binding.Bind, field.Type)
	}
	for _, field := range variant.Fields {
		if !seenFields[field.Name] {
			c.error(span, diag.SEM0095, "missing payload field "+field.Name)
			failed = true
		}
	}
	return failed
}

func sameEnumPatternName(enumType *Type, patternName string) bool {
	if enumType == nil {
		return false
	}
	if enumType.Name == patternName {
		return true
	}
	if base := strings.Split(enumType.Display(), "<")[0]; base == patternName {
		return true
	}
	if enumType.GenericOrigin != nil && enumType.GenericOrigin.Name == patternName {
		return true
	}
	if strings.HasSuffix(qualifiedTypeName(enumType), "."+patternName) {
		return true
	}
	if enumType.GenericOrigin != nil && strings.HasSuffix(qualifiedTypeName(enumType.GenericOrigin), "."+patternName) {
		return true
	}
	return false
}

func (c *checker) typeVariantConstructorExpr(moduleName string, expr *ast.VariantConstructorExpr, scope *Scope, ctx ContextKind, expected *Type) *Type {
	enumType, ok := c.index.lookupBaseType(moduleName, expr.Enum)
	if !ok || enumType == nil || enumType.Kind != KindEnum {
		c.error(expr.SpanV, diag.SEM0094, "unknown enum "+expr.Enum)
		return nil
	}

	originVariant, ok := c.enumVariant(enumType, expr.Variant)
	if !ok {
		c.error(expr.SpanV, diag.SEM0094, "unknown enum variant "+expr.Enum+"."+expr.Variant)
		return nil
	}

	concreteEnum := enumType
	if len(enumType.TypeParams) != 0 {
		args, inferred := c.inferEnumTypeArgs(moduleName, enumType, originVariant, expr.Args, expected, scope, ctx)
		if !inferred {
			c.error(expr.SpanV, diag.SEM0079, "cannot infer type arguments for "+expr.Enum+"."+expr.Variant)
			return nil
		}
		if expected != nil && expected.Kind == KindEnum && sameEnumPatternName(expected, expr.Enum) {
			concreteEnum = expected
		} else {
			concreteEnum = c.index.registerInstantiation(enumType, args)
		}
		for _, d := range c.index.completeInstantiation(concreteEnum.Key(), map[string]bool{}) {
			c.diags = append(c.diags, d)
		}
	}

	if expected != nil && expected.Kind == KindEnum && !typesCompatible(expected, concreteEnum) {
		c.error(expr.SpanV, diag.SEM0094, fmt.Sprintf("%s.%s does not construct %s", expr.Enum, expr.Variant, expected.Display()))
		return nil
	}

	variant, ok := c.enumVariant(concreteEnum, expr.Variant)
	if !ok {
		c.error(expr.SpanV, diag.SEM0094, "unknown enum variant "+expr.Enum+"."+expr.Variant)
		return nil
	}
	if c.checkVariantConstructorArgs(moduleName, expr, scope, ctx, variant) {
		return nil
	}
	c.rememberLifetime(expr, Lifetime{Kind: LifetimeExecutorRoot})
	return concreteEnum
}

func (c *checker) inferEnumTypeArgs(moduleName string, enum *Type, variant EnumVariant, args []ast.NamedArg, expected *Type, scope *Scope, ctx ContextKind) ([]*Type, bool) {
	if expected != nil && expected.Kind == KindEnum && (expected == enum || expected.GenericOrigin == enum || sameEnumPatternName(expected, enum.Name)) {
		if len(expected.TypeArgs) == len(enum.TypeParams) {
			return expected.TypeArgs, true
		}
		return nil, false
	}
	if len(variant.Fields) == 0 {
		return nil, false
	}

	inferred := map[string]*Type{}
	for _, field := range variant.Fields {
		if field.Type == nil || field.Type.Kind != KindTypeParam || field.Type.Module != "" || len(field.Type.TypeArgs) != 0 {
			continue
		}
		argExpr := namedArgExpr(args, field.Name)
		if argExpr == nil {
			continue
		}
		concrete := c.staticEnumInferenceType(moduleName, argExpr, scope)
		if concrete == nil {
			concrete = c.typeExpr(moduleName, argExpr, scope, ctx)
		}
		if concrete == nil || concrete.Kind == KindTypeParam {
			continue
		}
		if existing := inferred[field.Type.Name]; existing != nil && existing.Key() != concrete.Key() {
			return nil, false
		}
		inferred[field.Type.Name] = concrete
	}

	out := make([]*Type, 0, len(enum.TypeParams))
	for _, param := range enum.TypeParams {
		concrete := inferred[param.Name]
		if concrete == nil {
			return nil, false
		}
		out = append(out, concrete)
	}
	return out, true
}

func (c *checker) staticEnumInferenceType(moduleName string, expr ast.Expr, scope *Scope) *Type {
	switch e := expr.(type) {
	case *ast.IntLiteral:
		return c.mustType(moduleName, "U64")
	case *ast.StringLiteral:
		return c.mustType(moduleName, "StringLiteral")
	case *ast.BoolLiteral:
		return c.mustType(moduleName, "Bool")
	case *ast.ConstructorExpr:
		typ, ds := c.index.LookupTypeRef(moduleName, e.Type, nil)
		for _, d := range ds {
			c.diags = append(c.diags, d)
		}
		return typ
	default:
		return c.exprStaticType(moduleName, expr, scope)
	}
}

func (c *checker) checkVariantConstructorArgs(moduleName string, expr *ast.VariantConstructorExpr, scope *Scope, ctx ContextKind, variant EnumVariant) bool {
	byName := map[string]Field{}
	for _, field := range variant.Fields {
		byName[field.Name] = field
	}

	seenFields := map[string]bool{}
	failed := false
	for _, arg := range expr.Args {
		field, ok := byName[arg.Name]
		if !ok {
			c.error(arg.SpanV, diag.SEM0094, "unknown variant payload field "+arg.Name)
			failed = true
			continue
		}
		if seenFields[arg.Name] {
			c.error(arg.SpanV, diag.SEM0094, "duplicate variant payload field "+arg.Name)
			failed = true
			continue
		}
		seenFields[arg.Name] = true
		argType := c.typeExprExpected(moduleName, arg.Value, scope, ctx, field.Type)
		c.checkTypeAssign(arg.SpanV, field.Type, argType)
	}
	for _, field := range variant.Fields {
		if !seenFields[field.Name] {
			c.error(expr.SpanV, diag.SEM0094, "missing variant payload field "+field.Name)
			failed = true
		}
	}
	return failed
}
