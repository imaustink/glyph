package integration

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/glyph/api/internal/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOrgMembershipRequiredToUseOrgID regression-tests the fix for
// create/update/upsert handlers that copied a client-supplied orgId onto a
// page/task/template with no check that the requester actually belongs to
// that org — letting any user plant a row inside an org they don't belong
// to, which becomes visible to (and, depending on role, writable by) every
// member of that org once shared non-privately.
func TestOrgMembershipRequiredToUseOrgID(t *testing.T) {
	RunSpecs(t, map[string]func(t *testing.T, h *Harness){
		"CreatePageIntoForeignOrgIsForbidden": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "POST", "/api/v1/orgs", map[string]interface{}{"name": "Bob Corp"}, h.UserB.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			bobOrg := Decode[model.OrgWithRole](t, w)

			w = h.Do(t, "POST", "/api/v1/pages", map[string]interface{}{
				"title": "Injected", "type": "page", "orgId": bobOrg.ID.String(), "isPrivate": false,
			}, h.UserA.ID)
			assert.Equal(t, http.StatusForbidden, w.Code, "non-member must not create a page in another org: %s", w.Body.String())
		},

		"UpsertPageIntoForeignOrgIsForbidden": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "POST", "/api/v1/orgs", map[string]interface{}{"name": "Bob Corp"}, h.UserB.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			bobOrg := Decode[model.OrgWithRole](t, w)

			newID := uuid.New().String()
			w = h.Do(t, "PUT", fmt.Sprintf("/api/v1/pages/%s", newID), map[string]interface{}{
				"title": "Injected", "type": "page", "orgId": bobOrg.ID.String(), "isPrivate": false,
			}, h.UserA.ID)
			assert.Equal(t, http.StatusForbidden, w.Code, "non-member must not upsert a page into another org: %s", w.Body.String())

			// Confirm it never became visible to the org's actual member.
			w2 := h.Do(t, "GET", "/api/v1/pages", nil, h.UserB.ID)
			pages := Decode[[]model.Page](t, w2)
			for _, p := range pages {
				assert.NotEqual(t, "Injected", p.Title, "rejected page must not have been persisted")
			}
		},

		"UpdatePageOrgIDIntoForeignOrgIsForbidden": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "POST", "/api/v1/orgs", map[string]interface{}{"name": "Bob Corp"}, h.UserB.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			bobOrg := Decode[model.OrgWithRole](t, w)

			w = h.Do(t, "POST", "/api/v1/pages", map[string]interface{}{"title": "Mine", "type": "page"}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			page := Decode[model.Page](t, w)

			w = h.Do(t, "PATCH", "/api/v1/pages/"+page.ID.String(), map[string]interface{}{
				"orgId": bobOrg.ID.String(), "isPrivate": false,
			}, h.UserA.ID)
			assert.Equal(t, http.StatusForbidden, w.Code, "non-member must not move a page into another org via PATCH: %s", w.Body.String())
		},

		"CreateTaskIntoForeignOrgIsForbidden": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "POST", "/api/v1/orgs", map[string]interface{}{"name": "Bob Corp"}, h.UserB.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			bobOrg := Decode[model.OrgWithRole](t, w)

			w = h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{
				"title": "Injected Task", "orgId": bobOrg.ID.String(), "isPrivate": false,
			}, h.UserA.ID)
			assert.Equal(t, http.StatusForbidden, w.Code, "non-member must not create a task in another org: %s", w.Body.String())
		},

		"CreateTemplateIntoForeignOrgIsForbidden": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "POST", "/api/v1/orgs", map[string]interface{}{"name": "Bob Corp"}, h.UserB.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			bobOrg := Decode[model.OrgWithRole](t, w)

			w = h.Do(t, "POST", "/api/v1/templates", map[string]interface{}{
				"name": "Injected Template", "content": "x", "orgId": bobOrg.ID.String(), "isPrivate": false,
			}, h.UserA.ID)
			assert.Equal(t, http.StatusForbidden, w.Code, "non-member must not create a template in another org: %s", w.Body.String())
		},

		// Sanity check: an actual member of the org must still be able to
		// create org-scoped resources — this fix must not have collaterally
		// broken legitimate usage.
		"MemberCanCreatePageInOwnOrg": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "POST", "/api/v1/orgs", map[string]interface{}{"name": "Shared Org"}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			org := Decode[model.OrgWithRole](t, w)

			w = h.Do(t, "POST", "/api/v1/orgs/"+org.ID.String()+"/members",
				map[string]interface{}{"userId": h.UserB.ID.String(), "role": "editor"}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)

			w = h.Do(t, "POST", "/api/v1/pages", map[string]interface{}{
				"title": "Legit Org Page", "type": "page", "orgId": org.ID.String(), "isPrivate": false,
			}, h.UserB.ID)
			assert.Equal(t, http.StatusCreated, w.Code, "an actual org member should be able to create pages scoped to it: %s", w.Body.String())
		},
	})
}
