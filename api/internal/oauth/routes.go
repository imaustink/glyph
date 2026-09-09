package oauth

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/auth"
	"github.com/glyph/api/internal/handler"
)

// RegisterOAuthRoutes registers the machine-facing OAuth 2.0 protocol
// endpoints: /oauth/token (all three grant types) and /oauth/revoke. These
// are unauthenticated by session (clients authenticate with their own
// credentials) and are registered directly on the engine — not under
// apiGroup — since /oauth/authorize (the browser-facing SvelteKit consent
// page) already owns that path prefix for human navigation.
//
// optionalSessionMw, when non-nil, is applied only to /oauth/revoke — it
// populates the acting user in context when a valid session cookie is
// present (without requiring one), so RevokeHandler's "a user may revoke a
// token issued against their own account" branch has a user to check
// against. Pass nil where there is no session concept to attach (e.g. dev
// auth mode) — RevokeHandler still works via client-credential revocation
// either way.
func RegisterOAuthRoutes(r *gin.Engine, cfg Config, optionalSessionMw gin.HandlerFunc) {
	tokenLimiter := handler.NewRateLimiter(20, time.Minute)

	oauthGroup := r.Group("/oauth")
	{
		oauthGroup.POST("/token", tokenEndpointRateLimit(tokenLimiter), TokenHandler(cfg))
		revokeMiddlewares := []gin.HandlerFunc{tokenEndpointRateLimit(tokenLimiter)}
		if optionalSessionMw != nil {
			revokeMiddlewares = append(revokeMiddlewares, optionalSessionMw)
		}
		revokeMiddlewares = append(revokeMiddlewares, RevokeHandler(cfg))
		oauthGroup.POST("/revoke", revokeMiddlewares...)
	}
}

// RegisterConsentRoutes registers the human-facing consent endpoints that
// back the SvelteKit /oauth/authorize page: GET /oauth/consent (validates
// the request and returns the info the consent screen renders) and POST
// /oauth/consent/decision (records approve/deny). These live under apiGroup
// (deliberately NOT at /oauth/authorize itself, which is the frontend page's
// own route) so they inherit session auth + CSRF for free, matching every
// other cookie-authenticated mutation in the API.
func RegisterConsentRoutes(apiGroup *gin.RouterGroup, cfg Config) {
	apiGroup.GET("/oauth/consent", AuthorizeInfoHandler(cfg))
	apiGroup.POST("/oauth/consent/decision", AuthorizeDecisionHandler(cfg))
}

// RevokeHandler implements RFC 7009 token revocation. Accepts either client
// credentials (a client revoking its own token) or a logged-in user's
// session (a user revoking a token issued against their own account).
func RevokeHandler(cfg Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.PostForm("token")
		if token == "" {
			// Per RFC 7009, always return 200 even for malformed requests
			// that don't identify a token, to avoid leaking token validity.
			c.Status(http.StatusOK)
			return
		}
		tok, err := cfg.Tokens.GetByAccessHash(c.Request.Context(), hashToken(token))
		if err != nil {
			c.Status(http.StatusOK)
			return
		}

		// Authorize: either the owning client's credentials, or the acting
		// user's own session. This route runs with no unconditional session
		// middleware (client-credential revocation must work without any
		// session at all), so CurrentUserOrNil — not CurrentUser, which
		// panics when no user was ever set in context — is required here.
		authorized := false
		if user := auth.CurrentUserOrNil(c); user != nil && user.ID == tok.ActingUserID {
			authorized = true
		}
		if !authorized {
			// tryAuthenticateClient, not authenticateClient: this route must
			// always return 200 regardless of what (if anything) went wrong
			// with the client credentials — authenticateClient would instead
			// write its own 400/401 JSON error the moment credentials are
			// simply absent (the common case when a user-session request
			// reaches this fallback), which both breaks that contract and
			// leaks whether client_id/token combinations are valid.
			if client, ok := tryAuthenticateClient(c, cfg, true); ok && client.ID == tok.ClientID {
				authorized = true
			}
		}
		if authorized {
			_ = cfg.Tokens.Revoke(c.Request.Context(), tok.ID)
		}
		c.Status(http.StatusOK)
	}
}

// tokenEndpointRateLimit rate-limits /oauth/token and /oauth/revoke by
// client_id + IP rather than by authenticated user, since these are
// pre-authentication (or non-session) endpoints — brute-force secret
// guessing must be throttled before a client is ever resolved.
func tokenEndpointRateLimit(rl *handler.RateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		clientID, _, ok := c.Request.BasicAuth()
		if !ok {
			clientID = c.PostForm("client_id")
		}
		key := clientID + "|" + c.ClientIP()
		if !rl.Allow(key) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded, please try again later"})
			return
		}
		c.Next()
	}
}
