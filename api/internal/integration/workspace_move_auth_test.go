package integration

import (
	"net/http"
	"testing"

	"github.com/glyph/api/internal/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// addOrgMember adds memberID to orgID with role, as the org's owner.
func addOrgMember(t *testing.T, h *Harness, ownerID uuid.UUID, orgID string, memberID uuid.UUID, role string) {
	t.Helper()
	w := h.Do(t, "POST", "/api/v1/orgs/"+orgID+"/members",
		map[string]string{"userId": memberID.String(), "role": role}, ownerID)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
}

// orgFolder creates, as ownerID, a folder in orgID visible to the org, with
// a sub-page and a task on the folder's board.
func orgFolder(t *testing.T, h *Harness, ownerID uuid.UUID, orgID string) (folder, sub model.Page, task model.Task) {
	t.Helper()
	folder = createFolder(t, h, ownerID, "Org folder")
	sub = createChild(t, h, ownerID, folder.ID, "page", "Sub")
	w := h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{"title": "board task", "folderId": folder.ID.String()}, ownerID)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	task = Decode[model.Task](t, w)
	w = h.Do(t, "PATCH", "/api/v1/pages/"+folder.ID.String(), map[string]interface{}{"orgId": orgID, "isPrivate": false}, ownerID)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	return folder, sub, task
}

func getTask(t *testing.T, h *Harness, userID, id uuid.UUID) model.Task {
	t.Helper()
	w := h.Do(t, "GET", "/api/v1/tasks/"+id.String(), nil, userID)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	return Decode[model.Task](t, w)
}

func assertInOrg(t *testing.T, orgID string, got *uuid.UUID, msg string) {
	t.Helper()
	if assert.NotNil(t, got, msg) {
		assert.Equal(t, orgID, got.String(), msg)
	}
}

// orgOnlyScope is a token granted every write scope on orgID and nothing
// in the acting user's personal workspace.
func orgOnlyScope(orgID string) model.TokenScope {
	return model.TokenScope{
		Scopes: []model.OAuthScope{model.ScopePageRead, model.ScopePageWrite, model.ScopeTaskRead,
			model.ScopeTaskWrite, model.ScopeTemplateRead, model.ScopeTemplateWrite},
		OrgIDs: []uuid.UUID{uuid.MustParse(orgID)},
	}
}

// personalOnlyScope is a token granted every write scope on the acting
// user's personal workspace and no org.
func personalOnlyScope() model.TokenScope {
	s := orgOnlyScope(uuid.Nil.String())
	s.OrgIDs = nil
	s.Personal = true
	return s
}

// TestPageWorkspaceMoveAuthorization covers the destination of PATCH
// /pages/:id {"orgId": ...}. {"orgId": null} takes the node, its subtree
// and their tasks to the owner's Personal workspace, out of the org's
// reach, so only the owner may do it, and a bearer token only if its grant
// covers Personal writes. Any move must also land inside the token's grant.
func TestPageWorkspaceMoveAuthorization(t *testing.T) {
	RunSpecs(t, map[string]func(t *testing.T, h *Harness){
		// An org editor can write a colleague's folder, but taking it out of
		// the org would cut every other member off from it.
		"OrgEditorCannotMoveColleaguesFolderToPersonal": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			orgID := createOrgAs(t, h, h.UserA.ID, "Org")
			addOrgMember(t, h, h.UserA.ID, orgID, h.UserB.ID, "editor")
			f, sub, task := orgFolder(t, h, h.UserA.ID, orgID)

			w := h.Do(t, "PATCH", "/api/v1/pages/"+f.ID.String(), map[string]interface{}{"orgId": nil}, h.UserB.ID)
			require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())

			assertInOrg(t, orgID, getPage(t, h, h.UserA.ID, f.ID).OrgID, "an editor moved the owner's folder out of the org")
			assertInOrg(t, orgID, getPage(t, h, h.UserA.ID, sub.ID).OrgID, "an editor moved the folder's sub-page out of the org")
			assertInOrg(t, orgID, getTask(t, h, h.UserA.ID, task.ID).OrgID, "an editor moved the folder's task out of the org")
			// The editor can still make ordinary edits.
			w = h.Do(t, "PATCH", "/api/v1/pages/"+f.ID.String(), map[string]interface{}{"title": "Renamed"}, h.UserB.ID)
			assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
		},

		// A token scoped to the org only must not move the owner's data
		// out of it: it has no grant on Personal.
		"OrgScopedTokenCannotMovePageToPersonal": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			orgID := createOrgAs(t, h, h.UserA.ID, "Org")
			f, sub, _ := orgFolder(t, h, h.UserA.ID, orgID)

			w := h.DoAsToken(t, "PATCH", "/api/v1/pages/"+f.ID.String(), map[string]interface{}{"orgId": nil}, h.UserA.ID, orgOnlyScope(orgID))
			require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())

			assertInOrg(t, orgID, getPage(t, h, h.UserA.ID, f.ID).OrgID, "an org-scoped token moved the folder to Personal")
			assertInOrg(t, orgID, getPage(t, h, h.UserA.ID, sub.ID).OrgID, "an org-scoped token moved the sub-page to Personal")
		},

		// Nor may a token move a page into an org its grant doesn't cover.
		"PersonalScopedTokenCannotMovePageIntoOrg": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			orgID := createOrgAs(t, h, h.UserA.ID, "Org")
			p := createPage(t, h, h.UserA.ID, "Mine")

			w := h.DoAsToken(t, "PATCH", "/api/v1/pages/"+p.ID.String(), map[string]interface{}{"orgId": orgID}, h.UserA.ID, personalOnlyScope())
			require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
			assert.Nil(t, getPage(t, h, h.UserA.ID, p.ID).OrgID, "a Personal-only token moved the page into an org")
		},

		// The owner's token granted both workspaces may move between them.
		"TokenScopedToBothWorkspacesMovesOwnersPageToPersonal": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			orgID := createOrgAs(t, h, h.UserA.ID, "Org")
			f, sub, task := orgFolder(t, h, h.UserA.ID, orgID)
			scope := orgOnlyScope(orgID)
			scope.Personal = true

			w := h.DoAsToken(t, "PATCH", "/api/v1/pages/"+f.ID.String(), map[string]interface{}{"orgId": nil}, h.UserA.ID, scope)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			assert.Nil(t, getPage(t, h, h.UserA.ID, sub.ID).OrgID)
			assert.Nil(t, getTask(t, h, h.UserA.ID, task.ID).OrgID)
		},
	})
}

// createOrgTask creates, as ownerID, a task in orgID visible to the org.
func createOrgTask(t *testing.T, h *Harness, ownerID uuid.UUID, orgID string) model.Task {
	t.Helper()
	w := h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{"title": "Org task", "orgId": orgID, "isPrivate": false}, ownerID)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	return Decode[model.Task](t, w)
}

// createOrgTemplate creates, as ownerID, a template in orgID visible to the org.
func createOrgTemplate(t *testing.T, h *Harness, ownerID uuid.UUID, orgID string) model.Template {
	t.Helper()
	w := h.Do(t, "POST", "/api/v1/templates", map[string]interface{}{"name": "Org template", "orgId": orgID, "isPrivate": false}, ownerID)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	return Decode[model.Template](t, w)
}

func getTemplate(t *testing.T, h *Harness, userID, id uuid.UUID) model.Template {
	t.Helper()
	w := h.Do(t, "GET", "/api/v1/templates/"+id.String(), nil, userID)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	return Decode[model.Template](t, w)
}

// TestTaskAndTemplateWorkspaceMoveAuthorization applies
// TestPageWorkspaceMoveAuthorization's rule to every other write that can
// change a task's or template's org: PATCH {"orgId": null} (or another
// org), and PUT, which replaces the org with the body's (null when omitted).
func TestTaskAndTemplateWorkspaceMoveAuthorization(t *testing.T) {
	RunSpecs(t, map[string]func(t *testing.T, h *Harness){
		"OrgEditorCannotMoveColleaguesTaskToPersonal": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			orgID := createOrgAs(t, h, h.UserA.ID, "Org")
			addOrgMember(t, h, h.UserA.ID, orgID, h.UserB.ID, "editor")
			task := createOrgTask(t, h, h.UserA.ID, orgID)

			w := h.Do(t, "PATCH", "/api/v1/tasks/"+task.ID.String(), map[string]interface{}{"orgId": nil}, h.UserB.ID)
			require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
			assertInOrg(t, orgID, getTask(t, h, h.UserA.ID, task.ID).OrgID, "an editor moved the owner's task out of the org")

			w = h.Do(t, "PATCH", "/api/v1/tasks/"+task.ID.String(), map[string]interface{}{"title": "Renamed"}, h.UserB.ID)
			assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
		},

		"OrgScopedTokenCannotMoveTaskToPersonal": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			orgID := createOrgAs(t, h, h.UserA.ID, "Org")
			task := createOrgTask(t, h, h.UserA.ID, orgID)

			w := h.DoAsToken(t, "PATCH", "/api/v1/tasks/"+task.ID.String(), map[string]interface{}{"orgId": nil}, h.UserA.ID, orgOnlyScope(orgID))
			require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
			assertInOrg(t, orgID, getTask(t, h, h.UserA.ID, task.ID).OrgID, "an org-scoped token moved the task to Personal")
		},

		"PersonalScopedTokenCannotMoveTaskIntoOrg": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			orgID := createOrgAs(t, h, h.UserA.ID, "Org")
			w := h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{"title": "Mine"}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
			task := Decode[model.Task](t, w)

			w = h.DoAsToken(t, "PATCH", "/api/v1/tasks/"+task.ID.String(), map[string]interface{}{"orgId": orgID}, h.UserA.ID, personalOnlyScope())
			require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
			assert.Nil(t, getTask(t, h, h.UserA.ID, task.ID).OrgID, "a Personal-only token moved the task into an org")
		},

		// PUT replaces the org: a token without the org in its grant must
		// not rewrite (or take out of the org) a task that lives there.
		"PersonalScopedTokenCannotPutOrgTaskToPersonal": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			orgID := createOrgAs(t, h, h.UserA.ID, "Org")
			task := createOrgTask(t, h, h.UserA.ID, orgID)

			w := h.DoAsToken(t, "PUT", "/api/v1/tasks/"+task.ID.String(), map[string]interface{}{"title": "Org task", "orgId": nil}, h.UserA.ID, personalOnlyScope())
			require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
			assertInOrg(t, orgID, getTask(t, h, h.UserA.ID, task.ID).OrgID, "a Personal-only token PUT the org's task to Personal")
		},

		"OrgScopedTokenCannotPutOrgTaskToPersonal": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			orgID := createOrgAs(t, h, h.UserA.ID, "Org")
			task := createOrgTask(t, h, h.UserA.ID, orgID)

			w := h.DoAsToken(t, "PUT", "/api/v1/tasks/"+task.ID.String(), map[string]interface{}{"title": "Org task", "orgId": nil}, h.UserA.ID, orgOnlyScope(orgID))
			require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
			assertInOrg(t, orgID, getTask(t, h, h.UserA.ID, task.ID).OrgID, "an org-scoped token PUT the task to Personal")
		},

		"OrgEditorCannotMoveColleaguesTemplateToPersonal": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			orgID := createOrgAs(t, h, h.UserA.ID, "Org")
			addOrgMember(t, h, h.UserA.ID, orgID, h.UserB.ID, "editor")
			tmpl := createOrgTemplate(t, h, h.UserA.ID, orgID)

			w := h.Do(t, "PATCH", "/api/v1/templates/"+tmpl.ID.String(), map[string]interface{}{"orgId": nil}, h.UserB.ID)
			require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
			assertInOrg(t, orgID, getTemplate(t, h, h.UserA.ID, tmpl.ID).OrgID, "an editor moved the owner's template out of the org")

			w = h.Do(t, "PATCH", "/api/v1/templates/"+tmpl.ID.String(), map[string]interface{}{"name": "Renamed"}, h.UserB.ID)
			assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
		},

		"OrgScopedTokenCannotMoveTemplateToPersonal": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			orgID := createOrgAs(t, h, h.UserA.ID, "Org")
			tmpl := createOrgTemplate(t, h, h.UserA.ID, orgID)

			w := h.DoAsToken(t, "PATCH", "/api/v1/templates/"+tmpl.ID.String(), map[string]interface{}{"orgId": nil}, h.UserA.ID, orgOnlyScope(orgID))
			require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
			assertInOrg(t, orgID, getTemplate(t, h, h.UserA.ID, tmpl.ID).OrgID, "an org-scoped token moved the template to Personal")
		},

		"PersonalScopedTokenCannotPutOrgTemplateToPersonal": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			orgID := createOrgAs(t, h, h.UserA.ID, "Org")
			tmpl := createOrgTemplate(t, h, h.UserA.ID, orgID)

			w := h.DoAsToken(t, "PUT", "/api/v1/templates/"+tmpl.ID.String(), map[string]interface{}{"name": "Org template", "orgId": nil}, h.UserA.ID, personalOnlyScope())
			require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
			assertInOrg(t, orgID, getTemplate(t, h, h.UserA.ID, tmpl.ID).OrgID, "a Personal-only token PUT the org's template to Personal")
		},

		// Pages' PUT already checked the stored org against the token.
		"PersonalScopedTokenCannotPutOrgPageToPersonal": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			orgID := createOrgAs(t, h, h.UserA.ID, "Org")
			f, _, _ := orgFolder(t, h, h.UserA.ID, orgID)

			w := h.DoAsToken(t, "PUT", "/api/v1/pages/"+f.ID.String(), map[string]interface{}{"orgId": nil}, h.UserA.ID, personalOnlyScope())
			require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
			assertInOrg(t, orgID, getPage(t, h, h.UserA.ID, f.ID).OrgID, "a Personal-only token PUT the org's folder to Personal")
		},

		// Session owners keep moving their own things to Personal.
		"OwnerMovesTaskAndTemplateToPersonal": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			orgID := createOrgAs(t, h, h.UserA.ID, "Org")
			task := createOrgTask(t, h, h.UserA.ID, orgID)
			tmpl := createOrgTemplate(t, h, h.UserA.ID, orgID)

			w := h.Do(t, "PATCH", "/api/v1/tasks/"+task.ID.String(), map[string]interface{}{"orgId": nil}, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			assert.Nil(t, getTask(t, h, h.UserA.ID, task.ID).OrgID)
			w = h.Do(t, "PATCH", "/api/v1/templates/"+tmpl.ID.String(), map[string]interface{}{"orgId": nil}, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			assert.Nil(t, getTemplate(t, h, h.UserA.ID, tmpl.ID).OrgID)
		},
	})
}
