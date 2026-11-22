// Package webhook delivers webhook events: it builds and signs the request
// bodies, runs the background dispatcher that delivers pending events in
// sequence order and retries failures, and purges events past retention.
//
// Events are recorded by database triggers (migration 000027), so this
// package only ever reads and updates them.
package webhook

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/glyph/api/internal/model"
	"github.com/google/uuid"
)

// Request headers sent with every delivery (and expected on feed requests).
const (
	HeaderEventID   = "Glyph-Webhook-Id"
	HeaderEventType = "Glyph-Webhook-Event"
	HeaderSequence  = "Glyph-Webhook-Sequence"
	HeaderTimestamp = "Glyph-Webhook-Timestamp"
	HeaderSignature = "Glyph-Webhook-Signature"
)

// SignatureTolerance is how far a signed feed request's timestamp may be from
// the server's clock.
const SignatureTolerance = 5 * time.Minute

// NewSecret returns a new random signing secret.
func NewSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "whsec_" + base64.RawURLEncoding.EncodeToString(b), nil
}

// Sign returns the signature header value for message signed at ts:
// "v1=" + hex(HMAC-SHA256(secret, "<unix ts>.<message>")).
//
// For a delivery the message is the raw request body; for a feed request it
// is the request URI (path and query, e.g. "/api/v1/webhooks/<id>/feed?after=12").
func Sign(secret string, ts time.Time, message []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(ts.Unix(), 10)))
	mac.Write([]byte("."))
	mac.Write(message)
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}

// ErrBadSignature is returned by Verify for a missing, stale or wrong signature.
var ErrBadSignature = errors.New("invalid webhook signature")

// Verify checks a signature header (which may carry several comma-separated
// "v1=" values) against message and the timestamp header, accepting
// timestamps within SignatureTolerance of now.
func Verify(secret, timestampHeader, signatureHeader string, message []byte, now time.Time) error {
	unix, err := strconv.ParseInt(strings.TrimSpace(timestampHeader), 10, 64)
	if err != nil {
		return ErrBadSignature
	}
	ts := time.Unix(unix, 0)
	if d := now.Sub(ts); d > SignatureTolerance || d < -SignatureTolerance {
		return ErrBadSignature
	}
	want := Sign(secret, ts, message)
	for _, got := range strings.Split(signatureHeader, ",") {
		if hmac.Equal([]byte(strings.TrimSpace(got)), []byte(want)) {
			return nil
		}
	}
	return ErrBadSignature
}

// Envelope is the JSON body of a delivery, and the shape of each event the
// feed endpoint returns.
type Envelope struct {
	ID        uuid.UUID              `json:"id"`
	WebhookID uuid.UUID              `json:"webhookId"`
	Sequence  *int64                 `json:"sequence"`
	Type      model.WebhookEventType `json:"type"`
	CreatedAt time.Time              `json:"createdAt"`
	Data      json.RawMessage        `json:"data"`
}

// NewEnvelope builds the envelope for e. It is a pure function of the stored
// event, so a replay sends byte-for-byte the same body as the original.
func NewEnvelope(e *model.WebhookEvent) Envelope {
	return Envelope{
		ID:        e.ID,
		WebhookID: e.WebhookID,
		Sequence:  e.Seq,
		Type:      e.Type,
		CreatedAt: e.CreatedAt.UTC(),
		Data:      e.Data,
	}
}
