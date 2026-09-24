/**
 * Collaborative-mode bullet identity.
 *
 * A list item's nodeId is what its task is linked by, so two clients must
 * never assign identities to the same node: they would each pick a different
 * random id, one would win, and a task created by the other would point at
 * an id that no longer exists (and the server would soft-delete it).
 *
 * The rule that makes this race-free: a client only ever assigns identity to
 * list items inside ranges it changed itself, in the same transaction batch
 * as the change. Everything that existed when the page was first shared was
 * given an id by the server when it seeded the document.
 *
 *  - A new list item (typed, split with Enter, pasted) gets a fresh nodeId.
 *  - A pasted list item whose nodeId already exists elsewhere in the
 *    document gets a fresh nodeId and loses its task link, so one task stays
 *    linked to one bullet.
 *
 * Remote changes are never touched here. Duplicates that only arise from
 * concurrent edits are fixed by the server, the single actor for those.
 */
import { Extension } from '@tiptap/core';
import { Plugin, PluginKey } from '@tiptap/pm/state';
import { nanoid } from 'nanoid';
import { isLocalTransaction } from '$lib/collab/isLocalTransaction';
import { changedRanges, nodeTouches, type Range } from '$lib/editor/changedRanges';

export const collabNodeIdPluginKey = new PluginKey('collab-node-id');

export const CollabNodeIdExtension = Extension.create({
	name: 'collabNodeId',

	addProseMirrorPlugins() {
		return [
			new Plugin({
				key: collabNodeIdPluginKey,
				appendTransaction(transactions, _oldState, newState) {
					const local = transactions.filter((tr) => tr.docChanged && isLocalTransaction(tr));
					if (local.length === 0) return null;

					// Ranges changed by this batch, mapped onto the final document.
					const ranges: Range[] = [];
					for (let i = 0; i < transactions.length; i++) {
						const tr = transactions[i];
						if (!local.includes(tr)) continue;
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
					}
					if (ranges.length === 0) return null;

					// Every nodeId outside the changed ranges is established identity.
					const established = new Set<string>();
					newState.doc.descendants((node, pos) => {
						if (node.type.name === 'listItem' && node.attrs.nodeId && !nodeTouches(node, pos, ranges)) {
							established.add(node.attrs.nodeId as string);
						}
					});

					const tr = newState.tr;
					const seenInChange = new Set<string>();
					let changed = false;
					newState.doc.descendants((node, pos) => {
						if (node.type.name !== 'listItem' || !nodeTouches(node, pos, ranges)) return;
						const id = node.attrs.nodeId as string | null;
						if (!id) {
							tr.setNodeMarkup(pos, undefined, { ...node.attrs, nodeId: nanoid() });
							changed = true;
						} else if (established.has(id) || seenInChange.has(id)) {
							// Another bullet already has this identity: this one is a copy.
							tr.setNodeMarkup(pos, undefined, {
								...node.attrs,
								nodeId: nanoid(),
								taskId: null,
								checked: false,
								taskStatus: 'todo'
							});
							changed = true;
						} else {
							seenInChange.add(id);
						}
					});
					return changed ? tr.setMeta('addToHistory', false) : null;
				}
			})
		];
	}
});
