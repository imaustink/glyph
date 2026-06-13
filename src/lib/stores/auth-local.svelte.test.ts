/**
 * Tests the authStore in localStorage mode (storageMode = 'local').
 *
 * This file uses a separate vi.mock for $lib/storage/config so that
 * storageMode is 'local' — a separate file is required because vi.mock
 * is hoisted per-file and can't be changed mid-test in the same module.
 */

import { describe, it, expect, vi } from 'vitest';

vi.mock('$lib/storage/apiClient', () => ({
	API_BASE: 'http://localhost:8081'
}));

vi.mock('$lib/storage/config', () => ({
	storageMode: 'local',
	repositories: {}
}));

import { createAuthStore } from './auth.svelte';

describe('authStore (local mode)', () => {
	it('returns authenticated immediately without calling fetch', async () => {
		const fetchSpy = vi.spyOn(globalThis, 'fetch');
		const store = createAuthStore();

		const result = await store.load();

		expect(result).toBe('authenticated');
		expect(fetchSpy).not.toHaveBeenCalled();
	});

	it('sets loadResult to authenticated', async () => {
		const store = createAuthStore();
		await store.load();
		expect(store.loadResult).toBe('authenticated');
	});

	it('does not set a userId (stays null) in local mode', async () => {
		const store = createAuthStore();
		await store.load();
		expect(store.userId).toBeNull();
	});

	it('currentUser is null in local mode', async () => {
		const store = createAuthStore();
		await store.load();
		expect(store.currentUser).toBeNull();
	});

	it('email and name are null in local mode', async () => {
		const store = createAuthStore();
		await store.load();
		expect(store.email).toBeNull();
		expect(store.name).toBeNull();
	});
});
