import type { SortProvider } from '$lib/sort/types';
import type { SortConfig, SortContext, Task, Priority } from '$lib/models/types';
import { PRIORITY_WEIGHT } from '$lib/models/constants';

const TERMINAL_STATUSES = new Set<string>(['done', 'cancelled']);

/**
 * AutoSortProvider: sorts by dueDate ascending (nulls last — an impending due
 * date always outranks a task with no due date, regardless of priority), then
 * by task priority weight ascending, then by source-note priority ascending
 * (weight) as a final tie breaker, then by createdAt ascending. Terminal
 * statuses (done/cancelled) always sink to the bottom. Synchronous.
 */
export class AutoSortProvider implements SortProvider<Task> {
  readonly id = 'auto';
  readonly name = 'Smart Sort';

  async sort(items: Task[], _config: SortConfig, context?: SortContext): Promise<Task[]> {
    const notePriority = (task: Task): Priority =>
      context?.getNotePriority?.(task) ?? 'none';

    return [...items].sort((a, b) => {
      // Terminal statuses (done/cancelled) always sink to the bottom
      const at = TERMINAL_STATUSES.has(a.status) ? 1 : 0;
      const bt = TERMINAL_STATUSES.has(b.status) ? 1 : 0;
      if (at !== bt) return at - bt;

      // dueDate takes top precedence — nulls last, so any task with a due
      // date outranks one without, no matter how high its priority is.
      if (a.dueDate && b.dueDate) {
        const dc = a.dueDate.localeCompare(b.dueDate);
        if (dc !== 0) return dc;
      } else {
        if (a.dueDate) return -1;
        if (b.dueDate) return 1;
      }

      // Then task priority
      const pw = PRIORITY_WEIGHT[a.priority] - PRIORITY_WEIGHT[b.priority];
      if (pw !== 0) return pw;

      // Note priority only breaks ties left by dueDate + task priority
      const nw = PRIORITY_WEIGHT[notePriority(a)] - PRIORITY_WEIGHT[notePriority(b)];
      if (nw !== 0) return nw;

      return a.createdAt.localeCompare(b.createdAt);
    });
  }
}

export const autoSortProvider = new AutoSortProvider();
