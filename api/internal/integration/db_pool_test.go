package integration

import (
	"context"
	"testing"

	"github.com/glyph/api/internal/db"
	"github.com/stretchr/testify/require"
)

// DI-06: a runaway query (e.g. a recursive CTE over a parent_id cycle) must
// be cancelled by the server instead of running until memory or temp space
// runs out. The API pool therefore sets a statement_timeout on every
// connection unless the DSN already chooses one.
func TestDBConnectSetsStatementTimeout(t *testing.T) {
	pool := startMigrationDB(t)
	dsn := pool.Config().ConnString()

	t.Run("Default", func(t *testing.T) {
		t.Setenv("DATABASE_URL", dsn)
		p, err := db.Connect(context.Background())
		require.NoError(t, err)
		defer p.Close()
		var timeout string
		require.NoError(t, p.QueryRow(context.Background(), `SHOW statement_timeout`).Scan(&timeout))
		require.NotEqual(t, "0", timeout, "the API pool must not allow unbounded statements")
	})

	t.Run("DSNOverride", func(t *testing.T) {
		t.Setenv("DATABASE_URL", dsn+"&statement_timeout=7000")
		p, err := db.Connect(context.Background())
		require.NoError(t, err)
		defer p.Close()
		var timeout string
		require.NoError(t, p.QueryRow(context.Background(), `SHOW statement_timeout`).Scan(&timeout))
		require.Equal(t, "7s", timeout, "a statement_timeout in the DSN wins")
	})
}
