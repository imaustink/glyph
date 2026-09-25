package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/glyph/api/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type pgTaskStore struct{ pool DBPool }

func NewTaskStore(pool DBPool) TaskStore {
	return &pgTaskStore{pool: pool}
}

const taskColumns = `id, user_id, title, description, status, priority, tags, due_date::text, source_page_id, source_node_id, link, "order", org_id, is_private, folder_id, created_at, updated_at`

// taskAccessFilter enforces the task read policy and hides soft-deleted tasks
// from every read. A task is readable by the three tiers of
// store.ResourceAccessFilter, and — if it comes from a note — by anyone who
// can read that note: a note's tasks are part of the note.
var taskAccessFilter = "((" + ResourceAccessFilter(ResourceTask) + " OR " + taskSourcePageReadable + ") AND tasks.deleted_at IS NULL)"

// taskSourcePageReadable mirrors the page read filter for the task's source
// page. $1 = userID.
const taskSourcePageReadable = `(tasks.source_page_id IS NOT NULL AND EXISTS (
	SELECT 1 FROM pages sp
	WHERE sp.id = tasks.source_page_id AND (
		sp.user_id = $1
		OR (sp.org_id IS NOT NULL AND sp.is_private = false
		    AND sp.org_id IN (SELECT org_id FROM org_members WHERE user_id = $1))
		OR EXISTS (SELECT 1 FROM shares
		           WHERE resource_type = 'page' AND resource_id = sp.id AND shared_with_id = $1)
	)
))`

func scanTask(row interface{ Scan(...interface{}) error }) (*model.Task, error) {
	t := &model.Task{}
	var linkJSON []byte
	if err := row.Scan(
		&t.ID, &t.UserID, &t.Title, &t.Description, &t.Status, &t.Priority,
		&t.Tags, &t.DueDate, &t.SourcePageID, &t.SourceNodeID, &linkJSON, &t.Order,
		&t.OrgID, &t.IsPrivate, &t.FolderID, &t.CreatedAt, &t.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if len(linkJSON) > 0 {
		var lm model.LinkMeta
		if err := json.Unmarshal(linkJSON, &lm); err == nil {
			t.Link = &lm
		}
	}
	return t, nil
}

func (s *pgTaskStore) ListByUser(ctx context.Context, userID uuid.UUID) ([]*model.Task, error) {
	q := `SELECT ` + taskColumns + ` FROM tasks WHERE ` + taskAccessFilter + ` ORDER BY "order" ASC`
	rows, err := s.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("tasks list: %w", err)
	}
	defer rows.Close()
	tasks := make([]*model.Task, 0)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("tasks list scan: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// ListByUserPaginated returns a page of results with a total count.
func (s *pgTaskStore) ListByUserPaginated(ctx context.Context, userID uuid.UUID, pg Pagination) ([]*model.Task, int, error) {
	countQ := `SELECT COUNT(*) FROM tasks WHERE ` + taskAccessFilter
	var total int
	if err := s.pool.QueryRow(ctx, countQ, userID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("tasks count: %w", err)
	}

	q := `SELECT ` + taskColumns + ` FROM tasks WHERE ` + taskAccessFilter + ` ORDER BY "order" ASC LIMIT $2 OFFSET $3`
	rows, err := s.pool.Query(ctx, q, userID, pg.Limit, pg.Offset)
	if err != nil {
		return nil, 0, fmt.Errorf("tasks list paginated: %w", err)
	}
	defer rows.Close()
	tasks := make([]*model.Task, 0, pg.Limit)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("tasks list scan: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, total, rows.Err()
}

func (s *pgTaskStore) GetByID(ctx context.Context, id, userID uuid.UUID) (*model.Task, error) {
	q := `SELECT ` + taskColumns + ` FROM tasks WHERE ` + taskAccessFilter + ` AND id = $2`
	return scanTask(s.pool.QueryRow(ctx, q, userID, id))
}

func (s *pgTaskStore) ListBySourcePage(ctx context.Context, userID uuid.UUID, pageID uuid.UUID) ([]*model.Task, error) {
	q := `SELECT ` + taskColumns + ` FROM tasks WHERE ` + taskAccessFilter + ` AND source_page_id = $2 ORDER BY "order" ASC`
	rows, err := s.pool.Query(ctx, q, userID, pageID)
	if err != nil {
		return nil, fmt.Errorf("tasks list by page: %w", err)
	}
	defer rows.Close()
	tasks := make([]*model.Task, 0)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("tasks list by page scan: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (s *pgTaskStore) ListBySourceNode(ctx context.Context, userID uuid.UUID, sourceNodeID string) ([]*model.Task, error) {
	q := `SELECT ` + taskColumns + ` FROM tasks WHERE ` + taskAccessFilter + ` AND source_node_id = $2 ORDER BY "order" ASC`
	rows, err := s.pool.Query(ctx, q, userID, sourceNodeID)
	if err != nil {
		return nil, fmt.Errorf("tasks list by node: %w", err)
	}
	defer rows.Close()
	tasks := make([]*model.Task, 0)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("tasks list by node scan: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (s *pgTaskStore) Upsert(ctx context.Context, t *model.Task) (*model.Task, error) {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	var linkJSON []byte
	if t.Link != nil {
		linkJSON, _ = json.Marshal(t.Link)
	}
	q := `INSERT INTO tasks (id, user_id, title, description, status, priority, tags, due_date, source_page_id, source_node_id, link, "order", org_id, is_private, folder_id)
		  VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		  ON CONFLICT (id) DO UPDATE SET
		    title = EXCLUDED.title,
		    description = EXCLUDED.description,
		    status = EXCLUDED.status,
		    priority = EXCLUDED.priority,
		    tags = EXCLUDED.tags,
		    due_date = EXCLUDED.due_date,
		    source_page_id = EXCLUDED.source_page_id,
		    source_node_id = EXCLUDED.source_node_id,
		    link = EXCLUDED.link,
		    "order" = EXCLUDED."order",
		    org_id = EXCLUDED.org_id,
		    is_private = EXCLUDED.is_private,
		    folder_id = EXCLUDED.folder_id,
		    updated_at = NOW()
		  WHERE tasks.user_id = $2 AND tasks.deleted_at IS NULL
		  RETURNING ` + taskColumns
	// A soft-deleted row matches ON CONFLICT (id) but not the WHERE, so a stale
	// client re-PUTting a task whose bullet was removed gets ErrNotFound
	// instead of resurrecting it.
	result, err := scanTask(s.pool.QueryRow(ctx, q,
		t.ID, t.UserID, t.Title, t.Description, t.Status, t.Priority,
		t.Tags, t.DueDate, t.SourcePageID, t.SourceNodeID, linkJSON, t.Order, t.OrgID, t.IsPrivate, t.FolderID,
	))
	if err != nil {
		return nil, mapUniqueViolation(err)
	}
	return result, nil
}
func (s *pgTaskStore) Create(ctx context.Context, t *model.Task) (*model.Task, error) {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	var linkJSON []byte
	if t.Link != nil {
		linkJSON, _ = json.Marshal(t.Link)
	}
	q := `INSERT INTO tasks (id, user_id, title, description, status, priority, tags, due_date, source_page_id, source_node_id, link, "order", org_id, is_private, folder_id)
		  VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		  RETURNING ` + taskColumns
	out, err := scanTask(s.pool.QueryRow(ctx, q,
		t.ID, t.UserID, t.Title, t.Description, t.Status, t.Priority,
		t.Tags, t.DueDate, t.SourcePageID, t.SourceNodeID, linkJSON, t.Order, t.OrgID, t.IsPrivate, t.FolderID,
	))
	if err != nil {
		return nil, mapUniqueViolation(err)
	}
	return out, nil
}

// CreateLinked creates the task for a bullet, or returns the task that bullet
// is already linked to. See TaskStore.CreateLinked.
func (s *pgTaskStore) CreateLinked(ctx context.Context, t *model.Task) (*model.Task, bool, error) {
	if t.SourcePageID == nil || t.SourceNodeID == nil {
		return nil, false, fmt.Errorf("create linked task: sourcePageId and sourceNodeId are required")
	}
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	var linkJSON []byte
	if t.Link != nil {
		linkJSON, _ = json.Marshal(t.Link)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("create linked task — begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// No conflict target: this absorbs both a retried request (same id) and a
	// second client racing to create a task for the same bullet (same
	// page+node). Either way the loser falls through to the lookup below.
	q := `INSERT INTO tasks (id, user_id, title, description, status, priority, tags, due_date, source_page_id, source_node_id, link, "order", org_id, is_private, folder_id)
		  VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		  ON CONFLICT DO NOTHING
		  RETURNING ` + taskColumns
	created, err := scanTask(tx.QueryRow(ctx, q,
		t.ID, t.UserID, t.Title, t.Description, t.Status, t.Priority,
		t.Tags, t.DueDate, t.SourcePageID, t.SourceNodeID, linkJSON, t.Order, t.OrgID, t.IsPrivate, t.FolderID,
	))
	if err == nil {
		if err := tx.Commit(ctx); err != nil {
			return nil, false, fmt.Errorf("create linked task — commit: %w", err)
		}
		return created, true, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, false, fmt.Errorf("create linked task: %w", err)
	}

	// Something already holds this bullet or this id. Prefer the bullet's
	// task; the id match only matters for a retried request.
	existing, deleted, err := scanTaskWithDeleted(tx.QueryRow(ctx,
		`SELECT `+taskColumns+`, deleted_at IS NOT NULL FROM tasks
		 WHERE (source_page_id = $1 AND source_node_id = $2) OR id = $3
		 ORDER BY (source_page_id = $1 AND source_node_id = $2) DESC NULLS LAST
		 LIMIT 1 FOR UPDATE`,
		t.SourcePageID, t.SourceNodeID, t.ID,
	))
	if err != nil {
		return nil, false, fmt.Errorf("create linked task — lookup: %w", err)
	}
	if existing.SourcePageID == nil || existing.SourceNodeID == nil ||
		*existing.SourcePageID != *t.SourcePageID || *existing.SourceNodeID != *t.SourceNodeID {
		return nil, false, fmt.Errorf("%w: task id already in use", ErrConflict)
	}
	if deleted {
		// Restoring someone else's deleted task would let any editor of the
		// page resurrect (and so read) a task they were never shown.
		if existing.UserID != t.UserID {
			return nil, false, fmt.Errorf("%w: bullet is linked to a deleted task owned by another user", ErrConflict)
		}
		existing, err = scanTask(tx.QueryRow(ctx,
			`UPDATE tasks SET deleted_at = NULL, deleted_reason = NULL, updated_at = NOW()
			 WHERE id = $1 RETURNING `+taskColumns, existing.ID))
		if err != nil {
			return nil, false, fmt.Errorf("create linked task — restore: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, fmt.Errorf("create linked task — commit: %w", err)
	}
	return existing, false, nil
}

func scanTaskWithDeleted(row interface{ Scan(...interface{}) error }) (*model.Task, bool, error) {
	t := &model.Task{}
	var (
		linkJSON []byte
		deleted  bool
	)
	if err := row.Scan(
		&t.ID, &t.UserID, &t.Title, &t.Description, &t.Status, &t.Priority,
		&t.Tags, &t.DueDate, &t.SourcePageID, &t.SourceNodeID, &linkJSON, &t.Order,
		&t.OrgID, &t.IsPrivate, &t.FolderID, &t.CreatedAt, &t.UpdatedAt, &deleted,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, ErrNotFound
		}
		return nil, false, err
	}
	if len(linkJSON) > 0 {
		var lm model.LinkMeta
		if err := json.Unmarshal(linkJSON, &lm); err == nil {
			t.Link = &lm
		}
	}
	return t, deleted, nil
}

// mapUniqueViolation turns a Postgres unique_violation into ErrConflict so
// handlers answer 409 instead of 500.
func mapUniqueViolation(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return fmt.Errorf("%w: %s", ErrConflict, pgErr.ConstraintName)
	}
	return err
}

func (s *pgTaskStore) Update(ctx context.Context, t *model.Task) (*model.Task, error) {
	var linkJSON []byte
	if t.Link != nil {
		linkJSON, _ = json.Marshal(t.Link)
	}
	q := `UPDATE tasks
		  SET title=$1, description=$2, status=$3, priority=$4, tags=$5,
		      due_date=$6, source_page_id=$7, source_node_id=$8, link=$9, "order"=$10,
		      org_id=$11, is_private=$12, folder_id=$13, updated_at=NOW()
		  WHERE id=$14 AND user_id=$15 AND deleted_at IS NULL
		  RETURNING ` + taskColumns
	out, err := scanTask(s.pool.QueryRow(ctx, q,
		t.Title, t.Description, t.Status, t.Priority, t.Tags,
		t.DueDate, t.SourcePageID, t.SourceNodeID, linkJSON, t.Order,
		t.OrgID, t.IsPrivate, t.FolderID, t.ID, t.UserID,
	))
	if err != nil {
		return nil, mapUniqueViolation(err)
	}
	return out, nil
}

// Delete soft-deletes a task. The row is kept (deleted_reason 'user') so the
// delete is recoverable and the bullet's link can't be claimed by a new task.
func (s *pgTaskStore) Delete(ctx context.Context, id, userID uuid.UUID) error {
	result, err := s.pool.Exec(ctx,
		`UPDATE tasks SET deleted_at = NOW(), deleted_reason = 'user', updated_at = NOW()
		 WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL`, id, userID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *pgTaskStore) ListByFilter(ctx context.Context, userID uuid.UUID, fs model.FilterSet) ([]*model.Task, error) {
	filterClause, filterArgs := BuildTaskFilterSQL(fs, 2) // $1 = userID

	q := `SELECT ` + taskColumns + ` FROM tasks WHERE ` + taskAccessFilter + ` ORDER BY "order" ASC`
	baseArgs := []interface{}{userID}

	if filterClause != "" {
		q = `SELECT ` + taskColumns + ` FROM tasks WHERE ` + taskAccessFilter + ` AND ` + filterClause + ` ORDER BY "order" ASC`
		baseArgs = append(baseArgs, filterArgs...)
	}

	rows, err := s.pool.Query(ctx, q, baseArgs...)
	if err != nil {
		return nil, fmt.Errorf("list tasks by filter: %w", err)
	}
	defer rows.Close()
	tasks := make([]*model.Task, 0)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("list tasks by filter scan: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// ListByFolder returns all tasks that belong to the given folder: either via
// source_page_id being a descendant of the folder, or via tasks.folder_id = folderID.
// Access control is assumed to have been verified by the caller (folder handler).
// descendantPageIDs must be pre-computed by PageStore.GetDescendantIDs (inclusive of folderID).
func (s *pgTaskStore) ListByFolder(ctx context.Context, folderID uuid.UUID, descendantPageIDs []uuid.UUID) ([]*model.Task, error) {
	if len(descendantPageIDs) == 0 {
		// No descendants — only standalone tasks assigned to the folder.
		q := `SELECT ` + taskColumns + ` FROM tasks WHERE folder_id = $1 AND deleted_at IS NULL ORDER BY "order" ASC`
		rows, err := s.pool.Query(ctx, q, folderID)
		if err != nil {
			return nil, fmt.Errorf("list tasks by folder (standalone): %w", err)
		}
		defer rows.Close()
		tasks := make([]*model.Task, 0)
		for rows.Next() {
			t, err := scanTask(rows)
			if err != nil {
				return nil, fmt.Errorf("list tasks by folder scan: %w", err)
			}
			tasks = append(tasks, t)
		}
		return tasks, rows.Err()
	}

	// Build a parameterised IN clause for descendant page IDs.
	// $1 = folderID, $2..$N = descendant page IDs.
	placeholders := make([]string, len(descendantPageIDs))
	args := make([]interface{}, 0, len(descendantPageIDs)+1)
	args = append(args, folderID)
	for i, id := range descendantPageIDs {
		args = append(args, id)
		placeholders[i] = fmt.Sprintf("$%d", i+2)
	}
	inClause := "(" + joinStrings(placeholders, ",") + ")"

	q := `SELECT ` + taskColumns + ` FROM tasks
		  WHERE (source_page_id IN ` + inClause + ` OR folder_id = $1) AND deleted_at IS NULL
		  ORDER BY "order" ASC`
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list tasks by folder: %w", err)
	}
	defer rows.Close()
	tasks := make([]*model.Task, 0)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("list tasks by folder scan: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// joinStrings joins a slice of strings with a separator.
func joinStrings(ss []string, sep string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}
