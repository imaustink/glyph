package memstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
	"github.com/google/uuid"
)

const (
	deletedReasonUser          = "user"
	deletedReasonSourceRemoved = "source_removed"
)

type deletedTask struct {
	task   *model.Task
	reason string
}

// collabDoc mirrors a page_collab_docs row.
type collabDoc struct {
	epoch       int
	attached    bool
	snapshotSeq int64
	quarantined bool
}

// canReadTask mirrors the Postgres taskAccessFilter: the task's own
// owner/org/share tiers, or read access to the note it comes from. Must be
// called with the lock held.
func (r *Registry) canReadTask(userID uuid.UUID, t *model.Task) bool {
	if r.canRead(userID, t.UserID, t.OrgID, t.IsPrivate, model.ShareResourceTask, t.ID) {
		return true
	}
	if t.SourcePageID == nil {
		return false
	}
	p, ok := r.pages[*t.SourcePageID]
	return ok && r.canRead(userID, p.UserID, p.OrgID, p.IsPrivate, model.ShareResourcePage, p.ID)
}

// taskIDTaken reports whether id is used by a live or soft-deleted task.
// Must be called with the lock held.
func (r *Registry) taskIDTaken(id uuid.UUID) bool {
	_, live := r.tasks[id]
	_, deleted := r.deletedTasks[id]
	return live || deleted
}

// liveOrDeletedBySource returns the id of the task (live or soft-deleted)
// linked to the given bullet, or uuid.Nil. Must be called with the lock held.
func (r *Registry) liveOrDeletedBySource(pageID uuid.UUID, nodeID string) uuid.UUID {
	match := func(t *model.Task) bool {
		return t.SourcePageID != nil && *t.SourcePageID == pageID &&
			t.SourceNodeID != nil && *t.SourceNodeID == nodeID
	}
	for id, t := range r.tasks {
		if match(t) {
			return id
		}
	}
	for id, d := range r.deletedTasks {
		if match(d.task) {
			return id
		}
	}
	return uuid.Nil
}

// sourceTaken mirrors the tasks_source_page_node_uniq index: is t's bullet
// already linked to a task other than exceptID? Must be called with the lock
// held.
func (r *Registry) sourceTaken(t *model.Task, exceptID uuid.UUID) bool {
	if t.SourcePageID == nil || t.SourceNodeID == nil {
		return false
	}
	id := r.liveOrDeletedBySource(*t.SourcePageID, *t.SourceNodeID)
	return id != uuid.Nil && id != exceptID
}

func (s *pageStore) WriteCollabSnapshot(_ context.Context, snap *model.CollabSnapshot) (*model.PageContent, error) {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	if _, ok := s.r.pages[snap.PageID]; !ok {
		return nil, store.ErrNotFound
	}
	d := s.r.collab[snap.PageID]
	switch {
	case d == nil:
		return nil, fmt.Errorf("%w: page has no collaborative session", store.ErrStaleSnapshot)
	case !d.attached:
		return nil, fmt.Errorf("%w: page is detached", store.ErrStaleSnapshot)
	case d.epoch != snap.Epoch:
		return nil, fmt.Errorf("%w: epoch %d is not current (%d)", store.ErrStaleSnapshot, snap.Epoch, d.epoch)
	case snap.UpToSeq < d.snapshotSeq:
		return nil, fmt.Errorf("%w: seq %d is behind %d", store.ErrStaleSnapshot, snap.UpToSeq, d.snapshotSeq)
	case d.quarantined:
		return nil, fmt.Errorf("%w: page is quarantined", store.ErrStaleSnapshot)
	}
	var out *model.PageContent
	if cur, ok := s.r.contents[snap.PageID]; ok && jsonEqual(cur.Content, snap.Content) {
		cp := *cur
		out = &cp
	} else {
		out = s.r.writeContent(snap.PageID, snap.Content, snap.SchemaVersion)
	}
	d.snapshotSeq = snap.UpToSeq
	return out, nil
}

func (s *pageStore) RestoreContentVersion(_ context.Context, pageID uuid.UUID, versionID int64, userID uuid.UUID) (*model.PageContent, error) {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	p, ok := s.r.pages[pageID]
	if !ok || !s.r.canWrite(userID, p.UserID, p.OrgID, p.IsPrivate, model.ShareResourcePage, p.ID) {
		return nil, store.ErrForbidden
	}
	var version *model.PageContentVersion
	for i := range s.r.contentVersions[pageID] {
		if s.r.contentVersions[pageID][i].ID == versionID {
			v := s.r.contentVersions[pageID][i]
			version = &v
		}
	}
	if version == nil {
		return nil, store.ErrNotFound
	}
	if d := s.r.collab[pageID]; d != nil {
		d.attached = false
	}
	return s.r.writeContent(pageID, version.Content, version.SchemaVersion), nil
}

// AttachCollab stands in for the collab service seeding a page: it marks the
// page attached under a new epoch and returns that epoch. Test-only.
func (s *pageStore) AttachCollab(pageID uuid.UUID) int {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	d := s.r.collab[pageID]
	if d == nil {
		d = &collabDoc{}
		s.r.collab[pageID] = d
	}
	d.epoch++
	d.attached = true
	d.snapshotSeq = 0
	return d.epoch
}

func jsonEqual(a, b json.RawMessage) bool {
	var va, vb interface{}
	if json.Unmarshal(a, &va) != nil || json.Unmarshal(b, &vb) != nil {
		return bytes.Equal(a, b)
	}
	ca, _ := json.Marshal(va)
	cb, _ := json.Marshal(vb)
	return bytes.Equal(ca, cb)
}
