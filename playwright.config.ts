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

// Ports default to the values below for a lone `pnpm test:e2e*` run, but every
// one of them is overridable so scripts/test-e2e.sh can hand out ports that
// are unique per invocation — letting multiple agents/worktrees run the E2E
// suite on the same machine at the same time without port collisions.
const localPort = process.env.TEST_LOCAL_PORT ?? '5175';
const apiUiPort = process.env.TEST_API_UI_PORT ?? '5174';
const apiPort = process.env.TEST_API_PORT ?? '8083';
const collabPort = process.env.TEST_COLLAB_PORT ?? '1236';
// The api project runs with realtime collaboration on (TEST_COLLAB_ENABLED=false
// to run it single-writer), so every editing spec exercises the collaborative path.
const collabEnabled = process.env.TEST_COLLAB_ENABLED !== 'false';
const collabToken = 'e2e-collab-service-token';
// An app that is already deployed somewhere (scripts/test-e2e-k8s.sh points
// these at a cluster's Ingress). When set, that project's tests run against
// the URL and none of its webServer entries are started.
const externalLocalUrl = process.env.TEST_LOCAL_URL;
const externalApiUiUrl = process.env.TEST_API_UI_URL;
const databaseUrl = process.env.DATABASE_URL ?? 'postgres://glyph:glyph@localhost:5432/glyph?sslmode=disable';

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
				baseURL: externalLocalUrl ?? `http://localhost:${localPort}`
			}
		},
		{
			name: 'api',
			use: {
				storageMode: 'api' as const,
				baseURL: externalApiUiUrl ?? `http://localhost:${apiUiPort}`
			}
		}
	],

	webServer: [
		// ── Local-mode SvelteKit dev server ────────────────────────────────
		// Uses a dedicated port (5175) so it never collides with a normal
		// `pnpm dev` session on 5173 (which defaults to api mode via .env).
		...(wantLocal && !externalLocalUrl
			? [
					{
						command: process.env.CI
							? `PORT=${localPort} node build-local`
							: `VITE_STORAGE_MODE=local pnpm dev --port ${localPort}`,
						port: Number(localPort),
						reuseExistingServer: !process.env.CI as boolean,
						timeout: 30_000
					}
				]
			: []),
		// ── Go API (OIDC disabled → dev auth middleware) ───────────────────
		// Only started when the api project is requested.
		...(wantApi && !externalApiUiUrl
			? [
					{
						command: process.env.API_SERVER_CMD ?? 'cd api && go run ./cmd/api',
						port: Number(apiPort),
						reuseExistingServer: !!process.env.REUSE_API_SERVER,
					timeout: process.env.CI ? 60_000 : 30_000,
						env: {
							DATABASE_URL: databaseUrl,
							PORT: apiPort,
							OIDC_ISSUER_URL: '',
							OIDC_CLIENT_ID: '',
							OIDC_CLIENT_SECRET: '',
							GIN_MODE: 'test',
							E2E_RESET_ENABLED: 'true',
							COLLAB_ENABLED: String(collabEnabled),
							COLLAB_SERVICE_TOKEN: collabToken
						}
					},
					// ── Collab service ─────────────────────────────────────────
					// Built with `pnpm --filter @k5s/glyph-collab build`
					// (scripts/test-e2e.sh runs it in a container instead).
					...(collabEnabled
						? [
								{
									command: 'node --enable-source-maps collab/dist/server.js',
									url: `http://localhost:${collabPort}/collab/healthz`,
									reuseExistingServer: !!process.env.REUSE_API_SERVER,
									timeout: 30_000,
									env: {
										PORT: collabPort,
										DATABASE_URL: databaseUrl,
										API_URL: `http://localhost:${apiPort}`,
										COLLAB_SERVICE_TOKEN: collabToken,
										COLLAB_ALLOWED_ORIGINS: `http://localhost:${apiUiPort}`,
										COLLAB_STORE_DEBOUNCE_MS: '300',
										COLLAB_STORE_MAX_DEBOUNCE_MS: '1000',
										COLLAB_REAUTH_INTERVAL_MS: '5000'
									}
								}
							]
						: []),
					// ── API-mode SvelteKit dev server (proxies to Go API) ──────
					// E2E_RESET_ENABLED=true mirrors the Go API's own flag (set
					// above) — hooks.server.ts requires it before it will proxy
					// /test/* (see hooks.server.ts), so /test/reset stays reachable
					// for these e2e tests without exposing it whenever
					// API_PROXY_TARGET alone is set elsewhere (e.g. local dev).
					// With collaboration on, the dev server also proxies /collab
					// (WebSocket included). The prebuilt CI bundle can only proxy
					// its HTTP routes; it is built with VITE_COLLAB_URL pointing
					// the WebSocket at the collab service directly (see ci.yml).
					{
						command: process.env.CI
							? `PORT=${apiUiPort} API_PROXY_TARGET=http://localhost:${apiPort} COLLAB_PROXY_TARGET=http://localhost:${collabPort} E2E_RESET_ENABLED=true node build-api`
							: `VITE_STORAGE_MODE=api VITE_API_URL= API_PROXY_TARGET=http://localhost:${apiPort} COLLAB_PROXY_TARGET=http://localhost:${collabPort} E2E_RESET_ENABLED=true pnpm dev --port ${apiUiPort}`,
						port: Number(apiUiPort),
						reuseExistingServer: !process.env.CI as boolean,
						timeout: 30_000
					}
				]
			: [])
	]
});
