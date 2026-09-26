package integration

import (
	"context"
	"net/http"
	"testing"

	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStoreParity covers the audit's "Memstore vs Postgres divergences" for
// tasks, lanes, templates and orgs: both backends must fail the same way,
// with the typed sentinel errors handlers rely on.
func TestStoreParity(t *testing.T) {
	RunSpecs(t, map[string]func(t *testing.T, h *Harness){
		"MissingRowsAreErrNotFound": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			ctx := context.Background()
			missing := uuid.New()

			_, err := h.TaskStore.GetByID(ctx, missing, h.UserA.ID)
			assert.ErrorIs(t, err, store.ErrNotFound, "task GetByID")
			assert.ErrorIs(t, h.TaskStore.Delete(ctx, missing, h.UserA.ID), store.ErrNotFound, "task Delete")

			_, err = h.LaneStore.GetByID(ctx, missing, h.UserA.ID)
			assert.ErrorIs(t, err, store.ErrNotFound, "lane GetByID")
			assert.ErrorIs(t, h.LaneStore.Delete(ctx, missing, h.UserA.ID), store.ErrNotFound, "lane Delete")
			_, err = h.LaneStore.Update(ctx, &model.Lane{ID: missing, UserID: h.UserA.ID, Title: "x"})
			assert.ErrorIs(t, err, store.ErrNotFound, "lane Update")

			_, err = h.TemplateStore.GetByID(ctx, missing, h.UserA.ID)
			assert.ErrorIs(t, err, store.ErrNotFound, "template GetByID")
			assert.ErrorIs(t, h.TemplateStore.Delete(ctx, missing, h.UserA.ID), store.ErrNotFound, "template Delete")
			_, err = h.TemplateStore.Update(ctx, &model.Template{ID: missing, UserID: h.UserA.ID, Name: "x"})
			assert.ErrorIs(t, err, store.ErrNotFound, "template Update")

			_, err = h.OrgStore.GetByID(ctx, missing)
			assert.ErrorIs(t, err, store.ErrNotFound, "org GetByID")
			_, err = h.OrgStore.Update(ctx, &model.Organization{ID: missing, Name: "x"})
			assert.ErrorIs(t, err, store.ErrNotFound, "org Update")
			_, err = h.OrgStore.UpdateMemberRole(ctx, missing, h.UserA.ID, model.OrgRoleEditor)
			assert.ErrorIs(t, err, store.ErrNotFound, "org UpdateMemberRole")

			_, err = h.UserStore.GetByID(ctx, missing)
			assert.ErrorIs(t, err, store.ErrNotFound, "user GetByID")
		},

		"DeleteByOtherUserIsErrNotFound": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			ctx := context.Background()
			task := createTestTask(t, h, h.UserA.ID, "Alice's")
			assert.ErrorIs(t, h.TaskStore.Delete(ctx, task.ID, h.UserB.ID), store.ErrNotFound)
			_, err := h.TaskStore.GetByID(ctx, task.ID, h.UserA.ID)
			assert.NoError(t, err, "another user's delete removed the task")
		},

		"OrgDeleteMovesResourcesToPersonal": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			orgID := createOrgAs(t, h, h.UserA.ID, "Doomed")
			w := h.Do(t, "POST", "/api/v1/pages", map[string]interface{}{"title": "p", "orgId": orgID}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			page := Decode[model.Page](t, w)
			w = h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{"title": "t", "orgId": orgID}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			task := Decode[model.Task](t, w)
			w = h.Do(t, "POST", "/api/v1/templates", map[string]interface{}{"name": "tm", "orgId": orgID}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			tmpl := Decode[model.Template](t, w)

			require.Equal(t, http.StatusNoContent, h.Do(t, "DELETE", "/api/v1/orgs/"+orgID, nil, h.UserA.ID).Code)

			assert.Nil(t, Decode[model.Page](t, h.Do(t, "GET", "/api/v1/pages/"+page.ID.String(), nil, h.UserA.ID)).OrgID, "page")
			assert.Nil(t, Decode[model.Task](t, h.Do(t, "GET", "/api/v1/tasks/"+task.ID.String(), nil, h.UserA.ID)).OrgID, "task")
			assert.Nil(t, Decode[model.Template](t, h.Do(t, "GET", "/api/v1/templates/"+tmpl.ID.String(), nil, h.UserA.ID)).OrgID, "template")
		},

		// Audit: "CreateLinked returns the existing task where Postgres
		// returns ErrConflict". A retried create whose id already belongs to
		// a different bullet's task must conflict on both backends.
		"CreateLinkedWithIDOfAnotherBulletsTaskConflicts": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			ctx := context.Background()
			page := createPage(t, h, h.UserA.ID, "Note")
			n1, n2 := "n1", "n2"
			first, created, err := h.TaskStore.CreateLinked(ctx, &model.Task{
				UserID: h.UserA.ID, Title: "one", Status: model.StatusTodo, Priority: model.PriorityNone,
				Tags: []string{}, SourcePageID: &page.ID, SourceNodeID: &n1,
			})
			require.NoError(t, err)
			require.True(t, created)

			_, _, err = h.TaskStore.CreateLinked(ctx, &model.Task{
				ID: first.ID, UserID: h.UserA.ID, Title: "two", Status: model.StatusTodo, Priority: model.PriorityNone,
				Tags: []string{}, SourcePageID: &page.ID, SourceNodeID: &n2,
			})
			assert.ErrorIs(t, err, store.ErrConflict)
		},
	})
}
