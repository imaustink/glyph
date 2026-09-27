import { repositories } from '$lib/storage/config';
import type { ITemplateRepository } from '$lib/storage/interfaces';
import type { NoteTemplate } from '$lib/models/types';
import { DEFAULT_TODO_TRIGGER } from '$lib/models/types';
import { now, makeTimestamps } from '$lib/utils/time';
import { uuid } from '$lib/utils/uuid';
import { withCrossTabLock } from '$lib/utils/crossTabLock';

const DEFAULT_TEMPLATE_CONTENT = JSON.stringify({
  type: 'doc',
  content: [
    {
      type: 'heading',
      attrs: { level: 1 },
      content: [{ type: 'text', text: 'TODO' }]
    }
  ]
});

export function createTemplatesStore(injectedRepo?: ITemplateRepository) {
  const repo = injectedRepo ?? repositories.templates;
  let templates = $state<NoteTemplate[]>([]);
  let loaded = $state(false);
  let _loadPromise: Promise<void> | null = null;

  async function load() {
    if (loaded || _loadPromise) return _loadPromise ?? undefined;
    _loadPromise = (async () => {
      try {
        templates = await repo.getAll();
        loaded = true;
      } finally {
        _loadPromise = null;
      }
    })();
    return _loadPromise;
  }

  /** Idempotent initialization — seeds default template if none exist. Call after load(). */
  async function seedDefaults() {
    if (templates.length > 0) return;
    // Cross-tab lock + re-read, as for lanes: adopt another tab's seed.
    await withCrossTabLock('glyph:seed:templates', async () => {
      const current = await repo.getAll();
      if (current.length > 0) {
        templates = current;
        return;
      }
      const defaultTemplate: NoteTemplate = {
        id: uuid(),
        name: 'Default',
        content: DEFAULT_TEMPLATE_CONTENT,
        titleTemplate: '',
        todoTrigger: DEFAULT_TODO_TRIGGER,
        isDefault: true,
        ...makeTimestamps()
      };
      if (repo.seedIfEmpty) {
        templates = await repo.seedIfEmpty([defaultTemplate]);
      } else {
        const created = await repo.create(defaultTemplate);
        templates = [created];
      }
    });
  }

  /**
   * Clear `defaultFolderId` on every template that points at one of
   * `folderIds` (folders that were just deleted). Otherwise pages created
   * from those templates would be filed under a folder that no longer
   * exists and never show up in the tree.
   *
   * `persist: false` updates only local state, for backends that already
   * clear the reference themselves (the API's FK is ON DELETE SET NULL).
   */
  async function clearDefaultFolder(folderIds: Iterable<string>, { persist = true } = {}): Promise<void> {
    const gone = new Set(folderIds);
    const affected = templates.filter((t) => t.defaultFolderId && gone.has(t.defaultFolderId));
    if (affected.length === 0) return;
    templates = templates.map((t) => (affected.includes(t) ? { ...t, defaultFolderId: null } : t));
    if (!persist) return;
    const results = await Promise.allSettled(
      affected.map((t) => repo.update(t.id, { defaultFolderId: null, updatedAt: now() }))
    );
    const failure = results.find((r): r is PromiseRejectedResult => r.status === 'rejected');
    if (failure) throw failure.reason;
  }

  function getDefault(): NoteTemplate | null {
    return templates.find((t) => t.isDefault) ?? templates[0] ?? null;
  }

  async function createTemplate(name: string, content: string, titleTemplate = '', todoTrigger = DEFAULT_TODO_TRIGGER, defaultFolderId: string | null = null): Promise<NoteTemplate> {
    const template: NoteTemplate = {
      id: uuid(),
      name,
      content,
      titleTemplate,
      todoTrigger,
      defaultFolderId,
      isDefault: false,
      ...makeTimestamps()
    };
    const created = await repo.create(template);
    templates = [...templates, created];
    return created;
  }

  async function updateTemplate(
    id: string,
    patch: Partial<Pick<NoteTemplate, 'name' | 'content' | 'isDefault' | 'titleTemplate' | 'todoTrigger' | 'defaultFolderId' | 'orgId' | 'isPrivate'>>
  ): Promise<void> {
    const updated = await repo.update(id, { ...patch, updatedAt: now() });
    if (updated) {
      templates = templates.map((t) => (t.id === id ? updated : t));
    }
  }

  /**
   * Make `id` the only default template. Local storage does it in one batch
   * write. The API has no batch endpoint, so there it is one write per
   * changed template (the new default first); if any fails, the templates
   * are reloaded so the store shows what was actually saved.
   */
  async function setDefault(id: string): Promise<void> {
    const timestamp = now();
    const changed = templates.filter((t) => t.isDefault !== (t.id === id));
    if (changed.length === 0) return;
    if (repo.updateMany) {
      await repo.updateMany(new Map(changed.map((t) => [t.id, { isDefault: t.id === id, updatedAt: timestamp }])));
    } else {
      const ordered = [...changed].sort((a, b) => Number(b.id === id) - Number(a.id === id));
      try {
        for (const t of ordered) {
          await repo.update(t.id, { isDefault: t.id === id, updatedAt: timestamp });
        }
      } catch (err) {
        templates = await repo.getAll().catch(() => templates);
        throw err;
      }
    }
    templates = templates.map((t) => ({ ...t, isDefault: t.id === id }));
  }

  async function deleteTemplate(id: string): Promise<void> {
    await repo.delete(id);
    const remaining = templates.filter((t) => t.id !== id);
    templates = remaining;
    // If the deleted template was default, promote the first remaining template
    if (!remaining.some((t) => t.isDefault) && remaining.length > 0) {
      await setDefault(remaining[0].id);
    }
  }

  return {
    get templates() { return templates; },
    get loaded() { return loaded; },
    get defaultTemplate() { return getDefault(); },
    load,
    seedDefaults,
    createTemplate,
    updateTemplate,
    setDefault,
    clearDefaultFolder,
    deleteTemplate
  };
}

export const templatesStore = createTemplatesStore();
