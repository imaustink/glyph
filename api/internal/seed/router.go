package seed

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/auth"
	"github.com/glyph/api/internal/handler"
	"github.com/glyph/api/internal/model"
)

// NewRouter returns a gin router serving the /api/v1 routes the seed (and
// its tests) use, with every request authenticated as user. The handlers are
// wired as in cmd/api's newHandlers; collaborative editing is off, which
// only matters for pages that already have a live session (seeded pages are
// new).
func NewRouter(s Stores, user *model.User) *gin.Engine {
	handler.RegisterValidators()

	perms := &handler.PermissionChecker{Orgs: s.Orgs, Shares: s.Shares}
	pages := &handler.PageHandler{Pages: s.Pages, Perms: perms}
	tasks := &handler.TaskHandler{Tasks: s.Tasks, Perms: perms, Pages: s.Pages}
	folders := &handler.FolderHandler{Pages: s.Pages, Lanes: s.Lanes, Tasks: s.Tasks, Perms: perms}
	templates := &handler.TemplateHandler{Templates: s.Templates, Perms: perms}
	orgs := &handler.OrgHandler{Orgs: s.Orgs, Users: s.Users}

	r := gin.New()
	r.Use(gin.Recovery())
	g := r.Group("/api/v1", func(c *gin.Context) {
		c.Set(auth.ContextKey, user)
		c.Next()
	})

	g.GET("/orgs", orgs.ListOrgs)
	g.POST("/orgs", orgs.CreateOrg)
	g.PATCH("/orgs/:orgId", orgs.UpdateOrg)

	g.GET("/pages", pages.ListPages)
	g.POST("/pages", pages.CreatePage)
	g.GET("/pages/:id", pages.GetPage)
	g.PATCH("/pages/:id", pages.UpdatePage)
	g.GET("/pages/:id/content", pages.GetPageContent)
	g.PUT("/pages/:id/content", pages.UpsertPageContent)

	g.GET("/tasks", tasks.ListTasks)
	g.POST("/tasks", tasks.CreateTask)
	g.GET("/tasks/:id", tasks.GetTask)
	g.PATCH("/tasks/:id", tasks.UpdateTask)

	g.GET("/templates", templates.ListTemplates)
	g.POST("/templates", templates.CreateTemplate)

	g.GET("/folders/:id", folders.GetFolder)
	g.GET("/folders/:id/lanes", folders.ListFolderLanes)
	g.POST("/folders/:id/lanes", folders.CreateFolderLane)
	g.PUT("/folders/:id/lanes/:laneId", folders.UpdateFolderLane)
	g.GET("/folders/:id/tasks", folders.ListFolderTasks)
	return r
}

// client issues in-process JSON requests against a router.
type client struct {
	h http.Handler
}

func newClient(h http.Handler) *client { return &client{h: h} }

// do sends body as JSON and decodes a 2xx response into out (if non-nil).
// Any other status is an error carrying the response body.
func (c *client) do(ctx context.Context, method, path string, body, out any) error {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return fmt.Errorf("%s %s: encode body: %w", method, path, err)
		}
	}
	req := httptest.NewRequest(method, path, &buf).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, req)
	if w.Code < 200 || w.Code > 299 {
		return fmt.Errorf("%s %s: HTTP %d: %s", method, path, w.Code, bytes.TrimSpace(w.Body.Bytes()))
	}
	if out != nil {
		if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
			return fmt.Errorf("%s %s: decode response: %w", method, path, err)
		}
	}
	return nil
}

// newNodeID returns a list item identity in the editor's format (a 21
// character nanoid).
func newNodeID() string {
	const alphabet = "useandom-26T198340PX75pxJACKVERYMINDBUSHWOLF_GQZbfghjklqvwyzrict"
	b := make([]byte, 21)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand: %v", err))
	}
	for i := range b {
		b[i] = alphabet[b[i]&63]
	}
	return string(b)
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
