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
 */
export async function deleteTreeNode(params: {
  nodeId: string;
  taskIds: string[];
  deleteTasks: boolean;
  deleteNode: (id: string) => Promise<string[]>;
  deleteTask: (id: string) => Promise<void>;
}): Promise<{ deletedIds: string[]; failedTaskIds: string[] }> {
  const deletedIds = await params.deleteNode(params.nodeId);
  if (!params.deleteTasks) return { deletedIds, failedTaskIds: [] };

  const results = await Promise.allSettled(params.taskIds.map((id) => params.deleteTask(id)));
  const failedTaskIds = params.taskIds.filter((_, i) => results[i].status === 'rejected');
  return { deletedIds, failedTaskIds };
}
