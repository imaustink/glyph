/**
 * Tests for UrlPreviewExtension — the opt-in URL preview cards in notes.
 *
 * Covers: which links get a card (a bare URL alone on its line) and which
 * don't (custom anchor text, non-http, inline URLs), the enabled/disabled
 * toggle, and that a completed fetch renders the card DOM from cached metadata.
 */
import { Editor, type Content } from '@tiptap/core';
import StarterKit from '@tiptap/starter-kit';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { LinkMeta } from '$lib/models/types';
import {
  UrlPreviewExtension,
  urlPreviewPluginKey,
  setUrlPreviewEnabled,
  URL_PREVIEW_RETRY_MS,
  URL_PREVIEW_MAX_IN_FLIGHT,
  URL_PREVIEW_BUDGET,
  URL_PREVIEW_BUDGET_WINDOW_MS,
  __clearUrlPreviewCache
} from './UrlPreviewExtension';

const createdEditors: Editor[] = [];

function createEditor(
  content: Content,
  opts: {
    enabled?: boolean;
    fetchMeta?: ((url: string) => Promise<LinkMeta | null>) | null;
  } = {}
) {
  const mount = document.createElement('div');
  document.body.appendChild(mount);
  const editor = new Editor({
    element: mount,
    extensions: [
      StarterKit,
      UrlPreviewExtension.configure({
        enabled: opts.enabled ?? true,
        fetchMeta: opts.fetchMeta ?? null
      })
    ],
    content
  });
  createdEditors.push(editor);
  return editor;
}

/** A paragraph whose only content is a bare link (text === href). */
function bareLink(href: string): Content {
  return {
    type: 'doc',
    content: [
      {
        type: 'paragraph',
        content: [{ type: 'text', text: href, marks: [{ type: 'link', attrs: { href } }] }]
      }
    ]
  } as Content;
}

/** Several paragraphs, each a bare link. */
function bareLinks(hrefs: string[]): Content {
  return {
    type: 'doc',
    content: hrefs.map((href) => ({
      type: 'paragraph',
      content: [{ type: 'text', text: href, marks: [{ type: 'link', attrs: { href } }] }]
    }))
  } as Content;
}

function metaFor(url: string): LinkMeta {
  return { url, title: `Title of ${url}`, description: null, image: null, favicon: null, siteName: null };
}

function decorationCount(editor: Editor): number {
  const set = urlPreviewPluginKey.getState(editor.state)?.decorations;
  if (!set) return 0;
  return set.find().length;
}

/** Let the async fetch settle and the resulting refresh transaction apply. */
async function flush() {
  await Promise.resolve();
  await new Promise((r) => setTimeout(r, 0));
  await Promise.resolve();
}

beforeEach(() => {
  __clearUrlPreviewCache();
});

afterEach(() => {
  for (const editor of createdEditors.splice(0, createdEditors.length)) {
    editor.destroy();
  }
  document.body.innerHTML = '';
});

describe('UrlPreviewExtension', () => {
  it('adds a decoration for a bare URL alone on its own line', () => {
    const editor = createEditor(bareLink('https://example.com'));
    expect(decorationCount(editor)).toBe(1);
  });

  it('adds no decoration when disabled', () => {
    const editor = createEditor(bareLink('https://example.com'), { enabled: false });
    expect(decorationCount(editor)).toBe(0);
  });

  it('ignores a link with custom anchor text', () => {
    const content = {
      type: 'doc',
      content: [
        {
          type: 'paragraph',
          content: [
            {
              type: 'text',
              text: 'click here',
              marks: [{ type: 'link', attrs: { href: 'https://example.com' } }]
            }
          ]
        }
      ]
    } as Content;
    const editor = createEditor(content);
    expect(decorationCount(editor)).toBe(0);
  });

  it('ignores a URL that shares its paragraph with other text', () => {
    const content = {
      type: 'doc',
      content: [
        {
          type: 'paragraph',
          content: [
            { type: 'text', text: 'see ' },
            {
              type: 'text',
              text: 'https://example.com',
              marks: [{ type: 'link', attrs: { href: 'https://example.com' } }]
            }
          ]
        }
      ]
    } as Content;
    const editor = createEditor(content);
    expect(decorationCount(editor)).toBe(0);
  });

  it('ignores non-http(s) links', () => {
    const editor = createEditor(bareLink('ftp://example.com'));
    expect(decorationCount(editor)).toBe(0);
  });

  it('toggles previews on and off via a transaction', () => {
    const editor = createEditor(bareLink('https://example.com'), { enabled: false });
    expect(decorationCount(editor)).toBe(0);

    editor.view.dispatch(setUrlPreviewEnabled(editor.state.tr, true));
    expect(decorationCount(editor)).toBe(1);

    editor.view.dispatch(setUrlPreviewEnabled(editor.state.tr, false));
    expect(decorationCount(editor)).toBe(0);
  });

  it('renders a card from fetched metadata', async () => {
    const meta: LinkMeta = {
      url: 'https://example.com',
      title: 'Example Domain',
      description: 'An example site',
      image: null,
      favicon: null,
      siteName: 'example.com'
    };
    const fetchMeta = vi.fn().mockResolvedValue(meta);
    const editor = createEditor(bareLink('https://example.com'), { fetchMeta });

    await flush();

    expect(fetchMeta).toHaveBeenCalledWith('https://example.com');
    const card = editor.view.dom.querySelector('.url-preview-card');
    expect(card).not.toBeNull();
    expect(card?.textContent).toContain('Example Domain');
  });

  it('drops the card when a fetch yields no metadata', async () => {
    const fetchMeta = vi.fn().mockResolvedValue(null);
    const editor = createEditor(bareLink('https://example.com'), { fetchMeta });

    await flush();

    expect(decorationCount(editor)).toBe(0);
  });

  it('does not fetch when no fetcher is provided', () => {
    const editor = createEditor(bareLink('https://example.com'), { fetchMeta: null });
    // Still shows a (loading) placeholder decoration, but nothing is fetched.
    expect(decorationCount(editor)).toBe(1);
  });

  it('retries a rejected fetch after the backoff', async () => {
    const meta: LinkMeta = {
      url: 'https://example.com',
      title: 'Example Domain',
      description: null,
      image: null,
      favicon: null,
      siteName: null
    };
    const fetchMeta = vi
      .fn()
      .mockRejectedValueOnce(new Error('network down'))
      .mockResolvedValue(meta);
    const now = vi.spyOn(Date, 'now').mockReturnValue(1_000_000);
    try {
      const editor = createEditor(bareLink('https://example.com'), { fetchMeta });
      await flush();
      expect(fetchMeta).toHaveBeenCalledTimes(1);
      expect(decorationCount(editor)).toBe(0);

      // A view update before the backoff elapses does not refetch.
      editor.view.dispatch(editor.state.tr.setMeta('noop', true));
      await flush();
      expect(fetchMeta).toHaveBeenCalledTimes(1);

      // After the backoff, the next view update retries and renders the card.
      now.mockReturnValue(1_000_000 + URL_PREVIEW_RETRY_MS);
      editor.view.dispatch(editor.state.tr.setMeta('noop', true));
      await flush();
      expect(fetchMeta).toHaveBeenCalledTimes(2);
      expect(editor.view.dom.querySelector('.url-preview-card')?.textContent).toContain(
        'Example Domain'
      );
    } finally {
      now.mockRestore();
    }
  });

  it('does not refetch a URL whose fetch resolved null', async () => {
    const fetchMeta = vi.fn().mockResolvedValue(null);
    const now = vi.spyOn(Date, 'now').mockReturnValue(1_000_000);
    try {
      const editor = createEditor(bareLink('https://example.com'), { fetchMeta });
      await flush();
      now.mockReturnValue(1_000_000 + URL_PREVIEW_RETRY_MS * 10);
      editor.view.dispatch(editor.state.tr.setMeta('noop', true));
      await flush();
      expect(fetchMeta).toHaveBeenCalledTimes(1);
    } finally {
      now.mockRestore();
    }
  });

  it('keeps the same card DOM when text is typed above it', async () => {
    const meta: LinkMeta = {
      url: 'https://example.com',
      title: 'Example Domain',
      description: null,
      image: null,
      favicon: null,
      siteName: null
    };
    const fetchMeta = vi.fn().mockResolvedValue(meta);
    const editor = createEditor(
      {
        type: 'doc',
        content: [
          { type: 'paragraph', content: [{ type: 'text', text: 'Intro' }] },
          ...((bareLink('https://example.com') as { content: unknown[] }).content as object[])
        ]
      } as Content,
      { fetchMeta }
    );
    await flush();
    const before = editor.view.dom.querySelector('.url-preview-card');
    expect(before).not.toBeNull();

    editor.view.dispatch(editor.state.tr.insertText('abc', 1));
    await flush();

    expect(editor.view.dom.querySelector('.url-preview-card')).toBe(before);
  });

  it('refreshes a new editor when a fetch started by a destroyed editor settles', async () => {
    const meta: LinkMeta = {
      url: 'https://example.com',
      title: 'Example Domain',
      description: null,
      image: null,
      favicon: null,
      siteName: null
    };
    let resolve!: (value: LinkMeta) => void;
    const fetchMeta = vi.fn(
      () => new Promise<LinkMeta | null>((r) => (resolve = r as (value: LinkMeta) => void))
    );

    // Editor A starts the fetch, then is torn down before it resolves.
    const a = createEditor(bareLink('https://example.com'), { fetchMeta });
    expect(fetchMeta).toHaveBeenCalledTimes(1);
    a.destroy();

    // Editor B opens the same doc while the fetch is still in flight.
    const b = createEditor(bareLink('https://example.com'), { fetchMeta });
    expect(fetchMeta).toHaveBeenCalledTimes(1);
    expect(b.view.dom.querySelector('.url-preview-card.is-loading')).not.toBeNull();

    resolve(meta);
    await flush();

    // B renders the ready card without any doc change.
    expect(b.view.dom.querySelector('.url-preview-card.is-loading')).toBeNull();
    expect(b.view.dom.querySelector('.url-preview-card')?.textContent).toContain('Example Domain');
  });

  it('renders no card for fallback metadata with no title, description or image', async () => {
    // What the server returns (200) when it couldn't read the page.
    const fetchMeta = vi.fn().mockResolvedValue({
      url: 'https://example.com/file.pdf',
      title: null,
      description: null,
      image: null,
      favicon: null,
      siteName: 'example.com'
    } satisfies LinkMeta);
    const editor = createEditor(bareLink('https://example.com/file.pdf'), { fetchMeta });

    await flush();

    expect(fetchMeta).toHaveBeenCalledTimes(1);
    expect(decorationCount(editor)).toBe(0);
    expect(editor.view.dom.querySelector('.url-preview-card')).toBeNull();
  });

  it('never has more than the cap of fetches in flight for a link-heavy note', async () => {
    const hrefs = Array.from({ length: 6 }, (_, i) => `https://example.com/${i}`);
    const pending: Array<() => void> = [];
    let inFlight = 0;
    let maxInFlight = 0;
    const fetchMeta = vi.fn(
      (url: string) =>
        new Promise<LinkMeta | null>((resolve) => {
          inFlight++;
          maxInFlight = Math.max(maxInFlight, inFlight);
          pending.push(() => {
            inFlight--;
            resolve(metaFor(url));
          });
        })
    );
    const editor = createEditor(bareLinks(hrefs), { fetchMeta });
    expect(fetchMeta).toHaveBeenCalledTimes(URL_PREVIEW_MAX_IN_FLIGHT);

    while (pending.length) {
      pending.shift()!();
      await flush();
    }

    expect(fetchMeta).toHaveBeenCalledTimes(hrefs.length);
    expect(maxInFlight).toBe(URL_PREVIEW_MAX_IN_FLIGHT);
    expect(editor.view.dom.querySelectorAll('.url-preview-card:not(.is-loading)')).toHaveLength(
      hrefs.length
    );
  });

  it('starts at most the budget of fetches per window', async () => {
    vi.useFakeTimers({ now: 1_000_000 });
    try {
      const hrefs = Array.from({ length: URL_PREVIEW_BUDGET + 3 }, (_, i) => `https://example.com/${i}`);
      const fetchMeta = vi.fn((url: string) => Promise.resolve(metaFor(url)));
      const editor = createEditor(bareLinks(hrefs), { fetchMeta });
      await vi.advanceTimersByTimeAsync(0);
      expect(fetchMeta).toHaveBeenCalledTimes(URL_PREVIEW_BUDGET);
      // The rest wait as loading placeholders.
      expect(editor.view.dom.querySelectorAll('.url-preview-card.is-loading')).toHaveLength(3);

      await vi.advanceTimersByTimeAsync(URL_PREVIEW_BUDGET_WINDOW_MS);
      expect(fetchMeta).toHaveBeenCalledTimes(hrefs.length);
      expect(editor.view.dom.querySelectorAll('.url-preview-card.is-loading')).toHaveLength(0);
    } finally {
      vi.useRealTimers();
    }
  });

  it('pauses the whole queue after a rejected fetch instead of retrying in a burst', async () => {
    vi.useFakeTimers({ now: 1_000_000 });
    try {
      const hrefs = Array.from({ length: 5 }, (_, i) => `https://example.com/${i}`);
      // The first two URLs hit a 429 once, then succeed.
      const rejectOnce = new Set([hrefs[0], hrefs[1]]);
      const fetchMeta = vi.fn((url: string) =>
        rejectOnce.delete(url) ? Promise.reject(new Error('429')) : Promise.resolve(metaFor(url))
      );
      const editor = createEditor(bareLinks(hrefs), { fetchMeta });
      await vi.advanceTimersByTimeAsync(0);

      // The first two (the cap) fail; nothing else starts while paused.
      expect(fetchMeta).toHaveBeenCalledTimes(URL_PREVIEW_MAX_IN_FLIGHT);
      editor.view.dispatch(editor.state.tr.setMeta('noop', true));
      await vi.advanceTimersByTimeAsync(URL_PREVIEW_RETRY_MS / 2);
      expect(fetchMeta).toHaveBeenCalledTimes(URL_PREVIEW_MAX_IN_FLIGHT);

      // Two consecutive failures double the pause; once it lapses the queue
      // drains the waiting URLs, and the failed ones are retried through the
      // same queue: each exactly once, not in a repeated burst.
      await vi.advanceTimersByTimeAsync(URL_PREVIEW_RETRY_MS * 2);
      const calls = fetchMeta.mock.calls.map(([u]) => u);
      for (const href of hrefs.slice(2)) {
        expect(calls.filter((u) => u === href)).toHaveLength(1);
      }
      expect(calls.filter((u) => u === hrefs[0])).toHaveLength(2);
      expect(calls.filter((u) => u === hrefs[1])).toHaveLength(2);
      await vi.advanceTimersByTimeAsync(0);
      expect(editor.view.dom.querySelectorAll('.url-preview-card:not(.is-loading)')).toHaveLength(
        hrefs.length
      );
    } finally {
      vi.useRealTimers();
    }
  });
});
