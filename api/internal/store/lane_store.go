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

type pgLaneStore struct{ pool DBPool }

func NewLaneStore(pool DBPool) LaneStore {
	return &pgLaneStore{pool: pool}
}

const laneColumns = `id, user_id, title, filter_set, sort_config, "order", folder_id, created_at, updated_at`

const laneAccessFilter = `user_id = $1`

func scanLane(row interface{ Scan(...interface{}) error }) (*model.Lane, error) {
	l := &model.Lane{}
	var filterJSON, sortJSON []byte
	if err := row.Scan(
		&l.ID, &l.UserID, &l.Title, &filterJSON, &sortJSON, &l.Order, &l.FolderID, &l.CreatedAt, &l.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := json.Unmarshal(filterJSON, &l.FilterSet); err != nil {
		return nil, fmt.Errorf("unmarshal filter_set: %w", err)
	}
	if err := json.Unmarshal(sortJSON, &l.SortConfig); err != nil {
		return nil, fmt.Errorf("unmarshal sort_config: %w", err)
	}
	return l, nil
}

func (s *pgLaneStore) ListByUser(ctx context.Context, userID uuid.UUID) ([]*model.Lane, error) {
	// Personal board: only lanes with no folder scope.
	q := `SELECT ` + laneColumns + ` FROM lanes WHERE ` + laneAccessFilter + ` AND folder_id IS NULL ORDER BY "order" ASC`
	rows, err := s.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("lanes list: %w", err)
	}
	defer rows.Close()
	lanes := make([]*model.Lane, 0)
	for rows.Next() {
		l, err := scanLane(rows)
		if err != nil {
			return nil, fmt.Errorf("lanes scan: %w", err)
		}
		lanes = append(lanes, l)
	}
	return lanes, rows.Err()
}

// ListByFolder returns all lanes scoped to the given folder, accessible by the
// requesting user (owner or any user with folder access).
func (s *pgLaneStore) ListByFolder(ctx context.Context, folderID, userID uuid.UUID) ([]*model.Lane, error) {
	// Any user who can read the folder can list its lanes.
	// We verify folder access in the handler before calling this; here we just
	// filter by folder_id and include lanes owned by any user for that folder.
	q := `SELECT ` + laneColumns + ` FROM lanes WHERE folder_id = $1 ORDER BY "order" ASC`
	rows, err := s.pool.Query(ctx, q, folderID)
	if err != nil {
		return nil, fmt.Errorf("lanes list by folder: %w", err)
	}
	defer rows.Close()
	lanes := make([]*model.Lane, 0)
	for rows.Next() {
		l, err := scanLane(rows)
		if err != nil {
			return nil, fmt.Errorf("lanes list by folder scan: %w", err)
		}
		lanes = append(lanes, l)
	}
	return lanes, rows.Err()
}

func (s *pgLaneStore) GetByID(ctx context.Context, id, userID uuid.UUID) (*model.Lane, error) {
	q := `SELECT ` + laneColumns + ` FROM lanes WHERE ` + laneAccessFilter + ` AND id = $2`
	return scanLane(s.pool.QueryRow(ctx, q, userID, id))
}

// GetByIDAndFolder fetches a lane by ID if it belongs to the given folder.
// Access control (folder read permission) is assumed to be already verified by the caller.
func (s *pgLaneStore) GetByIDAndFolder(ctx context.Context, id, folderID uuid.UUID) (*model.Lane, error) {
	q := `SELECT ` + laneColumns + ` FROM lanes WHERE id = $1 AND folder_id = $2`
	return scanLane(s.pool.QueryRow(ctx, q, id, folderID))
}

func (s *pgLaneStore) Upsert(ctx context.Context, l *model.Lane) (*model.Lane, error) {
	filterJSON, err := jsonMarshal(l.FilterSet)
	if err != nil {
		return nil, fmt.Errorf("marshal filter_set: %w", err)
	}
	sortJSON, err := jsonMarshal(l.SortConfig)
	if err != nil {
		return nil, fmt.Errorf("marshal sort_config: %w", err)
	}
	if l.ID == uuid.Nil {
		l.ID = uuid.New()
	}
	q := `INSERT INTO lanes (id, user_id, title, filter_set, sort_config, "order", folder_id)
		  VALUES ($1,$2,$3,$4,$5,$6,$7)
		  ON CONFLICT (id) DO UPDATE SET
		    title = EXCLUDED.title,
		    filter_set = EXCLUDED.filter_set,
		    sort_config = EXCLUDED.sort_config,
		    "order" = EXCLUDED."order",
		    updated_at = NOW()
		  WHERE lanes.user_id = $2
		  RETURNING ` + laneColumns
	result, err := scanLane(s.pool.QueryRow(ctx, q, l.ID, l.UserID, l.Title, filterJSON, sortJSON, l.Order, l.FolderID))
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *pgLaneStore) ReorderAll(ctx context.Context, userID uuid.UUID, items []LaneReorderItem) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, item := range items {
		_, err := tx.Exec(ctx,
			`UPDATE lanes SET "order"=$1, updated_at=NOW() WHERE id=$2 AND user_id=$3`,
			item.Order, item.ID, userID)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func (s *pgLaneStore) Create(ctx context.Context, l *model.Lane) (*model.Lane, error) {
	filterJSON, err := jsonMarshal(l.FilterSet)
	if err != nil {
		return nil, fmt.Errorf("marshal filter_set: %w", err)
	}
	sortJSON, err := jsonMarshal(l.SortConfig)
	if err != nil {
		return nil, fmt.Errorf("marshal sort_config: %w", err)
	}
	if l.ID == uuid.Nil {
		l.ID = uuid.New()
	}
	q := `INSERT INTO lanes (id, user_id, title, filter_set, sort_config, "order", folder_id)
		  VALUES ($1,$2,$3,$4,$5,$6,$7)
		  RETURNING ` + laneColumns
	return scanLane(s.pool.QueryRow(ctx, q, l.ID, l.UserID, l.Title, filterJSON, sortJSON, l.Order, l.FolderID))
}

func (s *pgLaneStore) BatchCreate(ctx context.Context, lanes []*model.Lane) ([]*model.Lane, error) {
	if len(lanes) == 0 {
		return []*model.Lane{}, nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("batch create lanes begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	result := make([]*model.Lane, 0, len(lanes))
	for _, l := range lanes {
		filterJSON, err := jsonMarshal(l.FilterSet)
		if err != nil {
			return nil, fmt.Errorf("marshal filter_set: %w", err)
		}
		sortJSON, err := jsonMarshal(l.SortConfig)
		if err != nil {
			return nil, fmt.Errorf("marshal sort_config: %w", err)
		}
		if l.ID == uuid.Nil {
			l.ID = uuid.New()
		}
		q := `INSERT INTO lanes (id, user_id, title, filter_set, sort_config, "order", folder_id)
			  VALUES ($1,$2,$3,$4,$5,$6,$7)
			  RETURNING ` + laneColumns
		created, err := scanLane(tx.QueryRow(ctx, q, l.ID, l.UserID, l.Title, filterJSON, sortJSON, l.Order, l.FolderID))
		if err != nil {
			return nil, fmt.Errorf("batch create lane: %w", err)
		}
		result = append(result, created)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("batch create lanes commit: %w", err)
	}
	return result, nil
}

func (s *pgLaneStore) Update(ctx context.Context, l *model.Lane) (*model.Lane, error) {
	filterJSON, err := jsonMarshal(l.FilterSet)
	if err != nil {
		return nil, fmt.Errorf("marshal filter_set: %w", err)
	}
	sortJSON, err := jsonMarshal(l.SortConfig)
	if err != nil {
		return nil, fmt.Errorf("marshal sort_config: %w", err)
	}
	q := `UPDATE lanes
		  SET title=$1, filter_set=$2, sort_config=$3, "order"=$4, updated_at=NOW()
		  WHERE id=$5 AND user_id=$6
		  RETURNING ` + laneColumns
	return scanLane(s.pool.QueryRow(ctx, q, l.Title, filterJSON, sortJSON, l.Order, l.ID, l.UserID))
}

func (s *pgLaneStore) Patch(ctx context.Context, id, userID uuid.UUID, fn func(*model.Lane) error) (*model.Lane, error) {
	return s.patch(ctx, `id = $1 AND user_id = $2`, []any{id, userID}, fn)
}

// PatchByIDAndFolder is Patch for a folder-scoped lane. Access control
// (folder write permission) is assumed to be already verified by the caller.
func (s *pgLaneStore) PatchByIDAndFolder(ctx context.Context, id, folderID uuid.UUID, fn func(*model.Lane) error) (*model.Lane, error) {
	return s.patch(ctx, `id = $1 AND folder_id = $2`, []any{id, folderID}, fn)
}

// patch locks the lane matching where, applies fn and writes the editable
// columns back in the same transaction, so a concurrent edit to another
// field is seen instead of overwritten.
func (s *pgLaneStore) patch(ctx context.Context, where string, args []any, fn func(*model.Lane) error) (*model.Lane, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("patch lane — begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	l, err := scanLane(tx.QueryRow(ctx, `SELECT `+laneColumns+` FROM lanes WHERE `+where+` FOR UPDATE`, args...))
	if err != nil {
		return nil, err
	}
	if err := fn(l); err != nil {
		return nil, err
	}
	filterJSON, err := jsonMarshal(l.FilterSet)
	if err != nil {
		return nil, fmt.Errorf("marshal filter_set: %w", err)
	}
	sortJSON, err := jsonMarshal(l.SortConfig)
	if err != nil {
		return nil, fmt.Errorf("marshal sort_config: %w", err)
	}
	out, err := scanLane(tx.QueryRow(ctx, `UPDATE lanes
		  SET title=$1, filter_set=$2, sort_config=$3, "order"=$4, updated_at=NOW()
		  WHERE id=$5
		  RETURNING `+laneColumns, l.Title, filterJSON, sortJSON, l.Order, l.ID))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("patch lane — commit: %w", err)
	}
	return out, nil
}

// UpdateByIDAndFolder updates a folder-scoped lane regardless of who created it.
// Access control (folder write permission) is assumed to be already verified by the caller.
func (s *pgLaneStore) UpdateByIDAndFolder(ctx context.Context, l *model.Lane, folderID uuid.UUID) (*model.Lane, error) {
	filterJSON, err := jsonMarshal(l.FilterSet)
	if err != nil {
		return nil, fmt.Errorf("marshal filter_set: %w", err)
	}
	sortJSON, err := jsonMarshal(l.SortConfig)
	if err != nil {
		return nil, fmt.Errorf("marshal sort_config: %w", err)
	}
	q := `UPDATE lanes
		  SET title=$1, filter_set=$2, sort_config=$3, "order"=$4, updated_at=NOW()
		  WHERE id=$5 AND folder_id=$6
		  RETURNING ` + laneColumns
	return scanLane(s.pool.QueryRow(ctx, q, l.Title, filterJSON, sortJSON, l.Order, l.ID, folderID))
}

func (s *pgLaneStore) Delete(ctx context.Context, id, userID uuid.UUID) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM lanes WHERE id=$1 AND user_id=$2`, id, userID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteByIDAndFolder deletes a lane by ID if it belongs to the given folder.
// Access control (folder write permission) is assumed to be already verified by the caller.
func (s *pgLaneStore) DeleteByIDAndFolder(ctx context.Context, id, folderID uuid.UUID) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM lanes WHERE id=$1 AND folder_id=$2`, id, folderID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
