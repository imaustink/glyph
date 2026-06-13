/**
 * Unit tests for the folder board store.
 *
 * Injects a mock FolderBoardRepo so no real storage is involved.
 * Tests: load (empty + non-empty results), idempotency after load,
 *        in-flight deduplication, reset, createLane, updateLane, deleteLane.
 *
 * Regression: absence of lanes/tasks must never cause an infinite fetch cycle.
 * The `loaded` flag (not `lanes.length`) guards re-entry so that an empty
 * result from the API does not re-trigger the calling $effect.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { createFolderBoardStore } from './folderBoard.svelte';
import type { FolderBoardRepo } from '$lib/storage/config';
import type { Lane, Task, TreeNode } from '$lib/models/types';

function makeFolder(overrides: Partial<TreeNode> = {}): TreeNode {
  return {
    id: 'folder-1',
    type: 'folder',
    title: 'My Folder',
    parentId: null,
    order: 0,
    tags: [],
    isPrivate: false,
    orgId: null,
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
    ...overrides
  };
}

function makeLane(overrides: Partial<Lane> = {}): Lane {
  return {
    id: 'lane-1',
    title: 'Todo',
    filterSet: { conjunction: 'and', rules: [] },
    sortConfig: { mode: 'auto' },
    order: 0,
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
    ...overrides
  };
}

function makeTask(overrides: Partial<Task> = {}): Task {
  return {
    id: 'task-1',
    title: 'Do something',
    description: '',
    status: 'todo',
    priority: 'none',
    tags: [],
    dueDate: null,
    sourcePageId: null,
    sourceNodeId: null,
    link: null,
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
    order: 0,
    ...overrides
  };
}

function createMockRepo(overrides: Partial<FolderBoardRepo> = {}): FolderBoardRepo {
  return {
    getFolder: vi.fn().mockResolvedValue({ folder: makeFolder(), canEdit: true }),
    getLanes: vi.fn().mockResolvedValue([]),
    getTasks: vi.fn().mockResolvedValue([]),
    createLane: vi.fn().mockImplementation(async (_fid: string, lane: Omit<Lane, 'id' | 'createdAt' | 'updatedAt'>) => ({
      id: 'new-lane',
      createdAt: '2026-01-01T00:00:00Z',
      updatedAt: '2026-01-01T00:00:00Z',
      ...lane
    })),
    updateLane: vi.fn().mockImplementation(async (_fid: string, laneId: string, patch: Partial<Lane>) => ({
      ...makeLane({ id: laneId }),
      ...patch
    })),
    deleteLane: vi.fn().mockResolvedValue(undefined),
    ...overrides
  } as FolderBoardRepo;
}

describe('folderBoardStore', () => {
  let repo: ReturnType<typeof createMockRepo>;
  let store: ReturnType<typeof createFolderBoardStore>;

  beforeEach(() => {
    repo = createMockRepo();
    store = createFolderBoardStore(repo);
    vi.clearAllMocks();
  });

  // ─── load ────────────────────────────────────────────────────────────────

  describe('load', () => {
    it('fetches folder, lanes, and tasks in parallel', async () => {
      const lane = makeLane({ id: 'l1' });
      const task = makeTask({ id: 't1' });
      vi.mocked(repo.getLanes).mockResolvedValueOnce([lane]);
      vi.mocked(repo.getTasks).mockResolvedValueOnce([task]);

      await store.load('folder-1');

      expect(repo.getFolder).toHaveBeenCalledWith('folder-1');
      expect(repo.getLanes).toHaveBeenCalledWith('folder-1');
      expect(repo.getTasks).toHaveBeenCalledWith('folder-1');
      expect(store.lanes).toEqual([lane]);
      expect(store.tasks).toEqual([task]);
      expect(store.loaded).toBe(true);
    });

    it('sets loaded=false initially', () => {
      expect(store.loaded).toBe(false);
    });

    // ── Regression: empty results must not cause a fetch cycle ────────────
    //
    // When the folder has no lanes or tasks the API returns []. Before the fix,
    // the guard was `lanes.length > 0` which is always false for an empty board,
    // causing every subsequent load() call (re-triggered by Svelte $effect) to
    // re-fetch, producing thousands of API calls per second.

    it('does NOT re-fetch when called again after loading with empty lanes and tasks', async () => {
      // First load: board exists but has no lanes or tasks yet
      vi.mocked(repo.getLanes).mockResolvedValue([]);
      vi.mocked(repo.getTasks).mockResolvedValue([]);

      await store.load('folder-1');
      expect(store.loaded).toBe(true);
      expect(store.lanes).toHaveLength(0);
      expect(store.tasks).toHaveLength(0);

      // Simulate the $effect re-triggering (e.g., due to state propagation)
      vi.clearAllMocks();
      await store.load('folder-1');

      // No additional network calls should have been made
      expect(repo.getFolder).not.toHaveBeenCalled();
      expect(repo.getLanes).not.toHaveBeenCalled();
      expect(repo.getTasks).not.toHaveBeenCalled();
    });

    it('does NOT re-fetch when called again after loading with non-empty results', async () => {
      vi.mocked(repo.getLanes).mockResolvedValue([makeLane()]);
      vi.mocked(repo.getTasks).mockResolvedValue([makeTask()]);

      await store.load('folder-1');
      vi.clearAllMocks();
      await store.load('folder-1');

      expect(repo.getFolder).not.toHaveBeenCalled();
      expect(repo.getLanes).not.toHaveBeenCalled();
      expect(repo.getTasks).not.toHaveBeenCalled();
    });

    it('deduplicates concurrent in-flight calls for the same folder', async () => {
      // Don't await individually — fire both before either resolves
      await Promise.all([store.load('folder-1'), store.load('folder-1')]);

      expect(repo.getFolder).toHaveBeenCalledTimes(1);
      expect(repo.getLanes).toHaveBeenCalledTimes(1);
      expect(repo.getTasks).toHaveBeenCalledTimes(1);
    });

    it('re-fetches when the folder id changes', async () => {
      await store.load('folder-1');
      vi.clearAllMocks();
      vi.mocked(repo.getFolder).mockResolvedValueOnce({ folder: makeFolder({ id: 'folder-2' }), canEdit: false });
      await store.load('folder-2');

      expect(repo.getFolder).toHaveBeenCalledWith('folder-2');
      expect(repo.getLanes).toHaveBeenCalledWith('folder-2');
      expect(repo.getTasks).toHaveBeenCalledWith('folder-2');
    });

    it('sets error state when the repo throws', async () => {
      vi.mocked(repo.getFolder).mockRejectedValueOnce(new Error('Network error'));
      await store.load('folder-1');
      expect(store.error).toBe('Network error');
    });
  });

  // ─── reset ───────────────────────────────────────────────────────────────

  describe('reset', () => {
    it('clears all state including loaded flag', async () => {
      await store.load('folder-1');
      store.reset();
      expect(store.loaded).toBe(false);
      expect(store.folder).toBeNull();
      expect(store.lanes).toHaveLength(0);
      expect(store.tasks).toHaveLength(0);
    });

    it('allows a fresh load after reset', async () => {
      vi.mocked(repo.getLanes).mockResolvedValue([]);
      vi.mocked(repo.getTasks).mockResolvedValue([]);
      await store.load('folder-1');

      store.reset();
      vi.clearAllMocks();
      vi.mocked(repo.getLanes).mockResolvedValue([makeLane()]);
      vi.mocked(repo.getTasks).mockResolvedValue([makeTask()]);
      await store.load('folder-1');

      expect(repo.getFolder).toHaveBeenCalledTimes(1);
      expect(store.lanes).toHaveLength(1);
    });
  });

  // ─── createLane ──────────────────────────────────────────────────────────

  describe('createLane', () => {
    it('adds the lane optimistically and replaces with server response', async () => {
      await store.load('folder-1');
      const lane = await store.createLane('Sprint 1');
      expect(lane).not.toBeNull();
      expect(store.lanes.find((l) => l.title === 'Sprint 1')).toBeDefined();
      expect(repo.createLane).toHaveBeenCalledOnce();
    });

    it('rolls back optimistic lane if the repo throws', async () => {
      await store.load('folder-1');
      vi.mocked(repo.createLane).mockRejectedValueOnce(new Error('fail'));
      await expect(store.createLane('Broken')).rejects.toThrow('fail');
      expect(store.lanes.find((l) => l.title === 'Broken')).toBeUndefined();
    });
  });

  // ─── updateLane ──────────────────────────────────────────────────────────

  describe('updateLane', () => {
    it('applies optimistic update and confirms with server response', async () => {
      vi.mocked(repo.getLanes).mockResolvedValueOnce([makeLane({ id: 'l1', title: 'Old' })]);
      await store.load('folder-1');

      await store.updateLane('l1', { title: 'New' });

      expect(store.lanes.find((l) => l.id === 'l1')?.title).toBe('New');
    });

    it('rolls back on repo error', async () => {
      vi.mocked(repo.getLanes).mockResolvedValueOnce([makeLane({ id: 'l1', title: 'Original' })]);
      await store.load('folder-1');
      vi.mocked(repo.updateLane).mockRejectedValueOnce(new Error('fail'));

      await expect(store.updateLane('l1', { title: 'Changed' })).rejects.toThrow('fail');
      expect(store.lanes.find((l) => l.id === 'l1')?.title).toBe('Original');
    });
  });

  // ─── deleteLane ──────────────────────────────────────────────────────────

  describe('deleteLane', () => {
    it('removes the lane immediately', async () => {
      vi.mocked(repo.getLanes).mockResolvedValueOnce([makeLane({ id: 'l1' })]);
      await store.load('folder-1');

      await store.deleteLane('l1');

      expect(store.lanes.find((l) => l.id === 'l1')).toBeUndefined();
      expect(repo.deleteLane).toHaveBeenCalledWith('folder-1', 'l1');
    });

    it('re-fetches lanes on repo error to restore state', async () => {
      vi.mocked(repo.getLanes).mockResolvedValue([makeLane({ id: 'l1' })]);
      await store.load('folder-1');
      vi.mocked(repo.deleteLane).mockRejectedValueOnce(new Error('fail'));

      await expect(store.deleteLane('l1')).rejects.toThrow('fail');
      expect(repo.getLanes).toHaveBeenCalledTimes(2); // initial load + recovery
    });
  });
});
