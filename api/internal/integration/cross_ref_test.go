package integration

import (
	"net/http"
	"testing"

	"github.com/glyph/api/internal/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCrossReferenceValidation covers DI-23: references to other rows
// (source page, folder, org) must be checked against what the caller may
// write, explicit nulls must clear them, and a task's org must follow its
// note.
func TestCrossReferenceValidation(t *testing.T) {
	RunSpecs(t, map[string]func(t *testing.T, h *Harness){
		// ── PATCH /tasks sourcePageId ─────────────────────────────────────
		"PatchTaskCannotPointAtUnreadablePage": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			bobPage := createPage(t, h, h.UserB.ID, "Bob's private note")
			task := createTestTask(t, h, h.UserA.ID, "Alice's task")

			w := h.Do(t, "PATCH", "/api/v1/tasks/"+task.ID.String(), map[string]interface{}{
				"sourcePageId": bobPage.ID.String(), "sourceNodeId": "n1",
			}, h.UserA.ID)
			assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())

			got := Decode[model.Task](t, h.Do(t, "GET", "/api/v1/tasks/"+task.ID.String(), nil, h.UserA.ID))
			assert.Nil(t, got.SourcePageID, "task was attached to a page its owner cannot see")
		},

		"PatchTaskCannotPointAtReadOnlyPage": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			bobPage := createPage(t, h, h.UserB.ID, "Bob's note")
			sharePage(t, h, bobPage.ID, h.UserB.ID, h.UserA.ID, "viewer")
			task := createTestTask(t, h, h.UserA.ID, "Alice's task")

			w := h.Do(t, "PATCH", "/api/v1/tasks/"+task.ID.String(), map[string]interface{}{
				"sourcePageId": bobPage.ID.String(), "sourceNodeId": "n1",
			}, h.UserA.ID)
			assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
		},

		"PatchTaskOntoNoteReOwnsItToThePageOwner": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			alicePage := createPage(t, h, h.UserA.ID, "Alice's note")
			sharePage(t, h, alicePage.ID, h.UserA.ID, h.UserB.ID, "editor")
			task := createTestTask(t, h, h.UserB.ID, "Bob's task")

			w := h.Do(t, "PATCH", "/api/v1/tasks/"+task.ID.String(), map[string]interface{}{
				"sourcePageId": alicePage.ID.String(), "sourceNodeId": "n1",
			}, h.UserB.ID)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			got := Decode[model.Task](t, w)
			assert.Equal(t, h.UserA.ID, got.UserID, "a note's tasks belong to the note's owner")
		},

		// ── folderId on task and lane writes ──────────────────────────────
		"CreateTaskCannotPlantOnOthersFolderBoard": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			bobFolder := createFolder(t, h, h.UserB.ID, "Bob's board")
			w := h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{
				"title": "planted", "folderId": bobFolder.ID.String(),
			}, h.UserA.ID)
			assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())

			w = h.Do(t, "PUT", "/api/v1/tasks/"+uuid.NewString(), map[string]interface{}{
				"title": "planted", "folderId": bobFolder.ID.String(),
			}, h.UserA.ID)
			assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())

			w = h.Do(t, "GET", "/api/v1/folders/"+bobFolder.ID.String(), nil, h.UserB.ID)
			require.Equal(t, http.StatusOK, w.Code)
			assert.NotContains(t, w.Body.String(), "planted")
		},

		"CreateTaskFolderIdMustBeAFolder": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createPage(t, h, h.UserA.ID, "Just a page")
			w := h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{
				"title": "t", "folderId": page.ID.String(),
			}, h.UserA.ID)
			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		},

		"CreateTaskOnOwnFolderBoardStillWorks": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			folder := createFolder(t, h, h.UserA.ID, "Board")
			w := h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{
				"title": "t", "folderId": folder.ID.String(),
			}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		},

		"CreateLaneCannotPlantOnOthersFolderBoard": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			bobFolder := createFolder(t, h, h.UserB.ID, "Bob's board")
			lane := map[string]interface{}{"title": "planted", "folderId": bobFolder.ID.String()}

			w := h.Do(t, "POST", "/api/v1/lanes", lane, h.UserA.ID)
			assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
			w = h.Do(t, "POST", "/api/v1/lanes/batch", []interface{}{lane}, h.UserA.ID)
			assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
			w = h.Do(t, "PUT", "/api/v1/lanes/"+uuid.NewString(), lane, h.UserA.ID)
			assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())

			w = h.Do(t, "GET", "/api/v1/folders/"+bobFolder.ID.String()+"/lanes", nil, h.UserB.ID)
			require.Equal(t, http.StatusOK, w.Code)
			assert.Empty(t, Decode[[]model.Lane](t, w))
		},

		// ── explicit nulls ────────────────────────────────────────────────
		"PatchNullOrgMovesToPersonal": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			orgID := createOrgAs(t, h, h.UserA.ID, "Org")

			w := h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{"title": "t", "orgId": orgID}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			task := Decode[model.Task](t, w)
			w = h.Do(t, "PATCH", "/api/v1/tasks/"+task.ID.String(), map[string]interface{}{"orgId": nil}, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			assert.Nil(t, Decode[model.Task](t, w).OrgID, "task orgId: null was ignored")

			w = h.Do(t, "POST", "/api/v1/pages", map[string]interface{}{"title": "p", "orgId": orgID}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			page := Decode[model.Page](t, w)
			w = h.Do(t, "PATCH", "/api/v1/pages/"+page.ID.String(), map[string]interface{}{"orgId": nil}, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			assert.Nil(t, Decode[model.Page](t, w).OrgID, "page orgId: null was ignored")

			w = h.Do(t, "POST", "/api/v1/templates", map[string]interface{}{"name": "tm", "orgId": orgID}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			tmpl := Decode[model.Template](t, w)
			w = h.Do(t, "PATCH", "/api/v1/templates/"+tmpl.ID.String(), map[string]interface{}{"orgId": nil}, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			assert.Nil(t, Decode[model.Template](t, w).OrgID, "template orgId: null was ignored")
		},

		"PatchTemplateNullClearsDefaultFolderAndTrigger": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			folder := createFolder(t, h, h.UserA.ID, "Inbox")
			w := h.Do(t, "POST", "/api/v1/templates", map[string]interface{}{
				"name": "tm", "defaultFolderId": folder.ID.String(),
				"todoTrigger": map[string]interface{}{"pattern": "TODO", "blockTypes": []string{"listItem"}},
			}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
			tmpl := Decode[model.Template](t, w)
			require.NotNil(t, tmpl.DefaultFolderID)
			require.NotNil(t, tmpl.TodoTrigger)

			w = h.Do(t, "PATCH", "/api/v1/templates/"+tmpl.ID.String(),
				map[string]interface{}{"defaultFolderId": nil, "todoTrigger": nil}, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			got := Decode[model.Template](t, w)
			assert.Nil(t, got.DefaultFolderID, "defaultFolderId: null was ignored")
			assert.Nil(t, got.TodoTrigger, "todoTrigger: null was ignored")
		},

		// ── org follows the note ──────────────────────────────────────────
		"TaskInheritsOrgAndPrivacyFromSourcePage": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			orgID := createOrgAs(t, h, h.UserA.ID, "Org")
			w := h.Do(t, "POST", "/api/v1/pages", map[string]interface{}{"title": "Team note", "orgId": orgID}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			page := Decode[model.Page](t, w)
			w = h.Do(t, "PATCH", "/api/v1/pages/"+page.ID.String(), map[string]interface{}{"isPrivate": false}, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)

			// Linked (bullet) task, plain page task, and PUT: none send an org.
			w = h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{
				"title": "linked", "sourcePageId": page.ID.String(), "sourceNodeId": "n1", "isPrivate": true,
			}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
			linked := Decode[model.Task](t, w)
			w = h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{
				"title": "plain", "sourcePageId": page.ID.String(),
			}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
			plain := Decode[model.Task](t, w)
			w = h.Do(t, "PUT", "/api/v1/tasks/"+uuid.NewString(), map[string]interface{}{
				"title": "put", "sourcePageId": page.ID.String(), "sourceNodeId": "n2",
			}, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			put := Decode[model.Task](t, w)

			for _, task := range []model.Task{linked, plain, put} {
				if assert.NotNil(t, task.OrgID, "%s: task did not inherit the note's org", task.Title) {
					assert.Equal(t, orgID, task.OrgID.String(), task.Title)
				}
				assert.False(t, task.IsPrivate, "%s: task did not inherit the note's privacy", task.Title)
			}
		},

		"PageOrgChangeCascadesToDescendantsAndTheirTasks": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			orgID := createOrgAs(t, h, h.UserA.ID, "Org")
			folder := createFolder(t, h, h.UserA.ID, "Project")
			w := h.Do(t, "POST", "/api/v1/pages", map[string]interface{}{
				"title": "Child", "parentId": folder.ID.String(),
			}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			child := Decode[model.Page](t, w)
			w = h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{
				"title": "child task", "sourcePageId": child.ID.String(), "sourceNodeId": "n1",
			}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			task := Decode[model.Task](t, w)

			w = h.Do(t, "PATCH", "/api/v1/pages/"+folder.ID.String(), map[string]interface{}{"orgId": orgID}, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())

			gotChild := Decode[model.Page](t, h.Do(t, "GET", "/api/v1/pages/"+child.ID.String(), nil, h.UserA.ID))
			if assert.NotNil(t, gotChild.OrgID, "child page did not move with its folder") {
				assert.Equal(t, orgID, gotChild.OrgID.String())
			}
			gotTask := Decode[model.Task](t, h.Do(t, "GET", "/api/v1/tasks/"+task.ID.String(), nil, h.UserA.ID))
			if assert.NotNil(t, gotTask.OrgID, "the child's task did not move with its note") {
				assert.Equal(t, orgID, gotTask.OrgID.String())
			}

			// And back to Personal.
			w = h.Do(t, "PATCH", "/api/v1/pages/"+folder.ID.String(), map[string]interface{}{"orgId": nil}, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			gotChild = Decode[model.Page](t, h.Do(t, "GET", "/api/v1/pages/"+child.ID.String(), nil, h.UserA.ID))
			assert.Nil(t, gotChild.OrgID)
			gotTask = Decode[model.Task](t, h.Do(t, "GET", "/api/v1/tasks/"+task.ID.String(), nil, h.UserA.ID))
			assert.Nil(t, gotTask.OrgID)
		},
	})
}
