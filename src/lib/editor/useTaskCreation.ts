import type { Editor } from '@tiptap/core';
import type { DetectedBullet } from '$lib/editor/extensions/TodoDetectionExtension';
import { tasksStore } from '$lib/stores/tasks.svelte';
import { uiStore } from '$lib/stores/ui.svelte';
import { notificationsStore } from '$lib/stores/notifications.svelte';

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

export function useTaskCreation(getEditor: () => Editor | null, onPendingChange?: (p: PendingTaskDetails | null) => void): TaskCreationHandle {
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
		if (existing) return existing.id;

		if (promptedNodeIds.has(params.nodeId)) return null;
		promptedNodeIds.add(params.nodeId);

		try {
			uiStore.markSaving();
			const task = await tasksStore.createTask({
				title,
				sourcePageId: params.pageId,
				sourceNodeId: params.nodeId
			});

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
