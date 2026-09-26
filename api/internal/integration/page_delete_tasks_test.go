package integration

import (
	"net/http"
	"testing"

	"github.com/glyph/api/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPageDeleteTasks covers which tasks DELETE /pages/:id takes with the
// deleted subtree (DI-21) and which it leaves alone.
func TestPageDeleteTasks(t *testing.T) {
	RunSpecs(t, map[string]func(t *testing.T, h *Harness){
		// An editor on a shared folder can file their own tasks on its board
		// (CanUseFolder). Deleting the folder must not delete those — they
		// aren't the deleter's — but unfile them, as before DI-21. The
		// folder owner's own board tasks and the notes' tasks go with it.
		"FolderDeleteUnfilesOtherUsersBoardTasks": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			f := createFolder(t, h, h.UserA.ID, "Shared board")
			shareFolder(t, h, f.ID.String(), h.UserA.ID, h.UserB.ID, "editor")
			w := h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{"title": "Bob's board task", "folderId": f.ID.String(), "status": "in-progress"}, h.UserB.ID)
			require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
			bobs := Decode[model.Task](t, w)
			w = h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{"title": "Alice's board task", "folderId": f.ID.String()}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
			alices := Decode[model.Task](t, w)

			// Bob, editing Alice's shared note in the folder, adds a task to
			// it: a note's tasks are the note owner's (migration 000021), so
			// it is Alice's and goes with her note.
			note := createChild(t, h, h.UserA.ID, f.ID, "page", "Note")
			sharePage(t, h, note.ID, h.UserA.ID, h.UserB.ID, "editor")
			writeDoc(t, h, h.UserA.ID, note.ID, todoDoc(bullet{nodeID: "n1"}))
			noteTask := createLinkedTask(t, h, h.UserB.ID, note.ID, "n1")
			require.Equal(t, h.UserA.ID, noteTask.UserID, "a note's task belongs to the note's owner")

			require.Equal(t, http.StatusNoContent, h.Do(t, "DELETE", "/api/v1/pages/"+f.ID.String(), nil, h.UserA.ID).Code)

			w = h.Do(t, "GET", "/api/v1/tasks/"+bobs.ID.String(), nil, h.UserB.ID)
			require.Equal(t, http.StatusOK, w.Code, "Alice deleting her folder deleted Bob's task: %s", w.Body.String())
			got := Decode[model.Task](t, w)
			assert.Nil(t, got.FolderID, "Bob's task still points at the deleted folder")
			assert.Equal(t, "Bob's board task", got.Title)
			assert.Equal(t, model.StatusInProgress, got.Status)

			assert.False(t, taskVisible(t, h, h.UserA.ID, alices.ID), "the deleter's own board task goes with the folder")
			assert.False(t, taskVisible(t, h, h.UserA.ID, noteTask.ID), "the note's task goes with the note")
		},
	})
}
