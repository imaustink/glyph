/**
 * URL preview cards for the note editor.
 *
 * When enabled, a link that sits alone on its own line (a "bare" auto-linked
 * URL, where the visible text is the URL itself) gets an OG-metadata preview
 * card rendered beneath it. The card is a ProseMirror *widget decoration*: it
 * is view-only chrome that never enters the document.
 *
 * That is deliberate and load-bearing. This extension is added by Editor.svelte
 * on top of the schema-defining extensions in schema.ts, and — per the contract
 * documented there — behaviour-only extensions must not add nodes, marks or
 * attributes, or the schema fingerprint (and the collab data-integrity check
 * built on it) stops covering the real editor schema. Decorations touch neither
 * the document nor the schema, so previews are safe in collaborative editing:
 * every client renders its own cards from the same shared document.
 *
 * The feature is opt-in and defaults to off (see the preferences store), so
 * turning it on or off never changes a single byte of stored content.
 */
import { Extension } from '@tiptap/core';
import { Plugin, PluginKey, type Transaction } from '@tiptap/pm/state';
import { Decoration, DecorationSet } from '@tiptap/pm/view';
import type { Node as PMNode } from '@tiptap/pm/model';
import type { LinkMeta } from '$lib/models/types';

export interface UrlPreviewOptions {
  /** Render preview cards. When false the plugin holds no decorations. */
  enabled: boolean;
  /**
   * Fetch OG metadata for a URL. Returns null when a preview can't be built
   * (e.g. no backend available, or the fetch failed). null links render no
   * card. Provided by Editor.svelte; only wired up when the API backend — the
   * only place the unfurl endpoint lives — is in use.
   */
  fetchMeta: ((url: string) => Promise<LinkMeta | null>) | null;
}

interface CacheEntry {
  status: 'loading' | 'ready';
  /** Present only when status === 'ready' and a preview could be built. */
  meta?: LinkMeta;
}

/**
 * URL → metadata cache, shared across editor instances for the session so
 * revisiting a note (or the same link on another page) doesn't refetch.
 */
const cache = new Map<string, CacheEntry>();

/** Exposed for tests — reset the shared cache between cases. */
export function __clearUrlPreviewCache(): void {
  cache.clear();
}

interface PluginState {
  enabled: boolean;
  decorations: DecorationSet;
}

interface PluginMeta {
  enabled?: boolean;
  /** A finished fetch asks the plugin to rebuild its cards from the cache. */
  refresh?: boolean;
}

export const urlPreviewPluginKey = new PluginKey<PluginState>('url-preview');

/** Build a transaction meta that turns previews on or off for this editor. */
export function setUrlPreviewEnabled(tr: Transaction, enabled: boolean): Transaction {
  return tr.setMeta(urlPreviewPluginKey, { enabled } satisfies PluginMeta);
}

/**
 * If `node` is a paragraph whose entire content is a single bare link — the
 * visible text equals its http(s) href — return that href, else null. This is
 * exactly the shape auto-link produces for a URL typed or pasted on its own
 * line; a link given custom anchor text is left alone, since the user chose
 * that text deliberately.
 */
function standaloneUrl(node: PMNode): string | null {
  if (node.type.name !== 'paragraph' || node.childCount !== 1) return null;
  const child = node.firstChild;
  if (!child || !child.isText || !child.text) return null;
  const link = child.marks.find((m) => m.type.name === 'link');
  if (!link) return null;
  const href = typeof link.attrs.href === 'string' ? link.attrs.href : '';
  if (!/^https?:\/\//i.test(href)) return null;
  const text = child.text.trim();
  if (text !== href && text.replace(/\/+$/, '') !== href.replace(/\/+$/, '')) return null;
  return href;
}

/** Collect every top-level standalone URL and the doc position after its block. */
function collect(doc: PMNode): { href: string; pos: number }[] {
  const out: { href: string; pos: number }[] = [];
  doc.forEach((node, offset) => {
    const href = standaloneUrl(node);
    if (href) out.push({ href, pos: offset + node.nodeSize });
  });
  return out;
}

function displayUrl(url: string): string {
  try {
    const u = new URL(url);
    return u.hostname + (u.pathname !== '/' ? u.pathname : '');
  } catch {
    return url;
  }
}

function hideOnError(img: HTMLImageElement): void {
  img.addEventListener('error', () => {
    img.style.display = 'none';
  });
}

/**
 * Build the card DOM for a URL from whatever is currently cached. A loading
 * entry renders a placeholder; a ready entry with metadata renders the card.
 * (Ready-but-empty entries never reach here — `build` drops them.)
 */
function renderCard(href: string): HTMLElement {
  const wrap = document.createElement('div');
  wrap.className = 'url-preview-card';
  wrap.contentEditable = 'false';
  wrap.setAttribute('data-url-preview', href);

  const entry = cache.get(href);
  if (!entry || entry.status === 'loading' || !entry.meta) {
    wrap.classList.add('is-loading');
    const skeleton = document.createElement('div');
    skeleton.className = 'url-preview-loading';
    skeleton.textContent = 'Loading preview…';
    wrap.appendChild(skeleton);
    return wrap;
  }

  const meta = entry.meta;
  const a = document.createElement('a');
  a.className = 'url-preview-link';
  a.href = href;
  a.target = '_blank';
  a.rel = 'noopener noreferrer';
  a.title = href;

  if (meta.image) {
    const imageWrap = document.createElement('div');
    imageWrap.className = 'url-preview-image';
    const img = document.createElement('img');
    img.src = meta.image;
    img.alt = '';
    hideOnError(img);
    imageWrap.appendChild(img);
    a.appendChild(imageWrap);
  }

  const body = document.createElement('div');
  body.className = 'url-preview-body';

  if (meta.favicon || meta.siteName) {
    const header = document.createElement('div');
    header.className = 'url-preview-header';
    if (meta.favicon) {
      const fav = document.createElement('img');
      fav.className = 'url-preview-favicon';
      fav.src = meta.favicon;
      fav.alt = '';
      fav.width = 16;
      fav.height = 16;
      hideOnError(fav);
      header.appendChild(fav);
    }
    if (meta.siteName) {
      const site = document.createElement('span');
      site.className = 'url-preview-site';
      site.textContent = meta.siteName;
      header.appendChild(site);
    }
    body.appendChild(header);
  }

  if (meta.title) {
    const title = document.createElement('div');
    title.className = 'url-preview-title';
    title.textContent = meta.title;
    body.appendChild(title);
  }

  if (meta.description) {
    const desc = document.createElement('div');
    desc.className = 'url-preview-desc';
    desc.textContent = meta.description;
    body.appendChild(desc);
  }

  const url = document.createElement('div');
  url.className = 'url-preview-url';
  url.textContent = displayUrl(href);
  body.appendChild(url);

  a.appendChild(body);
  wrap.appendChild(a);
  return wrap;
}

/** Build the decoration set for a document from the current cache. */
function build(doc: PMNode): DecorationSet {
  const decorations: Decoration[] = [];
  for (const { href, pos } of collect(doc)) {
    const entry = cache.get(href);
    // A ready entry with no metadata means the preview couldn't be built —
    // render nothing rather than a stuck placeholder.
    if (entry?.status === 'ready' && !entry.meta) continue;
    const status = entry?.status ?? 'loading';
    decorations.push(
      Decoration.widget(pos, () => renderCard(href), {
        side: 1,
        // The status is part of the key so a loading → ready transition
        // replaces the placeholder DOM instead of reusing it; the position
        // keeps the key unique if the same URL appears on two lines.
        key: `url-preview:${pos}:${href}:${status}`,
        ignoreSelection: true
      })
    );
  }
  return decorations.length ? DecorationSet.create(doc, decorations) : DecorationSet.empty;
}

export const UrlPreviewExtension = Extension.create<UrlPreviewOptions>({
  name: 'urlPreview',

  addOptions() {
    return {
      enabled: false,
      fetchMeta: null
    };
  },

  addProseMirrorPlugins() {
    const options = this.options;

    return [
      new Plugin<PluginState>({
        key: urlPreviewPluginKey,

        state: {
          init: (_config, state) => ({
            enabled: options.enabled,
            decorations: options.enabled ? build(state.doc) : DecorationSet.empty
          }),
          apply: (tr, prev, _oldState, newState) => {
            const meta = tr.getMeta(urlPreviewPluginKey) as PluginMeta | undefined;
            const enabled = meta?.enabled ?? prev.enabled;
            const toggled = meta?.enabled !== undefined && meta.enabled !== prev.enabled;
            const needsRebuild = tr.docChanged || meta?.refresh || toggled;

            if (!needsRebuild) {
              // Positions may have shifted (they haven't, on a selection-only
              // change, but map anyway to stay correct) — no content rebuild.
              return { enabled, decorations: prev.decorations.map(tr.mapping, tr.doc) };
            }
            return {
              enabled,
              decorations: enabled ? build(newState.doc) : DecorationSet.empty
            };
          }
        },

        props: {
          decorations(state) {
            return urlPreviewPluginKey.getState(state)?.decorations ?? DecorationSet.empty;
          }
        },

        view: (view) => {
          const maybeFetch = () => {
            const pluginState = urlPreviewPluginKey.getState(view.state);
            const fetchMeta = options.fetchMeta;
            if (!pluginState?.enabled || !fetchMeta) return;

            for (const { href } of collect(view.state.doc)) {
              if (cache.has(href)) continue;
              cache.set(href, { status: 'loading' });
              void fetchMeta(href)
                .then((meta) => {
                  cache.set(href, { status: 'ready', meta: meta ?? undefined });
                })
                .catch(() => {
                  cache.set(href, { status: 'ready' });
                })
                .finally(() => {
                  if (view.isDestroyed) return;
                  view.dispatch(
                    view.state.tr.setMeta(urlPreviewPluginKey, { refresh: true } satisfies PluginMeta)
                  );
                });
            }
          };

          maybeFetch();
          return { update: maybeFetch };
        }
      })
    ];
  }
});
