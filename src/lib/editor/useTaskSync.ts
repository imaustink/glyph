import type { Editor } from '@tiptap/core';
import type { TaskStatus } from '$lib/models/types';
import { tasksStore } from '$lib/stores/tasks.svelte';
import { debouncedTaskTitleUpdate } from '$lib/editor/useTaskTitleDebounce';
import { STATUS_CYCLE } from '$lib/models/constants';
import type { PendingTaskDetails } from '$lib/editor/useTaskCreation';

export interface TaskSyncHandle {
	/** Handle a status indicator click (cycle task status). */
	handleStatusCycled(nodeId: string, taskId: string, currentStatus: string): Promise<void>;
	/** Full sync of all task statuses into the editor (call after content load). */
	syncTaskStatuses(editor: Editor, pageId: string): void;
	/** Sync external task status changes into the editor (call from $effect). */
	syncExternalStatusChanges(editor: Editor, pageId: string): void;
	/** Sync a linked task title when the bullet text changes (debounced). */
	syncLinkedTaskTitleRealtime(editor: Editor, getPending: () => PendingTaskDetails | null, setPending: (p: PendingTaskDetails | null) => void): void;
}

export function useTaskSync(getEditor: () => Editor | null, getPageId: () => string): TaskSyncHandle {
	let prevTaskStatuses = new Map<string, TaskStatus>();

	async function handleStatusCycled(nodeId: string, taskId: string, currentStatus: string) {
		const newStatus = STATUS_CYCLE[currentStatus as TaskStatus] || 'todo';
		const checked = newStatus === 'done' || newStatus === 'cancelled';
		const editor = getEditor();
		editor?.commands.setCheckedForNode(nodeId, checked);
		editor?.commands.setStatusForNode(nodeId, newStatus);
		await tasksStore.updateTask(taskId, { status: newStatus });
	}

	function syncTaskStatuses(editor: Editor, pageId: string) {
		const pageTasks = tasksStore.tasksByPage.get(pageId) ?? [];
		for (const task of pageTasks) {
			if (!task.sourceNodeId) continue;
			const checked = task.status === 'done' || task.status === 'cancelled';
			editor.commands.setCheckedForNode(task.sourceNodeId, checked);
			editor.commands.setStatusForNode(task.sourceNodeId, task.status);
		}
		prevTaskStatuses = new Map(pageTasks.map((t) => [t.id, t.status]));
	}

	/**
	 * Reactive sync: call from a $effect to push external status changes into the editor.
	 * Returns prevTaskStatuses reference for diffing.
	 */
	function syncExternalStatusChanges(editor: Editor, pageId: string) {
		const pageTasks = tasksStore.tasksByPage.get(pageId) ?? [];
		for (const task of pageTasks) {
			if (!task.sourceNodeId) continue;
			if (prevTaskStatuses.get(task.id) === task.status) continue;
			const checked = task.status === 'done' || task.status === 'cancelled';
			editor.commands.setCheckedForNode(task.sourceNodeId, checked);
			editor.commands.setStatusForNode(task.sourceNodeId, task.status);
		}
		prevTaskStatuses = new Map(pageTasks.map((t) => [t.id, t.status]));
	}

	function syncLinkedTaskTitleRealtime(
		editor: Editor,
		getPending: () => PendingTaskDetails | null,
		setPending: (p: PendingTaskDetails | null) => void
	) {
		const from = editor.state.selection.$from;

		for (let depth = from.depth; depth > 0; depth--) {
			const node = from.node(depth);
			if (node.type.name !== 'listItem') continue;

			const taskId = node.attrs.taskId as string | null;
			const nodeId = node.attrs.nodeId as string | null;
			const bulletText = getListItemText(node);

			const pending = getPending();
			if (pending && nodeId && nodeId === pending.nodeId && pending.bulletText !== bulletText) {
				setPending({ ...pending, bulletText });
			}

			if (!taskId || !bulletText) return;

			const linkedTask = tasksStore.getById(taskId);
			if (!linkedTask || linkedTask.title === bulletText) return;

			debouncedTaskTitleUpdate(taskId, bulletText);
			return;
		}
	}

	return { handleStatusCycled, syncTaskStatuses, syncLinkedTaskTitleRealtime, syncExternalStatusChanges };
}

function getListItemText(node: { forEach: (fn: (child: { type: { name: string }; textContent: string }) => void) => void }): string {
	let text = '';
	node.forEach((child) => {
		if (child.type.name === 'paragraph') {
			text += child.textContent;
		}
	});
	return text.trim();
}
