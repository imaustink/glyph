import { repositories } from '$lib/storage/config';
import type { OAuthClient, OAuthClientWithSecret, OAuthScope, OAuthToken } from '$lib/models/types';
import { handleAuthError } from '$lib/storage/apiClient';

const repo = repositories.oauthClients;

function createOAuthClientsStore() {
	let clients = $state<OAuthClient[]>([]);
	let loaded = $state(false);
	let loadedOrgId = $state<string | null>(null);
	let _loadPromise: Promise<void> | null = null;

	let tokens = $state<OAuthToken[]>([]);
	let tokensLoaded = $state(false);
	let tokensLoadedClientId = $state<string | null>(null);

	async function load(orgId: string) {
		if (!repo) return;
		if (_loadPromise) return _loadPromise;
		_loadPromise = (async () => {
			try {
				clients = await repo.list(orgId);
				loaded = true;
				loadedOrgId = orgId;
			} catch (err) {
				handleAuthError(err);
			} finally {
				_loadPromise = null;
			}
		})();
		return _loadPromise;
	}

	async function createClient(
		orgId: string,
		input: { name: string; scopes: OAuthScope[]; redirectUris: string[] }
	): Promise<OAuthClientWithSecret> {
		if (!repo) throw new Error('OAuth clients not available in local mode');
		const created = await repo.create(orgId, input);
		const { clientSecret: _clientSecret, ...withoutSecret } = created;
		clients = [...clients, withoutSecret];
		return created;
	}

	async function updateClient(
		orgId: string,
		clientId: string,
		input: Partial<{ name: string; scopes: OAuthScope[]; redirectUris: string[] }>
	): Promise<void> {
		if (!repo) return;
		const updated = await repo.update(orgId, clientId, input);
		clients = clients.map((c) => (c.id === clientId ? updated : c));
	}

	async function addOrg(orgId: string, clientId: string, otherOrgId: string): Promise<void> {
		if (!repo) return;
		const updated = await repo.addOrg(orgId, clientId, otherOrgId);
		clients = clients.map((c) => (c.id === clientId ? updated : c));
	}

	async function removeOrg(orgId: string, clientId: string, otherOrgId: string): Promise<void> {
		if (!repo) return;
		await repo.removeOrg(orgId, clientId, otherOrgId);
		clients = clients.map((c) =>
			c.id === clientId ? { ...c, orgIds: c.orgIds.filter((id) => id !== otherOrgId) } : c
		);
	}

	async function rotateSecret(
		orgId: string,
		clientId: string,
		options?: { revokeExisting?: boolean }
	): Promise<OAuthClientWithSecret> {
		if (!repo) throw new Error('OAuth clients not available in local mode');
		const rotated = await repo.rotateSecret(orgId, clientId, options);
		const { clientSecret: _clientSecret, ...withoutSecret } = rotated;
		clients = clients.map((c) => (c.id === clientId ? withoutSecret : c));
		return rotated;
	}

	async function revokeClient(orgId: string, clientId: string): Promise<void> {
		if (!repo) return;
		await repo.revoke(orgId, clientId);
		clients = clients.map((c) =>
			c.id === clientId ? { ...c, revokedAt: new Date().toISOString() } : c
		);
	}

	async function loadTokens(orgId: string, clientId: string): Promise<void> {
		if (!repo) return;
		tokensLoaded = false;
		try {
			tokens = await repo.listTokens(orgId, clientId);
			tokensLoaded = true;
			tokensLoadedClientId = clientId;
		} catch (err) {
			handleAuthError(err);
		}
	}

	async function revokeToken(orgId: string, clientId: string, tokenId: string): Promise<void> {
		if (!repo) return;
		await repo.revokeToken(orgId, clientId, tokenId);
		tokens = tokens.filter((t) => t.id !== tokenId);
	}

	async function revokeAllTokens(orgId: string, clientId: string): Promise<void> {
		if (!repo) return;
		await repo.revokeAllTokens(orgId, clientId);
		tokens = [];
	}

	return {
		get clients() {
			return clients;
		},
		get loaded() {
			return loaded;
		},
		get loadedOrgId() {
			return loadedOrgId;
		},
		get available() {
			return repo !== null;
		},
		get tokens() {
			return tokens;
		},
		get tokensLoaded() {
			return tokensLoaded;
		},
		get tokensLoadedClientId() {
			return tokensLoadedClientId;
		},
		load,
		createClient,
		updateClient,
		addOrg,
		removeOrg,
		rotateSecret,
		revokeClient,
		loadTokens,
		revokeToken,
		revokeAllTokens
	};
}

export const oauthClientsStore = createOAuthClientsStore();
