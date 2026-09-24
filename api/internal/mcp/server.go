// Package mcp implements Glyph's remote Model Context Protocol server: a
// stateless Streamable HTTP endpoint at /mcp that lets AI agents read and
// write a user's pages, tasks, lanes, and templates.
//
// Every tool call is executed by dispatching an ordinary request to the
// /api/v1 router with the caller's own bearer token (see apiClient), so tools
// get exactly the permission, OAuth scope, validation, and conflict checks the
// REST API applies — this package never touches a store directly for
// user-owned data.
package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
)

// Protocol versions this server can speak, newest first. Glyph uses only the
// parts of the protocol that are unchanged across these revisions: tools, and
// plain JSON responses to POSTed requests (no SSE, no server-initiated
// messages, no sessions).
var supportedProtocolVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

const (
	serverName = "glyph"
	// maxRequestBytes bounds a single POST body. Page content is the largest
	// thing a client sends; the API itself caps content at 5 MB.
	maxRequestBytes = 6 << 20

	serverInstructions = `Glyph is a notes app with a built-in task board. Notes ("pages") live in a tree of folders; ` +
		`bullets under a page's TODO heading are linked to tasks. Page content is read and written as Markdown. ` +
		`Use search or list_pages to find notes, get_page to read one (it also returns tasks linked to it), and ` +
		`write_page_content to change it — prefer mode "append" unless you mean to rewrite the whole note. ` +
		`Use create_task with page_id to add a task that also appears as a bullet in that note. ` +
		`list_workspaces shows which workspaces (personal and/or orgs) this connection can reach.`
)

// Server is the MCP endpoint. API is the gin engine serving /api/v1, used to
// execute tool calls; Orgs resolves workspace names for list_workspaces.
type Server struct {
	API     http.Handler
	Orgs    store.OrgStore
	Version string
	// PublicURL is the app's public origin, used to give agents links to the
	// pages and tasks they touch.
	PublicURL string
}

func (s *Server) appURL(path string) string {
	return strings.TrimRight(s.PublicURL, "/") + path
}

// ─── JSON-RPC 2.0 ───────────────────────────────────────────────────────────

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// isNotification reports whether the message expects no response: a
// notification (no id) or a client's response to a server request (no method).
func (r *rpcRequest) isNotification() bool {
	return len(r.ID) == 0 || string(r.ID) == "null" || r.Method == ""
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

func errorResponse(id json.RawMessage, code int, msg string) rpcResponse {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	return rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}}
}

// ─── HTTP transport ─────────────────────────────────────────────────────────

// Handle serves POST /mcp. Authentication has already run (see
// RequireBearer); the request carries a *model.TokenScope.
//
// The server is stateless: it never issues an Mcp-Session-Id and answers
// every request with a single application/json body, which the Streamable
// HTTP transport permits in place of an SSE stream. That also keeps every
// response comfortably inside the API's HTTP write timeout.
func (s *Server) Handle(c *gin.Context) {
	if v := c.GetHeader("Mcp-Protocol-Version"); v != "" && !isSupportedVersion(v) {
		c.JSON(http.StatusBadRequest, errorResponse(nil, codeInvalidRequest, "unsupported MCP-Protocol-Version: "+v))
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxRequestBytes+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(nil, codeParseError, "could not read request body"))
		return
	}
	if len(body) > maxRequestBytes {
		c.JSON(http.StatusRequestEntityTooLarge, errorResponse(nil, codeInvalidRequest, "request body too large"))
		return
	}
	body = bytes.TrimSpace(body)

	call := &callContext{
		api:   &apiClient{handler: s.API, authorization: c.GetHeader("Authorization"), ctx: c.Request.Context()},
		scope: tokenScope(c),
		srv:   s,
	}

	// Batches were part of protocol revision 2025-03-26 and dropped after it;
	// accepting them costs nothing and keeps older clients working.
	if len(body) > 0 && body[0] == '[' {
		var batch []json.RawMessage
		if err := json.Unmarshal(body, &batch); err != nil || len(batch) == 0 {
			c.JSON(http.StatusBadRequest, errorResponse(nil, codeParseError, "invalid JSON-RPC batch"))
			return
		}
		responses := make([]rpcResponse, 0, len(batch))
		for _, raw := range batch {
			if resp, ok := call.dispatchRaw(raw); ok {
				responses = append(responses, resp)
			}
		}
		if len(responses) == 0 {
			c.Status(http.StatusAccepted)
			return
		}
		c.JSON(http.StatusOK, responses)
		return
	}

	resp, ok := call.dispatchRaw(body)
	if !ok {
		c.Status(http.StatusAccepted)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// MethodNotAllowed answers GET and DELETE /mcp. A stateless server offers no
// standalone SSE stream (GET) and has no session to terminate (DELETE); 405
// is the transport's documented response for both.
func MethodNotAllowed(c *gin.Context) {
	c.Header("Allow", "POST")
	c.JSON(http.StatusMethodNotAllowed, errorResponse(nil, codeInvalidRequest, "method not allowed: this MCP server is stateless and only accepts POST"))
}

func isSupportedVersion(v string) bool {
	for _, s := range supportedProtocolVersions {
		if s == v {
			return true
		}
	}
	return false
}

func tokenScope(c *gin.Context) *model.TokenScope {
	v, _ := c.Get(model.TokenScopeContextKey)
	ts, _ := v.(*model.TokenScope)
	return ts
}

// RequireBearer authenticates /mcp with an OAuth access token only — never a
// session cookie, so a page on another origin can't drive a logged-in user's
// browser into making tool calls. A missing or invalid token gets a 401 whose
// WWW-Authenticate header points at the protected resource metadata, which
// is how an MCP client discovers where to send the user to authorize.
//
// Origin validation (which the transport spec recommends against DNS
// rebinding) is intentionally not applied: with no ambient credential, a
// rebound page has nothing to present, and browser-based MCP clients from
// arbitrary origins are legitimate.
//
// It returns a middleware chain rather than one handler because bearer calls
// c.Next() itself: the challenge is set before it (so its own 401 for an
// invalid or expired token carries the header) and cleared after it
// succeeds, before the MCP handler writes a response.
func RequireBearer(bearer gin.HandlerFunc, resourceMetadataURL string) []gin.HandlerFunc {
	challenge := `Bearer resource_metadata="` + resourceMetadataURL + `"`
	pre := func(c *gin.Context) {
		if !strings.HasPrefix(c.GetHeader("Authorization"), "Bearer ") {
			c.Header("WWW-Authenticate", challenge)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authorization required"})
			return
		}
		c.Header("WWW-Authenticate", challenge+`, error="invalid_token"`)
	}
	clear := func(c *gin.Context) {
		c.Writer.Header().Del("WWW-Authenticate")
	}
	return []gin.HandlerFunc{pre, bearer, clear}
}

// ─── Method dispatch ────────────────────────────────────────────────────────

// callContext carries per-HTTP-request state through method dispatch.
type callContext struct {
	api   *apiClient
	scope *model.TokenScope
	srv   *Server
}

// dispatchRaw handles one JSON-RPC message; ok is false when no response is due.
func (cc *callContext) dispatchRaw(raw json.RawMessage) (rpcResponse, bool) {
	var req rpcRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return errorResponse(nil, codeParseError, "invalid JSON"), true
	}
	if req.isNotification() {
		// notifications/initialized, notifications/cancelled, … need no reply.
		return rpcResponse{}, false
	}
	if req.JSONRPC != "2.0" {
		return errorResponse(req.ID, codeInvalidRequest, `jsonrpc must be "2.0"`), true
	}
	result, rerr := cc.dispatch(req.Method, req.Params)
	if rerr != nil {
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: rerr}, true
	}
	return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}, true
}

func (cc *callContext) dispatch(method string, params json.RawMessage) (interface{}, *rpcError) {
	switch method {
	case "initialize":
		return cc.initialize(params)
	case "ping":
		return struct{}{}, nil
	case "tools/list":
		return gin.H{"tools": cc.visibleTools()}, nil
	case "tools/call":
		return cc.callTool(params)
	default:
		return nil, &rpcError{Code: codeMethodNotFound, Message: "method not found: " + method}
	}
}

func (cc *callContext) initialize(params json.RawMessage) (interface{}, *rpcError) {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &rpcError{Code: codeInvalidParams, Message: "invalid initialize params"}
		}
	}
	// Echo the client's version when we support it; otherwise offer our
	// newest and let the client decide whether it can proceed.
	version := supportedProtocolVersions[0]
	if isSupportedVersion(p.ProtocolVersion) {
		version = p.ProtocolVersion
	}
	return gin.H{
		"protocolVersion": version,
		"capabilities": gin.H{
			"tools": gin.H{"listChanged": false},
		},
		"serverInfo": gin.H{
			"name":    serverName,
			"title":   "Glyph",
			"version": cc.srv.Version,
		},
		"instructions": serverInstructions,
	}, nil
}

func (cc *callContext) callTool(params json.RawMessage) (interface{}, *rpcError) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.Name == "" {
		return nil, &rpcError{Code: codeInvalidParams, Message: "tools/call requires a tool name"}
	}
	t, ok := toolsByName[p.Name]
	if !ok {
		return nil, &rpcError{Code: codeInvalidParams, Message: "unknown tool: " + p.Name}
	}
	if missing := t.missingScopes(cc.scope); len(missing) > 0 {
		return toolError("This connection wasn't granted the " + strings.Join(missing, ", ") +
			" permission needed for " + p.Name + ". Reconnect the app and approve that permission to use it."), nil
	}
	args := p.Arguments
	if len(args) == 0 || string(args) == "null" {
		args = json.RawMessage("{}")
	}
	out, err := t.run(cc, args)
	if err != nil {
		var ae *apiError
		var ue userError
		switch {
		case errors.As(err, &ae):
			return toolError(ae.Error()), nil
		case errors.As(err, &ue):
			return toolError(ue.Error()), nil
		default:
			slog.Error("mcp tool failed", "tool", p.Name, "error", err)
			return toolError("internal error running " + p.Name), nil
		}
	}
	return toolResult(out), nil
}

// visibleTools lists only the tools this token's scopes allow, so an agent
// isn't offered actions that would be refused.
func (cc *callContext) visibleTools() []toolDescriptor {
	out := make([]toolDescriptor, 0, len(tools))
	for _, t := range tools {
		if len(t.missingScopes(cc.scope)) == 0 {
			out = append(out, t.descriptor())
		}
	}
	return out
}

// ─── Tool results ───────────────────────────────────────────────────────────

func toolResult(v interface{}) gin.H {
	var text string
	switch x := v.(type) {
	case string:
		text = x
	default:
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return toolError("failed to encode result")
		}
		text = string(b)
	}
	return gin.H{"content": []gin.H{{"type": "text", "text": text}}, "isError": false}
}

func toolError(msg string) gin.H {
	return gin.H{"content": []gin.H{{"type": "text", "text": msg}}, "isError": true}
}

// userError is a tool-argument problem reported back to the agent verbatim.
type userError string

func (e userError) Error() string { return string(e) }
