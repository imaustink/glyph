/**
 * Unit tests for LaneRepository.
 *
 * Covers: getOrdered (sort by order), reorderAll (batch update).
 */

import { describe, it, expect, beforeEach } from 'vitest';
import { LaneRepository } from './LaneRepository';
import type { StorageAdapter, Lane } from '$lib/models/types';

function createMockAdapter(): StorageAdapter {
  const store = new Map<string, unknown>();
  return {
    async get<T>(key: string): Promise<T | null> {
      return (store.get(key) as T) ?? null;
    },
    async set<T>(key: string, value: T): Promise<void> {
      store.set(key, value);
    },
    async remove(key: string): Promise<void> {
      store.delete(key);
    },
    async keys(): Promise<string[]> {
      return [...store.keys()];
    },
    async clear(): Promise<void> {
      store.clear();
    }
  };
}

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

describe('LaneRepository', () => {
  let repo: LaneRepository;

  beforeEach(() => {
    repo = new LaneRepository(createMockAdapter());
  });

  describe('getOrdered', () => {
    it('returns lanes sorted by order ascending', async () => {
      await repo.create(makeLane({ id: 'l3', order: 2 }));
      await repo.create(makeLane({ id: 'l1', order: 0 }));
      await repo.create(makeLane({ id: 'l2', order: 1 }));
      const result = await repo.getOrdered();
      expect(result.map((l) => l.id)).toEqual(['l1', 'l2', 'l3']);
    });

    it('returns an empty array when no lanes exist', async () => {
      expect(await repo.getOrdered()).toEqual([]);
    });

    it('returns a single lane without error', async () => {
      await repo.create(makeLane({ id: 'only', order: 42 }));
      const result = await repo.getOrdered();
      expect(result).toHaveLength(1);
      expect(result[0].id).toBe('only');
    });

    it('handles lanes with equal order values (stable output)', async () => {
      await repo.create(makeLane({ id: 'a', order: 1 }));
      await repo.create(makeLane({ id: 'b', order: 1 }));
      const result = await repo.getOrdered();
      expect(result).toHaveLength(2);
      // Both have equal order — just ensure both are present
      expect(result.map((l) => l.id)).toContain('a');
      expect(result.map((l) => l.id)).toContain('b');
    });
  });

  describe('reorderAll', () => {
    it('assigns sequential order indices to lanes in provided order', async () => {
      await repo.create(makeLane({ id: 'l1', order: 5 }));
      await repo.create(makeLane({ id: 'l2', order: 10 }));

      const ts = '2026-05-01T00:00:00Z';
      await repo.reorderAll(['l2', 'l1'], ts);

      const all = await repo.getAll();
      const l1 = all.find((l) => l.id === 'l1')!;
      const l2 = all.find((l) => l.id === 'l2')!;
      expect(l2.order).toBe(0);
      expect(l1.order).toBe(1);
    });

    it('stamps the provided updatedAt on all reordered lanes', async () => {
      await repo.create(makeLane({ id: 'l1', order: 0 }));
      await repo.create(makeLane({ id: 'l2', order: 1 }));

      const ts = '2026-06-15T12:00:00Z';
      await repo.reorderAll(['l1', 'l2'], ts);

      const all = await repo.getAll();
      for (const lane of all) {
        expect(lane.updatedAt).toBe(ts);
      }
    });

    it('leaves other fields unchanged after reorder', async () => {
      const original = makeLane({
        id: 'l1',
        title: 'My Lane',
        filterSet: { conjunction: 'or', rules: [] },
        order: 99
      });
      await repo.create(original);

      await repo.reorderAll(['l1'], '2026-01-01T00:00:00Z');

      const updated = await repo.getById('l1');
      expect(updated?.title).toBe('My Lane');
      expect(updated?.filterSet.conjunction).toBe('or');
    });

    it('is a no-op for an empty id list', async () => {
      await repo.create(makeLane({ id: 'l1', order: 5 }));
      await repo.reorderAll([], '2026-01-01T00:00:00Z');
      const lane = await repo.getById('l1');
      expect(lane?.order).toBe(5);
    });
  });
});
