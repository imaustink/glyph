/**
 * In-memory Persistence with the same semantics as PgPersistence, for tests.
 * A per-page mutex stands in for the page row lock, and `seq` is a global
 * counter like the BIGSERIAL.
 */
import * as Y from 'yjs';
import type { CollabDocState, ExclusiveEdit, Lease, LoadedDoc, Persistence, Seeder, StoredUpdate, TaskTitle } from '../../src/persistence.js';
import { NotFoundError } from '../../src/persistence.js';

interface Row extends StoredUpdate {
	pageId: string;
	epoch: number;
	/** created_at: when the newest content in the row was written (logical clock). */
	at: number;
}

interface DocRow {
	epoch: number;
	attached: boolean;
	quarantined: boolean;
	quarantineReason: string | null;
	schemaFingerprint: string | null;
}

export class MemoryPersistence implements Persistence {
	pages = new Map<string, { content: unknown; schemaVersion: number } | null>();
	/** page_contents.updated_at (logical clock). */
	contentAt = new Map<string, number>();
	docs = new Map<string, DocRow>();
	rows: Row[] = [];
	seedCount = 0;
	/** Set to make the next N appends throw (simulating a database outage). */
	failAppends = 0;
	/** Test hook: runs at the start of a compaction, before rows are merged. */
	onCompact: ((pageId: string, epoch: number) => void) | null = null;
	/**
	 * Test hook: awaited at the start of every append, before the outage check
	 * and the epoch check. Lets a test hold an append in flight (to reopen a
	 * note meanwhile) or inject a foreign row between catch-up and append.
	 */
	beforeAppend: ((pageId: string, epoch: number) => void | Promise<void>) | null = null;
	/** page_collab_leases: `${pageId} ${holder}` → lease. */
	leases = new Map<string, { pageId: string; holder: string; epoch: number; expiresAt: number }>();
	/**
	 * Note tasks renamed outside the editor (tasks.title_renamed_at), stamped
	 * with the logical clock below, and the rename their bullet has caught up
	 * with (tasks.title_applied_at).
	 */
	renamedTasks: { pageId: string; nodeId: string; title: string; renamedAt: number; appliedAt?: number }[] = [];
	/** Set to make reading renamed task titles throw. */
	failRenamedTitles = false;
	private seq = 0;
	/** A logical clock standing in for the database's NOW(). */
	private clock = 0;
	private locks = new Map<string, Promise<void>>();

	/** The next moment of the logical clock. */
	tick(): number {
		return ++this.clock;
	}

	/** What PATCH /tasks/:id does for a rename from outside the note. */
	renameTask(pageId: string, nodeId: string, title: string) {
		this.renamedTasks = this.renamedTasks.filter((t) => !(t.pageId === pageId && t.nodeId === nodeId));
		this.renamedTasks.push({ pageId, nodeId, title, renamedAt: this.tick() });
	}

	/**
	 * What PATCH /tasks/:id with `X-Glyph-Change-Source: bullet` does: the
	 * editor gave the task its bullet's text. Not a rename (title_renamed_at
	 * is left as it is).
	 */
	titleFromBullet(pageId: string, nodeId: string, title: string) {
		for (const t of this.renamedTasks) if (t.pageId === pageId && t.nodeId === nodeId) t.title = title;
	}

	private takeLease(pageId: string, lease: Lease | undefined, epoch: number) {
		if (lease) this.leases.set(`${pageId} ${lease.holder}`, { pageId, holder: lease.holder, epoch, expiresAt: Date.now() + lease.ttlMs });
	}

	/** Test helper: append a row directly, as if a foreign replica had. */
	injectRow(pageId: string, epoch: number, data: Uint8Array): number {
		const seq = ++this.seq;
		this.rows.push({ pageId, epoch, seq, data, at: this.tick() });
		return seq;
	}

	/** Create a page, optionally with stored content. */
	addPage(pageId: string, content: unknown = null, schemaVersion = 1) {
		this.pages.set(pageId, null);
		if (content !== null) this.writeContent(pageId, content, schemaVersion);
	}

	/** Write page_contents, as the API does (REST save, snapshot, restore). */
	writeContent(pageId: string, content: unknown, schemaVersion = 1) {
		this.pages.set(pageId, { content, schemaVersion });
		this.contentAt.set(pageId, this.tick());
	}

	/** What the API does on a version restore / REST write while disabled. */
	detach(pageId: string, content?: unknown) {
		const d = this.docs.get(pageId);
		if (d) d.attached = false;
		if (content !== undefined) this.writeContent(pageId, content);
	}

	private async locked<T>(pageId: string, fn: () => T | Promise<T>): Promise<T> {
		const prev = this.locks.get(pageId) ?? Promise.resolve();
		let release!: () => void;
		const next = new Promise<void>((r) => (release = r));
		this.locks.set(pageId, prev.then(() => next));
		await prev;
		try {
			// Yield so concurrent callers genuinely interleave around the lock.
			await new Promise((r) => setTimeout(r, 1));
			return await fn();
		} finally {
			release();
		}
	}

	async getState(pageId: string): Promise<CollabDocState | null> {
		const d = this.docs.get(pageId);
		return d ? { epoch: d.epoch, attached: d.attached, quarantined: d.quarantined, schemaFingerprint: d.schemaFingerprint } : null;
	}

	loadOrSeed(pageId: string, seed: Seeder, fingerprint: string, lease?: Lease): Promise<LoadedDoc> {
		return this.locked(pageId, () => {
			if (!this.pages.has(pageId)) throw new NotFoundError();
			let d = this.docs.get(pageId);
			if (!d) {
				d = { epoch: 0, attached: false, quarantined: false, quarantineReason: null, schemaFingerprint: null };
				this.docs.set(pageId, d);
			}
			if (d.attached) {
				if (d.schemaFingerprint === fingerprint) this.takeLease(pageId, lease, d.epoch);
				return {
					epoch: d.epoch,
					quarantined: d.quarantined,
					schemaFingerprint: d.schemaFingerprint,
					seeded: false,
					updates: this.rowsFor(pageId, d.epoch, 0),
					contentAsOf: this.contentAsOf(pageId, d.epoch)
				};
			}
			const initial = seed(this.pages.get(pageId) ?? null);
			this.seedCount++;
			const contentAt = this.contentAt.get(pageId);
			const seq = this.startEpoch(pageId, d, d.epoch + 1, initial, fingerprint, contentAt ?? this.tick());
			// Renames older than the stored content are settled by it.
			for (const t of this.renamedTasks) {
				if (t.pageId === pageId && contentAt !== undefined && t.renamedAt <= contentAt) t.appliedAt = t.renamedAt;
			}
			this.takeLease(pageId, lease, d.epoch);
			return {
				epoch: d.epoch,
				quarantined: false,
				schemaFingerprint: fingerprint,
				seeded: true,
				updates: [{ seq, data: initial }],
				contentAsOf: this.contentAsOf(pageId, d.epoch)
			};
		});
	}

	reseed(pageId: string, fromEpoch: number, update: Uint8Array, fingerprint: string, lease?: Lease): Promise<number | 'held' | null> {
		return this.locked(pageId, () => {
			const d = this.docs.get(pageId);
			if (!d || !d.attached || d.epoch !== fromEpoch) return null;
			const now = Date.now();
			for (const l of this.leases.values()) {
				if (l.pageId === pageId && l.epoch === fromEpoch && l.holder !== lease?.holder && l.expiresAt > now) return 'held';
			}
			const logAt = Math.max(...this.rows.filter((r) => r.pageId === pageId).map((r) => r.at));
			this.startEpoch(pageId, d, fromEpoch + 1, update, fingerprint, logAt);
			this.takeLease(pageId, lease, d.epoch);
			return d.epoch;
		});
	}

	async renewLeases(holder: string, held: { pageId: string; epoch: number }[], ttlMs: number): Promise<void> {
		for (const h of held) if (this.pages.has(h.pageId)) this.takeLease(h.pageId, { holder, ttlMs }, h.epoch);
	}

	async releaseLease(pageId: string, holder: string, epoch: number): Promise<void> {
		const key = `${pageId} ${holder}`;
		if (this.leases.get(key)?.epoch === epoch) this.leases.delete(key);
	}

	private startEpoch(pageId: string, d: DocRow, epoch: number, initial: Uint8Array, fingerprint: string, at: number): number {
		this.rows = this.rows.filter((r) => r.pageId !== pageId);
		const seq = ++this.seq;
		this.rows.push({ pageId, epoch, seq, data: initial, at });
		Object.assign(d, { epoch, attached: true, quarantined: false, quarantineReason: null, schemaFingerprint: fingerprint });
		return seq;
	}

	async append(pageId: string, epoch: number, data: Uint8Array): Promise<number | null> {
		await this.beforeAppend?.(pageId, epoch);
		if (this.failAppends > 0) {
			this.failAppends--;
			throw new Error('simulated database outage');
		}
		const d = this.docs.get(pageId);
		if (!d || d.epoch !== epoch || !d.attached) return null;
		const seq = ++this.seq;
		this.rows.push({ pageId, epoch, seq, data, at: this.tick() });
		return seq;
	}

	appendExclusive(
		pageId: string,
		epoch: number,
		afterSeq: number,
		build: (rows: StoredUpdate[]) => ExclusiveEdit
	): Promise<number | null | 'stale'> {
		return this.locked(pageId, async () => {
			await this.beforeAppend?.(pageId, epoch);
			if (this.failAppends > 0) {
				this.failAppends--;
				throw new Error('simulated database outage');
			}
			const d = this.docs.get(pageId);
			if (!d || d.epoch !== epoch || !d.attached) return 'stale';
			const { update, titlesShown = [] } = build(this.rowsFor(pageId, epoch, afterSeq));
			for (const shown of titlesShown) {
				for (const t of this.renamedTasks) {
					if (t.pageId === pageId && t.nodeId === shown.nodeId && t.title === shown.title) t.appliedAt = t.renamedAt;
				}
			}
			if (!update) return null;
			const seq = ++this.seq;
			this.rows.push({ pageId, epoch, seq, data: update, at: this.tick() });
			return seq;
		});
	}

	async fetchSince(pageId: string, epoch: number, afterSeq: number): Promise<StoredUpdate[]> {
		return this.rowsFor(pageId, epoch, afterSeq);
	}

	compact(pageId: string, epoch: number): Promise<number | null> {
		return this.locked(pageId, () => {
			this.onCompact?.(pageId, epoch);
			const merged = this.rows.filter((r) => r.pageId === pageId && r.epoch === epoch);
			if (merged.length < 2) return null;
			const seq = ++this.seq;
			const ids = new Set(merged.map((r) => r.seq));
			this.rows = this.rows.filter((r) => !ids.has(r.seq));
			this.rows.push({ pageId, epoch, seq, data: Y.mergeUpdates(merged.map((r) => r.data)), at: Math.max(...merged.map((r) => r.at)) });
			return seq;
		});
	}

	async renamedTaskTitles(pageId: string): Promise<TaskTitle[]> {
		if (this.failRenamedTitles) throw new Error('simulated database outage');
		return this.renamedTasks
			.filter((t) => t.pageId === pageId && (t.appliedAt === undefined || t.appliedAt < t.renamedAt))
			.sort((a, b) => a.renamedAt - b.renamedAt)
			.map(({ nodeId, title }) => ({ nodeId, title }));
	}

	private contentAsOf(pageId: string, epoch: number): string | null {
		const times = this.rows.filter((r) => r.pageId === pageId && r.epoch === epoch).map((r) => r.at);
		return times.length ? String(Math.max(...times)) : null;
	}

	async quarantine(pageId: string, epoch: number, reason: string): Promise<void> {
		const d = this.docs.get(pageId);
		if (d && d.epoch === epoch && !d.quarantined) Object.assign(d, { quarantined: true, quarantineReason: reason });
	}

	async close(): Promise<void> {}

	/** The document a fresh load would produce, straight from the log. */
	replay(pageId: string): Y.Doc {
		const d = this.docs.get(pageId);
		const doc = new Y.Doc();
		if (d) for (const r of this.rowsFor(pageId, d.epoch, 0)) Y.applyUpdate(doc, r.data);
		return doc;
	}

	private rowsFor(pageId: string, epoch: number, afterSeq: number): StoredUpdate[] {
		return this.rows
			.filter((r) => r.pageId === pageId && r.epoch === epoch && r.seq > afterSeq)
			.sort((a, b) => a.seq - b.seq)
			.map(({ seq, data }) => ({ seq, data }));
	}
}
