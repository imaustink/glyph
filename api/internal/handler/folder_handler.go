package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/auth"
	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
	"github.com/google/uuid"
)

// FolderHandler handles folder board operations: lanes and tasks scoped to a folder.
type FolderHandler struct {
	Pages store.PageStore
	Lanes store.LaneStore
	Tasks store.TaskStore
	Perms *PermissionChecker
}

// ─── GET /folders/:id/lanes ───────────────────────────────────────────────────

func (h *FolderHandler) ListFolderLanes(c *gin.Context) {
	user := auth.CurrentUser(c)
	folderID, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	folder := h.Perms.CanReadFolder(c, h.Pages, folderID, user.ID)
	if folder == nil {
		return
	}
	lanes, err := h.Lanes.ListByFolder(c.Request.Context(), folderID, user.ID)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, lanes)
}

// ─── POST /folders/:id/lanes ──────────────────────────────────────────────────

func (h *FolderHandler) CreateFolderLane(c *gin.Context) {
	user := auth.CurrentUser(c)
	folderID, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	folder := h.Perms.CanReadFolder(c, h.Pages, folderID, user.ID)
	if folder == nil {
		return
	}
	if !h.Perms.CanWriteFolder(c, folder, user.ID) {
		return
	}
	var body model.Lane
	if !bindJSON(c, &body) {
		return
	}
	body.UserID = user.ID
	body.FolderID = &folderID
	if body.FilterSet.Rules == nil {
		body.FilterSet.Rules = []model.FilterRule{}
	}
	if body.FilterSet.Conjunction == "" {
		body.FilterSet.Conjunction = model.ConjunctionAnd
	}
	if body.SortConfig.Mode == "" {
		body.SortConfig.Mode = model.SortModeAuto
	}
	lane, err := h.Lanes.Create(c.Request.Context(), &body)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusCreated, lane)
}

// ─── PUT /folders/:id/lanes/:laneId ──────────────────────────────────────────

func (h *FolderHandler) UpdateFolderLane(c *gin.Context) {
	user := auth.CurrentUser(c)
	folderID, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	laneID, ok := parseUUID(c, "laneId")
	if !ok {
		return
	}
	folder := h.Perms.CanReadFolder(c, h.Pages, folderID, user.ID)
	if folder == nil {
		return
	}
	if !h.Perms.CanWriteFolder(c, folder, user.ID) {
		return
	}
	existing, err := h.Lanes.GetByIDAndFolder(c.Request.Context(), laneID, folderID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "lane not found"})
		return
	}
	var body model.Lane
	if !bindJSON(c, &body) {
		return
	}
	existing.Title = body.Title
	existing.FilterSet = body.FilterSet
	existing.SortConfig = body.SortConfig
	existing.Order = body.Order
	if existing.FilterSet.Rules == nil {
		existing.FilterSet.Rules = []model.FilterRule{}
	}
	updated, err := h.Lanes.UpdateByIDAndFolder(c.Request.Context(), existing, folderID)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, updated)
}

// ─── DELETE /folders/:id/lanes/:laneId ───────────────────────────────────────

func (h *FolderHandler) DeleteFolderLane(c *gin.Context) {
	user := auth.CurrentUser(c)
	folderID, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	laneID, ok := parseUUID(c, "laneId")
	if !ok {
		return
	}
	folder := h.Perms.CanReadFolder(c, h.Pages, folderID, user.ID)
	if folder == nil {
		return
	}
	if !h.Perms.CanWriteFolder(c, folder, user.ID) {
		return
	}
	if err := h.Lanes.DeleteByIDAndFolder(c.Request.Context(), laneID, folderID); err != nil {
		internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ─── GET /folders/:id/tasks ───────────────────────────────────────────────────

func (h *FolderHandler) ListFolderTasks(c *gin.Context) {
	user := auth.CurrentUser(c)
	folderID, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	if h.Perms.CanReadFolder(c, h.Pages, folderID, user.ID) == nil {
		return
	}
	descendantIDs, err := h.Pages.GetDescendantIDs(c.Request.Context(), folderID)
	if err != nil {
		internalError(c, err)
		return
	}
	// Convert []uuid.UUID to the format expected by the task store.
	tasks, err := h.Tasks.ListByFolder(c.Request.Context(), folderID, descendantIDs)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, tasks)
}

// ─── GET /folders/:id ─────────────────────────────────────────────────────────

// GetFolder returns the folder metadata plus a canEdit flag for the requesting user.
func (h *FolderHandler) GetFolder(c *gin.Context) {
	user := auth.CurrentUser(c)
	folderID, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	folder := h.Perms.CanReadFolder(c, h.Pages, folderID, user.ID)
	if folder == nil {
		return
	}
	canEdit := isUserEditorOfFolder(c.Request.Context(), h.Perms, folder, user.ID)
	c.JSON(http.StatusOK, gin.H{
		"folder":  folder,
		"canEdit": canEdit,
	})
}

// isUserEditorOfFolder reports whether userID has write access to the folder
// without writing to a Gin response context.
func isUserEditorOfFolder(ctx context.Context, pc *PermissionChecker, folder *model.Page, userID uuid.UUID) bool {
	if userID == folder.UserID {
		return true
	}
	if folder.OrgID != nil && pc.Orgs != nil {
		m, err := pc.Orgs.GetMember(ctx, *folder.OrgID, userID)
		if err == nil && (m.Role == model.OrgRoleOwner || m.Role == model.OrgRoleEditor) {
			return true
		}
	}
	if pc.Shares != nil {
		s, err := pc.Shares.GetForUserAndResource(ctx, userID, model.ShareResourceFolder, folder.ID)
		if err == nil && s.Permission == model.SharePermissionEditor {
			return true
		}
	}
	return false
}
