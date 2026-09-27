/**
 * Delete a page or folder and, if the user chose to, the tasks that came from
 * it.
 *
 * The node goes first: its delete can be refused (the API answers 409
 * subtree_has_other_owners for a folder holding other users' pages), and
 * tasks deleted before that refusal would be gone for good while the notes
 * whose bullets point at them stay. So the tasks are only touched once the
 * node delete succeeded. `taskIds` must be worked out before calling: once the
 * node is gone its tasks can no longer be found by page (the API soft-deletes
 * them in the same transaction as the pages; deleting one again is a 404,
 * which the task store treats as done).
 *
 * A node-delete failure propagates with no task touched. A task that can't be
 * deleted afterwards doesn't undo the node delete; its id is reported so the
 * caller can tell the user.
 *
 * Keeping the tasks (the user had tasks to choose about and chose to keep
 * them) must be asked of the node delete: the API otherwise deletes a
 * note's tasks with it. It then detaches them into standalone tasks, which
 * are re-read (`refreshTask`) so the local copies stop pointing at the
 * deleted note. With no tasks to choose about, the delete is the default.
 */
export async function deleteTreeNode(params: {
  nodeId: string;
  taskIds: string[];
  deleteTasks: boolean;
  deleteNode: (id: string, opts: { keepTasks: boolean }) => Promise<string[]>;
  deleteTask: (id: string) => Promise<void>;
  refreshTask?: (id: string) => Promise<unknown>;
}): Promise<{ deletedIds: string[]; failedTaskIds: string[] }> {
  const keepTasks = !params.deleteTasks && params.taskIds.length > 0;
  const deletedIds = await params.deleteNode(params.nodeId, { keepTasks });
  if (!params.deleteTasks) {
    if (keepTasks && params.refreshTask) {
      // Best effort: the tasks are kept either way; a failed re-read only
      // leaves a stale local copy until the next load.
      await Promise.allSettled(params.taskIds.map((id) => params.refreshTask!(id)));
    }
    return { deletedIds, failedTaskIds: [] };
  }

  const results = await Promise.allSettled(params.taskIds.map((id) => params.deleteTask(id)));
  const failedTaskIds = params.taskIds.filter((_, i) => results[i].status === 'rejected');
  return { deletedIds, failedTaskIds };
}
