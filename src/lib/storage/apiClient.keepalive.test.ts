/**
 * DI-30: the editor flushes its debounced content save when the page is
 * hidden or unloaded. A normal fetch started then is cancelled when the tab
 * closes, so the content PUT must be able to go out with `keepalive` (the
 * option api.patch already has for the task-description flush).
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

vi.mock('$app/environment', () => ({ browser: false }));

import { api } from './apiClient';
import { ApiPageRepository } from './repositories/ApiPageRepository';

describe('keepalive for the content save (DI-30)', () => {
	let fetchSpy: ReturnType<typeof vi.spyOn>;

	beforeEach(() => {
		fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
			status: 200,
			ok: true,
			headers: { get: () => 'application/json' },
			json: async () => ({})
		} as unknown as Response);
	});
	afterEach(() => {
		vi.restoreAllMocks();
	});

	const lastInit = () => fetchSpy.mock.calls.at(-1)![1] as RequestInit;

	it('api.put sends keepalive when asked', async () => {
		await api.put('/api/v1/pages/p/content', { content: { type: 'doc' } }, { keepalive: true });
		expect(lastInit().keepalive).toBe(true);
	});

	it('api.put does not use keepalive by default', async () => {
		await api.put('/api/v1/pages/p/content', { content: { type: 'doc' } });
		expect(lastInit().keepalive).toBeFalsy();
	});

	it('ApiPageRepository.saveContent passes keepalive through', async () => {
		await new ApiPageRepository().saveContent(
			{ pageId: 'p', content: { type: 'doc' }, updatedAt: '2026-01-01T00:00:00Z' },
			{ keepalive: true }
		);
		expect(lastInit().method).toBe('PUT');
		expect(lastInit().keepalive).toBe(true);
	});
});
