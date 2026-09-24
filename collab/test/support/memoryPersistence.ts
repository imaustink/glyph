/**
 * In-memory Persistence with the same semantics as PgPersistence, for tests.
 * A per-page mutex stands in for the page row lock, and `seq` is a global
 * counter like the BIGSERIAL.
 */
import * as Y from 'yjs';
import type { CollabDocState, LoadedDoc, Persistence, Seeder, StoredUpdate } from '../../src/persistence.js';
import { NotFoundError } from '../../src/persistence.js';

interface Row extends StoredUpdate {
	pageId: string;
	epoch: number;
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
	docs = new Map<string, DocRow>();
	rows: Row[] = [];
	seedCount = 0;
	/** Set to make the next N appends throw (simulating a database outage). */
	failAppends = 0;
	private seq = 0;
	private locks = new Map<string, Promise<void>>();

	/** Create a page, optionally with stored content. */
	addPage(pageId: string, content: unknown = null, schemaVersion = 1) {
		this.pages.set(pageId, content === null ? null : { content, schemaVersion });
	}

	/** What the API does on a version restore / REST write while disabled. */
	detach(pageId: string, content?: unknown) {
		const d = this.docs.get(pageId);
		if (d) d.attached = false;
		if (content !== undefined) this.pages.set(pageId, { content, schemaVersion: 1 });
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

	loadOrSeed(pageId: string, seed: Seeder, fingerprint: string): Promise<LoadedDoc> {
		return this.locked(pageId, () => {
			if (!this.pages.has(pageId)) throw new NotFoundError();
			let d = this.docs.get(pageId);
			if (!d) {
				d = { epoch: 0, attached: false, quarantined: false, quarantineReason: null, schemaFingerprint: null };
				this.docs.set(pageId, d);
			}
			if (d.attached) {
				return {
					epoch: d.epoch,
					quarantined: d.quarantined,
					schemaFingerprint: d.schemaFingerprint,
					seeded: false,
					updates: this.rowsFor(pageId, d.epoch, 0)
				};
			}
			const initial = seed(this.pages.get(pageId) ?? null);
			this.seedCount++;
			const seq = this.startEpoch(pageId, d, d.epoch + 1, initial, fingerprint);
			return { epoch: d.epoch, quarantined: false, schemaFingerprint: fingerprint, seeded: true, updates: [{ seq, data: initial }] };
		});
	}

	reseed(pageId: string, fromEpoch: number, update: Uint8Array, fingerprint: string): Promise<number | null> {
		return this.locked(pageId, () => {
			const d = this.docs.get(pageId);
			if (!d || !d.attached || d.epoch !== fromEpoch) return null;
			this.startEpoch(pageId, d, fromEpoch + 1, update, fingerprint);
			return d.epoch;
		});
	}

	private startEpoch(pageId: string, d: DocRow, epoch: number, initial: Uint8Array, fingerprint: string): number {
		this.rows = this.rows.filter((r) => r.pageId !== pageId);
		const seq = ++this.seq;
		this.rows.push({ pageId, epoch, seq, data: initial });
		Object.assign(d, { epoch, attached: true, quarantined: false, quarantineReason: null, schemaFingerprint: fingerprint });
		return seq;
	}

	async append(pageId: string, epoch: number, data: Uint8Array): Promise<number | null> {
		if (this.failAppends > 0) {
			this.failAppends--;
			throw new Error('simulated database outage');
		}
		const d = this.docs.get(pageId);
		if (!d || d.epoch !== epoch || !d.attached) return null;
		const seq = ++this.seq;
		this.rows.push({ pageId, epoch, seq, data });
		return seq;
	}

	async fetchSince(pageId: string, epoch: number, afterSeq: number): Promise<StoredUpdate[]> {
		return this.rowsFor(pageId, epoch, afterSeq);
	}

	compact(pageId: string, epoch: number): Promise<number | null> {
		return this.locked(pageId, () => {
			const merged = this.rows.filter((r) => r.pageId === pageId && r.epoch === epoch);
			if (merged.length < 2) return null;
			const seq = ++this.seq;
			const ids = new Set(merged.map((r) => r.seq));
			this.rows = this.rows.filter((r) => !ids.has(r.seq));
			this.rows.push({ pageId, epoch, seq, data: Y.mergeUpdates(merged.map((r) => r.data)) });
			return seq;
		});
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
