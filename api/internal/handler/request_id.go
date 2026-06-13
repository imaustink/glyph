package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// RequestIDKey is the Gin context key where the request ID is stored.
const RequestIDKey = "request_id"

// RequestIDMiddleware generates a unique request ID for each request and stores
// it in the Gin context. The ID is also set as an X-Request-ID response header
// so clients and operators can correlate logs with requests.
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Honour an incoming X-Request-ID if present (e.g., from a reverse proxy).
		reqID := c.GetHeader("X-Request-ID")
		if reqID == "" {
			reqID = uuid.New().String()
		}
		c.Set(RequestIDKey, reqID)
		c.Header("X-Request-ID", reqID)
		c.Next()
	}
}
