package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/auth"
	"github.com/glyph/api/internal/model"
	"github.com/google/uuid"
)

// ─── CSRFMiddleware ───────────────────────────────────────────────────────────

func TestCSRFMiddleware_GetAllowed(t *testing.T) {
	r := gin.New()
	r.Use(CSRFMiddleware())
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET should be allowed: got %d", w.Code)
	}
}

func TestCSRFMiddleware_HeadAllowed(t *testing.T) {
	r := gin.New()
	r.Use(CSRFMiddleware())
	r.Handle(http.MethodHead, "/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodHead, "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("HEAD should be allowed: got %d", w.Code)
	}
}

func TestCSRFMiddleware_PostWithHeaderAllowed(t *testing.T) {
	r := gin.New()
	r.Use(CSRFMiddleware())
	r.POST("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("POST with CSRF header should be allowed: got %d", w.Code)
	}
}

func TestCSRFMiddleware_PostWithoutHeaderForbidden(t *testing.T) {
	r := gin.New()
	r.Use(CSRFMiddleware())
	r.POST("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("POST without CSRF header should be 403: got %d", w.Code)
	}
}

func TestCSRFMiddleware_PutWithoutHeaderForbidden(t *testing.T) {
	r := gin.New()
	r.Use(CSRFMiddleware())
	r.PUT("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("PUT without CSRF header should be 403: got %d", w.Code)
	}
}

// ─── RequestIDMiddleware ──────────────────────────────────────────────────────

func TestRequestIDMiddleware_GeneratesID(t *testing.T) {
	r := gin.New()
	r.Use(RequestIDMiddleware())

	var capturedID interface{}
	r.GET("/test", func(c *gin.Context) {
		capturedID, _ = c.Get(RequestIDKey)
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)

	if capturedID == nil || capturedID.(string) == "" {
		t.Error("expected a non-empty request ID in context")
	}
	if w.Header().Get("X-Request-ID") == "" {
		t.Error("expected X-Request-ID response header")
	}
	if capturedID.(string) != w.Header().Get("X-Request-ID") {
		t.Error("context request ID should match response header")
	}
}

func TestRequestIDMiddleware_HonoursIncomingID(t *testing.T) {
	r := gin.New()
	r.Use(RequestIDMiddleware())

	const incomingID = "my-trace-id-1234"
	var capturedID interface{}
	r.GET("/test", func(c *gin.Context) {
		capturedID, _ = c.Get(RequestIDKey)
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Request-ID", incomingID)
	r.ServeHTTP(w, req)

	if capturedID.(string) != incomingID {
		t.Errorf("expected context ID %q, got %q", incomingID, capturedID)
	}
	if w.Header().Get("X-Request-ID") != incomingID {
		t.Errorf("expected response header %q, got %q", incomingID, w.Header().Get("X-Request-ID"))
	}
}

// ─── SecurityHeadersMiddleware ────────────────────────────────────────────────

func TestSecurityHeadersMiddleware_SetsExpectedHeaders(t *testing.T) {
	r := gin.New()
	r.Use(SecurityHeadersMiddleware())
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	headers := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
	}
	for header, expected := range headers {
		got := w.Header().Get(header)
		if got != expected {
			t.Errorf("header %s = %q, want %q", header, got, expected)
		}
	}
	if w.Header().Get("Content-Security-Policy") == "" {
		t.Error("expected Content-Security-Policy header")
	}
}

// ─── RateLimitMiddleware ──────────────────────────────────────────────────────

func TestRateLimitMiddleware_NilUser_Returns401(t *testing.T) {
	rl := NewRateLimiter(10, time.Second)
	defer rl.Stop()

	r := gin.New()
	// Inject a nil *model.User so CurrentUser returns nil without panicking
	r.Use(func(c *gin.Context) {
		c.Set(auth.ContextKey, (*model.User)(nil))
		c.Next()
	})
	r.Use(RateLimitMiddleware(rl))
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for nil user, got %d", w.Code)
	}
}

func TestRateLimitMiddleware_ValidUser_Passes(t *testing.T) {
	rl := NewRateLimiter(10, time.Second)
	defer rl.Stop()

	user := &model.User{ID: uuid.New()}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(auth.ContextKey, user)
		c.Next()
	})
	r.Use(RateLimitMiddleware(rl))
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for valid user under limit, got %d", w.Code)
	}
}

func TestRateLimitMiddleware_RateLimited_Returns429(t *testing.T) {
	rl := NewRateLimiter(1, time.Second) // burst=1
	defer rl.Stop()

	user := &model.User{ID: uuid.New()}
	// Exhaust the single token
	rl.Allow(user.ID.String())

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(auth.ContextKey, user)
		c.Next()
	})
	r.Use(RateLimitMiddleware(rl))
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 when rate limited, got %d", w.Code)
	}
}
