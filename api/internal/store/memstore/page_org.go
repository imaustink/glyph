package memstore

import (
	"context"
	"fmt"
	"time"

	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
	"github.com/google/uuid"
)

// UpdateFieldsMovingOrg mirrors the Postgres implementation: it refuses a
// subtree holding another owner's page (ErrSubtreeNotOwned) and otherwise
// moves the page, its descendants and their tasks (sourced from them or on
// their folder boards) to
// p.OrgID and writes the page's fields, all under the one lock, so a failed
// write (ErrCycle) changes no org.
func (s *pageStore) UpdateFieldsMovingOrg(_ context.Context, p *model.Page, fields []string) (*model.Page, error) {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	existing, ok := s.r.pages[p.ID]
	if !ok || existing.UserID != p.UserID {
		return nil, fmt.Errorf("pages update: %w", store.ErrNotFound)
	}
	for _, f := range fields {
		if f == "parentId" && s.r.wouldCycle(p.ID, p.ParentID) {
			return nil, store.ErrCycle
		}
	}
	ids := s.r.subtreeIDs(p.ID)
	for _, id := range ids {
		if s.r.pages[id].UserID != p.UserID {
			return nil, store.ErrSubtreeNotOwned
		}
	}
	// Nothing below can fail, so the cascade and the field write land
	// together.
	now := time.Now()
	subtree := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		subtree[id] = true
		pg := s.r.pages[id]
		pg.OrgID = copyUUIDPtr(p.OrgID)
		pg.UpdatedAt = now
	}
	inSubtree := func(t *model.Task) bool {
		return (t.SourcePageID != nil && subtree[*t.SourcePageID]) || (t.FolderID != nil && subtree[*t.FolderID])
	}
	for _, t := range s.r.tasks {
		if inSubtree(t) && !sameUUIDPtr(t.OrgID, p.OrgID) {
			t.OrgID = copyUUIDPtr(p.OrgID)
			t.UpdatedAt = now
		}
	}
	for _, d := range s.r.deletedTasks {
		if inSubtree(d.task) {
			d.task.OrgID = copyUUIDPtr(p.OrgID)
		}
	}
	return s.updateFieldsLocked(p, fields)
}

func sameUUIDPtr(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func copyUUIDPtr(id *uuid.UUID) *uuid.UUID {
	if id == nil {
		return nil
	}
	cp := *id
	return &cp
}
