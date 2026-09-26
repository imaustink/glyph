import type { SortConfig, SortDirection, SortMode, Task } from '$lib/models/types';

/**
 * Build the sortConfig LaneConfig saves. The manual `taskOrder` is carried
 * over from the existing config whatever the new mode is: LaneConfig has no
 * UI for it, and dropping it would lose the user's manual order (switching
 * back to manual later restores it).
 */
export function buildLaneSortConfig(
  mode: SortMode,
  field: keyof Task,
  direction: SortDirection,
  existing: SortConfig
): SortConfig {
  return {
    mode,
    field: mode === 'field' ? field : undefined,
    direction: mode === 'field' ? direction : undefined,
    ...(existing.taskOrder ? { taskOrder: [...existing.taskOrder] } : {})
  };
}

/**
 * Merge a reorder of the *visible* tasks into a lane's full manual order.
 *
 * With a search or filter active the lane shows only some of its tasks, so
 * the dropped order covers only those. Hidden tasks keep their positions; the
 * visible tasks fill the slots the visible tasks held before, in their new
 * order. Visible tasks that weren't in the old order (e.g. just dropped in
 * from another lane) go right after the last of those slots.
 */
export function mergeTaskOrder(existing: string[] | undefined, visibleOrder: string[]): string[] {
  if (!existing || existing.length === 0) return [...visibleOrder];
  const visible = new Set(visibleOrder);
  const queue = [...visibleOrder];
  const result: string[] = [];
  let lastSlot = -1;
  for (const id of existing) {
    if (visible.has(id)) {
      const next = queue.shift();
      if (next !== undefined) {
        result.push(next);
        lastSlot = result.length - 1;
      }
    } else {
      result.push(id);
    }
  }
  if (queue.length > 0) {
    result.splice(lastSlot + 1, 0, ...queue);
  }
  return result;
}
