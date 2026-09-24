/** Calls from the collab service to the Go API. */

export interface CollabSession {
	enabled: boolean;
	pageId: string;
	userId: string;
	name: string;
	canWrite: boolean;
}

export type SnapshotResult =
	| { kind: 'ok'; revision: number }
	/** The epoch was replaced or the snapshot is behind: evict our copy. */
	| { kind: 'stale' }
	/** The API refused the content: quarantine. */
	| { kind: 'invalid'; message: string }
	/** Collaborative editing is switched off: evict. */
	| { kind: 'disabled' };

export interface SnapshotBody {
	epoch: number;
	upToSeq: number;
	content: unknown;
	schemaVersion: number;
}

export interface Api {
	/**
	 * What the user behind `cookie` may do on the page, or null if they may not
	 * open it at all (or the cookie isn't a valid session).
	 */
	authorize(pageId: string, cookie: string): Promise<CollabSession | null>;
	snapshot(pageId: string, body: SnapshotBody): Promise<SnapshotResult>;
}

export class HttpApi implements Api {
	constructor(
		private readonly baseUrl: string,
		private readonly serviceToken: string,
		private readonly timeoutMs = 10000
	) {}

	async authorize(pageId: string, cookie: string): Promise<CollabSession | null> {
		// Always ask, even without a cookie: whether a cookie-less request is a
		// session is the API's decision (dev-auth mode, for one, says yes).
		const res = await fetch(`${this.baseUrl}/api/v1/pages/${encodeURIComponent(pageId)}/collab`, {
			headers: { ...(cookie ? { cookie } : {}), accept: 'application/json' },
			signal: AbortSignal.timeout(this.timeoutMs)
		});
		if (res.status === 401 || res.status === 403 || res.status === 404) return null;
		if (!res.ok) throw new Error(`authorize ${pageId}: API answered ${res.status}`);
		const s = (await res.json()) as CollabSession;
		if (s.pageId?.toLowerCase() !== pageId.toLowerCase()) throw new Error('authorize: API answered for a different page');
		return s;
	}

	async snapshot(pageId: string, body: SnapshotBody): Promise<SnapshotResult> {
		const res = await fetch(`${this.baseUrl}/internal/collab/pages/${encodeURIComponent(pageId)}/snapshot`, {
			method: 'PUT',
			headers: {
				authorization: `Bearer ${this.serviceToken}`,
				'content-type': 'application/json'
			},
			body: JSON.stringify(body),
			signal: AbortSignal.timeout(this.timeoutMs)
		});
		if (res.ok) {
			const out = (await res.json()) as { revision: number };
			return { kind: 'ok', revision: out.revision };
		}
		const err = (await res.json().catch(() => ({}))) as { code?: string; error?: string };
		if (res.status === 409 && err.code === 'disabled') return { kind: 'disabled' };
		if (res.status === 409 || res.status === 404) return { kind: 'stale' };
		if (res.status === 400) return { kind: 'invalid', message: err.error ?? 'invalid content' };
		throw new Error(`snapshot ${pageId}: API answered ${res.status} ${err.error ?? ''}`);
	}
}
