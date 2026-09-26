package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/glyph/api/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type pgOrgStore struct{ pool DBPool }

func NewOrgStore(pool DBPool) OrgStore {
	return &pgOrgStore{pool: pool}
}

// ─── Organization CRUD ────────────────────────────────────────────────────────

func (s *pgOrgStore) Create(ctx context.Context, org *model.Organization) (*model.Organization, error) {
	if org.ID == uuid.Nil {
		org.ID = uuid.New()
	}
	const q = `
		INSERT INTO organizations (id, name, created_by)
		VALUES ($1, $2, $3)
		RETURNING id, name, created_by, created_at, updated_at`
	out := &model.Organization{}
	if err := s.pool.QueryRow(ctx, q, org.ID, org.Name, org.CreatedBy).Scan(
		&out.ID, &out.Name, &out.CreatedBy, &out.CreatedAt, &out.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("org create: %w", err)
	}
	return out, nil
}

// CreateWithOwner inserts the org and its creator's owner membership in one
// transaction. Done as two separate writes, a failure in between left an
// org that nobody belonged to, so nobody could see, fix or delete it.
func (s *pgOrgStore) CreateWithOwner(ctx context.Context, org *model.Organization) (*model.Organization, error) {
	if org.ID == uuid.Nil {
		org.ID = uuid.New()
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("org create — begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	out := &model.Organization{}
	if err := tx.QueryRow(ctx, `
		INSERT INTO organizations (id, name, created_by)
		VALUES ($1, $2, $3)
		RETURNING id, name, created_by, created_at, updated_at`,
		org.ID, org.Name, org.CreatedBy,
	).Scan(&out.ID, &out.Name, &out.CreatedBy, &out.CreatedAt, &out.UpdatedAt); err != nil {
		return nil, fmt.Errorf("org create: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, $3)`,
		out.ID, out.CreatedBy, model.OrgRoleOwner); err != nil {
		return nil, fmt.Errorf("org create — add owner: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("org create — commit: %w", err)
	}
	out.MemberCount = 1
	return out, nil
}

func (s *pgOrgStore) GetByID(ctx context.Context, id uuid.UUID) (*model.Organization, error) {
	const q = `
		SELECT o.id, o.name, o.created_by,
		       COALESCE(c.member_count, 0) AS member_count,
		       o.created_at, o.updated_at
		FROM organizations o
		LEFT JOIN (
		    SELECT org_id, COUNT(*) AS member_count
		    FROM org_members
		    GROUP BY org_id
		) c ON c.org_id = o.id
		WHERE o.id = $1`
	out := &model.Organization{}
	if err := s.pool.QueryRow(ctx, q, id).Scan(
		&out.ID, &out.Name, &out.CreatedBy, &out.MemberCount, &out.CreatedAt, &out.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("org get: %w", err)
	}
	return out, nil
}

func (s *pgOrgStore) ListForUser(ctx context.Context, userID uuid.UUID) ([]*model.OrgWithRole, error) {
	const q = `
		SELECT o.id, o.name, o.created_by,
		       COALESCE(c.member_count, 0) AS member_count,
		       o.created_at, o.updated_at,
		       m.role
		FROM organizations o
		JOIN org_members m ON m.org_id = o.id AND m.user_id = $1
		LEFT JOIN (
		    SELECT org_id, COUNT(*) AS member_count
		    FROM org_members
		    GROUP BY org_id
		) c ON c.org_id = o.id
		ORDER BY o.created_at ASC`
	rows, err := s.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("orgs list: %w", err)
	}
	defer rows.Close()

	orgs := make([]*model.OrgWithRole, 0)
	for rows.Next() {
		owr := &model.OrgWithRole{}
		if err := rows.Scan(
			&owr.ID, &owr.Name, &owr.CreatedBy, &owr.MemberCount,
			&owr.CreatedAt, &owr.UpdatedAt, &owr.Role,
		); err != nil {
			return nil, fmt.Errorf("orgs list scan: %w", err)
		}
		orgs = append(orgs, owr)
	}
	return orgs, rows.Err()
}

func (s *pgOrgStore) Update(ctx context.Context, org *model.Organization) (*model.Organization, error) {
	const q = `
		UPDATE organizations SET name=$1, updated_at=NOW()
		WHERE id=$2
		RETURNING id, name, created_by, created_at, updated_at`
	out := &model.Organization{}
	if err := s.pool.QueryRow(ctx, q, org.Name, org.ID).Scan(
		&out.ID, &out.Name, &out.CreatedBy, &out.CreatedAt, &out.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("org update: %w", err)
	}
	return out, nil
}

func (s *pgOrgStore) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM organizations WHERE id=$1`, id)
	return err
}

// ─── Membership ───────────────────────────────────────────────────────────────

// AddMember inserts a new membership. An existing member is left untouched
// (ErrConflict): upserting the role here let "add member" with the default
// viewer role demote an existing owner, possibly the only one.
func (s *pgOrgStore) AddMember(ctx context.Context, orgID, userID uuid.UUID, role model.OrgRole) (*model.OrgMember, error) {
	const q = `
		INSERT INTO org_members (org_id, user_id, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (org_id, user_id) DO NOTHING
		RETURNING org_id, user_id, role, joined_at`
	m := &model.OrgMember{}
	if err := s.pool.QueryRow(ctx, q, orgID, userID, role).Scan(
		&m.OrgID, &m.UserID, &m.Role, &m.JoinedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w: already a member", ErrConflict)
		}
		return nil, fmt.Errorf("add member: %w", err)
	}
	// Hydrate name/email from users table
	s.pool.QueryRow(ctx, `SELECT email, name FROM users WHERE id=$1`, userID).Scan(&m.Email, &m.Name) //nolint:errcheck
	return m, nil
}

func (s *pgOrgStore) GetMember(ctx context.Context, orgID, userID uuid.UUID) (*model.OrgMember, error) {
	const q = `
		SELECT m.org_id, m.user_id, u.email, u.name, m.role, m.joined_at
		FROM org_members m
		JOIN users u ON u.id = m.user_id
		WHERE m.org_id=$1 AND m.user_id=$2`
	m := &model.OrgMember{}
	if err := s.pool.QueryRow(ctx, q, orgID, userID).Scan(
		&m.OrgID, &m.UserID, &m.Email, &m.Name, &m.Role, &m.JoinedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get member: %w", err)
	}
	return m, nil
}

func (s *pgOrgStore) ListMembers(ctx context.Context, orgID uuid.UUID) ([]*model.OrgMember, error) {
	const q = `
		SELECT m.org_id, m.user_id, u.email, u.name, m.role, m.joined_at
		FROM org_members m
		JOIN users u ON u.id = m.user_id
		WHERE m.org_id=$1
		ORDER BY m.joined_at ASC`
	rows, err := s.pool.Query(ctx, q, orgID)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	defer rows.Close()

	members := make([]*model.OrgMember, 0)
	for rows.Next() {
		m := &model.OrgMember{}
		if err := rows.Scan(&m.OrgID, &m.UserID, &m.Email, &m.Name, &m.Role, &m.JoinedAt); err != nil {
			return nil, fmt.Errorf("list members scan: %w", err)
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

// lockMembership starts a transaction that holds the org row lock, which
// serializes every owner-count check with the write it guards. Without it,
// two owners stepping down at once each saw the other still in place.
// Returns the member's current role (ErrNotFound if not a member).
func (s *pgOrgStore) lockMembership(ctx context.Context, orgID, userID uuid.UUID) (pgx.Tx, model.OrgRole, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("membership — begin: %w", err)
	}
	var one int
	if err := tx.QueryRow(ctx, `SELECT 1 FROM organizations WHERE id = $1 FOR UPDATE`, orgID).Scan(&one); err != nil {
		_ = tx.Rollback(ctx)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", ErrNotFound
		}
		return nil, "", fmt.Errorf("membership — lock org: %w", err)
	}
	var role model.OrgRole
	if err := tx.QueryRow(ctx,
		`SELECT role FROM org_members WHERE org_id = $1 AND user_id = $2`, orgID, userID).Scan(&role); err != nil {
		_ = tx.Rollback(ctx)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", ErrNotFound
		}
		return nil, "", fmt.Errorf("membership — get role: %w", err)
	}
	return tx, role, nil
}

// requireAnotherOwner returns ErrLastOwner unless the org has an owner
// besides the one being demoted or removed. Call inside lockMembership.
func requireAnotherOwner(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) error {
	var owners int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM org_members WHERE org_id = $1 AND role = $2`, orgID, model.OrgRoleOwner).Scan(&owners); err != nil {
		return fmt.Errorf("membership — count owners: %w", err)
	}
	if owners <= 1 {
		return ErrLastOwner
	}
	return nil
}

func (s *pgOrgStore) UpdateMemberRole(ctx context.Context, orgID, userID uuid.UUID, role model.OrgRole) (*model.OrgMember, error) {
	tx, current, err := s.lockMembership(ctx, orgID, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if current == model.OrgRoleOwner && role != model.OrgRoleOwner {
		if err := requireAnotherOwner(ctx, tx, orgID); err != nil {
			return nil, err
		}
	}
	const q = `
		UPDATE org_members SET role=$1
		WHERE org_id=$2 AND user_id=$3
		RETURNING org_id, user_id, role, joined_at`
	m := &model.OrgMember{}
	if err := tx.QueryRow(ctx, q, role, orgID, userID).Scan(
		&m.OrgID, &m.UserID, &m.Role, &m.JoinedAt,
	); err != nil {
		return nil, fmt.Errorf("update member role: %w", err)
	}
	tx.QueryRow(ctx, `SELECT email, name FROM users WHERE id=$1`, userID).Scan(&m.Email, &m.Name) //nolint:errcheck
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("update member role — commit: %w", err)
	}
	return m, nil
}

func (s *pgOrgStore) RemoveMember(ctx context.Context, orgID, userID uuid.UUID) error {
	tx, current, err := s.lockMembership(ctx, orgID, userID)
	if errors.Is(err, ErrNotFound) {
		return nil // already gone
	}
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if current == model.OrgRoleOwner {
		if err := requireAnotherOwner(ctx, tx, orgID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM org_members WHERE org_id=$1 AND user_id=$2`, orgID, userID); err != nil {
		return fmt.Errorf("remove member: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("remove member — commit: %w", err)
	}
	return nil
}

func (s *pgOrgStore) GetUserOrgIDs(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT org_id FROM org_members WHERE user_id=$1`, userID)
	if err != nil {
		return nil, fmt.Errorf("get user orgs: %w", err)
	}
	defer rows.Close()

	ids := make([]uuid.UUID, 0)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
