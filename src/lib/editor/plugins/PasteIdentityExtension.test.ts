/**
 * DI-10: pasted bullets keep the `data-node-id` / `data-task-id` they were
 * copied with (TaskLinkExtension parses them). Only the collaborative editor
 * deduplicated them, so with collab off a copy/paste made two bullets share
 * one task, and a bullet moved from another note kept that note's taskId —
 * a dangling link, since tasks are reconciled per source page.
 *
 * PasteIdentityExtension runs in every mode.
 */
import { describe, it, expect, afterEach } from 'vitest';
import { Editor } from '@tiptap/core';
import { documentExtensions } from '$lib/editor/schema';
import { NodeIdMapExtension } from '$lib/editor/plugins/NodeIdMapPlugin';
import { PasteIdentityExtension } from './PasteIdentityExtension';

const editors: Editor[] = [];
afterEach(() => {
	for (const e of editors.splice(0)) e.destroy();
	document.body.innerHTML = '';
});

const PAGE_TASKS = new Set(['t-milk']);

function makeEditor(items: { nodeId: string; taskId: string | null; text: string }[]) {
	const mount = document.createElement('div');
	document.body.appendChild(mount);
	const editor = new Editor({
		element: mount,
		extensions: [
			...documentExtensions(),
			NodeIdMapExtension,
			PasteIdentityExtension.configure({ taskBelongsHere: (id) => PAGE_TASKS.has(id) })
		],
		content: {
			type: 'doc',
			content: [
				{ type: 'heading', attrs: { level: 1 }, content: [{ type: 'text', text: 'TODO' }] },
				{
					type: 'bulletList',
					content: items.map((i) => ({
						type: 'listItem',
						attrs: { nodeId: i.nodeId, taskId: i.taskId, checked: false, taskStatus: 'todo' },
						content: [{ type: 'paragraph', content: [{ type: 'text', text: i.text }] }]
					}))
				},
				{ type: 'paragraph' }
			]
		}
	});
	editors.push(editor);
	return editor;
}

function items(editor: Editor) {
	const out: { text: string; nodeId: string | null; taskId: string | null }[] = [];
	editor.state.doc.descendants((n) => {
		if (n.type.name === 'listItem') {
			out.push({ text: n.textContent, nodeId: n.attrs.nodeId, taskId: n.attrs.taskId });
		}
	});
	return out;
}

function pasteBullet(editor: Editor, nodeId: string, taskId: string, text: string) {
	// Into the empty paragraph after the list.
	editor.commands.setTextSelection(editor.state.doc.content.size - 1);
	editor.view.pasteHTML(
		`<ul><li data-node-id="${nodeId}" data-task-id="${taskId}" data-task-status="todo"><p>${text}</p></li></ul>`,
		new Event('paste') as ClipboardEvent
	);
}

describe('PasteIdentityExtension (DI-10)', () => {
	it('a pasted copy of a bullet on the same page gets its own identity and no task link', () => {
		const editor = makeEditor([{ nodeId: 'n-milk', taskId: 't-milk', text: 'Buy milk' }]);

		pasteBullet(editor, 'n-milk', 't-milk', 'Buy milk');

		const all = items(editor).filter((i) => i.text === 'Buy milk');
		expect(all).toHaveLength(2);
		expect(all.filter((i) => i.taskId === 't-milk')).toHaveLength(1);
		expect(all.filter((i) => i.nodeId === 'n-milk')).toHaveLength(1);
		const copy = all.find((i) => i.nodeId !== 'n-milk')!;
		expect(copy.nodeId).toBeTruthy();
		expect(copy.taskId).toBeNull();
	});

	it("a bullet pasted from another note does not keep that note's task link", () => {
		const editor = makeEditor([{ nodeId: 'n-a', taskId: 't-milk', text: 'a' }]);

		pasteBullet(editor, 'n-elsewhere', 't-foreign', 'From another note');

		const pasted = items(editor).find((i) => i.text === 'From another note')!;
		expect(pasted.taskId).toBeNull();
		// A fresh identity too: the old nodeId names the other note's task.
		expect(pasted.nodeId).not.toBe('n-elsewhere');
		expect(pasted.nodeId).toBeTruthy();
	});

	it('a bullet cut and pasted back into its own note keeps its identity and task', () => {
		const editor = makeEditor([
			{ nodeId: 'n-a', taskId: null, text: 'a' },
			{ nodeId: 'n-milk', taskId: 't-milk', text: 'Buy milk' }
		]);
		// Cut: remove the linked bullet from the document.
		let from = -1;
		let to = -1;
		editor.state.doc.descendants((n, pos) => {
			if (n.type.name === 'listItem' && n.attrs.nodeId === 'n-milk') {
				from = pos;
				to = pos + n.nodeSize;
			}
		});
		editor.commands.deleteRange({ from, to });
		expect(items(editor).map((i) => i.nodeId)).toEqual(['n-a']);

		pasteBullet(editor, 'n-milk', 't-milk', 'Buy milk');

		const milk = items(editor).find((i) => i.text === 'Buy milk')!;
		expect(milk).toMatchObject({ nodeId: 'n-milk', taskId: 't-milk' });
	});
});
