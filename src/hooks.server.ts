import type { Handle } from '@sveltejs/kit';
import { env } from '$env/dynamic/private';

const API_PROXY_TARGET = env.API_PROXY_TARGET;

/**
 * When API_PROXY_TARGET is set (e.g. in E2E CI builds), proxy all
 * /api, /auth, /test, and /health requests to the Go backend.
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
			event.url.pathname.startsWith('/test') ||
			event.url.pathname.startsWith('/health'))
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
