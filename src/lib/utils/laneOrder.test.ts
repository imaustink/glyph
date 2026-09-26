import { describe, expect, it } from 'vitest';
import { buildLaneSortConfig, mergeTaskOrder } from './laneOrder';

describe('buildLaneSortConfig [DI-25]', () => {
  it('keeps the manual taskOrder when the lane config is saved', () => {
    const existing = { mode: 'manual' as const, taskOrder: ['a', 'b', 'c'] };
    expect(buildLaneSortConfig('manual', 'createdAt', 'asc', existing)).toEqual({
      mode: 'manual',
      field: undefined,
      direction: undefined,
      taskOrder: ['a', 'b', 'c']
    });
  });

  it('keeps the taskOrder when switching away from manual, so switching back restores it', () => {
    const existing = { mode: 'manual' as const, taskOrder: ['a', 'b'] };
    expect(buildLaneSortConfig('field', 'priority', 'desc', existing)).toEqual({
      mode: 'field',
      field: 'priority',
      direction: 'desc',
      taskOrder: ['a', 'b']
    });
  });

  it('omits taskOrder when the lane never had one', () => {
    expect(buildLaneSortConfig('auto', 'createdAt', 'asc', { mode: 'auto' })).not.toHaveProperty('taskOrder');
  });
});

describe('mergeTaskOrder [DI-25]', () => {
  it('reorders only the visible tasks and keeps hidden ones where they were', () => {
    // A search hides a and c; the user swaps b and d.
    expect(mergeTaskOrder(['a', 'b', 'c', 'd'], ['d', 'b'])).toEqual(['a', 'd', 'c', 'b']);
  });

  it('equals the new order when every task is visible', () => {
    expect(mergeTaskOrder(['a', 'b', 'c'], ['c', 'a', 'b'])).toEqual(['c', 'a', 'b']);
  });

  it('inserts a newly dropped task among the visible ones', () => {
    expect(mergeTaskOrder(['a', 'b', 'c'], ['a', 'x', 'b', 'c'])).toEqual(['a', 'x', 'b', 'c']);
  });

  it('places new tasks after the last visible slot while hidden tasks keep their place', () => {
    expect(mergeTaskOrder(['h1', 'a', 'h2'], ['x', 'a'])).toEqual(['h1', 'x', 'a', 'h2']);
  });

  it('uses the new order when there was no previous order', () => {
    expect(mergeTaskOrder(undefined, ['b', 'a'])).toEqual(['b', 'a']);
  });
});
