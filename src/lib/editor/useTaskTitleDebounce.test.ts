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
		expect(updateTask).toHaveBeenCalledWith('t1', { title: 'New title' });
		expect(hasPendingTaskTitleUpdates()).toBe(false);
		expect(setPendingDebounce).toHaveBeenLastCalledWith('task-title:t1', false);
	});
});
