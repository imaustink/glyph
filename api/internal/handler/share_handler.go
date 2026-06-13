package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/auth"
	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
	"github.com/google/uuid"
)

// ShareHandler handles direct sharing and user search.
type ShareHandler struct {
	Shares    store.ShareStore
	Users     store.UserStore
	Orgs      store.OrgStore
	Pages     store.PageStore
	Tasks     store.TaskStore
	Templates store.TemplateStore
}

// GET /shares?resourceType=X&resourceId=Y
func (h *ShareHandler) ListShares(c *gin.Context) {
	user := auth.CurrentUser(c)
	rawType := c.Query("resourceType")
	rawID := c.Query("resourceId")
	if rawType == "" || rawID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "resourceType and resourceId are required"})
		return
	}
	resourceType := model.ShareResourceType(rawType)
	if !resourceType.IsValid() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resourceType"})
		return
	}
	resourceID, err := uuid.Parse(rawID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resourceId"})
		return
	}
	// Only the resource owner can list shares.
	if !h.isResourceOwner(c, resourceType, resourceID, user.ID) {
		return
	}
	shares, err := h.Shares.ListForResource(c.Request.Context(), resourceType, resourceID)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, shares)
}

// POST /shares
func (h *ShareHandler) CreateShare(c *gin.Context) {
	user := auth.CurrentUser(c)
	var body struct {
		ResourceType    string                `json:"resourceType"`
		ResourceID      string                `json:"resourceId"`
		SharedWithID    string                `json:"sharedWithId"`
		SharedWithEmail string                `json:"sharedWithEmail"`
		Permission      model.SharePermission `json:"permission"`
	}
	if !bindJSON(c, &body) {
		return
	}
	if body.ResourceType == "" || body.ResourceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "resourceType and resourceId are required"})
		return
	}
	if body.SharedWithID == "" && body.SharedWithEmail == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "sharedWithId or sharedWithEmail is required"})
		return
	}
	resourceType := model.ShareResourceType(body.ResourceType)
	if !resourceType.IsValid() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resourceType"})
		return
	}
	resourceID, err := uuid.Parse(body.ResourceID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resourceId"})
		return
	}
	if body.Permission == "" {
		body.Permission = model.SharePermissionViewer
	}
	if !body.Permission.IsValid() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid permission"})
		return
	}
	// Only the resource owner can create shares.
	if !h.isResourceOwner(c, resourceType, resourceID, user.ID) {
		return
	}

	// Resolve recipient: prefer explicit ID, fall back to email lookup.
	var sharedWithID uuid.UUID
	if body.SharedWithID != "" {
		sharedWithID, err = uuid.Parse(body.SharedWithID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid sharedWithId"})
			return
		}
		if _, err := h.Users.GetByID(c.Request.Context(), sharedWithID); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
	} else {
		recipient, err := h.Users.GetByEmail(c.Request.Context(), body.SharedWithEmail)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "no user found with that email"})
			return
		}
		sharedWithID = recipient.ID
	}

	// Prevent sharing with self.
	if sharedWithID == user.ID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot share with yourself"})
		return
	}
	share := &model.Share{
		ResourceType: resourceType,
		ResourceID:   resourceID,
		SharedByID:   user.ID,
		SharedWith:   model.ShareUser{ID: sharedWithID},
		Permission:   body.Permission,
	}
	created, err := h.Shares.Create(c.Request.Context(), share)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusCreated, created)
}

// PATCH /shares/:shareId
func (h *ShareHandler) UpdateSharePermission(c *gin.Context) {
	user := auth.CurrentUser(c)
	shareID, ok := parseUUID(c, "shareId")
	if !ok {
		return
	}
	existing, err := h.Shares.GetByID(c.Request.Context(), shareID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	// Only the original sharer (resource owner) can update the permission.
	if !h.isResourceOwner(c, existing.ResourceType, existing.ResourceID, user.ID) {
		return
	}
	var body struct {
		Permission model.SharePermission `json:"permission"`
	}
	if !bindJSON(c, &body) {
		return
	}
	updated, err := h.Shares.UpdatePermission(c.Request.Context(), shareID, body.Permission)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, updated)
}

// DELETE /shares/:shareId
func (h *ShareHandler) DeleteShare(c *gin.Context) {
	user := auth.CurrentUser(c)
	shareID, ok := parseUUID(c, "shareId")
	if !ok {
		return
	}
	existing, err := h.Shares.GetByID(c.Request.Context(), shareID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if !h.isResourceOwner(c, existing.ResourceType, existing.ResourceID, user.ID) {
		return
	}
	if err := h.Shares.Delete(c.Request.Context(), shareID); err != nil {
		internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ─── User search ──────────────────────────────────────────────────────────────

// GET /users/search?q=...
func (h *ShareHandler) SearchUsers(c *gin.Context) {
	user := auth.CurrentUser(c)
	q := c.Query("q")
	if q == "" {
		c.JSON(http.StatusOK, []model.UserSearchResult{})
		return
	}
	// Scope results to users who share an org with the current user.
	orgIDs, err := h.Orgs.GetUserOrgIDs(c.Request.Context(), user.ID)
	if err != nil {
		internalError(c, err)
		return
	}
	results, err := h.Users.Search(c.Request.Context(), q, user.ID, orgIDs, 20)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, results)
}

// ─── Share helper ─────────────────────────────────────────────────────────────

// isResourceOwner verifies userID owns resourceID. Writes 403/404 if not.
func (h *ShareHandler) isResourceOwner(c *gin.Context, resourceType model.ShareResourceType, resourceID, userID uuid.UUID) bool {
	ctx := c.Request.Context()
	var ownerID uuid.UUID
	var err error
	switch resourceType {
	case model.ShareResourcePage:
		p, e := h.Pages.GetByID(ctx, resourceID, userID)
		if e != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return false
		}
		ownerID = p.UserID
	case model.ShareResourceFolder:
		p, e := h.Pages.GetByID(ctx, resourceID, userID)
		if e != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return false
		}
		ownerID = p.UserID
	case model.ShareResourceTask:
		t, e := h.Tasks.GetByID(ctx, resourceID, userID)
		if e != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return false
		}
		ownerID = t.UserID
	case model.ShareResourceTemplate:
		tmpl, e := h.Templates.GetByID(ctx, resourceID, userID)
		if e != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return false
		}
		ownerID = tmpl.UserID
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown resourceType"})
		return false
	}
	_ = err
	if ownerID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "only the owner can manage shares"})
		return false
	}
	return true
}
