/**
 * DI-01: the API accepts content the editor schema can't represent (an
 * `image` node — which MCP markdown produces — or a highlight/subscript/
 * superscript mark). TipTap's setContent swallowed the "unknown node type"
 * error and loaded an EMPTY document into an editable editor; the first
 * keystroke then saved the empty doc over the note (and the server
 * soft-deleted every task on it).
 *
 * Loading must detect this so the editor can stay read-only and never save.
 */
import { describe, it, expect, afterEach } from 'vitest';
import { Editor } from '@tiptap/core';
import { documentExtensions } from '$lib/editor/schema';
import { applyStoredContent, checkStoredContent } from './loadDocument';

const editors: Editor[] = [];
afterEach(() => {
	for (const e of editors.splice(0)) e.destroy();
	document.body.innerHTML = '';
});

function makeEditor() {
	const mount = document.createElement('div');
	document.body.appendChild(mount);
	const editor = new Editor({ element: mount, extensions: documentExtensions(), content: '' });
	editors.push(editor);
	return editor;
}

const para = (text: string, marks?: unknown[]) => ({
	type: 'paragraph',
	content: [{ type: 'text', text, ...(marks ? { marks } : {}) }]
});

const valid = { type: 'doc', content: [para('Hello')] };
const withImage = {
	type: 'doc',
	content: [para('Before'), { type: 'image', attrs: { src: 'https://example.com/a.png', alt: 'a' } }, para('After')]
};
const withHighlight = { type: 'doc', content: [para('marked', [{ type: 'highlight' }])] };
const withBadNesting = { type: 'doc', content: [{ type: 'text', text: 'bare text in doc' }] };

describe('applyStoredContent (DI-01)', () => {
	it('loads valid content', () => {
		const editor = makeEditor();
		expect(applyStoredContent(editor, valid)).toEqual({ ok: true });
		expect(editor.getText()).toBe('Hello');
	});

	it('treats missing or empty content as an empty note', () => {
		const editor = makeEditor();
		expect(applyStoredContent(editor, null).ok).toBe(true);
		expect(applyStoredContent(editor, {}).ok).toBe(true);
		expect(editor.getText()).toBe('');
	});

	it('reports content with a node the schema lacks (image) instead of loading a blank doc', () => {
		const editor = makeEditor();
		const result = applyStoredContent(editor, withImage);
		expect(result.ok).toBe(false);
	});

	it('reports content with a mark the schema lacks (highlight)', () => {
		const editor = makeEditor();
		expect(applyStoredContent(editor, withHighlight).ok).toBe(false);
	});

	it('reports structurally invalid content', () => {
		const editor = makeEditor();
		expect(applyStoredContent(editor, withBadNesting).ok).toBe(false);
	});

	it('does not leave the previous document in the editor after a failed load', () => {
		const editor = makeEditor();
		applyStoredContent(editor, valid);
		applyStoredContent(editor, withImage);
		expect(editor.getText()).toBe('');
	});
});

describe('checkStoredContent (DI-01, collaborative seeding)', () => {
	it('accepts valid and empty content', () => {
		expect(checkStoredContent(valid)).toBeNull();
		expect(checkStoredContent(null)).toBeNull();
		expect(checkStoredContent({})).toBeNull();
	});

	it('rejects content the collab service could not seed', () => {
		expect(checkStoredContent(withImage)).toBeInstanceOf(Error);
		expect(checkStoredContent(withHighlight)).toBeInstanceOf(Error);
		expect(checkStoredContent(withBadNesting)).toBeInstanceOf(Error);
	});
});
