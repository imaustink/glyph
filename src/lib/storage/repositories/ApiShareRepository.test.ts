/**
 * Unit tests for ApiShareRepository.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { ApiShareRepository } from './ApiShareRepository';
import type { Share } from '$lib/models/types';

vi.mock('$lib/storage/apiClient', () => ({
	API_BASE: 'http://localhost:8081',
	api: {
		get: vi.fn(),
		post: vi.fn(),
		patch: vi.fn(),
		del: vi.fn()
	}
}));

import { api } from '$lib/storage/apiClient';

const mockGet = vi.mocked(api.get);
const mockPost = vi.mocked(api.post);
const mockPatch = vi.mocked(api.patch);
const mockDel = vi.mocked(api.del);

function makeShare(overrides: Partial<Share> = {}): Share {
	return {
		id: 'share-1',
		resourceType: 'page',
		resourceId: 'page-1',
		sharedById: 'user-1',
		sharedWith: { id: 'user-2', email: 'bob@example.com', name: 'Bob' },
		permission: 'viewer',
		createdAt: '2026-01-01T00:00:00Z',
		...overrides
	};
}

describe('ApiShareRepository', () => {
	let repo: ApiShareRepository;

	beforeEach(() => {
		repo = new ApiShareRepository();
		vi.clearAllMocks();
	});

	describe('list', () => {
		it('calls GET /api/v1/shares with query params', async () => {
			const shares = [makeShare()];
			mockGet.mockResolvedValueOnce(shares);

			const result = await repo.list('page', 'page-1');
			expect(result).toEqual(shares);
			expect(mockGet).toHaveBeenCalledWith(
				'/api/v1/shares?resourceType=page&resourceId=page-1'
			);
		});

		it('returns empty array when null', async () => {
			mockGet.mockResolvedValueOnce(null);
			expect(await repo.list('page', 'page-1')).toEqual([]);
		});
	});

	describe('create', () => {
		it('calls POST /api/v1/shares with email field', async () => {
			const share = makeShare();
			mockPost.mockResolvedValueOnce(share);

			const result = await repo.create('page', 'page-1', 'bob@example.com', 'viewer');
			expect(result).toEqual(share);
			expect(mockPost).toHaveBeenCalledWith('/api/v1/shares', {
				resourceType: 'page',
				resourceId: 'page-1',
				sharedWithEmail: 'bob@example.com',
				permission: 'viewer'
			});
		});
	});

	describe('updatePermission', () => {
		it('calls PATCH /api/v1/shares/:shareId', async () => {
			const share = makeShare({ permission: 'editor' });
			mockPatch.mockResolvedValueOnce(share);

			const result = await repo.updatePermission('share-1', 'editor');
			expect(result).toEqual(share);
			expect(mockPatch).toHaveBeenCalledWith('/api/v1/shares/share-1', {
				permission: 'editor'
			});
		});
	});

	describe('delete', () => {
		it('calls DELETE /api/v1/shares/:shareId', async () => {
			mockDel.mockResolvedValueOnce(undefined);
			await repo.delete('share-1');
			expect(mockDel).toHaveBeenCalledWith('/api/v1/shares/share-1');
		});
	});

	describe('searchUsers', () => {
		it('calls GET /api/v1/users/search with encoded query', async () => {
			const users = [{ id: 'user-2', email: 'bob@example.com', name: 'Bob' }];
			mockGet.mockResolvedValueOnce(users);

			const result = await repo.searchUsers('bob');
			expect(result).toEqual(users);
			expect(mockGet).toHaveBeenCalledWith('/api/v1/users/search?q=bob');
		});

		it('returns empty array for empty query', async () => {
			const result = await repo.searchUsers('');
			expect(result).toEqual([]);
			expect(mockGet).not.toHaveBeenCalled();
		});

		it('URL-encodes the query string', async () => {
			mockGet.mockResolvedValueOnce([]);
			await repo.searchUsers('john doe');
			expect(mockGet).toHaveBeenCalledWith('/api/v1/users/search?q=john%20doe');
		});

		it('returns empty array when api.get returns null', async () => {
			mockGet.mockResolvedValueOnce(null);
			const result = await repo.searchUsers('any query');
			expect(result).toEqual([]);
		});
	});
});
