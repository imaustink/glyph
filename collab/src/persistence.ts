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
 *
 *  - A log row's created_at is when the newest content it holds was
 *    written, so the log's newest created_at says how recent the document
 *    is (DI-29: before migration 000026, a task renamed after that is put
 *    into its bullet on load; see the next point for the rule since).
 *    An appended row is stamped when appended; the row that starts an epoch
 *    carries the time of what it was built from (page_contents.updated_at
 *    for a seed, the replaced log's newest time for a schema re-seed); a
 *    compacted row the newest time of the rows it merged. (Collab builds
 *    from before this stamp seeds and compactions with NOW(), which only
 *    makes an older rename look already applied.)
 *
 *  - A task rename made outside the note (tasks.title_renamed_at) is owed
 *    to its bullet until tasks.title_applied_at catches up with it (DI-29,
 *    api/migrations/000026_task_title_applied_at.up.sql). That is set in
 *    the transaction of the exclusive append that put the title into the
 *    bullet (or found it there), and in the seeding transaction for renames
 *    older than the stored content the epoch is built from. Per task: the
 *    log's newest write says when the note last changed, not which bullet.
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
	/**
	 * When the newest content in `updates` was written (see the header), as
	 * a database timestamp: opaque, only ever passed back to
	 * renamedTaskTitles, which compares it in the database at full precision.
	 */
	contentAsOf: string | null;
}

/** A note task's title, to show in the bullet with this nodeId. */
export interface TaskTitle {
	nodeId: string;
	title: string;
}

/** What an appendExclusive build decided. */
export interface ExclusiveEdit {
	/** The update to append, or null for none. */
	update: Uint8Array | null;
	/**
	 * Task titles their bullets show once `update` is in (whether or not it
	 * changed them). Recorded as applied (tasks.title_applied_at) in the same
	 * transaction, for tasks whose title is still this one.
	 */
	titlesShown?: TaskTitle[];
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
	 * `afterSeq` (to apply before deciding) and returns the update to append
	 * (or null for none) and the task titles it leaves showing. Two replicas making the same server edit therefore
	 * run one after the other, and the second sees the first's row — so an
	 * edit that must happen once (a task title put into its bullet: made
	 * twice, the text would merge into a duplicate) happens once. Returns the
	 * appended seq, null if `build` returned no update, or 'stale' (build not
	 * called) if the epoch is no longer current and attached.
	 */
	appendExclusive(
		pageId: string,
		epoch: number,
		afterSeq: number,
		build: (rows: StoredUpdate[]) => ExclusiveEdit
	): Promise<number | null | 'stale'>;
	/** Updates in the epoch with seq > afterSeq, in seq order. */
	fetchSince(pageId: string, epoch: number, afterSeq: number): Promise<StoredUpdate[]>;
	/** Merge the epoch's log into one row. Returns the merged row's seq, or null if nothing to do. */
	compact(pageId: string, epoch: number): Promise<number | null>;
	/**
	 * The page's live tasks renamed outside the editor (tasks.title_renamed_at)
	 * whose bullets are still owed the rename: not yet recorded as applied
	 * (tasks.title_applied_at, see ExclusiveEdit.titlesShown), nor older than
	 * the stored content the epoch was seeded from. While the database
	 * predates title_applied_at, renames after `since` (a
	 * LoadedDoc.contentAsOf; null = any time) instead. Empty while it predates
	 * title_renamed_at.
	 */
	renamedTaskTitles(pageId: string, since: string | null): Promise<TaskTitle[]>;
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

/** Postgres SQLSTATE undefined_column. */
const UNDEFINED_COLUMN = '42703';

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
	private leases: SchemaCheck = { available: false, checkedAt: -Infinity };
	/** Likewise tasks.title_applied_at (migration 000026): until it exists, renames are compared with the log's newest write. */
	private titleMarkers: SchemaCheck = { available: false, checkedAt: -Infinity };

	constructor(private readonly pool: pg.Pool) {}

	private leasesAvailable(c: pg.PoolClient | pg.Pool = this.pool): Promise<boolean> {
		return schemaHas(this.leases, c, `SELECT to_regclass('page_collab_leases') IS NOT NULL AS ok`);
	}

	private titleMarkersAvailable(c: pg.PoolClient | pg.Pool = this.pool): Promise<boolean> {
		return schemaHas(
			this.titleMarkers,
			c,
			`SELECT EXISTS (SELECT 1 FROM pg_attribute
			   WHERE attrelid = to_regclass('tasks') AND attname = 'title_applied_at' AND NOT attisdropped) AS ok`
		);
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
					updates: updates.rows.map(toStoredUpdate),
					contentAsOf: await this.contentAsOf(c, pageId, st.epoch)
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
			const seq = await this.startEpoch(c, pageId, epoch, initial, schemaFingerprint, 'page_contents');
			if (lease && (await this.leasesAvailable(c))) await c.query(UPSERT_LEASE, [pageId, lease.holder, epoch, lease.ttlMs]);
			return {
				epoch,
				quarantined: false,
				schemaFingerprint,
				seeded: true,
				updates: [{ seq, data: initial }],
				contentAsOf: await this.contentAsOf(c, pageId, epoch)
			};
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
			await this.startEpoch(c, pageId, epoch, update, schemaFingerprint, 'log');
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

	/**
	 * Replace the log with a single initial update under a new epoch, stamped
	 * with the time of the content it was built from (see the header): the
	 * stored page content, or the log it replaces. Caller holds the locks.
	 */
	private async startEpoch(
		c: pg.PoolClient,
		pageId: string,
		epoch: number,
		initial: Uint8Array,
		fingerprint: string,
		builtFrom: 'page_contents' | 'log'
	): Promise<number> {
		const contentTime =
			builtFrom === 'page_contents'
				? `(SELECT updated_at FROM page_contents WHERE page_id = $1)`
				: `(SELECT max(created_at) FROM page_collab_updates WHERE page_id = $1)`;
		const ins = await c.query(
			`INSERT INTO page_collab_updates (page_id, epoch, data, created_at)
			 VALUES ($1, $2, $3, COALESCE(${contentTime}, NOW())) RETURNING seq`,
			[pageId, epoch, Buffer.from(initial)]
		);
		// Stored content written after a rename is a whole-document write
		// newer than it (a version restore, a REST save): the rename is
		// settled. A schema re-seed carries the replaced log's bullets over
		// as they were, and whatever they were owed with them.
		if (builtFrom === 'page_contents' && (await this.titleMarkersAvailable(c))) {
			await c.query(
				`UPDATE tasks SET title_applied_at = title_renamed_at
				 WHERE source_page_id = $1 AND title_renamed_at IS NOT NULL
				   AND title_renamed_at <= (SELECT updated_at FROM page_contents WHERE page_id = $1)
				   AND (title_applied_at IS NULL OR title_applied_at < title_renamed_at)`,
				[pageId]
			);
		}
		// Earlier epochs are dead: no client may ever sync into them again, and
		// their content is preserved in page_content_versions.
		await c.query(`DELETE FROM page_collab_updates WHERE page_id = $1 AND seq <> $2`, [pageId, ins.rows[0].seq]);
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
		build: (rows: StoredUpdate[]) => ExclusiveEdit
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
			const { update, titlesShown = [] } = build(rows.map(toStoredUpdate));
			let seq: number | null = null;
			if (update) {
				const ins = await c.query(
					`INSERT INTO page_collab_updates (page_id, epoch, data) VALUES ($1, $2, $3) RETURNING seq`,
					[pageId, epoch, Buffer.from(update)]
				);
				seq = Number(ins.rows[0].seq);
			}
			// Only a task whose title is still the one shown: a rename to
			// something else since is still owed.
			if (titlesShown.length > 0 && (await this.titleMarkersAvailable(c))) {
				await c.query(
					`UPDATE tasks t SET title_applied_at = t.title_renamed_at
					 FROM unnest($2::text[], $3::text[]) AS s(node_id, title)
					 WHERE t.source_page_id = $1 AND t.source_node_id = s.node_id AND t.title = s.title
					   AND t.deleted_at IS NULL AND t.title_renamed_at IS NOT NULL
					   AND (t.title_applied_at IS NULL OR t.title_applied_at < t.title_renamed_at)`,
					[pageId, titlesShown.map((t) => t.nodeId), titlesShown.map((t) => t.title)]
				);
			}
			return seq;
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
			// Stamped with the newest merged row's time: compacting is not
			// writing content (see the header).
			const ins = await c.query(
				`INSERT INTO page_collab_updates (page_id, epoch, data, created_at)
				 SELECT $1, $2, $3, max(created_at) FROM page_collab_updates WHERE seq = ANY($4::bigint[])
				 RETURNING seq`,
				[pageId, epoch, Buffer.from(merged), rows.map((r) => r.seq)]
			);
			// Delete exactly the rows merged — never a range, which could catch
			// a concurrent append that committed after our SELECT.
			await c.query(`DELETE FROM page_collab_updates WHERE seq = ANY($1::bigint[])`, [rows.map((r) => r.seq)]);
			return Number(ins.rows[0].seq);
		});
	}

	/** The newest created_at of the epoch's log, as text (full precision; see LoadedDoc.contentAsOf). */
	private async contentAsOf(c: pg.PoolClient, pageId: string, epoch: number): Promise<string | null> {
		const { rows } = await c.query(
			`SELECT max(created_at)::text AS at FROM page_collab_updates WHERE page_id = $1 AND epoch = $2`,
			[pageId, epoch]
		);
		return rows[0]?.at ?? null;
	}

	async renamedTaskTitles(pageId: string, since: string | null): Promise<TaskTitle[]> {
		try {
			const [owed, params] = (await this.titleMarkersAvailable())
				? [`(title_applied_at IS NULL OR title_applied_at < title_renamed_at)`, [pageId]]
				: [`($2::timestamptz IS NULL OR title_renamed_at > $2::timestamptz)`, [pageId, since]];
			const { rows } = await this.pool.query(
				`SELECT source_node_id, title FROM tasks
				 WHERE source_page_id = $1 AND source_node_id IS NOT NULL AND deleted_at IS NULL
				   AND title_renamed_at IS NOT NULL AND ${owed}
				 ORDER BY title_renamed_at`,
				params
			);
			return rows.map((r) => ({ nodeId: r.source_node_id, title: r.title }));
		} catch (err) {
			// The API's migration adding the column hasn't run yet (rollouts can
			// start this build first): nothing can have been recorded.
			if ((err as { code?: string }).code === UNDEFINED_COLUMN) return [];
			throw err;
		}
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

/** A cached check that part of the schema exists (see PgPersistence.leases). */
interface SchemaCheck {
	available: boolean;
	checkedAt: number;
}

/** Once there, it stays there; missing, it is re-checked at most every 30 s. */
async function schemaHas(check: SchemaCheck, c: pg.PoolClient | pg.Pool, sql: string): Promise<boolean> {
	if (check.available || Date.now() - check.checkedAt < 30000) return check.available;
	const { rows } = await c.query(sql);
	check.available = rows[0].ok;
	check.checkedAt = Date.now();
	return check.available;
}

function toStoredUpdate(row: { seq: string | number; data: Buffer }): StoredUpdate {
	return { seq: Number(row.seq), data: new Uint8Array(row.data) };
}
