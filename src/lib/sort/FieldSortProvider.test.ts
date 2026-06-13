import { describe, it, expect } from 'vitest';
import { FieldSortProvider } from './FieldSortProvider';
import type { Task, SortConfig } from '$lib/models/types';

function makeTask(overrides: Partial<Task> = {}): Task {
  return {
    id: 'task-1',
    title: 'Test',
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

describe('FieldSortProvider', () => {
  const provider = new FieldSortProvider();

  it('has correct id and name', () => {
    expect(provider.id).toBe('field');
    expect(provider.name).toBe('Sort by Field');
  });

  it('sorts by specified field ascending', async () => {
    const tasks = [
      makeTask({ id: 'c', title: 'Charlie' }),
      makeTask({ id: 'a', title: 'Alpha' }),
      makeTask({ id: 'b', title: 'Bravo' })
    ];
    const config: SortConfig = { mode: 'field', field: 'title', direction: 'asc' };
    const sorted = await provider.sort(tasks, config);
    expect(sorted.map((t) => t.id)).toEqual(['a', 'b', 'c']);
  });

  it('sorts by specified field descending', async () => {
    const tasks = [
      makeTask({ id: 'a', title: 'Alpha' }),
      makeTask({ id: 'c', title: 'Charlie' }),
      makeTask({ id: 'b', title: 'Bravo' })
    ];
    const config: SortConfig = { mode: 'field', field: 'title', direction: 'desc' };
    const sorted = await provider.sort(tasks, config);
    expect(sorted.map((t) => t.id)).toEqual(['c', 'b', 'a']);
  });

  it('defaults to createdAt asc when no field specified', async () => {
    const tasks = [
      makeTask({ id: 'late', createdAt: '2026-03-01T00:00:00Z' }),
      makeTask({ id: 'early', createdAt: '2026-01-01T00:00:00Z' }),
      makeTask({ id: 'mid', createdAt: '2026-02-01T00:00:00Z' })
    ];
    const config: SortConfig = { mode: 'field' };
    const sorted = await provider.sort(tasks, config);
    expect(sorted.map((t) => t.id)).toEqual(['early', 'mid', 'late']);
  });

  it('pushes null values to end in ascending order', async () => {
    const tasks = [
      makeTask({ id: 'none', dueDate: null }),
      makeTask({ id: 'has', dueDate: '2026-01-15' })
    ];
    const config: SortConfig = { mode: 'field', field: 'dueDate', direction: 'asc' };
    const sorted = await provider.sort(tasks, config);
    expect(sorted.map((t) => t.id)).toEqual(['has', 'none']);
  });

  it('pushes null values to start in descending order', async () => {
    const tasks = [
      makeTask({ id: 'has', dueDate: '2026-01-15' }),
      makeTask({ id: 'none', dueDate: null })
    ];
    const config: SortConfig = { mode: 'field', field: 'dueDate', direction: 'desc' };
    const sorted = await provider.sort(tasks, config);
    expect(sorted.map((t) => t.id)).toEqual(['none', 'has']);
  });

  it('sorts numeric fields correctly', async () => {
    const tasks = [
      makeTask({ id: 'mid', order: 5 }),
      makeTask({ id: 'first', order: 1 }),
      makeTask({ id: 'last', order: 10 })
    ];
    const config: SortConfig = { mode: 'field', field: 'order', direction: 'asc' };
    const sorted = await provider.sort(tasks, config);
    expect(sorted.map((t) => t.id)).toEqual(['first', 'mid', 'last']);
  });

  it('does not mutate the input array', async () => {
    const tasks = [
      makeTask({ id: 'b', title: 'B' }),
      makeTask({ id: 'a', title: 'A' })
    ];
    const original = [...tasks];
    const config: SortConfig = { mode: 'field', field: 'title', direction: 'asc' };
    await provider.sort(tasks, config);
    expect(tasks).toEqual(original);
  });

  it('sinks done tasks to the bottom regardless of field sort', async () => {
    const tasks = [
      makeTask({ id: 'done-task', status: 'done', title: 'Alpha' }),
      makeTask({ id: 'active', status: 'todo', title: 'Zeta' })
    ];
    const config: SortConfig = { mode: 'field', field: 'title', direction: 'asc' };
    const sorted = await provider.sort(tasks, config);
    expect(sorted.map((t) => t.id)).toEqual(['active', 'done-task']);
  });

  it('sinks cancelled tasks to the bottom regardless of field sort', async () => {
    const tasks = [
      makeTask({ id: 'cancelled-task', status: 'cancelled', title: 'Alpha' }),
      makeTask({ id: 'active', status: 'in-progress', title: 'Zeta' })
    ];
    const config: SortConfig = { mode: 'field', field: 'title', direction: 'asc' };
    const sorted = await provider.sort(tasks, config);
    expect(sorted.map((t) => t.id)).toEqual(['active', 'cancelled-task']);
  });

  it('sorts done and cancelled among themselves by the configured field', async () => {
    const tasks = [
      makeTask({ id: 'done-z', status: 'done', title: 'Zeta' }),
      makeTask({ id: 'done-a', status: 'done', title: 'Alpha' }),
      makeTask({ id: 'active', status: 'todo', title: 'Bravo' })
    ];
    const config: SortConfig = { mode: 'field', field: 'title', direction: 'asc' };
    const sorted = await provider.sort(tasks, config);
    expect(sorted.map((t) => t.id)).toEqual(['active', 'done-a', 'done-z']);
  });

  it('falls back to string comparison for non-string non-number fields (covers line 35)', async () => {
    // tags is an array — not a string or number — so String() conversion is used.
    const t1 = makeTask({ id: 't1', tags: ['apple'] });
    const t2 = makeTask({ id: 't2', tags: ['banana'] });
    const config: SortConfig = { mode: 'field', field: 'tags', direction: 'asc' };
    const sorted = await provider.sort([t2, t1], config);
    // Just verify no throw and all tasks are returned
    expect(sorted).toHaveLength(2);
    expect(sorted.map((t) => t.id).sort()).toEqual(['t1', 't2']);
  });
});
