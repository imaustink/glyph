// ─── Auth Types ───────────────────────────────────────────────────────────────

export interface CurrentUser {
  id: string;
  email: string;
  name: string;
}

// ─── Core Domain Types ────────────────────────────────────────────────────────

export type Priority = 'urgent' | 'high' | 'medium' | 'low' | 'none';
export type TaskStatus = 'todo' | 'in-progress' | 'done' | 'cancelled';

export interface Tag {
  id: string;
  name: string;
  color?: string;
}

export interface LinkMeta {
  url: string;
  title: string | null;
  description: string | null;
  image: string | null;
  favicon: string | null;
  siteName: string | null;
}

export interface Task {
  id: string;
  title: string;
  description: string;
  status: TaskStatus;
  priority: Priority;
  tags: string[]; // tag names
  dueDate: string | null; // ISO date string
  sourcePageId: string | null; // page where bullet lives
  sourceNodeId: string | null; // stable nodeId in ProseMirror doc
  link: LinkMeta | null; // external resource link with unfurled metadata
  createdAt: string; // ISO datetime
  updatedAt: string; // ISO datetime
  order: number; // for manual lane ordering
  /** Owner of this task. */
  userId?: string;
  /** Org this task is shared with (null = not shared with any org). */
  orgId?: string | null;
  /** When true, the task is hidden from org members. */
  isPrivate?: boolean;
  /** Folder this standalone task is assigned to (null = personal only). */
  folderId?: string | null;
}

export type TreeNodeType = 'page' | 'folder';

export interface TreeNode {
  id: string;
  type: TreeNodeType;
  title: string;
  parentId: string | null;
  order: number;
  tags: string[];
  /** Priority of this note. Drives task-board ordering (note priority first, then task priority). Defaults to 'none'. */
  priority?: Priority;
  /** Owner of this node. */
  userId?: string;
  /** Configures which heading/block pattern activates TODO bullet → task creation on this page. */
  todoTrigger?: TodoTriggerConfig;
  /** Org this page is shared with (null = not shared with any org). */
  orgId?: string | null;
  /** When true, the page is hidden from org members even if orgId is set. */
  isPrivate?: boolean;
  createdAt: string;
  updatedAt: string;
  // content stored separately to keep tree lightweight
}

export interface PageContent {
  pageId: string;
  content: Record<string, unknown>; // ProseMirror JSON document (not stringified)
  updatedAt: string;
  /**
   * Schema version of the serialized ProseMirror document.
   * Absent or 0 means "pre-versioning era" (treated as version 1).
   * Increment CURRENT_SCHEMA_VERSION whenever the ProseMirror schema changes
   * in a way that requires migrating existing documents.
   */
  schemaVersion?: number;
  /** Set to true when an automatic schema migration fails. UI should show a warning. */
  migrationFailed?: boolean;
}

// ─── ProseMirror JSON Types ───────────────────────────────────────────────────
// Lightweight types for manipulating serialized ProseMirror documents.
// These mirror the JSON structure from doc.toJSON() without pulling in
// the full @tiptap/pm dependencies.

export interface ProseMirrorJSONMark {
  type: string;
  attrs?: Record<string, unknown>;
}

export interface ProseMirrorJSONNode {
  type: string;
  attrs?: Record<string, unknown>;
  content?: ProseMirrorJSONNode[];
  marks?: ProseMirrorJSONMark[];
  text?: string;
}

// ─── Lane / Board Types ───────────────────────────────────────────────────────

export type FilterOperator =
  | 'any'
  | 'eq'
  | 'neq'
  | 'in'
  | 'not_in'
  | 'contains'
  | 'before'
  | 'after'
  | 'exists'
  | 'not_exists';

export type FilterConjunction = 'and' | 'or';

/** The set of value types a filter rule can hold. */
export type FilterValue = string | string[] | number | boolean | null;

/**
 * Fields a filter rule can target. This is any direct field of a Task, plus
 * synthetic/computed fields that are resolved from related resources:
 * - `sourcePageTags`: the tags of the note (source page) a task was created from.
 */
export type TaskFilterField = keyof Task | 'sourcePageTags';

export interface FilterRule {
  id: string;
  field: TaskFilterField;
  operator: FilterOperator;
  value: FilterValue;
}

export interface FilterSet {
  conjunction: FilterConjunction;
  rules: FilterRule[];
}

export type SortMode = 'auto' | 'field' | 'manual';
export type SortDirection = 'asc' | 'desc';

export interface SortConfig {
  mode: SortMode;
  field?: keyof Task;
  direction?: SortDirection;
  // for manual mode: explicit task IDs in order
  taskOrder?: string[];
}

/**
 * Optional context passed to a SortProvider so it can resolve values from
 * related resources that aren't stored on the Task itself.
 */
export interface SortContext {
  /**
   * Resolve the priority of a task's source note (page). Providers use this to
   * order tasks by their originating note's priority before the task's own
   * priority. Returns 'none' when the task has no linked note or the note has
   * no priority set.
   */
  getNotePriority?: (task: Task) => Priority;
}

export interface Lane {
  id: string;
  title: string;
  filterSet: FilterSet;
  sortConfig: SortConfig;
  order: number;
  /** Folder this lane is scoped to. null = personal board lane. */
  folderId?: string | null;
  createdAt: string;
  updatedAt: string;
}

// ─── Search Types ─────────────────────────────────────────────────────────────

export type SearchResultType = 'page' | 'task';

export interface SearchResult {
  id: string;
  type: SearchResultType;
  title: string;
  excerpt: string;
  score: number;
  // highlighted segments: [start, end] pairs
  highlights: [number, number][];
}

// ─── UI State ─────────────────────────────────────────────────────────────────

export interface PendingTaskCreation {
  nodeId: string;
  bulletText: string;
  pageId: string;
  /** Called with the new task ID once creation is confirmed, or null if dismissed */
  resolve: (taskId: string | null) => void;
}

// ─── Template Types ───────────────────────────────────────────────────────────

export type TodoTriggerMatchMode = 'exact' | 'regex';

export interface TodoTriggerConfig {
  /** Text to match against the block's content. For 'exact', compared case-insensitively. For 'regex', used as a RegExp source (case-sensitive unless the pattern includes flags). */
  pattern: string;
  matchMode: TodoTriggerMatchMode;
  /**
   * ProseMirror node type names to check. Use 'any' to match any block type.
   * Defaults to ['heading'] when absent or empty.
   */
  blockTypes: string[];
}

export const DEFAULT_TODO_TRIGGER: TodoTriggerConfig = {
  pattern: 'TODO',
  matchMode: 'exact',
  blockTypes: ['heading']
};

export interface NoteTemplate {
  id: string;
  name: string;
  content: string; // ProseMirror JSON serialized as string
  titleTemplate: string; // expression string with {{tokens}}, e.g. "{{date-long}}"
  /** Configures which block pattern activates TODO bullet → task creation for pages created from this template. */
  todoTrigger?: TodoTriggerConfig;
  /** Folder ID where new notes from this template are created. null = root. */
  defaultFolderId?: string | null;
  isDefault: boolean;
  createdAt: string;
  updatedAt: string;
  /** Owner of this template. */
  userId?: string;
  /** Org this template is shared with (null = not shared with any org). */
  orgId?: string | null;
  /** When true, the template is hidden from org members. */
  isPrivate?: boolean;
}

// ─── Organizations ────────────────────────────────────────────────────────────

export type OrgRole = 'owner' | 'editor' | 'viewer';

export interface Organization {
  id: string;
  name: string;
  createdBy: string;
  memberCount: number;
  createdAt: string;
  updatedAt: string;
}

export interface OrgWithRole extends Organization {
  role: OrgRole;
}

export interface OrgMember {
  orgId: string;
  userId: string;
  email: string | null;
  name: string | null;
  role: OrgRole;
  joinedAt: string;
}

// ─── Sharing ──────────────────────────────────────────────────────────────────

export type SharePermission = 'viewer' | 'editor';
export type ShareResourceType = 'page' | 'task' | 'template' | 'folder';

export interface ShareUser {
  id: string;
  email: string | null;
  name: string | null;
}

export interface Share {
  id: string;
  resourceType: ShareResourceType;
  resourceId: string;
  sharedById: string;
  sharedWith: ShareUser;
  permission: SharePermission;
  createdAt: string;
}

export interface UserSearchResult {
  id: string;
  email: string | null;
  name: string | null;
}

// ─── Storage Interfaces ───────────────────────────────────────────────────────

export interface StorageAdapter {
  get<T>(key: string): Promise<T | null>;
  set<T>(key: string, value: T): Promise<void>;
  remove(key: string): Promise<void>;
  keys(): Promise<string[]>;
  clear(): Promise<void>;
}

// ─── Sort Provider Interface ──────────────────────────────────────────────────
// NOTE: The primary SortProvider interface is in src/lib/sort/types.ts.
// This re-export maintains backward compatibility for any existing imports.
export type { SortProvider } from '$lib/sort/types';

// ─── Search Provider Interface ────────────────────────────────────────────────

export interface SearchableItem {
  id: string;
  type: SearchResultType;
  title: string;
  body: string;
  tags?: string[];
}

export interface SearchProvider {
  /** True if search is async (e.g. AI-powered). Show loading indicator when true. */
  isAsync: boolean;
  index(items: SearchableItem[]): Promise<void>;
  search(query: string, options?: SearchOptions): Promise<SearchResult[]>;
}

export interface SearchOptions {
  types?: SearchResultType[];
  limit?: number;
}
