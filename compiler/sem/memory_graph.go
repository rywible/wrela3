package sem

import (
	"fmt"

	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/source"
)

func arenaRangesOverlap(a, b ArenaNode) bool {
	if a.Parent == "" || b.Parent == "" || a.Parent != b.Parent {
		return false
	}
	aEnd := a.Offset + a.Bytes
	bEnd := b.Offset + b.Bytes
	return a.Offset < bEnd && b.Offset < aEnd
}

func (c *checker) validateArenaGraph() {
	seen := map[string]source.Span{}
	for _, arena := range c.graph.Arenas {
		if arena.Label == "" {
			continue
		}
		if prev, ok := seen[arena.Label]; ok {
			_ = prev
			c.error(arena.Span, diag.SEM0057, "duplicate arena identity "+arena.Label)
		}
		seen[arena.Label] = arena.Span
	}
	for i := range c.graph.Arenas {
		for j := i + 1; j < len(c.graph.Arenas); j++ {
			if arenaRangesOverlap(c.graph.Arenas[i], c.graph.Arenas[j]) {
				c.error(c.graph.Arenas[j].Span, diag.SEM0058, "statically overlapping arena placement")
			}
		}
	}
}

func (c *checker) recordArenaGraphCall(moduleName string, expr *ast.CallExpr, recvType *Type, scope *Scope, _ ContextKind) {
	if expr == nil || recvType == nil {
		return
	}
	receiverType := qualifiedTypeName(recvType)
	receiverOrigin := c.originForExprValue(moduleName, expr.Receiver, recvType, scope)
	switch receiverType {
	case "platform.hardware.memory.PhysicalRegionAuthority":
		c.recordCreateArenaFromRegion(expr, receiverOrigin)
	case "platform.hardware.memory.RootArena":
		switch expr.Method {
		case "child_at":
			c.recordRootArenaChildAt(expr, receiverOrigin)
		case "executor_memory":
			c.recordRootArenaExecutorMemory(moduleName, expr, receiverOrigin, scope)
		case "cache_arena":
			c.recordRootArenaCacheArena(expr, receiverOrigin)
		case "dma_buffer":
			c.recordRootArenaDMA(expr, receiverOrigin, scope)
		case "interrupt_queue":
			c.recordArenaInterruptQueue(moduleName, expr, receiverOrigin, scope)
		}
	case "platform.hardware.memory.ChildArena":
		if expr.Method == "child_at" {
			c.recordChildArenaChildAt(expr, receiverOrigin)
		} else if expr.Method == "interrupt_queue" {
			c.recordArenaInterruptQueue(moduleName, expr, receiverOrigin, scope)
		}
	}
}

func (c *checker) recordCreateArenaFromRegion(expr *ast.CallExpr, receiverOrigin localOrigin) {
	label, _ := arenaIdentityForArg(namedArgExpr(expr.Args, "identity"))
	c.graph.MemoryRoots = append(c.graph.MemoryRoots, MemoryRootNode{
		Label: label,
		Base:  receiverOrigin.ArenaBase,
		Bytes: receiverOrigin.ArenaBytes,
		Span:  expr.SpanV,
	})
	c.graph.Arenas = append(c.graph.Arenas, ArenaNode{
		Label:  label,
		Parent: "",
		Base:   receiverOrigin.ArenaBase,
		Offset: 0,
		Bytes:  receiverOrigin.ArenaBytes,
		Align:  receiverOrigin.ArenaAlign,
		Kind:   "root_arena",
		Span:   expr.SpanV,
	})
}

func (c *checker) recordRootArenaChildAt(expr *ast.CallExpr, receiverOrigin localOrigin) {
	label, labelOk := arenaIdentityForArg(namedArgExpr(expr.Args, "identity"))
	offset, hasOffset := arenaUnsignedIntArg(expr, "offset")
	length, hasLength := arenaUnsignedIntArg(expr, "length")
	align, hasAlign := arenaUnsignedIntArg(expr, "align")
	arena := ArenaNode{
		Label:  label,
		Parent: receiverOrigin.ArenaLabel,
		Kind:   "child_arena",
		Span:   expr.SpanV,
	}
	if labelOk && hasOffset && hasLength && hasAlign {
		arena.Offset = alignArenaOffset(offset, align)
		arena.Bytes = length
		arena.Align = align
		arena.Base = receiverOrigin.ArenaBase + arena.Offset
	}
	c.graph.Arenas = append(c.graph.Arenas, arena)
}

func (c *checker) recordChildArenaChildAt(expr *ast.CallExpr, receiverOrigin localOrigin) {
	label, labelOk := arenaIdentityForArg(namedArgExpr(expr.Args, "identity"))
	offset, hasOffset := arenaUnsignedIntArg(expr, "offset")
	length, hasLength := arenaUnsignedIntArg(expr, "length")
	align, hasAlign := arenaUnsignedIntArg(expr, "align")
	arena := ArenaNode{
		Label:  label,
		Parent: receiverOrigin.ArenaLabel,
		Kind:   "child_arena",
		Span:   expr.SpanV,
	}
	if labelOk && hasOffset && hasLength && hasAlign {
		arena.Offset = alignArenaOffset(offset, align)
		arena.Bytes = length
		arena.Align = align
		arena.Base = receiverOrigin.ArenaBase + arena.Offset
	}
	c.graph.Arenas = append(c.graph.Arenas, arena)
}

func (c *checker) recordRootArenaExecutorMemory(moduleName string, expr *ast.CallExpr, receiverOrigin localOrigin, scope *Scope) {
	owner := c.interruptQueueOwnerLabel(moduleName, namedArgExpr(expr.Args, "owner"), scope)
	c.graph.Arenas = append(c.graph.Arenas, ArenaNode{
		Label:  "",
		Parent: receiverOrigin.ArenaLabel,
		Owner:  owner,
		Kind:   "executor_memory",
		Span:   expr.SpanV,
	})
}

func (c *checker) recordRootArenaCacheArena(expr *ast.CallExpr, receiverOrigin localOrigin) {
	label, _ := arenaIdentityForArg(namedArgExpr(expr.Args, "identity"))
	c.graph.Arenas = append(c.graph.Arenas, ArenaNode{
		Label:  label,
		Parent: receiverOrigin.ArenaLabel,
		Owner:  receiverOrigin.ArenaLabel,
		Kind:   "cache_memory",
		Span:   expr.SpanV,
	})
}

func (c *checker) recordRootArenaDMA(expr *ast.CallExpr, receiverOrigin localOrigin, scope *Scope) {
	label, _ := arenaIdentityForArg(namedArgExpr(expr.Args, "identity"))
	owner := dmaOwnerIdentity(namedArgExpr(expr.Args, "owner"), scope)
	c.graph.DMABuffers = append(c.graph.DMABuffers, DMABufferNode{
		Label:       label,
		OwnerDevice: owner,
		Span:        expr.SpanV,
	})
	c.graph.Arenas = append(c.graph.Arenas, ArenaNode{
		Label:  label,
		Parent: receiverOrigin.ArenaLabel,
		Owner:  owner,
		Kind:   "dma_buffer",
		Span:   expr.SpanV,
	})
}

func (c *checker) recordArenaInterruptQueue(moduleName string, expr *ast.CallExpr, receiverOrigin localOrigin, scope *Scope) {
	overflow, overflowOK := interruptOverflowPolicy(namedArgExpr(expr.Args, "overflow"))
	if !overflowOK {
		c.error(expr.SpanV, diag.SEM0060, "interrupt queue overflow policy is missing or invalid")
	}
	label, _ := queueIdentityForArg(namedArgExpr(expr.Args, "identity"))
	owner := c.interruptQueueOwnerLabel(moduleName, namedArgExpr(expr.Args, "owner"), scope)
	capacity, _ := arenaUnsignedIntArg(expr, "capacity")
	payloadKind, payloadSize, payloadAlign := interruptPayloadKind(namedArgExpr(expr.Args, "payload"))
	c.graph.InterruptQueues = append(c.graph.InterruptQueues, InterruptQueueNode{
		Label:        label,
		Owner:        owner,
		Capacity:     capacity,
		PayloadKind:  payloadKind,
		PayloadSize:  payloadSize,
		PayloadAlign: payloadAlign,
		Overflow:     overflow,
		Span:         expr.SpanV,
	})
	c.graph.Arenas = append(c.graph.Arenas, ArenaNode{
		Label:  label,
		Parent: receiverOrigin.ArenaLabel,
		Owner:  owner,
		Kind:   "interrupt_queue",
		Span:   expr.SpanV,
	})
}

func dmaOwnerIdentity(expr ast.Expr, scope *Scope) string {
	key, ok := pciOriginKey(expr, scope)
	if ok {
		return key
	}
	return ""
}

func (c *checker) interruptQueueOwnerLabel(moduleName string, expr ast.Expr, scope *Scope) string {
	if label := c.slotLabelForExpr(moduleName, expr, scope); label != "" {
		return label
	}
	cons, ok := expr.(*ast.ConstructorExpr)
	if !ok || cons == nil || qualifiedTypeName(c.exprStaticType(moduleName, expr, scope)) != "machine.x86_64.cpu_state.ExecutorSlot" {
		return ""
	}
	id, ok := unsignedIntegerLiteral(constructorArg(cons, "id"))
	if !ok {
		return ""
	}
	return fmt.Sprintf("executor_slot.%d", id)
}

func queueIdentityForArg(expr ast.Expr) (string, bool) {
	identity, ok := expr.(*ast.ConstructorExpr)
	if !ok || identity == nil {
		return "", false
	}
	label, ok := stringLiteralArg(identity, "label")
	return label, ok
}

func interruptPayloadKind(expr ast.Expr) (string, uint64, uint64) {
	payload, ok := expr.(*ast.ConstructorExpr)
	if !ok || payload == nil {
		return "", 0, 0
	}
	kind, ok := unsignedIntegerLiteral(constructorArg(payload, "kind"))
	if !ok {
		return "", 0, 0
	}
	size, _ := unsignedIntegerLiteral(constructorArg(payload, "size"))
	align, _ := unsignedIntegerLiteral(constructorArg(payload, "align"))
	return fmt.Sprintf("kind:%d", kind), size, align
}

func interruptOverflowPolicy(expr ast.Expr) (string, bool) {
	overflow, ok := expr.(*ast.ConstructorExpr)
	if !ok || overflow == nil {
		return "", false
	}
	mode, ok := unsignedIntegerLiteral(constructorArg(overflow, "mode"))
	if !ok {
		return "", false
	}
	switch mode {
	case 0:
		return "drop_newest_and_set_flag", true
	case 1:
		return "drop_oldest_and_set_flag", true
	case 2:
		return "set_flag_and_wake", true
	case 3:
		return "boot_fatal", true
	default:
		return "", false
	}
}

func arenaIdentityForArg(expr ast.Expr) (string, bool) {
	identity, ok := expr.(*ast.ConstructorExpr)
	if !ok || identity == nil {
		return "", false
	}
	label, ok := stringLiteralArg(identity, "label")
	return label, ok
}

func arenaUnsignedIntArg(expr *ast.CallExpr, name string) (uint64, bool) {
	return unsignedIntegerLiteral(namedArgExpr(expr.Args, name))
}

func alignArenaOffset(offset, align uint64) uint64 {
	if align == 0 {
		return offset
	}
	return (offset + align - 1) & (0 - align)
}
