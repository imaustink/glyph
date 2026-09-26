import type { Editor } from '@tiptap/core';
import type { Schema } from '@tiptap/pm/model';

export type LoadResult = { ok: true } | { ok: false; error: Error };

// Stub: behaviour lands with the fix (DI-01).
export function applyStoredContent(editor: Editor, content: Record<string, unknown> | null | undefined): LoadResult {
	if (content && Object.keys(content).length > 0) {
		editor.commands.setContent(content, { emitUpdate: false });
	} else {
		editor.commands.setContent('', { emitUpdate: false });
	}
	return { ok: true };
}

// Stub: behaviour lands with the fix (DI-01).
export function checkStoredContent(_content: unknown, _schema?: Schema): Error | null {
	return null;
}
