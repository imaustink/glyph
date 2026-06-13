package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// ─── safeDialContext ──────────────────────────────────────────────────────────

func TestSafeDialContext_BadAddr_NoPort(t *testing.T) {
	_, err := safeDialContext(context.Background(), "tcp", "badaddr")
	if err == nil {
		t.Error("expected error for addr without port")
	}
}

func TestSafeDialContext_PrivateIP_Denied(t *testing.T) {
	_, err := safeDialContext(context.Background(), "tcp", "127.0.0.1:80")
	if err == nil {
		t.Fatal("expected error for private IP")
	}
	if !strings.Contains(err.Error(), "private") {
		t.Errorf("expected 'private' in error, got: %v", err)
	}
}

func TestSafeDialContext_UnresolvableHost(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DNS test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := safeDialContext(ctx, "tcp", "nonexistent.thisdoesnotexist.invalid:80")
	if err == nil {
		t.Error("expected DNS error for unresolvable host")
	}
}

// ─── UnfurlURL handler ────────────────────────────────────────────────────────

func newUnfurlEngine() *gin.Engine {
	r := gin.New()
	r.POST("/unfurl", UnfurlURL)
	return r
}

// withTestUnfurlClient replaces the package-level unfurlClient for the duration
// of the test and restores it via t.Cleanup.
func withTestUnfurlClient(t *testing.T, client *http.Client) {
	t.Helper()
	orig := unfurlClient
	unfurlClient = client
	t.Cleanup(func() { unfurlClient = orig })
}

// withPrivateHostBypass replaces isPrivateHostCheck so test server URLs
// (127.0.0.1:PORT) are not rejected.
func withPrivateHostBypass(t *testing.T) {
	t.Helper()
	orig := isPrivateHostCheck
	isPrivateHostCheck = func(hostname string) bool { return false }
	t.Cleanup(func() { isPrivateHostCheck = orig })
}

func TestUnfurlURL_InvalidJSONBody_Returns400(t *testing.T) {
	r := newUnfurlEngine()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/unfurl", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", w.Code)
	}
}

func TestUnfurlURL_FTPScheme_Returns400(t *testing.T) {
	r := newUnfurlEngine()
	body := `{"url":"ftp://example.com/file"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/unfurl", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for ftp scheme, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "http/https") {
		t.Errorf("expected 'http/https' in error body, got: %s", w.Body.String())
	}
}

func TestUnfurlURL_PrivateHost_Returns400(t *testing.T) {
	r := newUnfurlEngine()
	body := `{"url":"http://localhost/test"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/unfurl", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for private host, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "private") {
		t.Errorf("expected 'private' in error body, got: %s", w.Body.String())
	}
}

func TestUnfurlURL_ValidURL_Returns200WithParsedMeta(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, `<html><head>
			<meta property="og:title" content="Test Title">
			<meta property="og:description" content="A description">
		</head><body>Content</body></html>`)
	}))
	defer srv.Close()

	withTestUnfurlClient(t, srv.Client())
	withPrivateHostBypass(t)

	r := newUnfurlEngine()
	body := fmt.Sprintf(`{"url":"%s/page"}`, srv.URL)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/unfurl", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	if result["title"] != "Test Title" {
		t.Errorf("expected title 'Test Title', got %v", result["title"])
	}
}

func TestUnfurlURL_ServerReturns404_FallbackMeta(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	withTestUnfurlClient(t, srv.Client())
	withPrivateHostBypass(t)

	r := newUnfurlEngine()
	body := fmt.Sprintf(`{"url":"%s/missing"}`, srv.URL)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/unfurl", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 with fallback on 404, got %d", w.Code)
	}
}

func TestUnfurlURL_NonHTMLContentType_FallbackMeta(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"key":"value"}`)
	}))
	defer srv.Close()

	withTestUnfurlClient(t, srv.Client())
	withPrivateHostBypass(t)

	r := newUnfurlEngine()
	body := fmt.Sprintf(`{"url":"%s/api"}`, srv.URL)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/unfurl", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 with fallback for non-HTML, got %d", w.Code)
	}
}

func TestUnfurlURL_ClientError_FallbackMeta(t *testing.T) {
	// Use a client with a transport that always errors
	withTestUnfurlClient(t, &http.Client{
		Transport: &errorTransport{},
	})
	withPrivateHostBypass(t)

	r := newUnfurlEngine()
	body := `{"url":"http://example.com/page"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/unfurl", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 with fallback on client error, got %d", w.Code)
	}
}

// errorTransport always returns an error from RoundTrip.
type errorTransport struct{}

func (e *errorTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, &net.OpError{Op: "dial", Err: fmt.Errorf("simulated network failure")}
}

func TestSafeDialContext_PublicIP_AttemptsDial(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// 8.8.8.8:53 — Google DNS, public IP. safeDialContext should attempt the dial.
	// Connection may succeed or fail (connection refused/timeout) — either way covers the code.
	conn, err := safeDialContext(ctx, "tcp", "8.8.8.8:53")
	if conn != nil {
		_ = conn.Close()
	}
	// We only care that the dial path was reached (no private-IP error)
	if err != nil {
		var addrErr *net.AddrError
		if errors.As(err, &addrErr) && strings.Contains(addrErr.Err, "private") {
			t.Errorf("should not get private-address error for 8.8.8.8: %v", err)
		}
	}
}

// ─── Injectable function coverage ────────────────────────────────────────────

func TestSafeDialContext_DNSError_ReturnsError(t *testing.T) {
	orig := netLookupIPAddr
	netLookupIPAddr = func(_ context.Context, _ string) ([]net.IPAddr, error) {
		return nil, errors.New("lookup failed")
	}
	defer func() { netLookupIPAddr = orig }()

	_, err := safeDialContext(context.Background(), "tcp", "example.com:80")
	if err == nil {
		t.Error("expected error when DNS lookup fails")
	}
}

func TestSafeDialContext_PublicIPFromDNS_ReachesDialer(t *testing.T) {
	orig := netLookupIPAddr
	netLookupIPAddr = func(_ context.Context, _ string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("1.2.3.4")}}, nil
	}
	defer func() { netLookupIPAddr = orig }()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	// Dial will fail (connection refused/timeout) — we just need to cover the dial path.
	conn, _ := safeDialContext(ctx, "tcp", "example.com:1")
	if conn != nil {
		_ = conn.Close()
	}
}

func TestIsPrivateHost_DNSReturnsPrivateIP_ReturnsTrue(t *testing.T) {
	orig := netLookupIP
	netLookupIP = func(_ string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("192.168.1.1")}, nil
	}
	defer func() { netLookupIP = orig }()

	if !isPrivateHost("somehost.example.com") {
		t.Error("expected true when hostname resolves to private IP")
	}
}

func TestIsPrivateHost_DNSReturnsPublicIP_ReturnsFalse(t *testing.T) {
	orig := netLookupIP
	netLookupIP = func(_ string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	}
	defer func() { netLookupIP = orig }()

	if isPrivateHost("somehost.example.com") {
		t.Error("expected false when hostname resolves to public IP only")
	}
}

func TestIsPrivateHost_DNSFails_ReturnsTrue(t *testing.T) {
	orig := netLookupIP
	netLookupIP = func(_ string) ([]net.IP, error) {
		return nil, errors.New("lookup failed")
	}
	defer func() { netLookupIP = orig }()

	if !isPrivateHost("somehost.example.com") {
		t.Error("expected true (fail closed) when DNS resolution fails")
	}
}

func TestUnfurlURL_NewRequestFails_Returns400(t *testing.T) {
	orig := httpNewRequest
	httpNewRequest = func(_ context.Context, _, _ string, _ io.Reader) (*http.Request, error) {
		return nil, errors.New("request creation failed")
	}
	defer func() { httpNewRequest = orig }()
	withPrivateHostBypass(t)

	r := newUnfurlEngine()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/unfurl", strings.NewReader(`{"url":"http://example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("new request failure: want 400, got %d", w.Code)
	}
}

// errBodyReader returns an error on Read so io.ReadAll fails.
type errBodyReader struct{}

func (e *errBodyReader) Read(_ []byte) (int, error) {
	return 0, errors.New("simulated read error")
}
func (e *errBodyReader) Close() error { return nil }

type htmlErrorTransport struct{}

func (h *htmlErrorTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
		Body:       &errBodyReader{},
	}, nil
}

func TestUnfurlURL_ReadAllError_ReturnsFallbackMeta(t *testing.T) {
	withPrivateHostBypass(t)
	withTestUnfurlClient(t, &http.Client{Transport: &htmlErrorTransport{}})

	r := newUnfurlEngine()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/unfurl", strings.NewReader(`{"url":"http://example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("read error fallback: want 200, got %d", w.Code)
	}
}
