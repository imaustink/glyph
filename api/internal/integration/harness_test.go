package integration

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/auth"
	"github.com/glyph/api/internal/handler"
	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// ─── Backend interface ────────────────────────────────────────────────────────

// Backend abstracts the setup/teardown of a particular storage layer so the
// same test specs can run against multiple implementations.
type Backend interface {
	// Name returns a short, human-readable label (e.g. "postgres", "memstore").
	Name() string

	// Setup is called once before any tests run. It may start containers,
	// apply migrations, etc. It returns all seven store implementations.
	Setup(t *testing.T) (
		users store.UserStore,
		pages store.PageStore,
		tasks store.TaskStore,
		lanes store.LaneStore,
		templates store.TemplateStore,
		orgs store.OrgStore,
		shares store.ShareStore,
	)

	// Reset is called before every individual test to clear data and seed
	// two test users (Alice and Bob).
	Reset(t *testing.T)

	// Teardown is called after all tests have finished for this backend.
	Teardown()
}

// ─── Harness ──────────────────────────────────────────────────────────────────

// Harness holds the state shared across all specs within one backend run.
type Harness struct {
	Backend    Backend
	Router     *gin.Engine
	UserStore  store.UserStore
	OrgStore   store.OrgStore
	ShareStore store.ShareStore
	UserA      *model.User
	UserB      *model.User
}

// NewHarness wires up a Gin router with a test auth middleware and all the
// store-backed routes, using the stores provided by the backend.
func NewHarness(t *testing.T, b Backend) *Harness {
	t.Helper()

	users, pages, tasks, lanes, templates, orgs, shares := b.Setup(t)

	// Register custom validators (must be done once before binding)
	handler.RegisterValidators()

	perms := &handler.PermissionChecker{Orgs: orgs, Shares: shares}

	pageH := &handler.PageHandler{Pages: pages, Perms: perms}
	taskH := &handler.TaskHandler{Tasks: tasks, Perms: perms}
	laneH := &handler.LaneHandler{Lanes: lanes}
	tmplH := &handler.TemplateHandler{Templates: templates}
	orgH := &handler.OrgHandler{Orgs: orgs, Users: users}
	shareH := &handler.ShareHandler{
		Shares:    shares,
		Users:     users,
		Orgs:      orgs,
		Pages:     pages,
		Tasks:     tasks,
		Templates: templates,
	}

	router := gin.New()
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	harness := &Harness{
		Backend:    b,
		Router:     router,
		UserStore:  users,
		OrgStore:   orgs,
		ShareStore: shares,
	}

	api := router.Group("/api/v1", harness.testAuthMiddleware())
	{
		api.GET("/pages", pageH.ListPages)
		api.POST("/pages", pageH.CreatePage)
		api.GET("/pages/:id", pageH.GetPage)
		api.PATCH("/pages/:id", pageH.UpdatePage)
		api.PUT("/pages/:id", pageH.UpsertPage)
		api.DELETE("/pages/:id", pageH.DeletePage)
		api.GET("/pages/:id/content", pageH.GetPageContent)
		api.PUT("/pages/:id/content", pageH.UpsertPageContent)

		api.GET("/tasks", taskH.ListTasks)
		api.POST("/tasks", taskH.CreateTask)
		api.POST("/tasks/filter", taskH.FilterTasks)
		api.GET("/tasks/:id", taskH.GetTask)
		api.PATCH("/tasks/:id", taskH.UpdateTask)
		api.PUT("/tasks/:id", taskH.UpsertTask)
		api.DELETE("/tasks/:id", taskH.DeleteTask)

		api.GET("/lanes", laneH.ListLanes)
		api.POST("/lanes", laneH.CreateLane)
		api.POST("/lanes/batch", laneH.BatchCreateLanes)
		api.PUT("/lanes/reorder", laneH.ReorderLanes)
		api.GET("/lanes/:id", laneH.GetLane)
		api.PATCH("/lanes/:id", laneH.UpdateLane)
		api.PUT("/lanes/:id", laneH.UpsertLane)
		api.DELETE("/lanes/:id", laneH.DeleteLane)

		api.GET("/templates", tmplH.ListTemplates)
		api.POST("/templates", tmplH.CreateTemplate)
		api.GET("/templates/:id", tmplH.GetTemplate)
		api.PATCH("/templates/:id", tmplH.UpdateTemplate)
		api.PUT("/templates/:id", tmplH.UpsertTemplate)
		api.DELETE("/templates/:id", tmplH.DeleteTemplate)

		api.GET("/orgs", orgH.ListOrgs)
		api.POST("/orgs", orgH.CreateOrg)
		api.GET("/orgs/:orgId", orgH.GetOrg)
		api.PATCH("/orgs/:orgId", orgH.UpdateOrg)
		api.DELETE("/orgs/:orgId", orgH.DeleteOrg)
		api.POST("/orgs/:orgId/members", orgH.AddOrgMember)
		api.PATCH("/orgs/:orgId/members/:userId", orgH.UpdateOrgMemberRole)
		api.DELETE("/orgs/:orgId/members/:userId", orgH.RemoveOrgMember)

		api.GET("/shares", shareH.ListShares)
		api.POST("/shares", shareH.CreateShare)
		api.PATCH("/shares/:shareId", shareH.UpdateSharePermission)
		api.DELETE("/shares/:shareId", shareH.DeleteShare)

		api.GET("/users/search", shareH.SearchUsers)
	}

	return harness
}

func (h *Harness) testAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		userIDStr := c.GetHeader("X-Test-User-ID")
		if userIDStr == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing X-Test-User-ID"})
			return
		}
		id, err := uuid.Parse(userIDStr)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid user ID"})
			return
		}
		user, err := h.UserStore.GetByID(c.Request.Context(), id)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
			return
		}
		c.Set(auth.ContextKey, user)
		c.Next()
	}
}

// ResetDB delegates to the backend's Reset and refreshes the test users.
func (h *Harness) ResetDB(t *testing.T) {
	t.Helper()
	h.Backend.Reset(t)

	emailA, nameA := "alice@test.com", "Alice"
	var err error
	h.UserA, err = h.UserStore.Upsert(t.Context(), "sub-alice", "test-issuer", &emailA, &nameA)
	require.NoError(t, err)

	emailB, nameB := "bob@test.com", "Bob"
	h.UserB, err = h.UserStore.Upsert(t.Context(), "sub-bob", "test-issuer", &emailB, &nameB)
	require.NoError(t, err)
}

// ─── HTTP helpers ─────────────────────────────────────────────────────────────

func (h *Harness) Do(t *testing.T, method, path string, body interface{}, userID uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-Test-User-ID", userID.String())
	w := httptest.NewRecorder()
	h.Router.ServeHTTP(w, req)
	return w
}

func (h *Harness) DoNoAuth(t *testing.T, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	w := httptest.NewRecorder()
	h.Router.ServeHTTP(w, req)
	return w
}

func (h *Harness) DoRaw(t *testing.T, method, path string, rawBody []byte, userID uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(rawBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User-ID", userID.String())
	w := httptest.NewRecorder()
	h.Router.ServeHTTP(w, req)
	return w
}

func Decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	err := json.Unmarshal(w.Body.Bytes(), &v)
	require.NoError(t, err)
	return v
}
