package integration

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/glyph/api/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsolation(t *testing.T) {
	RunSpecs(t, map[string]func(t *testing.T, h *Harness){
		// ── Page Isolation ─────────────────────────────────────────────────
		"PageIsolation_ListOnlyOwn": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			createTestPage(t, h, h.UserA.ID, "Alice's Page")
			createTestPage(t, h, h.UserB.ID, "Bob's Page")

			w := h.Do(t, "GET", "/api/v1/pages", nil, h.UserA.ID)
			pages := Decode[[]model.Page](t, w)
			require.Len(t, pages, 1)
			assert.Equal(t, "Alice's Page", pages[0].Title)

			w = h.Do(t, "GET", "/api/v1/pages", nil, h.UserB.ID)
			pages = Decode[[]model.Page](t, w)
			require.Len(t, pages, 1)
			assert.Equal(t, "Bob's Page", pages[0].Title)
		},

		"PageIsolation_GetOthers404": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			alicePage := createTestPage(t, h, h.UserA.ID, "Alice's Secret")
			w := h.Do(t, "GET", fmt.Sprintf("/api/v1/pages/%s", alicePage.ID), nil, h.UserB.ID)
			assert.Equal(t, http.StatusNotFound, w.Code)
		},

		"PageIsolation_UpdateOthersBlocked": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			alicePage := createTestPage(t, h, h.UserA.ID, "Alice's Page")
			w := h.Do(t, "PATCH", fmt.Sprintf("/api/v1/pages/%s", alicePage.ID),
				map[string]interface{}{"title": "Hacked!"}, h.UserB.ID)
			assert.Equal(t, http.StatusNotFound, w.Code)

			w = h.Do(t, "GET", fmt.Sprintf("/api/v1/pages/%s", alicePage.ID), nil, h.UserA.ID)
			assert.Equal(t, "Alice's Page", Decode[model.Page](t, w).Title)
		},

		"PageIsolation_DeleteOthersNoOp": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			alicePage := createTestPage(t, h, h.UserA.ID, "Alice's Page")
			h.Do(t, "DELETE", fmt.Sprintf("/api/v1/pages/%s", alicePage.ID), nil, h.UserB.ID)
			w := h.Do(t, "GET", fmt.Sprintf("/api/v1/pages/%s", alicePage.ID), nil, h.UserA.ID)
			assert.Equal(t, http.StatusOK, w.Code)
		},

		"PageContentIsolation": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			alicePage := createTestPage(t, h, h.UserA.ID, "Alice's Content Page")
			secretDoc := map[string]interface{}{"type": "doc", "content": []interface{}{map[string]interface{}{"type": "paragraph", "content": []interface{}{map[string]interface{}{"type": "text", "text": "secret content"}}}}}
			secretDocJSON := `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"secret content"}]}]}`
			h.Do(t, "PUT", fmt.Sprintf("/api/v1/pages/%s/content", alicePage.ID),
				map[string]interface{}{"content": secretDoc}, h.UserA.ID)

			// Bob can't read
			w := h.Do(t, "GET", fmt.Sprintf("/api/v1/pages/%s/content", alicePage.ID), nil, h.UserB.ID)
			assert.Equal(t, http.StatusNotFound, w.Code)

			// Bob can't overwrite
			w = h.Do(t, "PUT", fmt.Sprintf("/api/v1/pages/%s/content", alicePage.ID),
				map[string]interface{}{"content": map[string]interface{}{"type": "doc", "content": []interface{}{map[string]interface{}{"type": "paragraph", "content": []interface{}{map[string]interface{}{"type": "text", "text": "hacked"}}}}}}, h.UserB.ID)
			assert.NotEqual(t, http.StatusOK, w.Code)

			// Alice's content unchanged
			w = h.Do(t, "GET", fmt.Sprintf("/api/v1/pages/%s/content", alicePage.ID), nil, h.UserA.ID)
			assert.JSONEq(t, secretDocJSON, string(Decode[model.PageContent](t, w).Content))
		},

		// ── Task Isolation ─────────────────────────────────────────────────
		"TaskIsolation_ListOnlyOwn": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			createTestTask(t, h, h.UserA.ID, "Alice's Task")
			createTestTask(t, h, h.UserB.ID, "Bob's Task")

			w := h.Do(t, "GET", "/api/v1/tasks", nil, h.UserA.ID)
			tasks := Decode[[]model.Task](t, w)
			require.Len(t, tasks, 1)
			assert.Equal(t, "Alice's Task", tasks[0].Title)
		},

		"TaskIsolation_GetOthers404": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			aliceTask := createTestTask(t, h, h.UserA.ID, "Alice's Secret Task")
			w := h.Do(t, "GET", fmt.Sprintf("/api/v1/tasks/%s", aliceTask.ID), nil, h.UserB.ID)
			assert.Equal(t, http.StatusNotFound, w.Code)
		},

		"TaskIsolation_UpdateOthersBlocked": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			aliceTask := createTestTask(t, h, h.UserA.ID, "Alice's Task")
			w := h.Do(t, "PATCH", fmt.Sprintf("/api/v1/tasks/%s", aliceTask.ID),
				map[string]interface{}{"title": "Hacked!"}, h.UserB.ID)
			assert.Equal(t, http.StatusNotFound, w.Code)
		},

		"TaskIsolation_DeleteOthersNoOp": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			aliceTask := createTestTask(t, h, h.UserA.ID, "Alice's Task")
			h.Do(t, "DELETE", fmt.Sprintf("/api/v1/tasks/%s", aliceTask.ID), nil, h.UserB.ID)
			w := h.Do(t, "GET", fmt.Sprintf("/api/v1/tasks/%s", aliceTask.ID), nil, h.UserA.ID)
			assert.Equal(t, http.StatusOK, w.Code)
		},

		// ── Lane Isolation ─────────────────────────────────────────────────
		"LaneIsolation_ListOnlyOwn": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			createTestLane(t, h, h.UserA.ID, "Alice's Lane")
			createTestLane(t, h, h.UserB.ID, "Bob's Lane")

			w := h.Do(t, "GET", "/api/v1/lanes", nil, h.UserA.ID)
			lanes := Decode[[]model.Lane](t, w)
			require.Len(t, lanes, 1)
			assert.Equal(t, "Alice's Lane", lanes[0].Title)
		},

		"LaneIsolation_GetOthers404": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			aliceLane := createTestLane(t, h, h.UserA.ID, "Alice's Lane")
			w := h.Do(t, "GET", fmt.Sprintf("/api/v1/lanes/%s", aliceLane.ID), nil, h.UserB.ID)
			assert.Equal(t, http.StatusNotFound, w.Code)
		},

		"LaneIsolation_UpdateOthersBlocked": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			aliceLane := createTestLane(t, h, h.UserA.ID, "Alice's Lane")
			w := h.Do(t, "PATCH", fmt.Sprintf("/api/v1/lanes/%s", aliceLane.ID),
				map[string]interface{}{"title": "Hacked!"}, h.UserB.ID)
			assert.Equal(t, http.StatusNotFound, w.Code)
		},

		// ── Template Isolation ─────────────────────────────────────────────
		"TemplateIsolation_ListOnlyOwn": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			createTestTemplate(t, h, h.UserA.ID, "Alice's Template")
			createTestTemplate(t, h, h.UserB.ID, "Bob's Template")

			w := h.Do(t, "GET", "/api/v1/templates", nil, h.UserA.ID)
			templates := Decode[[]model.Template](t, w)
			require.Len(t, templates, 1)
			assert.Equal(t, "Alice's Template", templates[0].Name)
		},

		"TemplateIsolation_GetOthers404": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			aliceTmpl := createTestTemplate(t, h, h.UserA.ID, "Alice's Template")
			w := h.Do(t, "GET", fmt.Sprintf("/api/v1/templates/%s", aliceTmpl.ID), nil, h.UserB.ID)
			assert.Equal(t, http.StatusNotFound, w.Code)
		},

		"TemplateIsolation_UpdateOthersBlocked": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			aliceTmpl := createTestTemplate(t, h, h.UserA.ID, "Alice's Template")
			w := h.Do(t, "PATCH", fmt.Sprintf("/api/v1/templates/%s", aliceTmpl.ID),
				map[string]interface{}{"name": "Hacked!"}, h.UserB.ID)
			assert.Equal(t, http.StatusNotFound, w.Code)
		},
	})
}
