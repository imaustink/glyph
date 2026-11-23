import { describe, expect, it, vi } from 'vitest';
import { filterTasksByLane } from './laneFilter';
import type { Task } from '$lib/models/types';

const task = (id: string) => ({ id }) as Task;

describe('filterTasksByLane (#101)', () => {
  it('returns sync results immediately and does not call onSettled', () => {
    const onSettled = vi.fn();
    const map = filterTasksByLane([{ id: 'a' }], () => [task('1')], new Map(), onSettled);
    expect(map.get('a')).toEqual([task('1')]);
    expect(onSettled).not.toHaveBeenCalled();
  });

  it('keeps an async lane\'s previous tasks until its filter resolves', async () => {
    let resolve!: (t: Task[]) => void;
    const pending = new Promise<Task[]>((r) => { resolve = r; });
    const previous = new Map([['b', [task('old')]]]);
    const onSettled = vi.fn();

    const map = filterTasksByLane(
      [{ id: 'a' }, { id: 'b' }],
      (lane) => (lane.id === 'a' ? [task('1')] : pending),
      previous,
      onSettled
    );

    expect(map.get('a')).toEqual([task('1')]);
    expect(map.get('b')).toEqual([task('old')]);

    resolve([task('new')]);
    await pending;
    await Promise.resolve();
    await Promise.resolve();

    expect(onSettled).toHaveBeenCalledOnce();
    const settled = onSettled.mock.calls[0][0] as Map<string, Task[]>;
    expect(settled.get('a')).toEqual([task('1')]);
    expect(settled.get('b')).toEqual([task('new')]);
  });

  it('drops lanes that no longer exist', () => {
    const previous = new Map([['gone', [task('x')]]]);
    const map = filterTasksByLane([{ id: 'a' }], () => new Promise<Task[]>(() => {}), previous, () => {});
    expect(map.has('gone')).toBe(false);
    expect(map.has('a')).toBe(false);
  });
});
