/**
 * useBulletRemoval decides what happens to a task when its bullet leaves the
 * document. With one writer (localStorage) the client deletes the task. With
 * a server (API mode) — and especially with several editors — only the server
 * may act, because every connected client observes the same "removal",
 * including for a cut/paste or an undo.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { Editor } from '@tiptap/core';
import StarterKit from '@tiptap/starter-kit';
import { TaskLinkExtension } from '$lib/editor/extensions/TaskLinkExtension';
import { NodeIdMapExtension } from '$lib/editor/plugins/NodeIdMapPlugin';

const deleteTask = vi.fn();
const forgetLocal = vi.fn();
const refreshTask = vi.fn();
const getById = vi.fn();

vi.mock('$lib/stores/tasks.svelte', () => ({
	tasksStore: {
		deleteTask: (...a: unknown[]) => deleteTask(...a),
		forgetLocal: (...a: unknown[]) => forgetLocal(...a),
		refreshTask: (...a: unknown[]) => refreshTask(...a),
		getById: (...a: unknown[]) => getById(...a)
	}
}));

import { useBulletRemoval } from './useBulletRemoval';

function docWith(...items: { nodeId: string; taskId?: string }[]) {
	return {
		type: 'doc',
		content: [
			{
				type: 'bulletList',
				content: items.map((i) => ({
					type: 'listItem',
					attrs: { nodeId: i.nodeId, taskId: i.taskId ?? null },
					content: [{ type: 'paragraph', content: [{ type: 'text', text: i.nodeId }] }]
				}))
			}
		]
	};
}

function makeEditor(content: object) {
	return new Editor({
		extensions: [StarterKit.configure({ listItem: false }), NodeIdMapExtension, TaskLinkExtension],
		content
	});
}

describe('useBulletRemoval', () => {
	let editor: Editor;

	beforeEach(() => {
		vi.clearAllMocks();
		vi.useFakeTimers();
		getById.mockReturnValue({ id: 'present' });
	});
	afterEach(() => {
		editor?.destroy();
		vi.useRealTimers();
	});

	describe('localStorage mode (client is the only writer)', () => {
		it('deletes the task whose bullet was removed', async () => {
			editor = makeEditor(docWith({ nodeId: 'a', taskId: 't-a' }, { nodeId: 'b', taskId: 't-b' }));
			const handle = useBulletRemoval();
			handle.snapshot(editor);

			editor.commands.setContent(docWith({ nodeId: 'b', taskId: 't-b' }));
			await handle.detectRemovedTaskBullets(editor);

			expect(deleteTask).toHaveBeenCalledTimes(1);
			expect(deleteTask).toHaveBeenCalledWith('t-a');
		});
	});

	describe('API mode (server reconciles)', () => {
		it('never deletes from storage — only drops the task from local state', async () => {
			editor = makeEditor(docWith({ nodeId: 'a', taskId: 't-a' }));
			const handle = useBulletRemoval({ serverReconciles: true });
			handle.snapshot(editor);

			editor.commands.setContent(docWith());
			await handle.detectRemovedTaskBullets(editor);

			expect(deleteTask).not.toHaveBeenCalled();
			expect(forgetLocal).toHaveBeenCalledWith(['t-a']);
		});

		it('re-reads a task whose bullet came back (undo / paste)', async () => {
			editor = makeEditor(docWith());
			const handle = useBulletRemoval({ serverReconciles: true });
			handle.snapshot(editor);
			getById.mockReturnValue(undefined); // not in local state any more
			refreshTask.mockResolvedValue(true);

			editor.commands.setContent(docWith({ nodeId: 'a', taskId: 't-a' }));
			await handle.detectRemovedTaskBullets(editor);
			expect(refreshTask).not.toHaveBeenCalled(); // waits for the server to save first

			await vi.advanceTimersByTimeAsync(2000);
			expect(refreshTask).toHaveBeenCalledWith('t-a');
		});

		it('keeps retrying until the server has restored the task, then stops', async () => {
			editor = makeEditor(docWith());
			const handle = useBulletRemoval({ serverReconciles: true });
			handle.snapshot(editor);
			getById.mockReturnValue(undefined);
			refreshTask.mockResolvedValueOnce(false).mockResolvedValueOnce(true);

			editor.commands.setContent(docWith({ nodeId: 'a', taskId: 't-a' }));
			await handle.detectRemovedTaskBullets(editor);
			await vi.advanceTimersByTimeAsync(2000 + 5000 + 12000);

			expect(refreshTask).toHaveBeenCalledTimes(2);
		});

		it('stops polling once destroyed', async () => {
			editor = makeEditor(docWith());
			const handle = useBulletRemoval({ serverReconciles: true });
			handle.snapshot(editor);
			getById.mockReturnValue(undefined);

			editor.commands.setContent(docWith({ nodeId: 'a', taskId: 't-a' }));
			await handle.detectRemovedTaskBullets(editor);
			handle.destroy();
			await vi.advanceTimersByTimeAsync(20000);

			expect(refreshTask).not.toHaveBeenCalled();
		});

		it('ignores a bullet that merely moved within the document', async () => {
			editor = makeEditor(docWith({ nodeId: 'a', taskId: 't-a' }, { nodeId: 'b', taskId: 't-b' }));
			const handle = useBulletRemoval({ serverReconciles: true });
			handle.snapshot(editor);

			editor.commands.setContent(docWith({ nodeId: 'b', taskId: 't-b' }, { nodeId: 'a', taskId: 't-a' }));
			await handle.detectRemovedTaskBullets(editor);

			expect(forgetLocal).toHaveBeenCalledWith([]);
			expect(refreshTask).not.toHaveBeenCalled();
		});
	});
});
