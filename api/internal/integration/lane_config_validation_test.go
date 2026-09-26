package integration

import (
	"net/http"
	"testing"

	"github.com/glyph/api/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLaneConfigValidation: a lane's filter and sort settings are stored as
// JSON and interpreted later by the board. Values outside the enums the
// board understands must be refused on write rather than stored.
func TestLaneConfigValidation(t *testing.T) {
	bad := map[string]map[string]interface{}{
		"conjunction": {"filterSet": map[string]interface{}{"conjunction": "xor", "rules": []interface{}{}}},
		"operator": {"filterSet": map[string]interface{}{"conjunction": "and", "rules": []interface{}{
			map[string]interface{}{"id": "r1", "field": "status", "operator": "matches", "value": "x"},
		}}},
		"sortMode":      {"sortConfig": map[string]interface{}{"mode": "random"}},
		"sortDirection": {"sortConfig": map[string]interface{}{"mode": "field", "field": "title", "direction": "sideways"}},
	}
	withTitle := func(m map[string]interface{}) map[string]interface{} {
		out := map[string]interface{}{"title": "Lane"}
		for k, v := range m {
			out[k] = v
		}
		return out
	}

	RunSpecs(t, map[string]func(t *testing.T, h *Harness){
		"PersonalLaneWritesRejectUnknownEnums": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "POST", "/api/v1/lanes", map[string]interface{}{"title": "Good"}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			lane := Decode[model.Lane](t, w)

			for name, body := range bad {
				assert.Equal(t, http.StatusBadRequest, h.Do(t, "POST", "/api/v1/lanes", withTitle(body), h.UserA.ID).Code, "POST %s", name)
				assert.Equal(t, http.StatusBadRequest, h.Do(t, "POST", "/api/v1/lanes/batch", []interface{}{withTitle(body)}, h.UserA.ID).Code, "batch %s", name)
				assert.Equal(t, http.StatusBadRequest, h.Do(t, "PUT", "/api/v1/lanes/"+lane.ID.String(), withTitle(body), h.UserA.ID).Code, "PUT %s", name)
				assert.Equal(t, http.StatusBadRequest, h.Do(t, "PATCH", "/api/v1/lanes/"+lane.ID.String(), body, h.UserA.ID).Code, "PATCH %s", name)
			}
			got := Decode[model.Lane](t, h.Do(t, "GET", "/api/v1/lanes/"+lane.ID.String(), nil, h.UserA.ID))
			assert.Equal(t, model.SortModeAuto, got.SortConfig.Mode)
			assert.Equal(t, model.ConjunctionAnd, got.FilterSet.Conjunction)
		},

		"FolderLaneWritesRejectUnknownEnums": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			folder, lane := createFolderWithLane(t, h)
			base := "/api/v1/folders/" + folder.ID.String() + "/lanes"
			for name, body := range bad {
				assert.Equal(t, http.StatusBadRequest, h.Do(t, "POST", base, withTitle(body), h.UserA.ID).Code, "POST %s", name)
				assert.Equal(t, http.StatusBadRequest, h.Do(t, "PUT", base+"/"+lane.ID.String(), body, h.UserA.ID).Code, "PUT %s", name)
			}
		},

		"ValidConfigsStillAccepted": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "POST", "/api/v1/lanes", map[string]interface{}{
				"title": "Due soon",
				"filterSet": map[string]interface{}{"conjunction": "or", "rules": []interface{}{
					map[string]interface{}{"id": "r1", "field": "dueDate", "operator": "before", "value": "2026-01-01"},
					map[string]interface{}{"id": "r2", "field": "tags", "operator": "any", "value": nil},
				}},
				"sortConfig": map[string]interface{}{"mode": "field", "field": "dueDate", "direction": "desc"},
			}, h.UserA.ID)
			assert.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		},
	})
}
