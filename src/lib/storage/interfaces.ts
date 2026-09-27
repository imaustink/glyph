/**
 * Repository interfaces — the contracts all storage implementations must satisfy.
 *
 * Both localStorage-backed and API-backed repositories implement these interfaces.
 * Stores import only the interfaces, never concrete classes, enabling seamless
 * backend swaps without code changes in the consumer layer.
 */

import type { TreeNode, PageContent, Task, Lane, NoteTemplate, FilterSet } from '$lib/models/types';
import type { FilterContext } from '$lib/storage/filterUtils';

// ─── Base Repository Interface ────────────────────────────────────────────────

/** Options for a single write. */
export interface WriteOptions {
  /** Send so it survives the page unloading (API: fetch keepalive). */
  keepalive?: boolean;
  /**
   * Where the change comes from. 'bullet': a task title taken from its
   * bullet's text. The API treats any other title change as a rename from
   * outside the note and puts it into the bullet (DI-29); an echo of the
   * bullet's own text could overwrite what was typed since.
   */
  source?: 'bullet';
}

export interface IRepository<T extends { id: string }> {
  getAll(): Promise<T[]>;
  getById(id: string): Promise<T | null>;
  create(item: T): Promise<T>;
  update(id: string, patch: Partial<Omit<T, 'id'>>, opts?: WriteOptions): Promise<T | null>;
  delete(id: string): Promise<boolean>;
  upsert(item: T): Promise<T>;
  deleteMany?(ids: string[]): Promise<void>;
  updateMany?(patches: Map<string, Partial<Omit<T, 'id'>>>): Promise<void>;
}

// ─── Page Repository Interface ────────────────────────────────────────────────

export interface IPageRepository extends IRepository<TreeNode> {
  getContent(pageId: string): Promise<PageContent | null>;
  /**
   * Persist page content. Returns the stored record (including the new
   * `revision`) so callers can hold an up-to-date optimistic-concurrency
   * precondition for the next write.
   */
  saveContent(content: PageContent, opts?: WriteOptions): Promise<PageContent | void>;
  deleteContent(pageId: string): Promise<void>;
  deleteWithContent(id: string): Promise<boolean>;
  /**
   * Delete a node and its descendants. `descendantIds` are the ones the
   * caller knows about; a local implementation may find more (e.g. created
   * by another tab). Returns every id it deleted when it knows them.
   *
   * `keepTasks`: the user chose to keep the subtree's tasks. The API deletes
   * them with their notes unless asked to keep them (it then detaches them
   * into standalone tasks); local storage never touches tasks here.
   */
  deleteSubtree(id: string, descendantIds: string[], opts?: DeleteSubtreeOptions): Promise<void | string[]>;
  getTree(nodes: TreeNode[]): TreeNode[];
  getChildren(nodes: TreeNode[], parentId: string): TreeNode[];
}

export interface DeleteSubtreeOptions {
  keepTasks?: boolean;
}

// ─── Task Repository Interface ────────────────────────────────────────────────

/** The bullet a task is moved to when its bullet is pasted into another note. */
export interface TaskBullet {
  sourcePageId: string;
  sourceNodeId: string;
}

export interface ITaskRepository extends IRepository<Task> {
  getByPageId(pageId: string): Promise<Task[]>;
  getByNodeId(nodeId: string): Promise<Task | null>;
  applyFilter(tasks: Task[], filterSet: FilterSet, ctx?: FilterContext): Task[] | Promise<Task[]>;
  /**
   * API mode: ask the server to move the task onto `dest` (POST
   * /tasks/:id/adopt). It only does so for a task whose bullet has left its
   * note; otherwise it rejects (409 "source_live" while the bullet is still
   * there). Absent in localStorage mode, where tasksStore moves the task
   * itself.
   */
  adopt?(id: string, dest: TaskBullet): Promise<Task>;
}

// ─── Lane Repository Interface ────────────────────────────────────────────────

export interface ILaneRepository extends IRepository<Lane> {
  getOrdered(): Promise<Lane[]>;
  reorderAll(orderedIds: string[], updatedAt: string): Promise<void>;
  createBatch?(items: Lane[]): Promise<Lane[]>;
  /**
   * First-run seeding in one atomic step: create `items` only if there are
   * no (global) lanes in storage yet, and return what storage holds after.
   */
  seedIfEmpty?(items: Lane[]): Promise<Lane[]>;
}

// ─── Template Repository Interface ────────────────────────────────────────────

export interface ITemplateRepository extends IRepository<NoteTemplate> {
  /** First-run seeding in one atomic step (see ILaneRepository.seedIfEmpty). */
  seedIfEmpty?(items: NoteTemplate[]): Promise<NoteTemplate[]>;
}
