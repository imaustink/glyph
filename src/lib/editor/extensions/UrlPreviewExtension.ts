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
import { Decoration, DecorationSet, type EditorView } from '@tiptap/pm/view';
import type { Node as PMNode } from '@tiptap/pm/model';
import type { LinkMeta } from '$lib/models/types';

export interface UrlPreviewOptions {
  /** Render preview cards. When false the plugin holds no decorations. */
  enabled: boolean;
  /**
   * Fetch OG metadata for a URL. Resolve null when the URL definitively has no
   * preview (e.g. the server rejected it); such links render no card for the
   * session, as do results with no title, description or image. Reject on a
   * transient failure (network error, 429, 5xx): the card is hidden for now,
   * all preview fetches pause for a growing backoff, and the URL is retried
   * after it. Calls are rate-limited by a shared queue (see below). Provided by
   * Editor.svelte; only wired up when the API backend — the only place the
   * unfurl endpoint lives — is in use.
   */
  fetchMeta: ((url: string) => Promise<LinkMeta | null>) | null;
}

interface CacheEntry {
  /**
   * 'failed' means the fetch threw (a transient failure). Like a ready entry
   * with no metadata it renders nothing, but unlike one it is refetched once
   * `retryAt` has passed.
   */
  status: 'loading' | 'ready' | 'failed';
  /** Present only when status === 'ready' and a preview could be built. */
  meta?: LinkMeta;
  /** For 'failed' entries: epoch ms after which the fetch may be retried. */
  retryAt?: number;
}

/** How long to wait before retrying an unfurl that failed transiently. */
export const URL_PREVIEW_RETRY_MS = 60_000;

/**
 * URL → metadata cache, shared across editor instances for the session so
 * revisiting a note (or the same link on another page) doesn't refetch.
 */
const cache = new Map<string, CacheEntry>();

/**
 * Every live EditorView running this plugin. A fetch is shared through the
 * module-level cache, so when it settles every view that renders that URL —
 * not just the one that started it, which may since have been destroyed —
 * must rebuild its cards.
 */
const liveViews = new Set<EditorView>();

/** Ask every live view whose document links `href` to rebuild its cards. */
function refreshViewsFor(href: string): void {
  for (const view of liveViews) {
    if (view.isDestroyed) {
      liveViews.delete(view);
      continue;
    }
    if (!collect(view.state.doc).some((link) => link.href === href)) continue;
    view.dispatch(
      view.state.tr.setMeta(urlPreviewPluginKey, { refresh: true } satisfies PluginMeta)
    );
  }
}

// ─── Fetch queue ─────────────────────────────────────────────────────────────
//
// The unfurl endpoint is rate-limited per user (30 req/min), and that budget is
// shared with the task-link unfurls a user triggers explicitly. Previews are
// display-only chrome, so they must never be able to starve that: every preview
// fetch, from every editor, goes through this one queue, which caps how many
// run at once and how many start per minute, and pauses entirely after a
// failure instead of letting each failed URL retry on its own.

/** Most preview unfurls in flight at once, across all editors. */
export const URL_PREVIEW_MAX_IN_FLIGHT = 2;
/** Most preview unfurls started per window — well under the server's 30/min. */
export const URL_PREVIEW_BUDGET = 10;
/** The window URL_PREVIEW_BUDGET applies to. */
export const URL_PREVIEW_BUDGET_WINDOW_MS = 60_000;
/** Ceiling for the queue's growing pause after repeated failures. */
export const URL_PREVIEW_MAX_PAUSE_MS = 10 * 60_000;

interface QueueItem {
  href: string;
  fetchMeta: (url: string) => Promise<LinkMeta | null>;
}

const queue: QueueItem[] = [];
let inFlight = 0;
/** Start times of recent fetches, oldest first, for the per-window budget. */
const recentStarts: number[] = [];
/** The whole queue is paused until this epoch ms (after a failure). */
let pausedUntil = 0;
/** Failures since the last success; the pause doubles with each one. */
let consecutiveFailures = 0;
let drainTimer: ReturnType<typeof setTimeout> | null = null;
/** Pending wake-ups that refresh views once a failed link may be retried. */
const retryTimers = new Set<ReturnType<typeof setTimeout>>();

/**
 * A failed link is only re-enqueued by a view update (maybeFetch), and nothing
 * else wakes an idle editor — so when the link's backoff ends, refresh the
 * views showing it. Their view update sees `retryAt` has passed and re-enqueues
 * it through the queue (which still honours any longer pause in effect).
 */
function scheduleRetry(href: string, retryAt: number): void {
  const timer = setTimeout(
    () => {
      retryTimers.delete(timer);
      refreshViewsFor(href);
    },
    Math.max(0, retryAt - Date.now())
  );
  retryTimers.add(timer);
}

/**
 * Metadata worth a card. The server answers 200 with only `url` and a hostname
 * `siteName` when it couldn't read the page (timeout, upstream error, non-HTML
 * like a PDF); rendering that would just repeat the link, so treat it as no
 * preview.
 */
function usableMeta(meta: LinkMeta | null): LinkMeta | undefined {
  return meta && (meta.title || meta.description || meta.image) ? meta : undefined;
}

function viewsLink(href: string): boolean {
  for (const view of liveViews) {
    if (!view.isDestroyed && collect(view.state.doc).some((link) => link.href === href)) {
      return true;
    }
  }
  return false;
}

function scheduleDrain(delay: number): void {
  if (drainTimer) clearTimeout(drainTimer);
  drainTimer = setTimeout(() => {
    drainTimer = null;
    drain();
  }, Math.max(0, delay));
}

/** Queue a URL for fetching. Its card shows the loading placeholder meanwhile. */
function enqueue(href: string, fetchMeta: QueueItem['fetchMeta']): void {
  cache.set(href, { status: 'loading' });
  queue.push({ href, fetchMeta });
  drain();
}

/** Start as many queued fetches as the concurrency cap, budget and pause allow. */
function drain(): void {
  while (queue.length > 0 && inFlight < URL_PREVIEW_MAX_IN_FLIGHT) {
    const now = Date.now();
    if (now < pausedUntil) {
      scheduleDrain(pausedUntil - now);
      return;
    }
    while (recentStarts.length && now - recentStarts[0] >= URL_PREVIEW_BUDGET_WINDOW_MS) {
      recentStarts.shift();
    }
    if (recentStarts.length >= URL_PREVIEW_BUDGET) {
      scheduleDrain(recentStarts[0] + URL_PREVIEW_BUDGET_WINDOW_MS - now);
      return;
    }

    const { href, fetchMeta } = queue.shift()!;
    // Every editor showing this link has closed while it waited: don't spend
    // budget on it. Forget it so it's queued afresh if it's shown again.
    if (!viewsLink(href)) {
      cache.delete(href);
      continue;
    }

    recentStarts.push(now);
    inFlight++;
    void fetchMeta(href)
      .then(
        (meta) => {
          consecutiveFailures = 0;
          cache.set(href, { status: 'ready', meta: usableMeta(meta) });
        },
        () => {
          // Transient failure (network, rate limit, 5xx): hide the card for
          // now and pause the whole queue with a growing backoff, so a burst
          // of failures can't turn into a burst of retries.
          consecutiveFailures++;
          const pause = Math.min(
            URL_PREVIEW_RETRY_MS * 2 ** (consecutiveFailures - 1),
            URL_PREVIEW_MAX_PAUSE_MS
          );
          pausedUntil = Math.max(pausedUntil, Date.now() + pause);
          cache.set(href, { status: 'failed', retryAt: pausedUntil });
          scheduleRetry(href, pausedUntil);
        }
      )
      .finally(() => {
        inFlight--;
        refreshViewsFor(href);
        drain();
      });
  }
}

/** Exposed for tests — reset the shared cache and fetch queue between cases. */
export function __clearUrlPreviewCache(): void {
  cache.clear();
  queue.length = 0;
  inFlight = 0;
  recentStarts.length = 0;
  pausedUntil = 0;
  consecutiveFailures = 0;
  if (drainTimer) clearTimeout(drainTimer);
  drainTimer = null;
  for (const timer of retryTimers) clearTimeout(timer);
  retryTimers.clear();
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
    // A ready entry with no metadata means the preview couldn't be built, and
    // a failed one means the fetch hasn't succeeded yet — render nothing
    // rather than a stuck placeholder.
    if (entry?.status === 'failed' || (entry?.status === 'ready' && !entry.meta)) continue;
    const status = entry?.status ?? 'loading';
    decorations.push(
      Decoration.widget(pos, () => renderCard(href), {
        side: 1,
        // The status is part of the key so a loading → ready transition
        // replaces the placeholder DOM instead of reusing it. Position is
        // deliberately excluded so edits above a card don't rebuild its DOM
        // (keys are only compared for equality, so they needn't be unique).
        key: `url-preview:${href}:${status}`,
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
              const existing = cache.get(href);
              if (existing && !(existing.status === 'failed' && Date.now() >= (existing.retryAt ?? 0))) {
                continue;
              }
              enqueue(href, fetchMeta);
            }
          };

          liveViews.add(view);
          maybeFetch();
          return {
            update: maybeFetch,
            destroy: () => {
              liveViews.delete(view);
            }
          };
        }
      })
    ];
  }
});
