/**
 * Thin HTTP client for the Glyph Go API.
 *
 * All methods are relative to the configured base URL.
 * The session cookie (set by /auth/callback) is sent automatically
 * because we use `credentials: 'include'`.
 */

import { browser } from '$app/environment';

/* c8 ignore next -- VITE_API_URL is always undefined in the test environment */
export const API_BASE = import.meta.env.VITE_API_URL ?? 'http://localhost:8081';

// ─── Typed error classes ───────────────────────────────────────────────────────

export class ApiError extends Error {
	constructor(
		public readonly status: number,
		public readonly method: string,
		public readonly path: string,
		public readonly body: unknown
	) {
		super(`${method} ${path} → ${status}`);
		this.name = 'ApiError';
	}
}

export class UnauthorizedError extends ApiError {
	constructor(method: string, path: string) {
		super(401, method, path, null);
		this.name = 'UnauthorizedError';
	}
}

// ─── Auth guard ────────────────────────────────────────────────────────────────

let _redirecting = false;

/**
 * Call this in store load() catch blocks and layout error handlers.
 * Redirects to login exactly once; re-throws all other errors.
 * Flushes pending saves before redirecting to prevent data loss.
 */
export function handleAuthError(err: unknown): never {
	if (err instanceof UnauthorizedError && browser && !_redirecting) {
		_redirecting = true;
		const next = encodeURIComponent(location.pathname + location.search);
		// Flush pending saves before navigating away to prevent data loss.
		// Dynamic import avoids circular dependency (stores → apiClient → stores).
		import('$lib/stores/ui.svelte')
			.then(({ uiStore }) => uiStore.waitForSaveComplete(3000))
			.catch(() => { /* timeout or import error — redirect anyway */ })
			.finally(() => {
				location.assign(`${API_BASE}/auth/login?next=${next}`);
			});
	}
	throw err;
}

// ─── Request ───────────────────────────────────────────────────────────────────

/** Default request timeout in milliseconds (15 seconds). */
const REQUEST_TIMEOUT_MS = 15_000;

export class TimeoutError extends Error {
	constructor(
		public readonly method: string,
		public readonly path: string
	) {
		super(`${method} ${path} timed out after ${REQUEST_TIMEOUT_MS}ms`);
		this.name = 'TimeoutError';
	}
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
	const url = `${API_BASE}${path}`;
	const controller = new AbortController();
	const timeoutId = setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS);

	const init: RequestInit = {
		method,
		credentials: 'include',
		signal: controller.signal,
		headers: {
			'Content-Type': 'application/json',
			'X-Requested-With': 'XMLHttpRequest'
		}
	};
	if (body !== undefined) {
		init.body = JSON.stringify(body);
	}

	let res: Response;
	try {
		res = await fetch(url, init);
	} catch (err) {
		clearTimeout(timeoutId);
		if (err instanceof DOMException && err.name === 'AbortError') {
			throw new TimeoutError(method, path);
		}
		throw err;
	}
	clearTimeout(timeoutId);

	if (res.status === 204) return undefined as unknown as T;

	if (res.status === 401) {
		throw new UnauthorizedError(method, path);
	}

	if (!res.ok) {
		const contentType = res.headers.get('content-type') ?? '';
		const responseBody: unknown = contentType.includes('application/json')
			? await res.json().catch(() => null)
			: await res.text().catch(() => '');
		throw new ApiError(res.status, method, path, responseBody);
	}

	return res.json() as Promise<T>;
}

export const api = {
	get: <T>(path: string) => request<T>('GET', path),
	post: <T>(path: string, body: unknown) => request<T>('POST', path, body),
	patch: <T>(path: string, body: unknown) => request<T>('PATCH', path, body),
	put: <T>(path: string, body: unknown) => request<T>('PUT', path, body),
	del: (path: string) => request<void>('DELETE', path),
	getOrNull: <T>(path: string): Promise<T | null> =>
		request<T>('GET', path).catch((err) => {
			if (err instanceof UnauthorizedError) throw err;
			return null;
		})
};
