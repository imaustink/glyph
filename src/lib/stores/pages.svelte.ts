import { repositories } from '$lib/storage/config';
import type { IPageRepository } from '$lib/storage/interfaces';
import type { TreeNode, PageContent, TodoTriggerConfig, ProseMirrorJSONNode } from '$lib/models/types';
import { now, makeTimestamps } from '$lib/utils/time';
import { nextOrder } from '$lib/utils/order';
import { uuid } from '$lib/utils/uuid';

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
    const node: TreeNode = {
      id: uuid(),
      type: 'page',
      title,
      parentId,
      order: nextOrder(),
      tags: [],
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
      isPrivate: true,
      orgId: null,
      ...makeTimestamps()
    };
    const created = await repo.create(node);
    setNodes([...nodes, created]);
    return created;
  }

  async function updateNode(id: string, patch: Partial<Omit<TreeNode, 'id' | 'createdAt'>>): Promise<void> {
    // Optimistic update: apply immediately, rollback on failure
    const prev = _idIndex.get(id);
    if (!prev) return;
    const optimistic = { ...prev, ...patch, updatedAt: now() };
    setNodes(nodes.map(n => n.id === id ? optimistic : n));

    try {
      const updated = await repo.update(id, { ...patch, updatedAt: now() });
      if (updated) {
        setNodes(nodes.map(n => n.id === id ? updated : n));
      }
    } catch (err) {
      // Rollback on failure
      setNodes(nodes.map(n => n.id === id ? prev : n));
      throw err;
    }
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
   */
  async function deleteNode(id: string): Promise<void> {
    const descendantIds = collectDescendantIds(id);
    await repo.deleteSubtree(id, descendantIds);

    // Update local state regardless of storage mode.
    const deletedSet = new Set([...descendantIds, id]);
    setNodes(nodes.filter((n) => !deletedSet.has(n.id)));
  }

  async function getContent(pageId: string): Promise<PageContent | null> {
    return repo.getContent(pageId);
  }

  async function saveContent(pageId: string, content: Record<string, unknown>): Promise<void> {
    await repo.saveContent({ pageId, content, updatedAt: now() });
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

  /**
   * Remove a list item (identified by nodeId) from a page's ProseMirror content.
   * Also removes parent list nodes that become empty after the removal.
   * Returns true if the node was found and removed.
   */
  async function removeBulletByNodeId(pageId: string, nodeId: string): Promise<boolean> {
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
    moveNode,
    removeBulletByNodeId
  };
}

export const pagesStore = createPagesStore();
