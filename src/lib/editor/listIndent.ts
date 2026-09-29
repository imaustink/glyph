/**
 * List indent / outdent helpers for the page editor.
 *
 * On desktop these are reached with Tab / Shift-Tab (StarterKit's ListItem
 * keyboard shortcuts). Touch keyboards on iOS and Android have no Tab key, so
 * there was no way to nest a list on mobile at all (issue #51). Editor.svelte
 * surfaces these as on-screen buttons; keeping the logic here makes it testable
 * without a Svelte component harness.
 */
import type { Editor } from '@tiptap/core';
import type { EditorState } from '@tiptap/pm/state';

/** The list-item node name used by the document schema (TaskLinkExtension). */
const LIST_ITEM = 'listItem';

/** Whether the current selection sits anywhere inside a list item. */
export function selectionInListItem(state: EditorState): boolean {
  const { $from } = state.selection;
  for (let depth = $from.depth; depth > 0; depth--) {
    if ($from.node(depth).type.name === LIST_ITEM) return true;
  }
  return false;
}

/**
 * Indent (nest) the list item(s) in the current selection — the mobile
 * equivalent of pressing Tab. Returns false (a no-op) when the selection
 * isn't in a list, or the item is already at the outermost position it can
 * be nested from.
 */
export function indentListItem(editor: Editor): boolean {
  return editor.chain().focus().sinkListItem(LIST_ITEM).run();
}

/**
 * Outdent (un-nest) the list item(s) in the current selection — the mobile
 * equivalent of pressing Shift-Tab. Returns false (a no-op) when there is
 * nothing to outdent.
 */
export function outdentListItem(editor: Editor): boolean {
  return editor.chain().focus().liftListItem(LIST_ITEM).run();
}
