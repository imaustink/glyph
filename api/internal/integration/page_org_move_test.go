package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPageOrgMove covers the org cascade of PATCH /pages/:id {"orgId": ...}:
// moving a folder to another workspace takes its subtree along, so it must
// refuse (as delete does) a subtree holding other users' pages, and it must
// be all-or-nothing with the rest of the PATCH.
func TestPageOrgMove(t *testing.T) {
	RunSpecs(t, map[string]func(t *testing.T, h *Harness){
		// Moving a folder would carry pages other users created in it (via an
		// editor share) into a workspace they never chose. Refused like
		// DI02_DeleteFolderContainingOtherUsersPageRefused.
		"OrgMoveOfFolderContainingOtherUsersPageRefused": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			orgID := createOrgAs(t, h, h.UserA.ID, "Alice's org")
			f := createFolder(t, h, h.UserA.ID, "Alice's folder")
			shareFolder(t, h, f.ID.String(), h.UserA.ID, h.UserB.ID, "editor")
			sub := createChild(t, h, h.UserA.ID, f.ID, "page", "Sub")
			bobs := createChild(t, h, h.UserB.ID, f.ID, "page", "Bob's page")

			w := h.Do(t, "PATCH", "/api/v1/pages/"+f.ID.String(), map[string]interface{}{"orgId": orgID}, h.UserA.ID)
			require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
			assert.Equal(t, "subtree_has_other_owners", Decode[map[string]interface{}](t, w)["code"])

			// Nothing moved: not Bob's page, not Alice's, not the folder.
			assert.Nil(t, getPage(t, h, h.UserB.ID, bobs.ID).OrgID, "Bob's page was moved into Alice's org")
			assert.Nil(t, getPage(t, h, h.UserA.ID, sub.ID).OrgID, "the refused move still rewrote the subtree")
			assert.Nil(t, getPage(t, h, h.UserA.ID, f.ID).OrgID, "the refused move still moved the folder")
		},

		// A PATCH that changes the org and also makes the node its own
		// ancestor fails as a whole: no org_id in the subtree changes.
		"OrgMoveWithCyclicParentLeavesOrgsUnchanged": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			orgID := createOrgAs(t, h, h.UserA.ID, "Org")
			f := createFolder(t, h, h.UserA.ID, "F")
			child := createChild(t, h, h.UserA.ID, f.ID, "folder", "Child")

			w := h.Do(t, "PATCH", "/api/v1/pages/"+f.ID.String(),
				map[string]interface{}{"orgId": orgID, "parentId": child.ID.String()}, h.UserA.ID)
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

			assert.Nil(t, getPage(t, h, h.UserA.ID, f.ID).OrgID)
			assert.Nil(t, getPage(t, h, h.UserA.ID, child.ID).OrgID, "the failed PATCH left the subtree's org rewritten")
		},

		// The same failure when the cycle only appears through a concurrent
		// move, which the handler's pre-check cannot see and only the
		// store's check under the tree-move lock catches. The org cascade
		// must roll back with the rejected parent write.
		"OrgMoveRacingCyclicMoveLeavesOrgsUnchanged": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			pool := h.pgPool(t)
			orgID := createOrgAs(t, h, h.UserA.ID, "Org")
			f := createFolder(t, h, h.UserA.ID, "F")
			child := createChild(t, h, h.UserA.ID, f.ID, "page", "Child")
			x := createFolder(t, h, h.UserA.ID, "X")

			// Concurrently, X is moved under F (holding the tree-move lock,
			// as the store does), so F under X becomes a cycle.
			w := interleave(t, pool, func(ctx context.Context, tx pgx.Tx) {
				_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('glyph.pages.tree_move'))`)
				require.NoError(t, err)
				_, err = tx.Exec(ctx, `UPDATE pages SET parent_id = $1 WHERE id = $2`, f.ID, x.ID)
				require.NoError(t, err)
			}, func() *httptest.ResponseRecorder {
				return h.Do(t, "PATCH", "/api/v1/pages/"+f.ID.String(),
					map[string]interface{}{"orgId": orgID, "parentId": x.ID.String()}, h.UserA.ID)
			})
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

			assert.Nil(t, getPage(t, h, h.UserA.ID, f.ID).OrgID, "the rejected PATCH moved the folder's org")
			assert.Nil(t, getPage(t, h, h.UserA.ID, child.ID).OrgID, "the rejected PATCH left the subtree's org rewritten")
			assert.Nil(t, getPage(t, h, h.UserA.ID, x.ID).OrgID)
		},
	})
}
