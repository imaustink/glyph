package integration

import (
	"testing"
	"time"

	"github.com/glyph/api/internal/store"
	"github.com/glyph/api/internal/store/memstore"
	"github.com/google/uuid"
)

func init() {
	registerBackend(&memBackend{})
}

// Resettable is implemented by memstore types to clear state between tests.
type Resettable interface {
	Reset()
}

type memBackend struct {
	users     store.UserStore
	pages     store.PageStore
	tasks     store.TaskStore
	lanes     store.LaneStore
	templates store.TemplateStore
	orgs      store.OrgStore
	shares    store.ShareStore
}

func (b *memBackend) Name() string { return "memstore" }

func (b *memBackend) Setup(_ *testing.T) (
	store.UserStore, store.PageStore, store.TaskStore, store.LaneStore, store.TemplateStore,
	store.OrgStore, store.ShareStore,
) {
	b.users, b.pages, b.tasks, b.lanes, b.templates, b.orgs, b.shares = memstore.NewStores()
	return b.users, b.pages, b.tasks, b.lanes, b.templates, b.orgs, b.shares
}

func (b *memBackend) Reset(_ *testing.T) {
	for _, s := range []interface{}{b.users, b.pages, b.tasks, b.lanes, b.templates, b.orgs, b.shares} {
		if r, ok := s.(Resettable); ok {
			r.Reset()
		}
	}
}

func (b *memBackend) Teardown() {} // nothing to clean up

// TitleRenamedAt asks the memstore when the task was last renamed outside its note.
func (b *memBackend) TitleRenamedAt(t *testing.T, taskID uuid.UUID) (time.Time, bool) {
	t.Helper()
	r, ok := b.tasks.(interface {
		TitleRenamedAt(uuid.UUID) (time.Time, bool)
	})
	if !ok {
		t.Fatal("memstore does not record when a task was renamed outside its note")
	}
	return r.TitleRenamedAt(taskID)
}

// AttachCollab stands in for the collab service seeding a page.
func (b *memBackend) AttachCollab(_ *testing.T, pageID uuid.UUID) int {
	return b.pages.(interface{ AttachCollab(uuid.UUID) int }).AttachCollab(pageID)
}
