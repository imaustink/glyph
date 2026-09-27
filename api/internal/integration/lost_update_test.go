package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/glyph/api/internal/model"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLostUpdates covers DI-05: a PATCH that changes one field must not put
// back another field that a concurrent write changed in the meantime. Each
// spec holds a competing, uncommitted write in a transaction (see
// interleave) while the PATCH runs.
func TestLostUpdates(t *testing.T) {
	RunSpecs(t, map[string]func(t *testing.T, h *Harness){
		"TaskPatchKeepsConcurrentStatusChange": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			pool := h.pgPool(t)
			task := createTestTask(t, h, h.UserA.ID, "Original")

			w := interleave(t, pool, func(ctx context.Context, tx pgx.Tx) {
				_, err := tx.Exec(ctx, `UPDATE tasks SET status = 'done' WHERE id = $1`, task.ID)
				require.NoError(t, err)
			}, func() *httptest.ResponseRecorder {
				return h.Do(t, "PATCH", "/api/v1/tasks/"+task.ID.String(), map[string]interface{}{"title": "Renamed"}, h.UserA.ID)
			})
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())

			got := Decode[model.Task](t, h.Do(t, "GET", "/api/v1/tasks/"+task.ID.String(), nil, h.UserA.ID))
			assert.Equal(t, "Renamed", got.Title)
			assert.Equal(t, model.StatusDone, got.Status, "the concurrent status change was lost")
		},

		"LanePatchKeepsConcurrentSortChange": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			pool := h.pgPool(t)
			w := h.Do(t, "POST", "/api/v1/lanes", map[string]interface{}{"title": "Lane"}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			lane := Decode[model.Lane](t, w)

			w = interleave(t, pool, func(ctx context.Context, tx pgx.Tx) {
				_, err := tx.Exec(ctx, `UPDATE lanes SET sort_config = '{"mode":"manual"}' WHERE id = $1`, lane.ID)
				require.NoError(t, err)
			}, func() *httptest.ResponseRecorder {
				return h.Do(t, "PATCH", "/api/v1/lanes/"+lane.ID.String(), map[string]interface{}{"title": "Renamed"}, h.UserA.ID)
			})
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())

			got := Decode[model.Lane](t, h.Do(t, "GET", "/api/v1/lanes/"+lane.ID.String(), nil, h.UserA.ID))
			assert.Equal(t, "Renamed", got.Title)
			assert.Equal(t, model.SortModeManual, got.SortConfig.Mode, "the concurrent sort change was lost")
		},

		"FolderLaneUpdateKeepsConcurrentFilterChange": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			pool := h.pgPool(t)
			folder, lane := createFolderWithLane(t, h)

			w := interleave(t, pool, func(ctx context.Context, tx pgx.Tx) {
				_, err := tx.Exec(ctx, `UPDATE lanes SET filter_set = '{"conjunction":"or","rules":[]}' WHERE id = $1`, lane.ID)
				require.NoError(t, err)
			}, func() *httptest.ResponseRecorder {
				return h.Do(t, "PUT", "/api/v1/folders/"+folder.ID.String()+"/lanes/"+lane.ID.String(),
					map[string]interface{}{"title": "Renamed"}, h.UserA.ID)
			})
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())

			got := getFolderLane(t, h, folder.ID.String())
			assert.Equal(t, "Renamed", got.Title)
			assert.Equal(t, model.ConjunctionOr, got.FilterSet.Conjunction, "the concurrent filter change was lost")
		},

		"TemplatePatchKeepsConcurrentContentChange": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			pool := h.pgPool(t)
			w := h.Do(t, "POST", "/api/v1/templates", map[string]interface{}{"name": "Tmpl", "content": "old"}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			tmpl := Decode[model.Template](t, w)

			w = interleave(t, pool, func(ctx context.Context, tx pgx.Tx) {
				_, err := tx.Exec(ctx, `UPDATE templates SET content = 'new' WHERE id = $1`, tmpl.ID)
				require.NoError(t, err)
			}, func() *httptest.ResponseRecorder {
				return h.Do(t, "PATCH", "/api/v1/templates/"+tmpl.ID.String(), map[string]interface{}{"name": "Renamed"}, h.UserA.ID)
			})
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())

			got := Decode[model.Template](t, h.Do(t, "GET", "/api/v1/templates/"+tmpl.ID.String(), nil, h.UserA.ID))
			assert.Equal(t, "Renamed", got.Name)
			assert.Equal(t, "new", got.Content, "the concurrent content change was lost")
		},
	})
}

// TestOAuthClientUpdateLostUpdate covers DI-05 for OAuth clients: renaming a
// client must not put back redirect URIs changed concurrently.
func TestOAuthClientUpdateLostUpdate(t *testing.T) {
	s := setupOAuthServer(t)
	s.reset(t)
	ctx := context.Background()
	owner := s.createUser(t, "sub-owner-lu", "owner-lu@test.com", "Owner")
	client, err := s.clients.Create(ctx, &model.OAuthClient{
		ClientID: "lost-update-client", Name: "Before", CreatedByID: owner.ID,
		GrantTypes:   []model.OAuthGrantType{model.GrantAuthorizationCode},
		Scopes:       []model.OAuthScope{model.ScopePageRead},
		RedirectURIs: []string{"http://localhost/old"},
	}, "secret-hash")
	require.NoError(t, err)

	name := "After"
	updated := interleave(t, s.pool, func(ctx context.Context, tx pgx.Tx) {
		_, err := tx.Exec(ctx, `UPDATE oauth_clients SET redirect_uris = '{http://localhost/new}' WHERE id = $1`, client.ID)
		require.NoError(t, err)
	}, func() error {
		_, err := s.clients.Update(ctx, client.ID, &name, nil, nil, nil)
		return err
	})
	require.NoError(t, updated)

	got, err := s.clients.GetByID(ctx, client.ID)
	require.NoError(t, err)
	assert.Equal(t, "After", got.Name)
	assert.Equal(t, []string{"http://localhost/new"}, got.RedirectURIs, "the concurrent redirect URI change was lost")
}
