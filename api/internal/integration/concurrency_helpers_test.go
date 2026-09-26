package integration

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// Pool exposes the Postgres pool so race specs can hold row locks directly.
func (b *postgresBackend) Pool() *pgxpool.Pool { return b.pool }

// pgPool returns the backend's Postgres pool, skipping the spec on backends
// without one. Race specs only make sense against a real database: memstore
// serializes every call under one mutex.
func (h *Harness) pgPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	p, ok := h.Backend.(interface{ Pool() *pgxpool.Pool })
	if !ok {
		t.Skipf("backend %s has no database locks to race against", h.Backend.Name())
	}
	return p.Pool()
}

// interleave reproduces a write racing another write deterministically.
//
// hold runs inside a transaction that stands in for a concurrent request
// that has already taken its locks and written, but not yet committed. req
// then runs in the background; once it has either finished or is blocked
// waiting for one of hold's locks, the transaction commits and req's
// response is returned.
//
// A request that reads the row before taking a lock (read-modify-write) sees
// the pre-commit state and then writes it back over hold's change. A request
// that locks the row before reading waits for the commit and sees it.
func interleave[T any](t *testing.T, pool *pgxpool.Pool, hold func(ctx context.Context, tx pgx.Tx), req func() T) T {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	hold(ctx, tx)

	done := make(chan T, 1)
	go func() { done <- req() }()

	var (
		w        T
		finished bool
	)
	deadline := time.Now().Add(10 * time.Second)
wait:
	for {
		select {
		case w = <-done:
			finished = true
			break wait
		default:
		}
		var waiting int
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT count(*) FROM pg_stat_activity
			 WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&waiting))
		if waiting > 0 {
			break
		}
		require.True(t, time.Now().Before(deadline), "request neither finished nor blocked")
		time.Sleep(5 * time.Millisecond)
	}
	require.NoError(t, tx.Commit(ctx))
	if !finished {
		select {
		case w = <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("request did not finish after the competing transaction committed")
		}
	}
	return w
}
