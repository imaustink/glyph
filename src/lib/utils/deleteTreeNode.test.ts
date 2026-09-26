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

    expect(deleteNode).toHaveBeenCalledWith('folder');
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

    const result = await deleteTreeNode({ nodeId: 'page', taskIds: ['t1'], deleteTasks: false, deleteNode, deleteTask });

    expect(deleteTask).not.toHaveBeenCalled();
    expect(result).toEqual({ deletedIds: ['page'], failedTaskIds: [] });
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
