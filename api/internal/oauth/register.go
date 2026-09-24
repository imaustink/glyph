package oauth

import (
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/handler"
	"github.com/glyph/api/internal/model"
)

const (
	maxRegisteredRedirectURIs = 10
	maxClientNameLen          = 100
	defaultDynamicClientName  = "MCP client"
)

// registrationRequest is the subset of RFC 7591 §2 client metadata Glyph
// acts on. Unknown fields (client_uri, logo_uri, contacts, …) are accepted
// and ignored, as §2 requires.
type registrationRequest struct {
	RedirectURIs            []string `json:"redirect_uris"`
	ClientName              string   `json:"client_name"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	Scope                   string   `json:"scope"`
}

// registrationError writes an RFC 7591 §3.2.2 error response.
func registrationError(c *gin.Context, code, description string) {
	c.JSON(http.StatusBadRequest, gin.H{"error": code, "error_description": description})
}

// RegisterHandler implements RFC 7591 dynamic client registration at
// POST /oauth/register, which MCP clients use to obtain a client_id with no
// prior setup.
//
// Registration is open (unauthenticated) by design — that is what lets a user
// connect an agent with nothing but the server URL — so a registered client
// is granted nothing on its own. It can only start an authorization_code +
// PKCE flow, and a user must approve it on the consent screen (which flags it
// as unverified) before any token exists. In particular it can never use
// client_credentials, which would let it mint tokens for arbitrary users.
func RegisterHandler(cfg Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req registrationRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			registrationError(c, "invalid_client_metadata", "request body must be a JSON object")
			return
		}

		if len(req.RedirectURIs) == 0 {
			registrationError(c, "invalid_redirect_uri", "at least one redirect_uri is required")
			return
		}
		if len(req.RedirectURIs) > maxRegisteredRedirectURIs {
			registrationError(c, "invalid_redirect_uri", "too many redirect_uris")
			return
		}
		for _, u := range req.RedirectURIs {
			if !validDynamicRedirectURI(u) {
				registrationError(c, "invalid_redirect_uri", "redirect_uri must be https, http on a loopback address, or a private-use app scheme: "+u)
				return
			}
		}

		authMethod := req.TokenEndpointAuthMethod
		if authMethod == "" {
			// RFC 7591 §2 defaults to client_secret_basic, but MCP clients are
			// overwhelmingly public (desktop/CLI) and PKCE-only; defaulting to
			// "none" avoids handing a long-lived secret to a client that
			// didn't ask for one and can't keep it.
			authMethod = "none"
		}
		switch authMethod {
		case "none", "client_secret_basic", "client_secret_post":
		default:
			registrationError(c, "invalid_client_metadata", "unsupported token_endpoint_auth_method")
			return
		}

		for _, g := range req.GrantTypes {
			if g != "authorization_code" && g != "refresh_token" {
				registrationError(c, "invalid_client_metadata", "only authorization_code and refresh_token grant types can be registered")
				return
			}
		}
		for _, rt := range req.ResponseTypes {
			if rt != "code" {
				registrationError(c, "invalid_client_metadata", "only the code response type is supported")
				return
			}
		}

		scopes := model.AllScopes
		if req.Scope != "" {
			scopes = intersectScopes(parseScopeParam(req.Scope), model.AllScopes)
			if len(scopes) == 0 {
				registrationError(c, "invalid_client_metadata", "no supported scopes requested")
				return
			}
		}

		clientID, clientSecret, secretHash, err := GenerateClientCredentials()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
			return
		}
		confidential := authMethod != "none"
		client := &model.OAuthClient{
			ClientID:       clientID,
			Name:           sanitizeClientName(req.ClientName),
			GrantTypes:     []model.OAuthGrantType{model.GrantAuthorizationCode},
			Scopes:         scopes,
			RedirectURIs:   req.RedirectURIs,
			IsConfidential: confidential,
			IsDynamic:      true,
		}
		created, err := cfg.Clients.Create(c.Request.Context(), client, secretHash)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
			return
		}

		resp := gin.H{
			"client_id":                  created.ClientID,
			"client_id_issued_at":        created.CreatedAt.Unix(),
			"client_name":                created.Name,
			"redirect_uris":              created.RedirectURIs,
			"grant_types":                []string{"authorization_code", "refresh_token"},
			"response_types":             []string{"code"},
			"token_endpoint_auth_method": authMethod,
			"scope":                      scopesToParam(created.Scopes),
		}
		if confidential {
			resp["client_secret"] = clientSecret
			resp["client_secret_expires_at"] = 0
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusCreated, resp)
	}
}

// validDynamicRedirectURI applies RFC 8252 / OAuth 2.1 redirect rules for
// self-registered clients: https anywhere; http only on a loopback address
// (native apps listening on localhost, e.g. Claude Code); or a private-use
// URI scheme (e.g. cursor://) for apps that claim one. Schemes a browser
// would execute or read locally are refused outright, and fragments are
// forbidden by RFC 6749 §3.1.2.
func validDynamicRedirectURI(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Fragment != "" || u.Scheme == "" {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return u.Host != ""
	case "http":
		return isLoopbackHost(u.Hostname())
	case "javascript", "data", "file", "vbscript", "blob", "about", "ftp", "ws", "wss", "chrome", "view-source":
		return false
	default:
		// A private-use scheme: must at least identify something.
		return u.Host != "" || u.Opaque != "" || u.Path != ""
	}
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// sanitizeClientName strips control characters and bounds the length of a
// self-asserted client name before it is shown on the consent screen.
func sanitizeClientName(name string) string {
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" {
		return defaultDynamicClientName
	}
	if r := []rune(name); len(r) > maxClientNameLen {
		name = string(r[:maxClientNameLen])
	}
	return name
}

// RedirectHost is the part of a redirect URI shown on the consent screen so
// the user can see where an unverified client will send them.
func RedirectHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if u.Scheme == "https" || u.Scheme == "http" {
		return u.Host
	}
	if u.Host != "" {
		return u.Scheme + "://" + u.Host
	}
	return u.Scheme + ":"
}

// registrationRateLimit throttles open registration per client IP. MCP
// clients register once per server they're configured with, so a low limit
// only affects abuse (filling oauth_clients with junk rows).
func registrationRateLimit(rl *handler.RateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !rl.Allow("register|" + c.ClientIP()) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded, please try again later"})
			return
		}
		c.Next()
	}
}

func newRegistrationLimiter() *handler.RateLimiter {
	return handler.NewRateLimiter(20, time.Hour)
}
