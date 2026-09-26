/**
 * Loading stored page content into the editor without ever losing it.
 *
 * The API accepts content the editor schema can't represent (an `image`
 * node, which MCP markdown produces, or a highlight/subscript/superscript
 * mark). TipTap's default is to swallow the "unknown node type" error and
 * load an EMPTY document instead — and an editable empty document gets
 * saved over the note on the first keystroke (DI-01). So content is always
 * parsed against the schema first, and the caller must keep the editor
 * read-only and never save when that fails.
 *
 * Only unknown node and mark types fail. A violation of the content
 * expressions (e.g. a listItem that doesn't start with a paragraph) parses
 * and renders without losing anything, and concurrent collaborative edits
 * can produce one legitimately — so it loads, as it always has, rather than
 * locking the note. (TipTap's errorOnInvalidContent would run node.check()
 * and reject those too.)
 */
import type { Editor } from '@tiptap/core';
import type { Schema } from '@tiptap/pm/model';
import { documentSchema } from '$lib/editor/schema';

export type LoadResult = { ok: true } | { ok: false; error: Error };

function isEmptyContent(content: unknown): boolean {
	return content == null || (typeof content === 'object' && Object.keys(content as object).length === 0);
}

/**
 * Replace the editor's document with stored content, without emitting an
 * update. On failure the editor is left holding an empty document (not the
 * previous page's) and the error is returned; nothing is thrown.
 */
export function applyStoredContent(editor: Editor, content: Record<string, unknown> | null | undefined): LoadResult {
	if (isEmptyContent(content)) {
		editor.commands.setContent('', { emitUpdate: false });
		return { ok: true };
	}
	const error = checkStoredContent(content, editor.schema);
	if (error) {
		editor.commands.setContent('', { emitUpdate: false });
		return { ok: false, error };
	}
	editor.commands.setContent(content as Record<string, unknown>, { emitUpdate: false });
	return { ok: true };
}

/**
 * Whether stored content can be represented in the editor schema — every
 * node and mark type exists — which is also what the collab service's
 * seeding needs. Null when it can.
 */
export function checkStoredContent(content: unknown, schema: Schema = documentSchema()): Error | null {
	if (isEmptyContent(content)) return null;
	try {
		schema.nodeFromJSON(content);
		return null;
	} catch (err) {
		return toError(err);
	}
}

function toError(err: unknown): Error {
	const e = err instanceof Error ? err : new Error(String(err));
	return e.cause instanceof Error ? e.cause : e;
}
