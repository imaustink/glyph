/**
 * Regression tests for the stale-document guard in useTaskCreation.
 *
 * Background: on 2026-09-21 a production incident overwrote 10 notes with an
 * 11th note's content. The mechanism was always the same — an async or
 * deferred continuation dispatched a ProseMirror transaction after the user
 * had navigated away, and the editor's onUpdate persisted the *previous*
 * page's document under the *new* page's id.
 *
 * doCreateTask awaits tasksStore.createTask() and then calls
 * setTaskIdForNode(), which dispatches a transaction. If the user navigated
 * during that await, mutating the document is unsafe.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';

const createTask = vi.fn();
const getByNodeId = vi.fn();

vi.mock('$lib/stores/tasks.svelte', () => ({
	tasksStore: {
		createTask: (...args: unknown[]) => createTask(...args),
		getByNodeId: (...args: unknown[]) => getByNodeId(...args)
	}
}));
vi.mock('$lib/stores/ui.svelte', () => ({
	uiStore: { markSaving: vi.fn(), markSaved: vi.fn() }
}));
vi.mock('$lib/stores/notifications.svelte', () => ({
	notificationsStore: { error: vi.fn() }
}));

import { useTaskCreation } from './useTaskCreation';

function makeEditor() {
	return {
		commands: { setTaskIdForNode: vi.fn(), setCheckedForNode: vi.fn(), setStatusForNode: vi.fn() }
	};
}

const bullet = {
	nodeId: 'node-1',
	pageId: 'page-A',
	bulletText: 'Write the thing'
};

/** Let the internal task-creation queue drain. */
const flush = () => new Promise((r) => setTimeout(r, 0));

describe('useTaskCreation stale-page guard', () => {
	beforeEach(() => {
		vi.clearAllMocks();
		getByNodeId.mockReturnValue(undefined);
		createTask.mockResolvedValue({ id: 'task-1' });
	});

	it('writes the taskId back when the editor is still on the originating page', async () => {
		const editor = makeEditor();
		const handle = useTaskCreation(
			() => editor as never,
			undefined,
			() => 'page-A'
		);

		handle.handleTodoBulletsDetected([bullet as never]);
		await flush();

		expect(createTask).toHaveBeenCalledTimes(1);
		expect(editor.commands.setTaskIdForNode).toHaveBeenCalledWith('node-1', 'task-1');
	});

	it('does NOT mutate the document when the editor has navigated to another page', async () => {
		const editor = makeEditor();
		let loadedPageId = 'page-A';
		// Simulate navigation completing while createTask is in flight.
		createTask.mockImplementation(async () => {
			loadedPageId = 'page-B';
			return { id: 'task-1' };
		});

		const handle = useTaskCreation(
			() => editor as never,
			undefined,
			() => loadedPageId
		);

		handle.handleTodoBulletsDetected([bullet as never]);
		await flush();

		// The task is still created — only the document mutation is skipped.
		expect(createTask).toHaveBeenCalledTimes(1);
		expect(editor.commands.setTaskIdForNode).not.toHaveBeenCalled();
	});

	it('does NOT mutate the document while a page load is in flight (null loadedPageId)', async () => {
		const editor = makeEditor();
		let loadedPageId: string | null = 'page-A';
		createTask.mockImplementation(async () => {
			loadedPageId = null;
			return { id: 'task-1' };
		});

		const handle = useTaskCreation(
			() => editor as never,
			undefined,
			() => loadedPageId
		);

		handle.handleTodoBulletsDetected([bullet as never]);
		await flush();

		expect(editor.commands.setTaskIdForNode).not.toHaveBeenCalled();
	});
});

// DI-28: a task created while the user navigated away is never written back
// to its bullet (the stale-page guard above skips the mutation). When the page
// is reopened the bullet is detected again; the task already exists for its
// nodeId, and the bullet must be linked to it rather than left unlinked.
describe('useTaskCreation links an existing task for the bullet (DI-28)', () => {
	beforeEach(() => {
		vi.clearAllMocks();
		createTask.mockResolvedValue({ id: 'task-new' });
	});

	it('links the bullet to the task that already exists for its nodeId', async () => {
		getByNodeId.mockReturnValue({ id: 'task-9', sourcePageId: 'page-A', sourceNodeId: 'node-1' });
		const editor = makeEditor();
		const handle = useTaskCreation(() => editor as never, undefined, () => 'page-A');

		handle.handleTodoBulletsDetected([bullet as never]);
		await flush();

		expect(createTask).not.toHaveBeenCalled();
		expect(editor.commands.setTaskIdForNode).toHaveBeenCalledWith('node-1', 'task-9');
	});

	it('does not link it while a different page is loaded', async () => {
		getByNodeId.mockReturnValue({ id: 'task-9', sourcePageId: 'page-A', sourceNodeId: 'node-1' });
		const editor = makeEditor();
		const handle = useTaskCreation(() => editor as never, undefined, () => null);

		handle.handleTodoBulletsDetected([bullet as never]);
		await flush();

		expect(editor.commands.setTaskIdForNode).not.toHaveBeenCalled();
	});

	it("never links a bullet to another page's task that happens to share its nodeId", async () => {
		getByNodeId.mockReturnValue({ id: 'task-other', sourcePageId: 'page-Z', sourceNodeId: 'node-1' });
		const editor = makeEditor();
		const handle = useTaskCreation(() => editor as never, undefined, () => 'page-A');

		handle.handleTodoBulletsDetected([bullet as never]);
		await flush();

		expect(editor.commands.setTaskIdForNode).not.toHaveBeenCalledWith('node-1', 'task-other');
	});
});
