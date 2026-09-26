package db

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DefaultStatementTimeout is applied to every pooled connection unless the
// DSN sets statement_timeout itself. Generous: the slowest legitimate API
// statements (large content writes with history pruning) take well under a
// second.
const DefaultStatementTimeout = "30s"

// Connect opens a pgxpool using the DATABASE_URL environment variable.
func Connect(ctx context.Context) (*pgxpool.Pool, error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return nil, fmt.Errorf("DATABASE_URL environment variable is not set")
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.ParseConfig: %w", err)
	}
	// Bound every statement so a runaway query (e.g. a recursive CTE over a
	// corrupt page tree) is cancelled by the server instead of running until
	// memory or temp space is exhausted. A statement_timeout in the DSN wins.
	if _, ok := cfg.ConnConfig.RuntimeParams["statement_timeout"]; !ok {
		cfg.ConnConfig.RuntimeParams["statement_timeout"] = DefaultStatementTimeout
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.New: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("db ping: %w", err)
	}

	return pool, nil
}

// WithTx executes fn within a database transaction. If fn returns an error,
// the transaction is rolled back; otherwise it is committed.
// This helper simplifies atomic multi-step operations.
func WithTx(ctx context.Context, pool *pgxpool.Pool, fn func(tx pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		// Rollback is a no-op if tx was already committed
		_ = tx.Rollback(ctx)
	}()

	if err := fn(tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}
