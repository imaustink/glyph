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
	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), handler.RequestIDMiddleware(), handler.SecurityHeadersMiddleware())

	// Health check — no auth required
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	apiGroup := setupAuth(ctx, r, pool, s.users, sessionSecret)

	h := newHandlers(s)
	registerRoutes(apiGroup, h)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	return &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
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
