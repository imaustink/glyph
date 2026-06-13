package integration

import (
	"net/http"
	"testing"

	"github.com/glyph/api/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSharingDirect(t *testing.T) {
	RunSpecs(t, map[string]func(t *testing.T, h *Harness){
		"OwnerCanSharePageWithUser": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := Decode[model.Page](t, h.Do(t, "POST", "/api/v1/pages", map[string]interface{}{"title": "Alice Page", "type": "page"}, h.UserA.ID))
			w := h.Do(t, "POST", "/api/v1/shares", map[string]interface{}{
				"resourceType": "page",
				"resourceId":   page.ID.String(),
				"sharedWithId": h.UserB.ID.String(),
				"permission":   "viewer",
			}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
		},
		"SharedPageAppearsInRecipientList": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := Decode[model.Page](t, h.Do(t, "POST", "/api/v1/pages", map[string]interface{}{"title": "Shared With Bob", "type": "page"}, h.UserA.ID))
			h.Do(t, "POST", "/api/v1/shares", map[string]interface{}{
				"resourceType": "page", "resourceId": page.ID.String(),
				"sharedWithId": h.UserB.ID.String(), "permission": "viewer",
			}, h.UserA.ID)
			w := h.Do(t, "GET", "/api/v1/pages", nil, h.UserB.ID)
			require.Equal(t, http.StatusOK, w.Code)
			pages := Decode[[]model.Page](t, w)
			require.Len(t, pages, 1)
			assert.Equal(t, "Shared With Bob", pages[0].Title)
		},
		"ViewerShareCannotEdit": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := Decode[model.Page](t, h.Do(t, "POST", "/api/v1/pages", map[string]interface{}{"title": "View Only", "type": "page"}, h.UserA.ID))
			h.Do(t, "POST", "/api/v1/shares", map[string]interface{}{
				"resourceType": "page", "resourceId": page.ID.String(),
				"sharedWithId": h.UserB.ID.String(), "permission": "viewer",
			}, h.UserA.ID)
			w := h.Do(t, "PATCH", "/api/v1/pages/"+page.ID.String(), map[string]interface{}{"title": "Hijacked", "type": "page"}, h.UserB.ID)
			assert.Equal(t, http.StatusForbidden, w.Code)
		},
		"EditorShareCanEdit": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := Decode[model.Page](t, h.Do(t, "POST", "/api/v1/pages", map[string]interface{}{"title": "Editable", "type": "page"}, h.UserA.ID))
			h.Do(t, "POST", "/api/v1/shares", map[string]interface{}{
				"resourceType": "page", "resourceId": page.ID.String(),
				"sharedWithId": h.UserB.ID.String(), "permission": "editor",
			}, h.UserA.ID)
			w := h.Do(t, "PATCH", "/api/v1/pages/"+page.ID.String(), map[string]interface{}{"title": "Updated By Bob", "type": "page"}, h.UserB.ID)
			assert.Equal(t, http.StatusOK, w.Code)
		},
		// Bob has no access to Alice's page, so GetByID returns 404 (not 403).
		// This is the correct security behaviour: don't reveal the resource exists.
		"NonOwnerCannotCreateShare": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := Decode[model.Page](t, h.Do(t, "POST", "/api/v1/pages", map[string]interface{}{"title": "Alice Only", "type": "page"}, h.UserA.ID))
			w := h.Do(t, "POST", "/api/v1/shares", map[string]interface{}{
				"resourceType": "page", "resourceId": page.ID.String(),
				"sharedWithId": h.UserA.ID.String(), "permission": "viewer",
			}, h.UserB.ID)
			// Bob cannot see the page at all, so 404 (resource not found from his view).
			assert.Equal(t, http.StatusNotFound, w.Code)
		},
		"OwnerCanListShares": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := Decode[model.Page](t, h.Do(t, "POST", "/api/v1/pages", map[string]interface{}{"title": "Listed", "type": "page"}, h.UserA.ID))
			h.Do(t, "POST", "/api/v1/shares", map[string]interface{}{
				"resourceType": "page", "resourceId": page.ID.String(),
				"sharedWithId": h.UserB.ID.String(), "permission": "viewer",
			}, h.UserA.ID)
			w := h.Do(t, "GET", "/api/v1/shares?resourceType=page&resourceId="+page.ID.String(), nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			shares := Decode[[]map[string]interface{}](t, w)
			require.Len(t, shares, 1)
			assert.Equal(t, "viewer", shares[0]["permission"])
		},
		"OwnerCanUpdateSharePermission": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := Decode[model.Page](t, h.Do(t, "POST", "/api/v1/pages", map[string]interface{}{"title": "Upgradeable", "type": "page"}, h.UserA.ID))
			share := Decode[map[string]interface{}](t, h.Do(t, "POST", "/api/v1/shares", map[string]interface{}{
				"resourceType": "page", "resourceId": page.ID.String(),
				"sharedWithId": h.UserB.ID.String(), "permission": "viewer",
			}, h.UserA.ID))
			shareID := share["id"].(string)
			w := h.Do(t, "PATCH", "/api/v1/shares/"+shareID, map[string]string{"permission": "editor"}, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			updated := Decode[map[string]interface{}](t, w)
			assert.Equal(t, "editor", updated["permission"])
		},
		"OwnerCanDeleteShare": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := Decode[model.Page](t, h.Do(t, "POST", "/api/v1/pages", map[string]interface{}{"title": "Revokeable", "type": "page"}, h.UserA.ID))
			share := Decode[map[string]interface{}](t, h.Do(t, "POST", "/api/v1/shares", map[string]interface{}{
				"resourceType": "page", "resourceId": page.ID.String(),
				"sharedWithId": h.UserB.ID.String(), "permission": "viewer",
			}, h.UserA.ID))
			shareID := share["id"].(string)
			w := h.Do(t, "DELETE", "/api/v1/shares/"+shareID, nil, h.UserA.ID)
			assert.Equal(t, http.StatusNoContent, w.Code)
			pages := Decode[[]model.Page](t, h.Do(t, "GET", "/api/v1/pages", nil, h.UserB.ID))
			assert.Empty(t, pages)
		},
		"CannotShareWithSelf": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := Decode[model.Page](t, h.Do(t, "POST", "/api/v1/pages", map[string]interface{}{"title": "Self Share", "type": "page"}, h.UserA.ID))
			w := h.Do(t, "POST", "/api/v1/shares", map[string]interface{}{
				"resourceType": "page", "resourceId": page.ID.String(),
				"sharedWithId": h.UserA.ID.String(), "permission": "viewer",
			}, h.UserA.ID)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		},
		"TaskDirectShare": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			task := Decode[model.Task](t, h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{"title": "Shared Task"}, h.UserA.ID))
			w := h.Do(t, "POST", "/api/v1/shares", map[string]interface{}{
				"resourceType": "task", "resourceId": task.ID.String(),
				"sharedWithId": h.UserB.ID.String(), "permission": "viewer",
			}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			w2 := h.Do(t, "GET", "/api/v1/tasks", nil, h.UserB.ID)
			require.Equal(t, http.StatusOK, w2.Code)
			tasks := Decode[[]model.Task](t, w2)
			require.Len(t, tasks, 1)
			assert.Equal(t, "Shared Task", tasks[0].Title)
		},
		"TemplateDirectShare": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			tmpl := Decode[model.Template](t, h.Do(t, "POST", "/api/v1/templates", map[string]interface{}{"name": "Shared Template", "content": "# Shared"}, h.UserA.ID))
			w := h.Do(t, "POST", "/api/v1/shares", map[string]interface{}{
				"resourceType": "template", "resourceId": tmpl.ID.String(),
				"sharedWithId": h.UserB.ID.String(), "permission": "viewer",
			}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			w2 := h.Do(t, "GET", "/api/v1/templates", nil, h.UserB.ID)
			require.Equal(t, http.StatusOK, w2.Code)
			templates := Decode[[]model.Template](t, w2)
			require.Len(t, templates, 1)
			assert.Equal(t, "Shared Template", templates[0].Name)
		},
		"ShareByEmail": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := Decode[model.Page](t, h.Do(t, "POST", "/api/v1/pages", map[string]interface{}{"title": "Email Share Page", "type": "page"}, h.UserA.ID))
			w := h.Do(t, "POST", "/api/v1/shares", map[string]interface{}{
				"resourceType":    "page",
				"resourceId":      page.ID.String(),
				"sharedWithEmail": "bob@test.com",
				"permission":      "viewer",
			}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			share := Decode[map[string]interface{}](t, w)
			assert.Equal(t, "viewer", share["permission"])
		},
	})
}
