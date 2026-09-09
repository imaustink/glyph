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

func createTestTemplate(t *testing.T, h *Harness, userID uuid.UUID, name string) model.Template {
	t.Helper()
	body := map[string]interface{}{"name": name, "content": "# " + name}
	w := h.Do(t, "POST", "/api/v1/templates", body, userID)
	require.Equal(t, http.StatusCreated, w.Code)
	return Decode[model.Template](t, w)
}

func TestTemplates(t *testing.T) {
	RunSpecs(t, map[string]func(t *testing.T, h *Harness){
		// ── Create ────────────────────────────────────────────────────────
		"CreateTemplate": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			body := map[string]interface{}{"name": "Meeting Notes", "content": "## Meeting Notes\n\n- "}
			w := h.Do(t, "POST", "/api/v1/templates", body, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			tmpl := Decode[model.Template](t, w)
			assert.Equal(t, "Meeting Notes", tmpl.Name)
			assert.Equal(t, "## Meeting Notes\n\n- ", tmpl.Content)
			assert.Equal(t, h.UserA.ID, tmpl.UserID)
			assert.NotEqual(t, uuid.Nil, tmpl.ID)
			assert.Equal(t, "", tmpl.TitleTemplate)
			assert.False(t, tmpl.IsDefault)
			assert.False(t, tmpl.CreatedAt.IsZero())
		},

		"CreateTemplateWithAllFields": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			body := map[string]interface{}{
				"name": "Full Template", "content": "# {{title}}\n\n## TODO\n\n- ",
				"titleTemplate": "Meeting {{date}}", "isDefault": true,
				"todoTrigger": map[string]interface{}{
					"pattern": "^TODO$", "matchMode": "regex", "blockTypes": []string{"heading"},
				},
			}
			w := h.Do(t, "POST", "/api/v1/templates", body, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			tmpl := Decode[model.Template](t, w)
			assert.Equal(t, "Full Template", tmpl.Name)
			assert.Equal(t, "Meeting {{date}}", tmpl.TitleTemplate)
			assert.True(t, tmpl.IsDefault)
			require.NotNil(t, tmpl.TodoTrigger)
			assert.Equal(t, "^TODO$", tmpl.TodoTrigger.Pattern)
			assert.Equal(t, model.MatchModeRegex, tmpl.TodoTrigger.MatchMode)
			assert.Equal(t, []string{"heading"}, tmpl.TodoTrigger.BlockTypes)
		},

		"CreateTemplateWithTodoTriggerExact": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			body := map[string]interface{}{
				"name": "Exact Trigger", "content": "# TODO\n\n- ",
				"todoTrigger": map[string]interface{}{
					"pattern": "TODO", "matchMode": "exact", "blockTypes": []string{"heading", "paragraph"},
				},
			}
			w := h.Do(t, "POST", "/api/v1/templates", body, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			tmpl := Decode[model.Template](t, w)
			require.NotNil(t, tmpl.TodoTrigger)
			assert.Equal(t, model.MatchModeExact, tmpl.TodoTrigger.MatchMode)
		},

		"CreateDefaultTemplate": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			body := map[string]interface{}{"name": "Default", "content": "", "isDefault": true}
			w := h.Do(t, "POST", "/api/v1/templates", body, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			assert.True(t, Decode[model.Template](t, w).IsDefault)
		},

		// ── List ──────────────────────────────────────────────────────────
		"ListTemplatesEmpty": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "GET", "/api/v1/templates", nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			assert.Len(t, Decode[[]model.Template](t, w), 0)
		},

		"ListTemplates": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			createTestTemplate(t, h, h.UserA.ID, "Template 1")
			createTestTemplate(t, h, h.UserA.ID, "Template 2")
			w := h.Do(t, "GET", "/api/v1/templates", nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			assert.Len(t, Decode[[]model.Template](t, w), 2)
		},

		"ListTemplatesOrderByCreatedAt": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			createTestTemplate(t, h, h.UserA.ID, "First")
			createTestTemplate(t, h, h.UserA.ID, "Second")
			createTestTemplate(t, h, h.UserA.ID, "Third")
			w := h.Do(t, "GET", "/api/v1/templates", nil, h.UserA.ID)
			templates := Decode[[]model.Template](t, w)
			require.Len(t, templates, 3)
			assert.Equal(t, "First", templates[0].Name)
			assert.Equal(t, "Second", templates[1].Name)
			assert.Equal(t, "Third", templates[2].Name)
		},

		// ── Get ───────────────────────────────────────────────────────────
		"GetTemplate": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			created := createTestTemplate(t, h, h.UserA.ID, "Get Me")
			w := h.Do(t, "GET", fmt.Sprintf("/api/v1/templates/%s", created.ID), nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			tmpl := Decode[model.Template](t, w)
			assert.Equal(t, created.ID, tmpl.ID)
			assert.Equal(t, "Get Me", tmpl.Name)
		},

		"GetTemplateNotFound": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "GET", fmt.Sprintf("/api/v1/templates/%s", uuid.New()), nil, h.UserA.ID)
			assert.Equal(t, http.StatusNotFound, w.Code)
		},

		// ── Update ────────────────────────────────────────────────────────
		"UpdateTemplateName": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			created := createTestTemplate(t, h, h.UserA.ID, "Old Name")
			w := h.Do(t, "PATCH", fmt.Sprintf("/api/v1/templates/%s", created.ID),
				map[string]interface{}{"name": "New Name"}, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, "New Name", Decode[model.Template](t, w).Name)
		},

		"UpdateTemplateContent": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			created := createTestTemplate(t, h, h.UserA.ID, "Content Template")
			w := h.Do(t, "PATCH", fmt.Sprintf("/api/v1/templates/%s", created.ID),
				map[string]interface{}{"content": "# Updated Content\n\nNew body here"}, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, "# Updated Content\n\nNew body here", Decode[model.Template](t, w).Content)
		},

		"UpdateTemplateTodoTrigger": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			createTestTemplate(t, h, h.UserA.ID, "No Trigger")
			created := createTestTemplate(t, h, h.UserA.ID, "Add Trigger")
			body := map[string]interface{}{
				"todoTrigger": map[string]interface{}{
					"pattern": "TASKS", "matchMode": "exact", "blockTypes": []string{"heading"},
				},
			}
			w := h.Do(t, "PATCH", fmt.Sprintf("/api/v1/templates/%s", created.ID), body, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			tmpl := Decode[model.Template](t, w)
			require.NotNil(t, tmpl.TodoTrigger)
			assert.Equal(t, "TASKS", tmpl.TodoTrigger.Pattern)
		},

		"UpdateTemplateIsDefault": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			created := createTestTemplate(t, h, h.UserA.ID, "Make Default")
			assert.False(t, created.IsDefault)
			w := h.Do(t, "PATCH", fmt.Sprintf("/api/v1/templates/%s", created.ID),
				map[string]interface{}{"isDefault": true}, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			assert.True(t, Decode[model.Template](t, w).IsDefault)
		},

		"UpdateTemplateNotFound": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "PATCH", fmt.Sprintf("/api/v1/templates/%s", uuid.New()),
				map[string]interface{}{"name": "Nope"}, h.UserA.ID)
			assert.Equal(t, http.StatusNotFound, w.Code)
		},

		// ── Delete ────────────────────────────────────────────────────────
		"DeleteTemplate": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			created := createTestTemplate(t, h, h.UserA.ID, "Delete Me")
			w := h.Do(t, "DELETE", fmt.Sprintf("/api/v1/templates/%s", created.ID), nil, h.UserA.ID)
			assert.Equal(t, http.StatusNoContent, w.Code)
			w = h.Do(t, "GET", fmt.Sprintf("/api/v1/templates/%s", created.ID), nil, h.UserA.ID)
			assert.Equal(t, http.StatusNotFound, w.Code)
		},

		"DeleteTemplateReducesList": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			t1 := createTestTemplate(t, h, h.UserA.ID, "Template 1")
			createTestTemplate(t, h, h.UserA.ID, "Template 2")
			h.Do(t, "DELETE", fmt.Sprintf("/api/v1/templates/%s", t1.ID), nil, h.UserA.ID)
			w := h.Do(t, "GET", "/api/v1/templates", nil, h.UserA.ID)
			templates := Decode[[]model.Template](t, w)
			assert.Len(t, templates, 1)
			assert.Equal(t, "Template 2", templates[0].Name)
		},

		// ── Upsert ────────────────────────────────────────────────────────
		"UpsertTemplateCreatesWhenNew": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			id := uuid.New()
			body := map[string]interface{}{"name": "Upserted Template", "content": "{}"}
			w := h.Do(t, "PUT", fmt.Sprintf("/api/v1/templates/%s", id), body, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			tmpl := Decode[model.Template](t, w)
			assert.Equal(t, id, tmpl.ID)
			assert.Equal(t, "Upserted Template", tmpl.Name)
			assert.Equal(t, h.UserA.ID, tmpl.UserID)
		},

		"UpsertTemplateUpdatesWhenExists": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			created := createTestTemplate(t, h, h.UserA.ID, "Original Template")
			body := map[string]interface{}{"name": "Updated via Upsert", "content": "{}"}
			w := h.Do(t, "PUT", fmt.Sprintf("/api/v1/templates/%s", created.ID), body, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			tmpl := Decode[model.Template](t, w)
			assert.Equal(t, "Updated via Upsert", tmpl.Name)
		},

		"UpsertTemplateDifferentUserGets404": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			created := createTestTemplate(t, h, h.UserA.ID, "UserA Template")
			body := map[string]interface{}{"name": "Hijack", "content": "{}"}
			w := h.Do(t, "PUT", fmt.Sprintf("/api/v1/templates/%s", created.ID), body, h.UserB.ID)
			assert.Equal(t, http.StatusNotFound, w.Code)
		},

		// ── Write permission regression tests ────────────────────────────
		// GetByID applies the READ-access filter (owner, org member, or any
		// share). Update used to hand the row's own owner ID back to the
		// store's ownership guard regardless of who the caller was, so
		// anyone with mere read access could edit someone else's template.

		"OrgViewerCannotPatchOthersTemplate": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "POST", "/api/v1/orgs", map[string]interface{}{"name": "Acme"}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			org := Decode[model.OrgWithRole](t, w)

			w = h.Do(t, "POST", "/api/v1/orgs/"+org.ID.String()+"/members",
				map[string]interface{}{"userId": h.UserB.ID.String(), "role": "viewer"}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code, "add member: %s", w.Body.String())

			w = h.Do(t, "POST", "/api/v1/templates", map[string]interface{}{
				"name": "Alice Template", "content": "original",
				"orgId": org.ID.String(), "isPrivate": false,
			}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code, "create tmpl: %s", w.Body.String())
			tmpl := Decode[model.Template](t, w)

			w = h.Do(t, "PATCH", "/api/v1/templates/"+tmpl.ID.String(),
				map[string]interface{}{"name": "PWNED BY BOB", "content": "tampered"}, h.UserB.ID)
			assert.Equal(t, http.StatusForbidden, w.Code, "org viewer must not be able to modify another user's template: %s", w.Body.String())

			w2 := h.Do(t, "GET", "/api/v1/templates/"+tmpl.ID.String(), nil, h.UserA.ID)
			after := Decode[model.Template](t, w2)
			assert.Equal(t, "Alice Template", after.Name, "template must be unchanged")
		},

		"ShareViewerCannotPatchOthersTemplate": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			tmpl := createTestTemplate(t, h, h.UserA.ID, "Alice Personal")

			w := h.Do(t, "POST", "/api/v1/shares", map[string]interface{}{
				"resourceType": "template", "resourceId": tmpl.ID.String(),
				"sharedWithId": h.UserB.ID.String(), "permission": "viewer",
			}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code, "create share: %s", w.Body.String())

			w = h.Do(t, "PATCH", "/api/v1/templates/"+tmpl.ID.String(),
				map[string]interface{}{"name": "PWNED VIA SHARE"}, h.UserB.ID)
			assert.Equal(t, http.StatusForbidden, w.Code, "viewer-only share must not grant write access: %s", w.Body.String())

			w2 := h.Do(t, "GET", "/api/v1/templates/"+tmpl.ID.String(), nil, h.UserA.ID)
			after := Decode[model.Template](t, w2)
			assert.Equal(t, "Alice Personal", after.Name, "template must be unchanged")
		},

		"OrgEditorCanPatchOthersTemplate": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "POST", "/api/v1/orgs", map[string]interface{}{"name": "Acme"}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			org := Decode[model.OrgWithRole](t, w)

			w = h.Do(t, "POST", "/api/v1/orgs/"+org.ID.String()+"/members",
				map[string]interface{}{"userId": h.UserB.ID.String(), "role": "editor"}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code, "add member: %s", w.Body.String())

			w = h.Do(t, "POST", "/api/v1/templates", map[string]interface{}{
				"name": "Alice Template", "content": "original",
				"orgId": org.ID.String(), "isPrivate": false,
			}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			tmpl := Decode[model.Template](t, w)

			// An org EDITOR legitimately has write access — this must keep working.
			w = h.Do(t, "PATCH", "/api/v1/templates/"+tmpl.ID.String(),
				map[string]interface{}{"name": "Edited By Bob"}, h.UserB.ID)
			assert.Equal(t, http.StatusOK, w.Code, "org editor should be able to modify the org's template: %s", w.Body.String())
		},

		"ShareEditorCanPatchOthersTemplate": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			tmpl := createTestTemplate(t, h, h.UserA.ID, "Alice Personal")

			w := h.Do(t, "POST", "/api/v1/shares", map[string]interface{}{
				"resourceType": "template", "resourceId": tmpl.ID.String(),
				"sharedWithId": h.UserB.ID.String(), "permission": "editor",
			}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)

			// An editor share legitimately grants write access — must keep working.
			w = h.Do(t, "PATCH", "/api/v1/templates/"+tmpl.ID.String(),
				map[string]interface{}{"name": "Edited Via Share"}, h.UserB.ID)
			assert.Equal(t, http.StatusOK, w.Code, "editor share should grant write access: %s", w.Body.String())
		},
	})
}
