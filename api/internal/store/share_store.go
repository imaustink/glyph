package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/glyph/api/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type pgShareStore struct{ pool DBPool }

func NewShareStore(pool DBPool) ShareStore {
	return &pgShareStore{pool: pool}
}

const shareSelectColumns = `
	s.id, s.resource_type, s.resource_id, s.shared_by_id,
	u.id, u.email, u.name,
	s.permission, s.created_at`

func scanShare(row interface{ Scan(...interface{}) error }) (*model.Share, error) {
	s := &model.Share{}
	if err := row.Scan(
		&s.ID, &s.ResourceType, &s.ResourceID, &s.SharedByID,
		&s.SharedWith.ID, &s.SharedWith.Email, &s.SharedWith.Name,
		&s.Permission, &s.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return s, nil
}

func (s *pgShareStore) Create(ctx context.Context, sh *model.Share) (*model.Share, error) {
	if sh.ID == uuid.Nil {
		sh.ID = uuid.New()
	}
	const q = `
		INSERT INTO shares (id, resource_type, resource_id, shared_by_id, shared_with_id, permission)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id`
	if err := s.pool.QueryRow(ctx, q,
		sh.ID, sh.ResourceType, sh.ResourceID, sh.SharedByID, sh.SharedWith.ID, sh.Permission,
	).Scan(&sh.ID); err != nil {
		return nil, fmt.Errorf("share create: %w", err)
	}
	// Re-fetch with joined user data
	return s.GetByID(ctx, sh.ID)
}

func (s *pgShareStore) GetByID(ctx context.Context, id uuid.UUID) (*model.Share, error) {
	q := `SELECT ` + shareSelectColumns + `
		FROM shares s JOIN users u ON u.id = s.shared_with_id
		WHERE s.id = $1`
	sh, err := scanShare(s.pool.QueryRow(ctx, q, id))
	if err != nil {
		return nil, fmt.Errorf("share get: %w", err)
	}
	return sh, nil
}

func (s *pgShareStore) ListForResource(
	ctx context.Context,
	resourceType model.ShareResourceType,
	resourceID uuid.UUID,
) ([]*model.Share, error) {
	q := `SELECT ` + shareSelectColumns + `
		FROM shares s JOIN users u ON u.id = s.shared_with_id
		WHERE s.resource_type = $1 AND s.resource_id = $2
		ORDER BY s.created_at ASC`
	rows, err := s.pool.Query(ctx, q, resourceType, resourceID)
	if err != nil {
		return nil, fmt.Errorf("shares list: %w", err)
	}
	defer rows.Close()

	shares := make([]*model.Share, 0)
	for rows.Next() {
		sh, err := scanShare(rows)
		if err != nil {
			return nil, fmt.Errorf("shares list scan: %w", err)
		}
		shares = append(shares, sh)
	}
	return shares, rows.Err()
}

func (s *pgShareStore) GetForUserAndResource(
	ctx context.Context,
	userID uuid.UUID,
	resourceType model.ShareResourceType,
	resourceID uuid.UUID,
) (*model.Share, error) {
	q := `SELECT ` + shareSelectColumns + `
		FROM shares s JOIN users u ON u.id = s.shared_with_id
		WHERE s.shared_with_id = $1 AND s.resource_type = $2 AND s.resource_id = $3`
	sh, err := scanShare(s.pool.QueryRow(ctx, q, userID, resourceType, resourceID))
	if err != nil {
		return nil, fmt.Errorf("share get for user: %w", err)
	}
	return sh, nil
}

func (s *pgShareStore) UpdatePermission(ctx context.Context, id uuid.UUID, permission model.SharePermission) (*model.Share, error) {
	_, err := s.pool.Exec(ctx,
		`UPDATE shares SET permission=$1 WHERE id=$2`, permission, id)
	if err != nil {
		return nil, fmt.Errorf("share update permission: %w", err)
	}
	return s.GetByID(ctx, id)
}

func (s *pgShareStore) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM shares WHERE id=$1`, id)
	return err
}
