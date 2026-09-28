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
	/**
	 * Size of `json` serialised, in UTF-8 bytes — what the API counts. The
	 * UTF-16 length undercounts CJK and emoji text by up to 3×, which let a
	 * document pass here only for the API to refuse it and quarantine the page.
	 */
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
	const bytes = Buffer.byteLength(serialised, 'utf8');
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
			el.removeAttribute('checkbox');
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
					delete attrs.checkbox;
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

// ─── Legacy content ───────────────────────────────────────────────────────────

/** What a node's children must be, which decides what an unknown child becomes. */
type ChildKind = 'block' | 'inline' | 'text' | 'list';

function childKind(schema: Schema, parentType: string): ChildKind {
	const type = schema.nodes[parentType];
	if (!type) return 'block';
	if (type.spec.code) return 'text';
	if (type.isTextblock) return 'inline';
	const listItem = schema.nodes.listItem;
	if (listItem && type.contentMatch.matchType(listItem) && !type.contentMatch.matchType(schema.nodes.paragraph)) return 'list';
	return 'block';
}

/**
 * Bring stored JSON into the editor schema before seeding, the way the API's
 * NormalizeStoredContent does before serving it: marks the schema lacks are
 * dropped (their text kept), an image becomes text linking to its source, and
 * any other unknown node is replaced by its text and known children, shaped to
 * fit where it stood. Content saved before the API's allowlist matched the
 * schema can hold such nodes (DI-01); seeded as-is it throws, and the note
 * could never be opened collaboratively. Mutates `node`.
 */
function downgradeToSchema(schema: Schema, node: ProseMirrorJSON) {
	if (node.marks) {
		node.marks = node.marks.filter((m) => schema.marks[m.type] !== undefined);
		if (node.marks.length === 0) delete node.marks;
	}
	if (!node.content) return;
	const kind = childKind(schema, node.type);
	const out: ProseMirrorJSON[] = [];
	for (const child of node.content) {
		if (schema.nodes[child.type]) {
			downgradeToSchema(schema, child);
			out.push(child);
		} else {
			out.push(...downgradeNode(schema, child, kind));
		}
	}
	node.content = out;
}

function downgradeNode(schema: Schema, node: ProseMirrorJSON, kind: ChildKind): ProseMirrorJSON[] {
	switch (kind) {
		case 'inline':
			return inlineOf(schema, node);
		case 'text':
			return inlineOf(schema, node)
				.filter((n) => n.type === 'text')
				.map(({ marks: _marks, ...text }) => text);
		case 'list': {
			const blocks = blocksOf(schema, node);
			if (blocks.length === 0) return [];
			if (blocks[0].type !== 'paragraph') blocks.unshift({ type: 'paragraph' });
			return [{ type: 'listItem', content: blocks }];
		}
		default:
			return blocksOf(schema, node);
	}
}

/** An image as a text node linking to its source (when safe), labelled with its alt text, title or URL. */
function imageLinkText(node: ProseMirrorJSON): ProseMirrorJSON | null {
	const src = typeof node.attrs?.src === 'string' ? node.attrs.src.trim() : '';
	const safe = src !== '' && isSafeUrl(src);
	const label =
		[node.attrs?.alt, node.attrs?.title].find((s): s is string => typeof s === 'string' && s.trim() !== '') ??
		(safe ? src : '');
	if (label === '') return null;
	return safe ? { type: 'text', text: label, marks: [{ type: 'link', attrs: { href: src } }] } : { type: 'text', text: label };
}

/** An unknown node flattened to inline nodes the schema has. */
function inlineOf(schema: Schema, node: ProseMirrorJSON): ProseMirrorJSON[] {
	if (node.type === 'image') {
		const text = imageLinkText(node);
		return text ? [text] : [];
	}
	if (node.text) {
		const text: ProseMirrorJSON = { type: 'text', text: node.text, marks: node.marks };
		downgradeToSchema(schema, text);
		return [text];
	}
	const out: ProseMirrorJSON[] = [];
	for (const child of node.content ?? []) {
		if (child.type === 'text' || child.type === 'hardBreak') {
			if (child.type === 'text' && !child.text) continue;
			downgradeToSchema(schema, child);
			out.push(child);
		} else {
			// Known blocks contribute their text; unknown nodes recurse.
			out.push(...inlineOf(schema, child));
		}
	}
	return out;
}

/**
 * An unknown node as block nodes the schema has: known block children are
 * kept, runs of inline content are wrapped in paragraphs, and stray list
 * items are wrapped in a bullet list.
 */
function blocksOf(schema: Schema, node: ProseMirrorJSON): ProseMirrorJSON[] {
	if (node.type === 'image') {
		const text = imageLinkText(node);
		return text ? [{ type: 'paragraph', content: [text] }] : [];
	}
	if (node.text) return [{ type: 'paragraph', content: inlineOf(schema, node) }];

	const out: ProseMirrorJSON[] = [];
	let inline: ProseMirrorJSON[] = [];
	let items: ProseMirrorJSON[] = [];
	const flushInline = () => {
		if (inline.length > 0) out.push({ type: 'paragraph', content: inline });
		inline = [];
	};
	const flushItems = () => {
		if (items.length > 0) out.push({ type: 'bulletList', content: items });
		items = [];
	};
	for (const child of node.content ?? []) {
		if (child.type === 'text' || child.type === 'hardBreak') {
			flushItems();
			inline.push(...inlineOf(schema, { type: '', content: [child] }));
		} else if (child.type === 'listItem') {
			flushInline();
			downgradeToSchema(schema, child);
			items.push(child);
		} else if (schema.nodes[child.type]) {
			flushInline();
			flushItems();
			downgradeToSchema(schema, child);
			out.push(child);
		} else {
			flushInline();
			flushItems();
			out.push(...blocksOf(schema, child));
		}
	}
	flushInline();
	flushItems();
	return out;
}

/**
 * The initial Yjs update for a page. Stored content is first brought into the
 * editor schema (downgradeToSchema, mirroring the API); throws if it still
 * can't be represented — seeding it would otherwise drop content.
 */
export function seedUpdate(schema: Schema, json: ProseMirrorJSON | null | undefined): Uint8Array {
	const normalised = normaliseForSeed(json);
	downgradeToSchema(schema, normalised);
	const doc = prosemirrorJSONToYDoc(schema, normalised, COLLAB_FRAGMENT);
	addSharedTextNodes(doc.getXmlFragment(COLLAB_FRAGMENT), schema);
	try {
		return Y.encodeStateAsUpdate(doc);
	} finally {
		doc.destroy();
	}
}

// ─── Server-side edits ────────────────────────────────────────────────────────

/**
 * Show a task's status on its bullet: the `taskStatus` and `checked` attributes
 * the editor renders. Only linked bullets (with a taskId) are touched, and
 * nothing is written when the bullet already shows this status. Returns
 * whether anything changed.
 */
export function setListItemStatus(doc: Y.Doc, nodeId: string, status: string, origin: unknown): boolean {
	const checked = status === 'done' || status === 'cancelled';
	const targets: Y.XmlElement[] = [];
	const find = (parent: Y.XmlFragment | Y.XmlElement) => {
		for (const child of parent.toArray()) {
			if (!(child instanceof Y.XmlElement)) continue;
			if (child.nodeName === 'listItem' && child.getAttribute('nodeId') === nodeId && child.getAttribute('taskId')) {
				const attrs = child.getAttributes() as Record<string, unknown>;
				// Unset attributes mean the schema defaults (todo / unchecked).
				if ((attrs.taskStatus ?? 'todo') !== status || (attrs.checked ?? false) !== checked) targets.push(child);
			}
			find(child);
		}
	};
	find(doc.getXmlFragment(COLLAB_FRAGMENT));
	if (targets.length === 0) return false;
	doc.transact(() => {
		for (const el of targets) {
			el.setAttribute('taskStatus', status);
			el.setAttribute('checked', checked as unknown as string);
		}
	}, origin);
	return true;
}

/**
 * Show a task's title in its bullet (DI-29): the text of the linked list
 * item's first paragraph becomes `title` (trimmed), as the single-writer
 * editor does with setBulletTextForNode. Used for renames made outside the
 * editor, which only the server may write: every editor making the same text
 * edit would merge into duplicated text.
 *
 * Only linked bullets (with a taskId) are touched, and nothing is written
 * when the paragraph already shows the title (ignoring surrounding space) or
 * the title is blank. The paragraph becomes plain text of the title, except
 * that only the part that differs is replaced: unchanged text at the start
 * and end keeps its marks, and new text takes the marks of the character
 * before it, as typing there would. Anything else inline in that paragraph
 * (a hard break, further text runs) is replaced; later paragraphs and nested
 * lists are left alone. Returns whether anything changed.
 */
export function setListItemText(doc: Y.Doc, nodeId: string, title: string, origin: unknown): boolean {
	const text = title.trim();
	if (text === '') return false;
	const targets = titleParagraphs(doc, nodeId).filter((p) => inlineText(p).trim() !== text);
	if (targets.length === 0) return false;
	doc.transact(() => {
		for (const paragraph of targets) {
			const children = paragraph.toArray();
			let run = children.find((c): c is Y.XmlText => c instanceof Y.XmlText);
			for (let i = children.length - 1; i >= 0; i--) {
				if (children[i] !== run) paragraph.delete(i, 1);
			}
			if (!run) {
				run = new Y.XmlText();
				paragraph.insert(0, [run]);
			}
			replaceChangedText(run, plainText(run), text);
		}
	}, origin);
	return true;
}

/**
 * Whether the linked bullet with this nodeId shows `title` (as
 * setListItemText leaves it): false if there is no such bullet.
 */
export function listItemShowsText(doc: Y.Doc, nodeId: string, title: string): boolean {
	const text = title.trim();
	const paragraphs = titleParagraphs(doc, nodeId);
	return text !== '' && paragraphs.length > 0 && paragraphs.every((p) => inlineText(p).trim() === text);
}

/** The first paragraph of each linked list item (with a taskId) with this nodeId. */
function titleParagraphs(doc: Y.Doc, nodeId: string): Y.XmlElement[] {
	const out: Y.XmlElement[] = [];
	const find = (parent: Y.XmlFragment | Y.XmlElement) => {
		for (const child of parent.toArray()) {
			if (!(child instanceof Y.XmlElement)) continue;
			if (child.nodeName === 'listItem' && child.getAttribute('nodeId') === nodeId && child.getAttribute('taskId')) {
				const paragraph = child.toArray().find((c): c is Y.XmlElement => c instanceof Y.XmlElement && c.nodeName === 'paragraph');
				if (paragraph) out.push(paragraph);
			}
			find(child);
		}
	};
	find(doc.getXmlFragment(COLLAB_FRAGMENT));
	return out;
}

/** The plain text of a Y.XmlText (embeds excluded). */
function plainText(text: Y.XmlText): string {
	return (text.toDelta() as { insert: unknown }[]).map((op) => (typeof op.insert === 'string' ? op.insert : '')).join('');
}

/** The plain text of a text block's inline content (like ProseMirror's textContent). */
function inlineText(block: Y.XmlElement): string {
	return block
		.toArray()
		.map((c) => (c instanceof Y.XmlText ? plainText(c) : ''))
		.join('');
}

const isHighSurrogate = (s: string, i: number) => {
	const c = s.charCodeAt(i);
	return c >= 0xd800 && c <= 0xdbff;
};
const isLowSurrogate = (s: string, i: number) => {
	const c = s.charCodeAt(i);
	return c >= 0xdc00 && c <= 0xdfff;
};

/**
 * Turn `text` (currently `from`) into `to`, replacing only the middle that
 * differs. Boundaries never fall inside a surrogate pair: Yjs would replace a
 * split pair's halves with U+FFFD.
 */
function replaceChangedText(text: Y.XmlText, from: string, to: string) {
	let start = 0;
	while (start < from.length && start < to.length && from[start] === to[start]) start++;
	if (start > 0 && isHighSurrogate(from, start - 1)) start--;
	let end = 0;
	while (end < from.length - start && end < to.length - start && from[from.length - 1 - end] === to[to.length - 1 - end]) end++;
	// The kept suffix must not begin with the low half of a pair.
	if (end > 0 && isLowSurrogate(from, from.length - end)) end--;
	const removed = from.length - start - end;
	if (removed > 0) text.delete(start, removed);
	if (to.length - start - end > 0) text.insert(start, to.slice(start, to.length - end));
}

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
