/**
 * Debounced task title update utility.
 * Tracks pending title updates and flushes them after a configurable delay.
 */

import { tasksStore } from '$lib/stores/tasks.svelte';
import { uiStore } from '$lib/stores/ui.svelte';
import { DEBOUNCE } from '$lib/models/constants';

const timers = new Map<string, ReturnType<typeof setTimeout>>();
const pendingUpdates = new Map<string, string>();

/**
 * Queue a debounced title update for a task.
 * Multiple calls for the same taskId will reset the timer.
 */
export function debouncedTaskTitleUpdate(taskId: string, title: string): void {
  pendingUpdates.set(taskId, title);
  const existing = timers.get(taskId);
  if (existing) clearTimeout(existing);
  timers.set(taskId, setTimeout(() => flushTaskTitleUpdate(taskId), DEBOUNCE.TASK_TITLE));
}

/**
 * Immediately flush a pending title update for a specific task.
 */
export async function flushTaskTitleUpdate(taskId: string): Promise<void> {
  const timer = timers.get(taskId);
  if (timer) clearTimeout(timer);
  timers.delete(taskId);
  const title = pendingUpdates.get(taskId);
  pendingUpdates.delete(taskId);
  if (title == null) return;
  uiStore.markSaving();
  try {
    await tasksStore.updateTask(taskId, { title });
  } finally {
    uiStore.markSaved();
  }
}

/**
 * Flush all pending debounced task title writes.
 * Returns when all are complete.
 */
export async function flushAllTaskTitleUpdates(): Promise<void> {
  const promises = [...pendingUpdates.keys()].map((id) => flushTaskTitleUpdate(id));
  await Promise.all(promises);
}
