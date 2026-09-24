import type { Transaction } from '@tiptap/pm/state';
import type { Node as ProseMirrorNode } from '@tiptap/pm/model';

export type Range = readonly [from: number, to: number];

/**
 * The ranges of `tr.doc` (the document after the transaction) that the
 * transaction replaced or inserted. Attribute-only steps have no range.
 */
export function changedRanges(tr: Transaction): Range[] {
	const ranges: Range[] = [];
	tr.mapping.maps.forEach((map, i) => {
		const rest = tr.mapping.slice(i + 1);
		map.forEach((_oldStart, _oldEnd, newStart, newEnd) => {
			ranges.push([rest.map(newStart, -1), rest.map(newEnd, 1)]);
		});
	});
	return ranges;
}

/**
 * Whether the node at `pos` overlaps any of the ranges. Strict: a range that
 * merely ends where the node starts (content inserted right before it) does
 * not count, so a paste next to a bullet doesn't make that bullet "changed".
 */
export function nodeTouches(node: ProseMirrorNode, pos: number, ranges: readonly Range[]): boolean {
	const end = pos + node.nodeSize;
	return ranges.some(([from, to]) => from < end && to > pos);
}
