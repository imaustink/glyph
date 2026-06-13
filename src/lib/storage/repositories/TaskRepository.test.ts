import { describe, it, expect, beforeEach } from 'vitest';
import { TaskRepository } from './TaskRepository';
import type { StorageAdapter, Task, FilterSet } from '$lib/models/types';

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

function makeTask(overrides: Partial<Task> = {}): Task {
  return {
    id: 'task-1',
    title: 'Test task',
    description: '',
    status: 'todo',
    priority: 'medium',
    tags: [],
    dueDate: null,
    sourcePageId: null,
    sourceNodeId: null,
    link: null,
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
    order: 0,
    ...overrides
  };
}

describe('TaskRepository', () => {
  let repo: TaskRepository;

  beforeEach(() => {
    repo = new TaskRepository(createMockAdapter());
  });

  describe('getByPageId', () => {
    it('returns tasks for the given page', async () => {
      await repo.create(makeTask({ id: 't1', sourcePageId: 'p1' }));
      await repo.create(makeTask({ id: 't2', sourcePageId: 'p2' }));
      await repo.create(makeTask({ id: 't3', sourcePageId: 'p1' }));
      const result = await repo.getByPageId('p1');
      expect(result.map((t) => t.id)).toEqual(['t1', 't3']);
    });
  });

  describe('getByNodeId', () => {
    it('returns the matching task', async () => {
      await repo.create(makeTask({ id: 't1', sourceNodeId: 'n1' }));
      const result = await repo.getByNodeId('n1');
      expect(result?.id).toBe('t1');
    });

    it('returns null when not found', async () => {
      expect(await repo.getByNodeId('missing')).toBeNull();
    });
  });

  describe('applyFilter', () => {
    const tasks = [
      makeTask({ id: 't1', status: 'todo', priority: 'high', tags: ['bug'], dueDate: '2026-03-01' }),
      makeTask({ id: 't2', status: 'done', priority: 'low', tags: ['feature'], dueDate: '2026-06-01' }),
      makeTask({ id: 't3', status: 'todo', priority: 'medium', tags: [], dueDate: null }),
      makeTask({ id: 't4', status: 'in-progress', priority: 'urgent', tags: ['bug', 'feature'], dueDate: '2026-01-15' })
    ];

    it('returns all tasks when rules are empty', () => {
      const filter: FilterSet = { conjunction: 'and', rules: [] };
      expect(repo.applyFilter(tasks, filter)).toEqual(tasks);
    });

    describe('any operator', () => {
      it('matches every task regardless of field value', () => {
        const filter: FilterSet = {
          conjunction: 'and',
          rules: [{ id: 'r1', field: 'status', operator: 'any', value: null }]
        };
        const result = repo.applyFilter(tasks, filter);
        expect(result).toEqual(tasks);
      });
    });

    describe('eq operator', () => {
      it('matches equal values', () => {
        const filter: FilterSet = {
          conjunction: 'and',
          rules: [{ id: 'r1', field: 'status', operator: 'eq', value: 'todo' }]
        };
        const result = repo.applyFilter(tasks, filter);
        expect(result.map((t) => t.id)).toEqual(['t1', 't3']);
      });
    });

    describe('neq operator', () => {
      it('matches non-equal values', () => {
        const filter: FilterSet = {
          conjunction: 'and',
          rules: [{ id: 'r1', field: 'status', operator: 'neq', value: 'done' }]
        };
        const result = repo.applyFilter(tasks, filter);
        expect(result.map((t) => t.id)).toEqual(['t1', 't3', 't4']);
      });
    });

    describe('in operator', () => {
      it('matches values in the array', () => {
        const filter: FilterSet = {
          conjunction: 'and',
          rules: [{ id: 'r1', field: 'priority', operator: 'in', value: ['high', 'urgent'] }]
        };
        const result = repo.applyFilter(tasks, filter);
        expect(result.map((t) => t.id)).toEqual(['t1', 't4']);
      });
    });

    describe('not_in operator', () => {
      it('excludes values in the array', () => {
        const filter: FilterSet = {
          conjunction: 'and',
          rules: [{ id: 'r1', field: 'priority', operator: 'not_in', value: ['high', 'urgent'] }]
        };
        const result = repo.applyFilter(tasks, filter);
        expect(result.map((t) => t.id)).toEqual(['t2', 't3']);
      });
    });

    describe('contains operator', () => {
      it('matches array containing the value', () => {
        const filter: FilterSet = {
          conjunction: 'and',
          rules: [{ id: 'r1', field: 'tags', operator: 'contains', value: 'bug' }]
        };
        const result = repo.applyFilter(tasks, filter);
        expect(result.map((t) => t.id)).toEqual(['t1', 't4']);
      });

      it('matches string containing the value', () => {
        const filter: FilterSet = {
          conjunction: 'and',
          rules: [{ id: 'r1', field: 'title', operator: 'contains', value: 'Test' }]
        };
        const result = repo.applyFilter(tasks, filter);
        expect(result).toHaveLength(4); // all tasks have "Test task" title
      });
    });

    describe('before operator', () => {
      it('matches dates before the value', () => {
        const filter: FilterSet = {
          conjunction: 'and',
          rules: [{ id: 'r1', field: 'dueDate', operator: 'before', value: '2026-04-01' }]
        };
        const result = repo.applyFilter(tasks, filter);
        expect(result.map((t) => t.id)).toEqual(['t1', 't4']);
      });
    });

    describe('after operator', () => {
      it('matches dates after the value', () => {
        const filter: FilterSet = {
          conjunction: 'and',
          rules: [{ id: 'r1', field: 'dueDate', operator: 'after', value: '2026-04-01' }]
        };
        const result = repo.applyFilter(tasks, filter);
        expect(result.map((t) => t.id)).toEqual(['t2']);
      });
    });

    describe('exists operator', () => {
      it('matches non-null/non-empty values', () => {
        const filter: FilterSet = {
          conjunction: 'and',
          rules: [{ id: 'r1', field: 'dueDate', operator: 'exists', value: null }]
        };
        const result = repo.applyFilter(tasks, filter);
        expect(result.map((t) => t.id)).toEqual(['t1', 't2', 't4']);
      });
    });

    describe('not_exists operator', () => {
      it('matches null/undefined/empty values', () => {
        const filter: FilterSet = {
          conjunction: 'and',
          rules: [{ id: 'r1', field: 'dueDate', operator: 'not_exists', value: null }]
        };
        const result = repo.applyFilter(tasks, filter);
        expect(result.map((t) => t.id)).toEqual(['t3']);
      });
    });

    describe('conjunction', () => {
      it('AND requires all rules to match', () => {
        const filter: FilterSet = {
          conjunction: 'and',
          rules: [
            { id: 'r1', field: 'status', operator: 'eq', value: 'todo' },
            { id: 'r2', field: 'priority', operator: 'eq', value: 'high' }
          ]
        };
        const result = repo.applyFilter(tasks, filter);
        expect(result.map((t) => t.id)).toEqual(['t1']);
      });

      it('OR requires at least one rule to match', () => {
        const filter: FilterSet = {
          conjunction: 'or',
          rules: [
            { id: 'r1', field: 'status', operator: 'eq', value: 'done' },
            { id: 'r2', field: 'priority', operator: 'eq', value: 'urgent' }
          ]
        };
        const result = repo.applyFilter(tasks, filter);
        expect(result.map((t) => t.id)).toEqual(['t2', 't4']);
      });
    });

    describe('default operator', () => {
      it('returns true for an unknown operator (default case)', () => {
        const filter: FilterSet = {
          conjunction: 'and',
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          rules: [{ id: 'r1', field: 'status', operator: 'unknown' as any, value: null }]
        };
        expect(repo.applyFilter(tasks, filter)).toHaveLength(tasks.length);
      });
    });
  });
});
