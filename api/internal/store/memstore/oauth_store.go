package memstore

import (
	"context"
	"sync"
	"time"

	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
	"github.com/google/uuid"
)

// oauthRegistry holds the in-memory OAuth data, separate from the main
// Registry since OAuth stores don't need cross-checks against pages/tasks/etc.
type oauthRegistry struct {
	mu sync.RWMutex

	clients      map[uuid.UUID]*model.OAuthClient
	secretHashes map[uuid.UUID]string
	clientOrgs   map[uuid.UUID]map[uuid.UUID]bool // clientID -> orgID set

	codes  map[string]*model.OAuthAuthorizationCode // keyed by codeHash
	tokens map[uuid.UUID]*oauthTokenEntry
}

type oauthTokenEntry struct {
	token       *model.OAuthToken
	accessHash  string
	refreshHash *string
}

// NewOAuthStores creates in-memory OAuthClientStore/OAuthCodeStore/OAuthTokenStore
// sharing the same backing data. users is used to hydrate acting-user emails
// for the token audit list.
func NewOAuthStores(users store.UserStore) (store.OAuthClientStore, store.OAuthCodeStore, store.OAuthTokenStore) {
	r := &oauthRegistry{
		clients:      make(map[uuid.UUID]*model.OAuthClient),
		secretHashes: make(map[uuid.UUID]string),
		clientOrgs:   make(map[uuid.UUID]map[uuid.UUID]bool),
		codes:        make(map[string]*model.OAuthAuthorizationCode),
		tokens:       make(map[uuid.UUID]*oauthTokenEntry),
	}
	return &oauthClientStore{r: r}, &oauthCodeStore{r: r}, &oauthTokenStore{r: r, users: users}
}

func cloneOAuthClient(c *model.OAuthClient) *model.OAuthClient {
	cp := *c
	cp.GrantTypes = append([]model.OAuthGrantType(nil), c.GrantTypes...)
	cp.Scopes = append([]model.OAuthScope(nil), c.Scopes...)
	cp.RedirectURIs = append([]string(nil), c.RedirectURIs...)
	cp.OrgIDs = append([]uuid.UUID(nil), c.OrgIDs...)
	if c.RevokedAt != nil {
		t := *c.RevokedAt
		cp.RevokedAt = &t
	}
	return &cp
}

// ─── OAuthClientStore ─────────────────────────────────────────────────────────

type oauthClientStore struct{ r *oauthRegistry }

func (s *oauthClientStore) Create(_ context.Context, c *model.OAuthClient, secretHash string) (*model.OAuthClient, error) {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	now := time.Now()
	c.CreatedAt, c.UpdatedAt = now, now
	stored := cloneOAuthClient(c)
	s.r.clients[stored.ID] = stored
	s.r.secretHashes[stored.ID] = secretHash
	s.r.clientOrgs[stored.ID] = make(map[uuid.UUID]bool)
	return cloneOAuthClient(stored), nil
}

func (s *oauthClientStore) hydrate(c *model.OAuthClient) *model.OAuthClient {
	cp := cloneOAuthClient(c)
	orgIDs := make([]uuid.UUID, 0, len(s.r.clientOrgs[c.ID]))
	for orgID := range s.r.clientOrgs[c.ID] {
		orgIDs = append(orgIDs, orgID)
	}
	cp.OrgIDs = orgIDs
	return cp
}

func (s *oauthClientStore) GetByID(_ context.Context, id uuid.UUID) (*model.OAuthClient, error) {
	s.r.mu.RLock()
	defer s.r.mu.RUnlock()
	c, ok := s.r.clients[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return s.hydrate(c), nil
}

func (s *oauthClientStore) GetByClientID(_ context.Context, clientID string) (*model.OAuthClient, string, error) {
	s.r.mu.RLock()
	defer s.r.mu.RUnlock()
	for _, c := range s.r.clients {
		if c.ClientID == clientID {
			return s.hydrate(c), s.r.secretHashes[c.ID], nil
		}
	}
	return nil, "", store.ErrNotFound
}

func (s *oauthClientStore) ListForOrg(_ context.Context, orgID uuid.UUID) ([]*model.OAuthClient, error) {
	s.r.mu.RLock()
	defer s.r.mu.RUnlock()
	out := make([]*model.OAuthClient, 0)
	for _, c := range s.r.clients {
		if s.r.clientOrgs[c.ID][orgID] {
			out = append(out, s.hydrate(c))
		}
	}
	return out, nil
}

func (s *oauthClientStore) Update(_ context.Context, id uuid.UUID, name *string, description *string, scopes []model.OAuthScope, redirectURIs []string) (*model.OAuthClient, error) {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	c, ok := s.r.clients[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	if name != nil {
		c.Name = *name
	}
	if description != nil {
		c.Description = description
	}
	if scopes != nil {
		c.Scopes = scopes
	}
	if redirectURIs != nil {
		c.RedirectURIs = redirectURIs
	}
	c.UpdatedAt = time.Now()
	return s.hydrate(c), nil
}

func (s *oauthClientStore) AddOrg(_ context.Context, clientID, orgID, _ uuid.UUID) error {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	if _, ok := s.r.clients[clientID]; !ok {
		return store.ErrNotFound
	}
	s.r.clientOrgs[clientID][orgID] = true
	return nil
}

func (s *oauthClientStore) RemoveOrg(_ context.Context, clientID, orgID uuid.UUID) error {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	delete(s.r.clientOrgs[clientID], orgID)
	return nil
}

func (s *oauthClientStore) HasOrg(_ context.Context, clientID, orgID uuid.UUID) (bool, error) {
	s.r.mu.RLock()
	defer s.r.mu.RUnlock()
	return s.r.clientOrgs[clientID][orgID], nil
}

func (s *oauthClientStore) RotateSecret(_ context.Context, id uuid.UUID, newSecretHash string) (*model.OAuthClient, error) {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	c, ok := s.r.clients[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	s.r.secretHashes[id] = newSecretHash
	c.UpdatedAt = time.Now()
	return s.hydrate(c), nil
}

func (s *oauthClientStore) Revoke(_ context.Context, id uuid.UUID) error {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	c, ok := s.r.clients[id]
	if !ok {
		return store.ErrNotFound
	}
	now := time.Now()
	c.RevokedAt = &now
	c.UpdatedAt = now
	return nil
}

// ─── OAuthCodeStore ───────────────────────────────────────────────────────────

type oauthCodeStore struct{ r *oauthRegistry }

func (s *oauthCodeStore) Create(_ context.Context, code *model.OAuthAuthorizationCode, codeHash string) error {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	if code.ID == uuid.Nil {
		code.ID = uuid.New()
	}
	code.CreatedAt = time.Now()
	cp := *code
	s.r.codes[codeHash] = &cp
	return nil
}

func (s *oauthCodeStore) ConsumeByHash(_ context.Context, codeHash string) (*model.OAuthAuthorizationCode, error) {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	code, ok := s.r.codes[codeHash]
	if !ok || code.UsedAt != nil || time.Now().After(code.ExpiresAt) {
		return nil, store.ErrConflict
	}
	now := time.Now()
	code.UsedAt = &now
	cp := *code
	return &cp, nil
}

// ─── OAuthTokenStore ──────────────────────────────────────────────────────────

type oauthTokenStore struct {
	r     *oauthRegistry
	users store.UserStore
}

func (s *oauthTokenStore) Create(_ context.Context, t *model.OAuthToken, accessHash string, refreshHash *string) error {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	t.CreatedAt = time.Now()
	cp := *t
	s.r.tokens[cp.ID] = &oauthTokenEntry{token: &cp, accessHash: accessHash, refreshHash: refreshHash}
	return nil
}

func (s *oauthTokenStore) GetByAccessHash(_ context.Context, hash string) (*model.OAuthToken, error) {
	s.r.mu.RLock()
	defer s.r.mu.RUnlock()
	for _, e := range s.r.tokens {
		if e.accessHash == hash {
			cp := *e.token
			return &cp, nil
		}
	}
	return nil, store.ErrNotFound
}

func (s *oauthTokenStore) RotateRefresh(_ context.Context, oldRefreshHash, newAccessHash, newRefreshHash string, accessExp time.Time, refreshExp time.Time) (*model.OAuthToken, error) {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	for _, e := range s.r.tokens {
		if e.refreshHash != nil && *e.refreshHash == oldRefreshHash && e.token.RevokedAt == nil {
			e.accessHash = newAccessHash
			e.refreshHash = &newRefreshHash
			e.token.AccessTokenExpiresAt = accessExp
			e.token.RefreshTokenExpiresAt = &refreshExp
			now := time.Now()
			e.token.LastUsedAt = &now
			cp := *e.token
			return &cp, nil
		}
	}
	return nil, store.ErrConflict
}

func (s *oauthTokenStore) TouchLastUsed(_ context.Context, id uuid.UUID) error {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	if e, ok := s.r.tokens[id]; ok {
		now := time.Now()
		e.token.LastUsedAt = &now
	}
	return nil
}

func (s *oauthTokenStore) Revoke(_ context.Context, id uuid.UUID) error {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	e, ok := s.r.tokens[id]
	if !ok {
		return store.ErrNotFound
	}
	now := time.Now()
	e.token.RevokedAt = &now
	return nil
}

func (s *oauthTokenStore) RevokeAllForClient(_ context.Context, clientID uuid.UUID) error {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	now := time.Now()
	for _, e := range s.r.tokens {
		if e.token.ClientID == clientID && e.token.RevokedAt == nil {
			e.token.RevokedAt = &now
		}
	}
	return nil
}

func (s *oauthTokenStore) ListActiveForClient(ctx context.Context, clientID uuid.UUID) ([]*model.OAuthToken, error) {
	s.r.mu.RLock()
	defer s.r.mu.RUnlock()
	out := make([]*model.OAuthToken, 0)
	for _, e := range s.r.tokens {
		if e.token.ClientID == clientID && e.token.RevokedAt == nil {
			cp := *e.token
			if s.users != nil {
				if u, err := s.users.GetByID(ctx, cp.ActingUserID); err == nil {
					cp.ActingUserEmail = u.Email
				}
			}
			out = append(out, &cp)
		}
	}
	return out, nil
}
