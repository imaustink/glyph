# Webhooks

Webhooks send a signed HTTP `POST` to your endpoint when notes or tasks change.
Manage them under **Webhooks** in the sidebar (API mode only).

## Scope

| Webhook | Receives changes to |
|---|---|
| Personal | Notes and tasks you own |
| Organization (org owners only) | The organization's non-private notes and tasks, for as long as the webhook's creator is still an owner of the organization |

Only the user who created a webhook can see or manage it.

## Events

Each webhook subscribes to any of:

| Type | When |
|---|---|
| `note.created` | A note is created |
| `note.updated` | A note's title, tags, priority, location, workspace or privacy changes, or its content changes |
| `note.deleted` | A note is deleted (including when a folder above it is deleted) |
| `task.created` | A task is created, or a deleted task is restored (e.g. its bullet reappears in a note) |
| `task.updated` | Any of a task's fields change |
| `task.deleted` | A task is deleted (including when its bullet is removed from the note) |

Folders produce no events. Events are captured in the database transaction that
made the change, so every change is reported however it was made (web app, MCP,
API clients, collaborative editing) and nothing is reported for a change that
did not commit.

`note.updated` for content edits fires each time the document is saved, which
during collaborative editing is every few seconds while someone is typing. The
payload is the note's metadata, not its content — fetch
`GET /api/v1/pages/:id/content` for the document.

## Delivery

```http
POST /your/endpoint
Content-Type: application/json
User-Agent: Glyph-Webhooks/1.0
Glyph-Webhook-Id: 1f0e5a52-6b8e-4bb1-a7b2-6f3e0b8c0d11
Glyph-Webhook-Event: task.updated
Glyph-Webhook-Sequence: 42
Glyph-Webhook-Timestamp: 1795312800
Glyph-Webhook-Signature: v1=5af4877ab3c93d32...

{
  "id": "1f0e5a52-6b8e-4bb1-a7b2-6f3e0b8c0d11",
  "webhookId": "9c0d…",
  "sequence": 42,
  "type": "task.updated",
  "createdAt": "2026-11-22T02:00:00.123456Z",
  "data": { "id": "…", "title": "Buy milk", "status": "done", … }
}
```

`data` is the note or task as the REST API returns it, after the change (for a
deleted note, as it was just before).

- **Success** is any `2xx` response within 10 seconds. Redirects are not followed.
- **Order.** Each webhook's events are delivered one at a time, in `sequence`
  order. If a delivery fails, it is retried after 10s, 30s, 1m, 5m, 15m, 30m and
  1h before later events are sent. After 8 failed attempts the event is marked
  failed and delivery moves on to the next event.
- **At least once.** An event can arrive more than once (a replay, or a retry
  after a response that was lost). Use `id` to de-duplicate.
- **Sequence numbers** are per webhook, start at 1 and have no gaps. A jump in
  `sequence` means you missed events — fetch them from the [feed](#missed-events-feed).
- Webhook URLs must be public `http`/`https` addresses on port 80 or 443.

## Verifying signatures

`Glyph-Webhook-Signature` is `v1=` followed by the hex HMAC-SHA256 of
`"<Glyph-Webhook-Timestamp>.<raw request body>"`, keyed with the webhook's
signing secret (shown once, when the webhook is created or its secret rotated).
Reject requests whose timestamp is more than a few minutes old.

```js
import crypto from 'node:crypto';

function verify(secret, timestamp, signature, rawBody) {
  const expected =
    'v1=' + crypto.createHmac('sha256', secret).update(`${timestamp}.${rawBody}`).digest('hex');
  const fresh = Math.abs(Date.now() / 1000 - Number(timestamp)) < 300;
  return (
    fresh &&
    signature.split(',').some((s) => {
      const a = Buffer.from(s.trim());
      const b = Buffer.from(expected);
      return a.length === b.length && crypto.timingSafeEqual(a, b);
    })
  );
}
```

Check your implementation against this vector: secret `secret`, timestamp
`1700000000`, body `payload` →
`v1=5af4877ab3c93d3201223b2c43d689a4c1e849ddd9091e066f03be6168ae79e9`.

## Event log and replay

The webhook's page lists its events from the last **30 days**, with each event's
delivery status, the HTTP status code of the last attempt, the number of
attempts and the payload. **Replay** sends an event again immediately (same body,
fresh timestamp and signature). Only events that are **delivered** or
**failed** can be replayed; an event still pending is queued for in-order
delivery, so replaying it returns `409 Conflict`. Events older than 30 days are deleted
automatically.

## Missed-events feed

```http
GET /api/v1/webhooks/:webhookId/feed?after=<sequence>&limit=<1-500, default 100>
Glyph-Webhook-Timestamp: <unix seconds>
Glyph-Webhook-Signature: v1=<hex HMAC-SHA256(secret, "<timestamp>.<request URI>")>
```

Returns the webhook's events with `sequence > after`, oldest first, regardless of
whether they were delivered — so after an outage, call it with the last sequence
you processed and page until `hasMore` is false:

```json
{
  "events": [ { "id": "…", "webhookId": "…", "sequence": 43, "type": "note.created", "createdAt": "…", "data": { … } } ],
  "hasMore": false,
  "lastSequence": 43
}
```

The request is authenticated with the webhook's signing secret rather than a
user session: sign the request URI exactly as sent — path and query string, e.g.
`/api/v1/webhooks/9c0d…/feed?after=42&limit=100`. Events are kept for 30 days.

```js
const uri = `/api/v1/webhooks/${webhookId}/feed?after=${lastSeq}`;
const ts = Math.floor(Date.now() / 1000);
const sig = 'v1=' + crypto.createHmac('sha256', secret).update(`${ts}.${uri}`).digest('hex');
const res = await fetch(`https://glyph.example.com${uri}`, {
  headers: { 'Glyph-Webhook-Timestamp': String(ts), 'Glyph-Webhook-Signature': sig }
});
```

## Management API

Session-authenticated only (OAuth bearer tokens get `403`).

| Method | Path | |
|---|---|---|
| `GET` | `/api/v1/webhooks` | List your webhooks |
| `POST` | `/api/v1/webhooks` | Create: `{ name, url, eventTypes, orgId?, active? }` → includes `secret` |
| `GET` | `/api/v1/webhooks/event-types` | The available event types |
| `GET` | `/api/v1/webhooks/:id` | Get one |
| `PATCH` | `/api/v1/webhooks/:id` | Update `name`, `url`, `eventTypes`, `active` |
| `DELETE` | `/api/v1/webhooks/:id` | Delete (with its events) |
| `POST` | `/api/v1/webhooks/:id/rotate-secret` | New signing secret → includes `secret` |
| `GET` | `/api/v1/webhooks/:id/events?limit=&offset=` | Events from the last 30 days, newest first |
| `POST` | `/api/v1/webhooks/:id/events/:eventId/replay` | Deliver an event again now |

## Operations

- Delivery runs in every API replica; each webhook is leased to one replica at a
  time, so delivery order holds across replicas.
- `WEBHOOKS_ALLOW_PRIVATE_URLS=true` lets webhooks target private addresses and
  other ports (e.g. a receiver on `localhost` in development). Don't set it where
  users are untrusted: it would let them make the server call internal services.
