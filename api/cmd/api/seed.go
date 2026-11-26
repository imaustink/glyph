package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/glyph/api/internal/db"
	"github.com/glyph/api/internal/seed"
	"github.com/jackc/pgx/v5/pgxpool"
)

// seedDemoCommand is the subcommand that seeds the preview demo workspace:
//
//	GLYPH_SEED_DEMO=true DATABASE_URL=... /api seed-demo
const seedDemoCommand = "seed-demo"

// seedDemoEnv must be "true" for seed-demo to run. The subcommand alone is
// not enough: the seed makes every user of the database a member of the
// demo organization (now and on every future sign-in), which must never
// happen to a real deployment by a mistyped command.
const seedDemoEnv = "GLYPH_SEED_DEMO"

// runSeedDemo runs the seed-demo subcommand and returns the process exit code.
func runSeedDemo() int {
	if os.Getenv(seedDemoEnv) != "true" {
		fmt.Fprintf(os.Stderr, "refusing to seed demo data: set %s=true to confirm this database is a disposable preview\n", seedDemoEnv)
		return 2
	}

	if os.Getenv("DATABASE_URL") == "" {
		fmt.Fprintln(os.Stderr, "seed: DATABASE_URL is not set")
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	pool, err := connectWithRetry(ctx)
	if err != nil {
		slog.Error("seed: database", "err", err)
		return 1
	}
	defer pool.Close()

	if err := seed.WaitForSchema(ctx, pool, 3*time.Second); err != nil {
		slog.Error("seed: schema", "err", err)
		return 1
	}

	res, err := seed.Run(ctx, pool, time.Now())
	if err != nil {
		slog.Error("seed: failed", "err", err)
		return 1
	}
	slog.Info("seed: " + res.String())
	return 0
}

// connectWithRetry opens the pool, retrying while the database comes up.
func connectWithRetry(ctx context.Context) (*pgxpool.Pool, error) {
	for {
		pool, err := db.Connect(ctx)
		if err == nil {
			return pool, nil
		}
		slog.Info("seed: waiting for database", "reason", err)
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("%w (last error: %v)", ctx.Err(), err)
		case <-time.After(3 * time.Second):
		}
	}
}
