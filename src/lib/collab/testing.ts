/**
 * Test helpers: real TipTap editors bound to Y.Docs with the Collaboration
 * extension, wired together in memory so that each editor sees the others'
 * edits exactly as it would from the collab service (as y-prosemirror remote
 * transactions).
 */
import * as Y from 'yjs';
import { Editor, type AnyExtension } from '@tiptap/core';
import Collaboration from '@tiptap/extension-collaboration';
import { documentExtensions, COLLAB_FRAGMENT } from '$lib/editor/schema';
import { NodeIdMapExtension } from '$lib/editor/plugins/NodeIdMapPlugin';
import { CollabNodeIdExtension } from '$lib/editor/plugins/CollabNodeIdExtension';

export const REMOTE = Symbol('remote');

export interface Peer {
	doc: Y.Doc;
	editor: Editor;
}

export function makePeer(extra: AnyExtension[] = [], doc = new Y.Doc()): Peer {
	const editor = new Editor({
		extensions: [
			...documentExtensions({ undoRedo: false }),
			NodeIdMapExtension,
			CollabNodeIdExtension,
			Collaboration.configure({ document: doc, field: COLLAB_FRAGMENT }),
			...extra
		]
	});
	return { doc, editor };
}

/**
 * Connect peers through a hub. When `auto` is false, updates queue until
 * flush() — simulating latency, so peers edit concurrently.
 */
export function network(peers: Peer[], auto = true) {
	const queues = new Map<Peer, Uint8Array[]>(peers.map((p) => [p, []]));
	for (const from of peers) {
		from.doc.on('update', (update: Uint8Array, origin: unknown) => {
			if (origin === REMOTE) return;
			for (const to of peers) {
				if (to === from) continue;
				if (auto) Y.applyUpdate(to.doc, update, REMOTE);
				else queues.get(to)!.push(update);
			}
		});
	}
	return {
		flush() {
			for (const [to, q] of queues) {
				while (q.length) Y.applyUpdate(to.doc, q.shift()!, REMOTE);
			}
		}
	};
}

/** Seed a doc the way the collab service does, and share it with the peers. */
export function seed(peers: Peer[], content: object) {
	const [first, ...rest] = peers;
	first.editor.commands.setContent(content);
	const state = Y.encodeStateAsUpdate(first.doc);
	for (const p of rest) Y.applyUpdate(p.doc, state, REMOTE);
}

export function listItems(editor: Editor): { nodeId: string | null; taskId: string | null; text: string }[] {
	const out: { nodeId: string | null; taskId: string | null; text: string }[] = [];
	editor.state.doc.descendants((node) => {
		if (node.type.name === 'listItem') {
			out.push({ nodeId: node.attrs.nodeId, taskId: node.attrs.taskId, text: node.firstChild?.textContent ?? '' });
		}
	});
	return out;
}

/** Position just inside the paragraph of the list item whose text starts with `prefix`. */
export function posInItem(editor: Editor, prefix: string): number {
	let found = -1;
	editor.state.doc.descendants((node, pos) => {
		if (found !== -1) return false;
		if (node.type.name === 'listItem' && (node.firstChild?.textContent ?? '').startsWith(prefix)) {
			found = pos + 2 + (node.firstChild?.textContent.length ?? 0);
		}
	});
	if (found === -1) throw new Error(`no list item starting with ${prefix}`);
	return found;
}
