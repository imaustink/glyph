/**
 * Unit tests for ApiTemplateRepository.
 *
 * Uses a mocked api client so no real HTTP calls are made.
 * Covers all CRUD methods.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { ApiTemplateRepository } from './ApiTemplateRepository';
import type { NoteTemplate } from '$lib/models/types';

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

function makeTemplate(overrides: Partial<NoteTemplate> = {}): NoteTemplate {
  return {
    id: 'tpl-1',
    name: 'Default',
    content: '{}',
    titleTemplate: '',
    todoTrigger: { pattern: 'TODO', matchMode: 'exact', blockTypes: ['heading'] },
    isDefault: false,
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
    ...overrides
  };
}

describe('ApiTemplateRepository', () => {
  let repo: ApiTemplateRepository;

  beforeEach(() => {
    repo = new ApiTemplateRepository();
    vi.clearAllMocks();
  });

  describe('getAll', () => {
    it('returns templates from GET /api/v1/templates', async () => {
      const tpls = [makeTemplate({ id: 't1' })];
      mockGet.mockResolvedValueOnce(tpls);
      expect(await repo.getAll()).toEqual(tpls);
      expect(mockGet).toHaveBeenCalledWith('/api/v1/templates');
    });

    it('returns empty array when response is null', async () => {
      mockGet.mockResolvedValueOnce(null);
      expect(await repo.getAll()).toEqual([]);
    });
  });

  describe('getById', () => {
    it('returns the template on success', async () => {
      const tpl = makeTemplate({ id: 't1' });
      mockGetOrNull.mockResolvedValueOnce(tpl);
      expect(await repo.getById('t1')).toEqual(tpl);
    });

    it('re-throws UnauthorizedError', async () => {
      mockGetOrNull.mockRejectedValueOnce(new UnauthorizedError('GET', '/api/v1/templates/t1'));
      await expect(repo.getById('t1')).rejects.toBeInstanceOf(UnauthorizedError);
    });

    it('returns null when not found', async () => {
      mockGetOrNull.mockResolvedValueOnce(null);
      expect(await repo.getById('t1')).toBeNull();
    });
  });

  describe('create', () => {
    it('calls POST /api/v1/templates', async () => {
      const tpl = makeTemplate({ id: 't1' });
      mockPost.mockResolvedValueOnce(tpl);
      const result = await repo.create(tpl);
      expect(result).toEqual(tpl);
      expect(mockPost).toHaveBeenCalledWith('/api/v1/templates', tpl);
    });
  });

  describe('update', () => {
    it('calls PATCH /api/v1/templates/:id', async () => {
      const tpl = makeTemplate({ id: 't1', name: 'Updated' });
      mockPatch.mockResolvedValueOnce(tpl);
      expect(await repo.update('t1', { name: 'Updated' })).toEqual(tpl);
      expect(mockPatch).toHaveBeenCalledWith('/api/v1/templates/t1', { name: 'Updated' });
    });

    it('propagates errors', async () => {
      mockPatch.mockRejectedValueOnce(new Error('fail'));
      await expect(repo.update('t1', {})).rejects.toThrow('fail');
    });
  });

  describe('delete', () => {
    it('returns true on success', async () => {
      mockDel.mockResolvedValueOnce(undefined);
      expect(await repo.delete('t1')).toBe(true);
      expect(mockDel).toHaveBeenCalledWith('/api/v1/templates/t1');
    });

    it('propagates errors', async () => {
      mockDel.mockRejectedValueOnce(new Error('fail'));
      await expect(repo.delete('t1')).rejects.toThrow('fail');
    });
  });

  describe('upsert', () => {
    it('calls PUT /api/v1/templates/:id with the full template', async () => {
      const tpl = makeTemplate({ id: 't1' });
      mockPut.mockResolvedValueOnce(tpl);
      const result = await repo.upsert(tpl);
      expect(mockPut).toHaveBeenCalledWith('/api/v1/templates/t1', tpl);
      expect(result).toEqual(tpl);
    });

    it('propagates errors from PUT', async () => {
      const tpl = makeTemplate({ id: 't1' });
      mockPut.mockRejectedValueOnce(new Error('fail'));
      await expect(repo.upsert(tpl)).rejects.toThrow('fail');
    });
  });
});
