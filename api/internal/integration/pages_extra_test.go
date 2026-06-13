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

func TestPagesExtra(t *testing.T) {
	RunSpecs(t, map[string]func(t *testing.T, h *Harness){
		// ── Pagination ────────────────────────────────────────────────────
		"ListPages_PaginationValid": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			createTestPage(t, h, h.UserA.ID, "Page Alpha")
			createTestPage(t, h, h.UserA.ID, "Page Beta")
			createTestPage(t, h, h.UserA.ID, "Page Gamma")
			w := h.Do(t, "GET", "/api/v1/pages?limit=2&offset=0", nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			pages := Decode[[]model.Page](t, w)
			assert.Len(t, pages, 2)
			assert.NotEmpty(t, w.Header().Get("X-Total-Count"))
		},

		"ListPages_PaginationOffset": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			createTestPage(t, h, h.UserA.ID, "Page 1")
			createTestPage(t, h, h.UserA.ID, "Page 2")
			createTestPage(t, h, h.UserA.ID, "Page 3")
			w := h.Do(t, "GET", "/api/v1/pages?limit=10&offset=2", nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			pages := Decode[[]model.Page](t, w)
			assert.Len(t, pages, 1)
		},

		"ListPages_PaginationLimitOnly": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			createTestPage(t, h, h.UserA.ID, "Page A")
			createTestPage(t, h, h.UserA.ID, "Page B")
			w := h.Do(t, "GET", "/api/v1/pages?limit=1", nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			pages := Decode[[]model.Page](t, w)
			assert.Len(t, pages, 1)
		},

		"ListPages_PaginationOffsetOnly": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			createTestPage(t, h, h.UserA.ID, "Page A")
			createTestPage(t, h, h.UserA.ID, "Page B")
			w := h.Do(t, "GET", "/api/v1/pages?offset=1", nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
		},

		"ListPages_InvalidLimit": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "GET", "/api/v1/pages?limit=abc", nil, h.UserA.ID)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		},

		"ListPages_NegativeLimit": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "GET", "/api/v1/pages?limit=-1", nil, h.UserA.ID)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		},

		"ListPages_InvalidOffset": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "GET", "/api/v1/pages?offset=foo", nil, h.UserA.ID)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		},

		"ListPages_NegativeOffset": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "GET", "/api/v1/pages?limit=10&offset=-1", nil, h.UserA.ID)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		},

		// ── Cycle prevention ─────────────────────────────────────────────
		"UpdatePage_SelfParentCycle": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createTestPage(t, h, h.UserA.ID, "Cycle Me")
			body := map[string]interface{}{"parentId": page.ID.String()}
			w := h.Do(t, "PATCH", fmt.Sprintf("/api/v1/pages/%s", page.ID), body, h.UserA.ID)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		},

		"UpdatePage_DescendantParentCycle": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			parent := createTestPage(t, h, h.UserA.ID, "Grandparent")
			// Create child under parent
			childBody := map[string]interface{}{"title": "Child", "type": "page", "parentId": parent.ID.String()}
			childW := h.Do(t, "POST", "/api/v1/pages", childBody, h.UserA.ID)
			require.Equal(t, http.StatusCreated, childW.Code)
			child := Decode[model.Page](t, childW)
			// Try to set parent's parentId to child (would create cycle)
			body := map[string]interface{}{"parentId": child.ID.String()}
			w := h.Do(t, "PATCH", fmt.Sprintf("/api/v1/pages/%s", parent.ID), body, h.UserA.ID)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		},

		// ── Forbidden delete ──────────────────────────────────────────────
		"DeletePage_ForbiddenSharedViewer": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createTestPage(t, h, h.UserA.ID, "Alice Page")
			// Share with Bob as viewer so he can see but not delete
			h.Do(t, "POST", "/api/v1/shares", map[string]interface{}{
				"resourceType": "page", "resourceId": page.ID.String(),
				"sharedWithId": h.UserB.ID.String(), "permission": "viewer",
			}, h.UserA.ID)
			w := h.Do(t, "DELETE", fmt.Sprintf("/api/v1/pages/%s", page.ID), nil, h.UserB.ID)
			assert.Equal(t, http.StatusForbidden, w.Code)
		},

		// ── UpsertPageContent invalid JSON ────────────────────────────────
		"UpsertPageContent_InvalidJSON": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createTestPage(t, h, h.UserA.ID, "Content Page")
			w := h.DoRaw(t, "PUT", fmt.Sprintf("/api/v1/pages/%s/content", page.ID), []byte("{bad json"), h.UserA.ID)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		},

		// ── Non-owner write via share ──────────────────────────────────────
		"UpdatePage_EditorShareCanUpdate": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createTestPage(t, h, h.UserA.ID, "Shared Editable")
			h.Do(t, "POST", "/api/v1/shares", map[string]interface{}{
				"resourceType": "page", "resourceId": page.ID.String(),
				"sharedWithId": h.UserB.ID.String(), "permission": "editor",
			}, h.UserA.ID)
			w := h.Do(t, "PATCH", fmt.Sprintf("/api/v1/pages/%s", page.ID),
				map[string]interface{}{"title": "Updated by Bob"}, h.UserB.ID)
			assert.Equal(t, http.StatusOK, w.Code)
		},

		// ── UpsertPageContent non-owner ───────────────────────────────────
		"UpsertPageContent_ForbiddenSharedViewer": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createTestPage(t, h, h.UserA.ID, "Content Guard")
			h.Do(t, "POST", "/api/v1/shares", map[string]interface{}{
				"resourceType": "page", "resourceId": page.ID.String(),
				"sharedWithId": h.UserB.ID.String(), "permission": "viewer",
			}, h.UserA.ID)
			body := map[string]interface{}{"content": map[string]interface{}{"type": "doc"}}
			w := h.Do(t, "PUT", fmt.Sprintf("/api/v1/pages/%s/content", page.ID), body, h.UserB.ID)
			assert.Equal(t, http.StatusForbidden, w.Code)
		},

		// ── GetPageContent page not found ─────────────────────────────────
		"GetPageContent_PageNotExists": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "GET", fmt.Sprintf("/api/v1/pages/%s/content", uuid.New()), nil, h.UserA.ID)
			assert.Equal(t, http.StatusNotFound, w.Code)
		},

		// ── UpdatePage invalid JSON body ──────────────────────────────────
		"UpdatePage_InvalidJSON": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createTestPage(t, h, h.UserA.ID, "PATCH Target")
			w := h.DoRaw(t, "PATCH", fmt.Sprintf("/api/v1/pages/%s", page.ID), []byte("{bad"), h.UserA.ID)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		},
	})
}
