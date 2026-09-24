/**
 * Unit tests for ApiOAuthConnectionsRepository.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { ApiOAuthConnectionsRepository } from './ApiOAuthConnectionsRepository';
import type { OAuthConnection } from '$lib/models/types';

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
const mockDel = vi.mocked(api.del);

function makeConnection(overrides: Partial<OAuthConnection> = {}): OAuthConnection {
	return {
		id: 'conn-1',
		clientName: 'Claude',
		dynamic: true,
		scopes: ['page:read', 'lane:read'],
		personal: true,
		orgs: [{ id: 'org-1', name: 'Acme' }],
		createdAt: '2026-01-01T00:00:00Z',
		lastUsedAt: null,
		...overrides
	};
}

describe('ApiOAuthConnectionsRepository', () => {
	let repo: ApiOAuthConnectionsRepository;

	beforeEach(() => {
		repo = new ApiOAuthConnectionsRepository();
		vi.clearAllMocks();
	});

	describe('list', () => {
		it('calls GET /api/v1/oauth/connections', async () => {
			const connections = [makeConnection()];
			mockGet.mockResolvedValueOnce(connections);

			expect(await repo.list()).toEqual(connections);
			expect(mockGet).toHaveBeenCalledWith('/api/v1/oauth/connections');
		});

		it('returns empty array when null', async () => {
			mockGet.mockResolvedValueOnce(null);
			expect(await repo.list()).toEqual([]);
		});
	});

	describe('revoke', () => {
		it('calls DELETE /api/v1/oauth/connections/:id', async () => {
			mockDel.mockResolvedValueOnce(undefined);
			await repo.revoke('conn-1');
			expect(mockDel).toHaveBeenCalledWith('/api/v1/oauth/connections/conn-1');
		});

		it('URL-encodes the id', async () => {
			mockDel.mockResolvedValueOnce(undefined);
			await repo.revoke('a/b');
			expect(mockDel).toHaveBeenCalledWith('/api/v1/oauth/connections/a%2Fb');
		});
	});
});
