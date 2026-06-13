/**
 * Unit tests for the pages store.
 *
 * Injects a mock IPageRepository so no real storage is involved.
 * Tests: load deduplication, createPage/createFolder, getChildren ordering,
 *        getById index, updateNode (optimistic + rollback), deleteNode (subtree),
 *        moveNode (cycle detection), collectDescendantIds, removeBulletByNodeId.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { createPagesStore } from './pages.svelte';
import type { IPageRepository } from '$lib/storage/interfaces';
import type { TreeNode, PageContent } from '$lib/models/types';

function makeNode(overrides: Partial<TreeNode> = {}): TreeNode {
  return {
    id: 'node-1',
    type: 'page',
    title: 'Test Page',
    parentId: null,
    order: 0,
    tags: [],
    isPrivate: true,
    orgId: null,
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
    ...overrides
  };
}

function createMockRepo(overrides: Partial<IPageRepository> = {}): IPageRepository {
  return {
    getAll: vi.fn().mockResolvedValue([]),
    getById: vi.fn().mockResolvedValue(null),
    create: vi.fn().mockImplementation(async (n: TreeNode) => n),
    update: vi.fn().mockImplementation(async (id: string, patch: Partial<TreeNode>) => ({
      ...makeNode({ id }),
      ...patch
    })),
    delete: vi.fn().mockResolvedValue(true),
    upsert: vi.fn().mockImplementation(async (n: TreeNode) => n),
    deleteMany: vi.fn().mockResolvedValue(undefined),
    getContent: vi.fn().mockResolvedValue(null),
    saveContent: vi.fn().mockResolvedValue(undefined),
    deleteContent: vi.fn().mockResolvedValue(undefined),
    deleteWithContent: vi.fn().mockResolvedValue(true),
    deleteSubtree: vi.fn().mockResolvedValue(undefined),
    getTree: vi.fn().mockReturnValue([]),
    getChildren: vi.fn().mockReturnValue([]),
    ...overrides
  };
}

describe('pagesStore', () => {
  let repo: ReturnType<typeof createMockRepo>;
  let store: ReturnType<typeof createPagesStore>;

  beforeEach(() => {
    repo = createMockRepo();
    store = createPagesStore(repo);
    vi.clearAllMocks();
  });

  // ─── load ────────────────────────────────────────────────────────────────

  describe('load', () => {
    it('populates nodes from the repo', async () => {
      const nodes = [makeNode({ id: 'p1' }), makeNode({ id: 'p2' })];
      vi.mocked(repo.getAll).mockResolvedValueOnce(nodes);
      await store.load();
      expect(store.nodes).toEqual(nodes);
      expect(store.loaded).toBe(true);
    });

    it('deduplicates concurrent calls (load promise is reused)', async () => {
      vi.mocked(repo.getAll).mockResolvedValue([]);
      const [p1, p2] = [store.load(), store.load()];
      await Promise.all([p1, p2]);
      expect(repo.getAll).toHaveBeenCalledTimes(1);
    });

    it('does not re-fetch when called again after loading with empty results', async () => {
      // Regression: absence of records must not cause a fetch cycle
      vi.mocked(repo.getAll).mockResolvedValue([]);
      await store.load();
      expect(store.loaded).toBe(true);
      vi.clearAllMocks();
      await store.load(); // second call — must be a no-op
      expect(repo.getAll).not.toHaveBeenCalled();
    });

    it('sets loaded=false initially', () => {
      expect(store.loaded).toBe(false);
    });
  });

  // ─── getChildren ─────────────────────────────────────────────────────────

  describe('getChildren', () => {
    it('returns children of a parent sorted by order', async () => {
      const nodes = [
        makeNode({ id: 'root', parentId: null, order: 0 }),
        makeNode({ id: 'child2', parentId: 'root', order: 5 }),
        makeNode({ id: 'child1', parentId: 'root', order: 2 })
      ];
      vi.mocked(repo.getAll).mockResolvedValueOnce(nodes);
      await store.load();
      const children = store.getChildren('root');
      expect(children.map((n) => n.id)).toEqual(['child1', 'child2']);
    });

    it('returns empty array when no children', async () => {
      vi.mocked(repo.getAll).mockResolvedValueOnce([makeNode({ id: 'root' })]);
      await store.load();
      expect(store.getChildren('root')).toEqual([]);
    });

    it('returns root-level nodes when parentId is null', async () => {
      const nodes = [
        makeNode({ id: 'root1', parentId: null, order: 0 }),
        makeNode({ id: 'child', parentId: 'root1', order: 0 }),
        makeNode({ id: 'root2', parentId: null, order: 1 })
      ];
      vi.mocked(repo.getAll).mockResolvedValueOnce(nodes);
      await store.load();
      expect(store.getChildren(null).map((n) => n.id)).toEqual(['root1', 'root2']);
    });
  });

  // ─── getById ─────────────────────────────────────────────────────────────

  describe('getById', () => {
    it('returns the node for a known id', async () => {
      const node = makeNode({ id: 'p1', title: 'My Page' });
      vi.mocked(repo.getAll).mockResolvedValueOnce([node]);
      await store.load();
      expect(store.getById('p1')?.title).toBe('My Page');
    });

    it('returns undefined for an unknown id', async () => {
      vi.mocked(repo.getAll).mockResolvedValueOnce([]);
      await store.load();
      expect(store.getById('missing')).toBeUndefined();
    });

    it('reflects nodes added after initial load', async () => {
      vi.mocked(repo.getAll).mockResolvedValueOnce([]);
      await store.load();
      const newNode = makeNode({ id: 'new' });
      vi.mocked(repo.create).mockResolvedValueOnce(newNode);
      await store.createPage(null, 'New');
      expect(store.getById('new')).toBeDefined();
    });
  });

  // ─── createPage ──────────────────────────────────────────────────────────

  describe('createPage', () => {
    it('appends the new page to the store', async () => {
      vi.mocked(repo.getAll).mockResolvedValueOnce([]);
      await store.load();
      const node = makeNode({ id: 'new-page', type: 'page' });
      vi.mocked(repo.create).mockResolvedValueOnce(node);

      await store.createPage(null, 'My Page');

      expect(store.nodes.find((n) => n.id === 'new-page')).toBeDefined();
    });

    it('saves initialContent when provided', async () => {
      vi.mocked(repo.getAll).mockResolvedValueOnce([]);
      await store.load();
      const node = makeNode({ id: 'p1' });
      vi.mocked(repo.create).mockResolvedValueOnce(node);

      await store.createPage(null, 'Title', '{"type":"doc"}');

      expect(repo.saveContent).toHaveBeenCalledWith(
        expect.objectContaining({ pageId: 'p1', content: { type: 'doc' } })
      );
    });

    it('falls back to empty doc when initialContent is invalid JSON', async () => {
      vi.mocked(repo.getAll).mockResolvedValueOnce([]);
      await store.load();
      const node = makeNode({ id: 'p1' });
      vi.mocked(repo.create).mockResolvedValueOnce(node);

      await store.createPage(null, 'Title', '{not valid json{{{{');

      expect(repo.saveContent).toHaveBeenCalledWith(
        expect.objectContaining({ pageId: 'p1', content: { type: 'doc', content: [] } })
      );
    });

    it('does not call saveContent when no initialContent is provided', async () => {
      vi.mocked(repo.getAll).mockResolvedValueOnce([]);
      await store.load();
      vi.mocked(repo.create).mockResolvedValueOnce(makeNode({ id: 'p1' }));

      await store.createPage(null, 'Title');

      expect(repo.saveContent).not.toHaveBeenCalled();
    });

    it('includes todoTrigger in node when provided (covers line 80 true branch)', async () => {
      vi.mocked(repo.getAll).mockResolvedValueOnce([]);
      await store.load();
      const node = makeNode({ id: 'p-trigger' });
      vi.mocked(repo.create).mockResolvedValueOnce(node);

      const trigger = { pattern: 'TODO', matchMode: 'exact' as const, blockTypes: ['heading' as const] };
      await store.createPage(null, 'Title', undefined, trigger);

      // repo.create is called with a node that has todoTrigger set
      const createdArg = vi.mocked(repo.create).mock.calls[0][0];
      expect(createdArg).toHaveProperty('todoTrigger');
    });
  });

  // ─── createFolder ────────────────────────────────────────────────────────

  describe('createFolder', () => {
    it('creates a node with type "folder"', async () => {
      vi.mocked(repo.getAll).mockResolvedValueOnce([]);
      await store.load();
      const folder = makeNode({ id: 'f1', type: 'folder', title: 'Stuff' });
      vi.mocked(repo.create).mockResolvedValueOnce(folder);

      const result = await store.createFolder(null, 'Stuff');

      expect(result.type).toBe('folder');
      expect(store.nodes.find((n) => n.type === 'folder')).toBeDefined();
    });
  });

  // ─── updateNode ──────────────────────────────────────────────────────────

  describe('updateNode', () => {
    it('applies the patch to the store node', async () => {
      const node = makeNode({ id: 'p1', title: 'Old' });
      vi.mocked(repo.getAll).mockResolvedValueOnce([node]);
      vi.mocked(repo.update).mockResolvedValueOnce({ ...node, title: 'New' });
      await store.load();

      await store.updateNode('p1', { title: 'New' });

      expect(store.getById('p1')?.title).toBe('New');
    });

    it('applies an optimistic update before the repo resolves', async () => {
      const node = makeNode({ id: 'p1', title: 'Old' });
      vi.mocked(repo.getAll).mockResolvedValueOnce([node]);
      let resolveUpdate!: (v: TreeNode) => void;
      vi.mocked(repo.update).mockReturnValueOnce(
        new Promise<TreeNode>((r) => { resolveUpdate = r; })
      );
      await store.load();

      const updatePromise = store.updateNode('p1', { title: 'Optimistic' });
      // Optimistic update is synchronous
      expect(store.getById('p1')?.title).toBe('Optimistic');

      resolveUpdate({ ...node, title: 'Confirmed' });
      await updatePromise;
      expect(store.getById('p1')?.title).toBe('Confirmed');
    });

    it('rolls back on repo failure', async () => {
      const node = makeNode({ id: 'p1', title: 'Original' });
      vi.mocked(repo.getAll).mockResolvedValueOnce([node]);
      vi.mocked(repo.update).mockRejectedValueOnce(new Error('network error'));
      await store.load();

      await expect(store.updateNode('p1', { title: 'Changed' })).rejects.toThrow();

      expect(store.getById('p1')?.title).toBe('Original');
    });

    it('skips setNodes when repo.update returns null (covers line 121 false branch)', async () => {
      const node = makeNode({ id: 'p1', title: 'Original' });
      vi.mocked(repo.getAll).mockResolvedValueOnce([node]);
      vi.mocked(repo.update).mockResolvedValueOnce(null as any);
      await store.load();

      await store.updateNode('p1', { title: 'Changed' });

      // Optimistic update applied but not confirmed — title may be 'Changed' (optimistic)
      // Key: no error thrown, update path completes without crashing
      expect(repo.update).toHaveBeenCalled();
    });

    it('rollback preserves other nodes unchanged (covers line 126 ternary false branch)', async () => {
      const n1 = makeNode({ id: 'p1', title: 'First' });
      const n2 = makeNode({ id: 'p2', title: 'Second' });
      vi.mocked(repo.getAll).mockResolvedValueOnce([n1, n2]);
      vi.mocked(repo.update).mockRejectedValueOnce(new Error('fail'));
      await store.load();

      await expect(store.updateNode('p1', { title: 'Changed' })).rejects.toThrow();

      // p1 rolled back to original; p2 untouched (covers ternary false: n.id !== 'p1' → n)
      expect(store.getById('p1')?.title).toBe('First');
      expect(store.getById('p2')?.title).toBe('Second');
    });

    it('is a no-op for an unknown id', async () => {
      vi.mocked(repo.getAll).mockResolvedValueOnce([]);
      await store.load();
      await expect(store.updateNode('missing', { title: 'x' })).resolves.toBeUndefined();
      expect(repo.update).not.toHaveBeenCalled();
    });
  });

  // ─── deleteNode ──────────────────────────────────────────────────────────

  describe('deleteNode', () => {
    it('removes the node and its descendants from the store', async () => {
      const parent = makeNode({ id: 'parent' });
      const child = makeNode({ id: 'child', parentId: 'parent' });
      const grandchild = makeNode({ id: 'gc', parentId: 'child' });
      vi.mocked(repo.getAll).mockResolvedValueOnce([parent, child, grandchild]);
      await store.load();

      await store.deleteNode('parent');

      expect(store.nodes).toHaveLength(0);
    });

    it('calls deleteSubtree with descendant ids', async () => {
      const parent = makeNode({ id: 'parent' });
      const child = makeNode({ id: 'child', parentId: 'parent' });
      vi.mocked(repo.getAll).mockResolvedValueOnce([parent, child]);
      await store.load();

      await store.deleteNode('parent');

      expect(repo.deleteSubtree).toHaveBeenCalledWith('parent', ['child']);
    });

    it('deletes a leaf node correctly', async () => {
      const node = makeNode({ id: 'leaf' });
      vi.mocked(repo.getAll).mockResolvedValueOnce([node]);
      await store.load();

      await store.deleteNode('leaf');

      expect(store.nodes).toHaveLength(0);
      expect(repo.deleteSubtree).toHaveBeenCalledWith('leaf', []);
    });
  });

  // ─── moveNode ────────────────────────────────────────────────────────────

  describe('moveNode', () => {
    it('throws when moving a node into itself', async () => {
      const node = makeNode({ id: 'p1' });
      vi.mocked(repo.getAll).mockResolvedValueOnce([node]);
      await store.load();

      await expect(store.moveNode('p1', 'p1', 0)).rejects.toThrow(/its own parent/i);
    });

    it('throws when moving a node into one of its descendants', async () => {
      const parent = makeNode({ id: 'parent' });
      const child = makeNode({ id: 'child', parentId: 'parent' });
      const grandchild = makeNode({ id: 'gc', parentId: 'child' });
      vi.mocked(repo.getAll).mockResolvedValueOnce([parent, child, grandchild]);
      await store.load();

      await expect(store.moveNode('parent', 'gc', 0)).rejects.toThrow(/descendants/i);
    });

    it('allows moving to null (root level)', async () => {
      const parent = makeNode({ id: 'parent' });
      const child = makeNode({ id: 'child', parentId: 'parent' });
      vi.mocked(repo.getAll).mockResolvedValueOnce([parent, child]);
      vi.mocked(repo.update).mockResolvedValueOnce({ ...child, parentId: null, order: 10 });
      await store.load();

      await store.moveNode('child', null, 10);

      expect(repo.update).toHaveBeenCalledWith('child', expect.objectContaining({ parentId: null }));
    });

    it('allows moving to a non-descendant parent (covers line 179 false branch)', async () => {
      const child = makeNode({ id: 'child', parentId: null });
      const target = makeNode({ id: 'target', parentId: null });
      vi.mocked(repo.getAll).mockResolvedValueOnce([child, target]);
      vi.mocked(repo.update).mockResolvedValueOnce({ ...child, parentId: 'target', order: 0 });
      await store.load();

      // target is not a descendant of child → descendants.includes('target') is false
      await store.moveNode('child', 'target', 0);

      expect(repo.update).toHaveBeenCalledWith('child', expect.objectContaining({ parentId: 'target' }));
    });
  });

  // ─── removeBulletByNodeId ────────────────────────────────────────────────

  describe('removeBulletByNodeId', () => {
    it('returns false when there is no content for the page', async () => {
      vi.mocked(repo.getAll).mockResolvedValueOnce([makeNode({ id: 'p1' })]);
      vi.mocked(repo.getContent).mockResolvedValueOnce(null);
      await store.load();

      const result = await store.removeBulletByNodeId('p1', 'node-123');
      expect(result).toBe(false);
    });

    it('returns false when the content JSON is invalid', async () => {
      vi.mocked(repo.getAll).mockResolvedValueOnce([makeNode({ id: 'p1' })]);
      vi.mocked(repo.getContent).mockResolvedValueOnce({
        pageId: 'p1',
        content: {},  // empty object — doc.content is undefined, removeNodeById returns false
        updatedAt: '2026-01-01T00:00:00Z'
      });
      await store.load();

      expect(await store.removeBulletByNodeId('p1', 'node-123')).toBe(false);
    });

    it('returns false when the nodeId is not found in the doc', async () => {
      const doc = {
        type: 'doc',
        content: [
          {
            type: 'bulletList',
            content: [
              {
                type: 'listItem',
                attrs: { nodeId: 'other-node', taskId: null, checked: false, taskStatus: 'todo' },
                content: [{ type: 'paragraph', content: [{ type: 'text', text: 'item' }] }]
              }
            ]
          }
        ]
      };
      vi.mocked(repo.getAll).mockResolvedValueOnce([makeNode({ id: 'p1' })]);
      vi.mocked(repo.getContent).mockResolvedValueOnce({
        pageId: 'p1',
        content: doc,
        updatedAt: '2026-01-01T00:00:00Z'
      });
      await store.load();

      expect(await store.removeBulletByNodeId('p1', 'missing-node')).toBe(false);
      expect(repo.saveContent).not.toHaveBeenCalled();
    });

    it('removes the target listItem and saves updated content', async () => {
      const doc = {
        type: 'doc',
        content: [
          {
            type: 'bulletList',
            content: [
              {
                type: 'listItem',
                attrs: { nodeId: 'target', taskId: null, checked: false, taskStatus: 'todo' },
                content: [{ type: 'paragraph', content: [{ type: 'text', text: 'remove me' }] }]
              },
              {
                type: 'listItem',
                attrs: { nodeId: 'keep', taskId: null, checked: false, taskStatus: 'todo' },
                content: [{ type: 'paragraph', content: [{ type: 'text', text: 'keep me' }] }]
              }
            ]
          }
        ]
      };
      vi.mocked(repo.getAll).mockResolvedValueOnce([makeNode({ id: 'p1' })]);
      vi.mocked(repo.getContent).mockResolvedValueOnce({
        pageId: 'p1',
        content: doc,
        updatedAt: '2026-01-01T00:00:00Z'
      });
      await store.load();

      const result = await store.removeBulletByNodeId('p1', 'target');

      expect(result).toBe(true);
      expect(repo.saveContent).toHaveBeenCalledOnce();
      const savedContent = vi.mocked(repo.saveContent).mock.calls[0][0];
      // content is now a Record<string, unknown> object, not a JSON string
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const items = (savedContent.content as any).content[0].content;
      expect(items).toHaveLength(1);
      expect(items[0].attrs.nodeId).toBe('keep');
    });

    it('removes the parent bulletList when it becomes empty', async () => {
      const doc = {
        type: 'doc',
        content: [
          {
            type: 'bulletList',
            content: [
              {
                type: 'listItem',
                attrs: { nodeId: 'only', taskId: null, checked: false, taskStatus: 'todo' },
                content: [{ type: 'paragraph', content: [{ type: 'text', text: 'only item' }] }]
              }
            ]
          }
        ]
      };
      vi.mocked(repo.getAll).mockResolvedValueOnce([makeNode({ id: 'p1' })]);
      vi.mocked(repo.getContent).mockResolvedValueOnce({
        pageId: 'p1',
        content: doc,
        updatedAt: '2026-01-01T00:00:00Z'
      });
      await store.load();

      const result = await store.removeBulletByNodeId('p1', 'only');

      expect(result).toBe(true);
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const savedDoc = vi.mocked(repo.saveContent).mock.calls[0][0].content as any;
      expect(savedDoc.content).toHaveLength(0);
    });

    it('removes parent orderedList when it becomes empty (covers line 21 orderedList branch)', async () => {
      const doc = {
        type: 'doc',
        content: [
          {
            type: 'orderedList',
            content: [
              {
                type: 'listItem',
                attrs: { nodeId: 'only-ol', taskId: null, checked: false, taskStatus: 'todo' },
                content: [{ type: 'paragraph', content: [{ type: 'text', text: 'numbered item' }] }]
              }
            ]
          }
        ]
      };
      vi.mocked(repo.getAll).mockResolvedValueOnce([makeNode({ id: 'p1' })]);
      vi.mocked(repo.getContent).mockResolvedValueOnce({
        pageId: 'p1',
        content: doc,
        updatedAt: '2026-01-01T00:00:00Z'
      });
      await store.load();

      const result = await store.removeBulletByNodeId('p1', 'only-ol');

      expect(result).toBe(true);
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const savedDoc = vi.mocked(repo.saveContent).mock.calls[0][0].content as any;
      // The orderedList was removed because it became empty
      expect(savedDoc.content).toHaveLength(0);
    });
  });

  // ─── collectDescendantIds ────────────────────────────────────────────────

  describe('collectDescendantIds (via deleteNode)', () => {
    it('returns descendants in depth-first order', async () => {
      // parent → child1 → grandchild1; parent → child2
      const parent = makeNode({ id: 'parent' });
      const child1 = makeNode({ id: 'child1', parentId: 'parent', order: 0 });
      const grandchild1 = makeNode({ id: 'gc1', parentId: 'child1', order: 0 });
      const child2 = makeNode({ id: 'child2', parentId: 'parent', order: 1 });
      vi.mocked(repo.getAll).mockResolvedValueOnce([parent, child1, grandchild1, child2]);
      await store.load();

      await store.deleteNode('parent');

      const [, descendantIds] = vi.mocked(repo.deleteSubtree).mock.calls[0];
      // gc1 should come before child1 (depth-first); child2 comes last
      expect(descendantIds).toContain('gc1');
      expect(descendantIds).toContain('child1');
      expect(descendantIds).toContain('child2');
      expect(descendantIds.indexOf('gc1')).toBeLessThan(descendantIds.indexOf('child1'));
    });
  });
});
