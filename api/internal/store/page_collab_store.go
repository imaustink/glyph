package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/glyph/api/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CollabNotifyChannel is the Postgres LISTEN/NOTIFY channel the collab
// service subscribes to. Notifications are delivered only on commit, so a
// listener never acts on a change that was rolled back.
const CollabNotifyChannel = "glyph_collab"

// collabNotification is the payload sent on CollabNotifyChannel.
//
// "reset": the page's shared document was replaced (a version restore, or a
// REST write while collaboration is disabled). Every connected client must
// drop its local Yjs state; the next session re-seeds from page_contents
// under a new epoch.
type collabNotification struct {
	Type   string    `json:"type"`
	PageID uuid.UUID `json:"pageId"`
}

// collabAttachedLocked reports whether the page is attached to a
// collaborative session, locking its page_collab_docs row (if any) for the
// rest of the transaction.
func collabAttachedLocked(ctx context.Context, tx pgx.Tx, pageID uuid.UUID) (bool, error) {
	var attached bool
	err := tx.QueryRow(ctx,
		`SELECT attached FROM page_collab_docs WHERE page_id = $1 FOR UPDATE`, pageID,
	).Scan(&attached)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return attached, err
}

// detachCollabLocked makes page_contents authoritative again and tells the
// collab service to evict every client holding the page's shared document.
// The epoch is deliberately left alone: the collab service bumps it when it
// next seeds, so no client can ever reconnect into the replaced document.
func detachCollabLocked(ctx context.Context, tx pgx.Tx, pageID uuid.UUID) error {
	if _, err := tx.Exec(ctx,
		`UPDATE page_collab_docs SET attached = false, updated_at = NOW() WHERE page_id = $1`,
		pageID,
	); err != nil {
		return err
	}
	payload, err := json.Marshal(collabNotification{Type: "reset", PageID: pageID})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `SELECT pg_notify($1, $2)`, CollabNotifyChannel, string(payload))
	return err
}

// WriteCollabSnapshot persists the collab service's view of a shared document
// to page_contents. It is refused (ErrStaleSnapshot) unless the page is still
// attached in the same epoch and the snapshot does not go backwards, so a
// lagging or partitioned collab replica can never overwrite a restore or a
// newer snapshot.
func (s *pgPageStore) WriteCollabSnapshot(ctx context.Context, snap *model.CollabSnapshot) (*model.PageContent, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("collab snapshot — begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var lockedPageID uuid.UUID
	if err := tx.QueryRow(ctx,
		`SELECT id FROM pages WHERE id = $1 FOR UPDATE`, snap.PageID,
	).Scan(&lockedPageID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("collab snapshot — page lookup: %w", err)
	}

	var (
		epoch       int
		attached    bool
		snapshotSeq int64
		quarantined bool
	)
	err = tx.QueryRow(ctx,
		`SELECT epoch, attached, snapshot_seq, quarantined_at IS NOT NULL
		 FROM page_collab_docs WHERE page_id = $1 FOR UPDATE`,
		snap.PageID,
	).Scan(&epoch, &attached, &snapshotSeq, &quarantined)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: page has no collaborative session", ErrStaleSnapshot)
	}
	if err != nil {
		return nil, fmt.Errorf("collab snapshot — state lookup: %w", err)
	}
	switch {
	case !attached:
		return nil, fmt.Errorf("%w: page is detached", ErrStaleSnapshot)
	case epoch != snap.Epoch:
		return nil, fmt.Errorf("%w: epoch %d is not current (%d)", ErrStaleSnapshot, snap.Epoch, epoch)
	case snap.UpToSeq < snapshotSeq:
		return nil, fmt.Errorf("%w: seq %d is behind %d", ErrStaleSnapshot, snap.UpToSeq, snapshotSeq)
	case quarantined:
		return nil, fmt.Errorf("%w: page is quarantined", ErrStaleSnapshot)
	}

	cur, err := currentContentLocked(ctx, tx, snap.PageID)
	if err != nil {
		return nil, fmt.Errorf("collab snapshot — current lookup: %w", err)
	}

	// Idle sessions re-snapshot the same document; don't burn a revision and a
	// history slot on every one.
	unchanged := false
	if cur != nil {
		// NULL-safe: legacy rows can have SQL NULL content (see GetContent's
		// COALESCE), and `content = $2` would then evaluate to NULL, which
		// fails to Scan into a bool. IS NOT DISTINCT FROM treats NULL current
		// content as "changed" so the snapshot is written rather than 500ing.
		if err := tx.QueryRow(ctx,
			`SELECT content IS NOT DISTINCT FROM $2::jsonb FROM page_contents WHERE page_id = $1`,
			snap.PageID, []byte(snap.Content),
		).Scan(&unchanged); err != nil {
			return nil, fmt.Errorf("collab snapshot — compare: %w", err)
		}
	}

	var out *model.PageContent
	if unchanged {
		out = &model.PageContent{
			PageID: snap.PageID, Content: cur.content, UpdatedAt: cur.updatedAt,
			SchemaVersion: cur.schemaVersion, Revision: cur.revision,
		}
	} else {
		out, err = writeContentLocked(ctx, tx, snap.PageID, snap.Content, snap.SchemaVersion, cur)
		if err != nil {
			return nil, err
		}
	}

	if _, err := tx.Exec(ctx,
		`UPDATE page_collab_docs SET snapshot_seq = $2, updated_at = NOW() WHERE page_id = $1`,
		snap.PageID, snap.UpToSeq,
	); err != nil {
		return nil, fmt.Errorf("collab snapshot — bookkeeping: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("collab snapshot — commit: %w", err)
	}
	return out, nil
}

// RestoreContentVersion makes a superseded revision current again. The
// content being replaced is archived first, so a restore is itself undoable.
// If the page is attached to a collaborative session it is detached: every
// connected client is evicted and the next session starts a new epoch from
// the restored content. Merging the restore into the existing shared
// document instead would let any client holding the old state re-introduce
// exactly what the restore was meant to remove.
func (s *pgPageStore) RestoreContentVersion(ctx context.Context, pageID uuid.UUID, versionID int64, userID uuid.UUID) (*model.PageContent, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("restore version — begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var lockedPageID uuid.UUID
	if err := tx.QueryRow(ctx,
		`SELECT id FROM pages WHERE id = $2 AND `+PageWriteSQL+` FOR UPDATE`,
		userID, pageID,
	).Scan(&lockedPageID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrForbidden
		}
		return nil, fmt.Errorf("restore version — page lookup: %w", err)
	}

	var (
		content       []byte
		schemaVersion int
	)
	if err := tx.QueryRow(ctx,
		`SELECT content, schema_version FROM page_content_versions WHERE id = $1 AND page_id = $2`,
		versionID, pageID,
	).Scan(&content, &schemaVersion); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("restore version — version lookup: %w", err)
	}

	attached, err := collabAttachedLocked(ctx, tx, pageID)
	if err != nil {
		return nil, fmt.Errorf("restore version — collab lookup: %w", err)
	}
	if attached {
		if err := detachCollabLocked(ctx, tx, pageID); err != nil {
			return nil, fmt.Errorf("restore version — detach: %w", err)
		}
	}

	cur, err := currentContentLocked(ctx, tx, pageID)
	if err != nil {
		return nil, fmt.Errorf("restore version — current lookup: %w", err)
	}
	out, err := writeContentLocked(ctx, tx, pageID, content, schemaVersion, cur)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("restore version — commit: %w", err)
	}
	return out, nil
}
