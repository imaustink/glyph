export interface CollabConfig {
	port: number;
	/** Postgres connection string (same database as the API). */
	databaseUrl: string;
	/** Base URL of the Go API, reachable from this service (not via the Ingress). */
	apiUrl: string;
	/** Shared secret for the API's /internal/collab routes. */
	serviceToken: string;
	/**
	 * Origins allowed to open a WebSocket (comma-separated). Empty means
	 * same-origin only: the Origin header's host must match the Host header.
	 * The WebSocket carries the user's session cookie, so without this check
	 * any site could open a socket as the user (cross-site WebSocket hijacking).
	 */
	allowedOrigins: string[];
	/** Debounce and ceiling for persisting a document (ms). */
	storeDebounceMs: number;
	storeMaxDebounceMs: number;
	/** How often each connection's access is re-checked with the API (ms). */
	reauthIntervalMs: number;
	/** How often loaded documents pull updates written by other replicas (ms, 0 = off). */
	catchUpIntervalMs: number;
	/** Compact a document's update log after this many appends. */
	compactEvery: number;
	/** Maximum serialised document size accepted (bytes). Matches the API's limit. */
	maxDocumentBytes: number;
}

function int(name: string, fallback: number): number {
	const raw = process.env[name];
	if (raw === undefined || raw === '') return fallback;
	const n = Number(raw);
	if (!Number.isFinite(n) || n < 0) throw new Error(`${name} must be a non-negative number`);
	return n;
}

function required(name: string): string {
	const v = process.env[name];
	if (!v) throw new Error(`${name} is required`);
	return v;
}

export function loadConfig(): CollabConfig {
	return {
		port: int('PORT', 1235),
		databaseUrl: required('DATABASE_URL'),
		apiUrl: required('API_URL').replace(/\/+$/, ''),
		serviceToken: required('COLLAB_SERVICE_TOKEN'),
		allowedOrigins: (process.env.COLLAB_ALLOWED_ORIGINS ?? '')
			.split(',')
			.map((s) => s.trim().replace(/\/+$/, ''))
			.filter(Boolean),
		storeDebounceMs: int('COLLAB_STORE_DEBOUNCE_MS', 2000),
		storeMaxDebounceMs: int('COLLAB_STORE_MAX_DEBOUNCE_MS', 10000),
		reauthIntervalMs: int('COLLAB_REAUTH_INTERVAL_MS', 60000),
		catchUpIntervalMs: int('COLLAB_CATCH_UP_INTERVAL_MS', 5000),
		compactEvery: int('COLLAB_COMPACT_EVERY', 100),
		maxDocumentBytes: int('COLLAB_MAX_DOCUMENT_BYTES', 5 * 1024 * 1024)
	};
}
