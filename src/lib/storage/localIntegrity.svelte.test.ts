/**
 * DI-26: localStorage-mode integrity.
 *
 * Uses the real LocalStorageAdapter (jsdom's localStorage) and real
 * repositories. Two repository instances over the same storage stand in for
 * two browser tabs: each has its own in-memory cache and write queue.
 */

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { LocalStorageAdapter } from './LocalStorageAdapter';
import { PageRepository } from './repositories/PageRepository';
import { TaskRepository } from './repositories/TaskRepository';
import { LaneRepository } from './repositories/LaneRepository';
import { TemplateRepository } from './repositories/TemplateRepository';
import { ApiError, apiErrorCode } from './apiClient';
import { createPagesStore } from '$lib/stores/pages.svelte';
import { createLanesStore } from '$lib/stores/lanes.svelte';
import { createTemplatesStore } from '$lib/stores/templates.svelte';
import type { Lane, NoteTemplate, Task, TreeNode } from '$lib/models/types';

function makeNode(overrides: Partial<TreeNode> = {}): TreeNode {
  return {
    id: 'n1',
    type: 'page',
    title: 'Page',
    parentId: null,
    order: 0,
    tags: [],
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
    ...overrides
  };
}

function makeTask(overrides: Partial<Task> = {}): Task {
  return {
    id: 't1',
    title: 'Task',
    description: '',
    status: 'todo',
    priority: 'none',
    tags: [],
    dueDate: null,
    sourcePageId: null,
    sourceNodeId: null,
    link: null,
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
    order: 0,
    ...overrides
  };
}

function makeLane(overrides: Partial<Lane> = {}): Lane {
  return {
    id: 'l1',
    title: 'Lane',
    filterSet: { conjunction: 'and', rules: [] },
    sortConfig: { mode: 'auto' },
    order: 0,
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
    ...overrides
  };
}

function makeTemplate(overrides: Partial<NoteTemplate> = {}): NoteTemplate {
  return {
    id: 'tpl1',
    name: 'Tpl',
    content: '{}',
    titleTemplate: '',
    todoTrigger: { pattern: 'TODO', matchMode: 'exact', blockTypes: ['heading'] },
    isDefault: true,
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
    ...overrides
  };
}

function corruptBackupKeys(collection: string): string[] {
  return Object.keys(localStorage).filter((k) => k.startsWith(`glyph:corrupt:${collection}:`));
}

/** A minimal Web Locks stand-in: requests with the same name run one at a time. */
function installLocksPolyfill() {
  const chains = new Map<string, Promise<unknown>>();
  const locks = {
    request: (name: string, cb: () => Promise<unknown>) => {
      const run = (chains.get(name) ?? Promise.resolve()).then(cb);
      chains.set(name, run.catch(() => {}));
      return run;
    }
  };
  Object.defineProperty(navigator, 'locks', { value: locks, configurable: true });
  return () => {
    delete (navigator as unknown as { locks?: unknown }).locks;
  };
}

beforeEach(() => {
  localStorage.clear();
});

describe('corrupt collections are backed up and never overwritten [DI-26]', () => {
  it('unparseable JSON is backed up, and writes to that key are refused', async () => {
    localStorage.setItem('glyph:tasks', 'not-json{{');
    const repo = new TaskRepository(new LocalStorageAdapter());
    vi.spyOn(console, 'error').mockImplementation(() => {});

    expect(await repo.getAll()).toEqual([]);
    const backups = corruptBackupKeys('tasks');
    expect(backups).toHaveLength(1);
    expect(localStorage.getItem(backups[0])).toBe('not-json{{');

    await expect(repo.create(makeTask())).rejects.toThrow(/corrupt/i);
    expect(localStorage.getItem('glyph:tasks')).toBe('not-json{{');
  });

  it('a non-array value is backed up, and writes to that key are refused', async () => {
    localStorage.setItem('glyph:lanes', JSON.stringify({ not: 'an array' }));
    const repo = new LaneRepository(new LocalStorageAdapter());
    vi.spyOn(console, 'error').mockImplementation(() => {});

    expect(await repo.getAll()).toEqual([]);
    const backups = corruptBackupKeys('lanes');
    expect(backups).toHaveLength(1);
    expect(JSON.parse(localStorage.getItem(backups[0])!)).toEqual({ not: 'an array' });

    await expect(repo.create(makeLane())).rejects.toThrow(/corrupt/i);
    expect(JSON.parse(localStorage.getItem('glyph:lanes')!)).toEqual({ not: 'an array' });
  });

  it('first-run seeding does not overwrite a corrupt collection', async () => {
    localStorage.setItem('glyph:lanes', 'garbage');
    const store = createLanesStore(new LaneRepository(new LocalStorageAdapter()));
    vi.spyOn(console, 'error').mockImplementation(() => {});
    await store.load();

    await expect(store.seedDefaults()).rejects.toThrow(/corrupt/i);
    expect(localStorage.getItem('glyph:lanes')).toBe('garbage');
  });

  it('backs up a corrupt value only once', async () => {
    localStorage.setItem('glyph:tasks', '{{');
    const repo = new TaskRepository(new LocalStorageAdapter());
    vi.spyOn(console, 'error').mockImplementation(() => {});
    await repo.getAll();
    await repo.getAll();
    expect(corruptBackupKeys('tasks')).toHaveLength(1);
  });
});

describe('folder lanes stay off the global board [DI-26]', () => {
  it('LaneRepository.getOrdered excludes folder-board lanes', async () => {
    const repo = new LaneRepository(new LocalStorageAdapter());
    await repo.create(makeLane({ id: 'global', order: 1 }));
    await repo.create(makeLane({ id: 'folder-lane', order: 0, folderId: 'f1' }));

    expect((await repo.getOrdered()).map((l) => l.id)).toEqual(['global']);
  });
});

describe('cross-tab writes [DI-26]', () => {
  it('a write from a tab with a stale cache does not drop another tab\'s write', async () => {
    const tabA = new TaskRepository(new LocalStorageAdapter());
    const tabB = new TaskRepository(new LocalStorageAdapter());
    await tabA.getAll(); // warm tab A's cache

    await tabB.create(makeTask({ id: 'from-b' }));
    await tabA.create(makeTask({ id: 'from-a' }));

    const stored = JSON.parse(localStorage.getItem('glyph:tasks')!) as Task[];
    expect(stored.map((t) => t.id).sort()).toEqual(['from-a', 'from-b']);
  });

  it('deleting a folder from a stale tab also deletes children another tab created', async () => {
    const tabA = new PageRepository(new LocalStorageAdapter());
    const tabB = new PageRepository(new LocalStorageAdapter());
    await tabA.create(makeNode({ id: 'folder', type: 'folder' }));
    await tabA.getAll();

    await tabB.create(makeNode({ id: 'late-child', parentId: 'folder' }));
    await tabB.saveContent({ pageId: 'late-child', content: { type: 'doc', content: [] }, updatedAt: '2026-01-01T00:00:00Z' });

    // Tab A only knows about the folder itself.
    await tabA.deleteSubtree('folder', []);

    const stored = JSON.parse(localStorage.getItem('glyph:pages')!) as TreeNode[];
    expect(stored).toEqual([]);
    expect(localStorage.getItem('glyph:content:late-child')).toBeNull();
  });
});

describe('local content revision check [DI-26]', () => {
  it('a save derived from a stale read is rejected like the API\'s 409', async () => {
    const tabA = new PageRepository(new LocalStorageAdapter());
    const tabB = new PageRepository(new LocalStorageAdapter());
    await tabA.saveContent({ pageId: 'p1', content: { type: 'doc', content: [] }, updatedAt: '2026-01-01T00:00:00Z' });

    const readA = await tabA.getContent('p1');
    const readB = await tabB.getContent('p1');
    expect(typeof readA?.revision).toBe('number');

    const savedB = await tabB.saveContent({
      pageId: 'p1',
      content: { type: 'doc', content: [{ type: 'paragraph' }] },
      updatedAt: '2026-01-01T00:00:01Z',
      expectedRevision: readB!.revision
    });
    expect(savedB && savedB.revision).toBe(readB!.revision! + 1);

    const err = await tabA
      .saveContent({
        pageId: 'p1',
        content: { type: 'doc', content: [] },
        updatedAt: '2026-01-01T00:00:02Z',
        expectedRevision: readA!.revision
      })
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).status).toBe(409);
    expect(apiErrorCode(err)).toBe('stale_revision');

    // Tab B's content survived.
    expect((await tabA.getContent('p1'))?.content).toEqual({ type: 'doc', content: [{ type: 'paragraph' }] });
  });
});

describe('deleted default folders [DI-26]', () => {
  it('createPage falls back to the root when the parent no longer exists', async () => {
    const store = createPagesStore(new PageRepository(new LocalStorageAdapter()));
    await store.load();

    const page = await store.createPage('deleted-folder', 'From template');

    expect(page.parentId).toBeNull();
    expect(store.getChildren(null).map((n) => n.id)).toContain(page.id);
  });

  it('clearing a deleted folder resets templates\' defaultFolderId in store and storage', async () => {
    const repo = new TemplateRepository(new LocalStorageAdapter());
    await repo.create(makeTemplate({ id: 'uses-folder', defaultFolderId: 'f1' }));
    await repo.create(makeTemplate({ id: 'other', isDefault: false, defaultFolderId: 'f2' }));
    const store = createTemplatesStore(repo);
    await store.load();

    await store.clearDefaultFolder(['f1', 'child-of-f1']);

    expect(store.templates.find((t) => t.id === 'uses-folder')?.defaultFolderId).toBeNull();
    expect(store.templates.find((t) => t.id === 'other')?.defaultFolderId).toBe('f2');
    const stored = JSON.parse(localStorage.getItem('glyph:templates')!) as NoteTemplate[];
    expect(stored.find((t) => t.id === 'uses-folder')?.defaultFolderId).toBeNull();
  });
});

describe('first-run seeding in two tabs [Low]', () => {
  it('two tabs seeding at once create one set of default lanes', async () => {
    const restore = installLocksPolyfill();
    try {
      const tabA = createLanesStore(new LaneRepository(new LocalStorageAdapter()));
      const tabB = createLanesStore(new LaneRepository(new LocalStorageAdapter()));
      await Promise.all([tabA.load(), tabB.load()]);

      await Promise.all([tabA.seedDefaults(), tabB.seedDefaults()]);

      const stored = JSON.parse(localStorage.getItem('glyph:lanes')!) as Lane[];
      expect(stored).toHaveLength(4);
      expect(tabB.lanes.map((l) => l.id).sort()).toEqual(tabA.lanes.map((l) => l.id).sort());
    } finally {
      restore();
    }
  });

  it('two tabs seeding at once create one default template', async () => {
    const restore = installLocksPolyfill();
    try {
      const tabA = createTemplatesStore(new TemplateRepository(new LocalStorageAdapter()));
      const tabB = createTemplatesStore(new TemplateRepository(new LocalStorageAdapter()));
      await Promise.all([tabA.load(), tabB.load()]);

      await Promise.all([tabA.seedDefaults(), tabB.seedDefaults()]);

      const stored = JSON.parse(localStorage.getItem('glyph:templates')!) as NoteTemplate[];
      expect(stored).toHaveLength(1);
      expect(tabB.templates.map((t) => t.id)).toEqual(tabA.templates.map((t) => t.id));
    } finally {
      restore();
    }
  });
});
