import { describe, expect, it, vi, beforeEach } from 'vitest';

// vi.mock is hoisted to the top of the compiled file, so the mock object
// below can't close over test-local variables — we mutate `mockEnv` instead
// and each test resets it before importing the module under test.
const mockEnv: Record<string, string | undefined> = {};

vi.mock('$env/dynamic/private', () => ({
	get env() {
		return mockEnv;
	}
}));

/**
 * hooks.server.ts reads env at module load time, so each test needs a fresh
 * module instance — vi.resetModules() + a dynamic import gets a clean read
 * of the current mockEnv values.
 */
async function loadHandle() {
	vi.resetModules();
	const mod = await import('./hooks.server');
	return mod.handle;
}

function makeEvent(pathname: string) {
	return {
		url: new URL(`http://localhost${pathname}`),
		request: new Request(`http://localhost${pathname}`)
	};
}

describe('hooks.server /test proxy gating', () => {
	beforeEach(() => {
		for (const k of Object.keys(mockEnv)) delete mockEnv[k];
	});

	it('does not proxy /test/reset when API_PROXY_TARGET is set but E2E_RESET_ENABLED is not', async () => {
		mockEnv.API_PROXY_TARGET = 'http://backend:8080';
		const handle = await loadHandle();

		const resolve = vi.fn().mockResolvedValue(new Response('resolved-by-sveltekit'));
		const fetchSpy = vi.spyOn(globalThis, 'fetch');

		const response = await handle({ event: makeEvent('/test/reset') as never, resolve });

		expect(fetchSpy).not.toHaveBeenCalled();
		expect(resolve).toHaveBeenCalled();
		expect(await response.text()).toBe('resolved-by-sveltekit');
		fetchSpy.mockRestore();
	});

	it('proxies /test/reset when both API_PROXY_TARGET and E2E_RESET_ENABLED are set', async () => {
		mockEnv.API_PROXY_TARGET = 'http://backend:8080';
		mockEnv.E2E_RESET_ENABLED = 'true';
		const handle = await loadHandle();

		const resolve = vi.fn();
		const fetchSpy = vi
			.spyOn(globalThis, 'fetch')
			.mockResolvedValue(new Response('proxied', { status: 200 }));

		await handle({ event: makeEvent('/test/reset') as never, resolve });

		expect(fetchSpy).toHaveBeenCalledWith(
			'http://backend:8080/test/reset',
			expect.objectContaining({ method: 'GET' })
		);
		expect(resolve).not.toHaveBeenCalled();
		fetchSpy.mockRestore();
	});

	it('still proxies ordinary /api routes when only API_PROXY_TARGET is set', async () => {
		mockEnv.API_PROXY_TARGET = 'http://backend:8080';
		const handle = await loadHandle();

		const resolve = vi.fn();
		const fetchSpy = vi
			.spyOn(globalThis, 'fetch')
			.mockResolvedValue(new Response('proxied', { status: 200 }));

		await handle({ event: makeEvent('/api/v1/pages') as never, resolve });

		expect(fetchSpy).toHaveBeenCalledWith(
			'http://backend:8080/api/v1/pages',
			expect.objectContaining({ method: 'GET' })
		);
		fetchSpy.mockRestore();
	});
});
