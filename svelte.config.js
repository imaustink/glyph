import adapter from '@sveltejs/adapter-node';

/** @type {import('@sveltejs/kit').Config} */
const config = {
	compilerOptions: {
		// Force runes mode for the project, except for libraries. Can be removed in svelte 6.
		runes: ({ filename }) => (filename.split(/[/\\]/).includes('node_modules') ? undefined : true)
	},
	kit: {
		// adapter-auto only supports some environments, see https://svelte.dev/docs/kit/adapter-auto for a list.
		// If your environment is not supported, or you settled on a specific environment, switch out the adapter.
		// See https://svelte.dev/docs/kit/adapters for more information about adapters.
		adapter: adapter(),
		// This app has no SvelteKit form actions — all mutations go through
		// the Go API via fetch (JSON) or, for OAuth machine-to-machine calls,
		// via hooks.server.ts's proxy to /oauth/token and /oauth/revoke.
		// SvelteKit's built-in CSRF check runs on every form-encoded/
		// multipart/text-plain POST BEFORE our custom `handle` hook even
		// executes, rejecting any request whose Origin header doesn't match
		// (real OAuth clients — AI agents, curl, non-browser HTTP clients —
		// never send a matching Origin, so this would 403 all genuine
		// client_credentials/refresh_token traffic in production, not just
		// tests). The Go API already enforces its own CSRF protection for
		// cookie-authenticated mutations (X-Requested-With header, bypassed
		// for bearer tokens), so this layer is redundant here and unsafe to
		// leave on for the OAuth endpoints.
		csrf: {
			checkOrigin: false
		}
	}
};

export default config;
