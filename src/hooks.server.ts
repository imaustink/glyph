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

/**
 * OAuth discovery metadata (RFC 8414 / RFC 9728) served by the Go API. Each is
 * matched as a prefix so the path-suffixed variants (e.g.
 * /.well-known/oauth-protected-resource/mcp) are covered too. /.well-known/
 * is deliberately NOT proxied wholesale — anything else under it stays
 * SvelteKit's (or static/'s) to serve.
 */
const WELL_KNOWN_OAUTH_PREFIXES = [
	'/.well-known/oauth-authorization-server',
	'/.well-known/oauth-protected-resource'
];

function isWellKnownOAuthPath(pathname: string): boolean {
	return WELL_KNOWN_OAUTH_PREFIXES.some(
		(prefix) => pathname === prefix || pathname.startsWith(`${prefix}/`)
	);
}

/**
 * When API_PROXY_TARGET is set (e.g. in E2E CI builds), proxy /api, /auth,
 * /oauth/token, /oauth/revoke, /oauth/register, /mcp, the OAuth
 * /.well-known/ metadata documents, and /health requests to the Go backend,
 * plus /test (only when TEST_PROXY_ENABLED is also set — see above).
 *
 * /mcp is the MCP Streamable HTTP endpoint and /oauth/register is RFC 7591
 * dynamic client registration; together with the discovery metadata they are
 * what lets an MCP client connect with just the public origin's /mcp URL.
 * Request headers (including Authorization — /mcp is bearer-authenticated)
 * and bodies are forwarded as-is, and the response body is streamed back
 * unbuffered with its original headers (so an SSE content-type survives).
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
	if (
		API_PROXY_TARGET &&
		(event.url.pathname.startsWith('/api') ||
			event.url.pathname.startsWith('/auth') ||
			event.url.pathname === '/oauth/token' ||
			event.url.pathname === '/oauth/revoke' ||
			event.url.pathname === '/oauth/register' ||
			event.url.pathname === '/mcp' ||
			isWellKnownOAuthPath(event.url.pathname) ||
			event.url.pathname.startsWith('/health') ||
			(TEST_PROXY_ENABLED && event.url.pathname.startsWith('/test')))
	) {
		const targetURL = `${API_PROXY_TARGET}${event.url.pathname}${event.url.search}`;

		const headers = new Headers(event.request.headers);
		// Remove headers that cause issues when proxying.
		headers.delete('host');

		const response = await fetch(targetURL, {
			method: event.request.method,
			headers,
			body:
				event.request.method !== 'GET' && event.request.method !== 'HEAD'
					? event.request.body
					: undefined,
			// @ts-expect-error - Node fetch supports duplex
			duplex: 'half'
		});

		return new Response(response.body, {
			status: response.status,
			headers: response.headers
		});
	}

	return resolve(event);
};
