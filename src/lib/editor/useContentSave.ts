import type { Editor } from '@tiptap/core';
import { pagesStore } from '$lib/stores/pages.svelte';
import { uiStore } from '$lib/stores/ui.svelte';
import { notificationsStore } from '$lib/stores/notifications.svelte';
import { flushAllTaskTitleUpdates } from '$lib/editor/useTaskTitleDebounce';
import { DEBOUNCE } from '$lib/models/constants';
import { ApiError, apiErrorCode } from '$lib/storage/apiClient';

export interface ContentSaveHandle {
	/** Schedule a debounced content save for the current editor state. */
	scheduleSave(editor: Editor, pageId: string): void;
	/** Immediately persist any pending content save. */
	flushContentSave(): Promise<void>;
	/** Flush all pending writes (content + task titles). */
	flushAll(): Promise<void>;
	/** Clean up timers. */
	destroy(): void;
}

/**
 * Called when the server rejects a save because the document was derived from a
 * stale read (HTTP 409). The caller is expected to reload the page's content —
 * retrying the same write would reintroduce the overwrite this guards against.
 */
export type ContentConflictHandler = (pageId: string) => void;

export function useContentSave(
	onchange?: () => void,
	onConflict?: ContentConflictHandler
): ContentSaveHandle {
	let saveTimer: ReturnType<typeof setTimeout> | null = null;
	let pendingContentJson: Record<string, unknown> | null = null;
	let pendingContentPageId: string | null = null;
	// At most one save may be in flight at a time. saveContent reads the known
	// revision at call time, so two overlapping PUTs would send the same
	// (now-stale after the first commits) expectedRevision and the second would
	// take a spurious 409 — the very self-inflicted edit loss this guards
	// against. Serialising also guarantees the newest pending content is written
	// under the revision the prior save advanced to.
	let inFlight: Promise<void> | null = null;

	async function persistPending(): Promise<void> {
		const json = pendingContentJson;
		const pid = pendingContentPageId;
		pendingContentJson = null;
		pendingContentPageId = null;
		if (json == null || pid == null) return;
		uiStore.markSaving();
		try {
			await pagesStore.saveContent(pid, json);
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

	async function flushContentSave(): Promise<void> {
		if (saveTimer) { clearTimeout(saveTimer); saveTimer = null; }
		// A save is already running: wait for it to settle rather than starting a
		// concurrent PUT with a stale precondition. Once it finishes, persist the
		// latest pending content — unless another waiter already claimed it.
		if (inFlight) {
			await inFlight;
			if (pendingContentJson == null || pendingContentPageId == null) return;
		}
		const run = persistPending();
		inFlight = run;
		try {
			await run;
		} finally {
			if (inFlight === run) inFlight = null;
		}
	}

	async function flushAll() {
		await Promise.all([flushContentSave(), flushAllTaskTitleUpdates()]);
	}

	function scheduleSave(editor: Editor, pageId: string) {
		pendingContentJson = editor.getJSON() as Record<string, unknown>;
		pendingContentPageId = pageId;
		if (saveTimer) clearTimeout(saveTimer);
		saveTimer = setTimeout(async () => {
			await flushContentSave();
		}, DEBOUNCE.CONTENT_SAVE);
	}

	function destroy() {
		if (saveTimer) clearTimeout(saveTimer);
	}

	return { scheduleSave, flushContentSave, flushAll, destroy };
}
