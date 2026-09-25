# Glyph collab service

Realtime collaborative editing for notes. A [Hocuspocus](https://tiptap.dev/docs/hocuspocus)
(Yjs) server that sits next to the Go API, shares the editor schema with the
frontend, and is the only writer of a note's content while people are
editing it together.

```
browser ── wss /collab ──▶ collab service ──▶ Postgres (page_collab_* : append-only Yjs log)
   │                         │   ▲
   │                         │   └── LISTEN glyph_collab ("reset" on version restore / detach)
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
| 3 | **Append-only log.** Updates are only appended; compaction replaces exactly the rows it merged in one transaction. Replicas can't lose each other's updates. | `PgPersistence` | `compaction never loses an append that races it` |
| 4 | **Same schema or no sync.** Clients present `schemaFingerprint()`; a mismatch is refused before sync. y-prosemirror deletes content it can't build, so an outdated client would otherwise erase newer content for everyone. | `onAuthenticate` | `schema gate` |
| 5 | **Epochs.** Every (re)seed starts a new epoch. A connection is bound to the document's epoch before any client state is applied; a Y.Doc that ever held another epoch's state (or state without a known epoch) is refused and must be discarded. So a version restore can't be undone by a client that still has the old document. | `beforeSync`, `CollabSession` | `epochs` |
| 6 | **Every update is validated before it's applied** (against a shadow copy): unknown node/mark types and oversized documents are refused and the connection closed. | `validateIncoming` | `update validation` |
| 7 | **Snapshots are checked twice.** The service writes a snapshot only if it converts under the schema; the API sanitises it and accepts it only for the current epoch and a non-decreasing sequence. An invalid document quarantines the page (collaborators become read-only) instead of being saved. | `persistOnce`, `WriteCollabSnapshot` | `quarantine`, `SnapshotFromAReplacedEpochIsRejected`, `SnapshotCannotGoBackwards` |
| 8 | **Side effects happen once.** Task creation, title sync and bullet identity only react to *local* transactions; tasks are reconciled server-side with the persisted document (soft delete + restore); repairs that two clients would make differently (duplicate `nodeId`s, unsafe links) are made by the server only. | `isLocalTransaction`, `CollabNodeIdExtension`, `reconcileSourceTasksLocked`, `repair` | `collabEditor.test.ts`, `TestTaskSourceIntegrity`, E2E |

## Lifecycle of a note

1. The browser asks the API `GET /pages/:id/collab`. If collaboration is on it
   opens a WebSocket for document `page:<id>`, sending `{epoch, fingerprint}` as
   its token (epoch `null` for a fresh, empty Y.Doc).
2. `onAuthenticate` checks Origin, fingerprint and access (the API answers with
   the forwarded cookie). Viewers are admitted read-only.
3. `onLoadDocument` loads the log, seeding a new epoch from `page_contents` if
   the page isn't attached (giving every list item a `nodeId`, and every empty
   text block a shared text node).
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

The API side: `COLLAB_ENABLED=true` and `COLLAB_SERVICE_TOKEN`. Helm: `collab.enabled`,
`collab.serviceToken` (see `helm/glyph/values.yaml`).

## Operations

- **Kill switch:** set `COLLAB_ENABLED=false` on the API. Browsers fall back to
  single-writer editing; each attached page detaches on its next save, and the
  collab service's snapshots for it are refused. Turning it back on re-seeds
  pages in new epochs.
- **Quarantined page** (`page_collab_docs.quarantined_at` set, logged at error
  level): collaborators are read-only. Restore a version
  (`POST /api/v1/pages/:id/content/versions/:vid/restore`) to recover; that
  starts a clean epoch.
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
- Two people typing their very first character into the same *newly created*
  empty paragraph at the same instant can have one character land out of
  order (a y-prosemirror quirk). Seeded paragraphs, including the trailing
  one, are immune. Nothing is lost either way.

## Task status on bullets

When a note task's status changes outside the editor (the board, the task
page, an API client), the API sends `NOTIFY glyph_collab
{"type":"task-status", pageId, nodeId, status}`. Every replica with that note
loaded sets the bullet's `taskStatus`/`checked` attributes in the shared
document (`onTaskStatus` → `setListItemStatus`), so all open editors update
live. The server is the only writer of this edit, so editors never race to
write it. Notes nobody has open are left alone; editors sync statuses from
the task list when they open a note.
