<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { Editor } from '@tiptap/core';
  import StarterKit from '@tiptap/starter-kit';
  import Placeholder from '@tiptap/extension-placeholder';
  import { TaskLinkExtension } from '$lib/editor/extensions/TaskLinkExtension';
  import { TodoDetectionExtension, type DetectedBullet } from '$lib/editor/extensions/TodoDetectionExtension';
  import { NodeIdMapExtension } from '$lib/editor/plugins/NodeIdMapPlugin';
  import { tasksStore } from '$lib/stores/tasks.svelte';
  import { pagesStore } from '$lib/stores/pages.svelte';
  import { uiStore } from '$lib/stores/ui.svelte';
  import { useContentSave } from '$lib/editor/useContentSave';
  import { useTaskCreation, type PendingTaskDetails } from '$lib/editor/useTaskCreation';
  import { useTaskSync } from '$lib/editor/useTaskSync';
  import { useBulletRemoval } from '$lib/editor/useBulletRemoval';
  import { DEBOUNCE } from '$lib/models/constants';
  import TaskCreationPopover from './TaskCreationPopover.svelte';
  import TaskHoverPreview from './TaskHoverPreview.svelte';
  import { nanoid } from 'nanoid';
  import type { TaskStatus } from '$lib/models/types';

  let {
    pageId,
    onchange
  }: {
    pageId: string;
    onchange?: () => void;
  } = $props();

  let editorEl = $state<HTMLDivElement | null>(null);
  let editor = $state<Editor | null>(null);

  // Reactive state for template rendering
  let pending = $state<PendingTaskDetails | null>(null);
  let hoverPreview = $state<{ taskId: string; x: number; y: number } | null>(null);
  let hoverTimer: ReturnType<typeof setTimeout> | null = null;
  let removedBulletTimer: ReturnType<typeof setTimeout> | null = null;

  // ─── Composables ────────────────────────────────────────────────────────────

  const contentSave = useContentSave(() => onchange?.());

  const taskCreation = useTaskCreation(() => editor, (p) => { pending = p; });

  const taskSync = useTaskSync(() => editor, () => pageId);

  const bulletRemoval = useBulletRemoval();

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
    taskSync.syncExternalStatusChanges(editor, pageId);
  });

  // ─── Content loading ────────────────────────────────────────────────────────

  /** Monotonically increasing generation counter to discard stale loadContent responses. */
  let loadGeneration = 0;

  async function loadContent() {
    if (!editor) return;
    const gen = ++loadGeneration;
    const content = await pagesStore.getContent(pageId);
    // Discard response if a newer loadContent was triggered while we were awaiting
    if (gen !== loadGeneration) return;
    if (content?.content && Object.keys(content.content).length > 0) {
      editor.commands.setContent(content.content as Record<string, unknown>, { emitUpdate: false });
    } else {
      editor.commands.setContent('', { emitUpdate: false });
    }
    taskCreation.clearPrompted();
    bulletRemoval.snapshot(editor);
    taskSync.syncTaskStatuses(editor, pageId);
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
      pagesStore.saveContent(pageId, editor.getJSON() as Record<string, unknown>);
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

  // ─── Hover preview ─────────────────────────────────────────────────────────

  function setupHoverDetection() {
    if (!editorEl) return;
    editorEl.addEventListener('mouseover', handleEditorMouseover);
    editorEl.addEventListener('mouseout', handleEditorMouseout);
  }

  function handleEditorMouseover(e: MouseEvent) {
    const target = e.target as HTMLElement;
    const li = target.closest('[data-task-id]') as HTMLElement | null;
    if (!li) { clearHoverPreview(); return; }

    const taskId = li.getAttribute('data-task-id');
    if (!taskId) return;

    if (hoverTimer) clearTimeout(hoverTimer);
    hoverTimer = setTimeout(() => {
      const rect = li.getBoundingClientRect();
      hoverPreview = { taskId, x: rect.left + rect.width / 2, y: rect.top - 8 };
    }, DEBOUNCE.HOVER_PREVIEW);
  }

  function handleEditorMouseout(e: MouseEvent) {
    const related = e.relatedTarget as HTMLElement | null;
    if (related?.closest('[data-task-id]')) return;
    clearHoverPreview();
  }

  function clearHoverPreview() {
    if (hoverTimer) clearTimeout(hoverTimer);
    hoverPreview = null;
  }

  // ─── Lifecycle ──────────────────────────────────────────────────────────────

  onMount(async () => {
    if (!editorEl) return;

    editor = new Editor({
      element: editorEl,
      extensions: [
        StarterKit.configure({
          listItem: false
        }),
        NodeIdMapExtension,
        TaskLinkExtension.configure({
          onStatusCycled: (nodeId: string, taskId: string, currentStatus: string) => {
            taskSync.handleStatusCycled(nodeId, taskId, currentStatus);
          }
        }),
        Placeholder.configure({
          placeholder: 'Start writing… Create a heading named TODO to track tasks.'
        }),
        TodoDetectionExtension.configure({
          onTodoBulletsDetected: (bullets: DetectedBullet[]) => {
            taskCreation.handleTodoBulletsDetected(bullets);
          },
          pageId: () => pageId,
          todoTrigger: () => pagesStore.getById(pageId)?.todoTrigger
        })
      ],
      editorProps: {
        attributes: {
          class: 'tiptap-editor',
          spellcheck: 'true'
        }
      },
      onSelectionUpdate: ({ editor: ed }) => {
        dismissPendingIfCursorLeft(ed);
      },
      onUpdate: ({ editor: ed }) => {
        taskSync.syncLinkedTaskTitleRealtime(
          ed,
          () => pending,
          (p) => { pending = p; }
        );
        dismissPendingIfCursorLeft(ed);

        // Debounce removal detection
        if (removedBulletTimer) clearTimeout(removedBulletTimer);
        removedBulletTimer = setTimeout(() => bulletRemoval.detectRemovedTaskBullets(ed), 1000);

        // Debounced content save
        contentSave.scheduleSave(ed, pageId);
      }
    });

    await loadContent();
    scheduleAutoAssignNodeIds();
    bulletRemoval.snapshot(editor);
    setupHoverDetection();
  });

  // Reload content when pageId changes (but not on initial mount — onMount handles that)
  let prevPageId: string | null = null;
  $effect(() => {
    if (editor && pageId) {
      if (prevPageId === null) {
        // First run: onMount already loaded content, just record the pageId
        prevPageId = pageId;
        return;
      }
      if (pageId !== prevPageId) {
        prevPageId = pageId;
        // Clear transient UI state that is page-scoped
        pending = null;
        clearHoverPreview();
        if (removedBulletTimer) { clearTimeout(removedBulletTimer); removedBulletTimer = null; }
        void contentSave.flushAll().then(() => {
          taskCreation.clearPrompted();
          loadContent();
        });
      }
    }
  });

  onDestroy(() => {
    const flushPromise = contentSave.flushAll();
    uiStore.registerPendingFlush(flushPromise);
    if (hoverTimer) clearTimeout(hoverTimer);
    if (removedBulletTimer) clearTimeout(removedBulletTimer);
    contentSave.destroy();
    bulletRemoval.destroy();
    editorEl?.removeEventListener('mouseover', handleEditorMouseover);
    editorEl?.removeEventListener('mouseout', handleEditorMouseout);
    editor?.destroy();
  });
</script>

<div class="editor-wrapper">
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

{#if hoverPreview}
  <TaskHoverPreview
    taskId={hoverPreview.taskId}
    x={hoverPreview.x}
    y={hoverPreview.y}
    onclose={clearHoverPreview}
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
