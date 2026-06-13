/**
 * Tests for FuseSearchProvider's buildExcerpt internal logic.
 *
 * Uses a mocked Fuse.js to control the exact match indices returned,
 * allowing coverage of the `from > 0` (leading ellipsis) branch on line 72.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';

// ── Mock Fuse.js before any imports ──────────────────────────────────────────
const mockSearch = vi.fn();
vi.mock('fuse.js', () => {
  // Fuse is used as `new Fuse(...)`, so we need a class-like constructor
  return {
    default: class MockFuse {
      search(...args: unknown[]) { return mockSearch(...args); }
    }
  };
});

// Import AFTER mock is set up
import { FuseSearchProvider } from './FuseSearchProvider';

describe('FuseSearchProvider buildExcerpt (mocked Fuse)', () => {
  let provider: FuseSearchProvider;

  const body = 'a'.repeat(200); // 200-char body

  beforeEach(async () => {
    provider = new FuseSearchProvider();
    await provider.index([
      { id: 'x1', type: 'page', title: 'Test', body, tags: [] }
    ]);
  });

  it('adds a leading ellipsis when body match starts after position 40 (covers line 72 from > 0)', async () => {
    // Fuse returns a body match with start index 100 → from = max(0, 100-40) = 60 > 0
    mockSearch.mockReturnValueOnce([
      {
        score: 0.1,
        item: { id: 'x1', type: 'page', title: 'Test', body, tags: [] },
        matches: [
          { key: 'body', value: body, indices: [[100, 107]] }
        ]
      }
    ]);

    const results = await provider.search('anything');
    expect(results).toHaveLength(1);
    // from = 60 > 0 → leading ellipsis prepended
    expect(results[0].excerpt).toMatch(/^…/);
  });

  it('omits leading ellipsis when body match starts at or before position 40', async () => {
    // start = 30 → from = max(0, 30-40) = 0 → no leading ellipsis
    mockSearch.mockReturnValueOnce([
      {
        score: 0.1,
        item: { id: 'x1', type: 'page', title: 'Test', body, tags: [] },
        matches: [
          { key: 'body', value: body, indices: [[30, 37]] }
        ]
      }
    ]);

    const results = await provider.search('anything');
    expect(results).toHaveLength(1);
    expect(results[0].excerpt).not.toMatch(/^…/);
  });

  it('adds a trailing ellipsis when body extends beyond excerpt window', async () => {
    // start = 0, from = 0, to = min(200, 0+140) = 140 < 200 → trailing ellipsis
    mockSearch.mockReturnValueOnce([
      {
        score: 0.1,
        item: { id: 'x1', type: 'page', title: 'Test', body, tags: [] },
        matches: [
          { key: 'body', value: body, indices: [[0, 5]] }
        ]
      }
    ]);

    const results = await provider.search('anything');
    expect(results).toHaveLength(1);
    expect(results[0].excerpt).toMatch(/…$/);
  });

  it('returns first 140 chars when body match is missing', async () => {
    // No body match → fallback to body.slice(0, 140)
    mockSearch.mockReturnValueOnce([
      {
        score: 0.1,
        item: { id: 'x1', type: 'page', title: 'Test', body, tags: [] },
        matches: [] // no matches at all
      }
    ]);

    const results = await provider.search('anything');
    expect(results).toHaveLength(1);
    expect(results[0].excerpt).toBe(body.slice(0, 140));
  });

  it('uses score=1 when r.score is undefined (covers ?? 1 branch, line 44)', async () => {
    mockSearch.mockReturnValueOnce([
      {
        score: undefined, // undefined → r.score ?? 1 returns 1
        item: { id: 'x1', type: 'page', title: 'Test', body, tags: [] },
        matches: []
      }
    ]);

    const results = await provider.search('anything');
    expect(results).toHaveLength(1);
    expect(results[0].score).toBe(1);
  });

  it('returns empty highlights when r.matches is undefined (covers !matches branch, line 52)', async () => {
    mockSearch.mockReturnValueOnce([
      {
        score: 0.5,
        item: { id: 'x1', type: 'page', title: 'Test', body, tags: [] },
        matches: undefined // → extractHighlights(undefined) → !matches → return []
      }
    ]);

    const results = await provider.search('anything');
    expect(results).toHaveLength(1);
    expect(results[0].highlights).toEqual([]);
  });

  it('collects title match indices into highlights (covers lines 56-57)', async () => {
    mockSearch.mockReturnValueOnce([
      {
        score: 0.1,
        item: { id: 'x1', type: 'page', title: 'Test', body, tags: [] },
        matches: [
          { key: 'title', value: 'Test', indices: [[0, 3]] }
        ]
      }
    ]);

    const results = await provider.search('anything');
    expect(results).toHaveLength(1);
    // Title match should appear in highlights
    expect(results[0].highlights).toContainEqual([0, 3]);
  });
});
