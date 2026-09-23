package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

// DiscoverEndpoint resolves the OAuth 2.0 authorization and token endpoints for
// an OIDC provider from its discovery document, and returns the provider's
// canonical issuer alongside them.
//
// The JWKS side of this package already fetches
// <issuer>/.well-known/openid-configuration to find jwks_uri (see
// keyManager.discoverJWKSURI); this reads the two endpoints the login flow
// needs out of the same document, so one configured value describes the
// provider completely.
//
// The returned issuer is the document's own `issuer` value (the trimmed
// configured value when the document omits it). Callers must use it — not the
// raw OIDC_ISSUER_URL — for SessionConfig.IssuerURL: jwt.WithIssuer does an
// exact-string compare against the ID token's `iss` claim, and the claim will
// carry exactly this canonical value. Feeding it the raw env var instead lets a
// trailing-slash or scheme difference survive the whole browser round-trip and
// fail every token with an opaque "invalid_id_token".
//
// Callers should treat a failure here as fatal: a deployment that cannot
// discover its provider cannot log anyone in, and substituting a default
// provider would hide that behind a confusing provider-side error.
func DiscoverEndpoint(ctx context.Context, issuerURL string) (oauth2.Endpoint, string, error) {
	var ep oauth2.Endpoint

	issuerURL = strings.TrimRight(issuerURL, "/")
	discoveryURL := issuerURL + "/.well-known/openid-configuration"

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return ep, "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ep, "", fmt.Errorf("fetch discovery: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return ep, "", fmt.Errorf("discovery endpoint returned %d", resp.StatusCode)
	}

	var doc struct {
		Issuer       string `json:"issuer"`
		AuthorizeURL string `json:"authorization_endpoint"`
		TokenURL     string `json:"token_endpoint"`
	}
	if err := decodeJSON(resp.Body, &doc); err != nil {
		return ep, "", fmt.Errorf("decode discovery: %w", err)
	}

	if doc.AuthorizeURL == "" {
		return ep, "", errors.New("discovery document missing authorization_endpoint")
	}
	if doc.TokenURL == "" {
		return ep, "", errors.New("discovery document missing token_endpoint")
	}

	// OIDC Discovery 1.0 §4.3 requires the document's own issuer to equal the
	// value used to build the discovery URL. Checking it here is not ceremony:
	// SessionConfig.IssuerURL is what callbackHandler later enforces on the ID
	// token via jwt.WithIssuer, so a provider whose canonical issuer differs
	// from OIDC_ISSUER_URL (a trailing-slash or scheme mismatch is the usual
	// cause) would complete the whole browser round-trip and only then fail
	// every token with an opaque "invalid_id_token". Better to refuse at boot.
	if doc.Issuer != "" && strings.TrimRight(doc.Issuer, "/") != issuerURL {
		return ep, "", fmt.Errorf(
			"discovery issuer mismatch: OIDC_ISSUER_URL is %q but the provider identifies as %q",
			issuerURL, doc.Issuer)
	}

	// Return the provider's canonical issuer verbatim so the caller enforces on
	// tokens exactly what the provider declares (and stamps into `iss`). A
	// trailing-slash-only difference passes the mismatch check above, but the
	// token's `iss` will be doc.Issuer, not the slash-suffixed OIDC_ISSUER_URL —
	// so returning the raw configured value here would still fail every login.
	canonicalIssuer := doc.Issuer
	if canonicalIssuer == "" {
		canonicalIssuer = issuerURL
	}

	ep.AuthURL = doc.AuthorizeURL
	ep.TokenURL = doc.TokenURL
	return ep, canonicalIssuer, nil
}
