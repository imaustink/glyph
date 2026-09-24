/**
 * One browser's connection to one page's shared document.
 *
 * The session enforces the client half of the integrity contract in
 * $lib/collab/protocol:
 *
 *  - It learns its epoch from the server before receiving content, and
 *    presents it on every reconnect. If it ever holds content without knowing
 *    its epoch, it refuses to reconnect and asks to be reset instead.
 *  - On StaleEpoch / Reset / InvalidUpdate it never retries: the Y.Doc is
 *    discarded (by the owner creating a new session) so no state from a
 *    replaced document can merge back in.
 *  - The editor is only created once the session is ready (synced, epoch
 *    known), so no local transaction — not even an automatic one like a
 *    trailing paragraph — can write into the Y.Doc before it has the server's
 *    content.
 */
import * as Y from 'yjs';
import { HocuspocusProvider } from '@hocuspocus/provider';
import {
	CollabReason,
	collabDocumentName,
	decodeServerMessage,
	encodeToken,
	isCollabReason,
	type CollabReasonValue
} from '$lib/collab/protocol';
import { schemaFingerprint } from '$lib/editor/schema';

export type CollabConnection = 'connecting' | 'connected' | 'offline';

export interface CollabSessionState {
	connection: CollabConnection;
	/** Local changes not yet acknowledged by the server. */
	unsynced: boolean;
	/** The user may not edit (viewer, or the page is quarantined). */
	readOnly: boolean;
	/** The server found the shared document invalid; editing is suspended. */
	quarantined: boolean;
}

export interface CollabSessionEvents {
	onState?(state: CollabSessionState): void;
	/**
	 * The shared document in this session must be thrown away. Create a new
	 * session (with a new, empty Y.Doc) to continue.
	 */
	onReset(reason: CollabReasonValue): void;
	/** Collaboration cannot continue for this page (e.g. access revoked, app outdated, disabled). */
	onFatal(reason: CollabReasonValue): void;
}

export interface CollabUser {
	name: string;
	color: string;
}

const UNAVAILABLE_RETRY_MS = [1000, 3000, 10000, 30000];

export class CollabSession {
	readonly doc = new Y.Doc();
	readonly provider: HocuspocusProvider;
	private epoch: number | null = null;
	private destroyed = false;
	private finished = false;
	private authenticated = false;
	private scopeReadOnly = true;
	private quarantined = false;
	private everSynced = false;
	private connection: CollabConnection = 'connecting';
	private unavailableAttempts = 0;
	private retryTimer: ReturnType<typeof setTimeout> | null = null;
	private readyResolvers: (() => void)[] = [];

	readonly user: CollabUser;

	constructor(
		readonly pageId: string,
		private readonly events: CollabSessionEvents,
		opts: { url: string; user: CollabUser }
	) {
		this.user = opts.user;
		this.provider = new HocuspocusProvider({
			url: opts.url,
			name: collabDocumentName(pageId),
			document: this.doc,
			token: () => this.token(),
			onStatus: ({ status }) => {
				this.connection = status === 'connected' ? 'connected' : status === 'connecting' ? 'connecting' : 'offline';
				if (status !== 'connected') this.authenticated = false;
				this.emitState();
			},
			onAuthenticated: ({ scope }) => {
				this.authenticated = true;
				this.unavailableAttempts = 0;
				this.scopeReadOnly = scope === 'readonly';
				this.emitState();
			},
			onAuthenticationFailed: ({ reason }) => this.handleReason(reason),
			onClose: ({ event }) => {
				const reason = (event as { reason?: unknown } | undefined)?.reason;
				if (isCollabReason(reason)) this.handleReason(reason);
			},
			onStateless: ({ payload }) => {
				const m = decodeServerMessage(payload);
				if (!m) return;
				if (m.type === 'epoch') {
					if (this.epoch !== null && this.epoch !== m.epoch) {
						// Never happens with a correct server; treat it as a reset
						// rather than risk mixing two epochs in one Y.Doc.
						this.handleReason(CollabReason.StaleEpoch);
						return;
					}
					this.epoch = m.epoch;
					this.checkReady();
				} else if (m.type === 'quarantined') {
					this.quarantined = true;
					this.emitState();
				} else if (m.type === 'access') {
					this.scopeReadOnly = !m.canWrite;
					this.emitState();
				}
			},
			onSynced: () => {
				this.everSynced = true;
				this.checkReady();
				this.emitState();
			},
			onUnsyncedChanges: () => this.emitState()
		});
		this.provider.setAwarenessField('user', opts.user);
	}

	/**
	 * Resolves once the session holds the server's content and knows its
	 * epoch — or when it is destroyed, so a superseded page open doesn't hang
	 * (callers re-check that the session is still theirs).
	 */
	whenReady(): Promise<void> {
		if (this.isReady()) return Promise.resolve();
		return new Promise((resolve) => this.readyResolvers.push(resolve));
	}

	get hasUnsyncedChanges(): boolean {
		return this.provider.hasUnsyncedChanges;
	}

	get state(): CollabSessionState {
		return {
			connection: this.connection,
			unsynced: this.provider.hasUnsyncedChanges,
			readOnly: this.scopeReadOnly || this.quarantined,
			quarantined: this.quarantined
		};
	}

	/** Whether the local user may edit right now. Edits while offline are kept and synced later. */
	get canEdit(): boolean {
		return this.isReady() && !this.finished && !this.scopeReadOnly && !this.quarantined;
	}

	destroy(): void {
		if (this.destroyed) return;
		this.destroyed = true;
		if (this.retryTimer) clearTimeout(this.retryTimer);
		const waiting = this.readyResolvers;
		this.readyResolvers = [];
		for (const r of waiting) r();
		this.provider.destroy();
		this.doc.destroy();
	}

	// ─── internals ──────────────────────────────────────────────────────────

	private isReady(): boolean {
		return this.everSynced && this.epoch !== null;
	}

	private checkReady() {
		if (!this.isReady()) return;
		const resolvers = this.readyResolvers;
		this.readyResolvers = [];
		for (const r of resolvers) r();
	}

	private token(): string {
		// Content from an unknown epoch must never be offered to the server.
		// (Only reachable if a connection dropped between receiving content and
		// receiving the epoch — the server sends the epoch first.)
		if (this.epoch === null && Y.encodeStateVector(this.doc).length > 1) {
			queueMicrotask(() => this.handleReason(CollabReason.StaleEpoch));
			throw new Error('session holds state from an unknown epoch');
		}
		return encodeToken({ v: 1, epoch: this.epoch, fingerprint: schemaFingerprint() });
	}

	private handleReason(reason: string) {
		if (this.destroyed || this.finished) return;
		switch (reason) {
			case CollabReason.StaleEpoch:
			case CollabReason.Reset:
			case CollabReason.InvalidUpdate:
				this.finish();
				this.events.onReset(reason);
				return;
			case CollabReason.SchemaMismatch:
			case CollabReason.Forbidden:
			case CollabReason.Disabled:
				this.finish();
				this.events.onFatal(reason);
				return;
			case CollabReason.Unavailable:
			default:
				// Transient: reconnect with the same document and epoch.
				this.scheduleReconnect();
		}
	}

	/** Whether the session has stopped for good (fatal reason or reset). */
	get isFinished(): boolean {
		return this.finished;
	}

	/** Stop syncing this Y.Doc for good; the owner will discard it. */
	private finish() {
		this.finished = true;
		// Resolve any pending whenReady() waiters. A fatal reason
		// (SchemaMismatch/Forbidden) is refused before the first sync, so
		// isReady() never becomes true on its own — without this a caller
		// awaiting whenReady() hangs forever and leaks with the note left blank.
		const waiting = this.readyResolvers;
		this.readyResolvers = [];
		for (const r of waiting) r();
		this.provider.disconnect();
		this.emitState();
	}

	private scheduleReconnect() {
		if (this.retryTimer) return;
		const delay = UNAVAILABLE_RETRY_MS[Math.min(this.unavailableAttempts, UNAVAILABLE_RETRY_MS.length - 1)];
		this.unavailableAttempts++;
		this.provider.disconnect();
		this.retryTimer = setTimeout(() => {
			this.retryTimer = null;
			if (!this.destroyed && !this.finished) void this.provider.connect();
		}, delay);
	}

	private emitState() {
		if (!this.destroyed) this.events.onState?.(this.state);
	}
}
