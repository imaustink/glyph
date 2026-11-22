package webhook

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Signatures ──────────────────────────────────────────────────────────────

func TestSignVerify(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	body := []byte(`{"hello":"world"}`)
	sig := Sign("whsec_test", now, body)
	ts := strconv.FormatInt(now.Unix(), 10)

	require.NoError(t, Verify("whsec_test", ts, sig, body, now))
	require.NoError(t, Verify("whsec_test", ts, "v1=deadbeef, "+sig, body, now), "any listed signature may match")
	require.NoError(t, Verify("whsec_test", ts, sig, body, now.Add(4*time.Minute)))

	assert.ErrorIs(t, Verify("other", ts, sig, body, now), ErrBadSignature, "wrong secret")
	assert.ErrorIs(t, Verify("whsec_test", ts, sig, []byte(`{}`), now), ErrBadSignature, "tampered body")
	assert.ErrorIs(t, Verify("whsec_test", ts, sig, body, now.Add(6*time.Minute)), ErrBadSignature, "stale")
	assert.ErrorIs(t, Verify("whsec_test", ts, sig, body, now.Add(-6*time.Minute)), ErrBadSignature, "future")
	assert.ErrorIs(t, Verify("whsec_test", "nope", sig, body, now), ErrBadSignature, "bad timestamp")
	assert.ErrorIs(t, Verify("whsec_test", ts, "", body, now), ErrBadSignature, "missing signature")
}

func TestSignKnownVector(t *testing.T) {
	// HMAC-SHA256("secret", "1700000000.payload"), so consumers can check
	// their implementation against a fixed value.
	got := Sign("secret", time.Unix(1_700_000_000, 0), []byte("payload"))
	assert.Equal(t, "v1=5af4877ab3c93d3201223b2c43d689a4c1e849ddd9091e066f03be6168ae79e9", got)
}

func TestNewSecret(t *testing.T) {
	a, err := NewSecret()
	require.NoError(t, err)
	b, err := NewSecret()
	require.NoError(t, err)
	assert.NotEqual(t, a, b)
	assert.Regexp(t, `^whsec_[A-Za-z0-9_-]{43}$`, a)
}

// ─── NextState ───────────────────────────────────────────────────────────────

func code(n int) *int      { return &n }
func str(s string) *string { return &s }

func TestNextState(t *testing.T) {
	at := time.Unix(1_800_000_000, 0)
	ok := model.WebhookDeliveryResult{StatusCode: code(204), At: at}
	bad := model.WebhookDeliveryResult{StatusCode: code(500), Error: str("unexpected response status 500"), At: at}

	s, next := NextState(3, ok)
	assert.Equal(t, model.WebhookEventDelivered, s)
	assert.Equal(t, at, next)

	s, next = NextState(0, bad)
	assert.Equal(t, model.WebhookEventPending, s)
	assert.Equal(t, at.Add(10*time.Second), next)

	s, next = NextState(MaxAttempts-2, bad)
	assert.Equal(t, model.WebhookEventPending, s)
	assert.Equal(t, at.Add(time.Hour), next)

	s, _ = NextState(MaxAttempts-1, bad)
	assert.Equal(t, model.WebhookEventFailed, s, "the last attempt failing gives up")
}

// ─── Deliver ─────────────────────────────────────────────────────────────────

func plainClient() *http.Client {
	return NewHTTPClient((&net.Dialer{Timeout: time.Second}).DialContext)
}

func testEvent(webhookID uuid.UUID, seq int64) *model.WebhookEvent {
	s := seq
	return &model.WebhookEvent{
		ID:            uuid.New(),
		WebhookID:     webhookID,
		Seq:           &s,
		Type:          model.WebhookTaskCreated,
		ResourceID:    uuid.New(),
		Data:          json.RawMessage(`{"id":"x","title":"Buy milk"}`),
		Status:        model.WebhookEventPending,
		NextAttemptAt: time.Unix(0, 0),
		CreatedAt:     time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC),
	}
}

func TestDeliver_SignsAndSendsEnvelope(t *testing.T) {
	var got *http.Request
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	now := time.Unix(1_800_000_000, 0)
	d := &Dispatcher{Client: plainClient(), Now: func() time.Time { return now }}
	w := &model.Webhook{ID: uuid.New(), URL: srv.URL + "/hook", Secret: "whsec_abc"}
	e := testEvent(w.ID, 7)

	res := d.Deliver(context.Background(), w, e)
	require.True(t, res.OK(), "error: %v", res.Error)
	assert.Equal(t, 202, *res.StatusCode)

	assert.Equal(t, http.MethodPost, got.Method)
	assert.Equal(t, "/hook", got.URL.Path)
	assert.Equal(t, "application/json", got.Header.Get("Content-Type"))
	assert.Equal(t, e.ID.String(), got.Header.Get(HeaderEventID))
	assert.Equal(t, "task.created", got.Header.Get(HeaderEventType))
	assert.Equal(t, "7", got.Header.Get(HeaderSequence))
	require.NoError(t, Verify("whsec_abc", got.Header.Get(HeaderTimestamp), got.Header.Get(HeaderSignature), gotBody, now))

	var env map[string]any
	require.NoError(t, json.Unmarshal(gotBody, &env))
	assert.Equal(t, e.ID.String(), env["id"])
	assert.Equal(t, w.ID.String(), env["webhookId"])
	assert.Equal(t, float64(7), env["sequence"])
	assert.Equal(t, "task.created", env["type"])
	assert.Equal(t, "2026-11-01T12:00:00Z", env["createdAt"])
	assert.Equal(t, map[string]any{"id": "x", "title": "Buy milk"}, env["data"])
}

func TestDeliver_Failures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/ok", http.StatusFound)
			return
		}
		if r.URL.Path == "/ok" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	d := &Dispatcher{Client: plainClient()}

	t.Run("non-2xx", func(t *testing.T) {
		w := &model.Webhook{ID: uuid.New(), URL: srv.URL + "/down", Secret: "s"}
		res := d.Deliver(context.Background(), w, testEvent(w.ID, 1))
		assert.False(t, res.OK())
		assert.Equal(t, 503, *res.StatusCode)
		assert.Equal(t, "unexpected response status 503", *res.Error)
	})
	t.Run("redirects are not followed", func(t *testing.T) {
		w := &model.Webhook{ID: uuid.New(), URL: srv.URL + "/redirect", Secret: "s"}
		res := d.Deliver(context.Background(), w, testEvent(w.ID, 1))
		assert.False(t, res.OK())
		assert.Equal(t, 302, *res.StatusCode)
	})
	t.Run("connection error", func(t *testing.T) {
		dead := httptest.NewServer(http.NotFoundHandler())
		dead.Close()
		w := &model.Webhook{ID: uuid.New(), URL: dead.URL, Secret: "s"}
		res := d.Deliver(context.Background(), w, testEvent(w.ID, 1))
		assert.False(t, res.OK())
		assert.Nil(t, res.StatusCode)
		require.NotNil(t, res.Error)
	})
}

// ─── Dispatcher ──────────────────────────────────────────────────────────────

// fakeStore implements the delivery half of store.WebhookStore in memory.
type fakeStore struct {
	store.WebhookStore // unused methods panic
	mu                 sync.Mutex
	hooks              map[uuid.UUID]*model.Webhook
	events             []*model.WebhookEvent
	released           []uuid.UUID
	purgedBefore       time.Time
	now                func() time.Time
}

func (f *fakeStore) LeaseDue(_ context.Context, limit int, _ time.Duration) ([]*model.Webhook, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*model.Webhook
	for _, w := range f.hooks {
		for _, e := range f.events {
			if e.WebhookID == w.ID && e.Status == model.WebhookEventPending && !e.NextAttemptAt.After(f.now()) {
				out = append(out, w)
				break
			}
		}
	}
	return out, nil
}

func (f *fakeStore) ReleaseLease(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.released = append(f.released, id)
	return nil
}

func (f *fakeStore) Sequence(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	var max int64
	for _, e := range f.events {
		if e.WebhookID == id && e.Seq != nil && *e.Seq > max {
			max = *e.Seq
		}
	}
	for _, e := range f.events {
		if e.WebhookID == id && e.Seq == nil {
			max++
			s := max
			e.Seq = &s
		}
	}
	return nil
}

func (f *fakeStore) NextPending(_ context.Context, id uuid.UUID) (*model.WebhookEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var pending []*model.WebhookEvent
	for _, e := range f.events {
		if e.WebhookID == id && e.Status == model.WebhookEventPending && e.Seq != nil {
			pending = append(pending, e)
		}
	}
	if len(pending) == 0 {
		return nil, store.ErrNotFound
	}
	sort.Slice(pending, func(i, j int) bool { return *pending[i].Seq < *pending[j].Seq })
	cp := *pending[0]
	return &cp, nil
}

func (f *fakeStore) GetEvent(_ context.Context, _, id uuid.UUID) (*model.WebhookEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.events {
		if e.ID == id {
			cp := *e
			return &cp, nil
		}
	}
	return nil, store.ErrNotFound
}

func (f *fakeStore) RecordAttempt(_ context.Context, id uuid.UUID, res model.WebhookDeliveryResult, status model.WebhookEventStatus, next time.Time) (*model.WebhookEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.events {
		if e.ID == id {
			e.Attempts++
			e.LastStatusCode = res.StatusCode
			e.LastError = res.Error
			at := res.At
			e.LastAttemptAt = &at
			e.Status = status
			e.NextAttemptAt = next
			if res.OK() {
				e.DeliveredAt = &at
			}
			cp := *e
			return &cp, nil
		}
	}
	return nil, store.ErrNotFound
}

func (f *fakeStore) PurgeEventsBefore(_ context.Context, cutoff time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.purgedBefore = cutoff
	return 0, nil
}

// receiver records the sequence numbers it was sent and answers with the
// next status from codes (200 once exhausted).
type receiver struct {
	mu    sync.Mutex
	seqs  []string
	codes []int
}

func (rc *receiver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.seqs = append(rc.seqs, r.Header.Get(HeaderSequence))
	c := http.StatusOK
	if len(rc.codes) > 0 {
		c, rc.codes = rc.codes[0], rc.codes[1:]
	}
	w.WriteHeader(c)
}

func newDispatchFixture(t *testing.T, codes []int, n int) (*Dispatcher, *fakeStore, *receiver, *time.Time) {
	t.Helper()
	rc := &receiver{codes: codes}
	srv := httptest.NewServer(rc)
	t.Cleanup(srv.Close)
	now := time.Unix(1_800_000_000, 0)
	clock := func() time.Time { return now }
	w := &model.Webhook{ID: uuid.New(), URL: srv.URL, Secret: "s", Active: true}
	fs := &fakeStore{hooks: map[uuid.UUID]*model.Webhook{w.ID: w}, now: clock}
	for i := 0; i < n; i++ {
		e := testEvent(w.ID, 0)
		e.Seq = nil // unsequenced, as recorded by the trigger
		fs.events = append(fs.events, e)
	}
	return &Dispatcher{Store: fs, Client: plainClient(), Now: clock}, fs, rc, &now
}

func TestDispatcher_DeliversInSequenceOrder(t *testing.T) {
	d, fs, rc, _ := newDispatchFixture(t, nil, 3)
	require.NoError(t, d.RunOnce(context.Background()))

	assert.Equal(t, []string{"1", "2", "3"}, rc.seqs)
	for _, e := range fs.events {
		assert.Equal(t, model.WebhookEventDelivered, e.Status)
		assert.Equal(t, 200, *e.LastStatusCode)
		assert.Equal(t, 1, e.Attempts)
	}
	assert.Len(t, fs.released, 1, "the lease is released")
}

func TestDispatcher_FailureBlocksLaterEventsUntilRetried(t *testing.T) {
	d, fs, rc, now := newDispatchFixture(t, []int{500}, 2)

	require.NoError(t, d.RunOnce(context.Background()))
	assert.Equal(t, []string{"1"}, rc.seqs, "event 2 waits for event 1")
	assert.Equal(t, model.WebhookEventPending, fs.events[0].Status)
	assert.Equal(t, 500, *fs.events[0].LastStatusCode)
	assert.Equal(t, now.Add(10*time.Second), fs.events[0].NextAttemptAt)

	require.NoError(t, d.RunOnce(context.Background()))
	assert.Equal(t, []string{"1"}, rc.seqs, "nothing is sent during the backoff")

	*now = now.Add(11 * time.Second)
	require.NoError(t, d.RunOnce(context.Background()))
	assert.Equal(t, []string{"1", "1", "2"}, rc.seqs)
	assert.Equal(t, model.WebhookEventDelivered, fs.events[0].Status)
	assert.Equal(t, 2, fs.events[0].Attempts)
	assert.Equal(t, model.WebhookEventDelivered, fs.events[1].Status)
}

func TestDispatcher_GivesUpAfterMaxAttemptsAndMovesOn(t *testing.T) {
	d, fs, rc, _ := newDispatchFixture(t, nil, 2)
	fs.events[0].Attempts = MaxAttempts - 1
	rc.codes = []int{410}

	require.NoError(t, d.RunOnce(context.Background()))
	assert.Equal(t, []string{"1", "2"}, rc.seqs)
	assert.Equal(t, model.WebhookEventFailed, fs.events[0].Status)
	assert.Equal(t, 410, *fs.events[0].LastStatusCode)
	assert.Equal(t, model.WebhookEventDelivered, fs.events[1].Status)
}

func TestDispatcher_Replay(t *testing.T) {
	d, fs, rc, _ := newDispatchFixture(t, nil, 1)
	w := fs.hooks[fs.events[0].WebhookID]
	fs.events[0].Status = model.WebhookEventFailed
	fs.events[0].Attempts = MaxAttempts

	rc.codes = []int{500}
	got, err := d.Replay(context.Background(), w, fs.events[0])
	require.NoError(t, err)
	assert.Equal(t, model.WebhookEventFailed, got.Status, "a failed replay leaves the status alone")
	assert.Equal(t, 500, *got.LastStatusCode)
	require.NotNil(t, got.Seq, "an unsequenced event is sequenced before it is sent")

	got, err = d.Replay(context.Background(), w, got)
	require.NoError(t, err)
	assert.Equal(t, model.WebhookEventDelivered, got.Status)
	assert.Equal(t, 200, *got.LastStatusCode)
	assert.Equal(t, MaxAttempts+2, got.Attempts)
	assert.Equal(t, []string{"1", "1"}, rc.seqs)
}

func TestDispatcher_RunPurgesExpiredEvents(t *testing.T) {
	d, fs, _, now := newDispatchFixture(t, nil, 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d.Run(ctx) // purges once on start, then sees the cancelled context
	assert.Equal(t, now.Add(-Retention), fs.purgedBefore)
}
