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

export interface IRepository<T extends { id: string }> {
  getAll(): Promise<T[]>;
  getById(id: string): Promise<T | null>;
  create(item: T): Promise<T>;
  update(id: string, patch: Partial<Omit<T, 'id'>>): Promise<T | null>;
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
  saveContent(content: PageContent): Promise<PageContent | void>;
  deleteContent(pageId: string): Promise<void>;
  deleteWithContent(id: string): Promise<boolean>;
  deleteSubtree(id: string, descendantIds: string[]): Promise<void>;
  getTree(nodes: TreeNode[]): TreeNode[];
  getChildren(nodes: TreeNode[], parentId: string): TreeNode[];
}

// ─── Task Repository Interface ────────────────────────────────────────────────

export interface ITaskRepository extends IRepository<Task> {
  getByPageId(pageId: string): Promise<Task[]>;
  getByNodeId(nodeId: string): Promise<Task | null>;
  applyFilter(tasks: Task[], filterSet: FilterSet, ctx?: FilterContext): Task[] | Promise<Task[]>;
}

// ─── Lane Repository Interface ────────────────────────────────────────────────

export interface ILaneRepository extends IRepository<Lane> {
  getOrdered(): Promise<Lane[]>;
  reorderAll(orderedIds: string[], updatedAt: string): Promise<void>;
  createBatch?(items: Lane[]): Promise<Lane[]>;
}

// ─── Template Repository Interface ────────────────────────────────────────────

export interface ITemplateRepository extends IRepository<NoteTemplate> {}
