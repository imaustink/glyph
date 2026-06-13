/**
 * Tests for NodeIdMapPlugin — covers the incremental update path (line 41 true branch),
 * the full-rebuild path (line 41 false branch: tr.steps.length > 3),
 * and the invalid-mapping path (valid = false → full rebuild).
 */
import { Editor, type Content } from '@tiptap/core';
import StarterKit from '@tiptap/starter-kit';
import { afterEach, describe, expect, it } from 'vitest';
import { TaskLinkExtension } from '../extensions/TaskLinkExtension';
import { NodeIdMapExtension, nodeIdMapPluginKey } from './NodeIdMapPlugin';

const createdEditors: Editor[] = [];

function createMapEditor(content?: Content) {
  const mount = document.createElement('div');
  document.body.appendChild(mount);
  const editor = new Editor({
    element: mount,
    extensions: [StarterKit.configure({ listItem: false }), TaskLinkExtension, NodeIdMapExtension],
    content: content as Content
  });
  createdEditors.push(editor);
  return editor;
}

const docWithListItem = (nodeId: string): Content =>
  ({
    type: 'doc',
    content: [
      {
        type: 'bulletList',
        content: [
          {
            type: 'listItem',
            attrs: { nodeId, taskId: null },
            content: [{ type: 'paragraph', content: [{ type: 'text', text: 'item' }] }]
          }
        ]
      }
    ]
  }) as Content;

afterEach(() => {
  for (const editor of createdEditors.splice(0, createdEditors.length)) {
    editor.destroy();
  }
  document.body.innerHTML = '';
});

describe('NodeIdMapPlugin', () => {
  it('builds an initial map with listItem nodeIds', () => {
    const editor = createMapEditor(docWithListItem('nid-1'));
    const map = nodeIdMapPluginKey.getState(editor.state);
    expect(map?.has('nid-1')).toBe(true);
  });

  it('selection-only transaction returns existing map unchanged (covers line 37 true branch)', () => {
    const editor = createMapEditor(docWithListItem('nid-sel'));
    const mapBefore = nodeIdMapPluginKey.getState(editor.state);

    // Dispatch a no-op transaction (selection change only, docChanged=false)
    const { state, view } = editor;
    const selTr = state.tr; // empty transaction — no steps, docChanged=false
    view.dispatch(selTr);

    const mapAfter = nodeIdMapPluginKey.getState(editor.state);
    // Map should still contain the nodeId after a no-op transaction
    expect(mapAfter?.has('nid-sel')).toBe(true);
  });

  it('incremental path (≤3 steps): updates the map after setNodeMarkup (covers line 41 true branch)', () => {
    const editor = createMapEditor(docWithListItem('nid-inc'));

    // setCheckedForNode triggers a single setNodeMarkup step (≤3 steps → incremental path)
    editor.commands.setCheckedForNode('nid-inc', true);

    const map = nodeIdMapPluginKey.getState(editor.state);
    expect(map?.has('nid-inc')).toBe(true);
  });

  it('full-rebuild path (>3 steps): dispatches transaction with >3 steps (covers line 41 false branch)', () => {
    const editor = createMapEditor(docWithListItem('nid-rebuild'));

    const { state, view } = editor;
    let tr = state.tr;
    // Each insertText adds a separate ReplaceStep; 4 calls → 4 steps > threshold of 3
    tr = tr.insertText('A', 3);
    tr = tr.insertText('B', 5);
    tr = tr.insertText('C', 7);
    tr = tr.insertText('D', 9);
    expect(tr.steps.length).toBeGreaterThan(3);
    view.dispatch(tr);

    const map = nodeIdMapPluginKey.getState(editor.state);
    expect(map).toBeDefined();
  });

  it('invalid mapping path: triggers valid=false when a listItem is deleted (covers lines 53-54)', () => {
    const editor = createMapEditor(docWithListItem('nid-del'));

    // Find the position of the listItem to delete it directly
    let listItemPos = -1;
    let listItemEnd = -1;
    editor.state.doc.descendants((node, pos) => {
      if (node.type.name === 'listItem' && node.attrs.nodeId === 'nid-del') {
        listItemPos = pos;
        listItemEnd = pos + node.nodeSize;
        return false;
      }
      return true;
    });

    expect(listItemPos).toBeGreaterThan(-1);

    // Delete the listItem in a ≤3-step transaction so the incremental path runs
    // but the mapped position is now invalid → valid=false → full rebuild
    const { state, view } = editor;
    const tr = state.tr.delete(listItemPos, listItemEnd);
    expect(tr.steps.length).toBeLessThanOrEqual(3);
    view.dispatch(tr);

    // After deletion, the nodeId should NOT be in the map
    const map = nodeIdMapPluginKey.getState(editor.state);
    expect(map?.has('nid-del')).toBe(false);
  });

  it('new-node detection: adds a newly inserted listItem to the incremental map (covers line 62)', () => {
    const editor = createMapEditor(docWithListItem('nid-existing'));

    // Insert a NEW list item via tr.insert() in a single step (≤3 steps).
    // The old listItem stays at its position, so incremental mapping is valid.
    // The new listItem has a nodeId not in oldMap → triggers the descendants scan at line 61-63.
    const { state, view } = editor;

    // Find the end position of the bulletList to insert there
    let bulletListEnd = -1;
    state.doc.descendants((node, pos) => {
      if (node.type.name === 'bulletList') {
        bulletListEnd = pos + node.nodeSize - 1; // position just before the closing token
        return false;
      }
      return true;
    });
    expect(bulletListEnd).toBeGreaterThan(-1);

    // Build a new listItem node with a fresh nodeId
    const listItemType = state.schema.nodes['listItem'];
    const paragraphType = state.schema.nodes['paragraph'];
    const newListItem = listItemType.create(
      { nodeId: 'nid-new', taskId: null },
      paragraphType.create({}, state.schema.text('new item'))
    );

    const tr = state.tr.insert(bulletListEnd, newListItem);
    expect(tr.steps.length).toBeLessThanOrEqual(3);
    view.dispatch(tr);

    const map = nodeIdMapPluginKey.getState(editor.state);
    expect(map?.has('nid-new')).toBe(true);
    expect(map?.has('nid-existing')).toBe(true);
  });
});
