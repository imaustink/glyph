<script lang="ts">
  import { onDestroy } from 'svelte';
  import { tasksStore } from '$lib/stores/tasks.svelte';
  import { notificationsStore } from '$lib/stores/notifications.svelte';
  import type { Priority, LinkMeta } from '$lib/models/types';
  import TagInput from '$lib/components/shared/TagInput.svelte';
  import DatePicker from '$lib/components/shared/DatePicker.svelte';
  import LinkPreview from '$lib/components/tasks/LinkPreview.svelte';
  import { storageMode } from '$lib/storage/config';
  import { api } from '$lib/storage/apiClient';

  let {
    taskId,
    bulletText,
    ontitlechange,
    onclose
  }: {
    taskId: string;
    bulletText: string;
    ontitlechange: (title: string) => void;
    onclose: () => void;
  } = $props();

  let title = $state('');
  let priority = $state<Priority>('none');
  let dueDate = $state('');
  let tags = $state<string[]>([]);
  let expanded = $state(false);
  let initializedForTaskId = $state<string | null>(null);
  let persistTimer: ReturnType<typeof setTimeout> | null = null;

  // ── Link / URL unfurl ──────────────────────────────────────────────────────
  const canUnfurl = storageMode === 'api';
  let link = $state<LinkMeta | null>(null);
  let linkInput = $state('');
  let linkLoading = $state(false);
  let linkError = $state('');

  const allTags = $derived(
    [...new Set(tasksStore.tasks.flatMap((t) => t.tags))]
  );

  $effect(() => {
    if (initializedForTaskId === taskId) return;

    const task = tasksStore.getById(taskId);
    if (!task) return;

    title = bulletText;
    priority = task.priority;
    dueDate = task.dueDate ?? '';
    tags = [...task.tags];
    link = task.link;
    expanded =
      task.priority !== 'none' || !!task.dueDate || task.tags.length > 0 || !!task.link;
    initializedForTaskId = taskId;
  });

  $effect(() => {
    title = bulletText;
  });

  $effect(() => {
    if (initializedForTaskId !== taskId) return;

    if (persistTimer) clearTimeout(persistTimer);

    const payload = {
      priority,
      dueDate: dueDate || null,
      tags: [...tags]
    };

    persistTimer = setTimeout(() => {
      void tasksStore.updateTask(taskId, payload).catch(() => {
        notificationsStore.error('Failed to save task details.');
      });
    }, 250);
  });

  function handleTitleInput() {
    ontitlechange(title);
  }

  async function addLink() {
    const raw = linkInput.trim();
    if (!raw) return;

    let normalizedUrl: string;
    try {
      const parsed = new URL(raw.startsWith('http') ? raw : `https://${raw}`);
      if (!['http:', 'https:'].includes(parsed.protocol)) {
        linkError = 'Only http/https URLs are supported';
        return;
      }
      if (!parsed.hostname.includes('.')) {
        linkError = 'Please enter a valid URL';
        return;
      }
      normalizedUrl = parsed.href;
    } catch {
      linkError = 'Please enter a valid URL';
      return;
    }

    linkError = '';
    linkLoading = true;
    try {
      const meta = await api.post<LinkMeta>('/api/v1/unfurl', { url: normalizedUrl });
      await tasksStore.updateTask(taskId, { link: meta });
      link = meta;
      linkInput = '';
    } catch {
      linkError = 'Failed to fetch link preview';
    } finally {
      linkLoading = false;
    }
  }

  function handleLinkKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') {
      e.preventDefault();
      e.stopPropagation();
      void addLink();
    }
    if (e.key === 'Escape') {
      e.stopPropagation();
      linkInput = '';
      linkError = '';
    }
  }

  async function removeLink() {
    try {
      await tasksStore.updateTask(taskId, { link: null });
      link = null;
    } catch {
      notificationsStore.error('Failed to remove link.');
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      onclose();
    }
    if (e.key === 'Escape') {
      onclose();
    }
  }

  onDestroy(() => {
    if (persistTimer) clearTimeout(persistTimer);
  });
</script>

<div class="popover" role="dialog" aria-label="Create task">
  <div class="popover-header">
    <span class="popover-title">Task details (optional)</span>
    <span class="hint">Enter closes, Esc dismisses</span>
  </div>

  <div class="field">
    <input
      class="title-input"
      bind:value={title}
      oninput={handleTitleInput}
      onkeydown={handleKeydown}
      placeholder="Task title…"
    />
  </div>

  {#if expanded}
    <div class="expanded-fields">
      <div class="field-row">
        <label class="field-label" for="priority">Priority</label>
        <select id="priority" bind:value={priority}>
          <option value="none">None</option>
          <option value="low">Low</option>
          <option value="medium">Medium</option>
          <option value="high">High</option>
          <option value="urgent">Urgent</option>
        </select>
      </div>

      <div class="field-row">
        <span class="field-label">Due date</span>
        <DatePicker value={dueDate || null} onchange={(v) => dueDate = v ?? ''} />
      </div>

      <div class="field-row">
        <span class="field-label">Tags</span>
        <TagInput bind:tags suggestions={allTags} />
      </div>

      {#if canUnfurl || link}
        <div class="link-field">
          <span class="field-label link-field-label">URL</span>
          <div class="link-field-body">
            {#if link}
              <LinkPreview {link} onremove={removeLink} />
            {:else if canUnfurl}
              <div class="link-input-row">
                <input
                  type="url"
                  class="link-input"
                  placeholder="Paste a URL…"
                  bind:value={linkInput}
                  onkeydown={handleLinkKeydown}
                  disabled={linkLoading}
                />
                <button
                  class="btn-ghost link-add-btn"
                  onclick={addLink}
                  disabled={linkLoading || !linkInput.trim()}
                  type="button"
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
        </div>
      {/if}
    </div>
  {/if}

  <div class="popover-footer">
    <button
      class="btn-ghost expand-btn"
      onclick={() => expanded = !expanded}
      type="button"
    >
      {expanded ? '− Less options' : '+ More options'}
    </button>
    <div class="footer-actions">
      <button class="btn-primary" onclick={onclose} type="button">Done</button>
    </div>
  </div>
</div>

<style>
  .popover {
    position: fixed;
    right: 20px;
    bottom: 20px;
    z-index: 48;
    background: var(--bg-modal);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-lg);
    padding: 20px;
    width: 420px;
    max-width: calc(100vw - 32px);
  }

  .popover-header {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    margin-bottom: 14px;
  }

  .popover-title {
    font-size: var(--font-size-md);
    font-weight: 600;
    color: var(--text-heading);
  }

  .hint {
    font-size: var(--font-size-xs);
    color: var(--text-muted);
  }

  .title-input {
    width: 100%;
    background: var(--bg-tertiary);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    font-size: var(--font-size-md);
    padding: 8px 12px;
    color: var(--text-primary);
  }
  .title-input:focus { border-color: var(--accent); outline: none; }

  .field { margin-bottom: 10px; }

  .expanded-fields {
    border-top: 1px solid var(--border-subtle);
    padding-top: 12px;
    display: flex;
    flex-direction: column;
    gap: 10px;
    margin-bottom: 14px;
  }

  .field-row {
    display: grid;
    grid-template-columns: 80px 1fr;
    align-items: center;
    gap: 10px;
  }

  .field-label {
    font-size: var(--font-size-sm);
    color: var(--text-secondary);
    text-align: right;
  }

  .link-field {
    display: grid;
    grid-template-columns: 80px 1fr;
    gap: 10px;
  }

  .link-field-label {
    padding-top: 8px;
  }

  .link-field-body {
    min-width: 0;
  }

  .link-input-row {
    display: flex;
    gap: 8px;
  }

  .link-input {
    flex: 1;
    min-width: 0;
    background: var(--bg-tertiary);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    font-size: var(--font-size-sm);
    padding: 6px 10px;
    color: var(--text-primary);
  }
  .link-input:focus { border-color: var(--accent); outline: none; }

  .link-add-btn {
    flex-shrink: 0;
    padding: 6px 12px;
    min-width: 52px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }

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
    animation: link-spin 0.6s linear infinite;
  }

  @keyframes link-spin {
    to { transform: rotate(360deg); }
  }

  .popover-footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding-top: 12px;
    border-top: 1px solid var(--border-subtle);
  }

  .expand-btn {
    font-size: var(--font-size-xs);
    color: var(--text-muted);
    padding: 4px 6px;
  }
  .expand-btn:hover { color: var(--text-primary); }

  .footer-actions {
    display: flex;
    gap: 8px;
  }

  @media (max-width: 720px) {
    .popover {
      right: 12px;
      bottom: 12px;
      width: calc(100vw - 24px);
      max-width: none;
    }
  }
</style>
