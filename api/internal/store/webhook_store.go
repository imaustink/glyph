package store

import (
	"context"
	"errors"
	"time"

	"github.com/glyph/api/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// WebhookPatch names the webhook fields an update changes (nil = unchanged).
type WebhookPatch struct {
	Name       *string
	URL        *string
	EventTypes []model.WebhookEventType // nil = unchanged
	Active     *bool
}

// WebhookStore persists webhooks and the events recorded for them. Events
// themselves are written by database triggers (migration 000027), not by
// this store.
type WebhookStore interface {
	Create(ctx context.Context, w *model.Webhook) (*model.Webhook, error)
	// Get returns the webhook if userID owns it (ErrNotFound otherwise).
	Get(ctx context.Context, id, userID uuid.UUID) (*model.Webhook, error)
	// GetByID returns the webhook regardless of owner, for request-signature
	// authentication by the consumer.
	GetByID(ctx context.Context, id uuid.UUID) (*model.Webhook, error)
	ListByUser(ctx context.Context, userID uuid.UUID) ([]*model.Webhook, error)
	Update(ctx context.Context, id, userID uuid.UUID, patch WebhookPatch) (*model.Webhook, error)
	SetSecret(ctx context.Context, id, userID uuid.UUID, secret string) (*model.Webhook, error)
	Delete(ctx context.Context, id, userID uuid.UUID) error

	// ListEvents returns the webhook's events created at or after since,
	// newest first, plus the total number of such events.
	ListEvents(ctx context.Context, webhookID uuid.UUID, since time.Time, limit, offset int) ([]*model.WebhookEvent, int, error)
	GetEvent(ctx context.Context, webhookID, eventID uuid.UUID) (*model.WebhookEvent, error)
	// Sequence assigns the next sequence numbers to the webhook's committed,
	// not-yet-sequenced events, in the order they were recorded.
	Sequence(ctx context.Context, webhookID uuid.UUID) error
	// ListEventsAfter returns sequenced events with seq > afterSeq, in order.
	ListEventsAfter(ctx context.Context, webhookID uuid.UUID, afterSeq int64, limit int) ([]*model.WebhookEvent, error)

	// LeaseDue leases up to limit active webhooks with a pending event due
	// for delivery and no live lease, for d. Release with ReleaseLease.
	LeaseDue(ctx context.Context, limit int, d time.Duration) ([]*model.Webhook, error)
	ReleaseLease(ctx context.Context, webhookID uuid.UUID) error
	// NextPending returns the webhook's lowest-sequence pending event
	// (ErrNotFound if none).
	NextPending(ctx context.Context, webhookID uuid.UUID) (*model.WebhookEvent, error)
	// RecordAttempt stores the outcome of a delivery attempt and moves the
	// event to status, next due at nextAttemptAt.
	RecordAttempt(ctx context.Context, eventID uuid.UUID, res model.WebhookDeliveryResult, status model.WebhookEventStatus, nextAttemptAt time.Time) (*model.WebhookEvent, error)
	// PurgeEventsBefore deletes events created before cutoff.
	PurgeEventsBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

type pgWebhookStore struct{ pool DBPool }

func NewWebhookStore(pool DBPool) WebhookStore {
	return &pgWebhookStore{pool: pool}
}

const webhookColumns = `id, user_id, org_id, name, url, secret, event_types, active, created_at, updated_at`

func scanWebhook(row pgx.Row) (*model.Webhook, error) {
	w := &model.Webhook{}
	var types []string
	if err := row.Scan(&w.ID, &w.UserID, &w.OrgID, &w.Name, &w.URL, &w.Secret, &types, &w.Active, &w.CreatedAt, &w.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	w.EventTypes = make([]model.WebhookEventType, len(types))
	for i, t := range types {
		w.EventTypes[i] = model.WebhookEventType(t)
	}
	return w, nil
}

func eventTypeStrings(types []model.WebhookEventType) []string {
	out := make([]string, len(types))
	for i, t := range types {
		out[i] = string(t)
	}
	return out
}

func (s *pgWebhookStore) Create(ctx context.Context, w *model.Webhook) (*model.Webhook, error) {
	return scanWebhook(s.pool.QueryRow(ctx, `
		INSERT INTO webhooks (user_id, org_id, name, url, secret, event_types, active)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+webhookColumns,
		w.UserID, w.OrgID, w.Name, w.URL, w.Secret, eventTypeStrings(w.EventTypes), w.Active))
}

func (s *pgWebhookStore) Get(ctx context.Context, id, userID uuid.UUID) (*model.Webhook, error) {
	return scanWebhook(s.pool.QueryRow(ctx,
		`SELECT `+webhookColumns+` FROM webhooks WHERE id = $1 AND user_id = $2`, id, userID))
}

func (s *pgWebhookStore) GetByID(ctx context.Context, id uuid.UUID) (*model.Webhook, error) {
	return scanWebhook(s.pool.QueryRow(ctx, `SELECT `+webhookColumns+` FROM webhooks WHERE id = $1`, id))
}

func (s *pgWebhookStore) ListByUser(ctx context.Context, userID uuid.UUID) ([]*model.Webhook, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+webhookColumns+` FROM webhooks WHERE user_id = $1 ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*model.Webhook{}
	for rows.Next() {
		w, err := scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *pgWebhookStore) Update(ctx context.Context, id, userID uuid.UUID, p WebhookPatch) (*model.Webhook, error) {
	var types []string
	if p.EventTypes != nil {
		types = eventTypeStrings(p.EventTypes)
	}
	return scanWebhook(s.pool.QueryRow(ctx, `
		UPDATE webhooks SET
			name        = COALESCE($3, name),
			url         = COALESCE($4, url),
			event_types = COALESCE($5, event_types),
			active      = COALESCE($6, active),
			updated_at  = NOW()
		WHERE id = $1 AND user_id = $2
		RETURNING `+webhookColumns,
		id, userID, p.Name, p.URL, types, p.Active))
}

func (s *pgWebhookStore) SetSecret(ctx context.Context, id, userID uuid.UUID, secret string) (*model.Webhook, error) {
	return scanWebhook(s.pool.QueryRow(ctx, `
		UPDATE webhooks SET secret = $3, updated_at = NOW()
		WHERE id = $1 AND user_id = $2
		RETURNING `+webhookColumns, id, userID, secret))
}

func (s *pgWebhookStore) Delete(ctx context.Context, id, userID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM webhooks WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ─── Events ──────────────────────────────────────────────────────────────────

const webhookEventColumns = `id, webhook_id, seq, event_type, resource_id, payload, status, attempts,
	last_status_code, last_error, last_attempt_at, next_attempt_at, delivered_at, created_at`

func scanWebhookEvent(row pgx.Row) (*model.WebhookEvent, error) {
	e := &model.WebhookEvent{}
	var data []byte
	if err := row.Scan(&e.ID, &e.WebhookID, &e.Seq, &e.Type, &e.ResourceID, &data, &e.Status, &e.Attempts,
		&e.LastStatusCode, &e.LastError, &e.LastAttemptAt, &e.NextAttemptAt, &e.DeliveredAt, &e.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	e.Data = data
	return e, nil
}

func collectWebhookEvents(rows pgx.Rows) ([]*model.WebhookEvent, error) {
	defer rows.Close()
	out := []*model.WebhookEvent{}
	for rows.Next() {
		e, err := scanWebhookEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *pgWebhookStore) ListEvents(ctx context.Context, webhookID uuid.UUID, since time.Time, limit, offset int) ([]*model.WebhookEvent, int, error) {
	var total int
	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM webhook_events WHERE webhook_id = $1 AND created_at >= $2`,
		webhookID, since).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+webhookEventColumns+` FROM webhook_events
		WHERE webhook_id = $1 AND created_at >= $2
		ORDER BY created_seq DESC
		LIMIT $3 OFFSET $4`, webhookID, since, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	events, err := collectWebhookEvents(rows)
	return events, total, err
}

func (s *pgWebhookStore) GetEvent(ctx context.Context, webhookID, eventID uuid.UUID) (*model.WebhookEvent, error) {
	return scanWebhookEvent(s.pool.QueryRow(ctx,
		`SELECT `+webhookEventColumns+` FROM webhook_events WHERE id = $1 AND webhook_id = $2`, eventID, webhookID))
}

// Sequence locks the webhook row first, in its own statement: under READ
// COMMITTED the numbering statement that follows then takes a snapshot that
// already sees everything a concurrent Sequence committed, so no event is
// numbered twice and the sequence stays gap-free.
func (s *pgWebhookStore) Sequence(ctx context.Context, webhookID uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var lastSeq int64
	if err := tx.QueryRow(ctx, `SELECT last_seq FROM webhooks WHERE id = $1 FOR UPDATE`, webhookID).Scan(&lastSeq); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	var maxSeq *int64
	if err := tx.QueryRow(ctx, `
		WITH todo AS (
			SELECT id, row_number() OVER (ORDER BY created_seq) AS rn
			FROM webhook_events
			WHERE webhook_id = $1 AND seq IS NULL
		), numbered AS (
			UPDATE webhook_events e SET seq = $2 + todo.rn
			FROM todo WHERE e.id = todo.id
			RETURNING e.seq
		)
		SELECT MAX(seq) FROM numbered`, webhookID, lastSeq).Scan(&maxSeq); err != nil {
		return err
	}
	if maxSeq == nil {
		return nil // nothing to number; the rollback just releases the lock
	}
	if _, err := tx.Exec(ctx, `UPDATE webhooks SET last_seq = $2 WHERE id = $1`, webhookID, *maxSeq); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *pgWebhookStore) ListEventsAfter(ctx context.Context, webhookID uuid.UUID, afterSeq int64, limit int) ([]*model.WebhookEvent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+webhookEventColumns+` FROM webhook_events
		WHERE webhook_id = $1 AND seq > $2
		ORDER BY seq
		LIMIT $3`, webhookID, afterSeq, limit)
	if err != nil {
		return nil, err
	}
	return collectWebhookEvents(rows)
}

// ─── Delivery ────────────────────────────────────────────────────────────────

func (s *pgWebhookStore) LeaseDue(ctx context.Context, limit int, d time.Duration) ([]*model.Webhook, error) {
	rows, err := s.pool.Query(ctx, `
		UPDATE webhooks SET lease_until = NOW() + make_interval(secs => $2)
		WHERE id IN (
			SELECT w.id FROM webhooks w
			WHERE w.active
			  AND (w.lease_until IS NULL OR w.lease_until < NOW())
			  AND EXISTS (SELECT 1 FROM webhook_events e
			              WHERE e.webhook_id = w.id AND e.status = 'pending' AND e.next_attempt_at <= NOW())
			ORDER BY w.id
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING `+webhookColumns, limit, d.Seconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*model.Webhook{}
	for rows.Next() {
		w, err := scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *pgWebhookStore) ReleaseLease(ctx context.Context, webhookID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE webhooks SET lease_until = NULL WHERE id = $1`, webhookID)
	return err
}

func (s *pgWebhookStore) NextPending(ctx context.Context, webhookID uuid.UUID) (*model.WebhookEvent, error) {
	return scanWebhookEvent(s.pool.QueryRow(ctx, `
		SELECT `+webhookEventColumns+` FROM webhook_events
		WHERE webhook_id = $1 AND status = 'pending' AND seq IS NOT NULL
		ORDER BY seq
		LIMIT 1`, webhookID))
}

func (s *pgWebhookStore) RecordAttempt(ctx context.Context, eventID uuid.UUID, res model.WebhookDeliveryResult, status model.WebhookEventStatus, nextAttemptAt time.Time) (*model.WebhookEvent, error) {
	return scanWebhookEvent(s.pool.QueryRow(ctx, `
		UPDATE webhook_events SET
			attempts         = attempts + 1,
			last_status_code = $2,
			last_error       = $3,
			last_attempt_at  = $4,
			status           = $5,
			next_attempt_at  = $6,
			delivered_at     = CASE WHEN $7 THEN $4 ELSE delivered_at END
		WHERE id = $1
		RETURNING `+webhookEventColumns,
		eventID, res.StatusCode, res.Error, res.At, string(status), nextAttemptAt, res.OK()))
}

// PurgeEventsBefore deletes in batches so a large backlog never holds one
// long-running delete.
func (s *pgWebhookStore) PurgeEventsBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	var total int64
	for {
		tag, err := s.pool.Exec(ctx, `
			DELETE FROM webhook_events WHERE id IN (
				SELECT id FROM webhook_events WHERE created_at < $1 LIMIT 5000
			)`, cutoff)
		if err != nil {
			return total, err
		}
		total += tag.RowsAffected()
		if tag.RowsAffected() < 5000 {
			return total, nil
		}
	}
}
