package integration

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/glyph/api/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func init() {
	registerBackend(&postgresBackend{})
}

type postgresBackend struct {
	container *tcpostgres.PostgresContainer
	pool      *pgxpool.Pool
	users     store.UserStore
	pages     store.PageStore
	tasks     store.TaskStore
	lanes     store.LaneStore
	templates store.TemplateStore
	orgs      store.OrgStore
	shares    store.ShareStore
}

func (b *postgresBackend) Name() string { return "postgres" }

func (b *postgresBackend) Setup(t *testing.T) (
	store.UserStore, store.PageStore, store.TaskStore, store.LaneStore, store.TemplateStore,
	store.OrgStore, store.ShareStore,
) {
	t.Helper()
	ctx := context.Background()

	container, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("noteboard_test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	require.NoError(t, err)
	b.container = container

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)
	b.pool = pool

	migrations, err := filepath.Glob("../../migrations/*.up.sql")
	require.NoError(t, err)
	sort.Strings(migrations)
	for _, path := range migrations {
		sql, err := os.ReadFile(path)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, string(sql))
		require.NoError(t, err)
	}

	b.users = store.NewUserStore(pool)
	b.pages = store.NewPageStore(pool)
	b.tasks = store.NewTaskStore(pool)
	b.lanes = store.NewLaneStore(pool)
	b.templates = store.NewTemplateStore(pool)
	b.orgs = store.NewOrgStore(pool)
	b.shares = store.NewShareStore(pool)

	return b.users, b.pages, b.tasks, b.lanes, b.templates, b.orgs, b.shares
}

func (b *postgresBackend) Reset(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	_, err := b.pool.Exec(ctx, "TRUNCATE shares, org_members, organizations, page_contents, tasks, lanes, templates, pages, users CASCADE")
	require.NoError(t, err)
}

func (b *postgresBackend) Teardown() {
	if b.pool != nil {
		b.pool.Close()
	}
	if b.container != nil {
		if err := b.container.Terminate(context.Background()); err != nil {
			log.Printf("postgres teardown: %v", err)
		}
	}
}
