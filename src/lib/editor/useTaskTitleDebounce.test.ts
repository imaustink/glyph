/**
 * DI-30: a task title write waiting on its debounce is a pending write.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

const updateTask = vi.fn();
const setPendingDebounce = vi.fn();
vi.mock('$lib/stores/tasks.svelte', () => ({
	tasksStore: { updateTask: (...a: unknown[]) => updateTask(...a) }
}));
vi.mock('$lib/stores/ui.svelte', () => ({
	uiStore: { markSaving: vi.fn(), markSaved: vi.fn(), setPendingDebounce: (...a: unknown[]) => setPendingDebounce(...a) }
}));

import {
	debouncedTaskTitleUpdate,
	flushAllTaskTitleUpdates,
	hasPendingTaskTitleUpdate,
	hasPendingTaskTitleUpdates
} from './useTaskTitleDebounce';

describe('useTaskTitleDebounce pending tracking (DI-30)', () => {
	beforeEach(() => {
		vi.clearAllMocks();
		vi.useFakeTimers();
		updateTask.mockResolvedValue(undefined);
	});
	afterEach(async () => {
		await flushAllTaskTitleUpdates();
		vi.useRealTimers();
	});

	it('reports and registers an armed title write until it is sent', async () => {
		expect(hasPendingTaskTitleUpdates()).toBe(false);
		debouncedTaskTitleUpdate('t1', 'New title');

		expect(hasPendingTaskTitleUpdate('t1')).toBe(true);
		expect(hasPendingTaskTitleUpdates()).toBe(true);
		expect(setPendingDebounce).toHaveBeenLastCalledWith('task-title:t1', true);

		await vi.advanceTimersByTimeAsync(1000);
		expect(updateTask).toHaveBeenCalledWith('t1', { title: 'New title' }, { source: 'bullet' });
		expect(hasPendingTaskTitleUpdates()).toBe(false);
		expect(setPendingDebounce).toHaveBeenLastCalledWith('task-title:t1', false);
	});
});

/**
 * DI-29: a title taken from the bullet's text is marked as such, so the API
 * doesn't treat it as a rename and push it back into the note, where the
 * bullet may already hold newer text.
 */
describe('useTaskTitleDebounce change source (DI-29)', () => {
	beforeEach(() => {
		vi.clearAllMocks();
		vi.useFakeTimers();
		updateTask.mockResolvedValue(undefined);
	});
	afterEach(() => {
		vi.useRealTimers();
	});

	it('marks the debounced write as coming from the bullet', async () => {
		debouncedTaskTitleUpdate('t1', 'Typed in the bullet');
		await vi.advanceTimersByTimeAsync(1000);
		expect(updateTask).toHaveBeenCalledWith('t1', { title: 'Typed in the bullet' }, { source: 'bullet' });
	});

	it('keeps the mark when flushed with keepalive on unload', async () => {
		debouncedTaskTitleUpdate('t2', 'Typed before closing');
		await flushAllTaskTitleUpdates({ keepalive: true });
		expect(updateTask).toHaveBeenCalledWith('t2', { title: 'Typed before closing' }, { keepalive: true, source: 'bullet' });
	});
});
