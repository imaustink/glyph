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

func createTestPage(t *testing.T, h *Harness, userID uuid.UUID, title string) model.Page {
	t.Helper()
	body := map[string]interface{}{"title": title, "type": "page"}
	w := h.Do(t, "POST", "/api/v1/pages", body, userID)
	require.Equal(t, http.StatusCreated, w.Code)
	return Decode[model.Page](t, w)
}

func TestPages(t *testing.T) {
	RunSpecs(t, map[string]func(t *testing.T, h *Harness){
		// ── Create ────────────────────────────────────────────────────────
		"CreatePage": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			body := map[string]interface{}{"title": "My First Page", "type": "page"}
			w := h.Do(t, "POST", "/api/v1/pages", body, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)

			page := Decode[model.Page](t, w)
			assert.Equal(t, "My First Page", page.Title)
			assert.Equal(t, model.NodeTypePage, page.Type)
			assert.Equal(t, h.UserA.ID, page.UserID)
			assert.NotEqual(t, uuid.Nil, page.ID)
			assert.Equal(t, []string{}, page.Tags)
			assert.Equal(t, 0, page.Order)
			assert.Nil(t, page.ParentID)
			assert.False(t, page.CreatedAt.IsZero())
			assert.False(t, page.UpdatedAt.IsZero())
		},

		"CreateFolder": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			body := map[string]interface{}{"title": "My Folder", "type": "folder"}
			w := h.Do(t, "POST", "/api/v1/pages", body, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)

			page := Decode[model.Page](t, w)
			assert.Equal(t, model.NodeTypeFolder, page.Type)
			assert.Equal(t, "My Folder", page.Title)
		},

		"CreatePageWithAllFields": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			body := map[string]interface{}{
				"title": "Tagged Page", "type": "page",
				"order": 5, "tags": []string{"work", "important"},
			}
			w := h.Do(t, "POST", "/api/v1/pages", body, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)

			page := Decode[model.Page](t, w)
			assert.Equal(t, "Tagged Page", page.Title)
			assert.Equal(t, 5, page.Order)
			assert.Equal(t, []string{"work", "important"}, page.Tags)
		},

		"CreatePageWithTodoTrigger": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			body := map[string]interface{}{
				"title": "Page with Trigger", "type": "page",
				"todoTrigger": map[string]interface{}{
					"pattern": "^TODO$", "matchMode": "regex", "blockTypes": []string{"heading"},
				},
			}
			w := h.Do(t, "POST", "/api/v1/pages", body, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)

			page := Decode[model.Page](t, w)
			require.NotNil(t, page.TodoTrigger)
			assert.Equal(t, "^TODO$", page.TodoTrigger.Pattern)
			assert.Equal(t, model.MatchModeRegex, page.TodoTrigger.MatchMode)
			assert.Equal(t, []string{"heading"}, page.TodoTrigger.BlockTypes)
		},

		"CreatePageWithParent": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			parent := createTestPage(t, h, h.UserA.ID, "Parent Folder")
			body := map[string]interface{}{
				"title": "Child Page", "type": "page", "parentId": parent.ID.String(),
			}
			w := h.Do(t, "POST", "/api/v1/pages", body, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)

			child := Decode[model.Page](t, w)
			require.NotNil(t, child.ParentID)
			assert.Equal(t, parent.ID, *child.ParentID)
		},

		// ── List ──────────────────────────────────────────────────────────
		"ListPagesEmpty": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "GET", "/api/v1/pages", nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			assert.Len(t, Decode[[]model.Page](t, w), 0)
		},

		"ListPages": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			createTestPage(t, h, h.UserA.ID, "Page A")
			createTestPage(t, h, h.UserA.ID, "Page B")
			w := h.Do(t, "GET", "/api/v1/pages", nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			assert.Len(t, Decode[[]model.Page](t, w), 2)
		},

		"ListPagesOrdering": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			for i, title := range []string{"Third", "First", "Second"} {
				orders := []int{3, 1, 2}
				body := map[string]interface{}{"title": title, "type": "page", "order": orders[i]}
				w := h.Do(t, "POST", "/api/v1/pages", body, h.UserA.ID)
				require.Equal(t, http.StatusCreated, w.Code)
			}
			w := h.Do(t, "GET", "/api/v1/pages", nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			pages := Decode[[]model.Page](t, w)
			require.Len(t, pages, 3)
			assert.Equal(t, "First", pages[0].Title)
			assert.Equal(t, "Second", pages[1].Title)
			assert.Equal(t, "Third", pages[2].Title)
		},

		// ── Get ───────────────────────────────────────────────────────────
		"GetPage": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			created := createTestPage(t, h, h.UserA.ID, "Get Me")
			w := h.Do(t, "GET", fmt.Sprintf("/api/v1/pages/%s", created.ID), nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			page := Decode[model.Page](t, w)
			assert.Equal(t, created.ID, page.ID)
			assert.Equal(t, "Get Me", page.Title)
		},

		"GetPageNotFound": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "GET", fmt.Sprintf("/api/v1/pages/%s", uuid.New()), nil, h.UserA.ID)
			assert.Equal(t, http.StatusNotFound, w.Code)
		},

		// ── Update ────────────────────────────────────────────────────────
		"UpdatePageTitle": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			created := createTestPage(t, h, h.UserA.ID, "Original")
			body := map[string]interface{}{"title": "Updated Title"}
			w := h.Do(t, "PATCH", fmt.Sprintf("/api/v1/pages/%s", created.ID), body, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			page := Decode[model.Page](t, w)
			assert.Equal(t, "Updated Title", page.Title)
			assert.Equal(t, model.NodeTypePage, page.Type)
		},

		"UpdatePageTags": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			created := createTestPage(t, h, h.UserA.ID, "Tag Me")
			body := map[string]interface{}{"tags": []string{"new-tag", "another"}}
			w := h.Do(t, "PATCH", fmt.Sprintf("/api/v1/pages/%s", created.ID), body, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, []string{"new-tag", "another"}, Decode[model.Page](t, w).Tags)
		},

		"UpdatePageOrder": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			created := createTestPage(t, h, h.UserA.ID, "Reorder Me")
			body := map[string]interface{}{"order": 99}
			w := h.Do(t, "PATCH", fmt.Sprintf("/api/v1/pages/%s", created.ID), body, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, 99, Decode[model.Page](t, w).Order)
		},

		"UpdatePageAddTodoTrigger": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			created := createTestPage(t, h, h.UserA.ID, "No Trigger")
			body := map[string]interface{}{
				"todoTrigger": map[string]interface{}{
					"pattern": "TODO", "matchMode": "exact", "blockTypes": []string{"heading", "paragraph"},
				},
			}
			w := h.Do(t, "PATCH", fmt.Sprintf("/api/v1/pages/%s", created.ID), body, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			page := Decode[model.Page](t, w)
			require.NotNil(t, page.TodoTrigger)
			assert.Equal(t, "TODO", page.TodoTrigger.Pattern)
			assert.Equal(t, model.MatchModeExact, page.TodoTrigger.MatchMode)
		},

		"UpdatePageNotFound": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			body := map[string]interface{}{"title": "Nope"}
			w := h.Do(t, "PATCH", fmt.Sprintf("/api/v1/pages/%s", uuid.New()), body, h.UserA.ID)
			assert.Equal(t, http.StatusNotFound, w.Code)
		},

		// ── Delete ────────────────────────────────────────────────────────
		"DeletePage": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			created := createTestPage(t, h, h.UserA.ID, "Delete Me")
			w := h.Do(t, "DELETE", fmt.Sprintf("/api/v1/pages/%s", created.ID), nil, h.UserA.ID)
			assert.Equal(t, http.StatusNoContent, w.Code)
			w = h.Do(t, "GET", fmt.Sprintf("/api/v1/pages/%s", created.ID), nil, h.UserA.ID)
			assert.Equal(t, http.StatusNotFound, w.Code)
		},

		"DeletePageCascadesContent": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createTestPage(t, h, h.UserA.ID, "With Content")
			contentBody := map[string]interface{}{"content": map[string]interface{}{"type": "doc"}}
			w := h.Do(t, "PUT", fmt.Sprintf("/api/v1/pages/%s/content", page.ID), contentBody, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			w = h.Do(t, "DELETE", fmt.Sprintf("/api/v1/pages/%s", page.ID), nil, h.UserA.ID)
			assert.Equal(t, http.StatusNoContent, w.Code)
			w = h.Do(t, "GET", fmt.Sprintf("/api/v1/pages/%s/content", page.ID), nil, h.UserA.ID)
			assert.Equal(t, http.StatusNotFound, w.Code)
		},

		// ── Page Content ──────────────────────────────────────────────────
		"UpsertPageContent": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createTestPage(t, h, h.UserA.ID, "Content Page")
			body := map[string]interface{}{"content": map[string]interface{}{"type": "doc", "content": []interface{}{map[string]interface{}{"type": "paragraph"}}}}
			w := h.Do(t, "PUT", fmt.Sprintf("/api/v1/pages/%s/content", page.ID), body, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			pc := Decode[model.PageContent](t, w)
			assert.Equal(t, page.ID, pc.PageID)
			// Validator re-serializes JSON (key order may differ), so compare parsed
			assert.JSONEq(t, `{"type":"doc","content":[{"type":"paragraph"}]}`, string(pc.Content))
			assert.False(t, pc.UpdatedAt.IsZero())
		},

		"GetPageContent": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createTestPage(t, h, h.UserA.ID, "Read Content")
			body := map[string]interface{}{"content": map[string]interface{}{"type": "doc", "content": []interface{}{map[string]interface{}{"type": "paragraph", "content": []interface{}{map[string]interface{}{"type": "text", "text": "hello world"}}}}}}
			h.Do(t, "PUT", fmt.Sprintf("/api/v1/pages/%s/content", page.ID), body, h.UserA.ID)
			w := h.Do(t, "GET", fmt.Sprintf("/api/v1/pages/%s/content", page.ID), nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			assert.JSONEq(t, `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"hello world"}]}]}`, string(Decode[model.PageContent](t, w).Content))
		},

		"GetPageContentNotFound": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createTestPage(t, h, h.UserA.ID, "No Content Yet")
			w := h.Do(t, "GET", fmt.Sprintf("/api/v1/pages/%s/content", page.ID), nil, h.UserA.ID)
			assert.Equal(t, http.StatusNotFound, w.Code)
		},

		"UpdatePageContent": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createTestPage(t, h, h.UserA.ID, "Update Content")
			h.Do(t, "PUT", fmt.Sprintf("/api/v1/pages/%s/content", page.ID),
				map[string]interface{}{"content": map[string]interface{}{"type": "doc", "content": []interface{}{map[string]interface{}{"type": "paragraph", "content": []interface{}{map[string]interface{}{"type": "text", "text": "version 1"}}}}}}, h.UserA.ID)
			w := h.Do(t, "PUT", fmt.Sprintf("/api/v1/pages/%s/content", page.ID),
				map[string]interface{}{"content": map[string]interface{}{"type": "doc", "content": []interface{}{map[string]interface{}{"type": "paragraph", "content": []interface{}{map[string]interface{}{"type": "text", "text": "version 2"}}}}}}, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			assert.JSONEq(t, `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"version 2"}]}]}`, string(Decode[model.PageContent](t, w).Content))
		},

		"UpsertContentForNonexistentPage": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			body := map[string]interface{}{"content": "orphan"}
			w := h.Do(t, "PUT", fmt.Sprintf("/api/v1/pages/%s/content", uuid.New()), body, h.UserA.ID)
			// Handler now fetches the page first and returns 404 (not 500) when it doesn't exist.
			assert.Equal(t, http.StatusNotFound, w.Code)
		},

		// ── Upsert ────────────────────────────────────────────────────────
		"UpsertPageCreatesWhenNew": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			id := uuid.New()
			body := map[string]interface{}{"title": "Upserted Page", "type": "page"}
			w := h.Do(t, "PUT", fmt.Sprintf("/api/v1/pages/%s", id), body, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			page := Decode[model.Page](t, w)
			assert.Equal(t, id, page.ID)
			assert.Equal(t, "Upserted Page", page.Title)
			assert.Equal(t, h.UserA.ID, page.UserID)
		},

		"UpsertPageUpdatesWhenExists": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			created := createTestPage(t, h, h.UserA.ID, "Original Page")
			body := map[string]interface{}{"title": "Updated via Upsert", "type": "page"}
			w := h.Do(t, "PUT", fmt.Sprintf("/api/v1/pages/%s", created.ID), body, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			page := Decode[model.Page](t, w)
			assert.Equal(t, "Updated via Upsert", page.Title)
		},

		"UpsertPageDifferentUserGets404": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			created := createTestPage(t, h, h.UserA.ID, "UserA Page")
			body := map[string]interface{}{"title": "Hijack", "type": "page"}
			w := h.Do(t, "PUT", fmt.Sprintf("/api/v1/pages/%s", created.ID), body, h.UserB.ID)
			assert.Equal(t, http.StatusNotFound, w.Code)
		},
	})
}
