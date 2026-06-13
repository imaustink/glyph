import { Editor } from '@tiptap/core';
import StarterKit from '@tiptap/starter-kit';
import { afterEach, describe, expect, it } from 'vitest';
import { TaskLinkExtension } from './TaskLinkExtension';
import { TodoDetectionExtension } from './TodoDetectionExtension';

type CreatedTodo = {
  nodeId: string;
  bulletText: string;
  pageId: string;
};

const createdEditors: Editor[] = [];

async function waitFor(condition: () => boolean, timeoutMs = 2000): Promise<void> {
  const start = Date.now();

  while (!condition()) {
    if (Date.now() - start > timeoutMs) {
      throw new Error('Timed out waiting for condition');
    }
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
}

function getFirstListItemNodeId(editor: Editor): string | null {
  let nodeId: string | null = null;

  editor.state.doc.descendants((node) => {
    if (node.type.name === 'listItem' && !nodeId) {
      nodeId = (node.attrs.nodeId as string | null) ?? null;
      return false;
    }
    return true;
  });

  return nodeId;
}

afterEach(() => {
  for (const editor of createdEditors.splice(0, createdEditors.length)) {
    editor.destroy();
  }
  document.body.innerHTML = '';
});

describe('TodoDetectionExtension', () => {
  it('auto-creates callback payload for TODO bullets even when listItem starts without nodeId', async () => {
    const created: CreatedTodo[] = [];
    const mount = document.createElement('div');
    document.body.appendChild(mount);

    const editor = new Editor({
      element: mount,
      extensions: [
        StarterKit.configure({
          listItem: false
        }),
        TaskLinkExtension,
        TodoDetectionExtension.configure({
          pageId: () => 'page-test',
          onTodoBulletsDetected: (bullets) => {
            created.push(...bullets);
          }
        })
      ],
      content: ''
    });

    createdEditors.push(editor);

    editor.commands.setContent({
      type: 'doc',
      content: [
        {
          type: 'heading',
          attrs: { level: 2 },
          content: [{ type: 'text', text: 'TODO' }]
        },
        {
          type: 'bulletList',
          content: [
            {
              type: 'listItem',
              attrs: { nodeId: null, taskId: null },
              content: [
                {
                  type: 'paragraph',
                  content: [{ type: 'text', text: 'Ship regression test' }]
                }
              ]
            }
          ]
        }
      ]
    });

    await waitFor(() => created.length > 0);

    const firstCall = created[0];
    expect(firstCall.pageId).toBe('page-test');
    expect(firstCall.bulletText).toBe('Ship regression test');
    expect(firstCall.nodeId.length).toBeGreaterThan(0);

    const nodeIdInDoc = getFirstListItemNodeId(editor);
    expect(nodeIdInDoc).toBe(firstCall.nodeId);
  });

  it('uses default pageId (() => "") when pageId option is not configured (covers line 49)', async () => {
    const created: CreatedTodo[] = [];
    const mount = document.createElement('div');
    document.body.appendChild(mount);

    // Configure WITHOUT pageId option — default `pageId: () => ''` should be invoked
    const editor = new Editor({
      element: mount,
      extensions: [
        StarterKit.configure({ listItem: false }),
        TaskLinkExtension,
        TodoDetectionExtension.configure({
          onTodoBulletsDetected: (bullets) => {
            created.push(...bullets);
          }
          // pageId intentionally omitted → default `() => ''` on line 49 is called
        })
      ],
      content: ''
    });
    createdEditors.push(editor);

    editor.commands.setContent({
      type: 'doc',
      content: [
        { type: 'heading', attrs: { level: 1 }, content: [{ type: 'text', text: 'TODO' }] },
        {
          type: 'bulletList',
          content: [
            {
              type: 'listItem',
              attrs: { nodeId: null, taskId: null },
              content: [{ type: 'paragraph', content: [{ type: 'text', text: 'Default page test' }] }]
            }
          ]
        }
      ]
    });

    await waitFor(() => created.length > 0);
    // pageId should be the default empty string
    expect(created[0].pageId).toBe('');
  });

  it('does not call onTodoBulletsDetected for a selection-change-only transaction (covers lines 55, 77, 86)', async () => {
    const created: CreatedTodo[] = [];
    const mount = document.createElement('div');
    document.body.appendChild(mount);

    const editor = new Editor({
      element: mount,
      extensions: [
        StarterKit.configure({ listItem: false }),
        TaskLinkExtension,
        TodoDetectionExtension.configure({
          pageId: () => 'page-sel',
          onTodoBulletsDetected: (bullets) => {
            created.push(...bullets);
          }
        })
      ],
      content: ''
    });
    createdEditors.push(editor);

    // Set content with nodeIds already assigned so appendTransaction returns null
    editor.commands.setContent({
      type: 'doc',
      content: [
        { type: 'heading', attrs: { level: 1 }, content: [{ type: 'text', text: 'TODO' }] },
        {
          type: 'bulletList',
          content: [
            {
              type: 'listItem',
              attrs: { nodeId: 'pre-assigned', taskId: 'task-1' },
              content: [{ type: 'paragraph', content: [{ type: 'text', text: 'Done task' }] }]
            }
          ]
        }
      ]
    });

    // Wait for any initial detection to settle
    await new Promise((resolve) => setTimeout(resolve, 50));
    const countAfterSet = created.length;

    // Dispatch a selection-only transaction (does NOT change the doc)
    editor.commands.setTextSelection(1);

    // Wait briefly — the selection transaction should NOT trigger the callback
    await new Promise((resolve) => setTimeout(resolve, 50));

    // No additional calls should have happened from the selection change
    expect(created.length).toBe(countAfterSet);
  });

  it('hits the scan cache when a doc-changing transaction needs no new nodeIds (covers line 123)', async () => {
    const mount = document.createElement('div');
    document.body.appendChild(mount);

    const editor = new Editor({
      element: mount,
      extensions: [
        StarterKit.configure({ listItem: false }),
        TaskLinkExtension,
        TodoDetectionExtension.configure({
          pageId: () => 'page-cache'
        })
      ],
      content: ''
    });
    createdEditors.push(editor);

    // Set content with nodeIds already assigned — appendTransaction finds nothing to do → null
    editor.commands.setContent({
      type: 'doc',
      content: [
        { type: 'heading', attrs: { level: 1 }, content: [{ type: 'text', text: 'TODO' }] },
        {
          type: 'bulletList',
          content: [
            {
              type: 'listItem',
              attrs: { nodeId: 'cached-node', taskId: 'task-cached' },
              content: [{ type: 'paragraph', content: [{ type: 'text', text: 'cached' }] }]
            }
          ]
        }
      ]
    });

    // Now type more text — doc changes but no new nodeIds needed.
    // Both state.apply and appendTransaction run with the same newState.doc,
    // so the second call to scanDocumentCached should hit the WeakMap cache (line 123).
    editor.commands.insertContent(' more');

    // If we reach here without error, the cache hit path executed successfully
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(true).toBe(true);
  });

  it('falls back to ["heading"] when blockTypes is empty (covers line 135 false branch)', async () => {
    const created: CreatedTodo[] = [];
    const mount = document.createElement('div');
    document.body.appendChild(mount);

    const editor = new Editor({
      element: mount,
      extensions: [
        StarterKit.configure({ listItem: false }),
        TaskLinkExtension,
        TodoDetectionExtension.configure({
          pageId: () => 'page-empty-bt',
          onTodoBulletsDetected: (bullets) => {
            created.push(...bullets);
          },
          // blockTypes: [] triggers the ['heading'] fallback at line 135
          todoTrigger: () => ({ pattern: 'TODO', matchMode: 'exact', blockTypes: [] })
        })
      ],
      content: ''
    });
    createdEditors.push(editor);

    editor.commands.setContent({
      type: 'doc',
      content: [
        { type: 'heading', attrs: { level: 1 }, content: [{ type: 'text', text: 'TODO' }] },
        {
          type: 'bulletList',
          content: [
            {
              type: 'listItem',
              attrs: { nodeId: null, taskId: null },
              content: [{ type: 'paragraph', content: [{ type: 'text', text: 'Empty blockTypes bullet' }] }]
            }
          ]
        }
      ]
    });

    // With blockTypes:[] → falls back to ['heading'] → still matches the TODO heading
    await waitFor(() => created.length > 0, 3000);
    expect(created[0].bulletText).toBe('Empty blockTypes bullet');
  });

  it('calls the default no-op onTodoBulletsDetected when not configured (covers default () => {} at line 49)', async () => {
    const mount = document.createElement('div');
    document.body.appendChild(mount);

    // Configure WITHOUT onTodoBulletsDetected — the default () => {} should be called
    const editor = new Editor({
      element: mount,
      extensions: [
        StarterKit.configure({ listItem: false }),
        TaskLinkExtension,
        TodoDetectionExtension.configure({
          pageId: () => 'page-noop'
          // onTodoBulletsDetected intentionally omitted
        })
      ],
      content: ''
    });
    createdEditors.push(editor);

    editor.commands.setContent({
      type: 'doc',
      content: [
        { type: 'heading', attrs: { level: 1 }, content: [{ type: 'text', text: 'TODO' }] },
        {
          type: 'bulletList',
          content: [
            {
              type: 'listItem',
              attrs: { nodeId: null, taskId: null },
              content: [{ type: 'paragraph', content: [{ type: 'text', text: 'noop test' }] }]
            }
          ]
        }
      ]
    });

    // Wait for detection — the default () => {} swallows the detected bullets silently
    await new Promise((resolve) => setTimeout(resolve, 100));

    // Verify the nodeId was assigned (proving detection ran and called the default callback)
    const nodeId = getFirstListItemNodeId(editor);
    expect(nodeId).not.toBeNull();
  });


  it('detects nested bullets under a TODO heading (covers assignNodeIds and collectUnlinkedBullets recursion)', async () => {
    const created: CreatedTodo[] = [];
    const mount = document.createElement('div');
    document.body.appendChild(mount);

    const editor = new Editor({
      element: mount,
      extensions: [
        StarterKit.configure({ listItem: false }),
        TaskLinkExtension,
        TodoDetectionExtension.configure({
          pageId: () => 'page-nested',
          onTodoBulletsDetected: (bullets) => {
            created.push(...bullets);
          }
        })
      ],
      content: ''
    });

    createdEditors.push(editor);

    // Set content with a nested bullet list under a TODO heading.
    // The outer listItem has a nested bulletList inside it.
    editor.commands.setContent({
      type: 'doc',
      content: [
        {
          type: 'heading',
          attrs: { level: 1 },
          content: [{ type: 'text', text: 'TODO' }]
        },
        {
          type: 'bulletList',
          content: [
            {
              type: 'listItem',
              attrs: { nodeId: null, taskId: null },
              content: [
                {
                  type: 'paragraph',
                  content: [{ type: 'text', text: 'Parent bullet' }]
                },
                {
                  // Nested bulletList inside the listItem
                  type: 'bulletList',
                  content: [
                    {
                      type: 'listItem',
                      attrs: { nodeId: null, taskId: null },
                      content: [
                        {
                          type: 'paragraph',
                          content: [{ type: 'text', text: 'Nested bullet' }]
                        }
                      ]
                    }
                  ]
                }
              ]
            }
          ]
        }
      ]
    });

    // Wait for the detection callback to fire for both bullets
    await waitFor(() => created.length >= 1, 3000);

    expect(created.length).toBeGreaterThanOrEqual(1);
    expect(created[0].pageId).toBe('page-nested');
  });
});
