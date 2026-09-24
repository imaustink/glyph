import type { Handle } from '@sveltejs/kit';
import { env } from '$env/dynamic/private';

const API_PROXY_TARGET = env.API_PROXY_TARGET;
// Mirrors the Go API's own E2E_RESET_ENABLED flag (see cmd/api/auth.go):
// proxying /test/* (which includes /test/reset — a full table TRUNCATE) is
// gated on this separately from the rest of the proxy, so setting
// API_PROXY_TARGET alone (needed for ordinary /api, /auth, and OAuth proxying
// in any API_PROXY_TARGET deployment) doesn't also expose the test-reset
// surface. The Go API refuses to register /test/* at all unless it agrees
// this is a test/dev-mode build, so this is defense in depth, not the only
// gate.
const TEST_PROXY_ENABLED = env.E2E_RESET_ENABLED === 'true';

// Base URL of the collab service, when this process fronts it (e.g. E2E CI).
const COLLAB_PROXY_TARGET = env.COLLAB_PROXY_TARGET;

/**
 * When API_PROXY_TARGET is set (e.g. in E2E CI builds), proxy /api, /auth,
 * /oauth/token, /oauth/revoke, and /health requests to the Go backend, plus
 * /test (only when TEST_PROXY_ENABLED is also set — see above).
 *
 * /oauth/authorize is deliberately NOT proxied — it's the SvelteKit consent
 * page's own route (src/routes/oauth/authorize); that page's own API calls
 * go through /api/v1/oauth/consent instead, already covered by the /api
 * prefix below. Proxying /oauth/authorize itself would hijack the page
 * navigation before SvelteKit's router ever sees it.
 *
 * This mirrors what the Vite dev server does via vite.config.ts proxy,
 * allowing us to serve a pre-built adapter-node bundle in CI without
 * needing the Vite dev server at all.
 */
export const handle: Handle = async ({ event, resolve }) => {
	// The collab service's plain-HTTP routes (server-side edits, health).
	// WebSocket upgrades can't be proxied from here; deployments that serve
	// the frontend this way point the browser at the collab service directly
	// with VITE_COLLAB_URL (it checks the Origin itself).
	if (COLLAB_PROXY_TARGET && event.url.pathname.startsWith('/collab/')) {
		return proxy(`${COLLAB_PROXY_TARGET}${event.url.pathname}${event.url.search}`, event.request);
	}

	if (
		API_PROXY_TARGET &&
		(event.url.pathname.startsWith('/api') ||
			event.url.pathname.startsWith('/auth') ||
			event.url.pathname === '/oauth/token' ||
			event.url.pathname === '/oauth/revoke' ||
			event.url.pathname.startsWith('/health') ||
			(TEST_PROXY_ENABLED && event.url.pathname.startsWith('/test')))
	) {
		return proxy(`${API_PROXY_TARGET}${event.url.pathname}${event.url.search}`, event.request);
	}

	return resolve(event);
};

async function proxy(targetURL: string, request: Request): Promise<Response> {
	const headers = new Headers(request.headers);
	// Remove headers that cause issues when proxying.
	headers.delete('host');

	const response = await fetch(targetURL, {
		method: request.method,
		headers,
		body: request.method !== 'GET' && request.method !== 'HEAD' ? request.body : undefined,
		// @ts-expect-error - Node fetch supports duplex
		duplex: 'half'
	});

	return new Response(response.body, {
		status: response.status,
		headers: response.headers
	});
}
