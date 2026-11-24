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
});
