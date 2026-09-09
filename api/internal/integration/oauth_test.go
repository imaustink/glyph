package integration

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/url"
	"testing"

	"github.com/glyph/api/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── fixtures ─────────────────────────────────────────────────────────────────

// oauthFixture seeds an org owned by owner, with member added at role, and
// returns the org ID.
func (s *oauthServer) createOrg(t *testing.T, owner *model.User, name string) string {
	t.Helper()
	w := s.doJSON(t, "POST", "/api/v1/orgs", reqOpts{userID: &owner.ID, jsonVal: map[string]string{"name": name}})
	require.Equal(t, http.StatusCreated, w.Code)
	org := decodeOAuth[map[string]interface{}](t, w)
	return org["id"].(string)
}

func (s *oauthServer) addMember(t *testing.T, owner *model.User, orgID string, member *model.User, role string) {
	t.Helper()
	w := s.doJSON(t, "POST", "/api/v1/orgs/"+orgID+"/members", reqOpts{
		userID: &owner.ID,
		jsonVal: map[string]string{
			"userId": member.ID.String(),
			"role":   role,
		},
	})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
}

// createClient creates an OAuth client scoped to orgID via the admin API and
// returns the decoded client (with its one-time secret).
func (s *oauthServer) createClient(t *testing.T, owner *model.User, orgID string, scopes, redirectURIs, grantTypes []string) model.OAuthClientWithSecret {
	t.Helper()
	body := map[string]interface{}{
		"name":         "Test Agent",
		"scopes":       scopes,
		"redirectUris": redirectURIs,
	}
	if grantTypes != nil {
		body["grantTypes"] = grantTypes
	}
	w := s.doJSON(t, "POST", "/api/v1/orgs/"+orgID+"/oauth-clients", reqOpts{userID: &owner.ID, jsonVal: body})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	return decodeOAuth[model.OAuthClientWithSecret](t, w)
}

// ─── Admin CRUD lifecycle ───────────────────────────────────────────────────────

func TestOAuthAdminCRUD(t *testing.T) {
	s := setupOAuthServer(t)
	s.reset(t)

	alice := s.createUser(t, "sub-alice", "alice@test.com", "Alice")
	orgID := s.createOrg(t, alice, "Alice Org")
	org2ID := s.createOrg(t, alice, "Alice Org 2")

	t.Run("Create", func(t *testing.T) {
		client := s.createClient(t, alice, orgID,
			[]string{"page:read", "task:write"},
			[]string{"https://agent.example.com/callback"},
			[]string{"client_credentials", "authorization_code"},
		)
		assert.NotEmpty(t, client.ClientID)
		assert.NotEmpty(t, client.ClientSecret)
		assert.Equal(t, []model.OAuthScope{"page:read", "task:write"}, client.Scopes)
		assert.Contains(t, client.OrgIDs, mustParseUUID(t, orgID))
	})

	t.Run("List", func(t *testing.T) {
		s.createClient(t, alice, orgID, []string{"page:read"}, nil, nil)
		w := s.doJSON(t, "GET", "/api/v1/orgs/"+orgID+"/oauth-clients", reqOpts{userID: &alice.ID})
		require.Equal(t, http.StatusOK, w.Code)
		clients := decodeOAuth[[]model.OAuthClient](t, w)
		assert.GreaterOrEqual(t, len(clients), 1)
		assert.NotContains(t, w.Body.String(), "clientSecret", "list response must never carry a plaintext secret")
	})

	t.Run("Get", func(t *testing.T) {
		created := s.createClient(t, alice, orgID, []string{"page:read"}, nil, nil)
		w := s.doJSON(t, "GET", "/api/v1/orgs/"+orgID+"/oauth-clients/"+created.ID.String(), reqOpts{userID: &alice.ID})
		require.Equal(t, http.StatusOK, w.Code)
		fetched := decodeOAuth[model.OAuthClient](t, w)
		assert.Equal(t, created.ID, fetched.ID)
	})

	t.Run("Update_ScopesAndRedirectURIsAndName", func(t *testing.T) {
		created := s.createClient(t, alice, orgID, []string{"page:read"}, []string{"https://a.example.com/cb"}, nil)
		w := s.doJSON(t, "PATCH", "/api/v1/orgs/"+orgID+"/oauth-clients/"+created.ID.String(), reqOpts{
			userID: &alice.ID,
			jsonVal: map[string]interface{}{
				"name":         "Renamed Agent",
				"scopes":       []string{"page:read", "page:write"},
				"redirectUris": []string{"https://b.example.com/cb"},
			},
		})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		updated := decodeOAuth[model.OAuthClient](t, w)
		assert.Equal(t, "Renamed Agent", updated.Name)
		assert.ElementsMatch(t, []model.OAuthScope{"page:read", "page:write"}, updated.Scopes)
		assert.Equal(t, []string{"https://b.example.com/cb"}, updated.RedirectURIs)
	})

	t.Run("AddAndRemoveOrg", func(t *testing.T) {
		created := s.createClient(t, alice, orgID, []string{"page:read"}, nil, nil)

		w := s.doJSON(t, "POST", "/api/v1/orgs/"+orgID+"/oauth-clients/"+created.ID.String()+"/orgs", reqOpts{
			userID:  &alice.ID,
			jsonVal: map[string]string{"orgId": org2ID},
		})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		afterAdd := decodeOAuth[model.OAuthClient](t, w)
		assert.Contains(t, afterAdd.OrgIDs, mustParseUUID(t, org2ID))

		w = s.doJSON(t, "GET", "/api/v1/orgs/"+orgID+"/oauth-clients/"+created.ID.String(), reqOpts{userID: &alice.ID})
		fetched := decodeOAuth[model.OAuthClient](t, w)
		assert.Contains(t, fetched.OrgIDs, mustParseUUID(t, org2ID))

		w = s.doJSON(t, "DELETE", "/api/v1/orgs/"+orgID+"/oauth-clients/"+created.ID.String()+"/orgs/"+org2ID, reqOpts{userID: &alice.ID})
		require.Equal(t, http.StatusNoContent, w.Code)

		w = s.doJSON(t, "GET", "/api/v1/orgs/"+orgID+"/oauth-clients/"+created.ID.String(), reqOpts{userID: &alice.ID})
		fetched = decodeOAuth[model.OAuthClient](t, w)
		assert.NotContains(t, fetched.OrgIDs, mustParseUUID(t, org2ID))
	})

	t.Run("RotateSecret_OldSecretStopsWorkingNewWorks", func(t *testing.T) {
		bob := s.createUser(t, "sub-bob-rotate", "bob-rotate@test.com", "Bob")
		s.addMember(t, alice, orgID, bob, "viewer")

		created := s.createClient(t, alice, orgID, []string{"page:read"}, nil, nil)

		form := url.Values{
			"grant_type": {"client_credentials"},
			"subject":    {bob.ID.String()},
			"org_id":     {orgID},
		}
		w := s.doForm(t, "POST", "/oauth/token", form, created.ClientID, created.ClientSecret)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		w = s.doJSON(t, "POST", "/api/v1/orgs/"+orgID+"/oauth-clients/"+created.ID.String()+"/rotate-secret", reqOpts{userID: &alice.ID})
		require.Equal(t, http.StatusOK, w.Code)
		rotated := decodeOAuth[model.OAuthClientWithSecret](t, w)
		assert.NotEqual(t, created.ClientSecret, rotated.ClientSecret)

		// Old secret no longer works.
		w = s.doForm(t, "POST", "/oauth/token", form, created.ClientID, created.ClientSecret)
		assert.Equal(t, http.StatusUnauthorized, w.Code)

		// New secret works.
		w = s.doForm(t, "POST", "/oauth/token", form, rotated.ClientID, rotated.ClientSecret)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("RevokeClient_TokensStopWorking", func(t *testing.T) {
		bob := s.createUser(t, "sub-bob-revoke", "bob-revoke@test.com", "Bob")
		s.addMember(t, alice, orgID, bob, "viewer")
		created := s.createClient(t, alice, orgID, []string{"page:read"}, nil, nil)

		form := url.Values{"grant_type": {"client_credentials"}, "subject": {bob.ID.String()}, "org_id": {orgID}}
		w := s.doForm(t, "POST", "/oauth/token", form, created.ClientID, created.ClientSecret)
		require.Equal(t, http.StatusOK, w.Code)
		tok := decodeOAuth[map[string]interface{}](t, w)
		accessToken := tok["access_token"].(string)

		// Token works before revocation.
		w = s.doJSON(t, "GET", "/api/v1/pages", reqOpts{bearer: accessToken})
		require.Equal(t, http.StatusOK, w.Code)

		w = s.doJSON(t, "POST", "/api/v1/orgs/"+orgID+"/oauth-clients/"+created.ID.String()+"/revoke", reqOpts{userID: &alice.ID})
		require.Equal(t, http.StatusNoContent, w.Code)

		// Token no longer works after revocation.
		w = s.doJSON(t, "GET", "/api/v1/pages", reqOpts{bearer: accessToken})
		assert.Equal(t, http.StatusUnauthorized, w.Code)

		// New tokens can't be issued for a revoked client either.
		w = s.doForm(t, "POST", "/oauth/token", form, created.ClientID, created.ClientSecret)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("ListAndRevokeIndividualToken", func(t *testing.T) {
		bob := s.createUser(t, "sub-bob-list-tok", "bob-list-tok@test.com", "Bob")
		s.addMember(t, alice, orgID, bob, "viewer")
		created := s.createClient(t, alice, orgID, []string{"page:read"}, nil, nil)

		form := url.Values{"grant_type": {"client_credentials"}, "subject": {bob.ID.String()}, "org_id": {orgID}}
		w := s.doForm(t, "POST", "/oauth/token", form, created.ClientID, created.ClientSecret)
		require.Equal(t, http.StatusOK, w.Code)
		tok := decodeOAuth[map[string]interface{}](t, w)
		accessToken := tok["access_token"].(string)

		w = s.doJSON(t, "GET", "/api/v1/orgs/"+orgID+"/oauth-clients/"+created.ID.String()+"/tokens", reqOpts{userID: &alice.ID})
		require.Equal(t, http.StatusOK, w.Code)
		tokens := decodeOAuth[[]model.OAuthToken](t, w)
		require.Len(t, tokens, 1)
		assert.Equal(t, bob.ID, tokens[0].ActingUserID)

		w = s.doJSON(t, "DELETE", "/api/v1/orgs/"+orgID+"/oauth-clients/"+created.ID.String()+"/tokens/"+tokens[0].ID.String(), reqOpts{userID: &alice.ID})
		require.Equal(t, http.StatusNoContent, w.Code)

		w = s.doJSON(t, "GET", "/api/v1/pages", reqOpts{bearer: accessToken})
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("RevokeAllTokens", func(t *testing.T) {
		bob := s.createUser(t, "sub-bob-revoke-all", "bob-revoke-all@test.com", "Bob")
		s.addMember(t, alice, orgID, bob, "viewer")
		created := s.createClient(t, alice, orgID, []string{"page:read"}, nil, nil)

		var tokens []string
		for i := 0; i < 3; i++ {
			form := url.Values{"grant_type": {"client_credentials"}, "subject": {bob.ID.String()}, "org_id": {orgID}}
			w := s.doForm(t, "POST", "/oauth/token", form, created.ClientID, created.ClientSecret)
			require.Equal(t, http.StatusOK, w.Code)
			tok := decodeOAuth[map[string]interface{}](t, w)
			tokens = append(tokens, tok["access_token"].(string))
		}

		w := s.doJSON(t, "POST", "/api/v1/orgs/"+orgID+"/oauth-clients/"+created.ID.String()+"/tokens/revoke-all", reqOpts{userID: &alice.ID})
		require.Equal(t, http.StatusNoContent, w.Code)

		for _, tok := range tokens {
			w := s.doJSON(t, "GET", "/api/v1/pages", reqOpts{bearer: tok})
			assert.Equal(t, http.StatusUnauthorized, w.Code)
		}
	})
}

// ─── Authorization gating on the admin API ─────────────────────────────────────

func TestOAuthAdminAuthzGating(t *testing.T) {
	s := setupOAuthServer(t)
	s.reset(t)

	alice := s.createUser(t, "sub-alice-g", "alice-g@test.com", "Alice")
	bobEditor := s.createUser(t, "sub-bob-g", "bob-g@test.com", "Bob")
	carolViewer := s.createUser(t, "sub-carol-g", "carol-g@test.com", "Carol")
	dave := s.createUser(t, "sub-dave-g", "dave-g@test.com", "Dave") // not a member at all

	orgID := s.createOrg(t, alice, "Gated Org")
	s.addMember(t, alice, orgID, bobEditor, "editor")
	s.addMember(t, alice, orgID, carolViewer, "viewer")

	created := s.createClient(t, alice, orgID, []string{"page:read"}, nil, nil)

	t.Run("NonOwnerEditor_Gets403", func(t *testing.T) {
		w := s.doJSON(t, "GET", "/api/v1/orgs/"+orgID+"/oauth-clients", reqOpts{userID: &bobEditor.ID})
		assert.Equal(t, http.StatusForbidden, w.Code)
		w = s.doJSON(t, "POST", "/api/v1/orgs/"+orgID+"/oauth-clients", reqOpts{userID: &bobEditor.ID, jsonVal: map[string]interface{}{"name": "x"}})
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("NonOwnerViewer_Gets403", func(t *testing.T) {
		w := s.doJSON(t, "GET", "/api/v1/orgs/"+orgID+"/oauth-clients/"+created.ID.String(), reqOpts{userID: &carolViewer.ID})
		assert.Equal(t, http.StatusForbidden, w.Code)
		w = s.doJSON(t, "POST", "/api/v1/orgs/"+orgID+"/oauth-clients/"+created.ID.String()+"/revoke", reqOpts{userID: &carolViewer.ID})
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("NonMember_Gets404_NotLeakingOrgExistence", func(t *testing.T) {
		w := s.doJSON(t, "GET", "/api/v1/orgs/"+orgID+"/oauth-clients", reqOpts{userID: &dave.ID})
		assert.Equal(t, http.StatusNotFound, w.Code)
		w = s.doJSON(t, "GET", "/api/v1/orgs/"+orgID+"/oauth-clients/"+created.ID.String(), reqOpts{userID: &dave.ID})
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("BearerAuthenticatedCaller_CannotManageClients", func(t *testing.T) {
		s.addMember(t, alice, orgID, dave, "viewer")
		form := url.Values{"grant_type": {"client_credentials"}, "subject": {dave.ID.String()}, "org_id": {orgID}}
		w := s.doForm(t, "POST", "/oauth/token", form, created.ClientID, created.ClientSecret)
		require.Equal(t, http.StatusOK, w.Code)
		tok := decodeOAuth[map[string]interface{}](t, w)
		accessToken := tok["access_token"].(string)

		// Even though the acting user (dave) is a legitimate member, a
		// bearer-authenticated request must never reach the admin API.
		w = s.doJSON(t, "GET", "/api/v1/orgs/"+orgID+"/oauth-clients", reqOpts{bearer: accessToken})
		assert.Equal(t, http.StatusForbidden, w.Code)
	})
}

// ─── client_credentials (+ subject) grant, end-to-end ──────────────────────────

func TestOAuthClientCredentialsFlow(t *testing.T) {
	s := setupOAuthServer(t)
	s.reset(t)

	alice := s.createUser(t, "sub-alice-cc", "alice-cc@test.com", "Alice")
	member := s.createUser(t, "sub-member-cc", "member-cc@test.com", "Member")
	outsider := s.createUser(t, "sub-outsider-cc", "outsider-cc@test.com", "Outsider")

	orgID := s.createOrg(t, alice, "CC Org")
	otherOrgID := s.createOrg(t, alice, "CC Other Org")
	s.addMember(t, alice, orgID, member, "editor")

	client := s.createClient(t, alice, orgID, []string{"page:read", "page:write", "task:read"}, nil, nil)

	t.Run("Success_SubjectIsMember", func(t *testing.T) {
		form := url.Values{"grant_type": {"client_credentials"}, "subject": {member.ID.String()}, "org_id": {orgID}}
		w := s.doForm(t, "POST", "/oauth/token", form, client.ClientID, client.ClientSecret)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		tok := decodeOAuth[map[string]interface{}](t, w)
		assert.NotEmpty(t, tok["access_token"])
		assert.Equal(t, member.ID.String(), tok["acting_user_id"])
		assert.Nil(t, tok["refresh_token"], "client_credentials must not issue a refresh token")
	})

	t.Run("Success_SubjectByEmail", func(t *testing.T) {
		form := url.Values{"grant_type": {"client_credentials"}, "subject": {"member-cc@test.com"}, "org_id": {orgID}}
		w := s.doForm(t, "POST", "/oauth/token", form, client.ClientID, client.ClientSecret)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	})

	t.Run("Rejected_SubjectNotOrgMember", func(t *testing.T) {
		form := url.Values{"grant_type": {"client_credentials"}, "subject": {outsider.ID.String()}, "org_id": {orgID}}
		w := s.doForm(t, "POST", "/oauth/token", form, client.ClientID, client.ClientSecret)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("Rejected_ClientNotScopedToOrg", func(t *testing.T) {
		form := url.Values{"grant_type": {"client_credentials"}, "subject": {alice.ID.String()}, "org_id": {otherOrgID}}
		w := s.doForm(t, "POST", "/oauth/token", form, client.ClientID, client.ClientSecret)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("Rejected_WrongClientSecret", func(t *testing.T) {
		form := url.Values{"grant_type": {"client_credentials"}, "subject": {member.ID.String()}, "org_id": {orgID}}
		w := s.doForm(t, "POST", "/oauth/token", form, client.ClientID, "wrong-secret")
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("Rejected_RevokedClient", func(t *testing.T) {
		revocable := s.createClient(t, alice, orgID, []string{"page:read"}, nil, nil)
		w := s.doJSON(t, "POST", "/api/v1/orgs/"+orgID+"/oauth-clients/"+revocable.ID.String()+"/revoke", reqOpts{userID: &alice.ID})
		require.Equal(t, http.StatusNoContent, w.Code)
		form := url.Values{"grant_type": {"client_credentials"}, "subject": {member.ID.String()}, "org_id": {orgID}}
		w = s.doForm(t, "POST", "/oauth/token", form, revocable.ClientID, revocable.ClientSecret)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("ScopeIntersection_RequestingOutOfGrantScopeIsDropped", func(t *testing.T) {
		// Client only has page:read/page:write/task:read; requesting
		// task:write must not be granted even though it's a "real" scope.
		form := url.Values{
			"grant_type": {"client_credentials"},
			"subject":    {member.ID.String()},
			"org_id":     {orgID},
			"scope":      {"page:read task:write"},
		}
		w := s.doForm(t, "POST", "/oauth/token", form, client.ClientID, client.ClientSecret)
		require.Equal(t, http.StatusOK, w.Code)
		tok := decodeOAuth[map[string]interface{}](t, w)
		assert.Equal(t, "page:read", tok["scope"])
	})

	t.Run("IssuedToken_CanAccessScopedOrgResource", func(t *testing.T) {
		page := decodeOAuth[model.Page](t, s.doJSON(t, "POST", "/api/v1/pages", reqOpts{
			userID:  &alice.ID,
			jsonVal: map[string]interface{}{"title": "Org Page", "type": "page"},
		}))
		// CreatePage always defaults new pages to private (see PageHandler.
		// CreatePage); a PATCH is required to share it with the org so the
		// delegated (non-owner) acting user below can see it.
		w0 := s.doJSON(t, "PATCH", "/api/v1/pages/"+page.ID.String(), reqOpts{
			userID:  &alice.ID,
			jsonVal: map[string]interface{}{"title": "Org Page", "type": "page", "orgId": orgID, "isPrivate": false},
		})
		require.Equal(t, http.StatusOK, w0.Code, w0.Body.String())

		form := url.Values{"grant_type": {"client_credentials"}, "subject": {member.ID.String()}, "org_id": {orgID}}
		w := s.doForm(t, "POST", "/oauth/token", form, client.ClientID, client.ClientSecret)
		require.Equal(t, http.StatusOK, w.Code)
		tok := decodeOAuth[map[string]interface{}](t, w)
		accessToken := tok["access_token"].(string)

		w = s.doJSON(t, "GET", "/api/v1/pages/"+page.ID.String(), reqOpts{bearer: accessToken})
		assert.Equal(t, http.StatusOK, w.Code)

		w = s.doJSON(t, "GET", "/api/v1/pages", reqOpts{bearer: accessToken})
		require.Equal(t, http.StatusOK, w.Code)
		pages := decodeOAuth[[]model.Page](t, w)
		found := false
		for _, p := range pages {
			if p.ID == page.ID {
				found = true
			}
		}
		assert.True(t, found, "scoped org page should be visible via bearer token")
	})

	t.Run("IssuedToken_CannotReachDifferentOrg", func(t *testing.T) {
		otherPage := decodeOAuth[model.Page](t, s.doJSON(t, "POST", "/api/v1/pages", reqOpts{
			userID:  &alice.ID,
			jsonVal: map[string]interface{}{"title": "Other Org Page", "type": "page", "orgId": otherOrgID, "isPrivate": false},
		}))

		form := url.Values{"grant_type": {"client_credentials"}, "subject": {alice.ID.String()}, "org_id": {orgID}}
		w := s.doForm(t, "POST", "/oauth/token", form, client.ClientID, client.ClientSecret)
		require.Equal(t, http.StatusOK, w.Code)
		tok := decodeOAuth[map[string]interface{}](t, w)
		accessToken := tok["access_token"].(string)

		w = s.doJSON(t, "GET", "/api/v1/pages/"+otherPage.ID.String(), reqOpts{bearer: accessToken})
		assert.Equal(t, http.StatusForbidden, w.Code)

		w = s.doJSON(t, "GET", "/api/v1/pages", reqOpts{bearer: accessToken})
		require.Equal(t, http.StatusOK, w.Code)
		pages := decodeOAuth[[]model.Page](t, w)
		for _, p := range pages {
			assert.NotEqual(t, otherPage.ID, p.ID, "page from an org outside the token's scope must not be listed")
		}
	})

	t.Run("IssuedToken_CannotReachPersonalResource", func(t *testing.T) {
		personalPage := decodeOAuth[model.Page](t, s.doJSON(t, "POST", "/api/v1/pages", reqOpts{
			userID:  &alice.ID,
			jsonVal: map[string]interface{}{"title": "Alice's Personal Page", "type": "page"},
		}))
		assert.Nil(t, personalPage.OrgID)

		form := url.Values{"grant_type": {"client_credentials"}, "subject": {alice.ID.String()}, "org_id": {orgID}}
		w := s.doForm(t, "POST", "/oauth/token", form, client.ClientID, client.ClientSecret)
		require.Equal(t, http.StatusOK, w.Code)
		tok := decodeOAuth[map[string]interface{}](t, w)
		accessToken := tok["access_token"].(string)

		w = s.doJSON(t, "GET", "/api/v1/pages/"+personalPage.ID.String(), reqOpts{bearer: accessToken})
		assert.Equal(t, http.StatusForbidden, w.Code, "a delegated org-scoped token must never reach the user's personal, non-org resources")
	})

	t.Run("ReadOnlyToken_CannotWrite", func(t *testing.T) {
		readOnlyClient := s.createClient(t, alice, orgID, []string{"page:read"}, nil, nil)
		form := url.Values{"grant_type": {"client_credentials"}, "subject": {member.ID.String()}, "org_id": {orgID}}
		w := s.doForm(t, "POST", "/oauth/token", form, readOnlyClient.ClientID, readOnlyClient.ClientSecret)
		require.Equal(t, http.StatusOK, w.Code)
		tok := decodeOAuth[map[string]interface{}](t, w)
		accessToken := tok["access_token"].(string)

		// Read works.
		w = s.doJSON(t, "GET", "/api/v1/pages", reqOpts{bearer: accessToken})
		assert.Equal(t, http.StatusOK, w.Code)

		// Write does not.
		w = s.doJSON(t, "POST", "/api/v1/pages", reqOpts{
			bearer:  accessToken,
			jsonVal: map[string]interface{}{"title": "Should Fail", "type": "page", "orgId": orgID, "isPrivate": false},
		})
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("WriteScopedToken_CanCreateAndUpdate", func(t *testing.T) {
		writeClient := s.createClient(t, alice, orgID, []string{"task:read", "task:write"}, nil, nil)
		form := url.Values{"grant_type": {"client_credentials"}, "subject": {member.ID.String()}, "org_id": {orgID}}
		w := s.doForm(t, "POST", "/oauth/token", form, writeClient.ClientID, writeClient.ClientSecret)
		require.Equal(t, http.StatusOK, w.Code)
		tok := decodeOAuth[map[string]interface{}](t, w)
		accessToken := tok["access_token"].(string)

		w = s.doJSON(t, "POST", "/api/v1/tasks", reqOpts{
			bearer:  accessToken,
			jsonVal: map[string]interface{}{"title": "Agent Task", "orgId": orgID, "isPrivate": false},
		})
		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		task := decodeOAuth[model.Task](t, w)
		assert.Equal(t, member.ID, task.UserID, "task is created as the delegated acting user, not the client")

		w = s.doJSON(t, "PATCH", "/api/v1/tasks/"+task.ID.String(), reqOpts{
			bearer:  accessToken,
			jsonVal: map[string]interface{}{"title": "Updated by agent"},
		})
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

// ─── authorization_code + PKCE grant, end-to-end ───────────────────────────────

func pkcePair() (verifier, challenge string) {
	// A fixed, spec-valid (43-128 char) verifier is fine for tests: PKCE only
	// needs to be unpredictable to an attacker who never sees the verifier,
	// not unique per test run.
	verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk-0123456789ABCDEFGHIJ"
	sum := sha256Sum(verifier)
	challenge = base64URLEncode(sum)
	return
}

func TestOAuthAuthorizationCodeFlow(t *testing.T) {
	s := setupOAuthServer(t)
	s.reset(t)

	alice := s.createUser(t, "sub-alice-ac", "alice-ac@test.com", "Alice")
	orgID := s.createOrg(t, alice, "AC Org")
	redirectURI := "https://third-party.example.com/callback"
	client := s.createClient(t, alice, orgID, []string{"page:read", "page:write"}, []string{redirectURI}, []string{"authorization_code", "client_credentials"})

	authorizeQuery := func(verifier, challenge, state string) url.Values {
		return url.Values{
			"response_type":         {"code"},
			"client_id":             {client.ClientID},
			"redirect_uri":          {redirectURI},
			"scope":                 {"page:read"},
			"state":                 {state},
			"code_challenge":        {challenge},
			"code_challenge_method": {"S256"},
			"org_id":                {orgID},
		}
	}

	t.Run("FullFlow_ApproveThenExchangeThenRefresh", func(t *testing.T) {
		verifier, challenge := pkcePair()
		q := authorizeQuery(verifier, challenge, "xyz-state")
		w := s.doJSON(t, "GET", "/api/v1/oauth/consent?"+q.Encode(), reqOpts{userID: &alice.ID})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		info := decodeOAuth[map[string]interface{}](t, w)
		clientInfo, ok := info["client"].(map[string]interface{})
		require.True(t, ok, "expected a nested client object, got: %s", w.Body.String())
		assert.Equal(t, "Test Agent", clientInfo["name"])
		assert.Contains(t, info["scopes"], "page:read")
		consentToken := info["consentToken"].(string)

		w = s.doJSON(t, "POST", "/api/v1/oauth/consent/decision", reqOpts{
			userID:  &alice.ID,
			jsonVal: map[string]interface{}{"consentToken": consentToken, "approve": true},
		})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		decision := decodeOAuth[map[string]interface{}](t, w)
		redirectURL, err := url.Parse(decision["redirectUrl"].(string))
		require.NoError(t, err)
		code := redirectURL.Query().Get("code")
		require.NotEmpty(t, code)
		assert.Equal(t, "xyz-state", redirectURL.Query().Get("state"))

		// Exchange the code for tokens.
		tokenForm := url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {code},
			"redirect_uri":  {redirectURI},
			"code_verifier": {verifier},
		}
		w = s.doForm(t, "POST", "/oauth/token", tokenForm, client.ClientID, client.ClientSecret)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		tok := decodeOAuth[map[string]interface{}](t, w)
		accessToken := tok["access_token"].(string)
		refreshToken := tok["refresh_token"].(string)
		assert.NotEmpty(t, accessToken)
		assert.NotEmpty(t, refreshToken)

		// The token works against a real resource endpoint.
		w = s.doJSON(t, "GET", "/api/v1/pages", reqOpts{bearer: accessToken})
		assert.Equal(t, http.StatusOK, w.Code)

		// Reusing an already-consumed code fails.
		w = s.doForm(t, "POST", "/oauth/token", tokenForm, client.ClientID, client.ClientSecret)
		assert.Equal(t, http.StatusBadRequest, w.Code)

		// Refresh rotation: old refresh token works once...
		refreshForm := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}, "client_id": {client.ClientID}, "client_secret": {client.ClientSecret}}
		w = s.doForm(t, "POST", "/oauth/token", refreshForm, "", "")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		rotated := decodeOAuth[map[string]interface{}](t, w)
		newAccessToken := rotated["access_token"].(string)
		newRefreshToken := rotated["refresh_token"].(string)
		assert.NotEqual(t, accessToken, newAccessToken)
		assert.NotEqual(t, refreshToken, newRefreshToken)

		// ...and the old refresh token can no longer be used (rotation-on-use).
		w = s.doForm(t, "POST", "/oauth/token", refreshForm, "", "")
		assert.Equal(t, http.StatusBadRequest, w.Code)

		// New access token works.
		w = s.doJSON(t, "GET", "/api/v1/pages", reqOpts{bearer: newAccessToken})
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("Deny_ReturnsAccessDeniedAndNoUsableCode", func(t *testing.T) {
		verifier, challenge := pkcePair()
		q := authorizeQuery(verifier, challenge, "deny-state")
		w := s.doJSON(t, "GET", "/api/v1/oauth/consent?"+q.Encode(), reqOpts{userID: &alice.ID})
		require.Equal(t, http.StatusOK, w.Code)
		info := decodeOAuth[map[string]interface{}](t, w)
		consentToken := info["consentToken"].(string)

		w = s.doJSON(t, "POST", "/api/v1/oauth/consent/decision", reqOpts{
			userID:  &alice.ID,
			jsonVal: map[string]interface{}{"consentToken": consentToken, "approve": false},
		})
		require.Equal(t, http.StatusOK, w.Code)
		decision := decodeOAuth[map[string]interface{}](t, w)
		redirectURL, err := url.Parse(decision["redirectUrl"].(string))
		require.NoError(t, err)
		assert.Equal(t, "access_denied", redirectURL.Query().Get("error"))
		assert.Equal(t, "deny-state", redirectURL.Query().Get("state"))
		assert.Empty(t, redirectURL.Query().Get("code"))
	})

	t.Run("Rejected_PKCEVerifierMismatch", func(t *testing.T) {
		verifier, challenge := pkcePair()
		q := authorizeQuery(verifier, challenge, "pkce-state")
		w := s.doJSON(t, "GET", "/api/v1/oauth/consent?"+q.Encode(), reqOpts{userID: &alice.ID})
		require.Equal(t, http.StatusOK, w.Code)
		info := decodeOAuth[map[string]interface{}](t, w)
		consentToken := info["consentToken"].(string)

		w = s.doJSON(t, "POST", "/api/v1/oauth/consent/decision", reqOpts{userID: &alice.ID, jsonVal: map[string]interface{}{"consentToken": consentToken, "approve": true}})
		require.Equal(t, http.StatusOK, w.Code)
		decision := decodeOAuth[map[string]interface{}](t, w)
		redirectURL, _ := url.Parse(decision["redirectUrl"].(string))
		code := redirectURL.Query().Get("code")

		tokenForm := url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {code},
			"redirect_uri":  {redirectURI},
			"code_verifier": {"wrong-verifier-wrong-verifier-wrong-verifier-000000000"},
		}
		w = s.doForm(t, "POST", "/oauth/token", tokenForm, client.ClientID, client.ClientSecret)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("Rejected_UnregisteredRedirectURI", func(t *testing.T) {
		_, challenge := pkcePair()
		q := authorizeQuery("", challenge, "bad-redirect-state")
		q.Set("redirect_uri", "https://evil.example.com/callback")
		w := s.doJSON(t, "GET", "/api/v1/oauth/consent?"+q.Encode(), reqOpts{userID: &alice.ID})
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, "", w.Header().Get("Location"), "server must never issue a redirect to an unregistered redirect_uri")
	})

	t.Run("Rejected_ExpiredCode", func(t *testing.T) {
		verifier, challenge := pkcePair()
		q := authorizeQuery(verifier, challenge, "expired-state")
		w := s.doJSON(t, "GET", "/api/v1/oauth/consent?"+q.Encode(), reqOpts{userID: &alice.ID})
		require.Equal(t, http.StatusOK, w.Code)
		info := decodeOAuth[map[string]interface{}](t, w)
		consentToken := info["consentToken"].(string)

		w = s.doJSON(t, "POST", "/api/v1/oauth/consent/decision", reqOpts{userID: &alice.ID, jsonVal: map[string]interface{}{"consentToken": consentToken, "approve": true}})
		require.Equal(t, http.StatusOK, w.Code)
		decision := decodeOAuth[map[string]interface{}](t, w)
		redirectURL, _ := url.Parse(decision["redirectUrl"].(string))
		code := redirectURL.Query().Get("code")

		// Force the code to be expired by rewriting its row directly, rather
		// than sleeping for the real ~60s TTL.
		_, err := s.pool.Exec(context.Background(),
			"UPDATE oauth_authorization_codes SET expires_at = now() - interval '1 minute' WHERE code_hash = $1",
			hashOpaqueToken(code))
		require.NoError(t, err)

		tokenForm := url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {code},
			"redirect_uri":  {redirectURI},
			"code_verifier": {verifier},
		}
		w = s.doForm(t, "POST", "/oauth/token", tokenForm, client.ClientID, client.ClientSecret)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// ─── CSRF interaction ───────────────────────────────────────────────────────────

func TestOAuthCSRFInteraction(t *testing.T) {
	s := setupOAuthServer(t)
	s.reset(t)

	alice := s.createUser(t, "sub-alice-csrf", "alice-csrf@test.com", "Alice")
	member := s.createUser(t, "sub-member-csrf", "member-csrf@test.com", "Member")
	orgID := s.createOrg(t, alice, "CSRF Org")
	s.addMember(t, alice, orgID, member, "editor")
	client := s.createClient(t, alice, orgID, []string{"page:read", "page:write"}, nil, nil)

	t.Run("SessionRequestWithoutCSRFHeader_StillRejected", func(t *testing.T) {
		w := s.doJSON(t, "POST", "/api/v1/pages", reqOpts{
			userID:   &alice.ID,
			omitCSRF: true,
			jsonVal:  map[string]interface{}{"title": "No CSRF header", "type": "page"},
		})
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("SessionRequestWithCSRFHeader_Succeeds", func(t *testing.T) {
		w := s.doJSON(t, "POST", "/api/v1/pages", reqOpts{
			userID:  &alice.ID,
			jsonVal: map[string]interface{}{"title": "Has CSRF header", "type": "page"},
		})
		assert.Equal(t, http.StatusCreated, w.Code)
	})

	t.Run("BearerRequestWithoutCSRFHeader_StillSucceeds", func(t *testing.T) {
		form := url.Values{"grant_type": {"client_credentials"}, "subject": {member.ID.String()}, "org_id": {orgID}}
		w := s.doForm(t, "POST", "/oauth/token", form, client.ClientID, client.ClientSecret)
		require.Equal(t, http.StatusOK, w.Code)
		tok := decodeOAuth[map[string]interface{}](t, w)
		accessToken := tok["access_token"].(string)

		// No X-Requested-With header set, and no CSRF header possible for an
		// agent — must still succeed since bearer tokens carry no ambient
		// cookie credential for a forged cross-site request to exploit.
		w = s.doJSON(t, "POST", "/api/v1/pages", reqOpts{
			bearer:  accessToken,
			jsonVal: map[string]interface{}{"title": "Agent page", "type": "page", "orgId": orgID, "isPrivate": false},
		})
		assert.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	})
}

// Rate limiting on /oauth/token is intentionally not covered here: the
// limiter's window (20 req/min, see internal/oauth/routes.go) would require
// either firing 20+ real requests (slow) or exposing internal test hooks to
// fast-forward its clock, and the limiter itself (handler.RateLimiter) is
// already covered by its own unit tests. An integration-level assertion here
// would mostly restate that coverage while making this suite slower/flakier.

// ─── PKCE test helpers ──────────────────────────────────────────────────────────

func sha256Sum(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}

func base64URLEncode(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}
