import type { Editor } from '@tiptap/core';
import { pagesStore } from '$lib/stores/pages.svelte';
import { uiStore } from '$lib/stores/ui.svelte';
import { notificationsStore } from '$lib/stores/notifications.svelte';
import { flushAllTaskTitleUpdates } from '$lib/editor/useTaskTitleDebounce';
import { DEBOUNCE } from '$lib/models/constants';
import { ApiError } from '$lib/storage/apiClient';

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

	async function flushContentSave() {
		if (saveTimer) { clearTimeout(saveTimer); saveTimer = null; }
		const json = pendingContentJson;
		const pid = pendingContentPageId;
		pendingContentJson = null;
		pendingContentPageId = null;
		if (json != null && pid != null) {
			uiStore.markSaving();
			try {
				await pagesStore.saveContent(pid, json);
				onchange?.();
				uiStore.markSaved();
			} catch (err) {
				uiStore.markSaved();
				if (err instanceof ApiError && err.status === 409) {
					// Someone else (or another client of ours) wrote newer content.
					// Do not retry: reload so the user sees the current document
					// instead of silently overwriting it with our stale copy.
					pagesStore.forgetRevision(pid);
					notificationsStore.error(
						'This note changed elsewhere. Reloading the latest version — your unsaved edits were not applied.'
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
