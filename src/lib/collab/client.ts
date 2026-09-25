/**
 * Browser-side access to collaborative editing: whether a page is edited
 * collaboratively, where the collab service is, and the server-side edits it
 * offers.
 */
import { api, ApiError } from '$lib/storage/apiClient';
import { storageMode } from '$lib/storage/config';

export interface CollabSessionInfo {
	enabled: boolean;
	pageId: string;
	userId: string;
	name: string;
	canWrite: boolean;
}

/**
 * Collaborative editing needs the API backend. VITE_COLLAB=off disables it
 * client-side (the server-side switch is COLLAB_ENABLED on the API).
 */
export const collabSupported: boolean = storageMode === 'api' && import.meta.env.VITE_COLLAB !== 'off';

/** WebSocket URL of the collab service. Same-origin /collab unless VITE_COLLAB_URL is set. */
export function collabWebSocketUrl(): string {
	const configured = import.meta.env.VITE_COLLAB_URL as string | undefined;
	if (configured) return configured;
	const { protocol, host } = window.location;
	return `${protocol === 'https:' ? 'wss' : 'ws'}://${host}/collab`;
}

/**
 * HTTP base of the collab service (for server-side edits). Always
 * same-origin — the ingress / nginx / SvelteKit proxy routes /collab — so
 * the cookie-authenticated request needs no CORS, even when the WebSocket is
 * pointed elsewhere with VITE_COLLAB_URL.
 */
export function collabHttpBase(): string {
	return `${window.location.origin}/collab`;
}

/**
 * Ask the API whether this page should open collaboratively. Returns null in
 * localStorage mode, when collaboration is off, or when the check fails —
 * callers then use the single-writer editor, which the API itself guards (a
 * whole-document write to a collaborative page is refused, not applied).
 */
export async function getCollabSession(pageId: string): Promise<CollabSessionInfo | null> {
	if (!collabSupported) return null;
	try {
		const info = await api.get<CollabSessionInfo>(`/api/v1/pages/${encodeURIComponent(pageId)}/collab`);
		return info?.enabled ? info : null;
	} catch (err) {
		if (err instanceof ApiError && (err.status === 404 || err.status === 403 || err.status === 400)) return null;
		throw err;
	}
}

export class CollabEditError extends Error {
	constructor(
		public readonly status: number,
		public readonly code: string | undefined
	) {
		super(`collab edit failed (${status}${code ? `, ${code}` : ''})`);
	}
}

/**
 * Remove a bullet from a collaboratively edited page. Applied by the collab
 * service to the shared document, so it reaches everyone editing the page
 * and never overwrites their changes.
 */
export async function removeListItemCollaboratively(pageId: string, nodeId: string): Promise<boolean> {
	const res = await fetch(`${collabHttpBase()}/ops/pages/${encodeURIComponent(pageId)}/remove-list-item`, {
		method: 'POST',
		credentials: 'include',
		headers: { 'content-type': 'application/json', 'x-requested-with': 'XMLHttpRequest' },
		body: JSON.stringify({ nodeId })
	});
	const body = (await res.json().catch(() => ({}))) as { removed?: boolean; code?: string };
	if (!res.ok) throw new CollabEditError(res.status, body.code);
	return !!body.removed;
}
