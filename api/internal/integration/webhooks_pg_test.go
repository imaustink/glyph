package integration

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Webhook events are captured by triggers (migration 000027), so these tests
// run against Postgres only and drive the tables with plain SQL, the way any
// write path would.

const (
	whAlice = "00000000-0000-0000-0000-0000000000a1"
	whBob   = "00000000-0000-0000-0000-0000000000b1"
	whOrg   = "00000000-0000-0000-0000-0000000000c1"
)

func webhookDB(t *testing.T) (*pgxpool.Pool, store.WebhookStore) {
	t.Helper()
	pool := startMigrationDB(t)
	applyMigrations(t, pool, "000001", "999999")
	_, err := pool.Exec(context.Background(), `
		INSERT INTO users (id, sub, issuer) VALUES ('`+whAlice+`', 'alice', 'i'), ('`+whBob+`', 'bob', 'i');
		INSERT INTO organizations (id, name, created_by) VALUES ('`+whOrg+`', 'Acme', '`+whAlice+`');
		INSERT INTO org_members (org_id, user_id, role) VALUES
			('`+whOrg+`', '`+whAlice+`', 'owner'), ('`+whOrg+`', '`+whBob+`', 'editor');`)
	require.NoError(t, err)
	return pool, store.NewWebhookStore(pool)
}

func createHook(t *testing.T, s store.WebhookStore, owner string, org *uuid.UUID, types ...model.WebhookEventType) *model.Webhook {
	t.Helper()
	w, err := s.Create(context.Background(), &model.Webhook{
		UserID: uuid.MustParse(owner), OrgID: org, Name: "hook", URL: "https://example.com/hook",
		Secret: "whsec_x", EventTypes: types, Active: true,
	})
	require.NoError(t, err)
	return w
}

func exec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	_, err := pool.Exec(context.Background(), sql, args...)
	require.NoError(t, err)
}

// eventTypes lists the webhook's events (oldest first) as "type:resource".
func eventLog(t *testing.T, s store.WebhookStore, w *model.Webhook) []string {
	t.Helper()
	events, _, err := s.ListEvents(context.Background(), w.ID, time.Now().Add(-time.Hour), 200, 0)
	require.NoError(t, err)
	out := []string{}
	for i := len(events) - 1; i >= 0; i-- {
		out = append(out, string(events[i].Type)+":"+events[i].ResourceID.String()[24:])
	}
	return out
}

func TestWebhooks_Postgres(t *testing.T) {
	t.Parallel()

	t.Run("PersonalWebhookCapturesOwnTaskLifecycle", func(t *testing.T) {
		pool, s := webhookDB(t)
		ctx := context.Background()
		w := createHook(t, s, whAlice, nil, model.WebhookTaskCreated, model.WebhookTaskUpdated, model.WebhookTaskDeleted)

		task := "00000000-0000-0000-0000-000000000001"
		exec(t, pool, `INSERT INTO tasks (id, user_id, title) VALUES ($1, $2, 'Buy milk')`, task, whAlice)
		exec(t, pool, `UPDATE tasks SET title = 'Buy oat milk', updated_at = NOW() WHERE id = $1`, task)
		exec(t, pool, `UPDATE tasks SET updated_at = NOW() + interval '1 second' WHERE id = $1`, task) // no visible change
		exec(t, pool, `UPDATE tasks SET deleted_at = NOW(), deleted_reason = 'user' WHERE id = $1`, task)
		exec(t, pool, `UPDATE tasks SET title = 'ignored' WHERE id = $1`, task) // already deleted
		exec(t, pool, `UPDATE tasks SET deleted_at = NULL, deleted_reason = NULL WHERE id = $1`, task)
		exec(t, pool, `DELETE FROM tasks WHERE id = $1`, task)
		// Bob's task is outside Alice's personal webhook.
		exec(t, pool, `INSERT INTO tasks (id, user_id, title) VALUES ('00000000-0000-0000-0000-000000000002', $1, 'Bob''s')`, whBob)

		assert.Equal(t, []string{
			"task.created:000000000001", "task.updated:000000000001", "task.deleted:000000000001",
			"task.created:000000000001", "task.deleted:000000000001",
		}, eventLog(t, s, w))

		events, total, err := s.ListEvents(ctx, w.ID, time.Now().Add(-time.Hour), 1, 4)
		require.NoError(t, err)
		require.Equal(t, 5, total)
		first := events[0]
		assert.Nil(t, first.Seq, "events are numbered after commit, not by the trigger")
		assert.Equal(t, model.WebhookEventPending, first.Status)
		var data map[string]any
		require.NoError(t, json.Unmarshal(first.Data, &data))
		assert.Equal(t, "Buy milk", data["title"])
		assert.Equal(t, whAlice, data["userId"])
		assert.Equal(t, "todo", data["status"])
		for _, k := range []string{"id", "description", "priority", "tags", "dueDate", "sourcePageId", "sourceNodeId", "link", "order", "orgId", "isPrivate", "folderId", "createdAt", "updatedAt"} {
			assert.Contains(t, data, k)
		}
	})

	t.Run("NotesOnlyAndOnlySubscribedTypes", func(t *testing.T) {
		pool, s := webhookDB(t)
		w := createHook(t, s, whAlice, nil, model.WebhookNoteCreated, model.WebhookNoteUpdated, model.WebhookNoteDeleted)
		createdOnly := createHook(t, s, whAlice, nil, model.WebhookNoteCreated)
		paused := createHook(t, s, whAlice, nil, model.WebhookNoteCreated)
		off := false
		_, err := s.Update(context.Background(), paused.ID, paused.UserID, store.WebhookPatch{Active: &off})
		require.NoError(t, err)

		folder, note := "00000000-0000-0000-0000-000000000010", "00000000-0000-0000-0000-000000000011"
		exec(t, pool, `INSERT INTO pages (id, user_id, type, title) VALUES ($1, $2, 'folder', 'F')`, folder, whAlice)
		exec(t, pool, `INSERT INTO pages (id, user_id, type, title, parent_id) VALUES ($1, $2, 'page', 'N', $3)`, note, whAlice, folder)
		exec(t, pool, `UPDATE pages SET title = 'F2' WHERE id = $1`, folder)
		exec(t, pool, `UPDATE pages SET title = 'N2' WHERE id = $1`, note)
		exec(t, pool, `INSERT INTO page_contents (page_id, content) VALUES ($1, '{"type":"doc","content":[]}')`, note)
		exec(t, pool, `UPDATE page_contents SET content = '{"type":"doc","content":[]}', revision = revision + 1 WHERE page_id = $1`, note) // unchanged
		exec(t, pool, `UPDATE page_contents SET content = '{"type":"doc","content":[{"type":"paragraph"}]}' WHERE page_id = $1`, note)
		exec(t, pool, `DELETE FROM pages WHERE id = $1`, folder) // cascades to the note

		assert.Equal(t, []string{
			"note.created:000000000011", "note.updated:000000000011", "note.updated:000000000011",
			"note.updated:000000000011", "note.deleted:000000000011",
		}, eventLog(t, s, w))
		assert.Equal(t, []string{"note.created:000000000011"}, eventLog(t, s, createdOnly))
		assert.Empty(t, eventLog(t, s, paused))
	})

	t.Run("OrgWebhookSeesNonPrivateOrgRowsWhileOwnerRemainsOwner", func(t *testing.T) {
		pool, s := webhookDB(t)
		org := uuid.MustParse(whOrg)
		w := createHook(t, s, whAlice, &org, model.WebhookNoteCreated, model.WebhookTaskCreated)

		exec(t, pool, `INSERT INTO pages (id, user_id, type, title, org_id) VALUES ('00000000-0000-0000-0000-000000000021', $1, 'page', 'Shared', $2)`, whBob, whOrg)
		exec(t, pool, `INSERT INTO pages (id, user_id, type, title, org_id, is_private) VALUES ('00000000-0000-0000-0000-000000000022', $1, 'page', 'Private', $2, true)`, whBob, whOrg)
		exec(t, pool, `INSERT INTO tasks (id, user_id, title) VALUES ('00000000-0000-0000-0000-000000000023', $1, 'Personal')`, whBob)
		exec(t, pool, `INSERT INTO tasks (id, user_id, title, org_id) VALUES ('00000000-0000-0000-0000-000000000024', $1, 'Org task', $2)`, whBob, whOrg)

		exec(t, pool, `UPDATE org_members SET role = 'editor' WHERE user_id = $1`, whAlice)
		exec(t, pool, `INSERT INTO tasks (id, user_id, title, org_id) VALUES ('00000000-0000-0000-0000-000000000025', $1, 'After demotion', $2)`, whBob, whOrg)

		assert.Equal(t, []string{"note.created:000000000021", "task.created:000000000024"}, eventLog(t, s, w))
	})

	t.Run("SequenceFeedLeaseAndPurge", func(t *testing.T) {
		pool, s := webhookDB(t)
		ctx := context.Background()
		w := createHook(t, s, whAlice, nil, model.WebhookTaskCreated)
		other := createHook(t, s, whAlice, nil, model.WebhookTaskCreated)
		for i := 0; i < 3; i++ {
			exec(t, pool, `INSERT INTO tasks (user_id, title) VALUES ($1, 't')`, whAlice)
		}

		require.NoError(t, s.Sequence(ctx, w.ID))
		exec(t, pool, `INSERT INTO tasks (user_id, title) VALUES ($1, 't')`, whAlice)
		require.NoError(t, s.Sequence(ctx, w.ID))
		require.NoError(t, s.Sequence(ctx, w.ID)) // idempotent

		feed, err := s.ListEventsAfter(ctx, w.ID, 0, 10)
		require.NoError(t, err)
		seqs := []int64{}
		for _, e := range feed {
			seqs = append(seqs, *e.Seq)
		}
		assert.Equal(t, []int64{1, 2, 3, 4}, seqs)
		feed, err = s.ListEventsAfter(ctx, w.ID, 2, 1)
		require.NoError(t, err)
		require.Len(t, feed, 1)
		assert.Equal(t, int64(3), *feed[0].Seq)
		otherFeed, err := s.ListEventsAfter(ctx, other.ID, 0, 10)
		require.NoError(t, err)
		assert.Empty(t, otherFeed, "the other webhook's events are numbered separately")

		// Lease: both webhooks are due; a second lease finds nothing.
		leased, err := s.LeaseDue(ctx, 10, time.Minute)
		require.NoError(t, err)
		assert.Len(t, leased, 2)
		again, err := s.LeaseDue(ctx, 10, time.Minute)
		require.NoError(t, err)
		assert.Empty(t, again)
		require.NoError(t, s.ReleaseLease(ctx, w.ID))

		next, err := s.NextPending(ctx, w.ID)
		require.NoError(t, err)
		assert.Equal(t, int64(1), *next.Seq)
		c := 204
		rec, err := s.RecordAttempt(ctx, next.ID, model.WebhookDeliveryResult{StatusCode: &c, At: time.Now()},
			model.WebhookEventDelivered, time.Now())
		require.NoError(t, err)
		assert.Equal(t, model.WebhookEventDelivered, rec.Status)
		assert.Equal(t, 1, rec.Attempts)
		assert.Equal(t, 204, *rec.LastStatusCode)
		assert.NotNil(t, rec.DeliveredAt)
		next, err = s.NextPending(ctx, w.ID)
		require.NoError(t, err)
		assert.Equal(t, int64(2), *next.Seq)

		// Retention: only events older than the cutoff go.
		exec(t, pool, `UPDATE webhook_events SET created_at = NOW() - interval '31 days' WHERE webhook_id = $1 AND seq <= 2`, w.ID)
		n, err := s.PurgeEventsBefore(ctx, time.Now().Add(-30*24*time.Hour))
		require.NoError(t, err)
		assert.Equal(t, int64(2), n)
		feed, err = s.ListEventsAfter(ctx, w.ID, 0, 10)
		require.NoError(t, err)
		assert.Len(t, feed, 2)

		// Deleting the webhook deletes its events.
		require.NoError(t, s.Delete(ctx, w.ID, w.UserID))
		var left int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM webhook_events WHERE webhook_id = $1`, w.ID).Scan(&left))
		assert.Zero(t, left)
		assert.ErrorIs(t, s.Delete(ctx, w.ID, w.UserID), store.ErrNotFound)
	})

	t.Run("OwnerScopedAccess", func(t *testing.T) {
		_, s := webhookDB(t)
		ctx := context.Background()
		w := createHook(t, s, whAlice, nil, model.WebhookTaskCreated)
		_, err := s.Get(ctx, w.ID, uuid.MustParse(whBob))
		assert.ErrorIs(t, err, store.ErrNotFound)
		name := "renamed"
		_, err = s.Update(ctx, w.ID, uuid.MustParse(whBob), store.WebhookPatch{Name: &name})
		assert.ErrorIs(t, err, store.ErrNotFound)
		got, err := s.Update(ctx, w.ID, w.UserID, store.WebhookPatch{Name: &name, EventTypes: []model.WebhookEventType{model.WebhookTaskDeleted}})
		require.NoError(t, err)
		assert.Equal(t, "renamed", got.Name)
		assert.Equal(t, []model.WebhookEventType{model.WebhookTaskDeleted}, got.EventTypes)
		assert.Equal(t, "https://example.com/hook", got.URL)
		rotated, err := s.SetSecret(ctx, w.ID, w.UserID, "whsec_new")
		require.NoError(t, err)
		assert.Equal(t, "whsec_new", rotated.Secret)
		list, err := s.ListByUser(ctx, uuid.MustParse(whBob))
		require.NoError(t, err)
		assert.Empty(t, list)
	})
}
