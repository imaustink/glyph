/**
 * Unit tests for ApiWebhookRepository.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { ApiWebhookRepository } from './ApiWebhookRepository';
import type { Webhook } from '$lib/models/types';

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

function makeWebhook(overrides: Partial<Webhook> = {}): Webhook {
	return {
		id: 'wh-1',
		userId: 'u-1',
		orgId: null,
		name: 'Zapier',
		url: 'https://hooks.example.com/glyph',
		eventTypes: ['task.created'],
		active: true,
		createdAt: '2026-11-01T00:00:00Z',
		updatedAt: '2026-11-01T00:00:00Z',
		...overrides
	};
}

describe('ApiWebhookRepository', () => {
	let repo: ApiWebhookRepository;

	beforeEach(() => {
		repo = new ApiWebhookRepository();
		vi.clearAllMocks();
	});

	it('lists webhooks, treating null as empty', async () => {
		mockGet.mockResolvedValueOnce([makeWebhook()]);
		expect(await repo.list()).toEqual([makeWebhook()]);
		expect(mockGet).toHaveBeenCalledWith('/api/v1/webhooks');

		mockGet.mockResolvedValueOnce(null);
		expect(await repo.list()).toEqual([]);
	});

	it('gets one webhook with an encoded id', async () => {
		mockGet.mockResolvedValueOnce(makeWebhook());
		await repo.get('a/b');
		expect(mockGet).toHaveBeenCalledWith('/api/v1/webhooks/a%2Fb');
	});

	it('creates, updates, rotates and deletes', async () => {
		const input = { name: 'n', url: 'https://x.test/h', eventTypes: ['note.created' as const], orgId: null };
		mockPost.mockResolvedValueOnce({ ...makeWebhook(), secret: 'whsec_x' });
		expect((await repo.create(input)).secret).toBe('whsec_x');
		expect(mockPost).toHaveBeenCalledWith('/api/v1/webhooks', input);

		mockPatch.mockResolvedValueOnce(makeWebhook({ active: false }));
		await repo.update('wh-1', { active: false });
		expect(mockPatch).toHaveBeenCalledWith('/api/v1/webhooks/wh-1', { active: false });

		mockPost.mockResolvedValueOnce({ ...makeWebhook(), secret: 'whsec_y' });
		await repo.rotateSecret('wh-1');
		expect(mockPost).toHaveBeenCalledWith('/api/v1/webhooks/wh-1/rotate-secret', {});

		mockDel.mockResolvedValueOnce(undefined);
		await repo.remove('wh-1');
		expect(mockDel).toHaveBeenCalledWith('/api/v1/webhooks/wh-1');
	});

	it('lists events with paging params', async () => {
		mockGet.mockResolvedValueOnce({ events: null, total: 0, retentionDays: 30 });
		expect(await repo.listEvents('wh-1')).toEqual({ events: [], total: 0, retentionDays: 30 });
		expect(mockGet).toHaveBeenCalledWith('/api/v1/webhooks/wh-1/events');

		mockGet.mockResolvedValueOnce({ events: [], total: 0, retentionDays: 30 });
		await repo.listEvents('wh-1', { limit: 50, offset: 100 });
		expect(mockGet).toHaveBeenCalledWith('/api/v1/webhooks/wh-1/events?limit=50&offset=100');
	});

	it('replays an event', async () => {
		mockPost.mockResolvedValueOnce({});
		await repo.replay('wh-1', 'ev-1');
		expect(mockPost).toHaveBeenCalledWith('/api/v1/webhooks/wh-1/events/ev-1/replay', {});
	});
});
