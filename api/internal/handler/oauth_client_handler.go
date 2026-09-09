package handler

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/auth"
	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
	"github.com/google/uuid"
)

// generateClientCredentials mirrors oauth.GenerateClientCredentials. It is
// duplicated here (rather than imported) because package oauth imports
// package handler (for CSRFMiddleware/RateLimiter), so handler cannot import
// oauth without creating an import cycle.
func generateClientCredentials() (clientID, clientSecret, secretHash string, err error) {
	randomToken := func(n int) (string, error) {
		b := make([]byte, n)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		return base64.RawURLEncoding.EncodeToString(b), nil
	}
	idPart, err := randomToken(18)
	if err != nil {
		return "", "", "", err
	}
	clientSecret, err = randomToken(32)
	if err != nil {
		return "", "", "", err
	}
	sum := sha256.Sum256([]byte(clientSecret))
	return "glyph_client_" + idPart, clientSecret, hex.EncodeToString(sum[:]), nil
}

// OAuthClientHandler handles org-owner-scoped CRUD for OAuth clients (the
// admin-facing side of OAuth delegation) plus audit/revocation of the tokens
// issued to them. It is only reachable via cookie-session auth — a bearer
// (OAuth) token can never manage OAuth clients, including its own.
type OAuthClientHandler struct {
	Clients store.OAuthClientStore
	Tokens  store.OAuthTokenStore
	Orgs    store.OrgStore
}

// RequireOrgOwner returns true if userID is an owner of orgID; writes 403 or
// 404 otherwise. Mirrors OrgHandler's orgOwnerGuard so both handlers enforce
// the same owner-only gate for org-scoped admin actions.
func RequireOrgOwner(c *gin.Context, orgs store.OrgStore, orgID, userID uuid.UUID) bool {
	m, err := orgs.GetMember(c.Request.Context(), orgID, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return false
	}
	if m.Role != model.OrgRoleOwner {
		c.JSON(http.StatusForbidden, gin.H{"error": "owner role required"})
		return false
	}
	return true
}

// rejectBearerAuth ensures the admin OAuth-client API is never reachable via
// a bearer (OAuth) token — a client must never manage its own or another
// client's credentials.
func rejectBearerAuth(c *gin.Context) bool {
	if _, ok := c.Get(model.TokenScopeContextKey); ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "OAuth clients cannot manage OAuth clients"})
		return false
	}
	return true
}

func validRedirectURIs(uris []string) bool {
	for _, u := range uris {
		if u == "" {
			return false
		}
	}
	return true
}

func validScopes(scopes []model.OAuthScope) bool {
	for _, s := range scopes {
		if !s.IsValid() {
			return false
		}
	}
	return true
}

// POST /orgs/:orgId/oauth-clients
func (h *OAuthClientHandler) CreateClient(c *gin.Context) {
	if !rejectBearerAuth(c) {
		return
	}
	user := auth.CurrentUser(c)
	orgID, ok := parseUUID(c, "orgId")
	if !ok {
		return
	}
	if !RequireOrgOwner(c, h.Orgs, orgID, user.ID) {
		return
	}

	var body struct {
		Name           string   `json:"name"`
		Description    *string  `json:"description"`
		Scopes         []string `json:"scopes"`
		RedirectURIs   []string `json:"redirectUris"`
		GrantTypes     []string `json:"grantTypes"`
		IsConfidential *bool    `json:"isConfidential"`
	}
	if !bindJSON(c, &body) {
		return
	}
	if body.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}

	scopes := make([]model.OAuthScope, len(body.Scopes))
	for i, s := range body.Scopes {
		scopes[i] = model.OAuthScope(s)
	}
	if !validScopes(scopes) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid scope"})
		return
	}
	if !validRedirectURIs(body.RedirectURIs) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid redirect_uris"})
		return
	}
	// Default to both grant types: the admin UI has no control for this field,
	// so a client created there must support client_credentials (M2M) AND
	// authorization_code (user consent) out of the box, or the consent flow
	// would be unreachable for any UI-created client. An API caller may still
	// restrict this explicitly via grantTypes.
	grantTypes := []model.OAuthGrantType{model.GrantClientCredentials, model.GrantAuthorizationCode}
	if len(body.GrantTypes) > 0 {
		grantTypes = make([]model.OAuthGrantType, len(body.GrantTypes))
		for i, g := range body.GrantTypes {
			gt := model.OAuthGrantType(g)
			if !gt.IsValid() {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid grant type"})
				return
			}
			grantTypes[i] = gt
		}
	}
	isConfidential := true
	if body.IsConfidential != nil {
		isConfidential = *body.IsConfidential
	}

	clientIDStr, clientSecret, secretHash, err := generateClientCredentials()
	if err != nil {
		internalError(c, err)
		return
	}

	client := &model.OAuthClient{
		ClientID:       clientIDStr,
		Name:           body.Name,
		Description:    body.Description,
		CreatedByID:    user.ID,
		GrantTypes:     grantTypes,
		Scopes:         scopes,
		RedirectURIs:   body.RedirectURIs,
		IsConfidential: isConfidential,
	}
	created, err := h.Clients.Create(c.Request.Context(), client, secretHash)
	if err != nil {
		internalError(c, err)
		return
	}
	if err := h.Clients.AddOrg(c.Request.Context(), created.ID, orgID, user.ID); err != nil {
		internalError(c, err)
		return
	}
	created.OrgIDs = []uuid.UUID{orgID}

	c.JSON(http.StatusCreated, model.OAuthClientWithSecret{OAuthClient: *created, ClientSecret: clientSecret})
}

// GET /orgs/:orgId/oauth-clients
func (h *OAuthClientHandler) ListClients(c *gin.Context) {
	if !rejectBearerAuth(c) {
		return
	}
	user := auth.CurrentUser(c)
	orgID, ok := parseUUID(c, "orgId")
	if !ok {
		return
	}
	if !RequireOrgOwner(c, h.Orgs, orgID, user.ID) {
		return
	}
	clients, err := h.Clients.ListForOrg(c.Request.Context(), orgID)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, clients)
}

// GET /orgs/:orgId/oauth-clients/:clientId
func (h *OAuthClientHandler) GetClient(c *gin.Context) {
	if !rejectBearerAuth(c) {
		return
	}
	user := auth.CurrentUser(c)
	orgID, ok := parseUUID(c, "orgId")
	if !ok {
		return
	}
	if !RequireOrgOwner(c, h.Orgs, orgID, user.ID) {
		return
	}
	client, ok := h.getScopedClient(c, orgID)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, client)
}

// PATCH /orgs/:orgId/oauth-clients/:clientId
func (h *OAuthClientHandler) UpdateClient(c *gin.Context) {
	if !rejectBearerAuth(c) {
		return
	}
	user := auth.CurrentUser(c)
	orgID, ok := parseUUID(c, "orgId")
	if !ok {
		return
	}
	if !RequireOrgOwner(c, h.Orgs, orgID, user.ID) {
		return
	}
	client, ok := h.getScopedClient(c, orgID)
	if !ok {
		return
	}

	var body struct {
		Name         *string   `json:"name"`
		Description  *string   `json:"description"`
		Scopes       *[]string `json:"scopes"`
		RedirectURIs *[]string `json:"redirectUris"`
	}
	if !bindJSON(c, &body) {
		return
	}

	var scopes []model.OAuthScope
	if body.Scopes != nil {
		scopes = make([]model.OAuthScope, len(*body.Scopes))
		for i, s := range *body.Scopes {
			scopes[i] = model.OAuthScope(s)
		}
		if !validScopes(scopes) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid scope"})
			return
		}
	}
	var redirectURIs []string
	if body.RedirectURIs != nil {
		if !validRedirectURIs(*body.RedirectURIs) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid redirect_uris"})
			return
		}
		redirectURIs = *body.RedirectURIs
	}

	updated, err := h.Clients.Update(c.Request.Context(), client.ID, body.Name, body.Description, scopes, redirectURIs)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, updated)
}

// POST /orgs/:orgId/oauth-clients/:clientId/orgs
func (h *OAuthClientHandler) AddClientOrg(c *gin.Context) {
	if !rejectBearerAuth(c) {
		return
	}
	user := auth.CurrentUser(c)
	orgID, ok := parseUUID(c, "orgId")
	if !ok {
		return
	}
	if !RequireOrgOwner(c, h.Orgs, orgID, user.ID) {
		return
	}
	client, ok := h.getScopedClient(c, orgID)
	if !ok {
		return
	}
	var body struct {
		OrgID string `json:"orgId"`
	}
	if !bindJSON(c, &body) {
		return
	}
	otherOrgID, err := uuid.Parse(body.OrgID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid orgId"})
		return
	}
	// Caller must also own the org being added.
	if !RequireOrgOwner(c, h.Orgs, otherOrgID, user.ID) {
		return
	}
	if err := h.Clients.AddOrg(c.Request.Context(), client.ID, otherOrgID, user.ID); err != nil {
		internalError(c, err)
		return
	}
	updated, err := h.Clients.GetByID(c.Request.Context(), client.ID)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, updated)
}

// DELETE /orgs/:orgId/oauth-clients/:clientId/orgs/:otherOrgId
func (h *OAuthClientHandler) RemoveClientOrg(c *gin.Context) {
	if !rejectBearerAuth(c) {
		return
	}
	user := auth.CurrentUser(c)
	orgID, ok := parseUUID(c, "orgId")
	if !ok {
		return
	}
	if !RequireOrgOwner(c, h.Orgs, orgID, user.ID) {
		return
	}
	client, ok := h.getScopedClient(c, orgID)
	if !ok {
		return
	}
	otherOrgID, ok := parseUUID(c, "otherOrgId")
	if !ok {
		return
	}
	if err := h.Clients.RemoveOrg(c.Request.Context(), client.ID, otherOrgID); err != nil {
		internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// POST /orgs/:orgId/oauth-clients/:clientId/rotate-secret?revokeExisting=true
//
// Rotating the secret alone does not affect tokens already issued under the
// old secret — they keep working until they naturally expire. That is
// usually what you want (a live rotation shouldn't interrupt in-flight
// delegated sessions), but it means a suspected-leaked secret isn't fully
// contained by rotation alone. Pass ?revokeExisting=true to also revoke
// every token issued to this client, for the "assume compromise" case.
func (h *OAuthClientHandler) RotateSecret(c *gin.Context) {
	if !rejectBearerAuth(c) {
		return
	}
	user := auth.CurrentUser(c)
	orgID, ok := parseUUID(c, "orgId")
	if !ok {
		return
	}
	if !RequireOrgOwner(c, h.Orgs, orgID, user.ID) {
		return
	}
	client, ok := h.getScopedClient(c, orgID)
	if !ok {
		return
	}
	_, newSecret, newHash, err := generateClientCredentials()
	if err != nil {
		internalError(c, err)
		return
	}
	updated, err := h.Clients.RotateSecret(c.Request.Context(), client.ID, newHash)
	if err != nil {
		internalError(c, err)
		return
	}
	if c.Query("revokeExisting") == "true" {
		if err := h.Tokens.RevokeAllForClient(c.Request.Context(), client.ID); err != nil {
			internalError(c, err)
			return
		}
	}
	c.JSON(http.StatusOK, model.OAuthClientWithSecret{OAuthClient: *updated, ClientSecret: newSecret})
}

// POST /orgs/:orgId/oauth-clients/:clientId/revoke
func (h *OAuthClientHandler) RevokeClient(c *gin.Context) {
	if !rejectBearerAuth(c) {
		return
	}
	user := auth.CurrentUser(c)
	orgID, ok := parseUUID(c, "orgId")
	if !ok {
		return
	}
	if !RequireOrgOwner(c, h.Orgs, orgID, user.ID) {
		return
	}
	client, ok := h.getScopedClient(c, orgID)
	if !ok {
		return
	}
	if err := h.Clients.Revoke(c.Request.Context(), client.ID); err != nil {
		internalError(c, err)
		return
	}
	if err := h.Tokens.RevokeAllForClient(c.Request.Context(), client.ID); err != nil {
		internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// GET /orgs/:orgId/oauth-clients/:clientId/tokens
func (h *OAuthClientHandler) ListClientTokens(c *gin.Context) {
	if !rejectBearerAuth(c) {
		return
	}
	user := auth.CurrentUser(c)
	orgID, ok := parseUUID(c, "orgId")
	if !ok {
		return
	}
	if !RequireOrgOwner(c, h.Orgs, orgID, user.ID) {
		return
	}
	client, ok := h.getScopedClient(c, orgID)
	if !ok {
		return
	}
	tokens, err := h.Tokens.ListActiveForClient(c.Request.Context(), client.ID)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, tokens)
}

// DELETE /orgs/:orgId/oauth-clients/:clientId/tokens/:tokenId
func (h *OAuthClientHandler) RevokeToken(c *gin.Context) {
	if !rejectBearerAuth(c) {
		return
	}
	user := auth.CurrentUser(c)
	orgID, ok := parseUUID(c, "orgId")
	if !ok {
		return
	}
	if !RequireOrgOwner(c, h.Orgs, orgID, user.ID) {
		return
	}
	if _, ok := h.getScopedClient(c, orgID); !ok {
		return
	}
	tokenID, ok := parseUUID(c, "tokenId")
	if !ok {
		return
	}
	if err := h.Tokens.Revoke(c.Request.Context(), tokenID); err != nil {
		internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// POST /orgs/:orgId/oauth-clients/:clientId/tokens/revoke-all
func (h *OAuthClientHandler) RevokeAllTokens(c *gin.Context) {
	if !rejectBearerAuth(c) {
		return
	}
	user := auth.CurrentUser(c)
	orgID, ok := parseUUID(c, "orgId")
	if !ok {
		return
	}
	if !RequireOrgOwner(c, h.Orgs, orgID, user.ID) {
		return
	}
	client, ok := h.getScopedClient(c, orgID)
	if !ok {
		return
	}
	if err := h.Tokens.RevokeAllForClient(c.Request.Context(), client.ID); err != nil {
		internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// getScopedClient fetches the :clientId param and verifies it belongs to orgID,
// writing 404 otherwise (so an owner of org A cannot probe/act on a client
// scoped only to org B by guessing IDs).
func (h *OAuthClientHandler) getScopedClient(c *gin.Context, orgID uuid.UUID) (*model.OAuthClient, bool) {
	clientID, ok := parseUUID(c, "clientId")
	if !ok {
		return nil, false
	}
	client, err := h.Clients.GetByID(c.Request.Context(), clientID)
	if err != nil {
		notFoundOrError(c, err)
		return nil, false
	}
	for _, id := range client.OrgIDs {
		if id == orgID {
			return client, true
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
	return nil, false
}
