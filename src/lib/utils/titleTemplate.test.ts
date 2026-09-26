import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { evaluateTitleTemplate, evaluateContentTemplate, TITLE_TOKENS } from './titleTemplate';

describe('titleTemplate', () => {
  const fixedDate = new Date('2026-04-18T14:30:00');

  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(fixedDate);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  describe('TITLE_TOKENS', () => {
    it('exports a non-empty array of token definitions', () => {
      expect(TITLE_TOKENS.length).toBeGreaterThan(0);
      for (const t of TITLE_TOKENS) {
        expect(t.token).toMatch(/^\{\{.+\}\}$/);
        expect(t.label).toBeTruthy();
        expect(t.description).toBeTruthy();
      }
    });
  });

  describe('evaluateTitleTemplate', () => {
    it('returns empty string for empty input', () => {
      expect(evaluateTitleTemplate('')).toBe('');
    });

    it('resolves {{date}} to ISO date', () => {
      expect(evaluateTitleTemplate('{{date}}')).toBe('2026-04-18');
    });

    it('resolves {{date-long}}', () => {
      expect(evaluateTitleTemplate('{{date-long}}')).toBe('April 18, 2026');
    });

    it('resolves {{date-short}}', () => {
      expect(evaluateTitleTemplate('{{date-short}}')).toBe('Apr 18, 2026');
    });

    it('resolves {{day}} to day name', () => {
      expect(evaluateTitleTemplate('{{day}}')).toBe('Saturday');
    });

    it('resolves {{year}}', () => {
      expect(evaluateTitleTemplate('{{year}}')).toBe('2026');
    });

    it('resolves {{week}} to ISO week number with W prefix', () => {
      // 2026-04-18 is ISO week 16
      expect(evaluateTitleTemplate('{{week}}')).toBe('W16');
    });

    it('resolves {{month}} to month name and year', () => {
      expect(evaluateTitleTemplate('{{month}}')).toBe('April 2026');
    });

    it('resolves {{time}}', () => {
      expect(evaluateTitleTemplate('{{time}}')).toBe('14:30');
    });

    it('resolves multiple tokens in one string', () => {
      const result = evaluateTitleTemplate('Notes for {{date}} at {{time}}');
      expect(result).toBe('Notes for 2026-04-18 at 14:30');
    });

    it('preserves unknown tokens unchanged', () => {
      expect(evaluateTitleTemplate('{{unknown}}')).toBe('{{unknown}}');
    });

    it('preserves plain text without tokens', () => {
      expect(evaluateTitleTemplate('Just plain text')).toBe('Just plain text');
    });
  });

  describe('evaluateContentTemplate', () => {
    it('returns empty string for empty input', () => {
      expect(evaluateContentTemplate('')).toBe('');
    });

    it('returns invalid JSON unchanged', () => {
      expect(evaluateContentTemplate('not json')).toBe('not json');
    });

    it('replaces tokens in text nodes', () => {
      const doc = {
        type: 'doc',
        content: [
          {
            type: 'heading',
            content: [{ type: 'text', text: '{{date-long}} Notes' }]
          }
        ]
      };
      const result = JSON.parse(evaluateContentTemplate(JSON.stringify(doc)));
      expect(result.content[0].content[0].text).toBe('April 18, 2026 Notes');
    });

    it('handles nested content recursively', () => {
      const doc = {
        type: 'doc',
        content: [
          {
            type: 'bulletList',
            content: [
              {
                type: 'listItem',
                content: [
                  {
                    type: 'paragraph',
                    content: [{ type: 'text', text: 'Due: {{date}}' }]
                  }
                ]
              }
            ]
          }
        ]
      };
      const result = JSON.parse(evaluateContentTemplate(JSON.stringify(doc)));
      const text = result.content[0].content[0].content[0].content[0].text;
      expect(text).toBe('Due: 2026-04-18');
    });

    it('preserves nodes without text or content', () => {
      const doc = {
        type: 'doc',
        content: [{ type: 'horizontalRule' }]
      };
      const result = JSON.parse(evaluateContentTemplate(JSON.stringify(doc)));
      expect(result.content[0].type).toBe('horizontalRule');
    });

    it('preserves unknown tokens in node.text (covers evaluateNode false branch at line 53)', () => {
      const doc = {
        type: 'doc',
        content: [
          {
            type: 'paragraph',
            content: [{ type: 'text', text: 'Hello {{unknown-token}} and {{date}}' }]
          }
        ]
      };
      const result = JSON.parse(evaluateContentTemplate(JSON.stringify(doc)));
      // {{unknown-token}} should be preserved as-is; {{date}} should be replaced
      expect(result.content[0].content[0].text).toMatch(/\{\{unknown-token\}\}/);
      expect(result.content[0].content[0].text).not.toMatch(/\{\{date\}\}/);
    });

    // A note created from a template must not share bullet identity with the
    // template (or with every other note made from it): a copied taskId links
    // the new note's bullet to someone else's task, and a copied nodeId makes
    // tasksStore.getByNodeId resolve to the wrong note's task.
    it('strips list-item identity attributes (nodeId, taskId, task state)', () => {
      const doc = {
        type: 'doc',
        content: [
          { type: 'heading', attrs: { level: 1 }, content: [{ type: 'text', text: 'TODO' }] },
          {
            type: 'bulletList',
            content: [
              {
                type: 'listItem',
                attrs: { nodeId: 'n-1', taskId: 't-1', checked: true, taskStatus: 'done' },
                content: [
                  { type: 'paragraph', content: [{ type: 'text', text: 'Review {{date}}' }] },
                  {
                    type: 'bulletList',
                    content: [
                      {
                        type: 'listItem',
                        attrs: { nodeId: 'n-2', taskId: null, checked: false, taskStatus: 'todo' },
                        content: [{ type: 'paragraph', content: [{ type: 'text', text: 'nested' }] }]
                      }
                    ]
                  }
                ]
              }
            ]
          }
        ]
      };
      const result = JSON.parse(evaluateContentTemplate(JSON.stringify(doc)));
      const outer = result.content[1].content[0];
      const inner = outer.content[1].content[0];
      for (const item of [outer, inner]) {
        expect(item.attrs?.nodeId ?? null).toBeNull();
        expect(item.attrs?.taskId ?? null).toBeNull();
        expect(item.attrs?.checked ?? false).toBe(false);
        expect(item.attrs?.taskStatus ?? 'todo').toBe('todo');
      }
      // Text is still evaluated.
      expect(outer.content[0].content[0].text).not.toMatch(/\{\{date\}\}/);
      // Other attributes survive.
      expect(result.content[0].attrs.level).toBe(1);
    });
  });
});
