import { Repository } from '$lib/storage/Repository';
import type { StorageAdapter, TreeNode, PageContent, ProseMirrorJSONNode } from '$lib/models/types';
import { applyMigrations, CURRENT_SCHEMA_VERSION } from '$lib/editor/migrations';
import { ApiError } from '$lib/storage/apiClient';

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
  /**
   * Incremented on every content save, mirroring the API's revision so two
   * tabs can't silently overwrite each other's edits. Absent on blobs written
   * before revisions existed (treated as 0).
   */
  revision?: number;
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
        schemaVersion: blob.schemaVersion ?? 0,
        revision: blob.revision ?? 0
      };
      // Clean up any leftover legacy meta key
      /* c8 ignore next -- remove() never throws in tests */
      await this.contentAdapter.remove(this.metaKey(pageId)).catch(() => {});
      // Persist the parsed object so future reads don't re-parse
      if (legacyStringMigrated) {
        /* c8 ignore next -- stored.schemaVersion is always set from blob.schemaVersion ?? 0 above */
        await this.writeContentAtomic(stored.pageId, stored.content, stored.updatedAt, stored.schemaVersion ?? 0, stored.revision);
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
        schemaVersion: meta.schemaVersion ?? 0,
        revision: 0
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
    return this.migrateIfNeeded({ ...legacy, revision: 0 });
  }

  /**
   * Save content. With `expectedRevision` set, the save is refused with the
   * same 409 `stale_revision` error the API returns when the stored revision
   * has moved on (another tab saved since this one read), so the editor
   * reloads instead of overwriting that tab's edits. Returns the stored
   * record with its new revision.
   */
  async saveContent(content: PageContent): Promise<PageContent> {
    const current = await this.contentAdapter.get<ContentBlob>(this.contentKey(content.pageId));
    const currentRevision =
      current && typeof current === 'object' && typeof current.revision === 'number' ? current.revision : 0;
    if (content.expectedRevision !== undefined && current && content.expectedRevision !== currentRevision) {
      throw new ApiError(409, 'PUT', `localStorage:${this.contentKey(content.pageId)}`, {
        code: 'stale_revision',
        error: 'content changed since it was read'
      });
    }
    const revision = currentRevision + 1;
    await this.writeContentAtomic(
      content.pageId,
      content.content,
      content.updatedAt,
      CURRENT_SCHEMA_VERSION,
      revision
    );
    return {
      pageId: content.pageId,
      content: content.content,
      updatedAt: content.updatedAt,
      schemaVersion: CURRENT_SCHEMA_VERSION,
      revision
    };
  }

  /**
   * Atomic single-key write: content + metadata in one localStorage call.
   * Also removes legacy meta key if present.
   */
  private async writeContentAtomic(pageId: string, content: Record<string, unknown>, updatedAt: string, schemaVersion: number, revision?: number): Promise<void> {
    const blob: ContentBlob = { content, updatedAt, schemaVersion, ...(revision !== undefined ? { revision } : {}) };
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
        await this.writeContentAtomic(stored.pageId, upgraded.content, upgraded.updatedAt, version, stored.revision);
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
   * The descendants are worked out from storage as it is now, inside the
   * serialized write, not only from `descendantIds`: the caller's list comes
   * from its in-memory tree, which misses children another tab created since,
   * and those would otherwise be left orphaned. Tree nodes are removed in one
   * read-modify-write cycle; content keys are removed individually afterwards
   * (a failure there leaves orphaned content, never orphaned tree nodes).
   *
   * @param id           - The root node to delete.
   * @param descendantIds - Descendants the caller knows about.
   * @returns Every id that was deleted.
   */
  async deleteSubtree(id: string, descendantIds: string[]): Promise<string[]> {
    const deleted = await this.serializeWrite(async () => {
      const items = await this.readFresh();
      const ids = new Set([id, ...descendantIds]);
      const visited = new Set<string>([id]);
      const queue = [id];
      while (queue.length > 0) {
        const current = queue.shift()!;
        for (const n of items) {
          if (n.parentId === current && !visited.has(n.id)) {
            visited.add(n.id);
            ids.add(n.id);
            queue.push(n.id);
          }
        }
      }
      await this.writeAll(items.filter((n) => !ids.has(n.id)));
      return [...ids];
    });
    for (const pageId of deleted) {
      try {
        await this.deleteContent(pageId);
      } catch {
        // Best-effort content removal; tree integrity takes priority.
      }
    }
    return deleted;
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
