/**
 * Unit tests for authStore (API mode).
 *
 * Mocks the global `fetch` and `$lib/storage/config` so no real network
 * calls are made.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import type { AuthLoadResult } from './auth.svelte';

vi.mock('$lib/storage/apiClient', () => ({
	API_BASE: 'http://localhost:8081'
}));

vi.mock('$lib/storage/config', () => ({
	storageMode: 'api',
	repositories: {}
}));

import { authStore, createAuthStore } from './auth.svelte';

function mockFetch(status: number, body: unknown) {
	return vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
		status,
		ok: status >= 200 && status < 300,
		json: async () => body
	} as Response);
}

describe('authStore', () => {
	beforeEach(() => {
		vi.restoreAllMocks();
	});

	describe('load', () => {
		it('returns authenticated and sets userId when /auth/me returns 200', async () => {
			mockFetch(200, { id: 'user-abc', email: 'user@example.com', name: 'Alice' });

			const result = await authStore.load();

			expect(result).toBe('authenticated');
			expect(authStore.userId).toBe('user-abc');
		});

		it('sets currentUser with id, email, and name from API response', async () => {
			const store = createAuthStore();
			mockFetch(200, { id: 'user-123', email: 'alice@example.com', name: 'Alice' });

			await store.load();

			expect(store.currentUser).toEqual({ id: 'user-123', email: 'alice@example.com', name: 'Alice' });
			expect(store.email).toBe('alice@example.com');
			expect(store.name).toBe('Alice');
			expect(store.userId).toBe('user-123');
		});

		it('returns unauthenticated when /auth/me returns 401', async () => {
			mockFetch(401, { error: 'unauthorized' });

			const result = await authStore.load();

			expect(result).toBe('unauthenticated');
		});

		it('returns network-error when fetch throws', async () => {
			vi.spyOn(globalThis, 'fetch').mockRejectedValueOnce(new Error('Network error'));

			const result = await authStore.load();

			expect(result).toBe('network-error');
		});

		it('passes credentials: include to fetch', async () => {
			const spy = mockFetch(200, { id: 'user-xyz', email: 'x@x.com', name: 'X' });

			await authStore.load();

			expect(spy).toHaveBeenCalledWith(
				'http://localhost:8081/auth/me',
				expect.objectContaining({ credentials: 'include' })
			);
		});

		it('sets currentUser to null when response body has no id field', async () => {
			const store = createAuthStore();
			mockFetch(200, {});

			await store.load();

			expect(store.currentUser).toBeNull();
			expect(store.userId).toBeNull();
			expect(store.email).toBeNull();
			expect(store.name).toBeNull();
		});

		it('uses empty string for missing email and name in currentUser', async () => {
			const store = createAuthStore();
			mockFetch(200, { id: 'user-partial' }); // email and name absent

			await store.load();

			expect(store.currentUser).toEqual({ id: 'user-partial', email: '', name: '' });
			expect(store.email).toBe('');
			expect(store.name).toBe('');
		});

		it('returns network-error for a non-401 server error (e.g. 500)', async () => {
			vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
				status: 500,
				ok: false,
				json: async () => ({})
			} as Response);

			const result = await authStore.load();
			expect(result).toBe('network-error');
		});

		it('sets loadResult correctly after each call', async () => {
			const store = createAuthStore();

			mockFetch(401, {});
			await store.load();
			expect(store.loadResult).toBe('unauthenticated');

			mockFetch(200, { id: 'u1', email: 'u@u.com', name: 'U' });
			await store.load();
			expect(store.loadResult).toBe('authenticated');
		});

		it('userId remains backward-compatible getter', async () => {
			const store = createAuthStore();
			mockFetch(200, { id: 'compat-id', email: 'c@c.com', name: 'C' });

			await store.load();

			// userId is a shorthand for currentUser?.id
			expect(store.userId).toBe('compat-id');
		});
	});
});
