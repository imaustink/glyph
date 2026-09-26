package integration

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Migration tests run the real migration files against a throwaway Postgres,
// seeding legacy data between steps. They only make sense for Postgres, so
// they bypass the backend matrix.

// startMigrationDB starts an empty Postgres and returns a pool to it.
func startMigrationDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	container, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("migrations_test"),
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

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

// upMigrations returns the up-migration files in order.
func upMigrations(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("../../migrations/*.up.sql")
	require.NoError(t, err)
	sort.Strings(files)
	return files
}

// applyMigrations runs every up migration whose version is in [from, to].
func applyMigrations(t *testing.T, pool *pgxpool.Pool, from, to string) {
	t.Helper()
	for _, path := range upMigrations(t) {
		version := strings.SplitN(filepath.Base(path), "_", 2)[0]
		if version < from || version > to {
			continue
		}
		sql, err := os.ReadFile(path)
		require.NoError(t, err)
		_, err = pool.Exec(context.Background(), string(sql))
		require.NoError(t, err, "migration %s", filepath.Base(path))
	}
}

func TestMigrations(t *testing.T) {
	t.Parallel()

	// DI-19: a page_contents row written before 000013 with the old TEXT
	// default ('') must survive the TEXT→JSONB conversion (000013 kept NOT
	// NULL but mapped '' to NULL) and the history seeding in 000017
	// (page_content_versions.content is NOT NULL).
	t.Run("LegacyEmptyContentMigratesToEmptyDoc", func(t *testing.T) {
		pool := startMigrationDB(t)
		ctx := context.Background()
		applyMigrations(t, pool, "000001", "000012")
		_, err := pool.Exec(ctx, `
			INSERT INTO users (id, sub, issuer) VALUES ('00000000-0000-0000-0000-000000000001', 's', 'i');
			INSERT INTO pages (id, user_id, type) VALUES ('00000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000001', 'page');
			INSERT INTO page_contents (page_id) VALUES ('00000000-0000-0000-0000-000000000002');`)
		require.NoError(t, err)

		applyMigrations(t, pool, "000013", "999999")

		var content, versioned string
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT content::text FROM page_contents WHERE page_id = '00000000-0000-0000-0000-000000000002'`,
		).Scan(&content))
		require.JSONEq(t, `{"type":"doc","content":[]}`, content)
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT content::text FROM page_content_versions WHERE page_id = '00000000-0000-0000-0000-000000000002'`,
		).Scan(&versioned))
		require.JSONEq(t, `{"type":"doc","content":[]}`, versioned)
	})
}
