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
		SELECT pc.page_id, COALESCE(pc.content, '{"type":"doc","content":[]}'::jsonb), pc.updated_at, pc.schema_version
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
	if err := s.pool.QueryRow(ctx, q, userID, pageID).Scan(&pc.PageID, &pc.Content, &pc.UpdatedAt, &pc.SchemaVersion); err != nil {
		return nil, fmt.Errorf("get content: %w", err)
	}
	if pc.Content == nil {
		pc.Content = json.RawMessage(`{"type":"doc","content":[]}`)
	}
	return pc, nil
}

func (s *pgPageStore) UpsertContent(ctx context.Context, pc *model.PageContent, userID uuid.UUID) (*model.PageContent, error) {
	// Verify the page exists and the user has access (through the page access filter).
	// The handler already checked write permission before calling this, so we only
	// confirm the page exists via the standard access filter.
	// pageAccessFilter uses $1 = userID; we add id = $2 for the specific page.
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pages WHERE `+pageAccessFilter+` AND id = $2)`, userID, pc.PageID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("upsert content — page lookup: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("upsert content: forbidden")
	}

	if pc.Content == nil {
		pc.Content = json.RawMessage(`{"type":"doc","content":[]}`)
	}

	const q = `
		INSERT INTO page_contents (page_id, content, schema_version, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (page_id) DO UPDATE
		  SET content = EXCLUDED.content,
		      schema_version = EXCLUDED.schema_version,
		      updated_at = NOW()
		RETURNING page_id, content, updated_at, schema_version`
	out := &model.PageContent{}
	if err := s.pool.QueryRow(ctx, q, pc.PageID, pc.Content, pc.SchemaVersion).Scan(
		&out.PageID, &out.Content, &out.UpdatedAt, &out.SchemaVersion,
	); err != nil {
		return nil, fmt.Errorf("upsert content: %w", err)
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
