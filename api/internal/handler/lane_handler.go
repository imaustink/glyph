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

// LaneHandler handles lane CRUD operations.
type LaneHandler struct {
	Lanes store.LaneStore
	// Pages resolves a folder-board lane's workspace (its folder's org) so the
	// bearer-token scope check in GetLane can be applied against the right org.
	Pages store.PageStore
	// Perms checks a lane's folderId is a folder the caller may write.
	// Optional: nil allows only the caller's own folders.
	Perms *PermissionChecker
}

// validLaneConfig reports whether a lane's filter and sort settings use only
// values the board understands. They are stored as JSON, so without this an
// unknown conjunction, operator, sort mode or direction was saved as-is.
// Empty conjunction/mode are allowed (they fall back to the defaults).
func validLaneConfig(fs *model.FilterSet, sc *model.SortConfig) bool {
	if fs != nil {
		switch fs.Conjunction {
		case "", model.ConjunctionAnd, model.ConjunctionOr:
		default:
			return false
		}
		for _, r := range fs.Rules {
			switch r.Operator {
			case model.FilterOpEq, model.FilterOpNeq, model.FilterOpIn, model.FilterOpNotIn,
				model.FilterOpContains, model.FilterOpBefore, model.FilterOpAfter,
				model.FilterOpAny, model.FilterOpExists, model.FilterOpNotExists:
			default:
				return false
			}
		}
	}
	if sc != nil {
		switch sc.Mode {
		case "", model.SortModeAuto, model.SortModeField, model.SortModeManual:
		default:
			return false
		}
		if sc.Direction != nil && *sc.Direction != model.SortDirectionAsc && *sc.Direction != model.SortDirectionDesc {
			return false
		}
	}
	return true
}

// checkLaneConfig writes 400 and returns false if validLaneConfig fails.
func checkLaneConfig(c *gin.Context, fs *model.FilterSet, sc *model.SortConfig) bool {
	if validLaneConfig(fs, sc) {
		return true
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": "invalid filterSet or sortConfig"})
	return false
}

// ─── Lanes ────────────────────────────────────────────────────────────────────

// GET /lanes
func (h *LaneHandler) ListLanes(c *gin.Context) {
	user := auth.CurrentUser(c)
	// Personal board lanes belong to no org: a bearer token needs lane:read
	// plus a personal-workspace grant.
	if !requireLaneReadScope(c, nil) {
		return
	}
	lanes, err := h.Lanes.ListByUser(c.Request.Context(), user.ID)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, lanes)
}

// POST /lanes
func (h *LaneHandler) CreateLane(c *gin.Context) {
	user := auth.CurrentUser(c)
	if !requireSessionAuth(c) {
		return
	}
	var body model.Lane
	if !bindJSON(c, &body) {
		return
	}
	if !checkLaneConfig(c, &body.FilterSet, &body.SortConfig) {
		return
	}
	if !h.Perms.CanUseFolder(c, h.Pages, body.FolderID, user.ID) {
		return
	}
	body.UserID = user.ID
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

// POST /lanes/batch
func (h *LaneHandler) BatchCreateLanes(c *gin.Context) {
	user := auth.CurrentUser(c)
	if !requireSessionAuth(c) {
		return
	}
	var bodies []model.Lane
	if !bindJSON(c, &bodies) {
		return
	}
	if len(bodies) == 0 {
		c.JSON(http.StatusOK, []*model.Lane{})
		return
	}
	if len(bodies) > 20 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "batch size exceeds maximum of 20"})
		return
	}
	lanes := make([]*model.Lane, 0, len(bodies))
	for i := range bodies {
		if !checkLaneConfig(c, &bodies[i].FilterSet, &bodies[i].SortConfig) {
			return
		}
		if !h.Perms.CanUseFolder(c, h.Pages, bodies[i].FolderID, user.ID) {
			return
		}
		bodies[i].UserID = user.ID
		if bodies[i].FilterSet.Rules == nil {
			bodies[i].FilterSet.Rules = []model.FilterRule{}
		}
		if bodies[i].FilterSet.Conjunction == "" {
			bodies[i].FilterSet.Conjunction = model.ConjunctionAnd
		}
		if bodies[i].SortConfig.Mode == "" {
			bodies[i].SortConfig.Mode = model.SortModeAuto
		}
		lanes = append(lanes, &bodies[i])
	}
	created, err := h.Lanes.BatchCreate(c.Request.Context(), lanes)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusCreated, created)
}

// GET /lanes/:id
func (h *LaneHandler) GetLane(c *gin.Context) {
	user := auth.CurrentUser(c)
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	lane, err := h.Lanes.GetByID(c.Request.Context(), id, user.ID)
	if err != nil {
		notFoundOrError(c, err)
		return
	}
	// Resolve the lane's workspace before the scope check: a folder-board lane
	// lives in its folder's org, so a personal-only bearer token must not read
	// it. GetByID already restricts to lanes the acting user can access, so we
	// can safely look up the folder to read its org.
	var orgID *uuid.UUID
	if lane.FolderID != nil {
		folder, err := h.Pages.GetFolderByID(c.Request.Context(), *lane.FolderID, user.ID)
		if err != nil {
			notFoundOrError(c, err)
			return
		}
		orgID = folder.OrgID
	}
	if !requireLaneReadScope(c, orgID) {
		return
	}
	c.JSON(http.StatusOK, lane)
}

// PATCH /lanes/:id
func (h *LaneHandler) UpdateLane(c *gin.Context) {
	user := auth.CurrentUser(c)
	if !requireSessionAuth(c) {
		return
	}
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	existing, err := h.Lanes.GetByID(c.Request.Context(), id, user.ID)
	if err != nil {
		notFoundOrError(c, err)
		return
	}
	var req UpdateLaneRequest
	if !bindJSON(c, &req) {
		return
	}
	if !checkLaneConfig(c, req.FilterSet, req.SortConfig) {
		return
	}
	lane, err := h.Lanes.Patch(c.Request.Context(), existing.ID, user.ID, func(l *model.Lane) error {
		req.ApplyTo(l)
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
	c.JSON(http.StatusOK, lane)
}

// DELETE /lanes/:id
func (h *LaneHandler) DeleteLane(c *gin.Context) {
	user := auth.CurrentUser(c)
	if !requireSessionAuth(c) {
		return
	}
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	if err := h.Lanes.Delete(c.Request.Context(), id, user.ID); err != nil {
		notFoundOrError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// PUT /lanes/:id
func (h *LaneHandler) UpsertLane(c *gin.Context) {
	user := auth.CurrentUser(c)
	if !requireSessionAuth(c) {
		return
	}
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	var body model.Lane
	if !bindJSON(c, &body) {
		return
	}
	if !checkLaneConfig(c, &body.FilterSet, &body.SortConfig) {
		return
	}
	if !h.Perms.CanUseFolder(c, h.Pages, body.FolderID, user.ID) {
		return
	}
	body.ID = id
	body.UserID = user.ID
	if body.FilterSet.Rules == nil {
		body.FilterSet.Rules = []model.FilterRule{}
	}
	if body.FilterSet.Conjunction == "" {
		body.FilterSet.Conjunction = model.ConjunctionAnd
	}
	if body.SortConfig.Mode == "" {
		body.SortConfig.Mode = model.SortModeAuto
	}
	lane, err := h.Lanes.Upsert(c.Request.Context(), &body)
	if err != nil {
		notFoundOrError(c, err)
		return
	}
	c.JSON(http.StatusOK, lane)
}

// PUT /lanes/reorder
func (h *LaneHandler) ReorderLanes(c *gin.Context) {
	user := auth.CurrentUser(c)
	if !requireSessionAuth(c) {
		return
	}
	var items []store.LaneReorderItem
	if !bindJSON(c, &items) {
		return
	}
	if len(items) == 0 {
		c.Status(http.StatusNoContent)
		return
	}
	if err := h.Lanes.ReorderAll(c.Request.Context(), user.ID, items); err != nil {
		internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
