// Package auth provides OIDC-based authentication for the Glyph API.
//
// Flow:
//  1. The SvelteKit frontend initiates the OIDC Authorization Code flow and
//     obtains an ID token (or access token with OIDC claims) from the provider.
//  2. The token is sent as a Bearer token in the Authorization header on every
//     API request.
//  3. This middleware validates the JWT signature against the provider's JWKS
//     endpoint (cached + auto-refreshed), verifies standard claims (iss, aud,
//     exp), and upserts the user row in Postgres.
//  4. The resolved *model.User is stored in the Gin context under the key
//     "user" for downstream handlers.
//
// Configuration is entirely via environment variables (see Config below).
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/sync/singleflight"
)

// ContextKey is the Gin context key under which the authenticated user is stored.
const ContextKey = "user"

// Config holds OIDC provider settings loaded from environment variables.
type Config struct {
	// IssuerURL is the base URL of the OIDC provider (e.g. https://accounts.google.com).
	// The JWKS endpoint is discovered via <IssuerURL>/.well-known/openid-configuration.
	IssuerURL string

	// Audience is the expected "aud" claim value (your client_id).
	Audience string
}

// Middleware validates the Bearer JWT, upserts the user, and stores it in ctx.
func Middleware(cfg Config, users store.UserStore) gin.HandlerFunc {
	km := newKeyManager(cfg.IssuerURL)

	return func(c *gin.Context) {
		rawToken, err := extractBearer(c.GetHeader("Authorization"))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing or malformed Authorization header"})
			return
		}

		claims, err := validateToken(c.Request.Context(), rawToken, cfg, km)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token: " + err.Error()})
			return
		}

		email := stringPtr(claims["email"])
		name := stringPtr(claims["name"])

		sub, _ := claims["sub"].(string)
		iss, _ := claims["iss"].(string)
		if sub == "" || iss == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token missing sub or iss claim"})
			return
		}

		user, err := users.Upsert(c.Request.Context(), sub, iss, email, name)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "could not resolve user"})
			return
		}

		c.Set(ContextKey, user)
		c.Next()
	}
}

// CurrentUser retrieves the authenticated user from the Gin context.
// Panics if called outside of an authenticated route (programming error).
func CurrentUser(c *gin.Context) *model.User {
	u, _ := c.MustGet(ContextKey).(*model.User)
	return u
}

// ─── Token validation ─────────────────────────────────────────────────────────

func extractBearer(header string) (string, error) {
	if !strings.HasPrefix(header, "Bearer ") {
		return "", errors.New("no Bearer token")
	}
	token := strings.TrimPrefix(header, "Bearer ")
	if token == "" {
		return "", errors.New("empty token")
	}
	return token, nil
}

func validateToken(ctx context.Context, rawToken string, cfg Config, km *keyManager) (jwt.MapClaims, error) {
	token, err := jwt.Parse(rawToken,
		func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
				if _, ok := t.Method.(*jwt.SigningMethodECDSA); !ok {
					return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
				}
			}
			kid, _ := t.Header["kid"].(string)
			return km.getKey(ctx, kid)
		},
		jwt.WithAudience(cfg.Audience),
		jwt.WithIssuer(cfg.IssuerURL),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid claims")
	}
	return claims, nil
}

// ─── JWKS key manager ─────────────────────────────────────────────────────────
// Fetches the JWKS from the OIDC discovery document and caches keys.
// Automatically refetches when an unknown kid is encountered (key rotation).
//
// singleflight.Group ensures that only one goroutine fetches the JWKS at a time
// even when many concurrent requests race past the TTL check simultaneously.

type keyManager struct {
	issuerURL string
	mu        sync.RWMutex
	keys      map[string]interface{} // kid → crypto public key
	fetchedAt time.Time
	sfg       singleflight.Group
}

const jwksCacheTTL = 5 * time.Minute

func newKeyManager(issuerURL string) *keyManager {
	return &keyManager{issuerURL: issuerURL, keys: make(map[string]interface{})}
}

func (km *keyManager) getKey(ctx context.Context, kid string) (interface{}, error) {
	km.mu.RLock()
	key, ok := km.keys[kid]
	expired := time.Since(km.fetchedAt) > jwksCacheTTL
	km.mu.RUnlock()

	if ok && !expired {
		return key, nil
	}

	// Deduplicate concurrent refreshes: only one goroutine fetches from the
	// OIDC provider; all others block and share its result. We use
	// context.Background() inside the group function so that a cancelled
	// individual request doesn't abort a refresh that other goroutines depend on.
	_, err, _ := km.sfg.Do("refresh", func() (interface{}, error) {
		return nil, km.refresh(context.Background())
	})
	if err != nil {
		return nil, fmt.Errorf("jwks refresh: %w", err)
	}

	km.mu.RLock()
	key, ok = km.keys[kid]
	km.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown kid: %q", kid)
	}
	return key, nil
}

func (km *keyManager) refresh(ctx context.Context) error {
	jwksURI, err := km.discoverJWKSURI(ctx)
	if err != nil {
		return err
	}

	// Use parent context with a timeout to ensure we don't block forever
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURI, nil)
	if err != nil {
		return fmt.Errorf("build jwks request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch jwks: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jwks endpoint returned %d", resp.StatusCode)
	}

	keySet, err := parseJWKS(resp.Body)
	if err != nil {
		return fmt.Errorf("parse jwks: %w", err)
	}

	km.mu.Lock()
	km.keys = keySet
	km.fetchedAt = time.Now()
	km.mu.Unlock()
	return nil
}

func (km *keyManager) discoverJWKSURI(ctx context.Context) (string, error) {
	discoveryURL := strings.TrimRight(km.issuerURL, "/") + "/.well-known/openid-configuration"

	// Use parent context with a timeout
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch discovery: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("discovery endpoint returned %d", resp.StatusCode)
	}

	var doc struct {
		JWKSURI string `json:"jwks_uri"`
	}
	if err := decodeJSON(resp.Body, &doc); err != nil {
		return "", fmt.Errorf("decode discovery: %w", err)
	}
	if doc.JWKSURI == "" {
		return "", errors.New("discovery document missing jwks_uri")
	}
	return doc.JWKSURI, nil
}

func stringPtr(v interface{}) *string {
	if v == nil {
		return nil
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return nil
	}
	return &s
}
