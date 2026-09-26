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

export interface TaskCreationHandle {
	/** Process detected TODO bullets by queuing task creation. */
	handleTodoBulletsDetected(bullets: DetectedBullet[]): void;
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

	function notifyPendingChange() {
		onPendingChange?.(pending);
	}

	function handleTodoBulletsDetected(bullets: DetectedBullet[]) {
		for (const bullet of bullets) {
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

	function getPending() { return pending; }
	function setPending(p: PendingTaskDetails | null) {
		pending = p;
		notifyPendingChange();
	}
	function clearPrompted() { promptedNodeIds.clear(); }

	return { handleTodoBulletsDetected, getPending, setPending, clearPrompted };
}
