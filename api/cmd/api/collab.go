package main

import (
	"log"
	"log/slog"
	"os"

	"github.com/gin-gonic/gin"
)

// collabConfig controls realtime collaborative editing.
type collabConfig struct {
	// enabled is the server-wide switch (COLLAB_ENABLED=true). Turning it off
	// is the kill switch: clients fall back to single-writer editing, and the
	// first REST write to a page that was being edited collaboratively
	// detaches it, making page_contents authoritative again.
	enabled bool
	// serviceToken authenticates the collab service's snapshot writes
	// (COLLAB_SERVICE_TOKEN). Must match the collab service's configuration.
	serviceToken string
}

func loadCollabConfig() collabConfig {
	cfg := collabConfig{
		enabled:      os.Getenv("COLLAB_ENABLED") == "true",
		serviceToken: os.Getenv("COLLAB_SERVICE_TOKEN"),
	}
	if cfg.enabled && cfg.serviceToken == "" {
		if gin.Mode() == gin.ReleaseMode {
			log.Fatal("COLLAB_SERVICE_TOKEN is required when COLLAB_ENABLED=true. Generate with: openssl rand -hex 32")
		}
		slog.Warn("COLLAB_ENABLED=true but COLLAB_SERVICE_TOKEN is unset — collab snapshots will be refused")
	}
	slog.Info("collaborative editing", "enabled", cfg.enabled)
	return cfg
}
