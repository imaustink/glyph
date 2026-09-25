/**
 * Collaborative-mode editor behaviour with real TipTap editors synced through
 * Yjs. Each test is about a side effect that one person's edit must cause
 * once — not once per connected client.
 */
import { describe, it, expect, vi, afterEach } from 'vitest';
import * as Y from 'yjs';
import { getSchema } from '@tiptap/core';
import { TodoDetectionExtension, type DetectedBullet } from '$lib/editor/extensions/TodoDetectionExtension';
import { documentExtensions, documentSchema, schemaFingerprint } from '$lib/editor/schema';
import { isLocalTransaction } from '$lib/collab/isLocalTransaction';
import { makePeer, network, seed, listItems, posInItem, REMOTE, type Peer } from '$lib/collab/testing';
import { prosemirrorJSONToYDoc } from '@tiptap/y-tiptap';
import { COLLAB_FRAGMENT } from '$lib/editor/schema';
import { addSharedTextNodes } from '$lib/collab/sharedText';
import { TaskLinkExtension } from '$lib/editor/extensions/TaskLinkExtension';

const todoDoc = (items: { nodeId: string; taskId?: string; text: string }[]) => ({
	type: 'doc',
	content: [
		{ type: 'heading', attrs: { level: 2 }, content: [{ type: 'text', text: 'TODO' }] },
		{
			type: 'bulletList',
			content: items.map((i) => ({
				type: 'listItem',
				attrs: { nodeId: i.nodeId, taskId: i.taskId ?? null },
				content: [{ type: 'paragraph', content: [{ type: 'text', text: i.text }] }]
			}))
		},
		{ type: 'paragraph' }
	]
});

let peers: Peer[] = [];
afterEach(() => {
	for (const p of peers) p.editor.destroy();
	peers = [];
});

function detectingPeer(onDetected: (b: DetectedBullet[]) => void): Peer {
	const p = makePeer([
		TodoDetectionExtension.configure({ onTodoBulletsDetected: onDetected, pageId: () => 'page', todoTrigger: () => undefined, localChangesOnly: true })
	]);
	peers.push(p);
	return p;
}

describe('schema fingerprint', () => {
	it('is stable for the same schema', () => {
		expect(schemaFingerprint(documentSchema())).toBe(schemaFingerprint(getSchema(documentExtensions())));
		expect(schemaFingerprint()).toMatch(/^[0-9a-f]{16}$/);
	});

	it('is unaffected by editor-only options', () => {
		expect(schemaFingerprint(getSchema(documentExtensions({ undoRedo: false })))).toBe(schemaFingerprint());
	});

	it('changes when a node attribute changes', () => {
		const changed = getSchema([
			...documentExtensions().slice(0, 1),
			TaskLinkExtension.extend({
				addAttributes() {
					return { ...this.parent?.(), priority: { default: null } };
				}
			})
		]);
		expect(schemaFingerprint(changed)).not.toBe(schemaFingerprint());
	});
});

describe('isLocalTransaction', () => {
	it('distinguishes this user\'s edits from ones applied from the shared doc', () => {
		const a = makePeer();
		const b = makePeer();
		peers.push(a, b);
		network([a, b]);
		const seen: boolean[] = [];
		b.editor.on('transaction', ({ transaction }) => {
			if (transaction.docChanged) seen.push(isLocalTransaction(transaction));
		});
		a.editor.commands.insertContent('from a');
		b.editor.commands.insertContent('from b');
		expect(seen).toContain(false); // a's edit, arriving at b
		expect(seen).toContain(true); // b's own edit
	});
});

describe('TODO detection in collaborative mode', () => {
	it('only the author of a new bullet is offered task creation', () => {
		const detectedA = vi.fn();
		const detectedB = vi.fn();
		const a = detectingPeer(detectedA);
		const b = detectingPeer(detectedB);
		network([a, b]);
		seed([a, b], todoDoc([{ nodeId: 'n1', taskId: 't1', text: 'linked' }]));
		detectedA.mockClear();
		detectedB.mockClear();

		// Alice presses Enter at the end of the bullet and types a new one.
		a.editor.chain().setTextSelection(posInItem(a.editor, 'linked')).splitListItem('listItem').insertContent('write docs').run();

		const offeredToA = detectedA.mock.calls.flatMap((c) => c[0] as DetectedBullet[]);
		expect(offeredToA.map((b) => b.bulletText)).toContain('write docs');
		expect(detectedB).not.toHaveBeenCalled();
	});

	it('does not offer someone else\'s pending bullet when editing elsewhere', () => {
		const detectedA = vi.fn();
		const detectedB = vi.fn();
		const a = detectingPeer(detectedA);
		const b = detectingPeer(detectedB);
		network([a, b]);
		// A bullet with no task yet (its author is still filling in the popover).
		seed([a, b], todoDoc([{ nodeId: 'n1', text: 'alice is writing this' }]));
		detectedB.mockClear();

		// Bob types somewhere else entirely.
		b.editor.commands.setTextSelection(b.editor.state.doc.content.size - 1);
		b.editor.commands.insertContent('unrelated');
		const offered = detectedB.mock.calls.flatMap((c) => c[0] as DetectedBullet[]);
		expect(offered.map((x) => x.nodeId)).not.toContain('n1');
	});

	it('typing the TODO heading offers the bullets beneath it', () => {
		const detected = vi.fn();
		const a = detectingPeer(detected);
		const b = makePeer();
		peers.push(b);
		network([a, b]);
		seed([a, b], {
			type: 'doc',
			content: [
				{ type: 'heading', attrs: { level: 2 }, content: [{ type: 'text', text: 'TOD' }] },
				{
					type: 'bulletList',
					content: [{ type: 'listItem', attrs: { nodeId: 'x1' }, content: [{ type: 'paragraph', content: [{ type: 'text', text: 'one' }] }] }]
				}
			]
		});
		detected.mockClear();
		a.editor.chain().setTextSelection(4).insertContent('O').run();
		const offered = detected.mock.calls.flatMap((c) => c[0] as DetectedBullet[]);
		expect(offered.map((x) => x.nodeId)).toContain('x1');
	});
});

describe('bullet identity in collaborative mode', () => {
	it('gives a new bullet an id, and the other client sees the same id', () => {
		const a = makePeer();
		const b = makePeer();
		peers.push(a, b);
		network([a, b]);
		seed([a, b], todoDoc([{ nodeId: 'n1', text: 'first' }]));

		a.editor.chain().setTextSelection(posInItem(a.editor, 'first')).splitListItem('listItem').insertContent('second').run();
		const itemsA = listItems(a.editor);
		expect(itemsA).toHaveLength(2);
		expect(itemsA[1].nodeId).toEqual(expect.any(String));
		expect(itemsA[1].nodeId).not.toBe('n1');
		expect(listItems(b.editor)).toEqual(itemsA);
	});

	it('a pasted copy of a linked bullet gets its own id and no task link', () => {
		const a = makePeer();
		peers.push(a);
		seed([a], todoDoc([{ nodeId: 'n1', taskId: 't1', text: 'linked' }]));
		const endOfDoc = a.editor.state.doc.content.size - 1;
		a.editor.chain().setTextSelection(endOfDoc).insertContent({
			type: 'bulletList',
			content: [{ type: 'listItem', attrs: { nodeId: 'n1', taskId: 't1' }, content: [{ type: 'paragraph', content: [{ type: 'text', text: 'copy' }] }] }]
		}).run();

		const items = listItems(a.editor);
		const original = items.find((i) => i.text === 'linked')!;
		const copy = items.find((i) => i.text === 'copy')!;
		expect(original).toMatchObject({ nodeId: 'n1', taskId: 't1' });
		expect(copy.nodeId).not.toBe('n1');
		expect(copy.taskId).toBeNull();
	});

	it('editing a bullet in place keeps its identity', () => {
		const a = makePeer();
		peers.push(a);
		seed([a], todoDoc([{ nodeId: 'n1', taskId: 't1', text: 'linked' }]));
		a.editor.chain().setTextSelection(posInItem(a.editor, 'linked')).insertContent(' and edited').run();
		expect(listItems(a.editor)[0]).toMatchObject({ nodeId: 'n1', taskId: 't1', text: 'linked and edited' });
	});

	it('never rewrites identity in response to a remote edit', () => {
		const a = makePeer();
		const b = makePeer();
		peers.push(a, b);
		network([a, b]);
		seed([a, b], todoDoc([{ nodeId: 'n1', text: 'x' }]));
		let bWrites = 0;
		b.doc.on('update', (_u: Uint8Array, origin: unknown) => {
			// Anything b writes itself has a y-prosemirror binding origin.
			if (typeof origin !== 'symbol') bWrites++;
		});
		// A remote duplicate (as concurrent moves could produce).
		a.doc.transact(() => {
			const list = a.doc.getXmlFragment('default').get(1) as Y.XmlElement;
			const li = new Y.XmlElement('listItem');
			li.setAttribute('nodeId', 'n1');
			const p = new Y.XmlElement('paragraph');
			p.insert(0, [new Y.XmlText('dup')]);
			li.insert(0, [p]);
			list.insert(list.length, [li]);
		});
		expect(listItems(b.editor).filter((i) => i.nodeId === 'n1')).toHaveLength(2);
		expect(bWrites).toBe(0); // left for the server, the single actor for this
	});
});

describe('concurrent typing into an empty paragraph', () => {
	/** Seed like the collab service: from JSON, then shared text nodes (unless disabled). */
	function seedLikeServer(ps: Peer[], withSharedText: boolean) {
		const ydoc = prosemirrorJSONToYDoc(documentSchema(), { type: 'doc', content: [{ type: 'paragraph' }] }, COLLAB_FRAGMENT);
		if (withSharedText) addSharedTextNodes(ydoc.getXmlFragment(COLLAB_FRAGMENT), documentSchema());
		const update = Y.encodeStateAsUpdate(ydoc);
		for (const p of ps) Y.applyUpdate(p.doc, update, REMOTE);
	}

	function typeTogether(withSharedText: boolean) {
		const a = makePeer();
		const b = makePeer();
		peers.push(a, b);
		const net = network([a, b], false);
		seedLikeServer([a, b], withSharedText);
		// Both put the cursor in the empty paragraph and hit their first key
		// before either sees the other's.
		a.editor.chain().setTextSelection(1).insertContent('A').run();
		b.editor.chain().setTextSelection(1).insertContent('B').run();
		net.flush();
		a.editor.commands.insertContent('lice');
		b.editor.commands.insertContent('ob');
		net.flush();
		return [a.editor.getText(), b.editor.getText()];
	}

	it('keeps each person\'s characters together when the server seeds shared text nodes', () => {
		const [textA, textB] = typeTogether(true);
		expect(textA).toBe(textB);
		expect(textA).toContain('Alice');
		expect(textA).toContain('Bob');
	});});

describe('convergence', () => {
	it('editors converge on the same valid document under concurrent edits', () => {
		const n = 3;
		for (let i = 0; i < n; i++) peers.push(makePeer());
		const net = network(peers, false);
		seed(peers, todoDoc([{ nodeId: 'a', text: 'alpha' }, { nodeId: 'b', text: 'beta' }]));

		let s = 7;
		const rand = (k: number) => ((s = (s * 1103515245 + 12345) & 0x7fffffff) % k);
		for (let round = 0; round < 150; round++) {
			const { editor } = peers[rand(n)];
			const size = editor.state.doc.content.size;
			const pos = 1 + rand(Math.max(1, size - 1));
			try {
				switch (rand(6)) {
					case 0: editor.chain().setTextSelection(pos).insertContent(`w${round} `).run(); break;
					case 1: editor.chain().setTextSelection(pos).splitListItem('listItem').run(); break;
					case 2: editor.chain().setTextSelection({ from: pos, to: Math.min(size - 1, pos + 1 + rand(6)) }).deleteSelection().run(); break;
					case 3: editor.chain().setTextSelection(pos).toggleBulletList().run(); break;
					case 4: editor.chain().setTextSelection(pos).sinkListItem('listItem').run(); break;
					case 5: editor.chain().setTextSelection(pos).liftListItem('listItem').run(); break;
				}
			} catch {
				// Invalid positions for a command are fine; we only care about state.
			}
			if (rand(4) === 0) net.flush();
		}
		net.flush();
		net.flush();

		const reference = peers[0].editor.getJSON();
		for (const p of peers) expect(p.editor.getJSON()).toEqual(reference);
		// Every peer's Y doc is identical too (what the server would persist).
		const ref = Y.encodeStateVector(peers[0].doc);
		for (const p of peers) expect(Y.encodeStateVector(p.doc)).toEqual(ref);
		// And the document is representable in the schema.
		expect(() => documentSchema().nodeFromJSON(reference)).not.toThrow();
	});
});
