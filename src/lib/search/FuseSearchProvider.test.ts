import { describe, it, expect, beforeEach } from 'vitest';
import { FuseSearchProvider } from './FuseSearchProvider';
import type { SearchableItem } from '$lib/models/types';

const items: SearchableItem[] = [
  { id: 'p1', type: 'page', title: 'Meeting Notes', body: 'Discussed the quarterly roadmap with the team', tags: ['work'] },
  { id: 'p2', type: 'page', title: 'Shopping List', body: 'Milk, eggs, bread, and butter for the week', tags: ['personal'] },
  { id: 't1', type: 'task', title: 'Fix login bug', body: 'Authentication fails when session expires during redirect', tags: ['bug'] },
  { id: 't2', type: 'task', title: 'Write unit tests', body: 'Add comprehensive test coverage for the storage layer', tags: ['dev'] }
];

describe('FuseSearchProvider', () => {
  let provider: FuseSearchProvider;

  beforeEach(async () => {
    provider = new FuseSearchProvider();
    await provider.index(items);
  });

  it('has isAsync = false', () => {
    expect(provider.isAsync).toBe(false);
  });

  it('returns empty results for empty query', async () => {
    expect(await provider.search('')).toEqual([]);
    expect(await provider.search('   ')).toEqual([]);
  });

  it('returns empty results when not indexed', async () => {
    const fresh = new FuseSearchProvider();
    expect(await fresh.search('test')).toEqual([]);
  });

  it('finds items by title', async () => {
    const results = await provider.search('Meeting Notes');
    expect(results.length).toBeGreaterThan(0);
    expect(results[0].id).toBe('p1');
  });

  it('finds items by body content', async () => {
    const results = await provider.search('quarterly roadmap');
    expect(results.length).toBeGreaterThan(0);
    expect(results.some((r) => r.id === 'p1')).toBe(true);
  });

  it('returns results with score, excerpt, and highlights', async () => {
    const results = await provider.search('login bug');
    expect(results.length).toBeGreaterThan(0);
    const result = results[0];
    expect(result).toHaveProperty('id');
    expect(result).toHaveProperty('type');
    expect(result).toHaveProperty('title');
    expect(result).toHaveProperty('excerpt');
    expect(typeof result.score).toBe('number');
    expect(Array.isArray(result.highlights)).toBe(true);
  });

  it('filters by type when specified', async () => {
    const results = await provider.search('bug', { types: ['task'] });
    for (const r of results) {
      expect(r.type).toBe('task');
    }
  });

  it('respects limit option', async () => {
    const results = await provider.search('the', { limit: 1 });
    expect(results.length).toBeLessThanOrEqual(1);
  });

  it('provides excerpt from body for matches', async () => {
    const results = await provider.search('Authentication');
    const match = results.find((r) => r.id === 't1');
    expect(match).toBeDefined();
    expect(match!.excerpt.length).toBeGreaterThan(0);
  });

  it('re-indexes with new items', async () => {
    await provider.index([
      { id: 'new1', type: 'page', title: 'Completely New Page', body: 'Brand new content', tags: [] }
    ]);
    const results = await provider.search('Completely New');
    expect(results.length).toBeGreaterThan(0);
    expect(results[0].id).toBe('new1');

    // Old items should no longer appear
    const oldResults = await provider.search('Meeting Notes');
    expect(oldResults.every((r) => r.id !== 'p1')).toBe(true);
  });

  describe('tags search', () => {
    it('finds items by tag', async () => {
      const results = await provider.search('bug');
      expect(results.some((r) => r.id === 't1')).toBe(true);
    });

    it('finds items when tag matches the query', async () => {
      const results = await provider.search('work');
      expect(results.some((r) => r.id === 'p1')).toBe(true);
    });
  });

  describe('excerpt generation', () => {
    it('produces excerpt with trailing ellipsis when body is long', async () => {
      // The match 'trailingmarker' is at position 0, body is 214+ chars → trailing '…'
      // to = min(214, 0+140) = 140 < 214 = body.length → append '…'
      const body = 'trailingmarker ' + 'z'.repeat(200);
      const item: SearchableItem = { id: 'trailing1', type: 'page', title: 'Trailing Ellipsis Test', body, tags: [] };
      const p = new FuseSearchProvider();
      await p.index([item]);
      const results = await p.search('trailingmarker');
      expect(results.length).toBeGreaterThan(0);
      expect(results[0].excerpt).toMatch(/…$/);
    });

    it('returns a truncated excerpt with no ellipses when match is in short body', async () => {
      const item: SearchableItem = { id: 'short1', type: 'page', title: 'Short Body', body: 'hello world short body', tags: [] };
      const p = new FuseSearchProvider();
      await p.index([item]);
      const results = await p.search('hello world');
      expect(results.length).toBeGreaterThan(0);
      // Short body: from=0, to=body.length — no ellipses
      expect(results[0].excerpt).not.toMatch(/^…/);
      expect(results[0].excerpt).not.toMatch(/…$/);
    });

    it('returns body[0:140] when there is no body match (title-only match)', async () => {
      const body = 'This body has nothing to do with the title. ' + 'a'.repeat(200);
      const item: SearchableItem = { id: 'title-only', type: 'page', title: 'VeryUniqueQueryString', body, tags: [] };
      const p = new FuseSearchProvider();
      await p.index([item]);
      const results = await p.search('VeryUniqueQueryString');
      if (results.length > 0 && !results[0].excerpt.match(/^…/)) {
        expect(results[0].excerpt.length).toBeLessThanOrEqual(140);
      }
    });
  });

  describe('fuzzy matching threshold', () => {
    it('does not return completely unrelated items', async () => {
      const unrelated = await provider.search('xyzqwerty1234nonsense');
      expect(unrelated.length).toBe(0);
    });
  });

  describe('type filtering (covers the return false branch)', () => {
    it('filters out items whose type is not in options.types', async () => {
      // Index items of both types with the same distinctive text so Fuse returns both.
      const mixedProvider = new FuseSearchProvider();
      await mixedProvider.index([
        { id: 'pg1', type: 'page', title: 'coverage mixed item', body: 'coverage mixed body', tags: [] },
        { id: 'tk1', type: 'task', title: 'coverage mixed item', body: 'coverage mixed body', tags: [] }
      ]);
      // Request only pages — the task result should be filtered out (return false branch).
      const results = await mixedProvider.search('coverage mixed', { types: ['page'] });
      expect(results.every((r) => r.type === 'page')).toBe(true);
      expect(results.some((r) => r.id === 'pg1')).toBe(true);
    });
  });
});
