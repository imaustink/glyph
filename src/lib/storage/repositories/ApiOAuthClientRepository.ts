import { api } from '$lib/storage/apiClient';
import type { OAuthClient, OAuthClientWithSecret, OAuthScope, OAuthToken } from '$lib/models/types';

export class ApiOAuthClientRepository {
	async list(orgId: string): Promise<OAuthClient[]> {
		return (await api.get<OAuthClient[]>(`/api/v1/orgs/${orgId}/oauth-clients`)) ?? [];
	}

	async get(orgId: string, clientId: string): Promise<OAuthClient> {
		return api.get<OAuthClient>(`/api/v1/orgs/${orgId}/oauth-clients/${clientId}`);
	}

	async create(
		orgId: string,
		input: { name: string; scopes: OAuthScope[]; redirectUris: string[] }
	): Promise<OAuthClientWithSecret> {
		return api.post<OAuthClientWithSecret>(`/api/v1/orgs/${orgId}/oauth-clients`, input);
	}

	async update(
		orgId: string,
		clientId: string,
		input: Partial<{ name: string; scopes: OAuthScope[]; redirectUris: string[] }>
	): Promise<OAuthClient> {
		return api.patch<OAuthClient>(`/api/v1/orgs/${orgId}/oauth-clients/${clientId}`, input);
	}

	async addOrg(orgId: string, clientId: string, otherOrgId: string): Promise<OAuthClient> {
		return api.post<OAuthClient>(`/api/v1/orgs/${orgId}/oauth-clients/${clientId}/orgs`, {
			orgId: otherOrgId
		});
	}

	async removeOrg(orgId: string, clientId: string, otherOrgId: string): Promise<void> {
		await api.del(`/api/v1/orgs/${orgId}/oauth-clients/${clientId}/orgs/${otherOrgId}`);
	}

	async rotateSecret(
		orgId: string,
		clientId: string,
		options?: { revokeExisting?: boolean }
	): Promise<OAuthClientWithSecret> {
		const query = options?.revokeExisting ? '?revokeExisting=true' : '';
		return api.post<OAuthClientWithSecret>(
			`/api/v1/orgs/${orgId}/oauth-clients/${clientId}/rotate-secret${query}`,
			{}
		);
	}

	async revoke(orgId: string, clientId: string): Promise<void> {
		await api.post(`/api/v1/orgs/${orgId}/oauth-clients/${clientId}/revoke`, {});
	}

	async listTokens(orgId: string, clientId: string): Promise<OAuthToken[]> {
		return (
			(await api.get<OAuthToken[]>(`/api/v1/orgs/${orgId}/oauth-clients/${clientId}/tokens`)) ?? []
		);
	}

	async revokeToken(orgId: string, clientId: string, tokenId: string): Promise<void> {
		await api.del(`/api/v1/orgs/${orgId}/oauth-clients/${clientId}/tokens/${tokenId}`);
	}

	async revokeAllTokens(orgId: string, clientId: string): Promise<void> {
		await api.post(`/api/v1/orgs/${orgId}/oauth-clients/${clientId}/tokens/revoke-all`, {});
	}
}
