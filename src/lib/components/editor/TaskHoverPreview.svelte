<script lang="ts">
  import { tasksStore } from '$lib/stores/tasks.svelte';
  import { goto } from '$app/navigation';
  import { STATUS_LABELS, PRIORITY_LABELS } from '$lib/models/constants';

  let {
    taskId,
    x,
    y,
    onclose
  }: {
    taskId: string;
    x: number;
    y: number;
    onclose: () => void;
  } = $props();

  const task = $derived(tasksStore.getById(taskId));
</script>

{#if task}
  <div
    class="preview"
    style="left: {x}px; top: {y}px; transform: translate(-50%, -100%);"
    onmouseenter={onclose}
    role="tooltip"
  >
    <div class="preview-header">
      <span class="preview-title">{task.title}</span>
      <a href="/tasks/{task.id}" class="preview-link" onclick={onclose}>
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/>
          <polyline points="15 3 21 3 21 9"/>
          <line x1="10" y1="14" x2="21" y2="3"/>
        </svg>
        Open
      </a>
    </div>

    <div class="preview-meta">
      <span class="badge badge-{task.status}">{STATUS_LABELS[task.status]}</span>
      {#if task.priority !== 'none'}
        <span class="badge badge-{task.priority}">{PRIORITY_LABELS[task.priority]}</span>
      {/if}
      {#if task.dueDate}
        <span class="due">
          <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <rect x="3" y="4" width="18" height="18" rx="2"/>
            <line x1="16" y1="2" x2="16" y2="6"/>
            <line x1="8" y1="2" x2="8" y2="6"/>
            <line x1="3" y1="10" x2="21" y2="10"/>
          </svg>
          {task.dueDate}
        </span>
      {/if}
    </div>

    {#if task.tags.length > 0}
      <div class="preview-tags">
        {#each task.tags as tag}
          <span class="tag-pill">{tag}</span>
        {/each}
      </div>
    {/if}
  </div>
{/if}

<style>
  .preview {
    position: fixed;
    z-index: 60;
    background: var(--bg-modal);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-md);
    padding: 12px 14px;
    min-width: 220px;
    max-width: 320px;
    pointer-events: auto;
  }

  .preview::after {
    content: '';
    position: absolute;
    left: 50%;
    bottom: -6px;
    transform: translateX(-50%);
    border-width: 6px 6px 0;
    border-style: solid;
    border-color: var(--border-default) transparent transparent;
  }

  .preview-header {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    margin-bottom: 8px;
  }

  .preview-title {
    flex: 1;
    font-size: var(--font-size-sm);
    font-weight: 600;
    color: var(--text-heading);
    line-height: 1.4;
  }

  .preview-link {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-size: var(--font-size-xs);
    color: var(--accent);
    white-space: nowrap;
    flex-shrink: 0;
    text-decoration: none;
  }
  .preview-link:hover { text-decoration: underline; }

  .preview-meta {
    display: flex;
    flex-wrap: wrap;
    gap: 5px;
    align-items: center;
  }

  .due {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-size: var(--font-size-xs);
    color: var(--text-secondary);
  }

  .preview-tags {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    margin-top: 8px;
  }
</style>
