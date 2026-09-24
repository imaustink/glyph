import type { Transaction } from '@tiptap/pm/state';
import { ySyncPluginKey } from '@tiptap/y-tiptap';

/**
 * Whether a ProseMirror transaction came from this user, as opposed to being
 * y-prosemirror applying someone else's edit (or a Yjs undo/redo, which it
 * also applies from the shared document).
 *
 * Anything with a side effect outside the document — creating a task,
 * pushing a task title, assigning identity to a node — must only run for
 * local transactions. Otherwise every connected client performs the same
 * side effect for one user's edit.
 *
 * Always true in single-writer mode, where the y-sync plugin isn't installed.
 */
export function isLocalTransaction(tr: Transaction): boolean {
	const meta = tr.getMeta(ySyncPluginKey) as { isChangeOrigin?: boolean } | undefined;
	return !meta?.isChangeOrigin;
}
