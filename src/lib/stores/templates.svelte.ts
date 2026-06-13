import { repositories } from '$lib/storage/config';
import type { ITemplateRepository } from '$lib/storage/interfaces';
import type { NoteTemplate } from '$lib/models/types';
import { DEFAULT_TODO_TRIGGER } from '$lib/models/types';
import { now, makeTimestamps } from '$lib/utils/time';
import { uuid } from '$lib/utils/uuid';

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
    const defaultTemplate: NoteTemplate = {
      id: uuid(),
      name: 'Default',
      content: DEFAULT_TEMPLATE_CONTENT,
      titleTemplate: '',
      todoTrigger: DEFAULT_TODO_TRIGGER,
      isDefault: true,
      ...makeTimestamps()
    };
    const created = await repo.create(defaultTemplate);
    templates = [created];
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

  async function setDefault(id: string): Promise<void> {
    await Promise.all(
      templates.map((t) =>
        t.isDefault !== (t.id === id)
          ? repo.update(t.id, { isDefault: t.id === id, updatedAt: now() })
          : Promise.resolve(null)
      )
    );
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
    deleteTemplate
  };
}

export const templatesStore = createTemplatesStore();
