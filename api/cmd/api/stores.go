package main

import (
	"github.com/glyph/api/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// stores holds all instantiated data stores.
type stores struct {
	users     store.UserStore
	pages     store.PageStore
	tasks     store.TaskStore
	lanes     store.LaneStore
	templates store.TemplateStore
	orgs      store.OrgStore
	shares    store.ShareStore
}

// newStores creates all store instances from the database pool.
func newStores(pool *pgxpool.Pool) *stores {
	return &stores{
		users:     store.NewUserStore(pool),
		pages:     store.NewPageStore(pool),
		tasks:     store.NewTaskStore(pool),
		lanes:     store.NewLaneStore(pool),
		templates: store.NewTemplateStore(pool),
		orgs:      store.NewOrgStore(pool),
		shares:    store.NewShareStore(pool),
	}
}
