/**
 * Unit tests for the templates store.
 *
 * Injects a mock ITemplateRepository so no real storage is involved.
 * Tests: load, seedDefaults (idempotent), getDefault, createTemplate,
 *        updateTemplate, setDefault, deleteTemplate (including default promotion).
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { createTemplatesStore } from './templates.svelte';
import type { ITemplateRepository } from '$lib/storage/interfaces';
import type { NoteTemplate } from '$lib/models/types';

function makeTemplate(overrides: Partial<NoteTemplate> = {}): NoteTemplate {
  return {
    id: 'tpl-1',
    name: 'Default',
    content: '{}',
    titleTemplate: '',
    todoTrigger: { pattern: 'TODO', matchMode: 'exact', blockTypes: ['heading'] },
    isDefault: false,
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
    ...overrides
  };
}

function createMockRepo(overrides: Partial<ITemplateRepository> = {}): ITemplateRepository {
  return {
    getAll: vi.fn().mockResolvedValue([]),
    getById: vi.fn().mockResolvedValue(null),
    create: vi.fn().mockImplementation(async (t: NoteTemplate) => t),
    update: vi.fn().mockImplementation(async (_id: string, patch: Partial<NoteTemplate>) => patch),
    delete: vi.fn().mockResolvedValue(true),
    upsert: vi.fn().mockImplementation(async (t: NoteTemplate) => t),
    ...overrides
  };
}

describe('templatesStore', () => {
  let repo: ReturnType<typeof createMockRepo>;
  let store: ReturnType<typeof createTemplatesStore>;

  beforeEach(() => {
    repo = createMockRepo();
    store = createTemplatesStore(repo);
    vi.clearAllMocks();
  });

  // ─── load ────────────────────────────────────────────────────────────────

  describe('load', () => {
    it('populates templates from the repo', async () => {
      const tpl = makeTemplate({ id: 'tpl-1', isDefault: true });
      vi.mocked(repo.getAll).mockResolvedValueOnce([tpl]);
      await store.load();
      expect(store.templates).toEqual([tpl]);
    });

    it('sets templates to empty array when repo returns nothing', async () => {
      vi.mocked(repo.getAll).mockResolvedValueOnce([]);
      await store.load();
      expect(store.templates).toEqual([]);
    });

    it('deduplicates concurrent load calls', async () => {
      vi.mocked(repo.getAll).mockResolvedValue([]);
      await Promise.all([store.load(), store.load(), store.load()]);
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
  });

  // ─── seedDefaults ────────────────────────────────────────────────────────

  describe('seedDefaults', () => {
    it('creates one default template when none exist', async () => {
      await store.load(); // templates is []
      await store.seedDefaults();
      expect(repo.create).toHaveBeenCalledOnce();
      expect(store.templates).toHaveLength(1);
      expect(store.templates[0].isDefault).toBe(true);
    });

    it('is idempotent: does not seed when templates already exist', async () => {
      vi.mocked(repo.getAll).mockResolvedValueOnce([makeTemplate({ isDefault: true })]);
      await store.load();
      await store.seedDefaults();
      expect(repo.create).not.toHaveBeenCalled();
    });
  });

  // ─── getDefault ──────────────────────────────────────────────────────────

  describe('defaultTemplate getter', () => {
    it('returns the template marked isDefault', async () => {
      const t1 = makeTemplate({ id: 't1', isDefault: false });
      const t2 = makeTemplate({ id: 't2', isDefault: true });
      vi.mocked(repo.getAll).mockResolvedValueOnce([t1, t2]);
      await store.load();
      expect(store.defaultTemplate?.id).toBe('t2');
    });

    it('falls back to templates[0] when none are marked isDefault', async () => {
      const t1 = makeTemplate({ id: 't1', isDefault: false });
      const t2 = makeTemplate({ id: 't2', isDefault: false });
      vi.mocked(repo.getAll).mockResolvedValueOnce([t1, t2]);
      await store.load();
      expect(store.defaultTemplate?.id).toBe('t1');
    });

    it('returns null when templates is empty', async () => {
      await store.load(); // empty
      expect(store.defaultTemplate).toBeNull();
    });
  });

  // ─── createTemplate ──────────────────────────────────────────────────────

  describe('createTemplate', () => {
    it('calls repo.create and appends the template', async () => {
      await store.load();
      const created = makeTemplate({ id: 'new-tpl', name: 'Mine', isDefault: false });
      vi.mocked(repo.create).mockResolvedValueOnce(created);

      const result = await store.createTemplate('Mine', '{}');

      expect(result.name).toBe('Mine');
      expect(store.templates).toHaveLength(1);
      expect(store.templates[0].id).toBe('new-tpl');
    });

    it('new templates are not default', async () => {
      await store.load();
      const created = makeTemplate({ id: 'x', isDefault: false });
      vi.mocked(repo.create).mockResolvedValueOnce(created);
      await store.createTemplate('X', '{}');
      expect(store.templates[0].isDefault).toBe(false);
    });
  });

  // ─── updateTemplate ──────────────────────────────────────────────────────

  describe('updateTemplate', () => {
    it('calls repo.update and replaces the template in the store', async () => {
      const tpl = makeTemplate({ id: 'tpl-1', name: 'Old Name' });
      vi.mocked(repo.getAll).mockResolvedValueOnce([tpl]);
      await store.load();

      const updated = makeTemplate({ id: 'tpl-1', name: 'New Name' });
      vi.mocked(repo.update).mockResolvedValueOnce(updated);

      await store.updateTemplate('tpl-1', { name: 'New Name' });

      expect(repo.update).toHaveBeenCalledOnce();
      expect(store.templates[0].name).toBe('New Name');
    });

    it('does not replace when repo.update returns null', async () => {
      const tpl = makeTemplate({ id: 'tpl-1', name: 'Keep This' });
      vi.mocked(repo.getAll).mockResolvedValueOnce([tpl]);
      await store.load();

      vi.mocked(repo.update).mockResolvedValueOnce(null);
      await store.updateTemplate('tpl-1', { name: 'New Name' });

      // Store should be unchanged since update returned null
      expect(store.templates[0].name).toBe('Keep This');
    });

    it('only replaces the matching template when multiple exist', async () => {
      const t1 = makeTemplate({ id: 't1', name: 'Alpha' });
      const t2 = makeTemplate({ id: 't2', name: 'Beta' });
      vi.mocked(repo.getAll).mockResolvedValueOnce([t1, t2]);
      await store.load();

      const updated = makeTemplate({ id: 't1', name: 'Alpha Updated' });
      vi.mocked(repo.update).mockResolvedValueOnce(updated);
      await store.updateTemplate('t1', { name: 'Alpha Updated' });

      expect(store.templates.find((t) => t.id === 't1')?.name).toBe('Alpha Updated');
      expect(store.templates.find((t) => t.id === 't2')?.name).toBe('Beta');
    });
  });

  // ─── setDefault ──────────────────────────────────────────────────────────

  describe('setDefault', () => {
    it('marks exactly one template as default and clears others', async () => {
      const t1 = makeTemplate({ id: 't1', isDefault: true });
      const t2 = makeTemplate({ id: 't2', isDefault: false });
      vi.mocked(repo.getAll).mockResolvedValueOnce([t1, t2]);
      // update mock: return the patched object
      vi.mocked(repo.update).mockImplementation(async (id, patch) => ({
        ...makeTemplate({ id }),
        ...patch
      }));
      await store.load();

      await store.setDefault('t2');

      expect(store.templates.find((t) => t.id === 't1')?.isDefault).toBe(false);
      expect(store.templates.find((t) => t.id === 't2')?.isDefault).toBe(true);
    });

    it('only calls repo.update for templates whose isDefault actually changes', async () => {
      const t1 = makeTemplate({ id: 't1', isDefault: true });
      const t2 = makeTemplate({ id: 't2', isDefault: false });
      vi.mocked(repo.getAll).mockResolvedValueOnce([t1, t2]);
      vi.mocked(repo.update).mockResolvedValue(null);
      await store.load();

      // t1 is already default, t2 is already non-default — calling setDefault('t1') changes nothing
      await store.setDefault('t1');
      // Only t2 needs updating (false→false is unchanged; t1 stays true→true)
      // Both are already in correct state, so no update calls needed
      expect(repo.update).not.toHaveBeenCalled();
    });
  });

  // ─── deleteTemplate ──────────────────────────────────────────────────────

  describe('deleteTemplate', () => {
    it('removes the template from the store', async () => {
      const t1 = makeTemplate({ id: 't1', isDefault: false });
      const t2 = makeTemplate({ id: 't2', isDefault: false });
      vi.mocked(repo.getAll).mockResolvedValueOnce([t1, t2]);
      await store.load();

      await store.deleteTemplate('t1');

      expect(store.templates).toHaveLength(1);
      expect(store.templates[0].id).toBe('t2');
    });

    it('promotes first remaining template to default when the deleted one was default', async () => {
      const t1 = makeTemplate({ id: 't1', isDefault: true });
      const t2 = makeTemplate({ id: 't2', isDefault: false });
      vi.mocked(repo.getAll).mockResolvedValueOnce([t1, t2]);
      vi.mocked(repo.update).mockImplementation(async (id, patch) => ({
        ...makeTemplate({ id }),
        ...patch
      }));
      await store.load();

      await store.deleteTemplate('t1');

      expect(store.templates.find((t) => t.id === 't2')?.isDefault).toBe(true);
    });

    it('does not call setDefault when remaining templates already have a default', async () => {
      const t1 = makeTemplate({ id: 't1', isDefault: false });
      const t2 = makeTemplate({ id: 't2', isDefault: true });
      const t3 = makeTemplate({ id: 't3', isDefault: false });
      vi.mocked(repo.getAll).mockResolvedValueOnce([t1, t2, t3]);
      await store.load();

      await store.deleteTemplate('t1'); // t2 still has isDefault=true

      expect(repo.update).not.toHaveBeenCalled();
    });

    it('is safe when the last template is deleted', async () => {
      const t1 = makeTemplate({ id: 't1', isDefault: true });
      vi.mocked(repo.getAll).mockResolvedValueOnce([t1]);
      await store.load();

      await store.deleteTemplate('t1');

      expect(store.templates).toHaveLength(0);
      expect(repo.update).not.toHaveBeenCalled();
    });
  });
});
