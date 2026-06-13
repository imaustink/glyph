import Fuse from 'fuse.js';
import type { FuseResultMatch } from 'fuse.js';
import type { SearchProvider, SearchableItem, SearchResult, SearchOptions } from '$lib/models/types';

export class FuseSearchProvider implements SearchProvider {
  readonly isAsync = false;
  private fuse: Fuse<SearchableItem> | null = null;
  private items: SearchableItem[] = [];

  async index(items: SearchableItem[]): Promise<void> {
    this.items = items;
    this.fuse = new Fuse(items, {
      keys: [
        { name: 'title', weight: 0.6 },
        { name: 'body', weight: 0.3 },
        { name: 'tags', weight: 0.1 }
      ],
      includeScore: true,
      includeMatches: true,
      threshold: 0.4,
      minMatchCharLength: 2
    });
  }

  async search(query: string, options?: SearchOptions): Promise<SearchResult[]> {
    if (!this.fuse || !query.trim()) return [];

    const raw = this.fuse.search(query, { limit: options?.limit ?? 50 });

    return raw
      .filter((r) => {
        if (options?.types && !options.types.includes(r.item.type)) return false;
        return true;
      })
      .map((r): SearchResult => {
        const highlights = extractHighlights(r.matches);
        const bodyMatch = r.matches?.find((m) => m.key === 'body');
        const excerpt = buildExcerpt(r.item.body, bodyMatch);
        return {
          id: r.item.id,
          type: r.item.type,
          title: r.item.title,
          excerpt,
          score: r.score ?? 1,
          highlights
        };
      });
  }
}

function extractHighlights(matches?: readonly FuseResultMatch[]): [number, number][] {
  if (!matches) return [];
  const result: [number, number][] = [];
  for (const match of matches) {
    if (match.key === 'title') {
      for (const [start, end] of match.indices) {
        result.push([start, end]);
      }
    }
  }
  return result;
}

function buildExcerpt(body: string, match?: FuseResultMatch): string {
  if (!match || !match.indices.length) {
    return body.slice(0, 140);
  }
  const [start] = match.indices[0];
  const from = Math.max(0, start - 40);
  const to = Math.min(body.length, from + 140);
  const excerpt = body.slice(from, to);
  return (from > 0 ? '…' : '') + excerpt + (to < body.length ? '…' : '');
}
