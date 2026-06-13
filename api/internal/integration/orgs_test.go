package integration

import (
	"net/http"
	"testing"

	"github.com/glyph/api/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrgs(t *testing.T) {
	RunSpecs(t, map[string]func(t *testing.T, h *Harness){
		"CreateOrg": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "POST", "/api/v1/orgs", map[string]string{"name": "Team Alpha"}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			org := Decode[map[string]interface{}](t, w)
			assert.Equal(t, "Team Alpha", org["name"])
			assert.NotEmpty(t, org["id"])
		},
		"CreateOrg_RequiresName": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "POST", "/api/v1/orgs", map[string]string{"name": ""}, h.UserA.ID)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		},
		"ListOrgs_ReturnsOnlyMemberOrgs": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			h.Do(t, "POST", "/api/v1/orgs", map[string]string{"name": "Alice Org"}, h.UserA.ID)
			h.Do(t, "POST", "/api/v1/orgs", map[string]string{"name": "Bob Org"}, h.UserB.ID)
			w := h.Do(t, "GET", "/api/v1/orgs", nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			orgs := Decode[[]map[string]interface{}](t, w)
			assert.Len(t, orgs, 1)
			assert.Equal(t, "Alice Org", orgs[0]["name"])
		},
		"GetOrg_MemberCanView": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			org := Decode[map[string]interface{}](t, h.Do(t, "POST", "/api/v1/orgs", map[string]string{"name": "Visible Org"}, h.UserA.ID))
			orgID := org["id"].(string)
			h.Do(t, "POST", "/api/v1/orgs/"+orgID+"/members", map[string]string{"userId": h.UserB.ID.String(), "role": "viewer"}, h.UserA.ID)
			w := h.Do(t, "GET", "/api/v1/orgs/"+orgID, nil, h.UserB.ID)
			require.Equal(t, http.StatusOK, w.Code)
			detail := Decode[map[string]interface{}](t, w)
			assert.Equal(t, "Visible Org", detail["org"].(map[string]interface{})["name"])
		},
		"GetOrg_NonMemberGets404": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			org := Decode[map[string]interface{}](t, h.Do(t, "POST", "/api/v1/orgs", map[string]string{"name": "Private Org"}, h.UserA.ID))
			orgID := org["id"].(string)
			w := h.Do(t, "GET", "/api/v1/orgs/"+orgID, nil, h.UserB.ID)
			assert.Equal(t, http.StatusNotFound, w.Code)
		},
		"UpdateOrg_OwnerCanRename": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			org := Decode[map[string]interface{}](t, h.Do(t, "POST", "/api/v1/orgs", map[string]string{"name": "Old Name"}, h.UserA.ID))
			orgID := org["id"].(string)
			w := h.Do(t, "PATCH", "/api/v1/orgs/"+orgID, map[string]string{"name": "New Name"}, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			updated := Decode[map[string]interface{}](t, w)
			assert.Equal(t, "New Name", updated["name"])
		},
		"UpdateOrg_NonOwnerGets403": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			org := Decode[map[string]interface{}](t, h.Do(t, "POST", "/api/v1/orgs", map[string]string{"name": "Alice Org"}, h.UserA.ID))
			orgID := org["id"].(string)
			h.Do(t, "POST", "/api/v1/orgs/"+orgID+"/members", map[string]string{"userId": h.UserB.ID.String(), "role": "editor"}, h.UserA.ID)
			w := h.Do(t, "PATCH", "/api/v1/orgs/"+orgID, map[string]string{"name": "Hijacked"}, h.UserB.ID)
			assert.Equal(t, http.StatusForbidden, w.Code)
		},
		"DeleteOrg_OwnerCanDelete": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			org := Decode[map[string]interface{}](t, h.Do(t, "POST", "/api/v1/orgs", map[string]string{"name": "Temp Org"}, h.UserA.ID))
			orgID := org["id"].(string)
			w := h.Do(t, "DELETE", "/api/v1/orgs/"+orgID, nil, h.UserA.ID)
			assert.Equal(t, http.StatusNoContent, w.Code)
			orgs := Decode[[]map[string]interface{}](t, h.Do(t, "GET", "/api/v1/orgs", nil, h.UserA.ID))
			assert.Empty(t, orgs)
		},
		"AddMember_OwnerOnly": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			org := Decode[map[string]interface{}](t, h.Do(t, "POST", "/api/v1/orgs", map[string]string{"name": "Guarded"}, h.UserA.ID))
			orgID := org["id"].(string)
			w := h.Do(t, "POST", "/api/v1/orgs/"+orgID+"/members", map[string]string{"userId": h.UserB.ID.String(), "role": "viewer"}, h.UserB.ID)
			assert.Equal(t, http.StatusNotFound, w.Code)
		},
		"CannotDemoteLastOwner": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			org := Decode[map[string]interface{}](t, h.Do(t, "POST", "/api/v1/orgs", map[string]string{"name": "Solo"}, h.UserA.ID))
			orgID := org["id"].(string)
			w := h.Do(t, "PATCH", "/api/v1/orgs/"+orgID+"/members/"+h.UserA.ID.String(), map[string]string{"role": "editor"}, h.UserA.ID)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		},
		"CannotRemoveLastOwner": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			org := Decode[map[string]interface{}](t, h.Do(t, "POST", "/api/v1/orgs", map[string]string{"name": "Solo2"}, h.UserA.ID))
			orgID := org["id"].(string)
			w := h.Do(t, "DELETE", "/api/v1/orgs/"+orgID+"/members/"+h.UserA.ID.String(), nil, h.UserA.ID)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		},
		"SelfLeave_AllowedWithoutOwnerRole": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			org := Decode[map[string]interface{}](t, h.Do(t, "POST", "/api/v1/orgs", map[string]string{"name": "Leave Test"}, h.UserA.ID))
			orgID := org["id"].(string)
			h.Do(t, "POST", "/api/v1/orgs/"+orgID+"/members", map[string]string{"userId": h.UserB.ID.String(), "role": "viewer"}, h.UserA.ID)
			w := h.Do(t, "DELETE", "/api/v1/orgs/"+orgID+"/members/"+h.UserB.ID.String(), nil, h.UserB.ID)
			assert.Equal(t, http.StatusNoContent, w.Code)
		},
	})
}

func TestOrgSharedPages(t *testing.T) {
	RunSpecs(t, map[string]func(t *testing.T, h *Harness){
		"OrgMemberCanListNonPrivatePage": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			org := Decode[map[string]interface{}](t, h.Do(t, "POST", "/api/v1/orgs", map[string]string{"name": "SharedOrg"}, h.UserA.ID))
			orgID := org["id"].(string)
			h.Do(t, "POST", "/api/v1/orgs/"+orgID+"/members", map[string]string{"userId": h.UserB.ID.String(), "role": "viewer"}, h.UserA.ID)
			// CreatePage always defaults to isPrivate=true; owner must PATCH to share.
			page := Decode[model.Page](t, h.Do(t, "POST", "/api/v1/pages", map[string]interface{}{"title": "Shared Page", "type": "page"}, h.UserA.ID))
			h.Do(t, "PATCH", "/api/v1/pages/"+page.ID.String(), map[string]interface{}{"title": "Shared Page", "type": "page", "orgId": orgID, "isPrivate": false}, h.UserA.ID)
			w := h.Do(t, "GET", "/api/v1/pages", nil, h.UserB.ID)
			require.Equal(t, http.StatusOK, w.Code)
			pages := Decode[[]map[string]interface{}](t, w)
			require.Len(t, pages, 1)
			assert.Equal(t, "Shared Page", pages[0]["title"])
		},
		"OrgMemberCannotSeePrivatePage": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			org := Decode[map[string]interface{}](t, h.Do(t, "POST", "/api/v1/orgs", map[string]string{"name": "PrivateOrg"}, h.UserA.ID))
			orgID := org["id"].(string)
			h.Do(t, "POST", "/api/v1/orgs/"+orgID+"/members", map[string]string{"userId": h.UserB.ID.String(), "role": "viewer"}, h.UserA.ID)
			// Page stays private (default); confirm Bob cannot see it.
			h.Do(t, "POST", "/api/v1/pages", map[string]interface{}{"title": "Private Page", "type": "page"}, h.UserA.ID)
			w := h.Do(t, "GET", "/api/v1/pages", nil, h.UserB.ID)
			pages := Decode[[]map[string]interface{}](t, w)
			assert.Empty(t, pages)
		},
		"OrgEditorCanEditPage": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			org := Decode[map[string]interface{}](t, h.Do(t, "POST", "/api/v1/orgs", map[string]string{"name": "EditorOrg"}, h.UserA.ID))
			orgID := org["id"].(string)
			h.Do(t, "POST", "/api/v1/orgs/"+orgID+"/members", map[string]string{"userId": h.UserB.ID.String(), "role": string(model.OrgRoleEditor)}, h.UserA.ID)
			page := Decode[model.Page](t, h.Do(t, "POST", "/api/v1/pages", map[string]interface{}{"title": "Editable Page", "type": "page"}, h.UserA.ID))
			// Share with org as non-private so Bob can see and edit it.
			h.Do(t, "PATCH", "/api/v1/pages/"+page.ID.String(), map[string]interface{}{"title": "Editable Page", "type": "page", "orgId": orgID, "isPrivate": false}, h.UserA.ID)
			w := h.Do(t, "PATCH", "/api/v1/pages/"+page.ID.String(), map[string]interface{}{"title": "Updated by Bob", "type": "page", "orgId": orgID, "isPrivate": false}, h.UserB.ID)
			assert.Equal(t, http.StatusOK, w.Code)
		},
		"OrgViewerCannotEditPage": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			org := Decode[map[string]interface{}](t, h.Do(t, "POST", "/api/v1/orgs", map[string]string{"name": "ReadOnlyOrg"}, h.UserA.ID))
			orgID := org["id"].(string)
			h.Do(t, "POST", "/api/v1/orgs/"+orgID+"/members", map[string]string{"userId": h.UserB.ID.String(), "role": string(model.OrgRoleViewer)}, h.UserA.ID)
			page := Decode[model.Page](t, h.Do(t, "POST", "/api/v1/pages", map[string]interface{}{"title": "Read Only Page", "type": "page"}, h.UserA.ID))
			// Share with org so Bob can see but not edit.
			h.Do(t, "PATCH", "/api/v1/pages/"+page.ID.String(), map[string]interface{}{"title": "Read Only Page", "type": "page", "orgId": orgID, "isPrivate": false}, h.UserA.ID)
			w := h.Do(t, "PATCH", "/api/v1/pages/"+page.ID.String(), map[string]interface{}{"title": "Attempt", "type": "page", "orgId": orgID, "isPrivate": false}, h.UserB.ID)
			assert.Equal(t, http.StatusForbidden, w.Code)
		},
	})
}

func TestOrgSharedTasks(t *testing.T) {
	RunSpecs(t, map[string]func(t *testing.T, h *Harness){
		"OrgMemberCanListNonPrivateTask": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			org := Decode[map[string]interface{}](t, h.Do(t, "POST", "/api/v1/orgs", map[string]string{"name": "TaskOrg"}, h.UserA.ID))
			orgID := org["id"].(string)
			h.Do(t, "POST", "/api/v1/orgs/"+orgID+"/members", map[string]string{"userId": h.UserB.ID.String(), "role": "viewer"}, h.UserA.ID)
			h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{"title": "Shared Task", "orgId": orgID, "isPrivate": false}, h.UserA.ID)
			w := h.Do(t, "GET", "/api/v1/tasks", nil, h.UserB.ID)
			require.Equal(t, http.StatusOK, w.Code)
			tasks := Decode[[]map[string]interface{}](t, w)
			require.Len(t, tasks, 1)
			assert.Equal(t, "Shared Task", tasks[0]["title"])
		},
		"OrgMemberCannotSeePrivateTask": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			org := Decode[map[string]interface{}](t, h.Do(t, "POST", "/api/v1/orgs", map[string]string{"name": "PrivateTaskOrg"}, h.UserA.ID))
			orgID := org["id"].(string)
			h.Do(t, "POST", "/api/v1/orgs/"+orgID+"/members", map[string]string{"userId": h.UserB.ID.String(), "role": "viewer"}, h.UserA.ID)
			h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{"title": "Private Task", "orgId": orgID, "isPrivate": true}, h.UserA.ID)
			w := h.Do(t, "GET", "/api/v1/tasks", nil, h.UserB.ID)
			tasks := Decode[[]map[string]interface{}](t, w)
			assert.Empty(t, tasks)
		},
		"OrgEditorCanEditTask": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			org := Decode[map[string]interface{}](t, h.Do(t, "POST", "/api/v1/orgs", map[string]string{"name": "TaskEditorOrg"}, h.UserA.ID))
			orgID := org["id"].(string)
			h.Do(t, "POST", "/api/v1/orgs/"+orgID+"/members", map[string]string{"userId": h.UserB.ID.String(), "role": string(model.OrgRoleEditor)}, h.UserA.ID)
			task := Decode[model.Task](t, h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{"title": "Editable Task", "orgId": orgID, "isPrivate": false}, h.UserA.ID))
			w := h.Do(t, "PATCH", "/api/v1/tasks/"+task.ID.String(), map[string]interface{}{"title": "Updated by Bob"}, h.UserB.ID)
			assert.Equal(t, http.StatusOK, w.Code)
		},
		"OrgViewerCannotEditTask": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			org := Decode[map[string]interface{}](t, h.Do(t, "POST", "/api/v1/orgs", map[string]string{"name": "TaskViewerOrg"}, h.UserA.ID))
			orgID := org["id"].(string)
			h.Do(t, "POST", "/api/v1/orgs/"+orgID+"/members", map[string]string{"userId": h.UserB.ID.String(), "role": string(model.OrgRoleViewer)}, h.UserA.ID)
			task := Decode[model.Task](t, h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{"title": "View Only Task", "orgId": orgID, "isPrivate": false}, h.UserA.ID))
			w := h.Do(t, "PATCH", "/api/v1/tasks/"+task.ID.String(), map[string]interface{}{"title": "Attempt"}, h.UserB.ID)
			assert.Equal(t, http.StatusForbidden, w.Code)
		},
	})
}

func TestOrgSharedTemplates(t *testing.T) {
	RunSpecs(t, map[string]func(t *testing.T, h *Harness){
		"OrgMemberCanListNonPrivateTemplate": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			org := Decode[map[string]interface{}](t, h.Do(t, "POST", "/api/v1/orgs", map[string]string{"name": "TemplateOrg"}, h.UserA.ID))
			orgID := org["id"].(string)
			h.Do(t, "POST", "/api/v1/orgs/"+orgID+"/members", map[string]string{"userId": h.UserB.ID.String(), "role": "viewer"}, h.UserA.ID)
			h.Do(t, "POST", "/api/v1/templates", map[string]interface{}{"name": "Shared Template", "content": "# Hello", "orgId": orgID, "isPrivate": false}, h.UserA.ID)
			w := h.Do(t, "GET", "/api/v1/templates", nil, h.UserB.ID)
			require.Equal(t, http.StatusOK, w.Code)
			templates := Decode[[]map[string]interface{}](t, w)
			require.Len(t, templates, 1)
			assert.Equal(t, "Shared Template", templates[0]["name"])
		},
		"OrgMemberCannotSeePrivateTemplate": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			org := Decode[map[string]interface{}](t, h.Do(t, "POST", "/api/v1/orgs", map[string]string{"name": "PrivateTemplateOrg"}, h.UserA.ID))
			orgID := org["id"].(string)
			h.Do(t, "POST", "/api/v1/orgs/"+orgID+"/members", map[string]string{"userId": h.UserB.ID.String(), "role": "viewer"}, h.UserA.ID)
			h.Do(t, "POST", "/api/v1/templates", map[string]interface{}{"name": "Private Template", "content": "# Secret", "orgId": orgID, "isPrivate": true}, h.UserA.ID)
			w := h.Do(t, "GET", "/api/v1/templates", nil, h.UserB.ID)
			templates := Decode[[]map[string]interface{}](t, w)
			assert.Empty(t, templates)
		},
	})
}

func TestOrgAddMemberByEmail(t *testing.T) {
	RunSpecs(t, map[string]func(t *testing.T, h *Harness){
		"AddMember_ByEmail": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			org := Decode[map[string]interface{}](t, h.Do(t, "POST", "/api/v1/orgs", map[string]string{"name": "Email Add Org"}, h.UserA.ID))
			orgID := org["id"].(string)
			w := h.Do(t, "POST", "/api/v1/orgs/"+orgID+"/members", map[string]string{"email": "bob@test.com", "role": "viewer"}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			member := Decode[map[string]interface{}](t, w)
			assert.Equal(t, "viewer", member["role"])
		},
		"AddMember_ByEmail_NotFound": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			org := Decode[map[string]interface{}](t, h.Do(t, "POST", "/api/v1/orgs", map[string]string{"name": "Email Fail Org"}, h.UserA.ID))
			orgID := org["id"].(string)
			w := h.Do(t, "POST", "/api/v1/orgs/"+orgID+"/members", map[string]string{"email": "nobody@example.com", "role": "viewer"}, h.UserA.ID)
			assert.Equal(t, http.StatusNotFound, w.Code)
		},
	})
}
