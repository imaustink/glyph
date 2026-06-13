package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/auth"
	"github.com/glyph/api/internal/handler"
	"github.com/glyph/api/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// setupAuth configures the authentication layer and returns the API router group
// with the appropriate middleware applied.
//
// In production (OIDC ready): registers OIDC auth routes and session middleware.
// In dev/test mode: provisions a dev user and registers test-only endpoints.
func setupAuth(ctx context.Context, r *gin.Engine, pool *pgxpool.Pool, users store.UserStore, sessionSecret []byte) *gin.RouterGroup {
	oidcIssuer := os.Getenv("OIDC_ISSUER_URL")
	oidcClientID := os.Getenv("OIDC_CLIENT_ID")
	oidcClientSecret := os.Getenv("OIDC_CLIENT_SECRET")
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "http://localhost:5173"
	}
	cookieDomain := os.Getenv("COOKIE_DOMAIN")
	cookieSecure := os.Getenv("COOKIE_SECURE") == "true"

	oidcReady := oidcIssuer != "" && oidcClientID != "" && oidcClientSecret != "" &&
		!isPlaceholder(oidcIssuer) && !isPlaceholder(oidcClientID) && !isPlaceholder(oidcClientSecret)

	// E2E_RESET_ENABLED forces dev/test mode so that /test/reset and /test/become
	// are available even when OIDC credentials are present in the environment.
	if os.Getenv("E2E_RESET_ENABLED") == "true" {
		oidcReady = false
	}

	warnPartialOIDC(oidcReady, oidcIssuer, oidcClientID, oidcClientSecret)

	if oidcReady {
		return setupOIDCAuth(r, users, oidcIssuer, oidcClientID, oidcClientSecret, sessionSecret, cookieDomain, cookieSecure, frontendURL)
	}
	return setupDevAuth(ctx, r, pool, users, sessionSecret)
}

func setupOIDCAuth(r *gin.Engine, users store.UserStore, issuer, clientID, clientSecret string, sessionSecret []byte, cookieDomain string, cookieSecure bool, frontendURL string) *gin.RouterGroup {
	callbackURL := os.Getenv("OIDC_REDIRECT_URL")
	if callbackURL == "" {
		port := os.Getenv("PORT")
		if port == "" {
			port = "8080"
		}
		callbackURL = "http://localhost:" + port + "/auth/callback"
	}

	sessionCfg := auth.SessionConfig{
		OAuth2: oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Endpoint:     google.Endpoint,
			RedirectURL:  callbackURL,
			Scopes:       []string{"openid", "email", "profile"},
		},
		IssuerURL:     issuer,
		SessionSecret: sessionSecret,
		CookieDomain:  cookieDomain,
		CookieSecure:  cookieSecure,
		FrontendURL:   frontendURL,
	}

	auth.RegisterAuthRoutes(r, sessionCfg, users)
	slog.Info("OIDC auth enabled", "issuer", issuer, "client_id", clientID)

	return r.Group("/api/v1", auth.SessionMiddleware(sessionCfg, users), handler.CSRFMiddleware())
}

func setupDevAuth(ctx context.Context, r *gin.Engine, pool *pgxpool.Pool, users store.UserStore, _ []byte) *gin.RouterGroup {
	devEmail := "dev@glyph.test"
	devName := "Dev User"
	if _, err := users.Upsert(ctx, "dev-user", "dev-issuer", &devEmail, &devName); err != nil {
		log.Fatalf("failed to create dev user: %v", err)
	}

	registerTestEndpoints(r, pool, users)

	devAuthMiddleware := makeDevAuthMiddleware(users, devEmail, devName)

	registerDevAuthMe(r, users, devEmail, devName)

	slog.Warn("OIDC not configured — running in DEV MODE",
		"auth", "disabled",
		"test_reset", "available",
		"WARNING", "⚠️  /test/* endpoints are enabled — do NOT expose this to the public internet")
	slog.Info("dev user configured", "name", devName, "email", devEmail)

	return r.Group("/api/v1", devAuthMiddleware, handler.CSRFMiddleware())
}

func registerTestEndpoints(r *gin.Engine, pool *pgxpool.Pool, users store.UserStore) {
	// /test/reset — truncates all tables for E2E test isolation.
	r.POST("/test/reset", func(c *gin.Context) {
		_, dbErr := pool.Exec(c.Request.Context(),
			"TRUNCATE shares, org_members, organizations, page_contents, tasks, lanes, templates, pages, users CASCADE")
		if dbErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": dbErr.Error()})
			return
		}
		aliceEmail, aliceName := "alice@test.com", "Alice"
		bobEmail, bobName := "bob@test.com", "Bob"
		alice, aErr := users.Upsert(c.Request.Context(), "e2e-alice", "dev-issuer", &aliceEmail, &aliceName)
		bob, bErr := users.Upsert(c.Request.Context(), "e2e-bob", "dev-issuer", &bobEmail, &bobName)
		if aErr != nil || bErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "seed users failed"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"status": "reset",
			"userA":  gin.H{"id": alice.ID, "email": alice.Email, "name": alice.Name},
			"userB":  gin.H{"id": bob.ID, "email": bob.Email, "name": bob.Name},
		})
	})

	// /test/become/:userId — switches the session to a different dev user (test-only).
	r.POST("/test/become/:userId", func(c *gin.Context) {
		userIDStr := c.Param("userId")
		userID, err := uuid.Parse(userIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid userId"})
			return
		}
		u, err := users.GetByID(c.Request.Context(), userID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		c.SetCookie("dev_user_id", u.ID.String(), 3600, "/", "", false, true)
		c.JSON(http.StatusOK, gin.H{"id": u.ID, "email": u.Email, "name": u.Name})
	})
}

func makeDevAuthMiddleware(users store.UserStore, devEmail, devName string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if cookieID, cookieErr := c.Cookie("dev_user_id"); cookieErr == nil {
			if uid, parseErr := uuid.Parse(cookieID); parseErr == nil {
				if u, getErr := users.GetByID(c.Request.Context(), uid); getErr == nil {
					c.Set(auth.ContextKey, u)
					c.Next()
					return
				}
			}
		}
		u, uErr := users.Upsert(c.Request.Context(), "dev-user", "dev-issuer", &devEmail, &devName)
		if uErr != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError,
				gin.H{"error": "dev auth: " + uErr.Error()})
			return
		}
		c.Set(auth.ContextKey, u)
		c.Next()
	}
}

func registerDevAuthMe(r *gin.Engine, users store.UserStore, devEmail, devName string) {
	r.GET("/auth/me", func(c *gin.Context) {
		if cookieID, cookieErr := c.Cookie("dev_user_id"); cookieErr == nil {
			if uid, parseErr := uuid.Parse(cookieID); parseErr == nil {
				if u, getErr := users.GetByID(c.Request.Context(), uid); getErr == nil {
					c.JSON(http.StatusOK, gin.H{"id": u.ID, "email": u.Email, "name": u.Name})
					return
				}
			}
		}
		u, uErr := users.Upsert(c.Request.Context(), "dev-user", "dev-issuer", &devEmail, &devName)
		if uErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "dev auth: " + uErr.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"id": u.ID, "email": devEmail, "name": devName})
	})
}

func warnPartialOIDC(oidcReady bool, issuer, clientID, clientSecret string) {
	if oidcReady {
		return
	}
	oidcVars := map[string]string{
		"OIDC_ISSUER_URL":    issuer,
		"OIDC_CLIENT_ID":     clientID,
		"OIDC_CLIENT_SECRET": clientSecret,
	}
	var set, unset []string
	for name, val := range oidcVars {
		if val != "" && !isPlaceholder(val) {
			set = append(set, name)
		} else {
			unset = append(unset, name)
		}
	}
	if len(set) > 0 && len(unset) > 0 {
		slog.Warn("OIDC partially configured, falling back to dev mode",
			"set", set,
			"missing", unset)
	}
}

func isPlaceholder(v string) bool {
	return strings.HasPrefix(v, "YOUR_") || strings.Contains(v, "YOUR_")
}
