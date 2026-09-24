package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
)

// apiClient executes REST calls in-process against the /api/v1 router,
// authenticated with the MCP caller's own Authorization header. Going
// through the router (rather than calling stores) means a tool can never
// do more than the same token could do over plain HTTP.
type apiClient struct {
	handler       http.Handler
	authorization string
	ctx           context.Context
}

// apiError is a non-2xx API response, surfaced to the agent as a tool error.
type apiError struct {
	Status  int
	Message string
}

func (e *apiError) Error() string {
	switch e.Status {
	case http.StatusNotFound:
		return "Not found (or not shared with this connection): " + e.Message
	case http.StatusForbidden:
		return "Not allowed: " + e.Message
	case http.StatusConflict:
		return "Conflict: " + e.Message
	default:
		return fmt.Sprintf("Glyph API error (%d): %s", e.Status, e.Message)
	}
}

func (a *apiClient) do(method, path string, query url.Values, body, out interface{}) error {
	target := "/api/v1" + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode %s %s: %w", method, path, err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(a.ctx, method, target, reader)
	if err != nil {
		return fmt.Errorf("build %s %s: %w", method, path, err)
	}
	req.Header.Set("Authorization", a.authorization)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	req.RemoteAddr = "127.0.0.1:0"

	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)

	if rec.Code >= 300 {
		var e struct {
			Error       string `json:"error"`
			Description string `json:"error_description"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &e)
		msg := e.Error
		if e.Description != "" {
			msg += ": " + e.Description
		}
		if msg == "" {
			msg = strings.TrimSpace(http.StatusText(rec.Code))
		}
		return &apiError{Status: rec.Code, Message: msg}
	}
	if out != nil && rec.Code != http.StatusNoContent && rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			return fmt.Errorf("decode %s %s: %w", method, path, err)
		}
	}
	return nil
}

func (a *apiClient) get(path string, query url.Values, out interface{}) error {
	return a.do(http.MethodGet, path, query, nil, out)
}

func (a *apiClient) post(path string, body, out interface{}) error {
	return a.do(http.MethodPost, path, nil, body, out)
}

func (a *apiClient) patch(path string, body, out interface{}) error {
	return a.do(http.MethodPatch, path, nil, body, out)
}

func (a *apiClient) put(path string, body, out interface{}) error {
	return a.do(http.MethodPut, path, nil, body, out)
}

func (a *apiClient) delete(path string) error {
	return a.do(http.MethodDelete, path, nil, nil, nil)
}
