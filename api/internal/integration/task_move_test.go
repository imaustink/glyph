package integration

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/glyph/api/internal/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// adoptTask asks the server to move task taskID onto the bullet nodeID of
// pageID: POST /tasks/:id/adopt, used when a bullet is pasted into another
// note.
func adoptTask(t *testing.T, h *Harness, userID, taskID, pageID uuid.UUID, nodeID string) *httptest.ResponseRecorder {
	t.Helper()
	return h.Do(t, "POST", "/api/v1/tasks/"+taskID.String()+"/adopt", map[string]interface{}{
		"sourcePageId": pageID.String(),
		"sourceNodeId": nodeID,
	}, userID)
}

func errorCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	body := Decode[map[string]interface{}](t, w)
	code, _ := body["code"].(string)
	return code
}

// cutLinkedTask sets up note A with a TODO bullet n1 and its task, gives the
// task some metadata, then saves A without the bullet (a cut): the server
// soft-deletes the task as 'source_removed'.
func cutLinkedTask(t *testing.T, h *Harness, owner uuid.UUID) (noteA model.Page, task model.Task) {
	t.Helper()
	noteA = createPage(t, h, owner, "Note A")
	writeDoc(t, h, owner, noteA.ID, todoDoc(bullet{nodeID: "n1", text: "Buy milk"}))
	task = createLinkedTask(t, h, owner, noteA.ID, "n1")
	w := h.Do(t, "PATCH", "/api/v1/tasks/"+task.ID.String(), map[string]interface{}{
		"status":      "in-progress",
		"dueDate":     "2026-10-01",
		"description": "the oat kind",
		"priority":    "high",
		"tags":        []string{"groceries"},
	}, owner)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	writeDoc(t, h, owner, noteA.ID, todoDoc())
	require.False(t, taskVisible(t, h, owner, task.ID), "precondition: the cut soft-deletes the task")
	return noteA, task
}

// TestTaskMovesWithItsBullet covers the review follow-up to DI-10: a bullet
// cut from one note and pasted into another must take its task along — the
// same task, with its status, due date, description, tags and priority —
// rather than leave it deleted with the first note and start a bare new task
// in the second.
func TestTaskMovesWithItsBullet(t *testing.T) {
	RunSpecs(t, map[string]func(t *testing.T, h *Harness){
		"CutBulletMovesTaskWithItsMetadata": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			noteA, task := cutLinkedTask(t, h, h.UserA.ID)
			noteB := createPage(t, h, h.UserA.ID, "Note B")

			w := adoptTask(t, h, h.UserA.ID, task.ID, noteB.ID, "n9")
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			moved := Decode[model.Task](t, w)
			assert.Equal(t, task.ID, moved.ID, "the same task, not a new one")
			assert.Equal(t, model.TaskStatus("in-progress"), moved.Status)
			if assert.NotNil(t, moved.DueDate) {
				assert.Equal(t, "2026-10-01", *moved.DueDate)
			}
			assert.Equal(t, "the oat kind", moved.Description)
			assert.Equal(t, model.Priority("high"), moved.Priority)
			assert.Equal(t, []string{"groceries"}, moved.Tags)
			if assert.NotNil(t, moved.SourcePageID) && assert.NotNil(t, moved.SourceNodeID) {
				assert.Equal(t, noteB.ID, *moved.SourcePageID)
				assert.Equal(t, "n9", *moved.SourceNodeID)
			}
			assert.True(t, taskVisible(t, h, h.UserA.ID, task.ID), "the moved task is live again")

			// B's next save (with the bullet) keeps it; A's next save leaves it alone.
			writeDoc(t, h, h.UserA.ID, noteB.ID, todoDoc(bullet{nodeID: "n9", taskID: task.ID.String(), text: "Buy milk"}))
			writeDoc(t, h, h.UserA.ID, noteA.ID, todoDoc(bullet{nodeID: "n2", text: "Something else"}))
			assert.True(t, taskVisible(t, h, h.UserA.ID, task.ID))

			onB := Decode[[]model.Task](t, h.Do(t, "GET", "/api/v1/tasks?sourcePageId="+noteB.ID.String(), nil, h.UserA.ID))
			require.Len(t, onB, 1, "no duplicate task on the destination note")
			assert.Equal(t, task.ID, onB[0].ID)
			onA := Decode[[]model.Task](t, h.Do(t, "GET", "/api/v1/tasks?sourcePageId="+noteA.ID.String(), nil, h.UserA.ID))
			assert.Empty(t, onA)
		},

		"RetriedAdoptIsIdempotent": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			_, task := cutLinkedTask(t, h, h.UserA.ID)
			noteB := createPage(t, h, h.UserA.ID, "Note B")

			require.Equal(t, http.StatusOK, adoptTask(t, h, h.UserA.ID, task.ID, noteB.ID, "n9").Code)
			w := adoptTask(t, h, h.UserA.ID, task.ID, noteB.ID, "n9")
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			assert.Equal(t, task.ID, Decode[model.Task](t, w).ID)
		},

		// A copy: the task's bullet is still on its note. The server can't
		// tell that from a cut whose save hasn't landed yet, so it refuses
		// with a code the client may retry on, and changes nothing.
		"LiveTaskIsNotMoved": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			noteA := createPage(t, h, h.UserA.ID, "Note A")
			writeDoc(t, h, h.UserA.ID, noteA.ID, todoDoc(bullet{nodeID: "n1"}))
			task := createLinkedTask(t, h, h.UserA.ID, noteA.ID, "n1")
			noteB := createPage(t, h, h.UserA.ID, "Note B")

			w := adoptTask(t, h, h.UserA.ID, task.ID, noteB.ID, "n9")
			require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
			assert.Equal(t, "source_live", errorCode(t, w))

			got := Decode[model.Task](t, h.Do(t, "GET", "/api/v1/tasks/"+task.ID.String(), nil, h.UserA.ID))
			require.NotNil(t, got.SourcePageID)
			assert.Equal(t, noteA.ID, *got.SourcePageID, "the task stays on its note")

			// Once the cut's save lands, the same request succeeds.
			writeDoc(t, h, h.UserA.ID, noteA.ID, todoDoc())
			assert.Equal(t, http.StatusOK, adoptTask(t, h, h.UserA.ID, task.ID, noteB.ID, "n9").Code)
		},

		"UserDeletedTaskIsNotResurrected": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			noteA := createPage(t, h, h.UserA.ID, "Note A")
			writeDoc(t, h, h.UserA.ID, noteA.ID, todoDoc(bullet{nodeID: "n1"}))
			task := createLinkedTask(t, h, h.UserA.ID, noteA.ID, "n1")
			require.Equal(t, http.StatusNoContent, h.Do(t, "DELETE", "/api/v1/tasks/"+task.ID.String(), nil, h.UserA.ID).Code)
			writeDoc(t, h, h.UserA.ID, noteA.ID, todoDoc())
			noteB := createPage(t, h, h.UserA.ID, "Note B")

			w := adoptTask(t, h, h.UserA.ID, task.ID, noteB.ID, "n9")
			require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
			assert.Equal(t, "not_movable", errorCode(t, w))
			assert.False(t, taskVisible(t, h, h.UserA.ID, task.ID), "a task the user deleted stays deleted")
		},

		"BulletAlreadyLinkedIsRefused": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			_, task := cutLinkedTask(t, h, h.UserA.ID)
			noteB := createPage(t, h, h.UserA.ID, "Note B")
			writeDoc(t, h, h.UserA.ID, noteB.ID, todoDoc(bullet{nodeID: "n9"}))
			other := createLinkedTask(t, h, h.UserA.ID, noteB.ID, "n9")

			w := adoptTask(t, h, h.UserA.ID, task.ID, noteB.ID, "n9")
			require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
			assert.Equal(t, "source_taken", errorCode(t, w))
			assert.True(t, taskVisible(t, h, h.UserA.ID, other.ID))
			assert.False(t, taskVisible(t, h, h.UserA.ID, task.ID), "the refused task stays where it was")
		},

		// A pasted (or guessed) task id must not let someone take a task
		// from a note they can't edit.
		"CannotTakeATaskFromANoteYouCannotEdit": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			_, task := cutLinkedTask(t, h, h.UserA.ID)
			bobsNote := createPage(t, h, h.UserB.ID, "Bob's note")

			w := adoptTask(t, h, h.UserB.ID, task.ID, bobsNote.ID, "n9")
			assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())

			// Untouched: Alice can still move it herself.
			aliceB := createPage(t, h, h.UserA.ID, "Note B")
			w = adoptTask(t, h, h.UserA.ID, task.ID, aliceB.ID, "n9")
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			assert.Equal(t, h.UserA.ID, Decode[model.Task](t, w).UserID)
		},

		"ViewerOfTheSourceNoteCannotTakeItsTask": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			noteA, task := cutLinkedTask(t, h, h.UserA.ID)
			sharePage(t, h, noteA.ID, h.UserA.ID, h.UserB.ID, "viewer")
			bobsNote := createPage(t, h, h.UserB.ID, "Bob's note")

			w := adoptTask(t, h, h.UserB.ID, task.ID, bobsNote.ID, "n9")
			assert.Contains(t, []int{http.StatusForbidden, http.StatusNotFound}, w.Code, w.Body.String())
			assert.Equal(t, http.StatusOK, adoptTask(t, h, h.UserA.ID, task.ID, noteA.ID, "n5").Code,
				"the refused attempt left the task where it was")
		},

		// Someone who may edit the source note moves the bullet into their own
		// note: the task goes with it and, like every note task, now belongs
		// to the note it is on.
		"EditorOfTheSourceNoteMovesTheTaskIntoTheirNote": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			noteA, task := cutLinkedTask(t, h, h.UserA.ID)
			sharePage(t, h, noteA.ID, h.UserA.ID, h.UserB.ID, "editor")
			bobsNote := createPage(t, h, h.UserB.ID, "Bob's note")

			w := adoptTask(t, h, h.UserB.ID, task.ID, bobsNote.ID, "n9")
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			moved := Decode[model.Task](t, w)
			assert.Equal(t, task.ID, moved.ID)
			assert.Equal(t, h.UserB.ID, moved.UserID, "owned by the destination note's owner")
			assert.Equal(t, model.TaskStatus("in-progress"), moved.Status)
		},

		"CannotMoveIntoANoteYouCannotEdit": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			_, task := cutLinkedTask(t, h, h.UserA.ID)
			bobsNote := createPage(t, h, h.UserB.ID, "Bob's note")
			sharePage(t, h, bobsNote.ID, h.UserB.ID, h.UserA.ID, "viewer")

			w := adoptTask(t, h, h.UserA.ID, task.ID, bobsNote.ID, "n9")
			assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
			hidden := createPage(t, h, h.UserB.ID, "Bob's private note")
			w = adoptTask(t, h, h.UserA.ID, task.ID, hidden.ID, "n9")
			assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())

			aliceB := createPage(t, h, h.UserA.ID, "Note B")
			assert.Equal(t, http.StatusOK, adoptTask(t, h, h.UserA.ID, task.ID, aliceB.ID, "n9").Code,
				"the refused attempts left the task where it was")
		},

		"UnknownTaskIsNotFound": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			noteB := createPage(t, h, h.UserA.ID, "Note B")
			assert.Equal(t, http.StatusNotFound, adoptTask(t, h, h.UserA.ID, uuid.New(), noteB.ID, "n9").Code)
			// And the endpoint exists: a real cut task can be adopted.
			_, task := cutLinkedTask(t, h, h.UserA.ID)
			assert.Equal(t, http.StatusOK, adoptTask(t, h, h.UserA.ID, task.ID, noteB.ID, "n9").Code)
		},

		"MissingBulletIsABadRequest": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			_, task := cutLinkedTask(t, h, h.UserA.ID)
			noteB := createPage(t, h, h.UserA.ID, "Note B")
			w := h.Do(t, "POST", "/api/v1/tasks/"+task.ID.String()+"/adopt",
				map[string]interface{}{"sourcePageId": noteB.ID.String()}, h.UserA.ID)
			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		},
	})
}
