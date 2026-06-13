package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/glyph/api/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type pgUserStore struct{ pool DBPool }

func NewUserStore(pool DBPool) UserStore {
	return &pgUserStore{pool: pool}
}

func (s *pgUserStore) Upsert(ctx context.Context, sub, issuer string, email, name *string) (*model.User, error) {
	const q = `
		INSERT INTO users (sub, issuer, email, name)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (sub, issuer) DO UPDATE
			SET email = EXCLUDED.email,
			    name  = EXCLUDED.name,
			    updated_at = NOW()
		RETURNING id, sub, issuer, email, name, created_at, updated_at`

	u := &model.User{}
	err := s.pool.QueryRow(ctx, q, sub, issuer, email, name).Scan(
		&u.ID, &u.Sub, &u.Issuer, &u.Email, &u.Name, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("user upsert: %w", err)
	}
	return u, nil
}

func (s *pgUserStore) GetByID(ctx context.Context, id uuid.UUID) (*model.User, error) {
	const q = `SELECT id, sub, issuer, email, name, created_at, updated_at FROM users WHERE id = $1`
	u := &model.User{}
	err := s.pool.QueryRow(ctx, q, id).Scan(
		&u.ID, &u.Sub, &u.Issuer, &u.Email, &u.Name, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("user get: %w", err)
	}
	return u, nil
}

func (s *pgUserStore) GetByEmail(ctx context.Context, email string) (*model.User, error) {
	const q = `SELECT id, sub, issuer, email, name, created_at, updated_at FROM users WHERE email = $1`
	u := &model.User{}
	err := s.pool.QueryRow(ctx, q, email).Scan(
		&u.ID, &u.Sub, &u.Issuer, &u.Email, &u.Name, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("user get by email: %w", err)
	}
	return u, nil
}

func (s *pgUserStore) Search(ctx context.Context, query string, excludeID uuid.UUID, orgIDs []uuid.UUID, limit int) ([]*model.UserSearchResult, error) {
	pattern := "%" + query + "%"
	var rows pgx.Rows
	var err error
	if len(orgIDs) > 0 {
		const q = `
			SELECT DISTINCT u.id, u.email, u.name
			FROM users u
			JOIN org_members om ON om.user_id = u.id
			WHERE u.id != $1
			  AND om.org_id = ANY($2)
			  AND (u.email ILIKE $3 OR u.name ILIKE $3)
			ORDER BY u.name ASC, u.email ASC
			LIMIT $4`
		rows, err = s.pool.Query(ctx, q, excludeID, orgIDs, pattern, limit)
	} else {
		const q = `
			SELECT id, email, name FROM users
			WHERE id != $1
			  AND (email ILIKE $2 OR name ILIKE $2)
			ORDER BY name ASC, email ASC
			LIMIT $3`
		rows, err = s.pool.Query(ctx, q, excludeID, pattern, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("user search: %w", err)
	}
	defer rows.Close()

	results := make([]*model.UserSearchResult, 0)
	for rows.Next() {
		r := &model.UserSearchResult{}
		if err := rows.Scan(&r.ID, &r.Email, &r.Name); err != nil {
			return nil, fmt.Errorf("user search scan: %w", err)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}
