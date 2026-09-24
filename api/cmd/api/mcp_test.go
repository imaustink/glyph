package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/auth"
	"github.com/glyph/api/internal/handler"
	"github.com/glyph/api/internal/model"
	glyphoauth "github.com/glyph/api/internal/oauth"
	"github.com/glyph/api/internal/store/memstore"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mcpEnv is the production route table (registerRoutes + registerMCP + the
// OAuth protocol and consent routes) over in-memory stores. Session auth is
// faked with an X-Test-User-ID header, as in the integration harness; bearer
// auth, CSRF, scopes, and the MCP endpoint are the real thing.
type mcpEnv struct {
	t      *testing.T
	router *gin.Engine
	s      *stores
	alice  *model.User
	bob    *model.User
	orgID  uuid.UUID
}

const testPublicURL = "https://glyph.test"

func newMCPEnv(t *testing.T) *mcpEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	handler.RegisterValidators()

	users, pages, tasks, lanes, templates, orgs, shares := memstore.NewStores()
	clients, codes, tokens := memstore.NewOAuthStores(users)
	s := &stores{
		users: users, pages: pages, tasks: tasks, lanes: lanes, templates: templates,
		orgs: orgs, shares: shares, oauthClients: clients, oauthCodes: codes, oauthTokens: tokens,
	}

	sessionMw := func(c *gin.Context) {
		id, err := uuid.Parse(c.GetHeader("X-Test-User-ID"))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "no session"})
			return
		}
		u, err := users.GetByID(c.Request.Context(), id)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "no session"})
			return
		}
		c.Set(auth.ContextKey, u)
		c.Next()
	}
	bearerMw := glyphoauth.BearerTokenMiddleware(tokens, users)

	r := gin.New()
	oauthCfg := glyphoauth.Config{
		Clients: clients, Codes: codes, Tokens: tokens, Users: users, Orgs: orgs,
		ConsentSecret: []byte("mcp-test-consent-secret-0123456789abcdef"),
		IssuerURL:     testPublicURL,
	}
	glyphoauth.RegisterOAuthRoutes(r, oauthCfg, nil)
	apiGroup := r.Group("/api/v1", glyphoauth.DualAuthMiddleware(sessionMw, bearerMw), handler.CSRFMiddleware())
	glyphoauth.RegisterConsentRoutes(apiGroup, oauthCfg)
	registerRoutes(apiGroup, newHandlers(s))
	registerMCP(r, s, oauthCfg, bearerMw)

	ctx := context.Background()
	ae, an := "alice@test.com", "Alice"
	be, bn := "bob@test.com", "Bob"
	alice, err := users.Upsert(ctx, "alice", "test", &ae, &an)
	require.NoError(t, err)
	bob, err := users.Upsert(ctx, "bob", "test", &be, &bn)
	require.NoError(t, err)

	env := &mcpEnv{t: t, router: r, s: s, alice: alice, bob: bob}
	org := env.session(alice, "POST", "/api/v1/orgs", map[string]interface{}{"name": "Acme"}, http.StatusCreated)
	env.orgID = uuid.MustParse(org["id"].(string))
	env.session(alice, "POST", "/api/v1/orgs/"+env.orgID.String()+"/members",
		map[string]interface{}{"userId": bob.ID.String(), "role": "editor"}, http.StatusCreated)
	return env
}

func (e *mcpEnv) do(req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	return w
}

// session makes a cookie-session-style request as u and returns the decoded
// JSON object body, asserting the status.
func (e *mcpEnv) session(u *model.User, method, path string, body interface{}, want int) map[string]interface{} {
	e.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(e.t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User-ID", u.ID.String())
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	w := e.do(req)
	require.Equal(e.t, want, w.Code, "%s %s: %s", method, path, w.Body.String())
	out := map[string]interface{}{}
	if w.Body.Len() > 0 && strings.HasPrefix(strings.TrimSpace(w.Body.String()), "{") {
		require.NoError(e.t, json.Unmarshal(w.Body.Bytes(), &out))
	}
	return out
}

const testRedirect = "http://127.0.0.1:33418/callback"

type grant struct {
	access, refresh, clientID string
}

// connect runs the full flow an MCP client performs: dynamic registration,
// consent (as user, choosing workspaces), and the PKCE code exchange.
func (e *mcpEnv) connect(u *model.User, personal bool, orgIDs []uuid.UUID, scope string) grant {
	e.t.Helper()
	reg := e.postJSON("/oauth/register", map[string]interface{}{
		"client_name":   "Test Agent",
		"redirect_uris": []string{testRedirect},
	})
	require.Equal(e.t, http.StatusCreated, reg.Code, reg.Body.String())
	var regBody map[string]interface{}
	require.NoError(e.t, json.Unmarshal(reg.Body.Bytes(), &regBody))
	clientID := regBody["client_id"].(string)
	assert.Equal(e.t, "none", regBody["token_endpoint_auth_method"])
	assert.Nil(e.t, regBody["client_secret"], "public clients get no secret")

	verifier := "verifier-" + strings.Repeat("x", 50)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	q := url.Values{
		"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {testRedirect},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"}, "state": {"st8"},
		"resource": {testPublicURL + "/mcp"},
	}
	if scope != "" {
		q.Set("scope", scope)
	}
	info := e.session(u, "GET", "/api/v1/oauth/consent?"+q.Encode(), nil, http.StatusOK)
	require.Equal(e.t, true, info["workspaceSelection"])
	client := info["client"].(map[string]interface{})
	assert.Equal(e.t, true, client["dynamic"])
	assert.Equal(e.t, "127.0.0.1:33418", client["redirectHost"])

	orgStrs := make([]string, len(orgIDs))
	for i, id := range orgIDs {
		orgStrs[i] = id.String()
	}
	dec := e.session(u, "POST", "/api/v1/oauth/consent/decision", map[string]interface{}{
		"consentToken": info["consentToken"], "approve": true, "personal": personal, "orgIds": orgStrs,
	}, http.StatusOK)
	redirect, err := url.Parse(dec["redirectUrl"].(string))
	require.NoError(e.t, err)
	assert.Equal(e.t, "st8", redirect.Query().Get("state"))

	tok := e.form("/oauth/token", url.Values{
		"grant_type": {"authorization_code"}, "code": {redirect.Query().Get("code")},
		"redirect_uri": {testRedirect}, "code_verifier": {verifier}, "client_id": {clientID},
		"resource": {testPublicURL + "/mcp"},
	})
	require.Equal(e.t, http.StatusOK, tok.Code, tok.Body.String())
	var tokBody map[string]interface{}
	require.NoError(e.t, json.Unmarshal(tok.Body.Bytes(), &tokBody))
	return grant{access: tokBody["access_token"].(string), refresh: tokBody["refresh_token"].(string), clientID: clientID}
}

func (e *mcpEnv) postJSON(path string, body interface{}) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	return e.do(req)
}

func (e *mcpEnv) form(path string, v url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", path, strings.NewReader(v.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return e.do(req)
}

// rpcRaw POSTs a JSON-RPC payload to /mcp.
func (e *mcpEnv) rpcRaw(token string, payload interface{}) *httptest.ResponseRecorder {
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return e.do(req)
}

type rpcResp struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (e *mcpEnv) rpc(token, method string, params interface{}) rpcResp {
	e.t.Helper()
	w := e.rpcRaw(token, map[string]interface{}{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	require.Equal(e.t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(e.t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))
	var r rpcResp
	require.NoError(e.t, json.Unmarshal(w.Body.Bytes(), &r))
	return r
}

// call invokes a tool and returns its text output and isError flag.
func (e *mcpEnv) call(token, name string, args interface{}) (string, bool) {
	e.t.Helper()
	r := e.rpc(token, "tools/call", map[string]interface{}{"name": name, "arguments": args})
	require.Nil(e.t, r.Error, "tools/call %s: %+v", name, r.Error)
	var res struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	require.NoError(e.t, json.Unmarshal(r.Result, &res))
	require.Len(e.t, res.Content, 1)
	return res.Content[0].Text, res.IsError
}

// callOK invokes a tool that must succeed and decodes its JSON output.
func (e *mcpEnv) callOK(token, name string, args interface{}) map[string]interface{} {
	e.t.Helper()
	text, isErr := e.call(token, name, args)
	require.False(e.t, isErr, "%s failed: %s", name, text)
	out := map[string]interface{}{}
	require.NoError(e.t, json.Unmarshal([]byte(text), &out), text)
	return out
}

func toolNames(t *testing.T, r rpcResp) []string {
	var res struct {
		Tools []struct {
			Name        string                 `json:"name"`
			InputSchema map[string]interface{} `json:"inputSchema"`
		} `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(r.Result, &res))
	names := make([]string, len(res.Tools))
	for i, tl := range res.Tools {
		names[i] = tl.Name
		assert.Equal(t, "object", tl.InputSchema["type"], tl.Name)
	}
	return names
}

// ─── Discovery & transport ──────────────────────────────────────────────────

func TestMCPDiscoveryAndAuthChallenge(t *testing.T) {
	e := newMCPEnv(t)

	w := e.do(httptest.NewRequest("GET", "/.well-known/oauth-authorization-server", nil))
	require.Equal(t, http.StatusOK, w.Code)
	var as map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &as))
	assert.Equal(t, testPublicURL, as["issuer"])
	assert.Equal(t, testPublicURL+"/oauth/authorize", as["authorization_endpoint"])
	assert.Equal(t, testPublicURL+"/oauth/token", as["token_endpoint"])
	assert.Equal(t, testPublicURL+"/oauth/register", as["registration_endpoint"])
	assert.Contains(t, as["code_challenge_methods_supported"], "S256")
	assert.Contains(t, as["scopes_supported"], "lane:read")
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))

	for _, path := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp"} {
		w = e.do(httptest.NewRequest("GET", path, nil))
		require.Equal(t, http.StatusOK, w.Code, path)
		var pr map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &pr))
		assert.Equal(t, testPublicURL+"/mcp", pr["resource"])
		assert.Equal(t, []interface{}{testPublicURL}, pr["authorization_servers"])
	}

	// No token: 401 pointing at the resource metadata.
	w = e.rpcRaw("", map[string]interface{}{"jsonrpc": "2.0", "id": 1, "method": "initialize"})
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, `Bearer resource_metadata="`+testPublicURL+`/.well-known/oauth-protected-resource/mcp"`, w.Header().Get("WWW-Authenticate"))

	// Bad token: same challenge, flagged invalid_token.
	w = e.rpcRaw("not-a-real-token", map[string]interface{}{"jsonrpc": "2.0", "id": 1, "method": "initialize"})
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Header().Get("WWW-Authenticate"), `error="invalid_token"`)

	// A session cookie alone never authenticates /mcp.
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	req.Header.Set("X-Test-User-ID", e.alice.ID.String())
	assert.Equal(t, http.StatusUnauthorized, e.do(req).Code)

	// Stateless: no SSE stream, no session to delete.
	assert.Equal(t, http.StatusMethodNotAllowed, e.do(httptest.NewRequest("GET", "/mcp", nil)).Code)
	assert.Equal(t, http.StatusMethodNotAllowed, e.do(httptest.NewRequest("DELETE", "/mcp", nil)).Code)

	// CORS preflight for browser-based clients.
	pre := httptest.NewRequest("OPTIONS", "/mcp", nil)
	pre.Header.Set("Origin", "http://localhost:6274")
	w = e.do(pre)
	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Contains(t, w.Header().Get("Access-Control-Allow-Headers"), "Authorization")
}

func TestMCPProtocol(t *testing.T) {
	e := newMCPEnv(t)
	g := e.connect(e.alice, true, nil, "")

	r := e.rpc(g.access, "initialize", map[string]interface{}{
		"protocolVersion": "2025-06-18", "capabilities": map[string]interface{}{},
		"clientInfo": map[string]interface{}{"name": "test", "version": "1"},
	})
	require.Nil(t, r.Error)
	var init struct {
		ProtocolVersion string                 `json:"protocolVersion"`
		Capabilities    map[string]interface{} `json:"capabilities"`
		ServerInfo      map[string]interface{} `json:"serverInfo"`
		Instructions    string                 `json:"instructions"`
	}
	require.NoError(t, json.Unmarshal(r.Result, &init))
	assert.Equal(t, "2025-06-18", init.ProtocolVersion)
	assert.Contains(t, init.Capabilities, "tools")
	assert.Equal(t, "glyph", init.ServerInfo["name"])
	assert.NotEmpty(t, init.Instructions)

	// An unknown version gets our newest instead.
	r = e.rpc(g.access, "initialize", map[string]interface{}{"protocolVersion": "1999-01-01"})
	require.NoError(t, json.Unmarshal(r.Result, &init))
	assert.Equal(t, "2025-11-25", init.ProtocolVersion)

	// Notifications are accepted with no body.
	w := e.rpcRaw(g.access, map[string]interface{}{"jsonrpc": "2.0", "method": "notifications/initialized"})
	assert.Equal(t, http.StatusAccepted, w.Code)
	assert.Zero(t, w.Body.Len())

	r = e.rpc(g.access, "ping", nil)
	assert.Nil(t, r.Error)
	assert.JSONEq(t, `{}`, string(r.Result))

	r = e.rpc(g.access, "resources/list", nil)
	require.NotNil(t, r.Error)
	assert.Equal(t, -32601, r.Error.Code)

	r = e.rpc(g.access, "tools/call", map[string]interface{}{"name": "no_such_tool"})
	require.NotNil(t, r.Error)
	assert.Equal(t, -32602, r.Error.Code)

	// Batches (2025-03-26) still work.
	w = e.rpcRaw(g.access, []interface{}{
		map[string]interface{}{"jsonrpc": "2.0", "id": "a", "method": "ping"},
		map[string]interface{}{"jsonrpc": "2.0", "method": "notifications/initialized"},
		map[string]interface{}{"jsonrpc": "2.0", "id": "b", "method": "ping"},
	})
	require.Equal(t, http.StatusOK, w.Code)
	var batch []map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &batch))
	assert.Len(t, batch, 2)

	// Unsupported protocol version header is refused.
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	req.Header.Set("Authorization", "Bearer "+g.access)
	req.Header.Set("Mcp-Protocol-Version", "1999-01-01")
	assert.Equal(t, http.StatusBadRequest, e.do(req).Code)

	// Malformed JSON.
	req = httptest.NewRequest("POST", "/mcp", strings.NewReader(`{nope`))
	req.Header.Set("Authorization", "Bearer "+g.access)
	w = e.do(req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "-32700")

	names := toolNames(t, e.rpc(g.access, "tools/list", nil))
	assert.ElementsMatch(t, []string{
		"list_workspaces", "search",
		"list_pages", "get_page", "create_page", "update_page", "write_page_content",
		"list_tasks", "get_task", "create_task", "update_task", "delete_task",
		"list_lanes", "get_lane_tasks",
		"list_templates", "create_page_from_template",
	}, names)
}

// ─── Registration ───────────────────────────────────────────────────────────

func TestDynamicClientRegistrationValidation(t *testing.T) {
	e := newMCPEnv(t)
	cases := []struct {
		name string
		body map[string]interface{}
		want string
	}{
		{"no redirect", map[string]interface{}{"client_name": "x"}, "invalid_redirect_uri"},
		{"http non-loopback", map[string]interface{}{"redirect_uris": []string{"http://evil.example/cb"}}, "invalid_redirect_uri"},
		{"javascript scheme", map[string]interface{}{"redirect_uris": []string{"javascript:alert(1)"}}, "invalid_redirect_uri"},
		{"fragment", map[string]interface{}{"redirect_uris": []string{"https://ok.example/cb#frag"}}, "invalid_redirect_uri"},
		{"client_credentials", map[string]interface{}{"redirect_uris": []string{"https://ok.example/cb"}, "grant_types": []string{"client_credentials"}}, "invalid_client_metadata"},
		{"implicit", map[string]interface{}{"redirect_uris": []string{"https://ok.example/cb"}, "response_types": []string{"token"}}, "invalid_client_metadata"},
		{"bad auth method", map[string]interface{}{"redirect_uris": []string{"https://ok.example/cb"}, "token_endpoint_auth_method": "private_key_jwt"}, "invalid_client_metadata"},
		{"no known scopes", map[string]interface{}{"redirect_uris": []string{"https://ok.example/cb"}, "scope": "admin:everything"}, "invalid_client_metadata"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := e.postJSON("/oauth/register", tc.body)
			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), tc.want)
		})
	}

	t.Run("accepted redirect forms", func(t *testing.T) {
		for _, u := range []string{"https://claude.ai/api/mcp/auth_callback", "http://localhost:8080/cb", "http://[::1]:9/cb", "cursor://anysphere.cursor-retrieval/oauth/callback"} {
			w := e.postJSON("/oauth/register", map[string]interface{}{"redirect_uris": []string{u}})
			assert.Equal(t, http.StatusCreated, w.Code, "%s: %s", u, w.Body.String())
		}
	})

	t.Run("confidential client gets a secret", func(t *testing.T) {
		w := e.postJSON("/oauth/register", map[string]interface{}{
			"redirect_uris": []string{"https://ok.example/cb"}, "token_endpoint_auth_method": "client_secret_post",
			"client_name": "  Spoofy\x07 App  ", "scope": "page:read task:read bogus",
		})
		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		var body map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.NotEmpty(t, body["client_secret"])
		assert.Equal(t, "Spoofy App", body["client_name"])
		assert.Equal(t, "page:read task:read", body["scope"])
	})

	t.Run("dynamic client can never use client_credentials", func(t *testing.T) {
		w := e.postJSON("/oauth/register", map[string]interface{}{
			"redirect_uris": []string{"https://ok.example/cb"}, "token_endpoint_auth_method": "client_secret_basic",
		})
		require.Equal(t, http.StatusCreated, w.Code)
		var body map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(url.Values{
			"grant_type": {"client_credentials"}, "subject": {e.alice.ID.String()}, "org_id": {e.orgID.String()},
		}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.SetBasicAuth(body["client_id"].(string), body["client_secret"].(string))
		w = e.do(req)
		assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "unauthorized_client")
	})
}

func TestConsentWorkspaceSelection(t *testing.T) {
	e := newMCPEnv(t)
	reg := e.postJSON("/oauth/register", map[string]interface{}{"redirect_uris": []string{testRedirect}})
	var regBody map[string]interface{}
	require.NoError(t, json.Unmarshal(reg.Body.Bytes(), &regBody))
	sum := sha256.Sum256([]byte("v" + strings.Repeat("y", 60)))
	q := url.Values{
		"response_type": {"code"}, "client_id": {regBody["client_id"].(string)}, "redirect_uri": {testRedirect},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"},
	}
	consent := func(u *model.User) string {
		info := e.session(u, "GET", "/api/v1/oauth/consent?"+q.Encode(), nil, http.StatusOK)
		return info["consentToken"].(string)
	}

	info := e.session(e.bob, "GET", "/api/v1/oauth/consent?"+q.Encode(), nil, http.StatusOK)
	ws := info["workspaces"].([]interface{})
	require.Len(t, ws, 2)
	assert.Equal(t, "personal", ws[0].(map[string]interface{})["id"])
	assert.Equal(t, e.orgID.String(), ws[1].(map[string]interface{})["id"])
	assert.Nil(t, info["orgName"])

	// Nothing selected.
	e.session(e.bob, "POST", "/api/v1/oauth/consent/decision",
		map[string]interface{}{"consentToken": consent(e.bob), "approve": true}, http.StatusBadRequest)

	// An org the user isn't in.
	other := e.session(e.alice, "POST", "/api/v1/orgs", map[string]interface{}{"name": "Alice Only"}, http.StatusCreated)
	e.session(e.bob, "POST", "/api/v1/oauth/consent/decision", map[string]interface{}{
		"consentToken": consent(e.bob), "approve": true, "orgIds": []string{other["id"].(string)},
	}, http.StatusForbidden)

	// Deny still redirects with access_denied.
	dec := e.session(e.bob, "POST", "/api/v1/oauth/consent/decision",
		map[string]interface{}{"consentToken": consent(e.bob), "approve": false}, http.StatusOK)
	assert.Contains(t, dec["redirectUrl"], "error=access_denied")
}

// ─── Tools end to end ───────────────────────────────────────────────────────

func TestMCPToolsEndToEnd(t *testing.T) {
	e := newMCPEnv(t)
	g := e.connect(e.alice, true, []uuid.UUID{e.orgID}, "")

	ws := e.rpc(g.access, "tools/call", map[string]interface{}{"name": "list_workspaces", "arguments": map[string]interface{}{}})
	assert.Contains(t, string(ws.Result), "Personal workspace")
	assert.Contains(t, string(ws.Result), "Acme")

	// Several workspaces: creating without one defaults to personal.
	folder := e.callOK(g.access, "create_page", map[string]interface{}{"title": "Projects", "type": "folder"})
	folderID := folder["page"].(map[string]interface{})["id"].(string)
	created := e.callOK(g.access, "create_page", map[string]interface{}{
		"title":     "Launch plan",
		"parent_id": folderID,
		"markdown":  "# Launch\n\nShip the **desktop** app with *auto-updates*.\n\n## TODO\n\n- Write release notes\n",
		"tags":      []string{"launch"},
	})
	page := created["page"].(map[string]interface{})
	pageID := page["id"].(string)
	assert.Equal(t, "Projects / Launch plan", page["path"])
	assert.Equal(t, "personal", page["workspace"])
	assert.Equal(t, testPublicURL+"/notes/"+pageID, page["url"])

	// A bullet written under TODO becomes a linked task, as when typed in the app.
	createdTasks := created["tasks_created"].([]interface{})
	require.Len(t, createdTasks, 1)
	notesTask := createdTasks[0].(map[string]interface{})
	assert.Equal(t, "Write release notes", notesTask["title"])
	assert.Equal(t, pageID, notesTask["sourcePageId"])

	got := e.callOK(g.access, "get_page", map[string]interface{}{"page_id": pageID})
	md := got["markdown"].(string)
	assert.Contains(t, md, "Ship the **desktop** app with *auto-updates*.")
	assert.Contains(t, md, "- [ ] Write release notes <!-- task:"+notesTask["id"].(string)+" -->")
	require.Len(t, got["tasks"], 1)
	rev := int(got["revision"].(float64))

	// Append leaves existing content alone.
	appended := e.callOK(g.access, "write_page_content", map[string]interface{}{
		"page_id": pageID, "markdown": "Owner: Alice", "expected_revision": rev,
	})
	assert.Greater(t, int(appended["revision"].(float64)), rev)
	got = e.callOK(g.access, "get_page", map[string]interface{}{"page_id": pageID})
	assert.True(t, strings.HasSuffix(strings.TrimSpace(got["markdown"].(string)), "Owner: Alice"))

	// A stale revision is refused rather than clobbering.
	text, isErr := e.call(g.access, "write_page_content", map[string]interface{}{
		"page_id": pageID, "markdown": "late", "expected_revision": rev,
	})
	assert.True(t, isErr)
	assert.Contains(t, text, "changed since you read it")

	// A task linked to the page lands under its TODO heading.
	task := e.callOK(g.access, "create_task", map[string]interface{}{
		"title": "Record demo video", "page_id": pageID, "priority": "high", "due_date": "2026-10-01",
	})
	taskView := task["task"].(map[string]interface{})
	taskID := taskView["id"].(string)
	assert.Equal(t, pageID, taskView["sourcePageId"])
	assert.Equal(t, "personal", taskView["workspace"])
	got = e.callOK(g.access, "get_page", map[string]interface{}{"page_id": pageID})
	md = got["markdown"].(string)
	assert.Contains(t, md, "- [ ] Record demo video <!-- task:"+taskID+" -->")
	assert.Less(t, strings.Index(md, "## TODO"), strings.Index(md, "Record demo video"))
	require.Len(t, got["tasks"], 2)

	// Replace keeps the task link when its marker is kept.
	rev = int(got["revision"].(float64))
	e.callOK(g.access, "write_page_content", map[string]interface{}{
		"page_id": pageID, "mode": "replace", "expected_revision": rev,
		"markdown": "# Launch v2\n\n## TODO\n\n- [ ] Record demo video <!-- task:" + taskID + " -->\n",
	})
	got = e.callOK(g.access, "get_page", map[string]interface{}{"page_id": pageID})
	assert.Contains(t, got["markdown"], "<!-- task:"+taskID+" -->")
	assert.NotContains(t, got["markdown"], "Owner: Alice")

	// Appending a bullet to the TODO list links it too; linked bullets aren't duplicated.
	app := e.callOK(g.access, "write_page_content", map[string]interface{}{"page_id": pageID, "markdown": "- Ship to beta"})
	require.Len(t, app["tasks_created"], 1)
	assert.Equal(t, "Ship to beta", app["tasks_created"].([]interface{})[0].(map[string]interface{})["title"])
	app = e.callOK(g.access, "write_page_content", map[string]interface{}{"page_id": pageID, "markdown": "Plain paragraph."})
	assert.Nil(t, app["tasks_created"])

	// A standalone task in the org.
	orgTask := e.callOK(g.access, "create_task", map[string]interface{}{
		"title": "Renew domain", "workspace": e.orgID.String(), "tags": []string{"ops"},
	})
	assert.Equal(t, e.orgID.String(), orgTask["task"].(map[string]interface{})["workspace"])

	// Write release notes, Record demo video, Ship to beta, Renew domain.
	list := e.callOK(g.access, "list_tasks", map[string]interface{}{})
	assert.EqualValues(t, 4, list["total"])
	first := list["tasks"].([]interface{})[0].(map[string]interface{})
	assert.Equal(t, "Record demo video", first["title"], "dated tasks sort first")

	list = e.callOK(g.access, "list_tasks", map[string]interface{}{"tags": []string{"OPS"}})
	assert.EqualValues(t, 1, list["total"])
	list = e.callOK(g.access, "list_tasks", map[string]interface{}{"folder_id": folderID})
	assert.EqualValues(t, 3, list["total"])
	list = e.callOK(g.access, "list_tasks", map[string]interface{}{"due_before": "2026-09-30"})
	assert.EqualValues(t, 0, list["total"])

	// Update, then clear the due date with null.
	upd := e.callOK(g.access, "update_task", map[string]interface{}{"task_id": taskID, "status": "in-progress"})
	assert.Equal(t, "in-progress", upd["status"])
	assert.Equal(t, "2026-10-01", upd["dueDate"])
	upd = e.callOK(g.access, "update_task", map[string]interface{}{"task_id": taskID, "due_date": nil})
	assert.Nil(t, upd["dueDate"])
	_, isErr = e.call(g.access, "update_task", map[string]interface{}{"task_id": taskID, "status": "blocked"})
	assert.True(t, isErr)

	// Search hits content and task titles.
	res := e.callOK(g.access, "search", map[string]interface{}{"query": "launch v2"})
	assert.Contains(t, mustJSON(t, res), pageID)
	res = e.callOK(g.access, "search", map[string]interface{}{"query": "renew", "types": []string{"task"}})
	assert.Len(t, res["results"], 1)

	// Lanes: seed the default board via the app, then read through MCP.
	e.session(e.alice, "POST", "/api/v1/lanes", map[string]interface{}{
		"title": "In Progress", "order": 0,
		"filterSet": map[string]interface{}{"conjunction": "and", "rules": []interface{}{
			map[string]interface{}{"id": "r1", "field": "status", "operator": "eq", "value": "in-progress"},
		}},
	}, http.StatusCreated)
	lanes := e.callOK(g.access, "list_lanes", map[string]interface{}{})
	laneList := lanes["lanes"].([]interface{})
	require.Len(t, laneList, 1)
	laneID := laneList[0].(map[string]interface{})["id"].(string)
	laneTasks := e.callOK(g.access, "get_lane_tasks", map[string]interface{}{"lane_id": laneID})
	assert.EqualValues(t, 1, laneTasks["total"])

	// Templates.
	e.session(e.alice, "POST", "/api/v1/templates", map[string]interface{}{
		"name":          "Daily",
		"titleTemplate": "Daily {{date}}",
		"content":       `{"type":"doc","content":[{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"TODO"}]},{"type":"bulletList","content":[{"type":"listItem","attrs":{"nodeId":"n1","taskId":"stale"},"content":[{"type":"paragraph","content":[{"type":"text","text":"Plan {{day}}"}]}]}]}]}`,
	}, http.StatusCreated)
	tmpls := e.callOK(g.access, "list_templates", map[string]interface{}{})
	tmplList := tmpls["templates"].([]interface{})
	require.Len(t, tmplList, 1)
	tmpl := tmplList[0].(map[string]interface{})
	assert.Contains(t, tmpl["markdown"], "Plan {{day}}")
	fromTmpl := e.callOK(g.access, "create_page_from_template", map[string]interface{}{
		"template_id": tmpl["id"], "timezone": "America/New_York",
	})
	daily := fromTmpl["page"].(map[string]interface{})
	assert.Regexp(t, `^Daily \d{4}-\d{2}-\d{2}$`, daily["title"])
	got = e.callOK(g.access, "get_page", map[string]interface{}{"page_id": daily["id"]})
	assert.NotContains(t, got["markdown"], "{{day}}")
	assert.NotContains(t, got["markdown"], "task:stale", "template task links must not be copied")

	// Page metadata edits and moves.
	moved := e.callOK(g.access, "update_page", map[string]interface{}{"page_id": pageID, "title": "Launch (moved)", "parent_id": ""})
	assert.Nil(t, moved["page"].(map[string]interface{})["parentId"])
	pages := e.callOK(g.access, "list_pages", map[string]interface{}{"parent_id": folderID})
	assert.EqualValues(t, 0, pages["total"])

	e.callOK(g.access, "delete_task", map[string]interface{}{"task_id": orgTask["task"].(map[string]interface{})["id"]})
	_, isErr = e.call(g.access, "get_task", map[string]interface{}{"task_id": orgTask["task"].(map[string]interface{})["id"]})
	assert.True(t, isErr)
}

func TestMCPWorkspaceIsolation(t *testing.T) {
	e := newMCPEnv(t)
	personalTok := e.connect(e.alice, true, nil, "").access
	orgTok := e.connect(e.alice, false, []uuid.UUID{e.orgID}, "").access

	mine := e.callOK(personalTok, "create_page", map[string]interface{}{"title": "Diary", "markdown": "secret"})
	mineID := mine["page"].(map[string]interface{})["id"].(string)
	orgPage := e.callOK(orgTok, "create_page", map[string]interface{}{"title": "Roadmap"})
	orgPageView := orgPage["page"].(map[string]interface{})
	assert.Equal(t, e.orgID.String(), orgPageView["workspace"], "single-org token defaults to its org")

	// The org-only token can't see or touch the personal page…
	text, isErr := e.call(orgTok, "get_page", map[string]interface{}{"page_id": mineID})
	assert.True(t, isErr, text)
	pages := e.callOK(orgTok, "list_pages", map[string]interface{}{})
	assert.NotContains(t, mustJSON(t, pages), mineID)
	res := e.callOK(orgTok, "search", map[string]interface{}{"query": "secret"})
	assert.Empty(t, res["results"])
	_, isErr = e.call(orgTok, "create_page", map[string]interface{}{"title": "x", "workspace": "personal"})
	assert.True(t, isErr)

	// …and the personal-only token can't reach the org.
	_, isErr = e.call(personalTok, "get_page", map[string]interface{}{"page_id": orgPageView["id"]})
	assert.True(t, isErr)
	_, isErr = e.call(personalTok, "create_task", map[string]interface{}{"title": "x", "workspace": e.orgID.String()})
	assert.True(t, isErr)

	// Personal lanes need a personal grant.
	_, isErr = e.call(orgTok, "list_lanes", map[string]interface{}{})
	assert.True(t, isErr)

	// Another user's token never sees Alice's personal data.
	bobTok := e.connect(e.bob, true, []uuid.UUID{e.orgID}, "").access
	_, isErr = e.call(bobTok, "get_page", map[string]interface{}{"page_id": mineID})
	assert.True(t, isErr)
}

// TestGetLaneWorkspaceScope proves GET /lanes/:id resolves the lane's workspace
// before the bearer-token scope check: a personal-only token with lane:read can
// read a personal lane but is refused an org folder-board lane, which lives in
// an org the token was never granted.
func TestGetLaneWorkspaceScope(t *testing.T) {
	e := newMCPEnv(t)

	// A personal lane (no folder scope).
	personalLane := e.session(e.alice, "POST", "/api/v1/lanes", map[string]interface{}{
		"title": "Personal", "order": 0,
	}, http.StatusCreated)
	personalLaneID := personalLane["id"].(string)

	// An org folder-board lane: an org folder page, then a lane inside it.
	folder := e.session(e.alice, "POST", "/api/v1/pages", map[string]interface{}{
		"title": "Org Board", "type": "folder", "orgId": e.orgID.String(),
	}, http.StatusCreated)
	folderID := folder["id"].(string)
	orgLane := e.session(e.alice, "POST", "/api/v1/folders/"+folderID+"/lanes", map[string]interface{}{
		"title": "Org Lane", "order": 0,
	}, http.StatusCreated)
	orgLaneID := orgLane["id"].(string)

	// A personal-only token carrying lane:read.
	tok := e.connect(e.alice, true, nil, "lane:read").access

	get := func(id string) int {
		req := httptest.NewRequest("GET", "/api/v1/lanes/"+id, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		return e.do(req).Code
	}
	assert.Equal(t, http.StatusOK, get(personalLaneID), "personal lane is within the personal grant")
	assert.Equal(t, http.StatusForbidden, get(orgLaneID), "org folder-board lane is outside a personal-only grant")
}

func TestMCPScopesLimitTools(t *testing.T) {
	e := newMCPEnv(t)
	tok := e.connect(e.alice, true, nil, "page:read").access

	names := toolNames(t, e.rpc(tok, "tools/list", nil))
	assert.ElementsMatch(t, []string{"list_workspaces", "search", "list_pages", "get_page"}, names)

	text, isErr := e.call(tok, "create_task", map[string]interface{}{"title": "nope"})
	assert.True(t, isErr)
	assert.Contains(t, text, "task:write")
	_, isErr = e.call(tok, "write_page_content", map[string]interface{}{"page_id": uuid.NewString(), "markdown": "x"})
	assert.True(t, isErr)

	// Write implies read.
	w := e.connect(e.alice, true, nil, "task:write").access
	names = toolNames(t, e.rpc(w, "tools/list", nil))
	assert.Contains(t, names, "list_tasks")
	assert.Contains(t, names, "create_task")
	assert.NotContains(t, names, "get_page")
	text, isErr = e.call(w, "create_task", map[string]interface{}{"title": "t", "page_id": uuid.NewString()})
	assert.True(t, isErr)
	assert.Contains(t, text, "page:write")

	// Without task:write, TODO bullets are saved but not turned into tasks,
	// and the agent is told so.
	pw := e.connect(e.alice, true, nil, "page:write").access
	out := e.callOK(pw, "create_page", map[string]interface{}{"title": "No tasks", "markdown": "## TODO\n\n- one\n- two\n"})
	assert.Nil(t, out["tasks_created"])
	assert.Contains(t, out["note"], "2 bullet(s)")
}

func TestConnectedAppsAndRefresh(t *testing.T) {
	e := newMCPEnv(t)
	g := e.connect(e.alice, true, []uuid.UUID{e.orgID}, "page:read task:read")
	e.callOK(g.access, "list_pages", map[string]interface{}{})

	// BearerTokenMiddleware records last-used in a background goroutine, so
	// poll the connections list until it reports lastUsedAt rather than racing
	// the async update.
	var c map[string]interface{}
	require.Eventually(t, func() bool {
		req := httptest.NewRequest("GET", "/api/v1/oauth/connections", nil)
		req.Header.Set("X-Test-User-ID", e.alice.ID.String())
		w := e.do(req)
		if w.Code != http.StatusOK {
			return false
		}
		var conns []map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &conns); err != nil || len(conns) != 1 {
			return false
		}
		if conns[0]["lastUsedAt"] == nil {
			return false
		}
		c = conns[0]
		return true
	}, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, "Test Agent", c["clientName"])
	assert.Equal(t, true, c["dynamic"])
	assert.Equal(t, true, c["personal"])
	assert.Equal(t, "Acme", c["orgs"].([]interface{})[0].(map[string]interface{})["name"])
	assert.NotNil(t, c["lastUsedAt"])

	// Connections are session-only.
	req := httptest.NewRequest("GET", "/api/v1/oauth/connections", nil)
	req.Header.Set("Authorization", "Bearer "+g.access)
	assert.Equal(t, http.StatusForbidden, e.do(req).Code)

	// Refresh keeps the personal grant.
	tok := e.form("/oauth/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {g.refresh}, "client_id": {g.clientID}})
	require.Equal(t, http.StatusOK, tok.Code, tok.Body.String())
	var refreshed map[string]interface{}
	require.NoError(t, json.Unmarshal(tok.Body.Bytes(), &refreshed))
	newAccess := refreshed["access_token"].(string)
	assert.Contains(t, mustJSON(t, e.callOK(newAccess, "list_pages", map[string]interface{}{})), "total")
	text, _ := e.call(newAccess, "list_workspaces", map[string]interface{}{})
	assert.Contains(t, text, "personal")

	// Bob can't revoke Alice's grant; Alice can.
	e.session(e.bob, "DELETE", "/api/v1/oauth/connections/"+c["id"].(string), nil, http.StatusNotFound)
	e.session(e.alice, "DELETE", "/api/v1/oauth/connections/"+c["id"].(string), nil, http.StatusNoContent)
	w := e.rpcRaw(newAccess, map[string]interface{}{"jsonrpc": "2.0", "id": 1, "method": "ping"})
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestSetupDevAuthMountsMCP guards the production wiring: every auth mode
// must mount the discovery documents, registration, and /mcp — not just the
// router the tests above assemble by hand.
func TestSetupDevAuthMountsMCP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("FRONTEND_URL", "https://dev.glyph.test/")
	users, pages, tasks, lanes, templates, orgs, shares := memstore.NewStores()
	clients, codes, tokens := memstore.NewOAuthStores(users)
	s := &stores{
		users: users, pages: pages, tasks: tasks, lanes: lanes, templates: templates,
		orgs: orgs, shares: shares, oauthClients: clients, oauthCodes: codes, oauthTokens: tokens,
	}
	r := gin.New()
	setupDevAuth(context.Background(), r, nil, s, nil)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/.well-known/oauth-authorization-server", nil))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"issuer":"https://dev.glyph.test"`)

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/.well-known/oauth-protected-resource/mcp", nil))
	assert.Equal(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/mcp", strings.NewReader(`{}`)))
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Header().Get("WWW-Authenticate"), "https://dev.glyph.test/.well-known/oauth-protected-resource/mcp")

	w = httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/oauth/register", strings.NewReader(`{"redirect_uris":["http://localhost:1/cb"]}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code, w.Body.String())
}

func mustJSON(t *testing.T, v interface{}) string {
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}
