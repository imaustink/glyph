/**
 * Unit tests for ApiOAuthConsentRepository.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { ApiOAuthConsentRepository } from './ApiOAuthConsentRepository';
import type { OAuthConsentInfo } from '$lib/models/types';

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

describe('ApiOAuthConsentRepository', () => {
	let repo: ApiOAuthConsentRepository;

	beforeEach(() => {
		repo = new ApiOAuthConsentRepository();
		vi.clearAllMocks();
	});

	describe('getInfo', () => {
		it('calls GET /api/v1/oauth/consent with the raw query string', async () => {
			const info: OAuthConsentInfo = {
				client: { name: 'Claude', dynamic: true, redirectHost: 'claude.ai' },
				scopes: ['page:read', 'lane:read'],
				consentToken: 'ct',
				orgName: null,
				workspaceSelection: true,
				workspaces: [{ id: 'personal', name: 'Personal workspace', kind: 'personal' }]
			};
			mockGet.mockResolvedValueOnce(info);

			const result = await repo.getInfo('client_id=abc&scope=page%3Aread');
			expect(result).toEqual(info);
			expect(mockGet).toHaveBeenCalledWith(
				'/api/v1/oauth/consent?client_id=abc&scope=page%3Aread'
			);
		});
	});

	describe('decide', () => {
		beforeEach(() => {
			mockPost.mockResolvedValue({ redirectUrl: 'https://example.com/cb?code=x' });
		});

		it('posts only token + approve in fixed mode', async () => {
			const result = await repo.decide('ct', true);
			expect(result).toEqual({ redirectUrl: 'https://example.com/cb?code=x' });
			expect(mockPost).toHaveBeenCalledWith('/api/v1/oauth/consent/decision', {
				consentToken: 'ct',
				approve: true
			});
		});

		it('includes the workspace selection when approving', async () => {
			await repo.decide('ct', true, { personal: true, orgIds: ['org-1', 'org-2'] });
			expect(mockPost).toHaveBeenCalledWith('/api/v1/oauth/consent/decision', {
				consentToken: 'ct',
				approve: true,
				personal: true,
				orgIds: ['org-1', 'org-2']
			});
		});

		it('omits the selection when denying', async () => {
			await repo.decide('ct', false, { personal: true, orgIds: [] });
			expect(mockPost).toHaveBeenCalledWith('/api/v1/oauth/consent/decision', {
				consentToken: 'ct',
				approve: false
			});
		});
	});
});
