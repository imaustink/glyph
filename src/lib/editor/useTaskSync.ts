import type { Editor } from '@tiptap/core';
import type { Transaction } from '@tiptap/pm/state';
import type { TaskStatus } from '$lib/models/types';
import { tasksStore } from '$lib/stores/tasks.svelte';
import { debouncedTaskTitleUpdate } from '$lib/editor/useTaskTitleDebounce';
import { STATUS_CYCLE } from '$lib/models/constants';
import type { PendingTaskDetails } from '$lib/editor/useTaskCreation';
import { isCheckedStatus } from '$lib/editor/todoDerivation';
import { changedRanges } from '$lib/editor/changedRanges';

export interface TaskSyncHandle {
	/** Handle a status indicator click (cycle task status). */
	handleStatusCycled(nodeId: string, taskId: string, currentStatus: string): Promise<void>;
	/** Full sync of all task statuses into the editor (call after content load). */
	syncTaskStatuses(editor: Editor, pageId: string): void;
	/** Sync external task status changes into the editor (call from $effect). */
	syncExternalStatusChanges(editor: Editor, pageId: string): void;
	/**
	 * Sync linked task titles when bullet text changes (debounced): the
	 * bullet under the cursor, and every linked bullet `transaction` changed.
	 */
	syncLinkedTaskTitleRealtime(
		editor: Editor,
		getPending: () => PendingTaskDetails | null,
		setPending: (p: PendingTaskDetails | null) => void,
		transaction?: Transaction
	): void;
	/** Forget per-page task status memory (call when switching pages). */
	resetStatusMemory(): void;
}

export function useTaskSync(getEditor: () => Editor | null, getPageId: () => string): TaskSyncHandle {
	let prevTaskStatuses = new Map<string, TaskStatus>();

	/**
	 * Drop the per-page status memory. Must be called when the editor switches
	 * pages: otherwise the incoming page's tasks all look "changed" relative to
	 * the previous page's snapshot and syncExternalStatusChanges dispatches a
	 * burst of transactions against a document that may not be loaded yet.
	 */
	function resetStatusMemory() {
		prevTaskStatuses = new Map();
	}

	async function handleStatusCycled(nodeId: string, taskId: string, currentStatus: string) {
		const newStatus = STATUS_CYCLE[currentStatus as TaskStatus] || 'todo';
		const checked = isCheckedStatus(newStatus);
		const editor = getEditor();
		editor?.commands.setCheckedForNode(nodeId, checked);
		editor?.commands.setStatusForNode(nodeId, newStatus);
		await tasksStore.updateTask(taskId, { status: newStatus });
	}

	function syncTaskStatuses(editor: Editor, pageId: string) {
		const pageTasks = tasksStore.tasksByPage.get(pageId) ?? [];
		for (const task of pageTasks) {
			if (!task.sourceNodeId) continue;
			const checked = isCheckedStatus(task.status);
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
			const checked = isCheckedStatus(task.status);
			editor.commands.setCheckedForNode(task.sourceNodeId, checked);
			editor.commands.setStatusForNode(task.sourceNodeId, task.status);
		}
		prevTaskStatuses = new Map(pageTasks.map((t) => [t.id, t.status]));
	}

	function pushTitle(taskId: string | null, bulletText: string) {
		if (!taskId || !bulletText) return;
		const linkedTask = tasksStore.getById(taskId);
		if (!linkedTask || linkedTask.title === bulletText) return;
		debouncedTaskTitleUpdate(taskId, bulletText);
	}

	function syncLinkedTaskTitleRealtime(
		editor: Editor,
		getPending: () => PendingTaskDetails | null,
		setPending: (p: PendingTaskDetails | null) => void,
		transaction?: Transaction
	) {
		const from = editor.state.selection.$from;
		let cursorNodeId: string | null = null;

		for (let depth = from.depth; depth > 0; depth--) {
			const node = from.node(depth);
			if (node.type.name !== 'listItem') continue;

			const taskId = node.attrs.taskId as string | null;
			const nodeId = node.attrs.nodeId as string | null;
			const bulletText = getListItemText(node);
			cursorNodeId = nodeId;

			const pending = getPending();
			if (pending && nodeId && nodeId === pending.nodeId && pending.bulletText !== bulletText) {
				setPending({ ...pending, bulletText });
			}

			pushTitle(taskId, bulletText);
			break;
		}

		// The edit may also have changed linked bullets the cursor isn't in:
		// splitting "Buy milk" in the middle leaves the task on "Buy" with the
		// cursor below it (DI-09); joining two bullets, or a paste, likewise.
		if (!transaction?.docChanged) return;
		const doc = editor.state.doc;
		for (const [rangeFrom, rangeTo] of changedRanges(transaction)) {
			doc.nodesBetween(Math.max(0, rangeFrom), Math.min(doc.content.size, rangeTo), (node) => {
				if (node.type.name !== 'listItem') return true;
				const nodeId = node.attrs.nodeId as string | null;
				if (nodeId !== cursorNodeId) pushTitle(node.attrs.taskId as string | null, getListItemText(node));
				return true;
			});
		}
	}

	return { handleStatusCycled, syncTaskStatuses, syncLinkedTaskTitleRealtime, syncExternalStatusChanges, resetStatusMemory };
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
