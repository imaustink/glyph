package handler

import (
	"errors"
	"net/http"
	"testing"

	"github.com/glyph/api/internal/store"
)

// ─── internalError ────────────────────────────────────────────────────────────

func TestInternalError_Returns500(t *testing.T) {
	c, w := newTestGinContext()
	internalError(c, errors.New("something broke"))

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
	body := w.Body.String()
	if body == "" {
		t.Error("expected non-empty response body")
	}
}

func TestInternalError_IncludesRequestIDWhenSet(t *testing.T) {
	c, w := newTestGinContext()
	c.Set(RequestIDKey, "test-req-id")
	internalError(c, errors.New("boom"))

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
	if !contains(w.Body.String(), "test-req-id") {
		t.Error("expected request_id in response body")
	}
}

// ─── notFoundOrError ──────────────────────────────────────────────────────────

func TestNotFoundOrError_NilErr_NoResponse(t *testing.T) {
	c, w := newTestGinContext()
	notFoundOrError(c, nil)
	// nil error should not write any response
	if w.Code != http.StatusOK {
		t.Errorf("nil error should leave status 200 (unset), got %d", w.Code)
	}
}

func TestNotFoundOrError_ErrNotFound_Returns404(t *testing.T) {
	c, w := newTestGinContext()
	notFoundOrError(c, store.ErrNotFound)

	if w.Code != http.StatusNotFound {
		t.Errorf("ErrNotFound should return 404, got %d", w.Code)
	}
}

func TestNotFoundOrError_ErrForbidden_Returns403(t *testing.T) {
	c, w := newTestGinContext()
	notFoundOrError(c, store.ErrForbidden)

	if w.Code != http.StatusForbidden {
		t.Errorf("ErrForbidden should return 403, got %d", w.Code)
	}
}

func TestNotFoundOrError_OtherError_Returns404(t *testing.T) {
	c, w := newTestGinContext()
	notFoundOrError(c, errors.New("unexpected database error"))

	// Unknown errors are returned as 404 to avoid leaking information
	if w.Code != http.StatusNotFound {
		t.Errorf("unknown error should return 404, got %d", w.Code)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}
