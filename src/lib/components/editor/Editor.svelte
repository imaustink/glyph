<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { Editor, type AnyExtension } from '@tiptap/core';
  import Placeholder from '@tiptap/extension-placeholder';
  import Collaboration from '@tiptap/extension-collaboration';
  import CollaborationCaret from '@tiptap/extension-collaboration-caret';
  import type { Transaction } from '@tiptap/pm/state';
  import { goto } from '$app/navigation';
  import { documentExtensions, COLLAB_FRAGMENT } from '$lib/editor/schema';
  import { TodoDetectionExtension, type DetectedBullet } from '$lib/editor/extensions/TodoDetectionExtension';
  import { NodeIdMapExtension } from '$lib/editor/plugins/NodeIdMapPlugin';
  import { CollabNodeIdExtension } from '$lib/editor/plugins/CollabNodeIdExtension';
  import { tasksStore } from '$lib/stores/tasks.svelte';
  import { pagesStore } from '$lib/stores/pages.svelte';
  import { uiStore } from '$lib/stores/ui.svelte';
  import { authStore } from '$lib/stores/auth.svelte';
  import { notificationsStore } from '$lib/stores/notifications.svelte';
  import { useContentSave } from '$lib/editor/useContentSave';
  import { useTaskCreation, type PendingTaskDetails } from '$lib/editor/useTaskCreation';
  import { useTaskSync } from '$lib/editor/useTaskSync';
  import { useBulletRemoval } from '$lib/editor/useBulletRemoval';
  import { storageMode } from '$lib/storage/config';
  import { CollabSession } from '$lib/collab/CollabSession';
  import { collabSupported, collabWebSocketUrl, getCollabSession } from '$lib/collab/client';
  import { isLocalTransaction } from '$lib/collab/isLocalTransaction';
  import { collabUserColor } from '$lib/collab/userColor';
  import { CollabReason, type CollabReasonValue } from '$lib/collab/protocol';
  import TaskCreationPopover from './TaskCreationPopover.svelte';
  import { nanoid } from 'nanoid';

  let {
    pageId,
    onchange
  }: {
    pageId: string;
    onchange?: () => void;
  } = $props();

  let editorEl = $state<HTMLDivElement | null>(null);
  let editor = $state<Editor | null>(null);
  let contentLoaded = $state(false);

  // The page whose content is *actually* in the editor right now. This lags
  // `pageId` during navigation because loadContent() is async: `pageId` updates
  // synchronously while the editor still holds the previous page's document.
  // Every write/sync path must key off this rather than `pageId`, otherwise a
  // transaction dispatched mid-navigation saves the old document under the new
  // page's id — the cause of the 2026-09-21 data-loss incident.
  let loadedPageId = $state<string | null>(null);

  // Reactive state for template rendering
  let pending = $state<PendingTaskDetails | null>(null);
  let removedBulletTimer: ReturnType<typeof setTimeout> | null = null;

  // ─── Composables ────────────────────────────────────────────────────────────

  const contentSave = useContentSave(
    () => onchange?.(),
    // On a save conflict the server has newer content than we based our edit
    // on — or the page is now edited collaboratively. Re-open it rather than
    // retrying, which would overwrite the newer copy; re-opening also picks
    // the collaborative editor when that is what the server now expects.
    (conflictedPageId) => {
      if (conflictedPageId === loadedPageId) reopen(conflictedPageId);
    }
  );

  const taskCreation = useTaskCreation(
    () => editor,
    (p) => { pending = p; },
    () => loadedPageId
  );

  const taskSync = useTaskSync(() => editor, () => pageId);

  // In API mode the server reconciles tasks with the saved document; the
  // editor only mirrors that locally and never deletes tasks itself.
  const bulletRemoval = useBulletRemoval({ serverReconciles: storageMode === 'api' });

  // ─── Editing mode ───────────────────────────────────────────────────────────
  //
  // Each page opens in one of two modes, decided by the API when the page is
  // opened:
  //
  //  - 'rest': single-writer editing. The document is loaded, edited locally
  //    and saved whole with an optimistic-concurrency revision. The TipTap
  //    editor is reused across pages (setContent on navigation).
  //
  //  - 'collab': realtime collaborative editing. The document lives in a Yjs
  //    doc synced through the collab service; nothing is saved from here. A
  //    fresh TipTap editor and CollabSession are created per page, and only
  //    once the session holds the server's content — so no local transaction
  //    can write into the shared document before it is populated.
  let mode: 'rest' | 'collab' | null = null;
  let session: CollabSession | null = null;
  /** Discards the continuation of a page open superseded by a newer one. */
  let openGeneration = 0;
  let mounted = false;

  // ─── Public API ─────────────────────────────────────────────────────────────

  export function focus() {
    editor?.commands.focus();
  }

  export async function flushAll() {
    await contentSave.flushAll();
  }

  // ─── Reactive sync ──────────────────────────────────────────────────────────

  // Sync external task status changes (e.g. from the task board) back into the editor.
  // Uses diff-based sync to avoid dispatching ProseMirror transactions when nothing changed.
  $effect(() => {
    // Svelte tracks tasksStore.tasks (new array ref on every mutation)
    if (!editor) return;
    // Only sync once the document in the editor is the one `pageId` refers to.
    // Without this guard, a tasksStore mutation during navigation dispatches
    // transactions against the *previous* page's document, and the resulting
    // onUpdate persists it under the new pageId.
    if (!contentLoaded || loadedPageId !== pageId) return;
    taskSync.syncExternalStatusChanges(editor, pageId);
  });

  // ─── Content loading ────────────────────────────────────────────────────────

  /** Monotonically increasing generation counter to discard stale loadContent responses. */
  let loadGeneration = 0;

  async function loadContent() {
    if (!editor) return;
    const gen = ++loadGeneration;
    // Capture the target page up front — `pageId` may change while we await.
    const targetPageId = pageId;
    const content = await pagesStore.getContent(targetPageId);
    // Discard response if a newer loadContent was triggered while we were awaiting
    if (gen !== loadGeneration) return;
    if (content?.content && Object.keys(content.content).length > 0) {
      editor.commands.setContent(content.content as Record<string, unknown>, { emitUpdate: false });
    } else {
      editor.commands.setContent('', { emitUpdate: false });
    }
    // From here on the editor genuinely holds targetPageId's document, so
    // writes keyed off loadedPageId are safe.
    loadedPageId = targetPageId;
    taskCreation.clearPrompted();
    bulletRemoval.snapshot(editor);
    taskSync.syncTaskStatuses(editor, targetPageId);
  }

  /**
   * Assign nodeIds to any list items missing them (content migration on load).
   * If any IDs are assigned, saves immediately so the document starts clean
   * and subsequent user edits don't trigger a spurious "dirty" save for
   * what is effectively a schema migration.
   */
  function scheduleAutoAssignNodeIds() {
    if (!editor) return;
    const { state, dispatch } = editor.view;
    const tr = state.tr;
    let changed = false;

    state.doc.descendants((node, pos) => {
      if (node.type.name === 'listItem' && !node.attrs.nodeId) {
        tr.setNodeMarkup(pos, undefined, { ...node.attrs, nodeId: nanoid() });
        changed = true;
      }
    });

    if (changed) {
      dispatch(tr);
      // Save under the page this document actually belongs to, not the
      // possibly-newer reactive pageId.
      if (loadedPageId) {
        pagesStore.saveContent(loadedPageId, editor.getJSON() as Record<string, unknown>);
      }
    }
  }

  // ─── Pending popover helpers ────────────────────────────────────────────────

  function dismissPendingIfCursorLeft(ed: Editor) {
    if (!pending) return;
    const from = ed.state.selection.$from;
    for (let depth = from.depth; depth > 0; depth--) {
      const node = from.node(depth);
      if (node.type.name === 'listItem' && node.attrs.nodeId === pending.nodeId) {
        return;
      }
    }
    pending = null;
  }

  function handleTaskDetailsClose() {
    pending = null;
  }

  function handlePendingTitleChange(title: string) {
    if (!pending || !editor) return;
    editor.commands.setBulletTextForNode(pending.nodeId, title);
    pending = { ...pending, bulletText: title };
  }

  // ─── Lifecycle ──────────────────────────────────────────────────────────────

  /**
   * Build a TipTap editor. The schema-defining extensions come from
   * documentExtensions(), the same list the collab service builds its schema
   * (and the schema fingerprint it checks) from.
   *
   * @param collabPageId set for a collaborative editor: the page it is bound
   *   to for its whole life. (A single-writer editor follows `pageId`.)
   */
  function createEditor(s: CollabSession | null, collabPageId: string | null): Editor {
    const collab = s !== null;
    const extensions: AnyExtension[] = [
      ...documentExtensions({
        // Local undo history would undo other people's edits; the
        // Collaboration extension brings a Yjs UndoManager that only undoes
        // this user's own changes.
        undoRedo: !collab,
        taskLink: {
          onStatusCycled: (nodeId: string, taskId: string, currentStatus: string) => {
            taskSync.handleStatusCycled(nodeId, taskId, currentStatus);
          },
          onTaskClicked: (taskId: string) => {
            void goto(`/tasks/${taskId}`);
          }
        }
      }),
      NodeIdMapExtension,
      Placeholder.configure({
        placeholder: 'Start writing… Create a heading named TODO to track tasks.'
      }),
      TodoDetectionExtension.configure({
        onTodoBulletsDetected: (bullets: DetectedBullet[]) => {
          taskCreation.handleTodoBulletsDetected(bullets);
        },
        pageId: collab ? () => collabPageId ?? '' : () => pageId,
        todoTrigger: () => pagesStore.getById(collab ? (collabPageId ?? '') : pageId)?.todoTrigger,
        localChangesOnly: collab
      })
    ];
    if (s) {
      extensions.push(
        Collaboration.configure({ document: s.doc, field: COLLAB_FRAGMENT }),
        CollaborationCaret.configure({ provider: s.provider, user: s.user }),
        CollabNodeIdExtension
      );
    }

    return new Editor({
      element: editorEl!,
      extensions,
      editorProps: {
        attributes: {
          class: 'tiptap-editor',
          spellcheck: 'true'
        }
      },
      // Single-writer: disabled until the page is loaded — otherwise a
      // keystroke landing before setContent() replaces the whole document
      // gets silently discarded (or appended to the stale template content).
      // Collaborative: only created once the content is there, so it starts
      // in its final state; toggling editable right after creation left
      // ProseMirror ignoring the first clicks' selection for a moment.
      editable: s ? s.canEdit : false,
      onSelectionUpdate: ({ editor: ed }) => {
        dismissPendingIfCursorLeft(ed);
      },
      onUpdate: ({ editor: ed, transaction }) => handleUpdate(ed, transaction)
    });
  }

  function handleUpdate(ed: Editor, transaction: Transaction) {
    // Pushing a bullet's text to its task is a side effect: only for this
    // user's own typing, or every collaborator would push the same title.
    if (isLocalTransaction(transaction)) {
      taskSync.syncLinkedTaskTitleRealtime(
        ed,
        () => pending,
        (p) => { pending = p; }
      );
    }
    dismissPendingIfCursorLeft(ed);

    // Debounce removal detection. In API mode this only updates the local
    // task list (the server reconciles tasks), so it runs for remote edits too.
    if (removedBulletTimer) clearTimeout(removedBulletTimer);
    removedBulletTimer = setTimeout(() => bulletRemoval.detectRemovedTaskBullets(ed), 1000);

    // Collaborative documents are persisted by the collab service.
    if (mode !== 'rest') return;
    // Debounced content save. Key off loadedPageId — the page the document
    // in the editor actually came from. Using the reactive `pageId` here
    // meant any transaction dispatched during navigation (task sync, async
    // task creation, nodeId migration) persisted the previous page's
    // document under the new page's id.
    if (!loadedPageId) return;
    contentSave.scheduleSave(ed, loadedPageId);
  }

  /** Destroy the current editor and collaborative session, if any. */
  function teardownEditor() {
    editor?.destroy();
    editor = null;
    session?.destroy();
    session = null;
    mode = null;
    uiStore.setCollabState(null);
  }

  /**
   * Open `target` in the mode the API chooses for it. Callers have already
   * made the editor non-editable and cleared loadedPageId.
   */
  async function openPage(target: string) {
    const gen = ++openGeneration;
    let collab = false;
    if (collabSupported) {
      try {
        collab = (await getCollabSession(target)) !== null;
      } catch (err) {
        // Can't tell; single-writer mode is safe either way (the API refuses a
        // whole-document write to a collaborative page rather than applying it).
        console.warn('[Editor] Could not check collaborative mode; editing single-writer', err);
      }
    }
    if (gen !== openGeneration || !mounted) return;
    if (collab) await openCollaborative(target, gen);
    else await openSingleWriter();
  }

  async function openSingleWriter() {
    const fresh = mode !== 'rest';
    if (fresh) {
      teardownEditor();
      editor = createEditor(null, null);
      mode = 'rest';
    }
    await loadContent();
    // Legacy documents may have list items without ids; give them ids once,
    // when an editor first loads a page.
    if (fresh) scheduleAutoAssignNodeIds();
    if (!editor) return;
    bulletRemoval.snapshot(editor);
    editor.setEditable(true);
    contentLoaded = true;
  }

  async function openCollaborative(target: string, gen: number) {
    teardownEditor();
    const s = new CollabSession(
      target,
      {
        onState: (st) => {
          if (session !== s) return;
          uiStore.setCollabState(st);
          // State events fire on every sync acknowledgement, i.e. per
          // keystroke. setEditable() re-applies the view state even when the
          // value is unchanged, which clobbers an in-progress DOM selection
          // (fast typing then acts on the wrong range) — so only toggle it.
          if (editor && editor.isEditable !== s.canEdit) editor.setEditable(s.canEdit);
        },
        onReset: (reason) => {
          if (session !== s) return;
          notifyReset(reason);
          reopen(target);
        },
        onFatal: (reason) => {
          if (session !== s) return;
          notifyFatal(reason);
          editor?.setEditable(false);
          // Collaboration was switched off: fall back to single-writer editing.
          if (reason === CollabReason.Disabled) reopen(target);
        }
      },
      { url: collabWebSocketUrl(), user: { name: authStore.name || authStore.email || 'Someone', color: collabUserColor(authStore.userId) } }
    );
    session = s;
    mode = 'collab';
    uiStore.setCollabState(s.state);

    await s.whenReady();
    if (gen !== openGeneration || session !== s || !mounted) return;

    editor = createEditor(s, target);
    loadedPageId = target;
    taskCreation.clearPrompted();
    bulletRemoval.snapshot(editor);
    // Editable as soon as the content is visible — keystrokes landing while it
    // shows but isn't editable would be silently dropped.
    editor.setEditable(s.canEdit);
    contentLoaded = true;

    // Other people may have changed this page's tasks since we loaded them;
    // refresh, then bring the bullets' status indicators up to date.
    try {
      await tasksStore.refreshForPage(target);
    } catch (err) {
      console.warn('[Editor] Could not refresh tasks for page', err);
    }
    if (gen !== openGeneration || session !== s || !editor) return;
    taskSync.syncTaskStatuses(editor, target);
  }

  /** Re-open the page from scratch (a new session with an empty Y.Doc). */
  function reopen(target: string) {
    if (pageId !== target) return;
    contentLoaded = false;
    loadedPageId = null;
    pending = null;
    teardownEditor();
    void openPage(target);
  }

  function notifyReset(reason: CollabReasonValue) {
    if (reason === CollabReason.InvalidUpdate) {
      notificationsStore.error('Your last change couldn’t be applied, so this note was reloaded.');
    } else if (reason === CollabReason.Reset) {
      notificationsStore.info('This note was replaced (for example, a version was restored). Reloading it.');
    }
  }

  function notifyFatal(reason: CollabReasonValue) {
    if (reason === CollabReason.SchemaMismatch) {
      notificationsStore.error('Glyph has been updated. Reload the page to keep editing this note.');
    } else if (reason === CollabReason.Forbidden) {
      notificationsStore.error('You no longer have access to edit this note.');
    }
  }

  /** Warn before leaving with collaborative edits the server hasn't acknowledged. */
  function handleBeforeUnload(e: BeforeUnloadEvent) {
    if (mode === 'collab' && session?.hasUnsyncedChanges) {
      e.preventDefault();
    }
  }

  onMount(async () => {
    if (!editorEl) return;
    mounted = true;
    prevPageId = pageId;
    window.addEventListener('beforeunload', handleBeforeUnload);
    await openPage(pageId);
  });

  // Open the new page when pageId changes (the initial page is opened by onMount).
  let prevPageId: string | null = null;
  $effect(() => {
    const next = pageId;
    if (!mounted || prevPageId === null || next === prevPageId) return;
    prevPageId = next;
    // Clear transient UI state that is page-scoped
    pending = null;
    if (removedBulletTimer) { clearTimeout(removedBulletTimer); removedBulletTimer = null; }
    contentLoaded = false;
    // Mark the editor as holding no known page until the next one is open.
    // Any transaction dispatched in this window is now a no-op for saving
    // rather than a write of the old document under the new id.
    loadedPageId = null;
    editor?.setEditable(false);
    // Drop per-page task status memory so the next page starts clean and
    // doesn't dispatch spurious status transactions for unrelated tasks.
    taskSync.resetStatusMemory();
    void contentSave.flushAll().then(async () => {
      taskCreation.clearPrompted();
      await openPage(next);
    });
  });

  onDestroy(() => {
    mounted = false;
    openGeneration++;
    const flushPromise = contentSave.flushAll();
    uiStore.registerPendingFlush(flushPromise);
    if (removedBulletTimer) clearTimeout(removedBulletTimer);
    if (typeof window !== 'undefined') window.removeEventListener('beforeunload', handleBeforeUnload);
    contentSave.destroy();
    bulletRemoval.destroy();
    teardownEditor();
  });
</script>

<div class="editor-wrapper" data-content-loaded={contentLoaded}>
  <div bind:this={editorEl} class="editor-mount"></div>
</div>

{#if pending}
  <TaskCreationPopover
    taskId={pending.taskId}
    bulletText={pending.bulletText}
    ontitlechange={handlePendingTitleChange}
    onclose={handleTaskDetailsClose}
  />
{/if}

<style>
  .editor-wrapper {
    height: 100%;
    display: flex;
    flex-direction: column;
  }

  .editor-mount {
    flex: 1;
    overflow-y: auto;
  }

  /* TipTap editor styles */
  :global(.tiptap-editor) {
    min-height: 100%;
    padding: 40px 60px;
    outline: none;
    font-size: var(--font-size-md);
    line-height: 1.75;
    color: var(--text-primary);
    caret-color: var(--accent);
    max-width: 760px;
    margin: 0 auto;
  }

  :global(.tiptap-editor h1) { font-size: 2em; font-weight: 700; color: var(--text-heading); margin: 1.2em 0 0.4em; line-height: 1.25; }
  :global(.tiptap-editor h2) { font-size: 1.5em; font-weight: 600; color: var(--text-heading); margin: 1.1em 0 0.35em; }
  :global(.tiptap-editor h3) { font-size: 1.2em; font-weight: 600; color: var(--text-heading); margin: 1em 0 0.3em; }
  :global(.tiptap-editor h4) { font-size: 1em; font-weight: 600; color: var(--text-heading); margin: 0.9em 0 0.25em; }

  /* Prevent the top margin from snapping in when a paragraph converts to a heading */
  :global(.tiptap-editor > :first-child) { margin-top: 0 !important; }

  /* Other people's carets (collaborative mode) */
  :global(.collaboration-carets__caret) {
    position: relative;
    margin-left: -1px;
    margin-right: -1px;
    border-left: 1px solid;
    border-right: 1px solid;
    word-break: normal;
    pointer-events: none;
  }
  :global(.collaboration-carets__label) {
    position: absolute;
    top: -1.4em;
    left: -1px;
    padding: 0.1rem 0.3rem;
    border-radius: 3px 3px 3px 0;
    color: var(--collab-label-text);
    font-size: var(--font-size-xs);
    font-weight: 600;
    line-height: normal;
    white-space: nowrap;
    user-select: none;
  }

  :global(.tiptap-editor p) { margin: 0.4em 0; }
  :global(.tiptap-editor p.is-editor-empty:first-child::before) {
    content: attr(data-placeholder);
    color: var(--text-muted);
    pointer-events: none;
    float: left;
    height: 0;
  }

  :global(.tiptap-editor ul) { padding-left: 1.5em; margin: 0.3em 0; }
  :global(.tiptap-editor ol) { padding-left: 1.5em; margin: 0.3em 0; }
  :global(.tiptap-editor li) { margin: 0.15em 0; position: relative; }
  :global(.tiptap-editor .list-item-content > p) { margin: 0; }

  /* Task-linked bullets: flex row with a real checkbox, no bullet marker */
  :global(.tiptap-editor li[data-task-id]) {
    list-style-type: none;
    display: flex;
    align-items: flex-start;
    gap: 0.45em;
    cursor: default;
  }

  :global(.tiptap-editor .task-status-wrapper) {
    flex-shrink: 0;
    padding-top: 0.38em;
    line-height: 1;
  }

  :global(.tiptap-editor .task-status-indicator) {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 15px;
    height: 15px;
    border-radius: 50%;
    border: 1.5px solid var(--status-todo);
    background: transparent;
    cursor: pointer;
    margin: 0;
    padding: 0;
    transition: border-color 0.15s ease, background-color 0.15s ease;
    vertical-align: middle;
  }

  :global(.tiptap-editor .task-status-indicator[data-status="todo"]) {
    border-color: var(--text-muted);
    background: transparent;
  }

  :global(.tiptap-editor .task-status-indicator[data-status="in-progress"]) {
    border-color: var(--status-in-progress);
    background: var(--status-in-progress);
  }

  :global(.tiptap-editor .task-status-indicator[data-status="done"]) {
    border-color: var(--status-done);
    background: var(--status-done);
  }

  :global(.tiptap-editor .task-status-indicator[data-status="done"])::after {
    content: '✓';
    font-size: 10px;
    color: var(--bg-primary);
    font-weight: 700;
    line-height: 1;
  }

  :global(.tiptap-editor .task-status-indicator[data-status="cancelled"]) {
    border-color: var(--status-cancelled);
    background: var(--status-cancelled);
  }

  :global(.tiptap-editor .task-status-indicator[data-status="cancelled"])::after {
    content: '✕';
    font-size: 9px;
    color: var(--text-muted);
    line-height: 1;
  }

  :global(.tiptap-editor .list-item-content) {
    flex: 1;
    min-width: 0;
  }

  :global(.tiptap-editor .task-open-link) {
    flex-shrink: 0;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 20px;
    height: 20px;
    margin-top: 0.2em;
    padding: 0;
    border: none;
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--text-muted);
    cursor: pointer;
    opacity: 0.55;
    transition: opacity 0.1s ease, color 0.1s ease, background-color 0.1s ease;
  }

  :global(.tiptap-editor li[data-task-id]:hover .task-open-link),
  :global(.tiptap-editor .task-open-link:focus-visible) {
    opacity: 1;
  }

  :global(.tiptap-editor .task-open-link:hover) {
    color: var(--accent);
    background: var(--bg-tertiary);
  }

  /* Checked / done task: strike-through + muted colour */
  :global(.tiptap-editor li[data-checked="true"] .list-item-content) {
    text-decoration: line-through;
    color: var(--text-muted);
  }

  :global(.tiptap-editor blockquote) {
    border-left: 3px solid var(--border-strong);
    padding-left: 1em;
    color: var(--text-secondary);
    margin: 0.5em 0;
  }

  :global(.tiptap-editor code) {
    background: var(--bg-tertiary);
    border: 1px solid var(--border-subtle);
    border-radius: var(--radius-sm);
    padding: 2px 6px;
    font-family: var(--font-mono);
    font-size: 0.88em;
  }

  :global(.tiptap-editor pre) {
    background: var(--bg-tertiary);
    border: 1px solid var(--border-subtle);
    border-radius: var(--radius-md);
    padding: 14px 18px;
    overflow-x: auto;
    margin: 0.6em 0;
  }

  :global(.tiptap-editor pre code) {
    background: none;
    border: none;
    padding: 0;
  }

  :global(.tiptap-editor strong) { color: var(--text-heading); font-weight: 600; }
  :global(.tiptap-editor em) { color: var(--text-secondary); }

  /* ─── Mobile responsive ─────────────────────────────────────────────────── */
  @media (max-width: 768px) {
    :global(.tiptap-editor) {
      padding: 20px 16px;
      max-width: 100%;
    }
  }

  :global(.tiptap-editor hr) {
    border: none;
    border-top: 1px solid var(--border-subtle);
    margin: 1.5em 0;
  }

  :global(.tiptap-editor .ProseMirror-selectednode) {
    outline: 2px solid var(--accent);
    border-radius: var(--radius-sm);
  }
</style>
