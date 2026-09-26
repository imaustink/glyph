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
const deleteRemovedBulletTask = vi.fn();
const restoreRemovedBulletTask = vi.fn();
const forgetLocal = vi.fn();
const refreshTask = vi.fn();
const getById = vi.fn();

vi.mock('$lib/stores/tasks.svelte', () => ({
	tasksStore: {
		deleteTask: (...a: unknown[]) => deleteTask(...a),
		deleteRemovedBulletTask: (...a: unknown[]) => deleteRemovedBulletTask(...a),
		restoreRemovedBulletTask: (...a: unknown[]) => restoreRemovedBulletTask(...a),
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
		// DI-10: the delete used to be a hard delete 1 s after the bullet left
		// the document, so a cut followed by a paste or undo more than 1 s later
		// lost the task. It is now a soft delete: hidden at once, deleted from
		// storage only when the editor leaves the page (flush).
		it('deletes the task whose bullet was removed once the page is flushed', async () => {
			editor = makeEditor(docWith({ nodeId: 'a', taskId: 't-a' }, { nodeId: 'b', taskId: 't-b' }));
			const handle = useBulletRemoval();
			handle.snapshot(editor);

			editor.commands.setContent(docWith({ nodeId: 'b', taskId: 't-b' }));
			await handle.detectRemovedTaskBullets(editor);

			expect(deleteRemovedBulletTask).not.toHaveBeenCalled();
			expect(forgetLocal).toHaveBeenCalledWith(['t-a']);

			await handle.flush();
			// Deleted as a removed bullet's task (kept aside so a paste into
			// another note can move it there), not as a user delete.
			expect(deleteRemovedBulletTask).toHaveBeenCalledTimes(1);
			expect(deleteRemovedBulletTask).toHaveBeenCalledWith('t-a');
			expect(deleteTask).not.toHaveBeenCalled();
		});

		it('after the flush, a bullet that comes back to its note gets its task back', async () => {
			editor = makeEditor(docWith({ nodeId: 'a', taskId: 't-a' }));
			const handle = useBulletRemoval();
			handle.snapshot(editor);
			editor.commands.setContent(docWith());
			await handle.detectRemovedTaskBullets(editor);
			await handle.flush();

			getById.mockReturnValue(undefined);
			restoreRemovedBulletTask.mockResolvedValue(true);
			editor.commands.setContent(docWith({ nodeId: 'a', taskId: 't-a' }));
			await handle.detectRemovedTaskBullets(editor);

			expect(restoreRemovedBulletTask).toHaveBeenCalledWith('t-a', 'a');
		});

		it('restores the task when its bullet comes back before the flush (cut → paste, undo)', async () => {
			editor = makeEditor(docWith({ nodeId: 'a', taskId: 't-a' }));
			const handle = useBulletRemoval();
			handle.snapshot(editor);

			editor.commands.setContent(docWith());
			await handle.detectRemovedTaskBullets(editor);
			await vi.advanceTimersByTimeAsync(5000);

			getById.mockReturnValue(undefined); // hidden from local state
			refreshTask.mockResolvedValue(true);
			editor.commands.setContent(docWith({ nodeId: 'a', taskId: 't-a' }));
			await handle.detectRemovedTaskBullets(editor);

			expect(refreshTask).toHaveBeenCalledWith('t-a');
			await handle.flush();
			expect(deleteRemovedBulletTask).not.toHaveBeenCalled();
		});
	});

	it('remembers which page a removed bullet’s task came from', async () => {
		editor = makeEditor(docWith({ nodeId: 'a', taskId: 't-a' }));
		getById.mockReturnValue({ id: 't-a', sourcePageId: 'page-A' });
		const handle = useBulletRemoval({ serverReconciles: true });
		handle.snapshot(editor);

		editor.commands.setContent(docWith());
		await handle.detectRemovedTaskBullets(editor);

		expect(handle.sourcePageOfRemoved('t-a')).toBe('page-A');
		expect(handle.sourcePageOfRemoved('t-unknown')).toBeUndefined();
	});

	// A cut in one note and a paste in another may span a remount of the
	// editor; the paste still needs to know the bullet was cut (it then waits
	// for the server to see the cut before moving the task).
	it('remembers it across editor instances in the same tab', async () => {
		editor = makeEditor(docWith({ nodeId: 'a', taskId: 't-cut' }));
		getById.mockReturnValue({ id: 't-cut', sourcePageId: 'page-A' });
		const first = useBulletRemoval({ serverReconciles: true });
		first.snapshot(editor);
		editor.commands.setContent(docWith());
		await first.detectRemovedTaskBullets(editor);
		first.destroy();

		expect(useBulletRemoval({ serverReconciles: true }).sourcePageOfRemoved('t-cut')).toBe('page-A');
	});

	describe('API mode (server reconciles)', () => {
		it('never deletes from storage — only drops the task from local state', async () => {
			editor = makeEditor(docWith({ nodeId: 'a', taskId: 't-a' }));
			const handle = useBulletRemoval({ serverReconciles: true });
			handle.snapshot(editor);

			editor.commands.setContent(docWith());
			await handle.detectRemovedTaskBullets(editor);
			await handle.flush();

			expect(deleteTask).not.toHaveBeenCalled();
			expect(deleteRemovedBulletTask).not.toHaveBeenCalled();
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
