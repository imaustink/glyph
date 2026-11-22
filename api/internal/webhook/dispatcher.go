package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
)

// Retention is how long events are kept (and listed) before being purged.
const Retention = 30 * 24 * time.Hour

// MaxAttempts is how many times the dispatcher tries to deliver an event
// before marking it failed and moving on to the next one.
const MaxAttempts = 8

// retryBackoff[n-1] is the wait after the n-th failed attempt (~2h in all).
var retryBackoff = []time.Duration{
	10 * time.Second, 30 * time.Second, time.Minute, 5 * time.Minute,
	15 * time.Minute, 30 * time.Minute, time.Hour,
}

const (
	// DeliveryTimeout bounds one delivery attempt.
	DeliveryTimeout = 10 * time.Second
	leaseDuration   = 2 * time.Minute
	leaseBatch      = 10
	maxPerLease     = 50
	maxErrorLen     = 500
	maxResponseRead = 64 << 10
)

// NewHTTPClient returns the client deliveries are sent with. dial is the
// connection hook (pass an SSRF-guarded dialer in production). Redirects are
// not followed: a 3xx is recorded as the response.
func NewHTTPClient(dial func(ctx context.Context, network, addr string) (net.Conn, error)) *http.Client {
	return &http.Client{
		Timeout:   DeliveryTimeout,
		Transport: &http.Transport{DialContext: dial},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// Dispatcher delivers pending events. Each webhook's events are delivered one
// at a time in sequence order: an event that fails is retried with backoff
// before any later event is sent, until it succeeds or exhausts MaxAttempts
// (then it is marked failed and delivery moves on). Consumers that see a gap
// in sequence numbers recover the missing events from the feed endpoint.
//
// Several API replicas may run a Dispatcher: a webhook is leased to one
// replica at a time.
type Dispatcher struct {
	Store  store.WebhookStore
	Client *http.Client
	// PollInterval is how often due events are looked for (default 2s).
	PollInterval time.Duration
	// PurgeInterval is how often expired events are purged (default 1h).
	PurgeInterval time.Duration
	// Now is the clock (default time.Now).
	Now func() time.Time
}

func (d *Dispatcher) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

// Run delivers and purges until ctx is cancelled.
func (d *Dispatcher) Run(ctx context.Context) {
	poll := d.PollInterval
	if poll <= 0 {
		poll = 2 * time.Second
	}
	purgeEvery := d.PurgeInterval
	if purgeEvery <= 0 {
		purgeEvery = time.Hour
	}
	pollTicker := time.NewTicker(poll)
	defer pollTicker.Stop()
	purgeTicker := time.NewTicker(purgeEvery)
	defer purgeTicker.Stop()

	d.purge(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-pollTicker.C:
			if err := d.RunOnce(ctx); err != nil && ctx.Err() == nil {
				slog.Error("webhook dispatch failed", "error", err)
			}
		case <-purgeTicker.C:
			d.purge(ctx)
		}
	}
}

func (d *Dispatcher) purge(ctx context.Context) {
	n, err := d.Store.PurgeEventsBefore(ctx, d.now().Add(-Retention))
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("webhook event purge failed", "error", err)
		}
		return
	}
	if n > 0 {
		slog.Info("purged expired webhook events", "count", n)
	}
}

// RunOnce leases the webhooks with due events and delivers them, webhooks in
// parallel, each webhook's events in order.
func (d *Dispatcher) RunOnce(ctx context.Context) error {
	hooks, err := d.Store.LeaseDue(ctx, leaseBatch, leaseDuration)
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	for _, w := range hooks {
		wg.Add(1)
		go func(w *model.Webhook) {
			defer wg.Done()
			d.drain(ctx, w)
		}(w)
	}
	wg.Wait()
	return nil
}

func (d *Dispatcher) drain(ctx context.Context, w *model.Webhook) {
	defer func() {
		// Release even if ctx was cancelled, so another replica can pick the
		// webhook up straight away instead of waiting out the lease.
		relCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := d.Store.ReleaseLease(relCtx, w.ID); err != nil {
			slog.Error("webhook lease release failed", "webhook_id", w.ID, "error", err)
		}
	}()

	if err := d.Store.Sequence(ctx, w.ID); err != nil {
		slog.Error("webhook event sequencing failed", "webhook_id", w.ID, "error", err)
		return
	}
	// Stop with enough lease left for one more attempt to finish inside it.
	stopAt := d.now().Add(leaseDuration - DeliveryTimeout - 10*time.Second)
	for i := 0; i < maxPerLease && ctx.Err() == nil && d.now().Before(stopAt); i++ {
		e, err := d.Store.NextPending(ctx, w.ID)
		if errors.Is(err, store.ErrNotFound) {
			return
		}
		if err != nil {
			slog.Error("webhook next event lookup failed", "webhook_id", w.ID, "error", err)
			return
		}
		if e.NextAttemptAt.After(d.now()) {
			return // the head event is waiting out a retry backoff
		}
		res := d.Deliver(ctx, w, e)
		status, next := NextState(e.Attempts, res)
		if _, err := d.Store.RecordAttempt(ctx, e.ID, res, status, next); err != nil {
			slog.Error("webhook attempt record failed", "webhook_id", w.ID, "event_id", e.ID, "error", err)
			return
		}
		if status == model.WebhookEventPending {
			return // retry later, before anything after it
		}
	}
}

// NextState decides an event's status after an attempt, given how many
// attempts preceded it.
func NextState(priorAttempts int, res model.WebhookDeliveryResult) (model.WebhookEventStatus, time.Time) {
	if res.OK() {
		return model.WebhookEventDelivered, res.At
	}
	n := priorAttempts + 1
	if n >= MaxAttempts {
		return model.WebhookEventFailed, res.At
	}
	i := n - 1
	if i >= len(retryBackoff) {
		i = len(retryBackoff) - 1
	}
	return model.WebhookEventPending, res.At.Add(retryBackoff[i])
}

// Deliver POSTs e to w's URL once and reports the outcome. It does not record it.
func (d *Dispatcher) Deliver(ctx context.Context, w *model.Webhook, e *model.WebhookEvent) model.WebhookDeliveryResult {
	ts := d.now()
	res := model.WebhookDeliveryResult{At: ts}
	fail := func(msg string) model.WebhookDeliveryResult {
		if len(msg) > maxErrorLen {
			msg = msg[:maxErrorLen]
		}
		res.Error = &msg
		return res
	}

	body, err := json.Marshal(NewEnvelope(e))
	if err != nil {
		return fail("encoding event: " + err.Error())
	}
	ctx, cancel := context.WithTimeout(ctx, DeliveryTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.URL, bytes.NewReader(body))
	if err != nil {
		return fail("building request: " + err.Error())
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Glyph-Webhooks/1.0")
	req.Header.Set(HeaderEventID, e.ID.String())
	req.Header.Set(HeaderEventType, string(e.Type))
	if e.Seq != nil {
		req.Header.Set(HeaderSequence, strconv.FormatInt(*e.Seq, 10))
	}
	req.Header.Set(HeaderTimestamp, strconv.FormatInt(ts.Unix(), 10))
	req.Header.Set(HeaderSignature, Sign(w.Secret, ts, body))

	resp, err := d.Client.Do(req)
	if err != nil {
		return fail(err.Error())
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseRead))
	_ = resp.Body.Close()
	code := resp.StatusCode
	res.StatusCode = &code
	if code < 200 || code >= 300 {
		return fail(fmt.Sprintf("unexpected response status %d", code))
	}
	return res
}

// Replay delivers e again now, outside the dispatcher's queue, and records the
// attempt. Success marks the event delivered; a failure leaves its status
// (and, for a pending event, its retry schedule) as it was.
func (d *Dispatcher) Replay(ctx context.Context, w *model.Webhook, e *model.WebhookEvent) (*model.WebhookEvent, error) {
	if e.Seq == nil {
		if err := d.Store.Sequence(ctx, w.ID); err != nil {
			return nil, err
		}
		fresh, err := d.Store.GetEvent(ctx, w.ID, e.ID)
		if err != nil {
			return nil, err
		}
		e = fresh
	}
	res := d.Deliver(ctx, w, e)
	status := e.Status
	if res.OK() {
		status = model.WebhookEventDelivered
	}
	return d.Store.RecordAttempt(ctx, e.ID, res, status, e.NextAttemptAt)
}
