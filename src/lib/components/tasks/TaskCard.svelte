<script lang="ts">
  import { goto } from '$app/navigation';
  import type { Task } from '$lib/models/types';
  import { STATUS_LABELS, PRIORITY_LABELS, STATUS_CYCLE } from '$lib/models/constants';
  import { tasksStore } from '$lib/stores/tasks.svelte';
  import { pagesStore } from '$lib/stores/pages.svelte';
  import { notificationsStore } from '$lib/stores/notifications.svelte';
  import { format, parseISO, isPast } from 'date-fns';
  import LinkChip from './LinkChip.svelte';

  let { task }: { task: Task } = $props();

  const sourcePage = $derived(
    task.sourcePageId ? pagesStore.getById(task.sourcePageId) : undefined
  );

  const isOverdue = $derived(
    task.dueDate !== null && task.status !== 'done' && task.status !== 'cancelled' &&
    isPast(parseISO(task.dueDate))
  );

  const formattedDue = $derived(
    task.dueDate ? format(parseISO(task.dueDate), 'MMM d') : null
  );

  async function cycleStatus() {
    try {
      await tasksStore.updateTask(task.id, { status: STATUS_CYCLE[task.status] });
    } catch {
      notificationsStore.error('Failed to update task status. Your change was reverted.');
    }
  }
</script>

<div
  class="task-card"
  class:done={task.status === 'done'}
  class:cancelled={task.status === 'cancelled'}
  role="button"
  tabindex="0"
  onclick={() => goto(`/tasks/${task.id}`)}
  onkeydown={(e) => e.key === 'Enter' && goto(`/tasks/${task.id}`)}
>
  <div class="card-top">
    <button class="status-dot" onclick={(e) => { e.stopPropagation(); cycleStatus(); }} title="Click to cycle status: {STATUS_LABELS[task.status]}">
      <span class="dot dot-{task.status}"></span>
    </button>
    <a href="/tasks/{task.id}" class="card-title">{task.title}</a>
  </div>

  {#if task.priority !== 'none' || task.dueDate || task.tags.length > 0}
    <div class="card-meta">
      {#if task.priority !== 'none'}
        <span class="badge badge-{task.priority}">{PRIORITY_LABELS[task.priority]}</span>
      {/if}
      {#if formattedDue}
        <span class="due-date" class:overdue={isOverdue}>
          <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <rect x="3" y="4" width="18" height="18" rx="2"/>
            <line x1="16" y1="2" x2="16" y2="6"/>
            <line x1="8" y1="2" x2="8" y2="6"/>
            <line x1="3" y1="10" x2="21" y2="10"/>
          </svg>
          {formattedDue}
        </span>
      {/if}
    </div>
  {/if}

  {#if task.tags.length > 0}
    <div class="card-tags">
      {#each task.tags.slice(0, 3) as tag}
        <span class="tag-pill">{tag}</span>
      {/each}
      {#if task.tags.length > 3}
        <span class="tag-pill">+{task.tags.length - 3}</span>
      {/if}
    </div>
  {/if}

  {#if sourcePage}
    <a
      href="/notes/{sourcePage.id}"
      class="source-note"
      title="From note: {sourcePage.title || 'Untitled'}"
      onclick={(e) => e.stopPropagation()}
    >
      <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/>
        <polyline points="14 2 14 8 20 8"/>
      </svg>
      <span class="source-note-name">{sourcePage.title || 'Untitled'}</span>
    </a>
  {/if}

  {#if task.link}
    <div class="card-link">
      <LinkChip link={task.link} />
    </div>
  {/if}
</div>

<style>
  .task-card {
    background: var(--bg-tertiary);
    border: 1px solid var(--border-subtle);
    border-radius: var(--radius-md);
    padding: 10px 12px;
    cursor: pointer;
    transition: border-color var(--transition-fast), background var(--transition-fast);
  }
  .task-card:hover { border-color: var(--border-default); background: var(--bg-hover); }
  .task-card.done { opacity: 0.6; }
  .task-card.cancelled { opacity: 0.4; }

  .card-top {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    margin-bottom: 6px;
  }

  .status-dot {
    background: none;
    border: none;
    padding: 0;
    cursor: pointer;
    flex-shrink: 0;
    margin-top: 3px;
    line-height: 0;
  }

  .dot {
    display: block;
    width: 10px;
    height: 10px;
    border-radius: 50%;
    border: 2px solid currentColor;
    transition: background var(--transition-fast);
  }
  .dot-todo { color: var(--status-todo); }
  .dot-in-progress { color: var(--status-in-progress); background: var(--status-in-progress); }
  .dot-done { color: var(--status-done); background: var(--status-done); }
  .dot-cancelled { color: var(--status-cancelled); }

  .card-title {
    flex: 1;
    font-size: var(--font-size-sm);
    color: var(--text-primary);
    text-decoration: none;
    line-height: 1.4;
    word-break: break-word;
  }
  .card-title:hover { color: var(--accent); }
  .done .card-title { text-decoration: line-through; color: var(--text-muted); }

  .card-meta {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
    margin-bottom: 5px;
  }

  .due-date {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    font-size: var(--font-size-xs);
    color: var(--text-muted);
  }
  .due-date.overdue { color: var(--priority-urgent); }

  .card-tags {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
  }

  .card-link {
    margin-top: 4px;
  }

  .source-note {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    max-width: 100%;
    margin-top: 6px;
    font-size: var(--font-size-xs);
    color: var(--text-muted);
    text-decoration: none;
    line-height: 1.2;
  }
  .source-note:hover { color: var(--accent); }
  .source-note svg { flex-shrink: 0; }
  .source-note-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
