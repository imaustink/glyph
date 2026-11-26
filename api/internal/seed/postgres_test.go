package seed

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// migratedDB returns a pool on a fresh database with every migration
// applied, plus a schema_migrations row as golang-migrate leaves it.
//
// It uses a Postgres container, like internal/integration. Set
// GLYPH_TEST_DATABASE_URL to an existing server instead (a throwaway
// database is created on it and dropped afterwards).
func migratedDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	var dsn string
	if admin := os.Getenv("GLYPH_TEST_DATABASE_URL"); admin != "" {
		name := "seed_test_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
		adminPool, err := pgxpool.New(ctx, admin)
		require.NoError(t, err)
		_, err = adminPool.Exec(ctx, "CREATE DATABASE "+name)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = adminPool.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
			adminPool.Close()
		})
		u, err := url.Parse(admin)
		require.NoError(t, err)
		u.Path = "/" + name
		dsn = u.String()
	} else {
		container, err := tcpostgres.Run(ctx,
			"postgres:16-alpine",
			tcpostgres.WithDatabase("seed_test"),
			tcpostgres.WithUsername("test"),
			tcpostgres.WithPassword("test"),
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").
					WithOccurrence(2).
					WithStartupTimeout(30*time.Second),
			),
		)
		require.NoError(t, err)
		t.Cleanup(func() { _ = container.Terminate(context.Background()) })
		dsn, err = container.ConnectionString(ctx, "sslmode=disable")
		require.NoError(t, err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	files, err := filepath.Glob("../../migrations/*.up.sql")
	require.NoError(t, err)
	sort.Strings(files)
	require.NotEmpty(t, files)
	for _, path := range files {
		sql, err := os.ReadFile(path)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, string(sql))
		require.NoError(t, err, filepath.Base(path))
	}
	var version int64
	_, err = fmt.Sscanf(filepath.Base(files[len(files)-1]), "%d_", &version)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `CREATE TABLE schema_migrations (version bigint NOT NULL PRIMARY KEY, dirty boolean NOT NULL);
		INSERT INTO schema_migrations VALUES (`+fmt.Sprint(version)+`, false)`)
	require.NoError(t, err)
	return pool
}

func countRows(t *testing.T, pool *pgxpool.Pool) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, table := range []string{"users", "organizations", "org_members", "pages", "page_contents", "tasks", "lanes", "templates"} {
		var n int
		require.NoError(t, pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n))
		out[table] = n
	}
	return out
}

func TestRun_Postgres(t *testing.T) {
	ctx := context.Background()
	pool := migratedDB(t)
	s := PGStores(pool)

	// Someone who signed in before the seed ran.
	early, err := s.Users.Upsert(ctx, "early-bird", "https://accounts.google.com", nil, nil)
	require.NoError(t, err)

	require.NoError(t, WaitForSchema(ctx, pool, 10*time.Millisecond))

	res, err := Run(ctx, pool, seedTime)
	require.NoError(t, err)
	require.True(t, res.Created)
	folders, pages, tasks, lanes := expectedCounts()
	after := countRows(t, pool)
	assert.Equal(t, folders+pages, after["pages"])
	assert.Equal(t, pages, after["page_contents"])
	assert.Equal(t, tasks, after["tasks"])
	assert.Equal(t, lanes, after["lanes"])
	assert.Equal(t, len(demoTemplates()), after["templates"])
	assert.Equal(t, 1, after["organizations"])

	t.Run("ExistingUserWasAdded", func(t *testing.T) {
		m, err := s.Orgs.GetMember(ctx, res.OrgID, early.ID)
		require.NoError(t, err)
		assert.Equal(t, MemberRole, m.Role)
	})

	t.Run("SecondRunIsNoop", func(t *testing.T) {
		again, err := Run(ctx, pool, seedTime.Add(72*time.Hour))
		require.NoError(t, err)
		assert.False(t, again.Created)
		assert.Equal(t, res.OrgID, again.OrgID)
		assert.Equal(t, after, countRows(t, pool))
	})

	t.Run("NewUserJoinsOnFirstLogin", func(t *testing.T) {
		// Exactly what callbackHandler does on a first OIDC login.
		email, name := "preview@example.com", "Preview User"
		user, err := s.Users.Upsert(ctx, "google-sub-1", "https://accounts.google.com", &email, &name)
		require.NoError(t, err)
		m, err := s.Orgs.GetMember(ctx, res.OrgID, user.ID)
		require.NoError(t, err)
		assert.Equal(t, MemberRole, m.Role)

		// A repeat login (the upsert's UPDATE path) is harmless.
		_, err = s.Users.Upsert(ctx, "google-sub-1", "https://accounts.google.com", &email, &name)
		require.NoError(t, err)

		assertMemberCanUseDemo(t, s, res, user)
	})

	t.Run("BrokenTriggerNeverBlocksLogin", func(t *testing.T) {
		// Make the trigger's insert fail (as a future schema change might):
		// the user must still be created.
		_, err := pool.Exec(ctx, `ALTER TABLE org_members ADD CONSTRAINT seed_test_block CHECK (role <> 'editor') NOT VALID`)
		require.NoError(t, err)
		defer func() {
			_, _ = pool.Exec(ctx, `ALTER TABLE org_members DROP CONSTRAINT seed_test_block`)
		}()
		_, err = s.Users.Upsert(ctx, "google-sub-2", "https://accounts.google.com", nil, nil)
		require.NoError(t, err)
	})
}

func TestRun_Postgres_RebuildsIncompleteWorkspace(t *testing.T) {
	ctx := context.Background()
	pool := migratedDB(t)
	s := PGStores(pool)

	// A run that died after creating the org and a note.
	u, err := SeedUser(ctx, s.Users)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		WITH o AS (INSERT INTO organizations (name, created_by) VALUES ($1, $2) RETURNING id),
		     m AS (INSERT INTO org_members (org_id, user_id, role) SELECT id, $2, 'owner' FROM o)
		INSERT INTO pages (user_id, type, title, org_id) SELECT $2, 'page', 'half-built', id FROM o`,
		pendingOrgName, u.ID)
	require.NoError(t, err)

	res, err := Run(ctx, pool, seedTime)
	require.NoError(t, err)
	assert.True(t, res.Created)

	counts := countRows(t, pool)
	assert.Equal(t, 1, counts["organizations"])
	folders, pages, _, _ := expectedCounts()
	assert.Equal(t, folders+pages, counts["pages"])
	var name string
	require.NoError(t, pool.QueryRow(ctx, `SELECT name FROM organizations`).Scan(&name))
	assert.Equal(t, OrgName, name)
}
