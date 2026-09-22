/**
 * Regression tests for optimistic-concurrency handling in the content save path.
 *
 * Background: page content writes were unconditional last-write-wins. A client
 * holding a stale document could overwrite newer content with no conflict
 * signal — the mechanism behind the 2026-09-21 production data-loss incidents.
 *
 * The server now rejects a write whose `expectedRevision` is stale with HTTP
 * 409. The client must reload rather than retry; retrying is the clobber.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { ApiError } from '$lib/storage/apiClient';

const saveContent = vi.fn();
const forgetRevision = vi.fn();
const notifyError = vi.fn();

vi.mock('$lib/stores/pages.svelte', () => ({
	pagesStore: {
		saveContent: (...args: unknown[]) => saveContent(...args),
		forgetRevision: (...args: unknown[]) => forgetRevision(...args)
	}
}));
vi.mock('$lib/stores/ui.svelte', () => ({
	uiStore: { markSaving: vi.fn(), markSaved: vi.fn() }
}));
vi.mock('$lib/stores/notifications.svelte', () => ({
	notificationsStore: { error: (...args: unknown[]) => notifyError(...args) }
}));
vi.mock('$lib/editor/useTaskTitleDebounce', () => ({
	flushAllTaskTitleUpdates: vi.fn().mockResolvedValue(undefined)
}));

import { useContentSave } from './useContentSave';

function fakeEditor(docText: string) {
	return { getJSON: () => ({ type: 'doc', text: docText }) } as never;
}

describe('useContentSave conflict handling', () => {
	beforeEach(() => {
		vi.clearAllMocks();
		saveContent.mockResolvedValue(undefined);
	});

	it('persists the scheduled document under the scheduled page', async () => {
		const handle = useContentSave();
		handle.scheduleSave(fakeEditor('hello'), 'page-A');
		await handle.flushContentSave();

		expect(saveContent).toHaveBeenCalledTimes(1);
		expect(saveContent).toHaveBeenCalledWith('page-A', { type: 'doc', text: 'hello' });
	});

	it('invokes the conflict handler and does NOT retry on 409', async () => {
		const onConflict = vi.fn();
		saveContent.mockRejectedValueOnce(
			new ApiError(409, 'PUT', '/api/v1/pages/page-A/content', { error: 'stale' })
		);

		const handle = useContentSave(undefined, onConflict);
		handle.scheduleSave(fakeEditor('stale doc'), 'page-A');
		await handle.flushContentSave();

		expect(onConflict).toHaveBeenCalledWith('page-A');
		// The stale revision must be dropped so the next write isn't guaranteed to conflict.
		expect(forgetRevision).toHaveBeenCalledWith('page-A');
		// Exactly one attempt — a retry would overwrite the newer content.
		expect(saveContent).toHaveBeenCalledTimes(1);
		expect(notifyError).toHaveBeenCalled();
	});

	it('treats non-conflict errors as ordinary failures', async () => {
		const onConflict = vi.fn();
		saveContent.mockRejectedValueOnce(
			new ApiError(500, 'PUT', '/api/v1/pages/page-A/content', { error: 'boom' })
		);

		const handle = useContentSave(undefined, onConflict);
		handle.scheduleSave(fakeEditor('doc'), 'page-A');
		await handle.flushContentSave();

		expect(onConflict).not.toHaveBeenCalled();
		expect(forgetRevision).not.toHaveBeenCalled();
		expect(notifyError).toHaveBeenCalled();
	});

	it('does not fire the conflict handler on a successful save', async () => {
		const onConflict = vi.fn();
		const handle = useContentSave(undefined, onConflict);
		handle.scheduleSave(fakeEditor('doc'), 'page-A');
		await handle.flushContentSave();

		expect(onConflict).not.toHaveBeenCalled();
		expect(notifyError).not.toHaveBeenCalled();
	});
});
