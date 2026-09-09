package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/glyph/api/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ─── OAuth Clients ────────────────────────────────────────────────────────────

type pgOAuthClientStore struct{ pool DBPool }

func NewOAuthClientStore(pool DBPool) OAuthClientStore {
	return &pgOAuthClientStore{pool: pool}
}

func scopesToStrings(scopes []model.OAuthScope) []string {
	out := make([]string, len(scopes))
	for i, s := range scopes {
		out[i] = string(s)
	}
	return out
}

func scopesFromStrings(strs []string) []model.OAuthScope {
	out := make([]model.OAuthScope, len(strs))
	for i, s := range strs {
		out[i] = model.OAuthScope(s)
	}
	return out
}

func grantTypesToStrings(gs []model.OAuthGrantType) []string {
	out := make([]string, len(gs))
	for i, g := range gs {
		out[i] = string(g)
	}
	return out
}

func grantTypesFromStrings(strs []string) []model.OAuthGrantType {
	out := make([]model.OAuthGrantType, len(strs))
	for i, s := range strs {
		out[i] = model.OAuthGrantType(s)
	}
	return out
}

func (s *pgOAuthClientStore) Create(ctx context.Context, c *model.OAuthClient, secretHash string) (*model.OAuthClient, error) {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	const q = `
		INSERT INTO oauth_clients (id, client_id, client_secret_hash, name, description, created_by_id, grant_types, scopes, redirect_uris, is_confidential)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, client_id, name, description, created_by_id, grant_types, scopes, redirect_uris, is_confidential, revoked_at, created_at, updated_at`

	redirectURIsIn := c.RedirectURIs
	if redirectURIsIn == nil {
		// redirect_uris is NOT NULL; a nil Go slice would otherwise be sent
		// as SQL NULL rather than the column's '{}' default, which only
		// applies when the column is omitted from the INSERT entirely.
		redirectURIsIn = []string{}
	}

	var grantTypes, scopes, redirectURIs []string
	out := &model.OAuthClient{}
	if err := s.pool.QueryRow(ctx, q,
		c.ID, c.ClientID, secretHash, c.Name, c.Description, c.CreatedByID,
		grantTypesToStrings(c.GrantTypes), scopesToStrings(c.Scopes), redirectURIsIn, c.IsConfidential,
	).Scan(
		&out.ID, &out.ClientID, &out.Name, &out.Description, &out.CreatedByID,
		&grantTypes, &scopes, &redirectURIs, &out.IsConfidential, &out.RevokedAt, &out.CreatedAt, &out.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("oauth client create: %w", err)
	}
	out.GrantTypes = grantTypesFromStrings(grantTypes)
	out.Scopes = scopesFromStrings(scopes)
	out.RedirectURIs = redirectURIs
	return out, nil
}

func (s *pgOAuthClientStore) scanClient(row pgx.Row) (*model.OAuthClient, error) {
	var grantTypes, scopes, redirectURIs, orgIDStrs []string
	out := &model.OAuthClient{}
	if err := row.Scan(
		&out.ID, &out.ClientID, &out.Name, &out.Description, &out.CreatedByID,
		&grantTypes, &scopes, &redirectURIs, &out.IsConfidential, &out.RevokedAt, &out.CreatedAt, &out.UpdatedAt,
		&orgIDStrs,
	); err != nil {
		return nil, err
	}
	out.GrantTypes = grantTypesFromStrings(grantTypes)
	out.Scopes = scopesFromStrings(scopes)
	out.RedirectURIs = redirectURIs
	orgIDs := make([]uuid.UUID, 0, len(orgIDStrs))
	for _, s := range orgIDStrs {
		id, err := uuid.Parse(s)
		if err == nil {
			orgIDs = append(orgIDs, id)
		}
	}
	out.OrgIDs = orgIDs
	return out, nil
}

const oauthClientWithOrgsQuery = `
	SELECT c.id, c.client_id, c.name, c.description, c.created_by_id,
	       c.grant_types, c.scopes, c.redirect_uris, c.is_confidential, c.revoked_at, c.created_at, c.updated_at,
	       COALESCE(ARRAY_AGG(co.org_id::text) FILTER (WHERE co.org_id IS NOT NULL), '{}')
	FROM oauth_clients c
	LEFT JOIN oauth_client_orgs co ON co.client_id = c.id`

func (s *pgOAuthClientStore) GetByID(ctx context.Context, id uuid.UUID) (*model.OAuthClient, error) {
	q := oauthClientWithOrgsQuery + ` WHERE c.id = $1 GROUP BY c.id`
	out, err := s.scanClient(s.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("oauth client get: %w", err)
	}
	return out, nil
}

func (s *pgOAuthClientStore) GetByClientID(ctx context.Context, clientID string) (*model.OAuthClient, string, error) {
	q := oauthClientWithOrgsQuery + ` WHERE c.client_id = $1 GROUP BY c.id`
	const secretQ = `SELECT client_secret_hash FROM oauth_clients WHERE client_id = $1`
	var secretHash string
	if err := s.pool.QueryRow(ctx, secretQ, clientID).Scan(&secretHash); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", ErrNotFound
		}
		return nil, "", fmt.Errorf("oauth client get by client_id: %w", err)
	}
	out, err := s.scanClient(s.pool.QueryRow(ctx, q, clientID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", ErrNotFound
		}
		return nil, "", fmt.Errorf("oauth client get by client_id: %w", err)
	}
	return out, secretHash, nil
}

func (s *pgOAuthClientStore) ListForOrg(ctx context.Context, orgID uuid.UUID) ([]*model.OAuthClient, error) {
	const q = `
		SELECT c.id, c.client_id, c.name, c.description, c.created_by_id,
		       c.grant_types, c.scopes, c.redirect_uris, c.is_confidential, c.revoked_at, c.created_at, c.updated_at,
		       COALESCE(ARRAY_AGG(DISTINCT co2.org_id::text) FILTER (WHERE co2.org_id IS NOT NULL), '{}')
		FROM oauth_clients c
		JOIN oauth_client_orgs co ON co.client_id = c.id AND co.org_id = $1
		LEFT JOIN oauth_client_orgs co2 ON co2.client_id = c.id
		GROUP BY c.id
		ORDER BY c.created_at ASC`
	rows, err := s.pool.Query(ctx, q, orgID)
	if err != nil {
		return nil, fmt.Errorf("oauth clients list for org: %w", err)
	}
	defer rows.Close()

	clients := make([]*model.OAuthClient, 0)
	for rows.Next() {
		c, err := s.scanClient(rows)
		if err != nil {
			return nil, fmt.Errorf("oauth clients list scan: %w", err)
		}
		clients = append(clients, c)
	}
	return clients, rows.Err()
}

func (s *pgOAuthClientStore) Update(ctx context.Context, id uuid.UUID, name *string, description *string, scopes []model.OAuthScope, redirectURIs []string) (*model.OAuthClient, error) {
	existing, err := s.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if name != nil {
		existing.Name = *name
	}
	if description != nil {
		existing.Description = description
	}
	if scopes != nil {
		existing.Scopes = scopes
	}
	if redirectURIs != nil {
		existing.RedirectURIs = redirectURIs
	}
	const q = `
		UPDATE oauth_clients SET name=$1, description=$2, scopes=$3, redirect_uris=$4, updated_at=NOW()
		WHERE id=$5`
	if _, err := s.pool.Exec(ctx, q, existing.Name, existing.Description, scopesToStrings(existing.Scopes), existing.RedirectURIs, id); err != nil {
		return nil, fmt.Errorf("oauth client update: %w", err)
	}
	return s.GetByID(ctx, id)
}

func (s *pgOAuthClientStore) AddOrg(ctx context.Context, clientID, orgID, addedByID uuid.UUID) error {
	const q = `
		INSERT INTO oauth_client_orgs (client_id, org_id, added_by_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (client_id, org_id) DO NOTHING`
	if _, err := s.pool.Exec(ctx, q, clientID, orgID, addedByID); err != nil {
		return fmt.Errorf("oauth client add org: %w", err)
	}
	return nil
}

func (s *pgOAuthClientStore) RemoveOrg(ctx context.Context, clientID, orgID uuid.UUID) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM oauth_client_orgs WHERE client_id=$1 AND org_id=$2`, clientID, orgID); err != nil {
		return fmt.Errorf("oauth client remove org: %w", err)
	}
	return nil
}

func (s *pgOAuthClientStore) HasOrg(ctx context.Context, clientID, orgID uuid.UUID) (bool, error) {
	var exists bool
	const q = `SELECT EXISTS(SELECT 1 FROM oauth_client_orgs WHERE client_id=$1 AND org_id=$2)`
	if err := s.pool.QueryRow(ctx, q, clientID, orgID).Scan(&exists); err != nil {
		return false, fmt.Errorf("oauth client has org: %w", err)
	}
	return exists, nil
}

func (s *pgOAuthClientStore) RotateSecret(ctx context.Context, id uuid.UUID, newSecretHash string) (*model.OAuthClient, error) {
	const q = `UPDATE oauth_clients SET client_secret_hash=$1, updated_at=NOW() WHERE id=$2`
	if _, err := s.pool.Exec(ctx, q, newSecretHash, id); err != nil {
		return nil, fmt.Errorf("oauth client rotate secret: %w", err)
	}
	return s.GetByID(ctx, id)
}

func (s *pgOAuthClientStore) Revoke(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE oauth_clients SET revoked_at=NOW(), updated_at=NOW() WHERE id=$1`
	if _, err := s.pool.Exec(ctx, q, id); err != nil {
		return fmt.Errorf("oauth client revoke: %w", err)
	}
	return nil
}

// ─── OAuth Authorization Codes ────────────────────────────────────────────────

type pgOAuthCodeStore struct{ pool DBPool }

func NewOAuthCodeStore(pool DBPool) OAuthCodeStore {
	return &pgOAuthCodeStore{pool: pool}
}

func (s *pgOAuthCodeStore) Create(ctx context.Context, code *model.OAuthAuthorizationCode, codeHash string) error {
	if code.ID == uuid.Nil {
		code.ID = uuid.New()
	}
	const q = `
		INSERT INTO oauth_authorization_codes
			(id, code_hash, client_id, user_id, redirect_uri, scopes, org_ids, code_challenge, code_challenge_method, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	orgIDs := code.OrgIDs
	if orgIDs == nil {
		orgIDs = []uuid.UUID{}
	}
	_, err := s.pool.Exec(ctx, q,
		code.ID, codeHash, code.ClientID, code.UserID, code.RedirectURI,
		scopesToStrings(code.Scopes), orgIDs, code.CodeChallenge, code.CodeChallengeMethod, code.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("oauth code create: %w", err)
	}
	return nil
}

func (s *pgOAuthCodeStore) ConsumeByHash(ctx context.Context, codeHash string) (*model.OAuthAuthorizationCode, error) {
	const q = `
		UPDATE oauth_authorization_codes
		SET used_at = NOW()
		WHERE code_hash = $1 AND used_at IS NULL AND expires_at > NOW()
		RETURNING id, client_id, user_id, redirect_uri, scopes, org_ids, code_challenge, code_challenge_method, expires_at, used_at, created_at`
	var scopes []string
	out := &model.OAuthAuthorizationCode{}
	err := s.pool.QueryRow(ctx, q, codeHash).Scan(
		&out.ID, &out.ClientID, &out.UserID, &out.RedirectURI, &scopes, &out.OrgIDs,
		&out.CodeChallenge, &out.CodeChallengeMethod, &out.ExpiresAt, &out.UsedAt, &out.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrConflict
		}
		return nil, fmt.Errorf("oauth code consume: %w", err)
	}
	out.Scopes = scopesFromStrings(scopes)
	return out, nil
}

// ─── OAuth Tokens ─────────────────────────────────────────────────────────────

type pgOAuthTokenStore struct{ pool DBPool }

func NewOAuthTokenStore(pool DBPool) OAuthTokenStore {
	return &pgOAuthTokenStore{pool: pool}
}

func (s *pgOAuthTokenStore) Create(ctx context.Context, t *model.OAuthToken, accessHash string, refreshHash *string) error {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	const q = `
		INSERT INTO oauth_tokens
			(id, access_token_hash, refresh_token_hash, client_id, acting_user_id, grant_type, scopes, org_ids, access_token_expires_at, refresh_token_expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	orgIDs := t.OrgIDs
	if orgIDs == nil {
		orgIDs = []uuid.UUID{}
	}
	_, err := s.pool.Exec(ctx, q,
		t.ID, accessHash, refreshHash, t.ClientID, t.ActingUserID, string(t.GrantType),
		scopesToStrings(t.Scopes), orgIDs, t.AccessTokenExpiresAt, t.RefreshTokenExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("oauth token create: %w", err)
	}
	return nil
}

func (s *pgOAuthTokenStore) scanToken(row pgx.Row) (*model.OAuthToken, error) {
	var grantType string
	var scopes []string
	out := &model.OAuthToken{}
	if err := row.Scan(
		&out.ID, &out.ClientID, &out.ActingUserID, &grantType, &scopes, &out.OrgIDs,
		&out.AccessTokenExpiresAt, &out.RefreshTokenExpiresAt, &out.RevokedAt, &out.LastUsedAt, &out.CreatedAt,
	); err != nil {
		return nil, err
	}
	out.GrantType = model.OAuthGrantType(grantType)
	out.Scopes = scopesFromStrings(scopes)
	return out, nil
}

const oauthTokenSelect = `
	SELECT id, client_id, acting_user_id, grant_type, scopes, org_ids,
	       access_token_expires_at, refresh_token_expires_at, revoked_at, last_used_at, created_at
	FROM oauth_tokens`

func (s *pgOAuthTokenStore) GetByAccessHash(ctx context.Context, hash string) (*model.OAuthToken, error) {
	q := oauthTokenSelect + ` WHERE access_token_hash = $1`
	out, err := s.scanToken(s.pool.QueryRow(ctx, q, hash))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("oauth token get by access hash: %w", err)
	}
	return out, nil
}

func (s *pgOAuthTokenStore) RotateRefresh(ctx context.Context, clientID uuid.UUID, oldRefreshHash, newAccessHash, newRefreshHash string, accessExp time.Time, refreshExp time.Time) (*model.OAuthToken, error) {
	const q = `
		UPDATE oauth_tokens
		SET access_token_hash = $1, refresh_token_hash = $2,
		    access_token_expires_at = $3, refresh_token_expires_at = $4,
		    last_used_at = NOW()
		WHERE refresh_token_hash = $5 AND client_id = $6
		  AND revoked_at IS NULL AND refresh_token_expires_at > NOW()
		RETURNING ` + `id, client_id, acting_user_id, grant_type, scopes, org_ids, access_token_expires_at, refresh_token_expires_at, revoked_at, last_used_at, created_at`
	out, err := s.scanToken(s.pool.QueryRow(ctx, q, newAccessHash, newRefreshHash, accessExp, refreshExp, oldRefreshHash, clientID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrConflict
		}
		return nil, fmt.Errorf("oauth token rotate refresh: %w", err)
	}
	return out, nil
}

func (s *pgOAuthTokenStore) TouchLastUsed(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE oauth_tokens SET last_used_at = NOW() WHERE id = $1`, id)
	return err
}

func (s *pgOAuthTokenStore) Revoke(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE oauth_tokens SET revoked_at = NOW() WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("oauth token revoke: %w", err)
	}
	return nil
}

func (s *pgOAuthTokenStore) RevokeAllForClient(ctx context.Context, clientID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE oauth_tokens SET revoked_at = NOW() WHERE client_id = $1 AND revoked_at IS NULL`, clientID)
	if err != nil {
		return fmt.Errorf("oauth token revoke all for client: %w", err)
	}
	return nil
}

func (s *pgOAuthTokenStore) ListActiveForClient(ctx context.Context, clientID uuid.UUID) ([]*model.OAuthToken, error) {
	const q = `
		SELECT t.id, t.client_id, t.acting_user_id, t.grant_type, t.scopes, t.org_ids,
		       t.access_token_expires_at, t.refresh_token_expires_at, t.revoked_at, t.last_used_at, t.created_at,
		       u.email
		FROM oauth_tokens t
		JOIN users u ON u.id = t.acting_user_id
		WHERE t.client_id = $1 AND t.revoked_at IS NULL
		ORDER BY t.created_at DESC`
	rows, err := s.pool.Query(ctx, q, clientID)
	if err != nil {
		return nil, fmt.Errorf("oauth tokens list active for client: %w", err)
	}
	defer rows.Close()

	tokens := make([]*model.OAuthToken, 0)
	for rows.Next() {
		var grantType string
		var scopes []string
		t := &model.OAuthToken{}
		if err := rows.Scan(
			&t.ID, &t.ClientID, &t.ActingUserID, &grantType, &scopes, &t.OrgIDs,
			&t.AccessTokenExpiresAt, &t.RefreshTokenExpiresAt, &t.RevokedAt, &t.LastUsedAt, &t.CreatedAt,
			&t.ActingUserEmail,
		); err != nil {
			return nil, fmt.Errorf("oauth tokens list active scan: %w", err)
		}
		t.GrantType = model.OAuthGrantType(grantType)
		t.Scopes = scopesFromStrings(scopes)
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}
