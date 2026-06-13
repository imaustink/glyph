import { describe, it, expect } from 'vitest';
import { AutoSortProvider } from './AutoSortProvider';
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

const config: SortConfig = { mode: 'auto' };

describe('AutoSortProvider', () => {
  const provider = new AutoSortProvider();

  it('has correct id and name', () => {
    expect(provider.id).toBe('auto');
    expect(provider.name).toBe('Smart Sort');
  });

  it('sorts by priority weight ascending', async () => {
    const tasks = [
      makeTask({ id: 'low', priority: 'low', createdAt: '2026-01-01T00:00:00Z' }),
      makeTask({ id: 'urgent', priority: 'urgent', createdAt: '2026-01-01T00:00:00Z' }),
      makeTask({ id: 'high', priority: 'high', createdAt: '2026-01-01T00:00:00Z' }),
      makeTask({ id: 'none', priority: 'none', createdAt: '2026-01-01T00:00:00Z' }),
      makeTask({ id: 'medium', priority: 'medium', createdAt: '2026-01-01T00:00:00Z' })
    ];
    const sorted = await provider.sort(tasks, config);
    expect(sorted.map((t) => t.id)).toEqual(['urgent', 'high', 'medium', 'low', 'none']);
  });

  it('breaks priority ties by dueDate ascending (nulls last)', async () => {
    const tasks = [
      makeTask({ id: 'no-due', priority: 'medium', dueDate: null, createdAt: '2026-01-01T00:00:00Z' }),
      makeTask({ id: 'late', priority: 'medium', dueDate: '2026-06-01', createdAt: '2026-01-01T00:00:00Z' }),
      makeTask({ id: 'early', priority: 'medium', dueDate: '2026-01-15', createdAt: '2026-01-01T00:00:00Z' })
    ];
    const sorted = await provider.sort(tasks, config);
    expect(sorted.map((t) => t.id)).toEqual(['early', 'late', 'no-due']);
  });

  it('breaks dueDate ties by createdAt ascending', async () => {
    const tasks = [
      makeTask({ id: 'newer', priority: 'medium', dueDate: '2026-03-01', createdAt: '2026-02-01T00:00:00Z' }),
      makeTask({ id: 'older', priority: 'medium', dueDate: '2026-03-01', createdAt: '2026-01-01T00:00:00Z' })
    ];
    const sorted = await provider.sort(tasks, config);
    expect(sorted.map((t) => t.id)).toEqual(['older', 'newer']);
  });

  it('does not mutate the input array', async () => {
    const tasks = [
      makeTask({ id: 'low', priority: 'low' }),
      makeTask({ id: 'high', priority: 'high' })
    ];
    const original = [...tasks];
    await provider.sort(tasks, config);
    expect(tasks).toEqual(original);
  });

  it('sinks done tasks to the bottom', async () => {
    const tasks = [
      makeTask({ id: 'done-task', status: 'done', priority: 'urgent' }),
      makeTask({ id: 'active', status: 'todo', priority: 'low' })
    ];
    const sorted = await provider.sort(tasks, config);
    expect(sorted.map((t) => t.id)).toEqual(['active', 'done-task']);
  });

  it('sinks cancelled tasks to the bottom', async () => {
    const tasks = [
      makeTask({ id: 'cancelled-task', status: 'cancelled', priority: 'urgent' }),
      makeTask({ id: 'active', status: 'in-progress', priority: 'none' })
    ];
    const sorted = await provider.sort(tasks, config);
    expect(sorted.map((t) => t.id)).toEqual(['active', 'cancelled-task']);
  });

  it('sorts done and cancelled among themselves by existing criteria', async () => {
    const tasks = [
      makeTask({ id: 'done-low', status: 'done', priority: 'low' }),
      makeTask({ id: 'cancelled-urgent', status: 'cancelled', priority: 'urgent' }),
      makeTask({ id: 'active', status: 'todo', priority: 'medium' })
    ];
    const sorted = await provider.sort(tasks, config);
    expect(sorted[0].id).toBe('active');
    expect(sorted.slice(1).map((t) => t.id)).toEqual(['cancelled-urgent', 'done-low']);
  });

  it('sorts task without dueDate after task with dueDate at same priority (covers line 37)', async () => {
    // Input order: [early, noDue, late] forces TimSort to compare (noDue, early) first:
    // a = noDue (null), b = early ('...') → else: a.dueDate null, b.dueDate truthy → line 37
    const early = makeTask({ id: 'early', priority: 'none', dueDate: '2026-01-15' });
    const noDue = makeTask({ id: 'no-due', priority: 'none', dueDate: null });
    const late = makeTask({ id: 'late', priority: 'none', dueDate: '2026-06-01' });
    const sorted = await provider.sort([early, noDue, late], config);
    // Tasks with due dates come before tasks without
    expect(sorted[sorted.length - 1].id).toBe('no-due');
  });

  it('sorts two tasks with null due dates by createdAt (covers line 37 false branch → falls through)', async () => {
    // Both have null due dates → else branch: a.dueDate false, b.dueDate false → fall through to createdAt
    const newer = makeTask({ id: 'newer', priority: 'none', dueDate: null, createdAt: '2026-02-01T00:00:00Z' });
    const older = makeTask({ id: 'older', priority: 'none', dueDate: null, createdAt: '2026-01-01T00:00:00Z' });
    const sorted = await provider.sort([newer, older], config);
    expect(sorted[0].id).toBe('older');
    expect(sorted[1].id).toBe('newer');
  });
});
