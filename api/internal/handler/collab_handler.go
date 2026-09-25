package handler

import (
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/auth"
	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
)

// CollabHandler serves the API side of realtime collaborative editing. The
// collab service (a separate Node process running Hocuspocus) owns the live
// Yjs documents; this handler tells it who may join which document, and is
// its only write path back into page_contents.
type CollabHandler struct {
	Pages store.PageStore
	Perms *PermissionChecker
	// Enabled is the server-wide kill switch (COLLAB_ENABLED).
	Enabled bool
	// ServiceToken authenticates the collab service on /internal routes.
	ServiceToken string
}

// GET /api/v1/pages/:id/collab
//
// Describes the caller's collaborative session for a page. The browser calls
// it to decide between collaborative and single-writer editing; the collab
// service calls it with the browser's forwarded cookie to authenticate and
// authorise a WebSocket connection, so both see the same answer.
func (h *CollabHandler) GetSession(c *gin.Context) {
	user := auth.CurrentUser(c)
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	// Collaborative sessions are browser-only. A bearer token (an OAuth app)
	// has no business holding a live document open, and the scope checks
	// below are written for cookie sessions.
	if currentTokenScope(c) != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "collaborative editing requires a browser session"})
		return
	}
	page, err := h.Pages.GetByID(c.Request.Context(), id, user.ID)
	if err != nil {
		notFoundOrError(c, err)
		return
	}
	if page.Type != model.NodeTypePage {
		c.JSON(http.StatusBadRequest, gin.H{"error": "only pages have content"})
		return
	}
	canWrite, err := h.Perms.WriteAllowed(c.Request.Context(), page.UserID, page.OrgID, model.ShareResourcePage, page.ID, user.ID)
	if err != nil {
		internalError(c, err)
		return
	}
	name := ""
	switch {
	case user.Name != nil && *user.Name != "":
		name = *user.Name
	case user.Email != nil:
		name = *user.Email
	}
	c.JSON(http.StatusOK, model.CollabSession{
		Enabled:  h.Enabled,
		PageID:   page.ID,
		UserID:   user.ID,
		Name:     name,
		CanWrite: canWrite,
	})
}

// ServiceAuth guards /internal/collab routes: only a request bearing the
// shared COLLAB_SERVICE_TOKEN gets through. These routes act without a user,
// so they must never be reachable with a browser credential.
func (h *CollabHandler) ServiceAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if h.ServiceToken == "" {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "collab service not configured"})
			return
		}
		got := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), []byte(h.ServiceToken)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	}
}

// PUT /internal/collab/pages/:id/snapshot
//
// The collab service writes the current state of a shared document back to
// page_contents. The content goes through the same validation and
// sanitisation as a REST write, and the same write path (history, task
// reconciliation). Stale snapshots — from an epoch that has since been
// replaced, or older than one already accepted — are refused with 409 so the
// collab service knows to evict its copy.
func (h *CollabHandler) WriteSnapshot(c *gin.Context) {
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	if !h.Enabled {
		c.JSON(http.StatusConflict, gin.H{"error": "collaborative editing is disabled", "code": "disabled"})
		return
	}
	var body model.CollabSnapshot
	if !bindJSON(c, &body) {
		return
	}
	body.PageID = id
	sanitized, err := ValidateProseMirrorContent(body.Content)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid content: " + err.Error(), "code": "invalid_content"})
		return
	}
	body.Content = sanitized

	out, err := h.Pages.WriteCollabSnapshot(c.Request.Context(), &body)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrStaleSnapshot):
			slog.Info("rejected stale collab snapshot", "page_id", id, "epoch", body.Epoch, "seq", body.UpToSeq, "reason", err)
			c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "code": "stale_snapshot"})
		case errors.Is(err, store.ErrNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		default:
			internalError(c, err)
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"revision": out.Revision})
}
