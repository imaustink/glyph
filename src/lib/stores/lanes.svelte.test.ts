/**
 * Unit tests for the lanes store.
 *
 * Injects a mock ILaneRepository so no real storage is involved.
 * Tests: load deduplication, seedDefaults (idempotent + createBatch), createLane,
 *        updateLane (optimistic + rollback), deleteLane, reorderLanes.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { createLanesStore } from './lanes.svelte';
import type { ILaneRepository } from '$lib/storage/interfaces';
import type { Lane } from '$lib/models/types';

function makeLane(overrides: Partial<Lane> = {}): Lane {
  return {
    id: 'lane-1',
    title: 'All Tasks',
    filterSet: { conjunction: 'and', rules: [] },
    sortConfig: { mode: 'auto' },
    order: 0,
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
    ...overrides
  };
}

function createMockRepo(overrides: Partial<ILaneRepository> = {}): ILaneRepository {
  return {
    getAll: vi.fn().mockResolvedValue([]),
    getById: vi.fn().mockResolvedValue(null),
    create: vi.fn().mockImplementation(async (l: Lane) => l),
    update: vi.fn().mockImplementation(async (id: string, patch: Partial<Lane>) => ({
      ...makeLane({ id }),
      ...patch
    })),
    delete: vi.fn().mockResolvedValue(true),
    upsert: vi.fn().mockImplementation(async (l: Lane) => l),
    getOrdered: vi.fn().mockResolvedValue([]),
    reorderAll: vi.fn().mockResolvedValue(undefined),
    ...overrides
  };
}

describe('lanesStore', () => {
  let repo: ReturnType<typeof createMockRepo>;
  let store: ReturnType<typeof createLanesStore>;

  beforeEach(() => {
    repo = createMockRepo();
    store = createLanesStore(repo);
    vi.clearAllMocks();
  });

  // ─── load ────────────────────────────────────────────────────────────────

  describe('load', () => {
    it('calls repo.getOrdered and populates lanes', async () => {
      const lanes = [makeLane({ id: 'l1' }), makeLane({ id: 'l2', order: 1 })];
      vi.mocked(repo.getOrdered).mockResolvedValueOnce(lanes);
      await store.load();
      expect(store.lanes).toEqual(lanes);
      expect(store.loaded).toBe(true);
    });

    it('deduplicates concurrent load calls', async () => {
      vi.mocked(repo.getOrdered).mockResolvedValue([]);
      await Promise.all([store.load(), store.load()]);
      expect(repo.getOrdered).toHaveBeenCalledTimes(1);
    });

    it('does not re-fetch when called again after loading with empty results', async () => {
      // Regression: absence of records must not cause a fetch cycle
      vi.mocked(repo.getOrdered).mockResolvedValue([]);
      await store.load();
      expect(store.loaded).toBe(true);
      vi.clearAllMocks();
      await store.load(); // second call — must be a no-op
      expect(repo.getOrdered).not.toHaveBeenCalled();
    });
  });

  // ─── seedDefaults ────────────────────────────────────────────────────────

  describe('seedDefaults', () => {
    it('seeds 4 default lanes when no lanes exist', async () => {
      await store.load(); // lanes = []
      await store.seedDefaults();
      expect(store.lanes).toHaveLength(4);
    });

    it('seeded lanes have the expected titles', async () => {
      await store.load();
      await store.seedDefaults();
      const titles = store.lanes.map((l) => l.title);
      expect(titles).toContain('All Tasks');
      expect(titles).toContain('In Progress');
      expect(titles).toContain('Done');
      expect(titles).toContain('Cancelled');
    });

    it('is idempotent: does not seed when lanes already exist', async () => {
      vi.mocked(repo.getOrdered).mockResolvedValueOnce([makeLane({ id: 'existing' })]);
      await store.load();
      await store.seedDefaults();
      expect(repo.create).not.toHaveBeenCalled();
    });

    it('uses createBatch when available', async () => {
      const createBatch = vi.fn().mockImplementation(async (items: Lane[]) => items);
      repo.createBatch = createBatch;
      store = createLanesStore(repo);
      await store.load();
      await store.seedDefaults();
      expect(createBatch).toHaveBeenCalledOnce();
      expect(repo.create).not.toHaveBeenCalled();
    });

    it('falls back to individual creates when createBatch is not available', async () => {
      // repo has no createBatch
      await store.load();
      await store.seedDefaults();
      expect(repo.create).toHaveBeenCalledTimes(4);
    });
  });

  // ─── createLane ──────────────────────────────────────────────────────────

  describe('createLane', () => {
    it('appends the new lane to the store', async () => {
      vi.mocked(repo.getOrdered).mockResolvedValueOnce([]);
      await store.load();
      const lane = makeLane({ id: 'new-lane', title: 'My Lane' });
      vi.mocked(repo.create).mockResolvedValueOnce(lane);

      const result = await store.createLane('My Lane');

      expect(result.title).toBe('My Lane');
      expect(store.lanes.find((l) => l.title === 'My Lane')).toBeDefined();
    });

    it('assigns order = maxExistingOrder + 1', async () => {
      const existing = [makeLane({ id: 'l1', order: 5 }), makeLane({ id: 'l2', order: 10 })];
      vi.mocked(repo.getOrdered).mockResolvedValueOnce(existing);
      vi.mocked(repo.create).mockImplementation(async (l: Lane) => l);
      await store.load();

      const result = await store.createLane('New');
      expect(result.order).toBe(11);
    });

    it('assigns order 0 when no lanes exist', async () => {
      vi.mocked(repo.getOrdered).mockResolvedValueOnce([]);
      vi.mocked(repo.create).mockImplementation(async (l: Lane) => l);
      await store.load();

      const result = await store.createLane('First');
      expect(result.order).toBe(0);
    });
  });

  // ─── updateLane ──────────────────────────────────────────────────────────

  describe('updateLane', () => {
    it('applies the patch to the store lane', async () => {
      const lane = makeLane({ id: 'l1', title: 'Old' });
      vi.mocked(repo.getOrdered).mockResolvedValueOnce([lane]);
      vi.mocked(repo.update).mockResolvedValueOnce({ ...lane, title: 'New' });
      await store.load();

      await store.updateLane('l1', { title: 'New' });

      expect(store.lanes.find((l) => l.id === 'l1')?.title).toBe('New');
    });

    it('applies optimistic update before repo resolves', async () => {
      const lane = makeLane({ id: 'l1', title: 'Old' });
      vi.mocked(repo.getOrdered).mockResolvedValueOnce([lane]);
      let resolveFn!: (l: Lane) => void;
      vi.mocked(repo.update).mockReturnValueOnce(
        new Promise<Lane>((r) => { resolveFn = r; })
      );
      await store.load();

      const p = store.updateLane('l1', { title: 'Optimistic' });
      expect(store.lanes.find((l) => l.id === 'l1')?.title).toBe('Optimistic');

      resolveFn({ ...lane, title: 'Confirmed' });
      await p;
      expect(store.lanes.find((l) => l.id === 'l1')?.title).toBe('Confirmed');
    });

    it('rolls back on repo failure', async () => {
      const lane = makeLane({ id: 'l1', title: 'Original' });
      vi.mocked(repo.getOrdered).mockResolvedValueOnce([lane]);
      vi.mocked(repo.update).mockRejectedValueOnce(new Error('fail'));
      await store.load();

      await expect(store.updateLane('l1', { title: 'Changed' })).rejects.toThrow();
      expect(store.lanes.find((l) => l.id === 'l1')?.title).toBe('Original');
    });

    it('rolls back only the updated lane and preserves others (covers rollback ternary false branch)', async () => {
      const l1 = makeLane({ id: 'l1', title: 'Original', order: 0 });
      const l2 = makeLane({ id: 'l2', title: 'Other', order: 1 });
      vi.mocked(repo.getOrdered).mockResolvedValueOnce([l1, l2]);
      vi.mocked(repo.update).mockRejectedValueOnce(new Error('fail'));
      await store.load();

      await expect(store.updateLane('l1', { title: 'Changed' })).rejects.toThrow();
      expect(store.lanes.find((l) => l.id === 'l1')?.title).toBe('Original');
      expect(store.lanes.find((l) => l.id === 'l2')?.title).toBe('Other');
    });

    it('is a no-op for an unknown id', async () => {
      vi.mocked(repo.getOrdered).mockResolvedValueOnce([]);
      await store.load();
      await store.updateLane('missing', { title: 'x' });
      expect(repo.update).not.toHaveBeenCalled();
    });

    it('keeps the optimistic value when repo.update returns null', async () => {
      const lane = makeLane({ id: 'l1', title: 'Old' });
      vi.mocked(repo.getOrdered).mockResolvedValueOnce([lane]);
      vi.mocked(repo.update).mockResolvedValueOnce(null);
      await store.load();

      await store.updateLane('l1', { title: 'Optimistic' });

      // When updated is null, the optimistic value stays in place
      expect(store.lanes.find((l) => l.id === 'l1')?.title).toBe('Optimistic');
    });

    it('preserves other lanes when updating one', async () => {
      const l1 = makeLane({ id: 'l1', title: 'L1', order: 0 });
      const l2 = makeLane({ id: 'l2', title: 'L2', order: 1 });
      vi.mocked(repo.getOrdered).mockResolvedValueOnce([l1, l2]);
      vi.mocked(repo.update).mockResolvedValueOnce({ ...l1, title: 'L1 Updated' });
      await store.load();

      await store.updateLane('l1', { title: 'L1 Updated' });

      expect(store.lanes.find((l) => l.id === 'l2')?.title).toBe('L2');
    });
  });

  // ─── deleteLane ──────────────────────────────────────────────────────────

  describe('deleteLane', () => {
    it('removes the lane from the store', async () => {
      const lane = makeLane({ id: 'l1' });
      vi.mocked(repo.getOrdered).mockResolvedValueOnce([lane]);
      await store.load();

      await store.deleteLane('l1');

      expect(store.lanes).toHaveLength(0);
      expect(repo.delete).toHaveBeenCalledWith('l1');
    });
  });

  // ─── reorderLanes ────────────────────────────────────────────────────────

  describe('reorderLanes', () => {
    it('calls repo.reorderAll with the new order', async () => {
      const lanes = [
        makeLane({ id: 'l1', order: 0 }),
        makeLane({ id: 'l2', order: 1 }),
        makeLane({ id: 'l3', order: 2 })
      ];
      vi.mocked(repo.getOrdered).mockResolvedValueOnce(lanes);
      await store.load();

      await store.reorderLanes(['l3', 'l1', 'l2']);

      expect(repo.reorderAll).toHaveBeenCalledWith(
        ['l3', 'l1', 'l2'],
        expect.any(String)
      );
    });

    it('updates in-store order to reflect the new sequence', async () => {
      const lanes = [
        makeLane({ id: 'l1', order: 0 }),
        makeLane({ id: 'l2', order: 1 })
      ];
      vi.mocked(repo.getOrdered).mockResolvedValueOnce(lanes);
      await store.load();

      await store.reorderLanes(['l2', 'l1']);

      const sorted = [...store.lanes].sort((a, b) => a.order - b.order);
      expect(sorted[0].id).toBe('l2');
      expect(sorted[1].id).toBe('l1');
    });
  });
});
