/**
 * useTaskSync keeps a linked bullet's text and its task's title in step.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { Editor } from '@tiptap/core';
import { documentExtensions } from '$lib/editor/schema';
import { NodeIdMapExtension } from '$lib/editor/plugins/NodeIdMapPlugin';
import type { Task } from '$lib/models/types';

let tasks: Partial<Task>[] = [];
vi.mock('$lib/stores/tasks.svelte', () => ({
	tasksStore: {
		getById: (id: string) => tasks.find((t) => t.id === id),
		get tasksByPage() {
			const m = new Map<string, Partial<Task>[]>();
			for (const t of tasks) {
				if (!t.sourcePageId) continue;
				m.set(t.sourcePageId, [...(m.get(t.sourcePageId) ?? []), t]);
			}
			return m;
		},
		updateTask: vi.fn()
	}
}));
const debouncedTaskTitleUpdate = vi.fn();
vi.mock('$lib/editor/useTaskTitleDebounce', () => ({
	debouncedTaskTitleUpdate: (...a: unknown[]) => debouncedTaskTitleUpdate(...a),
	hasPendingTaskTitleUpdate: () => false
}));

import { useTaskSync } from './useTaskSync';

const editors: Editor[] = [];
afterEach(() => {
	for (const e of editors.splice(0)) e.destroy();
	document.body.innerHTML = '';
});

function linkedDoc(text: string) {
	return {
		type: 'doc',
		content: [
			{
				type: 'bulletList',
				content: [
					{
						type: 'listItem',
						attrs: { nodeId: 'n-milk', taskId: 't-milk', checked: false, taskStatus: 'todo' },
						content: [{ type: 'paragraph', content: [{ type: 'text', text }] }]
					}
				]
			}
		]
	};
}

/** An editor wired like Editor.svelte: every update pushes bullet text to tasks. */
function makeEditor(content: object) {
	let editor: Editor | null = null;
	const sync = useTaskSync(() => editor, () => 'page-1');
	const mount = document.createElement('div');
	document.body.appendChild(mount);
	editor = new Editor({
		element: mount,
		extensions: [...documentExtensions(), NodeIdMapExtension],
		content,
		onUpdate: ({ editor: ed, transaction }) =>
			sync.syncLinkedTaskTitleRealtime(ed, () => null, () => {}, transaction)
	});
	editors.push(editor);
	return { editor, sync };
}

function bulletTexts(editor: Editor): string[] {
	const out: string[] = [];
	editor.state.doc.descendants((n) => {
		if (n.type.name === 'listItem') out.push(n.textContent);
	});
	return out;
}

describe('useTaskSync', () => {
	beforeEach(() => {
		vi.clearAllMocks();
		tasks = [{ id: 't-milk', title: 'Buy milk', status: 'todo', sourcePageId: 'page-1', sourceNodeId: 'n-milk' }];
	});

	// DI-09: splitting "Buy milk" in the middle leaves the task on the upper
	// half ("Buy") with the cursor in the lower half. The task title must
	// follow its bullet even though the cursor is no longer in it.
	it('resyncs the task title of a linked bullet split in the middle (DI-09)', () => {
		const { editor } = makeEditor(linkedDoc('Buy milk'));
		editor.commands.setTextSelection(3 + 3); // "Buy| milk"

		editor.commands.keyboardShortcut('Enter');

		expect(bulletTexts(editor)).toEqual(['Buy', ' milk']);
		expect(debouncedTaskTitleUpdate).toHaveBeenCalledWith('t-milk', 'Buy');
	});
});
