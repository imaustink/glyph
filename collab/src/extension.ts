/**
 * The Hocuspocus extension that makes a shared Glyph page safe to edit
 * concurrently. See src/lib/collab/protocol.ts for the client-facing contract.
 *
 * Per connection:
 *   onAuthenticate  origin check, schema fingerprint, session via the API
 *                   (forwarding the browser's cookie), read-only for viewers.
 *   beforeSync      binds the connection to the document's epoch before any
 *                   client state is applied, and validates every incoming
 *                   update against a shadow copy of the document. A message
 *                   that fails is dropped and the connection closed.
 *
 * Per document:
 *   onLoadDocument  loads the update log, seeding a new epoch from
 *                   page_contents exactly once if the page isn't attached.
 *   onStoreDocument (debounced) pulls other replicas' updates, applies
 *                   server-only repairs, appends new updates to the log,
 *                   compacts, and writes a validated snapshot to the API.
 *   eviction        when the API says the document was replaced, or a write
 *                   finds its epoch gone, every connection is closed with
 *                   Reset and the in-memory copy is discarded unsaved.
 */
import * as Y from 'yjs';
import type {
	Extension,
	Hocuspocus,
	Document,
	Connection,
	onAuthenticatePayload,
	beforeSyncPayload,
	beforeHandleAwarenessPayload,
	onLoadDocumentPayload,
	afterLoadDocumentPayload,
	onStoreDocumentPayload,
	afterUnloadDocumentPayload
} from '@hocuspocus/server';
import type { Schema } from '@tiptap/pm/model';
import { CURRENT_SCHEMA_VERSION } from '$lib/editor/schema';
import { applyMigrations } from '$lib/editor/migrations';
import type { ProseMirrorJSONNode } from '$lib/models/types';
import {
	CollabReason,
	decodeToken,
	pageIdFromDocumentName,
	collabDocumentName,
	type CollabReasonValue,
	type CollabServerMessage
} from '$lib/collab/protocol';
import type { Api, CollabSession } from './api.js';
import type { Persistence, StoredPageContent } from './persistence.js';
import { NotFoundError } from './persistence.js';
import { inspect, repair, seedUpdate, setListItemStatus, toJSON, type ProseMirrorJSON } from './documentRules.js';
import type { TaskStatus } from '$lib/models/types';
import type { Logger } from './log.js';
import { originAllowed } from './origin.js';

/** Updates replayed from the database: already persisted, never re-stored. */
export const DB_ORIGIN = { source: 'local', skipStoreHooks: true, glyph: 'db' } as const;
/** Server-made repairs: persisted and broadcast like any edit. */
export const REPAIR_ORIGIN = { source: 'local', glyph: 'repair' } as const;

/** Error carrying a reason the client understands; Hocuspocus forwards `reason`. */
export class CollabError extends Error {
	constructor(
		public readonly reason: CollabReasonValue,
		detail?: string,
		public readonly code = 4400
	) {
		super(detail ? `${reason}: ${detail}` : reason);
	}
}

export interface ConnectionContext {
	pageId: string;
	userId: string;
	name: string;
	canWrite: boolean;
	/** Forwarded session cookie, kept to re-check access periodically. */
	cookie: string;
	/** The epoch the client said its Y.Doc belongs to (null = fresh, empty Y.Doc). */
	clientEpoch: number | null;
	/** The epoch this connection was bound to by its first sync message. */
	boundEpoch: number | null;
	/** The awareness (presence) client this connection speaks for. */
	awarenessClientId?: number;
}

interface DocState {
	name: string;
	pageId: string;
	epoch: number;
	document: Document;
	/** Mirror of the document used to test incoming updates before they are applied. */
	shadow: Y.Doc;
	/** Updates not yet appended to the log (everything except DB replays). */
	pending: Uint8Array[];
	/** Highest log seq reflected in the in-memory document. */
	lastSeq: number;
	appendsSinceCompact: number;
	lastSnapshot: string | null;
	quarantined: boolean;
	evicted: boolean;
	/** Serialises persistence work for this document. */
	chain: Promise<void>;
	retryTimer: ReturnType<typeof setTimeout> | null;
	retryDelayMs: number;
	unsubscribe: () => void;
}

export interface GlyphCollabOptions {
	persistence: Persistence;
	api: Api;
	schema: Schema;
	fingerprint: string;
	allowedOrigins: string[];
	maxDocumentBytes: number;
	compactEvery: number;
	/**
	 * On shutdown, how long to keep retrying documents that still hold
	 * unpersisted updates before giving up on them (ms, 0 = don't wait).
	 */
	shutdownDrainMs?: number;
	log: Logger;
}

export class GlyphCollab implements Extension {
	extensionName = 'glyph-collab';
	private readonly docs = new Map<string, DocState>();
	private instance: Hocuspocus | null = null;

	constructor(private readonly opts: GlyphCollabOptions) {}

	// ─── Connection admission ────────────────────────────────────────────────

	async onConfigure({ instance }: { instance: Hocuspocus }) {
		this.instance = instance;
	}

	async onAuthenticate(data: onAuthenticatePayload): Promise<ConnectionContext> {
		const { documentName, token, requestHeaders, connectionConfig } = data;
		const pageId = pageIdFromDocumentName(documentName);
		if (!pageId) throw new CollabError(CollabReason.Forbidden, 'unknown document');
		if (!originAllowed(requestHeaders, this.opts.allowedOrigins)) throw new CollabError(CollabReason.Forbidden, 'origin not allowed');

		const t = decodeToken(token);
		if (!t) throw new CollabError(CollabReason.SchemaMismatch, 'malformed token');
		if (t.fingerprint !== this.opts.fingerprint) {
			throw new CollabError(CollabReason.SchemaMismatch, `client ${t.fingerprint}, server ${this.opts.fingerprint}`);
		}

		const cookie = requestHeaders.get('cookie') ?? '';
		let session: CollabSession | null;
		try {
			session = await this.opts.api.authorize(pageId, cookie);
		} catch (err) {
			this.opts.log.error('authorize failed', { pageId, err });
			throw new CollabError(CollabReason.Unavailable, 'could not check access');
		}
		if (!session) throw new CollabError(CollabReason.Forbidden);
		if (!session.enabled) throw new CollabError(CollabReason.Disabled);

		// Early epoch check so an obviously stale client is turned away
		// before the document loads. The binding check in beforeSync is the
		// one that matters: it runs after load, before any client state is
		// applied.
		const live = this.docs.get(documentName);
		const loaded = live && !live.evicted ? live : undefined;
		const stored = loaded ? null : await this.opts.persistence.getState(pageId);
		const current = loaded ? loaded.epoch : stored?.attached ? stored.epoch : null;
		if (t.epoch !== null && t.epoch !== current) throw new CollabError(CollabReason.StaleEpoch);

		const quarantined = loaded?.quarantined ?? stored?.quarantined ?? false;
		connectionConfig.readOnly = !session.canWrite || quarantined;

		return {
			pageId,
			userId: session.userId,
			name: session.name,
			canWrite: session.canWrite,
			cookie,
			clientEpoch: t.epoch,
			boundEpoch: null
		};
	}

	async beforeSync({ documentName, connection, type, payload }: beforeSyncPayload) {
		const state = this.docs.get(documentName);
		if (!state || state.evicted) throw new CollabError(CollabReason.Reset);
		const ctx = connection.context as ConnectionContext;

		if (ctx.boundEpoch === null) {
			if (ctx.clientEpoch !== null) {
				if (ctx.clientEpoch !== state.epoch) throw new CollabError(CollabReason.StaleEpoch);
			} else {
				// A client with no epoch must be a fresh, empty Y.Doc, and its
				// first message must be SyncStep1 carrying an empty state
				// vector. Anything else is state from an unknown epoch.
				if (type !== 0) throw new CollabError(CollabReason.StaleEpoch, 'first message was not SyncStep1');
				if (Y.decodeStateVector(payload).size > 0) {
					throw new CollabError(CollabReason.StaleEpoch, 'client holds state but no epoch');
				}
			}
			ctx.boundEpoch = state.epoch;
			// Sent before the server's reply to SyncStep1, so the client knows
			// its epoch before it receives any content.
			this.send(connection, { type: 'epoch', epoch: state.epoch });
			if (state.quarantined) this.send(connection, { type: 'quarantined' });
		} else if (ctx.boundEpoch !== state.epoch) {
			throw new CollabError(CollabReason.StaleEpoch);
		}

		// SyncStep2 / Update carry document changes. Read-only connections'
		// changes are discarded by Hocuspocus, so only writers are checked.
		if ((type === 1 || type === 2) && !connection.readOnly) {
			this.validateIncoming(state, payload, ctx);
		}
	}

	/**
	 * Presence (cursor labels) comes from the client, so pin the displayed
	 * name to the authenticated user — otherwise anyone could show their
	 * cursor as someone else.
	 */
	async beforeHandleAwareness({ states, connection }: beforeHandleAwarenessPayload) {
		const ctx = connection?.context as ConnectionContext | undefined;
		if (!ctx) return;
		for (const [clientId, state] of states) {
			const empty = !state || (typeof state === 'object' && Object.keys(state).length === 0);
			// A connection speaks only for its own awareness client: the first
			// one it sends a real state for. (The map also holds Hocuspocus'
			// scratch client, whose state is always empty.) States for anyone
			// else are dropped.
			if (ctx.awarenessClientId === undefined && !empty) ctx.awarenessClientId = clientId;
			if (clientId !== ctx.awarenessClientId) {
				states.delete(clientId);
				continue;
			}
			if (state && typeof state === 'object' && state.user && typeof state.user === 'object') {
				state.user = { ...state.user, name: ctx.name || 'Someone' };
			}
		}
	}

	/**
	 * Apply the update to the shadow document and check the result. On
	 * failure the shadow is rebuilt from the real document (which never saw
	 * the update) and the message is refused.
	 */
	private validateIncoming(state: DocState, update: Uint8Array, ctx: ConnectionContext) {
		try {
			Y.applyUpdate(state.shadow, update);
		} catch (err) {
			this.rebuildShadow(state);
			this.opts.log.warn('refused undecodable update', { pageId: state.pageId, userId: ctx.userId, err });
			throw new CollabError(CollabReason.InvalidUpdate, 'undecodable update');
		}
		const result = inspect(state.shadow, this.opts.schema, this.opts.maxDocumentBytes);
		if (result.fatal) {
			this.rebuildShadow(state);
			this.opts.log.warn('refused invalid update', { pageId: state.pageId, userId: ctx.userId, reason: result.fatal });
			throw new CollabError(CollabReason.InvalidUpdate, result.fatal);
		}
	}

	private rebuildShadow(state: DocState) {
		state.shadow.destroy();
		state.shadow = new Y.Doc();
		Y.applyUpdate(state.shadow, Y.encodeStateAsUpdate(state.document));
	}

	// ─── Document lifecycle ──────────────────────────────────────────────────

	async onLoadDocument({ documentName, document }: onLoadDocumentPayload) {
		const pageId = pageIdFromDocumentName(documentName);
		if (!pageId) throw new CollabError(CollabReason.Forbidden, 'unknown document');

		// A parked copy of this document (see below) may still have its unload
		// flush — or a retry — in flight. Wait for that work before loading: if
		// it succeeds, the load below reads its batch from the log; if it fails,
		// the batch is back in the parked copy's `pending` by the time we carry
		// it over. Carrying `pending` while a batch is in flight would take an
		// empty list, and the batch would later be returned to a copy that has
		// been replaced and never retries (DI-12). `chain` never rejects.
		const before = this.docs.get(documentName);
		if (before) await before.chain;

		let loaded;
		try {
			loaded = await this.opts.persistence.loadOrSeed(pageId, (stored) => this.seed(stored), this.opts.fingerprint);
		} catch (err) {
			if (err instanceof NotFoundError) throw new CollabError(CollabReason.Forbidden, 'page not found');
			this.opts.log.error('failed to load document', { pageId, err });
			throw new CollabError(CollabReason.Unavailable, 'could not load document');
		}

		let { epoch, updates } = loaded;
		if (!loaded.seeded && loaded.schemaFingerprint !== this.opts.fingerprint) {
			// The log was written under a different editor schema (an upgrade
			// since it was last open). Convert it through JSON — which
			// involves no schema, so drops nothing — apply document
			// migrations, and continue in a new epoch. Clients of the old
			// build are refused by the fingerprint check.
			const replay = new Y.Doc();
			for (const u of updates) Y.applyUpdate(replay, u.data);
			const json = toJSON(replay);
			replay.destroy();
			const update = this.seed({ content: json, schemaVersion: CURRENT_SCHEMA_VERSION });
			const next = await this.opts.persistence.reseed(pageId, epoch, update, this.opts.fingerprint);
			if (next === null) throw new CollabError(CollabReason.Unavailable, 'document changed while upgrading');
			this.opts.log.info('re-seeded document for a new schema', { pageId, from: epoch, to: next });
			epoch = next;
			updates = await this.opts.persistence.fetchSince(pageId, epoch, 0);
		}

		for (const u of updates) Y.applyUpdate(document, u.data, DB_ORIGIN);
		const shadow = new Y.Doc();
		Y.applyUpdate(shadow, Y.encodeStateAsUpdate(document));

		const state: DocState = {
			name: documentName,
			pageId,
			epoch,
			document,
			shadow,
			pending: [],
			lastSeq: updates.reduce((m, u) => Math.max(m, u.seq), 0),
			appendsSinceCompact: updates.length,
			lastSnapshot: null,
			quarantined: loaded.quarantined,
			evicted: false,
			chain: Promise.resolve(),
			retryTimer: null,
			retryDelayMs: 1000,
			unsubscribe: () => {}
		};
		const onUpdate = (update: Uint8Array, origin: unknown) => {
			if (state.shadow !== null) Y.applyUpdate(state.shadow, update);
			if ((origin as { glyph?: string } | null)?.glyph !== 'db') state.pending.push(update);
		};
		document.on('update', onUpdate);
		state.unsubscribe = () => document.off('update', onUpdate);

		// A previous copy of this document may still be parked here: its last
		// client left during a persistence outage, so afterUnloadDocument kept
		// it (and a retry) to write its unpersisted updates later. Hocuspocus
		// has already dropped that copy, and this load replaces its entry — so
		// its retry would never run again. Carry its updates into the new copy
		// instead (they re-enter `pending` via the listener above and are
		// persisted with it). Updates from a replaced epoch are dropped: they
		// belong to a document that no longer exists.
		const parked = this.docs.get(documentName);
		if (parked && parked !== state) {
			if (parked.retryTimer) clearTimeout(parked.retryTimer);
			parked.retryTimer = null;
			parked.evicted = true;
			const carried = parked.pending;
			parked.pending = [];
			if (parked.epoch === epoch && carried.length > 0) {
				for (const update of carried) Y.applyUpdate(document, update, REPAIR_ORIGIN);
				this.opts.log.info('carried unpersisted updates into reloaded document', { pageId, updates: carried.length });
			}
			parked.unsubscribe();
			parked.shadow.destroy();
		}
		this.docs.set(documentName, state);
		if (loaded.seeded) this.opts.log.info('seeded document', { pageId, epoch });
	}

	async afterLoadDocument({ documentName }: afterLoadDocumentPayload) {
		const state = this.docs.get(documentName);
		if (!state) return;
		this.applyRepairs(state);
		// Updates carried over from a parked copy (see onLoadDocument) arrived
		// before Hocuspocus started listening, so no store is scheduled for
		// them yet. Write them now rather than waiting for the next edit.
		if (state.pending.length > 0) void this.persist(state);
	}

	private seed(stored: StoredPageContent | null): Uint8Array {
		let json = (stored?.content ?? null) as ProseMirrorJSON | null;
		if (json && stored) {
			json = applyMigrations(json as unknown as ProseMirrorJSONNode, stored.schemaVersion || 1).doc as unknown as ProseMirrorJSON;
		}
		return seedUpdate(this.opts.schema, json);
	}

	private applyRepairs(state: DocState) {
		const report = repair(state.document, REPAIR_ORIGIN);
		if (report.dedupedNodeIds || report.strippedLinks) {
			this.opts.log.info('repaired document', { pageId: state.pageId, ...report });
		}
	}

	async onStoreDocument({ documentName }: onStoreDocumentPayload) {
		const state = this.docs.get(documentName);
		if (!state) return;
		await this.persist(state);
	}

	async afterUnloadDocument({ documentName }: afterUnloadDocumentPayload) {
		const state = this.docs.get(documentName);
		if (!state) return;

		// Don't tear a document down while it still has unpersisted updates. A
		// persistence outage leaves the batch in `pending` with only an in-process
		// retry scheduled — persist() resolves even on failure, so Hocuspocus's
		// sync ack (which is independent of server persistence) already told the
		// client its edits were saved. Clearing the retry timer and deleting the
		// state here would silently lose them. Flush once more; on success, unload;
		// on failure keep the document (and its retry) alive so the edits are
		// written when persistence recovers.
		if (!state.evicted && (state.pending.length > 0 || state.retryTimer !== null)) {
			if (state.retryTimer) {
				clearTimeout(state.retryTimer);
				state.retryTimer = null;
			}
			// Serialise with any in-flight persist and keep `chain` caught, so a
			// later retry's `chain.then(...)` still runs persistOnce (a rejected
			// chain would skip it and loop forever without ever re-appending).
			const run = state.chain.then(() => this.persistOnce(state));
			state.chain = run.catch(() => {});
			try {
				await run;
			} catch (err) {
				this.opts.log.error('deferring unload: document still has unpersisted updates', {
					pageId: state.pageId,
					err
				});
				this.scheduleRetry(state);
				return;
			}
			// The note was reopened while the flush ran: the new copy has
			// replaced this one in `docs` (and onLoadDocument has already torn
			// this one down). Deleting the entry now would delete the *new*
			// session's state, and nothing it did would ever be persisted.
			if (this.docs.get(documentName) !== state) return;
		}

		if (state.retryTimer) clearTimeout(state.retryTimer);
		state.unsubscribe();
		state.shadow.destroy();
		this.docs.delete(documentName);
	}

	/**
	 * Shutdown. Hocuspocus calls this once every document has unloaded — but
	 * a copy parked by afterUnloadDocument (its last client left while
	 * persistence was failing) is not a Hocuspocus document, so it doesn't
	 * hold shutdown up, and closing the pool after this would drop edits its
	 * clients were told were saved. Keep retrying such copies, with backoff,
	 * until they are all written or `shutdownDrainMs` runs out; then name
	 * every document whose updates are being dropped, at error level. Runs
	 * before server.ts's own onDestroy (extensions run in order), which is
	 * what closes the pool.
	 */
	async onDestroy() {
		await this.drain(this.opts.shutdownDrainMs ?? 0);
	}

	/** Persist every copy that still holds unpersisted updates, for up to `timeoutMs`. */
	async drain(timeoutMs: number): Promise<void> {
		const deadline = Date.now() + timeoutMs;
		let delay = 250;
		// A scheduled retry means the last persist failed (possibly only its
		// snapshot), so it counts as unsaved even with nothing in `pending`.
		let left = [...this.docs.values()].filter((s) => !s.evicted && (s.pending.length > 0 || s.retryTimer !== null));
		while (left.length > 0) {
			this.opts.log.info('shutdown: persisting documents with unpersisted updates', { documents: left.length });
			const failed: DocState[] = [];
			await Promise.all(
				left.map(async (state) => {
					// Take over from the scheduled retry: the drain is the retry now.
					if (state.retryTimer) clearTimeout(state.retryTimer);
					state.retryTimer = null;
					const run = state.chain.then(() => this.persistOnce(state));
					state.chain = run.catch(() => {});
					try {
						await run;
					} catch (err) {
						this.opts.log.warn('shutdown: persist failed; retrying until the deadline', { pageId: state.pageId, err });
						failed.push(state);
						return;
					}
					// Written. A parked copy (no Hocuspocus document behind it any
					// more) is torn down, as afterUnloadDocument would have done.
					if (state.document.getConnectionsCount() === 0 && this.docs.get(state.name) === state) {
						state.unsubscribe();
						state.shadow.destroy();
						this.docs.delete(state.name);
					}
				})
			);
			left = failed.filter((s) => !s.evicted && this.docs.get(s.name) === s);
			if (left.length === 0) break;
			if (Date.now() + delay > deadline) {
				for (const state of left) {
					this.opts.log.error('shutting down with unpersisted updates: they are lost', {
						pageId: state.pageId,
						epoch: state.epoch,
						// 0 = everything is in the log, but page_contents lacks the latest snapshot.
						unappendedUpdates: state.pending.length
					});
				}
				break;
			}
			await new Promise((r) => setTimeout(r, delay));
			delay = Math.min(delay * 2, 5000);
		}
		// Nothing may fire after the pool closes.
		for (const state of this.docs.values()) {
			if (state.retryTimer) clearTimeout(state.retryTimer);
			state.retryTimer = null;
		}
	}

	/** Run persistence for a document, serialised with any other run for it. */
	persist(state: DocState): Promise<void> {
		const run = state.chain.then(() => this.persistOnce(state));
		state.chain = run.catch(() => {});
		return run.catch((err) => {
			this.opts.log.error('failed to persist document; will retry', { pageId: state.pageId, err });
			this.scheduleRetry(state);
		});
	}

	private scheduleRetry(state: DocState) {
		if (state.evicted || state.retryTimer) return;
		const delay = state.retryDelayMs;
		state.retryDelayMs = Math.min(delay * 2, 30000);
		state.retryTimer = setTimeout(() => {
			state.retryTimer = null;
			if (this.docs.get(state.name) === state) void this.persist(state);
		}, delay);
	}

	private async persistOnce(state: DocState): Promise<void> {
		if (state.evicted) return;
		const { persistence, compactEvery, log } = this.opts;

		// 1. Pull in anything another replica appended.
		await this.catchUpOnce(state);
		if (state.evicted) return;

		// 2. Server-only repairs. They land in `pending` like any edit.
		this.applyRepairs(state);

		// 3. Append everything not yet in the log. The batch is only dropped
		//    once the database has it; on failure it goes back in front.
		if (state.pending.length > 0) {
			const batch = state.pending;
			state.pending = [];
			let seq: number | null;
			try {
				seq = await persistence.append(state.pageId, state.epoch, Y.mergeUpdates(batch));
			} catch (err) {
				this.returnBatch(state, batch);
				throw err;
			}
			if (seq === null) {
				this.evict(state, CollabReason.Reset, 'epoch replaced');
				return;
			}
			state.appendsSinceCompact++;
			// Don't jump lastSeq to our own seq: another replica may have
			// appended between step 1 and our append, and its row sits below
			// ours. Every later catch-up (strictly seq > lastSeq) would skip it,
			// and our snapshots would silently drop its edit (DI-11). Catch up
			// instead: that reads the foreign row and our own (re-applying our
			// own update is a no-op), and moves lastSeq past both. Seqs of a
			// page become visible in seq order (see persistence.ts), so nothing
			// below what we read here can still appear later.
			await this.catchUpOnce(state);
			if (state.evicted) return;
		}

		// 4. Keep the log short. Compaction merges the epoch's rows into one and
		//    deletes exactly those it merged. Another replica can append a row
		//    during our append round-trip that compaction either folds into the
		//    merged row (whose content we never applied locally) or misses
		//    entirely (it committed after compact's SELECT and kept a lower seq
		//    than the merged row). Jumping lastSeq to the merged seq would make
		//    the next catchUpOnce (fetchSince uses strict seq > afterSeq) skip
		//    such a row forever, so this replica's document — and its next
		//    snapshot — would drop a foreign update (README invariant 3). Leave
		//    lastSeq where it is and catch up: fetchSince re-reads the merged row
		//    (idempotent) plus any concurrent append, so nothing is lost.
		if (state.appendsSinceCompact >= compactEvery) {
			const seq = await persistence.compact(state.pageId, state.epoch);
			if (seq !== null) await this.catchUpOnce(state);
			state.appendsSinceCompact = 1;
			if (state.evicted) return;
		}

		// 5. Snapshot to page_contents through the API. "Behind" means another
		//    replica's snapshot for a later seq of this same epoch landed first
		//    (two replicas snapshotting concurrently). Nothing is wrong with our
		//    copy — everything we hold is in the log — so catch up past that
		//    seq and try once more. If that is still behind (or there was
		//    nothing to catch up), the newer snapshot already covers our
		//    appends; the next store tries again. Never evict for it: eviction
		//    Resets every editor and discards their unappended edits.
		if ((await this.snapshot(state)) === 'behind' && !state.evicted) {
			const seen = state.lastSeq;
			await this.catchUpOnce(state);
			if (state.evicted) return;
			if (state.lastSeq > seen && (await this.snapshot(state)) === 'ok') return;
			log.info('snapshot is behind another replica\'s; leaving it to the next store', { pageId: state.pageId, seq: state.lastSeq });
		}
	}

	/** Step 5 of persistOnce: validate the document and write it to page_contents. */
	private async snapshot(state: DocState): Promise<'ok' | 'behind' | 'done'> {
		const { api, schema, maxDocumentBytes, log } = this.opts;
		if (state.quarantined) return 'done';
		const result = inspect(state.document, schema, maxDocumentBytes);
		if (result.fatal) {
			await this.quarantine(state, result.fatal);
			return 'done';
		}
		if (result.contentError) {
			log.warn('document violates the content model (kept as is)', { pageId: state.pageId, error: result.contentError });
		}
		const serialised = JSON.stringify(result.json);
		if (serialised === state.lastSnapshot) {
			state.retryDelayMs = 1000;
			return 'ok';
		}
		const res = await api.snapshot(state.pageId, {
			epoch: state.epoch,
			upToSeq: state.lastSeq,
			content: result.json,
			schemaVersion: CURRENT_SCHEMA_VERSION
		});
		switch (res.kind) {
			case 'ok':
				state.lastSnapshot = serialised;
				state.retryDelayMs = 1000;
				if (res.disabled) this.windDown(state);
				return 'ok';
			case 'behind':
				return 'behind';
			case 'stale':
				this.evict(state, CollabReason.Reset, 'snapshot rejected as stale');
				return 'done';
			case 'disabled':
				this.evict(state, CollabReason.Disabled, 'collaboration disabled');
				return 'done';
			case 'invalid':
				await this.quarantine(state, `API refused snapshot: ${res.message}`);
				return 'done';
		}
	}

	/**
	 * Put a batch whose append failed back where it will be retried. Normally
	 * that is in front of this copy's `pending`. But if this copy was parked
	 * and has since been replaced by a reload of the same epoch (it is evicted
	 * and no longer registered), its retries will never run: hand the batch to
	 * the live copy instead, which persists and broadcasts it like any edit.
	 * A copy evicted for any other reason (a replaced epoch) drops it — those
	 * updates belong to a document that no longer exists.
	 */
	private returnBatch(state: DocState, batch: Uint8Array[]) {
		const live = this.docs.get(state.name);
		if (state.evicted && live && live !== state && !live.evicted && live.epoch === state.epoch) {
			for (const update of batch) Y.applyUpdate(live.document, update, REPAIR_ORIGIN);
			this.opts.log.info('moved a failed batch into the reloaded document', { pageId: state.pageId, updates: batch.length });
			return;
		}
		state.pending = batch.concat(state.pending);
	}

	/** Apply log entries written by other replicas since we last looked. */
	private async catchUpOnce(state: DocState) {
		const rows = await this.opts.persistence.fetchSince(state.pageId, state.epoch, state.lastSeq);
		for (const row of rows) {
			Y.applyUpdate(state.document, row.data, DB_ORIGIN);
			state.lastSeq = Math.max(state.lastSeq, row.seq);
		}
	}

	/** Periodic pull for multi-replica deployments; serialised with persistence. */
	catchUpAll(): void {
		for (const state of this.docs.values()) {
			if (state.evicted) continue;
			state.chain = state.chain
				.then(() => this.catchUpOnce(state))
				.catch((err) => this.opts.log.warn('catch-up failed', { pageId: state.pageId, err }));
		}
	}

	private async quarantine(state: DocState, reason: string) {
		this.opts.log.error('quarantining document: collaborators are now read-only', { pageId: state.pageId, epoch: state.epoch, reason });
		state.quarantined = true;
		await this.opts.persistence.quarantine(state.pageId, state.epoch, reason);
		for (const connection of state.document.connections.keys()) {
			connection.readOnly = true;
			this.send(connection, { type: 'quarantined' });
		}
	}

	// ─── Eviction and access changes ─────────────────────────────────────────

	/**
	 * Discard a document whose shared state is no longer authoritative. Every
	 * connection is closed with `reason`; clients drop their Y.Doc and
	 * reconnect fresh. Nothing more is written for this epoch.
	 */
	evict(state: DocState, reason: CollabReasonValue, detail: string) {
		if (state.evicted) return;
		state.evicted = true;
		this.opts.log.info('evicting document', { pageId: state.pageId, epoch: state.epoch, reason, detail });
		for (const connection of [...state.document.connections.keys()]) {
			connection.close({ code: 4409, reason });
		}
		void this.instance?.unloadDocument(state.document);
	}

	/**
	 * The kill switch is off, but the document is still attached and its
	 * snapshots are still accepted. Unlike evict(), keep the copy: close every
	 * connection with Disabled (editors fall back to single-writer mode) and
	 * let the unload flush append and snapshot whatever arrived meanwhile.
	 */
	private windDown(state: DocState) {
		const connections = [...state.document.connections.keys()];
		if (connections.length === 0) return;
		this.opts.log.info('collaboration disabled: closing connections after the final snapshot', { pageId: state.pageId });
		for (const connection of connections) connection.close({ code: 4403, reason: CollabReason.Disabled });
	}

	/** The API replaced a page's shared document (restore, or a write while disabled). */
	onReset(pageId: string) {
		const state = this.docs.get(collabDocumentName(pageId));
		if (state) this.evict(state, CollabReason.Reset, 'document replaced');
	}

	/**
	 * A note task's status changed outside the editor (the board, the task
	 * page, an API client). Update its bullet in the live document so every
	 * open copy shows it at once. The server is the only one making this
	 * edit, so editors never race each other to write it; a note that isn't
	 * open is left alone (editors sync statuses from the task list on open).
	 */
	onTaskStatus(pageId: string, nodeId: string, status: TaskStatus): boolean {
		const state = this.docs.get(collabDocumentName(pageId));
		if (!state || state.evicted || state.quarantined) return false;
		const changed = setListItemStatus(state.document, nodeId, status, REPAIR_ORIGIN);
		if (changed) this.opts.log.info('applied task status to bullet', { pageId, nodeId, status });
		return changed;
	}

	/**
	 * Re-check every connection's access. Revoked users are disconnected;
	 * users whose write access changed are switched to/from read-only.
	 */
	async reauthorizeAll(): Promise<void> {
		for (const state of this.docs.values()) {
			for (const connection of [...state.document.connections.keys()]) {
				const ctx = connection.context as ConnectionContext;
				let session: CollabSession | null;
				try {
					session = await this.opts.api.authorize(state.pageId, ctx.cookie);
				} catch (err) {
					this.opts.log.warn('re-authorization failed; keeping connection', { pageId: state.pageId, err });
					continue;
				}
				if (!session || session.userId !== ctx.userId) {
					connection.close({ code: 4403, reason: CollabReason.Forbidden });
					continue;
				}
				if (!session.enabled) {
					connection.close({ code: 4403, reason: CollabReason.Disabled });
					continue;
				}
				const readOnly = !session.canWrite || state.quarantined;
				if (readOnly !== connection.readOnly) {
					connection.readOnly = readOnly;
					ctx.canWrite = session.canWrite;
					this.send(connection, { type: 'access', canWrite: !readOnly });
				}
			}
		}
	}

	/**
	 * Make an edit to a page's shared document on the server's behalf (e.g.
	 * removing a deleted task's bullet). It goes through the same log,
	 * broadcast and snapshot path as a client edit, so connected editors see
	 * it immediately and nobody's copy is overwritten.
	 */
	async serverEdit(pageId: string, edit: (doc: Y.Doc) => boolean): Promise<'changed' | 'unchanged' | 'unavailable' | 'quarantined'> {
		if (!this.instance) return 'unavailable';
		const name = collabDocumentName(pageId);
		const conn = await this.instance.openDirectConnection(name, { glyph: 'server-edit' });
		try {
			const state = this.docs.get(name);
			if (!state || state.evicted) return 'unavailable';
			if (state.quarantined) return 'quarantined';
			let changed = false;
			await conn.transact((doc) => {
				changed = edit(doc);
			});
			return changed ? 'changed' : 'unchanged';
		} finally {
			await conn.disconnect();
		}
	}

	private send(connection: Connection, message: CollabServerMessage) {
		connection.sendStateless(JSON.stringify(message));
	}

	/** For tests and ops: the in-memory state of a document, if loaded. */
	inspectState(documentName: string) {
		const s = this.docs.get(documentName);
		return s ? { epoch: s.epoch, lastSeq: s.lastSeq, pending: s.pending.length, quarantined: s.quarantined, evicted: s.evicted } : null;
	}
}
