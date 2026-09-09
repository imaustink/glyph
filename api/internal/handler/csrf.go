package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// CSRFMiddleware rejects state-changing requests that lack the X-Requested-With
// header. Since custom headers cannot be set by cross-origin requests without a
// CORS preflight (which the server does not grant), this prevents CSRF attacks
// from third-party sites that rely on ambient cookie credentials.
//
// Requests bearing an Authorization: Bearer token are exempt: there is no
// ambient cookie credential for a forged cross-site request to ride on, and a
// cross-origin page cannot set a custom Authorization header without a CORS
// preflight the server does not grant — so CSRF does not apply to them.
func CSRFMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.HasPrefix(c.GetHeader("Authorization"), "Bearer ") {
			c.Next()
			return
		}
		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			// Safe methods — no check needed.
		default:
			if c.GetHeader("X-Requested-With") == "" {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "missing CSRF header"})
				return
			}
		}
		c.Next()
	}
}
