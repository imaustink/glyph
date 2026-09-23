package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// discoveryServer serves a discovery document whose `issuer` is the server's
// own URL, which is what a conforming provider does.
func discoveryServer(t *testing.T, body func(issuer string) string) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body(srv.URL)))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDiscoverEndpoint(t *testing.T) {
	srv := discoveryServer(t, func(iss string) string {
		return `{
			"issuer": "` + iss + `",
			"authorization_endpoint": "` + iss + `/authorize",
			"token_endpoint": "` + iss + `/api/oidc/token",
			"jwks_uri": "` + iss + `/.well-known/jwks.json"
		}`
	})

	ep, gotIssuer, err := DiscoverEndpoint(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("DiscoverEndpoint: %v", err)
	}
	if want := srv.URL + "/authorize"; ep.AuthURL != want {
		t.Errorf("AuthURL = %q, want %q", ep.AuthURL, want)
	}
	if want := srv.URL + "/api/oidc/token"; ep.TokenURL != want {
		t.Errorf("TokenURL = %q, want %q", ep.TokenURL, want)
	}
	if gotIssuer != srv.URL {
		t.Errorf("issuer = %q, want %q", gotIssuer, srv.URL)
	}
}

// A trailing slash on OIDC_ISSUER_URL is the most common way to get an issuer
// that looks right but does not match the ID token's `iss` claim. Discovery
// must tolerate it — and, critically, return the provider's canonical issuer
// (no trailing slash) so the value later enforced via jwt.WithIssuer is exactly
// what the token's `iss` will carry. Asserting only that discovery succeeds
// gives false confidence: the login still fails downstream if the caller then
// enforces the slash-suffixed value.
func TestDiscoverEndpointTrailingSlashIssuer(t *testing.T) {
	srv := discoveryServer(t, func(iss string) string {
		return `{
			"issuer": "` + iss + `",
			"authorization_endpoint": "` + iss + `/authorize",
			"token_endpoint": "` + iss + `/api/oidc/token"
		}`
	})

	_, gotIssuer, err := DiscoverEndpoint(context.Background(), srv.URL+"/")
	if err != nil {
		t.Fatalf("trailing slash should be tolerated, got: %v", err)
	}
	// srv.URL has no trailing slash; the returned issuer must match it exactly
	// so jwt.WithIssuer agrees with the ID token's `iss` claim.
	if gotIssuer != srv.URL {
		t.Errorf("canonical issuer = %q, want %q (no trailing slash)", gotIssuer, srv.URL)
	}
}

// When the discovery document omits `issuer`, discovery falls back to the
// trimmed configured value so the caller still has an exact issuer to enforce.
func TestDiscoverEndpointIssuerFallback(t *testing.T) {
	srv := discoveryServer(t, func(iss string) string {
		return `{
			"authorization_endpoint": "` + iss + `/authorize",
			"token_endpoint": "` + iss + `/api/oidc/token"
		}`
	})

	_, gotIssuer, err := DiscoverEndpoint(context.Background(), srv.URL+"/")
	if err != nil {
		t.Fatalf("DiscoverEndpoint: %v", err)
	}
	if gotIssuer != srv.URL {
		t.Errorf("canonical issuer = %q, want %q (trimmed configured value)", gotIssuer, srv.URL)
	}
}

func TestDiscoverEndpointErrors(t *testing.T) {
	tests := []struct {
		name    string
		doc     func(iss string) string
		wantErr string
	}{
		{
			name: "missing authorization_endpoint",
			doc: func(iss string) string {
				return `{"issuer": "` + iss + `", "token_endpoint": "` + iss + `/token"}`
			},
			wantErr: "missing authorization_endpoint",
		},
		{
			name: "missing token_endpoint",
			doc: func(iss string) string {
				return `{"issuer": "` + iss + `", "authorization_endpoint": "` + iss + `/authorize"}`
			},
			wantErr: "missing token_endpoint",
		},
		{
			// The failure this guards against is silent: without the check the
			// login round-trip completes and every ID token then fails
			// jwt.WithIssuer far from the actual misconfiguration.
			name: "issuer mismatch",
			doc: func(iss string) string {
				return `{
					"issuer": "https://someone-else.example.com",
					"authorization_endpoint": "` + iss + `/authorize",
					"token_endpoint": "` + iss + `/token"
				}`
			},
			wantErr: "issuer mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := discoveryServer(t, tt.doc)
			_, _, err := DiscoverEndpoint(context.Background(), srv.URL)
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestDiscoverEndpointUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	if _, _, err := DiscoverEndpoint(context.Background(), srv.URL); err == nil {
		t.Fatal("expected an error when discovery returns 500")
	}
}
