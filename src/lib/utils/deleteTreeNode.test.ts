/**
 * The sidebar's delete sequence: a page or folder is deleted first, and the
 * tasks the user chose to delete with it go only once that succeeded. The
 * delete can be refused (the API answers 409 subtree_has_other_owners for a
 * folder holding other users' pages), and tasks deleted before that refusal
 * would be gone while the notes pointing at them stay.
 */

import { describe, it, expect, vi } from 'vitest';
import { deleteTreeNode } from './deleteTreeNode';
import { ApiError } from '$lib/storage/apiClient';

describe('deleteTreeNode', () => {
  it('leaves every task alone when the node delete is refused', async () => {
    const refusal = new ApiError(409, 'DELETE', '/api/v1/pages/folder', { code: 'subtree_has_other_owners' });
    const deleteNode = vi.fn().mockRejectedValue(refusal);
    const deleteTask = vi.fn().mockResolvedValue(undefined);

    await expect(
      deleteTreeNode({ nodeId: 'folder', taskIds: ['t1', 't2'], deleteTasks: true, deleteNode, deleteTask })
    ).rejects.toBe(refusal);

    expect(deleteNode).toHaveBeenCalledWith('folder', { keepTasks: false });
    expect(deleteTask).not.toHaveBeenCalled();
  });

  it('deletes the chosen tasks only after the node is gone', async () => {
    const order: string[] = [];
    const deleteNode = vi.fn().mockImplementation(async (id: string) => {
      order.push(`node:${id}`);
      return [id, 'child'];
    });
    const deleteTask = vi.fn().mockImplementation(async (id: string) => {
      order.push(`task:${id}`);
    });

    const result = await deleteTreeNode({ nodeId: 'folder', taskIds: ['t1', 't2'], deleteTasks: true, deleteNode, deleteTask });

    expect(order[0]).toBe('node:folder');
    expect(order.slice(1).sort()).toEqual(['task:t1', 'task:t2']);
    expect(result).toEqual({ deletedIds: ['folder', 'child'], failedTaskIds: [] });
  });

  it('keeps the tasks when the user chose to', async () => {
    const deleteNode = vi.fn().mockResolvedValue(['page']);
    const deleteTask = vi.fn();
    const refreshTask = vi.fn().mockResolvedValue(true);

    const result = await deleteTreeNode({
      nodeId: 'page', taskIds: ['t1', 't2'], deleteTasks: false, deleteNode, deleteTask, refreshTask
    });

    // The API soft-deletes a deleted note's tasks unless told to keep them.
    expect(deleteNode).toHaveBeenCalledWith('page', { keepTasks: true });
    expect(deleteTask).not.toHaveBeenCalled();
    // The kept tasks are now standalone on the server: re-read them.
    expect(refreshTask.mock.calls.map((c) => c[0]).sort()).toEqual(['t1', 't2']);
    expect(result).toEqual({ deletedIds: ['page'], failedTaskIds: [] });
  });

  it('refreshes no task when the kept-task delete is refused', async () => {
    const deleteNode = vi.fn().mockRejectedValue(new Error('refused'));
    const refreshTask = vi.fn();

    await expect(
      deleteTreeNode({ nodeId: 'page', taskIds: ['t1'], deleteTasks: false, deleteNode, deleteTask: vi.fn(), refreshTask })
    ).rejects.toThrow('refused');
    expect(refreshTask).not.toHaveBeenCalled();
  });

  it('does not survive a failed refresh of a kept task', async () => {
    const deleteNode = vi.fn().mockResolvedValue(['page']);
    const refreshTask = vi.fn().mockRejectedValue(new Error('offline'));

    const result = await deleteTreeNode({
      nodeId: 'page', taskIds: ['t1'], deleteTasks: false, deleteNode, deleteTask: vi.fn(), refreshTask
    });
    expect(result).toEqual({ deletedIds: ['page'], failedTaskIds: [] });
  });

  it('uses the default delete when there were no tasks to choose about', async () => {
    const deleteNode = vi.fn().mockResolvedValue(['folder']);

    await deleteTreeNode({ nodeId: 'folder', taskIds: [], deleteTasks: false, deleteNode, deleteTask: vi.fn() });

    expect(deleteNode).toHaveBeenCalledWith('folder', { keepTasks: false });
  });

  it('reports the tasks it could not delete once the node is gone, without failing the delete', async () => {
    const deleteNode = vi.fn().mockResolvedValue(['page']);
    const deleteTask = vi.fn().mockImplementation(async (id: string) => {
      if (id === 't2') throw new Error('network down');
    });

    const result = await deleteTreeNode({ nodeId: 'page', taskIds: ['t1', 't2', 't3'], deleteTasks: true, deleteNode, deleteTask });

    expect(deleteTask).toHaveBeenCalledTimes(3);
    expect(result).toEqual({ deletedIds: ['page'], failedTaskIds: ['t2'] });
  });
});
