/**
 * tasksStore behaviour that matters once several clients edit the same page:
 * creating the task for a bullet another client already linked, and mirroring
 * the server's task reconciliation locally without writing to storage.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { createTasksStore } from './tasks.svelte';
import type { ITaskRepository } from '$lib/storage/interfaces';
import type { Task } from '$lib/models/types';

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

function createMockRepo(overrides: Partial<ITaskRepository> = {}): ITaskRepository {
  return {
    getAll: vi.fn().mockResolvedValue([]),
    getById: vi.fn().mockResolvedValue(null),
    create: vi.fn().mockImplementation(async (t: Task) => t),
    update: vi.fn(),
    delete: vi.fn().mockResolvedValue(true),
    upsert: vi.fn(),
    getByPageId: vi.fn().mockResolvedValue([]),
    getByNodeId: vi.fn().mockResolvedValue(null),
    applyFilter: vi.fn().mockReturnValue([]),
    ...overrides
  };
}

describe('tasksStore — bullet-linked tasks', () => {
  let repo: ITaskRepository;
  let store: ReturnType<typeof createTasksStore>;

  beforeEach(async () => {
    repo = createMockRepo();
    store = createTasksStore(repo);
    await store.load();
  });

  it('adopts the existing task the server returns instead of its local draft', async () => {
    const existing = makeTask({ id: 'already-linked', sourcePageId: 'p1', sourceNodeId: 'n1', status: 'in-progress' });
    vi.mocked(repo.create).mockResolvedValueOnce(existing);

    const result = await store.createTask({ title: 'mine', sourcePageId: 'p1', sourceNodeId: 'n1' });

    expect(result.id).toBe('already-linked');
    expect(store.tasks.map((t) => t.id)).toEqual(['already-linked']);
    expect(store.getByNodeId('n1')?.status).toBe('in-progress');
  });

  it('does not duplicate a task that is already in local state', async () => {
    const existing = makeTask({ id: 'shared', sourcePageId: 'p1', sourceNodeId: 'n1' });
    vi.mocked(repo.create).mockResolvedValue(existing);
    await store.createTask({ title: 'first', sourcePageId: 'p1', sourceNodeId: 'n1' });
    await store.createTask({ title: 'second', sourcePageId: 'p1', sourceNodeId: 'n1' });
    expect(store.tasks).toHaveLength(1);
  });

  it('falls back to the local draft when the repository returns nothing', async () => {
    vi.mocked(repo.create).mockResolvedValueOnce(undefined as unknown as Task);
    const result = await store.createTask({ title: 'local' });
    expect(store.getById(result.id)?.title).toBe('local');
  });

  it('forgetLocal drops tasks without touching storage', async () => {
    vi.mocked(repo.create).mockImplementation(async (t: Task) => t);
    const a = await store.createTask({ title: 'a' });
    const b = await store.createTask({ title: 'b' });

    store.forgetLocal([a.id]);

    expect(store.tasks.map((t) => t.id)).toEqual([b.id]);
    expect(repo.delete).not.toHaveBeenCalled();
  });

  it('refreshTask re-reads a restored task into local state', async () => {
    const restored = makeTask({ id: 'restored', title: 'back again' });
    vi.mocked(repo.getById).mockResolvedValueOnce(restored);
    await expect(store.refreshTask('restored')).resolves.toBe(true);
    expect(store.getById('restored')?.title).toBe('back again');
  });

  it('refreshTask drops a task that no longer exists', async () => {
    const gone = await store.createTask({ title: 'gone' });
    vi.mocked(repo.getById).mockResolvedValueOnce(null);
    await expect(store.refreshTask(gone.id)).resolves.toBe(false);
    expect(store.getById(gone.id)).toBeUndefined();
  });

  it('refreshForPage replaces only that page\'s tasks', async () => {
    const other = await store.createTask({ title: 'other', sourcePageId: 'p2', sourceNodeId: 'x' });
    await store.createTask({ title: 'stale', sourcePageId: 'p1', sourceNodeId: 'y' });
    const fresh = makeTask({ id: 'fresh', sourcePageId: 'p1', sourceNodeId: 'y', status: 'done' });
    vi.mocked(repo.getByPageId).mockResolvedValueOnce([fresh]);

    await store.refreshForPage('p1');

    expect(store.tasks.map((t) => t.id).sort()).toEqual([other.id, 'fresh'].sort());
    expect(store.getById('fresh')?.status).toBe('done');
  });
});
