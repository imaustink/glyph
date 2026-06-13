import type { StorageAdapter } from '$lib/models/types';

/**
 * Synchronous localStorage wrapped in the async StorageAdapter interface.
 *
 * The methods are async (returning resolved promises) because StorageAdapter
 * must support genuinely async backends (Postgres, IndexedDB, etc.) through
 * the same contract. The microtask overhead on already-resolved promises is
 * negligible in V8. This is intentional, not tech debt.
 */
export class LocalStorageAdapter implements StorageAdapter {
  async get<T>(key: string): Promise<T | null> {
    if (typeof localStorage === 'undefined') return null;
    const raw = localStorage.getItem(key);
    if (raw === null) return null;
    try {
      return JSON.parse(raw) as T;
    } catch {
      return null;
    }
  }

  async set<T>(key: string, value: T): Promise<void> {
    if (typeof localStorage === 'undefined') return;
    try {
      localStorage.setItem(key, JSON.stringify(value));
    } catch (e) {
      if (
        e instanceof DOMException &&
        (e.name === 'QuotaExceededError' || e.name === 'NS_ERROR_DOM_QUOTA_REACHED')
      ) {
        throw new Error(
          'Storage quota exceeded. Your notes are too large to save locally. ' +
          'Free up browser storage or switch to the API backend.'
        );
      }
      throw e;
    }
  }

  async remove(key: string): Promise<void> {
    if (typeof localStorage === 'undefined') return;
    localStorage.removeItem(key);
  }

  async keys(): Promise<string[]> {
    if (typeof localStorage === 'undefined') return [];
    return Object.keys(localStorage);
  }

  async clear(): Promise<void> {
    if (typeof localStorage === 'undefined') return;
    localStorage.clear();
  }
}
