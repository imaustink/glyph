package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/auth"
	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
	"github.com/google/uuid"
)

// TaskHandler handles task CRUD operations.
//
// Tasks that come from a note (sourcePageId set) belong to the note: the
// page's owner owns them whoever typed the bullet, anyone who can read the
// page can see them, and anyone who can edit the page can edit and delete
// them. Standalone tasks keep their own owner/org/share permissions.
type TaskHandler struct {
	Tasks store.TaskStore
	Perms *PermissionChecker
	// Pages resolves a task's source page. Optional only so unit tests that
	// never touch page-sourced tasks can leave it out.
	Pages store.PageStore
	// Collab, if set, is told when a note task's status changes so open
	// copies of the note update the bullet live. Best-effort: a missed
	// notification only means the indicator catches up on the next load.
	Collab store.CollabNotifier
}

// notifyStatusChange tells open copies of the note about a task's new
// status, if it changed and the task comes from a bullet.
func (h *TaskHandler) notifyStatusChange(c *gin.Context, before model.TaskStatus, task *model.Task) {
	if h.Collab == nil || task == nil || task.Status == before || task.SourcePageID == nil || task.SourceNodeID == nil {
		return
	}
	if err := h.Collab.TaskStatusChanged(c.Request.Context(), *task.SourcePageID, *task.SourceNodeID, task.Status); err != nil {
		slog.Warn("could not notify collab service of task status", "task_id", task.ID, "err", err)
	}
}

// resolveSourcePage loads the page a task is (to be) created from and checks
// that userID may edit it. On failure it writes the response and returns ok
// = false.
func (h *TaskHandler) resolveSourcePage(c *gin.Context, pageID uuid.UUID, userID uuid.UUID) (page *model.Page, ok bool) {
	if h.Pages == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return nil, false
	}
	page, err := h.Pages.GetByID(c.Request.Context(), pageID, userID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "source page not found"})
		} else {
			notFoundOrError(c, err)
		}
		return nil, false
	}
	if !h.Perms.CanWritePage(c, page, userID) {
		return nil, false
	}
	return page, true
}

// canWriteViaSourcePage reports whether userID may edit a task because they
// may edit the note it comes from. Writes no response.
func (h *TaskHandler) canWriteViaSourcePage(c *gin.Context, task *model.Task, userID uuid.UUID) (bool, error) {
	if task.SourcePageID == nil || h.Pages == nil {
		return false, nil
	}
	page, err := h.Pages.GetByID(c.Request.Context(), *task.SourcePageID, userID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return h.Perms.WriteAllowed(c.Request.Context(), page.UserID, page.OrgID, model.ShareResourcePage, page.ID, userID)
}

// authorizeTaskWrite checks that userID may modify task, writing the
// response (403/500) if not. The bearer-token scope is always checked.
func (h *TaskHandler) authorizeTaskWrite(c *gin.Context, task *model.Task, userID uuid.UUID) bool {
	if task.UserID == userID {
		return checkTokenScope(c, task.OrgID, model.ShareResourceTask, true)
	}
	viaPage, err := h.canWriteViaSourcePage(c, task, userID)
	if err != nil {
		internalError(c, err)
		return false
	}
	if viaPage {
		return checkTokenScope(c, task.OrgID, model.ShareResourceTask, true)
	}
	return h.Perms.CanWriteResource(c, task.UserID, task.OrgID, model.ShareResourceTask, task.ID, userID)
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
	if body.SourcePageID != nil {
		// A note's tasks belong to the note's owner, whoever typed the bullet —
		// and only someone who may edit the note may add tasks to it.
		page, ok := h.resolveSourcePage(c, *body.SourcePageID, user.ID)
		if !ok {
			return
		}
		body.UserID = page.UserID
	}
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
	// The existing task comes from the same note, so anyone who could edit
	// the note (checked above) can see it. Re-read it as the caller anyway,
	// and respect a bearer token's org scope.
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
	// Owners still need their bearer token's scope checked (authorizeTaskWrite
	// always does), or a token without task:write could modify any task its
	// granting user owns.
	if !h.authorizeTaskWrite(c, existing, user.ID) {
		return
	}
	var req UpdateTaskRequest
	keys, ok := bindJSONWithKeys(c, &req)
	if !ok {
		return
	}
	statusBefore := existing.Status
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
	h.notifyStatusChange(c, statusBefore, task)
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
	// The owner, or — for a note's task — anyone who may edit the note.
	if task.UserID != user.ID {
		viaPage, err := h.canWriteViaSourcePage(c, task, user.ID)
		if err != nil {
			internalError(c, err)
			return
		}
		if !viaPage {
			c.JSON(http.StatusForbidden, gin.H{"error": "only the owner can delete"})
			return
		}
	}
	if err := h.Tasks.Delete(c.Request.Context(), id, task.UserID); err != nil {
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
	if body.SourcePageID != nil {
		page, ok := h.resolveSourcePage(c, *body.SourcePageID, user.ID)
		if !ok {
			return
		}
		body.UserID = page.UserID
	}
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
	// The previous status isn't known here; the collab service ignores a
	// status the bullet already shows.
	h.notifyStatusChange(c, "", task)
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
