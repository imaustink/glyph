package integration

import (
	"testing"

	"github.com/glyph/api/internal/store"
	"github.com/glyph/api/internal/store/memstore"
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
