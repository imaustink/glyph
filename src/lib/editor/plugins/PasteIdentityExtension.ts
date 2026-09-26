/**
 * Bullet identity for pasted and dropped content, in every editing mode.
 *
 * Copied bullets carry their `data-node-id` and `data-task-id`
 * (TaskLinkExtension parses both back), so without this a paste could make
 * two bullets share one task, or bring another note's task link along — a
 * dangling link, since tasks are reconciled per source page (DI-10).
 *
 * For every list item a paste or drop inserted:
 *  - a nodeId that another bullet already has (outside the pasted range, or
 *    earlier in it) is a copy: fresh nodeId, task link dropped;
 *  - a task link that isn't one of this page's tasks (per the
 *    `taskBelongsHere` option), or that another bullet already has, is
 *    dropped, with a fresh nodeId (the old one names the other bullet's
 *    task). The bullet is then detected as a new TODO bullet if it is one.
 *  - A bullet cut and pasted back into the same note keeps both: its old
 *    copy is gone by then, and its task is this page's.
 *
 * Only paste/drop transactions are looked at: an undo or a remote change
 * brings back genuine identity. (CollabNodeIdExtension additionally assigns
 * ids to every local change in collaborative mode.)
 */
import { Extension } from '@tiptap/core';
import { Plugin, PluginKey, type Transaction } from '@tiptap/pm/state';
import { nanoid } from 'nanoid';
import { isLocalTransaction } from '$lib/collab/isLocalTransaction';
import { changedRanges, nodeTouches, type Range } from '$lib/editor/changedRanges';

export interface PasteIdentityOptions {
	/** Whether a task id seen in pasted content is one of this page's tasks. */
	taskBelongsHere: (taskId: string) => boolean;
}

export const pasteIdentityPluginKey = new PluginKey('paste-identity');

function isPasteOrDrop(tr: Transaction): boolean {
	const ev = tr.getMeta('uiEvent');
	return (ev === 'paste' || ev === 'drop') && tr.docChanged && isLocalTransaction(tr);
}

export const PasteIdentityExtension = Extension.create<PasteIdentityOptions>({
	name: 'pasteIdentity',

	addOptions() {
		return { taskBelongsHere: () => false };
	},

	addProseMirrorPlugins() {
		const options = this.options;
		return [
			new Plugin({
				key: pasteIdentityPluginKey,
				appendTransaction(transactions, _oldState, newState) {
					if (!transactions.some(isPasteOrDrop)) return null;

					// Ranges the paste/drop inserted, mapped onto the final document.
					const ranges: Range[] = [];
					transactions.forEach((tr, i) => {
						if (!isPasteOrDrop(tr)) return;
						const after = transactions.slice(i + 1);
						for (const [from, to] of changedRanges(tr)) {
							let f = from;
							let t = to;
							for (const later of after) {
								f = later.mapping.map(f, -1);
								t = later.mapping.map(t, 1);
							}
							ranges.push([f, t]);
						}
					});
					if (ranges.length === 0) return null;

					const establishedNodeIds = new Set<string>();
					const establishedTaskIds = new Set<string>();
					newState.doc.descendants((node, pos) => {
						if (node.type.name !== 'listItem' || nodeTouches(node, pos, ranges)) return;
						if (node.attrs.nodeId) establishedNodeIds.add(node.attrs.nodeId as string);
						if (node.attrs.taskId) establishedTaskIds.add(node.attrs.taskId as string);
					});

					const tr = newState.tr;
					const seenNodeIds = new Set<string>();
					const seenTaskIds = new Set<string>();
					let changed = false;
					newState.doc.descendants((node, pos) => {
						if (node.type.name !== 'listItem' || !nodeTouches(node, pos, ranges)) return;
						const nodeId = node.attrs.nodeId as string | null;
						const taskId = node.attrs.taskId as string | null;
						const copiedNode = !!nodeId && (establishedNodeIds.has(nodeId) || seenNodeIds.has(nodeId));
						const foreignTask =
							!!taskId &&
							(establishedTaskIds.has(taskId) || seenTaskIds.has(taskId) || !options.taskBelongsHere(taskId));
						if (copiedNode || foreignTask) {
							tr.setNodeMarkup(pos, undefined, {
								...node.attrs,
								nodeId: nanoid(),
								taskId: null,
								checked: false,
								taskStatus: 'todo'
							});
							changed = true;
							return;
						}
						if (nodeId) seenNodeIds.add(nodeId);
						if (taskId) seenTaskIds.add(taskId);
					});
					return changed ? tr.setMeta('addToHistory', false) : null;
				}
			})
		];
	}
});
