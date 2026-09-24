/**
 * Server-side rules for shared documents: what makes one acceptable, the
 * repairs only the server may make, and how a page's stored JSON becomes the
 * first state of a shared document.
 *
 * Everything here operates on Y.Docs without a ProseMirror view. Conversions
 * deliberately avoid y-prosemirror's schema-aware builders
 * (yXmlFragmentToProseMirrorRootNode et al.), which *delete* any Yjs element
 * they fail to build — acceptable in a browser, destructive on the server.
 */
import * as Y from 'yjs';
import { nanoid } from 'nanoid';
import type { Schema } from '@tiptap/pm/model';
import { prosemirrorJSONToYDoc, yXmlFragmentToProsemirrorJSON } from '@tiptap/y-tiptap';
import { COLLAB_FRAGMENT } from '$lib/editor/schema';
import { isSafeUrl } from '$lib/collab/protocol';
import { addSharedTextNodes } from '$lib/collab/sharedText';

export type ProseMirrorJSON = { type: string; attrs?: Record<string, unknown>; content?: ProseMirrorJSON[]; text?: string; marks?: { type: string; attrs?: Record<string, unknown> }[] };

export interface Inspection {
	/** The document as ProseMirror JSON. */
	json: ProseMirrorJSON;
	/** Size of `json` serialised, in bytes (UTF-16 length; close enough for a limit). */
	bytes: number;
	/**
	 * Why the document is unacceptable, or null. Fatal problems are things a
	 * same-schema client cannot produce: an unknown node or mark type, an
	 * oversized document. An update that introduces one is refused.
	 */
	fatal: string | null;
	/**
	 * Content-model violations (e.g. a listItem without a paragraph). These
	 * can arise legitimately from concurrent structural edits, and every
	 * client renders them, so they are logged rather than refused.
	 */
	contentError: string | null;
}

export function toJSON(doc: Y.Doc): ProseMirrorJSON {
	const json = yXmlFragmentToProsemirrorJSON(doc.getXmlFragment(COLLAB_FRAGMENT)) as ProseMirrorJSON;
	// Top-level text (never written by a y-prosemirror client) serialises as a
	// nested array; flatten so the schema check reports it cleanly.
	json.content = (json.content ?? []).flat() as ProseMirrorJSON[];
	return json;
}

export function inspect(doc: Y.Doc, schema: Schema, maxBytes: number): Inspection {
	const json = toJSON(doc);
	const serialised = JSON.stringify(json);
	const bytes = serialised.length;
	if (bytes > maxBytes) {
		return { json, bytes, fatal: `document is ${bytes} bytes, over the ${maxBytes} byte limit`, contentError: null };
	}
	let node;
	try {
		node = schema.nodeFromJSON(json);
	} catch (err) {
		return { json, bytes, fatal: `not representable in the editor schema: ${(err as Error).message}`, contentError: null };
	}
	let contentError: string | null = null;
	try {
		node.check();
	} catch (err) {
		contentError = (err as Error).message;
	}
	return { json, bytes, fatal: null, contentError };
}

// ─── Repairs ──────────────────────────────────────────────────────────────────

export interface RepairReport {
	/** List items whose duplicate nodeId was replaced (and task link dropped). */
	dedupedNodeIds: number;
	/** Link marks removed because their href had an unsafe scheme. */
	strippedLinks: number;
}

const HASHED_MARK = /(.*)(--[a-zA-Z0-9+/=]{8})$/;
const markName = (attr: string) => HASHED_MARK.exec(attr)?.[1] ?? attr;

/**
 * Repairs that must be made by exactly one actor — the server — because two
 * clients making them concurrently would each choose differently:
 *
 *  - Duplicate nodeIds. A bullet's nodeId is its identity for task linking.
 *    Concurrent moves (two users cut/pasting the same bullet) can leave two
 *    copies. The first in document order keeps the id; later copies get a
 *    fresh one and lose their task link, so a task stays linked to one bullet.
 *
 *  - Unsafe link hrefs (javascript: and friends). Editors render them
 *    harmlessly, but they must not persist or reach other tools.
 *
 * Runs in one transaction with the given origin, and is a no-op when nothing
 * needs fixing.
 */
export function repair(doc: Y.Doc, origin: unknown): RepairReport {
	const report: RepairReport = { dedupedNodeIds: 0, strippedLinks: 0 };
	const seen = new Set<string>();
	const duplicates: Y.XmlElement[] = [];
	const unsafeLinks: { text: Y.XmlText; index: number; length: number; attr: string }[] = [];

	const walk = (parent: Y.XmlFragment | Y.XmlElement) => {
		for (const child of parent.toArray()) {
			if (child instanceof Y.XmlElement) {
				if (child.nodeName === 'listItem') {
					const id = child.getAttribute('nodeId') as unknown;
					if (typeof id === 'string' && id !== '') {
						if (seen.has(id)) duplicates.push(child);
						else seen.add(id);
					}
				}
				walk(child);
			} else if (child instanceof Y.XmlText) {
				let index = 0;
				for (const op of child.toDelta() as { insert: unknown; attributes?: Record<string, unknown> }[]) {
					const length = typeof op.insert === 'string' ? op.insert.length : 1;
					for (const [attr, value] of Object.entries(op.attributes ?? {})) {
						if (markName(attr) !== 'link') continue;
						const href = (value as { href?: unknown } | null)?.href;
						if (href !== undefined && href !== null && !isSafeUrl(href)) {
							unsafeLinks.push({ text: child, index, length, attr });
						}
					}
					index += length;
				}
			}
		}
	};
	walk(doc.getXmlFragment(COLLAB_FRAGMENT));

	if (duplicates.length === 0 && unsafeLinks.length === 0) return report;

	doc.transact(() => {
		for (const el of duplicates) {
			el.setAttribute('nodeId', nanoid());
			el.removeAttribute('taskId');
			el.removeAttribute('checked');
			el.removeAttribute('taskStatus');
			report.dedupedNodeIds++;
		}
		for (const { text, index, length, attr } of unsafeLinks) {
			text.format(index, length, { [attr]: null });
			report.strippedLinks++;
		}
	}, origin);
	return report;
}

// ─── Seeding ──────────────────────────────────────────────────────────────────

/** An empty page: one empty paragraph, so two first-time editors don't each create one. */
const EMPTY_DOC: ProseMirrorJSON = { type: 'doc', content: [{ type: 'paragraph' }] };

/**
 * Prepare stored page JSON for seeding: give every list item a nodeId (so no
 * client ever has to assign one to shared content — two clients assigning
 * concurrently would race) and make nodeIds unique.
 */
export function normaliseForSeed(json: ProseMirrorJSON | null | undefined): ProseMirrorJSON {
	if (!json || json.type !== 'doc' || !Array.isArray(json.content) || json.content.length === 0) {
		return structuredClone(EMPTY_DOC);
	}
	const out = structuredClone(json);
	const seen = new Set<string>();
	const visit = (node: ProseMirrorJSON) => {
		if (node.type === 'listItem') {
			const attrs = (node.attrs ??= {});
			const id = attrs.nodeId;
			if (typeof id !== 'string' || id === '' || seen.has(id)) {
				if (typeof id === 'string' && seen.has(id)) {
					delete attrs.taskId;
					delete attrs.checked;
					delete attrs.taskStatus;
				}
				attrs.nodeId = nanoid();
			}
			seen.add(attrs.nodeId as string);
		}
		node.content?.forEach(visit);
	};
	visit(out);
	// The editor (StarterKit's TrailingNode) keeps a paragraph at the end of
	// every document and adds one if missing. Left to the clients, each one
	// opening the page would add its own — concurrently, so they'd stack up.
	if (out.content![out.content!.length - 1]?.type !== 'paragraph') out.content!.push({ type: 'paragraph' });
	return out;
}

/**
 * The initial Yjs update for a page. Throws if the stored document can't be
 * represented in the schema — seeding it would otherwise drop content.
 */
export function seedUpdate(schema: Schema, json: ProseMirrorJSON | null | undefined): Uint8Array {
	const doc = prosemirrorJSONToYDoc(schema, normaliseForSeed(json), COLLAB_FRAGMENT);
	addSharedTextNodes(doc.getXmlFragment(COLLAB_FRAGMENT), schema);
	try {
		return Y.encodeStateAsUpdate(doc);
	} finally {
		doc.destroy();
	}
}

// ─── Server-side edits ────────────────────────────────────────────────────────

/**
 * Remove the list item with the given nodeId (and its list, if that leaves it
 * empty). Used when a task is deleted from the task page. Returns whether
 * anything was removed.
 */
export function removeListItem(doc: Y.Doc, nodeId: string, origin: unknown): boolean {
	let target: Y.XmlElement | null = null;
	const find = (parent: Y.XmlFragment | Y.XmlElement) => {
		for (const child of parent.toArray()) {
			if (target) return;
			if (child instanceof Y.XmlElement) {
				if (child.nodeName === 'listItem' && child.getAttribute('nodeId') === nodeId) {
					target = child;
					return;
				}
				find(child);
			}
		}
	};
	find(doc.getXmlFragment(COLLAB_FRAGMENT));
	const el = target as Y.XmlElement | null;
	if (!el) return false;

	doc.transact(() => {
		const list = el.parent as Y.XmlElement | Y.XmlFragment;
		list.delete(list.toArray().indexOf(el), 1);
		if (list instanceof Y.XmlElement && list.length === 0) {
			const container = list.parent as Y.XmlElement | Y.XmlFragment;
			container.delete(container.toArray().indexOf(list), 1);
		}
	}, origin);
	return true;
}
