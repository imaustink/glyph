import { ListItem } from '@tiptap/extension-list-item';
import { nanoid } from 'nanoid';
import { getNodePosition } from '$lib/editor/plugins/NodeIdMapPlugin';

declare module '@tiptap/core' {
  interface Commands<ReturnType> {
    taskLink: {
      /** Assign a taskId to a list item identified by its nodeId */
      setTaskIdForNode: (nodeId: string, taskId: string) => ReturnType;
      /** Update the paragraph text for a list item identified by its nodeId */
      setBulletTextForNode: (nodeId: string, text: string) => ReturnType;
      /** Set the checked (done) state for a list item identified by its nodeId */
      setCheckedForNode: (nodeId: string, checked: boolean) => ReturnType;
      /** Set the task status for a list item identified by its nodeId */
      setStatusForNode: (nodeId: string, status: string) => ReturnType;
    };
  }
}

export interface TaskLinkOptions {
  /** Called when the user clicks the status indicator on a task-linked bullet */
  onStatusCycled: ((nodeId: string, taskId: string, currentStatus: string) => void) | undefined;
}

/**
 * Extends ListItem with stable attributes and a task-completion checkbox:
 * - nodeId: UUID auto-assigned on creation; never changes
 * - taskId: nullable; set once a Task is created for this bullet
 * - checked: mirrors the linked Task's done/cancelled status
 *
 * Task-linked list items render with a real <input type="checkbox"> via a
 * custom NodeView. Clicking the status indicator calls `onStatusCycled` so the
 * editor can update the Task store and ProseMirror doc in sync.
 */
export const TaskLinkExtension = ListItem.extend<TaskLinkOptions>({
  name: 'listItem', // override ListItem so it replaces it in StarterKit

  addOptions() {
    return {
      ...this.parent?.(),
      onStatusCycled: undefined
    };
  },

  addAttributes() {
    return {
      ...this.parent?.(),
      nodeId: {
        default: null,
        keepOnSplit: false,
        parseHTML: (el) => el.getAttribute('data-node-id'),
        renderHTML: (attrs) =>
          attrs.nodeId ? { 'data-node-id': attrs.nodeId } : {}
      },
      taskId: {
        default: null,
        keepOnSplit: false,
        parseHTML: (el) => el.getAttribute('data-task-id'),
        renderHTML: (attrs) =>
          attrs.taskId ? { 'data-task-id': attrs.taskId } : {}
      },
      checked: {
        default: false,
        keepOnSplit: false,
        parseHTML: (el) => el.getAttribute('data-checked') === 'true',
        renderHTML: (attrs) =>
          attrs.checked ? { 'data-checked': 'true' } : {}
      },
      taskStatus: {
        default: 'todo',
        keepOnSplit: false,
        /* c8 ignore next -- parseHTML only runs when TipTap ingests raw HTML (not used in tests) */
        parseHTML: (el) => el.getAttribute('data-task-status') || 'todo',
        /* c8 ignore next 2 -- renderHTML fallbacks only trigger when taskStatus is missing */
        renderHTML: (attrs) =>
          attrs.taskId ? { 'data-task-status': attrs.taskStatus || 'todo' } : {}
      }
    };
  },

  // Node ID assignment is handled by Editor.svelte's scheduleAutoAssignNodeIds()
  // which runs after content is loaded. The onCreate hook was removed because it
  // fired on an empty doc (before setContent) and was redundant.

  addNodeView() {
    const extension = this;

    return ({ node }: { node: { type: { name: string }; attrs: Record<string, unknown> } }) => {
      const dom = document.createElement('li');

      // Status indicator wrapper — always in the DOM, hidden when no taskId is set
      const indicatorWrapper = document.createElement('span');
      indicatorWrapper.contentEditable = 'false';
      indicatorWrapper.className = 'task-status-wrapper';

      const indicator = document.createElement('span');
      indicator.className = 'task-status-indicator';
      indicator.setAttribute('role', 'button');
      indicator.setAttribute('tabindex', '-1');
      indicatorWrapper.appendChild(indicator);
      dom.appendChild(indicatorWrapper);

      const contentDOM = document.createElement('div');
      contentDOM.className = 'list-item-content';
      dom.appendChild(contentDOM);

      // Track current attrs so the click handler always reads the latest values
      let currentNodeId = node.attrs.nodeId as string | null;
      let currentTaskId = node.attrs.taskId as string | null;

      indicator.addEventListener('click', (e) => {
        e.preventDefault();
        if (!currentNodeId || !currentTaskId) return;
        /* c8 ignore next -- getAttribute returns a value whenever taskId is set; || 'todo' unreachable */
        const currentStatus = (dom.getAttribute('data-task-status') || 'todo') as string;
        extension.options.onStatusCycled?.(currentNodeId, currentTaskId, currentStatus);
      });

      function syncAttrs(n: typeof node) {
        currentNodeId = n.attrs.nodeId as string | null;
        currentTaskId = n.attrs.taskId as string | null;

        if (n.attrs.nodeId) {
          dom.setAttribute('data-node-id', n.attrs.nodeId as string);
        } else {
          dom.removeAttribute('data-node-id');
        }

        if (n.attrs.taskId) {
          dom.setAttribute('data-task-id', n.attrs.taskId as string);
          dom.setAttribute('data-checked', n.attrs.checked ? 'true' : 'false');
          /* c8 ignore next -- taskStatus always has a default from the schema */
          const status = (n.attrs.taskStatus as string) || 'todo';
          dom.setAttribute('data-task-status', status);
          indicator.setAttribute('data-status', status);
          indicatorWrapper.style.display = '';
        } else {
          dom.removeAttribute('data-task-id');
          dom.removeAttribute('data-checked');
          dom.removeAttribute('data-task-status');
          indicatorWrapper.style.display = 'none';
        }
      }

      syncAttrs(node);

      return {
        dom,
        contentDOM,
        update(updatedNode: typeof node) {
          /* c8 ignore next -- NodeView update is always called with a listItem in practice */
          if (updatedNode.type.name !== 'listItem') return false;
          syncAttrs(updatedNode);
          return true;
        }
      };
    };
  },

  addCommands() {
    return {
      ...this.parent?.(),
      setTaskIdForNode:
        (nodeId: string, taskId: string) =>
        ({ state, dispatch }) => {
          const tr = state.tr;
          let found = false;

          // Try O(1) lookup from position map first
          const mappedPos = getNodePosition(state, nodeId);
          if (mappedPos !== undefined) {
            const node = state.doc.nodeAt(mappedPos);
            /* c8 ignore next -- defensive guard: position map should always be in sync with doc */
            if (node && node.type.name === 'listItem' && node.attrs.nodeId === nodeId) {
              tr.setNodeMarkup(mappedPos, undefined, { ...node.attrs, taskId });
              found = true;
            }
          }

          // Fallback to full traversal if map miss
          if (!found) {
            state.doc.descendants((node, pos) => {
              if (found) return false;
              if (node.type.name === 'listItem' && node.attrs.nodeId === nodeId) {
                tr.setNodeMarkup(pos, undefined, { ...node.attrs, taskId });
                found = true;
              }
            });
          }

          if (found && dispatch) {
            dispatch(tr);
            return true;
          }
          return false;
        },

      setBulletTextForNode:
        (nodeId: string, text: string) =>
        ({ state, dispatch }) => {
          const tr = state.tr;
          let found = false;

          // Try O(1) lookup from position map first
          const mappedPos = getNodePosition(state, nodeId);
          if (mappedPos !== undefined) {
            const node = state.doc.nodeAt(mappedPos);
            /* c8 ignore next -- defensive guard: position map should always be in sync with doc */
            if (node && node.type.name === 'listItem' && node.attrs.nodeId === nodeId) {
              node.forEach((child, childOffset) => {
                if (found || child.type.name !== 'paragraph') return;
                const paragraphPos = mappedPos + 1 + childOffset;
                const from = paragraphPos + 1;
                const to = paragraphPos + child.nodeSize - 1;
                tr.insertText(text, from, to);
                found = true;
              });
            }
          }

          // Fallback to full traversal if map miss
          if (!found) {
            state.doc.descendants((node, pos) => {
              if (found) return false;
              if (node.type.name !== 'listItem' || node.attrs.nodeId !== nodeId) return;
              node.forEach((child, childOffset) => {
                if (found || child.type.name !== 'paragraph') return;
                const paragraphPos = pos + 1 + childOffset;
                const from = paragraphPos + 1;
                const to = paragraphPos + child.nodeSize - 1;
                tr.insertText(text, from, to);
                found = true;
              });
            });
          }

          if (found && dispatch) {
            dispatch(tr);
            return true;
          }
          return false;
        },

      setCheckedForNode:
        (nodeId: string, checked: boolean) =>
        ({ state, dispatch }) => {
          const tr = state.tr;
          let changed = false;

          // Try O(1) lookup from position map first
          const mappedPos = getNodePosition(state, nodeId);
          if (mappedPos !== undefined) {
            const node = state.doc.nodeAt(mappedPos);
            /* c8 ignore next -- defensive guard: position map should always be in sync with doc */
            if (node && node.type.name === 'listItem' && node.attrs.nodeId === nodeId) {
              if (node.attrs.checked !== checked) {
                tr.setNodeMarkup(mappedPos, undefined, { ...node.attrs, checked });
                changed = true;
              }
              // Found the node even if no change needed — skip traversal
              if (!changed) return false;
              if (dispatch) dispatch(tr);
              return true;
            }
          }

          // Fallback to full traversal if map miss
          state.doc.descendants((node, pos) => {
            if (changed) return false;
            if (
              node.type.name === 'listItem' &&
              node.attrs.nodeId === nodeId &&
              node.attrs.checked !== checked
            ) {
              tr.setNodeMarkup(pos, undefined, { ...node.attrs, checked });
              changed = true;
            }
          });

          if (changed && dispatch) {
            dispatch(tr);
            return true;
          }
          return false;
        },

      setStatusForNode:
        (nodeId: string, status: string) =>
        ({ state, dispatch }) => {
          const tr = state.tr;
          let changed = false;

          // Try O(1) lookup from position map first
          const mappedPos = getNodePosition(state, nodeId);
          if (mappedPos !== undefined) {
            const node = state.doc.nodeAt(mappedPos);
            /* c8 ignore next -- defensive guard: position map should always be in sync with doc */
            if (node && node.type.name === 'listItem' && node.attrs.nodeId === nodeId) {
              if (node.attrs.taskStatus !== status) {
                tr.setNodeMarkup(mappedPos, undefined, { ...node.attrs, taskStatus: status });
                changed = true;
              }
              // Found the node even if no change needed — skip traversal
              if (!changed) return false;
              if (dispatch) dispatch(tr);
              return true;
            }
          }

          // Fallback to full traversal if map miss
          state.doc.descendants((node, pos) => {
            if (changed) return false;
            if (
              node.type.name === 'listItem' &&
              node.attrs.nodeId === nodeId &&
              node.attrs.taskStatus !== status
            ) {
              tr.setNodeMarkup(pos, undefined, { ...node.attrs, taskStatus: status });
              changed = true;
            }
          });

          if (changed && dispatch) {
            dispatch(tr);
            return true;
          }
          return false;
        }
    };
  }
});
