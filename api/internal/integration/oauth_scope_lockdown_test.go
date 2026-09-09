package integration

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOAuthBearerTokenScopeLockdown regression-tests the fix for routes that
// used to grant an OAuth bearer token the acting user's full permissions with
// no scope check at all — lanes, org administration, sharing, user
// directory search, and folder boards have no corresponding OAuth scope, so
// a bearer token must never reach them, regardless of what scopes it holds.
func TestOAuthBearerTokenScopeLockdown(t *testing.T) {
	s := setupOAuthServer(t)
	s.reset(t)

	alice := s.createUser(t, "sub-alice-lockdown", "alice-lockdown@test.com", "Alice")
	member := s.createUser(t, "sub-member-lockdown", "member-lockdown@test.com", "Member")
	orgID := s.createOrg(t, alice, "Lockdown Org")
	s.addMember(t, alice, orgID, member, "editor")

	// A client granted every real scope that exists — if the lockdown works,
	// none of that helps it reach the routes below.
	client := s.createClient(t, alice, orgID,
		[]string{"page:read", "page:write", "task:read", "task:write", "template:read", "template:write", "org:read"},
		nil, nil)
	form := url.Values{"grant_type": {"client_credentials"}, "subject": {member.ID.String()}, "org_id": {orgID}}
	w := s.doForm(t, "POST", "/oauth/token", form, client.ClientID, client.ClientSecret)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	tok := decodeOAuth[map[string]interface{}](t, w)
	accessToken := tok["access_token"].(string)

	t.Run("LanesRejectBearer", func(t *testing.T) {
		w := s.doJSON(t, "GET", "/api/v1/lanes", reqOpts{bearer: accessToken})
		assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())

		w = s.doJSON(t, "POST", "/api/v1/lanes", reqOpts{
			bearer:  accessToken,
			jsonVal: map[string]interface{}{"title": "Injected Lane", "order": 0},
		})
		assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	})

	t.Run("OrgAdministrationRejectsBearer", func(t *testing.T) {
		// Read-only org listing IS allowed (org:read is a real, granted scope).
		w := s.doJSON(t, "GET", "/api/v1/orgs", reqOpts{bearer: accessToken})
		assert.Equal(t, http.StatusOK, w.Code, w.Body.String())

		// But member management is administration, not "reading org info",
		// and has no corresponding write scope at all.
		w = s.doJSON(t, "POST", "/api/v1/orgs/"+orgID+"/members", reqOpts{
			bearer:  accessToken,
			jsonVal: map[string]interface{}{"userId": alice.ID.String(), "role": "owner"},
		})
		assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	})

	t.Run("CreateShareRejectsBearer", func(t *testing.T) {
		page := decodeOAuth[map[string]interface{}](t, s.doJSON(t, "POST", "/api/v1/pages", reqOpts{
			userID:  &alice.ID,
			jsonVal: map[string]interface{}{"title": "A Page", "type": "page"},
		}))
		w := s.doJSON(t, "POST", "/api/v1/shares", reqOpts{
			bearer: accessToken,
			jsonVal: map[string]interface{}{
				"resourceType": "page", "resourceId": page["id"],
				"sharedWithId": member.ID.String(), "permission": "editor",
			},
		})
		assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	})

	t.Run("UserSearchRejectsBearer", func(t *testing.T) {
		w := s.doJSON(t, "GET", "/api/v1/users/search?q=alice", reqOpts{bearer: accessToken})
		assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	})

	t.Run("FolderRoutesRejectBearer", func(t *testing.T) {
		folder := decodeOAuth[map[string]interface{}](t, s.doJSON(t, "POST", "/api/v1/pages", reqOpts{
			userID:  &alice.ID,
			jsonVal: map[string]interface{}{"title": "A Folder", "type": "folder"},
		}))
		folderID := folder["id"].(string)

		w := s.doJSON(t, "GET", "/api/v1/folders/"+folderID, reqOpts{bearer: accessToken})
		assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())

		w = s.doJSON(t, "GET", "/api/v1/folders/"+folderID+"/lanes", reqOpts{bearer: accessToken})
		assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	})

	// Sanity check: the same client can still do everything it's actually
	// scoped for — this lockdown must not have collaterally broken
	// legitimate scoped access.
	t.Run("ScopedRoutesStillWork", func(t *testing.T) {
		w := s.doJSON(t, "GET", "/api/v1/pages", reqOpts{bearer: accessToken})
		assert.Equal(t, http.StatusOK, w.Code, w.Body.String())

		w = s.doJSON(t, "GET", "/api/v1/tasks", reqOpts{bearer: accessToken})
		assert.Equal(t, http.StatusOK, w.Code, w.Body.String())

		w = s.doJSON(t, "GET", "/api/v1/templates", reqOpts{bearer: accessToken})
		assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	})
}

// TestOAuthBearerTokenCannotReadPageContentOutOfScope regression-tests
// GetPageContent, which used to have no scope check at all despite
// GetPage (one route over) enforcing it correctly.
func TestOAuthBearerTokenCannotReadPageContentOutOfScope(t *testing.T) {
	s := setupOAuthServer(t)
	s.reset(t)

	alice := s.createUser(t, "sub-alice-content", "alice-content@test.com", "Alice")
	member := s.createUser(t, "sub-member-content", "member-content@test.com", "Member")
	orgA := s.createOrg(t, alice, "Org A")
	orgB := s.createOrg(t, alice, "Org B")
	s.addMember(t, alice, orgA, member, "editor")
	s.addMember(t, alice, orgB, member, "editor")

	// A page that lives in org B, shared (non-private) so org B members can
	// read it via the ordinary permission model.
	page := decodeOAuth[map[string]interface{}](t, s.doJSON(t, "POST", "/api/v1/pages", reqOpts{
		userID:  &alice.ID,
		jsonVal: map[string]interface{}{"title": "Org B Page", "type": "page"},
	}))
	pageID := page["id"].(string)
	w := s.doJSON(t, "PATCH", "/api/v1/pages/"+pageID, reqOpts{
		userID:  &alice.ID,
		jsonVal: map[string]interface{}{"title": "Org B Page", "type": "page", "orgId": orgB, "isPrivate": false},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// A client scoped only to org A requests a token acting as member (who
	// is also in org B). The token's org grant is org A only.
	client := s.createClient(t, alice, orgA, []string{"page:read"}, nil, nil)
	form := url.Values{"grant_type": {"client_credentials"}, "subject": {member.ID.String()}, "org_id": {orgA}}
	w = s.doForm(t, "POST", "/oauth/token", form, client.ClientID, client.ClientSecret)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	tok := decodeOAuth[map[string]interface{}](t, w)
	accessToken := tok["access_token"].(string)

	w = s.doJSON(t, "GET", "/api/v1/pages/"+pageID+"/content", reqOpts{bearer: accessToken})
	assert.Equal(t, http.StatusForbidden, w.Code,
		"a token scoped to org A must not read content of a page in org B, even though the acting user can read the page itself: %s", w.Body.String())
}
