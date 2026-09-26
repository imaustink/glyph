# Glyph collab service

Realtime collaborative editing for notes. A [Hocuspocus](https://tiptap.dev/docs/hocuspocus)
(Yjs) server that sits next to the Go API, shares the editor schema with the
frontend, and is the only writer of a note's content while people are
editing it together.

```
browser ── wss /collab ──▶ collab service ──▶ Postgres (page_collab_* : append-only Yjs log)
   │                         │   ▲
   │                         │   └── LISTEN glyph_collab ("reset" on version restore / detach; task status/title)
   │                         ├── GET  /api/v1/pages/:id/collab      (who may join; cookie forwarded)
   │                         └── PUT  /internal/collab/pages/:id/snapshot  (service token)
   └── REST /api ───────────▶ Go API ──▶ page_contents (derived snapshot), tasks, history
```

## Integrity invariants

Each one is enforced in code and covered by a test.

| # | Invariant | Where | Tests |
|---|---|---|---|
| 1 | **One writer per document.** While a page is *attached*, only the collab service writes its content; REST `PUT /content` answers 409 `collaborative`. | `UpsertContent` (API) | `TestCollabWritePath` |
| 2 | **Seeded once.** A page's shared document is created from `page_contents` exactly once per epoch, under the same row lock REST writes take. Two independent seeds would duplicate the page when merged. Clients never seed. | `PgPersistence.loadOrSeed` | `seeds exactly once …` (memory + Postgres) |
| 3 | **Append-only log, visible in seq order.** Updates are only appended; compaction replaces exactly the rows it merged in one transaction. Every writer takes the page's `page_collab_docs` row lock before drawing a seq, so a page's seqs commit in order, and a replica never advances `lastSeq` past rows it hasn't applied (it catches up after its own append). Replicas can't lose each other's updates. | `PgPersistence`, `persistOnce` | `compaction never loses an append that races it`, `a reader that has seen a seq …`, `a foreign append between catch-up and append …` |
| 4 | **Same schema or no sync.** Clients present `schemaFingerprint()`; a mismatch is refused before sync. y-prosemirror deletes content it can't build, so an outdated client would otherwise erase newer content for everyone. | `onAuthenticate` | `schema gate` |
| 5 | **Epochs.** Every (re)seed starts a new epoch. A connection is bound to the document's epoch before any client state is applied; a Y.Doc that ever held another epoch's state (or state without a known epoch) is refused and must be discarded. So a version restore can't be undone by a client that still has the old document. | `beforeSync`, `CollabSession` | `epochs` |
| 6 | **Every update is validated before it's applied** (against a shadow copy): unknown node/mark types and oversized documents are refused and the connection closed. | `validateIncoming` | `update validation` |
| 7 | **Snapshots are checked twice.** The service writes a snapshot only if it converts under the schema; the API sanitises it and accepts it only for the current epoch and a non-decreasing sequence. A replaced epoch (`stale_snapshot`) evicts; a snapshot merely behind another replica's (`snapshot_behind`) catches up and retries instead. An invalid document quarantines the page (collaborators become read-only) instead of being saved. | `persistOnce`, `WriteCollabSnapshot` | `quarantine`, `SnapshotFromAReplacedEpochIsRejected`, `SnapshotCannotGoBackwards`, `a snapshot that loses the race …` |
| 8 | **Side effects happen once.** Task creation, title sync and bullet identity only react to *local* transactions; tasks are reconciled server-side with the persisted document (soft delete + restore); repairs that two clients would make differently (duplicate `nodeId`s, unsafe links) are made by the server only. | `isLocalTransaction`, `CollabNodeIdExtension`, `reconcileSourceTasksLocked`, `repair` | `collabEditor.test.ts`, `TestTaskSourceIntegrity`, E2E |

## Lifecycle of a note

1. The browser asks the API `GET /pages/:id/collab`. If collaboration is on it
   opens a WebSocket for document `page:<id>`, sending `{epoch, fingerprint}` as
   its token (epoch `null` for a fresh, empty Y.Doc).
2. `onAuthenticate` checks Origin, fingerprint and access (the API answers with
   the forwarded cookie). Viewers are admitted read-only.
3. `onLoadDocument` loads the log, seeding a new epoch from `page_contents` if
   the page isn't attached (giving every list item a `nodeId`, and every empty
   text block a shared text node). `afterLoadDocument` makes server repairs
   and applies task titles renamed while the note was closed (see below).
4. `beforeSync` binds the connection to the epoch and sends it to the client
   *before* any content. Every change message is validated on a shadow doc.
5. `onStoreDocument` (debounced, and on last disconnect): pull other replicas'
   updates → server repairs → append to log → compact → snapshot to the API
   (which writes `page_contents`, keeps version history, reconciles tasks).
6. A version restore (or a REST write while collaboration is disabled)
   *detaches* the page and sends `NOTIFY glyph_collab reset`. The service
   evicts every connection (`reset`); clients discard their Y.Doc and reconnect
   into a new epoch seeded from the restored content. If the notification is
   missed, the next append or snapshot fails its epoch check and evicts anyway.

## Configuration

| Variable | Default | |
|---|---|---|
| `DATABASE_URL` | — | Same database as the API. |
| `API_URL` | — | The API, reachable from this service (not via the Ingress). |
| `COLLAB_SERVICE_TOKEN` | — | Must equal the API's `COLLAB_SERVICE_TOKEN`. |
| `COLLAB_ALLOWED_ORIGINS` | *(same-origin)* | Comma-separated. Empty: Origin host must equal Host. |
| `PORT` | `1235` | |
| `COLLAB_STORE_DEBOUNCE_MS` / `_MAX_DEBOUNCE_MS` | `2000` / `10000` | Persistence debounce. |
| `COLLAB_REAUTH_INTERVAL_MS` | `60000` | How often each connection's access is re-checked. |
| `COLLAB_CATCH_UP_INTERVAL_MS` | `5000` | Pull other replicas' updates (multi-replica only). |
| `COLLAB_COMPACT_EVERY` | `100` | Appends between log compactions. |
| `COLLAB_MAX_DOCUMENT_BYTES` | `5242880` | Matches the API's content limit. |
| `COLLAB_LEASE_TTL_MS` | `30000` | Lifetime of this replica's lease on each loaded document (renewed every third of it). A schema re-seed waits for other replicas' leases, so this bounds how long a crashed replica can delay one. |
| `COLLAB_SHUTDOWN_DRAIN_MS` | `20000` | On SIGTERM, how long to keep retrying documents whose updates aren't persisted yet (e.g. during a database outage) before dropping them with an error log. Keep it below the pod's termination grace period. |

The API side: `COLLAB_ENABLED=true` and `COLLAB_SERVICE_TOKEN`. Helm: `collab.enabled`,
`collab.serviceToken` (see `helm/glyph/values.yaml`).

## Operations

- **Kill switch:** set `COLLAB_ENABLED=false` on the API. Browsers fall back to
  single-writer editing; each attached page detaches on its next save, and the
  collab service's snapshots for it are refused from then on. Until that save,
  the API still accepts the service's snapshots of the attached page (answering
  `disabled: true`, which closes its editors with `disabled` after the write),
  so edits made since the last snapshot reach `page_contents` before the detach.
  Keep the collab service and the API's `COLLAB_SERVICE_TOKEN` running while
  sessions drain. Turning it back on re-seeds detached pages in new epochs.
- **Quarantined page** (`page_collab_docs.quarantined_at` set, logged at error
  level): collaborators are read-only. Restore a version
  (`POST /api/v1/pages/:id/content/versions/:vid/restore`) to recover; that
  starts a clean epoch.
- **Schema upgrades during a rolling deploy:** the first new-build editor to
  open a note written under the old schema re-seeds it into a new epoch. While
  a replica of the other build still has that note loaded (it holds a lease in
  `page_collab_leases`), the re-seed is refused and the new editor retries
  (`unavailable`) until the old replica unloads it or its lease expires, so the
  old replica's not-yet-appended edits aren't thrown away. Collab builds from
  before leases don't take them: for the rollout that *introduces* leases, drain
  the old collab pods before new-build editors connect if the editor schema
  changed in the same release.
- **Replicas:** one is recommended. More are safe (invariant 3 plus epoch and
  sequence checks on snapshots), but editors on different replicas see each
  other's changes every `COLLAB_CATCH_UP_INTERVAL_MS` rather than live.

## Development

```bash
pnpm --filter @k5s/glyph-collab test    # unit + WebSocket integration tests
COLLAB_TEST_DATABASE_URL=postgres://… pnpm --filter @k5s/glyph-collab test   # + Postgres tests
pnpm --filter @k5s/glyph-collab build   # → dist/server.js (single bundle)
```

The service imports `src/lib/editor/schema.ts` and `src/lib/collab/protocol.ts`
from the frontend (bundled by esbuild via the `$lib` path alias), so schema and
wire protocol can't drift between the two.

## Known limitations

- No offline persistence in the browser: edits made while disconnected are kept
  in memory and sync on reconnect, but are lost if the tab is closed first
  (the app warns before unload while changes are unsynced).
- **"Synced" is not "durable".** A client counts an edit as synced once the
  collab service has applied it in memory (the Yjs sync ack); the service
  appends it to the log only on its next debounced store, up to
  `COLLAB_STORE_MAX_DEBOUNCE_MS` later, or longer during a database outage.
  The service holds such edits through outages (retrying, deferring unload,
  draining for `COLLAB_SHUTDOWN_DRAIN_MS` on shutdown), but a crash or `SIGKILL`
  of the collab process in that window loses them, even though every editor
  showed them as saved. Editors that are still connected re-send them on
  reconnect; an editor that has already closed the note cannot.
- Two people typing their very first character into the same *newly created*
  empty paragraph at the same instant can have one character land out of
  order (a y-prosemirror quirk). Seeded paragraphs, including the trailing
  one, are immune. Nothing is lost either way.

## Task status and titles on bullets

When a note task's status changes outside the editor (on the board, on the
task page, or from an API client), the API sends `NOTIFY glyph_collab
{"type":"task-status", pageId, nodeId, status}`. Every replica that has the
note loaded sets the bullet's `taskStatus`/`checked` attributes in the shared
document (`onTaskStatus` → `setListItemStatus`), so all open editors update
live. The server is the only writer of this edit, so editors never race to
write it. Notes nobody has open are left alone, because editors sync statuses
from the task list when they open a note.

**Titles (DI-29).** A task can also be renamed outside the note, on the task
page or with MCP `update_task`. If the bullet kept its old text, the next
keystroke in it would push that text back as the title. Editors can't sync
titles into a shared document: each one would make the same text edit, and
Yjs would merge them into duplicated text. So the collab service writes the
title, on two paths:

- **The note is open.** A `PATCH /tasks/:id` whose title changes, without
  `X-Glyph-Change-Source: bullet`, sends `{"type":"task-title", pageId,
  nodeId, title}`. The editor sends that header with the titles it takes from
  bullet text, and those are never echoed back into the note, because the
  bullet may already hold newer text. `onTaskTitle` makes the linked bullet's
  first paragraph read the title (`setListItemText`). Text that stays the same
  at either end keeps its formatting, and new text takes the formatting of the
  character before it.
- **The note is closed.** The same PATCH sets `tasks.title_renamed_at`. When a
  document loads, `afterLoadDocument` applies the titles of tasks whose bullets
  are still owed their rename, before any editor syncs.

Either way the edit is written once, even when several replicas hold the
note. It is built and appended under the page's log lock from the latest log
(`appendExclusive`). A replica that finds the title already there writes
nothing.

**Which renames are owed.** This is tracked per task, because nothing records
which bullet an edit touched. The note's newest write can't be used: an edit
to any other bullet would make it newer than the rename. A rename is owed
until `tasks.title_applied_at` (migration 000026) catches up with
`tasks.title_renamed_at`. Two things set it:

- The collab writer, in the same transaction as its `appendExclusive`, for
  each bullet that shows its task's title afterwards, whether it wrote the
  title or found it already there. This covers both the live path and the load
  path. A task renamed again to a different title since then stays owed.
- Seeding a new epoch from `page_contents` written after the rename, such as a
  version restore or a REST save that detached the note. That whole-document
  write is newer than the rename, so the stored content wins. A schema
  re-seed carries the old log's bullets over, and any renames they are owed
  go with them.

A bullet edited after the rename keeps its text, because the editor's title
sync gives the task that text. Writing the task's title into the bullet then
changes nothing, and the rename is recorded as applied. There is one edge. A
rename that the bullet never showed wins over a later edit to that bullet
whose title never reached the task (for example, the tab closed within the
sync debounce). The two cases look the same, and the task's title is the one
on record.

This is best effort, like status. If a notification is missed, or applying on
load fails, the rename stays owed and arrives on a later load. Some races
remain:

- Someone types in the bullet within the editor's title debounce (500 ms) of
  a rename. Their debounced title can then land after the rename, so the task
  keeps their text while the bullet shows the rename.
- `PUT /tasks/:id` (full upsert, which neither the app nor MCP uses) doesn't
  count as a rename.

**Rollout.** All of this is additive:

- API pods from before this change never send `task-title` or set the column.
- Collab pods from before it ignore the notification. They also stamp seed and
  compacted rows with `NOW()`, which can only make an older rename look
  already applied. It never applies a rename over newer text.
- A collab pod that starts before migration 000025 has run finds no renames.
- Before migration 000026 has run, a collab pod falls back to comparing renames
  with the note's newest write. That write is the newest `created_at` in the
  epoch's log, and each row's `created_at` is when the newest content in it
  was written: an appended row when it is appended, a seed row
  `page_contents.updated_at`, a schema re-seed row the replaced log's newest
  time, and a compacted row the newest time of the rows it merged. Under this
  fallback an edit to another bullet hides the rename, and nothing is recorded
  as applied. The pod rechecks for the column every 30 s.
- Collab pods from before this change never set `title_applied_at`. A new pod
  applies any rename still owed when it next loads the note. That is a no-op if
  the bullet already shows the title. One case goes wrong: a note seeded by an
  old pod from content restored after a rename. The new pod doesn't know the
  seed settled the rename, so it applies it over the restored text.
