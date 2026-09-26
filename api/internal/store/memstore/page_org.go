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
// moves the page, its descendants and the tasks sourced from them to
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
	for _, t := range s.r.tasks {
		if t.SourcePageID != nil && subtree[*t.SourcePageID] {
			t.OrgID = copyUUIDPtr(p.OrgID)
			t.UpdatedAt = now
		}
	}
	for _, d := range s.r.deletedTasks {
		if d.task.SourcePageID != nil && subtree[*d.task.SourcePageID] {
			d.task.OrgID = copyUUIDPtr(p.OrgID)
		}
	}
	return s.updateFieldsLocked(p, fields)
}

func copyUUIDPtr(id *uuid.UUID) *uuid.UUID {
	if id == nil {
		return nil
	}
	cp := *id
	return &cp
}
