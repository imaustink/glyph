import { describe, it, expect } from 'vitest';
import * as Y from 'yjs';
import { COLLAB_FRAGMENT } from '$lib/editor/schema';
import { inspect, repair, normaliseForSeed, seedUpdate, removeListItem, toJSON } from '../src/documentRules.js';
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
				{ nodeId: 'n1', taskId: 't1', text: 'copy' },
				{ nodeId: 'n2', text: 'other' }
			])
		);
		const report = repair(doc, 'test');
		expect(report.dedupedNodeIds).toBe(1);

		const items = (toJSON(doc).content![0].content ?? []).map((li) => li.attrs ?? {});
		expect(items[0]).toMatchObject({ nodeId: 'n1', taskId: 't1' });
		expect(items[1].nodeId).not.toBe('n1');
		expect(items[1].taskId).toBeUndefined();
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
						{ type: 'listItem', attrs: { nodeId: 'x', taskId: 't' }, content: [{ type: 'paragraph' }] }
					]
				}
			]
		});
		const items = out.content![0].content!.map((li) => li.attrs!);
		expect(items[0].nodeId).toEqual(expect.any(String));
		expect(items[1]).toMatchObject({ nodeId: 'x', taskId: 't' });
		expect(items[2].nodeId).not.toBe('x');
		expect(items[2].taskId).toBeUndefined();
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

	it('refuses to seed content the schema cannot represent rather than dropping it', () => {
		expect(() => seedUpdate(schema, { type: 'doc', content: [{ type: 'mystery' }] })).toThrow();
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
