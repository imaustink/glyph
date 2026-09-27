package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/glyph/api/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createOrgAs(t *testing.T, h *Harness, userID uuid.UUID, name string) string {
	t.Helper()
	w := h.Do(t, "POST", "/api/v1/orgs", map[string]string{"name": name}, userID)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	return Decode[map[string]interface{}](t, w)["id"].(string)
}

func countOwners(t *testing.T, h *Harness, orgID string) int {
	t.Helper()
	members, err := h.OrgStore.ListMembers(context.Background(), uuid.MustParse(orgID))
	require.NoError(t, err)
	n := 0
	for _, m := range members {
		if m.Role == model.OrgRoleOwner {
			n++
		}
	}
	return n
}

// TestOrgOwnerIntegrity covers DI-20: an org must never be left without an
// owner, whether by re-adding an existing member, by two owners stepping
// down at once, or by a failure halfway through creating the org.
func TestOrgOwnerIntegrity(t *testing.T) {
	RunSpecs(t, map[string]func(t *testing.T, h *Harness){
		"AddExistingOwnerDoesNotDemote": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			orgID := createOrgAs(t, h, h.UserA.ID, "Org")

			// No role given: the handler defaults to viewer. Re-adding the
			// sole owner must not silently demote them.
			w := h.Do(t, "POST", "/api/v1/orgs/"+orgID+"/members",
				map[string]string{"userId": h.UserA.ID.String()}, h.UserA.ID)
			assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())

			m, err := h.OrgStore.GetMember(context.Background(), uuid.MustParse(orgID), h.UserA.ID)
			require.NoError(t, err)
			assert.Equal(t, model.OrgRoleOwner, m.Role, "the sole owner was demoted")
		},

		"AddExistingMemberKeepsRoleAndJoinedAt": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			orgID := createOrgAs(t, h, h.UserA.ID, "Org")
			w := h.Do(t, "POST", "/api/v1/orgs/"+orgID+"/members",
				map[string]string{"userId": h.UserB.ID.String(), "role": "editor"}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			before, err := h.OrgStore.GetMember(context.Background(), uuid.MustParse(orgID), h.UserB.ID)
			require.NoError(t, err)

			w = h.Do(t, "POST", "/api/v1/orgs/"+orgID+"/members",
				map[string]string{"userId": h.UserB.ID.String(), "role": "viewer"}, h.UserA.ID)
			assert.Equal(t, http.StatusConflict, w.Code)

			after, err := h.OrgStore.GetMember(context.Background(), uuid.MustParse(orgID), h.UserB.ID)
			require.NoError(t, err)
			assert.Equal(t, model.OrgRoleEditor, after.Role)
			assert.True(t, before.JoinedAt.Equal(after.JoinedAt), "joined_at was reset")
		},

		// Alice and Bob are the two owners. Bob steps down (held open in a
		// transaction that, like the fixed handler, locks the org first)
		// while Alice demotes herself. One of them must be refused.
		"ConcurrentOwnerDemotionsKeepAnOwner": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			pool := h.pgPool(t)
			orgID := createOrgAs(t, h, h.UserA.ID, "Org")
			w := h.Do(t, "POST", "/api/v1/orgs/"+orgID+"/members",
				map[string]string{"userId": h.UserB.ID.String(), "role": "owner"}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)

			w = interleave(t, pool, func(ctx context.Context, tx pgx.Tx) {
				_, err := tx.Exec(ctx, `SELECT 1 FROM organizations WHERE id = $1 FOR UPDATE`, orgID)
				require.NoError(t, err)
				_, err = tx.Exec(ctx, `UPDATE org_members SET role = 'editor' WHERE org_id = $1 AND user_id = $2`, orgID, h.UserB.ID)
				require.NoError(t, err)
			}, func() *httptest.ResponseRecorder {
				return h.Do(t, "PATCH", "/api/v1/orgs/"+orgID+"/members/"+h.UserA.ID.String(),
					map[string]string{"role": "editor"}, h.UserA.ID)
			})
			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			assert.Equal(t, 1, countOwners(t, h, orgID), "the org was left without an owner")
		},

		"ConcurrentOwnerLeavesKeepAnOwner": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			pool := h.pgPool(t)
			orgID := createOrgAs(t, h, h.UserA.ID, "Org")
			w := h.Do(t, "POST", "/api/v1/orgs/"+orgID+"/members",
				map[string]string{"userId": h.UserB.ID.String(), "role": "owner"}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)

			w = interleave(t, pool, func(ctx context.Context, tx pgx.Tx) {
				_, err := tx.Exec(ctx, `SELECT 1 FROM organizations WHERE id = $1 FOR UPDATE`, orgID)
				require.NoError(t, err)
				_, err = tx.Exec(ctx, `DELETE FROM org_members WHERE org_id = $1 AND user_id = $2`, orgID, h.UserB.ID)
				require.NoError(t, err)
			}, func() *httptest.ResponseRecorder {
				return h.Do(t, "DELETE", "/api/v1/orgs/"+orgID+"/members/"+h.UserA.ID.String(), nil, h.UserA.ID)
			})
			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			assert.Equal(t, 1, countOwners(t, h, orgID), "the org was left without an owner")
		},

		// A failure adding the creator as owner must not leave an org behind
		// that nobody belongs to (and so nobody can see or delete).
		"CreateOrgIsAtomic": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			pool := h.pgPool(t)
			ctx := context.Background()
			_, err := pool.Exec(ctx, `
				CREATE FUNCTION test_fail_member_insert() RETURNS trigger LANGUAGE plpgsql AS
				$$ BEGIN RAISE EXCEPTION 'injected failure'; END $$;
				CREATE TRIGGER test_fail_member_insert BEFORE INSERT ON org_members
				FOR EACH ROW EXECUTE FUNCTION test_fail_member_insert();`)
			require.NoError(t, err)
			t.Cleanup(func() {
				_, _ = pool.Exec(ctx, `DROP TRIGGER IF EXISTS test_fail_member_insert ON org_members;
					DROP FUNCTION IF EXISTS test_fail_member_insert();`)
			})

			w := h.Do(t, "POST", "/api/v1/orgs", map[string]string{"name": "Doomed"}, h.UserA.ID)
			assert.Equal(t, http.StatusInternalServerError, w.Code)

			var orgs int
			require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM organizations`).Scan(&orgs))
			assert.Equal(t, 0, orgs, "an org with no members was left behind")
		},
	})
}
