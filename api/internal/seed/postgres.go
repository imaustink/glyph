package seed

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/glyph/api/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// advisoryLockKey serialises seed runs (two Jobs started by overlapping
// deploys) so they cannot both build the workspace.
const advisoryLockKey int64 = 0x676c7970685f6465 // "glyph_de"

// autoJoinSQL installs the trigger that makes every newly created user an
// editor of the demo organization — preview users are created on their
// first sign-in, so they see the demo data straight away. It exists only in
// databases this seed has run against. The org is looked up by its creator
// (the seed user) and name rather than by id, so the trigger stays correct if
// the workspace is ever rebuilt. Any error is downgraded to a warning: a
// broken demo trigger must never stop a user from signing in.
//
// CREATE OR REPLACE + DROP/CREATE TRIGGER make this idempotent, and re-running
// it on every deploy keeps the function current.
const autoJoinSQL = `
CREATE OR REPLACE FUNCTION glyph_demo_auto_join() RETURNS trigger
LANGUAGE plpgsql AS $fn$
BEGIN
    BEGIN
        INSERT INTO org_members (org_id, user_id, role)
        SELECT o.id, NEW.id, '` + string(MemberRole) + `'
        FROM organizations o
        JOIN users s ON s.id = o.created_by
        WHERE s.sub = '` + UserSub + `' AND s.issuer = '` + UserIssuer + `'
          AND o.name = '` + OrgName + `'
          AND s.id <> NEW.id
        ON CONFLICT (org_id, user_id) DO NOTHING;
    EXCEPTION WHEN OTHERS THEN
        RAISE WARNING 'glyph_demo_auto_join: % (%)', SQLERRM, SQLSTATE;
    END;
    RETURN NULL;
END;
$fn$;

DROP TRIGGER IF EXISTS glyph_demo_auto_join ON users;
CREATE TRIGGER glyph_demo_auto_join
    AFTER INSERT ON users
    FOR EACH ROW EXECUTE FUNCTION glyph_demo_auto_join();
`

// backfillSQL makes every existing user (except the seed user) a member of
// the demo org: users who signed in before the seed ran, or before the
// trigger existed. Existing memberships (and their roles) are left alone.
const backfillSQL = `
INSERT INTO org_members (org_id, user_id, role)
SELECT $1, u.id, '` + string(MemberRole) + `' FROM users u WHERE u.id <> $2
ON CONFLICT (org_id, user_id) DO NOTHING`

// PGStores builds the Postgres stores the seed writes through.
func PGStores(pool *pgxpool.Pool) Stores {
	return Stores{
		Users:     store.NewUserStore(pool),
		Pages:     store.NewPageStore(pool),
		Tasks:     store.NewTaskStore(pool),
		Lanes:     store.NewLaneStore(pool),
		Templates: store.NewTemplateStore(pool),
		Orgs:      store.NewOrgStore(pool),
		Shares:    store.NewShareStore(pool),
	}
}

// Run seeds the demo workspace into a migrated Postgres database and makes
// sure every user — existing and future — is a member of it. It is safe to
// run any number of times: once the workspace exists, a run only refreshes
// the trigger and adds users who are not members yet.
//
// A workspace left half-built by a run that died is deleted (by deleting
// the seed user, whose rows cascade) and built again.
func Run(ctx context.Context, pool *pgxpool.Pool, now time.Time) (*Result, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, advisoryLockKey); err != nil {
		return nil, fmt.Errorf("take seed lock: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, advisoryLockKey)
	}()

	stores := PGStores(pool)
	res, err := Seed(ctx, stores, now)
	if errors.Is(err, ErrIncomplete) {
		slog.Warn("seed: found a half-built demo workspace from an earlier run; deleting it and starting over")
		if _, err := pool.Exec(ctx, `DELETE FROM users WHERE sub = $1 AND issuer = $2`, UserSub, UserIssuer); err != nil {
			return nil, fmt.Errorf("delete incomplete demo workspace: %w", err)
		}
		res, err = Seed(ctx, stores, now)
	}
	if err != nil {
		return nil, err
	}

	if _, err := pool.Exec(ctx, autoJoinSQL); err != nil {
		return nil, fmt.Errorf("install auto-join trigger: %w", err)
	}
	tag, err := pool.Exec(ctx, backfillSQL, res.OrgID, res.UserID)
	if err != nil {
		return nil, fmt.Errorf("add existing users to demo org: %w", err)
	}
	if n := tag.RowsAffected(); n > 0 {
		slog.Info("seed: added existing users to the demo org", "count", n)
	}
	return res, nil
}

// WaitForSchema blocks until the database is reachable and migrated
// (golang-migrate's schema_migrations table exists and is not dirty), or
// until ctx is done. The seed Job may start while the migration Job is still
// running.
func WaitForSchema(ctx context.Context, pool *pgxpool.Pool, every time.Duration) error {
	for {
		var version int64
		var dirty bool
		err := pool.QueryRow(ctx, `SELECT version, dirty FROM schema_migrations LIMIT 1`).Scan(&version, &dirty)
		switch {
		case err == nil && !dirty:
			slog.Info("seed: database schema ready", "version", version)
			return nil
		case err == nil:
			err = fmt.Errorf("schema_migrations is dirty at version %d", version)
		}
		slog.Info("seed: waiting for migrations", "reason", err)
		select {
		case <-ctx.Done():
			return fmt.Errorf("database not migrated: %w (last error: %v)", ctx.Err(), err)
		case <-time.After(every):
		}
	}
}
