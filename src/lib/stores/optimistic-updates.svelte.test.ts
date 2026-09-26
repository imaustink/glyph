/**
 * DI-05 (client half): optimistic updates in the pages, lanes and tasks stores.
 *
 * - A server response that arrives after a newer optimistic patch on the same
 *   record must not overwrite that patch (it describes an older state).
 * - A failed write must not restore the snapshot taken when it started: that
 *   can undo another change that has since succeeded. The store refetches the
 *   record instead.
 */

import { describe, it, expect, vi } from 'vitest';
import { createTasksStore } from './tasks.svelte';
import { createPagesStore } from './pages.svelte';
import { createLanesStore } from './lanes.svelte';
import type { ITaskRepository, IPageRepository, ILaneRepository } from '$lib/storage/interfaces';
import type { Task, TreeNode, Lane } from '$lib/models/types';

function deferred<T>() {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
}

const flush = () => new Promise((r) => setTimeout(r, 0));

// ─── Tasks ────────────────────────────────────────────────────────────────────

function makeTask(overrides: Partial<Task> = {}): Task {
  return {
    id: 't1',
    title: 'Original',
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

function taskRepo(overrides: Partial<ITaskRepository> = {}): ITaskRepository {
  return {
    getAll: vi.fn().mockResolvedValue([makeTask()]),
    getById: vi.fn().mockResolvedValue(null),
    create: vi.fn(),
    update: vi.fn(),
    delete: vi.fn().mockResolvedValue(true),
    upsert: vi.fn(),
    getByPageId: vi.fn().mockResolvedValue([]),
    getByNodeId: vi.fn().mockResolvedValue(null),
    applyFilter: vi.fn().mockReturnValue([]),
    ...overrides
  };
}

describe('tasksStore.updateTask ordering [DI-05]', () => {
  it('a response for an older write does not revert a newer optimistic patch', async () => {
    const first = deferred<Task>();
    const second = deferred<Task>();
    const repo = taskRepo();
    vi.mocked(repo.update)
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise);
    const store = createTasksStore(repo);
    await store.load();

    const p1 = store.updateTask('t1', { title: 'Renamed' });
    const p2 = store.updateTask('t1', { description: 'typed while saving' });

    // The first PATCH answers with the row as it was before the second one.
    first.resolve(makeTask({ title: 'Renamed', description: '' }));
    await p1;
    expect(store.getById('t1')?.description).toBe('typed while saving');

    second.resolve(makeTask({ title: 'Renamed', description: 'typed while saving' }));
    await p2;
    expect(store.getById('t1')).toMatchObject({ title: 'Renamed', description: 'typed while saving' });
  });

  it('on failure, refetches the record instead of restoring the stale snapshot', async () => {
    const repo = taskRepo({
      update: vi.fn().mockRejectedValue(new Error('network')),
      // Meanwhile someone else marked it done.
      getById: vi.fn().mockResolvedValue(makeTask({ status: 'done' }))
    });
    const store = createTasksStore(repo);
    await store.load();

    await expect(store.updateTask('t1', { title: 'Nope' })).rejects.toThrow('network');

    expect(repo.getById).toHaveBeenCalledWith('t1');
    expect(store.getById('t1')).toMatchObject({ title: 'Original', status: 'done' });
  });

  it('a failed older write does not undo a newer optimistic patch', async () => {
    const first = deferred<Task>();
    const second = deferred<Task>();
    const repo = taskRepo();
    vi.mocked(repo.update)
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise);
    const store = createTasksStore(repo);
    await store.load();

    const p1 = store.updateTask('t1', { title: 'Renamed' });
    const p2 = store.updateTask('t1', { description: 'newer' });

    first.reject(new Error('network'));
    await expect(p1).rejects.toThrow('network');
    expect(store.getById('t1')?.description).toBe('newer');

    second.resolve(makeTask({ description: 'newer' }));
    await p2;
    expect(store.getById('t1')).toMatchObject({ title: 'Original', description: 'newer' });
  });
});

// ─── Pages ────────────────────────────────────────────────────────────────────

function makeNode(overrides: Partial<TreeNode> = {}): TreeNode {
  return {
    id: 'p1',
    type: 'page',
    title: 'Original',
    parentId: null,
    order: 0,
    tags: [],
    isPrivate: true,
    orgId: null,
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
    ...overrides
  };
}

function pageRepo(overrides: Partial<IPageRepository> = {}): IPageRepository {
  return {
    getAll: vi.fn().mockResolvedValue([makeNode()]),
    getById: vi.fn().mockResolvedValue(null),
    create: vi.fn(),
    update: vi.fn(),
    delete: vi.fn().mockResolvedValue(true),
    upsert: vi.fn(),
    getContent: vi.fn().mockResolvedValue(null),
    saveContent: vi.fn().mockResolvedValue(undefined),
    deleteContent: vi.fn().mockResolvedValue(undefined),
    deleteWithContent: vi.fn().mockResolvedValue(true),
    deleteSubtree: vi.fn().mockResolvedValue(undefined),
    getTree: vi.fn().mockReturnValue([]),
    getChildren: vi.fn().mockReturnValue([]),
    ...overrides
  };
}

describe('pagesStore.updateNode ordering [DI-05]', () => {
  it('an out-of-order response does not overwrite a newer optimistic patch', async () => {
    const first = deferred<TreeNode>();
    const second = deferred<TreeNode>();
    const repo = pageRepo();
    vi.mocked(repo.update)
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise);
    const store = createPagesStore(repo);
    await store.load();

    const p1 = store.updateNode('p1', { title: 'Renamed' });
    const p2 = store.updateNode('p1', { tags: ['x'] });

    // The newer write answers first, then the older one's stale row lands.
    second.resolve(makeNode({ title: 'Renamed', tags: ['x'] }));
    await p2;
    first.resolve(makeNode({ title: 'Renamed', tags: [] }));
    await p1;

    expect(store.getById('p1')).toMatchObject({ title: 'Renamed', tags: ['x'] });
  });

  it('on failure, refetches the record instead of restoring the stale snapshot', async () => {
    const repo = pageRepo({
      update: vi.fn().mockRejectedValue(new Error('network')),
      getById: vi.fn().mockResolvedValue(makeNode({ priority: 'high' }))
    });
    const store = createPagesStore(repo);
    await store.load();

    await expect(store.updateNode('p1', { title: 'Nope' })).rejects.toThrow('network');

    expect(store.getById('p1')).toMatchObject({ title: 'Original', priority: 'high' });
  });

  it('a failed older write does not undo a newer optimistic patch', async () => {
    const first = deferred<TreeNode>();
    const second = deferred<TreeNode>();
    const repo = pageRepo();
    vi.mocked(repo.update)
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise);
    const store = createPagesStore(repo);
    await store.load();

    const p1 = store.updateNode('p1', { title: 'Renamed' });
    const p2 = store.updateNode('p1', { tags: ['x'] });

    first.reject(new Error('network'));
    await expect(p1).rejects.toThrow('network');
    await flush();
    expect(store.getById('p1')?.tags).toEqual(['x']);

    second.resolve(makeNode({ tags: ['x'] }));
    await p2;
    expect(store.getById('p1')).toMatchObject({ title: 'Original', tags: ['x'] });
  });
});

// ─── Lanes ────────────────────────────────────────────────────────────────────

function makeLane(overrides: Partial<Lane> = {}): Lane {
  return {
    id: 'l1',
    title: 'Original',
    filterSet: { conjunction: 'and', rules: [] },
    sortConfig: { mode: 'auto' },
    order: 0,
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
    ...overrides
  };
}

function laneRepo(overrides: Partial<ILaneRepository> = {}): ILaneRepository {
  return {
    getAll: vi.fn().mockResolvedValue([makeLane()]),
    getById: vi.fn().mockResolvedValue(null),
    create: vi.fn(),
    update: vi.fn(),
    delete: vi.fn().mockResolvedValue(true),
    upsert: vi.fn(),
    getOrdered: vi.fn().mockResolvedValue([makeLane()]),
    reorderAll: vi.fn().mockResolvedValue(undefined),
    ...overrides
  };
}

describe('lanesStore.updateLane ordering [DI-05]', () => {
  it('an out-of-order response does not overwrite a newer optimistic patch', async () => {
    const first = deferred<Lane>();
    const second = deferred<Lane>();
    const repo = laneRepo();
    vi.mocked(repo.update)
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise);
    const store = createLanesStore(repo);
    await store.load();

    const p1 = store.updateLane('l1', { title: 'Renamed' });
    const p2 = store.updateLane('l1', { sortConfig: { mode: 'manual', taskOrder: ['a', 'b'] } });

    second.resolve(makeLane({ title: 'Renamed', sortConfig: { mode: 'manual', taskOrder: ['a', 'b'] } }));
    await p2;
    first.resolve(makeLane({ title: 'Renamed' }));
    await p1;

    expect(store.lanes[0].sortConfig).toEqual({ mode: 'manual', taskOrder: ['a', 'b'] });
  });

  it('on failure, refetches the record instead of restoring the stale snapshot', async () => {
    const repo = laneRepo({
      update: vi.fn().mockRejectedValue(new Error('network')),
      getById: vi.fn().mockResolvedValue(makeLane({ order: 5 }))
    });
    const store = createLanesStore(repo);
    await store.load();

    await expect(store.updateLane('l1', { title: 'Nope' })).rejects.toThrow('network');

    expect(store.lanes[0]).toMatchObject({ title: 'Original', order: 5 });
  });

  it('a failed older write does not undo a newer optimistic patch', async () => {
    const first = deferred<Lane>();
    const second = deferred<Lane>();
    const repo = laneRepo();
    vi.mocked(repo.update)
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise);
    const store = createLanesStore(repo);
    await store.load();

    const p1 = store.updateLane('l1', { title: 'Renamed' });
    const p2 = store.updateLane('l1', { sortConfig: { mode: 'manual', taskOrder: ['a'] } });

    first.reject(new Error('network'));
    await expect(p1).rejects.toThrow('network');
    await flush();
    expect(store.lanes[0].sortConfig).toEqual({ mode: 'manual', taskOrder: ['a'] });

    second.resolve(makeLane({ sortConfig: { mode: 'manual', taskOrder: ['a'] } }));
    await p2;
    expect(store.lanes[0]).toMatchObject({ title: 'Original', sortConfig: { mode: 'manual', taskOrder: ['a'] } });
  });
});
