import type { StorageAdapter } from '$lib/models/types';

const GLYPH_PREFIX = 'glyph:';

/**
 * Key a corrupt value is copied to before anything else can touch it:
 * `glyph:corrupt:<key without the glyph: prefix>:<timestamp>`.
 */
export function corruptBackupKey(key: string, timestamp = Date.now()): string {
  const bare = key.startsWith(GLYPH_PREFIX) ? key.slice(GLYPH_PREFIX.length) : key;
  return `${GLYPH_PREFIX}corrupt:${bare}:${timestamp}`;
}

/**
 * Thrown when writing to a key whose stored value could not be read. The
 * write is refused so the unreadable data (already copied to `backupKey`) is
 * never replaced by, for example, first-run seed data.
 */
export class CorruptStorageError extends Error {
  constructor(
    public readonly key: string,
    public readonly backupKey: string
  ) {
    super(
      `Saved data in "${key}" is corrupt and was not overwritten. ` +
      `A copy was kept in "${backupKey}". Export your data before clearing it.`
    );
    this.name = 'CorruptStorageError';
  }
}

/**
 * Synchronous localStorage wrapped in the async StorageAdapter interface.
 *
 * The methods are async (returning resolved promises) because StorageAdapter
 * must support genuinely async backends (Postgres, IndexedDB, etc.) through
 * the same contract. The microtask overhead on already-resolved promises is
 * negligible in V8. This is intentional, not tech debt.
 */
export class LocalStorageAdapter implements StorageAdapter {
  /**
   * Keys whose stored value is unreadable, with their backup key and a check
   * for whether the stored value has since become valid. Writes to them are
   * refused. Static because every adapter instance in a tab shares the same
   * localStorage.
   */
  private static readonly quarantined = new Map<string, { backupKey: string; isValid: (value: unknown) => boolean }>();

  async get<T>(key: string): Promise<T | null> {
    if (typeof localStorage === 'undefined') return null;
    const raw = localStorage.getItem(key);
    if (raw === null) return null;
    try {
      return JSON.parse(raw) as T;
    } catch {
      this.quarantine(key, raw);
      return null;
    }
  }

  /**
   * Copy an unreadable value aside (once) and refuse further writes to `key`
   * until the stored value is valid again. `raw` is stored as-is; a
   * non-string value is JSON-encoded. `isValid` decides what a valid stored
   * value is (default: any parseable JSON).
   */
  quarantine(key: string, raw: unknown, isValid: (value: unknown) => boolean = () => true): string {
    const existing = LocalStorageAdapter.quarantined.get(key);
    if (existing && localStorage.getItem(existing.backupKey) !== null) return existing.backupKey;
    const backupKey = corruptBackupKey(key);
    try {
      localStorage.setItem(backupKey, typeof raw === 'string' ? raw : JSON.stringify(raw));
    } catch (e) {
      console.error(`[LocalStorageAdapter] Could not back up corrupt "${key}":`, e);
    }
    LocalStorageAdapter.quarantined.set(key, { backupKey, isValid });
    console.error(`[LocalStorageAdapter] "${key}" is corrupt; backed up to "${backupKey}". Writes to it are refused.`);
    return backupKey;
  }

  async set<T>(key: string, value: T): Promise<void> {
    if (typeof localStorage === 'undefined') return;
    const entry = LocalStorageAdapter.quarantined.get(key);
    if (entry) {
      // The stored value may have been fixed (or cleared) since, e.g. by
      // another tab or by hand. Only keep refusing while it is still bad.
      if (LocalStorageAdapter.isStoredValueValid(localStorage.getItem(key), entry.isValid)) {
        LocalStorageAdapter.quarantined.delete(key);
      } else {
        throw new CorruptStorageError(key, entry.backupKey);
      }
    }
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

  private static isStoredValueValid(raw: string | null, isValid: (value: unknown) => boolean): boolean {
    if (raw === null) return true;
    try {
      return isValid(JSON.parse(raw));
    } catch {
      return false;
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
