/**
 * tasksStore.adoptTask: moving a task onto a bullet pasted into another note
 * (review follow-up to DI-10).
 *
 * API mode asks the server (POST /tasks/:id/adopt), which decides under a row
 * lock whether the task's bullet really left its note. localStorage mode does
 * the same locally: a task deleted because its bullet was removed
 * (deleteRemovedBulletTask, run when the editor leaves the note) is kept
 * aside for this tab, so a paste into another note can bring it back — same
 * id, same metadata.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { createTasksStore } from './tasks.svelte';
import { ApiError } from '$lib/storage/apiClient';
import { LocalStorageAdapter } from '$lib/storage/LocalStorageAdapter';
import { TaskRepository } from '$lib/storage/repositories/TaskRepository';
import type { ITaskRepository } from '$lib/storage/interfaces';
import type { Task } from '$lib/models/types';

function makeTask(overrides: Partial<Task> = {}): Task {
	return {
		id: 'task-T',
		title: 'Buy milk',
		description: 'the oat kind',
		status: 'in-progress',
		priority: 'high',
		tags: ['groceries'],
		dueDate: '2026-10-01',
		sourcePageId: 'page-A',
		sourceNodeId: 'n-a',
		link: null,
		createdAt: '2026-01-01T00:00:00Z',
		updatedAt: '2026-01-01T00:00:00Z',
		order: 0,
		...overrides
	};
}

function apiRepo(adopt: ITaskRepository['adopt']): ITaskRepository {
	return {
		getAll: vi.fn().mockResolvedValue([]),
		getById: vi.fn().mockResolvedValue(null),
		create: vi.fn().mockImplementation(async (t: Task) => t),
		update: vi.fn(),
		delete: vi.fn().mockResolvedValue(true),
		upsert: vi.fn(),
		getByPageId: vi.fn().mockResolvedValue([]),
		getByNodeId: vi.fn().mockResolvedValue(null),
		applyFilter: vi.fn().mockReturnValue([]),
		adopt
	};
}

const dest = { sourcePageId: 'page-B', sourceNodeId: 'n-b' };

describe('tasksStore.adoptTask — API mode', () => {
	it('moves the task and puts the server’s copy in local state', async () => {
		const moved = makeTask(dest);
		const adopt = vi.fn().mockResolvedValue(moved);
		const store = createTasksStore(apiRepo(adopt));
		await store.load();

		await expect(store.adoptTask('task-T', dest)).resolves.toEqual({ kind: 'moved', task: moved });
		expect(adopt).toHaveBeenCalledWith('task-T', dest);
		expect(store.getById('task-T')?.sourcePageId).toBe('page-B');
		expect(store.getByNodeId('n-b')?.id).toBe('task-T');
	});

	it('a task dropped from local state when its bullet was cut comes back on the next page refresh', async () => {
		const moved = makeTask(dest);
		const repo = apiRepo(vi.fn().mockResolvedValue(moved));
		const store = createTasksStore(repo);
		await store.load();
		store.forgetLocal(['task-T']); // the cut, seen by A's editor

		await store.adoptTask('task-T', dest);
		vi.mocked(repo.getByPageId).mockResolvedValue([moved]);
		await store.refreshForPage('page-B');

		expect(store.getById('task-T')).toBeDefined();
	});

	it('"source_live" (a copy, or a cut not saved yet) is reported as live', async () => {
		const adopt = vi.fn().mockRejectedValue(new ApiError(409, 'POST', '/api/v1/tasks/task-T/adopt', { code: 'source_live' }));
		const store = createTasksStore(apiRepo(adopt));
		await expect(store.adoptTask('task-T', dest)).resolves.toEqual({ kind: 'live' });
	});

	it('any other refusal is final', async () => {
		for (const [status, code] of [[409, 'not_movable'], [404, undefined], [403, undefined], [409, 'source_taken']] as const) {
			const adopt = vi.fn().mockRejectedValue(new ApiError(status, 'POST', '/x', code ? { code } : null));
			const store = createTasksStore(apiRepo(adopt));
			await expect(store.adoptTask('task-T', dest)).resolves.toEqual({ kind: 'refused' });
		}
	});
});

describe('tasksStore.adoptTask — localStorage mode', () => {
	let repo: TaskRepository;
	let store: ReturnType<typeof createTasksStore>;

	beforeEach(async () => {
		localStorage.clear();
		repo = new TaskRepository(new LocalStorageAdapter());
		await repo.create(makeTask());
		store = createTasksStore(repo);
		await store.load();
	});

	it('a task deleted with its cut bullet moves to the note it is pasted into, keeping its id and metadata', async () => {
		store.forgetLocal(['task-T']);
		await store.deleteRemovedBulletTask('task-T');
		expect(await repo.getById('task-T')).toBeNull();

		const result = await store.adoptTask('task-T', dest);

		expect(result.kind).toBe('moved');
		const stored = await repo.getById('task-T');
		expect(stored).toMatchObject({
			id: 'task-T',
			status: 'in-progress',
			dueDate: '2026-10-01',
			description: 'the oat kind',
			priority: 'high',
			tags: ['groceries'],
			sourcePageId: 'page-B',
			sourceNodeId: 'n-b'
		});
		expect(store.getById('task-T')?.sourcePageId).toBe('page-B');
		expect((await repo.getAll()).filter((t) => t.title === 'Buy milk')).toHaveLength(1);
	});

	it('only once: a second paste of the same bullet is a copy', async () => {
		await store.deleteRemovedBulletTask('task-T');
		await store.adoptTask('task-T', dest);
		await expect(store.adoptTask('task-T', { sourcePageId: 'page-C', sourceNodeId: 'n-c' })).resolves.toEqual({ kind: 'live' });
		expect((await repo.getById('task-T'))?.sourcePageId).toBe('page-B');
	});

	it('a task whose bullet is still on its note (a copy) is not moved', async () => {
		await expect(store.adoptTask('task-T', dest)).resolves.toEqual({ kind: 'live' });
		expect((await repo.getById('task-T'))?.sourcePageId).toBe('page-A');
	});

	it('a task the user deleted is not brought back', async () => {
		await store.deleteTask('task-T');
		await expect(store.adoptTask('task-T', dest)).resolves.toEqual({ kind: 'refused' });
		expect(await repo.getById('task-T')).toBeNull();
	});

	it('restoreRemovedBulletTask brings a task back when its own bullet returns to its note', async () => {
		await store.deleteRemovedBulletTask('task-T');
		await expect(store.restoreRemovedBulletTask('task-T', 'n-other')).resolves.toBe(false);
		await expect(store.restoreRemovedBulletTask('task-T', 'n-a')).resolves.toBe(true);
		expect((await repo.getById('task-T'))?.sourcePageId).toBe('page-A');
		expect(store.getById('task-T')).toBeDefined();
	});
});
