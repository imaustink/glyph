/**
 * ProseMirror plugin that maintains a Map<nodeId, pos> index.
 *
 * Instead of doing full doc.descendants() traversals to find a node by its
 * nodeId attribute, commands can look up the position in O(1) from this map.
 * The map is updated incrementally using transaction mappings.
 */
import { Extension } from '@tiptap/core';
import { Plugin, PluginKey } from '@tiptap/pm/state';
import type { EditorState } from '@tiptap/pm/state';

export const nodeIdMapPluginKey = new PluginKey<Map<string, number>>('node-id-map');

/**
 * Build the full nodeId → pos map from a document by traversing all descendants.
 * Called on init and after large changes that invalidate incremental mapping.
 */
function buildFullMap(state: EditorState): Map<string, number> {
  const map = new Map<string, number>();
  state.doc.descendants((node, pos) => {
    if (node.type.name === 'listItem' && node.attrs.nodeId) {
      map.set(node.attrs.nodeId as string, pos);
    }
  });
  return map;
}

const nodeIdMapPlugin = new Plugin({
  key: nodeIdMapPluginKey,

  state: {
    init(_, state) {
      return buildFullMap(state);
    },

    apply(tr, oldMap, _oldState, newState) {
      if (!tr.docChanged) return oldMap;

      // For simple transactions, try incremental update via mapping.
      // If the mapping produces invalid positions, fall back to full rebuild.
      if (tr.steps.length <= 3) {
        const newMap = new Map<string, number>();
        let valid = true;

        for (const [nodeId, oldPos] of oldMap) {
          const mappedPos = tr.mapping.map(oldPos);
          // Verify the mapped position still points to a listItem with this nodeId
          const node = newState.doc.nodeAt(mappedPos);
          if (node && node.type.name === 'listItem' && node.attrs.nodeId === nodeId) {
            newMap.set(nodeId, mappedPos);
          } else {
            // Mapping was incorrect — full rebuild needed
            valid = false;
            break;
          }
        }

        if (valid) {
          // Also check for newly created nodes (e.g. new listItems with nodeIds)
          newState.doc.descendants((node, pos) => {
            if (node.type.name === 'listItem' && node.attrs.nodeId && !newMap.has(node.attrs.nodeId as string)) {
              newMap.set(node.attrs.nodeId as string, pos);
            }
          });
          return newMap;
        }
      }

      // Fall back to full rebuild for complex transactions
      return buildFullMap(newState);
    }
  }
});

/**
 * TipTap extension wrapper for the NodeIdMap ProseMirror plugin.
 * Add this to the editor's extensions list.
 */
export const NodeIdMapExtension = Extension.create({
  name: 'nodeIdMap',

  addProseMirrorPlugins() {
    return [nodeIdMapPlugin];
  }
});

/**
 * Get the position of a listItem by its nodeId from the plugin state.
 * Returns undefined if not found.
 */
export function getNodePosition(state: EditorState, nodeId: string): number | undefined {
  const map = nodeIdMapPluginKey.getState(state);
  return map?.get(nodeId);
}
