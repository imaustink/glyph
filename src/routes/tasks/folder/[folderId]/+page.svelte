<script lang="ts">
  import { page } from '$app/state';
  import { onMount, onDestroy } from 'svelte';
  import { folderBoardStore } from '$lib/stores/folderBoard.svelte';
  import { notificationsStore } from '$lib/stores/notifications.svelte';
  import { pagesStore } from '$lib/stores/pages.svelte';
  import { storageMode } from '$lib/storage/config';
  import { applyFilter } from '$lib/storage/filterUtils';
  import Lane from '$lib/components/tasks/Lane.svelte';
  import LaneConfig from '$lib/components/tasks/LaneConfig.svelte';
  import ShareDialog from '$lib/components/shared/ShareDialog.svelte';
  import { dragHandleZone, type DndEvent } from 'svelte-dnd-action';
  import { matchesSearchText } from '$lib/storage/filterUtils';
  import type { Lane as LaneType, Task } from '$lib/models/types';

  const folderId = $derived(page.params.folderId);

  let configuringLane = $state<LaneType | null>(null);
  let showShareDialog = $state(false);
  let searchQuery = $state('');

  // Lane reordering — mirrors the store's lanes except mid-drag, when the
  // dnd library owns the array so the effect below doesn't fight it.
  let laneItems = $state<LaneType[]>([]);
  let isDraggingLanes = false;
  const laneFlipDurationMs = 200;

  $effect(() => {
    if (isDraggingLanes) return;
    laneItems = folderBoardStore.lanes;
  });

  function handleLaneConsider(e: CustomEvent<DndEvent<LaneType>>) {
    isDraggingLanes = true;
    laneItems = e.detail.items;
  }

  async function handleLaneFinalize(e: CustomEvent<DndEvent<LaneType>>) {
    laneItems = e.detail.items;
    isDraggingLanes = false;
    try {
      await folderBoardStore.reorderLanes(laneItems.map((l) => l.id));
    } catch {
      notificationsStore.error('Failed to save lane order.');
    }
  }

  $effect(() => {
    if (folderId) folderBoardStore.load(folderId);
  });

  onDestroy(() => {
    folderBoardStore.reset();
  });

  // Compute filtered tasks per lane from the folder board's task list.
  const filteredByLane = $derived.by(() => {
    const tasks = folderBoardStore.tasks;
    // Track page nodes so lanes re-filter when a note's tags change.
    const _nodes = pagesStore.nodes;
    const filterCtx = {
      getSourcePageTags: (task: Task) =>
        task.sourcePageId ? (pagesStore.getById(task.sourcePageId)?.tags ?? []) : []
    };
    const map = new Map<string, Task[]>();
    for (const lane of folderBoardStore.lanes) {
      map.set(lane.id, applyFilter(tasks, lane.filterSet, filterCtx));
    }
    return map;
  });

  // Layered on top of each lane's own filter — a plain client-side narrowing,
  // not another (async) lane re-filter.
  const displayedByLane = $derived.by(() => {
    if (!searchQuery.trim()) return filteredByLane;
    const map = new Map<string, Task[]>();
    for (const [laneId, tasks] of filteredByLane) {
      map.set(laneId, tasks.filter((t) => matchesSearchText(t, searchQuery)));
    }
    return map;
  });

  async function handleCreateLane() {
    try {
      await folderBoardStore.createLane('New Lane');
    } catch {
      notificationsStore.error('Failed to create lane.');
    }
  }
</script>

<div class="board-page">
  <div class="board-header">
    <div class="board-title-row">
      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="folder-icon">
        <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/>
      </svg>
      <h1 class="board-title">
        {folderBoardStore.folder?.title ?? 'Loading…'}
      </h1>
    </div>
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
      <span class="task-count">{folderBoardStore.tasks.length} tasks</span>
      {#if storageMode === 'api' && folderBoardStore.canEdit}
        <button
          class="btn-ghost share-btn"
          onclick={() => showShareDialog = true}
          title="Share this folder"
        >
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <circle cx="18" cy="5" r="3"/><circle cx="6" cy="12" r="3"/><circle cx="18" cy="19" r="3"/>
            <line x1="8.59" y1="13.51" x2="15.42" y2="17.49"/><line x1="15.41" y1="6.51" x2="8.59" y2="10.49"/>
          </svg>
          Share
        </button>
      {/if}
    </div>
  </div>

  {#if folderBoardStore.error}
    <div class="board-error">{folderBoardStore.error}</div>
  {:else}
    <div class="board-container">
      <div class="lanes-scroll">
        <div
          class="lanes-dndzone"
          use:dragHandleZone={{
            items: laneItems,
            flipDurationMs: laneFlipDurationMs,
            type: 'lane',
            dropTargetStyle: {},
            dragDisabled: !folderBoardStore.canEdit
          }}
          onconsider={handleLaneConsider}
          onfinalize={handleLaneFinalize}
        >
          {#each laneItems as lane (lane.id)}
            <Lane
              {lane}
              filteredTasks={displayedByLane.get(lane.id) ?? []}
              readonly={!folderBoardStore.canEdit}
              onconfig={() => { if (folderBoardStore.canEdit) configuringLane = lane; }}
            />
          {/each}
        </div>

        {#if folderBoardStore.canEdit}
          <div class="add-lane-col">
            <button class="add-lane-btn" onclick={handleCreateLane}>
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
                <line x1="12" y1="5" x2="12" y2="19"/>
                <line x1="5" y1="12" x2="19" y2="12"/>
              </svg>
              Add lane
            </button>
          </div>
        {/if}
      </div>
    </div>
  {/if}
</div>

{#if configuringLane}
  <LaneConfig
    lane={configuringLane}
    onclose={() => configuringLane = null}
    onupdate={(id, patch) => folderBoardStore.updateLane(id, patch)}
    ondelete={async (id) => { await folderBoardStore.deleteLane(id); configuringLane = null; }}
  />
{/if}

{#if showShareDialog && folderId}
  <ShareDialog
    resourceType="folder"
    resourceId={folderId}
    onclose={() => showShareDialog = false}
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

  .board-title-row {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .folder-icon {
    color: var(--text-muted);
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
    gap: 12px;
  }

  .task-count {
    font-size: var(--font-size-sm);
    color: var(--text-muted);
    white-space: nowrap;
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

  .share-btn {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: var(--font-size-sm);
    padding: 6px 12px;
    border-radius: var(--radius-md);
    color: var(--text-secondary);
    border: 1px solid var(--border-default);
  }
  .share-btn:hover {
    color: var(--text-primary);
    border-color: var(--border-strong);
    background: var(--bg-hover);
  }

  .board-error {
    padding: 32px;
    color: var(--text-muted);
    font-size: var(--font-size-sm);
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
