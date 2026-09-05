// Package handler provides HTTP handlers for the Glyph REST API.
// Handlers are organized into domain-specific structs:
//   - PageHandler     — Page CRUD + content
//   - TaskHandler     — Task CRUD
//   - LaneHandler     — Lane CRUD
//   - TemplateHandler — Template CRUD
//   - OrgHandler      — Organization + member management
//   - ShareHandler    — Direct sharing + user search
//   - UnfurlURL       — URL unfurling (package-level function)
//
// Cross-cutting write-permission logic lives in PermissionChecker (permissions.go).
package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/store"
	"github.com/google/uuid"
)

// ─── Helpers ──────────────────────────────────────────────────────────────────

func bindJSON(c *gin.Context, v interface{}) bool {
	if err := c.ShouldBindJSON(v); err != nil {
		// Do not echo the raw validator/binding error to the client — it can
		// expose internal struct field names and schema details. Log it server-side
		// and return a generic message.
		slog.Debug("request bind error", "method", c.Request.Method, "path", c.Request.URL.Path, "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return false
	}
	return true
}

// bindJSONWithKeys binds the request body into v (with the same validation as
// bindJSON) and additionally returns the set of top-level JSON keys that were
// present in the raw body.
//
// This lets PATCH handlers distinguish an explicitly-null field (e.g.
// {"parentId": null}, meaning "move to the top level") from an omitted field
// (meaning "leave unchanged"). A pointer-based DTO alone cannot tell these
// apart — both decode to a nil pointer.
func bindJSONWithKeys(c *gin.Context, v interface{}) (map[string]json.RawMessage, bool) {
	raw, err := c.GetRawData()
	if err != nil {
		slog.Debug("request read error", "method", c.Request.Method, "path", c.Request.URL.Path, "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return nil, false
	}
	// Restore the body so ShouldBindJSON (with validation) can read it again.
	c.Request.Body = io.NopCloser(bytes.NewReader(raw))
	if !bindJSON(c, v) {
		return nil, false
	}
	keys := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &keys); err != nil {
			// v already bound successfully, so this should not happen; treat a
			// malformed top-level object as "no keys" rather than failing.
			slog.Debug("request key scan error", "method", c.Request.Method, "path", c.Request.URL.Path, "error", err)
		}
	}
	return keys, true
}

// isJSONNull reports whether a raw JSON value is the literal `null`.
func isJSONNull(raw json.RawMessage) bool {
	return string(bytes.TrimSpace(raw)) == "null"
}

func parseUUID(c *gin.Context, param string) (uuid.UUID, bool) {
	raw := c.Param(param)
	id, err := uuid.Parse(raw)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid UUID: " + raw})
		return uuid.UUID{}, false
	}
	return id, true
}

func internalError(c *gin.Context, err error) {
	reqID, _ := c.Get(RequestIDKey)
	slog.Error("internal server error",
		"request_id", reqID,
		"method", c.Request.Method,
		"path", c.Request.URL.Path,
		"error", err)
	c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error", "request_id": reqID})
}

// notFoundOrError handles store errors by mapping them to appropriate HTTP responses.
// - ErrNotFound → 404
// - ErrForbidden → 403
// - Other errors → 500 (logged, generic message returned)
func notFoundOrError(c *gin.Context, err error) {
	if err == nil {
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if errors.Is(err, store.ErrForbidden) {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		return
	}
	// For any other error, treat as not found to avoid leaking info.
	// The original error is logged server-side.
	reqID, _ := c.Get(RequestIDKey)
	slog.Error("store error (returned as 404)",
		"request_id", reqID,
		"method", c.Request.Method,
		"path", c.Request.URL.Path,
		"error", err)
	c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
}
