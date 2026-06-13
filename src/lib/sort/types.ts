/**
 * Sort provider interface and types.
 *
 * The abstraction supports both synchronous (local) and asynchronous (AI-powered)
 * sort implementations via a unified Promise-based contract. Consumers use
 * Promise.race with a short delay to decide whether to show a loading state,
 * making the same code path work identically for instant and slow providers.
 */

import type { Task, SortConfig } from '$lib/models/types';

export interface SortProvider<T> {
  /** Unique identifier for this provider (used for config persistence). */
  readonly id: string;
  /** Human-readable name for UI display. */
  readonly name: string;
  /**
   * Sort items according to the given config.
   * May resolve instantly (local providers) or after seconds (AI providers).
   * Callers should race against a delay timer to show loading state only when needed.
   */
  sort(items: T[], config: SortConfig): Promise<T[]>;
}

export type TaskSortProvider = SortProvider<Task>;
