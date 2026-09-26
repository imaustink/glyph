package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/glyph/api/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// UpdateFieldsMovingOrg is UpdateFields for a write that changes the page's
// org to p.OrgID (nil = the personal workspace). A node's workspace is its
// subtree's, so the page's descendants move too, with every task sourced
// from any of them or placed on one of their folder boards (folder_id), as
// Delete matches them. Changing only the root's org left the children and
// their tasks in the old workspace: visible to (and editable by) the old
// org, and invisible to the new one. Lanes need nothing: they carry no org
// (migration 000008 dropped lanes.org_id) and follow their folder.
//
// Like Delete, it refuses (ErrSubtreeNotOwned) a subtree holding pages owned
// by anyone other than the root's owner: editor shares let other users
// create pages inside someone else's folder, and moving the folder must not
// carry their pages into a workspace they never chose. The cascade and the
// root's own field write are one transaction, under the tree-move lock, so a
// write rejected by the cycle check (ErrCycle) changes no org at all.
func (s *pgPageStore) UpdateFieldsMovingOrg(ctx context.Context, p *model.Page, fields []string) (*model.Page, error) {
	q, args, parent, err := pageUpdateFieldsQuery(p, fields)
	if err != nil {
		return nil, err
	}
	if parent != nil && *parent == p.ID {
		return nil, ErrCycle
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("page org move — begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// The tree-move lock keeps pages from moving into or out of the subtree
	// while it is checked and rewritten, and makes the cycle check below
	// see every committed move.
	if _, err := tx.Exec(ctx, pageTreeMoveLockSQL); err != nil {
		return nil, fmt.Errorf("page org move — tree lock: %w", err)
	}
	if parent != nil {
		var cycle bool
		if err := tx.QueryRow(ctx, wouldCycleSQL, p.ID, *parent).Scan(&cycle); err != nil {
			return nil, fmt.Errorf("page org move — cycle check: %w", err)
		}
		if cycle {
			return nil, ErrCycle
		}
	}
	var rootID uuid.UUID
	if err := tx.QueryRow(ctx,
		`SELECT id FROM pages WHERE id = $1 AND user_id = $2 FOR UPDATE`, p.ID, p.UserID,
	).Scan(&rootID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("page org move — lookup: %w", err)
	}
	// Lock the subtree so no page can be created in it (that needs a
	// key-share lock on the parent row) while it is checked and moved.
	if _, err := tx.Exec(ctx,
		pageSubtreeCTE+` SELECT 1 FROM pages p JOIN subtree s ON s.id = p.id FOR UPDATE OF p`, p.ID,
	); err != nil {
		return nil, fmt.Errorf("page org move — lock subtree: %w", err)
	}
	var foreign int
	if err := tx.QueryRow(ctx,
		pageSubtreeCTE+` SELECT COUNT(*) FROM pages p JOIN subtree s ON s.id = p.id WHERE p.user_id <> $2`,
		p.ID, p.UserID,
	).Scan(&foreign); err != nil {
		return nil, fmt.Errorf("page org move — read subtree: %w", err)
	}
	if foreign > 0 {
		return nil, ErrSubtreeNotOwned
	}
	if _, err := tx.Exec(ctx, pageSubtreeCTE+`, moved_pages AS (
			UPDATE pages SET org_id = $2, updated_at = NOW()
			WHERE id IN (SELECT id FROM subtree)
			RETURNING id
		)
		UPDATE tasks SET org_id = $2, updated_at = NOW()
		WHERE (source_page_id IN (SELECT id FROM moved_pages) OR folder_id IN (SELECT id FROM moved_pages))
		  AND org_id IS DISTINCT FROM $2`, p.ID, p.OrgID,
	); err != nil {
		return nil, fmt.Errorf("page org move — cascade: %w", err)
	}
	out, err := scanPage(tx.QueryRow(ctx, q, args...))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("page org move — commit: %w", err)
	}
	return out, nil
}
