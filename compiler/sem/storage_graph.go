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
	c.recordCoreLinkEndpointConstructor(moduleName, expr, typ, scope)
	c.recordProjectionFeedConstructor(moduleName, expr, typ, scope)
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

func (c *checker) recordCoreLinkEndpointConstructor(moduleName string, expr *ast.ConstructorExpr, typ *Type, scope *Scope) {
	if typ == nil || typ.Module != "machine.x86_64.core_link" {
		return
	}
	role := ""
	direction := ""
	switch typ.Name {
	case "CoreSpscProducer":
		role = "producer"
		direction = "tx"
	case "CoreSpscConsumer":
		role = "consumer"
		direction = "rx"
	default:
		return
	}
	owner := c.storagePathOwnerLabel(moduleName, constructorArg(expr, "owner"), scope)
	peer := c.storagePathOwnerLabel(moduleName, constructorArg(expr, "peer"), scope)
	depth, _ := unsignedIntegerLiteral(constructorArg(expr, "capacity"))
	c.graph.CoreLinkEndpoints = append(c.graph.CoreLinkEndpoints, CoreLinkEndpointNode{
		Label:     fmt.Sprintf("core_link.%s.%d", role, len(c.graph.CoreLinkEndpoints)),
		Direction: direction,
		Role:      role,
		Owner:     owner,
		Peer:      peer,
		Depth:     depth,
		Span:      expr.SpanV,
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
	c.checkProjectionFeeds()
}

func (c *checker) recordProjectionFeedConstructor(moduleName string, expr *ast.ConstructorExpr, typ *Type, scope *Scope) {
	projection := projectionWorkerProjection(typ)
	if projection == "" {
		return
	}
	sourceArg := projectionWorkerSourceArg(expr, typ)
	sourceType := c.exprStaticType(moduleName, sourceArg, scope)
	if !isCommittedGroupConsumer(sourceType) {
		c.error(expr.SpanV, diag.SEM0120, "projection worker feed is not boot wired")
		return
	}
	sourceOrigin := c.originForExprValue(moduleName, sourceArg, sourceType, scope)
	if sourceOrigin.Constructor == nil {
		c.error(expr.SpanV, diag.SEM0120, "projection worker feed is not boot wired")
		return
	}
	owner := c.storagePathOwnerLabel(moduleName, constructorArg(sourceOrigin.Constructor, "owner"), scope)
	c.graph.ProjectionFeeds = append(c.graph.ProjectionFeeds, ProjectionFeedNode{
		Projection:  projection,
		SourceLabel: coreLinkEndpointLabel("consumer", owner),
		Owner:       owner,
		Span:        expr.SpanV,
	})
}

func (c *checker) checkProjectionFeeds() {
	for _, exec := range c.graph.Executors {
		c.checkProjectionWorkerFeed(exec.Type, exec.Span)
	}
	for _, node := range c.graph.Constructed {
		c.checkProjectionWorkerFeed(node.Type, node.Span)
	}
	for _, feed := range c.graph.ProjectionFeeds {
		if feed.Owner != "maintenance" || !c.projectionFeedHasProducer(feed.Owner) {
			c.error(feed.Span, diag.SEM0120, "projection worker feed is not boot wired")
		}
	}
}

func (c *checker) checkProjectionWorkerFeed(typ *Type, span source.Span) {
	projection := projectionWorkerProjection(typ)
	if projection == "" || c.hasProjectionFeed(projection) {
		return
	}
	c.error(span, diag.SEM0120, "projection worker feed is not boot wired")
}

func (c *checker) hasProjectionFeed(projection string) bool {
	for _, feed := range c.graph.ProjectionFeeds {
		if feed.Projection == projection {
			return true
		}
	}
	return false
}

func (c *checker) projectionFeedHasProducer(owner string) bool {
	for _, endpoint := range c.graph.CoreLinkEndpoints {
		if endpoint.Role == "producer" && endpoint.Peer == owner {
			return true
		}
	}
	return false
}

func projectionWorkerProjection(typ *Type) string {
	if typ == nil || (typ.Kind != KindClass && typ.Kind != KindExecutor) {
		return ""
	}
	for _, field := range typ.Fields {
		if field.Type == nil || field.Type.Name != "ProjectionWriter" || len(field.Type.TypeArgs) != 1 {
			continue
		}
		projection := field.Type.TypeArgs[0]
		if projection != nil && projection.Kind == KindProjection {
			return qualifiedTypeName(projection)
		}
	}
	return ""
}

func projectionWorkerSourceArg(expr *ast.ConstructorExpr, typ *Type) ast.Expr {
	if typ == nil {
		return nil
	}
	for _, field := range typ.Fields {
		if isCommittedGroupConsumer(field.Type) {
			return constructorArg(expr, field.Name)
		}
	}
	return nil
}

func isCommittedGroupConsumer(typ *Type) bool {
	return typ != nil &&
		qualifiedTypeName(typ) == "machine.x86_64.core_link.CoreSpscConsumer" &&
		len(typ.TypeArgs) == 1 &&
		qualifiedTypeName(typ.TypeArgs[0]) == "storage.writer.CommittedAtomicGroup"
}

func coreLinkEndpointLabel(role string, owner string) string {
	if owner == "" {
		return ""
	}
	return "core_link." + role + "." + owner
}

func (c *checker) checkStoragePathSubmitCall(moduleName string, expr *ast.CallExpr, receiverType *Type, scope *Scope) {
	c.checkCoreLinkEndpointCall(expr, receiverType)
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

func (c *checker) checkCoreLinkEndpointCall(expr *ast.CallExpr, receiverType *Type) {
	role := ""
	switch {
	case receiverType != nil && receiverType.Module == "machine.x86_64.core_link" && receiverType.Name == "CoreSpscProducer" && expr.Method == "try_send":
		role = "producer"
	case receiverType != nil && receiverType.Module == "machine.x86_64.core_link" && receiverType.Name == "CoreSpscConsumer" && (expr.Method == "try_next" || expr.Method == "arm_wait"):
		role = "consumer"
	default:
		return
	}
	owner := c.currentExecutorSlotLabel()
	if owner == "" {
		return
	}
	endpoint, ok := c.singleCoreLinkEndpoint(role)
	if !ok || endpoint.Owner == "" || endpoint.Owner == owner {
		return
	}
	c.error(expr.SpanV, diag.SEM0112, role+" endpoint owned by "+endpoint.Owner+" cannot be used by executor "+owner)
}

func (c *checker) currentExecutorSlotLabel() string {
	if c.currentType == nil || c.currentType.Kind != KindExecutor {
		return ""
	}
	for _, exec := range c.graph.Executors {
		if exec.Type != nil && exec.Type.Key() == c.currentType.Key() {
			return exec.SlotLabel
		}
	}
	return ""
}

func (c *checker) singleCoreLinkEndpoint(role string) (CoreLinkEndpointNode, bool) {
	var out CoreLinkEndpointNode
	for _, endpoint := range c.graph.CoreLinkEndpoints {
		if endpoint.Role != role {
			continue
		}
		if out.Role != "" {
			return CoreLinkEndpointNode{}, false
		}
		out = endpoint
	}
	return out, out.Role != ""
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
