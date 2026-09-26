import { Extension } from '@tiptap/core';
import { Plugin, PluginKey } from '@tiptap/pm/state';
import type { Node as ProseMirrorNode } from '@tiptap/pm/model';
import { nanoid } from 'nanoid';
import type { TodoTriggerConfig } from '$lib/models/types';
import type { EditorState } from '@tiptap/pm/state';
import { isTriggerBlock, matchesTrigger, resolveTrigger } from '$lib/editor/todoDerivation';
import { isLocalTransaction } from '$lib/collab/isLocalTransaction';
import { changedRanges, nodeTouches, type Range } from '$lib/editor/changedRanges';

export interface DetectedBullet {
  nodeId: string;
  bulletText: string;
  pageId: string;
  /**
   * The cursor is in this bullet's own text — the user is typing it. An empty
   * bullet only becomes a task in that case (see shouldCreateTaskFor).
   */
  hasCursor?: boolean;
}

export interface TodoDetectionOptions {
  onTodoBulletsDetected: (bullets: DetectedBullet[]) => void;
  pageId: () => string;
  /** Returns the trigger config for TODO detection. Defaults to exact-match "TODO" on headings. */
  todoTrigger: () => TodoTriggerConfig | undefined;
  /**
   * Collaborative mode. Only this user's own changes create tasks: a bullet
   * is offered for task creation if the user typed/pasted it (or its text),
   * or just typed the TODO heading above it. Remote edits never are —
   * otherwise every connected client would try to create the task for the
   * bullet one person typed. Nor are other people's unlinked bullets that
   * merely exist in the document when this user edits something else.
   */
  localChangesOnly?: boolean;
}

export const todoDetectionPluginKey = new PluginKey<DetectedBullet[]>('todo-detection');

/**
 * Traverses the document after each doc-changing transaction, finds bullet
 * list items under TODO headings, assigns nodeIds, and stores detected
 * unlinked bullets in plugin state.
 *
 * The notification callback fires from TipTap's `onTransaction` hook —
 * which runs AFTER all appendTransaction passes complete — rather than
 * from within appendTransaction via queueMicrotask. This gives deterministic
 * ordering: the document is fully settled before any async side effects begin.
 */
export const TodoDetectionExtension = Extension.create<TodoDetectionOptions>({
  name: 'todoDetection',

  addOptions() {
    return {
      /* c8 ignore next -- no-op default; always overridden in practice */
      onTodoBulletsDetected: () => {},
      pageId: () => '',
      todoTrigger: () => undefined,
      localChangesOnly: false
    };
  },

  onTransaction({ transaction }) {
    if (!transaction.docChanged) return;

    const detected = todoDetectionPluginKey.getState(this.editor.state);
    if (detected && detected.length > 0) {
      // Fill in pageId from the extension options (not available inside plugin state)
      const pageId = this.options.pageId();
      const withPageId = detected.map(b => ({ ...b, pageId }));
      this.options.onTodoBulletsDetected(withPageId);
    }
  },

  addProseMirrorPlugins() {
    const options = this.options;

    return [
      new Plugin({
        key: todoDetectionPluginKey,

        state: {
          init() { return [] as DetectedBullet[]; },
          apply(tr, _value, oldState, newState) {
            // Only recompute on doc changes; otherwise clear
            if (!tr.docChanged) return [];

            if (options.localChangesOnly) {
              if (!isLocalTransaction(tr)) return [];
              return markCursor(scanChangedBullets(newState.doc, oldState.doc, options.todoTrigger(), changedRanges(tr)), newState);
            }

            const { unlinkedBullets } = scanDocumentCached(newState.doc, options.todoTrigger());
            return markCursor(unlinkedBullets, newState);
          }
        },

        appendTransaction(transactions, _oldState, newState) {
          // Only act on transactions that changed the document
          if (!transactions.some((tr) => tr.docChanged)) return null;
          // Never assign identity in response to someone else's edit (see
          // CollabNodeIdExtension for why that would race).
          if (options.localChangesOnly && !transactions.some((tr) => tr.docChanged && isLocalTransaction(tr))) {
            return null;
          }

          const { bulletListsInTodoSections } = scanDocumentCached(newState.doc, options.todoTrigger());

          const tr = newState.tr;
          let changed = false;

          for (const { node, offset } of bulletListsInTodoSections) {
            assignNodeIds(node, offset, tr, (didChange) => { changed = changed || didChange; });
          }

          return changed ? tr : null;
        }
      })
    ];
  }
});

// ─── Document scanning (cached to avoid redundant traversals within the same transaction cycle) ──

interface ScanResult {
  unlinkedBullets: DetectedBullet[];
  bulletListsInTodoSections: { node: ProseMirrorNode; offset: number }[];
}

/**
 * WeakMap cache keyed by doc node identity. Within a single transaction cycle,
 * appendTransaction and plugin state apply both receive the same doc reference,
 * so the scan only runs once. The WeakMap allows GC when the doc is replaced.
 */
const scanCache = new WeakMap<ProseMirrorNode, Map<string, ScanResult>>();

function scanDocumentCached(doc: ProseMirrorNode, triggerConfig: TodoTriggerConfig | undefined): ScanResult {
  const cacheKey = JSON.stringify(resolveTrigger(triggerConfig));
  let docCache = scanCache.get(doc);
  if (docCache) {
    const cached = docCache.get(cacheKey);
    /* c8 ignore next -- cache hit requires state.apply and appendTransaction to share the same doc ref */
    if (cached) return cached;
  } else {
    docCache = new Map();
    scanCache.set(doc, docCache);
  }
  const result = scanDocument(doc, triggerConfig);
  docCache.set(cacheKey, result);
  return result;
}

/** Flag the bullet whose own text holds the cursor (the innermost list item around it). */
function markCursor(bullets: DetectedBullet[], state: EditorState): DetectedBullet[] {
  if (bullets.length === 0) return bullets;
  const { $from } = state.selection;
  let cursorNodeId: string | null = null;
  // Only a cursor in the list item's own paragraph counts, not one in a
  // nested list below it.
  if ($from.depth >= 2 && $from.parent.type.name === 'paragraph') {
    const li = $from.node($from.depth - 1);
    if (li.type.name === 'listItem') cursorNodeId = (li.attrs.nodeId as string | null) ?? null;
  }
  if (!cursorNodeId) return bullets;
  return bullets.map((b) => (b.nodeId === cursorNodeId ? { ...b, hasCursor: true } : b));
}

function scanDocument(doc: ProseMirrorNode, triggerConfig: TodoTriggerConfig | undefined): ScanResult {
  const trigger = resolveTrigger(triggerConfig);

  const unlinkedBullets: DetectedBullet[] = [];
  const bulletListsInTodoSections: { node: ProseMirrorNode; offset: number }[] = [];
  let inTodoSection = false;

  doc.forEach((node, offset) => {
    if (isTriggerBlock(node.type.name, trigger)) {
      inTodoSection = matchesTrigger(node.textContent.trim(), trigger);
      return;
    }

    if (node.type.name === 'bulletList' && inTodoSection) {
      bulletListsInTodoSections.push({ node, offset });
      collectUnlinkedBullets(node, unlinkedBullets);
    }
  });

  return { unlinkedBullets, bulletListsInTodoSections };
}

/** Recursively assign nodeIds to list items that lack them. */
function assignNodeIds(
  bulletList: ProseMirrorNode,
  bulletListPos: number,
  tr: ReturnType<typeof Object.create>,
  reportChange: (changed: boolean) => void
) {
  bulletList.forEach((listItem, liOffset) => {
    const nodeId = listItem.attrs.nodeId as string | null;
    const listItemPos = bulletListPos + 1 + liOffset;

    if (!nodeId) {
      tr.setNodeMarkup(listItemPos, undefined, { ...listItem.attrs, nodeId: nanoid() });
      reportChange(true);
    }

    // Recurse into nested bullet lists
    listItem.forEach((child, childOffset) => {
      if (child.type.name === 'bulletList') {
        assignNodeIds(child, listItemPos + 1 + childOffset, tr, reportChange);
      }
    });
  });
}

/** Collect unlinked bullets (have nodeId but no taskId) from a bullet list. */
function collectUnlinkedBullets(bulletList: ProseMirrorNode, out: DetectedBullet[]) {
  bulletList.forEach((listItem) => {
    const nodeId = listItem.attrs.nodeId as string | null;
    const taskId = listItem.attrs.taskId as string | null;

    if (nodeId && !taskId) {
      out.push({
        nodeId,
        bulletText: getListItemText(listItem),
        pageId: '' // filled in by onTransaction hook
      });
    }

    // Recurse into nested bullet lists
    listItem.forEach((child) => {
      if (child.type.name === 'bulletList') {
        collectUnlinkedBullets(child, out);
      }
    });
  });
}

/**
 * Unlinked bullets in TODO sections that one local transaction affected: the
 * bullet was inserted, its own text was edited, it was just given its
 * identity (nodeId) by this client, or the section's TODO heading itself was
 * edited.
 */
function scanChangedBullets(
  doc: ProseMirrorNode,
  oldDoc: ProseMirrorNode,
  triggerConfig: TodoTriggerConfig | undefined,
  ranges: Range[]
): DetectedBullet[] {
  // Identity assignment is an attribute-only step with no content range, so
  // "newly identified" is checked against the previous document instead.
  let oldIds: Set<string> | null = null;
  const isNewId = (id: string) => {
    if (!oldIds) {
      oldIds = new Set();
      oldDoc.descendants((n) => {
        if (n.type.name === 'listItem' && n.attrs.nodeId) oldIds!.add(n.attrs.nodeId as string);
      });
    }
    return !oldIds.has(id);
  };
  const trigger = resolveTrigger(triggerConfig);

  const out: DetectedBullet[] = [];
  let inTodoSection = false;
  let headingChanged = false;

  const visitList = (bulletList: ProseMirrorNode, listPos: number) => {
    bulletList.forEach((listItem, liOffset) => {
      const pos = listPos + 1 + liOffset;
      const nodeId = listItem.attrs.nodeId as string | null;
      const taskId = listItem.attrs.taskId as string | null;
      if (nodeId && !taskId) {
        let ownTextChanged = false;
        listItem.forEach((child, childOffset) => {
          if (child.type.name === 'paragraph' && nodeTouches(child, pos + 1 + childOffset, ranges)) ownTextChanged = true;
        });
        const inserted = ranges.some(([from, to]) => from <= pos && to >= pos + listItem.nodeSize);
        if (headingChanged || ownTextChanged || inserted || isNewId(nodeId)) {
          out.push({ nodeId, bulletText: getListItemText(listItem), pageId: '' });
        }
      }
      listItem.forEach((child, childOffset) => {
        if (child.type.name === 'bulletList') visitList(child, pos + 1 + childOffset);
      });
    });
  };

  doc.forEach((node, offset) => {
    if (isTriggerBlock(node.type.name, trigger)) {
      inTodoSection = matchesTrigger(node.textContent.trim(), trigger);
      headingChanged = inTodoSection && nodeTouches(node, offset, ranges);
      return;
    }
    if (node.type.name === 'bulletList' && inTodoSection) visitList(node, offset);
  });
  return out;
}

function getListItemText(node: ProseMirrorNode): string {
  let text = '';
  node.forEach((child) => {
    if (child.type.name === 'paragraph') {
      text += child.textContent;
    }
  });
  return text.trim();
}
