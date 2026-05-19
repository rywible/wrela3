package sem

import (
	"strconv"

	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/source"
)

type StorageIndex struct {
	EventsByTypeID   map[uint64]EventInfo
	EventsByKey      map[string]EventInfo
	ProjectionsByID  map[uint64]ProjectionInfo
	ProjectionsByKey map[string]ProjectionInfo
}

type EventInfo struct {
	Module          string
	Name            string
	EventTypeID     uint64
	Layouts         []EventLayoutInfo
	CurrentLayoutID uint64
	Span            source.Span
}

type EventLayoutInfo struct {
	ID           uint64
	Current      bool
	PayloadSize  uint64
	PayloadAlign uint64
	Span         source.Span
}

type ProjectionInfo struct {
	Module          string
	Name            string
	ProjectionID    uint64
	Layouts         []ProjectionLayoutInfo
	CurrentLayoutID uint64
	Span            source.Span
}

type ProjectionLayoutInfo struct {
	ID      uint64
	Current bool
	Fields  []ProjectionFieldInfo
	Span    source.Span
}

type ProjectionFieldInfo struct {
	Name          string
	ContainerKind string
	Type          *Type
	Span          source.Span
}

func (c *checker) checkStorageDecls() StorageIndex {
	storage := StorageIndex{
		EventsByTypeID:   map[uint64]EventInfo{},
		EventsByKey:      map[string]EventInfo{},
		ProjectionsByID:  map[uint64]ProjectionInfo{},
		ProjectionsByKey: map[string]ProjectionInfo{},
	}
	for _, mod := range c.modules {
		for _, decl := range mod.Decls {
			switch d := decl.(type) {
			case *ast.EventDecl:
				c.recordStorageEvent(storage, mod.Name, d)
			case *ast.ProjectionDecl:
				c.recordStorageProjection(storage, mod.Name, d)
			}
		}
	}
	return storage
}

func (c *checker) recordStorageWriterConstructor(moduleName string, expr *ast.ConstructorExpr, typ *Type, scope *Scope) {
	c.checkBlobCipherPolicy(moduleName, expr, typ)
	if !isStorageWriterType(typ) {
		return
	}
	fields := map[string]string{}
	pathRoles := map[string]string{}
	for _, name := range []string{"foreground", "background", "stream_directory", "metrics"} {
		arg := namedArgExpr(expr.Args, name)
		named, ok := arg.(*ast.NameExpr)
		if !ok {
			continue
		}
		if scope == nil {
			continue
		}
		origin, ok := scope.LookupOrigin(named.Name)
		if !ok || origin.Constructor == nil || !storageWriterArgMatches(name, origin.Type) {
			continue
		}
		fields[name] = named.Name
		if name == "foreground" || name == "background" {
			pathRoles[name] = c.storageWrapperPathRole(origin.Constructor, scope)
		}
	}
	_ = moduleName
	c.graph.StorageWriters = append(c.graph.StorageWriters, StorageWriterNode{
		Phase:        c.currentPhase,
		DirectFields: fields,
		PathRoles:    pathRoles,
		Span:         expr.SpanV,
	})
}

func (c *checker) checkBlobCipherPolicy(moduleName string, expr *ast.ConstructorExpr, typ *Type) {
	if qualifiedTypeName(typ) != "storage.blob.BlobCipherPolicy" {
		return
	}
	mode, ok := c.constValueOfExpr(moduleName, constructorArg(expr, "mode"))
	if !ok || mode != 1 {
		return
	}
	optIn, ok := constructorArg(expr, "development_opt_in").(*ast.BoolLiteral)
	if ok && optIn.Value {
		return
	}
	c.error(expr.SpanV, diag.SEM0123, "development blob cipher requires explicit opt in")
}

func (c *checker) checkProjectionAdvanceCall(moduleName string, expr *ast.CallExpr, receiverType *Type, scope *Scope) {
	if receiverType == nil || receiverType.Name != "ProjectionTruth" || expr.Method != "accept_advance" {
		return
	}
	frontier, ok := c.projectionTruthFrontier(moduleName, expr.Receiver, scope)
	if !ok {
		return
	}
	advanceExpr := namedArgExpr(expr.Args, "advance")
	through, ok := c.advanceProjectionThroughEventID(moduleName, advanceExpr, scope)
	if !ok {
		return
	}
	if through > frontier {
		c.error(expr.SpanV, diag.SEM0119, "projection root watermark is invalid")
	}
}

func (c *checker) projectionTruthFrontier(moduleName string, expr ast.Expr, scope *Scope) (uint64, bool) {
	cons := c.constructorForExpr(moduleName, expr, scope)
	return c.constValueOfExpr(moduleName, constructorArg(cons, "atomic_group_frontier"))
}

func (c *checker) advanceProjectionThroughEventID(moduleName string, expr ast.Expr, scope *Scope) (uint64, bool) {
	cons := c.constructorForExpr(moduleName, expr, scope)
	return c.constValueOfExpr(moduleName, constructorArg(cons, "through_event_id"))
}

func (c *checker) constructorForExpr(moduleName string, expr ast.Expr, scope *Scope) *ast.ConstructorExpr {
	switch e := expr.(type) {
	case *ast.ConstructorExpr:
		return e
	case *ast.NameExpr:
		if scope == nil {
			return nil
		}
		origin, ok := scope.LookupOrigin(e.Name)
		if !ok {
			return nil
		}
		return origin.Constructor
	default:
		typ := c.exprStaticType(moduleName, expr, scope)
		origin := c.originForExprValue(moduleName, expr, typ, scope)
		return origin.Constructor
	}
}

func storageWriterArgMatches(field string, typ *Type) bool {
	if typ == nil {
		return false
	}
	switch field {
	case "foreground":
		return typ.Name == "ForegroundStoragePath"
	case "background":
		return typ.Name == "BackgroundStoragePath"
	case "stream_directory":
		return typ.Name == "StreamDirectory"
	case "metrics":
		return typ.Name == "StorageMetrics"
	default:
		return false
	}
}

func (c *checker) recordStorageAppendCall(moduleName string, expr ast.Expr, scope *Scope, observed bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok || call.Method != "enqueue_atomic_group" {
		return
	}
	receiverType := c.exprStaticType(moduleName, call.Receiver, scope)
	if !isStorageWriterType(receiverType) {
		return
	}
	c.graph.StorageAppendCalls = append(c.graph.StorageAppendCalls, StorageAppendCallNode{
		ResultObserved: observed,
		Span:           call.SpanV,
	})
}

func (c *checker) checkStorageAuthority() {
	c.checkStoragePathOwnership()
	for _, writer := range c.graph.StorageWriters {
		if writer.Phase == "owned_hardware" && storageWriterHasRequiredFields(writer.DirectFields) {
			continue
		}
		c.error(writer.Span, diag.SEM0113, "StorageWriter authority cannot be forged or shared")
	}
	for _, call := range c.graph.StorageAppendCalls {
		if call.ResultObserved {
			continue
		}
		c.error(call.Span, diag.SEM0116, "storage append result must be observed")
	}
}

func storageWriterHasRequiredFields(fields map[string]string) bool {
	for _, name := range []string{"foreground", "background", "stream_directory", "metrics"} {
		if fields[name] == "" {
			return false
		}
	}
	return true
}

func isStorageWriterType(typ *Type) bool {
	return typ != nil && typ.Name == "StorageWriter"
}

func (c *checker) recordStorageEvent(storage StorageIndex, moduleName string, decl *ast.EventDecl) {
	id, err := strconv.ParseUint(decl.ID, 10, 64)
	if err != nil || id == 0 {
		c.error(decl.SpanV, diag.SEM0100, "invalid durable event type id "+decl.ID)
		return
	}
	layouts, currentLayoutID := c.checkEventLayouts(moduleName, decl)
	info := EventInfo{
		Module:          moduleName,
		Name:            decl.Name,
		EventTypeID:     id,
		Layouts:         layouts,
		CurrentLayoutID: currentLayoutID,
		Span:            decl.SpanV,
	}
	if _, ok := storage.EventsByTypeID[id]; ok {
		c.error(decl.SpanV, diag.SEM0099, "duplicate durable event type id")
		return
	}
	storage.EventsByTypeID[id] = info
	storage.EventsByKey[moduleName+"."+decl.Name] = info
}

func (c *checker) checkEventLayouts(moduleName string, decl *ast.EventDecl) ([]EventLayoutInfo, uint64) {
	layouts := make([]EventLayoutInfo, 0, len(decl.Layouts))
	layoutFields := map[uint64]map[string]bool{}
	eventFieldTypes := c.eventFieldTypes(moduleName, decl.Fields)
	seen := map[uint64]source.Span{}
	currentCount := 0
	var currentLayoutID uint64

	for _, layout := range decl.Layouts {
		id, err := strconv.ParseUint(layout.ID, 10, 64)
		if err != nil || id == 0 {
			c.error(layout.Span, diag.SEM0102, "layout id 0 is reserved")
			continue
		}
		if _, ok := seen[id]; ok {
			c.error(layout.Span, diag.SEM0102, "duplicate event layout id")
			continue
		}
		seen[id] = layout.Span
		if layout.Current {
			currentCount++
			currentLayoutID = id
		}
		payloadSize, payloadAlign, fieldNames := c.eventLayoutPayload(moduleName, layout, eventFieldTypes)
		layoutFields[id] = fieldNames
		layouts = append(layouts, EventLayoutInfo{
			ID:           id,
			Current:      layout.Current,
			PayloadSize:  payloadSize,
			PayloadAlign: payloadAlign,
			Span:         layout.Span,
		})
	}

	if len(layouts) == 1 && currentCount == 0 {
		layouts[0].Current = true
		currentLayoutID = layouts[0].ID
		currentCount = 1
	}
	if len(layouts) > 1 && currentCount != 1 {
		c.error(decl.SpanV, diag.SEM0101, "event with multiple layouts must mark exactly one current layout")
	}

	c.checkEventUpcasts(decl, seen, layoutFields)
	return layouts, currentLayoutID
}

func (c *checker) eventFieldTypes(moduleName string, fields []ast.Field) map[string]*Type {
	types := map[string]*Type{}
	for _, field := range fields {
		typ, ds := c.index.LookupTypeRef(moduleName, field.Type, nil)
		if len(ds) == 0 && typ != nil {
			types[field.Name] = typ
		}
	}
	return types
}

func (c *checker) eventLayoutPayload(moduleName string, layout ast.EventLayoutDecl, eventFieldTypes map[string]*Type) (uint64, uint64, map[string]bool) {
	fieldNames := map[string]bool{}
	var payloadSize uint64
	var payloadAlign uint64 = 1
	for _, field := range layout.Fields {
		fieldNames[field.Name] = true
		typ, ds := c.index.LookupTypeRef(moduleName, field.Type, nil)
		if len(ds) != 0 || typ == nil {
			c.error(field.Span, diag.SEM0103, "invalid event layout field "+field.Name)
			continue
		}
		if isUnpublishedBlobRefType(typ) || eventEncodeReferencesUnpublishedBlob(field.Encode, eventFieldTypes) {
			c.error(field.Span, diag.SEM0117, "event payload cannot reference unpublished blob bytes")
			continue
		}
		fieldLayout, ok := semanticFieldSizeAndStorage(typ, map[string]bool{})
		if !ok {
			c.error(field.Span, diag.SEM0103, "invalid event layout field "+field.Name)
			continue
		}
		payloadSize = alignPayloadOffset(payloadSize, fieldLayout.valueAlign)
		payloadSize += fieldLayout.storageSize
		if fieldLayout.valueAlign > payloadAlign {
			payloadAlign = fieldLayout.valueAlign
		}
	}
	payloadSize = alignPayloadOffset(payloadSize, payloadAlign)
	if payloadSize > 448 {
		c.error(layout.Span, diag.SEM0121, "event payload exceeds inline slot budget")
	}
	return payloadSize, payloadAlign, fieldNames
}

func isUnpublishedBlobRefType(typ *Type) bool {
	return qualifiedTypeName(typ) == "storage.blob.UnpublishedBlobRef"
}

func eventEncodeReferencesUnpublishedBlob(expr ast.Expr, eventFieldTypes map[string]*Type) bool {
	field, ok := expr.(*ast.FieldExpr)
	if !ok {
		return false
	}
	base, ok := field.Base.(*ast.NameExpr)
	if !ok || base.Name != "self" {
		return false
	}
	return isUnpublishedBlobRefType(eventFieldTypes[field.Field])
}

func (c *checker) checkEventUpcasts(decl *ast.EventDecl, layouts map[uint64]source.Span, layoutFields map[uint64]map[string]bool) {
	for _, upcast := range decl.Upcasts {
		fromID, fromErr := strconv.ParseUint(upcast.FromID, 10, 64)
		toID, toErr := strconv.ParseUint(upcast.ToID, 10, 64)
		if fromErr != nil || toErr != nil {
			c.error(upcast.Span, diag.SEM0104, "invalid event upcast endpoint")
			continue
		}
		if _, ok := layouts[fromID]; !ok {
			c.error(upcast.Span, diag.SEM0104, "invalid event upcast endpoint")
			continue
		}
		targetFields, ok := layoutFields[toID]
		if !ok {
			c.error(upcast.Span, diag.SEM0104, "invalid event upcast endpoint")
			continue
		}
		for _, mapping := range upcast.Mappings {
			if !targetFields[mapping.To] {
				c.error(mapping.Span, diag.SEM0105, "missing event upcast field mapping")
			}
		}
	}
}

func (c *checker) recordStorageProjection(storage StorageIndex, moduleName string, decl *ast.ProjectionDecl) {
	id, err := strconv.ParseUint(decl.ID, 10, 64)
	if err != nil || id == 0 {
		c.error(decl.SpanV, diag.SEM0106, "invalid projection id 0")
		return
	}
	layouts, currentLayoutID := c.checkProjectionLayouts(moduleName, decl)
	info := ProjectionInfo{
		Module:          moduleName,
		Name:            decl.Name,
		ProjectionID:    id,
		Layouts:         layouts,
		CurrentLayoutID: currentLayoutID,
		Span:            decl.SpanV,
	}
	if _, ok := storage.ProjectionsByID[id]; ok {
		c.error(decl.SpanV, diag.SEM0106, "duplicate projection id")
		return
	}
	storage.ProjectionsByID[id] = info
	storage.ProjectionsByKey[moduleName+"."+decl.Name] = info
}

func (c *checker) checkProjectionLayouts(moduleName string, decl *ast.ProjectionDecl) ([]ProjectionLayoutInfo, uint64) {
	layouts := make([]ProjectionLayoutInfo, 0, len(decl.Layouts))
	layoutFields := map[uint64]map[string]bool{}
	seen := map[uint64]source.Span{}
	currentCount := 0
	var currentLayoutID uint64

	for _, layout := range decl.Layouts {
		id, err := strconv.ParseUint(layout.ID, 10, 64)
		if err != nil || id == 0 {
			c.error(layout.Span, diag.SEM0107, "projection layout id 0 is reserved")
			continue
		}
		if _, ok := seen[id]; ok {
			c.error(layout.Span, diag.SEM0107, "duplicate projection layout id")
			continue
		}
		seen[id] = layout.Span
		if layout.Current {
			currentCount++
			currentLayoutID = id
		}

		fields, fieldNames := c.checkProjectionLayoutFields(moduleName, layout)
		layoutFields[id] = fieldNames
		layouts = append(layouts, ProjectionLayoutInfo{
			ID:      id,
			Current: layout.Current,
			Fields:  fields,
			Span:    layout.Span,
		})
	}

	if len(layouts) == 1 && currentCount == 0 {
		layouts[0].Current = true
		currentLayoutID = layouts[0].ID
		currentCount = 1
	}
	if len(layouts) > 1 && currentCount != 1 {
		c.error(decl.SpanV, diag.SEM0107, "projection with multiple layouts must mark exactly one current layout")
	}

	c.checkProjectionUpcasts(decl, seen, layoutFields)
	return layouts, currentLayoutID
}

func (c *checker) checkProjectionLayoutFields(moduleName string, layout ast.ProjectionLayoutDecl) ([]ProjectionFieldInfo, map[string]bool) {
	fields := make([]ProjectionFieldInfo, 0, len(layout.Fields))
	fieldNames := map[string]bool{}
	for _, field := range layout.Fields {
		fieldNames[field.Name] = true
		typ, ds := c.index.LookupTypeRef(moduleName, field.Type, nil)
		if len(ds) != 0 || typ == nil {
			c.error(field.Span, diag.SEM0108, "unsupported projection container")
			continue
		}
		containerKind, ok := projectionContainerKind(typ)
		if !ok {
			c.error(field.Span, diag.SEM0108, "unsupported projection container")
			continue
		}
		fields = append(fields, ProjectionFieldInfo{
			Name:          field.Name,
			ContainerKind: containerKind,
			Type:          typ,
			Span:          field.Span,
		})
	}
	return fields, fieldNames
}

func projectionContainerKind(typ *Type) (string, bool) {
	if typ == nil {
		return "", false
	}
	switch typ.Name {
	case "StateCell":
		return "StateCell", len(typ.TypeArgs) == 1
	case "DenseEntityMap":
		return "DenseEntityMap", len(typ.TypeArgs) == 2
	case "OrderedPages":
		return "OrderedPages", len(typ.TypeArgs) == 3
	default:
		return "", false
	}
}

func (c *checker) checkProjectionUpcasts(decl *ast.ProjectionDecl, layouts map[uint64]source.Span, layoutFields map[uint64]map[string]bool) {
	for _, upcast := range decl.Upcasts {
		fromID, fromErr := strconv.ParseUint(upcast.FromID, 10, 64)
		toID, toErr := strconv.ParseUint(upcast.ToID, 10, 64)
		if fromErr != nil || toErr != nil {
			c.error(upcast.Span, diag.SEM0109, "invalid projection upcast endpoint")
			continue
		}
		if _, ok := layouts[fromID]; !ok {
			c.error(upcast.Span, diag.SEM0109, "invalid projection upcast endpoint")
			continue
		}
		targetFields, ok := layoutFields[toID]
		if !ok {
			c.error(upcast.Span, diag.SEM0109, "invalid projection upcast endpoint")
			continue
		}
		for _, mapping := range upcast.Mappings {
			if !targetFields[mapping.To] {
				c.error(mapping.Span, diag.SEM0109, "invalid projection upcast endpoint")
			}
		}
	}
}
