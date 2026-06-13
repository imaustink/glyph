package integration

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/glyph/api/internal/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestLane(t *testing.T, h *Harness, userID uuid.UUID, title string) model.Lane {
	t.Helper()
	body := map[string]interface{}{"title": title}
	w := h.Do(t, "POST", "/api/v1/lanes", body, userID)
	require.Equal(t, http.StatusCreated, w.Code)
	return Decode[model.Lane](t, w)
}

func TestLanes(t *testing.T) {
	RunSpecs(t, map[string]func(t *testing.T, h *Harness){
		// ── Create ────────────────────────────────────────────────────────
		"CreateLane": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "POST", "/api/v1/lanes", map[string]interface{}{"title": "Todo"}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			lane := Decode[model.Lane](t, w)
			assert.Equal(t, "Todo", lane.Title)
			assert.Equal(t, h.UserA.ID, lane.UserID)
			assert.NotEqual(t, uuid.Nil, lane.ID)
			assert.False(t, lane.CreatedAt.IsZero())
		},

		"CreateLaneDefaults": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "POST", "/api/v1/lanes", map[string]interface{}{"title": "Default Lane"}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			lane := Decode[model.Lane](t, w)
			assert.Equal(t, model.ConjunctionAnd, lane.FilterSet.Conjunction)
			assert.Equal(t, []model.FilterRule{}, lane.FilterSet.Rules)
			assert.Equal(t, model.SortModeAuto, lane.SortConfig.Mode)
			assert.Equal(t, 0, lane.Order)
		},

		"CreateLaneWithFilterRules": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			body := map[string]interface{}{
				"title": "Filtered Lane",
				"filterSet": map[string]interface{}{
					"conjunction": "and",
					"rules": []map[string]interface{}{
						{"id": "rule-1", "field": "status", "operator": "eq", "value": "todo"},
						{"id": "rule-2", "field": "priority", "operator": "in", "value": []string{"high", "urgent"}},
					},
				},
			}
			w := h.Do(t, "POST", "/api/v1/lanes", body, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			lane := Decode[model.Lane](t, w)
			assert.Equal(t, model.ConjunctionAnd, lane.FilterSet.Conjunction)
			require.Len(t, lane.FilterSet.Rules, 2)
			assert.Equal(t, "status", lane.FilterSet.Rules[0].Field)
			assert.Equal(t, model.FilterOpEq, lane.FilterSet.Rules[0].Operator)
		},

		"CreateLaneWithOrConjunction": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			body := map[string]interface{}{
				"title": "Or Lane",
				"filterSet": map[string]interface{}{
					"conjunction": "or",
					"rules":       []map[string]interface{}{{"id": "r1", "field": "status", "operator": "eq", "value": "done"}},
				},
			}
			w := h.Do(t, "POST", "/api/v1/lanes", body, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			assert.Equal(t, model.ConjunctionOr, Decode[model.Lane](t, w).FilterSet.Conjunction)
		},

		"CreateLaneWithFieldSort": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			body := map[string]interface{}{
				"title":      "Sorted Lane",
				"sortConfig": map[string]interface{}{"mode": "field", "field": "dueDate", "direction": "asc"},
			}
			w := h.Do(t, "POST", "/api/v1/lanes", body, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			lane := Decode[model.Lane](t, w)
			assert.Equal(t, model.SortModeField, lane.SortConfig.Mode)
			require.NotNil(t, lane.SortConfig.Field)
			assert.Equal(t, "dueDate", *lane.SortConfig.Field)
			require.NotNil(t, lane.SortConfig.Direction)
			assert.Equal(t, model.SortDirectionAsc, *lane.SortConfig.Direction)
		},

		"CreateLaneWithManualSort": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			body := map[string]interface{}{
				"title":      "Manual Lane",
				"sortConfig": map[string]interface{}{"mode": "manual", "taskOrder": []string{"t1", "t2", "t3"}},
			}
			w := h.Do(t, "POST", "/api/v1/lanes", body, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			lane := Decode[model.Lane](t, w)
			assert.Equal(t, model.SortModeManual, lane.SortConfig.Mode)
			assert.Equal(t, []string{"t1", "t2", "t3"}, lane.SortConfig.TaskOrder)
		},

		"CreateLaneWithOrder": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "POST", "/api/v1/lanes", map[string]interface{}{"title": "Ordered", "order": 5}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			assert.Equal(t, 5, Decode[model.Lane](t, w).Order)
		},

		// ── List ──────────────────────────────────────────────────────────
		"ListLanesEmpty": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "GET", "/api/v1/lanes", nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			assert.Len(t, Decode[[]model.Lane](t, w), 0)
		},

		"ListLanes": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			createTestLane(t, h, h.UserA.ID, "Todo")
			createTestLane(t, h, h.UserA.ID, "In Progress")
			createTestLane(t, h, h.UserA.ID, "Done")
			w := h.Do(t, "GET", "/api/v1/lanes", nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			assert.Len(t, Decode[[]model.Lane](t, w), 3)
		},

		"ListLanesOrdering": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			for i, title := range []string{"Third", "First", "Second"} {
				orders := []int{3, 1, 2}
				h.Do(t, "POST", "/api/v1/lanes", map[string]interface{}{"title": title, "order": orders[i]}, h.UserA.ID)
			}
			w := h.Do(t, "GET", "/api/v1/lanes", nil, h.UserA.ID)
			lanes := Decode[[]model.Lane](t, w)
			require.Len(t, lanes, 3)
			assert.Equal(t, "First", lanes[0].Title)
			assert.Equal(t, "Second", lanes[1].Title)
			assert.Equal(t, "Third", lanes[2].Title)
		},

		// ── Get ───────────────────────────────────────────────────────────
		"GetLane": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			created := createTestLane(t, h, h.UserA.ID, "Get Me")
			w := h.Do(t, "GET", fmt.Sprintf("/api/v1/lanes/%s", created.ID), nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, "Get Me", Decode[model.Lane](t, w).Title)
		},

		"GetLaneNotFound": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "GET", fmt.Sprintf("/api/v1/lanes/%s", uuid.New()), nil, h.UserA.ID)
			assert.Equal(t, http.StatusNotFound, w.Code)
		},

		// ── Update ────────────────────────────────────────────────────────
		"UpdateLaneTitle": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			created := createTestLane(t, h, h.UserA.ID, "Old Title")
			w := h.Do(t, "PATCH", fmt.Sprintf("/api/v1/lanes/%s", created.ID),
				map[string]interface{}{"title": "New Title"}, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, "New Title", Decode[model.Lane](t, w).Title)
		},

		"UpdateLaneFilterSet": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			created := createTestLane(t, h, h.UserA.ID, "Filter Lane")
			body := map[string]interface{}{
				"filterSet": map[string]interface{}{
					"conjunction": "or",
					"rules":       []map[string]interface{}{{"id": "r1", "field": "tags", "operator": "contains", "value": "important"}},
				},
			}
			w := h.Do(t, "PATCH", fmt.Sprintf("/api/v1/lanes/%s", created.ID), body, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			lane := Decode[model.Lane](t, w)
			assert.Equal(t, model.ConjunctionOr, lane.FilterSet.Conjunction)
			require.Len(t, lane.FilterSet.Rules, 1)
		},

		"UpdateLaneSortConfig": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			created := createTestLane(t, h, h.UserA.ID, "Sort Lane")
			body := map[string]interface{}{
				"sortConfig": map[string]interface{}{"mode": "field", "field": "priority", "direction": "desc"},
			}
			w := h.Do(t, "PATCH", fmt.Sprintf("/api/v1/lanes/%s", created.ID), body, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			lane := Decode[model.Lane](t, w)
			assert.Equal(t, model.SortModeField, lane.SortConfig.Mode)
			require.NotNil(t, lane.SortConfig.Direction)
			assert.Equal(t, model.SortDirectionDesc, *lane.SortConfig.Direction)
		},

		"UpdateLaneNotFound": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "PATCH", fmt.Sprintf("/api/v1/lanes/%s", uuid.New()),
				map[string]interface{}{"title": "Nope"}, h.UserA.ID)
			assert.Equal(t, http.StatusNotFound, w.Code)
		},

		// ── Delete ────────────────────────────────────────────────────────
		"DeleteLane": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			created := createTestLane(t, h, h.UserA.ID, "Delete Me")
			w := h.Do(t, "DELETE", fmt.Sprintf("/api/v1/lanes/%s", created.ID), nil, h.UserA.ID)
			assert.Equal(t, http.StatusNoContent, w.Code)
			w = h.Do(t, "GET", fmt.Sprintf("/api/v1/lanes/%s", created.ID), nil, h.UserA.ID)
			assert.Equal(t, http.StatusNotFound, w.Code)
		},

		"DeleteLaneReducesList": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			l1 := createTestLane(t, h, h.UserA.ID, "Lane 1")
			createTestLane(t, h, h.UserA.ID, "Lane 2")
			h.Do(t, "DELETE", fmt.Sprintf("/api/v1/lanes/%s", l1.ID), nil, h.UserA.ID)
			w := h.Do(t, "GET", "/api/v1/lanes", nil, h.UserA.ID)
			lanes := Decode[[]model.Lane](t, w)
			assert.Len(t, lanes, 1)
			assert.Equal(t, "Lane 2", lanes[0].Title)
		},

		// ── Batch Create ──────────────────────────────────────────────────
		"BatchCreateLanes": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			body := []map[string]interface{}{
				{"title": "All Tasks", "order": 0},
				{"title": "In Progress", "order": 1},
				{"title": "Done", "order": 2},
				{"title": "Cancelled", "order": 3},
			}
			w := h.Do(t, "POST", "/api/v1/lanes/batch", body, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			lanes := Decode[[]model.Lane](t, w)
			require.Len(t, lanes, 4)
			assert.Equal(t, "All Tasks", lanes[0].Title)
			assert.Equal(t, "In Progress", lanes[1].Title)
			assert.Equal(t, "Done", lanes[2].Title)
			assert.Equal(t, "Cancelled", lanes[3].Title)
			for _, l := range lanes {
				assert.Equal(t, h.UserA.ID, l.UserID)
				assert.NotEqual(t, uuid.Nil, l.ID)
				assert.False(t, l.CreatedAt.IsZero())
			}
		},

		"BatchCreateLanesEmpty": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "POST", "/api/v1/lanes/batch", []map[string]interface{}{}, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			lanes := Decode[[]model.Lane](t, w)
			assert.Len(t, lanes, 0)
		},

		"BatchCreateLanesDefaults": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			body := []map[string]interface{}{
				{"title": "Lane A"},
				{"title": "Lane B"},
			}
			w := h.Do(t, "POST", "/api/v1/lanes/batch", body, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			lanes := Decode[[]model.Lane](t, w)
			require.Len(t, lanes, 2)
			// Should get default conjunction and sort mode
			for _, l := range lanes {
				assert.Equal(t, model.ConjunctionAnd, l.FilterSet.Conjunction)
				assert.Equal(t, []model.FilterRule{}, l.FilterSet.Rules)
				assert.Equal(t, model.SortModeAuto, l.SortConfig.Mode)
			}
		},

		"BatchCreateLanesExceedsMax": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			body := make([]map[string]interface{}, 21)
			for i := range body {
				body[i] = map[string]interface{}{"title": fmt.Sprintf("Lane %d", i)}
			}
			w := h.Do(t, "POST", "/api/v1/lanes/batch", body, h.UserA.ID)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		},

		"BatchCreateLanesIsolation": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			body := []map[string]interface{}{
				{"title": "UserA Lane 1"},
				{"title": "UserA Lane 2"},
			}
			h.Do(t, "POST", "/api/v1/lanes/batch", body, h.UserA.ID)
			// UserB should not see UserA's lanes
			w := h.Do(t, "GET", "/api/v1/lanes", nil, h.UserB.ID)
			require.Equal(t, http.StatusOK, w.Code)
			assert.Len(t, Decode[[]model.Lane](t, w), 0)
		},

		// ── Upsert ────────────────────────────────────────────────────────
		"UpsertLaneCreatesWhenNew": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			id := uuid.New()
			body := map[string]interface{}{"title": "Upserted Lane", "order": 1}
			w := h.Do(t, "PUT", fmt.Sprintf("/api/v1/lanes/%s", id), body, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			lane := Decode[model.Lane](t, w)
			assert.Equal(t, id, lane.ID)
			assert.Equal(t, "Upserted Lane", lane.Title)
			assert.Equal(t, h.UserA.ID, lane.UserID)
		},

		"UpsertLaneUpdatesWhenExists": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			created := createTestLane(t, h, h.UserA.ID, "Original")
			body := map[string]interface{}{"title": "Updated via Upsert", "order": 99}
			w := h.Do(t, "PUT", fmt.Sprintf("/api/v1/lanes/%s", created.ID), body, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			lane := Decode[model.Lane](t, w)
			assert.Equal(t, "Updated via Upsert", lane.Title)
			assert.Equal(t, 99, lane.Order)
		},

		"UpsertLaneDifferentUserGets404": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			created := createTestLane(t, h, h.UserA.ID, "UserA Lane")
			body := map[string]interface{}{"title": "Hijack"}
			w := h.Do(t, "PUT", fmt.Sprintf("/api/v1/lanes/%s", created.ID), body, h.UserB.ID)
			assert.Equal(t, http.StatusNotFound, w.Code)
		},

		// ── ReorderAll ────────────────────────────────────────────────────
		"ReorderLanes": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			l1 := createTestLane(t, h, h.UserA.ID, "Lane A")
			l2 := createTestLane(t, h, h.UserA.ID, "Lane B")
			l3 := createTestLane(t, h, h.UserA.ID, "Lane C")
			items := []map[string]interface{}{
				{"id": l3.ID.String(), "order": 0},
				{"id": l1.ID.String(), "order": 1},
				{"id": l2.ID.String(), "order": 2},
			}
			w := h.Do(t, "PUT", "/api/v1/lanes/reorder", items, h.UserA.ID)
			assert.Equal(t, http.StatusNoContent, w.Code)
			w = h.Do(t, "GET", "/api/v1/lanes", nil, h.UserA.ID)
			lanes := Decode[[]model.Lane](t, w)
			require.Len(t, lanes, 3)
			assert.Equal(t, l3.ID, lanes[0].ID)
			assert.Equal(t, l1.ID, lanes[1].ID)
			assert.Equal(t, l2.ID, lanes[2].ID)
		},

		"ReorderLanesEmpty": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "PUT", "/api/v1/lanes/reorder", []map[string]interface{}{}, h.UserA.ID)
			assert.Equal(t, http.StatusNoContent, w.Code)
		},
	})
}
