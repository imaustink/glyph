import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vitest/config';
import { loadEnv } from 'vite';

export default defineConfig(({ mode }) => {
	const env = loadEnv(mode, '.', '');

	return {
		plugins: [sveltekit()],
		server: {
			host: '0.0.0.0',
			// When API_PROXY_TARGET is set (e.g. in E2E tests), proxy API
			// requests to the Go backend so the browser stays same-origin.
			proxy: env.API_PROXY_TARGET
				? {
						'/api': env.API_PROXY_TARGET,
						'/auth': env.API_PROXY_TARGET,
						// NOTE: /oauth/authorize is NOT proxied — it's the SvelteKit
						// consent page's own route (src/routes/oauth/authorize). Only
						// the machine-facing token/revoke endpoints live on the Go API
						// under /oauth; the consent page's own API calls go through
						// /api/v1/oauth/consent instead, which the '/api' entry above
						// already covers.
						'/oauth/token': env.API_PROXY_TARGET,
						'/oauth/revoke': env.API_PROXY_TARGET,
						'/health': env.API_PROXY_TARGET,
						// /test (which includes /test/reset — a full table TRUNCATE) is
						// gated on E2E_RESET_ENABLED separately, mirroring the Go API's
						// own flag (and hooks.server.ts's production build path) — so
						// setting API_PROXY_TARGET alone doesn't also expose it.
						...(env.E2E_RESET_ENABLED === 'true' ? { '/test': env.API_PROXY_TARGET } : {})
					}
				: undefined
		},
		// Polling is required on macOS/Windows Docker because inotify events
		// don't propagate from the host into the container via bind mounts
		watch: {
			usePolling: true,
			interval: 300
		},
		test: {
			environment: 'jsdom',
			include: ['src/**/*.test.ts']
		}
	};
});
