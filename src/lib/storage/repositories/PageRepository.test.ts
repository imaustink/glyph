import { describe, it, expect, beforeEach } from 'vitest';
import { PageRepository } from './PageRepository';
import type { StorageAdapter, TreeNode, PageContent } from '$lib/models/types';

function createMockAdapter(): StorageAdapter {
  const store = new Map<string, unknown>();
  return {
    async get<T>(key: string): Promise<T | null> {
      return (store.get(key) as T) ?? null;
    },
    async set<T>(key: string, value: T): Promise<void> {
      store.set(key, value);
    },
    async remove(key: string): Promise<void> {
      store.delete(key);
    },
    async keys(): Promise<string[]> {
      return [...store.keys()];
    },
    async clear(): Promise<void> {
      store.clear();
    }
  };
}

function makeNode(overrides: Partial<TreeNode> = {}): TreeNode {
  return {
    id: 'node-1',
    type: 'page',
    title: 'Test Page',
    parentId: null,
    order: 0,
    tags: [],
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
    ...overrides
  };
}

describe('PageRepository', () => {
  let repo: PageRepository;

  beforeEach(() => {
    repo = new PageRepository(createMockAdapter());
  });

  describe('content management', () => {
    it('saves and retrieves page content', async () => {
      const content: PageContent = {
        pageId: 'p1',
        content: { type: 'doc' },
        updatedAt: '2026-01-01T00:00:00Z'
      };
      await repo.saveContent(content);
      const result = await repo.getContent('p1');
      expect(result).toEqual({
        ...content,
        schemaVersion: expect.any(Number)
      });
    });

    it('returns null for non-existent content', async () => {
      expect(await repo.getContent('missing')).toBeNull();
    });

    it('reads atomic blob without schemaVersion field (covers ?? 0 at line 49)', async () => {
      // Store a blob that has 'content' and 'updatedAt' but NO 'schemaVersion'
      // so the ?? 0 fallback is triggered
      const contentKey = 'glyph:content:p-no-ver';
      const blobWithoutSchemaVersion = {
        content: { type: 'doc', content: [] },
        updatedAt: '2026-01-01T00:00:00Z'
        // schemaVersion intentionally absent
      };
      const store = new Map<string, unknown>([[contentKey, blobWithoutSchemaVersion]]);
      const adapter: import('$lib/models/types').StorageAdapter = {
        async get<T>(key: string) { return (store.get(key) as T) ?? null; },
        async set<T>(key: string, value: T) { store.set(key, value); },
        async remove(key: string) { store.delete(key); },
        async keys() { return [...store.keys()]; },
        async clear() { store.clear(); }
      };
      const freshRepo = new PageRepository(adapter);

      const result = await freshRepo.getContent('p-no-ver');

      expect(result).not.toBeNull();
      expect(result?.pageId).toBe('p-no-ver');
      // schemaVersion ?? 0 → treats as version 0, migration runs
      expect(typeof result?.schemaVersion).toBe('number');
    });

    it('returns null when legacy meta exists but content key is missing (covers line 60 return null)', async () => {
      // Legacy format: meta key exists with updatedAt, but the content key is absent (raw === null)
      const metaKey = 'glyph:content-meta:p-null-content';
      const store = new Map<string, unknown>([
        [metaKey, { updatedAt: '2026-01-01T00:00:00Z', schemaVersion: 1 }]
        // contentKey intentionally absent
      ]);
      const adapter: import('$lib/models/types').StorageAdapter = {
        async get<T>(key: string) { return (store.get(key) as T) ?? null; },
        async set<T>(key: string, value: T) { store.set(key, value); },
        async remove(key: string) { store.delete(key); },
        async keys() { return [...store.keys()]; },
        async clear() { store.clear(); }
      };
      const freshRepo = new PageRepository(adapter);

      const result = await freshRepo.getContent('p-null-content');

      // meta exists but raw content is null → returns null
      expect(result).toBeNull();
    });

    it('deletes page content', async () => {
      const content: PageContent = {
        pageId: 'p1',
        content: { type: 'doc' },
        updatedAt: '2026-01-01T00:00:00Z'
      };
      await repo.saveContent(content);
      await repo.deleteContent('p1');
      expect(await repo.getContent('p1')).toBeNull();
    });

    it('deleteWithContent removes both node and content', async () => {
      await repo.create(makeNode({ id: 'p1' }));
      await repo.saveContent({
        pageId: 'p1',
        content: { type: 'doc' },
        updatedAt: '2026-01-01T00:00:00Z'
      });
      const deleted = await repo.deleteWithContent('p1');
      expect(deleted).toBe(true);
      expect(await repo.getById('p1')).toBeNull();
      expect(await repo.getContent('p1')).toBeNull();
    });
  });

  describe('getTree', () => {
    it('returns only root nodes sorted by order', () => {
      const nodes = [
        makeNode({ id: 'c', parentId: null, order: 2 }),
        makeNode({ id: 'child', parentId: 'a', order: 0 }),
        makeNode({ id: 'a', parentId: null, order: 0 }),
        makeNode({ id: 'b', parentId: null, order: 1 })
      ];
      const tree = repo.getTree(nodes);
      expect(tree.map((n) => n.id)).toEqual(['a', 'b', 'c']);
    });
  });

  describe('getChildren', () => {
    it('returns children of a parent sorted by order', () => {
      const nodes = [
        makeNode({ id: 'root', parentId: null, order: 0 }),
        makeNode({ id: 'child2', parentId: 'root', order: 1 }),
        makeNode({ id: 'child1', parentId: 'root', order: 0 }),
        makeNode({ id: 'other', parentId: 'other-parent', order: 0 })
      ];
      const children = repo.getChildren(nodes, 'root');
      expect(children.map((n) => n.id)).toEqual(['child1', 'child2']);
    });

    it('returns empty array when no children', () => {
      const nodes = [makeNode({ id: 'root', parentId: null })];
      expect(repo.getChildren(nodes, 'root')).toEqual([]);
    });
  });

  describe('legacy content format migration', () => {
    it('reads split-key legacy format (meta + raw content) and migrates to atomic blob', async () => {
      // Simulate the old format: separate meta key + raw string content key
      const metaKey = 'glyph:content-meta:p1';
      const contentKey = 'glyph:content:p1';

      // Write legacy data directly into the adapter's underlying store
      // by using the adapter methods directly
      const legacyMeta = { updatedAt: '2026-01-01T00:00:00Z', schemaVersion: 1 };
      const legacyContent = '{"type":"doc","content":[]}';

      // We need to inject these as the old format — use a custom adapter that
      // pre-populates with split-key data
      const store = new Map<string, unknown>([
        [metaKey, legacyMeta],
        [contentKey, legacyContent]
      ]);
      const adapter: import('$lib/models/types').StorageAdapter = {
        async get<T>(key: string) { return (store.get(key) as T) ?? null; },
        async set<T>(key: string, value: T) { store.set(key, value); },
        async remove(key: string) { store.delete(key); },
        async keys() { return [...store.keys()]; },
        async clear() { store.clear(); }
      };
      const legacyRepo = new PageRepository(adapter);

      const result = await legacyRepo.getContent('p1');

      expect(result).not.toBeNull();
      // Legacy string content is parsed to an object on migration read
      expect(result?.content).toEqual({ type: 'doc', content: [] });
      expect(result?.updatedAt).toBe('2026-01-01T00:00:00Z');
      // After migration read, the atomic blob should now be stored with object content
      const migrated = store.get(contentKey) as Record<string, unknown>;
      expect(migrated).toBeDefined();
      expect(typeof migrated?.content).toBe('object');
    });

    it('reads split-key legacy format without schemaVersion in meta (covers meta.schemaVersion ?? 0 at line 65)', async () => {
      // Like the previous test but meta has NO schemaVersion field
      const metaKey = 'glyph:content-meta:p-no-meta-ver';
      const contentKey = 'glyph:content:p-no-meta-ver';
      const store = new Map<string, unknown>([
        [metaKey, { updatedAt: '2026-01-01T00:00:00Z' }],  // no schemaVersion
        [contentKey, '{"type":"doc","content":[]}']
      ]);
      const adapter: import('$lib/models/types').StorageAdapter = {
        async get<T>(key: string) { return (store.get(key) as T) ?? null; },
        async set<T>(key: string, value: T) { store.set(key, value); },
        async remove(key: string) { store.delete(key); },
        async keys() { return [...store.keys()]; },
        async clear() { store.clear(); }
      };
      const freshRepo = new PageRepository(adapter);

      const result = await freshRepo.getContent('p-no-meta-ver');

      expect(result).not.toBeNull();
      // schemaVersion defaults to 0 via ?? 0
      expect(typeof result?.schemaVersion).toBe('number');
    });

    it('reads oldest legacy format (PageContent object as blob) and migrates', async () => {
      const contentKey = 'glyph:content:p2';
      const legacyBlob = {
        pageId: 'p2',
        content: '{"type":"doc","content":[]}',
        updatedAt: '2026-02-01T00:00:00Z',
        schemaVersion: 1
      };
      const store = new Map<string, unknown>([[contentKey, legacyBlob]]);
      const adapter: import('$lib/models/types').StorageAdapter = {
        async get<T>(key: string) { return (store.get(key) as T) ?? null; },
        async set<T>(key: string, value: T) { store.set(key, value); },
        async remove(key: string) { store.delete(key); },
        async keys() { return [...store.keys()]; },
        async clear() { store.clear(); }
      };
      const legacyRepo = new PageRepository(adapter);

      const result = await legacyRepo.getContent('p2');

      expect(result?.pageId).toBe('p2');
      // Legacy string content is parsed to an object on migration read
      expect(result?.content).toEqual({ type: 'doc', content: [] });
      // The atomic blob should be written after read with object content
      const migrated = store.get(contentKey) as Record<string, unknown>;
      expect(typeof migrated?.content).toBe('object');
    });

    it('reads PageContent missing updatedAt (oldest legacy path, lines 77-78) and migrates', async () => {
      // Store an object with 'pageId' and 'content' but WITHOUT 'updatedAt'.
      // This means the first check ('content' in blob && 'updatedAt' in blob) FAILS,
      // the meta-key check also finds nothing, then the third fallback path (lines 77-78) runs.
      // schemaVersion is missing (undefined → treated as 0), so migrateIfNeeded also runs (line 113).
      const contentKey = 'glyph:content:p6';
      const oldFormatBlob = {
        pageId: 'p6',
        content: '{"type":"doc","content":[]}'
        // no 'updatedAt', no 'schemaVersion'
      };
      const store = new Map<string, unknown>([[contentKey, oldFormatBlob]]);
      const adapter: import('$lib/models/types').StorageAdapter = {
        async get<T>(key: string) { return (store.get(key) as T) ?? null; },
        async set<T>(key: string, value: T) { store.set(key, value); },
        async remove(key: string) { store.delete(key); },
        async keys() { return [...store.keys()]; },
        async clear() { store.clear(); }
      };
      const freshRepo = new PageRepository(adapter);

      const result = await freshRepo.getContent('p6');

      expect(result?.pageId).toBe('p6');
      // Migration should have run (schemaVersion 0 → CURRENT)
      expect(result?.schemaVersion).toBeGreaterThanOrEqual(0);
    });

    it('saveContent stamps CURRENT_SCHEMA_VERSION onto the blob', async () => {
      const { CURRENT_SCHEMA_VERSION } = await import('$lib/editor/migrations');
      const store = new Map<string, unknown>();
      const adapter: import('$lib/models/types').StorageAdapter = {
        async get<T>(key: string) { return (store.get(key) as T) ?? null; },
        async set<T>(key: string, value: T) { store.set(key, value); },
        async remove(key: string) { store.delete(key); },
        async keys() { return [...store.keys()]; },
        async clear() { store.clear(); }
      };
      const freshRepo = new PageRepository(adapter);

      await freshRepo.saveContent({
        pageId: 'p3',
        content: { type: 'doc' },
        updatedAt: '2026-05-01T00:00:00Z'
      });

      const blob = store.get('glyph:content:p3') as Record<string, unknown>;
      expect(blob?.schemaVersion).toBe(CURRENT_SCHEMA_VERSION);
    });

    it('returns empty doc when legacy content string is invalid JSON', async () => {
      // A blob with an unparseable string content (pre-JSONB era) is gracefully
      // normalised to an empty ProseMirror doc rather than propagating an error.
      const contentKey = 'glyph:content:p4';
      const badBlob = { content: 'NOT_VALID_JSON', updatedAt: '2026-01-01T00:00:00Z', schemaVersion: 1 };
      const store = new Map<string, unknown>([[contentKey, badBlob]]);
      const adapter: import('$lib/models/types').StorageAdapter = {
        async get<T>(key: string) { return (store.get(key) as T) ?? null; },
        async set<T>(key: string, value: T) { store.set(key, value); },
        async remove(key: string) { store.delete(key); },
        async keys() { return [...store.keys()]; },
        async clear() { store.clear(); }
      };
      const freshRepo = new PageRepository(adapter);

      const result = await freshRepo.getContent('p4');

      // Graceful degradation: invalid legacy string → empty doc, no migrationFailed flag
      expect(result).not.toBeNull();
      expect(result?.content).toEqual({ type: 'doc', content: [] });
      expect(result?.migrationFailed).toBeUndefined();
    });
  });

  describe('deleteSubtree', () => {
    it('deletes the root node and all descendants in one batch', async () => {
      await repo.create(makeNode({ id: 'root' }));
      await repo.create(makeNode({ id: 'child', parentId: 'root' }));
      await repo.create(makeNode({ id: 'other' }));

      await repo.deleteSubtree('root', ['child']);

      expect(await repo.getById('root')).toBeNull();
      expect(await repo.getById('child')).toBeNull();
      expect(await repo.getById('other')).not.toBeNull();
    });

    it('also removes content keys for all deleted nodes', async () => {
      const store = new Map<string, unknown>();
      const adapter: import('$lib/models/types').StorageAdapter = {
        async get<T>(key: string) { return (store.get(key) as T) ?? null; },
        async set<T>(key: string, value: T) { store.set(key, value); },
        async remove(key: string) { store.delete(key); },
        async keys() { return [...store.keys()]; },
        async clear() { store.clear(); }
      };
      const freshRepo = new PageRepository(adapter);
      await freshRepo.create(makeNode({ id: 'root' }));
      await freshRepo.saveContent({ pageId: 'root', content: { type: 'doc' }, updatedAt: '2026-01-01T00:00:00Z' });
      await freshRepo.create(makeNode({ id: 'child', parentId: 'root' }));
      await freshRepo.saveContent({ pageId: 'child', content: { type: 'doc' }, updatedAt: '2026-01-01T00:00:00Z' });

      await freshRepo.deleteSubtree('root', ['child']);

      expect(await freshRepo.getContent('root')).toBeNull();
      expect(await freshRepo.getContent('child')).toBeNull();
    });
  });

  // ─── Legacy format migration paths ────────────────────────────────────────

  describe('legacy format migration', () => {
    function makeAdapter(initial: Map<string, unknown>) {
      const store = new Map<string, unknown>(initial);
      return {
        adapter: {
          async get<T>(key: string) { return (store.get(key) as T) ?? null; },
          async set<T>(key: string, value: T) { store.set(key, value); },
          async remove(key: string) { store.delete(key); },
          async keys() { return [...store.keys()]; },
          async clear() { store.clear(); }
        } as import('$lib/models/types').StorageAdapter,
        store
      };
    }

    it('reads legacy split-key format with string content (parses JSON)', async () => {      const { adapter } = makeAdapter(new Map<string, unknown>([
        ['glyph:content-meta:p-legacy', { updatedAt: '2026-01-01T00:00:00Z', schemaVersion: 1 }],
        ['glyph:content:p-legacy', '{"type":"doc","content":[]}']
      ]));
      const freshRepo = new PageRepository(adapter);

      const result = await freshRepo.getContent('p-legacy');

      expect(result).not.toBeNull();
      expect(result?.content).toEqual({ type: 'doc', content: [] });
    });

    it('reads legacy split-key format with string content that is invalid JSON', async () => {
      const { adapter } = makeAdapter(new Map<string, unknown>([
        ['glyph:content-meta:p-badjson', { updatedAt: '2026-01-01T00:00:00Z', schemaVersion: 1 }],
        ['glyph:content:p-badjson', 'not valid json {{{{']
      ]));
      const freshRepo = new PageRepository(adapter);

      const result = await freshRepo.getContent('p-badjson');

      expect(result).not.toBeNull();
      expect(result?.content).toEqual({ type: 'doc', content: [] });
    });

    it('reads legacy split-key format with raw object content', async () => {
      const { adapter } = makeAdapter(new Map([
        ['glyph:content-meta:p-obj', { updatedAt: '2026-01-01T00:00:00Z', schemaVersion: 1 }],
        ['glyph:content:p-obj', { type: 'doc', content: [{ type: 'paragraph' }] }]
      ]));
      const freshRepo = new PageRepository(adapter);

      const result = await freshRepo.getContent('p-obj');

      expect(result?.content).toEqual({ type: 'doc', content: [{ type: 'paragraph' }] });
    });

    it('returns null from legacy split-key when content is neither string nor object', async () => {
      const { adapter } = makeAdapter(new Map<string, unknown>([
        ['glyph:content-meta:p-bad', { updatedAt: '2026-01-01T00:00:00Z', schemaVersion: 1 }],
        ['glyph:content:p-bad', 42]  // number — not string or object
      ]));
      const freshRepo = new PageRepository(adapter);

      const result = await freshRepo.getContent('p-bad');

      expect(result).toBeNull();
    });

    it('reads oldest legacy format (PageContent blob) with string content field', async () => {
      // The very oldest format: PageContent stored WITHOUT updatedAt — so it doesn't
      // match the atomic blob check ('content' in blob && 'updatedAt' in blob).
      // The third fallback path finds it via the content key and 'pageId' in legacy.
      const { adapter } = makeAdapter(new Map([
        ['glyph:content:p-oldest', {
          pageId: 'p-oldest',
          content: '{"type":"doc","content":[]}',  // string content, no updatedAt
          schemaVersion: 1
        }]
      ]));
      const freshRepo = new PageRepository(adapter);

      const result = await freshRepo.getContent('p-oldest');

      expect(result).not.toBeNull();
      expect(result?.content).toEqual({ type: 'doc', content: [] });
    });

    it('reads oldest legacy format with invalid string content field → empty doc', async () => {
      const { adapter } = makeAdapter(new Map([
        ['glyph:content:p-oldest-bad', {
          pageId: 'p-oldest-bad',
          content: 'not-json{{{{',  // no updatedAt, so hits third path
          schemaVersion: 1
        }]
      ]));
      const freshRepo = new PageRepository(adapter);

      const result = await freshRepo.getContent('p-oldest-bad');

      expect(result?.content).toEqual({ type: 'doc', content: [] });
    });

    it('reads oldest legacy format with object content (false branch of content string check)', async () => {
      // Third path, but content is already an object — false branch of line 108
      const { adapter } = makeAdapter(new Map([
        ['glyph:content:p-oldest-obj', {
          pageId: 'p-oldest-obj',
          content: { type: 'doc', content: [{ type: 'paragraph' }] },  // already an object, no updatedAt
          schemaVersion: 1
        }]
      ]));
      const freshRepo = new PageRepository(adapter);

      const result = await freshRepo.getContent('p-oldest-obj');

      expect(result?.content).toEqual({ type: 'doc', content: [{ type: 'paragraph' }] });
    });

    it('reads atomic blob with string content and writes back converted object (line 69 true branch)', async () => {
      // Atomic blob (has both content + updatedAt) where content is a JSON STRING
      // This triggers legacyStringMigrated = true → writeContentAtomic call (line 69)
      const { adapter, store } = makeAdapter(new Map([
        ['glyph:content:p-blob-str', {
          content: '{"type":"doc","content":[]}',  // string, not object
          updatedAt: '2026-01-01T00:00:00Z',
          schemaVersion: 1
        }]
      ]));
      const freshRepo = new PageRepository(adapter);

      const result = await freshRepo.getContent('p-blob-str');

      expect(result?.content).toEqual({ type: 'doc', content: [] });
      // The string should have been written back as an object (migration write-back)
      const rewritten = store.get('glyph:content:p-blob-str') as { content: unknown };
      expect(typeof rewritten?.content).toBe('object');
    });
  });

  describe('schema migration failure', () => {
    it('returns migrationFailed flag when applyMigrations throws', async () => {
      // Store content at schemaVersion 0 (below CURRENT=1) so migration is attempted.
      const store = new Map<string, unknown>([
        ['glyph:content:p-migrate-fail', {
          content: { type: 'doc', content: [] },
          updatedAt: '2026-01-01T00:00:00Z',
          schemaVersion: 0  // triggers migrateIfNeeded
        }]
      ]);
      const adapter: import('$lib/models/types').StorageAdapter = {
        async get<T>(key: string) { return (store.get(key) as T) ?? null; },
        async set<T>(key: string, value: T) { store.set(key, value); },
        async remove(key: string) { store.delete(key); },
        async keys() { return [...store.keys()]; },
        async clear() { store.clear(); }
      };

      // Inject a throwing migration so the catch block is exercised.
      const { migrations } = await import('$lib/editor/migrations');
      const bad = { fromVersion: 0, migrate: (): never => { throw new Error('Simulated migration failure'); } };
      migrations.push(bad);

      const freshRepo = new PageRepository(adapter);
      const result = await freshRepo.getContent('p-migrate-fail');

      // Clean up the injected migration
      migrations.splice(migrations.indexOf(bad), 1);

      expect(result).not.toBeNull();
      expect((result as unknown as Record<string, unknown>).migrationFailed).toBe(true);
    });
  });
});
