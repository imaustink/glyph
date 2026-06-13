/**
 * Unit tests for ApiTaskRepository.
 *
 * Uses a mocked api client so no real HTTP calls are made.
 * Covers CRUD, getByPageId, getByNodeId, and the applyFilter logic
 * (delegated to filterUtils, which includes the DATE_FIELDS guard).
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { ApiTaskRepository } from './ApiTaskRepository';
import type { Task, FilterSet } from '$lib/models/types';

vi.mock('$lib/storage/apiClient', () => ({
  API_BASE: 'http://localhost:8081',
  api: {
    get: vi.fn(),
    getOrNull: vi.fn(),
    post: vi.fn(),
    patch: vi.fn(),
    put: vi.fn(),
    del: vi.fn()
  },
  UnauthorizedError: class UnauthorizedError extends Error {
    constructor(method: string, path: string) { super(`${method} ${path}`); this.name = 'UnauthorizedError'; }
  }
}));

import { api, UnauthorizedError } from '$lib/storage/apiClient';

const mockGet = vi.mocked(api.get);
const mockGetOrNull = vi.mocked(api.getOrNull);
const mockPost = vi.mocked(api.post);
const mockPatch = vi.mocked(api.patch);
const mockPut = vi.mocked(api.put);
const mockDel = vi.mocked(api.del);

function makeTask(overrides: Partial<Task> = {}): Task {
  return {
    id: 'task-1',
    title: 'Test Task',
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

describe('ApiTaskRepository', () => {
  let repo: ApiTaskRepository;

  beforeEach(() => {
    repo = new ApiTaskRepository();
    vi.clearAllMocks();
  });

  describe('getAll', () => {
    it('returns tasks from GET /api/v1/tasks', async () => {
      const tasks = [makeTask({ id: 't1' })];
      mockGet.mockResolvedValueOnce(tasks);
      expect(await repo.getAll()).toEqual(tasks);
    });

    it('returns empty array when response is null', async () => {
      mockGet.mockResolvedValueOnce(null);
      expect(await repo.getAll()).toEqual([]);
    });
  });

  describe('getById', () => {
    it('returns the task on success', async () => {
      const task = makeTask({ id: 't1' });
      mockGetOrNull.mockResolvedValueOnce(task);
      expect(await repo.getById('t1')).toEqual(task);
    });

    it('re-throws UnauthorizedError', async () => {
      mockGetOrNull.mockRejectedValueOnce(new UnauthorizedError('GET', '/api/v1/tasks/t1'));
      await expect(repo.getById('t1')).rejects.toBeInstanceOf(UnauthorizedError);
    });

    it('returns null when not found', async () => {
      mockGetOrNull.mockResolvedValueOnce(null);
      expect(await repo.getById('t1')).toBeNull();
    });
  });

  describe('create', () => {
    it('calls POST /api/v1/tasks', async () => {
      const task = makeTask({ id: 't1' });
      mockPost.mockResolvedValueOnce(task);
      const result = await repo.create(task);
      expect(result).toEqual(task);
      expect(mockPost).toHaveBeenCalledWith('/api/v1/tasks', task);
    });
  });

  describe('update', () => {
    it('calls PATCH /api/v1/tasks/:id', async () => {
      const task = makeTask({ id: 't1', title: 'Updated' });
      mockPatch.mockResolvedValueOnce(task);
      expect(await repo.update('t1', { title: 'Updated' })).toEqual(task);
    });

    it('propagates errors', async () => {
      mockPatch.mockRejectedValueOnce(new Error('fail'));
      await expect(repo.update('t1', {})).rejects.toThrow('fail');
    });
  });

  describe('delete', () => {
    it('calls DEL /api/v1/tasks/:id and returns true', async () => {
      mockDel.mockResolvedValueOnce(undefined);
      expect(await repo.delete('t1')).toBe(true);
    });

    it('propagates errors', async () => {
      mockDel.mockRejectedValueOnce(new Error('fail'));
      await expect(repo.delete('t1')).rejects.toThrow('fail');
    });
  });

  describe('getByPageId', () => {
    it('calls GET /api/v1/tasks?sourcePageId=... with URL encoding', async () => {
      const tasks = [makeTask({ sourcePageId: 'page with spaces' })];
      mockGet.mockResolvedValueOnce(tasks);
      await repo.getByPageId('page with spaces');
      expect(mockGet).toHaveBeenCalledWith(
        `/api/v1/tasks?sourcePageId=${encodeURIComponent('page with spaces')}`
      );
    });

    it('returns empty array when response is null', async () => {
      mockGet.mockResolvedValueOnce(null);
      expect(await repo.getByPageId('p1')).toEqual([]);
    });
  });

  describe('getByNodeId', () => {
    it('calls GET /api/v1/tasks?sourceNodeId=... and returns first match', async () => {
      const tasks = [makeTask({ id: 't1', sourceNodeId: 'n1' })];
      mockGet.mockResolvedValueOnce(tasks);
      const result = await repo.getByNodeId('n1');
      expect(mockGet).toHaveBeenCalledWith(
        `/api/v1/tasks?sourceNodeId=${encodeURIComponent('n1')}`
      );
      expect(result?.id).toBe('t1');
    });

    it('returns null when response is empty', async () => {
      mockGet.mockResolvedValueOnce([]);
      expect(await repo.getByNodeId('missing')).toBeNull();
    });

    it('returns null when response is null', async () => {
      mockGet.mockResolvedValueOnce(null);
      expect(await repo.getByNodeId('n1')).toBeNull();
    });
  });

  describe('upsert', () => {
    it('calls PUT /api/v1/tasks/:id with the full task', async () => {
      const task = makeTask({ id: 't1' });
      mockPut.mockResolvedValueOnce(task);
      const result = await repo.upsert(task);
      expect(mockPut).toHaveBeenCalledWith('/api/v1/tasks/t1', task);
      expect(result).toEqual(task);
    });

    it('propagates errors from PUT', async () => {
      const task = makeTask({ id: 't1' });
      mockPut.mockRejectedValueOnce(new Error('fail'));
      await expect(repo.upsert(task)).rejects.toThrow('fail');
    });
  });

  describe('applyFilter', () => {
    const allTasks = [
      makeTask({ id: 't1', status: 'todo', priority: 'high', tags: ['bug'], dueDate: '2026-03-01' }),
      makeTask({ id: 't2', status: 'done', priority: 'low', tags: [], dueDate: '2026-06-01' }),
      makeTask({ id: 't3', status: 'todo', priority: 'none', tags: ['bug', 'feature'], dueDate: null })
    ];

    it('returns tasks as-is when rules are empty (no network call)', () => {
      const filter: FilterSet = { conjunction: 'and', rules: [] };
      const result = repo.applyFilter(allTasks, filter);
      expect(result).toBe(allTasks);
      expect(mockPost).not.toHaveBeenCalled();
    });

    it('calls POST /api/v1/tasks/filter with the filter set when rules are non-empty', async () => {
      const filtered = [allTasks[1]];
      mockPost.mockResolvedValueOnce(filtered);
      const filter: FilterSet = {
        conjunction: 'and',
        rules: [{ id: 'r1', field: 'status', operator: 'eq', value: 'done' }]
      };
      const result = await repo.applyFilter(allTasks, filter);
      expect(mockPost).toHaveBeenCalledWith('/api/v1/tasks/filter', filter);
      expect(result).toEqual(filtered);
    });

    it('delegates OR conjunction to the server', async () => {
      const filtered = [allTasks[0], allTasks[1]];
      mockPost.mockResolvedValueOnce(filtered);
      const filter: FilterSet = {
        conjunction: 'or',
        rules: [
          { id: 'r1', field: 'status', operator: 'eq', value: 'done' },
          { id: 'r2', field: 'priority', operator: 'eq', value: 'high' }
        ]
      };
      const result = await repo.applyFilter(allTasks, filter);
      expect(mockPost).toHaveBeenCalledWith('/api/v1/tasks/filter', filter);
      expect(result).toEqual(filtered);
    });
  });
});
