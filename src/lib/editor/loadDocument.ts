/**
 * Loading stored page content into the editor without ever losing it.
 *
 * The API accepts content the editor schema can't represent (an `image`
 * node, which MCP markdown produces, or a highlight/subscript/superscript
 * mark). TipTap's default is to swallow the "unknown node type" error and
 * load an EMPTY document instead — and an editable empty document gets
 * saved over the note on the first keystroke (DI-01). So content is always
 * loaded with the content check on, and the caller must keep the editor
 * read-only and never save when it fails.
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
	try {
		editor.commands.setContent(content as Record<string, unknown>, { emitUpdate: false, errorOnInvalidContent: true });
		return { ok: true };
	} catch (err) {
		editor.commands.setContent('', { emitUpdate: false });
		return { ok: false, error: toError(err) };
	}
}

/**
 * Whether stored content can be represented in the editor schema — the check
 * the collab service's seeding makes (and fails on). Null when it can.
 */
export function checkStoredContent(content: unknown, schema: Schema = documentSchema()): Error | null {
	if (isEmptyContent(content)) return null;
	try {
		schema.nodeFromJSON(content).check();
		return null;
	} catch (err) {
		return toError(err);
	}
}

function toError(err: unknown): Error {
	const e = err instanceof Error ? err : new Error(String(err));
	return e.cause instanceof Error ? e.cause : e;
}
