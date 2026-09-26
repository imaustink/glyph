package memstore

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// SetSubtreeOrg mirrors the Postgres implementation: the page, its
// descendants and the tasks sourced from them all move to orgID.
func (s *pageStore) SetSubtreeOrg(_ context.Context, pageID uuid.UUID, orgID *uuid.UUID) error {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	now := time.Now()
	subtree := map[uuid.UUID]bool{}
	queue := []uuid.UUID{pageID}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if subtree[id] {
			continue
		}
		p, ok := s.r.pages[id]
		if !ok {
			continue
		}
		subtree[id] = true
		p.OrgID = copyUUIDPtr(orgID)
		p.UpdatedAt = now
		for _, child := range s.r.pages {
			if child.ParentID != nil && *child.ParentID == id {
				queue = append(queue, child.ID)
			}
		}
	}
	for _, t := range s.r.tasks {
		if t.SourcePageID != nil && subtree[*t.SourcePageID] {
			t.OrgID = copyUUIDPtr(orgID)
			t.UpdatedAt = now
		}
	}
	for _, d := range s.r.deletedTasks {
		if d.task.SourcePageID != nil && subtree[*d.task.SourcePageID] {
			d.task.OrgID = copyUUIDPtr(orgID)
		}
	}
	return nil
}

func copyUUIDPtr(id *uuid.UUID) *uuid.UUID {
	if id == nil {
		return nil
	}
	cp := *id
	return &cp
}
