package main

import (
	"log/slog"
	"net"
	"os"
	"strconv"

	"github.com/glyph/api/internal/handler"
	"github.com/glyph/api/internal/webhook"
)

// webhookConfig is the webhook delivery wiring.
type webhookConfig struct {
	dispatcher *webhook.Dispatcher
	// allowPrivateURLs (WEBHOOKS_ALLOW_PRIVATE_URLS=true) lets webhooks target
	// private addresses and non-standard ports, e.g. a receiver on localhost
	// during development. Never enable it where users are untrusted: it
	// turns webhooks into a way to reach internal services.
	allowPrivateURLs bool
}

func loadWebhookConfig(s *stores) webhookConfig {
	allowPrivate, _ := strconv.ParseBool(os.Getenv("WEBHOOKS_ALLOW_PRIVATE_URLS"))
	dial := handler.SafeDialContext
	if allowPrivate {
		slog.Warn("WEBHOOKS_ALLOW_PRIVATE_URLS is set — webhooks may target private addresses")
		dial = (&net.Dialer{Timeout: webhook.DeliveryTimeout}).DialContext
	}
	return webhookConfig{
		dispatcher: &webhook.Dispatcher{
			Store:  s.webhooks,
			Client: webhook.NewHTTPClient(dial),
		},
		allowPrivateURLs: allowPrivate,
	}
}
