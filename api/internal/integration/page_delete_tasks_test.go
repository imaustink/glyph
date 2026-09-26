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

		// "Keep Tasks": ?keepTasks=true detaches the subtree's tasks instead
		// of deleting them. They become standalone tasks (no note, no bullet,
		// no folder board) with everything else as it was.
		"KeepTasksDetachesNoteAndBoardTasks": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			f := createFolder(t, h, h.UserA.ID, "F")
			note := createChild(t, h, h.UserA.ID, f.ID, "page", "Note")
			writeDoc(t, h, h.UserA.ID, note.ID, todoDoc(bullet{nodeID: "n1"}))
			body := linkedTaskBody(note.ID, "n1", "Note task")
			for k, v := range map[string]interface{}{
				"status": "in-progress", "priority": "high", "tags": []string{"x"},
				"description": "details", "dueDate": "2026-10-01",
			} {
				body[k] = v
			}
			w := h.Do(t, "POST", "/api/v1/tasks", body, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
			noteTask := Decode[model.Task](t, w)
			w = h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{"title": "Board task", "folderId": f.ID.String(), "status": "done"}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
			boardTask := Decode[model.Task](t, w)
			other := createFolder(t, h, h.UserA.ID, "Other")
			w = h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{"title": "Elsewhere", "folderId": other.ID.String()}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
			elsewhere := Decode[model.Task](t, w)

			w = h.Do(t, "DELETE", "/api/v1/pages/"+f.ID.String()+"?keepTasks=true", nil, h.UserA.ID)
			require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())

			w = h.Do(t, "GET", "/api/v1/tasks/"+noteTask.ID.String(), nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code, "keepTasks deleted the note's task: %s", w.Body.String())
			got := Decode[model.Task](t, w)
			assert.Nil(t, got.SourcePageID, "the kept task still points at the deleted note")
			assert.Nil(t, got.SourceNodeID, "the kept task still points at the deleted bullet")
			assert.Nil(t, got.FolderID)
			assert.Equal(t, "Note task", got.Title)
			assert.Equal(t, model.StatusInProgress, got.Status)
			assert.Equal(t, model.Priority("high"), got.Priority)
			assert.Equal(t, []string{"x"}, got.Tags)
			assert.Equal(t, "details", got.Description)
			if assert.NotNil(t, got.DueDate) {
				assert.Equal(t, "2026-10-01", (*got.DueDate)[:10])
			}

			w = h.Do(t, "GET", "/api/v1/tasks/"+boardTask.ID.String(), nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code, "keepTasks deleted the board task: %s", w.Body.String())
			got = Decode[model.Task](t, w)
			assert.Nil(t, got.FolderID, "the kept task still points at the deleted folder")
			assert.Equal(t, model.StatusDone, got.Status)

			if f := getTask(t, h, h.UserA.ID, elsewhere.ID).FolderID; assert.NotNil(t, f) {
				assert.Equal(t, other.ID, *f, "a task outside the subtree was unfiled")
			}
		},

		// Without the opt-in the subtree's tasks are soft-deleted (DI-21).
		"DeleteWithoutKeepTasksSoftDeletesTasks": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			note := createPage(t, h, h.UserA.ID, "Note")
			writeDoc(t, h, h.UserA.ID, note.ID, todoDoc(bullet{nodeID: "n1"}))
			linked := createLinkedTask(t, h, h.UserA.ID, note.ID, "n1")

			w := h.Do(t, "DELETE", "/api/v1/pages/"+note.ID.String()+"?keepTasks=false", nil, h.UserA.ID)
			require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
			assert.False(t, taskVisible(t, h, h.UserA.ID, linked.ID))
		},

		// keepTasks never reaches another user's task: theirs is unfiled
		// either way.
		"KeepTasksUnfilesOtherUsersBoardTasks": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			f := createFolder(t, h, h.UserA.ID, "Shared board")
			shareFolder(t, h, f.ID.String(), h.UserA.ID, h.UserB.ID, "editor")
			w := h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{"title": "Bob's", "folderId": f.ID.String()}, h.UserB.ID)
			require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
			bobs := Decode[model.Task](t, w)

			w = h.Do(t, "DELETE", "/api/v1/pages/"+f.ID.String()+"?keepTasks=true", nil, h.UserA.ID)
			require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
			got := getTask(t, h, h.UserB.ID, bobs.ID)
			assert.Nil(t, got.FolderID)
			assert.Equal(t, h.UserB.ID, got.UserID)
		},

		"KeepTasksMustBeABoolean": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			note := createPage(t, h, h.UserA.ID, "Note")
			w := h.Do(t, "DELETE", "/api/v1/pages/"+note.ID.String()+"?keepTasks=maybe", nil, h.UserA.ID)
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			getPage(t, h, h.UserA.ID, note.ID) // not deleted
		},
	})
}
