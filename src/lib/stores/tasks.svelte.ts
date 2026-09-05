import { repositories } from '$lib/storage/config';
import type { ITaskRepository } from '$lib/storage/interfaces';
import type { FilterContext } from '$lib/storage/filterUtils';
import type { Task, FilterSet, Priority, TaskStatus, TreeNode } from '$lib/models/types';
import { now, makeTimestamps } from '$lib/utils/time';
import { nextOrder } from '$lib/utils/order';
import { uuid } from '$lib/utils/uuid';

export function createTasksStore(injectedRepo?: ITaskRepository) {
  const repo = injectedRepo ?? repositories.tasks;
  let tasks = $state<Task[]>([]);
  /** O(1) lookup index by task ID, reactively derived from tasks array. */
  let _idIndex = $derived(new Map(tasks.map(t => [t.id, t])));
  /** O(1) lookup index by sourceNodeId, reactively derived from tasks array. */
  let _nodeIdIndex = $derived(new Map(
    tasks.filter(t => t.sourceNodeId).map(t => [t.sourceNodeId!, t])
  ));
  /** O(1) lookup index by sourcePageId, reactively derived from tasks array. */
  const tasksByPageId = $derived(
    tasks.reduce((m, t) => {
      if (!t.sourcePageId) return m;
      const arr = m.get(t.sourcePageId) ?? [];
      m.set(t.sourcePageId, [...arr, t]);
      return m;
    }, new Map<string, Task[]>())
  );
  let loaded = $state(false);
  let _loadPromise: Promise<void> | null = null;

  /** Set tasks array (indexes are automatically rebuilt via $derived). */
  function setTasks(newTasks: Task[]) {
    tasks = newTasks;
  }

  async function load() {
    if (loaded || _loadPromise) return _loadPromise ?? undefined;
    _loadPromise = (async () => {
      try {
        setTasks(await repo.getAll());
        loaded = true;
      } finally {
        _loadPromise = null;
      }
    })();
    return _loadPromise;
  }

  function getById(id: string): Task | undefined {
    return _idIndex.get(id);
  }

  function getByNodeId(nodeId: string): Task | undefined {
    return _nodeIdIndex.get(nodeId);
  }

  function getFiltered(filterSet: FilterSet, ctx?: FilterContext): Task[] | Promise<Task[]> {
    return repo.applyFilter(tasks, filterSet, ctx);
  }

  async function createTask(params: {
    title: string;
    sourcePageId?: string;
    sourceNodeId?: string;
    priority?: Priority;
    dueDate?: string | null;
    tags?: string[];
    status?: TaskStatus;
    description?: string;
  }): Promise<Task> {
    const task: Task = {
      id: uuid(),
      title: params.title,
      description: params.description ?? '',
      status: params.status ?? 'todo',
      priority: params.priority ?? 'none',
      tags: params.tags ?? [],
      dueDate: params.dueDate ?? null,
      sourcePageId: params.sourcePageId ?? null,
      sourceNodeId: params.sourceNodeId ?? null,
      link: null,
      ...makeTimestamps(),
      order: nextOrder()
    };
    await repo.create(task);
    setTasks([...tasks, task]);
    return task;
  }

  function getByPageIdRecursive(pageId: string, allNodes: TreeNode[]): Task[] {
    // Get all descendant page IDs
    const pageIds = new Set([pageId]);
    const stack = [pageId];

    while (stack.length > 0) {
      const currentId = stack.pop()!;
      const children = allNodes.filter((n) => n.parentId === currentId);
      children.forEach((child) => {
        pageIds.add(child.id);
        stack.push(child.id);
      });
    }

    // Return all tasks associated with any of these pages
    return tasks.filter((t) => t.sourcePageId && pageIds.has(t.sourcePageId));
  }

  /** Per-task write serialization to prevent concurrent update race conditions. */
  const _taskWriteLocks = new Map<string, Promise<void>>();

  async function updateTask(id: string, patch: Partial<Omit<Task, 'id' | 'createdAt'>>): Promise<void> {
    // Optimistic update: apply immediately (synchronously) for responsive UI
    const prev = _idIndex.get(id);
    if (!prev) return;
    const optimistic = { ...prev, ...patch, updatedAt: now() };
    setTasks(tasks.map(t => t.id === id ? optimistic : t));

    // Serialize the backend write per task ID to prevent concurrent overwrites
    const prevLock = _taskWriteLocks.get(id) ?? Promise.resolve();
    const current = (async () => {
      await prevLock;
      try {
        const updated = await repo.update(id, { ...patch, updatedAt: now() });
        if (updated) {
          setTasks(tasks.map(t => t.id === id ? updated : t));
        }
      } catch (err) {
        // Rollback on failure
        setTasks(tasks.map(t => t.id === id ? prev : t));
        throw err;
      }
    })();
    // Keep the queue alive but don't let failures block future writes
    _taskWriteLocks.set(id, current.catch(() => {}));
    return current;
  }

  async function deleteTask(id: string): Promise<void> {
    await repo.delete(id);
    setTasks(tasks.filter((t) => t.id !== id));
  }

  return {
    get tasks() { return tasks; },
    get loaded() { return loaded; },
    get tasksByPage() { return tasksByPageId; },
    load,
    getById,
    getByNodeId,
    getFiltered,
    getByPageIdRecursive,
    createTask,
    updateTask,
    deleteTask
  };
}

export const tasksStore = createTasksStore();
