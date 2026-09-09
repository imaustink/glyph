package oauth

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/auth"
	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
)

func extractBearer(header string) (string, bool) {
	if !strings.HasPrefix(header, "Bearer ") {
		return "", false
	}
	tok := strings.TrimPrefix(header, "Bearer ")
	if tok == "" {
		return "", false
	}
	return tok, true
}

// BearerTokenMiddleware authenticates a request via an OAuth access token
// (Authorization: Bearer ...), populating auth.ContextKey (the same key
// SessionMiddleware uses) with the acting user, plus a *model.TokenScope
// under model.TokenScopeContextKey for downstream scope checks.
func BearerTokenMiddleware(tokens store.OAuthTokenStore, users store.UserStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, ok := extractBearer(c.GetHeader("Authorization"))
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing or malformed Authorization header"})
			return
		}
		tok, err := tokens.GetByAccessHash(c.Request.Context(), hashToken(raw))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			return
		}
		if tok.RevokedAt != nil || tok.AccessTokenExpiresAt.Before(time.Now()) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			return
		}
		user, err := users.GetByID(c.Request.Context(), tok.ActingUserID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "acting user not found"})
			return
		}
		c.Set(auth.ContextKey, user)
		c.Set(model.TokenScopeContextKey, &model.TokenScope{
			ClientID: tok.ClientID,
			Scopes:   tok.Scopes,
			OrgIDs:   tok.OrgIDs,
		})
		go tokens.TouchLastUsed(context.WithoutCancel(c.Request.Context()), tok.ID) //nolint:errcheck
		c.Next()
	}
}

// CurrentScope returns the *model.TokenScope for the current request, or nil
// if the request authenticated via session cookie (unrestricted, governed
// only by the acting user's own permissions).
func CurrentScope(c *gin.Context) *model.TokenScope {
	v, ok := c.Get(model.TokenScopeContextKey)
	if !ok {
		return nil
	}
	ts, _ := v.(*model.TokenScope)
	return ts
}

// DualAuthMiddleware tries bearer-token auth when an Authorization header is
// present, otherwise falls back to the given session middleware. This lets
// /api/v1 serve both browser (cookie) and agent (bearer) traffic on the same
// route table.
func DualAuthMiddleware(sessionMw, bearerMw gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetHeader("Authorization") != "" {
			bearerMw(c)
			return
		}
		sessionMw(c)
	}
}
