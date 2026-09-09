<script lang="ts">
  import { lanesStore } from '$lib/stores/lanes.svelte';
  import { tasksStore } from '$lib/stores/tasks.svelte';
  import { pagesStore } from '$lib/stores/pages.svelte';
  import { uiStore } from '$lib/stores/ui.svelte';
  import { notificationsStore } from '$lib/stores/notifications.svelte';
  import Lane from '$lib/components/tasks/Lane.svelte';
  import LaneConfig from '$lib/components/tasks/LaneConfig.svelte';
  import { dragHandleZone, type DndEvent } from 'svelte-dnd-action';
  import { matchesSearchText } from '$lib/storage/filterUtils';
  import type { Lane as LaneType, Task } from '$lib/models/types';

  let configuringLane = $state<LaneType | null>(null);
  let searchQuery = $state('');

  // Lane reordering — mirrors the store's lanes except mid-drag, when the
  // dnd library owns the array so the effect below doesn't fight it.
  let laneItems = $state<LaneType[]>([]);
  let isDraggingLanes = false;
  const laneFlipDurationMs = 200;

  $effect(() => {
    if (isDraggingLanes) return;
    laneItems = lanesStore.lanes;
  });

  function handleLaneConsider(e: CustomEvent<DndEvent<LaneType>>) {
    isDraggingLanes = true;
    laneItems = e.detail.items;
  }

  async function handleLaneFinalize(e: CustomEvent<DndEvent<LaneType>>) {
    laneItems = e.detail.items;
    isDraggingLanes = false;
    try {
      await lanesStore.reorderLanes(laneItems.map((l) => l.id));
    } catch {
      notificationsStore.error('Failed to save lane order.');
    }
  }

  // Filtered tasks per lane — sync lanes (empty rules / local mode) update immediately;
  // API-backed async lanes update when their network call resolves.
  let filteredByLane = $state<Map<string, Task[]>>(new Map());

  $effect(() => {
    // Track reactive dependencies: tasks array ref and lanes array.
    const _tasks = tasksStore.tasks;
    const lanes = lanesStore.lanes;
    // Track page nodes so lanes re-filter when a note's tags change.
    const _nodes = pagesStore.nodes;

    // Resolves the tags of a task's source note for the synthetic
    // `sourcePageTags` filter field (local-mode / client-side filtering only).
    const filterCtx = {
      getSourcePageTags: (task: Task) =>
        task.sourcePageId ? (pagesStore.getById(task.sourcePageId)?.tags ?? []) : []
    };

    const newMap = new Map<string, Task[]>();
    const asyncWork: Promise<void>[] = [];

    for (const lane of lanes) {
      const result = tasksStore.getFiltered(lane.filterSet, filterCtx);
      if (result instanceof Promise) {
        asyncWork.push(result.then((filtered) => { newMap.set(lane.id, filtered); }));
      } else {
        newMap.set(lane.id, result);
      }
    }

    // Immediately expose synchronous results (local mode + empty-rule lanes in API mode).
    filteredByLane = new Map(newMap);

    // Then update async lanes when their API calls complete.
    if (asyncWork.length > 0) {
      Promise.all(asyncWork).then(() => { filteredByLane = new Map(newMap); });
    }
  });

  // Layered on top of each lane's own filter — narrows what's already shown
  // rather than triggering another (possibly async) lane re-filter, so typing
  // in the search box never issues extra API calls.
  const displayedByLane = $derived.by(() => {
    if (!searchQuery.trim()) return filteredByLane;
    const map = new Map<string, Task[]>();
    for (const [laneId, tasks] of filteredByLane) {
      map.set(laneId, tasks.filter((t) => matchesSearchText(t, searchQuery)));
    }
    return map;
  });
</script>

<div class="board-page">
  <div class="board-header">
    <h1 class="board-title">Task Board</h1>
    <div class="board-actions">
      <div class="board-search">
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <circle cx="11" cy="11" r="8"/>
          <line x1="21" y1="21" x2="16.65" y2="16.65"/>
        </svg>
        <input
          type="text"
          class="board-search-input"
          placeholder="Search tasks…"
          bind:value={searchQuery}
          aria-label="Search tasks"
        />
        {#if searchQuery}
          <button class="board-search-clear" onclick={() => searchQuery = ''} aria-label="Clear search">
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
              <line x1="18" y1="6" x2="6" y2="18"/>
              <line x1="6" y1="6" x2="18" y2="18"/>
            </svg>
          </button>
        {/if}
      </div>
      <span class="task-count">{tasksStore.tasks.length} tasks</span>
      <button class="btn-primary new-task-btn" onclick={() => uiStore.openCreateTask()}>
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
          <line x1="12" y1="5" x2="12" y2="19"/>
          <line x1="5" y1="12" x2="19" y2="12"/>
        </svg>
        New task
      </button>
    </div>
  </div>

  <div class="board-container">
    <div class="lanes-scroll">
      <div
        class="lanes-dndzone"
        use:dragHandleZone={{ items: laneItems, flipDurationMs: laneFlipDurationMs, type: 'lane', dropTargetStyle: {} }}
        onconsider={handleLaneConsider}
        onfinalize={handleLaneFinalize}
      >
        {#each laneItems as lane (lane.id)}
          <Lane
            {lane}
            filteredTasks={displayedByLane.get(lane.id) ?? []}
            onconfig={() => configuringLane = lane}
          />
        {/each}
      </div>

      <div class="add-lane-col">
        <button
          class="add-lane-btn"
          onclick={() => lanesStore.createLane('New Lane')}
        >
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
            <line x1="12" y1="5" x2="12" y2="19"/>
            <line x1="5" y1="12" x2="19" y2="12"/>
          </svg>
          Add lane
        </button>
      </div>
    </div>
  </div>
</div>

{#if configuringLane}
  <LaneConfig
    lane={configuringLane}
    onclose={() => configuringLane = null}
  />
{/if}

<style>
  .board-page {
    display: flex;
    flex-direction: column;
    height: 100%;
    overflow: hidden;
  }

  .board-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 24px 32px 16px;
    border-bottom: 1px solid var(--border-subtle);
    flex-shrink: 0;
  }

  .board-title {
    font-size: var(--font-size-xl);
    font-weight: 700;
    color: var(--text-heading);
    margin: 0;
  }

  .board-actions {
    display: flex;
    align-items: center;
    gap: 14px;
  }

  .task-count {
    font-size: var(--font-size-sm);
    color: var(--text-muted);
    white-space: nowrap;
  }

  .new-task-btn {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .board-search {
    display: flex;
    align-items: center;
    gap: 6px;
    width: 220px;
    padding: 6px 10px;
    background: var(--bg-tertiary);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    color: var(--text-muted);
    transition: border-color var(--transition-fast);
  }
  .board-search:focus-within { border-color: var(--accent); color: var(--text-primary); }
  .board-search svg { flex-shrink: 0; }

  .board-search-input {
    flex: 1;
    min-width: 0;
    background: none;
    border: none;
    outline: none;
    padding: 0;
    font-size: var(--font-size-sm);
    color: var(--text-primary);
  }
  .board-search-input::placeholder { color: var(--text-muted); }

  .board-search-clear {
    display: flex;
    flex-shrink: 0;
    color: var(--text-muted);
    padding: 2px;
    border-radius: var(--radius-sm);
    line-height: 0;
  }
  .board-search-clear:hover { color: var(--text-primary); background: var(--bg-hover); }

  .board-container {
    flex: 1;
    overflow: hidden;
  }

  .lanes-scroll {
    display: flex;
    gap: 16px;
    padding: 20px 24px;
    height: 100%;
    overflow-x: auto;
    align-items: flex-start;
  }

  .lanes-dndzone {
    display: flex;
    gap: 16px;
    align-items: flex-start;
    flex-shrink: 0;
  }

  .add-lane-col {
    flex-shrink: 0;
    width: 260px;
  }

  .add-lane-btn {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    background: var(--bg-tertiary);
    border: 1px dashed var(--border-default);
    border-radius: var(--radius-md);
    color: var(--text-muted);
    padding: 10px 14px;
    font-size: var(--font-size-sm);
    cursor: pointer;
    transition: background var(--transition-fast), color var(--transition-fast);
  }
  .add-lane-btn:hover {
    background: var(--bg-hover);
    color: var(--text-primary);
    border-color: var(--border-strong);
  }

  /* ─── Mobile responsive ─────────────────────────────────────────────────── */
  @media (max-width: 768px) {
    .board-header {
      /* Clear the fixed hamburger button (top:10 + height:36 = 46px) */
      padding: 56px 16px 12px;
      flex-wrap: wrap;
      row-gap: 10px;
    }

    .board-actions {
      width: 100%;
      justify-content: space-between;
    }

    .board-search {
      width: auto;
      flex: 1;
    }

    .lanes-scroll {
      padding: 12px 16px;
      gap: 12px;
    }
  }
</style>
