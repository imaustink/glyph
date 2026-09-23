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

	// A slow save must not overlap a second flush. saveContent reads the known
	// revision at call time, so two concurrent PUTs would send the same
	// (post-commit stale) expectedRevision and the second would take a spurious
	// 409 — reloading away the user's newest edits. Saves must serialise, and the
	// newest pending content must still be persisted.
	it('serialises overlapping saves and persists the newest content without a spurious conflict', async () => {
		let releaseFirst!: () => void;
		const firstGate = new Promise<void>((resolve) => {
			releaseFirst = resolve;
		});
		let active = 0;
		let maxActive = 0;
		saveContent.mockImplementation(async (_pid: string, content: { text: string }) => {
			active++;
			maxActive = Math.max(maxActive, active);
			if (content.text === 'v1') await firstGate;
			active--;
		});

		const onConflict = vi.fn();
		const handle = useContentSave(undefined, onConflict);

		// First save starts and blocks mid-flight.
		handle.scheduleSave(fakeEditor('v1'), 'page-A');
		const first = handle.flushContentSave();
		await Promise.resolve();
		expect(saveContent).toHaveBeenCalledTimes(1);

		// A newer edit is flushed while the first save is still outstanding.
		handle.scheduleSave(fakeEditor('v2'), 'page-A');
		const second = handle.flushContentSave();
		await Promise.resolve();
		// The second save must not have started while the first is in flight.
		expect(saveContent).toHaveBeenCalledTimes(1);

		releaseFirst();
		await Promise.all([first, second]);

		// Never ran concurrently, both persisted, newest content last.
		expect(maxActive).toBe(1);
		expect(saveContent).toHaveBeenCalledTimes(2);
		expect(saveContent).toHaveBeenNthCalledWith(1, 'page-A', { type: 'doc', text: 'v1' });
		expect(saveContent).toHaveBeenNthCalledWith(2, 'page-A', { type: 'doc', text: 'v2' });
		// No stale-precondition self-conflict, so no reload of the user's own edits.
		expect(onConflict).not.toHaveBeenCalled();
		expect(notifyError).not.toHaveBeenCalled();
	});
});
