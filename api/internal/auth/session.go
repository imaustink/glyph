package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/store"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/oauth2"
)

// SessionConfig holds the settings for OIDC login/callback and session cookies.
type SessionConfig struct {
	OAuth2        oauth2.Config
	IssuerURL     string // e.g. https://accounts.google.com
	SessionSecret []byte // HMAC key for signing session JWTs
	CookieDomain  string // e.g. "localhost"
	CookieSecure  bool   // true in production (HTTPS)
	FrontendURL   string // where to redirect after login, e.g. http://localhost:5173
}

const (
	sessionCookieName = "glyph_session"
	stateCookieName   = "glyph_oauth_state"
	nonceCookieName   = "glyph_oauth_nonce"
	sessionDuration   = 7 * 24 * time.Hour // 1 week
)

// RegisterAuthRoutes adds /auth/login, /auth/callback, /auth/me, /auth/logout.
func RegisterAuthRoutes(r *gin.Engine, cfg SessionConfig, users store.UserStore) {
	km := newKeyManager(cfg.IssuerURL)
	authCfg := Config{IssuerURL: cfg.IssuerURL, Audience: cfg.OAuth2.ClientID}

	a := r.Group("/auth")
	{
		a.GET("/login", loginHandler(cfg))
		a.GET("/callback", callbackHandler(cfg, authCfg, km, users))
		a.GET("/me", SessionMiddleware(cfg, users), meHandler())
		a.POST("/logout", logoutHandler(cfg))
	}
}

// SessionMiddleware validates the session cookie and populates the user in ctx.
// Use this instead of the Bearer-token Middleware for cookie-based sessions.
func SessionMiddleware(cfg SessionConfig, users store.UserStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		cookie, err := c.Cookie(sessionCookieName)
		if err != nil || cookie == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
			return
		}

		claims, err := parseSessionToken(cookie, cfg.SessionSecret)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid session"})
			return
		}

		userID, err := uuid.Parse(claims.Subject)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid session subject"})
			return
		}

		user, err := users.GetByID(c.Request.Context(), userID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
			return
		}

		c.Set(ContextKey, user)
		c.Next()
	}
}

// ─── Handlers ─────────────────────────────────────────────────────────────────

func loginHandler(cfg SessionConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		state, err := randomString(32)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate state"})
			return
		}
		nonce, err := randomString(32)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate nonce"})
			return
		}

		// Store state and nonce in short-lived, HTTP-only cookies
		setTempCookie(c, stateCookieName, state, cfg.CookieDomain, cfg.CookieSecure)
		setTempCookie(c, nonceCookieName, nonce, cfg.CookieDomain, cfg.CookieSecure)

		url := cfg.OAuth2.AuthCodeURL(state,
			oauth2.SetAuthURLParam("nonce", nonce),
			oauth2.SetAuthURLParam("prompt", "select_account"),
		)
		c.Redirect(http.StatusFound, url)
	}
}

func callbackHandler(cfg SessionConfig, authCfg Config, km *keyManager, users store.UserStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		// ── Verify state ──────────────────────────────────────────────────────
		state, _ := c.Cookie(stateCookieName)
		if state == "" || state != c.Query("state") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid state parameter"})
			return
		}
		clearCookie(c, stateCookieName, cfg.CookieDomain, cfg.CookieSecure)

		savedNonce, _ := c.Cookie(nonceCookieName)
		clearCookie(c, nonceCookieName, cfg.CookieDomain, cfg.CookieSecure)

		// ── Check for error from provider ─────────────────────────────────────
		if errParam := c.Query("error"); errParam != "" {
			desc := c.Query("error_description")
			slog.Warn("OIDC callback error",
				"error", errParam,
				"description", desc)
			c.Redirect(http.StatusFound, cfg.FrontendURL+"?error=auth_failed")
			return
		}

		// ── Exchange code for tokens ──────────────────────────────────────────
		code := c.Query("code")
		if code == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "missing code"})
			return
		}

		token, err := cfg.OAuth2.Exchange(c.Request.Context(), code)
		if err != nil {
			slog.Error("token exchange failed", "error", err)
			c.Redirect(http.StatusFound, cfg.FrontendURL+"?error=token_exchange_failed")
			return
		}

		// ── Extract and validate the ID token ─────────────────────────────────
		rawIDToken, ok := token.Extra("id_token").(string)
		if !ok || rawIDToken == "" {
			c.Redirect(http.StatusFound, cfg.FrontendURL+"?error=no_id_token")
			return
		}

		claims, err := validateToken(c.Request.Context(), rawIDToken, authCfg, km)
		if err != nil {
			slog.Error("ID token validation failed", "error", err)
			c.Redirect(http.StatusFound, cfg.FrontendURL+"?error=invalid_id_token")
			return
		}

		// ── Verify nonce ──────────────────────────────────────────────────────
		if nonceClaim, _ := claims["nonce"].(string); nonceClaim == "" || nonceClaim != savedNonce {
			c.Redirect(http.StatusFound, cfg.FrontendURL+"?error=invalid_nonce")
			return
		}

		// ── Upsert user ───────────────────────────────────────────────────────
		sub, _ := claims["sub"].(string)
		iss, _ := claims["iss"].(string)
		email := stringPtr(claims["email"])
		name := stringPtr(claims["name"])

		if sub == "" || iss == "" {
			c.Redirect(http.StatusFound, cfg.FrontendURL+"?error=missing_claims")
			return
		}

		user, err := users.Upsert(c.Request.Context(), sub, iss, email, name)
		if err != nil {
			slog.Error("user upsert failed", "error", err, "sub", sub)
			c.Redirect(http.StatusFound, cfg.FrontendURL+"?error=server_error")
			return
		}

		// ── Create session cookie ─────────────────────────────────────────────
		sessionToken, err := createSessionToken(user.ID.String(), cfg.SessionSecret, sessionDuration)
		if err != nil {
			slog.Error("session token creation failed", "error", err)
			c.Redirect(http.StatusFound, cfg.FrontendURL+"?error=server_error")
			return
		}

		setSessionCookie(c, sessionToken, cfg.CookieDomain, cfg.CookieSecure, int(sessionDuration.Seconds()))
		c.Redirect(http.StatusFound, cfg.FrontendURL)
	}
}

func meHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		user := CurrentUser(c)
		c.JSON(http.StatusOK, gin.H{
			"id":    user.ID,
			"email": user.Email,
			"name":  user.Name,
		})
	}
}

func logoutHandler(cfg SessionConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		clearCookie(c, sessionCookieName, cfg.CookieDomain, cfg.CookieSecure)
		c.JSON(http.StatusOK, gin.H{"status": "logged out"})
	}
}

// ─── Session tokens (signed JWTs stored in HTTP-only cookies) ─────────────────

func createSessionToken(userID string, secret []byte, duration time.Duration) (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   userID,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(duration)),
		Issuer:    "glyph-api",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(secret)
}

func parseSessionToken(tokenStr string, secret []byte) (*jwt.RegisteredClaims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &jwt.RegisteredClaims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return secret, nil
	}, jwt.WithExpirationRequired())
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*jwt.RegisteredClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid session token")
	}
	return claims, nil
}

// ─── Cookie helpers ───────────────────────────────────────────────────────────

func setSessionCookie(c *gin.Context, value, domain string, secure bool, maxAge int) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(sessionCookieName, value, maxAge, "/", domain, secure, true)
}

func setTempCookie(c *gin.Context, name, value, domain string, secure bool) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(name, value, 600, "/auth", domain, secure, true) // 10 min TTL
}

func clearCookie(c *gin.Context, name, domain string, secure bool) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(name, "", -1, "/", domain, secure, true)
}

// ─── Utilities ────────────────────────────────────────────────────────────────

func randomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
