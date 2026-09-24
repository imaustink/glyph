package oauth

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/model"
)

// MCPResourcePath is where the MCP server is mounted. It is the protected
// resource advertised by the RFC 9728 metadata below.
const MCPResourcePath = "/mcp"

// ProtectedResourceMetadataPath is the RFC 9728 well-known location for the
// MCP resource's metadata. The path-suffixed variant is what RFC 9728 §3.1
// derives from a resource URL with a path; clients that don't follow that
// rule fall back to the bare one, so both are served.
const ProtectedResourceMetadataPath = "/.well-known/oauth-protected-resource"

// baseURL returns the configured public origin with no trailing slash.
func (cfg Config) baseURL() string {
	return strings.TrimRight(cfg.IssuerURL, "/")
}

// MCPResourceURL is the canonical URL of the MCP server (RFC 8707 resource
// indicator and RFC 9728 `resource` value).
func (cfg Config) MCPResourceURL() string {
	return cfg.baseURL() + MCPResourcePath
}

// ResourceMetadataURL is the URL a 401 from /mcp points clients at in its
// WWW-Authenticate header, so they can discover the authorization server.
func (cfg Config) ResourceMetadataURL() string {
	return cfg.baseURL() + ProtectedResourceMetadataPath + MCPResourcePath
}

func scopeStrings() []string {
	return scopesToStringSlice(model.AllScopes)
}

// AuthorizationServerMetadataHandler serves RFC 8414 metadata at
// /.well-known/oauth-authorization-server, which is how MCP clients find the
// authorize, token, and registration endpoints.
//
// authorization_endpoint is the SvelteKit consent page (it owns
// /oauth/authorize on the public origin), not a Go route.
func AuthorizationServerMetadataHandler(cfg Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		base := cfg.baseURL()
		authMethods := []string{"none", "client_secret_basic", "client_secret_post"}
		c.Header("Cache-Control", "public, max-age=3600")
		c.JSON(http.StatusOK, gin.H{
			"issuer":                                     base,
			"authorization_endpoint":                     base + "/oauth/authorize",
			"token_endpoint":                             base + "/oauth/token",
			"registration_endpoint":                      base + "/oauth/register",
			"revocation_endpoint":                        base + "/oauth/revoke",
			"response_types_supported":                   []string{"code"},
			"response_modes_supported":                   []string{"query"},
			"grant_types_supported":                      []string{"authorization_code", "refresh_token", "client_credentials"},
			"code_challenge_methods_supported":           []string{"S256"},
			"token_endpoint_auth_methods_supported":      authMethods,
			"revocation_endpoint_auth_methods_supported": authMethods,
			"scopes_supported":                           scopeStrings(),
			"service_documentation":                      "https://github.com/imaustink/glyph#mcp-server",
		})
	}
}

// ProtectedResourceMetadataHandler serves RFC 9728 metadata describing /mcp.
func ProtectedResourceMetadataHandler(cfg Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "public, max-age=3600")
		c.JSON(http.StatusOK, gin.H{
			"resource":                 cfg.MCPResourceURL(),
			"authorization_servers":    []string{cfg.baseURL()},
			"scopes_supported":         scopeStrings(),
			"bearer_methods_supported": []string{"header"},
			"resource_name":            "Glyph",
		})
	}
}

// RegisterMetadataRoutes registers the discovery documents on the engine
// root (they must live at the origin's /.well-known/, outside /api/v1).
func RegisterMetadataRoutes(r *gin.Engine, cfg Config) {
	as := AuthorizationServerMetadataHandler(cfg)
	pr := ProtectedResourceMetadataHandler(cfg)
	r.GET("/.well-known/oauth-authorization-server", publicCORS, as)
	r.OPTIONS("/.well-known/oauth-authorization-server", publicCORS)
	r.GET(ProtectedResourceMetadataPath, publicCORS, pr)
	r.OPTIONS(ProtectedResourceMetadataPath, publicCORS)
	r.GET(ProtectedResourceMetadataPath+MCPResourcePath, publicCORS, pr)
	r.OPTIONS(ProtectedResourceMetadataPath+MCPResourcePath, publicCORS)
}

// publicCORS allows cross-origin use of the machine-facing OAuth/MCP
// endpoints by browser-based MCP clients (e.g. the MCP Inspector). It never
// allows credentials: these endpoints authenticate with bearer tokens or
// client credentials in the request itself, never with cookies, so there is
// no ambient authority for another origin to ride on.
func publicCORS(c *gin.Context) {
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, Mcp-Session-Id, Mcp-Protocol-Version, Last-Event-ID")
	c.Header("Access-Control-Expose-Headers", "WWW-Authenticate, Mcp-Session-Id, Mcp-Protocol-Version")
	c.Header("Access-Control-Max-Age", "86400")
	if c.Request.Method == http.MethodOptions {
		c.AbortWithStatus(http.StatusNoContent)
		return
	}
	c.Next()
}

// PublicCORS exposes publicCORS for the MCP route, registered outside this package.
func PublicCORS() gin.HandlerFunc { return publicCORS }
