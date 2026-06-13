/**
 * Order utilities for generating sort-order values.
 *
 * Uses a millisecond timestamp scaled to ~1µs resolution, with a module-level
 * counter that ensures strict monotonicity even when multiple calls occur
 * within the same millisecond (e.g. during E2E tests or batch imports).
 *
 * A random sub-millisecond offset (0–998) is added to each candidate to reduce
 * collision probability when items are created on multiple devices at the same
 * wall-clock millisecond (reduces collision odds by ~1000×).
 */

let _lastOrder = 0;

/**
 * Generate a strictly monotonically-increasing sort order value.
 *
 * The random jitter means two devices creating an item at the same millisecond
 * have only a ~1-in-999 chance of colliding (vs. 100% without jitter).
 * Within a single session the counter guarantees strict monotonicity regardless
 * of jitter.
 */
export function nextOrder(): number {
  const jitter = Math.floor(Math.random() * 999);
  const candidate = Date.now() * 1000 + jitter;
  _lastOrder = candidate > _lastOrder ? candidate : _lastOrder + 1;
  return _lastOrder;
}

/**
 * Compute an order value between two existing values.
 * Useful for drag-and-drop reordering without rewriting all siblings.
 */
export function orderBetween(before: number | null, after: number | null): number {
  const lo = before ?? 0;
  const hi = after ?? lo + 2_000_000;
  return Math.floor((lo + hi) / 2);
}
