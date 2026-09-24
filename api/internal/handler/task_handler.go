package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/auth"
	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
	"github.com/google/uuid"
)

// TaskHandler handles task CRUD operations.
type TaskHandler struct {
	Tasks store.TaskStore
	Perms *PermissionChecker
}

// ─── Tasks ────────────────────────────────────────────────────────────────────

// GET /tasks
func (h *TaskHandler) ListTasks(c *gin.Context) {
	user := auth.CurrentUser(c)

	// Support filtering by source node.
	if nodeIDStr := c.Query("sourceNodeId"); nodeIDStr != "" {
		tasks, err := h.Tasks.ListBySourceNode(c.Request.Context(), user.ID, nodeIDStr)
		if err != nil {
			internalError(c, err)
			return
		}
		tasks = FilterByTokenScope(c, model.ShareResourceTask, tasks, func(t *model.Task) *uuid.UUID { return t.OrgID })
		c.JSON(http.StatusOK, tasks)
		return
	}

	// Support filtering by source page.
	if pageIDStr := c.Query("sourcePageId"); pageIDStr != "" {
		pageID, err := uuid.Parse(pageIDStr)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid sourcePageId"})
			return
		}
		tasks, err := h.Tasks.ListBySourcePage(c.Request.Context(), user.ID, pageID)
		if err != nil {
			internalError(c, err)
			return
		}
		tasks = FilterByTokenScope(c, model.ShareResourceTask, tasks, func(t *model.Task) *uuid.UUID { return t.OrgID })
		c.JSON(http.StatusOK, tasks)
		return
	}

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
		tasks, total, err := h.Tasks.ListByUserPaginated(c.Request.Context(), user.ID, pg)
		if err != nil {
			internalError(c, err)
			return
		}
		tasks = FilterByTokenScope(c, model.ShareResourceTask, tasks, func(t *model.Task) *uuid.UUID { return t.OrgID })
		c.Header("X-Total-Count", strconv.Itoa(total))
		c.JSON(http.StatusOK, tasks)
		return
	}

	tasks, err := h.Tasks.ListByUser(c.Request.Context(), user.ID)
	if err != nil {
		internalError(c, err)
		return
	}
	tasks = FilterByTokenScope(c, model.ShareResourceTask, tasks, func(t *model.Task) *uuid.UUID { return t.OrgID })
	c.JSON(http.StatusOK, tasks)
}

// POST /tasks
func (h *TaskHandler) CreateTask(c *gin.Context) {
	user := auth.CurrentUser(c)
	var body model.Task
	if !bindJSON(c, &body) {
		return
	}
	if !checkTokenScope(c, body.OrgID, model.ShareResourceTask, true) {
		return
	}
	if !h.Perms.CanUseOrg(c, body.OrgID, user.ID) {
		return
	}
	body.UserID = user.ID
	if body.Tags == nil {
		body.Tags = []string{}
	}
	if body.Status == "" {
		body.Status = model.StatusTodo
	}
	if body.Priority == "" {
		body.Priority = model.PriorityNone
	}
	if body.SourcePageID != nil && body.SourceNodeID != nil {
		h.createLinkedTask(c, &body)
		return
	}
	task, err := h.Tasks.Create(c.Request.Context(), &body)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			c.JSON(http.StatusConflict, gin.H{"error": "task already exists"})
			return
		}
		internalError(c, err)
		return
	}
	c.JSON(http.StatusCreated, task)
}

// createLinkedTask handles POST /tasks for a task tied to a bullet. Creation
// is idempotent per bullet: when several editors of a page all see the same
// new TODO bullet, the first request creates the task and the rest get that
// same task back (200) instead of creating duplicates.
func (h *TaskHandler) createLinkedTask(c *gin.Context, body *model.Task) {
	user := auth.CurrentUser(c)
	task, created, err := h.Tasks.CreateLinked(c.Request.Context(), body)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "code": "source_taken"})
			return
		}
		internalError(c, err)
		return
	}
	if created {
		c.JSON(http.StatusCreated, task)
		return
	}
	// Someone else's task may not be visible to this caller (it can be
	// private); in that case only say the bullet is taken.
	visible, err := h.Tasks.GetByID(c.Request.Context(), task.ID, user.ID)
	if err != nil || !scopeAllows(currentTokenScope(c), visible.OrgID, model.ShareResourceTask, false) {
		c.JSON(http.StatusConflict, gin.H{"error": "this bullet is already linked to a task", "code": "source_taken"})
		return
	}
	c.JSON(http.StatusOK, visible)
}

// GET /tasks/:id
func (h *TaskHandler) GetTask(c *gin.Context) {
	user := auth.CurrentUser(c)
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	task, err := h.Tasks.GetByID(c.Request.Context(), id, user.ID)
	if err != nil {
		notFoundOrError(c, err)
		return
	}
	if !h.Perms.CanReadResource(c, task.OrgID, model.ShareResourceTask) {
		return
	}
	c.JSON(http.StatusOK, task)
}

// PATCH /tasks/:id
func (h *TaskHandler) UpdateTask(c *gin.Context) {
	user := auth.CurrentUser(c)
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	existing, err := h.Tasks.GetByID(c.Request.Context(), id, user.ID)
	if err != nil {
		notFoundOrError(c, err)
		return
	}
	// Owners still need their bearer token's scope checked — CanWriteResource is
	// the only caller of checkTokenScope, so skipping it for the owner let a
	// token without task:write modify any task its granting user owns.
	if existing.UserID != user.ID {
		if !h.Perms.CanWriteResource(c, existing.UserID, existing.OrgID, model.ShareResourceTask, id, user.ID) {
			return
		}
	} else if !checkTokenScope(c, existing.OrgID, model.ShareResourceTask, true) {
		return
	}
	var req UpdateTaskRequest
	keys, ok := bindJSONWithKeys(c, &req)
	if !ok {
		return
	}
	if req.OrgID != nil && !h.Perms.CanUseOrg(c, req.OrgID, user.ID) {
		return
	}
	req.ApplyTo(existing)
	// ApplyTo can't tell an explicit null from an omitted field; honor
	// {"dueDate": null} / {"link": null} as "clear it", which is how the web
	// app removes a due date.
	if raw, present := keys["dueDate"]; present && isJSONNull(raw) {
		existing.DueDate = nil
	}
	if raw, present := keys["link"]; present && isJSONNull(raw) {
		existing.Link = nil
	}
	task, err := h.Tasks.Update(c.Request.Context(), existing)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			c.JSON(http.StatusConflict, gin.H{"error": "that bullet is already linked to another task", "code": "source_taken"})
			return
		}
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, task)
}

// DELETE /tasks/:id
func (h *TaskHandler) DeleteTask(c *gin.Context) {
	user := auth.CurrentUser(c)
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	task, err := h.Tasks.GetByID(c.Request.Context(), id, user.ID)
	if err != nil {
		notFoundOrError(c, err)
		return
	}
	if !checkTokenScope(c, task.OrgID, model.ShareResourceTask, true) {
		return
	}
	if task.UserID != user.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "only the owner can delete"})
		return
	}
	if err := h.Tasks.Delete(c.Request.Context(), id, user.ID); err != nil {
		notFoundOrError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// PUT /tasks/:id
func (h *TaskHandler) UpsertTask(c *gin.Context) {
	user := auth.CurrentUser(c)
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	var body model.Task
	if !bindJSON(c, &body) {
		return
	}
	if !checkTokenScope(c, body.OrgID, model.ShareResourceTask, true) {
		return
	}
	if !h.Perms.CanUseOrg(c, body.OrgID, user.ID) {
		return
	}
	body.ID = id
	body.UserID = user.ID
	if body.Tags == nil {
		body.Tags = []string{}
	}
	if body.Status == "" {
		body.Status = model.StatusTodo
	}
	if body.Priority == "" {
		body.Priority = model.PriorityNone
	}
	task, err := h.Tasks.Upsert(c.Request.Context(), &body)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			c.JSON(http.StatusConflict, gin.H{"error": "that bullet is already linked to another task", "code": "source_taken"})
			return
		}
		notFoundOrError(c, err)
		return
	}
	c.JSON(http.StatusOK, task)
}

// POST /tasks/filter
func (h *TaskHandler) FilterTasks(c *gin.Context) {
	user := auth.CurrentUser(c)

	var fs model.FilterSet
	if !bindJSON(c, &fs) {
		return
	}

	tasks, err := h.Tasks.ListByFilter(c.Request.Context(), user.ID, fs)
	if err != nil {
		internalError(c, err)
		return
	}
	tasks = FilterByTokenScope(c, model.ShareResourceTask, tasks, func(t *model.Task) *uuid.UUID { return t.OrgID })
	c.JSON(http.StatusOK, tasks)
}
