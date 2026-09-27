package handler

import (
	"errors"
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
	Perms     *PermissionChecker
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
	if !h.Perms.CanUseOrg(c, body.OrgID, user.ID) {
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
	// GetByID applies the read-access filter (owner, org member, or any
	// share), which is broader than write access — non-owners still need an
	// explicit write permission (org editor/owner role, or an editor share)
	// before they may modify someone else's template. This also enforces
	// bearer-token scope via CanWriteResource.
	if existing.UserID != user.ID {
		if !h.Perms.CanWriteResource(c, existing.UserID, existing.OrgID, model.ShareResourceTemplate, id, user.ID) {
			return
		}
	} else if !checkTokenScope(c, existing.OrgID, model.ShareResourceTemplate, true) {
		return
	}
	var req UpdateTemplateRequest
	keys, ok := bindJSONWithKeys(c, &req)
	if !ok {
		return
	}
	// An org change must be to a destination the requester may move the
	// template to (Personal: the owner only; within the token's grant).
	if dest, sent := requestedOrg(keys, req.OrgID); sent && !sameOrg(existing.OrgID, dest) &&
		!h.Perms.CanMoveToOrg(c, dest, existing.UserID, user.ID, model.ShareResourceTemplate) {
		return
	}
	tmpl, err := h.Templates.Patch(c.Request.Context(), id, existing.UserID, func(t *model.Template) error {
		req.ApplyTo(t)
		// ApplyTo can't tell an explicit null from an omitted field: null
		// clears these (move to Personal, no default folder, no trigger).
		if raw, present := keys["orgId"]; present && isJSONNull(raw) {
			t.OrgID = nil
		}
		if raw, present := keys["defaultFolderId"]; present && isJSONNull(raw) {
			t.DefaultFolderID = nil
		}
		if raw, present := keys["todoTrigger"]; present && isJSONNull(raw) {
			t.TodoTrigger = nil
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			notFoundOrError(c, err)
			return
		}
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
	// PUT replaces the template's org, so a bearer token must be granted
	// the stored org as well as the body's. Only the owner's row is
	// replaced (the store's write is gated on user_id).
	if currentTokenScope(c) != nil {
		existing, err := h.Templates.GetByID(c.Request.Context(), id, user.ID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			internalError(c, err)
			return
		}
		if err == nil && existing.UserID == user.ID &&
			!checkTokenScope(c, existing.OrgID, model.ShareResourceTemplate, true) {
			return
		}
	}
	if !checkTokenScope(c, body.OrgID, model.ShareResourceTemplate, true) {
		return
	}
	if !h.Perms.CanUseOrg(c, body.OrgID, user.ID) {
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
