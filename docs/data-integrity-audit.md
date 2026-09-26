# Data Integrity Audit

**Date:** 2026-09-26
**Base commit:** `1a8046d` (main)
**Scope:** The whole app. That covers the Go store and migrations, the HTTP handlers, the MCP tools and the `pmmd` markdown converter, the collab service (Hocuspocus/Yjs), the editor and note↔task sync, the frontend stores and storage backends, and the deploy/ops layer (Helm, CNPG, CD, nginx).

## Method

Six parallel reviews each read one layer end to end and traced call paths across layer boundaries where needed.

- **Confirmed:** reproduced with a throwaway test in a scratch copy of the code, or traced through every step of the code path. None of the repro tests were added to the repo.
- **Likely:** the code path supports it but it was not reproduced.
- **Speculative:** plausible, but it depends on timing or on usage we did not observe.

Where two reviews found the same issue independently, that is noted.

## Executive summary

| # | Severity | Issue | Confidence |
|---|---|---|---|
| DI-01 | **Critical** | Content with an `image` node or a `highlight`/`subscript`/`superscript` mark (all accepted by the API, and `image` produced by MCP markdown) loads as a **blank editor**. The first keystroke saves the empty doc over the note and soft-deletes every task on the page. | Confirmed (2 reviews) |
| DI-02 | High | Deleting a folder hard-deletes every descendant page, **including other users' pages, their content and all version history**. There is no trash. | Confirmed |
| DI-03 | High | Backups and WAL archiving are **off by default**, the backup template has bugs, and the CNPG Cluster has no `resource-policy: keep` (a `helm uninstall` deletes the database). | Confirmed |
| DI-04 | High | After a 409, the save that was already waiting to go out is sent with the fresh revision and **overwrites the other writer's content**. | Confirmed (test) |
| DI-05 | High | PATCH on tasks, pages, lanes and templates is read-modify-write with no lock or precondition, so **concurrent edits to different fields undo each other**. | Confirmed |
| DI-06 | High | `PUT /pages/:id` does no cycle check. A cycle hides the subtree and makes the recursive CTEs **loop without end** (there is no `statement_timeout`). | Confirmed (2 reviews) |
| DI-07 | High | A missing `todoTrigger` is stored as JSONB `null` and read back as an empty config, which **silently disables TODO detection** on every page created without a template. | Confirmed |
| DI-08 | High | MCP `write_page_content` in replace mode is **unguarded** when `expected_revision` is omitted, and the markdown round-trip loses data: task links, nodeIds, list `start`, marks and tables. | Confirmed |
| DI-09 | High | Pressing Enter at the start of a linked bullet **moves the task link to the new empty bullet** and creates a duplicate task. | Confirmed (test) |
| DI-10 | High | Copy/paste duplicates `nodeId`/`taskId` when collab is off, and moving a bullet between notes **silently deletes its task**. | Confirmed (test) |
| DI-11 | High | Collab with more than one replica, which includes rollout overlap: a replica can **permanently skip another replica's log row**, and a stale-snapshot 409 **evicts clients and drops unpersisted edits**. | Confirmed (test) |
| DI-12 | High | Collab: reopening a note while its unload flush is running **deletes the new session's state**, so that session's edits are never persisted. | Confirmed (test) |
| DI-13 | High | Sidebar sibling reorder sends a fractional `order` that the API rejects, so reorder **always fails in API mode** with no error shown. | Confirmed |

The rest (Medium and Low) is detailed below. Two fixes that apply across the codebase would close whole groups of findings:

1. **Generate the content validator's allowlist from the editor schema** (`documentSchema()`), and load content with `enableContentCheck`. This closes DI-01, DI-24 and part of DI-08.
2. **Make each write a single transaction guarded by a lock or a version check**: field-level `SET`, or `SELECT … FOR UPDATE`, or an `updated_at` precondition. This closes DI-05, DI-06 (race variant) and DI-20.

---

## Critical

### DI-01 — Content the editor schema can't represent loads blank, and the next save wipes the note

- **Where:**
  - `api/internal/handler/content_validator.go:13-58` allowlists the `image` node and the `highlight`, `subscript` and `superscript` marks. The editor schema (`src/lib/editor/schema.ts`: StarterKit v3 plus TaskLink) has none of them.
  - `api/internal/pmmd/parse_block.go:122,303-320` produces `image` from `![alt](url)`.
  - `src/lib/components/editor/Editor.svelte:138-139` calls `setContent(json)` without a content check.
- **Failure:** an MCP agent writes markdown with an image, or any API client sends a `highlight` mark. When a user opens the note:
  - TipTap's `createNodeFromContent` catches "Unknown node type" and **falls back to an empty doc**, logging only a console warning.
  - The editor is editable. The first keystroke autosaves the empty doc with a valid `expectedRevision`.
  - `reconcileSourceTasksLocked` then soft-deletes every task on the page.
  - The note can only be recovered from `page_content_versions`, and only if its folder hasn't also been deleted (DI-02).
  - With collab on, the failure is different: `seedUpdate` throws ("not representable") and the page can never be opened collaboratively.
- **Fix:**
  1. Load with `enableContentCheck: true` and handle `contentError`. On failure, make the editor read-only and never save.
  2. Derive the Go allowlist from the editor schema and add a test that compares the two.
  3. Have `pmmd` render block images as a link paragraph, or add the Image extension to the editor.
  4. Add a guard in the store: refuse a content write that turns a doc with more than N nodes into an empty one unless the caller confirms it explicitly.

---

## High

### DI-02 — Folder delete cascades to other users' pages and deletes all version history
- **Where:**
  - Migration 000001: `pages.parent_id ON DELETE CASCADE`.
  - Migration 000017: `page_content_versions ON DELETE CASCADE`.
  - `api/internal/store/page_store.go:185-194` and `api/internal/handler/page_handler.go:204-228`.
- **Failure:** `CanUseParent` lets org editors and people with editor shares create their own pages inside someone else's folder. `DeletePage` checks only that the caller owns the root. The cascade then removes every descendant regardless of owner, along with its content, its version history and its collab state. Those pages' tasks become orphans (see DI-21).
- **Fix:** add soft delete / a trash for pages. Refuse the delete, or move the pages elsewhere, when the subtree contains pages owned by others. Keep `page_content_versions` after its page is deleted, either by dropping that FK cascade or by archiving first.

### DI-03 — Database durability: no backups by default, and uninstalling the release deletes the database
- **Where:**
  - `helm/glyph/values.yaml:194-197`: `backup.enabled: false`.
  - `helm/glyph/templates/cnpg-cluster.yaml:1-9, 27, 34`.
  - `_helpers.tpl:100-102`.
- **Failures:**
  - No base backups, no WAL archive and no point-in-time recovery. Three instances protect against losing a node, not against an app bug that overwrites data (DI-01, DI-04, DI-08). The comment in migration 000017 records a real incident (2026-09-21) where point-in-time recovery would have been the only way back.
  - The `Cluster` has no `helm.sh/resource-policy: keep`, and its name comes from the release name. `helm uninstall`, an Argo prune, or a change to `fullnameOverride` deletes the database, or silently starts a new empty one.
  - `{{- if .Values.cnpg.backup.s3 }}` is always true, because `s3` is a map. With `secretName` left empty, archiving fails and WAL builds up until the 10Gi volume is full and Postgres stops accepting writes.
  - The schedule `"0 0 * * *"` has 5 fields, but CNPG's `ScheduledBackup` expects 6 (seconds first). *(Likely.)*
  - Monitoring is off by default, so nothing alerts when archiving fails.
- **Fix:**
  - Add `resource-policy: keep` and pin the cluster name.
  - Change the condition to `if .Values.cnpg.backup.s3.secretName`.
  - Use a 6-field cron.
  - Fail the template when a production flag is set and backups are off.
  - Enable the PodMonitor and alert on `cnpg_pg_stat_archiver_failed_count` and on backup age.
  - Confirm that the private `values-production.yaml` actually enables backups.

### DI-04 — After a 409, the waiting debounced save overwrites the newer server content
- **Where:** `src/lib/editor/useContentSave.ts:42-75, 99-106` and `Editor.svelte:57-66, 412-419`.
- **Failure:**
  1. Save #1 is in flight while the user keeps typing, so save #2 is waiting on its timer.
  2. #1 gets a 409. `onConflict` then `reopen()` reloads the page and records the latest revision.
  3. The 500 ms timer is never cleared, and `pendingContentJson`, which was built on the stale copy, is still queued.
  4. #2 fires with the *fresh* revision, and the server accepts the stale document over the other writer's content.
  5. The editor now shows the other writer's version, but the server holds ours.
- **Repro:** the saves were `['ours-1','ours-2-stale']` and the server ended with `ours-2-stale`.
- **Fix:** on a 409, clear `saveTimer` and the pending state for that page. Alternatively, keep a per-page generation that `reopen` bumps and `persistPending` checks.

### DI-05 — Lost updates from read-modify-write PATCH
- **Where:**
  - Tasks: `task_handler.go:281-311` → `task_store.go:319-339`.
  - Pages: `page_handler.go:135-195` → `page_store.go:166-183`.
  - Also lanes (`lane_store.go:200-214`), templates (`template_store.go:116-130`) and OAuth clients.
- **Failure:** each handler reads the row, merges the patch in Go, and writes every column back, with no transaction, row lock or precondition. For example, the editor PATCHes a task title while the board PATCHes `status: done` at the same moment. Whichever write lands last puts the other field back to its old value. This is common on shared notes.
- **Frontend amplifiers:**
  - Rollbacks restore a snapshot taken at call time, which can undo another successful change (`pages.svelte.ts:119-137`, `lanes.svelte.ts:85-101`).
  - Server responses that arrive out of order overwrite newer optimistic state. On the task page this makes `MarkdownEditor` revert the description while the user is typing (`tasks.svelte.ts:152-181`, `MarkdownEditor.svelte:52-57`).
- **Fix:**
  - Server: write only the fields that were sent (a dynamic `SET`), or use `SELECT … FOR UPDATE` in a transaction, or add an `updated_at` precondition.
  - Client: keep a sequence number per record and ignore responses older than the latest optimistic patch.

### DI-06 — Page-tree cycles through PUT (and a race through PATCH)
- **Where:**
  - `page_handler.go:231-268`: `UpsertPage` calls only `CanUseParent`.
  - `page_store.go:113-146`.
  - The CTEs: `page_store.go:199-239` and `store/access.go:90-96`.
- **Failure:** `PUT /pages/F {parentId: F}`, or pointing a page at one of its descendants, stores a cycle. The subtree disappears from the tree. `GetDescendantIDs` and `IsAncestor` use `UNION ALL` with no cycle guard, and no `statement_timeout` is set, so those queries run until memory or temp space runs out. The memstore versions loop forever while holding the registry lock.
  - Two concurrent PATCH moves (A under B, B under A) both pass `IsAncestor`.
  - PUT also resets every field it wasn't given: an omitted `isPrivate` becomes `false` (the page becomes org-visible), and order, tags, priority and `todoTrigger` are reset to zero.
- **Fix:** share one cycle check between PUT and PATCH. Run the check and the write in one transaction with row locks, or with an advisory lock per user. Add `CYCLE … SET is_cycle` or a depth limit to the CTEs. Set a `statement_timeout` for the pool.

### DI-07 — `todoTrigger` stored as JSONB `null` disables TODO detection
- **Where:** `page_store.go:522-532` (`marshalNullableJSON(v interface{})`) and the same pattern in `template_store.go:71/101/117`.
- **Failure:**
  - A nil `*TodoTriggerConfig` wrapped in `interface{}` is not `== nil`, so `json.Marshal` returns `"null"`, which is stored as JSONB `null` rather than SQL NULL.
  - `scanPage` reads it back as `&TodoTriggerConfig{}` with an empty pattern.
  - The frontend's `?? DEFAULT` doesn't replace that empty object, and `matchesTrigger("")` is false.
  - Result: **no TODO bullet ever creates a task** on pages created without a template. The next PATCH writes the empty config back for good.
  - On templates, `TemplateEditForm` reads `blockTypes[0]` on `null`, which throws a TypeError.
  - The memstore keeps the nil pointer, so tests can't see this.
- **Fix:**
  - Use a typed helper that returns nil for a nil pointer, and treat a scanned `null` as nil.
  - Backfill: `UPDATE pages SET todo_trigger = NULL WHERE todo_trigger = 'null'::jsonb`, and the same for templates.
  - Make the frontend and Go agree on what an empty pattern means (Go falls back to "TODO"; see DI-27).

### DI-08 — MCP replace-mode writes: no concurrency guard, and the markdown round-trip loses data
- **Where:** `api/internal/mcp/tools_pages.go:686,707`, `api/internal/pmmd/*`.
- **Failures:**
  - **No guard:** when `expected_revision` is omitted, the tool re-reads the page just before writing and uses *that* revision. An agent that read rev 5 overwrites a user's rev 6 edits, and the tasks for any bullets the user added are soft-deleted.
  - **Lossy round-trip** (each item reproduced):
    - If the agent drops a `<!-- task:ID -->` marker, the old task is soft-deleted, losing its status, due date, description and tags, and a *new* duplicate task is created for the same bullet.
    - Every bullet without a task gets a new nodeId.
    - Ordered-list `start` is lost (`parse_block.go:378`).
    - The `underline` and `highlight` marks are lost, as are link `target`, `rel` and `class`.
    - Consecutive empty paragraphs collapse into one.
    - Tables and setext headings flatten into one paragraph.
    - A duplicated marker produces two list items with the same `taskId`. A marker naming another page's task is kept and left dangling.
  - **`create_task` with a `page_id`** (`tools_tasks.go:387-415`): it creates the task, then writes the bullet. If the write fails, for example with a collab 409, which is the usual case for an open note, the task stays live without its bullet and is soft-deleted at the next save. The agent was told the task was created. `putContent` also maps every 409 to "retry", so agents retry forever against an open collab note.
  - `linkTodoBullets` runs over the whole doc even in append mode, so it creates tasks for existing unlinked TODO bullets.
- **Fix:**
  - Require `expected_revision` in replace mode.
  - Implement replace as a structural merge against the previous doc: carry nodeIds forward, keep unknown marks and attributes, parse `start`.
  - Refuse replace when a linked bullet disappears, unless the caller confirms it.
  - Drop duplicate or foreign `taskId`s.
  - Roll back `create_task` when the bullet write fails, and return the collab 409 as its own error.

### DI-09 — Enter at the start of a linked bullet moves the task link to the wrong bullet
- **Where:** `src/lib/editor/extensions/TaskLinkExtension.ts:51-80` (`keepOnSplit: false`), TipTap `splitListItem`, and `TodoDetectionExtension.ts:97, 209-228`.
- **Failure:** the cursor is at offset 0 of "Buy milk" (task T) and the user presses Enter. The *empty* upper bullet keeps `{nodeId, taskId: T, status}`. The "Buy milk" bullet gets a new nodeId and a duplicate task. Typing into the empty bullet renames T, so T's due date, description and status now belong to the wrong item. A split in the middle of the text leaves T's title out of sync with its bullet. This happens in both REST and collab modes.
- **Fix:** handle Enter (or add an `appendTransaction`) so that a split at offset 0 keeps the identity attributes on the half that has the text, and resync the title after any split.

### DI-10 — Duplicate `nodeId`/`taskId` on paste; moving a bullet between notes deletes its task
- **Where:**
  - `parseHTML` reads `data-node-id` and `data-task-id` (`TaskLinkExtension.ts:54,61`).
  - Paste dedupe exists only in `CollabNodeIdExtension`, which is loaded only when collab is on (`Editor.svelte:244-249`).
  - Reconcile and `CreateLinked` are scoped to `source_page_id` (`page_store.go:458-481`, `task_store.go:250-256`).
- **Failures:**
  - **Pasting within a page:** two bullets point at one task, and edits to either one overwrite its title.
  - **Cutting from A and pasting into B:**
    1. A's save soft-deletes T.
    2. B's reconcile never restores T.
    3. B's bullet keeps `taskId=T`, so no new task is created.
    4. The task and its metadata are gone, with no error shown.
  - **Copying from A to B:** edits in B rename, and change the status of, A's task.
  - **localStorage mode** (`useBulletRemoval.ts:95-101`): the delete is a *hard* delete after 1 s. A cut followed by a paste or undo more than 1 s later loses the task. Navigating away within 1 s leaves the task orphaned.
- **Fix:**
  - Run the paste dedupe in all modes: a new nodeId, and strip any `taskId` that is already present.
  - When a pasted `taskId` belongs to another page, move that task to the new page instead of leaving a dangling link.
  - In local mode, soft-delete and restore when the bullet reappears.

### DI-11 — Collab with more than one replica: skipped log rows, and eviction that drops unpersisted edits
The default is 1 replica (`values.yaml:124`), but this applies whenever old and new pods overlap during a rollout, and whenever someone scales up. The Helm comments say "more are safe".

- **Skipped row** (`collab/src/extension.ts:517-541, 601-607`):
  1. Replica A catches up to seq 100.
  2. Replica B appends 101.
  3. A appends 102 and sets `lastSeq = 102`.
  4. `fetchSince(102)` never returns 101.

  A's clients never see B's edit. A snapshots with `upToSeq=102`, the API accepts it, and `page_contents` loses the edit. Reconcile then soft-deletes the tasks for bullets that were in B's edit. It heals only at the next compaction.
  - A related case *(Likely)*: BIGSERIAL assigns `seq` at INSERT, not at commit, so seq 10 can commit after seq 11 has already been read.
- **Stale-snapshot eviction** (`page_collab_store.go:134-135`, `collab/src/api.ts:73`, `extension.ts:588-590`):
  - A snapshot for seq N that arrives after B's snapshot for N+1 gets a 409, which is mapped to `stale`.
  - The replica then runs `evict(Reset)` and throws away `pending`, and clients discard their Y.Docs.
- **Fix:**
  - Allocate seqs in commit order, using a counter per page that is incremented under the `page_collab_docs` row lock.
  - Or never advance `lastSeq` past rows that haven't been applied.
  - Return a separate "behind" code that makes the replica catch up and retry instead of evicting.
  - Add tests that inject a foreign append between catch-up and append.

### DI-12 — Collab: reopening during the unload flush deletes the new session's state
- **Where:** `collab/src/extension.ts:465-490`.
- **Failure:** this happens with 1 replica.
  1. A persist has failed, so there is pending state or a scheduled retry.
  2. The last client leaves and the unload flush starts.
  3. Another user opens the note, and a new state is registered.
  4. The flush finishes and calls `this.docs.delete(name)`, which deletes the *new* state.
  5. From then on `onStoreDocument` does nothing, `beforeSync` throws Reset, and the new user's edits are never persisted.

  A second path *(Likely)*: a batch that fails during the flush is put back onto the parked state, which has been evicted and never retries.
- **Fix:** guard the teardown with `if (this.docs.get(name) === state)`. In `onLoadDocument`, wait for the parked state's chain before carrying its pending edits over.

### DI-13 — Sidebar sibling reorder always fails in API mode
- **Where:** `src/lib/components/sidebar/TreeNodeItem.svelte:195` sends `node.order + 0.5`, but `api/internal/handler/dto.go:17` declares `Order *int`.
- **Failure:** Go's JSON decoder rejects the fractional number and the API returns 400. The store rolls back, and `handleDrop` doesn't catch the error, so no toast appears. It works in local mode. No data is corrupted, but the user's reorder is silently not saved.
- **Fix:** compute integer orders on the client (renumber the siblings), or add a batch reorder endpoint. Catch the error and show a toast.

---

## Medium

| # | Issue | Where | Notes |
|---|---|---|---|
| DI-14 | **Collab kill switch loses edits.** Setting `COLLAB_ENABLED=false` makes the API refuse snapshots (`collab_handler.go:108-111`). Edits in the log are never snapshotted, and the next REST save detaches the page with stale `page_contents` (`page_store.go:337-343`). This contradicts "turning it off is safe" in `values.yaml:118-122`. | collab + API | Either accept final snapshots while a page is still attached, or rebuild from the log on detach when `snapshot_seq` is behind. |
| DI-15 | **Collab SIGTERM during a persistence outage.** Hocuspocus finishes destroying once its document count is 0. Parked states that still have retries pending aren't counted, so `onDestroy` closes the pool and the process exits (`server.ts:57-62`). | collab | Drain `this.docs` with a time limit in `onDestroy`, and add a `preStop` hook and a longer grace period. |
| DI-16 | **Schema-upgrade reseed during a rolling deploy** throws away the old replica's unappended edits, up to about 10 s, and resets its clients (`extension.ts:350-366, 526-539`). | collab | Check that the page is idle (a lease) before reseeding, or drain old replicas first. |
| DI-17 | **Mixed-version window on every rollout.** Migrations run in the init container of each new API pod *and* in a Job that isn't a hook, while the old pods are still serving. CD rolls the API twice per deploy. Nothing enforces expand/contract migrations. | helm, `cd.yml:150-166` | Use one `pre-upgrade` hook Job with `--atomic --wait`, drop the redundant `rollout restart`, and adopt an expand/contract policy. |
| DI-18 | **Migration pipeline failure modes.** A failed migration leaves the version dirty, so every new pod crash-loops until someone runs `migrate force` by hand. A `helm rollback` ships fewer migration files, so pods crash with "no migration found for version N" on any reschedule. `scripts/deploy.sh` bypasses CD's concurrency lock and pushes `:latest` while the pull policy is `IfNotPresent`. | helm, scripts | Have migrate tolerate a database version newer than its files, write a runbook for the dirty state, and retire or harden `deploy.sh`. |
| DI-19 | **Migration 000017 fails on legacy NULL content.** 000013 turned `''` into NULL, but `page_content_versions.content` is `NOT NULL`, so the seeding INSERT violates it and leaves the database dirty. | `000017_*.up.sql` | Likely. Add `WHERE content IS NOT NULL` or a `COALESCE`. |
| DI-20 | **Org owner integrity.** `POST /orgs/:id/members` for an existing member upserts the role, defaulting to viewer (`org_store.go:114-129`). That can demote the sole owner and leave the org ownerless. The last-owner checks are check-then-act with no lock. `CreateOrg` isn't atomic, so a failure after the create leaves an org with no members. | org handler/store | Use `DO NOTHING` or return 409 on conflict, count owners and write in one locked transaction, and wrap `CreateOrg` in a transaction. |
| DI-21 | **Deleting a note leaves its tasks alive as orphans.** `source_page_id ON DELETE SET NULL` keeps the stale `source_node_id`, and the tasks lose collaborator access. Folder-board tasks lose their folder. Removing a single bullet soft-deletes its task, so the two paths behave differently. | migrations 000001, 000014 | In the delete transaction, soft-delete the tasks for the whole subtree. |
| DI-22 | **Shares are never cleaned up.** `shares.resource_id` has no FK and nothing deletes shares when a resource is deleted. PUT with a client-chosen ID re-grants the old recipients access to the new resource. Changing a page's `type` (page↔folder) makes its shares impossible to revoke, because the share handler matches on type. | migration 000007, `share_handler.go` | Delete shares in the same transaction as the resource, garbage-collect existing orphans, and make `type` immutable. |
| DI-23 | **Cross-owner and cross-org references aren't validated.** PATCH `/tasks/:id` re-points `sourcePageId`/`sourceNodeId` with no page check. `folderId` on task and lane create/upsert is never checked, so rows can be planted on anyone's folder board. PATCH ignores an explicit `orgId: null`, so "move to Personal" silently fails, and the same applies to template `defaultFolderId`/`todoTrigger`. A task never inherits its page's `org_id`, and org changes don't cascade to descendants or tasks. | `task_handler.go:274-322`, `lane_handler.go`, `dto.go` | Run `resolveSourcePage`/`CanWriteFolder` on these fields, use `bindJSONWithKeys` for nullable fields, and derive org/privacy from the source page. |
| DI-24 | **The validator strips attributes the schema supports.** `orderedList.start`, `orderedList.type` and link `title` are missing from `allowedAttrs`, so every REST save and collab snapshot drops them, while the live Yjs doc still has them. They are lost for good after any detach. | `content_validator.go:29` | Fixed by the schema-derived allowlist (DI-01). |
| DI-25 | **Lane integrity on the frontend.** Saving a lane's config drops `sortConfig.taskOrder` (`LaneConfig.svelte:107-112`). A manual reorder while a search is active saves only the visible tasks as the full order (`Lane.svelte:175`). Folder-board lane renames and orders go through the global store and are silently dropped in API mode (`Lane.svelte:173-201`). Folder-board reorder uses N separate PATCHes, so a partial failure leaves mixed orders. There is no generation guard on folder-board loads. | lanes/folderBoard | Merge the new order into the existing one, pass update callbacks down, and use a batch endpoint. |
| DI-26 | **localStorage-mode integrity.** A corrupt JSON value is read as `[]` and then overwritten by the seed data (`LocalStorageAdapter.ts:16-20`, `Repository.ts:70-75`). Folder lanes leak into the global board, and reorder there renumbers them. Deleting a folder from a stale tab orphans children created in another tab. There's no revision check on content, so two tabs silently overwrite each other. A deleted folder that is still a template's `defaultFolderId` makes new pages unreachable. | `src/lib/storage/*` | Back up corrupt values and refuse to write over them, filter out folder lanes, compute descendants inside the serialized write, and clear `defaultFolderId` on delete. |
| DI-27 | **The frontend and Go derive TODOs differently.** The frontend creates tasks for *empty* bullets and Go doesn't. An empty trigger pattern means "no match" in JS but "TODO" in Go. The regex dialects differ (JS vs RE2). Go doesn't set `checked` for cancelled tasks. | `useTaskCreation.ts:61-76`, `pmmd/todo.go:94,142` | Share one fixture file between both test suites. |
| DI-28 | **Tasks created during navigation are never linked.** When `createTask` resolves after a page switch, the bullet is saved without a `taskId`. On reopen, `getByNodeId` finds the task but never calls `setTaskIdForNode` (`useTaskCreation.ts:64-65, 87-90`). | editor | Link it in the `existing` branch. |
| DI-29 | **Task title and bullet text drift apart.** Nothing syncs a task title back to its bullet (`useTaskSync.ts:90-95`), so a rename on the task page or through MCP `update_task` is reverted by the next keystroke in the bullet. The title and tag inputs on the note and task pages are reset by effects that fire on any change to the record, which clobbers an edit in progress. | editor, routes | Push `task.title` into the bullet on load and on external change, and sync inputs only when not editing. |
| DI-30 | **Edits lost on tab close.** In REST mode `beforeunload` only warns for collab, so the ~500 ms content debounce, the 600 ms task-description debounce and the pending title debounces are dropped when the tab closes (`Editor.svelte:438-446`, `tasks/[taskId]/+page.svelte:96-114`). The collab sync ack also comes before the data is durable, which leaves a window of up to 10 s. | editor | Warn and flush on `pagehide`/`visibilitychange` using a keepalive fetch, and count armed timers in `hasPendingWrites`. |

## Low

- **Missing `content` wipes the page.** `PUT /pages/:id/content` without `content` writes an empty doc and soft-deletes every task (`page_handler.go:325`, `page_store.go:408-410`). It should return 400.
- **Size limit counted two ways.** Collab counts the size limit in UTF-16 units (`documentRules.ts:50-51`) and the API counts bytes. A large CJK or emoji doc passes collab, the API rejects it, and the page is quarantined.
- **Body-size limits below the 5 MB content limit.** Dev nginx is at the 1 MB default. The ingress-nginx hint is commented out (`values.yaml:237`). The SvelteKit proxy defaults to 512 KB (`BODY_SIZE_LIMIT`).
- **`WriteTimeout: 15s`** (`server.go:47`) can drop the response after the write has committed. The client retries and gets a spurious 409.
- **Node-id auto-assign save bypasses serialization.** `Editor.svelte:157-179` saves outside the `useContentSave` queue, which can cause a spurious 409, a reload, and lost edits.
- **A failed title flush blocks navigation.** `Editor.svelte:474-477` chains `.then(openPage)`, so if the flush rejects, the next page opens read-only with the old content.
- **`CreateLinked` restores `deleted_reason='user'` tasks** (`task_store.go:262-274`), contradicting the migration's rule. *(Speculative reachability.)*
- **Templates copy identity attributes.** `evaluateContentTemplate` copies `nodeId`/`taskId` verbatim (`titleTemplate.ts:46-61`).
- **Deleting a task leaves its bullet linked.** DELETE `/tasks/:id` leaves the bullet with a dangling `taskId`. MCP `ToMarkdown` renders checkbox state from a stale `taskStatus` attribute.
- **`users.Upsert` nulls email and name** when the IdP omits the claims. `GetByEmail` isn't unique or case-folded, so a share can reach the wrong account.
- **Unstable pagination.** Pages are ordered by `"order"` alone, which has many ties. Add `, id` as a tie-breaker.
- **Destructive or failing down migrations.** 000004 down deletes every lane named "Cancelled". 000010 down fails once order values exceed int4. 000014 down fails if folder shares exist. 000021 can't be reversed. None of these is run by automation.
- **Operations gaps.** No PodDisruptionBudgets for the api, frontend or collab. No `preStop` hooks. CD never waits for the collab rollout.
- **Partial writes.** `setDefault` on templates isn't atomic. The MCP rollback deletes tasks one at a time.
- **Latent code.** `orderBetween` (`order.ts:34`) returns a colliding value when there's no gap; it's unused. `importData` would need to invalidate the repository cache if it is ever wired up.

## Memstore vs Postgres divergences (these hide bugs from tests)

- `Page.Delete` doesn't cascade to children, tasks, lanes, versions or collab state, and it returns nil instead of `ErrNotFound`. `Task.Delete` also returns nil.
- `Org.Delete` doesn't null `org_id` on the org's resources.
- Shares have no uniqueness check (Postgres returns a unique violation, which becomes a 500).
- `TodoTrigger` stays nil, which hides DI-07.
- `AddMember` resets `JoinedAt`.
- `CreateLinked` returns the existing task where Postgres returns `ErrConflict`.
- Error types are untyped, so `errors.Is(err, ErrNotFound)` fails.

Running the integration suite against both backends (the harness already exists) and fixing these would catch DI-02, DI-07 and DI-21.

## Test gaps worth closing

- Stored content with an unknown node or mark → the editor must not save (DI-01).
- 409 while another save is waiting (DI-04).
- Split at offset 0 of a linked bullet, and a paste of a linked bullet with collab off (DI-09, DI-10).
- A collab append injected between catch-up and append, concurrent snapshots from two replicas, and a reopen during the unload flush (DI-11, DI-12).
- A PUT that creates a cycle, and concurrent PATCH moves (DI-06).
- A `todoTrigger` round-trip through Postgres (DI-07).
- MCP replace with a dropped task marker (DI-08).
- Folder delete containing another user's page (DI-02).

## Areas reviewed and found sound

- **Content write path:** `UpsertContent` takes a row lock, requires `expectedRevision` once content exists, and archives, prunes and reconciles tasks in one transaction. Restore and collab snapshots share that path.
- **Linked tasks:** `CreateLinked` is idempotent under concurrency. The unique index on `(source_page_id, source_node_id)` includes soft-deleted rows. Reconcile is idempotent and restores only `source_removed` tasks.
- **Collab safeguards:** seeding under a lock (no double seed), epoch binding, the shadow-doc check on incoming updates, the snapshot seq/epoch guard, JSON-based server conversion, the schema fingerprint gate, and the document-name→pageId mapping.
- **Editor guards:** the `loadedPageId`/`loadGeneration` checks, and at most one save in flight at a time.
- **PATCH DTOs** use pointers, and `parentId`/`dueDate`/`link` nulls are handled.
- **Order values** are BIGINT and around 1.8e15, below 2^53, so there is no overflow or precision loss.
- **Frontend cycle prevention** on drag-and-drop moves, on both the client and the server's PATCH path.
- **Secrets:** no `randAlphaNum` regeneration. The session secret is required in release mode. DB credentials are managed by the operator.
- **Migrations:** golang-migrate takes an advisory lock, and each file runs in one implicit transaction.
- **OAuth token consume/rotate** are atomic conditional UPDATEs.
- **API graceful shutdown** drains with `srv.Shutdown`.

## Suggested remediation order

1. **Before anything else:** DI-01 (content check and allowlist), DI-03 (enable backups and PITR, add `resource-policy: keep`), and DI-07 (typed nil fix plus backfill).
2. **Next sprint:** DI-04, DI-06, DI-02 (soft delete), DI-08 (require `expected_revision`), DI-12 (a one-line guard), DI-13.
3. **Then:** DI-05 (field-level updates), DI-09/10 (task-link identity), DI-11 (seq allocation), and the Medium table.
