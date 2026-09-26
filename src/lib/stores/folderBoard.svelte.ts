import { repositories } from '$lib/storage/config';
import type { FolderBoardRepo } from '$lib/storage/config';
import type { ITaskRepository } from '$lib/storage/interfaces';
import type { Lane, Task, TreeNode } from '$lib/models/types';
import { now, makeTimestamps } from '$lib/utils/time';
import { uuid } from '$lib/utils/uuid';
import { createOptimisticWriter } from './optimisticWriter';

/** The part of the task repository the folder board writes through. */
type BoardTaskRepo = Pick<ITaskRepository, 'update'>;

export function createFolderBoardStore(injectedRepo?: FolderBoardRepo, injectedTaskRepo?: BoardTaskRepo) {
  let folderId = $state<string | null>(null);
  let folder = $state<TreeNode | null>(null);
  let lanes = $state<Lane[]>([]);
  let tasks = $state<Task[]>([]);
  let canEdit = $state(false);
  let loading = $state(false);
  let loaded = $state(false);
  let error = $state<string | null>(null);

  const repo = injectedRepo ?? repositories.folderBoard;
  const taskRepo = injectedTaskRepo ?? repositories.tasks;

  /**
   * Bumped whenever the board switches folder (load/reset). A fetch started
   * for an earlier generation must not write its result into the current one.
   */
  let generation = 0;

  async function load(id: string) {
    if (folderId === id && (loaded || loading)) return; // already loaded or in-flight
    const gen = ++generation;
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
      if (gen !== generation) return;
      folder = meta.folder;
      canEdit = meta.canEdit;
      lanes = folderLanes;
      tasks = folderTasks;
    } catch (e) {
      if (gen !== generation) return;
      error = e instanceof Error ? e.message : 'Failed to load folder board';
    } finally {
      if (gen === generation) {
        loading = false;
        loaded = true;
      }
    }
  }

  /** Re-fetch tasks only (used after task create/update/delete on the board). */
  async function reloadTasks() {
    if (!folderId) return;
    const gen = generation;
    try {
      const fresh = await repo.getTasks(folderId);
      if (gen === generation) tasks = fresh;
    } catch {
      // Non-fatal — stale tasks remain visible
    }
  }

  /** Re-fetch lanes only (used to recover from a failed lane write). */
  async function reloadLanes() {
    if (!folderId) return;
    const gen = generation;
    try {
      const fresh = await repo.getLanes(folderId);
      if (gen === generation) lanes = fresh;
    } catch {
      // Non-fatal — the optimistic state stays visible
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

  /** Serialized, sequenced optimistic lane writes (see optimisticWriter). */
  const laneWriter = createOptimisticWriter<Lane>({
    get: (id) => lanes.find((l) => l.id === id),
    replace: (lane) => { lanes = lanes.map((l) => (l.id === lane.id ? lane : l)); }
  });

  async function updateLane(laneId: string, patch: Partial<Omit<Lane, 'id'>>): Promise<void> {
    const fid = folderId;
    if (!fid) return;
    const full = { ...patch, updatedAt: now() };
    return laneWriter.update(
      laneId,
      full,
      () => repo.updateLane(fid, laneId, full),
      async () => (await repo.getLanes(fid)).find((l) => l.id === laneId) ?? null
    );
  }

  /**
   * Persist a new lane order. There is no atomic folder-lane reorder
   * endpoint, so this is one write per lane; if any fails, the lanes are
   * refetched so the board shows what the server actually holds rather than
   * a snapshot that may no longer be true.
   */
  async function reorderLanes(orderedIds: string[]): Promise<void> {
    const fid = folderId;
    if (!fid) return;
    const timestamp = now();
    const byId = new Map(lanes.map((l) => [l.id, l]));
    const next = orderedIds
      .map((id, i) => {
        const lane = byId.get(id);
        return lane ? { ...lane, order: i, updatedAt: timestamp } : null;
      })
      .filter((l): l is Lane => l !== null);
    lanes = next;
    const changed = next.filter((l) => byId.get(l.id)?.order !== l.order);
    const results = await Promise.allSettled(
      changed.map((lane) => repo.updateLane(fid, lane.id, { order: lane.order, updatedAt: timestamp }))
    );
    const failure = results.find((r): r is PromiseRejectedResult => r.status === 'rejected');
    if (failure) {
      await reloadLanes();
      throw failure.reason;
    }
  }

  async function deleteLane(laneId: string): Promise<void> {
    if (!folderId) return;
    lanes = lanes.filter((l) => l.id !== laneId);
    try {
      await repo.deleteLane(folderId, laneId);
    } catch (e) {
      // Re-fetch to restore state
      await reloadLanes();
      throw e;
    }
  }

  /**
   * Update a task shown on this board (e.g. a cross-lane drop). Board tasks
   * are not necessarily in the global tasks store (another user's task in a
   * shared folder), so the board keeps its own copy in step. On failure the
   * board's tasks are refetched.
   */
  async function updateTask(taskId: string, patch: Partial<Omit<Task, 'id' | 'createdAt'>>): Promise<void> {
    const full = { ...patch, updatedAt: now() };
    const prev = tasks.find((t) => t.id === taskId);
    if (!prev) return;
    tasks = tasks.map((t) => (t.id === taskId ? { ...t, ...full } : t));
    try {
      const saved = await taskRepo.update(taskId, full);
      if (saved) tasks = tasks.map((t) => (t.id === taskId ? saved : t));
    } catch (e) {
      await reloadTasks();
      throw e;
    }
  }

  /** Clear the store when navigating away from the folder board. */
  function reset() {
    generation++;
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
    reorderLanes,
    updateTask,
    reset
  };
}

export const folderBoardStore = createFolderBoardStore();
