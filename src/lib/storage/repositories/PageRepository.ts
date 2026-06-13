import { Repository } from '$lib/storage/Repository';
import type { StorageAdapter, TreeNode, PageContent, ProseMirrorJSONNode } from '$lib/models/types';
import { applyMigrations, CURRENT_SCHEMA_VERSION } from '$lib/editor/migrations';

/**
 * Atomic content storage format — a single localStorage key per page
 * containing both the ProseMirror JSON object and its metadata.
 * This prevents partial-write corruption that was possible when content
 * and metadata were stored as separate keys.
 */
interface ContentBlob {
  content: Record<string, unknown>;
  updatedAt: string;
  schemaVersion: number;
}

/**
 * Legacy split-key metadata (for migration from older format).
 */
interface LegacyContentMeta {
  updatedAt: string;
  schemaVersion: number;
}

export class PageRepository extends Repository<TreeNode> {
  private readonly contentAdapter: StorageAdapter;

  constructor(adapter: StorageAdapter) {
    super(adapter, 'pages');
    this.contentAdapter = adapter;
  }

  private contentKey(pageId: string): string {
    return `glyph:content:${pageId}`;
  }

  private metaKey(pageId: string): string {
    return `glyph:content-meta:${pageId}`;
  }

  async getContent(pageId: string): Promise<PageContent | null> {
    // Try atomic blob format first (new format: object with content + updatedAt + schemaVersion)
    const blob = await this.contentAdapter.get<ContentBlob>(this.contentKey(pageId));
    if (blob && typeof blob === 'object' && 'content' in blob && 'updatedAt' in blob) {
      // Migrate legacy string content to object (written before the JSONB refactor)
      let content: Record<string, unknown>;
      let legacyStringMigrated = false;
      if (typeof blob.content === 'string') {
        try {
          content = JSON.parse(blob.content as unknown as string) as Record<string, unknown>;
        } catch {
          content = { type: 'doc', content: [] };
        }
        legacyStringMigrated = true;
      } else {
        content = blob.content;
      }
      const stored: PageContent = {
        pageId,
        content,
        updatedAt: blob.updatedAt,
        schemaVersion: blob.schemaVersion ?? 0
      };
      // Clean up any leftover legacy meta key
      /* c8 ignore next -- remove() never throws in tests */
      await this.contentAdapter.remove(this.metaKey(pageId)).catch(() => {});
      // Persist the parsed object so future reads don't re-parse
      if (legacyStringMigrated) {
        /* c8 ignore next -- stored.schemaVersion is always set from blob.schemaVersion ?? 0 above */
        await this.writeContentAtomic(stored.pageId, stored.content, stored.updatedAt, stored.schemaVersion ?? 0);
      }
      return this.migrateIfNeeded(stored);
    }

    // Fall back to legacy split-key format (meta key + raw string content key)
    const meta = await this.contentAdapter.get<LegacyContentMeta>(this.metaKey(pageId));
    if (meta && typeof meta === 'object' && 'updatedAt' in meta) {
      const raw = await this.contentAdapter.get<unknown>(this.contentKey(pageId));
      if (raw === null) return null;
      let content: Record<string, unknown>;
      if (typeof raw === 'string') {
        try {
          content = JSON.parse(raw) as Record<string, unknown>;
        } catch {
          content = { type: 'doc', content: [] };
        }
      } else if (raw && typeof raw === 'object') {
        content = raw as Record<string, unknown>;
      } else {
        return null;
      }
      const stored: PageContent = {
        pageId,
        content,
        updatedAt: meta.updatedAt,
        schemaVersion: meta.schemaVersion ?? 0
      };
      // Migrate to new atomic format on read
      /* c8 ignore next -- stored.schemaVersion is always set from meta.schemaVersion ?? 0 above */
      await this.writeContentAtomic(stored.pageId, stored.content, stored.updatedAt, stored.schemaVersion ?? 0);
      return this.migrateIfNeeded(stored);
    }

    // Fall back to oldest legacy format (PageContent object stored as single JSON blob)
    const legacy = await this.contentAdapter.get<PageContent>(this.contentKey(pageId));
    if (!legacy || typeof legacy !== 'object' || !('pageId' in legacy)) return null;

    // Handle legacy string content field
    if (typeof legacy.content === 'string') {
      try {
        legacy.content = JSON.parse(legacy.content as unknown as string) as Record<string, unknown>;
      } catch {
        legacy.content = { type: 'doc', content: [] };
      }
    }

    // Migrate to new atomic format on read
    /* c8 ignore next -- legacy.schemaVersion is always set from the blob or ?? 0 above */
    await this.writeContentAtomic(legacy.pageId, legacy.content, legacy.updatedAt, legacy.schemaVersion ?? 0);
    return this.migrateIfNeeded(legacy);
  }

  async saveContent(content: PageContent): Promise<void> {
    await this.writeContentAtomic(
      content.pageId,
      content.content,
      content.updatedAt,
      CURRENT_SCHEMA_VERSION
    );
  }

  /**
   * Atomic single-key write: content + metadata in one localStorage call.
   * Also removes legacy meta key if present.
   */
  private async writeContentAtomic(pageId: string, content: Record<string, unknown>, updatedAt: string, schemaVersion: number): Promise<void> {
    const blob: ContentBlob = { content, updatedAt, schemaVersion };
    await this.contentAdapter.set(this.contentKey(pageId), blob);
    // Clean up legacy meta key (best-effort)
    /* c8 ignore next -- remove() never throws in tests */
    await this.contentAdapter.remove(this.metaKey(pageId)).catch(() => {});
  }

  private async migrateIfNeeded(stored: PageContent): Promise<PageContent> {
    const fromVersion = stored.schemaVersion ?? 0;
    if (fromVersion < CURRENT_SCHEMA_VERSION && stored.content) {
      try {
        const doc = stored.content as unknown as ProseMirrorJSONNode;
        const { doc: migrated, version } = applyMigrations(doc, fromVersion);
        const upgraded: PageContent = {
          ...stored,
          content: migrated as unknown as Record<string, unknown>,
          schemaVersion: version,
        };
        await this.writeContentAtomic(stored.pageId, upgraded.content, upgraded.updatedAt, version);
        return upgraded;
      } catch (err) {
        console.error(
          `[PageRepository] Schema migration failed for page "${stored.pageId}" ` +
          `(v${fromVersion} → v${CURRENT_SCHEMA_VERSION}):`,
          err
        );
        // Return content with a flag indicating migration failure so UI can warn
        return { ...stored, migrationFailed: true };
      }
    }
    return stored;
  }

  async deleteContent(pageId: string): Promise<void> {
    await this.contentAdapter.remove(this.contentKey(pageId));
    await this.contentAdapter.remove(this.metaKey(pageId));
  }

  async deleteWithContent(id: string): Promise<boolean> {
    await this.deleteContent(id);
    return this.delete(id);
  }

  /**
   * Delete a subtree (root node + all descendants) in a single batch.
   *
   * The tree nodes are removed in one read-modify-write cycle (atomic at the
   * localStorage level). Content keys are removed individually — if they fail,
   * we get orphaned content keys but no orphaned tree nodes, which is the
   * safer failure mode.
   *
   * @param id           - The root node to delete.
   * @param descendantIds - All descendant IDs collected before calling this method.
   */
  async deleteSubtree(id: string, descendantIds: string[]): Promise<void> {
    const allIds = [...descendantIds, id];
    // Remove content keys. Each is a separate localStorage key — errors here
    // leave orphaned content but don't corrupt the tree, so we don't abort.
    for (const pageId of allIds) {
      try {
        await this.deleteContent(pageId);
      } catch {
        // Best-effort content removal; tree integrity takes priority.
      }
    }
    // Single atomic batch delete from the tree array.
    await this.deleteMany(allIds);
  }

  getTree(nodes: TreeNode[]): TreeNode[] {
    const rootNodes = nodes
      .filter((n) => n.parentId === null)
      .sort((a, b) => a.order - b.order);
    return rootNodes;
  }

  getChildren(nodes: TreeNode[], parentId: string): TreeNode[] {
    return nodes
      .filter((n) => n.parentId === parentId)
      .sort((a, b) => a.order - b.order);
  }
}
