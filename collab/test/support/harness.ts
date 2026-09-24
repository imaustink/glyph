/**
 * Test harness: a real Hocuspocus server running the Glyph extensions against
 * in-memory persistence and a fake API, and real HocuspocusProvider clients
 * talking to it over WebSockets.
 */
import * as Y from 'yjs';
import WebSocket from 'ws';
import { Server } from '@hocuspocus/server';
import { HocuspocusProvider, HocuspocusProviderWebsocket } from '@hocuspocus/provider';
import { documentSchema, schemaFingerprint, COLLAB_FRAGMENT } from '$lib/editor/schema';
import { collabDocumentName, decodeServerMessage, encodeToken } from '$lib/collab/protocol';
import { GlyphCollab } from '../../src/extension.js';
import { GlyphHttp } from '../../src/http.js';
import type { Api, CollabSession, SnapshotBody, SnapshotResult } from '../../src/api.js';
import { silentLogger } from '../../src/log.js';
import { MemoryPersistence } from './memoryPersistence.js';

export const schema = documentSchema();
export const fingerprint = schemaFingerprint(schema);

// ─── Fake API ─────────────────────────────────────────────────────────────────

export class FakeApi implements Api {
	enabled = true;
	/** pageId → userId → access */
	access = new Map<string, Map<string, 'write' | 'read'>>();
	snapshots = new Map<string, { body: SnapshotBody; revision: number }[]>();
	/** Called on each snapshot; return a result to override the default. */
	onSnapshot: ((pageId: string, body: SnapshotBody) => SnapshotResult | undefined) | null = null;
	private lastSeq = new Map<string, number>();

	constructor(private readonly persistence: MemoryPersistence) {}

	grant(pageId: string, userId: string, level: 'write' | 'read') {
		if (!this.access.has(pageId)) this.access.set(pageId, new Map());
		this.access.get(pageId)!.set(userId, level);
	}
	revoke(pageId: string, userId: string) {
		this.access.get(pageId)?.delete(userId);
	}

	async authorize(pageId: string, cookie: string): Promise<CollabSession | null> {
		const userId = /user=([\w-]+)/.exec(cookie)?.[1];
		const level = userId ? this.access.get(pageId)?.get(userId) : undefined;
		if (!userId || !level) return null;
		return { enabled: this.enabled, pageId, userId, name: userId, canWrite: level === 'write' };
	}

	/** Mirrors the API's WriteCollabSnapshot preconditions. */
	async snapshot(pageId: string, body: SnapshotBody): Promise<SnapshotResult> {
		const override = this.onSnapshot?.(pageId, body);
		if (override) return override;
		if (!this.enabled) return { kind: 'disabled' };
		const d = this.persistence.docs.get(pageId);
		if (!d || !d.attached || d.epoch !== body.epoch || d.quarantined) return { kind: 'stale' };
		if (body.upToSeq < (this.lastSeq.get(pageId) ?? 0)) return { kind: 'stale' };
		this.lastSeq.set(pageId, body.upToSeq);
		const list = this.snapshots.get(pageId) ?? [];
		list.push({ body, revision: list.length + 1 });
		this.snapshots.set(pageId, list);
		this.persistence.pages.set(pageId, { content: body.content, schemaVersion: body.schemaVersion });
		return { kind: 'ok', revision: list.length };
	}

	latest(pageId: string) {
		const list = this.snapshots.get(pageId);
		return list?.[list.length - 1]?.body.content as { type: string; content?: unknown[] } | undefined;
	}
}

// ─── Server ───────────────────────────────────────────────────────────────────

export interface TestServer {
	url: string;
	port: number;
	origin: string;
	collab: GlyphCollab;
	server: Server;
	stop(): Promise<void>;
}

export async function startServer(
	persistence: MemoryPersistence,
	api: FakeApi,
	opts: { maxDocumentBytes?: number; compactEvery?: number; debounce?: number } = {}
): Promise<TestServer> {
	const collab = new GlyphCollab({
		persistence,
		api,
		schema,
		fingerprint,
		allowedOrigins: [],
		maxDocumentBytes: opts.maxDocumentBytes ?? 5 * 1024 * 1024,
		compactEvery: opts.compactEvery ?? 100,
		log: silentLogger
	});
	const http = new GlyphHttp({ collab, api, allowedOrigins: [], log: silentLogger });
	const server = new Server({
		port: 0,
		quiet: true,
		stopOnSignals: false,
		debounce: opts.debounce ?? 50,
		maxDebounce: 200,
		unloadImmediately: true,
		extensions: [collab, http]
	});
	await server.listen();
	const port = server.address.port;
	return {
		url: `ws://localhost:${port}/collab`,
		port,
		origin: `http://localhost:${port}`,
		collab,
		server,
		stop: () => server.destroy()
	};
}

// ─── Clients ──────────────────────────────────────────────────────────────────

export interface TestClient {
	doc: Y.Doc;
	provider: HocuspocusProvider;
	socket: HocuspocusProviderWebsocket;
	epoch: number | null;
	closeReasons: string[];
	authFailures: string[];
	readOnly: boolean | null;
	quarantined: boolean;
	fragment: Y.XmlFragment;
	synced(): Promise<void>;
	destroy(): void;
}

export interface ConnectOptions {
	user: string;
	epoch?: number | null;
	fingerprint?: string;
	doc?: Y.Doc;
	origin?: string;
}

export function connect(server: TestServer, pageId: string, o: ConnectOptions): TestClient {
	const headers = { origin: o.origin ?? server.origin, cookie: `user=${o.user}` };
	class HeaderWebSocket extends WebSocket {
		constructor(url: string) {
			super(url, { headers });
		}
	}
	const socket = new HocuspocusProviderWebsocket({
		url: server.url,
		WebSocketPolyfill: HeaderWebSocket,
		delay: 50,
		minDelay: 10,
		maxAttempts: 1
	});
	const doc = o.doc ?? new Y.Doc();
	const client: TestClient = {
		doc,
		provider: null as unknown as HocuspocusProvider,
		socket,
		epoch: o.epoch ?? null,
		closeReasons: [],
		authFailures: [],
		readOnly: null,
		quarantined: false,
		fragment: doc.getXmlFragment(COLLAB_FRAGMENT),
		synced: () =>
			new Promise<void>((resolve, reject) => {
				if (client.provider.synced) return resolve();
				const timer = setTimeout(() => reject(new Error('timed out waiting for sync')), 3000);
				client.provider.on('synced', () => {
					clearTimeout(timer);
					resolve();
				});
			}),
		destroy: () => {
			client.provider.destroy();
			socket.destroy();
		}
	};
	client.provider = new HocuspocusProvider({
		websocketProvider: socket,
		name: collabDocumentName(pageId),
		document: doc,
		token: () => encodeToken({ v: 1, epoch: client.epoch, fingerprint: o.fingerprint ?? fingerprint }),
		onStateless: ({ payload }) => {
			const m = decodeServerMessage(payload);
			if (m?.type === 'epoch') client.epoch = m.epoch;
			if (m?.type === 'quarantined') client.quarantined = true;
			if (m?.type === 'access') client.readOnly = !m.canWrite;
		},
		onAuthenticated: ({ scope }) => {
			client.readOnly = scope === 'readonly';
		},
		onAuthenticationFailed: ({ reason }) => {
			client.authFailures.push(reason);
		},
		onClose: ({ event }) => {
			if (event?.reason) client.closeReasons.push(String(event.reason));
		}
	});
	client.provider.attach();
	return client;
}

// ─── Document helpers (mirroring y-prosemirror's layout) ──────────────────────

export function paragraph(text: string): Y.XmlElement {
	const p = new Y.XmlElement('paragraph');
	const t = new Y.XmlText();
	t.insert(0, text);
	p.insert(0, [t]);
	return p;
}

export function bulletList(items: { nodeId?: string; taskId?: string; text: string }[]): Y.XmlElement {
	const list = new Y.XmlElement('bulletList');
	list.insert(
		0,
		items.map((i) => {
			const li = new Y.XmlElement('listItem');
			if (i.nodeId) li.setAttribute('nodeId', i.nodeId);
			if (i.taskId) li.setAttribute('taskId', i.taskId);
			li.insert(0, [paragraph(i.text)]);
			return li;
		})
	);
	return list;
}

export function textOf(doc: Y.Doc): string {
	return doc.getXmlFragment(COLLAB_FRAGMENT).toString();
}

export async function eventually(check: () => boolean | void, timeoutMs = 3000, message = 'condition not met'): Promise<void> {
	const start = Date.now();
	for (;;) {
		try {
			if (check() !== false) return;
		} catch (err) {
			if (Date.now() - start > timeoutMs) throw err;
		}
		if (Date.now() - start > timeoutMs) throw new Error(`timed out: ${message}`);
		await new Promise((r) => setTimeout(r, 20));
	}
}

export const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));
