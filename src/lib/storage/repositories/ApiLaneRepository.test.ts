/**
 * Unit tests for ApiLaneRepository.
 *
 * Uses a mocked api client so no real HTTP calls are made.
 * Covers CRUD, getOrdered, reorderAll, and createBatch.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { ApiLaneRepository } from './ApiLaneRepository';
import type { Lane } from '$lib/models/types';

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

function makeLane(overrides: Partial<Lane> = {}): Lane {
  return {
    id: 'lane-1',
    title: 'All Tasks',
    filterSet: { conjunction: 'and', rules: [] },
    sortConfig: { mode: 'auto' },
    order: 0,
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
    ...overrides
  };
}

describe('ApiLaneRepository', () => {
  let repo: ApiLaneRepository;

  beforeEach(() => {
    repo = new ApiLaneRepository();
    vi.clearAllMocks();
  });

  describe('getAll', () => {
    it('returns lanes from GET /api/v1/lanes', async () => {
      const lanes = [makeLane({ id: 'l1' })];
      mockGet.mockResolvedValueOnce(lanes);
      expect(await repo.getAll()).toEqual(lanes);
    });

    it('returns empty array when response is null', async () => {
      mockGet.mockResolvedValueOnce(null);
      expect(await repo.getAll()).toEqual([]);
    });
  });

  describe('getById', () => {
    it('returns the lane on success', async () => {
      const lane = makeLane({ id: 'l1' });
      mockGetOrNull.mockResolvedValueOnce(lane);
      expect(await repo.getById('l1')).toEqual(lane);
    });

    it('re-throws UnauthorizedError', async () => {
      mockGetOrNull.mockRejectedValueOnce(new UnauthorizedError('GET', '/api/v1/lanes/l1'));
      await expect(repo.getById('l1')).rejects.toBeInstanceOf(UnauthorizedError);
    });

    it('returns null when not found', async () => {
      mockGetOrNull.mockResolvedValueOnce(null);
      expect(await repo.getById('l1')).toBeNull();
    });
  });

  describe('create', () => {
    it('calls POST /api/v1/lanes', async () => {
      const lane = makeLane({ id: 'l1' });
      mockPost.mockResolvedValueOnce(lane);
      expect(await repo.create(lane)).toEqual(lane);
      expect(mockPost).toHaveBeenCalledWith('/api/v1/lanes', lane);
    });
  });

  describe('createBatch', () => {
    it('calls POST /api/v1/lanes/batch', async () => {
      const lanes = [makeLane({ id: 'l1' }), makeLane({ id: 'l2', order: 1 })];
      mockPost.mockResolvedValueOnce(lanes);
      const result = await repo.createBatch(lanes);
      expect(result).toEqual(lanes);
      expect(mockPost).toHaveBeenCalledWith('/api/v1/lanes/batch', lanes);
    });
  });

  describe('update', () => {
    it('calls PATCH /api/v1/lanes/:id', async () => {
      const lane = makeLane({ id: 'l1', title: 'Updated' });
      mockPatch.mockResolvedValueOnce(lane);
      expect(await repo.update('l1', { title: 'Updated' })).toEqual(lane);
    });

    it('propagates errors', async () => {
      mockPatch.mockRejectedValueOnce(new Error('fail'));
      await expect(repo.update('l1', {})).rejects.toThrow('fail');
    });
  });

  describe('delete', () => {
    it('returns true on success', async () => {
      mockDel.mockResolvedValueOnce(undefined);
      expect(await repo.delete('l1')).toBe(true);
    });

    it('propagates errors', async () => {
      mockDel.mockRejectedValueOnce(new Error('fail'));
      await expect(repo.delete('l1')).rejects.toThrow('fail');
    });
  });

  describe('getOrdered', () => {
    it('returns lanes sorted by order ascending', async () => {
      const lanes = [
        makeLane({ id: 'l3', order: 2 }),
        makeLane({ id: 'l1', order: 0 }),
        makeLane({ id: 'l2', order: 1 })
      ];
      mockGet.mockResolvedValueOnce(lanes);
      const result = await repo.getOrdered();
      expect(result.map((l) => l.id)).toEqual(['l1', 'l2', 'l3']);
    });
  });

  describe('upsert', () => {
    it('calls PUT /api/v1/lanes/:id with the full lane', async () => {
      const lane = makeLane({ id: 'l1' });
      mockPut.mockResolvedValueOnce(lane);
      const result = await repo.upsert(lane);
      expect(mockPut).toHaveBeenCalledWith('/api/v1/lanes/l1', lane);
      expect(result).toEqual(lane);
    });

    it('propagates errors from PUT', async () => {
      const lane = makeLane({ id: 'l1' });
      mockPut.mockRejectedValueOnce(new Error('fail'));
      await expect(repo.upsert(lane)).rejects.toThrow('fail');
    });
  });

  describe('reorderAll', () => {
    it('calls PUT /api/v1/lanes/reorder with id+order pairs', async () => {
      mockPut.mockResolvedValueOnce(undefined);
      await repo.reorderAll(['l2', 'l1', 'l3'], '2026-05-01T00:00:00Z');
      expect(mockPut).toHaveBeenCalledWith('/api/v1/lanes/reorder', [
        { id: 'l2', order: 0 },
        { id: 'l1', order: 1 },
        { id: 'l3', order: 2 }
      ]);
    });
  });
});
