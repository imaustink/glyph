package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/handler"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newServer creates a configured *http.Server with the Gin router,
// health check, auth middleware, and all API routes registered.
func newServer(ctx context.Context, pool *pgxpool.Pool, s *stores, sessionSecret []byte) *http.Server {
	// gin's mode is set in main(), before this function runs, so that
	// mode-dependent guards elsewhere in startup (SESSION_SECRET requirement,
	// dev-auth opt-in) see the correct value.
	to := defaultTimeouts()
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), requestDeadline(to.Request), handler.RequestIDMiddleware(), handler.SecurityHeadersMiddleware())

	// Health check — no auth required
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	apiGroup := setupAuth(ctx, r, pool, s, sessionSecret)

	h := newHandlers(s, loadCollabConfig())
	registerRoutes(apiGroup, h)
	registerInternalRoutes(r, h)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	return newHTTPServer(":"+port, r, to)
}

// timeouts are the server's I/O limits plus the deadline every request's
// context carries.
type timeouts struct {
	Read, ReadHeader, Write, Idle time.Duration
	// Request is the context deadline for each request. It must end well
	// before Write: a handler still running when the server gives up on
	// writing its response can commit a save the client never hears about
	// (the client retries and gets a spurious 409). Under the deadline,
	// pgx cancels the query and the transaction rolls back instead.
	Request time.Duration
}

func defaultTimeouts() timeouts {
	return timeouts{
		// Reading a request body up to the 5 MiB content / 6 MiB MCP limit.
		Read:       30 * time.Second,
		ReadHeader: 10 * time.Second,
		Request:    30 * time.Second,
		Write:      60 * time.Second,
		Idle:       60 * time.Second,
	}
}

func newHTTPServer(addr string, h http.Handler, to timeouts) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadTimeout:       to.Read,
		ReadHeaderTimeout: to.ReadHeader,
		WriteTimeout:      to.Write,
		IdleTimeout:       to.Idle,
	}
}

// requestDeadline bounds each request's context, so database work started
// by a handler is cancelled before the server's WriteTimeout can cut the
// response off (see timeouts.Request).
func requestDeadline(d time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), d)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// runWithGracefulShutdown starts the server and blocks until a SIGINT/SIGTERM
// is received, then gracefully shuts down with a 10-second deadline.
func runWithGracefulShutdown(srv *http.Server) {
	go func() {
		slog.Info("glyph api starting", "port", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("forced shutdown: %v", err)
	}
	log.Println("stopped")
}
