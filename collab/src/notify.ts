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

export type CollabNotification =
	| { type: 'reset'; pageId: string }
	/** A note task's status changed outside the editor (e.g. on the board). */
	| { type: 'task-status'; pageId: string; nodeId: string; status: string }
	/** A note task was renamed outside the editor (the task page, MCP, an API client) — DI-29. */
	| { type: 'task-title'; pageId: string; nodeId: string; title: string };

const TASK_STATUSES = new Set(['todo', 'in-progress', 'done', 'cancelled']);
/** The API's limit on a task title, in code points (Go's validator counts runes). */
const MAX_TITLE_CHARS = 500;

export function parseNotification(payload: string | undefined): CollabNotification | null {
	if (!payload) return null;
	try {
		const n = JSON.parse(payload) as Partial<{ type: string; pageId: string; nodeId: string; status: string; title: unknown }>;
		if (typeof n?.pageId !== 'string') return null;
		const pageId = n.pageId.toLowerCase();
		if (n.type === 'reset') return { type: 'reset', pageId };
		if (n.type === 'task-status' && typeof n.nodeId === 'string' && n.nodeId && typeof n.status === 'string' && TASK_STATUSES.has(n.status)) {
			return { type: 'task-status', pageId, nodeId: n.nodeId, status: n.status };
		}
		if (
			n.type === 'task-title' &&
			typeof n.nodeId === 'string' &&
			n.nodeId &&
			typeof n.title === 'string' &&
			n.title.trim() !== '' &&
			[...n.title].length <= MAX_TITLE_CHARS
		) {
			return { type: 'task-title', pageId, nodeId: n.nodeId, title: n.title };
		}
		return null;
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
