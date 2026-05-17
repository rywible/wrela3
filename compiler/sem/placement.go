package sem

import (
	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/source"
)

var hiddenSchedulerNames = map[string]bool{
	"Scheduler":        true,
	"RunnableQueue":    true,
	"migrate":          true,
	"work_steal":       true,
	"spawn_on_any_cpu": true,
}

func (c *checker) checkHiddenSchedulerVocabulary() {
	for _, module := range c.modules {
		for _, decl := range module.Decls {
			switch d := decl.(type) {
			case *ast.DataDecl:
				c.checkHiddenSchedulerTypeName(d.Name, d.SpanV)
				c.checkHiddenSchedulerMethods(d.Methods)
			case *ast.ClassDecl:
				c.checkHiddenSchedulerTypeName(d.Name, d.SpanV)
				c.checkHiddenSchedulerMethods(d.Methods)
			case *ast.DriverDecl:
				c.checkHiddenSchedulerTypeName(d.Name, d.SpanV)
				c.checkHiddenSchedulerMethods(d.Methods)
			case *ast.DriverPathDecl:
				c.checkHiddenSchedulerTypeName(d.Name, d.SpanV)
				c.checkHiddenSchedulerMethods(d.Methods)
			case *ast.ExecutorDecl:
				c.checkHiddenSchedulerTypeName(d.Name, d.SpanV)
				c.checkHiddenSchedulerMethods(d.Methods)
			}
		}
	}
}

func (c *checker) checkHiddenSchedulerTypeName(name string, span source.Span) {
	if hiddenSchedulerNames[name] {
		c.error(span, diag.SEM0067, "hidden scheduler construct is not allowed")
	}
}

func (c *checker) checkHiddenSchedulerMethods(methods []ast.MethodDecl) {
	for _, method := range methods {
		if hiddenSchedulerNames[method.Name] {
			c.error(method.SpanV, diag.SEM0067, "hidden scheduler construct is not allowed")
		}
	}
}

func (c *checker) recordPlacementGraphCall(moduleName string, expr *ast.CallExpr, recvType *Type, scope *Scope) {
	if expr == nil || qualifiedTypeName(recvType) != "machine.x86_64.cpu_state.CpuPlacementPlan" {
		return
	}
	switch expr.Method {
	case "require_separate_physical_cores":
		c.recordPlacementConstraint(moduleName, expr, scope, "separate_physical_cores", true, "")
	case "prefer_same_cache_group":
		c.recordPlacementConstraint(moduleName, expr, scope, "same_cache_group", false, "unknown_locality")
	case "prefer_near_device":
		c.recordPlacementConstraint(moduleName, expr, scope, "near_device", false, "unknown_locality")
	case "cpu_for":
		c.graph.PlacementDecisions = append(c.graph.PlacementDecisions, PlacementDecisionNode{
			SlotLabel: c.slotLabelForExpr(moduleName, namedArgExpr(expr.Args, "slot"), scope),
			Target:    "cpu",
			Satisfied: false,
			Fallback:  "unknown_locality",
			Span:      expr.SpanV,
		})
	}
}

func (c *checker) recordPlacementConstraint(moduleName string, expr *ast.CallExpr, scope *Scope, kind string, required bool, fallback string) {
	a := c.slotLabelForExpr(moduleName, namedArgExpr(expr.Args, "a"), scope)
	b := c.slotLabelForExpr(moduleName, namedArgExpr(expr.Args, "b"), scope)
	if kind == "near_device" {
		a = c.slotLabelForExpr(moduleName, namedArgExpr(expr.Args, "slot"), scope)
		b, _ = pciOriginKey(namedArgExpr(expr.Args, "device"), scope)
	}
	c.graph.PlacementConstraints = append(c.graph.PlacementConstraints, PlacementConstraintNode{
		Kind:      kind,
		A:         a,
		B:         b,
		Required:  required,
		Satisfied: false,
		Fallback:  fallback,
		Span:      expr.SpanV,
	})
}

func (c *checker) finalizePlacementConstraints() {
	slotVCPU := map[string]int{}
	for _, placement := range c.graph.VcpuPlacements {
		if placement.SlotLabel == "" {
			continue
		}
		slotVCPU[placement.SlotLabel] = placement.VcpuID
	}
	for i := range c.graph.PlacementConstraints {
		constraint := &c.graph.PlacementConstraints[i]
		constraint.A = c.resolveExecutorSeedLabel(constraint.A)
		constraint.B = c.resolveExecutorSeedLabel(constraint.B)
		constraint.Satisfied = placementConstraintSatisfied(*constraint, slotVCPU)
		if constraint.Required && !constraint.Satisfied && constraint.Fallback == "" {
			constraint.Fallback = "boot_fatal"
		}
	}
}

func placementConstraintSatisfied(constraint PlacementConstraintNode, slotVCPU map[string]int) bool {
	if constraint.Kind != "separate_physical_cores" || !constraint.Required {
		return false
	}
	if constraint.A == "" || constraint.B == "" || constraint.A == constraint.B {
		return false
	}
	aVCPU, aOK := slotVCPU[constraint.A]
	bVCPU, bOK := slotVCPU[constraint.B]
	return aOK && bOK && aVCPU != bVCPU
}
