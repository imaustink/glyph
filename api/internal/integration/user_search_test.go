package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// putUsersInSharedOrg creates an org owned by Alice and adds Bob as a member,
// so the two share an org — Search is scoped to shared-org members only (a
// user in no org matches nobody, to prevent it being used as a directory
// enumeration tool), so tests that expect a cross-user match need this.
func putUsersInSharedOrg(t *testing.T, h *Harness) {
	t.Helper()
	w := h.Do(t, "POST", "/api/v1/orgs", map[string]interface{}{"name": "Shared Org"}, h.UserA.ID)
	require.Equal(t, http.StatusCreated, w.Code, "create org: %s", w.Body.String())
	org := Decode[map[string]interface{}](t, w)
	orgID, ok := org["id"].(string)
	require.True(t, ok, "org id missing from response: %v", org)

	w = h.Do(t, "POST", "/api/v1/orgs/"+orgID+"/members",
		map[string]interface{}{"userId": h.UserB.ID.String(), "role": "viewer"}, h.UserA.ID)
	require.Equal(t, http.StatusCreated, w.Code, "add member: %s", w.Body.String())
}

func TestUserSearch(t *testing.T) {
	RunSpecs(t, map[string]func(t *testing.T, h *Harness){
		"SearchReturnsMatchingUsers": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			putUsersInSharedOrg(t, h)
			w := h.Do(t, "GET", "/api/v1/users/search?q=bob", nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			results := Decode[[]map[string]interface{}](t, w)
			require.Len(t, results, 1)
			assert.Equal(t, h.UserB.ID.String(), results[0]["id"])
		},
		"SearchExcludesCurrentUser": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			putUsersInSharedOrg(t, h)
			w := h.Do(t, "GET", "/api/v1/users/search?q=alice", nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			results := Decode[[]map[string]interface{}](t, w)
			assert.Empty(t, results)
		},
		"EmptyQueryReturnsEmpty": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "GET", "/api/v1/users/search", nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			results := Decode[[]map[string]interface{}](t, w)
			assert.Empty(t, results)
		},
		"NoSharedOrgReturnsEmpty": func(t *testing.T, h *Harness) {
			// Regression test: Search must never fall back to an unscoped,
			// directory-wide query when the caller belongs to no
			// organization — every account starts out in this state.
			h.ResetDB(t)
			w := h.Do(t, "GET", "/api/v1/users/search?q=bob", nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			results := Decode[[]map[string]interface{}](t, w)
			assert.Empty(t, results, "search must not leak users outside the caller's orgs")
		},
		"SearchByEmail": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			putUsersInSharedOrg(t, h)
			w := h.Do(t, "GET", "/api/v1/users/search?q=bob%40test.com", nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			results := Decode[[]map[string]interface{}](t, w)
			require.Len(t, results, 1)
			assert.Equal(t, h.UserB.ID.String(), results[0]["id"])
		},
		"SearchIsCaseInsensitive": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			putUsersInSharedOrg(t, h)
			w := h.Do(t, "GET", "/api/v1/users/search?q=BOB", nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			results := Decode[[]map[string]interface{}](t, w)
			require.Len(t, results, 1)
			assert.Equal(t, h.UserB.ID.String(), results[0]["id"])
		},
		"WildcardQueryDoesNotMatchEverything": func(t *testing.T, h *Harness) {
			// Regression test: a bare "%" must be treated as a literal
			// character to search for, not a SQL LIKE wildcard that matches
			// every row.
			h.ResetDB(t)
			putUsersInSharedOrg(t, h)
			w := h.Do(t, "GET", "/api/v1/users/search?q=%25", nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			results := Decode[[]map[string]interface{}](t, w)
			assert.Empty(t, results, "a bare %% must not match every user")
		},
	})
}
