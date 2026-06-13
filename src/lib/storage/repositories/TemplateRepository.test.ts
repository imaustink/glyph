/**
 * Unit tests for TemplateRepository.
 *
 * TemplateRepository just calls super(adapter, 'templates').
 * Verifies it behaves as a proper Repository<NoteTemplate>.
 */

import { describe, it, expect, beforeEach } from 'vitest';
import { TemplateRepository } from './TemplateRepository';
import { LocalStorageAdapter } from '$lib/storage/LocalStorageAdapter';
import type { NoteTemplate } from '$lib/models/types';

function makeTemplate(overrides: Partial<NoteTemplate> = {}): NoteTemplate {
  return {
    id: 'tpl-1',
    name: 'My Template',
    content: '{}',
    titleTemplate: '',
    todoTrigger: { pattern: 'TODO', matchMode: 'exact', blockTypes: ['heading'] },
    isDefault: false,
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
    ...overrides
  };
}

describe('TemplateRepository', () => {
  let repo: TemplateRepository;

  beforeEach(() => {
    // LocalStorageAdapter requires a real window.localStorage — jsdom provides it.
    // Clear any state between tests by using a fresh adapter each time.
    localStorage.clear();
    repo = new TemplateRepository(new LocalStorageAdapter());
  });

  it('starts empty', async () => {
    expect(await repo.getAll()).toEqual([]);
  });

  it('creates and retrieves a template', async () => {
    const tpl = makeTemplate({ id: 't1' });
    const created = await repo.create(tpl);
    expect(created).toEqual(tpl);
    expect(await repo.getAll()).toHaveLength(1);
  });

  it('getById returns the correct template', async () => {
    await repo.create(makeTemplate({ id: 'a' }));
    await repo.create(makeTemplate({ id: 'b', name: 'Second' }));
    const found = await repo.getById('b');
    expect(found?.name).toBe('Second');
  });

  it('getById returns null for unknown id', async () => {
    expect(await repo.getById('missing')).toBeNull();
  });

  it('delete removes a template', async () => {
    await repo.create(makeTemplate({ id: 't1' }));
    expect(await repo.delete('t1')).toBe(true);
    expect(await repo.getAll()).toHaveLength(0);
  });

  it('uses the correct storage key (glyph:templates)', async () => {
    await repo.create(makeTemplate({ id: 't1' }));
    const raw = localStorage.getItem('glyph:templates');
    expect(raw).not.toBeNull();
    const parsed = JSON.parse(raw!);
    expect(Array.isArray(parsed)).toBe(true);
    expect(parsed[0].id).toBe('t1');
  });
});
