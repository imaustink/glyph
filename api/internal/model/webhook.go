package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// WebhookEventType names a kind of change a webhook can subscribe to. The
// set is fixed by the webhooks.event_types CHECK constraint and the capture
// triggers in migration 000027.
type WebhookEventType string

const (
	WebhookNoteCreated WebhookEventType = "note.created"
	WebhookNoteUpdated WebhookEventType = "note.updated"
	WebhookNoteDeleted WebhookEventType = "note.deleted"
	WebhookTaskCreated WebhookEventType = "task.created"
	WebhookTaskUpdated WebhookEventType = "task.updated"
	WebhookTaskDeleted WebhookEventType = "task.deleted"
)

// WebhookEventTypes lists every event type, in display order.
var WebhookEventTypes = []WebhookEventType{
	WebhookNoteCreated, WebhookNoteUpdated, WebhookNoteDeleted,
	WebhookTaskCreated, WebhookTaskUpdated, WebhookTaskDeleted,
}

// IsValid returns true if t is a known webhook event type.
func (t WebhookEventType) IsValid() bool {
	for _, v := range WebhookEventTypes {
		if t == v {
			return true
		}
	}
	return false
}

// Webhook is a user-configured endpoint that receives signed event
// deliveries. OrgID nil = personal webhook (changes to rows the owner owns);
// set = org webhook (changes to the org's non-private rows; owner-only).
type Webhook struct {
	ID         uuid.UUID          `json:"id"`
	UserID     uuid.UUID          `json:"userId"`
	OrgID      *uuid.UUID         `json:"orgId"`
	Name       string             `json:"name"`
	URL        string             `json:"url"`
	EventTypes []WebhookEventType `json:"eventTypes"`
	Active     bool               `json:"active"`
	CreatedAt  time.Time          `json:"createdAt"`
	UpdatedAt  time.Time          `json:"updatedAt"`
	// Secret is the HMAC signing key. Never serialised except through
	// WebhookWithSecret (on create and rotate).
	Secret string `json:"-"`
}

// WebhookWithSecret is returned once, when a webhook is created or its secret
// is rotated.
type WebhookWithSecret struct {
	Webhook
	Secret string `json:"secret"`
}

// WebhookEventStatus is where an event is in delivery.
type WebhookEventStatus string

const (
	WebhookEventPending   WebhookEventStatus = "pending"
	WebhookEventDelivered WebhookEventStatus = "delivered"
	WebhookEventFailed    WebhookEventStatus = "failed"
)

// WebhookEvent is one event recorded for one webhook, with the outcome of
// its most recent delivery attempt.
type WebhookEvent struct {
	ID         uuid.UUID        `json:"id"`
	WebhookID  uuid.UUID        `json:"webhookId"`
	Seq        *int64           `json:"sequence"` // nil until sequenced (shortly after commit)
	Type       WebhookEventType `json:"type"`
	ResourceID uuid.UUID        `json:"resourceId"`
	// Data is the note or task as it was after the change (before it, for a
	// hard delete), in the REST API's JSON shape.
	Data           json.RawMessage    `json:"data"`
	Status         WebhookEventStatus `json:"status"`
	Attempts       int                `json:"attempts"`
	LastStatusCode *int               `json:"lastStatusCode"`
	LastError      *string            `json:"lastError"`
	LastAttemptAt  *time.Time         `json:"lastAttemptAt"`
	NextAttemptAt  time.Time          `json:"nextAttemptAt"`
	DeliveredAt    *time.Time         `json:"deliveredAt"`
	CreatedAt      time.Time          `json:"createdAt"`
}

// WebhookDeliveryResult is the outcome of one delivery attempt.
type WebhookDeliveryResult struct {
	StatusCode *int    // nil when no response was received
	Error      *string // nil on a 2xx response
	At         time.Time
}

// OK reports whether the attempt succeeded (a 2xx response).
func (r WebhookDeliveryResult) OK() bool {
	return r.Error == nil && r.StatusCode != nil && *r.StatusCode >= 200 && *r.StatusCode < 300
}
