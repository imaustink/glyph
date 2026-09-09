package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/auth"
	"github.com/glyph/api/internal/handler"
	"github.com/glyph/api/internal/model"
	glyphoauth "github.com/glyph/api/internal/oauth"
	"github.com/glyph/api/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// oauthServer is a dedicated, Postgres-backed integration harness for the
// OAuth delegation feature. It is deliberately separate from the shared
// Harness in harness_test.go: that harness authenticates every request via a
// header-based fake middleware (X-Test-User-ID) and never applies CSRF
// middleware, which is unsuited to testing real bearer-token verification,
// session/bearer dual-auth dispatch, and CSRF bypass behavior. oauthServer
// wires the actual production middleware chain (DualAuthMiddleware,
// BearerTokenMiddleware, CSRFMiddleware) and the actual OAuth/org/page/task
// handlers, against a real migrated Postgres database — mirroring the
// conventions of backend_postgres_test.go (testcontainers, migration glob,
// pgxpool) without touching that shared file or the Backend interface it
// implements for the rest of the suite.
type oauthServer struct {
	pool    *pgxpool.Pool
	router  *gin.Engine
	users   store.UserStore
	orgs    store.OrgStore
	pages   store.PageStore
	tasks   store.TaskStore
	clients store.OAuthClientStore
	codes   store.OAuthCodeStore
	tokens  store.OAuthTokenStore
}

const testConsentSecret = "test-only-consent-secret-do-not-use-in-prod-0123456789"

func setupOAuthServer(t *testing.T) *oauthServer {
	t.Helper()
	ctx := context.Background()

	container, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("noteboard_oauth_test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	migrations, err := filepath.Glob("../../migrations/*.up.sql")
	require.NoError(t, err)
	sort.Strings(migrations)
	for _, path := range migrations {
		sql, err := os.ReadFile(path)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, string(sql))
		require.NoError(t, err)
	}

	s := &oauthServer{
		pool:    pool,
		users:   store.NewUserStore(pool),
		orgs:    store.NewOrgStore(pool),
		pages:   store.NewPageStore(pool),
		tasks:   store.NewTaskStore(pool),
		clients: store.NewOAuthClientStore(pool),
		codes:   store.NewOAuthCodeStore(pool),
		tokens:  store.NewOAuthTokenStore(pool),
	}
	shares := store.NewShareStore(pool)

	handler.RegisterValidators()
	perms := &handler.PermissionChecker{Orgs: s.orgs, Shares: shares}

	// sessionMw mimics a cookie-authenticated human request (the harness
	// convention elsewhere in this package uses the same X-Test-User-ID
	// header trick rather than driving real OIDC/cookie machinery).
	sessionMw := func(c *gin.Context) {
		idStr := c.GetHeader("X-Test-User-ID")
		if idStr == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing X-Test-User-ID"})
			return
		}
		id, err := uuid.Parse(idStr)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid user id"})
			return
		}
		u, err := s.users.GetByID(c.Request.Context(), id)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
			return
		}
		c.Set(auth.ContextKey, u)
		c.Next()
	}
	bearerMw := glyphoauth.BearerTokenMiddleware(s.tokens, s.users)

	router := gin.New()
	gin.SetMode(gin.TestMode)

	oauthCfg := glyphoauth.Config{
		Clients:       s.clients,
		Codes:         s.codes,
		Tokens:        s.tokens,
		Users:         s.users,
		Orgs:          s.orgs,
		ConsentSecret: []byte(testConsentSecret),
	}
	// Real protocol endpoints: /oauth/token, /oauth/revoke.
	glyphoauth.RegisterOAuthRoutes(router, oauthCfg)

	// Real /api/v1 chain: dual session/bearer auth + CSRF, exactly as wired
	// in cmd/api/auth.go's setupOIDCAuth/setupDevAuth.
	apiGroup := router.Group("/api/v1", glyphoauth.DualAuthMiddleware(sessionMw, bearerMw), handler.CSRFMiddleware())
	{
		// Human-facing consent endpoints backing the SvelteKit
		// /oauth/authorize page: GET /api/v1/oauth/consent + POST
		// /api/v1/oauth/consent/decision. Registered here (not via
		// RegisterOAuthRoutes) so they inherit session auth + CSRF.
		glyphoauth.RegisterConsentRoutes(apiGroup, oauthCfg)

		orgH := &handler.OrgHandler{Orgs: s.orgs, Users: s.users}
		apiGroup.POST("/orgs", orgH.CreateOrg)
		apiGroup.GET("/orgs", orgH.ListOrgs)
		apiGroup.GET("/orgs/:orgId", orgH.GetOrg)
		apiGroup.POST("/orgs/:orgId/members", orgH.AddOrgMember)

		oauthClientH := &handler.OAuthClientHandler{Clients: s.clients, Tokens: s.tokens, Orgs: s.orgs}
		apiGroup.POST("/orgs/:orgId/oauth-clients", oauthClientH.CreateClient)
		apiGroup.GET("/orgs/:orgId/oauth-clients", oauthClientH.ListClients)
		apiGroup.GET("/orgs/:orgId/oauth-clients/:clientId", oauthClientH.GetClient)
		apiGroup.PATCH("/orgs/:orgId/oauth-clients/:clientId", oauthClientH.UpdateClient)
		apiGroup.POST("/orgs/:orgId/oauth-clients/:clientId/orgs", oauthClientH.AddClientOrg)
		apiGroup.DELETE("/orgs/:orgId/oauth-clients/:clientId/orgs/:otherOrgId", oauthClientH.RemoveClientOrg)
		apiGroup.POST("/orgs/:orgId/oauth-clients/:clientId/rotate-secret", oauthClientH.RotateSecret)
		apiGroup.POST("/orgs/:orgId/oauth-clients/:clientId/revoke", oauthClientH.RevokeClient)
		apiGroup.GET("/orgs/:orgId/oauth-clients/:clientId/tokens", oauthClientH.ListClientTokens)
		apiGroup.DELETE("/orgs/:orgId/oauth-clients/:clientId/tokens/:tokenId", oauthClientH.RevokeToken)
		apiGroup.POST("/orgs/:orgId/oauth-clients/:clientId/tokens/revoke-all", oauthClientH.RevokeAllTokens)

		pageH := &handler.PageHandler{Pages: s.pages, Perms: perms}
		apiGroup.GET("/pages", pageH.ListPages)
		apiGroup.POST("/pages", pageH.CreatePage)
		apiGroup.GET("/pages/:id", pageH.GetPage)
		apiGroup.PATCH("/pages/:id", pageH.UpdatePage)

		taskH := &handler.TaskHandler{Tasks: s.tasks, Perms: perms}
		apiGroup.GET("/tasks", taskH.ListTasks)
		apiGroup.POST("/tasks", taskH.CreateTask)
		apiGroup.PATCH("/tasks/:id", taskH.UpdateTask)
	}

	s.router = router
	return s
}

func (s *oauthServer) reset(t *testing.T) {
	t.Helper()
	_, err := s.pool.Exec(context.Background(),
		"TRUNCATE oauth_tokens, oauth_authorization_codes, oauth_client_orgs, oauth_clients, shares, org_members, organizations, page_contents, tasks, lanes, templates, pages, users CASCADE")
	require.NoError(t, err)
}

func (s *oauthServer) createUser(t *testing.T, sub, email, name string) *model.User {
	t.Helper()
	u, err := s.users.Upsert(context.Background(), sub, "test-issuer", &email, &name)
	require.NoError(t, err)
	return u
}

// ─── HTTP helpers ─────────────────────────────────────────────────────────────

type reqOpts struct {
	userID   *uuid.UUID // sets X-Test-User-ID (session auth)
	bearer   string     // sets Authorization: Bearer <token>
	omitCSRF bool       // suppress the auto-added X-Requested-With header (to test CSRF rejection)
	rawBody  []byte
	jsonVal  interface{}
}

func (s *oauthServer) doJSON(t *testing.T, method, path string, opts reqOpts) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if opts.jsonVal != nil {
		b, err := json.Marshal(opts.jsonVal)
		require.NoError(t, err)
		r = bytes.NewReader(b)
	} else if opts.rawBody != nil {
		r = bytes.NewReader(opts.rawBody)
	}
	req := httptest.NewRequest(method, path, r)
	if r != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if opts.userID != nil {
		req.Header.Set("X-Test-User-ID", opts.userID.String())
	}
	if opts.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+opts.bearer)
	}
	// CSRFMiddleware requires X-Requested-With on state-changing session
	// (cookie/header-auth) requests; a real browser client always sends it
	// automatically, so tests default to sending it too unless explicitly
	// testing CSRF rejection via omitCSRF. Bearer-authenticated requests are
	// exempt from CSRF entirely (see handler.CSRFMiddleware), so the header
	// is irrelevant for them either way.
	if opts.userID != nil && !opts.omitCSRF {
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
	}
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	return w
}

// doForm submits an application/x-www-form-urlencoded request, as required by
// the /oauth/token, /oauth/authorize (POST), and /oauth/revoke handlers,
// which read via c.PostForm rather than JSON binding.
func (s *oauthServer) doForm(t *testing.T, method, path string, form url.Values, basicUser, basicPass string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if basicUser != "" {
		req.SetBasicAuth(basicUser, basicPass)
	}
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	return w
}

func decodeOAuth[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	err := json.Unmarshal(w.Body.Bytes(), &v)
	require.NoError(t, err, "body: %s", w.Body.String())
	return v
}

// hashOpaqueToken mirrors the private oauth.hashToken so tests can locate a
// specific opaque secret's row directly in the database (e.g. to force an
// authorization code to expire without a real 60s sleep).
func hashOpaqueToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func mustParseUUID(t *testing.T, s string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(s)
	require.NoError(t, err)
	return id
}
