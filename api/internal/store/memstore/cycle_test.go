package memstore

import (
	"context"
	"testing"
	"time"

	"github.com/glyph/api/internal/model"
	"github.com/google/uuid"
)

// DI-06: a parent_id cycle already in the data must not hang tree walks.
// These used to loop forever while holding the registry lock.

func seedCycle(r *Registry) (a, b uuid.UUID) {
	a, b = uuid.New(), uuid.New()
	now := time.Now()
	owner := uuid.New()
	r.pages[a] = &model.Page{ID: a, UserID: owner, Type: model.NodeTypeFolder, ParentID: &b, CreatedAt: now, UpdatedAt: now}
	r.pages[b] = &model.Page{ID: b, UserID: owner, Type: model.NodeTypeFolder, ParentID: &a, CreatedAt: now, UpdatedAt: now}
	return a, b
}

// withinDeadline fails the test if fn doesn't return within two seconds.
func withinDeadline(t *testing.T, name string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { fn(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("%s did not terminate on a parent_id cycle", name)
	}
}

func TestIsAncestor_TerminatesOnCycle(t *testing.T) {
	r := NewRegistry()
	a, _ := seedCycle(r)
	s := &pageStore{r: r}
	withinDeadline(t, "IsAncestor", func() {
		ok, err := s.IsAncestor(context.Background(), uuid.New(), a)
		if err != nil || ok {
			t.Errorf("IsAncestor(unrelated, a) = %v, %v; want false, nil", ok, err)
		}
	})
}

func TestGetDescendantIDs_TerminatesOnCycle(t *testing.T) {
	r := NewRegistry()
	a, b := seedCycle(r)
	s := &pageStore{r: r}
	withinDeadline(t, "GetDescendantIDs", func() {
		ids, err := s.GetDescendantIDs(context.Background(), a)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if len(ids) != 2 || !containsID(ids, a) || !containsID(ids, b) {
			t.Errorf("GetDescendantIDs(a) = %v, want exactly [a b]", ids)
		}
	})
}

func containsID(ids []uuid.UUID, id uuid.UUID) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}
