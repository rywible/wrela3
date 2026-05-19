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
	Module      string
	Name        string
	EventTypeID uint64
	Span        source.Span
}

type ProjectionInfo struct {
	Module       string
	Name         string
	ProjectionID uint64
	Span         source.Span
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

func (c *checker) recordStorageEvent(storage StorageIndex, moduleName string, decl *ast.EventDecl) {
	id, err := strconv.ParseUint(decl.ID, 10, 64)
	if err != nil || id == 0 {
		c.error(decl.SpanV, diag.SEM0100, "invalid durable event type id "+decl.ID)
		return
	}
	info := EventInfo{
		Module:      moduleName,
		Name:        decl.Name,
		EventTypeID: id,
		Span:        decl.SpanV,
	}
	if _, ok := storage.EventsByTypeID[id]; ok {
		c.error(decl.SpanV, diag.SEM0099, "duplicate durable event type id")
		return
	}
	storage.EventsByTypeID[id] = info
	storage.EventsByKey[moduleName+"."+decl.Name] = info
}

func (c *checker) recordStorageProjection(storage StorageIndex, moduleName string, decl *ast.ProjectionDecl) {
	id, err := strconv.ParseUint(decl.ID, 10, 64)
	if err != nil || id == 0 {
		c.error(decl.SpanV, diag.SEM0106, "invalid projection id 0")
		return
	}
	info := ProjectionInfo{
		Module:       moduleName,
		Name:         decl.Name,
		ProjectionID: id,
		Span:         decl.SpanV,
	}
	if _, ok := storage.ProjectionsByID[id]; ok {
		c.error(decl.SpanV, diag.SEM0106, "duplicate projection id")
		return
	}
	storage.ProjectionsByID[id] = info
	storage.ProjectionsByKey[moduleName+"."+decl.Name] = info
}
