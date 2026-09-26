import type { Editor } from '@tiptap/core';
import type { DetectedBullet } from '$lib/editor/extensions/TodoDetectionExtension';
import { tasksStore } from '$lib/stores/tasks.svelte';
import { uiStore } from '$lib/stores/ui.svelte';
import { notificationsStore } from '$lib/stores/notifications.svelte';
import { isCheckedStatus, shouldCreateTaskFor } from '$lib/editor/todoDerivation';

export interface PendingTaskDetails {
	taskId: string;
	nodeId: string;
	pageId: string;
	bulletText: string;
}

/** A bullet pasted with another note's task (see PasteIdentityExtension). */
export interface PastedTaskMove {
	/** The pasted bullet's (new) nodeId. */
	nodeId: string;
	/** The task it carried. */
	taskId: string;
	/**
	 * An editor in this tab saw that task's bullet leave its note: this is
	 * most likely a move (cut → paste), so keep asking while the server hasn't
	 * seen the cut yet.
	 */
	cut: boolean;
}

/**
 * Waits between asking the server again to move a cut bullet's task. The
 * server refuses while the task's bullet is still in its note's stored
 * content, and a cut reaches it with the note's next save: at once in
 * single-writer mode (leaving a note flushes its save), but in a
 * collaborative note with the collab service's next snapshot — up to its
 * 10 s debounce ceiling later. About 15 s in all.
 */
export const ADOPT_RETRY_DELAYS_MS = [500, 1000, 2000, 4000, 8000];

export interface TaskCreationHandle {
	/** Process detected TODO bullets by queuing task creation. */
	handleTodoBulletsDetected(bullets: DetectedBullet[]): void;
	/**
	 * Bullets pasted into page `pageId` with another note's task: ask to move
	 * each task onto its bullet, and hold back creating a new task for the
	 * bullet until that has settled — a new one only if the move is refused.
	 * Only this client's own paste ever gets here, so in a collaborative note
	 * one client asks, and the task link it then writes is an ordinary local
	 * edit.
	 */
	adoptPasted(moves: PastedTaskMove[], pageId: string): void;
	/** Get the current pending task details (for popover display). */
	getPending(): PendingTaskDetails | null;
	/** Set pending task details (used when title changes via popover). */
	setPending(p: PendingTaskDetails | null): void;
	/** Clear the prompted nodeIds set (call on page change). */
	clearPrompted(): void;
}

export interface TaskCreationOptions {
	getEditor: () => Editor | null;
	/** Called whenever the pending task details change (set or cleared). */
	onPendingChange?: (pending: PendingTaskDetails | null) => void;
}

export function useTaskCreation(
	getEditor: () => Editor | null,
	onPendingChange?: (p: PendingTaskDetails | null) => void,
	/**
	 * Returns the page whose document is currently loaded in the editor, or null
	 * while a load is in flight. Used to drop async continuations that would
	 * otherwise mutate a document belonging to a different page.
	 */
	getLoadedPageId?: () => string | null
): TaskCreationHandle {
	let pending: PendingTaskDetails | null = null;
	const promptedNodeIds = new Set<string>();
	let taskCreationQueue: Promise<string | null> = Promise.resolve(null);
	/**
	 * Pasted bullets whose task is being moved onto them, by nodeId, with the
	 * bullet as task detection last saw it (to create a task from, if the
	 * move is refused).
	 */
	const adopting = new Map<string, { latest: DetectedBullet | null }>();

	function notifyPendingChange() {
		onPendingChange?.(pending);
	}

	function handleTodoBulletsDetected(bullets: DetectedBullet[]) {
		for (const bullet of bullets) {
			const move = adopting.get(bullet.nodeId);
			if (move) {
				move.latest = bullet;
				continue;
			}
			taskCreationQueue = taskCreationQueue
				.then(() => doCreateTask(bullet))
				.catch((err) => {
					console.error('[Editor] Task creation queue error:', err);
					notificationsStore.error('Failed to create task from TODO bullet.');
					return null;
				});
		}
	}

	async function doCreateTask(params: DetectedBullet): Promise<string | null> {
		const title = params.bulletText.trim();

		const existing = tasksStore.getByNodeId(params.nodeId);
		// A task already exists for this bullet — typically one whose creation
		// finished after the user navigated away, so it was never written back
		// (see the guard below). Link the bullet to it instead of leaving it
		// unlinked forever (DI-28). Only this page's task, though: a nodeId
		// seen on another page is a copy, not this bullet's identity.
		if (existing && existing.sourcePageId === params.pageId) {
			if (getLoadedPageId && getLoadedPageId() !== params.pageId) return existing.id;
			const commands = getEditor()?.commands;
			if (commands) {
				commands.setTaskIdForNode(params.nodeId, existing.id);
				commands.setCheckedForNode(params.nodeId, isCheckedStatus(existing.status));
				commands.setStatusForNode(params.nodeId, existing.status);
			}
			return existing.id;
		}

		// An empty bullet the user isn't typing in stays a plain bullet until it
		// has text (it is detected again then). Matches Go (DI-27).
		if (!shouldCreateTaskFor(params)) return null;

		if (promptedNodeIds.has(params.nodeId)) return null;
		promptedNodeIds.add(params.nodeId);

		try {
			uiStore.markSaving();
			const task = await tasksStore.createTask({
				title,
				sourcePageId: params.pageId,
				sourceNodeId: params.nodeId
			});

			// The await above may have spanned a page navigation. Mutating the
			// document now would dispatch a transaction against whatever page is
			// currently loaded, and the editor's onUpdate would persist it — the
			// stale-document write pattern behind the 2026-09-21 incident. Only
			// touch the document if it is still the one this task came from.
			//
			// Note the explicit callback check rather than a `?? params.pageId`
			// fallback: a *null* return means "a page load is in flight", which
			// must also skip the mutation. Only an absent callback opts out.
			if (getLoadedPageId && getLoadedPageId() !== params.pageId) {
				uiStore.markSaved();
				return task.id;
			}

			getEditor()?.commands.setTaskIdForNode(params.nodeId, task.id);
			pending = {
				taskId: task.id,
				nodeId: params.nodeId,
				pageId: params.pageId,
				bulletText: title
			};
			notifyPendingChange();

			uiStore.markSaved();
			return task.id;
		} catch (err) {
			console.error('[Editor] Failed to create task from TODO bullet:', err);
			promptedNodeIds.delete(params.nodeId);
			uiStore.markSaved();
			return null;
		}
	}

	function adoptPasted(moves: PastedTaskMove[], pageId: string) {
		for (const move of moves) {
			const entry = { latest: null as DetectedBullet | null };
			adopting.set(move.nodeId, entry);
			void adoptOne(move, pageId).then((moved) => {
				if (adopting.get(move.nodeId) === entry) adopting.delete(move.nodeId);
				// Refused: the bullet is a new TODO bullet after all.
				if (!moved && entry.latest) handleTodoBulletsDetected([entry.latest]);
			});
		}
	}

	/**
	 * Whether page `pageId` is loaded and no longer has the bullet `nodeId`
	 * (the paste was undone, or the bullet deleted). With another page
	 * loaded, keep going: a task moved meanwhile is linked when the page is
	 * next opened.
	 */
	function bulletGone(pageId: string, nodeId: string): boolean {
		if (getLoadedPageId && getLoadedPageId() !== pageId) return false;
		const doc = getEditor()?.state?.doc;
		if (!doc) return false;
		let found = false;
		doc.descendants((node) => {
			if (found) return false;
			if (node.type.name === 'listItem' && node.attrs.nodeId === nodeId) found = true;
			return !found;
		});
		return !found;
	}

	/** Move the task onto the pasted bullet and link it. Whether it moved. */
	async function adoptOne(move: PastedTaskMove, pageId: string): Promise<boolean> {
		const delays = move.cut ? ADOPT_RETRY_DELAYS_MS : [];
		for (let attempt = 0; ; attempt++) {
			let result: Awaited<ReturnType<typeof tasksStore.adoptTask>>;
			try {
				result = await tasksStore.adoptTask(move.taskId, { sourcePageId: pageId, sourceNodeId: move.nodeId });
			} catch (err) {
				console.warn('[Editor] Could not move a pasted bullet’s task; will retry', { taskId: move.taskId }, err);
				result = { kind: 'live' };
			}
			if (result.kind === 'moved') {
				// Link the bullet — only if the document is still this page's
				// (see doCreateTask). Otherwise the next load links it: the task
				// is now this bullet's (DI-28 path in doCreateTask).
				if (!getLoadedPageId || getLoadedPageId() === pageId) {
					const commands = getEditor()?.commands;
					if (commands) {
						commands.setTaskIdForNode(move.nodeId, result.task.id);
						commands.setCheckedForNode(move.nodeId, isCheckedStatus(result.task.status));
						commands.setStatusForNode(move.nodeId, result.task.status);
					}
				}
				return true;
			}
			if (result.kind === 'refused' || attempt >= delays.length) return false;
			await new Promise((r) => setTimeout(r, delays[attempt]));
			// The paste was undone: nothing to move the task to, or create one for.
			if (bulletGone(pageId, move.nodeId)) return true;
		}
	}

	function getPending() { return pending; }
	function setPending(p: PendingTaskDetails | null) {
		pending = p;
		notifyPendingChange();
	}
	function clearPrompted() { promptedNodeIds.clear(); }

	return { handleTodoBulletsDetected, adoptPasted, getPending, setPending, clearPrompted };
}
