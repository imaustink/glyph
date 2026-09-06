import type { SortProvider } from '$lib/sort/types';
import type { SortConfig, SortContext, Task } from '$lib/models/types';

const TERMINAL_STATUSES = new Set<string>(['done', 'cancelled']);

/**
 * FieldSortProvider: sorts by any field on Task in a given direction.
 * Falls back to 'createdAt' asc when no field is specified. Synchronous.
 * The `context` param is accepted for interface parity but unused.
 */
export class FieldSortProvider implements SortProvider<Task> {
  readonly id = 'field';
  readonly name = 'Sort by Field';

  async sort(items: Task[], config: SortConfig, _context?: SortContext): Promise<Task[]> {
    const field = (config.field ?? 'createdAt') as keyof Task;
    const dir = config.direction ?? 'asc';
    const mul = dir === 'asc' ? 1 : -1;

    return [...items].sort((a, b) => {
      // Terminal statuses (done/cancelled) always sink to the bottom
      const at = TERMINAL_STATUSES.has(a.status) ? 1 : 0;
      const bt = TERMINAL_STATUSES.has(b.status) ? 1 : 0;
      if (at !== bt) return at - bt;

      const av = a[field];
      const bv = b[field];
      if (av === null || av === undefined) return 1 * mul;
      if (bv === null || bv === undefined) return -1 * mul;
      if (typeof av === 'string' && typeof bv === 'string') {
        return av.localeCompare(bv) * mul;
      }
      if (typeof av === 'number' && typeof bv === 'number') {
        return (av - bv) * mul;
      }
      return String(av).localeCompare(String(bv)) * mul;
    });
  }
}

export const fieldSortProvider = new FieldSortProvider();
