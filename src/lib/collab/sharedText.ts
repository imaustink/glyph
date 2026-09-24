import * as Y from 'yjs';
import type { Schema } from '@tiptap/pm/model';

/**
 * Give every empty text block (paragraph, heading, code block…) in a shared
 * document an empty Y.XmlText child.
 *
 * y-prosemirror represents an empty paragraph as an element with no text
 * node, and creates one on the first keystroke. If two people type the first
 * character into the same empty paragraph at the same time, each creates
 * their own text node; y-prosemirror later merges them by moving one
 * person's characters into the other's node, which misplaces that person's
 * cursor — their next keystrokes land before their first one ("lice… A").
 * With a text node already present, both type into the same Y.Text and Yjs
 * keeps each person's run of characters intact.
 *
 * Only the server applies this (when seeding), so there is one text node per
 * block rather than one per client.
 */
export function addSharedTextNodes(fragment: Y.XmlFragment, schema: Schema): number {
	let added = 0;
	const visit = (el: Y.XmlFragment | Y.XmlElement) => {
		for (const child of el.toArray()) {
			if (!(child instanceof Y.XmlElement)) continue;
			const type = schema.nodes[child.nodeName];
			if (type?.isTextblock) {
				if (child.length === 0) {
					child.insert(0, [new Y.XmlText()]);
					added++;
				}
			} else {
				visit(child);
			}
		}
	};
	visit(fragment);
	return added;
}
