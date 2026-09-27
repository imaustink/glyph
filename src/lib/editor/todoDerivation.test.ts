/**
 * DI-27: the editor and the Go API (api/internal/pmmd/todo.go, used for
 * content written through MCP) must derive the same tasks from the same
 * content. Both run this one fixture file; see its _comment.
 *
 * The editor side runs the real pipeline — TodoDetectionExtension feeding
 * useTaskCreation — on a single-writer editor, then records which bullets a
 * task was created for after an unrelated edit elsewhere in the note (the
 * cursor is outside every bullet, as for content that merely exists).
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { Editor } from '@tiptap/core';
import { documentExtensions } from '$lib/editor/schema';
import { NodeIdMapExtension } from '$lib/editor/plugins/NodeIdMapPlugin';
import { TodoDetectionExtension } from '$lib/editor/extensions/TodoDetectionExtension';
import type { TodoTriggerConfig } from '$lib/models/types';
import fixture from '../../../api/internal/pmmd/testdata/todo_derivation.json';

const createTask = vi.fn();
vi.mock('$lib/stores/tasks.svelte', () => ({
	tasksStore: {
		createTask: (...a: unknown[]) => createTask(...a),
		getByNodeId: () => undefined
	}
}));
vi.mock('$lib/stores/ui.svelte', () => ({ uiStore: { markSaving: vi.fn(), markSaved: vi.fn() } }));
vi.mock('$lib/stores/notifications.svelte', () => ({ notificationsStore: { error: vi.fn() } }));

import { useTaskCreation } from './useTaskCreation';
import { isCheckedStatus } from './todoDerivation';

interface DetectionCase {
	name: string;
	trigger: TodoTriggerConfig | null;
	doc: Record<string, unknown>;
	want: string[];
}

const editors: Editor[] = [];
afterEach(() => {
	for (const e of editors.splice(0)) e.destroy();
	document.body.innerHTML = '';
});

async function settle() {
	for (let i = 0; i < 20; i++) await new Promise((r) => setTimeout(r, 0));
}

async function tasksCreatedFor(c: DetectionCase): Promise<string[]> {
	let editor: Editor | null = null;
	const creation = useTaskCreation(() => editor, undefined, () => 'page-1');
	const mount = document.createElement('div');
	document.body.appendChild(mount);
	editor = new Editor({
		element: mount,
		extensions: [
			...documentExtensions(),
			NodeIdMapExtension,
			TodoDetectionExtension.configure({
				pageId: () => 'page-1',
				todoTrigger: () => (c.trigger ?? undefined) as TodoTriggerConfig | undefined,
				onTodoBulletsDetected: (b) => creation.handleTodoBulletsDetected(b)
			})
		],
		content: c.doc
	});
	editors.push(editor);
	// Park the cursor in the intro paragraph every fixture doc opens with,
	// then make an unrelated edit there.
	editor.commands.setTextSelection(1);
	editor.commands.insertContentAt(1, 'x');
	await settle();
	return createTask.mock.calls.map(([params]) => (params as { title: string }).title);
}

describe('TODO derivation shared fixture (editor side)', () => {
	beforeEach(() => {
		createTask.mockReset();
		let n = 0;
		createTask.mockImplementation(async () => ({ id: `task-${++n}` }));
	});

	for (const c of (fixture as { detection: DetectionCase[] }).detection) {
		it(c.name, async () => {
			expect(await tasksCreatedFor(c)).toEqual(c.want);
		});
	}

	for (const { status, checked } of (fixture as { checked: { status: string; checked: boolean }[] }).checked) {
		it(`status ${status} shows as checked=${checked}`, () => {
			expect(isCheckedStatus(status)).toBe(checked);
		});
	}
});
