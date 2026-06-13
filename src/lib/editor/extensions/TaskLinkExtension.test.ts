/**
 * Unit tests for TaskLinkExtension commands.
 *
 * Uses a real TipTap editor in jsdom (same pattern as TodoDetectionExtension.test.ts).
 * Tests: setTaskIdForNode, setBulletTextForNode, setCheckedForNode, setStatusForNode,
 *        and no-op behaviour when the nodeId does not exist.
 */

import { Editor, type Content } from '@tiptap/core';
import StarterKit from '@tiptap/starter-kit';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { TaskLinkExtension } from './TaskLinkExtension';
import { NodeIdMapExtension } from '$lib/editor/plugins/NodeIdMapPlugin';

const createdEditors: Editor[] = [];

afterEach(() => {
  for (const editor of createdEditors.splice(0)) {
    editor.destroy();
  }
  document.body.innerHTML = '';
});

/** Create a minimal editor with TaskLinkExtension and the NodeIdMap plugin. */
function createEditor(): Editor {
  const mount = document.createElement('div');
  document.body.appendChild(mount);

  const editor = new Editor({
    element: mount,
    extensions: [
      StarterKit.configure({ listItem: false }),
      TaskLinkExtension,
      NodeIdMapExtension
    ],
    content: ''
  });

  createdEditors.push(editor);
  return editor;
}

/**
 * Build a ProseMirror JSON doc with a single bulletList containing one listItem
 * that has a pre-set nodeId.
 */
function docWithListItem(nodeId: string, text = 'test bullet'): object {
  return {
    type: 'doc',
    content: [
      {
        type: 'bulletList',
        content: [
          {
            type: 'listItem',
            attrs: {
              nodeId,
              taskId: null,
              checked: false,
              taskStatus: 'todo'
            },
            content: [
              {
                type: 'paragraph',
                content: [{ type: 'text', text }]
              }
            ]
          }
        ]
      }
    ]
  };
}

/** Find the attrs of the first listItem in the editor document. */
function getFirstListItemAttrs(editor: Editor): Record<string, unknown> {
  let attrs: Record<string, unknown> = {};
  editor.state.doc.descendants((node) => {
    if (node.type.name === 'listItem') {
      attrs = node.attrs as Record<string, unknown>;
      return false;
    }
  });
  return attrs;
}

/** Get the text content of the first paragraph inside the first listItem. */
function getFirstBulletText(editor: Editor): string {
  let text = '';
  editor.state.doc.descendants((node) => {
    if (node.type.name === 'listItem') {
      node.descendants((child) => {
        if (child.type.name === 'text') {
          text = child.text ?? '';
          return false;
        }
      });
      return false;
    }
  });
  return text;
}

// ─── setTaskIdForNode ─────────────────────────────────────────────────────────

describe('setTaskIdForNode', () => {
  it('sets the taskId attribute on the matching listItem', () => {
    const editor = createEditor();
    editor.commands.setContent(docWithListItem('node-abc'));

    const result = editor.commands.setTaskIdForNode('node-abc', 'task-xyz');

    expect(result).toBe(true);
    expect(getFirstListItemAttrs(editor).taskId).toBe('task-xyz');
  });

  it('preserves other attributes when setting taskId', () => {
    const editor = createEditor();
    editor.commands.setContent(docWithListItem('n1'));

    editor.commands.setTaskIdForNode('n1', 'task-1');

    const attrs = getFirstListItemAttrs(editor);
    expect(attrs.nodeId).toBe('n1');
    expect(attrs.checked).toBe(false);
    expect(attrs.taskStatus).toBe('todo');
  });

  it('returns false for an unknown nodeId (no-op)', () => {
    const editor = createEditor();
    editor.commands.setContent(docWithListItem('real-node'));

    const result = editor.commands.setTaskIdForNode('nonexistent', 'task-x');

    expect(result).toBe(false);
    // The real node is unchanged
    expect(getFirstListItemAttrs(editor).taskId).toBeNull();
  });

  it('returns false when the document is empty', () => {
    const editor = createEditor();
    const result = editor.commands.setTaskIdForNode('any-node', 'task-x');
    expect(result).toBe(false);
  });
});

// ─── setBulletTextForNode ────────────────────────────────────────────────────

describe('setBulletTextForNode', () => {
  it('replaces the paragraph text inside the matching listItem', () => {
    const editor = createEditor();
    editor.commands.setContent(docWithListItem('n1', 'original text'));

    const result = editor.commands.setBulletTextForNode('n1', 'updated text');

    expect(result).toBe(true);
    expect(getFirstBulletText(editor)).toBe('updated text');
  });

  it('returns false for an unknown nodeId', () => {
    const editor = createEditor();
    editor.commands.setContent(docWithListItem('n1', 'text'));

    const result = editor.commands.setBulletTextForNode('nonexistent', 'new text');

    expect(result).toBe(false);
    expect(getFirstBulletText(editor)).toBe('text');
  });
});

// ─── setCheckedForNode ───────────────────────────────────────────────────────

describe('setCheckedForNode', () => {
  it('sets checked to true', () => {
    const editor = createEditor();
    editor.commands.setContent(docWithListItem('n1'));

    const result = editor.commands.setCheckedForNode('n1', true);

    expect(result).toBe(true);
    expect(getFirstListItemAttrs(editor).checked).toBe(true);
  });

  it('sets checked to false', () => {
    const editor = createEditor();
    // Start with checked=true
    editor.commands.setContent({
      type: 'doc',
      content: [{
        type: 'bulletList',
        content: [{
          type: 'listItem',
          attrs: { nodeId: 'n1', taskId: 'task-1', checked: true, taskStatus: 'done' },
          content: [{ type: 'paragraph', content: [{ type: 'text', text: 'done item' }] }]
        }]
      }]
    });

    editor.commands.setCheckedForNode('n1', false);

    expect(getFirstListItemAttrs(editor).checked).toBe(false);
  });

  it('is a no-op (returns false) when checked state would not change', () => {
    const editor = createEditor();
    editor.commands.setContent(docWithListItem('n1')); // checked defaults to false

    const result = editor.commands.setCheckedForNode('n1', false);

    expect(result).toBe(false);
  });

  it('returns false for an unknown nodeId', () => {
    const editor = createEditor();
    editor.commands.setContent(docWithListItem('n1'));

    expect(editor.commands.setCheckedForNode('missing', true)).toBe(false);
    expect(getFirstListItemAttrs(editor).checked).toBe(false);
  });

  it('sets checked via full-traversal fallback when NodeIdMap is absent (covers lines 267-268, 273-274)', () => {
    const mount = document.createElement('div');
    document.body.appendChild(mount);
    const editor = new Editor({
      element: mount,
      extensions: [
        StarterKit.configure({ listItem: false }),
        TaskLinkExtension
        // NodeIdMapExtension intentionally omitted → map miss → fallback traversal
      ],
      content: docWithListItem('fallback-check') as Content
    });
    createdEditors.push(editor);

    const result = editor.commands.setCheckedForNode('fallback-check', true);

    expect(result).toBe(true);
    expect(getFirstListItemAttrs(editor).checked).toBe(true);
  });
});

// ─── setStatusForNode ────────────────────────────────────────────────────────

describe('setStatusForNode', () => {
  it('sets the taskStatus attribute', () => {
    const editor = createEditor();
    editor.commands.setContent(docWithListItem('n1'));

    const result = editor.commands.setStatusForNode('n1', 'done');

    expect(result).toBe(true);
    expect(getFirstListItemAttrs(editor).taskStatus).toBe('done');
  });

  it('can cycle through all status values', () => {
    const editor = createEditor();
    const statuses = ['todo', 'in-progress', 'done', 'cancelled'];

    for (const status of statuses) {
      editor.commands.setContent(docWithListItem('n1'));
      editor.commands.setStatusForNode('n1', status);
      expect(getFirstListItemAttrs(editor).taskStatus).toBe(status);
    }
  });

  it('returns false for an unknown nodeId', () => {
    const editor = createEditor();
    editor.commands.setContent(docWithListItem('n1'));

    expect(editor.commands.setStatusForNode('missing', 'done')).toBe(false);
    expect(getFirstListItemAttrs(editor).taskStatus).toBe('todo');
  });

  it('sets taskStatus via full-traversal fallback when NodeIdMap is absent', () => {
    // Create an editor WITHOUT NodeIdMapExtension so there is no fast-path map.
    // This forces setStatusForNode to fall back to the doc.descendants traversal (lines 309-316).
    const mount = document.createElement('div');
    document.body.appendChild(mount);
    const editor = new Editor({
      element: mount,
      extensions: [
        StarterKit.configure({ listItem: false }),
        TaskLinkExtension
        // NodeIdMapExtension intentionally omitted → map miss → fallback traversal
      ],
      content: docWithListItem('fallback-node') as Content
    });
    createdEditors.push(editor);

    const result = editor.commands.setStatusForNode('fallback-node', 'done');

    expect(result).toBe(true);
    expect(getFirstListItemAttrs(editor).taskStatus).toBe('done');
  });

  it('returns false via fallback traversal when nodeId is not in document (no map)', () => {
    const mount = document.createElement('div');
    document.body.appendChild(mount);
    const editor = new Editor({
      element: mount,
      extensions: [
        StarterKit.configure({ listItem: false }),
        TaskLinkExtension
      ],
      content: docWithListItem('n1') as Content
    });
    createdEditors.push(editor);

    expect(editor.commands.setStatusForNode('nonexistent', 'done')).toBe(false);
  });
});

// ─── setTaskIdForNode fallback traversal ──────────────────────────────────────

describe('setTaskIdForNode fallback traversal (no NodeIdMap)', () => {
  it('sets taskId via full-traversal fallback when NodeIdMap is absent (covers lines 179-180)', () => {
    const mount = document.createElement('div');
    document.body.appendChild(mount);
    const editor = new Editor({
      element: mount,
      extensions: [
        StarterKit.configure({ listItem: false }),
        TaskLinkExtension
        // NodeIdMapExtension intentionally omitted → fallback traversal
      ],
      content: docWithListItem('fallback-task') as Content
    });
    createdEditors.push(editor);

    const result = editor.commands.setTaskIdForNode('fallback-task', 'task-99');

    expect(result).toBe(true);
    expect(getFirstListItemAttrs(editor).taskId).toBe('task-99');
  });
});

// ─── setBulletTextForNode fallback traversal ──────────────────────────────────

describe('setBulletTextForNode fallback traversal (no NodeIdMap)', () => {
  it('updates bullet text via full-traversal fallback when NodeIdMap is absent (covers lines 219-225)', () => {
    const mount = document.createElement('div');
    document.body.appendChild(mount);
    const editor = new Editor({
      element: mount,
      extensions: [
        StarterKit.configure({ listItem: false }),
        TaskLinkExtension
        // NodeIdMapExtension intentionally omitted → fallback traversal
      ],
      content: docWithListItem('fallback-text', 'original') as Content
    });
    createdEditors.push(editor);

    const result = editor.commands.setBulletTextForNode('fallback-text', 'updated via fallback');

    expect(result).toBe(true);
    expect(getFirstBulletText(editor)).toBe('updated via fallback');
  });
});

describe('setBulletTextForNode with nested bullet list (covers found/non-paragraph branch in forEach)', () => {
  function getParagraphTextForNode(editor: Editor, nodeId: string): string {
    let text = '';
    editor.state.doc.descendants((node) => {
      if (node.type.name === 'listItem' && node.attrs.nodeId === nodeId) {
        node.forEach((child) => {
          if (!text && child.type.name === 'paragraph') {
            text = child.textContent;
          }
        });
        return false;
      }
    });
    return text;
  }

  it('updates text in listItem that also has a nested bulletList (fast-path, covers line 204)', () => {
    const editor = createEditor();

    // A listItem with a paragraph followed by a nested bulletList
    editor.commands.setContent({
      type: 'doc',
      content: [
        {
          type: 'bulletList',
          content: [
            {
              type: 'listItem',
              attrs: { nodeId: 'with-nested', taskId: null, checked: false, taskStatus: 'todo' },
              content: [
                { type: 'paragraph', content: [{ type: 'text', text: 'parent text' }] },
                {
                  type: 'bulletList',
                  content: [
                    {
                      type: 'listItem',
                      attrs: { nodeId: 'child-node', taskId: null, checked: false, taskStatus: 'todo' },
                      content: [{ type: 'paragraph', content: [{ type: 'text', text: 'child' }] }]
                    }
                  ]
                }
              ]
            }
          ]
        }
      ]
    } as Content);

    const result = editor.commands.setBulletTextForNode('with-nested', 'updated parent');
    expect(result).toBe(true);
    // Only check the paragraph text of the outer listItem (not the nested child)
    expect(getParagraphTextForNode(editor, 'with-nested')).toBe('updated parent');
  });

  it('updates text via fallback path when listItem also has a nested bulletList (covers line 220)', () => {
    const mount = document.createElement('div');
    document.body.appendChild(mount);
    const editor = new Editor({
      element: mount,
      extensions: [
        StarterKit.configure({ listItem: false }),
        TaskLinkExtension
        // NodeIdMapExtension intentionally omitted → fallback traversal
      ],
      content: {
        type: 'doc',
        content: [
          {
            type: 'bulletList',
            content: [
              {
                type: 'listItem',
                attrs: { nodeId: 'nested-fallback', taskId: null, checked: false, taskStatus: 'todo' },
                content: [
                  { type: 'paragraph', content: [{ type: 'text', text: 'original' }] },
                  {
                    type: 'bulletList',
                    content: [
                      {
                        type: 'listItem',
                        attrs: { nodeId: 'child-fallback', taskId: null, checked: false, taskStatus: 'todo' },
                        content: [{ type: 'paragraph', content: [{ type: 'text', text: 'child' }] }]
                      }
                    ]
                  }
                ]
              }
            ]
          }
        ]
      } as Content
    });
    createdEditors.push(editor);

    const result = editor.commands.setBulletTextForNode('nested-fallback', 'updated via fallback nested');
    expect(result).toBe(true);
    expect(getParagraphTextForNode(editor, 'nested-fallback')).toBe('updated via fallback nested');
  });
});

describe('NodeView (addNodeView) DOM rendering', () => {
  it('renders listItem with taskId: sets data-task-id, data-checked, data-task-status on DOM element (covers lines 83-138)', () => {
    const mount = document.createElement('div');
    document.body.appendChild(mount);
    const editor = new Editor({
      element: mount,
      extensions: [StarterKit.configure({ listItem: false }), TaskLinkExtension, NodeIdMapExtension],
      content: {
        type: 'doc',
        content: [
          {
            type: 'bulletList',
            content: [
              {
                type: 'listItem',
                attrs: { nodeId: 'nv-1', taskId: 'task-nv1', checked: false, taskStatus: 'in-progress' },
                content: [{ type: 'paragraph', content: [{ type: 'text', text: 'NodeView test' }] }]
              }
            ]
          }
        ]
      } as Content
    });
    createdEditors.push(editor);

    // Verify the NodeView rendered the attributes into the DOM
    const li = mount.querySelector('li[data-node-id="nv-1"]');
    expect(li).not.toBeNull();
    expect(li?.getAttribute('data-task-id')).toBe('task-nv1');
    expect(li?.getAttribute('data-task-status')).toBe('in-progress');
  });

  it('renders listItem without taskId: removes task-related attributes (covers lines 133-138)', () => {
    const mount = document.createElement('div');
    document.body.appendChild(mount);
    const editor = new Editor({
      element: mount,
      extensions: [StarterKit.configure({ listItem: false }), TaskLinkExtension, NodeIdMapExtension],
      content: {
        type: 'doc',
        content: [
          {
            type: 'bulletList',
            content: [
              {
                type: 'listItem',
                attrs: { nodeId: 'nv-2', taskId: null, checked: false },
                content: [{ type: 'paragraph', content: [{ type: 'text', text: 'No task' }] }]
              }
            ]
          }
        ]
      } as Content
    });
    createdEditors.push(editor);

    const li = mount.querySelector('li[data-node-id="nv-2"]');
    expect(li).not.toBeNull();
    expect(li?.hasAttribute('data-task-id')).toBe(false);
  });

  it('NodeView update() method called when attrs change — sets checked attribute (covers lines 146-150)', () => {
    const mount = document.createElement('div');
    document.body.appendChild(mount);
    const editor = new Editor({
      element: mount,
      extensions: [StarterKit.configure({ listItem: false }), TaskLinkExtension, NodeIdMapExtension],
      content: {
        type: 'doc',
        content: [
          {
            type: 'bulletList',
            content: [
              {
                type: 'listItem',
                attrs: { nodeId: 'nv-update', taskId: 'task-u1', checked: false, taskStatus: 'todo' },
                content: [{ type: 'paragraph', content: [{ type: 'text', text: 'Update test' }] }]
              }
            ]
          }
        ]
      } as Content
    });
    createdEditors.push(editor);

    // Trigger an update to the listItem's attrs via setCheckedForNode
    editor.commands.setCheckedForNode('nv-update', true);

    const li = mount.querySelector('li[data-node-id="nv-update"]');
    expect(li?.getAttribute('data-checked')).toBe('true');
  });

  it('indicator click with no taskId does nothing (covers line 111 true branch)', () => {
    const mount = document.createElement('div');
    document.body.appendChild(mount);
    const onStatusCycled = vi.fn();
    const editor = new Editor({
      element: mount,
      extensions: [
        StarterKit.configure({ listItem: false }),
        TaskLinkExtension.configure({ onStatusCycled })
      ],
      content: {
        type: 'doc',
        content: [
          {
            type: 'bulletList',
            content: [
              {
                type: 'listItem',
                attrs: { nodeId: 'nv-click', taskId: null, checked: false },
                content: [{ type: 'paragraph', content: [{ type: 'text', text: 'Click test' }] }]
              }
            ]
          }
        ]
      } as Content
    });
    createdEditors.push(editor);

    // Click the indicator — should be a no-op because taskId is null
    const indicator = mount.querySelector('.task-status-indicator');
    indicator?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    expect(onStatusCycled).not.toHaveBeenCalled();
  });

  it('indicator click with taskId calls onStatusCycled (covers lines 112-113)', () => {
    const mount = document.createElement('div');
    document.body.appendChild(mount);
    const onStatusCycled = vi.fn();
    const editor = new Editor({
      element: mount,
      extensions: [
        StarterKit.configure({ listItem: false }),
        TaskLinkExtension.configure({ onStatusCycled })
      ],
      content: {
        type: 'doc',
        content: [
          {
            type: 'bulletList',
            content: [
              {
                type: 'listItem',
                attrs: { nodeId: 'nv-click2', taskId: 'task-click', checked: false, taskStatus: 'todo' },
                content: [{ type: 'paragraph', content: [{ type: 'text', text: 'Click test 2' }] }]
              }
            ]
          }
        ]
      } as Content
    });
    createdEditors.push(editor);

    const indicator = mount.querySelector('.task-status-indicator');
    indicator?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    expect(onStatusCycled).toHaveBeenCalledWith('nv-click2', 'task-click', 'todo');
  });
});

// ─── parseHTML attr functions (lines 51, 58, 65, 72) ─────────────────────────

describe('parseHTML attribute functions — HTML content parsing', () => {
  it('parses nodeId, taskId, checked, taskStatus from data-* HTML attributes (covers lines 51,58,65,72)', () => {
    const mount = document.createElement('div');
    document.body.appendChild(mount);
    const editor = new Editor({
      element: mount,
      extensions: [StarterKit.configure({ listItem: false }), TaskLinkExtension, NodeIdMapExtension],
      content: ''
    });
    createdEditors.push(editor);

    // Set HTML content so ProseMirror calls parseHTML functions to parse data-* attrs
    editor.commands.setContent(
      '<ul><li data-node-id="parsed-1" data-task-id="task-parsed" data-checked="true" data-task-status="done"><p>Parsed item</p></li></ul>'
    );

    const attrs = getFirstListItemAttrs(editor);
    expect(attrs.nodeId).toBe('parsed-1');
    expect(attrs.taskId).toBe('task-parsed');
    expect(attrs.checked).toBe(true);
    expect(attrs.taskStatus).toBe('done');
  });
});

// ─── NodeView syncAttrs with null nodeId (line 123) ──────────────────────────

describe('NodeView syncAttrs: null nodeId removes data-node-id attribute (line 123)', () => {
  it('removes data-node-id when nodeId attr is set to null via setNodeMarkup', () => {
    const mount = document.createElement('div');
    document.body.appendChild(mount);
    const editor = new Editor({
      element: mount,
      extensions: [StarterKit.configure({ listItem: false }), TaskLinkExtension, NodeIdMapExtension],
      content: {
        type: 'doc',
        content: [
          {
            type: 'bulletList',
            content: [
              {
                type: 'listItem',
                attrs: { nodeId: 'nv-null', taskId: null, checked: false },
                content: [{ type: 'paragraph', content: [{ type: 'text', text: 'Null nodeId' }] }]
              }
            ]
          }
        ]
      } as Content
    });
    createdEditors.push(editor);

    // Verify initial state
    expect(mount.querySelector('li[data-node-id="nv-null"]')).not.toBeNull();

    // Dispatch a transaction that sets nodeId to null — triggers syncAttrs with n.attrs.nodeId = null
    let listItemPos = -1;
    editor.state.doc.descendants((node, pos) => {
      if (node.type.name === 'listItem' && node.attrs.nodeId === 'nv-null') {
        listItemPos = pos;
        return false;
      }
      return true;
    });
    const tr = editor.state.tr.setNodeMarkup(listItemPos, undefined, {
      nodeId: null,
      taskId: null,
      checked: false,
      taskStatus: 'todo'
    });
    editor.view.dispatch(tr);

    // data-node-id should be removed from the DOM element
    const li = mount.querySelector('li');
    expect(li?.hasAttribute('data-node-id')).toBe(false);
  });
});

// ─── can() dry-run mode (dispatch=undefined branches, lines 254 and 296) ──────

describe('can() dry-run: setCheckedForNode and setStatusForNode with dispatch=undefined', () => {
  it('can().setCheckedForNode returns true when node exists and state would change (covers line 254 false branch)', () => {
    const editor = createEditor();
    editor.commands.setContent(docWithListItem('can-check'));

    // editor.can() calls command with dispatch=undefined (dry-run)
    // This covers the `if (dispatch) dispatch(tr)` FALSE branch when dispatch is undefined
    const result = editor.can().setCheckedForNode('can-check', true);
    expect(result).toBe(true);
  });

  it('can().setStatusForNode returns true when node exists and status would change (covers line 296 false branch)', () => {
    const editor = createEditor();
    editor.commands.setContent(docWithListItem('can-status'));

    const result = editor.can().setStatusForNode('can-status', 'done');
    expect(result).toBe(true);
  });
});

// ─── parseHTML / renderHTML fallback branches (|| 'todo') ────────────────────

describe('taskStatus attribute parseHTML/renderHTML fallback branches', () => {
  it('parseHTML falls back to "todo" when data-task-status attribute is absent (covers || "todo" on line 73)', () => {
    const editor = createEditor();
    // Setting content as HTML triggers TipTap's parseHTML for each attribute.
    // The <li> element has no data-task-status attribute → getAttribute returns null → || 'todo'
    editor.commands.setContent('<ul><li>plain list item</li></ul>');

    // After parsing, the listItem node's taskStatus should default to 'todo'
    let taskStatus: string | undefined;
    editor.state.doc.descendants((node) => {
      if (node.type.name === 'listItem' && taskStatus === undefined) {
        taskStatus = node.attrs.taskStatus as string;
      }
    });
    expect(taskStatus).toBe('todo');
  });

  it('renderHTML uses "todo" fallback when taskStatus attr is empty (covers taskStatus || "todo" on line 76)', () => {
    const editor = createEditor();
    // Set a listItem with a taskId but an empty taskStatus string (falsy)
    editor.commands.setContent({
      type: 'doc',
      content: [{
        type: 'bulletList',
        content: [{
          type: 'listItem',
          attrs: { nodeId: 'rs-node', taskId: 'task-rs', taskStatus: '', checked: false },
          content: [{ type: 'paragraph', content: [{ type: 'text', text: 'fallback' }] }]
        }]
      }]
    });

    // getHTML() triggers renderHTML for each attribute. With taskId set but taskStatus='',
    // the renderHTML function evaluates '' || 'todo' → 'todo'.
    const html = editor.getHTML();
    expect(html).toContain('data-task-status="todo"');
  });
});
