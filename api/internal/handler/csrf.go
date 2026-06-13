package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// CSRFMiddleware rejects state-changing requests that lack the X-Requested-With
// header. Since custom headers cannot be set by cross-origin requests without a
// CORS preflight (which the server does not grant), this prevents CSRF attacks
// from third-party sites that rely on ambient cookie credentials.
func CSRFMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
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
