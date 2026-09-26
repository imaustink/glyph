package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/auth"
	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
	"github.com/google/uuid"
)

// PageHandler handles page CRUD and content operations.
type PageHandler struct {
	Pages store.PageStore
	Perms *PermissionChecker
	// CollabEnabled mirrors CollabHandler.Enabled. While collaborative
	// editing is on, REST content writes to a page attached to a live
	// session are refused; while it is off they detach the page instead.
	CollabEnabled bool
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
		pages = FilterByTokenScope(c, model.ShareResourcePage, pages, func(p *model.Page) *uuid.UUID { return p.OrgID })
		c.Header("X-Total-Count", strconv.Itoa(total))
		c.JSON(http.StatusOK, pages)
		return
	}

	pages, err := h.Pages.ListByUser(c.Request.Context(), user.ID)
	if err != nil {
		internalError(c, err)
		return
	}
	pages = FilterByTokenScope(c, model.ShareResourcePage, pages, func(p *model.Page) *uuid.UUID { return p.OrgID })
	c.JSON(http.StatusOK, pages)
}

// POST /pages
func (h *PageHandler) CreatePage(c *gin.Context) {
	user := auth.CurrentUser(c)
	var body model.Page
	if !bindJSON(c, &body) {
		return
	}
	if !checkTokenScope(c, body.OrgID, model.ShareResourcePage, true) {
		return
	}
	if !h.Perms.CanUseOrg(c, body.OrgID, user.ID) {
		return
	}
	if !h.Perms.CanUseParent(c, h.Pages, body.ParentID, user.ID) {
		return
	}
	body.UserID = user.ID
	if body.Tags == nil {
		body.Tags = []string{}
	}
	// Default the node type, as UpsertPage already does. Without this an
	// omitted type reached Postgres as '' and tripped pages_type_check,
	// surfacing a client mistake as a 500.
	if body.Type == "" {
		body.Type = model.NodeTypePage
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
	if !h.Perms.CanReadResource(c, page.OrgID, model.ShareResourcePage) {
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
	// Non-owners need write permission. Owners still need their bearer token's
	// scope checked — CanWritePage is the only caller of checkTokenScope, so
	// skipping it for the owner let a token without page:write (or scoped to a
	// different org) modify any page its granting user owns.
	if existing.UserID != user.ID {
		if !h.Perms.CanWritePage(c, existing, user.ID) {
			return
		}
	} else if !checkTokenScope(c, existing.OrgID, model.ShareResourcePage, true) {
		return
	}
	var req UpdatePageRequest
	keys, ok := bindJSONWithKeys(c, &req)
	if !ok {
		return
	}
	if req.Type != nil && *req.Type != existing.Type {
		typeImmutable(c)
		return
	}
	if req.OrgID != nil && !h.Perms.CanUseOrg(c, req.OrgID, user.ID) {
		return
	}
	// Remember the pre-update parent so we only re-validate on an actual move.
	originalParentID := existing.ParentID

	req.ApplyTo(existing)

	// ApplyTo cannot distinguish {"parentId": null} (move to the top level) from
	// an omitted parentId (leave unchanged) — both decode to a nil pointer. When
	// the client explicitly sends null, clear the parent so the node moves to root.
	if raw, present := keys["parentId"]; present && isJSONNull(raw) {
		existing.ParentID = nil
	}

	// A reparent must land somewhere the requester can actually write.
	if existing.ParentID != nil && (originalParentID == nil || *existing.ParentID != *originalParentID) {
		if !h.Perms.CanUseParent(c, h.Pages, existing.ParentID, user.ID) {
			return
		}
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

	// Write only the fields the request sent, so a concurrent PATCH of other
	// fields isn't undone by this one writing back its stale copy (DI-05).
	fields := make([]string, 0, len(keys))
	for k := range keys {
		fields = append(fields, k)
	}
	page, err := h.Pages.UpdateFields(c.Request.Context(), existing, fields)
	if err != nil {
		// The store re-checks for a cycle under the tree-move lock, which
		// catches a concurrent move the check above could not see.
		if errors.Is(err, store.ErrCycle) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "cannot move a node into one of its own descendants"})
			return
		}
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
	if !checkTokenScope(c, page.OrgID, model.ShareResourcePage, true) {
		return
	}
	// Only the owner can delete.
	if page.UserID != user.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "only the owner can delete"})
		return
	}
	if err := h.Pages.Delete(c.Request.Context(), id, user.ID); err != nil {
		if errors.Is(err, store.ErrSubtreeNotOwned) {
			// Deleting would cascade to pages other users created inside
			// this folder. They must move or delete their pages first.
			c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "code": "subtree_has_other_owners"})
			return
		}
		notFoundOrError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// PUT /pages/:id
//
// Creates the page, or replaces the fields the body contains. A field the
// body omits keeps its stored value (on create: its default, and isPrivate
// defaults to true as with POST); an explicit value, including null,
// replaces it. PUT used to reset every omitted field, so a client that
// didn't send isPrivate made a private page visible to its whole org.
func (h *PageHandler) UpsertPage(c *gin.Context) {
	user := auth.CurrentUser(c)
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	var body model.Page
	keys, ok := bindJSONWithKeys(c, &body)
	if !ok {
		return
	}
	existing, err := h.Pages.GetByID(c.Request.Context(), id, user.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		internalError(c, err)
		return
	}
	if existing != nil && existing.UserID != user.ID {
		// Only the owner can replace a page (the store's write is gated on
		// user_id); don't merge from, or validate against, someone else's.
		existing = nil
	}
	if existing != nil {
		keepOmittedPageFields(&body, existing, keys)
		if !checkTokenScope(c, existing.OrgID, model.ShareResourcePage, true) {
			return
		}
	} else if _, sent := keys["isPrivate"]; !sent {
		body.IsPrivate = true
	}
	if !checkTokenScope(c, body.OrgID, model.ShareResourcePage, true) {
		return
	}
	// A caller-supplied orgId must belong to an org the requester is
	// actually a member of — otherwise this (Upsert can both create and
	// update) would let any user plant a brand-new page inside an org they
	// don't belong to, visible to that org once shared non-privately.
	if !sameUUID(body.OrgID, orgIDOf(existing)) && !h.Perms.CanUseOrg(c, body.OrgID, user.ID) {
		return
	}
	if !sameUUID(body.ParentID, parentIDOf(existing)) && !h.Perms.CanUseParent(c, h.Pages, body.ParentID, user.ID) {
		return
	}
	body.ID = id
	body.UserID = user.ID
	if body.Tags == nil {
		body.Tags = []string{}
	}
	if body.Type == "" {
		body.Type = model.NodeTypePage
		if existing != nil {
			body.Type = existing.Type
		}
	}
	// The type is fixed at creation. The store never rewrites it either; this
	// turns an attempt into a clear 400 instead of a silently ignored field.
	if existing != nil && body.Type != existing.Type {
		typeImmutable(c)
		return
	}
	page, err := h.Pages.Upsert(c.Request.Context(), &body)
	if err != nil {
		// PUT shares PATCH's cycle check; the store runs it in the writing
		// transaction under the tree-move lock.
		if errors.Is(err, store.ErrCycle) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "cannot move a node into one of its own descendants"})
			return
		}
		notFoundOrError(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}

// keepOmittedPageFields copies onto body every field of existing that the
// PUT body did not mention (keys are the body's top-level JSON keys).
func keepOmittedPageFields(body, existing *model.Page, keys map[string]json.RawMessage) {
	omitted := func(k string) bool { _, sent := keys[k]; return !sent }
	if omitted("type") {
		body.Type = existing.Type
	}
	if omitted("title") {
		body.Title = existing.Title
	}
	if omitted("parentId") {
		body.ParentID = existing.ParentID
	}
	if omitted("order") {
		body.Order = existing.Order
	}
	if omitted("tags") {
		body.Tags = existing.Tags
	}
	if omitted("priority") {
		body.Priority = existing.Priority
	}
	if omitted("todoTrigger") {
		body.TodoTrigger = existing.TodoTrigger
	}
	if omitted("orgId") {
		body.OrgID = existing.OrgID
	}
	if omitted("isPrivate") {
		body.IsPrivate = existing.IsPrivate
	}
}

func orgIDOf(p *model.Page) *uuid.UUID {
	if p == nil {
		return nil
	}
	return p.OrgID
}

func parentIDOf(p *model.Page) *uuid.UUID {
	if p == nil {
		return nil
	}
	return p.ParentID
}

func sameUUID(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// typeImmutable rejects a request that would turn a page into a folder or
// back. Shares are typed ('page' vs 'folder'), so a changed type made the
// existing shares impossible to list or revoke.
func typeImmutable(c *gin.Context) {
	c.JSON(http.StatusBadRequest, gin.H{"error": store.ErrTypeImmutable.Error(), "code": "type_immutable"})
}

// GET /pages/:id/content
func (h *PageHandler) GetPageContent(c *gin.Context) {
	user := auth.CurrentUser(c)
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	// Fetch the page first so we can check bearer-token scope against its
	// org — GetContent alone enforces the read-access filter (owner/org/
	// share) but has no notion of OAuth scope, so a token restricted to a
	// different org could otherwise read this page's full body.
	page, err := h.Pages.GetByID(c.Request.Context(), id, user.ID)
	if err != nil {
		notFoundOrError(c, err)
		return
	}
	if !h.Perms.CanReadResource(c, page.OrgID, model.ShareResourcePage) {
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
	} else if !checkTokenScope(c, page.OrgID, model.ShareResourcePage, true) {
		return
	}
	var body model.PageContent
	if !bindJSON(c, &body) {
		return
	}
	body.PageID = id
	body.DetachCollab = !h.CollabEnabled

	// A missing (or null) document is a client bug, not "clear the page":
	// the store would write an empty doc and reconcile would soft-delete
	// every task on the page. Clearing a page means sending an empty doc.
	if len(body.Content) == 0 || isJSONNull(body.Content) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "content is required"})
		return
	}

	// Validate and sanitize ProseMirror content to prevent XSS via stored documents.
	sanitized, valErr := ValidateProseMirrorContent(body.Content)
	if valErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid content: " + valErr.Error()})
		return
	}
	body.Content = sanitized

	// The store re-checks write permission inside the writing transaction (and
	// under a row lock), so a permission revoked between the check above and
	// the write is caught. It also enforces the optimistic-concurrency
	// precondition, surfacing a stale write as ErrConflict → 409 rather than
	// silently overwriting newer content.
	content, err := h.Pages.UpsertContent(c.Request.Context(), &body, user.ID)
	if err != nil {
		// Map the store's sentinels explicitly and keep 500 for anything
		// unexpected — notFoundOrError's catch-all 404 would disguise a real
		// database failure as a missing page on a write endpoint.
		switch {
		case errors.Is(err, store.ErrCollaborative):
			// The client must switch to the collaborative editor; retrying
			// the whole-document write can never succeed.
			c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "code": "collaborative"})
		case errors.Is(err, store.ErrConflict):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "code": "stale_revision"})
		case errors.Is(err, store.ErrForbidden):
			c.JSON(http.StatusForbidden, gin.H{"error": "write access denied"})
		case errors.Is(err, store.ErrNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		default:
			internalError(c, err)
		}
		return
	}
	c.JSON(http.StatusOK, content)
}

// POST /pages/:id/content/versions/:versionId/restore
//
// Makes a superseded revision current again. The revision it replaces is
// archived first, so a restore can itself be undone. Any collaborative
// session on the page is ended: connected clients are evicted and reload the
// restored document rather than merging their now-replaced state back in.
func (h *PageHandler) RestorePageContentVersion(c *gin.Context) {
	user := auth.CurrentUser(c)
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	versionID, err := strconv.ParseInt(c.Param("versionId"), 10, 64)
	if err != nil || versionID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid version id"})
		return
	}
	page, err := h.Pages.GetByID(c.Request.Context(), id, user.ID)
	if err != nil {
		notFoundOrError(c, err)
		return
	}
	if page.UserID != user.ID {
		if !h.Perms.CanWritePage(c, page, user.ID) {
			return
		}
	} else if !checkTokenScope(c, page.OrgID, model.ShareResourcePage, true) {
		return
	}
	content, err := h.Pages.RestoreContentVersion(c.Request.Context(), id, versionID, user.ID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrForbidden):
			c.JSON(http.StatusForbidden, gin.H{"error": "write access denied"})
		case errors.Is(err, store.ErrNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		default:
			internalError(c, err)
		}
		return
	}
	c.JSON(http.StatusOK, content)
}

// GET /pages/:id/content/versions
//
// Superseded revisions of a page's content, newest first. Exists so an
// unintended overwrite is recoverable in-product instead of requiring a
// database point-in-time restore.
func (h *PageHandler) ListPageContentVersions(c *gin.Context) {
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
	if !h.Perms.CanReadResource(c, page.OrgID, model.ShareResourcePage) {
		return
	}
	versions, err := h.Pages.ListContentVersions(c.Request.Context(), id, user.ID, 0)
	if err != nil {
		notFoundOrError(c, err)
		return
	}
	c.JSON(http.StatusOK, versions)
}
