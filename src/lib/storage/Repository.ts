import type { StorageAdapter } from '$lib/models/types';

/**
 * Generic async-ready repository base class for localStorage.
 *
 * Maintains an in-memory cache after first load so that sequential read
 * operations within a write-queue cycle don't redundantly hit localStorage.
 * The cache is always consistent with storage because all writes go through
 * the serialized write queue and update both cache and storage atomically.
 *
 * API-backed repositories do NOT extend this class — they implement the
 * IRepository interface directly with HTTP calls.
 */
export class Repository<T extends { id: string }> {
  protected readonly storageKey: string;

  /**
   * Serializes all write operations (create/update/delete/upsert) to prevent
   * read-modify-write races when multiple mutations overlap at async boundaries.
   */
  private _writeQueue: Promise<unknown> = Promise.resolve();

  /** In-memory cache of the full collection. Null means cache is cold. */
  private _cache: T[] | null = null;

  /** Cross-tab notification channel for immediate cache invalidation. */
  private _channel: BroadcastChannel | null = null;

  private serializeWrite<R>(fn: () => Promise<R>): Promise<R> {
    const next = this._writeQueue.then(fn);
    // Don't let a failed write poison the queue for future operations.
    // The error is still propagated to the caller via `next`.
    this._writeQueue = next.catch((err) => {
      console.error(`[Repository:${this.storageKey}] Write failed:`, err);
    });
    return next;
  }

  constructor(
    protected readonly adapter: StorageAdapter,
    collectionKey: string
  ) {
    this.storageKey = `glyph:${collectionKey}`;

    if (typeof window !== 'undefined') {
      // Invalidate cache when another tab writes to our storage key.
      // The 'storage' event only fires in other tabs, not the one that wrote.
      window.addEventListener('storage', (e: StorageEvent) => {
        if (e.key === this.storageKey) {
          this._cache = null;
        }
      });

      // BroadcastChannel provides immediate cross-tab cache invalidation
      // (faster than waiting for the storage event which can be delayed).
      if (typeof BroadcastChannel !== 'undefined') {
        this._channel = new BroadcastChannel(`glyph:sync:${collectionKey}`);
        this._channel.onmessage = () => {
          this._cache = null;
        };
      }
    }
  }

  /** Notify other tabs that this collection has been updated. */
  private notifyOtherTabs(): void {
    this._channel?.postMessage('invalidate');
  }

  protected async readAll(): Promise<T[]> {
    if (this._cache !== null) return this._cache;
    const items = (await this.adapter.get<T[]>(this.storageKey)) ?? [];
    this._cache = items;
    return items;
  }

  protected async writeAll(items: T[]): Promise<void> {
    await this.adapter.set(this.storageKey, items);
    this._cache = items;
    this.notifyOtherTabs();
  }

  async getAll(): Promise<T[]> {
    // Return a copy so callers can't accidentally mutate the cache
    return [...(await this.readAll())];
  }

  async getById(id: string): Promise<T | null> {
    const items = await this.readAll();
    return items.find((item) => item.id === id) ?? null;
  }

  async create(item: T): Promise<T> {
    return this.serializeWrite(async () => {
      const items = await this.readAll();
      const updated = [...items, item];
      await this.writeAll(updated);
      return item;
    });
  }

  async update(id: string, patch: Partial<Omit<T, 'id'>>): Promise<T | null> {
    return this.serializeWrite(async () => {
      const items = await this.readAll();
      const idx = items.findIndex((item) => item.id === id);
      if (idx === -1) return null;
      const patched = { ...items[idx], ...patch };
      const updated = [...items];
      updated[idx] = patched;
      await this.writeAll(updated);
      return patched;
    });
  }

  async delete(id: string): Promise<boolean> {
    return this.serializeWrite(async () => {
      const items = await this.readAll();
      const filtered = items.filter((item) => item.id !== id);
      if (filtered.length === items.length) return false;
      await this.writeAll(filtered);
      return true;
    });
  }

  async upsert(item: T): Promise<T> {
    return this.serializeWrite(async () => {
      const items = await this.readAll();
      const idx = items.findIndex((i) => i.id === item.id);
      const updated = [...items];
      if (idx === -1) {
        updated.push(item);
      } else {
        updated[idx] = item;
      }
      await this.writeAll(updated);
      return item;
    });
  }

  /**
   * Delete multiple items in a single read-modify-write cycle.
   */
  async deleteMany(ids: string[]): Promise<void> {
    if (ids.length === 0) return;
    return this.serializeWrite(async () => {
      const idSet = new Set(ids);
      const items = await this.readAll();
      const filtered = items.filter((item) => !idSet.has(item.id));
      await this.writeAll(filtered);
    });
  }

  /**
   * Apply partial updates to multiple items in a single read-modify-write cycle.
   */
  async updateMany(patches: Map<string, Partial<Omit<T, 'id'>>>): Promise<void> {
    if (patches.size === 0) return;
    return this.serializeWrite(async () => {
      const items = await this.readAll();
      const updated = items.map(item => {
        const patch = patches.get(item.id);
        return patch ? { ...item, ...patch } : item;
      });
      await this.writeAll(updated);
    });
  }
}
