import type { SortProvider } from '$lib/sort/types';
import type { SortConfig, Task } from '$lib/models/types';

const TERMINAL_STATUSES = new Set<string>(['done', 'cancelled']);

const PRIORITY_WEIGHT: Record<string, number> = {
  urgent: 0,
  high: 1,
  medium: 2,
  low: 3,
  none: 4
};

/**
 * AutoSortProvider: sorts by priority weight ascending, then by dueDate ascending
 * (nulls last), then by createdAt ascending. Synchronous.
 */
export class AutoSortProvider implements SortProvider<Task> {
  readonly id = 'auto';
  readonly name = 'Smart Sort';

  async sort(items: Task[], _config: SortConfig): Promise<Task[]> {
    return [...items].sort((a, b) => {
      // Terminal statuses (done/cancelled) always sink to the bottom
      const at = TERMINAL_STATUSES.has(a.status) ? 1 : 0;
      const bt = TERMINAL_STATUSES.has(b.status) ? 1 : 0;
      if (at !== bt) return at - bt;

      const pw = PRIORITY_WEIGHT[a.priority] - PRIORITY_WEIGHT[b.priority];
      if (pw !== 0) return pw;
      // nulls last
      if (a.dueDate && b.dueDate) {
        const dc = a.dueDate.localeCompare(b.dueDate);
        if (dc !== 0) return dc;
      } else {
        if (a.dueDate) return -1;
        if (b.dueDate) return 1;
      }
      return a.createdAt.localeCompare(b.createdAt);
    });
  }
}

export const autoSortProvider = new AutoSortProvider();
