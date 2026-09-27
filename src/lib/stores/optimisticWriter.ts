/**
 * Optimistic, per-record write sequencing shared by the pages, lanes and tasks
 * stores.
 *
 * - Writes to the same record are serialized, so the server sees them in the
 *   order the user made them.
 * - Every optimistic patch gets a sequence number. A server response is only
 *   applied if no newer patch has been made to that record since; an older
 *   response describes a state the user has already moved past.
 * - A failed write never restores the snapshot taken when it started (that
 *   can undo a change that succeeded meanwhile). If it is still the latest
 *   patch, the record is refetched; if the refetch fails too, only the fields
 *   this patch touched are reverted. If a newer patch exists, its own
 *   response reconciles the record.
 */
export function createOptimisticWriter<T extends { id: string }>(access: {
  get(id: string): T | undefined;
  replace(record: T): void;
}) {
  let counter = 0;
  const latest = new Map<string, number>();
  const queues = new Map<string, Promise<unknown>>();

  function enqueue<R>(id: string, fn: () => Promise<R>): Promise<R> {
    const run = (queues.get(id) ?? Promise.resolve()).then(fn);
    const tail = run.catch(() => {});
    queues.set(id, tail);
    tail.then(() => {
      if (queues.get(id) === tail) queues.delete(id);
    });
    return run;
  }

  /**
   * Apply `patch` to the record now and persist it with `write`.
   * Resolves once the write has settled; rejects with the write's error.
   * No-op when the record isn't loaded.
   */
  function update(
    id: string,
    patch: Partial<T>,
    write: () => Promise<T | null>,
    refetch: () => Promise<T | null>
  ): Promise<void> {
    const prev = access.get(id);
    if (!prev) return Promise.resolve();
    const seq = ++counter;
    latest.set(id, seq);
    access.replace({ ...prev, ...patch });
    const isLatest = () => latest.get(id) === seq;

    return enqueue(id, async () => {
      try {
        const saved = await write();
        if (saved && isLatest()) access.replace(saved);
      } catch (err) {
        if (isLatest()) {
          let fresh: T | null = null;
          try {
            fresh = await refetch();
          } catch {
            // Fall back to reverting this patch's fields below.
          }
          const current = access.get(id);
          if (isLatest() && current) {
            access.replace(fresh ?? revertFields(current, prev, patch));
          }
        }
        throw err;
      }
    });
  }

  return { update };
}

/** Undo only the fields `patch` touched, keeping every other field of `current`. */
export function revertFields<T extends object>(current: T, prev: T, patch: Partial<T>): T {
  const out = { ...current };
  for (const key of Object.keys(patch) as (keyof T)[]) {
    out[key] = prev[key];
  }
  return out;
}
