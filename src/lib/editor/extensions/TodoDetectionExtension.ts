import { Extension } from '@tiptap/core';
import { Plugin, PluginKey } from '@tiptap/pm/state';
import type { Node as ProseMirrorNode } from '@tiptap/pm/model';
import { nanoid } from 'nanoid';
import type { TodoTriggerConfig } from '$lib/models/types';
import { DEFAULT_TODO_TRIGGER } from '$lib/models/types';
import { safeRegexTest } from '$lib/utils/safeRegex';

export interface DetectedBullet {
  nodeId: string;
  bulletText: string;
  pageId: string;
}

export interface TodoDetectionOptions {
  onTodoBulletsDetected: (bullets: DetectedBullet[]) => void;
  pageId: () => string;
  /** Returns the trigger config for TODO detection. Defaults to exact-match "TODO" on headings. */
  todoTrigger: () => TodoTriggerConfig | undefined;
}

export const todoDetectionPluginKey = new PluginKey<DetectedBullet[]>('todo-detection');

function matchesTrigger(text: string, config: TodoTriggerConfig): boolean {
  const { pattern, matchMode } = config;
  if (!pattern) return false;
  if (matchMode === 'regex') {
    return safeRegexTest(pattern, text);
  }
  return text.toLowerCase() === pattern.trim().toLowerCase();
}

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
      todoTrigger: () => undefined
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
          apply(tr, _value, _oldState, newState) {
            // Only recompute on doc changes; otherwise clear
            if (!tr.docChanged) return [];

            const { unlinkedBullets } = scanDocumentCached(newState.doc, options.todoTrigger());
            return unlinkedBullets;
          }
        },

        appendTransaction(transactions, _oldState, newState) {
          // Only act on transactions that changed the document
          if (!transactions.some((tr) => tr.docChanged)) return null;

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
  const cacheKey = triggerConfig ? `${triggerConfig.pattern}:${triggerConfig.matchMode}` : '__default__';
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

function scanDocument(doc: ProseMirrorNode, triggerConfig: TodoTriggerConfig | undefined): ScanResult {
  const trigger = triggerConfig ?? DEFAULT_TODO_TRIGGER;
  const blockTypes = trigger.blockTypes?.length ? trigger.blockTypes : ['heading'];
  const matchesAnyType = blockTypes.includes('any');

  const unlinkedBullets: DetectedBullet[] = [];
  const bulletListsInTodoSections: { node: ProseMirrorNode; offset: number }[] = [];
  let inTodoSection = false;

  doc.forEach((node, offset) => {
    const isMatchableType = matchesAnyType || blockTypes.includes(node.type.name);

    if (isMatchableType) {
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

function getListItemText(node: ProseMirrorNode): string {
  let text = '';
  node.forEach((child) => {
    if (child.type.name === 'paragraph') {
      text += child.textContent;
    }
  });
  return text.trim();
}
