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
	vector, _ := storagePathVectorValue(constructorArg(expr, "vector"))
	c.graph.StoragePaths = append(c.graph.StoragePaths, StoragePathNode{
		Label:   label,
		Role:    role,
		Owner:   owner,
		QueueID: uint16(queueID),
		Vector:  uint8(vector),
		Span:    expr.SpanV,
	})
	topicKind, eventType, eventFunctionSymbol := pathRouteMetadata(typ)
	if eventType != "" {
		if publisher := c.pathPublisherOrigin(moduleName, expr, scope); publisher.Type != nil && publisher.TopicLabel != "" {
			c.recordInterruptTopicRoute(InterruptTopicRouteNode{
				Vector:              int(vector),
				PathLabel:           label,
				ContextSymbol:       interruptContextSymbol(label),
				TopicLabel:          publisher.TopicLabel,
				TopicKind:           topicKind,
				EventType:           eventType,
				EventFunctionSymbol: eventFunctionSymbol,
				Span:                expr.SpanV,
			})
		}
	}
}

func (c *checker) recordNestedInterruptPathRoutes(binding string, origin localOrigin, span source.Span) {
	if binding == "" || origin.Type == nil || len(origin.FieldOrigins) == 0 {
		return
	}
	if !isNvmeStoragePathWrapper(origin.Type) {
		return
	}
	for fieldName, fieldOrigin := range origin.FieldOrigins {
		if fieldOrigin.Type == nil || fieldOrigin.Type.Kind != KindDriverPath {
			continue
		}
		if fieldOrigin.EventType == "" || fieldOrigin.TopicLabel == "" {
			continue
		}
		vector := fieldOrigin.PathVector
		if vector == 0 {
			for _, path := range c.graph.StoragePaths {
				if path.Label == fieldOrigin.PathLabel {
					vector = int(path.Vector)
					break
				}
			}
		}
		publishes := fieldOrigin.PublishesInterrupts || fieldOrigin.TopicLabel != ""
		c.graph.Paths = append(c.graph.Paths, PathNode{
			Label:               fieldOrigin.PathLabel,
			Kind:                fieldOrigin.TopicKind,
			Binding:             binding,
			PublishesInterrupts: publishes,
			Span:                span,
		})
		c.recordInterruptTopicRoute(InterruptTopicRouteNode{
			Vector:              vector,
			PathLabel:           fieldOrigin.PathLabel,
			PathBinding:         binding,
			PathBindingType:     origin.Type,
			PathField:           fieldName,
			ContextSymbol:       interruptContextSymbol(fieldOrigin.PathLabel),
			TopicLabel:          fieldOrigin.TopicLabel,
			TopicKind:           fieldOrigin.TopicKind,
			EventType:           fieldOrigin.EventType,
			EventFunctionSymbol: fieldOrigin.EventFunctionSymbol,
			Span:                span,
		})
	}
}

func isNvmeStoragePathWrapper(typ *Type) bool {
	if typ == nil || typ.Module != "machine.x86_64.nvme" {
		return false
	}
	return typ.Name == "ForegroundStoragePath" || typ.Name == "BackgroundStoragePath"
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
	depth := c.storageGraphUintArg(moduleName, constructorArg(expr, "capacity"))
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

func (c *checker) storageGraphUintArg(moduleName string, expr ast.Expr) uint64 {
	if value, ok := unsignedIntegerLiteral(expr); ok {
		return value
	}
	if named, ok := expr.(*ast.NameExpr); ok {
		if value, ok := c.index.LookupConst(moduleName, named.Name); ok && value.Type != nil {
			return value.Value
		}
	}
	return 0
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

func storagePathVectorValue(expr ast.Expr) (uint64, bool) {
	if value, ok := unsignedIntegerLiteral(expr); ok {
		return value, true
	}
	field, ok := expr.(*ast.FieldExpr)
	if !ok {
		return 0, false
	}
	switch field.Field {
	case "foreground_vector":
		return 0x50, true
	case "background_vector":
		return 0x51, true
	default:
		return 0, false
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

func (c *checker) storageWrapperPathRole(moduleName string, expr *ast.ConstructorExpr, scope *Scope) string {
	if expr == nil || scope == nil {
		return ""
	}
	pathExpr := constructorArg(expr, "nvme_path")
	if pathExpr == nil {
		pathExpr = constructorArg(expr, "path")
	}
	pathType := c.exprStaticType(moduleName, pathExpr, scope)
	pathOrigin := c.originForExprValue(moduleName, pathExpr, pathType, scope)
	return c.storagePathRoleForOrigin(pathOrigin)
}

func (c *checker) storagePathRoleForOrigin(origin localOrigin) string {
	if origin.PathLabel != "" {
		for _, path := range c.graph.StoragePaths {
			if path.Label == origin.PathLabel {
				return path.Role
			}
		}
	}
	if origin.Constructor == nil {
		return ""
	}
	return storagePathRole(constructorArg(origin.Constructor, "role"))
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
	c.checkCoreLinkEndpointCall(moduleName, expr, receiverType, scope)
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

func (c *checker) checkCoreLinkEndpointCall(moduleName string, expr *ast.CallExpr, receiverType *Type, scope *Scope) {
	role := ""
	switch {
	case receiverType != nil && receiverType.Module == "machine.x86_64.core_link" && receiverType.Name == "CoreSpscProducer" && expr.Method == "try_send":
		role = "producer"
	case receiverType != nil && receiverType.Module == "machine.x86_64.core_link" && receiverType.Name == "CoreSpscConsumer" && (expr.Method == "try_next" || expr.Method == "arm_wait"):
		role = "consumer"
	default:
		return
	}
	endpoint, ok := c.coreLinkEndpointForReceiver(moduleName, expr.Receiver, receiverType, role, scope)
	c.graph.CoreLinkEndpointUses = append(c.graph.CoreLinkEndpointUses, CoreLinkEndpointUseNode{
		Role:         role,
		Owner:        c.currentExecutorSlotLabel(),
		Endpoint:     endpoint,
		EndpointOK:   ok,
		ReceiverType: receiverType,
		ExecutorType: c.currentType,
		FieldName:    selfFieldName(expr.Receiver),
		Span:         expr.SpanV,
	})
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

func (c *checker) checkCoreLinkEndpointOwnership() {
	for _, use := range c.graph.CoreLinkEndpointUses {
		if use.FieldName != "" && use.ExecutorType != nil {
			resolved := false
			for _, exec := range c.graph.Executors {
				if exec.Type == nil || exec.Type.Key() != use.ExecutorType.Key() {
					continue
				}
				endpoint, ok := c.coreLinkEndpointForExecutorNodeField(use.Role, exec, use.FieldName)
				if !ok {
					continue
				}
				resolved = true
				c.checkCoreLinkEndpointUse(use, exec.SlotLabel, endpoint, ok)
			}
			if resolved {
				continue
			}
		}
		owner := use.Owner
		if owner == "" {
			owner = c.executorSlotForType(use.ExecutorType)
		}
		c.checkCoreLinkEndpointUse(use, owner, use.Endpoint, use.EndpointOK)
	}
}

func (c *checker) checkCoreLinkEndpointUse(use CoreLinkEndpointUseNode, owner string, endpoint CoreLinkEndpointNode, endpointOK bool) {
	if !endpointOK || endpoint.Owner == "" || endpoint.Owner == owner {
		return
	}
	c.error(use.Span, diag.SEM0112, use.Role+" endpoint owned by "+endpoint.Owner+" cannot be used by executor "+owner)
}

func (c *checker) executorSlotForType(typ *Type) string {
	if c.currentType != nil && c.currentType.Kind == KindExecutor {
		for _, exec := range c.graph.Executors {
			if exec.Type != nil && exec.Type.Key() == c.currentType.Key() {
				return exec.SlotLabel
			}
		}
	}
	if typ == nil {
		return ""
	}
	for _, exec := range c.graph.Executors {
		if exec.Type != nil && exec.Type.Key() == typ.Key() {
			return exec.SlotLabel
		}
	}
	return ""
}

func (c *checker) coreLinkEndpointForExecutorField(role string, typ *Type, fieldName string) (CoreLinkEndpointNode, bool) {
	if typ == nil || fieldName == "" {
		return CoreLinkEndpointNode{}, false
	}
	for _, exec := range c.graph.Executors {
		if exec.Type == nil || exec.Type.Key() != typ.Key() {
			continue
		}
		if endpoint, ok := c.coreLinkEndpointForExecutorNodeField(role, exec, fieldName); ok {
			return endpoint, true
		}
	}
	return CoreLinkEndpointNode{}, false
}

func (c *checker) coreLinkEndpointForExecutorNodeField(role string, exec ExecutorNode, fieldName string) (CoreLinkEndpointNode, bool) {
	if origin, ok := exec.fieldOrigins[fieldName]; ok {
		return c.coreLinkEndpointForOrigin(role, origin)
	}
	if binding := exec.FieldBindings[fieldName]; binding != "" {
		return c.coreLinkEndpointForOrigin(role, localOrigin{FieldBindings: map[string]string{"self": binding}})
	}
	return CoreLinkEndpointNode{}, false
}

func (c *checker) coreLinkEndpointForReceiver(moduleName string, receiver ast.Expr, receiverType *Type, role string, scope *Scope) (CoreLinkEndpointNode, bool) {
	origin := c.originForExprValue(moduleName, receiver, receiverType, scope)
	if origin.Constructor != nil {
		return c.coreLinkEndpointForConstructor(role, origin.Constructor)
	}
	if field, ok := receiver.(*ast.FieldExpr); ok {
		if origin, ok := c.currentExecutorFieldOrigin(field); ok {
			return c.coreLinkEndpointForOrigin(role, origin)
		}
	}
	return CoreLinkEndpointNode{}, false
}

func (c *checker) coreLinkEndpointForOrigin(role string, origin localOrigin) (CoreLinkEndpointNode, bool) {
	if origin.Constructor != nil {
		return c.coreLinkEndpointForConstructor(role, origin.Constructor)
	}
	if origin.FieldBindings != nil {
		if binding := origin.FieldBindings["self"]; binding != "" {
			return c.coreLinkEndpointForBinding(role, binding)
		}
	}
	return CoreLinkEndpointNode{}, false
}

func (c *checker) coreLinkEndpointForConstructor(role string, constructor *ast.ConstructorExpr) (CoreLinkEndpointNode, bool) {
	if constructor == nil {
		return CoreLinkEndpointNode{}, false
	}
	for _, endpoint := range c.graph.CoreLinkEndpoints {
		if endpoint.Role == role && endpoint.Span.Start == constructor.SpanV.Start && endpoint.Span.End == constructor.SpanV.End {
			return endpoint, true
		}
	}
	return CoreLinkEndpointNode{}, false
}

func (c *checker) currentExecutorFieldOrigin(field *ast.FieldExpr) (localOrigin, bool) {
	if c.currentType == nil || field == nil || field.Field == "" {
		return localOrigin{}, false
	}
	base, ok := field.Base.(*ast.NameExpr)
	if !ok || base.Name != "self" {
		return localOrigin{}, false
	}
	for _, exec := range c.graph.Executors {
		if exec.Type != nil && exec.Type.Key() == c.currentType.Key() {
			if origin, ok := exec.fieldOrigins[field.Field]; ok {
				return origin, true
			}
			if binding := exec.FieldBindings[field.Field]; binding != "" {
				return localOrigin{FieldBindings: map[string]string{"self": binding}}, true
			}
		}
	}
	return localOrigin{}, false
}

func selfFieldName(expr ast.Expr) string {
	field, ok := expr.(*ast.FieldExpr)
	if !ok || field.Field == "" {
		return ""
	}
	base, ok := field.Base.(*ast.NameExpr)
	if !ok || base.Name != "self" {
		return ""
	}
	return field.Field
}

func (c *checker) coreLinkEndpointForBinding(role string, binding string) (CoreLinkEndpointNode, bool) {
	if binding == "" {
		return CoreLinkEndpointNode{}, false
	}
	for _, module := range c.modules {
		for _, decl := range module.Decls {
			image, ok := decl.(*ast.ImageDecl)
			if !ok {
				continue
			}
			if endpoint, ok := c.coreLinkEndpointForBindingInPhases(role, binding, image.Phases); ok {
				return endpoint, true
			}
		}
	}
	return CoreLinkEndpointNode{}, false
}

func (c *checker) coreLinkEndpointForBindingInPhases(role string, binding string, phases []ast.PhaseDecl) (CoreLinkEndpointNode, bool) {
	for _, phase := range phases {
		if endpoint, ok := c.coreLinkEndpointForBindingInStmts(role, binding, phase.Body, map[string]bool{}); ok {
			return endpoint, true
		}
	}
	return CoreLinkEndpointNode{}, false
}

func (c *checker) coreLinkEndpointForBindingInStmts(role string, binding string, stmts []ast.Stmt, seen map[string]bool) (CoreLinkEndpointNode, bool) {
	if binding == "" {
		return CoreLinkEndpointNode{}, false
	}
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *ast.LetStmt:
			if s.Name == binding {
				if constructor, ok := s.Expr.(*ast.ConstructorExpr); ok {
					return c.coreLinkEndpointForConstructor(role, constructor)
				}
				if alias, ok := s.Expr.(*ast.NameExpr); ok {
					if seen[binding] {
						return CoreLinkEndpointNode{}, false
					}
					nextSeen := cloneBindingSeen(seen)
					nextSeen[binding] = true
					return c.coreLinkEndpointForBindingInStmts(role, alias.Name, stmts, nextSeen)
				}
			}
		case *ast.IfStmt:
			if endpoint, ok := c.coreLinkEndpointForBindingInStmts(role, binding, s.Then, seen); ok {
				return endpoint, true
			}
			if endpoint, ok := c.coreLinkEndpointForBindingInStmts(role, binding, s.Else, seen); ok {
				return endpoint, true
			}
		case *ast.WithStmt:
			if endpoint, ok := c.coreLinkEndpointForBindingInStmts(role, binding, s.Body, seen); ok {
				return endpoint, true
			}
		case *ast.WhileStmt:
			if endpoint, ok := c.coreLinkEndpointForBindingInStmts(role, binding, s.Body, seen); ok {
				return endpoint, true
			}
		case *ast.ForStmt:
			if endpoint, ok := c.coreLinkEndpointForBindingInStmts(role, binding, s.Body, seen); ok {
				return endpoint, true
			}
		case *ast.MatchStmt:
			for _, arm := range s.Arms {
				if endpoint, ok := c.coreLinkEndpointForBindingInStmts(role, binding, arm.Body, seen); ok {
					return endpoint, true
				}
			}
		case *ast.IfLetStmt:
			if endpoint, ok := c.coreLinkEndpointForBindingInStmts(role, binding, s.Body, seen); ok {
				return endpoint, true
			}
		}
	}
	return CoreLinkEndpointNode{}, false
}

func cloneBindingSeen(seen map[string]bool) map[string]bool {
	next := map[string]bool{}
	for key, value := range seen {
		next[key] = value
	}
	return next
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
