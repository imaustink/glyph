import { repositories } from '$lib/storage/config';
import type { ILaneRepository } from '$lib/storage/interfaces';
import type { Lane, FilterSet, SortConfig } from '$lib/models/types';
import { now, makeTimestamps } from '$lib/utils/time';
import { uuid } from '$lib/utils/uuid';

const DEFAULT_LANES: Omit<Lane, 'id' | 'createdAt' | 'updatedAt'>[] = [
  {
    title: 'All Tasks',
    filterSet: { conjunction: 'and', rules: [] },
    sortConfig: { mode: 'auto' },
    order: 0
  },
  {
    title: 'In Progress',
    filterSet: { conjunction: 'and', rules: [{ id: 'default-1', field: 'status', operator: 'eq', value: 'in-progress' }] },
    sortConfig: { mode: 'auto' },
    order: 1
  },
  {
    title: 'Done',
    filterSet: { conjunction: 'and', rules: [{ id: 'default-2', field: 'status', operator: 'eq', value: 'done' }] },
    sortConfig: { mode: 'field', field: 'updatedAt', direction: 'desc' },
    order: 2
  },
  {
    title: 'Cancelled',
    filterSet: { conjunction: 'and', rules: [{ id: 'default-3', field: 'status', operator: 'eq', value: 'cancelled' }] },
    sortConfig: { mode: 'auto' },
    order: 3
  }
];

export function createLanesStore(injectedRepo?: ILaneRepository) {
  const repo = injectedRepo ?? repositories.lanes;
  let lanes = $state<Lane[]>([]);
  /** O(1) lookup index by lane ID, reactively derived from lanes array. */
  let _idIndex = $derived(new Map(lanes.map(l => [l.id, l])));
  let loaded = $state(false);
  let _loadPromise: Promise<void> | null = null;

  /** Set lanes array (index is automatically rebuilt via $derived). */
  function setLanes(newLanes: Lane[]) {
    lanes = newLanes;
  }

  async function load() {
    if (loaded || _loadPromise) return _loadPromise ?? undefined;
    _loadPromise = (async () => {
      try {
        setLanes(await repo.getOrdered());
        loaded = true;
      } finally {
        _loadPromise = null;
      }
    })();
    return _loadPromise;
  }

  /** Idempotent initialization — seeds default lanes if none exist. Call after load(). */
  async function seedDefaults() {
    if (lanes.length > 0) return;
    const defaults = DEFAULT_LANES.map(def => ({
      ...def, id: uuid(), ...makeTimestamps()
    }));
    if (repo.createBatch) {
      lanes = await repo.createBatch(defaults);
    } else {
      await Promise.all(defaults.map(lane => repo.create(lane)));
      lanes = defaults;
    }
  }

  async function createLane(title: string): Promise<Lane> {
    const maxOrder = lanes.reduce((m, l) => Math.max(m, l.order), -1);
    const lane: Lane = {
      id: uuid(),
      title,
      filterSet: { conjunction: 'and', rules: [] },
      sortConfig: { mode: 'auto' },
      order: maxOrder + 1,
      ...makeTimestamps()
    };
    await repo.create(lane);
    setLanes([...lanes, lane]);
    return lane;
  }

  async function updateLane(id: string, patch: Partial<Omit<Lane, 'id' | 'createdAt'>>): Promise<void> {
    // Optimistic update: apply immediately, rollback on failure
    const prev = _idIndex.get(id);
    if (!prev) return;
    const optimistic = { ...prev, ...patch, updatedAt: now() };
    setLanes(lanes.map(l => l.id === id ? optimistic : l));

    try {
      const updated = await repo.update(id, { ...patch, updatedAt: now() });
      if (updated) {
        setLanes(lanes.map(l => l.id === id ? updated : l));
      }
    } catch (err) {
      // Rollback on failure
      setLanes(lanes.map(l => l.id === id ? prev : l));
      throw err;
    }
  }

  async function deleteLane(id: string): Promise<void> {
    await repo.delete(id);
    setLanes(lanes.filter((l) => l.id !== id));
  }

  async function reorderLanes(orderedIds: string[]): Promise<void> {
    const timestamp = now();
    await repo.reorderAll(orderedIds, timestamp);
    setLanes(orderedIds
      .map((id, i) => {
        const lane = _idIndex.get(id)!;
        return { ...lane, order: i, updatedAt: timestamp };
      })
      .sort((a, b) => a.order - b.order));
  }

  return {
    get lanes() { return lanes; },
    get loaded() { return loaded; },
    load,
    seedDefaults,
    createLane,
    updateLane,
    deleteLane,
    reorderLanes
  };
}

export const lanesStore = createLanesStore();
