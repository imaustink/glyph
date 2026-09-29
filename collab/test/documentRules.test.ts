import { describe, it, expect } from 'vitest';
import * as Y from 'yjs';
import { COLLAB_FRAGMENT } from '$lib/editor/schema';
import { inspect, repair, normaliseForSeed, seedUpdate, removeListItem, setListItemText, toJSON } from '../src/documentRules.js';
import { schema, paragraph, bulletList } from './support/harness.js';

const MAX = 5 * 1024 * 1024;

function docWith(...nodes: Y.XmlElement[]): Y.Doc {
	const doc = new Y.Doc();
	doc.getXmlFragment(COLLAB_FRAGMENT).insert(0, nodes);
	return doc;
}

describe('inspect', () => {
	it('accepts a document the editor schema can represent', () => {
		const result = inspect(docWith(paragraph('hello')), schema, MAX);
		expect(result.fatal).toBeNull();
		expect(result.json).toEqual({ type: 'doc', content: [{ type: 'paragraph', content: [{ type: 'text', text: 'hello' }] }] });
	});

	it('rejects a node type the schema does not have', () => {
		// y-prosemirror would *delete* this element in every client whose
		// schema lacks it, so it must never be accepted into a shared doc.
		const doc = docWith(new Y.XmlElement('script'));
		expect(inspect(doc, schema, MAX).fatal).toMatch(/schema/);
	});

	it('rejects an unknown mark', () => {
		const p = new Y.XmlElement('paragraph');
		const t = new Y.XmlText();
		t.insert(0, 'x', { blink: {} });
		p.insert(0, [t]);
		expect(inspect(docWith(p), schema, MAX).fatal).toMatch(/schema/);
	});

	it('rejects an oversized document', () => {
		const result = inspect(docWith(paragraph('x'.repeat(200))), schema, 100);
		expect(result.fatal).toMatch(/limit/);
	});

	it('counts the size limit in UTF-8 bytes, as the API does [DI-LOW]', () => {
		// UTF-16 length undercounts CJK and emoji text (3–4 bytes per 1–2
		// units). A document under the limit by that count but over it in
		// bytes would pass here, be refused by the API, and get quarantined.
		const doc = docWith(paragraph('你好🙂'.repeat(40)));
		const serialised = JSON.stringify(toJSON(doc));
		const limit = serialised.length + 10;
		expect(Buffer.byteLength(serialised, 'utf8')).toBeGreaterThan(limit);
		const result = inspect(doc, schema, limit);
		expect(result.fatal ?? 'accepted').toMatch(/limit/);
		expect(result.bytes).toBe(Buffer.byteLength(serialised, 'utf8'));
	});

	it('reports a content-model violation without refusing it', () => {
		// A list item with no paragraph can come out of concurrent structural
		// edits; every client renders it, so it is logged, not refused.
		const list = new Y.XmlElement('bulletList');
		list.insert(0, [new Y.XmlElement('listItem')]);
		const result = inspect(docWith(list), schema, MAX);
		expect(result.fatal).toBeNull();
		expect(result.contentError).not.toBeNull();
	});
});

describe('repair', () => {
	it('gives duplicate nodeIds a fresh id and drops the duplicate task link', () => {
		const doc = docWith(
			bulletList([
				{ nodeId: 'n1', taskId: 't1', text: 'original' },
				{ nodeId: 'n1', taskId: 't1', checkbox: true, checked: true, text: 'copy' },
				{ nodeId: 'n2', text: 'other' }
			])
		);
		const report = repair(doc, 'test');
		expect(report.dedupedNodeIds).toBe(1);

		const items = (toJSON(doc).content![0].content ?? []).map((li) => li.attrs ?? {});
		expect(items[0]).toMatchObject({ nodeId: 'n1', taskId: 't1' });
		expect(items[1].nodeId).not.toBe('n1');
		expect(items[1].taskId).toBeUndefined();
		// The dropped duplicate loses its checkbox marker too, so it can't render
		// as a ticked box (checkbox:true) with no checked state after de-dup.
		expect(items[1].checkbox).toBeUndefined();
		expect(items[1].checked).toBeUndefined();
		expect(items[2].nodeId).toBe('n2');
	});

	it('strips links with an unsafe scheme', () => {
		const p = new Y.XmlElement('paragraph');
		const t = new Y.XmlText();
		t.insert(0, 'safe', { link: { href: 'https://example.com' } });
		t.insert(4, 'evil', { link: { href: 'java\tscript:alert(1)' } });
		p.insert(0, [t]);
		const doc = docWith(p);

		expect(repair(doc, 'test').strippedLinks).toBe(1);
		const text = toJSON(doc).content![0].content!;
		expect(text[0].marks).toEqual([{ type: 'link', attrs: { href: 'https://example.com' } }]);
		expect(JSON.stringify(text)).not.toContain('script:');
	});

	it('does nothing (and makes no transaction) on a clean document', () => {
		const doc = docWith(bulletList([{ nodeId: 'a', text: 'a' }]), paragraph('p'));
		let updates = 0;
		doc.on('update', () => updates++);
		expect(repair(doc, 'test')).toEqual({ dedupedNodeIds: 0, strippedLinks: 0 });
		expect(updates).toBe(0);
	});

	it('converges when two replicas repair the same document', () => {
		// Only the server repairs, but a repair must still be safe to apply
		// twice (e.g. two replicas before they catch up).
		const base = docWith(bulletList([{ nodeId: 'n1', text: 'a' }, { nodeId: 'n1', text: 'b' }]));
		const a = new Y.Doc();
		const b = new Y.Doc();
		Y.applyUpdate(a, Y.encodeStateAsUpdate(base));
		Y.applyUpdate(b, Y.encodeStateAsUpdate(base));
		repair(a, 'x');
		repair(b, 'y');
		Y.applyUpdate(a, Y.encodeStateAsUpdate(b));
		Y.applyUpdate(b, Y.encodeStateAsUpdate(a));
		expect(toJSON(a)).toEqual(toJSON(b));
		const ids = toJSON(a).content![0].content!.map((li) => li.attrs!.nodeId);
		expect(new Set(ids).size).toBe(2);
	});
});

describe('seeding', () => {
	it('assigns nodeIds to list items and de-duplicates them', () => {
		const out = normaliseForSeed({
			type: 'doc',
			content: [
				{
					type: 'bulletList',
					content: [
						{ type: 'listItem', content: [{ type: 'paragraph' }] },
						{ type: 'listItem', attrs: { nodeId: 'x', taskId: 't' }, content: [{ type: 'paragraph' }] },
						{ type: 'listItem', attrs: { nodeId: 'x', taskId: 't', checkbox: true, checked: true }, content: [{ type: 'paragraph' }] }
					]
				}
			]
		});
		const items = out.content![0].content!.map((li) => li.attrs!);
		expect(items[0].nodeId).toEqual(expect.any(String));
		expect(items[1]).toMatchObject({ nodeId: 'x', taskId: 't' });
		expect(items[2].nodeId).not.toBe('x');
		expect(items[2].taskId).toBeUndefined();
		expect(items[2].checkbox).toBeUndefined();
		expect(items[2].checked).toBeUndefined();
	});

	it('seeds an empty page with one paragraph', () => {
		expect(normaliseForSeed(null)).toEqual({ type: 'doc', content: [{ type: 'paragraph' }] });
		expect(normaliseForSeed({ type: 'doc', content: [] })).toEqual({ type: 'doc', content: [{ type: 'paragraph' }] });
	});

	it('round-trips stored JSON through the shared document without loss', () => {
		const stored = {
			type: 'doc',
			content: [
				{ type: 'heading', attrs: { level: 2 }, content: [{ type: 'text', text: 'TODO' }] },
				{
					type: 'bulletList',
					content: [
						{
							type: 'listItem',
							attrs: { nodeId: 'n1', taskId: 't1', checked: true, taskStatus: 'done' },
							content: [{ type: 'paragraph', content: [{ type: 'text', text: 'ship it', marks: [{ type: 'bold' }] }] }]
						}
					]
				}
			]
		};
		const doc = new Y.Doc();
		Y.applyUpdate(doc, seedUpdate(schema, stored));
		const back = schema.nodeFromJSON(toJSON(doc)).toJSON();
		// Identical apart from the trailing paragraph the editor would add anyway.
		expect(back).toEqual(schema.nodeFromJSON({ ...stored, content: [...stored.content, { type: 'paragraph' }] }).toJSON());
	});

	it('ends every seeded document with a paragraph, once', () => {
		const out = normaliseForSeed({ type: 'doc', content: [{ type: 'bulletList', content: [] }] });
		expect(out.content!.map((n) => n.type)).toEqual(['bulletList', 'paragraph']);
		expect(normaliseForSeed(out).content!.map((n) => n.type)).toEqual(['bulletList', 'paragraph']);
	});

	// DI-01: content saved before the API's allowlist matched the editor
	// schema can hold nodes and marks the editor lacks. The API serves such
	// content downgraded (NormalizeStoredContent); seeding must do the same,
	// or the note can never be opened collaboratively.
	describe('downgrades legacy content the way the API does', () => {
		const seeded = (content: unknown[]) => {
			const doc = new Y.Doc();
			Y.applyUpdate(doc, seedUpdate(schema, { type: 'doc', content } as never));
			return toJSON(doc).content ?? [];
		};

		it('turns a block image into a paragraph linking to its source', () => {
			expect(seeded([{ type: 'image', attrs: { src: 'https://e.com/a.png', alt: 'diagram' } }])[0]).toEqual({
				type: 'paragraph',
				content: [{ type: 'text', text: 'diagram', marks: [{ type: 'link', attrs: expect.objectContaining({ href: 'https://e.com/a.png' }) }] }]
			});
		});

		it('keeps an image with an unsafe source as plain text', () => {
			expect(seeded([{ type: 'image', attrs: { src: 'javascript:alert(1)', alt: 'x' } }])[0]).toEqual({
				type: 'paragraph',
				content: [{ type: 'text', text: 'x' }]
			});
		});

		it('drops a mark the editor lacks but keeps its text', () => {
			const content = seeded([
				{ type: 'paragraph', content: [{ type: 'text', text: 'hot', marks: [{ type: 'highlight' }, { type: 'bold' }] }] }
			]);
			expect(content[0]).toEqual({
				type: 'paragraph',
				content: [{ type: 'text', text: 'hot', marks: [expect.objectContaining({ type: 'bold' })] }]
			});
		});

		it('keeps the text and known children of an unknown block', () => {
			const content = seeded([
				{
					type: 'mystery',
					content: [
						{ type: 'text', text: 'loose' },
						{ type: 'paragraph', content: [{ type: 'text', text: 'kept' }] }
					]
				}
			]);
			expect(content.slice(0, 2)).toEqual([
				{ type: 'paragraph', content: [{ type: 'text', text: 'loose' }] },
				{ type: 'paragraph', content: [{ type: 'text', text: 'kept' }] }
			]);
		});

		it('flattens an unknown inline node to its text', () => {
			const content = seeded([
				{ type: 'paragraph', content: [{ type: 'text', text: 'a ' }, { type: 'mention', content: [{ type: 'text', text: '@bo' }] }] }
			]);
			expect(content[0]).toEqual({ type: 'paragraph', content: [{ type: 'text', text: 'a @bo' }] });
		});
	});
});

describe('removeListItem', () => {
	it('removes the item and an emptied list', () => {
		const doc = docWith(bulletList([{ nodeId: 'only', text: 'x' }]), paragraph('after'));
		expect(removeListItem(doc, 'only', 'test')).toBe(true);
		expect(toJSON(doc).content!.map((n) => n.type)).toEqual(['paragraph']);
	});

	it('keeps the list when other items remain', () => {
		const doc = docWith(bulletList([{ nodeId: 'a', text: 'a' }, { nodeId: 'b', text: 'b' }]));
		expect(removeListItem(doc, 'a', 'test')).toBe(true);
		expect(toJSON(doc).content![0].content!.map((li) => li.attrs!.nodeId)).toEqual(['b']);
	});

	it('reports when there is nothing to remove', () => {
		expect(removeListItem(docWith(paragraph('x')), 'missing', 'test')).toBe(false);
	});
});

describe('setListItemText (DI-29)', () => {
	/** The first list item's first paragraph, as ProseMirror JSON. */
	const firstParagraph = (doc: Y.Doc) => toJSON(doc).content!.find((n) => n.type === 'bulletList')!.content![0].content![0];
	const textOf = (doc: Y.Doc) => (firstParagraph(doc).content ?? []).map((n) => n.text ?? '').join('');

	/** A linked bullet whose first paragraph holds exactly `inline`. */
	function linkedBullet(inline: (Y.XmlText | Y.XmlElement)[], after: Y.XmlElement[] = []): Y.XmlElement {
		const li = new Y.XmlElement('listItem');
		li.setAttribute('nodeId', 'n1');
		li.setAttribute('taskId', 't1');
		const p = new Y.XmlElement('paragraph');
		p.insert(0, inline);
		li.insert(0, [p, ...after]);
		const list = new Y.XmlElement('bulletList');
		list.insert(0, [li]);
		return list;
	}

	it('puts a renamed task\'s title into its bullet, in one transaction with the given origin', () => {
		const doc = docWith(bulletList([{ nodeId: 'n1', taskId: 't1', text: 'Buy milk' }]));
		const origins: unknown[] = [];
		doc.on('afterTransaction', (tr: Y.Transaction) => origins.push(tr.origin));
		expect(setListItemText(doc, 'n1', 'Buy oat milk', 'test')).toBe(true);
		expect(textOf(doc)).toBe('Buy oat milk');
		expect(origins).toEqual(['test']);
	});

	it('writes nothing when the bullet already shows the title (ignoring surrounding space)', () => {
		const doc = docWith(bulletList([{ nodeId: 'n1', taskId: 't1', text: ' Buy milk ' }]));
		let transactions = 0;
		doc.on('afterTransaction', () => transactions++);
		expect(setListItemText(doc, 'n1', 'Buy milk', 'test')).toBe(false);
		expect(setListItemText(doc, 'n1', '   ', 'test')).toBe(false);
		expect(transactions).toBe(0);
	});

	it('leaves bullets without a task, and other bullets, alone', () => {
		const doc = docWith(
			bulletList([
				{ nodeId: 'plain', text: 'no task' },
				{ nodeId: 'other', taskId: 't2', text: 'another task' }
			])
		);
		expect(setListItemText(doc, 'plain', 'renamed', 'test')).toBe(false);
		expect(setListItemText(doc, 'missing', 'renamed', 'test')).toBe(false);
		const items = toJSON(doc).content![0].content!;
		expect(items.map((li) => li.content![0].content![0].text)).toEqual(['no task', 'another task']);
	});

	it('keeps the formatting of the text it doesn\'t change; new text takes the formatting before it', () => {
		const t = new Y.XmlText();
		const doc = docWith(linkedBullet([t]));
		t.insert(0, 'Buy ');
		t.insert(4, 'milk', { bold: {} });

		expect(setListItemText(doc, 'n1', 'Buy oat milk', 'test')).toBe(true);
		expect(firstParagraph(doc).content).toEqual([
			{ type: 'text', text: 'Buy oat ' },
			{ type: 'text', text: 'milk', marks: [{ type: 'bold', attrs: {} }] }
		]);
	});

	it('replaces the whole first paragraph (hard breaks too) and nothing after it', () => {
		const a = new Y.XmlText();
		const b = new Y.XmlText();
		const doc = docWith(linkedBullet([a, new Y.XmlElement('hardBreak'), b], [bulletList([{ nodeId: 'child', text: 'sub item' }])]));
		a.insert(0, 'line one');
		b.insert(0, 'line two');

		expect(setListItemText(doc, 'n1', 'One line', 'test')).toBe(true);
		const item = toJSON(doc).content![0].content![0];
		expect(item.content![0]).toEqual({ type: 'paragraph', content: [{ type: 'text', text: 'One line' }] });
		expect(item.content![1].content![0].content![0].content![0].text).toBe('sub item');
		expect(inspect(doc, schema, MAX).fatal).toBeNull();
	});

	it('fills an empty bullet', () => {
		const doc = docWith(linkedBullet([]));
		expect(setListItemText(doc, 'n1', 'Named at last', 'test')).toBe(true);
		expect(textOf(doc)).toBe('Named at last');
	});

	it('never splits a character outside the Basic Multilingual Plane', () => {
		// 😀 and 😃 share their first UTF-16 unit; a diff that kept it would
		// leave a lone surrogate, which Yjs replaces with U+FFFD.
		const doc = docWith(bulletList([{ nodeId: 'n1', taskId: 't1', text: 'Ship it 😀' }]));
		expect(setListItemText(doc, 'n1', 'Ship it 😃', 'test')).toBe(true);
		expect(textOf(doc)).toBe('Ship it 😃');
		expect(setListItemText(doc, 'n1', '😃 Ship it 😃', 'test')).toBe(true);
		expect(textOf(doc)).toBe('😃 Ship it 😃');
	});

	it('converges with a replica that receives the edit', () => {
		const doc = docWith(bulletList([{ nodeId: 'n1', taskId: 't1', text: 'Buy milk' }]));
		const replica = new Y.Doc();
		Y.applyUpdate(replica, Y.encodeStateAsUpdate(doc));
		setListItemText(doc, 'n1', 'Buy oat milk', 'test');
		Y.applyUpdate(replica, Y.encodeStateAsUpdate(doc));
		expect(textOf(replica)).toBe('Buy oat milk');
	});
});
