package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"log/slog"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/db"
	"github.com/glyph/api/internal/handler"
	"github.com/joho/godotenv"
)

func main() {
	// Load .env if present (local development); ignore errors in production.
	_ = godotenv.Load()

	ctx := context.Background()

	pool, err := db.Connect(ctx)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer pool.Close()

	s := newStores(pool)

	handler.RegisterValidators()

	sessionSecret := getOrGenerateSessionSecret()

	srv := newServer(ctx, pool, s, sessionSecret)
	runWithGracefulShutdown(srv)
}

// getOrGenerateSessionSecret reads SESSION_SECRET from the environment (hex-encoded)
// or generates a random 32-byte key for local dev. In production (GIN_MODE=release),
// this function fatals if SESSION_SECRET is not set — sessions must survive restarts.
func getOrGenerateSessionSecret() []byte {
	if s := os.Getenv("SESSION_SECRET"); s != "" {
		b, err := hex.DecodeString(s)
		if err != nil {
			log.Fatalf("SESSION_SECRET must be a hex-encoded string: %v", err)
		}
		return b
	}
	if gin.Mode() == gin.ReleaseMode {
		log.Fatal("SESSION_SECRET is required in production (GIN_MODE=release). Generate with: openssl rand -hex 32")
	}
	slog.Warn("SESSION_SECRET not set — generating a random key. Sessions will not survive restarts.")
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		log.Fatalf("failed to generate session secret: %v", err)
	}
	return b
}
