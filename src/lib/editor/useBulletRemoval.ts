import type { Editor } from '@tiptap/core';
import { tasksStore } from '$lib/stores/tasks.svelte';

export interface BulletRemovalHandle {
	/** Snapshot the current task-linked nodes (call after content load). */
	snapshot(editor: Editor): void;
	/** Detect removed (and re-added) task-linked bullets (debounced externally). */
	detectRemovedTaskBullets(editor: Editor): Promise<void>;
	/**
	 * localStorage mode: delete from storage the tasks whose bullets are still
	 * gone. Call when the editor leaves the page (and on unload). No-op in API
	 * mode, where the server does this.
	 */
	flush(): Promise<void>;
	/**
	 * The source page of a task whose bullet this editor saw removed (its task
	 * may no longer be in local state), or undefined if it saw none.
	 */
	sourcePageOfRemoved(taskId: string): string | undefined;
	/** Clean up timers. */
	destroy(): void;
}

export interface BulletRemovalOptions {
	/**
	 * True when the server reconciles tasks with the saved document (API
	 * mode): it soft-deletes a task whose bullet is gone and restores it when
	 * the bullet comes back. The client then only mirrors that in its local
	 * view and never deletes anything itself — with several editors, a
	 * client-side delete fires from every connected client, including for a
	 * cut/paste or an undo.
	 *
	 * False (localStorage mode): this client is the only writer, so it deletes
	 * the task itself — softly. The task is hidden at once and deleted from
	 * storage only by flush() (when the editor leaves the page); a bullet that
	 * comes back first (paste after a cut, undo) gets its task back (DI-10).
	 */
	serverReconciles?: boolean;
}

/** Back-off for re-reading a task whose bullet reappeared (e.g. undo). */
const REFRESH_DELAYS_MS = [2000, 5000, 12000];

export function useBulletRemoval(options: BulletRemovalOptions = {}): BulletRemovalHandle {
	let knownTaskNodeIds = new Map<string, string>(); // nodeId → taskId
	const refreshTimers = new Set<ReturnType<typeof setTimeout>>();
	let destroyed = false;
	/** localStorage mode: removed bullets' tasks awaiting deletion (taskId → nodeId). */
	const pendingDeletes = new Map<string, string>();
	/** taskId → source page, for tasks whose bullet was removed. */
	const removedFrom = new Map<string, string>();

	function collectTaskNodeIds(editor: Editor): Map<string, string> {
		const map = new Map<string, string>();
		editor.state.doc.descendants((node) => {
			if (node.type.name === 'listItem') {
				if (node.attrs.taskId && node.attrs.nodeId) {
					map.set(node.attrs.nodeId as string, node.attrs.taskId as string);
				}
				return true;
			}
			if (node.type.name === 'paragraph' || node.type.name === 'heading' || node.type.name === 'codeBlock') {
				return false;
			}
			return true;
		});
		return map;
	}

	function snapshot(editor: Editor) {
		knownTaskNodeIds = collectTaskNodeIds(editor);
	}

	/**
	 * The server restores a task once it has saved a document containing its
	 * bullet again, which happens some time after this client sees the bullet
	 * return. Poll a few times rather than guessing when that save lands.
	 */
	function scheduleRefresh(taskId: string, attempt = 0) {
		if (destroyed || attempt >= REFRESH_DELAYS_MS.length) return;
		const timer = setTimeout(async () => {
			refreshTimers.delete(timer);
			if (destroyed || tasksStore.getById(taskId)) return;
			try {
				if (await tasksStore.refreshTask(taskId)) return;
			} catch (err) {
				console.warn('[Editor] Failed to refresh task for restored bullet:', { taskId }, err);
			}
			scheduleRefresh(taskId, attempt + 1);
		}, REFRESH_DELAYS_MS[attempt]);
		refreshTimers.add(timer);
	}

	async function detectRemovedTaskBullets(editor: Editor) {
		const current = collectTaskNodeIds(editor);
		const removed: [string, string][] = [];
		for (const [nodeId, taskId] of knownTaskNodeIds) {
			if (!current.has(nodeId)) removed.push([nodeId, taskId]);
		}
		const reappeared: string[] = [];
		for (const [nodeId, taskId] of current) {
			if (!knownTaskNodeIds.has(nodeId) && !tasksStore.getById(taskId)) reappeared.push(taskId);
		}
		knownTaskNodeIds = current;

		for (const [, taskId] of removed) {
			const sourcePageId = tasksStore.getById(taskId)?.sourcePageId;
			if (sourcePageId) removedFrom.set(taskId, sourcePageId);
		}
		tasksStore.forgetLocal(removed.map(([, taskId]) => taskId));

		if (options.serverReconciles) {
			for (const taskId of reappeared) scheduleRefresh(taskId);
			return;
		}

		for (const [nodeId, taskId] of removed) pendingDeletes.set(taskId, nodeId);
		for (const taskId of reappeared) {
			if (!pendingDeletes.delete(taskId)) continue;
			try {
				// Still in storage: bring it back into local state.
				await tasksStore.refreshTask(taskId);
			} catch (err) {
				console.warn('[Editor] Failed to restore task for returned bullet:', { taskId }, err);
			}
		}
	}

	async function flush() {
		if (options.serverReconciles) return;
		const doomed = [...pendingDeletes];
		pendingDeletes.clear();
		for (const [taskId, nodeId] of doomed) {
			try {
				await tasksStore.deleteTask(taskId);
			} catch (err) {
				console.error('[Editor] Failed to delete task for removed bullet:', { nodeId, taskId }, err);
			}
		}
	}

	function sourcePageOfRemoved(taskId: string): string | undefined {
		return removedFrom.get(taskId);
	}

	function destroy() {
		destroyed = true;
		for (const t of refreshTimers) clearTimeout(t);
		refreshTimers.clear();
	}

	return { snapshot, detectRemovedTaskBullets, flush, sourcePageOfRemoved, destroy };
}
