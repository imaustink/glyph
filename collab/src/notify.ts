/**
 * Subscribes to the API's collab notifications (Postgres LISTEN/NOTIFY on
 * glyph_collab). The API sends "reset" when a page's shared document is
 * replaced — a version restore, or a REST write while collaboration is off —
 * and every client holding it must be evicted.
 *
 * Notifications are an accelerator, not the safety mechanism: if one is
 * missed (listener reconnecting), the next append or snapshot for that page
 * fails its epoch check and evicts the document anyway.
 */
import pg from 'pg';

export const COLLAB_CHANNEL = 'glyph_collab';

export interface CollabNotification {
	type: 'reset';
	pageId: string;
}

export function parseNotification(payload: string | undefined): CollabNotification | null {
	if (!payload) return null;
	try {
		const n = JSON.parse(payload) as CollabNotification;
		return n?.type === 'reset' && typeof n.pageId === 'string' ? { type: 'reset', pageId: n.pageId.toLowerCase() } : null;
	} catch {
		return null;
	}
}

export function listen(
	databaseUrl: string,
	onNotification: (n: CollabNotification) => void,
	log: (msg: string, extra?: unknown) => void
): () => Promise<void> {
	let client: pg.Client | null = null;
	let stopped = false;
	let retry: ReturnType<typeof setTimeout> | null = null;

	const connect = async () => {
		if (stopped) return;
		const c = new pg.Client({ connectionString: databaseUrl });
		client = c;
		c.on('notification', (msg) => {
			const n = parseNotification(msg.payload);
			if (n) onNotification(n);
		});
		c.on('error', (err) => {
			log('collab notify listener error; reconnecting', err);
			void c.end().catch(() => {});
			schedule();
		});
		try {
			await c.connect();
			await c.query(`LISTEN ${COLLAB_CHANNEL}`);
			log('listening for collab notifications');
		} catch (err) {
			log('collab notify listener failed to connect; retrying', err);
			void c.end().catch(() => {});
			schedule();
		}
	};
	const schedule = () => {
		if (stopped || retry) return;
		retry = setTimeout(() => {
			retry = null;
			void connect();
		}, 2000);
	};

	void connect();
	return async () => {
		stopped = true;
		if (retry) clearTimeout(retry);
		await client?.end().catch(() => {});
	};
}
