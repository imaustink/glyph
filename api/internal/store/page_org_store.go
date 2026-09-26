package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// SetSubtreeOrg moves pageID, all of its descendants and every task sourced
// from any of them into orgID (nil = the personal workspace), in a single
// statement. Changing only the root's org left the children and the notes'
// tasks in the old workspace: visible to (and editable by) the old org, and
// invisible to the new one.
func (s *pgPageStore) SetSubtreeOrg(ctx context.Context, pageID uuid.UUID, orgID *uuid.UUID) error {
	// UNION (not UNION ALL) so a parent cycle can't make the walk endless.
	const q = `
		WITH RECURSIVE subtree AS (
			SELECT id FROM pages WHERE id = $1
			UNION
			SELECT p.id FROM pages p JOIN subtree st ON p.parent_id = st.id
		), moved_pages AS (
			UPDATE pages SET org_id = $2, updated_at = NOW()
			WHERE id IN (SELECT id FROM subtree)
			RETURNING id
		)
		UPDATE tasks SET org_id = $2, updated_at = NOW()
		WHERE source_page_id IN (SELECT id FROM moved_pages)
		  AND org_id IS DISTINCT FROM $2`
	if _, err := s.pool.Exec(ctx, q, pageID, orgID); err != nil {
		return fmt.Errorf("set subtree org: %w", err)
	}
	return nil
}
