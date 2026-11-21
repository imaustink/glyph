package integration

import (
	"net/http"
	"testing"

	"github.com/glyph/api/internal/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Sharing a folder shares its board — and the tasks on it. Regression for
// #82: the invitee could see the shared folder's board, but every task on it
// 404'd when opened (GET /tasks/:id), because the task read filter only
// honored task and page shares, never folder shares.
func TestFolderShareTasks(t *testing.T) {
	type fixture struct {
		folder, subfolder, note, other  model.Page
		noteTask, folderTask, otherTask model.Task
		shareID                         string
	}
	setup := func(t *testing.T, h *Harness, permission string) fixture {
		t.Helper()
		h.ResetDB(t)
		var f fixture
		f.folder = Decode[model.Page](t, h.Do(t, "POST", "/api/v1/pages",
			map[string]interface{}{"title": "Shared Folder", "type": "folder"}, h.UserA.ID))
		f.subfolder = Decode[model.Page](t, h.Do(t, "POST", "/api/v1/pages",
			map[string]interface{}{"title": "Sub", "type": "folder", "parentId": f.folder.ID.String()}, h.UserA.ID))
		f.note = Decode[model.Page](t, h.Do(t, "POST", "/api/v1/pages",
			map[string]interface{}{"title": "Nested Note", "type": "page", "parentId": f.subfolder.ID.String()}, h.UserA.ID))
		f.other = Decode[model.Page](t, h.Do(t, "POST", "/api/v1/pages",
			map[string]interface{}{"title": "Unshared Note", "type": "page"}, h.UserA.ID))

		f.noteTask = Decode[model.Task](t, h.Do(t, "POST", "/api/v1/tasks",
			map[string]interface{}{"title": "From nested note", "sourcePageId": f.note.ID.String()}, h.UserA.ID))
		f.folderTask = Decode[model.Task](t, h.Do(t, "POST", "/api/v1/tasks",
			map[string]interface{}{"title": "Assigned to folder", "folderId": f.folder.ID.String()}, h.UserA.ID))
		f.otherTask = Decode[model.Task](t, h.Do(t, "POST", "/api/v1/tasks",
			map[string]interface{}{"title": "Elsewhere", "sourcePageId": f.other.ID.String()}, h.UserA.ID))

		w := h.Do(t, "POST", "/api/v1/shares", map[string]interface{}{
			"resourceType": "folder", "resourceId": f.folder.ID.String(),
			"sharedWithId": h.UserB.ID.String(), "permission": permission,
		}, h.UserA.ID)
		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		f.shareID = Decode[map[string]interface{}](t, w)["id"].(string)
		return f
	}

	RunSpecs(t, map[string]func(t *testing.T, h *Harness){
		"InviteeCanOpenEveryTaskOnTheSharedBoard": func(t *testing.T, h *Harness) {
			f := setup(t, h, "viewer")

			w := h.Do(t, "GET", "/api/v1/folders/"+f.folder.ID.String()+"/tasks", nil, h.UserB.ID)
			require.Equal(t, http.StatusOK, w.Code)
			board := Decode[[]model.Task](t, w)
			require.Len(t, board, 2)

			for _, task := range board {
				w := h.Do(t, "GET", "/api/v1/tasks/"+task.ID.String(), nil, h.UserB.ID)
				require.Equal(t, http.StatusOK, w.Code, "task %q on the shared board should open", task.Title)
				assert.Equal(t, task.ID, Decode[model.Task](t, w).ID)
			}
		},
		"InviteeCannotOpenTasksOutsideTheSharedFolder": func(t *testing.T, h *Harness) {
			f := setup(t, h, "viewer")
			w := h.Do(t, "GET", "/api/v1/tasks/"+f.otherTask.ID.String(), nil, h.UserB.ID)
			assert.Equal(t, http.StatusNotFound, w.Code)
		},
		"RevokingTheFolderShareRevokesItsTasks": func(t *testing.T, h *Harness) {
			f := setup(t, h, "viewer")
			require.Equal(t, http.StatusNoContent, h.Do(t, "DELETE", "/api/v1/shares/"+f.shareID, nil, h.UserA.ID).Code)
			for _, task := range []model.Task{f.noteTask, f.folderTask} {
				w := h.Do(t, "GET", "/api/v1/tasks/"+task.ID.String(), nil, h.UserB.ID)
				assert.Equal(t, http.StatusNotFound, w.Code, "task %q", task.Title)
			}
		},
		// A share only passes on what its owner can read. Carol writes a
		// private note in Alice's org folder; Alice shares the folder with
		// Bob. Carol's private task must stay hidden from Bob, while Alice's
		// own tasks on the board stay readable.
		"PrivateTasksOfOtherMembersStayPrivate": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			email, name := "carol@test.com", "Carol"
			carol, err := h.UserStore.Upsert(t.Context(), "sub-carol", "test-issuer", &email, &name)
			require.NoError(t, err)

			w := h.Do(t, "POST", "/api/v1/orgs", map[string]interface{}{"name": "Alice Corp"}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
			orgID := Decode[model.OrgWithRole](t, w).ID.String()
			addOrgMember(t, h, h.UserA.ID, orgID, carol.ID, "editor")
			folder, _, aliceTask := orgFolder(t, h, h.UserA.ID, orgID)

			w = h.Do(t, "POST", "/api/v1/pages", map[string]interface{}{
				"title": "Carol's private note", "type": "page", "parentId": folder.ID.String(),
				"orgId": orgID, "isPrivate": true,
			}, carol.ID)
			require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
			privateNote := Decode[model.Page](t, w)
			w = h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{
				"title": "Carol's private task", "sourcePageId": privateNote.ID.String(),
			}, carol.ID)
			require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
			privateTask := Decode[model.Task](t, w)
			require.True(t, privateTask.IsPrivate, "a private note's task must be private")

			w = h.Do(t, "POST", "/api/v1/shares", map[string]interface{}{
				"resourceType": "folder", "resourceId": folder.ID.String(),
				"sharedWithId": h.UserB.ID.String(), "permission": "viewer",
			}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

			assert.Equal(t, http.StatusOK, h.Do(t, "GET", "/api/v1/tasks/"+aliceTask.ID.String(), nil, h.UserB.ID).Code,
				"the folder owner's task on the shared board should open")
			assert.Equal(t, http.StatusNotFound, h.Do(t, "GET", "/api/v1/tasks/"+privateTask.ID.String(), nil, h.UserB.ID).Code,
				"another member's private task must not leak through the folder share")

			w = h.Do(t, "GET", "/api/v1/tasks", nil, h.UserB.ID)
			require.Equal(t, http.StatusOK, w.Code)
			listed := map[uuid.UUID]bool{}
			for _, task := range Decode[[]model.Task](t, w) {
				listed[task.ID] = true
			}
			assert.True(t, listed[aliceTask.ID], "the folder owner's task should be listed")
			assert.False(t, listed[privateTask.ID], "another member's private task must not be listed")
		},
		"ViewerShareStillCannotEditTasks": func(t *testing.T, h *Harness) {
			f := setup(t, h, "viewer")
			w := h.Do(t, "PATCH", "/api/v1/tasks/"+f.noteTask.ID.String(),
				map[string]interface{}{"title": "Hijacked"}, h.UserB.ID)
			assert.Equal(t, http.StatusForbidden, w.Code)
		},
	})
}
