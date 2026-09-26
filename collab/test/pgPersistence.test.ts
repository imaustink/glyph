/**
 * PgPersistence against a real Postgres with the API's migrations applied.
 * Runs when COLLAB_TEST_DATABASE_URL points at a disposable database
 * (CI provides one; locally: `docker run -e POSTGRES_PASSWORD=t -p 55432:5432 postgres:16-alpine`
 * and COLLAB_TEST_DATABASE_URL=postgres://postgres:t@localhost:55432/postgres).
 */
import { describe, it, expect, beforeAll, afterAll, beforeEach } from 'vitest';
import { readdirSync, readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { randomUUID } from 'node:crypto';
import pg from 'pg';
import * as Y from 'yjs';
import { PgPersistence } from '../src/persistence.js';
import { seedUpdate, toJSON } from '../src/documentRules.js';
import { schema, fingerprint, paragraph, textOf } from './support/harness.js';
import { COLLAB_FRAGMENT } from '$lib/editor/schema';

const url = process.env.COLLAB_TEST_DATABASE_URL;
const migrations = path.join(path.dirname(fileURLToPath(import.meta.url)), '../../api/migrations');

describe.skipIf(!url)('PgPersistence (Postgres)', () => {
	let pool: pg.Pool;
	let persistence: PgPersistence;
	let userId: string;

	beforeAll(async () => {
		pool = new pg.Pool({ connectionString: url, max: 20 });
		await pool.query('DROP SCHEMA public CASCADE; CREATE SCHEMA public;');
		for (const f of readdirSync(migrations).filter((f) => f.endsWith('.up.sql')).sort()) {
			await pool.query(readFileSync(path.join(migrations, f), 'utf8'));
		}
		const u = await pool.query(`INSERT INTO users (sub, issuer, email) VALUES ('s', 'i', 'a@b.c') RETURNING id`);
		userId = u.rows[0].id;
		persistence = new PgPersistence(pool);
	});
	afterAll(async () => {
		await pool.end();
	});

	let pageId: string;
	beforeEach(async () => {
		pageId = randomUUID();
		await pool.query(`INSERT INTO pages (id, user_id, type, title) VALUES ($1, $2, 'page', 't')`, [pageId, userId]);
		await pool.query(`INSERT INTO page_contents (page_id, content) VALUES ($1, $2)`, [
			pageId,
			JSON.stringify({ type: 'doc', content: [{ type: 'paragraph', content: [{ type: 'text', text: 'stored' }] }] })
		]);
	});

	const seeder = (stored: { content: unknown } | null) => seedUpdate(schema, stored?.content as never);

	it('seeds exactly once under concurrent first loads', async () => {
		let seeds = 0;
		const counting = (s: { content: unknown } | null) => {
			seeds++;
			return seeder(s);
		};
		const loads = await Promise.all(Array.from({ length: 12 }, () => persistence.loadOrSeed(pageId, counting, fingerprint)));
		expect(seeds).toBe(1);
		expect(new Set(loads.map((l) => l.epoch))).toEqual(new Set([1]));
		expect(loads.filter((l) => l.seeded)).toHaveLength(1);

		const doc = new Y.Doc();
		for (const u of loads.find((l) => !l.seeded)!.updates) Y.applyUpdate(doc, u.data);
		expect(textOf(doc).match(/stored/g)).toHaveLength(1);
	});

	it('waits for a REST write holding the page lock before seeding', async () => {
		const client = await pool.connect();
		await client.query('BEGIN');
		await client.query(`SELECT id FROM pages WHERE id = $1 FOR UPDATE`, [pageId]);
		const loading = persistence.loadOrSeed(pageId, seeder, fingerprint);
		await new Promise((r) => setTimeout(r, 150));
		await client.query(`UPDATE page_contents SET content = $2 WHERE page_id = $1`, [
			pageId,
			JSON.stringify({ type: 'doc', content: [{ type: 'paragraph', content: [{ type: 'text', text: 'written while locked' }] }] })
		]);
		await client.query('COMMIT');
		client.release();

		const loaded = await loading;
		const doc = new Y.Doc();
		for (const u of loaded.updates) Y.applyUpdate(doc, u.data);
		expect(textOf(doc)).toContain('written while locked');
	});

	it('refuses appends to a replaced epoch', async () => {
		const loaded = await persistence.loadOrSeed(pageId, seeder, fingerprint);
		await pool.query(`UPDATE page_collab_docs SET attached = false WHERE page_id = $1`, [pageId]);
		expect(await persistence.append(pageId, loaded.epoch, new Uint8Array([0, 0]))).toBeNull();
		const next = await persistence.loadOrSeed(pageId, seeder, fingerprint);
		expect(next.epoch).toBe(loaded.epoch + 1);
		expect(await persistence.append(pageId, loaded.epoch, new Uint8Array([0, 0]))).toBeNull();
	});

	it('compaction never loses an append that races it', async () => {
		const loaded = await persistence.loadOrSeed(pageId, seeder, fingerprint);
		const doc = new Y.Doc();
		for (const u of loaded.updates) Y.applyUpdate(doc, u.data);
		const updates: Uint8Array[] = [];
		doc.on('update', (u: Uint8Array) => updates.push(u));
		const frag = doc.getXmlFragment(COLLAB_FRAGMENT);
		for (let i = 0; i < 40; i++) frag.insert(frag.length, [paragraph(`p${i}`)]);

		// Append the first half, then compact while appending the second half.
		for (const u of updates.slice(0, 20)) await persistence.append(pageId, loaded.epoch, u);
		await Promise.all([
			persistence.compact(pageId, loaded.epoch),
			...updates.slice(20).map((u) => persistence.append(pageId, loaded.epoch, u)),
			persistence.compact(pageId, loaded.epoch)
		]);

		const replay = new Y.Doc();
		for (const u of await persistence.fetchSince(pageId, loaded.epoch, 0)) Y.applyUpdate(replay, u.data);
		expect(toJSON(replay)).toEqual(toJSON(doc));
	});

	it('a reader that has seen a seq has seen every lower seq of the page [DI-11]', async () => {
		// BIGSERIAL assigns seq when the row is inserted, not when it commits.
		// An append that takes seq N and commits after another append took and
		// committed N+1 is invisible to a reader that already moved past N+1
		// (fetchSince is strictly seq > afterSeq), so that replica skips it
		// forever. A trigger stands in for a slow commit: it sleeps, after the
		// seq is assigned and before the statement commits, for one marked row.
		await pool.query(`
			CREATE OR REPLACE FUNCTION test_slow_append() RETURNS trigger AS $$
			BEGIN
				IF NEW.data = '\\xdead'::bytea THEN PERFORM pg_sleep(0.5); END IF;
				RETURN NEW;
			END $$ LANGUAGE plpgsql;
			CREATE TRIGGER test_slow_append AFTER INSERT ON page_collab_updates
				FOR EACH ROW EXECUTE FUNCTION test_slow_append();`);
		try {
			const loaded = await persistence.loadOrSeed(pageId, seeder, fingerprint);
			const slow = persistence.append(pageId, loaded.epoch, new Uint8Array([0xde, 0xad]));
			await new Promise((r) => setTimeout(r, 100)); // slow has its seq, not yet committed
			await persistence.append(pageId, loaded.epoch, new Uint8Array([0, 0]));
			const seen = await persistence.fetchSince(pageId, loaded.epoch, 0);
			await slow;

			const upTo = Math.max(...seen.map((u) => u.seq));
			const later = await persistence.fetchSince(pageId, loaded.epoch, upTo);
			const all = await persistence.fetchSince(pageId, loaded.epoch, 0);
			// Everything in the log is reachable from what the reader saw.
			const bySeq = (a: number, b: number) => a - b;
			expect([...seen, ...later].map((u) => u.seq).sort(bySeq)).toEqual(all.map((u) => u.seq).sort(bySeq));
		} finally {
			await pool.query(`DROP TRIGGER test_slow_append ON page_collab_updates; DROP FUNCTION test_slow_append();`);
		}
	});

	it('reseed only succeeds from the current epoch', async () => {
		const loaded = await persistence.loadOrSeed(pageId, seeder, fingerprint);
		const update = seedUpdate(schema, { type: 'doc', content: [{ type: 'paragraph', content: [{ type: 'text', text: 'v2' }] }] });
		expect(await persistence.reseed(pageId, loaded.epoch + 5, update, fingerprint)).toBeNull();
		const next = await persistence.reseed(pageId, loaded.epoch, update, fingerprint);
		expect(next).toBe(loaded.epoch + 1);
		expect(await persistence.reseed(pageId, loaded.epoch, update, fingerprint)).toBeNull();
	});

	it('a schema reseed waits for other replicas\' leases on the epoch [DI-16]', async () => {
		const older = { holder: 'replica-old', ttlMs: 60000 };
		const newer = { holder: 'replica-new', ttlMs: 60000 };
		const loaded = await persistence.loadOrSeed(pageId, seeder, 'old-build', older);
		// The new build loads the old-fingerprint log without leasing it…
		await persistence.loadOrSeed(pageId, seeder, fingerprint, newer);
		const leases = await pool.query(`SELECT holder, epoch FROM page_collab_leases WHERE page_id = $1`, [pageId]);
		expect(leases.rows).toEqual([{ holder: 'replica-old', epoch: loaded.epoch }]);

		// …and may not replace the epoch while the older replica holds it.
		const update = seedUpdate(schema, { type: 'doc', content: [{ type: 'paragraph', content: [{ type: 'text', text: 'v2' }] }] });
		expect(await persistence.reseed(pageId, loaded.epoch, update, fingerprint, newer)).toBe('held');

		await persistence.releaseLease(pageId, older.holder, loaded.epoch);
		expect(await persistence.reseed(pageId, loaded.epoch, update, fingerprint, newer)).toBe(loaded.epoch + 1);
		const after = await pool.query(`SELECT holder, epoch FROM page_collab_leases WHERE page_id = $1`, [pageId]);
		expect(after.rows).toEqual([{ holder: 'replica-new', epoch: loaded.epoch + 1 }]);
	});

	it('an expired lease does not hold up a reseed; renewal keeps one alive [DI-16]', async () => {
		const loaded = await persistence.loadOrSeed(pageId, seeder, 'old-build', { holder: 'gone', ttlMs: 1 });
		await persistence.renewLeases('alive', [{ pageId, epoch: loaded.epoch }], 60000);
		const update = seedUpdate(schema, null);
		await new Promise((r) => setTimeout(r, 20));
		expect(await persistence.reseed(pageId, loaded.epoch, update, fingerprint, { holder: 'new', ttlMs: 60000 })).toBe('held');
		await persistence.releaseLease(pageId, 'alive', loaded.epoch);
		expect(await persistence.reseed(pageId, loaded.epoch, update, fingerprint, { holder: 'new', ttlMs: 60000 })).toBe(loaded.epoch + 1);
	});

	it('skips leasing while the leases table does not exist yet (migration not run)', async () => {
		await pool.query(`ALTER TABLE page_collab_leases RENAME TO page_collab_leases_hidden`);
		try {
			const fresh = new PgPersistence(pool);
			const loaded = await fresh.loadOrSeed(pageId, seeder, 'old-build', { holder: 'a', ttlMs: 60000 });
			const update = seedUpdate(schema, null);
			expect(await fresh.reseed(pageId, loaded.epoch, update, fingerprint, { holder: 'b', ttlMs: 60000 })).toBe(loaded.epoch + 1);
			await fresh.renewLeases('b', [{ pageId, epoch: loaded.epoch + 1 }], 60000);
			await fresh.releaseLease(pageId, 'b', loaded.epoch + 1);
		} finally {
			await pool.query(`ALTER TABLE page_collab_leases_hidden RENAME TO page_collab_leases`);
		}
	});

	it('appendExclusive builds from the latest log under the log lock, one writer at a time (DI-29)', async () => {
		const loaded = await persistence.loadOrSeed(pageId, seeder, fingerprint);
		const after = Math.max(...loaded.updates.map((u) => u.seq));
		const seen: number[][] = [];
		const build = (rows: { seq: number }[]) => {
			seen.push(rows.map((r) => r.seq));
			// Like a title edit: only the first writer (seeing no one else's row) writes.
			return rows.length === 0 ? new Uint8Array([0, 0]) : null;
		};
		const results = await Promise.all([
			persistence.appendExclusive(pageId, loaded.epoch, after, build),
			persistence.appendExclusive(pageId, loaded.epoch, after, build)
		]);
		const written = results.filter((r): r is number => typeof r === 'number');
		expect(written).toHaveLength(1);
		expect(results.filter((r) => r === null)).toHaveLength(1);
		// The second writer saw the first one's row.
		expect(seen).toEqual([[], [written[0]]]);
	});

	it('appendExclusive refuses a replaced epoch (DI-29)', async () => {
		const loaded = await persistence.loadOrSeed(pageId, seeder, fingerprint);
		await pool.query(`UPDATE page_collab_docs SET attached = false WHERE page_id = $1`, [pageId]);
		let built = false;
		const result = await persistence.appendExclusive(pageId, loaded.epoch, 0, () => {
			built = true;
			return new Uint8Array([0, 0]);
		});
		expect(result).toBe('stale');
		expect(built).toBe(false);
	});

	describe('task titles renamed outside the note (DI-29)', () => {
		const pause = () => new Promise((r) => setTimeout(r, 5));
		async function addTask(nodeId: string, title: string, page = pageId) {
			const { rows } = await pool.query(
				`INSERT INTO tasks (user_id, title, source_page_id, source_node_id) VALUES ($1, $2, $3, $4) RETURNING id`,
				[userId, title, page, nodeId]
			);
			return rows[0].id as string;
		}
		const rename = (id: string, title: string) => pool.query(`UPDATE tasks SET title = $2, title_renamed_at = NOW() WHERE id = $1`, [id, title]);

		it('lists the page\'s live tasks renamed after the loaded content was written', async () => {
			const early = await addTask('early', 'early');
			await rename(early, 'renamed before the content');
			await addTask('never', 'never renamed');
			const gone = await addTask('gone', 'gone');
			await pause();
			await pool.query(`UPDATE page_contents SET updated_at = NOW() WHERE page_id = $1`, [pageId]); // content written after "early"'s rename
			await pause();
			const loaded = await persistence.loadOrSeed(pageId, seeder, fingerprint);
			expect(loaded.contentAsOf).not.toBeNull();
			await pause();

			const later = await addTask('later', 'later');
			await rename(later, 'renamed after the content');
			await rename(gone, 'renamed, then deleted');
			await pool.query(`UPDATE tasks SET deleted_at = NOW(), deleted_reason = 'user' WHERE id = $1`, [gone]);
			const elsewhere = randomUUID();
			await pool.query(`INSERT INTO pages (id, user_id, type, title) VALUES ($1, $2, 'page', 'other')`, [elsewhere, userId]);
			await rename(await addTask('later', 'on another page', elsewhere), 'renamed elsewhere');

			expect(await persistence.renamedTaskTitles(pageId, loaded.contentAsOf)).toEqual([{ nodeId: 'later', title: 'renamed after the content' }]);
			expect(await persistence.renamedTaskTitles(pageId, null)).toHaveLength(2);
		});

		it('a seeded document is as old as the stored content it came from', async () => {
			const id = await addTask('n1', 'Buy milk');
			await pause();
			await rename(id, 'Buy oat milk'); // after page_contents was written, before the seed
			await pause();
			const seeded = await persistence.loadOrSeed(pageId, seeder, fingerprint);
			expect(seeded.seeded).toBe(true);
			expect(await persistence.renamedTaskTitles(pageId, seeded.contentAsOf)).toEqual([{ nodeId: 'n1', title: 'Buy oat milk' }]);
			// …and stays so on the next load, if the first one couldn't apply it.
			const reloaded = await persistence.loadOrSeed(pageId, seeder, fingerprint);
			expect(await persistence.renamedTaskTitles(pageId, reloaded.contentAsOf)).toHaveLength(1);
		});

		it('an append after the rename makes the loaded content newer; compaction does not', async () => {
			const id = await addTask('n1', 'Buy milk');
			const first = await persistence.loadOrSeed(pageId, seeder, fingerprint);
			await pause();
			await rename(id, 'Buy oat milk');
			await pause();
			// Compacting the rows from before the rename doesn't make them newer.
			await persistence.append(pageId, first.epoch, new Uint8Array([0, 0]));
			await pool.query(`UPDATE page_collab_updates SET created_at = created_at - INTERVAL '1 hour' WHERE page_id = $1`, [pageId]);
			expect(await persistence.compact(pageId, first.epoch)).not.toBeNull();
			const compacted = await persistence.loadOrSeed(pageId, seeder, fingerprint);
			expect(await persistence.renamedTaskTitles(pageId, compacted.contentAsOf)).toHaveLength(1);

			// An edit after the rename (the bullet typed in) does.
			await persistence.append(pageId, first.epoch, new Uint8Array([0, 0]));
			const edited = await persistence.loadOrSeed(pageId, seeder, fingerprint);
			expect(await persistence.renamedTaskTitles(pageId, edited.contentAsOf)).toEqual([]);
		});

		it('a rename stays owed to its bullet through later edits to the note (review)', async () => {
			// An append after the rename is an edit somewhere in the note, not
			// necessarily in this bullet: it doesn't settle the rename.
			const id = await addTask('n1', 'Buy milk');
			const first = await persistence.loadOrSeed(pageId, seeder, fingerprint);
			await pause();
			await rename(id, 'Buy oat milk');
			await pause();
			await persistence.append(pageId, first.epoch, new Uint8Array([0, 0]));
			const edited = await persistence.loadOrSeed(pageId, seeder, fingerprint);
			expect(await persistence.renamedTaskTitles(pageId, edited.contentAsOf)).toEqual([{ nodeId: 'n1', title: 'Buy oat milk' }]);
		});

		it('a schema reseed keeps the replaced log\'s content time', async () => {
			const id = await addTask('n1', 'Buy milk');
			const loaded = await persistence.loadOrSeed(pageId, seeder, 'old-build');
			await pool.query(`UPDATE page_collab_updates SET created_at = created_at - INTERVAL '1 hour' WHERE page_id = $1`, [pageId]);
			await rename(id, 'Buy oat milk');
			expect(await persistence.reseed(pageId, loaded.epoch, seedUpdate(schema, null), fingerprint)).toBe(loaded.epoch + 1);
			const after = await persistence.loadOrSeed(pageId, seeder, fingerprint);
			expect(await persistence.renamedTaskTitles(pageId, after.contentAsOf)).toHaveLength(1);
		});

		it('finds nothing, without failing, before the column exists (migration not run)', async () => {
			await rename(await addTask('n1', 'Buy milk'), 'Buy oat milk');
			await pool.query(`ALTER TABLE tasks RENAME COLUMN title_renamed_at TO title_renamed_at_hidden`);
			try {
				expect(await persistence.renamedTaskTitles(pageId, null)).toEqual([]);
			} finally {
				await pool.query(`ALTER TABLE tasks RENAME COLUMN title_renamed_at_hidden TO title_renamed_at`);
			}
		});
	});

	it('quarantine is visible in state and cleared by the next epoch', async () => {
		const loaded = await persistence.loadOrSeed(pageId, seeder, fingerprint);
		await persistence.quarantine(pageId, loaded.epoch, 'test');
		expect((await persistence.getState(pageId))?.quarantined).toBe(true);
		await pool.query(`UPDATE page_collab_docs SET attached = false WHERE page_id = $1`, [pageId]);
		await persistence.loadOrSeed(pageId, seeder, fingerprint);
		expect((await persistence.getState(pageId))?.quarantined).toBe(false);
	});
});
