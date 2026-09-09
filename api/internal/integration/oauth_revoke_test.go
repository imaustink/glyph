package integration

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOAuthRevoke regression-tests POST /oauth/revoke, which used to panic
// on every call: it read the acting user via auth.CurrentUser (which panics
// when no user was ever set in context) despite running with no
// unconditional session middleware attached — so gin.Recovery() turned every
// revocation attempt, including the RFC 7009 "a client revokes its own
// token" case, into a 500 with the token left active.
func TestOAuthRevoke(t *testing.T) {
	s := setupOAuthServer(t)
	s.reset(t)

	alice := s.createUser(t, "sub-alice-revoke", "alice-revoke@test.com", "Alice")
	member := s.createUser(t, "sub-member-revoke", "member-revoke@test.com", "Member")
	orgID := s.createOrg(t, alice, "Revoke Org")
	s.addMember(t, alice, orgID, member, "editor")
	client := s.createClient(t, alice, orgID, []string{"page:read"}, nil, nil)

	issueToken := func(t *testing.T) string {
		t.Helper()
		form := url.Values{"grant_type": {"client_credentials"}, "subject": {member.ID.String()}, "org_id": {orgID}}
		w := s.doForm(t, "POST", "/oauth/token", form, client.ClientID, client.ClientSecret)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		return decodeOAuth[map[string]interface{}](t, w)["access_token"].(string)
	}

	t.Run("ClientCanRevokeItsOwnToken", func(t *testing.T) {
		accessToken := issueToken(t)

		// Sanity: the token works before revocation.
		w := s.doJSON(t, "GET", "/api/v1/pages", reqOpts{bearer: accessToken})
		require.Equal(t, http.StatusOK, w.Code)

		form := url.Values{"token": {accessToken}}
		w = s.doForm(t, "POST", "/oauth/revoke", form, client.ClientID, client.ClientSecret)
		require.Equal(t, http.StatusOK, w.Code, "revoke must not panic/500: %s", w.Body.String())

		w = s.doJSON(t, "GET", "/api/v1/pages", reqOpts{bearer: accessToken})
		assert.Equal(t, http.StatusUnauthorized, w.Code, "token should be unusable after revocation")
	})

	t.Run("UserCanRevokeTokenIssuedAsThem", func(t *testing.T) {
		accessToken := issueToken(t)

		// The acting user (member), authenticated via session, revokes a
		// token that was issued to act as them — no client credentials.
		form := url.Values{"token": {accessToken}}
		w := s.doFormAsUser(t, "POST", "/oauth/revoke", form, member.ID.String())
		require.Equal(t, http.StatusOK, w.Code, "revoke must not panic/500: %s", w.Body.String())

		w = s.doJSON(t, "GET", "/api/v1/pages", reqOpts{bearer: accessToken})
		assert.Equal(t, http.StatusUnauthorized, w.Code, "token should be unusable after the acting user revoked it")
	})

	t.Run("UnrelatedUserCannotRevokeSomeoneElsesToken", func(t *testing.T) {
		accessToken := issueToken(t)

		form := url.Values{"token": {accessToken}}
		w := s.doFormAsUser(t, "POST", "/oauth/revoke", form, alice.ID.String())
		require.Equal(t, http.StatusOK, w.Code, "RFC 7009: always 200, even when not authorized to revoke")

		// But the token must still work — alice was not authorized to revoke it.
		w = s.doJSON(t, "GET", "/api/v1/pages", reqOpts{bearer: accessToken})
		assert.Equal(t, http.StatusOK, w.Code, "an unrelated user must not be able to revoke another client's token")
	})

	t.Run("WrongClientCannotRevokeAnothersToken", func(t *testing.T) {
		accessToken := issueToken(t)
		otherClient := s.createClient(t, alice, orgID, []string{"page:read"}, nil, nil)

		form := url.Values{"token": {accessToken}}
		w := s.doForm(t, "POST", "/oauth/revoke", form, otherClient.ClientID, otherClient.ClientSecret)
		require.Equal(t, http.StatusOK, w.Code)

		w = s.doJSON(t, "GET", "/api/v1/pages", reqOpts{bearer: accessToken})
		assert.Equal(t, http.StatusOK, w.Code, "a different client must not be able to revoke this token")
	})

	t.Run("MalformedRequestReturns200WithoutPanicking", func(t *testing.T) {
		// Per RFC 7009, a request that doesn't even identify a token
		// returns 200 to avoid leaking token validity — must not panic.
		w := s.doForm(t, "POST", "/oauth/revoke", url.Values{}, "", "")
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("UnknownTokenReturns200WithoutPanicking", func(t *testing.T) {
		form := url.Values{"token": {"not-a-real-token"}}
		w := s.doForm(t, "POST", "/oauth/revoke", form, "", "")
		assert.Equal(t, http.StatusOK, w.Code)
	})
}
