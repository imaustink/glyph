import type { Task } from '$lib/models/types';

/**
 * Build the placeholder shown for an async lane while its filter call is in
 * flight: the lane's previous tasks, each swapped for its current Task object.
 * A task that has changed since (or no longer exists) is dropped, so a card
 * just moved out of the lane does not snap back into it and no lane renders
 * an outdated copy of a task. Previous entries are usually server responses,
 * not the store's own objects, so "unchanged" means the same reference or the
 * same `updatedAt` — every task write bumps `updatedAt`.
 */
function placeholderFor(prior: Task[], current: Map<string, Task>): Task[] {
  const out: Task[] = [];
  for (const old of prior) {
    const now = current.get(old.id);
    if (now && (now === old || now.updatedAt === old.updatedAt)) out.push(now);
  }
  return out;
}

/**
 * Run each lane's filter and build a laneId → tasks map.
 *
 * Synchronous results are in the returned map straight away. A lane whose
 * filter is async (an API call) keeps showing its `previous` tasks — minus any
 * that changed since, see placeholderFor — until the call resolves, so the
 * board never shows it empty in between (#101). Once every async lane has
 * settled, `onSettled` receives the complete map. A lane whose call fails
 * keeps its placeholder rather than holding up the other lanes.
 */
export function filterTasksByLane<L extends { id: string }>(
  lanes: L[],
  tasks: Task[],
  filter: (lane: L) => Task[] | Promise<Task[]>,
  previous: Map<string, Task[]>,
  onSettled: (map: Map<string, Task[]>) => void
): Map<string, Task[]> {
  const settled = new Map<string, Task[]>();
  const immediate = new Map<string, Task[]>();
  const asyncWork: Promise<void>[] = [];
  let current: Map<string, Task> | undefined;

  for (const lane of lanes) {
    const result = filter(lane);
    if (result instanceof Promise) {
      const prior = previous.get(lane.id);
      let placeholder: Task[] = [];
      if (prior) {
        current ??= new Map(tasks.map((t) => [t.id, t]));
        placeholder = placeholderFor(prior, current);
        immediate.set(lane.id, placeholder);
      }
      asyncWork.push(result.then(
        (filtered) => { settled.set(lane.id, filtered); },
        () => { settled.set(lane.id, placeholder); }
      ));
    } else {
      settled.set(lane.id, result);
      immediate.set(lane.id, result);
    }
  }

  if (asyncWork.length > 0) {
    Promise.all(asyncWork).then(() => {
      // Keep lane order matching `lanes`.
      onSettled(new Map(lanes.map((l) => [l.id, settled.get(l.id) ?? []])));
    });
  }

  return immediate;
}
