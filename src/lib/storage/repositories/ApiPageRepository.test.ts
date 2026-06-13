/**
 * Unit tests for ApiPageRepository.
 *
 * Uses a mocked api client so no real HTTP calls are made.
 * Covers all public methods including the special deleteSubtree behaviour
 * (ignores descendantIds, delegates to a single DELETE).
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { ApiPageRepository } from './ApiPageRepository';
import type { TreeNode, PageContent } from '$lib/models/types';

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

function makeNode(overrides: Partial<TreeNode> = {}): TreeNode {
  return {
    id: 'node-1',
    type: 'page',
    title: 'Test Page',
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

describe('ApiPageRepository', () => {
  let repo: ApiPageRepository;

  beforeEach(() => {
    repo = new ApiPageRepository();
    vi.clearAllMocks();
  });

  describe('getAll', () => {
    it('returns pages from GET /api/v1/pages', async () => {
      const nodes = [makeNode({ id: 'p1' })];
      mockGet.mockResolvedValueOnce(nodes);
      expect(await repo.getAll()).toEqual(nodes);
      expect(mockGet).toHaveBeenCalledWith('/api/v1/pages');
    });

    it('returns empty array when response is null', async () => {
      mockGet.mockResolvedValueOnce(null);
      expect(await repo.getAll()).toEqual([]);
    });
  });

  describe('getById', () => {
    it('returns the node on success', async () => {
      const node = makeNode({ id: 'p1' });
      mockGetOrNull.mockResolvedValueOnce(node);
      expect(await repo.getById('p1')).toEqual(node);
      expect(mockGetOrNull).toHaveBeenCalledWith('/api/v1/pages/p1');
    });

    it('re-throws UnauthorizedError', async () => {
      mockGetOrNull.mockRejectedValueOnce(new UnauthorizedError('GET', '/api/v1/pages/p1'));
      await expect(repo.getById('p1')).rejects.toBeInstanceOf(UnauthorizedError);
    });

    it('returns null when not found', async () => {
      mockGetOrNull.mockResolvedValueOnce(null);
      expect(await repo.getById('p1')).toBeNull();
    });
  });

  describe('create', () => {
    it('calls POST /api/v1/pages with the node', async () => {
      const node = makeNode({ id: 'p1' });
      mockPost.mockResolvedValueOnce(node);
      const result = await repo.create(node);
      expect(result).toEqual(node);
      expect(mockPost).toHaveBeenCalledWith('/api/v1/pages', node);
    });
  });

  describe('update', () => {
    it('calls PATCH /api/v1/pages/:id with the patch', async () => {
      const node = makeNode({ id: 'p1', title: 'Updated' });
      mockPatch.mockResolvedValueOnce(node);
      const result = await repo.update('p1', { title: 'Updated' });
      expect(result).toEqual(node);
      expect(mockPatch).toHaveBeenCalledWith('/api/v1/pages/p1', { title: 'Updated' });
    });

    it('propagates errors', async () => {
      mockPatch.mockRejectedValueOnce(new Error('fail'));
      await expect(repo.update('p1', { title: 'x' })).rejects.toThrow('fail');
    });
  });

  describe('delete', () => {
    it('calls DEL /api/v1/pages/:id and returns true', async () => {
      mockDel.mockResolvedValueOnce(undefined);
      expect(await repo.delete('p1')).toBe(true);
      expect(mockDel).toHaveBeenCalledWith('/api/v1/pages/p1');
    });

    it('propagates errors', async () => {
      mockDel.mockRejectedValueOnce(new Error('fail'));
      await expect(repo.delete('p1')).rejects.toThrow('fail');
    });
  });

  describe('getContent', () => {
    it('returns content from GET /api/v1/pages/:id/content', async () => {
      const content: PageContent = { pageId: 'p1', content: { type: 'doc' }, updatedAt: '2026-01-01T00:00:00Z' };
      mockGetOrNull.mockResolvedValueOnce(content);
      expect(await repo.getContent('p1')).toEqual(content);
    });

    it('returns null when content not found', async () => {
      mockGetOrNull.mockResolvedValueOnce(null);
      expect(await repo.getContent('p1')).toBeNull();
    });
  });

  describe('saveContent', () => {
    it('calls PUT /api/v1/pages/:id/content', async () => {
      const content: PageContent = { pageId: 'p1', content: { type: 'doc' }, updatedAt: '2026-01-01T00:00:00Z' };
      mockPut.mockResolvedValueOnce(undefined);
      await repo.saveContent(content);
      expect(mockPut).toHaveBeenCalledWith('/api/v1/pages/p1/content', content);
    });
  });

  describe('deleteContent', () => {
    it('is a no-op (content is cascade-deleted server-side)', async () => {
      // deleteContent is intentionally empty on the API side — just verify it resolves
      await expect(repo.deleteContent('p1')).resolves.toBeUndefined();
    });
  });

  describe('deleteSubtree', () => {
    it('calls delete(id) and ignores descendantIds', async () => {
      mockDel.mockResolvedValueOnce(undefined);
      await repo.deleteSubtree('root', ['child1', 'child2', 'grandchild']);
      // Only one DELETE call for the root
      expect(mockDel).toHaveBeenCalledTimes(1);
      expect(mockDel).toHaveBeenCalledWith('/api/v1/pages/root');
    });
  });

  describe('upsert', () => {
    it('calls PUT /api/v1/pages/:id with the full page', async () => {
      const page = makeNode({ id: 'p1' });
      mockPut.mockResolvedValueOnce(page);
      const result = await repo.upsert(page);
      expect(mockPut).toHaveBeenCalledWith('/api/v1/pages/p1', page);
      expect(result).toEqual(page);
    });

    it('propagates errors from PUT', async () => {
      const page = makeNode({ id: 'p1' });
      mockPut.mockRejectedValueOnce(new Error('fail'));
      await expect(repo.upsert(page)).rejects.toThrow('fail');
    });
  });

  describe('deleteWithContent', () => {
    it('delegates to delete and returns true on success', async () => {
      mockDel.mockResolvedValueOnce(undefined);
      const result = await repo.deleteWithContent('p1');
      expect(result).toBe(true);
      expect(mockDel).toHaveBeenCalledWith('/api/v1/pages/p1');
    });

    it('propagates error when delete fails', async () => {
      mockDel.mockRejectedValueOnce(new Error('fail'));
      await expect(repo.deleteWithContent('p1')).rejects.toThrow('fail');
    });
  });

  describe('getTree', () => {
    it('returns root nodes sorted by order', () => {
      const nodes = [
        makeNode({ id: 'c', parentId: null, order: 2 }),
        makeNode({ id: 'child', parentId: 'a', order: 0 }),
        makeNode({ id: 'a', parentId: null, order: 0 }),
        makeNode({ id: 'b', parentId: null, order: 1 })
      ];
      expect(repo.getTree(nodes).map((n) => n.id)).toEqual(['a', 'b', 'c']);
    });
  });

  describe('getChildren', () => {
    it('returns children of a parent sorted by order', () => {
      const nodes = [
        makeNode({ id: 'child2', parentId: 'root', order: 1 }),
        makeNode({ id: 'child1', parentId: 'root', order: 0 }),
        makeNode({ id: 'other', parentId: 'other-parent', order: 0 })
      ];
      expect(repo.getChildren(nodes, 'root').map((n) => n.id)).toEqual(['child1', 'child2']);
    });
  });
});
