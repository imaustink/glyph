/**
 * DI-30: the editor flushes pending writes when the page is hidden or
 * unloaded. A normal fetch started then is cancelled when the tab closes; a
 * write started while the document is hidden must use `keepalive` so it
 * outlives the page. (Browsers cap keepalive bodies at 64 KiB in total, so
 * larger bodies go without it rather than fail outright.)
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

vi.mock('$app/environment', () => ({ browser: false }));

import { api } from './apiClient';

function setVisibility(state: 'visible' | 'hidden') {
	Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => state });
}

describe('apiClient keepalive while the page is hidden (DI-30)', () => {
	let fetchSpy: ReturnType<typeof vi.spyOn>;

	beforeEach(() => {
		fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
			status: 204,
			ok: true,
			headers: { get: () => null }
		} as unknown as Response);
	});
	afterEach(() => {
		setVisibility('visible');
		vi.restoreAllMocks();
	});

	const lastInit = () => fetchSpy.mock.calls.at(-1)![1] as RequestInit;

	it('sends writes with keepalive while the document is hidden', async () => {
		setVisibility('hidden');
		await api.put('/api/v1/pages/p/content', { content: { type: 'doc' } });
		expect(lastInit().keepalive).toBe(true);
	});

	it('does not use keepalive while the document is visible', async () => {
		setVisibility('visible');
		await api.put('/api/v1/pages/p/content', { content: { type: 'doc' } });
		expect(lastInit().keepalive).toBeFalsy();
	});

	it('skips keepalive for bodies over the browser limit', async () => {
		setVisibility('hidden');
		await api.put('/api/v1/pages/p/content', { content: 'x'.repeat(70_000) });
		expect(lastInit().keepalive).toBeFalsy();
	});
});
