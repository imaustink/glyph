/**
 * Unit tests for ApiOrgRepository.
 *
 * Uses a global fetch mock so no real network calls are made.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { ApiOrgRepository } from './ApiOrgRepository';
import type { OrgWithRole, OrgMember } from '$lib/models/types';

// Mock the api client module
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

function makeOrg(overrides: Partial<OrgWithRole> = {}): OrgWithRole {
	return {
		id: 'org-1',
		name: 'Test Org',
		createdBy: 'user-1',
		memberCount: 1,
		role: 'owner',
		createdAt: '2026-01-01T00:00:00Z',
		updatedAt: '2026-01-01T00:00:00Z',
		...overrides
	};
}

function makeMember(overrides: Partial<OrgMember> = {}): OrgMember {
	return {
		orgId: 'org-1',
		userId: 'user-2',
		email: 'bob@example.com',
		name: 'Bob',
		role: 'viewer',
		joinedAt: '2026-01-01T00:00:00Z',
		...overrides
	};
}

describe('ApiOrgRepository', () => {
	let repo: ApiOrgRepository;

	beforeEach(() => {
		repo = new ApiOrgRepository();
		vi.clearAllMocks();
	});

	describe('list', () => {
		it('returns orgs from GET /api/v1/orgs', async () => {
			const orgs = [makeOrg()];
			mockGet.mockResolvedValueOnce(orgs);

			const result = await repo.list();
			expect(result).toEqual(orgs);
			expect(mockGet).toHaveBeenCalledWith('/api/v1/orgs');
		});

		it('returns empty array when response is null', async () => {
			mockGet.mockResolvedValueOnce(null);
			const result = await repo.list();
			expect(result).toEqual([]);
		});
	});

	describe('create', () => {
		it('calls POST /api/v1/orgs with name', async () => {
			const org = makeOrg({ name: 'New Org' });
			mockPost.mockResolvedValueOnce(org);

			const result = await repo.create('New Org');
			expect(result).toEqual(org);
			expect(mockPost).toHaveBeenCalledWith('/api/v1/orgs', { name: 'New Org' });
		});
	});

	describe('get', () => {
		it('returns org detail with members', async () => {
			const detail = { org: makeOrg(), members: [makeMember()] };
			mockGet.mockResolvedValueOnce(detail);

			const result = await repo.get('org-1');
			expect(result).toEqual(detail);
			expect(mockGet).toHaveBeenCalledWith('/api/v1/orgs/org-1');
		});

		it('returns null on error', async () => {
			mockGet.mockRejectedValueOnce(new Error('404'));
			const result = await repo.get('missing');
			expect(result).toBeNull();
		});
	});

	describe('update', () => {
		it('calls PATCH /api/v1/orgs/:id with name', async () => {
			const updated = makeOrg({ name: 'Renamed' });
			mockPatch.mockResolvedValueOnce(updated);

			const result = await repo.update('org-1', 'Renamed');
			expect(result).toEqual(updated);
			expect(mockPatch).toHaveBeenCalledWith('/api/v1/orgs/org-1', { name: 'Renamed' });
		});
	});

	describe('delete', () => {
		it('calls DELETE /api/v1/orgs/:id', async () => {
			mockDel.mockResolvedValueOnce(undefined);
			await repo.delete('org-1');
			expect(mockDel).toHaveBeenCalledWith('/api/v1/orgs/org-1');
		});
	});

	describe('addMember', () => {
		it('calls POST /api/v1/orgs/:id/members with email field', async () => {
			const member = makeMember();
			mockPost.mockResolvedValueOnce(member);

			// addMember takes an email string (2nd arg) and passes it as { email, role }
			const result = await repo.addMember('org-1', 'bob@example.com', 'viewer');
			expect(result).toEqual(member);
			expect(mockPost).toHaveBeenCalledWith('/api/v1/orgs/org-1/members', {
				email: 'bob@example.com',
				role: 'viewer'
			});
		});
	});

	describe('updateMemberRole', () => {
		it('calls PATCH /api/v1/orgs/:id/members/:userId', async () => {
			const member = makeMember({ role: 'editor' });
			mockPatch.mockResolvedValueOnce(member);

			const result = await repo.updateMemberRole('org-1', 'user-2', 'editor');
			expect(result).toEqual(member);
			expect(mockPatch).toHaveBeenCalledWith('/api/v1/orgs/org-1/members/user-2', {
				role: 'editor'
			});
		});
	});

	describe('removeMember', () => {
		it('calls DELETE /api/v1/orgs/:id/members/:userId', async () => {
			mockDel.mockResolvedValueOnce(undefined);
			await repo.removeMember('org-1', 'user-2');
			expect(mockDel).toHaveBeenCalledWith('/api/v1/orgs/org-1/members/user-2');
		});
	});
});
