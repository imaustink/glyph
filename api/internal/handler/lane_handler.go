package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/auth"
	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
)

// LaneHandler handles lane CRUD operations.
type LaneHandler struct {
	Lanes store.LaneStore
}

// ─── Lanes ────────────────────────────────────────────────────────────────────

// GET /lanes
func (h *LaneHandler) ListLanes(c *gin.Context) {
	user := auth.CurrentUser(c)
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
	var body model.Lane
	if !bindJSON(c, &body) {
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
	c.JSON(http.StatusOK, lane)
}

// PATCH /lanes/:id
func (h *LaneHandler) UpdateLane(c *gin.Context) {
	user := auth.CurrentUser(c)
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
	req.ApplyTo(existing)
	lane, err := h.Lanes.Update(c.Request.Context(), existing)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, lane)
}

// DELETE /lanes/:id
func (h *LaneHandler) DeleteLane(c *gin.Context) {
	user := auth.CurrentUser(c)
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
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	var body model.Lane
	if !bindJSON(c, &body) {
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
