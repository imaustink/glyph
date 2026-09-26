import type { Editor } from '@tiptap/core';
import { pagesStore } from '$lib/stores/pages.svelte';
import { uiStore } from '$lib/stores/ui.svelte';
import { notificationsStore } from '$lib/stores/notifications.svelte';
import { flushAllTaskTitleUpdates } from '$lib/editor/useTaskTitleDebounce';
import { DEBOUNCE } from '$lib/models/constants';
import { ApiError, apiErrorCode } from '$lib/storage/apiClient';
import type { WriteOptions } from '$lib/storage/interfaces';

export interface ContentSaveHandle {
	/** Schedule a debounced content save for the current editor state. */
	scheduleSave(editor: Editor, pageId: string): void;
	/** Immediately persist any pending content save. */
	flushContentSave(opts?: WriteOptions): Promise<void>;
	/**
	 * Flush all pending writes (content + task titles). Never rejects.
	 * `{ keepalive: true }` when the page is being hidden or unloaded.
	 */
	flushAll(opts?: WriteOptions): Promise<void>;
	/** Whether a content save is waiting on its timer, queued, or in flight. */
	hasPendingWork(): boolean;
	/** Clean up timers. */
	destroy(): void;
}

/**
 * Called when the server rejects a save because the document was derived from a
 * stale read (HTTP 409). The caller is expected to reload the page's content —
 * retrying the same write would reintroduce the overwrite this guards against.
 */
export type ContentConflictHandler = (pageId: string) => void;

let instanceCounter = 0;

export function useContentSave(
	onchange?: () => void,
	onConflict?: ContentConflictHandler
): ContentSaveHandle {
	let saveTimer: ReturnType<typeof setTimeout> | null = null;
	// Registered with uiStore while the debounce timer is armed, so the
	// navigation guard and tab-close warning see the waiting save (DI-30).
	const pendingKey = `content-save:${++instanceCounter}`;
	function clearTimer() {
		if (saveTimer) { clearTimeout(saveTimer); saveTimer = null; }
		uiStore.setPendingDebounce(pendingKey, false);
	}
	let pendingContentJson: Record<string, unknown> | null = null;
	let pendingContentPageId: string | null = null;
	// At most one save may be in flight at a time. saveContent reads the known
	// revision at call time, so two overlapping PUTs would send the same
	// (now-stale after the first commits) expectedRevision and the second would
	// take a spurious 409 — the very self-inflicted edit loss this guards
	// against. Serialising also guarantees the newest pending content is written
	// under the revision the prior save advanced to.
	let inFlight: Promise<void> | null = null;

	async function persistPending(opts?: WriteOptions): Promise<void> {
		const json = pendingContentJson;
		const pid = pendingContentPageId;
		pendingContentJson = null;
		pendingContentPageId = null;
		if (json == null || pid == null) return;
		uiStore.markSaving();
		try {
			await (opts ? pagesStore.saveContent(pid, json, opts) : pagesStore.saveContent(pid, json));
			onchange?.();
			uiStore.markSaved();
		} catch (err) {
			uiStore.markSaved();
			if (err instanceof ApiError && err.status === 409) {
				// Someone else (or another client of ours) wrote newer content,
				// or the page is now owned by a collaborative session. Do not
				// retry: reload so the user sees the current document instead
				// of silently overwriting it with our stale copy. The reload
				// also switches to the collaborative editor where applicable.
				pagesStore.forgetRevision(pid);
				// Anything still waiting for this page — its debounce timer
				// armed, or queued behind this save — was built on the same
				// stale copy. Sent after the reload it would carry the fresh
				// revision and overwrite the other writer's content (DI-04).
				if (pendingContentPageId === pid) {
					clearTimer();
					pendingContentJson = null;
					pendingContentPageId = null;
				}
				notificationsStore.error(
					apiErrorCode(err) === 'collaborative'
						? 'This note is now being edited collaboratively. Reloading it — your last few seconds of edits were not applied.'
						: 'This note changed elsewhere. Reloading the latest version — your unsaved edits were not applied.'
				);
				console.warn('[Editor] Content save conflict for page', pid);
				onConflict?.(pid);
				return;
			}
			const message = err instanceof Error ? err.message : 'Failed to save note.';
			notificationsStore.error(message);
			console.error('[Editor] Content save failed:', err);
		}
	}

	async function flushContentSave(opts?: WriteOptions): Promise<void> {
		clearTimer();
		// A save is already running: wait for it to settle rather than starting a
		// concurrent PUT with a stale precondition. Once it finishes, persist the
		// latest pending content — unless another waiter already claimed it.
		if (inFlight) {
			await inFlight;
			if (pendingContentJson == null || pendingContentPageId == null) return;
		}
		const run = persistPending(opts);
		inFlight = run;
		try {
			await run;
		} finally {
			if (inFlight === run) inFlight = null;
		}
	}

	/**
	 * Never rejects: callers chain navigation onto it (the editor opens the
	 * next page once it settles), so one failed write must not strand them.
	 */
	async function flushAll(opts?: WriteOptions) {
		const results = await Promise.allSettled([flushContentSave(opts), flushAllTaskTitleUpdates(opts)]);
		for (const r of results) {
			if (r.status === 'rejected') {
				console.error('[Editor] Flushing pending writes failed:', r.reason);
				notificationsStore.error('Some changes could not be saved.');
			}
		}
	}

	function scheduleSave(editor: Editor, pageId: string) {
		pendingContentJson = editor.getJSON() as Record<string, unknown>;
		pendingContentPageId = pageId;
		if (saveTimer) clearTimeout(saveTimer);
		uiStore.setPendingDebounce(pendingKey, true);
		saveTimer = setTimeout(async () => {
			await flushContentSave();
		}, DEBOUNCE.CONTENT_SAVE);
	}

	function hasPendingWork(): boolean {
		return saveTimer !== null || pendingContentJson !== null || inFlight !== null;
	}

	function destroy() {
		clearTimer();
	}

	return { scheduleSave, flushContentSave, flushAll, hasPendingWork, destroy };
}
