/**
 * Debounced task title update utility.
 * Tracks pending title updates and flushes them after a configurable delay.
 */

import { tasksStore } from '$lib/stores/tasks.svelte';
import { uiStore } from '$lib/stores/ui.svelte';

const pendingKey = (taskId: string) => `task-title:${taskId}`;
import { DEBOUNCE } from '$lib/models/constants';
import type { WriteOptions } from '$lib/storage/interfaces';

const timers = new Map<string, ReturnType<typeof setTimeout>>();
const pendingUpdates = new Map<string, string>();

/**
 * Queue a debounced title update for a task.
 * Multiple calls for the same taskId will reset the timer.
 */
export function debouncedTaskTitleUpdate(taskId: string, title: string): void {
  pendingUpdates.set(taskId, title);
  uiStore.setPendingDebounce(pendingKey(taskId), true);
  const existing = timers.get(taskId);
  if (existing) clearTimeout(existing);
  timers.set(taskId, setTimeout(() => flushTaskTitleUpdate(taskId), DEBOUNCE.TASK_TITLE));
}

/** Whether any task title write is waiting on its debounce timer. */
export function hasPendingTaskTitleUpdates(): boolean {
  return pendingUpdates.size > 0;
}

/** Whether a title write for this task is waiting on its debounce timer. */
export function hasPendingTaskTitleUpdate(taskId: string): boolean {
  return pendingUpdates.has(taskId);
}

/**
 * Immediately flush a pending title update for a specific task.
 */
export async function flushTaskTitleUpdate(taskId: string, opts?: WriteOptions): Promise<void> {
  const timer = timers.get(taskId);
  if (timer) clearTimeout(timer);
  timers.delete(taskId);
  const title = pendingUpdates.get(taskId);
  pendingUpdates.delete(taskId);
  uiStore.setPendingDebounce(pendingKey(taskId), false);
  if (title == null) return;
  uiStore.markSaving();
  try {
    // Marked as the bullet's own text, so the API doesn't take it for a
    // rename and push it back into the note (DI-29).
    await tasksStore.updateTask(taskId, { title }, { ...opts, source: 'bullet' });
  } finally {
    uiStore.markSaved();
  }
}

/**
 * Flush all pending debounced task title writes.
 * Returns when all are complete.
 */
export async function flushAllTaskTitleUpdates(opts?: WriteOptions): Promise<void> {
  const promises = [...pendingUpdates.keys()].map((id) => flushTaskTitleUpdate(id, opts));
  await Promise.all(promises);
}
