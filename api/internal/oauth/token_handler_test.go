package oauth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store/memstore"
	"github.com/google/uuid"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func newTestConfig(t *testing.T) (Config, uuid.UUID /* orgID */, *model.User /* member */) {
	t.Helper()
	users, _, _, _, _, orgs, _ := memstore.NewStores()
	clients, codes, tokens := memstore.NewOAuthStores(users)

	member, err := users.Upsert(t.Context(), "sub-member", "issuer", strPtr("member@test.com"), strPtr("Member"))
	if err != nil {
		t.Fatal(err)
	}
	org, err := orgs.Create(t.Context(), &model.Organization{Name: "Test Org", CreatedBy: member.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := orgs.AddMember(t.Context(), org.ID, member.ID, model.OrgRoleOwner); err != nil {
		t.Fatal(err)
	}

	return Config{
		Clients:       clients,
		Codes:         codes,
		Tokens:        tokens,
		Users:         users,
		Orgs:          orgs,
		ConsentSecret: []byte("test-consent-secret"),
	}, org.ID, member
}

func strPtr(s string) *string { return &s }

func newFormRequest(form url.Values) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return c, w
}

func createTestClient(t *testing.T, cfg Config, orgID uuid.UUID, grantTypes []model.OAuthGrantType, scopes []model.OAuthScope, redirectURIs []string, confidential bool) (*model.OAuthClient, string) {
	t.Helper()
	clientID, secret, hash, err := GenerateClientCredentials()
	if err != nil {
		t.Fatal(err)
	}
	client := &model.OAuthClient{
		ClientID:       clientID,
		Name:           "Test Client",
		CreatedByID:    uuid.New(),
		GrantTypes:     grantTypes,
		Scopes:         scopes,
		RedirectURIs:   redirectURIs,
		IsConfidential: confidential,
	}
	created, err := cfg.Clients.Create(t.Context(), client, hash)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Clients.AddOrg(t.Context(), created.ID, orgID, uuid.New()); err != nil {
		t.Fatal(err)
	}
	return created, secret
}

// ─── client_credentials ─────────────────────────────────────────────────────────

func TestClientCredentials_Success(t *testing.T) {
	cfg, orgID, member := newTestConfig(t)
	client, secret := createTestClient(t, cfg, orgID, []model.OAuthGrantType{model.GrantClientCredentials}, []model.OAuthScope{model.ScopePageRead, model.ScopePageWrite}, nil, true)

	form := url.Values{
		"grant_type": {"client_credentials"},
		"subject":    {*member.Email},
		"org_id":     {orgID.String()},
	}
	c, w := newFormRequest(form)
	c.Request.SetBasicAuth(client.ClientID, secret)

	TokenHandler(cfg)(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "access_token") {
		t.Errorf("expected access_token in response: %s", w.Body.String())
	}
}

func TestClientCredentials_SubjectNotOrgMemberRejected(t *testing.T) {
	cfg, orgID, _ := newTestConfig(t)
	client, secret := createTestClient(t, cfg, orgID, []model.OAuthGrantType{model.GrantClientCredentials}, []model.OAuthScope{model.ScopePageRead}, nil, true)

	nonMember, err := cfg.Users.Upsert(t.Context(), "sub-outsider", "issuer", strPtr("outsider@test.com"), strPtr("Outsider"))
	if err != nil {
		t.Fatal(err)
	}

	form := url.Values{
		"grant_type": {"client_credentials"},
		"subject":    {*nonMember.Email},
		"org_id":     {orgID.String()},
	}
	c, w := newFormRequest(form)
	c.Request.SetBasicAuth(client.ClientID, secret)

	TokenHandler(cfg)(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-member subject, got %d: %s", w.Code, w.Body.String())
	}
}

func TestClientCredentials_WrongSecretRejected(t *testing.T) {
	cfg, orgID, member := newTestConfig(t)
	client, _ := createTestClient(t, cfg, orgID, []model.OAuthGrantType{model.GrantClientCredentials}, nil, nil, true)

	form := url.Values{
		"grant_type": {"client_credentials"},
		"subject":    {*member.Email},
		"org_id":     {orgID.String()},
	}
	c, w := newFormRequest(form)
	c.Request.SetBasicAuth(client.ClientID, "wrong-secret")

	TokenHandler(cfg)(c)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong secret, got %d", w.Code)
	}
}

func TestClientCredentials_ClientNotScopedToOrgRejected(t *testing.T) {
	cfg, _, member := newTestConfig(t)
	otherOrgID := uuid.New()
	// Client created but never scoped to otherOrgID.
	clientID, secret, hash, _ := GenerateClientCredentials()
	client := &model.OAuthClient{
		ClientID:       clientID,
		Name:           "Unscoped Client",
		CreatedByID:    uuid.New(),
		GrantTypes:     []model.OAuthGrantType{model.GrantClientCredentials},
		IsConfidential: true,
	}
	if _, err := cfg.Clients.Create(t.Context(), client, hash); err != nil {
		t.Fatal(err)
	}

	form := url.Values{
		"grant_type": {"client_credentials"},
		"subject":    {*member.Email},
		"org_id":     {otherOrgID.String()},
	}
	c, w := newFormRequest(form)
	c.Request.SetBasicAuth(clientID, secret)

	TokenHandler(cfg)(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for client not scoped to org, got %d: %s", w.Code, w.Body.String())
	}
}

func TestClientCredentials_RevokedClientRejected(t *testing.T) {
	cfg, orgID, member := newTestConfig(t)
	client, secret := createTestClient(t, cfg, orgID, []model.OAuthGrantType{model.GrantClientCredentials}, nil, nil, true)
	if err := cfg.Clients.Revoke(t.Context(), client.ID); err != nil {
		t.Fatal(err)
	}

	form := url.Values{
		"grant_type": {"client_credentials"},
		"subject":    {*member.Email},
		"org_id":     {orgID.String()},
	}
	c, w := newFormRequest(form)
	c.Request.SetBasicAuth(client.ClientID, secret)

	TokenHandler(cfg)(c)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for revoked client, got %d", w.Code)
	}
}

// ─── authorization_code + PKCE ──────────────────────────────────────────────────

func TestAuthorizationCode_Success(t *testing.T) {
	cfg, orgID, member := newTestConfig(t)
	client, secret := createTestClient(t, cfg, orgID, []model.OAuthGrantType{model.GrantAuthorizationCode}, []model.OAuthScope{model.ScopePageRead}, []string{"https://agent.example.com/callback"}, true)

	verifier := "this-is-a-sufficiently-long-pkce-verifier-1234567890"
	challenge := pkceChallenge(verifier)

	code, codeHash, err := GenerateAuthCode()
	if err != nil {
		t.Fatal(err)
	}
	authCode := &model.OAuthAuthorizationCode{
		ClientID:            client.ID,
		UserID:              member.ID,
		RedirectURI:         "https://agent.example.com/callback",
		Scopes:              []model.OAuthScope{model.ScopePageRead},
		OrgIDs:              []uuid.UUID{orgID},
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
		ExpiresAt:           timeNowPlus(t, authCodeTTL),
	}
	if err := cfg.Codes.Create(t.Context(), authCode, codeHash); err != nil {
		t.Fatal(err)
	}

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {"https://agent.example.com/callback"},
		"client_id":     {client.ClientID},
		"client_secret": {secret},
		"code_verifier": {verifier},
	}
	c, w := newFormRequest(form)
	TokenHandler(cfg)(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "refresh_token") {
		t.Errorf("expected refresh_token in response: %s", w.Body.String())
	}
}

func TestAuthorizationCode_PKCEMismatchRejected(t *testing.T) {
	cfg, orgID, member := newTestConfig(t)
	client, secret := createTestClient(t, cfg, orgID, []model.OAuthGrantType{model.GrantAuthorizationCode}, nil, []string{"https://agent.example.com/callback"}, true)

	correctVerifier := "this-is-a-sufficiently-long-pkce-verifier-1234567890"
	challenge := pkceChallenge(correctVerifier)

	code, codeHash, _ := GenerateAuthCode()
	authCode := &model.OAuthAuthorizationCode{
		ClientID: client.ID, UserID: member.ID,
		RedirectURI:   "https://agent.example.com/callback",
		CodeChallenge: challenge, CodeChallengeMethod: "S256",
		ExpiresAt: timeNowPlus(t, authCodeTTL),
	}
	cfg.Codes.Create(t.Context(), authCode, codeHash) //nolint:errcheck

	form := url.Values{
		"grant_type": {"authorization_code"}, "code": {code},
		"redirect_uri": {"https://agent.example.com/callback"},
		"client_id":    {client.ClientID}, "client_secret": {secret},
		"code_verifier": {"wrong-verifier-that-does-not-match-the-challenge-1234"},
	}
	c, w := newFormRequest(form)
	TokenHandler(cfg)(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for PKCE mismatch, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAuthorizationCode_ExpiredCodeRejected(t *testing.T) {
	cfg, orgID, member := newTestConfig(t)
	client, secret := createTestClient(t, cfg, orgID, []model.OAuthGrantType{model.GrantAuthorizationCode}, nil, []string{"https://agent.example.com/callback"}, true)

	verifier := "this-is-a-sufficiently-long-pkce-verifier-1234567890"
	code, codeHash, _ := GenerateAuthCode()
	authCode := &model.OAuthAuthorizationCode{
		ClientID: client.ID, UserID: member.ID,
		RedirectURI:         "https://agent.example.com/callback",
		CodeChallenge:       pkceChallenge(verifier),
		CodeChallengeMethod: "S256",
		ExpiresAt:           timeNowMinus(t, authCodeTTL), // already expired
	}
	cfg.Codes.Create(t.Context(), authCode, codeHash) //nolint:errcheck

	form := url.Values{
		"grant_type": {"authorization_code"}, "code": {code},
		"redirect_uri": {"https://agent.example.com/callback"},
		"client_id":    {client.ClientID}, "client_secret": {secret},
		"code_verifier": {verifier},
	}
	c, w := newFormRequest(form)
	TokenHandler(cfg)(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for expired code, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAuthorizationCode_ReusedCodeRejected(t *testing.T) {
	cfg, orgID, member := newTestConfig(t)
	client, secret := createTestClient(t, cfg, orgID, []model.OAuthGrantType{model.GrantAuthorizationCode}, nil, []string{"https://agent.example.com/callback"}, true)

	verifier := "this-is-a-sufficiently-long-pkce-verifier-1234567890"
	code, codeHash, _ := GenerateAuthCode()
	authCode := &model.OAuthAuthorizationCode{
		ClientID: client.ID, UserID: member.ID,
		RedirectURI:         "https://agent.example.com/callback",
		CodeChallenge:       pkceChallenge(verifier),
		CodeChallengeMethod: "S256",
		ExpiresAt:           timeNowPlus(t, authCodeTTL),
	}
	cfg.Codes.Create(t.Context(), authCode, codeHash) //nolint:errcheck

	form := url.Values{
		"grant_type": {"authorization_code"}, "code": {code},
		"redirect_uri": {"https://agent.example.com/callback"},
		"client_id":    {client.ClientID}, "client_secret": {secret},
		"code_verifier": {verifier},
	}
	c1, w1 := newFormRequest(form)
	TokenHandler(cfg)(c1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first use should succeed, got %d: %s", w1.Code, w1.Body.String())
	}

	c2, w2 := newFormRequest(form)
	TokenHandler(cfg)(c2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("reused code should be rejected, got %d: %s", w2.Code, w2.Body.String())
	}
}

// ─── refresh_token rotation ─────────────────────────────────────────────────────

// TestRefreshToken_RequiresClientAuthentication guards against a refresh
// token being redeemable with no client credentials at all (client_id
// omitted entirely used to skip client authentication outright).
func TestRefreshToken_RequiresClientAuthentication(t *testing.T) {
	cfg, orgID, member := newTestConfig(t)
	client, secret := createTestClient(t, cfg, orgID, []model.OAuthGrantType{model.GrantAuthorizationCode}, nil, []string{"https://agent.example.com/callback"}, true)

	refreshToken, refreshHash, err := GenerateRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	accessToken, accessHash, err := GenerateAccessToken()
	if err != nil {
		t.Fatal(err)
	}
	_ = accessToken
	refreshExp := timeNowPlus(t, refreshTokenTTL)
	tok := &model.OAuthToken{
		ClientID: client.ID, ActingUserID: member.ID,
		GrantType:             model.GrantAuthorizationCode,
		AccessTokenExpiresAt:  timeNowPlus(t, accessTokenTTL),
		RefreshTokenExpiresAt: &refreshExp,
	}
	if err := cfg.Tokens.Create(t.Context(), tok, accessHash, &refreshHash); err != nil {
		t.Fatal(err)
	}

	// No client_id, no client_secret at all.
	c, w := newFormRequest(url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	})
	TokenHandler(cfg)(c)
	if w.Code == http.StatusOK {
		t.Fatalf("refresh redeemed with zero client authentication, want rejection: %d %s", w.Code, w.Body.String())
	}

	// Right client, right secret should still work.
	c2, w2 := newFormRequest(url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {client.ClientID},
		"client_secret": {secret},
	})
	TokenHandler(cfg)(c2)
	if w2.Code != http.StatusOK {
		t.Fatalf("legitimate refresh with correct client credentials should succeed, got %d: %s", w2.Code, w2.Body.String())
	}
}

// TestRefreshToken_BoundToIssuingClient guards against a refresh token
// issued to one client being redeemable by a different, unrelated client
// that merely authenticates with its own valid credentials.
func TestRefreshToken_BoundToIssuingClient(t *testing.T) {
	cfg, orgID, member := newTestConfig(t)
	clientA, _ := createTestClient(t, cfg, orgID, []model.OAuthGrantType{model.GrantAuthorizationCode}, []model.OAuthScope{model.ScopePageWrite}, []string{"https://a.example/cb"}, true)
	clientB, secretB := createTestClient(t, cfg, orgID, []model.OAuthGrantType{model.GrantAuthorizationCode}, []model.OAuthScope{model.ScopePageRead}, []string{"https://b.example/cb"}, true)

	refreshToken, refreshHash, err := GenerateRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	_, accessHash, err := GenerateAccessToken()
	if err != nil {
		t.Fatal(err)
	}
	refreshExp := timeNowPlus(t, refreshTokenTTL)
	tok := &model.OAuthToken{
		ClientID: clientA.ID, ActingUserID: member.ID,
		GrantType:             model.GrantAuthorizationCode,
		Scopes:                []model.OAuthScope{model.ScopePageWrite},
		AccessTokenExpiresAt:  timeNowPlus(t, accessTokenTTL),
		RefreshTokenExpiresAt: &refreshExp,
	}
	if err := cfg.Tokens.Create(t.Context(), tok, accessHash, &refreshHash); err != nil {
		t.Fatal(err)
	}

	// Client B authenticates with its OWN valid credentials but presents
	// client A's refresh token.
	c, w := newFormRequest(url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {clientB.ClientID},
		"client_secret": {secretB},
	})
	TokenHandler(cfg)(c)
	if w.Code == http.StatusOK {
		t.Fatalf("client B redeemed client A's refresh token, want rejection: %d %s", w.Code, w.Body.String())
	}
}

// TestRefreshToken_ExpiredRejected guards against RotateRefresh ignoring
// refresh_token_expires_at and renewing a token indefinitely past its TTL.
func TestRefreshToken_ExpiredRejected(t *testing.T) {
	cfg, orgID, member := newTestConfig(t)
	client, secret := createTestClient(t, cfg, orgID, []model.OAuthGrantType{model.GrantAuthorizationCode}, nil, []string{"https://agent.example.com/callback"}, true)

	refreshToken, refreshHash, err := GenerateRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	_, accessHash, err := GenerateAccessToken()
	if err != nil {
		t.Fatal(err)
	}
	expiredRefresh := timeNowMinus(t, refreshTokenTTL+time.Hour) // well past its TTL
	tok := &model.OAuthToken{
		ClientID: client.ID, ActingUserID: member.ID,
		GrantType:             model.GrantAuthorizationCode,
		AccessTokenExpiresAt:  timeNowPlus(t, accessTokenTTL),
		RefreshTokenExpiresAt: &expiredRefresh,
	}
	if err := cfg.Tokens.Create(t.Context(), tok, accessHash, &refreshHash); err != nil {
		t.Fatal(err)
	}

	c, w := newFormRequest(url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {client.ClientID},
		"client_secret": {secret},
	})
	TokenHandler(cfg)(c)
	if w.Code == http.StatusOK {
		t.Fatalf("expired refresh token was renewed, want rejection: %d %s", w.Code, w.Body.String())
	}
}

func TestRefreshToken_ReuseOfRotatedTokenRejected(t *testing.T) {
	cfg, orgID, member := newTestConfig(t)
	client, secret := createTestClient(t, cfg, orgID, []model.OAuthGrantType{model.GrantAuthorizationCode}, nil, []string{"https://agent.example.com/callback"}, true)

	verifier := "this-is-a-sufficiently-long-pkce-verifier-1234567890"
	code, codeHash, _ := GenerateAuthCode()
	authCode := &model.OAuthAuthorizationCode{
		ClientID: client.ID, UserID: member.ID,
		RedirectURI:         "https://agent.example.com/callback",
		CodeChallenge:       pkceChallenge(verifier),
		CodeChallengeMethod: "S256",
		ExpiresAt:           timeNowPlus(t, authCodeTTL),
	}
	cfg.Codes.Create(t.Context(), authCode, codeHash) //nolint:errcheck

	form := url.Values{
		"grant_type": {"authorization_code"}, "code": {code},
		"redirect_uri": {"https://agent.example.com/callback"},
		"client_id":    {client.ClientID}, "client_secret": {secret},
		"code_verifier": {verifier},
	}
	c, w := newFormRequest(form)
	TokenHandler(cfg)(c)
	if w.Code != http.StatusOK {
		t.Fatalf("initial exchange failed: %d %s", w.Code, w.Body.String())
	}
	refreshToken := extractJSONField(t, w.Body.String(), "refresh_token")

	// First rotation should succeed.
	rform := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}, "client_id": {client.ClientID}, "client_secret": {secret}}
	c1, w1 := newFormRequest(rform)
	TokenHandler(cfg)(c1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first rotation should succeed, got %d: %s", w1.Code, w1.Body.String())
	}

	// Reusing the OLD (already-rotated) refresh token must fail.
	c2, w2 := newFormRequest(rform)
	TokenHandler(cfg)(c2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("reused refresh token should be rejected, got %d: %s", w2.Code, w2.Body.String())
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func pkceChallenge(verifier string) string {
	return b64URLSHA256(verifier)
}

func extractJSONField(t *testing.T, body, field string) string {
	t.Helper()
	// crude but sufficient for these tests' flat JSON responses
	marker := `"` + field + `":"`
	idx := strings.Index(body, marker)
	if idx == -1 {
		t.Fatalf("field %q not found in body: %s", field, body)
	}
	rest := body[idx+len(marker):]
	end := strings.Index(rest, `"`)
	return rest[:end]
}
