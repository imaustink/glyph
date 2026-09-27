package integration

import (
	"net/http"
	"testing"

	"github.com/glyph/api/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLinkedTaskUserDeleteIsFinal covers the audit's Low item "CreateLinked
// restores deleted_reason='user' tasks": only a task the server removed
// because its bullet disappeared ('source_removed') may come back. One the
// user deleted stays deleted; a new request for that bullet gets a new task.
func TestLinkedTaskUserDeleteIsFinal(t *testing.T) {
	RunSpecs(t, map[string]func(t *testing.T, h *Harness){
		"RecreatingBulletTaskAfterUserDeleteDoesNotResurrect": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createPage(t, h, h.UserA.ID, "Note")
			linked := map[string]interface{}{"title": "Do it", "sourcePageId": page.ID.String(), "sourceNodeId": "n1"}

			w := h.Do(t, "POST", "/api/v1/tasks", linked, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
			first := Decode[model.Task](t, w)
			require.Equal(t, http.StatusNoContent, h.Do(t, "DELETE", "/api/v1/tasks/"+first.ID.String(), nil, h.UserA.ID).Code)

			w = h.Do(t, "POST", "/api/v1/tasks", linked, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code, "a user-deleted task was restored: %s", w.Body.String())
			second := Decode[model.Task](t, w)
			assert.NotEqual(t, first.ID, second.ID)
			assert.Equal(t, http.StatusNotFound, h.Do(t, "GET", "/api/v1/tasks/"+first.ID.String(), nil, h.UserA.ID).Code,
				"the deleted task is visible again")
		},
	})
}
