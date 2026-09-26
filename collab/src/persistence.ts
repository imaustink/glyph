/**
 * Storage for shared documents: an append-only Yjs update log per page and
 * epoch (see api/migrations/000019_page_collab.up.sql).
 *
 * Invariants this module maintains:
 *
 *  - Seeding happens exactly once per epoch, in one transaction that holds
 *    the page row lock (the same lock REST content writes take). Two replicas
 *    or two simultaneous first visitors can't each seed — which would
 *    duplicate the whole page when their states merged — and a REST write
 *    can't land between reading page_contents and attaching the page.
 *
 *  - Updates are only ever appended. Compaction replaces exactly the rows it
 *    merged, in one transaction, so a concurrent append is never lost.
 *
 *  - A page's seqs become visible in seq order. BIGSERIAL hands out a seq
 *    when the row is inserted, not when it commits, so two unserialised
 *    appends can commit out of order — and a reader that has already moved
 *    past the later seq (fetchSince is strictly seq > afterSeq) skips the
 *    earlier one forever. Every writer of page_collab_updates therefore
 *    takes the page's page_collab_docs row lock *before* its INSERT draws a
 *    seq, and holds it to commit: the next writer can only draw a seq after
 *    the previous one is visible. (Collab pods from before this rule append
 *    without the lock; during a rollout that overlaps them the old race
 *    remains, and it is gone once they are.)
 *
 *  - A schema re-seed never replaces an epoch another replica has loaded
 *    (api/migrations/000024_page_collab_leases.up.sql). Loading takes a lease
 *    under the page_collab_docs row lock; re-seeding checks for other
 *    holders' unexpired leases under the same lock.
 */
import * as Y from 'yjs';
import pg from 'pg';

export interface StoredUpdate {
	seq: number;
	data: Uint8Array;
}

export interface CollabDocState {
	epoch: number;
	attached: boolean;
	quarantined: boolean;
	schemaFingerprint: string | null;
}

export interface LoadedDoc {
	epoch: number;
	quarantined: boolean;
	/** Fingerprint of the schema the log was written under. */
	schemaFingerprint: string | null;
	/** True if this call created the epoch (seeded from page_contents). */
	seeded: boolean;
	updates: StoredUpdate[];
}

export interface StoredPageContent {
	content: unknown;
	schemaVersion: number;
}

/**
 * This replica's claim on the documents it has loaded: `holder` is unique per
 * process, and a lease not renewed within `ttlMs` expires.
 */
export interface Lease {
	holder: string;
	ttlMs: number;
}

/** Builds the first update of a new epoch from the page's stored content (null = no content yet). */
export type Seeder = (content: StoredPageContent | null) => Uint8Array;

export interface Persistence {
	/** Current collab state for a page, or null if it has never been collaborative. */
	getState(pageId: string): Promise<CollabDocState | null>;
	/**
	 * Load the page's shared document, seeding a new epoch from page_contents
	 * if the page isn't attached. Throws NotFoundError if the page is gone.
	 * With `lease`, the caller holds the loaded epoch from then on — unless
	 * the log's fingerprint differs from `schemaFingerprint`, in which case
	 * the caller is about to reseed() and takes its lease there.
	 */
	loadOrSeed(pageId: string, seed: Seeder, schemaFingerprint: string, lease?: Lease): Promise<LoadedDoc>;
	/**
	 * Replace an attached document's log with a fresh epoch built from the
	 * given update (used when the stored log predates the current schema).
	 * Returns the new epoch (held by `lease`, if given), null if `fromEpoch`
	 * is no longer current, or 'held' if another replica holds an unexpired
	 * lease on `fromEpoch` — it may have edits not yet in the log, which a
	 * new epoch would make it unable to append.
	 */
	reseed(pageId: string, fromEpoch: number, update: Uint8Array, schemaFingerprint: string, lease?: Lease): Promise<number | 'held' | null>;
	/** Extend (or re-take) `holder`'s leases on the given loaded copies. */
	renewLeases(holder: string, held: { pageId: string; epoch: number }[], ttlMs: number): Promise<void>;
	/** Give up `holder`'s lease on the page's `epoch` (its copy unloaded). */
	releaseLease(pageId: string, holder: string, epoch: number): Promise<void>;
	/** Append an update. Returns its seq, or null if the epoch is no longer current and attached. */
	append(pageId: string, epoch: number, data: Uint8Array): Promise<number | null>;
	/**
	 * Append an update built from the latest log, holding the page's log
	 * lock from reading it to committing: `build` gets the epoch's rows after
	 * `afterSeq` (to apply before deciding) and returns the update to append,
	 * or null for none. Two replicas making the same server edit therefore
	 * run one after the other, and the second sees the first's row — so an
	 * edit that must happen once (a task title put into its bullet: made
	 * twice, the text would merge into a duplicate) happens once. Returns the
	 * appended seq, null if `build` returned null, or 'stale' (build not
	 * called) if the epoch is no longer current and attached.
	 */
	appendExclusive(
		pageId: string,
		epoch: number,
		afterSeq: number,
		build: (rows: StoredUpdate[]) => Uint8Array | null
	): Promise<number | null | 'stale'>;
	/** Updates in the epoch with seq > afterSeq, in seq order. */
	fetchSince(pageId: string, epoch: number, afterSeq: number): Promise<StoredUpdate[]>;
	/** Merge the epoch's log into one row. Returns the merged row's seq, or null if nothing to do. */
	compact(pageId: string, epoch: number): Promise<number | null>;
	/** Mark the page quarantined (read-only for collaborators) in the given epoch. */
	quarantine(pageId: string, epoch: number, reason: string): Promise<void>;
	close(): Promise<void>;
}

export class NotFoundError extends Error {
	constructor(message = 'page not found') {
		super(message);
		this.name = 'NotFoundError';
	}
}

const UPSERT_LEASE = `
	INSERT INTO page_collab_leases (page_id, holder, epoch, expires_at)
	VALUES ($1, $2, $3, NOW() + $4 * INTERVAL '1 millisecond')
	ON CONFLICT (page_id, holder) DO UPDATE SET epoch = EXCLUDED.epoch, expires_at = EXCLUDED.expires_at`;

export class PgPersistence implements Persistence {
	/**
	 * Whether page_collab_leases exists. The migrate Job and a collab rollout
	 * can race, so a new build may start against a database that doesn't
	 * have the table yet: until it does, leasing is skipped (re-seeding
	 * behaves as it did before leases). Re-checked at most every 30 s.
	 */
	private leases: { available: boolean; checkedAt: number } | null = null;

	constructor(private readonly pool: pg.Pool) {}

	private async leasesAvailable(c: pg.PoolClient | pg.Pool = this.pool): Promise<boolean> {
		if (this.leases && (this.leases.available || Date.now() - this.leases.checkedAt < 30000)) return this.leases.available;
		const { rows } = await c.query(`SELECT to_regclass('page_collab_leases') IS NOT NULL AS ok`);
		this.leases = { available: rows[0].ok, checkedAt: Date.now() };
		return this.leases.available;
	}

	static fromUrl(url: string): PgPersistence {
		return new PgPersistence(new pg.Pool({ connectionString: url, max: 10 }));
	}

	private async tx<T>(fn: (client: pg.PoolClient) => Promise<T>): Promise<T> {
		const client = await this.pool.connect();
		try {
			await client.query('BEGIN');
			const out = await fn(client);
			await client.query('COMMIT');
			return out;
		} catch (err) {
			await client.query('ROLLBACK').catch(() => {});
			throw err;
		} finally {
			client.release();
		}
	}

	async getState(pageId: string): Promise<CollabDocState | null> {
		const { rows } = await this.pool.query(
			`SELECT epoch, attached, quarantined_at IS NOT NULL AS quarantined, schema_fingerprint
			 FROM page_collab_docs WHERE page_id = $1`,
			[pageId]
		);
		if (rows.length === 0) return null;
		return {
			epoch: rows[0].epoch,
			attached: rows[0].attached,
			quarantined: rows[0].quarantined,
			schemaFingerprint: rows[0].schema_fingerprint
		};
	}

	async loadOrSeed(pageId: string, seed: Seeder, schemaFingerprint: string, lease?: Lease): Promise<LoadedDoc> {
		return this.tx(async (c) => {
			// Same lock as the API's content writes: serialises seeding against
			// REST writes and against other replicas seeding the same page.
			const page = await c.query(`SELECT id FROM pages WHERE id = $1 AND type = 'page' FOR UPDATE`, [pageId]);
			if (page.rowCount === 0) throw new NotFoundError();

			await c.query(`INSERT INTO page_collab_docs (page_id) VALUES ($1) ON CONFLICT DO NOTHING`, [pageId]);
			const { rows } = await c.query(
				`SELECT epoch, attached, quarantined_at IS NOT NULL AS quarantined, schema_fingerprint
				 FROM page_collab_docs WHERE page_id = $1 FOR UPDATE`,
				[pageId]
			);
			const st = rows[0];

			if (st.attached) {
				const updates = await c.query(
					`SELECT seq, data FROM page_collab_updates WHERE page_id = $1 AND epoch = $2 ORDER BY seq`,
					[pageId, st.epoch]
				);
				// Held from inside the row lock, so a reseed either sees this
				// lease or ran first (and we loaded its new epoch).
				if (lease && st.schema_fingerprint === schemaFingerprint && (await this.leasesAvailable(c))) {
					await c.query(UPSERT_LEASE, [pageId, lease.holder, st.epoch, lease.ttlMs]);
				}
				return {
					epoch: st.epoch,
					quarantined: st.quarantined,
					schemaFingerprint: st.schema_fingerprint,
					seeded: false,
					updates: updates.rows.map(toStoredUpdate)
				};
			}

			const content = await c.query(
				`SELECT content, schema_version FROM page_contents WHERE page_id = $1`,
				[pageId]
			);
			const initial = seed(
				content.rowCount ? { content: content.rows[0].content, schemaVersion: content.rows[0].schema_version } : null
			);
			const epoch = st.epoch + 1;
			const seq = await this.startEpoch(c, pageId, epoch, initial, schemaFingerprint);
			if (lease && (await this.leasesAvailable(c))) await c.query(UPSERT_LEASE, [pageId, lease.holder, epoch, lease.ttlMs]);
			return { epoch, quarantined: false, schemaFingerprint, seeded: true, updates: [{ seq, data: initial }] };
		});
	}

	async reseed(
		pageId: string,
		fromEpoch: number,
		update: Uint8Array,
		schemaFingerprint: string,
		lease?: Lease
	): Promise<number | 'held' | null> {
		return this.tx(async (c) => {
			await c.query(`SELECT id FROM pages WHERE id = $1 FOR UPDATE`, [pageId]);
			const { rows } = await c.query(
				`SELECT epoch, attached FROM page_collab_docs WHERE page_id = $1 FOR UPDATE`,
				[pageId]
			);
			if (rows.length === 0 || !rows[0].attached || rows[0].epoch !== fromEpoch) return null;
			const leasing = await this.leasesAvailable(c);
			if (leasing) {
				const held = await c.query(
					`SELECT 1 FROM page_collab_leases
					 WHERE page_id = $1 AND epoch = $2 AND holder <> $3 AND expires_at > NOW() LIMIT 1`,
					[pageId, fromEpoch, lease?.holder ?? '']
				);
				if (held.rowCount) return 'held';
			}
			const epoch = fromEpoch + 1;
			await this.startEpoch(c, pageId, epoch, update, schemaFingerprint);
			if (leasing && lease) await c.query(UPSERT_LEASE, [pageId, lease.holder, epoch, lease.ttlMs]);
			return epoch;
		});
	}

	async renewLeases(holder: string, held: { pageId: string; epoch: number }[], ttlMs: number): Promise<void> {
		if (!(await this.leasesAvailable())) return;
		if (held.length > 0) {
			// An upsert, not an UPDATE: it also re-takes a lease that a racing
			// release (of an earlier copy of the same page) deleted. Pages
			// deleted meanwhile are skipped rather than failing the batch on
			// the foreign key.
			await this.pool.query(
				`INSERT INTO page_collab_leases (page_id, holder, epoch, expires_at)
				 SELECT t.page_id, $1, t.epoch, NOW() + $4 * INTERVAL '1 millisecond'
				 FROM unnest($2::uuid[], $3::int[]) AS t(page_id, epoch)
				 WHERE EXISTS (SELECT 1 FROM pages WHERE id = t.page_id)
				 ON CONFLICT (page_id, holder) DO UPDATE SET epoch = EXCLUDED.epoch, expires_at = EXCLUDED.expires_at`,
				[holder, held.map((h) => h.pageId), held.map((h) => h.epoch), ttlMs]
			);
		}
		// Leases of replicas that died without releasing them. Expired ones
		// already don't count; this only keeps the table small.
		await this.pool.query(`DELETE FROM page_collab_leases WHERE expires_at < NOW() - INTERVAL '1 hour'`);
	}

	async releaseLease(pageId: string, holder: string, epoch: number): Promise<void> {
		if (!(await this.leasesAvailable())) return;
		await this.pool.query(`DELETE FROM page_collab_leases WHERE page_id = $1 AND holder = $2 AND epoch = $3`, [pageId, holder, epoch]);
	}

	/** Replace the log with a single initial update under a new epoch. Caller holds the locks. */
	private async startEpoch(c: pg.PoolClient, pageId: string, epoch: number, initial: Uint8Array, fingerprint: string): Promise<number> {
		// Earlier epochs are dead: no client may ever sync into them again, and
		// their content is preserved in page_content_versions.
		await c.query(`DELETE FROM page_collab_updates WHERE page_id = $1`, [pageId]);
		const ins = await c.query(
			`INSERT INTO page_collab_updates (page_id, epoch, data) VALUES ($1, $2, $3) RETURNING seq`,
			[pageId, epoch, Buffer.from(initial)]
		);
		await c.query(
			`UPDATE page_collab_docs
			 SET epoch = $2, attached = true, schema_fingerprint = $3, snapshot_seq = 0,
			     quarantined_at = NULL, quarantine_reason = NULL, updated_at = NOW()
			 WHERE page_id = $1`,
			[pageId, epoch, fingerprint]
		);
		return Number(ins.rows[0].seq);
	}

	async append(pageId: string, epoch: number, data: Uint8Array): Promise<number | null> {
		// Conditional on the epoch still being live, so a replica holding a
		// replaced document finds out on its next write. The CTE locks the
		// page_collab_docs row before the INSERT draws its seq, and the lock is
		// held until this (single, autocommitted) statement commits — so seqs
		// for a page commit in seq order (see the header).
		const { rows } = await this.pool.query(
			`WITH live AS (
			   SELECT 1 FROM page_collab_docs WHERE page_id = $1 AND epoch = $2 AND attached FOR UPDATE
			 )
			 INSERT INTO page_collab_updates (page_id, epoch, data)
			 SELECT $1, $2, $3 FROM live
			 RETURNING seq`,
			[pageId, epoch, Buffer.from(data)]
		);
		return rows.length ? Number(rows[0].seq) : null;
	}

	async appendExclusive(
		pageId: string,
		epoch: number,
		afterSeq: number,
		build: (rows: StoredUpdate[]) => Uint8Array | null
	): Promise<number | null | 'stale'> {
		return this.tx(async (c) => {
			// The seq-order lock (see the header), held to commit.
			const live = await c.query(
				`SELECT 1 FROM page_collab_docs WHERE page_id = $1 AND epoch = $2 AND attached FOR UPDATE`,
				[pageId, epoch]
			);
			if (live.rowCount === 0) return 'stale';
			const { rows } = await c.query(
				`SELECT seq, data FROM page_collab_updates WHERE page_id = $1 AND epoch = $2 AND seq > $3 ORDER BY seq`,
				[pageId, epoch, afterSeq]
			);
			const update = build(rows.map(toStoredUpdate));
			if (!update) return null;
			const ins = await c.query(
				`INSERT INTO page_collab_updates (page_id, epoch, data) VALUES ($1, $2, $3) RETURNING seq`,
				[pageId, epoch, Buffer.from(update)]
			);
			return Number(ins.rows[0].seq);
		});
	}

	async fetchSince(pageId: string, epoch: number, afterSeq: number): Promise<StoredUpdate[]> {
		const { rows } = await this.pool.query(
			`SELECT seq, data FROM page_collab_updates WHERE page_id = $1 AND epoch = $2 AND seq > $3 ORDER BY seq`,
			[pageId, epoch, afterSeq]
		);
		return rows.map(toStoredUpdate);
	}

	async compact(pageId: string, epoch: number): Promise<number | null> {
		return this.tx(async (c) => {
			// The merged row draws a new seq: take the seq-order lock first,
			// like append (see the header). Appends wait for the compaction,
			// and every append that committed before it is in the SELECT below.
			await c.query(`SELECT 1 FROM page_collab_docs WHERE page_id = $1 FOR UPDATE`, [pageId]);
			const { rows } = await c.query(
				`SELECT seq, data FROM page_collab_updates WHERE page_id = $1 AND epoch = $2 ORDER BY seq FOR UPDATE`,
				[pageId, epoch]
			);
			if (rows.length < 2) return null;
			const merged = Y.mergeUpdates(rows.map((r) => new Uint8Array(r.data)));
			const ins = await c.query(
				`INSERT INTO page_collab_updates (page_id, epoch, data) VALUES ($1, $2, $3) RETURNING seq`,
				[pageId, epoch, Buffer.from(merged)]
			);
			// Delete exactly the rows merged — never a range, which could catch
			// a concurrent append that committed after our SELECT.
			await c.query(`DELETE FROM page_collab_updates WHERE seq = ANY($1::bigint[])`, [rows.map((r) => r.seq)]);
			return Number(ins.rows[0].seq);
		});
	}

	async quarantine(pageId: string, epoch: number, reason: string): Promise<void> {
		await this.pool.query(
			`UPDATE page_collab_docs SET quarantined_at = NOW(), quarantine_reason = $3, updated_at = NOW()
			 WHERE page_id = $1 AND epoch = $2 AND quarantined_at IS NULL`,
			[pageId, epoch, reason.slice(0, 1000)]
		);
	}

	async close(): Promise<void> {
		await this.pool.end();
	}
}

function toStoredUpdate(row: { seq: string | number; data: Buffer }): StoredUpdate {
	return { seq: Number(row.seq), data: new Uint8Array(row.data) };
}
