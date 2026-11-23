import { describe, expect, it, vi } from 'vitest';
import { filterTasksByLane } from './laneFilter';
import type { Task } from '$lib/models/types';

const task = (id: string, updatedAt = 't0', extra: Partial<Task> = {}) =>
  ({ id, updatedAt, ...extra }) as Task;

const flush = async () => {
  for (let i = 0; i < 5; i++) await Promise.resolve();
};

describe('filterTasksByLane (#101)', () => {
  it('returns sync results immediately and does not call onSettled', () => {
    const onSettled = vi.fn();
    const map = filterTasksByLane([{ id: 'a' }], [], () => [task('1')], new Map(), onSettled);
    expect(map.get('a')).toEqual([task('1')]);
    expect(onSettled).not.toHaveBeenCalled();
  });

  it('keeps an async lane\'s previous tasks until its filter resolves', async () => {
    let resolve!: (t: Task[]) => void;
    const pending = new Promise<Task[]>((r) => { resolve = r; });
    const old = task('old');
    const previous = new Map([['b', [old]]]);
    const onSettled = vi.fn();

    const map = filterTasksByLane(
      [{ id: 'a' }, { id: 'b' }],
      [old],
      (lane) => (lane.id === 'a' ? [task('1')] : pending),
      previous,
      onSettled
    );

    expect(map.get('a')).toEqual([task('1')]);
    expect(map.get('b')).toEqual([old]);

    resolve([task('new')]);
    await flush();

    expect(onSettled).toHaveBeenCalledOnce();
    const settled = onSettled.mock.calls[0][0] as Map<string, Task[]>;
    expect(settled.get('a')).toEqual([task('1')]);
    expect(settled.get('b')).toEqual([task('new')]);
  });

  it('drops lanes that no longer exist', () => {
    const previous = new Map([['gone', [task('x')]]]);
    const map = filterTasksByLane([{ id: 'a' }], [task('x')], () => new Promise<Task[]>(() => {}), previous, () => {});
    expect(map.has('gone')).toBe(false);
    expect(map.has('a')).toBe(false);
  });

  it('drops a changed (moved) task from the placeholder so it does not snap back', () => {
    // Lane A previously held tasks 1 and 2 (as returned by the server).
    const previous = new Map([['a', [task('1', 't0', { status: 'todo' }), task('2', 't0')]]]);
    // Task 1 was just moved out of A: the store holds an updated copy.
    const moved = task('1', 't1', { status: 'done' });
    const unchanged = task('2', 't0');

    const map = filterTasksByLane(
      [{ id: 'a' }],
      [moved, unchanged],
      () => new Promise<Task[]>(() => {}),
      previous,
      () => {}
    );

    expect(map.get('a')).toEqual([unchanged]);
  });

  it('keeps an unchanged task, using the store\'s current object', () => {
    const fromServer = task('2', 't0');
    const inStore = task('2', 't0');
    const map = filterTasksByLane(
      [{ id: 'a' }],
      [inStore],
      () => new Promise<Task[]>(() => {}),
      new Map([['a', [fromServer]]]),
      () => {}
    );
    expect(map.get('a')![0]).toBe(inStore);
  });

  it('drops a task that no longer exists from the placeholder', () => {
    const map = filterTasksByLane(
      [{ id: 'a' }],
      [],
      () => new Promise<Task[]>(() => {}),
      new Map([['a', [task('deleted')]]]),
      () => {}
    );
    expect(map.get('a')).toEqual([]);
  });

  it('still settles the other lanes when one lane\'s filter call fails', async () => {
    const keep = task('k');
    const onSettled = vi.fn();
    filterTasksByLane(
      [{ id: 'ok' }, { id: 'bad' }],
      [keep],
      (lane) => (lane.id === 'ok' ? Promise.resolve([task('fresh')]) : Promise.reject(new Error('500'))),
      new Map([['bad', [keep]]]),
      onSettled
    );
    await flush();

    expect(onSettled).toHaveBeenCalledOnce();
    const settled = onSettled.mock.calls[0][0] as Map<string, Task[]>;
    expect(settled.get('ok')).toEqual([task('fresh')]);
    expect(settled.get('bad')).toEqual([keep]);
  });
});
