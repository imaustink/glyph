import type { SearchResult, TreeNode } from '$lib/models/types';
import type { PageContentMatch } from '$lib/storage/interfaces';

const SNIPPET_RADIUS = 80;

const BLOCK_TYPES = new Set(['paragraph', 'heading', 'codeBlock', 'hardBreak']);

/**
 * Flatten a ProseMirror JSON document to its text, one block per line.
 * Mirrors the API's store.DocPlainText so both backends match the same way.
 */
export function docPlainText(doc: unknown): string {
  const parts: string[] = [];
  const walk = (node: unknown) => {
    if (!node || typeof node !== 'object') return;
    const n = node as { type?: unknown; text?: unknown; content?: unknown };
    if (typeof n.text === 'string') parts.push(n.text);
    if (Array.isArray(n.content)) n.content.forEach(walk);
    if (typeof n.type === 'string' && BLOCK_TYPES.has(n.type)) parts.push('\n');
  };
  walk(doc);
  return parts.join('').trim();
}

function normalize(s: string): string {
  return s.split(/\s+/).filter(Boolean).join(' ');
}

/**
 * A snippet of up to SNIPPET_RADIUS characters either side of the first
 * case-insensitive occurrence of `query` in `text` (whitespace runs, including
 * line breaks between blocks, count as one space), or null if there is none.
 */
export function contentSnippet(text: string, query: string): string | null {
  const q = normalize(query).toLowerCase();
  if (!q) return null;
  const flat = normalize(text);
  const idx = flat.toLowerCase().indexOf(q);
  // toLowerCase can change the length of a few characters (e.g. 'İ'); in that
  // case idx may be slightly off, which only shifts the snippet window.
  if (idx < 0) return null;
  const start = Math.max(0, idx - SNIPPET_RADIUS);
  const end = Math.min(flat.length, idx + q.length + SNIPPET_RADIUS);
  return (start > 0 ? '…' : '') + flat.slice(start, end) + (end < flat.length ? '…' : '');
}

/**
 * Add page-content matches to (title/tag) search results. A page already in
 * `results` gets the content snippet as its excerpt; any other page is
 * appended after the existing results. Matches for pages not in `nodes`
 * (e.g. deleted since) are dropped.
 */
export function mergeContentMatches(
  results: SearchResult[],
  matches: PageContentMatch[],
  nodes: TreeNode[]
): SearchResult[] {
  if (matches.length === 0) return results;
  const byId = new Map(nodes.map((n) => [n.id, n]));
  const merged = results.map((r) => ({ ...r }));
  const index = new Map(merged.filter((r) => r.type === 'page').map((r) => [r.id, r]));
  for (const m of matches) {
    const existing = index.get(m.pageId);
    if (existing) {
      if (!existing.excerpt && m.snippet) existing.excerpt = m.snippet;
      continue;
    }
    const node = byId.get(m.pageId);
    if (!node || node.type !== 'page') continue;
    const r: SearchResult = { id: node.id, type: 'page', title: node.title, excerpt: m.snippet, score: 1, highlights: [] };
    merged.push(r);
    index.set(r.id, r);
  }
  return merged;
}
