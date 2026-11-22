package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/auth"
	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
	"github.com/glyph/api/internal/store/memstore"
	"github.com/glyph/api/internal/webhook"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeWebhookStore is an in-memory store.WebhookStore for handler tests.
type fakeWebhookStore struct {
	store.WebhookStore // delivery methods are unused here
	mu                 sync.Mutex
	hooks              map[uuid.UUID]*model.Webhook
	events             []*model.WebhookEvent
	sequenced          int
}

func newFakeWebhookStore() *fakeWebhookStore {
	return &fakeWebhookStore{hooks: map[uuid.UUID]*model.Webhook{}}
}

func (f *fakeWebhookStore) Create(_ context.Context, w *model.Webhook) (*model.Webhook, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *w
	cp.ID = uuid.New()
	f.hooks[cp.ID] = &cp
	out := cp
	return &out, nil
}

func (f *fakeWebhookStore) Get(_ context.Context, id, userID uuid.UUID) (*model.Webhook, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w, ok := f.hooks[id]
	if !ok || w.UserID != userID {
		return nil, store.ErrNotFound
	}
	cp := *w
	return &cp, nil
}

func (f *fakeWebhookStore) GetByID(_ context.Context, id uuid.UUID) (*model.Webhook, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w, ok := f.hooks[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	cp := *w
	return &cp, nil
}

func (f *fakeWebhookStore) ListByUser(_ context.Context, userID uuid.UUID) ([]*model.Webhook, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []*model.Webhook{}
	for _, w := range f.hooks {
		if w.UserID == userID {
			cp := *w
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (f *fakeWebhookStore) Update(_ context.Context, id, userID uuid.UUID, p store.WebhookPatch) (*model.Webhook, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w, ok := f.hooks[id]
	if !ok || w.UserID != userID {
		return nil, store.ErrNotFound
	}
	if p.Name != nil {
		w.Name = *p.Name
	}
	if p.URL != nil {
		w.URL = *p.URL
	}
	if p.EventTypes != nil {
		w.EventTypes = p.EventTypes
	}
	if p.Active != nil {
		w.Active = *p.Active
	}
	cp := *w
	return &cp, nil
}

func (f *fakeWebhookStore) SetSecret(_ context.Context, id, userID uuid.UUID, secret string) (*model.Webhook, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w, ok := f.hooks[id]
	if !ok || w.UserID != userID {
		return nil, store.ErrNotFound
	}
	w.Secret = secret
	cp := *w
	return &cp, nil
}

func (f *fakeWebhookStore) Delete(_ context.Context, id, userID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	w, ok := f.hooks[id]
	if !ok || w.UserID != userID {
		return store.ErrNotFound
	}
	delete(f.hooks, id)
	return nil
}

func (f *fakeWebhookStore) ListEvents(_ context.Context, id uuid.UUID, since time.Time, limit, offset int) ([]*model.WebhookEvent, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var all []*model.WebhookEvent
	for i := len(f.events) - 1; i >= 0; i-- {
		if e := f.events[i]; e.WebhookID == id && !e.CreatedAt.Before(since) {
			all = append(all, e)
		}
	}
	total := len(all)
	if offset > len(all) {
		offset = len(all)
	}
	all = all[offset:]
	if len(all) > limit {
		all = all[:limit]
	}
	return all, total, nil
}

func (f *fakeWebhookStore) GetEvent(_ context.Context, webhookID, eventID uuid.UUID) (*model.WebhookEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.events {
		if e.ID == eventID && e.WebhookID == webhookID {
			cp := *e
			return &cp, nil
		}
	}
	return nil, store.ErrNotFound
}

func (f *fakeWebhookStore) Sequence(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sequenced++
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

func (f *fakeWebhookStore) ListEventsAfter(_ context.Context, id uuid.UUID, after int64, limit int) ([]*model.WebhookEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []*model.WebhookEvent{}
	for _, e := range f.events {
		if e.WebhookID == id && e.Seq != nil && *e.Seq > after && len(out) < limit {
			out = append(out, e)
		}
	}
	return out, nil
}

type fakeReplayer struct {
	got *model.WebhookEvent
}

func (r *fakeReplayer) Replay(_ context.Context, _ *model.Webhook, e *model.WebhookEvent) (*model.WebhookEvent, error) {
	r.got = e
	cp := *e
	c := 200
	cp.LastStatusCode = &c
	cp.Status = model.WebhookEventDelivered
	return &cp, nil
}

type webhookEnv struct {
	t        *testing.T
	router   *gin.Engine
	store    *fakeWebhookStore
	replayer *fakeReplayer
	orgs     store.OrgStore
	alice    *model.User
	bob      *model.User
	now      time.Time
}

func newWebhookEnv(t *testing.T) *webhookEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	orig := isPrivateHostCheck
	isPrivateHostCheck = func(h string) bool { return h == "internal.example" }
	t.Cleanup(func() { isPrivateHostCheck = orig })

	_, _, _, _, _, orgs, _ := memstore.NewStores()
	env := &webhookEnv{
		t:        t,
		store:    newFakeWebhookStore(),
		replayer: &fakeReplayer{},
		orgs:     orgs,
		alice:    &model.User{ID: uuid.New()},
		bob:      &model.User{ID: uuid.New()},
		now:      time.Unix(1_800_000_000, 0),
	}
	h := &WebhookHandler{Webhooks: env.store, Orgs: orgs, Replayer: env.replayer, Now: func() time.Time { return env.now }}

	r := gin.New()
	api := r.Group("/api/v1", func(c *gin.Context) {
		switch c.GetHeader("X-User") {
		case "alice":
			c.Set(auth.ContextKey, env.alice)
		case "bob":
			c.Set(auth.ContextKey, env.bob)
		}
		if c.GetHeader("X-Bearer") != "" {
			c.Set(model.TokenScopeContextKey, &model.TokenScope{})
		}
		c.Next()
	})
	api.GET("/webhooks", h.ListWebhooks)
	api.POST("/webhooks", h.CreateWebhook)
	api.GET("/webhooks/event-types", h.ListEventTypes)
	api.GET("/webhooks/:id", h.GetWebhook)
	api.PATCH("/webhooks/:id", h.UpdateWebhook)
	api.DELETE("/webhooks/:id", h.DeleteWebhook)
	api.POST("/webhooks/:id/rotate-secret", h.RotateSecret)
	api.GET("/webhooks/:id/events", h.ListEvents)
	api.POST("/webhooks/:id/events/:eventId/replay", h.ReplayEvent)
	r.GET("/api/v1/webhooks/:id/feed", h.Feed)
	env.router = r
	return env
}

func (e *webhookEnv) do(user, method, path string, body any, headers ...string) *httptest.ResponseRecorder {
	e.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(e.t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User", user)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	return w
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &v), w.Body.String())
	return v
}

func (e *webhookEnv) create(body map[string]any) model.WebhookWithSecret {
	e.t.Helper()
	w := e.do("alice", "POST", "/api/v1/webhooks", body)
	require.Equal(e.t, http.StatusCreated, w.Code, w.Body.String())
	return decode[model.WebhookWithSecret](e.t, w)
}

func validHook() map[string]any {
	return map[string]any{
		"name":       "Zapier",
		"url":        "https://hooks.example.com/glyph",
		"eventTypes": []string{"task.deleted", "task.created", "task.created"},
	}
}

func TestWebhookHandler_CreateAndManage(t *testing.T) {
	env := newWebhookEnv(t)
	created := env.create(validHook())
	assert.Regexp(t, `^whsec_`, created.Secret)
	assert.Equal(t, env.alice.ID, created.UserID)
	assert.True(t, created.Active)
	assert.Nil(t, created.OrgID)
	assert.Equal(t, []model.WebhookEventType{model.WebhookTaskCreated, model.WebhookTaskDeleted}, created.EventTypes,
		"event types are de-duplicated into canonical order")

	// The secret is only shown on create/rotate.
	w := env.do("alice", "GET", "/api/v1/webhooks/"+created.ID.String(), nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.NotContains(t, w.Body.String(), created.Secret)
	assert.NotContains(t, w.Body.String(), `"secret"`)
	w = env.do("alice", "GET", "/api/v1/webhooks", nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Len(t, decode[[]model.Webhook](t, w), 1)
	assert.NotContains(t, w.Body.String(), created.Secret)

	// Other users can't see or touch it.
	assert.Equal(t, http.StatusNotFound, env.do("bob", "GET", "/api/v1/webhooks/"+created.ID.String(), nil).Code)
	assert.Equal(t, http.StatusNotFound, env.do("bob", "DELETE", "/api/v1/webhooks/"+created.ID.String(), nil).Code)
	assert.Empty(t, decode[[]model.Webhook](t, env.do("bob", "GET", "/api/v1/webhooks", nil)))

	// Update.
	w = env.do("alice", "PATCH", "/api/v1/webhooks/"+created.ID.String(), map[string]any{
		"active": false, "eventTypes": []string{"note.updated"},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	updated := decode[model.Webhook](t, w)
	assert.False(t, updated.Active)
	assert.Equal(t, []model.WebhookEventType{model.WebhookNoteUpdated}, updated.EventTypes)
	assert.Equal(t, "https://hooks.example.com/glyph", updated.URL)
	assert.Equal(t, http.StatusBadRequest, env.do("alice", "PATCH", "/api/v1/webhooks/"+created.ID.String(),
		map[string]any{"eventTypes": []string{}}).Code)
	assert.Equal(t, http.StatusBadRequest, env.do("alice", "PATCH", "/api/v1/webhooks/"+created.ID.String(),
		map[string]any{"url": "ftp://x"}).Code)

	// Rotate.
	w = env.do("alice", "POST", "/api/v1/webhooks/"+created.ID.String()+"/rotate-secret", nil)
	require.Equal(t, http.StatusOK, w.Code)
	rotated := decode[model.WebhookWithSecret](t, w)
	assert.NotEqual(t, created.Secret, rotated.Secret)

	// Delete.
	assert.Equal(t, http.StatusNoContent, env.do("alice", "DELETE", "/api/v1/webhooks/"+created.ID.String(), nil).Code)
	assert.Equal(t, http.StatusNotFound, env.do("alice", "GET", "/api/v1/webhooks/"+created.ID.String(), nil).Code)
}

func TestWebhookHandler_CreateValidation(t *testing.T) {
	env := newWebhookEnv(t)
	cases := map[string]func(b map[string]any){
		"no event types":     func(b map[string]any) { b["eventTypes"] = []string{} },
		"unknown event type": func(b map[string]any) { b["eventTypes"] = []string{"lane.created"} },
		"missing url":        func(b map[string]any) { delete(b, "url") },
		"relative url":       func(b map[string]any) { b["url"] = "/hook" },
		"non-http scheme":    func(b map[string]any) { b["url"] = "file:///etc/passwd" },
		"credentials in url": func(b map[string]any) { b["url"] = "https://u:p@hooks.example.com/" },
		"private address":    func(b map[string]any) { b["url"] = "https://internal.example/hook" },
		"non-standard port":  func(b map[string]any) { b["url"] = "https://hooks.example.com:8443/hook" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			b := validHook()
			mutate(b)
			assert.Equal(t, http.StatusBadRequest, env.do("alice", "POST", "/api/v1/webhooks", b).Code)
		})
	}

	t.Run("private URLs allowed when configured", func(t *testing.T) {
		h := &WebhookHandler{AllowPrivateURLs: true}
		assert.Empty(t, h.validateWebhookURL("http://internal.example:3000/hook"))
	})
}

func TestWebhookHandler_OrgWebhooksRequireOrgOwner(t *testing.T) {
	env := newWebhookEnv(t)
	ctx := context.Background()
	org, err := env.orgs.Create(ctx, &model.Organization{Name: "Acme", CreatedBy: env.alice.ID})
	require.NoError(t, err)
	_, err = env.orgs.AddMember(ctx, org.ID, env.alice.ID, model.OrgRoleOwner)
	require.NoError(t, err)
	_, err = env.orgs.AddMember(ctx, org.ID, env.bob.ID, model.OrgRoleEditor)
	require.NoError(t, err)

	b := validHook()
	b["orgId"] = org.ID.String()
	assert.Equal(t, http.StatusForbidden, env.do("bob", "POST", "/api/v1/webhooks", b).Code)
	created := env.create(b)
	require.NotNil(t, created.OrgID)
	assert.Equal(t, org.ID, *created.OrgID)

	b["orgId"] = uuid.NewString()
	assert.Equal(t, http.StatusNotFound, env.do("alice", "POST", "/api/v1/webhooks", b).Code)
}

func TestWebhookHandler_RejectsBearerTokens(t *testing.T) {
	env := newWebhookEnv(t)
	created := env.create(validHook())
	for _, r := range [][2]string{
		{"GET", "/api/v1/webhooks"},
		{"POST", "/api/v1/webhooks"},
		{"GET", "/api/v1/webhooks/" + created.ID.String()},
		{"PATCH", "/api/v1/webhooks/" + created.ID.String()},
		{"DELETE", "/api/v1/webhooks/" + created.ID.String()},
		{"POST", "/api/v1/webhooks/" + created.ID.String() + "/rotate-secret"},
		{"GET", "/api/v1/webhooks/" + created.ID.String() + "/events"},
	} {
		w := env.do("alice", r[0], r[1], validHook(), "X-Bearer", "1")
		assert.Equal(t, http.StatusForbidden, w.Code, "%s %s", r[0], r[1])
	}
}

func (e *webhookEnv) addEvent(hookID uuid.UUID, seq *int64, age time.Duration) *model.WebhookEvent {
	ev := &model.WebhookEvent{
		ID: uuid.New(), WebhookID: hookID, Seq: seq, Type: model.WebhookTaskCreated,
		ResourceID: uuid.New(), Data: json.RawMessage(`{"title":"t"}`),
		Status: model.WebhookEventPending, CreatedAt: e.now.Add(-age),
	}
	e.store.events = append(e.store.events, ev)
	return ev
}

func TestWebhookHandler_EventsAndReplay(t *testing.T) {
	env := newWebhookEnv(t)
	created := env.create(validHook())
	env.addEvent(created.ID, nil, 31*24*time.Hour) // outside retention
	older := env.addEvent(created.ID, nil, time.Hour)
	newer := env.addEvent(created.ID, nil, time.Minute)

	w := env.do("alice", "GET", "/api/v1/webhooks/"+created.ID.String()+"/events", nil)
	require.Equal(t, http.StatusOK, w.Code)
	page := decode[struct {
		Events        []model.WebhookEvent `json:"events"`
		Total         int                  `json:"total"`
		RetentionDays int                  `json:"retentionDays"`
	}](t, w)
	assert.Equal(t, 2, page.Total)
	assert.Equal(t, 30, page.RetentionDays)
	require.Len(t, page.Events, 2)
	assert.Equal(t, newer.ID, page.Events[0].ID, "newest first")
	assert.Equal(t, older.ID, page.Events[1].ID)

	assert.Equal(t, http.StatusBadRequest, env.do("alice", "GET", "/api/v1/webhooks/"+created.ID.String()+"/events?limit=0", nil).Code)
	assert.Equal(t, http.StatusNotFound, env.do("bob", "GET", "/api/v1/webhooks/"+created.ID.String()+"/events", nil).Code)

	// A pending event is still queued for the dispatcher: replaying it would
	// break sequence order, so it is refused without touching the replayer.
	w = env.do("alice", "POST", "/api/v1/webhooks/"+created.ID.String()+"/events/"+newer.ID.String()+"/replay", nil)
	assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Nil(t, env.replayer.got)

	older.Status = model.WebhookEventFailed
	w = env.do("alice", "POST", "/api/v1/webhooks/"+created.ID.String()+"/events/"+older.ID.String()+"/replay", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, older.ID, env.replayer.got.ID)
	assert.Equal(t, model.WebhookEventDelivered, decode[model.WebhookEvent](t, w).Status)

	newer.Status = model.WebhookEventDelivered
	w = env.do("alice", "POST", "/api/v1/webhooks/"+created.ID.String()+"/events/"+newer.ID.String()+"/replay", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, newer.ID, env.replayer.got.ID)

	assert.Equal(t, http.StatusNotFound, env.do("bob", "POST",
		"/api/v1/webhooks/"+created.ID.String()+"/events/"+older.ID.String()+"/replay", nil).Code)
	assert.Equal(t, http.StatusNotFound, env.do("alice", "POST",
		"/api/v1/webhooks/"+created.ID.String()+"/events/"+uuid.NewString()+"/replay", nil).Code)
}

func (e *webhookEnv) feed(hook model.WebhookWithSecret, query, secret string, ts time.Time) *httptest.ResponseRecorder {
	path := "/api/v1/webhooks/" + hook.ID.String() + "/feed" + query
	return e.do("", "GET", path, nil,
		webhook.HeaderTimestamp, strconv.FormatInt(ts.Unix(), 10),
		webhook.HeaderSignature, webhook.Sign(secret, ts, []byte(path)))
}

func TestWebhookHandler_Feed(t *testing.T) {
	env := newWebhookEnv(t)
	hook := env.create(validHook())
	for i := 0; i < 5; i++ {
		env.addEvent(hook.ID, nil, time.Duration(5-i)*time.Minute)
	}

	type feedPage struct {
		Events       []webhook.Envelope `json:"events"`
		HasMore      bool               `json:"hasMore"`
		LastSequence int64              `json:"lastSequence"`
	}

	w := env.feed(hook, "?after=1&limit=2", hook.Secret, env.now)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, 1, env.store.sequenced, "unsequenced events are numbered before the feed is read")
	p := decode[feedPage](t, w)
	require.Len(t, p.Events, 2)
	assert.Equal(t, int64(2), *p.Events[0].Sequence)
	assert.Equal(t, int64(3), *p.Events[1].Sequence)
	assert.Equal(t, hook.ID, p.Events[0].WebhookID)
	assert.True(t, p.HasMore)
	assert.Equal(t, int64(3), p.LastSequence)

	p = decode[feedPage](t, env.feed(hook, "?after=3", hook.Secret, env.now))
	assert.Len(t, p.Events, 2)
	assert.False(t, p.HasMore)
	assert.Equal(t, int64(5), p.LastSequence)

	p = decode[feedPage](t, env.feed(hook, "?after=5", hook.Secret, env.now))
	assert.Empty(t, p.Events)
	assert.Equal(t, int64(5), p.LastSequence, "an empty page echoes the cursor")

	// Authentication.
	assert.Equal(t, http.StatusUnauthorized, env.feed(hook, "", "wrong", env.now).Code)
	assert.Equal(t, http.StatusUnauthorized, env.feed(hook, "", hook.Secret, env.now.Add(-10*time.Minute)).Code, "stale")
	assert.Equal(t, http.StatusUnauthorized, env.do("", "GET", "/api/v1/webhooks/"+hook.ID.String()+"/feed", nil).Code, "unsigned")
	// A signature over a different query can't be reused.
	path := "/api/v1/webhooks/" + hook.ID.String() + "/feed?after=0"
	w = env.do("", "GET", "/api/v1/webhooks/"+hook.ID.String()+"/feed?after=4", nil,
		webhook.HeaderTimestamp, strconv.FormatInt(env.now.Unix(), 10),
		webhook.HeaderSignature, webhook.Sign(hook.Secret, env.now, []byte(path)))
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	unknown := model.WebhookWithSecret{Webhook: model.Webhook{ID: uuid.New()}}
	assert.Equal(t, http.StatusUnauthorized, env.feed(unknown, "", hook.Secret, env.now).Code)
	assert.Equal(t, http.StatusBadRequest, env.feed(hook, "?after=-1", hook.Secret, env.now).Code)
}

func TestWebhookFeedRateLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rl := NewRateLimiter(2, time.Minute)
	t.Cleanup(rl.Stop)
	r := gin.New()
	r.GET("/feed/:id", WebhookFeedRateLimit(rl), func(c *gin.Context) { c.Status(http.StatusOK) })
	get := func(id string) int {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/feed/"+id, nil))
		return w.Code
	}
	assert.Equal(t, http.StatusOK, get("a"))
	assert.Equal(t, http.StatusOK, get("a"))
	assert.Equal(t, http.StatusTooManyRequests, get("a"))
	assert.Equal(t, http.StatusOK, get("b"), "limits are per webhook")
}
