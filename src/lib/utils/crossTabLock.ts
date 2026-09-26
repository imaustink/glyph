/**
 * Run `fn` while holding a lock that is exclusive across every tab of this
 * origin (Web Locks API). Where the API is missing, `fn` just runs.
 *
 * Used for first-run seeding: without it, two tabs opened together both see
 * an empty store and both seed defaults.
 */
export async function withCrossTabLock<R>(name: string, fn: () => Promise<R>): Promise<R> {
  const locks = typeof navigator !== 'undefined'
    ? (navigator as Navigator & { locks?: Pick<LockManager, 'request'> }).locks
    : undefined;
  if (!locks?.request) return fn();
  return (await locks.request(name, fn)) as R;
}
