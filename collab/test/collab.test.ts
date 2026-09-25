/**
 * End-to-end tests of the collab service's integrity guarantees: a real
 * Hocuspocus server with the Glyph extension, real providers over
 * WebSockets, in-memory persistence with the same semantics as Postgres.
 */
import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import * as Y from 'yjs';
import { CollabReason } from '$lib/collab/protocol';
import { COLLAB_FRAGMENT } from '$lib/editor/schema';
import { MemoryPersistence } from './support/memoryPersistence.js';
import {
	FakeApi,
	startServer,
	connect,
	paragraph,
	bulletList,
	textOf,
	eventually,
	sleep,
	type TestServer,
	type TestClient
} from './support/harness.js';
import { toJSON } from '../src/documentRules.js';

const PAGE = '11111111-1111-4111-8111-111111111111';

const storedDoc = (text: string) => ({
	type: 'doc',
	content: [{ type: 'paragraph', content: [{ type: 'text', text }] }]
});

let persistence: MemoryPersistence;
let api: FakeApi;
let server: TestServer;
let clients: TestClient[];

function open(o: Parameters<typeof connect>[2], s: TestServer = server): TestClient {
	const c = connect(s, PAGE, o);
	clients.push(c);
	return c;
}

beforeEach(async () => {
	persistence = new MemoryPersistence();
	api = new FakeApi(persistence);
	persistence.addPage(PAGE, storedDoc('hello from REST'));
	api.grant(PAGE, 'alice', 'write');
	api.grant(PAGE, 'bob', 'write');
	api.grant(PAGE, 'viewer', 'read');
	server = await startServer(persistence, api);
	clients = [];
});

afterEach(async () => {
	for (const c of clients) c.destroy();
	await server.stop();
});

describe('seeding', () => {
	it('seeds the shared document from the stored page content', async () => {
		const alice = open({ user: 'alice' });
		await alice.synced();
		expect(textOf(alice.doc)).toContain('hello from REST');
		expect(alice.epoch).toBe(1);
	});

	it('seeds exactly once when many clients open a page at the same moment', async () => {
		// Two independent seeds would be two different Y histories of the same
		// text; merging them duplicates the whole page.
		const many = Array.from({ length: 8 }, (_, i) => open({ user: i % 2 ? 'alice' : 'bob' }));
		await Promise.all(many.map((c) => c.synced()));
		expect(persistence.seedCount).toBe(1);
		for (const c of many) {
			expect(textOf(c.doc).match(/hello from REST/g)).toHaveLength(1);
			expect(c.epoch).toBe(1);
		}
	});

	it('seeds exactly once across two server replicas', async () => {
		const other = await startServer(persistence, api);
		try {
			const a = open({ user: 'alice' });
			const b = open({ user: 'bob' }, other);
			await Promise.all([a.synced(), b.synced()]);
			expect(persistence.seedCount).toBe(1);
			expect(a.epoch).toBe(b.epoch);
		} finally {
			await other.stop();
		}
	});
});

describe('convergence and persistence', () => {
	it('propagates edits between clients and snapshots them to the API', async () => {
		const alice = open({ user: 'alice' });
		const bob = open({ user: 'bob' });
		await Promise.all([alice.synced(), bob.synced()]);

		alice.fragment.insert(alice.fragment.length, [paragraph('from alice')]);
		bob.fragment.insert(bob.fragment.length, [paragraph('from bob')]);

		await eventually(() => textOf(alice.doc) === textOf(bob.doc) && textOf(alice.doc).includes('from bob') && textOf(alice.doc).includes('from alice'));
		await eventually(() => JSON.stringify(api.latest(PAGE) ?? '').includes('from bob') && JSON.stringify(api.latest(PAGE)).includes('from alice'));
	});

	it('persists deletions (which do not advance the Yjs state vector)', async () => {
		const alice = open({ user: 'alice' });
		await alice.synced();
		alice.fragment.insert(alice.fragment.length, [paragraph('doomed')]);
		await eventually(() => textOf(persistence.replay(PAGE)).includes('doomed'));

		alice.fragment.delete(alice.fragment.length - 1, 1);
		await eventually(() => !textOf(persistence.replay(PAGE)).includes('doomed'), 3000, 'deletion persisted');
		await eventually(() => !JSON.stringify(api.latest(PAGE)).includes('doomed'));
	});

	it('keeps unpersisted updates in memory through a database outage and writes them later', async () => {
		const alice = open({ user: 'alice' });
		await alice.synced();
		persistence.failAppends = 2;
		alice.fragment.insert(alice.fragment.length, [paragraph('written during outage')]);
		await eventually(() => textOf(persistence.replay(PAGE)).includes('written during outage'), 8000, 'retried after outage');
	});

	it('flushes unsynced edits when the last client leaves during a persistence outage', async () => {
		// The sync ack is independent of server persistence, so the client already
		// believes these edits saved. If the last client disconnects while the
		// append is still failing, tearing the document down (cancelling the retry
		// and dropping `pending`) would silently lose them.
		const alice = open({ user: 'alice' });
		await alice.synced();
		persistence.failAppends = 3;
		alice.fragment.insert(alice.fragment.length, [paragraph('written then left')]);
		await sleep(150); // let the first persist fail and fall back to a retry
		alice.destroy(); // last client leaves while persistence is still failing
		await eventually(
			() => textOf(persistence.replay(PAGE)).includes('written then left'),
			10000,
			'pending update written after the client left and persistence recovered'
		);
	});

	it('keeps parked edits when the note is reopened before the deferred retry runs', async () => {
		// Same outage, but persistence recovers and someone opens the note again
		// before the parked document's retry fires. Loading the note afresh must
		// not orphan the parked edits: they belong in the new copy.
		const alice = open({ user: 'alice' });
		await alice.synced();
		persistence.failAppends = 100;
		alice.fragment.insert(alice.fragment.length, [paragraph('parked edit')]);
		await sleep(150);
		alice.destroy(); // unload is deferred: the final flush fails
		await sleep(150);
		persistence.failAppends = 0; // recovered, but the retry hasn't fired yet

		const bob = open({ user: 'bob' });
		await bob.synced();
		await eventually(() => textOf(bob.doc).includes('parked edit'), 3000, 'reopened note has the parked edit');
		await eventually(() => textOf(persistence.replay(PAGE)).includes('parked edit'), 5000, 'parked edit persisted');
	});

	it('reloads the same document from the log after every client leaves', async () => {
		const alice = open({ user: 'alice' });
		await alice.synced();
		alice.fragment.insert(alice.fragment.length, [paragraph('survives unload')]);
		await eventually(() => textOf(persistence.replay(PAGE)).includes('survives unload'));
		alice.destroy();
		await sleep(300);

		const later = open({ user: 'bob' });
		await later.synced();
		expect(textOf(later.doc)).toContain('survives unload');
		expect(persistence.seedCount).toBe(1);
	});

	it('compaction preserves the document', async () => {
		await server.stop();
		server = await startServer(persistence, api, { compactEvery: 3 });
		const alice = open({ user: 'alice' });
		await alice.synced();
		for (let i = 0; i < 8; i++) {
			alice.fragment.insert(alice.fragment.length, [paragraph(`line ${i}`)]);
			await sleep(80);
		}
		await eventually(() => textOf(persistence.replay(PAGE)).includes('line 7'));
		expect(persistence.rows.filter((r) => r.pageId === PAGE).length).toBeLessThan(8);
		expect(textOf(persistence.replay(PAGE))).toBe(textOf(alice.doc));
	});

	it('a compaction that folds in a foreign append still keeps it (no cross-replica update loss)', async () => {
		// README invariant 3: replicas can't lose each other's updates. Another
		// replica can append a row in the window between this replica catching up
		// and compacting; compaction folds that row into the merged one, whose
		// content this replica never applied. Advancing lastSeq to the merged seq
		// would make the next catch-up (strict seq > afterSeq) skip it forever.
		await server.stop();
		server = await startServer(persistence, api, { compactEvery: 2 });
		const alice = open({ user: 'alice' });
		await alice.synced();

		// A valid update from another replica's edit, relative to the seeded doc —
		// held back, not appended yet.
		const base = persistence.replay(PAGE);
		const foreign = new Y.Doc();
		Y.applyUpdate(foreign, Y.encodeStateAsUpdate(base));
		const beforeEdit = Y.encodeStateVector(foreign);
		foreign.getXmlFragment(COLLAB_FRAGMENT).insert(0, [paragraph('from another replica')]);
		const foreignUpdate = Y.encodeStateAsUpdate(foreign, beforeEdit);

		// Inject it exactly when alice's replica starts compacting — after it has
		// caught up and appended, so its in-memory doc has never seen it.
		let injected = false;
		persistence.onCompact = (pageId, epoch) => {
			if (injected) return;
			injected = true;
			persistence.injectRow(pageId, epoch, foreignUpdate);
		};

		for (let i = 0; i < 3; i++) {
			alice.fragment.insert(alice.fragment.length, [paragraph(`edit ${i}`)]);
			await sleep(60);
		}

		// The merged row must reach both alice's doc and her snapshot; otherwise
		// her next snapshot would regress the other replica's committed edit.
		await eventually(
			() => JSON.stringify(api.latest(PAGE) ?? '').includes('from another replica'),
			8000,
			'foreign append survived compaction in the snapshot'
		);
		await eventually(() => textOf(alice.doc).includes('from another replica'));
	});

	it('replicas pick up each other\'s updates before snapshotting', async () => {
		const other = await startServer(persistence, api);
		try {
			const a = open({ user: 'alice' });
			const b = open({ user: 'bob' }, other);
			await Promise.all([a.synced(), b.synced()]);
			a.fragment.insert(a.fragment.length, [paragraph('via replica A')]);
			await eventually(() => textOf(persistence.replay(PAGE)).includes('via replica A'));
			b.fragment.insert(b.fragment.length, [paragraph('via replica B')]);
			// B's replica catches up with A's append before snapshotting, so its
			// snapshot can't regress A's edit.
			await eventually(() => {
				const snap = JSON.stringify(api.latest(PAGE));
				return snap.includes('via replica A') && snap.includes('via replica B');
			});
		} finally {
			await other.stop();
		}
	});
});

describe('schema gate', () => {
	it('refuses a client whose editor schema differs before it can sync', async () => {
		const old = open({ user: 'alice', fingerprint: 'an-older-build' });
		await eventually(() => old.authFailures.length > 0);
		expect(old.authFailures[0]).toBe(CollabReason.SchemaMismatch);
		expect(textOf(old.doc)).toBe('');
	});
});

describe('access control', () => {
	it('refuses users without access', async () => {
		const mallory = open({ user: 'mallory' });
		await eventually(() => mallory.authFailures.length > 0);
		expect(mallory.authFailures[0]).toBe(CollabReason.Forbidden);
	});

	it('refuses a cross-site origin even with a valid session cookie', async () => {
		const hijack = open({ user: 'alice', origin: 'https://evil.example' });
		await eventually(() => hijack.authFailures.length > 0);
		expect(hijack.authFailures[0]).toBe(CollabReason.Forbidden);
	});

	it('discards a viewer\'s edits', async () => {
		const viewer = open({ user: 'viewer' });
		const alice = open({ user: 'alice' });
		await Promise.all([viewer.synced(), alice.synced()]);
		viewer.fragment.insert(viewer.fragment.length, [paragraph('viewer vandalism')]);
		await sleep(300);
		expect(textOf(alice.doc)).not.toContain('viewer vandalism');
		expect(textOf(persistence.replay(PAGE))).not.toContain('viewer vandalism');
	});

	it('disconnects a user whose access is revoked', async () => {
		const bob = open({ user: 'bob' });
		await bob.synced();
		api.revoke(PAGE, 'bob');
		await server.collab.reauthorizeAll();
		await eventually(() => bob.closeReasons.includes(CollabReason.Forbidden));
	});

	it('shows the authenticated name on presence, whatever the client claims', async () => {
		const bob = open({ user: 'bob' });
		const alice = open({ user: 'alice' });
		await Promise.all([bob.synced(), alice.synced()]);
		bob.provider.setAwarenessField('user', { name: 'alice (really)', color: '#123456' });
		let names: (string | undefined)[] = [];
		await eventually(() => {
			names = [...alice.provider.awareness!.getStates().values()].map((s) => (s as { user?: { name: string } }).user?.name);
			return names.includes('bob') && !names.includes('alice (really)');
		}, 3000, `names seen: ${JSON.stringify(names)}`).catch((e) => {
			throw new Error(`${(e as Error).message} ${JSON.stringify(names)}`);
		});
	});

	it('makes a user read-only when downgraded to viewer', async () => {
		const bob = open({ user: 'bob' });
		const alice = open({ user: 'alice' });
		await Promise.all([bob.synced(), alice.synced()]);
		api.grant(PAGE, 'bob', 'read');
		await server.collab.reauthorizeAll();
		await eventually(() => bob.readOnly === true);
		bob.fragment.insert(bob.fragment.length, [paragraph('after downgrade')]);
		await sleep(300);
		expect(textOf(alice.doc)).not.toContain('after downgrade');
	});
});

describe('epochs', () => {
	it('evicts every client when the document is replaced, and a stale client cannot merge back', async () => {
		const alice = open({ user: 'alice' });
		await alice.synced();
		alice.fragment.insert(alice.fragment.length, [paragraph('edit made before the restore')]);
		await eventually(() => textOf(persistence.replay(PAGE)).includes('edit made before the restore'));

		// A version restore: the API writes the restored content and detaches.
		persistence.detach(PAGE, storedDoc('restored version'));
		server.collab.onReset(PAGE);
		await eventually(() => alice.closeReasons.includes(CollabReason.Reset));

		// Alice's Y.Doc still holds the pre-restore state. If it could sync, the
		// restore would be undone. Reconnecting with her old epoch is refused…
		const staleEpoch = alice.epoch!;
		const staleDoc = alice.doc;
		alice.destroy();
		await sleep(200);
		const stale = open({ user: 'alice', epoch: staleEpoch, doc: staleDoc });
		await eventually(() => stale.authFailures.length > 0 || stale.closeReasons.length > 0);
		expect([...stale.authFailures, ...stale.closeReasons]).toContain(CollabReason.StaleEpoch);

		// …and so is pretending to be fresh while holding state.
		stale.destroy();
		await sleep(100);
		const sneaky = open({ user: 'alice', epoch: null, doc: staleDoc });
		await eventually(() => sneaky.closeReasons.includes(CollabReason.StaleEpoch));

		// A genuinely fresh client gets the restored document in a new epoch.
		const fresh = open({ user: 'bob' });
		await fresh.synced();
		expect(fresh.epoch).toBe(staleEpoch + 1);
		expect(textOf(fresh.doc)).toContain('restored version');
		expect(textOf(fresh.doc)).not.toContain('edit made before the restore');
		expect(textOf(persistence.replay(PAGE))).not.toContain('edit made before the restore');
	});

	it('evicts when the API rejects a snapshot as stale', async () => {
		const alice = open({ user: 'alice' });
		await alice.synced();
		api.onSnapshot = () => ({ kind: 'stale' });
		alice.fragment.insert(alice.fragment.length, [paragraph('x')]);
		await eventually(() => alice.closeReasons.includes(CollabReason.Reset));
	});

	it('evicts when an append finds the epoch replaced (missed notification)', async () => {
		const alice = open({ user: 'alice' });
		await alice.synced();
		// Detached without the notification reaching this replica.
		persistence.detach(PAGE);
		alice.fragment.insert(alice.fragment.length, [paragraph('lost to the restore')]);
		await eventually(() => alice.closeReasons.includes(CollabReason.Reset));
		expect(api.latest(PAGE) ? JSON.stringify(api.latest(PAGE)) : '').not.toContain('lost to the restore');
	});

	it('re-seeds through JSON when the stored log predates the current schema', async () => {
		const alice = open({ user: 'alice' });
		await alice.synced();
		alice.fragment.insert(alice.fragment.length, [paragraph('written by the old build')]);
		await eventually(() => textOf(persistence.replay(PAGE)).includes('written by the old build'));
		alice.destroy();
		await sleep(300);
		persistence.docs.get(PAGE)!.schemaFingerprint = 'old-build';

		const next = open({ user: 'bob' });
		await next.synced();
		expect(next.epoch).toBe(2);
		expect(textOf(next.doc)).toContain('written by the old build');
		expect(textOf(next.doc)).toContain('hello from REST');
	});
});

describe('update validation', () => {
	it('refuses an update the schema cannot represent, without applying it', async () => {
		const alice = open({ user: 'alice' });
		const bob = open({ user: 'bob' });
		await Promise.all([alice.synced(), bob.synced()]);

		// Other clients' y-prosemirror would delete this element when it failed
		// to build — the server must never let it in.
		bob.fragment.insert(bob.fragment.length, [new Y.XmlElement('iframe')]);
		await eventually(() => bob.closeReasons.includes(CollabReason.InvalidUpdate));
		await sleep(200);
		expect(textOf(alice.doc)).not.toContain('iframe');
		expect(textOf(persistence.replay(PAGE))).not.toContain('iframe');

		// The document keeps working for everyone else.
		alice.fragment.insert(alice.fragment.length, [paragraph('still fine')]);
		await eventually(() => textOf(persistence.replay(PAGE)).includes('still fine'));
	});

	it('refuses an update that makes the document too large', async () => {
		await server.stop();
		server = await startServer(persistence, api, { maxDocumentBytes: 2000 });
		const alice = open({ user: 'alice' });
		await alice.synced();
		alice.fragment.insert(alice.fragment.length, [paragraph('x'.repeat(5000))]);
		await eventually(() => alice.closeReasons.includes(CollabReason.InvalidUpdate));
		expect(textOf(persistence.replay(PAGE))).not.toContain('xxxxxxxx');
	});
});

describe('server-only repairs', () => {
	it('de-duplicates nodeIds so a task stays linked to one bullet', async () => {
		const alice = open({ user: 'alice' });
		await alice.synced();
		alice.fragment.insert(alice.fragment.length, [
			bulletList([
				{ nodeId: 'same', taskId: 't1', text: 'first' },
				{ nodeId: 'same', taskId: 't1', text: 'second' }
			])
		]);
		await eventually(() => {
			const snap = api.latest(PAGE) as { content: { type: string; content?: { attrs: Record<string, unknown> }[] }[] } | undefined;
			const list = snap?.content.find((n) => n.type === 'bulletList');
			if (!list?.content) return false;
			const ids = list.content.map((li) => li.attrs.nodeId);
			return new Set(ids).size === 2 && list.content.filter((li) => li.attrs.taskId === 't1').length === 1;
		});
		// The fix reached the client too.
		await eventually(() => {
			const list = toJSON(alice.doc).content!.find((n) => n.type === 'bulletList');
			return new Set(list?.content?.map((li) => li.attrs!.nodeId)).size === 2;
		});
	});

	it('strips javascript: links from the shared document', async () => {
		const alice = open({ user: 'alice' });
		const bob = open({ user: 'bob' });
		await Promise.all([alice.synced(), bob.synced()]);
		const p = new Y.XmlElement('paragraph');
		const t = new Y.XmlText();
		t.insert(0, 'click me', { link: { href: 'javascript:alert(document.cookie)' } });
		p.insert(0, [t]);
		alice.fragment.insert(alice.fragment.length, [p]);

		await eventually(() => textOf(persistence.replay(PAGE)).includes('click me') && !textOf(persistence.replay(PAGE)).includes('javascript:'));
		await eventually(() => textOf(bob.doc).includes('click me') && !textOf(bob.doc).includes('javascript:'));
	});
});

describe('quarantine', () => {
	it('makes collaborators read-only when the API refuses the content', async () => {
		const alice = open({ user: 'alice' });
		await alice.synced();
		api.onSnapshot = () => ({ kind: 'invalid', message: 'nope' });
		alice.fragment.insert(alice.fragment.length, [paragraph('trigger')]);
		await eventually(() => alice.quarantined);
		expect(persistence.docs.get(PAGE)!.quarantined).toBe(true);

		api.onSnapshot = null;
		alice.fragment.insert(alice.fragment.length, [paragraph('after quarantine')]);
		await sleep(300);
		expect(textOf(persistence.replay(PAGE))).not.toContain('after quarantine');
	});
});

describe('server edits', () => {
	it('removes a list item through the shared document so editors see it', async () => {
		const alice = open({ user: 'alice' });
		await alice.synced();
		alice.fragment.insert(alice.fragment.length, [bulletList([{ nodeId: 'gone', text: 'delete me' }, { nodeId: 'kept', text: 'keep me' }])]);
		await eventually(() => textOf(persistence.replay(PAGE)).includes('delete me'));

		const res = await fetch(`http://localhost:${server.port}/collab/ops/pages/${PAGE}/remove-list-item`, {
			method: 'POST',
			headers: { origin: server.origin, cookie: 'user=bob', 'x-requested-with': 'XMLHttpRequest', 'content-type': 'application/json' },
			body: JSON.stringify({ nodeId: 'gone' })
		});
		expect(res.status).toBe(200);
		expect(await res.json()).toEqual({ removed: true });
		await eventually(() => !textOf(alice.doc).includes('delete me') && textOf(alice.doc).includes('keep me'));
		await eventually(() => !textOf(persistence.replay(PAGE)).includes('delete me'));
	});

	it('refuses cross-site and read-only callers', async () => {
		const call = (headers: Record<string, string>) =>
			fetch(`http://localhost:${server.port}/collab/ops/pages/${PAGE}/remove-list-item`, {
				method: 'POST',
				headers: { 'content-type': 'application/json', ...headers },
				body: JSON.stringify({ nodeId: 'x' })
			});
		expect((await call({ origin: 'https://evil.example', cookie: 'user=alice', 'x-requested-with': 'x' })).status).toBe(403);
		expect((await call({ origin: server.origin, cookie: 'user=alice' })).status).toBe(403);
		expect((await call({ origin: server.origin, cookie: 'user=viewer', 'x-requested-with': 'x' })).status).toBe(403);
		expect((await call({ origin: server.origin, cookie: 'user=mallory', 'x-requested-with': 'x' })).status).toBe(404);
	});
});

describe('convergence under concurrent editing', () => {
	it('every client and the persisted log agree after random concurrent edits', async () => {
		const users = ['alice', 'bob', 'alice', 'bob'];
		const cs = users.map((user) => open({ user }));
		await Promise.all(cs.map((c) => c.synced()));
		let seed = 42;
		const rand = (n: number) => {
			seed = (seed * 1103515245 + 12345) & 0x7fffffff;
			return seed % n;
		};
		for (let round = 0; round < 60; round++) {
			const c = cs[rand(cs.length)];
			const frag = c.fragment;
			const op = rand(4);
			if (op === 0 || frag.length === 0) {
				frag.insert(rand(frag.length + 1), [paragraph(`r${round}`)]);
			} else if (op === 1) {
				frag.insert(rand(frag.length + 1), [bulletList([{ nodeId: `n${round}`, text: `b${round}` }])]);
			} else if (op === 2 && frag.length > 1) {
				frag.delete(rand(frag.length), 1);
			} else {
				const el = frag.get(rand(frag.length));
				const text = el instanceof Y.XmlElement ? el.get(0) : null;
				if (text instanceof Y.XmlText) text.insert(rand(text.length + 1), '·');
			}
			if (rand(3) === 0) await sleep(5);
		}
		await eventually(() => cs.every((c) => textOf(c.doc) === textOf(cs[0].doc)), 5000, 'clients converged');
		await eventually(() => textOf(persistence.replay(PAGE)) === textOf(cs[0].doc), 5000, 'log matches clients');
		const snapshotted = JSON.stringify(api.latest(PAGE));
		await eventually(() => JSON.stringify(api.latest(PAGE)) === JSON.stringify(toJSON(cs[0].doc)), 5000, 'snapshot matches clients');
		expect(snapshotted).toBeTruthy();
		// No duplicated seed content.
		expect(textOf(cs[0].doc).match(/hello from REST/g)?.length ?? 0).toBeLessThanOrEqual(1);
		// The fragment still converts under the schema (nothing unrepresentable).
		expect(() => cs[0].doc.getXmlFragment(COLLAB_FRAGMENT).toJSON()).not.toThrow();
	});
});
