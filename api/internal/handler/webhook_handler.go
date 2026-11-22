package handler

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/auth"
	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
	"github.com/glyph/api/internal/webhook"
	"github.com/google/uuid"
)

// SafeDialContext is the SSRF-guarded dialer used for URL unfurling, exported
// for webhook deliveries: it refuses private/reserved addresses and ports
// other than 80 and 443, and dials the IP it validated.
var SafeDialContext = safeDialContext

// WebhookReplayer re-delivers one event (webhook.Dispatcher).
type WebhookReplayer interface {
	Replay(ctx context.Context, w *model.Webhook, e *model.WebhookEvent) (*model.WebhookEvent, error)
}

// WebhookHandler manages a user's webhooks and their event history. The
// management endpoints are session-only (like OAuth clients: an OAuth token
// must not be able to point events, or a signing secret, elsewhere). Feed is
// the exception: the webhook's consumer calls it, authenticated by a request
// signature made with the webhook's secret.
type WebhookHandler struct {
	Webhooks store.WebhookStore
	Orgs     store.OrgStore
	Replayer WebhookReplayer
	// AllowPrivateURLs lifts the public-address/standard-port requirement on
	// webhook URLs (local development only).
	AllowPrivateURLs bool
	// Now is the clock (default time.Now).
	Now func() time.Time
}

const (
	maxWebhookNameLen = 100
	maxWebhookURLLen  = 2048
	maxEventsPage     = 200
	maxFeedPage       = 500
)

func (h *WebhookHandler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

func rejectBearerForWebhooks(c *gin.Context) bool {
	if _, ok := c.Get(model.TokenScopeContextKey); ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "OAuth clients cannot manage webhooks"})
		return false
	}
	return true
}

// validateWebhookURL returns an error message, or "" if raw is acceptable.
func (h *WebhookHandler) validateWebhookURL(raw string) string {
	if raw == "" || len(raw) > maxWebhookURLLen {
		return "url is required (at most 2048 characters)"
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return "url must be an absolute http or https URL"
	}
	if u.User != nil {
		return "url must not contain credentials"
	}
	if h.AllowPrivateURLs {
		return ""
	}
	if p := u.Port(); p != "" && p != "80" && p != "443" {
		return "url must use port 80 or 443"
	}
	if isPrivateHostCheck(u.Hostname()) {
		return "url points to a private/reserved address"
	}
	return ""
}

// normalizeEventTypes validates and de-duplicates types (keeping the
// canonical order), or returns ok=false.
func normalizeEventTypes(types []string) ([]model.WebhookEventType, bool) {
	if len(types) == 0 {
		return nil, false
	}
	seen := map[model.WebhookEventType]bool{}
	for _, t := range types {
		et := model.WebhookEventType(t)
		if !et.IsValid() {
			return nil, false
		}
		seen[et] = true
	}
	out := []model.WebhookEventType{}
	for _, et := range model.WebhookEventTypes {
		if seen[et] {
			out = append(out, et)
		}
	}
	return out, true
}

// GET /webhooks/event-types
func (h *WebhookHandler) ListEventTypes(c *gin.Context) {
	c.JSON(http.StatusOK, model.WebhookEventTypes)
}

// POST /webhooks
func (h *WebhookHandler) CreateWebhook(c *gin.Context) {
	if !rejectBearerForWebhooks(c) {
		return
	}
	user := auth.CurrentUser(c)
	var body struct {
		Name       string     `json:"name"`
		URL        string     `json:"url"`
		EventTypes []string   `json:"eventTypes"`
		OrgID      *uuid.UUID `json:"orgId"`
		Active     *bool      `json:"active"`
	}
	if !bindJSON(c, &body) {
		return
	}
	name := strings.TrimSpace(body.Name)
	if len(name) > maxWebhookNameLen {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name must be at most 100 characters"})
		return
	}
	if msg := h.validateWebhookURL(strings.TrimSpace(body.URL)); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": msg})
		return
	}
	types, ok := normalizeEventTypes(body.EventTypes)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "eventTypes must list at least one known event type"})
		return
	}
	if body.OrgID != nil && !RequireOrgOwner(c, h.Orgs, *body.OrgID, user.ID) {
		return
	}
	secret, err := webhook.NewSecret()
	if err != nil {
		internalError(c, err)
		return
	}
	active := true
	if body.Active != nil {
		active = *body.Active
	}
	created, err := h.Webhooks.Create(c.Request.Context(), &model.Webhook{
		UserID:     user.ID,
		OrgID:      body.OrgID,
		Name:       name,
		URL:        strings.TrimSpace(body.URL),
		EventTypes: types,
		Active:     active,
		Secret:     secret,
	})
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusCreated, model.WebhookWithSecret{Webhook: *created, Secret: created.Secret})
}

// GET /webhooks
func (h *WebhookHandler) ListWebhooks(c *gin.Context) {
	if !rejectBearerForWebhooks(c) {
		return
	}
	hooks, err := h.Webhooks.ListByUser(c.Request.Context(), auth.CurrentUser(c).ID)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, hooks)
}

// loadOwned resolves :id to a webhook the caller owns, writing the error
// response otherwise.
func (h *WebhookHandler) loadOwned(c *gin.Context) (*model.Webhook, bool) {
	if !rejectBearerForWebhooks(c) {
		return nil, false
	}
	id, ok := parseUUID(c, "id")
	if !ok {
		return nil, false
	}
	w, err := h.Webhooks.Get(c.Request.Context(), id, auth.CurrentUser(c).ID)
	if err != nil {
		notFoundOrError(c, err)
		return nil, false
	}
	return w, true
}

// GET /webhooks/:id
func (h *WebhookHandler) GetWebhook(c *gin.Context) {
	w, ok := h.loadOwned(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, w)
}

// PATCH /webhooks/:id
func (h *WebhookHandler) UpdateWebhook(c *gin.Context) {
	w, ok := h.loadOwned(c)
	if !ok {
		return
	}
	var body struct {
		Name       *string  `json:"name"`
		URL        *string  `json:"url"`
		EventTypes []string `json:"eventTypes"`
		Active     *bool    `json:"active"`
	}
	keys, ok := bindJSONWithKeys(c, &body)
	if !ok {
		return
	}
	var patch store.WebhookPatch
	if body.Name != nil {
		name := strings.TrimSpace(*body.Name)
		if len(name) > maxWebhookNameLen {
			c.JSON(http.StatusBadRequest, gin.H{"error": "name must be at most 100 characters"})
			return
		}
		patch.Name = &name
	}
	if body.URL != nil {
		u := strings.TrimSpace(*body.URL)
		if msg := h.validateWebhookURL(u); msg != "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": msg})
			return
		}
		patch.URL = &u
	}
	if _, present := keys["eventTypes"]; present {
		types, ok := normalizeEventTypes(body.EventTypes)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "eventTypes must list at least one known event type"})
			return
		}
		patch.EventTypes = types
	}
	patch.Active = body.Active
	updated, err := h.Webhooks.Update(c.Request.Context(), w.ID, w.UserID, patch)
	if err != nil {
		notFoundOrError(c, err)
		return
	}
	c.JSON(http.StatusOK, updated)
}

// DELETE /webhooks/:id
func (h *WebhookHandler) DeleteWebhook(c *gin.Context) {
	w, ok := h.loadOwned(c)
	if !ok {
		return
	}
	if err := h.Webhooks.Delete(c.Request.Context(), w.ID, w.UserID); err != nil {
		notFoundOrError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// POST /webhooks/:id/rotate-secret
func (h *WebhookHandler) RotateSecret(c *gin.Context) {
	w, ok := h.loadOwned(c)
	if !ok {
		return
	}
	secret, err := webhook.NewSecret()
	if err != nil {
		internalError(c, err)
		return
	}
	updated, err := h.Webhooks.SetSecret(c.Request.Context(), w.ID, w.UserID, secret)
	if err != nil {
		notFoundOrError(c, err)
		return
	}
	c.JSON(http.StatusOK, model.WebhookWithSecret{Webhook: *updated, Secret: updated.Secret})
}

func intQuery(c *gin.Context, name string, def, lo, hi int) (int, bool) {
	raw := c.Query(name)
	if raw == "" {
		return def, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < lo || n > hi {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid " + name})
		return 0, false
	}
	return n, true
}

// GET /webhooks/:id/events?limit=&offset=
//
// The webhook's events from the retention window (the last 30 days), newest
// first, with each one's delivery status and last response code.
func (h *WebhookHandler) ListEvents(c *gin.Context) {
	w, ok := h.loadOwned(c)
	if !ok {
		return
	}
	limit, ok := intQuery(c, "limit", 50, 1, maxEventsPage)
	if !ok {
		return
	}
	offset, ok := intQuery(c, "offset", 0, 0, 1<<30)
	if !ok {
		return
	}
	events, total, err := h.Webhooks.ListEvents(c.Request.Context(), w.ID, h.now().Add(-webhook.Retention), limit, offset)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"events":        events,
		"total":         total,
		"retentionDays": int(webhook.Retention / (24 * time.Hour)),
	})
}

// POST /webhooks/:id/events/:eventId/replay
func (h *WebhookHandler) ReplayEvent(c *gin.Context) {
	w, ok := h.loadOwned(c)
	if !ok {
		return
	}
	eventID, ok := parseUUID(c, "eventId")
	if !ok {
		return
	}
	e, err := h.Webhooks.GetEvent(c.Request.Context(), w.ID, eventID)
	if err != nil {
		notFoundOrError(c, err)
		return
	}
	updated, err := h.Replayer.Replay(c.Request.Context(), w, e)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, updated)
}

// WebhookFeedRateLimit limits Feed per webhook and client IP. Feed has no
// session, so it can't use the per-user RateLimitMiddleware.
func WebhookFeedRateLimit(rl *RateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !rl.Allow(c.Param("id") + "|" + c.ClientIP()) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded, please try again later"})
			return
		}
		c.Next()
	}
}

// GET /webhooks/:id/feed?after=<sequence>&limit=
//
// Lets a webhook's consumer fetch the events after the last sequence number
// it processed, e.g. to fill a gap left by an outage. Not session
// authenticated: the request must carry Glyph-Webhook-Timestamp and
// Glyph-Webhook-Signature headers signing its request URI with the webhook's
// secret (see webhook.Sign).
func (h *WebhookHandler) Feed(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	unauthorized := func() {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or missing webhook signature"})
	}
	if err != nil {
		unauthorized()
		return
	}
	w, err := h.Webhooks.GetByID(c.Request.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		unauthorized() // don't reveal which webhook IDs exist
		return
	}
	if err != nil {
		internalError(c, err)
		return
	}
	if webhook.Verify(w.Secret, c.GetHeader(webhook.HeaderTimestamp), c.GetHeader(webhook.HeaderSignature),
		[]byte(c.Request.URL.RequestURI()), h.now()) != nil {
		unauthorized()
		return
	}

	var after int64
	if raw := c.Query("after"); raw != "" {
		after, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || after < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid after"})
			return
		}
	}
	limit, ok := intQuery(c, "limit", 100, 1, maxFeedPage)
	if !ok {
		return
	}
	// Number anything committed but not yet sequenced, so the feed is never
	// behind what the dispatcher will send.
	if err := h.Webhooks.Sequence(c.Request.Context(), w.ID); err != nil {
		internalError(c, err)
		return
	}
	// One extra row tells us whether there is more.
	events, err := h.Webhooks.ListEventsAfter(c.Request.Context(), w.ID, after, limit+1)
	if err != nil {
		internalError(c, err)
		return
	}
	hasMore := len(events) > limit
	if hasMore {
		events = events[:limit]
	}
	out := make([]webhook.Envelope, len(events))
	last := after
	for i, e := range events {
		out[i] = webhook.NewEnvelope(e)
		last = *e.Seq
	}
	c.JSON(http.StatusOK, gin.H{"events": out, "hasMore": hasMore, "lastSequence": last})
}
