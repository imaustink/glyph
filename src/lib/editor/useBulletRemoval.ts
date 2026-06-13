import type { Editor } from '@tiptap/core';
import { tasksStore } from '$lib/stores/tasks.svelte';

export interface BulletRemovalHandle {
	/** Snapshot the current task-linked nodes (call after content load). */
	snapshot(editor: Editor): void;
	/** Detect removed task-linked bullets and delete their tasks (debounced externally). */
	detectRemovedTaskBullets(editor: Editor): Promise<void>;
	/** Clean up timers. */
	destroy(): void;
}

export function useBulletRemoval(): BulletRemovalHandle {
	let knownTaskNodeIds = new Map<string, string>(); // nodeId → taskId
	let removedBulletTimer: ReturnType<typeof setTimeout> | null = null;

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

	async function detectRemovedTaskBullets(editor: Editor) {
		const current = collectTaskNodeIds(editor);
		for (const [nodeId, taskId] of knownTaskNodeIds) {
			if (!current.has(nodeId)) {
				try {
					await tasksStore.deleteTask(taskId);
				} catch (err) {
					console.error('[Editor] Failed to delete task for removed bullet:', { nodeId, taskId }, err);
				}
			}
		}
		knownTaskNodeIds = current;
	}

	function destroy() {
		if (removedBulletTimer) clearTimeout(removedBulletTimer);
	}

	return { snapshot, detectRemovedTaskBullets, destroy };
}
