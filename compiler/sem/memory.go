package sem

import (
	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/source"
)

type MemoryKind uint8

const (
	MemoryKindNone MemoryKind = iota
	MemoryKindRootArena
	MemoryKindFrameArena
	MemoryKindBytes
	MemoryKindMutableBytes
	MemoryKindCacheArena
)

type LifetimeKind uint8

const (
	LifetimeUnknown LifetimeKind = iota
	LifetimeStatic
	LifetimeExecutorRoot
	LifetimeFrame
	LifetimeCacheLookup
	LifetimeCacheCopy
)

type Lifetime struct {
	Kind  LifetimeKind
	Scope int
}

func ClassifyMemoryType(t *Type) MemoryKind {
	if t == nil {
		return MemoryKindNone
	}
	switch t.Module + "." + t.Name {
	case "machine.x86_64.executor_memory.ExecutorMemory":
		return MemoryKindRootArena
	case "machine.x86_64.executor_memory.ArenaFrame":
		return MemoryKindFrameArena
	case "machine.x86_64.executor_memory.Bytes":
		return MemoryKindBytes
	case "machine.x86_64.executor_memory.MutableBytes":
		return MemoryKindMutableBytes
	case "machine.x86_64.cache_memory.CacheArena":
		return MemoryKindCacheArena
	}
	return MemoryKindNone
}

func IsArenaType(t *Type) bool {
	kind := ClassifyMemoryType(t)
	return kind == MemoryKindRootArena || kind == MemoryKindFrameArena
}

func isCanonicalFrameIntrinsic(moduleName string, typ *Type, method ast.MethodDecl) bool {
	if moduleName != "machine.x86_64.executor_memory" || method.Name != "frame" {
		return false
	}
	if typ == nil || (typ.Name != "ExecutorMemory" && typ.Name != "ArenaFrame") {
		return false
	}
	params := method.Params
	if len(params) > 0 && params[0].Name == "self" {
		params = params[1:]
	}
	return len(params) == 1 && params[0].Name == "length" && params[0].Type == "U64"
}

func (c *checker) isFrameCall(moduleName string, expr ast.Expr, scope *Scope, ctx ContextKind) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok || call.Method != "frame" {
		return false
	}
	recvType := c.typeExpr(moduleName, call.Receiver, scope, ctx)
	if !IsArenaType(recvType) {
		c.error(call.Receiver.Span(), diag.SEM0021, "frame receiver must be ExecutorMemory or ArenaFrame")
		return false
	}
	if len(call.Args) != 1 || call.Args[0].Name != "length" {
		return false
	}
	lengthType := c.typeExpr(moduleName, call.Args[0].Value, scope, ctx)
	u64 := c.mustType(moduleName, "U64")
	if lengthType != nil && !typesCompatible(u64, lengthType) {
		c.error(call.Args[0].Value.Span(), diag.SEM0023, "frame length must be U64")
		return false
	}
	return true
}

func (c *checker) frameReceiverLifetime(expr ast.Expr, scope *Scope) Lifetime {
	call, ok := expr.(*ast.CallExpr)
	if !ok || call.Method != "frame" {
		return Lifetime{Kind: LifetimeExecutorRoot}
	}
	return c.lifetimeOfExpr(call.Receiver, scope)
}

func (c *checker) lifetimeOfExpr(expr ast.Expr, scope *Scope) Lifetime {
	if expr == nil {
		return Lifetime{Kind: LifetimeUnknown}
	}
	if c.exprLifetimes != nil {
		if lifetime, ok := c.exprLifetimes[expr]; ok {
			return lifetime
		}
	}
	switch e := expr.(type) {
	case *ast.NameExpr:
		if lifetime, ok := scope.LookupLifetime(e.Name); ok {
			return lifetime
		}
	case *ast.FieldExpr:
		return c.lifetimeOfExpr(e.Base, scope)
	case *ast.StringLiteral:
		return Lifetime{Kind: LifetimeStatic}
	case *ast.IntLiteral, *ast.BoolLiteral:
		return Lifetime{Kind: LifetimeStatic}
	}
	return Lifetime{Kind: LifetimeUnknown}
}

func (c *checker) rememberLifetime(expr ast.Expr, lifetime Lifetime) {
	if c.exprLifetimes == nil {
		c.exprLifetimes = map[ast.Expr]Lifetime{}
	}
	c.exprLifetimes[expr] = lifetime
}

func (c *checker) pushFrameLifetime(name string, span source.Span, parentLifetime Lifetime) int {
	_ = name
	_ = span
	c.nextFrameScope++
	id := c.nextFrameScope
	parent := 0
	if parentLifetime.Kind == LifetimeFrame || parentLifetime.Kind == LifetimeCacheLookup || parentLifetime.Kind == LifetimeCacheCopy {
		parent = parentLifetime.Scope
	}
	if c.frameLifetimeParents == nil {
		c.frameLifetimeParents = map[int]int{}
	}
	c.frameLifetimeParents[id] = parent
	c.frameLifetimeStack = append(c.frameLifetimeStack, id)
	return id
}

func (c *checker) popFrameLifetime(id int) {
	_ = id
	if len(c.frameLifetimeStack) == 0 {
		return
	}
	c.frameLifetimeStack = c.frameLifetimeStack[:len(c.frameLifetimeStack)-1]
}

func (c *checker) currentFrameLifetime() Lifetime {
	if len(c.frameLifetimeStack) == 0 {
		return Lifetime{Kind: LifetimeExecutorRoot}
	}
	return Lifetime{Kind: LifetimeFrame, Scope: c.frameLifetimeStack[len(c.frameLifetimeStack)-1]}
}

func (c *checker) typeArenaIntrinsicCall(moduleName string, expr *ast.CallExpr, scope *Scope, ctx ContextKind) *Type {
	recvType := c.typeExpr(moduleName, expr.Receiver, scope, ctx)
	if !IsArenaType(recvType) {
		return nil
	}
	if ctx == ContextOnHandler && (expr.Method == "place" || expr.Method == "reserve") {
		c.error(expr.SpanV, diag.SEM0016, "on handler cannot place or reserve arena memory")
		return nil
	}
	switch expr.Method {
	case "place":
		if len(expr.Args) != 1 || expr.Args[0].Name != "" {
			c.error(expr.SpanV, diag.SEM0026, "place expects one constructor argument")
			return nil
		}
		cons, ok := expr.Args[0].Value.(*ast.ConstructorExpr)
		if !ok {
			c.error(expr.Args[0].Value.Span(), diag.SEM0026, "place argument must be a constructor expression")
			return nil
		}
		typ := c.typeConstructorExpr(moduleName, cons, scope, ctx)
		c.rememberLifetime(expr, c.currentFrameLifetime())
		return typ
	case "reserve":
		c.requireReserveArgs(moduleName, expr, scope, ctx)
		c.rememberLifetime(expr, c.currentFrameLifetime())
		return c.resolveType("machine.x86_64.executor_memory", "MutableBytes")
	default:
		return nil
	}
}

func (c *checker) requireReserveArgs(moduleName string, expr *ast.CallExpr, scope *Scope, ctx ContextKind) {
	if len(expr.Args) != 2 || expr.Args[0].Name != "length" || expr.Args[1].Name != "align" {
		c.error(expr.SpanV, diag.SEM0027, "reserve expects length and align")
		return
	}
	u64 := c.mustType(moduleName, "U64")
	lengthType := c.typeExpr(moduleName, expr.Args[0].Value, scope, ctx)
	if lengthType != nil && !typesCompatible(u64, lengthType) {
		c.error(expr.Args[0].Value.Span(), diag.SEM0027, "reserve length and align must be U64")
	}
	alignType := c.typeExpr(moduleName, expr.Args[1].Value, scope, ctx)
	if alignType != nil && !typesCompatible(u64, alignType) {
		c.error(expr.Args[1].Value.Span(), diag.SEM0027, "reserve length and align must be U64")
	}
}
