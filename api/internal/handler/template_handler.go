package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/auth"
	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
	"github.com/google/uuid"
)

// TemplateHandler handles template CRUD operations.
type TemplateHandler struct {
	Templates store.TemplateStore
}

// ─── Templates ────────────────────────────────────────────────────────────────

// GET /templates
func (h *TemplateHandler) ListTemplates(c *gin.Context) {
	user := auth.CurrentUser(c)
	templates, err := h.Templates.ListByUser(c.Request.Context(), user.ID)
	if err != nil {
		internalError(c, err)
		return
	}
	templates = FilterByTokenScope(c, model.ShareResourceTemplate, templates, func(t *model.Template) *uuid.UUID { return t.OrgID })
	c.JSON(http.StatusOK, templates)
}

// POST /templates
func (h *TemplateHandler) CreateTemplate(c *gin.Context) {
	user := auth.CurrentUser(c)
	var body model.Template
	if !bindJSON(c, &body) {
		return
	}
	if !checkTokenScope(c, body.OrgID, model.ShareResourceTemplate, true) {
		return
	}
	body.UserID = user.ID
	tmpl, err := h.Templates.Create(c.Request.Context(), &body)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusCreated, tmpl)
}

// GET /templates/:id
func (h *TemplateHandler) GetTemplate(c *gin.Context) {
	user := auth.CurrentUser(c)
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	tmpl, err := h.Templates.GetByID(c.Request.Context(), id, user.ID)
	if err != nil {
		notFoundOrError(c, err)
		return
	}
	if !checkTokenScope(c, tmpl.OrgID, model.ShareResourceTemplate, false) {
		return
	}
	c.JSON(http.StatusOK, tmpl)
}

// PATCH /templates/:id
func (h *TemplateHandler) UpdateTemplate(c *gin.Context) {
	user := auth.CurrentUser(c)
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	existing, err := h.Templates.GetByID(c.Request.Context(), id, user.ID)
	if err != nil {
		notFoundOrError(c, err)
		return
	}
	var req UpdateTemplateRequest
	if !bindJSON(c, &req) {
		return
	}
	req.ApplyTo(existing)
	tmpl, err := h.Templates.Update(c.Request.Context(), existing)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, tmpl)
}

// DELETE /templates/:id
func (h *TemplateHandler) DeleteTemplate(c *gin.Context) {
	user := auth.CurrentUser(c)
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	if scope := currentTokenScope(c); scope != nil {
		existing, err := h.Templates.GetByID(c.Request.Context(), id, user.ID)
		if err != nil {
			notFoundOrError(c, err)
			return
		}
		if !checkTokenScope(c, existing.OrgID, model.ShareResourceTemplate, true) {
			return
		}
	}
	if err := h.Templates.Delete(c.Request.Context(), id, user.ID); err != nil {
		notFoundOrError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// PUT /templates/:id
func (h *TemplateHandler) UpsertTemplate(c *gin.Context) {
	user := auth.CurrentUser(c)
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	var body model.Template
	if !bindJSON(c, &body) {
		return
	}
	if !checkTokenScope(c, body.OrgID, model.ShareResourceTemplate, true) {
		return
	}
	body.ID = id
	body.UserID = user.ID
	tmpl, err := h.Templates.Upsert(c.Request.Context(), &body)
	if err != nil {
		notFoundOrError(c, err)
		return
	}
	c.JSON(http.StatusOK, tmpl)
}
