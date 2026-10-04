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
  import { PasteIdentityExtension } from '$lib/editor/plugins/PasteIdentityExtension';
  import { tasksStore } from '$lib/stores/tasks.svelte';
  import { pagesStore } from '$lib/stores/pages.svelte';
  import { uiStore } from '$lib/stores/ui.svelte';
  import { authStore } from '$lib/stores/auth.svelte';
  import { notificationsStore } from '$lib/stores/notifications.svelte';
  import { useContentSave } from '$lib/editor/useContentSave';
  import { useTaskCreation, type PendingTaskDetails } from '$lib/editor/useTaskCreation';
  import { useTaskSync } from '$lib/editor/useTaskSync';
  import { hasPendingTaskTitleUpdates } from '$lib/editor/useTaskTitleDebounce';
  import { useBulletRemoval } from '$lib/editor/useBulletRemoval';
  import { applyStoredContent, checkStoredContent } from '$lib/editor/loadDocument';
  import { selectionInListItem, indentListItem, outdentListItem } from '$lib/editor/listIndent';
  import Icon from '$lib/components/shared/Icon.svelte';
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
  /**
   * Set when the page's stored content can't be represented in the editor
   * schema (DI-01). The editor then stays read-only and nothing is saved:
   * showing — and saving — the blank document TipTap would otherwise load
   * erased the note.
   */
  let contentError = $state<string | null>(null);

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

  // ─── Mobile list toolbar ──────────────────────────────────────────────────
  // Touch keyboards (iOS/Android) have no Tab key, so on mobile there was no way
  // to nest a list — the desktop Tab / Shift-Tab shortcuts are unreachable
  // (issue #51). When the editor is focused with the cursor in a list item, we
  // show a small indent/outdent bar pinned just above the on-screen keyboard.
  let editorFocused = $state(false);
  let selectionInList = $state(false);
  // Height of the on-screen keyboard, from the visual viewport, so the bar
  // rides above it instead of hiding behind it on iOS.
  let keyboardInset = $state(0);
  const showListToolbar = $derived(editorFocused && selectionInList);

  function refreshListSelection(ed: Editor) {
    selectionInList = selectionInListItem(ed.state);
  }

  function updateKeyboardInset() {
    const vv = typeof window !== 'undefined' ? window.visualViewport : null;
    if (!vv) return;
    // The gap between the layout viewport's bottom and the visible area's
    // bottom is the space the keyboard (and any browser chrome) takes up.
    keyboardInset = Math.max(0, window.innerHeight - (vv.height + vv.offsetTop));
  }

  function handleIndent() {
    if (editor) indentListItem(editor);
  }

  function handleOutdent() {
    if (editor) outdentListItem(editor);
  }

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

  // Task titles flow back into bullets only in single-writer mode: in a
  // collaborative note every client would make the same text edit.
  const taskSync = useTaskSync(() => editor, () => pageId, { syncTitlesToBullets: () => mode === 'rest' });

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

  /** Whether the page loaded, was superseded, or has content the editor can't show. */
  async function loadContent(): Promise<'loaded' | 'stale' | 'invalid'> {
    if (!editor) return 'stale';
    const gen = ++loadGeneration;
    // Capture the target page up front — `pageId` may change while we await.
    const targetPageId = pageId;
    const content = await pagesStore.getContent(targetPageId);
    // Discard response if a newer loadContent was triggered while we were awaiting
    if (gen !== loadGeneration || !editor) return 'stale';
    const result = applyStoredContent(editor, content?.content as Record<string, unknown> | undefined);
    if (!result.ok) {
      // loadedPageId stays null, so no path can save the (empty) editor
      // document over the note.
      showContentError(targetPageId, result.error);
      return 'invalid';
    }
    contentError = null;
    // From here on the editor genuinely holds targetPageId's document, so
    // writes keyed off loadedPageId are safe.
    loadedPageId = targetPageId;
    taskCreation.clearPrompted();
    bulletRemoval.snapshot(editor);
    taskSync.syncTaskStatuses(editor, targetPageId);
    return 'loaded';
  }

  function showContentError(target: string, error: Error) {
    console.error('[Editor] Stored content cannot be shown in the editor; opened read-only', { pageId: target }, error);
    contentError = 'This note contains content this version of Glyph can’t show, so it is open read-only and nothing you type will be saved. The note itself is unchanged.';
    notificationsStore.error('This note can’t be shown in the editor. It was opened read-only so nothing is lost.');
    editor?.setEditable(false);
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
      // possibly-newer reactive pageId — and through the save queue: a save
      // of its own could overlap the one the user's first keystroke starts,
      // and the second PUT would take a spurious 409 (and a reload).
      if (loadedPageId) {
        contentSave.scheduleSave(editor, loadedPageId);
        void contentSave.flushContentSave();
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
      // Pasted/dropped bullets get their own identity, and never keep a task
      // link to another note's (or another bullet's) task — in every mode. A
      // bullet brought over from another note asks for that note's task to
      // be moved onto it: it is, if the bullet was cut from there.
      PasteIdentityExtension.configure({
        taskBelongsHere: (taskId: string) => {
          const page = collab ? collabPageId : loadedPageId;
          const source = tasksStore.getById(taskId)?.sourcePageId ?? bulletRemoval.sourcePageOfRemoved(taskId);
          return !!page && source === page;
        },
        onForeignTaskPasted: (pasted) => {
          const page = collab ? collabPageId : loadedPageId;
          if (!page) return;
          taskCreation.adoptPasted(
            pasted.map((p) => ({ ...p, cut: bulletRemoval.sourcePageOfRemoved(p.taskId) !== undefined })),
            page
          );
        }
      }),
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
      // Collaborative: only created once the content is there, so it can
      // start in its final state.
      editable: s ? s.canEdit : false,
      onFocus: ({ editor: ed }) => {
        editorFocused = true;
        refreshListSelection(ed);
        updateKeyboardInset();
      },
      onBlur: () => {
        editorFocused = false;
      },
      onSelectionUpdate: ({ editor: ed }) => {
        dismissPendingIfCursorLeft(ed);
        refreshListSelection(ed);
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
        (p) => { pending = p; },
        transaction
      );
    }
    dismissPendingIfCursorLeft(ed);

    // Debounce removal detection. In API mode this only updates the local
    // task list (the server reconciles tasks), so it runs for remote edits too.
    if (removedBulletTimer) clearTimeout(removedBulletTimer);
    removedBulletTimer = setTimeout(() => {
      removedBulletTimer = null;
      void bulletRemoval.detectRemovedTaskBullets(ed);
    }, 1000);

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
    const loaded = await loadContent();
    if (loaded !== 'loaded') return;
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
    let ready = false;
    let seedChecked = false;
    /**
     * The collab service seeds the shared document from the stored content;
     * content the schema can't represent makes that fail, and the session
     * just keeps retrying with a blank, non-editable note. When the session
     * drops before its first sync, check the stored content ourselves and, if
     * that is the reason, stop and say so (DI-01).
     */
    const checkSeedable = async () => {
      if (seedChecked) return;
      seedChecked = true;
      let stored;
      try {
        stored = await pagesStore.getContent(target);
      } catch {
        return; // Can't tell; the session keeps retrying.
      }
      if (gen !== openGeneration || session !== s || ready) return;
      const err = checkStoredContent(stored?.content);
      if (!err) return;
      showContentError(target, err);
      teardownEditor(); // also resolves whenReady() below
    };
    const s = new CollabSession(
      target,
      {
        onState: (st) => {
          if (session !== s) return;
          if (!ready && st.connection === 'offline') void checkSeedable();
          uiStore.setCollabState(st);
          // State events fire on every sync acknowledgement, i.e. per
          // keystroke, and setEditable() pushes a view update (and an
          // 'update' event) even when the value is unchanged — so only call
          // it when editability actually changes.
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
    ready = true;
    // A fatal reason (schema mismatch / forbidden) can finish the session
    // before its first sync. whenReady() now resolves in that case instead of
    // hanging, but the Y.Doc is empty and detached — don't build a TipTap
    // editor over it (onFatal has already surfaced the reason to the user).
    if (s.isFinished) return;

    editor = createEditor(s, target);
    contentError = null;
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

  /**
   * Leaving the page: run a bullet-removal check that is still waiting on its
   * debounce against the document being left (else a bullet removed just
   * before navigating is never noticed), then apply the local-mode
   * deletions it deferred. Never rejects.
   */
  function settleBulletRemoval(): Promise<void> {
    let check: Promise<void> = Promise.resolve();
    if (removedBulletTimer) {
      clearTimeout(removedBulletTimer);
      removedBulletTimer = null;
      if (editor) check = bulletRemoval.detectRemovedTaskBullets(editor);
    }
    return check
      .then(() => bulletRemoval.flush())
      .catch((err) => console.error('[Editor] Settling removed bullets failed:', err));
  }

  /**
   * Warn before leaving with edits that aren't stored yet: collaborative
   * edits the server hasn't acknowledged, or — single-writer — a content save
   * or task title write still waiting on its debounce or in flight (DI-30).
   */
  function handleBeforeUnload(e: BeforeUnloadEvent) {
    const unsaved =
      (mode === 'collab' && session?.hasUnsyncedChanges) ||
      contentSave.hasPendingWork() ||
      hasPendingTaskTitleUpdates();
    if (unsaved) {
      e.preventDefault();
      // WebKit/Safari (and older engines) only show the leave-confirmation when
      // returnValue is set to a non-empty value; preventDefault() alone is
      // enough for Chromium but not them.
      e.returnValue = '';
    }
  }

  /**
   * The page is being hidden (tab switch, app switch on mobile) or unloaded:
   * send the debounced writes now rather than when their timers fire, which
   * may be never. They are sent with keepalive so they survive the tab
   * closing (DI-30).
   */
  function handleVisibilityChange() {
    if (document.visibilityState === 'hidden') void contentSave.flushAll({ keepalive: true });
  }

  function handlePageHide() {
    void contentSave.flushAll({ keepalive: true });
    // Leaving for good: apply deferred local-mode task deletions too.
    void settleBulletRemoval();
  }

  onMount(async () => {
    if (!editorEl) return;
    mounted = true;
    prevPageId = pageId;
    window.addEventListener('beforeunload', handleBeforeUnload);
    window.addEventListener('pagehide', handlePageHide);
    document.addEventListener('visibilitychange', handleVisibilityChange);
    // Keep the mobile list toolbar pinned above the on-screen keyboard.
    window.visualViewport?.addEventListener('resize', updateKeyboardInset);
    window.visualViewport?.addEventListener('scroll', updateKeyboardInset);
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
    contentError = null;
    const removal = settleBulletRemoval();
    contentLoaded = false;
    // Mark the editor as holding no known page until the next one is open.
    // Any transaction dispatched in this window is now a no-op for saving
    // rather than a write of the old document under the new id.
    loadedPageId = null;
    editor?.setEditable(false);
    // Drop per-page task status memory so the next page starts clean and
    // doesn't dispatch spurious status transactions for unrelated tasks.
    taskSync.resetStatusMemory();
    void Promise.all([contentSave.flushAll(), removal]).then(async () => {
      taskCreation.clearPrompted();
      await openPage(next);
    });
  });

  onDestroy(() => {
    mounted = false;
    openGeneration++;
    const flushPromise = Promise.all([contentSave.flushAll(), settleBulletRemoval()]).then(() => {});
    uiStore.registerPendingFlush(flushPromise);
    if (typeof window !== 'undefined') {
      window.removeEventListener('beforeunload', handleBeforeUnload);
      window.removeEventListener('pagehide', handlePageHide);
      document.removeEventListener('visibilitychange', handleVisibilityChange);
      window.visualViewport?.removeEventListener('resize', updateKeyboardInset);
      window.visualViewport?.removeEventListener('scroll', updateKeyboardInset);
    }
    contentSave.destroy();
    bulletRemoval.destroy();
    teardownEditor();
  });
</script>

<div class="editor-wrapper" data-content-loaded={contentLoaded} data-content-error={contentError ? 'true' : undefined}>
  {#if contentError}
    <div class="content-error" role="alert">{contentError}</div>
  {/if}
  <div bind:this={editorEl} class="editor-mount"></div>

  {#if showListToolbar}
    <!--
      Mobile-only (see the media query below): indent / outdent controls for
      lists, since touch keyboards have no Tab key (issue #51). onpointerdown is
      prevented so tapping a button doesn't blur the editor before the command
      runs. It stays pinned above the on-screen keyboard via keyboardInset.
    -->
    <div class="list-toolbar" style="bottom: {keyboardInset}px" role="toolbar" aria-label="List indentation">
      <button
        type="button"
        class="list-toolbar-btn"
        aria-label="Outdent list item"
        title="Outdent"
        onpointerdown={(e) => e.preventDefault()}
        onclick={handleOutdent}
      >
        <Icon name="outdent" size={20} />
      </button>
      <button
        type="button"
        class="list-toolbar-btn"
        aria-label="Indent list item"
        title="Indent"
        onpointerdown={(e) => e.preventDefault()}
        onclick={handleIndent}
      >
        <Icon name="indent" size={20} />
      </button>
    </div>
  {/if}
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

  /*
   * Mobile list-indent toolbar. Hidden by default (desktop has Tab /
   * Shift-Tab); revealed only on narrow, touch-first viewports below.
   */
  .list-toolbar {
    display: none;
    position: fixed;
    left: 0;
    right: 0;
    z-index: 50;
    justify-content: flex-end;
    gap: 8px;
    padding: 6px 12px;
    background: var(--bg-secondary);
    border-top: 1px solid var(--border-subtle);
  }

  .list-toolbar-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 44px;
    height: 44px;
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    background: var(--bg-primary);
    color: var(--text-secondary);
    cursor: pointer;
    -webkit-tap-highlight-color: transparent;
    transition: background-color var(--transition-fast), color var(--transition-fast);
  }
  .list-toolbar-btn:active {
    background: var(--bg-hover);
    color: var(--text-primary);
  }

  .content-error {
    max-width: 760px;
    margin: 16px auto 0;
    padding: 10px 14px;
    border: 1px solid var(--status-cancelled);
    border-radius: var(--radius-md);
    background: var(--bg-tertiary);
    color: var(--text-primary);
    font-size: var(--font-size-sm);
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
    /* Break long words/URLs rather than forcing a horizontal scroll that
       pushes text off screen (especially on narrow mobile viewports). */
    overflow-wrap: break-word;
    word-break: break-word;
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

    /* Give the last lines room to clear the pinned toolbar. */
    .editor-mount {
      scroll-padding-bottom: 56px;
    }

    .list-toolbar {
      display: flex;
    }
  }

  :global(.tiptap-editor hr) {
    border: none;
    border-top: 1px solid var(--border-subtle);
    margin: 1.5em 0;
  }

  /* ─── Tables (GFM, issue #75) ────────────────────────────────────────────── */
  :global(.tiptap-editor table) {
    border-collapse: collapse;
    margin: 0.6em 0;
    width: auto;
    max-width: 100%;
    overflow: hidden;
    table-layout: fixed;
  }

  :global(.tiptap-editor th),
  :global(.tiptap-editor td) {
    border: 1px solid var(--border-strong);
    padding: 6px 10px;
    vertical-align: top;
    text-align: left;
    min-width: 3em;
  }

  :global(.tiptap-editor th) {
    background: var(--bg-tertiary);
    color: var(--text-heading);
    font-weight: 600;
  }

  /* Empty cells still need to take up space so the grid reads as a table. */
  :global(.tiptap-editor th p),
  :global(.tiptap-editor td p) {
    margin: 0;
  }

  :global(.tiptap-editor .selectedCell::after) {
    content: '';
    position: absolute;
    inset: 0;
    background: var(--accent);
    opacity: 0.12;
    pointer-events: none;
  }

  :global(.tiptap-editor th),
  :global(.tiptap-editor td) {
    position: relative;
  }

  :global(.tiptap-editor .ProseMirror-selectednode) {
    outline: 2px solid var(--accent);
    border-radius: var(--radius-sm);
  }
</style>
