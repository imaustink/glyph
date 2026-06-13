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
						'/test': env.API_PROXY_TARGET,
						'/health': env.API_PROXY_TARGET
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
