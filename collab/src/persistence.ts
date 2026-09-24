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

/** Builds the first update of a new epoch from the page's stored content (null = no content yet). */
export type Seeder = (content: StoredPageContent | null) => Uint8Array;

export interface Persistence {
	/** Current collab state for a page, or null if it has never been collaborative. */
	getState(pageId: string): Promise<CollabDocState | null>;
	/**
	 * Load the page's shared document, seeding a new epoch from page_contents
	 * if the page isn't attached. Throws NotFoundError if the page is gone.
	 */
	loadOrSeed(pageId: string, seed: Seeder, schemaFingerprint: string): Promise<LoadedDoc>;
	/**
	 * Replace an attached document's log with a fresh epoch built from the
	 * given update (used when the stored log predates the current schema).
	 * Returns the new epoch, or null if `fromEpoch` is no longer current.
	 */
	reseed(pageId: string, fromEpoch: number, update: Uint8Array, schemaFingerprint: string): Promise<number | null>;
	/** Append an update. Returns its seq, or null if the epoch is no longer current and attached. */
	append(pageId: string, epoch: number, data: Uint8Array): Promise<number | null>;
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

export class PgPersistence implements Persistence {
	constructor(private readonly pool: pg.Pool) {}

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

	async loadOrSeed(pageId: string, seed: Seeder, schemaFingerprint: string): Promise<LoadedDoc> {
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
			return { epoch, quarantined: false, schemaFingerprint, seeded: true, updates: [{ seq, data: initial }] };
		});
	}

	async reseed(pageId: string, fromEpoch: number, update: Uint8Array, schemaFingerprint: string): Promise<number | null> {
		return this.tx(async (c) => {
			await c.query(`SELECT id FROM pages WHERE id = $1 FOR UPDATE`, [pageId]);
			const { rows } = await c.query(
				`SELECT epoch, attached FROM page_collab_docs WHERE page_id = $1 FOR UPDATE`,
				[pageId]
			);
			if (rows.length === 0 || !rows[0].attached || rows[0].epoch !== fromEpoch) return null;
			const epoch = fromEpoch + 1;
			await this.startEpoch(c, pageId, epoch, update, schemaFingerprint);
			return epoch;
		});
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
		// replaced document finds out on its next write.
		const { rows } = await this.pool.query(
			`INSERT INTO page_collab_updates (page_id, epoch, data)
			 SELECT $1, $2, $3
			 WHERE EXISTS (SELECT 1 FROM page_collab_docs WHERE page_id = $1 AND epoch = $2 AND attached)
			 RETURNING seq`,
			[pageId, epoch, Buffer.from(data)]
		);
		return rows.length ? Number(rows[0].seq) : null;
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
