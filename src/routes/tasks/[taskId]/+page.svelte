<script lang="ts">
  import { page } from '$app/state';
  import { goto } from '$app/navigation';
  import { tasksStore } from '$lib/stores/tasks.svelte';
  import { pagesStore } from '$lib/stores/pages.svelte';
  import TagInput from '$lib/components/shared/TagInput.svelte';
  import DatePicker from '$lib/components/shared/DatePicker.svelte';
  import LinkPreview from '$lib/components/tasks/LinkPreview.svelte';
  import ShareDialog from '$lib/components/shared/ShareDialog.svelte';
  import VisibilityPicker from '$lib/components/shared/VisibilityPicker.svelte';
  import MarkdownEditor from '$lib/components/shared/MarkdownEditor.svelte';
  import { authStore } from '$lib/stores/auth.svelte';
  import { notificationsStore } from '$lib/stores/notifications.svelte';
  import { storageMode } from '$lib/storage/config';
  import { api } from '$lib/storage/apiClient';
  import type { Priority, TaskStatus, LinkMeta } from '$lib/models/types';

  const canUnfurl = storageMode === 'api';
  import { format, parseISO } from 'date-fns';

  const taskId = $derived(page.params.taskId!);
  const task = $derived(tasksStore.getById(taskId));
  const sourcePage = $derived(task?.sourcePageId ? pagesStore.getById(task.sourcePageId) : undefined);
  const hasLinkedNote = $derived(!!task?.sourcePageId && !!task?.sourceNodeId);
  const isOrphaned = $derived(hasLinkedNote && !sourcePage);
  const allTags = $derived([...new Set(tasksStore.tasks.flatMap((t) => t.tags))]);

  const PRIORITY_OPTIONS: { value: Priority; label: string }[] = [
    { value: 'none', label: 'None' },
    { value: 'low', label: 'Low' },
    { value: 'medium', label: 'Medium' },
    { value: 'high', label: 'High' },
    { value: 'urgent', label: 'Urgent' }
  ];

  const STATUS_OPTIONS: { value: TaskStatus; label: string }[] = [
    { value: 'todo', label: 'Todo' },
    { value: 'in-progress', label: 'In Progress' },
    { value: 'done', label: 'Done' },
    { value: 'cancelled', label: 'Cancelled' }
  ];

  let titleEdit = $state('');
  let editingTitle = $state(false);

  $effect(() => {
    if (task) titleEdit = task.title;
  });

  async function updateField<K extends keyof import('$lib/models/types').Task>(
    field: K,
    value: import('$lib/models/types').Task[K]
  ) {
    if (!task) return;
    try {
      await tasksStore.updateTask(task.id, { [field]: value } as Partial<import('$lib/models/types').Task>);
    } catch (err) {
      notificationsStore.error('Failed to save changes. Please try again.');
      console.error('updateField failed:', err);
    }
  }

  async function commitTitle() {
    editingTitle = false;
    if (titleEdit.trim() && task && titleEdit.trim() !== task.title) {
      await updateField('title', titleEdit.trim());
    }
  }

  function handleTitleKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') { e.preventDefault(); commitTitle(); }
    if (e.key === 'Escape') { editingTitle = false; titleEdit = task?.title ?? ''; }
  }

  // ── Tags ─────────────────────────────────────────────────────────────────
  // localTags mirrors task.tags locally. Saved immediately via onchange callback
  // from TagInput, which fires only on user-driven mutations (not on prop sync).
  let localTags = $state<string[]>([]);
  $effect(() => { if (task) localTags = [...task.tags]; });
  async function saveTags() {
    if (!task) return;
    await updateField('tags', localTags);
  }

  // ── Description ──────────────────────────────────────────────────────────
  // Single debounce timer; only one save can be in-flight for description at a time.
  // The MarkdownEditor owns the editing state; we only track the latest markdown
  // to persist on the debounced save.
  let _pendingDesc = '';
  let _descTimer: ReturnType<typeof setTimeout> | null = null;
  function handleDescChange(markdown: string) {
    _pendingDesc = markdown;
    if (_descTimer) clearTimeout(_descTimer);
    _descTimer = setTimeout(() => {
      _descTimer = null;
      updateField('description', _pendingDesc).catch(() => {
        // Error already surfaced via notificationsStore inside updateField
      });
    }, 600);
  }

  // ── Link / URL unfurl ────────────────────────────────────────────────────
  let linkInput = $state('');
  let linkLoading = $state(false);
  let linkError = $state('');

  async function addLink() {
    const url = linkInput.trim();
    if (!url) return;

    // URL validation
    try {
      const parsed = new URL(url.startsWith('http') ? url : `https://${url}`);
      if (!['http:', 'https:'].includes(parsed.protocol)) {
        linkError = 'Only http/https URLs are supported';
        return;
      }
      // Require a valid hostname with at least one dot (e.g. example.com)
      if (!parsed.hostname.includes('.')) {
        linkError = 'Please enter a valid URL';
        return;
      }
    } catch {
      linkError = 'Please enter a valid URL';
      return;
    }

    linkError = '';
    linkLoading = true;
    try {
      const normalizedUrl = url.startsWith('http') ? url : `https://${url}`;
      const meta = await api.post<LinkMeta>('/api/v1/unfurl', { url: normalizedUrl });
      await updateField('link', meta);
      linkInput = '';
    } catch {
      linkError = 'Failed to fetch link preview';
    } finally {
      linkLoading = false;
    }
  }

  function handleLinkKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') { e.preventDefault(); addLink(); }
    if (e.key === 'Escape') { linkInput = ''; linkError = ''; }
  }

  async function removeLink() {
    await updateField('link', null);
  }

  // ── Share task ───────────────────────────────────────────────────────────
  let showShareDialog = $state(false);

  async function handleVisibilityChange(newOrgId: string | null, newIsPrivate: boolean) {
    if (!task) return;
    await tasksStore.updateTask(task.id, { orgId: newOrgId, isPrivate: newIsPrivate });
  }

  // ── Delete task ──────────────────────────────────────────────────────────
  let showDeleteConfirm = $state(false);
  let deleting = $state(false);

  async function confirmDelete() {
    if (!task) return;
    deleting = true;
    try {
      // Remove the linked bullet from the note if the page still exists
      if (task.sourcePageId && task.sourceNodeId && sourcePage) {
        try {
          await pagesStore.removeBulletByNodeId(task.sourcePageId, task.sourceNodeId);
        } catch {
          // Page or bullet no longer exists — safe to ignore
        }
      }
      await tasksStore.deleteTask(task.id);
      goto('/tasks');
    } finally {
      deleting = false;
      showDeleteConfirm = false;
    }
  }
</script>

<div class="task-detail-page">
  <div class="detail-nav">
    <button class="btn-ghost back-btn" onclick={() => history.back()}>
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
        <polyline points="15 18 9 12 15 6"/>
      </svg>
      Back
    </button>
    <div class="nav-actions">
      {#if sourcePage}
        <a href="/notes/{sourcePage.id}" class="source-link">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/>
            <polyline points="14 2 14 8 20 8"/>
          </svg>
          Open in {sourcePage.title}
        </a>
      {/if}
      {#if storageMode === 'api' && task}
        {#if task.userId === authStore.userId}
          <VisibilityPicker
            orgId={task.orgId}
            isPrivate={task.isPrivate ?? true}
            onchange={handleVisibilityChange}
            onshare={() => showShareDialog = true}
          />
        {/if}
      {/if}
      <button class="btn-danger delete-btn" onclick={() => showDeleteConfirm = true} title="Delete task">
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <polyline points="3 6 5 6 21 6"/>
          <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/>
        </svg>
        Delete
      </button>
    </div>
  </div>

  {#if !task}
    <div class="not-found">Task not found.</div>
  {:else}
    <div class="detail-content">
      <!-- Title -->
      <div class="title-section">
        {#if editingTitle}
          <!-- svelte-ignore a11y_autofocus -->
          <input
            class="title-edit"
            bind:value={titleEdit}
            onblur={commitTitle}
            onkeydown={handleTitleKeydown}
            autofocus
          />
        {:else}
          <h1
            class="task-title"
            class:done={task.status === 'done'}
            ondblclick={() => editingTitle = true}
            title="Double-click to edit"
          >
            {task.title}
          </h1>
        {/if}
      </div>

      <!-- Meta fields -->
      <div class="meta-grid">
        <div class="meta-row">
          <span class="meta-label">Status</span>
          <select
            class="meta-select"
            value={task.status}
            onchange={(e) => updateField('status', (e.target as HTMLSelectElement).value as TaskStatus)}
          >
            {#each STATUS_OPTIONS as opt}
              <option value={opt.value}>{opt.label}</option>
            {/each}
          </select>
        </div>

        <div class="meta-row">
          <span class="meta-label">Priority</span>
          <select
            class="meta-select"
            value={task.priority}
            onchange={(e) => updateField('priority', (e.target as HTMLSelectElement).value as Priority)}
          >
            {#each PRIORITY_OPTIONS as opt}
              <option value={opt.value}>{opt.label}</option>
            {/each}
          </select>
        </div>

        <div class="meta-row">
          <span class="meta-label">Due date</span>
          <DatePicker value={task.dueDate ?? null} onchange={(v) => updateField('dueDate', v)} />
        </div>

        <div class="meta-row">
          <span class="meta-label">Tags</span>
          <TagInput bind:tags={localTags} suggestions={allTags} onchange={saveTags} />
        </div>
      </div>

      <!-- Description -->
      <div class="description-section">
        <span class="desc-label">Description</span>
        {#key task.id}
          <MarkdownEditor
            value={task.description}
            placeholder="Add a description…"
            ariaLabel="Task description"
            onchange={handleDescChange}
          />
        {/key}
      </div>

      <!-- External Link -->
      {#if canUnfurl || task.link}
      <div class="link-section">
        <span class="desc-label">External Link</span>
        {#if task.link}
          <LinkPreview link={task.link} onremove={removeLink} />
        {:else if canUnfurl}
          <div class="link-input-row">
            <div class="link-input-wrapper">
              <svg class="link-input-icon" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/>
                <path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/>
              </svg>
              <input
                type="url"
                class="link-input"
                placeholder="Paste a URL…"
                bind:value={linkInput}
                onkeydown={handleLinkKeydown}
                disabled={linkLoading}
              />
            </div>
            <button
              class="btn-ghost link-add-btn"
              onclick={addLink}
              disabled={linkLoading || !linkInput.trim()}
            >
              {#if linkLoading}
                <span class="link-spinner"></span>
              {:else}
                Add
              {/if}
            </button>
          </div>
          {#if linkError}
            <p class="link-error">{linkError}</p>
          {/if}
        {/if}
      </div>
      {/if}

      <!-- Timestamps -->
      <div class="timestamps">
        <span>Created {format(parseISO(task.createdAt), 'PPP')}</span>
        <span>·</span>
        <span>Updated {format(parseISO(task.updatedAt), 'PPP p')}</span>
      </div>
    </div>
  {/if}
</div>

{#if showShareDialog && task}
  <ShareDialog
    resourceType="task"
    resourceId={task.id}
    onclose={() => showShareDialog = false}
  />
{/if}

{#if showDeleteConfirm}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="modal-backdrop" onkeydown={(e) => e.key === 'Escape' && (showDeleteConfirm = false)} onclick={() => showDeleteConfirm = false}>
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <div class="modal-panel delete-modal" onclick={(e) => e.stopPropagation()}>
      <h3 class="delete-modal-title">Delete task</h3>
      <p class="delete-modal-text">
        Are you sure you want to delete <strong>{task?.title}</strong>?
      </p>
      {#if hasLinkedNote}
        {#if isOrphaned}
          <p class="delete-modal-info">
            This task was linked to a note that no longer exists. It can be safely deleted.
          </p>
        {:else}
          <p class="delete-modal-warning">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z"/>
              <line x1="12" y1="9" x2="12" y2="13"/>
              <line x1="12" y1="17" x2="12.01" y2="17"/>
            </svg>
            The linked bullet in the note will also be removed.
          </p>
        {/if}
      {/if}
      <div class="delete-modal-actions">
        <button class="btn-ghost" onclick={() => showDeleteConfirm = false} disabled={deleting}>Cancel</button>
        <button class="btn-danger delete-confirm-btn" onclick={confirmDelete} disabled={deleting}>
          {deleting ? 'Deleting…' : 'Delete'}
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
  .task-detail-page {
    max-width: 700px;
    margin: 0 auto;
    padding: 24px 40px 60px;
    height: 100%;
    overflow-y: auto;
  }

  .detail-nav {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-bottom: 24px;
  }

  .back-btn {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: var(--font-size-sm);
    color: var(--text-muted);
    padding: 4px 8px;
  }
  .back-btn:hover { color: var(--text-primary); }

  .nav-actions {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-left: auto;
  }

  .source-link {
    display: flex;
    align-items: center;
    gap: 5px;
    font-size: var(--font-size-sm);
    color: var(--text-link);
  }

  .detail-content { display: contents; }

  .title-section { margin-bottom: 24px; }

  .task-title {
    font-size: 1.8em;
    font-weight: 700;
    color: var(--text-heading);
    margin: 0;
    cursor: text;
    line-height: 1.25;
  }
  .task-title:hover { color: var(--text-primary); }
  .task-title.done { text-decoration: line-through; opacity: 0.7; }

  .title-edit {
    width: 100%;
    font-size: 1.8em;
    font-weight: 700;
    background: none;
    border: none;
    border-bottom: 2px solid var(--accent);
    border-radius: 0;
    padding: 0;
    color: var(--text-heading);
    outline: none;
  }

  .meta-grid {
    position: relative;
    z-index: 1;
    background: var(--bg-secondary);
    border: 1px solid var(--border-subtle);
    border-radius: var(--radius-md);
    margin-bottom: 24px;
  }

  .meta-grid > .meta-row:first-child {
    border-radius: var(--radius-md) var(--radius-md) 0 0;
  }

  .meta-grid > .meta-row:last-child {
    border-radius: 0 0 var(--radius-md) var(--radius-md);
  }

  .meta-row {
    display: grid;
    grid-template-columns: 100px 1fr;
    align-items: center;
    gap: 12px;
    padding: 10px 16px;
    border-bottom: 1px solid var(--border-subtle);
  }
  .meta-row:last-child { border-bottom: none; }

  .meta-label {
    font-size: var(--font-size-sm);
    color: var(--text-muted);
    font-weight: 500;
  }

  .meta-select {
    background: transparent;
    border: 1px solid transparent;
    padding: 3px 6px;
    font-size: var(--font-size-sm);
  }
  .meta-select:hover { border-color: var(--border-default); }
  .meta-select:focus { border-color: var(--accent); background: var(--bg-tertiary); }

  .description-section { margin-bottom: 24px; }

  .desc-label {
    display: block;
    font-size: var(--font-size-xs);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--text-muted);
    margin-bottom: 8px;
  }

  .timestamps {
    display: flex;
    gap: 8px;
    font-size: var(--font-size-xs);
    color: var(--text-muted);
  }

  .link-section {
    margin-bottom: 24px;
  }

  .link-input-row {
    display: flex;
    gap: 8px;
    align-items: center;
  }

  .link-input-wrapper {
    flex: 1;
    position: relative;
    display: flex;
    align-items: center;
  }

  .link-input-icon {
    position: absolute;
    left: 10px;
    color: var(--text-muted);
    pointer-events: none;
  }

  .link-input {
    width: 100%;
    padding: 8px 12px 8px 32px;
    font-size: var(--font-size-sm);
    background: var(--bg-secondary);
    border: 1px solid var(--border-subtle);
    border-radius: var(--radius-md);
    color: var(--text-primary);
  }
  .link-input:hover { border-color: var(--border-default); }
  .link-input:focus { border-color: var(--accent); background: var(--bg-tertiary); }
  .link-input:disabled { opacity: 0.6; }

  .link-add-btn {
    padding: 8px 16px;
    font-size: var(--font-size-sm);
    white-space: nowrap;
  }
  .link-add-btn:disabled { opacity: 0.4; cursor: not-allowed; }

  .link-error {
    margin: 6px 0 0;
    font-size: var(--font-size-xs);
    color: var(--priority-urgent);
  }

  .link-spinner {
    display: inline-block;
    width: 14px;
    height: 14px;
    border: 2px solid var(--text-muted);
    border-top-color: transparent;
    border-radius: 50%;
    animation: spin 0.6s linear infinite;
  }

  @keyframes spin {
    to { transform: rotate(360deg); }
  }

  .not-found { padding: 60px; color: var(--text-muted); }

  .delete-btn {
    display: flex;
    align-items: center;
    gap: 5px;
    padding: 4px 10px;
    font-size: var(--font-size-sm);
  }

  .delete-modal-title {
    margin: 0 0 8px;
    font-size: var(--font-size-lg);
    color: var(--text-heading);
  }

  .delete-modal-text {
    margin: 0 0 12px;
    font-size: var(--font-size-sm);
    color: var(--text-secondary);
    line-height: 1.5;
  }

  .delete-modal-warning {
    display: flex;
    align-items: center;
    gap: 6px;
    margin: 0 0 16px;
    padding: 8px 12px;
    background: rgba(224, 108, 117, 0.1);
    border: 1px solid rgba(224, 108, 117, 0.25);
    border-radius: var(--radius-md);
    font-size: var(--font-size-sm);
    color: var(--priority-urgent);
    line-height: 1.4;
  }

  .delete-modal-info {
    margin: 0 0 16px;
    padding: 8px 12px;
    background: var(--accent-bg);
    border: 1px solid rgba(123, 145, 219, 0.25);
    border-radius: var(--radius-md);
    font-size: var(--font-size-sm);
    color: var(--text-secondary);
    line-height: 1.4;
  }

  .delete-modal-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }

  .delete-confirm-btn {
    padding: 6px 16px;
    font-weight: 600;
  }
  .delete-confirm-btn:disabled { opacity: 0.5; cursor: not-allowed; }

  /* ─── Mobile responsive ─────────────────────────────────────────────────── */
  @media (max-width: 768px) {
    .task-detail-page {
      /* Clear the fixed hamburger button (top:10 + height:36 = 46px) */
      padding: 56px 16px 40px;
    }

    .not-found {
      padding: 48px 16px;
    }
  }
</style>
