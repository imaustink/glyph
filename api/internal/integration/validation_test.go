package integration

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidation(t *testing.T) {
	RunSpecs(t, map[string]func(t *testing.T, h *Harness){
		// ── Health ─────────────────────────────────────────────────────────
		"HealthEndpoint": func(t *testing.T, h *Harness) {
			w := h.DoNoAuth(t, "GET", "/health")
			require.Equal(t, http.StatusOK, w.Code)
			assert.Contains(t, w.Body.String(), "ok")
		},

		// ── Missing auth ──────────────────────────────────────────────────
		"MissingAuth_Pages": func(t *testing.T, h *Harness) {
			w := h.DoNoAuth(t, "GET", "/api/v1/pages")
			assert.Equal(t, http.StatusUnauthorized, w.Code)
		},

		"MissingAuth_Tasks": func(t *testing.T, h *Harness) {
			w := h.DoNoAuth(t, "GET", "/api/v1/tasks")
			assert.Equal(t, http.StatusUnauthorized, w.Code)
		},

		"MissingAuth_Lanes": func(t *testing.T, h *Harness) {
			w := h.DoNoAuth(t, "GET", "/api/v1/lanes")
			assert.Equal(t, http.StatusUnauthorized, w.Code)
		},

		"MissingAuth_Templates": func(t *testing.T, h *Harness) {
			w := h.DoNoAuth(t, "GET", "/api/v1/templates")
			assert.Equal(t, http.StatusUnauthorized, w.Code)
		},

		// ── Nonexistent user ──────────────────────────────────────────────
		"NonexistentUser": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			fakeID := uuid.New()
			w := h.Do(t, "GET", "/api/v1/pages", nil, fakeID)
			assert.Equal(t, http.StatusUnauthorized, w.Code)
		},

		// ── Invalid UUIDs ─────────────────────────────────────────────────
		"InvalidUUID_GetPage": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "GET", "/api/v1/pages/not-a-uuid", nil, h.UserA.ID)
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), "invalid UUID")
		},

		"InvalidUUID_GetTask": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "GET", "/api/v1/tasks/not-a-uuid", nil, h.UserA.ID)
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), "invalid UUID")
		},

		"InvalidUUID_GetLane": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "GET", "/api/v1/lanes/not-a-uuid", nil, h.UserA.ID)
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), "invalid UUID")
		},

		"InvalidUUID_GetTemplate": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "GET", "/api/v1/templates/not-a-uuid", nil, h.UserA.ID)
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), "invalid UUID")
		},

		"InvalidUUID_UpdatePage": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "PATCH", "/api/v1/pages/garbage", map[string]interface{}{"title": "x"}, h.UserA.ID)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		},

		"InvalidUUID_DeleteTask": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "DELETE", "/api/v1/tasks/garbage", nil, h.UserA.ID)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		},

		// ── Invalid JSON ──────────────────────────────────────────────────
		"InvalidJSON_CreatePage": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.DoRaw(t, "POST", "/api/v1/pages", []byte("{invalid"), h.UserA.ID)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		},

		"InvalidJSON_CreateTask": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.DoRaw(t, "POST", "/api/v1/tasks", []byte("{broken json}"), h.UserA.ID)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		},

		"InvalidJSON_CreateLane": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.DoRaw(t, "POST", "/api/v1/lanes", []byte("not json at all"), h.UserA.ID)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		},

		"InvalidJSON_CreateTemplate": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.DoRaw(t, "POST", "/api/v1/templates", []byte("{{}}"), h.UserA.ID)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		},

		// ── 404 on nonexistent resources ──────────────────────────────────
		"NotFound_GetPage": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "GET", fmt.Sprintf("/api/v1/pages/%s", uuid.New()), nil, h.UserA.ID)
			assert.Equal(t, http.StatusNotFound, w.Code)
		},

		"NotFound_GetTask": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "GET", fmt.Sprintf("/api/v1/tasks/%s", uuid.New()), nil, h.UserA.ID)
			assert.Equal(t, http.StatusNotFound, w.Code)
		},

		"NotFound_GetLane": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "GET", fmt.Sprintf("/api/v1/lanes/%s", uuid.New()), nil, h.UserA.ID)
			assert.Equal(t, http.StatusNotFound, w.Code)
		},

		"NotFound_GetTemplate": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "GET", fmt.Sprintf("/api/v1/templates/%s", uuid.New()), nil, h.UserA.ID)
			assert.Equal(t, http.StatusNotFound, w.Code)
		},

		"NotFound_GetPageContent": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "GET", fmt.Sprintf("/api/v1/pages/%s/content", uuid.New()), nil, h.UserA.ID)
			assert.Equal(t, http.StatusNotFound, w.Code)
		},

		"NotFound_DeletePage": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "DELETE", fmt.Sprintf("/api/v1/pages/%s", uuid.New()), nil, h.UserA.ID)
			// DELETE on nonexistent may be 204 (idempotent) or 404 depending on handler
			assert.True(t, w.Code == http.StatusNoContent || w.Code == http.StatusNotFound,
				"expected 204 or 404, got %d", w.Code)
		},

		// ── Invalid auth header ───────────────────────────────────────────
		"InvalidAuthHeader": func(t *testing.T, h *Harness) {
			req, _ := http.NewRequest("GET", "/api/v1/pages", nil)
			req.Header.Set("X-Test-User-ID", "not-a-uuid")
			w := httptest.NewRecorder()
			h.Router.ServeHTTP(w, req)
			assert.Equal(t, http.StatusUnauthorized, w.Code)
		},
	})
}
