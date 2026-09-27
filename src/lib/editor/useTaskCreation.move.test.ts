/**
 * Review follow-up to DI-10: a bullet cut from note A and pasted into note B
 * takes its task along instead of B getting a bare new task (and A's task,
 * with its status, due date, description…, being deleted with A's bullet).
 *
 * PasteIdentityExtension gives the pasted bullet a fresh nodeId and reports
 * it; useTaskCreation asks the server to move the task onto it
 * (tasksStore.adoptTask). The server refuses while the task is still live —
 * a copy, or a cut whose save hasn't reached it yet — so a bullet this editor
 * saw leave its note is retried for a while; anything else falls back to a
 * new task at once, as before.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

const createTask = vi.fn();
const getByNodeId = vi.fn();
const adoptTask = vi.fn();

vi.mock('$lib/stores/tasks.svelte', () => ({
	tasksStore: {
		createTask: (...args: unknown[]) => createTask(...args),
		getByNodeId: (...args: unknown[]) => getByNodeId(...args),
		adoptTask: (...args: unknown[]) => adoptTask(...args)
	}
}));
vi.mock('$lib/stores/ui.svelte', () => ({
	uiStore: { markSaving: vi.fn(), markSaved: vi.fn() }
}));
vi.mock('$lib/stores/notifications.svelte', () => ({
	notificationsStore: { error: vi.fn() }
}));

import { useTaskCreation, ADOPT_RETRY_DELAYS_MS } from './useTaskCreation';

function makeEditor(nodeIds: string[] = ['n-new']) {
	return {
		state: {
			doc: {
				descendants(fn: (node: { type: { name: string }; attrs: Record<string, unknown> }) => unknown) {
					for (const nodeId of nodeIds) fn({ type: { name: 'listItem' }, attrs: { nodeId } });
				}
			}
		},
		commands: { setTaskIdForNode: vi.fn(), setCheckedForNode: vi.fn(), setStatusForNode: vi.fn() }
	};
}

const pasted = { nodeId: 'n-new', pageId: 'page-B', bulletText: 'Buy milk', hasCursor: false };
const movedTask = { id: 'task-T', status: 'in-progress', sourcePageId: 'page-B', sourceNodeId: 'n-new' };
const retryBudget = () => ADOPT_RETRY_DELAYS_MS.reduce((a, b) => a + b, 0);

describe('useTaskCreation — a pasted bullet takes its task along', () => {
	beforeEach(() => {
		vi.clearAllMocks();
		vi.useFakeTimers();
		getByNodeId.mockReturnValue(undefined);
		createTask.mockResolvedValue({ id: 'task-new' });
	});
	afterEach(() => vi.useRealTimers());

	it('links the pasted bullet to the moved task, with its status, and creates no new task', async () => {
		adoptTask.mockResolvedValue({ kind: 'moved', task: movedTask });
		const editor = makeEditor();
		const handle = useTaskCreation(() => editor as never, undefined, () => 'page-B');

		handle.adoptPasted([{ nodeId: 'n-new', taskId: 'task-T', cut: true }], 'page-B');
		// Detection sees the (stripped) bullet in the same transaction.
		handle.handleTodoBulletsDetected([pasted]);
		await vi.runAllTimersAsync();

		expect(adoptTask).toHaveBeenCalledWith('task-T', { sourcePageId: 'page-B', sourceNodeId: 'n-new' });
		expect(editor.commands.setTaskIdForNode).toHaveBeenCalledWith('n-new', 'task-T');
		expect(editor.commands.setStatusForNode).toHaveBeenCalledWith('n-new', 'in-progress');
		expect(editor.commands.setCheckedForNode).toHaveBeenCalledWith('n-new', false);
		expect(createTask).not.toHaveBeenCalled();
	});

	it("retries while the cut hasn't reached the server yet, then links the task", async () => {
		adoptTask
			.mockResolvedValueOnce({ kind: 'live' })
			.mockResolvedValueOnce({ kind: 'live' })
			.mockResolvedValue({ kind: 'moved', task: movedTask });
		const editor = makeEditor();
		const handle = useTaskCreation(() => editor as never, undefined, () => 'page-B');

		handle.adoptPasted([{ nodeId: 'n-new', taskId: 'task-T', cut: true }], 'page-B');
		handle.handleTodoBulletsDetected([pasted]);
		await vi.runAllTimersAsync();

		expect(adoptTask).toHaveBeenCalledTimes(3);
		expect(editor.commands.setTaskIdForNode).toHaveBeenCalledWith('n-new', 'task-T');
		expect(createTask).not.toHaveBeenCalled();
	});

	it('a copy (task still live, its bullet not cut here) gets a new task at once', async () => {
		adoptTask.mockResolvedValue({ kind: 'live' });
		const editor = makeEditor();
		const handle = useTaskCreation(() => editor as never, undefined, () => 'page-B');

		handle.adoptPasted([{ nodeId: 'n-new', taskId: 'task-T', cut: false }], 'page-B');
		handle.handleTodoBulletsDetected([pasted]);
		await vi.advanceTimersByTimeAsync(0);

		expect(adoptTask).toHaveBeenCalledTimes(1);
		expect(createTask).toHaveBeenCalledTimes(1);
		expect(createTask).toHaveBeenCalledWith({ title: 'Buy milk', sourcePageId: 'page-B', sourceNodeId: 'n-new' });
		expect(editor.commands.setTaskIdForNode).toHaveBeenCalledWith('n-new', 'task-new');
	});

	it('gives up after the retry budget and creates a new task', async () => {
		adoptTask.mockResolvedValue({ kind: 'live' });
		const editor = makeEditor();
		const handle = useTaskCreation(() => editor as never, undefined, () => 'page-B');

		handle.adoptPasted([{ nodeId: 'n-new', taskId: 'task-T', cut: true }], 'page-B');
		handle.handleTodoBulletsDetected([pasted]);
		await vi.advanceTimersByTimeAsync(retryBudget() - 1);
		expect(createTask).not.toHaveBeenCalled();
		await vi.runAllTimersAsync();

		expect(adoptTask).toHaveBeenCalledTimes(ADOPT_RETRY_DELAYS_MS.length + 1);
		expect(createTask).toHaveBeenCalledTimes(1);
	});

	it('a refused move (task deleted by the user, or not ours) falls back to a new task without retrying', async () => {
		adoptTask.mockResolvedValue({ kind: 'refused' });
		const editor = makeEditor();
		const handle = useTaskCreation(() => editor as never, undefined, () => 'page-B');

		handle.adoptPasted([{ nodeId: 'n-new', taskId: 'task-T', cut: true }], 'page-B');
		handle.handleTodoBulletsDetected([pasted]);
		await vi.advanceTimersByTimeAsync(0);

		expect(adoptTask).toHaveBeenCalledTimes(1);
		expect(createTask).toHaveBeenCalledTimes(1);
	});

	it('uses the bullet text as it is when the move is refused, not as it was pasted', async () => {
		adoptTask.mockResolvedValue({ kind: 'live' });
		const editor = makeEditor();
		const handle = useTaskCreation(() => editor as never, undefined, () => 'page-B');

		handle.adoptPasted([{ nodeId: 'n-new', taskId: 'task-T', cut: true }], 'page-B');
		handle.handleTodoBulletsDetected([pasted]);
		handle.handleTodoBulletsDetected([{ ...pasted, bulletText: 'Buy oat milk' }]);
		await vi.runAllTimersAsync();

		expect(createTask).toHaveBeenCalledTimes(1);
		expect(createTask).toHaveBeenCalledWith(expect.objectContaining({ title: 'Buy oat milk' }));
	});

	it('does not touch the document once the editor has left the page', async () => {
		let loaded = 'page-B';
		adoptTask.mockImplementation(async () => {
			loaded = 'page-C';
			return { kind: 'moved', task: movedTask };
		});
		const editor = makeEditor();
		const handle = useTaskCreation(() => editor as never, undefined, () => loaded);

		handle.adoptPasted([{ nodeId: 'n-new', taskId: 'task-T', cut: true }], 'page-B');
		await vi.runAllTimersAsync();

		expect(editor.commands.setTaskIdForNode).not.toHaveBeenCalled();
		expect(createTask).not.toHaveBeenCalled();
	});

	it('stops retrying once the pasted bullet is gone (e.g. the paste was undone)', async () => {
		adoptTask.mockResolvedValue({ kind: 'live' });
		const editor = makeEditor([]);
		const handle = useTaskCreation(() => editor as never, undefined, () => 'page-B');

		handle.adoptPasted([{ nodeId: 'n-new', taskId: 'task-T', cut: true }], 'page-B');
		await vi.runAllTimersAsync();

		expect(adoptTask).toHaveBeenCalledTimes(1);
		expect(createTask).not.toHaveBeenCalled();
	});
});
