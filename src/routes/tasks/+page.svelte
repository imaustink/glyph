<script lang="ts">
  import { lanesStore } from '$lib/stores/lanes.svelte';
  import { tasksStore } from '$lib/stores/tasks.svelte';
  import Lane from '$lib/components/tasks/Lane.svelte';
  import LaneConfig from '$lib/components/tasks/LaneConfig.svelte';
  import type { Lane as LaneType, Task } from '$lib/models/types';

  let configuringLane = $state<LaneType | null>(null);

  // Filtered tasks per lane — sync lanes (empty rules / local mode) update immediately;
  // API-backed async lanes update when their network call resolves.
  let filteredByLane = $state<Map<string, Task[]>>(new Map());

  $effect(() => {
    // Track reactive dependencies: tasks array ref and lanes array.
    const _tasks = tasksStore.tasks;
    const lanes = lanesStore.lanes;

    const newMap = new Map<string, Task[]>();
    const asyncWork: Promise<void>[] = [];

    for (const lane of lanes) {
      const result = tasksStore.getFiltered(lane.filterSet);
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
</script>

<div class="board-page">
  <div class="board-header">
    <h1 class="board-title">Task Board</h1>
    <div class="board-actions">
      <span class="task-count">{tasksStore.tasks.length} tasks</span>
    </div>
  </div>

  <div class="board-container">
    <div class="lanes-scroll">
      {#each lanesStore.lanes as lane (lane.id)}
        <Lane
          {lane}
          filteredTasks={filteredByLane.get(lane.id) ?? []}
          onconfig={() => configuringLane = lane}
        />
      {/each}

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

  .task-count {
    font-size: var(--font-size-sm);
    color: var(--text-muted);
  }

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
      padding: 48px 16px 12px;
    }

    .lanes-scroll {
      padding: 12px 16px;
      gap: 12px;
    }
  }
</style>
