<script lang="ts">
  import { untrack } from 'svelte';
  import type { Lane, Task, FilterSet, TaskStatus } from '$lib/models/types';
  import { tasksStore } from '$lib/stores/tasks.svelte';
  import { lanesStore } from '$lib/stores/lanes.svelte';
  import { notificationsStore } from '$lib/stores/notifications.svelte';
  import { autoSortProvider } from '$lib/sort/AutoSortProvider';
  import { fieldSortProvider } from '$lib/sort/FieldSortProvider';

  import { dndzone, TRIGGERS, type DndEvent } from 'svelte-dnd-action';
  import TaskCard from './TaskCard.svelte';

  let {
    lane,
    filteredTasks,
    onconfig,
    readonly = false
  }: {
    lane: Lane;
    filteredTasks: Task[];
    onconfig: () => void;
    /** When true, hides add-task and disables lane config/drag for read-only viewers. */
    readonly?: boolean;
  } = $props();

  let sortedTasks = $state<Task[]>([]);
  let loading = $state(false);
  let editingTitle = $state(false);
  let titleValue = $state('');
  let lastCacheKey = '';
  let wasDragging = false;

  interface DndTaskItem { id: string; task: Task; [key: string]: any }
  let dndItems = $state<DndTaskItem[]>([]);
  let isDragging = $state(false);
  const flipDurationMs = 200;

  const autoSort = autoSortProvider;
  const fieldSort = fieldSortProvider;
  /** Generation counter to discard stale sort results. */
  let sortGeneration = 0;

  async function resortTasks() {
    const gen = ++sortGeneration;
    const provider = lane.sortConfig.mode === 'auto' ? autoSort : fieldSort;

    try {
      if (lane.sortConfig.mode === 'manual' && lane.sortConfig.taskOrder) {
        // Manual: use explicit order array, appending any new tasks at end
        const orderMap = new Map(lane.sortConfig.taskOrder.map((id, i) => [id, i]));
        sortedTasks = [...filteredTasks].sort((a, b) => {
          const ai = orderMap.get(a.id) ?? Infinity;
          const bi = orderMap.get(b.id) ?? Infinity;
          return ai - bi;
        });
      } else {
        // Race the sort against a 150ms timer to decide if we show loading state.
        // Sync providers resolve immediately and never hit the timer.
        const loadingTimer = setTimeout(() => { loading = true; }, 150);
        const result = await provider.sort(filteredTasks, lane.sortConfig);
        clearTimeout(loadingTimer);
        // Discard if a newer sort was triggered while we were awaiting
        if (gen !== sortGeneration) return;
        sortedTasks = result;
      }
    } finally {
      loading = false;
    }
    dndItems = sortedTasks.map(t => ({ id: `${lane.id}:${t.id}`, task: t }));
  }

  // Re-sort when filtered tasks or lane sort config changes.
  $effect(() => {
    // Track dependencies — filteredTasks is a new array ref on every parent recompute
    const taskIds = filteredTasks.map(t => `${t.id}:${t.status}:${t.priority}`).join(',');
    lane.sortConfig;

    // Always resort when transitioning out of a drag (dndItems may have been
    // mutated by the dnd library and needs to be rebuilt from source of truth).
    const dragEnded = wasDragging && !isDragging;
    wasDragging = isDragging;

    if (isDragging) return;

    // Fingerprint prevents re-triggering expensive async sorts when nothing changed
    const cacheKey = `${taskIds}|${lane.sortConfig.mode}|${lane.sortConfig.field ?? ''}|${lane.sortConfig.direction ?? ''}|${lane.sortConfig.taskOrder?.length ?? 0}`;

    if (!dragEnded && cacheKey === lastCacheKey) return;
    lastCacheKey = cacheKey;

    untrack(() => resortTasks());
  });

  function extractTaskId(dndId: string): string {
    const colonIndex = dndId.indexOf(':');
    return colonIndex >= 0 ? dndId.substring(colonIndex + 1) : dndId;
  }

  function inferStatusFromFilter(filterSet: FilterSet): TaskStatus | null {
    for (const rule of filterSet.rules) {
      if (rule.field === 'status' && rule.operator === 'eq' && typeof rule.value === 'string') {
        return rule.value as TaskStatus;
      }
    }
    return null;
  }

  // Reject foreign items whose task is already present in this lane.
  // This prevents dragging e.g. from "In Progress" into "All Tasks" when the task is already shown there.
  function filterDuplicates(items: DndTaskItem[]): DndTaskItem[] {
    const nativeTaskIds = new Set(sortedTasks.map(t => t.id));
    return items.filter(item => {
      if (item.id.startsWith(lane.id + ':')) return true;
      return !nativeTaskIds.has(extractTaskId(item.id));
    });
  }

  function handleConsider(e: CustomEvent<DndEvent<DndTaskItem>>) {
    isDragging = true;
    dndItems = filterDuplicates(e.detail.items);
  }

  async function handleFinalize(e: CustomEvent<DndEvent<DndTaskItem>>) {
    const { trigger } = e.detail.info;
    dndItems = filterDuplicates(e.detail.items);

    if (trigger === TRIGGERS.DROPPED_INTO_ANOTHER || trigger === TRIGGERS.DROPPED_OUTSIDE_OF_ANY) {
      isDragging = false;
      return;
    }

    // This lane received a drop — handle cross-lane status change
    for (const item of dndItems) {
      if (!item.id.startsWith(lane.id + ':')) {
        const taskId = extractTaskId(item.id);
        const targetStatus = inferStatusFromFilter(lane.filterSet);
        if (targetStatus && item.task.status !== targetStatus) {
          try {
            await tasksStore.updateTask(taskId, { status: targetStatus });
            item.task = { ...item.task, status: targetStatus };
          } catch {
            notificationsStore.error('Failed to update task status.');
          }
        }
        item.id = `${lane.id}:${taskId}`;
      }
    }

    // Persist task order for manual sort mode
    if (lane.sortConfig.mode === 'manual') {
      const newOrder = dndItems.map(item => extractTaskId(item.id));
      try {
        await lanesStore.updateLane(lane.id, {
          sortConfig: { ...lane.sortConfig, taskOrder: newOrder }
        });
      } catch {
        notificationsStore.error('Failed to save task order.');
      }
    }

    isDragging = false;
  }

  function startEditTitle() {
    editingTitle = true;
    titleValue = lane.title;
  }

  async function commitTitle() {
    editingTitle = false;
    if (titleValue.trim()) {
      try {
        await lanesStore.updateLane(lane.id, { title: titleValue.trim() });
      } catch {
        notificationsStore.error('Failed to rename lane.');
      }
    }
  }

  function handleTitleKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') { e.preventDefault(); commitTitle(); }
    if (e.key === 'Escape') { editingTitle = false; }
  }
</script>

<div class="lane">
  <div class="lane-header">
    <div class="lane-title-row">
      {#if editingTitle}
        <!-- svelte-ignore a11y_autofocus -->
        <input
          class="lane-title-input"
          bind:value={titleValue}
          onblur={commitTitle}
          onkeydown={handleTitleKeydown}
          autofocus
        />
      {:else}
        <button class="lane-title" ondblclick={startEditTitle}>
          {lane.title}
        </button>
      {/if}
      <span class="lane-count">{sortedTasks.length}</span>
    </div>
    {#if !readonly}
    <button class="btn-ghost icon-btn" onclick={onconfig} title="Configure lane">
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <circle cx="12" cy="12" r="3"/>
        <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/>
      </svg>
    </button>
    {/if}
  </div>

  {#if loading}
    <div class="lane-body">
      {#each Array(3) as _, i}
        <div class="skeleton" style="height: 72px; margin-bottom: 8px; width: 100%;"></div>
      {/each}
    </div>
  {:else}
    <div
      class="lane-body"
      use:dndzone={{items: dndItems, flipDurationMs, type: 'task', dropTargetStyle: {}, dropTargetClasses: ['lane-drop-active'], dragDisabled: readonly}}
      onconsider={handleConsider}
      onfinalize={handleFinalize}
    >
      {#each dndItems as item (item.id)}
        <TaskCard task={item.task} />
      {/each}
    </div>
  {/if}
</div>

<style>
  .lane {
    width: 280px;
    min-width: 280px;
    background: var(--bg-secondary);
    border: 1px solid var(--border-subtle);
    border-radius: var(--radius-lg);
    display: flex;
    flex-direction: column;
    max-height: calc(100vh - 120px);
    flex-shrink: 0;
  }

  .lane-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 12px 14px 8px;
    border-bottom: 1px solid var(--border-subtle);
    flex-shrink: 0;
  }

  .lane-title-row {
    display: flex;
    align-items: center;
    gap: 8px;
    flex: 1;
    min-width: 0;
  }

  .lane-title {
    background: none;
    border: none;
    padding: 0;
    font-size: var(--font-size-sm);
    font-weight: 600;
    color: var(--text-heading);
    cursor: pointer;
    text-align: left;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .lane-title:hover { color: var(--text-primary); }

  .lane-title-input {
    flex: 1;
    background: var(--bg-primary);
    border: 1px solid var(--accent);
    border-radius: var(--radius-sm);
    padding: 2px 6px;
    font-size: var(--font-size-sm);
    font-weight: 600;
    color: var(--text-heading);
    outline: none;
    min-width: 0;
  }

  .lane-count {
    font-size: var(--font-size-xs);
    color: var(--text-muted);
    background: var(--bg-tertiary);
    border-radius: 999px;
    padding: 1px 7px;
    flex-shrink: 0;
  }

  .icon-btn {
    padding: 4px;
    border-radius: var(--radius-sm);
    color: var(--text-muted);
    line-height: 0;
    flex-shrink: 0;
  }
  .icon-btn:hover { color: var(--text-primary); }

  .lane-body {
    flex: 1;
    overflow-y: auto;
    padding: 10px;
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-height: 60px;
  }

  :global(.lane-drop-active) {
    background: var(--bg-hover) !important;
  }
</style>
