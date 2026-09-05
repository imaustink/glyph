package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/auth"
	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
)

// PageHandler handles page CRUD and content operations.
type PageHandler struct {
	Pages store.PageStore
	Perms *PermissionChecker
}

// ─── Pages ────────────────────────────────────────────────────────────────────

// GET /pages
func (h *PageHandler) ListPages(c *gin.Context) {
	user := auth.CurrentUser(c)

	// Support optional pagination via ?limit=N&offset=N query params.
	limitStr := c.Query("limit")
	offsetStr := c.Query("offset")
	if limitStr != "" || offsetStr != "" {
		var limit, offset int
		if limitStr != "" {
			var err error
			limit, err = strconv.Atoi(limitStr)
			if err != nil || limit < 0 {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "limit must be a non-negative integer"})
				return
			}
		}
		if offsetStr != "" {
			var err error
			offset, err = strconv.Atoi(offsetStr)
			if err != nil || offset < 0 {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "offset must be a non-negative integer"})
				return
			}
		}
		pg := store.DefaultPagination(limit, offset)
		pages, total, err := h.Pages.ListByUserPaginated(c.Request.Context(), user.ID, pg)
		if err != nil {
			internalError(c, err)
			return
		}
		c.Header("X-Total-Count", strconv.Itoa(total))
		c.JSON(http.StatusOK, pages)
		return
	}

	pages, err := h.Pages.ListByUser(c.Request.Context(), user.ID)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, pages)
}

// POST /pages
func (h *PageHandler) CreatePage(c *gin.Context) {
	user := auth.CurrentUser(c)
	var body model.Page
	if !bindJSON(c, &body) {
		return
	}
	body.UserID = user.ID
	if body.Tags == nil {
		body.Tags = []string{}
	}
	// Default new pages to private so they are not visible to org members
	// until the owner explicitly shares them.
	body.IsPrivate = true
	page, err := h.Pages.Create(c.Request.Context(), &body)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusCreated, page)
}

// GET /pages/:id
func (h *PageHandler) GetPage(c *gin.Context) {
	user := auth.CurrentUser(c)
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	page, err := h.Pages.GetByID(c.Request.Context(), id, user.ID)
	if err != nil {
		notFoundOrError(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}

// PATCH /pages/:id
func (h *PageHandler) UpdatePage(c *gin.Context) {
	user := auth.CurrentUser(c)
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	existing, err := h.Pages.GetByID(c.Request.Context(), id, user.ID)
	if err != nil {
		notFoundOrError(c, err)
		return
	}
	// Non-owners need write permission.
	if existing.UserID != user.ID {
		if !h.Perms.CanWritePage(c, existing, user.ID) {
			return
		}
	}
	var req UpdatePageRequest
	keys, ok := bindJSONWithKeys(c, &req)
	if !ok {
		return
	}
	req.ApplyTo(existing)

	// ApplyTo cannot distinguish {"parentId": null} (move to the top level) from
	// an omitted parentId (leave unchanged) — both decode to a nil pointer. When
	// the client explicitly sends null, clear the parent so the node moves to root.
	if raw, present := keys["parentId"]; present && isJSONNull(raw) {
		existing.ParentID = nil
	}

	// Guard against reparenting a node into one of its own descendants (cycle).
	if existing.ParentID != nil {
		if *existing.ParentID == id {
			c.JSON(http.StatusBadRequest, gin.H{"error": "a node cannot be its own parent"})
			return
		}
		isAncestor, err := h.Pages.IsAncestor(c.Request.Context(), id, *existing.ParentID)
		if err != nil {
			internalError(c, err)
			return
		}
		if isAncestor {
			c.JSON(http.StatusBadRequest, gin.H{"error": "cannot move a node into one of its own descendants"})
			return
		}
	}

	page, err := h.Pages.Update(c.Request.Context(), existing)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}

// DELETE /pages/:id
func (h *PageHandler) DeletePage(c *gin.Context) {
	user := auth.CurrentUser(c)
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	page, err := h.Pages.GetByID(c.Request.Context(), id, user.ID)
	if err != nil {
		notFoundOrError(c, err)
		return
	}
	// Only the owner can delete.
	if page.UserID != user.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "only the owner can delete"})
		return
	}
	if err := h.Pages.Delete(c.Request.Context(), id, user.ID); err != nil {
		notFoundOrError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// PUT /pages/:id
func (h *PageHandler) UpsertPage(c *gin.Context) {
	user := auth.CurrentUser(c)
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	var body model.Page
	if !bindJSON(c, &body) {
		return
	}
	body.ID = id
	body.UserID = user.ID
	if body.Tags == nil {
		body.Tags = []string{}
	}
	if body.Type == "" {
		body.Type = model.NodeTypePage
	}
	page, err := h.Pages.Upsert(c.Request.Context(), &body)
	if err != nil {
		notFoundOrError(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}

// GET /pages/:id/content
func (h *PageHandler) GetPageContent(c *gin.Context) {
	user := auth.CurrentUser(c)
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	content, err := h.Pages.GetContent(c.Request.Context(), id, user.ID)
	if err != nil {
		notFoundOrError(c, err)
		return
	}
	c.JSON(http.StatusOK, content)
}

// PUT /pages/:id/content
func (h *PageHandler) UpsertPageContent(c *gin.Context) {
	user := auth.CurrentUser(c)
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	// Fetch the page to check access and get the owner ID.
	page, err := h.Pages.GetByID(c.Request.Context(), id, user.ID)
	if err != nil {
		notFoundOrError(c, err)
		return
	}
	if page.UserID != user.ID {
		if !h.Perms.CanWritePage(c, page, user.ID) {
			return
		}
	}
	var body model.PageContent
	if !bindJSON(c, &body) {
		return
	}
	body.PageID = id

	// Validate and sanitize ProseMirror content to prevent XSS via stored documents.
	if len(body.Content) > 0 {
		sanitized, valErr := ValidateProseMirrorContent(body.Content)
		if valErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid content: " + valErr.Error()})
			return
		}
		body.Content = sanitized
	}

	// Pass the owner's ID so the store ownership check passes.
	content, err := h.Pages.UpsertContent(c.Request.Context(), &body, page.UserID)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, content)
}
