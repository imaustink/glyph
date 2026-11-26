package auth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"golang.org/x/oauth2"
)

// login runs /auth/login with cfg and returns the state sent to the provider
// and the state cookie the callback will compare it against.
func login(t *testing.T, cfg SessionConfig) (sentState, cookieState string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/auth/login", loginHandler(cfg))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/auth/login", nil))
	if w.Code != http.StatusFound {
		t.Fatalf("login: status %d, want 302", w.Code)
	}
	loc, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatalf("login: bad Location: %v", err)
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == stateCookieName {
			cookieState = c.Value
		}
	}
	return loc.Query().Get("state"), cookieState
}

func testSessionConfig() SessionConfig {
	return SessionConfig{
		OAuth2: oauth2.Config{
			ClientID:    "client",
			Endpoint:    oauth2.Endpoint{AuthURL: "https://idp.example/authorize", TokenURL: "https://idp.example/token"},
			RedirectURL: "https://auth.preview.example/callback",
		},
	}
}

func TestLoginStatePrefix(t *testing.T) {
	cfg := testSessionConfig()
	cfg.StatePrefix = "pr-42."

	sent, cookie := login(t, cfg)
	if !strings.HasPrefix(sent, "pr-42.") {
		t.Errorf("state %q does not start with the configured prefix", sent)
	}
	if len(sent) <= len("pr-42.") {
		t.Errorf("state %q has no random part after the prefix", sent)
	}
	// The callback compares the whole value, prefix included.
	if sent != cookie {
		t.Errorf("state sent to the provider %q != state cookie %q", sent, cookie)
	}
	// The bounce splits on the first '.', so the random part must not add one.
	if strings.Count(sent, ".") != 1 {
		t.Errorf("state %q: random part must not contain '.'", sent)
	}
}

func TestLoginStateNoPrefix(t *testing.T) {
	sent, cookie := login(t, testSessionConfig())
	if sent == "" || sent != cookie {
		t.Fatalf("state %q / cookie %q: want equal and non-empty", sent, cookie)
	}
	if strings.Contains(sent, ".") {
		t.Errorf("state %q: unexpected '.' without a prefix", sent)
	}
}
