import type { Task } from '$lib/models/types';

/**
 * Run each lane's filter and build a laneId → tasks map.
 *
 * Synchronous results are in the returned map straight away. A lane whose
 * filter is async (an API call) keeps its `previous` tasks until the call
 * resolves, so the board never shows it empty in between (#101). Once every
 * async lane has resolved, `onSettled` receives the complete map.
 */
export function filterTasksByLane<L extends { id: string }>(
  lanes: L[],
  filter: (lane: L) => Task[] | Promise<Task[]>,
  previous: Map<string, Task[]>,
  onSettled: (map: Map<string, Task[]>) => void
): Map<string, Task[]> {
  const settled = new Map<string, Task[]>();
  const immediate = new Map<string, Task[]>();
  const asyncWork: Promise<void>[] = [];

  for (const lane of lanes) {
    const result = filter(lane);
    if (result instanceof Promise) {
      const prior = previous.get(lane.id);
      if (prior) immediate.set(lane.id, prior);
      asyncWork.push(result.then((filtered) => { settled.set(lane.id, filtered); }));
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
