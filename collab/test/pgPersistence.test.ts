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

	it('reseed only succeeds from the current epoch', async () => {
		const loaded = await persistence.loadOrSeed(pageId, seeder, fingerprint);
		const update = seedUpdate(schema, { type: 'doc', content: [{ type: 'paragraph', content: [{ type: 'text', text: 'v2' }] }] });
		expect(await persistence.reseed(pageId, loaded.epoch + 5, update, fingerprint)).toBeNull();
		const next = await persistence.reseed(pageId, loaded.epoch, update, fingerprint);
		expect(next).toBe(loaded.epoch + 1);
		expect(await persistence.reseed(pageId, loaded.epoch, update, fingerprint)).toBeNull();
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
