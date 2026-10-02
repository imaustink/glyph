import { repositories } from '$lib/storage/config';
import type { SharedItem } from '$lib/models/types';
import { handleAuthError } from '$lib/storage/apiClient';

const repo = repositories.shares;

/**
 * Holds the notes and folders shared *with* the current user, powering the
 * sidebar's "Shared with me" section. Only meaningful in API mode — sharing
 * does not exist in local mode, where `repo` is null and every method no-ops.
 */
function createSharedStore() {
	let items = $state<SharedItem[]>([]);
	let loaded = $state(false);
	let _loadPromise: Promise<void> | null = null;

	async function load() {
		if (!repo) return;
		if (_loadPromise) return _loadPromise;
		_loadPromise = (async () => {
			try {
				items = await repo.sharedWithMe();
				loaded = true;
			} catch (err) {
				handleAuthError(err);
			} finally {
				_loadPromise = null;
			}
		})();
		return _loadPromise;
	}

	let _refreshPromise: Promise<void> | null = null;

	/**
	 * Re-fetch, e.g. after a share is granted/revoked or when the user returns
	 * to the tab. Overlapping calls coalesce into one in-flight request, so the
	 * focus + visibilitychange pair that fires on tab return only hits the API
	 * once.
	 */
	async function refresh() {
		if (!repo) return;
		if (_refreshPromise) return _refreshPromise;
		_refreshPromise = (async () => {
			try {
				items = await repo!.sharedWithMe();
			} catch (err) {
				handleAuthError(err);
			} finally {
				_refreshPromise = null;
			}
		})();
		return _refreshPromise;
	}

	return {
		get items() {
			return items;
		},
		get loaded() {
			return loaded;
		},
		get available() {
			return repo !== null;
		},
		load,
		refresh
	};
}

export const sharedStore = createSharedStore();
