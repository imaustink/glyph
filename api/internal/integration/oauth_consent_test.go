package integration

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOAuthConsentTokenSingleUse regression-tests the fix for a replayable
// consent token: previously, one approval could be POSTed to
// /oauth/consent/decision multiple times within the token's 5-minute TTL,
// each time minting a fresh, usable authorization code.
func TestOAuthConsentTokenSingleUse(t *testing.T) {
	s := setupOAuthServer(t)
	s.reset(t)

	alice := s.createUser(t, "sub-alice-consent", "alice-consent@test.com", "Alice")
	orgID := s.createOrg(t, alice, "Consent Org")
	redirectURI := "https://third-party.example.com/callback"
	client := s.createClient(t, alice, orgID, []string{"page:read"}, []string{redirectURI}, []string{"authorization_code"})

	_, challenge := pkcePair()
	q := url.Values{
		"response_type": {"code"}, "client_id": {client.ClientID}, "redirect_uri": {redirectURI},
		"scope": {"page:read"}, "state": {"s1"}, "code_challenge": {challenge},
		"code_challenge_method": {"S256"}, "org_id": {orgID},
	}
	w := s.doJSON(t, "GET", "/api/v1/oauth/consent?"+q.Encode(), reqOpts{userID: &alice.ID})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	consentToken := decodeOAuth[map[string]interface{}](t, w)["consentToken"].(string)

	decide := func() int {
		w := s.doJSON(t, "POST", "/api/v1/oauth/consent/decision", reqOpts{
			userID:  &alice.ID,
			jsonVal: map[string]interface{}{"consentToken": consentToken, "approve": true},
		})
		return w.Code
	}

	// First use succeeds and yields a usable code.
	require.Equal(t, http.StatusOK, decide())

	// Replaying the exact same consent token must be rejected outright.
	assert.Equal(t, http.StatusBadRequest, decide(),
		"replaying an already-used consent token must be rejected")
}

// TestOAuthConsentRedirectURIRevalidatedAtDecision regression-tests that a
// client's redirect_uri is checked again at decision time, not only at the
// earlier GET /oauth/consent — a client's registered redirect URIs can
// change in the window between the two requests.
func TestOAuthConsentRedirectURIRevalidatedAtDecision(t *testing.T) {
	s := setupOAuthServer(t)
	s.reset(t)

	alice := s.createUser(t, "sub-alice-redirect", "alice-redirect@test.com", "Alice")
	orgID := s.createOrg(t, alice, "Redirect Org")
	redirectURI := "https://third-party.example.com/callback"
	client := s.createClient(t, alice, orgID, []string{"page:read"}, []string{redirectURI}, []string{"authorization_code"})

	_, challenge := pkcePair()
	q := url.Values{
		"response_type": {"code"}, "client_id": {client.ClientID}, "redirect_uri": {redirectURI},
		"scope": {"page:read"}, "state": {"s1"}, "code_challenge": {challenge},
		"code_challenge_method": {"S256"}, "org_id": {orgID},
	}
	w := s.doJSON(t, "GET", "/api/v1/oauth/consent?"+q.Encode(), reqOpts{userID: &alice.ID})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	consentToken := decodeOAuth[map[string]interface{}](t, w)["consentToken"].(string)

	// The client's redirect_uri registration changes before the decision
	// is posted (e.g. an owner edits it, or it's removed).
	updW := s.doJSON(t, "PATCH", "/api/v1/orgs/"+orgID+"/oauth-clients/"+client.ID.String(), reqOpts{
		userID:  &alice.ID,
		jsonVal: map[string]interface{}{"redirectUris": []string{"https://attacker.example.com/callback"}},
	})
	require.Equal(t, http.StatusOK, updW.Code, updW.Body.String())

	w = s.doJSON(t, "POST", "/api/v1/oauth/consent/decision", reqOpts{
		userID:  &alice.ID,
		jsonVal: map[string]interface{}{"consentToken": consentToken, "approve": true},
	})
	assert.Equal(t, http.StatusBadRequest, w.Code,
		"decision must re-validate redirect_uri, not just trust what the earlier GET saw: %s", w.Body.String())
}
