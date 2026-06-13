import type { CurrentUser } from '$lib/models/types';
import { API_BASE } from '$lib/storage/apiClient';
import { storageMode } from '$lib/storage/config';

export type AuthLoadResult = 'authenticated' | 'unauthenticated' | 'network-error';

export function createAuthStore() {
	let currentUser = $state<CurrentUser | null>(null);
	let loadResult = $state<AuthLoadResult | null>(null);

	/**
	 * Loads the current user from /auth/me.
	 * Returns the result so callers can handle each case appropriately.
	 */
	async function load(): Promise<AuthLoadResult> {
		if (storageMode !== 'api') {
			loadResult = 'authenticated';
			return 'authenticated';
		}
		try {
			const res = await fetch(`${API_BASE}/auth/me`, { credentials: 'include' });
			if (res.status === 401) {
				loadResult = 'unauthenticated';
				return 'unauthenticated';
			}
			if (res.ok) {
				const data = await res.json();
				currentUser = data.id != null
					? { id: data.id, email: data.email ?? '', name: data.name ?? '' }
					: null;
				loadResult = 'authenticated';
				return 'authenticated';
			}
			// Non-401 error status from server
			loadResult = 'network-error';
			return 'network-error';
		} catch {
			loadResult = 'network-error';
			return 'network-error';
		}
	}

	return {
		/** The full current user profile, or null if not authenticated / localStorage mode. */
		get currentUser() { return currentUser; },
		/** Backward-compatible shorthand for currentUser?.id. */
		get userId() { return currentUser?.id ?? null; },
		get email() { return currentUser?.email ?? null; },
		get name() { return currentUser?.name ?? null; },
		get loadResult() { return loadResult; },
		load,
	};
}

export const authStore = createAuthStore();
