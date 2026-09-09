package oauth

import (
	"errors"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/auth"
	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// consentClaims re-encodes the validated /oauth/authorize query params into a
// short-lived signed token, closing the tampering window between the GET
// (which validates client/redirect_uri/scope) and the POST (which acts on
// the user's decision) — the frontend never re-supplies redirect_uri/scope
// itself, so it cannot smuggle in a different value.
type consentClaims struct {
	jwt.RegisteredClaims
	ClientID            string   `json:"client_id"`
	RedirectURI         string   `json:"redirect_uri"`
	Scopes              []string `json:"scopes"`
	OrgID               string   `json:"org_id"`
	State               string   `json:"state"`
	CodeChallenge       string   `json:"code_challenge"`
	CodeChallengeMethod string   `json:"code_challenge_method"`
}

// consentTokenTracker enforces single-use consent tokens: once a token's jti
// has been consumed by a decision (approve or deny), a replay with the same
// token must be rejected — otherwise one approval could mint multiple
// authorization codes within the 5-minute consent-token TTL. Entries are
// evicted once their token would have expired anyway (parseConsentToken
// already rejects an expired token on its own), so this map never grows
// beyond roughly consentTokenTTL worth of traffic.
type consentTokenTracker struct {
	mu   sync.Mutex
	used map[string]time.Time // jti -> expiry
}

func newConsentTokenTracker() *consentTokenTracker {
	return &consentTokenTracker{used: make(map[string]time.Time)}
}

// consumeOnce records jti as used and returns true the first time it is
// seen; it returns false — a replay — on any subsequent call with the same
// jti.
func (t *consentTokenTracker) consumeOnce(jti string, expiresAt time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	for j, exp := range t.used {
		if exp.Before(now) {
			delete(t.used, j)
		}
	}
	if _, seen := t.used[jti]; seen {
		return false
	}
	t.used[jti] = expiresAt
	return true
}

var globalConsentTokenTracker = newConsentTokenTracker()

func signConsentToken(cfg Config, cc consentClaims) (string, error) {
	now := time.Now()
	jti, err := randomToken(16)
	if err != nil {
		return "", err
	}
	cc.ID = jti
	cc.IssuedAt = jwt.NewNumericDate(now)
	cc.ExpiresAt = jwt.NewNumericDate(now.Add(consentTokenTTL))
	cc.Issuer = "glyph-oauth-consent"
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, cc)
	return token.SignedString(cfg.ConsentSecret)
}

func parseConsentToken(cfg Config, raw string) (*consentClaims, error) {
	claims := &consentClaims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return cfg.ConsentSecret, nil
	}, jwt.WithExpirationRequired(), jwt.WithIssuer("glyph-oauth-consent"))
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, errors.New("invalid consent token")
	}
	return claims, nil
}

// AuthorizeInfoHandler handles GET /api/v1/oauth/consent (backing the
// SvelteKit /oauth/authorize page) — validates the request and returns the
// consent screen payload plus a signed consent_token for the frontend to
// render and later post back.
func AuthorizeInfoHandler(cfg Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireInteractiveUser(c) {
			return
		}
		user := auth.CurrentUser(c)
		if user == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
			return
		}

		if c.Query("response_type") != "code" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "error_description": "response_type must be code"})
			return
		}
		clientID := c.Query("client_id")
		redirectURI := c.Query("redirect_uri")
		state := c.Query("state")
		codeChallenge := c.Query("code_challenge")
		codeChallengeMethod := c.Query("code_challenge_method")
		orgIDStr := c.Query("org_id")

		if clientID == "" || redirectURI == "" || codeChallenge == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "error_description": "client_id, redirect_uri, and code_challenge are required"})
			return
		}
		if codeChallengeMethod != "S256" || !isValidCodeChallenge(codeChallenge) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "error_description": "code_challenge_method must be S256 with a valid code_challenge"})
			return
		}

		client, _, err := cfg.Clients.GetByClientID(c.Request.Context(), clientID)
		if err != nil || client.RevokedAt != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_client", "error_description": "unknown or revoked client"})
			return
		}
		if !hasGrantType(client, model.GrantAuthorizationCode) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unauthorized_client", "error_description": "client is not authorized for authorization_code"})
			return
		}
		if !redirectURIAllowed(client, redirectURI) {
			// Never redirect for a mismatched/unregistered redirect_uri — open
			// redirect safety. Only a JSON error is returned.
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "error_description": "redirect_uri is not registered for this client"})
			return
		}

		orgID, err := uuid.Parse(orgIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "error_description": "org_id is required and must be a valid UUID"})
			return
		}
		hasOrg, err := cfg.Clients.HasOrg(c.Request.Context(), client.ID, orgID)
		if err != nil || !hasOrg {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_grant", "error_description": "client is not scoped to this org"})
			return
		}
		member, err := cfg.Orgs.GetMember(c.Request.Context(), orgID, user.ID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				c.JSON(http.StatusForbidden, gin.H{"error": "invalid_grant", "error_description": "you are not a member of this org"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
			return
		}
		_ = member

		org, err := cfg.Orgs.GetByID(c.Request.Context(), orgID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
			return
		}

		scopes := intersectScopes(parseScopeParam(c.Query("scope")), client.Scopes)

		token, err := signConsentToken(cfg, consentClaims{
			ClientID:            clientID,
			RedirectURI:         redirectURI,
			Scopes:              scopesToStringSlice(scopes),
			OrgID:               orgID.String(),
			State:               state,
			CodeChallenge:       codeChallenge,
			CodeChallengeMethod: codeChallengeMethod,
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"client": gin.H{
				"name":        client.Name,
				"description": client.Description,
			},
			"orgName":      org.Name,
			"scopes":       scopesToStringSlice(scopes),
			"consentToken": token,
		})
	}
}

// AuthorizeDecisionHandler handles POST /api/v1/oauth/consent/decision
// (backing the SvelteKit /oauth/authorize page) — the user's approve/deny
// decision from the consent screen.
func AuthorizeDecisionHandler(cfg Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireInteractiveUser(c) {
			return
		}
		user := auth.CurrentUser(c)
		if user == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
			return
		}

		var body struct {
			ConsentToken string `json:"consentToken"`
			Approve      bool   `json:"approve"`
		}
		if err := c.ShouldBindJSON(&body); err != nil || body.ConsentToken == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
			return
		}

		claims, err := parseConsentToken(cfg, body.ConsentToken)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "error_description": "consent token is invalid or expired"})
			return
		}
		// Single-use: without this, one approval could be replayed to mint
		// multiple authorization codes within the token's 5-minute TTL.
		if claims.ID == "" || !globalConsentTokenTracker.consumeOnce(claims.ID, claims.ExpiresAt.Time) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "error_description": "consent token has already been used"})
			return
		}

		if !body.Approve {
			c.JSON(http.StatusOK, gin.H{"redirectUrl": buildRedirect(claims.RedirectURI, map[string]string{
				"error": "access_denied",
				"state": claims.State,
			})})
			return
		}

		client, _, err := cfg.Clients.GetByClientID(c.Request.Context(), claims.ClientID)
		if err != nil || client.RevokedAt != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_client"})
			return
		}
		// Re-validate redirect_uri against the client's current registered
		// list, not just at the earlier GET — a client's redirect URIs can
		// change in the window between the two requests.
		if !redirectURIAllowed(client, claims.RedirectURI) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "error_description": "redirect_uri is no longer registered for this client"})
			return
		}
		orgID, err := uuid.Parse(claims.OrgID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
			return
		}
		// Re-verify membership at decision time (not just at GET time).
		if _, err := cfg.Orgs.GetMember(c.Request.Context(), orgID, user.ID); err != nil {
			c.JSON(http.StatusForbidden, gin.H{"error": "invalid_grant", "error_description": "you are not a member of this org"})
			return
		}

		code, codeHash, err := GenerateAuthCode()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
			return
		}
		scopes := make([]model.OAuthScope, len(claims.Scopes))
		for i, s := range claims.Scopes {
			scopes[i] = model.OAuthScope(s)
		}
		authCode := &model.OAuthAuthorizationCode{
			ClientID:            client.ID,
			UserID:              user.ID,
			RedirectURI:         claims.RedirectURI,
			Scopes:              scopes,
			OrgIDs:              []uuid.UUID{orgID},
			CodeChallenge:       claims.CodeChallenge,
			CodeChallengeMethod: claims.CodeChallengeMethod,
			ExpiresAt:           time.Now().Add(authCodeTTL),
		}
		if err := cfg.Codes.Create(c.Request.Context(), authCode, codeHash); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"redirectUrl": buildRedirect(claims.RedirectURI, map[string]string{
			"code":  code,
			"state": claims.State,
		})})
	}
}

// requireInteractiveUser rejects requests authenticated via an OAuth bearer
// token — consent must come from an actual logged-in human session, not an
// M2M client credential (which has no business granting consent on a user's
// behalf, and has no session for CSRF protection to key off of anyway).
func requireInteractiveUser(c *gin.Context) bool {
	if _, ok := c.Get(model.TokenScopeContextKey); ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "consent requires an interactive user session"})
		return false
	}
	return true
}

func redirectURIAllowed(client *model.OAuthClient, redirectURI string) bool {
	for _, u := range client.RedirectURIs {
		if u == redirectURI {
			return true
		}
	}
	return false
}

func scopesToStringSlice(scopes []model.OAuthScope) []string {
	out := make([]string, len(scopes))
	for i, s := range scopes {
		out[i] = string(s)
	}
	return out
}

func buildRedirect(base string, params map[string]string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	q := u.Query()
	for k, v := range params {
		if v != "" {
			q.Set(k, v)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}
