import { Editor } from '@tiptap/core';
import StarterKit from '@tiptap/starter-kit';
import { afterEach, describe, expect, it } from 'vitest';
import { TaskLinkExtension } from './TaskLinkExtension';
import { TodoDetectionExtension } from './TodoDetectionExtension';
import type { TodoTriggerConfig } from '$lib/models/types';

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

function createEditor(
  trigger: TodoTriggerConfig | undefined,
  created: CreatedTodo[],
  pageId = 'page-test'
): Editor {
  const mount = document.createElement('div');
  document.body.appendChild(mount);

  const editor = new Editor({
    element: mount,
    extensions: [
      StarterKit.configure({ listItem: false }),
      TaskLinkExtension,
      TodoDetectionExtension.configure({
        pageId: () => pageId,
        todoTrigger: () => trigger,
        onTodoBulletsDetected: (bullets) => {
          created.push(...bullets);
        }
      })
    ],
    content: ''
  });

  createdEditors.push(editor);
  return editor;
}

afterEach(() => {
  for (const editor of createdEditors.splice(0, createdEditors.length)) {
    editor.destroy();
  }
  document.body.innerHTML = '';
});

describe('TodoDetectionExtension — matchesTrigger modes', () => {
  it('exact match is case-insensitive', async () => {
    const created: CreatedTodo[] = [];
    const editor = createEditor(
      { pattern: 'todo', matchMode: 'exact', blockTypes: ['heading'] },
      created
    );

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
              content: [{ type: 'paragraph', content: [{ type: 'text', text: 'Task A' }] }]
            }
          ]
        }
      ]
    });

    await waitFor(() => created.length > 0);
    expect(created[0].bulletText).toBe('Task A');
  });

  it('regex trigger matches custom pattern', async () => {
    const created: CreatedTodo[] = [];
    const editor = createEditor(
      { pattern: '^(TODO|TASKS?)$', matchMode: 'regex', blockTypes: ['heading'] },
      created
    );

    editor.commands.setContent({
      type: 'doc',
      content: [
        {
          type: 'heading',
          attrs: { level: 1 },
          content: [{ type: 'text', text: 'TASKS' }]
        },
        {
          type: 'bulletList',
          content: [
            {
              type: 'listItem',
              attrs: { nodeId: null, taskId: null },
              content: [{ type: 'paragraph', content: [{ type: 'text', text: 'Regex match' }] }]
            }
          ]
        }
      ]
    });

    await waitFor(() => created.length > 0);
    expect(created[0].bulletText).toBe('Regex match');
  });

  it('does not trigger when heading text does not match', async () => {
    const created: CreatedTodo[] = [];
    const editor = createEditor(
      { pattern: 'TODO', matchMode: 'exact', blockTypes: ['heading'] },
      created
    );

    editor.commands.setContent({
      type: 'doc',
      content: [
        {
          type: 'heading',
          attrs: { level: 2 },
          content: [{ type: 'text', text: 'NOTES' }]
        },
        {
          type: 'bulletList',
          content: [
            {
              type: 'listItem',
              attrs: { nodeId: null, taskId: null },
              content: [{ type: 'paragraph', content: [{ type: 'text', text: 'Not a task' }] }]
            }
          ]
        }
      ]
    });

    // Give it time — should NOT trigger
    await new Promise((resolve) => setTimeout(resolve, 200));
    expect(created).toHaveLength(0);
  });

  it('uses default trigger when todoTrigger returns undefined', async () => {
    const created: CreatedTodo[] = [];
    const editor = createEditor(undefined, created);

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
              content: [{ type: 'paragraph', content: [{ type: 'text', text: 'Default trigger' }] }]
            }
          ]
        }
      ]
    });

    await waitFor(() => created.length > 0);
    expect(created[0].bulletText).toBe('Default trigger');
  });

  it('skips items that already have a taskId', async () => {
    const created: CreatedTodo[] = [];
    const editor = createEditor(undefined, created);

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
              attrs: { nodeId: 'existing-node', taskId: 'existing-task' },
              content: [{ type: 'paragraph', content: [{ type: 'text', text: 'Already linked' }] }]
            }
          ]
        }
      ]
    });

    await new Promise((resolve) => setTimeout(resolve, 200));
    expect(created).toHaveLength(0);
  });

  it('detects multiple bullets under the same heading', async () => {
    const created: CreatedTodo[] = [];
    const editor = createEditor(undefined, created);

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
              content: [{ type: 'paragraph', content: [{ type: 'text', text: 'First task' }] }]
            },
            {
              type: 'listItem',
              attrs: { nodeId: null, taskId: null },
              content: [{ type: 'paragraph', content: [{ type: 'text', text: 'Second task' }] }]
            }
          ]
        }
      ]
    });

    await waitFor(() => created.length >= 2);
    expect(created.map((c) => c.bulletText)).toContain('First task');
    expect(created.map((c) => c.bulletText)).toContain('Second task');
  });

  it('invalid regex pattern does not crash and returns no match', async () => {
    const created: CreatedTodo[] = [];
    const editor = createEditor(
      { pattern: '[invalid', matchMode: 'regex', blockTypes: ['heading'] },
      created
    );

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
              content: [{ type: 'paragraph', content: [{ type: 'text', text: 'Wont match' }] }]
            }
          ]
        }
      ]
    });

    await new Promise((resolve) => setTimeout(resolve, 200));
    expect(created).toHaveLength(0);
  });

  it('empty pattern does not match anything', async () => {
    const created: CreatedTodo[] = [];
    const editor = createEditor(
      { pattern: '', matchMode: 'exact', blockTypes: ['heading'] },
      created
    );

    editor.commands.setContent({
      type: 'doc',
      content: [
        {
          type: 'heading',
          attrs: { level: 2 }
        },
        {
          type: 'bulletList',
          content: [
            {
              type: 'listItem',
              attrs: { nodeId: null, taskId: null },
              content: [{ type: 'paragraph', content: [{ type: 'text', text: 'No trigger' }] }]
            }
          ]
        }
      ]
    });

    await new Promise((resolve) => setTimeout(resolve, 200));
    expect(created).toHaveLength(0);
  });
});
