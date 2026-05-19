package sem

import (
	"fmt"
	"strings"

	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/source"
)

type StoragePathNode struct {
	Label   string
	Role    string
	Owner   string
	QueueID uint16
	Vector  uint8
	Span    source.Span
}

type CoreLinkEndpointNode struct {
	Label     string
	Direction string
	Role      string
	Owner     string
	Peer      string
	Depth     uint64
	Span      source.Span
}

type ProjectionFeedNode struct {
	Projection  string
	SourceLabel string
	Owner       string
	Span        source.Span
}

type StorageWriterNode struct {
	Phase        string
	DirectFields map[string]string
	PathRoles    map[string]string
	Span         source.Span
}

type StorageAppendCallNode struct {
	ResultObserved bool
	Span           source.Span
}

func (c *checker) recordStoragePathConstructor(moduleName string, expr *ast.ConstructorExpr, typ *Type, scope *Scope) {
	if typ == nil || typ.Kind != KindDriverPath || typ.Name != "NvmeIoPath" {
		return
	}
	label := storagePathLabel(constructorArg(expr, "identity"))
	if label == "" {
		label = fmt.Sprintf("storage_path.%d", len(c.graph.StoragePaths))
	}
	role := storagePathRole(constructorArg(expr, "role"))
	owner := c.storagePathOwnerLabel(moduleName, constructorArg(expr, "owner"), scope)
	queueID, _ := unsignedIntegerLiteral(constructorArg(expr, "queue_id"))
	vector, _ := unsignedIntegerLiteral(constructorArg(expr, "vector"))
	c.graph.StoragePaths = append(c.graph.StoragePaths, StoragePathNode{
		Label:   label,
		Role:    role,
		Owner:   owner,
		QueueID: uint16(queueID),
		Vector:  uint8(vector),
		Span:    expr.SpanV,
	})
}

func (c *checker) storagePathOwnerLabel(moduleName string, expr ast.Expr, scope *Scope) string {
	if named, ok := expr.(*ast.NameExpr); ok && scope != nil {
		if origin, found := scope.LookupOrigin(named.Name); found && origin.SlotLabel != "" {
			return origin.SlotLabel
		}
	}
	if cons, ok := expr.(*ast.ConstructorExpr); ok {
		if id, found := unsignedIntegerLiteral(constructorArg(cons, "id")); found {
			return fmt.Sprintf("executor_slot.%d", id)
		}
	}
	if named, ok := expr.(*ast.NameExpr); ok && scope != nil {
		if origin, found := scope.LookupOrigin(named.Name); found && origin.Constructor != nil {
			if id, hasID := unsignedIntegerLiteral(constructorArg(origin.Constructor, "id")); hasID {
				return fmt.Sprintf("executor_slot.%d", id)
			}
		}
	}
	return c.slotLabelForExpr(moduleName, expr, scope)
}

func storagePathLabel(expr ast.Expr) string {
	identity, ok := expr.(*ast.ConstructorExpr)
	if !ok {
		return ""
	}
	label, _ := stringLiteralArg(identity, "label")
	return label
}

func storagePathRole(expr ast.Expr) string {
	role, ok := expr.(*ast.ConstructorExpr)
	if !ok {
		return ""
	}
	value, ok := constNameArg(role, "role")
	if !ok {
		return ""
	}
	switch value {
	case "NVME_PATH_FOREGROUND":
		return "foreground"
	case "NVME_PATH_BACKGROUND":
		return "background"
	default:
		return ""
	}
}

func constNameArg(expr *ast.ConstructorExpr, name string) (string, bool) {
	arg := constructorArg(expr, name)
	named, ok := arg.(*ast.NameExpr)
	if !ok {
		return "", false
	}
	return named.Name, true
}

func (c *checker) storageWrapperPathRole(expr *ast.ConstructorExpr, scope *Scope) string {
	if expr == nil || scope == nil {
		return ""
	}
	pathExpr := constructorArg(expr, "nvme_path")
	if pathExpr == nil {
		pathExpr = constructorArg(expr, "path")
	}
	named, ok := pathExpr.(*ast.NameExpr)
	if !ok {
		return ""
	}
	origin, ok := scope.LookupOrigin(named.Name)
	if !ok {
		return ""
	}
	label := origin.PathLabel
	for _, path := range c.graph.StoragePaths {
		if path.Label == label {
			return path.Role
		}
	}
	return ""
}

func (c *checker) checkStoragePathOwnership() {
	for _, writer := range c.graph.StorageWriters {
		if writer.PathRoles["foreground"] == "background" {
			c.error(writer.Span, diag.SEM0111, "foreground storage writer cannot use background NVMe path")
		}
		if writer.PathRoles["background"] == "foreground" {
			c.error(writer.Span, diag.SEM0111, "background storage path cannot use foreground NVMe path")
		}
	}
}

func (c *checker) checkStoragePathSubmitCall(moduleName string, expr *ast.CallExpr, receiverType *Type, scope *Scope) {
	if receiverType != nil && receiverType.Name == "MaintenanceWorker" && expr.Method == "submit" {
		if storageCallHasForegroundPathArg(moduleName, c, expr, scope) {
			c.error(expr.SpanV, diag.SEM0111, "maintenance worker cannot submit through foreground NVMe path")
		}
		return
	}
	if c.currentType == nil || c.currentType.Name != "MaintenanceWorker" || !strings.HasPrefix(expr.Method, "submit_") {
		return
	}
	if storageReceiverIsForegroundPath(moduleName, c, expr.Receiver, scope) {
		c.error(expr.SpanV, diag.SEM0111, "maintenance worker cannot submit through foreground NVMe path")
	}
}

func storageCallHasForegroundPathArg(moduleName string, c *checker, expr *ast.CallExpr, scope *Scope) bool {
	for _, arg := range expr.Args {
		if typ := c.exprStaticType(moduleName, arg.Value, scope); typ != nil && typ.Name == "ForegroundStoragePath" {
			return true
		}
	}
	return false
}

func storageReceiverIsForegroundPath(moduleName string, c *checker, expr ast.Expr, scope *Scope) bool {
	field, ok := expr.(*ast.FieldExpr)
	if !ok {
		return false
	}
	baseType := c.exprStaticType(moduleName, field.Base, scope)
	return baseType != nil && baseType.Name == "ForegroundStoragePath"
}
