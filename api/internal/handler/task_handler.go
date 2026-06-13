package handler

import (
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
		c.Header("X-Total-Count", strconv.Itoa(total))
		c.JSON(http.StatusOK, tasks)
		return
	}

	tasks, err := h.Tasks.ListByUser(c.Request.Context(), user.ID)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, tasks)
}

// POST /tasks
func (h *TaskHandler) CreateTask(c *gin.Context) {
	user := auth.CurrentUser(c)
	var body model.Task
	if !bindJSON(c, &body) {
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
	task, err := h.Tasks.Create(c.Request.Context(), &body)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusCreated, task)
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
	if existing.UserID != user.ID {
		if !h.Perms.CanWriteResource(c, existing.UserID, existing.OrgID, model.ShareResourceTask, id, user.ID) {
			return
		}
	}
	var req UpdateTaskRequest
	if !bindJSON(c, &req) {
		return
	}
	req.ApplyTo(existing)
	task, err := h.Tasks.Update(c.Request.Context(), existing)
	if err != nil {
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
		notFoundOrError(c, err)
		return
	}
	c.JSON(http.StatusOK, task)
}

// POST /tasks/filter
func (h *TaskHandler) FilterTasks(c *gin.Context) {
	user := auth.CurrentUser(c)

	var fs model.FilterSet
	if err := c.ShouldBindJSON(&fs); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid filter set: " + err.Error()})
		return
	}

	tasks, err := h.Tasks.ListByFilter(c.Request.Context(), user.ID, fs)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, tasks)
}

