import { repositories } from '$lib/storage/config';
import type { FolderBoardRepo } from '$lib/storage/config';
import type { Lane, Task, TreeNode } from '$lib/models/types';
import { now, makeTimestamps } from '$lib/utils/time';
import { uuid } from '$lib/utils/uuid';

export function createFolderBoardStore(injectedRepo?: FolderBoardRepo) {
  let folderId = $state<string | null>(null);
  let folder = $state<TreeNode | null>(null);
  let lanes = $state<Lane[]>([]);
  let tasks = $state<Task[]>([]);
  let canEdit = $state(false);
  let loading = $state(false);
  let loaded = $state(false);
  let error = $state<string | null>(null);

  const repo = injectedRepo ?? repositories.folderBoard;

  async function load(id: string) {
    if (folderId === id && (loaded || loading)) return; // already loaded or in-flight
    folderId = id;
    loaded = false;
    loading = true;
    error = null;
    try {
      const [meta, folderLanes, folderTasks] = await Promise.all([
        repo.getFolder(id),
        repo.getLanes(id),
        repo.getTasks(id)
      ]);
      folder = meta.folder;
      canEdit = meta.canEdit;
      lanes = folderLanes;
      tasks = folderTasks;
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to load folder board';
    } finally {
      loading = false;
      loaded = true;
    }
  }

  /** Re-fetch tasks only (used after task create/update/delete on the board). */
  async function reloadTasks() {
    if (!folderId) return;
    try {
      tasks = await repo.getTasks(folderId);
    } catch {
      // Non-fatal — stale tasks remain visible
    }
  }

  async function createLane(title: string): Promise<Lane | null> {
    if (!folderId) return null;
    const maxOrder = lanes.reduce((m, l) => Math.max(m, l.order), -1);
    const draft = {
      title,
      filterSet: { conjunction: 'and' as const, rules: [] },
      sortConfig: { mode: 'auto' as const },
      order: maxOrder + 1,
      folderId,
      ...makeTimestamps()
    };
    // Optimistic insert
    const optimistic: Lane = { id: uuid(), ...draft };
    lanes = [...lanes, optimistic];
    try {
      const created = await repo.createLane(folderId, draft);
      // Replace optimistic entry with server response (has real id + timestamps)
      lanes = lanes.map((l) => (l.id === optimistic.id ? created : l));
      return created;
    } catch (e) {
      lanes = lanes.filter((l) => l.id !== optimistic.id);
      throw e;
    }
  }

  async function updateLane(laneId: string, patch: Partial<Omit<Lane, 'id'>>): Promise<void> {
    if (!folderId) return;
    const prev = lanes.find((l) => l.id === laneId);
    if (!prev) return;
    const optimistic = { ...prev, ...patch, updatedAt: now() };
    lanes = lanes.map((l) => (l.id === laneId ? optimistic : l));
    try {
      const updated = await repo.updateLane(folderId, laneId, { ...patch, updatedAt: now() });
      lanes = lanes.map((l) => (l.id === laneId ? updated : l));
    } catch (e) {
      lanes = lanes.map((l) => (l.id === laneId ? prev : l));
      throw e;
    }
  }

  async function deleteLane(laneId: string): Promise<void> {
    if (!folderId) return;
    lanes = lanes.filter((l) => l.id !== laneId);
    try {
      await repo.deleteLane(folderId, laneId);
    } catch (e) {
      // Re-fetch to restore state
      if (folderId) lanes = await repo.getLanes(folderId);
      throw e;
    }
  }

  /** Clear the store when navigating away from the folder board. */
  function reset() {
    folderId = null;
    folder = null;
    lanes = [];
    tasks = [];
    canEdit = false;
    loading = false;
    loaded = false;
    error = null;
  }

  return {
    get folderId() { return folderId; },
    get folder() { return folder; },
    get lanes() { return lanes; },
    get tasks() { return tasks; },
    get canEdit() { return canEdit; },
    get loading() { return loading; },
    get loaded() { return loaded; },
    get error() { return error; },
    load,
    reloadTasks,
    createLane,
    updateLane,
    deleteLane,
    reset
  };
}

export const folderBoardStore = createFolderBoardStore();
