package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/glyph/api/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type pgPageStore struct{ pool DBPool }

func NewPageStore(pool DBPool) PageStore {
	return &pgPageStore{pool: pool}
}

func scanPage(row interface {
	Scan(...interface{}) error
}) (*model.Page, error) {
	p := &model.Page{}
	var triggerJSON []byte
	if err := row.Scan(
		&p.ID, &p.UserID, &p.Type, &p.Title, &p.ParentID,
		&p.Order, &p.Tags, &p.Priority, &triggerJSON, &p.OrgID, &p.IsPrivate, &p.CreatedAt, &p.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if triggerJSON != nil {
		p.TodoTrigger = &model.TodoTriggerConfig{}
		if err := json.Unmarshal(triggerJSON, p.TodoTrigger); err != nil {
			return nil, fmt.Errorf("unmarshal todo_trigger: %w", err)
		}
	}
	return p, nil
}

const pageColumns = `id, user_id, type, title, parent_id, "order", tags, priority, todo_trigger, org_id, is_private, created_at, updated_at`

// pageAccessFilter enforces the three-tier access policy for pages.
// See store.ResourceAccessFilter for the policy definition.
var pageAccessFilter = ResourceAccessFilter(ResourcePage)

// folderAccessFilter is like pageAccessFilter but checks resource_type = 'folder'
// shares so that folder-specific share grants are respected.
var folderAccessFilter = ResourceAccessFilter(ResourceFolder)

func (s *pgPageStore) ListByUser(ctx context.Context, userID uuid.UUID) ([]*model.Page, error) {
	q := `SELECT ` + pageColumns + ` FROM pages WHERE ` + pageAccessFilter + ` ORDER BY "order" ASC`
	rows, err := s.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("pages list: %w", err)
	}
	defer rows.Close()

	pages := make([]*model.Page, 0)
	for rows.Next() {
		p, err := scanPage(rows)
		if err != nil {
			return nil, fmt.Errorf("pages list scan: %w", err)
		}
		pages = append(pages, p)
	}
	return pages, rows.Err()
}

// ListByUserPaginated returns a page of results with a total count.
func (s *pgPageStore) ListByUserPaginated(ctx context.Context, userID uuid.UUID, pg Pagination) ([]*model.Page, int, error) {
	countQ := `SELECT COUNT(*) FROM pages WHERE ` + pageAccessFilter
	var total int
	if err := s.pool.QueryRow(ctx, countQ, userID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("pages count: %w", err)
	}

	q := `SELECT ` + pageColumns + ` FROM pages WHERE ` + pageAccessFilter + ` ORDER BY "order" ASC LIMIT $2 OFFSET $3`
	rows, err := s.pool.Query(ctx, q, userID, pg.Limit, pg.Offset)
	if err != nil {
		return nil, 0, fmt.Errorf("pages list paginated: %w", err)
	}
	defer rows.Close()

	pages := make([]*model.Page, 0, pg.Limit)
	for rows.Next() {
		p, err := scanPage(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("pages list scan: %w", err)
		}
		pages = append(pages, p)
	}
	return pages, total, rows.Err()
}

func (s *pgPageStore) GetByID(ctx context.Context, id, userID uuid.UUID) (*model.Page, error) {
	// $1 = userID for access filter, $2 = id for row selection
	q := `SELECT ` + pageColumns + ` FROM pages WHERE ` + pageAccessFilter + ` AND id = $2`
	return scanPage(s.pool.QueryRow(ctx, q, userID, id))
}

// GetFolderByID fetches a folder row the same way as GetByID but uses the
// folder access filter (resource_type = 'folder' in shares) so that direct
// folder shares grant read access to the folder board endpoints.
func (s *pgPageStore) GetFolderByID(ctx context.Context, id, userID uuid.UUID) (*model.Page, error) {
	q := `SELECT ` + pageColumns + ` FROM pages WHERE ` + folderAccessFilter + ` AND id = $2`
	return scanPage(s.pool.QueryRow(ctx, q, userID, id))
}

func (s *pgPageStore) Upsert(ctx context.Context, p *model.Page) (*model.Page, error) {
	triggerJSON, err := marshalNullableJSON(p.TodoTrigger)
	if err != nil {
		return nil, err
	}
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	if p.Priority == "" {
		p.Priority = model.PriorityNone
	}
	q := `INSERT INTO pages (id, user_id, parent_id, type, title, "order", tags, priority, todo_trigger, org_id, is_private)
		  VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		  ON CONFLICT (id) DO UPDATE SET
		    parent_id = EXCLUDED.parent_id,
		    type = EXCLUDED.type,
		    title = EXCLUDED.title,
		    "order" = EXCLUDED."order",
		    tags = EXCLUDED.tags,
		    priority = EXCLUDED.priority,
		    todo_trigger = EXCLUDED.todo_trigger,
		    org_id = EXCLUDED.org_id,
		    is_private = EXCLUDED.is_private,
		    updated_at = NOW()
		  WHERE pages.user_id = $2
		  RETURNING ` + pageColumns
	result, err := scanPage(s.pool.QueryRow(ctx, q,
		p.ID, p.UserID, p.ParentID, p.Type, p.Title, p.Order, p.Tags, p.Priority, triggerJSON, p.OrgID, p.IsPrivate,
	))
	if err != nil {
		return nil, err
	}
	return result, nil
}
func (s *pgPageStore) Create(ctx context.Context, p *model.Page) (*model.Page, error) {
	triggerJSON, err := marshalNullableJSON(p.TodoTrigger)
	if err != nil {
		return nil, err
	}
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	if p.Priority == "" {
		p.Priority = model.PriorityNone
	}
	q := `INSERT INTO pages (id, user_id, type, title, parent_id, "order", tags, priority, todo_trigger, org_id, is_private)
		  VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		  RETURNING ` + pageColumns
	return scanPage(s.pool.QueryRow(ctx, q,
		p.ID, p.UserID, p.Type, p.Title, p.ParentID, p.Order, p.Tags, p.Priority, triggerJSON, p.OrgID, p.IsPrivate,
	))
}

func (s *pgPageStore) Update(ctx context.Context, p *model.Page) (*model.Page, error) {
	triggerJSON, err := marshalNullableJSON(p.TodoTrigger)
	if err != nil {
		return nil, err
	}
	if p.Priority == "" {
		p.Priority = model.PriorityNone
	}
	q := `UPDATE pages
		  SET type=$1, title=$2, parent_id=$3, "order"=$4, tags=$5, priority=$6, todo_trigger=$7,
		      org_id=$8, is_private=$9, updated_at=NOW()
		  WHERE id=$10 AND user_id=$11
		  RETURNING ` + pageColumns
	return scanPage(s.pool.QueryRow(ctx, q,
		p.Type, p.Title, p.ParentID, p.Order, p.Tags, p.Priority, triggerJSON,
		p.OrgID, p.IsPrivate, p.ID, p.UserID,
	))
}

func (s *pgPageStore) Delete(ctx context.Context, id, userID uuid.UUID) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM pages WHERE id=$1 AND user_id=$2`, id, userID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// IsAncestor walks the parent_id chain upward from nodeID using a recursive
// CTE and reports whether candidateAncestorID appears anywhere in that chain.
// Returns false (not an error) when either ID does not exist.
func (s *pgPageStore) IsAncestor(ctx context.Context, candidateAncestorID, nodeID uuid.UUID) (bool, error) {
	const q = `
		WITH RECURSIVE ancestors AS (
			SELECT parent_id FROM pages WHERE id = $2
			UNION ALL
			SELECT p.parent_id FROM pages p JOIN ancestors a ON p.id = a.parent_id
			WHERE a.parent_id IS NOT NULL
		)
		SELECT EXISTS (SELECT 1 FROM ancestors WHERE parent_id = $1)`
	var result bool
	if err := s.pool.QueryRow(ctx, q, candidateAncestorID, nodeID).Scan(&result); err != nil {
		return false, fmt.Errorf("is_ancestor: %w", err)
	}
	return result, nil
}

// GetDescendantIDs returns the IDs of all pages/folders that are descendants of
// folderID (including folderID itself). Uses a recursive CTE to walk the tree.
func (s *pgPageStore) GetDescendantIDs(ctx context.Context, folderID uuid.UUID) ([]uuid.UUID, error) {
	const q = `
		WITH RECURSIVE descendants AS (
			SELECT id FROM pages WHERE id = $1
			UNION ALL
			SELECT p.id FROM pages p JOIN descendants d ON p.parent_id = d.id
		)
		SELECT id FROM descendants`
	rows, err := s.pool.Query(ctx, q, folderID)
	if err != nil {
		return nil, fmt.Errorf("get_descendant_ids: %w", err)
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("get_descendant_ids scan: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *pgPageStore) GetContent(ctx context.Context, pageID, userID uuid.UUID) (*model.PageContent, error) {
	// Verify access via expanded access filter (owner OR org member OR direct share).
	// We alias the pages table to "p" but ResourceAccessFilter uses "pages"; rewrite
	// the filter manually here so the alias is consistent with the join.
	// COALESCE handles rows where content was NULL after the TEXT→JSONB migration.
	const q = `
		SELECT pc.page_id, COALESCE(pc.content, '{"type":"doc","content":[]}'::jsonb), pc.updated_at, pc.schema_version, pc.revision
		FROM page_contents pc
		JOIN pages p ON p.id = pc.page_id
		WHERE pc.page_id = $2 AND (
			p.user_id = $1
			OR (p.org_id IS NOT NULL AND p.is_private = false
			    AND p.org_id IN (SELECT org_id FROM org_members WHERE user_id = $1))
			OR EXISTS (SELECT 1 FROM shares
			           WHERE resource_type = 'page' AND resource_id = p.id AND shared_with_id = $1)
		)`
	pc := &model.PageContent{}
	if err := s.pool.QueryRow(ctx, q, userID, pageID).Scan(&pc.PageID, &pc.Content, &pc.UpdatedAt, &pc.SchemaVersion, &pc.Revision); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get content: %w", err)
	}
	if pc.Content == nil {
		pc.Content = json.RawMessage(`{"type":"doc","content":[]}`)
	}
	return pc, nil
}

// Version history retention. A count cap alone (the previous policy kept the
// last 20) is useless once content is autosaved every few seconds by a
// collaborative session: twenty versions span about a minute. Retention is
// therefore time-bucketed — every superseded revision is kept while it is
// recent, then thinned to one per bucket as it ages.
const (
	// contentVersionKeepRecent versions are always kept regardless of age.
	contentVersionKeepRecent = 20
	// maxContentVersionList caps a single ListContentVersions response.
	maxContentVersionList = 200
)

// contentVersionPruneSQL deletes every version of page $1 that no retention
// rule wants: the newest contentVersionKeepRecent, everything from the last
// hour, the newest per 5 minutes for a day, and the newest per day for 30
// days.
const contentVersionPruneSQL = `
	DELETE FROM page_content_versions
	WHERE page_id = $1 AND id NOT IN (
	    SELECT id FROM (
	        SELECT id, replaced_at,
	               row_number() OVER (ORDER BY replaced_at DESC, id DESC) AS rn,
	               row_number() OVER (PARTITION BY date_bin('5 minutes', replaced_at, TIMESTAMPTZ 'epoch')
	                                  ORDER BY replaced_at DESC, id DESC) AS rn_5m,
	               row_number() OVER (PARTITION BY date_trunc('day', replaced_at)
	                                  ORDER BY replaced_at DESC, id DESC) AS rn_day
	        FROM page_content_versions
	        WHERE page_id = $1
	    ) ranked
	    WHERE rn <= $2
	       OR replaced_at > NOW() - INTERVAL '1 hour'
	       OR (replaced_at > NOW() - INTERVAL '24 hours' AND rn_5m = 1)
	       OR (replaced_at > NOW() - INTERVAL '30 days' AND rn_day = 1)
	)`

func (s *pgPageStore) UpsertContent(ctx context.Context, pc *model.PageContent, userID uuid.UUID) (*model.PageContent, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("upsert content — begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Re-check write permission as part of the writing transaction, and take a
	// row lock on the page. This both closes the check-then-write window and
	// serialises concurrent content writes for the same page, so the
	// read-modify-write below cannot interleave. The collab service takes the
	// same lock before seeding, so a REST write can't slip in between its read
	// of page_contents and it attaching the page.
	var lockedPageID uuid.UUID
	err = tx.QueryRow(ctx,
		`SELECT id FROM pages WHERE id = $2 AND `+PageWriteSQL+` FOR UPDATE`,
		userID, pc.PageID,
	).Scan(&lockedPageID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrForbidden
		}
		return nil, fmt.Errorf("upsert content — page lookup: %w", err)
	}

	// While a page is attached to a collaborative session the Yjs log is the
	// source of truth; a whole-document REST write would be silently
	// overwritten by the next snapshot (or, worse, clobber collaborators).
	attached, err := collabAttachedLocked(ctx, tx, pc.PageID)
	if err != nil {
		return nil, fmt.Errorf("upsert content — collab lookup: %w", err)
	}
	if attached {
		if !pc.DetachCollab {
			return nil, ErrCollaborative
		}
		if err := detachCollabLocked(ctx, tx, pc.PageID); err != nil {
			return nil, fmt.Errorf("upsert content — detach: %w", err)
		}
	}

	cur, err := currentContentLocked(ctx, tx, pc.PageID)
	if err != nil {
		return nil, fmt.Errorf("upsert content — current lookup: %w", err)
	}

	// Optimistic concurrency. Once content exists every write must say which
	// revision it was derived from. Treating a missing precondition as
	// "overwrite unconditionally" let clients that never read the page (or an
	// old build that predates revisions) clobber it.
	if cur != nil {
		if pc.ExpectedRevision == 0 {
			return nil, fmt.Errorf("%w: expectedRevision is required, current is %d",
				ErrConflict, cur.revision)
		}
		if pc.ExpectedRevision != cur.revision {
			return nil, fmt.Errorf("%w: content revision %d is stale, current is %d",
				ErrConflict, pc.ExpectedRevision, cur.revision)
		}
	}

	out, err := writeContentLocked(ctx, tx, pc.PageID, pc.Content, pc.SchemaVersion, cur)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("upsert content — commit: %w", err)
	}
	return out, nil
}

// storedContent is the page_contents row being superseded by a write.
type storedContent struct {
	content       []byte
	revision      int
	schemaVersion int
	updatedAt     time.Time
}

// currentContentLocked returns the page's current content row, or nil if the
// page has none yet. The caller must hold the page row lock.
func currentContentLocked(ctx context.Context, tx pgx.Tx, pageID uuid.UUID) (*storedContent, error) {
	cur := &storedContent{}
	err := tx.QueryRow(ctx,
		`SELECT content, revision, schema_version, updated_at FROM page_contents WHERE page_id = $1`,
		pageID,
	).Scan(&cur.content, &cur.revision, &cur.schemaVersion, &cur.updatedAt)
	switch {
	case err == nil:
		return cur, nil
	case errors.Is(err, pgx.ErrNoRows):
		return nil, nil
	default:
		return nil, err
	}
}

// writeContentLocked is the single write path for page_contents, shared by
// REST writes, collaborative snapshots and version restores. It archives the
// superseded revision, writes the new one, applies history retention and
// reconciles the page's bullet-linked tasks with the new document — all inside
// the caller's transaction, which must hold the page row lock.
func writeContentLocked(ctx context.Context, tx pgx.Tx, pageID uuid.UUID, content json.RawMessage, schemaVersion int, cur *storedContent) (*model.PageContent, error) {
	if content == nil {
		content = json.RawMessage(`{"type":"doc","content":[]}`)
	}

	// Archive the revision we are about to replace so it stays recoverable.
	if cur != nil && cur.content != nil {
		if _, err := tx.Exec(ctx,
			`INSERT INTO page_content_versions (page_id, content, revision, schema_version, replaced_at)
			 VALUES ($1, $2, $3, $4, $5)`,
			pageID, cur.content, cur.revision, cur.schemaVersion, cur.updatedAt,
		); err != nil {
			return nil, fmt.Errorf("write content — archive: %w", err)
		}
	}

	const q = `
		INSERT INTO page_contents (page_id, content, schema_version, updated_at, revision)
		VALUES ($1, $2, $3, NOW(), 1)
		ON CONFLICT (page_id) DO UPDATE
		  SET content = EXCLUDED.content,
		      schema_version = EXCLUDED.schema_version,
		      updated_at = NOW(),
		      revision = page_contents.revision + 1
		RETURNING page_id, content, updated_at, schema_version, revision`
	out := &model.PageContent{}
	if err := tx.QueryRow(ctx, q, pageID, content, schemaVersion).Scan(
		&out.PageID, &out.Content, &out.UpdatedAt, &out.SchemaVersion, &out.Revision,
	); err != nil {
		return nil, fmt.Errorf("write content: %w", err)
	}

	if _, err := tx.Exec(ctx, contentVersionPruneSQL, pageID, contentVersionKeepRecent); err != nil {
		return nil, fmt.Errorf("write content — prune history: %w", err)
	}

	if err := reconcileSourceTasksLocked(ctx, tx, pageID, content); err != nil {
		return nil, fmt.Errorf("write content — reconcile tasks: %w", err)
	}
	return out, nil
}

// reconcileSourceTasksLocked makes the page's bullet-linked tasks agree with
// the document just written: a live task whose bullet is gone is soft-deleted
// ('source_removed'), and a task soft-deleted that way whose bullet is back
// (undo, paste, version restore) is restored with all its fields intact.
//
// This used to be done by whichever client noticed a bullet disappear, which
// with several editors meant every connected client deleting the same task —
// including for a cut/paste or an undo. The server is now the only actor, it
// acts on the persisted document, and the operation is idempotent.
func reconcileSourceTasksLocked(ctx context.Context, tx pgx.Tx, pageID uuid.UUID, content json.RawMessage) error {
	nodeIDs, err := ListItemNodeIDs(content)
	if err != nil {
		// The handler validated this document; failing to walk it here means
		// we can't know which bullets exist, so change nothing.
		return nil
	}
	if _, err := tx.Exec(ctx,
		`UPDATE tasks SET deleted_at = NOW(), deleted_reason = 'source_removed', updated_at = NOW()
		 WHERE source_page_id = $1 AND source_node_id IS NOT NULL AND deleted_at IS NULL
		   AND NOT (source_node_id = ANY($2::text[]))`,
		pageID, nodeIDs,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE tasks SET deleted_at = NULL, deleted_reason = NULL, updated_at = NOW()
		 WHERE source_page_id = $1 AND deleted_reason = 'source_removed'
		   AND source_node_id = ANY($2::text[])`,
		pageID, nodeIDs,
	); err != nil {
		return err
	}
	return nil
}

// ListContentVersions returns superseded revisions for a page, newest first.
// Access is gated by the same read filter used by GetContent.
func (s *pgPageStore) ListContentVersions(ctx context.Context, pageID, userID uuid.UUID, limit int) ([]model.PageContentVersion, error) {
	if limit <= 0 || limit > maxContentVersionList {
		limit = maxContentVersionList
	}
	const q = `
		SELECT v.id, v.page_id, v.content, v.revision, v.schema_version, v.replaced_at
		FROM page_content_versions v
		JOIN pages p ON p.id = v.page_id
		WHERE v.page_id = $2 AND (
			p.user_id = $1
			OR (p.org_id IS NOT NULL AND p.is_private = false
			    AND p.org_id IN (SELECT org_id FROM org_members WHERE user_id = $1))
			OR EXISTS (SELECT 1 FROM shares
			           WHERE resource_type = 'page' AND resource_id = p.id AND shared_with_id = $1)
		)
		ORDER BY v.replaced_at DESC, v.id DESC
		LIMIT $3`
	rows, err := s.pool.Query(ctx, q, userID, pageID, limit)
	if err != nil {
		return nil, fmt.Errorf("list content versions: %w", err)
	}
	defer rows.Close()
	out := []model.PageContentVersion{}
	for rows.Next() {
		var v model.PageContentVersion
		if err := rows.Scan(&v.ID, &v.PageID, &v.Content, &v.Revision, &v.SchemaVersion, &v.ReplacedAt); err != nil {
			return nil, fmt.Errorf("list content versions — scan: %w", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list content versions — rows: %w", err)
	}
	return out, nil
}

// marshalNullableJSON encodes v as JSON bytes, returning nil when v is nil.
func marshalNullableJSON(v interface{}) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	b, err := jsonMarshal(v)
	if err != nil {
		return nil, fmt.Errorf("json marshal: %w", err)
	}
	return b, nil
}
