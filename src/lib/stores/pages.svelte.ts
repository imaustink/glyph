import { repositories } from '$lib/storage/config';
import type { DeleteSubtreeOptions, IPageRepository, WriteOptions } from '$lib/storage/interfaces';
import type { TreeNode, PageContent, TodoTriggerConfig, ProseMirrorJSONNode } from '$lib/models/types';
import { now, makeTimestamps } from '$lib/utils/time';
import { nextOrder, orderBetween, orderAfter } from '$lib/utils/order';
import { uuid } from '$lib/utils/uuid';
import { createOptimisticWriter } from './optimisticWriter';
import { collabSupported, getCollabSession, removeListItemCollaboratively } from '$lib/collab/client';

/** Recursively remove a listItem with the given nodeId from a ProseMirror JSON tree. */
function removeNodeById(node: ProseMirrorJSONNode, nodeId: string): boolean {
  if (!node.content || !Array.isArray(node.content)) return false;

  for (let i = node.content.length - 1; i >= 0; i--) {
    const child = node.content[i];
    if (child.type === 'listItem' && child.attrs?.nodeId === nodeId) {
      node.content.splice(i, 1);
      return true;
    }
    if (removeNodeById(child, nodeId)) {
      // Remove parent list if it's now empty
      if (
        (child.type === 'bulletList' || child.type === 'orderedList') &&
        (!child.content || child.content.length === 0)
      ) {
        node.content.splice(i, 1);
      }
      return true;
    }
  }
  return false;
}

export function createPagesStore(injectedRepo?: IPageRepository) {
  const repo = injectedRepo ?? repositories.pages;
  let nodes = $state<TreeNode[]>([]);
  let loaded = $state(false);
  let _loadPromise: Promise<void> | null = null;

  /** O(1) lookup index by node ID, reactively derived from nodes array. */
  let _idIndex = $derived(new Map(nodes.map(n => [n.id, n])));

  /** Set nodes array (index is automatically rebuilt via $derived). */
  function setNodes(newNodes: TreeNode[]) {
    nodes = newNodes;
  }

  async function load() {
    if (loaded || _loadPromise) return _loadPromise ?? undefined;
    _loadPromise = (async () => {
      try {
        setNodes(await repo.getAll());
        loaded = true;
      } finally {
        _loadPromise = null;
      }
    })();
    return _loadPromise;
  }

  function getChildren(parentId: string | null): TreeNode[] {
    return nodes
      .filter((n) => n.parentId === parentId)
      .sort((a, b) => a.order - b.order);
  }

  function getById(id: string): TreeNode | undefined {
    return _idIndex.get(id);
  }

  async function createPage(parentId: string | null = null, title = '', initialContent?: string, todoTrigger?: TodoTriggerConfig): Promise<TreeNode> {
    // Use Date.now() as the sort order. This is monotonically increasing per-device
    // and eliminates the read-modify-write race that occurred when two concurrent
    // calls both read max(sibling.order) before either write completes.
    // A template's defaultFolderId (or a stale caller) can name a folder that
    // has since been deleted; a page filed under it would be unreachable.
    if (parentId !== null && !_idIndex.has(parentId)) parentId = null;
    const node: TreeNode = {
      id: uuid(),
      type: 'page',
      title,
      parentId,
      order: nextOrder(),
      tags: [],
      priority: 'none',
      ...(todoTrigger ? { todoTrigger } : {}),
      isPrivate: true,
      orgId: null,
      ...makeTimestamps()
    };
    const created = await repo.create(node);
    setNodes([...nodes, created]);
    if (initialContent) {
      let contentObj: Record<string, unknown>;
      try {
        contentObj = JSON.parse(initialContent) as Record<string, unknown>;
      } catch {
        contentObj = { type: 'doc', content: [] };
      }
      await repo.saveContent({ pageId: created.id, content: contentObj, updatedAt: now() });
    }
    return created;
  }

  async function createFolder(parentId: string | null = null, title = 'New Folder'): Promise<TreeNode> {
    const node: TreeNode = {
      id: uuid(),
      type: 'folder',
      title,
      parentId,
      order: nextOrder(),
      tags: [],
      priority: 'none',
      isPrivate: true,
      orgId: null,
      ...makeTimestamps()
    };
    const created = await repo.create(node);
    setNodes([...nodes, created]);
    return created;
  }

  /** Serialized, sequenced optimistic writes per node (see optimisticWriter). */
  const _writer = createOptimisticWriter<TreeNode>({
    get: (id) => _idIndex.get(id),
    replace: (node) => setNodes(nodes.map((n) => (n.id === node.id ? node : n)))
  });

  async function updateNode(id: string, patch: Partial<Omit<TreeNode, 'id' | 'createdAt'>>): Promise<void> {
    const full = { ...patch, updatedAt: now() };
    return _writer.update(id, full, () => repo.update(id, full), () => repo.getById(id));
  }

  /**
   * Collect all descendant IDs (children, grandchildren, etc.) for a node.
   * Returns IDs in depth-first order (deepest children first) for safe deletion.
   */
  function collectDescendantIds(id: string): string[] {
    const ids: string[] = [];
    const children = getChildren(id);
    for (const child of children) {
      ids.push(...collectDescendantIds(child.id));
      ids.push(child.id);
    }
    return ids;
  }

  /**
   * Delete a node and all its descendants.
   *
   * Both storage backends handle this through `deleteSubtree()`:
   * - localStorage: removes all tree nodes in a single read-modify-write cycle
   * - API: sends a single DELETE; Postgres ON DELETE CASCADE handles descendants
   *
   * Throws on failure so the caller can surface the error to the user.
   * Returns the ids of every node that was deleted. `opts.keepTasks`: see
   * IPageRepository.deleteSubtree.
   */
  async function deleteNode(id: string, opts?: DeleteSubtreeOptions): Promise<string[]> {
    const descendantIds = collectDescendantIds(id);
    const reported = await repo.deleteSubtree(id, descendantIds, opts);

    // Update local state regardless of storage mode.
    const deletedSet = new Set([...descendantIds, id, ...(reported ?? [])]);
    setNodes(nodes.filter((n) => !deletedSet.has(n.id)));
    return [...deletedSet];
  }

  /**
   * Last revision observed per page, used as the optimistic-concurrency
   * precondition on the next write. Populated by getContent and refreshed from
   * every successful saveContent.
   */
  const knownRevisions = new Map<string, number>();

  async function getContent(pageId: string): Promise<PageContent | null> {
    const content = await repo.getContent(pageId);
    if (content && typeof content.revision === 'number') {
      knownRevisions.set(pageId, content.revision);
    }
    return content;
  }

  /**
   * Persist page content.
   *
   * Sends the revision this client last saw so the server can reject a write
   * derived from a stale read (409) rather than silently overwriting newer
   * content. On conflict the local revision is dropped and a ContentConflictError
   * is thrown so the caller can reload instead of retrying blindly — retrying
   * without a precondition is exactly the clobber this prevents.
   */
  async function saveContent(pageId: string, content: Record<string, unknown>, opts?: WriteOptions): Promise<void> {
    const expectedRevision = knownRevisions.get(pageId);
    const saved = await repo.saveContent({
      pageId,
      content,
      updatedAt: now(),
      ...(expectedRevision !== undefined ? { expectedRevision } : {})
    }, opts);
    if (saved && typeof saved.revision === 'number') {
      knownRevisions.set(pageId, saved.revision);
    } else {
      // Unknown new revision — drop the stale precondition rather than reusing
      // it, so the next write is unconditional instead of guaranteed-conflicting.
      knownRevisions.delete(pageId);
    }
  }

  /** Forget a cached revision (e.g. after a conflict forces a reload). */
  function forgetRevision(pageId: string): void {
    knownRevisions.delete(pageId);
  }

  async function moveNode(id: string, newParentId: string | null, newOrder: number): Promise<void> {
    if (newParentId !== null) {
      if (newParentId === id) {
        throw new Error('A node cannot be its own parent.');
      }
      // Collect all descendant IDs and check that the target parent is not among them.
      // This prevents creating a cycle (e.g. dragging a folder into one of its children).
      const descendants = collectDescendantIds(id);
      if (descendants.includes(newParentId)) {
        throw new Error('Cannot move a node into one of its own descendants.');
      }
    }
    await updateNode(id, { parentId: newParentId, order: newOrder });
  }

  /** Spacing used when siblings have to be renumbered, leaving room for later single-write moves. */
  const ORDER_STEP = 1024;

  /**
   * Move `draggedId` so it sits directly after `targetId`, under the target's
   * parent. Orders are always integers: the API's `order` is an int, and a
   * fractional value is rejected with a 400.
   *
   * When there is an integer gap after the target only the dragged node is
   * written. Otherwise the siblings are renumbered. Throws on failure so the
   * caller can tell the user.
   */
  async function placeAfter(draggedId: string, targetId: string): Promise<void> {
    if (draggedId === targetId) return;
    const target = _idIndex.get(targetId);
    const dragged = _idIndex.get(draggedId);
    if (!target || !dragged) return;
    const parentId = target.parentId;
    if (parentId !== null && (parentId === draggedId || collectDescendantIds(draggedId).includes(parentId))) {
      throw new Error('Cannot move a node into one of its own descendants.');
    }

    const siblings = getChildren(parentId).filter((n) => n.id !== draggedId);
    const idx = siblings.findIndex((n) => n.id === targetId);
    const next = siblings[idx + 1];
    const gapOrder = next ? orderBetween(target.order, next.order) : orderAfter(target.order);
    if (gapOrder !== null) {
      await moveNode(draggedId, parentId, gapOrder);
      return;
    }

    const ordered = [...siblings.slice(0, idx + 1), dragged, ...siblings.slice(idx + 1)];
    // The dragged node goes first: it may be changing parent, and moveNode
    // runs the cycle check before anything is written.
    await moveNode(draggedId, parentId, (ordered.indexOf(dragged) + 1) * ORDER_STEP);
    for (let i = 0; i < ordered.length; i++) {
      const n = ordered[i];
      const order = (i + 1) * ORDER_STEP;
      if (n.id === draggedId || n.order === order) continue;
      await updateNode(n.id, { order });
    }
  }

  /**
   * Remove a list item (identified by nodeId) from a page's ProseMirror content.
   * Also removes parent list nodes that become empty after the removal.
   * Returns true if the node was found and removed.
   */
  async function removeBulletByNodeId(pageId: string, nodeId: string): Promise<boolean> {
    // A collaboratively edited page can't take a whole-document write (the
    // API refuses it, since it would overwrite collaborators). Have the
    // collab service remove the bullet from the shared document instead.
    if (collabSupported && (await getCollabSession(pageId).catch(() => null))) {
      return removeListItemCollaboratively(pageId, nodeId);
    }
    const pageContent = await getContent(pageId);
    if (!pageContent?.content) return false;

    const doc = pageContent.content as unknown as ProseMirrorJSONNode;
    if (!removeNodeById(doc, nodeId)) return false;

    await saveContent(pageId, pageContent.content);
    return true;
  }

  return {
    get nodes() { return nodes; },
    get loaded() { return loaded; },
    load,
    getChildren,
    getById,
    createPage,
    createFolder,
    updateNode,
    deleteNode,
    getContent,
    saveContent,
    forgetRevision,
    moveNode,
    placeAfter,
    removeBulletByNodeId
  };
}

export const pagesStore = createPagesStore();
