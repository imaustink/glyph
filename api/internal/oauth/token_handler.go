package oauth

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
	"github.com/google/uuid"
)

const (
	accessTokenTTL  = 1 * time.Hour
	refreshTokenTTL = 30 * 24 * time.Hour
	authCodeTTL     = 60 * time.Second
	consentTokenTTL = 5 * time.Minute
)

// Config holds the stores the OAuth protocol handlers depend on.
type Config struct {
	Clients       store.OAuthClientStore
	Codes         store.OAuthCodeStore
	Tokens        store.OAuthTokenStore
	Users         store.UserStore
	Orgs          store.OrgStore
	ConsentSecret []byte
}

// tokenError writes an RFC 6749 §5.2 JSON error response.
func tokenError(c *gin.Context, status int, code, description string) {
	c.JSON(status, gin.H{"error": code, "error_description": description})
}

// authenticateClient parses client credentials from HTTP Basic auth or form
// fields, and validates the secret (skipped entirely for public clients on
// the authorization_code grant, where PKCE substitutes for a client secret).
func authenticateClient(c *gin.Context, cfg Config, requireSecret bool) (*model.OAuthClient, bool) {
	clientID, clientSecret, hasBasic := c.Request.BasicAuth()
	if !hasBasic {
		clientID = c.PostForm("client_id")
		clientSecret = c.PostForm("client_secret")
	}
	if clientID == "" {
		tokenError(c, http.StatusBadRequest, "invalid_request", "client_id is required")
		return nil, false
	}
	client, secretHash, err := cfg.Clients.GetByClientID(c.Request.Context(), clientID)
	if err != nil {
		tokenError(c, http.StatusUnauthorized, "invalid_client", "unknown client")
		return nil, false
	}
	if client.RevokedAt != nil {
		tokenError(c, http.StatusUnauthorized, "invalid_client", "client has been revoked")
		return nil, false
	}
	if client.IsConfidential || requireSecret {
		if clientSecret == "" || !secureCompareHash(clientSecret, secretHash) {
			tokenError(c, http.StatusUnauthorized, "invalid_client", "invalid client credentials")
			return nil, false
		}
	}
	return client, true
}

// tryAuthenticateClient resolves client credentials from the request exactly
// like authenticateClient, but never writes a response — it only reports
// success or failure. Use this on a route (like /oauth/revoke) whose
// contract requires a uniform response regardless of what went wrong, where
// authenticateClient's error responses on a merely-absent or invalid
// credential would leak information or break that contract.
func tryAuthenticateClient(c *gin.Context, cfg Config, requireSecret bool) (*model.OAuthClient, bool) {
	clientID, clientSecret, hasBasic := c.Request.BasicAuth()
	if !hasBasic {
		clientID = c.PostForm("client_id")
		clientSecret = c.PostForm("client_secret")
	}
	if clientID == "" {
		return nil, false
	}
	client, secretHash, err := cfg.Clients.GetByClientID(c.Request.Context(), clientID)
	if err != nil || client.RevokedAt != nil {
		return nil, false
	}
	if client.IsConfidential || requireSecret {
		if clientSecret == "" || !secureCompareHash(clientSecret, secretHash) {
			return nil, false
		}
	}
	return client, true
}

func hasGrantType(client *model.OAuthClient, g model.OAuthGrantType) bool {
	for _, gt := range client.GrantTypes {
		if gt == g {
			return true
		}
	}
	return false
}

// intersectScopes returns the subset of requested that is also in allowed.
// If requested is empty, allowed is returned unchanged (default: full grant
// of everything the client itself is configured with — the same default
// RFC 6749 §3.3 leaves to server policy when a client omits `scope`
// entirely). This is a deliberate, documented policy choice, not an
// oversight: every existing caller (this codebase's own tests included)
// that omits `scope` today expects the full grant, so silently narrowing
// the default here would be a breaking API change, not a pure bug fix.
// Anything requesting less than full access should pass `scope` explicitly.
func intersectScopes(requested []model.OAuthScope, allowed []model.OAuthScope) []model.OAuthScope {
	if len(requested) == 0 {
		return allowed
	}
	allowedSet := make(map[model.OAuthScope]bool, len(allowed))
	for _, s := range allowed {
		allowedSet[s] = true
	}
	out := make([]model.OAuthScope, 0, len(requested))
	for _, s := range requested {
		if allowedSet[s] {
			out = append(out, s)
		}
	}
	return out
}

func parseScopeParam(raw string) []model.OAuthScope {
	if raw == "" {
		return nil
	}
	parts := strings.Fields(raw)
	out := make([]model.OAuthScope, 0, len(parts))
	for _, p := range parts {
		out = append(out, model.OAuthScope(p))
	}
	return out
}

func scopesToParam(scopes []model.OAuthScope) string {
	parts := make([]string, len(scopes))
	for i, s := range scopes {
		parts[i] = string(s)
	}
	return strings.Join(parts, " ")
}

// TokenHandler dispatches POST /oauth/token on grant_type.
func TokenHandler(cfg Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		grantType := c.PostForm("grant_type")
		switch model.OAuthGrantType(grantType) {
		case model.GrantClientCredentials:
			handleClientCredentials(c, cfg)
		case model.GrantAuthorizationCode:
			handleAuthorizationCode(c, cfg)
		case "refresh_token":
			handleRefreshToken(c, cfg)
		default:
			tokenError(c, http.StatusBadRequest, "unsupported_grant_type", "grant_type must be client_credentials, authorization_code, or refresh_token")
		}
	}
}

// ─── client_credentials (+ subject) ────────────────────────────────────────────

// handleClientCredentials implements the client_credentials+subject
// (token exchange) grant: given only a valid client secret, a client can
// mint a token acting as ANY member of ANY org it is scoped to — the
// subject is never notified and never separately consents. This is by
// design (it is what lets an already-trusted agent act on behalf of a whole
// org's membership without an interactive OAuth dance per user), but it
// means the client secret is equivalent in value to that org's data: treat
// creating a client_credentials-capable client with the same care as
// granting the whole org's access to whoever holds the secret, and prefer
// OAuthClientHandler.RotateSecret's ?revokeExisting=true (not just a plain
// rotation) if the secret may have leaked.
func handleClientCredentials(c *gin.Context, cfg Config) {
	client, ok := authenticateClient(c, cfg, true)
	if !ok {
		return
	}
	if !hasGrantType(client, model.GrantClientCredentials) {
		tokenError(c, http.StatusBadRequest, "unauthorized_client", "client is not authorized for client_credentials")
		return
	}

	subject := c.PostForm("subject")
	if subject == "" {
		tokenError(c, http.StatusBadRequest, "invalid_request", "subject is required")
		return
	}
	orgIDStr := c.PostForm("org_id")
	if orgIDStr == "" {
		tokenError(c, http.StatusBadRequest, "invalid_request", "org_id is required")
		return
	}
	orgID, err := uuid.Parse(orgIDStr)
	if err != nil {
		tokenError(c, http.StatusBadRequest, "invalid_request", "invalid org_id")
		return
	}

	// Resolve subject: try UUID first, fall back to email lookup.
	var subjectUser *model.User
	if uid, err := uuid.Parse(subject); err == nil {
		subjectUser, err = cfg.Users.GetByID(c.Request.Context(), uid)
		if err != nil {
			subjectUser = nil
		}
	}
	if subjectUser == nil {
		subjectUser, err = cfg.Users.GetByEmail(c.Request.Context(), subject)
		if err != nil {
			tokenError(c, http.StatusBadRequest, "invalid_grant", "subject user not found")
			return
		}
	}

	hasOrg, err := cfg.Clients.HasOrg(c.Request.Context(), client.ID, orgID)
	if err != nil || !hasOrg {
		tokenError(c, http.StatusForbidden, "invalid_grant", "client is not scoped to org")
		return
	}
	if _, err := cfg.Orgs.GetMember(c.Request.Context(), orgID, subjectUser.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			tokenError(c, http.StatusForbidden, "invalid_grant", "subject is not a member of org")
			return
		}
		tokenError(c, http.StatusInternalServerError, "server_error", "failed to verify org membership")
		return
	}

	scopes := intersectScopes(parseScopeParam(c.PostForm("scope")), client.Scopes)

	accessToken, accessHash, err := GenerateAccessToken()
	if err != nil {
		tokenError(c, http.StatusInternalServerError, "server_error", "failed to generate token")
		return
	}
	tok := &model.OAuthToken{
		ClientID:             client.ID,
		ActingUserID:         subjectUser.ID,
		GrantType:            model.GrantClientCredentials,
		Scopes:               scopes,
		OrgIDs:               []uuid.UUID{orgID},
		AccessTokenExpiresAt: time.Now().Add(accessTokenTTL),
	}
	if err := cfg.Tokens.Create(c.Request.Context(), tok, accessHash, nil); err != nil {
		tokenError(c, http.StatusInternalServerError, "server_error", "failed to persist token")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"access_token":   accessToken,
		"token_type":     "Bearer",
		"expires_in":     int(accessTokenTTL.Seconds()),
		"scope":          scopesToParam(scopes),
		"acting_user_id": subjectUser.ID,
		"org_id":         orgID,
	})
}

// ─── authorization_code (+ PKCE) ───────────────────────────────────────────────

func handleAuthorizationCode(c *gin.Context, cfg Config) {
	clientID := c.PostForm("client_id")
	if clientID == "" {
		if id, _, ok := c.Request.BasicAuth(); ok {
			clientID = id
		}
	}
	client, secretHash, err := cfg.Clients.GetByClientID(c.Request.Context(), clientID)
	if err != nil {
		tokenError(c, http.StatusUnauthorized, "invalid_client", "unknown client")
		return
	}
	if client.RevokedAt != nil {
		tokenError(c, http.StatusUnauthorized, "invalid_client", "client has been revoked")
		return
	}
	if client.IsConfidential {
		_, clientSecret, hasBasic := c.Request.BasicAuth()
		if !hasBasic {
			clientSecret = c.PostForm("client_secret")
		}
		if clientSecret == "" || !secureCompareHash(clientSecret, secretHash) {
			tokenError(c, http.StatusUnauthorized, "invalid_client", "invalid client credentials")
			return
		}
	}
	if !hasGrantType(client, model.GrantAuthorizationCode) {
		tokenError(c, http.StatusBadRequest, "unauthorized_client", "client is not authorized for authorization_code")
		return
	}

	code := c.PostForm("code")
	redirectURI := c.PostForm("redirect_uri")
	verifier := c.PostForm("code_verifier")
	if code == "" || redirectURI == "" || verifier == "" {
		tokenError(c, http.StatusBadRequest, "invalid_request", "code, redirect_uri, and code_verifier are required")
		return
	}

	authCode, err := cfg.Codes.ConsumeByHash(c.Request.Context(), hashToken(code))
	if err != nil {
		tokenError(c, http.StatusBadRequest, "invalid_grant", "code is invalid, expired, or already used")
		return
	}
	if authCode.ClientID != client.ID {
		tokenError(c, http.StatusBadRequest, "invalid_grant", "code was not issued to this client")
		return
	}
	if authCode.RedirectURI != redirectURI {
		tokenError(c, http.StatusBadRequest, "invalid_grant", "redirect_uri mismatch")
		return
	}
	if authCode.CodeChallengeMethod != "S256" || !verifyPKCE(verifier, authCode.CodeChallenge) {
		tokenError(c, http.StatusBadRequest, "invalid_grant", "PKCE verification failed")
		return
	}

	accessToken, accessHash, err := GenerateAccessToken()
	if err != nil {
		tokenError(c, http.StatusInternalServerError, "server_error", "failed to generate token")
		return
	}
	refreshToken, refreshHash, err := GenerateRefreshToken()
	if err != nil {
		tokenError(c, http.StatusInternalServerError, "server_error", "failed to generate token")
		return
	}

	now := time.Now()
	refreshExp := now.Add(refreshTokenTTL)
	tok := &model.OAuthToken{
		ClientID:              client.ID,
		ActingUserID:          authCode.UserID,
		GrantType:             model.GrantAuthorizationCode,
		Scopes:                authCode.Scopes,
		OrgIDs:                authCode.OrgIDs,
		AccessTokenExpiresAt:  now.Add(accessTokenTTL),
		RefreshTokenExpiresAt: &refreshExp,
	}
	if err := cfg.Tokens.Create(c.Request.Context(), tok, accessHash, &refreshHash); err != nil {
		tokenError(c, http.StatusInternalServerError, "server_error", "failed to persist token")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"token_type":    "Bearer",
		"expires_in":    int(accessTokenTTL.Seconds()),
		"scope":         scopesToParam(authCode.Scopes),
	})
}

// ─── refresh_token (rotation-on-use) ───────────────────────────────────────────

func handleRefreshToken(c *gin.Context, cfg Config) {
	refreshToken := c.PostForm("refresh_token")
	if refreshToken == "" {
		tokenError(c, http.StatusBadRequest, "invalid_request", "refresh_token is required")
		return
	}

	// The client must always be identified (client_id required) and, if
	// confidential, authenticated with its secret — the same rule
	// handleAuthorizationCode applies, so a public (PKCE) client's refresh
	// flow keeps working without a secret it was never issued. RotateRefresh
	// below then binds rotation to this client's ID, so a refresh token
	// cannot be redeemed by any client other than the one it was issued to.
	client, ok := authenticateClient(c, cfg, false)
	if !ok {
		return
	}

	newAccessToken, newAccessHash, err := GenerateAccessToken()
	if err != nil {
		tokenError(c, http.StatusInternalServerError, "server_error", "failed to generate token")
		return
	}
	newRefreshToken, newRefreshHash, err := GenerateRefreshToken()
	if err != nil {
		tokenError(c, http.StatusInternalServerError, "server_error", "failed to generate token")
		return
	}

	now := time.Now()
	refreshExp := now.Add(refreshTokenTTL)
	tok, err := cfg.Tokens.RotateRefresh(c.Request.Context(), client.ID, hashToken(refreshToken), newAccessHash, newRefreshHash, now.Add(accessTokenTTL), refreshExp)
	if err != nil {
		// Reuse of an already-rotated/expired/revoked refresh token, or an
		// attempt to redeem a token issued to a different client. We cannot
		// identify which token row to revoke without an old-hash lookup keyed
		// differently, so nothing further to revoke here beyond the fact that
		// RotateRefresh's WHERE clause already prevented it from succeeding.
		tokenError(c, http.StatusBadRequest, "invalid_grant", "refresh token is invalid, expired, already used, or was not issued to this client")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"access_token":  newAccessToken,
		"refresh_token": newRefreshToken,
		"token_type":    "Bearer",
		"expires_in":    int(accessTokenTTL.Seconds()),
		"scope":         scopesToParam(tok.Scopes),
	})
}
