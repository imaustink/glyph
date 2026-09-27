/**
 * Unit tests for the mobile list indent / outdent helpers (issue #51).
 */
import { describe, it, expect, afterEach } from 'vitest';
import { Editor } from '@tiptap/core';
import { documentExtensions } from '$lib/editor/schema';
import { NodeIdMapExtension } from '$lib/editor/plugins/NodeIdMapPlugin';
import { selectionInListItem, indentListItem, outdentListItem } from '$lib/editor/listIndent';

function makeEditor(content: object): Editor {
  return new Editor({
    extensions: [...documentExtensions({ undoRedo: false }), NodeIdMapExtension],
    content
  });
}

/** A bullet list of top-level items, plus a trailing paragraph. */
const list = (texts: string[]) => ({
  type: 'doc',
  content: [
    {
      type: 'bulletList',
      content: texts.map((t) => ({
        type: 'listItem',
        content: [{ type: 'paragraph', content: [{ type: 'text', text: t }] }]
      }))
    },
    { type: 'paragraph' }
  ]
});

/** Move the cursor into the paragraph of the list item whose text is `text`. */
function cursorInItem(editor: Editor, text: string) {
  let pos = -1;
  editor.state.doc.descendants((node, p) => {
    if (pos !== -1) return false;
    if (node.type.name === 'listItem' && (node.firstChild?.textContent ?? '') === text) {
      pos = p + 2; // inside the paragraph
    }
  });
  if (pos === -1) throw new Error(`no list item "${text}"`);
  editor.commands.setTextSelection(pos);
}

/** Nesting depth of the list item whose text is `text` (number of bulletList ancestors). */
function depthOfItem(editor: Editor, text: string): number {
  let depth = -1;
  editor.state.doc.descendants((node, pos) => {
    if (depth !== -1) return false;
    if (node.type.name === 'listItem' && (node.firstChild?.textContent ?? '') === text) {
      const $pos = editor.state.doc.resolve(pos);
      let lists = 0;
      for (let d = $pos.depth; d > 0; d--) {
        if ($pos.node(d).type.name === 'bulletList') lists++;
      }
      depth = lists;
    }
  });
  if (depth === -1) throw new Error(`no list item "${text}"`);
  return depth;
}

let editors: Editor[] = [];
afterEach(() => {
  for (const e of editors) e.destroy();
  editors = [];
});

describe('selectionInListItem', () => {
  it('is true when the cursor is inside a list item', () => {
    const editor = makeEditor(list(['first', 'second']));
    editors.push(editor);
    cursorInItem(editor, 'second');
    expect(selectionInListItem(editor.state)).toBe(true);
  });

  it('is false when the cursor is in a plain paragraph', () => {
    const editor = makeEditor(list(['first']));
    editors.push(editor);
    // Select the trailing paragraph at the end of the doc
    editor.commands.setTextSelection(editor.state.doc.content.size - 1);
    expect(selectionInListItem(editor.state)).toBe(false);
  });
});

describe('indentListItem', () => {
  it('nests the second item under the first', () => {
    const editor = makeEditor(list(['first', 'second']));
    editors.push(editor);
    expect(depthOfItem(editor, 'second')).toBe(1);
    cursorInItem(editor, 'second');
    expect(indentListItem(editor)).toBe(true);
    expect(depthOfItem(editor, 'second')).toBe(2);
  });

  it('does nothing for the first item (nothing to nest under)', () => {
    const editor = makeEditor(list(['first', 'second']));
    editors.push(editor);
    cursorInItem(editor, 'first');
    expect(indentListItem(editor)).toBe(false);
    expect(depthOfItem(editor, 'first')).toBe(1);
  });
});

describe('outdentListItem', () => {
  it('un-nests a nested item back to the top level', () => {
    const editor = makeEditor(list(['first', 'second']));
    editors.push(editor);
    cursorInItem(editor, 'second');
    indentListItem(editor);
    expect(depthOfItem(editor, 'second')).toBe(2);

    cursorInItem(editor, 'second');
    expect(outdentListItem(editor)).toBe(true);
    expect(depthOfItem(editor, 'second')).toBe(1);
  });

  it('lifts a top-level item out of the list', () => {
    const editor = makeEditor(list(['only']));
    editors.push(editor);
    cursorInItem(editor, 'only');
    expect(outdentListItem(editor)).toBe(true);
    // The item is no longer inside any list.
    let stillInList = false;
    editor.state.doc.descendants((node) => {
      if (node.type.name === 'listItem') stillInList = true;
    });
    expect(stillInList).toBe(false);
  });
});
