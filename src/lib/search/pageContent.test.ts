import { describe, it, expect } from 'vitest';
import { docPlainText, contentSnippet, mergeContentMatches } from './pageContent';
import type { SearchResult, TreeNode } from '$lib/models/types';

function node(id: string, title: string, type: 'page' | 'folder' = 'page'): TreeNode {
  return {
    id, type, title, parentId: null, order: 0, tags: [],
    createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z'
  } as TreeNode;
}

function result(id: string, overrides: Partial<SearchResult> = {}): SearchResult {
  return { id, type: 'page', title: id, excerpt: '', score: 0.1, highlights: [], ...overrides };
}

describe('docPlainText', () => {
  it('joins text nodes and puts blocks on their own lines', () => {
    const doc = {
      type: 'doc',
      content: [
        { type: 'heading', attrs: { level: 1 }, content: [{ type: 'text', text: 'Title' }] },
        { type: 'paragraph', content: [
          { type: 'text', text: 'hello ' },
          { type: 'text', text: 'bold', marks: [{ type: 'bold' }] },
          { type: 'text', text: ' world' }
        ] }
      ]
    };
    expect(docPlainText(doc)).toBe('Title\nhello bold world');
  });

  it('ignores node type names and attributes', () => {
    const doc = { type: 'doc', content: [{ type: 'paragraph', attrs: { id: 'paragraph' } }] };
    expect(docPlainText(doc)).toBe('');
  });

  it('returns "" for malformed input', () => {
    expect(docPlainText(null)).toBe('');
    expect(docPlainText('nope')).toBe('');
  });
});

describe('contentSnippet', () => {
  it('matches case-insensitively and across block boundaries', () => {
    expect(contentSnippet('First line\nsecond line', 'LINE SECOND')).toBe('First line second line');
  });

  it('returns null when there is no match or the query is blank', () => {
    expect(contentSnippet('abc', 'xyz')).toBeNull();
    expect(contentSnippet('abc', '   ')).toBeNull();
  });

  it('trims long text around the match with ellipses', () => {
    const text = 'a'.repeat(200) + ' needle ' + 'b'.repeat(200);
    const s = contentSnippet(text, 'needle')!;
    expect(s.startsWith('…')).toBe(true);
    expect(s.endsWith('…')).toBe(true);
    expect(s).toContain('needle');
    expect(s.length).toBeLessThan(200);
  });
});

describe('mergeContentMatches', () => {
  const nodes = [node('p1', 'One'), node('p2', 'Two'), node('f1', 'Folder', 'folder')];

  it('appends content-only matches after existing results', () => {
    const merged = mergeContentMatches([result('p1')], [{ pageId: 'p2', snippet: 'snip' }], nodes);
    expect(merged.map((r) => r.id)).toEqual(['p1', 'p2']);
    expect(merged[1]).toMatchObject({ type: 'page', title: 'Two', excerpt: 'snip' });
  });

  it('adds the snippet to a page already in the results instead of duplicating it', () => {
    const input = [result('p1')];
    const merged = mergeContentMatches(input, [{ pageId: 'p1', snippet: 'snip' }], nodes);
    expect(merged).toHaveLength(1);
    expect(merged[0].excerpt).toBe('snip');
    expect(input[0].excerpt).toBe(''); // input not mutated
  });

  it('does not merge into a task result with the same id', () => {
    const merged = mergeContentMatches([result('p1', { type: 'task' })], [{ pageId: 'p1', snippet: 's' }], nodes);
    expect(merged.map((r) => r.type)).toEqual(['task', 'page']);
  });

  it('drops matches for unknown pages and folders', () => {
    const merged = mergeContentMatches([], [{ pageId: 'gone', snippet: 's' }, { pageId: 'f1', snippet: 's' }], nodes);
    expect(merged).toEqual([]);
  });
});
