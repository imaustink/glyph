package model

import (
	"time"

	"github.com/google/uuid"
)

// ─── OAuth grant types & scopes ────────────────────────────────────────────────

type OAuthGrantType string

const (
	GrantClientCredentials OAuthGrantType = "client_credentials"
	GrantAuthorizationCode OAuthGrantType = "authorization_code"
)

// IsValid returns true if g is a known grant type.
func (g OAuthGrantType) IsValid() bool {
	switch g {
	case GrantClientCredentials, GrantAuthorizationCode:
		return true
	}
	return false
}

// OAuthScope enumerates the resource:permission pairs a client/token may carry.
type OAuthScope string

const (
	ScopePageRead      OAuthScope = "page:read"
	ScopePageWrite     OAuthScope = "page:write"
	ScopeTaskRead      OAuthScope = "task:read"
	ScopeTaskWrite     OAuthScope = "task:write"
	ScopeTemplateRead  OAuthScope = "template:read"
	ScopeTemplateWrite OAuthScope = "template:write"
	ScopeOrgRead       OAuthScope = "org:read"
)

// IsValid returns true if s is a known OAuth scope value.
func (s OAuthScope) IsValid() bool {
	switch s {
	case ScopePageRead, ScopePageWrite, ScopeTaskRead, ScopeTaskWrite,
		ScopeTemplateRead, ScopeTemplateWrite, ScopeOrgRead:
		return true
	}
	return false
}

// ScopeResourceType maps an OAuth scope to the ShareResourceType it governs,
// used to check a scope against a resource's type in permission checks.
// Returns "" and false for scopes with no corresponding resource type (e.g. org:read).
func (s OAuthScope) ResourceType() (ShareResourceType, bool) {
	switch s {
	case ScopePageRead, ScopePageWrite:
		return ShareResourcePage, true
	case ScopeTaskRead, ScopeTaskWrite:
		return ShareResourceTask, true
	case ScopeTemplateRead, ScopeTemplateWrite:
		return ShareResourceTemplate, true
	}
	return "", false
}

// IsWrite returns true if the scope grants write access.
func (s OAuthScope) IsWrite() bool {
	switch s {
	case ScopePageWrite, ScopeTaskWrite, ScopeTemplateWrite:
		return true
	}
	return false
}

// ─── OAuth Clients ──────────────────────────────────────────────────────────────

type OAuthClient struct {
	ID             uuid.UUID        `json:"id"`
	ClientID       string           `json:"clientId"`
	Name           string           `json:"name"`
	Description    *string          `json:"description,omitempty"`
	CreatedByID    uuid.UUID        `json:"createdById"`
	GrantTypes     []OAuthGrantType `json:"grantTypes"`
	Scopes         []OAuthScope     `json:"scopes"`
	RedirectURIs   []string         `json:"redirectUris"`
	IsConfidential bool             `json:"isConfidential"`
	OrgIDs         []uuid.UUID      `json:"orgIds,omitempty"`
	RevokedAt      *time.Time       `json:"revokedAt,omitempty"`
	CreatedAt      time.Time        `json:"createdAt"`
	UpdatedAt      time.Time        `json:"updatedAt"`
}

// OAuthClientWithSecret is returned exactly once (create/rotate) — the only
// response shape that ever includes the plaintext secret.
type OAuthClientWithSecret struct {
	OAuthClient
	ClientSecret string `json:"clientSecret"`
}

// ─── OAuth Authorization Codes ───────────────────────────────────────────────────

type OAuthAuthorizationCode struct {
	ID                  uuid.UUID    `json:"id"`
	ClientID            uuid.UUID    `json:"clientId"`
	UserID              uuid.UUID    `json:"userId"`
	RedirectURI         string       `json:"redirectUri"`
	Scopes              []OAuthScope `json:"scopes"`
	OrgIDs              []uuid.UUID  `json:"orgIds"`
	CodeChallenge       string       `json:"codeChallenge"`
	CodeChallengeMethod string       `json:"codeChallengeMethod"`
	ExpiresAt           time.Time    `json:"expiresAt"`
	UsedAt              *time.Time   `json:"usedAt,omitempty"`
	CreatedAt           time.Time    `json:"createdAt"`
}

// ─── OAuth Tokens ─────────────────────────────────────────────────────────────

type OAuthToken struct {
	ID                    uuid.UUID      `json:"id"`
	ClientID              uuid.UUID      `json:"clientId"`
	ActingUserID          uuid.UUID      `json:"actingUserId"`
	ActingUserEmail       *string        `json:"actingUserEmail,omitempty"`
	GrantType             OAuthGrantType `json:"grantType"`
	Scopes                []OAuthScope   `json:"scopes"`
	OrgIDs                []uuid.UUID    `json:"orgIds"`
	AccessTokenExpiresAt  time.Time      `json:"accessTokenExpiresAt"`
	RefreshTokenExpiresAt *time.Time     `json:"refreshTokenExpiresAt,omitempty"`
	RevokedAt             *time.Time     `json:"revokedAt,omitempty"`
	LastUsedAt            *time.Time     `json:"lastUsedAt,omitempty"`
	CreatedAt             time.Time      `json:"createdAt"`
}

// TokenScope is the subset of an OAuthToken carried on the request context
// for use by PermissionChecker/access checks. Present only when the request
// authenticated via an OAuth bearer token; nil for cookie-session requests
// (meaning "unrestricted" — i.e. governed only by the user's own permissions).
//
// Defined in package model (rather than package oauth) so that both the
// oauth package (which sets it) and the handler package (which reads it via
// PermissionChecker) can depend on it without an import cycle.
type TokenScope struct {
	ClientID uuid.UUID
	Scopes   []OAuthScope
	OrgIDs   []uuid.UUID
}

// HasScope returns true if the token was granted the given scope.
func (ts *TokenScope) HasScope(s OAuthScope) bool {
	for _, g := range ts.Scopes {
		if g == s {
			return true
		}
	}
	return false
}

// HasOrg returns true if orgID is within the token's granted org scope.
func (ts *TokenScope) HasOrg(orgID uuid.UUID) bool {
	for _, id := range ts.OrgIDs {
		if id == orgID {
			return true
		}
	}
	return false
}

// TokenScopeContextKey is the Gin context key under which a *TokenScope is
// stored for bearer-token-authenticated requests.
const TokenScopeContextKey = "oauth_scope"
