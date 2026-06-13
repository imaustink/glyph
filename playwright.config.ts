import { defineConfig, devices } from '@playwright/test';

/**
 * Playwright config — runs the same E2E specs against two storage backends:
 *
 *   local  — SvelteKit + localStorage (no API, no DB)
 *   api    — SvelteKit + Go REST API + Postgres
 *
 * Adding a new backend:
 *   1. Add a new project below with a unique `storageMode` in `use`.
 *   2. Add a matching `webServer` entry to start whatever services it needs.
 *   3. If the backend needs custom reset logic, extend `e2e/fixtures.ts`.
 *
 * Usage:
 *   pnpm test:e2e              # all projects (requires Postgres for api)
 *   pnpm test:e2e:local        # localStorage only — no backend needed
 *   pnpm test:e2e:api          # API only — needs Postgres + Go API
 */

const wantApi =
	!process.env.PLAYWRIGHT_PROJECT || process.env.PLAYWRIGHT_PROJECT === 'api';

const wantLocal =
	!process.env.PLAYWRIGHT_PROJECT || process.env.PLAYWRIGHT_PROJECT === 'local';

export default defineConfig({
	testDir: './e2e',
	fullyParallel: true,
	forbidOnly: !!process.env.CI,
	retries: process.env.CI ? 1 : 0,
	workers: process.env.CI ? 1 : 1, // Serialise to prevent concurrent /test/reset DB conflicts
	reporter: process.env.CI ? 'dot' : 'list',
	timeout: process.env.CI ? 60_000 : 30_000,

	use: {
		trace: 'on-first-retry',
		screenshot: 'only-on-failure',
		actionTimeout: process.env.CI ? 15_000 : 5_000,
		navigationTimeout: process.env.CI ? 30_000 : 10_000,
		...devices['Desktop Chrome']
	},

	projects: [
		{
			name: 'local',
			use: {
				storageMode: 'local' as const,
				baseURL: 'http://localhost:5175'
			}
		},
		{
			name: 'api',
			use: {
				storageMode: 'api' as const,
				baseURL: 'http://localhost:5174'
			}
		}
	],

	webServer: [
		// ── Local-mode SvelteKit dev server ────────────────────────────────
		// Uses a dedicated port (5175) so it never collides with a normal
		// `pnpm dev` session on 5173 (which defaults to api mode via .env).
		...(wantLocal
			? [
					{
						command: process.env.CI
							? 'PORT=5175 node build-local'
							: 'VITE_STORAGE_MODE=local pnpm dev --port 5175',
						port: 5175,
						reuseExistingServer: !process.env.CI as boolean,
						timeout: 30_000
					}
				]
			: []),
		// ── Go API (OIDC disabled → dev auth middleware) ───────────────────
		// Only started when the api project is requested.
		...(wantApi
			? [
					{
						command: process.env.API_SERVER_CMD ?? 'cd api && go run ./cmd/api',
						port: 8083,
						reuseExistingServer: !!process.env.REUSE_API_SERVER,
					timeout: process.env.CI ? 60_000 : 30_000,
						env: {
							DATABASE_URL:
								process.env.DATABASE_URL ??
								'postgres://glyph:glyph@localhost:5432/glyph?sslmode=disable',
							PORT: '8083',
							OIDC_ISSUER_URL: '',
							OIDC_CLIENT_ID: '',
							OIDC_CLIENT_SECRET: '',
							GIN_MODE: 'test',
							E2E_RESET_ENABLED: 'true'
						}
					},
					// ── API-mode SvelteKit dev server (proxies to Go API) ──────
					{
						command: process.env.CI
							? 'PORT=5174 API_PROXY_TARGET=http://localhost:8083 node build-api'
							: 'VITE_STORAGE_MODE=api VITE_API_URL= API_PROXY_TARGET=http://localhost:8083 pnpm dev --port 5174',
						port: 5174,
						reuseExistingServer: !process.env.CI as boolean,
						timeout: 30_000
					}
				]
			: [])
	]
});
