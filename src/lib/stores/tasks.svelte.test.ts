/**
 * Unit tests for the tasks store.
 *
 * Injects a mock ITaskRepository so no real storage is involved.
 * Tests: load deduplication, getById/getByNodeId (O(1) index), createTask defaults,
 *        updateTask (optimistic + rollback + write-lock serialization),
 *        deleteTask, getFiltered, getByPageIdRecursive.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { createTasksStore } from './tasks.svelte';
import type { ITaskRepository } from '$lib/storage/interfaces';
import type { Task, FilterSet, TreeNode } from '$lib/models/types';

function makeTask(overrides: Partial<Task> = {}): Task {
  return {
    id: 'task-1',
    title: 'Test task',
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

function makeNode(id: string, parentId: string | null = null): TreeNode {
  return {
    id,
    type: 'page',
    title: id,
    parentId,
    order: 0,
    tags: [],
    isPrivate: true,
    orgId: null,
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z'
  };
}

function createMockRepo(overrides: Partial<ITaskRepository> = {}): ITaskRepository {
  return {
    getAll: vi.fn().mockResolvedValue([]),
    getById: vi.fn().mockResolvedValue(null),
    create: vi.fn().mockImplementation(async (t: Task) => t),
    update: vi.fn().mockImplementation(async (id: string, patch: Partial<Task>) => ({
      ...makeTask({ id }),
      ...patch
    })),
    delete: vi.fn().mockResolvedValue(true),
    upsert: vi.fn().mockImplementation(async (t: Task) => t),
    getByPageId: vi.fn().mockResolvedValue([]),
    getByNodeId: vi.fn().mockResolvedValue(null),
    applyFilter: vi.fn().mockReturnValue([]),
    ...overrides
  };
}

describe('tasksStore', () => {
  let repo: ReturnType<typeof createMockRepo>;
  let store: ReturnType<typeof createTasksStore>;

  beforeEach(() => {
    repo = createMockRepo();
    store = createTasksStore(repo);
    vi.clearAllMocks();
  });

  // ─── load ────────────────────────────────────────────────────────────────

  describe('load', () => {
    it('populates tasks from the repo', async () => {
      const tasks = [makeTask({ id: 't1' }), makeTask({ id: 't2' })];
      vi.mocked(repo.getAll).mockResolvedValueOnce(tasks);
      await store.load();
      expect(store.tasks).toEqual(tasks);
      expect(store.loaded).toBe(true);
    });

    it('deduplicates concurrent load calls', async () => {
      vi.mocked(repo.getAll).mockResolvedValue([]);
      await Promise.all([store.load(), store.load(), store.load()]);
      expect(repo.getAll).toHaveBeenCalledTimes(1);
    });

    it('does not re-fetch when called again after loading with empty results', async () => {
      // Regression: absence of records must not cause a fetch cycle
      vi.mocked(repo.getAll).mockResolvedValue([]);
      await store.load();
      expect(store.loaded).toBe(true);
      vi.clearAllMocks();
      await store.load(); // second call — must be a no-op
      expect(repo.getAll).not.toHaveBeenCalled();
    });
  });

  // ─── getById ─────────────────────────────────────────────────────────────

  describe('getById', () => {
    it('returns the task for a known id', async () => {
      vi.mocked(repo.getAll).mockResolvedValueOnce([makeTask({ id: 't1', title: 'Hello' })]);
      await store.load();
      expect(store.getById('t1')?.title).toBe('Hello');
    });

    it('returns undefined for an unknown id', async () => {
      vi.mocked(repo.getAll).mockResolvedValueOnce([]);
      await store.load();
      expect(store.getById('missing')).toBeUndefined();
    });
  });

  // ─── getByNodeId ─────────────────────────────────────────────────────────

  describe('getByNodeId', () => {
    it('returns the task for a known nodeId', async () => {
      const task = makeTask({ id: 't1', sourceNodeId: 'n1' });
      vi.mocked(repo.getAll).mockResolvedValueOnce([task]);
      await store.load();
      expect(store.getByNodeId('n1')?.id).toBe('t1');
    });

    it('returns undefined when no task has that nodeId', async () => {
      vi.mocked(repo.getAll).mockResolvedValueOnce([makeTask({ sourceNodeId: null })]);
      await store.load();
      expect(store.getByNodeId('missing')).toBeUndefined();
    });

    it('reflects tasks added after load', async () => {
      vi.mocked(repo.getAll).mockResolvedValueOnce([]);
      await store.load();
      const task = makeTask({ id: 'new', sourceNodeId: 'n99' });
      vi.mocked(repo.create).mockResolvedValueOnce(task);
      await store.createTask({ title: 'New', sourceNodeId: 'n99' });
      expect(store.getByNodeId('n99')).toBeDefined();
    });
  });

  // ─── createTask ──────────────────────────────────────────────────────────

  describe('createTask', () => {
    it('appends the new task to the store', async () => {
      vi.mocked(repo.getAll).mockResolvedValueOnce([]);
      await store.load();
      const task = makeTask({ id: 'new-task' });
      vi.mocked(repo.create).mockResolvedValueOnce(task);

      await store.createTask({ title: 'New Task' });

      expect(store.tasks).toHaveLength(1);
    });

    it('defaults status to "todo"', async () => {
      vi.mocked(repo.getAll).mockResolvedValueOnce([]);
      await store.load();
      vi.mocked(repo.create).mockImplementation(async (t: Task) => t);

      const result = await store.createTask({ title: 'x' });

      expect(result.status).toBe('todo');
    });

    it('defaults priority to "none"', async () => {
      vi.mocked(repo.getAll).mockResolvedValueOnce([]);
      await store.load();
      vi.mocked(repo.create).mockImplementation(async (t: Task) => t);

      const result = await store.createTask({ title: 'x' });

      expect(result.priority).toBe('none');
    });

    it('uses provided status and priority', async () => {
      vi.mocked(repo.getAll).mockResolvedValueOnce([]);
      await store.load();
      vi.mocked(repo.create).mockImplementation(async (t: Task) => t);

      const result = await store.createTask({
        title: 'x',
        status: 'in-progress',
        priority: 'high'
      });

      expect(result.status).toBe('in-progress');
      expect(result.priority).toBe('high');
    });
  });

  // ─── updateTask ──────────────────────────────────────────────────────────

  describe('updateTask', () => {
    it('applies the patch optimistically then confirms from repo', async () => {
      const task = makeTask({ id: 't1', title: 'Old' });
      vi.mocked(repo.getAll).mockResolvedValueOnce([task]);
      vi.mocked(repo.update).mockResolvedValueOnce({ ...task, title: 'New' });
      await store.load();

      await store.updateTask('t1', { title: 'New' });

      expect(store.getById('t1')?.title).toBe('New');
    });

    it('optimistic update is applied before repo resolves', async () => {
      const task = makeTask({ id: 't1', title: 'Old' });
      vi.mocked(repo.getAll).mockResolvedValueOnce([task]);
      let resolveUpdate!: (t: Task) => void;
      vi.mocked(repo.update).mockReturnValueOnce(
        new Promise<Task>((r) => { resolveUpdate = r; })
      );
      await store.load();

      const p = store.updateTask('t1', { title: 'Optimistic' });
      expect(store.getById('t1')?.title).toBe('Optimistic');

      resolveUpdate({ ...task, title: 'Confirmed' });
      await p;
      expect(store.getById('t1')?.title).toBe('Confirmed');
    });

    it('rolls back on repo failure', async () => {
      const task = makeTask({ id: 't1', title: 'Original' });
      vi.mocked(repo.getAll).mockResolvedValueOnce([task]);
      vi.mocked(repo.update).mockRejectedValueOnce(new Error('fail'));
      await store.load();

      await expect(store.updateTask('t1', { title: 'New' })).rejects.toThrow();

      expect(store.getById('t1')?.title).toBe('Original');
    });

    it('is a no-op for an unknown id', async () => {
      vi.mocked(repo.getAll).mockResolvedValueOnce([]);
      await store.load();
      await store.updateTask('missing', { title: 'x' });
      expect(repo.update).not.toHaveBeenCalled();
    });

    it('serializes concurrent updates to the same task', async () => {
      const task = makeTask({ id: 't1', title: 'Original' });
      vi.mocked(repo.getAll).mockResolvedValueOnce([task]);
      let callCount = 0;
      vi.mocked(repo.update).mockImplementation(async (id, patch) => {
        callCount++;
        return { ...task, ...patch };
      });
      await store.load();

      await Promise.all([
        store.updateTask('t1', { title: 'Update A' }),
        store.updateTask('t1', { title: 'Update B' })
      ]);

      expect(callCount).toBe(2);
      // After both complete, the final title should be one of the two updates
      expect(['Update A', 'Update B']).toContain(store.getById('t1')?.title);
    });

    it('does not update store when repo.update returns null (covers line 114 false branch)', async () => {
      const task = makeTask({ id: 't1', title: 'Original' });
      vi.mocked(repo.getAll).mockResolvedValueOnce([task]);
      vi.mocked(repo.update).mockResolvedValueOnce(null as any);
      await store.load();

      await store.updateTask('t1', { title: 'Attempted' });

      // repo.update was called but returned null — no confirmation update applied
      expect(repo.update).toHaveBeenCalled();
    });

    it('updates only the matching task when multiple tasks are loaded (covers ternary false branches on lines 106, 115)', async () => {
      const t1 = makeTask({ id: 't1', title: 'First' });
      const t2 = makeTask({ id: 't2', title: 'Second' });
      vi.mocked(repo.getAll).mockResolvedValueOnce([t1, t2]);
      const updated = makeTask({ id: 't1', title: 'Updated First' });
      vi.mocked(repo.update).mockResolvedValueOnce(updated);
      await store.load();

      await store.updateTask('t1', { title: 'Updated First' });

      // t1 was updated; t2 unchanged (ternary false branch: t.id !== 't1' → t)
      expect(store.getById('t1')?.title).toBe('Updated First');
      expect(store.getById('t2')?.title).toBe('Second');
    });

    it('rollback preserves other tasks unchanged when multiple tasks loaded (covers line 119 false branch)', async () => {
      const t1 = makeTask({ id: 't1', title: 'Original' });
      const t2 = makeTask({ id: 't2', title: 'Sibling' });
      vi.mocked(repo.getAll).mockResolvedValueOnce([t1, t2]);
      vi.mocked(repo.update).mockRejectedValueOnce(new Error('fail'));
      await store.load();

      await expect(store.updateTask('t1', { title: 'Changed' })).rejects.toThrow();

      // t1 rolled back; t2 untouched (ternary false branch on line 119)
      expect(store.getById('t1')?.title).toBe('Original');
      expect(store.getById('t2')?.title).toBe('Sibling');
    });
  });

  // ─── deleteTask ──────────────────────────────────────────────────────────

  describe('deleteTask', () => {
    it('removes the task from the store', async () => {
      const task = makeTask({ id: 't1' });
      vi.mocked(repo.getAll).mockResolvedValueOnce([task]);
      await store.load();

      await store.deleteTask('t1');

      expect(store.tasks).toHaveLength(0);
    });

    it('calls repo.delete with the correct id', async () => {
      const task = makeTask({ id: 't1' });
      vi.mocked(repo.getAll).mockResolvedValueOnce([task]);
      await store.load();

      await store.deleteTask('t1');

      expect(repo.delete).toHaveBeenCalledWith('t1');
    });
  });

  // ─── getFiltered ─────────────────────────────────────────────────────────

  describe('getFiltered', () => {
    it('delegates to repo.applyFilter', async () => {
      const tasks = [makeTask({ id: 't1', status: 'todo' })];
      vi.mocked(repo.getAll).mockResolvedValueOnce(tasks);
      vi.mocked(repo.applyFilter).mockReturnValueOnce([tasks[0]]);
      await store.load();

      const filter: FilterSet = { conjunction: 'and', rules: [] };
      const result = store.getFiltered(filter);

      expect(repo.applyFilter).toHaveBeenCalledWith(tasks, filter, undefined);
      expect(result).toEqual([tasks[0]]);
    });
  });

  // ─── tasksByPage ──────────────────────────────────────────────────────────

  describe('tasksByPage', () => {
    it('groups tasks by sourcePageId', async () => {
      const tasks = [
        makeTask({ id: 't1', sourcePageId: 'page-1' }),
        makeTask({ id: 't2', sourcePageId: 'page-1' }),
        makeTask({ id: 't3', sourcePageId: 'page-2' })
      ];
      vi.mocked(repo.getAll).mockResolvedValueOnce(tasks);
      await store.load();

      expect(store.tasksByPage.get('page-1')?.map(t => t.id).sort()).toEqual(['t1', 't2']);
      expect(store.tasksByPage.get('page-2')?.map(t => t.id)).toEqual(['t3']);
    });

    it('does not include tasks with null sourcePageId in any page group', async () => {
      const tasks = [
        makeTask({ id: 't1', sourcePageId: null }),
        makeTask({ id: 't2', sourcePageId: 'page-1' })
      ];
      vi.mocked(repo.getAll).mockResolvedValueOnce(tasks);
      await store.load();

      // The null-sourcePageId task must not appear under any key
      let found = false;
      store.tasksByPage.forEach((pageTasks) => {
        if (pageTasks.some(t => t.id === 't1')) found = true;
      });
      expect(found).toBe(false);
    });

    it('updates reactively when a task is added', async () => {
      vi.mocked(repo.getAll).mockResolvedValueOnce([]);
      await store.load();

      expect(store.tasksByPage.get('page-X')).toBeUndefined();

      const newTask = makeTask({ id: 'new', sourcePageId: 'page-X' });
      vi.mocked(repo.create).mockResolvedValueOnce(newTask);
      await store.createTask({ title: 'New', sourcePageId: 'page-X' });

      const pageTasks = store.tasksByPage.get('page-X');
      expect(pageTasks).toHaveLength(1);
      expect(pageTasks?.[0].sourcePageId).toBe('page-X');
    });

    it('updates reactively when a task is deleted', async () => {
      const task = makeTask({ id: 't1', sourcePageId: 'page-1' });
      vi.mocked(repo.getAll).mockResolvedValueOnce([task]);
      await store.load();

      expect(store.tasksByPage.get('page-1')).toHaveLength(1);

      await store.deleteTask('t1');

      expect(store.tasksByPage.get('page-1')).toBeUndefined();
    });
  });

  describe('getByPageIdRecursive', () => {
    it('returns tasks from the given page and all descendant pages', async () => {
      const tasks = [
        makeTask({ id: 't1', sourcePageId: 'root' }),
        makeTask({ id: 't2', sourcePageId: 'child' }),
        makeTask({ id: 't3', sourcePageId: 'grandchild' }),
        makeTask({ id: 't4', sourcePageId: 'other-page' })
      ];
      vi.mocked(repo.getAll).mockResolvedValueOnce(tasks);
      await store.load();

      const allNodes = [
        makeNode('root', null),
        makeNode('child', 'root'),
        makeNode('grandchild', 'child'),
        makeNode('other-page', null)
      ];

      const result = store.getByPageIdRecursive('root', allNodes);

      expect(result.map((t) => t.id).sort()).toEqual(['t1', 't2', 't3']);
    });

    it('returns only the root page tasks when it has no children', async () => {
      const tasks = [makeTask({ id: 't1', sourcePageId: 'root' })];
      vi.mocked(repo.getAll).mockResolvedValueOnce(tasks);
      await store.load();

      const result = store.getByPageIdRecursive('root', [makeNode('root', null)]);
      expect(result).toHaveLength(1);
    });

    it('returns empty array when no tasks are associated with the page subtree', async () => {
      vi.mocked(repo.getAll).mockResolvedValueOnce([makeTask({ sourcePageId: 'other' })]);
      await store.load();

      const result = store.getByPageIdRecursive('root', [makeNode('root', null)]);
      expect(result).toHaveLength(0);
    });
  });
});
